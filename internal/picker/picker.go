// Package picker provides a minimal interactive list picker that renders
// to a terminal. It uses raw terminal mode to read individual keystrokes
// without requiring external TUI dependencies.
package picker

import (
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/CelikE/soko/internal/output"
)

// Item represents a single selectable entry in the picker.
type Item struct {
	Label string
	Desc  string
}

// Options configures the picker appearance.
type Options struct {
	Title string
	Items []Item
}

// Run displays an interactive picker and returns the index of the selected
// item, or -1 if the user cancelled (Ctrl+C / Escape / q).
//
// The picker renders to stderr so stdout remains clean for piping. It reads
// keystrokes from the provided input (typically os.Stdin).
func Run(in *os.File, w io.Writer, opts Options) int {
	oldState, err := term.MakeRaw(int(in.Fd()))
	if err != nil {
		return -1
	}
	defer func() {
		_ = term.Restore(int(in.Fd()), oldState)
		clearLines(w, lineCount(opts))
	}()

	labelWidth := computeLabelWidth(opts.Items)

	cursor := 0
	render(w, opts, cursor, labelWidth)

	buf := make([]byte, 3)
	for {
		n, readErr := in.Read(buf)
		if readErr != nil {
			return -1
		}

		switch {
		// Enter.
		case n == 1 && (buf[0] == '\r' || buf[0] == '\n'):
			return cursor

		// Ctrl+C or Escape or q.
		case n == 1 && (buf[0] == 3 || buf[0] == 27 || buf[0] == 'q'):
			return -1

		// Up arrow (ESC [ A) or k.
		case (n == 3 && buf[0] == 27 && buf[1] == '[' && buf[2] == 'A') || (n == 1 && buf[0] == 'k'):
			if cursor > 0 {
				cursor--
				clearLines(w, lineCount(opts))
				render(w, opts, cursor, labelWidth)
			}

		// Down arrow (ESC [ B) or j.
		case (n == 3 && buf[0] == 27 && buf[1] == '[' && buf[2] == 'B') || (n == 1 && buf[0] == 'j'):
			if cursor < len(opts.Items)-1 {
				cursor++
				clearLines(w, lineCount(opts))
				render(w, opts, cursor, labelWidth)
			}
		}
	}
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

func render(w io.Writer, opts Options, cursor, labelWidth int) {
	// Title.
	_, _ = fmt.Fprintf(w, "  %s\r\n", output.Dim(opts.Title))

	// Header.
	_, _ = fmt.Fprintf(w, "    %s %s\r\n",
		output.Dim(padRight("NAME", labelWidth)),
		output.Dim("PATH"))

	// Items.
	for i, item := range opts.Items {
		paddedLabel := padRight(item.Label, labelWidth)
		if i == cursor {
			_, _ = fmt.Fprintf(w, "  %s %s %s\r\n",
				output.Green("›"),
				output.Green(paddedLabel),
				output.Dim(item.Desc))
		} else {
			_, _ = fmt.Fprintf(w, "    %s %s\r\n",
				paddedLabel,
				output.Dim(item.Desc))
		}
	}

	// Help line.
	_, _ = fmt.Fprint(w, "\r\n")
	_, _ = fmt.Fprintf(w, "  %s\r\n", output.Dim("↑↓ navigate · enter select · q quit"))
}

func lineCount(opts Options) int {
	// title + header + items + blank + help
	return 1 + 1 + len(opts.Items) + 1 + 1
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
