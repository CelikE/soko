package cli_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// The same test runs on Windows and Unix. Windows does not support trailing
// spaces or newlines in ordinary directory names, so those cases are Unix-only.
func TestIntegration_PowerShellExactNavigation(t *testing.T) {
	pwsh, err := exec.LookPath("pwsh")
	if err != nil {
		t.Skip("pwsh is not installed")
	}
	binary := filepath.Join(t.TempDir(), "soko")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	build := exec.CommandContext(t.Context(), "go", "build", "-o", binary, "../../cmd/soko")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building soko: %v\n%s", err, out)
	}
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	names := []string{"ordinary space", " leading space", "literal[brackets]", "blå"}
	if runtime.GOOS != "windows" {
		names = append(names, "trailing space ", "trailing newline\n")
	}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			repo := filepath.Join(base, name)
			initRepo(t, repo)
			configDir := filepath.Join(t.TempDir(), "config with spaces")
			if name == "literal[brackets]" {
				configDir = filepath.Join(t.TempDir(), "config[with] spaces")
			}
			t.Setenv("XDG_CONFIG_HOME", configDir)
			t.Setenv("SOKO_NAV_SESSION", "")
			runSokoInit(t, repo)
			otherSession := filepath.Join(configDir, "soko", ".nav.999999999")
			if err := os.WriteFile(otherSession, []byte(base), 0o600); err != nil {
				t.Fatal(err)
			}
			for _, navigation := range []string{"cd", "go", "retry"} {
				t.Run(navigation, func(t *testing.T) {
					const script = `param($Binary, $Target, $Name, $Navigation)
$ErrorActionPreference = 'Stop'
& $Binary shell-init --pwsh | Out-String | Invoke-Expression
if ($LASTEXITCODE -ne 0) { throw 'shell-init failed' }
$navFile = Join-Path (Join-Path $env:XDG_CONFIG_HOME 'soko') ".nav.$env:SOKO_NAV_SESSION"
if ($Navigation -eq 'retry') {
    [IO.File]::WriteAllText($navFile, (Join-Path $Target 'missing'))
    __soko_nav_hook
    if (!(Test-Path -LiteralPath $navFile)) { throw 'invalid request was consumed' }
    [IO.File]::WriteAllText($navFile, $Target)
} elseif ($Navigation -eq 'go') {
    & $Binary go
    if ($LASTEXITCODE -ne 0) { throw 'go failed' }
} else {
    & $Binary cd $Name
    if ($LASTEXITCODE -ne 0) { throw 'cd failed' }
}

__soko_nav_hook
if (Test-Path -LiteralPath $navFile) { throw 'request was not consumed' }
[Console]::Write($PWD.ProviderPath)
`
					path := filepath.Join(t.TempDir(), "navigate.ps1")
					if err := os.WriteFile(path, []byte(script), 0o600); err != nil {
						t.Fatal(err)
					}
					cmd := exec.CommandContext(t.Context(), pwsh, "-NoLogo", "-NoProfile", "-NonInteractive", "-File", path, binary, repo, name, navigation)
					cmd.Dir = base
					var stdout, stderr bytes.Buffer
					cmd.Stdout, cmd.Stderr = &stdout, &stderr
					if err := cmd.Run(); err != nil {
						t.Fatalf("PowerShell navigation: %v\nstdout: %q\nstderr: %s", err, stdout.String(), stderr.String())
					}
					// Never trim this oracle: whitespace is part of the directory name.
					if got := stdout.String(); got != repo {
						t.Errorf("working directory = %q, want %q", got, repo)
					}
					if data, err := os.ReadFile(otherSession); err != nil || string(data) != base {
						t.Errorf("other session request changed: %q, %v", data, err)
					}
				})
			}
		})
	}
}
