package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// newVersionCmd creates the cobra command for soko version.
func newVersionCmd(version string) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the soko version",
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "soko %s\n", version)
			return nil
		},
	}
}
