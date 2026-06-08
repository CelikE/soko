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

func TestIntegration_Pull(t *testing.T) {
	testEnv(t)
	repo := filepath.Join(t.TempDir(), "pull-repo")
	seed := setupUpstreamClone(t, repo)
	runSokoInit(t, repo)

	// Up to date on the first pull — nothing to advance.
	out := runSoko(t, "pull")
	if !strings.Contains(out, "up to date") {
		t.Errorf("pull = %q, want a row 'up to date'", out)
	}
	if !strings.Contains(out, "0 updated") || !strings.Contains(out, "1 up to date") {
		t.Errorf("pull summary = %q, want '0 updated' and '1 up to date'", out)
	}

	// Advancing the upstream means the next pull fast-forwards.
	advanceUpstream(t, seed)
	out = runSoko(t, "pull")
	if !strings.Contains(out, "1 updated") {
		t.Errorf("pull after upstream advance summary = %q, want '1 updated'", out)
	}
	if !strings.Contains(out, "0 failed") {
		t.Errorf("pull summary = %q, want '0 failed'", out)
	}
}

// TestIntegration_PullRebaseDiverged exercises the branch where --rebase is
// genuinely different from --ff-only: a local commit AND an upstream commit, so
// the branch has diverged. The default (--ff-only) must fail, --rebase succeeds.
func TestIntegration_PullRebaseDiverged(t *testing.T) {
	testEnv(t)
	repo := filepath.Join(t.TempDir(), "diverge-repo")
	seed := setupUpstreamClone(t, repo)
	runSokoInit(t, repo)

	// Diverge: a commit locally and a different commit upstream.
	localCommit(t, repo, "local.txt", "local")
	advanceUpstream(t, seed)

	// Plain pull is --ff-only, which cannot fast-forward a diverged branch.
	var stdout bytes.Buffer
	cmd := cli.NewRootCmd("test")
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"pull"})
	if err := cmd.Execute(); err == nil {
		t.Error("pull --ff-only on a diverged branch should fail")
	}
	if got := stdout.String(); !strings.Contains(got, "1 failed") {
		t.Errorf("diverged pull = %q, want '1 failed'", got)
	}

	// --rebase replays the local commit on top of the upstream and succeeds.
	out := runSoko(t, "pull", "--rebase")
	if !strings.Contains(out, "1 updated") {
		t.Errorf("pull --rebase = %q, want '1 updated'", out)
	}
	if !strings.Contains(out, "0 failed") {
		t.Errorf("pull --rebase = %q, want '0 failed'", out)
	}
}

// TestIntegration_PullMixed registers three repos with different outcomes and
// asserts both the per-outcome counts and that rows are rendered in config
// (registration) order regardless of parallel completion order.
func TestIntegration_PullMixed(t *testing.T) {
	testEnv(t)
	base := t.TempDir()

	upToDate := filepath.Join(base, "mix-alpha")
	updated := filepath.Join(base, "mix-bravo")
	skipped := filepath.Join(base, "mix-charlie")

	setupUpstreamClone(t, upToDate)
	advSeed := setupUpstreamClone(t, updated)
	initRepo(t, skipped) // no remote -> no upstream -> skipped

	// Register in a known order.
	runSokoInit(t, upToDate)
	runSokoInit(t, updated)
	runSokoInit(t, skipped)

	advanceUpstream(t, advSeed)

	out := runSoko(t, "pull")

	if !strings.Contains(out, "1 updated") ||
		!strings.Contains(out, "1 up to date") ||
		!strings.Contains(out, "1 skipped") ||
		!strings.Contains(out, "0 failed") {
		t.Errorf("mixed pull summary = %q, want 1 updated / 1 up to date / 1 skipped / 0 failed", out)
	}
	if !strings.Contains(out, "no upstream") {
		t.Errorf("mixed pull = %q, want a 'no upstream' skip row", out)
	}

	// Rows must appear in registration order, not completion order.
	assertOrder(t, out, "mix-alpha", "mix-bravo", "mix-charlie")
}

func TestIntegration_PullJSON(t *testing.T) {
	testEnv(t)
	base := t.TempDir()

	upToDate := filepath.Join(base, "json-alpha")
	updated := filepath.Join(base, "json-bravo")
	skipped := filepath.Join(base, "json-charlie")

	setupUpstreamClone(t, upToDate)
	advSeed := setupUpstreamClone(t, updated)
	initRepo(t, skipped)

	runSokoInit(t, upToDate)
	runSokoInit(t, updated)
	runSokoInit(t, skipped)

	advanceUpstream(t, advSeed)

	out := runSoko(t, "pull", "--json")

	var entries []struct {
		Name   string `json:"name"`
		Status string `json:"status"`
		Error  string `json:"error"`
	}
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		t.Fatalf("parsing JSON: %v\noutput: %s", err, out)
	}
	if len(entries) != 3 {
		t.Fatalf("JSON entries = %d, want 3", len(entries))
	}

	byName := map[string]string{}
	for _, e := range entries {
		byName[e.Name] = e.Status
	}
	if byName["json-alpha"] != "up-to-date" {
		t.Errorf("json-alpha status = %q, want 'up-to-date'", byName["json-alpha"])
	}
	if byName["json-bravo"] != "updated" {
		t.Errorf("json-bravo status = %q, want 'updated'", byName["json-bravo"])
	}
	if byName["json-charlie"] != "skipped" {
		t.Errorf("json-charlie status = %q, want 'skipped'", byName["json-charlie"])
	}
}

