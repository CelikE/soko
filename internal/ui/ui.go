// Package ui implements `soko ui` — a live, full-screen dashboard of local
// workspace state (dirt, ahead/behind, branch, last-commit age, health).
// Every side effect — navigation (enter), open-in-browser (o/p/i/a), pull
// (P), undo (u), fetch (r/R), clipboard (y) — is delegated to injected
// callbacks, so this package owns no git or shell I/O and mutations are
// confirmed in-model before their callback runs.
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

	"github.com/CelikE/soko/internal/browser"
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
// the background fetch tick or an explicit `r` keypress.
type Collector func(ctx context.Context, fetch bool) []Row

// PullTarget identifies one repo a pull acts on.
type PullTarget struct {
	Name string
	Path string
}

// Config wires the model to its side effects. Everything I/O-bound is injected
// so the model stays a pure state machine for tests.
type Config struct {
	Ctx          context.Context
	Collect      Collector
	OnSelect     func(path string) error                    // enter — write the shell nav file
	OnOpen       func(path string, page browser.Page) error // o/p/i/a — open a repo page in a browser
	OnPull       func(repos []PullTarget) (string, error)   // P — fast-forward pull (cursor or marked repos)
	OnUndo       func() (string, error)                     // u — undo the last recorded pull
	OnFetchRepo  func(path string) error                    // R — fetch one repo from its remotes
	OnCopy       func(text string) error                    // y — copy the repo path to the clipboard
	RefreshEvery time.Duration                              // local state refresh cadence
	FetchEvery   time.Duration                              // background fetch cadence; 0 disables
}

// pendingKind is a mutating action awaiting y/n confirmation.
type pendingKind int

