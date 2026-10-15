package cli

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CelikE/soko/internal/config"
	"github.com/CelikE/soko/internal/output"
)

func TestExecJSONExitStatus(t *testing.T) {
	t.Cleanup(func() { output.SetPerf(false) })

	for _, seq := range []bool{false, true} {
		for _, perf := range []bool{false, true} {
			t.Run(fmt.Sprintf("seq=%t/perf=%t", seq, perf), func(t *testing.T) {
				for _, tt := range []struct {
					name          string
					args          []string
					missing       bool
					wantError     string
					wantExitCodes [2]int
				}{
					{name: "success", args: []string{"git", "--version"}},
					{name: "command failure", args: []string{"false"}, wantError: "2 repos failed", wantExitCodes: [2]int{1, 1}},
					{name: "missing path", args: []string{"git", "--version"}, missing: true, wantError: "1 repo failed", wantExitCodes: [2]int{0, 1}},
					{name: "spawn failure", args: []string{"soko-no-such-binary-xyz"}, wantError: "2 repos failed", wantExitCodes: [2]int{1, 1}},
				} {
					t.Run(tt.name, func(t *testing.T) {
						t.Setenv("XDG_CONFIG_HOME", t.TempDir())
						firstPath, secondPath := t.TempDir(), t.TempDir()
						if tt.missing {
							secondPath = filepath.Join(secondPath, "missing")
						}
						saveConfig(t,
							config.RepoEntry{Name: "first", Path: firstPath},
							config.RepoEntry{Name: "second", Path: secondPath},
						)

						args := []string{"exec", "--json", fmt.Sprintf("--seq=%t", seq), fmt.Sprintf("--perf=%t", perf), "--"}
						out, stderr, err := runRootIO(t, append(args, tt.args...)...)
						if tt.wantError == "" {
							if err != nil {
								t.Fatalf("exec: %v", err)
							}
						} else if err == nil || err.Error() != tt.wantError {
							t.Errorf("exec error = %v, want %q", err, tt.wantError)
						}
						if stderr != "" {
							t.Errorf("unexpected command stderr: %s", stderr)
						}

						data := []byte(out)
						if perf {
							var envelope struct {
								Repos  json.RawMessage       `json:"repos"`
								Timing output.TimingEnvelope `json:"timing"`
							}
							if err := json.Unmarshal(data, &envelope); err != nil {
								t.Fatalf("parse perf JSON: %v\n%s", err, out)
							}
							if envelope.Timing.Repos != 2 {
								t.Errorf("timing.repos = %d, want 2", envelope.Timing.Repos)
							}
							data = envelope.Repos
						}
						var entries []execJSON
						if err := json.Unmarshal(data, &entries); err != nil {
							t.Fatalf("parse repo JSON: %v\n%s", err, out)
						}
						if len(entries) != 2 {
							t.Fatalf("got %d repo results, want 2", len(entries))
						}
						for i, name := range []string{"first", "second"} {
							entry := entries[i]
							if entry.Name != name || entry.ExitCode != tt.wantExitCodes[i] {
								t.Errorf("entry %d = %+v, want name=%s exit_code=%d", i, entry, name, tt.wantExitCodes[i])
							}
							if entry.ExitCode == 0 && !strings.HasPrefix(entry.Stdout, "git version ") {
								t.Errorf("successful entry missing command output: %+v", entry)
							}
						}
					})
				}
			})
		}
	}
}
