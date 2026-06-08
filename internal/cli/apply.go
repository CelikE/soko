package cli

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"

	"github.com/CelikE/soko/internal/config"
	"github.com/CelikE/soko/internal/output"
)

type applyAction string

const (
	applyCreate    applyAction = "create"
	applyUpdate    applyAction = "update"
	applyUnchanged applyAction = "unchanged"
	applyError     applyAction = "error"
)

type applyResult struct {
	Name    string      `json:"repo"`
	Path    string      `json:"path"`
	Dest    string      `json:"dest"`
	Action  applyAction `json:"action"`
	Diff    string      `json:"diff,omitempty"`
	Written bool        `json:"written,omitempty"`
	Error   string      `json:"error,omitempty"`
}

// newApplyCmd creates the cobra command for soko apply.
func newApplyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "apply <source-file> [repos...]",
		Short: "Copy a file into many repos with a diff preview",
		Long: `Copy or template a single source file into many registered repos at once.

By default soko apply is a dry run: it shows a per-repo unified diff and writes
nothing. Pass --write to apply the changes (with a confirmation prompt unless
--force). The destination is given with --to, relative to each repo root.

Writes are local file operations — apply never touches git or the network.`,
		Example: `  soko apply ci.yml --to .github/workflows/ci.yml          # dry-run diff
  soko apply ci.yml --to .github/workflows/ci.yml --tag go # only repos tagged "go"
  soko apply LICENSE --to LICENSE backend auth             # only these repos
  soko apply ci.yml --to .github/workflows/ci.yml --write  # write after confirmation
  soko apply ci.yml --to .github/workflows/ci.yml --write --force # no prompt
  soko apply .editorconfig --to .editorconfig --json       # machine-readable plan`,
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: repoNameCompletionFunc(),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			w := cmd.OutOrStdout()

			src := args[0]
			repoArgs := args[1:]

			dest, _ := cmd.Flags().GetString("to")
			write, _ := cmd.Flags().GetBool("write")
			force, _ := cmd.Flags().GetBool("force")
			jsonFlag, _ := cmd.Flags().GetBool("json")

			if jsonFlag && write && !force {
				return fmt.Errorf("--json with --write requires --force")
			}

			// Read the source once up front; a bad source is a hard error before
			// any repo is touched — it is the one input the user fully controls.
			srcBytes, err := os.ReadFile(src)
			if err != nil {
				return fmt.Errorf("reading source file: %w", err)
			}
			srcInfo, err := os.Stat(src)
			if err != nil {
				return fmt.Errorf("stat source file: %w", err)
			}
			srcMode := srcInfo.Mode().Perm()

			cfg, repos, err := loadReposWithTagFilter(cmd)
			if err != nil {
				return err
			}

			if len(repoArgs) > 0 {
				repos = findReposMatching(repos, repoArgs)
				if len(repos) == 0 {
					output.Info(w, fmt.Sprintf("no repos found matching: %s", strings.Join(repoArgs, ", ")))
					return nil
				}
			}

			if len(repos) == 0 {
				output.Info(w, noReposMessage(len(cfg.Repos)))
				return nil
			}

			if noWorktrees, _ := cmd.Flags().GetBool("no-worktrees"); noWorktrees {
				repos = config.FilterNoWorktrees(repos)
			}

			results := planApply(ctx, repos, srcBytes, dest)

			var create, update, unchanged, missing int
			for i := range results {
				switch results[i].Action {
				case applyCreate:
					create++
				case applyUpdate:
					update++
				case applyUnchanged:
					unchanged++
				case applyError:
					if results[i].Error == "path not found" {
						missing++
					}
				}
			}

			if !write {
				if jsonFlag {
					return output.RenderJSON(w, results)
				}
				output.RenderApplyPlan(w, toApplyRows(results))
				output.RenderApplySummary(w, create, update, unchanged, len(results), false)
				if create+update > 0 {
					output.Info(w, "run with --write to apply")
				}
				renderMissingHint(w, missing)
				return nil
			}

			changed := create + update
			if changed == 0 {
				if jsonFlag {
					return output.RenderJSON(w, results)
				}
				output.Info(w, "nothing to apply — all repos already in sync")
				renderMissingHint(w, missing)
				return nil
			}

			if !force {
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(),
					"\n  apply %s to %d %s (%d create, %d update)? [y/N] ",
					filepath.Base(src), changed, output.Plural(changed, "repo"), create, update)
				scanner := bufio.NewScanner(cmd.InOrStdin())
				if !scanner.Scan() {
					return nil
				}
				answer := strings.TrimSpace(strings.ToLower(scanner.Text()))
				if answer != "y" && answer != "yes" {
					_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "  aborted")
					return nil
				}
			}

			writeApply(ctx, results, srcBytes, srcMode)

			if jsonFlag {
				return output.RenderJSON(w, results)
			}

			var rows []output.ActionRow
			var okCreate, okUpdate, failed int
			for i := range results {
				r := &results[i]
				if r.Action != applyCreate && r.Action != applyUpdate {
					continue
				}
				if r.Written {
					verb := "created"
					if r.Action == applyUpdate {
						verb = "updated"
						okUpdate++
					} else {
						okCreate++
					}
					rows = append(rows, output.ActionRow{Name: r.Name, Success: true, Message: fmt.Sprintf("%s %s", verb, r.Dest)})
				} else {
					failed++
					rows = append(rows, output.ActionRow{Name: r.Name, Success: false, Message: r.Error})
				}
			}

			_, _ = fmt.Fprintln(w)
			output.RenderActionResults(w, rows)
			output.RenderApplySummary(w, okCreate, okUpdate, unchanged, len(results), true)
			renderMissingHint(w, missing)

			if failed > 0 {
				return fmt.Errorf("%d %s failed to write", failed, output.Plural(failed, "repo"))
			}
			return nil
		},
	}

	cmd.Flags().String("to", "", "destination path relative to each repo root (required)")
	_ = cmd.MarkFlagRequired("to")
	cmd.Flags().Bool("write", false, "write the files (default is a dry-run diff)")
	cmd.Flags().Bool("force", false, "skip the confirmation prompt (with --write)")
	cmd.Flags().Bool("no-worktrees", false, "skip worktree entries, only apply to parent repos")
	cmd.Flags().StringSlice("tag", nil, "filter by tag (can be repeated, combines with OR)")
	_ = cmd.RegisterFlagCompletionFunc("tag", tagCompletionFunc())

	return cmd
}

