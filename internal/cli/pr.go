package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"

	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"

	"github.com/CelikE/soko/internal/config"
	"github.com/CelikE/soko/internal/output"
)

// prItem is one open pull request, as reported by the GitHub CLI.
type prItem struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	Branch string `json:"branch"`
	State  string `json:"state"`
	URL    string `json:"url"`
	Author string `json:"author"`
}

// fetchOpenPRs lists a repo's open pull requests via the GitHub CLI (gh), which
// handles auth and host/owner/name resolution from the repo's remotes. The
// caller is responsible for checking gh exists (commandExists) first.
func fetchOpenPRs(ctx context.Context, path string) ([]prItem, error) {
	c := exec.CommandContext(ctx, "gh", "pr", "list",
		"--state", "open", "--limit", "50",
		"--json", "number,title,headRefName,state,url,author")
	c.Dir = path
	var stdout, stderr bytes.Buffer
	c.Stdout = &stdout
	c.Stderr = &stderr
	if err := c.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = err.Error()
		}
		return nil, fmt.Errorf("gh pr list: %s", detail)
	}

	var raw []struct {
		Number      int    `json:"number"`
		Title       string `json:"title"`
		HeadRefName string `json:"headRefName"`
		State       string `json:"state"`
		URL         string `json:"url"`
		Author      struct {
			Login string `json:"login"`
		} `json:"author"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &raw); err != nil {
		return nil, fmt.Errorf("parsing gh output: %w", err)
	}
	prs := make([]prItem, len(raw))
	for i, p := range raw {
		prs[i] = prItem{
			Number: p.Number,
			Title:  p.Title,
			Branch: p.HeadRefName,
			State:  p.State,
			URL:    p.URL,
			Author: p.Author.Login,
		}
	}
	return prs, nil
}

// newPrCmd creates the cobra command for soko pr. With no single target it
// summarises open PR counts across every tracked repo; given one repo it lists
// that repo's open pull requests. Both rely on the GitHub CLI (gh).
func newPrCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pr [repos...]",
		Short: "Gather open pull request information across repos",
		Long: `Gather open pull request information using the GitHub CLI (gh).

With no arguments (or a tag filter) soko pr prints a per-repo summary of how
many pull requests are open. Given a single repo it lists that repo's open
pull requests (#number, title, branch, author). Use --json for scripting.

Requires the gh CLI (https://cli.github.com), authenticated for your hosts.`,
		Example: `  soko pr
  soko pr --tag backend
  soko pr my-service
  soko pr --json`,
		Args:              cobra.ArbitraryArgs,
		ValidArgsFunction: repoNameCompletionFunc(),
		RunE: func(cmd *cobra.Command, args []string) error {
			w := cmd.OutOrStdout()

			if !commandExists("gh") {
				return fmt.Errorf("gh CLI not found — install it from https://cli.github.com to use soko pr")
			}

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

			jsonFlag, _ := cmd.Flags().GetBool("json")

			// One explicit repo → list its pull requests; otherwise summarise.
			if len(args) > 0 && len(repos) == 1 {
				return runPrList(cmd, &repos[0], jsonFlag)
			}
			return runPrSummary(cmd, repos, jsonFlag)
		},
	}

	cmd.Flags().StringSlice("tag", nil, "filter by tag (can be repeated, combines with OR)")
	_ = cmd.RegisterFlagCompletionFunc("tag", tagCompletionFunc())

	return cmd
}

// runPrList lists one repo's open pull requests.
func runPrList(cmd *cobra.Command, repo *config.RepoEntry, jsonFlag bool) error {
	w := cmd.OutOrStdout()

	if !pathExists(repo.Path) {
		return fmt.Errorf("%s: path not found (%s)", repo.Name, repo.Path)
	}

	prs, err := fetchOpenPRs(cmd.Context(), repo.Path)
	if err != nil {
		return err
	}

	if jsonFlag {
		if prs == nil {
			prs = []prItem{}
		}
		return output.RenderJSON(w, prs)
	}

	if len(prs) == 0 {
		output.Info(w, fmt.Sprintf("%s: no open pull requests", repo.Name))
		return nil
	}

	renderPrList(w, repo.Name, prs)
	return nil
}

// renderPrList prints a repo's pull requests as an aligned table.
func renderPrList(w io.Writer, repo string, prs []prItem) {
	cNum := len("PR")
	cBranch := len("BRANCH")
	cAuthor := len("AUTHOR")
	for _, p := range prs {
		if n := len(fmt.Sprintf("#%d", p.Number)); n > cNum {
			cNum = n
		}
		if len(p.Branch) > cBranch {
			cBranch = len(p.Branch)
		}
		if len(p.Author) > cAuthor {
			cAuthor = len(p.Author)
		}
	}
	cNum += 2
	cBranch += 2
	cAuthor += 2

	_, _ = fmt.Fprintf(w, "  %s\n", output.Dim(repo))
	header := fmt.Sprintf("  %-*s %-*s %-*s %s", cNum, "PR", cBranch, "BRANCH", cAuthor, "AUTHOR", "TITLE")
	_, _ = fmt.Fprintln(w, output.Dim(header))
	_, _ = fmt.Fprintln(w, output.Dim("  "+strings.Repeat("─", len(header)-2)))

	for _, p := range prs {
		_, _ = fmt.Fprintf(w, "  %-*s %-*s %-*s %s\n",
			cNum, fmt.Sprintf("#%d", p.Number),
			cBranch, p.Branch,
			cAuthor, p.Author,
			p.Title,
		)
	}
}

// prSummaryResult is one repo's open-PR count for the summary table.
type prSummaryResult struct {
	index int
	name  string
	path  string
	count int
	err   string
}

// runPrSummary prints a per-repo count of open pull requests.
func runPrSummary(cmd *cobra.Command, repos []config.RepoEntry, jsonFlag bool) error {
	w := cmd.OutOrStdout()
	results := collectPrCounts(cmd, repos)

	if jsonFlag {
		type prSummaryJSON struct {
			Name  string `json:"name"`
			Path  string `json:"path"`
			Open  int    `json:"open"`
			Error string `json:"error,omitempty"`
		}
		entries := make([]prSummaryJSON, len(results))
		for i := range results {
			entries[i] = prSummaryJSON{
				Name:  results[i].name,
				Path:  results[i].path,
				Open:  results[i].count,
				Error: results[i].err,
			}
		}
		return output.RenderJSON(w, entries)
	}

	renderPrSummary(w, results)
	return nil
}

// collectPrCounts gathers open-PR counts for every repo in parallel with
// bounded concurrency, mirroring collectRemotes. Per-repo failures are captured
// on the row and never abort the run; results are restored to config order.
func collectPrCounts(cmd *cobra.Command, repos []config.RepoEntry) []prSummaryResult {
	ctx := cmd.Context()
	results := make([]prSummaryResult, 0, len(repos))
	var mu sync.Mutex

	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(maxConcurrency)

	for i, repo := range repos {
		g.Go(func() error {
			r := prSummaryResult{index: i, name: repo.Name, path: repo.Path}
			if !pathExists(repo.Path) {
				r.err = "path not found"
			} else if prs, err := fetchOpenPRs(ctx, repo.Path); err != nil {
				r.err = gitErrDetail(err)
			} else {
				r.count = len(prs)
			}
			mu.Lock()
			results = append(results, r)
			mu.Unlock()
			return nil
		})
	}
	_ = g.Wait()

	ordered := make([]prSummaryResult, len(results))
	for idx := range results {
		ordered[results[idx].index] = results[idx]
	}
	return ordered
}

// renderPrSummary prints the per-repo open-PR count table and a total.
func renderPrSummary(w io.Writer, results []prSummaryResult) {
	cName := len("REPO")
	for i := range results {
		if len(results[i].name) > cName {
			cName = len(results[i].name)
		}
	}
	cName += 2

	header := fmt.Sprintf("  %-*s %s", cName, "REPO", "OPEN PRS")
	_, _ = fmt.Fprintln(w, output.Dim(header))
	_, _ = fmt.Fprintln(w, output.Dim("  "+strings.Repeat("─", len(header)-2)))

	var total, withPRs int
	for i := range results {
		r := &results[i]
		switch {
		case r.err != "":
			_, _ = fmt.Fprintln(w, output.Red(fmt.Sprintf("  %-*s %s %s", cName, r.name, output.SymConflict, r.err)))
		case r.count > 0:
			total += r.count
			withPRs++
			_, _ = fmt.Fprintln(w, output.Yellow(fmt.Sprintf("  %-*s %d", cName, r.name, r.count)))
		default:
			_, _ = fmt.Fprintln(w, output.Dim(fmt.Sprintf("  %-*s %d", cName, r.name, 0)))
		}
	}

	if !output.Quiet() {
		_, _ = fmt.Fprintf(w, "\n  %s\n", output.Dim(fmt.Sprintf(
			"%d %s · %d open %s · %d %s with PRs",
			len(results), output.Plural(len(results), "repo"),
			total, output.Plural(total, "PR"),
			withPRs, output.Plural(withPRs, "repo"),
		)))
	}
}
