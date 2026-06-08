package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestNameFromURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want string
	}{
		{
			name: "ssh with .git suffix",
			url:  "git@github.com:user/repo.git",
			want: "repo",
		},
		{
			name: "https with .git suffix",
			url:  "https://github.com/user/repo.git",
			want: "repo",
		},
		{
			name: "https without .git suffix",
			url:  "https://github.com/user/repo",
			want: "repo",
		},
		{
			name: "ssh without .git suffix",
			url:  "git@github.com:user/repo",
			want: "repo",
		},
		{
			name: "nested path https",
			url:  "https://gitlab.com/org/group/repo.git",
			want: "repo",
		},
		{
			name: "nested path ssh",
			url:  "git@gitlab.com:org/group/repo.git",
			want: "repo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := nameFromURL(tt.url)
			if got != tt.want {
				t.Errorf("nameFromURL(%q) = %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}

func TestIsGitRepo(t *testing.T) {
	ctx := context.Background()

	t.Run("valid git repo", func(t *testing.T) {
		dir := initTestRepo(t)
		if !IsGitRepo(ctx, dir) {
			t.Error("IsGitRepo() = false, want true")
		}
	})

	t.Run("not a git repo", func(t *testing.T) {
		dir := t.TempDir()
		if IsGitRepo(ctx, dir) {
			t.Error("IsGitRepo() = true, want false")
		}
	})
}

func TestRepoName_FallsBackToBasename(t *testing.T) {
	ctx := context.Background()
	dir := initTestRepo(t)

	got := RepoName(ctx, dir)
	want := filepath.Base(dir)

	if got != want {
		t.Errorf("RepoName() = %q, want %q (basename fallback)", got, want)
	}
}

func TestRepoName_UsesRemote(t *testing.T) {
	ctx := context.Background()
	dir := initTestRepo(t)

	// Add a remote origin.
	cmd := exec.CommandContext(ctx, "git", "remote", "add", "origin", "git@github.com:testuser/my-project.git")
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		t.Fatalf("adding remote: %v", err)
	}

	got := RepoName(ctx, dir)
	if got != "my-project" {
		t.Errorf("RepoName() = %q, want %q", got, "my-project")
	}
}

func TestRun_ReturnsErrorWithStderr(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	_, err := Run(ctx, dir, "log")
	if err == nil {
		t.Fatal("Run() error = nil, want error for git log in non-repo")
	}
}

func TestPull(t *testing.T) {
	ctx := context.Background()

	// Bare upstream with HEAD fixed to master so clones check out cleanly.
	bare := t.TempDir()
	gitMust(t, bare, "init", "--bare", "-b", "master", ".")

	// Seed working tree pushes the first commit to the upstream.
	seed := t.TempDir()
	gitMust(t, seed, "clone", bare, ".")
	configRepo(t, seed)
	writeAndCommit(t, seed, "a.txt", "one", "first")
	gitMust(t, seed, "push", "origin", "master")

	// The repo under test tracks the upstream.
	repo := t.TempDir()
	gitMust(t, repo, "clone", bare, ".")
	configRepo(t, repo)

	// Nothing new upstream: HEAD must not move.
	updated, err := Pull(ctx, repo, false)
	if err != nil {
		t.Fatalf("Pull (up to date) error = %v", err)
	}
	if updated {
		t.Error("Pull() updated = true, want false when already up to date")
	}

	// Advance the upstream, then pull must fast-forward.
	writeAndCommit(t, seed, "b.txt", "two", "second")
	gitMust(t, seed, "push", "origin", "master")

	updated, err = Pull(ctx, repo, false)
	if err != nil {
		t.Fatalf("Pull (update) error = %v", err)
	}
	if !updated {
		t.Error("Pull() updated = false, want true after upstream advanced")
	}
}

func TestPull_NoUpstreamReturnsError(t *testing.T) {
	ctx := context.Background()
	dir := initTestRepo(t)

	// No remote and no upstream tracking branch — pull must error, not panic.
	if _, err := Pull(ctx, dir, false); err == nil {
		t.Error("Pull() error = nil, want error for repo with no upstream")
	}
}

// TestPull_RebaseDiverged covers the path where rebase=true matters: a branch
// that has diverged from its upstream. --ff-only must fail, --rebase succeeds.
func TestPull_RebaseDiverged(t *testing.T) {
	ctx := context.Background()

	bare := t.TempDir()
	gitMust(t, bare, "init", "--bare", "-b", "master", ".")

	seed := t.TempDir()
	gitMust(t, seed, "clone", bare, ".")
	configRepo(t, seed)
	writeAndCommit(t, seed, "a.txt", "one", "first")
	gitMust(t, seed, "push", "origin", "master")

	repo := t.TempDir()
	gitMust(t, repo, "clone", bare, ".")
	configRepo(t, repo)

	// Diverge: a local commit in repo and a different commit upstream.
	writeAndCommit(t, repo, "local.txt", "local", "local change")
	writeAndCommit(t, seed, "b.txt", "two", "upstream change")
	gitMust(t, seed, "push", "origin", "master")

	// Fast-forward-only cannot reconcile a diverged branch.
	if _, err := Pull(ctx, repo, false); err == nil {
		t.Error("Pull(rebase=false) on diverged branch = nil, want error")
	}

	// Rebase replays the local commit on top of the upstream and advances HEAD.
	updated, err := Pull(ctx, repo, true)
	if err != nil {
		t.Fatalf("Pull(rebase=true) error = %v", err)
	}
	if !updated {
		t.Error("Pull(rebase=true) updated = false, want true after rebase")
	}
}

func TestHasUpstream(t *testing.T) {
	ctx := context.Background()

	t.Run("no upstream", func(t *testing.T) {
		dir := initTestRepo(t)
		if HasUpstream(ctx, dir) {
			t.Error("HasUpstream() = true, want false for repo with no remote")
		}
	})

	t.Run("with upstream", func(t *testing.T) {
		bare := t.TempDir()
		gitMust(t, bare, "init", "--bare", "-b", "master", ".")
		seed := t.TempDir()
		gitMust(t, seed, "clone", bare, ".")
		configRepo(t, seed)
		writeAndCommit(t, seed, "a.txt", "one", "first")
		gitMust(t, seed, "push", "origin", "master")

		repo := t.TempDir()
		gitMust(t, repo, "clone", bare, ".")
		if !HasUpstream(ctx, repo) {
			t.Error("HasUpstream() = false, want true for tracking clone")
		}
	})
}

func TestRemotes(t *testing.T) {
	ctx := context.Background()

	t.Run("no remotes returns empty non-nil map", func(t *testing.T) {
		dir := initTestRepo(t)
		got, err := Remotes(ctx, dir)
		if err != nil {
			t.Fatalf("Remotes() error = %v", err)
		}
		if got == nil {
			t.Fatal("Remotes() = nil, want empty non-nil map")
		}
		if len(got) != 0 {
			t.Errorf("Remotes() = %v, want empty", got)
		}
	})

	t.Run("single origin", func(t *testing.T) {
		dir := initTestRepo(t)
		gitMust(t, dir, "remote", "add", "origin", "git@github.com:acme/api.git")

		got, err := Remotes(ctx, dir)
		if err != nil {
			t.Fatalf("Remotes() error = %v", err)
		}
		if want := "git@github.com:acme/api.git"; got["origin"] != want {
			t.Errorf("Remotes()[origin] = %q, want %q", got["origin"], want)
		}
		if len(got) != 1 {
			t.Errorf("Remotes() has %d entries, want 1 (fetch/push deduped)", len(got))
		}
	})

	t.Run("multiple remotes", func(t *testing.T) {
		dir := initTestRepo(t)
		gitMust(t, dir, "remote", "add", "origin", "git@github.com:acme/api.git")
		gitMust(t, dir, "remote", "add", "fork", "git@github.com:me/api.git")

		got, err := Remotes(ctx, dir)
		if err != nil {
			t.Fatalf("Remotes() error = %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("Remotes() has %d entries, want 2", len(got))
		}
		if got["origin"] != "git@github.com:acme/api.git" {
			t.Errorf("Remotes()[origin] = %q", got["origin"])
		}
		if got["fork"] != "git@github.com:me/api.git" {
			t.Errorf("Remotes()[fork] = %q", got["fork"])
		}
	})

	t.Run("url with spaces survives intact", func(t *testing.T) {
		dir := initTestRepo(t)
		// A local file remote whose path contains a space is legal and must
		// not be truncated by whitespace splitting.
		url := "/tmp/my repo/upstream.git"
		gitMust(t, dir, "remote", "add", "origin", url)

		got, err := Remotes(ctx, dir)
		if err != nil {
			t.Fatalf("Remotes() error = %v", err)
		}
		if got["origin"] != url {
			t.Errorf("Remotes()[origin] = %q, want %q", got["origin"], url)
		}
	})

	t.Run("errors on non-repo directory", func(t *testing.T) {
		dir := t.TempDir() // exists but is not a git repo
		if _, err := Remotes(ctx, dir); err == nil {
			t.Error("Remotes() error = nil, want error for non-repo directory")
		}
	})
}

func TestUpstreamBranch(t *testing.T) {
	ctx := context.Background()

	t.Run("no upstream returns error", func(t *testing.T) {
		dir := initTestRepo(t)
		if _, err := UpstreamBranch(ctx, dir); err == nil {
			t.Error("UpstreamBranch() error = nil, want error for repo with no upstream")
		}
		if HasUpstream(ctx, dir) {
			t.Error("HasUpstream() = true, want false — must agree with UpstreamBranch")
		}
	})

	t.Run("tracking branch returns short name", func(t *testing.T) {
		bare := t.TempDir()
		gitMust(t, bare, "init", "--bare", "-b", "master", ".")
		seed := t.TempDir()
		gitMust(t, seed, "clone", bare, ".")
		configRepo(t, seed)
		writeAndCommit(t, seed, "a.txt", "one", "first")
		gitMust(t, seed, "push", "origin", "master")

		repo := t.TempDir()
		gitMust(t, repo, "clone", bare, ".")

		got, err := UpstreamBranch(ctx, repo)
		if err != nil {
			t.Fatalf("UpstreamBranch() error = %v", err)
		}
		if want := "origin/master"; got != want {
			t.Errorf("UpstreamBranch() = %q, want %q", got, want)
		}
		if !HasUpstream(ctx, repo) {
			t.Error("HasUpstream() = false, want true — must agree with UpstreamBranch")
		}
	})
}

// gitMust runs a git command in dir and fails the test on error.
func gitMust(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// configRepo sets a local identity so commits succeed under an isolated config.
func configRepo(t *testing.T, dir string) {
	t.Helper()
	gitMust(t, dir, "config", "user.email", "test@test.com")
	gitMust(t, dir, "config", "user.name", "Test")
}

// writeAndCommit writes a file and commits it in dir.
func writeAndCommit(t *testing.T, dir, name, content, message string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("writing %s: %v", name, err)
	}
	gitMust(t, dir, "add", ".")
	gitMust(t, dir, "commit", "-m", message)
}

// initTestRepo creates a temporary git repository and returns its path.
func initTestRepo(t *testing.T) string {
	t.Helper()

	ctx := context.Background()
	dir := t.TempDir()

	cmds := [][]string{
		{"git", "init"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
	}

	for _, args := range cmds {
		cmd := exec.CommandContext(ctx, args[0], args[1:]...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("running %v: %v\n%s", args, err, out)
		}
	}

	return dir
}
