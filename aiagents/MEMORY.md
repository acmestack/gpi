# Gpi 项目沟通记录（MEMORY）

- **文档版本**：v13（2026-08-14）
- 本文件记录从项目立项至今的每一次沟通内容与决策，供后续对话快速恢复上下文。
- 变更规则遵循项目根 `AGENTS.md`：docs 长期文档版本号记录在内容中，此处同理。
- **v13（2026-08-14）**：补录版本号机制——版本号唯一来源 git tag，`internal/buildinfo.Version` 唯一定义处，release 时 ldflags/build-arg 注入。
- **v12（2026-08-13）**：补录 §9.0 数据库表结构整理；移除旧单表迁移（代码/测试/文档/历史版本记录全量清理）。
- **v11（2026-08-13）**：补录 config 架构决策——云专项配置下沉云包、`internal/config` 云无关、两个 config 区分。
- **v10（2026-08-13）**：补录 docs 版本记录移到文档末尾。
- **v9（2026-08-13）**：补录 lexicographicOptimizer 独立成 lexicographic.go。
- **v8（2026-08-13）**：补录 Objective→Metric、strategyOptimizer→lexicographicOptimizer 改名。
- **v7（2026-08-13）**：补录扩展指南"打分后分数如何使用"小节。
- **v6（2026-08-13）**：补录 optimizer 扩展文档文件表修正。
- **v5（2026-08-13）**：补录 AGENTS 提交确认/tag 规则与 release tar 修复。
- **v4（2026-08-09）**：补录 v0.0.1 发布与 RELEASE_NOTES 决策。
- **v3（2026-08-09）**：补录 ec2 命名、RunInstances 复用、optimizer 包拆分、OpenAPI 改 GitLab 渲染等近期决策；更新 OpenAPI 查看方式速查。
- **v2（2026-08-09）**：按 AGENTS.md 新规则建立"每次沟通后追加记录"；补齐 K8s 部署 / OpenAPI 在线预览 / License/CLA / PR 模板等近期决策；修正头部日期。
- **v1（2026-08-09）**：创建本文件，汇总从立项起的沟通历史与关键决策。

## 沟通记录

### 2026-08-08（立项 · v1 骨架）

- 确定项目：参考 SkyPilot 模式，用 Go 重写实现多云算力调度，对标 SkyPilot 的 launcher / optimizer / SkyServe / Sky Jobs / API server。
- 确定位置 `opencode/gpi`（后更名），module `github.com/acmestack/gpi`，CLI 名 `gpi`，首个云 aliyun，CLI+Server 双形态。
- 完成 v1 全部功能骨架并通过测试。

### 2026-08-08（AWS Provider · Ray 集群）

- 实现 AWS Provider（`internal/cloud/aws` + `internal/catalog/aws*`）：零 SDK 依赖、SigV4 签名、EC2 全生命周期、默认 VPC/新建 VPC+IGW+Route、SG、KeyPair、Ubuntu AMI 自动选择。
- catalog 覆盖 13 region × (t3/m5/m6i/c5 + g4dn/g5/p3/p4d)。
- optimizer 默认改为遍历全部已注册云做跨云比价。
- 补齐 Ray 集群架构：`num_nodes>1` 时 head/worker 角色，launch 后自动 bootstrap Ray。
- 支持自定义 tags 与 labels、每次任务动态 AK/SK（`credentials:`）、tags/labels 合并。

### 2026-08-08（gpilet · 持久化 · 后端抽象）

- 新增 gpilet 节点 agent（对标 skylet）：`gpilet serve` 常驻采集 CPU/内存/磁盘/GPU/Ray 状态。
- 持久化可插拔：`state.Backend`，file（默认）/sqlite/mysql。
- 新增执行后端抽象（对标 SkyPilot backend 层）：cloud/existing/docker/local。
- API 响应结构可定制（ResponseEncoder raw/envelope）、Request ID、响应 key 风格（camel/snake/pascal）。

### 2026-08-08（平台 · 表结构 · 认证 · Middleware · 更名 gpi）

- 明确平台支持 Linux/macOS，移除 Windows。
- SQL 后端按实体拆表（clusters/services/jobs）。
- 补齐集群快照/历史/事件（cluster_yaml/history/events）。
- 补齐 config 表与 service_account_tokens（--require-auth Bearer 认证）。
- Middleware 抽象 + OpenAPI/Swagger（--docs）。
- 全项目去重名 `gpilot → gpi`：module、文档、标题/描述/日志/DB/云资源名统一。

