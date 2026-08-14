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
  <strong>Gpi multi-cloud compute scheduling (gpi = "ji-pai", a homophone of the Chinese for "chicken fried")</strong>
  <br/>
  <em>Manage all your AI compute</em>
</p>

<p align="center">
  <strong><a href="README.md">🌐 中文</a></strong>
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
  Implemented in <code>Go</code> · module <code>github.com/acmestack/gpi</code> · CLI <code>gpi</code> (homophone of "ji-pai") · supported platforms <code>Linux</code> / <code>macOS</code>
</p>

## Overview

Gpi is a lightweight, multi-cloud, self-hostable compute scheduler. Declare your resources and run commands in a task YAML, and it automatically handles price-based selection, cross-cloud provisioning, environment preparation, and task execution.

- **Tasks as code**: a single YAML declares resources / setup / run, runnable on any supported cloud.
- **Price-driven scheduling**: performs resource matching and price comparison across all registered clouds and outputs the optimal placement plan.
- **Multi-cloud provisioning**: supports aliyun + aws out of the box (zero official SDK; signatures implemented with the standard library).
- **Serving**: multi-replica deployment with second-level scaling and built-in health checks.
- **Scheduled jobs**: cron scheduling with automatic retry on failure.
- **Multiple execution backends**: cloud VMs, attaching existing hosts, local Docker, and running directly on the local machine.
- **Pluggable**: state storage (file / sqlite / mysql / redis) and API response formats can all be configured as needed.
- **Extensible**: [add a new cloud](docs/en/gpi-new-cloud.md) · [extend the Optimizer](docs/en/gpi-optimizer-extension.md)

> For a comparison with SkyPilot and the enhancement differences, see [docs/gpi-enhancements-over-skypilot.md](docs/en/gpi-enhancements-over-skypilot.md).
>
> For the architecture diagram, see [docs/gpi-architecture.md](docs/en/gpi-architecture.md).

## Getting started

Build the two binaries (`gpi` control plane + `gpilet` node agent):

```bash
make build
```

### Gpi in 1 minute

Write the following task to `my_task.yaml`:

```yaml
name: mytrain

resources:
  accelerators: A100:1   # 1x NVIDIA A100 GPU
  cpus: 8+
  memory: 32+

num_nodes: 1             # number of nodes (>1 automatically forms a Ray cluster)

# setup commands run before the task starts
setup: |
  pip install -r requirements.txt

# the task's main command
run: |
  python train.py --epochs 1
```

First, just inspect the scheduling plan (cross-cloud price comparison, no actual deployment):

```bash
./gpi optimize my_task.yaml
```

Once you confirm, deploy for real (requires cloud credentials; see below):

```bash
./gpi launch my_task.yaml -y
```

Gpi automatically: picks the cheapest viable cloud/region → provisions instances → uploads the workdir → runs setup → runs the task and streams the logs.

### Cloud credentials

```bash
# Alibaba Cloud
export ALIBABA_CLOUD_ACCESS_KEY_ID=... ALIBABA_CLOUD_ACCESS_KEY_SECRET=...
# or ~/.aliyun/config.json

# AWS
export AWS_ACCESS_KEY_ID=... AWS_SECRET_ACCESS_KEY=... AWS_REGION=...
# or ~/.aws/credentials
```

You can also specify AK/SK dynamically per task in a `credentials:` block in the task YAML (see [examples/yaml/with-credentials.yaml](../examples/yaml/with-credentials.yaml)).

## Commands

```
gpi launch TASK.yaml       schedule + provision + run
gpi optimize TASK.yaml     show the placement plan only (no provisioning)
                           optional --optimizer cost|time|cost,time (optimizer or policy)
gpi status                 cluster list
gpi cluster status|nodes C  cluster topology / node details / real-time health
gpi cluster yaml|history|events C  cluster YAML snapshot / launch history / lifecycle events
gpi exec C -- CMD          run a command on the head node
gpi down/stop/start C      lifecycle management
gpi serve up S.yaml        serving deployment
gpi jobs submit T.yaml     register a scheduled job
gpi server start           start the REST API + scheduler
gpi server token create|list|revoke|rotate   API token management
```

## Features

### Multi-node Ray cluster

When `num_nodes>1`, a head/worker Ray cluster is formed automatically (head `ray start --head`, workers join); setup runs in parallel on all nodes, and run executes on the head. Inside the task you can inject distributed parameters using `{{cluster.head_ip}}` and `{{cluster.num_workers}}`; see [examples/yaml/distributed-train.yaml](../examples/yaml/distributed-train.yaml).

- Cloud instance tags: top-level `tags:` and `resources.labels:` are merged onto the cloud instance (`tags:` wins on conflict), and include the built-in `gpi:cluster`/`gpi:cloud` tags.
- Ray node labels: in addition to being written to the cloud instance, `resources.labels:` are injected into every node via `ray start --labels`.
- `gpi cluster status C` shows topology, labels and tags.

### Node agent (gpilet)

`gpilet` is a lightweight agent that runs on every node (pure Go, zero dependencies):

- `gpilet serve --dir /var/lib/gpilet --interval 10`: continuously collects CPU/memory/disk/GPU/Ray status and writes it to `status.json`.
- `gpi cluster nodes C --health`: reads real-time health of each node over SSH.
- At launch, if the `gpilet` binary sits next to `gpi` (or at `GPI_CSLET` / `$GPI_HOME/bin/gpilet`) it is uploaded and started automatically.

### Execution backends

The top-level `backend:` field in the task YAML selects the execution method; `cloud` is the default (cloud VM):

