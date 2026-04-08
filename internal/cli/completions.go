package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/CelikE/soko/internal/config"
)

// repoNameCompletionFunc returns a completion function that completes
// registered repo names. It fails silently if the config can't be loaded.
func repoNameCompletionFunc() cobra.CompletionFunc {
	return func(_ *cobra.Command, _ []string, _ string) ([]cobra.Completion, cobra.ShellCompDirective) {
		cfg, err := config.Load()
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		names := make([]cobra.Completion, 0, len(cfg.Repos))
		for _, r := range cfg.Repos {
			names = append(names, r.Name)
		}

		return names, cobra.ShellCompDirectiveNoFileComp
	}
}

// tagCompletionFunc returns a completion function that completes known tags.
func tagCompletionFunc() cobra.CompletionFunc {
	return func(_ *cobra.Command, _ []string, _ string) ([]cobra.Completion, cobra.ShellCompDirective) {
		cfg, err := config.Load()
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		tags := config.ListTags(cfg)
		completions := make([]cobra.Completion, len(tags))
		copy(completions, tags)

		return completions, cobra.ShellCompDirectiveNoFileComp
	}
}

// findReposMatching returns repos matching any of the given queries.
// Each query tries exact match first, then prefix match — same as config.FindRepo.
func findReposMatching(repos []config.RepoEntry, queries []string) []config.RepoEntry {
	seen := make(map[string]bool)
	var matched []config.RepoEntry

	for _, q := range queries {
		// Exact match first.
		for _, r := range repos {
			if r.Name == q && !seen[r.Path] {
				seen[r.Path] = true
				matched = append(matched, r)
			}
		}
		if seen[q] || len(matched) > 0 && matched[len(matched)-1].Name == q {
			continue
		}
		// Prefix match fallback.
		for _, r := range repos {
			if strings.HasPrefix(r.Name, q) && !seen[r.Path] {
				seen[r.Path] = true
				matched = append(matched, r)
			}
		}
	}

	return matched
}

// loadReposWithTagFilter loads the config and applies --tag filtering if set.
func loadReposWithTagFilter(cmd *cobra.Command) (*config.Config, []config.RepoEntry, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, nil, fmt.Errorf("loading config: %w", err)
	}

	repos := cfg.Repos
	tags, _ := cmd.Flags().GetStringSlice("tag")
	if len(tags) > 0 {
		repos = config.FilterByTags(repos, tags)
	}

	return cfg, repos, nil
}
