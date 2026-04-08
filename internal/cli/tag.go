package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/spf13/cobra"

	"github.com/CelikE/soko/internal/config"
	"github.com/CelikE/soko/internal/output"
)

// newTagCmd creates the cobra command for soko tag.
//
// Usage:
//
//	soko tag backend go       # add tags to current repo (shorthand)
//	soko tag add backend      # add tag to current repo
//	soko tag add -r name go   # add tag to a specific repo
//	soko tag remove backend   # remove tag from current repo
//	soko tag list             # list all tags
func newTagCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tag [tags...]",
		Short: "Manage repo tags",
		Long: `Add tags to the current repo, or use subcommands for more control.

Running soko tag <tags...> in a registered repo adds the given tags.
Use subcommands add, remove, and list for explicit operations.`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}

			// Shorthand: soko tag backend go → add tags to current repo.
			repoName, err := resolveCurrentRepo()
			if err != nil {
				return err
			}

			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			for _, tag := range args {
				cfg, err = config.AddTag(cfg, repoName, tag)
				if err != nil {
					return fmt.Errorf("adding tag %q: %w", tag, err)
				}
			}

			if err := config.Save(cfg); err != nil {
				return fmt.Errorf("saving config: %w", err)
			}

			if len(args) == 1 {
				output.Confirm(cmd.OutOrStdout(), fmt.Sprintf("tagged %s with %q", repoName, args[0]))
			} else {
				output.Confirm(cmd.OutOrStdout(), fmt.Sprintf("tagged %s with %v", repoName, args))
			}
			return nil
		},
	}

	cmd.AddCommand(newTagAddCmd())
	cmd.AddCommand(newTagRemoveCmd())
	cmd.AddCommand(newTagListCmd())

	return cmd
}

// resolveCurrentRepo finds the repo name for the current working directory.
func resolveCurrentRepo() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("getting working directory: %w", err)
	}

	cfg, err := config.Load()
	if err != nil {
		return "", fmt.Errorf("loading config: %w", err)
	}

	entry, err := config.FindRepoByPath(cfg, dir)
	if err != nil {
		return "", fmt.Errorf("current directory is not a registered repo — run soko init first")
	}

	return entry.Name, nil
}

// resolveRepoName returns the repo name from the -r flag or the current directory.
func resolveRepoName(cmd *cobra.Command) (string, error) {
	repoFlag, _ := cmd.Flags().GetString("repo")
	if repoFlag != "" {
		return repoFlag, nil
	}
	return resolveCurrentRepo()
}

func newTagAddCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add <tags...>",
		Short: "Add tags to the current repo (or use -r to specify)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			repoName, err := resolveRepoName(cmd)
			if err != nil {
				return err
			}

			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			for _, tag := range args {
				cfg, err = config.AddTag(cfg, repoName, tag)
				if err != nil {
					return fmt.Errorf("adding tag %q: %w", tag, err)
				}
			}

			if err := config.Save(cfg); err != nil {
				return fmt.Errorf("saving config: %w", err)
			}

			if len(args) == 1 {
				output.Confirm(cmd.OutOrStdout(), fmt.Sprintf("tagged %s with %q", repoName, args[0]))
			} else {
				output.Confirm(cmd.OutOrStdout(), fmt.Sprintf("tagged %s with %v", repoName, args))
			}
			return nil
		},
	}

	cmd.Flags().StringP("repo", "r", "", "target repo name (defaults to current directory)")
	_ = cmd.RegisterFlagCompletionFunc("repo", repoNameCompletionFunc())

	return cmd
}

func newTagRemoveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remove <tags...>",
		Short: "Remove tags from the current repo (or use -r to specify)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			repoName, err := resolveRepoName(cmd)
			if err != nil {
				return err
			}

			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			for _, tag := range args {
				cfg, err = config.RemoveTag(cfg, repoName, tag)
				if err != nil {
					return fmt.Errorf("removing tag %q: %w", tag, err)
				}
			}

			if err := config.Save(cfg); err != nil {
				return fmt.Errorf("saving config: %w", err)
			}

			if len(args) == 1 {
				output.Confirm(cmd.OutOrStdout(), fmt.Sprintf("removed tag %q from %s", args[0], repoName))
			} else {
				output.Confirm(cmd.OutOrStdout(), fmt.Sprintf("removed tags %v from %s", args, repoName))
			}
			return nil
		},
	}

	cmd.Flags().StringP("repo", "r", "", "target repo name (defaults to current directory)")
	_ = cmd.RegisterFlagCompletionFunc("repo", repoNameCompletionFunc())

	return cmd
}

func newTagListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all tags in use",
		RunE: func(cmd *cobra.Command, _ []string) error {
			w := cmd.OutOrStdout()

			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			tags := config.ListTags(cfg)
			if len(tags) == 0 {
				output.Info(w, "no tags in use")
				return nil
			}

			jsonFlag, _ := cmd.Flags().GetBool("json")
			if jsonFlag {
				return renderTagListJSON(w, cfg, tags)
			}

			counts := config.TagCount(cfg)
			for _, tag := range tags {
				_, _ = fmt.Fprintf(w, "  %s (%d %s)\n", tag, counts[tag], output.Plural(counts[tag], "repo"))
			}

			return nil
		},
	}
}

type tagListJSON struct {
	Tag   string `json:"tag"`
	Count int    `json:"count"`
}

func renderTagListJSON(w io.Writer, cfg *config.Config, tags []string) error {
	counts := config.TagCount(cfg)
	entries := make([]tagListJSON, len(tags))

	sort.Strings(tags)
	for i, tag := range tags {
		entries[i] = tagListJSON{Tag: tag, Count: counts[tag]}
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(entries); err != nil {
		return fmt.Errorf("encoding json: %w", err)
	}
	return nil
}
