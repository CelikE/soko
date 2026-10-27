package cli

import (
	"bufio"
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
	"github.com/CelikE/soko/internal/journal"
	"github.com/CelikE/soko/internal/output"
)

type cleanResult struct {
	Name      string               `json:"name"`
	Path      string               `json:"path"`
	Branches  []string             `json:"branches"`
	Deleted   []string             `json:"deleted,omitempty"`
	Failed    []cleanBranchFailure `json:"failed,omitempty"`
	Pruned    bool                 `json:"pruned,omitempty"`
	Error     string               `json:"error,omitempty"`
	ErrorCode string               `json:"error_code,omitempty"`
}

type cleanBranchFailure struct {
	Branch    string `json:"branch"`
	Error     string `json:"error"`
	ErrorCode string `json:"error_code"`
}

func newCleanCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "clean [repos...]",
		Short: "Delete merged branches across repos",
		Long: `Find and delete branches that have been merged into the default branch
(main/master). Skips the default branch and branches checked out in any worktree.

Use --dry-run to preview what would be deleted. Branch deletion failures cause
a non-zero exit status, including with --json. JSON keeps the planned branches
and adds deleted and failed outcomes; failures include error and error_code.`,
		Example: `  soko clean --dry-run               # preview stale branches
  soko clean                         # delete with confirmation
  soko clean --force                 # delete without confirmation
  soko clean --prune                 # also prune stale remote refs
  soko clean --tag backend           # only backend repos`,
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
					return renderNoMatchingRepos(cmd, args)
				}
			}

			if len(repos) == 0 {
				return renderNoRepos(cmd, len(cfg.Repos))
			}

			dryRun, _ := cmd.Flags().GetBool("dry-run")
			prune, _ := cmd.Flags().GetBool("prune")
			force, _ := cmd.Flags().GetBool("force")
			jsonFlag, _ := cmd.Flags().GetBool("json")
			selectFlag, _ := cmd.Flags().GetBool("select")

			if jsonFlag && selectFlag {
				return fmt.Errorf("--select cannot be combined with --json")
			}
			if jsonFlag && !force && !dryRun {
				return fmt.Errorf("--json requires --force or --dry-run")
			}

			results := findStaleBranches(ctx, repos, prune)

			// Filter to repos that have stale branches.
			var withBranches []cleanResult
			for i := range results {
				r := &results[i]
				if len(r.Branches) > 0 || r.Error != "" {
					withBranches = append(withBranches, *r)
				}
			}

			if len(withBranches) == 0 {
				if jsonFlag {
					_, _ = fmt.Fprintln(w, "[]")
					return nil
				}
				output.Info(w, "all clean — no merged branches to delete")
				return nil
			}

			// Optional interactive refinement: let the user deselect repos
			// before anything is deleted. Narrow-only — it can never add a repo.
			if selectFlag {
				entries := make([]config.RepoEntry, len(withBranches))
				for i := range withBranches {
					r := &withBranches[i]
					entries[i] = config.RepoEntry{Name: r.Name, Path: r.Path}
				}
				chosen, ok := selectRepos(cmd, "Select repos to clean (space toggles, enter confirms):", entries)
				if !ok {
					_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "  aborted")
					return nil
				}
				withBranches = filterCleanResults(withBranches, chosen)
			}

			if jsonFlag && dryRun {
				return output.RenderJSON(w, withBranches)
			}

			if !jsonFlag {
				renderCleanTable(w, withBranches)
			}

			if dryRun {
				return nil
			}

			// Confirm unless --force or --json (machine consumers pass --force explicitly).
			if !force {
				totalBranches := 0
				for i := range withBranches {
					totalBranches += len(withBranches[i].Branches)
				}
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(),
					"\n  delete %d %s across %d %s? [y/N] ",
					totalBranches, output.Plural(totalBranches, "branch"),
					len(withBranches), output.Plural(len(withBranches), "repo"))

				scanner := bufio.NewScanner(cmd.InOrStdin())
				if !scanner.Scan() {
					return nil
				}
				answer := strings.TrimSpace(strings.ToLower(scanner.Text()))
				if answer != "y" && answer != "yes" {
					_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "  aborted")
					return nil
				}
			}

			refs, deleted, failed := deleteBranches(ctx, withBranches)

			// Record the deletion so soko undo can recreate the branches. Journal
			// failure must never fail the clean itself — warn and move on.
			if len(refs) > 0 {
				entry := journal.Entry{
					Op:   journal.OpClean,
					Time: time.Now(),
					Summary: fmt.Sprintf("deleted %d %s across %d %s",
						deleted, output.Plural(deleted, "branch"),
						len(withBranches), output.Plural(len(withBranches), "repo")),
					Branches: refs,
				}
				if err := journal.Append(&entry); err != nil {
					output.Warn(cmd.ErrOrStderr(), fmt.Sprintf("could not record undo journal: %v", err))
				}
			}

			if jsonFlag {
				if err := output.RenderJSON(w, withBranches); err != nil {
					return err
				}
			}

			// A partial failure is a real failure: return an error so it stays
			// visible (the error path is never gated by --quiet) and the exit
			// code is non-zero, matching pull/fetch/stash.
			if failed > 0 {
				return fmt.Errorf("deleted %d %s, %d failed to delete",
					deleted, output.Plural(deleted, "branch"), failed)
			}
			if jsonFlag {
				return nil
			}

			_, _ = fmt.Fprintln(w)
			output.Confirm(w, fmt.Sprintf("deleted %d stale %s across %d %s",
				deleted, output.Plural(deleted, "branch"),
				len(withBranches), output.Plural(len(withBranches), "repo")))

			return nil
		},
	}

	cmd.Flags().Bool("dry-run", false, "show what would be deleted without deleting")
	cmd.Flags().Bool("prune", false, "also prune stale remote tracking refs")
	cmd.Flags().Bool("force", false, "skip confirmation prompt")
	cmd.Flags().StringSlice("tag", nil, "filter by tag (can be repeated, combines with OR)")
	_ = cmd.RegisterFlagCompletionFunc("tag", tagCompletionFunc())
	addSelectFlag(cmd)

	return cmd
}

