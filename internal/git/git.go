// Package git provides low-level wrappers around the git CLI. This is the only
// package in soko that calls exec.Command("git", ...).
package git

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// binary is the git executable path. Defaults to "git" (resolved via PATH).
// Set via SetBinary to use a custom git installation.
var binary = "git"

// SetBinary overrides the git binary path used by all functions in this package.
func SetBinary(path string) {
	if path != "" {
		binary = path
	}
}

// Binary returns the current git binary path.
func Binary() string {
	return binary
}

// Run executes git with the given arguments in the specified directory. It
// returns the trimmed stdout on success, or an error that includes stderr.
func Run(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Dir = dir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// Format: "git <args>: <exit error>: <stderr output>"
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}

	return strings.TrimSpace(stdout.String()), nil
}

// IsGitRepo returns true if dir is inside a git repository.
func IsGitRepo(ctx context.Context, dir string) bool {
	_, err := Run(ctx, dir, "rev-parse", "--show-toplevel")
	return err == nil
}

// RepoName returns the repository name for the git repo at dir. It first tries
// to extract the name from the origin remote URL. If there is no remote, it
// falls back to the directory basename.
func RepoName(ctx context.Context, dir string) string {
	url, err := Run(ctx, dir, "remote", "get-url", "origin")
	if err != nil || url == "" {
		return filepath.Base(dir)
	}

	return nameFromURL(url)
}

// Fetch runs git fetch in the given directory. If prune is true, it passes
// --prune to clean up stale remote tracking refs.
func Fetch(ctx context.Context, dir string, prune bool) error {
	args := []string{"fetch"}
	if prune {
		args = append(args, "--prune")
	}
	_, err := Run(ctx, dir, args...)
	return err
}

// IsWorktree returns true if dir is a linked git worktree (not the main
// working tree). It compares --git-dir and --git-common-dir.
func IsWorktree(ctx context.Context, dir string) bool {
	gitDir, err := Run(ctx, dir, "rev-parse", "--git-dir")
	if err != nil {
		return false
	}
	commonDir, err := Run(ctx, dir, "rev-parse", "--git-common-dir")
	if err != nil {
		return false
	}
	// In a linked worktree, git-dir points to .git/worktrees/<name> while
	// git-common-dir points to the shared .git directory.
	return filepath.Clean(gitDir) != filepath.Clean(commonDir)
}

// MainRepoPath returns the top-level working directory of the main checkout
// for the repository. In a linked worktree this resolves back to the primary
// working tree; for a normal repo it returns the repo root.
func MainRepoPath(ctx context.Context, dir string) (string, error) {
	commonDir, err := Run(ctx, dir, "rev-parse", "--git-common-dir")
	if err != nil {
		return "", err
	}
	// commonDir is the shared .git directory (absolute or relative to dir).
	if !filepath.IsAbs(commonDir) {
		commonDir = filepath.Join(dir, commonDir)
	}
	// The main repo is the parent of the .git directory.
	mainRepo := filepath.Dir(filepath.Clean(commonDir))
	return mainRepo, nil
}

// nameFromURL extracts a repository name from a git remote URL. It handles
// both SSH (git@host:user/repo.git) and HTTPS (https://host/user/repo.git)
// formats.
func nameFromURL(rawURL string) string {
	// SSH format: git@github.com:user/repo.git
	if idx := strings.LastIndex(rawURL, ":"); idx != -1 && !strings.Contains(rawURL, "://") {
		rawURL = rawURL[idx+1:]
	}

	// Take the last path segment.
	name := rawURL
	if idx := strings.LastIndex(name, "/"); idx != -1 {
		name = name[idx+1:]
	}

	// Strip .git suffix.
	name = strings.TrimSuffix(name, ".git")

	return name
}
