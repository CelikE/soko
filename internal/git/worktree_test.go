package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseWorktreesPreservesPathsAndFlags(t *testing.T) {
	out := "worktree /repos/base\x00HEAD abc\x00branch refs/heads/main\x00\x00" +
		"worktree /worktrees/päth\" and\nnewline \x00HEAD def\x00detached\x00locked external drive\x00\x00" +
		"worktree /missing\x00HEAD ghi\x00branch refs/heads/feat/oauth\x00prunable gitdir missing\x00\x00"
	entries := parseWorktrees(out)
	if len(entries) != 3 {
		t.Fatalf("entries = %+v", entries)
	}
	if entries[1].Path != "/worktrees/päth\" and\nnewline " || !entries[1].Detached ||
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

func TestWorktreeResolution(t *testing.T) {
	root := initTestRepo(t)
	gitMust(t, root, "commit", "--allow-empty", "-m", "initial")
	base := t.TempDir()
	linked, detached := filepath.Join(base, "feature space ü"), filepath.Join(base, "detached")
	gitMust(t, root, "worktree", "add", "-b", "feat/linked", linked)
	gitMust(t, root, "worktree", "add", "--detach", detached, "HEAD")
	for _, dir := range []string{root, linked} {
		if err := os.Mkdir(filepath.Join(dir, "nested"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, tt := range []struct {
		name   string
		dir    string
		linked bool
	}{
		{name: "main", dir: root},
		{name: "main subdirectory", dir: filepath.Join(root, "nested")},
		{name: "linked", dir: linked, linked: true},
		{name: "linked subdirectory", dir: filepath.Join(linked, "nested"), linked: true},
		{name: "detached linked", dir: detached, linked: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsWorktree(t.Context(), tt.dir); got != tt.linked {
				t.Errorf("IsWorktree(%q) = %v, want %v", tt.dir, got, tt.linked)
			}
			main, err := MainRepoPath(t.Context(), tt.dir)
			if err != nil || filepath.Clean(main) != filepath.Clean(resolveTestPath(t, root)) {
				t.Errorf("MainRepoPath(%q) = %q, %v; want %q", tt.dir, main, err, root)
			}
			entries, err := WorktreeList(t.Context(), tt.dir)
			if err != nil || len(entries) != 2 {
				t.Fatalf("WorktreeList(%q) = %+v, %v; want two linked checkouts", tt.dir, entries, err)
			}
			byPath := make(map[string]WorktreeInfo)
			for _, entry := range entries {
				byPath[filepath.Clean(entry.Path)] = entry
			}
			attached := byPath[filepath.Clean(resolveTestPath(t, linked))]
			detachedEntry := byPath[filepath.Clean(resolveTestPath(t, detached))]
			if attached.Branch != "feat/linked" || attached.Detached || detachedEntry.Branch != "" || !detachedEntry.Detached || attached.HEAD == "" || attached.HEAD != detachedEntry.HEAD {
				t.Errorf("worktree metadata = %+v", entries)
			}
		})
	}
	t.Run("not a repository", func(t *testing.T) {
		dir := t.TempDir()
		if IsWorktree(t.Context(), dir) {
			t.Error("non-repository reported as worktree")
		}
		if main, err := MainRepoPath(t.Context(), dir); err == nil || main != "" {
			t.Errorf("MainRepoPath(non-repo) = %q, %v; want error", main, err)
		}
		if entries, err := WorktreeList(t.Context(), dir); err == nil || len(entries) != 0 {
			t.Errorf("WorktreeList(non-repo) = %+v, %v; want error", entries, err)
		}
	})
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
