package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/CelikE/soko/internal/config"
)

// Execute the examples published in the user guide so documentation cannot silently
// drift from the result shapes. Fixture paths are the only substituted values.
const documentedJSONPattern = "(?s)<!-- json-example: (\\w+) \\| ([^\\n]+) -->\\s*```json\\r?\\n(.*?)\\r?\\n```"
const manualJSONPattern = `(?s)\.nf\r?\n(.*?)\r?\n\.fi`

func TestDocumentedJSONLineEndings(t *testing.T) {
	for _, newline := range []string{"\n", "\r\n"} {
		guide := strings.Join([]string{"<!-- json-example: empty | status --json -->", "```json", "[]", "```"}, newline)
		examples := regexp.MustCompile(documentedJSONPattern).FindAllStringSubmatch(guide, -1)
		if len(examples) != 1 || examples[0][2] != "status --json" || examples[0][3] != "[]" {
			t.Errorf("user guide examples with %q endings: %q", newline, examples)
		}
		manual := strings.Join([]string{".nf", "[]", ".fi"}, newline)
		blocks := regexp.MustCompile(manualJSONPattern).FindAllStringSubmatch(manual, -1)
		if len(blocks) != 1 || blocks[0][1] != "[]" {
			t.Errorf("manual examples with %q endings: %q", newline, blocks)
		}
	}
}

func TestIntegration_DocumentedJSONExamples(t *testing.T) {
	guide, err := os.ReadFile(filepath.Join("..", "..", "docs", "guide.md"))
	if err != nil {
		t.Fatal(err)
	}
	pattern := regexp.MustCompile(documentedJSONPattern)
	examples := pattern.FindAllStringSubmatch(string(guide), -1)
	if len(examples) != 5 {
		t.Fatalf("found %d executable JSON examples, want 5", len(examples))
	}
	for _, example := range examples {
		t.Run(example[1]+"/"+example[2], func(t *testing.T) {
			testEnv(t)
			t.Setenv("SOKO_PERF", "")
			t.Setenv("SOKO_QUIET", "")
			wantJSON := example[3]
			if example[1] != "empty" {
				name := "api"
				if example[1] == "missing" {
					name = "missing"
				}
				path := filepath.Join(t.TempDir(), name)
				cfg, err := config.Load()
				if err != nil {
					t.Fatal(err)
				}
				cfg.Repos = []config.RepoEntry{{Name: name, Path: path}}
				if err := config.Save(cfg); err != nil {
					t.Fatal(err)
				}
				encodedPath, err := json.Marshal(path)
				if err != nil {
					t.Fatal(err)
				}
				wantJSON = strings.ReplaceAll(wantJSON, `"/repos/`+name+`"`, string(encodedPath))
			}
			var want any
			if err := json.Unmarshal([]byte(wantJSON), &want); err != nil {
				t.Fatalf("invalid documented JSON: %v", err)
			}
			for _, quiet := range []bool{false, true} {
				args := strings.Fields(example[2])
				if quiet {
					args = append(args, "--quiet")
				}
				out, err := runSokoErr(t, args...)
				if (err != nil) != (example[1] == "missing") {
					t.Fatalf("%v: unexpected command error: %v", args, err)
				}
				var got any
				if err := json.Unmarshal([]byte(out), &got); err != nil {
					t.Fatalf("%v: invalid output: %v\n%s", args, err, out)
				}
				if !reflect.DeepEqual(got, want) {
					t.Errorf("%v:\ngot  %s\nwant %s", args, out, wantJSON)
				}
			}
		})
	}

	// The manual publishes the same array, stats, and perf examples in roff.
	// Compare their decoded documents to the CLI-checked user guide examples.
	manual, err := os.ReadFile(filepath.Join("..", "..", "docs", "soko.1"))
	if err != nil {
		t.Fatal(err)
	}
	blocks := regexp.MustCompile(manualJSONPattern).FindAllStringSubmatch(string(manual), -1)
	var checked int
	for _, block := range blocks {
		text := strings.TrimSpace(block[1])
		if !strings.HasPrefix(text, "{") && !strings.HasPrefix(text, "[") {
			continue
		}
		var got any
		if err := json.Unmarshal([]byte(text), &got); err != nil {
			t.Fatalf("invalid manual JSON: %v\n%s", err, text)
		}
		matched := false
		for _, example := range examples {
			var want any
			if err := json.Unmarshal([]byte(example[3]), &want); err != nil {
				t.Fatal(err)
			}
			if reflect.DeepEqual(got, want) {
				matched = true
				break
			}
		}
		if !matched {
			t.Fatalf("manual JSON has no CLI-checked user guide counterpart: %s", text)
		}
		checked++
	}
	if checked != 3 {
		t.Fatalf("checked %d manual JSON examples, want 3", checked)
	}
}

