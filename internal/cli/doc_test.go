package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CelikE/soko/internal/config"
)

func TestIntegration_DocWorktreeFamilyReferences(t *testing.T) {
	for _, format := range []string{"text", "json"} {
		for _, quiet := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/quiet=%t", format, quiet), func(t *testing.T) {
				testEnv(t)
				root := t.TempDir()
				parent := filepath.Join(root, "parent")
				other := filepath.Join(root, "other")
				initRepo(t, parent)
				initRepo(t, other)
				wtGit(t, parent, "remote", "add", "origin", "https://example.invalid/api.git")
				wtGit(t, other, "remote", "add", "origin", "https://example.invalid/other.git")
				child := func(name, family string) config.RepoEntry {
					t.Helper()
					path := filepath.Join(root, name)
					wtGit(t, parent, "worktree", "add", "-b", name, path)
					return config.RepoEntry{Name: name, Path: path, WorktreeOf: family,
						Tags: []string{"local"}, Meta: map[string]string{"note": "keep child"}}
				}
				cfg := &config.Config{
					Aliases: map[string]string{"ls": "list"},
					Repos: []config.RepoEntry{
						{Name: "api-v2", Path: parent, Tags: []string{"backend"}, Meta: map[string]string{"note": "keep parent"}},
						child("orphan", "api"), // A prefix match is not a parent identity.
						child("valid", "api-v2"),
						{Name: "legacy", Path: parent},
						{Name: "legacy", Path: other},
						child("ambiguous", "legacy"),
					},
				}
				if err := config.Save(cfg); err != nil {
					t.Fatal(err)
				}
				cfgPath, err := config.Path()
				if err != nil {
					t.Fatal(err)
				}
				before, err := os.ReadFile(cfgPath)
				if err != nil {
					t.Fatal(err)
				}
				args := []string{"doc"}
				if format == "json" {
					args = append(args, "--json")
				}
				if quiet {
					args = append(args, "--quiet")
				}
				for _, fix := range []bool{false, true} {
					call := append([]string(nil), args...)
					if fix {
						call = append(call, "--fix")
					}
					out := runSoko(t, call...)
					if format == "json" {
						var rows []struct {
							Name, Status, Message string
							Fixable, Fixed        bool
						}
						if err := json.Unmarshal([]byte(out), &rows); err != nil {
							t.Fatalf("decoding doc JSON: %v\n%s", err, out)
						}
						var families []string
						for _, row := range rows {
							if row.Fixed || row.Fixable {
								t.Errorf("healthy checkout or manual diagnostic advertised repair: %+v", row)
							}
							if strings.Contains(row.Message, "worktree_of") {
								families = append(families, row.Name+":"+row.Status)
							}
						}
						if got := strings.Join(families, ","); got != "orphan:warning,ambiguous:warning" {
							t.Errorf("family diagnostics = %s\n%s", got, out)
						}
					} else if strings.Count(out, "worktree_of") != 2 {
						t.Errorf("want only orphan and ambiguous family warnings: %s", out)
					}
					for _, want := range []string{"orphan", "ambiguous", "not registered", "--repo", "worktree commands", "soko config edit", parent, other, "duplicate"} {
						// JSON escapes Windows path separators in candidate paths.
						if format == "json" {
							encoded, err := json.Marshal(want)
							if err != nil {
								t.Fatal(err)
							}
							want = string(encoded[1 : len(encoded)-1])
						}
						if !strings.Contains(out, want) {
							t.Errorf("doc output missing %q: %s", want, out)
						}
					}
					if after, err := os.ReadFile(cfgPath); err != nil || !bytes.Equal(before, after) {
						t.Fatalf("doc fix=%t changed registry bytes: %v", fix, err)
					}
					for _, repo := range cfg.Repos {
						if status := wtGit(t, repo.Path, "status", "--porcelain"); status != "" {
							t.Errorf("checkout %s changed: %s", repo.Name, status)
						}
					}
				}
			})
		}
	}
}

