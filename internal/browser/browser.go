// Package browser opens URLs in the user's default browser.
package browser

import (
	"fmt"
	"net/url"
)

// Open opens the given URL in the default browser. Only HTTPS and HTTP URLs
// are allowed, and URLs are never passed through a command shell.
func Open(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return fmt.Errorf("refusing to open non-HTTP URL: %s", rawURL)
	}

	if err := openURL(rawURL); err != nil {
		return fmt.Errorf("opening browser: %w", err)
	}
	return nil
}
