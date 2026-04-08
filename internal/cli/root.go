// Package cli defines the cobra command tree for soko.
package cli

import (
	"github.com/spf13/cobra"
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
	}

	cmd.PersistentFlags().Bool("json", false, "output in JSON format")

	cmd.AddCommand(newInitCmd())
	cmd.AddCommand(newScanCmd())
	cmd.AddCommand(newStatusCmd())
	cmd.AddCommand(newDiffCmd())
	cmd.AddCommand(newStashCmd())
	cmd.AddCommand(newListCmd())
	cmd.AddCommand(newRemoveCmd())
	cmd.AddCommand(newFetchCmd())
	cmd.AddCommand(newCdCmd())
	cmd.AddCommand(newGoCmd())
	cmd.AddCommand(newExecCmd())
	cmd.AddCommand(newOpenCmd())
	cmd.AddCommand(newTagCmd())
	cmd.AddCommand(newDocCmd())
	cmd.AddCommand(newConfigCmd())
	cmd.AddCommand(newShellInitCmd())
	cmd.AddCommand(newVersionCmd(version))

	return cmd
}
