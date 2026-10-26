package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/CelikE/soko/internal/cli"
	"github.com/CelikE/soko/internal/config"
)

// writeAndCommit writes files into a repo and commits them so git grep can
// find tracked content.
func writeAndCommit(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	ctx := context.Background()
	for name, content := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	for _, args := range [][]string{{"add", "."}, {"commit", "-m", "fixtures"}} {
		c := exec.CommandContext(ctx, "git", args...)
		c.Dir = dir
		c.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null")
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

func TestIntegration_GrepGroupsByRepoOmitsClean(t *testing.T) {
	testEnv(t)
	base := t.TempDir()
	hitA := filepath.Join(base, "hit-a")
	hitB := filepath.Join(base, "hit-b")
	clean := filepath.Join(base, "clean")
	for _, d := range []string{hitA, hitB, clean} {
		initRepo(t, d)
	}
	writeAndCommit(t, hitA, map[string]string{"x.go": "func handleAuth() {}\n"})
	writeAndCommit(t, hitB, map[string]string{"y.go": "// handleAuth here\n"})
	writeAndCommit(t, clean, map[string]string{"z.go": "nothing to see\n"})
	for _, d := range []string{hitA, hitB, clean} {
		runSokoInit(t, d)
	}

	out := runSoko(t, "grep", "handleAuth")
	if !strings.Contains(out, "hit-a") || !strings.Contains(out, "hit-b") {
		t.Errorf("grep = %q, want hit-a and hit-b", out)
	}
	if strings.Contains(out, "clean") {
		t.Errorf("grep = %q, should omit the clean repo", out)
	}
	if !strings.Contains(out, "2 repos") {
		t.Errorf("grep summary = %q, want '2 repos'", out)
	}
}

func TestIntegration_GrepNoMatches(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "repo")
	initRepo(t, dir)
	writeAndCommit(t, dir, map[string]string{"a.go": "hello\n"})
	runSokoInit(t, dir)

	out := runSoko(t, "grep", "zzznope")
	if !strings.Contains(out, "no matches in 1 repo") {
		t.Errorf("grep no-match = %q, want 'no matches in 1 repo'", out)
	}
	if out := runSoko(t, "grep", "zzznope", "--json"); strings.TrimSpace(out) != "[]" {
		t.Errorf("grep no-match JSON = %q, want []", out)
	}
}

func TestIntegration_GrepFilesOnly(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "repo")
	initRepo(t, dir)
	writeAndCommit(t, dir, map[string]string{"a.go": "TODO 1\nTODO 2\n"})
	runSokoInit(t, dir)

	out := runSoko(t, "grep", "TODO", "--files-only")
	if !strings.Contains(out, "a.go") {
		t.Errorf("grep --files-only = %q, want 'a.go'", out)
	}
	if !strings.Contains(out, "1 file") {
		t.Errorf("grep --files-only summary = %q, want '1 file'", out)
	}
}

func TestIntegration_GrepJSON(t *testing.T) {
	testEnv(t)
	base := t.TempDir()
	hit := filepath.Join(base, "hit")
	clean := filepath.Join(base, "clean")
	initRepo(t, hit)
	initRepo(t, clean)
	writeAndCommit(t, hit, map[string]string{"x.go": "func handleAuth() {}\n"})
	writeAndCommit(t, clean, map[string]string{"z.go": "nothing\n"})
	runSokoInit(t, hit)
	runSokoInit(t, clean)

	out := runSoko(t, "grep", "handleAuth", "--json")
	var entries []struct {
		Repo    string `json:"repo"`
		Path    string `json:"path"`
		Matches []struct {
			File string `json:"file"`
			Line int    `json:"line"`
			Text string `json:"text"`
		} `json:"matches"`
	}
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		t.Fatalf("json unmarshal: %v\noutput: %s", err, out)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1 (clean repo omitted)", len(entries))
	}
	if entries[0].Repo != "hit" || len(entries[0].Matches) != 1 {
		t.Errorf("entry = %+v, want repo=hit with 1 match", entries[0])
	}
	if entries[0].Matches[0].File == "" || entries[0].Matches[0].Line < 1 {
		t.Errorf("match = %+v, want file + positive line", entries[0].Matches[0])
	}
}

