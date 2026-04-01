package git

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// RepoStatus holds the parsed status of a git repository.
type RepoStatus struct {
	Branch            string
	Ahead             int
	Behind            int
	Modified          int
	Untracked         int
	Deleted           int
	Conflicts         int
	LastCommitTime    time.Time
	LastCommitMessage string
}

// ParseStatus runs git status and git log in the given directory and returns
// a populated RepoStatus.
func ParseStatus(ctx context.Context, dir string) (*RepoStatus, error) {
	statusOut, err := Run(ctx, dir, "status", "--porcelain=v2", "--branch")
	if err != nil {
		return nil, fmt.Errorf("running git status: %w", err)
	}

	rs := parseStatusOutput(statusOut)

	logOut, err := Run(ctx, dir, "log", "-1", "--format=%ct////%s")
	if err == nil {
		parseLogOutput(logOut, rs)
	}

	return rs, nil
}

// parseStatusOutput parses the raw output of `git status --porcelain=v2 --branch`
// into a RepoStatus. This is separated from ParseStatus so it can be unit tested
// with hardcoded strings.
func parseStatusOutput(raw string) *RepoStatus {
	rs := &RepoStatus{}

	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		switch {
		case strings.HasPrefix(line, "# branch.head "):
			rs.Branch = strings.TrimPrefix(line, "# branch.head ")
		case strings.HasPrefix(line, "# branch.ab "):
			parseAheadBehind(line, rs)
		case strings.HasPrefix(line, "1 ") || strings.HasPrefix(line, "2 "):
			parseChangedEntry(line, rs)
		case strings.HasPrefix(line, "? "):
			rs.Untracked++
		case strings.HasPrefix(line, "u "):
			rs.Conflicts++
		}
	}

	return rs
}

// parseAheadBehind parses a line like "# branch.ab +3 -1" into ahead/behind counts.
func parseAheadBehind(line string, rs *RepoStatus) {
	parts := strings.Fields(line)
	if len(parts) < 4 {
		return
	}

	if v, err := strconv.Atoi(strings.TrimPrefix(parts[2], "+")); err == nil {
		rs.Ahead = v
	}
	if v, err := strconv.Atoi(strings.TrimPrefix(parts[3], "-")); err == nil {
		rs.Behind = v
	}
}

// parseChangedEntry parses a porcelain v2 changed entry line. The XY status
// pair is at index 2 (e.g., "1 .M N..." or "1 D. N..."). The first character
// is the index status, the second is the worktree status.
func parseChangedEntry(line string, rs *RepoStatus) {
	parts := strings.Fields(line)
	if len(parts) < 2 {
		return
	}

	xy := parts[1]
	if len(xy) < 2 {
		return
	}

	// Check both index (xy[0]) and worktree (xy[1]) status.
	for _, ch := range []byte{xy[0], xy[1]} {
		switch ch {
		case 'M', 'A', 'R', 'C':
			rs.Modified++
			return
		case 'D':
			rs.Deleted++
			return
		}
	}
}

const logSeparator = "////"

// parseLogOutput parses the output of `git log -1 --format=%ct////%s`.
func parseLogOutput(raw string, rs *RepoStatus) {
	parts := strings.SplitN(raw, logSeparator, 2)
	if len(parts) < 2 {
		return
	}

	ts, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return
	}

	rs.LastCommitTime = time.Unix(ts, 0)
	rs.LastCommitMessage = parts[1]
}
