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

// testEnv sets up a temp config dir and returns the XDG path and a cleanup
// function. Tests should call t.Setenv to point XDG_CONFIG_HOME at the
// returned directory.
func testEnv(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	return dir
}

// initRepo creates a git repo at the given path with an initial commit.
func initRepo(t *testing.T, path string) {
	t.Helper()

	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("creating repo dir: %v", err)
	}

	ctx := context.Background()
	cmds := [][]string{
		{"git", "init"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
	}
	for _, args := range cmds {
		cmd := exec.CommandContext(ctx, args[0], args[1:]...)
		cmd.Dir = path
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("running %v: %v\n%s", args, err, out)
		}
	}

	readme := filepath.Join(path, "README.md")
	if err := os.WriteFile(readme, []byte("# test\n"), 0o644); err != nil {
		t.Fatalf("writing README: %v", err)
	}

	add := exec.CommandContext(ctx, "git", "add", ".")
	add.Dir = path
	if out, err := add.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}

	commit := exec.CommandContext(ctx, "git", "commit", "-m", "initial commit")
	commit.Dir = path
	commit.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null")
	if out, err := commit.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v\n%s", err, out)
	}
}

// runSoko executes a soko command and returns stdout.
func runSoko(t *testing.T, args ...string) string {
	t.Helper()

	var stdout bytes.Buffer
	cmd := cli.NewRootCmd("test")
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs(args)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("soko %s: %v", strings.Join(args, " "), err)
	}

	return stdout.String()
}

// runSokoInDir executes soko init in a specific directory by changing the
// working directory temporarily.
func runSokoInit(t *testing.T, dir string) string {
	t.Helper()

	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getting cwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir to %s: %v", dir, err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(orig)
	})

	return runSoko(t, "init")
}

func TestIntegration_InitAndList(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "my-repo")
	initRepo(t, dir)

	out := runSokoInit(t, dir)
	if !strings.Contains(out, "registered") {
		t.Errorf("init output = %q, want 'registered'", out)
	}

	out = runSoko(t, "list")
	if !strings.Contains(out, "my-repo") {
		t.Errorf("list output = %q, want to contain 'my-repo'", out)
	}
}

func TestIntegration_InitDuplicate(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "my-repo")
	initRepo(t, dir)

	runSokoInit(t, dir)
	out := runSokoInit(t, dir)
	if !strings.Contains(out, "already registered") {
		t.Errorf("init duplicate output = %q, want 'already registered'", out)
	}
}

func TestIntegration_RemoveByName(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "my-repo")
	initRepo(t, dir)
	runSokoInit(t, dir)

	out := runSoko(t, "remove", "my-repo")
	if !strings.Contains(out, "removed") {
		t.Errorf("remove output = %q, want 'removed'", out)
	}

	out = runSoko(t, "list")
	if !strings.Contains(out, "no repos registered") {
		t.Errorf("list after remove = %q, want 'no repos registered'", out)
	}
}

func TestIntegration_StatusCleanRepo(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "clean-repo")
	initRepo(t, dir)
	runSokoInit(t, dir)

	out := runSoko(t, "status")
	if !strings.Contains(out, "clean") {
		t.Errorf("status output = %q, want to contain 'clean'", out)
	}
	if !strings.Contains(out, "0 dirty") {
		t.Errorf("status output = %q, want to contain '0 dirty'", out)
	}
}

func TestIntegration_StatusDirtyRepo(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "dirty-repo")
	initRepo(t, dir)
	runSokoInit(t, dir)

	// Make it dirty.
	if err := os.WriteFile(filepath.Join(dir, "new-file.txt"), []byte("change"), 0o644); err != nil {
		t.Fatalf("writing file: %v", err)
	}

	out := runSoko(t, "status")
	if !strings.Contains(out, "1U") {
		t.Errorf("status output = %q, want to contain '1U'", out)
	}
	if !strings.Contains(out, "1 dirty") {
		t.Errorf("status output = %q, want to contain '1 dirty'", out)
	}
}

