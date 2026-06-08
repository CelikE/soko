package output

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// perf, when true, makes parallel commands report per-repo and aggregate
// timing after their normal output. It is process-global state set once from
// the --perf flag (or SOKO_PERF) in the root pre-run, mirroring quiet.
var perf bool

// SetPerf enables or disables timing output for parallel commands.
func SetPerf(p bool) { perf = p }

// Perf reports whether --perf is active.
func Perf() bool { return perf }

// perfMaxRows caps the rows shown in the timing breakdown (and the JSON
// "slowest" list), matching defaultMaxRows used elsewhere.
const perfMaxRows = 25

// perfBarWidth is the width in cells of the longest (slowest) timing bar.
const perfBarWidth = 16

// TimingRow is one repo's measured duration for the --perf breakdown.
type TimingRow struct {
	Name     string
	Duration time.Duration
}

// FormatDuration renders d as "<n>ms" below one second, otherwise "<n.n>s"
// with a single decimal — e.g. 88ms, 412ms, 1.3s.
func FormatDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}

// RenderTiming writes the slowest-first timing breakdown to w: a header with
// the repo count and max concurrency, a bar per repo (slowest first, capped at
// perfMaxRows with a "… N more" line), and a "wall … · git …" footer where git
// is the summed per-repo duration. It is a no-op when perf is off or rows is
// empty, so callers can invoke it unconditionally.
func RenderTiming(w io.Writer, rows []TimingRow, wall time.Duration, concurrency int) {
	if !perf || len(rows) == 0 {
		return
	}

	sorted := make([]TimingRow, len(rows))
	copy(sorted, rows)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].Duration > sorted[j].Duration
	})

	var git time.Duration
	for _, r := range sorted {
		git += r.Duration
	}
	maxDur := sorted[0].Duration

	header := fmt.Sprintf("timing — %d %s · %d max concurrency",
		len(rows), Plural(len(rows), "repo"), concurrency)

	visible := sorted
	hidden := 0
	if len(sorted) > perfMaxRows {
		visible = sorted[:perfMaxRows]
		hidden = len(sorted) - perfMaxRows
	}

	// Width of the name column and the right-aligned duration column.
	nameW, durW := 0, 0
	for _, r := range visible {
		if l := utf8.RuneCountInString(r.Name); l > nameW {
			nameW = l
		}
		if l := len(FormatDuration(r.Duration)); l > durW {
			durW = l
		}
	}

	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintf(w, "  %s\n", Dim(header))
	_, _ = fmt.Fprintf(w, "  %s\n", Dim(strings.Repeat("─", utf8.RuneCountInString(header))))
	for _, r := range visible {
		bar := ""
		if maxDur > 0 && r.Duration > 0 {
			n := max(int(float64(r.Duration)/float64(maxDur)*float64(perfBarWidth)), 1)
			bar = strings.Repeat("█", n)
		}
		_, _ = fmt.Fprintf(w, "  %-*s  %*s  %s\n",
			nameW, r.Name, durW, FormatDuration(r.Duration), bar)
	}
	if hidden > 0 {
		_, _ = fmt.Fprintf(w, "  %s\n", Dim(fmt.Sprintf("… %d more", hidden)))
	}
	_, _ = fmt.Fprintf(w, "  %s\n", Dim(fmt.Sprintf(
		"wall %s · git %s", FormatDuration(wall), FormatDuration(git))))
}

// SlowestEntry is one repo in the JSON "slowest" list.
type SlowestEntry struct {
	Name       string `json:"name"`
	DurationMS int64  `json:"duration_ms"`
}

// TimingEnvelope is the "timing" block added to JSON output under --perf.
type TimingEnvelope struct {
	WallMS      int64          `json:"wall_ms"`
	GitMS       int64          `json:"git_ms"`
	Repos       int            `json:"repos"`
	Concurrency int            `json:"concurrency"`
	Slowest     []SlowestEntry `json:"slowest"`
}

// BuildTiming assembles the JSON timing envelope from per-repo durations:
// git_ms is the summed per-repo time, repos is the count, and slowest lists the
// repos sorted slowest-first (capped at perfMaxRows).
func BuildTiming(rows []TimingRow, wall time.Duration, concurrency int) TimingEnvelope {
	sorted := make([]TimingRow, len(rows))
	copy(sorted, rows)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].Duration > sorted[j].Duration
	})

	var git time.Duration
	for _, r := range sorted {
		git += r.Duration
	}

	limit := min(len(sorted), perfMaxRows)
	slowest := make([]SlowestEntry, limit)
	for i := range limit {
		slowest[i] = SlowestEntry{Name: sorted[i].Name, DurationMS: sorted[i].Duration.Milliseconds()}
	}

	return TimingEnvelope{
		WallMS:      wall.Milliseconds(),
		GitMS:       git.Milliseconds(),
		Repos:       len(rows),
		Concurrency: concurrency,
		Slowest:     slowest,
	}
}

// RenderPerfJSON emits the --perf --json document: the per-repo entries wrapped
// in a "repos" array next to a "timing" envelope.
func RenderPerfJSON(w io.Writer, repos any, timing TimingEnvelope) error {
	return RenderJSON(w, struct {
		Repos  any            `json:"repos"`
		Timing TimingEnvelope `json:"timing"`
	}{Repos: repos, Timing: timing})
}
