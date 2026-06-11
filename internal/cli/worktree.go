package cli

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"sync"

	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"

	"github.com/CelikE/soko/internal/config"
	"github.com/CelikE/soko/internal/git"
	"github.com/CelikE/soko/internal/output"
)

// newWorktreeCmd creates the cobra command for soko worktree and subcommands.
func newWorktreeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "worktree",
		Short: "Manage git worktrees across repos",
		Long: `Create, list, and remove linked git worktrees with automatic registry
bookkeeping. soko already tracks worktrees (scan --worktrees, init --worktree);
this command manages their lifecycle: one step instead of git worktree add,
cd, soko init --worktree, and the reverse on teardown.`,
		Example: `  soko worktree add api feat-x        # create + register, print the path
  soko worktree add api feat-x -b     # create the branch too
  soko worktree list                  # all worktrees across repos
  soko worktree rm api/feat-x         # remove + unregister`,
	}

	cmd.AddCommand(newWorktreeAddCmd())
	cmd.AddCommand(newWorktreeListCmd())
	cmd.AddCommand(newWorktreeRmCmd())

	return cmd
}

// worktreeNameCompletionFunc completes registered worktree entry names.
func worktreeNameCompletionFunc() cobra.CompletionFunc {
	return func(_ *cobra.Command, _ []string, _ string) ([]cobra.Completion, cobra.ShellCompDirective) {
		cfg, err := config.Load()
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		var names []cobra.Completion
		for i := range cfg.Repos {
			if cfg.Repos[i].IsWorktreeEntry() {
				names = append(names, cfg.Repos[i].Name)
			}
		}
		return names, cobra.ShellCompDirectiveNoFileComp
	}
}

func newWorktreeAddCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add <repo> <branch>",
		Short: "Create a worktree and register it",
		Long: `Create a linked worktree for a registered repo and register it in one step.

The worktree lands next to the main repo as <repo>-<branch> unless --path
says otherwise, and registers under the existing parent/branch naming so
status, cd, and go pick it up unchanged. The branch must exist — pass -b to
create it (from the repo's current HEAD) in the same step.

The new path is printed last on stdout, so shell users can:
  cd "$(soko worktree add api feat-x -q)"`,
		Args:              cobra.ExactArgs(2),
		ValidArgsFunction: repoNameCompletionFunc(),
		RunE:              runWorktreeAdd,
	}
	cmd.Flags().StringP("path", "p", "", "directory for the new worktree (default: sibling of the main repo)")
	cmd.Flags().BoolP("create-branch", "b", false, "create the branch if it does not exist")
	cmd.Flags().StringSlice("tag", nil, "tags for the new worktree entry (can be repeated)")
	_ = cmd.RegisterFlagCompletionFunc("tag", tagCompletionFunc())
	return cmd
}

func runWorktreeAdd(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	w := cmd.OutOrStdout()
	repoQuery, branch := args[0], args[1]

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	parent, err := resolveWorktreeParent(cfg, repoQuery)
	if err != nil {
		return err
	}
	if !pathExists(parent.Path) {
		return fmt.Errorf("%s: path not found: %s", parent.Name, parent.Path)
	}

	entryName := parent.Name + "/" + branch
	for i := range cfg.Repos {
		if cfg.Repos[i].Name == entryName {
			return fmt.Errorf("worktree %s already registered (%s)", entryName, cfg.Repos[i].Path)
		}
	}

	createBranch, _ := cmd.Flags().GetBool("create-branch")
	branchExists := branchExistsLocally(cmd, parent.Path, branch)
	if !branchExists && !createBranch {
		return fmt.Errorf("branch %q does not exist in %s — pass -b to create it", branch, parent.Name)
	}
	if branchExists && createBranch {
		// -b on an existing branch would make git error; just check it out.
		createBranch = false
	}

	wtPath, _ := cmd.Flags().GetString("path")
	if wtPath == "" {
		// Sibling of the main repo, branch slashes flattened so feat/x nests
		// next to the repo, not inside a feat/ directory.
		dirName := filepath.Base(parent.Path) + "-" + strings.ReplaceAll(branch, "/", "-")
		wtPath = filepath.Join(filepath.Dir(parent.Path), dirName)
	}
	wtPath, err = filepath.Abs(wtPath)
	if err != nil {
		return fmt.Errorf("resolving path: %w", err)
	}
	if pathExists(wtPath) {
		return fmt.Errorf("path already exists: %s", wtPath)
	}

	gitArgs := []string{"worktree", "add", wtPath, branch}
	if createBranch {
		gitArgs = []string{"worktree", "add", "-b", branch, wtPath}
	}
	if _, err := git.Run(ctx, parent.Path, gitArgs...); err != nil {
		return fmt.Errorf("creating worktree: %w", err)
	}

	tags, _ := cmd.Flags().GetStringSlice("tag")
	entry := config.RepoEntry{Name: entryName, Path: wtPath, Tags: tags, WorktreeOf: parent.Name}
	if cfg, err = config.AddRepo(cfg, &entry); err != nil {
		return fmt.Errorf("registering worktree: %w", err)
	}
	if err := config.Save(cfg); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	jsonFlag, _ := cmd.Flags().GetBool("json")
	if jsonFlag {
		return output.RenderJSON(w, struct {
			Name   string `json:"name"`
			Parent string `json:"parent"`
			Branch string `json:"branch"`
			Path   string `json:"path"`
		}{entryName, parent.Name, branch, wtPath})
	}

	output.Confirm(w, fmt.Sprintf("created worktree %s", entryName))
	output.Info(w, fmt.Sprintf("jump there with: soko cd %s", entryName))
	// The bare path goes last on stdout so command substitution picks it up.
	_, _ = fmt.Fprintln(w, wtPath)
	return nil
}

