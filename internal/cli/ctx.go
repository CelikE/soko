package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"

	"github.com/CelikE/soko/internal/config"
	"github.com/CelikE/soko/internal/git"
	"github.com/CelikE/soko/internal/output"
)

// ctxStashMessage is the stash message used for a context's stashes. Stashes
// are found again by this message (not by index, which shifts), so a context
// survives unrelated stash activity in the same repo.
func ctxStashMessage(name string) string {
	return "soko-ctx:" + name
}

type ctxRepoResult struct {
	index     int
	name      string
	branch    string
	stashed   bool
	files     int
	success   bool
	message   string
	errorCode string
}

// newCtxCmd creates the cobra command for soko ctx and its subcommands.
func newCtxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ctx",
		Short: "Save and restore workspace contexts",
		Long: `Save and restore the state of the whole workspace under a named context:
which branch each repo is on, plus uncommitted work (stashed per repo).

Switch between client projects, oncall firefighting, and feature work in one
command — no more visiting each repo to stash, note the branch, and reverse
it all from memory an hour later.`,
		Example: `  soko ctx save client-a            # record branches, stash dirty trees
  soko ctx switch client-a          # restore branches, pop the stashes
  soko ctx list                     # saved contexts
  soko ctx show client-a            # per-repo detail
  soko ctx drop client-a            # forget it (stashes stay recoverable)`,
	}

	cmd.AddCommand(newCtxSaveCmd())
	cmd.AddCommand(newCtxSwitchCmd())
	cmd.AddCommand(newCtxListCmd())
	cmd.AddCommand(newCtxShowCmd())
	cmd.AddCommand(newCtxDropCmd())

	return cmd
}

// contextNameCompletionFunc completes saved context names. It fails silently
// if the config can't be loaded.
func contextNameCompletionFunc() cobra.CompletionFunc {
	return func(_ *cobra.Command, _ []string, _ string) ([]cobra.Completion, cobra.ShellCompDirective) {
		cfg, err := config.Load()
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		names := config.ContextNames(cfg)
		completions := make([]cobra.Completion, len(names))
		copy(completions, names)
		return completions, cobra.ShellCompDirectiveNoFileComp
	}
}

func newCtxSaveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "save <name> [repos...]",
		Short: "Record branch per repo and stash dirty trees",
		Long: `Record the current branch of every selected repo under a named context, and
stash uncommitted changes (including untracked files) in the repos that have
any. Clean repos are recorded without a stash.

Re-saving an existing name requires --force and overwrites the recorded
branches; stashes from the earlier save stay in their repos.`,
		Args: cobra.MinimumNArgs(1),
		RunE: runCtxSave,
	}
	cmd.Flags().Bool("force", false, "overwrite an existing context")
	cmd.Flags().StringSlice("tag", nil, "filter by tag (can be repeated, combines with OR)")
	_ = cmd.RegisterFlagCompletionFunc("tag", tagCompletionFunc())
	return cmd
}

