package output

import (
	"fmt"
	"io"
	"strings"
	"time"
)

// RowState indicates the health of a repo row for colorization.
type RowState int

const (
	// StateClean means the repo is clean and in sync.
	StateClean RowState = iota
	// StateDirty means the repo has uncommitted changes or is ahead.
	StateDirty
	// StateConflict means the repo has conflicts or is significantly behind.
	StateConflict
)

// StatusRow holds everything needed to render one row in the status table.
type StatusRow struct {
	Name            string
	Branch          string
	StatusText      string
	AheadBehindText string
	LastCommitText  string
	State           RowState
}

// columnWidths computes the width for each column based on the actual data,
// with minimum widths so headers are never truncated.
func columnWidths(rows []StatusRow) (repo, branch, status, ab int) {
	repo = len("REPO")
	branch = len("BRANCH")
	status = len("STATUS")
	ab = len("↑↓")

	for _, r := range rows {
		if len(r.Name) > repo {
			repo = len(r.Name)
		}
		if len(r.Branch) > branch {
			branch = len(r.Branch)
		}
		if len(r.StatusText) > status {
			status = len(r.StatusText)
		}
		if len(r.AheadBehindText) > ab {
			ab = len(r.AheadBehindText)
		}
	}

	repo += 2
	branch += 2
	status += 2
	ab += 2

	return repo, branch, status, ab
}

// defaultMaxRows is the max rows shown before truncation. 0 means no limit.
const defaultMaxRows = 25

// RenderStatusTable writes a formatted status table to w.
// If there are more rows than defaultMaxRows, the table is truncated with a hint.
func RenderStatusTable(w io.Writer, rows []StatusRow) {
	RenderStatusTableN(w, rows, defaultMaxRows)
}

// RenderStatusTableN writes a status table with a custom max row limit.
// Pass 0 for maxRows to show all rows.
func RenderStatusTableN(w io.Writer, rows []StatusRow, maxRows int) {
	cRepo, cBranch, cStatus, cAB := columnWidths(rows)

	header := fmt.Sprintf("  %-*s %-*s %-*s %-*s %s",
		cRepo, "REPO",
		cBranch, "BRANCH",
		cStatus, "STATUS",
		cAB, "↑↓",
		"LAST COMMIT",
	)
	_, _ = fmt.Fprintln(w, Dim(header))
	_, _ = fmt.Fprintln(w, Dim("  "+strings.Repeat("─", len(header)-2)))

	truncated := false
	visible := rows
	if maxRows > 0 && len(rows) > maxRows {
		visible = rows[:maxRows]
		truncated = true
	}

	for _, r := range visible {
		line := fmt.Sprintf("  %-*s %-*s %-*s %-*s %s",
			cRepo, r.Name,
			cBranch, r.Branch,
			cStatus, r.StatusText,
			cAB, r.AheadBehindText,
			r.LastCommitText,
		)

		switch r.State {
		case StateClean:
			_, _ = fmt.Fprintln(w, Green(line))
		case StateDirty:
			_, _ = fmt.Fprintln(w, Yellow(line))
		case StateConflict:
			_, _ = fmt.Fprintln(w, Red(line))
		default:
			_, _ = fmt.Fprintln(w, line)
		}
	}

	if truncated {
		_, _ = fmt.Fprintln(w, Dim(fmt.Sprintf(
			"  ... showing %d of %d %s — use --all to show all, or --tag/--dirty to filter",
			maxRows, len(rows), Plural(len(rows), "repo"))))
	}
}

// StatusGroup holds a tag name and its associated status rows.
type StatusGroup struct {
	Tag  string
	Rows []StatusRow
}

// RenderStatusGrouped writes a status table grouped by tag to w.
func RenderStatusGrouped(w io.Writer, groups []StatusGroup) {
	// Collect all rows to compute column widths.
	var allRows []StatusRow
	for _, g := range groups {
		allRows = append(allRows, g.Rows...)
	}
	cRepo, cBranch, cStatus, cAB := columnWidths(allRows)

	// Header — offset by 6 chars to account for tree connector "  └── ".
	header := fmt.Sprintf("        %-*s %-*s %-*s %-*s %s",
		cRepo, "REPO",
		cBranch, "BRANCH",
		cStatus, "STATUS",
		cAB, "↑↓",
		"LAST COMMIT",
	)
	_, _ = fmt.Fprintln(w, Dim(header))
	_, _ = fmt.Fprintln(w, Dim("  "+strings.Repeat("─", len(header)-2)))

	for i, g := range groups {
		if i > 0 {
			_, _ = fmt.Fprintln(w)
		}
		_, _ = fmt.Fprintln(w, "  "+Dim(g.Tag))

		for j, r := range g.Rows {
			connector := "├──"
			if j == len(g.Rows)-1 {
				connector = "└──"
			}

			line := fmt.Sprintf("  %s %-*s %-*s %-*s %-*s %s",
				Dim(connector),
				cRepo, r.Name,
				cBranch, r.Branch,
				cStatus, r.StatusText,
				cAB, r.AheadBehindText,
				r.LastCommitText,
			)

			switch r.State {
			case StateClean:
				_, _ = fmt.Fprintln(w, Green(line))
			case StateDirty:
				_, _ = fmt.Fprintln(w, Yellow(line))
			case StateConflict:
				_, _ = fmt.Fprintln(w, Red(line))
			default:
				_, _ = fmt.Fprintln(w, line)
			}
		}
	}
}

