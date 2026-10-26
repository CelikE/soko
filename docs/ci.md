# Continuous integration

## Pull requests and master

The `CI` workflow runs four jobs on every pull request and push to master:

- Lint, release configuration validation, and CI runner unit tests.
- The full normal Go test suite in one macOS job and one Windows job. The Windows job also runs the PowerShell worktree/navigation smoke check.
- The full race-enabled suite with coverage on Linux.

Every package runs on every platform. Linux's race-enabled run also provides its full-suite coverage, avoiding a duplicate normal test run. CLI tests share environment variables, working directories, and output state inside a process, so they remain sequential. Each platform runs an unsharded suite to reduce hosted-runner demand and repeated setup overhead; only one macOS runner is needed per CI run.

The `Cross-platform race` workflow runs one full race-enabled suite on each of macOS and Windows. It runs weekly on Monday at 04:23 UTC and through **Run workflow**, not automatically on pull requests. Run it manually against a feature branch when a change needs additional platform-specific race validation.

Both workflows cancel superseded runs for the same ref. Go test packages have an explicit 15-minute timeout and test jobs a 20-minute bound.

If required status checks are configured, use `check`, `Race and coverage (Linux)`, `Test (macos-latest)`, and `Test (windows-latest)`. Replace any old shard-specific requirements when adopting these names. Weekly/manual race jobs should not be required for pull request merges.

## Diagnostics and local reproduction

Each test job uploads raw `go test -json` events as a timing artifact, including on failure, with 14-day retention. Logs show the ten slowest top-level tests. The Linux race artifact also contains `coverage.out`. Compare job execution and queue times over several pull requests before considering sharding again; use the timing artifacts to identify expensive fixtures.

From the repository root:

```sh
python3 -m unittest discover -s .github/scripts -p 'test_*.py'
python3 .github/scripts/ci_test.py --timings /tmp/test.jsonl
python3 .github/scripts/ci_test.py --race --timings /tmp/race.jsonl
python3 .github/scripts/ci_test.py --race --coverprofile /tmp/coverage.out --timings /tmp/race.jsonl
```

A Go test failure remains a nonzero runner exit, and failure output is printed while the JSON event log is preserved. Runs use `-count=1` so timing artifacts describe actual execution rather than replayed cached test results. Go's compiled-package cache remains enabled. The runner still supports `--shard N --shards M` for targeted diagnostics.
