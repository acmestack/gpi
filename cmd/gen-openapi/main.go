// Command gen-openapi regenerates openapi.json from the built-in OpenAPI spec.
// Run via `make openapi`; commit the resulting file. GitLab renders an
// openapi.json at the repository root with its built-in OpenAPI viewer, so the
// API docs are viewable interactively at the repo's web URL.
package main

import (
	"fmt"
	"os"

	"github.com/acmestack/gpi/internal/server"
)

const defaultPrefix = "/api/v1/gpi"

func main() {
	prefix := defaultPrefix
	if len(os.Args) > 1 {
		prefix = os.Args[1]
	}
	spec, err := server.OpenAPISpecJSON(prefix)
	if err != nil {
		fmt.Fprintln(os.Stderr, "render openapi:", err)
		os.Exit(1)
	}
	const out = "openapi.json"
	if err := os.WriteFile(out, spec, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "write openapi:", err)
		os.Exit(1)
	}
	fmt.Printf("wrote %s (prefix %s)\n", out, prefix)
}
