package cli

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"

	"github.com/CelikE/soko/internal/config"
	"github.com/CelikE/soko/internal/git"
	"github.com/CelikE/soko/internal/output"
)

// branchState is where a branch stands in one repo, from the lookup's point of
// view: checked out, present locally, only on a remote, or absent.
type branchState int

const (
	branchMissing branchState = iota
	branchRemoteOnly
	branchLocal
	branchCurrent
)

// newBranchCmd creates the cobra command for soko branch and subcommands.
func newBranchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "branch [name] [repos...]",
		Short: "See and drive branches across repos",
		Long: `See which branch every repo is on, find which repos have a given branch,
and switch a set of repos to the same branch.

Without arguments it lists the current branch per repo. With a name it shows
where that branch exists — checked out, local, remote only, or missing — the
map you need before driving polyrepo feature work with soko branch switch.`,
		Example: `  soko branch                         # current branch per repo
  soko branch feat/sso                # which repos have feat/sso
  soko branch feat/sso api front      # only these repos
  soko branch switch feat/sso         # check it out where it exists
  soko branch switch feat/sso -b      # create it where missing
  soko branch stale                   # unmerged branches untouched > 90 days`,
		Args:              cobra.ArbitraryArgs,
		ValidArgsFunction: branchArgsCompletionFunc(),
		RunE:              runBranchOverview,
	}
	cmd.Flags().StringSlice("tag", nil, "filter by tag (can be repeated, combines with OR)")
	_ = cmd.RegisterFlagCompletionFunc("tag", tagCompletionFunc())

	cmd.AddCommand(newBranchSwitchCmd())
	cmd.AddCommand(newBranchStaleCmd())

	return cmd
}

// branchArgsCompletionFunc completes nothing for the first argument (a branch
// name) and registered repo names for the rest.
func branchArgsCompletionFunc() cobra.CompletionFunc {
	return func(c *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
		if len(args) == 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return repoNameCompletionFunc()(c, args, toComplete)
	}
}

type branchRow struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	Branch    string `json:"branch"`
	Detached  bool   `json:"detached,omitempty"`
	State     string `json:"state,omitempty"`
	Error     string `json:"error,omitempty"`
	ErrorCode string `json:"error_code,omitempty"`
	state     branchState
}

