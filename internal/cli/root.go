// Package cli defines the cobra command tree for soko.
package cli

import (
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/CelikE/soko/internal/output"
)

// NewRootCmd creates and returns the root cobra command for soko.
func NewRootCmd(version string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "soko",
		Short: "All your repos, one command.",
		Long: `soko (倉庫) gives developers instant visibility and control across all
their git repositories from a single command. Register repos with soko init,
then run soko status from anywhere to see the state of every tracked repo.`,
		Example: `  soko init      Register the current repo
  soko status    Show status of all registered repos
  soko list      List all registered repos`,
		SilenceUsage:  true,
		SilenceErrors: true,
		// Read --quiet/--perf (and their SOKO_QUIET/SOKO_PERF fallbacks) once for
		// every subcommand and flip the output gates before any command runs.
		PersistentPreRunE: func(c *cobra.Command, _ []string) error {
			q, _ := c.Flags().GetBool("quiet")
			// An explicit --quiet (true or false) always wins; the env fallback
			// applies only when the flag was not provided.
			if !c.Flags().Changed("quiet") {
				q = isTruthyEnv(os.Getenv("SOKO_QUIET"))
			}
			output.SetQuiet(q)

			p, _ := c.Flags().GetBool("perf")
			// Same precedence: explicit --perf wins, SOKO_PERF only fills in when
			// the flag was not provided.
			if !c.Flags().Changed("perf") {
				p = isTruthyEnv(os.Getenv("SOKO_PERF"))
			}
			output.SetPerf(p)
			return nil
		},
	}

	cmd.PersistentFlags().Bool("json", false, "output in JSON format")
	cmd.PersistentFlags().BoolP("quiet", "q", false, "suppress hints, progress, and summary lines")
	cmd.PersistentFlags().Bool("perf", false, "report per-repo and aggregate timing after a parallel command")

	cmd.AddCommand(newInitCmd())
	cmd.AddCommand(newScanCmd())
	cmd.AddCommand(newDiscoverCmd())
	cmd.AddCommand(newStatusCmd())
	cmd.AddCommand(newRemotesCmd())
	cmd.AddCommand(newDiffCmd())
	cmd.AddCommand(newStashCmd())
	cmd.AddCommand(newListCmd())
	cmd.AddCommand(newRemoveCmd())
	cmd.AddCommand(newPruneCmd())
	cmd.AddCommand(newFetchCmd())
	cmd.AddCommand(newPullCmd())
	cmd.AddCommand(newSyncCmd())
	cmd.AddCommand(newCtxCmd())
	cmd.AddCommand(newWorktreeCmd())
	cmd.AddCommand(newCdCmd())
	cmd.AddCommand(newCleanCmd())
	cmd.AddCommand(newGoCmd())
	cmd.AddCommand(newExecCmd())
	cmd.AddCommand(newApplyCmd())
	cmd.AddCommand(newGrepCmd())
	cmd.AddCommand(newOpenCmd())
	cmd.AddCommand(newReportCmd())
	cmd.AddCommand(newStatsCmd())
	cmd.AddCommand(newHealthCmd())
	cmd.AddCommand(newTagCmd())
	cmd.AddCommand(newAnnotateCmd())
	cmd.AddCommand(newDocCmd())
	cmd.AddCommand(newAliasCmd())
	cmd.AddCommand(newConfigCmd())
	cmd.AddCommand(newShellInitCmd())
	cmd.AddCommand(newVersionCmd(version))

	return cmd
}

// isTruthyEnv reports whether an environment value means "on". It accepts
// 1/true/yes (case-insensitive); anything else — including a malformed value —
// is treated as off, so a typo can never crash soko.
func isTruthyEnv(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes":
		return true
	default:
		return false
	}
}
