// Package catalog defines the metadata contract for clouds — instance specs,
// regions and prices — plus the TTL cache that serves them. It mirrors
// SkyPilot's sky/catalogs: the catalog is the source of truth for instance
// types and their (live) prices, decoupled from the cloud providers that
// fetch them. Contract, registry and cache live together so consumers depend
// on a single catalog package.
package catalog

import (
	"context"
	"time"
)

// Instance describes an instance type's specs in a cloud/region. Prices are
// intentionally NOT part of this struct: they are volatile and always fetched
// live (and cached) through the Source, so static price data never goes stale.
type Instance struct {
	Cloud        string
	Region       string
	InstanceType string
	VCPUs        int
	MemoryGiB    float64
	Accelerators map[string]int
	MaxDiskGiB   float64
}

// Price is the current hourly price of an instance type in a region.
type Price struct {
	OnDemand float64
	Spot     float64
}

// Source fetches instance-type metadata (specs, regions and prices) for a
// single cloud. A Provider may implement Source too: cloud.Register detects
// this and registers the provider as its own metadata source automatically.
type Source interface {
	// Cloud returns the cloud name this source serves.
	Cloud() string
	// SpecsTTL is how long fetched instance specs stay valid. Specs change
	// rarely, so a long TTL (hours/days) is appropriate.
	SpecsTTL() time.Duration
	// PriceTTL is how long fetched prices stay valid. Prices change often,
	// so a short TTL (minutes) is appropriate.
	PriceTTL() time.Duration
	// Regions returns the cloud's supported regions.
	Regions(ctx context.Context) ([]string, error)
	// FetchSpecs returns the specs of instance types available in region.
	FetchSpecs(ctx context.Context, region string) ([]*Instance, error)
	// FetchPrices returns current prices for the given instance types in
	// region. Instance types the cloud does not know about are omitted.
	FetchPrices(ctx context.Context, region string, instanceTypes []string) (map[string]Price, error)
}

// DefaultSpecsTTL is used when a source returns a non-positive SpecsTTL.
const DefaultSpecsTTL = 24 * time.Hour

// DefaultPriceTTL is used when a source returns a non-positive PriceTTL.
const DefaultPriceTTL = 10 * time.Minute

var registry = map[string]Source{}

// Register adds a metadata source for a cloud.
func Register(s Source) {
	registry[s.Cloud()] = s
}

// SourceFor returns the registered metadata source for a cloud, or nil.
func SourceFor(cloud string) Source {
	return registry[cloud]
}

// HasCloud reports whether a metadata source is registered for the cloud.
func HasCloud(cloud string) bool {
	return SourceFor(cloud) != nil
}

// Clouds lists all registered cloud metadata source names.
func Clouds() []string {
	out := make([]string, 0, len(registry))
	for name := range registry {
		out = append(out, name)
	}
	return out
}

// ResetForTest clears the registry; only used by tests.
func ResetForTest() {
	registry = map[string]Source{}
}
