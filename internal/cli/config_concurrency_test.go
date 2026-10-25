package cli_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/CelikE/soko/internal/cli"
	"github.com/CelikE/soko/internal/config"
	"github.com/CelikE/soko/internal/git"
)

// TestConfigCLIProcess executes an actual independent command process without
// rebuilding soko for every scheduling case.
func TestConfigCLIProcess(t *testing.T) {
	if os.Getenv("SOKO_CONFIG_CLI_PROCESS") != "1" {
		return
	}
	separator := slices.Index(os.Args, "--")
	if separator < 0 {
		t.Fatal("missing command arguments")
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	git.SetBinary(cfg.GitBinary())
	cmd := cli.NewRootCmd("test")
	cmd.SetArgs(os.Args[separator+1:])
	cmd.SetOut(os.Stdout)
	cmd.SetErr(os.Stderr)
	if err := cmd.Execute(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}

func configCLIProcess(ctx context.Context, t *testing.T, args ...string) *exec.Cmd {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	argv := append([]string{"-test.run=^TestConfigCLIProcess$", "--"}, args...)
	cmd := exec.CommandContext(ctx, executable, argv...)
	cmd.Env = append(os.Environ(), "SOKO_CONFIG_CLI_PROCESS=1")
	return cmd
}

func TestIntegration_ConfigConcurrentContextAndAnnotation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Git scheduling fixture requires a POSIX shell")
	}
	for _, dirty := range []bool{false, true} {
		t.Run(fmt.Sprintf("dirty-%v", dirty), func(t *testing.T) {
			testEnv(t)
			repo := filepath.Join(t.TempDir(), "A")
			initRepo(t, repo)
			runSokoInit(t, repo)
			if dirty {
				ctxWrite(t, filepath.Join(repo, "README.md"), "staged work\n")
				ctxGit(t, repo, "add", "README.md")
				ctxWrite(t, filepath.Join(repo, "README.md"), "unstaged work\n")
				ctxWrite(t, filepath.Join(repo, "new.txt"), "untracked work\n")
			}
			before := ctxGit(t, repo, "status", "--porcelain")
			barrier := t.TempDir()
			realGit, err := exec.LookPath("git")
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv("SOKO_CONFIG_REAL_GIT", realGit)
			t.Setenv("SOKO_CONFIG_BARRIER", barrier)
			wrapper := filepath.Join(t.TempDir(), "git")
			script := `#!/bin/sh
if [ "$1 $2" = "status --porcelain" ]; then
  touch "$SOKO_CONFIG_BARRIER/loaded"
  count=0
  while [ ! -f "$SOKO_CONFIG_BARRIER/release" ]; do
    count=$((count + 1))
    if [ "$count" -ge 1500 ]; then exit 1; fi
    sleep 0.01
  done
fi
exec "$SOKO_CONFIG_REAL_GIT" "$@"
`
			if err := os.WriteFile(wrapper, []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
			cfg, err := config.Load()
			if err != nil {
				t.Fatal(err)
			}
			cfg.GitPath = wrapper
			if err := config.Save(cfg); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			save := configCLIProcess(ctx, t, "ctx", "save", "overlap", "--json")
			var stdout, stderr bytes.Buffer
			save.Stdout, save.Stderr = &stdout, &stderr
			if err := save.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if save.ProcessState == nil {
					_ = save.Process.Kill()
					_ = save.Wait()
				}
			})
			deadline := time.Now().Add(15 * time.Second)
			for {
				if _, err := os.Stat(filepath.Join(barrier, "loaded")); err == nil {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("context save did not reach loaded barrier")
				}
				time.Sleep(5 * time.Millisecond)
			}
			annotate := configCLIProcess(ctx, t, "annotate", "A", "--set", "owner=writer-b")
			if out, err := annotate.CombinedOutput(); err != nil {
				t.Fatalf("annotation could not complete while Git was paused: %v, %s", err, out)
			}
			if err := os.WriteFile(filepath.Join(barrier, "release"), nil, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := save.Wait(); err == nil {
				t.Errorf("stale context save reported success: %s", stdout.String())
			} else if !strings.Contains(stderr.String(), "config changed") {
				t.Fatalf("save failed for an unexpected reason: %v; %s", err, stderr.String())
			}
			cfg, err = config.Load()
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Repos[0].Meta["owner"] != "writer-b" {
				t.Errorf("completed annotation lost: %+v; save stderr: %s", cfg.Repos[0].Meta, stderr.String())
			}
			if _, exists := cfg.Contexts["overlap"]; exists {
				t.Error("conflicting context was persisted")
			}
			if got := ctxGit(t, repo, "status", "--porcelain"); got != before {
				t.Errorf("failed save did not restore original state: %q, want %q", got, before)
			}
			if dirty {
				ctxContents(t, filepath.Join(repo, "README.md"), "unstaged work\n")
				ctxContents(t, filepath.Join(repo, "new.txt"), "untracked work\n")
				if got := ctxGit(t, repo, "show", ":README.md"); got != "staged work" {
					t.Errorf("staged work lost: %s", got)
				}
			}
		})
	}
}

func TestIntegration_ConfigSequentialContextAndAnnotation(t *testing.T) {
	for _, contextFirst := range []bool{false, true} {
		t.Run(fmt.Sprintf("context-first-%v", contextFirst), func(t *testing.T) {
			testEnv(t)
			repo := filepath.Join(t.TempDir(), "A")
			initRepo(t, repo)
			runSokoInit(t, repo)
			commands := [][]string{
				{"annotate", "A", "--set", "owner=writer-b"},
				{"ctx", "save", "sequential", "--json"},
			}
			if contextFirst {
				commands[0], commands[1] = commands[1], commands[0]
			}
			for _, args := range commands {
				runSoko(t, args...)
			}
			cfg, err := config.Load()
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Repos[0].Meta["owner"] != "writer-b" || cfg.Contexts["sequential"] == nil {
				t.Fatalf("sequential command lost an update: %+v", cfg)
			}
		})
	}
}