func TestIntegration_GrepFilesOnlyJSON(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "repo")
	initRepo(t, dir)
	writeAndCommit(t, dir, map[string]string{"a.go": "TODO here\n"})
	runSokoInit(t, dir)

	out := runSoko(t, "grep", "TODO", "--files-only", "--json")
	var entries []struct {
		Repo    string `json:"repo"`
		Matches []struct {
			File string `json:"file"`
			Line int    `json:"line"`
			Text string `json:"text"`
		} `json:"matches"`
	}
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		t.Fatalf("json unmarshal: %v\noutput: %s", err, out)
	}
	if len(entries) != 1 || len(entries[0].Matches) != 1 {
		t.Fatalf("entries = %+v, want 1 repo with 1 match", entries)
	}
	m := entries[0].Matches[0]
	if m.File == "" {
		t.Errorf("files-only json should populate file, got %+v", m)
	}
	// Files-only mode carries only file: line is 0 and text is empty.
	if m.Line != 0 || m.Text != "" {
		t.Errorf("files-only json should carry only file, got line=%d text=%q", m.Line, m.Text)
	}
}

func TestIntegration_GrepJSONPreservesFilenames(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "repo")
	initRepo(t, dir)
	files := map[string]string{
		"päth.txt":             "before\nneedle: https://example.com:8080/path\n",
		"file with spaces.txt": "needle here\n",
	}
	if runtime.GOOS != "windows" {
		files["a:b.txt"] = "needle here\n"
		files["line\nbreak.txt"] = "needle here\n"
	}
	writeAndCommit(t, dir, files)
	c := exec.CommandContext(context.Background(), "git", "config", "core.quotePath", "true")
	c.Dir = dir
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("git config: %v\n%s", err, out)
	}
	runSokoInit(t, dir)

	for _, filesOnly := range []bool{false, true} {
		args := []string{"grep", "needle", "--json"}
		if filesOnly {
			args = append(args, "--files-only")
		}
		out := runSoko(t, args...)
		var entries []struct {
			Repo    string `json:"repo"`
			Matches []struct {
				File string `json:"file"`
				Line int    `json:"line"`
				Text string `json:"text"`
			} `json:"matches"`
		}
		if err := json.Unmarshal([]byte(out), &entries); err != nil {
			t.Fatalf("json unmarshal: %v\noutput: %s", err, out)
		}
		if len(entries) != 1 || entries[0].Repo != "repo" || len(entries[0].Matches) != len(files) {
			t.Fatalf("filesOnly=%v: entries = %+v, want repo with %d matches", filesOnly, entries, len(files))
		}
		seen := make(map[string]bool)
		for _, m := range entries[0].Matches {
			content, ok := files[m.File]
			if !ok || seen[m.File] {
				t.Errorf("filesOnly=%v: unexpected or duplicate filename %q", filesOnly, m.File)
				continue
			}
			seen[m.File] = true
			if filesOnly {
				if m.Line != 0 || m.Text != "" {
					t.Errorf("files-only match = %+v, want filename only", m)
				}
			} else {
				lines := strings.Split(content, "\n")
				if m.Line < 1 || m.Line > len(lines) || m.Text != lines[m.Line-1] {
					t.Errorf("match = %+v, want exact source line", m)
				}
			}
		}
	}
}

func TestIntegration_GrepTagAndRepoFilter(t *testing.T) {
	testEnv(t)
	base := t.TempDir()
	a := filepath.Join(base, "svc-a")
	b := filepath.Join(base, "svc-b")
	initRepo(t, a)
	initRepo(t, b)
	writeAndCommit(t, a, map[string]string{"x.go": "needle\n"})
	writeAndCommit(t, b, map[string]string{"y.go": "needle\n"})
	runSokoInit(t, a)
	runSokoInit(t, b)
	runSoko(t, "tag", "add", "-r", "svc-a", "target")

	// --tag restricts the search set.
	out := runSoko(t, "grep", "needle", "--tag", "target")
	if !strings.Contains(out, "svc-a") || strings.Contains(out, "svc-b") {
		t.Errorf("grep --tag = %q, want only svc-a", out)
	}

	// Positional repo filter restricts it too.
	out = runSoko(t, "grep", "needle", "svc-b")
	if !strings.Contains(out, "svc-b") || strings.Contains(out, "svc-a") {
		t.Errorf("grep svc-b = %q, want only svc-b", out)
	}
}

