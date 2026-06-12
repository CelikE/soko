// Package ui implements `soko ui` — a live, full-screen dashboard of local
// workspace state (dirt, ahead/behind, branch, last-commit age, health). It is
// read-only: navigation (enter) and open-in-browser (o/p/i/a) are the only
// actions, both delegated to injected callbacks so this package owns no git or
// shell I/O.
//
// The model is a standard bubbletea Elm loop. Refresh of local state is driven
// by a cheap timer tick; an optional, slower timer triggers a background fetch.
// Rendering reuses soko's output formatters and lipgloss for styling, so View()
// is a pure function of the model and snapshot-testable.
package ui

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
)

// Row is one repo's live state as shown in the dashboard. The collector fills
// it from a cheap, local-only status read — no network, no disk walk.
type Row struct {
	Name       string
	Branch     string
	Path       string
	Tags       []string
	Dirty      bool
	Changes    int
	Ahead      int
	Behind     int
	Conflicts  int
	LastCommit time.Time
	StatusText string   // pre-formatted dirty marker (output.FormatStatus)
	Health     int      // urgency score, higher = more neglected
	Severity   string   // "ok" | "warn" | "crit"
	Reasons    []string // human-readable health reasons (from scoreRepo)
	WorktreeOf string   // parent repo name when this row is a linked worktree
	Missing    bool
}

// firstTag is the tag a row is grouped under in grouped view; "" means the row
// is untagged (rendered last, under an "untagged" header).
func (r *Row) firstTag() string {
	if len(r.Tags) == 0 {
		return ""
	}
	return r.Tags[0]
}

// Collector returns the current local state of every tracked repo. When fetch
// is true it first fetches from remotes (slow) — the dashboard only sets it on
// the background fetch tick or an explicit `g` keypress.
type Collector func(ctx context.Context, fetch bool) []Row

// Config wires the model to its side effects. Everything I/O-bound is injected
// so the model stays a pure state machine for tests.
type Config struct {
	Ctx          context.Context
	Collect      Collector
	OnSelect     func(path string) error                 // enter — write the shell nav file
	OnOpen       func(path, page string) error           // o/p/i/a — open a repo page in a browser
	OnPull       func(name, path string) (string, error) // P — fast-forward pull the selected repo
	RefreshEvery time.Duration                           // local state refresh cadence
	FetchEvery   time.Duration                           // background fetch cadence; 0 disables
}

// pendingKind is a mutating action awaiting y/n confirmation.
type pendingKind int

const (
	pendingNone pendingKind = iota
	pendingPull
)

// sortMode is the row ordering cycled with `s`.
type sortMode int

const (
	sortName sortMode = iota
	sortDirty
	sortBehind
	sortHealth
	numSortModes
)

func (s sortMode) String() string {
	switch s {
	case sortDirty:
		return "dirty"
	case sortBehind:
		return "behind"
	case sortHealth:
		return "health"
	default:
		return "name"
	}
}

// filterMode is the row filter cycled with `f`.
type filterMode int

const (
	filterAll filterMode = iota
	filterDirty
	filterBehind
	filterAhead
	filterConflicts
	numFilterModes
)

func (f filterMode) String() string {
	switch f {
	case filterDirty:
		return "dirty"
	case filterBehind:
		return "behind"
	case filterAhead:
		return "ahead"
	case filterConflicts:
		return "conflicts"
	default:
		return "all"
	}
}

// match reports whether a row passes the filter.
func (f filterMode) match(r *Row) bool {
	switch f {
	case filterDirty:
		return r.Dirty
	case filterBehind:
		return r.Behind > 0
	case filterAhead:
		return r.Ahead > 0
	case filterConflicts:
		return r.Conflicts > 0
	default:
		return true
	}
}

// Browser page targets passed to OnOpen.
const (
	pageHome    = "home"
	pagePRs     = "prs"
	pageIssues  = "issues"
	pageActions = "actions"
)

// defaultRefresh is used when Config.RefreshEvery is unset. The feature contract
// is "local state every 5s (cheap, no network)".
const defaultRefresh = 5 * time.Second

// defaultWidth backs rendering before the first WindowSizeMsg arrives (and in
// tests, which never send one).
const defaultWidth = 80

// wheelStep is how many rows one mouse-wheel tick moves the cursor.
const wheelStep = 3

// statusTTL is how long a transient status or error line stays on screen
// before a refresh tick clears it.
const statusTTL = 8 * time.Second

