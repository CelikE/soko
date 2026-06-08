package cli

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/CelikE/soko/internal/config"
	"github.com/CelikE/soko/internal/picker"
)

// addSelectFlag registers the shared --select flag so its text and behavior
// stay identical across clean, prune, and remove.
func addSelectFlag(cmd *cobra.Command) {
	cmd.Flags().Bool("select", false,
		"open the interactive picker to choose which repos the operation touches (requires a TTY)")
}

// selectRepos opens the multi-select picker on the matched repos and returns
// the chosen subset. Every repo starts checked; the picker can only narrow the
// set, never widen it. When stdin is not a terminal it returns repos unchanged
// (graceful fallback — --select is ignored in pipes/CI). A cancel or an empty
// selection returns (nil, false), which callers treat as "aborted".
func selectRepos(cmd *cobra.Command, title string, repos []config.RepoEntry) ([]config.RepoEntry, bool) {
	if !picker.HasTerminal(os.Stdin) {
		return repos, true
	}

	names := make([]string, len(repos))
	paths := make([]string, len(repos))
	for i, r := range repos {
		names[i] = r.Name
		paths[i] = r.Path
	}

	stderr := cmd.ErrOrStderr()
	picker.HideCursor(stderr)
	indices := picker.RunMulti(os.Stdin, stderr, picker.Options{
		Title: title,
		Items: picker.FormatItems(names, paths),
	})
	picker.ShowCursor(stderr)

	if indices == nil {
		return nil, false // cancelled with esc/Ctrl+C
	}
	chosen := mapSelected(repos, indices)
	if len(chosen) == 0 {
		return nil, false // deselected everything — treat as abort
	}
	return chosen, true
}

// mapSelected maps picker indices back to the repos they refer to, preserving
// order and ignoring any out-of-range index defensively.
func mapSelected(repos []config.RepoEntry, indices []int) []config.RepoEntry {
	out := make([]config.RepoEntry, 0, len(indices))
	for _, i := range indices {
		if i >= 0 && i < len(repos) {
			out = append(out, repos[i])
		}
	}
	return out
}
