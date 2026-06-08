package cli

import "github.com/CelikE/soko/internal/git"

// Stable machine-readable error codes emitted on per-repo JSON failures. The
// git-derived codes mirror git.ErrorClass.Code(); the structural ones
// (path_missing/no_upstream/unknown) are owned here because they are not
// classified from git output.
const (
	codePathMissing   = "path_missing"
	codeNoUpstream    = "no_upstream"
	codeNotFastFwd    = "not_fast_forward"
	codeMergeConflict = "merge_conflict"
	codeAuthFailed    = "auth_failed"
	codeGitFailure    = "git_failure"
	codeUnknown       = "unknown"
)

// gitErrorCode classifies a git error from internal/git into a stable JSON
// error_code. Any unrecognized git error degrades to git_failure.
func gitErrorCode(err error) string {
	switch git.ClassifyError(err) {
	case git.ErrNotFastForward:
		return codeNotFastFwd
	case git.ErrMergeConflict:
		return codeMergeConflict
	case git.ErrAuthFailed:
		return codeAuthFailed
	default:
		return codeGitFailure
	}
}
