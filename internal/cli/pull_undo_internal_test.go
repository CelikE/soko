package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CelikE/soko/internal/git"
	"github.com/CelikE/soko/internal/journal"
	"github.com/CelikE/soko/internal/ui"
)

func pullUndoGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := git.Run(context.Background(), dir, args...)
	if err != nil {
		t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}
	return out
}

func pullUndoWrite(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// setupJournaledPull exercises the dashboard's real fast-forward and journal
// writer against a local remote, without requiring a terminal or network.
func setupJournaledPull(t *testing.T) (dir, before, after string) {
	t.Helper()
	p := setupJournaledPulls(t, "repo")[0]
	return p.Path, p.SHA, p.PostSHA
}

func setupJournaledPulls(t *testing.T, names ...string) []journal.PullRef {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_TEMPLATE_DIR", t.TempDir())
	t.Setenv("GIT_ALLOW_PROTOCOL", "file")
	base := t.TempDir()
	pulls := make([]journal.PullRef, 0, len(names))
	targets := make([]ui.PullTarget, 0, len(names))
	for _, name := range names {
		source := filepath.Join(base, "source-"+name)
		dir := filepath.Join(base, name)
		pullUndoGit(t, base, "init", "--initial-branch=main", source)
		pullUndoGit(t, source, "config", "user.name", "Test")
		pullUndoGit(t, source, "config", "user.email", "test@example.com")
		pullUndoWrite(t, source, "README.md", "before pull\n")
		pullUndoGit(t, source, "add", ".")
		pullUndoGit(t, source, "commit", "-m", "initial")
		before := pullUndoGit(t, source, "rev-parse", "HEAD")
		pullUndoGit(t, base, "clone", source, dir)
		pullUndoGit(t, dir, "config", "user.name", "Test")
		pullUndoGit(t, dir, "config", "user.email", "test@example.com")
		pullUndoWrite(t, source, "README.md", "after pull\n")
		pullUndoGit(t, source, "commit", "-am", "remote update")
		after := pullUndoGit(t, source, "rev-parse", "HEAD")
		pulls = append(pulls, journal.PullRef{
			Repo: name, Path: dir, SHA: before, Branch: "refs/heads/main", PostSHA: after,
		})
		targets = append(targets, ui.PullTarget{Name: name, Path: dir})
	}
	message, err := pullReposForUI(context.Background())(targets)
	if err != nil {
		t.Fatalf("pull: %v", err)
	}
	for _, p := range pulls {
		if !strings.Contains(message, "pulled") || pullUndoGit(t, p.Path, "rev-parse", "HEAD") != p.PostSHA {
			t.Fatalf("pull did not advance %s: %s", p.Repo, message)
		}
	}
	return pulls
}

func runPullUndo(t *testing.T, dashboard bool) (string, error) {
	t.Helper()
	if dashboard {
		return undoLastPullForUI(context.Background())()
	}
	var out bytes.Buffer
	cmd := NewRootCmd("test")
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"undo"})
	err := cmd.Execute()
	return out.String(), err
}

func pullUndoCheckoutState(t *testing.T, dir string) string {
	t.Helper()
	var state strings.Builder
	for _, args := range [][]string{
		{"rev-parse", "HEAD"},
		{"rev-parse", "--abbrev-ref", "HEAD"},
		{"for-each-ref", "--format=%(refname) %(objectname)"},
		{"reflog", "--all", "--format=%gD %H %gs"},
		{"status", "--porcelain", "--untracked-files=all"},
		{"diff", "--binary"},
		{"diff", "--cached", "--binary"},
	} {
		state.WriteString(pullUndoGit(t, dir, args...))
		state.WriteByte('\n')
	}
	for _, name := range []string{"README.md", "untracked.txt"} {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		state.Write(data)
	}
	return state.String()
}

