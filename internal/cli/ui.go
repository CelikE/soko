package cli

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/CelikE/soko/internal/browser"
	"github.com/CelikE/soko/internal/config"
	"github.com/CelikE/soko/internal/git"
	"github.com/CelikE/soko/internal/output"
	"github.com/CelikE/soko/internal/picker"
	"github.com/CelikE/soko/internal/ui"
)

// uiRefreshInterval is how often the dashboard re-reads local state. Kept cheap
// (status only, no network, no disk walk) so it can run all day in a tmux pane.
const uiRefreshInterval = 5 * time.Second

// newUICmd creates the cobra command for soko ui.
func newUICmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ui",
		Short: "Live full-screen dashboard of local workspace state",
		Long: `Open a full-screen, auto-refreshing dashboard of your local workspace:
each repo's branch, dirty state, ahead/behind, last-commit age, and a health
badge. Local state refreshes every 5s — cheap, no network. Meant to live in a
tmux pane all day.

Keys: j/k move · enter cd (needs shell integration, see soko shell-init) ·
/ search by name · s cycle sort · f cycle filter · t cycle tag filter ·
G group by tag · o open home (p/i/a for PRs/issues/actions) · g re-fetch now ·
? help · q quit.

Read-only: the dashboard never pulls, stashes, or cleans. Use --fetch to fetch
from remotes in the background on an interval (e.g. --fetch 5m).`,
		Example: `  soko ui
  soko ui --tag backend
  soko ui --fetch 5m`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			w := cmd.OutOrStdout()
			stderr := cmd.ErrOrStderr()

			cfg, repos, err := loadReposWithTagFilter(cmd)
			if err != nil {
				return err
			}
			if len(repos) == 0 {
				output.Info(w, noReposMessage(len(cfg.Repos)))
				return nil
			}

			// Degrade gracefully without a terminal — a full-screen TUI needs one.
			if !picker.HasTerminal(os.Stdin) || !picker.HasTerminal(os.Stdout) {
				return fmt.Errorf("soko ui needs an interactive terminal — use soko status instead")
			}

			fetchEvery, _ := cmd.Flags().GetDuration("fetch")

			model := ui.New(ui.Config{
				Ctx:          cmd.Context(),
				Collect:      func(_ context.Context, fetch bool) []ui.Row { return collectUIRows(cmd, repos, fetch) },
				OnSelect:     writeNavFile,
				OnOpen:       openRepoInBrowser(cmd.Context()),
				RefreshEvery: uiRefreshInterval,
				FetchEvery:   fetchEvery,
			})

			selected, err := ui.Run(&model)
			if err != nil {
				return err
			}
			if selected != "" {
				output.Confirm(stderr, fmt.Sprintf("→ %s", selected))
			}
			return nil
		},
	}

	cmd.Flags().StringSlice("tag", nil, "filter by tag (can be repeated, combines with OR)")
	_ = cmd.RegisterFlagCompletionFunc("tag", tagCompletionFunc())
	cmd.Flags().Duration("fetch", 0, "fetch from remotes in the background every interval (e.g. 5m); 0 disables")

	return cmd
}

// collectUIRows gathers one dashboard frame. It reuses the status collector
// (collectAll) verbatim, then derives a health score from the cheap status
// signals via scoreRepo — deliberately skipping the stale-branch and size scans
// that soko health/stats do, so the live loop stays network- and disk-cheap.
func collectUIRows(cmd *cobra.Command, repos []config.RepoEntry, fetch bool) []ui.Row {
	collected, _ := collectAll(cmd, &config.Config{Repos: repos}, fetch)
	sortByIndex(collected)

	rows := make([]ui.Row, len(collected))
	for i := range collected {
		r := &collected[i]

		st := repoStats{
			missing:   r.err == "path not found",
			hasRemote: true, // not known from status cheaply; assume true
			changes:   r.changes,
		}
		var ahead, behind, conflicts int
		var lastCommit time.Time
		if r.status != nil {
			st.behind = r.status.Behind
			st.conflicts = r.status.Conflicts
			st.detached = r.status.Branch == detachedBranch
			ahead = r.status.Ahead
			behind = r.status.Behind
			conflicts = r.status.Conflicts
			lastCommit = r.status.LastCommitTime
		}
		score, severity, _ := scoreRepo(&st)

		rows[i] = ui.Row{
			Name:       r.row.Name,
			Branch:     r.row.Branch,
			Path:       r.path,
			Tags:       r.tags,
			Dirty:      r.dirty,
			Changes:    r.changes,
			Ahead:      ahead,
			Behind:     behind,
			Conflicts:  conflicts,
			LastCommit: lastCommit,
			StatusText: r.row.StatusText,
			Health:     score,
			Severity:   severity,
			Missing:    st.missing,
		}
	}
	return rows
}

// openRepoInBrowser returns a callback that opens a repo's origin URL at a
// named page (home/prs/issues/actions), bound to the given context. Mirrors
// soko open's URL construction.
func openRepoInBrowser(ctx context.Context) func(path, page string) error {
	return func(path, page string) error {
		remote, err := git.Run(ctx, path, "remote", "get-url", "origin")
		if err != nil {
			return fmt.Errorf("no remote origin configured")
		}
		baseURL := browser.RemoteToHTTPS(remote)
		fullURL := baseURL + browser.SubPagePath(baseURL, uiBrowserPage(page))
		return browser.Open(fullURL)
	}
}

// uiBrowserPage maps the ui's page token to a browser.Page.
func uiBrowserPage(page string) browser.Page {
	switch page {
	case "prs":
		return browser.PagePRs
	case "issues":
		return browser.PageIssues
	case "actions":
		return browser.PageActions
	default:
		return browser.PageHome
	}
}
