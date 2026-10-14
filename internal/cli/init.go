package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/CelikE/soko/internal/config"
	"github.com/CelikE/soko/internal/git"
	"github.com/CelikE/soko/internal/output"
	"github.com/CelikE/soko/internal/worktree"
)

// newInitCmd creates the cobra command for soko init.
func newInitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Register the current repo with soko",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("getting working directory: %w", err)
			}
			linked, _ := cmd.Flags().GetBool("worktree")
			tags, _ := cmd.Flags().GetStringSlice("tag")
			jsonFlag, _ := cmd.Flags().GetBool("json")
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			result, err := worktree.Register(cmd.Context(), cfg, dir, tags, linked)
			if err != nil {
				return err
			}
			if !result.Existing || result.ParentAdded {
				if err := config.Save(cfg); err != nil {
					return fmt.Errorf("saving config: %w", err)
				}
			}
			w := cmd.OutOrStdout()
			if jsonFlag {
				return output.RenderJSON(w, struct {
					listEntry
					AlreadyRegistered bool `json:"already_registered"`
				}{listEntry: listEntry{Name: result.Entry.Name, Path: result.Entry.Path,
					Tags: result.Entry.Tags, WorktreeOf: result.Entry.WorktreeOf, Meta: result.Entry.Meta},
					AlreadyRegistered: result.Existing})
			}
			if !linked && git.IsWorktree(cmd.Context(), dir) {
				output.Info(w, fmt.Sprintf("worktree detected — registering main repo at %s", result.Entry.Path))
				output.Info(w, "use --worktree to register this worktree instead")
			}
			if result.ParentAdded {
				output.Confirm(w, fmt.Sprintf("registered %s (%s)", result.Parent.Name, result.Parent.Path))
			}
			if result.Existing {
				output.Warn(w, fmt.Sprintf("already registered %s (%s)", result.Entry.Name, result.Entry.Path))
				return nil
			}
			output.Confirm(w, fmt.Sprintf("registered %s (%s)", result.Entry.Name, result.Entry.Path))
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
