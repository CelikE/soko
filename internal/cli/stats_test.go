package cli_test

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

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
	runSoko(t, "tag", "add", "alpha", "backend")

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