func runBranchOverview(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	w := cmd.OutOrStdout()

	// The first positional argument is a branch name, the rest narrow the
	// repo set — `soko branch feat/sso api front`.
	var lookup string
	if len(args) > 0 {
		lookup = args[0]
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

	rows := collectBranchRows(ctx, repos, lookup)

	jsonFlag, _ := cmd.Flags().GetBool("json")
	if jsonFlag {
		return output.RenderJSON(w, rows)
	}

	renderBranchTable(w, rows, lookup)
	return nil
}

// collectBranchRows resolves the current branch (and, when lookup is set, the
// lookup branch's state) for every repo in parallel, preserving config order.
func collectBranchRows(ctx context.Context, repos []config.RepoEntry, lookup string) []branchRow {
	rows := make([]branchRow, len(repos))

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(maxConcurrency)
	for i, repo := range repos {
		g.Go(func() error {
			row := branchRow{Name: repo.Name, Path: repo.Path}

			if !pathExists(repo.Path) {
				row.Branch = "—"
				row.Error = "path not found"
				row.ErrorCode = codePathMissing
				rows[i] = row
				return nil
			}

			branch, detached, err := git.CurrentBranch(gctx, repo.Path)
			if err != nil {
				row.Branch = "—"
				row.Error = err.Error()
				row.ErrorCode = gitErrorCode(err)
				rows[i] = row
				return nil
			}
			row.Branch = branch
			row.Detached = detached

			if lookup != "" {
				row.state = lookupBranchState(gctx, repo.Path, lookup, branch, detached)
				row.State = branchStateName(row.state)
			}

			rows[i] = row
			return nil
		})
	}
	_ = g.Wait()

	return rows
}

// lookupBranchState classifies where a branch stands in one repo.
func lookupBranchState(ctx context.Context, dir, name, current string, detached bool) branchState {
	switch {
	case !detached && current == name:
		return branchCurrent
	case localBranchExists(ctx, dir, name):
		return branchLocal
	case remoteBranchExists(ctx, dir, name):
		return branchRemoteOnly
	default:
		return branchMissing
	}
}

// branchStateName maps a branchState to its stable JSON value.
func branchStateName(s branchState) string {
	switch s {
	case branchCurrent:
		return "current"
	case branchLocal:
		return "local"
	case branchRemoteOnly:
		return "remote"
	default:
		return "missing"
	}
}

// localBranchExists reports whether refs/heads/<name> exists in the repo.
func localBranchExists(ctx context.Context, dir, name string) bool {
	_, err := git.Run(ctx, dir, "rev-parse", "--verify", "--quiet", "refs/heads/"+name)
	return err == nil
}

// remoteBranchExists reports whether any remote has a tracking ref for the
// branch (refs/remotes/<remote>/<name>).
func remoteBranchExists(ctx context.Context, dir, name string) bool {
	return len(remoteRefsFor(ctx, dir, name)) > 0
}

// remoteRefsFor returns the short remote tracking refs for a branch name
// (e.g. ["origin/feat/sso"]), one per remote that has it.
func remoteRefsFor(ctx context.Context, dir, name string) []string {
	out, err := git.Run(ctx, dir, "for-each-ref", "--format=%(refname:short)", "refs/remotes/*/"+name)
	if err != nil || out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}

// renderBranchTable prints the overview table, with the lookup column when a
// branch name was asked about.
func renderBranchTable(w io.Writer, rows []branchRow, lookup string) {
	cName, cBranch := len("REPO"), len("BRANCH")
	for i := range rows {
		if len(rows[i].Name) > cName {
			cName = len(rows[i].Name)
		}
		if len(branchDisplay(&rows[i])) > cBranch {
			cBranch = len(branchDisplay(&rows[i]))
		}
	}
	cName += 2
	cBranch += 2

	header := fmt.Sprintf("  %-*s %-*s", cName, "REPO", cBranch, "BRANCH")
	if lookup != "" {
		header += " " + lookup + "?"
	}
	_, _ = fmt.Fprintln(w, output.Dim(header))
	_, _ = fmt.Fprintln(w, output.Dim("  "+strings.Repeat("─", len(header)-2)))

	var current, local, remote, missing int
	for i := range rows {
		r := &rows[i]
		line := fmt.Sprintf("  %-*s %-*s", cName, r.Name, cBranch, branchDisplay(r))
		switch {
		case r.Error != "":
			line += " " + output.Red(r.Error)
		case lookup != "":
			switch r.state {
			case branchCurrent:
				line += " " + output.Green("✓ current")
				current++
			case branchLocal:
				line += " " + output.Green("✓ local")
				local++
			case branchRemoteOnly:
				line += " " + output.Yellow("○ remote only")
				remote++
			default:
				line += " " + output.Dim("— missing")
				missing++
			}
		}
		_, _ = fmt.Fprintln(w, line)
	}

	if lookup == "" || output.Quiet() {
		return
	}
	_, _ = fmt.Fprintf(w, "\n  %s\n", output.Dim(fmt.Sprintf(
		"%d %s · %d current · %d local · %d remote only · %d missing",
		len(rows), output.Plural(len(rows), "repo"), current, local, remote, missing)))
}

// branchDisplay is the branch cell for one row: the branch name, or a
// detached-HEAD marker since a raw SHA is noise at a glance.
func branchDisplay(r *branchRow) string {
	if r.Detached {
		return "(detached)"
	}
	return r.Branch
}

// switchStatus is the outcome of switching a single repo. The zero value is
// switchFailed so an unset result is treated as a failure, mirroring sync.
type switchStatus int

const (
	switchFailed switchStatus = iota
	switchAlready
	switchSwitched
	switchCreated
	switchDirty
	switchMissing
)

type switchResult struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	Branch    string `json:"branch"`
	Status    string `json:"status"`
	Error     string `json:"error,omitempty"`
	ErrorCode string `json:"error_code,omitempty"`
	status    switchStatus
	message   string
}

func newBranchSwitchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "switch <name> [repos...]",
		Short: "Check out a branch in every repo that has it",
		Long: `Check out the branch in every selected repo where it exists — local
branches are checked out directly, remote-only branches get a local tracking
branch. Repos without the branch are reported and skipped unless -b creates
the branch there from the repo's default branch.

A dirty repo is refused and left untouched (the others continue) — commit,
stash, or soko ctx save first.`,
		Example: `  soko branch switch feat/sso         # check out where it exists
  soko branch switch feat/sso -b      # create from default branch where missing
  soko branch switch feat/sso --tag backend`,
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: branchArgsCompletionFunc(),
		RunE:              runBranchSwitch,
	}
	cmd.Flags().BoolP("create", "b", false, "create the branch from the default branch where missing")
	cmd.Flags().StringSlice("tag", nil, "filter by tag (can be repeated, combines with OR)")
	_ = cmd.RegisterFlagCompletionFunc("tag", tagCompletionFunc())
	return cmd
}

func runBranchSwitch(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	w := cmd.OutOrStdout()
	name := args[0]

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

	create, _ := cmd.Flags().GetBool("create")

	results := make([]switchResult, len(repos))
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(maxConcurrency)
	for i, repo := range repos {
		g.Go(func() error {
			results[i] = switchRepo(gctx, &repo, name, create)
			return nil
		})
	}
	_ = g.Wait()

	jsonFlag, _ := cmd.Flags().GetBool("json")
	if jsonFlag {
		if err := output.RenderJSON(w, results); err != nil {
			return err
		}
	} else {
		renderSwitchTable(w, results)
	}

	var failed int
	for i := range results {
		if results[i].status == switchFailed {
			failed++
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d %s failed to switch", failed, output.Plural(failed, "repo"))
	}
	return nil
}

// switchRepo checks out the branch in one repo when that is safe: path
// present, clean working tree, branch available locally or on a remote — or
// created from the default branch with -b. Every other state is reported,
// never acted on.
func switchRepo(ctx context.Context, repo *config.RepoEntry, name string, create bool) switchResult {
	r := switchResult{Name: repo.Name, Path: repo.Path, Branch: name}

	fail := func(msg, code string) switchResult {
		r.Status = "failed"
		r.message = msg
		r.Error = msg
		r.ErrorCode = code
		return r
	}

	if !pathExists(repo.Path) {
		return fail("path not found", codePathMissing)
	}

	current, detached, err := git.CurrentBranch(ctx, repo.Path)
	if err != nil {
		return fail(err.Error(), gitErrorCode(err))
	}
	if !detached && current == name {
		r.status = switchAlready
		r.Status = "already"
		r.message = "already on branch"
		return r
	}

	// Never overwrite work in progress: a dirty repo is refused and the
	// others continue — soko ctx save is the escape hatch.
	statusOut, err := git.Run(ctx, repo.Path, "status", "--porcelain")
	if err != nil {
		return fail(err.Error(), gitErrorCode(err))
	}
	if statusOut != "" {
		r.status = switchDirty
		r.Status = "dirty"
		r.message = "dirty — commit, stash, or ctx save first"
		return r
	}

	state := lookupBranchState(ctx, repo.Path, name, current, detached)
	if state == branchMissing && !create {
		r.status = switchMissing
		r.Status = "missing"
		r.message = "branch missing — pass -b to create"
		return r
	}

	switch state {
	case branchMissing:
		def := defaultBranch(ctx, repo.Path)
		if def == "" {
			return fail("could not detect default branch", codeGitFailure)
		}
		if _, err := git.Run(ctx, repo.Path, "checkout", "-b", name, def); err != nil {
			return fail(err.Error(), gitErrorCode(err))
		}
		r.status = switchCreated
		r.Status = "created"
		r.message = "created from default branch"

	case branchRemoteOnly:
		refs := remoteRefsFor(ctx, repo.Path, name)
		if len(refs) > 1 {
			return fail(fmt.Sprintf("branch exists on multiple remotes (%s) — check out manually",
				strings.Join(refs, ", ")), codeGitFailure)
		}
		// Prefer a tracking branch; a ref whose remote is no longer
		// configured cannot track, so fall back to a plain start point.
		if _, err := git.Run(ctx, repo.Path, "checkout", "-b", name, "--track", refs[0]); err != nil {
			if _, err := git.Run(ctx, repo.Path, "checkout", "-b", name, refs[0]); err != nil {
				return fail(err.Error(), gitErrorCode(err))
			}
		}
		r.status = switchSwitched
		r.Status = "switched"
		r.message = "switched (tracking remote)"

	default:
		if _, err := git.Run(ctx, repo.Path, "checkout", name); err != nil {
			return fail(err.Error(), gitErrorCode(err))
		}
		r.status = switchSwitched
		r.Status = "switched"
		r.message = "switched"
	}
	return r
}

// renderSwitchTable prints per-repo switch outcomes and a summary line.
func renderSwitchTable(w io.Writer, results []switchResult) {
	cName := len("REPO")
	for i := range results {
		if len(results[i].Name) > cName {
			cName = len(results[i].Name)
		}
	}
	cName += 2

	header := fmt.Sprintf("  %-*s %s", cName, "REPO", "RESULT")
	_, _ = fmt.Fprintln(w, output.Dim(header))
	_, _ = fmt.Fprintln(w, output.Dim("  "+strings.Repeat("─", len(header)-2)))

	var switched, created, already, dirty, missing, failed int
	for i := range results {
		r := &results[i]
		// The table cell only fits one line, so condense git's multi-line
		// failure output to its most informative line.
		var cell string
		switch r.status {
		case switchSwitched:
			cell = output.Green("✓ " + r.message)
			switched++
		case switchCreated:
			cell = output.Green("✓ " + r.message)
			created++
		case switchAlready:
			cell = output.Dim("· " + r.message)
			already++
		case switchDirty:
			cell = output.Yellow("⚠ " + r.message)
			dirty++
		case switchMissing:
			cell = output.Dim("— " + r.message)
			missing++
		default:
			cell = output.Red("✗ " + gitErrorReason(r.message))
			failed++
		}
		_, _ = fmt.Fprintf(w, "  %-*s %s\n", cName, r.Name, cell)
	}

	if output.Quiet() {
		return
	}
	parts := []string{fmt.Sprintf("%d %s", len(results), output.Plural(len(results), "repo"))}
	for _, p := range []struct {
		n     int
		label string
	}{
		{switched, "switched"}, {created, "created"}, {already, "already"},
		{dirty, "dirty"}, {missing, "missing"}, {failed, "failed"},
	} {
		if p.n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", p.n, p.label))
		}
	}
	_, _ = fmt.Fprintf(w, "\n  %s\n", output.Dim(strings.Join(parts, " · ")))
}

