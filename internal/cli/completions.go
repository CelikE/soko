package cli

import (
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
