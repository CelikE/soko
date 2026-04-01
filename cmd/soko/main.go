// Package main is the entrypoint for the soko CLI.
package main

import (
	"os"

	"github.com/CelikE/soko/internal/cli"
)

// version is set via ldflags at build time.
var version = "dev"

func main() {
	if err := cli.NewRootCmd(version).Execute(); err != nil {
		os.Exit(1)
	}
}
