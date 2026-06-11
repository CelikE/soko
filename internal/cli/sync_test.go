package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CelikE/soko/internal/cli"
)

func TestIntegration_Sync(t *testing.T) {
	testEnv(t)
	repo := filepath.Join(t.TempDir(), "sync-repo")
	seed := setupUpstreamClone(t, repo)
	runSokoInit(t, repo)

	// Up to date on the first sync — fetch runs, nothing to pull.
	out := runSoko(t, "sync")
	if !strings.Contains(out, "up to date") {
		t.Errorf("sync = %q, want a row 'up to date'", out)
	}
	if !strings.Contains(out, "0 pulled") || !strings.Contains(out, "1 up to date") {
		t.Errorf("sync summary = %q, want '0 pulled' and '1 up to date'", out)
	}

	// Advancing the upstream means the next sync fast-forwards.
	advanceUpstream(t, seed)
	out = runSoko(t, "sync")
	if !strings.Contains(out, "1 pulled") {
		t.Errorf("sync after upstream advance summary = %q, want '1 pulled'", out)
	}
	if !strings.Contains(out, "1 new commit") {
		t.Errorf("sync = %q, want '1 new commit'", out)
	}
	if !strings.Contains(out, "fetch + pull") {
		t.Errorf("sync = %q, want action 'fetch + pull'", out)
	}
}

// TestIntegration_SyncDirtySkipsPull is the core safety property: a dirty repo
// that is behind its upstream is fetched but never pulled, and that is not a
// failure (exit 0).
func TestIntegration_SyncDirtySkipsPull(t *testing.T) {
	testEnv(t)
	repo := filepath.Join(t.TempDir(), "dirty-sync-repo")
	seed := setupUpstreamClone(t, repo)
	runSokoInit(t, repo)

	advanceUpstream(t, seed)
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("# local edit\n"), 0o644); err != nil {
		t.Fatalf("dirtying repo: %v", err)
	}

	out := runSoko(t, "sync") // must not error
	if !strings.Contains(out, "dirty (skipped pull)") {
		t.Errorf("sync dirty = %q, want 'dirty (skipped pull)'", out)
	}
	if !strings.Contains(out, "0 pulled") || !strings.Contains(out, "1 need attention") {
		t.Errorf("sync dirty summary = %q, want '0 pulled' and '1 need attention'", out)
	}
}

// TestIntegration_SyncDiverged verifies a clean repo that is both ahead and
// behind is reported as diverged and left untouched, with exit 0 — unlike
// pull, sync treats divergence as the user's call, not a failure.
func TestIntegration_SyncDiverged(t *testing.T) {
	testEnv(t)
	repo := filepath.Join(t.TempDir(), "diverged-sync-repo")
	seed := setupUpstreamClone(t, repo)
	runSokoInit(t, repo)

	localCommit(t, repo, "local.txt", "local")
	advanceUpstream(t, seed)

	out := runSoko(t, "sync") // must not error
	if !strings.Contains(out, "diverged (needs rebase)") {
		t.Errorf("sync diverged = %q, want 'diverged (needs rebase)'", out)
	}
	if !strings.Contains(out, "1 need attention") || !strings.Contains(out, "0 failed") {
		t.Errorf("sync diverged summary = %q, want '1 need attention' and '0 failed'", out)
	}
}

func TestIntegration_SyncFetchOnly(t *testing.T) {
	testEnv(t)
	repo := filepath.Join(t.TempDir(), "fetchonly-sync-repo")
	seed := setupUpstreamClone(t, repo)
	runSokoInit(t, repo)

	advanceUpstream(t, seed)

	out := runSoko(t, "sync", "--fetch-only")
	if !strings.Contains(out, "behind 1 (not pulled)") {
		t.Errorf("sync --fetch-only = %q, want 'behind 1 (not pulled)'", out)
	}
	if !strings.Contains(out, "0 pulled") {
		t.Errorf("sync --fetch-only summary = %q, want '0 pulled'", out)
	}

	// The repo must still be behind: fetch-only never mutates the tree.
	out = runSoko(t, "sync")
	if !strings.Contains(out, "1 pulled") {
		t.Errorf("sync after --fetch-only = %q, want '1 pulled'", out)
	}
}

