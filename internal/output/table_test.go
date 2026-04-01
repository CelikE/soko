package output

import (
	"testing"
	"time"
)

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
