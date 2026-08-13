# Optimizer 扩展指南（Extending the Placement Optimizer）

- **文档版本**：v6（2026-08-13）
- **适用项目**：Gpi（`github.com/acmestack/gpi`）
- **v6（2026-08-13）**：修正文件职责表以匹配当前 optimizer 包结构——`optimizer.go`（接口+Get/Resolve）、`plan.go`、`request.go`、`meta.go`（含 `newCacheMeta`）、`registry.go`、`match.go`；移除已合并的 `meta_adapter.go`。
- **v5（2026-08-09）**：新增"Objective 与 Optimizer 的差异"小节（打分维度 vs 决策整体，含选型指引）。
- **v4（2026-08-09）**：移除文档内"变更规则"行（该规则统一到项目根 `AGENTS.md`）；补齐版本变更记录区。
- **v3（2026-08-09）**：`timeObjective` 移入 `time.go` 并新增显式 `OptimizeByTime`/`OptimizeByTimeContext`（与 cost 对称）；`NewStrategy`/`ParseStrategy` 移到 `strategy.go`（策略构造归属）。`objective.go` 只剩 `Objective` 接口与目标注册表。更新文件职责表。
- **v2（2026-08-09）**：`costObjective` 移到 `cost.go` 成为 cost 目标的归属；新增显式 `OptimizeByCost`/`OptimizeByCostContext`，`Optimize`/`OptimizeWithContext` 变为别名。更新文件职责表与便捷入口示例。
- **v1（2026-08-09）**：创建本指南（架构总览、核心类型、组合式/完全式扩展、测试、演进方向、速查表）。

## 定位

本指南完整讲解 gpi 的 placement optimizer（`internal/optimizer`）如何扩展。它回答三类问题：

1. **怎么用内置优化器/策略**（不写代码，改 CLI/API 参数）。
2. **怎么加一个新的优化目标**（实现 `Objective`，如 latency / carbon / budget），融入 `cost,time` 这类策略。
3. **怎么写一个完全自定义的 Optimizer**（替换整个排序逻辑）。

读完本指南应能独立实现并注册一个自定义优化器，并为其编写测试。

---

## 1. 架构总览

optimizer 包按职责分文件：

| 文件 | 内容 |
|------|------|
| `optimizer.go` | `Optimizer` 接口 + `Get`/`Resolve` 解析入口（按名字/策略解析） |
| `plan.go` | `Launch`/`Plan`（placement 决策 + 排序输出） |
| `request.go` | `Options`/`Request`（搜索空间 + 输入） |
| `meta.go` | `Meta` 接口 + 共享 `defaultMeta`（含 `newCacheMeta` 适配） + `SetDefaultMeta`/`PricesForced` |
| `registry.go` | 命名优化器注册表（`Register`/`Names`/`Default`/`DefaultName`） |
| `candidate.go` | `Candidate`（可排序的候选单元）+ 内部候选管道（`collectCandidates`/`attachPrices`，未导出） |
| `objective.go` | `Objective` 接口 + `RegisterObjective`/`ObjectiveNames`（目标注册表） |
| `strategy.go` | `strategyOptimizer`：多目标字典序排序 + `NewStrategy`/`ParseStrategy` + 内置 `cost`/`time` 注册 |
| `cost.go` | **cost 目标的归属**：`costObjective` 定义 + 显式入口 `OptimizeByCost`/`OptimizeByCostContext` + 别名 `Optimize`/`OptimizeWithContext` |
| `time.go` | **time 目标的归属**：`timeObjective` 定义 + 显式入口 `OptimizeByTime`/`OptimizeByTimeContext` + `estimateRuntime` 启发式 |
| `match.go` | `matchesResources`：候选资源匹配过滤 |

### 一次优化发生了什么

```
Request{Resources, Options}
        │
        ▼
strategyOptimizer.Optimize ──► collectCandidates()   // 1. 匹配资源需求，收集可行机型
        │                      attachPrices()         // 2. 并发拉实时价（含预算截断）
        │                      (time目标→预计算 estTime)
        │                      o.Rank(c, useSpot)     // 3. 每个目标打分
        │                      sort lexicographic     // 4. 字典序排序（无价候选沉底）
        ▼
Plan{Launches[] *Launch}       // 5. failover 顺序 + 总价/总时长
```

关键点：**候选收集与拉价是共享的、包内私有的**；扩展者不需要重写它们，只需要提供"怎么给一个候选打分"（`Objective`），或"整个排序怎么做"（自定义 `Optimizer`）。

### 两种扩展层次

| 层次 | 扩展点 | 介入程度 | 适用 |
|------|--------|---------|------|
| 组合式 | 实现 `Objective` + `RegisterObjective`/`NewStrategy` | 只需打分 | 大多数场景（latency/carbon/budget 等新目标） |
| 完全式 | 实现 `Optimizer` + `Register` | 接管整个 Optimize | 需要自定义候选管道/搜索逻辑（如 ILP、遗传算法） |

