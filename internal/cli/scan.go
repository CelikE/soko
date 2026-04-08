package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/CelikE/soko/internal/config"
	"github.com/CelikE/soko/internal/git"
	"github.com/CelikE/soko/internal/output"
)

// newScanCmd creates the cobra command for soko scan.
func newScanCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "scan <directory>",
		Short: "Discover and register git repos in a directory",
		Long: `Walk a directory tree and register all git repositories found.
Skips hidden directories and repos already registered.`,
		Example: `  soko scan ~/projects
  soko scan ~/work --tag company
  soko scan . --dry-run`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			w := cmd.OutOrStdout()

			root, err := filepath.Abs(args[0])
			if err != nil {
				return fmt.Errorf("resolving path: %w", err)
			}
			// Resolve symlinks for consistent path matching (macOS /var → /private/var).
			if resolved, evalErr := filepath.EvalSymlinks(root); evalErr == nil {
				root = resolved
			}

			if _, statErr := os.Stat(root); os.IsNotExist(statErr) {
				return fmt.Errorf("directory does not exist: %s", root)
			}

			tags, _ := cmd.Flags().GetStringSlice("tag")
			dryRun, _ := cmd.Flags().GetBool("dry-run")
			maxDepth, _ := cmd.Flags().GetInt("depth")
			jsonFlag, _ := cmd.Flags().GetBool("json")

			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			rootDepth := strings.Count(root, string(os.PathSeparator))

			var found []scanResult
			walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
				if walkErr != nil {
					return nil // skip unreadable directories
				}

				if !d.IsDir() {
					return nil
				}

				// Skip hidden directories (except root itself).
				if path != root && strings.HasPrefix(d.Name(), ".") {
					return fs.SkipDir
				}

				// Enforce depth limit.
				depth := strings.Count(path, string(os.PathSeparator)) - rootDepth
				if depth > maxDepth {
					return fs.SkipDir
				}

				// Check if this directory is a git repo.
				gitDir := filepath.Join(path, ".git")
				info, statErr := os.Stat(gitDir)
				if os.IsNotExist(statErr) {
					return nil // not a repo, keep walking
				}

				// A linked worktree has a .git *file* (not directory). Skip it
				// to avoid registering the same repo multiple times.
				if !info.IsDir() {
					return fs.SkipDir
				}

				name := git.RepoName(ctx, path)
				r := scanResult{Name: name, Path: path}

				if dryRun {
					// In dry-run, check if it would be a duplicate without modifying config.
					for _, existing := range cfg.Repos {
						if existing.Path == path {
							r.AlreadyRegistered = true
							break
						}
					}
					if !r.AlreadyRegistered {
						r.Registered = true
					}
				} else {
					_, addErr := config.AddRepo(cfg, config.RepoEntry{
						Name: name,
						Path: path,
						Tags: tags,
					})
					if addErr != nil {
						if errors.Is(addErr, config.ErrRepoAlreadyExists) {
							r.AlreadyRegistered = true
						} else {
							r.Error = addErr.Error()
						}
					} else {
						r.Registered = true
					}
				}

				found = append(found, r)
				return fs.SkipDir
			})

			if walkErr != nil {
				return fmt.Errorf("scanning directory: %w", walkErr)
			}

			if len(found) == 0 {
				output.Info(w, "no git repos found")
				return nil
			}

			if jsonFlag {
				return renderScanJSON(w, found)
			}

			// Print header.
			scanHeader := fmt.Sprintf("scanning %s (depth: %d", shortenHome(root), maxDepth)
			if len(tags) > 0 {
				scanHeader += ", tags: " + strings.Join(tags, ", ")
			}
			if dryRun {
				scanHeader += ", dry-run"
			}
			scanHeader += ")"
			output.Info(w, scanHeader)
			_, _ = fmt.Fprintln(w)

			// Compute column widths.
			nameWidth := len("NAME")
			for _, r := range found {
				if len(r.Name) > nameWidth {
					nameWidth = len(r.Name)
				}
			}
			nameWidth += 2

			// Table header.
			header := fmt.Sprintf("  %-*s %-8s %s", nameWidth, "NAME", "SOKO", "PATH")
			_, _ = fmt.Fprintln(w, output.Dim(header))
			_, _ = fmt.Fprintln(w, output.Dim("  "+strings.Repeat("─", len(header)-2)))

			// Rows.
			var registered, existing int
			for _, r := range found {
				shortPath := shortenHome(r.Path)

				switch {
				case r.Error != "":
					_, _ = fmt.Fprintln(w, output.Red(fmt.Sprintf(
						"  %-*s %-8s %s", nameWidth, r.Name, output.SymConflict, r.Error)))
				case r.AlreadyRegistered:
					_, _ = fmt.Fprintf(w, "  %-*s %s       %s\n",
						nameWidth, r.Name,
						output.Green(output.SymClean),
						output.Dim(shortPath))
					existing++
				case r.Registered:
					_, _ = fmt.Fprintln(w, output.Green(fmt.Sprintf(
						"  %-*s %s       %s", nameWidth, r.Name, output.SymClean, shortPath)))
					registered++
				}
			}

			// Save unless dry-run.
			if !dryRun && registered > 0 {
				if saveErr := config.Save(cfg); saveErr != nil {
					return fmt.Errorf("saving config: %w", saveErr)
				}
			}

			_, _ = fmt.Fprintln(w)
			if dryRun {
				output.Info(w, fmt.Sprintf(
					"found %d repos · %d not initialized · %d already in soko",
					len(found), registered, existing))
			} else {
				output.Info(w, fmt.Sprintf(
					"found %d repos · %d initialized · %d already in soko",
					len(found), registered, existing))
			}

			return nil
		},
	}

	cmd.Flags().StringSlice("tag", nil, "tags to apply to discovered repos")
	cmd.Flags().Bool("dry-run", false, "show repos that would be registered without registering them")
	cmd.Flags().Int("depth", 5, "maximum directory depth to scan")

	return cmd
}

type scanResult struct {
	Name              string `json:"name"`
	Path              string `json:"path"`
	Registered        bool   `json:"registered"`
	AlreadyRegistered bool   `json:"already_registered"`
	Error             string `json:"error,omitempty"`
}

func renderScanJSON(w io.Writer, results []scanResult) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(results); err != nil {
		return fmt.Errorf("encoding json: %w", err)
	}
	return nil
}

func shortenHome(path string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if strings.HasPrefix(path, home) {
		return "~" + path[len(home):]
	}
	return path
}
