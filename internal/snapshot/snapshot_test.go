package snapshot_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/CelikE/soko/internal/snapshot"
)

func testSnap(name string, created time.Time) *snapshot.Snapshot {
	return &snapshot.Snapshot{
		Name:    name,
		Created: created,
		Repos: []snapshot.Repo{
			{Name: "repo-a", Path: "/tmp/repo-a", Branch: "main", SHA: "abc123", Dirty: true},
		},
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	want := testSnap("round", time.Date(2026, 6, 11, 10, 0, 0, 0, time.UTC))
	if err := snapshot.Save(want); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := snapshot.Load("round")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Name != want.Name || !got.Created.Equal(want.Created) || len(got.Repos) != 1 {
		t.Errorf("Load = %+v, want %+v", got, want)
	}
	if got.Repos[0] != want.Repos[0] {
		t.Errorf("Load repo = %+v, want %+v", got.Repos[0], want.Repos[0])
	}
}

func TestSaveReplacesFileAtomically(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	want := testSnap("replace", time.Now().UTC())
	if err := snapshot.Save(want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	dir, err := snapshot.Dir()
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "replace.yaml")
	before, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	// A hard link keeps the previous file observable across replacement. An
	// in-place write would truncate and overwrite this copy too.
	backup := filepath.Join(t.TempDir(), "previous.yaml")
	if err := os.Link(p, backup); err != nil {
		t.Skipf("hard links unavailable: %v", err)
	}
	want.Repos[0].SHA = "def456"
	if err := snapshot.Save(want); err != nil {
		t.Fatalf("overwriting snapshot: %v", err)
	}
	previous, err := os.ReadFile(backup)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(previous, before) {
		t.Error("Save modified the previous snapshot file instead of replacing it")
	}
	got, err := snapshot.Load(want.Name)
	if err != nil {
		t.Fatal(err)
	}
	if got.Repos[0].SHA != want.Repos[0].SHA {
		t.Errorf("saved SHA = %q, want %q", got.Repos[0].SHA, want.Repos[0].SHA)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "replace.yaml" {
		t.Errorf("snapshot directory after save = %v, want only replace.yaml", entries)
	}
}

func TestSaveCleansUpAfterReplacementFailure(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir, err := snapshot.Dir()
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "blocked.yaml")
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(p, "keep")
	if err := os.WriteFile(marker, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := snapshot.Save(testSnap("blocked", time.Now().UTC())); err == nil {
		t.Fatal("Save over a directory succeeded, want replacement failure")
	}
	if data, err := os.ReadFile(marker); err != nil || string(data) != "keep" {
		t.Fatalf("failed save changed existing destination: data=%q, err=%v", data, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "blocked.yaml" {
		t.Errorf("failed save left temporary files: %v", entries)
	}
}

func TestLoadRejectsCorruptSnapshots(t *testing.T) {
	for _, tc := range []struct {
		name string
		data string
		want string
	}{
		{"malformed", "repos: [", "parsing snapshot"},
		{"empty", "", "name"},
		{"wrong-name", "name: other\n", "name"},
		{"missing-created", "name: broken\n", "created"},
		{"missing-repos", "name: broken\ncreated: 2026-06-11T10:00:00Z\n", "repos"},
		{"incomplete-repo", "name: broken\ncreated: 2026-06-11T10:00:00Z\nrepos:\n  - name: api\n", "repo"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			dir, err := snapshot.Dir()
			if err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "broken.yaml"), []byte(tc.data), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := snapshot.Load("broken"); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("Load corrupt snapshot = %v, want error containing %q", err, tc.want)
			}
		})
	}
}

func TestLoadMissingReturnsErrNotFound(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	if _, err := snapshot.Load("nope"); !errors.Is(err, snapshot.ErrNotFound) {
		t.Errorf("Load(missing) = %v, want ErrNotFound", err)
	}
}

func TestListSortsNewestFirst(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	base := time.Date(2026, 6, 11, 10, 0, 0, 0, time.UTC)
	for i, name := range []string{"old", "mid", "new"} {
		if err := snapshot.Save(testSnap(name, base.Add(time.Duration(i)*time.Hour))); err != nil {
			t.Fatalf("Save %s: %v", name, err)
		}
	}

	names, err := snapshot.Names()
	if err != nil {
		t.Fatalf("Names: %v", err)
	}
	if len(names) != 3 || names[0] != "new" || names[2] != "old" {
		t.Errorf("Names = %v, want [new mid old]", names)
	}
}

func TestListIncludesUnreadableSnapshots(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := snapshot.Save(testSnap("healthy", time.Now().UTC())); err != nil {
		t.Fatal(err)
	}
	dir, err := snapshot.Dir()
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string]string{
		"broken.yaml":            "repos: [",
		"empty.yaml":             "",
		".snapshot-leftover.tmp": "partially written temporary file",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "directory.yaml"), 0o755); err != nil {
		t.Fatal(err)
	}
	entries, err := snapshot.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("List = %+v, want healthy and both damaged snapshots only", entries)
	}
	if entries[0].Name != "healthy" || entries[0].Repos != 1 || entries[0].Error != "" {
		t.Errorf("healthy entry = %+v, want healthy with 1 repo and no error", entries[0])
	}
	for _, entry := range entries[1:] {
		if entry.Error == "" || !entry.Created.IsZero() || entry.Repos != 0 {
			t.Errorf("unreadable entry = %+v, want filename and error without fabricated metadata", entry)
		}
	}
	names, err := snapshot.Names()
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(names, []string{"healthy", "broken", "empty"}) {
		t.Errorf("Names = %v, want [healthy broken empty]", names)
	}
}

func TestDelete(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	if err := snapshot.Save(testSnap("gone", time.Now().UTC())); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := snapshot.Delete("gone"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := snapshot.Delete("gone"); !errors.Is(err, snapshot.ErrNotFound) {
		t.Errorf("Delete(missing) = %v, want ErrNotFound", err)
	}
}

func TestValidateName(t *testing.T) {
	for _, ok := range []string{"sprint-42", "a", "Pre.Fight_1"} {
		if err := snapshot.ValidateName(ok); err != nil {
			t.Errorf("ValidateName(%q) = %v, want nil", ok, err)
		}
	}
	for _, bad := range []string{"", ".hidden", "-dash", "a/b", "../up", "has space"} {
		if err := snapshot.ValidateName(bad); err == nil {
			t.Errorf("ValidateName(%q) = nil, want error", bad)
		}
	}
}
