package git

import "strings"

// ErrorClass is a coarse, machine-stable classification of a git failure. It
// lets callers branch on the kind of failure without regex-parsing prose.
type ErrorClass int

const (
	// ErrUnknown is the zero value, used for a nil or non-git error. Callers
	// that produce non-git failures (e.g. failing to spawn a binary) map their
	// own code; ClassifyError never returns it for a non-nil git error.
	ErrUnknown ErrorClass = iota
	// ErrGitFailure is the catch-all: git ran and failed for a reason that does
	// not match a more specific signature (including locale-translated text).
	ErrGitFailure
	// ErrNotFastForward is a --ff-only pull against a diverged branch.
	ErrNotFastForward
	// ErrMergeConflict is a rebase pull or stash pop that hit conflicts.
	ErrMergeConflict
	// ErrAuthFailed is a network or credential failure.
	ErrAuthFailed
)

// Code returns the stable JSON string code for the class, or "" for ErrUnknown
// (whose callers supply their own code).
func (c ErrorClass) Code() string {
	switch c {
	case ErrGitFailure:
		return "git_failure"
	case ErrNotFastForward:
		return "not_fast_forward"
	case ErrMergeConflict:
		return "merge_conflict"
	case ErrAuthFailed:
		return "auth_failed"
	default:
		return ""
	}
}

// Label returns a short human label for the class for table rendering, or ""
// when the class has no stable label (ErrUnknown/ErrGitFailure), in which case
// callers fall back to their existing prose extraction.
func (c ErrorClass) Label() string {
	switch c {
	case ErrNotFastForward:
		return "diverged"
	case ErrMergeConflict:
		return "conflict"
	case ErrAuthFailed:
		return "auth failed"
	default:
		return ""
	}
}

// ClassifyError buckets a git error by case-insensitive substring match. A nil
// error yields ErrUnknown; any non-empty error that matches no known signature
// degrades to ErrGitFailure — so a locale-translated message is never
// mis-bucketed, and the function never panics on unexpected input.
func ClassifyError(err error) ErrorClass {
	if err == nil {
		return ErrUnknown
	}
	msg := strings.ToLower(err.Error())
	switch {
	case containsAny(msg, "not possible to fast-forward", "non-fast-forward"):
		return ErrNotFastForward
	case containsAny(msg, "conflict", "needs merge", "would be overwritten"):
		return ErrMergeConflict
	case containsAny(msg,
		"authentication failed", "permission denied", "could not read username",
		"could not resolve host", "connection refused"):
		return ErrAuthFailed
	default:
		return ErrGitFailure
	}
}

// containsAny reports whether s contains any of the given substrings.
func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