func TestIntegration_PullJSONFailed(t *testing.T) {
	testEnv(t)
	repo := filepath.Join(t.TempDir(), "json-fail-repo")
	setupUpstreamClone(t, repo)
	runSokoInit(t, repo)

	if err := os.RemoveAll(repo); err != nil {
		t.Fatalf("removing dir: %v", err)
	}

	var stdout bytes.Buffer
	cmd := cli.NewRootCmd("test")
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"pull", "--json"})

	if err := cmd.Execute(); err == nil {
		t.Error("pull --json with a failure should return an error")
	}

	var entries []struct {
		Status string `json:"status"`
		Error  string `json:"error"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &entries); err != nil {
		t.Fatalf("parsing JSON: %v\noutput: %s", err, stdout.String())
	}
	if len(entries) != 1 {
		t.Fatalf("JSON entries = %d, want 1", len(entries))
	}
	if entries[0].Status != "failed" {
		t.Errorf("status = %q, want 'failed'", entries[0].Status)
	}
	if entries[0].Error == "" {
		t.Error("failed entry should populate the error field")
	}
}

// TestIntegration_PullNoUpstream verifies that a branch with no upstream is
// skipped — not counted as a failure and not a non-zero exit.
func TestIntegration_PullNoUpstream(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "no-upstream-repo")
	initRepo(t, dir) // no remote -> no upstream
	runSokoInit(t, dir)

	out := runSoko(t, "pull") // must not error
	if !strings.Contains(out, "no upstream") {
		t.Errorf("pull no upstream = %q, want 'no upstream'", out)
	}
	if !strings.Contains(out, "1 skipped") || !strings.Contains(out, "0 failed") {
		t.Errorf("pull no upstream summary = %q, want '1 skipped' and '0 failed'", out)
	}
}

func TestIntegration_PullMissingPath(t *testing.T) {
	testEnv(t)
	repo := filepath.Join(t.TempDir(), "gone-pull-repo")
	setupUpstreamClone(t, repo)
	runSokoInit(t, repo)

	if err := os.RemoveAll(repo); err != nil {
		t.Fatalf("removing dir: %v", err)
	}

	var stdout bytes.Buffer
	cmd := cli.NewRootCmd("test")
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"pull"})

	if err := cmd.Execute(); err == nil {
		t.Error("pull with missing path should return an error")
	}

	out := stdout.String()
	if !strings.Contains(out, "path not found") {
		t.Errorf("pull missing path = %q, want 'path not found'", out)
	}
	if !strings.Contains(out, "soko prune") {
		t.Errorf("pull missing path = %q, want a 'soko prune' hint", out)
	}
}

// setupUpstreamClone creates a bare upstream named after path's basename (so the
// registered repo name is predictable and distinct) with one commit, then clones
// it to path so the clone tracks it. It returns a separate seed working tree
// whose pushes (via advanceUpstream) the clone can later pull.
func setupUpstreamClone(t *testing.T, path string) (seed string) {
	t.Helper()

	bare := filepath.Join(t.TempDir(), filepath.Base(path)+".git")
	gitExec(t, "", "init", "--bare", "-b", "master", bare)

	seed = t.TempDir()
	gitExec(t, "", "clone", bare, seed)
	gitConfigure(t, seed)
	if err := os.WriteFile(filepath.Join(seed, "README.md"), []byte("# seed\n"), 0o644); err != nil {
		t.Fatalf("writing seed README: %v", err)
	}
	gitExec(t, seed, "add", ".")
	gitExec(t, seed, "commit", "-m", "initial commit")
	gitExec(t, seed, "push", "origin", "master")

	gitExec(t, "", "clone", bare, path)
	gitConfigure(t, path)

	return seed
}

// advanceUpstream commits a new file in the seed working tree and pushes it so a
// clone pulling from the same upstream will fast-forward.
func advanceUpstream(t *testing.T, seed string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(seed, "change.txt"), []byte("change\n"), 0o644); err != nil {
		t.Fatalf("writing upstream change: %v", err)
	}
	gitExec(t, seed, "add", ".")
	gitExec(t, seed, "commit", "-m", "upstream change")
	gitExec(t, seed, "push", "origin", "master")
}

// localCommit creates an unpushed commit in repo so its branch diverges from the
// upstream once advanceUpstream is also called.
func localCommit(t *testing.T, repo, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repo, name), []byte(content+"\n"), 0o644); err != nil {
		t.Fatalf("writing local file: %v", err)
	}
	gitExec(t, repo, "add", ".")
	gitExec(t, repo, "commit", "-m", "local change")
}

// assertOrder fails the test unless names appear in out in the given order.
func assertOrder(t *testing.T, out string, names ...string) {
	t.Helper()
	last := -1
	for _, n := range names {
		i := strings.Index(out, n)
		if i < 0 {
			t.Errorf("output missing %q\n%s", n, out)
			continue
		}
		if i < last {
			t.Errorf("output order wrong: %q appears before a prior name\n%s", n, out)
		}
		last = i
	}
}

// gitExec runs a git command in dir (or the current directory when dir is "")
// under an isolated global config, failing the test on error.
func gitExec(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// gitConfigure sets a local commit identity in dir.
func gitConfigure(t *testing.T, dir string) {
	t.Helper()
	gitExec(t, dir, "config", "user.email", "test@test.com")
	gitExec(t, dir, "config", "user.name", "Test")
}