func TestIntegration_StatusFilterDirty(t *testing.T) {
	testEnv(t)
	base := t.TempDir()

	// Create clean and dirty repos.
	cleanDir := filepath.Join(base, "clean-repo")
	dirtyDir := filepath.Join(base, "dirty-repo")
	initRepo(t, cleanDir)
	initRepo(t, dirtyDir)

	runSokoInit(t, cleanDir)
	runSokoInit(t, dirtyDir)

	// Dirty one repo.
	if err := os.WriteFile(filepath.Join(dirtyDir, "change.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("writing file: %v", err)
	}

	out := runSoko(t, "status", "--dirty")
	if !strings.Contains(out, "dirty-repo") {
		t.Errorf("--dirty output = %q, want to contain 'dirty-repo'", out)
	}
	if strings.Contains(out, "clean-repo") {
		t.Errorf("--dirty output = %q, should not contain 'clean-repo'", out)
	}
}

func TestIntegration_StatusFilterClean(t *testing.T) {
	testEnv(t)
	base := t.TempDir()

	cleanDir := filepath.Join(base, "clean-repo")
	dirtyDir := filepath.Join(base, "dirty-repo")
	initRepo(t, cleanDir)
	initRepo(t, dirtyDir)

	runSokoInit(t, cleanDir)
	runSokoInit(t, dirtyDir)

	if err := os.WriteFile(filepath.Join(dirtyDir, "change.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("writing file: %v", err)
	}

	out := runSoko(t, "status", "--clean")
	if !strings.Contains(out, "clean-repo") {
		t.Errorf("--clean output = %q, want to contain 'clean-repo'", out)
	}
	if strings.Contains(out, "dirty-repo") {
		t.Errorf("--clean output = %q, should not contain 'dirty-repo'", out)
	}
}

func TestIntegration_StatusFilterNoMatch(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "clean-repo")
	initRepo(t, dir)
	runSokoInit(t, dir)

	// All repos are clean, --dirty should match nothing.
	out := runSoko(t, "status", "--dirty")
	if !strings.Contains(out, "no repos match the filter") {
		t.Errorf("--dirty on clean repos = %q, want 'no repos match the filter'", out)
	}
}

func TestIntegration_StatusJSON(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "json-repo")
	initRepo(t, dir)
	runSokoInit(t, dir)

	out := runSoko(t, "status", "--json")

	var entries []map[string]interface{}
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		t.Fatalf("parsing JSON: %v\noutput: %s", err, out)
	}
	if len(entries) != 1 {
		t.Fatalf("JSON entries = %d, want 1", len(entries))
	}
	if entries[0]["name"] != "json-repo" {
		t.Errorf("JSON name = %v, want 'json-repo'", entries[0]["name"])
	}
}

func TestIntegration_StatusFilterDirtyJSON(t *testing.T) {
	testEnv(t)
	base := t.TempDir()

	cleanDir := filepath.Join(base, "clean-repo")
	dirtyDir := filepath.Join(base, "dirty-repo")
	initRepo(t, cleanDir)
	initRepo(t, dirtyDir)

	runSokoInit(t, cleanDir)
	runSokoInit(t, dirtyDir)

	if err := os.WriteFile(filepath.Join(dirtyDir, "change.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("writing file: %v", err)
	}

	out := runSoko(t, "status", "--dirty", "--json")

	var entries []map[string]interface{}
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		t.Fatalf("parsing JSON: %v\noutput: %s", err, out)
	}
	if len(entries) != 1 {
		t.Fatalf("JSON entries = %d, want 1", len(entries))
	}
	if entries[0]["name"] != "dirty-repo" {
		t.Errorf("JSON name = %v, want 'dirty-repo'", entries[0]["name"])
	}
}

