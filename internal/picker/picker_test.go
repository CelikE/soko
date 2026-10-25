package picker

import (
	"bytes"
	"strings"
	"testing"

	"github.com/fatih/color"
)

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

func TestFrameRows(t *testing.T) {
	for _, tt := range []struct {
		name  string
		frame string
		width int
		want  int
	}{
		{name: "empty", width: 10},
		{name: "blank rows", frame: "\r\n\r\n", width: 10, want: 2},
		{name: "exact width", frame: "1234567890\r\n", width: 10, want: 1},
		{name: "one cell over", frame: "12345678901\r\n", width: 10, want: 2},
		{name: "multiple wraps", frame: "123456789012345678901\r\n", width: 10, want: 3},
		{name: "ANSI styles", frame: "\x1b[32m1234567890\x1b[0m\r\n", width: 10, want: 1},
		{name: "wide characters", frame: "界界界\r\n", width: 3, want: 3},
		{name: "combining characters", frame: "e\u0301e\u0301\r\n", width: 2, want: 1},
		{name: "wrapped spaces", frame: "1234567890  \r\n", width: 10, want: 2},
		{name: "unknown width", frame: "long line\r\nsecond line\r\n", want: 2},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := frameRows(tt.frame, tt.width); got != tt.want {
				t.Errorf("frameRows(%q, %d) = %d, want %d", tt.frame, tt.width, got, tt.want)
			}
		})
	}
}

func TestRenderFrameTracksWrappedRows(t *testing.T) {
	wasNoColor := color.NoColor
	color.NoColor = false
	t.Cleanup(func() { color.NoColor = wasNoColor })

	for _, multi := range []bool{false, true} {
		name := "single"
		wantRows := 12
		if multi {
			name = "multi"
			wantRows = 13
		}
		t.Run(name, func(t *testing.T) {
			s := multiState("api")
			s.multi = multi
			s.allItems[0].Desc = "/" + strings.Repeat("p", 80)
			var buf bytes.Buffer
			renderFrame(&buf, "Pick:", s, 20)
			if s.lastLines != wantRows {
				t.Fatalf("rendered rows = %d, want %d (frame: %q)", s.lastLines, wantRows, buf.String())
			}
			buf.Reset()
			clearLines(&buf, s.lastLines)
			if got := strings.Count(buf.String(), "\x1b[A"); got != wantRows {
				t.Errorf("cleanup moves up %d rows, want %d", got, wantRows)
			}

			// Each frame replaces the count; filtering to no matches must not
			// retain the old, taller frame's cleanup distance.
			s.query = "absent"
			s.filtered = nil
			buf.Reset()
			renderFrame(&buf, "Pick:", s, 80)
			if s.lastLines != 6 {
				t.Errorf("filtered frame rows = %d, want 6", s.lastLines)
			}
		})
	}
}
