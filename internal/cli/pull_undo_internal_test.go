package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// setupJournaledPull exercises the dashboard's real fast-forward and journal
// writer against a local remote, without requiring a terminal or network.
func setupJournaledPull(t *testing.T) (dir, before, after string) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	base := t.TempDir()
	source := filepath.Join(base, "source")
	dir = filepath.Join(base, "checkout")
	pullUndoGit(t, base, "init", "--initial-branch=main", source)
	pullUndoGit(t, source, "config", "user.name", "Test")
	pullUndoGit(t, source, "config", "user.email", "test@example.com")
	pullUndoWrite(t, source, "README.md", "before pull\n")
	pullUndoGit(t, source, "add", ".")
	pullUndoGit(t, source, "commit", "-m", "initial")
	before = pullUndoGit(t, source, "rev-parse", "HEAD")
	pullUndoGit(t, base, "clone", source, dir)
	pullUndoGit(t, dir, "config", "user.name", "Test")
	pullUndoGit(t, dir, "config", "user.email", "test@example.com")
	pullUndoWrite(t, source, "README.md", "after pull\n")
	pullUndoGit(t, source, "commit", "-am", "remote update")
	after = pullUndoGit(t, source, "rev-parse", "HEAD")
	message, err := pullReposForUI(context.Background())([]ui.PullTarget{{Name: "repo", Path: dir}})
	if err != nil {
		t.Fatalf("pull: %v", err)
	}
	if !strings.Contains(message, "pulled") || pullUndoGit(t, dir, "rev-parse", "HEAD") != after {
		t.Fatalf("pull did not advance checkout: %s", message)
	}
	return dir, before, after
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
