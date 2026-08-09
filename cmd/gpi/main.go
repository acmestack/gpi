package main

import (
	"github.com/acmestack/gpi/internal/cli"

	// Register every cloud provider (aliyun + aws + ...) in one place.
	_ "github.com/acmestack/gpi/internal/cloud/imports"
)

func main() {
	cli.Execute()
}