// resolveWorktreeParent finds the main repo for a query, following a worktree
// entry to its parent so `worktree add api/feat-x other-branch` works too.
func resolveWorktreeParent(cfg *config.Config, query string) (*config.RepoEntry, error) {
	matches := config.FindRepo(cfg, query)
	if len(matches) == 0 {
		return nil, notFoundWithSuggestions(query, cfg.Repos)
	}
	if len(matches) > 1 {
		names := make([]string, len(matches))
		for i := range matches {
			names[i] = matches[i].Name
		}
		return nil, fmt.Errorf("ambiguous repo %q — matches: %s", query, strings.Join(names, ", "))
	}

	entry := matches[0]
	if !entry.IsWorktreeEntry() {
		return &entry, nil
	}
	for i := range cfg.Repos {
		if cfg.Repos[i].Name == entry.WorktreeOf {
			return &cfg.Repos[i], nil
		}
	}
	return nil, fmt.Errorf("parent repo %q of worktree %s is not registered", entry.WorktreeOf, entry.Name)
}

// branchExistsLocally reports whether a local branch exists in the repo.
func branchExistsLocally(cmd *cobra.Command, dir, branch string) bool {
	_, err := git.Run(cmd.Context(), dir, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
	return err == nil
}

type worktreeRow struct {
	index   int
	Name    string `json:"name"`
	Parent  string `json:"parent"`
	Branch  string `json:"branch"`
	Status  string `json:"status"`
	Path    string `json:"path"`
	missing bool
	dirty   bool
}

func newWorktreeListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List registered worktrees across repos",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			w := cmd.OutOrStdout()

			cfg, err := config.Load()
			if err != nil {
				return err
			}

			var entries []config.RepoEntry
			for i := range cfg.Repos {
				if cfg.Repos[i].IsWorktreeEntry() {
					entries = append(entries, cfg.Repos[i])
				}
			}

			jsonFlag, _ := cmd.Flags().GetBool("json")
			if len(entries) == 0 {
				if jsonFlag {
					_, _ = fmt.Fprintln(w, "[]")
					return nil
				}
				output.Info(w, "no worktrees registered — create one with: soko worktree add <repo> <branch>")
				return nil
			}

			rows := make([]worktreeRow, 0, len(entries))
			var mu sync.Mutex
			g, gctx := errgroup.WithContext(ctx)
			g.SetLimit(maxConcurrency)
			for i, e := range entries {
				g.Go(func() error {
					row := worktreeRow{index: i, Name: e.Name, Parent: e.WorktreeOf, Path: e.Path}
					switch {
					case !pathExists(e.Path):
						row.missing = true
						row.Branch = "—"
						row.Status = "missing"
					default:
						branch, branchErr := git.Run(gctx, e.Path, "rev-parse", "--abbrev-ref", "HEAD")
						if branchErr != nil {
							branch = "—"
						}
						row.Branch = branch
						row.Status = "clean"
						if st, stErr := git.Run(gctx, e.Path, "status", "--porcelain"); stErr == nil && st != "" {
							row.dirty = true
							row.Status = "dirty"
						}
					}
					mu.Lock()
					rows = append(rows, row)
					mu.Unlock()
					return nil
				})
			}
			_ = g.Wait()

			ordered := make([]worktreeRow, len(rows))
			for i := range rows {
				ordered[rows[i].index] = rows[i]
			}

			if jsonFlag {
				return output.RenderJSON(w, ordered)
			}

			renderWorktreeTable(w, ordered)
			var missing int
			for i := range ordered {
				if ordered[i].missing {
					missing++
				}
			}
			if missing > 0 {
				output.Warn(w, fmt.Sprintf("%d %s missing on disk — run soko prune", missing, output.Plural(missing, "worktree")))
			}
			return nil
		},
	}
}

