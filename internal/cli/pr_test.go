package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/CelikE/soko/internal/git"
	"github.com/CelikE/soko/internal/output"
)

func TestRenderPrList(t *testing.T) {
	prs := []prItem{
		{Number: 7, Title: "fix login", Branch: "fix/login", Author: "alice"},
		{Number: 12, Title: "add cache", Branch: "feat/cache", Author: "bob"},
	}
	var buf bytes.Buffer
	renderPrList(&buf, "api", prs)
	out := buf.String()

	for _, want := range []string{"api", "#7", "fix login", "fix/login", "alice", "#12", "bob"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func fakePRCLI(t *testing.T) (argsPath, dirPath string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake gh requires a POSIX shell")
	}
	dir := t.TempDir()
	argsPath = filepath.Join(dir, "args")
	dirPath = filepath.Join(dir, "cwd")
	script := `#!/bin/sh
printf '%s\n' "$@" > "$SOKO_TEST_GH_ARGS"
pwd -P > "$SOKO_TEST_GH_DIR"
printf '%s' "$SOKO_TEST_GH_STDOUT"
printf '%s' "$SOKO_TEST_GH_STDERR" >&2
exit "$SOKO_TEST_GH_EXIT"
`
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("SOKO_TEST_GH_ARGS", argsPath)
	t.Setenv("SOKO_TEST_GH_DIR", dirPath)
	t.Setenv("SOKO_TEST_GH_STDOUT", "[]")
	t.Setenv("SOKO_TEST_GH_STDERR", "")
	t.Setenv("SOKO_TEST_GH_EXIT", "0")
	return argsPath, dirPath
}

func TestPRCommandLimit(t *testing.T) {
	argsPath, dirPath := fakePRCLI(t)
	t.Setenv("SOKO_QUIET", "")
	t.Cleanup(func() { output.SetQuiet(false) })
	repo := t.TempDir()
	if _, err := git.Run(context.Background(), repo, "init"); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(repo, "nested")
	if err := os.Mkdir(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(nested)
	root, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name     string
		args     []string
		count    int
		limit    int
		wantHint bool
	}{
		{name: "default capped", count: 50, limit: 50, wantHint: true},
		{name: "larger limit JSON", args: []string{"--limit", "100", "--json"}, count: 60, limit: 100},
		{name: "capped JSON", args: []string{"--limit", "1", "--json"}, count: 1, limit: 1, wantHint: true},
		{name: "below cap", count: 2, limit: 50},
		{name: "empty JSON", args: []string{"--json"}, limit: 50},
		{name: "quiet capped JSON", args: []string{"--json", "--quiet"}, count: 50, limit: 50},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := make([]map[string]any, tt.count)
			for i := range raw {
				raw[i] = map[string]any{
					"number": i + 1, "title": fmt.Sprintf("PR %d", i+1),
					"headRefName": fmt.Sprintf("fix/%d", i+1), "state": "OPEN",
					"url":    fmt.Sprintf("https://example.com/pull/%d", i+1),
					"author": map[string]string{"login": "alice"},
				}
			}
			data, err := json.Marshal(raw)
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv("SOKO_TEST_GH_STDOUT", string(data))
			var stdout, stderr bytes.Buffer
			cmd := NewRootCmd("test")
			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)
			cmd.SetArgs(append([]string{"pr"}, tt.args...))
			if err := cmd.Execute(); err != nil {
				t.Fatal(err)
			}
			if hint := strings.Contains(stderr.String(), "increase --limit"); hint != tt.wantHint {
				t.Errorf("stderr = %q, want limit hint: %v", stderr.String(), tt.wantHint)
			}
			if slices.Contains(tt.args, "--json") {
				var prs []prItem
				if err := json.Unmarshal(stdout.Bytes(), &prs); err != nil {
					t.Fatalf("invalid JSON: %v\n%s", err, stdout.String())
				}
				if prs == nil || len(prs) != tt.count {
					t.Fatalf("PRs = %+v, want array of %d", prs, tt.count)
				}
				for i, pr := range prs {
					want := prItem{
						Number: i + 1, Title: fmt.Sprintf("PR %d", i+1), Branch: fmt.Sprintf("fix/%d", i+1),
						State: "OPEN", URL: fmt.Sprintf("https://example.com/pull/%d", i+1), Author: "alice",
					}
					if pr != want {
						t.Errorf("PR %d = %+v, want %+v", i, pr, want)
					}
				}
			} else if !strings.Contains(stdout.String(), fmt.Sprintf("#%d", tt.count)) {
				t.Errorf("output missing final PR #%d:\n%s", tt.count, stdout.String())
			}
			args, err := os.ReadFile(argsPath)
			if err != nil {
				t.Fatal(err)
			}
			wantArgs := fmt.Sprintf("pr\nlist\n--state\nopen\n--limit\n%d\n--json\nnumber,title,headRefName,state,url,author\n", tt.limit)
			if string(args) != wantArgs {
				t.Errorf("gh args = %q, want %q", args, wantArgs)
			}
			cwd, err := os.ReadFile(dirPath)
			if err != nil {
				t.Fatal(err)
			}
			if strings.TrimSpace(string(cwd)) != root {
				t.Errorf("gh cwd = %q, want repository root %q", cwd, root)
			}
		})
	}
}

func TestPRCommandValidation(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	for _, tt := range []struct {
		args []string
		want string
	}{
		{args: []string{"pr", "--limit", "0"}, want: "--limit must be greater than 0"},
		{args: []string{"pr", "--limit", "-1"}, want: "--limit must be greater than 0"},
		{args: []string{"pr"}, want: "gh CLI not found"},
	} {
		cmd := NewRootCmd("test")
		cmd.SetArgs(tt.args)
		if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("soko %v: error = %v, want %q", tt.args, err, tt.want)
		}
	}
}

func TestFetchOpenPRsErrors(t *testing.T) {
	fakePRCLI(t)
	for _, tt := range []struct {
		name   string
		stdout string
		stderr string
		exit   string
		want   string
	}{
		{name: "malformed JSON", stdout: "not JSON", exit: "0", want: "parsing gh output"},
		{name: "gh failure", stderr: "authentication required\n", exit: "1", want: "gh pr list: authentication required"},
		{name: "failure without stderr", exit: "1", want: "gh pr list: exit status 1"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("SOKO_TEST_GH_STDOUT", tt.stdout)
			t.Setenv("SOKO_TEST_GH_STDERR", tt.stderr)
			t.Setenv("SOKO_TEST_GH_EXIT", tt.exit)
			if _, err := fetchOpenPRs(context.Background(), t.TempDir(), 50); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %v, want %q", err, tt.want)
			}
		})
	}
}
