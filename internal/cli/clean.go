package cli

import (
	"bufio"
	"context"
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

type cleanResult struct {
	Name     string   `json:"name"`
	Path     string   `json:"path"`
	Branches []string `json:"branches"`
	Pruned   bool     `json:"pruned,omitempty"`
	Error    string   `json:"error,omitempty"`
}

func newCleanCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "clean [repos...]",
		Short: "Delete merged branches across repos",
		Long: `Find and delete branches that have been merged into the default branch
(main/master). Skips the current branch and the default branch itself.

Use --dry-run to preview what would be deleted.`,
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
					output.Info(w, fmt.Sprintf("no repos found matching: %s", strings.Join(args, ", ")))
					return nil
				}
			}

			if len(repos) == 0 {
				output.Info(w, noReposMessage(len(cfg.Repos)))
				return nil
			}

			dryRun, _ := cmd.Flags().GetBool("dry-run")
			prune, _ := cmd.Flags().GetBool("prune")
			force, _ := cmd.Flags().GetBool("force")
			jsonFlag, _ := cmd.Flags().GetBool("json")

			results := findStaleBranches(ctx, repos, prune)

			// Filter to repos that have stale branches.
			var withBranches []cleanResult
			for _, r := range results {
				if len(r.Branches) > 0 || r.Error != "" {
					withBranches = append(withBranches, r)
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
				for _, r := range withBranches {
					totalBranches += len(r.Branches)
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

			deleted, failed := deleteBranches(ctx, withBranches)

			if jsonFlag {
				return output.RenderJSON(w, withBranches)
			}

			_, _ = fmt.Fprintln(w)
			if failed > 0 {
				output.Warn(w, fmt.Sprintf("deleted %d %s, %d failed",
					deleted, output.Plural(deleted, "branch"), failed))
			} else {
				output.Confirm(w, fmt.Sprintf("deleted %d stale %s across %d %s",
					deleted, output.Plural(deleted, "branch"),
					len(withBranches), output.Plural(len(withBranches), "repo")))
			}

			return nil
		},
	}

	cmd.Flags().Bool("dry-run", false, "show what would be deleted without deleting")
	cmd.Flags().Bool("prune", false, "also prune stale remote tracking refs")
	cmd.Flags().Bool("force", false, "skip confirmation prompt")
	cmd.Flags().StringSlice("tag", nil, "filter by tag (can be repeated, combines with OR)")
	_ = cmd.RegisterFlagCompletionFunc("tag", tagCompletionFunc())

	return cmd
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
				results[i] = r
				return nil
			}

			// Get current branch.
			current, _ := git.Run(ctx, repo.Path, "rev-parse", "--abbrev-ref", "HEAD")

			// List branches merged into default.
			merged, err := git.Run(ctx, repo.Path, "branch", "--merged", defBranch)
			if err != nil {
				r.Error = "failed to list merged branches"
				results[i] = r
				return nil
			}

			for _, line := range strings.Split(merged, "\n") {
				branch := strings.TrimSpace(line)
				branch = strings.TrimPrefix(branch, "* ")
				if branch == "" || branch == defBranch || branch == current {
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
	for _, r := range results {
		if len(r.Name) > nameWidth {
			nameWidth = len(r.Name)
		}
	}
	nameWidth += 2

	header := fmt.Sprintf("  %-*s %s", nameWidth, "REPO", "STALE BRANCHES")
	_, _ = fmt.Fprintln(w, output.Dim(header))
	_, _ = fmt.Fprintln(w, output.Dim("  "+strings.Repeat("─", len(header)-2)))

	totalBranches := 0
	for _, r := range results {
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

	_, _ = fmt.Fprintf(w, "\n  %s\n", output.Dim(fmt.Sprintf(
		"%d stale %s across %d %s",
		totalBranches, output.Plural(totalBranches, "branch"),
		len(results), output.Plural(len(results), "repo"))))
}

// deleteBranches deletes stale branches in parallel and returns deleted/failed counts.
func deleteBranches(ctx context.Context, results []cleanResult) (deleted, failed int) {
	var mu sync.Mutex
	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(maxConcurrency)

	for _, r := range results {
		if r.Error != "" {
			continue
		}
		for _, branch := range r.Branches {
			g.Go(func() error {
				_, err := git.Run(ctx, r.Path, "branch", "-d", branch)
				mu.Lock()
				if err != nil {
					failed++
				} else {
					deleted++
				}
				mu.Unlock()
				return nil
			})
		}
	}

	_ = g.Wait()
	return deleted, failed
}
