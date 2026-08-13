# Gpi on Kubernetes

在 Kubernetes 上部署 gpi 控制面（REST API + job scheduler）。

## 快速开始

```bash
# 一键部署（kustomize 自动套用命名空间与全部资源）
kubectl apply -k deploy/k8s

# 查看状态
kubectl -n gpi get pods,svc
```

或者逐个应用：

```bash
kubectl apply -f deploy/k8s/namespace.yaml
kubectl apply -f deploy/k8s/configmap.yaml
kubectl apply -f deploy/k8s/redis.yaml      # redis 后端（推荐）
kubectl apply -f deploy/k8s/deployment.yaml
kubectl apply -f deploy/k8s/service.yaml
```

## 访问

```bash
# 端口转发到本地
kubectl -n gpi port-forward svc/gpi 8080:8080

# 健康检查
curl http://localhost:8080/healthz

# API 文档（最新规范在仓库根 openapi.json，GitLab 内建在线渲染）
open https://github.com/acmestack/gpi/blob/main/openapi.json
```

Deployment 默认**不开启** `--docs`（Swagger UI 不随服务暴露）。最新 OpenAPI 规范在仓库根 `openapi.json`（`make openapi` 重新生成），GitLab 打开即得交互式 UI；需要交互式 UI 时粘贴到 [Swagger Editor](https://editor.swagger.io)。如需在服务上提供 `/swagger.json`，在 `deployment.yaml` 的 `args` 中加回 `--docs`。生产环境可用 Ingress / LoadBalancer 暴露 Service。

## 配置

- **ConfigMap**（`configmap.yaml`）：`GPI_STATE_BACKEND=redis`（共享多副本状态）、`GPI_API_PREFIX=/api/v1/gpi`、key style 等。
- **后端选择**：
  - **redis（默认）**：`redis.yaml` 提供单节点 Redis，适合多副本 gpi。生产建议改用托管 Redis 或带持久化的 Redis StatefulSet。
  - **sqlite（单副本）**：改 `configmap.yaml` 为 `sqlite`，并启用 `pvc.yaml` + `deployment.yaml` 中 PVC 相关注释（见文件内说明）。
- **镜像**：默认 `ghcr.io/acmestack/gpi:latest`（Release workflow 构建推送）。可替换为你自己的镜像仓库与 tag。
- **认证**：如需 `--require-auth`，先 `gpi server token create` 生成令牌，通过 Secret 注入，并在 `deployment.yaml` 的 `args` 追加 `--require-auth`。

## 镜像

由 `.github/workflows/release.yml` 在打 `v*` tag 时构建并推送到 `ghcr.io/acmestack/gpi`（`latest` + tag）。
