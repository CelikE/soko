package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"

	"github.com/spf13/cobra"

	"github.com/CelikE/soko/internal/config"
)

// newTagCmd creates the cobra command for soko tag with subcommands.
func newTagCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tag",
		Short: "Manage repo tags",
	}

	cmd.AddCommand(newTagAddCmd())
	cmd.AddCommand(newTagRemoveCmd())
	cmd.AddCommand(newTagListCmd())

	return cmd
}

func newTagAddCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "add <repo> <tag>",
		Short:             "Add a tag to a repo",
		Args:              cobra.ExactArgs(2),
		ValidArgsFunction: repoNameCompletionFunc(),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			cfg, err = config.AddTag(cfg, args[0], args[1])
			if err != nil {
				return fmt.Errorf("adding tag: %w", err)
			}

			if err := config.Save(cfg); err != nil {
				return fmt.Errorf("saving config: %w", err)
			}

			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "tagged %s with %q\n", args[0], args[1])
			return nil
		},
	}
}

func newTagRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "remove <repo> <tag>",
		Short:             "Remove a tag from a repo",
		Args:              cobra.ExactArgs(2),
		ValidArgsFunction: repoNameCompletionFunc(),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			cfg, err = config.RemoveTag(cfg, args[0], args[1])
			if err != nil {
				return fmt.Errorf("removing tag: %w", err)
			}

			if err := config.Save(cfg); err != nil {
				return fmt.Errorf("saving config: %w", err)
			}

			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "removed tag %q from %s\n", args[1], args[0])
			return nil
		},
	}
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
				_, _ = fmt.Fprintln(w, "no tags in use")
				return nil
			}

			jsonFlag, _ := cmd.Flags().GetBool("json")
			if jsonFlag {
				return renderTagListJSON(w, cfg, tags)
			}

			counts := config.TagCount(cfg)
			for _, tag := range tags {
				_, _ = fmt.Fprintf(w, "  %s (%d repos)\n", tag, counts[tag])
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

	// Sort for consistent output.
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
