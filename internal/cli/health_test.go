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

func TestIntegration_HealthRanksCleanRepo(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "clean-repo")
	initRepo(t, dir)
	runSokoInit(t, dir)

	out := runSoko(t, "health")
	if !strings.Contains(out, "clean-repo") {
		t.Errorf("health = %q, want 'clean-repo'", out)
	}
	if !strings.Contains(out, "SEVERITY") || !strings.Contains(out, "SCORE") {
		t.Errorf("health = %q, want SEVERITY and SCORE headers", out)
	}
	// A clean repo with no remote is the only repo, so the summary counts 1.
	if !strings.Contains(out, "1 repo") {
		t.Errorf("health summary = %q, want '1 repo'", out)
	}
}

func TestIntegration_HealthJSON(t *testing.T) {
	testEnv(t)
	base := t.TempDir()
	dirtyDir := filepath.Join(base, "dirty-repo")
	cleanDir := filepath.Join(base, "clean-repo")
	initRepo(t, dirtyDir)
	initRepo(t, cleanDir)
	runSokoInit(t, dirtyDir)
	runSokoInit(t, cleanDir)

	// Make dirty-repo dirty so it outranks clean-repo.
	if err := os.WriteFile(filepath.Join(dirtyDir, "new.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("writing file: %v", err)
	}

	out := runSoko(t, "health", "--json")

	var entries []struct {
		Name     string   `json:"name"`
		Path     string   `json:"path"`
		Score    int      `json:"score"`
		Severity string   `json:"severity"`
		Reasons  []string `json:"reasons"`
	}
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		t.Fatalf("json unmarshal: %v\noutput: %s", err, out)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(entries))
	}
	// Worst-first ordering: dirty-repo (untracked file + no remote) outranks
	// the clean repo.
	if entries[0].Name != "dirty-repo" {
		t.Errorf("first entry = %q, want 'dirty-repo' (worst first)", entries[0].Name)
	}
	if entries[0].Score < entries[1].Score {
		t.Errorf("not worst-first: %d < %d", entries[0].Score, entries[1].Score)
	}
	if entries[0].Severity == "" || len(entries[0].Reasons) == 0 {
		t.Errorf("entry missing severity/reasons: %+v", entries[0])
	}
}

func TestIntegration_HealthTop(t *testing.T) {
	testEnv(t)
	base := t.TempDir()
	for _, name := range []string{"repo-a", "repo-b", "repo-c"} {
		dir := filepath.Join(base, name)
		initRepo(t, dir)
		runSokoInit(t, dir)
	}

	out := runSoko(t, "health", "--top", "2", "--json")
	var entries []map[string]any
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		t.Fatalf("json unmarshal: %v\noutput: %s", err, out)
	}
	if len(entries) != 2 {
		t.Errorf("--top 2 returned %d entries, want 2", len(entries))
	}
}

func TestIntegration_HealthThresholdCrit(t *testing.T) {
	testEnv(t)
	base := t.TempDir()
	cleanDir := filepath.Join(base, "clean-repo")
	initRepo(t, cleanDir)
	runSokoInit(t, cleanDir)

	// Only an ok/warn repo exists, so --threshold crit yields an empty ranking.
	out := runSoko(t, "health", "--threshold", "crit", "--json")
	var entries []map[string]any
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		t.Fatalf("json unmarshal: %v\noutput: %s", err, out)
	}
	if len(entries) != 0 {
		t.Errorf("--threshold crit returned %d entries, want 0", len(entries))
	}
}

func TestIntegration_HealthInvalidThreshold(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "repo")
	initRepo(t, dir)
	runSokoInit(t, dir)

	var stdout bytes.Buffer
	cmd := cli.NewRootCmd("test")
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"health", "--threshold", "bogus"})

	if err := cmd.Execute(); err == nil {
		t.Error("health --threshold bogus should return an error")
	}
}

func TestIntegration_HealthNoRepos(t *testing.T) {
	testEnv(t)

	out := runSoko(t, "health")
	if !strings.Contains(out, "no repos") {
		t.Errorf("health = %q, want 'no repos'", out)
	}
}
