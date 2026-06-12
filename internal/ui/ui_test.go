package ui

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

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
	m.handleKey("b")
	if !m.grouped {
		t.Fatal("b did not enable grouping")
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

	m.handleKey("b")
	if m.grouped {
		t.Error("second b did not disable grouping")
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

	_, cmd := m.handleKey("r")
	if !m.fetching {
		t.Error("r did not set fetching")
	}
	if cmd == nil {
		t.Error("r did not issue a refresh command")
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

// TestPullConfirmAndRun covers the full mutate path: P asks, the banner shows,
// y runs the injected pull, and the done message lands a status line.
func TestPullConfirmAndRun(t *testing.T) {
	var gotName, gotPath string
	onPull := func(name, path string) (string, error) {
		gotName, gotPath = name, path
		return "pulled", nil
	}

	m := New(Config{OnPull: onPull})
	m.Update(rowsMsg{rows: sampleRows()})
	m.width = 100

	m.handleKey("j") // select bravo
	m.handleKey("P")
	if m.pending != pendingPull {
		t.Fatal("P did not arm a pending pull")
	}
	if out := m.View(); !strings.Contains(out, "pull bravo? [y/N]") {
		t.Errorf("View missing confirm banner\n%s", out)
	}

	_, cmd := m.handleConfirmKey("y")
	if m.pending != pendingNone || !m.busy {
		t.Fatalf("y did not start the pull: pending=%v busy=%v", m.pending, m.busy)
	}
	if cmd == nil {
		t.Fatal("confirmed pull returned no command")
	}

	// The command runs the injected callback and yields a pullDoneMsg.
	msg := cmd()
	done, ok := msg.(pullDoneMsg)
	if !ok {
		t.Fatalf("pull command returned %T, want pullDoneMsg", msg)
	}
	if gotName != "bravo" || gotPath != "/b" {
		t.Errorf("onPull called with (%q,%q), want (bravo,/b)", gotName, gotPath)
	}

	m.Update(done)
	if m.busy {
		t.Error("busy not cleared after pullDoneMsg")
	}
	if m.statusMsg != "bravo: pulled" {
		t.Errorf("statusMsg = %q, want 'bravo: pulled'", m.statusMsg)
	}
}

// TestPullCancel confirms n/esc dismiss the prompt without pulling.
func TestPullCancel(t *testing.T) {
	called := false
	onPull := func(string, string) (string, error) { called = true; return "", nil }

	for _, key := range []string{"n", "esc"} {
		m := New(Config{OnPull: onPull})
		m.Update(rowsMsg{rows: sampleRows()})
		m.handleKey("P")
		m.handleConfirmKey(key)
		if m.pending != pendingNone {
			t.Errorf("%q did not cancel pending", key)
		}
	}
	if called {
		t.Error("onPull ran despite cancel")
	}
}

// TestPullError surfaces a failed pull as an error, not a status line.
func TestPullError(t *testing.T) {
	onPull := func(string, string) (string, error) { return "", context.DeadlineExceeded }
	m := New(Config{OnPull: onPull})
	m.Update(rowsMsg{rows: sampleRows()})

	m.handleKey("P")
	_, cmd := m.handleConfirmKey("y")
	m.Update(cmd())

	if m.lastErr == nil {
		t.Error("failed pull did not set lastErr")
	}
	if m.statusMsg != "" {
		t.Errorf("statusMsg = %q, want empty on error", m.statusMsg)
	}
}

// TestPullNoCallback makes P a no-op when no pull callback is wired.
func TestPullNoCallback(t *testing.T) {
	m := loadedModel(t, nil, nil) // OnPull is nil
	m.handleKey("P")
	if m.pending != pendingNone {
		t.Error("P armed a pull with no callback wired")
	}
}

// TestCursorFollowsRepoAcrossRefresh keeps the cursor on the same repo when a
// background refresh changes the sort order.
func TestCursorFollowsRepoAcrossRefresh(t *testing.T) {
	m := loadedModel(t, nil, nil) // cursor on alpha (name sort)

	// Changing the sort keeps the cursor on alpha at its new position.
	m.handleKey("s") // dirty sort → bravo, alpha, charlie
	if r, _ := m.current(); r.Name != "alpha" || m.cursor != 1 {
		t.Fatalf("cursor on %q at %d after sort, want alpha at 1", r.Name, m.cursor)
	}

	// Refresh: charlie becomes the dirtiest → order charlie, bravo, alpha.
	rows := sampleRows()
	rows[2].Dirty, rows[2].Changes = true, 9
	m.Update(rowsMsg{rows: rows})

	if r, _ := m.current(); r.Name != "alpha" {
		t.Errorf("cursor jumped to %q after refresh, want alpha", r.Name)
	}
	if m.cursor != 2 {
		t.Errorf("cursor index = %d, want 2 (alpha's new position)", m.cursor)
	}
}

// TestCursorFallsBackWhenRepoLeaves clamps the cursor when the repo under it
// disappears from the view.
func TestCursorFallsBackWhenRepoLeaves(t *testing.T) {
	m := loadedModel(t, nil, nil)
	m.handleKey("G")                          // charlie
	m.Update(rowsMsg{rows: sampleRows()[:2]}) // charlie removed

	if r, ok := m.current(); !ok || r.Name != "bravo" {
		t.Errorf("cursor = %v, want clamp to bravo", r.Name)
	}
}

// TestPendingPullPinned proves the armed pull targets the repo shown in the
// banner even if a refresh re-sorts the view before confirmation.
func TestPendingPullPinned(t *testing.T) {
	var gotName string
	onPull := func(name, _ string) (string, error) { gotName = name; return "pulled", nil }

	m := New(Config{OnPull: onPull})
	m.Update(rowsMsg{rows: sampleRows()})
	m.width = 100

	m.handleKey("j") // bravo
	m.handleKey("P")

	// Refresh re-sorts so a different repo sits under the cursor index.
	rows := sampleRows()
	rows[0].Name, rows[0].Path = "aaa-new", "/new" // displaces bravo's position
	m.Update(rowsMsg{rows: rows})

	if out := m.View(); !strings.Contains(out, "pull bravo? [y/N]") {
		t.Errorf("banner lost pinned target\n%s", out)
	}

	_, cmd := m.handleConfirmKey("y")
	if cmd == nil {
		t.Fatal("confirmed pull returned no command")
	}
	cmd()
	if gotName != "bravo" {
		t.Errorf("pull ran against %q, want bravo", gotName)
	}
}

// TestFetchIndicatorSurvivesCheapRefresh keeps the fetching flag up until the
// fetched frame arrives, even when a cheap tick refresh lands first.
func TestFetchIndicatorSurvivesCheapRefresh(t *testing.T) {
	m := loadedModel(t, nil, nil)
	m.collect = func(context.Context, bool) []Row { return sampleRows() }

	m.handleKey("r")
	if !m.fetching {
		t.Fatal("r did not set fetching")
	}

	m.Update(rowsMsg{rows: sampleRows(), fetched: false}) // cheap refresh wins the race
	if !m.fetching {
		t.Error("cheap refresh cleared the fetching indicator")
	}

	m.Update(rowsMsg{rows: sampleRows(), fetched: true})
	if m.fetching {
		t.Error("fetched frame did not clear the indicator")
	}
	if m.lastFetch.IsZero() {
		t.Error("fetched frame did not stamp lastFetch")
	}
	if out := m.View(); !strings.Contains(out, "fetched ") {
		t.Errorf("status line missing fetch staleness\n%s", out)
	}
}

// TestTickSkipsCollectWhileFetching re-arms only the timer during a fetch so
// slow fetches don't pile up concurrent collectors.
func TestTickSkipsCollectWhileFetching(t *testing.T) {
	m := New(Config{RefreshEvery: time.Millisecond})
	m.Update(rowsMsg{rows: sampleRows()})

	m.fetching = true
	_, cmd := m.Update(tickMsg(time.Now()))
	if msg := cmd(); msg != nil {
		if _, isTick := msg.(tickMsg); !isTick {
			t.Errorf("tick while fetching returned %T, want tickMsg only", msg)
		}
	}
}

// TestStatusExpires clears transient status and error lines after statusTTL.
func TestStatusExpires(t *testing.T) {
	m := loadedModel(t, nil, nil)
	m.collect = func(context.Context, bool) []Row { return sampleRows() }

	m.setStatus("bravo: pulled")
	m.msgSetAt = time.Now().Add(-statusTTL - time.Second)
	m.Update(tickMsg(time.Now()))
	if m.statusMsg != "" {
		t.Errorf("statusMsg = %q, want expired", m.statusMsg)
	}

	m.setError(context.DeadlineExceeded)
	m.msgSetAt = time.Now().Add(-statusTTL - time.Second)
	m.Update(tickMsg(time.Now()))
	if m.lastErr != nil {
		t.Errorf("lastErr = %v, want expired", m.lastErr)
	}
}

// TestSuccessReplacesError lets a later success clear a stale error so status
// lines never get stuck behind one failed action.
func TestSuccessReplacesError(t *testing.T) {
	onPull := func(string, string) (string, error) { return "pulled", nil }
	m := New(Config{OnPull: onPull})
	m.Update(rowsMsg{rows: sampleRows()})

	m.setError(context.DeadlineExceeded)

	m.handleKey("P")
	_, cmd := m.handleConfirmKey("y")
	m.Update(cmd())

	if m.lastErr != nil {
		t.Errorf("lastErr = %v, want cleared by pull success", m.lastErr)
	}
	if m.statusMsg == "" {
		t.Error("statusMsg empty after successful pull")
	}
}

// TestMissingRepoGuards blocks enter/P/o on a missing repo with an explanatory
// error instead of acting on a nonexistent path.
func TestMissingRepoGuards(t *testing.T) {
	selected, opened, pulled := false, false, false
	m := New(Config{
		OnSelect: func(string) error { selected = true; return nil },
		OnOpen:   func(string, string) error { opened = true; return nil },
		OnPull:   func(string, string) (string, error) { pulled = true; return "", nil },
	})
	rows := sampleRows()
	rows[0].Missing = true
	m.Update(rowsMsg{rows: rows}) // cursor on alpha (missing)

	if _, cmd := m.handleKey("enter"); isQuit(cmd) || selected {
		t.Error("enter acted on a missing repo")
	}
	if m.lastErr == nil {
		t.Error("enter on missing repo set no error")
	}

	m.handleKey("o")
	if opened {
		t.Error("o opened a browser for a missing repo")
	}

	m.handleKey("P")
	if m.pending != pendingNone || pulled {
		t.Error("P armed a pull for a missing repo")
	}

	m.width = 100
	if out := m.View(); !strings.Contains(out, "missing") {
		t.Errorf("missing badge not rendered\n%s", out)
	}
}

// TestWorktreeMarker marks linked worktrees in the REPO column and names the
// parent in the detail line.
func TestWorktreeMarker(t *testing.T) {
	m := loadedModel(t, nil, nil)
	rows := sampleRows()
	rows[1].WorktreeOf = "alpha"
	m.Update(rowsMsg{rows: rows})
	m.width = 120

	out := m.View()
	if !strings.Contains(out, "↳ bravo") {
		t.Errorf("worktree marker missing\n%s", out)
	}

	m.handleKey("j") // bravo
	if out := m.View(); !strings.Contains(out, "worktree of alpha") {
		t.Errorf("detail line missing worktree parent\n%s", out)
	}
}

// TestDetailLine shows the cursor repo's path and health reasons below the
// table.
func TestDetailLine(t *testing.T) {
	m := loadedModel(t, nil, nil)
	rows := sampleRows()
	rows[2].Reasons = []string{"1 conflict", "10 behind"}
	m.Update(rowsMsg{rows: rows})
	m.width = 120

	m.handleKey("G") // charlie
	out := m.View()
	if !strings.Contains(out, "› charlie — /c") {
		t.Errorf("detail line missing path\n%s", out)
	}
	if !strings.Contains(out, "1 conflict, 10 behind") {
		t.Errorf("detail line missing reasons\n%s", out)
	}
}

// TestTruncateRuneSafe never splits a multibyte rune.
func TestTruncateRuneSafe(t *testing.T) {
	got := truncate("brånçh-nämé-lông", 8)
	if !strings.HasSuffix(got, "…") {
		t.Errorf("truncate = %q, want ellipsis suffix", got)
	}
	if lipgloss.Width(got) > 8 {
		t.Errorf("truncate width = %d, want <= 8", lipgloss.Width(got))
	}
	for _, r := range got {
		if r == '�' {
			t.Errorf("truncate produced replacement char: %q", got)
		}
	}
}

// TestGroupedLegendMatchesHeaders counts the legend by first tag in grouped
// mode so legend and group header numbers agree for multi-tag repos.
func TestGroupedLegendMatchesHeaders(t *testing.T) {
	m := loadedModel(t, nil, nil)
	rows := sampleRows()
	rows[0].Tags = []string{"backend", "frontend"} // alpha counts once, under backend
	m.Update(rowsMsg{rows: rows})
	m.width = 120

	if out := m.View(); !strings.Contains(out, "frontend(2)") {
		t.Errorf("ungrouped legend should count all tags\n%s", out)
	}

	m.handleKey("b")
	if out := m.View(); !strings.Contains(out, "frontend(1)") {
		t.Errorf("grouped legend should count first tags only\n%s", out)
	}
}

// TestSearchCommit keeps the query active after enter and lets normal-mode
// actions operate on the filtered list.
func TestSearchCommit(t *testing.T) {
	var gotPath string
	m := loadedModel(t, func(p string) error { gotPath = p; return nil }, nil)

	m.handleKey("/")
	m.handleKey("c")
	m.handleKey("h")
	m.handleKey("enter") // commit, not select

	if m.searching {
		t.Error("enter did not leave search mode")
	}
	if m.query != "ch" {
		t.Errorf("query = %q, want kept as ch", m.query)
	}
	if got := viewNames(m.view); !eq(got, []string{"charlie"}) {
		t.Errorf("committed view = %v, want [charlie]", got)
	}

	// Normal-mode enter now selects the match.
	_, cmd := m.handleKey("enter")
	if gotPath != "/c" || !isQuit(cmd) {
		t.Errorf("select after commit = (%q, quit=%v), want (/c, true)", gotPath, isQuit(cmd))
	}
}

// TestEscUnwind clears the committed search first, then filters, then quits.
func TestEscUnwind(t *testing.T) {
	m := loadedModel(t, nil, nil)

	m.handleKey("/")
	m.handleKey("c")
	m.handleKey("enter") // commit search
	m.handleKey("f")     // filter: dirty
	m.handleKey("t")     // tag: backend

	m.handleKey("esc")
	if m.query != "" {
		t.Errorf("first esc kept query %q", m.query)
	}
	if m.filter == filterAll || m.tagFilter == "" {
		t.Error("first esc should only clear the search")
	}

	m.handleKey("esc")
	if m.filter != filterAll || m.tagFilter != "" {
		t.Errorf("second esc kept filter=%v tag=%q", m.filter, m.tagFilter)
	}
	if m.quitting {
		t.Error("second esc quit too early")
	}

	_, cmd := m.handleKey("esc")
	if !m.quitting || !isQuit(cmd) {
		t.Error("third esc did not quit")
	}
}

// TestReverseCycles checks S/F/T step the cycles backwards (with wrap).
func TestReverseCycles(t *testing.T) {
	m := loadedModel(t, nil, nil)

	m.handleKey("S")
	if m.sort.String() != "health" {
		t.Errorf("S from name = %q, want health (wrap)", m.sort.String())
	}
	m.handleKey("F")
	if m.filter.String() != "conflicts" {
		t.Errorf("F from all = %q, want conflicts (wrap)", m.filter.String())
	}
	m.handleKey("T")
	if m.tagFilter != "frontend" {
		t.Errorf("T from all = %q, want frontend (wrap)", m.tagFilter)
	}
	m.handleKey("t")
	if m.tagFilter != "" {
		t.Errorf("t after T = %q, want all", m.tagFilter)
	}
}

// TestSearchMatchesBranchAndTags widens search beyond the repo name.
func TestSearchMatchesBranchAndTags(t *testing.T) {
	m := loadedModel(t, nil, nil)

	m.handleKey("/")
	m.handleKey("d")
	m.handleKey("e")
	m.handleKey("v")
	if got := viewNames(m.view); !eq(got, []string{"charlie"}) {
		t.Errorf("branch search view = %v, want [charlie]", got)
	}

	m.handleKey("esc")
	m.handleKey("/")
	for _, k := range []string{"f", "r", "o", "n", "t"} {
		m.handleKey(k)
	}
	if got := viewNames(m.view); !eq(got, []string{"bravo"}) {
		t.Errorf("tag search view = %v, want [bravo]", got)
	}
}

// manyRows builds n distinct repos for viewport tests.
func manyRows(n int) []Row {
	rows := make([]Row, n)
	for i := range rows {
		rows[i] = Row{
			Name: fmt.Sprintf("repo-%02d", i), Path: fmt.Sprintf("/r/%02d", i),
			Branch: "main", Severity: "ok", StatusText: "✓ clean",
		}
	}
	return rows
}

// TestJumpAndPagingKeys covers g/G/home/end and the paging keys.
func TestJumpAndPagingKeys(t *testing.T) {
	m := New(Config{})
	m.Update(rowsMsg{rows: manyRows(40)})
	m.height = 20 // pageSize derives from the viewport

	m.handleKey("G")
	if m.cursor != 39 {
		t.Errorf("G cursor = %d, want 39", m.cursor)
	}
	m.handleKey("g")
	if m.cursor != 0 {
		t.Errorf("g cursor = %d, want 0", m.cursor)
	}
	m.handleKey("end")
	if m.cursor != 39 {
		t.Errorf("end cursor = %d, want 39", m.cursor)
	}
	m.handleKey("home")
	if m.cursor != 0 {
		t.Errorf("home cursor = %d, want 0", m.cursor)
	}

	page := m.pageSize()
	m.handleKey("pgdown")
	if m.cursor != page {
		t.Errorf("pgdown cursor = %d, want %d", m.cursor, page)
	}
	m.handleKey("ctrl+u")
	if m.cursor != page-page/2 {
		t.Errorf("ctrl+u cursor = %d, want %d", m.cursor, page-page/2)
	}
}

// TestViewportClampsToHeight keeps the frame within the terminal height and the
// cursor row visible while walking a long list.
func TestViewportClampsToHeight(t *testing.T) {
	m := New(Config{})
	m.Update(rowsMsg{rows: manyRows(40)})
	m.width, m.height = 100, 16

	for range 30 {
		m.handleKey("j")
	}
	out := m.View()
	lines := strings.Count(out, "\n") + 1
	if lines > m.height {
		t.Errorf("View() is %d lines, want <= %d\n%s", lines, m.height, out)
	}
	if !strings.Contains(out, "› repo-30") {
		t.Errorf("cursor row repo-30 not visible\n%s", out)
	}
	if !strings.Contains(out, "lines ") {
		t.Errorf("footer missing scroll position\n%s", out)
	}

	// Jumping back to the top scrolls the window up.
	m.handleKey("g")
	if out := m.View(); !strings.Contains(out, "repo-00") {
		t.Errorf("top row not visible after g\n%s", out)
	}
}

// TestViewportUnclampedWithoutHeight renders everything when no WindowSizeMsg
// has arrived (height 0), preserving the old behavior for tests and dumps.
func TestViewportUnclampedWithoutHeight(t *testing.T) {
	m := New(Config{})
	m.Update(rowsMsg{rows: manyRows(40)})

	out := m.View()
	if !strings.Contains(out, "repo-00") || !strings.Contains(out, "repo-39") {
		t.Errorf("unclamped view missing rows\n%s", out)
	}
}

// TestClampWidth truncates over-wide lines ANSI-aware instead of letting the
// terminal wrap them.
func TestClampWidth(t *testing.T) {
	m := New(Config{})
	m.Update(rowsMsg{rows: manyRows(5)})
	m.width = 30

	for line := range strings.SplitSeq(m.View(), "\n") {
		if w := lipgloss.Width(line); w > 30 {
			t.Errorf("line width %d > 30: %q", w, line)
		}
	}
}

// TestMouse maps wheel ticks to cursor movement and a click to row selection.
func TestMouse(t *testing.T) {
	m := New(Config{})
	m.Update(rowsMsg{rows: manyRows(10)})
	m.height = 30

	m.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress})
	if m.cursor != wheelStep {
		t.Errorf("wheel down cursor = %d, want %d", m.cursor, wheelStep)
	}
	m.Update(tea.MouseMsg{Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress})
	if m.cursor != 0 {
		t.Errorf("wheel up cursor = %d, want 0", m.cursor)
	}

	// Click the third row: top chrome is 4 lines (title, blank, two header
	// lines), so row index 2 renders at screen line 6.
	m.Update(tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, Y: 6})
	if m.cursor != 2 {
		t.Errorf("click cursor = %d, want 2", m.cursor)
	}

	// A click outside the table changes nothing.
	m.Update(tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, Y: 25})
	if m.cursor != 2 {
		t.Errorf("out-of-table click moved cursor to %d", m.cursor)
	}
}

func isQuit(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}
