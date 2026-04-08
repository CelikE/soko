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

func readNavFile(t *testing.T) string {
	t.Helper()
	xdg := os.Getenv("XDG_CONFIG_HOME")
	navPath := filepath.Join(xdg, "soko", ".nav")
	data, err := os.ReadFile(navPath)
	if err != nil {
		t.Fatalf("reading nav file: %v", err)
	}
	// Clean up after reading.
	_ = os.Remove(navPath)
	return string(data)
}

func TestIntegration_CdExactMatch(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "my-repo")
	initRepo(t, dir)
	runSokoInit(t, dir)

	runSoko(t, "cd", "my-repo")
	got := readNavFile(t)
	wantReal, _ := filepath.EvalSymlinks(dir)
	if got != dir && got != wantReal {
		t.Errorf("cd nav file = %q, want %q", got, dir)
	}
}

func TestIntegration_CdPrefixMatch(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "auth-service")
	initRepo(t, dir)
	runSokoInit(t, dir)

	runSoko(t, "cd", "auth")
	got := readNavFile(t)
	wantReal, _ := filepath.EvalSymlinks(dir)
	if got != dir && got != wantReal {
		t.Errorf("cd prefix nav file = %q, want %q", got, dir)
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

	// With only one repo and non-interactive stdin, it writes to nav file.
	runSoko(t, "go")
	got := readNavFile(t)
	wantReal, _ := filepath.EvalSymlinks(dir)
	if got != dir && got != wantReal {
		t.Errorf("go single repo nav = %q, want %q", got, dir)
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

func TestIntegration_ErrorMessageShown(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "err-repo")
	initRepo(t, dir)
	runSokoInit(t, dir)

	var stdout, stderr bytes.Buffer
	cmd := cli.NewRootCmd("test")
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"cd", "nonexistent"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error")
	}

	// Error message comes from the returned error (displayed by main.go).
	if !strings.Contains(err.Error(), "no repo matching") {
		t.Errorf("error = %q, want 'no repo matching'", err.Error())
	}
}

func TestIntegration_ListShowsTags(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "tagged-list-repo")
	initRepo(t, dir)
	runSokoInit(t, dir)
	runSoko(t, "tag", "add", "-r", "tagged-list-repo", "backend")

	out := runSoko(t, "list")
	if !strings.Contains(out, "TAGS") {
		t.Errorf("list = %q, want TAGS header", out)
	}
	if !strings.Contains(out, "backend") {
		t.Errorf("list = %q, want 'backend' tag", out)
	}
}

func TestIntegration_ListJSONIncludesTags(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "json-tag-repo")
	initRepo(t, dir)
	runSokoInit(t, dir)
	runSoko(t, "tag", "add", "-r", "json-tag-repo", "infra")

	out := runSoko(t, "list", "--json")
	if !strings.Contains(out, `"tags"`) {
		t.Errorf("list --json = %q, want 'tags' field", out)
	}
	if !strings.Contains(out, `"infra"`) {
		t.Errorf("list --json = %q, want 'infra' in tags", out)
	}
}

func TestIntegration_ExecShowsCommand(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "cmd-repo")
	initRepo(t, dir)
	runSokoInit(t, dir)

	out := runSoko(t, "exec", "--", "echo", "hi")
	if !strings.Contains(out, "running: echo hi") {
		t.Errorf("exec = %q, want 'running: echo hi'", out)
	}
}

func TestIntegration_ConfigPath(t *testing.T) {
	testEnv(t)
	out := runSoko(t, "config", "path")
	if !strings.Contains(out, "soko/config.yaml") {
		t.Errorf("config path = %q, want 'soko/config.yaml'", out)
	}
}

func TestIntegration_ShellInitFish(t *testing.T) {
	out := runSoko(t, "shell-init", "--fish")
	if !strings.Contains(out, "fish_postexec") {
		t.Errorf("shell-init --fish = %q, want 'fish_postexec'", out)
	}
}

