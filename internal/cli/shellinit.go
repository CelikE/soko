package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/spf13/cobra"
)

const shellInitScript = `# soko shell integration
# Add this to your shell profile (.bashrc, .zshrc):
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

const fishInitScript = `# soko shell integration for fish
# Add to ~/.config/fish/config.fish:
#   soko shell-init --fish | source

function __soko_nav_hook --on-event fish_postexec
  if set -q XDG_CONFIG_HOME
    set -l base $XDG_CONFIG_HOME
  else
    set -l base $HOME/.config
  end
  set -l nav_file $base/soko/.nav

  if test -f $nav_file
    set -l target (string trim -- (cat $nav_file))
    rm -f $nav_file
    if test -d $target
      cd $target
    end
  end
end
`

const pwshInitScript = `# soko shell integration for PowerShell
# Add to your $PROFILE:
#   soko shell-init --pwsh | Invoke-Expression

function __soko_nav_hook {
    if ($env:LOCALAPPDATA) {
        $base = $env:LOCALAPPDATA
    } else {
        $base = Join-Path $env:USERPROFILE ".config"
    }
    $navFile = Join-Path (Join-Path $base "soko") ".nav"

    if (Test-Path $navFile) {
        $target = (Get-Content $navFile -Raw).Trim()
        Remove-Item $navFile -Force
        if (Test-Path $target -PathType Container) {
            Set-Location $target
        }
    }
}

# Install as prompt hook.
if (-not (Get-Variable __soko_original_prompt -Scope Global -ErrorAction SilentlyContinue)) {
    $global:__soko_original_prompt = $function:prompt
    function global:prompt {
        __soko_nav_hook
        & $global:__soko_original_prompt
    }
}
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
  soko shell-init --fish | source

PowerShell:
  soko shell-init --pwsh | Invoke-Expression`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			fishFlag, _ := cmd.Flags().GetBool("fish")
			pwshFlag, _ := cmd.Flags().GetBool("pwsh")

			switch {
			case fishFlag:
				_, _ = fmt.Fprint(cmd.OutOrStdout(), fishInitScript)
			case pwshFlag:
				_, _ = fmt.Fprint(cmd.OutOrStdout(), pwshInitScript)
			default:
				_, _ = fmt.Fprint(cmd.OutOrStdout(), shellInitScript)
			}
			return nil
		},
	}

	cmd.Flags().Bool("fish", false, "output fish shell syntax")
	cmd.Flags().Bool("pwsh", false, "output PowerShell syntax")

	return cmd
}

// navFilePath returns the path to the navigation file.
func navFilePath() string {
	if runtime.GOOS == "windows" {
		localAppData := os.Getenv("LOCALAPPDATA")
		if localAppData != "" {
			return filepath.Join(localAppData, "soko", ".nav")
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		return filepath.Join(home, ".config", "soko", ".nav")
	}

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
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating nav directory: %w", err)
	}

	// Prevent symlink attacks: if the file exists and is a symlink, remove it.
	if info, err := os.Lstat(navPath); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			if err := os.Remove(navPath); err != nil {
				return fmt.Errorf("removing symlinked nav file: %w", err)
			}
		}
	}

	return os.WriteFile(navPath, []byte(path), 0o600)
}

// shellInitHint returns the hint message shown after init.
func shellInitHint() string {
	if runtime.GOOS == "windows" {
		return `To enable directory navigation (soko go, soko cd), add to your PowerShell $PROFILE:
  soko shell-init --pwsh | Invoke-Expression`
	}
	return `To enable directory navigation (soko go, soko cd), add to your shell profile:
  eval "$(soko shell-init)"`
}
