# gpi v0.0.1

# gpi v0.0.1

**gpi** 是一个轻量、自托管、多云算力调度器（multi-cloud compute scheduler），用 Go 编写、对标 SkyPilot。用一个任务 YAML 声明资源与运行命令，自动完成比价选型、跨云置备、环境准备与任务执行。

**gpi** is a lightweight, self-hosted, multi-cloud compute scheduler written in Go, positioned against SkyPilot. Declare resources and run commands in a single task YAML; gpi handles price-based selection, cross-cloud provisioning, environment setup, and task execution automatically.

## 核心能力

## Core features

- **任务即代码**：一份 YAML 声明 `resources` / `setup` / `run`，可在任何支持的云上运行。

- **Task-as-code**: one YAML declares `resources` / `setup` / `run`, runnable on any supported cloud.

- **价格驱动调度**：跨全部已注册云做资源匹配与实时比价，输出 failover placement plan。

- **Price-driven scheduling**: resource matching and real-time price comparison across all registered clouds, producing a failover placement plan.

- **可插拔 Optimizer**：内置 `cost`（默认）与 `time` 两种指标，支持 `--optimizer cost,time` 等字典序多指标策略；通过 `Metric` 接口可扩展自定义指标（时延/碳排/预算等）。

- **Pluggable Optimizer**: built-in `cost` (default) and `time` metrics, plus lexicographic multi-metric strategies like `--optimizer cost,time`; extend via the `Metric` interface with custom metrics (latency / carbon / budget, etc.).

- **多云置备**：已支持 **aliyun + aws**（零官方 SDK，标准库实现签名），完整实例生命周期（VPC/SG/KeyPair/镜像/弹性 IP）。

- **Multi-cloud provisioning**: **aliyun + aws** supported (zero official SDKs, signature via the standard library), full instance lifecycle (VPC/SG/KeyPair/image/elastic IP).

- **节点 Agent（gpilet）**：跑在每个节点上，采集 CPU/内存/磁盘/GPU/Ray 状态（对标 skylet）。

- **Node Agent (gpilet)**: runs on every node, collecting CPU/memory/disk/GPU/Ray status (SkyPilot's skylet equivalent).

- **多节点 Ray 集群**：`num_nodes>1` 自动组成 head/worker Ray 集群。

- **Multi-node Ray clusters**: `num_nodes>1` automatically forms head/worker Ray clusters.

- **服务化部署**：多副本服务、健康检查（对标 SkyServe）。

- **Serving**: multi-replica services with health checks (SkyServe equivalent).

- **定时任务**：cron 调度 + 失败自动重试（对标 Sky Jobs）。

- **Scheduled jobs**: cron scheduling + automatic retry on failure (Sky Jobs equivalent).

- **多执行方式**：云 VM / 挂已有主机 SSH / 本地 Docker / 本机直跑。

- **Multiple execution modes**: cloud VM / SSH to existing hosts / local Docker / run directly on the local machine.

- **可插拔持久化**：file（默认）/ sqlite / mysql / redis。

- **Pluggable persistence**: file (default) / sqlite / mysql / redis.

- **REST API + 调度器**：`gpi server`，含响应格式定制、Request ID、key 风格、Bearer 认证、中间件、OpenAPI/Swagger（GitLab 内建渲染）。

- **REST API + scheduler**: `gpi server`, with response format customization, Request ID, key style, Bearer auth, middleware, OpenAPI/Swagger (rendered natively by GitLab).

## 元数据设计

## Metadata design

- **全动态、无静态数据**：规格与价格都从云 API 实时拉取，按每云 TTL 缓存（规格 24h、价格 10min，stale-while-error）。

- **Fully dynamic, no static data**: specs and prices are pulled live from cloud APIs, cached per-cloud with TTL (specs 24h, prices 10min, stale-while-error).

- 契约层 `internal/cloud/catalog`（对应 SkyPilot `sky/catalogs`），TTL 缓存运行时 `internal/metacache`。

- Contract layer `internal/cloud/catalog` (SkyPilot's `sky/catalogs` equivalent), TTL cache runtime `internal/metacache`.

- 新增云只需一个包 + 一个 struct（`Provider` 同时实现生命周期与元数据）。

- Adding a cloud requires only one package + one struct (`Provider` implements both lifecycle and metadata).

## 本次发布内容

## In this release

- 首个可运行版本，覆盖上述全部核心能力。

- First runnable release covering all core features above.

- CLI：`gpi launch / optimize / status / cluster / serve / jobs / server` 等。

- CLI: `gpi launch / optimize / status / cluster / serve / jobs / server`, etc.

- 构建：`make build`；Docker 镜像多阶段构建；K8s 部署清单 `deploy/k8s/`。

- Build: `make build`; multi-stage Docker image build; K8s deployment manifests in `deploy/k8s/`.

- 文档：架构设计、接入新云指南、Optimizer 扩展指南、增强对比清单。

- Docs: architecture design, adding-a-new-cloud guide, Optimizer extension guide, enhancement comparison list.

## 快速开始

## Quick start

```bash
make build
./gpi optimize my_task.yaml     # 只看 placement plan
./gpi launch my_task.yaml -y    # 真正部署
```

```bash
make build
./gpi optimize my_task.yaml     # see the placement plan only
./gpi launch my_task.yaml -y    # actually deploy
```

## 获取

## Getting it

- 源码：https://github.com/acmestack/gpi.git

- Source: https://github.com/acmestack/gpi.git

- 二进制：见本 Release 附件（linux / darwin × amd64 / arm64）

- Binaries: see the assets attached to this Release (linux / darwin × amd64 / arm64)

- 容器镜像：见仓库 Release 配置

- Container images: see the repository Release configuration

## 已知限制

## Known limitations

- 平台仅支持 Linux / macOS（无 Windows）。

- Linux / macOS only (no Windows).

- 阿里云开实例受账号余额限制（价格/元数据 API 不受影响）。

- Creating Alibaba Cloud instances is subject to account balance (price/metadata APIs are unaffected).

- 安全组默认放行 `0.0.0.0/0:22` 与 `1024-65535`，生产需收敛。

- Security groups default to opening `0.0.0.0/0:22` and `1024-65535`; tighten for production.