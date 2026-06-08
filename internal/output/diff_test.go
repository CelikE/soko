package output

import (
	"strings"
	"testing"
)

func TestDiffUnifiedIdentical(t *testing.T) {
	if got := DiffUnified([]byte("a\nb\n"), []byte("a\nb\n"), "f"); got != "" {
		t.Errorf("identical inputs should diff to empty, got %q", got)
	}
	// Line-based: a differing trailing newline only is treated as identical.
	if got := DiffUnified([]byte("a\nb"), []byte("a\nb\n"), "f"); got != "" {
		t.Errorf("trailing-newline-only difference should be empty, got %q", got)
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

func TestDiffUnifiedNoTrailingNewline(t *testing.T) {
	// Must not panic on inputs lacking a trailing newline.
	got := DiffUnified([]byte("a"), []byte("b"), "f")
	if !strings.Contains(got, "-a") || !strings.Contains(got, "+b") {
		t.Errorf("expected -a/+b, got %q", got)
	}
}
