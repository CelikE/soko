package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/CelikE/soko/internal/config"
	"github.com/CelikE/soko/internal/git"
)

func TestIntegration_StashPopRepoFilters(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		want    []string
		message string
	}{
		{
			name: "exact name does not include prefix matches",
			args: []string{"auth"},
			want: []string{"auth"},
		},
		{
			name: "prefix matches",
			args: []string{"au", "--json"},
			want: []string{"auth", "auth-api"},
		},
		{
			name: "multiple names",
			args: []string{"auth", "web", "--json"},
			want: []string{"auth", "web"},
		},
		{
			name: "names intersect tag filter",
			args: []string{"auth", "web", "--tag", "backend", "--json"},
			want: []string{"auth"},
		},
		{
			name:    "unknown name restores nothing",
			args:    []string{"missing"},
			message: "no repos found matching: missing",
		},
		{
			name:    "name excluded by tag restores nothing",
			args:    []string{"auth", "--tag", "frontend"},
			message: "no repos found matching: auth",
		},
		{
			name: "no names restores all repos",
			args: []string{"--json"},
			want: []string{"auth", "auth-api", "api", "web"},
		},
		{
			name: "tag only",
			args: []string{"--tag", "backend", "--json"},
			want: []string{"auth", "auth-api", "api"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testEnv(t)
			base := t.TempDir()
			repos := []config.RepoEntry{
				{Name: "auth", Tags: []string{"backend"}},
				{Name: "auth-api", Tags: []string{"backend"}},
				{Name: "api", Tags: []string{"backend"}},
				{Name: "web", Tags: []string{"frontend"}},
			}
			stashes := make(map[string]string, len(repos))
			for i := range repos {
				repo := &repos[i]
				repo.Path = filepath.Join(base, repo.Name)
				initRepo(t, repo.Path)
				if err := os.WriteFile(filepath.Join(repo.Path, "README.md"), []byte("# work in "+repo.Name+"\n"), 0o644); err != nil {
					t.Fatalf("dirtying %s: %v", repo.Name, err)
				}
				runGit(t, repo.Path, "stash", "push", "-m", "soko stash")
				sha, err := git.Run(t.Context(), repo.Path, "rev-parse", "refs/stash")
				if err != nil {
					t.Fatal(err)
				}
				stashes[repo.Name] = sha
			}
			if err := config.Save(&config.Config{Repos: repos}); err != nil {
				t.Fatalf("saving config: %v", err)
			}

			out := runSoko(t, append([]string{"stash", "pop"}, tt.args...)...)
			if tt.message != "" && !strings.Contains(out, tt.message) {
				t.Errorf("stash pop output = %q, want %q", out, tt.message)
			}
			if slices.Contains(tt.args, "--json") {
				var entries []struct {
					Name   string `json:"name"`
					Action string `json:"action"`
					Status string `json:"status"`
				}
				if err := json.Unmarshal([]byte(out), &entries); err != nil {
					t.Fatalf("parsing JSON: %v\noutput: %s", err, out)
				}
				names := make([]string, len(entries))
				for i, entry := range entries {
					names[i] = entry.Name
					if entry.Action != "pop" || entry.Status != "restored" {
						t.Errorf("entry = %+v, want pop/restored", entry)
					}
				}
				if !slices.Equal(names, tt.want) {
					t.Errorf("restored repos = %v, want %v", names, tt.want)
				}
			}

			for _, repo := range repos {
				wantContent := "# test\n"
				wantStash := stashes[repo.Name]
				if slices.Contains(tt.want, repo.Name) {
					wantContent = "# work in " + repo.Name + "\n"
					wantStash = ""
				}
				content, err := os.ReadFile(filepath.Join(repo.Path, "README.md"))
				if err != nil {
					t.Fatal(err)
				}
				if string(content) != wantContent {
					t.Errorf("%s README = %q, want %q", repo.Name, content, wantContent)
				}
				stash, err := git.Run(t.Context(), repo.Path, "stash", "list", "--format=%H")
				if err != nil {
					t.Fatal(err)
				}
				if stash != wantStash {
					t.Errorf("%s stash = %q, want %q", repo.Name, stash, wantStash)
				}
			}
		})
	}
}

func TestIntegration_StashPopRepoCompletion(t *testing.T) {
	testEnv(t)
	if err := config.Save(&config.Config{Repos: []config.RepoEntry{
		{Name: "auth", Path: filepath.Join(t.TempDir(), "auth")},
	}}); err != nil {
		t.Fatal(err)
	}

	out := runSoko(t, "__complete", "stash", "pop", "")
	if !strings.Contains(out, "auth\n") || !strings.Contains(out, ":4\n") {
		t.Errorf("stash pop completion = %q, want auth and no file completion", out)
	}
}
