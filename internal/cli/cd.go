package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/CelikE/soko/internal/config"
	"github.com/CelikE/soko/internal/output"
)

// newCdCmd creates the cobra command for soko cd.
func newCdCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "cd <name>",
		Short:             "Navigate to a registered repo by name",
		ValidArgsFunction: repoNameCompletionFunc(),
		Long: `Navigate to a registered repo by name. Supports exact and prefix matching.

Requires shell integration. See: soko shell-init --help`,
		RunE: func(cmd *cobra.Command, args []string) error {
			w := cmd.OutOrStdout()
			stderr := cmd.ErrOrStderr()

			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			if len(cfg.Repos) == 0 {
				output.Info(stderr, noReposMessage(0))
				return fmt.Errorf("no repos registered")
			}

			if len(args) == 0 {
				output.Info(stderr, "usage: soko cd <name>")
				output.Info(stderr, "for interactive selection use: soko go")
				return fmt.Errorf("no repo name provided")
			}

			jsonFlag, _ := cmd.Flags().GetBool("json")
			return cdByQuery(cfg, args[0], jsonFlag, w, stderr)
		},
	}
}

func cdByQuery(cfg *config.Config, query string, jsonOut bool, w, stderr io.Writer) error {
	matches := config.FindRepo(cfg, query)

	switch len(matches) {
	case 0:
		return fmt.Errorf("no repo matching: %s", query)
	case 1:
		if jsonOut {
			return writeCdJSON(w, matches[0])
		}

		// Write nav file so the shell hook can cd.
		if err := writeNavFile(matches[0].Path); err != nil {
			// Fall back to printing the path.
			_, _ = fmt.Fprintln(w, matches[0].Path)
			return nil
		}

		output.Confirm(stderr, fmt.Sprintf("→ %s", matches[0].Name))
		return nil
	default:
		output.Warn(stderr, fmt.Sprintf("multiple repos match %q:", query))
		for _, m := range matches {
			_, _ = fmt.Fprintf(stderr, "    %s  %s\n", m.Name, output.Dim(m.Path))
		}
		return fmt.Errorf("multiple repos match %q", query)
	}
}

type cdJSON struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

func writeCdJSON(w io.Writer, entry config.RepoEntry) error {
	return output.RenderJSON(w, cdJSON{Name: entry.Name, Path: entry.Path})
}
