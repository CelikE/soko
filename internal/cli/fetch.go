package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"

	"github.com/CelikE/soko/internal/git"
	"github.com/CelikE/soko/internal/output"
)

type fetchResult struct {
	index   int
	name    string
	path    string
	success bool
	message string
}

// newFetchCmd creates the cobra command for soko fetch.
func newFetchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "fetch",
		Short: "Fetch all registered repos",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			w := cmd.OutOrStdout()

			cfg, repos, err := loadReposWithTagFilter(cmd)
			if err != nil {
				return err
			}

			if len(repos) == 0 {
				if len(cfg.Repos) == 0 {
					output.Info(w, "no repos registered yet — cd into a repo and run: soko init")
				} else {
					output.Info(w, fmt.Sprintf("no repos match the tag filter (%d repos registered)", len(cfg.Repos)))
				}
				return nil
			}

			pruneFlag, _ := cmd.Flags().GetBool("prune")

			results := make([]fetchResult, 0, len(repos))
			var mu sync.Mutex

			g, ctx := errgroup.WithContext(ctx)
			g.SetLimit(maxConcurrency)

			for i, repo := range repos {
				g.Go(func() error {
					r := fetchResult{index: i, name: repo.Name, path: repo.Path}

					if _, statErr := os.Stat(repo.Path); os.IsNotExist(statErr) {
						r.message = "path not found"
						mu.Lock()
						results = append(results, r)
						mu.Unlock()
						return nil
					}

					if fetchErr := git.Fetch(ctx, repo.Path, pruneFlag); fetchErr != nil {
						r.message = fetchErr.Error()
						mu.Lock()
						results = append(results, r)
						mu.Unlock()
						return nil
					}

					r.success = true
					r.message = "fetched"
					mu.Lock()
					results = append(results, r)
					mu.Unlock()
					return nil
				})
			}

			_ = g.Wait()

			// Restore config order.
			ordered := make([]fetchResult, len(results))
			for idx := range results {
				ordered[results[idx].index] = results[idx]
			}

			jsonFlag, _ := cmd.Flags().GetBool("json")
			if jsonFlag {
				return renderFetchJSON(w, ordered)
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

			if failed > 0 {
				return fmt.Errorf("%d repos failed to fetch", failed)
			}

			return nil
		},
	}

	cmd.Flags().Bool("prune", false, "pass --prune to git fetch to clean up stale remote refs")
	cmd.Flags().StringSlice("tag", nil, "filter by tag (can be repeated, combines with OR)")
	_ = cmd.RegisterFlagCompletionFunc("tag", tagCompletionFunc())

	return cmd
}

type fetchJSON struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

func renderFetchJSON(w io.Writer, results []fetchResult) error {
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
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(entries); err != nil {
		return fmt.Errorf("encoding json: %w", err)
	}
	return nil
}