func TestIntegration_DocFixDuplicateNames(t *testing.T) {
	for _, jsonOut := range []bool{false, true} {
		name := "text"
		if jsonOut {
			name = "json"
		}
		t.Run(name, func(t *testing.T) {
			assertConfigData := func(want *config.Config) {
				t.Helper()
				gotData, err := json.Marshal(loadRegistry(t))
				if err != nil {
					t.Fatal(err)
				}
				wantData, err := json.Marshal(want)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(gotData, wantData) {
					t.Fatalf("registry data = %s, want %s", gotData, wantData)
				}
			}
			testEnv(t)
			base := t.TempDir()
			healthyPath := filepath.Join(base, "healthy")
			otherPath := filepath.Join(base, "other")
			missingPath := filepath.Join(base, "missing")
			notGitPath := filepath.Join(base, "not-git")
			initRepo(t, healthyPath)
			initRepo(t, otherPath)
			if err := os.Mkdir(notGitPath, 0o755); err != nil {
				t.Fatal(err)
			}

			healthy := config.RepoEntry{
				Name: "api", Path: healthyPath,
				Tags: []string{"backend"}, Meta: map[string]string{"note": "keep me"},
			}
			other := config.RepoEntry{Name: "other", Path: otherPath}
			// Legacy or hand-edited configs can contain duplicate names even
			// though new registrations now require unique aliases.
			cfg := &config.Config{
				Aliases: map[string]string{"ls": "list"},
				Repos: []config.RepoEntry{
					healthy,
					{Name: "api", Path: missingPath},
					other,
					{Name: "api", Path: notGitPath},
				},
			}
			if err := config.Save(cfg); err != nil {
				t.Fatal(err)
			}

			args := []string{"doc"}
			if jsonOut {
				args = append(args, "--json")
			}
			runSoko(t, args...)
			assertConfigData(cfg)

			args = append(args, "--fix")
			out := runSoko(t, args...)
			want := &config.Config{Aliases: cfg.Aliases, Repos: []config.RepoEntry{healthy, other}}
			assertConfigData(want)
			if jsonOut {
				var rows []struct {
					Message string `json:"message"`
					Fixed   bool   `json:"fixed"`
				}
				if err := json.Unmarshal([]byte(out), &rows); err != nil {
					t.Fatalf("decoding doc JSON: %v\n%s", err, out)
				}
				var fixed []string
				for _, row := range rows {
					if row.Fixed {
						fixed = append(fixed, row.Message)
					}
				}
				if len(fixed) != 2 || !strings.Contains(fixed[0], missingPath) || !strings.Contains(fixed[1], notGitPath) {
					t.Errorf("fixed checks = %v, want only both broken paths", fixed)
				}
			} else if strings.Count(out, "removed from config") != 2 {
				t.Errorf("doc --fix output = %q, want both broken entries removed", out)
			}

			// A repeated repair must leave the surviving entries intact.
			runSoko(t, args...)
			assertConfigData(want)
		})
	}
}

