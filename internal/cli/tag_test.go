package cli_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

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
