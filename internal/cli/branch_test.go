package cli_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// brGitEnv runs a git command in dir with extra environment variables, for
// branch tests that need controlled committer dates.
func brGitEnv(t *testing.T, dir string, env []string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "git", args...)
	cmd.Dir = dir
	cmd.Env = append(append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null"), env...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestIntegration_BranchOverview(t *testing.T) {
	testEnv(t)
	base := t.TempDir()
	api := filepath.Join(base, "br-api")
	front := filepath.Join(base, "br-front")
	initRepo(t, api)
	initRepo(t, front)
	runSokoInit(t, api)
	runSokoInit(t, front)
	wtGit(t, api, "checkout", "-q", "-b", "feat/sso")

	out := runSoko(t, "branch")
	if !strings.Contains(out, "br-api") || !strings.Contains(out, "feat/sso") {
		t.Errorf("branch overview = %q, want br-api on feat/sso", out)
	}
	if !strings.Contains(out, "br-front") || !strings.Contains(out, "master") {
		t.Errorf("branch overview = %q, want br-front on master", out)
	}
}

func TestIntegration_BranchLookupStates(t *testing.T) {
	testEnv(t)
	base := t.TempDir()

	// current: checked out; local: exists but not checked out;
	// remote: only as a remote tracking ref; missing: absent.
	current := filepath.Join(base, "br-current")
	local := filepath.Join(base, "br-local")
	remote := filepath.Join(base, "br-remote")
	missing := filepath.Join(base, "br-missing")

	initRepo(t, current)
	wtGit(t, current, "checkout", "-q", "-b", "feat/sso")
	initRepo(t, local)
	wtGit(t, local, "branch", "feat/sso")
	initRepo(t, remote)
	wtGit(t, remote, "update-ref", "refs/remotes/origin/feat/sso", "HEAD")
	initRepo(t, missing)

	for _, dir := range []string{current, local, remote, missing} {
		runSokoInit(t, dir)
	}

	out := runSoko(t, "branch", "feat/sso")
	for _, want := range []string{"✓ current", "✓ local", "○ remote only", "— missing"} {
		if !strings.Contains(out, want) {
			t.Errorf("branch lookup = %q, want %q", out, want)
		}
	}

	var rows []struct {
		Name  string `json:"name"`
		State string `json:"state"`
	}
	jsonOut := runSoko(t, "branch", "feat/sso", "--json")
	if err := json.Unmarshal([]byte(jsonOut), &rows); err != nil {
		t.Fatalf("parsing JSON: %v\noutput: %s", err, jsonOut)
	}
	states := make(map[string]string, len(rows))
	for _, r := range rows {
		states[r.Name] = r.State
	}
	want := map[string]string{
		"br-current": "current",
		"br-local":   "local",
		"br-remote":  "remote",
		"br-missing": "missing",
	}
	for name, state := range want {
		if states[name] != state {
			t.Errorf("state[%s] = %q, want %q", name, states[name], state)
		}
	}
}

func TestIntegration_BranchSwitch(t *testing.T) {
	testEnv(t)
	base := t.TempDir()
	has := filepath.Join(base, "sw-has")
	dirty := filepath.Join(base, "sw-dirty")
	without := filepath.Join(base, "sw-without")

	initRepo(t, has)
	wtGit(t, has, "branch", "feat/sso")
	initRepo(t, dirty)
	wtGit(t, dirty, "branch", "feat/sso")
	initRepo(t, without)
	for _, dir := range []string{has, dirty, without} {
		runSokoInit(t, dir)
	}
	if err := os.WriteFile(filepath.Join(dirty, "wip.txt"), []byte("wip\n"), 0o644); err != nil {
		t.Fatalf("dirtying repo: %v", err)
	}

	out := runSoko(t, "branch", "switch", "feat/sso")
	if !strings.Contains(out, "✓ switched") {
		t.Errorf("switch = %q, want sw-has switched", out)
	}
	if !strings.Contains(out, "dirty") {
		t.Errorf("switch = %q, want sw-dirty reported dirty", out)
	}
	if !strings.Contains(out, "pass -b to create") {
		t.Errorf("switch = %q, want sw-without missing with -b hint", out)
	}

	if br := wtGit(t, has, "rev-parse", "--abbrev-ref", "HEAD"); br != "feat/sso" {
		t.Errorf("sw-has branch = %q, want feat/sso", br)
	}
	// The dirty repo is left exactly as it was.
	if br := wtGit(t, dirty, "rev-parse", "--abbrev-ref", "HEAD"); br != "master" {
		t.Errorf("sw-dirty branch = %q, want master untouched", br)
	}

	// Running again reports already on branch.
	out = runSoko(t, "branch", "switch", "feat/sso", "sw-has")
	if !strings.Contains(out, "already on branch") {
		t.Errorf("second switch = %q, want already on branch", out)
	}
}

