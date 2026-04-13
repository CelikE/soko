package output

import (
	"fmt"
	"io"
	"os"
	"strings"
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
	lineLen int // length of last written line, for clearing
}

// NewProgress creates a progress indicator that writes to w.
// If w is not a terminal, the returned Progress is inert (no output).
func NewProgress(w io.Writer, msg string, total int) *Progress {
	p := &Progress{
		w:     w,
		msg:   msg,
		total: total,
	}

	if f, ok := w.(*os.File); ok {
		p.active = term.IsTerminal(int(f.Fd()))
	}

	if p.active {
		p.render(0)
	}

	return p
}

// Increment advances the counter by one and refreshes the display.
func (p *Progress) Increment() {
	n := int(p.current.Add(1))
	if p.active {
		p.render(n)
	}
}

// Done clears the progress line so subsequent output starts clean.
func (p *Progress) Done() {
	if !p.active {
		return
	}
	// Overwrite with spaces, then return to start of line.
	blank := "\r" + strings.Repeat(" ", p.lineLen) + "\r"
	_, _ = fmt.Fprint(p.w, blank)
}

func (p *Progress) render(n int) {
	line := fmt.Sprintf("\r  %s %s... %d/%d", Dim("·"), p.msg, n, p.total)
	p.lineLen = len(line) - 1 // subtract \r
	_, _ = fmt.Fprint(p.w, line)
}
