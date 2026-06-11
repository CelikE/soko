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

// snapGit runs a git command in dir for snapshot tests, returning trimmed stdout.
func snapGit(t *testing.T, dir string, args ...string) string {
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

// snapCommit creates an empty commit and returns the new HEAD SHA.
func snapCommit(t *testing.T, dir, msg string) string {
	t.Helper()
	snapGit(t, dir, "commit", "--allow-empty", "-m", msg)
	return snapGit(t, dir, "rev-parse", "HEAD")
}

// TestIntegration_SnapshotSaveRestore is the full round trip: save records
// branch+SHA, the branch moves forward, restore rewinds it to the recorded
// commit.
func TestIntegration_SnapshotSaveRestore(t *testing.T) {
	testEnv(t)
	repo := filepath.Join(t.TempDir(), "snap-repo")
	initRepo(t, repo)
	runSokoInit(t, repo)

	saved := snapGit(t, repo, "rev-parse", "HEAD")

	out := runSoko(t, "snapshot", "save", "pre-fight")
	if !strings.Contains(out, "master @ "+saved[:7]) {
		t.Errorf("snapshot save = %q, want 'master @ %s'", out, saved[:7])
	}
	if !strings.Contains(out, `snapshot "pre-fight" saved`) {
		t.Errorf("snapshot save = %q, want saved summary", out)
	}

	// The boss fight: the branch moves forward.
	moved := snapCommit(t, repo, "risky change")

	out = runSoko(t, "snapshot", "restore", "pre-fight")
	if !strings.Contains(out, "moved back to "+saved[:7]) || !strings.Contains(out, "(was "+moved[:7]+")") {
		t.Errorf("snapshot restore = %q, want 'moved back to %s (was %s)'", out, saved[:7], moved[:7])
	}
	if head := snapGit(t, repo, "rev-parse", "HEAD"); head != saved {
		t.Errorf("after restore, HEAD = %s, want %s", head, saved)
	}
	if br := snapGit(t, repo, "symbolic-ref", "--short", "HEAD"); br != "master" {
		t.Errorf("after restore, branch = %q, want master", br)
	}
}

// TestIntegration_SnapshotRestoreRecreatesBranch covers the undo synergy: a
// branch deleted since the save is recreated at the recorded SHA.
func TestIntegration_SnapshotRestoreRecreatesBranch(t *testing.T) {
	testEnv(t)
	repo := filepath.Join(t.TempDir(), "snap-recreate")
	initRepo(t, repo)
	runSokoInit(t, repo)

	snapGit(t, repo, "checkout", "-b", "feat/doomed")
	sha := snapCommit(t, repo, "work on doomed branch")

	runSoko(t, "snapshot", "save", "before-clean")

	snapGit(t, repo, "checkout", "master")
	snapGit(t, repo, "branch", "-D", "feat/doomed")

	out := runSoko(t, "snapshot", "restore", "before-clean")
	if !strings.Contains(out, "feat/doomed recreated @ "+sha[:7]) {
		t.Errorf("snapshot restore = %q, want 'feat/doomed recreated @ %s'", out, sha[:7])
	}
	if head := snapGit(t, repo, "rev-parse", "HEAD"); head != sha {
		t.Errorf("after restore, HEAD = %s, want %s", head, sha)
	}
}

// TestIntegration_SnapshotRestoreRefusesDirty is the safety property: restore
// never touches a repo that is dirty right now.
func TestIntegration_SnapshotRestoreRefusesDirty(t *testing.T) {
	testEnv(t)
	repo := filepath.Join(t.TempDir(), "snap-refuse")
	initRepo(t, repo)
	runSokoInit(t, repo)

	runSoko(t, "snapshot", "save", "snap")

	if err := os.WriteFile(filepath.Join(repo, "new.txt"), []byte("untracked\n"), 0o644); err != nil {
		t.Fatalf("dirtying repo: %v", err)
	}

	out, err := runSokoErr(t, "snapshot", "restore", "snap")
	if err == nil {
		t.Error("snapshot restore with a dirty repo should return an error")
	}
	if !strings.Contains(out, "dirty — commit or stash first") {
		t.Errorf("snapshot restore dirty = %q, want the dirty refusal", out)
	}
	if _, err := os.Stat(filepath.Join(repo, "new.txt")); err != nil {
		t.Errorf("restore must not touch the dirty repo: %v", err)
	}
}

// TestIntegration_SnapshotDetached covers the detached-HEAD round trip.
func TestIntegration_SnapshotDetached(t *testing.T) {
	testEnv(t)
	repo := filepath.Join(t.TempDir(), "snap-detached")
	initRepo(t, repo)
	runSokoInit(t, repo)

	first := snapGit(t, repo, "rev-parse", "HEAD")
	snapCommit(t, repo, "second")
	snapGit(t, repo, "checkout", "--detach", first)

	runSoko(t, "snapshot", "save", "det")
	snapGit(t, repo, "checkout", "master")

	out := runSoko(t, "snapshot", "restore", "det")
	if !strings.Contains(out, "detached @ "+first[:7]) {
		t.Errorf("snapshot restore = %q, want 'detached @ %s'", out, first[:7])
	}
	if head := snapGit(t, repo, "rev-parse", "HEAD"); head != first {
		t.Errorf("after restore, HEAD = %s, want %s", head, first)
	}
}

func TestIntegration_SnapshotSaveExistingNeedsForce(t *testing.T) {
	testEnv(t)
	repo := filepath.Join(t.TempDir(), "snap-force")
	initRepo(t, repo)
	runSokoInit(t, repo)

	runSoko(t, "snapshot", "save", "twice")

	_, err := runSokoErr(t, "snapshot", "save", "twice")
	if err == nil || !strings.Contains(err.Error(), "--force") {
		t.Errorf("re-saving without --force should error with a --force hint, got %v", err)
	}

	out := runSoko(t, "snapshot", "save", "twice", "--force")
	if !strings.Contains(out, `snapshot "twice" saved`) {
		t.Errorf("snapshot save --force = %q, want saved summary", out)
	}
}

func TestIntegration_SnapshotListShowDrop(t *testing.T) {
	testEnv(t)
	repo := filepath.Join(t.TempDir(), "snap-lsd")
	initRepo(t, repo)
	runSokoInit(t, repo)

	out := runSoko(t, "snapshot", "list")
	if !strings.Contains(out, "no saved snapshots") {
		t.Errorf("snapshot list (empty) = %q, want the empty hint", out)
	}

	sha := snapGit(t, repo, "rev-parse", "HEAD")
	runSoko(t, "snapshot", "save", "alpha")

	out = runSoko(t, "snapshot", "list")
	if !strings.Contains(out, "alpha") || !strings.Contains(out, "1 repo") {
		t.Errorf("snapshot list = %q, want alpha with 1 repo", out)
	}

	out = runSoko(t, "snapshot", "show", "alpha")
	if !strings.Contains(out, "snap-lsd") || !strings.Contains(out, "master @ "+sha[:7]) {
		t.Errorf("snapshot show = %q, want repo row with branch @ sha", out)
	}

	out = runSoko(t, "snapshot", "drop", "alpha", "--force")
	if !strings.Contains(out, `dropped snapshot "alpha"`) {
		t.Errorf("snapshot drop = %q, want dropped confirmation", out)
	}

	_, err := runSokoErr(t, "snapshot", "show", "alpha")
	if err == nil || !strings.Contains(err.Error(), "none saved yet") {
		t.Errorf("show after drop should error with none-saved hint, got %v", err)
	}
}

func TestIntegration_SnapshotInvalidName(t *testing.T) {
	testEnv(t)
	repo := filepath.Join(t.TempDir(), "snap-name")
	initRepo(t, repo)
	runSokoInit(t, repo)

	for _, bad := range []string{"../escape", "a/b", ".hidden", ""} {
		if _, err := runSokoErr(t, "snapshot", "save", bad); err == nil {
			t.Errorf("snapshot save %q should be rejected", bad)
		}
	}
}

func TestIntegration_SnapshotSaveTagFilter(t *testing.T) {
	testEnv(t)
	base := t.TempDir()
	tagged := filepath.Join(base, "snap-tagged")
	other := filepath.Join(base, "snap-other")
	initRepo(t, tagged)
	initRepo(t, other)
	runSokoInit(t, tagged)
	runSokoInit(t, other)
	runSoko(t, "tag", "add", "-r", "snap-tagged", "backend")

	out := runSoko(t, "snapshot", "save", "scoped", "--tag", "backend")
	if !strings.Contains(out, "snap-tagged") || strings.Contains(out, "snap-other") {
		t.Errorf("snapshot save --tag = %q, want only the tagged repo", out)
	}

	out = runSoko(t, "snapshot", "show", "scoped")
	if strings.Contains(out, "snap-other") {
		t.Errorf("snapshot show = %q, must not contain the untagged repo", out)
	}
}

func TestIntegration_SnapshotJSON(t *testing.T) {
	testEnv(t)
	repo := filepath.Join(t.TempDir(), "snap-json")
	initRepo(t, repo)
	runSokoInit(t, repo)

	sha := snapGit(t, repo, "rev-parse", "HEAD")

	out := runSoko(t, "snapshot", "save", "j1", "--json")
	var saveRows []struct {
		Name   string `json:"name"`
		Branch string `json:"branch"`
		SHA    string `json:"sha"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(out), &saveRows); err != nil {
		t.Fatalf("snapshot save --json output is not valid JSON: %v\n%s", err, out)
	}
	if len(saveRows) != 1 || saveRows[0].SHA != sha || saveRows[0].Status != "ok" {
		t.Errorf("snapshot save --json = %+v, want one ok row with sha %s", saveRows, sha)
	}

	out = runSoko(t, "snapshot", "list", "--json")
	var listRows []struct {
		Name  string `json:"name"`
		Repos int    `json:"repos"`
	}
	if err := json.Unmarshal([]byte(out), &listRows); err != nil {
		t.Fatalf("snapshot list --json output is not valid JSON: %v\n%s", err, out)
	}
	if len(listRows) != 1 || listRows[0].Name != "j1" || listRows[0].Repos != 1 {
		t.Errorf("snapshot list --json = %+v, want [{j1 1}]", listRows)
	}
}

// TestIntegration_SnapshotRestoreMissingPath: a repo whose directory vanished
// is reported but does not block the rest.
func TestIntegration_SnapshotRestoreMissingPath(t *testing.T) {
	testEnv(t)
	base := t.TempDir()
	keep := filepath.Join(base, "snap-keep")
	gone := filepath.Join(base, "snap-gone")
	initRepo(t, keep)
	initRepo(t, gone)
	runSokoInit(t, keep)
	runSokoInit(t, gone)

	runSoko(t, "snapshot", "save", "partial")
	if err := os.RemoveAll(gone); err != nil {
		t.Fatalf("removing repo dir: %v", err)
	}

	out, err := runSokoErr(t, "snapshot", "restore", "partial")
	if err == nil {
		t.Error("restore with a missing path should return an error")
	}
	if !strings.Contains(out, "path not found — skipped") || !strings.Contains(out, "snap-keep") {
		t.Errorf("snapshot restore = %q, want skipped row and the surviving repo restored", out)
	}
}
