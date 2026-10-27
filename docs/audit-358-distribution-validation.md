# Audit 358: service and distribution validation

Evidence for [issue #358](https://github.com/CelikE/soko/issues/358); unexecuted scenarios below are not passes.

## Environment

- 2026-10-27, macOS Darwin 25.6.0 arm64, Go 1.27.3, GitHub CLI 2.103.3; source checkout at `6a4c90e`.
- Public downloads came from github.com. GitHub CLI queries used existing credentials only for read-only repository, release and manifest inspection; no dedicated test account or repository was provided. No credentials are recorded here.
- Installation destinations were fresh temporary directories. No release was created, workflow triggered, or external repository modified.
- A disposable Alpine 3.22.5 arm64 Podman container ran on a Linux VM; its filesystem and installation paths were temporary. This is Linux container evidence, not native Linux hardware or package-manager evidence.
- Follow-up source is `e65aebc` plus this change. The user explicitly authorized read-only public GitHub fixtures and manual CI/platform/distribution workflow runs. No external fixture PRs or test releases are created.

## Observations

| Scenario | Result |
| --- | --- |
| Published installer, latest release | PASS on macOS arm64: downloaded `master/install.sh` from raw.githubusercontent.com, ran it with `--dir` and `MAN_DIR` pointing to temporary directories. It resolved v0.22.0, downloaded the public release archive and checksum file, and reported checksum verified. The installed binary reported `soko 0.22.0`; binary mode was 755 and manpage mode 644. |
| Checkout installer, explicit version | PASS on macOS arm64: `sh ./install.sh --dir <temporary-bin> --version v0.22.0` with temporary `MAN_DIR` produced the same version, checksum result, binary and manpage permissions. |
| Published installer, Linux container | PASS in Alpine 3.22.5 on Linux aarch64: downloaded `master/install.sh`, installed v0.22.0 with temporary `--dir` and `MAN_DIR`, and verified checksum, `soko 0.22.0`, binary mode 755 and manpage mode 644. The container was removed after the test. |
| Published manifests | Read-only inspection of the v0.22.0 Homebrew cask and Scoop manifest found their listed archive hashes match the corresponding public release checksum entries. This is not a package-manager installation test. |
| Release ordering | Read-only inspection of `.github/workflows/release.yml`: the `merge-upstream` step precedes GoReleaser in the same job, with no `continue-on-error`. `.github/workflows/winget.yml` runs on a published release. This does not verify credentials, upstream synchronization, workflow execution or publication. |
| Adapter regression | `go test ./internal/cli -run 'Test.*(PR|Pr|Install)' -count=1` passed. These tests are not live PR-service or native package-manager evidence. |
| Live PR service | `.github/scripts/audit_pr_service.py` passed seven live checks with GitHub CLI 2.103.3 on github.com, using `golang/go` (at least 101 open PRs) and `CelikE/homebrew-tap` (empty). Existing credentials were reused with authorization, but HOME/config/cache/GH_CONFIG_DIR were disposable and the token was passed only in the child environment. These are public read-only snapshots, not dedicated test credentials or repositories. |
| Live PR output/limits | Exact normalized output from actual `gh pr list` matched `soko pr` from a nested directory. Limits 1, default 50 and 101 returned the expected counts; the 101-item case crossed gh's 100-item page boundary. Cap hints appeared only on stderr, quiet preserved JSON and suppressed hints, and empty JSON/text/quiet cases behaved as documented. |
| Live errors and URL | A deliberately invalid token produced HTTP 401 through real gh, nonzero exit and empty stdout under quiet/JSON. A reserved non-listening loopback proxy caused a real gh connection failure with the same output contract; it was not a GitHub outage. A returned public PR URL matched its repository/number and returned HTTP 200; no browser was opened. |
| Native distributions on a8d6dd3 | The distribution jobs in [run 40562201597](https://github.com/CelikE/soko/actions/runs/40562201597) passed: published archives and `go install @latest` on Linux/macOS/Windows; both Unix installers on Linux/macOS; native Windows winget and Scoop; published macOS Homebrew cask and locally generated corrected cask with manual; deb on the disposable Ubuntu runner and rpm in a disposable Fedora container. The overall workflow failed unrelated platform tests, so this is a distribution-job result, not an overall green workflow claim. |

The Windows package artifact records Windows Server 2025 (10.0.26100), PowerShell 7.6.7 and WinGet v1.11.510. Both package managers installed and invoked `soko 0.22.0`; Scoop verified the package checksum. The Windows archive artifact records the exact SHA256 and an included manpage. Windows does not install a Unix manpage into a system manual directory.

## Reproducible checks

```sh
python .github/scripts/audit_pr_service.py --output /path/to/live-pr.json
python .github/scripts/audit_distribution.py --source-install --output /path/to/distribution.json
```

Reports record platform, source, release and individual outcomes, not tokens or credential-bearing environments. Public PR fixtures can change; drift or insufficient PRs fails the live check rather than producing a false pass. The native distribution script verifies the existing public archive's SHA256, invokes its binary, checks the archived manual, and tests both checkout and published Unix installers in temporary prefixes. The optional source path executes the documented `go install ...@latest`; that build does not set release ldflags, so module metadata identifies its version even if the CLI reports `dev`.

The manual `Distribution audit` workflow runs archive checks on Linux/macOS/Windows, fresh Homebrew installation on a disposable macOS runner, and winget/Scoop on a disposable Windows runner. It is also called by the manual `Full platform tests` workflow so a new audit workflow can be tested before it exists on the default branch. It never invokes the release or Winget publication workflows.

## Distribution finding

The published v0.22.0 Homebrew cask declares only the binary, not the archived manual. The source release configuration now declares `man/soko.1` for future casks, and a configuration regression test protects that declaration. README documents the old release's manual-install workaround. The audit does not claim that changing source has updated an already-published cask, and does not authorize a release.

Local GoReleaser 2.15.2 snapshot generation (`release --snapshot --clean --skip=publish,announce,before`, with a temporary dist/config path) passed for all six OS/architecture binaries, archives, deb/rpm packages, Scoop manifest and Homebrew cask. The actual generated cask contains `manpage "man/soko.1"`. Nothing was published. The native workflow additionally installs this generated cask from local archives in a sandbox tap, separately from checking the old published cask. Homebrew's documented Linux and macOS paths are both included in the final audit matrix.

## Remaining work

- Complete the corrected native platform rerun and added Linux Homebrew job; retain the already-passing distribution results above. The Alpine container check does not replace native runner or package-manager coverage.
- Validate manifest synchronization in an authorized sandbox if execution evidence is required. Actual release publication requires separate authorization.

Issue #358 was closed on request before its validation was complete. Parent #336 remains open until the remaining evidence is recorded; issue closure alone is not a passing result.
