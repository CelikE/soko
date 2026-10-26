package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CelikE/soko/internal/config"
)

func runSokoAt(t *testing.T, dir string, args ...string) string {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(orig) }()
	return runSoko(t, args...)
}

func familyFixture(t *testing.T) (root, alpha, beta string) {
	t.Helper()
	base := t.TempDir()
	root = filepath.Join(base, "api")
	initRepo(t, root)
	alpha, beta = filepath.Join(base, "oauth"), filepath.Join(base, "login")
	wtGit(t, root, "worktree", "add", "-b", "feat/oauth", alpha)
	wtGit(t, root, "worktree", "add", "-b", "fix/login", beta)
	return root, alpha, beta
}

func loadRegistry(t *testing.T) *config.Config {
	t.Helper()
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestIntegration_WorktreeRegistrationRootAndAliases(t *testing.T) {
	testEnv(t)
	root, alpha, _ := familyFixture(t)
	subdir := filepath.Join(alpha, "nested")
	if err := os.Mkdir(subdir, 0o755); err != nil {
		t.Fatal(err)
	}
	runSokoAt(t, subdir, "init", "--worktree", "--json")
	cfg := loadRegistry(t)
	if len(cfg.Repos) != 2 || cfg.Repos[1].Path != resolve(t, alpha) || cfg.Repos[1].WorktreeOf != "api" {
		t.Fatalf("registry = %+v", cfg.Repos)
	}
	// Reuse a custom base alias, and keep the worktree alias after changing branch.
	cfg.Repos[0].Name, cfg.Repos[1].WorktreeOf = "custom", "custom"
	cfg.Repos[1].Meta = map[string]string{"note": "preserve me"}
	if err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	wtGit(t, alpha, "switch", "-c", "renamed")
	runSokoAt(t, alpha, "init", "--worktree")
	runSoko(t, "worktree", "adopt", "custom")
	cfg = loadRegistry(t)
	if len(cfg.Repos) != 3 || cfg.Repos[1].Name != "api/feat/oauth" || cfg.Repos[1].Meta["note"] != "preserve me" ||
		cfg.Repos[2].Name != "custom/fix/login" || cfg.Repos[2].WorktreeOf != "custom" {
		t.Fatalf("aliases or metadata changed: %+v", cfg.Repos)
	}
	if cfg.Repos[0].Path != resolve(t, root) {
		t.Fatal("base path changed")
	}
}

func TestIntegration_WorktreeInitPersistsMissingParent(t *testing.T) {
	for _, format := range []string{"text", "json"} {
		t.Run(format, func(t *testing.T) {
			testEnv(t)
			root, linked, _ := familyFixture(t)
			runSokoAt(t, linked, "init", "--worktree", "--tag", "wip")
			cfg := loadRegistry(t)
			// Simulate a legacy registry whose worktree survived parent removal.
			cfg.Repos = cfg.Repos[1:]
			cfg.Repos[0].Meta = map[string]string{"note": "preserve me"}
			if err := config.Save(cfg); err != nil {
				t.Fatal(err)
			}
			args := []string{"init", "--worktree"}
			if format == "json" {
				args = append(args, "--json")
			}
			out := runSokoAt(t, linked, args...)
			if format == "json" {
				var row struct {
					Name              string `json:"name"`
					AlreadyRegistered bool   `json:"already_registered"`
				}
				if err := json.Unmarshal([]byte(out), &row); err != nil || row.Name != "api/feat/oauth" || !row.AlreadyRegistered {
					t.Errorf("init = %s, %v; want existing worktree", out, err)
				}
			} else if !strings.Contains(out, "registered api (") || !strings.Contains(out, "already registered api/feat/oauth") {
				t.Errorf("init = %q, want new parent and existing worktree", out)
			}
			cfg = loadRegistry(t)
			if len(cfg.Repos) != 2 || cfg.Repos[0].Name != "api/feat/oauth" || cfg.Repos[0].Meta["note"] != "preserve me" || len(cfg.Repos[0].Tags) != 1 || cfg.Repos[0].Tags[0] != "wip" || cfg.Repos[1].Name != "api" || cfg.Repos[1].Path != resolve(t, root) {
				t.Errorf("parent registration was lost or worktree changed: %+v", cfg.Repos)
			}
		})
	}
}

func TestIntegration_WorktreeScanLinkedRootJSONAndDryRun(t *testing.T) {
	testEnv(t)
	_, alpha, _ := familyFixture(t)
	runSoko(t, "scan", alpha, "--worktrees", "--dry-run", "--json")
	if len(loadRegistry(t).Repos) != 0 {
		t.Fatal("dry-run mutated registry")
	}
	out := runSoko(t, "scan", alpha, "--worktrees", "--json")
	var rows []map[string]any
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 || len(loadRegistry(t).Repos) != 3 {
		t.Fatalf("linked scan did not persist all checkouts: %s", out)
	}
	runSoko(t, "scan", alpha, "--worktrees", "--json")
	if len(loadRegistry(t).Repos) != 3 {
		t.Fatal("repeat scan duplicated checkouts")
	}
}

func TestIntegration_WorktreeAdoptDriftAndIdempotency(t *testing.T) {
	testEnv(t)
	root, alpha, _ := familyFixture(t)
	runSokoAt(t, root, "init", "--tag", "backend")
	wtGit(t, root, "worktree", "lock", "--reason", "portable drive", alpha)
	out := runSoko(t, "worktree", "list", "--repo", "api", "--json")
	var rows []struct {
		Registered bool   `json:"registered"`
		Status     string `json:"status"`
		Locked     bool   `json:"locked"`
	}
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].Registered || rows[0].Status != "unregistered" || !rows[0].Locked && !rows[1].Locked {
		t.Fatalf("drift output = %s", out)
	}
	if len(loadRegistry(t).Repos) != 1 {
		t.Fatal("list registered worktrees")
	}
	runSoko(t, "worktree", "adopt", "api", "--dry-run", "--json")
	if len(loadRegistry(t).Repos) != 1 {
		t.Fatal("adopt dry-run mutated registry")
	}
	runSoko(t, "worktree", "adopt", "--tag", "backend", "--tag-with", "wip", "--json")
	cfg := loadRegistry(t)
	if len(cfg.Repos) != 3 || len(cfg.Repos[1].Tags) != 1 || cfg.Repos[1].Tags[0] != "wip" {
		t.Fatalf("adopted entries = %+v", cfg.Repos)
	}
	if out := runSoko(t, "worktree", "adopt", "api", "--json"); strings.TrimSpace(out) != "[]" {
		t.Fatalf("repeat adopt = %s", out)
	}
	// An existing directory that is no longer a linked checkout is stale, not clean.
	wtGit(t, root, "worktree", "unlock", alpha)
	wtGit(t, root, "worktree", "remove", alpha)
	if err := os.Mkdir(alpha, 0o755); err != nil {
		t.Fatal(err)
	}
	out = runSoko(t, "worktree", "list", "--json")
	if !strings.Contains(out, `"status": "stale"`) {
		t.Fatalf("stale checkout not detected: %s", out)
	}
}