func TestIntegration_JSONPerfCapabilities(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "api")
	initRepo(t, dir)
	runSokoInit(t, dir)
	t.Setenv("SOKO_PERF", "1")
	for _, args := range [][]string{
		{"status"}, {"fetch"}, {"pull"}, {"sync"},
		{"exec", "--", "git", "status", "--porcelain"},
		{"exec", "--seq", "--", "git", "status", "--porcelain"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			for _, perf := range []bool{false, true} {
				flags := []string{"--json", "--quiet", "--perf=false"}
				if perf {
					// The environment enables the same envelope as --perf.
					flags = flags[:2]
				}
				argv := append(append([]string(nil), flags...), args...)
				out := runSoko(t, argv...)
				if !perf {
					var rows []map[string]any
					if err := json.Unmarshal([]byte(out), &rows); err != nil || len(rows) != 1 {
						t.Fatalf("%v: expected one bare row: %v\n%s", argv, err, out)
					}
					if _, ok := rows[0]["duration_ms"]; ok {
						t.Fatalf("%v: duration_ms without perf: %s", argv, out)
					}
					continue
				}
				var envelope perfEnvelope
				if err := json.Unmarshal([]byte(out), &envelope); err != nil {
					t.Fatalf("%v: invalid envelope: %v\n%s", argv, err, out)
				}
				if len(envelope.Repos) != 1 || envelope.Timing.Repos != 1 ||
					envelope.Timing.Concurrency != 8 || len(envelope.Timing.Slowest) != 1 {
					t.Fatalf("%v: unexpected envelope: %s", argv, out)
				}
			}
		})
	}
}

func TestIntegration_JSONGeneratorAndHelpBoundaries(t *testing.T) {
	testEnv(t)
	for _, args := range [][]string{
		{"version"}, {"--help"}, {"status", "--help"}, {"shell-init"},
		{"shell-init", "--fish"}, {"shell-init", "--pwsh"},
		{"completion", "bash"}, {"completion", "zsh"},
		{"completion", "fish"}, {"completion", "powershell"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			plain := runSoko(t, args...)
			argv := append(append([]string(nil), args...), "--json", "--quiet")
			out := runSoko(t, argv...)
			if strings.TrimSpace(out) == "" || json.Valid([]byte(out)) {
				t.Fatalf("%v: expected nonempty text/script, got %q", argv, out)
			}
			if out != plain {
				t.Fatalf("%v: JSON/quiet altered text/script output", argv)
			}
		})
	}
	help := runSoko(t, "--help")
	for _, text := range []string{"JSON support is command-specific", "JSON OUTPUT", "repos/timing"} {
		if !strings.Contains(help, text) {
			t.Errorf("root help missing %q", text)
		}
	}
}

func TestIntegration_JSONInteractiveRestrictions(t *testing.T) {
	testEnv(t)
	dir := filepath.Join(t.TempDir(), "api")
	initRepo(t, dir)
	runSokoInit(t, dir)
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"clean", "--json"}, "--json requires --force or --dry-run"},
		{[]string{"prune", "--json"}, "--json requires --force or --dry-run"},
		{[]string{"clean", "--json", "--force", "--select"}, "--select cannot be combined with --json"},
		{[]string{"prune", "--json", "--force", "--select"}, "--select cannot be combined with --json"},
		{[]string{"remove", "--all", "--json", "--select"}, "--select cannot be combined with --json"},
		{[]string{"apply", "unused", "--to", "unused", "--write", "--json"}, "--json with --write requires --force"},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			out, err := runSokoErr(t, tc.args...)
			if err == nil || !strings.Contains(err.Error(), tc.want) || out != "" {
				t.Fatalf("%v: stdout=%q, error=%v; want %q without document", tc.args, out, err, tc.want)
			}
		})
	}
}