type staleBranch struct {
	Branch string `json:"branch"`
	Days   int    `json:"days"`
}

type staleResult struct {
	Name      string        `json:"name"`
	Path      string        `json:"path"`
	Branches  []staleBranch `json:"branches"`
	Error     string        `json:"error,omitempty"`
	ErrorCode string        `json:"error_code,omitempty"`
}

func newBranchStaleCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stale [repos...]",
		Short: "List unmerged branches untouched for too long",
		Long: `List local branches that are not merged into the default branch and have
had no commit for longer than the threshold (90 days by default). These are
the branches soko clean cannot touch — work that was started and abandoned —
surfaced per repo so you can rebase, merge, or delete them deliberately.`,
		Example: `  soko branch stale                   # unmerged + untouched > 90 days
  soko branch stale --days 30         # tighter threshold
  soko branch stale --tag backend     # only backend repos`,
		Args:              cobra.ArbitraryArgs,
		ValidArgsFunction: repoNameCompletionFunc(),
		RunE:              runBranchStale,
	}
	cmd.Flags().Int("days", 90, "report branches with no commit for this many days")
	cmd.Flags().StringSlice("tag", nil, "filter by tag (can be repeated, combines with OR)")
	_ = cmd.RegisterFlagCompletionFunc("tag", tagCompletionFunc())
	return cmd
}

