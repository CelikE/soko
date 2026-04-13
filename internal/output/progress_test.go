package output

import (
	"bytes"
	"testing"
)

func TestProgress_nonTTY_produces_no_output(t *testing.T) {
	var buf bytes.Buffer
	prog := NewProgress(&buf, "Testing", 5)

	prog.Increment()
	prog.Increment()
	prog.Done()

	if buf.Len() != 0 {
		t.Errorf("expected no output for non-TTY writer, got %q", buf.String())
	}
}

func TestProgress_increment_advances_counter(t *testing.T) {
	p := &Progress{total: 3}

	p.Increment()
	p.Increment()

	got := int(p.current.Load())
	if got != 2 {
		t.Errorf("expected counter = 2, got %d", got)
	}
}