// planApply computes the per-repo apply plan in parallel. It is read-only:
// it reads each destination and classifies the outcome but writes nothing.
func planApply(ctx context.Context, repos []config.RepoEntry, srcBytes []byte, dest string) []applyResult {
	results := make([]applyResult, len(repos))

	g, _ := errgroup.WithContext(ctx)
	g.SetLimit(maxConcurrency)

	for i, repo := range repos {
		g.Go(func() error {
			r := applyResult{Name: repo.Name, Path: repo.Path, Dest: dest}

			switch {
			case !pathExists(repo.Path):
				r.Action = applyError
				r.Error = "path not found"
			case filepath.IsAbs(dest):
				r.Action = applyError
				r.Error = "destination must be relative to the repo root"
			default:
				destAbs := filepath.Join(repo.Path, dest)
				if !config.PathWithinRoot(destAbs, repo.Path) {
					r.Action = applyError
					r.Error = "destination escapes repo root"
					break
				}
				cur, readErr := os.ReadFile(destAbs)
				switch {
				case readErr != nil && os.IsNotExist(readErr):
					r.Action = applyCreate
					r.Diff = output.DiffUnified(nil, srcBytes, dest)
				case readErr != nil:
					// Unreadable or a directory at the destination path.
					r.Action = applyError
					r.Error = readErr.Error()
				case bytes.Equal(cur, srcBytes):
					r.Action = applyUnchanged
				default:
					r.Action = applyUpdate
					r.Diff = output.DiffUnified(cur, srcBytes, dest)
				}
			}

			results[i] = r
			return nil
		})
	}

	_ = g.Wait()
	return results
}

// writeApply writes the source bytes to the destination of every create/update
// result, atomically (temp file + rename), in parallel. Each result's Written
// or Error is set; Action is left intact so the success message can name the
// original operation.
func writeApply(ctx context.Context, results []applyResult, srcBytes []byte, mode os.FileMode) {
	g, _ := errgroup.WithContext(ctx)
	g.SetLimit(maxConcurrency)

	for i := range results {
		r := &results[i]
		if r.Action != applyCreate && r.Action != applyUpdate {
			continue
		}
		g.Go(func() error {
			destAbs := filepath.Join(r.Path, r.Dest)
			if err := atomicWriteFile(destAbs, srcBytes, mode); err != nil {
				r.Error = err.Error()
			} else {
				r.Written = true
			}
			return nil
		})
	}

	_ = g.Wait()
}

// atomicWriteFile writes data to path via a temp file in the destination
// directory followed by a rename, creating parent directories and applying the
// given mode, so a crash mid-write never leaves a half-written file.
func atomicWriteFile(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".soko-apply-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // no-op once renamed

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, mode); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// toApplyRows projects apply results into the output package's view type.
func toApplyRows(results []applyResult) []output.ApplyRow {
	rows := make([]output.ApplyRow, len(results))
	for i := range results {
		r := &results[i]
		rows[i] = output.ApplyRow{
			Name:   r.Name,
			Dest:   r.Dest,
			Action: string(r.Action),
			Diff:   r.Diff,
			Err:    r.Error,
		}
	}
	return rows
}
