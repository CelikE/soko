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

func TestIntegration_PruneNothingToPrune(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "present-repo")
	initRepo(t, dir)
	runSokoInit(t, dir)

	out := runSoko(t, "prune")
	if !strings.Contains(out, "nothing to prune") {
		t.Errorf("prune = %q, want 'nothing to prune'", out)
	}
}

func TestIntegration_PruneNoRepos(t *testing.T) {
	testEnv(t)

	out := runSoko(t, "prune")
	if !strings.Contains(out, "no repos registered") {
		t.Errorf("prune empty = %q, want 'no repos registered'", out)
	}
}

func TestIntegration_PruneDryRun(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "gone-repo")
	initRepo(t, dir)
	runSokoInit(t, dir)

	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("removing dir: %v", err)
	}

	out := runSoko(t, "prune", "--dry-run")
	if !strings.Contains(out, "gone-repo") {
		t.Errorf("prune --dry-run = %q, want 'gone-repo'", out)
	}
	if !strings.Contains(out, "1 missing repo") {
		t.Errorf("prune --dry-run = %q, want '1 missing repo'", out)
	}

	// Dry-run must not modify the config.
	listOut := runSoko(t, "list")
	if !strings.Contains(listOut, "gone-repo") {
		t.Errorf("list after dry-run = %q, want 'gone-repo' still present", listOut)
	}
}

func TestIntegration_PruneForce(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "deleted-repo")
	initRepo(t, dir)
	runSokoInit(t, dir)

	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("removing dir: %v", err)
	}

	out := runSoko(t, "prune", "--force")
	if !strings.Contains(out, "pruned 1 missing repo") {
		t.Errorf("prune --force = %q, want 'pruned 1 missing repo'", out)
	}

	listOut := runSoko(t, "list")
	if !strings.Contains(listOut, "no repos registered") {
		t.Errorf("list after prune = %q, want 'no repos registered'", listOut)
	}
}

func TestIntegration_PruneKeepsExistingRepos(t *testing.T) {
	testEnv(t)
	base := t.TempDir()
	keep := filepath.Join(base, "keep-repo")
	gone := filepath.Join(base, "gone-repo")
	initRepo(t, keep)
	initRepo(t, gone)
	runSokoInit(t, keep)
	runSokoInit(t, gone)

	if err := os.RemoveAll(gone); err != nil {
		t.Fatalf("removing dir: %v", err)
	}

	out := runSoko(t, "prune", "--force")
	if !strings.Contains(out, "pruned 1 missing repo") {
		t.Errorf("prune --force = %q, want 'pruned 1 missing repo'", out)
	}

	listOut := runSoko(t, "list")
	if !strings.Contains(listOut, "keep-repo") {
		t.Errorf("list after prune = %q, want 'keep-repo' retained", listOut)
	}
	if strings.Contains(listOut, "gone-repo") {
		t.Errorf("list after prune = %q, should not contain 'gone-repo'", listOut)
	}
}

func TestIntegration_PruneTagScoping(t *testing.T) {
	testEnv(t)
	base := t.TempDir()
	// Names must not be substrings of each other so list assertions are exact.
	scoped := filepath.Join(base, "scoped-repo")
	other := filepath.Join(base, "loose-repo")
	initRepo(t, scoped)
	initRepo(t, other)
	runSokoInit(t, scoped)
	runSokoInit(t, other)
	runSoko(t, "tag", "add", "-r", "scoped-repo", "work")

	if err := os.RemoveAll(scoped); err != nil {
		t.Fatalf("removing dir: %v", err)
	}
	if err := os.RemoveAll(other); err != nil {
		t.Fatalf("removing dir: %v", err)
	}

	// --tag work must prune only the tagged missing repo and retain the
	// untagged-but-also-missing one.
	out := runSoko(t, "prune", "--tag", "work", "--force")
	if !strings.Contains(out, "pruned 1 missing repo") {
		t.Errorf("prune --tag work = %q, want 'pruned 1 missing repo'", out)
	}

	listOut := runSoko(t, "list")
	if strings.Contains(listOut, "scoped-repo") {
		t.Errorf("list after prune --tag = %q, should not contain 'scoped-repo'", listOut)
	}
	if !strings.Contains(listOut, "loose-repo") {
		t.Errorf("list after prune --tag = %q, want 'loose-repo' retained", listOut)
	}
}

