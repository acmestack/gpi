# SkyPilot 能力差距清单（待创建 GitHub Issues）

在 https://github.com/acmestack/gpi/issues/new 创建以下 issue（逐个创建，或按需合并）。

## Issue 1: Managed Jobs 控制器 + 恢复策略（FAILOVER / EAGER_NEXT_REGION）

**标题**：`feat: managed jobs controller with recovery strategies (FAILOVER / EAGER_NEXT_REGION)`

SkyPilot 的 `sky/jobs/controller.py` 提供云端 managed job 调度：任务失败后按恢复策略在失败区域重启（`FAILOVER`）或直接跳下一区域（`EAGER_NEXT_REGION`，适合 spot 抢占）。gpi 当前 jobs 是本地 cron 触发，无区域级故障恢复。managed jobs 的定位是可**规模化扩展**：轻松管理成百上千 job、`--detach-run --async` 批量提交，gpi 需对齐这一扩展能力。

## Issue 2: Serve 自动扩缩容（autoscaler）

**标题**：`feat: serve autoscaler based on load`

SkyPilot `sky/serve/autoscalers.py` 根据请求负载自动扩缩副本数。gpi `serve` 目前副本数固定。

## Issue 3: Serve Load Balancer + 负载均衡策略

**标题**：`feat: serve load balancer with configurable load balancing policies`

SkyPilot `sky/serve/load_balancer.py` + `load_balancing_policies.py` 在副本间分发流量。gpi serve 目前只记录 endpoints，无 LB。

## Issue 4: Spot 抢占自动恢复

**标题**：`feat: spot instance preemption auto-recovery`

SkyPilot `sky/jobs/recovery_strategy.py` 针对 spot 抢占自动换区重启。gpi 无此机制。注意 SkyPilot 的区分：`sky jobs launch --use-spot` 抢占**自动恢复**，而 `sky launch --use-spot` 抢占**不自动处理**（仅适合交互式开发）；gpi 需明确 spot 恢复能力归属 managed jobs。

## Issue 5: Storage / Volumes（对象存储挂载）

**标题**：`feat: storage & volumes (object-store mounts)`

SkyPilot `sky/storage.py`、`sky/volumes/` 支持云对象存储（S3/GCS）与卷挂载到任务。gpi 无。

## Issue 6: ssh_node_pools / workspaces

**标题**：`feat: ssh node pools and workspaces`

SkyPilot `sky/ssh_node_pools/`、`sky/workspaces/` 提供 SSH 节点池与工作区能力。gpi 无。

## Issue 7: Dashboard（Web UI）

**标题**：`feat: web dashboard`

SkyPilot 提供 `sky/dashboard/` 可视化界面。gpi 仅 REST API。

## Issue 8: 多租户（admin_policy / users / usage）

**标题**：`feat: multi-tenancy (admin policy, users, usage accounting)`

SkyPilot `sky/admin_policy.py`、`sky/users/`、`sky/usage/` 提供管理策略、用户与用量统计。gpi 无。

## Issue 9: Cloud Stores（云存储抽象）

**标题**：`feat: cloud stores abstraction`

SkyPilot `sky/cloud_stores.py` 抽象云厂商对象存储。gpi 无。

## Issue 10: resources 预算上限（max_hourly_cost）

**标题**：`feat: per-task max hourly cost budget`

SkyPilot `resources.max_hourly_cost` 约束单机小时成本上限。gpi optimizer 已有 budget 思路但未做成任务级字段。

## Issue 11: resources 多候选（any_of / ordered）

**标题**：`feat: multi-candidate resources (any_of / ordered)`

SkyPilot `Task.resources` 是 List，支持 `any_of`（候选集，optimizer 决定 failover 顺序）与 `ordered`（按序 failover），外层字段作为默认值。gpi 当前为单数 `Resources`，属重大设计变更。

## Issue 12: Pipeline 与 Job Groups（多任务编排）

**标题**：`feat: pipeline and job groups in jobs`

SkyPilot `sky jobs launch` 支持三种任务形态：单个 task、**pipeline**（顺序执行的多 task，可用不同资源，task 间通过共享 file_mount 传数据，如 train→eval）、**job group**（`execution: parallel`，并行执行且可互相通信的异构 task，如 RL 场景）。gpi `jobs submit` 仅支持单 task，无多任务编排。

## Issue 13: 异步执行 + detach 运行

**标题**：`feat: async execution and detach-run`

SkyPilot 所有 CLI/API 都是异步请求（返回 request ID），`Ctrl+C` 后任务继续在后台执行（`sky api logs <id>`/`sky api cancel <id>` 管理）；`--detach-run` 提交后立即返回、`--async` 批量并发提交数百 job。gpi `launch`/`jobs run` 前台阻塞执行，无后台任务管理。

## Issue 14: Managed Jobs 临时集群生命周期

**标题**：`feat: ephemeral cluster lifecycle for managed jobs`

SkyPilot `sky jobs launch` 为每个 job 创建**专属临时集群**，任务结束即自动清理资源；而 `sky launch` 的集群长生命周期、可手动复用（`-c`）。gpi `jobs` 目前与 `launch` 一样复用 `job-<name>` 常驻集群、运行后不清理，缺少 managed-job 的"用完即销毁"语义。

## Issue 15: workdir 同步差异（.git 处理）

**标题**：`feat: align workdir sync semantics (.git exclusion) for jobs`

SkyPilot 对 workdir 的 `.git` 同步策略不一致：`sky launch` 默认同步 `.git`，`sky jobs launch` 默认排除 `.git`（实验追踪类工具依赖时需显式 `file_mounts: ~/sky_workdir/.git: .git` 补回）。gpi 需定义并文档化 jobs 与 launch 的 workdir/`.git` 同步行为，避免语义含糊。