// Model is the bubbletea model for the dashboard.
type Model struct {
	ctx      context.Context
	collect  Collector
	onSelect func(path string) error
	onOpen   func(path, page string) error
	onPull   func(name, path string) (string, error)

	all  []Row // last collected, in config order
	view []Row // all after filter + search + sort (+ group ordering)

	cursor     int
	cursorPath string // path of the repo under the cursor; keeps the cursor on
	// the same repo when a refresh re-sorts or re-filters the view
	offset    int // first visible line item (viewport scroll position)
	sort      sortMode
	filter    filterMode
	tagFilter string // "" = all tags
	allTags   []string
	grouped   bool

	searching bool
	query     string

	pending     pendingKind // mutation awaiting confirmation
	pendingName string      // repo the pending mutation targets, pinned at
	pendingPath string      // arm time so a background refresh can't retarget it
	busy        bool        // a mutation is running
	statusMsg   string      // transient result line (e.g. "repo: pulled")
	msgSetAt    time.Time   // when statusMsg/lastErr was set; expires after statusTTL

	showHelp bool

	width, height int
	refreshEvery  time.Duration
	fetchEvery    time.Duration

	fetching  bool
	lastFetch time.Time // when the last remote fetch finished; "" = never
	loaded    bool
	lastErr   error

	selected string // path chosen via enter; read by the caller after Run
	quitting bool
}

// New builds a Model from cfg, applying defaults.
func New(cfg Config) Model {
	ctx := cfg.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	refresh := cfg.RefreshEvery
	if refresh <= 0 {
		refresh = defaultRefresh
	}
	return Model{
		ctx:          ctx,
		collect:      cfg.Collect,
		onSelect:     cfg.OnSelect,
		onOpen:       cfg.OnOpen,
		onPull:       cfg.OnPull,
		refreshEvery: refresh,
		fetchEvery:   cfg.FetchEvery,
		width:        defaultWidth,
	}
}

// Selected returns the repo path chosen with enter, or "" if the user quit
// without selecting. Exposed for tests; the caller normally reads Run's return.
func (m *Model) Selected() string { return m.selected }

// --- messages ---

type tickMsg time.Time
type fetchTickMsg time.Time

// rowsMsg carries one collected frame; fetched marks frames that included a
// remote fetch, so a concurrent cheap refresh can't clear the fetch indicator.
type rowsMsg struct {
	rows    []Row
	fetched bool
}
type pullDoneMsg struct {
	name string
	msg  string
	err  error
}

// --- commands ---

func (m *Model) refreshCmd(fetch bool) tea.Cmd {
	collect := m.collect
	ctx := m.ctx
	return func() tea.Msg {
		return rowsMsg{rows: collect(ctx, fetch), fetched: fetch}
	}
}

func tickCmd(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func fetchTickCmd(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(t time.Time) tea.Msg { return fetchTickMsg(t) })
}

// Init kicks off the first refresh and arms the timers.
func (m *Model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.refreshCmd(false), tickCmd(m.refreshEvery)}
	if m.fetchEvery > 0 {
		cmds = append(cmds, fetchTickCmd(m.fetchEvery))
	}
	return tea.Batch(cmds...)
}

// Update advances the model. Key handling is delegated to handleKey so the
// keybinding → action mapping is unit-testable without a running program.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case rowsMsg:
		m.all = msg.rows
		m.allTags = collectTags(msg.rows)
		// A tag filter whose tag vanished (repo removed) falls back to "all".
		if m.tagFilter != "" && !slices.Contains(m.allTags, m.tagFilter) {
			m.tagFilter = ""
		}
		// Only a fetched frame ends the fetch indicator — a cheap refresh
		// finishing first must not clear it while the fetch is still running.
		if msg.fetched {
			m.fetching = false
			m.lastFetch = time.Now()
		}
		m.loaded = true
		m.rebuild()
		return m, nil

	case tickMsg:
		m.expireStatus()
		// Re-arm the local refresh timer; skip the collect while a fetch is in
		// flight so slow fetches don't pile up concurrent collectors.
		if m.fetching {
			return m, tickCmd(m.refreshEvery)
		}
		return m, tea.Batch(m.refreshCmd(false), tickCmd(m.refreshEvery))

	case fetchTickMsg:
		m.fetching = true
		return m, tea.Batch(m.refreshCmd(true), fetchTickCmd(m.fetchEvery))

	case pullDoneMsg:
		m.busy = false
		if msg.err != nil {
			m.setError(msg.err)
		} else {
			m.setStatus(msg.name + ": " + msg.msg)
		}
		// Reflect the new local state right away.
		cmd := m.refreshCmd(false)
		return m, cmd

	case tea.MouseMsg:
		return m.handleMouse(tea.MouseEvent(msg))

	case tea.KeyMsg:
		return m.handleKey(msg.String())
	}
	return m, nil
}

