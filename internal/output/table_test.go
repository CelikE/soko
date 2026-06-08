package output

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/fatih/color"
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

func TestRenderGrepResults(t *testing.T) {
	var buf bytes.Buffer
	groups := []GrepGroup{
		{Repo: "auth-service", Matches: []GrepMatch{
			{File: "h.go", Line: 42, Text: "func handleAuth() {}"},
			{File: "h.go", Line: 89, Text: "// handleAuth callback"},
		}},
		{Repo: "gateway", Matches: []GrepMatch{
			{File: "m.go", Line: 15, Text: "auth.handleAuth(ctx)"},
		}},
	}
	RenderGrepResults(&buf, groups, false)
	got := buf.String()

	for _, want := range []string{"auth-service", "gateway", "h.go:42", "h.go:89", "m.go:15", "func handleAuth() {}"} {
		if !strings.Contains(got, want) {
			t.Errorf("RenderGrepResults missing %q\n%s", want, got)
		}
	}
}

func TestRenderGrepResultsFilesOnly(t *testing.T) {
	var buf bytes.Buffer
	groups := []GrepGroup{
		{Repo: "auth-service", Matches: []GrepMatch{{File: "deploy/config.yaml"}}},
	}
	RenderGrepResults(&buf, groups, true)
	got := buf.String()
	if !strings.Contains(got, "deploy/config.yaml") {
		t.Errorf("files-only render missing path\n%s", got)
	}
	if strings.Contains(got, ":0") {
		t.Errorf("files-only render leaked a line number\n%s", got)
	}
}

func TestRenderGrepResultsEmpty(t *testing.T) {
	var buf bytes.Buffer
	RenderGrepResults(&buf, nil, false)
	if buf.Len() != 0 {
		t.Errorf("empty groups should render nothing, got %q", buf.String())
	}
}

func TestRenderGrepResultsHighlight(t *testing.T) {
	old := color.NoColor
	color.NoColor = false // force color so the highlight is observable
	defer func() { color.NoColor = old }()

	var buf bytes.Buffer
	groups := []GrepGroup{
		{Repo: "r", Matches: []GrepMatch{
			// "  func handleAuth() {}" — "handleAuth" starts at byte 7, len 10.
			{File: "h.go", Line: 1, Text: "  func handleAuth() {}", Col: 7, Length: 10},
		}},
	}
	RenderGrepResults(&buf, groups, false)
	got := buf.String()

	if !strings.Contains(got, Yellow("handleAuth")) {
		t.Errorf("expected matched span highlighted in yellow, got %q", got)
	}
	if !strings.Contains(got, "h.go:1") {
		t.Errorf("expected file:line preserved, got %q", got)
	}
	// Leading indentation is trimmed; the surrounding code is untouched.
	if !strings.Contains(got, "func ") || !strings.Contains(got, "() {}") {
		t.Errorf("expected surrounding text preserved, got %q", got)
	}
}

func TestRenderGrepResultsNoColor(t *testing.T) {
	old := color.NoColor
	color.NoColor = true // NO_COLOR equivalent
	defer func() { color.NoColor = old }()

	var buf bytes.Buffer
	groups := []GrepGroup{
		{Repo: "r", Matches: []GrepMatch{
			{File: "h.go", Line: 1, Text: "func handleAuth() {}", Col: 5, Length: 10},
		}},
	}
	RenderGrepResults(&buf, groups, false)
	got := buf.String()

	if strings.Contains(got, "\x1b[") {
		t.Errorf("expected no ANSI escapes under NO_COLOR, got %q", got)
	}
	if !strings.Contains(got, "func handleAuth() {}") {
		t.Errorf("expected plain text preserved, got %q", got)
	}
}

func TestRenderGrepSummary(t *testing.T) {
	cases := []struct {
		repos, matches int
		filesOnly      bool
		want           string
	}{
		{1, 1, false, "1 repo · 1 match"},
		{2, 3, false, "2 repos · 3 matches"},
		{2, 3, true, "2 repos · 3 files"},
	}
	for _, c := range cases {
		var buf bytes.Buffer
		RenderGrepSummary(&buf, c.repos, c.matches, c.filesOnly)
		if !strings.Contains(buf.String(), c.want) {
			t.Errorf("RenderGrepSummary(%d,%d,%v) = %q, want %q", c.repos, c.matches, c.filesOnly, buf.String(), c.want)
		}
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
