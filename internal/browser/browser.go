// Package browser opens URLs in the user's default browser.
package browser

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
)

// Open opens the given URL in the default browser.
func Open(url string) error {
	var cmd string
	var args []string

	switch runtime.GOOS {
	case "darwin":
		cmd = "open"
		args = []string{url}
	case "windows":
		cmd = "cmd"
		args = []string{"/c", "start", "", url}
	default: // linux, freebsd, etc.
		cmd = "xdg-open"
		args = []string{url}
	}

	if err := exec.CommandContext(context.Background(), cmd, args...).Start(); err != nil {
		return fmt.Errorf("opening browser: %w", err)
	}
	return nil
}
