package cli

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/CelikE/soko/internal/config"
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
					_, _ = fmt.Fprintln(w, "no repos registered yet — cd into a repo and run: soko init")
				} else {
					_, _ = fmt.Fprintln(w, "no repos match the tag filter")
				}
				return nil
			}

			jsonFlag, _ := cmd.Flags().GetBool("json")
			if jsonFlag {
				return renderListJSON(w, repos)
			}

			renderListTable(w, repos)
			return nil
		},
	}

	cmd.Flags().StringSlice("tag", nil, "filter by tag (can be repeated, combines with OR)")
	_ = cmd.RegisterFlagCompletionFunc("tag", tagCompletionFunc())

	return cmd
}

type listEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

func renderListJSON(w io.Writer, repos []config.RepoEntry) error {
	entries := make([]listEntry, len(repos))
	for i, r := range repos {
		entries[i] = listEntry{Name: r.Name, Path: r.Path}
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
	for _, r := range repos {
		if len(r.Name) > nameWidth {
			nameWidth = len(r.Name)
		}
	}
	nameWidth += 2

	_, _ = fmt.Fprintf(w, "  %-*s %s\n", nameWidth, "NAME", "PATH")
	for _, r := range repos {
		_, _ = fmt.Fprintf(w, "  %-*s %s\n", nameWidth, r.Name, r.Path)
	}
}
