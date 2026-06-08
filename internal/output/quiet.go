package output

// quiet, when true, suppresses the non-essential chrome soko prints on every
// run — info lines, warnings/hints, progress, and the trailing summary footers.
// Tables, RenderJSON, Confirm, and Fail are never gated. It is process-global
// state set once from the --quiet flag (or SOKO_QUIET) in the root pre-run.
var quiet bool

// SetQuiet enables or disables quiet mode for all human-facing output. When
// enabled, Info, Warn, the summary footers, and Progress become no-ops; tables,
// RenderJSON, Confirm, and Fail are unaffected.
func SetQuiet(q bool) { quiet = q }

// Quiet reports whether quiet mode is active.
func Quiet() bool { return quiet }