// filterCleanResults keeps only the results whose repo is in the chosen set,
// matched by path (the picker's stable identity).
func filterCleanResults(results []cleanResult, chosen []config.RepoEntry) []cleanResult {
	keep := make(map[string]bool, len(chosen))
	for _, c := range chosen {
		keep[c.Path] = true
	}
	filtered := make([]cleanResult, 0, len(chosen))
	for i := range results {
		r := &results[i]
		if keep[r.Path] {
			filtered = append(filtered, *r)
		}
	}
	return filtered
}

// defaultBranch detects the default branch for a repo.
func defaultBranch(ctx context.Context, repoPath string) string {
	// Try symbolic-ref from remote HEAD.
	ref, err := git.Run(ctx, repoPath, "symbolic-ref", "refs/remotes/origin/HEAD")
	if err == nil {
		ref = strings.TrimPrefix(ref, "refs/remotes/origin/")
		if ref != "" {
			return ref
		}
	}

	// Fallback: check if main or master exists.
	for _, name := range []string{"main", "master"} {
		if _, err := git.Run(ctx, repoPath, "rev-parse", "--verify", name); err == nil {
			return name
		}
	}

	return ""
}

// findStaleBranches discovers merged branches across repos in parallel.
func findStaleBranches(ctx context.Context, repos []config.RepoEntry, prune bool) []cleanResult {
	results := make([]cleanResult, len(repos))

	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(maxConcurrency)

	for i, repo := range repos {
		g.Go(func() error {
			r := cleanResult{Name: repo.Name, Path: repo.Path}

			if !pathExists(repo.Path) {
				r.Error = "path not found"
				r.ErrorCode = codePathMissing
				results[i] = r
				return nil
			}

			if prune {
				_, _ = git.Run(ctx, repo.Path, "remote", "prune", "origin")
				r.Pruned = true
			}

			defBranch := defaultBranch(ctx, repo.Path)
			if defBranch == "" {
				r.Error = "could not detect default branch"
				r.ErrorCode = codeGitFailure
				results[i] = r
				return nil
			}

			// Enumerate real local refs, without detached-HEAD pseudo-branches
			// or display markers. Git also identifies branches checked out in
			// any worktree, including checkouts not registered with soko.
			merged, err := git.Run(ctx, repo.Path, "for-each-ref", "--merged="+defBranch,
				"--format=%(refname)%00%(worktreepath)", "refs/heads/")
			if err != nil {
				r.Error = "failed to list merged branches"
				r.ErrorCode = gitErrorCode(err)
				results[i] = r
				return nil
			}

			for _, line := range strings.Split(merged, "\n") {
				ref, worktreePath, ok := strings.Cut(line, "\x00")
				if !ok || worktreePath != "" {
					continue
				}
				branch := strings.TrimPrefix(ref, "refs/heads/")
				if branch == "" || branch == defBranch {
					continue
				}
				// Also protect common branch names.
				if branch == "main" || branch == "master" || branch == "develop" {
					continue
				}
				r.Branches = append(r.Branches, branch)
			}

			results[i] = r
			return nil
		})
	}

	_ = g.Wait()
	return results
}

