package cli_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/CelikE/soko/internal/config"
)

func TestIntegration_RegistrationTagLifecycle(t *testing.T) {
	for _, producer := range []string{"init", "init worktree", "scan", "scan worktrees", "discover", "worktree add", "worktree adopt"} {
		for _, variant := range []struct {
			name string
			tags []string
			want []string
		}{
			{"uppercase", []string{"Backend"}, []string{"backend"}},
			{"trim and deduplicate", []string{" Backend ", "backend", "BACKEND", "Go", " go "}, []string{"backend", "go"}},
		} {
			t.Run(producer+"/"+variant.name, func(t *testing.T) {
				testEnv(t)
				base := t.TempDir()
				root, linked := filepath.Join(base, "api"), filepath.Join(base, "feature")
				initRepo(t, root)
				other := filepath.Join(base, "other")
				initRepo(t, other)
				runSokoAt(t, other, "init", "--tag", "unrelated")
				target, flag := "api", "--tag"
				if producer == "init worktree" || producer == "scan worktrees" || producer == "worktree add" || producer == "worktree adopt" {
					runSokoAt(t, root, "init", "--tag", "parent")
					target = "api/feature"
					if producer != "worktree add" {
						gitIn(t, root, "worktree", "add", "-b", "feature", linked)
					}
				}
				args := make([]string, 0, 10+2*len(variant.tags))
				switch producer {
				case "init", "init worktree":
					args = append(args, "init", "--json")
					if producer == "init worktree" {
						args = append(args, "--worktree")
					}
				case "scan":
					args = append(args, "scan", root, "--json")
				case "scan worktrees":
					args = append(args, "scan", linked, "--worktrees", "--json")
				case "discover":
					args = append(args, "discover", "on", "--root", base)
				case "worktree add":
					args = append(args, "worktree", "add", "api", "feature", "-b", "--path", linked, "--json")
				case "worktree adopt":
					args, flag = append(args, "worktree", "adopt", "api", "--json"), "--tag-with"
				}
				for _, tag := range variant.tags {
					args = append(args, flag, tag)
				}
				cwd := root
				if producer == "init worktree" {
					cwd = linked
				}
				runSokoAt(t, cwd, args...)
				if producer == "discover" {
					runSoko(t, "discover", "hook", root)
					// Re-enabling must not add another spelling of the same tag.
					runSoko(t, "discover", "on", "--tag", " BACKEND ")
					if got := loadRegistry(t).Discover.Tags; !slices.Equal(got, variant.want) {
						t.Errorf("discovery defaults = %v, want %v", got, variant.want)
					}
				}
				checkTags := func(want []string) {
					t.Helper()
					cfg := loadRegistry(t)
					entry, err := config.FindRepoByName(cfg, target)
					if err != nil {
						t.Fatal(err)
					}
					if !slices.Equal(entry.Tags, want) {
						t.Errorf("persisted %s tags = %v, want %v", target, entry.Tags, want)
					}
					for _, entry := range cfg.Repos {
						if entry.Name == "other" && !slices.Equal(entry.Tags, []string{"unrelated"}) || entry.Name == "api" && target != "api" && !slices.Equal(entry.Tags, []string{"parent"}) {
							t.Errorf("unrelated entry changed: %+v", entry)
						}
					}
				}
				checkTags(variant.want)
				for _, spelling := range []string{"Backend", " BACKEND ", "backend"} {
					out := runSoko(t, "list", "--tag", spelling, "--json")
					var rows []struct{ Name string }
					if err := json.Unmarshal([]byte(out), &rows); err != nil || len(rows) != 1 || rows[0].Name != target {
						t.Errorf("selection by %q = %s, %v; want only %s", spelling, out, err, target)
					}
				}
				runSoko(t, "tag", "add", "-r", target, " BACKEND ")
				checkTags(variant.want)
				runSoko(t, "tag", "remove", "-r", target, " Backend ")
				checkTags(variant.want[1:])
			})
		}
	}
}

func TestIntegration_LegacyTagInheritance(t *testing.T) {
	for _, owns := range []bool{false, true} {
		name := "inherited only"
		if owns {
			name = "owned and inherited"
		}
		t.Run(name, func(t *testing.T) {
			testEnv(t)
			root, linked, _ := familyFixture(t)
			runSokoAt(t, root, "init")
			runSokoAt(t, linked, "init", "--worktree")
			cfg := loadRegistry(t)
			cfg.Repos[0].Tags = []string{" Backend ", "BACKEND", "Go"}
			cfg.Repos[1].Tags = []string{" WIP ", "wip"}
			if owns {
				cfg.Repos[1].Tags = append(cfg.Repos[1].Tags, "BACKEND", " backend ")
			}
			if err := config.Save(cfg); err != nil {
				t.Fatal(err)
			}
			path, err := config.Path()
			if err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			out := runSoko(t, "list", "--tag", "Backend", "--json")
			var selected []struct{ Name string }
			if err := json.Unmarshal([]byte(out), &selected); err != nil || len(selected) != 2 {
				t.Errorf("legacy parent/child selection = %s, %v; want both entries", out, err)
			}
			out = runSoko(t, "tag", "list", "--json")
			var counts []struct {
				Tag   string `json:"tag"`
				Count int    `json:"count"`
			}
			if err := json.Unmarshal([]byte(out), &counts); err != nil || len(counts) != 3 || counts[0].Tag != "backend" || counts[0].Count != 2 || counts[1].Tag != "go" || counts[1].Count != 2 || counts[2].Tag != "wip" || counts[2].Count != 1 {
				t.Errorf("legacy tag counts = %s, %v; want backend:2, go:2, wip:1", out, err)
			}
			if after, err := os.ReadFile(path); err != nil || !bytes.Equal(after, before) {
				t.Fatal("read-only tag operations changed persisted config")
			}
			if owns {
				runSoko(t, "tag", "remove", "-r", "api/feat/oauth", " Backend ")
				if got := loadRegistry(t).Repos[1].Tags; !slices.Equal(got, []string{"wip"}) {
					t.Errorf("owned tag variants not removed: %v", got)
				}
			}
			before, err = os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			_, err = runSokoErr(t, "tag", "remove", "-r", "api/feat/oauth", " BACKEND ")
			if !errors.Is(err, config.ErrInheritedTag) {
				t.Errorf("removing inherited-only tag = %v, want ErrInheritedTag", err)
			}
			if after, err := os.ReadFile(path); err != nil || !bytes.Equal(after, before) {
				t.Fatal("refused inherited removal changed persisted config")
			}
			if owns {
				runSoko(t, "tag", "add", "-r", "api", " BACKEND ", " ")
				if got := loadRegistry(t).Repos[0].Tags; !slices.Equal(got, []string{"backend", "go"}) {
					t.Errorf("adding existing legacy tag produced duplicates or blanks: %v", got)
				}
			}
			runSoko(t, "tag", "remove", "-r", "api", "backend")
			cfg = loadRegistry(t)
			if got := config.FilterByTags(cfg, cfg.Repos, []string{"Backend"}); len(got) != 0 {
				t.Errorf("parent retag did not rescope family: %+v", got)
			}
			if got := cfg.Repos[0].Tags; !slices.Equal(got, []string{"go"}) {
				t.Errorf("legacy parent tags after removal = %v, want [go]", got)
			}
		})
	}
}