// handleMouse maps wheel scrolling to cursor movement and a left click to
// selecting the clicked row.
func (m *Model) handleMouse(ev tea.MouseEvent) (tea.Model, tea.Cmd) {
	if m.showHelp || m.searching || m.pending != pendingNone {
		return m, nil
	}
	switch {
	case ev.Button == tea.MouseButtonWheelUp:
		m.moveCursor(-wheelStep)
	case ev.Button == tea.MouseButtonWheelDown:
		m.moveCursor(wheelStep)
	case ev.Button == tea.MouseButtonLeft && ev.Action == tea.MouseActionPress:
		if row, ok := m.rowAtScreenLine(ev.Y); ok {
			m.setCursor(row)
		}
	}
	return m, nil
}

// handleKey applies a single keystroke. Help overlay and search capture input
// first; otherwise the normal-mode bindings run.
func (m *Model) handleKey(key string) (tea.Model, tea.Cmd) {
	if m.showHelp {
		// Any key dismisses help; quit keys still quit.
		if key == "q" || key == "ctrl+c" {
			m.quitting = true
			return m, tea.Quit
		}
		m.showHelp = false
		return m, nil
	}

	if m.pending != pendingNone {
		return m.handleConfirmKey(key)
	}

	if m.searching {
		return m.handleSearchKey(key)
	}

	return m.handleNormalKey(key)
}

// handleConfirmKey resolves a pending mutation: y/enter runs it, anything else
// (n, esc, …) cancels. ctrl+c always quits.
func (m *Model) handleConfirmKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "y", "Y", "enter":
		return m.runPending()
	case "ctrl+c":
		m.quitting = true
		return m, tea.Quit
	default:
		m.clearPending()
		return m, nil
	}
}

// runPending executes the confirmed mutation as an async command, against the
// target pinned when the confirmation was armed.
func (m *Model) runPending() (tea.Model, tea.Cmd) {
	kind := m.pending
	name, path := m.pendingName, m.pendingPath
	m.clearPending()

	if kind == pendingPull {
		if path == "" || m.onPull == nil {
			return m, nil
		}
		m.busy = true
		m.statusMsg = ""
		m.lastErr = nil
		onPull := m.onPull
		return m, func() tea.Msg {
			msg, err := onPull(name, path)
			return pullDoneMsg{name: name, msg: msg, err: err}
		}
	}
	return m, nil
}

// clearPending resets a pending confirmation and its pinned target.
func (m *Model) clearPending() {
	m.pending = pendingNone
	m.pendingName, m.pendingPath = "", ""
}

// setStatus shows a transient success line; it replaces any visible error.
func (m *Model) setStatus(s string) {
	m.statusMsg = s
	m.lastErr = nil
	m.msgSetAt = time.Now()
}

// setError shows a transient error line; it replaces any visible status.
func (m *Model) setError(err error) {
	m.lastErr = err
	m.statusMsg = ""
	m.msgSetAt = time.Now()
}

// expireStatus clears the transient status/error line once it has been on
// screen for statusTTL.
func (m *Model) expireStatus() {
	if !m.msgSetAt.IsZero() && time.Since(m.msgSetAt) > statusTTL {
		m.statusMsg = ""
		m.lastErr = nil
		m.msgSetAt = time.Time{}
	}
}

