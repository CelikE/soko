package cli

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode"

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
per repo and cause a non-zero exit status. In JSON, file names are actual paths;
renames and copies use the destination as name and include original_name.`,
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
					return renderNoMatchingRepos(cmd, args)
				}
			}

			if len(repos) == 0 {
				return renderNoRepos(cmd, len(cfg.Repos))
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

				statusOut, err := git.StatusPorcelainZ(ctx, repo.Path)
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

				files := parseDiffPorcelainZ(statusOut)

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
					name := displayDiffName(f.Name)
					if f.OriginalName != "" {
						name = displayDiffName(f.OriginalName) + " -> " + name
					}
					var colored string
					switch {
					case strings.Contains(f.Status, "M"):
						colored = output.Yellow(fmt.Sprintf("    %s  %s", f.Status, name))
					case strings.Contains(f.Status, "A"):
						colored = output.Green(fmt.Sprintf("    %s  %s", f.Status, name))
					case strings.Contains(f.Status, "D"):
						colored = output.Red(fmt.Sprintf("    %s  %s", f.Status, name))
					default:
						colored = output.Dim(fmt.Sprintf("    %s  %s", f.Status, name))
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
	Status       string `json:"status"`
	Name         string `json:"name"`
	OriginalName string `json:"original_name,omitempty"`
}

// parseDiffPorcelainZ reads XY SP path NUL records. For renames and copies,
// Git puts the destination first and the original path in a second NUL record.
func parseDiffPorcelainZ(out string) []diffFile {
	var files []diffFile
	for out != "" {
		var record string
		var ok bool
		record, out, ok = strings.Cut(out, "\x00")
		if !ok || len(record) < 4 || record[2] != ' ' {
			break
		}
		file := diffFile{Status: strings.TrimSpace(record[:2]), Name: record[3:]}
		if strings.ContainsAny(record[:2], "RC") {
			file.OriginalName, out, ok = strings.Cut(out, "\x00")
			if !ok {
				break
			}
		}
		files = append(files, file)
	}
	return files
}

// Keep control characters from splitting text rows; JSON still carries raw paths.
func displayDiffName(name string) string {
	if strings.IndexFunc(name, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsControl(r) || r == '"' || r == '\\'
	}) >= 0 {
		return strconv.Quote(name)
	}
	return name
}

func renderDiffJSON(w io.Writer, results []diffResult) error {
	return output.RenderJSON(w, results)
}