func TestPullUndoPreservesChangedCheckout(t *testing.T) {
	cases := []struct {
		name   string
		change func(*testing.T, string)
		want   string
	}{
		{"unstaged edits", func(t *testing.T, dir string) {
			pullUndoWrite(t, dir, "README.md", "my uncommitted work\n")
		}, "working tree is dirty"},
		{"staged edits", func(t *testing.T, dir string) {
			pullUndoWrite(t, dir, "README.md", "my staged work\n")
			pullUndoGit(t, dir, "add", "README.md")
		}, "working tree is dirty"},
		{"untracked files hidden by config", func(t *testing.T, dir string) {
			pullUndoWrite(t, dir, "untracked.txt", "my new work\n")
			pullUndoGit(t, dir, "config", "status.showUntrackedFiles", "no")
		}, "working tree is dirty"},
		{"new commits", func(t *testing.T, dir string) {
			pullUndoGit(t, dir, "commit", "--allow-empty", "-m", "my new commit")
		}, "HEAD changed since pull"},
		{"manually reset to pre-pull tip", func(t *testing.T, dir string) {
			pullUndoGit(t, dir, "reset", "--keep", "HEAD^")
		}, "HEAD changed since pull"},
		{"different branch at pulled tip", func(t *testing.T, dir string) {
			pullUndoGit(t, dir, "checkout", "-b", "other")
		}, "branch changed since pull"},
		{"detached at pulled tip", func(t *testing.T, dir string) {
			pullUndoGit(t, dir, "checkout", "--detach")
		}, "cannot verify original branch"},
		{"legacy journal without pulled tip", func(t *testing.T, _ string) {
			changePullJournal(t, func(p *journal.PullRef) { p.PostSHA = "" })
		}, "cannot safely undo this older entry"},
		{"legacy journal without branch", func(t *testing.T, _ string) {
			changePullJournal(t, func(p *journal.PullRef) { p.Branch = "" })
		}, "cannot safely undo this older entry"},
	}
	for _, dashboard := range []bool{false, true} {
		entrypoint := "CLI"
		if dashboard {
			entrypoint = "dashboard"
		}
		t.Run(entrypoint, func(t *testing.T) {
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					dir, _, _ := setupJournaledPull(t)
					tc.change(t, dir)
					before := pullUndoCheckoutState(t, dir)
					journalPath, err := journal.Path()
					if err != nil {
						t.Fatal(err)
					}
					journalBefore, err := os.ReadFile(journalPath)
					if err != nil {
						t.Fatal(err)
					}
					out, err := runPullUndo(t, dashboard)
					if err == nil {
						t.Errorf("undo succeeded for changed checkout: %s", out)
					} else if !strings.Contains(out+err.Error(), tc.want) {
						t.Errorf("undo = %q, %v; want reason %q", out, err, tc.want)
					}
					if after := pullUndoCheckoutState(t, dir); after != before {
						t.Errorf("undo changed user work\nbefore:\n%s\nafter:\n%s", before, after)
					}
					j, err := journal.Load()
					if err != nil || len(j.Entries) != 1 {
						t.Errorf("refused undo lost journal entry: %+v, %v", j, err)
					}
					if after, err := os.ReadFile(journalPath); err != nil || !bytes.Equal(after, journalBefore) {
						t.Errorf("refused undo changed journal: %v", err)
					}
				})
			}
		})
	}
}

func changePullJournal(t *testing.T, change func(*journal.PullRef)) {
	t.Helper()
	j, err := journal.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(j.Entries) != 1 || len(j.Entries[0].Pulls) != 1 {
		t.Fatalf("unexpected pull journal: %+v", j)
	}
	change(&j.Entries[0].Pulls[0])
	if err := journal.Save(j); err != nil {
		t.Fatal(err)
	}
}

