package output

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestRenderHealthTable(t *testing.T) {
	var buf bytes.Buffer
	rows := []HealthRow{
		{Rank: 1, Name: "legacy-api", SeverityText: SymConflict + " crit", ScoreText: "108", Reason: "detached HEAD", State: StateConflict},
		{Rank: 2, Name: "web", SeverityText: SymWarning + " warn", ScoreText: "12", Reason: "3 changes", State: StateDirty},
		{Rank: 3, Name: "notes", SeverityText: SymClean + " ok", ScoreText: "0", Reason: "clean · in sync", State: StateClean},
	}
	RenderHealthTable(&buf, rows)
	got := buf.String()

	for _, want := range []string{"#", "REPO", "SEVERITY", "SCORE", "REASON", "legacy-api", "web", "notes", "108", "detached HEAD"} {
		if !strings.Contains(got, want) {
			t.Errorf("RenderHealthTable output missing %q\n%s", want, got)
		}
	}
	// Header + separator + one line per row.
	lines := strings.Count(strings.TrimRight(got, "\n"), "\n") + 1
	if lines != 2+len(rows) {
		t.Errorf("line count = %d, want %d\n%s", lines, 2+len(rows), got)
	}
}

func TestRenderHealthTableEmpty(t *testing.T) {
	var buf bytes.Buffer
	RenderHealthTable(&buf, nil)
	// Header + separator only; no data rows.
	if strings.Count(strings.TrimRight(buf.String(), "\n"), "\n")+1 != 2 {
		t.Errorf("empty health table should render only header + rule, got:\n%s", buf.String())
	}
}

func TestRenderHealthSummary(t *testing.T) {
	var buf bytes.Buffer
	RenderHealthSummary(&buf, 5, 2, 2, 1)
	got := buf.String()
	if !strings.Contains(got, "5 repos · 2 crit · 2 warn · 1 ok") {
		t.Errorf("RenderHealthSummary = %q, want '5 repos · 2 crit · 2 warn · 1 ok'", got)
	}
}

func TestFormatStatus(t *testing.T) {
	tests := []struct {
		name      string
		modified  int
		untracked int
		deleted   int
		conflicts int
		want      string
	}{
		{
			name: "clean",
			want: "✓ clean",
		},
		{
			name:     "modified only",
			modified: 3,
			want:     "✎ 3M",
		},
		{
			name:      "untracked only",
			untracked: 2,
			want:      "✎ 2U",
		},
		{
			name:    "deleted only",
			deleted: 1,
			want:    "✎ 1D",
		},
		{
			name:      "mixed modified and untracked",
			modified:  1,
			untracked: 2,
			want:      "✎ 1M 2U",
		},
		{
			name:      "all file types",
			modified:  3,
			untracked: 2,
			deleted:   1,
			want:      "✎ 3M 2U 1D",
		},
		{
			name:      "conflicts take precedence",
			modified:  1,
			conflicts: 2,
			want:      "✗ 2C",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatStatus(tt.modified, tt.untracked, tt.deleted, tt.conflicts)
			if got != tt.want {
				t.Errorf("FormatStatus(%d, %d, %d, %d) = %q, want %q",
					tt.modified, tt.untracked, tt.deleted, tt.conflicts, got, tt.want)
			}
		})
	}
}

func TestFormatAheadBehind(t *testing.T) {
	tests := []struct {
		name   string
		ahead  int
		behind int
		want   string
	}{
		{
			name: "in sync",
			want: "·",
		},
		{
			name:  "ahead only",
			ahead: 2,
			want:  "↑2",
		},
		{
			name:   "behind only",
			behind: 3,
			want:   "↓3",
		},
		{
			name:   "ahead and behind",
			ahead:  5,
			behind: 1,
			want:   "↑5 ↓1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatAheadBehind(tt.ahead, tt.behind)
			if got != tt.want {
				t.Errorf("FormatAheadBehind(%d, %d) = %q, want %q",
					tt.ahead, tt.behind, got, tt.want)
			}
		})
	}
}

func TestFormatTimeAgo(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name string
		t    time.Time
		want string
	}{
		{
			name: "zero time",
			t:    time.Time{},
			want: "-",
		},
		{
			name: "just now",
			t:    now.Add(-30 * time.Second),
			want: "just now",
		},
		{
			name: "minutes ago",
			t:    now.Add(-45 * time.Minute),
			want: "45m ago",
		},
		{
			name: "hours ago",
			t:    now.Add(-3 * time.Hour),
			want: "3h ago",
		},
		{
			name: "days ago",
			t:    now.Add(-2 * 24 * time.Hour),
			want: "2d ago",
		},
		{
			name: "weeks ago",
			t:    now.Add(-3 * 7 * 24 * time.Hour),
			want: "3w ago",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatTimeAgo(tt.t)
			if got != tt.want {
				t.Errorf("FormatTimeAgo() = %q, want %q", got, tt.want)
			}
		})
	}
}
