# Continuous integration

## Pull requests and master

The `CI` workflow runs:

- Lint, release configuration validation, and CI runner unit tests.
- Normal Go tests on Linux, macOS, and Windows. Each platform has three independent CLI test shards. Shard 1 also runs every other package and, on Windows, the PowerShell worktree/navigation smoke check.
- The full race-enabled suite with coverage on Linux.

CLI tests share environment variables, working directories, and output state inside a process. Shards run in separate processes on separate runners; tests remain sequential within each shard. Top-level tests are sorted and distributed round-robin, with all subtests kept together. Runner tests check that the partition is complete and disjoint. Every package runs on every platform.

The `Cross-platform race` workflow runs three race-enabled CLI shards on both macOS and Windows. It runs weekly on Monday at 04:23 UTC, through **Run workflow**, and on pull requests touching concurrency-sensitive code. Its `paths` list includes the execution, Git, registry, journal, snapshot, picker, dashboard, and worktree packages, relevant CLI operations, Go dependencies, and CI runner/workflow changes. Update that list when introducing concurrency in another area. It has no path-filtered checks that should be marked as universally required in branch protection.

Both workflows cancel superseded runs for the same ref. Go test packages have an explicit 15-minute timeout and jobs a 20-minute bound; sharding reduces runtime, while timeout headroom prevents ordinary runner variation from causing a retry.

## Diagnostics and local reproduction

Each test job uploads raw `go test -json` events as a timing artifact, including on failure, with 14-day retention. Logs show the ten slowest top-level tests. The Linux race artifact also contains `coverage.out`. Use these timings to rebalance shards or reduce expensive fixtures based on measured cost.

From the repository root:

```sh
python3 -m unittest discover -s .github/scripts -p 'test_*.py'
python3 .github/scripts/ci_test.py --shard 1 --shards 3 --timings /tmp/shard-1.jsonl
python3 .github/scripts/ci_test.py --shard 2 --shards 3 --timings /tmp/shard-2.jsonl
python3 .github/scripts/ci_test.py --shard 3 --shards 3 --timings /tmp/shard-3.jsonl
python3 .github/scripts/ci_test.py --race --coverprofile /tmp/coverage.out --timings /tmp/race.jsonl
```

Add `--race` to a sharded command to reproduce a macOS/Windows race job. A Go test failure remains a nonzero runner exit, and failure output is printed while the JSON event log is preserved. Runs use `-count=1` so timing artifacts describe actual execution rather than replayed cached test results. Go's compiled-package cache remains enabled.
