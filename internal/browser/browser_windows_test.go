package browser

import (
	"errors"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func TestOpenWindowsLiteralURL(t *testing.T) {
	original := shellExecute
	t.Cleanup(func() { shellExecute = original })

	for _, rawURL := range []string{
		"https://host/repo&calc",
		"https://host/repo|calc",
		"https://host/repo^&calc",
		"https://host/%CD%20repo",
		"https://host/repo?q=one&sort=two#fragment",
		"http://host/répo with spaces",
	} {
		t.Run(rawURL, func(t *testing.T) {
			calls := 0
			shellExecute = func(hwnd windows.Handle, verb, file, args, cwd *uint16, showCmd int32) error {
				calls++
				if got := windows.UTF16PtrToString(file); got != rawURL {
					t.Errorf("URL = %q, want literal %q", got, rawURL)
				}
				if hwnd != 0 || verb != nil || args != nil || cwd != nil || showCmd != windows.SW_SHOWNORMAL {
					t.Error("URL must be opened with the default Windows handler, without command arguments")
				}
				return nil
			}
			if err := Open(rawURL); err != nil {
				t.Fatalf("Open(%q): %v", rawURL, err)
			}
			if calls != 1 {
				t.Fatalf("ShellExecute called %d times, want 1", calls)
			}
		})
	}
}

func TestOpenWindowsLaunchError(t *testing.T) {
	original := shellExecute
	t.Cleanup(func() { shellExecute = original })
	want := errors.New("no browser associated")
	shellExecute = func(windows.Handle, *uint16, *uint16, *uint16, *uint16, int32) error {
		return want
	}

	err := Open("https://example.com/repo")
	if !errors.Is(err, want) || !strings.HasPrefix(err.Error(), "opening browser:") {
		t.Fatalf("Open() = %v, want wrapped launch error", err)
	}
}

func TestOpenWindowsRejectsInvalidURLBeforeLaunch(t *testing.T) {
	original := shellExecute
	t.Cleanup(func() { shellExecute = original })
	shellExecute = func(windows.Handle, *uint16, *uint16, *uint16, *uint16, int32) error {
		t.Fatal("invalid URL reached ShellExecute")
		return nil
	}

	for _, rawURL := range []string{"file:///C:/Windows/notepad.exe", "https://host/repo\x00&calc"} {
		if err := Open(rawURL); err == nil {
			t.Fatalf("Open(%q) succeeded, want URL validation error", rawURL)
		}
	}
}
