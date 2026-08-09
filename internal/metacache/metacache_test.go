package metacache_test

import (
	"context"
	"testing"
	"time"

	"github.com/acmestack/gpi/internal/cloud/catalog"
	"github.com/acmestack/gpi/internal/metacache"

	// Register every cloud provider (aliyun + aws + ...) in one place.
	_ "github.com/acmestack/gpi/internal/cloud/imports"
)

func TestMultiCloudRegistered(t *testing.T) {
	names := catalog.Clouds()
	if len(names) < 2 {
		t.Fatalf("expected at least 2 clouds, got %v", names)
	}
	for _, name := range []string{"aws", "aliyun"} {
		if !catalog.HasCloud(name) {
			t.Fatalf("cloud %s not registered", name)
		}
	}
}

// fakeSource is a deterministic catalog.Source used to test the cache without
// network access.
type fakeSource struct{}

func (fakeSource) Cloud() string { return "fake" }

func (fakeSource) SpecsTTL() time.Duration { return time.Hour }

func (fakeSource) PriceTTL() time.Duration { return time.Minute }

func (fakeSource) Regions(context.Context) ([]string, error) {
	return []string{"region-1"}, nil
}

func (fakeSource) FetchSpecs(_ context.Context, region string) ([]*catalog.Instance, error) {
	return []*catalog.Instance{
		{Cloud: "fake", Region: region, InstanceType: "gpu-x", VCPUs: 4, MemoryGiB: 16, Accelerators: map[string]int{"A100": 1}, MaxDiskGiB: 500},
		{Cloud: "fake", Region: region, InstanceType: "cpu-2", VCPUs: 2, MemoryGiB: 4, MaxDiskGiB: 500},
	}, nil
}

func (fakeSource) FetchPrices(_ context.Context, _ string, types []string) (map[string]catalog.Price, error) {
	out := map[string]catalog.Price{}
	for _, it := range types {
		if it == "gpu-x" {
			out[it] = catalog.Price{OnDemand: 1.0, Spot: 0.3}
		} else {
			out[it] = catalog.Price{OnDemand: 0.1, Spot: 0.03}
		}
	}
	return out, nil
}

func TestCacheSpecsAndPrices(t *testing.T) {
	catalog.Register(fakeSource{})
	defer catalog.ResetForTest()
	c := metacache.NewCache()
	ctx := context.Background()

	insts, err := c.Instances(ctx, "fake", "region-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(insts) != 2 {
		t.Fatalf("expected 2 instances, got %d", len(insts))
	}

	prices, err := c.Prices(ctx, "fake", "region-1", []string{"gpu-x", "cpu-2"})
	if err != nil {
		t.Fatal(err)
	}
	if prices["gpu-x"].OnDemand != 1.0 {
		t.Fatalf("unexpected gpu-x price: %+v", prices["gpu-x"])
	}
	if prices["cpu-2"].Spot != 0.03 {
		t.Fatalf("unexpected cpu-2 spot: %+v", prices["cpu-2"])
	}
}

func TestCacheSpecsTTL(t *testing.T) {
	c := metacache.NewCache()
	ctx := context.Background()

	// Short-TTL source forces a refetch after the TTL expires.
	catalog.Register(shortTTLSource{})
	defer catalog.ResetForTest()
	if _, err := c.Instances(ctx, "short", "r"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(15 * time.Millisecond)
	if _, err := c.Instances(ctx, "short", "r"); err != nil {
		t.Fatal(err)
	}
}

type shortTTLSource struct{}

func (shortTTLSource) Cloud() string { return "short" }

func (shortTTLSource) SpecsTTL() time.Duration { return 10 * time.Millisecond }

func (shortTTLSource) PriceTTL() time.Duration { return 10 * time.Millisecond }

func (shortTTLSource) Regions(context.Context) ([]string, error) { return []string{"r"}, nil }

func (shortTTLSource) FetchSpecs(context.Context, string) ([]*catalog.Instance, error) {
	return []*catalog.Instance{{Cloud: "short", Region: "r", InstanceType: "x"}}, nil
}

func (shortTTLSource) FetchPrices(context.Context, string, []string) (map[string]catalog.Price, error) {
	return map[string]catalog.Price{}, nil
}
