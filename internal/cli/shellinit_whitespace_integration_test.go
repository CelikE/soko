package cli_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// Exercise emitted integration in real shells; comparing stdout verbatim is
// essential because trimming the cwd would hide the trailing-whitespace bug.
func TestIntegration_ShellNavigationPreservesWhitespace(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix filename semantics; PowerShell native paths have separate coverage")
	}
	binary := filepath.Join(t.TempDir(), "soko")
	build := exec.CommandContext(t.Context(), "go", "build", "-o", binary, "../../cmd/soko")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building soko: %v\n%s", err, out)
	}

	for _, shell := range []string{"bash", "zsh", "fish"} {
		shellPath, err := exec.LookPath(shell)
		if err != nil {
			t.Run(shell, func(t *testing.T) { t.Skipf("%s is not installed", shell) })
			continue
		}
		t.Run(shell, func(t *testing.T) {
			base, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			for _, ending := range []string{"ordinary space", "trailing space ", "trailing newline\n"} {
				t.Run(ending, func(t *testing.T) {
					configDir := filepath.Join(base, "config with spaces", ending)
					t.Setenv("XDG_CONFIG_HOME", configDir)
					t.Setenv("SOKO_NAV_SESSION", "")
					otherSession := filepath.Join(configDir, "soko", ".nav.999999999")
					if err := os.MkdirAll(filepath.Dir(otherSession), 0o700); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(otherSession, []byte(base), 0o600); err != nil {
						t.Fatal(err)
					}
					repo := filepath.Join(base, ending)
					initRepo(t, repo)
					runSokoInit(t, repo)
					for _, navigation := range []string{"cd", "go"} {
						t.Run(navigation, func(t *testing.T) {
							const bashZshScript = `eval "$("$1" shell-init)" || exit 1
if [[ "$2" == go ]]; then
  "$1" go || exit 1
else
  "$1" cd "$3" || exit 1
fi
__soko_nav_hook || exit 1
printf '%s' "$PWD"
[[ ! -e "${XDG_CONFIG_HOME}/soko/.nav.${SOKO_NAV_SESSION}" ]] || exit 1
`
							const fishScript = `"$argv[1]" shell-init --fish | source
or exit 1
if test "$argv[2]" = go
  "$argv[1]" go
else
  "$argv[1]" cd "$argv[3]"
end
or exit 1
emit fish_postexec
or exit 1
printf '%s' "$PWD"
if test -e "$XDG_CONFIG_HOME/soko/.nav.$SOKO_NAV_SESSION"
  exit 1
end
`
							var cmd *exec.Cmd
							if shell == "fish" {
								cmd = exec.CommandContext(t.Context(), shellPath, "--no-config", "-c", fishScript, binary, navigation, ending)
							} else {
								cmd = exec.CommandContext(t.Context(), shellPath, "-c", bashZshScript, "navigation", binary, navigation, ending)
							}
							cmd.Dir = base
							var stdout, stderr bytes.Buffer
							cmd.Stdout, cmd.Stderr = &stdout, &stderr
							if err := cmd.Run(); err != nil {
								t.Fatalf("%s navigation: %v\nstdout: %q\nstderr: %s", shell, err, stdout.String(), stderr.String())
							}
							if got := stdout.String(); got != repo {
								t.Errorf("working directory = %q, want %q", got, repo)
							}
							if data, err := os.ReadFile(otherSession); err != nil || string(data) != base {
								t.Errorf("other session request changed: %q, %v", data, err)
							}
						})
					}
					t.Run("invalid request retry", func(t *testing.T) {
						const bashZshScript = `eval "$("$1" shell-init)" || exit 1
nav_file="${XDG_CONFIG_HOME}/soko/.nav.${SOKO_NAV_SESSION}"
printf '%s' "$2/missing" > "$nav_file"
__soko_nav_hook
[[ -f "$nav_file" && "$PWD" != "$2" ]] || exit 1
printf '%s' "$2" > "$nav_file"
__soko_nav_hook || exit 1
printf '%s' "$PWD"
[[ ! -e "$nav_file" ]] || exit 1
`
						const fishScript = `"$argv[1]" shell-init --fish | source
or exit 1
set -l nav_file "$XDG_CONFIG_HOME/soko/.nav.$SOKO_NAV_SESSION"
printf '%s' "$argv[2]/missing" > "$nav_file"
emit fish_postexec
test -f "$nav_file"; and test "$PWD" != "$argv[2]"
or exit 1
printf '%s' "$argv[2]" > "$nav_file"
emit fish_postexec
or exit 1
printf '%s' "$PWD"
test ! -e "$nav_file"
or exit 1
`
						var cmd *exec.Cmd
						if shell == "fish" {
							cmd = exec.CommandContext(t.Context(), shellPath, "--no-config", "-c", fishScript, binary, repo)
						} else {
							cmd = exec.CommandContext(t.Context(), shellPath, "-c", bashZshScript, "navigation", binary, repo)
						}
						cmd.Dir = base
						out, err := cmd.CombinedOutput()
						if err != nil || string(out) != repo {
							t.Fatalf("invalid request retry: %v, output = %q, want %q", err, out, repo)
						}
					})
				})
			}
		})
	}
}
