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

func newAliasCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "alias",
		Short: "Manage command aliases",
		Long: `Create, list, and remove user-defined command shortcuts.

Aliases are stored in the soko config and expand transparently when invoked.`,
		Example: `  soko alias set morning "sync --tag work"
  soko alias set deploy "exec --tag production -- make deploy"
  soko alias list
  soko alias remove morning
  soko morning`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(newAliasSetCmd())
	cmd.AddCommand(newAliasRemoveCmd())
	cmd.AddCommand(newAliasListCmd())

	return cmd
}

func newAliasSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set <name> <command>",
		Short: "Create or update an alias",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			command := args[1]

			if strings.TrimSpace(name) == "" || strings.ContainsAny(name, " \t") {
				return fmt.Errorf("alias name must be a single word without spaces")
			}
			if strings.TrimSpace(command) == "" {
				return fmt.Errorf("alias command must not be empty")
			}

			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			if cfg.Aliases == nil {
				cfg.Aliases = make(map[string]string)
			}
			cfg.Aliases[name] = command

			if err := config.Save(cfg); err != nil {
				return fmt.Errorf("saving config: %w", err)
			}

			output.Confirm(cmd.OutOrStdout(), fmt.Sprintf("alias %s = %s", name, command))
			return nil
		},
	}
}

func newAliasRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <name>",
		Short: "Remove an alias",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]

			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			if _, ok := cfg.Aliases[name]; !ok {
				return fmt.Errorf("alias %q not found", name)
			}

			delete(cfg.Aliases, name)

			if err := config.Save(cfg); err != nil {
				return fmt.Errorf("saving config: %w", err)
			}

			output.Confirm(cmd.OutOrStdout(), fmt.Sprintf("removed alias %s", name))
			return nil
		},
	}
}

func newAliasListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all aliases",
		RunE: func(cmd *cobra.Command, _ []string) error {
			w := cmd.OutOrStdout()

			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			jsonFlag, _ := cmd.Flags().GetBool("json")

			if len(cfg.Aliases) == 0 {
				if jsonFlag {
					_, _ = fmt.Fprintln(w, "[]")
					return nil
				}
				output.Info(w, "no aliases defined")
				return nil
			}
			if jsonFlag {
				return renderAliasListJSON(w, cfg.Aliases)
			}

			names := make([]string, 0, len(cfg.Aliases))
			for name := range cfg.Aliases {
				names = append(names, name)
			}
			sort.Strings(names)

			nameWidth := 0
			for _, name := range names {
				if len(name) > nameWidth {
					nameWidth = len(name)
				}
			}
			nameWidth += 2

			header := fmt.Sprintf("  %-*s %s", nameWidth, "NAME", "COMMAND")
			_, _ = fmt.Fprintln(w, output.Dim(header))
			_, _ = fmt.Fprintln(w, output.Dim("  "+strings.Repeat("─", len(header)-2)))

			for _, name := range names {
				_, _ = fmt.Fprintf(w, "  %-*s %s\n", nameWidth, name, cfg.Aliases[name])
			}

			return nil
		},
	}
}

type aliasListJSON struct {
	Name    string `json:"name"`
	Command string `json:"command"`
}

func renderAliasListJSON(w io.Writer, aliases map[string]string) error {
	names := make([]string, 0, len(aliases))
	for name := range aliases {
		names = append(names, name)
	}
	sort.Strings(names)

	entries := make([]aliasListJSON, len(names))
	for i, name := range names {
		entries[i] = aliasListJSON{Name: name, Command: aliases[name]}
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(entries); err != nil {
		return fmt.Errorf("encoding json: %w", err)
	}
	return nil
}
