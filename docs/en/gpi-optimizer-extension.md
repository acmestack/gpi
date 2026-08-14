# Extending the Placement Optimizer

- **Doc version**: v9 (2026-08-13)
- **Applies to**: Gpi (`github.com/acmestack/gpi`)

## Overview

This guide walks through how to extend gpi's placement optimizer (`internal/optimizer`). It answers three kinds of questions:

1. **How to use the built-in optimizers/policies** (no code, just CLI/API parameters).
2. **How to add a new optimization objective** (implement a `Metric`, e.g. latency / carbon / budget) and plug it into strategies like `cost,time`.
3. **How to write a fully custom Optimizer** (replacing the whole sorting logic).

After reading this guide you should be able to implement and register a custom optimizer on your own, and write tests for it.

---

## 1. Architecture overview

The optimizer package is split into files by responsibility:

| File | Contents |
|------|----------|
| `optimizer.go` | `Optimizer` interface + `Get`/`Resolve` resolution entry points (resolve by name/strategy) |
| `plan.go` | `Launch`/`Plan` (placement decision + sorted output) |
| `request.go` | `Options`/`Request` (search space + input) |
| `meta.go` | `Meta` interface + shared `defaultMeta` (including `newCacheMeta` adapter) + `SetDefaultMeta`/`PricesForced` |
| `registry.go` | Named optimizer registry (`Register`/`Names`/`Default`/`DefaultName`) |
| `candidate.go` | `Candidate` (sortable candidate unit) + internal candidate pipeline (`collectCandidates`/`attachPrices`, unexported) |
| `metric.go` | `Metric` interface + `RegisterMetric`/`MetricNames` (metric registry) |
| `lexicographic.go` | `lexicographicOptimizer`: lexicographic multi-objective sort implementation (`Name`/`Optimize`) |
| `strategy.go` | Strategy construction: `NewStrategy`/`ParseStrategy` + built-in `cost`/`time` registration |
| `cost.go` | **Ownership of the cost metric**: `costMetric` definition + explicit entry points `OptimizeByCost`/`OptimizeByCostContext` + aliases `Optimize`/`OptimizeWithContext` |
| `time.go` | **Ownership of the time metric**: `timeMetric` definition + explicit entry points `OptimizeByTime`/`OptimizeByTimeContext` + `estimateRuntime` heuristic |
| `match.go` | `matchesResources`: candidate resource matching filter |

### What happens during one optimization

```
Request{Resources, Options}
        │
        ▼
lexicographicOptimizer.Optimize ──► collectCandidates()   // 1. Match resource requirements, gather feasible instance types
        │                      attachPrices()         // 2. Fetch real-time prices concurrently (with budget truncation)
        │                      (time objective→precompute estTime)
        │                      o.Rank(c, useSpot)     // 3. Score each objective
        │                      sort lexicographic     // 4. Lexicographic sort (unpriced candidates sink to the bottom)
        ▼
Plan{Launches[] *Launch}       // 5. failover order + total price / total time
```

Key point: **candidate collection and price fetching are shared and package-private**; extenders don't need to rewrite them—they only need to provide "how to score a candidate" (`Metric`), or "how the whole sort works" (custom `Optimizer`).

### Two extension levels

| Level | Extension point | Degree of involvement | Use cases |
|------|--------|---------|------|
| Composable | Implement `Metric` + `RegisterMetric`/`NewStrategy` | Only scoring | Most scenarios (new objectives like latency/carbon/budget) |
| Full | Implement `Optimizer` + `Register` | Takes over the whole Optimize | Needs a custom candidate pipeline/search logic (e.g. ILP, genetic algorithms) |

### Metric vs Optimizer

The two are often confused; fundamentally it's the difference between "a scoring dimension" and "the whole decision":

| | `Metric` | `Optimizer` |
|---|---|---|
| Question answered | **How to score a candidate** (one dimension) | **How to produce a Plan** (the whole decision) |
| Granularity | One score per candidate | Candidate set → sort → truncate → Plan |
| Method signature | `Rank(c *Candidate, useSpot) float64` | `Optimize(ctx, *Request) (*Plan, error)` |
| Perspective | Local (single candidate) | Global (all candidates + metadata + search strategy) |
| Composable? | Composable into strategies (`cost,time` lexicographic) | Registered as a named optimizer (`--optimizer <name>`) |
| Touches metadata? | No (only reads data already attached to `Candidate`) | Yes (can read `Meta`/metadata, control collection and price fetching) |
| Typical extensions | latency / carbon / budget / delay | ILP / genetic algorithms / constrained search |

