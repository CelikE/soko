package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/CelikE/soko/internal/config"
)

func TestIntegration_NoArgumentLeavesRejectBeforeEffects(t *testing.T) {
	for _, state := range []string{"empty", "invalid", "registered"} {
		t.Run(state, func(t *testing.T) {
			testEnv(t)
			t.Setenv("EDITOR", "soko-editor-must-not-run")
			path, err := config.Path()
			if err != nil {
				t.Fatal(err)
			}
			switch state {
			case "registered":
				if err := config.Save(&config.Config{Repos: []config.RepoEntry{{Name: "missing", Path: filepath.Join(t.TempDir(), "missing")}}}); err != nil {
					t.Fatal(err)
				}
			case "invalid":
				if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte("repos: ["), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			before, beforeErr := os.ReadFile(path)
			for _, command := range []string{"version", "list", "stats", "doc --fix", "alias list", "tag list", "config path", "config edit"} {
				for _, extra := range [][]string{{"extra"}, {"--", "-literal"}, {"--json", "extra", "extra"}} {
					args := append(strings.Fields(command), extra...)
					out, err := runSokoErr(t, args...)
					if err == nil || !strings.Contains(err.Error(), "unknown command") || out != "" {
						t.Errorf("%v: output=%q error=%v; want argument error with no output", args, out, err)
					}
				}
			}
			after, afterErr := os.ReadFile(path)
			if !bytes.Equal(after, before) || os.IsNotExist(afterErr) != os.IsNotExist(beforeErr) {
				t.Fatal("invalid invocations changed configuration")
			}
		})
	}
}

func TestIntegration_ShellInitModeContract(t *testing.T) {
	testEnv(t)
	for _, args := range [][]string{
		{"shell-init", "--fish", "--pwsh"},
		{"--json", "shell-init", "--pwsh", "--fish", "--quiet"},
		{"shell-init", "--fish", "--fish", "--pwsh"},
	} {
		out, err := runSokoErr(t, args...)
		if err == nil || !strings.Contains(err.Error(), "mutually exclusive") || out != "" {
			t.Errorf("%v: output=%q error=%v", args, out, err)
		}
	}
	for _, tc := range []struct {
		flags []string
		mode  string
	}{
		{nil, ""},
		{[]string{"--fish", "--fish"}, "--fish"},
		{[]string{"--pwsh", "--pwsh"}, "--pwsh"},
		{[]string{"--fish=false", "--pwsh"}, "--pwsh"},
		{[]string{"--pwsh=false", "--fish"}, "--fish"},
		{[]string{"--fish", "--pwsh", "--fish=false"}, "--pwsh"},
		{[]string{"--fish=false", "--pwsh=false"}, ""},
	} {
		wantArgs := []string{"shell-init"}
		if tc.mode != "" {
			wantArgs = append(wantArgs, tc.mode)
		}
		want := runSoko(t, wantArgs...)
		out := runSoko(t, append([]string{"shell-init"}, tc.flags...)...)
		if out != want {
			t.Errorf("flags %v generated wrong shell", tc.flags)
		}
	}
	for _, word := range []string{"fish", "pwsh", "powershell", "bash", "zsh", "typo"} {
		out, err := runSokoErr(t, "shell-init", word)
		if err == nil || !strings.Contains(err.Error(), "unknown argument") || out != "" {
			t.Errorf("shell-init %s: output=%q error=%v", word, out, err)
		}
	}
}

func TestIntegration_ArgumentParserCompatibility(t *testing.T) {
	testEnv(t)
	t.Setenv("SOKO_PERF", "")
	t.Setenv("SOKO_QUIET", "")
	for _, args := range [][]string{
		{"version"}, {"version", "--json"}, {"--json", "version"},
		{"--quiet", "version", "--perf"}, {"version", "--"},
	} {
		if out := runSoko(t, args...); out != "soko test\n" {
			t.Errorf("%v: version output=%q", args, out)
		}
	}
	dir := t.TempDir()
	initRepo(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "-needle.txt"), []byte("-needle\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "--", "-needle.txt")
	if err := config.Save(&config.Config{Repos: []config.RepoEntry{
		{Name: "-api", Path: dir, Tags: []string{"work"}, Meta: map[string]string{"owner": "team,one", "status": "active"}},
		{Name: "api-v2", Path: filepath.Join(t.TempDir(), "missing"), Tags: []string{"other"}},
	}}); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"--json", "status", "--", "-api", "-api"},
		{"status", "--json", "--dirty", "--clean", "--", "-api"},
		{"status", "--dirty", "--dirty=false", "--dirty", "--json", "--", "-api"},
		{"status", "--json", "--tag", "work", "--tag", "other,work", "--meta", "owner=team,one", "--meta", "status=active"},
	} {
		out := runSoko(t, args...)
		var rows []struct{ Name string }
		if err := json.Unmarshal([]byte(out), &rows); err != nil || len(rows) != 1 || rows[0].Name != "-api" {
			t.Errorf("%v: output=%s error=%v", args, out, err)
		}
	}
	// Leading-hyphen patterns are operands, not accidental soko flags.
	out := runSoko(t, "grep", "--json", "--", "-needle", "-api")
	if !strings.Contains(out, `"file": "-needle.txt"`) {
		t.Fatalf("grep lost leading-hyphen operand: %s", out)
	}
	for _, protocol := range []string{"__complete", "__completeNoDesc"} {
		out := runSoko(t, protocol, "status", "")
		if !strings.Contains(out, "-api\n") || !strings.HasSuffix(out, ":4\n") {
			t.Errorf("%s: unexpected completion protocol output %q", protocol, out)
		}
	}
	// A child Go test prints its argv as JSON. This works on all native platforms
	// and proves flags and empty/whitespace arguments survive actual execution.
	childArgs := []string{"--json", "--quiet", "--perf", "--tag", "work", "--", "-literal", "", "two words"}
	t.Setenv("SOKO_ARGUMENT_HELPER", "1")
	args := make([]string, 0, 8+len(childArgs))
	args = append(args, "--json", "exec", "--tag", "work", "--", os.Args[0], "-test.run=^TestArgumentHelper$", "--")
	args = append(args, childArgs...)
	out = runSoko(t, args...)
	var rows []struct{ Stdout string }
	if err := json.Unmarshal([]byte(out), &rows); err != nil || len(rows) != 1 {
		t.Fatalf("exec output=%s error=%v", out, err)
	}
	var got []string
	if err := json.Unmarshal([]byte(rows[0].Stdout), &got); err != nil || !reflect.DeepEqual(got, childArgs) {
		t.Fatalf("child args=%q, want %q; error=%v", got, childArgs, err)
	}
}

