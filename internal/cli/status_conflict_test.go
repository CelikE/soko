package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/CelikE/soko/internal/config"
	"github.com/CelikE/soko/internal/git"
	"github.com/CelikE/soko/internal/ui"
)

func TestStatusConflictOnly(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_TEMPLATE_DIR", t.TempDir())
	conflicted, untouched, ordinary := initGitRepo(t), initGitRepo(t), initGitRepo(t)
	write := func(dir, name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	readGit := func(args ...string) string {
		t.Helper()
		out, err := git.Run(context.Background(), conflicted, args...)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	gitInDir(t, conflicted, "switch", "-c", "side")
	write(conflicted, "README.md", "side\n")
	gitInDir(t, conflicted, "commit", "-am", "side")
	gitInDir(t, conflicted, "switch", "master")
	write(conflicted, "README.md", "main\n")
	gitInDir(t, conflicted, "commit", "-am", "main")
	if _, err := git.Run(context.Background(), conflicted, "merge", "side"); err == nil {
		t.Fatal("fixture merge did not conflict")
	}
	if got := readGit("status", "--porcelain"); got != "UU README.md" {
		t.Fatalf("fixture is not conflict-only: %q", got)
	}
	write(ordinary, "untracked.txt", "ordinary dirty sibling\n")
	saveConfig(t,
		config.RepoEntry{Name: "conflicted", Path: conflicted},
		config.RepoEntry{Name: "untouched", Path: untouched},
		config.RepoEntry{Name: "ordinary", Path: ordinary},
	)
	configPath := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "soko", "config.yaml")
	read := func(path string) []byte {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	run := func(args ...string) string {
		t.Helper()
		out, stderr, err := runRootIO(t, args...)
		if err != nil {
			t.Fatalf("soko %v: %v\n%s", args, err, stderr)
		}
		return out
	}
	for _, state := range []struct {
		name      string
		dirty     bool
		conflicts int
		modified  int
	}{
		{name: "unresolved", dirty: true, conflicts: 1},
		{name: "resolved-staged", dirty: true, modified: 1},
		{name: "committed"},
	} {
		// Advance the fixture even when -run selects only one lifecycle subtest.
		switch state.name {
		case "resolved-staged":
			write(conflicted, "README.md", "resolved\n")
			gitInDir(t, conflicted, "add", "README.md")
		case "committed":
			gitInDir(t, conflicted, "commit", "-m", "resolve merge")
		}
		t.Run(state.name, func(t *testing.T) {
			beforeConfig, beforeFile := read(configPath), read(filepath.Join(conflicted, "README.md"))
			beforeRefs, beforeStatus := readGit("show-ref"), readGit("status", "--porcelain=v2", "--branch")
			dirtyCount := 1 // The ordinary dirty sibling remains unchanged.
			if state.dirty {
				dirtyCount++
			}
			for _, filter := range []string{"", "--dirty", "--clean"} {
				args := []string{"status", "--json"}
				if filter != "" {
					args = append(args, filter)
				}
				var rows []statusJSON
				if err := json.Unmarshal([]byte(run(args...)), &rows); err != nil {
					t.Fatal(err)
				}
				names := make([]string, 0, len(rows))
				for _, row := range rows {
					names = append(names, row.Name)
					if row.Name == "conflicted" && (row.Dirty != state.dirty || row.Conflicts != state.conflicts ||
						row.Modified != state.modified || row.Untracked != 0 || row.Deleted != 0) {
						t.Errorf("%s status = %+v, want dirty=%v, conflicts=%d, modified=%d", filter, row, state.dirty, state.conflicts, state.modified)
					}
				}
				want := []string{"conflicted", "untouched", "ordinary"}
				switch filter {
				case "--dirty":
					want = []string{"ordinary"}
					if state.dirty {
						want = []string{"conflicted", "ordinary"}
					}
				case "--clean":
					want = []string{"untouched"}
					if !state.dirty {
						want = []string{"conflicted", "untouched"}
					}
				}
				if !slices.Equal(names, want) {
					t.Errorf("%s membership = %v, want %v", filter, names, want)
				}
			}
			if out := run("status"); !strings.Contains(out, fmt.Sprintf("%d dirty", dirtyCount)) ||
				!strings.Contains(out, fmt.Sprintf("%d change", 1+state.modified)) {
				t.Errorf("status summary: %s", out)
			}
			var stats struct{ Dirty, Clean int }
			if err := json.Unmarshal([]byte(run("stats", "--json")), &stats); err != nil {
				t.Fatal(err)
			}
			if stats.Dirty != dirtyCount || stats.Clean != 3-dirtyCount {
				t.Errorf("stats = %+v, want dirty=%d clean=%d", stats, dirtyCount, 3-dirtyCount)
			}
			cmd := NewRootCmd("test")
			cmd.SetContext(context.Background())
			cfg, err := config.Load()
			if err != nil {
				t.Fatal(err)
			}
			model := ui.New(&ui.Config{Collect: func(_ context.Context, fetch bool) []ui.Row {
				return collectUIRows(cmd, cfg.Repos, fetch)
			}})
			batch := model.Init()().(tea.BatchMsg)
			model.Update(batch[0]()) // Run the real collector through the dashboard refresh path.
			model.Update(tea.WindowSizeMsg{Width: 160, Height: 30})
			if view := ansi.Strip(model.View()); !strings.Contains(view, fmt.Sprintf("%d dirty", dirtyCount)) {
				t.Errorf("dashboard summary: %s", view)
			}
			model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
			view := ansi.Strip(model.View())
			if !strings.Contains(view, "filter:dirty") || !strings.Contains(view, "ordinary") ||
				strings.Contains(view, "untouched") || strings.Contains(view, "conflicted") != state.dirty {
				t.Errorf("dashboard dirty filter: %s", view)
			}
			if !bytes.Equal(beforeConfig, read(configPath)) || !bytes.Equal(beforeFile, read(filepath.Join(conflicted, "README.md"))) ||
				beforeRefs != readGit("show-ref") || beforeStatus != readGit("status", "--porcelain=v2", "--branch") {
				t.Fatal("read-only status/stats/dashboard changed registry, refs, file, or index/worktree state")
			}
		})
	}
}
