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
	if got := ctxRollbackGit(t, repo.Path, "stash", "list", "--format=%H"); got != r.stashOID {
		t.Errorf("rollback backup = %s, want owned stash %s", got, r.stashOID)
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
	if got := ctxRollbackGit(t, second.Path, "stash", "list", "--format=%H"); got != secondResult.stashOID {
		t.Errorf("successful rollback lost its backup: %s", got)
	}
}

func TestFindStashCommitRejectsAmbiguousCapture(t *testing.T) {
	repo, r := setupCtxRollback(t, 0)
	subject := ctxRollbackGit(t, repo.Path, "stash", "list", "--format=%gs")
	if err := os.WriteFile(filepath.Join(repo.Path, "other.txt"), []byte("other work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctxRollbackGit(t, repo.Path, "stash", "push", "-u", "-m", "unrelated")
	ctxRollbackGit(t, repo.Path, "stash", "store", "-m", subject, r.stashOID)
	before := ctxRollbackGit(t, repo.Path, "stash", "list", "--format=%H %gs")
	oid, found, err := findStashCommit(context.Background(), repo.Path, r.stashMessage)
	if err == nil || found || oid != "" {
		t.Fatalf("ambiguous capture resolved to %s, %v, %v", oid, found, err)
	}
	if after := ctxRollbackGit(t, repo.Path, "stash", "list", "--format=%H %gs"); after != before {
		t.Errorf("ambiguous lookup changed stashes: %s, want %s", after, before)
	}
}

func TestRollbackCtxSaveUsesPinnedIdentityAfterStashRemoval(t *testing.T) {
	repo, r := setupCtxRollback(t, 0)
	ctxRollbackGit(t, repo.Path, "stash", "clear")
	if err := rollbackCtxSave(context.Background(), []config.RepoEntry{repo}, []ctxRepoResult{r}, false); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(repo.Path, "work.txt"))
	if err != nil || string(data) != "unsaved work\n" {
		t.Errorf("pinned recovery failed: %q, %v", data, err)
	}
	if got := ctxRollbackGit(t, repo.Path, "stash", "list"); got != "" {
		t.Errorf("rollback changed shared stash list: %s", got)
	}
}

func TestFindStashCommitIgnoresRelabeledForeignCommit(t *testing.T) {
	repo, r := setupCtxRollback(t, 0)
	subject := ctxRollbackGit(t, repo.Path, "show", "-s", "--format=%s", r.stashOID)
	if err := os.WriteFile(filepath.Join(repo.Path, "foreign.txt"), []byte("foreign work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctxRollbackGit(t, repo.Path, "stash", "push", "-u", "-m", "foreign")
	foreign := ctxRollbackGit(t, repo.Path, "rev-parse", "refs/stash")
	ctxRollbackGit(t, repo.Path, "stash", "clear")
	// Reflog labels are mutable independently of their commit's contents.
	ctxRollbackGit(t, repo.Path, "stash", "store", "-m", subject, foreign)
	oid, found, err := findStashCommit(context.Background(), repo.Path, r.stashMessage)
	if err != nil || found || oid != "" {
		t.Fatalf("foreign commit inherited ownership through reflog label: %s, %v, %v", oid, found, err)
	}
}
