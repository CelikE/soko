// Package main is the entrypoint for the soko CLI.
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

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

	// Expand aliases: if the first argument matches a configured alias
	// and is NOT a built-in command, replace it with the expanded command.
	if cfg, err := config.Load(); err == nil && len(cfg.Aliases) > 0 && len(os.Args) > 1 {
		if expanded, ok := cfg.Aliases[os.Args[1]]; ok {
			rootCmd := cli.NewRootCmd(version)
			if !isBuiltinCommand(rootCmd, os.Args[1]) {
				expandedArgs := strings.Fields(expanded)
				os.Args = append([]string{os.Args[0]}, append(expandedArgs, os.Args[2:]...)...)
			}
		}
	}

	if err := cli.NewRootCmd(version).Execute(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "  ✗ %s\n", err)
		os.Exit(1)
	}
}

// isBuiltinCommand returns true if name matches a registered cobra command.
func isBuiltinCommand(root *cobra.Command, name string) bool {
	for _, c := range root.Commands() {
		if c.Name() == name {
			return true
		}
		for _, alias := range c.Aliases {
			if alias == name {
				return true
			}
		}
	}
	return false
}
