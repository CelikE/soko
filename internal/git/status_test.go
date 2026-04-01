package git

import (
	"testing"
	"time"
)

func TestParseStatusOutput(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want RepoStatus
	}{
		{
			name: "clean repo on main",
			raw: `# branch.oid abc123
# branch.head main
# branch.upstream origin/main
# branch.ab +0 -0`,
			want: RepoStatus{
				Branch: "main",
				Ahead:  0,
				Behind: 0,
			},
		},
		{
			name: "modified untracked and deleted files",
			raw: `# branch.oid abc123
# branch.head dev
# branch.upstream origin/dev
# branch.ab +0 -0
1 .M N... 100644 100644 100644 abc123 def456 file1.go
1 .M N... 100644 100644 100644 abc123 def456 file2.go
1 .D N... 100644 100644 000000 abc123 def456 removed.go
? untracked1.go
? untracked2.go
? untracked3.go`,
			want: RepoStatus{
				Branch:    "dev",
				Ahead:     0,
				Behind:    0,
				Modified:  2,
				Deleted:   1,
				Untracked: 3,
			},
		},
		{
			name: "ahead and behind remote",
			raw: `# branch.oid abc123
# branch.head feature/auth
# branch.upstream origin/feature/auth
# branch.ab +5 -3`,
			want: RepoStatus{
				Branch: "feature/auth",
				Ahead:  5,
				Behind: 3,
			},
		},
		{
			name: "merge conflicts",
			raw: `# branch.oid abc123
# branch.head main
# branch.upstream origin/main
# branch.ab +0 -0
u UU N... 100644 100644 100644 100644 abc123 def456 ghi789 conflicted.go
u UU N... 100644 100644 100644 100644 abc123 def456 ghi789 also-conflicted.go`,
			want: RepoStatus{
				Branch:    "main",
				Conflicts: 2,
			},
		},
		{
			name: "detached HEAD",
			raw: `# branch.oid abc123def456
# branch.head (detached)`,
			want: RepoStatus{
				Branch: "(detached)",
			},
		},
		{
			name: "no upstream tracking",
			raw: `# branch.oid abc123
# branch.head new-branch
1 M. N... 100644 100644 100644 abc123 def456 staged.go`,
			want: RepoStatus{
				Branch:   "new-branch",
				Modified: 1,
			},
		},
		{
			name: "staged additions count as modified",
			raw: `# branch.oid abc123
# branch.head main
# branch.upstream origin/main
# branch.ab +1 -0
1 A. N... 000000 100644 100644 000000 abc123 new-file.go`,
			want: RepoStatus{
				Branch:   "main",
				Ahead:    1,
				Modified: 1,
			},
		},
		{
			name: "rename entry",
			raw: `# branch.oid abc123
# branch.head main
2 R. N... 100644 100644 100644 abc123 def456 R100 new-name.go	old-name.go`,
			want: RepoStatus{
				Branch:   "main",
				Modified: 1,
			},
		},
		{
			name: "mixed status",
			raw: `# branch.oid abc123
# branch.head develop
# branch.upstream origin/develop
# branch.ab +2 -1
1 .M N... 100644 100644 100644 abc123 def456 modified.go
1 .D N... 100644 100644 000000 abc123 def456 deleted.go
1 .D N... 100644 100644 000000 abc123 def456 also-deleted.go
? new-file.txt
u UU N... 100644 100644 100644 100644 abc123 def456 ghi789 conflict.go`,
			want: RepoStatus{
				Branch:    "develop",
				Ahead:     2,
				Behind:    1,
				Modified:  1,
				Deleted:   2,
				Untracked: 1,
				Conflicts: 1,
			},
		},
		{
			name: "empty output",
			raw:  "",
			want: RepoStatus{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseStatusOutput(tt.raw)

			if got.Branch != tt.want.Branch {
				t.Errorf("Branch = %q, want %q", got.Branch, tt.want.Branch)
			}
			if got.Ahead != tt.want.Ahead {
				t.Errorf("Ahead = %d, want %d", got.Ahead, tt.want.Ahead)
			}
			if got.Behind != tt.want.Behind {
				t.Errorf("Behind = %d, want %d", got.Behind, tt.want.Behind)
			}
			if got.Modified != tt.want.Modified {
				t.Errorf("Modified = %d, want %d", got.Modified, tt.want.Modified)
			}
			if got.Untracked != tt.want.Untracked {
				t.Errorf("Untracked = %d, want %d", got.Untracked, tt.want.Untracked)
			}
			if got.Deleted != tt.want.Deleted {
				t.Errorf("Deleted = %d, want %d", got.Deleted, tt.want.Deleted)
			}
			if got.Conflicts != tt.want.Conflicts {
				t.Errorf("Conflicts = %d, want %d", got.Conflicts, tt.want.Conflicts)
			}
		})
	}
}

func TestParseLogOutput(t *testing.T) {
	tests := []struct {
		name        string
		raw         string
		wantTime    time.Time
		wantMessage string
	}{
		{
			name:        "valid timestamp and message",
			raw:         "1700000000////initial commit",
			wantTime:    time.Unix(1700000000, 0),
			wantMessage: "initial commit",
		},
		{
			name:        "message with separator in it",
			raw:         "1700000000////feat: add ////support",
			wantTime:    time.Unix(1700000000, 0),
			wantMessage: "feat: add ////support",
		},
		{
			name:        "empty string",
			raw:         "",
			wantTime:    time.Time{},
			wantMessage: "",
		},
		{
			name:        "no separator",
			raw:         "1700000000",
			wantTime:    time.Time{},
			wantMessage: "",
		},
		{
			name:        "invalid timestamp",
			raw:         "notanumber////some message",
			wantTime:    time.Time{},
			wantMessage: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rs := &RepoStatus{}
			parseLogOutput(tt.raw, rs)

			if !rs.LastCommitTime.Equal(tt.wantTime) {
				t.Errorf("LastCommitTime = %v, want %v", rs.LastCommitTime, tt.wantTime)
			}
			if rs.LastCommitMessage != tt.wantMessage {
				t.Errorf("LastCommitMessage = %q, want %q", rs.LastCommitMessage, tt.wantMessage)
			}
		})
	}
}
