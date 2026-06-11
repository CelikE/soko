package cli

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"

	"github.com/CelikE/soko/internal/config"
	"github.com/CelikE/soko/internal/git"
	"github.com/CelikE/soko/internal/output"
)

// syncStatus is the outcome of syncing a single repo. The zero value is
// syncFailed so an unset result is treated as a failure, mirroring pull.
type syncStatus int

const (
	syncFailed syncStatus = iota
	syncSkipped
	syncDirty
	syncDiverged
	syncBehind
	syncUpToDate
	syncPulled
)

type syncResult struct {
	index      int
	name       string
	path       string
	status     syncStatus
	action     string
	message    string
	errorCode  string
	newCommits int
	elapsed    time.Duration
}

// newSyncCmd creates the cobra command for soko sync.
func newSyncCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sync [repos...]",
		Short: "Fetch all repos and fast-forward the safe ones",
		Long: `Fetch all (or specific) registered repos, then fast-forward the ones that
are safe to pull: clean working tree, an upstream, and no divergence.

sync never creates merge commits, never rebases, and never touches a dirty
working tree — repos it refuses to pull are reported as needing attention so
you keep full control. It is the one-command morning routine: everything that
can be brought up to date is, and everything else is listed.`,
		Args:              cobra.ArbitraryArgs,
		ValidArgsFunction: repoNameCompletionFunc(),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
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

			noWorktrees, _ := cmd.Flags().GetBool("no-worktrees")
			if noWorktrees {
				repos = config.FilterNoWorktrees(repos)
			}

			fetchOnly, _ := cmd.Flags().GetBool("fetch-only")
			jsonFlag, _ := cmd.Flags().GetBool("json")

			var prog *output.Progress
			if !jsonFlag {
				prog = output.NewProgress(cmd.ErrOrStderr(), "Syncing repositories", len(repos))
			}

			results := make([]syncResult, 0, len(repos))
			var mu sync.Mutex

			g, ctx := errgroup.WithContext(ctx)
			g.SetLimit(maxConcurrency)

			wallStart := time.Now()
			for i, repo := range repos {
				g.Go(func() error {
					r := syncRepo(ctx, i, &repo, fetchOnly)
					mu.Lock()
					results = append(results, r)
					mu.Unlock()
					if prog != nil {
						prog.Increment()
					}
					return nil
				})
			}

			// Goroutines never return errors (captured in results), so Wait only
			// returns nil or a context cancellation which is safe to ignore.
			_ = g.Wait()
			wall := time.Since(wallStart)

			if prog != nil {
				prog.Done()
			}

			// Restore config order.
			ordered := make([]syncResult, len(results))
			for idx := range results {
				ordered[results[idx].index] = results[idx]
			}

			timingRows := make([]output.TimingRow, len(ordered))
			for i := range ordered {
				timingRows[i] = output.TimingRow{Name: ordered[i].name, Duration: ordered[i].elapsed}
			}

			if jsonFlag {
				if err := renderSyncJSON(w, ordered, timingRows, wall); err != nil {
					return err
				}
				if failed := countSyncFailures(ordered); failed > 0 {
					return fmt.Errorf("%d %s failed to sync", failed, output.Plural(failed, "repo"))
				}
				return nil
			}

			rows := make([]output.SyncRow, len(ordered))
			var pulled, upToDate, attention, skipped, failed, missing, newCommits int
			for i := range ordered {
				// The table cell only fits one line, so condense git's
				// multi-line failure output to its most informative line.
				message := ordered[i].message
				if ordered[i].status == syncFailed {
					message = gitErrorReason(message)
				}
				rows[i] = output.SyncRow{
					Name:    ordered[i].name,
					Action:  ordered[i].action,
					Outcome: syncOutcome(ordered[i].status),
					Result:  message,
				}
				switch ordered[i].status {
				case syncPulled:
					pulled++
					newCommits += ordered[i].newCommits
				case syncUpToDate:
					upToDate++
				case syncDirty, syncDiverged, syncBehind:
					attention++
				case syncSkipped:
					skipped++
				case syncFailed:
					failed++
					if ordered[i].message == "path not found" {
						missing++
					}
				}
			}

			output.RenderSyncResults(w, rows)
			output.RenderSyncSummary(w, len(rows), pulled, upToDate, attention, skipped, failed, newCommits)
			renderMissingHint(w, missing)
			output.RenderTiming(w, timingRows, wall, maxConcurrency)

			if failed > 0 {
				return fmt.Errorf("%d %s failed to sync", failed, output.Plural(failed, "repo"))
			}

			return nil
		},
	}

	cmd.Flags().Bool("fetch-only", false, "fetch every repo but never pull")
	cmd.Flags().Bool("no-worktrees", false, "skip worktree entries, only sync parent repos")
	cmd.Flags().StringSlice("tag", nil, "filter by tag (can be repeated, combines with OR)")
	_ = cmd.RegisterFlagCompletionFunc("tag", tagCompletionFunc())

	return cmd
}

