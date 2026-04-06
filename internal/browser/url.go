package browser

import (
	"strings"
)

// RemoteToHTTPS converts a git remote URL to an HTTPS browser URL.
// Handles SSH (git@host:user/repo.git), HTTPS, and ssh:// protocol URLs.
func RemoteToHTTPS(remote string) string {
	remote = strings.TrimSpace(remote)

	// Strip .git suffix.
	remote = strings.TrimSuffix(remote, ".git")

	// SSH protocol format: ssh://git@host/user/repo
	if strings.HasPrefix(remote, "ssh://") {
		remote = strings.TrimPrefix(remote, "ssh://")
		if at := strings.Index(remote, "@"); at != -1 {
			remote = remote[at+1:]
		}
		return "https://" + remote
	}

	// Already HTTPS.
	if strings.HasPrefix(remote, "https://") || strings.HasPrefix(remote, "http://") {
		return remote
	}

	// SSH format: git@host:user/repo
	if at := strings.Index(remote, "@"); at != -1 {
		rest := remote[at+1:]
		// Replace first : with / (host:user/repo → host/user/repo)
		if colon := strings.Index(rest, ":"); colon != -1 {
			rest = rest[:colon] + "/" + rest[colon+1:]
		}
		return "https://" + rest
	}

	// Unknown format, return as-is.
	return remote
}