// handleNormalKey handles the default (non-search, non-help) bindings.
func (m *Model) handleNormalKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "q", "ctrl+c":
		m.quitting = true
		return m, tea.Quit

	case "esc":
		// Unwind state one level before quitting: a committed search first,
		// then active filters, then quit.
		switch {
		case m.query != "":
			m.query = ""
			m.rebuild()
		case m.filter != filterAll || m.tagFilter != "":
			m.filter = filterAll
			m.tagFilter = ""
			m.rebuild()
		default:
			m.quitting = true
			return m, tea.Quit
		}
		return m, nil

	case "?":
		m.showHelp = true
		return m, nil

	case "/":
		m.searching = true
		return m, nil

	case "j", "down":
		m.moveCursor(1)
		return m, nil

	case "k", "up":
		m.moveCursor(-1)
		return m, nil

	case "g", "home":
		m.setCursor(0)
		return m, nil

	case "G", "end":
		m.setCursor(len(m.view) - 1)
		return m, nil

	case "ctrl+d":
		m.moveCursor(m.pageSize() / 2)
		return m, nil

	case "ctrl+u":
		m.moveCursor(-m.pageSize() / 2)
		return m, nil

	case "pgdown":
		m.moveCursor(m.pageSize())
		return m, nil

	case "pgup":
		m.moveCursor(-m.pageSize())
		return m, nil

	case "s":
		m.sort = (m.sort + 1) % numSortModes
		m.rebuild()
		return m, nil

	case "S":
		m.sort = (m.sort - 1 + numSortModes) % numSortModes
		m.rebuild()
		return m, nil

	case "f":
		m.filter = (m.filter + 1) % numFilterModes
		m.rebuild()
		return m, nil

	case "F":
		m.filter = (m.filter - 1 + numFilterModes) % numFilterModes
		m.rebuild()
		return m, nil

	case "t":
		m.cycleTag(1)
		m.rebuild()
		return m, nil

	case "T":
		m.cycleTag(-1)
		m.rebuild()
		return m, nil

	case "b":
		m.grouped = !m.grouped
		m.rebuild()
		return m, nil

	case "r":
		m.fetching = true
		cmd := m.refreshCmd(true)
		return m, cmd

	case "P":
		// Ask before mutating. The target is pinned now so a background
		// refresh that re-sorts the view can't silently retarget the pull.
		if r, ok := m.current(); ok && m.onPull != nil && !m.busy {
			if r.Missing {
				m.setError(errMissing(&r))
				return m, nil
			}
			m.pending = pendingPull
			m.pendingName, m.pendingPath = r.Name, r.Path
		}
		return m, nil

	case "o":
		return m.openCurrent(pageHome)
	case "p":
		return m.openCurrent(pagePRs)
	case "i":
		return m.openCurrent(pageIssues)
	case "a":
		return m.openCurrent(pageActions)

	case "enter":
		return m.selectCurrent()
	}
	return m, nil
}

// handleSearchKey handles input while the live name filter is active.
func (m *Model) handleSearchKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "ctrl+c":
		m.quitting = true
		return m, tea.Quit

	case "esc":
		m.searching = false
		m.query = ""
		m.rebuild()
		return m, nil

	case "enter", "tab":
		// Commit the query and return to normal mode: the filtered list stays
		// and every normal-mode action (j/k, enter, o, P, …) works on it.
		m.searching = false
		return m, nil

	case "backspace":
		if m.query != "" {
			_, size := utf8.DecodeLastRuneInString(m.query)
			m.query = m.query[:len(m.query)-size]
			m.cursor, m.cursorPath = 0, ""
			m.rebuild()
		}
		return m, nil

	case "up":
		m.moveCursor(-1)
		return m, nil
	case "down":
		m.moveCursor(1)
		return m, nil

	default:
		// A single printable rune extends the query; everything else is ignored
		// so search mode never swallows control sequences as text.
		if utf8.RuneCountInString(key) == 1 && key >= " " {
			m.query += key
			m.cursor, m.cursorPath = 0, ""
			m.rebuild()
		}
		return m, nil
	}
}

// openCurrent opens the cursor's repo at the given browser page.
func (m *Model) openCurrent(page string) (tea.Model, tea.Cmd) {
	if r, ok := m.current(); ok && m.onOpen != nil {
		if r.Missing {
			m.setError(errMissing(&r))
			return m, nil
		}
		if err := m.onOpen(r.Path, page); err != nil {
			m.setError(err)
		}
	}
	return m, nil
}

// errMissing explains why an action on a missing repo was refused.
func errMissing(r *Row) error {
	return fmt.Errorf("%s: path not found (%s)", r.Name, r.Path)
}

// selectCurrent writes the nav file for the cursor's repo and quits. On a nav
// write error it keeps the dashboard open and surfaces the error.
func (m *Model) selectCurrent() (tea.Model, tea.Cmd) {
	r, ok := m.current()
	if !ok {
		return m, nil
	}
	if r.Missing {
		m.setError(errMissing(&r))
		return m, nil
	}
	if m.onSelect != nil {
		if err := m.onSelect(r.Path); err != nil {
			m.setError(err)
			return m, nil
		}
	}
	m.selected = r.Path
	m.quitting = true
	return m, tea.Quit
}

// moveCursor shifts the cursor by delta, clamped to the view bounds.
func (m *Model) moveCursor(delta int) {
	m.setCursor(m.cursor + delta)
}

