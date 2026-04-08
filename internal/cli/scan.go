package cli

import (
	"context"
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
			worktreesFlag, _ := cmd.Flags().GetBool("worktrees")

			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			found, walkErr := discoverRepos(ctx, root, cfg, tags, maxDepth, dryRun, worktreesFlag)
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

			registered, existing, wtCount := renderScanTable(w, found, root, maxDepth, worktreesFlag, tags, dryRun)

			// Save unless dry-run.
			if !dryRun && registered > 0 {
				if saveErr := config.Save(cfg); saveErr != nil {
					return fmt.Errorf("saving config: %w", saveErr)
				}
			}

			renderScanSummary(w, found, registered, existing, wtCount, dryRun)

			return nil
		},
	}

	cmd.Flags().StringSlice("tag", nil, "tags to apply to discovered repos")
	cmd.Flags().Bool("dry-run", false, "show repos that would be registered without registering them")
	cmd.Flags().Int("depth", 5, "maximum directory depth to scan")
	cmd.Flags().Bool("worktrees", false, "also discover and register linked git worktrees")

	return cmd
}

// discoverRepos walks root and discovers git repos, optionally registering them.
func discoverRepos(ctx context.Context, root string, cfg *config.Config, tags []string, maxDepth int, dryRun, worktrees bool) ([]scanResult, error) {
	rootDepth := strings.Count(root, string(os.PathSeparator))
	var found []scanResult

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
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
		found = append(found, classifyRepo(cfg, name, path, "", tags, dryRun))

		// Discover linked worktrees if flag is set.
		if worktrees {
			wts, wtErr := git.WorktreeList(ctx, path)
			if wtErr == nil {
				for _, wt := range wts {
					wtName := name + "/" + wt.Branch
					found = append(found, classifyRepo(cfg, wtName, wt.Path, name, tags, dryRun))
				}
			}
		}

		return fs.SkipDir
	})

	return found, err
}

// classifyRepo determines whether a repo should be registered, is already registered, or had an error.
func classifyRepo(cfg *config.Config, name, path, worktreeOf string, tags []string, dryRun bool) scanResult {
	r := scanResult{Name: name, Path: path, IsWorktree: worktreeOf != ""}

	if dryRun {
		for _, existing := range cfg.Repos {
			if existing.Path == path {
				r.AlreadyRegistered = true
				return r
			}
		}
		r.Registered = true
		return r
	}

	entry := config.RepoEntry{Name: name, Path: path, Tags: tags, WorktreeOf: worktreeOf}
	_, addErr := config.AddRepo(cfg, entry)
	if addErr != nil {
		if errors.Is(addErr, config.ErrRepoAlreadyExists) {
			r.AlreadyRegistered = true
		} else {
			r.Error = addErr.Error()
		}
	} else {
		r.Registered = true
	}

	return r
}

// renderScanTable prints the scan results as a table and returns counts.
func renderScanTable(w io.Writer, found []scanResult, root string, maxDepth int, worktreesFlag bool, tags []string, dryRun bool) (registered, existing, wtCount int) {
	// Print header.
	scanHeader := fmt.Sprintf("scanning %s (depth: %d", shortenHome(root), maxDepth)
	if worktreesFlag {
		scanHeader += ", worktrees: on"
	}
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
	for _, r := range found {
		shortPath := shortenHome(r.Path)
		displayName := r.Name
		if r.IsWorktree {
			displayName = r.Name + " → " + strings.SplitN(r.Name, "/", 2)[0]
		}

		switch {
		case r.Error != "":
			_, _ = fmt.Fprintln(w, output.Red(fmt.Sprintf(
				"  %-*s %-8s %s", nameWidth, displayName, output.SymConflict, r.Error)))
		case r.AlreadyRegistered:
			_, _ = fmt.Fprintf(w, "  %-*s %s       %s\n",
				nameWidth, displayName,
				output.Green(output.SymClean),
				output.Dim(shortPath))
			existing++
		case r.Registered:
			_, _ = fmt.Fprintln(w, output.Green(fmt.Sprintf(
				"  %-*s %s       %s", nameWidth, displayName, output.SymClean, shortPath)))
			registered++
		}
		if r.IsWorktree {
			wtCount++
		}
	}

	return registered, existing, wtCount
}

// renderScanSummary prints the scan summary line.
func renderScanSummary(w io.Writer, found []scanResult, registered, existing, wtCount int, dryRun bool) {
	_, _ = fmt.Fprintln(w)
	summary := fmt.Sprintf("found %d %s", len(found), output.Plural(len(found), "repo"))
	if dryRun {
		summary += fmt.Sprintf(" · %d not initialized · %d already in soko", registered, existing)
	} else {
		summary += fmt.Sprintf(" · %d initialized · %d already in soko", registered, existing)
	}
	if wtCount > 0 {
		summary += fmt.Sprintf(" · %d %s", wtCount, output.Plural(wtCount, "worktree"))
	}
	output.Info(w, summary)
}

type scanResult struct {
	Name              string `json:"name"`
	Path              string `json:"path"`
	Registered        bool   `json:"registered"`
	AlreadyRegistered bool   `json:"already_registered"`
	IsWorktree        bool   `json:"is_worktree,omitempty"`
	Error             string `json:"error,omitempty"`
}

func renderScanJSON(w io.Writer, results []scanResult) error {
	return output.RenderJSON(w, results)
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
