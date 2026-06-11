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

// sampleRows returns three repos chosen so every sort/filter mode produces a
// distinct, checkable order.
func sampleRows() []Row {
	return []Row{
		{Name: "alpha", Path: "/a", Branch: "main", Tags: []string{"backend"},
			Changes: 0, Behind: 0, Health: 0, Severity: "ok", StatusText: "✓ clean"},
		{Name: "bravo", Path: "/b", Branch: "main", Tags: []string{"frontend"},
			Dirty: true, Changes: 2, Behind: 3, Health: 12, Severity: "warn", StatusText: "✎ 2M"},
		{Name: "charlie", Path: "/c", Branch: "dev", Tags: []string{"backend"},
			Changes: 0, Behind: 10, Health: 80, Severity: "crit", StatusText: "✗ 1C"},
	}
}

// loadedModel returns a model already populated with sampleRows. The model uses
// pointer receivers, so handleKey/Update mutate it in place — tests read the
// fields directly after each keystroke.
func loadedModel(t *testing.T, onSelect, onOpen func(string) error) *Model {
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

// TestRefreshPopulates verifies the collector → rowsMsg path fills the model and
// derives the tag set, leaving the cursor at the top.
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

// TestSortCycle checks `s` rotates name → dirty → behind → health → name and
// that each mode reorders the view as expected.
func TestSortCycle(t *testing.T) {
	m := loadedModel(t, nil, nil)

	// Default is name order.
	if got := viewNames(m.view); !eq(got, []string{"alpha", "bravo", "charlie"}) {
		t.Fatalf("name order = %v", got)
	}

	want := []struct {
		mode  string
		names []string
	}{
		{"dirty", []string{"bravo", "alpha", "charlie"}}, // by changes desc, tie by name
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

// TestFilterDirty toggles the dirty filter on and off.
func TestFilterDirty(t *testing.T) {
	m := loadedModel(t, nil, nil)

	m.handleKey("f")
	if got := viewNames(m.view); !eq(got, []string{"bravo"}) {
		t.Errorf("dirty filter = %v, want [bravo]", got)
	}

	m.handleKey("f")
	if len(m.view) != 3 {
		t.Errorf("after toggle off = %d rows, want 3", len(m.view))
	}
}

// TestTagCycle walks the tag filter through "" → backend → frontend → "".
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

// TestNavigationClamp checks j/k movement and that the cursor never escapes the
// view bounds.
func TestNavigationClamp(t *testing.T) {
	m := loadedModel(t, nil, nil)

	// Up at the top is a no-op.
	m.handleKey("k")
	if m.cursor != 0 {
		t.Errorf("k at top moved cursor to %d", m.cursor)
	}

	// Walk to the bottom and past it.
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

// TestEnterSelects confirms enter invokes onSelect with the cursor's path,
// records the selection, and asks the program to quit.
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

// TestEnterSelectError leaves the dashboard open when the nav write fails.
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

// TestOpenInvokesCallback checks `o` opens the cursor's repo.
func TestOpenInvokesCallback(t *testing.T) {
	var opened string
	onOpen := func(p string) error { opened = p; return nil }

	m := loadedModel(t, nil, onOpen)
	m.handleKey("j")
	m.handleKey("j")
	m.handleKey("o")
	if opened != "/c" {
		t.Errorf("onOpen path = %q, want /c", opened)
	}
}

// TestQuitKeys confirms q, esc, and ctrl+c all request quit.
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

// TestFetchKeyMarksFetching checks `g` flips the fetching flag and issues a
// refresh command.
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

// TestViewRendersRows is a light snapshot: with color stripped, the rendered
// dashboard contains the title, every repo, the cursor marker, and the footer
// totals. Last-commit times are zero so the AGE column is stable ("-").
func TestViewRendersRows(t *testing.T) {
	m := loadedModel(t, nil, nil)
	m.width, m.height = 100, 30

	out := m.View()

	for _, want := range []string{"soko ui", "alpha", "bravo", "charlie", "3 repos", "1 dirty", "2 behind"} {
		if !strings.Contains(out, want) {
			t.Errorf("View() missing %q\n---\n%s", want, out)
		}
	}
	// Cursor marker sits on the first row.
	if !strings.Contains(out, "› ") {
		t.Errorf("View() missing cursor marker\n---\n%s", out)
	}
}

// TestViewEmptyFilter renders the empty-state when a filter excludes every repo.
func TestViewEmptyFilter(t *testing.T) {
	m := loadedModel(t, nil, nil)
	m.filterDirty = true
	m.all = []Row{{Name: "x", Severity: "ok"}} // clean only
	m.rebuild()

	if got := m.View(); !strings.Contains(got, "no repos match") {
		t.Errorf("empty View() = %q", got)
	}
}

// isQuit reports whether cmd is bubbletea's Quit command.
func isQuit(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}
