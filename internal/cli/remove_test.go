package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/CelikE/soko/internal/cli"
	"github.com/CelikE/soko/internal/config"
)

func TestIntegration_RemoveRejectsExtraNames(t *testing.T) {
	for _, registry := range []string{"empty", "registered", "invalid"} {
		for _, mode := range []string{"text", "json", "all", "path"} {
			t.Run(registry+"/"+mode, func(t *testing.T) {
				xdg := testEnv(t)
				configPath := filepath.Join(xdg, "soko", "config.yaml")
				switch registry {
				case "registered":
					cfg := loadRegistry(t)
					cfg.Repos = []config.RepoEntry{
						{Name: "api", Path: t.TempDir()},
						{Name: "web", Path: t.TempDir()},
					}
					if err := config.Save(cfg); err != nil {
						t.Fatal(err)
					}
				case "invalid":
					if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(configPath, []byte("repos: ["), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				before, beforeErr := os.ReadFile(configPath)
				if beforeErr != nil && !os.IsNotExist(beforeErr) {
					t.Fatal(beforeErr)
				}
				args := []string{"remove", "api", "web"}
				switch mode {
				case "json":
					args = append(args, "--json")
				case "all":
					args = append(args, "--all", "--force")
				case "path":
					args = append(args, "--path", t.TempDir())
				}
				var stdout bytes.Buffer
				cmd := cli.NewRootCmd("test")
				cmd.SetOut(&stdout)
				cmd.SetErr(&bytes.Buffer{})
				cmd.SetArgs(args)
				if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "accepts at most 1 arg(s)") {
					t.Fatalf("want argument-count error before registry access, got %v", err)
				}
				if stdout.Len() != 0 {
					t.Errorf("rejected command produced output: %q", stdout.String())
				}
				after, afterErr := os.ReadFile(configPath)
				if !bytes.Equal(before, after) || (beforeErr == nil) != (afterErr == nil) ||
					(afterErr != nil && !os.IsNotExist(afterErr)) {
					t.Fatalf("rejected command changed config: before %q (%v), after %q (%v)", before, beforeErr, after, afterErr)
				}
			})
		}
	}
}

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
