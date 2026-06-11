// Package ui implements `soko ui` — a live, full-screen dashboard of local
// workspace state (dirt, ahead/behind, branch, last-commit age, health). It is
// read-only: navigation (enter) and open-in-browser (o) are the only actions,
// both delegated to injected callbacks so this package owns no git or shell I/O.
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
	"time"

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
	LastCommit time.Time
	StatusText string // pre-formatted dirty marker (output.FormatStatus)
	Health     int    // urgency score, higher = more neglected
	Severity   string // "ok" | "warn" | "crit"
	Missing    bool
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
	OnSelect     func(path string) error // enter — write the shell nav file
	OnOpen       func(path string) error // o — open the repo in a browser
	RefreshEvery time.Duration           // local state refresh cadence
	FetchEvery   time.Duration           // background fetch cadence; 0 disables
}

// sortMode is the row ordering cycled with `s`.
type sortMode int

const (
	sortName sortMode = iota
	sortDirty
	sortBehind
	sortHealth
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

// defaultRefresh is used when Config.RefreshEvery is unset. The feature contract
// is "local state every 5s (cheap, no network)".
const defaultRefresh = 5 * time.Second

// defaultWidth backs rendering before the first WindowSizeMsg arrives (and in
// tests, which never send one).
const defaultWidth = 80

// Model is the bubbletea model for the dashboard.
type Model struct {
	ctx      context.Context
	collect  Collector
	onSelect func(path string) error
	onOpen   func(path string) error

	all  []Row // last collected, in config order
	view []Row // all after filter + sort

	cursor      int
	sort        sortMode
	filterDirty bool
	tagFilter   string // "" = all tags
	allTags     []string

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

	case tea.KeyMsg:
		return m.handleKey(msg.String())
	}
	return m, nil
}

// handleKey applies a single keystroke and returns the next model plus any
// command. Split out from Update so tests can drive it by key name.
func (m *Model) handleKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "q", "ctrl+c", "esc":
		m.quitting = true
		return m, tea.Quit

	case "j", "down":
		if m.cursor < len(m.view)-1 {
			m.cursor++
		}
		return m, nil

	case "k", "up":
		if m.cursor > 0 {
			m.cursor--
		}
		return m, nil

	case "s":
		m.sort = (m.sort + 1) % 4
		m.rebuild()
		return m, nil

	case "f":
		m.filterDirty = !m.filterDirty
		m.rebuild()
		return m, nil

	case "t":
		m.cycleTag()
		m.rebuild()
		return m, nil

	case "g":
		// Re-fetch now.
		m.fetching = true
		cmd := m.refreshCmd(true)
		return m, cmd

	case "o":
		if r, ok := m.current(); ok && m.onOpen != nil {
			if err := m.onOpen(r.Path); err != nil {
				m.lastErr = err
			}
		}
		return m, nil

	case "enter":
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
	return m, nil
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

// rebuild recomputes the visible rows from m.all by applying the active filters
// and sort, then clamps the cursor.
func (m *Model) rebuild() {
	rows := make([]Row, 0, len(m.all))
	for i := range m.all {
		r := &m.all[i]
		if m.filterDirty && !r.Dirty {
			continue
		}
		if m.tagFilter != "" && !slices.Contains(r.Tags, m.tagFilter) {
			continue
		}
		rows = append(rows, *r)
	}
	sortRows(rows, m.sort)
	m.view = rows

	if m.cursor >= len(m.view) {
		m.cursor = len(m.view) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
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
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithContext(m.ctx))
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