// syncRepo fetches one repo and fast-forwards it when that is provably safe:
// path present, upstream configured, clean working tree, behind without having
// diverged. Every other state is reported, never acted on.
func syncRepo(ctx context.Context, index int, repo *config.RepoEntry, fetchOnly bool) syncResult {
	start := time.Now()
	r := syncResult{index: index, name: repo.Name, path: repo.Path, action: "fetch"}
	defer func() { r.elapsed = time.Since(start) }()

	if !pathExists(repo.Path) {
		r.status = syncFailed
		r.action = "—"
		r.message = "path not found"
		r.errorCode = codePathMissing
		return r
	}

	// A repo with no remote at all is not an error: modern git exits 0 on a
	// bare `git fetch` with nothing configured, and the repo is then skipped
	// below as "no upstream".
	if err := git.Fetch(ctx, repo.Path, false); err != nil {
		r.status = syncFailed
		r.message = err.Error()
		r.errorCode = gitErrorCode(err)
		return r
	}

	st, err := git.ParseStatus(ctx, repo.Path)
	if err != nil {
		r.status = syncFailed
		r.message = err.Error()
		r.errorCode = gitErrorCode(err)
		return r
	}

	if !git.HasUpstream(ctx, repo.Path) {
		// Local-only branch or detached HEAD — nothing to pull.
		// Treat as a skip, not a failure.
		r.status = syncSkipped
		r.message = "no upstream"
		r.errorCode = codeNoUpstream
		return r
	}

	dirty := st.Modified+st.Untracked+st.Deleted+st.Conflicts > 0
	switch {
	case dirty:
		r.status = syncDirty
		r.action = "fetch only"
		r.message = "dirty"
		if st.Behind > 0 {
			r.message = "dirty (skipped pull)"
		}
	case st.Behind > 0 && st.Ahead > 0:
		r.status = syncDiverged
		r.message = "diverged (needs rebase)"
	case st.Behind > 0 && fetchOnly:
		r.status = syncBehind
		r.message = fmt.Sprintf("behind %d (not pulled)", st.Behind)
	case st.Behind > 0:
		r.action = "fetch + pull"
		if _, pullErr := git.Pull(ctx, repo.Path, false); pullErr != nil {
			r.status = syncFailed
			// Keep the full error here; the table cleans it to a single
			// line, but --json emits it in full like pull.
			r.message = pullErr.Error()
			r.errorCode = gitErrorCode(pullErr)
			return r
		}
		r.status = syncPulled
		r.newCommits = st.Behind
		r.message = fmt.Sprintf("%d new %s", st.Behind, output.Plural(st.Behind, "commit"))
	default:
		r.status = syncUpToDate
		r.message = "up to date"
	}

	return r
}

// syncOutcome maps an internal syncStatus to the output package's row outcome.
func syncOutcome(s syncStatus) output.SyncOutcome {
	switch s {
	case syncPulled:
		return output.SyncOK
	case syncUpToDate:
		return output.SyncNeutral
	case syncDirty, syncDiverged, syncBehind:
		return output.SyncWarn
	case syncSkipped:
		return output.SyncSkip
	default:
		return output.SyncFail
	}
}

// countSyncFailures returns the number of results that failed to sync.
func countSyncFailures(results []syncResult) int {
	var failed int
	for i := range results {
		if results[i].status == syncFailed {
			failed++
		}
	}
	return failed
}

type syncJSON struct {
	Name       string `json:"name"`
	Path       string `json:"path"`
	Status     string `json:"status"`
	Action     string `json:"action"`
	NewCommits int    `json:"new_commits,omitempty"`
	Error      string `json:"error,omitempty"`
	ErrorCode  string `json:"error_code,omitempty"`
	DurationMS int64  `json:"duration_ms,omitempty"`
}

func renderSyncJSON(w io.Writer, results []syncResult, rows []output.TimingRow, wall time.Duration) error {
	entries := make([]syncJSON, len(results))
	for i := range results {
		r := &results[i]
		entries[i] = syncJSON{
			Name:       r.name,
			Path:       r.path,
			Action:     r.action,
			NewCommits: r.newCommits,
		}
		switch r.status {
		case syncPulled:
			entries[i].Status = "pulled"
		case syncUpToDate:
			entries[i].Status = "up-to-date"
		case syncDirty:
			entries[i].Status = "dirty"
		case syncDiverged:
			entries[i].Status = "diverged"
		case syncBehind:
			entries[i].Status = "behind"
		case syncSkipped:
			entries[i].Status = "skipped"
		case syncFailed:
			entries[i].Status = "failed"
			entries[i].Error = r.message
		}
		if r.errorCode != "" {
			entries[i].ErrorCode = r.errorCode
		}
		if output.Perf() {
			entries[i].DurationMS = r.elapsed.Milliseconds()
		}
	}

	if output.Perf() {
		return output.RenderPerfJSON(w, entries, output.BuildTiming(rows, wall, maxConcurrency))
	}
	return output.RenderJSON(w, entries)
}
