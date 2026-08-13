# SkyPilot 能力差距清单（待创建 GitHub Issues）

在 https://github.com/acmestack/gpi/issues/new 创建以下 issue（逐个创建，或按需合并）。

## Issue 1: Managed Jobs 控制器 + 恢复策略（FAILOVER / EAGER_NEXT_REGION）

**标题**：`feat: managed jobs controller with recovery strategies (FAILOVER / EAGER_NEXT_REGION)`

SkyPilot 的 `sky/jobs/controller.py` 提供云端 managed job 调度：任务失败后按恢复策略在失败区域重启（`FAILOVER`）或直接跳下一区域（`EAGER_NEXT_REGION`，适合 spot 抢占）。gpi 当前 jobs 是本地 cron 触发，无区域级故障恢复。

## Issue 2: Serve 自动扩缩容（autoscaler）

**标题**：`feat: serve autoscaler based on load`

SkyPilot `sky/serve/autoscalers.py` 根据请求负载自动扩缩副本数。gpi `serve` 目前副本数固定。

## Issue 3: Serve Load Balancer + 负载均衡策略

**标题**：`feat: serve load balancer with configurable load balancing policies`

SkyPilot `sky/serve/load_balancer.py` + `load_balancing_policies.py` 在副本间分发流量。gpi serve 目前只记录 endpoints，无 LB。

## Issue 4: Spot 抢占自动恢复

**标题**：`feat: spot instance preemption auto-recovery`

SkyPilot `sky/jobs/recovery_strategy.py` 针对 spot 抢占自动换区重启。gpi 无此机制。

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
