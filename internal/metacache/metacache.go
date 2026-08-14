// Package metacache implements a TTL-protected in-memory cache for cloud
// metadata (instance specs, regions and prices), reading through the catalog
// Source contract. It is the default metadata accessor used by the optimizer.
package metacache

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/acmestack/gpi/internal/cloud/catalog"
	"github.com/acmestack/gpi/internal/logging"
)

// logger is the package logger, tagged with the module name.
var logger = logging.WithName("metacache")

// Cache is a TTL-protected in-memory metadata cache for instance specs,
// regions and prices. It is safe for concurrent use. Fetch errors keep
// previously cached data so placement still works while a cloud is flaky or
// offline.
type Cache struct {
	mu sync.Mutex
	// key: cloud + "\x00" + region
	specs map[string]specEntry
	// key: cloud
	regions map[string]regionEntry
	// key: cloud + "\x00" + region
	prices map[string]priceEntry
}

type specEntry struct {
	instances []*catalog.Instance
	fetched   time.Time
}

type regionEntry struct {
	regions []string
	fetched time.Time
}

type priceEntry struct {
	prices   map[string]catalog.Price
	fetched  time.Time
	failedAt time.Time // zero until a fetch failed
}

// NewCache builds an empty metadata cache.
func NewCache() *Cache {
	return &Cache{
		specs:   map[string]specEntry{},
		regions: map[string]regionEntry{},
		prices:  map[string]priceEntry{},
	}
}

// cacheKey builds the in-memory map key for a (cloud, region) pair. The NUL
// separator avoids collisions between cloud/region name combinations.
func cacheKey(cloud, region string) string { return cloud + "\x00" + region }

// Instances returns cached instance specs for a cloud/region, fetching them
// live (via the cloud's Source) when missing or expired. The specs TTL comes
// from the source. On fetch failure previously cached specs are returned with
// the error so callers can fall back.
func (c *Cache) Instances(ctx context.Context, cloud, region string) ([]*catalog.Instance, error) {
	s := catalog.SourceFor(cloud)
	if s == nil {
		return nil, nil
	}
	ttl := s.SpecsTTL()
	if ttl <= 0 {
		ttl = catalog.DefaultSpecsTTL
	}
	k := cacheKey(cloud, region)
	now := time.Now()

	c.mu.Lock()
	entry, ok := c.specs[k]
	if ok && now.Sub(entry.fetched) <= ttl {
		c.mu.Unlock()
		return entry.instances, nil
	}
	c.mu.Unlock()

	fresh, err := s.FetchSpecs(ctx, region)
	c.mu.Lock()
	defer c.mu.Unlock()
	if err != nil {
		logger.Debug("fetch specs failed", "cloud", cloud, "region", region, "error", err)
		if ok {
			return entry.instances, err
		}
		return nil, err
	}
	c.specs[k] = specEntry{instances: fresh, fetched: now}
	logger.Debug("fetched specs", "cloud", cloud, "region", region, "count", len(fresh))
	return fresh, nil
}

// Regions returns cached region names for a cloud, fetching them live when
// missing or expired. The specs TTL applies (regions change rarely).
func (c *Cache) Regions(ctx context.Context, cloud string) ([]string, error) {
	s := catalog.SourceFor(cloud)
	if s == nil {
		return nil, nil
	}
	ttl := s.SpecsTTL()
	if ttl <= 0 {
		ttl = catalog.DefaultSpecsTTL
	}
	now := time.Now()

	c.mu.Lock()
	entry, ok := c.regions[cloud]
	if ok && now.Sub(entry.fetched) <= ttl {
		c.mu.Unlock()
		return entry.regions, nil
	}
	c.mu.Unlock()

	fresh, err := s.Regions(ctx)
	c.mu.Lock()
	defer c.mu.Unlock()
	if err != nil {
		return nil, err
	}
	c.regions[cloud] = regionEntry{regions: fresh, fetched: now}
	return fresh, nil
}

// Prices returns cached prices for the instance types in a cloud/region,
// fetching them live when missing or expired. The price TTL comes from the
// source. On fetch failure previously cached prices are returned with the
// error so callers can fall back to stale prices.
func (c *Cache) Prices(ctx context.Context, cloud, region string, instanceTypes []string) (map[string]catalog.Price, error) {
	s := catalog.SourceFor(cloud)
	if s == nil {
		return nil, nil
	}
	ttl := s.PriceTTL()
	if ttl <= 0 {
		ttl = catalog.DefaultPriceTTL
	}
	k := cacheKey(cloud, region)
	now := time.Now()

	c.mu.Lock()
	entry, ok := c.prices[k]
	if ok && now.Sub(entry.fetched) <= ttl {
		c.mu.Unlock()
		return entry.prices, nil
	}
	// If a recent fetch just failed, don't hammer the API again.
	if ok && !entry.failedAt.IsZero() && now.Sub(entry.failedAt) < 5*time.Second {
		c.mu.Unlock()
		return entry.prices, nil
	}
	c.mu.Unlock()

	fresh, err := s.FetchPrices(ctx, region, instanceTypes)
	c.mu.Lock()
	defer c.mu.Unlock()
	if err != nil {
		logger.Debug("fetch prices failed", "cloud", cloud, "region", region, "error", err)
		if ok {
			entry.failedAt = now
			c.prices[k] = entry
		}
		return entry.prices, err
	}
	// Merge: keep any prices we already had that the fetch did not return.
	merged := entry.prices
	if merged == nil {
		merged = map[string]catalog.Price{}
	}
	for it, p := range fresh {
		merged[it] = p
	}
	c.prices[k] = priceEntry{prices: merged, fetched: now}
	logger.Debug("fetched prices", "cloud", cloud, "region", region, "types", len(fresh))
	return merged, nil
}

// PricesForced bypasses the TTL and fetches fresh prices for the instance
// types, used right before launch so the decision reflects the current
// market. On failure it returns the cached prices (may be nil) and the error.
func (c *Cache) PricesForced(ctx context.Context, cloud, region string, instanceTypes []string) (map[string]catalog.Price, error) {
	s := catalog.SourceFor(cloud)
	if s == nil {
		return nil, nil
	}
	k := cacheKey(cloud, region)
	now := time.Now()

	c.mu.Lock()
	entry, ok := c.prices[k]
	c.mu.Unlock()

	fresh, err := s.FetchPrices(ctx, region, instanceTypes)
	c.mu.Lock()
	defer c.mu.Unlock()
	if err != nil {
		if ok {
			entry.failedAt = now
			c.prices[k] = entry
		}
		return entry.prices, err
	}
	merged := entry.prices
	if merged == nil {
		merged = map[string]catalog.Price{}
	}
	for it, p := range fresh {
		merged[it] = p
	}
	c.prices[k] = priceEntry{prices: merged, fetched: now}
	return merged, nil
}

// AvailableRegions returns the cached region list for a cloud, or an error if
// no source is registered.
func (c *Cache) AvailableRegions(ctx context.Context, cloud string) ([]string, error) {
	s := catalog.SourceFor(cloud)
	if s == nil {
		return nil, fmt.Errorf("unknown cloud %q (registered: %s)", cloud, strings.Join(catalog.Clouds(), ", "))
	}
	return c.Regions(ctx, cloud)
}
