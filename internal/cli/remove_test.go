package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/CelikE/soko/internal/config"
)

func TestIntegration_RemoveCascadesWorktrees(t *testing.T) {
	for _, byPath := range []bool{false, true} {
		for _, jsonOut := range []bool{false, true} {
			name := "name"
			if byPath {
				name = "path"
			}
			if jsonOut {
				name += "-json"
			}
			t.Run(name, func(t *testing.T) {
				testEnv(t)
				root, alpha, beta := familyFixture(t)
				runSokoAt(t, root, "init")
				runSoko(t, "worktree", "adopt", "api")
				other := config.RepoEntry{Name: "other", Path: t.TempDir(), Meta: map[string]string{"note": "keep me"}}
				cfg := loadRegistry(t)
				cfg.Repos = append(cfg.Repos, other)
				if err := config.Save(cfg); err != nil {
					t.Fatal(err)
				}

				args := []string{"remove", "api"}
				if byPath {
					args = []string{"remove", "--path", resolve(t, root)}
				}
				if jsonOut {
					args = append(args, "--json")
				}
				out := runSoko(t, args...)
				if jsonOut {
					var removed []config.RepoEntry
					if err := json.Unmarshal([]byte(out), &removed); err != nil {
						t.Fatalf("decode removed entries: %v\n%s", err, out)
					}
					if len(removed) != 3 {
						t.Fatalf("removed = %+v, want parent and both worktrees", removed)
					}
					for i, entry := range cfg.Repos[:3] {
						if removed[i].Name != entry.Name || removed[i].Path != entry.Path {
							t.Errorf("removed[%d] = %+v, want %s (%s)", i, removed[i], entry.Name, entry.Path)
						}
					}
				} else {
					for _, entry := range cfg.Repos[:3] {
						if !strings.Contains(out, "removed "+entry.Name+" (") {
							t.Errorf("output = %q, missing removal of %s", out, entry.Name)
						}
					}
				}
				if got := loadRegistry(t).Repos; !reflect.DeepEqual(got, []config.RepoEntry{other}) {
					t.Fatalf("registry = %+v, want unrelated entry unchanged", got)
				}
				for _, dir := range []string{root, alpha, beta} {
					if _, err := os.Stat(filepath.Join(dir, "README.md")); err != nil {
						t.Errorf("unregistering changed checkout %s: %v", dir, err)
					}
				}
			})
		}
	}
}

func TestIntegration_RemoveWorktreeKeepsFamily(t *testing.T) {
	testEnv(t)
	root, alpha, _ := familyFixture(t)
	runSokoAt(t, root, "init")
	runSoko(t, "worktree", "adopt", "api")
	before := loadRegistry(t).Repos
	runSoko(t, "remove", "--path", resolve(t, alpha))
	var want []config.RepoEntry
	for _, entry := range before {
		if entry.Path != resolve(t, alpha) {
			want = append(want, entry)
		}
	}
	if got := loadRegistry(t).Repos; !reflect.DeepEqual(got, want) {
		t.Fatalf("registry = %+v, want parent and sibling retained: %+v", got, want)
	}
}
