# Contributing to Gpi

Welcome to contributing to Gpi! This guide applies to all developers who wish to participate in development, including first-time contributors.

> This guide is organized based on the [AcmeStack Contributor Guide](https://acmestack.com/docs/contributing/guide/).

## Table of Contents

- [Contributing to gpi](#contributing-to-gpi)
  - [Table of Contents](#table-of-contents)
  - [Development Environment](#development-environment)
  - [Code Style](#code-style)
  - [Commit Conventions](#commit-conventions)
  - [Branches & Workflow](#branches--workflow)
  - [Pull Request Workflow](#pull-request-workflow)
  - [Testing](#testing)

## Development Environment

Requirements: Go ≥ 1.26 (see `go.mod`).

```bash
# Build the control-plane gpi and the node agent gpilet
make build

# Run all tests
make test

# Static analysis
make vet
```

During development, point `GPI_HOME` at a temporary state directory to avoid polluting the real environment:

```bash
export GPI_HOME=/tmp/gpi-dev
```

## Code Style

- Keep "zero unnecessary dependencies": prefer the Go standard library for cloud-side, SSH, and HTTP concerns.
- All exported types/functions must have doc comments (`go vet` checks this).
- Run `gofmt -l .` before committing to ensure consistent formatting.
- Follow existing code conventions (error handling, naming, package structure); see [docs/gpi-architecture.md](gpi-architecture.md).

## Commit Conventions

Follow [Conventional Commits](https://www.conventionalcommits.org/):

```
<type>(<scope>): <subject>

<body>
```

Common types:

- `feat:` new feature
- `fix:` bug fix
- `docs:` documentation
- `chore:` miscellaneous (build, CI, dependencies, etc.)
- `ci:` CI configuration
- `test:` tests
- `refactor:` refactoring

Examples:

```
feat(server): add configurable API response key case style
docs: add clickable links for referenced docs in README
```

## Branches & Workflow

- `main` is the primary branch; all PRs are merged into `main`.
- Feature development: branch off from `main` (`feat/xxx`, `fix/xxx`).
- Releases: tagging `v*` triggers the [Release workflow](../../.github/workflows/release.yml) to build and publish automatically.

## Pull Request Workflow

1. Fork this repository and create your branch.
2. Commit your changes (following the commit conventions).
3. Push the branch and open a PR against `main`.
4. **Sign the CLA**: your first PR will trigger a comment from the CLA assistant bot; reply with `I have read the CLA Document and I hereby sign the CLA` to sign, or reply `recheck` to re-check the signing status. The full CLA text is in [.github/CLA.md](../../.github/CLA.md), and the official instructions are in the [AcmeStack Contributor License Agreement](https://acmestack.com/docs/contributing/contributor-license-agreement/). Make sure `git config user.name` matches your GitHub username.
5. Make sure CI (build/vet/test + cross-compilation) passes.
6. Maintainers review and merge.

## Testing

- New features should come with unit tests (test files in the same package as the code under test, e.g. `internal/server/server_test.go`).
- Run the full test suite: `make test`.
- Changes involving the HTTP API should add handler-level tests (refer to `internal/server/server_test.go`).