package cli

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/CelikE/soko/internal/config"
	"github.com/CelikE/soko/internal/git"
	"github.com/CelikE/soko/internal/output"
)

type reportCommit struct {
	Message string    `json:"message"`
	Time    time.Time `json:"time"`
}

type reportResult struct {
	Name    string         `json:"name"`
	Path    string         `json:"path"`
	Branch  string         `json:"branch,omitempty"`
	Commits []reportCommit `json:"commits,omitempty"`
	Error   string         `json:"error,omitempty"`
	Code    string         `json:"error_code,omitempty"`
}

func newReportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "report [repos...]",
		Short: "Summarize commit activity across repos",
		Long: `Generate a summary of commit activity across all repos for a time period.
Built for standups, timesheets, and weekly updates.

By default shows only your commits (based on git config user.name).
If Git cannot resolve user.name from the current directory, use --author
to choose a name or --all-authors to include everyone's commits.

An unreadable or missing selected repository makes the report incomplete:
successful activity is still shown, but the command exits non-zero. JSON is
an array of active repositories plus failure entries (name, path, error,
error_code); inactive repositories are omitted. An empty array means every
selected repository was read successfully and none had matching activity.`,
		Example: `  soko report                         # your commits, last 7 days
  soko report --days 1                # standup: your commits yesterday
  soko report --days 30               # monthly summary
  soko report --tag backend           # only backend repos
  soko report --all-authors           # everyone's commits
  soko report --author "John"         # specific author`,
		Args:              cobra.ArbitraryArgs,
		ValidArgsFunction: repoNameCompletionFunc(),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			w := cmd.OutOrStdout()

			_, repos, err := loadReposWithTagFilter(cmd)
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
				if jsonFlag, _ := cmd.Flags().GetBool("json"); jsonFlag {
					return renderEmptySelectionJSON(cmd)
				}
				output.Info(w, "no repos registered yet — cd into a repo and run: soko init")
				return nil
			}

			days, _ := cmd.Flags().GetInt("days")
			if days <= 0 {
				return fmt.Errorf("--days must be a positive integer")
			}
			maxCommits, _ := cmd.Flags().GetInt("max")
			authorFlag, _ := cmd.Flags().GetString("author")
			allAuthors, _ := cmd.Flags().GetBool("all-authors")
			jsonFlag, _ := cmd.Flags().GetBool("json")

			author := authorFlag
			if author == "" && !allAuthors {
				author, err = gitUserName(ctx)
				if err != nil {
					return err
				}
			}

			since := fmt.Sprintf("%d days ago", days)
			active, inactive, failed := collectReport(ctx, repos, since, author)
			completionError := func() error {
				if len(failed) > 0 {
					return fmt.Errorf("report incomplete: failed to inspect %d of %d %s", len(failed), len(repos), output.Plural(len(repos), "repo"))
				}
				return nil
			}

			if jsonFlag {
				if err := output.RenderJSON(w, append(active, failed...)); err != nil {
					return err
				}
				return completionError()
			}

			if len(active) == 0 {
				if len(failed) == 0 {
					output.Info(w, fmt.Sprintf("no activity in the last %d %s", days, output.Plural(days, "day")))
				}
			} else {
				renderReport(w, active, inactive, days, maxCommits)
			}
			for _, r := range failed {
				output.Fail(cmd.ErrOrStderr(), fmt.Sprintf("%s (%s): %s", r.Name, r.Path, r.Error))
			}
			return completionError()
		},
	}

	cmd.Flags().Int("days", 7, "number of days to look back")
	cmd.Flags().Int("max", 5, "max commits to show per repo (0 for all)")
	cmd.Flags().String("author", "", "filter commits by author name (substring match)")
	cmd.Flags().Bool("all-authors", false, "show commits from all authors")
	cmd.Flags().StringSlice("tag", nil, "filter by tag (can be repeated, combines with OR)")
	_ = cmd.RegisterFlagCompletionFunc("tag", tagCompletionFunc())

	return cmd
}

// gitUserName requires a configured author so a default report cannot broaden
// silently to all authors when Git cannot resolve the current identity.
func gitUserName(ctx context.Context) (string, error) {
	name, err := git.Run(ctx, ".", "config", "user.name")
	if err != nil || name == "" {
		return "", fmt.Errorf("cannot determine default report author from git config user.name; use --author NAME or --all-authors")
	}
	return name, nil
}

