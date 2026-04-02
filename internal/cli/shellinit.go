package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

const shellInitScript = `# soko shell integration
# Add this to your shell profile (.bashrc, .zshrc, etc.):
#   eval "$(soko shell-init)"

sgo() { local dir; dir=$(soko go "$@") && cd "$dir"; }
s() { local dir; dir=$(soko cd "$@") && cd "$dir"; }
`

// newShellInitCmd creates the cobra command for soko shell-init.
func newShellInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "shell-init",
		Short: "Print shell wrapper functions for cd and go",
		Long: `Print shell functions that enable directory navigation with soko.

Add to your shell profile:
  eval "$(soko shell-init)"

This defines:
  s <name>   — jump to a repo by name (uses soko cd)
  sgo        — interactive repo picker (uses soko go)`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, _ = fmt.Fprint(cmd.OutOrStdout(), shellInitScript)
			return nil
		},
	}
}

// shellInitHint returns the hint message shown after first init.
func shellInitHint() string {
	return `To enable directory navigation (soko go, soko cd), add to your shell profile:
  eval "$(soko shell-init)"`
}

// shellNavHint returns the message shown when go/cd is used without the wrapper.
func shellNavHint() string {
	return `This command prints a path — to navigate, use the shell wrapper:
  eval "$(soko shell-init)"
  Then use: sgo (interactive) or s <name> (direct)`
}
