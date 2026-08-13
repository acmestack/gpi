// Package optimizer ranks placement candidates for a task across clouds and
// regions. Built-in strategies follow SkyPilot's approach: build the feasible
// candidate set from the metadata catalog, rank each candidate by one or more
// objectives (cost, time, ...), and return the top as a failover Plan.
//
// The Objective interface is the extension point: implement one (e.g. latency,
// carbon, budget) and register it via RegisterObjective to use it in strategies
// like "cost,latency", or compose with NewStrategy. Candidate collection and
// pricing are handled internally against the shared metadata source.
//
// Package layout:
//
//	plan.go       - Launch / Plan (placement decision + ranked output)
//	request.go    - Options / Request (search space + input)
//	meta.go       - Meta interface, shared defaultMeta, prices
//	registry.go   - Optimizer interface + named registry
//	objective.go  - Objective interface + registration + ParseStrategy/NewStrategy
//	strategy.go   - strategyOptimizer: lexicographic multi-objective ranking
//	candidate.go  - Candidate + shared collection/pricing pipeline
//	cost.go       - cost objective + OptimizeByCost entry points
//	time.go       - time objective + OptimizeByTime + runtime estimate
package optimizer
