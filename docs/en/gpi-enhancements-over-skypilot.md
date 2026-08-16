# Gpi Capability Enhancements over SkyPilot

- **Doc version**: v1 (2026-08-08)
- **Scope**: Gpi follows the [SkyPilot](https://skypilot.readthedocs.io) model and implements multi-cloud compute scheduler in Go. This document specifically records the enhancements/differentiation that Gpi makes beyond the capabilities SkyPilot already has, for evaluation and evolution reference. The baseline capabilities (task YAML → Optimizer → Provisioner → setup/run, SkyServe, Sky Jobs, API server, etc.) are described in `gpi-architecture.md`.

---

## 1. Zero-SDK cloud integration

- **SkyPilot**: relies on the official Python SDK of each cloud provider (boto3, aliyun-python-sdk, etc.).
- **Gpi**: does not pull in any official SDK; it implements cloud APIs and signing directly with the Go standard library:
  - aliyun: HMAC-SHA1 signing (`internal/cloud/aliyun/sign.go`).
  - aws: SigV4 (AWS4-HMAC-SHA256) signing (`internal/cloud/aws/sign.go`).
  - Benefits: single binary with no runtime dependencies, small footprint, easy cross-compilation and offline deployment.

## 2. Per-task dynamic AK/SK

- **SkyPilot**: credentials come only from environment variables or local config files (`~/.aws`, `~/.aliyun`, etc.).
- **Gpi**: the top-level `credentials:` block in the task YAML (split per cloud: aws/aliyun) can dynamically supply AccessKey/Secret for each task:
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
  - If provided → this launch and subsequent down/stop/start reuse those credentials;
  - If not provided → falls back to the existing default loading from env/disk (`LoadCredentials`).
  - Credentials are persisted into the cluster state (`state.CloudCreds`), so lifecycle operations need not re-specify them.
  - Example: `examples/yaml/with-credentials.yaml`.

## 3. Unified tags/labels merge

- **SkyPilot**: instance tags and cluster-internal labels are distinct concepts, maintained separately.
- **Gpi**: treats tags and labels as essentially the same for the cloud (both are instance key-value labels) and handles them uniformly:
  - Built-in `gpi:cluster` / `gpi:cloud` + top-level `tags:` + `resources.labels:` are **merged** and written to the cloud instance (`LaunchSpec.Tags`);
  - On conflict, top-level `tags:` wins;
  - `resources.labels:` are not only written to the cloud instance, but also injected into all nodes via `ray start --labels='{"k":"v"}'` for Ray scheduling.

## 4. Automatic multi-node Ray cluster

- **SkyPilot**: multi-node orchestration relies on Ray's own `sky`/`ray` ecosystem configuration.
- **Gpi**: when `num_nodes>1`, launch automatically assigns node roles and bootstraps Ray:
  - The first node is the head (`ray start --head`, dashboard 8265); the rest are workers (`ray start --address=<head>:6379`);
  - setup runs in parallel on all nodes, run executes on the head;
  - `{{cluster.head_ip}}` / `{{cluster.num_workers}}` are injected into the task for composing distributed training parameters;
  - `gpi cluster status|nodes` shows the topology and roles.
  - Example: `examples/yaml/distributed-train.yaml`.

## 5. Single binary, dual form (CLI + Server)

- **SkyPilot**: the CLI and server are two entry points of the same Python package.
- **Gpi**: a single Go binary provides both the `gpi` CLI and the `gpi server` REST API + scheduler; state is uniformly persisted in local JSON (`~/.gpi`, overridable via `GPI_HOME`), with no database dependency.

## 6. gpilet node agent (skylet counterpart)

- **SkyPilot**: a skylet daemon resides on nodes to collect status and forward commands/logs.
- **Gpi**: adds a lightweight agent `gpilet` (`cmd/gpilet` + `internal/gpilet`), pure Go, zero dependency:
  - `gpilet serve --dir /var/lib/gpilet --interval 10`: a resident daemon that periodically collects node status (CPU usage, loadavg, memory, disk, GPU usage, whether Ray is running) and writes it to `status.json`;
  - `gpilet status`: one-shot output of the current status (JSON);
  - On `Launch`, the provisioner automatically uploads the gpilet binary to each node and starts it (if no local gpilet binary is found, it silently skips and provisioning is unaffected);
  - `gpi cluster nodes C --health`: reads each node's gpilet status over SSH and shows live health (cpu/load/mem/gpu/ray).
  - Positioning: a lightweight equivalent of skylet that provides a node-side data source for future autoscaling, health polling, and real-time resource reporting; there is currently no command/log proxy (the control plane uses direct SSH).
- **Kubernetes node image (counterpart of SkyPilot k8s images with skylet baked in)**: `Dockerfile.gpi-base` is based on `rayproject/ray` with the gpilet binary preinstalled at `/usr/local/bin/gpilet`; the default node image is `ghcr.io/acmestack/gpi-base:latest`; the pod start command auto-launches gpilet + Ray (head/worker), so no SSH upload is needed (complementary to the VM backend's SSH upload path).

## 7. Pluggable persistence (file / sqlite / mysql / redis)

- **SkyPilot**: state is stored in local files (JSON under `~/.sky/`).
- **Gpi**: abstracts `state.Backend`, selected via the `GPI_STATE_BACKEND` environment variable:
  - `file` (default, one JSON file per data type, compatible with `~/.gpi/*.json`);
  - `sqlite` (single-file database, path set via `GPI_STATE_SQLITE`);
  - `mysql` (data source set via `GPI_STATE_MYSQL_DSN`);
  - `redis` (connection set via `GPI_STATE_REDIS_ADDR/PASSWORD/DB`, one key per data type: `gpi:clusters`/`gpi:services`/`gpi:jobs`/`gpi:cluster_yaml`/`gpi:cluster_history`/`gpi:cluster_events`/`gpi:config`/`gpi:tokens`).
  - sqlite/mysql create one table per entity (`clusters`/`services`/`jobs`/`cluster_yaml`/`cluster_history`/`cluster_events`/`config`/`service_account_tokens`, aligned with the SkyPilot table schema): commonly used fields become explicit indexed columns, and the full entity is stored in a `data` JSON column. This eases deployment onto a shared database with concurrent reads/writes from multiple instances.

## 12. API token auth (service_account_tokens)

- **SkyPilot**: the API server uses JWT with sha256 hashes stored in the DB; the auth middleware checks revocation/rotation/expiry against the DB on every request.
- **Gpi**: `gpi server start --require-auth` enables Bearer auth; the plaintext token is returned only once at creation time, and only its sha256 hash is stored; supports expiry (`--expires-in`), revocation (`delete`), and rotation (`rotate`); `POST /api/v1/gpi/tokens` and `/healthz` are public so the first token can be bootstrapped. Aligned with SkyPilot's `service_account_tokens` table and auth semantics.

## 13. Middleware abstraction & OpenAPI/Swagger

- **SkyPilot**: FastAPI middleware stack (RBAC, Basic/Bearer auth, RequestID, CORS, SecurityHeaders, etc.) plus built-in OpenAPI/Swagger.
- **Gpi**: the `server.Middleware` interface (`Wrap(next)`) plus `AddMiddleware` for custom extensions, with built-in security headers/CORS/auth/request-id/logging; `--docs` provides `/swagger.json` (OpenAPI 3.0), `/docs` (Swagger UI), and `/redoc`, and the docs are publicly viewable. Implemented with the standard library, no framework needed.

## 8. Execution backend abstraction (SkyPilot backend layer counterpart)

- **SkyPilot**: the backend layer is split into `CloudVmRayBackend` / `LocalBackend` / `LocalDockerBackend` etc., responsible for "how a task runs".
- **Gpi**: `internal/backend` defines the `Backend` interface (Launch/RunTask/Exec/Down/Stop/Start) plus a `Manager` that dispatches based on `task.Backend`, with later dispatch on `cluster.Backend`:
  - `cloud` (default): cloud VM + Ray + gpilet (the former provisioner);
  - `existing`: attach to existing hosts, execute over SSH, no provisioning and no destruction of external hosts;
  - `docker`: executes in local Docker containers (volumes/envs/gpus configurable); down deletes the container, stop/start pause and resume it;
  - `local`: runs setup/run directly on the local machine.
  - Non-cloud backends skip the optimizer (no placement). Thinner than SkyPilot: it does not do multi-cluster abstraction and keeps single-backend dispatch.

## 9. Customizable API response (ResponseEncoder)

- **SkyPilot**: the REST response structure is fixed.
- **Gpi**: the `server.ResponseEncoder` interface unifies all responses, and teams can plug in their own:
  - Built-in `raw` (default, raw data / `{"error":...}`) and `envelope` (`{"code","message","data"}`, field names configurable);
  - Selected via the `GPI_RESPONSE_FORMAT` environment variable or `gpi server start --response-format`;
  - Any team custom formatter: implement the interface and register it via `SetResponseEncoder(...)`, with no handler changes.

## 10. Request ID end-to-end tracing

- **SkyPilot**: has no built-in request ID mechanism.
- **Gpi**: if the request header carries an upstream ID it is passed through; otherwise a 32-character random hexadecimal ID is generated; the same value is echoed back in both the response header and body:
  - header key defaults to `x-request-id`, configurable via `GPI_REQUEST_ID_HEADER` or `--request-id-header`;
  - body field defaults to `request_id`, orthogonal to the ResponseEncoder (injected into raw/envelope/custom alike).

## 11. Configurable response key style

- **SkyPilot**: the response JSON key style is fixed.
- **Gpi**: `GPI_API_RESPONSE_KEY_STYLE` / `--api-key-style` / `SetKeyStyle` choose `camel` (default, `numNodes`), `snake` (`num_nodes`), or `pascal` (`NumNodes`); applied recursively to every key in the response body, changing only the wire format without affecting handlers or internal models.

---

## Roadmap (future enhancements)

- Multi-instance GPU slicing (e.g., A100:8 across nodes).
- More pluggable targets/optimizers (latency, carbon, budget...) extended via the `optimizer.Metric`/`optimizer.Optimizer` interfaces, usable as standalone optimizers or as priority combinations within the `cost,latency` strategy.
- Spot bid price caps, autoscaling/health polling (SkyServe counterpart).
- Job log persistence and historical query.