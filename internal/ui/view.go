package ui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

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
		b.WriteString("  " + styleWarn.Render(fmt.Sprintf("pull %s? [y/N]", m.pendingName)) + "\n")
	}
	if m.searching {
		b.WriteString("  " + styleDim.Render("/") + m.query + styleDim.Render("▏") + "\n")
	}
	b.WriteString("\n")

	if !m.loaded {
		b.WriteString("  " + styleDim.Render("loading…") + "\n")
		b.WriteString(m.helpLine())
		return clampWidth(b.String(), m.width)
	}

	if len(m.view) == 0 {
		b.WriteString("  " + styleDim.Render(m.emptyHint()) + "\n\n")
		b.WriteString(m.tagLegend())
		b.WriteString(m.footer(0, 0))
		b.WriteString(m.helpLine())
		return clampWidth(b.String(), m.width)
	}

	items := m.buildItems()
	start, end := m.scrollWindow(items)
	b.WriteString(m.table(items[start:end]))
	b.WriteString("\n")
	b.WriteString(m.detailLine())
	b.WriteString(m.tagLegend())
	b.WriteString(m.footer(start, end))
	b.WriteString(m.helpLine())
	return clampWidth(b.String(), m.width)
}

// detailLine expands the cursor's repo below the table: full path, health
// reasons, and worktree parentage — detail the columns have no room for.
func (m *Model) detailLine() string {
	r, ok := m.current()
	if !ok {
		return ""
	}
	parts := []string{r.Path}
	if len(r.Reasons) > 0 {
		parts = append(parts, strings.Join(r.Reasons, ", "))
	}
	if r.WorktreeOf != "" {
		parts = append(parts, "worktree of "+r.WorktreeOf)
	}
	return styleDim.Render("  › "+r.Name+" — "+strings.Join(parts, " · ")) + "\n"
}

// lineItem is one renderable table line: either a repo row (row >= 0, an index
// into m.view) or a group header (row == -1) in grouped mode. The viewport
// scrolls over these items so group headers consume screen budget too.
type lineItem struct {
	row   int    // index into m.view, or -1 for a group header
	group string // header label's tag ("" = untagged) when row == -1
}

// buildItems flattens the current view into renderable line items, inserting a
// group header before each first-tag cluster in grouped mode.
func (m *Model) buildItems() []lineItem {
	items := make([]lineItem, 0, len(m.view)+8)
	lastGroup := "\x00" // sentinel so the first group always gets a header
	for i := range m.view {
		if m.grouped {
			if g := m.view[i].firstTag(); g != lastGroup {
				lastGroup = g
				items = append(items, lineItem{row: -1, group: g})
			}
		}
		items = append(items, lineItem{row: i})
	}
	return items
}

// tableCapacity is how many table line items fit on screen after the fixed
// chrome (title, banners, header, legend, footer, help). Unknown height (tests,
// pre-WindowSizeMsg) means no clamping.
func (m *Model) tableCapacity() int {
	if m.height <= 0 {
		return len(m.view) * 2 // unbounded: rows + worst-case group headers
	}
	chrome := 8 // title, blank, 2 table header lines, blank, detail, footer, help
	if m.pending != pendingNone {
		chrome++
	}
	if m.searching {
		chrome++
	}
	if len(m.allTags) > 0 {
		chrome++ // tag legend
	}
	return max(m.height-chrome, 1)
}

// pageSize is the viewport height in rows, used by paging keys.
func (m *Model) pageSize() int {
	return max(m.tableCapacity(), 1)
}

// scrollWindow clamps m.offset so the cursor's line item stays visible and
// returns the visible item range. When the cursor row sits directly under its
// group header, the header is pulled into view with it.
func (m *Model) scrollWindow(items []lineItem) (start, end int) {
	capacity := m.tableCapacity()

	cursorItem := 0
	for i := range items {
		if items[i].row == m.cursor {
			cursorItem = i
			break
		}
	}

	if m.offset > cursorItem {
		m.offset = cursorItem
	}
	if cursorItem-m.offset >= capacity {
		m.offset = cursorItem - capacity + 1
	}
	// Keep the cursor's group header attached when scrolling up to its row.
	if m.offset == cursorItem && m.offset > 0 && items[m.offset-1].row == -1 {
		m.offset--
	}
	m.offset = min(m.offset, max(len(items)-capacity, 0))
	m.offset = max(m.offset, 0)

	return m.offset, min(len(items), m.offset+capacity)
}

