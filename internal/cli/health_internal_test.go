package cli

import (
	"reflect"
	"slices"
	"testing"
)

func TestScoreRepo(t *testing.T) {
	tests := []struct {
		name     string
		stats    repoStats
		wantMin  int // minimum expected score (exact where deterministic)
		wantSev  string
		contains []string // reason substrings that must be present, in order
	}{
		{
			name:     "clean repo scores zero and is ok",
			stats:    repoStats{hasRemote: true},
			wantMin:  0,
			wantSev:  sevOK,
			contains: []string{"clean", "in sync"},
		},
		{
			name:     "dirty only is warn",
			stats:    repoStats{hasRemote: true, changes: 3},
			wantMin:  scorePerChange * 3,
			wantSev:  sevWarn,
			contains: []string{"3 changes"},
		},
		{
			name:     "single conflict forces crit regardless of low score",
			stats:    repoStats{hasRemote: true, conflicts: 1},
			wantMin:  scoreConflict,
			wantSev:  sevCrit,
			contains: []string{"1 conflict"},
		},
		{
			name:     "detached head forces crit",
			stats:    repoStats{hasRemote: true, detached: true},
			wantMin:  scoreDetached,
			wantSev:  sevCrit,
			contains: []string{"detached HEAD"},
		},
		{
			name:    "significantly behind forces crit",
			stats:   repoStats{hasRemote: true, behind: significantBehindThreshold + 1},
			wantMin: scorePerBehindCommit*(significantBehindThreshold+1) + scoreSignificantBehind,
			wantSev: sevCrit,
		},
		{
			name:    "behind below threshold stays warn",
			stats:   repoStats{hasRemote: true, behind: 2},
			wantMin: scorePerBehindCommit * 2,
			wantSev: sevWarn,
		},
		{
			name:     "missing repo scores 100 crit",
			stats:    repoStats{missing: true},
			wantMin:  scoreMissing,
			wantSev:  sevCrit,
			contains: []string{"missing"},
		},
		{
			name:     "no remote alone is a mild warn",
			stats:    repoStats{hasRemote: false},
			wantMin:  scoreNoRemote,
			wantSev:  sevWarn,
			contains: []string{"no remote"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score, sev, reasons := scoreRepo(&tt.stats)
			if score < tt.wantMin {
				t.Errorf("score = %d, want >= %d", score, tt.wantMin)
			}
			if sev != tt.wantSev {
				t.Errorf("severity = %q, want %q", sev, tt.wantSev)
			}
			for _, sub := range tt.contains {
				if !slices.Contains(reasons, sub) {
					t.Errorf("reasons %v, want to contain %q", reasons, sub)
				}
			}
		})
	}
}

func TestScoreRepoReasonOrderAndPlural(t *testing.T) {
	// Blocking signals (conflict, detached) must come before divergence and
	// housekeeping, and counts must pluralize correctly.
	r := repoStats{
		hasRemote:     true,
		conflicts:     2,
		detached:      true,
		behind:        4,
		changes:       1,
		staleBranches: 1,
	}
	_, sev, reasons := scoreRepo(&r)
	if sev != sevCrit {
		t.Fatalf("severity = %q, want crit", sev)
	}
	want := []string{"2 conflicts", "detached HEAD", "4 behind", "1 change", "1 stale branch"}
	if !reflect.DeepEqual(reasons, want) {
		t.Errorf("reasons = %v, want %v", reasons, want)
	}
}

func TestRankHealthWorstFirstStableTie(t *testing.T) {
	stats := []repoStats{
		{name: "clean", hasRemote: true},
		{name: "crit", hasRemote: true, conflicts: 1},
		{name: "warn", hasRemote: true, changes: 1},
		// Same score as "warn" — tie must break by name (alpha < warn).
		{name: "alpha", hasRemote: true, changes: 1},
	}
	got := rankHealth(stats)

	wantOrder := []string{"crit", "alpha", "warn", "clean"}
	for i, name := range wantOrder {
		if got[i].Name != name {
			t.Errorf("rank %d = %q, want %q (full: %+v)", i, got[i].Name, name, names(got))
		}
	}
	// Descending by score.
	for i := 1; i < len(got); i++ {
		if got[i-1].Score < got[i].Score {
			t.Errorf("not sorted worst-first: %d(%d) < %d(%d)", i-1, got[i-1].Score, i, got[i].Score)
		}
	}
}

func TestSeverityRank(t *testing.T) {
	if severityRank(sevCrit) <= severityRank(sevWarn) || severityRank(sevWarn) <= severityRank(sevOK) {
		t.Errorf("severityRank ordering wrong: crit=%d warn=%d ok=%d",
			severityRank(sevCrit), severityRank(sevWarn), severityRank(sevOK))
	}
}

func names(entries []healthEntry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Name
	}
	return out
}
