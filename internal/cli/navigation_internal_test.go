package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNavigationRequestsAreSessionScoped(t *testing.T) {
	base, first, second := t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	t.Setenv("SOKO_NAV_SESSION", "101")
	if err := writeNavFile(first); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SOKO_NAV_SESSION", "202")
	if err := writeNavFile(second); err != nil {
		t.Fatal(err)
	}
	for session, target := range map[string]string{"101": first, "202": second} {
		data, err := os.ReadFile(filepath.Join(base, "soko", ".nav."+session))
		if err != nil || string(data) != target {
			t.Fatalf("session %s target = %q, %v", session, data, err)
		}
	}
	t.Setenv("SOKO_NAV_SESSION", "../../escape")
	if navFilePath() != filepath.Join(base, "soko", ".nav") {
		t.Fatal("invalid session escaped config directory")
	}
	if err := writeNavFile(filepath.Join(base, "missing")); err == nil {
		t.Fatal("missing navigation target accepted")
	}
}
