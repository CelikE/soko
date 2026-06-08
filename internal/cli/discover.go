package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/CelikE/soko/internal/config"
	"github.com/CelikE/soko/internal/git"
	"github.com/CelikE/soko/internal/output"
)

// newDiscoverCmd creates the cobra command for soko discover.
func newDiscoverCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "discover",
		Short: "Automatically register repos as you navigate into them",
		Long: `Auto-discovery registers a git repo the first time you cd into it, so you
never have to run soko scan or soko init by hand.

It is driven by the shell integration hook (see: soko shell-init). With
discovery enabled, the hook fires on each directory change; when you enter a
git repo that is not yet registered, soko adds it to the registry.

  soko discover on                 # turn it on
  soko discover on --root ~/work   # only discover under ~/work
  soko discover status             # show current settings
  soko discover off                # turn it off

Enabling or disabling discovery changes what soko shell-init emits, so open a
new shell or re-run eval "$(soko shell-init)" afterwards to activate it.`,
		// Running "soko discover" with no subcommand shows status.
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDiscoverStatus(cmd)
		},
	}

	cmd.AddCommand(newDiscoverOnCmd())
	cmd.AddCommand(newDiscoverOffCmd())
	cmd.AddCommand(newDiscoverStatusCmd())
	cmd.AddCommand(newDiscoverHookCmd())

	return cmd
}

func newDiscoverOnCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "on",
		Short: "Enable automatic repo discovery",
		Long: `Enable auto-discovery. Optionally scope it to one or more root directories
and apply tags to every discovered repo.

  soko discover on
  soko discover on --root ~/work --root ~/oss
  soko discover on --tag discovered
  soko discover on --ignore '*-scratch'`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			w := cmd.OutOrStdout()

			roots, _ := cmd.Flags().GetStringSlice("root")
			tags, _ := cmd.Flags().GetStringSlice("tag")
			ignore, _ := cmd.Flags().GetStringSlice("ignore")

			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			// Validate roots up front so a typo fails loudly instead of silently
			// matching nothing at hook time.
			for _, r := range roots {
				info, statErr := os.Stat(r)
				if statErr != nil {
					return fmt.Errorf("root %s: %w", r, statErr)
				}
				if !info.IsDir() {
					return fmt.Errorf("root %s: not a directory", r)
				}
			}

			d := cfg.EnsureDiscover()
			d.Enabled = true
			for _, r := range roots {
				d.Roots = appendUnique(d.Roots, normalizeRoot(r))
			}
			for _, t := range tags {
				d.Tags = appendUnique(d.Tags, t)
			}
			for _, ig := range ignore {
				d.Ignore = appendUnique(d.Ignore, ig)
			}

			if err := config.Save(cfg); err != nil {
				return fmt.Errorf("saving config: %w", err)
			}

			output.Confirm(w, "auto-discovery enabled")
			renderDiscoverSettings(w, d)
			_, _ = fmt.Fprintln(w)
			output.Info(w, "open a new shell or re-run the shell hook to activate:")
			output.Info(w, "  "+shellInitActivateHint())
			return nil
		},
	}

	cmd.Flags().StringSlice("root", nil, "only discover repos under these directories (repeatable)")
	cmd.Flags().StringSlice("tag", nil, "tags to apply to discovered repos (repeatable)")
	_ = cmd.RegisterFlagCompletionFunc("tag", tagCompletionFunc())
	cmd.Flags().StringSlice("ignore", nil, "glob patterns of paths to skip (repeatable)")

	return cmd
}

func newDiscoverOffCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "off",
		Short: "Disable automatic repo discovery",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			w := cmd.OutOrStdout()

			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			if !cfg.DiscoverEnabled() {
				output.Info(w, "auto-discovery is already off")
				return nil
			}

			cfg.Discover.Enabled = false
			if err := config.Save(cfg); err != nil {
				return fmt.Errorf("saving config: %w", err)
			}

			output.Confirm(w, "auto-discovery disabled")
			_, _ = fmt.Fprintln(w)
			output.Info(w, "open a new shell or re-run the shell hook to stop discovering:")
			output.Info(w, "  "+shellInitActivateHint())
			return nil
		},
	}
}

func newDiscoverStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show auto-discovery settings",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDiscoverStatus(cmd)
		},
	}
}

// runDiscoverStatus prints the current discovery configuration.
func runDiscoverStatus(cmd *cobra.Command) error {
	w := cmd.OutOrStdout()

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	if jsonFlag, _ := cmd.Flags().GetBool("json"); jsonFlag {
		out := discoverStatusJSON{Enabled: cfg.DiscoverEnabled()}
		if cfg.Discover != nil {
			out.Roots = cfg.Discover.Roots
			out.Tags = cfg.Discover.Tags
			out.Ignore = cfg.Discover.Ignore
		}
		return output.RenderJSON(w, out)
	}

	if !cfg.DiscoverEnabled() {
		output.Info(w, "auto-discovery: off")
		output.Info(w, "enable with: soko discover on")
		return nil
	}

	output.Confirm(w, "auto-discovery: on")
	renderDiscoverSettings(w, cfg.Discover)
	return nil
}

