---
bump: minor
---

Worktree entries now inherit their parent repo's tags at filter time, so `--tag` filters (status, sync, fetch, list, etc.) include worktrees of tagged repos. Retagging the parent instantly re-scopes its worktrees; `tag remove` on a worktree only removes own tags and errors on inherited ones.
