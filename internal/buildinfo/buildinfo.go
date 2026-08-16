// Package buildinfo holds build-time metadata for gpi. Version is the single
// source of truth for the gpi version string, consumed by the CLI (gpi
// --version) and the OpenAPI/Swagger document.
//
// The default below is the latest released tag (without the leading "v").
// Release builds override it at link time with the current git tag, so the
// shipped binaries always report the exact released version:
//
//	go build -ldflags "-X github.com/acmestack/gpi/internal/buildinfo.Version=0.0.1" ./cmd/gpi
package buildinfo

// Version is the current gpi version. Keep it equal to the latest release tag
// (without the "v" prefix); CI/release workflows override it via -ldflags.
var Version = "0.0.2"
