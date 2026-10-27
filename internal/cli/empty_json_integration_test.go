package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/CelikE/soko/internal/config"
)

// Every successful JSON command must return one document, including when
// there is no registry, a selector misses, or a post-collection filter drops
// every row. --quiet must not hide the document.
func TestIntegration_EmptySelectionJSON(t *testing.T) {
	for _, fixture := range []string{"empty registry", "registered"} {
		t.Run(fixture, func(t *testing.T) {
			testEnv(t)
			var configFile string
			var before []byte
			var repo string
			var refs, head, status string
			if fixture == "registered" {
				repo = filepath.Join(t.TempDir(), "api")
				initRepo(t, repo)
				runSokoInit(t, repo)
				refs = gitIn(t, repo, "for-each-ref", "--format=%(refname) %(objectname)")
				head = gitIn(t, repo, "rev-parse", "HEAD")
				status = gitIn(t, repo, "status", "--porcelain")
				var err error
				configFile, err = config.Path()
				if err != nil {
					t.Fatal(err)
				}
				before, err = os.ReadFile(configFile)
				if err != nil {
					t.Fatal(err)
				}
			} else {
				var err error
				configFile, err = config.Path()
				if err != nil {
					t.Fatal(err)
				}
			}

			commands := [][]string{
				{"list"}, {"status"}, {"remotes"}, {"diff"}, {"report"},
				{"stats"}, {"health"}, {"branch"}, {"branch", "stale"},
				{"stash"}, {"stash", "pop"}, {"clean", "--force"},
				{"fetch"}, {"pull"}, {"sync"},
			}
			if fixture == "registered" {
				commands = [][]string{
					{"list", "--tag", "absent"}, {"status", "missing"},
					{"status", "--dirty"}, {"status", "--tag", "absent"},
					{"remotes", "missing"}, {"diff", "missing"},
					{"report", "missing"}, {"report", "--tag", "absent"},
					{"stats", "--tag", "absent"}, {"health", "--tag", "absent"},
					{"branch", "--tag", "absent"}, {"branch", "feature", "missing"},
					{"branch", "stale", "missing"}, {"branch", "switch", "feature", "missing"},
					{"stash", "missing"}, {"stash", "pop", "missing"},
					{"clean", "--force", "missing"},
					{"fetch", "missing"}, {"pull", "missing"}, {"sync", "missing"},
				}
			}
			for _, args := range commands {
				for _, flags := range [][]string{{"--json"}, {"--json", "--quiet"}, {"--json", "--perf"}, {"--json", "--quiet", "--perf"}} {
					t.Run(strings.Join(append(append([]string(nil), args...), flags...), " "), func(t *testing.T) {
						argv := append(append([]string(nil), args...), flags...)
						out := runSoko(t, argv...)
						var doc any
						if err := json.Unmarshal([]byte(out), &doc); err != nil {
							t.Fatalf("%v returned invalid JSON: %v\n%s", argv, err, out)
						}
						if args[0] == "stats" {
							obj, ok := doc.(map[string]any)
							if !ok || obj["repos"] != float64(0) || obj["activity_days"] != float64(30) {
								t.Fatalf("%v: expected zero stats object, got %v", argv, doc)
							}
							return
						}
						if flags[len(flags)-1] == "--perf" && (args[0] == "status" || args[0] == "fetch" || args[0] == "pull" || args[0] == "sync") {
							obj, ok := doc.(map[string]any)
							if !ok {
								t.Fatalf("%v: expected perf envelope, got %v", argv, doc)
							}
							if !reflect.DeepEqual(obj["repos"], []any{}) {
								t.Fatalf("%v: repos = %v, want []", argv, obj["repos"])
							}
							timing, ok := obj["timing"].(map[string]any)
							if !ok || timing["repos"] != float64(0) || timing["slowest"] == nil {
								t.Fatalf("%v: unexpected timing %v", argv, obj["timing"])
							}
						} else if !reflect.DeepEqual(doc, []any{}) {
							t.Fatalf("%v: expected [], got %v", argv, doc)
						}
					})
				}
			}
			if fixture == "registered" {
				after, err := os.ReadFile(configFile)
				if err != nil || !reflect.DeepEqual(before, after) {
					t.Fatalf("empty selection modified registry: %v", err)
				}
				if out := runSoko(t, "status", "api", "--json"); out == "[]\n" {
					t.Fatal("empty selections hid registered repo")
				}
				if gitIn(t, repo, "for-each-ref", "--format=%(refname) %(objectname)") != refs ||
					gitIn(t, repo, "rev-parse", "HEAD") != head ||
					gitIn(t, repo, "status", "--porcelain") != status {
					t.Fatal("empty selection modified Git refs or working tree")
				}
			} else if _, err := os.Stat(configFile); !os.IsNotExist(err) {
				t.Fatalf("empty selection created registry: %v", err)
			}
		})
	}
}
