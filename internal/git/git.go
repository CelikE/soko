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

// Run executes git with the given arguments in the specified directory. It
// returns the trimmed stdout on success, or an error that includes stderr.
func Run(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
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
