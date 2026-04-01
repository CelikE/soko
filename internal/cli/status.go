package cli

import (
	"github.com/spf13/cobra"
)

// newStatusCmd creates the cobra command for soko status.
func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show the status of all registered repos.",
		RunE: func(_ *cobra.Command, _ []string) error {
			return nil
		},
	}
}
