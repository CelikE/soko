package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"

	"github.com/CelikE/soko/internal/config"
	"github.com/CelikE/soko/internal/git"
	"github.com/CelikE/soko/internal/output"
)

// staleBranchDays is the age threshold (in days) beyond which a local branch
// is considered stale.
const staleBranchDays = 90

// activityWindowDays is the look-back window (in days) for the ACTIVITY
// section of the stats output.
const activityWindowDays = 30

type repoStats struct {
	index         int
	name          string
	path          string
	tags          []string
	missing       bool
	hasRemote     bool
	dirty         bool
	behind        int
	commits       int
	branches      int
	sizeBytes     int64
	staleBranches int
	recentCommits int
	authors       map[string]struct{}
}

type statsSummary struct {
	Repos          int           `json:"repos"`
	Tags           int           `json:"tags"`
	TotalCommits   int           `json:"total_commits"`
	TotalBranches  int           `json:"total_branches"`
	TotalSizeBytes int64         `json:"total_size_bytes"`
	Clean          int           `json:"clean"`
	Dirty          int           `json:"dirty"`
	BehindRemote   int           `json:"behind_remote"`
	StaleBranches  int           `json:"stale_branches"`
	StaleRepos     int           `json:"stale_repos"`
	NoRemote       int           `json:"no_remote"`
	Missing        int           `json:"missing,omitempty"`
	ActivityDays   int           `json:"activity_days"`
	RecentCommits  int           `json:"recent_commits"`
	ActiveRepos    int           `json:"active_repos"`
	ActiveAuthors  int           `json:"active_authors"`
	MostActive     *repoActivity `json:"most_active,omitempty"`
	LeastActive    *repoActivity `json:"least_active,omitempty"`
}

type repoActivity struct {
	Name    string `json:"name"`
	Commits int    `json:"commits"`
}

// newStatsCmd creates the cobra command for soko stats.
func newStatsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Show workspace-level statistics and health metrics",
		Long: `Aggregate statistics across all registered repos: size, commit and
branch counts, health signals (dirty, behind, stale branches), and recent
activity over the last 30 days.`,
		Example: `  soko stats
  soko stats --tag backend
  soko stats --json`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			w := cmd.OutOrStdout()

			cfg, repos, err := loadReposWithTagFilter(cmd)
			if err != nil {
				return err
			}

			if len(repos) == 0 {
				output.Info(w, noReposMessage(len(cfg.Repos)))
				return nil
			}

			collected := collectStats(cmd, repos)
			summary := buildStatsSummary(repos, collected)

			jsonFlag, _ := cmd.Flags().GetBool("json")
			if jsonFlag {
				return output.RenderJSON(w, summary)
			}

			renderStats(w, &summary)
			return nil
		},
	}

	cmd.Flags().StringSlice("tag", nil, "filter by tag (can be repeated, combines with OR)")
	_ = cmd.RegisterFlagCompletionFunc("tag", tagCompletionFunc())

	return cmd
}

func collectStats(cmd *cobra.Command, repos []config.RepoEntry) []repoStats {
	ctx := cmd.Context()
	results := make([]repoStats, 0, len(repos))
	var mu sync.Mutex

	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(maxConcurrency)

	for i, repo := range repos {
		g.Go(func() error {
			r := repoStats{
				index:   i,
				name:    repo.Name,
				path:    repo.Path,
				tags:    repo.Tags,
				authors: map[string]struct{}{},
			}

			if !pathExists(repo.Path) {
				r.missing = true
				mu.Lock()
				results = append(results, r)
				mu.Unlock()
				return nil
			}

			collectRepoStats(ctx, repo.Path, &r)

			mu.Lock()
			results = append(results, r)
			mu.Unlock()
			return nil
		})
	}

	_ = g.Wait()
	sort.Slice(results, func(i, j int) bool {
		return results[i].index < results[j].index
	})
	return results
}

