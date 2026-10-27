package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/CelikE/soko/internal/config"
)

const shellInitScript = `# soko shell integration
# Add this to your shell profile (.bashrc, .zshrc):
#   eval "$(soko shell-init)"

# Lets soko detect the integration (e.g. soko ui's cd-on-enter hint).
export SOKO_SHELL_INTEGRATION=1
export SOKO_NAV_SESSION=$$

__soko_nav_hook() {
  local nav_file="${XDG_CONFIG_HOME:-$HOME/.config}/soko/.nav.$SOKO_NAV_SESSION"
  if [[ -f "$nav_file" ]]; then
    local target
    # The sentinel keeps trailing newlines through command substitution.
    target=$(cat "$nav_file" && printf '.') || return
    target=${target%.}
    if [[ -d "$target" ]] && builtin cd "$target"; then
      rm -f "$nav_file"
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

# Lets soko detect the integration (e.g. soko ui's cd-on-enter hint).
set -gx SOKO_SHELL_INTEGRATION 1
set -gx SOKO_NAV_SESSION $fish_pid

function __soko_nav_hook --on-event fish_postexec
  set -l base $HOME/.config
  if set -q XDG_CONFIG_HOME; and test -n "$XDG_CONFIG_HOME"
    set base $XDG_CONFIG_HOME
  end
  set -l nav_file $base/soko/.nav.$SOKO_NAV_SESSION

  if test -f $nav_file
    # Collect as one argument, retaining spaces and trailing newlines.
    set -l target (string collect --no-trim-newlines < $nav_file)
    if test -d "$target"; and cd "$target"
      rm -f $nav_file
    end
  end
end
`

const pwshInitScript = `# soko shell integration for PowerShell
# Add to your $PROFILE:
#   soko shell-init --pwsh | Invoke-Expression

# Lets soko detect the integration (e.g. soko ui's cd-on-enter hint).
$env:SOKO_SHELL_INTEGRATION = "1"
$env:SOKO_NAV_SESSION = "$PID"

function __soko_nav_hook {
    if ($env:XDG_CONFIG_HOME) {
        $base = $env:XDG_CONFIG_HOME
    } elseif ($env:LOCALAPPDATA) {
        $base = $env:LOCALAPPDATA
    } else {
        $base = Join-Path $env:USERPROFILE ".config"
    }
    $navFile = Join-Path (Join-Path $base "soko") ".nav.$env:SOKO_NAV_SESSION"

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

// bashZshDiscoverScript installs a directory-change hook that auto-discovers
// repos. Emitted only when discovery is enabled. zsh uses the native chpwd
// hook; bash tracks $PWD across prompts. The hook only spawns soko when the
// directory holds a .git entry, so plain cds incur no process startup.
const bashZshDiscoverScript = `
# soko auto-discovery (toggle with: soko discover on|off)
__soko_discover_hook() {
  [[ $- == *i* ]] || return
  [[ -e "$PWD/.git" ]] || return
  command soko discover hook
}
if [[ -n "${ZSH_VERSION-}" ]]; then
  autoload -Uz add-zsh-hook
  add-zsh-hook chpwd __soko_discover_hook
elif [[ -n "${BASH_VERSION-}" ]]; then
  __soko_discover_pc() {
    if [[ "$PWD" != "${__soko_last_pwd-}" ]]; then
      __soko_last_pwd="$PWD"
      __soko_discover_hook
    fi
  }
  PROMPT_COMMAND="__soko_discover_pc${PROMPT_COMMAND:+;$PROMPT_COMMAND}"
fi
`

const fishDiscoverScript = `
# soko auto-discovery (toggle with: soko discover on|off)
function __soko_discover_hook --on-variable PWD
  status is-interactive; or return
  test -e "$PWD/.git"; or return
  command soko discover hook
end
`

const pwshDiscoverScript = `
# soko auto-discovery (toggle with: soko discover on|off)
function __soko_discover_hook {
    if (Test-Path (Join-Path $PWD ".git")) {
        if ($global:__soko_last_pwd -ne $PWD.Path) {
            $global:__soko_last_pwd = $PWD.Path
            & soko discover hook
        }
    }
}
if (-not (Get-Variable __soko_discover_prompt -Scope Global -ErrorAction SilentlyContinue)) {
    $global:__soko_discover_prompt = $function:prompt
    function global:prompt {
        __soko_discover_hook
        & $global:__soko_discover_prompt
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
  soko shell-init --pwsh | Invoke-Expression

--fish and --pwsh cannot both be enabled. With neither enabled, output Bash/Zsh.`,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				hints := map[string]string{
					"fish":       "--fish",
					"pwsh":       "--pwsh",
					"powershell": "--pwsh",
				}
				if flag, ok := hints[args[0]]; ok {
					return fmt.Errorf("unknown argument %q — did you mean %s?", args[0], flag)
				}
				if args[0] == "bash" || args[0] == "zsh" {
					return fmt.Errorf("unknown argument %q — bash/zsh is the default, just run: soko shell-init", args[0])
				}
				return fmt.Errorf("unknown argument %q — use --fish or --pwsh for non-bash/zsh shells", args[0])
			}
			fish, _ := cmd.Flags().GetBool("fish")
			pwsh, _ := cmd.Flags().GetBool("pwsh")
			if fish && pwsh {
				return fmt.Errorf("--fish and --pwsh are mutually exclusive")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			fishFlag, _ := cmd.Flags().GetBool("fish")
			pwshFlag, _ := cmd.Flags().GetBool("pwsh")

			// Auto-discovery is opt-in: only emit the directory-change hook when
			// the user has enabled it, so non-users pay no per-prompt cost.
			discover := false
			if cfg, err := config.Load(); err == nil {
				discover = cfg.DiscoverEnabled()
			}

			var base, discoverBlock string
			switch {
			case fishFlag:
				base, discoverBlock = fishInitScript, fishDiscoverScript
			case pwshFlag:
				base, discoverBlock = pwshInitScript, pwshDiscoverScript
			default:
				base, discoverBlock = shellInitScript, bashZshDiscoverScript
			}

			out := cmd.OutOrStdout()
			_, _ = fmt.Fprint(out, base)
			if discover {
				_, _ = fmt.Fprint(out, discoverBlock)
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
	name := ".nav"
	if session := os.Getenv("SOKO_NAV_SESSION"); session != "" {
		if id, err := strconv.ParseUint(session, 10, 64); err == nil && id > 0 {
			name += "." + session
		}
	}
	if runtime.GOOS == "windows" && os.Getenv("XDG_CONFIG_HOME") == "" {
		localAppData := os.Getenv("LOCALAPPDATA")
		if localAppData != "" {
			return filepath.Join(localAppData, "soko", name)
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		return filepath.Join(home, ".config", "soko", name)
	}

	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "soko", name)
}

// writeNavFile writes a path to the navigation file so the shell hook
// can pick it up and cd into it.
func writeNavFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("navigation target: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("navigation target is not a directory: %s", path)
	}
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

// shellInitActivateHint returns the one-line command that re-evaluates the
// shell integration, used to (de)activate auto-discovery in the current shell.
func shellInitActivateHint() string {
	if runtime.GOOS == "windows" {
		return "soko shell-init --pwsh | Invoke-Expression"
	}
	return `eval "$(soko shell-init)"`
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
