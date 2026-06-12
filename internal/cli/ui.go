package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/CelikE/soko/internal/browser"
	"github.com/CelikE/soko/internal/config"
	"github.com/CelikE/soko/internal/git"
	"github.com/CelikE/soko/internal/journal"
	"github.com/CelikE/soko/internal/output"
	"github.com/CelikE/soko/internal/picker"
	"github.com/CelikE/soko/internal/ui"
)

// uiRefreshInterval is how often the dashboard re-reads local state. Kept cheap
// (status only, no network, no disk walk) so it can run all day in a tmux pane.
const uiRefreshInterval = 5 * time.Second

// minFetchInterval floors --fetch so a typo (e.g. 5s) can't hammer every
// remote with a full workspace fetch in a tight loop.
const minFetchInterval = 30 * time.Second

// newUICmd creates the cobra command for soko ui.
func newUICmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ui",
		Short: "Live full-screen dashboard of local workspace state",
		Long: `Open a full-screen, auto-refreshing dashboard of your local workspace:
each repo's branch, dirty state, ahead/behind, last-commit age, and a health
badge. Local state refreshes every 5s — cheap, no network. Meant to live in a
tmux pane all day.

Keys: j/k move · g/G top/bottom · ctrl+d/u half page · enter cd (needs shell
integration, see soko shell-init) · / search across name, branch, and tags
(enter keeps the filter) · s/S cycle sort · f/F cycle filter · t/T cycle tag
filter · b group by tag · space mark (* marks all visible) · o open home
(p/i/a for PRs/issues/actions) · P pull the marked or selected repos ·
u undo the last pull · r re-fetch all (R just the selected repo) · y copy the
repo path · esc clear search/filters · ? help · q quit. The mouse works too:
wheel scrolls, click selects.

Mutations are confirmed and undoable: P fast-forward pulls the marked repos
(or the one under the cursor) after a y/N prompt and records one journal
entry, so u — or soko undo — resets the whole batch. Use --fetch to fetch
from remotes in the background on an interval (e.g. --fetch 5m, minimum 30s).`,
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
			if fetchEvery > 0 && fetchEvery < minFetchInterval {
				return fmt.Errorf("--fetch interval must be at least %s (got %s)", minFetchInterval, fetchEvery)
			}

			model := ui.New(&ui.Config{
				Ctx:          cmd.Context(),
				Collect:      func(_ context.Context, fetch bool) []ui.Row { return collectUIRows(cmd, repos, fetch) },
				OnSelect:     writeNavFile,
				OnOpen:       openRepoInBrowser(cmd.Context()),
				OnPull:       pullReposForUI(cmd.Context()),
				OnUndo:       undoLastPullForUI(cmd.Context()),
				OnFetchRepo:  fetchRepoForUI(cmd.Context()),
				OnCopy:       copyToClipboard,
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
		score, severity, reasons := scoreRepo(&st)

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
			Reasons:    reasons,
			WorktreeOf: r.worktreeOf,
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

// pullReposForUI returns the ui's pull callback: fast-forward-only pulls of
// one or more repos, recorded as a single journal entry so soko undo (or the
// dashboard's u key) can rewind the whole batch. It returns a short human
// status for the dashboard's status line.
func pullReposForUI(ctx context.Context) func([]ui.PullTarget) (string, error) {
	return func(targets []ui.PullTarget) (string, error) {
		var pulled, upToDate int
		var refs []journal.PullRef
		var failures []string

		for _, t := range targets {
			preSHA, err := git.Run(ctx, t.Path, "rev-parse", "HEAD")
			if err != nil {
				failures = append(failures, t.Name+": not a git repo")
				continue
			}
			advanced, err := git.Pull(ctx, t.Path, false)
			if err != nil {
				failures = append(failures, t.Name+": "+gitErrDetail(err))
				continue
			}
			if !advanced {
				upToDate++
				continue
			}
			pulled++
			refs = append(refs, journal.PullRef{Repo: t.Name, Path: t.Path, SHA: preSHA})
		}

		// One journal entry for the whole batch keeps undo atomic.
		journalNote := ""
		if len(refs) > 0 {
			entry := journal.Entry{
				Op:      journal.OpPull,
				Time:    time.Now(),
				Summary: fmt.Sprintf("pulled %d %s (soko ui)", len(refs), output.Plural(len(refs), "repo")),
				Pulls:   refs,
			}
			if jerr := journal.Append(&entry); jerr != nil {
				// The pulls succeeded; surface the journal miss without failing.
				journalNote = " (undo unavailable: " + jerr.Error() + ")"
			}
		}

		if len(failures) > 0 {
			summary := strings.Join(failures, "; ")
			if pulled+upToDate > 0 {
				return "", fmt.Errorf("%d ok, %d failed: %s", pulled+upToDate, len(failures), summary)
			}
			return "", fmt.Errorf("%s", summary)
		}
		if len(targets) == 1 {
			if pulled == 1 {
				return targets[0].Name + ": pulled" + journalNote, nil
			}
			return targets[0].Name + ": already up to date", nil
		}
		return fmt.Sprintf("pulled %d · up to date %d%s", pulled, upToDate, journalNote), nil
	}
}

// gitErrDetail compacts a git error (which embeds stderr) to its most useful
// line, capped for the status bar.
func gitErrDetail(err error) string {
	s := strings.TrimSpace(err.Error())
	if i := strings.LastIndexByte(s, '\n'); i >= 0 {
		s = strings.TrimSpace(s[i+1:])
	}
	const maxLen = 90
	if utf8.RuneCountInString(s) > maxLen {
		s = string([]rune(s)[:maxLen-1]) + "…"
	}
	return s
}

// undoLastPullForUI returns the ui's undo callback: it rewinds the most recent
// journal entry if and only if it is a pull, mirroring soko undo's pull path.
func undoLastPullForUI(ctx context.Context) func() (string, error) {
	return func() (string, error) {
		j, err := journal.Load()
		if err != nil {
			return "", err
		}
		entry, ok := j.Last()
		if !ok {
			return "", fmt.Errorf("nothing to undo")
		}
		if entry.Op != journal.OpPull {
			return "", fmt.Errorf("last operation is %q, not a pull — use soko undo", entry.Op)
		}

		var reset int
		for _, p := range entry.Pulls {
			if _, err := git.Run(ctx, p.Path, "reset", "--hard", p.SHA); err != nil {
				return "", fmt.Errorf("%s: %s", p.Repo, gitErrDetail(err))
			}
			reset++
		}
		if _, err := journal.PopLast(); err != nil {
			return "", fmt.Errorf("updating journal: %w", err)
		}
		return fmt.Sprintf("undid pull (%d %s reset)", reset, output.Plural(reset, "repo")), nil
	}
}

// fetchRepoForUI returns the ui's single-repo fetch callback (R key).
func fetchRepoForUI(ctx context.Context) func(path string) error {
	return func(path string) error {
		if err := git.Fetch(ctx, path, false); err != nil {
			return fmt.Errorf("fetch failed: %s", gitErrDetail(err))
		}
		return nil
	}
}

// copyToClipboard writes text to the system clipboard via the platform's
// native tool (pbcopy/clip/wl-copy/xclip/xsel).
func copyToClipboard(text string) error {
	ctx := context.Background()
	var c *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		c = exec.CommandContext(ctx, "pbcopy")
	case "windows":
		c = exec.CommandContext(ctx, "clip")
	default:
		switch {
		case commandExists("wl-copy"):
			c = exec.CommandContext(ctx, "wl-copy")
		case commandExists("xclip"):
			c = exec.CommandContext(ctx, "xclip", "-selection", "clipboard")
		case commandExists("xsel"):
			c = exec.CommandContext(ctx, "xsel", "--clipboard", "--input")
		default:
			return fmt.Errorf("no clipboard tool found (install wl-copy, xclip, or xsel)")
		}
	}
	c.Stdin = strings.NewReader(text)
	return c.Run()
}

func commandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
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
