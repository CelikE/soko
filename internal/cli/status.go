package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
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
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show status of all registered repos",
		RunE: func(cmd *cobra.Command, _ []string) error {
			w := cmd.OutOrStdout()

			cfg, repos, err := loadReposWithTagFilter(cmd)
			if err != nil {
				return err
			}

			if len(repos) == 0 {
				if len(cfg.Repos) == 0 {
					_, _ = fmt.Fprintln(w, "no repos registered yet — cd into a repo and run: soko init")
				} else {
					_, _ = fmt.Fprintln(w, "no repos match the tag filter")
				}
				return nil
			}

			fetchFlag, _ := cmd.Flags().GetBool("fetch")
			filteredCfg := &config.Config{Repos: repos}
			collected := collectAll(cmd, filteredCfg, fetchFlag)

			dirtyFlag, _ := cmd.Flags().GetBool("dirty")
			cleanFlag, _ := cmd.Flags().GetBool("clean")
			aheadFlag, _ := cmd.Flags().GetBool("ahead")
			behindFlag, _ := cmd.Flags().GetBool("behind")

			if dirtyFlag || cleanFlag || aheadFlag || behindFlag {
				collected = filterResults(collected, dirtyFlag, cleanFlag, aheadFlag, behindFlag)
			}

			if len(collected) == 0 {
				_, _ = fmt.Fprintln(w, "no repos match the filter")
				return nil
			}

			jsonFlag, _ := cmd.Flags().GetBool("json")
			if jsonFlag {
				return renderStatusJSON(w, collected)
			}

			// Sort by original config order and compute summary.
			sortByIndex(collected)
			rows := make([]output.StatusRow, len(collected))
			var dirtyCount, behindCount, totalChanges int
			for i := range collected {
				r := &collected[i]
				rows[i] = r.row
				if r.dirty {
					dirtyCount++
				}
				if r.behind {
					behindCount++
				}
				totalChanges += r.changes
			}

			output.RenderStatusTable(w, rows)
			output.RenderSummary(w, len(collected), dirtyCount, behindCount, totalChanges)

			return nil
		},
	}

	cmd.Flags().Bool("fetch", false, "fetch from remotes before collecting status")
	cmd.Flags().StringSlice("tag", nil, "filter by tag (can be repeated, combines with OR)")
	_ = cmd.RegisterFlagCompletionFunc("tag", tagCompletionFunc())
	cmd.Flags().Bool("dirty", false, "show only repos with uncommitted changes")
	cmd.Flags().Bool("clean", false, "show only clean repos in sync with remote")
	cmd.Flags().Bool("ahead", false, "show only repos ahead of remote")
	cmd.Flags().Bool("behind", false, "show only repos behind remote")

	return cmd
}

type statusResult struct {
	index   int
	row     output.StatusRow
	status  *git.RepoStatus
	path    string
	dirty   bool
	ahead   bool
	behind  bool
	changes int
	err     string
}

func collectAll(cmd *cobra.Command, cfg *config.Config, fetch bool) []statusResult {
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

			if fetch {
				// Best-effort fetch — don't fail status if fetch fails.
				_ = git.Fetch(ctx, repo.Path, false)
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
			r.ahead = status.Ahead > 0
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

// sortByIndex sorts results by their original config index.
func sortByIndex(results []statusResult) {
	sort.Slice(results, func(i, j int) bool {
		return results[i].index < results[j].index
	})
}

// filterResults returns only the results matching at least one of the enabled
// filters. Multiple filters combine with OR.
func filterResults(results []statusResult, dirty, clean, ahead, behind bool) []statusResult {
	filtered := make([]statusResult, 0, len(results))
	for i := range results {
		r := &results[i]
		if dirty && r.dirty {
			filtered = append(filtered, *r)
			continue
		}
		if clean && !r.dirty && !r.ahead && !r.behind && r.err == "" {
			filtered = append(filtered, *r)
			continue
		}
		if ahead && r.ahead {
			filtered = append(filtered, *r)
			continue
		}
		if behind && r.behind {
			filtered = append(filtered, *r)
			continue
		}
	}
	return filtered
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

func renderStatusJSON(w io.Writer, results []statusResult) error {
	sortByIndex(results)

	entries := make([]statusJSON, len(results))
	for i := range results {
		r := &results[i]
		entry := statusJSON{
			Name:  r.row.Name,
			Path:  r.path,
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
