package cli_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CelikE/soko/internal/journal"
)

func initRepoWithBranches(t *testing.T, dir string, branches []string) {
	t.Helper()
	initRepo(t, dir)

	ctx := context.Background()
	for _, branch := range branches {
		// Create branch, add a file, commit, switch back to master.
		cmd := exec.CommandContext(ctx, "git", "checkout", "-b", branch)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git checkout -b %s: %v\n%s", branch, err, out)
		}

		safeName := strings.ReplaceAll(branch, "/", "-")
		f := filepath.Join(dir, safeName+".txt")
		if err := os.WriteFile(f, []byte(branch), 0o644); err != nil {
			t.Fatalf("writing file: %v", err)
		}

		add := exec.CommandContext(ctx, "git", "add", ".")
		add.Dir = dir
		if out, err := add.CombinedOutput(); err != nil {
			t.Fatalf("git add: %v\n%s", err, out)
		}

		cm := exec.CommandContext(ctx, "git", "commit", "-m", "commit on "+branch)
		cm.Dir = dir
		cm.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null")
		if out, err := cm.CombinedOutput(); err != nil {
			t.Fatalf("git commit: %v\n%s", err, out)
		}

		// Switch back to master and merge (so branch is "merged").
		checkout := exec.CommandContext(ctx, "git", "checkout", "master")
		checkout.Dir = dir
		if out, err := checkout.CombinedOutput(); err != nil {
			t.Fatalf("git checkout master: %v\n%s", err, out)
		}

		merge := exec.CommandContext(ctx, "git", "merge", branch, "--no-ff", "-m", "merge "+branch)
		merge.Dir = dir
		merge.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null")
		if out, err := merge.CombinedOutput(); err != nil {
			t.Fatalf("git merge: %v\n%s", err, out)
		}
	}
}

func TestIntegration_CleanDryRun(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "clean-repo")
	initRepoWithBranches(t, dir, []string{"feat/old", "fix/typo"})
	runSokoInit(t, dir)

	out := runSoko(t, "clean", "--dry-run")
	if !strings.Contains(out, "feat/old") {
		t.Errorf("clean --dry-run = %q, want 'feat/old'", out)
	}
	if !strings.Contains(out, "fix/typo") {
		t.Errorf("clean --dry-run = %q, want 'fix/typo'", out)
	}
	if !strings.Contains(out, "2 stale branches") {
		t.Errorf("clean --dry-run = %q, want '2 stale branches'", out)
	}

	// Verify branches still exist (dry-run doesn't delete).
	ctx := context.Background()
	branches, _ := exec.CommandContext(ctx, "git", "branch").Output()
	if !strings.Contains(string(branches), "feat/old") {
		t.Errorf("dry-run deleted branches: %s", string(branches))
	}
}

func TestIntegration_CleanForce(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "clean-force")
	initRepoWithBranches(t, dir, []string{"feat/done"})
	runSokoInit(t, dir)

	out := runSoko(t, "clean", "--force")
	if !strings.Contains(out, "deleted") {
		t.Errorf("clean --force = %q, want 'deleted'", out)
	}

	// Verify branch is gone.
	ctx := context.Background()
	cmd := exec.CommandContext(ctx, "git", "branch")
	cmd.Dir = dir
	branches, _ := cmd.Output()
	if strings.Contains(string(branches), "feat/done") {
		t.Errorf("branch not deleted: %s", string(branches))
	}
}

