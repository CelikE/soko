package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/CelikE/soko/internal/config"
	"github.com/CelikE/soko/internal/git"
	"github.com/CelikE/soko/internal/output"
)

// newInitCmd creates the cobra command for soko init.
func newInitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Register the current repo with soko",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			w := cmd.OutOrStdout()

			dir, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("getting working directory: %w", err)
			}

			if !git.IsGitRepo(ctx, dir) {
				return fmt.Errorf("not a git repository")
			}

			worktreeFlag, _ := cmd.Flags().GetBool("worktree")
			tags, _ := cmd.Flags().GetStringSlice("tag")

			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			isWt := git.IsWorktree(ctx, dir)

			// If inside a linked worktree without --worktree, resolve to main repo.
			if isWt && !worktreeFlag {
				mainPath, resolveErr := git.MainRepoPath(ctx, dir)
				if resolveErr == nil {
					output.Info(w, fmt.Sprintf("worktree detected — registering main repo at %s", mainPath))
					output.Info(w, "use --worktree to register this worktree instead")
					dir = mainPath
					isWt = false
				}
			}

			name := git.RepoName(ctx, dir)
			var worktreeOf string

			// Register as a worktree entry: name as parent/branch.
			if isWt && worktreeFlag {
				mainPath, resolveErr := git.MainRepoPath(ctx, dir)
				if resolveErr != nil {
					return fmt.Errorf("resolving main repo: %w", resolveErr)
				}

				parentName := git.RepoName(ctx, mainPath)
				branchOut, branchErr := git.Run(ctx, dir, "rev-parse", "--abbrev-ref", "HEAD")
				if branchErr != nil {
					return fmt.Errorf("getting branch: %w", branchErr)
				}

				name = parentName + "/" + branchOut
				worktreeOf = parentName

				// Auto-register parent if not already registered.
				parentEntry := config.RepoEntry{Name: parentName, Path: mainPath, Tags: tags}
				if _, addErr := config.AddRepo(cfg, parentEntry); addErr == nil {
					output.Confirm(w, fmt.Sprintf("registered %s (%s)", parentName, mainPath))
				}
			}

			entry := config.RepoEntry{
				Name:       name,
				Path:       dir,
				Tags:       tags,
				WorktreeOf: worktreeOf,
			}

			cfg, err = config.AddRepo(cfg, entry)
			if err != nil {
				if errors.Is(err, config.ErrRepoAlreadyExists) {
					output.Warn(w, fmt.Sprintf("already registered %s (%s)", name, dir))
					return nil
				}
				return fmt.Errorf("adding repo: %w", err)
			}

			if err := config.Save(cfg); err != nil {
				return fmt.Errorf("saving config: %w", err)
			}

			output.Confirm(w, fmt.Sprintf("registered %s (%s)", name, dir))
			_, _ = fmt.Fprintln(w)
			output.Info(w, shellInitHint())

			return nil
		},
	}

	cmd.Flags().StringSlice("tag", nil, "tags to apply to the repo (can be repeated)")
	_ = cmd.RegisterFlagCompletionFunc("tag", tagCompletionFunc())
	cmd.Flags().Bool("worktree", false, "register as a linked worktree instead of resolving to main repo")

	return cmd
}
