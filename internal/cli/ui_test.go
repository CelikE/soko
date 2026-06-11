package cli_test

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CelikE/soko/internal/cli"
)

// TestIntegration_UINoTTY verifies soko ui degrades when stdin/stdout is not a
// terminal (as in the test runner) instead of trying to take over the screen.
func TestIntegration_UINoTTY(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "my-repo")
	initRepo(t, dir)
	runSokoInit(t, dir)

	var stdout, stderr bytes.Buffer
	cmd := cli.NewRootCmd("test")
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"ui"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("soko ui without a TTY should error, got nil")
	}
	if !strings.Contains(err.Error(), "interactive terminal") {
		t.Errorf("error = %q, want it to mention 'interactive terminal'", err)
	}
}

// TestIntegration_UINoRepos exits cleanly (no TTY error) when nothing is
// registered — the empty-workspace message wins before the terminal check.
func TestIntegration_UINoRepos(t *testing.T) {
	testEnv(t)

	out := runSoko(t, "ui")
	if !strings.Contains(out, "no repos registered") {
		t.Errorf("ui with no repos = %q, want the empty-workspace hint", out)
	}
}
