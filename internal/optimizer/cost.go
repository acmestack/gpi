package optimizer

import (
	"context"

	"github.com/acmestack/gpi/internal/task"
)

// This file is the home of the "cost" objective — gpi's default placement
// goal. Cost ranks placement candidates by hourly price (SkyPilot
// minimize=COST): among all feasible instance types, the cheapest fit comes
// first as the primary failover choice. Everything cost-specific lives here;
// the shared pipeline is in candidate.go and multi-objective ranking in
// strategy.go/objective.go.

// costObjective ranks candidates by hourly cost (SkyPilot minimize=COST).
// Lower hourly cost ranks earlier, so the cheapest feasible machine is the
// primary placement candidate.
type costObjective struct{}

// Name returns the objective's registered name ("cost").
func (costObjective) Name() string { return "cost" }

// Rank returns the candidate's hourly cost under the given spot preference.
// A missing price is estimated via CostPerHour's fallback so ranking stays
// meaningful; truly unpriced candidates are sunk by the strategy optimizer.
func (costObjective) Rank(c *Candidate, useSpot bool) float64 {
	return c.CostPerHour(useSpot)
}

// OptimizeByCost ranks placement candidates by hourly cost ascending using the
// shared metadata cache. It is the explicit form of the default behavior:
// "pick the cheapest feasible machine, then the next cheapest, and so on".
func OptimizeByCost(rs *task.Resources, opts *Options) (*Plan, error) {
	return OptimizeByCostContext(context.Background(), rs, opts)
}

// OptimizeByCostContext is OptimizeByCost with a caller-supplied context, used
// so live price refreshes can be cancelled/timed out with the parent
// operation.
func OptimizeByCostContext(ctx context.Context, rs *task.Resources, opts *Options) (*Plan, error) {
	return NewStrategy(costObjective{}).Optimize(ctx, &Request{
		Resources: rs,
		Options:   opts,
	})
}

// Optimize runs the default optimizer — which is the cost optimizer — against
// the shared metadata cache. It is a shorthand for callers that only need the
// default behavior and do not pick an optimizer explicitly.
func Optimize(rs *task.Resources, opts *Options) (*Plan, error) {
	return OptimizeByCostContext(context.Background(), rs, opts)
}

// OptimizeWithContext is Optimize with a caller-supplied context, used so live
// price refreshes can be cancelled/timed out with the parent operation.
func OptimizeWithContext(ctx context.Context, rs *task.Resources, opts *Options) (*Plan, error) {
	return OptimizeByCostContext(ctx, rs, opts)
}