### Objective 与 Optimizer 的差异

两者经常被混淆，本质是"打分的维度"与"决策的整体"的区别：

| | `Objective` | `Optimizer` |
|---|---|---|
| 回答的问题 | **怎么给一个候选打分**（一个维度） | **怎么排出一份 Plan**（整个决策） |
| 粒度 | 一个候选一行打分值 | 候选集 → 排序 → 截断 → Plan |
| 方法签名 | `Rank(c *Candidate, useSpot) float64` | `Optimize(ctx, *Request) (*Plan, error)` |
| 视角 | 局部（单候选） | 全局（所有候选 + 元数据 + 搜索策略） |
| 可否组合 | 可组合成策略（`cost,time` 字典序） | 注册成命名优化器（`--optimizer <name>`） |
| 是否接触元数据 | 否（只读 `Candidate` 上已附好的数据） | 是（可读 `Meta`/元数据，控制收集与拉价） |
| 典型扩展 | latency / carbon / budget / 时延 | ILP / 遗传算法 / 带约束搜索 |

**什么时候用 Objective，什么时候用 Optimizer？**

- 你的扩展本质是"**新增一个考虑因素/打分维度**"（如时延、碳排、预算上限）→ **实现 `Objective`**。候选收集、拉价、无价沉底、字典序排序全部由 `strategyOptimizer` 处理，你只需 `Rank` 返回一个数，零样板代码。
- 你的扩展本质是"**换一种决策方式**"（无法用若干目标打分表达，例如组合优化、带约束搜索、遗传算法）→ **实现 `Optimizer`**，接管整个 `Optimize`，但需要自己处理候选收集/拉价（或基于 `Optimize` 的结果二次处理）。

> 简单记忆：**Objective 是"一个打分"，Optimizer 是"一整套决策"**。想加维度用 Objective，想换算法用 Optimizer。

---

## 2. 核心类型

### 2.1 Candidate

`Candidate` 是打分/排序的最小单元：一个云机型 + 实时价格 + 预估时长。**价格不在实例规格上，而在 Candidate 上**（价格易变，必须实时）。

```go
type Candidate struct {
    *catalog.Instance                 // Cloud/Region/InstanceType/VCPUs/MemoryGiB/Accelerators/MaxDiskGiB
    OnDemand     float64              // 按量价（0 = 缺失）
    Spot         float64              // spot 价（0 = 缺失）
    EstimatedTime float64             // 预估运行时长（小时），time 目标用
}

func (c *Candidate) CostPerHour(useSpot bool) float64  // 给定 spot 偏好的小时成本（缺价回退估算）
func (c *Candidate) Priced() bool                       // 是否有实时价（无价候选永远沉底）
```

### 2.2 Request / Options

```go
type Request struct {
    Resources *task.Resources   // 任务资源需求（cpus/memory/disk/accelerators/cloud/region...）
    Options   *Options          // 搜索空间控制
}

type Options struct {
    NumNodes      int    // 节点数，默认 1
    UseSpot       bool   // 偏好 spot
    Cloud         string // 云过滤（逗号分隔）
    Region        string // region 过滤
    Zone          string // zone 过滤
    MaxCandidates int    // 返回候选上限，默认 10
}
```

### 2.3 Plan / Launch

```go
type Plan struct {
    Launches            []*Launch  // 按 failover 顺序排列
    TotalCostPerHour    float64    // 首选的小时成本
    TotalEstimatedTime  float64    // 首选的预估时长（仅 time/策略有意义）
}

type Launch struct {
    Cloud/Region/Zone/InstanceType/NumNodes/Accelerators/VCPUs/MemoryGiB
    OnDemandCost/SpotCost/UseSpot
    EstimatedTime float64          // 预估时长（小时）
    Order         int              // 排名（1 = 首选）
}
func (l *Launch) CostPerHour() float64
```

### 2.4 元数据访问：Meta 与 defaultMeta

`Meta` 是云元数据访问器（云列表/region/机型规格/价格）。所有优化器统一读**全局共享**的 `defaultMeta`（一个 `metacache.Cache`，带 TTL 缓存）：

```go
type Meta interface {
    Clouds(ctx) ([]string, error)
    Regions(ctx, cloud) ([]string, error)
    Instances(ctx, cloud, region) ([]*catalog.Instance, error)
    Prices(ctx, cloud, region, types) (map[string]catalog.Price, error)
    PricesForced(ctx, cloud, region, types) (map[string]catalog.Price, error)
}

// 包内私有，不可直接引用；通过下面两个入口使用：
func SetDefaultMeta(m Meta)                                   // 测试/扩展注入
func PricesForced(ctx, cloud, region, types) (map[string]catalog.Price, error)  // launch 前强刷价
```

