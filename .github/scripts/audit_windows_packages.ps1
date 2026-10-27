# Run only on a disposable native Windows runner. No releases/manifests published.
$ErrorActionPreference = 'Stop'
$PSNativeCommandUseErrorActionPreference = $true
$root = Join-Path $env:RUNNER_TEMP 'soko-package-audit'
New-Item -ItemType Directory -Force $root | Out-Null
$evidence = [System.Collections.Generic.List[object]]::new()

try {
    # GitHub-hosted Windows images do not always expose WinGet to this account.
    if (!(Get-Command winget -ErrorAction SilentlyContinue)) {
        Install-Module Microsoft.WinGet.Client -Force -Repository PSGallery -Scope CurrentUser
        Repair-WinGetPackageManager -Force -Latest
    }
    $winget = (Get-Command winget).Source
    & $winget --version
    & $winget install --id CelikE.soko --exact --source winget --version 0.22.0 --scope user --location (Join-Path $root 'winget') --accept-source-agreements --accept-package-agreements --disable-interactivity
    $wingetBinary = Get-ChildItem (Join-Path $root 'winget') -Filter soko.exe -Recurse | Select-Object -First 1
    if (!$wingetBinary) { throw 'WinGet did not install soko.exe at the requested location' }
    $version = & $wingetBinary.FullName version
    if ($version -notmatch '^soko v?0\.22\.0(?:\s|$)') { throw "wrong WinGet version: $version" }
    $evidence.Add(@{name='winget'; result='PASS'; version=$version; manager=(& $winget --version)})

    # Bootstrap Scoop into a temporary prefix rather than the normal user prefix.
    $env:SCOOP = Join-Path $root 'scoop'
    $bootstrap = Join-Path $root 'install-scoop.ps1'
    Invoke-WebRequest https://raw.githubusercontent.com/ScoopInstaller/Install/master/install.ps1 -OutFile $bootstrap
    & $bootstrap -RunAsAdmin
    $scoop = Join-Path $env:SCOOP 'shims/scoop.ps1'
    & $scoop bucket add soko https://github.com/CelikE/homebrew-tap
    & $scoop install soko
    $scoopBinary = Join-Path $env:SCOOP 'apps/soko/current/soko.exe'
    $version = & $scoopBinary version
    if ($version -notmatch '^soko v?0\.22\.0(?:\s|$)') { throw "wrong Scoop version: $version" }
    $evidence.Add(@{name='scoop'; result='PASS'; version=$version; checksum='verified by Scoop'})
} catch {
    $evidence.Add(@{name='execution'; result='FAIL'; error=$_.Exception.Message})
    throw
} finally {
    @{source=(git rev-parse HEAD); os=[Environment]::OSVersion.VersionString; powershell=$PSVersionTable.PSVersion.ToString(); checks=$evidence} |
        ConvertTo-Json -Depth 5 | Set-Content -Encoding utf8 (Join-Path $env:RUNNER_TEMP 'audit-windows-packages.json')
}
