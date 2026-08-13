package optimizer_test

import (
	"context"
	"strings"
	"testing"

	"github.com/acmestack/gpi/internal/cloud/catalog"
	"github.com/acmestack/gpi/internal/optimizer"
	"github.com/acmestack/gpi/internal/task"
)

// This external-package test proves extension optimizers can be built with the
// public API alone (Metric, RegisterMetric, ParseStrategy, NewStrategy,
// SetDefaultMeta) — no access to package internals required.

// latencyMetric is a made-up extension metric: score by region name
// length (a stand-in for "region latency" metadata an extension would fetch).
type latencyMetric struct{}

func (latencyMetric) Name() string { return "latency" }

func (latencyMetric) Rank(c *optimizer.Candidate, _ bool) float64 {
	// Pretend farther/longer region names are higher latency.
	return float64(len(c.Region))
}

// fixedMeta is a minimal Meta backed by in-memory data.
type fixedMeta struct{}

func (fixedMeta) Clouds(context.Context) ([]string, error) { return []string{"aliyun"}, nil }
func (fixedMeta) Regions(context.Context, string) ([]string, error) {
	return []string{"cn-hangzhou", "eu-central-1"}, nil
}
func (fixedMeta) Instances(_ context.Context, _, region string) ([]*catalog.Instance, error) {
	return []*catalog.Instance{
		{Cloud: "aliyun", Region: region, InstanceType: "g6.large", VCPUs: 2, MemoryGiB: 8, MaxDiskGiB: 500},
	}, nil
}
func (fixedMeta) Prices(context.Context, string, string, []string) (map[string]catalog.Price, error) {
	return nil, nil
}
func (fixedMeta) PricesForced(context.Context, string, string, []string) (map[string]catalog.Price, error) {
	return nil, nil
}

// TestExternalMetricExtension verifies a third-party metric can be
// registered and used in a strategy, ranking by its own metadata.
func TestExternalMetricExtension(t *testing.T) {
	optimizer.RegisterMetric("latency", latencyMetric{})
	defer func() { /* keep registry clean for other tests */ }()

	strat, err := optimizer.ParseStrategy("latency")
	if err != nil {
		t.Fatalf("strategy with custom metric should parse, got %v", err)
	}
	ts, err := task.Parse([]byte("resources:\n  cpus: 2+"))
	if err != nil {
		t.Fatal(err)
	}
	optimizer.SetDefaultMeta(fixedMeta{})
	plan, err := strat.Optimize(context.Background(), &optimizer.Request{
		Resources: ts.Resources,
		Options:   &optimizer.Options{MaxCandidates: 5},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Launches) == 0 {
		t.Fatal("expected launches")
	}
	// cn-hangzhou (11 chars) should rank before eu-central-1 (13 chars).
	first := plan.Launches[0]
	if !strings.HasPrefix(first.Region, "cn-hangzhou") {
		t.Fatalf("expected shortest region first, got %s", first.Region)
	}
}
