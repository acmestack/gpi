package optimizer

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/acmestack/gpi/internal/cloud/catalog"
	"github.com/acmestack/gpi/internal/task"
)

// fakeMeta is a deterministic Meta for tests: two clouds, specs + prices all
// in-memory so tests never hit the network.
type fakeMeta struct {
	instances map[string][]*catalog.Instance // key: cloud+"/"+region
	prices    map[string]map[string]catalog.Price
}

var testMeta = &fakeMeta{
	instances: map[string][]*catalog.Instance{
		"aliyun/cn-hangzhou": {
			{Cloud: "aliyun", Region: "cn-hangzhou", InstanceType: "ecs.g6.large", VCPUs: 2, MemoryGiB: 8, MaxDiskGiB: 500},
			{Cloud: "aliyun", Region: "cn-hangzhou", InstanceType: "ecs.gn7-c12g1.3xlarge", VCPUs: 12, MemoryGiB: 94, Accelerators: map[string]int{"A100": 1}, MaxDiskGiB: 500},
		},
		"aws/us-east-1": {
			{Cloud: "aws", Region: "us-east-1", InstanceType: "m5.large", VCPUs: 2, MemoryGiB: 8, MaxDiskGiB: 500},
			{Cloud: "aws", Region: "us-east-1", InstanceType: "p3.2xlarge", VCPUs: 8, MemoryGiB: 61, Accelerators: map[string]int{"V100": 1}, MaxDiskGiB: 500},
		},
	},
	prices: map[string]map[string]catalog.Price{
		"aliyun/cn-hangzhou": {
			"ecs.g6.large":          {OnDemand: 0.049, Spot: 0.015},
			"ecs.gn7-c12g1.3xlarge": {OnDemand: 2.2, Spot: 0.726},
		},
		"aws/us-east-1": {
			"m5.large":   {OnDemand: 0.096, Spot: 0.029},
			"p3.2xlarge": {OnDemand: 3.06, Spot: 0.918},
		},
	},
}

func (m *fakeMeta) Clouds(_ context.Context) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	for k := range m.instances {
		cloud, _, _ := strings.Cut(k, "/")
		if !seen[cloud] {
			seen[cloud] = true
			out = append(out, cloud)
		}
	}
	return out, nil
}

func (m *fakeMeta) Regions(_ context.Context, cloudName string) ([]string, error) {
	var out []string
	for k := range m.instances {
		prefix := cloudName + "/"
		if len(k) > len(prefix) && k[:len(prefix)] == prefix {
			out = append(out, k[len(prefix):])
		}
	}
	if len(out) == 0 {
		return nil, errors.New("unknown cloud " + cloudName)
	}
	return out, nil
}

func (m *fakeMeta) Instances(_ context.Context, cloudName, region string) ([]*catalog.Instance, error) {
	insts, ok := m.instances[cloudName+"/"+region]
	if !ok {
		return nil, nil
	}
	out := make([]*catalog.Instance, len(insts))
	for i, it := range insts {
		cp := *it
		if cp.Accelerators != nil {
			cp.Accelerators = map[string]int{}
			for k, v := range it.Accelerators {
				cp.Accelerators[k] = v
			}
		}
		out[i] = &cp
	}
	return out, nil
}

func (m *fakeMeta) Prices(_ context.Context, cloudName, region string, types []string) (map[string]catalog.Price, error) {
	all, ok := m.prices[cloudName+"/"+region]
	if !ok {
		return nil, nil
	}
	out := map[string]catalog.Price{}
	for _, it := range types {
		if p, ok := all[it]; ok {
			out[it] = p
		}
	}
	return out, nil
}

func (m *fakeMeta) PricesForced(ctx context.Context, cloudName, region string, types []string) (map[string]catalog.Price, error) {
	return m.Prices(ctx, cloudName, region, types)
}

// run executes the default optimizer with the given request.
func run(req *Request) (*Plan, error) {
	return Default().Optimize(context.Background(), req)
}

// testRequest builds a Request from a resources YAML fragment and options.
// It points the shared DefaultMeta at testMeta (deterministic, no network).
func testRequest(resources string, opts *Options) *Request {
	SetDefaultMeta(testMeta)
	ts, err := task.Parse([]byte("resources:\n" + resources))
	if err != nil {
		panic(err)
	}
	return &Request{Resources: ts.Resources, Options: opts}
}
func TestOptimizeAccelerator(t *testing.T) {
	req := testRequest("  accelerators: A100:1\n  cpus: 8+", &Options{NumNodes: 1, MaxCandidates: 3})
	plan, err := run(req)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Launches) == 0 {
		t.Fatal("no launches")
	}
	first := plan.Launches[0]
	if first.Accelerators["A100"] < 1 {
		t.Fatalf("first launch should have A100, got %v", first.Accelerators)
	}
	if first.Order != 1 {
		t.Fatal("first launch should be order 1")
	}

}

