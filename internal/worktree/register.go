// Package worktree reconciles Git checkouts with soko's repository registry.
package worktree

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/CelikE/soko/internal/config"
	"github.com/CelikE/soko/internal/git"
)

// Registration describes a registry update. Existing entries retain their metadata.
type Registration struct {
	Entry       config.RepoEntry
	Parent      config.RepoEntry
	ParentAdded bool
	Existing    bool
}

// CanonicalPath resolves absolute paths and existing symlinks consistently.
func CanonicalPath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved
	}
	return abs
}

// FindByPath finds an existing checkout through its canonical path.
func FindByPath(cfg *config.Config, path string) *config.RepoEntry {
	path = CanonicalPath(path)
	for i := range cfg.Repos {
		if CanonicalPath(cfg.Repos[i].Path) == path {
			return &cfg.Repos[i]
		}
	}
	return nil
}

// Register adds a checkout and its base, resolving directories through Git.
// With linked=false a linked checkout registers only its primary checkout.
// Persistence is left to the caller, allowing a batch or dry-run plan.
func Register(ctx context.Context, cfg *config.Config, dir string, tags []string, linked bool) (Registration, error) {
	root, err := git.Toplevel(ctx, dir)
	if err != nil {
		return Registration{}, fmt.Errorf("not a git working tree: %w", err)
	}
	root = CanonicalPath(root)
	main, err := BasePath(ctx, cfg, root)
	if err != nil {
		return Registration{}, fmt.Errorf("resolving base repository: %w", err)
	}
	main = CanonicalPath(main)
	if !linked || root == main {
		return registerBase(ctx, cfg, main, tags)
	}
	parent, err := registerBase(ctx, cfg, main, tags)
	if err != nil {
		return Registration{}, err
	}
	result := Registration{Parent: parent.Entry, ParentAdded: !parent.Existing}
	if entry := FindByPath(cfg, root); entry != nil {
		result.Entry, result.Existing = *entry, true
		return result, nil
	}
	branch, detached, err := git.CurrentBranch(ctx, root)
	if err != nil {
		return Registration{}, fmt.Errorf("resolving worktree branch: %w", err)
	}
	if detached {
		branch = filepath.Base(root)
	}
	result.Entry = config.RepoEntry{
		Name: config.UniqueRepoName(cfg, parent.Entry.Name+"/"+branch),
		Path: root, Tags: tags, WorktreeOf: parent.Entry.Name,
	}
	_, err = config.AddRepo(cfg, &result.Entry)
	return result, err
}

// BasePath uses Git's primary checkout when available, then resolves a known
// base by shared Git identity for separate-Git-directory layouts.
func BasePath(ctx context.Context, cfg *config.Config, dir string) (string, error) {
	main, resolveErr := git.MainRepoPath(ctx, dir)
	if resolveErr == nil {
		return main, nil
	}
	common, err := git.CommonDir(ctx, dir)
	if err != nil {
		return "", err
	}
	common = CanonicalPath(common)
	for i := range cfg.Repos {
		entry := &cfg.Repos[i]
		if entry.IsWorktreeEntry() {
			continue
		}
		registeredCommon, err := git.CommonDir(ctx, entry.Path)
		if err == nil && CanonicalPath(registeredCommon) == common {
			if _, err := git.Toplevel(ctx, entry.Path); err != nil {
				return "", fmt.Errorf("registered base has no working tree: %w", err)
			}
			return entry.Path, nil
		}
	}
	return "", resolveErr
}

func registerBase(ctx context.Context, cfg *config.Config, path string, tags []string) (Registration, error) {
	if entry := FindByPath(cfg, path); entry != nil {
		if entry.WorktreeOf != "" {
			return Registration{}, fmt.Errorf("base checkout %s is registered as a linked worktree", path)
		}
		if matches := config.FindRepo(cfg, entry.Name); len(matches) != 1 {
			return Registration{}, fmt.Errorf("base alias %q is ambiguous — repair duplicate names in soko config", entry.Name)
		}
		return Registration{Entry: *entry, Existing: true}, nil
	}
	entry := config.RepoEntry{Name: config.UniqueRepoName(cfg, git.RepoName(ctx, path)), Path: path, Tags: tags}
	_, err := config.AddRepo(cfg, &entry)
	return Registration{Entry: entry}, err
}
