package ui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/CelikE/soko/internal/output"
)

// Styles. Colors are stripped automatically when the renderer's color profile
// is Ascii (set in tests), so View() output stays deterministic for snapshots.
var (
	styleTitle    = lipgloss.NewStyle().Bold(true)
	styleHeader   = lipgloss.NewStyle().Faint(true)
	styleDim      = lipgloss.NewStyle().Faint(true)
	styleGroup    = lipgloss.NewStyle().Bold(true).Faint(true)
	styleCursor   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("10"))
	styleActive   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	styleCrit     = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	styleWarn     = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	styleOK       = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	styleErr      = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	styleFetching = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
)

// View renders the full-screen dashboard.
func (m *Model) View() string {
	if m.quitting {
		return ""
	}
	if m.showHelp {
		return m.helpOverlay()
	}

	var b strings.Builder

	// Title bar.
	b.WriteString("  " + styleTitle.Render("soko ui") + "  " + styleDim.Render(m.statusLine()) + "\n")
	if m.pending == pendingPull {
		name := ""
		if r, ok := m.current(); ok {
			name = r.Name
		}
		b.WriteString("  " + styleWarn.Render(fmt.Sprintf("pull %s? [y/N]", name)) + "\n")
	}
	if m.searching {
		b.WriteString("  " + styleDim.Render("/") + m.query + styleDim.Render("▏") + "\n")
	}
	b.WriteString("\n")

	if !m.loaded {
		b.WriteString("  " + styleDim.Render("loading…") + "\n")
		b.WriteString(m.helpLine())
		return b.String()
	}

	if len(m.view) == 0 {
		b.WriteString("  " + styleDim.Render(m.emptyHint()) + "\n\n")
		b.WriteString(m.tagLegend())
		b.WriteString(m.footer())
		b.WriteString(m.helpLine())
		return b.String()
	}

	b.WriteString(m.table())
	b.WriteString("\n")
	b.WriteString(m.tagLegend())
	b.WriteString(m.footer())
	b.WriteString(m.helpLine())
	return b.String()
}

// statusLine is the right-hand summary on the title bar: sort, active filters,
// grouping, and any in-flight fetch or error.
func (m *Model) statusLine() string {
	parts := []string{"sort:" + m.sort.String()}
	if m.filter != filterAll {
		parts = append(parts, "filter:"+m.filter.String())
	}
	if m.tagFilter != "" {
		parts = append(parts, "tag:"+m.tagFilter)
	}
	if m.grouped {
		parts = append(parts, "grouped")
	}
	if m.query != "" {
		parts = append(parts, "search:"+m.query)
	}
	line := strings.Join(parts, " · ")
	if m.fetching {
		line += "  " + styleFetching.Render("fetching…")
	}
	if m.busy {
		line += "  " + styleFetching.Render("pulling…")
	}
	if m.statusMsg != "" && m.lastErr == nil {
		line += "  " + styleOK.Render(m.statusMsg)
	}
	if m.lastErr != nil {
		line += "  " + styleErr.Render("error: "+m.lastErr.Error())
	}
	return line
}

// emptyHint explains why no rows are showing, so an over-eager filter never
// looks like an empty workspace.
func (m *Model) emptyHint() string {
	switch {
	case m.query != "":
		return fmt.Sprintf("no repos match search %q", m.query)
	case m.filter != filterAll:
		return "no repos match filter: " + m.filter.String()
	case m.tagFilter != "":
		return "no repos tagged: " + m.tagFilter
	default:
		return "no repos to show"
	}
}

// table renders the repo rows with a header. Columns: REPO BRANCH STATUS ↑↓ AGE
// HEALTH — the same vocabulary as soko status plus a health badge. In grouped
// mode a dim tag header precedes each cluster.
func (m *Model) table() string {
	repoW, branchW, statusW, abW := m.columnWidths()

	var b strings.Builder
	header := fmt.Sprintf("  %-*s %-*s %-*s %-*s %-10s %s",
		repoW, "REPO",
		branchW, "BRANCH",
		statusW, "STATUS",
		abW, "↑↓",
		"AGE",
		"HEALTH",
	)
	b.WriteString(styleHeader.Render(header) + "\n")
	b.WriteString(styleHeader.Render("  "+strings.Repeat("─", lipgloss.Width(header)-2)) + "\n")

	counts := m.groupCounts()
	lastGroup := "\x00" // sentinel so the first group always prints a header
	for i := range m.view {
		r := &m.view[i]

		if m.grouped {
			if g := r.firstTag(); g != lastGroup {
				lastGroup = g
				label := g
				if label == "" {
					label = "untagged"
				}
				b.WriteString(styleGroup.Render(fmt.Sprintf("  %s (%d)", label, counts[g])) + "\n")
			}
		}

		marker := "  "
		if i == m.cursor {
			marker = styleCursor.Render("› ")
		}
		age := output.FormatTimeAgo(r.LastCommit)
		line := fmt.Sprintf("%-*s %-*s %-*s %-*s %-10s %s",
			repoW, r.Name,
			branchW, truncate(r.Branch, branchW),
			statusW, r.StatusText,
			abW, output.FormatAheadBehind(r.Ahead, r.Behind),
			truncate(age, 10),
			m.healthBadge(r),
		)
		if i == m.cursor {
			line = styleCursor.Render(line)
		}
		b.WriteString(marker + line + "\n")
	}
	return b.String()
}

