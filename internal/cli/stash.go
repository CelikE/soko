package cli

import (
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
	index     int
	name      string
	path      string
	action    string
	success   bool
	message   string
	errorCode string
}

func newStashCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stash [repos...]",
		Short: "Stash uncommitted changes across repos",
		Long: `Stash tracked changes and untracked files across all dirty repos in one command.
Clean repos are silently skipped. Ignored files are left untouched.
Use "soko stash pop" to restore only stashes created by soko.`,
		Example: `  soko stash
  soko stash auth
  soko stash pop
  soko stash pop auth
  soko stash --tag backend`,
		Args:              cobra.ArbitraryArgs,
		ValidArgsFunction: repoNameCompletionFunc(),
		RunE:              runStashPush,
	}

	cmd.AddCommand(newStashPopCmd())

	cmd.Flags().StringSlice("tag", nil, "filter by tag (can be repeated, combines with OR)")
	_ = cmd.RegisterFlagCompletionFunc("tag", tagCompletionFunc())

	return cmd
}

func newStashPopCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pop [repos...]",
		Short: "Pop soko-created stashes across repos",
		Long: `Restore only stashes created by soko. Repos without a soko stash on top are skipped.
Filter repos by name or prefix, optionally combined with --tag. With no repo names,
restore stashes across all repos matching the tag filter.`,
		Args:              cobra.ArbitraryArgs,
		ValidArgsFunction: repoNameCompletionFunc(),
		RunE:              runStashPop,
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
		repos = findReposMatching(repos, args)
		if len(repos) == 0 {
			return renderNoMatchingRepos(cmd, args)
		}
	}

	if len(repos) == 0 {
		return renderNoRepos(cmd, len(cfg.Repos))
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
				r.errorCode = codePathMissing
				mu.Lock()
				results = append(results, r)
				mu.Unlock()
				return nil
			}

			statusOut, err := git.Run(ctx, repo.Path, "status", "--porcelain")
			switch {
			case err != nil:
				r.message = err.Error()
				r.errorCode = gitErrorCode(err)
			case statusOut != "":
				if _, err := git.Run(ctx, repo.Path, "stash", "push", "--include-untracked", "-m", stashMessage); err != nil {
					r.message = err.Error()
					r.errorCode = gitErrorCode(err)
				} else {
					r.success = true
					r.message = "stashed"
				}
			}

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

	var stashed, failed int
	for _, r := range visible {
		if r.success {
			if !jsonFlag {
				output.Confirm(w, fmt.Sprintf("%s — stashed", r.name))
			}
			stashed++
		} else {
			if !jsonFlag {
				output.Fail(w, fmt.Sprintf("%s — %s", r.name, r.message))
			}
			failed++
		}
	}

	if jsonFlag {
		if err := renderStashJSON(w, visible); err != nil {
			return err
		}
	} else {
		_, _ = fmt.Fprintln(w)
		output.Info(w, fmt.Sprintf("%d %s stashed", stashed, output.Plural(stashed, "repo")))
	}

	if failed > 0 {
		return fmt.Errorf("%d %s failed to stash", failed, output.Plural(failed, "repo"))
	}

	return nil
}

func runStashPop(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	w := cmd.OutOrStdout()

	cfg, repos, err := loadReposWithTagFilter(cmd)
	if err != nil {
		return err
	}

	if len(args) > 0 {
		repos = findReposMatching(repos, args)
		if len(repos) == 0 {
			return renderNoMatchingRepos(cmd, args)
		}
	}

	if len(repos) == 0 {
		return renderNoRepos(cmd, len(cfg.Repos))
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
				r.errorCode = codePathMissing
				mu.Lock()
				results = append(results, r)
				mu.Unlock()
				return nil
			}

			// Match the complete message, not a substring in a manual stash
			// or the commit subject included by Git's default WIP message.
			description, err := git.Run(ctx, repo.Path, "stash", "list", "-1", "--format=%B")
			_, message, _ := strings.Cut(description, ": ")
			switch {
			case err != nil:
				r.message = err.Error()
				r.errorCode = gitErrorCode(err)
			case message == stashMessage:
				if _, err := git.Run(ctx, repo.Path, "stash", "pop"); err != nil {
					r.message = err.Error()
					r.errorCode = gitErrorCode(err)
				} else {
					r.success = true
					r.message = "restored"
				}
			}

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
		if jsonFlag {
			_, _ = fmt.Fprintln(w, "[]")
			return nil
		}
		output.Info(w, "no soko stashes found")
		return nil
	}

	var restored, failed int
	for _, r := range visible {
		if r.success {
			if !jsonFlag {
				output.Confirm(w, fmt.Sprintf("%s — restored", r.name))
			}
			restored++
		} else {
			if !jsonFlag {
				output.Fail(w, fmt.Sprintf("%s — %s", r.name, r.message))
			}
			failed++
		}
	}

	if jsonFlag {
		if err := renderStashJSON(w, visible); err != nil {
			return err
		}
	} else {
		_, _ = fmt.Fprintln(w)
		output.Info(w, fmt.Sprintf("%d %s restored", restored, output.Plural(restored, "repo")))
	}

	if failed > 0 {
		return fmt.Errorf("%d %s failed to pop stash", failed, output.Plural(failed, "repo"))
	}

	return nil
}

type stashJSON struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	Action    string `json:"action"`
	Status    string `json:"status"`
	Error     string `json:"error,omitempty"`
	ErrorCode string `json:"error_code,omitempty"`
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
			entries[i].ErrorCode = r.errorCode
		}
	}

	return output.RenderJSON(w, entries)
}
