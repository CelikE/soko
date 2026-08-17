package output

import (
	"bytes"
	"sync"
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

// TestProgress_concurrent_increment_no_data_race is a regression test for the
// data race reported in issue #195: Increment is called concurrently from the
// errgroup workers in fetch/pull/grep/sync (up to maxConcurrency), and on a TTY
// each call runs render, which writes p.lineLen and Fprints to the shared
// writer with no synchronization. The production active flag is only set for a
// real *os.File TTY, which a test cannot attach, so it is forced here to
// exercise the render path. Run under -race (as CI does); before the fix this
// reports a data race on lineLen and the writer.
func TestProgress_concurrent_increment_no_data_race(t *testing.T) {
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
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < perWork; j++ {
				p.Increment()
			}
		}()
	}
	wg.Wait()

	p.Done()

	if got := int(p.current.Load()); got != expected {
		t.Errorf("expected counter = %d, got %d", expected, got)
	}
}
