# Multi-stage build for the gpi + gpilet binaries.
#
# Build:
#   docker build -t gpi:latest .
#
# Run (server):
#   docker run --rm -it \
#     -e GPI_STATE_BACKEND=file \
#     -v "$HOME/.gpi:/root/.gpi" \
#     -p 8080:8080 \
#     gpi:latest server start

# Stage 1: build
FROM golang:1.26 AS builder
WORKDIR /src

# gpi version; release workflows pass the git tag (e.g. v0.0.1).
ARG VERSION=0.0.1

# Cache dependencies first (layers change only when go.mod/go.sum do).
COPY go.mod go.sum ./
RUN go mod download

# Regenerate the cloud imports aggregate (scan for cloud.Register) then build
# both binaries; go:generate needs the module source, so copy the tree after
# the dependency layer.
COPY . .
RUN V="${VERSION#v}" && \
    go generate ./internal/cloud/imports && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X github.com/acmestack/gpi/internal/buildinfo.Version=${V}" -o /out/gpi ./cmd/gpi && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X github.com/acmestack/gpi/internal/buildinfo.Version=${V}" -o /out/gpilet ./cmd/gpilet

# Stage 2: minimal runtime
FROM scratch
COPY --from=builder /out/gpi /usr/local/bin/gpi
COPY --from=builder /out/gpilet /usr/local/bin/gpilet
ENTRYPOINT ["gpi"]
