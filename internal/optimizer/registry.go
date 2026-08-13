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

var registry = map[string]Optimizer{}

// DefaultName is the name of the built-in cost-based optimizer.
const DefaultName = "cost"

// Register adds an optimizer implementation by name.
func Register(o Optimizer) {
	if o.Name() == "" {
		panic("optimizer: registered optimizer must have a name")
	}
	registry[o.Name()] = o
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

// Names lists all registered optimizer names.
func Names() []string {
	out := make([]string, 0, len(registry))
	for name := range registry {
		out = append(out, name)
	}
	return out
}

// Default returns the default optimizer ("cost").
func Default() Optimizer {
	return registry[DefaultName]
}
