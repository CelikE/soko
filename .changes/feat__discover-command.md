---
bump: minor
---

Automatic repo discovery with `soko discover` — opt in with `soko discover on` and repos register themselves the first time you cd into them, no `soko scan` needed. Driven by the shell hook (fires on directory change); scope it with `--root`, apply `--tag`s, and skip paths with `--ignore`. Skips submodules, the home directory, `node_modules`/`vendor`, and non-interactive shells. `soko doc` reports discovery status
