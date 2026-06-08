package cli

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/CelikE/soko/internal/config"
	"github.com/CelikE/soko/internal/output"
)

// newConfigCmd creates the cobra command for soko config.
func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "View and manage soko configuration",
	}

	cmd.AddCommand(newConfigPathCmd())
	cmd.AddCommand(newConfigEditCmd())
	cmd.AddCommand(newConfigSetCmd())
	cmd.AddCommand(newConfigGetCmd())
	cmd.AddCommand(newConfigListCmd())

	return cmd
}

func newConfigPathCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "path",
		Short: "Print the config file path",
		RunE: func(cmd *cobra.Command, _ []string) error {
			path, err := config.Path()
			if err != nil {
				return fmt.Errorf("resolving config path: %w", err)
			}
			if jsonFlag, _ := cmd.Flags().GetBool("json"); jsonFlag {
				return output.RenderJSON(cmd.OutOrStdout(), configPathJSON{Path: path})
			}
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), path)
			return nil
		},
	}
}

func newConfigEditCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "edit",
		Short: "Open the config file in $EDITOR",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()

			path, err := config.Path()
			if err != nil {
				return fmt.Errorf("resolving config path: %w", err)
			}

			editor := os.Getenv("EDITOR")
			if editor == "" {
				if runtime.GOOS == "windows" {
					editor = "notepad"
				} else {
					editor = "vi"
				}
			}

			c := exec.CommandContext(ctx, editor, path)
			c.Stdin = os.Stdin
			c.Stdout = os.Stdout
			c.Stderr = os.Stderr

			if err := c.Run(); err != nil {
				return fmt.Errorf("opening editor: %w", err)
			}

			output.Confirm(cmd.ErrOrStderr(), "config saved")
			return nil
		},
	}
}

func newConfigSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a config value",
		Long: `Set a configuration value. Available keys:
  git_path    Path to the git binary (default: git from PATH)`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			// Capture the prior value before mutating so JSON consumers get a
			// before/after they can diff.
			previous, _ := config.Get(cfg, args[0])

			if err := config.Set(cfg, args[0], args[1]); err != nil {
				return err
			}

			if err := config.Save(cfg); err != nil {
				return fmt.Errorf("saving config: %w", err)
			}

			if jsonFlag, _ := cmd.Flags().GetBool("json"); jsonFlag {
				// Re-read so the value matches config get's default-substitution.
				value, _ := config.Get(cfg, args[0])
				return output.RenderJSON(cmd.OutOrStdout(), configSetJSON{
					Key: args[0], Value: value, Previous: previous,
				})
			}

			output.Confirm(cmd.OutOrStdout(), fmt.Sprintf("%s = %s", args[0], args[1]))
			return nil
		},
	}
}

func newConfigGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <key>",
		Short: "Get a config value",
		Long: `Get a configuration value. Available keys:
  git_path    Path to the git binary (default: git from PATH)`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			val, err := config.Get(cfg, args[0])
			if err != nil {
				return err
			}

			if jsonFlag, _ := cmd.Flags().GetBool("json"); jsonFlag {
				return output.RenderJSON(cmd.OutOrStdout(), configGetJSON{Key: args[0], Value: val})
			}

			_, _ = fmt.Fprintln(cmd.OutOrStdout(), val)
			return nil
		},
	}
}

func newConfigListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "Show the effective config",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			w := cmd.OutOrStdout()

			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			// git_path via config.Get keeps the annotated default ("git
			// (default)") consistent with what config get reports.
			gitPath, _ := config.Get(cfg, "git_path")

			if jsonFlag, _ := cmd.Flags().GetBool("json"); jsonFlag {
				out := configListJSON{
					GitPath:   gitPath,
					Aliases:   cfg.Aliases,
					RepoCount: config.RepoCount(cfg),
				}
				if cfg.Discover != nil {
					out.Discover = &discoverStatusJSON{
						Enabled: cfg.Discover.Enabled,
						Roots:   cfg.Discover.Roots,
						Tags:    cfg.Discover.Tags,
						Ignore:  cfg.Discover.Ignore,
					}
				}
				return output.RenderJSON(w, out)
			}

			renderConfigList(w, cfg, gitPath)
			return nil
		},
	}
}

// renderConfigList prints the effective config as an aligned key/value block.
func renderConfigList(w io.Writer, cfg *config.Config, gitPath string) {
	label := func(s string) string { return output.Dim(fmt.Sprintf("%-10s", s)) }

	_, _ = fmt.Fprintf(w, "  %s %s\n", label("git_path"), gitPath)
	_, _ = fmt.Fprintf(w, "  %s %d %s\n", label("aliases"),
		len(cfg.Aliases), output.Plural(len(cfg.Aliases), "alias"))
	_, _ = fmt.Fprintf(w, "  %s %s\n", label("discover"), discoverSummary(cfg))
	_, _ = fmt.Fprintf(w, "  %s %d %s\n", label("repos"),
		config.RepoCount(cfg), output.Plural(config.RepoCount(cfg), "repo"))
}

// discoverSummary renders the discover line for config list: "off", or
// "on  ·  roots: …  ·  tags: …" mirroring soko discover status.
func discoverSummary(cfg *config.Config) string {
	if !cfg.DiscoverEnabled() {
		return "off"
	}
	parts := []string{"on"}
	d := cfg.Discover
	if len(d.Roots) == 0 {
		parts = append(parts, "roots: (anywhere)")
	} else {
		parts = append(parts, "roots: "+strings.Join(shortenAll(d.Roots), ", "))
	}
	if len(d.Tags) > 0 {
		parts = append(parts, "tags: "+strings.Join(d.Tags, ", "))
	}
	return strings.Join(parts, "  ·  ")
}

type configPathJSON struct {
	Path string `json:"path"`
}

type configGetJSON struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type configSetJSON struct {
	Key      string `json:"key"`
	Value    string `json:"value"`
	Previous string `json:"previous"`
}

type configListJSON struct {
	GitPath   string              `json:"git_path"`
	Aliases   map[string]string   `json:"aliases,omitempty"`
	Discover  *discoverStatusJSON `json:"discover,omitempty"`
	RepoCount int                 `json:"repo_count"`
}