func collectRepoStats(ctx context.Context, path string, r *repoStats) {
	if out, err := git.Run(ctx, path, "remote"); err == nil {
		r.hasRemote = out != ""
	}

	if status, err := git.ParseStatus(ctx, path); err == nil {
		r.dirty = status.Modified+status.Untracked+status.Deleted+status.Conflicts > 0
		r.behind = status.Behind
	}

	if out, err := git.Run(ctx, path, "rev-list", "--count", "HEAD"); err == nil {
		if n, convErr := strconv.Atoi(out); convErr == nil {
			r.commits = n
		}
	}

	if out, err := git.Run(ctx, path, "for-each-ref", "--format=%(committerdate:unix)", "refs/heads"); err == nil && out != "" {
		cutoff := time.Now().Add(-staleBranchDays * 24 * time.Hour).Unix()
		for line := range strings.SplitSeq(out, "\n") {
			if line == "" {
				continue
			}
			r.branches++
			if ts, convErr := strconv.ParseInt(strings.TrimSpace(line), 10, 64); convErr == nil {
				if ts < cutoff {
					r.staleBranches++
				}
			}
		}
	}

	r.sizeBytes = dirSize(filepath.Join(path, ".git"))

	since := fmt.Sprintf("%d days ago", activityWindowDays)
	if out, err := git.Run(ctx, path, "log", "--since="+since, "--format=%ae"); err == nil && out != "" {
		for email := range strings.SplitSeq(out, "\n") {
			email = strings.TrimSpace(email)
			if email == "" {
				continue
			}
			r.recentCommits++
			r.authors[email] = struct{}{}
		}
	}
}

// dirSize returns the total size of a directory in bytes. Returns 0 on any
// error; size is a best-effort signal and not worth failing the whole command.
func dirSize(path string) int64 {
	var total int64
	_ = filepath.WalkDir(path, func(_ string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		info, infoErr := d.Info()
		if infoErr != nil {
			return nil
		}
		total += info.Size()
		return nil
	})
	return total
}

func buildStatsSummary(repos []config.RepoEntry, stats []repoStats) statsSummary {
	tagSet := map[string]struct{}{}
	for _, r := range repos {
		for _, t := range r.Tags {
			tagSet[t] = struct{}{}
		}
	}

	s := statsSummary{
		Repos:        len(stats),
		Tags:         len(tagSet),
		ActivityDays: activityWindowDays,
	}

	allAuthors := map[string]struct{}{}
	type activity struct {
		name    string
		commits int
	}
	var activities []activity

	for i := range stats {
		r := &stats[i]
		if r.missing {
			s.Missing++
			continue
		}
		s.TotalCommits += r.commits
		s.TotalBranches += r.branches
		s.TotalSizeBytes += r.sizeBytes

		if r.dirty {
			s.Dirty++
		} else {
			s.Clean++
		}
		if r.behind > 0 {
			s.BehindRemote++
		}
		if r.staleBranches > 0 {
			s.StaleBranches += r.staleBranches
			s.StaleRepos++
		}
		if !r.hasRemote {
			s.NoRemote++
		}

		if r.recentCommits > 0 {
			s.ActiveRepos++
			activities = append(activities, activity{name: r.name, commits: r.recentCommits})
		}
		s.RecentCommits += r.recentCommits
		for a := range r.authors {
			allAuthors[a] = struct{}{}
		}
	}

	s.ActiveAuthors = len(allAuthors)

	if len(activities) > 0 {
		sort.Slice(activities, func(i, j int) bool {
			return activities[i].commits > activities[j].commits
		})
		s.MostActive = &repoActivity{Name: activities[0].name, Commits: activities[0].commits}
		last := activities[len(activities)-1]
		s.LeastActive = &repoActivity{Name: last.name, Commits: last.commits}
	}

	return s
}

