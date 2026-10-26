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
	"github.com/CelikE/soko/internal/output"
)

func TestIntegration_StashReportsInspectionErrors(t *testing.T) {
	t.Cleanup(func() { output.SetQuiet(false) })
	for _, action := range []string{"push", "pop"} {
		for _, format := range []string{"text", "quiet", "json", "json-quiet"} {
			t.Run(action+"/"+format, func(t *testing.T) {
				testEnv(t)
				base := t.TempDir()
				repos := []config.RepoEntry{
					{Name: "broken", Path: filepath.Join(base, "broken")},
					{Name: "good", Path: filepath.Join(base, "good")},
					{Name: "clean", Path: filepath.Join(base, "clean")},
				}
				for _, repo := range repos {
					initRepo(t, repo.Path)
				}
				if err := config.Save(&config.Config{Repos: repos}); err != nil {
					t.Fatal(err)
				}
				// Both status and stash list must fail without relying on permissions.
				if err := os.WriteFile(filepath.Join(repos[0].Path, ".git", "config"), []byte("[invalid\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				good := repos[1].Path
				readme := filepath.Join(good, "README.md")
				if err := os.WriteFile(readme, []byte("# work in progress\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				args := []string{"stash"}
				wantStatus, wantError := "stashed", "1 repo failed to stash"
				if action == "pop" {
					runGit(t, good, "stash", "push", "-m", "soko stash")
					args = append(args, "pop")
					wantStatus, wantError = "restored", "1 repo failed to pop stash"
				}
				jsonFlag := strings.HasPrefix(format, "json")
				if jsonFlag {
					args = append(args, "--json")
				}
				if strings.Contains(format, "quiet") {
					args = append(args, "--quiet")
				}

				out, err := runSokoErr(t, args...)
				if err == nil || err.Error() != wantError {
					t.Errorf("stash %s error = %v, want %q", action, err, wantError)
				}
				if jsonFlag {
					var entries []struct {
						Name      string  `json:"name"`
						Path      string  `json:"path"`
						Action    string  `json:"action"`
						Status    string  `json:"status"`
						Error     string  `json:"error"`
						ErrorCode *string `json:"error_code"`
					}
					if err := json.Unmarshal([]byte(out), &entries); err != nil {
						t.Fatalf("parsing JSON: %v\noutput: %s", err, out)
					}
					if len(entries) != 2 {
						t.Fatalf("stash %s output = %s, want broken and good results only", action, out)
					}
					broken, good := entries[0], entries[1]
					if broken.Name != "broken" || broken.Path != repos[0].Path || broken.Action != action || broken.Status != "failed" || broken.Error == "" || broken.ErrorCode == nil || *broken.ErrorCode != "git_failure" {
						t.Errorf("broken result = %+v, want failed/git_failure with Git error", broken)
					}
					if good.Name != "good" || good.Path != repos[1].Path || good.Action != action || good.Status != wantStatus || good.Error != "" || good.ErrorCode != nil {
						t.Errorf("good result = %+v, want %s with no error", good, wantStatus)
					}
				} else {
					if !strings.Contains(out, "broken — git ") || !strings.Contains(out, "bad config line") {
						t.Errorf("stash %s output = %q, want broken repo and Git error", action, out)
					}
					if !strings.Contains(out, "good — "+wantStatus) || strings.Contains(out, "clean —") {
						t.Errorf("stash %s output = %q, want successful repo and no clean repo", action, out)
					}
				}

				wantReadme := "# test\n"
				if action == "pop" {
					wantReadme = "# work in progress\n"
				}
				if content, err := os.ReadFile(readme); err != nil || string(content) != wantReadme {
					t.Errorf("good README after %s = %q, %v; want %q", action, content, err, wantReadme)
				}
				stash, err := git.Run(t.Context(), good, "stash", "list")
				if err != nil {
					t.Fatal(err)
				}
				if (action == "push") != strings.Contains(stash, "soko stash") {
					t.Errorf("good stash after %s = %q", action, stash)
				}
			})
		}
	}
}

func TestIntegration_StashIncludesUntrackedFiles(t *testing.T) {
	for _, trackedChanges := range []bool{false, true} {
		for _, format := range []string{"text", "json"} {
			name := "untracked-only/" + format
			if trackedChanges {
				name = "tracked-and-untracked/" + format
			}
			t.Run(name, func(t *testing.T) {
				testEnv(t)
				repo := filepath.Join(t.TempDir(), "stash-repo")
				initRepo(t, repo)
				runSokoInit(t, repo)
				gitOut := func(args ...string) string {
					t.Helper()
					out, err := git.Run(t.Context(), repo, args...)
					if err != nil {
						t.Fatal(err)
					}
					return out
				}

				// An older soko stash must remain below the newly saved work.
				readme := filepath.Join(repo, "README.md")
				if err := os.WriteFile(readme, []byte("# older work\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				gitOut("stash", "push", "-m", "soko stash")
				olderStash := gitOut("rev-parse", "refs/stash")

				if err := os.MkdirAll(filepath.Join(repo, "notes"), 0o755); err != nil {
					t.Fatal(err)
				}
				files := map[string]string{
					"notes/draft.txt":   "untracked draft\n",
					"ignored.log":       "ignored contents\n",
					".git/info/exclude": "ignored.log\n",
				}
				for path, content := range files {
					if err := os.WriteFile(filepath.Join(repo, path), []byte(content), 0o644); err != nil {
						t.Fatal(err)
					}
				}
				wantReadme := "# test\n"
				if trackedChanges {
					if err := os.WriteFile(readme, []byte("# staged work\n"), 0o644); err != nil {
						t.Fatal(err)
					}
					gitOut("add", "README.md")
					wantReadme = "# working tree changes\n"
					if err := os.WriteFile(readme, []byte(wantReadme), 0o644); err != nil {
						t.Fatal(err)
					}
				}

				pushArgs := []string{"stash"}
				popArgs := []string{"stash", "pop"}
				if format == "json" {
					pushArgs = append(pushArgs, "--json")
					popArgs = append(popArgs, "--json")
				}
				checkOutput := func(out, action, status string) {
					t.Helper()
					if format == "json" {
						var entries []struct {
							Name   string `json:"name"`
							Action string `json:"action"`
							Status string `json:"status"`
						}
						if err := json.Unmarshal([]byte(out), &entries); err != nil {
							t.Fatalf("parsing JSON: %v\noutput: %s", err, out)
						}
						if len(entries) != 1 || entries[0].Name != "stash-repo" || entries[0].Action != action || entries[0].Status != status {
							t.Errorf("stash %s output = %s, want stash-repo/%s", action, out, status)
						}
					} else if !strings.Contains(out, "stash-repo — "+status) {
						t.Errorf("stash %s output = %q, want stash-repo — %s", action, out, status)
					}
				}

				checkOutput(runSoko(t, pushArgs...), "push", "stashed")
				if status := gitOut("status", "--porcelain"); status != "" {
					t.Fatalf("status after stash = %q, want clean working tree", status)
				}
				stashes := strings.Split(gitOut("stash", "list", "--format=%H"), "\n")
				if len(stashes) != 2 || stashes[0] == olderStash || stashes[1] != olderStash {
					t.Fatalf("stashes after push = %v, want new stash above %s", stashes, olderStash)
				}
				if _, err := os.Stat(filepath.Join(repo, "notes", "draft.txt")); !os.IsNotExist(err) {
					t.Fatalf("untracked file still present after stash: %v", err)
				}
				if content, err := os.ReadFile(filepath.Join(repo, "ignored.log")); err != nil || string(content) != files["ignored.log"] {
					t.Fatalf("ignored file after stash = %q, %v", content, err)
				}

				// A second push on the now-clean tree must not add another stash.
				out := runSoko(t, pushArgs...)
				if format == "json" {
					if strings.TrimSpace(out) != "[]" {
						t.Errorf("clean stash output = %q, want []", out)
					}
				} else if !strings.Contains(out, "all repos clean") {
					t.Errorf("clean stash output = %q, want all repos clean", out)
				}
				if got := gitOut("stash", "list", "--format=%H"); got != strings.Join(stashes, "\n") {
					t.Fatalf("clean push changed stash stack: %s", got)
				}

				checkOutput(runSoko(t, popArgs...), "pop", "restored")
				for path, want := range map[string]string{
					"README.md":       wantReadme,
					"notes/draft.txt": files["notes/draft.txt"],
					"ignored.log":     files["ignored.log"],
				} {
					content, err := os.ReadFile(filepath.Join(repo, path))
					if err != nil || string(content) != want {
						t.Errorf("%s after pop = %q, %v; want %q", path, content, err, want)
					}
				}
				if got := gitOut("stash", "list", "--format=%H"); got != olderStash {
					t.Errorf("stash after pop = %q, want untouched older stash %s", got, olderStash)
				}
			})
		}
	}
}

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

func TestIntegration_StashPopTopMessage(t *testing.T) {
	for _, tt := range []struct {
		name     string
		message  string
		restore  bool
		detached bool
	}{
		{name: "soko marker", message: "soko stash", restore: true},
		{name: "detached soko marker", message: "soko stash", restore: true, detached: true},
		{name: "manual stash", message: "manual backup"},
		{name: "marker substring", message: "not a soko stash"},
		{name: "marker prefix", message: "soko stash backup"},
		{name: "multiline message", message: "soko stash\nmanual backup"},
		{name: "message body", message: "soko stash\n\nmanual backup"},
		{name: "default message contains marker"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			testEnv(t)
			repo := filepath.Join(t.TempDir(), "stash-owner")
			initRepo(t, repo)
			runSokoInit(t, repo)
			// A default WIP stash includes the current commit's subject.
			runGit(t, repo, "commit", "--allow-empty", "-m", "soko stash")
			if tt.detached {
				runGit(t, repo, "checkout", "--detach")
			}
			readme := filepath.Join(repo, "README.md")
			if err := os.WriteFile(readme, []byte("older work\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			runSoko(t, "stash")
			older := wtGit(t, repo, "rev-parse", "refs/stash")
			if err := os.WriteFile(readme, []byte("newer work\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			args := []string{"stash", "push"}
			if tt.message != "" {
				args = append(args, "-m", tt.message)
			}
			runGit(t, repo, args...)
			before := wtGit(t, repo, "stash", "list", "--format=%H %gs")

			out := runSoko(t, "stash", "pop", "--json")
			var rows []struct {
				Name   string `json:"name"`
				Status string `json:"status"`
			}
			if err := json.Unmarshal([]byte(out), &rows); err != nil {
				t.Fatal(err)
			}
			wantContent := "# test\n"
			if tt.restore {
				wantContent = "newer work\n"
				if len(rows) != 1 || rows[0].Name != "stash-owner" || rows[0].Status != "restored" {
					t.Errorf("pop = %s, want restored stash-owner", out)
				}
				if got := wtGit(t, repo, "stash", "list", "--format=%H"); got != older {
					t.Errorf("remaining stash = %s, want older stash %s", got, older)
				}
			} else {
				if len(rows) != 0 {
					t.Errorf("pop restored unrelated stash: %s", out)
				}
				if after := wtGit(t, repo, "stash", "list", "--format=%H %gs"); after != before {
					t.Errorf("pop changed unrelated stash stack\nbefore: %s\nafter: %s", before, after)
				}
				if status := wtGit(t, repo, "status", "--porcelain"); status != "" {
					t.Errorf("skipped pop changed checkout: %s", status)
				}
			}
			if data, err := os.ReadFile(readme); err != nil || string(data) != wantContent {
				t.Errorf("README = %q, %v; want %q", data, err, wantContent)
			}
		})
	}
}

func TestIntegration_StashPopConflictRetainsRecovery(t *testing.T) {
	for _, format := range []string{"text", "json"} {
		t.Run(format, func(t *testing.T) {
			testEnv(t)
			base := t.TempDir()
			conflict, good := filepath.Join(base, "conflict"), filepath.Join(base, "good")
			for _, repo := range []string{conflict, good} {
				initRepo(t, repo)
				runSokoInit(t, repo)
				if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("stashed work\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			runSoko(t, "stash")
			stash := wtGit(t, conflict, "rev-parse", "refs/stash")
			if err := os.WriteFile(filepath.Join(conflict, "README.md"), []byte("newer committed work\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			runGit(t, conflict, "commit", "-am", "conflicting change")
			head := wtGit(t, conflict, "rev-parse", "HEAD")
			args := []string{"stash", "pop"}
			if format == "json" {
				args = append(args, "--json")
			}
			out, err := runSokoErr(t, args...)
			if err == nil || err.Error() != "1 repo failed to pop stash" {
				t.Fatalf("pop = %s, %v; want one failed repo", out, err)
			}
			if format == "json" {
				var rows []struct {
					Name      string `json:"name"`
					Status    string `json:"status"`
					Error     string `json:"error"`
					ErrorCode string `json:"error_code"`
				}
				if err := json.Unmarshal([]byte(out), &rows); err != nil {
					t.Fatal(err)
				}
				if len(rows) != 2 || rows[0].Name != "conflict" || rows[0].Status != "failed" || rows[0].Error == "" || rows[0].ErrorCode == "" || rows[1].Name != "good" || rows[1].Status != "restored" {
					t.Errorf("pop results = %s, want conflict failure and good restored", out)
				}
			} else if !strings.Contains(out, "conflict —") || !strings.Contains(out, "good — restored") {
				t.Errorf("pop output = %q, want failure and success", out)
			}
			if got := wtGit(t, conflict, "rev-parse", "refs/stash"); got != stash {
				t.Errorf("conflict lost recovery stash: %s, want %s", got, stash)
			}
			if got := wtGit(t, conflict, "rev-parse", "HEAD"); got != head {
				t.Errorf("conflict moved HEAD: %s, want %s", got, head)
			}
			if got := wtGit(t, conflict, "ls-files", "--unmerged"); got == "" {
				t.Error("conflict should remain in index for manual resolution")
			}
			data, err := os.ReadFile(filepath.Join(conflict, "README.md"))
			if err != nil || !strings.Contains(string(data), "stashed work") || !strings.Contains(string(data), "newer committed work") {
				t.Errorf("conflict lost user content: %q, %v", data, err)
			}
			if data, err := os.ReadFile(filepath.Join(good, "README.md")); err != nil || string(data) != "stashed work\n" {
				t.Errorf("other repo was not restored: %q, %v", data, err)
			}
			if got := wtGit(t, good, "stash", "list"); got != "" {
				t.Errorf("successful pop retained stash: %s", got)
			}
		})
	}
}
