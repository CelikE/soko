// Package cli defines the cobra command tree for soko.
package cli

import (
	"github.com/spf13/cobra"
)

// NewRootCmd creates and returns the root cobra command for soko.
func NewRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "soko",
		Short: "All your repos, one command.",
		Long: `soko (倉庫) gives developers instant visibility and control across all
their git repositories from a single command. Register repos with soko init,
then run soko status from anywhere to see the state of every tracked repo.`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	cmd.AddCommand(newInitCmd())
	cmd.AddCommand(newStatusCmd())

	return cmd
}
