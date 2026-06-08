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

const applySrc = "hello\nworld\n"

type applyEntry struct {
	Repo    string `json:"repo"`
	Path    string `json:"path"`
	Dest    string `json:"dest"`
	Action  string `json:"action"`
	Diff    string `json:"diff"`
	Written bool   `json:"written"`
	Error   string `json:"error"`
}

// writeSrc creates the source file under a temp dir and returns its path.
func writeSrc(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "src.txt")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write src: %v", err)
	}
	return p
}

// runSokoStdin runs soko with a stdin string, returning stdout and the error.
func runSokoStdin(t *testing.T, stdin string, args ...string) (string, error) {
	t.Helper()
	var stdout bytes.Buffer
	cmd := cli.NewRootCmd("test")
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetArgs(args)
	return stdout.String(), cmd.Execute()
}

func applyEntries(t *testing.T, out string) []applyEntry {
	t.Helper()
	var entries []applyEntry
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		t.Fatalf("parse apply json: %v\n%s", err, out)
	}
	return entries
}

func entryFor(entries []applyEntry, repo string) (applyEntry, bool) {
	for _, e := range entries {
		if e.Repo == repo {
			return e, true
		}
	}
	return applyEntry{}, false
}

func TestIntegration_ApplyDryRunClassifies(t *testing.T) {
	testEnv(t)
	base := t.TempDir()
	rc := filepath.Join(base, "rc") // create: no dest
	ru := filepath.Join(base, "ru") // update: different dest
	rx := filepath.Join(base, "rx") // unchanged: identical dest
	for _, d := range []string{rc, ru, rx} {
		initRepo(t, d)
		runSokoInit(t, d)
	}
	if err := os.WriteFile(filepath.Join(ru, "shared.txt"), []byte("OLD\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rx, "shared.txt"), []byte(applySrc), 0o644); err != nil {
		t.Fatal(err)
	}
	src := writeSrc(t, applySrc)

	out := runSoko(t, "apply", src, "--to", "shared.txt")
	for _, want := range []string{"(create)", "(update)", "· unchanged", "1 to create · 1 to update · 1 unchanged · 3 repos", "run with --write"} {
		if !strings.Contains(out, want) {
			t.Errorf("dry-run output missing %q\n%s", want, out)
		}
	}

	// Dry run writes nothing.
	if _, err := os.Stat(filepath.Join(rc, "shared.txt")); !os.IsNotExist(err) {
		t.Error("dry run should not create the file")
	}
	if b, _ := os.ReadFile(filepath.Join(ru, "shared.txt")); string(b) != "OLD\n" {
		t.Errorf("dry run should not modify ru, got %q", b)
	}
}

func TestIntegration_ApplyWriteForce(t *testing.T) {
	testEnv(t)
	base := t.TempDir()
	rc := filepath.Join(base, "rc")
	rx := filepath.Join(base, "rx")
	initRepo(t, rc)
	initRepo(t, rx)
	runSokoInit(t, rc)
	runSokoInit(t, rx)
	if err := os.WriteFile(filepath.Join(rx, "shared.txt"), []byte(applySrc), 0o644); err != nil {
		t.Fatal(err)
	}
	src := writeSrc(t, applySrc)

	out := runSoko(t, "apply", src, "--to", "nested/dir/shared.txt", "--write", "--force")
	_ = out

	// Create wrote the file, with parent dirs created.
	got, err := os.ReadFile(filepath.Join(rc, "nested", "dir", "shared.txt"))
	if err != nil || string(got) != applySrc {
		t.Errorf("rc file = %q, err=%v, want %q", got, err, applySrc)
	}
}

func TestIntegration_ApplyWriteConfirmYesNo(t *testing.T) {
	testEnv(t)
	base := t.TempDir()
	r := filepath.Join(base, "r")
	initRepo(t, r)
	runSokoInit(t, r)
	src := writeSrc(t, applySrc)

	// "n" aborts: nothing written.
	if _, err := runSokoStdin(t, "n\n", "apply", src, "--to", "f.txt", "--write"); err != nil {
		t.Fatalf("abort path returned error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(r, "f.txt")); !os.IsNotExist(err) {
		t.Error("answering 'n' should write nothing")
	}

	// "y" writes.
	if _, err := runSokoStdin(t, "y\n", "apply", src, "--to", "f.txt", "--write"); err != nil {
		t.Fatalf("confirm path returned error: %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(r, "f.txt")); string(b) != applySrc {
		t.Errorf("answering 'y' should write the file, got %q", b)
	}
}

func TestIntegration_ApplyRequiresTo(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "r")
	initRepo(t, dir)
	runSokoInit(t, dir)
	src := writeSrc(t, applySrc)

	if _, err := runSokoErr(t, "apply", src); err == nil {
		t.Error("apply without --to should error")
	}
}

func TestIntegration_ApplyBadDestinations(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "r")
	initRepo(t, dir)
	runSokoInit(t, dir)
	src := writeSrc(t, applySrc)

	// Escaping destination -> per-repo error, nothing written.
	out := runSoko(t, "apply", src, "--to", "../../escape.txt", "--json")
	e, _ := entryFor(applyEntries(t, out), "r")
	if e.Action != "error" || !strings.Contains(e.Error, "escapes repo root") {
		t.Errorf("escape entry = %+v, want error/escapes repo root", e)
	}

	// Absolute destination -> per-repo error.
	out = runSoko(t, "apply", src, "--to", "/tmp/abs.txt", "--json")
	e, _ = entryFor(applyEntries(t, out), "r")
	if e.Action != "error" || !strings.Contains(e.Error, "must be relative") {
		t.Errorf("absolute entry = %+v, want error/must be relative", e)
	}
}

func TestIntegration_ApplyPreservesMode(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "r")
	initRepo(t, dir)
	runSokoInit(t, dir)

	// Distinctive mode on the source.
	srcPath := filepath.Join(t.TempDir(), "exec.sh")
	if err := os.WriteFile(srcPath, []byte("#!/bin/sh\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(srcPath, 0o640); err != nil {
		t.Fatal(err)
	}

	runSoko(t, "apply", srcPath, "--to", "exec.sh", "--write", "--force")
	info, err := os.Stat(filepath.Join(dir, "exec.sh"))
	if err != nil {
		t.Fatalf("stat written file: %v", err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Errorf("written mode = %o, want 640", info.Mode().Perm())
	}
}

func TestIntegration_ApplyTagAndRepoFilter(t *testing.T) {
	testEnv(t)
	base := t.TempDir()
	a := filepath.Join(base, "svc-a")
	b := filepath.Join(base, "svc-b")
	initRepo(t, a)
	initRepo(t, b)
	runSokoInit(t, a)
	runSokoInit(t, b)
	runSoko(t, "tag", "add", "-r", "svc-a", "target")
	src := writeSrc(t, applySrc)

	out := runSoko(t, "apply", src, "--to", "f.txt", "--tag", "target")
	if !strings.Contains(out, "svc-a") || strings.Contains(out, "svc-b") {
		t.Errorf("--tag = %q, want only svc-a", out)
	}

	out = runSoko(t, "apply", src, "--to", "f.txt", "svc-b")
	if !strings.Contains(out, "svc-b") || strings.Contains(out, "svc-a") {
		t.Errorf("positional filter = %q, want only svc-b", out)
	}
}

func TestIntegration_ApplyMissingPathHint(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "gone")
	initRepo(t, dir)
	runSokoInit(t, dir)
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	src := writeSrc(t, applySrc)

	out := runSoko(t, "apply", src, "--to", "f.txt")
	if !strings.Contains(out, "soko prune") {
		t.Errorf("missing repo should trigger prune hint, got %q", out)
	}
}

func TestIntegration_ApplyDestIsDirectory(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "r")
	initRepo(t, dir)
	runSokoInit(t, dir)
	if err := os.MkdirAll(filepath.Join(dir, "f.txt"), 0o755); err != nil {
		t.Fatal(err)
	}
	src := writeSrc(t, applySrc)

	// A directory at the destination is a per-repo error, never a panic.
	out := runSoko(t, "apply", src, "--to", "f.txt", "--json")
	e, ok := entryFor(applyEntries(t, out), "r")
	if !ok || e.Action != "error" {
		t.Errorf("dir-dest entry = %+v, want error", e)
	}
}

