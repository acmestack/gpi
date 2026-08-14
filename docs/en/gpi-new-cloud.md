# Adding a New Cloud (How to Add a New Cloud)

- **Doc version**: v15 (2026-08-13)
- **Applies to**: Gpi (`github.com/acmestack/gpi`)

## Overview: one cloud = one package + one struct

A new cloud adds only **1 Go package** to the source tree (`internal/cloud/<cloud>/`), and all registration is concentrated in that package's single `provider.go` `init()`. **A single `Provider` struct implements both interfaces**: `cloud.Provider` (instance lifecycle) + `catalog.Source` (spec/price metadata; `catalog` lives in `internal/cloud/catalog`, corresponding to SkyPilot `sky/catalogs`), and `cloud.Register` automatically detects and registers the metadata source. **There is no static spec/price data** — all metadata is fetched live. No changes to the core logic of optimizer / server / cli are needed.

| Interface | Responsibility | Methods (one struct fully implements) |
|------|------|----------|
| `cloud.Provider` | Create/query/delete instances, VPC/SG/KeyPair/images | `Name`/`Regions`/`RunInstances`/`DescribeInstances`/`Start`/`Stop`/`Terminate`/`GetPublicIP`/`DescribeZones`/`CreateKeyPair`/`DeleteKeyPair`/`CreateSecurityGroup`/`AuthorizeSecurityGroup`/`CreateVPC`/`CreateVSwitch`/`ListVSwitches`/`GetImage` |
| `catalog.Source` | Metadata: specs + prices, both fetched live | `Cloud`/`SpecsTTL`/`PriceTTL`/`FetchSpecs`/`FetchPrices` (`Regions` reuses the Provider's) |

> Both `Cloud()` and `Name()` return the cloud name (different method names, same semantics); the `Regions(ctx)` signature is identical across the two interfaces, so implement it once.

## Steps

### 1. Metadata methods (`metadata.go`, attached to the Provider)

Implement the `catalog.Source` methods on the Provider (**no separate `source` struct needed**):

```go
package foo

// metadataClient returns the client used to fetch metadata (specs/prices); when
// bound it uses the Provider credentials, otherwise it falls back to
// env/default config.
func (p Provider) metadataClient(region string) (*Client, error) { ... }

// The following methods make the Provider also satisfy catalog.Source.
func (p Provider) Cloud() string { return "foo" }

func (p Provider) SpecsTTL() time.Duration { return 24 * time.Hour }  // specs are low-frequency
func (p Provider) PriceTTL() time.Duration { return 10 * time.Minute } // prices are high-frequency

func (p Provider) FetchSpecs(ctx context.Context, region string) ([]*catalog.Instance, error) {
	// c.DescribeInstanceTypes(...) converted to []*catalog.Instance
	// Each Instance: Cloud/Region/InstanceType/VCPUs/MemoryGiB/MaxDiskGiB/Accelerators
	// (prices are not on Instance — they are provided separately by FetchPrices)
}

func (p Provider) FetchPrices(ctx context.Context, region string, types []string) (map[string]cloud.Price, error) {
	// on-demand + spot, returns map[instanceType]cloud.Price
	// concurrent queries recommended (see aliyun/aws priceWorkers); skip failures
	// of individual models without aborting
}
```

### 2. Provider (`internal/cloud/<cloud>/`)

Create a new package directory, modeled on the existing `internal/cloud/aliyun/` implementation:

- **`provider.go`**: implements the `cloud.Provider` interface and registers in the **single `init()`** — **just the one line `cloud.Register(Provider{})`** (internally a type assertion automatically registers providers that satisfy `catalog.Source` as metadata sources):

```go
package foo

import (
	"github.com/acmestack/gpi/internal/cloud"
)

func init() {
	cloud.Register(Provider{})
	cloud.RegisterFactory("foo", func(creds *cloud.Credentials) (cloud.Provider, error) {
		return NewProvider(&Credentials{
			AccessKeyID:     creds.AccessKeyID,
			AccessKeySecret: creds.SecretAccessKey,
			Region:          creds.Region,
		}), nil
	})
}
```

- **`config.go` (optional)**: this cloud's **cloud-specific config** (network reuse, etc.) is defined by this package itself and placed here — **`internal/config` is cloud-agnostic**, and it is decoded via `Section(cloudName, &cfg)`:

```go
package foo

import "github.com/acmestack/gpi/internal/config"

// Config is the "foo" section of the gpi user config.
type Config struct {
	VPCID string `yaml:"vpc_id"`
	// ...
}

// LoadConfig returns the merged "foo" config section, or nil when unset.
func LoadConfig() *Config {
	var c Config
	if err := config.Load().Section(CloudName, &c); err != nil {
		return nil
	}
	return &c
}
```

  - The `CloudName` constant is already defined in `provider.go` (`const CloudName = "foo"`); the key for `Section` matches the cloud section name in the user config file.
  - When the provider needs it internally, just call `LoadConfig()`; **a new cloud does not need to change any code in `internal/config`**.

- **`client.go` / `sign.go`**: request signing and calling for the cloud API (aliyun uses HMAC-SHA1, aws uses SigV4; implement per the new cloud's protocol).
- **`pricing.go`**: the cloud API price-query methods (`DescribeOnDemandPrice` / `DescribeSpotPriceHistory`), called by `Provider.FetchPrices`.

### 3. Auto-generated aggregation package at build time (zero manual work)

A new cloud needs **no manual import edits and no separate command** — the aggregation package is generated automatically at build time:

- The `build` target in the `Makefile` first runs `go generate ./internal/cloud/imports`, then compiles `gpi`/`gpilet`.
- The generator (`internal/cloud/imports/gen.go`) scans `internal/cloud/` for every subpackage containing `cloud.Register(`, and rewrites the blank-import list in `imports.go`.
- Therefore, **once the new cloud's package is created, `make build` picks it up automatically**; CI uses `make build`, so it takes effect there too.
- To trigger generation separately: `go generate ./internal/cloud/imports`.

> The `gpi` binary and all tests uniformly import `_ "github.com/acmestack/gpi/internal/cloud/imports"`; that package's contents are maintained automatically at build time, so no other files change when a new cloud is added.

### 4. Verification

```bash
go build ./... && go vet ./... && go test ./...
# cross-platform
GOOS=linux GOARCH=amd64 go build ./cmd/gpi
GOOS=darwin GOARCH=arm64 go build ./cmd/gpi
# real test (requires credentials for that cloud)
gpi optimize examples/yaml/train.yaml --cloud foo --region region-a
```

## Design notes

- **Why does one struct implement two interfaces?** `cloud.Provider` manages "how cloud instances are used", `catalog.Source` manages "where cloud metadata comes from" — the two responsibilities are separated and the interfaces are independent; but the implementer is usually the same cloud client, so one struct implements both, and at registration `cloud.Register`'s type assertion automatically routes it — **a new cloud only writes one struct + one set of methods**.
- **Why is the spec/price contract in cloud instead of a separate package?** Metadata comes from cloud APIs, and the implementer knows best how to fetch it; the contract (`Source` interface + `Instance`/`Price` types + registry) belongs to the same "cloud" domain as the Provider, kept together in `internal/cloud/catalog` (corresponding to SkyPilot `sky/catalogs`), so a new cloud only deals with one package. The runtime TTL cache lives separately in `internal/metacache` (kept heavy—to avoid bloating cloud/catalog).
- **How does the TTL cache work?** `metacache.Cache` caches specs/prices per (cloud, region); `SpecsTTL()`/`PriceTTL()` determine each cloud's refresh frequency; on fetch failure the old data is kept (stale-while-error), and `PricesForced` bypasses the TTL to force a refresh (used before `gpi launch` confirmation).
- **Why the aggregation package `internal/cloud/imports`?** In Go, a package must be imported for its `init()` to run; there is no import-free way. The aggregation package centralizes the "blank imports of all clouds" into a single file, auto-generated by `gen.go`, wired into the `make build` build prerequisites — a new cloud only needs to create its package, and it is **picked up automatically at build time, zero manual work**.
- **How does the Optimizer consume metadata?** The built-in `cost`/`time` optimizers and strategies such as `cost,time` read specs through the `optimizer.Meta` accessor and match them against task resources, then **fetch prices concurrently** for the candidates (candidate truncation + fallback on missing prices, see the architecture doc). Once a new cloud implements `Provider.FetchSpecs/FetchPrices` it is covered automatically, with no optimizer changes.
- **Credential sources**: the provider's `NewClient` is responsible for loading from env (`FOO_ACCESS_KEY_ID`/`FOO_SECRET` or the cloud's default config file); task-level `credentials:` is injected through `cloud.RegisterFactory`. `task.Credentials` is a **cloud-agnostic generic map** (`credentials: { <cloud>: { access_key_id, secret_access_key, region } }`), reused directly by new clouds, with no need to add any types or switch to the task package.
- **Where does cloud-specific config live?** In **each cloud's own package** (`internal/cloud/<cloud>/config.go`); `internal/config` only does cloud-agnostic loading/layered merging. This way adding a new cloud does **not change `internal/config`** — which is exactly why the `cloud.Provider` interface does not expose a `Config()` method: an interface method cannot return different types per cloud (only `any`, losing type safety), and the cloud section is consumed only by the provider itself anyway.

## Roadmap

- GPU memory-variant matching, multi-instance sharding across cards (A100:8 across nodes).
- More pluggable optimizers (latency, carbon emissions, budget…) extended via the `optimizer.Optimizer` interface.

## Version history

- **v14 (2026-08-13)**: Generalized task-level `credentials:` — `task.Credentials` changed to a cloud-agnostic generic map (`credentials: { <cloud>: { access_key_id, secret_access_key, region } }`), so new clouds reuse it directly without changing the task package; aliyun's old field `access_key_secret` remains compatible.
- **v13 (2026-08-13)**: Added cloud-specific config (`config.go` + `config.Load().Section(CloudName, &cfg)`); clarified that `internal/config` is cloud-agnostic and that the interface does not add `Config()`.
- **v12 (2026-08-09)**: Removed the "change rules" line from the doc (rules consolidated into `AGENTS.md`); added a version-change record section.
- **v11 (2026-08-09)**: `--optimizer` now supports strategies (`cost,time` etc.); built-in optimizer descriptions updated to cost/time.
- **v10 (2026-08-09)**: Added the built-in `time` optimizer; supplemented the explanation of how the optimizer consumes metadata.
- **v9 (2026-08-09)**: Finalized the split of the metadata contract and the TTL cache runtime — contract in `internal/cloud/catalog`, Cache in `internal/metacache`.
- **v8 (2026-08-09)**: Doc version-number rules consolidated into the project `AGENTS.md` (long-lived doc version numbers are recorded in the content).
- **v7 (2026-08-09)**: Adding a new cloud only needs **one struct** — `Provider` implements both `cloud.Provider` + `catalog.Source`, and `cloud.Register`'s type assertion auto-registers the metadata source; removed the separate `source` struct.
- **v6 (2026-08-09)**: Added Redis state backend and synced the Dockerfile notes.
- **v5 (2026-08-09)**: Metadata fully dynamic — implemented `catalog.Source` (live spec/price fetching), no static data.
- **v4 (2026-08-08)**: Aggregation package generation wired into the `make build` prerequisites.
- **v3 (2026-08-08)**: Aggregation package `internal/cloud/imports` auto-generated (`gen.go`).
- **v2 (2026-08-08)**: Registration consolidated into a single entry point (`cloud.Register`/`RegisterFactory`/`catalog.Register`).
- **v1 (2026-08-08)**: Created this guide.
