package cli_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CelikE/soko/internal/config"
)

func TestIntegration_OpenRepositoryFailures(t *testing.T) {
	for _, tt := range []struct {
		name  string
		repos []string
	}{
		{name: "missing repository", repos: []string{"missing"}},
		{name: "not a repository", repos: []string{"broken"}},
		{name: "missing origin", repos: []string{"no-origin"}},
		{name: "invalid git config", repos: []string{"bad-config"}},
		{name: "mixed failures", repos: []string{"missing", "broken", "no-origin", "bad-config"}},
	} {
		for _, mode := range []string{"text", "quiet", "json"} {
			t.Run(tt.name+"/"+mode, func(t *testing.T) {
				testEnv(t)
				t.Setenv("LC_ALL", "C")
				base := t.TempDir()
				cfg := &config.Config{}
				var wantErrors []string
				missing := false
				for _, name := range tt.repos {
					path := filepath.Join(base, name)
					if name == "broken" {
						if err := os.Mkdir(path, 0o755); err != nil {
							t.Fatal(err)
						}
					} else {
						initRepo(t, path)
					}
					cfg.Repos = append(cfg.Repos, config.RepoEntry{Name: name, Path: path, Tags: []string{"work"}})
					switch name {
					case "missing":
						missing = true
						if err := os.RemoveAll(path); err != nil {
							t.Fatal(err)
						}
						wantErrors = append(wantErrors, "path not found")
					case "broken":
						wantErrors = append(wantErrors, "not a git repository")
					case "no-origin":
						wantErrors = append(wantErrors, "No such remote 'origin'")
					case "bad-config":
						if err := os.WriteFile(filepath.Join(path, ".git", "config"), []byte("[invalid\n"), 0o644); err != nil {
							t.Fatal(err)
						}
						wantErrors = append(wantErrors, "bad config line")
					}
				}
				if err := config.Save(cfg); err != nil {
					t.Fatal(err)
				}

				args := []string{"open", tt.repos[0]}
				if len(tt.repos) > 1 {
					args = []string{"open", "--tag", "work"}
				}
				if mode != "text" {
					args = append(args, "--"+mode)
				}
				out, err := runSokoErr(t, args...)
				noun := "repo"
				if len(tt.repos) > 1 {
					noun = "repos"
				}
				wantErr := fmt.Sprintf("%d %s failed to open", len(tt.repos), noun)
				if err == nil || err.Error() != wantErr {
					t.Errorf("open error = %v, want %q", err, wantErr)
				}

				if mode == "json" {
					var entries []struct {
						Name  string `json:"name"`
						Path  string `json:"path"`
						URL   string `json:"url"`
						Error string `json:"error"`
					}
					if err := json.Unmarshal([]byte(out), &entries); err != nil {
						t.Fatalf("parse JSON: %v\n%s", err, out)
					}
					if len(entries) != len(tt.repos) {
						t.Fatalf("got %d results, want %d\n%s", len(entries), len(tt.repos), out)
					}
					for i, entry := range entries {
						if entry.Name != cfg.Repos[i].Name || entry.Path != cfg.Repos[i].Path || !strings.Contains(entry.Error, wantErrors[i]) || entry.URL != "" {
							t.Errorf("result = %+v, want %s failure with %q", entry, cfg.Repos[i].Name, wantErrors[i])
						}
					}
					return
				}

				for i, want := range wantErrors {
					if !strings.Contains(out, tt.repos[i]+": ") || !strings.Contains(out, want) {
						t.Errorf("missing failure for %s: %q\n%s", tt.repos[i], want, out)
					}
				}
				if wantHint := missing && mode == "text"; strings.Contains(out, "soko prune") != wantHint {
					t.Errorf("prune hint present = %t, want %t\n%s", strings.Contains(out, "soko prune"), wantHint, out)
				}
			})
		}
	}
}
