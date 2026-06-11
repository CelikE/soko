package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"

	"github.com/CelikE/soko/internal/config"
	"github.com/CelikE/soko/internal/git"
	"github.com/CelikE/soko/internal/output"
	"github.com/CelikE/soko/internal/snapshot"
)

type snapshotRepoResult struct {
	index     int
	name      string
	branch    string
	sha       string
	dirty     bool
	success   bool
	message   string
	errorCode string
}

// newSnapshotCmd creates the cobra command for soko snapshot and its subcommands.
func newSnapshotCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "snapshot",
		Short: "Save and restore exact repo positions (branch + commit)",
		Long: `Record the exact position of every repo — branch and HEAD SHA — under a
named snapshot, and move back to it later. The save game before the boss
fight: take one before a risky bulk operation (sync, pull, clean), and
restore returns every branch to the recorded commit.

Snapshots pin commits; they never touch uncommitted work. Saving records
whether a repo was dirty, and restoring refuses dirty repos — stash or use
soko ctx for work in progress.`,
		Example: `  soko snapshot save pre-sync       # record branch + SHA per repo
  soko snapshot restore pre-sync    # move each repo back (refuses dirty)
  soko snapshot list                # saved snapshots
  soko snapshot show pre-sync       # per-repo detail
  soko snapshot drop pre-sync       # delete it`,
	}

	cmd.AddCommand(newSnapshotSaveCmd())
	cmd.AddCommand(newSnapshotRestoreCmd())
	cmd.AddCommand(newSnapshotListCmd())
	cmd.AddCommand(newSnapshotShowCmd())
	cmd.AddCommand(newSnapshotDropCmd())

	return cmd
}

// snapshotNameCompletionFunc completes saved snapshot names. It fails silently
// if the snapshots directory can't be read.
func snapshotNameCompletionFunc() cobra.CompletionFunc {
	return func(_ *cobra.Command, _ []string, _ string) ([]cobra.Completion, cobra.ShellCompDirective) {
		names, err := snapshot.Names()
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		completions := make([]cobra.Completion, len(names))
		copy(completions, names)
		return completions, cobra.ShellCompDirectiveNoFileComp
	}
}

func newSnapshotSaveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "save <name> [repos...]",
		Short: "Record branch and HEAD SHA per repo",
		Long: `Record each selected repo's branch and HEAD commit under a named snapshot.
Nothing in the repos is modified — dirty trees are recorded as dirty and left
alone. Re-saving an existing name requires --force.`,
		Args: cobra.MinimumNArgs(1),
		RunE: runSnapshotSave,
	}
	cmd.Flags().Bool("force", false, "overwrite an existing snapshot")
	cmd.Flags().StringSlice("tag", nil, "filter by tag (can be repeated, combines with OR)")
	_ = cmd.RegisterFlagCompletionFunc("tag", tagCompletionFunc())
	return cmd
}