### 2026-08-08（Catalog 静态+实时 · 注册收敛 · 聚合包）

- Catalog 改为规格静态 + 价格实时：新增 `internal/pricing`（TTL 缓存 + 惰性实时拉取）。
- 云注册收敛为单一入口（`cloud.Register`/`RegisterFactory`/`pricing.Register`/`catalog.Register`）。
- 新增聚合包 `internal/cloud/imports`，所有云空白导入集中一处。
- 聚合包改为自动生成（`gen.go`，`go generate ./internal/cloud/imports`），挂 `make build` 前置。

### 2026-08-09（Catalog 全面元数据化 · Optimizer 可插拔）

- **关键决策**：Catalog 全面元数据化，删除全部静态 `xx_data.go`。规格与价格都是"定时读取的元数据"。
- 每云实现 `catalog.Source`（`SpecsTTL`/`PriceTTL` 按云自定义），`catalog.Cache` TTL 缓存（规格 24h、价格 10min，stale-while-error）。
- `internal/pricing` 包移除，价格并入 catalog；aliyun/aws 新增 `metadata.go`（`DescribeInstanceTypes`/`DescribeInstanceTypeOfferings` 全量规格）。
- Optimizer 可插拔化：`optimizer.Optimizer` 接口 + 注册表 + `--optimizer` 标志。
- 价格拉取预算化：候选截断 `maxPricedCandidates=200` + 并发 `priceWorkers=8`。
- 无价候选排有价之后；修复分页签名残留（`call` 内清理 `Signature`）。
- 真机问题：1956 个机型逐个拉价超时 → 并发+截断；修复并行数组排序 bug。

### 2026-08-09（新云接入单 struct · Redis 后端 · Dockerfile）

- **决策**：新云接入只需一个 struct——`Provider` 同时实现 `cloud.Provider` + `catalog.Source`，`cloud.Register` 类型断言自动注册元数据源。
- 新增 Redis 状态后端（`GPI_STATE_BACKEND=redis`，go-redis/v9，miniredis 测试）。
- 新增 `Dockerfile`（多阶段构建 gpi+gpilet）+ `.dockerignore` + `make docker`。

### 2026-08-09（元数据包拆分演进：catalog↔metacache）

- 三次重构：①`internal/catalog` 删除，契约并入 `internal/cloud`，Cache 独立 `internal/metacache`；②catalog 包保留并下沉 `internal/cloud/catalog`（撤销 metacache 拆分）；③最终定稿：`internal/cloud/catalog` 只留契约（Source 接口 + Instance/Price + 注册表，对应 `sky/catalogs`），TTL Cache 运行时独立 `internal/metacache`。
- 依赖单向：`cloud ← cloud/catalog ← metacache ← optimizer`。
- 规格类型最终为 `catalog.Instance`。

### 2026-08-09（注释补齐 · time 优化器 · 策略优化器）

- 删除死代码 `matchInstances`；全仓库补齐导出符号注释。
- 新增 `time` 优化器（参考 SkyPilot `OptimizeTarget.TIME`）：按预估运行时长升序，`resources.time_sec` 或算力启发式（`est = 4h/(vcpus+GPU×16)`）。
- **决策**：Optimizer 支持两种选择模式——指定优化器（`cost`/`time`）与指定优化策略（`--optimizer cost,time` 字典序多目标）。

### 2026-08-09（优化器扩展 API 开放）

- `Optimizer`/`Objective`/`Candidate`/`RegisterObjective`/`ParseStrategy`/`NewStrategy` 全部导出；外部包测试 `optimizer_ext_test.go` 即扩展示例。
- `Request.Meta` 移除；`DefaultMeta` 降为未导出 `defaultMeta`，`SetDefaultMeta` 测试注入，`PricesForced` 便捷导出。
- 文件按职责拆分：`candidate.go`（管道）、`objective.go`（接口）、`strategy.go`（排序）、`cost.go`（cost 目标+`OptimizeByCost`）、`time.go`（time 目标+`OptimizeByTime`）。
- `Objective.Rank` 更名（原 `Score`）；`NewStrategy`/`ParseStrategy` 移到 strategy.go。
- 新增 `docs/gpi-optimizer-extension.md` 完整扩展指南。

### 2026-08-09（Swagger/CI/文档规则）

- `strategy.go` 的 `init` 移到文件末尾。
- CI/Release workflow 增加 Docker 构建（CI 构建验证，Release 推送 GHCR）。
- **决策**：文档版本号规则从各文档内移到项目根 `AGENTS.md`；docs 长期文档版本号记录在内容中。
- Swagger 补齐请求体 schema（含 `optimizer` 字段）；修复 KeyStyle 下 `$ref` 断裂（`convertKeys` 同步转换 schema 名）。

