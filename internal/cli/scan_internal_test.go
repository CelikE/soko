package cli

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestShortenHome(t *testing.T) {
	home := filepath.Join(t.TempDir(), "alice")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	for _, tt := range []struct {
		name string
		path string
		want string
	}{
		{"home", home, "~"},
		{"descendant", filepath.Join(home, "projects", "repo"), filepath.Join("~", "projects", "repo")},
		{"sibling prefix", home + "-projects", home + "-projects"},
		{"sibling descendant", filepath.Join(home+"-projects", "repo"), filepath.Join(home+"-projects", "repo")},
		{"relative", "alice/repo", "alice/repo"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := shortenHome(tt.path); got != tt.want {
				t.Fatalf("shortenHome(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestShortenHomeUnavailable(t *testing.T) {
	if runtime.GOOS == "plan9" {
		t.Skip("Plan 9 uses a different home environment variable")
	}
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	path := filepath.Join(t.TempDir(), "repo")
	if got := shortenHome(path); got != path {
		t.Fatalf("shortenHome(%q) = %q without a home directory", path, got)
	}
}