// collectReport separates read failures from successfully inspected inactive repos.
func collectReport(ctx context.Context, repos []config.RepoEntry, since, author string) (active []reportResult, inactive []string, failed []reportResult) {
	active = make([]reportResult, 0)
	failed = make([]reportResult, 0)
	for _, repo := range repos {
		if !pathExists(repo.Path) {
			failed = append(failed, reportResult{Name: repo.Name, Path: repo.Path, Error: "path not found", Code: codePathMissing})
			continue
		}

		branch, branchErr := git.Run(ctx, repo.Path, "rev-parse", "--abbrev-ref", "HEAD")
		if branchErr != nil && !isUnbornReportRepo(ctx, repo.Path) {
			failed = append(failed, reportResult{Name: repo.Name, Path: repo.Path, Error: branchErr.Error(), Code: gitErrorCode(branchErr)})
			continue
		}
		if branchErr != nil {
			inactive = append(inactive, repo.Name)
			continue
		}

		gitArgs := []string{"log", "--format=%s\x1f%aI", "--since=" + since}
		if author != "" {
			gitArgs = append(gitArgs, "--fixed-strings", "--author="+author)
		}

		out, err := git.Run(ctx, repo.Path, gitArgs...)
		if err != nil {
			failed = append(failed, reportResult{Name: repo.Name, Path: repo.Path, Error: err.Error(), Code: gitErrorCode(err)})
			continue
		}
		if out == "" {
			inactive = append(inactive, repo.Name)
			continue
		}

		var commits []reportCommit
		for _, line := range strings.Split(out, "\n") {
			parts := strings.SplitN(line, "\x1f", 2)
			if len(parts) < 2 || parts[0] == "" {
				continue
			}
			t, _ := time.Parse(time.RFC3339, parts[1])
			commits = append(commits, reportCommit{Message: parts[0], Time: t})
		}

		if len(commits) == 0 {
			inactive = append(inactive, repo.Name)
			continue
		}

		active = append(active, reportResult{
			Name:    repo.Name,
			Path:    repo.Path,
			Branch:  branch,
			Commits: commits,
		})
	}

	// Sort by most active first.
	sort.Slice(active, func(i, j int) bool {
		return len(active[i].Commits) > len(active[j].Commits)
	})

	return active, inactive, failed
}

// An unborn branch has no HEAD commit for git log to inspect, but is a valid
// checkout with no activity. Do not mistake a broken Git directory for one.
func isUnbornReportRepo(ctx context.Context, path string) bool {
	inside, err := git.Run(ctx, path, "rev-parse", "--is-inside-work-tree")
	if err != nil || inside != "true" {
		return false
	}
	ref, err := git.Run(ctx, path, "symbolic-ref", "--quiet", "HEAD")
	if err != nil {
		return false
	}
	_, err = git.Run(ctx, path, "show-ref", "--verify", "--quiet", ref)
	if err == nil {
		return false
	}
	_, err = git.Run(ctx, path, "status", "--porcelain")
	return err == nil
}

// renderReport prints the report to w.
func renderReport(w io.Writer, active []reportResult, inactive []string, days, maxCommits int) {
	// Compute column widths.
	nameWidth := len("REPO")
	branchWidth := len("BRANCH")
	for _, r := range active {
		if len(r.Name) > nameWidth {
			nameWidth = len(r.Name)
		}
		if len(r.Branch) > branchWidth {
			branchWidth = len(r.Branch)
		}
	}
	nameWidth += 2
	branchWidth += 2

	header := fmt.Sprintf("  %-*s %-*s %s", nameWidth, "REPO", branchWidth, "BRANCH", "COMMITS")
	separatorWidth := len(header) - 2
	if separatorWidth < 60 {
		separatorWidth = 60
	}
	_, _ = fmt.Fprintln(w, output.Dim(header))
	_, _ = fmt.Fprintln(w, output.Dim("  "+strings.Repeat("─", separatorWidth)))

	totalCommits := 0
	for i, r := range active {
		// Pad branch manually before dimming to avoid ANSI codes breaking alignment.
		paddedBranch := fmt.Sprintf("%-*s", branchWidth, r.Branch)
		_, _ = fmt.Fprintf(w, "  %-*s %s %s\n",
			nameWidth, r.Name,
			output.Dim(paddedBranch),
			output.Dim(fmt.Sprintf("%d", len(r.Commits))))

		show := r.Commits
		remaining := 0
		if maxCommits > 0 && len(show) > maxCommits {
			remaining = len(show) - maxCommits
			show = show[:maxCommits]
		}

		for j, c := range show {
			connector := "├──"
			if j == len(show)-1 && remaining == 0 {
				connector = "└──"
			}
			ts := c.Time.Format("01-02 15:04")
			_, _ = fmt.Fprintf(w, "  %s %s   %s\n",
				output.Dim(connector),
				output.Dim(ts),
				c.Message)
		}
		if remaining > 0 {
			_, _ = fmt.Fprintf(w, "  %s %s\n",
				output.Dim("└──"),
				output.Dim(fmt.Sprintf("+%d more", remaining)))
		}

		if i < len(active)-1 {
			_, _ = fmt.Fprintln(w)
		}
		totalCommits += len(r.Commits)
	}

	// The summary footer and the inactive addendum are trailing decoration.
	if output.Quiet() {
		return
	}

	_, _ = fmt.Fprintf(w, "\n  %s\n", output.Dim(fmt.Sprintf(
		"%d active %s · %d %s · last %d %s",
		len(active), output.Plural(len(active), "repo"),
		totalCommits, output.Plural(totalCommits, "commit"),
		days, output.Plural(days, "day"))))

	if len(inactive) > 0 {
		maxInactive := 5
		shown := inactive
		extra := 0
		if len(shown) > maxInactive {
			extra = len(shown) - maxInactive
			shown = shown[:maxInactive]
		}
		line := "  " + output.Dim("inactive: "+strings.Join(shown, ", "))
		if extra > 0 {
			line += output.Dim(fmt.Sprintf(" (+%d more)", extra))
		}
		_, _ = fmt.Fprintln(w, line)
	}
}
