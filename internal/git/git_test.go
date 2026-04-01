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