func TestPullUndoCleanCheckout(t *testing.T) {
	for _, dashboard := range []bool{false, true} {
		entrypoint := "CLI"
		if dashboard {
			entrypoint = "dashboard"
		}
		t.Run(entrypoint, func(t *testing.T) {
			dir, before, after := setupJournaledPull(t)
			j, err := journal.Load()
			if err != nil {
				t.Fatal(err)
			}
			entry, ok := j.Last()
			if !ok || len(entry.Pulls) != 1 {
				t.Fatalf("unexpected pull journal: %+v", j)
			}
			p := entry.Pulls[0]
			if p.SHA != before || p.PostSHA != after || p.Branch != "refs/heads/main" {
				t.Fatalf("journal did not record both pull tips and branch: %+v", p)
			}
			// Ordinary ignored files do not make a checkout dirty and survive undo.
			pullUndoWrite(t, dir, ".git/info/exclude", "ignored.txt\n")
			pullUndoWrite(t, dir, "ignored.txt", "local ignored work\n")
			out, err := runPullUndo(t, dashboard)
			if err != nil {
				t.Fatalf("undo: %v\n%s", err, out)
			}
			if got := pullUndoGit(t, dir, "rev-parse", "HEAD"); got != before {
				t.Errorf("HEAD = %s, want pre-pull %s", got, before)
			}
			if got := pullUndoGit(t, dir, "symbolic-ref", "HEAD"); got != p.Branch {
				t.Errorf("branch = %s, want %s", got, p.Branch)
			}
			data, err := os.ReadFile(filepath.Join(dir, "README.md"))
			if err != nil || string(data) != "before pull\n" {
				t.Errorf("README after undo = %q, %v", data, err)
			}
			data, err = os.ReadFile(filepath.Join(dir, "ignored.txt"))
			if err != nil || string(data) != "local ignored work\n" {
				t.Errorf("ignored file after undo = %q, %v", data, err)
			}
			j, err = journal.Load()
			if err != nil || len(j.Entries) != 0 {
				t.Errorf("successful undo did not consume journal entry: %+v, %v", j, err)
			}
		})
	}
}

