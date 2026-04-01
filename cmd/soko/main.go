// Package main is the entrypoint for the soko CLI.
package main

import (
	"os"

	"github.com/CelikE/soko/internal/cli"
)

func main() {
	if err := cli.NewRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}
