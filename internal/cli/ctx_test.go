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

// ctxGit runs a git command in dir for ctx tests, returning trimmed stdout.
func ctxGit(t *testing.T, dir string, args ...string) string {
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

// TestIntegration_CtxSaveSwitch is the full round trip: save records branches
// and stashes the dirty repo, the user moves on, switch restores the branch
// and pops the stash.
func TestIntegration_CtxSaveSwitch(t *testing.T) {
	testEnv(t)
	base := t.TempDir()
	dirty := filepath.Join(base, "ctx-dirty")
	clean := filepath.Join(base, "ctx-clean")
	initRepo(t, dirty)
	initRepo(t, clean)
	runSokoInit(t, dirty)
	runSokoInit(t, clean)

	// Feature work in progress: a branch plus an uncommitted file.
	ctxGit(t, dirty, "checkout", "-b", "feat/sso")
	wip := filepath.Join(dirty, "wip.txt")
	if err := os.WriteFile(wip, []byte("half-done\n"), 0o644); err != nil {
		t.Fatalf("writing wip file: %v", err)
	}

	out := runSoko(t, "ctx", "save", "work")
	if !strings.Contains(out, "feat/sso, stashed 1 file") {
		t.Errorf("ctx save = %q, want 'feat/sso, stashed 1 file'", out)
	}
	if !strings.Contains(out, "clean") || !strings.Contains(out, `context "work" saved`) {
		t.Errorf("ctx save = %q, want a clean row and a saved summary", out)
	}

	// The stash left the tree clean and the file gone.
	if st := ctxGit(t, dirty, "status", "--porcelain"); st != "" {
		t.Errorf("after save, dirty repo status = %q, want clean", st)
	}

	// Move on: oncall happens on master.
	ctxGit(t, dirty, "checkout", "master")

	out = runSoko(t, "ctx", "switch", "work")
	if !strings.Contains(out, "feat/sso (stash restored)") {
		t.Errorf("ctx switch = %q, want 'feat/sso (stash restored)'", out)
	}

	if br := ctxGit(t, dirty, "symbolic-ref", "--short", "HEAD"); br != "feat/sso" {
		t.Errorf("after switch, branch = %q, want feat/sso", br)
	}
	if _, err := os.Stat(wip); err != nil {
		t.Errorf("after switch, wip file should be restored: %v", err)
	}
}

// TestIntegration_CtxSwitchRefusesDirty is the safety property: switch never
// touches a repo that is dirty right now.
func TestIntegration_CtxSwitchRefusesDirty(t *testing.T) {
	testEnv(t)
	repo := filepath.Join(t.TempDir(), "ctx-refuse")
	initRepo(t, repo)
	runSokoInit(t, repo)

	runSoko(t, "ctx", "save", "snap")

	// New uncommitted work appears after the save.
	if err := os.WriteFile(filepath.Join(repo, "new.txt"), []byte("untracked\n"), 0o644); err != nil {
		t.Fatalf("dirtying repo: %v", err)
	}

	out, err := runSokoErr(t, "ctx", "switch", "snap")
	if err == nil {
		t.Error("ctx switch with a dirty repo should return an error")
	}
	if !strings.Contains(out, "dirty — commit, stash, or ctx save first") {
		t.Errorf("ctx switch dirty = %q, want the dirty refusal", out)
	}

	// The untracked file must be exactly where the user left it.
	if _, err := os.Stat(filepath.Join(repo, "new.txt")); err != nil {
		t.Errorf("switch must not touch the dirty repo: %v", err)
	}
}

func TestIntegration_CtxSaveExistingNeedsForce(t *testing.T) {
	testEnv(t)
	repo := filepath.Join(t.TempDir(), "ctx-force")
	initRepo(t, repo)
	runSokoInit(t, repo)

	runSoko(t, "ctx", "save", "twice")

	_, err := runSokoErr(t, "ctx", "save", "twice")
	if err == nil || !strings.Contains(err.Error(), "--force") {
		t.Errorf("re-saving without --force should error with a --force hint, got %v", err)
	}

	out := runSoko(t, "ctx", "save", "twice", "--force")
	if !strings.Contains(out, `context "twice" saved`) {
		t.Errorf("ctx save --force = %q, want saved summary", out)
	}
}

func TestIntegration_CtxListShowDrop(t *testing.T) {
	testEnv(t)
	repo := filepath.Join(t.TempDir(), "ctx-lifecycle")
	initRepo(t, repo)
	runSokoInit(t, repo)

	runSoko(t, "ctx", "save", "alpha")

	out := runSoko(t, "ctx", "list")
	if !strings.Contains(out, "alpha") || !strings.Contains(out, "1 repo") {
		t.Errorf("ctx list = %q, want 'alpha' with '1 repo'", out)
	}

	out = runSoko(t, "ctx", "show", "alpha")
	if !strings.Contains(out, "ctx-lifecycle") || !strings.Contains(out, "master") {
		t.Errorf("ctx show = %q, want repo name and branch", out)
	}

	out = runSoko(t, "ctx", "drop", "alpha", "--force")
	if !strings.Contains(out, `dropped context "alpha"`) {
		t.Errorf("ctx drop = %q, want dropped confirmation", out)
	}

	out = runSoko(t, "ctx", "list")
	if !strings.Contains(out, "no saved contexts") {
		t.Errorf("ctx list after drop = %q, want 'no saved contexts'", out)
	}
}

func TestIntegration_CtxUnknownName(t *testing.T) {
	testEnv(t)
	repo := filepath.Join(t.TempDir(), "ctx-unknown")
	initRepo(t, repo)
	runSokoInit(t, repo)

	runSoko(t, "ctx", "save", "exists")

	_, err := runSokoErr(t, "ctx", "switch", "nope")
	if err == nil || !strings.Contains(err.Error(), "exists") {
		t.Errorf("switch to unknown context should list saved ones, got %v", err)
	}
}

// TestIntegration_CtxStashMissing verifies a manually consumed stash degrades
// to a warning: the branch is still restored and the switch succeeds.
func TestIntegration_CtxStashMissing(t *testing.T) {
	testEnv(t)
	repo := filepath.Join(t.TempDir(), "ctx-gone-stash")
	initRepo(t, repo)
	runSokoInit(t, repo)

	ctxGit(t, repo, "checkout", "-b", "feat/x")
	if err := os.WriteFile(filepath.Join(repo, "wip.txt"), []byte("wip\n"), 0o644); err != nil {
		t.Fatalf("writing wip: %v", err)
	}
	runSoko(t, "ctx", "save", "lost")

	// The user pops the stash by hand and moves to master.
	ctxGit(t, repo, "stash", "drop")
	ctxGit(t, repo, "checkout", "master")

	out := runSoko(t, "ctx", "switch", "lost") // must not error
	if !strings.Contains(out, "stash missing") {
		t.Errorf("ctx switch = %q, want a 'stash missing' note", out)
	}
	if br := ctxGit(t, repo, "symbolic-ref", "--short", "HEAD"); br != "feat/x" {
		t.Errorf("branch after switch = %q, want feat/x", br)
	}
}

func TestIntegration_CtxSaveJSON(t *testing.T) {
	testEnv(t)
	repo := filepath.Join(t.TempDir(), "ctx-json")
	initRepo(t, repo)
	runSokoInit(t, repo)

	out := runSoko(t, "ctx", "save", "j", "--json")
	var entries []struct {
		Name    string `json:"name"`
		Branch  string `json:"branch"`
		Stashed bool   `json:"stashed"`
		Status  string `json:"status"`
	}
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		t.Fatalf("parsing JSON: %v\noutput: %s", err, out)
	}
	if len(entries) != 1 {
		t.Fatalf("JSON entries = %d, want 1", len(entries))
	}
	if entries[0].Status != "ok" || entries[0].Branch != "master" || entries[0].Stashed {
		t.Errorf("entry = %+v, want ok/master/unstashed", entries[0])
	}
}
