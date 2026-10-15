package cli

import "github.com/CelikE/soko/internal/config"

// entryKey identifies a registry entry by name, path, and worktree parent so
// duplicate paths (only reachable via a hand-edited config) are not confused.
func entryKey(e *config.RepoEntry) string {
	return e.Name + "\x00" + e.Path + "\x00" + e.WorktreeOf
}

// registryRemovalTargets includes linked worktrees that would be orphaned by
// removing the selected entries. It de-duplicates entries, preserves discovery
// order, and does not mutate cfg.
func registryRemovalTargets(cfg *config.Config, selected []config.RepoEntry) []config.RepoEntry {
	seen := make(map[string]bool, len(selected))
	var targets []config.RepoEntry

	add := func(e config.RepoEntry) {
		if k := entryKey(&e); !seen[k] {
			seen[k] = true
			targets = append(targets, e)
		}
	}

	for _, r := range selected {
		add(r)
		for _, wt := range config.FindWorktrees(cfg, r.Name) {
			add(wt)
		}
	}

	return targets
}

// removeRegistryTargets removes the given entries from cfg by identity.
func removeRegistryTargets(cfg *config.Config, targets []config.RepoEntry) {
	drop := make(map[string]bool, len(targets))
	for _, t := range targets {
		drop[entryKey(&t)] = true
	}

	kept := make([]config.RepoEntry, 0, len(cfg.Repos))
	for _, e := range cfg.Repos {
		if !drop[entryKey(&e)] {
			kept = append(kept, e)
		}
	}
	cfg.Repos = kept
}
