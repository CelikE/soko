package cli_test

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CelikE/soko/internal/cli"
	"github.com/CelikE/soko/internal/config"
)

func TestIntegration_NoWorktreesEmptySelection(t *testing.T) {
	testEnv(t)
	root, alpha, _ := familyFixture(t)
	runSokoAt(t, alpha, "init", "--worktree", "--tag", "worktree-only")
	cfg := loadRegistry(t)
	cfg.Repos[0].Tags = nil
	if err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	commands := []struct {
		name     string
		args     []string
		command  []string
		repoArgs bool
	}{
		{name: "exec", args: []string{"exec"}, command: []string{"--", "git", "rev-parse", "--show-toplevel"}},
		{name: "exec-sequential", args: []string{"exec", "--seq"}, command: []string{"--", "git", "rev-parse", "--show-toplevel"}},
		{name: "fetch", args: []string{"fetch"}, repoArgs: true},
		{name: "pull", args: []string{"pull"}, repoArgs: true},
		{name: "grep", args: []string{"grep", "test"}, repoArgs: true},
		{name: "sync", args: []string{"sync"}, repoArgs: true},
		{name: "apply-preview", args: []string{"apply", filepath.Join(root, "README.md"), "--to", "copied.md"}, repoArgs: true},
		{name: "apply-write", args: []string{"apply", filepath.Join(root, "README.md"), "--to", "copied.md", "--write", "--force"}, repoArgs: true},
	}
	for _, filter := range []struct {
		name         string
		args         []string
		repoArgs     bool
		worktreeOnly bool
	}{
		{name: "tag", args: []string{"--tag", "worktree-only"}},
		{name: "repo-name", args: []string{"api/feat/oauth"}, repoArgs: true},
		{name: "worktree-only-registry", worktreeOnly: true},
	} {
		t.Run(filter.name, func(t *testing.T) {
			selected := *cfg
			if filter.worktreeOnly {
				selected.Repos = cfg.Repos[1:]
			}
			if err := config.Save(&selected); err != nil {
				t.Fatal(err)
			}
			for _, command := range commands {
				if filter.repoArgs && !command.repoArgs {
					continue
				}
				t.Run(command.name, func(t *testing.T) {
					for _, mode := range []string{"text", "json", "quiet"} {
						t.Run(mode, func(t *testing.T) {
							args := append([]string(nil), command.args...)
							args = append(args, filter.args...)
							args = append(args, "--no-worktrees")
							if mode != "text" {
								args = append(args, "--"+mode)
							}
							args = append(args, command.command...)
							var stdout, stderr bytes.Buffer
							cmd := cli.NewRootCmd("test")
							cmd.SetOut(&stdout)
							cmd.SetErr(&stderr)
							cmd.SetArgs(args)
							if err := cmd.Execute(); err != nil {
								t.Fatalf("soko %v: %v", args, err)
							}
							out := strings.TrimSpace(stdout.String())
							switch mode {
							case "text":
								if !strings.Contains(out, "no repos match the filters") || strings.Contains(out, "\n") {
									t.Errorf("output = %q, want only the no-repos message", out)
								}
							case "json":
								if out != "[]" {
									t.Errorf("output = %q, want []", out)
								}
							case "quiet":
								if stdout.Len() != 0 {
									t.Errorf("quiet output = %q, want no output", stdout.String())
								}
							}
							if stderr.Len() != 0 {
								t.Errorf("stderr = %q, want no progress output", stderr.String())
							}
						})
					}
				})
			}
		})
	}

	restored := loadRegistry(t)
	restored.Repos = cfg.Repos
	if err := config.Save(restored); err != nil {
		t.Fatal(err)
	}
	for _, command := range commands {
		t.Run("mixed-registry/"+command.name, func(t *testing.T) {
			args := append([]string(nil), command.args...)
			args = append(args, "--no-worktrees", "--json")
			args = append(args, command.command...)
			out := runSoko(t, args...)
			var rows []struct {
				Name string `json:"name"`
				Repo string `json:"repo"`
			}
			if err := json.Unmarshal([]byte(out), &rows); err != nil {
				t.Fatalf("parsing JSON: %v\noutput: %s", err, out)
			}
			if len(rows) != 1 || (rows[0].Name != "api" && rows[0].Repo != "api") {
				t.Errorf("results = %+v, want only the parent repo", rows)
			}
		})
	}
}
