package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/CelikE/soko/internal/config"
)

// runRoot executes a full soko command line against an isolated config and
// returns stdout. The caller must have pointed XDG_CONFIG_HOME at a temp dir.
func runRoot(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var stdout bytes.Buffer
	cmd := NewRootCmd("test")
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs(args)
	err := cmd.Execute()
	return stdout.String(), err
}

func TestConfigPathJSON(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	wantPath, err := config.Path()
	if err != nil {
		t.Fatalf("config.Path: %v", err)
	}

	out, err := runRoot(t, "config", "path", "--json")
	if err != nil {
		t.Fatalf("config path --json: %v", err)
	}
	var got configPathJSON
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal %q: %v", out, err)
	}
	if got.Path != wantPath {
		t.Errorf("path = %q, want %q", got.Path, wantPath)
	}

	// Without --json: bare path, no JSON braces.
	out, err = runRoot(t, "config", "path")
	if err != nil {
		t.Fatalf("config path: %v", err)
	}
	if strings.Contains(out, "{") {
		t.Errorf("text output contains JSON braces: %q", out)
	}
	if strings.TrimSpace(out) != wantPath {
		t.Errorf("text path = %q, want %q", strings.TrimSpace(out), wantPath)
	}
}

func TestConfigGetJSON(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	out, err := runRoot(t, "config", "get", "git_path", "--json")
	if err != nil {
		t.Fatalf("config get --json: %v", err)
	}
	var got configGetJSON
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal %q: %v", out, err)
	}
	if got.Key != "git_path" || got.Value != "git (default)" {
		t.Errorf("got %+v, want {git_path git (default)}", got)
	}
}

func TestConfigGetUnknownJSON(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	out, err := runRoot(t, "config", "get", "nope", "--json")
	if err == nil {
		t.Fatal("config get nope --json: error = nil, want unknown-key error")
	}
	if strings.TrimSpace(out) != "" {
		t.Errorf("error path produced stdout: %q", out)
	}
}

func TestConfigSetJSON(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	exe := testExecutable(t)

	out, err := runRoot(t, "config", "set", "git_path", exe, "--json")
	if err != nil {
		t.Fatalf("config set --json: %v", err)
	}
	var got configSetJSON
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal %q: %v", out, err)
	}
	if got.Key != "git_path" || got.Value != exe || got.Previous != "git (default)" {
		t.Errorf("got %+v, want {git_path %s git (default)}", got, exe)
	}

	// The config file on disk actually changed.
	path, _ := config.Path()
	cfg, err := config.LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if cfg.GitPath != exe {
		t.Errorf("on-disk git_path = %q, want %q", cfg.GitPath, exe)
	}

	// A second set reports the prior value as previous.
	out, err = runRoot(t, "config", "set", "git_path", exe, "--json")
	if err != nil {
		t.Fatalf("second set: %v", err)
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal second set %q: %v", out, err)
	}
	if got.Previous != exe {
		t.Errorf("second set previous = %q, want %q", got.Previous, exe)
	}
}

func TestConfigSetInvalidWritesNothing(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	out, err := runRoot(t, "config", "set", "git_path", "/no/such/bin", "--json")
	if err == nil {
		t.Fatal("config set invalid: error = nil, want validation error")
	}
	if strings.TrimSpace(out) != "" {
		t.Errorf("error path produced stdout: %q", out)
	}

	// Config file unchanged (git_path stays empty / default).
	path, _ := config.Path()
	cfg, _ := config.LoadFrom(path)
	if cfg.GitPath != "" {
		t.Errorf("on-disk git_path = %q, want empty after failed set", cfg.GitPath)
	}
}

func TestConfigListText(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	out, err := runRoot(t, "config", "list")
	if err != nil {
		t.Fatalf("config list: %v", err)
	}
	for _, want := range []string{"git (default)", "0 aliases", "off", "0 repos"} {
		if !strings.Contains(out, want) {
			t.Errorf("config list output missing %q:\n%s", want, out)
		}
	}
}

