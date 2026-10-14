$ErrorActionPreference = "Stop"
$PSNativeCommandUseErrorActionPreference = $true

$binary = Join-Path $env:RUNNER_TEMP "soko.exe"
go build -o $binary ./cmd/soko

$workspace = Join-Path $env:RUNNER_TEMP "soko-worktree-smoke"
$root = Join-Path $workspace "api"
$feature = Join-Path $workspace "oauth"
$review = Join-Path $workspace "review"
$env:XDG_CONFIG_HOME = Join-Path $workspace "config"
$env:SOKO_NAV_SESSION = ""

git init -b main $root
git -C $root config user.name "Soko CI"
git -C $root config user.email "soko-ci@example.com"
Set-Content (Join-Path $root "README.md") "Windows worktree smoke test"
git -C $root add README.md
git -C $root commit -m "Initial fixture"
git -C $root worktree add -b feat/oauth $feature
git -C $root worktree add --detach $review HEAD

& $binary scan $feature --worktrees --dry-run --json
if (Test-Path (Join-Path $env:XDG_CONFIG_HOME "soko/config.yaml")) {
    throw "Dry-run wrote the registry"
}

Push-Location $root
try {
    & $binary init --tag backend --json
    $adopted = @(& $binary worktree adopt api --json | ConvertFrom-Json)
    if ($adopted.Count -ne 2) {
        throw "Expected two adopted worktrees"
    }
    $repos = @(& $binary list --repo api --json | ConvertFrom-Json)
    if ($repos.Count -ne 3 -or "api/feat/oauth" -notin $repos.name -or "api/review" -notin $repos.name) {
        throw "Worktree aliases or repository-family membership are incorrect"
    }
    $repeated = @(& $binary worktree adopt api --json | ConvertFrom-Json)
    if ($repeated.Count -ne 0) {
        throw "Repeated adoption was not idempotent"
    }
    & $binary list --group-by repo
    & $binary status --repo api --group-by repo

    Invoke-Expression (& $binary shell-init --pwsh | Out-String)
    & $binary cd api/feat/oauth
    __soko_nav_hook
    if ($PWD.Path -ne $feature) {
        throw "PowerShell hook did not navigate to the feature worktree"
    }
    Set-Content (Join-Path $feature "unfinished.txt") "Keep this work"
    $family = @(& $binary list --repo . --json | ConvertFrom-Json)
    if ($family.Count -ne 3) {
        throw "Current-worktree family resolution failed"
    }
    & $binary cd api/review
    __soko_nav_hook
    if ($PWD.Path -ne $review -or !(Test-Path (Join-Path $feature "unfinished.txt"))) {
        throw "Worktree navigation failed or modified unfinished work"
    }
} finally {
    Pop-Location
}

# JSON scanning must also persist registrations on Windows.
$env:XDG_CONFIG_HOME = Join-Path $workspace "scan-config"
$scanned = @(& $binary scan $feature --worktrees --json | ConvertFrom-Json)
$registered = @(& $binary list --json | ConvertFrom-Json)
if ($scanned.Count -ne 3 -or $registered.Count -ne 3) {
    throw "Linked-root JSON scanning did not persist all checkouts"
}