func runSnapshotSave(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	w := cmd.OutOrStdout()

	name := args[0]
	if err := snapshot.ValidateName(name); err != nil {
		return err
	}

	force, _ := cmd.Flags().GetBool("force")
	exists, err := snapshot.Exists(name)
	if err != nil {
		return err
	}
	if exists && !force {
		return fmt.Errorf("snapshot %q already exists — use --force to overwrite", name)
	}

	cfg, repos, err := loadReposWithTagFilter(cmd)
	if err != nil {
		return err
	}
	if len(args) > 1 {
		repos = findReposMatching(repos, args[1:])
		if len(repos) == 0 {
			output.Info(w, fmt.Sprintf("no repos found matching: %s", strings.Join(args[1:], ", ")))
			return nil
		}
	}
	if len(repos) == 0 {
		output.Info(w, noReposMessage(len(cfg.Repos)))
		return nil
	}

	jsonFlag, _ := cmd.Flags().GetBool("json")

	results := make([]snapshotRepoResult, 0, len(repos))
	var mu sync.Mutex

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(maxConcurrency)

	for i, repo := range repos {
		g.Go(func() error {
			r := snapshotSaveRepo(gctx, i, &repo)
			mu.Lock()
			results = append(results, r)
			mu.Unlock()
			return nil
		})
	}
	_ = g.Wait()

	ordered := make([]snapshotRepoResult, len(results))
	for idx := range results {
		ordered[results[idx].index] = results[idx]
	}

	// Record only the repos whose state was captured; failed repos are
	// reported but not part of the snapshot.
	snap := &snapshot.Snapshot{Name: name, Created: time.Now().UTC()}
	for i := range ordered {
		if !ordered[i].success {
			continue
		}
		snap.Repos = append(snap.Repos, snapshot.Repo{
			Name:     ordered[i].name,
			Path:     repos[ordered[i].index].Path,
			Branch:   ordered[i].branch,
			Detached: ordered[i].message == "detached",
			SHA:      ordered[i].sha,
			Dirty:    ordered[i].dirty,
		})
	}
	if len(snap.Repos) == 0 {
		return fmt.Errorf("no repo state could be saved for snapshot %q", name)
	}
	if err := snapshot.Save(snap); err != nil {
		return fmt.Errorf("saving snapshot: %w", err)
	}

	if jsonFlag {
		if err := renderSnapshotJSON(w, ordered); err != nil {
			return err
		}
		return snapshotFailureError(ordered, "save")
	}

	var dirty, failed int
	for i := range ordered {
		r := &ordered[i]
		switch {
		case !r.success:
			output.Fail(w, fmt.Sprintf("%s — %s", r.name, gitErrorReason(r.message)))
			failed++
		case r.dirty:
			output.Confirm(w, fmt.Sprintf("%s — %s @ %s (dirty)", r.name, r.branch, shortSHA(r.sha)))
			dirty++
		default:
			output.Confirm(w, fmt.Sprintf("%s — %s @ %s", r.name, r.branch, shortSHA(r.sha)))
		}
	}

	_, _ = fmt.Fprintln(w)
	output.Info(w, fmt.Sprintf("snapshot %q saved · %d %s · %d dirty",
		name, len(snap.Repos), output.Plural(len(snap.Repos), "repo"), dirty))
	output.Info(w, fmt.Sprintf("restore with: soko snapshot restore %s", name))

	if failed > 0 {
		return fmt.Errorf("%d %s failed to save", failed, output.Plural(failed, "repo"))
	}
	return nil
}

// snapshotSaveRepo captures one repo's branch and HEAD SHA. It never modifies
// the repo.
func snapshotSaveRepo(ctx context.Context, index int, repo *config.RepoEntry) snapshotRepoResult {
	r := snapshotRepoResult{index: index, name: repo.Name}

	if !pathExists(repo.Path) {
		r.message = "path not found"
		r.errorCode = codePathMissing
		return r
	}

	branch, detached, err := git.CurrentBranch(ctx, repo.Path)
	if err != nil {
		r.message = err.Error()
		r.errorCode = gitErrorCode(err)
		return r
	}
	r.branch = branch
	if detached {
		r.message = "detached"
	}

	sha, err := git.Run(ctx, repo.Path, "rev-parse", "HEAD")
	if err != nil {
		r.message = err.Error()
		r.errorCode = gitErrorCode(err)
		return r
	}
	r.sha = strings.TrimSpace(sha)

	statusOut, err := git.Run(ctx, repo.Path, "status", "--porcelain")
	if err != nil {
		r.message = err.Error()
		r.errorCode = gitErrorCode(err)
		return r
	}
	r.dirty = statusOut != ""

	r.success = true
	return r
}

func newSnapshotRestoreCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "restore <name>",
		Short: "Move each repo back to the recorded branch and commit",
		Long: `Check out the recorded branch at the recorded commit in every repo of the
snapshot. A branch that moved since the save is pointed back at the recorded
commit (the old tip is reported, so nothing is silently lost); a branch that
was deleted is recreated.

A repo that is dirty right now is refused and left exactly as it is. A repo
whose path no longer exists is skipped with a warning.`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: snapshotNameCompletionFunc(),
		RunE:              runSnapshotRestore,
	}
	return cmd
}

