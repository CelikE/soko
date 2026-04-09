package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/CelikE/soko/internal/git"
	"github.com/CelikE/soko/internal/output"
)

// newDiffCmd creates the cobra command for soko diff.
func newDiffCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "diff [repos...]",
		Short: "Show uncommitted file changes across repos",
		Long: `Show a file-level summary of uncommitted changes across all dirty repos.
Clean repos are silently skipped.`,
		Example: `  soko diff
  soko diff auth
  soko diff --tag backend`,
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

			jsonFlag, _ := cmd.Flags().GetBool("json")

			var results []diffResult
			var totalFiles int

			for _, repo := range repos {
				if !pathExists(repo.Path) {
					continue
				}

				statusOut, err := git.Run(ctx, repo.Path, "status", "--porcelain")
				if err != nil || statusOut == "" {
					continue
				}

				lines := strings.Split(statusOut, "\n")
				var files []diffFile
				for _, line := range lines {
					line = strings.TrimSpace(line)
					if line == "" {
						continue
					}
					// After TrimSpace, format is: "XY filename" or "?? filename".
					// Split into status and filename at the first space after status.
					parts := strings.SplitN(line, " ", 2)
					if len(parts) != 2 {
						continue
					}
					status := parts[0]
					name := strings.TrimSpace(parts[1])
					if name == "" {
						continue
					}
					files = append(files, diffFile{Status: status, Name: name})
				}

				if len(files) > 0 {
					results = append(results, diffResult{
						Name:  repo.Name,
						Path:  repo.Path,
						Files: files,
					})
					totalFiles += len(files)
				}
			}

			if len(results) == 0 {
				if jsonFlag {
					_, _ = fmt.Fprintln(w, "[]")
					return nil
				}
				output.Info(w, "all repos clean")
				return nil
			}

			if jsonFlag {
				return renderDiffJSON(w, results)
			}

			for i, r := range results {
				if i > 0 {
					_, _ = fmt.Fprintln(w)
				}
				_, _ = fmt.Fprintf(w, "  %s %s\n",
					r.Name,
					output.Dim(fmt.Sprintf("(%d files)", len(r.Files))))

				for _, f := range r.Files {
					var colored string
					switch {
					case strings.Contains(f.Status, "M"):
						colored = output.Yellow(fmt.Sprintf("    %s  %s", f.Status, f.Name))
					case strings.Contains(f.Status, "A"):
						colored = output.Green(fmt.Sprintf("    %s  %s", f.Status, f.Name))
					case strings.Contains(f.Status, "D"):
						colored = output.Red(fmt.Sprintf("    %s  %s", f.Status, f.Name))
					default:
						colored = output.Dim(fmt.Sprintf("    %s  %s", f.Status, f.Name))
					}
					_, _ = fmt.Fprintln(w, colored)
				}
			}

			_, _ = fmt.Fprintln(w)
			output.Info(w, fmt.Sprintf("%d %s · %d %s changed", len(results), output.Plural(len(results), "repo"), totalFiles, output.Plural(totalFiles, "file")))

			return nil
		},
	}

	cmd.Flags().StringSlice("tag", nil, "filter by tag (can be repeated, combines with OR)")
	_ = cmd.RegisterFlagCompletionFunc("tag", tagCompletionFunc())

	return cmd
}

type diffResult struct {
	Name  string     `json:"name"`
	Path  string     `json:"path"`
	Files []diffFile `json:"files"`
}

type diffFile struct {
	Status string `json:"status"`
	Name   string `json:"name"`
}

func renderDiffJSON(w io.Writer, results []diffResult) error {
	return output.RenderJSON(w, results)
}
