package exec

import (
	"context"
	"os"
	"testing"
)

func TestRunCommand_Success(t *testing.T) {
	r, err := RunCommand(context.Background(), ".", []string{"echo", "hello"})
	if err != nil {
		t.Fatalf("RunCommand() error = %v", err)
	}
	if r.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", r.ExitCode)
	}
	if r.Stdout != "hello\n" {
		t.Errorf("Stdout = %q, want %q", r.Stdout, "hello\n")
	}
}

func TestRunCommand_NonZeroExit(t *testing.T) {
	r, err := RunCommand(context.Background(), ".", []string{"sh", "-c", "exit 42"})
	if err != nil {
		t.Fatalf("RunCommand() error = %v, want nil (exit code captured)", err)
	}
	if r.ExitCode != 42 {
		t.Errorf("ExitCode = %d, want 42", r.ExitCode)
	}
}

func TestRunCommand_Stderr(t *testing.T) {
	r, err := RunCommand(context.Background(), ".", []string{"sh", "-c", "echo oops >&2"})
	if err != nil {
		t.Fatalf("RunCommand() error = %v", err)
	}
	if r.Stderr != "oops\n" {
		t.Errorf("Stderr = %q, want %q", r.Stderr, "oops\n")
	}
}

func TestRunCommand_EmptyArgs(t *testing.T) {
	_, err := RunCommand(context.Background(), ".", nil)
	if err == nil {
		t.Fatal("RunCommand(nil) should return error")
	}
}

func TestRunCommand_CommandNotFound(t *testing.T) {
	_, err := RunCommand(context.Background(), ".", []string{"nonexistent-command-xyz"})
	if err == nil {
		t.Fatal("RunCommand(nonexistent) should return error")
	}
}

func TestRunCommand_Dir(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/marker.txt", []byte("found"), 0o644); err != nil {
		t.Fatalf("writing marker: %v", err)
	}

	r, err := RunCommand(context.Background(), dir, []string{"cat", "marker.txt"})
	if err != nil {
		t.Fatalf("RunCommand() error = %v", err)
	}
	if r.Stdout != "found" {
		t.Errorf("Stdout = %q, want %q", r.Stdout, "found")
	}
}

func TestRunCommand_CancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := RunCommand(ctx, ".", []string{"sleep", "10"})
	if err == nil {
		t.Fatal("RunCommand with cancelled context should return error")
	}
}
