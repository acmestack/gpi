# 接入新云指南（How to Add a New Cloud）

- **文档版本**：v12（2026-08-09）
- **适用项目**：Gpi（`github.com/acmestack/gpi`）
- **v12（2026-08-09）**：移除文档内"变更规则"行（规则统一到 `AGENTS.md`）；补齐版本变更记录区。
- **v11（2026-08-09）**：`--optimizer` 支持策略（`cost,time` 等）；内置优化器描述更新为 cost/time。
- **v10（2026-08-09）**：新增内置 `time` 优化器；优化器消费元数据说明补充。
- **v9（2026-08-09）**：元数据契约与 TTL 缓存运行时拆分定稿——契约在 `internal/cloud/catalog`，Cache 在 `internal/metacache`。
- **v8（2026-08-09）**：文档版本号规则统一到项目 `AGENTS.md`（docs 长期文档版本号记录在内容中）。
- **v7（2026-08-09）**：新云接入只需**一个 struct**——`Provider` 同时实现 `cloud.Provider` + `catalog.Source`，`cloud.Register` 类型断言自动注册元数据源；删除独立 `source` struct。
- **v6（2026-08-09）**：新增 Redis 状态后端与 Dockerfile 说明同步。
- **v5（2026-08-09）**：元数据全面动态化——实现 `catalog.Source`（规格/价格实时拉取），无静态数据。
- **v4（2026-08-08）**：聚合包生成挂 `make build` 前置。
- **v3（2026-08-08）**：聚合包 `internal/cloud/imports` 自动生成（`gen.go`）。
- **v2（2026-08-08）**：注册收敛为单一入口（`cloud.Register`/`RegisterFactory`/`catalog.Register`）。
- **v1（2026-08-08）**：创建本指南。

## 总览：一个云 = 一个包 + 一个 struct

新云在源码中只新增 **1 个 Go 包**（`internal/cloud/<cloud>/`），注册全部收敛在该包的 `provider.go` 单一 `init()` 里。**一个 `Provider` 结构体同时实现两套接口**：`cloud.Provider`（实例生命周期）+ `catalog.Source`（规格/价格元数据，`catalog` 在 `internal/cloud/catalog`，对应 SkyPilot `sky/catalogs`），`cloud.Register` 自动识别并注册元数据源。**没有静态规格/价格数据**——元数据全部实时拉取。不需要改动 optimizer / server / cli 任何核心逻辑。

| 接口 | 职责 | 方法（Provider 一个 struct 全实现） |
|------|------|----------|
| `cloud.Provider` | 建/查/删实例、VPC/SG/KeyPair/镜像 | `Name`/`Regions`/`RunInstances`/`DescribeInstances`/`Start`/`Stop`/`Terminate`/`GetPublicIP`/`DescribeZones`/`CreateKeyPair`/`DeleteKeyPair`/`CreateSecurityGroup`/`AuthorizeSecurityGroup`/`CreateVPC`/`CreateVSwitch`/`ListVSwitches`/`GetImage` |
| `catalog.Source` | 元数据：规格 + 价格，均实时拉取 | `Cloud`/`SpecsTTL`/`PriceTTL`/`FetchSpecs`/`FetchPrices`（`Regions` 复用 Provider 的） |

> `Cloud()` 与 `Name()` 都返回云名（方法名不同、语义相同）；`Regions(ctx)` 签名两组接口一致，实现一次即可。

## 步骤

### 1. 元数据方法（`metadata.go`，挂在 Provider 上）

在 Provider 上实现 `catalog.Source` 的方法（**无需独立 `source` struct**）：

```go
package foo

// metadataClient 返回元数据（规格/价格）拉取用的 client，有绑定时用
// Provider 凭据，否则走 env/磁盘默认。
func (p Provider) metadataClient(region string) (*Client, error) { ... }

// 下面这些方法让 Provider 同时满足 catalog.Source。
func (p Provider) Cloud() string { return "foo" }

func (p Provider) SpecsTTL() time.Duration { return 24 * time.Hour }  // 规格低频
func (p Provider) PriceTTL() time.Duration { return 10 * time.Minute } // 价格高频

func (p Provider) FetchSpecs(ctx context.Context, region string) ([]*catalog.Instance, error) {
	// c.DescribeInstanceTypes(...) 转为 []*catalog.Instance
	// 每个 Instance: Cloud/Region/InstanceType/VCPUs/MemoryGiB/MaxDiskGiB/Accelerators
	// （价格不在 Instance 上——价格由 FetchPrices 单独提供）
}

func (p Provider) FetchPrices(ctx context.Context, region string, types []string) (map[string]cloud.Price, error) {
	// 按量 + spot，返回 map[instanceType]cloud.Price
	// 建议并发查询（参考 aliyun/aws 的 priceWorkers），单个型号失败跳过不中断
}
```

