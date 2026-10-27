package cli_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/CelikE/soko/internal/git"
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
	cmd := exec.CommandContext(ctx, "git", "branch")
	cmd.Dir = dir
	branches, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
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

func TestIntegration_CleanDeletionOutcomes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("reference-transaction fixture requires a POSIX shell")
	}
	branches := []string{"allowed", "blocked", "later"}
	for _, tt := range []struct {
		name     string
		rejected []string
	}{
		{name: "success"},
		{name: "partial failure", rejected: []string{"blocked"}},
		{name: "all failed", rejected: branches},
	} {
		for _, mode := range []string{"text", "quiet", "json", "quiet json"} {
			t.Run(tt.name+"/"+mode, func(t *testing.T) {
				testEnv(t)
				t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
				t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
				t.Setenv("GIT_TEMPLATE_DIR", t.TempDir())
				base, err := filepath.EvalSymlinks(t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				dir := filepath.Join(base, "repo")
				initRepoWithBranches(t, dir, branches)
				runSokoInit(t, dir)
				before := gitIn(t, dir, "rev-parse", "HEAD")
				wantSHAs := make(map[string]string)
				var wantDeleted []string
				for _, branch := range branches {
					wantSHAs[branch] = gitIn(t, dir, "rev-parse", "refs/heads/"+branch)
					if !slices.Contains(tt.rejected, branch) {
						wantDeleted = append(wantDeleted, branch)
					}
				}
				patterns := []string{"refs/heads/never"}
				for _, branch := range tt.rejected {
					patterns = append(patterns, "refs/heads/"+branch)
				}
				hook := fmt.Sprintf(`#!/bin/sh
test "$1" = prepared || exit 0
while read -r old new ref; do
  case "$new" in *[!0]*) continue ;; esac
  case "$ref" in
    %s) printf 'fixture refuses deletion of %%s\n' "$ref" >&2; exit 1 ;;
  esac
done
exit 0
`, strings.Join(patterns, "|"))
				hooksDir := filepath.Join(dir, ".git", "hooks")
				if err := os.MkdirAll(hooksDir, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(hooksDir, "reference-transaction"), []byte(hook), 0o755); err != nil {
					t.Fatal(err)
				}

				// Preview must remain a plan, without deletion outcomes or mutations.
				preview := runSoko(t, "clean", "--dry-run", "--json")
				var planned []struct {
					Branches []string        `json:"branches"`
					Deleted  json.RawMessage `json:"deleted"`
					Failed   json.RawMessage `json:"failed"`
				}
				if err := json.Unmarshal([]byte(preview), &planned); err != nil {
					t.Fatal(err)
				}
				if len(planned) != 1 || !slices.Equal(planned[0].Branches, branches) || planned[0].Deleted != nil || planned[0].Failed != nil {
					t.Fatalf("unexpected preview: %s", preview)
				}
				for branch, sha := range wantSHAs {
					if got := gitIn(t, dir, "rev-parse", "refs/heads/"+branch); got != sha {
						t.Fatalf("preview changed %s to %s, want %s", branch, got, sha)
					}
				}

				args := []string{"clean", "--force"}
				for _, flag := range strings.Fields(mode) {
					if flag != "text" {
						args = append(args, "--"+flag)
					}
				}
				out, err := runSokoErr(t, args...)
				if len(tt.rejected) == 0 {
					if err != nil {
						t.Errorf("successful clean: %v", err)
					}
				} else {
					wantErr := fmt.Sprintf("deleted %d branches, %d failed to delete", len(wantDeleted), len(tt.rejected))
					if err == nil || err.Error() != wantErr {
						t.Errorf("clean error = %v, want %q", err, wantErr)
					}
				}
				if strings.Contains(mode, "json") {
					var rows []struct {
						Name     string   `json:"name"`
						Path     string   `json:"path"`
						Branches []string `json:"branches"`
						Deleted  []string `json:"deleted"`
						Failed   []struct {
							Branch    string `json:"branch"`
							Error     string `json:"error"`
							ErrorCode string `json:"error_code"`
						} `json:"failed"`
						Error     string `json:"error"`
						ErrorCode string `json:"error_code"`
					}
					if err := json.Unmarshal([]byte(out), &rows); err != nil {
						t.Fatalf("parse JSON: %v\n%s", err, out)
					}
					if len(rows) != 1 {
						t.Fatalf("want one result: %s", out)
					}
					r := rows[0]
					if r.Name != "repo" || r.Path != dir || !slices.Equal(r.Branches, branches) || !slices.Equal(r.Deleted, wantDeleted) || len(r.Failed) != len(tt.rejected) {
						t.Errorf("incorrect deletion outcomes: %s", out)
					}
					for i, failure := range r.Failed {
						if i >= len(tt.rejected) || failure.Branch != tt.rejected[i] || !strings.Contains(failure.Error, "fixture refuses deletion of refs/heads/"+failure.Branch) || failure.ErrorCode != "git_failure" {
							t.Errorf("incorrect branch failure: %+v", failure)
						}
					}
					if len(tt.rejected) > 0 {
						if r.Error == "" || r.ErrorCode != "git_failure" {
							t.Errorf("missing repository failure: %s", out)
						}
					} else if r.Error != "" || r.ErrorCode != "" {
						t.Errorf("unexpected repository failure: %s", out)
					}
				} else if len(tt.rejected) > 0 && strings.Contains(out, "deleted 0 stale") {
					t.Errorf("misleading success output: %s", out)
				}

				if got := gitIn(t, dir, "rev-parse", "HEAD"); got != before {
					t.Errorf("clean moved HEAD to %s, want %s", got, before)
				}
				for _, branch := range branches {
					if got, want := branchExists(t, dir, branch), slices.Contains(tt.rejected, branch); got != want {
						t.Errorf("branch %s exists = %v, want %v", branch, got, want)
					}
				}
				j, err := journal.Load()
				if err != nil {
					t.Fatal(err)
				}
				entry, ok := j.Last()
				if len(wantDeleted) == 0 {
					if ok {
						t.Errorf("failed deletions produced journal entry: %+v", entry)
					}
					return
				}
				if !ok || entry.Op != journal.OpClean || len(entry.Branches) != len(wantDeleted) {
					t.Fatalf("journal = %+v, want only deleted branches %v", entry, wantDeleted)
				}
				seen := make(map[string]bool)
				for _, ref := range entry.Branches {
					if ref.Repo != "repo" || ref.Path != dir || !slices.Contains(wantDeleted, ref.Branch) || ref.SHA != wantSHAs[ref.Branch] || seen[ref.Branch] {
						t.Errorf("incorrect journal ref: %+v", ref)
					}
					seen[ref.Branch] = true
				}
				runSoko(t, "undo")
				for branch, sha := range wantSHAs {
					if got := gitIn(t, dir, "rev-parse", "refs/heads/"+branch); got != sha {
						t.Errorf("undo restored %s at %s, want %s", branch, got, sha)
					}
				}
			})
		}
	}
}