func TestIntegration_Fetch(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "fetch-repo")
	initRepo(t, dir)
	runSokoInit(t, dir)

	out := runSoko(t, "fetch")
	if !strings.Contains(out, "fetched") {
		t.Errorf("fetch output = %q, want to contain 'fetched'", out)
	}
}

func TestIntegration_FetchMissingPath(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "temp-repo")
	initRepo(t, dir)
	runSokoInit(t, dir)

	// Remove the repo directory.
	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("removing dir: %v", err)
	}

	// Fetch should not panic — it should show failure and return an error.
	var stdout bytes.Buffer
	cmd := cli.NewRootCmd("test")
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"fetch"})

	err := cmd.Execute()
	if err == nil {
		t.Error("fetch with missing path should return an error")
	}

	out := stdout.String()
	if !strings.Contains(out, "path not found") {
		t.Errorf("fetch missing path output = %q, want 'path not found'", out)
	}
}

func TestIntegration_StatusMissingPath(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "gone-repo")
	initRepo(t, dir)
	runSokoInit(t, dir)

	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("removing dir: %v", err)
	}

	out := runSoko(t, "status")
	if !strings.Contains(out, "not found") {
		t.Errorf("status missing path = %q, want to contain 'not found'", out)
	}
}

func TestIntegration_ListJSON(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "list-repo")
	initRepo(t, dir)
	runSokoInit(t, dir)

	out := runSoko(t, "list", "--json")

	var entries []map[string]interface{}
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		t.Fatalf("parsing JSON: %v\noutput: %s", err, out)
	}
	if len(entries) != 1 {
		t.Fatalf("JSON entries = %d, want 1", len(entries))
	}
	if entries[0]["name"] != "list-repo" {
		t.Errorf("JSON name = %v, want 'list-repo'", entries[0]["name"])
	}
}

func TestIntegration_Version(t *testing.T) {
	out := runSoko(t, "version")
	if !strings.Contains(out, "soko test") {
		t.Errorf("version output = %q, want 'soko test'", out)
	}
}

func TestIntegration_RemoveAll(t *testing.T) {
	testEnv(t)
	base := t.TempDir()

	for _, name := range []string{"repo-a", "repo-b"} {
		dir := filepath.Join(base, name)
		initRepo(t, dir)
		runSokoInit(t, dir)
	}

	out := runSoko(t, "remove", "--all", "--force")
	if !strings.Contains(out, "removed all 2 repos") {
		t.Errorf("remove --all output = %q, want 'removed all 2 repos'", out)
	}
}

func TestIntegration_CdExactMatch(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "my-repo")
	initRepo(t, dir)
	runSokoInit(t, dir)

	out := runSoko(t, "cd", "my-repo")
	got := strings.TrimSpace(out)
	// Resolve symlinks for macOS /var -> /private/var.
	wantReal, _ := filepath.EvalSymlinks(dir)
	if got != dir && got != wantReal {
		t.Errorf("cd output = %q, want %q", got, dir)
	}
}

func TestIntegration_CdPrefixMatch(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "auth-service")
	initRepo(t, dir)
	runSokoInit(t, dir)

	out := runSoko(t, "cd", "auth")
	got := strings.TrimSpace(out)
	wantReal, _ := filepath.EvalSymlinks(dir)
	if got != dir && got != wantReal {
		t.Errorf("cd prefix output = %q, want %q", got, dir)
	}
}

func TestIntegration_CdNoMatch(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "my-repo")
	initRepo(t, dir)
	runSokoInit(t, dir)

	var stdout bytes.Buffer
	cmd := cli.NewRootCmd("test")
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"cd", "nonexistent"})

	err := cmd.Execute()
	if err == nil {
		t.Error("cd with no match should return an error")
	}
}

func TestIntegration_CdMultipleMatches(t *testing.T) {
	testEnv(t)
	base := t.TempDir()

	for _, name := range []string{"auth-service", "auth-worker"} {
		dir := filepath.Join(base, name)
		initRepo(t, dir)
		runSokoInit(t, dir)
	}

	var stdout bytes.Buffer
	cmd := cli.NewRootCmd("test")
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"cd", "auth"})

	err := cmd.Execute()
	if err == nil {
		t.Error("cd with multiple matches should return an error")
	}
}

