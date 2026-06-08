package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/CelikE/soko/internal/config"
	"github.com/CelikE/soko/internal/output"
)

// runRootIO executes a soko command line and returns stdout and stderr.
func runRootIO(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	var outBuf, errBuf bytes.Buffer
	cmd := NewRootCmd("test")
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	cmd.SetArgs(args)
	err = cmd.Execute()
	return outBuf.String(), errBuf.String(), err
}

func TestQuietStatusSuppressesSummaryAndHint(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Cleanup(func() { output.SetQuiet(false) })
	// Fake paths render "not found" rows (a table) plus a prune hint.
	saveConfig(t,
		config.RepoEntry{Name: "api", Path: "/x/api"},
		config.RepoEntry{Name: "web", Path: "/x/web"},
	)

	loud, _, err := runRootIO(t, "status")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !strings.Contains(loud, "behind") {
		t.Fatalf("plain status missing summary footer:\n%s", loud)
	}

	quiet, _, err := runRootIO(t, "status", "--quiet")
	if err != nil {
		t.Fatalf("status --quiet: %v", err)
	}
	if !strings.Contains(quiet, "api") {
		t.Errorf("quiet status dropped the table:\n%s", quiet)
	}
	if strings.Contains(quiet, "behind") {
		t.Errorf("quiet status still printed the summary footer:\n%s", quiet)
	}
	if strings.Contains(quiet, "soko prune") {
		t.Errorf("quiet status still printed the prune hint:\n%s", quiet)
	}
}

func TestQuietStatusJSON(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Cleanup(func() { output.SetQuiet(false) })
	saveConfig(t, config.RepoEntry{Name: "api", Path: "/x/api"})

	out, _, err := runRootIO(t, "status", "--quiet", "--json")
	if err != nil {
		t.Fatalf("status --quiet --json: %v", err)
	}
	if !strings.HasPrefix(strings.TrimSpace(out), "[") {
		t.Errorf("quiet --json stdout should start with [ (no info line before it):\n%s", out)
	}
	var decoded []map[string]any
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		t.Fatalf("quiet --json is not valid JSON: %v\n%s", err, out)
	}
}

func TestQuietEnvVar(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Cleanup(func() { output.SetQuiet(false) })
	saveConfig(t, config.RepoEntry{Name: "api", Path: "/x/api"})

	// SOKO_QUIET=1 behaves like --quiet without the flag.
	t.Setenv("SOKO_QUIET", "1")
	out, _, err := runRootIO(t, "status")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if strings.Contains(out, "behind") {
		t.Errorf("SOKO_QUIET=1 did not suppress the summary:\n%s", out)
	}

	// An explicit --quiet=false overrides the env (flag precedence).
	out, _, err = runRootIO(t, "status", "--quiet=false")
	if err != nil {
		t.Fatalf("status --quiet=false: %v", err)
	}
	if !strings.Contains(out, "behind") {
		t.Errorf("--quiet=false should override SOKO_QUIET=1 and show the summary:\n%s", out)
	}
}

func TestQuietMalformedEnvIsOff(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Cleanup(func() { output.SetQuiet(false) })
	saveConfig(t, config.RepoEntry{Name: "api", Path: "/x/api"})

	// A garbage SOKO_QUIET value is treated as off, never crashes.
	t.Setenv("SOKO_QUIET", "banana")
	out, _, err := runRootIO(t, "status")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !strings.Contains(out, "behind") {
		t.Errorf("malformed SOKO_QUIET should be off (summary present):\n%s", out)
	}
}

func TestQuietEmptyRegistry(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Cleanup(func() { output.SetQuiet(false) })

	out, _, err := runRootIO(t, "status", "--quiet")
	if err != nil {
		t.Fatalf("status --quiet on empty registry: %v", err)
	}
	if strings.TrimSpace(out) != "" {
		t.Errorf("quiet empty-registry status should be silent, got:\n%s", out)
	}
}
