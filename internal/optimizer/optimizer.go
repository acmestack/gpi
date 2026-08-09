// Package optimizer ranks placement candidates for a task across clouds and
// regions. Built-in strategies follow SkyPilot's approach: build the feasible
// candidate set from the metadata catalog, rank each candidate by one or more
// objectives (cost, time, ...), and return the top as a failover Plan.
//
// The Objective interface is the extension point: implement one (e.g. latency,
// carbon, budget) and register it via RegisterObjective to use it in strategies
// like "cost,latency", or compose with NewStrategy. Candidate collection and
// pricing are handled internally against the shared metadata source.
package optimizer

import (
	"context"
	"fmt"
	"strings"

	"github.com/acmestack/gpi/internal/cloud/catalog"
	"github.com/acmestack/gpi/internal/metacache"
	"github.com/acmestack/gpi/internal/task"
)

// Launch is a single placement decision: which cloud/region/instance type to
// use, at what cost. It is one candidate row of a Plan.
type Launch struct {
	Cloud        string
	Region       string
	Zone         string
	InstanceType string
	NumNodes     int
	Accelerators map[string]int
	VCPUs        int
	MemoryGiB    float64
	OnDemandCost float64
	SpotCost     float64
	UseSpot      bool
	// EstimatedTime is the estimated runtime in hours for this placement. It
	// is only populated by optimizers that minimize run time (e.g. "time").
	EstimatedTime float64
	Order         int
}

// CostPerHour returns the hourly cost of this launch given its spot choice.
// When the chosen mode's price is missing, the other mode's price is used as
// an estimate so ranking and display stay meaningful even with partial data.
func (l *Launch) CostPerHour() float64 {
	if l.UseSpot {
		if l.SpotCost > 0 {
			return l.SpotCost
		}
		if l.OnDemandCost > 0 {
			return l.OnDemandCost * 0.3
		}
		return 0
	}
	if l.OnDemandCost > 0 {
		return l.OnDemandCost
	}
	if l.SpotCost > 0 {
		return l.SpotCost * 3
	}
	return 0
}

// TotalCostPerHour returns the total hourly cost across all nodes.
func (l *Launch) TotalCostPerHour() float64 {
	return l.CostPerHour() * float64(l.NumNodes)
}

// Plan is the optimizer output: ranked placement candidates plus total cost.
type Plan struct {
	Launches []*Launch
	// TotalCostPerHour is the hourly cost of the top candidate.
	TotalCostPerHour float64
	// TotalEstimatedTime is the estimated total runtime in hours of the top
	// candidate; only meaningful for time-minimizing optimizers.
	TotalEstimatedTime float64
}

// LaunchTypes returns the instance types of the plan's launches in ranked
// order (useful for tests and summaries).
func (p *Plan) LaunchTypes() []string {
	out := make([]string, 0, len(p.Launches))
	for _, l := range p.Launches {
		out = append(out, l.InstanceType)
	}
	return out
}

// Meta is the metadata accessor an optimizer uses to read instance specs,
// regions and prices. The shared instance is the unexported defaultMeta (a
// metacache.Cache); tests can swap it via SetDefaultMeta. Extension optimizers
// that need richer metadata implement Meta themselves and pass it through
// SetDefaultMeta.
type Meta interface {
	// Clouds returns the names of available clouds.
	Clouds(ctx context.Context) ([]string, error)
	// Instances returns the specs of instance types available in region.
	Instances(ctx context.Context, cloud, region string) ([]*catalog.Instance, error)
	// Regions returns the cloud's supported regions.
	Regions(ctx context.Context, cloud string) ([]string, error)
	// Prices returns current prices for the given instance types in region.
	Prices(ctx context.Context, cloud, region string, types []string) (map[string]catalog.Price, error)
	// PricesForced bypasses the TTL and fetches fresh prices.
	PricesForced(ctx context.Context, cloud, region string, types []string) (map[string]catalog.Price, error)
}

// Options controls the placement search space.
type Options struct {
	NumNodes      int
	UseSpot       bool
	Cloud         string
	Region        string
	Zone          string
	MaxCandidates int
}

// DefaultOptions returns Options with NumNodes=1 and MaxCandidates=10.
func DefaultOptions() *Options {
	return &Options{NumNodes: 1, MaxCandidates: 10}
}

// Request is everything an optimizer needs to rank placement candidates.
// Cloud metadata (specs/prices) is always read from the shared defaultMeta.
type Request struct {
	Resources *task.Resources
	Options   *Options
}

// Optimizer ranks placement candidates for a task. Implementations are
// registered by name and selected via CLI/server flags.
type Optimizer interface {
	Name() string
	Optimize(ctx context.Context, req *Request) (*Plan, error)
}

var registry = map[string]Optimizer{}

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

// DefaultName is the name of the built-in cost-based optimizer.
const DefaultName = "cost"

// defaultMeta is the shared metadata cache used by all optimizers. It is not
// exported; callers that need a forced price refresh use PricesForced, and
// tests/extensions swap it via SetDefaultMeta.
var defaultMeta = newCacheMeta(metacache.NewCache())

// SetDefaultMeta replaces the shared metadata accessor. It is mainly for tests
// that need to inject fake metadata without hitting the network; restore the
// original with defer afterwards.
func SetDefaultMeta(m Meta) {
	if m == nil {
		panic("optimizer: SetDefaultMeta requires a non-nil Meta")
	}
	defaultMeta = m
}

// PricesForced bypasses the TTL and fetches fresh prices for the given
// instance types in a cloud/region through the shared metadata source, used
// right before launch so the decision reflects the current market.
func PricesForced(ctx context.Context, cloud, region string, types []string) (map[string]catalog.Price, error) {
	return defaultMeta.PricesForced(ctx, cloud, region, types)
}