**When to use a Metric, when to use an Optimizer?**

- If your extension is essentially "**add a new consideration/scoring dimension**" (e.g. latency, carbon, budget ceiling) → **implement a `Metric`**. Candidate collection, price fetching, unpriced-sinking, and lexicographic sort are all handled by `lexicographicOptimizer`; you only need `Rank` to return a number, zero boilerplate.
- If your extension is essentially "**swap in a different way of deciding**" (something that can't be expressed as scoring across several objectives, e.g. combinatorial optimization, constrained search, genetic algorithms) → **implement an `Optimizer`**, taking over the entire `Optimize`, but you must handle candidate collection/price fetching yourself (or post-process on top of `Optimize`'s result).

> Simple mnemonic: **A Metric is "one score", an Optimizer is "a whole decision"**. Want to add a dimension → use a Metric; want to swap the algorithm → use an Optimizer.

### After scoring: how are scores actually used?

The score returned by `Metric.Rank` **does not directly become the final result**—it is only a key that feeds into the sort. Inside `lexicographicOptimizer`:

1. **Compute one score for each candidate on each objective**, producing a "score vector":
   ```go
   // Each candidate maps to a []float64, length = number of objectives
   scores[i][j] = metrics[j].Rank(cand[i], useSpot)
   ```
2. **Lexicographic sort**: sort by the 1st objective's (primary) score ascending; candidates with equal scores are then compared by the 2nd objective's score ascending… and so on. `sort.SliceStable` keeps the original stable order for ties. **Unpriced candidates (`Priced()==false`) always sink to the bottom** and don't participate in ranking.
3. **Truncate**: after sorting, keep only `Options.MaxCandidates` candidates (default 10).
4. **Assemble the Plan**: each candidate becomes a `Launch`, `Order` is its rank (1 = top choice), output together with total price/total time as the failover order.

**The score itself never enters the Plan**—`Launch` stores raw data like `OnDemandCost`/`SpotCost`/`EstimatedTime` for display and later launch; your `Rank` return value only matters during the sort step.

**Example: `--optimizer cost,latency` (strategy: cost first, then latency when costs are equal)**

Three candidates scored as follows (assuming real prices are already attached):

| Candidate | cost score ($/hr) | latency score (ms) | Sort result |
|------|----------------|------------------|----------|
| `a.g6.large` | 0.049 | 180 | 1 (smallest cost) |
| `b.m5.large` | 0.096 | 30 | 3 (2nd-smallest cost, latency irrelevant) |
| `c.c7.large` | 0.049 | 55 | 2 (same cost as a → compare latency: 55 > 30) |

- Primary objective cost ascending: `a` (0.049), `c` (0.049), `b` (0.096).
- `a` and `c` have the same cost (0.049) → fall to the second objective latency: `a` (30ms) < `c` (55ms) → `a` ranks ahead.
- Final order `a → c → b`; `a` is the primary failover.

**Now swap to `--optimizer latency,cost`** (primary becomes latency):

| Candidate | cost score ($/hr) | latency score (ms) | Sort result |
|------|----------------|------------------|----------|
| `a.g6.large` | 0.049 | 180 | 3 (largest latency) |
| `b.m5.large` | 0.096 | 30 | 1 (smallest latency) |
| `c.c7.large` | 0.049 | 55 | 2 (2nd-smallest latency) |

Same candidates, same scores, only **the priority order is different**—and the top choice flips from cheapest to fastest.

**Contrast: with an Optimizer (full extension) the scores are entirely under your control**

If you implement an `Optimizer`, `Optimize` returns a `*Plan`—there is no fixed "score → sort" flow anymore; you can use the scores any way you like (weighted sum, Pareto front, constraint filtering, etc.). For example, a budget optimizer could first filter out candidates over budget by `CostPerHour`, then re-sort the remaining candidates by `EstimatedTime` or `VCPUs`, or even do numeric computation directly on `c.OnDemand`—not just use it as a comparison key.

---