func TestPullUndoPartialRetry(t *testing.T) {
	for _, firstDashboard := range []bool{false, true} {
		for _, retryDashboard := range []bool{false, true} {
			name := "CLI"
			if firstDashboard {
				name = "dashboard"
			}
			name += "/retry-"
			if retryDashboard {
				name += "dashboard"
			} else {
				name += "CLI"
			}
			t.Run(name, func(t *testing.T) {
				pulls := setupJournaledPulls(t, "A", "B")
				a, b := pulls[0], pulls[1]
				j, err := journal.Load()
				if err != nil {
					t.Fatal(err)
				}
				// An older clean entry must remain inaccessible until the pull
				// reversal finishes, then become the next ordinary undo.
				older := journal.Entry{
					Op: journal.OpClean, Time: time.Unix(1700000000, 0), Summary: "older cleanup",
					Branches: []journal.BranchRef{{Repo: a.Repo, Path: a.Path, Branch: "older", SHA: a.SHA}},
				}
				j.Entries = append([]journal.Entry{older}, j.Entries...)
				if err := journal.Save(j); err != nil {
					t.Fatal(err)
				}
				pullUndoWrite(t, b.Path, "untracked.txt", "preserve this work\n")
				bState := pullUndoCheckoutState(t, b.Path)
				out, err := runPullUndo(t, firstDashboard)
				if err == nil || !strings.Contains(out+err.Error(), "working tree is dirty") {
					t.Fatalf("partial undo = %q, %v; want dirty-tree refusal", out, err)
				}
				if got := pullUndoGit(t, a.Path, "rev-parse", "HEAD"); got != a.SHA {
					t.Fatalf("A HEAD = %s, want pre-pull %s", got, a.SHA)
				}
				if got := pullUndoCheckoutState(t, b.Path); got != bState {
					t.Fatal("partial undo changed refused B checkout")
				}
				j, err = journal.Load()
				if err != nil || len(j.Entries) != 2 || len(j.Entries[1].Pulls) != 2 {
					t.Fatalf("partial undo lost history: %+v, %v", j, err)
				}
				completed := a
				completed.Undone = true
				if j.Entries[1].Pulls[0] != completed || j.Entries[1].Pulls[1] != b {
					t.Fatalf("partial undo did not checkpoint only A: %+v", j.Entries[1].Pulls)
				}

				// Work after A's completed reversal must never be revisited,
				// even if it now violates every guard for a pending target.
				pullUndoGit(t, a.Path, "checkout", "-b", "after-undo")
				pullUndoGit(t, a.Path, "commit", "--allow-empty", "-m", "new work after undo")
				pullUndoWrite(t, a.Path, "README.md", "staged after undo\n")
				pullUndoGit(t, a.Path, "add", "README.md")
				pullUndoWrite(t, a.Path, "README.md", "unstaged after undo\n")
				pullUndoWrite(t, a.Path, "untracked.txt", "new untracked work\n")
				aState := pullUndoCheckoutState(t, a.Path)
				out, err = runPullUndo(t, retryDashboard)
				if err == nil || !strings.Contains(out+err.Error(), "working tree is dirty") {
					t.Fatalf("still-blocked retry = %q, %v; want B dirty-tree refusal", out, err)
				}
				if got := pullUndoCheckoutState(t, a.Path); got != aState {
					t.Fatal("blocked retry changed already-completed A")
				}
				if got := pullUndoCheckoutState(t, b.Path); got != bState {
					t.Fatal("blocked retry changed pending B")
				}

				preserved := filepath.Join(t.TempDir(), "preserved-work.txt")
				if err := os.Rename(filepath.Join(b.Path, "untracked.txt"), preserved); err != nil {
					t.Fatal(err)
				}
				out, err = runPullUndo(t, retryDashboard)
				if err != nil {
					t.Fatalf("retry after resolving B: %v\n%s", err, out)
				}
				if got := pullUndoCheckoutState(t, a.Path); got != aState {
					t.Fatal("successful retry changed already-completed A")
				}
				if got := pullUndoGit(t, b.Path, "rev-parse", "HEAD"); got != b.SHA {
					t.Errorf("B HEAD = %s, want pre-pull %s", got, b.SHA)
				}
				if got := pullUndoGit(t, b.Path, "symbolic-ref", "HEAD"); got != b.Branch {
					t.Errorf("B branch = %s, want %s", got, b.Branch)
				}
				if data, err := os.ReadFile(filepath.Join(b.Path, "README.md")); err != nil || string(data) != "before pull\n" {
					t.Errorf("B README after undo = %q, %v", data, err)
				}
				if data, err := os.ReadFile(preserved); err != nil || string(data) != "preserve this work\n" {
					t.Errorf("preserved B work = %q, %v", data, err)
				}
				j, err = journal.Load()
				if err != nil || len(j.Entries) != 1 || j.Entries[0].Summary != older.Summary {
					t.Fatalf("completed pull did not expose older entry: %+v, %v", j, err)
				}
				if out, err := runPullUndo(t, false); err != nil {
					t.Fatalf("older undo: %v\n%s", err, out)
				}
				if got := pullUndoGit(t, a.Path, "rev-parse", "refs/heads/older"); got != a.SHA {
					t.Errorf("older branch = %s, want %s", got, a.SHA)
				}
				j, err = journal.Load()
				if err != nil || len(j.Entries) != 0 {
					t.Errorf("older undo did not drain journal: %+v, %v", j, err)
				}
			})
		}
	}
}

