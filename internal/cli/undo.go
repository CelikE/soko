package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/CelikE/soko/internal/git"
	"github.com/CelikE/soko/internal/journal"
	"github.com/CelikE/soko/internal/output"
)

// newUndoCmd creates the cobra command for soko undo.
func newUndoCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "undo",
		Short: "Revert the last destructive soko operation",
		Long: `Reverse the most recent destructive operation soko performed, using a small
capped journal of pre-images (refs, registry entries) that soko records as it
works. Undo is for "oops, just now" — it reverts the latest entry, not history.

Currently reverses: soko clean (recreates deleted branches at their SHAs) and
fast-forward pulls from soko ui (resets the branch to its pre-pull SHA).`,
		Example: `  soko undo            # revert the last operation
  soko undo --list     # show the journal without changing anything`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			w := cmd.OutOrStdout()

			listFlag, _ := cmd.Flags().GetBool("list")
			jsonFlag, _ := cmd.Flags().GetBool("json")

			j, err := journal.Load()
			if err != nil {
				return fmt.Errorf("loading journal: %w", err)
			}

			if listFlag {
				return renderJournal(w, j, jsonFlag)
			}

			entry, ok := j.Last()
			if !ok {
				output.Info(w, "nothing to undo")
				return nil
			}

			if err := revertEntry(cmd.Context(), w, &entry); err != nil {
				return err
			}

			// The entry is consumed whether or not every sub-step succeeded — a
			// re-run would otherwise try to recreate branches that already exist.
			if _, err := journal.PopLast(); err != nil {
				return fmt.Errorf("updating journal: %w", err)
			}
			return nil
		},
	}

	cmd.Flags().Bool("list", false, "show the undo journal instead of reverting")
	return cmd
}

// revertEntry dispatches an entry to the right reversal routine.
func revertEntry(ctx context.Context, w io.Writer, e *journal.Entry) error {
	switch e.Op {
	case journal.OpClean:
		return undoClean(ctx, w, e)
	case journal.OpPull:
		return undoPull(ctx, w, e)
	default:
		return fmt.Errorf("don't know how to undo operation %q", e.Op)
	}
}

// undoPull resets each fast-forwarded repo back to its pre-pull SHA. Because the
// pull was fast-forward only, the recorded SHA is an ancestor of HEAD, so the
// reset cleanly rewinds the just-pulled commits.
func undoPull(ctx context.Context, w io.Writer, e *journal.Entry) error {
	var reset, failed int
	for _, p := range e.Pulls {
		if !pathExists(p.Path) {
			output.Fail(w, fmt.Sprintf("%s: path not found, cannot reset", p.Repo))
			failed++
			continue
		}
		if _, err := git.Run(ctx, p.Path, "reset", "--hard", p.SHA); err != nil {
			output.Fail(w, fmt.Sprintf("%s: failed to reset to %s: %v", p.Repo, shortSHA(p.SHA), err))
			failed++
			continue
		}
		output.Confirm(w, fmt.Sprintf("%s: reset to %s", p.Repo, shortSHA(p.SHA)))
		reset++
	}

	if failed > 0 {
		return fmt.Errorf("undo pull: reset %d %s, %d failed",
			reset, output.Plural(reset, "repo"), failed)
	}
	if reset == 0 {
		output.Info(w, "nothing to reset")
	}
	return nil
}

// undoClean recreates each deleted branch at its recorded SHA. A branch that
// already exists is reported and skipped rather than failing the whole undo.
func undoClean(ctx context.Context, w io.Writer, e *journal.Entry) error {
	var restored, failed int
	for _, b := range e.Branches {
		if !pathExists(b.Path) {
			output.Fail(w, fmt.Sprintf("%s: path not found, cannot restore %s", b.Repo, b.Branch))
			failed++
			continue
		}
		if _, err := git.Run(ctx, b.Path, "rev-parse", "--verify", "--quiet", b.Branch); err == nil {
			output.Warn(w, fmt.Sprintf("%s: branch %s already exists, skipping", b.Repo, b.Branch))
			continue
		}
		if _, err := git.Run(ctx, b.Path, "branch", b.Branch, b.SHA); err != nil {
			output.Fail(w, fmt.Sprintf("%s: failed to recreate %s: %v", b.Repo, b.Branch, err))
			failed++
			continue
		}
		output.Confirm(w, fmt.Sprintf("%s: recreated %s at %s", b.Repo, b.Branch, shortSHA(b.SHA)))
		restored++
	}

	if failed > 0 {
		return fmt.Errorf("undo clean: restored %d %s, %d failed",
			restored, output.Plural(restored, "branch"), failed)
	}
	if restored == 0 {
		output.Info(w, "nothing to restore")
	}
	return nil
}

// renderJournal prints the journal newest-first, or as JSON.
func renderJournal(w io.Writer, j *journal.Journal, jsonOut bool) error {
	if jsonOut {
		return output.RenderJSON(w, j.Entries)
	}

	if len(j.Entries) == 0 {
		output.Info(w, "undo journal is empty")
		return nil
	}

	_, _ = fmt.Fprintf(w, "\n  %s\n\n", output.Dim("undo journal — newest first"))
	for i := len(j.Entries) - 1; i >= 0; i-- {
		e := j.Entries[i]
		marker := "  "
		if i == len(j.Entries)-1 {
			marker = "› " // the entry the next `soko undo` will revert
		}
		_, _ = fmt.Fprintf(w, "%s%s  %s  %s\n",
			marker,
			output.Dim(output.FormatTimeAgo(e.Time)),
			string(e.Op),
			e.Summary)
	}
	_, _ = fmt.Fprintln(w)
	return nil
}

func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}