func TestIntegration_WorktreeFamiliesAndNavigation(t *testing.T) {
	testEnv(t)
	root, alpha, beta := familyFixture(t)
	runSokoAt(t, root, "init", "--tag", "backend")
	runSoko(t, "worktree", "adopt", "api")
	other := filepath.Join(t.TempDir(), "other")
	initRepo(t, other)
	runSokoAt(t, other, "init", "--tag", "backend")
	out := runSoko(t, "list", "--repo", "api", "--group-by", "repo")
	if strings.Contains(out, "other") || !strings.Contains(out, "api/feat/oauth") || !strings.Contains(out, "api/fix/login") {
		t.Fatalf("family list = %s", out)
	}
	out = runSoko(t, "status", "--repo", "api", "--group-by", "repo")
	if !strings.Contains(out, "api/feat/oauth") || strings.Contains(out, "other") {
		t.Fatalf("family status = %s", out)
	}
	out = runSoko(t, "status", "--repo", "api", "--group")
	if strings.Contains(out, "untagged") || !strings.Contains(out, "backend") {
		t.Fatalf("inherited tag grouping = %s", out)
	}
	if err := os.WriteFile(filepath.Join(alpha, "wip.txt"), []byte("unfinished"), 0o644); err != nil {
		t.Fatal(err)
	}
	runSokoAt(t, alpha, "go", "login", "--repo", ".")
	if target := readNavFile(t); target != resolve(t, beta) {
		t.Fatalf("target = %s", target)
	}
	if content, err := os.ReadFile(filepath.Join(alpha, "wip.txt")); err != nil || string(content) != "unfinished" {
		t.Fatal("navigation changed work in progress")
	}
	if br := wtGit(t, alpha, "branch", "--show-current"); br != "feat/oauth" {
		t.Fatal("navigation changed branch")
	}
	_, err := runSokoErr(t, "list", "--group", "--group-by", "repo")
	if err == nil {
		t.Fatal("contradictory grouping flags accepted")
	}
	_, err = runSokoErr(t, "status", "--group-by", "invalid")
	if err == nil {
		t.Fatal("invalid grouping accepted")
	}
}

