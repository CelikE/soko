---
bump: minor
---

Prune deleted repos with `soko prune` — remove registry entries whose directories no longer exist on disk (cascading to their linked worktrees), with `--dry-run`/`--force`/`--json` and `--tag` filtering. `status` and `list` now warn when registered repos have gone missing
