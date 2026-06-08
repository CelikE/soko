package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"

	"github.com/CelikE/soko/internal/config"
	"github.com/CelikE/soko/internal/output"
)

func TestOriginURL(t *testing.T) {
	tests := []struct {
		name    string
		remotes map[string]string
		want    string
	}{
		{"no remotes", map[string]string{}, "-"},
		{"nil map", nil, "-"},
		{"origin present", map[string]string{"origin": "git@github.com:acme/api.git"}, "git@github.com:acme/api.git"},
		{
			"origin preferred over others",
			map[string]string{"fork": "git@github.com:me/api.git", "origin": "git@github.com:acme/api.git"},
			"git@github.com:acme/api.git",
		},
		{
			"no origin falls back to first sorted remote",
			map[string]string{"zeta": "z.git", "alpha": "a.git"},
			"a.git",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := originURL(tt.remotes); got != tt.want {
				t.Errorf("originURL(%v) = %q, want %q", tt.remotes, got, tt.want)
			}
		})
	}
}

func TestFilterTrackingFailures(t *testing.T) {
	results := []remoteResult{
		{index: 0, name: "ok", trackingOK: true},
		{index: 1, name: "noremote", flag: "no remote"},
		{index: 2, name: "noupstream", flag: "no upstream"},
		{index: 3, name: "ok2", trackingOK: true},
		{index: 4, name: "err", err: "path not found"},
	}

	got := filterTrackingFailures(results)
	if len(got) != 3 {
		t.Fatalf("filterTrackingFailures() returned %d, want 3", len(got))
	}
	wantNames := []string{"noremote", "noupstream", "err"}
	for i, w := range wantNames {
		if got[i].name != w {
			t.Errorf("filterTrackingFailures()[%d].name = %q, want %q", i, got[i].name, w)
		}
	}
}

func TestRemoteRowOf(t *testing.T) {
	tests := []struct {
		name         string
		result       remoteResult
		wantOrigin   string
		wantUpstream string
		wantTracking string
		wantState    output.RowState
	}{
		{
			name:         "tracking ok",
			result:       remoteResult{name: "api", remotes: map[string]string{"origin": "u.git"}, upstream: "origin/main", trackingOK: true},
			wantOrigin:   "u.git",
			wantUpstream: "origin/main",
			wantTracking: output.SymClean + " ok",
			wantState:    output.StateClean,
		},
		{
			name:         "no remote",
			result:       remoteResult{name: "scratch", remotes: map[string]string{}, flag: "no remote"},
			wantOrigin:   "-",
			wantUpstream: "-",
			wantTracking: output.SymWarning + " no remote",
			wantState:    output.StateDirty,
		},
		{
			name:         "no upstream",
			result:       remoteResult{name: "spike", remotes: map[string]string{"origin": "u.git"}, flag: "no upstream"},
			wantOrigin:   "u.git",
			wantUpstream: "-",
			wantTracking: output.SymWarning + " no upstream",
			wantState:    output.StateDirty,
		},
		{
			name:         "errored",
			result:       remoteResult{name: "gone", flag: "not found", err: "path not found"},
			wantOrigin:   "-",
			wantUpstream: "-",
			wantTracking: output.SymConflict + " not found",
			wantState:    output.StateConflict,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := tt.result
			row := remoteRowOf(&r)
			if row.Origin != tt.wantOrigin {
				t.Errorf("Origin = %q, want %q", row.Origin, tt.wantOrigin)
			}
			if row.Upstream != tt.wantUpstream {
				t.Errorf("Upstream = %q, want %q", row.Upstream, tt.wantUpstream)
			}
			if row.Tracking != tt.wantTracking {
				t.Errorf("Tracking = %q, want %q", row.Tracking, tt.wantTracking)
			}
			if row.State != tt.wantState {
				t.Errorf("State = %v, want %v", row.State, tt.wantState)
			}
		})
	}
}

