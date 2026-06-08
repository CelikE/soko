package cli

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/CelikE/soko/internal/output"
)

// detachedBranch is the branch.head value git reports for a detached HEAD in
// `git status --porcelain=v2 --branch`.
const detachedBranch = "(detached)"

// Urgency weights. Higher score = more neglected. The intent is
// "blocking > divergent > housekeeping"; weights are starting points, not
// load-bearing, and live here so they are auditable and unit-testable.
const (
	scoreMissing           = 100 // path gone from disk — always crit
	scoreConflict          = 50  // merge conflicts block work
	scoreDetached          = 40  // detached HEAD blocks work
	scoreSignificantBehind = 25  // bonus once Behind crosses the threshold
	scorePerBehindCommit   = 2
	scorePerChange         = 3
	scorePerStaleBranch    = 2
	scoreNoRemote          = 5
)

// Severity labels, ordered least to most urgent. severityRank gives each an
// integer so --threshold filtering and summary counting can compare them.
const (
	sevOK   = "ok"
	sevWarn = "warn"
	sevCrit = "crit"
)

func severityRank(severity string) int {
	switch severity {
	case sevCrit:
		return 2
	case sevWarn:
		return 1
	default:
		return 0
	}
}

// healthEntry is one repo's ranked health, also the --json shape.
type healthEntry struct {
	Name     string   `json:"name"`
	Path     string   `json:"path"`
	Score    int      `json:"score"`
	Severity string   `json:"severity"`
	Reasons  []string `json:"reasons"`
}

