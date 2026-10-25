package cli

import (
	"bytes"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/CelikE/soko/internal/config"
)

func TestPruneSelectedRepos(t *testing.T) {
	for _, tt := range []struct {
		name        string
		selected    []int
		wantRemoved []int
		dryRun      bool
		force       bool
		answer      string
	}{
		{name: "parent only", selected: []int{0}, wantRemoved: []int{0, 1, 2}, answer: "y\n"},
		{name: "parent and one worktree", selected: []int{0, 1}, wantRemoved: []int{0, 1, 2}, force: true},
		{name: "worktree only", selected: []int{1}, wantRemoved: []int{1}, answer: "y\n"},
		{name: "dry run cascade", selected: []int{0}, wantRemoved: []int{0, 1, 2}, dryRun: true},
		{name: "decline cascade", selected: []int{0}, wantRemoved: []int{0, 1, 2}, answer: "n\n"},
		{name: "EOF cascade", selected: []int{0}, wantRemoved: []int{0, 1, 2}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			cfg := &config.Config{Repos: []config.RepoEntry{
				{Name: "api", Path: "/gone/api"},
				{Name: "custom-alias", Path: "/present/feature", WorktreeOf: "api"},
				{Name: "api/fix", Path: "/present/fix", WorktreeOf: "api"},
				{Name: "other", Path: "/present/other", Meta: map[string]string{"note": "keep me"}},
			}}
			if err := config.Save(cfg); err != nil {
				t.Fatal(err)
			}
			// Model a narrowed picker selection, including deselected worktrees.
			selected := mapSelected(cfg.Repos, tt.selected)
			removed := mapSelected(cfg.Repos, tt.wantRemoved)
			want := append([]config.RepoEntry(nil), cfg.Repos...)
			applied := !tt.dryRun && (tt.force || tt.answer == "y\n")
			if applied {
				want = nil
				for i, entry := range cfg.Repos {
					if !slices.Contains(tt.wantRemoved, i) {
						want = append(want, entry)
					}
				}
			}

			var stdout, stderr bytes.Buffer
			cmd := newPruneCmd()
			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)
			cmd.SetIn(strings.NewReader(tt.answer))
			if err := pruneSelectedRepos(cmd, cfg, selected, tt.dryRun, tt.force, false); err != nil {
				t.Fatal(err)
			}
			for _, entry := range removed {
				if !strings.Contains(stdout.String(), entry.Path) {
					t.Errorf("preview = %q, want affected path %q", stdout.String(), entry.Path)
				}
			}
			if !tt.dryRun && !tt.force {
				if !strings.Contains(stderr.String(), fmt.Sprintf("remove %d missing", len(removed))) {
					t.Errorf("prompt = %q, want expanded removal count %d", stderr.String(), len(removed))
				}
			} else if stderr.Len() != 0 {
				t.Errorf("stderr = %q, want no confirmation prompt", stderr.String())
			}
			saved, err := config.Load()
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(saved.Repos, want) {
				t.Errorf("registry = %+v, want %+v", saved.Repos, want)
			}
			if applied {
				if !strings.Contains(stdout.String(), fmt.Sprintf("pruned %d missing", len(removed))) {
					t.Errorf("output = %q, want expanded removal count %d", stdout.String(), len(removed))
				}
			} else if strings.Contains(stdout.String(), "pruned") {
				t.Errorf("output = %q, must not claim removal", stdout.String())
			}
		})
	}
}

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

	targets := registryRemovalTargets(cfg, missing)
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

	removeRegistryTargets(cfg, targets)
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

	removeRegistryTargets(cfg, targets)
	if len(cfg.Repos) != 1 {
		t.Fatalf("remaining = %d, want 1", len(cfg.Repos))
	}
	if cfg.Repos[0].Name != "keepme" {
		t.Errorf("remaining = %q, want 'keepme' (identity match, not first path match)", cfg.Repos[0].Name)
	}
}
