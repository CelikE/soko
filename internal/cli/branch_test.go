package cli_test

import (
	"context"
	"encoding/json"
	"fmt"
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

func TestIntegration_BranchSwitchRepeatedRepoArgs(t *testing.T) {
	testEnv(t)
	base := t.TempDir()
	for _, name := range []string{"api", "api-v2", "web"} {
		dir := filepath.Join(base, name)
		initRepo(t, dir)
		wtGit(t, dir, "branch", "feat/selected")
		runSokoInit(t, dir)
	}

	out := runSoko(t, "branch", "switch", "feat/selected", "api", "web", "api", "--json")
	var results []struct {
		Name   string `json:"name"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(out), &results); err != nil {
		t.Fatalf("parsing JSON: %v\noutput: %s", err, out)
	}
	if len(results) != 2 {
		t.Fatalf("switch results = %+v, want only api and web", results)
	}
	for i, name := range []string{"api", "web"} {
		if results[i].Name != name || results[i].Status != "switched" {
			t.Errorf("result[%d] = %+v, want %s switched", i, results[i], name)
		}
	}
	for _, name := range []string{"api", "api-v2", "web"} {
		want := "feat/selected"
		if name == "api-v2" {
			want = "master"
		}
		if got := wtGit(t, filepath.Join(base, name), "rev-parse", "--abbrev-ref", "HEAD"); got != want {
			t.Errorf("%s branch = %q, want %q", name, got, want)
		}
	}
}

func TestIntegration_BranchSwitchSkipsWorktrees(t *testing.T) {
	for _, tt := range []struct {
		name   string
		create bool
		json   bool
	}{
		{name: "existing/text"},
		{name: "existing/json-tag", json: true},
		{name: "create/text", create: true},
		{name: "create/json-tag", create: true, json: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			testEnv(t)
			base := t.TempDir()
			parent := filepath.Join(base, "sw-family")
			other := filepath.Join(base, "sw-other")
			for _, dir := range []string{parent, other} {
				initRepo(t, dir)
				runSokoInit(t, dir)
				if !tt.create {
					wtGit(t, dir, "branch", "feat/selected")
				}
			}
			runSoko(t, "tag", "add", "-r", "sw-family", "backend")
			runSoko(t, "worktree", "add", "sw-family", "side", "-b")
			linked := filepath.Join(base, "sw-family-side")
			before := wtGit(t, linked, "rev-parse", "HEAD")

			args := []string{"branch", "switch", "feat/selected"}
			if tt.create {
				args = append(args, "-b")
			}
			if tt.json {
				args = append(args, "--json", "--tag", "backend")
			}
			out := runSoko(t, args...)
			wantStatus := "switched"
			if tt.create {
				wantStatus = "created"
			}
			if tt.json {
				var rows []struct {
					Name   string `json:"name"`
					Status string `json:"status"`
				}
				if err := json.Unmarshal([]byte(out), &rows); err != nil {
					t.Fatalf("parsing JSON: %v\noutput: %s", err, out)
				}
				if len(rows) != 1 || rows[0].Name != "sw-family" || rows[0].Status != wantStatus {
					t.Errorf("switch results = %+v, want only sw-family %s", rows, wantStatus)
				}
			} else if !strings.Contains(out, "2 "+wantStatus) || strings.Contains(out, "sw-family/side") {
				t.Errorf("switch output = %q, want two base repos %s and no linked worktree", out, wantStatus)
			}

			for dir, want := range map[string]string{parent: "feat/selected", linked: "side", other: "feat/selected"} {
				if dir == other && tt.json {
					want = "master"
				}
				if got := wtGit(t, dir, "rev-parse", "--abbrev-ref", "HEAD"); got != want {
					t.Errorf("branch in %s = %q, want %q", dir, got, want)
				}
			}
			if got := wtGit(t, linked, "rev-parse", "HEAD"); got != before {
				t.Errorf("linked worktree HEAD = %q, want unchanged %q", got, before)
			}
		})
	}
}

func TestIntegration_BranchSwitchWorktreeOnlySelection(t *testing.T) {
	testEnv(t)
	base := t.TempDir()
	parent := filepath.Join(base, "sw-family")
	initRepo(t, parent)
	runSokoInit(t, parent)
	runSoko(t, "worktree", "add", "sw-family", "side", "-b", "--tag", "worktree-only")
	linked := filepath.Join(base, "sw-family-side")

	for _, filter := range [][]string{{"sw-family/side"}, {"--tag", "worktree-only"}} {
		args := append([]string{"branch", "switch", "feat/selected", "-b"}, filter...)
		out := runSoko(t, args...)
		if !strings.Contains(out, "no repos match the filters") || strings.Contains(out, "RESULT") {
			t.Errorf("worktree-only switch output = %q, want only no-repos message", out)
		}
		if out := strings.TrimSpace(runSoko(t, append(args, "--json")...)); out != "[]" {
			t.Errorf("worktree-only switch JSON = %q, want []", out)
		}
	}
	for dir, want := range map[string]string{parent: "master", linked: "side"} {
		if got := wtGit(t, dir, "rev-parse", "--abbrev-ref", "HEAD"); got != want {
			t.Errorf("branch in %s = %q, want unchanged %q", dir, got, want)
		}
	}
	if got := wtGit(t, parent, "branch", "--list", "feat/selected"); got != "" {
		t.Errorf("branch created for empty selection: %q", got)
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

func TestIntegration_BranchSwitchRemoteSafety(t *testing.T) {
	for _, format := range []string{"text", "json"} {
		for _, ambiguous := range []bool{false, true} {
			name := "unconfigured-remote/" + format
			if ambiguous {
				name = "multiple-remotes/" + format
			}
			t.Run(name, func(t *testing.T) {
				testEnv(t)
				repo := filepath.Join(t.TempDir(), "remote-safety")
				initRepo(t, repo)
				runSokoInit(t, repo)
				before := wtGit(t, repo, "rev-parse", "HEAD")
				wtGit(t, repo, "commit", "--allow-empty", "-m", "remote work")
				tip := wtGit(t, repo, "rev-parse", "HEAD")
				wtGit(t, repo, "update-ref", "refs/remotes/old/feat/remote", tip)
				wtGit(t, repo, "reset", "--hard", before)
				if ambiguous {
					wtGit(t, repo, "update-ref", "refs/remotes/other/feat/remote", tip)
				}

				args := []string{"branch", "switch", "feat/remote"}
				if format == "json" {
					args = append(args, "--json")
				}
				out, err := runSokoErr(t, args...)
				wantStatus := "switched"
				if ambiguous {
					wantStatus = "failed"
					if err == nil || !strings.Contains(out, "branch exists on multiple remotes") {
						t.Errorf("ambiguous switch = %s, %v; want refusal", out, err)
					}
				} else if err != nil {
					t.Fatalf("non-tracking fallback failed: %s, %v", out, err)
				}
				if format == "json" {
					var rows []struct {
						Name   string `json:"name"`
						Status string `json:"status"`
					}
					if err := json.Unmarshal([]byte(out), &rows); err != nil {
						t.Fatal(err)
					}
					if len(rows) != 1 || rows[0].Name != "remote-safety" || rows[0].Status != wantStatus {
						t.Errorf("switch = %s, want %s", out, wantStatus)
					}
				} else if !ambiguous && (!strings.Contains(out, "no upstream") || strings.Contains(out, "tracking remote")) {
					t.Errorf("fallback output = %q, want accurate non-tracking message", out)
				}

				wantHEAD, wantBranch := tip, "feat/remote"
				if ambiguous {
					wantHEAD, wantBranch = before, "master"
					if refs := wtGit(t, repo, "for-each-ref", "--format=%(refname)", "refs/heads/feat/remote"); refs != "" {
						t.Errorf("ambiguous switch created local branch: %s", refs)
					}
				}
				if got := wtGit(t, repo, "rev-parse", "HEAD"); got != wantHEAD {
					t.Errorf("HEAD = %s, want %s", got, wantHEAD)
				}
				if got := wtGit(t, repo, "branch", "--show-current"); got != wantBranch {
					t.Errorf("branch = %s, want %s", got, wantBranch)
				}
				if upstream := wtGit(t, repo, "for-each-ref", "--format=%(upstream)", "refs/heads/feat/remote"); upstream != "" {
					t.Errorf("fallback configured nonexistent upstream: %s", upstream)
				}
				if status := wtGit(t, repo, "status", "--porcelain"); status != "" {
					t.Errorf("switch dirtied checkout: %s", status)
				}
			})
		}
	}
}

func TestIntegration_BranchStaleDaysValidation(t *testing.T) {
	for _, selection := range []string{"empty", "name", "tag"} {
		t.Run(selection, func(t *testing.T) {
			testEnv(t)
			args := []string{"branch", "stale"}
			if selection != "empty" {
				repo := filepath.Join(t.TempDir(), "stale-validation")
				initRepo(t, repo)
				runSokoInit(t, repo)
				if selection == "name" {
					args = append(args, "nonexistent")
				} else {
					args = append(args, "--tag", "nonexistent")
				}
			}
			out, err := runSokoErr(t, append(args, "--days", "-1")...)
			if err == nil || !strings.Contains(err.Error(), "--days must be zero or positive") {
				t.Fatalf("negative days = %q, %v; want threshold validation error", out, err)
			}
			if out, err := runSokoErr(t, append(args, "--days", "0")...); err != nil {
				t.Fatalf("zero days = %q, %v; want success", out, err)
			}
		})
	}
}

func TestIntegration_BranchStaleFailures(t *testing.T) {
	for _, jsonOut := range []bool{false, true} {
		for _, failure := range []string{"missing", "not-git"} {
			t.Run(fmt.Sprintf("json=%t/%s", jsonOut, failure), func(t *testing.T) {
				testEnv(t)
				repo := filepath.Join(t.TempDir(), "broken")
				initRepo(t, repo)
				runSokoInit(t, repo)
				remove := repo
				if failure == "not-git" {
					remove = filepath.Join(repo, ".git")
				}
				if err := os.RemoveAll(remove); err != nil {
					t.Fatal(err)
				}
				args := []string{"branch", "stale"}
				if jsonOut {
					args = append(args, "--json")
				}
				out, err := runSokoErr(t, args...)
				if err == nil || !strings.Contains(err.Error(), "1 repo failed to inspect stale branches") {
					t.Errorf("stale = %q, %v; want repository failure", out, err)
				}
				if jsonOut {
					var results []struct {
						Error     string `json:"error"`
						ErrorCode string `json:"error_code"`
					}
					if err := json.Unmarshal([]byte(out), &results); err != nil {
						t.Fatalf("invalid JSON: %v: %s", err, out)
					}
					if len(results) != 1 || results[0].Error == "" || results[0].ErrorCode == "" {
						t.Fatalf("missing error details: %+v", results)
					}
				}
			})
		}
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

	// Failed inspections must not hide stale branches from healthy repositories.
	broken := filepath.Join(t.TempDir(), "missing")
	initRepo(t, broken)
	runSokoInit(t, broken)
	if err := os.RemoveAll(broken); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"branch", "stale"}, {"branch", "stale", "--json"}} {
		out, err := runSokoErr(t, args...)
		if err == nil || !strings.Contains(err.Error(), "1 repo failed to inspect stale branches") {
			t.Errorf("mixed stale report = %q, %v; want repository failure", out, err)
		}
		if !strings.Contains(out, "old-work") || !strings.Contains(out, "path not found") {
			t.Errorf("mixed stale report lost success or error: %s", out)
		}
	}
}
