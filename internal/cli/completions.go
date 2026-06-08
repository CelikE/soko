package cli

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/CelikE/soko/internal/config"
)

// repoNameCompletionFunc returns a completion function that completes
// registered repo names. It fails silently if the config can't be loaded.
func repoNameCompletionFunc() cobra.CompletionFunc {
	return func(_ *cobra.Command, _ []string, _ string) ([]cobra.Completion, cobra.ShellCompDirective) {
		cfg, err := config.Load()
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		names := make([]cobra.Completion, 0, len(cfg.Repos))
		for _, r := range cfg.Repos {
			names = append(names, r.Name)
		}

		return names, cobra.ShellCompDirectiveNoFileComp
	}
}

// tagCompletionFunc returns a completion function that completes known tags.
func tagCompletionFunc() cobra.CompletionFunc {
	return func(_ *cobra.Command, _ []string, _ string) ([]cobra.Completion, cobra.ShellCompDirective) {
		cfg, err := config.Load()
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		tags := config.ListTags(cfg)
		completions := make([]cobra.Completion, len(tags))
		copy(completions, tags)

		return completions, cobra.ShellCompDirectiveNoFileComp
	}
}

// findReposMatching returns repos matching any of the given queries.
// Each query tries exact match first, then prefix match — same as config.FindRepo.
func findReposMatching(repos []config.RepoEntry, queries []string) []config.RepoEntry {
	seen := make(map[string]bool)
	var matched []config.RepoEntry

	for _, q := range queries {
		// Exact match first.
		for _, r := range repos {
			if r.Name == q && !seen[r.Path] {
				seen[r.Path] = true
				matched = append(matched, r)
			}
		}
		if seen[q] || len(matched) > 0 && matched[len(matched)-1].Name == q {
			continue
		}
		// Prefix match fallback.
		for _, r := range repos {
			if strings.HasPrefix(r.Name, q) && !seen[r.Path] {
				seen[r.Path] = true
				matched = append(matched, r)
			}
		}
	}

	return matched
}

// suggestRepoNames returns up to 3 registered repo names closest to query,
// ranked by bounded edit distance plus substring containment. It is a pure,
// local computation over the same name set repoNameCompletionFunc iterates, so
// suggestions and tab-completion never drift. A degenerate input (empty query
// or registry, or nothing within threshold) yields an empty slice. It never
// suggests a name equal to the query — callers invoke it only on a real miss.
func suggestRepoNames(repos []config.RepoEntry, query string) []string {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return nil
	}
	threshold := maxEditDistance(query)

	type candidate struct {
		name string
		dist int
	}
	var candidates []candidate
	seen := make(map[string]bool)
	for _, r := range repos {
		lname := strings.ToLower(r.Name)
		// Dedupe on the lowercased name so a duplicate or a case variant
		// ("Auth" and "auth") is suggested at most once.
		if lname == query || seen[lname] {
			continue
		}
		d := boundedLevenshtein(query, lname, threshold)
		// A name within edit-distance threshold is a typo candidate; a name
		// that contains the query as a substring is an intent candidate even
		// when the raw distance is large (e.g. "pay" → "payments").
		if d <= threshold || strings.Contains(lname, query) {
			seen[lname] = true
			candidates = append(candidates, candidate{name: r.Name, dist: d})
		}
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].dist != candidates[j].dist {
			return candidates[i].dist < candidates[j].dist
		}
		return candidates[i].name < candidates[j].name
	})
	if len(candidates) > 3 {
		candidates = candidates[:3]
	}

	out := make([]string, len(candidates))
	for i, c := range candidates {
		out[i] = c.name
	}
	return out
}

// notFoundWithSuggestions builds the "no repo matching" error shared by cd, go
// (name path), and open, appending a "did you mean" hint when there is a close
// name. With no suggestion it returns the bare message verbatim, keeping the
// existing behavior and any scripts that match on it.
func notFoundWithSuggestions(query string, repos []config.RepoEntry) error {
	if s := suggestRepoNames(repos, query); len(s) > 0 {
		return fmt.Errorf("no repo matching: %s — did you mean: %s?", query, strings.Join(s, ", "))
	}
	return fmt.Errorf("no repo matching: %s", query)
}

// maxEditDistance returns a length-relative edit-distance threshold so short
// names do not over-suggest: 1 for queries up to 4 runes, 2 for 5–8, 3 beyond.
func maxEditDistance(query string) int {
	switch n := len([]rune(query)); {
	case n <= 4:
		return 1
	case n <= 8:
		return 2
	default:
		return 3
	}
}

// boundedLevenshtein computes the Levenshtein distance between a and b over
// runes, abandoning early and returning limit+1 once the best achievable
// distance on a row exceeds limit. This keeps suggestion cost trivial on large
// registries.
func boundedLevenshtein(a, b string, limit int) int {
	ra := []rune(a)
	rb := []rune(b)
	la, lb := len(ra), len(rb)

	// The distance is at least the length difference, so bail before the DP
	// when the strings are too far apart to matter.
	if la-lb > limit || lb-la > limit {
		return limit + 1
	}

	prev := make([]int, lb+1)
	curr := make([]int, lb+1)
	for j := 0; j <= lb; j++ {
		prev[j] = j
	}

	for i := 1; i <= la; i++ {
		curr[0] = i
		rowMin := curr[0]
		for j := 1; j <= lb; j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			m := prev[j] + 1 // deletion
			if ins := curr[j-1] + 1; ins < m {
				m = ins
			}
			if sub := prev[j-1] + cost; sub < m {
				m = sub
			}
			curr[j] = m
			if m < rowMin {
				rowMin = m
			}
		}
		if rowMin > limit {
			return limit + 1
		}
		prev, curr = curr, prev
	}

	if prev[lb] > limit {
		return limit + 1
	}
	return prev[lb]
}

// loadReposWithTagFilter loads the config and applies --tag filtering if set.
func loadReposWithTagFilter(cmd *cobra.Command) (*config.Config, []config.RepoEntry, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, nil, fmt.Errorf("loading config: %w", err)
	}

	repos := cfg.Repos
	tags, _ := cmd.Flags().GetStringSlice("tag")
	if len(tags) > 0 {
		repos = config.FilterByTags(repos, tags)
	}

	return cfg, repos, nil
}
