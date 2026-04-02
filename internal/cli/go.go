package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/CelikE/soko/internal/config"
	"github.com/CelikE/soko/internal/output"
	"github.com/CelikE/soko/internal/picker"
)

// newGoCmd creates the cobra command for soko go.
func newGoCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "go",
		Short: "Interactively select a repo and print its path",
		Long: `Open an interactive picker to select a registered repo. The selected
repo's path is printed to stdout for use with command substitution:

  cd $(soko go)

Use --tag to filter the picker to repos with specific tags.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			w := cmd.OutOrStdout()
			stderr := cmd.ErrOrStderr()

			cfg, repos, err := loadReposWithTagFilter(cmd)
			if err != nil {
				return err
			}

			if len(repos) == 0 {
				if len(cfg.Repos) == 0 {
					output.Info(stderr, "no repos registered yet — cd into a repo and run: soko init")
				} else {
					output.Info(stderr, "no repos match the tag filter")
				}
				return fmt.Errorf("no repos available")
			}

			jsonFlag, _ := cmd.Flags().GetBool("json")

			// If not a terminal (piped), fall back to numbered list on stderr.
			if !picker.HasTerminal(os.Stdin) {
				return goNonInteractive(cmd, repos, jsonFlag, w, stderr)
			}

			names := make([]string, len(repos))
			paths := make([]string, len(repos))
			for i, r := range repos {
				names[i] = r.Name
				paths[i] = r.Path
			}

			items := picker.FormatItems(names, paths)
			picker.HideCursor(stderr)
			idx := picker.Run(os.Stdin, stderr, picker.Options{
				Title: "Select a repo:",
				Items: items,
			})
			picker.ShowCursor(stderr)

			if idx < 0 {
				return fmt.Errorf("cancelled")
			}

			selected := repos[idx]
			picker.RenderSelected(stderr, selected.Name, picker.FormatItems(
				[]string{selected.Name}, []string{selected.Path},
			)[0].Desc)

			if jsonFlag {
				return writeGoJSON(w, selected)
			}

			_, _ = fmt.Fprintln(w, selected.Path)
			return nil
		},
	}

	cmd.Flags().StringSlice("tag", nil, "filter by tag (can be repeated, combines with OR)")
	_ = cmd.RegisterFlagCompletionFunc("tag", tagCompletionFunc())

	return cmd
}

// goNonInteractive handles the case when stdin is not a terminal (piped).
func goNonInteractive(cmd *cobra.Command, repos []config.RepoEntry, jsonOut bool, w, stderr io.Writer) error {
	if len(repos) == 1 {
		if jsonOut {
			return writeGoJSON(w, repos[0])
		}
		_, _ = fmt.Fprintln(w, repos[0].Path)
		return nil
	}

	output.Info(stderr, "multiple repos — use soko cd <name> or run interactively")
	for i, r := range repos {
		_, _ = fmt.Fprintf(stderr, "  %s %s %s\n",
			output.Dim(fmt.Sprintf("[%d]", i+1)),
			r.Name,
			output.Dim(r.Path))
	}
	return fmt.Errorf("interactive selection requires a terminal")
}

type goJSON struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

func writeGoJSON(w io.Writer, entry config.RepoEntry) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(goJSON{Name: entry.Name, Path: entry.Path}); err != nil {
		return fmt.Errorf("encoding json: %w", err)
	}
	return nil
}
