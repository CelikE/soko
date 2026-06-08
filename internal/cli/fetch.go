package cli

import (
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

type fetchResult struct {
	index   int
	name    string
	path    string
	success bool
	message string
	elapsed time.Duration
}

// newFetchCmd creates the cobra command for soko fetch.
func newFetchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:               "fetch [repos...]",
		Short:             "Fetch all registered repos",
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

			pruneFlag, _ := cmd.Flags().GetBool("prune")
			jsonFlag, _ := cmd.Flags().GetBool("json")

			var prog *output.Progress
			if !jsonFlag {
				prog = output.NewProgress(cmd.ErrOrStderr(), "Fetching repositories", len(repos))
			}

			results := make([]fetchResult, 0, len(repos))
			var mu sync.Mutex

			g, ctx := errgroup.WithContext(ctx)
			g.SetLimit(maxConcurrency)

			wallStart := time.Now()
			for i, repo := range repos {
				g.Go(func() error {
					start := time.Now()
					r := fetchResult{index: i, name: repo.Name, path: repo.Path}

					switch {
					case !pathExists(repo.Path):
						r.message = "path not found"
					default:
						if fetchErr := git.Fetch(ctx, repo.Path, pruneFlag); fetchErr != nil {
							r.message = fetchErr.Error()
						} else {
							r.success = true
							r.message = "fetched"
						}
					}

					r.elapsed = time.Since(start)
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
			ordered := make([]fetchResult, len(results))
			for idx := range results {
				ordered[results[idx].index] = results[idx]
			}

			timingRows := make([]output.TimingRow, len(ordered))
			for i := range ordered {
				timingRows[i] = output.TimingRow{Name: ordered[i].name, Duration: ordered[i].elapsed}
			}

			if jsonFlag {
				if err := renderFetchJSON(w, ordered, timingRows, wall); err != nil {
					return err
				}
				var failed int
				for _, r := range ordered {
					if !r.success {
						failed++
					}
				}
				if failed > 0 {
					return fmt.Errorf("%d %s failed to fetch", failed, output.Plural(failed, "repo"))
				}
				return nil
			}

			rows := make([]output.FetchRow, len(ordered))
			var fetched, failed int
			for i := range ordered {
				rows[i] = output.FetchRow{
					Name:    ordered[i].name,
					Success: ordered[i].success,
					Message: ordered[i].message,
				}
				if ordered[i].success {
					fetched++
				} else {
					failed++
				}
			}

			output.RenderFetchTable(w, rows)
			output.RenderFetchSummary(w, len(rows), fetched, failed)
			output.RenderTiming(w, timingRows, wall, maxConcurrency)

			if failed > 0 {
				return fmt.Errorf("%d %s failed to fetch", failed, output.Plural(failed, "repo"))
			}

			return nil
		},
	}

	cmd.Flags().Bool("prune", false, "pass --prune to git fetch to clean up stale remote refs")
	cmd.Flags().Bool("no-worktrees", false, "skip worktree entries, only fetch parent repos")
	cmd.Flags().StringSlice("tag", nil, "filter by tag (can be repeated, combines with OR)")
	_ = cmd.RegisterFlagCompletionFunc("tag", tagCompletionFunc())

	return cmd
}

type fetchJSON struct {
	Name       string `json:"name"`
	Path       string `json:"path"`
	Status     string `json:"status"`
	Error      string `json:"error,omitempty"`
	DurationMS int64  `json:"duration_ms,omitempty"`
}

func renderFetchJSON(w io.Writer, results []fetchResult, rows []output.TimingRow, wall time.Duration) error {
	entries := make([]fetchJSON, len(results))
	for i := range results {
		r := &results[i]
		entries[i] = fetchJSON{
			Name: r.name,
			Path: r.path,
		}
		if r.success {
			entries[i].Status = "fetched"
		} else {
			entries[i].Status = "failed"
			entries[i].Error = r.message
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
