package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/CelikE/soko/internal/config"
	"github.com/CelikE/soko/internal/output"
)

// newRemoveCmd creates the cobra command for soko remove.
func newRemoveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remove [name]",
		Short: "Remove a repo from the registry",
		RunE: func(cmd *cobra.Command, args []string) error {
			w := cmd.OutOrStdout()

			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			allFlag, _ := cmd.Flags().GetBool("all")
			forceFlag, _ := cmd.Flags().GetBool("force")
			pathFlag, _ := cmd.Flags().GetString("path")
			jsonFlag, _ := cmd.Flags().GetBool("json")
			selectFlag, _ := cmd.Flags().GetBool("select")

			if jsonFlag && selectFlag {
				return fmt.Errorf("--select cannot be combined with --json")
			}

			if allFlag {
				return removeAll(cmd, cfg, forceFlag, jsonFlag, selectFlag, w)
			}

			if pathFlag != "" {
				return removeByPath(cfg, pathFlag, jsonFlag, w)
			}

			if len(args) == 0 {
				return cmd.Usage()
			}

			return removeByName(cfg, args[0], jsonFlag, w)
		},
	}

	cmd.Flags().String("path", "", "remove by absolute path instead of name")
	cmd.Flags().Bool("all", false, "remove all registered repos")
	cmd.Flags().Bool("force", false, "skip confirmation prompt")
	addSelectFlag(cmd)
	cmd.ValidArgsFunction = repoNameCompletionFunc()

	return cmd
}

func removeByName(cfg *config.Config, name string, jsonOut bool, w io.Writer) error {
	// Check for linked worktrees that would be orphaned.
	worktrees := config.FindWorktrees(cfg, name)

	cfg, removed, err := config.RemoveRepo(cfg, name)
	if err != nil {
		if errors.Is(err, config.ErrRepoNotFound) {
			return fmt.Errorf("not found: %s", name)
		}
		return fmt.Errorf("removing repo: %w", err)
	}

	// Also remove linked worktrees.
	var allRemoved []config.RepoEntry
	allRemoved = append(allRemoved, removed)
	for _, wt := range worktrees {
		cfg, removed, err = config.RemoveRepo(cfg, wt.Name)
		if err == nil {
			allRemoved = append(allRemoved, removed)
		}
	}

	if err := config.Save(cfg); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	if jsonOut {
		return writeRemovedJSON(w, allRemoved)
	}

	output.Confirm(w, fmt.Sprintf("removed %s (%s)", allRemoved[0].Name, allRemoved[0].Path))
	for _, wt := range allRemoved[1:] {
		output.Confirm(w, fmt.Sprintf("removed %s (%s)", wt.Name, wt.Path))
	}
	return nil
}

func removeByPath(cfg *config.Config, path string, jsonOut bool, w io.Writer) error {
	cfg, removed, err := config.RemoveRepoByPath(cfg, path)
	if err != nil {
		if errors.Is(err, config.ErrRepoNotFound) {
			return fmt.Errorf("not found: %s", path)
		}
		return fmt.Errorf("removing repo: %w", err)
	}

	if err := config.Save(cfg); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	if jsonOut {
		return writeRemovedJSON(w, []config.RepoEntry{removed})
	}

	output.Confirm(w, fmt.Sprintf("removed %s (%s)", removed.Name, removed.Path))
	return nil
}

func removeAll(cmd *cobra.Command, cfg *config.Config, force, jsonOut, selectFlag bool, w io.Writer) error {
	count := len(cfg.Repos)
	if count == 0 {
		output.Info(w, "no repos registered")
		return nil
	}

	// Optional interactive refinement: pick exactly which repos to unregister.
	// Narrow-only — the picker can never add a repo that was not registered.
	targets := cfg.Repos
	if selectFlag {
		chosen, ok := selectRepos(cmd, "Select repos to remove from the registry:", cfg.Repos)
		if !ok {
			_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "aborted")
			return nil
		}
		targets = chosen
	}

	if !force {
		prompt := fmt.Sprintf("remove all %d %s? [y/N] ", count, output.Plural(count, "repo"))
		if len(targets) != count {
			prompt = fmt.Sprintf("remove %d selected %s? [y/N] ", len(targets), output.Plural(len(targets), "repo"))
		}
		_, _ = fmt.Fprint(cmd.ErrOrStderr(), prompt)

		scanner := bufio.NewScanner(cmd.InOrStdin())
		if !scanner.Scan() {
			return nil
		}

		answer := strings.TrimSpace(strings.ToLower(scanner.Text()))
		if answer != "y" && answer != "yes" {
			_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "aborted")
			return nil
		}
	}

	removed := make([]config.RepoEntry, len(targets))
	copy(removed, targets)

	if len(targets) == count {
		cfg = config.Clear(cfg)
	} else {
		// Remove only the chosen entries, by path, leaving the rest registered.
		for _, t := range targets {
			cfg, _, _ = config.RemoveRepoByPath(cfg, t.Path)
		}
	}

	if err := config.Save(cfg); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	if jsonOut {
		return writeRemovedJSON(w, removed)
	}

	if len(removed) == count {
		output.Confirm(w, fmt.Sprintf("removed all %d %s", count, output.Plural(count, "repo")))
	} else {
		output.Confirm(w, fmt.Sprintf("removed %d %s", len(removed), output.Plural(len(removed), "repo")))
	}
	return nil
}

func writeRemovedJSON(w io.Writer, entries []config.RepoEntry) error {
	type entry struct {
		Name string `json:"name"`
		Path string `json:"path"`
	}

	out := make([]entry, len(entries))
	for i, e := range entries {
		out[i] = entry{Name: e.Name, Path: e.Path}
	}

	return output.RenderJSON(w, out)
}
