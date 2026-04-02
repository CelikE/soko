package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

const shellInitScript = `# soko shell integration
# Add this to your shell profile (.bashrc, .zshrc, config.fish):
#   eval "$(soko shell-init)"       # bash/zsh
#   soko shell-init | source        # fish

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

const fishInitScript = `# soko shell integration for fish
# Add to ~/.config/fish/config.fish:
#   soko shell-init | source

function __soko_nav_hook --on-event fish_postexec
  set -l nav_file (set -q XDG_CONFIG_HOME; and echo $XDG_CONFIG_HOME; or echo $HOME/.config)/soko/.nav
  if test -f $nav_file
    set -l target (cat $nav_file)
    rm -f $nav_file
    if test -d $target
      cd $target
    end
  end
end
`

// newShellInitCmd creates the cobra command for soko shell-init.
func newShellInitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "shell-init",
		Short: "Print shell integration hook for navigation",
		Long: `Print a shell hook that enables soko go and soko cd to change
your working directory. The hook runs after each command and checks
for a navigation request.

Bash/Zsh:
  eval "$(soko shell-init)"

Fish:
  soko shell-init --fish | source`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			fishFlag, _ := cmd.Flags().GetBool("fish")
			if fishFlag {
				_, _ = fmt.Fprint(cmd.OutOrStdout(), fishInitScript)
			} else {
				_, _ = fmt.Fprint(cmd.OutOrStdout(), shellInitScript)
			}
			return nil
		},
	}

	cmd.Flags().Bool("fish", false, "output fish shell syntax")

	return cmd
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
