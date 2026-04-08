package cli_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
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
	if !strings.Contains(out, "...and") {
		t.Errorf("report --max 2 = %q, want '...and N more'", out)
	}
}

func TestIntegration_ReportJSON(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "json-repo")
	initRepo(t, dir)
	runSokoInit(t, dir)

	out := runSoko(t, "report", "--days", "1", "--json", "--all-authors")

	var results []struct {
		Name    string   `json:"name"`
		Commits []string `json:"commits"`
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
