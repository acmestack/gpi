package optimizer

import (
	"context"
	"strings"

	"github.com/acmestack/gpi/internal/cloud/catalog"
	"github.com/acmestack/gpi/internal/task"
)

// This file holds the candidate representation and the shared pipeline that
// the strategy optimizer uses internally: collect feasible instances from the
// shared metadata source, then attach live prices. Candidate is exported
// because it is the unit an Metric ranks; the pipeline itself is unexported
// since extensions compose metrics instead of reimplementing matching.

// maxPricedCandidates bounds how many candidates get a live price lookup; the
// rest are dropped after a spec-proxy pre-rank. SkyPilot similarly limits the
// launchable set it scores.
const maxPricedCandidates = 200

// Candidate is a cloud instance spec augmented with live prices. The spec
// intentionally carries no price fields (prices are volatile), so scoring
// happens on this wrapper. It is the unit an Metric ranks.
type Candidate struct {
	*catalog.Instance
	// OnDemand is the hourly on-demand price, 0 when unavailable.
	OnDemand float64
	// Spot is the hourly spot price, 0 when unavailable.
	Spot float64
	// EstimatedTime is the estimated runtime in hours, populated by the
	// "time" metric for ranking/display.
	EstimatedTime float64
}

// CostPerHour returns the hourly cost given the spot choice. A missing price
// falls back to the other mode's estimate so ranking stays meaningful even
// when one of the two price signals is unavailable.
func (c *Candidate) CostPerHour(useSpot bool) float64 {
	return (&Launch{
		OnDemandCost: c.OnDemand,
		SpotCost:     c.Spot,
		UseSpot:      useSpot,
	}).CostPerHour()
}

// Priced reports whether the candidate has any live price signal, used to keep
// unpriced candidates from ranking ahead of priced ones.
func (c *Candidate) Priced() bool { return c.OnDemand > 0 || c.Spot > 0 }

// collectCandidates collects feasible instance types from the shared metadata
// source for every selected cloud/region, matching the task's resource
// requirements. cloudFilter is a comma-separated list of clouds to restrict to
// (empty = all).
func collectCandidates(ctx context.Context, rs *task.Resources, opts *Options, cloudFilter string) ([]*Candidate, error) {
	selected := map[string]bool{}
	if cloudFilter != "" {
		for _, c := range strings.Split(cloudFilter, ",") {
			selected[strings.TrimSpace(c)] = true
		}
	}
	names, err := defaultMeta.Clouds(ctx)
	if err != nil {
		return nil, err
	}
	var out []*Candidate
	for _, name := range names {
		if len(selected) > 0 && !selected[name] {
			continue
		}
		regions, err := defaultMeta.Regions(ctx, name)
		if err != nil {
			return nil, err
		}
		regionFilter := opts.Region
		if regionFilter == "" {
			regionFilter = rs.Region
		}
		for _, region := range regions {
			if regionFilter != "" && region != regionFilter {
				continue
			}
			insts, err := defaultMeta.Instances(ctx, name, region)
			if err != nil {
				return nil, err
			}
			for _, inst := range insts {
				if !matchesResources(inst, rs) {
					continue
				}
				out = append(out, &Candidate{Instance: inst})
			}
		}
	}
	return out, nil
}

// attachPrices fills each candidate's live prices from the shared metadata
// source, grouping by (cloud, region) so one refresh covers all instance types
// there. Failures are intentionally ignored: stale cached prices (or the other
// pricing mode's estimate) keep placement working.
func attachPrices(ctx context.Context, cands []*Candidate, useSpot bool) {
	groups := map[string][]*Candidate{}
	for _, c := range cands {
		g := c.Cloud + "\x00" + c.Region
		groups[g] = append(groups[g], c)
	}
	for g, members := range groups {
		cloud, region, _ := strings.Cut(g, "\x00")
		types := make([]string, 0, len(members))
		for _, c := range members {
			types = append(types, c.InstanceType)
		}
		prices, err := defaultMeta.Prices(ctx, cloud, region, types)
		if err != nil || len(prices) == 0 {
			continue
		}
		for _, c := range members {
			p, ok := prices[c.InstanceType]
			if !ok {
				continue
			}
			if p.OnDemand > 0 {
				c.OnDemand = p.OnDemand
			}
			if p.Spot > 0 {
				c.Spot = p.Spot
			}
		}
	}
}

// cloudNames returns a human-readable label for the cloud filter in error
// messages (e.g. "aws" when filtering, "all clouds" otherwise).
func cloudNames(opts *Options) string {
	if opts.Cloud != "" {
		return opts.Cloud
	}
	return "all clouds"
}
