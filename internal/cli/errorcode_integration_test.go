package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/CelikE/soko/internal/cli"
)

// errEntry is the subset of any command's per-repo JSON we assert on. Using a
// pointer for ErrorCode lets a test distinguish "absent" (nil) from "" so the
// omitempty contract can be verified.
type errEntry struct {
	Name      string  `json:"name"`
	Status    string  `json:"status"`
	Error     string  `json:"error"`
	ErrorCode *string `json:"error_code"`
	ExitCode  *int    `json:"exit_code"`
}

func entryByName(entries []errEntry, name string) (errEntry, bool) {
	for _, e := range entries {
		if e.Name == name {
			return e, true
		}
	}
	return errEntry{}, false
}

// runSokoErr runs a soko command capturing stdout and returns it with the
// command error (for paths that legitimately exit non-zero).
func runSokoErr(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var stdout bytes.Buffer
	cmd := cli.NewRootCmd("test")
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs(args)
	err := cmd.Execute()
	return stdout.String(), err
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
}

func TestIntegration_PullJSONErrorCodes(t *testing.T) {
	testEnv(t)
	base := t.TempDir()
	good := filepath.Join(base, "good") // fresh repo, no upstream -> skipped/no_upstream
	gone := filepath.Join(base, "gone") // removed -> failed/path_missing
	initRepo(t, good)
	initRepo(t, gone)
	runSokoInit(t, good)
	runSokoInit(t, gone)
	if err := os.RemoveAll(gone); err != nil {
		t.Fatalf("rm: %v", err)
	}

	out, err := runSokoErr(t, "pull", "--json")
	if err == nil {
		t.Error("pull with a missing path should return an error")
	}

	var entries []errEntry
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		t.Fatalf("parse: %v\n%s", err, out)
	}

	g, ok := entryByName(entries, "gone")
	if !ok || g.Status != "failed" || g.ErrorCode == nil || *g.ErrorCode != "path_missing" {
		t.Errorf("gone entry = %+v, want failed/path_missing", g)
	}
	s, ok := entryByName(entries, "good")
	if !ok || s.Status != "skipped" || s.ErrorCode == nil || *s.ErrorCode != "no_upstream" {
		t.Errorf("good entry = %+v, want skipped/no_upstream", s)
	}
}

