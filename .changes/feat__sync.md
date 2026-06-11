---
bump: minor
---

Add `soko sync` — fetch all repos, then fast-forward the ones that are provably safe (clean, upstream, not diverged); dirty, diverged, and fetch-only-behind repos are reported as needing attention. Supports positional repos, `--tag`, `--fetch-only`, `--no-worktrees`, and `--json`.
