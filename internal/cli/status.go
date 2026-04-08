package cli

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"

	"github.com/CelikE/soko/internal/config"
	"github.com/CelikE/soko/internal/git"
	"github.com/CelikE/soko/internal/output"
)

// significantBehindThreshold is the number of commits behind remote that
// triggers a red (conflict) state in the status output.
const significantBehindThreshold = 5

// newStatusCmd creates the cobra command for soko status.
func newStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status [repos...]",
		Short: "Show status of all registered repos",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			w := cmd.OutOrStdout()

			cfg, repos, err := loadReposWithTagFilter(cmd)
			if err != nil {
				return err
			}

			if len(args) > 0 {
				repos = findReposMatching(repos, args)
				if len(repos) == 0 {
					output.Info(w, fmt.Sprintf("no repos found matching: %s", strings.Join(args, ", ")))
					return nil
				}
			}

			if len(repos) == 0 {
				output.Info(w, noReposMessage(len(cfg.Repos)))
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
				output.Info(w, "no repos match the filter")
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

			groupFlag, _ := cmd.Flags().GetBool("group")
			allFlag, _ := cmd.Flags().GetBool("all")

			switch {
			case groupFlag:
				groups := buildStatusGroups(collected)
				output.RenderStatusGrouped(w, groups)
			case allFlag:
				output.RenderStatusTableN(w, rows, 0)
			default:
				output.RenderStatusTable(w, rows)
			}
			output.RenderSummary(w, len(collected), dirtyCount, behindCount, totalChanges)

			return nil
		},
	}

	cmd.Flags().Bool("fetch", false, "fetch from remotes before collecting status")
	cmd.Flags().Bool("group", false, "group repos by tag in a tree view")
	cmd.Flags().Bool("all", false, "show all repos without truncation")
	cmd.Flags().StringSlice("tag", nil, "filter by tag (can be repeated, combines with OR)")
	_ = cmd.RegisterFlagCompletionFunc("tag", tagCompletionFunc())
	cmd.Flags().Bool("dirty", false, "show only repos with uncommitted changes")
	cmd.Flags().Bool("clean", false, "show only clean repos in sync with remote")
	cmd.Flags().Bool("ahead", false, "show only repos ahead of remote")
	cmd.Flags().Bool("behind", false, "show only repos behind remote")

	return cmd
}

type statusResult struct {
	index      int
	row        output.StatusRow
	status     *git.RepoStatus
	path       string
	tags       []string
	worktreeOf string
	dirty      bool
	ahead      bool
	behind     bool
	changes    int
	err        string
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
				index:      i,
				path:       repo.Path,
				tags:       repo.Tags,
				worktreeOf: repo.WorktreeOf,
				row:        output.StatusRow{Name: repo.Name},
			}

			if !pathExists(repo.Path) {
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
			r.row.LastCommitText = output.FormatLastCommit(status.LastCommitTime, status.LastCommitMessage)
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

	// Goroutines never return errors (captured in results), so Wait only
	// returns nil or a context cancellation which is safe to ignore.
	_ = g.Wait()
	return results
}

// buildStatusGroups organizes results by tag for grouped rendering.
func buildStatusGroups(results []statusResult) []output.StatusGroup {
	groups := make(map[string][]output.StatusRow)
	var untagged []output.StatusRow

	for i := range results {
		r := &results[i]
		if len(r.tags) == 0 {
			untagged = append(untagged, r.row)
			continue
		}
		for _, tag := range r.tags {
			groups[tag] = append(groups[tag], r.row)
		}
	}

	// Sort tag names.
	tags := make([]string, 0, len(groups))
	for tag := range groups {
		tags = append(tags, tag)
	}
	sort.Strings(tags)

	var result []output.StatusGroup
	for _, tag := range tags {
		result = append(result, output.StatusGroup{Tag: tag, Rows: groups[tag]})
	}
	if len(untagged) > 0 {
		result = append(result, output.StatusGroup{Tag: "untagged", Rows: untagged})
	}

	return result
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
	WorktreeOf        string `json:"worktree_of,omitempty"`
	Error             string `json:"error,omitempty"`
}

func renderStatusJSON(w io.Writer, results []statusResult) error {
	sortByIndex(results)

	entries := make([]statusJSON, len(results))
	for i := range results {
		r := &results[i]
		entry := statusJSON{
			Name:       r.row.Name,
			Path:       r.path,
			WorktreeOf: r.worktreeOf,
			Error:      r.err,
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

	return output.RenderJSON(w, entries)
}

// rowState determines the RowState from a RepoStatus.
func rowState(s *git.RepoStatus) output.RowState {
	if s.Conflicts > 0 || s.Behind > significantBehindThreshold {
		return output.StateConflict
	}
	if s.Modified+s.Untracked+s.Deleted > 0 || s.Ahead > 0 || s.Behind > 0 {
		return output.StateDirty
	}
	return output.StateClean
}