const (
	pendingNone pendingKind = iota
	pendingPull
	pendingUndo
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

// Severity labels carried in Row.Severity (the health scorer's vocabulary).
const (
	SevOK   = "ok"
	SevWarn = "warn"
	SevCrit = "crit"
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
	ctx         context.Context
	collect     Collector
	onSelect    func(path string) error
	onOpen      func(path string, page browser.Page) error
	onPull      func(repos []PullTarget) (string, error)
	onUndo      func() (string, error)
	onFetchRepo func(path string) error
	onCopy      func(text string) error

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

	marked map[string]bool // paths marked with space for batch actions

	pending      pendingKind  // mutation awaiting confirmation
	pendingPulls []PullTarget // pull targets pinned at arm time so a background
	// refresh can't retarget the confirmed action
	busy      bool      // a mutation is running
	quitArmed bool      // q pressed once while busy; next q quits anyway
	statusMsg string    // transient result line (e.g. "repo: pulled")
	msgSetAt  time.Time // when statusMsg/lastErr was set; expires after statusTTL

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
func New(cfg *Config) Model {
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
		onUndo:       cfg.OnUndo,
		onFetchRepo:  cfg.OnFetchRepo,
		onCopy:       cfg.OnCopy,
		marked:       map[string]bool{},
		refreshEvery: refresh,
		fetchEvery:   cfg.FetchEvery,
		width:        defaultWidth,
	}
}

// --- messages ---

type tickMsg time.Time
type fetchTickMsg time.Time

// rowsMsg carries one collected frame; fetched marks frames that included a
// remote fetch, so a concurrent cheap refresh can't clear the fetch indicator.
type rowsMsg struct {
	rows    []Row
	fetched bool
}

// actionDoneMsg reports the outcome of an async mutation (pull, undo): a
// human status line on success, an error otherwise.
type actionDoneMsg struct {
	msg string
	err error
}

// fetchRepoDoneMsg reports a single-repo fetch (R key).
type fetchRepoDoneMsg struct {
	name string
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
		// Drop marks for repos that left the workspace.
		if len(m.marked) > 0 {
			live := make(map[string]bool, len(msg.rows))
			for i := range msg.rows {
				live[msg.rows[i].Path] = true
			}
			for p := range m.marked {
				if !live[p] {
					delete(m.marked, p)
				}
			}
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

	case actionDoneMsg:
		m.busy = false
		if msg.err != nil {
			m.setError(msg.err)
		} else {
			m.setStatus(msg.msg)
		}
		// Reflect the new local state right away.
		cmd := m.refreshCmd(false)
		return m, cmd

	case fetchRepoDoneMsg:
		if msg.err != nil {
			m.setError(msg.err)
			return m, nil
		}
		m.setStatus(msg.name + ": fetched")
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
// targets pinned when the confirmation was armed.
func (m *Model) runPending() (tea.Model, tea.Cmd) {
	kind := m.pending
	targets := m.pendingPulls
	m.clearPending()

	switch kind {
	case pendingPull:
		if len(targets) == 0 || m.onPull == nil {
			return m, nil
		}
		m.busy = true
		m.statusMsg = ""
		m.lastErr = nil
		for _, t := range targets {
			delete(m.marked, t.Path)
		}
		onPull := m.onPull
		return m, func() tea.Msg {
			msg, err := onPull(targets)
			return actionDoneMsg{msg: msg, err: err}
		}

	case pendingUndo:
		if m.onUndo == nil {
			return m, nil
		}
		m.busy = true
		m.statusMsg = ""
		m.lastErr = nil
		onUndo := m.onUndo
		return m, func() tea.Msg {
			msg, err := onUndo()
			return actionDoneMsg{msg: msg, err: err}
		}
	}
	return m, nil
}

// clearPending resets a pending confirmation and its pinned targets.
func (m *Model) clearPending() {
	m.pending = pendingNone
	m.pendingPulls = nil
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
	if key != "q" && key != "esc" {
		m.quitArmed = false
	}
	switch key {
	case "ctrl+c":
		m.quitting = true
		return m, tea.Quit

	case "q":
		if cmd, quit := m.tryQuit(); quit {
			return m, cmd
		}
		return m, nil

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
			if cmd, quit := m.tryQuit(); quit {
				return m, cmd
			}
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

	case " ":
		// Mark/unmark the cursor row for batch actions, then advance.
		if r, ok := m.current(); ok && !r.Missing {
			if m.marked[r.Path] {
				delete(m.marked, r.Path)
			} else {
				m.marked[r.Path] = true
			}
			m.moveCursor(1)
		}
		return m, nil

	case "*":
		// Mark every visible repo; if all are already marked, unmark them.
		all := true
		for i := range m.view {
			if !m.view[i].Missing && !m.marked[m.view[i].Path] {
				all = false
				break
			}
		}
		for i := range m.view {
			if m.view[i].Missing {
				continue
			}
			if all {
				delete(m.marked, m.view[i].Path)
			} else {
				m.marked[m.view[i].Path] = true
			}
		}
		return m, nil

	case "P":
		// Ask before mutating. Targets — the marked repos, or the cursor's —
		// are pinned now so a background refresh can't retarget the pull.
		if m.onPull == nil || m.busy {
			return m, nil
		}
		targets := m.pullTargets()
		if len(targets) == 0 {
			return m, nil
		}
		m.pending = pendingPull
		m.pendingPulls = targets
		return m, nil

	case "u":
		// Undo the last recorded pull, after confirmation.
		if m.onUndo != nil && !m.busy {
			m.pending = pendingUndo
		}
		return m, nil

	case "R":
		// Fetch just the cursor's repo from its remotes.
		if r, ok := m.current(); ok && m.onFetchRepo != nil && !m.fetching {
			if r.Missing {
				m.setError(errMissing(&r))
				return m, nil
			}
			onFetch := m.onFetchRepo
			name, path := r.Name, r.Path
			m.setStatus(name + ": fetching…")
			return m, func() tea.Msg {
				return fetchRepoDoneMsg{name: name, err: onFetch(path)}
			}
		}
		return m, nil

	case "y":
		// Copy the cursor repo's path to the clipboard.
		if r, ok := m.current(); ok && m.onCopy != nil {
			if err := m.onCopy(r.Path); err != nil {
				m.setError(err)
			} else {
				m.setStatus("copied " + r.Path)
			}
		}
		return m, nil

	case "o":
		return m.openCurrent(browser.PageHome)
	case "p":
		return m.openCurrent(browser.PagePRs)
	case "i":
		return m.openCurrent(browser.PageIssues)
	case "a":
		return m.openCurrent(browser.PageActions)

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
func (m *Model) openCurrent(page browser.Page) (tea.Model, tea.Cmd) {
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

// tryQuit quits unless a mutation is mid-flight, in which case the first
// press warns and the second quits anyway (ctrl+c always quits directly).
func (m *Model) tryQuit() (tea.Cmd, bool) {
	if m.busy && !m.quitArmed {
		m.quitArmed = true
		m.setStatus("pull in progress — press again to quit")
		return nil, false
	}
	m.quitting = true
	return tea.Quit, true
}

// pullTargets resolves what P acts on: the marked repos (in workspace order,
// missing ones skipped) or, with no marks, the repo under the cursor.
func (m *Model) pullTargets() []PullTarget {
	if len(m.marked) > 0 {
		targets := make([]PullTarget, 0, len(m.marked))
		for i := range m.all {
			r := &m.all[i]
			if m.marked[r.Path] && !r.Missing {
				targets = append(targets, PullTarget{Name: r.Name, Path: r.Path})
			}
		}
		return targets
	}
	r, ok := m.current()
	if !ok {
		return nil
	}
	if r.Missing {
		m.setError(errMissing(&r))
		return nil
	}
	return []PullTarget{{Name: r.Name, Path: r.Path}}
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
