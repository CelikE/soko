package cli_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/CelikE/soko/internal/config"
	"github.com/CelikE/soko/internal/git"
)

func ctxWrite(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func ctxContents(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil || string(got) != want {
		t.Errorf("%s = %q, %v; want %q", path, got, err, want)
	}
}

// ctxGitWrapper changes scheduling, never Git's output. Each barrier waits for
// a completed Git operation, with a bounded wait so failures cannot hang tests.
func ctxGitWrapper(t *testing.T, body string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("deterministic Git scheduling requires a POSIX shell")
	}
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("SOKO_CTX_REAL_GIT", realGit)
	t.Setenv("SOKO_CTX_BARRIER", t.TempDir())
	wrapper := filepath.Join(t.TempDir(), "git")
	script := `#!/bin/sh
wait_for() {
  count=0
  while [ ! -f "$SOKO_CTX_BARRIER/$1" ]; do
    count=$((count + 1))
    if [ "$count" -ge 1000 ]; then
      printf 'timed out waiting for %s\n' "$1" >&2
      exit 1
    fi
    sleep 0.01
  done
}
` + body
	if err := os.WriteFile(wrapper, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	original := git.Binary()
	git.SetBinary(wrapper)
	t.Cleanup(func() { git.SetBinary(original) })
}

func TestIntegration_CtxLinkedWorktreeOwnership(t *testing.T) {
	for _, saveOrder := range []string{"AB", "BA"} {
		for _, restoreOrder := range []string{"AB", "BA"} {
			t.Run("save-"+saveOrder+"-restore-"+restoreOrder, func(t *testing.T) {
				testEnv(t)
				t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
				t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
				base := t.TempDir()
				a, b := filepath.Join(base, "A"), filepath.Join(base, "B")
				initRepo(t, a)
				ctxGit(t, a, "worktree", "add", "-b", "branch-b", b)
				cfg := &config.Config{Repos: []config.RepoEntry{
					{Name: "A", Path: a}, {Name: "B", Path: b, WorktreeOf: "A"},
				}}
				if err := config.Save(cfg); err != nil {
					t.Fatal(err)
				}
				// Canary predates the context and must survive every ordering.
				ctxWrite(t, filepath.Join(a, "canary.txt"), "unrelated stash\n")
				ctxGit(t, a, "stash", "push", "-u", "-m", "unrelated-canary")
				canary := ctxGit(t, a, "stash", "list", "--format=%H %gs")
				ctxWrite(t, filepath.Join(a, "README.md"), "A-only\n")
				ctxWrite(t, filepath.Join(b, "README.md"), "B-only\n")
				ctxGitWrapper(t, `
who=${PWD##*/}
first=${SOKO_CTX_ORDER%?}
second=${SOKO_CTX_ORDER#?}
if [ "$SOKO_CTX_PHASE" = save ] && [ "$1 $2" = "stash push" ] && [ "$who" = "$second" ]; then
  wait_for "saved-$first"
fi
if [ "$SOKO_CTX_PHASE" = restore ] && [ "$1" = checkout ] && [ "$who" = "$second" ]; then
  wait_for "restored-$first"
fi
"$SOKO_CTX_REAL_GIT" "$@"
status=$?
if [ "$status" -eq 0 ] && [ "$1 $2" = "stash push" ]; then
  touch "$SOKO_CTX_BARRIER/saved-$who"
fi
if [ "$1" = stash ] && { [ "$2" = pop ] || [ "$2" = apply ]; }; then
  touch "$SOKO_CTX_BARRIER/restored-$who"
fi
exit "$status"
`)
				t.Setenv("SOKO_CTX_PHASE", "save")
				t.Setenv("SOKO_CTX_ORDER", saveOrder)
				if out, err := runSokoErr(t, "ctx", "save", "ownership", "--json"); err != nil {
					t.Fatalf("save failed: %v\n%s\nstashes: %q", err, out, ctxGit(t, a, "stash", "list", "--format=%H%x00%gs"))
				}
				t.Setenv("SOKO_CTX_PHASE", "restore")
				t.Setenv("SOKO_CTX_ORDER", restoreOrder)
				runSoko(t, "ctx", "switch", "ownership", "--json")
				ctxContents(t, filepath.Join(a, "README.md"), "A-only\n")
				ctxContents(t, filepath.Join(b, "README.md"), "B-only\n")
				remaining := ctxGit(t, a, "stash", "list", "--format=%H %gs")
				if !strings.Contains(remaining, canary) {
					t.Errorf("unrelated stash changed: %s; want %s", remaining, canary)
				}
				for _, repo := range []string{a, b} {
					if _, err := os.Stat(filepath.Join(repo, "canary.txt")); !os.IsNotExist(err) {
						t.Errorf("unrelated stash applied in %s: %v", repo, err)
					}
				}
			})
		}
	}
}

func TestIntegration_CtxStashInsertionAfterLookup(t *testing.T) {
	for _, mutation := range []string{"insert", "remove-owned", "reorder"} {
		t.Run(mutation, func(t *testing.T) {
			testEnv(t)
			t.Setenv("SOKO_CTX_MUTATION", mutation)
			repo := filepath.Join(t.TempDir(), "A")
			initRepo(t, repo)
			runSokoInit(t, repo)
			ctxWrite(t, filepath.Join(repo, "README.md"), "context work\n")
			runSoko(t, "ctx", "save", "ownership", "--json")
			ctxGitWrapper(t, `
if { [ "$1 $2" = "stash list" ] || [ "$1" = for-each-ref ]; } && [ ! -f "$SOKO_CTX_BARRIER/inserted" ]; then
  "$SOKO_CTX_REAL_GIT" "$@" > "$SOKO_CTX_BARRIER/captured" || exit 1
  owned=$("$SOKO_CTX_REAL_GIT" rev-parse refs/stash) || exit 1
  subject=$("$SOKO_CTX_REAL_GIT" log -1 --format=%s "$owned") || exit 1
  printf 'unrelated work\n' > unrelated.txt
  "$SOKO_CTX_REAL_GIT" stash push -u -m unrelated-canary > /dev/null || exit 1
  "$SOKO_CTX_REAL_GIT" rev-parse refs/stash > "$SOKO_CTX_BARRIER/inserted"
  if [ "$SOKO_CTX_MUTATION" != insert ]; then
    "$SOKO_CTX_REAL_GIT" stash drop 'stash@{1}' > /dev/null || exit 1
  fi
  if [ "$SOKO_CTX_MUTATION" = reorder ]; then
    "$SOKO_CTX_REAL_GIT" stash store -m "$subject" "$owned" || exit 1
  fi
  "$SOKO_CTX_REAL_GIT" stash list '--format=%H %gs' > "$SOKO_CTX_BARRIER/expected"
  cat "$SOKO_CTX_BARRIER/captured"
  exit 0
fi
exec "$SOKO_CTX_REAL_GIT" "$@"
`)
			runSoko(t, "ctx", "switch", "ownership", "--json")
			ctxContents(t, filepath.Join(repo, "README.md"), "context work\n")
			if _, err := os.Stat(filepath.Join(repo, "unrelated.txt")); !os.IsNotExist(err) {
				t.Errorf("unrelated stash was applied: %v", err)
			}
			inserted, err := os.ReadFile(filepath.Join(os.Getenv("SOKO_CTX_BARRIER"), "inserted"))
			if err != nil {
				t.Fatal(err)
			}
			if remaining := ctxGit(t, repo, "stash", "list", "--format=%H"); !strings.Contains(remaining, strings.TrimSpace(string(inserted))) {
				t.Errorf("unrelated stash consumed: %s; want %s", remaining, inserted)
			}
			expected, err := os.ReadFile(filepath.Join(os.Getenv("SOKO_CTX_BARRIER"), "expected"))
			if err != nil {
				t.Fatal(err)
			}
			if got := ctxGit(t, repo, "stash", "list", "--format=%H %gs"); got != strings.TrimSpace(string(expected)) {
				t.Errorf("restore changed stash entries: %s; want %s", got, expected)
			}
		})
	}
}

func TestIntegration_CtxCaptureIdentityAfterForeignPush(t *testing.T) {
	testEnv(t)
	repo := filepath.Join(t.TempDir(), "capture")
	initRepo(t, repo)
	runSokoInit(t, repo)
	ctxWrite(t, filepath.Join(repo, "README.md"), "owned work\n")
	ctxGitWrapper(t, `
"$SOKO_CTX_REAL_GIT" "$@" || exit 1
if [ "$1 $2" = "stash push" ]; then
  printf 'foreign work\n' > foreign.txt
  "$SOKO_CTX_REAL_GIT" stash push -u -m foreign-canary > /dev/null || exit 1
fi
`)
	runSoko(t, "ctx", "save", "capture")
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	owned := cfg.Contexts["capture"].Repos[0].StashOID
	foreign := ctxGit(t, repo, "rev-parse", "refs/stash")
	if owned == "" || owned == foreign {
		t.Fatalf("capture recorded tip %q instead of owned commit %q", foreign, owned)
	}
	runSoko(t, "ctx", "switch", "capture")
	ctxContents(t, filepath.Join(repo, "README.md"), "owned work\n")
	if _, err := os.Stat(filepath.Join(repo, "foreign.txt")); !os.IsNotExist(err) {
		t.Errorf("foreign stash applied: %v", err)
	}
	if got := ctxGit(t, repo, "rev-parse", "refs/stash"); got != foreign {
		t.Errorf("foreign stash changed: %s, want %s", got, foreign)
	}
}

func TestIntegration_CtxLegacyAndInvalidIdentityRefuse(t *testing.T) {
	for _, oid := range []string{"", "stash@{0}", "deadbeef", strings.Repeat("0", 40)} {
		t.Run("identity-"+oid, func(t *testing.T) {
			testEnv(t)
			repo := filepath.Join(t.TempDir(), "legacy")
			initRepo(t, repo)
			runSokoInit(t, repo)
			// A legacy clean context needs no stash ownership metadata.
			runSoko(t, "ctx", "save", "clean")
			ctxGit(t, repo, "checkout", "-b", "feature")
			ctxWrite(t, filepath.Join(repo, "README.md"), "saved work\n")
			runSoko(t, "ctx", "save", "legacy")
			cfg, err := config.Load()
			if err != nil {
				t.Fatal(err)
			}
			cfg.Contexts["legacy"].Repos[0].StashOID = oid
			if err := config.Save(cfg); err != nil {
				t.Fatal(err)
			}
			runSoko(t, "ctx", "switch", "clean")
			before := ctxGit(t, repo, "stash", "list", "--format=%H %gs")
			out, err := runSokoErr(t, "ctx", "switch", "legacy", "--json")
			if err == nil || !strings.Contains(out, `"status": "failed"`) {
				t.Fatalf("unsafe identity restored successfully: %s, %v", out, err)
			}
			ctxContents(t, filepath.Join(repo, "README.md"), "# test\n")
			if br := ctxGit(t, repo, "symbolic-ref", "--short", "HEAD"); br != "master" {
				t.Errorf("refused restore checked out %s", br)
			}
			if after := ctxGit(t, repo, "stash", "list", "--format=%H %gs"); after != before {
				t.Errorf("unsafe restore changed stashes: %s; want %s", after, before)
			}
		})
	}
}

func TestIntegration_CtxPartialRestoreRecovery(t *testing.T) {
	testEnv(t)
	base := t.TempDir()
	a, b := filepath.Join(base, "A"), filepath.Join(base, "B")
	for _, repo := range []string{a, b} {
		initRepo(t, repo)
		runSokoInit(t, repo)
		ctxWrite(t, filepath.Join(repo, "README.md"), filepath.Base(repo)+" work\n")
	}
	runSoko(t, "ctx", "save", "partial")
	stashes := map[string]string{}
	for _, repo := range []string{a, b} {
		stashes[repo] = ctxGit(t, repo, "stash", "list", "--format=%H %gs")
	}
	// The recorded branch has advanced incompatibly since capture.
	ctxWrite(t, filepath.Join(a, "README.md"), "conflicting committed work\n")
	ctxGit(t, a, "commit", "-am", "advance branch")
	out, err := runSokoErr(t, "ctx", "switch", "partial", "--json")
	if err == nil || !strings.Contains(out, `"status": "failed"`) {
		t.Fatalf("conflicted restore should fail: %s, %v", out, err)
	}
	ctxContents(t, filepath.Join(b, "README.md"), "B work\n")
	for _, repo := range []string{a, b} {
		if got := ctxGit(t, repo, "stash", "list", "--format=%H %gs"); got != stashes[repo] {
			t.Errorf("%s lost recovery backup: %s, want %s", repo, got, stashes[repo])
		}
	}
	// Resolve the conflict back to capture's base in this disposable fixture.
	ctxWrite(t, filepath.Join(a, "README.md"), "# test\n")
	ctxGit(t, a, "add", "README.md")
	ctxGit(t, a, "commit", "-m", "resolve to base")
	// A successful member is now clean with different work: retry must not
	// replay its old backup merely because that stash still exists.
	ctxWrite(t, filepath.Join(b, "README.md"), "B newer committed work\n")
	ctxGit(t, b, "commit", "-am", "newer work")
	runSoko(t, "ctx", "switch", "partial")
	ctxContents(t, filepath.Join(a, "README.md"), "A work\n")
	ctxContents(t, filepath.Join(b, "README.md"), "B newer committed work\n")
}

func TestIntegration_CtxSwitchPreservesConcurrentSave(t *testing.T) {
	for _, force := range []bool{false, true} {
		t.Run(map[bool]string{false: "different-context", true: "force-same-context"}[force], func(t *testing.T) {
			cfgRoot := testEnv(t)
			base := t.TempDir()
			a, b := filepath.Join(base, "A"), filepath.Join(base, "B")
			for _, repo := range []string{a, b} {
				initRepo(t, repo)
				runSokoInit(t, repo)
			}
			ctxWrite(t, filepath.Join(a, "README.md"), "first work\n")
			runSoko(t, "ctx", "save", "first", "A")
			cfgPath := filepath.Join(cfgRoot, "soko", "config.yaml")
			before, err := os.ReadFile(cfgPath)
			if err != nil {
				t.Fatal(err)
			}
			ctxWrite(t, filepath.Join(b, "README.md"), "second work\n")
			name := "second"
			args := []string{"ctx", "save", name, "B"}
			if force {
				name = "first"
				args = []string{"ctx", "save", name, "B", "--force"}
			}
			runSoko(t, args...)
			concurrent, err := os.ReadFile(cfgPath)
			if err != nil {
				t.Fatal(err)
			}
			// Hold publication of this real save's config snapshot until the
			// older switch has loaded its snapshot and applied A's changes.
			payload := filepath.Join(t.TempDir(), "completed-save.yaml")
			ctxWrite(t, payload, string(concurrent))
			ctxWrite(t, cfgPath, string(before))
			t.Setenv("SOKO_CTX_CONCURRENT", payload)
			t.Setenv("SOKO_CTX_CONFIG", cfgPath)
			ctxGitWrapper(t, `
"$SOKO_CTX_REAL_GIT" "$@" || exit 1
if [ "$1 $2" = "stash apply" ]; then
  cp "$SOKO_CTX_CONCURRENT" "$SOKO_CTX_CONFIG.next" || exit 1
  mv "$SOKO_CTX_CONFIG.next" "$SOKO_CTX_CONFIG" || exit 1
fi
`)
			runSoko(t, "ctx", "switch", "first", "--json")
			after, err := os.ReadFile(cfgPath)
			if err != nil || !bytes.Equal(after, concurrent) {
				t.Fatalf("switch replaced completed concurrent save:\n%s\nwant:\n%s\n%v", after, concurrent, err)
			}
			runSoko(t, "ctx", "switch", name)
			ctxContents(t, filepath.Join(a, "README.md"), "first work\n")
			ctxContents(t, filepath.Join(b, "README.md"), "second work\n")
		})
	}
}

func TestIntegration_CtxCompletionFailurePreservesRecovery(t *testing.T) {
	testEnv(t)
	repo := filepath.Join(t.TempDir(), "completion")
	initRepo(t, repo)
	runSokoInit(t, repo)
	ctxWrite(t, filepath.Join(repo, "README.md"), "saved work\n")
	runSoko(t, "ctx", "save", "work")
	before := ctxGit(t, repo, "stash", "list", "--format=%H %gs")
	ctxGitWrapper(t, `
if [ "$1" = update-ref ]; then
  printf 'injected completion failure\n' >&2
  exit 1
fi
exec "$SOKO_CTX_REAL_GIT" "$@"
`)
	out, err := runSokoErr(t, "ctx", "switch", "work", "--json")
	if err == nil || !strings.Contains(out, "work restored but could not record completion") {
		t.Fatalf("completion failure reported success: %s, %v", out, err)
	}
	ctxContents(t, filepath.Join(repo, "README.md"), "saved work\n")
	if got := ctxGit(t, repo, "stash", "list", "--format=%H %gs"); got != before {
		t.Errorf("completion failure lost backup: %s, want %s", got, before)
	}
}

func TestIntegration_CtxRefusesReassignedWorktree(t *testing.T) {
	testEnv(t)
	base := t.TempDir()
	a, b := filepath.Join(base, "A"), filepath.Join(base, "B")
	initRepo(t, a)
	ctxGit(t, a, "checkout", "--detach")
	ctxGit(t, a, "worktree", "add", "--detach", b)
	if err := config.Save(&config.Config{Repos: []config.RepoEntry{{Name: "member", Path: a}}}); err != nil {
		t.Fatal(err)
	}
	ctxWrite(t, filepath.Join(a, "README.md"), "A owned work\n")
	runSoko(t, "ctx", "save", "ownership")
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	// A registry alias can later identify a different linked checkout. Both
	// are detached so Git's branch-in-use check cannot mask wrong ownership.
	cfg.Repos[0].Path = b
	if err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	before := ctxGit(t, a, "stash", "list", "--format=%H %gs")
	out, err := runSokoErr(t, "ctx", "switch", "ownership", "--json")
	if err == nil || !strings.Contains(out, `"status": "failed"`) {
		t.Errorf("reassigned member restored into wrong checkout: %s, %v", out, err)
	}
	ctxContents(t, filepath.Join(a, "README.md"), "# test\n")
	ctxContents(t, filepath.Join(b, "README.md"), "# test\n")
	if after := ctxGit(t, a, "stash", "list", "--format=%H %gs"); after != before {
		t.Errorf("reassigned member changed stash list: %s, want %s", after, before)
	}
}

func TestIntegration_CtxRefusesRecycledWorktree(t *testing.T) {
	for _, samePath := range []bool{false, true} {
		t.Run(map[bool]string{false: "different-path", true: "same-path"}[samePath], func(t *testing.T) {
			testEnv(t)
			base := t.TempDir()
			a, old := filepath.Join(base, "A"), filepath.Join(base, "old", "B")
			initRepo(t, a)
			ctxGit(t, a, "worktree", "add", "--detach", old)
			if err := config.Save(&config.Config{Repos: []config.RepoEntry{{Name: "member", Path: old}}}); err != nil {
				t.Fatal(err)
			}
			ctxWrite(t, filepath.Join(old, "README.md"), "original B work\n")
			runSoko(t, "ctx", "save", "ownership")
			oldGitDir := ctxGit(t, old, "rev-parse", "--absolute-git-dir")
			before := ctxGit(t, a, "stash", "list", "--format=%H %gs")
			ctxGit(t, a, "worktree", "remove", old)
			replacement := old
			if !samePath {
				replacement = filepath.Join(base, "new", "B")
			}
			ctxGit(t, a, "worktree", "add", "--detach", replacement)
			if got := ctxGit(t, replacement, "rev-parse", "--absolute-git-dir"); got != oldGitDir {
				t.Fatalf("fixture did not recycle Git directory: %s, want %s", got, oldGitDir)
			}
			cfg, err := config.Load()
			if err != nil {
				t.Fatal(err)
			}
			cfg.Repos[0].Path = replacement
			if err := config.Save(cfg); err != nil {
				t.Fatal(err)
			}
			out, err := runSokoErr(t, "ctx", "switch", "ownership", "--json")
			if err == nil || !strings.Contains(out, `"status": "failed"`) {
				t.Errorf("recycled checkout accepted original ownership: %s, %v", out, err)
			}
			ctxContents(t, filepath.Join(replacement, "README.md"), "# test\n")
			if after := ctxGit(t, a, "stash", "list", "--format=%H %gs"); after != before {
				t.Errorf("replacement checkout changed stashes: %s, want %s", after, before)
			}
		})
	}
}

func TestIntegration_CtxPinnedBackupSurvivesStashRemovalAndGC(t *testing.T) {
	for _, linked := range []bool{false, true} {
		t.Run(map[bool]string{false: "standalone", true: "linked"}[linked], func(t *testing.T) {
			testEnv(t)
			base := t.TempDir()
			primary := filepath.Join(base, "A")
			initRepo(t, primary)
			repo := primary
			if linked {
				repo = filepath.Join(base, "B")
				ctxGit(t, primary, "worktree", "add", "-b", "branch-b", repo)
			}
			if err := config.Save(&config.Config{Repos: []config.RepoEntry{{Name: "member", Path: repo}}}); err != nil {
				t.Fatal(err)
			}
			ctxWrite(t, filepath.Join(repo, "README.md"), "saved tracked work\n")
			ctxWrite(t, filepath.Join(repo, "new.txt"), "saved untracked work\n")
			runSoko(t, "ctx", "save", "pinned")
			cfg, err := config.Load()
			if err != nil {
				t.Fatal(err)
			}
			oid := cfg.Contexts["pinned"].Repos[0].StashOID
			ctxGit(t, primary, "stash", "clear")
			ctxGit(t, primary, "reflog", "expire", "--expire=now", "--all")
			ctxGit(t, primary, "gc", "--prune=now")
			ctxGit(t, primary, "cat-file", "-e", oid+"^{commit}")
			ctxGit(t, primary, "cat-file", "-e", oid+"^3^{commit}")
			runSoko(t, "ctx", "switch", "pinned")
			ctxContents(t, filepath.Join(repo, "README.md"), "saved tracked work\n")
			ctxContents(t, filepath.Join(repo, "new.txt"), "saved untracked work\n")
			// Completion pins the backup even if the checkout-local ownership
			// ref later disappears; recovery never depends on dangling objects.
			ctxGit(t, repo, "update-ref", "-d", "refs/worktree/soko/ctx/"+oid, oid)
			ctxGit(t, primary, "update-ref", "-d", "refs/soko/ctx-backups/"+oid, oid)
			ctxGit(t, primary, "reflog", "expire", "--expire=now", "--all")
			ctxGit(t, primary, "gc", "--prune=now")
			ctxGit(t, primary, "cat-file", "-e", oid+"^{commit}")
			ctxGit(t, primary, "cat-file", "-e", oid+"^3^{commit}")
		})
	}
}
