package optimizer

import (
	"context"

	"github.com/acmestack/gpi/internal/cloud/catalog"
	"github.com/acmestack/gpi/internal/metacache"
)

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

// cacheMeta adapts metacache.Cache to the optimizer.Meta interface so the
// default optimizer can read cloud metadata (clouds, regions, specs, prices)
// straight from the shared cache. Extension optimizers may use their own Meta
// implementation enriched with additional metadata.
type cacheMeta struct {
	cache *metacache.Cache
}

// Clouds implements Meta.Clouds by listing registered metadata sources.
func (a cacheMeta) Clouds(context.Context) ([]string, error) {
	return catalog.Clouds(), nil
}

// Regions implements Meta.Regions via the underlying cache.
func (a cacheMeta) Regions(ctx context.Context, cloud string) ([]string, error) {
	return a.cache.Regions(ctx, cloud)
}

// Instances implements Meta.Instances via the underlying cache.
func (a cacheMeta) Instances(ctx context.Context, cloud, region string) ([]*catalog.Instance, error) {
	return a.cache.Instances(ctx, cloud, region)
}

// Prices implements Meta.Prices via the underlying cache.
func (a cacheMeta) Prices(ctx context.Context, cloud, region string, types []string) (map[string]catalog.Price, error) {
	return a.cache.Prices(ctx, cloud, region, types)
}

// PricesForced implements Meta.PricesForced via the underlying cache.
func (a cacheMeta) PricesForced(ctx context.Context, cloud, region string, types []string) (map[string]catalog.Price, error) {
	return a.cache.PricesForced(ctx, cloud, region, types)
}

// newCacheMeta wraps a metacache.Cache as an optimizer.Meta; it backs the
// unexported defaultMeta.
func newCacheMeta(cache *metacache.Cache) Meta {
	return cacheMeta{cache: cache}
}
