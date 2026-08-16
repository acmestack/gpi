.PHONY: all gpi gpilet build generate openapi test e2e vet docker clean

all: build

build: generate gpi gpilet

# Regenerate internal/cloud/imports (scan for cloud packages) before building,
# so a newly added cloud is picked up automatically with no manual imports.
generate:
	go generate ./internal/cloud/imports

# Regenerate openapi.json (repo root) from the built-in OpenAPI spec. GitLab
# renders a repo-root openapi.json with its built-in viewer; commit the result.
openapi:
	go run ./cmd/gen-openapi

gpi:
	go build -o gpi ./cmd/gpi

gpilet:
	go build -o gpilet ./cmd/gpilet

test:
	go test ./...

# Run Kubernetes e2e tests against a real cluster (KUBECONFIG must point to
# a running cluster, e.g. kind). Excluded from `make test` via the e2e tag.
e2e:
	go test -tags e2e -count=1 -v ./internal/cloud/kubernetes/

vet:
	go vet ./...

# Build the gpi Docker image (multi-stage; see Dockerfile).
docker:
	docker build -t gpi:latest .

clean:
	rm -f gpi gpilet
