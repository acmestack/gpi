# Gpi 项目沟通记录（MEMORY）

- **文档版本**：v24（2026-08-15）
- 本文件记录从项目立项至今的每一次沟通内容与决策，供后续对话快速恢复上下文。
- 变更规则遵循项目根 `AGENTS.md`：docs 长期文档版本号记录在内容中，此处同理。
- **v24（2026-08-15）**：新增 `upstream` remote（`acmestack/gpi`）并修复分支基线（本地 main 曾落后上游 1 提交致 PR 冲突，教训：切新分支前先 `git fetch upstream && git reset --hard upstream/main`）；新增分支 `fix-docs-links`——修复根 README 指向 `examples/`、`openapi.json`、`deploy/k8s/README.md`、`LICENSE` 的错误 `../` 前缀（仓库根资源直接用文件名）；架构文档 v61→v62，两张 mermaid 图补 logging 横切能力节点 + 包结构新增 `internal/logging`，中英同步。
- **v23（2026-08-15）**：补录 logging 体系完整决策（包级 WithName logger、CLIPrintf 通道、补充 cloud/backend/optimizer 关键日志）+ server middleware 拆分文件。
- **v22（2026-08-14）**：补录文档双语化——`docs/zh/` 与 `docs/en/` 两套目录；随后用户调整：**README 放仓库根**（`README.md` 中文 + `README.en.md` 英文，互相语言切换），`docs/zh|en` 各含其余 5 个文档；`deploy/k8s/README.md` 等代码配套说明保持单语原位；**`.github/CLA.md` 与 `.github/RELEASE_NOTES.md` 同文件逐句双语（每句中文后紧跟对应英文）**。
- **v21（2026-08-13）**：补录 CLA workflow 修复 + 清理 useSpot 死参数。
- **v20（2026-08-13）**：补录 example json 命名 + 删 Task.Time。
- **v19（2026-08-13）**：补录 task 拆分 + examples yaml/json 分目录。
- **v18（2026-08-13）**：补录 task 包 json tag 统一小驼峰 + Credentials omitempty。
- **v17（2026-08-13）**：补录 Credentials 泛化为云无关 map。
- **v16（2026-08-13）**：补录 Resources ordered failover。
- **v15（2026-08-13）**：补录 serve vs server 区别小节。
- **v14（2026-08-13）**：补录 serve/jobs 优化器支持 + SkyPilot gap 调研（含待建 issues）。
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
- **决策**：移除 GitHub Pages（不支持 OpenAPI 渲染）——删除 `pages.yml`/`docs/apis/index.html`/`swagger-initializer.js`；`openapi.json` 改提交到**仓库根**，用 **GitLab 内建 OpenAPI viewer** 在线查看。

### 2026-08-09（发布 v0.0.1 · Release notes）

- **决策**：发布首个版本 `v0.0.1`，tag 已推送。
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

### 2026-08-13（serve/jobs 支持自定义优化器 · SkyPilot gap 调研）

- **决策**：参考 SkyPilot，`gpi serve up` 与 `gpi jobs` 都应支持优化器（SkyPilot 中 `sky serve` 用 `sdk.optimize(dag)`、jobs 用 `optimize_job_group(dag)`）。已改造：
  - `serve up` 加 `--optimizer` flag，用 `optimizer.Resolve` 替代硬编码 `Default()`。
  - `state.Job` 加 `Optimizer` 字段；`jobs submit` 加 `--optimizer` flag；`Manager.Submit` 接收并持久化；`runOnce` 用 `Resolve(job.Optimizer)`（空则默认 cost）。
  - server API `jobSubmitRequest` 加 `optimizer` 字段；swagger 同步；补 `internal/jobs/jobs_test.go`（持久化/默认空测试）。
- **调研（SkyPilot 有而 gpi 缺，待创建 GitHub issues）**：managed jobs 控制器 + 恢复策略（FAILOVER/EAGER_NEXT_REGION）、serve autoscaler、serve load balancer + 负载均衡策略、spot 抢占自动恢复、storage/volumes 对象存储、ssh_node_pools/workspaces、dashboard Web UI、admin_policy/users/usage 多租户、cloud_stores、resources 的 max_hourly_cost 预算上限。
- **调研（问题 2，另议）**：SkyPilot `Task.resources` 是 List（`any_of`/`ordered` 候选集 + 外层默认值），gpi 现为单数；改造成重大设计变更，另行讨论。
- 注：`gh` CLI 未安装，GitHub issues 需手动创建或改用 API。

### 2026-08-13（架构文档补 serve vs server 区别）

