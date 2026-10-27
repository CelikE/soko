package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/CelikE/soko/internal/config"
	"github.com/CelikE/soko/internal/git"
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
// and applies the exact saved stash, retaining its backup.
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
	if !strings.Contains(out, "feat/sso (stash restored; backup retained)") {
		t.Errorf("ctx switch = %q, want restored stash and retained backup", out)
	}

	if br := ctxGit(t, dirty, "symbolic-ref", "--short", "HEAD"); br != "feat/sso" {
		t.Errorf("after switch, branch = %q, want feat/sso", br)
	}
	if _, err := os.Stat(wip); err != nil {
		t.Errorf("after switch, wip file should be restored: %v", err)
	}
}

// Successful switches restore tracked contents, not the saved index. Failed
// saves use a different, index-preserving rollback path (tested below).
func TestIntegration_CtxSwitchRestoresContentsWithoutStaging(t *testing.T) {
	for _, tc := range []struct {
		name, staged, working, untracked string
	}{
		{name: "staged-only", staged: "staged work\n", working: "staged work\n"},
		{name: "unstaged-only", working: "unstaged work\n"},
		{name: "mixed-same-file", staged: "staged work\n", working: "unstaged work\n"},
		{name: "untracked-only", untracked: "new work\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testEnv(t)
			repo := filepath.Join(t.TempDir(), "ctx-index")
			initRepo(t, repo)
			runSokoInit(t, repo)
			ctxGit(t, repo, "checkout", "-b", "feature")
			tracked := filepath.Join(repo, "README.md")
			if tc.staged != "" {
				ctxWrite(t, tracked, tc.staged)
				ctxGit(t, repo, "add", "README.md")
			}
			if tc.working != "" {
				ctxWrite(t, tracked, tc.working)
			}
			if tc.untracked != "" {
				ctxWrite(t, filepath.Join(repo, "new.txt"), tc.untracked)
			}

			runSoko(t, "ctx", "save", "index-policy")
			entry, ok := config.GetContext(loadRegistry(t), "index-policy")
			if !ok || len(entry.Repos) != 1 || !entry.Repos[0].Stashed || entry.Repos[0].StashOID == "" {
				t.Fatalf("saved context = %+v, exists = %v", entry, ok)
			}
			oid := entry.Repos[0].StashOID
			if got := ctxGit(t, repo, "status", "--porcelain"); got != "" {
				t.Fatalf("save left dirty checkout: %s", got)
			}
			ctxGit(t, repo, "checkout", "master")
			runSoko(t, "ctx", "switch", "index-policy")
			if got := ctxGit(t, repo, "symbolic-ref", "--short", "HEAD"); got != "feature" {
				t.Errorf("branch = %s, want feature", got)
			}
			wantTracked := "# test\n"
			if tc.working != "" {
				wantTracked = tc.working
			}
			ctxContents(t, tracked, wantTracked)
			if got := ctxGit(t, repo, "show", ":README.md"); got != "# test" {
				t.Errorf("index content = %q, want original commit", got)
			}
			if got := ctxGit(t, repo, "diff", "--cached", "--name-only"); got != "" {
				t.Errorf("restored staged paths = %q, want none", got)
			}
			if tc.untracked != "" {
				ctxContents(t, filepath.Join(repo, "new.txt"), tc.untracked)
				if got := ctxGit(t, repo, "ls-files", "--others", "--exclude-standard"); got != "new.txt" {
					t.Errorf("restored untracked paths = %q, want new.txt", got)
				}
			}
			if got := ctxGit(t, repo, "stash", "list", "--format=%H"); got != oid {
				t.Errorf("backup = %q, want %s", got, oid)
			}
			if got := ctxGit(t, repo, "for-each-ref", "--format=%(objectname)", "refs/soko/ctx-restored/"+oid); got != oid {
				t.Errorf("completion = %q, want %s", got, oid)
			}
		})
	}
}

