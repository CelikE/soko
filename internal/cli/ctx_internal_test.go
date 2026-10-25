package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CelikE/soko/internal/config"
	"github.com/CelikE/soko/internal/git"
)

func ctxRollbackGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := git.Run(context.Background(), dir, args...)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func setupCtxRollback(t *testing.T, index int) (config.RepoEntry, ctxRepoResult) {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	dir := t.TempDir()
	ctxRollbackGit(t, dir, "init", "--initial-branch=main")
	ctxRollbackGit(t, dir, "config", "user.name", "Test")
	ctxRollbackGit(t, dir, "config", "user.email", "test@example.com")
	ctxRollbackGit(t, dir, "commit", "--allow-empty", "-m", "initial")
	if err := os.WriteFile(filepath.Join(dir, "work.txt"), []byte("unsaved work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	repo := config.RepoEntry{Name: filepath.Base(dir), Path: dir}
	r := ctxSaveRepo(context.Background(), index, &repo, "work")
	if !r.success || !r.stashed {
		t.Fatalf("save did not create a stash: %+v", r)
	}
	return repo, r
}

func TestRollbackCtxSaveCanceled(t *testing.T) {
	repo, r := setupCtxRollback(t, 0)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := rollbackCtxSave(ctx, []config.RepoEntry{repo}, []ctxRepoResult{r}, false); err != nil {
		t.Fatalf("canceled save did not restore work: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(repo.Path, "work.txt"))
	if err != nil || string(data) != "unsaved work\n" {
		t.Errorf("restored work = %q, %v", data, err)
	}
	if got := ctxRollbackGit(t, repo.Path, "stash", "list"); got != "" {
		t.Errorf("rollback left a stash: %s", got)
	}
}

func TestRollbackCtxSaveConflictRetainsStashAndContinues(t *testing.T) {
	first, firstResult := setupCtxRollback(t, 0)
	second, secondResult := setupCtxRollback(t, 1)
	// A new file occupying the original untracked path must not be overwritten.
	if err := os.WriteFile(filepath.Join(first.Path, "work.txt"), []byte("newer work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := rollbackCtxSave(context.Background(), []config.RepoEntry{first, second},
		[]ctxRepoResult{firstResult, secondResult}, false)
	if err == nil {
		t.Fatal("rollback should report the conflicting path")
	}
	for _, want := range []string{first.Name, "could not restore unsaved work", "git stash list", firstResult.stashMessage} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("rollback error = %q, want %q", err, want)
		}
	}
	data, readErr := os.ReadFile(filepath.Join(first.Path, "work.txt"))
	if readErr != nil || string(data) != "newer work\n" {
		t.Errorf("rollback overwrote newer work: %q, %v", data, readErr)
	}
	if got := ctxRollbackGit(t, first.Path, "stash", "list"); !strings.Contains(got, firstResult.stashMessage) {
		t.Errorf("failed rollback lost its stash: %s", got)
	}
	data, readErr = os.ReadFile(filepath.Join(second.Path, "work.txt"))
	if readErr != nil || string(data) != "unsaved work\n" {
		t.Errorf("rollback did not continue to second repo: %q, %v", data, readErr)
	}
	if got := ctxRollbackGit(t, second.Path, "stash", "list"); got != "" {
		t.Errorf("successful rollback left a stash: %s", got)
	}
}