func TestIntegration_CdJSON(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "my-repo")
	initRepo(t, dir)
	runSokoInit(t, dir)

	out := runSoko(t, "cd", "--json", "my-repo")

	var entry map[string]string
	if err := json.Unmarshal([]byte(out), &entry); err != nil {
		t.Fatalf("parsing JSON: %v\noutput: %s", err, out)
	}
	if entry["name"] != "my-repo" {
		t.Errorf("JSON name = %q, want 'my-repo'", entry["name"])
	}
	wantReal, _ := filepath.EvalSymlinks(dir)
	if entry["path"] != dir && entry["path"] != wantReal {
		t.Errorf("JSON path = %q, want %q", entry["path"], dir)
	}
}

func TestIntegration_ExecEcho(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "exec-repo")
	initRepo(t, dir)
	runSokoInit(t, dir)

	out := runSoko(t, "exec", "--", "echo", "hello")
	if !strings.Contains(out, "hello") {
		t.Errorf("exec echo output = %q, want to contain 'hello'", out)
	}
	if !strings.Contains(out, "1 ok") {
		t.Errorf("exec summary = %q, want '1 ok'", out)
	}
}

func TestIntegration_ExecFailingCommand(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "fail-repo")
	initRepo(t, dir)
	runSokoInit(t, dir)

	var stdout bytes.Buffer
	cmd := cli.NewRootCmd("test")
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"exec", "--", "false"})

	err := cmd.Execute()
	if err == nil {
		t.Error("exec with failing command should return an error")
	}

	out := stdout.String()
	if !strings.Contains(out, "1 failed") {
		t.Errorf("exec fail summary = %q, want '1 failed'", out)
	}
}

func TestIntegration_ExecJSON(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "json-exec-repo")
	initRepo(t, dir)
	runSokoInit(t, dir)

	out := runSoko(t, "exec", "--json", "--", "echo", "world")

	var entries []map[string]any
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		t.Fatalf("parsing JSON: %v\noutput: %s", err, out)
	}
	if len(entries) != 1 {
		t.Fatalf("JSON entries = %d, want 1", len(entries))
	}
	if !strings.Contains(entries[0]["stdout"].(string), "world") {
		t.Errorf("JSON stdout = %v, want to contain 'world'", entries[0]["stdout"])
	}
}

func TestIntegration_ExecMissingPath(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "gone-exec-repo")
	initRepo(t, dir)
	runSokoInit(t, dir)

	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("removing dir: %v", err)
	}

	var stdout bytes.Buffer
	cmd := cli.NewRootCmd("test")
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"exec", "--", "echo", "hi"})

	err := cmd.Execute()
	if err == nil {
		t.Error("exec with missing path should return an error")
	}

	out := stdout.String()
	if !strings.Contains(out, "path not found") {
		t.Errorf("exec missing path output = %q, want 'path not found'", out)
	}
}

func TestIntegration_ExecSequential(t *testing.T) {
	testEnv(t)
	base := t.TempDir()

	for _, name := range []string{"seq-a", "seq-b"} {
		dir := filepath.Join(base, name)
		initRepo(t, dir)
		runSokoInit(t, dir)
	}

	out := runSoko(t, "exec", "--seq", "--", "echo", "ok")
	if !strings.Contains(out, "seq-a") && !strings.Contains(out, "seq-b") {
		t.Errorf("sequential exec = %q, want both repo names", out)
	}
	if !strings.Contains(out, "2 ok") {
		t.Errorf("sequential summary = %q, want '2 ok'", out)
	}
}

