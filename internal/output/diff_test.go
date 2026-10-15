package output

import (
	"strings"
	"testing"
)

func TestDiffUnifiedIdentical(t *testing.T) {
	for _, content := range []string{"", "a\nb\n", "a\nb", "\n\n"} {
		if got := DiffUnified([]byte(content), []byte(content), "f"); got != "" {
			t.Errorf("identical inputs %q should diff to empty, got %q", content, got)
		}
	}
}

func TestDiffUnifiedSingleChange(t *testing.T) {
	got := DiffUnified([]byte("a\nb\nc\n"), []byte("a\nB\nc\n"), "f")
	for _, want := range []string{"--- f", "+++ f", "@@ -1,3 +1,3 @@", "-b", "+B", " a", " c"} {
		if !strings.Contains(got, want) {
			t.Errorf("diff missing %q\n%s", want, got)
		}
	}
}

func TestDiffUnifiedPureAddition(t *testing.T) {
	got := DiffUnified(nil, []byte("x\ny\n"), "f")
	if !strings.Contains(got, "@@ -0,0 +1,2 @@") {
		t.Errorf("pure-add hunk header wrong:\n%s", got)
	}
	if !strings.Contains(got, "+x") || !strings.Contains(got, "+y") {
		t.Errorf("pure-add should have + lines:\n%s", got)
	}
	// No removed content lines for a pure addition.
	for _, line := range strings.Split(got, "\n") {
		if strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---") {
			t.Errorf("pure-add should have no removed lines, found %q", line)
		}
	}
}

func TestDiffUnifiedNewlineHandling(t *testing.T) {
	tests := []struct {
		name string
		old  string
		new  string
		want string
	}{
		{
			name: "remove final newline",
			old:  "a\n",
			new:  "a",
			want: "@@ -1,1 +1,1 @@\n-a\n+a\n\\ No newline at end of file\n",
		},
		{
			name: "add final newline",
			old:  "a\nb",
			new:  "a\nb\n",
			want: "@@ -1,2 +1,2 @@\n a\n-b\n\\ No newline at end of file\n+b\n",
		},
		{
			name: "change unterminated lines",
			old:  "a",
			new:  "b",
			want: "@@ -1,1 +1,1 @@\n-a\n\\ No newline at end of file\n+b\n\\ No newline at end of file\n",
		},
		{
			name: "create unterminated file",
			new:  "a",
			want: "@@ -0,0 +1,1 @@\n+a\n\\ No newline at end of file\n",
		},
		{
			name: "delete unterminated file",
			old:  "a",
			want: "@@ -1,1 +0,0 @@\n-a\n\\ No newline at end of file\n",
		},
		{
			name: "unterminated context line",
			old:  "a\nb",
			new:  "A\nb",
			want: "@@ -1,2 +1,2 @@\n-a\n+A\n b\n\\ No newline at end of file\n",
		},
		{
			name: "add empty line to empty file",
			new:  "\n",
			want: "@@ -0,0 +1,1 @@\n+\n",
		},
		{
			name: "remove only empty line",
			old:  "\n",
			want: "@@ -1,1 +0,0 @@\n-\n",
		},
		{
			name: "add trailing blank line",
			old:  "a\n",
			new:  "a\n\n",
			want: "@@ -1,1 +1,2 @@\n a\n+\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			want := "--- f\n+++ f\n" + tt.want
			if got := DiffUnified([]byte(tt.old), []byte(tt.new), "f"); got != want {
				t.Errorf("diff = %q, want %q", got, want)
			}
		})
	}
}
