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

func TestIntegration_DiffRepositoryFailures(t *testing.T) {
	for _, tt := range []struct {
		name    string
		missing bool
		broken  bool
		mixed   bool
	}{
		{name: "missing repository", missing: true},
		{name: "git failure", broken: true},
		{name: "mixed repositories", missing: true, broken: true, mixed: true},
	} {
		for _, mode := range []string{"text", "quiet", "json"} {
			t.Run(tt.name+"/"+mode, func(t *testing.T) {
				testEnv(t)
				base := t.TempDir()
				cfg := &config.Config{}
				var wantErrors []string
				if tt.missing {
					cfg.Repos = append(cfg.Repos, config.RepoEntry{Name: "missing", Path: filepath.Join(base, "missing")})
					wantErrors = append(wantErrors, "path not found")
				}
				if tt.broken {
					cfg.Repos = append(cfg.Repos, config.RepoEntry{Name: "broken", Path: t.TempDir()})
					wantErrors = append(wantErrors, "not a git repository")
				}
				if tt.mixed {
					for _, name := range []string{"clean", "dirty"} {
						path := filepath.Join(base, name)
						initRepo(t, path)
						cfg.Repos = append(cfg.Repos, config.RepoEntry{Name: name, Path: path})
					}
					if err := os.WriteFile(filepath.Join(base, "dirty", "new.txt"), []byte("change\n"), 0o644); err != nil {
						t.Fatal(err)
					}
				}
				if err := config.Save(cfg); err != nil {
					t.Fatal(err)
				}

				args := []string{"diff"}
				if mode != "text" {
					args = append(args, "--"+mode)
				}
				out, err := runSokoErr(t, args...)
				wantErr := fmt.Sprintf("%d repo", len(wantErrors))
				if len(wantErrors) != 1 {
					wantErr += "s"
				}
				wantErr += " failed to diff"
				if err == nil || err.Error() != wantErr {
					t.Errorf("diff error = %v, want %q", err, wantErr)
				}

				if mode == "json" {
					var entries []struct {
						Name  string `json:"name"`
						Path  string `json:"path"`
						Files []struct {
							Name string `json:"name"`
						} `json:"files"`
						Error     string  `json:"error"`
						ErrorCode *string `json:"error_code"`
					}
					if err := json.Unmarshal([]byte(out), &entries); err != nil {
						t.Fatalf("parse JSON: %v\n%s", err, out)
					}
					wantCount := len(wantErrors)
					if tt.mixed {
						wantCount++
					}
					if len(entries) != wantCount {
						t.Fatalf("got %d results, want %d\n%s", len(entries), wantCount, out)
					}
					for i, want := range wantErrors {
						e := entries[i]
						wantCode := "git_failure"
						if cfg.Repos[i].Name == "missing" {
							wantCode = "path_missing"
						}
						if e.Name != cfg.Repos[i].Name || e.Path != cfg.Repos[i].Path || !strings.Contains(e.Error, want) || e.ErrorCode == nil || *e.ErrorCode != wantCode {
							t.Errorf("failure result = %+v, want %s with %q/%s", e, cfg.Repos[i].Name, want, wantCode)
						}
						if e.Files == nil || len(e.Files) != 0 {
							t.Errorf("failure files = %+v, want empty array", e.Files)
						}
					}
					if tt.mixed {
						e := entries[len(entries)-1]
						if e.Name != "dirty" || e.Error != "" || e.ErrorCode != nil || len(e.Files) != 1 || e.Files[0].Name != "new.txt" {
							t.Errorf("dirty result = %+v, want successful new.txt result", e)
						}
					}
					return
				}

				for i, want := range wantErrors {
					if !strings.Contains(out, cfg.Repos[i].Name+": ") || !strings.Contains(out, want) {
						t.Errorf("missing failure for %s: %q\n%s", cfg.Repos[i].Name, want, out)
					}
				}
				if strings.Contains(out, "all repos clean") || strings.Contains(out, "(0 files)") {
					t.Errorf("misleading clean result:\n%s", out)
				}
				wantHint := tt.missing && mode == "text"
				if strings.Contains(out, "soko prune") != wantHint {
					t.Errorf("prune hint present = %t, want %t\n%s", strings.Contains(out, "soko prune"), wantHint, out)
				}
				if tt.mixed {
					if !strings.Contains(out, "new.txt") {
						t.Errorf("missing successful diff:\n%s", out)
					}
					if mode == "text" && !strings.Contains(out, "1 repo · 1 file changed") {
						t.Errorf("summary should count only changed repositories:\n%s", out)
					}
				} else if strings.Contains(out, "changed") {
					t.Errorf("failure-only output should not claim changes:\n%s", out)
				}
			})
		}
	}
}

func TestIntegration_DiffCleanJSON(t *testing.T) {
	testEnv(t)
	path := filepath.Join(t.TempDir(), "clean")
	initRepo(t, path)
	if err := config.Save(&config.Config{Repos: []config.RepoEntry{{Name: "clean", Path: path}}}); err != nil {
		t.Fatal(err)
	}
	if out := runSoko(t, "diff", "--json"); strings.TrimSpace(out) != "[]" {
		t.Errorf("clean JSON = %q, want []", out)
	}
}
