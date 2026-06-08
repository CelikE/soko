package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetMeta(t *testing.T) {
	cfg := &Config{Repos: []RepoEntry{{Name: "api", Path: "/x/api"}}}

	// First set allocates the map.
	cfg, err := SetMeta(cfg, "api", "owner", "alice")
	if err != nil {
		t.Fatalf("SetMeta() error = %v", err)
	}
	if cfg.Repos[0].Meta["owner"] != "alice" {
		t.Errorf("owner = %q, want alice", cfg.Repos[0].Meta["owner"])
	}

	// Second set upserts and normalizes the key (Owner == owner).
	cfg, err = SetMeta(cfg, "api", "Owner", "bob")
	if err != nil {
		t.Fatalf("SetMeta() error = %v", err)
	}
	if cfg.Repos[0].Meta["owner"] != "bob" {
		t.Errorf("owner = %q, want bob (key normalized)", cfg.Repos[0].Meta["owner"])
	}
	if len(cfg.Repos[0].Meta) != 1 {
		t.Errorf("meta has %d keys, want 1", len(cfg.Repos[0].Meta))
	}
}

func TestMetaUnknownRepo(t *testing.T) {
	cfg := &Config{Repos: []RepoEntry{{Name: "api", Path: "/x/api"}}}

	if _, err := SetMeta(cfg, "nope", "owner", "alice"); !errors.Is(err, ErrRepoNotFound) {
		t.Errorf("SetMeta unknown = %v, want ErrRepoNotFound", err)
	}
	if _, err := UnsetMeta(cfg, "nope", "owner"); !errors.Is(err, ErrRepoNotFound) {
		t.Errorf("UnsetMeta unknown = %v, want ErrRepoNotFound", err)
	}
	if _, err := ClearMeta(cfg, "nope"); !errors.Is(err, ErrRepoNotFound) {
		t.Errorf("ClearMeta unknown = %v, want ErrRepoNotFound", err)
	}
}

func TestUnsetMetaClearsMapWhenEmpty(t *testing.T) {
	cfg := &Config{Repos: []RepoEntry{{Name: "api", Path: "/x/api", Meta: map[string]string{"owner": "alice"}}}}

	cfg, err := UnsetMeta(cfg, "api", "owner")
	if err != nil {
		t.Fatalf("UnsetMeta() error = %v", err)
	}
	if cfg.Repos[0].Meta != nil {
		t.Errorf("meta = %v, want nil after removing last key", cfg.Repos[0].Meta)
	}

	// Unsetting an absent key is a no-op, not an error.
	if _, err := UnsetMeta(cfg, "api", "ghost"); err != nil {
		t.Errorf("UnsetMeta absent key error = %v, want nil", err)
	}
}

func TestClearMeta(t *testing.T) {
	cfg := &Config{Repos: []RepoEntry{
		{Name: "api", Path: "/x/api", Meta: map[string]string{"owner": "alice", "status": "active"}},
		{Name: "web", Path: "/x/web", Meta: map[string]string{"owner": "bob"}},
	}}

	cfg, err := ClearMeta(cfg, "api")
	if err != nil {
		t.Fatalf("ClearMeta() error = %v", err)
	}
	if cfg.Repos[0].Meta != nil {
		t.Errorf("api meta = %v, want nil", cfg.Repos[0].Meta)
	}
	// Clear touches only the named repo.
	if cfg.Repos[1].Meta["owner"] != "bob" {
		t.Errorf("web meta was disturbed: %v", cfg.Repos[1].Meta)
	}
}

func TestFilterByMeta(t *testing.T) {
	repos := []RepoEntry{
		{Name: "api", Meta: map[string]string{"status": "active", "priority": "high"}},
		{Name: "web", Meta: map[string]string{"status": "active", "priority": "low"}},
		{Name: "legacy", Meta: map[string]string{"status": "archived"}},
		{Name: "scratch"},
	}

	tests := []struct {
		name        string
		constraints map[string]string
		want        []string
	}{
		{"single constraint", map[string]string{"status": "active"}, []string{"api", "web"}},
		{"AND across keys", map[string]string{"status": "active", "priority": "high"}, []string{"api"}},
		{"value mismatch", map[string]string{"status": "paused"}, nil},
		{"key normalization", map[string]string{"STATUS": "archived"}, []string{"legacy"}},
		{"empty constraints returns all", map[string]string{}, []string{"api", "web", "legacy", "scratch"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FilterByMeta(repos, tt.constraints)
			names := make([]string, len(got))
			for i, r := range got {
				names[i] = r.Name
			}
			if strings.Join(names, ",") != strings.Join(tt.want, ",") {
				t.Errorf("FilterByMeta() = %v, want %v", names, tt.want)
			}
		})
	}
}

func TestMetaRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	cfg := &Config{Repos: []RepoEntry{
		{Name: "api", Path: "/x/api", Meta: map[string]string{"owner": "alice", "status": "active"}},
		{Name: "scratch", Path: "/x/scratch"},
	}}
	if err := SaveTo(cfg, path); err != nil {
		t.Fatalf("SaveTo() error = %v", err)
	}

	loaded, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom() error = %v", err)
	}
	if loaded.Repos[0].Meta["owner"] != "alice" || loaded.Repos[0].Meta["status"] != "active" {
		t.Errorf("round-trip meta = %v", loaded.Repos[0].Meta)
	}
	if loaded.Repos[1].Meta != nil {
		t.Errorf("scratch meta = %v, want nil", loaded.Repos[1].Meta)
	}
}

func TestSaveToOmitsMetaWhenAbsent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	cfg := &Config{Repos: []RepoEntry{{Name: "a", Path: "/x/a"}}}
	if err := SaveTo(cfg, path); err != nil {
		t.Fatalf("SaveTo() error = %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading config: %v", err)
	}
	if strings.Contains(string(data), "meta") {
		t.Errorf("a repo with no annotations must not write a meta block:\n%s", data)
	}
}
