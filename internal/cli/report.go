package cli

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/CelikE/soko/internal/config"
	"github.com/CelikE/soko/internal/git"
	"github.com/CelikE/soko/internal/output"
)

type reportResult struct {
	Name    string   `json:"name"`
	Path    string   `json:"path"`
	Commits []string `json:"commits"`
}

func newReportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "report [repos...]",
		Short: "Summarize commit activity across repos",
		Long: `Generate a summary of commit activity across all repos for a time period.
Built for standups, timesheets, and weekly updates.

By default shows only your commits (based on git config user.name).
Use --all-authors to include everyone's commits.`,
		Example: `  soko report                         # your commits, last 7 days
  soko report --days 1                # standup: your commits yesterday
  soko report --days 30               # monthly summary
  soko report --tag backend           # only backend repos
  soko report --all-authors           # everyone's commits
  soko report --author "John"         # specific author`,
		Args: cobra.ArbitraryArgs,
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
					output.Info(w, fmt.Sprintf("no repos found matching: %s", strings.Join(args, ", ")))
					return nil
				}
			}

			if len(repos) == 0 {
				output.Info(w, "no repos registered yet — cd into a repo and run: soko init")
				return nil
			}

			days, _ := cmd.Flags().GetInt("days")
			maxCommits, _ := cmd.Flags().GetInt("max")
			authorFlag, _ := cmd.Flags().GetString("author")
			allAuthors, _ := cmd.Flags().GetBool("all-authors")
			jsonFlag, _ := cmd.Flags().GetBool("json")

			author := authorFlag
			if author == "" && !allAuthors {
				author = gitUserName(ctx)
			}

			since := fmt.Sprintf("%d days ago", days)
			active, inactive := collectReport(ctx, repos, since, author)

			if jsonFlag {
				return output.RenderJSON(w, active)
			}

			if len(active) == 0 {
				output.Info(w, fmt.Sprintf("no activity in the last %d %s", days, output.Plural(days, "day")))
				return nil
			}

			renderReport(w, active, inactive, days, maxCommits)
			return nil
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

// gitUserName returns the current git user.name, or empty string on failure.
func gitUserName(ctx context.Context) string {
	name, err := git.Run(ctx, ".", "config", "user.name")
	if err != nil {
		return ""
	}
	return name
}

// collectReport gathers commit activity across repos.
func collectReport(ctx context.Context, repos []config.RepoEntry, since, author string) (active []reportResult, inactive []string) {
	for _, repo := range repos {
		if !pathExists(repo.Path) {
			continue
		}

		gitArgs := []string{"log", "--oneline", "--format=%s", "--since=" + since}
		if author != "" {
			gitArgs = append(gitArgs, "--author="+author)
		}

		out, err := git.Run(ctx, repo.Path, gitArgs...)
		if err != nil || out == "" {
			inactive = append(inactive, repo.Name)
			continue
		}

		commits := strings.Split(out, "\n")
		if len(commits) == 0 {
			inactive = append(inactive, repo.Name)
			continue
		}

		active = append(active, reportResult{
			Name:    repo.Name,
			Path:    repo.Path,
			Commits: commits,
		})
	}

	// Sort by most active first.
	sort.Slice(active, func(i, j int) bool {
		return len(active[i].Commits) > len(active[j].Commits)
	})

	return active, inactive
}

// renderReport prints the report to w.
func renderReport(w io.Writer, active []reportResult, inactive []string, days, maxCommits int) {
	_, _ = fmt.Fprintf(w, "  %s\n\n", output.Dim(fmt.Sprintf("activity: last %d %s", days, output.Plural(days, "day"))))

	totalCommits := 0
	for _, r := range active {
		_, _ = fmt.Fprintf(w, "  %s (%d %s)\n", r.Name, len(r.Commits), output.Plural(len(r.Commits), "commit"))

		show := r.Commits
		remaining := 0
		if maxCommits > 0 && len(show) > maxCommits {
			remaining = len(show) - maxCommits
			show = show[:maxCommits]
		}

		for _, msg := range show {
			_, _ = fmt.Fprintf(w, "    %s\n", msg)
		}
		if remaining > 0 {
			_, _ = fmt.Fprintf(w, "    %s\n", output.Dim(fmt.Sprintf("...and %d more", remaining)))
		}

		_, _ = fmt.Fprintln(w)
		totalCommits += len(r.Commits)
	}

	_, _ = fmt.Fprintf(w, "  %s\n", output.Dim(fmt.Sprintf(
		"%d active %s · %d %s",
		len(active), output.Plural(len(active), "repo"),
		totalCommits, output.Plural(totalCommits, "commit"))))

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
