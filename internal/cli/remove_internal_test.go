package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/CelikE/soko/internal/config"
)

func TestRemoveSelectedRepos(t *testing.T) {
	for _, tt := range []struct {
		name        string
		selected    []int
		wantRemoved []int
		force       bool
		jsonOut     bool
		decline     bool
	}{
		{name: "parent only", selected: []int{0}, wantRemoved: []int{0, 1, 2}},
		{name: "parent and one worktree", selected: []int{0, 1}, wantRemoved: []int{0, 1, 2}, force: true},
		{name: "worktree only", selected: []int{1}, wantRemoved: []int{1}},
		{name: "decline cascade", selected: []int{0}, wantRemoved: []int{0, 1, 2}, decline: true},
		{name: "all entries", selected: []int{0, 1, 2, 3}, wantRemoved: []int{0, 1, 2, 3}, force: true, jsonOut: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			cfg := &config.Config{Repos: []config.RepoEntry{
				{Name: "api", Path: "/repos/api"},
				{Name: "custom-alias", Path: "/repos/feature", WorktreeOf: "api"},
				{Name: "api/fix", Path: "/repos/fix", WorktreeOf: "api"},
				{Name: "other", Path: "/repos/other", Meta: map[string]string{"note": "keep me"}},
			}}
			if err := config.Save(cfg); err != nil {
				t.Fatal(err)
			}
			selected := mapSelected(cfg.Repos, tt.selected)
			removed := mapSelected(cfg.Repos, tt.wantRemoved)
			want := append([]config.RepoEntry(nil), cfg.Repos...)
			if !tt.decline {
				want = nil
				for i, entry := range cfg.Repos {
					if !slices.Contains(tt.wantRemoved, i) {
						want = append(want, entry)
					}
				}
			}
			var stdout, stderr bytes.Buffer
			cmd := NewRootCmd("test")
			cmd.SetErr(&stderr)
			answer := "y\n"
			if tt.decline {
				answer = "n\n"
			}
			cmd.SetIn(strings.NewReader(answer))
			if err := removeSelectedRepos(cmd, cfg, selected, tt.force, tt.jsonOut, &stdout); err != nil {
				t.Fatal(err)
			}
			if !tt.force && !strings.Contains(stderr.String(), fmt.Sprintf("remove %d selected", len(removed))) {
				t.Errorf("prompt = %q, want expanded removal count %d", stderr.String(), len(removed))
			}
			saved, err := config.Load()
			if err != nil {
				t.Fatal(err)
			}
			if len(saved.Repos) != len(want) || len(want) > 0 && !reflect.DeepEqual(saved.Repos, want) {
				t.Fatalf("registry = %+v, want %+v", saved.Repos, want)
			}
			switch {
			case tt.decline:
				if stdout.Len() != 0 || !strings.Contains(stderr.String(), "aborted") {
					t.Errorf("declined removal: stdout = %q, stderr = %q", stdout.String(), stderr.String())
				}
			case tt.jsonOut:
				var rows []config.RepoEntry
				if err := json.Unmarshal(stdout.Bytes(), &rows); err != nil {
					t.Fatal(err)
				}
				if len(rows) != len(removed) {
					t.Errorf("removed = %+v, want %d entries without duplicates", rows, len(removed))
				}
			default:
				if !strings.Contains(stdout.String(), fmt.Sprintf("removed %d repo", len(removed))) {
					t.Errorf("output = %q, want expanded removal count %d", stdout.String(), len(removed))
				}
			}
		})
	}
}