func runSnapshotRestore(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	w := cmd.OutOrStdout()

	snap, err := snapshot.Load(args[0])
	if err != nil {
		if errors.Is(err, snapshot.ErrNotFound) {
			return snapshotUnknownError(args[0])
		}
		return err
	}

	jsonFlag, _ := cmd.Flags().GetBool("json")

	results := make([]snapshotRepoResult, 0, len(snap.Repos))
	var mu sync.Mutex

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(maxConcurrency)

	for i, sr := range snap.Repos {
		g.Go(func() error {
			r := snapshotRestoreRepo(gctx, i, &sr)
			mu.Lock()
			results = append(results, r)
			mu.Unlock()
			return nil
		})
	}
	_ = g.Wait()

	ordered := make([]snapshotRepoResult, len(results))
	for idx := range results {
		ordered[results[idx].index] = results[idx]
	}

	if jsonFlag {
		if err := renderSnapshotJSON(w, ordered); err != nil {
			return err
		}
		return snapshotFailureError(ordered, "restore")
	}

	var failed int
	for i := range ordered {
		r := &ordered[i]
		if r.success {
			output.Confirm(w, fmt.Sprintf("%s — %s", r.name, r.message))
		} else {
			output.Fail(w, fmt.Sprintf("%s — %s", r.name, gitErrorReason(r.message)))
			failed++
		}
	}

	_, _ = fmt.Fprintln(w)
	output.Info(w, fmt.Sprintf("snapshot %q restored · %d %s",
		args[0], len(ordered)-failed, output.Plural(len(ordered)-failed, "repo")))

	if failed > 0 {
		return fmt.Errorf("%d %s failed to restore", failed, output.Plural(failed, "repo"))
	}
	return nil
}

// snapshotRestoreRepo moves one repo back to its recorded branch@SHA: refuse
// if dirty, recreate or rewind the branch with checkout -B, report a tip that
// moved so the abandoned commit stays findable.
func snapshotRestoreRepo(ctx context.Context, index int, sr *snapshot.Repo) snapshotRepoResult {
	r := snapshotRepoResult{index: index, name: sr.Name, branch: sr.Branch, sha: sr.SHA}

	if !pathExists(sr.Path) {
		r.message = "path not found — skipped"
		r.errorCode = codePathMissing
		return r
	}

	// Never overwrite work in progress: a dirty repo is the user's to deal
	// with, restore refuses it and continues with the others.
	statusOut, err := git.Run(ctx, sr.Path, "status", "--porcelain")
	if err != nil {
		r.message = err.Error()
		r.errorCode = gitErrorCode(err)
		return r
	}
	if statusOut != "" {
		r.message = "dirty — commit or stash first"
		r.errorCode = codeDirtyTree
		return r
	}

	// The recorded commit must still exist; after a long time and a git gc
	// it may not.
	if _, err := git.Run(ctx, sr.Path, "cat-file", "-e", sr.SHA+"^{commit}"); err != nil {
		r.message = fmt.Sprintf("recorded commit %s no longer exists (gc'd?)", shortSHA(sr.SHA))
		r.errorCode = codeUnknown
		return r
	}

	if sr.Detached {
		if _, err := git.Run(ctx, sr.Path, "checkout", "--detach", sr.SHA); err != nil {
			r.message = err.Error()
			r.errorCode = gitErrorCode(err)
			return r
		}
		r.message = fmt.Sprintf("detached @ %s", shortSHA(sr.SHA))
		r.success = true
		return r
	}

	// Where does the branch point right now? Tells us whether restore is a
	// no-op, a rewind, or a recreation of a deleted branch.
	oldTip, tipErr := git.Run(ctx, sr.Path, "rev-parse", "--verify", "--quiet", "refs/heads/"+sr.Branch)
	oldTip = strings.TrimSpace(oldTip)

	if _, err := git.Run(ctx, sr.Path, "checkout", "-B", sr.Branch, sr.SHA); err != nil {
		r.message = err.Error()
		r.errorCode = gitErrorCode(err)
		return r
	}

	switch {
	case tipErr != nil:
		r.message = fmt.Sprintf("%s recreated @ %s", sr.Branch, shortSHA(sr.SHA))
	case oldTip == sr.SHA:
		r.message = fmt.Sprintf("%s @ %s", sr.Branch, shortSHA(sr.SHA))
	default:
		r.message = fmt.Sprintf("%s moved back to %s (was %s)",
			sr.Branch, shortSHA(sr.SHA), shortSHA(oldTip))
	}
	r.success = true
	return r
}

func newSnapshotListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List saved snapshots",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			w := cmd.OutOrStdout()
			snaps, err := snapshot.List()
			if err != nil {
				return err
			}
			jsonFlag, _ := cmd.Flags().GetBool("json")

			if jsonFlag {
				type snapListJSON struct {
					Name    string    `json:"name"`
					Created time.Time `json:"created"`
					Repos   int       `json:"repos"`
				}
				entries := make([]snapListJSON, 0, len(snaps))
				for _, s := range snaps {
					entries = append(entries, snapListJSON{Name: s.Name, Created: s.Created, Repos: len(s.Repos)})
				}
				return output.RenderJSON(w, entries)
			}

			if len(snaps) == 0 {
				output.Info(w, "no saved snapshots — create one with: soko snapshot save <name>")
				return nil
			}
			for _, s := range snaps {
				_, _ = fmt.Fprintf(w, "  %s  %s\n", s.Name, output.Dim(fmt.Sprintf(
					"%d %s · saved %s",
					len(s.Repos), output.Plural(len(s.Repos), "repo"),
					output.FormatTimeAgo(s.Created))))
			}
			return nil
		},
	}
}

func newSnapshotShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "show <name>",
		Short:             "Show a snapshot's per-repo detail",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: snapshotNameCompletionFunc(),
		RunE: func(cmd *cobra.Command, args []string) error {
			w := cmd.OutOrStdout()
			snap, err := snapshot.Load(args[0])
			if err != nil {
				if errors.Is(err, snapshot.ErrNotFound) {
					return snapshotUnknownError(args[0])
				}
				return err
			}
			jsonFlag, _ := cmd.Flags().GetBool("json")
			if jsonFlag {
				return output.RenderJSON(w, snap)
			}

			output.Info(w, fmt.Sprintf("snapshot %q · saved %s", snap.Name, output.FormatTimeAgo(snap.Created)))
			for _, sr := range snap.Repos {
				detail := fmt.Sprintf("%s @ %s", sr.Branch, shortSHA(sr.SHA))
				if sr.Detached {
					detail = fmt.Sprintf("detached @ %s", shortSHA(sr.SHA))
				}
				if sr.Dirty {
					detail += " · was dirty"
				}
				_, _ = fmt.Fprintf(w, "  %s  %s\n", sr.Name, output.Dim(detail))
			}
			return nil
		},
	}
}

func newSnapshotDropCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:               "drop <name>",
		Short:             "Delete a saved snapshot",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: snapshotNameCompletionFunc(),
		RunE: func(cmd *cobra.Command, args []string) error {
			w := cmd.OutOrStdout()
			exists, err := snapshot.Exists(args[0])
			if err != nil {
				return err
			}
			if !exists {
				return snapshotUnknownError(args[0])
			}

			force, _ := cmd.Flags().GetBool("force")
			if !force {
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "drop snapshot %q? [y/N] ", args[0])
				scanner := bufio.NewScanner(cmd.InOrStdin())
				if !scanner.Scan() {
					return nil
				}
				answer := strings.TrimSpace(strings.ToLower(scanner.Text()))
				if answer != "y" && answer != "yes" {
					_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "aborted")
					return nil
				}
			}

			if err := snapshot.Delete(args[0]); err != nil {
				return err
			}
			output.Info(w, fmt.Sprintf("dropped snapshot %q", args[0]))
			return nil
		},
	}
	cmd.Flags().Bool("force", false, "skip confirmation prompt")
	return cmd
}

// snapshotUnknownError builds the unknown-snapshot error, listing what exists.
func snapshotUnknownError(name string) error {
	names, err := snapshot.Names()
	if err != nil || len(names) == 0 {
		return fmt.Errorf("no snapshot named %q — none saved yet", name)
	}
	return fmt.Errorf("no snapshot named %q — saved snapshots: %s", name, strings.Join(names, ", "))
}

// snapshotFailureError returns the command-level error when any repo failed.
func snapshotFailureError(results []snapshotRepoResult, verb string) error {
	var failed int
	for i := range results {
		if !results[i].success {
			failed++
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d %s failed to %s", failed, output.Plural(failed, "repo"), verb)
	}
	return nil
}

type snapshotJSON struct {
	Name      string `json:"name"`
	Branch    string `json:"branch,omitempty"`
	SHA       string `json:"sha,omitempty"`
	Dirty     bool   `json:"dirty"`
	Status    string `json:"status"`
	Error     string `json:"error,omitempty"`
	ErrorCode string `json:"error_code,omitempty"`
}

func renderSnapshotJSON(w io.Writer, results []snapshotRepoResult) error {
	entries := make([]snapshotJSON, len(results))
	for i := range results {
		r := &results[i]
		entries[i] = snapshotJSON{Name: r.name, Branch: r.branch, SHA: r.sha, Dirty: r.dirty, Status: "ok"}
		if !r.success {
			entries[i].Status = "failed"
			entries[i].Error = r.message
			entries[i].ErrorCode = r.errorCode
		}
	}
	return output.RenderJSON(w, entries)
}
