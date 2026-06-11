package ui

import (
	"fmt"
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
	styleCursor   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("10"))
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

	var b strings.Builder

	// Title bar.
	title := styleTitle.Render("soko ui")
	b.WriteString("  " + title + "  " + styleDim.Render(m.statusLine()) + "\n\n")

	if !m.loaded {
		b.WriteString("  " + styleDim.Render("loading…") + "\n")
		b.WriteString(m.helpLine())
		return b.String()
	}

	if len(m.view) == 0 {
		b.WriteString("  " + styleDim.Render("no repos match the current filter") + "\n\n")
		b.WriteString(m.footer())
		b.WriteString(m.helpLine())
		return b.String()
	}

	b.WriteString(m.table())
	b.WriteString("\n")
	b.WriteString(m.footer())
	b.WriteString(m.helpLine())
	return b.String()
}

// statusLine is the right-hand summary on the title bar: sort, filters, and any
// in-flight fetch or error.
func (m *Model) statusLine() string {
	parts := []string{"sort:" + m.sort.String()}
	if m.filterDirty {
		parts = append(parts, "filter:dirty")
	}
	if m.tagFilter != "" {
		parts = append(parts, "tag:"+m.tagFilter)
	}
	line := strings.Join(parts, " · ")
	if m.fetching {
		line += "  " + styleFetching.Render("fetching…")
	}
	if m.lastErr != nil {
		line += "  " + styleErr.Render("error: "+m.lastErr.Error())
	}
	return line
}

// table renders the repo rows with a header. Columns: REPO BRANCH STATUS ↑↓ AGE
// HEALTH — the same vocabulary as soko status plus a health badge.
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

	for i := range m.view {
		r := &m.view[i]
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
	line := fmt.Sprintf("  %d %s · %d dirty · %d behind · %d crit",
		len(m.all), output.Plural(len(m.all), "repo"), dirty, behind, crit)
	return styleDim.Render(line) + "\n"
}

// helpLine is the bottom keybinding cheatsheet.
func (m *Model) helpLine() string {
	help := "  j/k move · enter cd · s sort · f dirty · t tag · o open · g fetch · q quit"
	return styleDim.Render(help)
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
