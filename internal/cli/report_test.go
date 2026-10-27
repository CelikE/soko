package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CelikE/soko/internal/cli"
	"github.com/CelikE/soko/internal/config"
)

func TestIntegration_ReportWithActivity(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "report-repo")
	initRepo(t, dir)
	runSokoInit(t, dir)

	out := runSoko(t, "report", "--days", "1", "--all-authors")
	if !strings.Contains(out, "report-repo") {
		t.Errorf("report = %q, want 'report-repo'", out)
	}
	if !strings.Contains(out, "initial commit") {
		t.Errorf("report = %q, want 'initial commit'", out)
	}
	if !strings.Contains(out, "1 active repo") {
		t.Errorf("report = %q, want '1 active repo'", out)
	}
}

func TestIntegration_ReportNoActivity(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "old-repo")
	initRepo(t, dir)
	runSokoInit(t, dir)

	// Backdate the commit so it's outside the window.
	ctx := context.Background()
	cmd := exec.CommandContext(ctx, "git", "commit", "--amend", "--date=2020-01-01T00:00:00", "--no-edit")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_COMMITTER_DATE=2020-01-01T00:00:00")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git amend: %v\n%s", err, out)
	}

	out := runSoko(t, "report", "--days", "1", "--all-authors")
	if !strings.Contains(out, "no activity") {
		t.Errorf("report = %q, want 'no activity'", out)
	}

	out = runSoko(t, "report", "--days", "1", "--all-authors", "--json")
	if strings.TrimSpace(out) != "[]" {
		t.Errorf("report --json = %q, want empty array", out)
	}
}

func TestIntegration_ReportAuthorFilter(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "author-repo")
	initRepo(t, dir)
	runSokoInit(t, dir)

	out := runSoko(t, "report", "--days", "1", "--author", "Test")
	if !strings.Contains(out, "author-repo") {
		t.Errorf("report --author Test = %q, want 'author-repo'", out)
	}

	out = runSoko(t, "report", "--days", "1", "--author", "Nonexistent")
	if !strings.Contains(out, "no activity") {
		t.Errorf("report --author Nonexistent = %q, want 'no activity'", out)
	}
}

func TestIntegration_ReportUnresolvedDefaultAuthor(t *testing.T) {
	testEnv(t)
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	base := t.TempDir()
	first := filepath.Join(base, "first")
	second := filepath.Join(base, "second")
	initRepo(t, first)
	initRepo(t, second)
	runGit(t, first, "config", "user.name", "First Author")
	runGit(t, second, "config", "user.name", "Second Author")
	runGit(t, first, "commit", "--allow-empty", "-m", "first author commit")
	runGit(t, first, "commit", "--allow-empty", "--author=Other Person <other@example.invalid>", "-m", "other author commit")
	runGit(t, first, "commit", "--allow-empty", "--author=Global Author <global@example.invalid>", "-m", "global author commit")
	runGit(t, second, "commit", "--allow-empty", "-m", "second author commit")
	runSokoInit(t, first)
	runSokoInit(t, second)

	// No cwd-local or global identity: default must not silently become all authors.
	t.Chdir(base)
	for _, jsonOut := range []bool{false, true} {
		args := []string{"report"}
		if jsonOut {
			args = append(args, "--json")
		}
		out, err := runSokoErr(t, args...)
		if err == nil || !strings.Contains(err.Error(), "--author") || !strings.Contains(err.Error(), "--all-authors") {
			t.Errorf("report (json=%v) error = %v, want author selection guidance", jsonOut, err)
		}
		if out != "" {
			t.Errorf("report (json=%v) stdout = %q, want no unfiltered results", jsonOut, out)
		}
	}

	// Explicit selections work outside Git; literal filter must exclude other authors.
	out := runSoko(t, "report", "--author", "First Author", "--json")
	if !strings.Contains(out, "first author commit") || strings.Contains(out, "other author commit") || strings.Contains(out, "global author commit") || strings.Contains(out, "second author commit") {
		t.Errorf("explicit author report = %q, want only first author's commit", out)
	}
	out = runSoko(t, "report", "--all-authors", "--json")
	for _, message := range []string{"first author commit", "other author commit", "global author commit", "second author commit"} {
		if !strings.Contains(out, message) {
			t.Errorf("all-authors report = %q, missing %q", out, message)
		}
	}

	// Global identity remains usable when invoked outside a repository.
	globalConfig := filepath.Join(base, "global.gitconfig")
	runGit(t, base, "config", "--file", globalConfig, "user.name", "Global Author")
	t.Setenv("GIT_CONFIG_GLOBAL", globalConfig)
	out = runSoko(t, "report", "--json")
	if !strings.Contains(out, "global author commit") || strings.Contains(out, "first author commit") || strings.Contains(out, "other author commit") || strings.Contains(out, "second author commit") {
		t.Errorf("global default author report = %q, want only global author's commit", out)
	}

	// A cwd-local identity still filters results across registered repositories.
	t.Chdir(first)
	out = runSoko(t, "report", "--json")
	if !strings.Contains(out, "first author commit") || strings.Contains(out, "other author commit") || strings.Contains(out, "global author commit") || strings.Contains(out, "second author commit") {
		t.Errorf("default report inside repo = %q, want only cwd author's commit", out)
	}
}

