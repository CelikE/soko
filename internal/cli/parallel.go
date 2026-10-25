package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/CelikE/soko/internal/output"
)

// maxConcurrency is the maximum number of goroutines for parallel repo
// operations. 8 balances throughput with file descriptor limits.
const maxConcurrency = 8

// pathExists returns true if the given path exists on disk.
func pathExists(path string) bool {
	_, err := os.Stat(path)
	return !os.IsNotExist(err)
}

// noReposMessage returns the appropriate message when no repos are available.
func noReposMessage(totalRepos int) string {
	if totalRepos == 0 {
		return "no repos registered yet — cd into a repo and run: soko init"
	}
	noun := "repos"
	if totalRepos == 1 {
		noun = "repo"
	}
	return fmt.Sprintf("no repos match the filters (%d %s registered)", totalRepos, noun)
}

// renderNoRepos reports an empty selection without starting any repo operations.
func renderNoRepos(cmd *cobra.Command, totalRepos int) error {
	w := cmd.OutOrStdout()
	if jsonFlag, _ := cmd.Flags().GetBool("json"); jsonFlag {
		return output.RenderJSON(w, []any{})
	}
	output.Info(w, noReposMessage(totalRepos))
	return nil
}

// renderMissingHint prints a non-destructive warning nudging the user to run
// soko prune when n registered repos no longer exist on disk. It is a no-op
// when n is zero.
func renderMissingHint(w io.Writer, n int) {
	if n <= 0 {
		return
	}
	verb := "exist"
	if n == 1 {
		verb = "exists"
	}
	output.Warn(w, fmt.Sprintf("%d %s no longer %s — run: soko prune",
		n, output.Plural(n, "repo"), verb))
}
