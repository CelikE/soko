package journal

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

func cleanEntry(summary string) Entry {
	return Entry{
		Op:      OpClean,
		Time:    time.Unix(1700000000, 0),
		Summary: summary,
		Branches: []BranchRef{
			{Repo: "r", Path: "/r", Branch: "feature", SHA: "abc123"},
		},
	}
}

// TestAppendAndLoad round-trips an entry through the on-disk journal.
func TestAppendAndLoad(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	e := cleanEntry("deleted 1 branch")
	if err := Append(&e); err != nil {
		t.Fatalf("Append: %v", err)
	}

	j, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(j.Entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(j.Entries))
	}
	last, ok := j.Last()
	if !ok {
		t.Fatal("Last returned ok=false on a non-empty journal")
	}
	if last.Summary != "deleted 1 branch" || len(last.Branches) != 1 || last.Branches[0].SHA != "abc123" {
		t.Errorf("round-trip mismatch: %+v", last)
	}
}

// TestLoadMissing returns an empty journal when the file does not exist.
func TestLoadMissing(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	j, err := Load()
	if err != nil {
		t.Fatalf("Load on missing file: %v", err)
	}
	if len(j.Entries) != 0 {
		t.Errorf("entries = %d, want 0", len(j.Entries))
	}
	if _, ok := j.Last(); ok {
		t.Error("Last returned ok=true on empty journal")
	}
}

// TestCap keeps only the most recent MaxEntries entries.
func TestCap(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	for i := range MaxEntries + 5 {
		e := cleanEntry(fmt.Sprintf("%d", i))
		if err := Append(&e); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}

	j, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(j.Entries) != MaxEntries {
		t.Fatalf("entries = %d, want %d", len(j.Entries), MaxEntries)
	}
	// The oldest five (0..4) fell off the front; the first kept is "5".
	if j.Entries[0].Summary != "5" {
		t.Errorf("oldest kept = %q, want 5", j.Entries[0].Summary)
	}
	if last, _ := j.Last(); last.Summary != fmt.Sprintf("%d", MaxEntries+4) {
		t.Errorf("newest = %q, want %d", last.Summary, MaxEntries+4)
	}
}

// TestPopLast removes entries newest-first and reports ErrEmpty when drained.
func TestPopLast(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	a, b := cleanEntry("a"), cleanEntry("b")
	if err := Append(&a); err != nil {
		t.Fatal(err)
	}
	if err := Append(&b); err != nil {
		t.Fatal(err)
	}

	popped, err := PopLast()
	if err != nil {
		t.Fatalf("PopLast: %v", err)
	}
	if popped.Summary != "b" {
		t.Errorf("popped %q, want b", popped.Summary)
	}

	j, _ := Load()
	if len(j.Entries) != 1 || j.Entries[0].Summary != "a" {
		t.Errorf("after pop, entries = %+v, want [a]", j.Entries)
	}

	if _, err := PopLast(); err != nil {
		t.Fatalf("second PopLast: %v", err)
	}
	if _, err := PopLast(); !errors.Is(err, ErrEmpty) {
		t.Errorf("PopLast on empty = %v, want ErrEmpty", err)
	}
}
