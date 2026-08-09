package optimizer

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// This file implements the strategy optimizer: it ranks placement candidates
// by multiple objectives in priority order (lexicographic multi-objective
// optimization). The shared candidate pipeline (candidate.go) runs once; only
// the scoring/ordering differs from a single-objective optimizer.

// strategyOptimizer ranks candidates by a priority-ordered list of objectives.
// A single-objective strategy behaves exactly like a named optimizer (e.g.
// "cost", "time").
type strategyOptimizer struct {
	objectives []Objective
}

// Name returns the optimizer's registered name: a comma-joined list of the
// objective names, e.g. "cost,time".
func (s strategyOptimizer) Name() string {
	names := make([]string, len(s.objectives))
	for i, o := range s.objectives {
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
//  5. If a "time" objective is present, pre-compute each candidate's estimated
//     runtime once (it is reused for ranking and display).
//  6. Rank candidates lexicographically by the objectives in priority order:
//     the primary objective decides, ties fall to the next objective, and so
//     on. Unpriced candidates always rank after priced ones so a missing price
//     never looks "best".
//  7. Keep the top MaxCandidates and return them as the Plan (failover order).
//
// The returned Plan carries both the hourly cost and the estimated runtime of
// the top candidate so callers can display either.
func (s strategyOptimizer) Optimize(ctx context.Context, req *Request) (*Plan, error) {
	if req == nil || req.Resources == nil {
		return nil, errors.New("optimizer: request and resources must not be nil")
	}
	opts := req.Options
	if opts == nil {
		opts = DefaultOptions()
	}
	if opts.NumNodes <= 0 {
		opts.NumNodes = 1
	}
	if opts.MaxCandidates <= 0 {
		opts.MaxCandidates = 10
	}
	if len(s.objectives) == 0 {
		return nil, errors.New("optimizer: strategy has no objectives")
	}

	useSpot := opts.UseSpot
	if req.Resources.UseSpot != nil {
		useSpot = *req.Resources.UseSpot
	}

	cands, err := collectCandidates(ctx, req.Resources, opts, opts.Cloud)
	if err != nil {
		return nil, err
	}
	if len(cands) == 0 {
		return nil, fmt.Errorf("no instance type found matching resources %s in %s", req.Resources.String(), cloudNames(opts))
	}

	// Bound price lookups to a spec-proxy-ranked subset (see Optimize step 3).
	if len(cands) > maxPricedCandidates {
		sort.SliceStable(cands, func(i, j int) bool {
			return cands[i].VCPUs < cands[j].VCPUs
		})
		cands = cands[:maxPricedCandidates]
	}
	attachPrices(ctx, cands, useSpot)

	// Pre-compute the runtime estimate once if a "time" objective is present,
	// so ranking and display reuse a single value per candidate.
	hasTime := false
	for _, o := range s.objectives {
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

	// Score each candidate on every objective. The scores travel with their
	// candidates inside a single struct so sorting never mixes them up.
	type scored struct {
		c *Candidate
		s []float64
	}
	scoredCands := make([]scored, len(cands))
	for i, c := range cands {
		scoredCands[i] = scored{c: c, s: make([]float64, len(s.objectives))}
		for j, o := range s.objectives {
			scoredCands[i].s[j] = o.Rank(c, useSpot)
		}
	}

	// Lexicographic sort by objective priority; unpriced candidates sink last.
	sort.SliceStable(scoredCands, func(i, j int) bool {
		pi, pj := scoredCands[i].c.Priced(), scoredCands[j].c.Priced()
		if pi != pj {
			return pi
		}
		for k := range s.objectives {
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
	return plan, nil
}

// NewStrategy builds an optimizer ranking candidates by the given objectives
// in priority order (first = primary, last = least important tie-break). With
// no objectives it falls back to a cost-only strategy.
func NewStrategy(objectives ...Objective) Optimizer {
	if len(objectives) == 0 {
		objectives = []Objective{costObjective{}}
	}
	return strategyOptimizer{objectives: objectives}
}

// ParseStrategy builds a strategy optimizer from a priority-ordered list of
// objective names, e.g. "cost,time" (cost primary, time tie-break). A single
// name yields a single-objective strategy equivalent to the named optimizer.
// Unknown objective names return an error listing the available ones.
func ParseStrategy(spec string) (Optimizer, error) {
	names := strings.Split(spec, ",")
	objs := make([]Objective, 0, len(names))
	for _, raw := range names {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		o, ok := objectiveBuilders[name]
		if !ok {
			return nil, fmt.Errorf("unknown objective %q (available: %s)", name, strings.Join(ObjectiveNames(), ", "))
		}
		objs = append(objs, o)
	}
	if len(objs) == 0 {
		return nil, fmt.Errorf("empty optimizer strategy %q", spec)
	}
	return strategyOptimizer{objectives: objs}, nil
}

// registerBuiltins registers the single-objective optimizers "cost" and "time"
// so optimizer.Get("cost") / Get("time") resolve to ready-to-use strategies.
func init() {
	Register(NewStrategy(costObjective{}))
	Register(NewStrategy(timeObjective{}))
}
