package cli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"

	"github.com/CelikE/soko/internal/config"
)

func TestCollectUIRowsFailedReadAndUnborn(t *testing.T) {
	base := t.TempDir()
	broken := filepath.Join(base, "broken")
	unborn := filepath.Join(base, "unborn")
	for _, dir := range []string{broken, unborn} {
		cmd := exec.CommandContext(context.Background(), "git", "init", "-b", "main", dir)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git init: %v: %s", err, out)
		}
	}
	if err := os.Rename(filepath.Join(broken, ".git"), filepath.Join(base, "moved-git")); err != nil {
		t.Fatal(err)
	}
	repos := []config.RepoEntry{{Name: "broken", Path: broken}, {Name: "unborn", Path: unborn}}
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	rows := collectUIRows(cmd, repos, false)
	if len(rows) != 2 {
		t.Fatalf("rows = %+v", rows)
	}
	if r := rows[0]; !r.ReadError || r.Missing || r.Severity != sevCrit || r.StatusText != "✗ error" || len(r.Reasons) == 0 || r.Reasons[0] != "read error" {
		t.Errorf("broken row = %+v, want read error and critical severity", r)
	}
	if r := rows[1]; r.ReadError || r.Missing || r.Severity == sevCrit || r.StatusText == "✗ error" {
		t.Errorf("unborn row = %+v, want valid status", r)
	}
}
