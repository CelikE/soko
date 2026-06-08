package cli

import (
	"errors"
	"testing"
)

func TestGitErrorCode(t *testing.T) {
	cases := []struct {
		msg  string
		want string
	}{
		{"fatal: Not possible to fast-forward, aborting.", codeNotFastFwd},
		{"CONFLICT (content): Merge conflict in foo.go", codeMergeConflict},
		{"fatal: Authentication failed for 'https://x'", codeAuthFailed},
		{"fatal: not a git repository", codeGitFailure},
		{"some untranslated locale error", codeGitFailure},
	}
	for _, c := range cases {
		if got := gitErrorCode(errors.New(c.msg)); got != c.want {
			t.Errorf("gitErrorCode(%q) = %q, want %q", c.msg, got, c.want)
		}
	}
}
