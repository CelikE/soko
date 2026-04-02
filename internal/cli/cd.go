package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/CelikE/soko/internal/config"
	"github.com/CelikE/soko/internal/output"
	"github.com/CelikE/soko/internal/picker"
)

// newCdCmd creates the cobra command for soko cd.
func newCdCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "cd [name]",
		Short:             "Print the path of a registered repo",
		ValidArgsFunction: repoNameCompletionFunc(),
		Long: `Print the absolute path of a registered repo so you can use it with
command substitution: cd $(soko cd auth)

Supports exact and prefix matching. If multiple repos match a prefix,
they are listed so you can refine your query.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			w := cmd.OutOrStdout()
			stderr := cmd.ErrOrStderr()

			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			if len(cfg.Repos) == 0 {
				output.Info(stderr, "no repos registered yet — cd into a repo and run: soko init")
				return fmt.Errorf("no repos registered")
			}

			jsonFlag, _ := cmd.Flags().GetBool("json")

			if len(args) > 0 {
				return cdByQuery(cfg, args[0], jsonFlag, w, stderr)
			}

			return cdInteractive(cmd, cfg, jsonFlag, w, stderr)
		},
	}
}

func cdByQuery(cfg *config.Config, query string, jsonOut bool, w, stderr io.Writer) error {
	matches := config.FindRepo(cfg, query)

	switch len(matches) {
	case 0:
		_, _ = fmt.Fprintf(stderr, "no repo matching: %s\n", query)
		return fmt.Errorf("no repo matching: %s", query)
	case 1:
		if jsonOut {
			return writeCdJSON(w, matches[0])
		}
		_, _ = fmt.Fprintln(w, matches[0].Path)
		if picker.HasTerminal(os.Stdout) {
			_, _ = fmt.Fprintln(stderr)
			output.Info(stderr, shellNavHint())
		}
		return nil
	default:
		_, _ = fmt.Fprintf(stderr, "multiple repos match %q:\n", query)
		for _, m := range matches {
			_, _ = fmt.Fprintf(stderr, "  %s  %s\n", m.Name, m.Path)
		}
		return fmt.Errorf("multiple repos match %q", query)
	}
}

func cdInteractive(cmd *cobra.Command, cfg *config.Config, jsonOut bool, w, stderr io.Writer) error {
	for i, r := range cfg.Repos {
		_, _ = fmt.Fprintf(stderr, "  [%d] %s  %s\n", i+1, r.Name, r.Path)
	}
	_, _ = fmt.Fprint(stderr, "select repo: ")

	scanner := bufio.NewScanner(cmd.InOrStdin())
	if !scanner.Scan() {
		return fmt.Errorf("no selection")
	}

	input := strings.TrimSpace(scanner.Text())
	idx, err := strconv.Atoi(input)
	if err != nil || idx < 1 || idx > len(cfg.Repos) {
		return fmt.Errorf("invalid selection: %s", input)
	}

	selected := cfg.Repos[idx-1]
	if jsonOut {
		return writeCdJSON(w, selected)
	}
	_, _ = fmt.Fprintln(w, selected.Path)
	return nil
}

type cdJSON struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

func writeCdJSON(w io.Writer, entry config.RepoEntry) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(cdJSON{Name: entry.Name, Path: entry.Path}); err != nil {
		return fmt.Errorf("encoding json: %w", err)
	}
	return nil
}
