package cli

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/CelikE/soko/internal/config"
)

// newRemoveCmd creates the cobra command for soko remove.
func newRemoveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remove [name]",
		Short: "Remove a repo from the registry",
		RunE: func(cmd *cobra.Command, args []string) error {
			w := cmd.OutOrStdout()

			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			allFlag, _ := cmd.Flags().GetBool("all")
			forceFlag, _ := cmd.Flags().GetBool("force")
			pathFlag, _ := cmd.Flags().GetString("path")
			jsonFlag, _ := cmd.Flags().GetBool("json")

			if allFlag {
				return removeAll(cmd, cfg, forceFlag, jsonFlag, w)
			}

			if pathFlag != "" {
				return removeByPath(cfg, pathFlag, jsonFlag, w)
			}

			if len(args) == 0 {
				return cmd.Usage()
			}

			return removeByName(cfg, args[0], jsonFlag, w)
		},
	}

	cmd.Flags().String("path", "", "remove by absolute path instead of name")
	cmd.Flags().Bool("all", false, "remove all registered repos")
	cmd.Flags().Bool("force", false, "skip confirmation prompt")
	cmd.ValidArgsFunction = repoNameCompletionFunc()

	return cmd
}

func removeByName(cfg *config.Config, name string, jsonOut bool, w io.Writer) error {
	cfg, removed, err := config.RemoveRepo(cfg, name)
	if err != nil {
		if errors.Is(err, config.ErrRepoNotFound) {
			return fmt.Errorf("not found: %s", name)
		}
		return fmt.Errorf("removing repo: %w", err)
	}

	if err := config.Save(cfg); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	if jsonOut {
		return writeRemovedJSON(w, []config.RepoEntry{removed})
	}

	_, _ = fmt.Fprintf(w, "removed: %s (%s)\n", removed.Name, removed.Path)
	return nil
}

func removeByPath(cfg *config.Config, path string, jsonOut bool, w io.Writer) error {
	cfg, removed, err := config.RemoveRepoByPath(cfg, path)
	if err != nil {
		if errors.Is(err, config.ErrRepoNotFound) {
			return fmt.Errorf("not found: %s", path)
		}
		return fmt.Errorf("removing repo: %w", err)
	}

	if err := config.Save(cfg); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	if jsonOut {
		return writeRemovedJSON(w, []config.RepoEntry{removed})
	}

	_, _ = fmt.Fprintf(w, "removed: %s (%s)\n", removed.Name, removed.Path)
	return nil
}

func removeAll(cmd *cobra.Command, cfg *config.Config, force, jsonOut bool, w io.Writer) error {
	count := len(cfg.Repos)
	if count == 0 {
		_, _ = fmt.Fprintln(w, "no repos registered")
		return nil
	}

	if !force {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "remove all %d repos? [y/N] ", count)

		scanner := bufio.NewScanner(cmd.InOrStdin())
		if !scanner.Scan() {
			return nil
		}

		answer := strings.TrimSpace(strings.ToLower(scanner.Text()))
		if answer != "y" && answer != "yes" {
			_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "aborted")
			return nil
		}
	}

	removed := make([]config.RepoEntry, len(cfg.Repos))
	copy(removed, cfg.Repos)

	cfg = config.Clear(cfg)

	if err := config.Save(cfg); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	if jsonOut {
		return writeRemovedJSON(w, removed)
	}

	_, _ = fmt.Fprintf(w, "removed all %d repos\n", count)
	return nil
}

func writeRemovedJSON(w io.Writer, entries []config.RepoEntry) error {
	type entry struct {
		Name string `json:"name"`
		Path string `json:"path"`
	}

	out := make([]entry, len(entries))
	for i, e := range entries {
		out[i] = entry{Name: e.Name, Path: e.Path}
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		return fmt.Errorf("encoding json: %w", err)
	}
	return nil
}
