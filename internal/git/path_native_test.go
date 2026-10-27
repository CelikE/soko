package git

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestToplevelNativePaths(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	names := []string{"ordinary space", "blå"}
	if runtime.GOOS != "windows" {
		names = append(names, "trailing space ", "trailing newline\n")
	}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			root := filepath.Join(base, name)
			nested := filepath.Join(root, "nested")
			if err := os.MkdirAll(nested, 0o755); err != nil {
				t.Fatal(err)
			}
			gitMust(t, root, "init")
			for _, cwd := range []string{root, nested} {
				got, err := Toplevel(t.Context(), cwd)
				if err != nil || got != root {
					t.Errorf("Toplevel(%q) = %q, %v; want native path %q", cwd, got, err, root)
				}
			}
		})
	}
}