func runBranchStale(cmd *cobra.Command, args []string) error {
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

	days, _ := cmd.Flags().GetInt("days")
	if days < 0 {
		return fmt.Errorf("--days must be zero or positive")
	}

	results := make([]staleResult, len(repos))
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(maxConcurrency)
	for i, repo := range repos {
		g.Go(func() error {
			results[i] = staleRepo(gctx, &repo, days)
			return nil
		})
	}
	_ = g.Wait()

	// Only repos with stale branches (or a real failure) are worth a row.
	var withStale []staleResult
	for i := range results {
		if len(results[i].Branches) > 0 || results[i].Error != "" {
			withStale = append(withStale, results[i])
		}
	}

	jsonFlag, _ := cmd.Flags().GetBool("json")
	if len(withStale) == 0 {
		if jsonFlag {
			_, _ = fmt.Fprintln(w, "[]")
			return nil
		}
		output.Info(w, fmt.Sprintf("no unmerged branches older than %d days", days))
		return nil
	}

	if jsonFlag {
		return output.RenderJSON(w, withStale)
	}

	renderStaleTable(w, withStale, days)
	return nil
}

// staleRepo finds the repo's local branches that are not merged into the
// default branch and whose last commit is older than the threshold.
func staleRepo(ctx context.Context, repo *config.RepoEntry, days int) staleResult {
	r := staleResult{Name: repo.Name, Path: repo.Path}

	if !pathExists(repo.Path) {
		r.Error = "path not found"
		r.ErrorCode = codePathMissing
		return r
	}

	def := defaultBranch(ctx, repo.Path)
	if def == "" {
		r.Error = "could not detect default branch"
		r.ErrorCode = codeGitFailure
		return r
	}

	unmergedOut, err := git.Run(ctx, repo.Path, "branch", "--no-merged", def, "--format=%(refname:short)")
	if err != nil {
		r.Error = "failed to list unmerged branches"
		r.ErrorCode = gitErrorCode(err)
		return r
	}
	unmerged := make(map[string]bool)
	for line := range strings.SplitSeq(unmergedOut, "\n") {
		if b := strings.TrimSpace(line); b != "" {
			unmerged[b] = true
		}
	}
	if len(unmerged) == 0 {
		return r
	}

	// Committer dates come from for-each-ref in one shot rather than a log
	// call per branch.
	refsOut, err := git.Run(ctx, repo.Path, "for-each-ref", "--format=%(refname:short)%00%(committerdate:unix)", "refs/heads")
	if err != nil {
		r.Error = "failed to list branches"
		r.ErrorCode = gitErrorCode(err)
		return r
	}
	now := time.Now().Unix()
	for line := range strings.SplitSeq(refsOut, "\n") {
		branch, stamp, ok := strings.Cut(line, "\x00")
		if !ok || !unmerged[branch] {
			continue
		}
		unix, err := strconv.ParseInt(stamp, 10, 64)
		if err != nil {
			continue
		}
		age := int((now - unix) / 86400)
		if age > days {
			r.Branches = append(r.Branches, staleBranch{Branch: branch, Days: age})
		}
	}

	return r
}

// renderStaleTable prints stale branches per repo with their age.
func renderStaleTable(w io.Writer, results []staleResult, days int) {
	cName, cBranch := len("REPO"), len("BRANCH")
	for i := range results {
		if len(results[i].Name) > cName {
			cName = len(results[i].Name)
		}
		for _, b := range results[i].Branches {
			if len(b.Branch) > cBranch {
				cBranch = len(b.Branch)
			}
		}
	}
	cName += 2
	cBranch += 2

	header := fmt.Sprintf("  %-*s %-*s %s", cName, "REPO", cBranch, "BRANCH", "AGE")
	_, _ = fmt.Fprintln(w, output.Dim(header))
	_, _ = fmt.Fprintln(w, output.Dim("  "+strings.Repeat("─", len(header)-2)))

	var total int
	for i := range results {
		r := &results[i]
		if r.Error != "" {
			_, _ = fmt.Fprintf(w, "  %-*s %s\n", cName, r.Name, output.Red(r.Error))
			continue
		}
		// The repo name shows once; further branches indent under it.
		name := r.Name
		for _, b := range r.Branches {
			_, _ = fmt.Fprintf(w, "  %-*s %-*s %s\n", cName, name, cBranch, b.Branch,
				output.Yellow(fmt.Sprintf("%dd", b.Days)))
			name = ""
			total++
		}
	}

	if output.Quiet() {
		return
	}
	_, _ = fmt.Fprintf(w, "\n  %s\n", output.Dim(fmt.Sprintf(
		"%d stale %s across %d %s (unmerged, no commit in %d days)",
		total, output.Plural(total, "branch"),
		len(results), output.Plural(len(results), "repo"), days)))
}