func TestIntegration_ConfigSetAndGet(t *testing.T) {
	testEnv(t)

	out := runSoko(t, "config", "get", "git_path")
	if !strings.Contains(out, "git (default)") {
		t.Errorf("config get default = %q, want 'git (default)'", out)
	}

	// Create a temporary executable to use as a custom git path.
	tmpDir := t.TempDir()
	customGit := filepath.Join(tmpDir, "custom-git")
	if err := os.WriteFile(customGit, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("creating custom git: %v", err)
	}

	out = runSoko(t, "config", "set", "git_path", customGit)
	if !strings.Contains(out, "git_path = "+customGit) {
		t.Errorf("config set = %q, want 'git_path = %s'", out, customGit)
	}

	out = runSoko(t, "config", "get", "git_path")
	if !strings.Contains(out, customGit) {
		t.Errorf("config get after set = %q, want '%s'", out, customGit)
	}
}

func TestIntegration_ConfigSetUnknownKey(t *testing.T) {
	testEnv(t)

	var stdout bytes.Buffer
	cmd := cli.NewRootCmd("test")
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"config", "set", "unknown", "value"})

	err := cmd.Execute()
	if err == nil {
		t.Error("config set unknown key should return an error")
	}
}

func TestIntegration_ScanDiscoversRepos(t *testing.T) {
	testEnv(t)
	base := t.TempDir()

	for _, name := range []string{"repo-a", "repo-b", "repo-c"} {
		initRepo(t, filepath.Join(base, name))
	}

	out := runSoko(t, "scan", base)
	if !strings.Contains(out, "repo-a") {
		t.Errorf("scan = %q, want 'repo-a'", out)
	}
	if !strings.Contains(out, "repo-b") {
		t.Errorf("scan = %q, want 'repo-b'", out)
	}
	if !strings.Contains(out, "3 initialized") {
		t.Errorf("scan summary = %q, want '3 initialized'", out)
	}
	if !strings.Contains(out, "NAME") || !strings.Contains(out, "SOKO") {
		t.Errorf("scan = %q, want table headers NAME and SOKO", out)
	}

	// Verify they're actually in the config.
	listOut := runSoko(t, "list")
	if !strings.Contains(listOut, "repo-a") || !strings.Contains(listOut, "repo-b") || !strings.Contains(listOut, "repo-c") {
		t.Errorf("list after scan = %q, want all 3 repos", listOut)
	}
}

func TestIntegration_ScanSkipsDuplicates(t *testing.T) {
	testEnv(t)
	base := t.TempDir()
	dir := filepath.Join(base, "existing-repo")
	initRepo(t, dir)
	runSokoInit(t, dir)

	out := runSoko(t, "scan", base)
	if !strings.Contains(out, "already in soko") {
		t.Errorf("scan duplicate = %q, want 'already in soko'", out)
	}
	if !strings.Contains(out, "0 initialized") {
		t.Errorf("scan summary = %q, want '0 initialized'", out)
	}
}

func TestIntegration_ScanWithTags(t *testing.T) {
	testEnv(t)
	base := t.TempDir()
	initRepo(t, filepath.Join(base, "tagged-repo"))

	runSoko(t, "scan", base, "--tag", "work")

	out := runSoko(t, "tag", "list")
	if !strings.Contains(out, "work") {
		t.Errorf("tag list after scan = %q, want 'work'", out)
	}
}

func TestIntegration_ScanDryRun(t *testing.T) {
	testEnv(t)
	base := t.TempDir()
	initRepo(t, filepath.Join(base, "dry-repo"))

	out := runSoko(t, "scan", base, "--dry-run")
	if !strings.Contains(out, "not initialized") {
		t.Errorf("scan dry-run = %q, want 'not initialized'", out)
	}
	if !strings.Contains(out, "dry-run") {
		t.Errorf("scan dry-run header = %q, want 'dry-run'", out)
	}

	// Should NOT be in config.
	listOut := runSoko(t, "list")
	if !strings.Contains(listOut, "no repos registered") {
		t.Errorf("list after dry-run = %q, want 'no repos registered'", listOut)
	}
}