func TestIntegration_DocHealthy(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "healthy-repo")
	initRepo(t, dir)
	runSokoInit(t, dir)

	out := runSoko(t, "doc")
	if !strings.Contains(out, "0 errors") {
		t.Errorf("doc healthy = %q, want '0 errors'", out)
	}
	// Repo has no remote, so it gets a warning — but no errors.
	if !strings.Contains(out, "healthy-repo") {
		t.Errorf("doc healthy = %q, want to contain 'healthy-repo'", out)
	}
}

func TestIntegration_DocMissingPath(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "gone-repo")
	initRepo(t, dir)
	runSokoInit(t, dir)

	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("removing dir: %v", err)
	}

	out := runSoko(t, "doc")
	if !strings.Contains(out, "path does not exist") {
		t.Errorf("doc missing = %q, want 'path does not exist'", out)
	}
	if !strings.Contains(out, "soko remove") {
		t.Errorf("doc missing = %q, want suggestion 'soko remove'", out)
	}
}

func TestIntegration_DocFixMissingPath(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "fix-repo")
	initRepo(t, dir)
	runSokoInit(t, dir)

	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("removing dir: %v", err)
	}

	out := runSoko(t, "doc", "--fix")
	if !strings.Contains(out, "removed from config") {
		t.Errorf("doc --fix = %q, want 'removed from config'", out)
	}

	// Verify it was actually removed.
	listOut := runSoko(t, "list")
	if !strings.Contains(listOut, "no repos registered") {
		t.Errorf("list after fix = %q, want 'no repos registered'", listOut)
	}
}

func TestIntegration_DocJSON(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "json-doc-repo")
	initRepo(t, dir)
	runSokoInit(t, dir)

	out := runSoko(t, "doc", "--json")

	var entries []map[string]any
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		t.Fatalf("parsing JSON: %v\noutput: %s", err, out)
	}
	// Should have at least git check, config check, and repo check.
	if len(entries) < 3 {
		t.Fatalf("JSON entries = %d, want at least 3", len(entries))
	}
}

func TestIntegration_StatusFetch(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "fetch-status-repo")
	initRepo(t, dir)
	runSokoInit(t, dir)

	// --fetch should not crash and should still show status.
	out := runSoko(t, "status", "--fetch")
	if !strings.Contains(out, "fetch-status-repo") {
		t.Errorf("status --fetch output = %q, want to contain 'fetch-status-repo'", out)
	}
}

func TestIntegration_StatusFetchWithFilter(t *testing.T) {
	testEnv(t)
	base := t.TempDir()

	cleanDir := filepath.Join(base, "clean-repo")
	dirtyDir := filepath.Join(base, "dirty-repo")
	initRepo(t, cleanDir)
	initRepo(t, dirtyDir)
	runSokoInit(t, cleanDir)
	runSokoInit(t, dirtyDir)

	if err := os.WriteFile(filepath.Join(dirtyDir, "change.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("writing file: %v", err)
	}

	out := runSoko(t, "status", "--fetch", "--dirty")
	if !strings.Contains(out, "dirty-repo") {
		t.Errorf("status --fetch --dirty = %q, want to contain 'dirty-repo'", out)
	}
	if strings.Contains(out, "clean-repo") {
		t.Errorf("status --fetch --dirty = %q, should not contain 'clean-repo'", out)
	}
}

func TestIntegration_TagAddAndList(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "tagged-repo")
	initRepo(t, dir)
	runSokoInit(t, dir)

	out := runSoko(t, "tag", "add", "-r", "tagged-repo", "backend")
	if !strings.Contains(out, "tagged") {
		t.Errorf("tag add output = %q, want 'tagged'", out)
	}

	out = runSoko(t, "tag", "list")
	if !strings.Contains(out, "backend") {
		t.Errorf("tag list output = %q, want 'backend'", out)
	}
}

func TestIntegration_TagRemove(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "tag-rm-repo")
	initRepo(t, dir)
	runSokoInit(t, dir)

	runSoko(t, "tag", "add", "-r", "tag-rm-repo", "frontend")
	runSoko(t, "tag", "remove", "-r", "tag-rm-repo", "frontend")

	out := runSoko(t, "tag", "list")
	if !strings.Contains(out, "no tags in use") {
		t.Errorf("tag list after remove = %q, want 'no tags in use'", out)
	}
}