## 2. Core types

### 2.1 Candidate

`Candidate` is the smallest unit of scoring/sorting: a cloud instance type + real-time price + estimated runtime. **The price is not on the instance spec, it's on the Candidate** (prices are volatile and must be fetched in real time).

```go
type Candidate struct {
    *catalog.Instance                 // Cloud/Region/InstanceType/VCPUs/MemoryGiB/Accelerators/MaxDiskGiB
    OnDemand     float64              // on-demand price (0 = missing)
    Spot         float64              // spot price (0 = missing)
    EstimatedTime float64             // estimated runtime (hours), used by the time objective
}

func (c *Candidate) CostPerHour(useSpot bool) float64  // hourly cost for the given spot preference (falls back to estimation when price missing)
func (c *Candidate) Priced() bool                       // whether it has real-time prices (unpriced candidates always sink to the bottom)
```

### 2.2 Request / Options

```go
type Request struct {
    Resources *task.Resources   // task resource requirements (cpus/memory/disk/accelerators/cloud/region...)
    Options   *Options          // search space control
}

type Options struct {
    NumNodes      int    // number of nodes, default 1
    UseSpot       bool   // prefer spot
    Cloud         string // cloud filter (comma-separated)
    Region        string // region filter
    Zone          string // zone filter
    MaxCandidates int    // max candidates to return, default 10
}
```

### 2.3 Plan / Launch

```go
type Plan struct {
    Launches            []*Launch  // in failover order
    TotalCostPerHour    float64    // hourly cost of the top choice
    TotalEstimatedTime  float64    // estimated runtime of the top choice (only meaningful for time/strategies)
}

type Launch struct {
    Cloud/Region/Zone/InstanceType/NumNodes/Accelerators/VCPUs/MemoryGiB
    OnDemandCost/SpotCost/UseSpot
    EstimatedTime float64          // estimated runtime (hours)
    Order         int              // rank (1 = top choice)
}
func (l *Launch) CostPerHour() float64
```

### 2.4 Metadata access: Meta & defaultMeta

`Meta` is the cloud metadata accessor (cloud list/regions/instance specs/prices). All optimizers uniformly read the **globally shared** `defaultMeta` (a `metacache.Cache` with TTL caching):

```go
type Meta interface {
    Clouds(ctx) ([]string, error)
    Regions(ctx, cloud) ([]string, error)
    Instances(ctx, cloud, region) ([]*catalog.Instance, error)
    Prices(ctx, cloud, region, types) (map[string]catalog.Price, error)
    PricesForced(ctx, cloud, region, types) (map[string]catalog.Price, error)
}

// Package-private, not directly referenceable; use it through the two entry points below:
func SetDefaultMeta(m Meta)                                   // injection for testing/extensions
func PricesForced(ctx, cloud, region, types) (map[string]catalog.Price, error)  // force-refresh prices before launch
```

`Request` carries no `Meta` field—metadata is globally unique. What this means for extenders: inside a custom `Metric`/`Optimizer` you don't need to and shouldn't pass `Meta` around; read directly from the package-level `Meta` interface (if a custom `Optimizer` needs to read metadata, it can access it via the `Meta` injected through `SetDefaultMeta`; but **reading the data already attached to `Candidate` is recommended**).

---

## 3. Using the built-in optimizer/policies (zero code)

### 3.1 Specifying an optimizer

```bash
gpi optimize train.yaml --optimizer cost    # default, ascending by $/hr
gpi optimize train.yaml --optimizer time    # ascending by estimated runtime (prints EST TIME column)
```

### 3.2 Specifying a strategy (multi-metric lexicographic)

```bash
gpi optimize train.yaml --optimizer cost,time   # cost first, then time when prices are equal
gpi optimize train.yaml --optimizer time,cost   # time first, then cost when times are equal
```

Priorities can be arbitrarily combined/repeated (e.g. `cost,cost,time`). Same field in the API request body:

```json
{ "task": "...", "cloud": "aliyun", "dry_run": true, "optimizer": "cost,time" }
```

### 3.3 Convenience entry points (Go callers)