// tableTopLines is the number of screen lines above the first table item:
// title, optional banners, the blank separator, and the two header lines.
func (m *Model) tableTopLines() int {
	top := 4 // title + blank + 2 table header lines
	if m.pending != pendingNone {
		top++
	}
	if m.searching {
		top++
	}
	return top
}

// rowAtScreenLine maps a terminal line (0-based, from a mouse click) to the
// view row rendered there.
func (m *Model) rowAtScreenLine(y int) (int, bool) {
	if !m.loaded || len(m.view) == 0 {
		return 0, false
	}
	items := m.buildItems()
	start, end := m.scrollWindow(items)
	idx := start + (y - m.tableTopLines())
	if idx < start || idx >= end || items[idx].row < 0 {
		return 0, false
	}
	return items[idx].row, true
}

// clampWidth truncates every rendered line to the terminal width (ANSI-aware)
// so long rows never wrap and break the table layout.
func clampWidth(s string, width int) string {
	if width <= 0 {
		return s
	}
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		if lipgloss.Width(line) > width {
			lines[i] = ansi.Truncate(line, width-1, "…")
		}
	}
	return strings.Join(lines, "\n")
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
	// While typing, the search input line already shows the query.
	if m.query != "" && !m.searching {
		parts = append(parts, "search:"+m.query)
	}
	line := strings.Join(parts, " · ")
	if m.fetching {
		line += "  " + styleFetching.Render("fetching…")
	} else if !m.lastFetch.IsZero() {
		line += "  " + "fetched " + output.FormatTimeAgo(m.lastFetch)
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

// table renders the visible window of line items under a column header.
// Columns: REPO BRANCH STATUS ↑↓ AGE HEALTH — the same vocabulary as soko
// status plus a health badge. In grouped mode a dim tag header precedes each
// cluster.
func (m *Model) table(items []lineItem) string {
	repoW, branchW, statusW, abW := m.columnWidths()

	var b strings.Builder
	header := "  " + pad("REPO", repoW) + " " + pad("BRANCH", branchW) + " " +
		pad("STATUS", statusW) + " " + pad("↑↓", abW) + " " + pad("AGE", ageW) + " HEALTH"
	b.WriteString(styleHeader.Render(header) + "\n")
	b.WriteString(styleHeader.Render("  "+strings.Repeat("─", lipgloss.Width(header)-2)) + "\n")

	counts := m.groupCounts()
	for _, it := range items {
		if it.row < 0 {
			label := it.group
			if label == "" {
				label = "untagged"
			}
			b.WriteString(styleGroup.Render(fmt.Sprintf("  %s (%d)", label, counts[it.group])) + "\n")
			continue
		}

		r := &m.view[it.row]
		marker := "  "
		if it.row == m.cursor {
			marker = styleCursor.Render("› ")
		}
		age := output.FormatTimeAgo(r.LastCommit)
		line := pad(displayName(r), repoW) + " " +
			pad(truncate(r.Branch, branchW), branchW) + " " +
			pad(r.StatusText, statusW) + " " +
			pad(output.FormatAheadBehind(r.Ahead, r.Behind), abW) + " " +
			pad(truncate(age, ageW), ageW) + " " +
			m.healthBadge(r)
		if it.row == m.cursor {
			line = styleCursor.Render(line)
		}
		b.WriteString(marker + line + "\n")
	}
	return b.String()
}

// ageW is the fixed width of the AGE column ("102w ago" fits).
const ageW = 10

// displayName is the REPO cell: linked worktrees get a marker so duplicate
// names (a repo and its worktrees) are distinguishable.
func displayName(r *Row) string {
	if r.WorktreeOf != "" {
		return "↳ " + r.Name
	}
	return r.Name
}

// pad right-pads s with spaces to display width w (ANSI- and rune-aware, so
// multibyte symbols never skew the columns).
func pad(s string, w int) string {
	if gap := w - lipgloss.Width(s); gap > 0 {
		return s + strings.Repeat(" ", gap)
	}
	return s
}

// groupCounts tallies the rows in the current view per first-tag group.
func (m *Model) groupCounts() map[string]int {
	counts := map[string]int{}
	for i := range m.view {
		counts[m.view[i].firstTag()]++
	}
	return counts
}

// columnWidths sizes the data-driven columns to their widest cell (in display
// cells, not bytes), with header minimums so headers are never clipped.
func (m *Model) columnWidths() (repo, branch, status, ab int) {
	repo, branch, status, ab = lipgloss.Width("REPO"), lipgloss.Width("BRANCH"), lipgloss.Width("STATUS"), lipgloss.Width("↑↓")
	for i := range m.view {
		r := &m.view[i]
		repo = max(repo, lipgloss.Width(displayName(r)))
		branch = max(branch, min(lipgloss.Width(r.Branch), 24))
		status = max(status, lipgloss.Width(r.StatusText))
		ab = max(ab, lipgloss.Width(output.FormatAheadBehind(r.Ahead, r.Behind)))
	}
	return repo, branch, status, ab
}

// healthBadge renders the colored severity cell for a row.
func (m *Model) healthBadge(r *Row) string {
	if r.Missing {
		return styleCrit.Render(output.SymConflict + " missing")
	}
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
		if m.grouped {
			// Count by first tag so the legend matches the group headers.
			counts[m.all[i].firstTag()]++
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
// computed from the cheap live signals. When the viewport clips the table it
// also shows which slice of the list is on screen.
func (m *Model) footer(start, end int) string {
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
	scroll := ""
	if total := len(m.buildItems()); end-start < total {
		scroll = fmt.Sprintf(" · lines %d–%d/%d", start+1, end, total)
	}
	line := fmt.Sprintf("  %s%d %s · %d dirty · %d behind · %d crit%s",
		shown, len(m.all), output.Plural(len(m.all), "repo"), dirty, behind, crit, scroll)
	return styleDim.Render(line) + "\n"
}

// helpLine is the bottom keybinding cheatsheet (the short form; ? opens full).
func (m *Model) helpLine() string {
	help := "  j/k move · g/G top/bottom · enter cd · / search · s/S sort · f/F filter · t/T tag · b group · o open · P pull · r fetch · esc clear · ? help · q quit"
	return styleDim.Render(help)
}

// helpOverlay is the full keybinding reference shown when ? is pressed.
func (m *Model) helpOverlay() string {
	rows := [][2]string{
		{"j / ↓", "move down"},
		{"k / ↑", "move up"},
		{"g / home", "jump to the top"},
		{"G / end", "jump to the bottom"},
		{"ctrl+d / ctrl+u", "half page down / up"},
		{"pgdn / pgup", "page down / up"},
		{"enter", "cd to the selected repo (needs shell integration)"},
		{"/", "live search across name, branch, and tags"},
		{"enter / tab", "(in search) keep the filter and return to the list"},
		{"s / S", "cycle sort forward/back: name → dirty → behind → health"},
		{"f / F", "cycle filter forward/back: all → dirty → behind → ahead → conflicts"},
		{"t / T", "cycle tag filter forward/back through your tags"},
		{"b", "toggle group-by-tag view"},
		{"o", "open the repo home page in a browser"},
		{"p / i / a", "open pull requests / issues / actions"},
		{"P", "pull the selected repo (fast-forward, confirmed, undoable)"},
		{"r", "re-fetch from remotes now"},
		{"mouse", "wheel scrolls · click selects a row"},
		{"?", "toggle this help"},
		{"esc", "clear search, then filters, then quit"},
		{"q", "quit"},
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

// truncate cuts s to maxW display cells with an ellipsis, never splitting a
// rune (ANSI-aware via x/ansi).
func truncate(s string, maxW int) string {
	if maxW <= 0 || lipgloss.Width(s) <= maxW {
		return s
	}
	if maxW == 1 {
		return "…"
	}
	return ansi.Truncate(s, maxW-1, "…")
}
