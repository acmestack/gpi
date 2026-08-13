package optimizer

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/acmestack/gpi/internal/task"
)

// This file implements the lexicographicOptimizer: it ranks placement
// candidates by multiple metrics in priority order (lexicographic multi-metric
// optimization). The shared candidate pipeline (candidate.go) runs once; only
// the scoring/ordering differs from a single-metric optimizer. Strategy
// construction (NewStrategy/ParseStrategy) lives in strategy.go.

// lexicographicOptimizer ranks candidates by a priority-ordered list of metrics.
// A single-metric strategy behaves exactly like a named optimizer (e.g.
// "cost", "time").
type lexicographicOptimizer struct {
	metrics []Metric
}

// Name returns the optimizer's registered name: a comma-joined list of the
// metric names, e.g. "cost,time".
func (s lexicographicOptimizer) Name() string {
	names := make([]string, len(s.metrics))
	for i, o := range s.metrics {
		names[i] = o.Name()
	}
	return strings.Join(names, ",")
}

// Optimize ranks the placement candidates for a task. Steps:
//
//  1. Resolve defaults for options (node count, candidate budget). Cloud
//     metadata is always read from the shared defaultMeta.
//  2. Collect the feasible candidate set: every instance type across the
//     selected clouds/regions that satisfies the task's resource requirements.
//  3. Bound the price lookups: when the region offers more candidates than the
//     budget, pre-rank by vcpus (a spec proxy) and keep the cheapest-to-price
//     subset, since the smallest fitting machines are usually the cheapest.
//  4. Attach live prices to the surviving candidates (grouped per cloud/region
//     so one refresh covers all types there). Failures keep stale prices.
//  5. If a "time" metric is present, pre-compute each candidate's estimated
//     runtime once (it is reused for ranking and display).
//  6. Rank candidates lexicographically by the metrics in priority order:
//     the primary metric decides, ties fall to the next metric, and so
//     on. Unpriced candidates always rank after priced ones so a missing price
//     never looks "best".
//  7. Keep the top MaxCandidates and return them as the Plan (failover order).
//
// The returned Plan carries both the hourly cost and the estimated runtime of
// the top candidate so callers can display either.
func (s lexicographicOptimizer) Optimize(ctx context.Context, req *Request) (*Plan, error) {
	if req == nil || req.Resources == nil {
		return nil, errors.New("optimizer: request and resources must not be nil")
	}
	opts := req.Options
	if opts == nil {
		opts = defaultOptions()
	}
	if opts.NumNodes <= 0 {
		opts.NumNodes = 1
	}
	if opts.MaxCandidates <= 0 {
		opts.MaxCandidates = 10
	}
	if len(s.metrics) == 0 {
		return nil, errors.New("optimizer: strategy has no metrics")
	}

	useSpot := opts.UseSpot
	if req.Resources.UseSpot != nil {
		useSpot = *req.Resources.UseSpot
	}

	// Ordered failover: the primary resources rank first, then each ordered
	// entry's candidates follow in the given order. Without ordered, only the
	// primary resources are considered.
	groups := []*task.Resources{req.Resources}
	for _, entry := range req.Resources.Ordered {
		groups = append(groups, entry)
	}

	var allCands []*Candidate
	// groupOf records which ordered group each candidate came from, so ranking
	// preserves the ordered failover order (group 0 first, then group 1, ...).
	groupOf := make(map[*Candidate]int)
	for gi, rs := range groups {
		groupCands, err := s.collectAndPrice(ctx, req, rs, opts, useSpot)
		if err != nil {
			return nil, err
		}
		for _, c := range groupCands {
			groupOf[c] = gi
		}
		allCands = append(allCands, groupCands...)
	}
	if len(allCands) == 0 {
		return nil, fmt.Errorf("no instance type found matching resources %s in %s", req.Resources.String(), cloudNames(opts))
	}

	plan := s.rankPlan(allCands, groupOf, req, opts, useSpot)
	return plan, nil
}

// collectAndPrice gathers feasible candidates for one resource group and
// attaches live prices, bounding lookups to the spec-proxy subset.
func (s lexicographicOptimizer) collectAndPrice(ctx context.Context, req *Request, rs *task.Resources, opts *Options, useSpot bool) ([]*Candidate, error) {
	cands, err := collectCandidates(ctx, rs, opts, opts.Cloud)
	if err != nil {
		return nil, err
	}
	if len(cands) > maxPricedCandidates {
		sort.SliceStable(cands, func(i, j int) bool {
			return cands[i].VCPUs < cands[j].VCPUs
		})
		cands = cands[:maxPricedCandidates]
	}
	attachPrices(ctx, cands, useSpot)
	return cands, nil
}

// rankPlan scores and lexicographically sorts all candidates across every
// ordered group, then emits them as the failover Plan (group order preserved:
// primary group's candidates first, then each ordered entry in turn).
func (s lexicographicOptimizer) rankPlan(cands []*Candidate, groupOf map[*Candidate]int, req *Request, opts *Options, useSpot bool) *Plan {
	// Pre-compute the runtime estimate once if a "time" metric is present,
	// so ranking and display reuse a single value per candidate.
	hasTime := false
	for _, o := range s.metrics {
		if o.Name() == "time" {
			hasTime = true
			break
		}
	}
	if hasTime {
		for _, c := range cands {
			c.EstimatedTime = estimateRuntime(req.Resources, c)
		}
	}

	// Score each candidate on every metric. The scores travel with their
	// candidates inside a single struct so sorting never mixes them up.
	type scored struct {
		c *Candidate
		s []float64
	}
	scoredCands := make([]scored, len(cands))
	for i, c := range cands {
		scoredCands[i] = scored{c: c, s: make([]float64, len(s.metrics))}
		for j, o := range s.metrics {
			scoredCands[i].s[j] = o.Rank(c, useSpot)
		}
	}

	// Ordered failover first: group 0's candidates rank before group 1's, and
	// so on. Within a group, lexicographic sort by metric priority applies;
	// unpriced candidates sink last.
	sort.SliceStable(scoredCands, func(i, j int) bool {
		gi, gj := groupOf[scoredCands[i].c], groupOf[scoredCands[j].c]
		if gi != gj {
			return gi < gj
		}
		pi, pj := scoredCands[i].c.Priced(), scoredCands[j].c.Priced()
		if pi != pj {
			return pi
		}
		for k := range s.metrics {
			if scoredCands[i].s[k] != scoredCands[j].s[k] {
				return scoredCands[i].s[k] < scoredCands[j].s[k]
			}
		}
		return false
	})
	for i := range scoredCands {
		cands[i] = scoredCands[i].c
	}
	if len(cands) > opts.MaxCandidates {
		cands = cands[:opts.MaxCandidates]
	}

	launches := make([]*Launch, 0, len(cands))
	for i, c := range cands {
		launches = append(launches, &Launch{
			Cloud:         c.Cloud,
			Region:        c.Region,
			Zone:          "",
			InstanceType:  c.InstanceType,
			NumNodes:      opts.NumNodes,
			Accelerators:  c.Accelerators,
			VCPUs:         c.VCPUs,
			MemoryGiB:     c.MemoryGiB,
			OnDemandCost:  c.OnDemand,
			SpotCost:      c.Spot,
			UseSpot:       useSpot,
			EstimatedTime: c.EstimatedTime,
			Order:         i + 1,
		})
	}
	plan := &Plan{Launches: launches, TotalCostPerHour: launches[0].CostPerHour()}
	plan.TotalEstimatedTime = launches[0].EstimatedTime
	return plan
}
