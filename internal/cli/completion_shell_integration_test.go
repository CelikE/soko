//go:build !windows

package cli_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Send real TAB input through the loaded shell completion scripts. Recording
// the executed argv prevents cd's prefix matching from masking broken completion.
func TestTerminalAuditShellCompletion(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "fish"} {
		t.Run(shell, func(t *testing.T) {
			path, err := exec.LookPath(shell)
			if err != nil {
				t.Skipf("%s is not installed", shell)
			}
			bashCompletion := ""
			if shell == "bash" {
				for _, candidate := range []string{os.Getenv("SOKO_BASH_COMPLETION"), "/usr/share/bash-completion/bash_completion", "/etc/bash_completion", "/opt/homebrew/etc/bash_completion", "/usr/local/etc/bash_completion", "/opt/homebrew/etc/profile.d/bash_completion.sh", "/usr/local/etc/profile.d/bash_completion.sh"} {
					if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
						bashCompletion = candidate
						break
					}
				}
				if bashCompletion == "" {
					t.Skip("bash-completion is not installed; set SOKO_BASH_COMPLETION to its script")
				}
			}
			a := newTerminalAudit(t)
			insecure := filepath.Join(a.root, "insecure-completions")
			if err := os.Mkdir(insecure, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(insecure, 0o777); err != nil {
				t.Fatal(err)
			}
			repo := filepath.Join(a.root, "audit-target")
			a.git(a.root, "init", "-b", "main", repo)
			a.run(repo, a.bin, "init")
			completion := a.run(a.root, a.bin, "completion", shell)
			completionFile := filepath.Join(a.root, "completion")
			if err := os.WriteFile(completionFile, []byte(completion), 0o600); err != nil {
				t.Fatal(err)
			}
			argsFile := filepath.Join(a.root, "executed-args")
			a.env = append(a.env, "PATH="+a.root+string(os.PathListSeparator)+os.Getenv("PATH"), "AUDIT_BINARY="+a.bin, "AUDIT_ARGS="+argsFile, "AUDIT_COMPLETION="+completionFile, "AUDIT_INSECURE_FPATH="+insecure, "SOKO_NAV_SESSION=completion-audit", "TERM=dumb", "BASH_COMPLETION="+bashCompletion)
			var args []string
			setup := `source "$AUDIT_COMPLETION"; soko() { printf '%s\n' "$@" > "$AUDIT_ARGS"; command "$AUDIT_BINARY" "$@"; }; printf 'AUDIT_READY\n'`
			switch shell {
			case "bash":
				args = []string{"--noprofile", "--norc", "-i"}
				setup = `source "$BASH_COMPLETION"; ` + setup
			case "zsh":
				args = []string{"-f", "-i"}
				// Ignore insecure completion directories, rather than prompting
				// or disabling compinit's permission checks with -u.
				setup = `fpath=("$AUDIT_INSECURE_FPATH" $fpath); autoload -Uz compinit; compinit -i -D; ` + setup
			case "fish":
				args = []string{"--no-config", "-i"}
				setup = `source "$AUDIT_COMPLETION"; function soko; printf '%s\n' $argv > "$AUDIT_ARGS"; command "$AUDIT_BINARY" $argv; end; printf 'AUDIT_READY\n'`
			}
			s := a.startCommand(path, args...)
			s.wait(s.send(setup+"\r"), "AUDIT_READY\r\n")
			for _, tc := range []struct{ prefix, want string }{
				{"audit-tar", "audit-target"},
				{"__missing__", "__missing__"},
			} {
				s.wait(s.send("soko cd "+tc.prefix+"\t; printf 'AUDIT_DONE\\n'\r"), "AUDIT_DONE\r\n")
				data, err := os.ReadFile(argsFile)
				if err != nil || string(data) != "cd\n"+tc.want+"\n" {
					t.Fatalf("%s TAB executed argv = %q, %v; want cd %q", shell, data, err, tc.want)
				}
			}
			s.send("exit\r")
		})
	}
}
