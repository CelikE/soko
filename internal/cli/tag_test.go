package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/CelikE/soko/internal/config"
)

func TestIntegration_TagListJSON(t *testing.T) {
	type tagCount struct {
		Tag   string `json:"tag"`
		Count int    `json:"count"`
	}
	for _, registry := range []string{"empty", "untagged", "tagged"} {
		for _, quiet := range []string{"off", "flag", "environment"} {
			t.Run(registry+"/quiet-"+quiet, func(t *testing.T) {
				xdg := testEnv(t)
				t.Setenv("SOKO_QUIET", "")
				want := []tagCount{}
				if registry != "empty" {
					cfg := &config.Config{Repos: []config.RepoEntry{{Name: "api", Path: t.TempDir()}}}
					if registry == "tagged" {
						cfg.Repos[0].Tags = []string{"go", "backend"}
						cfg.Repos = append(cfg.Repos, config.RepoEntry{Name: "worker", Path: t.TempDir(), Tags: []string{"go"}})
						want = []tagCount{{Tag: "backend", Count: 1}, {Tag: "go", Count: 2}}
					}
					if err := config.Save(cfg); err != nil {
						t.Fatal(err)
					}
				}
				args := []string{"tag", "list", "--json"}
				switch quiet {
				case "flag":
					args = append(args, "--quiet")
				case "environment":
					t.Setenv("SOKO_QUIET", "1")
				}
				out := runSoko(t, args...)
				var got []tagCount
				if err := json.Unmarshal([]byte(out), &got); err != nil {
					t.Fatalf("tag list output is not JSON: %q: %v", out, err)
				}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("tag list = %#v, want %#v", got, want)
				}
				if registry == "empty" {
					if _, err := os.Stat(filepath.Join(xdg, "soko", "config.yaml")); !os.IsNotExist(err) {
						t.Fatalf("listing tags must not create config, stat: %v", err)
					}
				}
			})
		}
	}
}

func TestIntegration_TagFromSubdirectory(t *testing.T) {
	for _, checkout := range []string{"repo", "worktree"} {
		t.Run(checkout, func(t *testing.T) {
			testEnv(t)
			base := t.TempDir()
			root := filepath.Join(base, "tagged-repo")
			initRepo(t, root)
			runSokoAt(t, root, "init", "--tag", "original")
			target := "tagged-repo"
			if checkout == "worktree" {
				linked := filepath.Join(base, "feature")
				wtGit(t, root, "worktree", "add", "-b", "feature", linked)
				runSokoAt(t, linked, "init", "--worktree", "--tag", "original")
				root, target = linked, "tagged-repo/feature"
			}
			subdir := filepath.Join(root, "src", "nested")
			if err := os.MkdirAll(subdir, 0o755); err != nil {
				t.Fatal(err)
			}

			steps := []struct {
				args []string
				want []string
			}{
				{[]string{"tag", "backend"}, []string{"original", "backend"}},
				{[]string{"tag", "add", "go"}, []string{"original", "backend", "go"}},
				{[]string{"tag", "remove", "original"}, []string{"backend", "go"}},
			}
			for _, step := range steps {
				runSokoAt(t, subdir, step.args...)
				for _, entry := range loadRegistry(t).Repos {
					want := []string{"original"}
					if entry.Name == target {
						want = step.want
					}
					if !slices.Equal(entry.Tags, want) {
						t.Fatalf("after %v: %s tags = %v, want %v", step.args, entry.Name, entry.Tags, want)
					}
				}
			}
		})
	}
}

func TestIntegration_TagRejectsUnregisteredCheckout(t *testing.T) {
	for _, location := range []string{"nested-repo", "outside-git"} {
		t.Run(location, func(t *testing.T) {
			testEnv(t)
			base := t.TempDir()
			root := filepath.Join(base, "registered")
			initRepo(t, root)
			runSokoAt(t, root, "init", "--tag", "original")

			dir := base
			wantError := "resolving current repository"
			if location == "nested-repo" {
				dir = filepath.Join(root, "nested-repo")
				initRepo(t, dir)
				dir = filepath.Join(dir, "src")
				if err := os.Mkdir(dir, 0o755); err != nil {
					t.Fatal(err)
				}
				wantError = "not a registered repo"
			}
			t.Chdir(dir)
			for _, args := range [][]string{{"tag", "backend"}, {"tag", "add", "backend"}, {"tag", "remove", "original"}} {
				_, err := runSokoErr(t, args...)
				if err == nil || !strings.Contains(err.Error(), wantError) {
					t.Fatalf("soko %v: error = %v, want %q", args, err, wantError)
				}
			}
			cfg := loadRegistry(t)
			if len(cfg.Repos) != 1 || !slices.Equal(cfg.Repos[0].Tags, []string{"original"}) {
				t.Fatalf("failed tag commands changed registry: %+v", cfg.Repos)
			}
		})
	}
}