func TestIntegration_ApplyJSON(t *testing.T) {
	testEnv(t)
	base := t.TempDir()
	rc := filepath.Join(base, "rc")
	rx := filepath.Join(base, "rx")
	initRepo(t, rc)
	initRepo(t, rx)
	runSokoInit(t, rc)
	runSokoInit(t, rx)
	if err := os.WriteFile(filepath.Join(rx, "f.txt"), []byte(applySrc), 0o644); err != nil {
		t.Fatal(err)
	}
	src := writeSrc(t, applySrc)

	// Dry-run JSON: unchanged present with empty diff; create has a diff.
	out := runSoko(t, "apply", src, "--to", "f.txt", "--json")
	entries := applyEntries(t, out)
	c, _ := entryFor(entries, "rc")
	if c.Action != "create" || c.Diff == "" {
		t.Errorf("rc = %+v, want create with a diff", c)
	}
	x, _ := entryFor(entries, "rx")
	if x.Action != "unchanged" || x.Diff != "" {
		t.Errorf("rx = %+v, want unchanged with empty diff", x)
	}

	// --json --write requires --force.
	if _, err := runSokoErr(t, "apply", src, "--to", "f.txt", "--json", "--write"); err == nil {
		t.Error("--json --write without --force should error")
	}

	// --json --write --force sets written:true on the created entry.
	out = runSoko(t, "apply", src, "--to", "f.txt", "--json", "--write", "--force")
	c, _ = entryFor(applyEntries(t, out), "rc")
	if !c.Written {
		t.Errorf("rc = %+v, want written:true", c)
	}
}

func TestIntegration_ApplyMissingSource(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "r")
	initRepo(t, dir)
	runSokoInit(t, dir)

	if _, err := runSokoErr(t, "apply", "/nonexistent/source.txt", "--to", "f.txt"); err == nil {
		t.Error("a missing source file should be a hard error")
	}
}

func TestIntegration_ApplyWriteFailureExitsNonZero(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("write-permission failure cannot be simulated as root")
	}
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "r")
	initRepo(t, dir)
	runSokoInit(t, dir)
	// Read-only directory under the repo: writing into it fails.
	ro := filepath.Join(dir, "ro")
	if err := os.MkdirAll(ro, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(ro, 0o755) })
	src := writeSrc(t, applySrc)

	_, err := runSokoErr(t, "apply", src, "--to", "ro/f.txt", "--write", "--force")
	if err == nil {
		t.Error("a write failure should make apply exit non-zero")
	}
}
