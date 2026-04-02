package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/CelikE/soko/internal/config"
	"github.com/CelikE/soko/internal/output"
)

// newListCmd creates the cobra command for soko list.
func newListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all registered repos",
		RunE: func(cmd *cobra.Command, _ []string) error {
			w := cmd.OutOrStdout()

			cfg, repos, err := loadReposWithTagFilter(cmd)
			if err != nil {
				return err
			}

			if len(repos) == 0 {
				if len(cfg.Repos) == 0 {
					output.Info(w, "no repos registered yet — cd into a repo and run: soko init")
				} else {
					output.Info(w, fmt.Sprintf("no repos match the tag filter (%d repos registered)", len(cfg.Repos)))
				}
				return nil
			}

			jsonFlag, _ := cmd.Flags().GetBool("json")
			groupFlag, _ := cmd.Flags().GetBool("group")

			if jsonFlag {
				return renderListJSON(w, repos)
			}

			if groupFlag {
				renderListTree(w, repos)
				return nil
			}

			renderListTable(w, repos)
			return nil
		},
	}

	cmd.Flags().StringSlice("tag", nil, "filter by tag (can be repeated, combines with OR)")
	cmd.Flags().Bool("group", false, "group repos by tag in a tree view")
	_ = cmd.RegisterFlagCompletionFunc("tag", tagCompletionFunc())

	return cmd
}

type listEntry struct {
	Name string   `json:"name"`
	Path string   `json:"path"`
	Tags []string `json:"tags,omitempty"`
}

func renderListJSON(w io.Writer, repos []config.RepoEntry) error {
	entries := make([]listEntry, len(repos))
	for i, r := range repos {
		entries[i] = listEntry{Name: r.Name, Path: r.Path, Tags: r.Tags}
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(entries); err != nil {
		return fmt.Errorf("encoding json: %w", err)
	}
	return nil
}

func renderListTable(w io.Writer, repos []config.RepoEntry) {
	nameWidth := len("NAME")
	tagWidth := len("TAGS")
	hasTags := false

	for _, r := range repos {
		if len(r.Name) > nameWidth {
			nameWidth = len(r.Name)
		}
		tagStr := strings.Join(r.Tags, ", ")
		if len(tagStr) > tagWidth {
			tagWidth = len(tagStr)
		}
		if len(r.Tags) > 0 {
			hasTags = true
		}
	}
	nameWidth += 2
	tagWidth += 2

	if hasTags {
		header := fmt.Sprintf("  %-*s %-*s %s", nameWidth, "NAME", tagWidth, "TAGS", "PATH")
		_, _ = fmt.Fprintln(w, output.Dim(header))
		_, _ = fmt.Fprintln(w, output.Dim("  "+strings.Repeat("─", len(header)-2)))

		for _, r := range repos {
			tagStr := strings.Join(r.Tags, ", ")
			_, _ = fmt.Fprintf(w, "  %-*s %s%-*s %s\n",
				nameWidth, r.Name,
				"", tagWidth, output.Dim(tagStr),
				output.Dim(r.Path))
		}
	} else {
		header := fmt.Sprintf("  %-*s %s", nameWidth, "NAME", "PATH")
		_, _ = fmt.Fprintln(w, output.Dim(header))
		_, _ = fmt.Fprintln(w, output.Dim("  "+strings.Repeat("─", len(header)-2)))

		for _, r := range repos {
			_, _ = fmt.Fprintf(w, "  %-*s %s\n", nameWidth, r.Name, output.Dim(r.Path))
		}
	}
}

func renderListTree(w io.Writer, repos []config.RepoEntry) {
	groups := make(map[string][]config.RepoEntry)
	var untagged []config.RepoEntry

	for _, r := range repos {
		if len(r.Tags) == 0 {
			untagged = append(untagged, r)
			continue
		}
		for _, tag := range r.Tags {
			groups[tag] = append(groups[tag], r)
		}
	}

	tags := make([]string, 0, len(groups))
	for tag := range groups {
		tags = append(tags, tag)
	}
	sort.Strings(tags)

	nameWidth := 0
	for _, r := range repos {
		if len(r.Name) > nameWidth {
			nameWidth = len(r.Name)
		}
	}
	nameWidth += 2

	for i, tag := range tags {
		if i > 0 {
			_, _ = fmt.Fprintln(w)
		}
		_, _ = fmt.Fprintln(w, "  "+output.Dim(tag))
		renderTreeEntries(w, groups[tag], nameWidth)
	}

	if len(untagged) > 0 {
		if len(tags) > 0 {
			_, _ = fmt.Fprintln(w)
		}
		_, _ = fmt.Fprintln(w, "  "+output.Dim("untagged"))
		renderTreeEntries(w, untagged, nameWidth)
	}
}

func renderTreeEntries(w io.Writer, repos []config.RepoEntry, nameWidth int) {
	for i, r := range repos {
		connector := "├──"
		if i == len(repos)-1 {
			connector = "└──"
		}
		_, _ = fmt.Fprintf(w, "  %s %-*s %s\n",
			output.Dim(connector), nameWidth, r.Name, output.Dim(r.Path))
	}
}
