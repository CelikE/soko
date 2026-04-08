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
	// Only use custom git_path if the binary is a valid executable.
	if cfg, err := config.Load(); err == nil && cfg.GitPath != "" {
		if config.ValidateGitPath(cfg.GitPath) == nil {
			git.SetBinary(cfg.GitPath)
		}
	}

	if err := cli.NewRootCmd(version).Execute(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "  ✗ %s\n", err)
		os.Exit(1)
	}
}
