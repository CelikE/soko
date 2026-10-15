//go:build !windows

package browser

import (
	"context"
	"os/exec"
	"runtime"
)

func openURL(rawURL string) error {
	cmd := "xdg-open"
	if runtime.GOOS == "darwin" {
		cmd = "open"
	}
	return exec.CommandContext(context.Background(), cmd, rawURL).Start()
}