// TestOptimizeByCost verifies the explicit cost entry point: it ranks
// candidates by hourly cost ascending, so the cheapest feasible machine is
// first. g6.large (0.049) must beat the A100 gn7 (2.2) on a CPU-only task.
func TestOptimizeByCost(t *testing.T) {
	SetDefaultMeta(testMeta)
	ts, err := task.Parse([]byte("resources:\n  cpus: 2+\n  memory: 4+"))
	if err != nil {
		t.Fatal(err)
	}
	plan, err := OptimizeByCost(ts.Resources, &Options{NumNodes: 1, MaxCandidates: 3, Region: "cn-hangzhou"})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Launches) == 0 {
		t.Fatal("no launches")
	}
	if plan.Launches[0].InstanceType != "ecs.g6.large" {
		t.Fatalf("cost entry point should pick cheapest first, got %s", plan.Launches[0].InstanceType)
	}
	// Convenience alias Optimize must behave identically.
	alias, err := Optimize(ts.Resources, &Options{NumNodes: 1, MaxCandidates: 3, Region: "cn-hangzhou"})
	if err != nil {
		t.Fatal(err)
	}
	if alias.Launches[0].InstanceType != plan.Launches[0].InstanceType {
		t.Fatal("Optimize alias should match OptimizeByCost")
	}
}

// TestOptimizeByTime verifies the explicit time entry point: it ranks
// candidates by estimated runtime ascending, so the fastest feasible machine
// is first. The A100 gn7 must beat the cheaper CPU-only g6.large.
func TestOptimizeByTime(t *testing.T) {
	SetDefaultMeta(testMeta)
	ts, err := task.Parse([]byte("resources:\n  cpus: 2+\n  memory: 4+"))
	if err != nil {
		t.Fatal(err)
	}
	plan, err := OptimizeByTime(ts.Resources, &Options{NumNodes: 1, MaxCandidates: 3, Region: "cn-hangzhou"})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Launches) == 0 {
		t.Fatal("no launches")
	}
	if plan.Launches[0].InstanceType != "ecs.gn7-c12g1.3xlarge" {
		t.Fatalf("time entry point should pick fastest first, got %s", plan.Launches[0].InstanceType)
	}
	if plan.TotalEstimatedTime <= 0 {
		t.Fatalf("expected a positive estimated time, got %f", plan.TotalEstimatedTime)
	}
}

func TestOptimizeCPUOnly(t *testing.T) {
	req := testRequest("  cpus: 2+\n  memory: 4+", &Options{NumNodes: 1, MaxCandidates: 3})
	plan, err := run(req)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Launches) == 0 {
		t.Fatal("no launches")
	}
	if plan.Launches[0].VCPUs < 2 {
		t.Fatalf("first launch vcpus too low: %d", plan.Launches[0].VCPUs)
	}
}

func TestOptimizeNoMatch(t *testing.T) {
	req := testRequest("  instance_type: ecs.does-not-exist", &Options{NumNodes: 1, MaxCandidates: 3})
	if _, err := run(req); err == nil {
		t.Fatal("expected error for no matching instance")
	}
}

func TestOptimizeSpotCheaper(t *testing.T) {
	onDemand, err := run(testRequest("  cpus: 2+", &Options{NumNodes: 1, MaxCandidates: 5, UseSpot: false}))
	if err != nil {
		t.Fatal(err)
	}
	spot, err := run(testRequest("  cpus: 2+", &Options{NumNodes: 1, MaxCandidates: 5, UseSpot: true}))
	if err != nil {
		t.Fatal(err)
	}
	if spot.Launches[0].CostPerHour() >= onDemand.Launches[0].CostPerHour() {
		t.Fatal("spot should be cheaper or equal than on-demand")
	}
}

func TestOptimizeRegionFilter(t *testing.T) {
	plan, err := run(testRequest("  cpus: 2+", &Options{NumNodes: 1, MaxCandidates: 10, Region: "cn-hangzhou"}))
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range plan.Launches {
		if l.Region != "cn-hangzhou" {
			t.Fatalf("region filter violated: %s", l.Region)
		}
	}
}

func TestOptimizerRegistry(t *testing.T) {
	if Default().Name() != "cost" {
		t.Fatal("default optimizer should be cost")
	}
	if Get("cost") == nil {
		t.Fatal("cost optimizer not registered")
	}
	if Get("time") == nil {
		t.Fatal("time optimizer not registered")
	}
}

