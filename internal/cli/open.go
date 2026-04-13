package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/CelikE/soko/internal/browser"
	"github.com/CelikE/soko/internal/config"
	"github.com/CelikE/soko/internal/git"
	"github.com/CelikE/soko/internal/output"
)

// newOpenCmd creates the cobra command for soko open.
func newOpenCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "open [name]",
		Short: "Open a repo in the browser",
		Long: `Open a repo's remote URL in the default browser. If no name is given,
opens the repo in the current directory. Use flags to jump to specific
pages like pull requests, issues, or CI/CD.`,
		Example: `  soko open                     # current repo
  soko open auth-service        # by name
  soko open --prs               # pull requests
  soko open --tag backend       # open all backend repos
  soko open --tag backend --prs # PRs for all backend repos`,
		ValidArgsFunction: repoNameCompletionFunc(),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			w := cmd.OutOrStdout()

			page := resolvePageFlag(cmd)

			// Determine which repos to open.
			var repos []config.RepoEntry

			if tags, _ := cmd.Flags().GetStringSlice("tag"); len(tags) > 0 {
				cfg, err := config.Load()
				if err != nil {
					return fmt.Errorf("loading config: %w", err)
				}
				repos = config.FilterByTags(cfg.Repos, tags)
				if len(repos) == 0 {
					output.Info(w, noReposMessage(len(cfg.Repos)))
					return nil
				}
			} else if len(args) > 0 {
				cfg, err := config.Load()
				if err != nil {
					return fmt.Errorf("loading config: %w", err)
				}
				matches := config.FindRepo(cfg, args[0])
				if len(matches) == 0 {
					return fmt.Errorf("no repo matching: %s", args[0])
				}
				if len(matches) > 1 {
					output.Warn(w, fmt.Sprintf("multiple repos match %q:", args[0]))
					for _, m := range matches {
						_, _ = fmt.Fprintf(w, "    %s  %s\n", m.Name, output.Dim(m.Path))
					}
					return fmt.Errorf("multiple repos match %q", args[0])
				}
				repos = matches
			} else {
				// Current directory.
				dir, err := os.Getwd()
				if err != nil {
					return fmt.Errorf("getting working directory: %w", err)
				}
				cfg, err := config.Load()
				if err != nil {
					return fmt.Errorf("loading config: %w", err)
				}
				entry, err := config.FindRepoByPath(cfg, dir)
				if err != nil {
					return fmt.Errorf("current directory is not a registered repo — run soko init first")
				}
				repos = []config.RepoEntry{*entry}
			}

			jsonFlag, _ := cmd.Flags().GetBool("json")

			type openResultJSON struct {
				Name  string `json:"name"`
				Path  string `json:"path"`
				URL   string `json:"url,omitempty"`
				Error string `json:"error,omitempty"`
			}

			var jsonResults []openResultJSON
			var failed int

			// Open each repo.
			for _, repo := range repos {
				remote, err := git.Run(ctx, repo.Path, "remote", "get-url", "origin")
				if err != nil {
					failed++
					if jsonFlag {
						jsonResults = append(jsonResults, openResultJSON{
							Name:  repo.Name,
							Path:  repo.Path,
							Error: "no remote origin configured",
						})
					} else {
						output.Fail(w, fmt.Sprintf("%s: no remote origin configured", repo.Name))
					}
					continue
				}

				baseURL := browser.RemoteToHTTPS(remote)
				fullURL := baseURL + browser.SubPagePath(baseURL, page)

				if err := browser.Open(fullURL); err != nil {
					failed++
					if jsonFlag {
						jsonResults = append(jsonResults, openResultJSON{
							Name:  repo.Name,
							Path:  repo.Path,
							Error: err.Error(),
						})
					} else {
						output.Fail(w, fmt.Sprintf("%s: %s", repo.Name, err))
					}
					continue
				}

				if jsonFlag {
					jsonResults = append(jsonResults, openResultJSON{
						Name: repo.Name,
						Path: repo.Path,
						URL:  fullURL,
					})
				} else {
					output.Confirm(w, fmt.Sprintf("opened %s %s", repo.Name, output.Dim(fullURL)))
				}
			}

			if jsonFlag {
				if err := output.RenderJSON(w, jsonResults); err != nil {
					return err
				}
			}

			if failed > 0 {
				return fmt.Errorf("%d %s failed to open", failed, output.Plural(failed, "repo"))
			}

			return nil
		},
	}

	cmd.Flags().Bool("prs", false, "open pull/merge requests page")
	cmd.Flags().Bool("issues", false, "open issues page")
	cmd.Flags().Bool("actions", false, "open CI/CD actions page")
	cmd.Flags().Bool("branches", false, "open branches page")
	cmd.Flags().Bool("settings", false, "open settings page")
	cmd.Flags().StringSlice("tag", nil, "open repos with these tags (can be repeated)")
	_ = cmd.RegisterFlagCompletionFunc("tag", tagCompletionFunc())

	return cmd
}

func resolvePageFlag(cmd *cobra.Command) browser.Page {
	if f, _ := cmd.Flags().GetBool("prs"); f {
		return browser.PagePRs
	}
	if f, _ := cmd.Flags().GetBool("issues"); f {
		return browser.PageIssues
	}
	if f, _ := cmd.Flags().GetBool("actions"); f {
		return browser.PageActions
	}
	if f, _ := cmd.Flags().GetBool("branches"); f {
		return browser.PageBranches
	}
	if f, _ := cmd.Flags().GetBool("settings"); f {
		return browser.PageSettings
	}
	return browser.PageHome
}