func renderStats(w io.Writer, s *statsSummary) {
	_, _ = fmt.Fprintf(w, "\n  %s\n\n", "workspace stats")

	section(w, "OVERVIEW", [][2]string{
		{"repos", formatInt(s.Repos)},
		{"tags", formatInt(s.Tags)},
		{"total commits", formatInt(s.TotalCommits)},
		{"total branches", formatInt(s.TotalBranches)},
		{"total size", formatSize(s.TotalSizeBytes)},
	})

	healthRows := [][2]string{
		{"clean", fmt.Sprintf("%d %s", s.Clean, output.Plural(s.Clean, "repo"))},
		{"dirty", fmt.Sprintf("%d %s", s.Dirty, output.Plural(s.Dirty, "repo"))},
		{"behind remote", fmt.Sprintf("%d %s", s.BehindRemote, output.Plural(s.BehindRemote, "repo"))},
		{"stale branches", fmt.Sprintf("%d across %d %s", s.StaleBranches, s.StaleRepos, output.Plural(s.StaleRepos, "repo"))},
		{"no remote", fmt.Sprintf("%d %s", s.NoRemote, output.Plural(s.NoRemote, "repo"))},
	}
	if s.Missing > 0 {
		healthRows = append(healthRows, [2]string{"missing", fmt.Sprintf("%d %s", s.Missing, output.Plural(s.Missing, "repo"))})
	}
	section(w, "HEALTH", healthRows)

	activityRows := [][2]string{
		{"commits", formatInt(s.RecentCommits)},
		{"active repos", formatInt(s.ActiveRepos)},
		{"active authors", formatInt(s.ActiveAuthors)},
	}
	if s.MostActive != nil {
		activityRows = append(activityRows,
			[2]string{"most active", fmt.Sprintf("%s (%d %s)", s.MostActive.Name, s.MostActive.Commits, output.Plural(s.MostActive.Commits, "commit"))})
	}
	if s.LeastActive != nil && (s.MostActive == nil || s.LeastActive.Name != s.MostActive.Name) {
		activityRows = append(activityRows,
			[2]string{"least active", fmt.Sprintf("%s (%d %s)", s.LeastActive.Name, s.LeastActive.Commits, output.Plural(s.LeastActive.Commits, "commit"))})
	}
	sectionTitle := fmt.Sprintf("ACTIVITY (last %d days)", s.ActivityDays)
	section(w, sectionTitle, activityRows)
}

func section(w io.Writer, title string, rows [][2]string) {
	_, _ = fmt.Fprintf(w, "  %s\n", output.Dim(title))
	_, _ = fmt.Fprintf(w, "  %s\n", output.Dim(strings.Repeat("─", 30)))

	labelWidth := 0
	for _, row := range rows {
		if len(row[0]) > labelWidth {
			labelWidth = len(row[0])
		}
	}
	labelWidth += 2

	for _, row := range rows {
		_, _ = fmt.Fprintf(w, "  %-*s %s\n", labelWidth, row[0], row[1])
	}
	_, _ = fmt.Fprintln(w)
}

// formatInt formats n with thousands separators (e.g., 12847 -> "12,847").
func formatInt(n int) string {
	if n < 0 {
		return "-" + formatInt(-n)
	}
	s := strconv.Itoa(n)
	if len(s) <= 3 {
		return s
	}
	var b strings.Builder
	first := len(s) % 3
	if first > 0 {
		b.WriteString(s[:first])
		if len(s) > first {
			b.WriteByte(',')
		}
	}
	for i := first; i < len(s); i += 3 {
		b.WriteString(s[i : i+3])
		if i+3 < len(s) {
			b.WriteByte(',')
		}
	}
	return b.String()
}

// formatSize returns a human-readable size string (e.g., "2.3 GB", "512 KB").
func formatSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	suffix := []string{"KB", "MB", "GB", "TB", "PB"}[exp]
	return fmt.Sprintf("%.1f %s", float64(bytes)/float64(div), suffix)
}
