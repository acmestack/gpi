# Gpi 相对 SkyPilot 的能力增强

- **文档版本**：v1（2026-08-08）
- **定位**：Gpi 参考 [SkyPilot](https://skypilot.readthedocs.io) 的模式，用 Go 实现多云算力调度（multi-cloud compute scheduling）。本文档专门记录 Gpi 在对照 SkyPilot 已有能力之外所做的增强/差异化设计，供评估与演进参考。基线能力（任务 YAML → Optimizer → Provisioner → setup/run、SkyServe、Sky Jobs、API server 等）见 `gpi-architecture.md`。

---

## 1. 零 SDK 依赖的云接入

- **SkyPilot**：依赖各云厂商官方 Python SDK（boto3、aliyun-python-sdk 等）。
- **Gpi**：不引入任何官方 SDK，用 Go 标准库直接实现云 API 与签名：
  - aliyun：HMAC-SHA1 签名（`internal/cloud/aliyun/sign.go`）。
  - aws：SigV4（AWS4-HMAC-SHA256）签名（`internal/cloud/aws/sign.go`）。
  - 好处：单二进制无运行时依赖、体积小、便于交叉编译与离线部署。

## 2. 每次任务动态 AK/SK

- **SkyPilot**：凭据只来自环境变量或本地配置文件（`~/.aws`、`~/.aliyun` 等）。
- **Gpi**：任务 YAML 顶层 `credentials:`（按云分块 aws/aliyun）可为每次任务动态提供 AccessKey/Secret：
  ```yaml
  credentials:
    aws:
      access_key_id: AKIA...
      secret_access_key: ...
      region: us-east-1
    aliyun:
      access_key_id: LTAI...
      access_key_secret: ...
  ```
  - 提供了 → 本次 launch 及后续 down/stop/start 复用该凭据；
  - 未提供 → 回退到现有 env/磁盘默认加载（`LoadCredentials`）。
  - 凭据持久化到集群状态（`state.CloudCreds`），生命周期操作无需重新指定。
  - 示例：`examples/with-credentials.yaml`。

## 3. tags / labels 统一合并

- **SkyPilot**：实例标签与集群内部标签是不同概念，分别维护。
- **Gpi**：对云而言 tags 与 labels 本质相同（都是实例 key-value 标签），统一处理：
  - 内置 `gpi:cluster` / `gpi:cloud` + 顶层 `tags:` + `resources.labels:` **合并**后写入云实例（`LaunchSpec.Tags`）；
  - 冲突时顶层 `tags:` 优先；
  - `resources.labels:` 除写云实例外，还以 `ray start --labels='{"k":"v"}'` 注入全部节点供 Ray 调度。

## 4. 多节点自动组 Ray 集群

- **SkyPilot**：多节点编排依赖 Ray 自有的 `sky`/`ray` 生态配置。
- **Gpi**：`num_nodes>1` 时 launch 自动完成节点角色划分与 Ray bootstrap：
  - 第 1 节点为 head（`ray start --head`，dashboard 8265），其余为 worker（`ray start --address=<head>:6379`）；
  - setup 在全部节点并行执行，run 在 head 执行；
  - 任务内注入 `{{cluster.head_ip}}` / `{{cluster.num_workers}}`，便于拼接分布式训练参数；
  - `gpi cluster status|nodes` 查看拓扑与角色。
  - 示例：`examples/distributed-train.yaml`。

## 5. 单进程双形态（CLI + Server）

- **SkyPilot**：CLI 与 server 为同一 Python 包的两种入口。
- **Gpi**：单 Go 二进制同时提供 `gpi` CLI 与 `gpi server` REST API + 调度器，状态统一落在本地 JSON（`~/.gpi`，`GPI_HOME` 可覆盖），无数据库依赖。

## 6. gpilet 节点 Agent（对标 skylet）

- **SkyPilot**：节点上有 skylet 常驻 agent 收集状态、转发命令/日志。
- **Gpi**：新增轻量 agent `gpilet`（`cmd/gpilet` + `internal/gpilet`），纯 Go、零依赖：
  - `gpilet serve --dir /var/lib/gpilet --interval 10`：常驻 daemon，周期采集节点状态（CPU 用量、loadavg、内存、磁盘、GPU 用量、Ray 是否运行）写入 `status.json`；
  - `gpilet status`：一次性输出当前状态（JSON）；
  - `Launch` 时 provisioner 自动把 gpilet 二进制上传到每个节点并拉起（未找到本地 gpilet 二进制则静默跳过，不影响置备）；
  - `gpi cluster nodes C --health`：经 SSH 读取各节点 gpilet 状态，展示实时健康（cpu/load/mem/gpu/ray）。
  - 定位：作为 skylet 的轻量等价物，为后续自动伸缩、健康轮询、资源实时上报提供节点侧数据源；暂无命令/日志代理（当前控制面走 SSH 直连）。

## 7. 可插拔持久化（file / sqlite / mysql / redis）

- **SkyPilot**：状态保存在本地文件（`~/.sky/` 下的 JSON）。
- **Gpi**：抽象 `state.Backend`，按 `GPI_STATE_BACKEND` 环境变量选择：
  - `file`（默认，每类数据一个 JSON 文件，兼容 `~/.gpi/*.json`）；
  - `sqlite`（单文件库，`GPI_STATE_SQLITE` 指定路径）；
  - `mysql`（`GPI_STATE_MYSQL_DSN` 指定数据源）；
  - `redis`（`GPI_STATE_REDIS_ADDR/PASSWORD/DB` 指定连接，每类数据一个 key：`gpi:clusters`/`gpi:services`/`gpi:jobs`/`gpi:cluster_yaml`/`gpi:cluster_history`/`gpi:cluster_events`/`gpi:config`/`gpi:tokens`）。
  - sqlite/mysql 按实体建表（`clusters`/`services`/`jobs`/`cluster_yaml`/`cluster_history`/`cluster_events`/`config`/`service_account_tokens`，对齐 SkyPilot 表结构）：常用字段抽为显式索引列、完整实体存 `data` JSON 列。便于部署到共享数据库、多实例并发读写。

## 12. API 令牌认证（service_account_tokens）

- **SkyPilot**：API server 用 JWT + sha256 哈希存库，认证中间件每请求查库校验撤销/轮换/过期。
- **Gpi**：`gpi server start --require-auth` 开启 Bearer 认证；明文令牌仅创建时返回一次，库中只存 sha256 哈希；支持过期（`--expires-in`）、撤销（`delete`）、轮换（`rotate`）；`POST /api/v1/gpi/tokens` 与 `/healthz` 公开以便引导首个令牌。对齐 SkyPilot 的 `service_account_tokens` 表与认证语义。

## 13. Middleware 抽象与 OpenAPI/Swagger

- **SkyPilot**：FastAPI 中间件栈（RBAC、Basic/Bearer auth、RequestID、CORS、SecurityHeaders 等）+ 自带 OpenAPI/Swagger。
- **Gpi**：`server.Middleware` 接口（`Wrap(next)`）+ `AddMiddleware` 自定义扩展，内置安全头/CORS/认证/request-id/日志；`--docs` 提供 `/swagger.json`（OpenAPI 3.0）、`/docs`（Swagger UI）、`/redoc`，docs 公开可看。标准库实现，无需引入框架。

## 8. 执行后端抽象（对标 SkyPilot backend 层）

- **SkyPilot**：backend 层分 `CloudVmRayBackend` / `LocalBackend` / `LocalDockerBackend` 等，负责"任务怎么跑"。
- **Gpi**：`internal/backend` 定义 `Backend` 接口（Launch/RunTask/Exec/Down/Stop/Start）+ `Manager` 分派，按 `task.Backend` 选择、按 `cluster.Backend` 后续分派：
  - `cloud`（默认）：云 VM + Ray + gpilet（原 provisioner）；
  - `existing`：挂已有主机，SSH 执行，不置备、不销毁外部主机；
  - `docker`：本地 Docker 容器执行（volumes/envs/gpus 可配），down 删容器、stop/start 停启容器；
  - `local`：本机直接执行 setup/run。
  - 非 cloud 后端跳过 optimizer（无 placement）。相比 SkyPilot 更薄：不做多集群抽象，保持单后端分派。

## 9. 可定制 API 响应结构（ResponseEncoder）

- **SkyPilot**：REST 响应结构固定。
- **Gpi**：`server.ResponseEncoder` 接口统一收口所有响应，团队可插拔：
  - 内置 `raw`（默认，原样数据 / `{"error":...}`）与 `envelope`（`{"code","message","data"}`，字段名可配置）；
  - 环境变量 `GPI_RESPONSE_FORMAT` 或 `gpi server start --response-format` 选择；
  - 任意团队自定义：实现接口后 `SetResponseEncoder(...)` 注册即可，无需改动 handler。

## 10. Request ID 全链路追踪

- **SkyPilot**：无内置 request ID 机制。
- **Gpi**：请求头携带上游 ID 则透传，否则生成 32 位十六进制随机 ID；响应 header 与 body 均回写同值：
  - header key 默认 `x-request-id`，`GPI_REQUEST_ID_HEADER` 或 `--request-id-header` 可改；
  - body 字段默认 `request_id`，与 ResponseEncoder 正交（raw/envelope/自定义均注入）。

## 11. 响应 key 风格可配置

- **SkyPilot**：响应 JSON key 风格固定。
- **Gpi**：`GPI_API_RESPONSE_KEY_STYLE` / `--api-key-style` / `SetKeyStyle` 可选 `camel`（默认，`numNodes`）、`snake`（`num_nodes`）、`pascal`（`NumNodes`）；递归作用于整个响应体所有 key，仅改线上格式、不影响 handler 与内部模型。

---

## 演进方向（后续增强）

- 多实例拆卡（如 A100:8 跨节点）。
- 更多可插拔目标/优化器（时延、碳排、预算……）按 `optimizer.Metric`/`optimizer.Optimizer` 接口扩展，可作为独立优化器或 `cost,latency` 策略中的优先级组合。
- spot 竞价格上限、自动伸缩/健康轮询（SkyServe 对等）。
- job 日志持久化与历史查询。
