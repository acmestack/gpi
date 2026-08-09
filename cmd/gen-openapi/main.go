// Command gen-openapi regenerates docs/apis/openapi.json from the built-in
// OpenAPI spec. Run via `make openapi`; commit the resulting file so the
// GitHub Pages Swagger UI (https://<owner>.github.io/gpi/apis) stays current.
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
	const out = "docs/apis/openapi.json"
	if err := os.WriteFile(out, spec, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "write openapi:", err)
		os.Exit(1)
	}
	fmt.Printf("wrote %s (prefix %s)\n", out, prefix)
}
