package config_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/CelikE/soko/internal/config"
)

func TestContextSetGetDelete(t *testing.T) {
	cfg := &config.Config{}

	if _, ok := config.GetContext(cfg, "work"); ok {
		t.Error("GetContext on empty config should report not found")
	}

	entry := &config.ContextEntry{
		SavedAt: time.Date(2026, 6, 11, 9, 30, 0, 0, time.UTC),
		Repos: []config.ContextRepo{
			{Name: "api", Branch: "feat/sso", Stashed: true},
			{Name: "frontend", Branch: "main"},
		},
	}
	config.SetContext(cfg, "work", entry)

	got, ok := config.GetContext(cfg, "work")
	if !ok {
		t.Fatal("GetContext after Set should find the context")
	}
	if len(got.Repos) != 2 || got.Repos[0].Branch != "feat/sso" || !got.Repos[0].Stashed {
		t.Errorf("GetContext repos = %+v, want the saved entries", got.Repos)
	}

	if !config.DeleteContext(cfg, "work") {
		t.Error("DeleteContext should report true for an existing context")
	}
	if config.DeleteContext(cfg, "work") {
		t.Error("DeleteContext should report false for a missing context")
	}
	if cfg.Contexts != nil {
		t.Error("deleting the last context should nil the map so YAML omits it")
	}
}

func TestContextNamesSorted(t *testing.T) {
	cfg := &config.Config{}
	for _, n := range []string{"zulu", "alpha", "mike"} {
		config.SetContext(cfg, n, &config.ContextEntry{})
	}
	names := config.ContextNames(cfg)
	want := []string{"alpha", "mike", "zulu"}
	if len(names) != len(want) {
		t.Fatalf("ContextNames = %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Errorf("ContextNames[%d] = %q, want %q", i, names[i], want[i])
		}
	}
}

func TestContextRoundTripYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg := &config.Config{}
	config.SetContext(cfg, "client-a", &config.ContextEntry{
		SavedAt: time.Date(2026, 6, 11, 9, 30, 0, 0, time.UTC),
		Repos:   []config.ContextRepo{{Name: "api", Branch: "feat/x", Detached: false, Stashed: true}},
	})

	if err := config.SaveTo(cfg, path); err != nil {
		t.Fatalf("SaveTo: %v", err)
	}
	loaded, err := config.LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}

	entry, ok := config.GetContext(loaded, "client-a")
	if !ok {
		t.Fatal("context lost in YAML round trip")
	}
	if entry.Repos[0].Name != "api" || entry.Repos[0].Branch != "feat/x" || !entry.Repos[0].Stashed {
		t.Errorf("round-tripped repo = %+v, want api/feat-x/stashed", entry.Repos[0])
	}
	if !entry.SavedAt.Equal(time.Date(2026, 6, 11, 9, 30, 0, 0, time.UTC)) {
		t.Errorf("round-tripped SavedAt = %v", entry.SavedAt)
	}
}
