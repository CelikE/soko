package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CelikE/soko/internal/cli"
)

func TestIntegration_StatusPerf(t *testing.T) {
	testEnv(t)
	base := t.TempDir()
	for _, name := range []string{"perf-a", "perf-b"} {
		dir := filepath.Join(base, name)
		initRepo(t, dir)
		runSokoInit(t, dir)
	}

	// --perf appends a timing breakdown after the normal table + summary.
	out := runSoko(t, "status", "--perf")
	if !strings.Contains(out, "perf-a") || !strings.Contains(out, "perf-b") {
		t.Errorf("status --perf = %q, want both repos", out)
	}
	if !strings.Contains(out, "timing —") {
		t.Errorf("status --perf = %q, want a 'timing —' block", out)
	}

	// Plain status has neither the timing block nor a duration_ms.
	plain := runSoko(t, "status")
	if strings.Contains(plain, "timing —") {
		t.Errorf("plain status = %q, should not contain a timing block", plain)
	}
}

// perfEnvelope mirrors the --perf --json document shape.
type perfEnvelope struct {
	Repos  []map[string]any `json:"repos"`
	Timing struct {
		WallMS      int64 `json:"wall_ms"`
		GitMS       int64 `json:"git_ms"`
		Repos       int   `json:"repos"`
		Concurrency int   `json:"concurrency"`
		Slowest     []struct {
			Name       string `json:"name"`
			DurationMS int64  `json:"duration_ms"`
		} `json:"slowest"`
	} `json:"timing"`
}

func TestIntegration_StatusPerfJSON(t *testing.T) {
	testEnv(t)
	base := t.TempDir()
	for _, name := range []string{"json-a", "json-b"} {
		dir := filepath.Join(base, name)
		initRepo(t, dir)
		runSokoInit(t, dir)
	}

	out := runSoko(t, "status", "--perf", "--json")

	var env perfEnvelope
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("parsing --perf --json: %v\noutput: %s", err, out)
	}
	if len(env.Repos) != 2 {
		t.Fatalf("repos = %d, want 2", len(env.Repos))
	}
	for _, r := range env.Repos {
		if _, ok := r["duration_ms"]; !ok {
			t.Errorf("repo entry %v missing duration_ms", r["name"])
		}
	}
	if env.Timing.Repos != 2 {
		t.Errorf("timing.repos = %d, want 2", env.Timing.Repos)
	}
	// Mirrors cli.maxConcurrency.
	if env.Timing.Concurrency != 8 {
		t.Errorf("timing.concurrency = %d, want 8", env.Timing.Concurrency)
	}
}

func TestIntegration_StatusJSONNoPerfUnchanged(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "plain-json")
	initRepo(t, dir)
	runSokoInit(t, dir)

	out := runSoko(t, "status", "--json")

	// Without --perf the document is still a bare array with no duration_ms.
	var entries []map[string]any
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		t.Fatalf("status --json should stay a bare array: %v\noutput: %s", err, out)
	}
	if strings.Contains(out, "duration_ms") {
		t.Errorf("plain --json leaked duration_ms: %s", out)
	}
	if strings.Contains(out, `"timing"`) {
		t.Errorf("plain --json leaked timing envelope: %s", out)
	}
}

func TestIntegration_FetchPerf(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "fetch-perf")
	initRepo(t, dir)
	runSokoInit(t, dir)

	out := runSoko(t, "fetch", "--perf")
	if !strings.Contains(out, "timing —") {
		t.Errorf("fetch --perf = %q, want a timing block", out)
	}
}

func TestIntegration_PullPerfMissingPathStillTimed(t *testing.T) {
	testEnv(t)
	base := t.TempDir()
	good := filepath.Join(base, "good")
	gone := filepath.Join(base, "gone")
	initRepo(t, good)
	initRepo(t, gone)
	runSokoInit(t, good)
	runSokoInit(t, gone)
	if err := os.RemoveAll(gone); err != nil {
		t.Fatalf("rm: %v", err)
	}

	// A missing path fails the run; --perf still prints the breakdown that
	// includes the errored repo, proving error paths are timed.
	var stdout bytes.Buffer
	cmd := cli.NewRootCmd("test")
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"pull", "--perf"})
	err := cmd.Execute()
	if err == nil {
		t.Error("pull with a missing path should return an error")
	}

	out := stdout.String()
	if !strings.Contains(out, "timing —") {
		t.Errorf("pull --perf = %q, want a timing block even on failure", out)
	}
	if !strings.Contains(out, "gone") {
		t.Errorf("pull --perf = %q, want the errored repo in the breakdown", out)
	}
}

func TestIntegration_ExecPerfSeq(t *testing.T) {
	testEnv(t)
	base := t.TempDir()
	for _, name := range []string{"seq-perf-a", "seq-perf-b"} {
		dir := filepath.Join(base, name)
		initRepo(t, dir)
		runSokoInit(t, dir)
	}

	out := runSoko(t, "exec", "--perf", "--seq", "--", "echo", "ok")
	if !strings.Contains(out, "timing —") {
		t.Errorf("exec --perf --seq = %q, want a timing block in sequential mode", out)
	}
}

func TestIntegration_PerfEnvAndOverride(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "env-perf")
	initRepo(t, dir)
	runSokoInit(t, dir)

	t.Setenv("SOKO_PERF", "1")

	// SOKO_PERF=1 with no flag behaves like --perf.
	out := runSoko(t, "status")
	if !strings.Contains(out, "timing —") {
		t.Errorf("SOKO_PERF=1 status = %q, want a timing block", out)
	}

	// An explicit --perf=false overrides the env.
	out = runSoko(t, "status", "--perf=false")
	if strings.Contains(out, "timing —") {
		t.Errorf("status --perf=false = %q, should suppress the timing block despite SOKO_PERF", out)
	}
}
