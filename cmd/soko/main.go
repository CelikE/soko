// Package main is the entrypoint for the soko CLI.
package main

import (
	"fmt"
	"os"

	"github.com/CelikE/soko/internal/cli"
	"github.com/CelikE/soko/internal/config"
	"github.com/CelikE/soko/internal/git"
)

// version is set via ldflags at build time.
var version = "dev"

func main() {
	// Load config early to set git binary path before any commands run.
	if cfg, err := config.Load(); err == nil {
		git.SetBinary(cfg.GitBinary())
	}

	if err := cli.NewRootCmd(version).Execute(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "  ✗ %s\n", err)
		os.Exit(1)
	}
}
