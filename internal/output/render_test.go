package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestRenderJSON(t *testing.T) {
	var buf bytes.Buffer
	data := []struct {
		Name string `json:"name"`
	}{{Name: "test"}}

	if err := RenderJSON(&buf, data); err != nil {
		t.Fatalf("RenderJSON() error = %v", err)
	}

	var result []map[string]string
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if result[0]["name"] != "test" {
		t.Errorf("name = %q, want %q", result[0]["name"], "test")
	}
	// Verify pretty-printing (indented).
	if !strings.Contains(buf.String(), "  ") {
		t.Error("RenderJSON should produce indented output")
	}
}

func TestPlural(t *testing.T) {
	tests := []struct {
		n    int
		word string
		want string
	}{
		{0, "repo", "repos"},
		{1, "repo", "repo"},
		{2, "repo", "repos"},
		{1, "commit", "commit"},
		{5, "commit", "commits"},
		{1, "branch", "branch"},
		{2, "branch", "branches"},
		{1, "match", "match"},
		{3, "match", "matches"},
		{1, "fix", "fix"},
		{2, "fix", "fixes"},
		{1, "change", "change"},
		{2, "change", "changes"},
		{1, "check", "check"},
		{2, "check", "checks"},
		{1, "warning", "warning"},
		{2, "warning", "warnings"},
		{1, "error", "error"},
		{2, "error", "errors"},
		{1, "worktree", "worktree"},
		{2, "worktree", "worktrees"},
	}

	for _, tt := range tests {
		got := Plural(tt.n, tt.word)
		if got != tt.want {
			t.Errorf("Plural(%d, %q) = %q, want %q", tt.n, tt.word, got, tt.want)
		}
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		s      string
		maxLen int
		want   string
	}{
		{"short", 10, "short"},
		{"exactly10!", 10, "exactly10!"},
		{"this is way too long", 10, "this is w…"},
		{"", 5, ""},
	}

	for _, tt := range tests {
		got := truncate(tt.s, tt.maxLen)
		if got != tt.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", tt.s, tt.maxLen, got, tt.want)
		}
	}
}

func TestFormatLastCommit(t *testing.T) {
	now := time.Now()

	got := FormatLastCommit(time.Time{}, "")
	if got != "-" {
		t.Errorf("FormatLastCommit(zero, empty) = %q, want %q", got, "-")
	}

	got = FormatLastCommit(now.Add(-2*time.Hour), "feat: add something new")
	if !strings.Contains(got, "2h ago") {
		t.Errorf("FormatLastCommit() = %q, want to contain '2h ago'", got)
	}
	if !strings.Contains(got, "feat: add something new") {
		t.Errorf("FormatLastCommit() = %q, want to contain message", got)
	}

	// Long message should be truncated.
	got = FormatLastCommit(now, "this is a very long commit message that should be truncated at thirty chars")
	if len(got) > 100 {
		t.Errorf("FormatLastCommit() too long: %d chars", len(got))
	}
}

func TestRenderSummary(t *testing.T) {
	var buf bytes.Buffer
	RenderSummary(&buf, 5, 2, 1, 10)
	out := buf.String()

	if !strings.Contains(out, "5 repos") {
		t.Errorf("RenderSummary = %q, want '5 repos'", out)
	}
	if !strings.Contains(out, "2 dirty") {
		t.Errorf("RenderSummary = %q, want '2 dirty'", out)
	}
	if !strings.Contains(out, "1 behind") {
		t.Errorf("RenderSummary = %q, want '1 behind'", out)
	}
	if !strings.Contains(out, "10 changes") {
		t.Errorf("RenderSummary = %q, want '10 changes'", out)
	}

	// Singular.
	buf.Reset()
	RenderSummary(&buf, 1, 0, 0, 1)
	out = buf.String()
	if !strings.Contains(out, "1 repo") {
		t.Errorf("RenderSummary singular = %q, want '1 repo'", out)
	}
	if !strings.Contains(out, "1 change") {
		t.Errorf("RenderSummary singular = %q, want '1 change'", out)
	}
}

func TestRenderActionSummary(t *testing.T) {
	var buf bytes.Buffer
	RenderActionSummary(&buf, 3, 2, 1)
	out := buf.String()

	if !strings.Contains(out, "3 repos") {
		t.Errorf("RenderActionSummary = %q, want '3 repos'", out)
	}
	if !strings.Contains(out, "2 ok") {
		t.Errorf("RenderActionSummary = %q, want '2 ok'", out)
	}
	if !strings.Contains(out, "1 failed") {
		t.Errorf("RenderActionSummary = %q, want '1 failed'", out)
	}
}

func TestConfirmWarnFailInfo(t *testing.T) {
	tests := []struct {
		name string
		fn   func(*bytes.Buffer, string)
		msg  string
		want string
	}{
		{"Confirm", func(b *bytes.Buffer, m string) { Confirm(b, m) }, "done", "done"},
		{"Warn", func(b *bytes.Buffer, m string) { Warn(b, m) }, "careful", "careful"},
		{"Fail", func(b *bytes.Buffer, m string) { Fail(b, m) }, "broken", "broken"},
		{"Info", func(b *bytes.Buffer, m string) { Info(b, m) }, "note", "note"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			tt.fn(&buf, tt.msg)
			if !strings.Contains(buf.String(), tt.want) {
				t.Errorf("%s() = %q, want to contain %q", tt.name, buf.String(), tt.want)
			}
		})
	}
}

func TestRenderStatusTableN_Truncation(t *testing.T) {
	rows := make([]StatusRow, 5)
	for i := range rows {
		rows[i] = StatusRow{Name: "repo", Branch: "main", StatusText: "clean", AheadBehindText: "·"}
	}

	// No truncation.
	var buf bytes.Buffer
	RenderStatusTableN(&buf, rows, 0)
	if strings.Contains(buf.String(), "showing") {
		t.Error("maxRows=0 should not truncate")
	}

	// Truncation at 3.
	buf.Reset()
	RenderStatusTableN(&buf, rows, 3)
	if !strings.Contains(buf.String(), "showing 3 of 5") {
		t.Errorf("truncated output = %q, want 'showing 3 of 5'", buf.String())
	}
}

func TestColumnWidths(t *testing.T) {
	rows := []StatusRow{
		{Name: "short", Branch: "m", StatusText: "ok", AheadBehindText: "·"},
		{Name: "a-longer-name", Branch: "feature/branch", StatusText: "✎ 3M 2U", AheadBehindText: "↑2 ↓3"},
	}

	repo, branch, status, ab := columnWidths(rows)

	// Should be at least the header width + 2 padding.
	if repo < len("REPO")+2 {
		t.Errorf("repo width = %d, too small", repo)
	}
	if repo < len("a-longer-name")+2 {
		t.Errorf("repo width = %d, should fit 'a-longer-name'", repo)
	}
	if branch < len("feature/branch")+2 {
		t.Errorf("branch width = %d, should fit 'feature/branch'", branch)
	}
	if status < len("✎ 3M 2U")+2 {
		t.Errorf("status width = %d, should fit '✎ 3M 2U'", status)
	}
	if ab < len("↑2 ↓3")+2 {
		t.Errorf("ab width = %d, should fit '↑2 ↓3'", ab)
	}
}
