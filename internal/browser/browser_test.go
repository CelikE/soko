package browser

import (
	"strings"
	"testing"
)

func TestOpenRejectsInvalidURLs(t *testing.T) {
	for _, tt := range []struct {
		name   string
		url    string
		prefix string
	}{
		{"invalid escape", "https://example.com/%zz", "invalid URL:"},
		{"control character", "https://example.com/repo\x00", "invalid URL:"},
		{"file", "file:///tmp/repo", "refusing to open non-HTTP URL:"},
		{"javascript", "javascript:alert(1)", "refusing to open non-HTTP URL:"},
		{"command", "cmd.exe /c calc", "refusing to open non-HTTP URL:"},
		{"relative", "example.com/repo", "refusing to open non-HTTP URL:"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := Open(tt.url)
			if err == nil || !strings.HasPrefix(err.Error(), tt.prefix) {
				t.Fatalf("Open(%q) = %v, want error starting with %q", tt.url, err, tt.prefix)
			}
		})
	}
}