// newHealthCmd creates the cobra command for soko health.
func newHealthCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "health",
		Short: "Rank repos by an urgency score — most neglected first",
		Long: `Score every registered repo on a single urgency scale and list them worst
first, so you can see at a glance which repo needs attention. Reuses the same
signals as soko stats — dirty state, commits behind, stale branches, conflicts,
detached HEAD, missing remote — but ranks individual repos instead of
aggregating workspace totals. Read-only: it never fetches or mutates a repo.`,
		Example: `  soko health
  soko health --tag backend
  soko health --top 5
  soko health --threshold crit
  soko health --json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			w := cmd.OutOrStdout()

			threshold, _ := cmd.Flags().GetString("threshold")
			if threshold != "" && threshold != sevWarn && threshold != sevCrit {
				return fmt.Errorf("invalid --threshold %q: want %q or %q", threshold, sevWarn, sevCrit)
			}

			cfg, repos, err := loadReposWithTagFilter(cmd)
			if err != nil {
				return err
			}

			if len(repos) == 0 {
				output.Info(w, noReposMessage(len(cfg.Repos)))
				return nil
			}

			collected := collectStats(cmd, repos)
			entries := rankHealth(collected)

			if threshold != "" {
				minRank := severityRank(threshold)
				kept := entries[:0:0]
				for _, e := range entries {
					if severityRank(e.Severity) >= minRank {
						kept = append(kept, e)
					}
				}
				entries = kept
			}

			top, _ := cmd.Flags().GetInt("top")
			if top > 0 && top < len(entries) {
				entries = entries[:top]
			}

			jsonFlag, _ := cmd.Flags().GetBool("json")
			if jsonFlag {
				return output.RenderJSON(w, entries)
			}

			renderHealth(w, entries)
			renderMissingHint(w, len(findMissingRepos(repos)))
			return nil
		},
	}

	cmd.Flags().StringSlice("tag", nil, "filter by tag (can be repeated, combines with OR)")
	_ = cmd.RegisterFlagCompletionFunc("tag", tagCompletionFunc())
	cmd.Flags().Int("top", 0, "show only the N most-neglected repos (0 = all)")
	cmd.Flags().String("threshold", "", "minimum severity to display: warn or crit")
	_ = cmd.RegisterFlagCompletionFunc("threshold", cobra.FixedCompletions(
		[]cobra.Completion{sevWarn, sevCrit}, cobra.ShellCompDirectiveNoFileComp))

	return cmd
}

// scoreRepo turns a repo's collected signals into an urgency score, a severity
// label, and a human-readable, blocking-first list of reasons. It is pure (no
// I/O) so it is trivially unit-testable.
func scoreRepo(r *repoStats) (score int, severity string, reasons []string) {
	if r.missing {
		return scoreMissing, sevCrit, []string{"missing"}
	}

	crit := false

	// Blocking signals first — they dominate the reason line.
	if r.conflicts > 0 {
		score += scoreConflict
		crit = true
		reasons = append(reasons, fmt.Sprintf("%d %s", r.conflicts, output.Plural(r.conflicts, "conflict")))
	}
	if r.detached {
		score += scoreDetached
		crit = true
		reasons = append(reasons, "detached HEAD")
	}

	// Divergence from upstream.
	if r.behind > 0 {
		score += scorePerBehindCommit * r.behind
		if r.behind > significantBehindThreshold {
			score += scoreSignificantBehind
			crit = true
		}
		reasons = append(reasons, fmt.Sprintf("%d behind", r.behind))
	}

	// Housekeeping signals.
	if r.changes > 0 {
		score += scorePerChange * r.changes
		reasons = append(reasons, fmt.Sprintf("%d %s", r.changes, output.Plural(r.changes, "change")))
	}
	if r.staleBranches > 0 {
		score += scorePerStaleBranch * r.staleBranches
		reasons = append(reasons, fmt.Sprintf("%d stale %s", r.staleBranches, output.Plural(r.staleBranches, "branch")))
	}
	if !r.hasRemote {
		score += scoreNoRemote
		reasons = append(reasons, "no remote")
	}

	switch {
	case crit:
		severity = sevCrit
	case score > 0:
		severity = sevWarn
	default:
		severity = sevOK
		reasons = []string{"clean", "in sync"}
	}

	return score, severity, reasons
}

// rankHealth scores every repo and sorts the result worst-first, breaking ties
// by name for a stable, deterministic order.
func rankHealth(stats []repoStats) []healthEntry {
	entries := make([]healthEntry, len(stats))
	for i := range stats {
		score, severity, reasons := scoreRepo(&stats[i])
		entries[i] = healthEntry{
			Name:     stats[i].name,
			Path:     stats[i].path,
			Score:    score,
			Severity: severity,
			Reasons:  reasons,
		}
	}

	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].Score != entries[j].Score {
			return entries[i].Score > entries[j].Score
		}
		return entries[i].Name < entries[j].Name
	})

	return entries
}

// renderHealth prints the ranked health table and summary.
func renderHealth(w io.Writer, entries []healthEntry) {
	_, _ = fmt.Fprintf(w, "\n  %s\n\n", output.Dim("repo health — most neglected first"))

	rows := make([]output.HealthRow, len(entries))
	var crit, warn, ok int
	for i, e := range entries {
		rows[i] = output.HealthRow{
			Rank:         i + 1,
			Name:         e.Name,
			SeverityText: healthSeverityText(e.Severity),
			ScoreText:    fmt.Sprintf("%d", e.Score),
			Reason:       strings.Join(e.Reasons, " · "),
			State:        healthState(e.Severity),
		}
		switch e.Severity {
		case sevCrit:
			crit++
		case sevWarn:
			warn++
		default:
			ok++
		}
	}

	output.RenderHealthTable(w, rows)
	output.RenderHealthSummary(w, len(entries), crit, warn, ok)
}

// healthSeverityText returns the plain (uncolored) severity cell; the row is
// colored as a whole by State, mirroring the status table.
func healthSeverityText(severity string) string {
	switch severity {
	case sevCrit:
		return output.SymConflict + " crit"
	case sevWarn:
		return output.SymWarning + " warn"
	default:
		return output.SymClean + " ok"
	}
}

func healthState(severity string) output.RowState {
	switch severity {
	case sevCrit:
		return output.StateConflict
	case sevWarn:
		return output.StateDirty
	default:
		return output.StateClean
	}
}
