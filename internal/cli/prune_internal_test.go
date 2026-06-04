package cli

import (
	"testing"

	"github.com/CelikE/soko/internal/config"
)

// TestPruneTargets_CascadesWorktrees verifies that removing a missing parent
// repo also targets its linked worktree entries, even when the worktree's own
// directory still exists — so no dangling worktree_of reference is left behind.
func TestPruneTargets_CascadesWorktrees(t *testing.T) {
	cfg := &config.Config{Repos: []config.RepoEntry{
		{Name: "parent", Path: "/gone/parent"},
		{Name: "parent/feature", Path: "/present/parent-wt", WorktreeOf: "parent"},
		{Name: "other", Path: "/present/other"},
	}}

	// Only the parent's directory is missing; the worktree dir survives.
	missing := []config.RepoEntry{cfg.Repos[0]}

	targets := pruneTargets(cfg, missing)
	if len(targets) != 2 {
		t.Fatalf("targets = %d, want 2 (parent + cascaded worktree)", len(targets))
	}

	names := map[string]bool{}
	for _, tg := range targets {
		names[tg.Name] = true
	}
	if !names["parent"] || !names["parent/feature"] {
		t.Errorf("targets = %v, want 'parent' and 'parent/feature'", names)
	}

	removePruneTargets(cfg, targets)
	if len(cfg.Repos) != 1 || cfg.Repos[0].Name != "other" {
		t.Errorf("remaining = %+v, want only 'other'", cfg.Repos)
	}
}

// TestRemovePruneTargets_RemovesByIdentity verifies that removal matches the
// specific identified entry rather than the first entry sharing its path —
// guarding against destroying an out-of-scope duplicate-path sibling (only
// reachable via a hand-edited config).
func TestRemovePruneTargets_RemovesByIdentity(t *testing.T) {
	cfg := &config.Config{Repos: []config.RepoEntry{
		{Name: "keepme", Path: "/dup/path"},
		{Name: "removeme", Path: "/dup/path", Tags: []string{"work"}},
	}}

	// The tagged second entry is the identified prune target.
	targets := []config.RepoEntry{cfg.Repos[1]}

	removePruneTargets(cfg, targets)
	if len(cfg.Repos) != 1 {
		t.Fatalf("remaining = %d, want 1", len(cfg.Repos))
	}
	if cfg.Repos[0].Name != "keepme" {
		t.Errorf("remaining = %q, want 'keepme' (identity match, not first path match)", cfg.Repos[0].Name)
	}
}