// TestIntegration_SyncNoRemote verifies a repo with no remote at all is
// skipped (it has no upstream either) — not a failure, not a non-zero exit.
func TestIntegration_SyncNoRemote(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "no-remote-sync-repo")
	initRepo(t, dir) // no remote -> no upstream -> skipped
	runSokoInit(t, dir)

	out := runSoko(t, "sync") // must not error
	if !strings.Contains(out, "no upstream") {
		t.Errorf("sync no remote = %q, want 'no upstream'", out)
	}
	if !strings.Contains(out, "1 skipped") || !strings.Contains(out, "0 failed") {
		t.Errorf("sync no remote summary = %q, want '1 skipped' and '0 failed'", out)
	}
}

func TestIntegration_SyncJSON(t *testing.T) {
	testEnv(t)
	base := t.TempDir()

	upToDate := filepath.Join(base, "sync-json-alpha")
	pulled := filepath.Join(base, "sync-json-bravo")
	skipped := filepath.Join(base, "sync-json-charlie")

	setupUpstreamClone(t, upToDate)
	advSeed := setupUpstreamClone(t, pulled)
	initRepo(t, skipped)

	runSokoInit(t, upToDate)
	runSokoInit(t, pulled)
	runSokoInit(t, skipped)

	advanceUpstream(t, advSeed)

	out := runSoko(t, "sync", "--json")

	var entries []struct {
		Name       string `json:"name"`
		Status     string `json:"status"`
		Action     string `json:"action"`
		NewCommits int    `json:"new_commits"`
	}
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		t.Fatalf("parsing JSON: %v\noutput: %s", err, out)
	}
	if len(entries) != 3 {
		t.Fatalf("JSON entries = %d, want 3", len(entries))
	}

	byName := map[string]struct {
		status, action string
		newCommits     int
	}{}
	for _, e := range entries {
		byName[e.Name] = struct {
			status, action string
			newCommits     int
		}{e.Status, e.Action, e.NewCommits}
	}
	if got := byName["sync-json-alpha"]; got.status != "up-to-date" {
		t.Errorf("sync-json-alpha status = %q, want 'up-to-date'", got.status)
	}
	if got := byName["sync-json-bravo"]; got.status != "pulled" || got.action != "fetch + pull" || got.newCommits != 1 {
		t.Errorf("sync-json-bravo = %+v, want pulled / fetch + pull / 1 new commit", got)
	}
	if got := byName["sync-json-charlie"]; got.status != "skipped" {
		t.Errorf("sync-json-charlie status = %q, want 'skipped'", got.status)
	}
}

func TestIntegration_SyncMissingPath(t *testing.T) {
	testEnv(t)
	repo := filepath.Join(t.TempDir(), "gone-sync-repo")
	setupUpstreamClone(t, repo)
	runSokoInit(t, repo)

	if err := os.RemoveAll(repo); err != nil {
		t.Fatalf("removing dir: %v", err)
	}

	var stdout bytes.Buffer
	cmd := cli.NewRootCmd("test")
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"sync"})

	if err := cmd.Execute(); err == nil {
		t.Error("sync with missing path should return an error")
	}

	out := stdout.String()
	if !strings.Contains(out, "path not found") {
		t.Errorf("sync missing path = %q, want 'path not found'", out)
	}
	if !strings.Contains(out, "soko prune") {
		t.Errorf("sync missing path = %q, want a 'soko prune' hint", out)
	}
}

// TestIntegration_SyncPositionalFilter verifies positional repo names narrow
// the sync set, matching the status/fetch/pull selection grammar.
func TestIntegration_SyncPositionalFilter(t *testing.T) {
	testEnv(t)
	base := t.TempDir()

	alpha := filepath.Join(base, "sync-pos-alpha")
	bravo := filepath.Join(base, "sync-pos-bravo")
	setupUpstreamClone(t, alpha)
	setupUpstreamClone(t, bravo)
	runSokoInit(t, alpha)
	runSokoInit(t, bravo)

	out := runSoko(t, "sync", "sync-pos-alpha")
	if !strings.Contains(out, "sync-pos-alpha") {
		t.Errorf("sync filtered = %q, want sync-pos-alpha present", out)
	}
	if strings.Contains(out, "sync-pos-bravo") {
		t.Errorf("sync filtered = %q, must not include sync-pos-bravo", out)
	}
	if !strings.Contains(out, "1 repo") {
		t.Errorf("sync filtered summary = %q, want '1 repo'", out)
	}
}