func TestIntegration_StatusFilterByTag(t *testing.T) {
	testEnv(t)
	base := t.TempDir()

	backDir := filepath.Join(base, "backend-svc")
	frontDir := filepath.Join(base, "frontend-app")
	initRepo(t, backDir)
	initRepo(t, frontDir)
	runSokoInit(t, backDir)
	runSokoInit(t, frontDir)

	runSoko(t, "tag", "add", "-r", "backend-svc", "backend")
	runSoko(t, "tag", "add", "-r", "frontend-app", "frontend")

	out := runSoko(t, "status", "--tag", "backend")
	if !strings.Contains(out, "backend-svc") {
		t.Errorf("status --tag backend = %q, want 'backend-svc'", out)
	}
	if strings.Contains(out, "frontend-app") {
		t.Errorf("status --tag backend = %q, should not contain 'frontend-app'", out)
	}
}

func TestIntegration_ListFilterByTag(t *testing.T) {
	testEnv(t)
	base := t.TempDir()

	aDir := filepath.Join(base, "svc-a")
	bDir := filepath.Join(base, "svc-b")
	initRepo(t, aDir)
	initRepo(t, bDir)
	runSokoInit(t, aDir)
	runSokoInit(t, bDir)

	runSoko(t, "tag", "add", "-r", "svc-a", "infra")

	out := runSoko(t, "list", "--tag", "infra")
	if !strings.Contains(out, "svc-a") {
		t.Errorf("list --tag infra = %q, want 'svc-a'", out)
	}
	if strings.Contains(out, "svc-b") {
		t.Errorf("list --tag infra = %q, should not contain 'svc-b'", out)
	}
}

func TestIntegration_InitWithTag(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "init-tag-repo")
	initRepo(t, dir)

	orig, _ := os.Getwd()
	_ = os.Chdir(dir)
	t.Cleanup(func() { _ = os.Chdir(orig) })

	runSoko(t, "init", "--tag", "backend", "--tag", "go")

	out := runSoko(t, "tag", "list")
	if !strings.Contains(out, "backend") || !strings.Contains(out, "go") {
		t.Errorf("tag list after init --tag = %q, want 'backend' and 'go'", out)
	}
}

func TestIntegration_ExecFilterByTag(t *testing.T) {
	testEnv(t)
	base := t.TempDir()

	aDir := filepath.Join(base, "exec-a")
	bDir := filepath.Join(base, "exec-b")
	initRepo(t, aDir)
	initRepo(t, bDir)
	runSokoInit(t, aDir)
	runSokoInit(t, bDir)

	runSoko(t, "tag", "add", "-r", "exec-a", "target")

	out := runSoko(t, "exec", "--tag", "target", "--", "echo", "hit")
	if !strings.Contains(out, "exec-a") {
		t.Errorf("exec --tag = %q, want 'exec-a'", out)
	}
	if strings.Contains(out, "exec-b") {
		t.Errorf("exec --tag = %q, should not contain 'exec-b'", out)
	}
	if !strings.Contains(out, "1 ok") {
		t.Errorf("exec --tag summary = %q, want '1 ok'", out)
	}
}

