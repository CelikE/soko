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
		Long: `Remove repos from the registry without deleting their directories.
Linked worktrees are unregistered alongside a removed parent, including when
removing by path or selecting a subset with --all --select.`,
		Args: cobra.MaximumNArgs(1),
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
	repo, err := config.FindRepoByName(cfg, name)
	if err == nil {
		return removeEntries(cfg, []config.RepoEntry{*repo}, jsonOut, w)
	}
	if !errors.Is(err, config.ErrRepoNotFound) {
		return fmt.Errorf("removing repo: %w; use --path to remove a specific entry", err)
	}
	if s := suggestRepoNames(cfg.Repos, name); len(s) > 0 {
		return fmt.Errorf("not found: %s — did you mean: %s?", name, strings.Join(s, ", "))
	}
	return fmt.Errorf("not found: %s", name)
}

func removeByPath(cfg *config.Config, path string, jsonOut bool, w io.Writer) error {
	repo, err := config.FindRepoByPath(cfg, path)
	if err != nil {
		if errors.Is(err, config.ErrRepoNotFound) {
			return fmt.Errorf("not found: %s", path)
		}
		return fmt.Errorf("removing repo: %w", err)
	}

	return removeEntries(cfg, []config.RepoEntry{*repo}, jsonOut, w)
}

func removeEntries(cfg *config.Config, entries []config.RepoEntry, jsonOut bool, w io.Writer) error {
	removed := registryRemovalTargets(cfg, entries)
	removeRegistryTargets(cfg, removed)

	if err := config.Save(cfg); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	if jsonOut {
		return writeRemovedJSON(w, removed)
	}

	for _, repo := range removed {
		output.Confirm(w, fmt.Sprintf("removed %s (%s)", repo.Name, repo.Path))
	}
	return nil
}

func removeAll(cmd *cobra.Command, cfg *config.Config, force, jsonOut, selectFlag bool, w io.Writer) error {
	count := len(cfg.Repos)
	if count == 0 {
		output.Info(w, "no repos registered")
		return nil
	}

	// Pick initial targets; removing a parent also unregisters its worktrees.
	targets := cfg.Repos
	if selectFlag {
		chosen, ok := selectRepos(cmd, "Select repos to remove from the registry:", cfg.Repos)
		if !ok {
			_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "aborted")
			return nil
		}
		targets = chosen
	}

	return removeSelectedRepos(cmd, cfg, targets, force, jsonOut, w)
}

func removeSelectedRepos(cmd *cobra.Command, cfg *config.Config, selected []config.RepoEntry, force, jsonOut bool, w io.Writer) error {
	count := len(cfg.Repos)
	// Expand before confirmation so the prompt counts every affected entry,
	// including worktrees deselected while their parent remains selected.
	targets := registryRemovalTargets(cfg, selected)

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
		removeRegistryTargets(cfg, targets)
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
