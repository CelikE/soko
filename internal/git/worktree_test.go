package git

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseWorktreesPreservesPathsAndFlags(t *testing.T) {
	out := "worktree /repos/base\x00HEAD abc\x00branch refs/heads/main\x00\x00" +
		"worktree /worktrees/quote\" and\nnewline \x00HEAD def\x00detached\x00locked external drive\x00\x00" +
		"worktree /missing\x00HEAD ghi\x00branch refs/heads/feat/oauth\x00prunable gitdir missing\x00\x00"
	entries := parseWorktrees(out)
	if len(entries) != 3 {
		t.Fatalf("entries = %+v", entries)
	}
	if entries[1].Path != "/worktrees/quote\" and\nnewline " || !entries[1].Detached ||
		!entries[1].Locked || entries[1].LockReason != "external drive" || entries[1].HEAD != "def" {
		t.Fatalf("detached entry lost metadata: %+v", entries[1])
	}
	if entries[2].Branch != "feat/oauth" || !entries[2].Prunable || entries[2].PruneReason != "gitdir missing" {
		t.Fatalf("prunable entry = %+v", entries[2])
	}
	bare := parseWorktrees("worktree /repos/bare.git\x00bare\x00\x00")
	if len(bare) != 1 || !bare[0].Bare {
		t.Fatalf("bare entry = %+v", bare)
	}
}

func TestWorktreeResolutionWithSeparateGitDirectory(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	root := filepath.Join(dir, "checkout")
	admin := filepath.Join(dir, "administration")
	gitMust(t, dir, "init", "-b", "main", "--separate-git-dir", admin, root)
	configRepo(t, root)
	writeAndCommit(t, root, "file.txt", "content", "initial")
	linked := filepath.Join(dir, "feature space")
	gitMust(t, root, "worktree", "add", "-b", "feat/oauth", linked)
	main, err := MainRepoPath(ctx, root)
	if err != nil || filepath.Clean(main) != filepath.Clean(resolveTestPath(t, root)) {
		t.Fatalf("main = %q, error = %v; want %s", main, err, root)
	}
	baseCommon, err := CommonDir(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	linkedCommon, err := CommonDir(ctx, linked)
	if err != nil || baseCommon != linkedCommon || !IsWorktree(ctx, linked) || IsWorktree(ctx, root) {
		t.Fatalf("repository identity mismatch: %q %q, %v", baseCommon, linkedCommon, err)
	}
	entries, err := WorktreeList(ctx, root)
	if err != nil || len(entries) != 1 || entries[0].Branch != "feat/oauth" {
		t.Fatalf("linked inventory = %+v, %v", entries, err)
	}
}

func TestMainRepoPathRejectsBareBase(t *testing.T) {
	dir := t.TempDir()
	gitMust(t, dir, "init", "--bare", ".")
	_, err := MainRepoPath(context.Background(), dir)
	if err == nil || !strings.Contains(err.Error(), "bare") {
		t.Fatalf("bare base should fail explicitly, got %v", err)
	}
}

func resolveTestPath(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}