func TestCollectRemotes(t *testing.T) {
	// Repo 0: has origin + tracking upstream → trackingOK.
	tracking := cloneWithUpstream(t)
	// Repo 1: a fresh repo, no remote → "no remote".
	local := initGitRepo(t)
	// Repo 2: a path that does not exist → error row.
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	// Repo 3: a path that exists but is not a git repo → git.Remotes errors.
	notRepo := t.TempDir()

	repos := []config.RepoEntry{
		{Name: "tracking", Path: tracking},
		{Name: "local", Path: local},
		{Name: "missing", Path: missing},
		{Name: "broken", Path: notRepo},
	}

	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	got := collectRemotes(cmd, repos)

	if len(got) != 4 {
		t.Fatalf("collectRemotes() returned %d, want 4", len(got))
	}

	// Order must follow config order regardless of goroutine completion.
	if got[0].name != "tracking" || got[1].name != "local" || got[2].name != "missing" || got[3].name != "broken" {
		t.Fatalf("collectRemotes() order = [%s %s %s %s], want [tracking local missing broken]",
			got[0].name, got[1].name, got[2].name, got[3].name)
	}

	if !got[0].trackingOK {
		t.Errorf("tracking repo trackingOK = false, want true (flag=%q err=%q)", got[0].flag, got[0].err)
	}
	if got[0].upstream != "origin/master" {
		t.Errorf("tracking repo upstream = %q, want origin/master", got[0].upstream)
	}

	if got[1].trackingOK || got[1].flag != "no remote" {
		t.Errorf("local repo = {trackingOK:%v flag:%q}, want {false no remote}", got[1].trackingOK, got[1].flag)
	}

	if got[2].err == "" || got[2].flag != "not found" {
		t.Errorf("missing repo = {err:%q flag:%q}, want non-empty err and flag=not found", got[2].err, got[2].flag)
	}

	// An existing path that is not a git repo: git.Remotes errors → "error" flag.
	if got[3].err == "" || got[3].flag != "error" || got[3].trackingOK {
		t.Errorf("broken repo = {err:%q flag:%q trackingOK:%v}, want non-empty err, flag=error, trackingOK=false",
			got[3].err, got[3].flag, got[3].trackingOK)
	}
}

func TestRenderRemotesJSON(t *testing.T) {
	results := []remoteResult{
		{name: "api", path: "/work/api", remotes: map[string]string{"origin": "u.git"}, upstream: "origin/main", trackingOK: true},
		{name: "scratch", path: "/work/scratch", remotes: nil, err: ""},
	}

	var buf bytes.Buffer
	if err := renderRemotesJSON(&buf, results); err != nil {
		t.Fatalf("renderRemotesJSON() error = %v", err)
	}

	var decoded []remotesJSON
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if len(decoded) != 2 {
		t.Fatalf("decoded %d entries, want 2", len(decoded))
	}
	if decoded[0].Remotes["origin"] != "u.git" || !decoded[0].TrackingOK {
		t.Errorf("entry 0 = %+v", decoded[0])
	}
	// A local-only repo must serialize remotes as {} (not null).
	if decoded[1].Remotes == nil {
		t.Error("entry 1 Remotes = nil, want empty map {}")
	}
	if !bytes.Contains(buf.Bytes(), []byte(`"remotes": {}`)) {
		t.Errorf("expected empty remotes to render as {}, got:\n%s", buf.String())
	}
}

// initGitRepo creates a temp git repo with an initial commit and returns its
// path. No remote is configured.
func initGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitInDir(t, dir, "init", "-b", "master")
	gitInDir(t, dir, "config", "user.email", "test@test.com")
	gitInDir(t, dir, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# t\n"), 0o644); err != nil {
		t.Fatalf("writing README: %v", err)
	}
	gitInDir(t, dir, "add", ".")
	gitInDir(t, dir, "commit", "-m", "initial")
	return dir
}

// cloneWithUpstream creates a bare upstream, seeds it, and returns a clone that
// tracks origin/master.
func cloneWithUpstream(t *testing.T) string {
	t.Helper()
	bare := t.TempDir()
	gitInDir(t, bare, "init", "--bare", "-b", "master", ".")

	seed := t.TempDir()
	gitInDir(t, seed, "clone", bare, ".")
	gitInDir(t, seed, "config", "user.email", "test@test.com")
	gitInDir(t, seed, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(seed, "a.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatalf("writing a.txt: %v", err)
	}
	gitInDir(t, seed, "add", ".")
	gitInDir(t, seed, "commit", "-m", "first")
	gitInDir(t, seed, "push", "origin", "master")

	repo := t.TempDir()
	gitInDir(t, repo, "clone", bare, ".")
	return repo
}

func gitInDir(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}
