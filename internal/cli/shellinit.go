package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

const shellInitScript = `# soko shell integration
# Add this to your shell profile (.bashrc, .zshrc, etc.):
#   eval "$(soko shell-init)"

__soko_nav_hook() {
  local nav_file="${XDG_CONFIG_HOME:-$HOME/.config}/soko/.nav"
  if [[ -f "$nav_file" ]]; then
    local target
    target=$(cat "$nav_file")
    rm -f "$nav_file"
    if [[ -d "$target" ]]; then
      builtin cd "$target" || return
    fi
  fi
}

# Install hook based on shell.
if [[ -n "${ZSH_VERSION-}" ]]; then
  autoload -Uz add-zsh-hook
  add-zsh-hook precmd __soko_nav_hook
elif [[ -n "${BASH_VERSION-}" ]]; then
  PROMPT_COMMAND="__soko_nav_hook${PROMPT_COMMAND:+;$PROMPT_COMMAND}"
fi
`

// newShellInitCmd creates the cobra command for soko shell-init.
func newShellInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "shell-init",
		Short: "Print shell integration hook for navigation",
		Long: `Print a shell hook that enables soko go and soko cd to change
your working directory. The hook runs after each command and checks
for a navigation request.

Add to your shell profile:
  eval "$(soko shell-init)"

Then soko go and soko cd will navigate into repos directly.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, _ = fmt.Fprint(cmd.OutOrStdout(), shellInitScript)
			return nil
		},
	}
}

// navFilePath returns the path to the navigation file.
func navFilePath() string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "soko", ".nav")
}

// writeNavFile writes a path to the navigation file so the shell hook
// can pick it up and cd into it.
func writeNavFile(path string) error {
	navPath := navFilePath()
	if navPath == "" {
		return fmt.Errorf("could not determine nav file path")
	}
	dir := filepath.Dir(navPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating nav directory: %w", err)
	}
	return os.WriteFile(navPath, []byte(path), 0o644)
}

// shellInitHint returns the hint message shown after init.
func shellInitHint() string {
	return `To enable directory navigation (soko go, soko cd), add to your shell profile:
  eval "$(soko shell-init)"`
}
