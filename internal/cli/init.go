package cli

import (
	"github.com/spf13/cobra"
)

// newInitCmd creates the cobra command for soko init.
func newInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Register the current git repo in the global soko registry.",
		RunE: func(_ *cobra.Command, _ []string) error {
			return nil
		},
	}
}
