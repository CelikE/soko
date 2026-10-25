package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CelikE/soko/internal/config"
)

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