func TestIntegration_PruneTagNoMatch(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "tagless-gone")
	initRepo(t, dir)
	runSokoInit(t, dir)

	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("removing dir: %v", err)
	}

	// A missing untagged repo exists, but the tag filter excludes it — the
	// message must not claim the whole registry is clean.
	out := runSoko(t, "prune", "--tag", "nonexistent")
	if !strings.Contains(out, "no missing repos match the tag filter") {
		t.Errorf("prune --tag nonexistent = %q, want 'no missing repos match the tag filter'", out)
	}
}

func TestIntegration_PruneJSON(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "json-gone-repo")
	initRepo(t, dir)
	runSokoInit(t, dir)

	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("removing dir: %v", err)
	}

	out := runSoko(t, "prune", "--force", "--json")

	var entries []map[string]any
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		t.Fatalf("parsing JSON: %v\noutput: %s", err, out)
	}
	if len(entries) != 1 {
		t.Fatalf("JSON entries = %d, want 1", len(entries))
	}
	if entries[0]["name"] != "json-gone-repo" {
		t.Errorf("JSON name = %v, want 'json-gone-repo'", entries[0]["name"])
	}
}

func TestIntegration_PruneDryRunJSON(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "dryjson-repo")
	initRepo(t, dir)
	runSokoInit(t, dir)

	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("removing dir: %v", err)
	}

	out := runSoko(t, "prune", "--dry-run", "--json")

	var entries []map[string]any
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		t.Fatalf("parsing JSON: %v\noutput: %s", err, out)
	}
	if len(entries) != 1 || entries[0]["name"] != "dryjson-repo" {
		t.Fatalf("JSON = %v, want one entry named 'dryjson-repo'", entries)
	}

	// Dry-run must not modify the config.
	listOut := runSoko(t, "list")
	if !strings.Contains(listOut, "dryjson-repo") {
		t.Errorf("list after dry-run --json = %q, want 'dryjson-repo' retained", listOut)
	}
}

func TestIntegration_PruneJSONEmptyArrayAllPresent(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "present-json-repo")
	initRepo(t, dir)
	runSokoInit(t, dir)

	out := runSoko(t, "prune", "--dry-run", "--json")

	var entries []map[string]any
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		t.Fatalf("parsing JSON: %v\noutput: %q", err, out)
	}
	if entries == nil || len(entries) != 0 {
		t.Errorf("prune --dry-run --json (all present) = %q, want []", out)
	}
}

func TestIntegration_PruneJSONEmptyArrayNoRepos(t *testing.T) {
	testEnv(t)

	out := runSoko(t, "prune", "--dry-run", "--json")

	var entries []map[string]any
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		t.Fatalf("parsing JSON: %v\noutput: %q", err, out)
	}
	if entries == nil || len(entries) != 0 {
		t.Errorf("prune --dry-run --json (no repos) = %q, want []", out)
	}
}

func TestIntegration_PruneJSONRequiresForceOrDryRun(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "json-repo")
	initRepo(t, dir)
	runSokoInit(t, dir)

	var stdout bytes.Buffer
	cmd := cli.NewRootCmd("test")
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"prune", "--json"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("prune --json without --force/--dry-run should return an error")
	}
	if err.Error() != "--json requires --force or --dry-run" {
		t.Errorf("error = %q, want '--json requires --force or --dry-run'", err.Error())
	}
	// No prompt text or partial JSON may leak to machine consumers.
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want empty", stdout.String())
	}
}

func TestIntegration_PruneConfirmAbort(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "abort-repo")
	initRepo(t, dir)
	runSokoInit(t, dir)

	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("removing dir: %v", err)
	}

	var stdout, stderr bytes.Buffer
	cmd := cli.NewRootCmd("test")
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetIn(strings.NewReader("n\n"))
	cmd.SetArgs([]string{"prune"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("prune: %v", err)
	}

	if !strings.Contains(stderr.String(), "aborted") {
		t.Errorf("stderr = %q, want 'aborted'", stderr.String())
	}
	// Aborting must leave the config untouched.
	listOut := runSoko(t, "list")
	if !strings.Contains(listOut, "abort-repo") {
		t.Errorf("list after abort = %q, want 'abort-repo' retained", listOut)
	}
}