func TestIntegration_WorktreeDetachedAndSameRemoteClones(t *testing.T) {
	testEnv(t)
	root, _, _ := familyFixture(t)
	wtGit(t, root, "remote", "add", "origin", "https://example.com/acme/api.git")
	for _, name := range []string{"review-a", "review-b"} {
		path := filepath.Join(filepath.Dir(root), name)
		wtGit(t, root, "worktree", "add", "--detach", path, "HEAD")
		runSokoAt(t, path, "init", "--worktree")
	}
	second := filepath.Join(t.TempDir(), "another-clone")
	initRepo(t, second)
	wtGit(t, second, "remote", "add", "origin", "https://example.com/acme/api.git")
	linked := filepath.Join(filepath.Dir(second), "another-feature")
	wtGit(t, second, "worktree", "add", "-b", "feat/oauth", linked)
	runSokoAt(t, linked, "init", "--worktree")
	cfg := loadRegistry(t)
	seen := make(map[string]bool)
	for _, entry := range cfg.Repos {
		if seen[entry.Name] {
			t.Fatalf("duplicate alias: %+v", cfg.Repos)
		}
		seen[entry.Name] = true
	}
	if !seen["api/review-a"] || !seen["api/review-b"] || !seen["api-2/feat/oauth"] {
		t.Fatalf("aliases = %+v", cfg.Repos)
	}
	if cfg.Repos[len(cfg.Repos)-1].WorktreeOf != "api-2" {
		t.Fatal("second clone linked to wrong base")
	}
}

func TestIntegration_WorktreeLegacyAmbiguityAndMissingNavigation(t *testing.T) {
	testEnv(t)
	first, second := t.TempDir(), t.TempDir()
	cfg := &config.Config{Repos: []config.RepoEntry{{Name: "duplicate", Path: first}, {Name: "duplicate", Path: second}}}
	if err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	_, err := runSokoErr(t, "cd", "duplicate", "--json")
	if err == nil || !strings.Contains(err.Error(), "multiple") {
		t.Fatalf("ambiguous exact match = %v", err)
	}
	cfg.Repos = []config.RepoEntry{{Name: "missing", Path: filepath.Join(first, "missing")}}
	if err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"cd", "go"} {
		_, err := runSokoErr(t, command, "missing", "--json")
		if err == nil || !strings.Contains(err.Error(), "path not found") {
			t.Fatalf("%s missing target = %v", command, err)
		}
	}
}

func TestIntegration_WorktreeSeparateGitDirectory(t *testing.T) {
	testEnv(t)
	base := t.TempDir()
	root, admin := filepath.Join(base, "api"), filepath.Join(base, "admin")
	wtGit(t, base, "init", "-b", "main", "--separate-git-dir", admin, root)
	wtGit(t, root, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "--allow-empty", "-m", "fixture")
	runSokoAt(t, root, "init")
	linked := filepath.Join(base, "feature")
	wtGit(t, root, "worktree", "add", "-b", "feature", linked)
	runSokoAt(t, linked, "init", "--worktree")
	out := runSokoAt(t, linked, "list", "--repo", ".", "--json")
	var entries []map[string]any
	if err := json.Unmarshal([]byte(out), &entries); err != nil || len(entries) != 2 {
		t.Fatalf("separate Git directory family = %s, %v", out, err)
	}
	if loadRegistry(t).Repos[1].WorktreeOf != "api" {
		t.Fatal("separate-directory worktree has incorrect parent")
	}
}

func TestIntegration_WorktreeBareBaseRejected(t *testing.T) {
	testEnv(t)
	root, _, _ := familyFixture(t)
	base := t.TempDir()
	bare, linked := filepath.Join(base, "bare.git"), filepath.Join(base, "linked")
	wtGit(t, base, "clone", "--bare", root, bare)
	wtGit(t, bare, "worktree", "add", "-b", "bare-feature", linked)
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(linked); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(orig) }()
	_, err = runSokoErr(t, "init", "--worktree")
	if err == nil || !strings.Contains(err.Error(), "bare") || len(loadRegistry(t).Repos) != 0 {
		t.Fatalf("bare base registration = %v, %+v", err, loadRegistry(t).Repos)
	}
}
