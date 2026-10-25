package output

import (
	"bytes"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/fatih/color"
)

func TestStatusColumnWidthsDisplayCells(t *testing.T) {
	tests := []struct {
		name string
		rows []StatusRow
		want [4]int
	}{
		{name: "headers", want: [4]int{6, 8, 8, 4}},
		{
			name: "unicode",
			rows: []StatusRow{{Name: "倉庫倉庫倉庫", Branch: "feature/🌳", StatusText: "✎ 3M 2U", AheadBehindText: "↑2 ↓3"}},
			want: [4]int{14, 12, 9, 7},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, branch, status, ab := columnWidths(tt.rows)
			if got := [4]int{repo, branch, status, ab}; got != tt.want {
				t.Errorf("column widths = %v, want %v display cells", got, tt.want)
			}
		})
	}
}

func TestRenderStatusColumnAlignment(t *testing.T) {
	old := color.NoColor
	t.Cleanup(func() { color.NoColor = old })

	rows := []StatusRow{
		{Name: "api", Branch: "main", StatusText: "✓ clean", AheadBehindText: "·", LastCommitText: "first commit", State: StateClean},
		{Name: "倉庫倉庫倉庫", Branch: "feature/🌳", StatusText: "✎ 3M 2U", AheadBehindText: "↑2 ↓3", LastCommitText: "second commit", State: StateDirty},
		{Name: "cafe\u0301-👩‍💻", Branch: "修正", StatusText: "✗ 2C", AheadBehindText: "↓12", LastCommitText: "third commit", State: StateConflict},
	}
	for _, noColor := range []bool{true, false} {
		name := "plain"
		if !noColor {
			name = "colored"
		}
		t.Run(name, func(t *testing.T) {
			color.NoColor = noColor
			for _, grouped := range []bool{false, true} {
				name := "flat"
				indent := 2
				if grouped {
					name, indent = "grouped", 6
				}
				t.Run(name, func(t *testing.T) {
					var buf bytes.Buffer
					if grouped {
						RenderStatusGrouped(&buf, []StatusGroup{{Tag: "backend", Rows: rows[:2]}, {Tag: "frontend", Rows: rows[2:]}})
					} else {
						RenderStatusTableN(&buf, rows, 0)
					}
					lines := strings.Split(strings.TrimSuffix(ansi.Strip(buf.String()), "\n"), "\n")
					headers := []string{"REPO", "BRANCH", "STATUS", "↑↓", "LAST COMMIT"}
					columns := make([]int, len(headers))
					for i, header := range headers {
						columns[i] = statusDisplayColumn(t, lines[0], header)
					}
					if columns[0] != indent {
						t.Errorf("REPO starts at column %d, want %d", columns[0], indent)
					}
					if ansi.StringWidth(lines[1]) != ansi.StringWidth(lines[0]) {
						t.Errorf("separator width = %d, header width = %d", ansi.StringWidth(lines[1]), ansi.StringWidth(lines[0]))
					}
					for _, row := range rows {
						var line string
						for _, candidate := range lines[2:] {
							if strings.Contains(candidate, row.Name) {
								line = candidate
								break
							}
						}
						for i, cell := range []string{row.Name, row.Branch, row.StatusText, row.AheadBehindText, row.LastCommitText} {
							if got := statusDisplayColumn(t, line, cell); got != columns[i] {
								t.Errorf("%s: %s starts at column %d, header at %d\n%q", row.Name, headers[i], got, columns[i], line)
							}
						}
					}
				})
			}
		})
	}
}

func statusDisplayColumn(t *testing.T, line, cell string) int {
	t.Helper()
	i := strings.Index(line, cell)
	if i < 0 {
		t.Fatalf("cell %q missing from %q", cell, line)
	}
	return ansi.StringWidth(line[:i])
}