// groupCounts tallies the rows in the current view per first-tag group.
func (m *Model) groupCounts() map[string]int {
	counts := map[string]int{}
	for i := range m.view {
		counts[m.view[i].firstTag()]++
	}
	return counts
}

// columnWidths sizes the data-driven columns to their widest cell, with header
// minimums so headers are never clipped.
func (m *Model) columnWidths() (repo, branch, status, ab int) {
	repo, branch, status, ab = len("REPO"), len("BRANCH"), len("STATUS"), len("↑↓")
	for i := range m.view {
		r := &m.view[i]
		repo = max(repo, len(r.Name))
		branch = max(branch, min(len(r.Branch), 24))
		status = max(status, len(r.StatusText))
		ab = max(ab, len(output.FormatAheadBehind(r.Ahead, r.Behind)))
	}
	return repo, branch, status, ab
}

// healthBadge renders the colored severity cell for a row.
func (m *Model) healthBadge(r *Row) string {
	switch r.Severity {
	case "crit":
		return styleCrit.Render(output.SymConflict + " crit")
	case "warn":
		return styleWarn.Render(output.SymWarning + " warn")
	default:
		return styleOK.Render(output.SymClean + " ok")
	}
}

// tagLegend lists every tag with its repo count, highlighting the active tag
// filter. It is the at-a-glance "what tags exist" line.
func (m *Model) tagLegend() string {
	if len(m.allTags) == 0 {
		return ""
	}

	counts := map[string]int{}
	untagged := 0
	for i := range m.all {
		if len(m.all[i].Tags) == 0 {
			untagged++
			continue
		}
		for _, t := range m.all[i].Tags {
			counts[t]++
		}
	}

	tags := make([]string, 0, len(counts))
	for t := range counts {
		tags = append(tags, t)
	}
	sort.Strings(tags)

	parts := make([]string, 0, len(tags)+1)
	for _, t := range tags {
		cell := fmt.Sprintf("%s(%d)", t, counts[t])
		if t == m.tagFilter {
			cell = styleActive.Render(cell)
		}
		parts = append(parts, cell)
	}
	if untagged > 0 {
		parts = append(parts, fmt.Sprintf("untagged(%d)", untagged))
	}

	return styleDim.Render("  tags: ") + strings.Join(parts, styleDim.Render(" · ")) + "\n"
}

// footer shows workspace totals — the same numbers as soko stats' HEALTH block,
// computed from the cheap live signals.
func (m *Model) footer() string {
	var dirty, behind, crit int
	for i := range m.all {
		r := &m.all[i]
		if r.Dirty {
			dirty++
		}
		if r.Behind > 0 {
			behind++
		}
		if r.Severity == "crit" {
			crit++
		}
	}
	shown := ""
	if len(m.view) != len(m.all) {
		shown = fmt.Sprintf("%d shown · ", len(m.view))
	}
	line := fmt.Sprintf("  %s%d %s · %d dirty · %d behind · %d crit",
		shown, len(m.all), output.Plural(len(m.all), "repo"), dirty, behind, crit)
	return styleDim.Render(line) + "\n"
}

// helpLine is the bottom keybinding cheatsheet (the short form; ? opens full).
func (m *Model) helpLine() string {
	help := "  j/k move · enter cd · / search · s sort · f filter · t tag · G group · o open · P pull · g fetch · ? help · q quit"
	return styleDim.Render(help)
}

// helpOverlay is the full keybinding reference shown when ? is pressed.
func (m *Model) helpOverlay() string {
	rows := [][2]string{
		{"j / ↓", "move down"},
		{"k / ↑", "move up"},
		{"enter", "cd to the selected repo (needs shell integration)"},
		{"/", "live search by name"},
		{"s", "cycle sort: name → dirty → behind → health"},
		{"f", "cycle filter: all → dirty → behind → ahead → conflicts"},
		{"t", "cycle tag filter through your tags"},
		{"G", "toggle group-by-tag view"},
		{"o", "open the repo home page in a browser"},
		{"p / i / a", "open pull requests / issues / actions"},
		{"P", "pull the selected repo (fast-forward, confirmed, undoable)"},
		{"g", "re-fetch from remotes now"},
		{"?", "toggle this help"},
		{"q / esc", "quit"},
	}

	var b strings.Builder
	b.WriteString("  " + styleTitle.Render("soko ui — keys") + "\n\n")
	keyW := 0
	for _, r := range rows {
		keyW = max(keyW, len(r[0]))
	}
	for _, r := range rows {
		keyCell := fmt.Sprintf("%-*s", keyW, r[0]) // pad before styling so ANSI never skews width
		b.WriteString("  " + styleActive.Render(keyCell) + "  " + r[1] + "\n")
	}
	b.WriteString("\n" + styleDim.Render("  press any key to close"))
	return b.String()
}

func truncate(s string, maxLen int) string {
	if maxLen <= 0 || len(s) <= maxLen {
		return s
	}
	if maxLen == 1 {
		return "…"
	}
	return s[:maxLen-1] + "…"
}