// RenderSummary writes the status summary line to w.
func RenderSummary(w io.Writer, totalRepos, dirtyCount, behindCount, totalChanges int) {
	_, _ = fmt.Fprintf(w, "\n  %s\n", Dim(fmt.Sprintf(
		"%d %s · %d dirty · %d behind · %d %s",
		totalRepos, Plural(totalRepos, "repo"),
		dirtyCount, behindCount,
		totalChanges, Plural(totalChanges, "change"),
	)))
}

// ActionRow holds the result of an action (fetch, exec, etc.) for one repo.
type ActionRow struct {
	Name    string
	Success bool
	Message string
}

// RenderActionResults writes a list of action results to w.
func RenderActionResults(w io.Writer, rows []ActionRow) {
	nameWidth := 0
	for _, r := range rows {
		if len(r.Name) > nameWidth {
			nameWidth = len(r.Name)
		}
	}
	nameWidth += 2

	for _, r := range rows {
		if r.Success {
			_, _ = fmt.Fprintln(w, Green(fmt.Sprintf(
				"  %s %-*s %s", SymClean, nameWidth, r.Name, r.Message)))
		} else {
			_, _ = fmt.Fprintln(w, Red(fmt.Sprintf(
				"  %s %-*s %s", SymConflict, nameWidth, r.Name, r.Message)))
		}
	}
}

// RenderActionSummary writes a summary line for action commands to w.
func RenderActionSummary(w io.Writer, total, ok, failed int) {
	_, _ = fmt.Fprintf(w, "\n  %s\n", Dim(fmt.Sprintf(
		"%d %s · %d ok · %d failed", total, Plural(total, "repo"), ok, failed)))
}

// Confirm prints a success confirmation message.
func Confirm(w io.Writer, message string) {
	_, _ = fmt.Fprintf(w, "  %s %s\n", Green(SymClean), message)
}

// Warn prints a warning message.
func Warn(w io.Writer, message string) {
	_, _ = fmt.Fprintf(w, "  %s %s\n", Yellow(SymWarning), message)
}

// Fail prints an error message.
func Fail(w io.Writer, message string) {
	_, _ = fmt.Fprintf(w, "  %s %s\n", Red(SymConflict), message)
}

// Info prints an informational message (dimmed).
func Info(w io.Writer, message string) {
	_, _ = fmt.Fprintf(w, "  %s\n", Dim(message))
}

// FetchRow is an alias for ActionRow for backward compatibility.
type FetchRow = ActionRow

// RenderFetchTable renders fetch results using the unified action format.
func RenderFetchTable(w io.Writer, rows []FetchRow) {
	RenderActionResults(w, rows)
}

// RenderFetchSummary renders the fetch summary using the unified format.
func RenderFetchSummary(w io.Writer, total, fetched, failed int) {
	RenderActionSummary(w, total, fetched, failed)
}

// FormatStatus returns a compact status string from file counts.
func FormatStatus(modified, untracked, deleted, conflicts int) string {
	if conflicts > 0 {
		return fmt.Sprintf("%s %dC", SymConflict, conflicts)
	}

	total := modified + untracked + deleted
	if total == 0 {
		return SymClean + " clean"
	}

	var parts []string
	if modified > 0 {
		parts = append(parts, fmt.Sprintf("%dM", modified))
	}
	if untracked > 0 {
		parts = append(parts, fmt.Sprintf("%dU", untracked))
	}
	if deleted > 0 {
		parts = append(parts, fmt.Sprintf("%dD", deleted))
	}

	return SymModified + " " + strings.Join(parts, " ")
}

// FormatAheadBehind returns a compact ahead/behind string.
func FormatAheadBehind(ahead, behind int) string {
	if ahead == 0 && behind == 0 {
		return SymInSync
	}

	var parts []string
	if ahead > 0 {
		parts = append(parts, fmt.Sprintf("%s%d", SymAhead, ahead))
	}
	if behind > 0 {
		parts = append(parts, fmt.Sprintf("%s%d", SymBehind, behind))
	}

	return strings.Join(parts, " ")
}

// FormatTimeAgo returns a human-readable relative time string.
func FormatTimeAgo(t time.Time) string {
	if t.IsZero() {
		return "-"
	}

	d := time.Since(t)

	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		m := int(d.Minutes())
		return fmt.Sprintf("%dm ago", m)
	case d < 24*time.Hour:
		h := int(d.Hours())
		return fmt.Sprintf("%dh ago", h)
	case d < 7*24*time.Hour:
		days := int(d.Hours() / 24)
		return fmt.Sprintf("%dd ago", days)
	default:
		weeks := int(d.Hours() / (24 * 7))
		return fmt.Sprintf("%dw ago", weeks)
	}
}

// FormatLastCommit returns a combined time-ago and truncated commit message.
func FormatLastCommit(t time.Time, message string) string {
	timeStr := FormatTimeAgo(t)
	if message == "" {
		return timeStr
	}
	return timeStr + "  " + truncate(message, 30)
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-1] + "…"
}
