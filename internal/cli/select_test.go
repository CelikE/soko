package cli_test

import (
	"bytes"
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CelikE/soko/internal/cli"
)

// execErr runs a soko command and returns (stdout, error) without t.Fatal on
// failure, so tests can assert on the error.
func execErr(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var stdout bytes.Buffer
	cmd := cli.NewRootCmd("test")
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs(args)
	return stdout.String(), cmd.Execute()
}

func TestIntegration_CleanSelectJSONConflict(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "repo")
	initRepo(t, dir)
	runSokoInit(t, dir)

	_, err := execErr(t, "clean", "--select", "--json")
	if err == nil || !strings.Contains(err.Error(), "--select cannot be combined with --json") {
		t.Errorf("clean --select --json error = %v, want combination error", err)
	}
}

func TestIntegration_PruneSelectJSONConflict(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "repo")
	initRepo(t, dir)
	runSokoInit(t, dir)

	// --dry-run lets the json-requires-force guard pass so we reach (and assert)
	// the select+json guard specifically.
	_, err := execErr(t, "prune", "--select", "--json", "--dry-run")
	if err == nil || !strings.Contains(err.Error(), "--select cannot be combined with --json") {
		t.Errorf("prune --select --json error = %v, want combination error", err)
	}
}

func TestIntegration_RemoveSelectJSONConflict(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "repo")
	initRepo(t, dir)
	runSokoInit(t, dir)

	_, err := execErr(t, "remove", "--all", "--select", "--json")
	if err == nil || !strings.Contains(err.Error(), "--select cannot be combined with --json") {
		t.Errorf("remove --all --select --json error = %v, want combination error", err)
	}
}

func TestIntegration_PruneSelectNonTTYFallback(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "gone-repo")
	initRepo(t, dir)
	runSokoInit(t, dir)

	// Delete the directory so it becomes a prune target, then prune with
	// --select on a non-terminal stdin: --select is ignored, all targets pruned.
	if err := exec.CommandContext(context.Background(), "rm", "-rf", dir).Run(); err != nil {
		t.Fatalf("removing dir: %v", err)
	}

	out := runSoko(t, "prune", "--select", "--force")
	if !strings.Contains(out, "pruned 1") {
		t.Errorf("prune --select --force (non-tty) = %q, want 'pruned 1'", out)
	}
}

func TestIntegration_RemoveAllSelectNonTTYFallback(t *testing.T) {
	testEnv(t)
	base := t.TempDir()
	for _, name := range []string{"repo-a", "repo-b"} {
		dir := filepath.Join(base, name)
		initRepo(t, dir)
		runSokoInit(t, dir)
	}

	// Non-terminal stdin: --select is ignored, behaves like remove --all.
	out := runSoko(t, "remove", "--all", "--select", "--force")
	if !strings.Contains(out, "removed all 2 repos") {
		t.Errorf("remove --all --select --force (non-tty) = %q, want 'removed all 2 repos'", out)
	}
}

func TestIntegration_CleanSelectNonTTYFallback(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "clean-repo")
	initRepo(t, dir)
	runSokoInit(t, dir)

	// Create a branch merged into the default branch, then return to default so
	// it shows up as a stale merged branch.
	ctx := context.Background()
	for _, args := range [][]string{
		{"checkout", "-b", "feature/done"},
		{"checkout", "-"},
	} {
		c := exec.CommandContext(ctx, "git", args...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	// --select on non-tty stdin falls back to the full set; --force skips the
	// prompt, so the merged branch is deleted (parity with plain clean).
	out := runSoko(t, "clean", "--select", "--force")
	if !strings.Contains(out, "deleted") || !strings.Contains(out, "feature/done") {
		// feature/done may appear only in the table; assert deletion happened.
		if !strings.Contains(out, "deleted 1 stale branch") {
			t.Errorf("clean --select --force (non-tty) = %q, want a deletion", out)
		}
	}
}