```go
plan, err := optimizer.OptimizeByCost(ts.Resources, opts)     // explicit optimization by cost (cost objective)
plan, err = optimizer.Optimize(ts.Resources, opts)            // alias: defaults to cost
// or
opt, _ := optimizer.Resolve("cost,time")                      // resolve by name (including strategies)
plan, err = opt.Optimize(ctx, &optimizer.Request{Resources: ts.Resources, Options: opts})
```

---

## 4. Composable extension: implement a Metric (recommended)

This is the most common extension approach: **you only provide "how to score a candidate"**—candidate collection, price fetching, sorting, and unpriced-sinking are all handled by `lexicographicOptimizer`.

### 4.1 The Metric interface

```go
type Metric interface {
    Name() string                                // unique name, used in strategy strings (e.g. "latency")
    Rank(c *Candidate, useSpot bool) float64     // scoring; smaller values rank higher
}
```

### 4.2 Example: a `latency` metric

Assume the extender has their own latency metadata (e.g. per-region probe data). Full steps:

```go
package myoptimizer

import (
    "github.com/acmestack/gpi/internal/optimizer"
)

// latencyTable: the extender's own metadata (e.g. RTT in ms per region).
// Note: Metric.Rank can only see a Candidate; here the extender maps the candidate's
// region to their own latency table.
var latencyTable = map[string]float64{
    "cn-hangzhou": 30,
    "cn-beijing":  55,
    "us-east-1":   180,
}

type latencyMetric struct{}

func (latencyMetric) Name() string { return "latency" }

func (latencyMetric) Rank(c *optimizer.Candidate, _ bool) float64 {
    if ms, ok := latencyTable[c.Region]; ok {
        return ms
    }
    return 1e9 // unknown region treated as extremely slow, ranks last
}
```

### 4.3 Register & compose

```go
// Register as a named objective: afterwards --optimizer cost,latency / ParseStrategy("cost,latency") work
optimizer.RegisterMetric("latency", latencyMetric{})

// or compose programmatically (registration is optional)
strat := optimizer.NewStrategy(costMetric{}, latencyMetric{})
```

> Note: `RegisterMetric` writes to a global registry. In production code call it once in `init()` or at startup; in tests remember to clean up or accept the global side effects.

### 4.4 Complete runnable example

```go
opt, _ := optimizer.ParseStrategy("cost,latency")   // cost first, latency as tiebreaker
plan, err := opt.Optimize(ctx, &optimizer.Request{
    Resources: ts.Resources,
    Options:   &optimizer.Options{MaxCandidates: 5, Cloud: "aliyun"},
})
for _, l := range plan.Launches {
    fmt.Printf("#%d %s/%s %s $%.3f/h\n", l.Order, l.Cloud, l.Region, l.InstanceType, l.CostPerHour())
}
```

### 4.5 Key points

