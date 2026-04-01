package cli

import (
	"testing"
)

func TestFilterResults(t *testing.T) {
	results := []statusResult{
		{index: 0, dirty: true, ahead: false, behind: false, changes: 3},  // dirty only
		{index: 1, dirty: false, ahead: false, behind: false, changes: 0}, // clean
		{index: 2, dirty: false, ahead: true, behind: false, changes: 0},  // ahead only
		{index: 3, dirty: false, ahead: false, behind: true, changes: 0},  // behind only
		{index: 4, dirty: true, ahead: false, behind: true, changes: 2},   // dirty + behind
		{index: 5, dirty: false, ahead: false, behind: false, err: "path not found"},
	}

	tests := []struct {
		name    string
		dirty   bool
		clean   bool
		ahead   bool
		behind  bool
		wantIdx []int
	}{
		{
			name:    "no filters returns empty",
			wantIdx: nil,
		},
		{
			name:    "dirty only",
			dirty:   true,
			wantIdx: []int{0, 4},
		},
		{
			name:    "clean only",
			clean:   true,
			wantIdx: []int{1},
		},
		{
			name:    "ahead only",
			ahead:   true,
			wantIdx: []int{2},
		},
		{
			name:    "behind only",
			behind:  true,
			wantIdx: []int{3, 4},
		},
		{
			name:    "dirty or behind",
			dirty:   true,
			behind:  true,
			wantIdx: []int{0, 3, 4},
		},
		{
			name:    "clean or ahead",
			clean:   true,
			ahead:   true,
			wantIdx: []int{1, 2},
		},
		{
			name:    "all filters",
			dirty:   true,
			clean:   true,
			ahead:   true,
			behind:  true,
			wantIdx: []int{0, 1, 2, 3, 4},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := filterResults(results, tt.dirty, tt.clean, tt.ahead, tt.behind)

			if len(got) != len(tt.wantIdx) {
				t.Fatalf("filterResults() returned %d results, want %d", len(got), len(tt.wantIdx))
			}

			for i, want := range tt.wantIdx {
				if got[i].index != want {
					t.Errorf("filterResults()[%d].index = %d, want %d", i, got[i].index, want)
				}
			}
		})
	}
}
