package snapshot_test

import (
	"errors"
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
