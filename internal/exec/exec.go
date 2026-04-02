// Package exec runs arbitrary user commands in a given directory. This is the
// only package besides internal/git/ that calls os/exec.Command, because it
// runs user-supplied commands rather than git commands.
package exec

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
)

// Result holds the outcome of a command execution.
type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// RunCommand executes a command in the given directory and returns the captured
// output and exit code.
func RunCommand(ctx context.Context, dir string, args []string) (*Result, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("no command provided")
	}

	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = dir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	r := &Result{
		Stdout: stdout.String(),
		Stderr: stderr.String(),
	}

	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			r.ExitCode = exitErr.ExitCode()
			return r, nil
		}
		return r, fmt.Errorf("executing command: %w", err)
	}

	return r, nil
}