func TestIntegration_ReportLiteralAuthor(t *testing.T) {
	for _, tc := range []struct {
		name        string
		author      string
		filter      string
		other       string
		patternType string
	}{
		{"brackets", "Dev [Work]", "[Work]", "Dev Work", "basic"},
		{"dot", "Jane.Doe (Work)", "Jane.Doe", "JaneXDoe (Work)", "basic"},
		{"backslash", `Dev\Work`, `Dev\Work`, "DevWork", "basic"},
		{"extended", "C++ (Work)", "C++", "CCC Work", "extended"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testEnv(t)
			dir := filepath.Join(t.TempDir(), "author-repo")
			initRepo(t, dir)
			runGit(t, dir, "config", "user.name", tc.author)
			runGit(t, dir, "config", "grep.patternType", tc.patternType)
			runGit(t, dir, "commit", "--allow-empty", "-m", "literal author commit")
			runGit(t, dir, "commit", "--allow-empty", "--author="+tc.other+" <other@example.com>", "-m", "other author commit")
			runSokoInit(t, dir)

			for _, mode := range []struct {
				name string
				args []string
			}{
				{"default", nil},
				{"explicit-substring", []string{"--author", tc.filter}},
			} {
				t.Run(mode.name, func(t *testing.T) {
					for _, jsonOut := range []bool{false, true} {
						args := append([]string{"report", "--days", "1"}, mode.args...)
						if jsonOut {
							args = append(args, "--json")
						}
						out := runSokoAt(t, dir, args...)
						if !strings.Contains(out, "literal author commit") {
							t.Errorf("report (json=%v) = %q, want literal author's commit", jsonOut, out)
						}
						if strings.Contains(out, "other author commit") || strings.Contains(out, "initial commit") {
							t.Errorf("report (json=%v) = %q, contains another author's commit", jsonOut, out)
						}
					}
				})
			}

			out := runSoko(t, "report", "--days", "1", "--author", ".*", "--json")
			if strings.TrimSpace(out) != "[]" {
				t.Errorf("report --author .* --json = %q, want no literal matches", out)
			}
		})
	}
}