- 架构文档新增 **§9.2 serve 与 server 的区别**：`gpi serve`=把任务部署成常驻服务（SkyServe 对标），`gpi server`=启动 gpi 的 HTTP API 服务；用 LLM 服务示例 + 调用链（HTTP → server → serve）说明，记忆口诀"serve 把任务 serve 出去，server 让 gpi 当 server"。
- 顺带修正头部版本号不一致（其他电脑升 changelog 到 v55 但头部停在 v53），统一为 v56。

### 2026-08-13（Resources 支持 ordered failover）

- **决策**：`task.Resources` 新增 `ordered []*Resources`（对标 SkyPilot `resources.ordered`）：外层字段作所有候选默认值、条目覆盖默认；optimizer 按序为每组生成候选并**串联成 failover Plan**（组间保持 ordered 顺序，组内按 metric 字典序排序）。
- 实现：resources.go 加字段/解析/fillDefaultsFrom/Copy/Nothing/String；lexicographicOptimizer 重构为 `collectAndPrice`（每组收集+拉价）+ `rankPlan`（按 groupOf 分组排序）；测试 `TestOrderedFailoverResources`/`TestOrderedEntryOverridesDefaults`；示例 `examples/ordered-failover.yaml`（AWS → 阿里云）+ README 链接。
- 架构文档 §4 记录 ordered 语义（v57）。

### 2026-08-13（Credentials 泛化为云无关 map）

- **决策**：`task.Credentials` 从按云硬编码（`AWS`/`Aliyun` struct + `ForCloud` switch）改为**云无关通用 map** `credentials: { <cloud>: { access_key_id, secret_access_key, region } }`——新云零改动即可复用；自定义 UnmarshalYAML 兼容 aliyun 旧字段 `access_key_secret`；`ForCloud`/`Validate` 去 switch；MarshalYAML 输出 `secret_access_key`。
- 测试：`TestCredentialsGenericCloud`（gcp 零改动）/`TestCredentialsLegacySecretField`。
- new-cloud v14、架构 v58 记录。

### 2026-08-13（task 包 json tag 统一小驼峰 + Credentials omitempty）

- **决策**：task 包所有 json tag 从 snake 统一为**小驼峰**（`num_nodes`→`numNodes`、`access_key_id`→`accessKeyId` 等），yaml tag 保持 snake（用户手写 YAML 不变）；`Credentials.Clouds`/`Task.Credentials` 加 `omitempty`。
- server API DTO（`launchRequest`/`taskLaunchRequest`）json tag 同步改 camel，对齐 swagger 文档（此前 swagger 写 camel 但 DTO 是 snake，属不一致）。
- 影响：`/tasks/{name}/launch` 的 Task JSON 与 launch 请求体字段名改 camel（`numNodes`/`useSpot`/`clusterName` 等）；`TestLaunchTaskJSONBody` body 更新；补 `TestCredentialsJSONCamelAndOmitEmpty`。

### 2026-08-13（task 拆分 + examples 拆 yaml/json）

- **决策**：task 包拆分——`credentials.go`（Credentials/CloudCredentials + 编解码）、`spec.go`（SSHTarget/DockerSpec/ServiceSpec），`task.go` 只留 Task 与解析/默认/命令方法；Task 每字段加注释、可选字段（envs/setup/workdir 等）补 omitempty。
- **决策**：`Range.MarshalJSON` 输出紧凑字符串（`"8+"`/`"4-8"`/`"8"`），JSON 请求体 `cpus:"8+"` 友好且 round-trip。
- **决策**：examples 拆两目录——`examples/yaml/`（10 个任务文件）+ `examples/json/`（每个场景 2 个：`{scene}-launch.json` 供 `/clusters/{name}/launch` YAML 字符串形式、`{scene}-task.json` 供 `/tasks/{name}/launch` Task 结构体形式）；`gpi-config.yaml` 留根。
- swagger Task schema 补可选字段说明 + SSHTarget/DockerSpec/ServiceSpec 子 schema。
- 架构 v60、new-cloud v15。

### 2026-08-13（example json 命名 + 删除死字段 Task.Time）

- **决策**：examples/json 命名——Task 结构体形式 `{scene}-obj.json`、YAML 字符串形式 `{scene}-yamlstr.json`（原 `-task`/`-launch` 改名）。
- **决策**：删除死字段 `Task.Time`（无任何业务消费，仅 OrderFields 判空；SkyPilot 用 `resources.time_sec` 做运行时估算，gpi 已有 `Resources.TimeSec`）；同步删 swagger/openapi/架构文档的 `time` 字段。
- **确认**：`config.AllowedClouds` 用途正常——`cfg.Cloud()` 在 `cli/launch.go:48,174` 被 `gpi launch/optimize` 未指定 `--cloud` 时用作默认云过滤。

### 2026-08-13（CLA workflow 修复 · 清理 useSpot 死参数）