func TestConfigListJSON_Empty(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	out, err := runRoot(t, "config", "list", "--json")
	if err != nil {
		t.Fatalf("config list --json: %v", err)
	}
	var got configListJSON
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal %q: %v", out, err)
	}
	if got.GitPath != "git (default)" {
		t.Errorf("git_path = %q, want git (default)", got.GitPath)
	}
	if got.RepoCount != 0 {
		t.Errorf("repo_count = %d, want 0", got.RepoCount)
	}
	if got.Discover != nil {
		t.Errorf("discover = %+v, want nil (omitted when never enabled)", got.Discover)
	}
	// The discover block must be absent from the raw JSON.
	if strings.Contains(out, "discover") {
		t.Errorf("raw JSON should omit discover block:\n%s", out)
	}
	// With no aliases, the omitempty map drops out of the JSON entirely.
	if len(got.Aliases) != 0 {
		t.Errorf("aliases = %v, want empty", got.Aliases)
	}
	if strings.Contains(out, "aliases") {
		t.Errorf("raw JSON should omit empty aliases block:\n%s", out)
	}
}

// TestConfigListJSON_DiscoverDisabled covers the case where discovery was
// configured then turned off: cfg.Discover is non-nil but Enabled is false, so
// the block is still present (with enabled:false), unlike the never-configured
// case where it is omitted entirely.
func TestConfigListJSON_DiscoverDisabled(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	cfg := &config.Config{
		Discover: &config.DiscoverConfig{
			Enabled: false,
			Roots:   []string{"/Users/me/work"},
		},
	}
	if err := config.Save(cfg); err != nil {
		t.Fatalf("seeding config: %v", err)
	}

	out, err := runRoot(t, "config", "list", "--json")
	if err != nil {
		t.Fatalf("config list --json: %v", err)
	}
	var got configListJSON
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal %q: %v", out, err)
	}
	if got.Discover == nil {
		t.Fatal("discover = nil, want present-but-disabled block")
	}
	if got.Discover.Enabled {
		t.Error("discover.enabled = true, want false")
	}
	if len(got.Discover.Roots) != 1 || got.Discover.Roots[0] != "/Users/me/work" {
		t.Errorf("discover roots = %v, want [/Users/me/work]", got.Discover.Roots)
	}

	// Text view shows discover off even though the block is configured.
	text, err := runRoot(t, "config", "list")
	if err != nil {
		t.Fatalf("config list text: %v", err)
	}
	if !strings.Contains(text, "off") {
		t.Errorf("text discover line should show off:\n%s", text)
	}
}

func TestConfigListJSON_Populated(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	cfg := &config.Config{
		Aliases: map[string]string{"morning": "pull --tag work"},
		Discover: &config.DiscoverConfig{
			Enabled: true,
			Roots:   []string{"/Users/me/work"},
			Tags:    []string{"discovered"},
		},
		Repos: []config.RepoEntry{
			{Name: "a", Path: "/tmp/a"},
			{Name: "b", Path: "/tmp/b"},
		},
	}
	if err := config.Save(cfg); err != nil {
		t.Fatalf("seeding config: %v", err)
	}

	out, err := runRoot(t, "config", "list", "--json")
	if err != nil {
		t.Fatalf("config list --json: %v", err)
	}
	var got configListJSON
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal %q: %v", out, err)
	}
	if got.RepoCount != 2 {
		t.Errorf("repo_count = %d, want 2", got.RepoCount)
	}
	if got.Aliases["morning"] != "pull --tag work" {
		t.Errorf("aliases = %v", got.Aliases)
	}
	if got.Discover == nil || !got.Discover.Enabled {
		t.Fatalf("discover = %+v, want enabled", got.Discover)
	}
	if len(got.Discover.Roots) != 1 || got.Discover.Roots[0] != "/Users/me/work" {
		t.Errorf("discover roots = %v", got.Discover.Roots)
	}

	// Text view reflects the populated counts.
	text, err := runRoot(t, "config", "list")
	if err != nil {
		t.Fatalf("config list text: %v", err)
	}
	for _, want := range []string{"1 alias", "2 repos", "on", "discovered"} {
		if !strings.Contains(text, want) {
			t.Errorf("text config list missing %q:\n%s", want, text)
		}
	}
}

// testExecutable returns the path to a real regular executable usable as a
// git_path value in tests.
func testExecutable(t *testing.T) string {
	t.Helper()
	for _, p := range []string{"/bin/sh", "/usr/bin/true", "/bin/echo"} {
		if config.ValidateGitPath(p) == nil {
			return p
		}
	}
	t.Skip("no suitable executable found for git_path test")
	return ""
}