// setCursor places the cursor at index i (clamped) and remembers the repo
// under it, so rebuild can keep following that repo across refreshes.
func (m *Model) setCursor(i int) {
	m.cursor = min(i, len(m.view)-1)
	m.cursor = max(m.cursor, 0)
	if m.cursor < len(m.view) {
		m.cursorPath = m.view[m.cursor].Path
	} else {
		m.cursorPath = ""
	}
}

// current returns the row under the cursor, or false when the view is empty.
func (m *Model) current() (Row, bool) {
	if m.cursor < 0 || m.cursor >= len(m.view) {
		return Row{}, false
	}
	return m.view[m.cursor], true
}

// cycleTag advances the tag filter through ["", tag1, tag2, ...] by dir (+1
// forward, -1 backward) and wraps.
func (m *Model) cycleTag(dir int) {
	if len(m.allTags) == 0 {
		m.tagFilter = ""
		return
	}
	options := append([]string{""}, m.allTags...)
	idx := 0
	for i, opt := range options {
		if opt == m.tagFilter {
			idx = i
			break
		}
	}
	m.tagFilter = options[(idx+dir+len(options))%len(options)]
}

// rebuild recomputes the visible rows from m.all by applying filter, search,
// and tag constraints, then sorting and (optionally) grouping. Finally it
// clamps the cursor.
func (m *Model) rebuild() {
	query := strings.ToLower(m.query)
	rows := make([]Row, 0, len(m.all))
	for i := range m.all {
		r := &m.all[i]
		if !m.filter.match(r) {
			continue
		}
		if m.tagFilter != "" && !slices.Contains(r.Tags, m.tagFilter) {
			continue
		}
		if query != "" && !matchesQuery(r, query) {
			continue
		}
		rows = append(rows, *r)
	}
	sortRows(rows, m.sort)
	if m.grouped {
		groupRows(rows)
	}
	m.view = rows

	// Follow the repo the cursor was on; fall back to clamping the index when
	// that repo left the view (filtered out or removed).
	if m.cursorPath != "" {
		for i := range m.view {
			if m.view[i].Path == m.cursorPath {
				m.cursor = i
				return
			}
		}
	}
	m.setCursor(m.cursor)
}

// matchesQuery reports whether a lowercase query matches the row's name,
// branch, or any tag.
func matchesQuery(r *Row, query string) bool {
	if strings.Contains(strings.ToLower(r.Name), query) ||
		strings.Contains(strings.ToLower(r.Branch), query) {
		return true
	}
	for _, t := range r.Tags {
		if strings.Contains(strings.ToLower(t), query) {
			return true
		}
	}
	return false
}

// sortRows orders rows in place per mode. All modes break ties by name for a
// stable, deterministic order.
func sortRows(rows []Row, mode sortMode) {
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := &rows[i], &rows[j]
		switch mode {
		case sortDirty:
			if a.Changes != b.Changes {
				return a.Changes > b.Changes
			}
		case sortBehind:
			if a.Behind != b.Behind {
				return a.Behind > b.Behind
			}
		case sortHealth:
			if a.Health != b.Health {
				return a.Health > b.Health
			}
		}
		return a.Name < b.Name
	})
}

// groupRows stably reorders already-sorted rows so repos cluster by their first
// tag (alphabetical), with untagged repos last. Intra-group order — the active
// sort — is preserved because the sort is stable.
func groupRows(rows []Row) {
	sort.SliceStable(rows, func(i, j int) bool {
		gi, gj := rows[i].firstTag(), rows[j].firstTag()
		if gi == gj {
			return false
		}
		if gi == "" { // untagged sorts last
			return false
		}
		if gj == "" {
			return true
		}
		return gi < gj
	})
}

// collectTags returns the sorted, unique set of tags across rows.
func collectTags(rows []Row) []string {
	seen := map[string]struct{}{}
	for i := range rows {
		for _, t := range rows[i].Tags {
			seen[t] = struct{}{}
		}
	}
	tags := make([]string, 0, len(seen))
	for t := range seen {
		tags = append(tags, t)
	}
	sort.Strings(tags)
	return tags
}

// Run starts the program in the alternate screen and returns the selected repo
// path (empty if the user quit without choosing).
func Run(m *Model) (string, error) {
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion(), tea.WithContext(m.ctx))
	final, err := p.Run()
	if err != nil {
		return "", err
	}
	fm, ok := final.(*Model)
	if !ok {
		return "", nil
	}
	return fm.selected, nil
}
