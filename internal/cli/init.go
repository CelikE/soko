package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/CelikE/soko/internal/config"
	"github.com/CelikE/soko/internal/git"
)

// newInitCmd creates the cobra command for soko init.
func newInitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Register the current repo with soko",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()

			dir, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("getting working directory: %w", err)
			}

			if !git.IsGitRepo(ctx, dir) {
				_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "error: not a git repository")
				os.Exit(1)
			}

			name := git.RepoName(ctx, dir)
			tags, _ := cmd.Flags().GetStringSlice("tag")

			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			entry := config.RepoEntry{
				Name: name,
				Path: dir,
				Tags: tags,
			}

			cfg, err = config.AddRepo(cfg, entry)
			if err != nil {
				if errors.Is(err, config.ErrRepoAlreadyExists) {
					_, _ = fmt.Fprintf(cmd.OutOrStdout(), "already registered: %s (%s)\n", name, dir)
					return nil
				}
				return fmt.Errorf("adding repo: %w", err)
			}

			if err := config.Save(cfg); err != nil {
				return fmt.Errorf("saving config: %w", err)
			}

			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "registered: %s (%s)\n", name, dir)
			return nil
		},
	}

	cmd.Flags().StringSlice("tag", nil, "tags to apply to the repo (can be repeated)")

	return cmd
}
