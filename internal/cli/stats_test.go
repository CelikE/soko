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

// A checkout can remain on disk after its Git directory is moved away. It is
// neither missing nor a verified clean repository.
func TestIntegration_StatsFailedReadAndUnborn(t *testing.T) {
	testEnv(t)
	base := t.TempDir()
	valid := filepath.Join(base, "valid")
	broken := filepath.Join(base, "broken")
	unborn := filepath.Join(base, "unborn")
	for _, dir := range []string{valid, broken} {
		initRepo(t, dir)
		runSokoInit(t, dir)
	}
	if err := os.Mkdir(unborn, 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(context.Background(), "git", "init", "-b", "main", unborn)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init unborn: %v: %s", err, out)
	}
	runSokoInit(t, unborn)
	if err := os.Rename(filepath.Join(broken, ".git"), filepath.Join(base, "moved-git")); err != nil {
		t.Fatal(err)
	}

	var summary struct {
		Repos        int `json:"repos"`
		Clean        int `json:"clean"`
		Dirty        int `json:"dirty"`
		Errors       int `json:"errors"`
		NoRemote     int `json:"no_remote"`
		TotalCommits int `json:"total_commits"`
	}
	out := runSoko(t, "stats", "--json")
	if err := json.Unmarshal([]byte(out), &summary); err != nil {
		t.Fatalf("stats JSON: %v: %s", err, out)
	}
	if summary.Repos != 3 || summary.Clean != 2 || summary.Dirty != 0 || summary.Errors != 1 || summary.NoRemote != 2 || summary.TotalCommits != 1 {
		t.Errorf("stats = %+v, want 2 clean (valid and unborn), 1 error, 2 without remotes and 1 commit", summary)
	}
	if text := runSoko(t, "stats"); !strings.Contains(text, "errors") || !strings.Contains(text, "1 repo") {
		t.Errorf("stats text omits read error: %s", text)
	}
}

func TestIntegration_StatsOverview(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "stats-repo")
	initRepo(t, dir)
	runSokoInit(t, dir)

	out := runSoko(t, "stats")

	for _, want := range []string{"OVERVIEW", "HEALTH", "ACTIVITY", "repos", "total commits", "total size"} {
		if !strings.Contains(out, want) {
			t.Errorf("stats = %q, want %q", out, want)
		}
	}
}

func TestIntegration_StatsJSON(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "stats-json")
	initRepo(t, dir)
	runSokoInit(t, dir)

	out := runSoko(t, "stats", "--json")

	var summary struct {
		Repos         int `json:"repos"`
		TotalCommits  int `json:"total_commits"`
		TotalBranches int `json:"total_branches"`
		ActiveRepos   int `json:"active_repos"`
		ActivityDays  int `json:"activity_days"`
		MostActive    *struct {
			Name    string `json:"name"`
			Commits int    `json:"commits"`
		} `json:"most_active,omitempty"`
	}
	if err := json.Unmarshal([]byte(out), &summary); err != nil {
		t.Fatalf("json unmarshal: %v\noutput: %s", err, out)
	}

	if summary.Repos != 1 {
		t.Errorf("repos = %d, want 1", summary.Repos)
	}
	if summary.TotalCommits < 1 {
		t.Errorf("total_commits = %d, want >=1", summary.TotalCommits)
	}
	if summary.ActivityDays != 30 {
		t.Errorf("activity_days = %d, want 30", summary.ActivityDays)
	}
	if summary.MostActive == nil || summary.MostActive.Name != "stats-json" {
		t.Errorf("most_active = %+v, want name=stats-json", summary.MostActive)
	}
}

func TestIntegration_StatsTagFilter(t *testing.T) {
	testEnv(t)
	dirA := filepath.Join(t.TempDir(), "alpha")
	dirB := filepath.Join(t.TempDir(), "bravo")
	initRepo(t, dirA)
	initRepo(t, dirB)
	runSokoInit(t, dirA)
	runSokoInit(t, dirB)
	runSoko(t, "tag", "add", "-r", "alpha", "backend")

	out := runSoko(t, "stats", "--tag", "backend", "--json")
	var summary struct {
		Repos int `json:"repos"`
	}
	if err := json.Unmarshal([]byte(out), &summary); err != nil {
		t.Fatalf("json unmarshal: %v\noutput: %s", err, out)
	}
	if summary.Repos != 1 {
		t.Errorf("repos with --tag backend = %d, want 1", summary.Repos)
	}
}

func TestIntegration_StatsNoRepos(t *testing.T) {
	testEnv(t)

	out := runSoko(t, "stats")
	if !strings.Contains(out, "no repos") {
		t.Errorf("stats = %q, want 'no repos'", out)
	}
}
