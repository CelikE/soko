package output

import (
	"bytes"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/fatih/color"
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

// TestProgress_concurrent_increment_no_data_race is a regression test for the
// data race reported in issue #195: Increment is called concurrently from the
// errgroup workers in fetch/pull/grep/sync (up to maxConcurrency), and on a TTY
// each call runs render, which writes p.lineLen and Fprints to the shared
// writer with no synchronization. Force active rendering to exercise that path
// without requiring a real TTY, and check complete, non-decreasing updates and
// the final clearing sequence. Run under -race (as CI does); before the fix this
// reports a data race on lineLen and the writer.
func TestProgress_concurrent_increment_no_data_race(t *testing.T) {
	old := color.NoColor
	color.NoColor = false
	t.Cleanup(func() { color.NoColor = old })

	const (
		workers  = 8
		perWork  = 25
		expected = workers * perWork
	)

	var buf bytes.Buffer
	p := &Progress{
		w:      &buf,
		msg:    "fetching",
		total:  expected,
		active: true, // force the render path (real code sets this only for a TTY)
	}

	var wg sync.WaitGroup
	start := make(chan struct{})
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			<-start
			for j := 0; j < perWork; j++ {
				p.Increment()
			}
		}()
	}
	close(start)
	wg.Wait()

	if got := int(p.current.Load()); got != expected {
		t.Errorf("expected counter = %d, got %d", expected, got)
	}

	rendered := buf.String()
	frames := strings.Split(strings.TrimPrefix(rendered, "\r"), "\r")
	if len(frames) != expected {
		t.Fatalf("expected %d complete progress frames, got %d", expected, len(frames))
	}
	prefix := "  " + Dim("·") + " fetching... "
	suffix := "/" + strconv.Itoa(expected)
	var previous int
	for _, frame := range frames {
		value, ok := strings.CutPrefix(frame, prefix)
		if !ok {
			t.Fatalf("incomplete progress frame: %q", frame)
		}
		value, ok = strings.CutSuffix(value, suffix)
		if !ok {
			t.Fatalf("incomplete progress frame: %q", frame)
		}
		current, err := strconv.Atoi(value)
		if err != nil {
			t.Fatalf("invalid progress count in %q: %v", frame, err)
		}
		if current < 1 || current < previous || current > expected {
			t.Fatalf("progress count moved backwards or out of range: %d after %d", current, previous)
		}
		previous = current
	}
	if previous != expected {
		t.Errorf("last displayed count = %d, want %d", previous, expected)
	}

	buf.Reset()
	p.Done()
	// ANSI styles occupy no columns, and the multibyte bullet occupies one.
	clearing := "\r" + strings.Repeat(" ", len([]rune("  · fetching... 200/200"))) + "\r"
	if got := buf.String(); got != clearing {
		t.Errorf("Done output = %q, want %q", got, clearing)
	}
}
