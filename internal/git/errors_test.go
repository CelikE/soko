package git

import (
	"errors"
	"testing"
)

func TestClassifyError(t *testing.T) {
	cases := []struct {
		name string
		msg  string
		want ErrorClass
	}{
		{"ff-only divergence", "git pull --ff-only: exit status 128: fatal: Not possible to fast-forward, aborting.", ErrNotFastForward},
		{"non-fast-forward rejection", "Updates were rejected because the tip is behind (non-fast-forward)", ErrNotFastForward},
		{"merge conflict", "CONFLICT (content): Merge conflict in foo.go", ErrMergeConflict},
		{"needs merge", "error: path 'x' needs merge", ErrMergeConflict},
		{"would be overwritten", "error: Your local changes would be overwritten by merge", ErrMergeConflict},
		{"auth failed", "fatal: Authentication failed for 'https://example.com/repo.git'", ErrAuthFailed},
		{"permission denied", "git@github.com: Permission denied (publickey).", ErrAuthFailed},
		{"could not read username", "fatal: could not read Username for 'https://github.com'", ErrAuthFailed},
		{"could not resolve host", "ssh: Could not resolve hostname example.com: nodename nor servname provided", ErrAuthFailed},
		{"connection refused", "fatal: unable to access 'https://x/': Failed to connect: Connection refused", ErrAuthFailed},
		{"generic git error", "fatal: not a git repository", ErrGitFailure},
		{"locale-translated (no match)", "fatal: impossible d'avancer rapidement", ErrGitFailure},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ClassifyError(errors.New(c.msg)); got != c.want {
				t.Errorf("ClassifyError(%q) = %v, want %v", c.msg, got, c.want)
			}
		})
	}

	if got := ClassifyError(nil); got != ErrUnknown {
		t.Errorf("ClassifyError(nil) = %v, want ErrUnknown", got)
	}
}

func TestErrorClassCode(t *testing.T) {
	cases := []struct {
		c    ErrorClass
		want string
	}{
		{ErrUnknown, ""},
		{ErrGitFailure, "git_failure"},
		{ErrNotFastForward, "not_fast_forward"},
		{ErrMergeConflict, "merge_conflict"},
		{ErrAuthFailed, "auth_failed"},
	}
	for _, c := range cases {
		if got := c.c.Code(); got != c.want {
			t.Errorf("ErrorClass(%d).Code() = %q, want %q", c.c, got, c.want)
		}
	}
}
