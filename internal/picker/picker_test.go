package picker

import "testing"

// TestFilterItems covers the substring, case-insensitive matching that backs
// both live typing and the pre-seeded InitialQuery on soko go.
func TestFilterItems(t *testing.T) {
	items := []Item{
		{Label: "reactjs-fso"},
		{Label: "soko"},
		{Label: "widget"},
	}

	tests := []struct {
		query string
		want  []string
	}{
		{"", []string{"reactjs-fso", "soko", "widget"}},
		{"so", []string{"reactjs-fso", "soko"}}, // substring, not prefix
		{"SO", []string{"reactjs-fso", "soko"}}, // case-insensitive
		{"wid", []string{"widget"}},
		{"zzz", nil},
	}

	for _, tt := range tests {
		got := filterItems(items, tt.query)
		if len(got) != len(tt.want) {
			t.Errorf("filterItems(%q) = %d items, want %d", tt.query, len(got), len(tt.want))
			continue
		}
		for i, w := range tt.want {
			if got[i].Label != w {
				t.Errorf("filterItems(%q)[%d] = %q, want %q", tt.query, i, got[i].Label, w)
			}
		}
	}
}