### 2. Provider（`internal/cloud/<cloud>/`）

新建包目录，参考 `internal/cloud/aliyun/` 的既有实现：

- **`provider.go`**：实现 `cloud.Provider` 接口，并在**唯一 `init()`** 中完成注册——**只需 `cloud.Register(Provider{})` 一行**（内部用类型断言自动把满足 `catalog.Source` 的 provider 注册为元数据源）：

```go
package foo

import (
	"github.com/acmestack/gpi/internal/cloud"
)

func init() {
	cloud.Register(Provider{})
	cloud.RegisterFactory("foo", func(creds *cloud.Credentials) (cloud.Provider, error) {
		return NewProvider(&Credentials{
			AccessKeyID:     creds.AccessKeyID,
			AccessKeySecret: creds.SecretAccessKey,
			Region:          creds.Region,
		}), nil
	})
}
```

- **`client.go` / `sign.go`**：云 API 的请求签名与调用（aliyun 用 HMAC-SHA1，aws 用 SigV4，新云按各自协议实现）。
- **`pricing.go`**：云 API 的价格查询方法（`DescribeOnDemandPrice` / `DescribeSpotPriceHistory`），由 `Provider.FetchPrices` 调用。

### 3. 构建时自动生成聚合包（零手动操作）

新云**无需手改任何 import、无需单独命令**——聚合包在构建时自动生成：

- `Makefile` 的 `build` 目标先执行 `go generate ./internal/cloud/imports`，再编译 `gpi`/`gpilet`。
- 生成器（`internal/cloud/imports/gen.go`）扫描 `internal/cloud/` 下所有含 `cloud.Register(` 的子包，重写 `imports.go` 的空白导入列表。
- 因此**新云建好包后，直接 `make build` 即可自动带上**；CI 用 `make build`，同样自动生效。
- 需要单独触发生成时：`go generate ./internal/cloud/imports`。

> `gpi` 二进制与所有测试统一 `_ "github.com/acmestack/gpi/internal/cloud/imports"`，该包内容由构建自动维护，新云加入后其他文件零改动。

### 4. 验证

```bash
go build ./... && go vet ./... && go test ./...
# 跨平台
GOOS=linux GOARCH=amd64 go build ./cmd/gpi
GOOS=darwin GOARCH=arm64 go build ./cmd/gpi
# 真实测试（需要有该云凭据）
gpi optimize examples/train.yaml --cloud foo --region region-a
```

## 设计说明

- **为什么一个 struct 实现两套接口？** `cloud.Provider` 管"云实例怎么用"，`catalog.Source` 管"云元数据从哪来"，两者职责分离、接口独立；但实现者通常是同一个云客户端，所以一个 struct 全实现，注册时 `cloud.Register` 类型断言自动分流——**新云只写一个 struct + 一组方法**。
- **为什么规格/价格契约放在 cloud 而不是单独包？** 元数据来自云 API，实现者最了解如何拉取；契约（`Source` 接口 + `Instance`/`Price` 类型 + 注册表）与 Provider 同属"云"这个域，整体收在 `internal/cloud/catalog`（对应 SkyPilot `sky/catalogs`），新云只面对一个包。TTL 缓存运行时则独立在 `internal/metacache`（重，避免 cloud/catalog 臃肿）。
- **TTL 缓存如何工作？** `metacache.Cache` 按 (cloud, region) 缓存规格/价格，`SpecsTTL()`/`PriceTTL()` 决定各云刷新频率；拉取失败保留旧数据（stale-while-error），`PricesForced` 绕过 TTL 强制刷新（`gpi launch` 确认前用）。
- **为什么要有聚合包 `internal/cloud/imports`？** Go 中包必须被 import 其 `init()` 才会执行，无法免 import。聚合包把"所有云的空白导入"集中到一个文件，且由 `gen.go` 自动生成，挂在 `make build` 的构建前置——新云只需建包，**构建时自动带上，零手动操作**。
- **Optimizer 如何消费元数据？** 内置 `cost`/`time` 优化器及 `cost,time` 等策略经 `optimizer.Meta` 访问器读取规格并匹配任务资源，再对候选**并发拉价**（候选截断 + 缺价回退，见架构文档）。新云实现 `Provider.FetchSpecs/FetchPrices` 后自动被覆盖，无需改动 optimizer。
- **凭据来源**：provider 的 `NewClient` 负责从 env（`FOO_ACCESS_KEY_ID`/`FOO_SECRET` 或云默认配置文件）加载；任务级 `credentials:` 通过 `cloud.RegisterFactory` 注入。

## 演进方向

- GPU 内存变体匹配、多实例拆卡（A100:8 跨节点）。
- 更多可插拔优化器（时延、碳排、预算……）按 `optimizer.Optimizer` 接口扩展。
