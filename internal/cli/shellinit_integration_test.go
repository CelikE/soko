package cli_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestIntegration_FishNavigation(t *testing.T) {
	fish, err := exec.LookPath("fish")
	if err != nil {
		t.Skip("fish is not installed")
	}

	binary := filepath.Join(t.TempDir(), "soko")
	build := exec.CommandContext(t.Context(), "go", "build", "-o", binary, "../../cmd/soko")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building soko: %v\n%s", err, out)
	}

	for _, mode := range []string{"xdg", "unset", "empty"} {
		t.Run(mode, func(t *testing.T) {
			base, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			home := filepath.Join(base, "home with spaces")
			if err := os.MkdirAll(home, 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("HOME", home)
			t.Setenv("SOKO_NAV_SESSION", "")
			t.Setenv("XDG_CONFIG_HOME", "")
			configDir := filepath.Join(home, ".config")
			switch mode {
			case "xdg":
				configDir = filepath.Join(base, "config with spaces")
				t.Setenv("XDG_CONFIG_HOME", configDir)
			case "unset":
				if err := os.Unsetenv("XDG_CONFIG_HOME"); err != nil {
					t.Fatal(err)
				}
			}

			repo := filepath.Join(base, "fish repo")
			initRepo(t, repo)
			runSokoInit(t, repo)
			otherSession := filepath.Join(configDir, "soko", ".nav.999999999")
			if err := os.WriteFile(otherSession, []byte(home), 0o600); err != nil {
				t.Fatal(err)
			}

			// Source the actual CLI output and trigger the event used by Fish
			// after a command, consuming the request written by soko cd.
			const script = `"$argv[1]" shell-init --fish | source
or exit 1
"$argv[1]" cd "fish repo"
or exit 1
emit fish_postexec
or exit 1
printf '%s\n' "$PWD"
if test -e "$argv[2]/soko/.nav.$SOKO_NAV_SESSION"
  printf 'navigation request was not consumed\n' >&2
  exit 1
end
`
			cmd := exec.CommandContext(t.Context(), fish, "--no-config", "-c", script, binary, configDir)
			cmd.Dir = base
			var stdout, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			if err := cmd.Run(); err != nil {
				t.Fatalf("fish navigation: %v\nstdout: %s\nstderr: %s", err, stdout.String(), stderr.String())
			}
			if got := strings.TrimSpace(stdout.String()); got != repo {
				t.Errorf("working directory = %q, want %q", got, repo)
			}
			if data, err := os.ReadFile(otherSession); err != nil || string(data) != home {
				t.Errorf("other session request changed: %q, %v", data, err)
			}
		})
	}
}
