package optimizer

import (
	"context"

	"github.com/acmestack/gpi/internal/cloud/catalog"
	"github.com/acmestack/gpi/internal/metacache"
)

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
