package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CelikE/soko/internal/config"
)

func openGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
}

func openRepo(t *testing.T, path, url string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	openGit(t, path, "init")
	openGit(t, path, "config", "user.name", "Test")
	openGit(t, path, "config", "user.email", "test@example.com")
	if err := os.WriteFile(filepath.Join(path, "README.md"), []byte("test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	openGit(t, path, "add", "README.md")
	openGit(t, path, "commit", "-m", "initial")
	openGit(t, path, "remote", "add", "origin", url)
}

func runOpen(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var stdout bytes.Buffer
	cmd := NewRootCmd("test")
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs(append([]string{"open"}, args...))
	err := cmd.Execute()
	return stdout.String(), err
}

func TestOpenCurrentCheckout(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	parent := filepath.Join(base, "parent")
	linked := filepath.Join(base, "linked")
	clone := filepath.Join(base, "clone")
	source := filepath.Join(base, "module-source")
	module := filepath.Join(parent, "modules", "child")
	const parentURL = "https://github.com/example/parent.git"
	const moduleURL = "https://github.com/example/child.git"
	openRepo(t, parent, parentURL)
	openGit(t, parent, "worktree", "add", "-b", "linked", linked)
	openRepo(t, clone, parentURL) // same remote does not imply registration
	openRepo(t, source, moduleURL)
	openGit(t, parent, "-c", "protocol.file.allow=always", "submodule", "add", source, "modules/child")
	openGit(t, module, "remote", "set-url", "origin", moduleURL)

	cfg := &config.Config{Repos: []config.RepoEntry{
		{Name: "parent", Path: parent},
		{Name: "parent/linked", Path: linked, WorktreeOf: "parent"},
	}}
	if err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}

	var opened []string
	previous := openBrowser
	openBrowser = func(url string) error {
		opened = append(opened, url)
		return nil
	}
	t.Cleanup(func() { openBrowser = previous })

	assertOpen := func(dir, wantName, wantPath, wantURL string) {
		t.Helper()
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		t.Chdir(dir)
		out, err := runOpen(t, "--json")
		if err != nil {
			t.Fatalf("open from %s: %v", dir, err)
		}
		var results []struct{ Name, Path, URL string }
		if err := json.Unmarshal([]byte(out), &results); err != nil {
			t.Fatalf("invalid JSON %q: %v", out, err)
		}
		if len(results) != 1 || results[0].Name != wantName || results[0].Path != wantPath || results[0].URL != wantURL {
			t.Fatalf("from %s: results = %+v, want %s %s %s", dir, results, wantName, wantPath, wantURL)
		}
		if len(opened) == 0 || opened[len(opened)-1] != wantURL {
			t.Fatalf("browser calls = %v, want last %s", opened, wantURL)
		}
	}
	assertRejected := func(dir string, args []string, wantErr string) {
		t.Helper()
		t.Chdir(dir)
		before := len(opened)
		out, err := runOpen(t, args...)
		if err == nil || !strings.Contains(err.Error(), wantErr) || out != "" {
			t.Fatalf("from %s: output %q, error %v, want %q", dir, out, err, wantErr)
		}
		if len(opened) != before {
			t.Fatalf("invalid selector opened browser: %v", opened[before:])
		}
	}

	assertOpen(parent, "parent", parent, strings.TrimSuffix(parentURL, ".git"))
	assertOpen(filepath.Join(parent, "nested"), "parent", parent, strings.TrimSuffix(parentURL, ".git"))
	assertOpen(linked, "parent/linked", linked, strings.TrimSuffix(parentURL, ".git"))
	assertOpen(filepath.Join(linked, "nested"), "parent/linked", linked, strings.TrimSuffix(parentURL, ".git"))
	assertRejected(clone, []string{"--json"}, "current directory is not a registered repo")
	assertRejected(module, []string{"--json"}, "current directory is not a registered repo")
	assertRejected(base, []string{"--json"}, "not inside a git repository")
	t.Chdir(base)
	out, err := runOpen(t, "parent", "--json")
	if err != nil || !strings.Contains(out, `"name": "parent"`) {
		t.Fatalf("explicit name outside a checkout: output %q, error %v", out, err)
	}
	assertRejected(parent, []string{"parent", "--tag", "work"}, "a repo name cannot be combined with --tag")
	cfg.Repos = cfg.Repos[:1]
	if err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	assertRejected(linked, []string{"--json"}, "current directory is not a registered repo")

	// A submodule selects only its own explicitly registered checkout, never
	// its registered superproject, even when invoked from deeper inside it.
	cfg.Repos = append(cfg.Repos, config.RepoEntry{Name: "child", Path: module})
	if err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	assertOpen(filepath.Join(module, "nested"), "child", module, strings.TrimSuffix(moduleURL, ".git"))

	// Browser errors remain visible and cause command failure.
	openBrowser = func(url string) error { return errors.New("browser unavailable") }
	t.Chdir(parent)
	out, err = runOpen(t, "--json")
	if err == nil || err.Error() != "1 repo failed to open" || !strings.Contains(out, `"error": "browser unavailable"`) {
		t.Fatalf("browser failure: output %q, error %v", out, err)
	}
}