func TestIntegration_ReportMaxTruncation(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "multi-repo")
	initRepo(t, dir)

	// Add extra commits.
	ctx := context.Background()
	for i := range 3 {
		f := filepath.Join(dir, "file"+string(rune('a'+i))+".txt")
		if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
			t.Fatalf("writing file: %v", err)
		}
		add := exec.CommandContext(ctx, "git", "add", ".")
		add.Dir = dir
		if out, err := add.CombinedOutput(); err != nil {
			t.Fatalf("git add: %v\n%s", err, out)
		}
		cm := exec.CommandContext(ctx, "git", "commit", "-m", "commit "+string(rune('a'+i)))
		cm.Dir = dir
		cm.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null")
		if out, err := cm.CombinedOutput(); err != nil {
			t.Fatalf("git commit: %v\n%s", err, out)
		}
	}

	runSokoInit(t, dir)

	out := runSoko(t, "report", "--days", "1", "--max", "2", "--all-authors")
	if !strings.Contains(out, "+") || !strings.Contains(out, "more") {
		t.Errorf("report --max 2 = %q, want '+N more'", out)
	}
}

func TestIntegration_ReportJSON(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "json-repo")
	initRepo(t, dir)
	runSokoInit(t, dir)

	out := runSoko(t, "report", "--days", "1", "--json", "--all-authors")

	var results []struct {
		Name    string `json:"name"`
		Branch  string `json:"branch"`
		Commits []struct {
			Message string `json:"message"`
		} `json:"commits"`
	}
	if err := json.Unmarshal([]byte(out), &results); err != nil {
		t.Fatalf("json unmarshal: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 repo, got %d", len(results))
	}
	if results[0].Name != "json-repo" {
		t.Errorf("name = %q, want 'json-repo'", results[0].Name)
	}
}

func TestIntegration_ReportRepoArg(t *testing.T) {
	testEnv(t)
	dir1 := filepath.Join(t.TempDir(), "alpha")
	dir2 := filepath.Join(t.TempDir(), "bravo")
	initRepo(t, dir1)
	initRepo(t, dir2)
	runSokoInit(t, dir1)
	runSokoInit(t, dir2)

	out := runSoko(t, "report", "alpha", "--days", "1", "--all-authors")
	if !strings.Contains(out, "alpha") {
		t.Errorf("report alpha = %q, want 'alpha'", out)
	}
	if strings.Contains(out, "bravo") {
		t.Errorf("report alpha = %q, should not contain 'bravo'", out)
	}
}

