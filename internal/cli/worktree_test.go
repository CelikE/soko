package cli_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// wtGit runs a git command in dir for worktree tests, returning trimmed stdout.
func wtGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestIntegration_WorktreeAddCreateBranch(t *testing.T) {
	testEnv(t)
	base := t.TempDir()
	repo := filepath.Join(base, "wt-api")
	initRepo(t, repo)
	runSokoInit(t, repo)

	out := runSoko(t, "worktree", "add", "wt-api", "feat-x", "-b")
	if !strings.Contains(out, "created worktree wt-api/feat-x") {
		t.Errorf("worktree add = %q, want 'created worktree wt-api/feat-x'", out)
	}

	wtPath := filepath.Join(base, "wt-api-feat-x")
	if _, err := os.Stat(wtPath); err != nil {
		t.Errorf("worktree dir should exist at %s: %v", wtPath, err)
	}
	if !strings.Contains(out, wtPath) {
		t.Errorf("worktree add = %q, want the path %s printed", out, wtPath)
	}
	if br := wtGit(t, wtPath, "rev-parse", "--abbrev-ref", "HEAD"); br != "feat-x" {
		t.Errorf("worktree branch = %q, want feat-x", br)
	}

	// Registered under the parent/branch convention and linked to the parent.
	listOut := runSoko(t, "list")
	if !strings.Contains(listOut, "wt-api/feat-x") {
		t.Errorf("soko list = %q, want the worktree entry", listOut)
	}
}

func TestIntegration_WorktreeAddMissingBranchNeedsFlag(t *testing.T) {
	testEnv(t)
	repo := filepath.Join(t.TempDir(), "wt-nob")
	initRepo(t, repo)
	runSokoInit(t, repo)

	_, err := runSokoErr(t, "worktree", "add", "wt-nob", "ghost")
	if err == nil || !strings.Contains(err.Error(), "-b") {
		t.Errorf("add with missing branch should error with a -b hint, got %v", err)
	}

	// With an existing branch no -b is needed.
	wtGit(t, repo, "branch", "real")
	out := runSoko(t, "worktree", "add", "wt-nob", "real")
	if !strings.Contains(out, "created worktree wt-nob/real") {
		t.Errorf("worktree add existing branch = %q, want created", out)
	}
}

func TestIntegration_WorktreeList(t *testing.T) {
	testEnv(t)
	base := t.TempDir()
	repo := filepath.Join(base, "wt-list")
	initRepo(t, repo)
	runSokoInit(t, repo)
	runSoko(t, "worktree", "add", "wt-list", "feat-y", "-b")

	out := runSoko(t, "worktree", "list")
	if !strings.Contains(out, "wt-list/feat-y") || !strings.Contains(out, "clean") {
		t.Errorf("worktree list = %q, want the entry marked clean", out)
	}

	// Dirty the worktree; list must flag it.
	wtPath := filepath.Join(base, "wt-list-feat-y")
	if err := os.WriteFile(filepath.Join(wtPath, "x.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatalf("dirtying worktree: %v", err)
	}
	out = runSoko(t, "worktree", "list")
	if !strings.Contains(out, "dirty") {
		t.Errorf("worktree list = %q, want dirty status", out)
	}
}

func TestIntegration_WorktreeRm(t *testing.T) {
	testEnv(t)
	base := t.TempDir()
	repo := filepath.Join(base, "wt-rm")
	initRepo(t, repo)
	runSokoInit(t, repo)
	runSoko(t, "worktree", "add", "wt-rm", "feat-z", "-b")
	wtPath := filepath.Join(base, "wt-rm-feat-z")

	// Dirty worktree is refused without --force.
	if err := os.WriteFile(filepath.Join(wtPath, "wip.txt"), []byte("wip\n"), 0o644); err != nil {
		t.Fatalf("dirtying worktree: %v", err)
	}
	_, err := runSokoErr(t, "worktree", "rm", "wt-rm/feat-z")
	if err == nil || !strings.Contains(err.Error(), "--force") {
		t.Errorf("rm dirty worktree should error with --force hint, got %v", err)
	}
	if _, statErr := os.Stat(wtPath); statErr != nil {
		t.Errorf("refused rm must not delete the worktree: %v", statErr)
	}

	out := runSoko(t, "worktree", "rm", "wt-rm/feat-z", "--force")
	if !strings.Contains(out, "removed worktree wt-rm/feat-z") {
		t.Errorf("worktree rm --force = %q, want removed confirmation", out)
	}
	if _, statErr := os.Stat(wtPath); !os.IsNotExist(statErr) {
		t.Errorf("worktree dir should be gone, stat err = %v", statErr)
	}
	if listOut := runSoko(t, "list"); strings.Contains(listOut, "wt-rm/feat-z") {
		t.Errorf("soko list = %q, worktree entry should be unregistered", listOut)
	}
}

// TestIntegration_WorktreeRmVanishedDir verifies rm still unregisters when the
// directory was deleted by hand, pruning git's stale record in the parent.
func TestIntegration_WorktreeRmVanishedDir(t *testing.T) {
	testEnv(t)
	base := t.TempDir()
	repo := filepath.Join(base, "wt-gone")
	initRepo(t, repo)
	runSokoInit(t, repo)
	runSoko(t, "worktree", "add", "wt-gone", "feat-w", "-b")
	wtPath := filepath.Join(base, "wt-gone-feat-w")

	if err := os.RemoveAll(wtPath); err != nil {
		t.Fatalf("removing worktree dir: %v", err)
	}

	out := runSoko(t, "worktree", "rm", "wt-gone/feat-w")
	if !strings.Contains(out, "removed worktree wt-gone/feat-w") {
		t.Errorf("worktree rm vanished = %q, want removed confirmation", out)
	}
	if listOut := runSoko(t, "list"); strings.Contains(listOut, "wt-gone/feat-w") {
		t.Errorf("soko list = %q, vanished worktree should be unregistered", listOut)
	}
}

func TestIntegration_WorktreeAddJSON(t *testing.T) {
	testEnv(t)
	base := t.TempDir()
	repo := filepath.Join(base, "wt-json")
	initRepo(t, repo)
	runSokoInit(t, repo)

	out := runSoko(t, "worktree", "add", "wt-json", "feat-j", "-b", "--json")
	var entry struct {
		Name   string `json:"name"`
		Parent string `json:"parent"`
		Branch string `json:"branch"`
		Path   string `json:"path"`
	}
	if err := json.Unmarshal([]byte(out), &entry); err != nil {
		t.Fatalf("parsing JSON: %v\noutput: %s", err, out)
	}
	if entry.Name != "wt-json/feat-j" || entry.Parent != "wt-json" || entry.Branch != "feat-j" {
		t.Errorf("entry = %+v, want wt-json/feat-j under wt-json", entry)
	}
	if entry.Path == "" {
		t.Error("entry path must not be empty")
	}
}
