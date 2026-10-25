package journal

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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

func TestSaveReplacesFileAtomically(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	j := &Journal{Entries: []Entry{cleanEntry("before")}}
	if err := Save(j); err != nil {
		t.Fatal(err)
	}
	path, err := Path()
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(t.TempDir(), "previous.yaml")
	if err := os.Link(path, backup); err != nil {
		t.Skipf("hard links unavailable: %v", err)
	}
	j.Entries = append(j.Entries, cleanEntry("after"))
	if err := Save(j); err != nil {
		t.Fatal(err)
	}
	previous, err := os.ReadFile(backup)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(previous, before) {
		t.Error("Save rewrote the previous journal inode instead of replacing it")
	}
	got, err := Load()
	if err != nil || len(got.Entries) != 2 || got.Entries[1].Summary != "after" {
		t.Fatalf("replacement journal = %+v, %v; want both entries", got, err)
	}
	if runtime.GOOS != "windows" {
		for _, file := range []string{path, path + ".lock"} {
			info, err := os.Stat(file)
			if err != nil || info.Mode().Perm() != 0o600 {
				t.Errorf("journal/lock permissions: %v, %v", info, err)
			}
		}
	}
	assertNoJournalTemps(t, path)
}

func assertNoJournalTemps(t *testing.T, path string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(filepath.Dir(path), ".journal-*.tmp"))
	if err != nil || len(matches) != 0 {
		t.Errorf("temporary journal files remain: %v, %v", matches, err)
	}
}

func TestSaveCleansUpAfterReplacementFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.yaml")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(path, "keep")
	if err := os.WriteFile(marker, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	lock, err := lockJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lock.Close() }()
	if err := save(path, &Journal{Entries: []Entry{cleanEntry("new")}}); err == nil {
		t.Fatal("save over directory succeeded")
	}
	if data, err := os.ReadFile(marker); err != nil || string(data) != "keep" {
		t.Fatalf("failed save changed destination: %q, %v", data, err)
	}
	assertNoJournalTemps(t, path)
}

func TestStaleJournalCannotOverwriteOrPopNewerEntries(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	e := cleanEntry("original")
	if err := Append(&e); err != nil {
		t.Fatal(err)
	}
	j, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	e = cleanEntry("concurrent")
	if err := Append(&e); err != nil {
		t.Fatal(err)
	}
	j.Entries[0].Summary = "stale checkpoint"
	if err := Save(j); !errors.Is(err, ErrConflict) {
		t.Errorf("stale Save = %v, want ErrConflict", err)
	}
	if _, err := j.PopLast(); !errors.Is(err, ErrConflict) {
		t.Errorf("stale PopLast = %v, want ErrConflict", err)
	}
	if len(j.Entries) != 1 {
		t.Error("failed PopLast changed the loaded journal")
	}
	if err := Save(&Journal{}); !errors.Is(err, ErrConflict) {
		t.Errorf("unobserved replacement = %v, want ErrConflict", err)
	}
	got, err := Load()
	if err != nil || len(got.Entries) != 2 || got.Entries[0].Summary != "original" || got.Entries[1].Summary != "concurrent" {
		t.Fatalf("concurrent history changed: %+v, %v", got, err)
	}
	copyJournal := *got
	got.Entries[0].Summary = "checkpoint"
	if err := Save(got); err != nil {
		t.Fatal(err)
	}
	if err := Save(&copyJournal); !errors.Is(err, ErrConflict) {
		t.Errorf("copied stale snapshot = %v, want ErrConflict", err)
	}
	if popped, err := got.PopLast(); err != nil || popped.Summary != "concurrent" {
		t.Fatalf("PopLast after checkpoint = %+v, %v", popped, err)
	}
	if popped, err := got.PopLast(); err != nil || popped.Summary != "checkpoint" {
		t.Fatalf("second PopLast = %+v, %v", popped, err)
	}
	if _, err := got.PopLast(); !errors.Is(err, ErrEmpty) {
		t.Errorf("empty snapshot PopLast = %v, want ErrEmpty", err)
	}
}

func waitJournalFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		if _, err := os.Stat(path); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", path)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestJournalProcess(t *testing.T) {
	name := os.Getenv("SOKO_JOURNAL_PROCESS")
	if name == "" {
		return
	}
	path, err := Path()
	if err != nil {
		t.Fatal(err)
	}
	operation := os.Getenv("SOKO_JOURNAL_OPERATION")
	if operation == "hold" {
		lock, err := lockJournal(path)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = lock.Close() }()
	}
	if err := os.WriteFile(path+".ready-"+name, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	waitJournalFile(t, path+".release")
	if operation == "hold" {
		return
	}
	var popped []string
	for i := range 5 {
		if operation == "pop" {
			e, err := PopLast()
			if err != nil {
				t.Fatal(err)
			}
			popped = append(popped, e.Summary)
		} else {
			e := cleanEntry(fmt.Sprintf("%s-%d", name, i))
			if err := Append(&e); err != nil {
				t.Fatal(err)
			}
		}
	}
	data, err := json.Marshal(popped)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".result-"+name, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestJournalLockReleasedOnProcessExit(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	path, err := Path()
	if err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, "-test.run=^TestJournalProcess$")
	cmd.Env = append(os.Environ(), "SOKO_JOURNAL_PROCESS=holder", "SOKO_JOURNAL_OPERATION=hold")
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})
	waitJournalFile(t, path+".ready-holder")
	before, err := os.Stat(path + ".lock")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		e := cleanEntry("after crash")
		done <- Append(&e)
	}()
	select {
	case err := <-done:
		t.Fatalf("Append did not wait for another process's lock: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Append after holder exit: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("dead process stranded journal lock")
	}
	after, err := os.Stat(path + ".lock")
	if err != nil || !os.SameFile(before, after) {
		t.Fatalf("journal lock inode replaced: %v", err)
	}
}

func TestConcurrentJournalProcesses(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	path, err := Path()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	want := make(map[string]bool)
	for _, operation := range []string{"append", "pop"} {
		t.Run(operation, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			commands := make([]*exec.Cmd, 0, 4)
			outputs := make([]*bytes.Buffer, 0, 4)
			for i := range 4 {
				name := fmt.Sprintf("%s-%d", operation, i)
				cmd := exec.CommandContext(ctx, executable, "-test.run=^TestJournalProcess$")
				cmd.Env = append(os.Environ(), "SOKO_JOURNAL_PROCESS="+name, "SOKO_JOURNAL_OPERATION="+operation)
				out := &bytes.Buffer{}
				cmd.Stdout, cmd.Stderr = out, out
				if err := cmd.Start(); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					if cmd.ProcessState == nil {
						_ = cmd.Process.Kill()
						_ = cmd.Wait()
					}
				})
				commands = append(commands, cmd)
				outputs = append(outputs, out)
				waitJournalFile(t, path+".ready-"+name)
			}
			if err := os.WriteFile(path+".release", nil, 0o600); err != nil {
				t.Fatal(err)
			}
			for i, cmd := range commands {
				if err := cmd.Wait(); err != nil {
					t.Fatalf("journal process %d: %v\n%s", i, err, outputs[i])
				}
			}
			if err := os.Remove(path + ".release"); err != nil {
				t.Fatal(err)
			}
			j, err := Load()
			if err != nil {
				t.Fatal(err)
			}
			if operation == "append" {
				for _, e := range j.Entries {
					want[e.Summary] = true
				}
				if len(want) != MaxEntries {
					t.Fatalf("concurrent appends kept %d distinct entries, want %d", len(want), MaxEntries)
				}
				return
			}
			if len(j.Entries) != 0 {
				t.Fatalf("concurrent pops left %d entries", len(j.Entries))
			}
			for i := range commands {
				data, err := os.ReadFile(fmt.Sprintf("%s.result-pop-%d", path, i))
				if err != nil {
					t.Fatal(err)
				}
				var popped []string
				if err := json.Unmarshal(data, &popped); err != nil {
					t.Fatal(err)
				}
				for _, summary := range popped {
					if !want[summary] {
						t.Fatalf("popped duplicate or unknown entry %q", summary)
					}
					delete(want, summary)
				}
			}
			if len(want) != 0 {
				t.Errorf("entries never returned by PopLast: %v", want)
			}
		})
		if t.Failed() {
			return
		}
	}
}
