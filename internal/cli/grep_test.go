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

	// Missing repo is skipped, not failed; command exits 0 with a prune hint.
	out := runSoko(t, "grep", "needle")
	if !strings.Contains(out, "soko prune") {
		t.Errorf("grep with missing repo = %q, want 'soko prune' hint", out)
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
