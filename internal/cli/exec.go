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
	iexec "github.com/CelikE/soko/internal/exec"

	"github.com/CelikE/soko/internal/output"
)

type execResult struct {
	index     int
	name      string
	path      string
	stdout    string
	stderr    string
	exitCode  int
	err       string
	errorCode string
	elapsed   time.Duration
}

// newExecCmd creates the cobra command for soko exec.
func newExecCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "exec -- <command> [args...]",
		Short: "Run a command in all registered repos",
		Long: `Run an arbitrary command in every registered repo. Everything after --
is the command to execute. Output is shown per repo with separators.

By default commands run in parallel. Use --seq for sequential execution.`,
		Example: `  soko exec -- git stash
  soko exec -- make test
  soko exec --seq -- git pull`,
		RunE: func(cmd *cobra.Command, args []string) error {
			w := cmd.OutOrStdout()

			if len(args) == 0 {
				return fmt.Errorf("no command specified — usage: soko exec -- <command> [args...]")
			}

			cfg, repos, err := loadReposWithTagFilter(cmd)
			if err != nil {
				return err
			}

			if len(repos) == 0 {
				output.Info(w, noReposMessage(len(cfg.Repos)))
				return nil
			}

			noWorktrees, _ := cmd.Flags().GetBool("no-worktrees")
			if noWorktrees {
				repos = config.FilterNoWorktrees(repos)
			}

			seqFlag, _ := cmd.Flags().GetBool("seq")
			jsonFlag, _ := cmd.Flags().GetBool("json")

			if !jsonFlag {
				output.Info(w, fmt.Sprintf("running: %s", strings.Join(args, " ")))
				_, _ = fmt.Fprintln(w)
			}

			wallStart := time.Now()
			var results []execResult
			if seqFlag {
				results = execSequential(cmd, repos, args, w, jsonFlag)
			} else {
				results = execParallel(cmd, repos, args)
			}
			wall := time.Since(wallStart)

			timingRows := make([]output.TimingRow, len(results))
			for i := range results {
				timingRows[i] = output.TimingRow{Name: results[i].name, Duration: results[i].elapsed}
			}

			if jsonFlag {
				return renderExecJSON(w, results, timingRows, wall)
			}

			// For parallel, print buffered output now.
			if !seqFlag {
				for i := range results {
					printExecResult(w, &results[i])
				}
			}

			var succeeded, failed int
			for i := range results {
				if results[i].exitCode == 0 && results[i].err == "" {
					succeeded++
				} else {
					failed++
				}
			}

			output.RenderActionSummary(w, len(results), succeeded, failed)
			output.RenderTiming(w, timingRows, wall, maxConcurrency)

			if failed > 0 {
				return fmt.Errorf("%d %s failed", failed, output.Plural(failed, "repo"))
			}
			return nil
		},
	}

	cmd.Flags().Bool("seq", false, "run sequentially instead of in parallel")
	cmd.Flags().Bool("no-worktrees", false, "skip worktree entries, only run on parent repos")
	cmd.Flags().StringSlice("tag", nil, "filter by tag (can be repeated, combines with OR)")
	_ = cmd.RegisterFlagCompletionFunc("tag", tagCompletionFunc())

	return cmd
}

func execParallel(cmd *cobra.Command, repos []config.RepoEntry, args []string) []execResult {
	ctx := cmd.Context()
	results := make([]execResult, len(repos))
	var mu sync.Mutex

	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(maxConcurrency)

	for i, repo := range repos {
		g.Go(func() error {
			r := execOne(ctx, i, &repo, args)
			mu.Lock()
			results[i] = r
			mu.Unlock()
			return nil
		})
	}

	// Goroutines never return errors (captured in results), so Wait only
	// returns nil or a context cancellation which is safe to ignore.
	_ = g.Wait()
	return results
}

func execSequential(cmd *cobra.Command, repos []config.RepoEntry, args []string, w io.Writer, jsonOut bool) []execResult {
	ctx := cmd.Context()
	results := make([]execResult, 0, len(repos))

	for i, repo := range repos {
		r := execOne(ctx, i, &repo, args)
		results = append(results, r)

		// Print immediately for sequential non-JSON.
		if !jsonOut {
			printExecResult(w, &r)
		}
	}

	return results
}

func execOne(ctx context.Context, index int, repo *config.RepoEntry, args []string) (r execResult) {
	start := time.Now()
	// Named return + defer times every exit path (missing, spawn error, success)
	// in one place, covering both the parallel and sequential callers.
	defer func() { r.elapsed = time.Since(start) }()

	r = execResult{index: index, name: repo.Name, path: repo.Path}

	if !pathExists(repo.Path) {
		r.err = "path not found"
		r.errorCode = codePathMissing
		r.exitCode = 1
		return r
	}

	result, err := iexec.RunCommand(ctx, repo.Path, args)
	if err != nil {
		// Failed to spawn the binary — a non-git failure, not an exit code.
		r.err = err.Error()
		r.errorCode = codeUnknown
		r.exitCode = 1
		return r
	}

	r.stdout = result.Stdout
	r.stderr = result.Stderr
	r.exitCode = result.ExitCode
	return r
}

func printExecResult(w io.Writer, r *execResult) {
	_, _ = fmt.Fprintf(w, "  %s %s %s\n",
		output.Dim("───"), r.name, output.Dim(r.path))

	if r.err != "" {
		_, _ = fmt.Fprintln(w, "  "+output.Red(r.err))
		return
	}

	combined := strings.TrimRight(r.stdout+r.stderr, "\n")
	if combined != "" {
		_, _ = fmt.Fprintln(w, combined)
	}
	_, _ = fmt.Fprintln(w)
}

type execJSON struct {
	Name       string `json:"name"`
	Path       string `json:"path"`
	ExitCode   int    `json:"exit_code"`
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	Error      string `json:"error,omitempty"`
	ErrorCode  string `json:"error_code,omitempty"`
	DurationMS int64  `json:"duration_ms,omitempty"`
}

func renderExecJSON(w io.Writer, results []execResult, rows []output.TimingRow, wall time.Duration) error {
	entries := make([]execJSON, len(results))
	for i := range results {
		r := &results[i]
		entries[i] = execJSON{
			Name:     r.name,
			Path:     r.path,
			ExitCode: r.exitCode,
			Stdout:   r.stdout,
			Stderr:   r.stderr,
			Error:    r.err,
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