func TestPullUndoPartialRetryThreeTargets(t *testing.T) {
	for _, dashboard := range []bool{false, true} {
		name := "CLI"
		if dashboard {
			name = "dashboard"
		}
		t.Run(name, func(t *testing.T) {
			pulls := setupJournaledPulls(t, "A", "B", "C")
			pullUndoWrite(t, pulls[1].Path, "untracked.txt", "preserve B work\n")
			if out, err := runPullUndo(t, dashboard); err == nil {
				t.Fatalf("partial undo succeeded: %s", out)
			}
			j, err := journal.Load()
			if err != nil || len(j.Entries) != 1 || len(j.Entries[0].Pulls) != 3 {
				t.Fatalf("unexpected partial journal: %+v, %v", j, err)
			}
			// CLI continues past refused B; dashboard stops at B. Both must
			// retain every checkpoint when recording later progress.
			var completedStates [3]string
			for i, p := range j.Entries[0].Pulls {
				wantUndone := i == 0 || (i == 2 && !dashboard)
				if p.Undone != wantUndone {
					t.Fatalf("%s undone = %t, want %t", p.Repo, p.Undone, wantUndone)
				}
				if p.Undone {
					completedStates[i] = pullUndoCheckoutState(t, p.Path)
				}
			}
			preserved := filepath.Join(t.TempDir(), "preserved-B.txt")
			if err := os.Rename(filepath.Join(pulls[1].Path, "untracked.txt"), preserved); err != nil {
				t.Fatal(err)
			}
			if out, err := runPullUndo(t, dashboard); err != nil {
				t.Fatalf("retry: %v\n%s", err, out)
			}
			for i, p := range pulls {
				if got := pullUndoGit(t, p.Path, "rev-parse", "HEAD"); got != p.SHA {
					t.Errorf("%s HEAD = %s, want %s", p.Repo, got, p.SHA)
				}
				if completedStates[i] != "" && pullUndoCheckoutState(t, p.Path) != completedStates[i] {
					t.Errorf("retry replayed completed %s", p.Repo)
				}
			}
			j, err = journal.Load()
			if err != nil || len(j.Entries) != 0 {
				t.Errorf("retry did not consume completed entry: %+v, %v", j, err)
			}
		})
	}
}

// Append after the reset's checkpoint but before undo consumes its entry.
// This deterministic interleaving models another command completing meanwhile.
type appendDuringUndoWriter struct {
	bytes.Buffer
	entry     journal.Entry
	appended  bool
	appendErr error
}

func (w *appendDuringUndoWriter) Write(p []byte) (int, error) {
	if !w.appended {
		w.appended = true
		w.appendErr = journal.Append(&w.entry)
	}
	return w.Buffer.Write(p)
}

func TestPullUndoPreservesConcurrentJournalAppend(t *testing.T) {
	dir, before, _ := setupJournaledPull(t)
	out := &appendDuringUndoWriter{entry: journal.Entry{
		Op: journal.OpClean, Time: time.Now(), Summary: "concurrent cleanup",
		Branches: []journal.BranchRef{{Repo: "repo", Path: dir, Branch: "concurrent", SHA: before}},
	}}
	cmd := NewRootCmd("test")
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"undo"})
	if err := cmd.Execute(); !errors.Is(err, journal.ErrConflict) {
		t.Fatalf("undo = %v, want journal conflict; output: %s", err, out.String())
	}
	if !out.appended || out.appendErr != nil {
		t.Fatalf("concurrent append failed: %v", out.appendErr)
	}
	j, err := journal.Load()
	if err != nil || len(j.Entries) != 2 || !j.Entries[0].Pulls[0].Undone || j.Entries[1].Summary != out.entry.Summary {
		t.Fatalf("undo lost progress or concurrent entry: %+v, %v", j, err)
	}
	if got := pullUndoGit(t, dir, "rev-parse", "HEAD"); got != before {
		t.Fatalf("HEAD = %s, want reset %s", got, before)
	}
	for range 2 {
		if out, err := runPullUndo(t, false); err != nil {
			t.Fatalf("retry undo: %v\n%s", err, out)
		}
	}
	j, err = journal.Load()
	if err != nil || len(j.Entries) != 0 {
		t.Fatalf("retry did not consume completed entries: %+v, %v", j, err)
	}
	if got := pullUndoGit(t, dir, "rev-parse", "refs/heads/concurrent"); got != before {
		t.Errorf("concurrent branch = %s, want %s", got, before)
	}
}
