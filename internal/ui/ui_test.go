package ui

import (
	"context"
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// TestMain strips color so View() output is deterministic for snapshot-style
// assertions, mirroring how the picker tests run with color disabled.
func TestMain(m *testing.M) {
	lipgloss.SetColorProfile(termenv.Ascii)
	os.Exit(m.Run())
}

// sampleRows returns three repos chosen so every sort/filter/group mode produces
// a distinct, checkable order.
func sampleRows() []Row {
	return []Row{
		{Name: "alpha", Path: "/a", Branch: "main", Tags: []string{"backend"},
			Changes: 0, Behind: 0, Health: 0, Severity: "ok", StatusText: "✓ clean"},
		{Name: "bravo", Path: "/b", Branch: "main", Tags: []string{"frontend"},
			Dirty: true, Changes: 2, Behind: 3, Health: 12, Severity: "warn", StatusText: "✎ 2M"},
		{Name: "charlie", Path: "/c", Branch: "dev", Tags: []string{"backend"},
			Changes: 0, Behind: 10, Conflicts: 1, Health: 80, Severity: "crit", StatusText: "✗ 1C"},
	}
}

// loadedModel returns a model already populated with sampleRows. The model uses
// pointer receivers, so handleKey/Update mutate it in place — tests read the
// fields directly after each keystroke.
func loadedModel(t *testing.T, onSelect func(string) error, onOpen func(string, string) error) *Model {
	t.Helper()
	m := New(Config{OnSelect: onSelect, OnOpen: onOpen})
	m.Update(rowsMsg{rows: sampleRows()})
	return &m
}

func viewNames(rows []Row) []string {
	out := make([]string, len(rows))
	for i := range rows {
		out[i] = rows[i].Name
	}
	return out
}

func eq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestRefreshPopulates(t *testing.T) {
	m := loadedModel(t, nil, nil)
	if !m.loaded {
		t.Fatal("model not marked loaded after rowsMsg")
	}
	if len(m.view) != 3 {
		t.Fatalf("view = %d rows, want 3", len(m.view))
	}
	if !eq(m.allTags, []string{"backend", "frontend"}) {
		t.Errorf("allTags = %v, want [backend frontend]", m.allTags)
	}
	if m.cursor != 0 {
		t.Errorf("cursor = %d, want 0", m.cursor)
	}
}

// TestSortCycle checks `s` rotates name → dirty → behind → health → name.
func TestSortCycle(t *testing.T) {
	m := loadedModel(t, nil, nil)

	if got := viewNames(m.view); !eq(got, []string{"alpha", "bravo", "charlie"}) {
		t.Fatalf("name order = %v", got)
	}

	want := []struct {
		mode  string
		names []string
	}{
		{"dirty", []string{"bravo", "alpha", "charlie"}},
		{"behind", []string{"charlie", "bravo", "alpha"}},
		{"health", []string{"charlie", "bravo", "alpha"}},
		{"name", []string{"alpha", "bravo", "charlie"}},
	}
	for _, tc := range want {
		m.handleKey("s")
		if m.sort.String() != tc.mode {
			t.Fatalf("sort = %q, want %q", m.sort.String(), tc.mode)
		}
		if got := viewNames(m.view); !eq(got, tc.names) {
			t.Errorf("%s order = %v, want %v", tc.mode, got, tc.names)
		}
	}
}

// TestFilterCycle checks `f` rotates all → dirty → behind → ahead → conflicts.
func TestFilterCycle(t *testing.T) {
	m := loadedModel(t, nil, nil)

	steps := []struct {
		mode  string
		names []string
	}{
		{"dirty", []string{"bravo"}},
		{"behind", []string{"bravo", "charlie"}},
		{"ahead", []string{}},
		{"conflicts", []string{"charlie"}},
		{"all", []string{"alpha", "bravo", "charlie"}},
	}
	for _, s := range steps {
		m.handleKey("f")
		if m.filter.String() != s.mode {
			t.Fatalf("filter = %q, want %q", m.filter.String(), s.mode)
		}
		if got := viewNames(m.view); !eq(got, s.names) {
			t.Errorf("filter %q view = %v, want %v", s.mode, got, s.names)
		}
	}
}

func TestTagCycle(t *testing.T) {
	m := loadedModel(t, nil, nil)

	steps := []struct {
		tag   string
		names []string
	}{
		{"backend", []string{"alpha", "charlie"}},
		{"frontend", []string{"bravo"}},
		{"", []string{"alpha", "bravo", "charlie"}},
	}
	for _, s := range steps {
		m.handleKey("t")
		if m.tagFilter != s.tag {
			t.Fatalf("tagFilter = %q, want %q", m.tagFilter, s.tag)
		}
		if got := viewNames(m.view); !eq(got, s.names) {
			t.Errorf("tag %q view = %v, want %v", s.tag, got, s.names)
		}
	}
}

// TestGroupByTag clusters rows under their first tag, untagged last, preserving
// the active sort within each group.
func TestGroupByTag(t *testing.T) {
	m := loadedModel(t, nil, nil)
	m.handleKey("G")
	if !m.grouped {
		t.Fatal("G did not enable grouping")
	}
	// backend (alpha, charlie) then frontend (bravo).
	if got := viewNames(m.view); !eq(got, []string{"alpha", "charlie", "bravo"}) {
		t.Errorf("grouped order = %v, want [alpha charlie bravo]", got)
	}

	m.width = 100
	out := m.View()
	if !strings.Contains(out, "backend (2)") || !strings.Contains(out, "frontend (1)") {
		t.Errorf("grouped View missing tag headers\n%s", out)
	}

	m.handleKey("G")
	if m.grouped {
		t.Error("second G did not disable grouping")
	}
}

// TestSearch narrows the view by typed substring and restores it on esc.
func TestSearch(t *testing.T) {
	m := loadedModel(t, nil, nil)

	m.handleKey("/")
	if !m.searching {
		t.Fatal("/ did not enter search mode")
	}
	m.handleKey("c")
	m.handleKey("h")
	if m.query != "ch" {
		t.Fatalf("query = %q, want ch", m.query)
	}
	if got := viewNames(m.view); !eq(got, []string{"charlie"}) {
		t.Errorf("search view = %v, want [charlie]", got)
	}

	m.handleKey("backspace")
	if m.query != "c" {
		t.Errorf("query after backspace = %q, want c", m.query)
	}

	m.handleKey("esc")
	if m.searching || m.query != "" {
		t.Errorf("esc did not exit search: searching=%v query=%q", m.searching, m.query)
	}
	if len(m.view) != 3 {
		t.Errorf("view after esc = %d rows, want 3", len(m.view))
	}
}

func TestNavigationClamp(t *testing.T) {
	m := loadedModel(t, nil, nil)

	m.handleKey("k")
	if m.cursor != 0 {
		t.Errorf("k at top moved cursor to %d", m.cursor)
	}
	for range 5 {
		m.handleKey("j")
	}
	if m.cursor != 2 {
		t.Errorf("cursor = %d after walking down, want 2 (clamped)", m.cursor)
	}
	m.handleKey("k")
	if m.cursor != 1 {
		t.Errorf("cursor = %d after k, want 1", m.cursor)
	}
}

func TestEnterSelects(t *testing.T) {
	var gotPath string
	onSelect := func(p string) error { gotPath = p; return nil }

	m := loadedModel(t, onSelect, nil)
	m.handleKey("j") // move to bravo
	_, cmd := m.handleKey("enter")

	if gotPath != "/b" {
		t.Errorf("onSelect path = %q, want /b", gotPath)
	}
	if m.selected != "/b" {
		t.Errorf("selected = %q, want /b", m.selected)
	}
	if !isQuit(cmd) {
		t.Error("enter did not return tea.Quit")
	}
}

func TestEnterSelectError(t *testing.T) {
	onSelect := func(string) error { return context.Canceled }
	m := loadedModel(t, onSelect, nil)

	_, cmd := m.handleKey("enter")

	if m.selected != "" {
		t.Errorf("selected = %q, want empty on error", m.selected)
	}
	if m.lastErr == nil {
		t.Error("lastErr not set on select failure")
	}
	if isQuit(cmd) {
		t.Error("enter quit despite select error")
	}
}

// TestOpenPages maps o/p/i/a to the right browser page for the cursor's repo.
func TestOpenPages(t *testing.T) {
	var gotPath, gotPage string
	onOpen := func(p, page string) error { gotPath, gotPage = p, page; return nil }

	cases := map[string]string{"o": "home", "p": "prs", "i": "issues", "a": "actions"}
	for key, wantPage := range cases {
		m := loadedModel(t, nil, onOpen)
		m.handleKey("j") // bravo
		m.handleKey(key)
		if gotPath != "/b" || gotPage != wantPage {
			t.Errorf("%q opened (%q,%q), want (/b,%q)", key, gotPath, gotPage, wantPage)
		}
	}
}

func TestQuitKeys(t *testing.T) {
	for _, key := range []string{"q", "esc", "ctrl+c"} {
		m := loadedModel(t, nil, nil)
		_, cmd := m.handleKey(key)
		if !m.quitting {
			t.Errorf("%q did not set quitting", key)
		}
		if !isQuit(cmd) {
			t.Errorf("%q did not return tea.Quit", key)
		}
	}
}

func TestFetchKeyMarksFetching(t *testing.T) {
	m := loadedModel(t, nil, nil)
	m.collect = func(context.Context, bool) []Row { return sampleRows() }

	_, cmd := m.handleKey("g")
	if !m.fetching {
		t.Error("g did not set fetching")
	}
	if cmd == nil {
		t.Error("g did not issue a refresh command")
	}
}

// TestHelpOverlay toggles the full help panel and confirms a stray key closes
// it while quit keys still quit.
func TestHelpOverlay(t *testing.T) {
	m := loadedModel(t, nil, nil)
	m.width = 100

	m.handleKey("?")
	if !m.showHelp {
		t.Fatal("? did not open help")
	}
	if out := m.View(); !strings.Contains(out, "soko ui — keys") {
		t.Errorf("help View missing title\n%s", out)
	}

	m.handleKey("j") // any non-quit key closes
	if m.showHelp {
		t.Error("stray key did not close help")
	}

	m.handleKey("?")
	_, cmd := m.handleKey("q")
	if !isQuit(cmd) {
		t.Error("q in help overlay did not quit")
	}
}

// TestViewRendersRows is a light snapshot: with color stripped, the dashboard
// contains the title, every repo, the tag legend, and footer totals.
func TestViewRendersRows(t *testing.T) {
	m := loadedModel(t, nil, nil)
	m.width, m.height = 100, 30

	out := m.View()

	for _, want := range []string{
		"soko ui", "alpha", "bravo", "charlie",
		"tags:", "backend(2)", "frontend(1)",
		"3 repos", "1 dirty", "2 behind",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("View() missing %q\n---\n%s", want, out)
		}
	}
	if !strings.Contains(out, "› ") {
		t.Errorf("View() missing cursor marker\n---\n%s", out)
	}
}

// TestViewEmptyFilter renders the explanatory empty-state when a filter excludes
// every repo.
func TestViewEmptyFilter(t *testing.T) {
	m := loadedModel(t, nil, nil)
	m.width = 100
	m.filter = filterAhead // no repo is ahead
	m.rebuild()

	if got := m.View(); !strings.Contains(got, "no repos match filter: ahead") {
		t.Errorf("empty View() = %q", got)
	}
}

func isQuit(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}