func TestIntegration_ScanEmptyDirectory(t *testing.T) {
	testEnv(t)
	base := t.TempDir()

	out := runSoko(t, "scan", base)
	if !strings.Contains(out, "no git repos found") {
		t.Errorf("scan empty = %q, want 'no git repos found'", out)
	}
}

func TestIntegration_ScanNonExistent(t *testing.T) {
	testEnv(t)

	var stdout bytes.Buffer
	cmd := cli.NewRootCmd("test")
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"scan", "/nonexistent/path"})

	err := cmd.Execute()
	if err == nil {
		t.Error("scan nonexistent should return an error")
	}
}

func TestIntegration_ScanJSON(t *testing.T) {
	testEnv(t)
	base := t.TempDir()
	initRepo(t, filepath.Join(base, "json-scan-repo"))

	out := runSoko(t, "scan", base, "--json")

	var entries []map[string]any
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		t.Fatalf("parsing JSON: %v\noutput: %s", err, out)
	}
	if len(entries) != 1 {
		t.Fatalf("JSON entries = %d, want 1", len(entries))
	}
	if entries[0]["name"] != "json-scan-repo" {
		t.Errorf("JSON name = %v, want 'json-scan-repo'", entries[0]["name"])
	}
}

func TestIntegration_ShellInitPwsh(t *testing.T) {
	out := runSoko(t, "shell-init", "--pwsh")
	if !strings.Contains(out, "Invoke-Expression") {
		t.Errorf("shell-init --pwsh = %q, want 'Invoke-Expression'", out)
	}
	if !strings.Contains(out, "__soko_nav_hook") {
		t.Errorf("shell-init --pwsh = %q, want '__soko_nav_hook'", out)
	}
}

func TestIntegration_StatusShowsCommitMessage(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "msg-repo")
	initRepo(t, dir)
	runSokoInit(t, dir)

	out := runSoko(t, "status")
	if !strings.Contains(out, "initial commit") {
		t.Errorf("status = %q, want 'initial commit' message", out)
	}
}

func TestIntegration_DiffShowsFiles(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "diff-repo")
	initRepo(t, dir)
	runSokoInit(t, dir)

	// Make it dirty.
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("writing file: %v", err)
	}

	out := runSoko(t, "diff")
	if !strings.Contains(out, "diff-repo") {
		t.Errorf("diff = %q, want 'diff-repo'", out)
	}
	if !strings.Contains(out, "new.txt") {
		t.Errorf("diff = %q, want 'new.txt'", out)
	}
	if !strings.Contains(out, "1 files changed") {
		t.Errorf("diff summary = %q, want '1 files changed'", out)
	}
}

func TestIntegration_DiffCleanRepos(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "clean-diff-repo")
	initRepo(t, dir)
	runSokoInit(t, dir)

	out := runSoko(t, "diff")
	if !strings.Contains(out, "all repos clean") {
		t.Errorf("diff clean = %q, want 'all repos clean'", out)
	}
}

func TestIntegration_DiffJSON(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "json-diff-repo")
	initRepo(t, dir)
	runSokoInit(t, dir)

	if err := os.WriteFile(filepath.Join(dir, "changed.go"), []byte("x"), 0o644); err != nil {
		t.Fatalf("writing file: %v", err)
	}

	out := runSoko(t, "diff", "--json")

	var entries []map[string]any
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		t.Fatalf("parsing JSON: %v\noutput: %s", err, out)
	}
	if len(entries) != 1 {
		t.Fatalf("JSON entries = %d, want 1", len(entries))
	}
	if entries[0]["name"] != "json-diff-repo" {
		t.Errorf("JSON name = %v, want 'json-diff-repo'", entries[0]["name"])
	}
}

