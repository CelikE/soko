package cli_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CelikE/soko/internal/journal"
)

// gitIn runs a git command in dir and fails the test on error.
func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// branchExists reports whether a local branch is present in dir.
func branchExists(t *testing.T, dir, branch string) bool {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "git", "rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null")
	return cmd.Run() == nil
}

// TestIntegration_UndoRecreatesBranch is the headline round-trip: clean deletes
// a merged branch, undo restores it at the same SHA.
func TestIntegration_UndoRecreatesBranch(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "repo")
	initRepo(t, dir)
	runSokoInit(t, dir)

	// A branch pointing at HEAD is merged into the default branch.
	wantSHA := gitIn(t, dir, "rev-parse", "HEAD")
	gitIn(t, dir, "branch", "feature")
	if !branchExists(t, dir, "feature") {
		t.Fatal("setup: feature branch missing")
	}

	runSoko(t, "clean", "--force")
	if branchExists(t, dir, "feature") {
		t.Fatal("clean did not delete the merged branch")
	}

	out := runSoko(t, "undo")
	if !strings.Contains(out, "feature") {
		t.Errorf("undo output = %q, want it to mention feature", out)
	}
	if !branchExists(t, dir, "feature") {
		t.Fatal("undo did not recreate the branch")
	}
	if got := gitIn(t, dir, "rev-parse", "feature"); got != wantSHA {
		t.Errorf("recreated SHA = %s, want %s", got, wantSHA)
	}

	// Journal is now drained.
	if out := runSoko(t, "undo"); !strings.Contains(out, "nothing to undo") {
		t.Errorf("second undo = %q, want 'nothing to undo'", out)
	}
}

// TestIntegration_UndoList shows the journal newest-first without changing it.
func TestIntegration_UndoList(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "repo")
	initRepo(t, dir)
	runSokoInit(t, dir)
	gitIn(t, dir, "branch", "feature")
	runSoko(t, "clean", "--force")

	out := runSoko(t, "undo", "--list")
	if !strings.Contains(out, "clean") || !strings.Contains(out, "deleted 1 branch") {
		t.Errorf("undo --list = %q, want the clean entry", out)
	}
	// Listing must not consume the entry.
	if !branchExists(t, dir, "feature") {
		// listing should not have reverted anything, but also must not delete —
		// feature is still deleted here; the point is the journal is intact.
		out := runSoko(t, "undo", "--list")
		if !strings.Contains(out, "deleted 1 branch") {
			t.Errorf("journal was consumed by --list: %q", out)
		}
	}
}

// TestIntegration_UndoPullResets verifies undo of a journaled pull rewinds the
// branch to its pre-pull SHA — the reversal soko ui's pull relies on.
func TestIntegration_UndoPullResets(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "repo")
	initRepo(t, dir)

	preSHA := gitIn(t, dir, "rev-parse", "HEAD")

	// A second commit stands in for "what the pull fast-forwarded to".
	if err := os.WriteFile(filepath.Join(dir, "more.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-m", "second")
	if gitIn(t, dir, "rev-parse", "HEAD") == preSHA {
		t.Fatal("setup: HEAD did not advance")
	}

	// Journal it as a pull whose pre-image is the first commit.
	entry := journal.Entry{
		Op:      journal.OpPull,
		Time:    time.Unix(1700000000, 0),
		Summary: "pulled repo",
		Pulls:   []journal.PullRef{{Repo: "repo", Path: dir, SHA: preSHA}},
	}
	if err := journal.Append(&entry); err != nil {
		t.Fatalf("seeding journal: %v", err)
	}

	out := runSoko(t, "undo")
	if !strings.Contains(out, "reset") {
		t.Errorf("undo output = %q, want it to mention reset", out)
	}
	if got := gitIn(t, dir, "rev-parse", "HEAD"); got != preSHA {
		t.Errorf("HEAD after undo = %s, want pre-pull %s", got, preSHA)
	}
}

// TestIntegration_UndoEmpty reports cleanly when there is nothing to undo.
func TestIntegration_UndoEmpty(t *testing.T) {
	testEnv(t)
	out := runSoko(t, "undo")
	if !strings.Contains(out, "nothing to undo") {
		t.Errorf("undo on empty journal = %q", out)
	}
}
