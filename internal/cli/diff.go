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
Clean repos are silently skipped. Missing paths and Git failures are reported
per repo and cause a non-zero exit status.`,
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

			results := make([]diffResult, 0, len(repos))
			var totalFiles, failed, missing int

			for _, repo := range repos {
				r := diffResult{Name: repo.Name, Path: repo.Path, Files: []diffFile{}}
				if !pathExists(repo.Path) {
					r.Error = "path not found"
					r.ErrorCode = codePathMissing
					results = append(results, r)
					failed++
					missing++
					continue
				}

				statusOut, err := git.Run(ctx, repo.Path, "status", "--porcelain")
				if err != nil {
					r.Error = err.Error()
					r.ErrorCode = gitErrorCode(err)
					results = append(results, r)
					failed++
					continue
				}
				if statusOut == "" {
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
					r.Files = files
					results = append(results, r)
					totalFiles += len(files)
				}
			}

			if len(results) == 0 {
				if jsonFlag {
					return renderDiffJSON(w, results)
				}
				output.Info(w, "all repos clean")
				return nil
			}

			if jsonFlag {
				if err := renderDiffJSON(w, results); err != nil {
					return err
				}
				if failed > 0 {
					return fmt.Errorf("%d %s failed to diff", failed, output.Plural(failed, "repo"))
				}
				return nil
			}

			for i, r := range results {
				if i > 0 {
					_, _ = fmt.Fprintln(w)
				}
				if r.Error != "" {
					output.Fail(w, fmt.Sprintf("%s: %s", r.Name, r.Error))
					continue
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

			if changed := len(results) - failed; changed > 0 {
				_, _ = fmt.Fprintln(w)
				output.Info(w, fmt.Sprintf("%d %s · %d %s changed", changed, output.Plural(changed, "repo"), totalFiles, output.Plural(totalFiles, "file")))
			}
			renderMissingHint(w, missing)

			if failed > 0 {
				return fmt.Errorf("%d %s failed to diff", failed, output.Plural(failed, "repo"))
			}
			return nil
		},
	}

	cmd.Flags().StringSlice("tag", nil, "filter by tag (can be repeated, combines with OR)")
	_ = cmd.RegisterFlagCompletionFunc("tag", tagCompletionFunc())

	return cmd
}

type diffResult struct {
	Name      string     `json:"name"`
	Path      string     `json:"path"`
	Files     []diffFile `json:"files"`
	Error     string     `json:"error,omitempty"`
	ErrorCode string     `json:"error_code,omitempty"`
}

type diffFile struct {
	Status string `json:"status"`
	Name   string `json:"name"`
}

func renderDiffJSON(w io.Writer, results []diffResult) error {
	return output.RenderJSON(w, results)
}
