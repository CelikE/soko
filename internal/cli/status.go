package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

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
			w := cmd.OutOrStdout()

			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			if len(cfg.Repos) == 0 {
				_, _ = fmt.Fprintln(w, "no repos registered yet — cd into a repo and run: soko init")
				return nil
			}

			collected := collectAll(cmd, cfg)

			jsonFlag, _ := cmd.Flags().GetBool("json")
			if jsonFlag {
				return renderStatusJSON(w, cfg.Repos, collected)
			}

			// Restore config order and compute summary.
			rows := make([]output.StatusRow, len(collected))
			var dirtyCount, behindCount, totalChanges int
			for i := range collected {
				r := &collected[i]
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

type statusResult struct {
	index   int
	row     output.StatusRow
	status  *git.RepoStatus
	path    string
	dirty   bool
	behind  bool
	changes int
	err     string
}

func collectAll(cmd *cobra.Command, cfg *config.Config) []statusResult {
	ctx := cmd.Context()
	results := make([]statusResult, 0, len(cfg.Repos))
	var mu sync.Mutex

	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(maxConcurrency)

	for i, repo := range cfg.Repos {
		g.Go(func() error {
			r := statusResult{
				index: i,
				path:  repo.Path,
				row:   output.StatusRow{Name: repo.Name},
			}

			if _, statErr := os.Stat(repo.Path); os.IsNotExist(statErr) {
				r.row.Branch = "-"
				r.row.StatusText = output.SymConflict + " not found"
				r.row.AheadBehindText = "-"
				r.row.LastCommitText = "-"
				r.row.State = output.StateConflict
				r.err = "path not found"

				mu.Lock()
				results = append(results, r)
				mu.Unlock()
				return nil
			}

			status, parseErr := git.ParseStatus(ctx, repo.Path)
			if parseErr != nil {
				r.row.Branch = "-"
				r.row.StatusText = output.SymConflict + " error"
				r.row.AheadBehindText = "-"
				r.row.LastCommitText = "-"
				r.row.State = output.StateConflict
				r.err = parseErr.Error()

				mu.Lock()
				results = append(results, r)
				mu.Unlock()
				return nil
			}

			changes := status.Modified + status.Untracked + status.Deleted
			r.status = status
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

	_ = g.Wait()
	return results
}

type statusJSON struct {
	Name              string `json:"name"`
	Path              string `json:"path"`
	Branch            string `json:"branch"`
	Dirty             bool   `json:"dirty"`
	Modified          int    `json:"modified"`
	Untracked         int    `json:"untracked"`
	Deleted           int    `json:"deleted"`
	Conflicts         int    `json:"conflicts"`
	Ahead             int    `json:"ahead"`
	Behind            int    `json:"behind"`
	LastCommitTime    string `json:"last_commit_time"`
	LastCommitMessage string `json:"last_commit_message"`
	Error             string `json:"error,omitempty"`
}

func renderStatusJSON(w io.Writer, repos []config.RepoEntry, results []statusResult) error {
	// Index results by position.
	indexed := make(map[int]*statusResult, len(results))
	for i := range results {
		indexed[results[i].index] = &results[i]
	}

	entries := make([]statusJSON, len(repos))
	for i, repo := range repos {
		r := indexed[i]
		entry := statusJSON{
			Name:  repo.Name,
			Path:  repo.Path,
			Error: r.err,
		}

		if r.status != nil {
			entry.Branch = r.status.Branch
			entry.Dirty = r.dirty
			entry.Modified = r.status.Modified
			entry.Untracked = r.status.Untracked
			entry.Deleted = r.status.Deleted
			entry.Conflicts = r.status.Conflicts
			entry.Ahead = r.status.Ahead
			entry.Behind = r.status.Behind
			if !r.status.LastCommitTime.IsZero() {
				entry.LastCommitTime = r.status.LastCommitTime.Format(time.RFC3339)
			}
			entry.LastCommitMessage = r.status.LastCommitMessage
		}

		entries[i] = entry
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(entries); err != nil {
		return fmt.Errorf("encoding json: %w", err)
	}
	return nil
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
