package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/CelikE/soko/internal/config"
)

func saveConfig(t *testing.T, repos ...config.RepoEntry) {
	t.Helper()
	if err := config.Save(&config.Config{Repos: repos}); err != nil {
		t.Fatalf("saving config: %v", err)
	}
}

func TestAnnotateSet(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	saveConfig(t, config.RepoEntry{Name: "api", Path: "/x/api"})

	out, err := runRoot(t, "annotate", "api", "--set", "owner=alice", "--set", "status=active")
	if err != nil {
		t.Fatalf("annotate set: %v", err)
	}
	if !strings.Contains(out, "annotated api (2 keys set)") {
		t.Errorf("confirm = %q", out)
	}

	// Persisted to disk.
	cfg, _ := config.Load()
	if cfg.Repos[0].Meta["owner"] != "alice" || cfg.Repos[0].Meta["status"] != "active" {
		t.Errorf("on-disk meta = %v", cfg.Repos[0].Meta)
	}
}

func TestAnnotateSetInvalidWritesNothing(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	saveConfig(t, config.RepoEntry{Name: "api", Path: "/x/api"})

	_, err := runRoot(t, "annotate", "api", "--set", "owneralice")
	if err == nil || !strings.Contains(err.Error(), "expected key=value") {
		t.Fatalf("annotate set invalid err = %v, want usage error", err)
	}
	cfg, _ := config.Load()
	if cfg.Repos[0].Meta != nil {
		t.Errorf("meta written despite invalid --set: %v", cfg.Repos[0].Meta)
	}
}

func TestAnnotateShow(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	saveConfig(t, config.RepoEntry{
		Name: "api", Path: "/x/api",
		Meta: map[string]string{"owner": "alice", "priority": "high", "status": "active"},
	})

	out, err := runRoot(t, "annotate", "api")
	if err != nil {
		t.Fatalf("annotate show: %v", err)
	}
	// Sorted by key: owner before priority before status.
	if i, j, k := strings.Index(out, "owner"), strings.Index(out, "priority"), strings.Index(out, "status"); i >= j || j >= k {
		t.Errorf("keys not sorted in output:\n%s", out)
	}

	out, err = runRoot(t, "annotate", "api", "--json")
	if err != nil {
		t.Fatalf("annotate show --json: %v", err)
	}
	var got annotateJSON
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal %q: %v", out, err)
	}
	if got.Name != "api" || got.Meta["owner"] != "alice" {
		t.Errorf("json = %+v", got)
	}
}

func TestAnnotateUnsetAndClear(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	saveConfig(t, config.RepoEntry{
		Name: "api", Path: "/x/api",
		Meta: map[string]string{"owner": "alice", "status": "active"},
	})

	if _, err := runRoot(t, "annotate", "api", "--unset", "owner"); err != nil {
		t.Fatalf("annotate unset: %v", err)
	}
	cfg, _ := config.Load()
	if _, ok := cfg.Repos[0].Meta["owner"]; ok {
		t.Error("owner still present after unset")
	}

	// Unsetting an absent key is a no-op.
	if _, err := runRoot(t, "annotate", "api", "--unset", "ghost"); err != nil {
		t.Errorf("annotate unset absent err = %v, want nil", err)
	}

	if _, err := runRoot(t, "annotate", "api", "--clear"); err != nil {
		t.Fatalf("annotate clear: %v", err)
	}
	cfg, _ = config.Load()
	if cfg.Repos[0].Meta != nil {
		t.Errorf("meta = %v after clear, want nil", cfg.Repos[0].Meta)
	}
}

func TestAnnotateMutuallyExclusive(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	saveConfig(t, config.RepoEntry{Name: "api", Path: "/x/api"})

	_, err := runRoot(t, "annotate", "api", "--set", "owner=alice", "--unset", "status")
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("err = %v, want mutually-exclusive error", err)
	}
}

func TestAnnotateUnknownRepo(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	saveConfig(t, config.RepoEntry{Name: "api", Path: "/x/api"})

	_, err := runRoot(t, "annotate", "nope", "--set", "owner=alice")
	if err == nil || err.Error() != "not found: nope" {
		t.Errorf("err = %v, want not found: nope", err)
	}
}

func TestAnnotateList(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	saveConfig(t,
		config.RepoEntry{Name: "api", Path: "/x/api", Meta: map[string]string{"owner": "alice", "status": "active"}},
		config.RepoEntry{Name: "legacy", Path: "/x/legacy", Meta: map[string]string{"status": "archived"}},
		config.RepoEntry{Name: "plain", Path: "/x/plain"},
	)

	out, err := runRoot(t, "annotate", "--list")
	if err != nil {
		t.Fatalf("annotate --list: %v", err)
	}
	if !strings.Contains(out, "api") || !strings.Contains(out, "legacy") {
		t.Errorf("list missing annotated repos:\n%s", out)
	}
	if strings.Contains(out, "plain") {
		t.Errorf("list should exclude un-annotated repo:\n%s", out)
	}
	if !strings.Contains(out, "2 repos annotated") {
		t.Errorf("list missing footer:\n%s", out)
	}

	out, err = runRoot(t, "annotate", "--list", "--json")
	if err != nil {
		t.Fatalf("annotate --list --json: %v", err)
	}
	var entries []annotateJSON
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		t.Fatalf("unmarshal %q: %v", out, err)
	}
	if len(entries) != 2 {
		t.Errorf("list json has %d entries, want 2", len(entries))
	}
}

func TestListMetaFilter(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	saveConfig(t,
		config.RepoEntry{Name: "api", Path: "/x/api", Meta: map[string]string{"status": "active", "priority": "high"}},
		config.RepoEntry{Name: "web", Path: "/x/web", Meta: map[string]string{"status": "active", "priority": "low"}},
		config.RepoEntry{Name: "legacy", Path: "/x/legacy", Meta: map[string]string{"status": "archived"}},
	)

	out, err := runRoot(t, "list", "--meta", "status=active", "--json")
	if err != nil {
		t.Fatalf("list --meta: %v", err)
	}
	var entries []listEntry
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2 (api, web)", len(entries))
	}

	// Repeated --meta combines with AND.
	out, _ = runRoot(t, "list", "--meta", "status=active", "--meta", "priority=high", "--json")
	_ = json.Unmarshal([]byte(out), &entries)
	if len(entries) != 1 || entries[0].Name != "api" {
		t.Errorf("AND filter = %v, want [api]", entries)
	}

	// Malformed --meta is a usage error.
	if _, err := runRoot(t, "list", "--meta", "status"); err == nil {
		t.Error("list --meta status (no =) should be a usage error")
	}
}

func TestStatusMetaFilter(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	saveConfig(t,
		config.RepoEntry{Name: "api", Path: "/x/api", Meta: map[string]string{"status": "active"}},
		config.RepoEntry{Name: "legacy", Path: "/x/legacy", Meta: map[string]string{"status": "archived"}},
	)

	// The meta filter narrows the set before git collection; paths don't exist
	// so rows are error rows, but only the matching repo should appear.
	out, err := runRoot(t, "status", "--meta", "status=active", "--json")
	if err != nil {
		t.Fatalf("status --meta: %v", err)
	}
	if !strings.Contains(out, "api") || strings.Contains(out, "legacy") {
		t.Errorf("status --meta did not narrow to api:\n%s", out)
	}
}