func TestIntegration_ReportIncomplete(t *testing.T) {
	for _, tc := range []struct {
		name  string
		mixed bool
	}{
		{name: "all failed"},
		{name: "mixed", mixed: true},
	} {
		for _, mode := range []string{"text", "quiet", "json"} {
			t.Run(tc.name+"/"+mode, func(t *testing.T) {
				testEnv(t)
				base := t.TempDir()
				cfg := &config.Config{Repos: []config.RepoEntry{
					{Name: "missing", Path: filepath.Join(base, "missing")},
					{Name: "not-git", Path: base},
				}}
				if tc.mixed {
					for _, name := range []string{"active", "inactive"} {
						path := filepath.Join(base, name)
						initRepo(t, path)
						cfg.Repos = append(cfg.Repos, config.RepoEntry{Name: name, Path: path})
					}
					runGit(t, filepath.Join(base, "active"), "commit", "--allow-empty", "--author=Dev [Work] <dev@example.invalid>", "-m", "matching activity")
				}
				if err := config.Save(cfg); err != nil {
					t.Fatal(err)
				}

				args := []string{"report", "--author", "[Work]"}
				if mode != "text" {
					args = append(args, "--"+mode)
				}
				var stdout, stderr bytes.Buffer
				cmd := cli.NewRootCmd("test")
				cmd.SetOut(&stdout)
				cmd.SetErr(&stderr)
				cmd.SetArgs(args)
				err := cmd.Execute()
				if err == nil || !strings.Contains(err.Error(), "report incomplete: failed to inspect 2 of ") {
					t.Fatalf("report error = %v, want incomplete inspection", err)
				}

				if mode == "json" {
					var results []struct {
						Name    string `json:"name"`
						Path    string `json:"path"`
						Branch  string `json:"branch"`
						Commits []struct {
							Message string `json:"message"`
						} `json:"commits"`
						Error     string  `json:"error"`
						ErrorCode *string `json:"error_code"`
					}
					if err := json.Unmarshal(stdout.Bytes(), &results); err != nil {
						t.Fatalf("invalid JSON: %v\n%s", err, stdout.String())
					}
					wantCount := 2
					if tc.mixed {
						wantCount++
					}
					if len(results) != wantCount {
						t.Fatalf("results = %+v, want %d entries", results, wantCount)
					}
					start := 0
					if tc.mixed {
						start = 1
						if results[0].Name != "active" || results[0].Branch == "" || len(results[0].Commits) != 1 || results[0].Commits[0].Message != "matching activity" || results[0].Error != "" || results[0].ErrorCode != nil {
							t.Errorf("successful activity = %+v", results[0])
						}
					}
					for i, want := range []struct{ name, code, message string }{
						{"missing", "path_missing", "path not found"},
						{"not-git", "git_failure", "not a git repository"},
					} {
						r := results[start+i]
						if r.Name != want.name || r.Path != cfg.Repos[i].Path || r.ErrorCode == nil || *r.ErrorCode != want.code || !strings.Contains(r.Error, want.message) || r.Branch != "" || len(r.Commits) != 0 {
							t.Errorf("failure = %+v, want %s: %s (%s)", r, want.name, want.message, want.code)
						}
					}
					return
				}
				if strings.Contains(stdout.String(), "no activity") || strings.Contains(stdout.String(), "inactive: missing") || strings.Contains(stdout.String(), "inactive: not-git") {
					t.Errorf("failure described as inactivity: %q", stdout.String())
				}
				for _, want := range []string{"missing (", "path not found", "not-git (", "not a git repository"} {
					if !strings.Contains(stderr.String(), want) {
						t.Errorf("stderr = %q, missing %q", stderr.String(), want)
					}
				}
				if tc.mixed && !strings.Contains(stdout.String(), "matching activity") {
					t.Errorf("lost successful activity: %q", stdout.String())
				}
			})
		}
	}
}

func TestIntegration_ReportUnbornIsInactive(t *testing.T) {
	testEnv(t)
	path := filepath.Join(t.TempDir(), "unborn")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, path, "init", "-b", "main")
	if err := config.Save(&config.Config{Repos: []config.RepoEntry{{Name: "unborn", Path: path}}}); err != nil {
		t.Fatal(err)
	}
	if got := runSoko(t, "report", "--all-authors", "--json"); strings.TrimSpace(got) != "[]" {
		t.Errorf("unborn report = %q, want empty successful result", got)
	}
}

func TestIntegration_ReportGitLogFailure(t *testing.T) {
	testEnv(t)
	path := filepath.Join(t.TempDir(), "broken-log")
	initRepo(t, path)
	// HEAD still identifies a branch, but its commit is unreadable to git log.
	if err := os.WriteFile(filepath.Join(path, ".git", "refs", "heads", "master"), []byte(strings.Repeat("0", 40)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := config.Save(&config.Config{Repos: []config.RepoEntry{{Name: "broken-log", Path: path}}}); err != nil {
		t.Fatal(err)
	}
	out, err := runSokoErr(t, "report", "--all-authors", "--json")
	if err == nil || !strings.Contains(err.Error(), "report incomplete") {
		t.Fatalf("git log failure returned %v, want incomplete report", err)
	}
	var rows []struct {
		Name      string  `json:"name"`
		Error     string  `json:"error"`
		ErrorCode *string `json:"error_code"`
	}
	if err := json.Unmarshal([]byte(out), &rows); err != nil || len(rows) != 1 {
		t.Fatalf("git log failure JSON = %q: %v", out, err)
	}
	if rows[0].Name != "broken-log" || !strings.Contains(rows[0].Error, "bad object HEAD") || rows[0].ErrorCode == nil || *rows[0].ErrorCode != "git_failure" {
		t.Errorf("git log failure = %+v, want actionable git_failure", rows[0])
	}
}
