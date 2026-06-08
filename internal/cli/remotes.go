package cli

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"

	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"

	"github.com/CelikE/soko/internal/config"
	"github.com/CelikE/soko/internal/git"
	"github.com/CelikE/soko/internal/output"
)

// newRemotesCmd creates the cobra command for soko remotes. It is the
// read-only sibling of soko status: where status answers "what changed?",
// remotes answers "where does this repo push and pull?".
func newRemotesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:               "remotes [repos...]",
		Short:             "Show remotes and upstream tracking for every repo",
		Args:              cobra.ArbitraryArgs,
		ValidArgsFunction: repoNameCompletionFunc(),
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

			results := collectRemotes(cmd, repos)

			missingUpstream, _ := cmd.Flags().GetBool("missing-upstream")
			if missingUpstream {
				results = filterTrackingFailures(results)
			}

			jsonFlag, _ := cmd.Flags().GetBool("json")
			if jsonFlag {
				return renderRemotesJSON(w, results)
			}

			rows := make([]output.RemoteRow, len(results))
			var okCount int
			for i := range results {
				r := &results[i]
				rows[i] = remoteRowOf(r)
				if r.trackingOK {
					okCount++
				}
			}

			output.RenderRemotesTable(w, rows)
			output.RenderRemotesSummary(w, len(results), okCount, len(results)-okCount)

			return nil
		},
	}

	cmd.Flags().StringSlice("tag", nil, "filter by tag (can be repeated, combines with OR)")
	_ = cmd.RegisterFlagCompletionFunc("tag", tagCompletionFunc())
	cmd.Flags().Bool("missing-upstream", false, "show only repos with no remote or no upstream")

	return cmd
}

// remoteResult holds the collected remote configuration for one repo.
type remoteResult struct {
	index      int
	name       string
	path       string
	remotes    map[string]string
	upstream   string
	trackingOK bool
	flag       string
	err        string
}

// collectRemotes gathers remote configuration for every repo in parallel with
// bounded concurrency, mirroring collectAll. Per-repo failures are captured on
// the row and never abort the run; results are restored to config order.
func collectRemotes(cmd *cobra.Command, repos []config.RepoEntry) []remoteResult {
	ctx := cmd.Context()
	results := make([]remoteResult, 0, len(repos))
	var mu sync.Mutex

	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(maxConcurrency)

	for i, repo := range repos {
		g.Go(func() error {
			r := remoteResult{index: i, name: repo.Name, path: repo.Path}

			if !pathExists(repo.Path) {
				r.err = "path not found"
				r.flag = "not found"
				mu.Lock()
				results = append(results, r)
				mu.Unlock()
				return nil
			}

			remotes, remErr := git.Remotes(ctx, repo.Path)
			if remErr != nil {
				r.err = remErr.Error()
				r.flag = "error"
				mu.Lock()
				results = append(results, r)
				mu.Unlock()
				return nil
			}
			r.remotes = remotes

			upstream, upErr := git.UpstreamBranch(ctx, repo.Path)
			if upErr == nil {
				r.upstream = upstream
			}

			switch {
			case len(remotes) == 0:
				r.flag = "no remote"
			case upErr != nil:
				r.flag = "no upstream"
			default:
				r.trackingOK = true
			}

			mu.Lock()
			results = append(results, r)
			mu.Unlock()
			return nil
		})
	}

	// Goroutines never return errors (captured in results), so Wait only
	// returns nil or a context cancellation which is safe to ignore.
	_ = g.Wait()

	// Restore config order so the table is stable across runs.
	ordered := make([]remoteResult, len(results))
	for idx := range results {
		ordered[results[idx].index] = results[idx]
	}
	return ordered
}

// filterTrackingFailures keeps only the rows that are not tracking_ok — the
// repos with no remote, no upstream, or an error.
func filterTrackingFailures(results []remoteResult) []remoteResult {
	filtered := make([]remoteResult, 0, len(results))
	for i := range results {
		if !results[i].trackingOK {
			filtered = append(filtered, results[i])
		}
	}
	return filtered
}

// originURL returns the URL of the origin remote, or any remote if origin is
// absent (first by sorted name), or "-" when there are no remotes.
func originURL(remotes map[string]string) string {
	if url, ok := remotes["origin"]; ok {
		return url
	}
	names := make([]string, 0, len(remotes))
	for name := range remotes {
		names = append(names, name)
	}
	if len(names) == 0 {
		return "-"
	}
	sort.Strings(names)
	return remotes[names[0]]
}

// remoteRowOf maps a collected result to a renderable table row.
func remoteRowOf(r *remoteResult) output.RemoteRow {
	row := output.RemoteRow{
		Name:     r.name,
		Origin:   originURL(r.remotes),
		Upstream: r.upstream,
	}
	if row.Upstream == "" {
		row.Upstream = "-"
	}

	switch {
	case r.err != "":
		row.Tracking = output.SymConflict + " " + r.flag
		row.State = output.StateConflict
	case r.trackingOK:
		row.Tracking = output.SymClean + " ok"
		row.State = output.StateClean
	default:
		row.Tracking = output.SymWarning + " " + r.flag
		row.State = output.StateDirty
	}
	return row
}

type remotesJSON struct {
	Name       string            `json:"name"`
	Path       string            `json:"path"`
	Remotes    map[string]string `json:"remotes"`
	Upstream   string            `json:"upstream"`
	TrackingOK bool              `json:"tracking_ok"`
	Error      string            `json:"error,omitempty"`
}

func renderRemotesJSON(w io.Writer, results []remoteResult) error {
	entries := make([]remotesJSON, len(results))
	for i := range results {
		r := &results[i]
		remotes := r.remotes
		if remotes == nil {
			remotes = map[string]string{}
		}
		entries[i] = remotesJSON{
			Name:       r.name,
			Path:       r.path,
			Remotes:    remotes,
			Upstream:   r.upstream,
			TrackingOK: r.trackingOK,
			Error:      r.err,
		}
	}
	return output.RenderJSON(w, entries)
}