func TestIntegration_CleanPackedRefsAndUndo(t *testing.T) {
	testEnv(t)
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	branches := []string{"feat/one", "feat/two", "fix/three", "fix/four"}
	wantRefs := make(map[string]string)
	for _, name := range []string{"alpha", "bravo"} {
		dir := filepath.Join(base, name)
		initRepoWithBranches(t, dir, branches)
		runSokoInit(t, dir)
		for _, branch := range branches {
			wantRefs[filepath.Join(dir, branch)] = gitIn(t, dir, "rev-parse", branch)
		}
		gitIn(t, dir, "pack-refs", "--all")
		gitIn(t, dir, "config", "core.packedRefsTimeout", "0")

		// Hold the packed-ref lock briefly so overlapping deletions fail
		// reliably, rather than depending on filesystem timing.
		hook := "#!/bin/sh\nif [ \"$1\" = prepared ]; then\n  sleep 0.1\nfi\n"
		if err := os.WriteFile(filepath.Join(dir, ".git", "hooks", "reference-transaction"), []byte(hook), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	out := runSoko(t, "clean", "--force")
	if !strings.Contains(out, "deleted 8 stale branches across 2 repos") {
		t.Errorf("clean --force = %q, want all eight branches deleted", out)
	}

	j, err := journal.Load()
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := j.Last()
	if !ok || entry.Op != journal.OpClean || len(entry.Branches) != len(wantRefs) {
		t.Fatalf("clean journal = %+v, want all deleted branches", entry)
	}
	seen := make(map[string]bool)
	for _, ref := range entry.Branches {
		key := filepath.Join(ref.Path, ref.Branch)
		if seen[key] || ref.SHA != wantRefs[key] || ref.Repo != filepath.Base(ref.Path) {
			t.Errorf("unexpected or duplicate journal ref: %+v", ref)
		}
		seen[key] = true
		if branchExists(t, ref.Path, ref.Branch) {
			t.Errorf("clean left branch %s in %s", ref.Branch, ref.Path)
		}
	}

	runSoko(t, "undo")
	for _, ref := range entry.Branches {
		if got := gitIn(t, ref.Path, "rev-parse", "refs/heads/"+ref.Branch); got != ref.SHA {
			t.Errorf("restored %s in %s at %s, want %s", ref.Branch, ref.Path, got, ref.SHA)
		}
	}
}

func TestIntegration_CleanNoStaleBranches(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "clean-none")
	initRepo(t, dir)
	runSokoInit(t, dir)

	out := runSoko(t, "clean", "--dry-run")
	if !strings.Contains(out, "all clean") {
		t.Errorf("clean = %q, want 'all clean'", out)
	}
}

func TestIntegration_CleanProtectsCurrentBranch(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "clean-current")
	initRepoWithBranches(t, dir, []string{"feat/active"})

	// Switch to feat/active so it's the current branch.
	ctx := context.Background()
	checkout := exec.CommandContext(ctx, "git", "checkout", "feat/active")
	checkout.Dir = dir
	if out, err := checkout.CombinedOutput(); err != nil {
		t.Fatalf("git checkout: %v\n%s", err, out)
	}

	// Switch back — feat/active is merged but was current.
	// Actually, let's test by staying on master and checking that master isn't listed.
	checkoutMaster := exec.CommandContext(ctx, "git", "checkout", "master")
	checkoutMaster.Dir = dir
	if out, err := checkoutMaster.CombinedOutput(); err != nil {
		t.Fatalf("git checkout master: %v\n%s", err, out)
	}

	runSokoInit(t, dir)

	// feat/active should show as stale (it's merged and not current).
	out := runSoko(t, "clean", "--dry-run")
	if !strings.Contains(out, "feat/active") {
		t.Errorf("clean = %q, want 'feat/active'", out)
	}
	// master should NOT appear in stale list.
	if strings.Contains(out, "  master") {
		t.Errorf("clean lists master as stale: %s", out)
	}
}

func TestIntegration_CleanRepoArg(t *testing.T) {
	testEnv(t)
	dir1 := filepath.Join(t.TempDir(), "alpha")
	dir2 := filepath.Join(t.TempDir(), "bravo")
	initRepoWithBranches(t, dir1, []string{"feat/old"})
	initRepoWithBranches(t, dir2, []string{"feat/other"})
	runSokoInit(t, dir1)
	runSokoInit(t, dir2)

	out := runSoko(t, "clean", "alpha", "--dry-run")
	if !strings.Contains(out, "alpha") {
		t.Errorf("clean alpha = %q, want 'alpha'", out)
	}
	if strings.Contains(out, "bravo") {
		t.Errorf("clean alpha = %q, should not contain 'bravo'", out)
	}
}
