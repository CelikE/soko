// Package picker provides a minimal interactive list picker that renders
// to a terminal. It uses raw terminal mode to read individual keystrokes
// without requiring external TUI dependencies.
package picker

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/fatih/color"
	"golang.org/x/term"

	"github.com/CelikE/soko/internal/output"
)

// Item represents a single selectable entry in the picker.
type Item struct {
	Label string
	Desc  string
	index int // original index in the full list
}

// Options configures the picker appearance.
type Options struct {
	Title string
	Items []Item
}

type state struct {
	allItems   []Item
	filtered   []Item
	query      string
	cursor     int
	labelWidth int
	lastLines  int // lines rendered on last draw, for clearing
}

// Run displays an interactive picker and returns the index of the selected
// item from the original Items slice, or -1 if the user cancelled.
func Run(in *os.File, w io.Writer, opts Options) int {
	// Force colors on — the picker renders to stderr which is a terminal,
	// but fatih/color checks stdout which may be a pipe (cd $(soko go)).
	wasNoColor := color.NoColor
	color.NoColor = false
	defer func() { color.NoColor = wasNoColor }()

	oldState, err := term.MakeRaw(int(in.Fd()))
	if err != nil {
		return -1
	}

	// Tag each item with its original index.
	for i := range opts.Items {
		opts.Items[i].index = i
	}

	s := &state{
		allItems:   opts.Items,
		filtered:   opts.Items,
		labelWidth: computeLabelWidth(opts.Items),
	}

	defer func() {
		_ = term.Restore(int(in.Fd()), oldState)
		clearLines(w, s.lastLines)
	}()

	render(w, opts.Title, s)

	buf := make([]byte, 3)
	for {
		n, readErr := in.Read(buf)
		if readErr != nil {
			return -1
		}

		switch {
		// Enter — select current item.
		case n == 1 && (buf[0] == '\r' || buf[0] == '\n'):
			if len(s.filtered) == 0 {
				continue
			}
			return s.filtered[s.cursor].index

		// Ctrl+C.
		case n == 1 && buf[0] == 3:
			return -1

		// Escape — clear filter if active, otherwise quit.
		case n == 1 && buf[0] == 27:
			if s.query != "" {
				clearLines(w, s.lastLines)
				s.query = ""
				s.filtered = s.allItems
				s.cursor = 0
				render(w, opts.Title, s)
			} else {
				return -1
			}

		// Backspace (127 or 8).
		case n == 1 && (buf[0] == 127 || buf[0] == 8):
			if s.query != "" {
				clearLines(w, s.lastLines)
				s.query = s.query[:len(s.query)-1]
				s.filtered = filterItems(s.allItems, s.query)
				s.cursor = 0
				render(w, opts.Title, s)
			}

		// Up arrow (ESC [ A).
		case n == 3 && buf[0] == 27 && buf[1] == '[' && buf[2] == 'A':
			if s.cursor > 0 {
				clearLines(w, s.lastLines)
				s.cursor--
				render(w, opts.Title, s)
			}

		// Down arrow (ESC [ B).
		case n == 3 && buf[0] == 27 && buf[1] == '[' && buf[2] == 'B':
			if s.cursor < len(s.filtered)-1 {
				clearLines(w, s.lastLines)
				s.cursor++
				render(w, opts.Title, s)
			}

		// Printable character — add to search query.
		case n == 1 && buf[0] >= 32 && buf[0] < 127:
			clearLines(w, s.lastLines)
			s.query += string(buf[0])
			s.filtered = filterItems(s.allItems, s.query)
			s.cursor = 0
			render(w, opts.Title, s)
		}
	}
}

func filterItems(items []Item, query string) []Item {
	if query == "" {
		return items
	}
	query = strings.ToLower(query)
	var matched []Item
	for _, item := range items {
		if strings.Contains(strings.ToLower(item.Label), query) {
			matched = append(matched, item)
		}
	}
	return matched
}

func computeLabelWidth(items []Item) int {
	w := len("NAME")
	for _, item := range items {
		if len(item.Label) > w {
			w = len(item.Label)
		}
	}
	return w + 2
}

func padRight(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(s))
}

func render(w io.Writer, title string, s *state) {
	lines := 0

	// Title with search query.
	if s.query != "" {
		_, _ = fmt.Fprintf(w, "  %s %s\r\n",
			output.Dim(title),
			output.Green(s.query))
	} else {
		_, _ = fmt.Fprintf(w, "  %s\r\n", output.Dim(title))
	}
	lines++

	// Header + separator.
	header := fmt.Sprintf("    %s %s", padRight("NAME", s.labelWidth), "PATH")
	_, _ = fmt.Fprintf(w, "  %s\r\n", output.Dim(header))
	_, _ = fmt.Fprintf(w, "  %s\r\n", output.Dim(strings.Repeat("─", len(header))))
	lines += 2

	// Items.
	if len(s.filtered) == 0 {
		_, _ = fmt.Fprintf(w, "  %s\r\n", output.Dim("  no matches"))
		lines++
	} else {
		for i, item := range s.filtered {
			paddedLabel := padRight(item.Label, s.labelWidth)
			if i == s.cursor {
				line := fmt.Sprintf("  › %s %s", paddedLabel, item.Desc)
				_, _ = fmt.Fprintf(w, "%s\r\n", output.Green(line))
			} else {
				_, _ = fmt.Fprintf(w, "    %s %s\r\n",
					paddedLabel,
					output.Dim(item.Desc))
			}
			lines++
		}
	}

	// Help line.
	_, _ = fmt.Fprint(w, "\r\n")
	lines++
	if s.query != "" {
		_, _ = fmt.Fprintf(w, "  %s\r\n", output.Dim("↑↓ navigate · enter select · esc clear · backspace delete"))
	} else {
		_, _ = fmt.Fprintf(w, "  %s\r\n", output.Dim("↑↓ navigate · enter select · type to search · esc quit"))
	}
	lines++

	s.lastLines = lines
}

func clearLines(w io.Writer, n int) {
	for range n {
		_, _ = fmt.Fprint(w, "\x1b[2K") // clear line
		_, _ = fmt.Fprint(w, "\x1b[A")  // move up
	}
	_, _ = fmt.Fprint(w, "\x1b[2K") // clear the top line too
	_, _ = fmt.Fprint(w, "\r")
}

// HasTerminal returns true if the given file is a terminal.
func HasTerminal(f *os.File) bool {
	return term.IsTerminal(int(f.Fd()))
}

// RenderSelected prints a confirmation line showing what was picked.
func RenderSelected(w io.Writer, label, desc string) {
	_, _ = fmt.Fprintf(w, "  %s %s %s\r\n",
		output.Green("›"),
		label,
		output.Dim(desc))
}

// ShowCursor makes the cursor visible again.
func ShowCursor(w io.Writer) {
	_, _ = fmt.Fprint(w, "\x1b[?25h")
}

// HideCursor hides the cursor during picker rendering.
func HideCursor(w io.Writer) {
	_, _ = fmt.Fprint(w, "\x1b[?25l")
}

// FormatItems converts repo names and paths into picker items.
func FormatItems(names, paths []string) []Item {
	items := make([]Item, len(names))
	for i := range names {
		desc := ""
		if i < len(paths) {
			desc = shortenPath(paths[i])
		}
		items[i] = Item{Label: names[i], Desc: desc}
	}
	return items
}

func shortenPath(path string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if strings.HasPrefix(path, home) {
		return "~" + path[len(home):]
	}
	return path
}