func runCtxSave(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	w := cmd.OutOrStdout()

	name := strings.TrimSpace(args[0])
	if name == "" || strings.ContainsAny(name, " \t") {
		return fmt.Errorf("invalid context name: %q (must be non-empty, no whitespace)", args[0])
	}

	cfg, repos, err := loadReposWithTagFilter(cmd)
	if err != nil {
		return err
	}

	force, _ := cmd.Flags().GetBool("force")
	if _, exists := config.GetContext(cfg, name); exists && !force {
		return fmt.Errorf("context %q already exists — use --force to overwrite", name)
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

	results := make([]ctxRepoResult, 0, len(repos))
	var mu sync.Mutex

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(maxConcurrency)

	for i, repo := range repos {
		g.Go(func() error {
			r := ctxSaveRepo(gctx, i, &repo, name)
			mu.Lock()
			results = append(results, r)
			mu.Unlock()
			return nil
		})
	}
	_ = g.Wait()

	ordered := make([]ctxRepoResult, len(results))
	for idx := range results {
		ordered[results[idx].index] = results[idx]
	}

	// Record only the repos whose state was captured; failed repos are
	// reported but not part of the context.
	entry := &config.ContextEntry{SavedAt: time.Now().UTC()}
	for i := range ordered {
		if !ordered[i].success {
			continue
		}
		entry.Repos = append(entry.Repos, config.ContextRepo{
			Name:     ordered[i].name,
			Branch:   ordered[i].branch,
			Detached: ordered[i].message == "detached",
			Stashed:  ordered[i].stashed,
		})
	}
	if len(entry.Repos) == 0 {
		return fmt.Errorf("no repo state could be saved for context %q", name)
	}
	config.SetContext(cfg, name, entry)
	if err := config.Save(cfg); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	if jsonFlag {
		if err := renderCtxJSON(w, ordered); err != nil {
			return err
		}
		return ctxFailureError(ordered, "save")
	}

	var stashed, failed int
	for i := range ordered {
		r := &ordered[i]
		switch {
		case !r.success:
			output.Fail(w, fmt.Sprintf("%s — %s", r.name, gitErrorReason(r.message)))
			failed++
		case r.stashed:
			output.Confirm(w, fmt.Sprintf("%s — %s, stashed %d %s",
				r.name, r.branch, r.files, output.Plural(r.files, "file")))
			stashed++
		default:
			output.Confirm(w, fmt.Sprintf("%s — %s, clean", r.name, r.branch))
		}
	}

	_, _ = fmt.Fprintln(w)
	output.Info(w, fmt.Sprintf("context %q saved · %d %s · %d stashed",
		name, len(entry.Repos), output.Plural(len(entry.Repos), "repo"), stashed))
	output.Info(w, fmt.Sprintf("switch back with: soko ctx switch %s", name))

	if failed > 0 {
		return fmt.Errorf("%d %s failed to save", failed, output.Plural(failed, "repo"))
	}
	return nil
}

// ctxSaveRepo captures one repo's branch and stashes its dirty tree under the
// context's stash message.
func ctxSaveRepo(ctx context.Context, index int, repo *config.RepoEntry, name string) ctxRepoResult {
	r := ctxRepoResult{index: index, name: repo.Name}

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

	statusOut, err := git.Run(ctx, repo.Path, "status", "--porcelain")
	if err != nil {
		r.message = err.Error()
		r.errorCode = gitErrorCode(err)
		return r
	}

	if statusOut != "" {
		r.files = len(strings.Split(statusOut, "\n"))
		if _, err := git.Run(ctx, repo.Path, "stash", "push", "-u", "-m", ctxStashMessage(name)); err != nil {
			r.message = err.Error()
			r.errorCode = gitErrorCode(err)
			return r
		}
		r.stashed = true
	}

	r.success = true
	return r
}

func newCtxSwitchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "switch <name>",
		Short: "Restore a saved context's branches and stashes",
		Long: `Check out the branch each repo had when the context was saved, and pop the
stash the save created (found by message, so unrelated stashes are untouched).

A repo that is dirty right now is refused and left exactly as it is — save the
current state first (soko ctx save <other-name>) or clean it up, then switch
again. A missing stash is reported but does not block the rest.`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: contextNameCompletionFunc(),
		RunE:              runCtxSwitch,
	}
	return cmd
}

