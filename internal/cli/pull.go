package cli

import (
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"

	"github.com/CelikE/soko/internal/config"
	"github.com/CelikE/soko/internal/git"
	"github.com/CelikE/soko/internal/output"
)

// pullStatus is the outcome of pulling a single repo. The zero value is
// pullFailed so an unset result is treated as a failure, mirroring fetch.
type pullStatus int

const (
	pullFailed pullStatus = iota
	pullSkipped
	pullUpToDate
	pullUpdated
)

type pullResult struct {
	index   int
	name    string
	path    string
	status  pullStatus
	message string
}

// newPullCmd creates the cobra command for soko pull.
func newPullCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pull [repos...]",
		Short: "Pull all registered repos",
		Long: `Pull all (or specific) registered repos in parallel.

By default each repo is pulled with --ff-only, so soko never creates a merge
commit and fails fast on a branch that has diverged from its upstream. Pass
--rebase to replay your local commits on top of the upstream instead.`,
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

			rebase, _ := cmd.Flags().GetBool("rebase")
			jsonFlag, _ := cmd.Flags().GetBool("json")

			var prog *output.Progress
			if !jsonFlag {
				prog = output.NewProgress(cmd.ErrOrStderr(), "Pulling repositories", len(repos))
			}

			results := make([]pullResult, 0, len(repos))
			var mu sync.Mutex

			g, ctx := errgroup.WithContext(ctx)
			g.SetLimit(maxConcurrency)

			for i, repo := range repos {
				g.Go(func() error {
					r := pullResult{index: i, name: repo.Name, path: repo.Path}

					switch {
					case !pathExists(repo.Path):
						r.status = pullFailed
						r.message = "path not found"
					case !git.HasUpstream(ctx, repo.Path):
						// Local-only branch or detached HEAD — nothing to pull.
						// Treat as a skip, not a failure.
						r.status = pullSkipped
						r.message = "no upstream"
					default:
						updated, pullErr := git.Pull(ctx, repo.Path, rebase)
						switch {
						case pullErr != nil:
							r.status = pullFailed
							// Keep the full error here; the table cleans it to a
							// single line, but --json emits it in full like fetch.
							r.message = pullErr.Error()
						case updated:
							r.status = pullUpdated
							r.message = "updated"
						default:
							r.status = pullUpToDate
							r.message = "up to date"
						}
					}

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

			if prog != nil {
				prog.Done()
			}

			// Restore config order.
			ordered := make([]pullResult, len(results))
			for idx := range results {
				ordered[results[idx].index] = results[idx]
			}

			if jsonFlag {
				if err := renderPullJSON(w, ordered); err != nil {
					return err
				}
				if failed := countPullFailures(ordered); failed > 0 {
					return fmt.Errorf("%d %s failed to pull", failed, output.Plural(failed, "repo"))
				}
				return nil
			}

			rows := make([]output.PullRow, len(ordered))
			var updated, upToDate, skipped, failed, missing int
			for i := range ordered {
				// The table cell only fits one line, so condense git's
				// multi-line failure output to its most informative line.
				message := ordered[i].message
				if ordered[i].status == pullFailed {
					message = gitErrorReason(message)
				}
				rows[i] = output.PullRow{
					Name:    ordered[i].name,
					Outcome: pullOutcome(ordered[i].status),
					Message: message,
				}
				switch ordered[i].status {
				case pullUpdated:
					updated++
				case pullUpToDate:
					upToDate++
				case pullSkipped:
					skipped++
				case pullFailed:
					failed++
					if ordered[i].message == "path not found" {
						missing++
					}
				}
			}

			output.RenderPullResults(w, rows)
			output.RenderPullSummary(w, len(rows), updated, upToDate, skipped, failed)
			renderMissingHint(w, missing)

			if failed > 0 {
				return fmt.Errorf("%d %s failed to pull", failed, output.Plural(failed, "repo"))
			}

			return nil
		},
	}

	cmd.Flags().Bool("rebase", false, "rebase local commits onto the upstream instead of --ff-only")
	cmd.Flags().Bool("no-worktrees", false, "skip worktree entries, only pull parent repos")
	cmd.Flags().StringSlice("tag", nil, "filter by tag (can be repeated, combines with OR)")
	_ = cmd.RegisterFlagCompletionFunc("tag", tagCompletionFunc())

	return cmd
}

// pullOutcome maps an internal pullStatus to the output package's row outcome.
func pullOutcome(s pullStatus) output.PullOutcome {
	switch s {
	case pullUpdated, pullUpToDate:
		return output.PullOK
	case pullSkipped:
		return output.PullSkip
	default:
		return output.PullErr
	}
}

// gitErrorReason extracts the most informative single line from a git error.
// git prints fetch progress (e.g. "From github.com:org/repo") to stderr before
// the actual "fatal:"/"error:" line, so the first line is often noise; prefer
// the last fatal/error line and fall back to the first line otherwise.
func gitErrorReason(s string) string {
	s = strings.TrimSpace(s)
	var reason string
	for line := range strings.Lines(s) {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "fatal:") || strings.HasPrefix(t, "error:") {
			reason = t
		}
	}
	if reason != "" {
		return reason
	}
	first, _, _ := strings.Cut(s, "\n")
	return strings.TrimSpace(first)
}

// countPullFailures returns the number of results that failed to pull.
func countPullFailures(results []pullResult) int {
	var failed int
	for i := range results {
		if results[i].status == pullFailed {
			failed++
		}
	}
	return failed
}

type pullJSON struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

func renderPullJSON(w io.Writer, results []pullResult) error {
	entries := make([]pullJSON, len(results))
	for i := range results {
		r := &results[i]
		entries[i] = pullJSON{
			Name: r.name,
			Path: r.path,
		}
		switch r.status {
		case pullUpdated:
			entries[i].Status = "updated"
		case pullUpToDate:
			entries[i].Status = "up-to-date"
		case pullSkipped:
			entries[i].Status = "skipped"
		case pullFailed:
			entries[i].Status = "failed"
			entries[i].Error = r.message
		}
	}

	return output.RenderJSON(w, entries)
}