### 2026-08-09（REST API 前缀 · Task 结构体接口）

- **决策**：API 前缀可自定义，默认 `/api/v1/gpi`（`GPI_API_PREFIX` 或 `--api-prefix`），swagger 显示完整 path。
- **决策**：task 两种输入方式且 path 有区分——`clusters/{name}/launch`（YAML 字符串）+ `tasks/{name}/launch`（Task 结构体 JSON）。
- 给 task 结构补 JSON tags、`Range.UnmarshalJSON`（支持 `"cpus":"8+"`）；抽取共享 `launchTask` 管道。
- 修复 Swagger UI 下 POST 无请求体：OpenAPI 文档豁免 KeyStyle（`writeRawJSON`），schema 属性名统一 camelCase。

### 2026-08-09（Swagger Task schema · 文档版本格式 · 收尾）

- swagger 的 `taskLaunchRequest.task` 引用完整 `Task`/`Resources` schema。
- **决策**：docs/*.md 统一版本格式——顶部版本号 + 下方每次变更的版本记录（对齐 gpi-optimizer-extension.md）。architecture 版本记录移到顶部（v43→v1 降序），new-cloud 补齐变更记录。
- 排查并修正文档/代码不一致：`/api/v1/tokens` → `/api/v1/gpi/tokens`、README 存储后端列表补 redis。
- **待办**：创建 `aiagents/MEMORY.md`；LICENSE 用 MIT；项目 CLA（参考 acmestack）加入 `.github`。

### 2026-08-09（K8s 部署 · OpenAPI 在线预览 · CLA · 收尾）

- **决策**：新增 `deploy/k8s/` 部署资源（namespace/configmap/deployment/service/redis/pvc/kustomization），`kubectl apply -k deploy/k8s` 一键部署。
- **决策**：OpenAPI 规范以 `docs/apis/openapi.json` 提交（`cmd/gen-openapi` + `make openapi` 生成），K8s Deployment 默认不开启 `--docs`。
- **决策**：GitLab 内建 OpenAPI 预览，GitHub 不渲染 → 用 **GitHub Pages + Swagger UI** 提供交互式预览，访问 `https://acmestack.github.io/gpi/apis`（`docs/apis/index.html` + `swagger-initializer.js` + `.github/workflows/pages.yml`）。
- **决策**：LICENSE 用 MIT（© ACMEStack）；项目 CLA 参考 AcmeStack（`acmestack/.github` 的 AcmeStack-CLA.md 中英全文）→ `.github/CLA.md` + `cla-assistant.json`；新增 PR 模板（`.github/PULL_REQUEST_TEMPLATE.md`，中英双语）。
- README 新增"部署到 Kubernetes"与 Overview 内可扩展入口链接。
- **决策**：AGENTS.md 新增"沟通记录（MEMORY）"规则——每次沟通结束都追加到 `aiagents/MEMORY.md`。

### 2026-08-09（ec2 命名 · RunInstances 复用 · optimizer 拆分 · OpenAPI 换 GitLab 渲染）

- **任务1**：aws `ec2.go` 命名确认正确（AWS 虚拟机服务是 EC2，ECS 是容器服务），不改。
- **决策**：`RunInstances` 参考 SkyPilot 改为**先 list 再复用/重启/创建**——按 cluster 名查已有实例，running 足够直接复用、stopped（`ResumeStoppedNodes`）`StartInstances` 重启、否则新建（aliyun + aws；`LaunchSpec.ResumeStoppedNodes`，provisioner 默认开启）。
- **决策**：optimizer 包按职责拆分——`plan.go`/`request.go`/`meta.go`（合并原 meta_adapter）/`registry.go`/`candidate.go`/`objective.go`/`strategy.go`/`cost.go`/`time.go`；`Optimizer` 接口 + `Get`/`Resolve` 最终保留在 `optimizer.go`，`registry.go` 只留注册表。
- 扩展指南补"Objective vs Optimizer"差异（打分维度 vs 决策整体）。
- **决策**：移除 GitHub Pages（不支持 OpenAPI 渲染）——删除 `pages.yml`/`docs/apis/index.html`/`swagger-initializer.js`；`openapi.json` 改提交到**仓库根**，用 **GitLab 内建 OpenAPI viewer** 在线查看（托管在 code.cestc.cn）。

### 2026-08-09（发布 v0.0.1 · Release notes）

- **决策**：发布首个版本 `v0.0.1`，tag 已推送 code.cestc.cn。
- **决策**：不新增 GitLab CI——发布自动化保留在 GitHub（`.github/workflows/release.yml`，tag 触发构建 + GHCR + GitHub Release）。
- **决策**：新建 `.github/RELEASE_NOTES.md` 作为丰富版发布说明，release workflow 用 `body_path` 引用（替代自动生成）。

### 2026-08-13（AGENTS 提交确认/tag 规则 · 修复 release tar 报错）

- **决策**：AGENTS.md 增加两条规则——①**每次修改后先展示 diff，用户确认后再 `git commit`**，不得直接提交；②版本发布（tag）规则：语义化版本、一律 **annotated tag**（`git tag -a` 带 message）、tag message 首行定位+版本特性、发布后改动用 `git tag -f` + `git push -f`。
- 修复 GitHub release 报错：`tar --exclude` 必须放在文件列表**之前**（GNU tar 要求），release.yml 的 Compress 步骤调整参数顺序。

### 2026-08-13（修正 optimizer 扩展文档文件表）

- 修正 `docs/gpi-optimizer-extension.md` 文件职责表，对齐当前 optimizer 包拆分后的结构（optimizer/plan/request/meta/registry/candidate/objective/strategy/cost/time/match），移除已合并的 `meta_adapter.go` 行。

### 2026-08-13（扩展指南补"打分后分数如何使用"）

- 在 `docs/gpi-optimizer-extension.md` 新增小节：分数向量 → 字典序排序 → 截断 → Plan 的机制，`cost,latency` vs `latency,cost` 具体排序演示，以及 Optimizer 场景下分数的自由使用。

### 2026-08-13（Objective→Metric、strategyOptimizer→lexicographicOptimizer 改名）

- **决策**：命名调整——`Objective` → `Metric`（打分指标），`costObjective`/`timeObjective` → `costMetric`/`timeMetric`，`RegisterObjective` → `RegisterMetric`，`ObjectiveNames` → `MetricNames`，`objective.go` → `metric.go`；`strategyOptimizer` → `lexicographicOptimizer`（字典序多指标排序实现），对外 `NewStrategy`/`ParseStrategy` 保留。
- **待办**：`strategyOptimizer` 命名曾列为待定，本次定为 `lexicographicOptimizer`；`Metric` 选用理由（单一打分维度，避免 Objective 的多目标歧义）。

### 2026-08-13（lexicographicOptimizer 独立成 lexicographic.go）

- **决策**：`lexicographicOptimizer`（算法实现：Name/Optimize）独立为 `lexicographic.go`；`strategy.go` 只留策略构造（`NewStrategy`/`ParseStrategy`）+ 内置 `cost`/`time` 注册，职责更清晰。

### 2026-08-13（docs 版本记录移到文档末尾）

- **决策**：docs 长期文档的版本变更记录（changelog）统一移到**文档末尾** `## 版本记录` 区块（vN 降序），顶部只保留版本号 + 元信息 + 正文，避免 changelog 影响重点；同步更新 AGENTS.md 的 docs 约定。

### 2026-08-13（config 架构：云专项配置下沉 + 两个 config 区分）

- **背景**：此前在 `internal/config` 用强类型 `AWS`/`Aliyun` 结构体 + `AWSConfig()`/`AliyunConfig()` 访问器承载云网络复用配置，新增云需改 `internal/config`（加结构体/字段/overlay/访问器），破坏"新云只增 1 包"承诺。- **决策①（云专项配置下沉）**：`internal/config` 改为**云无关**——只留通用字段（`allowed_clouds`/`region`/`zone`/`use_spot`）+ `Section(name, out)` 通用解码；层叠合并从字段级改为 **yaml 节点级**（`parseNode` + `mergeNode`，项目覆盖用户，嵌套 mapping 递归合并）。
- **决策②（云配置放各云包）**：每个云在**自己的包**定义 `Config` struct（`internal/cloud/aws/config.go`、`internal/cloud/aliyun/config.go`）+ `LoadConfig()`（`config.Load().Section(CloudName, &c)`）。新增云**零改动** `internal/config`。
- **决策③（Provider 接口不加 Config()）**：Go 接口方法无法为不同云返回各自类型（只能 `any` 丢类型安全），且云段只有 provider 自己消费；不采纳"接口加 Config()"方案。
- **决策④（一次解码）**：`RunInstances` 里 `cfg := LoadConfig()` 只解码一次，`vpcFor`/`securityGroupFor`/`subnetsFor`（aws）、`vswitchesFor`（aliyun）改为接收 `spec *cloud.LaunchSpec` + `cfg *Config` 参数（cfg 在 client 之后），消除重复 yaml 解码。
- **决策⑤（两个 config 区分，只文档区分不改代码）**：`internal/config` = **文件配置**（单机客户端，启动偏好）；`internal/state` 的 `config` 表 = **运行时配置**（服务端多实例共享 KV，目前零生产消费者，测试 `autostop` 占位）。`config.Load()` **不读** state 表。新增架构文档 §9.1.1 对比表 + 一句话记忆，代码命名不动。
- **改动文件**：`internal/config/config.go`（云无关化 + Section/mergeNode）、`internal/config/config_test.go`、`internal/cloud/aws/config.go`（新）、`internal/cloud/aliyun/config.go`（新）、`internal/cloud/aws/provider.go`、`internal/cloud/aliyun/provider.go`、`internal/cloud/aws/provider_config_test.go`、`docs/gpi-architecture.md`（§9.1.1，升 v54）、`docs/gpi-new-cloud.md`（config.go 步骤，升 v13）。
- 验证：`go build ./... && go vet ./... && go test ./...` 全绿。

### 2026-08-13（数据库表结构整理 · 移除旧单表迁移）

- 架构文档新增 **§9.0 数据库表结构（sqlite/mysql）**：全部 8 张表（`clusters`/`services`/`jobs`/`cluster_yaml`/`cluster_history`/`cluster_events`/`config`/`service_account_tokens`）的列/类型/约束/主键对比表 + `ensureTables` 完整 DDL，附 file/redis 后端映射说明。
- **移除旧版单表迁移（全量）**：`internal/state/sql.go` 删除 `migrateLegacyTable`（`ensureTables` 不再调用），删除 `state_test.go` 的 `TestMigrateLegacyTable` 与 `database/sql` 导入；文档正文、§9.0、历史版本记录（v15/v9）、`gpi-enhancements-over-skypilot.md` §11、MEMORY 速查表均清理相关描述。
- 架构文档升 v55；`go build/vet/test ./internal/state` 通过。

## 关键设计决策速查

| 决策 | 结论 |
|------|------|
| 元数据来源 | 全动态，无静态数据；规格/价格按 TTL 缓存，每云可自定义 |
| 元数据包结构 | 契约 `internal/cloud/catalog`（对应 sky/catalogs）+ Cache `internal/metacache` |
| 新云接入 | 一个包 + 一个 struct（Provider 同时实现 Provider+Source） |
| Optimizer | 可插拔；两种模式：指定优化器（cost/time）或策略（cost,time 字典序） |
| 扩展优化器 | 实现 `Metric` + `RegisterMetric`/`NewStrategy`（字典序 `lexicographicOptimizer` 排序） |
| 元数据访问 | 全局 `defaultMeta`（`SetDefaultMeta` 注入） |
| REST 前缀 | `/api/v1/gpi` 可自定义（`GPI_API_PREFIX`/`--api-prefix`） |
| task 输入 | `clusters/{name}/launch`=YAML 字符串；`tasks/{name}/launch`=Task JSON |
| 文档版本 | 版本号记录在 docs 内容中；变更记录放顶部版本号下方 |
| K8s 部署 | `deploy/k8s/`，`kubectl apply -k deploy/k8s`；默认 redis 后端 |
| OpenAPI 在线查看 | 仓库根 `openapi.json` + GitLab 内建 viewer（无需 GitHub Pages） |
| License / CLA | MIT；AcmeStack CLA（`.github/CLA.md` + cla-assistant） |
| 沟通记录 | 每次沟通后追加到 `aiagents/MEMORY.md` |
| 平台 | Linux/macOS；无 Windows |
| 用户配置文件 | `$GPI_HOME/config.yaml`（默认 `~/.gpi/config.yaml`）+ 项目 `.gpi.yaml` 层叠（项目覆盖用户） |
| 云专项配置 | 各云自己包内定义 `Config` struct + `LoadConfig()`（`config.Load().Section(CloudName, &c)`），`internal/config` 云无关、新云零改动 |
| 两个 config | `internal/config`=文件配置（客户端启动偏好）；`internal/state` 的 `config` 表=运行时 KV（服务端共享，零消费者）。config.Load() 不读 state 表 |
| 数据库表结构 | 8 张表按实体拆分（PK + 索引列 + data JSON 列），完整结构见架构文档 §9.0 |
