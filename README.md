# Gpi

```text
           _____   _____   _____ 
          / ____| |  __ \ |_   _|
         | |  __  | |__) |  | |  
         | | |_ | |  ___/   | |  
         | |__| | | |      _| |_ 
          \_____| |_|     |_____|

          Multi-Cloud Compute Scheduling
          =============================
          provision . optimize . serve . jobs
          Go  |  aliyun + aws  |  zero-SDK
```

<p align="center">
  <strong>Gpi 多云算力调度器（"机排/鸡排"）（multi-cloud compute scheduling）</strong>
  <br/>
  <em>Manage all your AI compute</em>
</p>

<p align="center">
  <a href="https://github.com/acmestack/gpi/actions/workflows/ci.yml">
    <img alt="CI" src="https://github.com/acmestack/gpi/actions/workflows/ci.yml/badge.svg">
  </a>
  <a href="https://github.com/acmestack/gpi/releases">
    <img alt="GitHub Release" src="https://img.shields.io/github/v/release/acmestack/gpi">
  </a>
  <a href="https://github.com/acmestack/gpi">
    <img alt="GitHub Repo" src="https://img.shields.io/github/stars/acmestack/gpi">
  </a>
</p>

<p align="center">
  <code>Go</code> 实现 · module <code>github.com/acmestack/gpi</code> · CLI <code>gpi</code>（谐音"机排/鸡排"）· 支持平台 <code>Linux</code> / <code>macOS</code>
</p>

## Overview

Gpi 是一个轻量、多云、可自托管的算力调度器。用任务 YAML 声明资源与运行命令，即可自动完成比价选型、跨云置备、环境准备与任务执行。

- **任务即代码**：一份 YAML 声明 resources / setup / run，可在任何支持的云上运行。
- **价格驱动调度**：跨全部已注册云做资源匹配与比价，输出最优 placement plan。
- **多云置备**：已支持 aliyun + aws（零官方 SDK，标准库实现签名）。
- **服务化部署**：多副本服务，秒级扩容，自带健康检查。
- **定时任务**：cron 调度 + 失败自动重试。
- **多执行方式**：云 VM、挂已有主机、本地 Docker、本机直跑。
- **可插拔**：状态存储（file / sqlite / mysql / redis）、API 响应格式均可按需配置。
- **可扩展**：[接入新云](docs/gpi-new-cloud.md) · [扩展 Optimizer](docs/gpi-optimizer-extension.md)