func TestIntegration_BranchSwitchCreate(t *testing.T) {
	testEnv(t)
	repo := filepath.Join(t.TempDir(), "sw-create")
	initRepo(t, repo)
	runSokoInit(t, repo)

	// Park the repo on a side branch with an extra commit, so creating from
	// the default branch is distinguishable from creating from HEAD.
	wtGit(t, repo, "checkout", "-q", "-b", "side")
	if err := os.WriteFile(filepath.Join(repo, "side.txt"), []byte("side\n"), 0o644); err != nil {
		t.Fatalf("writing side file: %v", err)
	}
	wtGit(t, repo, "add", ".")
	wtGit(t, repo, "commit", "-q", "-m", "side commit")

	out := runSoko(t, "branch", "switch", "feat/new", "-b", "--json")
	var results []struct {
		Name   string `json:"name"`
		Branch string `json:"branch"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(out), &results); err != nil {
		t.Fatalf("parsing JSON: %v\noutput: %s", err, out)
	}
	if len(results) != 1 || results[0].Status != "created" || results[0].Branch != "feat/new" {
		t.Errorf("switch -b results = %+v, want one created feat/new", results)
	}

	if br := wtGit(t, repo, "rev-parse", "--abbrev-ref", "HEAD"); br != "feat/new" {
		t.Errorf("branch = %q, want feat/new checked out", br)
	}
	// Created from master, not from the side branch.
	if wtGit(t, repo, "rev-parse", "feat/new") != wtGit(t, repo, "rev-parse", "master") {
		t.Error("feat/new should point at master, not the side branch")
	}
}

func TestIntegration_BranchSwitchRemoteTracking(t *testing.T) {
	testEnv(t)
	base := t.TempDir()
	origin := filepath.Join(base, "sw-origin")
	clone := filepath.Join(base, "sw-clone")
	initRepo(t, origin)
	wtGit(t, origin, "branch", "feat/sso")
	wtGit(t, base, "clone", "-q", origin, clone)
	runSokoInit(t, clone)

	out := runSoko(t, "branch", "switch", "feat/sso")
	if !strings.Contains(out, "switched (tracking remote)") {
		t.Errorf("switch = %q, want remote-only branch checked out with tracking", out)
	}
	if br := wtGit(t, clone, "rev-parse", "--abbrev-ref", "HEAD"); br != "feat/sso" {
		t.Errorf("clone branch = %q, want feat/sso", br)
	}
}

func TestIntegration_BranchStale(t *testing.T) {
	testEnv(t)
	repo := filepath.Join(t.TempDir(), "br-stale")
	initRepo(t, repo)
	runSokoInit(t, repo)

	// An unmerged branch whose last commit is years old.
	old := []string{
		"GIT_COMMITTER_DATE=2020-01-01T00:00:00",
		"GIT_AUTHOR_DATE=2020-01-01T00:00:00",
	}
	brGitEnv(t, repo, nil, "checkout", "-q", "-b", "old-work")
	if err := os.WriteFile(filepath.Join(repo, "old.txt"), []byte("old\n"), 0o644); err != nil {
		t.Fatalf("writing file: %v", err)
	}
	brGitEnv(t, repo, nil, "add", ".")
	brGitEnv(t, repo, old, "commit", "-q", "-m", "abandoned work")
	brGitEnv(t, repo, nil, "checkout", "-q", "master")

	out := runSoko(t, "branch", "stale")
	if !strings.Contains(out, "old-work") {
		t.Errorf("stale = %q, want old-work listed", out)
	}

	// The threshold is honoured: a huge --days hides it.
	out = runSoko(t, "branch", "stale", "--days", "100000")
	if strings.Contains(out, "old-work") {
		t.Errorf("stale --days 100000 = %q, want old-work below threshold", out)
	}

	jsonOut := runSoko(t, "branch", "stale", "--json")
	var results []struct {
		Name     string `json:"name"`
		Branches []struct {
			Branch string `json:"branch"`
			Days   int    `json:"days"`
		} `json:"branches"`
	}
	if err := json.Unmarshal([]byte(jsonOut), &results); err != nil {
		t.Fatalf("parsing JSON: %v\noutput: %s", err, jsonOut)
	}
	if len(results) != 1 || len(results[0].Branches) != 1 {
		t.Fatalf("stale JSON = %+v, want one repo with one stale branch", results)
	}
	if results[0].Branches[0].Branch != "old-work" || results[0].Branches[0].Days < 365 {
		t.Errorf("stale entry = %+v, want old-work older than a year", results[0].Branches[0])
	}
}
