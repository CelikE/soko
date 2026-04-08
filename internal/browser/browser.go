// Package browser opens URLs in the user's default browser.
package browser

import (
	"context"
	"fmt"
	"net/url"
	"os/exec"
	"runtime"
)

// Open opens the given URL in the default browser. Only HTTPS and HTTP URLs
// are allowed to prevent command injection via malicious remote URLs.
func Open(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return fmt.Errorf("refusing to open non-HTTP URL: %s", rawURL)
	}

	var cmd string
	var args []string

	switch runtime.GOOS {
	case "darwin":
		cmd = "open"
		args = []string{rawURL}
	case "windows":
		cmd = "cmd"
		args = []string{"/c", "start", "", rawURL}
	default: // linux, freebsd, etc.
		cmd = "xdg-open"
		args = []string{rawURL}
	}

	if err := exec.CommandContext(context.Background(), cmd, args...).Start(); err != nil {
		return fmt.Errorf("opening browser: %w", err)
	}
	return nil
}
