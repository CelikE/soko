package cli

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/CelikE/soko/internal/config"
	"github.com/CelikE/soko/internal/output"
)

// newPruneCmd creates the cobra command for soko prune.
func newPruneCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "prune",
		Short: "Remove repos whose directories no longer exist",
		Long: `Find registered repos whose paths no longer exist on disk — for example
local directories you have deleted — and remove them from the registry.
Linked worktrees of a pruned repo are removed alongside it.

Use --dry-run to preview what would be pruned.`,
		Example: `  soko prune --dry-run            # preview repos that would be pruned
  soko prune                      # remove with confirmation
  soko prune --force              # remove without confirmation
  soko prune --tag work           # only prune within tagged repos
  soko prune --dry-run --json     # machine-readable preview`,
		// prune deliberately takes no positional repo args: it acts on whatever
		// registry entries are missing on disk. Scope with --tag if needed.
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			w := cmd.OutOrStdout()

			cfg, repos, err := loadReposWithTagFilter(cmd)
			if err != nil {
				return err
			}

			tags, _ := cmd.Flags().GetStringSlice("tag")
			dryRun, _ := cmd.Flags().GetBool("dry-run")
			force, _ := cmd.Flags().GetBool("force")
			jsonFlag, _ := cmd.Flags().GetBool("json")
			selectFlag, _ := cmd.Flags().GetBool("select")

			if jsonFlag && selectFlag {
				return fmt.Errorf("--select cannot be combined with --json")
			}
			// Without --force or --dry-run, prune prompts interactively, which
			// would corrupt JSON output for machine consumers.
			if jsonFlag && !force && !dryRun {
				return fmt.Errorf("--json requires --force or --dry-run")
			}

			if len(cfg.Repos) == 0 {
				if jsonFlag {
					_, _ = fmt.Fprintln(w, "[]")
					return nil
				}
				output.Info(w, noReposMessage(0))
				return nil
			}

			// Targets = missing repos plus any worktrees orphaned by removing
			// them, so no dangling worktree_of reference is left behind.
			targets := pruneTargets(cfg, findMissingRepos(repos))

			if len(targets) == 0 {
				if jsonFlag {
					_, _ = fmt.Fprintln(w, "[]")
					return nil
				}
				if len(tags) > 0 {
					output.Info(w, "no missing repos match the tag filter")
				} else {
					output.Info(w, "all registered repos exist — nothing to prune")
				}
				return nil
			}

			// Optional interactive refinement before removing anything.
			if selectFlag {
				chosen, ok := selectRepos(cmd, "Select repos to prune (space toggles, enter confirms):", targets)
				if !ok {
					_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "  aborted")
					return nil
				}
				targets = chosen
			}

			if !jsonFlag {
				renderPruneTable(w, targets)
			}

			if dryRun {
				if jsonFlag {
					return writePrunedJSON(w, targets)
				}
				return nil
			}

			// Confirm unless --force.
			if !force {
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(),
					"\n  remove %d missing %s from the registry? [y/N] ",
					len(targets), output.Plural(len(targets), "repo"))

				scanner := bufio.NewScanner(cmd.InOrStdin())
				if !scanner.Scan() {
					return nil
				}
				answer := strings.TrimSpace(strings.ToLower(scanner.Text()))
				if answer != "y" && answer != "yes" {
					_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "  aborted")
					return nil
				}
			}

			removePruneTargets(cfg, targets)

			if err := config.Save(cfg); err != nil {
				return fmt.Errorf("saving config: %w", err)
			}

			if jsonFlag {
				return writePrunedJSON(w, targets)
			}

			_, _ = fmt.Fprintln(w)
			output.Confirm(w, fmt.Sprintf("pruned %d missing %s",
				len(targets), output.Plural(len(targets), "repo")))
			return nil
		},
	}

	cmd.Flags().Bool("dry-run", false, "show repos that would be pruned without removing them")
	cmd.Flags().Bool("force", false, "skip confirmation prompt")
	cmd.Flags().StringSlice("tag", nil, "filter by tag (can be repeated, combines with OR)")
	_ = cmd.RegisterFlagCompletionFunc("tag", tagCompletionFunc())
	addSelectFlag(cmd)

	return cmd
}

// findMissingRepos returns the entries whose paths no longer exist on disk.
func findMissingRepos(repos []config.RepoEntry) []config.RepoEntry {
	var missing []config.RepoEntry
	for _, r := range repos {
		if !pathExists(r.Path) {
			missing = append(missing, r)
		}
	}
	return missing
}

// entryKey identifies a registry entry by name, path, and worktree parent so
// duplicate paths (only reachable via a hand-edited config) are not confused.
func entryKey(e *config.RepoEntry) string {
	return e.Name + "\x00" + e.Path + "\x00" + e.WorktreeOf
}

// pruneTargets returns the entries to remove: every missing repo plus any
// linked worktrees that would be orphaned by removing it. The result is
// de-duplicated and preserves discovery order. It does not mutate cfg.
func pruneTargets(cfg *config.Config, missing []config.RepoEntry) []config.RepoEntry {
	seen := make(map[string]bool, len(missing))
	var targets []config.RepoEntry

	add := func(e config.RepoEntry) {
		if k := entryKey(&e); !seen[k] {
			seen[k] = true
			targets = append(targets, e)
		}
	}

	for _, r := range missing {
		add(r)
		// Cascade to linked worktrees so no entry is left pointing at a parent
		// that no longer exists in the registry — mirrors soko remove.
		for _, wt := range config.FindWorktrees(cfg, r.Name) {
			add(wt)
		}
	}

	return targets
}

// removePruneTargets removes the given entries from cfg by identity.
func removePruneTargets(cfg *config.Config, targets []config.RepoEntry) {
	drop := make(map[string]bool, len(targets))
	for _, t := range targets {
		drop[entryKey(&t)] = true
	}

	kept := make([]config.RepoEntry, 0, len(cfg.Repos))
	for _, e := range cfg.Repos {
		if drop[entryKey(&e)] {
			continue
		}
		kept = append(kept, e)
	}
	cfg.Repos = kept
}

// renderPruneTable prints the repos that will be pruned along with a summary.
func renderPruneTable(w io.Writer, targets []config.RepoEntry) {
	nameWidth := len("REPO")
	for _, r := range targets {
		if len(r.Name) > nameWidth {
			nameWidth = len(r.Name)
		}
	}
	nameWidth += 2

	header := fmt.Sprintf("  %-*s %s", nameWidth, "REPO", "PATH")
	_, _ = fmt.Fprintln(w, output.Dim(header))
	_, _ = fmt.Fprintln(w, output.Dim("  "+strings.Repeat("─", len(header)-2)))

	for _, r := range targets {
		pathStr := output.Dim(r.Path)
		if r.WorktreeOf != "" {
			pathStr += output.Dim("  → " + r.WorktreeOf)
		}
		_, _ = fmt.Fprintf(w, "  %-*s %s\n", nameWidth, r.Name, pathStr)
	}

	_, _ = fmt.Fprintf(w, "\n  %s\n", output.Dim(fmt.Sprintf(
		"%d missing %s", len(targets), output.Plural(len(targets), "repo"))))
}

// writePrunedJSON renders the pruned (or to-be-pruned) entries as JSON.
func writePrunedJSON(w io.Writer, entries []config.RepoEntry) error {
	out := make([]listEntry, len(entries))
	for i, e := range entries {
		out[i] = listEntry{Name: e.Name, Path: e.Path, Tags: e.Tags, WorktreeOf: e.WorktreeOf}
	}
	return output.RenderJSON(w, out)
}
