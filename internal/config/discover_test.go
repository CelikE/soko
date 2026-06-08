package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiscoverEnabled(t *testing.T) {
	tests := []struct {
		name string
		cfg  *Config
		want bool
	}{
		{"nil discover", &Config{}, false},
		{"present but off", &Config{Discover: &DiscoverConfig{Enabled: false}}, false},
		{"present and on", &Config{Discover: &DiscoverConfig{Enabled: true}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.DiscoverEnabled(); got != tt.want {
				t.Errorf("DiscoverEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEnsureDiscover(t *testing.T) {
	cfg := &Config{}
	d := cfg.EnsureDiscover()
	if d == nil || cfg.Discover == nil {
		t.Fatal("EnsureDiscover() did not allocate Discover")
	}
	// Second call returns the same instance.
	d.Enabled = true
	if cfg.EnsureDiscover() != d {
		t.Error("EnsureDiscover() allocated a second instance")
	}
	if !cfg.DiscoverEnabled() {
		t.Error("mutation through EnsureDiscover() was lost")
	}
}

func TestShouldDiscover(t *testing.T) {
	base := t.TempDir()
	repo := filepath.Join(base, "proj")

	tests := []struct {
		name string
		cfg  *Config
		path string
		want bool
	}{
		{
			name: "disabled returns false",
			cfg:  &Config{Discover: &DiscoverConfig{Enabled: false}},
			path: repo,
			want: false,
		},
		{
			name: "nil discover returns false",
			cfg:  &Config{},
			path: repo,
			want: false,
		},
		{
			name: "enabled with no roots allows anywhere",
			cfg:  &Config{Discover: &DiscoverConfig{Enabled: true}},
			path: repo,
			want: true,
		},
		{
			name: "path under a configured root",
			cfg:  &Config{Discover: &DiscoverConfig{Enabled: true, Roots: []string{base}}},
			path: repo,
			want: true,
		},
		{
			name: "path equal to a configured root",
			cfg:  &Config{Discover: &DiscoverConfig{Enabled: true, Roots: []string{repo}}},
			path: repo,
			want: true,
		},
		{
			name: "path outside all roots",
			cfg:  &Config{Discover: &DiscoverConfig{Enabled: true, Roots: []string{filepath.Join(base, "other")}}},
			path: repo,
			want: false,
		},
		{
			name: "root must match on path boundary",
			cfg:  &Config{Discover: &DiscoverConfig{Enabled: true, Roots: []string{filepath.Join(base, "pr")}}},
			path: filepath.Join(base, "proj"),
			want: false,
		},
		{
			name: "built-in node_modules ignore on a segment",
			cfg:  &Config{Discover: &DiscoverConfig{Enabled: true}},
			path: filepath.Join(base, "node_modules", "dep"),
			want: false,
		},
		{
			name: "built-in vendor ignore on a segment",
			cfg:  &Config{Discover: &DiscoverConfig{Enabled: true}},
			path: filepath.Join(base, "vendor", "pkg"),
			want: false,
		},
		{
			name: "user ignore glob on a segment",
			cfg:  &Config{Discover: &DiscoverConfig{Enabled: true, Ignore: []string{"*-scratch"}}},
			path: filepath.Join(base, "demo-scratch"),
			want: false,
		},
		{
			name: "user ignore glob on the full path",
			cfg:  &Config{Discover: &DiscoverConfig{Enabled: true, Ignore: []string{filepath.Join(base, "*")}}},
			path: repo,
			want: false,
		},
		{
			name: "non-matching ignore keeps the path",
			cfg:  &Config{Discover: &DiscoverConfig{Enabled: true, Ignore: []string{"*-scratch"}}},
			path: repo,
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ShouldDiscover(tt.cfg, tt.path); got != tt.want {
				t.Errorf("ShouldDiscover(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestPathWithinRoot(t *testing.T) {
	tests := []struct {
		name string
		path string
		root string
		want bool
	}{
		{"equal", "/home/me/proj", "/home/me/proj", true},
		{"nested", "/home/me/proj/src", "/home/me/proj", true},
		{"sibling prefix is not within", "/home/me/proj", "/home/me/pr", false},
		{"unrelated", "/var/tmp/x", "/home/me", false},
		{"trailing slash on root", "/home/me/proj", "/home/me/", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := PathWithinRoot(tt.path, tt.root); got != tt.want {
				t.Errorf("PathWithinRoot(%q, %q) = %v, want %v", tt.path, tt.root, got, tt.want)
			}
		})
	}
}

func TestDiscoverRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	cfg := &Config{
		Discover: &DiscoverConfig{
			Enabled: true,
			Roots:   []string{"/home/dev/work"},
			Ignore:  []string{"*-scratch"},
			Tags:    []string{"discovered"},
		},
		Repos: []RepoEntry{{Name: "a", Path: "/home/dev/work/a"}},
	}

	if err := SaveTo(cfg, path); err != nil {
		t.Fatalf("SaveTo() error = %v", err)
	}

	loaded, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom() error = %v", err)
	}
	if !loaded.DiscoverEnabled() {
		t.Fatal("round-trip lost discover.enabled")
	}
	if len(loaded.Discover.Roots) != 1 || loaded.Discover.Roots[0] != "/home/dev/work" {
		t.Errorf("round-trip roots = %v, want [/home/dev/work]", loaded.Discover.Roots)
	}
	if len(loaded.Discover.Tags) != 1 || loaded.Discover.Tags[0] != "discovered" {
		t.Errorf("round-trip tags = %v, want [discovered]", loaded.Discover.Tags)
	}
}

func TestSaveToOmitsDiscoverWhenAbsent(t *testing.T) {
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
	if strings.Contains(string(data), "discover") {
		t.Errorf("config for a user who never enabled discovery should not contain a discover block:\n%s", data)
	}
}

func TestSaveToLeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	if err := SaveTo(&Config{}, path); err != nil {
		t.Fatalf("SaveTo() error = %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading dir: %v", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".config-") {
			t.Errorf("atomic SaveTo left a temp file behind: %s", e.Name())
		}
	}
}
