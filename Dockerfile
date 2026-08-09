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

# Cache dependencies first (layers change only when go.mod/go.sum do).
COPY go.mod go.sum ./
RUN go mod download

# Regenerate the cloud imports aggregate (scan for cloud.Register) then build
# both binaries; go:generate needs the module source, so copy the tree after
# the dependency layer.
COPY . .
RUN go generate ./internal/cloud/imports && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/gpi ./cmd/gpi && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/gpilet ./cmd/gpilet

# Stage 2: minimal runtime
FROM scratch
COPY --from=builder /out/gpi /usr/local/bin/gpi
COPY --from=builder /out/gpilet /usr/local/bin/gpilet
ENTRYPOINT ["gpi"]