func TestIntegration_StatusPredicateUnion(t *testing.T) {
	testEnv(t)
	t.Setenv("SOKO_PERF", "")
	repos := make([]config.RepoEntry, 0, 5)
	for _, name := range []string{"clean", "dirty", "ahead", "behind"} {
		dir := filepath.Join(t.TempDir(), name)
		initRepo(t, dir)
		switch name {
		case "dirty":
			if err := os.WriteFile(filepath.Join(dir, "untracked"), []byte("work"), 0o600); err != nil {
				t.Fatal(err)
			}
		case "ahead", "behind":
			runGit(t, dir, "remote", "add", "origin", filepath.Join(t.TempDir(), "unused-remote"))
			runGit(t, dir, "update-ref", "refs/remotes/origin/master", "HEAD")
			runGit(t, dir, "branch", "--set-upstream-to=origin/master")
			runGit(t, dir, "commit", "--allow-empty", "-m", "second")
			if name == "behind" {
				runGit(t, dir, "update-ref", "refs/remotes/origin/master", "HEAD")
				runGit(t, dir, "reset", "--hard", "HEAD~1")
			}
		}
		repos = append(repos, config.RepoEntry{Name: name, Path: dir})
	}
	repos = append(repos, config.RepoEntry{Name: "missing", Path: filepath.Join(t.TempDir(), "missing")})
	if err := config.Save(&config.Config{Repos: repos}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		flags []string
		want  []string
	}{
		{[]string{"--dirty", "--clean"}, []string{"clean", "dirty"}},
		{[]string{"--dirty", "--ahead"}, []string{"dirty", "ahead"}},
		{[]string{"--clean", "--behind"}, []string{"clean", "behind"}},
		{[]string{"--dirty", "--clean", "--ahead", "--behind"}, []string{"clean", "dirty", "ahead", "behind"}},
	} {
		out := runSoko(t, append([]string{"status", "--json"}, tc.flags...)...)
		var rows []struct{ Name string }
		if err := json.Unmarshal([]byte(out), &rows); err != nil {
			t.Fatal(err)
		}
		names := make([]string, 0, len(rows))
		for _, row := range rows {
			names = append(names, row.Name)
		}
		if !reflect.DeepEqual(names, tc.want) {
			t.Errorf("%v: selected %v, want %v", tc.flags, names, tc.want)
		}
	}
}

func TestArgumentHelper(t *testing.T) {
	if os.Getenv("SOKO_ARGUMENT_HELPER") != "1" {
		return
	}
	for i, arg := range os.Args {
		if arg == "--" {
			if err := json.NewEncoder(os.Stdout).Encode(os.Args[i+1:]); err != nil {
				os.Exit(2)
			}
			os.Exit(0)
		}
	}
	os.Exit(2)
}
