package cli

import (
	"strings"
	"testing"

	"github.com/CelikE/soko/internal/config"
)

func TestBoundedLevenshtein(t *testing.T) {
	tests := []struct {
		a, b string
		max  int
		want int
	}{
		{"auth", "auth", 3, 0},      // identical
		{"autth", "auth", 3, 1},     // deletion
		{"auth", "authx", 3, 1},     // insertion
		{"auth", "auix", 3, 2},      // two substitutions
		{"ab", "ba", 3, 2},          // transposition is 2 under Levenshtein
		{"kitten", "sitting", 3, 3}, // classic distance 3
		{"abc", "xyz", 2, 3},        // exceeds max → max+1 sentinel
		{"a", "abcdef", 2, 3},       // length gap exceeds max → max+1
	}

	for _, tt := range tests {
		t.Run(tt.a+"_"+tt.b, func(t *testing.T) {
			if got := boundedLevenshtein(tt.a, tt.b, tt.max); got != tt.want {
				t.Errorf("boundedLevenshtein(%q,%q,%d) = %d, want %d", tt.a, tt.b, tt.max, got, tt.want)
			}
		})
	}
}

func TestMaxEditDistance(t *testing.T) {
	tests := []struct {
		query string
		want  int
	}{
		{"abcd", 1},      // len 4 → 1
		{"abcde", 2},     // len 5 → 2
		{"abcdefgh", 2},  // len 8 → 2
		{"abcdefghi", 3}, // len 9 → 3
		{"", 1},          // degenerate
	}
	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			if got := maxEditDistance(tt.query); got != tt.want {
				t.Errorf("maxEditDistance(%q) = %d, want %d", tt.query, got, tt.want)
			}
		})
	}
}

func TestSuggestRepoNames(t *testing.T) {
	base := reposFromNames("auth", "api-gateway", "payments")

	tests := []struct {
		name  string
		repos []config.RepoEntry
		query string
		want  []string
	}{
		{"typo within threshold", base, "autth", []string{"auth"}},
		{"substring intent", base, "pay", []string{"payments"}},
		{"nothing close", base, "zzzzz", nil},
		{"empty query", base, "", nil},
		{"empty registry", nil, "auth", nil},
		{"exact name not suggested back", base, "auth", nil},
		{"case insensitive", base, "AUTTH", []string{"auth"}},
		{
			"multiple ordered by distance then name",
			reposFromNames("used", "user", "service"),
			"use",
			[]string{"used", "user"},
		},
		{
			"capped at three",
			reposFromNames("aab", "aac", "aad", "aae"),
			"aax",
			[]string{"aab", "aac", "aad"},
		},
		{
			"case-variant names deduped to one",
			[]config.RepoEntry{
				{Name: "Auth", Path: "/tmp/Auth"},
				{Name: "auth", Path: "/tmp/auth"},
			},
			"autth",
			[]string{"Auth"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := suggestRepoNames(tt.repos, tt.query)
			if !equalStrings(got, tt.want) {
				t.Errorf("suggestRepoNames(%q) = %v, want %v", tt.query, got, tt.want)
			}
			if len(got) > 3 {
				t.Errorf("returned %d suggestions, want ≤3", len(got))
			}
		})
	}
}

func TestNotFoundWithSuggestions(t *testing.T) {
	base := reposFromNames("auth", "api-gateway", "payments")

	withHint := notFoundWithSuggestions("autth", base)
	if !strings.Contains(withHint.Error(), "did you mean: auth?") {
		t.Errorf("error = %q, want it to contain the hint", withHint)
	}

	noHint := notFoundWithSuggestions("zzzzz", base)
	if noHint.Error() != "no repo matching: zzzzz" {
		t.Errorf("error = %q, want exactly the bare message", noHint)
	}
}

// Command-level: the hint reaches the returned error, never stdout JSON.

func TestCdSuggestion(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	seedNamedRepos(t, "auth", "api-gateway", "payments")

	_, err := runRoot(t, "cd", "autth")
	if err == nil || !strings.Contains(err.Error(), "did you mean: auth?") {
		t.Errorf("cd autth error = %v, want hint", err)
	}

	_, err = runRoot(t, "cd", "zzzzz")
	if err == nil || err.Error() != "no repo matching: zzzzz" {
		t.Errorf("cd zzzzz error = %v, want bare message (no hint)", err)
	}

	// Under --json the hint must not leak onto stdout.
	out, err := runRoot(t, "cd", "autth", "--json")
	if err == nil || !strings.Contains(err.Error(), "did you mean") {
		t.Errorf("cd autth --json error = %v, want hint", err)
	}
	if strings.TrimSpace(out) != "" {
		t.Errorf("cd autth --json stdout = %q, want empty (hint not leaked to JSON)", out)
	}
}

func TestOpenSuggestion(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	seedNamedRepos(t, "auth", "api-gateway", "payments")

	_, err := runRoot(t, "open", "paymnt")
	if err == nil || !strings.Contains(err.Error(), "did you mean: payments?") {
		t.Errorf("open paymnt error = %v, want hint", err)
	}
}

func TestRemoveSuggestion(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	seedNamedRepos(t, "auth", "api-gateway", "payments")

	_, err := runRoot(t, "remove", "ath")
	if err == nil || !strings.Contains(err.Error(), "not found: ath — did you mean: auth") {
		t.Errorf("remove ath error = %v, want not-found hint", err)
	}

	_, err = runRoot(t, "remove", "zzzzz")
	if err == nil || err.Error() != "not found: zzzzz" {
		t.Errorf("remove zzzzz error = %v, want bare not-found", err)
	}
}

func reposFromNames(names ...string) []config.RepoEntry {
	repos := make([]config.RepoEntry, len(names))
	for i, n := range names {
		repos[i] = config.RepoEntry{Name: n, Path: "/tmp/" + n}
	}
	return repos
}

// seedNamedRepos writes a config with the given repo names to the isolated
// XDG config dir for command-level tests.
func seedNamedRepos(t *testing.T, names ...string) {
	t.Helper()
	if err := config.Save(&config.Config{Repos: reposFromNames(names...)}); err != nil {
		t.Fatalf("seeding repos: %v", err)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
