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
	StatusText string // pre-formatted dirty marker (output.FormatStatus)
	Health     int    // urgency score, higher = more neglected
	Severity   string // "ok" | "warn" | "crit"
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

	showHelp bool

	width, height int
	refreshEvery  time.Duration
	fetchEvery    time.Duration

	fetching bool
	loaded   bool
	lastErr  error

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
type rowsMsg struct{ rows []Row }
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
		return rowsMsg{rows: collect(ctx, fetch)}
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
		m.fetching = false
		m.loaded = true
		m.rebuild()
		return m, nil

	case tickMsg:
		// Re-arm the local refresh timer and collect again.
		return m, tea.Batch(m.refreshCmd(false), tickCmd(m.refreshEvery))

	case fetchTickMsg:
		m.fetching = true
		return m, tea.Batch(m.refreshCmd(true), fetchTickCmd(m.fetchEvery))

	case pullDoneMsg:
		m.busy = false
		if msg.err != nil {
			m.lastErr = msg.err
		} else {
			m.statusMsg = msg.name + ": " + msg.msg
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

// handleNormalKey handles the default (non-search, non-help) bindings.
func (m *Model) handleNormalKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "q", "ctrl+c", "esc":
		m.quitting = true
		return m, tea.Quit

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

	case "f":
		m.filter = (m.filter + 1) % numFilterModes
		m.rebuild()
		return m, nil

	case "t":
		m.cycleTag()
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

	case "enter":
		// Confirm the current match (and exit the program via cd).
		return m.selectCurrent()

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
		if err := m.onOpen(r.Path, page); err != nil {
			m.lastErr = err
		}
	}
	return m, nil
}

// selectCurrent writes the nav file for the cursor's repo and quits. On a nav
// write error it keeps the dashboard open and surfaces the error.
func (m *Model) selectCurrent() (tea.Model, tea.Cmd) {
	r, ok := m.current()
	if !ok {
		return m, nil
	}
	if m.onSelect != nil {
		if err := m.onSelect(r.Path); err != nil {
			m.lastErr = err
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

// cycleTag advances the tag filter through ["", tag1, tag2, ...] and wraps.
func (m *Model) cycleTag() {
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
	m.tagFilter = options[(idx+1)%len(options)]
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
		if query != "" && !strings.Contains(strings.ToLower(r.Name), query) {
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
