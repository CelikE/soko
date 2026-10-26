package config

import (
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestNameMutationsRejectDuplicateNames(t *testing.T) {
	newConfig := func() *Config {
		return &Config{Repos: []RepoEntry{
			{Name: "api", Path: "/work/api", Tags: []string{"backend"}, Meta: map[string]string{"owner": "alice"}},
			{Name: "web", Path: "/work/web"},
			{Name: "api", Path: "/oss/api", Tags: []string{"public"}, Meta: map[string]string{"owner": "bob"}},
		}}
	}
	for _, tt := range []struct {
		name   string
		mutate func(*Config) (*Config, error)
	}{
		{"remove repo", func(cfg *Config) (*Config, error) {
			cfg, _, err := RemoveRepo(cfg, "api")
			return cfg, err
		}},
		{"add tag", func(cfg *Config) (*Config, error) { return AddTag(cfg, "api", "new") }},
		{"add existing tag", func(cfg *Config) (*Config, error) { return AddTag(cfg, "api", "backend") }},
		{"remove tag", func(cfg *Config) (*Config, error) { return RemoveTag(cfg, "api", "backend") }},
		{"set metadata", func(cfg *Config) (*Config, error) { return SetMeta(cfg, "api", "owner", "carol") }},
		{"unset metadata", func(cfg *Config) (*Config, error) { return UnsetMeta(cfg, "api", "owner") }},
		{"clear metadata", func(cfg *Config) (*Config, error) { return ClearMeta(cfg, "api") }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cfg := newConfig()
			result, err := tt.mutate(cfg)
			if !errors.Is(err, ErrAmbiguousRepo) {
				t.Errorf("error = %v, want ErrAmbiguousRepo", err)
			} else {
				for _, want := range []string{"api", "/work/api", "/oss/api", "soko config edit"} {
					if !strings.Contains(err.Error(), want) {
						t.Errorf("error = %q, want %q", err, want)
					}
				}
			}
			if !reflect.DeepEqual(cfg, newConfig()) || result != cfg {
				t.Errorf("failed mutation changed config: %+v", cfg.Repos)
			}
		})
	}
}

func TestEffectiveTagsRejectsAmbiguousParent(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		cfg := &Config{Repos: []RepoEntry{
			{Name: "api", Path: "/work/api", Tags: []string{"backend"}},
			{Name: "api", Path: "/oss/api", Tags: []string{"public"}},
		}}
		if reverse {
			slices.Reverse(cfg.Repos)
		}
		linked := RepoEntry{Name: "api/feature", Path: "/work/feature", WorktreeOf: "api", Tags: []string{"own"}}
		cfg.Repos = append(cfg.Repos, linked)
		if got := EffectiveTags(cfg, &linked); !slices.Equal(got, linked.Tags) {
			t.Errorf("EffectiveTags() = %v, want only owned tags %v", got, linked.Tags)
		}
		if got := FilterByTags(cfg, []RepoEntry{linked}, []string{"backend", "public"}); len(got) != 0 {
			t.Errorf("ambiguous inheritance selected worktree: %+v", got)
		}
		if got := FilterByTags(cfg, []RepoEntry{linked}, []string{"own"}); len(got) != 1 {
			t.Errorf("owned tag failed to select worktree: %+v", got)
		}
	}
}
