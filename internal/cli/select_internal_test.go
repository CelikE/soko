package cli

import (
	"testing"

	"github.com/CelikE/soko/internal/config"
)

func TestMapSelected(t *testing.T) {
	repos := []config.RepoEntry{
		{Name: "a", Path: "/p/a"},
		{Name: "b", Path: "/p/b"},
		{Name: "c", Path: "/p/c"},
	}

	got := mapSelected(repos, []int{0, 2})
	if len(got) != 2 || got[0].Name != "a" || got[1].Name != "c" {
		t.Errorf("mapSelected([0 2]) = %+v, want a and c", got)
	}

	// Order follows the index slice.
	got = mapSelected(repos, []int{2, 1})
	if len(got) != 2 || got[0].Name != "c" || got[1].Name != "b" {
		t.Errorf("mapSelected([2 1]) = %+v, want c then b", got)
	}

	// Out-of-range indices are ignored, not a panic.
	got = mapSelected(repos, []int{1, 99, -1})
	if len(got) != 1 || got[0].Name != "b" {
		t.Errorf("mapSelected with bad indices = %+v, want just b", got)
	}

	// Empty selection yields an empty (non-nil) slice.
	if got := mapSelected(repos, []int{}); len(got) != 0 {
		t.Errorf("mapSelected([]) = %+v, want empty", got)
	}
}

func TestFilterCleanResults(t *testing.T) {
	results := []cleanResult{
		{Name: "a", Path: "/p/a", Branches: []string{"x"}},
		{Name: "b", Path: "/p/b", Branches: []string{"y"}},
		{Name: "c", Path: "/p/c", Branches: []string{"z"}},
	}
	chosen := []config.RepoEntry{{Name: "a", Path: "/p/a"}, {Name: "c", Path: "/p/c"}}

	got := filterCleanResults(results, chosen)
	if len(got) != 2 || got[0].Path != "/p/a" || got[1].Path != "/p/c" {
		t.Errorf("filterCleanResults = %+v, want a and c", got)
	}
}
