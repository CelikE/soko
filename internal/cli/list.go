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
	return &cobra.Command{
		Use:   "list",
		Short: "List all registered repos",
		RunE: func(cmd *cobra.Command, _ []string) error {
			w := cmd.OutOrStdout()

			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			if len(cfg.Repos) == 0 {
				_, _ = fmt.Fprintln(w, "no repos registered yet — cd into a repo and run: soko init")
				return nil
			}

			jsonFlag, _ := cmd.Flags().GetBool("json")
			if jsonFlag {
				return renderListJSON(w, cfg.Repos)
			}

			renderListTable(w, cfg.Repos)
			return nil
		},
	}
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
	// Compute name column width.
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