// discoverStatusJSON is the machine-readable form of the discovery settings.
type discoverStatusJSON struct {
	Enabled bool     `json:"enabled"`
	Roots   []string `json:"roots,omitempty"`
	Tags    []string `json:"tags,omitempty"`
	Ignore  []string `json:"ignore,omitempty"`
}

// renderDiscoverSettings prints the roots, tags, and ignore patterns.
func renderDiscoverSettings(w io.Writer, d *config.DiscoverConfig) {
	if len(d.Roots) == 0 {
		output.Info(w, "roots: (anywhere)")
	} else {
		output.Info(w, "roots: "+strings.Join(shortenAll(d.Roots), ", "))
	}
	if len(d.Tags) > 0 {
		output.Info(w, "tags: "+strings.Join(d.Tags, ", "))
	}
	if len(d.Ignore) > 0 {
		output.Info(w, "ignore: "+strings.Join(d.Ignore, ", "))
	}
}

// newDiscoverHookCmd is the hidden entrypoint invoked by the shell integration
// on each directory change. It registers the current repo if eligible and is
// designed to never fail the shell prompt: it always exits 0.
func newDiscoverHookCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "hook [dir]",
		Short:  "Internal: discover the repo at the current directory",
		Hidden: true,
		Args:   cobra.MaximumNArgs(1),
		// Never surface usage or errors — this runs inside the shell prompt.
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			var dir string
			if len(args) == 1 {
				dir = args[0]
			}
			discoverHook(cmd, dir)
			return nil
		},
	}
}

// discoverHook performs the discovery for dir (or the working directory when
// dir is empty). It is intentionally silent on every path except a successful
// registration, which it reports to stderr. It never returns an error.
func discoverHook(cmd *cobra.Command, dir string) {
	ctx := cmd.Context()
	stderr := cmd.ErrOrStderr()

	cfg, err := config.Load()
	if err != nil || !cfg.DiscoverEnabled() {
		return
	}

	if dir == "" {
		dir, err = os.Getwd()
		if err != nil || dir == "" {
			return
		}
	}

	// Resolve the working-tree root. Toplevel fails for non-repos, so this also
	// covers the "not a git repo" case without a separate probe.
	top, err := git.Toplevel(ctx, dir)
	if err != nil {
		return
	}
	if resolved, evalErr := filepath.EvalSymlinks(top); evalErr == nil {
		top = resolved
	}

	// Hot path: re-entering an already-registered repo. Return before any
	// further git calls (worktree/submodule resolution).
	if _, findErr := config.FindRepoByPath(cfg, top); findErr == nil {
		return
	}

	// Mirror soko init: inside a linked worktree, register the main repo rather
	// than a worktree-path entry that is not marked as such. If the main repo
	// cannot be resolved, skip rather than register a malformed entry.
	if git.IsWorktree(ctx, top) {
		main, mainErr := git.MainRepoPath(ctx, top)
		if mainErr != nil {
			return
		}
		if resolved, evalErr := filepath.EvalSymlinks(main); evalErr == nil {
			main = resolved
		}
		top = main
		if _, findErr := config.FindRepoByPath(cfg, top); findErr == nil {
			return
		}
	}

	// Skip submodules: their .git file trips the shell gate, but they belong to
	// a superproject and should not be registered as standalone repos.
	if git.Superproject(ctx, top) != "" {
		return
	}

	// Never auto-register the home directory itself (e.g. a dotfiles repo) — it
	// is almost always unintended and pollutes the registry.
	if home, homeErr := os.UserHomeDir(); homeErr == nil {
		if resolved, evalErr := filepath.EvalSymlinks(home); evalErr == nil {
			home = resolved
		}
		if filepath.Clean(top) == filepath.Clean(home) {
			return
		}
	}

	if !config.ShouldDiscover(cfg, top) {
		return
	}

	name := git.RepoName(ctx, top)
	entry := config.RepoEntry{Name: name, Path: top, Tags: cfg.Discover.Tags}

	// Re-load immediately before writing and add to that fresh copy. Each hook
	// is a separate process with no shared lock, so re-reading here shrinks the
	// load→save window to the atomic rename and avoids clobbering a repo that
	// another shell discovered in the meantime.
	fresh, err := config.Load()
	if err != nil {
		return
	}
	if _, findErr := config.FindRepoByPath(fresh, top); findErr == nil {
		return
	}
	if _, addErr := config.AddRepo(fresh, &entry); addErr != nil {
		return
	}
	if saveErr := config.Save(fresh); saveErr != nil {
		return
	}

	output.Confirm(stderr, fmt.Sprintf("discovered %s (%s)", name, shortenHome(top)))
}

// normalizeRoot resolves a root directory to an absolute, symlink-free path so
// it matches the paths produced during discovery.
func normalizeRoot(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return filepath.Clean(p)
	}
	if resolved, evalErr := filepath.EvalSymlinks(abs); evalErr == nil {
		return resolved
	}
	return abs
}

// appendUnique appends v to s unless it is already present.
func appendUnique(s []string, v string) []string {
	for _, existing := range s {
		if existing == v {
			return s
		}
	}
	return append(s, v)
}

// shortenAll applies shortenHome to every path in the slice.
func shortenAll(paths []string) []string {
	out := make([]string, len(paths))
	for i, p := range paths {
		out[i] = shortenHome(p)
	}
	return out
}