func TestTimeOptimizerRanksFastestFirst(t *testing.T) {
	// Under the time optimizer, the GPU machine (A100) has far more compute
	// capacity than the CPU-only g6.large, so it must rank first even though
	// it is far more expensive.
	req := testRequest("  cpus: 2+\n  memory: 4+", &Options{NumNodes: 1, MaxCandidates: 3, Region: "cn-hangzhou"})
	timeOpt := Get("time")
	if timeOpt == nil {
		t.Fatal("time optimizer not registered")
	}
	plan, err := timeOpt.Optimize(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	first := plan.Launches[0]
	if first.InstanceType != "ecs.gn7-c12g1.3xlarge" {
		t.Fatalf("expected fastest machine first, got %s (est %fh)", first.InstanceType, first.EstimatedTime)
	}
	if first.EstimatedTime <= 0 {
		t.Fatalf("expected a positive estimated time, got %f", first.EstimatedTime)
	}
}

func TestTimeOptimizerRespectsTimeSec(t *testing.T) {
	// When the task pins an explicit runtime (resources.time_sec), every
	// candidate gets that runtime; the ranking must still be deterministic and
	// estimated times must match the pinned value.
	ts, err := task.Parse([]byte("resources:\n  cpus: 2+\n  time_sec: 7200"))
	if err != nil {
		t.Fatal(err)
	}
	SetDefaultMeta(testMeta)
	req := &Request{Resources: ts.Resources, Options: &Options{NumNodes: 1, MaxCandidates: 3, Region: "cn-hangzhou"}}
	plan, err := Get("time").Optimize(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Launches) == 0 {
		t.Fatal("no launches")
	}
	if got := plan.Launches[0].EstimatedTime; got != 2.0 {
		t.Fatalf("expected pinned estimated time 2.0h, got %f", got)
	}
	if plan.TotalEstimatedTime != 2.0 {
		t.Fatalf("expected total estimated time 2.0h, got %f", plan.TotalEstimatedTime)
	}
}

func TestEstimateRuntimeHeuristic(t *testing.T) {
	cpuOnly := &Candidate{Instance: &catalog.Instance{VCPUs: 8}}
	gpu := &Candidate{Instance: &catalog.Instance{VCPUs: 8, Accelerators: map[string]int{"A100": 1}}}
	rs := task.DefaultResources()
	cpuTime := estimateRuntime(rs, cpuOnly)
	if cpuTime <= 0 {
		t.Fatal("cpu estimate must be positive")
	}
	if gpuTime := estimateRuntime(rs, gpu); gpuTime >= cpuTime {
		t.Fatalf("gpu estimate (%f) must be smaller than cpu estimate (%f)", gpuTime, cpuTime)
	}
}

func TestUnpricedCandidateRanksAfterPriced(t *testing.T) {
	// g6.large has prices; add a matching instance with NO price at all. It
	// must rank strictly after every priced candidate, never look cheapest.
	insts := append([]*catalog.Instance(nil), testMeta.instances["aliyun/cn-hangzhou"]...)
	insts = append(insts, &catalog.Instance{
		Cloud: "aliyun", Region: "cn-hangzhou", InstanceType: "ecs.mystery.2xlarge",
		VCPUs: 2, MemoryGiB: 8, MaxDiskGiB: 500,
	})
	m := &fakeMeta{instances: map[string][]*catalog.Instance{"aliyun/cn-hangzhou": insts}, prices: testMeta.prices}
	req := testRequest("  cpus: 2+\n  memory: 4+", &Options{NumNodes: 1, MaxCandidates: 10, Region: "cn-hangzhou"})
	SetDefaultMeta(m)
	plan, err := run(req)
	if err != nil {
		t.Fatal(err)
	}
	for i, l := range plan.Launches {
		if l.InstanceType == "ecs.mystery.2xlarge" {
			if i == 0 {
				t.Fatal("unpriced candidate must not rank first")
			}
			return
		}
	}
	t.Fatal("mystery instance missing from launches")
}

func TestCostPerHourFallback(t *testing.T) {
	// On-demand mode with only a spot price should estimate via spot*3; and a
	// launch with no price at all must cost 0.
	l := &Launch{OnDemandCost: 0, SpotCost: 0.243, UseSpot: false}
	if got := l.CostPerHour(); got < 0.7 || got > 0.75 {
		t.Fatalf("expected on-demand fallback ~0.729, got %f", got)
	}
	if got := (&Launch{SpotCost: 0.243, UseSpot: true}).CostPerHour(); got != 0.243 {
		t.Fatalf("expected spot cost 0.243, got %f", got)
	}
	if got := (&Launch{}).CostPerHour(); got != 0 {
		t.Fatalf("expected unpriced cost 0, got %f", got)
	}
}

func TestStrategyCostThenTime(t *testing.T) {
	// Strategy "cost,time": cheapest machine ranks first; among equal-cost
	// ties, the faster one ranks first. aliyun g6.large (0.049) is cheaper
	// than gn7 (2.2), so it must be #1 despite being far slower.
	req := testRequest("  cpus: 2+\n  memory: 4+", &Options{NumNodes: 1, MaxCandidates: 5, Region: "cn-hangzhou"})
	opt, err := ParseStrategy("cost,time")
	if err != nil {
		t.Fatal(err)
	}
	plan, err := opt.Optimize(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Launches[0].InstanceType != "ecs.g6.large" {
		t.Fatalf("cost,time strategy should pick cheapest first, got %s", plan.Launches[0].InstanceType)
	}
}

// TestStrategySortsByCostAscending guards against the parallel-array sort bug:
// scores must travel with their candidates as the slice is reordered.
func TestStrategySortsByCostAscending(t *testing.T) {
	// Candidates deliberately returned out of cost order (expensive first).
	insts := []*catalog.Instance{
		{Cloud: "aliyun", Region: "cn-hangzhou", InstanceType: "big", VCPUs: 104, MemoryGiB: 768, Accelerators: map[string]int{"A100": 8}, MaxDiskGiB: 500},
		{Cloud: "aliyun", Region: "cn-hangzhou", InstanceType: "small", VCPUs: 12, MemoryGiB: 94, Accelerators: map[string]int{"A100": 1}, MaxDiskGiB: 500},
		{Cloud: "aliyun", Region: "cn-hangzhou", InstanceType: "mid", VCPUs: 26, MemoryGiB: 220, Accelerators: map[string]int{"A100": 1}, MaxDiskGiB: 500},
	}
	prices := map[string]map[string]catalog.Price{
		"aliyun/cn-hangzhou": {
			"big":   {OnDemand: 252.7},
			"small": {OnDemand: 31.6},
			"mid":   {OnDemand: 63.2},
		},
	}
	m := &fakeMeta{instances: map[string][]*catalog.Instance{"aliyun/cn-hangzhou": insts}, prices: prices}
	req := testRequest("  accelerators: A100:1\n  cpus: 8+", &Options{NumNodes: 1, MaxCandidates: 5, Region: "cn-hangzhou"})
	SetDefaultMeta(m)
	opt, err := ParseStrategy("cost")
	if err != nil {
		t.Fatal(err)
	}
	plan, err := opt.Optimize(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"small", "mid", "big"}
	for i, l := range plan.Launches {
		if l.InstanceType != want[i] {
			t.Fatalf("position %d: expected %s, got %s (all: %v)", i, want[i], l.InstanceType, plan.LaunchTypes())
		}
	}
}

func TestStrategyTimeThenCost(t *testing.T) {
	// Strategy "time,cost": fastest machine ranks first even if it costs more.
	// gn7 (A100) is far faster than g6.large, so it must be #1.
	req := testRequest("  cpus: 2+\n  memory: 4+", &Options{NumNodes: 1, MaxCandidates: 5, Region: "cn-hangzhou"})
	opt, err := ParseStrategy("time,cost")
	if err != nil {
		t.Fatal(err)
	}
	plan, err := opt.Optimize(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Launches[0].InstanceType != "ecs.gn7-c12g1.3xlarge" {
		t.Fatalf("time,cost strategy should pick fastest first, got %s", plan.Launches[0].InstanceType)
	}
}

func TestGetResolvesStrategyName(t *testing.T) {
	if Get("cost,time") == nil {
		t.Fatal("Get should resolve comma-separated strategy")
	}
	if Get("time,cost") == nil {
		t.Fatal("Get should resolve comma-separated strategy")
	}
	if Get("cost") == nil || Get("cost").Name() != "cost" {
		t.Fatal("Get should resolve single objective")
	}
	if Get("bogus") != nil {
		t.Fatal("Get should return nil for unknown name")
	}
}

func TestParseStrategyErrors(t *testing.T) {
	if _, err := ParseStrategy("bogus"); err == nil {
		t.Fatal("expected error for unknown objective")
	}
	if _, err := ParseStrategy(","); err == nil {
		t.Fatal("expected error for empty strategy")
	}
	if _, err := ParseStrategy(""); err == nil {
		t.Fatal("expected error for empty strategy")
	}
}