`Request` 不含 `Meta` 字段——元数据全局唯一。这对扩展者的含义：自定义 `Objective`/`Optimizer` 里不需要也不应该传 Meta，直接从包级 `Meta` 接口读（如果实现自定义 `Optimizer` 需要读取元数据，可通过 `SetDefaultMeta` 注入的 `Meta` 访问；但**推荐只读 `Candidate` 上已附好的数据**）。

---

## 3. 使用内置优化器/策略（零代码）

### 3.1 指定优化器

```bash
gpi optimize train.yaml --optimizer cost    # 默认，按 $/hr 升序
gpi optimize train.yaml --optimizer time    # 按预估运行时长升序（输出 EST TIME 列）
```

### 3.2 指定策略（多目标字典序）

```bash
gpi optimize train.yaml --optimizer cost,time   # 先成本，同价再看时长
gpi optimize train.yaml --optimizer time,cost   # 先时长，同时长再看成本
```

优先级可任意组合/重复（如 `cost,cost,time`）。API 请求体同字段：

```json
{ "task": "...", "cloud": "aliyun", "dry_run": true, "optimizer": "cost,time" }
```

### 3.3 便捷入口（Go 调用方）

```go
plan, err := optimizer.OptimizeByCost(ts.Resources, opts)     // 显式按成本优化（cost 目标）
plan, err = optimizer.Optimize(ts.Resources, opts)            // 别名：默认即 cost
// 或
opt, _ := optimizer.Resolve("cost,time")                      // 按名字解析（含策略）
plan, err = opt.Optimize(ctx, &optimizer.Request{Resources: ts.Resources, Options: opts})
```

---

## 4. 组合式扩展：实现一个 Objective（推荐）

这是最常用的扩展方式：**只需提供"怎么给候选打分"**，候选收集、拉价、排序、无价沉底全部由 `strategyOptimizer` 处理。

### 4.1 Objective 接口

```go
type Objective interface {
    Name() string                                // 唯一名，用于策略字符串（如 "latency"）
    Rank(c *Candidate, useSpot bool) float64     // 打分；值越小排名越靠前
}
```

### 4.2 示例：实现一个 `latency` 目标

假设扩展者有自己的时延元数据（例如按 region 的探测数据）。完整步骤：

```go
package myoptimizer

import (
    "github.com/acmestack/gpi/internal/optimizer"
)

// latencyTable：扩展者自己的元数据（例如每个 region 的 RTT 毫秒）。
// 说明：Objective.Rank 只能看到 Candidate；扩展者在此把候选的 region 映射到自己的时延表。
var latencyTable = map[string]float64{
    "cn-hangzhou": 30,
    "cn-beijing":  55,
    "us-east-1":   180,
}

type latencyObjective struct{}

func (latencyObjective) Name() string { return "latency" }

func (latencyObjective) Rank(c *optimizer.Candidate, _ bool) float64 {
    if ms, ok := latencyTable[c.Region]; ok {
        return ms
    }
    return 1e9 // 未知 region 视为极慢，排最后
}
```

### 4.3 注册并组合

```go
// 注册为命名目标：此后 --optimizer cost,latency / ParseStrategy("cost,latency") 可用
optimizer.RegisterObjective("latency", latencyObjective{})

// 或编程式组合（不注册也行）
strat := optimizer.NewStrategy(costObjective{}, latencyObjective{})
```

> 注意：`RegisterObjective` 写入全局注册表。生产代码在 `init()` 或启动时调用一次即可；测试里记得清理或接受全局副作用。

### 4.4 完整运行示例

```go
opt, _ := optimizer.ParseStrategy("cost,latency")   // cost 优先，时延平局
plan, err := opt.Optimize(ctx, &optimizer.Request{
    Resources: ts.Resources,
    Options:   &optimizer.Options{MaxCandidates: 5, Cloud: "aliyun"},
})
for _, l := range plan.Launches {
    fmt.Printf("#%d %s/%s %s $%.3f/h\n", l.Order, l.Cloud, l.Region, l.InstanceType, l.CostPerHour())
}
```

### 4.5 要点

- `Rank` 被每个候选调用一次，**保持廉价**（不要在这里做网络请求；扩展元数据应预先查询/缓存）。
- 返回值越小越靠前；相同分数时落到下一个目标（字典序）。
- 无价候选自动沉底，不需要在 `Rank` 里处理。
- 若目标依赖 spot/on-demand 偏好，用第二个参数 `useSpot`（`cost` 就是这么做的）。

---

## 5. 完全式扩展：实现一个 Optimizer

当排序逻辑无法用"若干目标的字典序"表达时（例如 ILP、带约束的搜索、遗传算法），实现 `Optimizer` 接管整个 `Optimize`。

### 5.1 Optimizer 接口

