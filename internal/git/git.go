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
	out, err := run(ctx, dir, args...)
	return strings.TrimSpace(out), err
}

// runPath removes only Git's line terminator, preserving whitespace in paths.
func runPath(ctx context.Context, dir string, args ...string) (string, error) {
	out, err := run(ctx, dir, args...)
	return strings.TrimSuffix(out, "\n"), err
}

func run(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Dir = dir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// Format: "git <args>: <exit error>: <stderr output>"
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}

	return stdout.String(), nil
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
	args := []string{"grep", "--color=never", "--null"}
	if filesOnly {
		args = append(args, "-l")
	} else {
		// Binary summaries have no source line or NUL delimiter. Skip them
		// in line mode so they cannot become part of the next match's path.
		args = append(args, "--line-number", "-I")
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

// parseGrep turns `git grep --null` stdout into matches. Line mode records are
// "path\x00line\x00text\n"; files-only records are "path\x00". Read each path
// through its NUL terminator before splitting the row, since paths can contain
// colons and newlines and are never quoted in this format.
func parseGrep(out string, filesOnly bool) []Match {
	var matches []Match
	for out != "" {
		file, rest, ok := strings.Cut(out, "\x00")
		if !ok {
			break // incomplete record
		}
		out = rest
		if filesOnly {
			if file != "" {
				matches = append(matches, Match{File: file})
			}
			continue
		}
		var row string
		row, out, _ = strings.Cut(out, "\n")
		line, text, ok := strings.Cut(row, "\x00")
		if !ok || file == "" {
			continue // malformed — skip defensively
		}
		n, err := strconv.Atoi(line)
		if err != nil {
			continue
		}
		matches = append(matches, Match{File: file, Line: n, Text: text})
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
	return runPath(ctx, dir, "rev-parse", "--show-toplevel")
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
	_, err := UpstreamBranch(ctx, dir)
	return err == nil
}

// CurrentBranch returns the short name of the checked-out branch. On a
// detached HEAD it returns the commit SHA with detached=true, so callers can
// restore the exact position later via `checkout --detach`.
func CurrentBranch(ctx context.Context, dir string) (name string, detached bool, err error) {
	name, err = Run(ctx, dir, "symbolic-ref", "--short", "-q", "HEAD")
	if err == nil && name != "" {
		return name, false, nil
	}
	sha, shaErr := Run(ctx, dir, "rev-parse", "HEAD")
	if shaErr != nil {
		return "", false, fmt.Errorf("resolving HEAD: %w", shaErr)
	}
	return sha, true, nil
}

// UpstreamBranch resolves the current branch's upstream tracking ref and
// returns its short name (e.g. "origin/main"). It returns an error when there
// is no upstream — a detached HEAD or a branch never pushed. This is the same
// ref lookup that backs HasUpstream, kept as a single source of truth.
func UpstreamBranch(ctx context.Context, dir string) (string, error) {
	return Run(ctx, dir, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}")
}

// Remotes runs `git remote -v` and returns a map of remote name to fetch URL.
// Only the (fetch) rows are kept; push and fetch URLs usually match and tracking
// resolves against the fetch URL. On success a repo with no remotes yields an
// empty, non-nil map, so callers need no nil guard.
func Remotes(ctx context.Context, dir string) (map[string]string, error) {
	out, err := Run(ctx, dir, "remote", "-v")
	if err != nil {
		return nil, err
	}

	remotes := make(map[string]string)
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		// Lines look like: "origin\tgit@github.com:acme/api.git (fetch)". The
		// name is tab-separated from the rest, and the URL is everything before
		// the trailing " (fetch)" — split on the tab and trim the suffix rather
		// than on whitespace, so a URL containing spaces survives intact.
		name, rest, ok := strings.Cut(line, "\t")
		if !ok {
			continue
		}
		url, ok := strings.CutSuffix(rest, " (fetch)")
		if !ok {
			continue // a (push) row or malformed — skip
		}
		remotes[name] = url
	}
	return remotes, nil
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
	gitDir, err := runPath(ctx, dir, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return false
	}
	commonDir, err := CommonDir(ctx, dir)
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
	if !IsWorktree(ctx, dir) {
		if root, err := Toplevel(ctx, dir); err == nil {
			return root, nil
		}
	}
	entries, err := AllWorktrees(ctx, dir)
	if err != nil {
		return "", err
	}
	if len(entries) == 0 {
		return "", fmt.Errorf("repository has no primary worktree")
	}
	if entries[0].Bare {
		return "", fmt.Errorf("bare repository bases are not supported")
	}
	if root, err := Toplevel(ctx, entries[0].Path); err == nil && !IsWorktree(ctx, root) {
		return root, nil
	}
	// A separate Git directory may not retain a reverse checkout pointer.
	if path, err := Run(ctx, dir, "config", "--path", "--get", "core.worktree"); err == nil {
		if !filepath.IsAbs(path) {
			common, commonErr := CommonDir(ctx, dir)
			if commonErr != nil {
				return "", commonErr
			}
			path = filepath.Join(common, path)
		}
		if root, rootErr := Toplevel(ctx, path); rootErr == nil {
			return root, nil
		}
	}
	return "", fmt.Errorf("primary checkout cannot be resolved — register the base checkout first")
}

// CommonDir returns the absolute shared Git directory identifying a repository.
func CommonDir(ctx context.Context, dir string) (string, error) {
	return runPath(ctx, dir, "rev-parse", "--path-format=absolute", "--git-common-dir")
}

// WorktreeInfo holds metadata about a single git worktree.
type WorktreeInfo struct {
	Path        string
	HEAD        string
	Branch      string
	Bare        bool
	Detached    bool
	Locked      bool
	LockReason  string
	Prunable    bool
	PruneReason string
}

// WorktreeList returns all linked worktrees for the repo at dir.
// The main working tree is excluded from the results.
func WorktreeList(ctx context.Context, dir string) ([]WorktreeInfo, error) {
	entries, err := AllWorktrees(ctx, dir)
	if err != nil {
		return nil, err
	}
	if len(entries) > 1 {
		return entries[1:], nil
	}
	return nil, nil
}

// AllWorktrees enumerates Git's worktree inventory, including the primary entry.
// NUL delimiters preserve paths containing whitespace, quotes, and newlines.
func AllWorktrees(ctx context.Context, dir string) ([]WorktreeInfo, error) {
	out, err := Run(ctx, dir, "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return nil, err
	}
	return parseWorktrees(out), nil
}

func parseWorktrees(out string) []WorktreeInfo {
	var entries []WorktreeInfo
	var current WorktreeInfo
	for _, field := range strings.Split(out, "\x00") {
		key, value, _ := strings.Cut(field, " ")
		switch key {
		case "worktree":
			if current.Path != "" {
				entries = append(entries, current)
			}
			current = WorktreeInfo{Path: value}
		case "HEAD":
			current.HEAD = value
		case "branch":
			current.Branch = strings.TrimPrefix(value, "refs/heads/")
		case "bare":
			current.Bare = true
		case "detached":
			current.Detached = true
		case "locked":
			current.Locked, current.LockReason = true, value
		case "prunable":
			current.Prunable, current.PruneReason = true, value
		}
	}
	if current.Path != "" {
		entries = append(entries, current)
	}
	return entries
}

// nameFromURL extracts a repository name from a git remote URL. It handles
// both SSH (git@host:user/repo.git) and HTTPS (https://host/user/repo.git)
// formats.
func nameFromURL(rawURL string) string {
	// Local Windows remotes use native path separators instead of URL slashes.
	rawURL = filepath.ToSlash(rawURL)

	// SSH format: git@github.com:user/repo.git
	if idx := strings.LastIndex(rawURL, ":"); idx != -1 && !strings.Contains(rawURL, "://") {
		rawURL = rawURL[idx+1:]
	}

	// Ignore trailing slashes before taking the last path segment.
	name := strings.TrimRight(rawURL, "/")
	if idx := strings.LastIndex(name, "/"); idx != -1 {
		name = name[idx+1:]
	}

	// Strip .git suffix.
	name = strings.TrimSuffix(name, ".git")

	return name
}