func TestIntegration_GrepUnmatchedRepoFilter(t *testing.T) {
	testEnv(t)
	cfg := &config.Config{Repos: []config.RepoEntry{{
		Name: "svc-a", Path: t.TempDir(), Tags: []string{"backend"},
	}}}
	if err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}

	for _, filter := range []struct {
		name string
		args []string
	}{
		{name: "unknown name", args: []string{"unknown"}},
		{name: "unknown names", args: []string{"unknown", "missing"}},
		{name: "tag excludes named repo", args: []string{"svc-a", "--tag", "frontend"}},
	} {
		for _, mode := range []string{"text", "quiet", "json", "json-quiet"} {
			t.Run(filter.name+"/"+mode, func(t *testing.T) {
				args := append([]string{"grep", "needle"}, filter.args...)
				if strings.Contains(mode, "json") {
					args = append(args, "--json")
				}
				if strings.Contains(mode, "quiet") {
					args = append(args, "--quiet")
				}
				var stdout, stderr bytes.Buffer
				cmd := cli.NewRootCmd("test")
				cmd.SetOut(&stdout)
				cmd.SetErr(&stderr)
				cmd.SetArgs(args)
				if err := cmd.Execute(); err != nil {
					t.Fatalf("soko %v: %v", args, err)
				}
				out := strings.TrimSpace(stdout.String())
				switch mode {
				case "json", "json-quiet":
					if out != "[]" {
						t.Errorf("output = %q, want []", out)
					}
				case "quiet":
					if stdout.Len() != 0 {
						t.Errorf("quiet output = %q, want no output", stdout.String())
					}
				case "text":
					if !strings.Contains(out, "no repos found matching: "+filter.args[0]) {
						t.Errorf("output = %q, want unmatched repository hint", out)
					}
				}
				if stderr.Len() != 0 {
					t.Errorf("stderr = %q, want no progress or errors", stderr.String())
				}
			})
		}
	}
}

func TestIntegration_GrepRequiresPattern(t *testing.T) {
	testEnv(t)
	var stdout bytes.Buffer
	cmd := cli.NewRootCmd("test")
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"grep"})
	if err := cmd.Execute(); err == nil {
		t.Error("grep with no pattern should return an error")
	}
}

func TestIntegration_GrepMissingPathHint(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "gone")
	initRepo(t, dir)
	writeAndCommit(t, dir, map[string]string{"a.go": "needle\n"})
	runSokoInit(t, dir)
	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("rm: %v", err)
	}

	out, err := runSokoErr(t, "grep", "needle")
	if err == nil || err.Error() != "1 repo failed to search" {
		t.Errorf("grep error = %v, want missing repository failure", err)
	}
	if !strings.Contains(out, "soko prune") {
		t.Errorf("grep with missing repo = %q, want 'soko prune' hint", out)
	}
}