- `Rank` is called once per candidate—**keep it cheap** (no network requests here; pre-query/cache your extension metadata).
- Smaller return values rank higher; equal scores fall through to the next objective (lexicographic).
- Unpriced candidates sink automatically—no need to handle that in `Rank`.
- If the objective depends on the spot/on-demand preference, use the second parameter `useSpot` (that's what `cost` does).

---

## 5. Full extension: implement an Optimizer

When the sorting logic can't be expressed as "a lexicographic order of several objectives" (e.g. ILP, constrained search, genetic algorithms), implement an `Optimizer` and take over the entire `Optimize`.

### 5.1 The Optimizer interface

```go
type Optimizer interface {
    Name() string                                    // registration name (for --optimizer)
    Optimize(ctx context.Context, req *Request) (*Plan, error)
}
```

### 5.2 Notes

- `lexicographicOptimizer` is the reference implementation (`lexicographic.go`); follow its structure: parse `Request`/default `Options` → collect candidates → fetch prices → sort → assemble `Plan`.
- Candidate collection and price fetching are currently **package-private** (`collectCandidates`/`attachPrices`). Full extenders can't reuse them—you need to implement matching/price fetching yourself, or **only rely on the data already present on `Candidate`** (e.g. re-sorting the candidates of an existing Plan). This is a deliberate trade-off: full extensions target "swapping the whole algorithm", composable extensions target "adding a dimension".
- Metadata reading: the package-level `Meta` injected via `SetDefaultMeta` is the only data source; if a custom Optimizer wants to read specs/prices, go through the `Meta` interface methods (but prefer the data already attached to `Candidate`).

### 5.3 Example: a budget-constrained optimizer

Goal: pick machines with as many vCPUs as possible, subject to "per-instance price ≤ 2 $/h" (cost-constrained).

```go
package myoptimizer

import (
    "context"
    "errors"
    "sort"

    "github.com/acmestack/gpi/internal/optimizer"
    "github.com/acmestack/gpi/internal/task"
)

type budgetOptimizer struct{ maxPrice float64 }

func (budgetOptimizer) Name() string { return "budget" }

func (o budgetOptimizer) Optimize(ctx context.Context, req *optimizer.Request) (*optimizer.Plan, error) {
    if req == nil || req.Resources == nil {
        return nil, errors.New("request required")
    }
    opts := req.Options
    if opts == nil {
        opts = optimizer.DefaultOptions()
    }
    // Use the convenience entry point to get candidates (run the default cost strategy,
    // and use its raw candidate set).
    // Note: this leverages optimizer.Optimize to fetch candidates and prices, then
    // does budget filtering and re-sorting.
    base, err := optimizer.Optimize(req.Resources, opts)
    if err != nil {
        return nil, err
    }
    var kept []*optimizer.Launch
    for _, l := range base.Launches {
        if l.CostPerHour() <= o.maxPrice {
            kept = append(kept, l)
        }
    }
    // Within budget, sort by VCPUs descending (compute-first)
    sort.SliceStable(kept, func(i, j int) bool { return kept[i].VCPUs > kept[j].VCPUs })
    for i, l := range kept {
        l.Order = i + 1
    }
    if len(kept) == 0 {
        return nil, errors.New("no candidate within budget")
    }
    return &optimizer.Plan{Launches: kept, TotalCostPerHour: kept[0].CostPerHour()}, nil
}

func init() {
    optimizer.Register(budgetOptimizer{maxPrice: 2.0})   // --optimizer budget
}
```

> The example above reuses `optimizer.Optimize` (the default cost candidate set) as its data source, then re-sorts—this is the least-effort path for full extensions. If you need the raw `Candidate`s (with price fields), implement the same collection logic in-package, or wait for the pipeline to become an exported API (see section 7 Roadmap).

### 5.4 Register & use

```go
// After registration: --optimizer budget
optimizer.Register(budgetOptimizer{maxPrice: 2.0})

// Resolve
opt, _ := optimizer.Resolve("budget")
```

---

## 6. Testing

### 6.1 Injecting fake metadata

Tests shouldn't hit the network. Inject an in-memory Meta with `SetDefaultMeta`, and restore with `defer`:

```go
import (
    "context"
    "testing"

    "github.com/acmestack/gpi/internal/cloud/catalog"
    "github.com/acmestack/gpi/internal/optimizer"
)

type fakeMeta struct{}

func (fakeMeta) Clouds(context.Context) ([]string, error) { return []string{"aliyun"}, nil }
func (fakeMeta) Regions(context.Context, string) ([]string, error) { return []string{"cn-hangzhou"}, nil }
func (fakeMeta) Instances(context.Context, string, string) ([]*catalog.Instance, error) {
    return []*catalog.Instance{
        {Cloud: "aliyun", Region: "cn-hangzhou", InstanceType: "g6.large", VCPUs: 2, MemoryGiB: 8},
    }, nil
}
func (fakeMeta) Prices(context.Context, string, string, []string) (map[string]catalog.Price, error) {
    return map[string]catalog.Price{"g6.large": {OnDemand: 0.049}}, nil
}
func (fakeMeta) PricesForced(context.Context, string, string, []string) (map[string]catalog.Price, error) {
    return nil, nil
}

func TestMyMetric(t *testing.T) {
    optimizer.SetDefaultMeta(fakeMeta{})
    // ... run your optimizer ...
}
```

The recommended way to restore the global Meta: initialize/restore in `TestMain`, or re-`SetDefaultMeta` a clean instance in each test's `defer`. There is no exported "read the current Meta" entry point, so the convention is:

```go
func TestMain(m *testing.M) {
    os.Exit(m.Run()) // if restoration is needed, SetDefaultMeta(new instance) in each test's defer
}
```

For most tests, simply `SetDefaultMeta(fakeMeta{})` suffices—tests in the same binary run sequentially, and later tests set their own Meta.

### 6.2 Reference implementation

`internal/optimizer/optimizer_ext_test.go` (`package optimizer_test`, external package) demonstrates:
- implementing a custom `Metric` (`latencyMetric`);
- composing a strategy with `RegisterMetric` + `ParseStrategy("latency")`;
- running `Optimize` after injecting fake metadata via `SetDefaultMeta`, verifying the sort.

This is the golden reference from the "external user's perspective".

---

## 7. Roadmap

- Expose the candidate pipeline (`collectCandidates`/`attachPrices`) as an exported API (e.g. `RankCandidates(ctx, meta, rs, opts, metrics)`), so full extenders can reuse matching/price fetching instead of rewriting it.
- Add an exported "restore default Meta" convenience (e.g. `NewDefaultMeta()`) to make it easier for tests to reset global state.
- More built-in objectives: carbon (`carbon`), budget (`budget`), availability/spot-price ceilings, etc., all implemented as `Metric`s.

---

## 8. Cheat sheet

| I need to… | Use this |
|-----------|--------|
| Swap the optimizer/strategy without writing code | `--optimizer cost` / `time` / `cost,time` |
| Add a scoring dimension to candidates | Implement `Metric` → `RegisterMetric` |
| Compose a strategy from metric instances (in code) | `NewStrategy(latencyMetric{}, costMetric{})` |
| Resolve by strategy name | `ParseStrategy("cost,latency")` / `Resolve("...")` |
| Take over the whole optimization algorithm | Implement `Optimizer` → `Register` |
| Inject fake metadata in tests | `SetDefaultMeta(fakeMeta{})` |
| Force-refresh prices before launch | `PricesForced(ctx, cloud, region, types)` |
| Read raw candidate fields | `Candidate` (specs, `OnDemand`/`Spot`/`EstimatedTime`, `CostPerHour`/`Priced`) |
| List built-in metric names | `MetricNames()` |
| List built-in optimizer names | `Names()` |

## Version history

- **v9 (2026-08-13)**: Split `lexicographicOptimizer` out into `lexicographic.go` (algorithm implementation); `strategy.go` now only holds strategy construction (`NewStrategy`/`ParseStrategy`) + built-in registration.
- **v8 (2026-08-13)**: Naming changes—`Objective` → `Metric` (scoring metric), `costObjective`/`timeObjective` → `costMetric`/`timeMetric`, `RegisterObjective` → `RegisterMetric`, `objective.go` → `metric.go`; `strategyOptimizer` → `lexicographicOptimizer` (lexicographic multi-metric sort implementation), public `NewStrategy`/`ParseStrategy` preserved.
- **v7 (2026-08-13)**: Added the "After scoring: how are scores actually used" section—score vector → lexicographic sort → truncation → Plan mechanics, including the concrete `cost,latency` and `latency,cost` sort demos, plus the free use of scores in the Optimizer scenario.
- **v6 (2026-08-13)**: Fixed the file-responsibility table to match the current optimizer package structure—`optimizer.go` (interface + Get/Resolve), `plan.go`, `request.go`, `meta.go` (including `newCacheMeta`), `registry.go`, `match.go`; removed the merged-away `meta_adapter.go`.
- **v5 (2026-08-09)**: Added the "Objective vs Optimizer" section (scoring dimension vs whole decision, with selection guidance).
- **v4 (2026-08-09)**: Removed the in-doc "change rules" line (that rule is consolidated in the project root `AGENTS.md`); completed the version-change log section.
- **v3 (2026-08-09)**: Moved `timeObjective` into `time.go` and added explicit `OptimizeByTime`/`OptimizeByTimeContext` (symmetric with cost); moved `NewStrategy`/`ParseStrategy` into `strategy.go` (strategy construction ownership). `objective.go` now only holds the `Objective` interface and the objective registry. Updated the file-responsibility table.
- **v2 (2026-08-09)**: Moved `costObjective` into `cost.go` as the owner of the cost objective; added explicit `OptimizeByCost`/`OptimizeByCostContext`, `Optimize`/`OptimizeWithContext` became aliases. Updated the file-responsibility table and convenience entry-point examples.
- **v1 (2026-08-09)**: Created this guide (architecture overview, core types, composable/full extensions, testing, roadmap, cheat sheet).
