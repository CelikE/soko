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
		Short: "Interactively select a repo and navigate to it",
		Long: `Open an interactive picker to select a registered repo. Once selected,
soko navigates your shell to that directory.

Requires shell integration:
  eval "$(soko shell-init)"

Use --tag to filter the picker to repos with specific tags.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			w := cmd.OutOrStdout()
			stderr := cmd.ErrOrStderr()

			cfg, repos, err := loadReposWithTagFilter(cmd)
			if err != nil {
				return err
			}

			if len(repos) == 0 {
				output.Info(stderr, noReposMessage(len(cfg.Repos)))
				return fmt.Errorf("no repos available")
			}

			jsonFlag, _ := cmd.Flags().GetBool("json")

			// If stdin is not a terminal (piped), fall back.
			if !picker.HasTerminal(os.Stdin) {
				return goNonInteractive(repos, jsonFlag, w, stderr)
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

			if jsonFlag {
				return writeGoJSON(w, selected)
			}

			// Write the nav file so the shell hook can cd.
			if err := writeNavFile(selected.Path); err != nil {
				// Fall back to printing the path if nav file fails.
				_, _ = fmt.Fprintln(w, selected.Path)
				return nil
			}

			output.Confirm(stderr, fmt.Sprintf("→ %s", selected.Name))
			return nil
		},
	}

	cmd.Flags().StringSlice("tag", nil, "filter by tag (can be repeated, combines with OR)")
	_ = cmd.RegisterFlagCompletionFunc("tag", tagCompletionFunc())

	return cmd
}

// goNonInteractive handles the case when stdin is not a terminal.
func goNonInteractive(repos []config.RepoEntry, jsonOut bool, w, stderr io.Writer) error {
	if len(repos) == 1 {
		if jsonOut {
			return writeGoJSON(w, repos[0])
		}
		if err := writeNavFile(repos[0].Path); err != nil {
			_, _ = fmt.Fprintln(w, repos[0].Path)
		}
		return nil
	}

	output.Info(stderr, "multiple repos match — pick one with soko cd:")
	for _, r := range repos {
		_, _ = fmt.Fprintf(stderr, "    soko cd %s\n", r.Name)
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
