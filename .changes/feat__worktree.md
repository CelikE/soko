---
bump: minor
---

Add `soko worktree` to manage git worktrees across repos: `worktree add <repo> <branch>` creates and registers in one step (with `-b` to create the branch, `--path`, `--tag`), `worktree list` shows all worktrees with live branch and dirty status, and `worktree rm` removes the directory and registry entry together (refusing dirty trees without `--force`).
