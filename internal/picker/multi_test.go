package picker

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
)

// multiState builds a multi-select state with n items, all checked, cursor at 0.
func multiState(labels ...string) *state {
	items := make([]Item, len(labels))
	for i, l := range labels {
		items[i] = Item{Label: l, Desc: "/p/" + l, index: i}
	}
	s := &state{
		allItems:   items,
		filtered:   items,
		multi:      true,
		checked:    make(map[int]bool, len(items)),
		labelWidth: computeLabelWidth(items),
	}
	for _, it := range items {
		s.checked[it.index] = true
	}
	return s
}

func TestToggleCurrentAndCheckedIndices(t *testing.T) {
	s := multiState("a", "b", "c")

	// All start checked.
	if got := s.checkedIndices(); !reflect.DeepEqual(got, []int{0, 1, 2}) {
		t.Fatalf("initial checkedIndices = %v, want [0 1 2]", got)
	}

	// Toggle the cursor row (index 0) off.
	s.toggleCurrent()
	if got := s.checkedIndices(); !reflect.DeepEqual(got, []int{1, 2}) {
		t.Errorf("after toggle 0: %v, want [1 2]", got)
	}

	// Move cursor to index 1 and toggle it off too.
	s.cursor = 1
	s.toggleCurrent()
	if got := s.checkedIndices(); !reflect.DeepEqual(got, []int{2}) {
		t.Errorf("after toggle 1: %v, want [2]", got)
	}

	// Toggle index 0 back on; order must follow the original list.
	s.cursor = 0
	s.toggleCurrent()
	if got := s.checkedIndices(); !reflect.DeepEqual(got, []int{0, 2}) {
		t.Errorf("after re-toggle 0: %v, want [0 2]", got)
	}
}

func TestCheckedIndicesEmptyIsNonNil(t *testing.T) {
	s := multiState("a", "b")
	s.checked[0] = false
	s.checked[1] = false
	got := s.checkedIndices()
	if got == nil {
		t.Fatal("checkedIndices returned nil, want non-nil empty slice (confirmed-nothing)")
	}
	if len(got) != 0 {
		t.Errorf("checkedIndices = %v, want empty", got)
	}
}

func TestToggleCurrentNoopOutsideMulti(t *testing.T) {
	s := multiState("a")
	s.multi = false
	s.toggleCurrent() // must not panic or change state
	if !s.checked[0] {
		t.Error("toggleCurrent mutated state in single-select mode")
	}
}

func TestRenderMultiShowsCheckboxes(t *testing.T) {
	s := multiState("api-gateway", "billing")
	s.checked[1] = false // billing unchecked

	var buf bytes.Buffer
	render(&buf, "Select repos:", s)
	got := buf.String()

	if !strings.Contains(got, "[x] ") {
		t.Errorf("multi render missing checked glyph [x]\n%s", got)
	}
	if !strings.Contains(got, "[ ] ") {
		t.Errorf("multi render missing unchecked glyph [ ]\n%s", got)
	}
	if !strings.Contains(got, "space toggle") || !strings.Contains(got, "1 of 2 selected") {
		t.Errorf("multi help line wrong\n%s", got)
	}
}

func TestRenderSingleHasNoCheckbox(t *testing.T) {
	items := []Item{{Label: "solo", Desc: "/p/solo", index: 0}}
	s := &state{allItems: items, filtered: items, labelWidth: computeLabelWidth(items)}

	var buf bytes.Buffer
	render(&buf, "Select a repo:", s)
	got := buf.String()

	if strings.Contains(got, "[x]") || strings.Contains(got, "[ ]") {
		t.Errorf("single-select render leaked a checkbox glyph\n%s", got)
	}
	if !strings.Contains(got, "enter select") {
		t.Errorf("single-select help line missing\n%s", got)
	}
}