func TestIntegration_ListGroupTree(t *testing.T) {
	testEnv(t)
	base := t.TempDir()

	for _, name := range []string{"auth-svc", "api-svc", "web-app", "scripts"} {
		dir := filepath.Join(base, name)
		initRepo(t, dir)
		runSokoInit(t, dir)
	}

	runSoko(t, "tag", "add", "-r", "auth-svc", "backend")
	runSoko(t, "tag", "add", "-r", "api-svc", "backend")
	runSoko(t, "tag", "add", "-r", "web-app", "frontend")

	out := runSoko(t, "list", "--group")

	if !strings.Contains(out, "backend") {
		t.Errorf("list --group = %q, want 'backend' group", out)
	}
	if !strings.Contains(out, "frontend") {
		t.Errorf("list --group = %q, want 'frontend' group", out)
	}
	if !strings.Contains(out, "untagged") {
		t.Errorf("list --group = %q, want 'untagged' group", out)
	}
	for _, name := range []string{"auth-svc", "api-svc", "web-app", "scripts"} {
		if !strings.Contains(out, name) {
			t.Errorf("list --group = %q, want to contain %q", out, name)
		}
	}
}

func TestIntegration_ListGroupNoTags(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "plain-repo")
	initRepo(t, dir)
	runSokoInit(t, dir)

	out := runSoko(t, "list", "--group")
	if !strings.Contains(out, "untagged") {
		t.Errorf("list --group no tags = %q, want 'untagged'", out)
	}
	if !strings.Contains(out, "plain-repo") {
		t.Errorf("list --group no tags = %q, want 'plain-repo'", out)
	}
}

func TestIntegration_TagShorthand(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "shorthand-repo")
	initRepo(t, dir)
	runSokoInit(t, dir)

	orig, _ := os.Getwd()
	_ = os.Chdir(dir)
	t.Cleanup(func() { _ = os.Chdir(orig) })

	out := runSoko(t, "tag", "backend")
	if !strings.Contains(out, "tagged") {
		t.Errorf("tag shorthand = %q, want 'tagged'", out)
	}

	out = runSoko(t, "tag", "list")
	if !strings.Contains(out, "backend") {
		t.Errorf("tag list after shorthand = %q, want 'backend'", out)
	}
}

func TestIntegration_TagAddCurrentDir(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "cwd-repo")
	initRepo(t, dir)
	runSokoInit(t, dir)

	orig, _ := os.Getwd()
	_ = os.Chdir(dir)
	t.Cleanup(func() { _ = os.Chdir(orig) })

	out := runSoko(t, "tag", "add", "frontend", "go")
	if !strings.Contains(out, "tagged") {
		t.Errorf("tag add cwd = %q, want 'tagged'", out)
	}

	out = runSoko(t, "tag", "list")
	if !strings.Contains(out, "frontend") || !strings.Contains(out, "go") {
		t.Errorf("tag list = %q, want 'frontend' and 'go'", out)
	}
}

func TestIntegration_TagRemoveCurrentDir(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "rm-cwd-repo")
	initRepo(t, dir)
	runSokoInit(t, dir)

	orig, _ := os.Getwd()
	_ = os.Chdir(dir)
	t.Cleanup(func() { _ = os.Chdir(orig) })

	runSoko(t, "tag", "backend", "go")

	out := runSoko(t, "tag", "remove", "go")
	if !strings.Contains(out, "removed tag") {
		t.Errorf("tag remove cwd = %q, want 'removed tag'", out)
	}

	out = runSoko(t, "tag", "list")
	if !strings.Contains(out, "backend") {
		t.Errorf("tag list = %q, want 'backend'", out)
	}
	if strings.Contains(out, "go (") {
		t.Errorf("tag list = %q, should not contain 'go'", out)
	}
}

func TestIntegration_GoSingleRepo(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "go-repo")
	initRepo(t, dir)
	runSokoInit(t, dir)

	// With only one repo and non-interactive stdin, it should print the path directly.
	out := runSoko(t, "go")
	got := strings.TrimSpace(out)
	wantReal, _ := filepath.EvalSymlinks(dir)
	if got != dir && got != wantReal {
		t.Errorf("go single repo = %q, want %q", got, dir)
	}
}

func TestIntegration_GoNoRepos(t *testing.T) {
	testEnv(t)

	var stdout bytes.Buffer
	cmd := cli.NewRootCmd("test")
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"go"})

	err := cmd.Execute()
	if err == nil {
		t.Error("go with no repos should return an error")
	}
}
