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
				if _, statErr := os.Stat(gitDir); os.IsNotExist(statErr) {
					return nil // not a repo, keep walking
				}

				name := git.RepoName(ctx, path)
				r := scanResult{Name: name, Path: path}

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

				found = append(found, r)

				// Don't descend into git repos (they're self-contained).
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

			// Print results.
			var registered, skipped int
			for _, r := range found {
				switch {
				case r.Error != "":
					output.Fail(w, fmt.Sprintf("%s (%s)", r.Name, r.Error))
				case r.AlreadyRegistered:
					output.Warn(w, fmt.Sprintf("already registered %s (%s)", r.Name, r.Path))
					skipped++
				case dryRun:
					output.Info(w, fmt.Sprintf("would register %s (%s)", r.Name, r.Path))
					registered++
				default:
					output.Confirm(w, fmt.Sprintf("registered %s (%s)", r.Name, r.Path))
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
			output.Info(w, fmt.Sprintf(
				"found %d repos · %d registered · %d already tracked",
				len(found), registered, skipped))

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