```yaml
backend: existing    # attach to an existing host, run setup/run over SSH (requires ssh: block)
ssh:
  host: 1.2.3.4
  user: root
  key: ~/.ssh/id_rsa

backend: docker      # run in a local Docker container (requires docker: block)
docker:
  image: pytorch/pytorch:2.1.0
  gpus: 1

backend: local       # run setup/run directly on the local machine
```

Examples: [examples/yaml/existing-cluster.yaml](../examples/yaml/existing-cluster.yaml), [examples/yaml/docker-task.yaml](../examples/yaml/docker-task.yaml), [examples/yaml/local-task.yaml](../examples/yaml/local-task.yaml).

### Persistence (pluggable backend)

File is the default, but sqlite / mysql / redis are also available, configured via environment variables:

```bash
GPI_STATE_BACKEND=file                  # default: one JSON file per data type under ~/.gpi/state.json
GPI_STATE_BACKEND=sqlite                # single-file database; GPI_STATE_SQLITE sets the path
GPI_STATE_BACKEND=mysql GPI_STATE_MYSQL_DSN="user:pass@tcp(host:3306)/gpi"
GPI_STATE_BACKEND=redis GPI_STATE_REDIS_ADDR=localhost:6379

# cluster snapshot/history/events (aligned with SkyPilot table schema)
# gpi cluster yaml|history|events CLUSTER
```

### Serving deployment and scheduled jobs

```bash
# serving deployment (multi-replica)
./gpi serve up examples/yaml/llm-service.yaml -y
./gpi serve status

# register a scheduled job (cron + retry on failure)
./gpi jobs submit examples/yaml/nightly-benchmark.yaml --schedule "@daily" --retries 2
```

### REST API

```bash
./gpi server start --port 8080
```

- Customizable response format: `raw` by default, switchable to `envelope` (`{code,message,data}`) or a team-custom Encoder (`--response-format`).
- End-to-end request ID: forwarded or generated from the request header, written back to both header and body (key defaults to `x-request-id`, configurable via `--request-id-header`).
- Response key style: camel by default, also snake / pascal (`--api-key-style`).
- API authentication: `--require-auth` enables Bearer token auth; generate a token first with `gpi server token create` (requires one HTTP bootstrap), then send `Authorization: Bearer <token>`, with support for expiry/revocation/rotation (`gpi server token list|revoke|rotate`).
- Extensible middleware: the `server.Middleware` interface + `AddMiddleware` for customization (auth/rate limiting/tracing, etc.); built-in security headers, CORS (`--enable-cors`), request-id, and logging middleware.
- OpenAPI/Swagger: `--docs` enables `/swagger.json`, `/docs` (Swagger UI), and `/redoc`; the latest spec lives at the repo root in [openapi.json](../openapi.json) (rendered online by GitLab).

### Deploying to Kubernetes

Full K8s deployment resources are provided (namespace / configmap / deployment / service / redis backend / PVC):

```bash
kubectl apply -k deploy/k8s
kubectl -n gpi port-forward svc/gpi 8080:8080
```

See [deploy/k8s/README.md](../deploy/k8s/README.md) for details. Images are built by the Release workflow and pushed to `ghcr.io/acmestack/gpi`.

## Examples

Task YAMLs (`examples/yaml/`, hand-written task files) with their matching HTTP API request bodies (`examples/json/`, where `{scene}-launch.json` is the YAML-string form for `/clusters/{name}/launch` and `{scene}-task.json` is the Task-struct form for `/tasks/{name}/launch`):

- [examples/yaml/train.yaml](../examples/yaml/train.yaml) — single-node training · [obj.json](../examples/json/train-obj.json) · [yamlstr.json](../examples/json/train-yamlstr.json)
- [examples/yaml/aws-train.yaml](../examples/yaml/aws-train.yaml) — targeting AWS
- [examples/yaml/ordered-failover.yaml](../examples/yaml/ordered-failover.yaml) — `resources.ordered` sequential failover (AWS → Alibaba Cloud)
- [examples/yaml/distributed-train.yaml](../examples/yaml/distributed-train.yaml) — multi-node Ray distributed training
- [examples/yaml/llm-service.yaml](../examples/yaml/llm-service.yaml) — LLM serving deployment
- [examples/yaml/nightly-benchmark.yaml](../examples/yaml/nightly-benchmark.yaml) — scheduled job
- [examples/yaml/with-credentials.yaml](../examples/yaml/with-credentials.yaml) — dynamic AK/SK
- [examples/yaml/existing-cluster.yaml](../examples/yaml/existing-cluster.yaml) — attach to an existing host
- [examples/yaml/docker-task.yaml](../examples/yaml/docker-task.yaml) — run in Docker
- [examples/yaml/local-task.yaml](../examples/yaml/local-task.yaml) — run directly on the local machine

## Learn more

- [docs/gpi-architecture.md](docs/en/gpi-architecture.md) — architecture design document (version number recorded in the content)
- [docs/gpi-new-cloud.md](docs/en/gpi-new-cloud.md) — **guide to adding a new cloud** (how to add a new cloud provider)
- [docs/gpi-optimizer-extension.md](docs/en/gpi-optimizer-extension.md) — **guide to extending the placement optimizer** (Metric / Optimizer / policy)
- [docs/gpi-enhancements-over-skypilot.md](docs/en/gpi-enhancements-over-skypilot.md) — list of capability enhancements over SkyPilot
- `gpi --help` / `gpi <command> --help` — command help

## Contributing

Issues and PRs are welcome. See [CONTRIBUTING](docs/en/CONTRIBUTING.md).

## License

[MIT](../LICENSE) © [AcmeStack](https://acmestack.com)