func TestIntegration_StatusGroup(t *testing.T) {
	testEnv(t)
	base := t.TempDir()

	for _, name := range []string{"back-svc", "front-app", "plain-repo"} {
		dir := filepath.Join(base, name)
		initRepo(t, dir)
		runSokoInit(t, dir)
	}

	runSoko(t, "tag", "add", "-r", "back-svc", "backend")
	runSoko(t, "tag", "add", "-r", "front-app", "frontend")

	out := runSoko(t, "status", "--group")

	if !strings.Contains(out, "backend") {
		t.Errorf("status --group = %q, want 'backend' group", out)
	}
	if !strings.Contains(out, "frontend") {
		t.Errorf("status --group = %q, want 'frontend' group", out)
	}
	if !strings.Contains(out, "untagged") {
		t.Errorf("status --group = %q, want 'untagged' group", out)
	}
	if !strings.Contains(out, "back-svc") {
		t.Errorf("status --group = %q, want 'back-svc'", out)
	}
}

func TestIntegration_StatusByRepoName(t *testing.T) {
	testEnv(t)
	dir1 := filepath.Join(t.TempDir(), "alpha")
	dir2 := filepath.Join(t.TempDir(), "bravo")
	initRepo(t, dir1)
	initRepo(t, dir2)
	runSokoInit(t, dir1)
	runSokoInit(t, dir2)

	// Exact match: only alpha.
	out := runSoko(t, "status", "alpha")
	if !strings.Contains(out, "alpha") {
		t.Errorf("status alpha = %q, want 'alpha'", out)
	}
	if strings.Contains(out, "bravo") {
		t.Errorf("status alpha = %q, should not contain 'bravo'", out)
	}
	if !strings.Contains(out, "1 repo") {
		t.Errorf("status alpha = %q, want '1 repo' in summary", out)
	}
}

func TestIntegration_StatusByRepoPrefix(t *testing.T) {
	testEnv(t)
	dir1 := filepath.Join(t.TempDir(), "api-auth")
	dir2 := filepath.Join(t.TempDir(), "api-gateway")
	dir3 := filepath.Join(t.TempDir(), "frontend")
	initRepo(t, dir1)
	initRepo(t, dir2)
	initRepo(t, dir3)
	runSokoInit(t, dir1)
	runSokoInit(t, dir2)
	runSokoInit(t, dir3)

	// Prefix match: api- matches two repos.
	out := runSoko(t, "status", "api-")
	if !strings.Contains(out, "api-auth") {
		t.Errorf("status api- = %q, want 'api-auth'", out)
	}
	if !strings.Contains(out, "api-gateway") {
		t.Errorf("status api- = %q, want 'api-gateway'", out)
	}
	if strings.Contains(out, "frontend") {
		t.Errorf("status api- = %q, should not contain 'frontend'", out)
	}
}

func TestIntegration_StatusMultipleRepoArgs(t *testing.T) {
	testEnv(t)
	dir1 := filepath.Join(t.TempDir(), "alpha")
	dir2 := filepath.Join(t.TempDir(), "bravo")
	dir3 := filepath.Join(t.TempDir(), "charlie")
	initRepo(t, dir1)
	initRepo(t, dir2)
	initRepo(t, dir3)
	runSokoInit(t, dir1)
	runSokoInit(t, dir2)
	runSokoInit(t, dir3)

	// Multiple args: alpha and charlie.
	out := runSoko(t, "status", "alpha", "charlie")
	if !strings.Contains(out, "alpha") {
		t.Errorf("status alpha charlie = %q, want 'alpha'", out)
	}
	if !strings.Contains(out, "charlie") {
		t.Errorf("status alpha charlie = %q, want 'charlie'", out)
	}
	if strings.Contains(out, "bravo") {
		t.Errorf("status alpha charlie = %q, should not contain 'bravo'", out)
	}
}