- **决策**：CLA assistant 用 `contributor-assistant/github-action@v2.6.1`（维护中版本，替代已归档的 `cla-assistant/github-action@v2.1.3-beta`）；签名存同一仓库 `branch: cla`（acmestack/gpi，分支不可保护）；**不设 remote-org/repo**（避免 PAT 401）；`permissions` 需完整（actions/contents/pull-requests/statuses write）；`lock-pullrequest-aftermerge: false`；PR 模板**删除 CLA 段落**（避免与 bot 冲突）。
- **决策**：`attachPrices`/`collectAndPrice` 的 `useSpot` 参数为**死参数**（attachPrices 总是同时拉 on-demand+spot 两份价格），已从签名与调用处移除。useSpot 真正生效点：`rankPlan` → `Metric.Rank(c, useSpot)`（cost.go 的 `CostPerHour(useSpot)`）→ `Launch.UseSpot` → provisioner。
- **决策**：全部提交 GPG 签名（`user.signingkey=EE2178E827265FD0`，commit/tag 用 `-S`）。

### 2026-08-15（README 链接修复 · 架构图补 logging）

- **背景**：用户发现根 README 有文件链接不对，并询问架构图是否需要更新。
- **决策**：扫描全部 markdown 链接（README/README.en/docs/zh/docs/en/deploy k8s/AGENTS）——根 README 在仓库根，指向仓库根资源（`examples/`、`openapi.json`、`deploy/k8s/README.md`、`LICENSE`）直接用文件名，**不能加 `../`**（此前错误加了 `../`，链接指向仓库外）。修复后 0 broken 链接。
- **决策**：架构文档 v61→v62——架构总览图 & 分层视图的横切能力层补 `logging` 节点（结构化日志 + CLI 输出双通道），`SERVER` 子图更名「横切能力」，包结构新增 `internal/logging`；中英同步，版本记录补 v62。
- **工作流教训**：PR 冲突根因是切分支前未同步上游——本地 main 落后上游 `acmestack/gpi` 1 提交（双语化）。已新增 `upstream` remote，此后切新分支前先 `git fetch upstream && git checkout main && git reset --hard upstream/main`。
- **涉及文件**：`README.md`、`README.en.md`、`docs/zh|en/gpi-architecture.md`（v62）、`aiagents/MEMORY.md`（v24）。分支 `fix-docs-links`。

### 2026-08-15（logging 体系定稿 · server middleware 拆分）

- **决策**：日志体系（zap v1.28.0 + lumberjack v2.2.1，vendor 模式）定稿。双通道：诊断日志（`internal/logging`）与 CLI 用户输出（`logging.CLIPrintf`/`CLIPrintln`/`CLIPrint`，写 stdout、不被 `--log-file` 重定向）。
- **决策**：包级 logger 统一模式——每个包在同包名 go 文件最顶部（import 之后）定义 `var logger = logging.WithName("模块名")`；**禁止**结构体 `Log *logging.Logger` 字段 + 构造函数初始化（已从 provisioner/serve/jobs/server/metacache/backend 三后端/cloud aws+aliyun 全部移除）。`WithName` 每次调用经 `Get()` 动态解析 base logger，故包级 var 在 `Setup` 前创建仍跟随后续配置。
- **决策**：`Logger` 为 `zap.SugaredLogger` 薄封装，级别方法签名 `Info(msg string, kvs ...any)`（禁止 `zap.String` 等字段构造）；`AddCallerSkip(1)` 正确（SugaredLogger 内部抵消一层）。`internal/logging` 拆分 7 文件：logging.go/setup.go/config.go/encoder.go/rotate.go/level.go/cli.go。
- **决策**：日志配置优先级 CLI flag > 环境变量（`GPI_LOG_LEVEL`/`GPI_LOG_FILE`/`GPI_LOG_FORMAT`）> config `logging:` 段 > 默认（stdout/info/text）；轮转默认 MaxSize=100MB/MaxBackups=5/MaxAge=30/Compress=true。
- **决策**：`/healthz` 请求不记日志；移除冗余日志（backend launch、optimizing placement、backend.Manager.Log 字段）；移除仅测试用的 `Text()`。
- **决策**：为 cloud 关键路径补日志——aws/aliyun `RunInstances` 决策分支（reuse running / resume stopped / create instances，Info 级）；backend docker/local/existing 生命周期（launch/teardown/stop/start）；optimizer 关键点（candidates collected / chose placement，Debug 级）。gpilet collect、state、task、optimizer 纯计算层不加（高频/低层/无副作用，调用方已覆盖）。
- **决策**：`internal/server` 的多个 middleware 各自独立文件：`middleware.go`（接口+chain）、`cors.go`、`security.go`、`logging.go`；删除无引用的 `unixMillis`。
- **决策**：import 分组三段式（系统/第三方/内部），统一 `~/go/bin/goimports -local github.com/acmestack/gpi -w`；约定已写入 AGENTS.md「Go 代码约定」。
- **决策**：json tag 复核结论——state snake_case 为存储格式（API 输出由 `applyKeyStyle` 转 camelCase）、aws/aliyun PascalCase 为云厂商响应格式、task 已小驼峰、gpilet snake_case 为磁盘协议，均无需改动。
- **决策**：`internal/cli` 与 `cmd/gpilet` 也统一包级 `var logger = logging.WithName(...)` 模式（cli/serve.go 的 `logging.Get().Warn` 改 `logger.Warn`），全库不再有散落的 `Log` 字段或 `logging.Get()` 调用。

