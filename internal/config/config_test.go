package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestPath(t *testing.T) {
	tests := []struct {
		name    string
		xdgHome string
		wantEnd string
	}{
		{
			name:    "uses XDG_CONFIG_HOME when set",
			xdgHome: "/custom/config",
			wantEnd: "/custom/config/soko/config.yaml",
		},
		{
			name:    "falls back to home .config when XDG unset",
			xdgHome: "",
			wantEnd: ".config/soko/config.yaml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", tt.xdgHome)

			got, err := Path()
			if err != nil {
				t.Fatalf("Path() error = %v", err)
			}

			if tt.xdgHome != "" {
				if got != tt.wantEnd {
					t.Errorf("Path() = %q, want %q", got, tt.wantEnd)
				}
			} else {
				if filepath.Base(filepath.Dir(got)) != "soko" {
					t.Errorf("Path() = %q, want parent dir to be 'soko'", got)
				}
				if filepath.Base(got) != "config.yaml" {
					t.Errorf("Path() = %q, want filename 'config.yaml'", got)
				}
			}
		})
	}
}

func TestLoadFrom_FileNotExist(t *testing.T) {
	cfg, err := LoadFrom(filepath.Join(t.TempDir(), "nonexistent.yaml"))
	if err != nil {
		t.Fatalf("LoadFrom() error = %v, want nil", err)
	}
	if len(cfg.Repos) != 0 {
		t.Errorf("LoadFrom() repos = %d, want 0", len(cfg.Repos))
	}
}

