package optimizer

import (
	"context"

	"github.com/acmestack/gpi/internal/task"
)

// This file is the home of the "time" metric — gpi's run-time-minimizing
// placement goal. Time ranks candidates by estimated runtime (SkyPilot
// minimize=TIME): the fastest feasible machine comes first as the primary
// failover choice. Everything time-specific lives here: the timeMetric and
// the runtime-estimation heuristic.

// timeMetric ranks candidates by estimated runtime in hours (SkyPilot
// minimize=TIME). The estimate is pre-computed into candidate.EstimatedTime by
// the strategy optimizer before ranking, so Rank just returns it.
type timeMetric struct{}

// Name returns the metric.s registered name ("time").
func (timeMetric) Name() string { return "time" }

// Rank returns the candidate's estimated runtime in hours. The value is
// pre-computed by the strategy optimizer into c.EstimatedTime.
func (timeMetric) Rank(c *Candidate, _ bool) float64 { return c.EstimatedTime }

// OptimizeByTime ranks placement candidates by estimated runtime ascending
// using the shared metadata cache. It is the explicit form of "pick the
// fastest feasible machine, then the next fastest, and so on".
func OptimizeByTime(rs *task.Resources, opts *Options) (*Plan, error) {
	return OptimizeByTimeContext(context.Background(), rs, opts)
}

// OptimizeByTimeContext is OptimizeByTime with a caller-supplied context, used
// so live price refreshes can be cancelled/timed out with the parent
// operation.
func OptimizeByTimeContext(ctx context.Context, rs *task.Resources, opts *Options) (*Plan, error) {
	return NewStrategy(timeMetric{}).Optimize(ctx, &Request{
		Resources: rs,
		Options:   opts,
	})
}

// estimateRuntime returns the estimated runtime in hours for a candidate. If
// the task carries an explicit runtime estimate (resources.time_sec), it wins.
// Otherwise a workload-neutral estimate is derived from compute capacity: a
// baseline machine (4 vcpus, no GPU) is taken to run in 1 hour, and stronger
// machines finish proportionally faster.
func estimateRuntime(rs *task.Resources, c *Candidate) float64 {
	if rs.TimeSec != nil && *rs.TimeSec > 0 {
		return float64(*rs.TimeSec) / 3600.0
	}
	compute := float64(c.VCPUs)
	if compute <= 0 {
		compute = 4
	}
	totalGPU := 0
	for _, n := range c.Accelerators {
		totalGPU += n
	}
	if totalGPU > 0 {
		// A GPU unit is treated as ~16 vcpus of compute throughput.
		compute += float64(totalGPU) * 16
	}
	return 4.0 / compute
}
