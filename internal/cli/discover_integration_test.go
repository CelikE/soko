package cli_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CelikE/soko/internal/cli"
	"github.com/CelikE/soko/internal/config"
)

// runSokoFull executes a soko command and returns both stdout and stderr. The
// discover hook reports successful registration on stderr, so tests need both.
func runSokoFull(t *testing.T, args ...string) (stdout, stderr string) {
	t.Helper()

	var out, errb bytes.Buffer
	cmd := cli.NewRootCmd("test")
	cmd.SetOut(&out)
	cmd.SetErr(&errb)
	cmd.SetArgs(args)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("soko %s: %v", strings.Join(args, " "), err)
	}
	return out.String(), errb.String()
}

// runSokoExpectErr executes a soko command and returns its error (if any)
// without failing the test, for asserting validation failures.
func runSokoExpectErr(t *testing.T, args ...string) error {
	t.Helper()

	cmd := cli.NewRootCmd("test")
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs(args)
	return cmd.Execute()
}

// gitCmd runs a git command in dir and fails the test on error.
func gitCmd(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// resolve returns the symlink-free absolute form of p, matching the paths soko
// stores after discovery.
func resolve(t *testing.T, p string) string {
	t.Helper()
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}

func TestIntegration_DiscoverHook_RegistersFreshRepo(t *testing.T) {
	testEnv(t)
	repo := filepath.Join(t.TempDir(), "fresh")
	initRepo(t, repo)

	runSoko(t, "discover", "on", "--tag", "discovered")

	_, stderr := runSokoFull(t, "discover", "hook", repo)
	if !strings.Contains(stderr, "discovered fresh") {
		t.Errorf("hook stderr = %q, want to mention 'discovered fresh'", stderr)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("loading config: %v", err)
	}
	if len(cfg.Repos) != 1 {
		t.Fatalf("registered repos = %d, want 1", len(cfg.Repos))
	}
	if got := cfg.Repos[0].Path; got != resolve(t, repo) {
		t.Errorf("registered path = %q, want %q", got, resolve(t, repo))
	}
	if len(cfg.Repos[0].Tags) != 1 || cfg.Repos[0].Tags[0] != "discovered" {
		t.Errorf("registered tags = %v, want [discovered]", cfg.Repos[0].Tags)
	}
}

func TestIntegration_DiscoverHook_Idempotent(t *testing.T) {
	testEnv(t)
	repo := filepath.Join(t.TempDir(), "again")
	initRepo(t, repo)

	runSoko(t, "discover", "on")
	runSokoFull(t, "discover", "hook", repo)

	_, stderr := runSokoFull(t, "discover", "hook", repo)
	if strings.Contains(stderr, "discovered") {
		t.Errorf("second hook should be silent, got stderr = %q", stderr)
	}

	cfg, _ := config.Load()
	if len(cfg.Repos) != 1 {
		t.Errorf("repos after re-visit = %d, want 1", len(cfg.Repos))
	}
}

func TestIntegration_DiscoverHook_DisabledIsNoop(t *testing.T) {
	testEnv(t)
	repo := filepath.Join(t.TempDir(), "untracked")
	initRepo(t, repo)

	// Discovery never enabled.
	_, stderr := runSokoFull(t, "discover", "hook", repo)
	if stderr != "" {
		t.Errorf("disabled hook stderr = %q, want empty", stderr)
	}

	cfg, _ := config.Load()
	if len(cfg.Repos) != 0 {
		t.Errorf("repos = %d, want 0 (discovery disabled)", len(cfg.Repos))
	}
}

func TestIntegration_DiscoverHook_RespectsRoots(t *testing.T) {
	testEnv(t)
	root := t.TempDir()
	inside := filepath.Join(root, "inside")
	outside := filepath.Join(t.TempDir(), "outside")
	initRepo(t, inside)
	initRepo(t, outside)

	runSoko(t, "discover", "on", "--root", root)

	runSokoFull(t, "discover", "hook", inside)
	runSokoFull(t, "discover", "hook", outside)

	cfg, _ := config.Load()
	if len(cfg.Repos) != 1 {
		t.Fatalf("repos = %d, want 1 (only the in-root repo)", len(cfg.Repos))
	}
	if cfg.Repos[0].Path != resolve(t, inside) {
		t.Errorf("registered path = %q, want %q", cfg.Repos[0].Path, resolve(t, inside))
	}
}

func TestIntegration_DiscoverHook_SkipsBuiltinIgnore(t *testing.T) {
	testEnv(t)
	repo := filepath.Join(t.TempDir(), "node_modules", "dep")
	initRepo(t, repo)

	runSoko(t, "discover", "on")
	runSokoFull(t, "discover", "hook", repo)

	cfg, _ := config.Load()
	if len(cfg.Repos) != 0 {
		t.Errorf("repos = %d, want 0 (node_modules ignored)", len(cfg.Repos))
	}
}

func TestIntegration_DiscoverHook_ResolvesWorktreeToMain(t *testing.T) {
	testEnv(t)
	main := filepath.Join(t.TempDir(), "main")
	initRepo(t, main)

	wt := filepath.Join(t.TempDir(), "feature-wt")
	gitCmd(t, main, "worktree", "add", "-b", "feature", wt)

	runSoko(t, "discover", "on")
	runSokoFull(t, "discover", "hook", wt)

	cfg, _ := config.Load()
	if len(cfg.Repos) != 1 {
		t.Fatalf("repos = %d, want 1", len(cfg.Repos))
	}
	if got := cfg.Repos[0].Path; got != resolve(t, main) {
		t.Errorf("registered path = %q, want main repo %q (not the worktree)", got, resolve(t, main))
	}
	if cfg.Repos[0].WorktreeOf != "" {
		t.Errorf("discovered entry marked as worktree (%q), want a plain main-repo entry", cfg.Repos[0].WorktreeOf)
	}
}

func TestIntegration_DiscoverOnOffStatus(t *testing.T) {
	testEnv(t)

	out, _ := runSokoFull(t, "discover", "status")
	if !strings.Contains(out, "off") {
		t.Errorf("initial status = %q, want 'off'", out)
	}

	runSoko(t, "discover", "on")
	out, _ = runSokoFull(t, "discover", "status")
	if !strings.Contains(out, "on") {
		t.Errorf("status after on = %q, want 'on'", out)
	}
	cfg, _ := config.Load()
	if !cfg.DiscoverEnabled() {
		t.Error("config not enabled after 'discover on'")
	}

	runSoko(t, "discover", "off")
	cfg, _ = config.Load()
	if cfg.DiscoverEnabled() {
		t.Error("config still enabled after 'discover off'")
	}
}

func TestIntegration_DiscoverStatusJSON(t *testing.T) {
	testEnv(t)
	root := t.TempDir()

	runSoko(t, "discover", "on", "--root", root, "--tag", "disc")

	out, _ := runSokoFull(t, "discover", "status", "--json")
	if !strings.Contains(out, `"enabled": true`) {
		t.Errorf("status --json = %q, want enabled true", out)
	}
	if !strings.Contains(out, `"disc"`) {
		t.Errorf("status --json = %q, want tag 'disc'", out)
	}
}

func TestIntegration_DiscoverOnRejectsMissingRoot(t *testing.T) {
	testEnv(t)
	missing := filepath.Join(t.TempDir(), "does-not-exist")

	err := runSokoExpectErr(t, "discover", "on", "--root", missing)
	if err == nil {
		t.Fatal("discover on with a missing root should error")
	}

	cfg, _ := config.Load()
	if cfg.DiscoverEnabled() {
		t.Error("discovery was enabled despite an invalid root")
	}
}

func TestIntegration_ShellInitIncludesDiscoverHookWhenEnabled(t *testing.T) {
	testEnv(t)

	// Disabled: no discover hook in any shell flavor.
	for _, flag := range [][]string{{}, {"--fish"}, {"--pwsh"}} {
		out, _ := runSokoFull(t, append([]string{"shell-init"}, flag...)...)
		if strings.Contains(out, "__soko_discover_hook") {
			t.Errorf("shell-init %v emitted discover hook while disabled:\n%s", flag, out)
		}
	}

	runSoko(t, "discover", "on")

	for _, flag := range [][]string{{}, {"--fish"}, {"--pwsh"}} {
		out, _ := runSokoFull(t, append([]string{"shell-init"}, flag...)...)
		if !strings.Contains(out, "__soko_discover_hook") {
			t.Errorf("shell-init %v missing discover hook while enabled:\n%s", flag, out)
		}
	}
}
