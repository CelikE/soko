package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/CelikE/soko/internal/config"
	"github.com/CelikE/soko/internal/git"
	"github.com/CelikE/soko/internal/output"
	"github.com/CelikE/soko/internal/worktree"
)

func newWorktreeAdoptCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "adopt [repos...]",
		Short: "Register existing Git worktrees of registered base repos",
		Long: `Reconcile registered base repositories with Git's worktree inventory.
Register live, unregistered linked checkouts, including detached worktrees.
Existing names, tags, and metadata are preserved. No Git state is changed.
Without arguments, inspect all registered base repositories.`,
		Args:              cobra.ArbitraryArgs,
		ValidArgsFunction: repoNameCompletionFunc(),
		RunE:              runWorktreeAdopt,
	}
	cmd.Flags().Bool("dry-run", false, "preview registration without saving")
	cmd.Flags().StringSlice("tag", nil, "filter base repositories by tag (repeatable)")
	cmd.Flags().StringSlice("tag-with", nil, "tags to apply to newly adopted worktrees (repeatable)")
	_ = cmd.RegisterFlagCompletionFunc("tag", tagCompletionFunc())
	_ = cmd.RegisterFlagCompletionFunc("tag-with", tagCompletionFunc())
	addFamilyFlag(cmd)
	return cmd
}

func runWorktreeAdopt(cmd *cobra.Command, args []string) error {
	cfg, repos, err := loadReposWithTagFilter(cmd)
	if err != nil {
		return err
	}
	parents, err := selectWorktreeParents(cmd, cfg, repos, args)
	if err != nil {
		return err
	}
	tags, _ := cmd.Flags().GetStringSlice("tag-with")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	jsonFlag, _ := cmd.Flags().GetBool("json")
	results := make([]scanResult, 0)
	var added, failed int
	for _, parent := range parents {
		inventory, invErr := git.WorktreeList(cmd.Context(), parent.Path)
		if invErr != nil {
			failed++
			results = append(results, scanResult{Name: parent.Name, Path: parent.Path, Error: invErr.Error()})
			continue
		}
		for _, wt := range inventory {
			if worktree.FindByPath(cfg, wt.Path) != nil {
				continue
			}
			if !pathExists(wt.Path) {
				// Git may retain missing/locked worktrees on disconnected drives.
				// They cannot be registered as live navigation targets.
				continue
			}
			registration, regErr := worktree.Register(cmd.Context(), cfg, wt.Path, tags, true)
			if regErr != nil {
				failed++
				results = append(results, scanResult{Path: wt.Path, Name: parent.Name, IsWorktree: true, Error: regErr.Error()})
				continue
			}
			added++
			results = append(results, scanResult{Name: registration.Entry.Name, Path: registration.Entry.Path,
				IsWorktree: true, WorktreeOf: registration.Entry.WorktreeOf, Registered: true})
		}
	}
	if !dryRun && added > 0 {
		if err := config.Save(cfg); err != nil {
			return fmt.Errorf("saving adopted worktrees: %w", err)
		}
	}
	w := cmd.OutOrStdout()
	if jsonFlag {
		if err := output.RenderJSON(w, results); err != nil {
			return err
		}
	} else {
		for _, result := range results {
			if result.Error != "" {
				output.Fail(w, fmt.Sprintf("%s: %s", result.Path, result.Error))
				continue
			}
			verb := "registered"
			if dryRun {
				verb = "would register"
			}
			output.Confirm(w, fmt.Sprintf("%s %s (%s)", verb, result.Name, result.Path))
		}
		if added == 0 && failed == 0 {
			output.Info(w, "no unregistered worktrees to adopt")
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d worktree inspections failed", failed)
	}
	return nil
}
