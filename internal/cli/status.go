package cli

import (
	"fmt"
	"os"
	"sync"

	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"

	"github.com/CelikE/soko/internal/config"
	"github.com/CelikE/soko/internal/git"
	"github.com/CelikE/soko/internal/output"
)

const maxConcurrency = 8

// newStatusCmd creates the cobra command for soko status.
func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show status of all registered repos",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			w := cmd.OutOrStdout()

			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			if len(cfg.Repos) == 0 {
				_, _ = fmt.Fprintln(w, "no repos registered yet — cd into a repo and run: soko init")
				return nil
			}

			type result struct {
				index   int
				row     output.StatusRow
				dirty   bool
				behind  bool
				changes int
			}

			results := make([]result, 0, len(cfg.Repos))
			var mu sync.Mutex

			g, ctx := errgroup.WithContext(ctx)
			g.SetLimit(maxConcurrency)

			for i, repo := range cfg.Repos {
				g.Go(func() error {
					r := result{index: i, row: output.StatusRow{Name: repo.Name}}

					if _, statErr := os.Stat(repo.Path); os.IsNotExist(statErr) {
						r.row.Branch = "-"
						r.row.StatusText = "path not found"
						r.row.AheadBehindText = "-"
						r.row.LastCommitText = "-"
						r.row.State = output.StateConflict

						mu.Lock()
						results = append(results, r)
						mu.Unlock()
						return nil
					}

					status, parseErr := git.ParseStatus(ctx, repo.Path)
					if parseErr != nil {
						r.row.Branch = "-"
						r.row.StatusText = "error"
						r.row.AheadBehindText = "-"
						r.row.LastCommitText = "-"
						r.row.State = output.StateConflict

						mu.Lock()
						results = append(results, r)
						mu.Unlock()
						return nil
					}

					changes := status.Modified + status.Untracked + status.Deleted
					r.row.Branch = status.Branch
					r.row.StatusText = output.FormatStatus(status.Modified, status.Untracked, status.Deleted, status.Conflicts)
					r.row.AheadBehindText = output.FormatAheadBehind(status.Ahead, status.Behind)
					r.row.LastCommitText = output.FormatTimeAgo(status.LastCommitTime)
					r.row.State = rowState(status)
					r.dirty = changes > 0
					r.behind = status.Behind > 0
					r.changes = changes

					mu.Lock()
					results = append(results, r)
					mu.Unlock()
					return nil
				})
			}

			if err := g.Wait(); err != nil {
				return fmt.Errorf("collecting status: %w", err)
			}

			// Restore config order and compute summary.
			rows := make([]output.StatusRow, len(results))
			var dirtyCount, behindCount, totalChanges int
			for _, r := range results {
				rows[r.index] = r.row
				if r.dirty {
					dirtyCount++
				}
				if r.behind {
					behindCount++
				}
				totalChanges += r.changes
			}

			output.RenderStatusTable(w, rows)
			output.RenderSummary(w, len(cfg.Repos), dirtyCount, behindCount, totalChanges)

			return nil
		},
	}
}

// rowState determines the RowState from a RepoStatus.
func rowState(s *git.RepoStatus) output.RowState {
	if s.Conflicts > 0 || s.Behind > 5 {
		return output.StateConflict
	}
	if s.Modified+s.Untracked+s.Deleted > 0 || s.Ahead > 0 || s.Behind > 0 {
		return output.StateDirty
	}
	return output.StateClean
}
