package browser

import (
	"net/url"
	"strings"
)

// Page represents a sub-page on a git hosting platform.
type Page int

const (
	// PageHome is the repo homepage.
	PageHome Page = iota
	// PagePRs is the pull/merge requests page.
	PagePRs
	// PageIssues is the issues page.
	PageIssues
	// PageActions is the CI/CD page.
	PageActions
	// PageBranches is the branches page.
	PageBranches
	// PageSettings is the settings page.
	PageSettings
)

type platform int

const (
	platformGitHub platform = iota
	platformGitLab
	platformBitbucket
)

// SubPagePath returns the URL path suffix for the given page on the
// detected platform.
func SubPagePath(repoURL string, page Page) string {
	if page == PageHome {
		return ""
	}

	p := detectPlatform(repoURL)

	switch p {
	case platformGitLab:
		switch page {
		case PagePRs:
			return "/-/merge_requests"
		case PageIssues:
			return "/-/issues"
		case PageActions:
			return "/-/pipelines"
		case PageBranches:
			return "/-/branches"
		case PageSettings:
			return "/-/settings"
		}
	case platformBitbucket:
		switch page {
		case PagePRs:
			return "/pull-requests"
		case PageIssues:
			return "/issues"
		case PageActions:
			return "/pipelines"
		case PageBranches:
			return "/branches"
		case PageSettings:
			return "/admin"
		}
	default: // GitHub and unknown platforms.
		switch page {
		case PagePRs:
			return "/pulls"
		case PageIssues:
			return "/issues"
		case PageActions:
			return "/actions"
		case PageBranches:
			return "/branches"
		case PageSettings:
			return "/settings"
		}
	}

	return ""
}

func detectPlatform(repoURL string) platform {
	parsed, err := url.Parse(repoURL)
	if err != nil {
		return platformGitHub
	}

	host := strings.ToLower(parsed.Hostname())

	switch {
	case strings.Contains(host, "gitlab"):
		return platformGitLab
	case strings.Contains(host, "bitbucket"):
		return platformBitbucket
	default:
		return platformGitHub
	}
}