func TestIntegration_GrepRepositoryFailures(t *testing.T) {
	for _, tt := range []struct {
		name  string
		repos []string
	}{
		{name: "missing repository", repos: []string{"missing"}},
		{name: "git failure", repos: []string{"broken"}},
		{name: "mixed repositories", repos: []string{"hit", "missing", "clean", "broken"}},
	} {
		for _, mode := range []string{"text", "quiet", "json", "json-files-only"} {
			t.Run(tt.name+"/"+mode, func(t *testing.T) {
				testEnv(t)
				base := t.TempDir()
				cfg := &config.Config{}
				wantErrors := map[string]string{}
				for _, name := range tt.repos {
					path := filepath.Join(base, name)
					cfg.Repos = append(cfg.Repos, config.RepoEntry{Name: name, Path: path})
					switch name {
					case "missing":
						wantErrors[name] = "path not found"
					case "broken":
						if err := os.Mkdir(path, 0o755); err != nil {
							t.Fatal(err)
						}
						wantErrors[name] = "not a git repository"
					default:
						initRepo(t, path)
						if name == "hit" {
							writeAndCommit(t, path, map[string]string{"match.txt": "needle here\n"})
						}
					}
				}
				if err := config.Save(cfg); err != nil {
					t.Fatal(err)
				}

				args := []string{"grep", "needle"}
				switch mode {
				case "quiet":
					args = append(args, "--quiet")
				case "json":
					args = append(args, "--json")
				case "json-files-only":
					args = append(args, "--json", "--files-only")
				}
				out, err := runSokoErr(t, args...)
				plural := "repo"
				if len(wantErrors) != 1 {
					plural = "repos"
				}
				wantErr := fmt.Sprintf("%d %s failed to search", len(wantErrors), plural)
				if err == nil || err.Error() != wantErr {
					t.Errorf("grep error = %v, want %q", err, wantErr)
				}

				if strings.HasPrefix(mode, "json") {
					var entries []struct {
						Repo    string `json:"repo"`
						Path    string `json:"path"`
						Matches []struct {
							File string `json:"file"`
							Line int    `json:"line"`
							Text string `json:"text"`
						} `json:"matches"`
						Error     string  `json:"error"`
						ErrorCode *string `json:"error_code"`
					}
					if err := json.Unmarshal([]byte(out), &entries); err != nil {
						t.Fatalf("parse JSON: %v\n%s", err, out)
					}
					var wantRepos []config.RepoEntry
					for _, repo := range cfg.Repos {
						if repo.Name != "clean" {
							wantRepos = append(wantRepos, repo)
						}
					}
					if len(entries) != len(wantRepos) {
						t.Fatalf("got %d results, want %d\n%s", len(entries), len(wantRepos), out)
					}
					for i, e := range entries {
						if e.Repo != wantRepos[i].Name || e.Path != wantRepos[i].Path {
							t.Errorf("result = %+v, want repo %s at %s in config order", e, wantRepos[i].Name, wantRepos[i].Path)
						}
						if want, failed := wantErrors[e.Repo]; failed {
							wantCode := "git_failure"
							if e.Repo == "missing" {
								wantCode = "path_missing"
							}
							if !strings.Contains(e.Error, want) || e.ErrorCode == nil || *e.ErrorCode != wantCode {
								t.Errorf("failure = %+v, want %q/%s", e, want, wantCode)
							}
							if e.Matches == nil || len(e.Matches) != 0 {
								t.Errorf("failure matches = %+v, want empty array", e.Matches)
							}
							continue
						}
						if e.Error != "" || e.ErrorCode != nil || len(e.Matches) != 1 || e.Matches[0].File != "match.txt" {
							t.Fatalf("successful result = %+v, want match.txt without errors", e)
						}
						m := e.Matches[0]
						if mode == "json-files-only" {
							if m.Line != 0 || m.Text != "" {
								t.Errorf("files-only match = %+v, want filename only", m)
							}
						} else if m.Line != 1 || m.Text != "needle here" {
							t.Errorf("match = %+v, want line 1 and exact source text", m)
						}
					}
					return
				}

				for name, want := range wantErrors {
					if !strings.Contains(out, name+": ") || !strings.Contains(out, want) {
						t.Errorf("missing failure for %s: %q\n%s", name, want, out)
					}
				}
				if strings.Contains(out, "no matches") {
					t.Errorf("failed searches should not claim no matches:\n%s", out)
				}
				wantHint := wantErrors["missing"] != "" && mode == "text"
				if strings.Contains(out, "soko prune") != wantHint {
					t.Errorf("prune hint present = %t, want %t\n%s", strings.Contains(out, "soko prune"), wantHint, out)
				}
				if len(tt.repos) > 1 && !strings.Contains(out, "match.txt") {
					t.Errorf("missing successful match:\n%s", out)
				}
			})
		}
	}
}

func TestIntegration_GrepErrorExitsNonZero(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "repo")
	initRepo(t, dir)
	writeAndCommit(t, dir, map[string]string{"a.go": "hello\n"})
	runSokoInit(t, dir)

	// A malformed extended regex makes git grep error (exit >= 2).
	var stdout bytes.Buffer
	cmd := cli.NewRootCmd("test")
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"grep", "[", "--regexp"})
	if err := cmd.Execute(); err == nil {
		t.Error("grep with a bad regex should return an error")
	}
}