func TestIntegration_PullJSONNotFastForward(t *testing.T) {
	testEnv(t)
	base := t.TempDir()
	originBare := filepath.Join(base, "origin.git")
	a := filepath.Join(base, "A")
	b := filepath.Join(base, "B")

	// Bare origin with a fixed default branch so the test is deterministic.
	runGit(t, base, "init", "--bare", "-b", "main", originBare)

	// Clone A, seed a commit, push.
	runGit(t, base, "clone", originBare, a)
	runGit(t, a, "config", "user.email", "a@a.a")
	runGit(t, a, "config", "user.name", "A")
	if err := os.WriteFile(filepath.Join(a, "f1.txt"), []byte("1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, a, "add", "-A")
	runGit(t, a, "commit", "-m", "c1")
	runGit(t, a, "push", "-u", "origin", "main")

	// Clone B from origin (now at c1), tracking origin/main.
	runGit(t, base, "clone", originBare, b)
	runGit(t, b, "config", "user.email", "b@b.b")
	runGit(t, b, "config", "user.name", "B")

	// A advances origin.
	if err := os.WriteFile(filepath.Join(a, "f2.txt"), []byte("2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, a, "add", "-A")
	runGit(t, a, "commit", "-m", "c2")
	runGit(t, a, "push", "origin", "main")

	// B makes a divergent local commit -> --ff-only pull cannot fast-forward.
	if err := os.WriteFile(filepath.Join(b, "f3.txt"), []byte("3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, b, "add", "-A")
	runGit(t, b, "commit", "-m", "c3")

	runSokoInit(t, b)

	out, err := runSokoErr(t, "pull", "--json")
	if err == nil {
		t.Error("pull on a diverged branch should return an error")
	}
	var entries []errEntry
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		t.Fatalf("parse: %v\n%s", err, out)
	}
	// B is the only registered repo (named from its origin remote URL); assert
	// on the single failed entry rather than its derived name.
	var e errEntry
	for _, x := range entries {
		if x.Status == "failed" {
			e = x
		}
	}
	if e.ErrorCode == nil || *e.ErrorCode != "not_fast_forward" {
		t.Errorf("failed entry = %+v (all=%+v), want not_fast_forward", e, entries)
	}
}

func TestIntegration_FetchJSONErrorCodes(t *testing.T) {
	testEnv(t)
	base := t.TempDir()
	ok := filepath.Join(base, "ok")         // fresh repo -> fetch succeeds, no code
	broken := filepath.Join(base, "broken") // bad remote -> git_failure
	gone := filepath.Join(base, "gone")     // removed -> path_missing
	initRepo(t, ok)
	initRepo(t, broken)
	initRepo(t, gone)
	// Remote basename "broken" so soko names the repo "broken"; the path does
	// not exist, so the fetch fails with a generic git error.
	runGit(t, broken, "remote", "add", "origin", filepath.Join(base, "broken.git"))
	runSokoInit(t, ok)
	runSokoInit(t, broken)
	runSokoInit(t, gone)
	if err := os.RemoveAll(gone); err != nil {
		t.Fatalf("rm: %v", err)
	}

	out, err := runSokoErr(t, "fetch", "--json")
	if err == nil {
		t.Error("fetch with failing repos should return an error")
	}
	var entries []errEntry
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		t.Fatalf("parse: %v\n%s", err, out)
	}

	o, _ := entryByName(entries, "ok")
	if o.Status != "fetched" || o.ErrorCode != nil {
		t.Errorf("ok entry = %+v, want fetched with no error_code (omitempty)", o)
	}
	br, _ := entryByName(entries, "broken")
	if br.ErrorCode == nil || *br.ErrorCode != "git_failure" {
		t.Errorf("broken entry = %+v, want git_failure", br)
	}
	gn, _ := entryByName(entries, "gone")
	if gn.ErrorCode == nil || *gn.ErrorCode != "path_missing" {
		t.Errorf("gone entry = %+v, want path_missing", gn)
	}
}

func TestIntegration_ExecJSONErrorCodes(t *testing.T) {
	testEnv(t)
	base := t.TempDir()
	ok := filepath.Join(base, "ok")
	gone := filepath.Join(base, "gone")
	initRepo(t, ok)
	initRepo(t, gone)
	runSokoInit(t, ok)
	runSokoInit(t, gone)
	if err := os.RemoveAll(gone); err != nil {
		t.Fatalf("rm: %v", err)
	}

	// A non-zero exit code is a command failure, not a soko failure: no code.
	// (exec --json reports per-repo and returns success at the process level.)
	out := runSoko(t, "exec", "--json", "--", "false")
	var entries []errEntry
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		t.Fatalf("parse: %v\n%s", err, out)
	}
	o, _ := entryByName(entries, "ok")
	if o.ExitCode == nil || *o.ExitCode == 0 {
		t.Errorf("ok entry = %+v, want non-zero exit_code", o)
	}
	if o.ErrorCode != nil {
		t.Errorf("ok entry = %+v, want no error_code on a non-zero exit", o)
	}
	gn, _ := entryByName(entries, "gone")
	if gn.ErrorCode == nil || *gn.ErrorCode != "path_missing" {
		t.Errorf("gone entry = %+v, want path_missing", gn)
	}
}

func TestIntegration_ExecJSONSpawnUnknown(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "ok")
	initRepo(t, dir)
	runSokoInit(t, dir)

	// Failing to spawn the binary is a non-git failure -> unknown.
	out := runSoko(t, "exec", "--json", "--", "soko-no-such-binary-xyz")
	var entries []errEntry
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		t.Fatalf("parse: %v\n%s", err, out)
	}
	e, _ := entryByName(entries, "ok")
	if e.ErrorCode == nil || *e.ErrorCode != "unknown" {
		t.Errorf("ok entry = %+v, want error_code unknown", e)
	}
}

func TestIntegration_CleanJSONPathMissing(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "gone")
	initRepo(t, dir)
	runSokoInit(t, dir)
	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("rm: %v", err)
	}

	out := runSoko(t, "clean", "--json", "--dry-run")
	var entries []errEntry
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		t.Fatalf("parse: %v\n%s", err, out)
	}
	e, ok := entryByName(entries, "gone")
	if !ok || e.ErrorCode == nil || *e.ErrorCode != "path_missing" {
		t.Errorf("gone entry = %+v, want path_missing", e)
	}
}

func TestIntegration_StashJSONPathMissing(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "gone")
	initRepo(t, dir)
	runSokoInit(t, dir)
	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("rm: %v", err)
	}

	// stash --json reports per-repo and returns success at the process level.
	out := runSoko(t, "stash", "--json")
	var entries []errEntry
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		t.Fatalf("parse: %v\n%s", err, out)
	}
	e, ok := entryByName(entries, "gone")
	if !ok || e.Status != "failed" || e.ErrorCode == nil || *e.ErrorCode != "path_missing" {
		t.Errorf("gone entry = %+v, want failed/path_missing", e)
	}
}