func runCtxSwitch(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	w := cmd.OutOrStdout()
	name := args[0]

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	entry, ok := config.GetContext(cfg, name)
	if !ok {
		return ctxUnknownError(cfg, name)
	}

	jsonFlag, _ := cmd.Flags().GetBool("json")

	byName := make(map[string]*config.RepoEntry, len(cfg.Repos))
	for i := range cfg.Repos {
		byName[cfg.Repos[i].Name] = &cfg.Repos[i]
	}

	results := make([]ctxRepoResult, 0, len(entry.Repos))
	var mu sync.Mutex

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(maxConcurrency)

	for i, cr := range entry.Repos {
		g.Go(func() error {
			r := ctxSwitchRepo(gctx, i, byName[cr.Name], &cr, name)
			mu.Lock()
			results = append(results, r)
			mu.Unlock()
			return nil
		})
	}
	_ = g.Wait()

	ordered := make([]ctxRepoResult, len(results))
	for idx := range results {
		ordered[results[idx].index] = results[idx]
	}

	if jsonFlag {
		if err := renderCtxJSON(w, ordered); err != nil {
			return err
		}
		return ctxFailureError(ordered, "switch")
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
	output.Info(w, fmt.Sprintf("switched to context %q · %d %s",
		name, len(ordered)-failed, output.Plural(len(ordered)-failed, "repo")))

	if failed > 0 {
		return fmt.Errorf("%d %s failed to switch", failed, output.Plural(failed, "repo"))
	}
	return nil
}

// ctxSwitchRepo restores one repo: refuse if dirty, check out the recorded
// branch, pop the context's stash when one was taken.
func ctxSwitchRepo(ctx context.Context, index int, repo *config.RepoEntry, cr *config.ContextRepo, name string) ctxRepoResult {
	r := ctxRepoResult{index: index, name: cr.Name, branch: cr.Branch, stashed: cr.Stashed}

	if repo == nil {
		r.message = "not registered (removed since save?)"
		r.errorCode = codeUnknown
		return r
	}
	if !pathExists(repo.Path) {
		r.message = "path not found"
		r.errorCode = codePathMissing
		return r
	}

	// Never overwrite work in progress: a dirty repo is the user's to deal
	// with, switch refuses it and continues with the others.
	statusOut, err := git.Run(ctx, repo.Path, "status", "--porcelain")
	if err != nil {
		r.message = err.Error()
		r.errorCode = gitErrorCode(err)
		return r
	}
	if statusOut != "" {
		r.message = "dirty — commit, stash, or ctx save first"
		r.errorCode = codeDirtyTree
		return r
	}

	checkoutArgs := []string{"checkout", cr.Branch}
	if cr.Detached {
		checkoutArgs = []string{"checkout", "--detach", cr.Branch}
	}
	if _, err := git.Run(ctx, repo.Path, checkoutArgs...); err != nil {
		r.message = err.Error()
		r.errorCode = gitErrorCode(err)
		return r
	}

	r.message = cr.Branch
	if cr.Stashed {
		ref, found, err := findCtxStash(ctx, repo.Path, name)
		switch {
		case err != nil:
			r.message = err.Error()
			r.errorCode = gitErrorCode(err)
			return r
		case !found:
			// The stash is gone (popped manually, or dropped). The branch is
			// restored, which is the best we can do — report, don't fail.
			r.message = cr.Branch + " (stash missing — already popped?)"
		default:
			if _, err := git.Run(ctx, repo.Path, "stash", "pop", ref); err != nil {
				// On conflict git keeps the stash, so nothing is lost.
				r.message = err.Error()
				r.errorCode = gitErrorCode(err)
				return r
			}
			r.message = cr.Branch + " (stash restored)"
		}
	}

	r.success = true
	return r
}

// findCtxStash locates the most recent stash carrying the context's message.
// Matching by message suffix (not index) keeps the lookup stable against
// unrelated stash pushes and pops since the save.
func findCtxStash(ctx context.Context, dir, name string) (ref string, found bool, err error) {
	out, err := git.Run(ctx, dir, "stash", "list", "--format=%gd %gs")
	if err != nil {
		return "", false, err
	}
	for line := range strings.Lines(out) {
		line = strings.TrimSpace(line)
		if strings.HasSuffix(line, ctxStashMessage(name)) {
			refPart, _, _ := strings.Cut(line, " ")
			return refPart, true, nil
		}
	}
	return "", false, nil
}

func newCtxListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List saved contexts",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			w := cmd.OutOrStdout()
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			jsonFlag, _ := cmd.Flags().GetBool("json")

			names := config.ContextNames(cfg)
			if jsonFlag {
				type ctxListJSON struct {
					Name    string    `json:"name"`
					SavedAt time.Time `json:"saved_at"`
					Repos   int       `json:"repos"`
					Stashed int       `json:"stashed"`
				}
				entries := make([]ctxListJSON, 0, len(names))
				for _, n := range names {
					e, _ := config.GetContext(cfg, n)
					entries = append(entries, ctxListJSON{
						Name: n, SavedAt: e.SavedAt, Repos: len(e.Repos), Stashed: ctxStashedCount(e),
					})
				}
				return output.RenderJSON(w, entries)
			}

			if len(names) == 0 {
				output.Info(w, "no saved contexts — create one with: soko ctx save <name>")
				return nil
			}
			for _, n := range names {
				e, _ := config.GetContext(cfg, n)
				_, _ = fmt.Fprintf(w, "  %s  %s\n", n, output.Dim(fmt.Sprintf(
					"%d %s · %d stashed · saved %s",
					len(e.Repos), output.Plural(len(e.Repos), "repo"),
					ctxStashedCount(e), output.FormatTimeAgo(e.SavedAt))))
			}
			return nil
		},
	}
}

func newCtxShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "show <name>",
		Short:             "Show a context's per-repo detail",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: contextNameCompletionFunc(),
		RunE: func(cmd *cobra.Command, args []string) error {
			w := cmd.OutOrStdout()
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			entry, ok := config.GetContext(cfg, args[0])
			if !ok {
				return ctxUnknownError(cfg, args[0])
			}
			jsonFlag, _ := cmd.Flags().GetBool("json")
			if jsonFlag {
				return output.RenderJSON(w, struct {
					Name    string               `json:"name"`
					SavedAt time.Time            `json:"saved_at"`
					Repos   []config.ContextRepo `json:"repos"`
				}{args[0], entry.SavedAt, entry.Repos})
			}

			output.Info(w, fmt.Sprintf("context %q · saved %s", args[0], output.FormatTimeAgo(entry.SavedAt)))
			for _, cr := range entry.Repos {
				detail := cr.Branch
				if cr.Detached {
					detail += " (detached)"
				}
				if cr.Stashed {
					detail += " · stashed"
				}
				_, _ = fmt.Fprintf(w, "  %s  %s\n", cr.Name, output.Dim(detail))
			}
			return nil
		},
	}
}

func newCtxDropCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:               "drop <name>",
		Short:             "Forget a saved context (stashes stay in their repos)",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: contextNameCompletionFunc(),
		RunE: func(cmd *cobra.Command, args []string) error {
			w := cmd.OutOrStdout()
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			entry, ok := config.GetContext(cfg, args[0])
			if !ok {
				return ctxUnknownError(cfg, args[0])
			}

			force, _ := cmd.Flags().GetBool("force")
			if !force {
				prompt := fmt.Sprintf("drop context %q? [y/N] ", args[0])
				if n := ctxStashedCount(entry); n > 0 {
					prompt = fmt.Sprintf("drop context %q? its %d %s stay in their repos' stash lists [y/N] ",
						args[0], n, output.Plural(n, "stash"))
				}
				_, _ = fmt.Fprint(cmd.ErrOrStderr(), prompt)

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

			config.DeleteContext(cfg, args[0])
			if err := config.Save(cfg); err != nil {
				return fmt.Errorf("saving config: %w", err)
			}
			output.Info(w, fmt.Sprintf("dropped context %q", args[0]))
			return nil
		},
	}
	cmd.Flags().Bool("force", false, "skip confirmation prompt")
	return cmd
}

// ctxStashedCount returns how many repos in the context carry a stash.
func ctxStashedCount(e *config.ContextEntry) int {
	var n int
	for i := range e.Repos {
		if e.Repos[i].Stashed {
			n++
		}
	}
	return n
}

// ctxUnknownError builds the unknown-context error, listing what exists.
func ctxUnknownError(cfg *config.Config, name string) error {
	names := config.ContextNames(cfg)
	if len(names) == 0 {
		return fmt.Errorf("no context named %q — none saved yet", name)
	}
	sort.Strings(names)
	return fmt.Errorf("no context named %q — saved contexts: %s", name, strings.Join(names, ", "))
}

// ctxFailureError returns the command-level error when any repo failed.
func ctxFailureError(results []ctxRepoResult, verb string) error {
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

type ctxJSON struct {
	Name      string `json:"name"`
	Branch    string `json:"branch,omitempty"`
	Stashed   bool   `json:"stashed"`
	Status    string `json:"status"`
	Error     string `json:"error,omitempty"`
	ErrorCode string `json:"error_code,omitempty"`
}

func renderCtxJSON(w io.Writer, results []ctxRepoResult) error {
	entries := make([]ctxJSON, len(results))
	for i := range results {
		r := &results[i]
		entries[i] = ctxJSON{Name: r.name, Branch: r.branch, Stashed: r.stashed, Status: "ok"}
		if !r.success {
			entries[i].Status = "failed"
			entries[i].Error = r.message
			entries[i].ErrorCode = r.errorCode
		}
	}
	return output.RenderJSON(w, entries)
}