```go
type Optimizer interface {
    Name() string                                    // 注册名（用于 --optimizer）
    Optimize(ctx context.Context, req *Request) (*Plan, error)
}
```

### 5.2 注意事项

- `strategyOptimizer` 是参考实现（`strategy.go`）；照它的结构：解析 `Request`/默认 `Options` → 收集候选 → 拉价 → 排序 → 组装 `Plan`。
- 候选收集与拉价目前是**包内私有**（`collectCandidates`/`attachPrices`）。完全式扩展者无法复用它们——需要自己实现匹配/拉价，或**只依赖 `Candidate` 上已有的数据**（比如对某个现成 Plan 的候选做二次重排）。这是有意的取舍：完全式扩展面向"换整个算法"，组合式面向"加一个维度"。
- 元数据读取：包级 `SetDefaultMeta` 注入的 `Meta` 是唯一数据源；自定义 Optimizer 若要读规格/价格，直接通过 `Meta` 接口方法（但推荐优先用 `Candidate` 已附数据）。

### 5.3 示例：预算约束优化器

目标：在满足"单机价格 ≤ 2 $/h"的前提下，尽可能选 vCPU 多的机器（成本优先）。

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
    // 用便捷入口拿到候选（走默认 cost 策略，取其原始候选集）。
    // 说明：这里利用 optimizer.Optimize 获取候选并拉价，然后做预算过滤重排。
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
    // 在预算内按 vCPU 降序（算力优先）
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

> 上面示例复用了 `optimizer.Optimize`（默认 cost 候选集）作为数据来源，再重排——这是完全式扩展里最省事的路径。若需要原始 `Candidate`（含价格字段），可在包内实现同款收集逻辑，或后续把管道开放为导出 API（见第 7 节演进方向）。

### 5.4 注册与使用

```go
// 注册后：--optimizer budget
optimizer.Register(budgetOptimizer{maxPrice: 2.0})

// 解析
opt, _ := optimizer.Resolve("budget")
```

---

## 6. 测试

### 6.1 注入假元数据

测试不该打网络。用 `SetDefaultMeta` 注入内存 Meta，并 `defer` 恢复：

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

func TestMyObjective(t *testing.T) {
    optimizer.SetDefaultMeta(fakeMeta{})
    // ... 运行你的优化器 ...
}
```

恢复全局 Meta 的推荐做法：在 `TestMain` 里初始化/恢复，或在每个测试的 `defer` 中重新 `SetDefaultMeta` 回一个干净的实例。包内没有导出"读取当前 Meta"的入口，所以约定是：

```go
func TestMain(m *testing.M) {
    os.Exit(m.Run()) // 若需恢复，可在每个测试 defer 中 SetDefaultMeta(新实例)
}
```

对大多数测试而言，直接 `SetDefaultMeta(fakeMeta{})` 就够——同一测试二进制内顺序执行，后续测试自己会再设置自己的 Meta。

### 6.2 参考实现

`internal/optimizer/optimizer_ext_test.go`（`package optimizer_test`，外部包）演示了：
- 实现自定义 `Objective`（`latencyObjective`）；
- `RegisterObjective` + `ParseStrategy("latency")` 组合成策略；
- `SetDefaultMeta` 注入假元数据后跑 `Optimize`，验证排序。

这是"外部使用者视角"的黄金参考。

---

## 7. 演进方向

- 把候选管道（`collectCandidates`/`attachPrices`）开放为导出 API（如 `RankCandidates(ctx, meta, rs, opts, objectives)`），让完全式扩展者也能复用匹配/拉价，而不是重写。
- 增加"恢复默认 Meta"的导出便捷（如 `NewDefaultMeta()`），让测试更易还原全局状态。
- 更多内置目标：碳排（`carbon`）、预算（`budget`）、可用性/竞价格上限等，均按 `Objective` 实现。

---

## 8. 速查表

| 我需要…… | 用这个 |
|-----------|--------|
| 换优化器/策略，不写代码 | `--optimizer cost` / `time` / `cost,time` |
| 给候选加一个打分维度 | 实现 `Objective` → `RegisterObjective` |
| 用目标实例组合策略（代码里） | `NewStrategy(latencyObjective{}, costObjective{})` |
| 按策略名解析 | `ParseStrategy("cost,latency")` / `Resolve("...")` |
| 接管整个优化算法 | 实现 `Optimizer` → `Register` |
| 测试时注入假元数据 | `SetDefaultMeta(fakeMeta{})` |
| launch 前强刷价格 | `PricesForced(ctx, cloud, region, types)` |
| 读候选原始字段 | `Candidate`（规格、`OnDemand`/`Spot`/`EstimatedTime`、`CostPerHour`/`Priced`） |
| 看内置目标名 | `ObjectiveNames()` |
| 看内置优化器名 | `Names()` |