func renderWorktreeTable(w io.Writer, rows []worktreeRow) {
	cName, cParent, cBranch, cStatus := len("WORKTREE"), len("PARENT"), len("BRANCH"), len("STATUS")
	for i := range rows {
		if len(rows[i].Name) > cName {
			cName = len(rows[i].Name)
		}
		if len(rows[i].Parent) > cParent {
			cParent = len(rows[i].Parent)
		}
		if len(rows[i].Branch) > cBranch {
			cBranch = len(rows[i].Branch)
		}
		if len(rows[i].Status) > cStatus {
			cStatus = len(rows[i].Status)
		}
	}
	cName += 2
	cParent += 2
	cBranch += 2
	cStatus += 2

	header := fmt.Sprintf("  %-*s %-*s %-*s %-*s %s",
		cName, "WORKTREE", cParent, "PARENT", cBranch, "BRANCH", cStatus, "STATUS", "PATH")
	_, _ = fmt.Fprintln(w, output.Dim(header))
	_, _ = fmt.Fprintln(w, output.Dim("  "+strings.Repeat("─", len(header)-2)))

	for i := range rows {
		r := &rows[i]
		line := fmt.Sprintf("  %-*s %-*s %-*s %-*s %s",
			cName, r.Name, cParent, r.Parent, cBranch, r.Branch, cStatus, r.Status, r.Path)
		switch {
		case r.missing:
			_, _ = fmt.Fprintln(w, output.Red(line))
		case r.dirty:
			_, _ = fmt.Fprintln(w, output.Yellow(line))
		default:
			_, _ = fmt.Fprintln(w, output.Green(line))
		}
	}
}

func newWorktreeRmCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rm <worktree>",
		Short: "Remove a worktree and unregister it",
		Long: `Remove a linked worktree's directory (git worktree remove in the parent
repo) and drop its registry entry. A dirty worktree is refused without
--force. Branches are never touched — that is soko clean's job.`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: worktreeNameCompletionFunc(),
		RunE:              runWorktreeRm,
	}
	cmd.Flags().Bool("force", false, "remove even if the worktree has uncommitted changes")
	return cmd
}

func runWorktreeRm(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	w := cmd.OutOrStdout()

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	matches := config.FindRepo(cfg, args[0])
	var worktrees []config.RepoEntry
	for i := range matches {
		if matches[i].IsWorktreeEntry() {
			worktrees = append(worktrees, matches[i])
		}
	}
	if len(worktrees) == 0 {
		return fmt.Errorf("no worktree matching: %s (worktree rm only removes worktree entries — see soko remove)", args[0])
	}
	if len(worktrees) > 1 {
		names := make([]string, len(worktrees))
		for i := range worktrees {
			names[i] = worktrees[i].Name
		}
		return fmt.Errorf("ambiguous worktree %q — matches: %s", args[0], strings.Join(names, ", "))
	}
	entry := worktrees[0]

	var parentPath string
	for i := range cfg.Repos {
		if cfg.Repos[i].Name == entry.WorktreeOf {
			parentPath = cfg.Repos[i].Path
			break
		}
	}
	if parentPath == "" {
		return fmt.Errorf("parent repo %q of worktree %s is not registered", entry.WorktreeOf, entry.Name)
	}

	force, _ := cmd.Flags().GetBool("force")

	if pathExists(entry.Path) {
		if !force {
			if st, stErr := git.Run(ctx, entry.Path, "status", "--porcelain"); stErr == nil && st != "" {
				return fmt.Errorf("worktree %s has uncommitted changes — use --force to remove anyway", entry.Name)
			}
		}
		rmArgs := []string{"worktree", "remove", entry.Path}
		if force {
			rmArgs = []string{"worktree", "remove", "--force", entry.Path}
		}
		if _, err := git.Run(ctx, parentPath, rmArgs...); err != nil {
			return fmt.Errorf("removing worktree: %w", err)
		}
	} else if pathExists(parentPath) {
		// Directory already gone — let git forget its administrative record.
		_, _ = git.Run(ctx, parentPath, "worktree", "prune")
	}

	if cfg, _, err = config.RemoveRepoByPath(cfg, entry.Path); err != nil {
		return fmt.Errorf("unregistering worktree: %w", err)
	}
	if err := config.Save(cfg); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	jsonFlag, _ := cmd.Flags().GetBool("json")
	if jsonFlag {
		return output.RenderJSON(w, struct {
			Name    string `json:"name"`
			Path    string `json:"path"`
			Removed bool   `json:"removed"`
		}{entry.Name, entry.Path, true})
	}

	output.Confirm(w, fmt.Sprintf("removed worktree %s (%s)", entry.Name, entry.Path))
	return nil
}