func TestLoadFrom_ValidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	content := `repos:
  - name: my-repo
    path: /home/dev/my-repo
  - name: other-repo
    path: /home/dev/other-repo
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing test file: %v", err)
	}

	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom() error = %v", err)
	}

	if len(cfg.Repos) != 2 {
		t.Fatalf("LoadFrom() repos = %d, want 2", len(cfg.Repos))
	}

	if cfg.Repos[0].Name != "my-repo" {
		t.Errorf("Repos[0].Name = %q, want %q", cfg.Repos[0].Name, "my-repo")
	}
	if cfg.Repos[0].Path != "/home/dev/my-repo" {
		t.Errorf("Repos[0].Path = %q, want %q", cfg.Repos[0].Path, "/home/dev/my-repo")
	}
	if cfg.Repos[1].Name != "other-repo" {
		t.Errorf("Repos[1].Name = %q, want %q", cfg.Repos[1].Name, "other-repo")
	}
}

func TestLoadFrom_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	if err := os.WriteFile(path, []byte(":\x00not valid"), 0o644); err != nil {
		t.Fatalf("writing test file: %v", err)
	}

	_, err := LoadFrom(path)
	if err == nil {
		t.Fatal("LoadFrom() error = nil, want error for invalid YAML")
	}
}

func TestSaveTo_CreatesDirectoryAndFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "dir", "config.yaml")

	cfg := &Config{
		Repos: []RepoEntry{
			{Name: "test-repo", Path: "/tmp/test-repo"},
		},
	}

	if err := SaveTo(cfg, path); err != nil {
		t.Fatalf("SaveTo() error = %v", err)
	}

	loaded, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom() after save error = %v", err)
	}

	if len(loaded.Repos) != 1 {
		t.Fatalf("loaded repos = %d, want 1", len(loaded.Repos))
	}
	if loaded.Repos[0].Name != "test-repo" {
		t.Errorf("loaded Repos[0].Name = %q, want %q", loaded.Repos[0].Name, "test-repo")
	}
	if loaded.Repos[0].Path != "/tmp/test-repo" {
		t.Errorf("loaded Repos[0].Path = %q, want %q", loaded.Repos[0].Path, "/tmp/test-repo")
	}
}

func TestSaveTo_EmptyConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	cfg := &Config{}

	if err := SaveTo(cfg, path); err != nil {
		t.Fatalf("SaveTo() error = %v", err)
	}

	loaded, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom() after save error = %v", err)
	}

	if len(loaded.Repos) != 0 {
		t.Errorf("loaded repos = %d, want 0", len(loaded.Repos))
	}
}

func TestAddRepo(t *testing.T) {
	tests := []struct {
		name    string
		initial []RepoEntry
		entry   RepoEntry
		wantLen int
		wantErr error
	}{
		{
			name:    "adds to empty config",
			initial: nil,
			entry:   RepoEntry{Name: "my-repo", Path: "/home/dev/my-repo"},
			wantLen: 1,
			wantErr: nil,
		},
		{
			name: "adds to existing config",
			initial: []RepoEntry{
				{Name: "existing", Path: "/home/dev/existing"},
			},
			entry:   RepoEntry{Name: "new-repo", Path: "/home/dev/new-repo"},
			wantLen: 2,
			wantErr: nil,
		},
		{
			name: "rejects duplicate path",
			initial: []RepoEntry{
				{Name: "my-repo", Path: "/home/dev/my-repo"},
			},
			entry:   RepoEntry{Name: "different-name", Path: "/home/dev/my-repo"},
			wantLen: 1,
			wantErr: ErrRepoAlreadyExists,
		},
		{
			name: "allows same name different path",
			initial: []RepoEntry{
				{Name: "my-repo", Path: "/home/dev/my-repo"},
			},
			entry:   RepoEntry{Name: "my-repo", Path: "/home/dev/other-location"},
			wantLen: 2,
			wantErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{Repos: tt.initial}

			result, err := AddRepo(cfg, tt.entry)

			if !errors.Is(err, tt.wantErr) {
				t.Errorf("AddRepo() error = %v, want %v", err, tt.wantErr)
			}
			if len(result.Repos) != tt.wantLen {
				t.Errorf("AddRepo() repos = %d, want %d", len(result.Repos), tt.wantLen)
			}
		})
	}
}

func TestRemoveRepo(t *testing.T) {
	tests := []struct {
		name       string
		initial    []RepoEntry
		removeName string
		wantLen    int
		wantErr    error
	}{
		{
			name: "removes existing repo",
			initial: []RepoEntry{
				{Name: "alpha", Path: "/repos/alpha"},
				{Name: "beta", Path: "/repos/beta"},
			},
			removeName: "alpha",
			wantLen:    1,
			wantErr:    nil,
		},
		{
			name: "returns error for non-existent name",
			initial: []RepoEntry{
				{Name: "alpha", Path: "/repos/alpha"},
			},
			removeName: "missing",
			wantLen:    1,
			wantErr:    ErrRepoNotFound,
		},
		{
			name:       "returns error on empty config",
			initial:    nil,
			removeName: "anything",
			wantLen:    0,
			wantErr:    ErrRepoNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{Repos: tt.initial}

			result, removed, err := RemoveRepo(cfg, tt.removeName)

			if !errors.Is(err, tt.wantErr) {
				t.Errorf("RemoveRepo() error = %v, want %v", err, tt.wantErr)
			}
			if len(result.Repos) != tt.wantLen {
				t.Errorf("RemoveRepo() repos = %d, want %d", len(result.Repos), tt.wantLen)
			}
			if err == nil && removed.Name != tt.removeName {
				t.Errorf("RemoveRepo() removed.Name = %q, want %q", removed.Name, tt.removeName)
			}
		})
	}
}

func TestRemoveRepoByPath(t *testing.T) {
	tests := []struct {
		name       string
		initial    []RepoEntry
		removePath string
		wantLen    int
		wantErr    error
	}{
		{
			name: "removes existing repo by path",
			initial: []RepoEntry{
				{Name: "alpha", Path: "/repos/alpha"},
				{Name: "beta", Path: "/repos/beta"},
			},
			removePath: "/repos/beta",
			wantLen:    1,
			wantErr:    nil,
		},
		{
			name: "returns error for non-existent path",
			initial: []RepoEntry{
				{Name: "alpha", Path: "/repos/alpha"},
			},
			removePath: "/repos/missing",
			wantLen:    1,
			wantErr:    ErrRepoNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{Repos: tt.initial}

			result, removed, err := RemoveRepoByPath(cfg, tt.removePath)

			if !errors.Is(err, tt.wantErr) {
				t.Errorf("RemoveRepoByPath() error = %v, want %v", err, tt.wantErr)
			}
			if len(result.Repos) != tt.wantLen {
				t.Errorf("RemoveRepoByPath() repos = %d, want %d", len(result.Repos), tt.wantLen)
			}
			if err == nil && removed.Path != tt.removePath {
				t.Errorf("RemoveRepoByPath() removed.Path = %q, want %q", removed.Path, tt.removePath)
			}
		})
	}
}

func TestClear(t *testing.T) {
	cfg := &Config{
		Repos: []RepoEntry{
			{Name: "alpha", Path: "/repos/alpha"},
			{Name: "beta", Path: "/repos/beta"},
		},
	}

	result := Clear(cfg)

	if len(result.Repos) != 0 {
		t.Errorf("Clear() repos = %d, want 0", len(result.Repos))
	}
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	original := &Config{
		Repos: []RepoEntry{
			{Name: "alpha", Path: "/repos/alpha"},
			{Name: "beta", Path: "/repos/beta"},
			{Name: "gamma", Path: "/repos/gamma"},
		},
	}

	if err := SaveTo(original, path); err != nil {
		t.Fatalf("SaveTo() error = %v", err)
	}

	loaded, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom() error = %v", err)
	}

	if len(loaded.Repos) != len(original.Repos) {
		t.Fatalf("round-trip repos = %d, want %d", len(loaded.Repos), len(original.Repos))
	}

	for i, want := range original.Repos {
		got := loaded.Repos[i]
		if got.Name != want.Name || got.Path != want.Path {
			t.Errorf("Repos[%d] = {%q, %q}, want {%q, %q}", i, got.Name, got.Path, want.Name, want.Path)
		}
	}
}

func TestFindRepo(t *testing.T) {
	cfg := &Config{
		Repos: []RepoEntry{
			{Name: "auth-service", Path: "/repos/auth-service"},
			{Name: "auth-worker", Path: "/repos/auth-worker"},
			{Name: "backend-api", Path: "/repos/backend-api"},
			{Name: "frontend", Path: "/repos/frontend"},
		},
	}

	tests := []struct {
		name      string
		query     string
		wantNames []string
	}{
		{
			name:      "exact match",
			query:     "frontend",
			wantNames: []string{"frontend"},
		},
		{
			name:      "exact match takes priority over prefix",
			query:     "auth-service",
			wantNames: []string{"auth-service"},
		},
		{
			name:      "single prefix match",
			query:     "back",
			wantNames: []string{"backend-api"},
		},
		{
			name:      "multiple prefix matches",
			query:     "auth",
			wantNames: []string{"auth-service", "auth-worker"},
		},
		{
			name:      "no match",
			query:     "infra",
			wantNames: nil,
		},
		{
			name:      "empty query matches all as prefix",
			query:     "",
			wantNames: []string{"auth-service", "auth-worker", "backend-api", "frontend"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FindRepo(cfg, tt.query)

			if len(got) != len(tt.wantNames) {
				t.Fatalf("FindRepo(%q) returned %d results, want %d", tt.query, len(got), len(tt.wantNames))
			}

			for i, want := range tt.wantNames {
				if got[i].Name != want {
					t.Errorf("FindRepo(%q)[%d].Name = %q, want %q", tt.query, i, got[i].Name, want)
				}
			}
		})
	}
}

func TestAddTag(t *testing.T) {
	tests := []struct {
		name     string
		repoName string
		tag      string
		wantTags []string
		wantErr  error
	}{
		{
			name:     "adds tag to repo with no tags",
			repoName: "alpha",
			tag:      "backend",
			wantTags: []string{"backend"},
		},
		{
			name:     "adds second tag",
			repoName: "alpha",
			tag:      "go",
			wantTags: []string{"backend", "go"},
		},
		{
			name:     "duplicate tag is no-op",
			repoName: "alpha",
			tag:      "backend",
			wantTags: []string{"backend", "go"},
		},
		{
			name:     "normalizes to lowercase",
			repoName: "alpha",
			tag:      "GO",
			wantTags: []string{"backend", "go"},
		},
		{
			name:     "repo not found",
			repoName: "missing",
			tag:      "x",
			wantErr:  ErrRepoNotFound,
		},
	}

	cfg := &Config{Repos: []RepoEntry{
		{Name: "alpha", Path: "/repos/alpha"},
	}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := AddTag(cfg, tt.repoName, tt.tag)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("AddTag() error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr == nil {
				got := cfg.Repos[0].Tags
				if len(got) != len(tt.wantTags) {
					t.Fatalf("tags = %v, want %v", got, tt.wantTags)
				}
				for i, want := range tt.wantTags {
					if got[i] != want {
						t.Errorf("tags[%d] = %q, want %q", i, got[i], want)
					}
				}
			}
		})
	}
}

func TestRemoveTag(t *testing.T) {
	cfg := &Config{Repos: []RepoEntry{
		{Name: "alpha", Path: "/repos/alpha", Tags: []string{"backend", "go", "auth"}},
	}}

	// Remove existing tag.
	if _, err := RemoveTag(cfg, "alpha", "go"); err != nil {
		t.Fatalf("RemoveTag() error = %v", err)
	}
	if len(cfg.Repos[0].Tags) != 2 {
		t.Fatalf("tags = %v, want 2 tags", cfg.Repos[0].Tags)
	}

	// Remove non-existent tag is no-op.
	if _, err := RemoveTag(cfg, "alpha", "nonexistent"); err != nil {
		t.Fatalf("RemoveTag() non-existent error = %v", err)
	}
	if len(cfg.Repos[0].Tags) != 2 {
		t.Fatalf("tags after no-op = %v, want 2 tags", cfg.Repos[0].Tags)
	}

	// Repo not found.
	if _, err := RemoveTag(cfg, "missing", "x"); !errors.Is(err, ErrRepoNotFound) {
		t.Errorf("RemoveTag() missing repo error = %v, want ErrRepoNotFound", err)
	}
}

func TestListTags(t *testing.T) {
	cfg := &Config{Repos: []RepoEntry{
		{Name: "a", Path: "/a", Tags: []string{"go", "backend"}},
		{Name: "b", Path: "/b", Tags: []string{"react", "frontend"}},
		{Name: "c", Path: "/c", Tags: []string{"go", "frontend"}},
	}}

	got := ListTags(cfg)
	want := []string{"backend", "frontend", "go", "react"}

	if len(got) != len(want) {
		t.Fatalf("ListTags() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("ListTags()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestFilterByTags(t *testing.T) {
	repos := []RepoEntry{
		{Name: "a", Path: "/a", Tags: []string{"backend", "go"}},
		{Name: "b", Path: "/b", Tags: []string{"frontend", "react"}},
		{Name: "c", Path: "/c", Tags: []string{"backend", "infra"}},
		{Name: "d", Path: "/d"},
	}

	tests := []struct {
		name      string
		tags      []string
		wantNames []string
	}{
		{
			name:      "single tag",
			tags:      []string{"backend"},
			wantNames: []string{"a", "c"},
		},
		{
			name:      "multiple tags OR",
			tags:      []string{"frontend", "infra"},
			wantNames: []string{"b", "c"},
		},
		{
			name:      "no matches",
			tags:      []string{"python"},
			wantNames: nil,
		},
		{
			name:      "repos without tags never match",
			tags:      []string{"backend"},
			wantNames: []string{"a", "c"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FilterByTags(repos, tt.tags)
			if len(got) != len(tt.wantNames) {
				t.Fatalf("FilterByTags(%v) = %d results, want %d", tt.tags, len(got), len(tt.wantNames))
			}
			for i, want := range tt.wantNames {
				if got[i].Name != want {
					t.Errorf("FilterByTags(%v)[%d].Name = %q, want %q", tt.tags, i, got[i].Name, want)
				}
			}
		})
	}
}

func TestSaveAndLoadRoundTripWithTags(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	original := &Config{Repos: []RepoEntry{
		{Name: "a", Path: "/repos/a", Tags: []string{"backend", "go"}},
		{Name: "b", Path: "/repos/b"},
	}}

	if err := SaveTo(original, path); err != nil {
		t.Fatalf("SaveTo() error = %v", err)
	}

	loaded, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom() error = %v", err)
	}

	if len(loaded.Repos[0].Tags) != 2 {
		t.Errorf("round-trip tags = %v, want [backend go]", loaded.Repos[0].Tags)
	}
	if len(loaded.Repos[1].Tags) != 0 {
		t.Errorf("round-trip empty tags = %v, want []", loaded.Repos[1].Tags)
	}
}
