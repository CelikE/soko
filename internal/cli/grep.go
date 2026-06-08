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

type grepResult struct {
	index   int
	name    string
	path    string
	matches []git.Match
	missing bool
	err     string
}

// newGrepCmd creates the cobra command for soko grep.
func newGrepCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "grep <pattern> [repos...]",
		Short: "Search file content across repos with git grep",
		Long: `Run git grep across all (or filtered) registered repos in parallel and show
matches grouped by repo. The pattern is a fixed string by default; pass
--regexp to treat it as a POSIX extended regular expression.

Read-only and parallel — a repo with no match simply contributes nothing.`,
		Example: `  soko grep handleAuth                  # search every registered repo
  soko grep handleAuth auth backend     # only these repos
  soko grep handleAuth --tag go         # only repos tagged "go"
  soko grep "func .*Handler" --regexp   # extended regex
  soko grep TODO -i                     # case-insensitive
  soko grep config.yaml --files-only    # matching file paths only
  soko grep handleAuth --json           # machine-readable, grouped by repo`,
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: repoNameCompletionFunc(),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			w := cmd.OutOrStdout()

			pattern := args[0]
			repoArgs := args[1:]

			cfg, repos, err := loadReposWithTagFilter(cmd)
			if err != nil {
				return err
			}

			if len(repoArgs) > 0 {
				repos = findReposMatching(repos, repoArgs)
				if len(repos) == 0 {
					output.Info(w, fmt.Sprintf("no repos found matching: %s", strings.Join(repoArgs, ", ")))
					return nil
				}
			}

			if len(repos) == 0 {
				output.Info(w, noReposMessage(len(cfg.Repos)))
				return nil
			}

			if noWorktrees, _ := cmd.Flags().GetBool("no-worktrees"); noWorktrees {
				repos = config.FilterNoWorktrees(repos)
			}

			ignoreCase, _ := cmd.Flags().GetBool("ignore-case")
			regex, _ := cmd.Flags().GetBool("regexp")
			filesOnly, _ := cmd.Flags().GetBool("files-only")
			jsonFlag, _ := cmd.Flags().GetBool("json")

			var prog *output.Progress
			if !jsonFlag {
				prog = output.NewProgress(cmd.ErrOrStderr(), "Searching repositories", len(repos))
			}

			results := make([]grepResult, 0, len(repos))
			var mu sync.Mutex

			g, ctx := errgroup.WithContext(ctx)
			g.SetLimit(maxConcurrency)

			for i, repo := range repos {
				g.Go(func() error {
					r := grepResult{index: i, name: repo.Name, path: repo.Path}

					if !pathExists(repo.Path) {
						r.missing = true
					} else if matches, grepErr := git.Grep(ctx, repo.Path, pattern, regex, ignoreCase, filesOnly); grepErr != nil {
						r.err = grepErr.Error()
					} else {
						r.matches = matches
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

			// Goroutines never return errors (captured per-repo), so Wait only
			// returns nil or a context cancellation, which is safe to ignore.
			_ = g.Wait()

			if prog != nil {
				prog.Done()
			}

			// Restore config order so output is deterministic.
			ordered := make([]grepResult, len(results))
			for idx := range results {
				ordered[results[idx].index] = results[idx]
			}

			var failed, missing int
			for i := range ordered {
				if ordered[i].err != "" {
					failed++
				}
				if ordered[i].missing {
					missing++
				}
			}

			if jsonFlag {
				if err := renderGrepJSON(w, ordered); err != nil {
					return err
				}
				if failed > 0 {
					return fmt.Errorf("%d %s failed to search", failed, output.Plural(failed, "repo"))
				}
				return nil
			}

			groups := make([]output.GrepGroup, 0, len(ordered))
			var repoCount, matchCount int
			for i := range ordered {
				r := &ordered[i]
				if r.err != "" {
					output.Fail(w, fmt.Sprintf("%s: %s", r.name, r.err))
					continue
				}
				if len(r.matches) == 0 {
					continue
				}
				groups = append(groups, output.GrepGroup{Repo: r.name, Matches: toGrepMatches(r.matches)})
				repoCount++
				matchCount += len(r.matches)
			}

			if len(groups) == 0 {
				if failed == 0 {
					output.Info(w, fmt.Sprintf("no matches in %d %s", len(repos), output.Plural(len(repos), "repo")))
				}
				renderMissingHint(w, missing)
				if failed > 0 {
					return fmt.Errorf("%d %s failed to search", failed, output.Plural(failed, "repo"))
				}
				return nil
			}

			output.RenderGrepResults(w, groups, filesOnly)
			output.RenderGrepSummary(w, repoCount, matchCount, filesOnly)
			renderMissingHint(w, missing)

			if failed > 0 {
				return fmt.Errorf("%d %s failed to search", failed, output.Plural(failed, "repo"))
			}
			return nil
		},
	}

	cmd.Flags().BoolP("ignore-case", "i", false, "case-insensitive match")
	cmd.Flags().BoolP("regexp", "e", false, "treat the pattern as a POSIX extended regex (default: fixed string)")
	cmd.Flags().Bool("files-only", false, "list matching file paths only, not lines")
	cmd.Flags().Bool("no-worktrees", false, "skip worktree entries, only search parent repos")
	cmd.Flags().StringSlice("tag", nil, "filter by tag (can be repeated, combines with OR)")
	_ = cmd.RegisterFlagCompletionFunc("tag", tagCompletionFunc())

	return cmd
}

func toGrepMatches(matches []git.Match) []output.GrepMatch {
	out := make([]output.GrepMatch, len(matches))
	for i, m := range matches {
		out[i] = output.GrepMatch{File: m.File, Line: m.Line, Text: m.Text}
	}
	return out
}

type matchJSON struct {
	File string `json:"file"`
	Line int    `json:"line"`
	Text string `json:"text"`
}

type grepJSON struct {
	Repo    string      `json:"repo"`
	Path    string      `json:"path"`
	Matches []matchJSON `json:"matches"`
}

// renderGrepJSON emits one entry per repo that has matches; repos with zero
// matches (and missing/errored repos) are omitted from the array.
func renderGrepJSON(w io.Writer, results []grepResult) error {
	entries := make([]grepJSON, 0, len(results))
	for i := range results {
		r := &results[i]
		if len(r.matches) == 0 {
			continue
		}
		matches := make([]matchJSON, len(r.matches))
		for j, m := range r.matches {
			matches[j] = matchJSON{File: m.File, Line: m.Line, Text: m.Text}
		}
		entries = append(entries, grepJSON{Repo: r.name, Path: r.path, Matches: matches})
	}
	return output.RenderJSON(w, entries)
}
