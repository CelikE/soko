package output

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"

	"golang.org/x/term"
)

// Progress displays a single-line progress counter on a terminal.
// It overwrites the current line using \r so output stays compact.
// When the writer is not a TTY, all methods are no-ops.
type Progress struct {
	w       io.Writer
	msg     string
	total   int
	current atomic.Int32
	active  bool

	// mu serializes the display update. Increment is called concurrently from
	// the errgroup workers in fetch/pull/grep/sync, so render and Done — which
	// mutate lineLen and write to w — must not run in parallel (issue #195).
	mu      sync.Mutex
	lineLen int // length of last written line, for clearing; guarded by mu
}

// NewProgress creates a progress indicator that writes to w.
// If w is not a terminal, or quiet mode is active, the returned Progress is
// inert (no output) — Increment and Done already no-op when inactive.
func NewProgress(w io.Writer, msg string, total int) *Progress {
	p := &Progress{
		w:     w,
		msg:   msg,
		total: total,
	}

	if f, ok := w.(*os.File); ok && !quiet {
		p.active = term.IsTerminal(int(f.Fd()))
	}

	if p.active {
		p.render()
	}

	return p
}

// Increment advances the counter by one and refreshes the display.
func (p *Progress) Increment() {
	p.current.Add(1)
	if p.active {
		p.render()
	}
}

// Done clears the progress line so subsequent output starts clean.
func (p *Progress) Done() {
	if !p.active {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	// Overwrite with spaces, then return to start of line.
	blank := "\r" + strings.Repeat(" ", p.lineLen) + "\r"
	_, _ = fmt.Fprint(p.w, blank)
}

func (p *Progress) render() {
	p.mu.Lock()
	defer p.mu.Unlock()
	// Read the latest count under the display lock so a delayed worker cannot
	// overwrite a newer progress count with an older one.
	line := fmt.Sprintf("\r  %s %s... %d/%d", Dim("·"), p.msg, p.current.Load(), p.total)
	p.lineLen = len(line) - 1 // subtract \r
	_, _ = fmt.Fprint(p.w, line)
}
