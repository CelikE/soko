package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/CelikE/soko/internal/config"
)

func TestWriteApplyRejectsSymlinkSwapAfterPlan(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	docs := filepath.Join(dir, "docs")
	if err := os.Mkdir(docs, 0o755); err != nil {
		t.Fatal(err)
	}
	const original = "external content\n"
	outsideFile := filepath.Join(outside, "f.txt")
	if err := os.WriteFile(outsideFile, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	src := []byte("replacement\n")
	results := planApply(ctx, []config.RepoEntry{{Name: "repo", Path: dir}}, src, "docs/f.txt")
	if len(results) != 1 || results[0].Action != applyCreate {
		t.Fatalf("plan = %+v, want create", results)
	}

	// The destination can change while the user is reading the confirmation.
	if err := os.Remove(docs); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, docs); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	writeApply(ctx, results, src, 0o644)
	if results[0].Written || results[0].Error == "" {
		t.Errorf("write = %+v, want failure after symlink swap", results[0])
	}
	if got, err := os.ReadFile(outsideFile); err != nil || string(got) != original {
		t.Errorf("external file = %q, err=%v, want original content", got, err)
	}
	entries, err := os.ReadDir(outside)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "f.txt" {
		t.Errorf("external directory changed: %v", entries)
	}
}
