package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"

	"github.com/CelikE/soko/internal/git"
	"github.com/CelikE/soko/internal/output"
)

const stashMessage = "soko stash"

type stashResult struct {
	index   int
	name    string
	path    string
	action  string
	success bool
	message string
}

func newStashCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stash [repos...]",
		Short: "Stash uncommitted changes across repos",
		Long: `Stash all dirty repos in one command. Clean repos are silently skipped.
Use "soko stash pop" to restore only stashes created by soko.`,
		Example: `  soko stash
  soko stash auth
  soko stash pop
  soko stash --tag backend`,
		Args: cobra.ArbitraryArgs,
		RunE: runStashPush,
	}

	cmd.AddCommand(newStashPopCmd())

	cmd.Flags().StringSlice("tag", nil, "filter by tag (can be repeated, combines with OR)")
	_ = cmd.RegisterFlagCompletionFunc("tag", tagCompletionFunc())

	return cmd
}

func newStashPopCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pop",
		Short: "Pop soko-created stashes across repos",
		RunE:  runStashPop,
	}

	cmd.Flags().StringSlice("tag", nil, "filter by tag (can be repeated, combines with OR)")
	_ = cmd.RegisterFlagCompletionFunc("tag", tagCompletionFunc())

	return cmd
}

func runStashPush(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	w := cmd.OutOrStdout()

	cfg, repos, err := loadReposWithTagFilter(cmd)
	if err != nil {
		return err
	}

	if len(args) > 0 {
		repos = matchReposByName(repos, args)
		if len(repos) == 0 {
			output.Info(w, fmt.Sprintf("no repos found matching: %s", strings.Join(args, ", ")))
			return nil
		}
	}

	if len(repos) == 0 {
		output.Info(w, noReposMessage(len(cfg.Repos)))
		return nil
	}

	jsonFlag, _ := cmd.Flags().GetBool("json")

	results := make([]stashResult, 0, len(repos))
	var mu sync.Mutex

	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(maxConcurrency)

	for i, repo := range repos {
		g.Go(func() error {
			r := stashResult{index: i, name: repo.Name, path: repo.Path, action: "push"}

			if !pathExists(repo.Path) {
				r.message = "path not found"
				mu.Lock()
				results = append(results, r)
				mu.Unlock()
				return nil
			}

			statusOut, err := git.Run(ctx, repo.Path, "status", "--porcelain")
			if err != nil || statusOut == "" {
				// Clean repo or error — skip.
				mu.Lock()
				results = append(results, r)
				mu.Unlock()
				return nil
			}

			if _, err := git.Run(ctx, repo.Path, "stash", "push", "-m", stashMessage); err != nil {
				r.message = err.Error()
				mu.Lock()
				results = append(results, r)
				mu.Unlock()
				return nil
			}

			r.success = true
			r.message = "stashed"
			mu.Lock()
			results = append(results, r)
			mu.Unlock()
			return nil
		})
	}

	_ = g.Wait()

	ordered := make([]stashResult, len(results))
	for idx := range results {
		ordered[results[idx].index] = results[idx]
	}

	// Filter to only repos that had something to report.
	var visible []stashResult
	for _, r := range ordered {
		if r.success || r.message != "" {
			visible = append(visible, r)
		}
	}

	if len(visible) == 0 {
		if jsonFlag {
			_, _ = fmt.Fprintln(w, "[]")
			return nil
		}
		output.Info(w, "all repos clean")
		return nil
	}

	if jsonFlag {
		return renderStashJSON(w, visible)
	}

	var stashed, failed int
	for _, r := range visible {
		if r.success {
			output.Confirm(w, fmt.Sprintf("%s — stashed", r.name))
			stashed++
		} else {
			output.Fail(w, fmt.Sprintf("%s — %s", r.name, r.message))
			failed++
		}
	}

	_, _ = fmt.Fprintln(w)
	output.Info(w, fmt.Sprintf("%d %s stashed", stashed, output.Plural(stashed, "repo")))

	if failed > 0 {
		return fmt.Errorf("%d %s failed to stash", failed, output.Plural(failed, "repo"))
	}

	return nil
}

func runStashPop(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	w := cmd.OutOrStdout()

	cfg, repos, err := loadReposWithTagFilter(cmd)
	if err != nil {
		return err
	}

	if len(repos) == 0 {
		output.Info(w, noReposMessage(len(cfg.Repos)))
		return nil
	}

	jsonFlag, _ := cmd.Flags().GetBool("json")

	results := make([]stashResult, 0, len(repos))
	var mu sync.Mutex

	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(maxConcurrency)

	for i, repo := range repos {
		g.Go(func() error {
			r := stashResult{index: i, name: repo.Name, path: repo.Path, action: "pop"}

			if !pathExists(repo.Path) {
				r.message = "path not found"
				mu.Lock()
				results = append(results, r)
				mu.Unlock()
				return nil
			}

			// Check if top stash was created by soko.
			msg, err := git.Run(ctx, repo.Path, "stash", "list", "-1")
			if err != nil || !strings.Contains(msg, stashMessage) {
				// No soko stash — skip.
				mu.Lock()
				results = append(results, r)
				mu.Unlock()
				return nil
			}

			if _, err := git.Run(ctx, repo.Path, "stash", "pop"); err != nil {
				r.message = err.Error()
				mu.Lock()
				results = append(results, r)
				mu.Unlock()
				return nil
			}

			r.success = true
			r.message = "restored"
			mu.Lock()
			results = append(results, r)
			mu.Unlock()
			return nil
		})
	}

	_ = g.Wait()

	ordered := make([]stashResult, len(results))
	for idx := range results {
		ordered[results[idx].index] = results[idx]
	}

	var visible []stashResult
	for _, r := range ordered {
		if r.success || r.message != "" {
			visible = append(visible, r)
		}
	}

	if len(visible) == 0 {
		output.Info(w, "no soko stashes found")
		return nil
	}

	if jsonFlag {
		return renderStashJSON(w, visible)
	}

	var restored, failed int
	for _, r := range visible {
		if r.success {
			output.Confirm(w, fmt.Sprintf("%s — restored", r.name))
			restored++
		} else {
			output.Fail(w, fmt.Sprintf("%s — %s", r.name, r.message))
			failed++
		}
	}

	_, _ = fmt.Fprintln(w)
	output.Info(w, fmt.Sprintf("%d %s restored", restored, output.Plural(restored, "repo")))

	if failed > 0 {
		return fmt.Errorf("%d %s failed to pop stash", failed, output.Plural(failed, "repo"))
	}

	return nil
}

type stashJSON struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	Action string `json:"action"`
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

func renderStashJSON(w io.Writer, results []stashResult) error {
	entries := make([]stashJSON, len(results))
	for i, r := range results {
		entries[i] = stashJSON{
			Name:   r.name,
			Path:   r.path,
			Action: r.action,
		}
		if r.success {
			entries[i].Status = r.message
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
