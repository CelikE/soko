package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

// initRepoWithFiles creates a repo with the given files committed, so git grep
// has tracked content to search.
func initRepoWithFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := initTestRepo(t)
	ctx := context.Background()

	for name, content := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	for _, args := range [][]string{{"add", "."}, {"commit", "-m", "fixtures"}} {
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return dir
}

func TestGrepLineMode(t *testing.T) {
	ctx := context.Background()
	dir := initRepoWithFiles(t, map[string]string{
		"a.go":     "package a\nfunc handleAuth() {}\n",
		"b.go":     "package b\n// nothing here\n",
		"sub/c.go": "package c\nvar x = handleAuth\n",
	})

	matches, err := Grep(ctx, dir, "handleAuth", false, false, false)
	if err != nil {
		t.Fatalf("Grep: %v", err)
	}
	if len(matches) != 2 {
		t.Fatalf("matches = %d, want 2: %+v", len(matches), matches)
	}
	for _, m := range matches {
		if m.Line < 1 {
			t.Errorf("match has non-positive line: %+v", m)
		}
		if m.File == "" || m.Text == "" {
			t.Errorf("match missing file/text: %+v", m)
		}
	}
}

func TestGrepNoMatchIsNotError(t *testing.T) {
	ctx := context.Background()
	dir := initRepoWithFiles(t, map[string]string{"a.txt": "hello world\n"})

	matches, err := Grep(ctx, dir, "zzznope", false, false, false)
	if err != nil {
		t.Fatalf("Grep no-match returned error: %v", err)
	}
	if matches != nil {
		t.Errorf("no-match matches = %+v, want nil", matches)
	}
}

func TestGrepIgnoreCase(t *testing.T) {
	ctx := context.Background()
	dir := initRepoWithFiles(t, map[string]string{"a.txt": "Hello World\n"})

	if m, err := Grep(ctx, dir, "hello", false, false, false); err != nil || len(m) != 0 {
		t.Errorf("case-sensitive 'hello' = %+v (err %v), want no match", m, err)
	}
	if m, err := Grep(ctx, dir, "hello", false, true, false); err != nil || len(m) != 1 {
		t.Errorf("case-insensitive 'hello' = %+v (err %v), want 1 match", m, err)
	}
}

func TestGrepRegexVsFixed(t *testing.T) {
	ctx := context.Background()
	dir := initRepoWithFiles(t, map[string]string{
		"abc.txt":   "abc\n",
		"adotc.txt": "a.c\n",
	})

	// Fixed string "a.c" matches only the literal "a.c".
	fixed, err := Grep(ctx, dir, "a.c", false, false, false)
	if err != nil {
		t.Fatalf("fixed Grep: %v", err)
	}
	if len(fixed) != 1 || fixed[0].File != "adotc.txt" {
		t.Errorf("fixed 'a.c' = %+v, want only adotc.txt", fixed)
	}

	// Extended regex "a.c" matches both "abc" and "a.c" (. is any char).
	re, err := Grep(ctx, dir, "a.c", true, false, false)
	if err != nil {
		t.Fatalf("regex Grep: %v", err)
	}
	if len(re) != 2 {
		t.Errorf("regex 'a.c' = %+v, want 2 matches", re)
	}
}

func TestGrepFilesOnly(t *testing.T) {
	ctx := context.Background()
	dir := initRepoWithFiles(t, map[string]string{
		"a.txt": "TODO one\nTODO two\n", // two hits, one file
		"b.txt": "TODO three\n",
		"c.txt": "clean\n",
	})

	matches, err := Grep(ctx, dir, "TODO", false, false, true)
	if err != nil {
		t.Fatalf("Grep files-only: %v", err)
	}
	if len(matches) != 2 {
		t.Fatalf("files-only matches = %d, want 2 (one per file): %+v", len(matches), matches)
	}
	for _, m := range matches {
		if m.Line != 0 || m.Text != "" {
			t.Errorf("files-only match should carry only File: %+v", m)
		}
	}
}

func TestGrepBadRegexErrors(t *testing.T) {
	ctx := context.Background()
	dir := initRepoWithFiles(t, map[string]string{"a.txt": "hello\n"})

	if _, err := Grep(ctx, dir, "[", true, false, false); err == nil {
		t.Error("Grep with malformed regex should return an error")
	}
}

func TestGrepColonInText(t *testing.T) {
	ctx := context.Background()
	dir := initRepoWithFiles(t, map[string]string{
		"u.txt": "see https://example.com:8080/path for details\n",
	})

	matches, err := Grep(ctx, dir, "https", false, false, false)
	if err != nil {
		t.Fatalf("Grep: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("matches = %d, want 1", len(matches))
	}
	if matches[0].File != "u.txt" || matches[0].Line != 1 {
		t.Errorf("match loc = %+v, want u.txt:1", matches[0])
	}
	if want := "https://example.com:8080/path"; !strings.Contains(matches[0].Text, want) {
		t.Errorf("match text = %q, want to contain %q", matches[0].Text, want)
	}
}

func TestGrepPreservesFilenames(t *testing.T) {
	tests := []struct {
		name      string
		file      string
		posixOnly bool
	}{
		{name: "non-ASCII", file: "päth.txt"},
		{name: "nested Unicode", file: "sub/日本語.txt"},
		{name: "spaces", file: "file with spaces.txt"},
		{name: "colon", file: "a:b.txt", posixOnly: true},
		{name: "newline", file: "line\nbreak.txt", posixOnly: true},
		{name: "tab", file: "tab\tname.txt", posixOnly: true},
		{name: "quote", file: "quote\"name.txt", posixOnly: true},
		{name: "backslash", file: "back\\slash.txt", posixOnly: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.posixOnly && runtime.GOOS == "windows" {
				t.Skip("filename is not supported on Windows")
			}
			ctx := context.Background()
			text := "needle: https://example.com:8080/path \t "
			dir := initRepoWithFiles(t, map[string]string{
				tt.file: "before\n" + text + "\nneedle again\n",
			})
			if _, err := Run(ctx, dir, "config", "core.quotePath", "true"); err != nil {
				t.Fatalf("git config: %v", err)
			}
			for _, filesOnly := range []bool{false, true} {
				matches, err := Grep(ctx, dir, "needle", false, false, filesOnly)
				if err != nil {
					t.Fatalf("Grep(filesOnly=%v): %v", filesOnly, err)
				}
				want := []Match{
					{File: tt.file, Line: 2, Text: text},
					{File: tt.file, Line: 3, Text: "needle again"},
				}
				if filesOnly {
					want = []Match{{File: tt.file}}
				}
				if !reflect.DeepEqual(matches, want) {
					t.Errorf("Grep(filesOnly=%v) = %#v, want %#v", filesOnly, matches, want)
				}
			}
		})
	}
}

func TestGrepBinaryMatches(t *testing.T) {
	ctx := context.Background()
	dir := initRepoWithFiles(t, map[string]string{
		"a.bin": "needle\x00binary data\n",
		"b.txt": "needle text\n",
	})
	for _, filesOnly := range []bool{false, true} {
		matches, err := Grep(ctx, dir, "needle", false, false, filesOnly)
		if err != nil {
			t.Fatalf("Grep(filesOnly=%v): %v", filesOnly, err)
		}
		want := []Match{{File: "b.txt", Line: 1, Text: "needle text"}}
		if filesOnly {
			want = []Match{{File: "a.bin"}, {File: "b.txt"}}
		}
		if !reflect.DeepEqual(matches, want) {
			t.Errorf("Grep(filesOnly=%v) = %#v, want %#v", filesOnly, matches, want)
		}
	}
}
