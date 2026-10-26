# Continuous integration

## Pull requests and master

The `CI` workflow runs three jobs on every pull request and push to master:

- Lint, release configuration validation, and CI runner unit tests.
- A native Windows build and PowerShell worktree/navigation smoke check, with a five-minute job timeout.
- The full race-enabled suite with coverage on Linux.

Every package runs on Linux for every change. Linux's race-enabled run also provides full-suite coverage, avoiding a duplicate normal test run. The Windows smoke check exercises native executable startup, JSON output and registry persistence, linked and detached worktrees, adoption, repository-family selection, and PowerShell navigation. Full macOS and Windows suites run outside the pull request path so their runtime does not delay routine merges.

## Scheduled and manual platform validation

The `Full platform tests` workflow runs the full normal Go suite on macOS and Windows nightly at 03:23 UTC and through **Run workflow**. Select a feature branch when manually validating a platform-sensitive change before merging. Platform-specific failures outside the smoke coverage can therefore be detected after merge rather than on every pull request.

CLI tests share environment variables, working directories, and output state inside a process, so they remain sequential. Each platform uses one unsharded job to limit hosted-runner demand. The previous Windows job spent roughly nine minutes in the CLI package alone; Go caching does not eliminate execution time for tests that create real Git repositories and subprocesses.

The `Cross-platform race` workflow runs one full race-enabled suite on each of macOS and Windows. It runs weekly on Monday at 04:23 UTC and through **Run workflow**, not automatically on pull requests. Run it manually against a feature branch when a change needs additional platform-specific race validation.

All workflows cancel superseded runs for the same ref. Go test packages have an explicit 15-minute timeout and full-suite jobs a 20-minute bound.

If required status checks are configured, use `check`, `Race and coverage (Linux)`, and `Windows smoke`. Replace old full-suite or shard-specific requirements when adopting these names. Scheduled/manual platform and race jobs should not be required for pull request merges.

## Diagnostics and local reproduction

Each full-suite job uploads raw `go test -json` events as a timing artifact, including on failure, with 14-day retention. Logs show the ten slowest top-level tests. The Linux race artifact also contains `coverage.out`. Use timing artifacts to identify expensive fixtures and distinguish execution time from queue or setup delays.

From the repository root:

```sh
python3 -m unittest discover -s .github/scripts -p 'test_*.py'
python3 .github/scripts/ci_test.py --timings /tmp/test.jsonl
python3 .github/scripts/ci_test.py --race --timings /tmp/race.jsonl
python3 .github/scripts/ci_test.py --race --coverprofile /tmp/coverage.out --timings /tmp/race.jsonl
```

A Go test failure remains a nonzero runner exit, and failure output is printed while the JSON event log is preserved. Runs use `-count=1` so timing artifacts describe actual execution rather than replayed cached test results. Go's compiled-package cache remains enabled. The runner still supports `--shard N --shards M` for targeted diagnostics.