func TestIntegration_PruneConfirmYes(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "confirm-repo")
	initRepo(t, dir)
	runSokoInit(t, dir)

	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("removing dir: %v", err)
	}

	var stdout, stderr bytes.Buffer
	cmd := cli.NewRootCmd("test")
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetIn(strings.NewReader("y\n"))
	cmd.SetArgs([]string{"prune"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("prune: %v", err)
	}

	if !strings.Contains(stdout.String(), "pruned 1 missing repo") {
		t.Errorf("stdout = %q, want 'pruned 1 missing repo'", stdout.String())
	}
	listOut := runSoko(t, "list")
	if !strings.Contains(listOut, "no repos registered") {
		t.Errorf("list after confirm = %q, want 'no repos registered'", listOut)
	}
}

func TestIntegration_PruneConfirmEOF(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "eof-repo")
	initRepo(t, dir)
	runSokoInit(t, dir)

	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("removing dir: %v", err)
	}

	var stdout, stderr bytes.Buffer
	cmd := cli.NewRootCmd("test")
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetIn(strings.NewReader("")) // EOF before any answer
	cmd.SetArgs([]string{"prune"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("prune: %v", err)
	}

	// EOF is a silent no-op; the config must be untouched.
	listOut := runSoko(t, "list")
	if !strings.Contains(listOut, "eof-repo") {
		t.Errorf("list after EOF = %q, want 'eof-repo' retained", listOut)
	}
}

func TestIntegration_StatusWarnsMissingRepo(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "vanished-repo")
	initRepo(t, dir)
	runSokoInit(t, dir)

	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("removing dir: %v", err)
	}

	out := runSoko(t, "status")
	if !strings.Contains(out, "1 repo no longer exists") {
		t.Errorf("status with missing repo = %q, want '1 repo no longer exists'", out)
	}
	if !strings.Contains(out, "soko prune") {
		t.Errorf("status with missing repo = %q, want 'soko prune' hint", out)
	}
}

func TestIntegration_StatusWarnsMissingReposPlural(t *testing.T) {
	testEnv(t)
	base := t.TempDir()
	dirs := []string{filepath.Join(base, "gone-a"), filepath.Join(base, "gone-b")}
	// Register both before deleting any — runSokoInit calls os.Getwd, which
	// fails on Linux if the cwd was removed by an earlier loop iteration.
	for _, dir := range dirs {
		initRepo(t, dir)
		runSokoInit(t, dir)
	}
	for _, dir := range dirs {
		if err := os.RemoveAll(dir); err != nil {
			t.Fatalf("removing dir: %v", err)
		}
	}

	out := runSoko(t, "status")
	if !strings.Contains(out, "2 repos no longer exist") {
		t.Errorf("status = %q, want '2 repos no longer exist'", out)
	}
}

func TestIntegration_StatusNoMissingNoHint(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "alive-repo")
	initRepo(t, dir)
	runSokoInit(t, dir)

	out := runSoko(t, "status")
	if strings.Contains(out, "soko prune") {
		t.Errorf("status (no missing) = %q, should not nudge 'soko prune'", out)
	}
}

func TestIntegration_ListWarnsMissingRepo(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "vanished-list-repo")
	initRepo(t, dir)
	runSokoInit(t, dir)

	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("removing dir: %v", err)
	}

	out := runSoko(t, "list")
	if !strings.Contains(out, "1 repo no longer exists") {
		t.Errorf("list with missing repo = %q, want '1 repo no longer exists'", out)
	}
	if !strings.Contains(out, "soko prune") {
		t.Errorf("list with missing repo = %q, want 'soko prune' hint", out)
	}
}

func TestIntegration_ListNoMissingNoHint(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "alive-list-repo")
	initRepo(t, dir)
	runSokoInit(t, dir)

	out := runSoko(t, "list")
	if strings.Contains(out, "soko prune") {
		t.Errorf("list (no missing) = %q, should not nudge 'soko prune'", out)
	}
}
