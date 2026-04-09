// Package output owns all terminal rendering for soko, including table
// formatting and color helpers.
package output

import (
	"strings"

	"github.com/fatih/color"
)

// Color helper functions. These respect the NO_COLOR environment variable
// automatically via fatih/color.
var (
	// Green formats text in green (clean, in sync).
	Green = color.New(color.FgGreen).SprintFunc()
	// Yellow formats text in yellow (dirty, ahead).
	Yellow = color.New(color.FgYellow).SprintFunc()
	// Red formats text in red (conflicts, behind).
	Red = color.New(color.FgRed).SprintFunc()
	// Dim formats text in a dimmed/faint style.
	Dim = color.New(color.Faint).SprintFunc()
)

// Plural returns word as-is when n == 1, otherwise appends "s" or "es".
func Plural(n int, word string) string {
	if n == 1 {
		return word
	}
	if strings.HasSuffix(word, "ch") || strings.HasSuffix(word, "sh") ||
		strings.HasSuffix(word, "s") || strings.HasSuffix(word, "x") ||
		strings.HasSuffix(word, "z") {
		return word + "es"
	}
	return word + "s"
}

// Symbols used in status output.
const (
	SymClean    = "✓"
	SymModified = "✎"
	SymConflict = "✗"
	SymWarning  = "⚠"
	SymAhead    = "↑"
	SymBehind   = "↓"
	SymInSync   = "·"
)
