package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/CelikE/soko/internal/config"
	"github.com/CelikE/soko/internal/worktree"
)

func addFamilyFlag(cmd *cobra.Command) {
	cmd.Flags().StringP("repo", "r", "", "limit to a base repo and its worktrees (use . for the current family)")
	_ = cmd.RegisterFlagCompletionFunc("repo", repoNameCompletionFunc())
}

func addGroupingFlag(cmd *cobra.Command) {
	cmd.Flags().String("group-by", "none", "group by none, tag, or repo")
	_ = cmd.RegisterFlagCompletionFunc("group-by", func(_ *cobra.Command, _ []string, _ string) ([]cobra.Completion, cobra.ShellCompDirective) {
		return []cobra.Completion{"none", "tag", "repo"}, cobra.ShellCompDirectiveNoFileComp
	})
}

func groupingMode(cmd *cobra.Command) (string, error) {
	mode, _ := cmd.Flags().GetString("group-by")
	legacy, _ := cmd.Flags().GetBool("group")
	if legacy {
		if cmd.Flags().Changed("group-by") && mode != "tag" {
			return "", fmt.Errorf("--group cannot be combined with --group-by %s", mode)
		}
		mode = "tag"
	}
	switch mode {
	case "none", "tag", "repo":
		return mode, nil
	default:
		return "", fmt.Errorf("invalid --group-by %q: use none, tag, or repo", mode)
	}
}

func filterRepoFamily(cmd *cobra.Command, cfg *config.Config, repos []config.RepoEntry) ([]config.RepoEntry, error) {
	query, _ := cmd.Flags().GetString("repo")
	if query == "" {
		return repos, nil
	}
	if query == "." {
		dir, err := os.Getwd()
		if err != nil {
			return nil, err
		}
		base, err := worktree.BasePath(cmd.Context(), cfg, dir)
		if err != nil {
			return nil, fmt.Errorf("resolving current repository family: %w", err)
		}
		entry := worktree.FindByPath(cfg, base)
		if entry == nil {
			return nil, fmt.Errorf("current repository family is not registered — run soko init --worktree")
		}
		query = entry.Name
	}
	parent, err := resolveWorktreeParent(cfg, query)
	if err != nil {
		return nil, err
	}
	var selected []config.RepoEntry
	for i := range repos {
		if config.RepoFamily(&repos[i]) == parent.Name {
			selected = append(selected, repos[i])
		}
	}
	return selected, nil
}

func selectWorktreeParents(cmd *cobra.Command, cfg *config.Config, repos []config.RepoEntry, queries []string) ([]config.RepoEntry, error) {
	if len(queries) == 0 {
		return config.FilterNoWorktrees(repos), nil
	}
	var parents []config.RepoEntry
	seen := make(map[string]bool)
	for _, query := range queries {
		parent, err := resolveWorktreeParent(cfg, query)
		if err != nil {
			return nil, err
		}
		eligible := false
		for i := range repos {
			if config.RepoFamily(&repos[i]) == parent.Name {
				eligible = true
				break
			}
		}
		if !eligible {
			return nil, fmt.Errorf("repo %q does not match the supplied filters", query)
		}
		if !seen[parent.Path] {
			seen[parent.Path] = true
			parents = append(parents, *parent)
		}
	}
	return parents, nil
}
