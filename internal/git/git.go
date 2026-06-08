// Package git provides low-level wrappers around the git CLI. This is the only
// package in soko that calls exec.Command("git", ...).
package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
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

// Match is one git grep hit: a file path, a 1-based line number, and the
// matched line text. In files-only mode Line is 0 and Text is empty.
type Match struct {
	File string
	Line int
	Text string
}

// Grep runs `git grep` for pattern in dir and returns the parsed matches.
// regex selects -E (POSIX extended regex) over the default -F (fixed string);
// ignoreCase adds -i; filesOnly adds -l (paths only). Exit code 1 means "no
// match" and yields (nil, nil) — the basis for graceful degradation across a
// workspace. Exit codes >= 2 (e.g. a bad regex) return an error.
//
// It runs git directly rather than via Run because Run collapses any non-zero
// exit into an error and discards stdout, whereas Grep must distinguish exit 1
// from exit >= 2 and keep stdout.
func Grep(ctx context.Context, dir, pattern string, regex, ignoreCase, filesOnly bool) ([]Match, error) {
	args := []string{"grep", "--color=never"}
	if filesOnly {
		args = append(args, "-l")
	} else {
		args = append(args, "--line-number")
	}
	if ignoreCase {
		args = append(args, "-i")
	}
	if regex {
		args = append(args, "-E")
	} else {
		args = append(args, "-F")
	}
	args = append(args, "-e", pattern)

	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Dir = dir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return nil, nil // no matches — not an error
		}
		return nil, fmt.Errorf("git grep: %w: %s", err, strings.TrimSpace(stderr.String()))
	}

	return parseGrep(stdout.String(), filesOnly), nil
}

// parseGrep turns `git grep` stdout into matches. Line mode rows are
// "path:line:text" (split on the first two colons so colons in the text — URLs,
// timestamps — survive); files-only rows are bare paths.
func parseGrep(out string, filesOnly bool) []Match {
	var matches []Match
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		if filesOnly {
			matches = append(matches, Match{File: line})
			continue
		}
		parts := strings.SplitN(line, ":", 3)
		if len(parts) < 3 {
			continue // malformed — skip defensively
		}
		n, err := strconv.Atoi(parts[1])
		if err != nil {
			continue
		}
		matches = append(matches, Match{File: parts[0], Line: n, Text: parts[2]})
	}
	return matches
}

// IsGitRepo returns true if dir is inside a git repository.
func IsGitRepo(ctx context.Context, dir string) bool {
	_, err := Run(ctx, dir, "rev-parse", "--show-toplevel")
	return err == nil
}

// Toplevel returns the absolute path to the root of the working tree that dir
// belongs to. For a linked worktree this is the worktree's own root, not the
// main checkout.
func Toplevel(ctx context.Context, dir string) (string, error) {
	return Run(ctx, dir, "rev-parse", "--show-toplevel")
}

// Superproject returns the working-tree root of the superproject when dir is
// inside a git submodule, or an empty string when it is not. It is used to
// avoid registering submodules as standalone repositories.
func Superproject(ctx context.Context, dir string) string {
	out, err := Run(ctx, dir, "rev-parse", "--show-superproject-working-tree")
	if err != nil {
		return ""
	}
	return out
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

// HasUpstream reports whether the current branch has an upstream tracking
// branch configured. It returns false for a detached HEAD or a branch with no
// tracking information — both cases where git pull has nothing to pull from.
func HasUpstream(ctx context.Context, dir string) bool {
	_, err := Run(ctx, dir, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}")
	return err == nil
}

// Pull runs git pull in the given directory and reports whether it advanced the
// working tree. By default it passes --ff-only so it never creates a merge
// commit and fails fast on diverged branches; pass rebase=true to use --rebase
// instead. The updated flag is derived by comparing HEAD before and after, so an
// already up-to-date repo returns (false, nil).
func Pull(ctx context.Context, dir string, rebase bool) (bool, error) {
	before, _ := Run(ctx, dir, "rev-parse", "HEAD")

	args := []string{"pull"}
	if rebase {
		args = append(args, "--rebase")
	} else {
		args = append(args, "--ff-only")
	}
	if _, err := Run(ctx, dir, args...); err != nil {
		return false, err
	}

	after, _ := Run(ctx, dir, "rev-parse", "HEAD")
	return before != after, nil
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

// WorktreeInfo holds metadata about a single git worktree.
type WorktreeInfo struct {
	Path   string
	Branch string
}

// WorktreeList returns all linked worktrees for the repo at dir.
// The main working tree is excluded from the results.
func WorktreeList(ctx context.Context, dir string) ([]WorktreeInfo, error) {
	out, err := Run(ctx, dir, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}

	var worktrees []WorktreeInfo
	var current WorktreeInfo
	isFirst := true

	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			if !isFirst && current.Path != "" {
				worktrees = append(worktrees, current)
			}
			isFirst = false
			current = WorktreeInfo{Path: strings.TrimPrefix(line, "worktree ")}
		case strings.HasPrefix(line, "branch "):
			ref := strings.TrimPrefix(line, "branch ")
			// Strip refs/heads/ prefix to get the short branch name.
			current.Branch = strings.TrimPrefix(ref, "refs/heads/")
		case line == "bare":
			current.Branch = "(bare)"
		case line == "detached":
			current.Branch = "(detached)"
		}
	}
	// Append last entry.
	if current.Path != "" && !isFirst {
		worktrees = append(worktrees, current)
	}

	// The first entry is always the main worktree — skip it.
	if len(worktrees) > 1 {
		return worktrees[1:], nil
	}
	return nil, nil
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
