package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/CelikE/soko/internal/git"
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

// newPrCmd creates the cobra command for soko pr. It lists the open pull
// requests of the git repository the user is currently in, via the GitHub CLI.
func newPrCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pr",
		Short: "List open pull requests for the current repository",
		Long: `List the open pull requests of the git repository you are currently in,
using the GitHub CLI (gh): #number, title, branch, and author.

Run it from anywhere inside a repository's working tree. Use --json for
scripting. Requires the gh CLI (https://cli.github.com), authenticated for
your hosts.`,
		Example: `  soko pr
  soko pr --json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			w := cmd.OutOrStdout()

			if !commandExists("gh") {
				return fmt.Errorf("gh CLI not found — install it from https://cli.github.com to use soko pr")
			}

			dir, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("getting working directory: %w", err)
			}
			root, err := git.Toplevel(cmd.Context(), dir)
			if err != nil {
				return fmt.Errorf("not inside a git repository")
			}

			prs, err := fetchOpenPRs(cmd.Context(), root)
			if err != nil {
				return err
			}

			jsonFlag, _ := cmd.Flags().GetBool("json")
			if jsonFlag {
				if prs == nil {
					prs = []prItem{}
				}
				return output.RenderJSON(w, prs)
			}

			name := filepath.Base(root)
			if len(prs) == 0 {
				output.Info(w, fmt.Sprintf("%s: no open pull requests", name))
				return nil
			}
			renderPrList(w, name, prs)
			return nil
		},
	}

	return cmd
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