func TestIntegration_DocFixMissingBaseWithLiveWorktree(t *testing.T) {
	for _, format := range []string{"text", "json"} {
		t.Run(format, func(t *testing.T) {
			testEnv(t)
			root := t.TempDir()
			base := filepath.Join(root, "api")
			admin := filepath.Join(root, "admin")
			linked := filepath.Join(root, "linked")
			away := filepath.Join(root, "api-away")
			other := filepath.Join(root, "other")
			cmd := exec.CommandContext(context.Background(), "git", "init", "-b", "main", "--separate-git-dir", admin, base)
			cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null")
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("init separate-admin repo: %v\n%s", err, out)
			}
			wtGit(t, base, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "--allow-empty", "-m", "initial")
			runSokoAt(t, base, "init")
			wtGit(t, base, "worktree", "add", "-b", "feature", linked)
			runSokoAt(t, linked, "init", "--worktree")
			initRepo(t, other)
			runSokoAt(t, other, "init")
			cfg := loadRegistry(t)
			if len(cfg.Repos) != 3 || cfg.Repos[1].WorktreeOf != cfg.Repos[0].Name {
				t.Fatalf("initial registry = %+v", cfg.Repos)
			}
			cfg.Aliases = map[string]string{"ls": "list"}
			cfg.Repos[2].Meta = map[string]string{"note": "keep me"}
			if err := config.Save(cfg); err != nil {
				t.Fatal(err)
			}
			family := cfg.Repos[1].Name
			if out := runSoko(t, "list", "--repo", family, "--json"); !strings.Contains(out, `"worktree_of": "api"`) {
				t.Fatalf("family before repair = %s", out)
			}

			if err := os.Rename(base, away); err != nil {
				t.Fatal(err)
			}
			if out := runSoko(t, "doc", "--json"); strings.Contains(out, `"fixed": true`) {
				t.Fatalf("doc without --fix claimed repair: %s", out)
			}
			if len(loadRegistry(t).Repos) != 3 {
				t.Fatal("doc without --fix changed registry")
			}
			if err := os.Rename(away, base); err != nil {
				t.Fatal(err)
			}
			runSoko(t, "list", "--repo", family, "--json") // restoring base alone preserves family before repair
			if err := os.Rename(base, away); err != nil {
				t.Fatal(err)
			}

			args := []string{"doc", "--fix"}
			if format == "json" {
				args = append(args, "--json")
			}
			out := runSoko(t, args...)
			if format == "json" {
				var rows []struct {
					Name, Message string
					Fixed         bool
				}
				if err := json.Unmarshal([]byte(out), &rows); err != nil {
					t.Fatalf("doc JSON: %v\n%s", err, out)
				}
				var fixed []string
				for _, row := range rows {
					if row.Fixed {
						fixed = append(fixed, row.Name)
						if !strings.Contains(row.Message, "removed from config") {
							t.Errorf("fixed row lacks removal explanation: %+v", row)
						}
					}
				}
				if len(fixed) != 2 || fixed[0] != "api" || fixed[1] != family {
					t.Fatalf("fixed rows = %v, want api and %s", fixed, family)
				}
			} else if !strings.Contains(out, "api/feature: parent repo") || strings.Count(out, "removed from config") != 2 {
				t.Fatalf("doc text did not report both removals: %s", out)
			}
			saved := loadRegistry(t)
			if len(saved.Repos) != 1 || saved.Repos[0].Name != "other" || saved.Repos[0].Meta["note"] != "keep me" || saved.Aliases["ls"] != "list" {
				t.Fatalf("registry after repair = %+v", saved)
			}
			if _, err := os.Stat(linked); err != nil {
				t.Fatalf("linked checkout was touched: %v", err)
			}
			if status := wtGit(t, linked, "status", "--porcelain"); status != "" {
				t.Fatalf("linked checkout changed: %s", status)
			}
			if out := runSoko(t, "list", "--json"); strings.Contains(out, `"worktree_of"`) || !strings.Contains(out, `"name": "other"`) {
				t.Fatalf("registry list after repair = %s", out)
			}
			if _, err := runSokoErr(t, "list", "--repo", family, "--json"); err == nil || strings.Contains(err.Error(), "parent repo") {
				t.Fatalf("family query after removal = %v, want unregistered family without dangling parent", err)
			}
			if err := os.Rename(away, base); err != nil {
				t.Fatal(err)
			}
			if len(loadRegistry(t).Repos) != 1 {
				t.Fatal("restoring base directory changed repaired registry")
			}
			if out := runSoko(t, args...); strings.Contains(out, "worktree_of") {
				t.Fatalf("repeated repair reported an already-removed family: %s", out)
			}
		})
	}
}