// renderCleanTable prints the stale branches table.
func renderCleanTable(w io.Writer, results []cleanResult) {
	nameWidth := len("REPO")
	for i := range results {
		r := &results[i]
		if len(r.Name) > nameWidth {
			nameWidth = len(r.Name)
		}
	}
	nameWidth += 2

	header := fmt.Sprintf("  %-*s %s", nameWidth, "REPO", "STALE BRANCHES")
	_, _ = fmt.Fprintln(w, output.Dim(header))
	_, _ = fmt.Fprintln(w, output.Dim("  "+strings.Repeat("─", len(header)-2)))

	totalBranches := 0
	for i := range results {
		r := &results[i]
		if r.Error != "" {
			_, _ = fmt.Fprintf(w, "  %-*s %s\n", nameWidth, r.Name, output.Red(r.Error))
			continue
		}
		maxShow := 5
		shown := r.Branches
		remaining := 0
		if len(shown) > maxShow {
			remaining = len(shown) - maxShow
			shown = shown[:maxShow]
		}
		line := strings.Join(shown, ", ")
		if remaining > 0 {
			line += fmt.Sprintf(" (+%d more)", remaining)
		}
		_, _ = fmt.Fprintf(w, "  %-*s %s\n", nameWidth, r.Name, line)
		totalBranches += len(r.Branches)
	}

	if output.Quiet() {
		return
	}
	_, _ = fmt.Fprintf(w, "\n  %s\n", output.Dim(fmt.Sprintf(
		"%d stale %s across %d %s",
		totalBranches, output.Plural(totalBranches, "branch"),
		len(results), output.Plural(len(results), "repo"))))
}

// deleteBranches deletes stale branches in parallel across repositories, but
// sequentially within each repository to avoid packed-ref lock contention.
// It returns the journal refs for every branch it actually deleted (repo,
// path, branch, pre-delete SHA) plus deleted/failed counts. The SHAs let
// soko undo recreate the branches exactly where they were. Each result also
// records its actual deletion outcomes without changing the planned branches.
func deleteBranches(ctx context.Context, results []cleanResult) (refs []journal.BranchRef, deleted, failed int) {
	var mu sync.Mutex
	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(maxConcurrency)

	for i := range results {
		r := &results[i]
		if r.Error != "" {
			continue
		}
		g.Go(func() error {
			for _, branch := range r.Branches {
				// Capture the exact local tip, never a same-named tag. Refuse
				// deletion if its preimage cannot be verified for undo.
				sha, err := git.Run(ctx, r.Path, "rev-parse", "--verify", "refs/heads/"+branch)
				if err != nil {
					err = fmt.Errorf("could not capture branch tip: %w", err)
				} else {
					_, err = git.Run(ctx, r.Path, "branch", "-d", branch)
				}
				mu.Lock()
				if err != nil {
					failed++
					r.Failed = append(r.Failed, cleanBranchFailure{
						Branch:    branch,
						Error:     err.Error(),
						ErrorCode: gitErrorCode(err),
					})
				} else {
					deleted++
					r.Deleted = append(r.Deleted, branch)
					refs = append(refs, journal.BranchRef{
						Repo:   r.Name,
						Path:   r.Path,
						Branch: branch,
						SHA:    sha,
					})
				}
				mu.Unlock()
			}
			if len(r.Failed) > 0 {
				r.Error = fmt.Sprintf("%d %s failed to delete", len(r.Failed), output.Plural(len(r.Failed), "branch"))
				r.ErrorCode = codeGitFailure
			}
			return nil
		})
	}

	_ = g.Wait()
	return refs, deleted, failed
}