### 2026-08-14（文档双语化：docs/zh + docs/en + 根 README）

- **背景**：用户要求"所有面向用户的文档中英双语，每语言一套"，除 issue/PR 模板与代码配套说明外都要两个语言版本，每种语言从 README 作入口链接全套。
- **决策①（目录布局，用户两次澄清后定稿）**：**README 放仓库根**（GitHub 只渲染根目录 README.md，软链接不可靠）——`README.md`（中文主 README）+ `README.en.md`（英文），两者顶部互相语言切换（`README.en.md`/`README.md`）；其余文档在 **`docs/zh/`** 与 **`docs/en/`** 两个平级目录，各 5 个同名文档（`CONTRIBUTING.md`/`gpi-architecture.md`/`gpi-new-cloud.md`/`gpi-optimizer-extension.md`/`gpi-enhancements-over-skypilot.md`）；`deploy/k8s/README.md` 等**代码配套说明保持单语原位**、不迁移不双语。
- **决策②（中文优先）**：中文为主文件、优先维护；英文随后同步。**`.github/CLA.md` 与 `.github/RELEASE_NOTES.md` 单文件逐句双语**（每句中文后紧跟对应英文）。
- **决策③（链接深度）**：根 README → `docs/zh|en/` 文档用 `docs/zh|en/` 前缀，仓库根资源（`examples/`、`openapi.json`、`LICENSE`、`deploy/k8s/README.md`）直接用文件名；`docs/zh|en` 树内互链用文件名、根级资源用 `../../`。
- **改动文件**：`git mv` 6 个中文文档 → `docs/zh/`（README 再 mv 到根 `README.md`）；`docs/en/README.md` → 根 `README.en.md`；新增 `docs/en/` 5 个英文翻译（架构/新云/优化器扩展/增强清单/CONTRIBUTING）+ 根 README 两份；`.github/CLA.md` 逐句双语、`.github/RELEASE_NOTES.md` 逐句双语；AGENTS.md 更新双语约定；MEMORY 升 v22。
- 未纳入双语：`AGENTS.md`、`aiagents/MEMORY.md`、`.github/PULL_REQUEST_TEMPLATE.md`、`deploy/k8s/README.md`。

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
| resources.ordered | 对标 SkyPilot `ordered`：failover 候选列表，组间按序、组内按 metric 排序 |
| 文档版本 | 版本号记录在 docs 内容中；变更记录放文档末尾 `## 版本记录` |
| 优化器入口 | launch/optimize/serve up/jobs/server API 均支持 `--optimizer`/`optimizer` 字段（`Resolve`） |
| K8s 部署 | `deploy/k8s/`，`kubectl apply -k deploy/k8s`；默认 redis 后端 |
| OpenAPI 在线查看 | 仓库根 `openapi.json` + GitLab 内建 viewer（无需 GitHub Pages） |
| License / CLA | MIT；AcmeStack CLA（`.github/CLA.md` + cla-assistant） |
| 沟通记录 | 每次沟通后追加到 `aiagents/MEMORY.md` |
| 平台 | Linux/macOS；无 Windows |
| 用户配置文件 | `$GPI_HOME/config.yaml`（默认 `~/.gpi/config.yaml`）+ 项目 `.gpi.yaml` 层叠（项目覆盖用户） |
| 云专项配置 | 各云自己包内定义 `Config` struct + `LoadConfig()`（`config.Load().Section(CloudName, &c)`），`internal/config` 云无关、新云零改动 |
| 两个 config | `internal/config`=文件配置（客户端启动偏好）；`internal/state` 的 `config` 表=运行时 KV（服务端共享，零消费者）。config.Load() 不读 state 表 |
| 数据库表结构 | 8 张表按实体拆分（PK + 索引列 + data JSON 列），完整结构见架构文档 §9.0 |
| 文档布局 | 根 `README.md`（中文）+ `README.en.md`（英文），互相语言切换；`docs/zh/` + `docs/en/` 各 5 个同名文档；`.github/CLA.md`/`RELEASE_NOTES.md` 单文件逐句双语（每句中文后紧跟英文）；`deploy/k8s/README.md` 等代码配套说明单语原位 |
