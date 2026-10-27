package cli_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestIntegration_PowerShellCompletion(t *testing.T) {
	pwsh, err := exec.LookPath("pwsh")
	if err != nil {
		t.Skip("pwsh is not installed")
	}
	base := t.TempDir()
	binary := filepath.Join(base, "soko")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	build := exec.CommandContext(t.Context(), "go", "build", "-o", binary, "../../cmd/soko")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building soko: %v\n%s", err, out)
	}
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(base, "config"))
	t.Setenv("PATH", base+string(os.PathListSeparator)+os.Getenv("PATH"))
	repo := filepath.Join(base, "audit-target")
	initRepo(t, repo)
	runSokoInit(t, repo)
	generate := exec.CommandContext(t.Context(), binary, "completion", "powershell")
	completion, err := generate.Output()
	if err != nil {
		t.Fatal(err)
	}
	completionFile := filepath.Join(base, "completion.ps1")
	if err := os.WriteFile(completionFile, completion, 0o600); err != nil {
		t.Fatal(err)
	}
	// CompleteInput is PowerShell's registered native TAB completion engine,
	// not an invocation of soko's hidden completion protocol in isolation.
	const script = `param($Completion)
$ErrorActionPreference = 'Stop'
. $Completion
$line = 'soko cd audit-tar'
$result = [System.Management.Automation.CommandCompletion]::CompleteInput($line, $line.Length, $null)
if ($result.CompletionMatches.Count -ne 1 -or $result.CompletionMatches[0].CompletionText.TrimEnd() -cne 'audit-target') {
    throw "wrong completion: $($result.CompletionMatches | Out-String)"
}
$line = 'soko cd __missing__'
$result = [System.Management.Automation.CommandCompletion]::CompleteInput($line, $line.Length, $null)
if ($result.CompletionMatches.Count -ne 1 -or $result.CompletionMatches[0].CompletionText -cne '__missing__') {
    throw 'invalid name changed or fell back to filename completion'
}
[Console]::Write('PASS')
`
	scriptFile := filepath.Join(base, "test-completion.ps1")
	if err := os.WriteFile(scriptFile, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), pwsh, "-NoLogo", "-NoProfile", "-NonInteractive", "-File", scriptFile, completionFile)
	cmd.Dir = base
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if out, err := cmd.Output(); err != nil || strings.TrimSpace(string(out)) != "PASS" {
		t.Fatalf("PowerShell completion: %v\n%s\n%s", err, out, stderr.String())
	}
}