> 与 SkyPilot 的对比与增强差异见 [docs/gpi-enhancements-over-skypilot.md](docs/gpi-enhancements-over-skypilot.md)。
>
> 架构图见 [docs/gpi-architecture.md](docs/gpi-architecture.md#2-架构总览)。

## Getting started

构建两个二进制（`gpi` 控制面 + `gpilet` 节点 agent）：

```bash
make build
```

### Gpi in 1 minute

将下面的任务写入 `my_task.yaml`：

```yaml
name: mytrain

resources:
  accelerators: A100:1   # 1x NVIDIA A100 GPU
  cpus: 8+
  memory: 32+

num_nodes: 1             # 节点数（>1 自动组 Ray 集群）

# 任务开始前执行的准备命令
setup: |
  pip install -r requirements.txt

# 任务主命令
run: |
  python train.py --epochs 1
```

先只看调度方案（跨云比价，不真正部署）：

```bash
./gpi optimize my_task.yaml
```

确认后真正部署（需配置云凭据，见下文）：

```bash
./gpi launch my_task.yaml -y
```

Gpi 会自动完成：比价选最便宜可行的云/区 → 置备实例 → 上传 workdir → 执行 setup → 执行 run 并流式输出日志。

### 云凭据

```bash
# 阿里云
export ALIBABA_CLOUD_ACCESS_KEY_ID=... ALIBABA_CLOUD_ACCESS_KEY_SECRET=...
# 或 ~/.aliyun/config.json

# AWS
export AWS_ACCESS_KEY_ID=... AWS_SECRET_ACCESS_KEY=... AWS_REGION=...
# 或 ~/.aws/credentials
```

也可以在任务 YAML 的 `credentials:` 分块中为每次任务动态指定 AK/SK（见 [examples/yaml/with-credentials.yaml](examples/yaml/with-credentials.yaml)）。

## Commands

```
gpi launch TASK.yaml       调度+置备+运行
gpi optimize TASK.yaml     只看 placement plan（不置备）
                           可选 --optimizer cost|time|cost,time（优化器或策略）
gpi status                 集群列表
gpi cluster status|nodes C  集群拓扑 / 节点明细 / 实时健康
gpi cluster yaml|history|events C  集群 YAML 快照 / 启动历史 / 生命周期事件
gpi exec C -- CMD          head 节点上执行命令
gpi down/stop/start C      生命周期管理
gpi serve up S.yaml        服务化部署
gpi jobs submit T.yaml     注册定时任务
gpi server start           启动 REST API + 调度器
gpi server token create|list|revoke|rotate   API 令牌管理
```

## Features

### 多节点 Ray 集群

`num_nodes>1` 时自动组成 head/worker Ray 集群（head `ray start --head`，worker 加入）；setup 在全部节点并行执行，run 在 head 执行。任务内可用 `{{cluster.head_ip}}` 与 `{{cluster.num_workers}}` 注入分布式参数，见 [examples/yaml/distributed-train.yaml](examples/yaml/distributed-train.yaml)。

- 云实例标签：顶层 `tags:` 与 `resources.labels:` 合并写入云实例（冲突时 `tags:` 优先），并含内置 `gpi:cluster`/`gpi:cloud`。
- Ray node labels：`resources.labels:` 除写云实例外，还作为 `ray start --labels` 注入所有节点。
- `gpi cluster status C` 查看拓扑、labels 与 tags。

### 节点 Agent（gpilet）

`gpilet` 是跑在每个节点上的轻量 agent（纯 Go、零依赖）：

- `gpilet serve --dir /var/lib/gpilet --interval 10`：常驻采集 CPU/内存/磁盘/GPU/Ray 状态写入 `status.json`。
- `gpi cluster nodes C --health`：经 SSH 读取各节点实时健康。
- Launch 时若 `gpilet` 二进制在 `gpi` 旁（或 `GPI_CSLET` / `$GPI_HOME/bin/gpilet`）会自动上传并拉起。

### 执行后端

任务 YAML 顶层 `backend:` 选择执行方式，`cloud` 为默认（云 VM）：

```yaml
backend: existing    # 挂已有主机，SSH 执行 setup/run（需 ssh: 块）
ssh:
  host: 1.2.3.4
  user: root
  key: ~/.ssh/id_rsa

backend: docker      # 本地 Docker 容器执行（需 docker: 块）
docker:
  image: pytorch/pytorch:2.1.0
  gpus: 1

backend: local       # 本机直接执行 setup/run
```

示例：[examples/yaml/existing-cluster.yaml](examples/yaml/existing-cluster.yaml)、[examples/yaml/docker-task.yaml](examples/yaml/docker-task.yaml)、[examples/yaml/local-task.yaml](examples/yaml/local-task.yaml)。

### 持久化（可插拔后端）

默认本地文件，也可选 sqlite / mysql / redis，通过环境变量配置：

```bash
GPI_STATE_BACKEND=file                  # 默认：~/.gpi/state.json 等每类数据一个 JSON 文件
GPI_STATE_BACKEND=sqlite                # 单文件库，GPI_STATE_SQLITE 可指定路径
GPI_STATE_BACKEND=mysql GPI_STATE_MYSQL_DSN="user:pass@tcp(host:3306)/gpi"
GPI_STATE_BACKEND=redis GPI_STATE_REDIS_ADDR=localhost:6379

# 集群快照/历史/事件（对齐 SkyPilot 表结构）
# gpi cluster yaml|history|events CLUSTER
```

### 日志（Logging）

默认结构化日志输出到 **stdout**（zap，text 格式、info 级别）。可输出到文件，并按大小轮转、gzip 压缩、保留备份（基于 lumberjack）：

```bash
# CLI 标志（优先级最高）
gpi --log-level debug --log-file /var/log/gpi/gpi.log --log-format json server start

# 环境变量
GPI_LOG_LEVEL=info  GPI_LOG_FILE=/var/log/gpi/gpi.log  GPI_LOG_FORMAT=text

# 或 ~/.gpi/config.yaml / 项目 .gpi.yaml 的 logging 段
logging:
  level: info          # debug | info | warn | error
  format: text         # text | json
  file: /var/log/gpi/gpi.log   # 空 = stdout
  max_size: 100        # 超过该 MB 触发轮转（默认 100）
  max_backups: 5       # 保留的备份文件数（默认 5）
  max_age: 30          # 备份保留天数（默认 30）
  compress: true       # 轮转文件 gzip 压缩（默认 true）
```

优先级：CLI 标志 > 配置文件 > 环境变量 > 默认值。

### 服务化部署与定时任务

```bash
# 服务化部署（多副本）
./gpi serve up examples/yaml/llm-service.yaml -y
./gpi serve status

# 注册定时任务（cron + 失败重试）
./gpi jobs submit examples/yaml/nightly-benchmark.yaml --schedule "@daily" --retries 2
```

### REST API

```bash
./gpi server start --port 8080
```

- 响应格式可定制：默认 raw，可换 envelope（`{code,message,data}`）或团队自定义 Encoder（`--response-format`）。
- 全链路 Request ID：请求头透传或生成，回写 header + body（key 默认 `x-request-id`，`--request-id-header` 可改）。
- 响应 key 风格：默认 camel，可 snake / pascal（`--api-key-style`）。
- API 认证：`--require-auth` 开启 Bearer 令牌认证；先 `gpi server token create` 生成令牌（需经 HTTP 引导一次），请求带 `Authorization: Bearer <token>`，支持过期/撤销/轮换（`gpi server token list|revoke|rotate`）。
- Middleware 可扩展：`server.Middleware` 接口 + `AddMiddleware` 定制（认证/限流/追踪等）；内置安全头、CORS（`--enable-cors`）、request-id、日志中间件。
- OpenAPI/Swagger：`--docs` 开启 `/swagger.json`、`/docs`（Swagger UI）、`/redoc`；最新规范在仓库根 [openapi.json](openapi.json)（GitLab 内建在线渲染）。

### 部署到 Kubernetes

提供完整 K8s 部署资源（namespace / configmap / deployment / service / redis 后端 / PVC）：

```bash
kubectl apply -k deploy/k8s
kubectl -n gpi port-forward svc/gpi 8080:8080
```

详见 [deploy/k8s/README.md](deploy/k8s/README.md)。镜像由 Release workflow 构建推送到 `ghcr.io/acmestack/gpi`。

## Examples

任务 YAML（`examples/yaml/`，手写任务文件）与对应的 HTTP API 请求体（`examples/json/`，`{scene}-launch.json` 用于 `/clusters/{name}/launch` 的 YAML 字符串形式、`{scene}-task.json` 用于 `/tasks/{name}/launch` 的 Task 结构体形式）：

- [examples/yaml/train.yaml](examples/yaml/train.yaml) — 单机训练 · [obj.json](examples/json/train-obj.json) · [yamlstr.json](examples/json/train-yamlstr.json)
- [examples/yaml/aws-train.yaml](examples/yaml/aws-train.yaml) — 指定 AWS
- [examples/yaml/ordered-failover.yaml](examples/yaml/ordered-failover.yaml) — resources.ordered 按序 failover（AWS → 阿里云）
- [examples/yaml/distributed-train.yaml](examples/yaml/distributed-train.yaml) — 多节点 Ray 分布式训练
- [examples/yaml/llm-service.yaml](examples/yaml/llm-service.yaml) — LLM 服务化部署
- [examples/yaml/nightly-benchmark.yaml](examples/yaml/nightly-benchmark.yaml) — 定时任务
- [examples/yaml/with-credentials.yaml](examples/yaml/with-credentials.yaml) — 动态 AK/SK
- [examples/yaml/existing-cluster.yaml](examples/yaml/existing-cluster.yaml) — 挂已有主机
- [examples/yaml/docker-task.yaml](examples/yaml/docker-task.yaml) — Docker 执行
- [examples/yaml/local-task.yaml](examples/yaml/local-task.yaml) — 本机直跑

## Learn more

- [docs/gpi-architecture.md](docs/gpi-architecture.md) — 架构设计文档（版本号记录在内容中）
- [docs/gpi-new-cloud.md](docs/gpi-new-cloud.md) — **接入新云指南**（如何新增一个云 Provider）
- [docs/gpi-optimizer-extension.md](docs/gpi-optimizer-extension.md) — **扩展 placement optimizer 指南**（Metric / Optimizer / 策略）
- [docs/gpi-enhancements-over-skypilot.md](docs/gpi-enhancements-over-skypilot.md) — 相对 SkyPilot 的能力增强清单
- `gpi --help` / `gpi <command> --help` — 命令帮助

## Contributing

欢迎提交 issue 与 PR。参见 [CONTRIBUTING](CONTRIBUTING.md)。

## License

[MIT](LICENSE) © [AcmeStack](https://acmestack.com)
