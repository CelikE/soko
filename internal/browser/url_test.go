package browser

import (
	"testing"
)

func TestRemoteToHTTPS(t *testing.T) {
	tests := []struct {
		name   string
		remote string
		want   string
	}{
		{
			name:   "ssh with .git",
			remote: "git@github.com:user/repo.git",
			want:   "https://github.com/user/repo",
		},
		{
			name:   "ssh without .git",
			remote: "git@github.com:user/repo",
			want:   "https://github.com/user/repo",
		},
		{
			name:   "https with .git",
			remote: "https://github.com/user/repo.git",
			want:   "https://github.com/user/repo",
		},
		{
			name:   "https without .git",
			remote: "https://github.com/user/repo",
			want:   "https://github.com/user/repo",
		},
		{
			name:   "ssh protocol",
			remote: "ssh://git@gitlab.com/org/repo.git",
			want:   "https://gitlab.com/org/repo",
		},
		{
			name:   "gitlab ssh",
			remote: "git@gitlab.com:org/group/repo.git",
			want:   "https://gitlab.com/org/group/repo",
		},
		{
			name:   "bitbucket ssh",
			remote: "git@bitbucket.org:team/repo.git",
			want:   "https://bitbucket.org/team/repo",
		},
		{
			name:   "http",
			remote: "http://github.com/user/repo.git",
			want:   "http://github.com/user/repo",
		},
		{
			name:   "whitespace trimmed",
			remote: "  git@github.com:user/repo.git  \n",
			want:   "https://github.com/user/repo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RemoteToHTTPS(tt.remote)
			if got != tt.want {
				t.Errorf("RemoteToHTTPS(%q) = %q, want %q", tt.remote, got, tt.want)
			}
		})
	}
}

func TestSubPagePath(t *testing.T) {
	tests := []struct {
		name    string
		repoURL string
		page    Page
		want    string
	}{
		{
			name:    "github prs",
			repoURL: "https://github.com/user/repo",
			page:    PagePRs,
			want:    "/pulls",
		},
		{
			name:    "github issues",
			repoURL: "https://github.com/user/repo",
			page:    PageIssues,
			want:    "/issues",
		},
		{
			name:    "github actions",
			repoURL: "https://github.com/user/repo",
			page:    PageActions,
			want:    "/actions",
		},
		{
			name:    "gitlab merge requests",
			repoURL: "https://gitlab.com/org/repo",
			page:    PagePRs,
			want:    "/-/merge_requests",
		},
		{
			name:    "gitlab pipelines",
			repoURL: "https://gitlab.com/org/repo",
			page:    PageActions,
			want:    "/-/pipelines",
		},
		{
			name:    "bitbucket pull requests",
			repoURL: "https://bitbucket.org/team/repo",
			page:    PagePRs,
			want:    "/pull-requests",
		},
		{
			name:    "home page returns empty",
			repoURL: "https://github.com/user/repo",
			page:    PageHome,
			want:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SubPagePath(tt.repoURL, tt.page)
			if got != tt.want {
				t.Errorf("SubPagePath(%q, %d) = %q, want %q", tt.repoURL, tt.page, got, tt.want)
			}
		})
	}
}
