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
//	optimizer.go   - Optimizer interface + resolution entry points (Get/Resolve)
//	plan.go        - Launch / Plan (placement decision + ranked output)
//	request.go     - Options / Request (search space + input)
//	meta.go        - Meta interface, shared defaultMeta, prices
//	registry.go    - named optimizer registry (Register/Names/Default)
//	objective.go   - Objective interface + registration + ParseStrategy/NewStrategy
//	strategy.go    - strategyOptimizer: lexicographic multi-objective ranking
//	candidate.go   - Candidate + shared collection/pricing pipeline
//	cost.go        - cost objective + OptimizeByCost entry points
//	time.go        - time objective + OptimizeByTime + runtime estimate
package optimizer

import (
	"context"
	"fmt"
	"strings"
)

// Optimizer ranks placement candidates for a task. Implementations are
// registered by name and selected via CLI/server flags.
type Optimizer interface {
	Name() string
	Optimize(ctx context.Context, req *Request) (*Plan, error)
}

// Get returns the optimizer registered under name. If name is a comma-
// separated strategy of objectives (e.g. "cost,time"), it builds the strategy
// optimizer on the fly; unknown single names return nil.
func Get(name string) Optimizer {
	if o, ok := registry[name]; ok {
		return o
	}
	if strings.Contains(name, ",") {
		if o, err := ParseStrategy(name); err == nil {
			return o
		}
	}
	return nil
}

// Resolve returns the optimizer for a selection spec: an empty spec yields the
// default optimizer, a single name yields the registered optimizer, and a
// comma-separated list yields a strategy. It returns an error for unknown
// names so callers (CLI/server) can surface a helpful message.
func Resolve(spec string) (Optimizer, error) {
	if spec == "" {
		return Default(), nil
	}
	if o := Get(spec); o != nil {
		return o, nil
	}
	return nil, fmt.Errorf("unknown optimizer %q (registered: %s)", spec, strings.Join(Names(), ", "))
}