func TestIntegration_CtxDetachedRoundTrip(t *testing.T) {
	for _, dirty := range []bool{false, true} {
		name := "clean"
		if dirty {
			name = "dirty"
		}
		t.Run(name, func(t *testing.T) {
			testEnv(t)
			repo := filepath.Join(t.TempDir(), "ctx-detached")
			initRepo(t, repo)
			runSokoInit(t, repo)
			original := ctxGit(t, repo, "rev-parse", "HEAD")
			ctxGit(t, repo, "checkout", "--detach", original)
			wip := filepath.Join(repo, "wip.txt")
			if dirty {
				if err := os.WriteFile(wip, []byte("detached work\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			runSoko(t, "ctx", "save", "detached", "--json")
			entry, ok := config.GetContext(loadRegistry(t), "detached")
			if !ok || len(entry.Repos) != 1 {
				t.Fatalf("saved context = %+v, %v", entry, ok)
			}
			member := entry.Repos[0]
			if !member.Detached || member.Branch != original || member.Stashed != dirty || (member.StashOID != "") != dirty {
				t.Errorf("saved detached state = %+v", member)
			}
			if status := ctxGit(t, repo, "status", "--porcelain"); status != "" {
				t.Fatalf("save left dirty checkout: %s", status)
			}
			ctxGit(t, repo, "checkout", "master")
			ctxGit(t, repo, "commit", "--allow-empty", "-m", "later work")
			later := ctxGit(t, repo, "rev-parse", "HEAD")
			runSoko(t, "ctx", "switch", "detached", "--json")
			branch, detached, err := git.CurrentBranch(t.Context(), repo)
			if err != nil || !detached || branch != original {
				t.Errorf("restored HEAD = %q, %v, %v; want detached %s", branch, detached, err, original)
			}
			if got := ctxGit(t, repo, "rev-parse", "master"); got != later {
				t.Errorf("restore rewound named branch: %s, want %s", got, later)
			}
			if dirty {
				if data, err := os.ReadFile(wip); err != nil || string(data) != "detached work\n" {
					t.Errorf("restored work = %q, %v", data, err)
				}
				if got := ctxGit(t, repo, "stash", "list", "--format=%H"); got != member.StashOID {
					t.Errorf("restore lost backup: %s, want %s", got, member.StashOID)
				}
			} else if status := ctxGit(t, repo, "status", "--porcelain"); status != "" {
				t.Errorf("clean restore dirtied checkout: %s", status)
			}
		})
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

func TestIntegration_CtxSaveConfigFailureRestoresWork(t *testing.T) {
	for _, mode := range []string{"text", "json", "force"} {
		t.Run(mode, func(t *testing.T) {
			cfgRoot := testEnv(t)
			base := t.TempDir()
			dirty := []string{filepath.Join(base, "ctx-first"), filepath.Join(base, "ctx-second")}
			clean := filepath.Join(base, "ctx-clean")
			for _, repo := range append(append([]string(nil), dirty...), clean) {
				initRepo(t, repo)
				runSokoInit(t, repo)
			}
			if mode == "force" {
				runSoko(t, "ctx", "save", "work")
			}

			statuses := make(map[string]string)
			stashes := make(map[string]string)
			for _, repo := range dirty {
				readme := filepath.Join(repo, "README.md")
				if err := os.WriteFile(readme, []byte("older work\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				// Older stashes with the same context name must not be restored.
				ctxGit(t, repo, "stash", "push", "-m", "soko-ctx:work")
				stashes[repo] = ctxGit(t, repo, "stash", "list", "--format=%H %gs")
				if err := os.WriteFile(readme, []byte("staged work\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				ctxGit(t, repo, "add", "README.md")
				if err := os.WriteFile(readme, []byte("unstaged work\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(repo, "untracked.txt"), []byte("new work\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				statuses[repo] = ctxGit(t, repo, "status", "--porcelain")
			}

			cfgDir := filepath.Join(cfgRoot, "soko")
			cfgPath := filepath.Join(cfgDir, "config.yaml")
			before, err := os.ReadFile(cfgPath)
			if err != nil {
				t.Fatal(err)
			}
			// Loading remains possible, but atomic config writes must fail.
			if err := os.Chmod(cfgDir, 0o500); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chmod(cfgDir, 0o700) })
			if probe, err := os.CreateTemp(cfgDir, ".write-probe-"); err == nil {
				_ = probe.Close()
				_ = os.Remove(probe.Name())
				t.Skip("directory permissions do not prevent config writes")
			}

			args := []string{"ctx", "save", "work"}
			switch mode {
			case "json":
				args = append(args, "--json")
			case "force":
				args = append(args, "--force")
			}
			out, err := runSokoErr(t, args...)
			if err == nil || !strings.Contains(err.Error(), "saving config") {
				t.Fatalf("ctx save error = %v, want config write failure", err)
			}
			if strings.Contains(out, "saved") {
				t.Errorf("failed save reported success: %s", out)
			}
			for _, repo := range dirty {
				if got := ctxGit(t, repo, "status", "--porcelain"); got != statuses[repo] {
					t.Errorf("%s status = %q, want %q", repo, got, statuses[repo])
				}
				if got := ctxGit(t, repo, "show", ":README.md"); got != "staged work" {
					t.Errorf("%s staged content = %q, want staged work", repo, got)
				}
				for name, want := range map[string]string{"README.md": "unstaged work\n", "untracked.txt": "new work\n"} {
					got, err := os.ReadFile(filepath.Join(repo, name))
					if err != nil || string(got) != want {
						t.Errorf("%s/%s = %q, %v; want %q", repo, name, got, err, want)
					}
				}
				got := ctxGit(t, repo, "stash", "list", "--format=%H %gs")
				if !strings.HasSuffix(got, stashes[repo]) || len(strings.Split(got, "\n")) != 2 {
					t.Errorf("%s stashes = %q, want retained backup and unchanged older stash %q", repo, got, stashes[repo])
				}
			}
			if got := ctxGit(t, clean, "status", "--porcelain"); got != "" {
				t.Errorf("clean repo was modified: %s", got)
			}
			after, err := os.ReadFile(cfgPath)
			if err != nil || !bytes.Equal(after, before) {
				t.Errorf("config changed after failed save: %s, %v", after, err)
			}
		})
	}
}

func TestIntegration_CtxSavePartialStashFailureRestoresWork(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("git fault injection requires a POSIX shell")
	}
	for _, partial := range []bool{false, true} {
		name := "no captured repos"
		if partial {
			name = "partial context"
		}
		t.Run(name, func(t *testing.T) {
			testEnv(t)
			repo := filepath.Join(t.TempDir(), "ctx-failed-stash")
			initRepo(t, repo)
			runSokoInit(t, repo)
			if partial {
				clean := filepath.Join(t.TempDir(), "ctx-clean")
				initRepo(t, clean)
				runSokoInit(t, clean)
			}
			if err := os.WriteFile(filepath.Join(repo, "work.txt"), []byte("unsaved work\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			before := ctxGit(t, repo, "status", "--porcelain")
			realGit, err := exec.LookPath("git")
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv("SOKO_TEST_REAL_GIT", realGit)
			wrapper := filepath.Join(t.TempDir(), "git")
			script := `#!/bin/sh
"$SOKO_TEST_REAL_GIT" "$@"
status=$?
if [ "$status" -eq 0 ] && [ "$1" = stash ] && [ "$2" = push ]; then
  printf 'injected failure after stash creation\n' >&2
  exit 1
fi
exit "$status"
`
			if err := os.WriteFile(wrapper, []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
			originalGit := git.Binary()
			git.SetBinary(wrapper)
			t.Cleanup(func() { git.SetBinary(originalGit) })

			_, err = runSokoErr(t, "ctx", "save", "work", "--json")
			if err == nil {
				t.Fatal("ctx save succeeded despite a stash failure")
			}
			if got := ctxGit(t, repo, "status", "--porcelain"); got != before {
				t.Errorf("work was not restored: status = %q, want %q", got, before)
			}
			data, err := os.ReadFile(filepath.Join(repo, "work.txt"))
			if err != nil || string(data) != "unsaved work\n" {
				t.Errorf("work = %q, %v", data, err)
			}
			if got := ctxGit(t, repo, "stash", "list"); !strings.Contains(got, "soko-ctx:work") {
				t.Errorf("failed capture lost its recovery backup: %s", got)
			}
			cfg, err := config.Load()
			if err != nil {
				t.Fatal(err)
			}
			entry, exists := config.GetContext(cfg, "work")
			if partial {
				if !exists || len(entry.Repos) != 1 || entry.Repos[0].Name != "ctx-clean" {
					t.Errorf("partial context = %+v, exists = %v; want only clean repo", entry, exists)
				}
			} else if exists {
				t.Errorf("failed save persisted a context: %+v", entry)
			}
		})
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

// TestIntegration_CtxStashMissing verifies a missing intended stash fails
// before checkout instead of reporting success without restoring saved work.
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

	// Both stash-list entry and per-worktree ownership have been removed.
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	oid := cfg.Contexts["lost"].Repos[0].StashOID
	ctxGit(t, repo, "stash", "drop")
	ctxGit(t, repo, "update-ref", "-d", "refs/worktree/soko/ctx/"+oid, oid)
	ctxGit(t, repo, "checkout", "master")

	out, err := runSokoErr(t, "ctx", "switch", "lost")
	if err == nil {
		t.Fatal("switch with missing saved work must fail")
	}
	if !strings.Contains(out, "stash ownership missing") {
		t.Errorf("ctx switch = %q, want missing ownership refusal", out)
	}
	if br := ctxGit(t, repo, "symbolic-ref", "--short", "HEAD"); br != "master" {
		t.Errorf("branch after refused switch = %q, want master", br)
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