func TestIntegration_CleanRequiresBranchPreimage(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Git failure fixture requires a POSIX shell")
	}
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "repo")
	branches := []string{"allowed", "blocked", "later"}
	initRepoWithBranches(t, dir, branches)
	runSokoInit(t, dir)
	wantSHAs := make(map[string]string)
	for _, branch := range branches {
		wantSHAs[branch] = gitIn(t, dir, "rev-parse", "refs/heads/"+branch)
	}
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("SOKO_CLEAN_REAL_GIT", realGit)
	wrapper := filepath.Join(t.TempDir(), "git")
	script := `#!/bin/sh
if [ "$1" = rev-parse ]; then
  for ref do
    case "$ref" in
      blocked|refs/heads/blocked)
        printf 'fixture cannot read branch tip\n' >&2
        exit 1 ;;
    esac
  done
fi
exec "$SOKO_CLEAN_REAL_GIT" "$@"
`
	if err := os.WriteFile(wrapper, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	originalGit := git.Binary()
	git.SetBinary(wrapper)
	t.Cleanup(func() { git.SetBinary(originalGit) })

	out, err := runSokoErr(t, "clean", "--force", "--json")
	if err == nil || err.Error() != "deleted 2 branches, 1 failed to delete" {
		t.Errorf("clean error = %v, want partial failure", err)
	}
	var rows []struct {
		Deleted []string `json:"deleted"`
		Failed  []struct {
			Branch    string `json:"branch"`
			Error     string `json:"error"`
			ErrorCode string `json:"error_code"`
		} `json:"failed"`
	}
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || !slices.Equal(rows[0].Deleted, []string{"allowed", "later"}) || len(rows[0].Failed) != 1 {
		t.Errorf("incorrect deletion outcomes: %s", out)
	} else if failure := rows[0].Failed[0]; failure.Branch != "blocked" || !strings.Contains(failure.Error, "fixture cannot read branch tip") || failure.ErrorCode != "git_failure" {
		t.Errorf("incorrect preimage failure: %+v", failure)
	}
	if !branchExists(t, dir, "blocked") {
		t.Error("clean deleted branch without a verified preimage")
	} else if got := gitIn(t, dir, "rev-parse", "refs/heads/blocked"); got != wantSHAs["blocked"] {
		t.Errorf("clean changed blocked branch to %s, want %s", got, wantSHAs["blocked"])
	}
	j, err := journal.Load()
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := j.Last()
	if !ok || len(entry.Branches) != 2 {
		t.Fatalf("journal must contain only successful deletions: %+v", entry)
	}
	for _, ref := range entry.Branches {
		if ref.Branch == "blocked" || ref.SHA != wantSHAs[ref.Branch] {
			t.Errorf("incorrect journal ref: %+v", ref)
		}
	}
	runSoko(t, "undo")
	for branch, sha := range wantSHAs {
		if got := gitIn(t, dir, "rev-parse", "refs/heads/"+branch); got != sha {
			t.Errorf("undo left %s at %s, want %s", branch, got, sha)
		}
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

func TestIntegration_CleanCheckoutStates(t *testing.T) {
	for _, tt := range []struct {
		name         string
		detached     bool
		worktree     bool
		wantBranches []string
	}{
		{name: "current branch", wantBranches: []string{"feat/done"}},
		{name: "detached HEAD", detached: true, wantBranches: []string{"feat/done", "feat/held"}},
		{name: "linked worktree", worktree: true, wantBranches: []string{"feat/done"}},
		{name: "detached HEAD with linked worktree", detached: true, worktree: true, wantBranches: []string{"feat/done"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			testEnv(t)
			base, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			dir := filepath.Join(base, "clean-checkouts")
			initRepoWithBranches(t, dir, []string{"feat/done", "feat/held"})
			wtGit(t, dir, "checkout", "-b", "feat/unmerged")
			wtGit(t, dir, "commit", "--allow-empty", "-m", "unmerged work")
			wtGit(t, dir, "checkout", "master")

			worktree := filepath.Join(base, "linked checkout")
			if tt.worktree {
				// Keep this checkout unregistered: Git's inventory must protect it.
				wtGit(t, dir, "worktree", "add", worktree, "feat/held")
			} else if !tt.detached {
				wtGit(t, dir, "checkout", "feat/held")
			}
			if tt.detached {
				wtGit(t, dir, "checkout", "--detach", "master")
			}
			head := wtGit(t, dir, "rev-parse", "HEAD")
			branch := wtGit(t, dir, "rev-parse", "--abbrev-ref", "HEAD")
			wantSHAs := make(map[string]string)
			for _, name := range tt.wantBranches {
				wantSHAs[name] = wtGit(t, dir, "rev-parse", "refs/heads/"+name)
			}
			runSokoInit(t, dir)

			out := runSoko(t, "clean", "--dry-run", "--json")
			var rows []struct {
				Branches []string `json:"branches"`
			}
			if err := json.Unmarshal([]byte(out), &rows); err != nil {
				t.Fatalf("parsing clean preview: %v\n%s", err, out)
			}
			if len(rows) != 1 || !slices.Equal(rows[0].Branches, tt.wantBranches) {
				t.Fatalf("clean preview = %s, want branches %v", out, tt.wantBranches)
			}

			runSoko(t, "clean", "--force")
			for _, name := range []string{"master", "feat/done", "feat/held", "feat/unmerged"} {
				if got, want := branchExists(t, dir, name), !slices.Contains(tt.wantBranches, name); got != want {
					t.Errorf("branch %s exists = %v, want %v", name, got, want)
				}
			}
			if got := wtGit(t, dir, "rev-parse", "HEAD"); got != head {
				t.Errorf("clean moved HEAD to %s, want %s", got, head)
			}
			if got := wtGit(t, dir, "rev-parse", "--abbrev-ref", "HEAD"); got != branch {
				t.Errorf("clean changed checkout to %s, want %s", got, branch)
			}
			if tt.worktree {
				if got := wtGit(t, worktree, "rev-parse", "--abbrev-ref", "HEAD"); got != "feat/held" {
					t.Errorf("clean changed linked checkout to %s", got)
				}
			}

			j, err := journal.Load()
			if err != nil {
				t.Fatal(err)
			}
			entry, ok := j.Last()
			if !ok || entry.Op != journal.OpClean || len(entry.Branches) != len(wantSHAs) {
				t.Fatalf("clean journal = %+v, want deleted branches %v", entry, tt.wantBranches)
			}
			for _, ref := range entry.Branches {
				if ref.Path != dir || ref.SHA != wantSHAs[ref.Branch] {
					t.Errorf("unexpected journal ref: %+v", ref)
				}
			}
			runSoko(t, "undo")
			for name, sha := range wantSHAs {
				if got := wtGit(t, dir, "rev-parse", "refs/heads/"+name); got != sha {
					t.Errorf("undo restored %s at %s, want %s", name, got, sha)
				}
			}
		})
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
