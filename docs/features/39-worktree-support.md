# Feature: Git Worktree Support

## Summary

First-class support for git worktrees. Users who use worktrees as their
primary workflow can register, navigate, and monitor worktrees alongside
regular repos. Casual users who don't use worktrees see no change in behavior.

## Problem

Power users — especially those with tmux-sessionizer setups — use git worktrees
for nearly everything. Instead of switching branches, they create a new worktree
per branch and open each in a separate terminal/tmux session:

```
~/projects/api/main/           ← main branch
~/projects/api/feat-oauth/     ← feature worktree
~/projects/api/hotfix-123/     ← hotfix worktree
```

Currently soko either skips these (scan) or silently resolves them to the main
repo (init). This means:

- `soko go` can't jump to a worktree
- `soko cd api/feat-oauth` doesn't work
- `soko status` doesn't show worktree-specific state
- `soko scan` ignores half their workspace

For these users, each worktree **is** a workspace — soko should treat it as one.

## Design Principles

1. **Opt-in** — worktree support is behind `--worktrees` flags. Default behavior
   unchanged for users who don't use worktrees.
2. **Linked, not duplicated** — worktrees know their parent repo. Commands can
   group or flatten as needed.
3. **Navigation first** — the primary value is `soko go` / `soko cd` landing in
   the right worktree directory. Everything else is secondary.
4. **No new subcommands** — extend existing commands with flags. `git worktree`
   already handles creation/deletion; soko just needs awareness.

## Config Schema Change

Add an optional `worktree_of` field to `RepoEntry`:

```yaml
repos:
  - name: api
    path: /home/dev/projects/api/main
    tags:
      - backend

  - name: api/feat-oauth
    path: /home/dev/projects/api/feat-oauth
    worktree_of: api
    tags:
      - backend

  - name: api/hotfix-123
    path: /home/dev/projects/api/hotfix-123
    worktree_of: api
    tags:
      - backend
```

Naming convention: `<parent-repo>/<branch-name>`. The branch name is extracted
from `git rev-parse --abbrev-ref HEAD` in the worktree directory.

Worktrees inherit tags from the parent repo at registration time but can be
modified independently afterward.

## Commands

### soko init (from a worktree)

Current behavior: resolves to main repo, registers that.

New behavior:

```
$ cd ~/projects/api/feat-oauth
$ soko init
  worktree detected — registering main repo at ~/projects/api/main
  ✓ registered api (~/projects/api/main)
```

```
$ cd ~/projects/api/feat-oauth
$ soko init --worktree
  ✓ registered api/feat-oauth (~/projects/api/feat-oauth)
    linked to api
```

If the parent repo isn't registered yet, register it first automatically.

### soko scan

Current behavior: skips worktree directories.

New behavior with flag:

```
$ soko scan ~/projects --worktrees
  scanning ~/projects (depth: 5, worktrees: on)

  NAME               SOKO     PATH
  ──────────────────────────────────────────────────────────────
  api                ✓       ~/projects/api/main
  api/feat-oauth     +       ~/projects/api/feat-oauth
  api/hotfix-123     +       ~/projects/api/hotfix-123
  frontend           ✓       ~/projects/frontend

  found 4 repos · 2 worktrees linked · 1 already in soko
```

Without `--worktrees`, behavior is unchanged (worktrees skipped).

**Discovery mechanism**: After finding a repo, run `git worktree list` to
discover linked worktrees. Register each with `worktree_of` set to the parent.

### soko status

Default: worktrees shown inline like any other repo.

```
$ soko status
  REPO               BRANCH         STATUS       ↑↓      LAST COMMIT
  ──────────────────────────────────────────────────────────────────────
  api                main           ✓ clean      ↑0      2h ago  fix: rate limiter
  api/feat-oauth     feat/oauth     ✎ 2M         ↑1      30m ago feat: add OAuth
  api/hotfix-123     hotfix/123     ✓ clean      ↑1      1h ago  fix: crash
  frontend           dev            ✎ 1M         ↑0      4h ago  refactor: nav
```

With `--group` flag, worktrees nest under their parent:

```
$ soko status --group
  api
  ├── main           ✓ clean      ↑0      2h ago  fix: rate limiter
  ├── feat/oauth     ✎ 2M         ↑1      30m ago feat: add OAuth
  └── hotfix/123     ✓ clean      ↑1      1h ago  fix: crash

  frontend
  └── dev            ✎ 1M         ↑0      4h ago  refactor: nav
```

### soko list

Default: worktrees shown inline with a dim `→ parent` indicator.

```
$ soko list
  NAME               TAGS        PATH
  ─────────────────────────────────────────────────
  api                backend     ~/projects/api/main
  api/feat-oauth     backend     ~/projects/api/feat-oauth  → api
  api/hotfix-123     backend     ~/projects/api/hotfix-123  → api
  frontend           frontend    ~/projects/frontend
```

With `--group`:

```
$ soko list --group
  backend
  ├── api
  │   ├── feat-oauth     ~/projects/api/feat-oauth
  │   └── hotfix-123     ~/projects/api/hotfix-123
  └── frontend           ~/projects/frontend
```

### soko go / soko cd

Worktrees are searchable and navigable like any other entry.

```
$ soko go
  api                ~/projects/api/main
  api/feat-oauth     ~/projects/api/feat-oauth
  api/hotfix-123     ~/projects/api/hotfix-123
  frontend           ~/projects/frontend
```

Typing "oauth" in the picker filters to `api/feat-oauth`.

```
$ soko cd api/feat
  ✓ → api/feat-oauth
```

Prefix matching works on the full `parent/branch` name.

### soko remove

Removing a parent repo with registered worktrees prompts:

```
$ soko remove api
  api has 2 linked worktrees: api/feat-oauth, api/hotfix-123
  remove all? (y/n)
```

With `--force`, removes parent and all linked worktrees.

Removing a single worktree only removes that entry:

```
$ soko remove api/feat-oauth
  ✓ removed api/feat-oauth
```

### soko exec / soko fetch / soko stash

By default these commands run on all registered entries, including worktrees.
No special handling needed — each worktree has its own path and git state.

To target only parent repos (skip worktrees):

```
$ soko fetch --no-worktrees
```

To target only worktrees of a specific repo:

```
$ soko status --tag backend    # includes worktrees tagged "backend"
```

## Implementation

### Config changes

```go
type RepoEntry struct {
    Name       string   `yaml:"name"`
    Path       string   `yaml:"path"`
    Tags       []string `yaml:"tags,omitempty"`
    WorktreeOf string   `yaml:"worktree_of,omitempty"`
}
```

### New config helpers

```go
// IsWorktreeEntry returns true if this entry is a linked worktree.
func (r *RepoEntry) IsWorktreeEntry() bool {
    return r.WorktreeOf != ""
}

// FindWorktrees returns all entries that are worktrees of the named repo.
func FindWorktrees(cfg *Config, parentName string) []RepoEntry

// FindParent returns the parent entry for a worktree, or ErrRepoNotFound.
func FindParent(cfg *Config, worktreeOf string) (*RepoEntry, error)
```

### New git helpers

```go
// WorktreeList returns all worktrees for the repo at dir.
// Each entry contains the path, HEAD commit, and branch name.
func WorktreeList(ctx context.Context, dir string) ([]WorktreeInfo, error)

type WorktreeInfo struct {
    Path   string
    Branch string
    Bare   bool
}
```

### Files to modify

| File | Change |
|------|--------|
| `internal/config/config.go` | Add `WorktreeOf` field, helper functions |
| `internal/git/git.go` | Add `WorktreeList()` helper |
| `internal/cli/init.go` | Add `--worktree` flag, register with link |
| `internal/cli/scan.go` | Add `--worktrees` flag, discover via `git worktree list` |
| `internal/cli/status.go` | Group worktrees under parent with `--group` |
| `internal/cli/list.go` | Show `→ parent` indicator, group in tree mode |
| `internal/cli/remove.go` | Cascade prompt for parent with worktrees |
| `internal/cli/exec.go` | Add `--no-worktrees` flag |
| `internal/cli/fetch.go` | Add `--no-worktrees` flag |

### Files unchanged

- `internal/cli/go.go` — picker already shows all registered entries
- `internal/cli/cd.go` — prefix match already works on `parent/branch` names
- `internal/cli/open.go` — worktrees share remotes, opens same URL (correct)
- `internal/cli/stash.go` — operates on path, works as-is

## Backward Compatibility

- Default behavior of `soko init` and `soko scan` is unchanged
- Existing configs without `worktree_of` continue to work (field is optional)
- `--worktrees` / `--worktree` flags are opt-in
- `--no-worktrees` filter is additive

## Edge Cases

| Case | Behavior |
|------|----------|
| Worktree on detached HEAD | Name becomes `parent/<short-hash>` |
| Parent repo not registered | Auto-register parent first, then worktree |
| Worktree directory deleted | `soko doc --fix` prunes stale entries |
| Bare repo with worktrees | `soko scan --worktrees` discovers worktrees; bare repo itself registered with path to `.git` parent |
| Worktree registered without parent | Allowed — `worktree_of` points to a name that may not exist. Commands degrade gracefully (no grouping, shown as standalone). |
| `soko scan` without `--worktrees` after worktrees were previously registered | Existing worktree entries in config are preserved. Scan only controls *discovery*, not removal. |
| Two repos with same branch name in worktrees | Full name includes parent: `api/feat-oauth` vs `frontend/feat-oauth` — no collision |

## JSON Output

All commands with `--json` include the `worktree_of` field:

```json
[
  {
    "name": "api",
    "path": "/home/dev/projects/api/main",
    "branch": "main",
    "dirty": false,
    "worktree_of": ""
  },
  {
    "name": "api/feat-oauth",
    "path": "/home/dev/projects/api/feat-oauth",
    "branch": "feat/oauth",
    "dirty": true,
    "worktree_of": "api"
  }
]
```

## User-Facing Documentation

### Quick start for worktree users

```bash
# Scan and discover worktrees
soko scan ~/projects --worktrees --tag work

# Or register individually
cd ~/projects/api/feat-oauth
soko init --worktree --tag backend

# See everything
soko status --group

# Jump to a worktree
soko cd api/feat-oauth
soko go    # pick interactively
```

### tmux-sessionizer integration

```bash
# In your sessionizer script, use soko to pick the target:
TARGET=$(soko list --json | jq -r '.[].path' | fzf)
tmux new-session -d -s "$(basename $TARGET)" -c "$TARGET"
```

Or use soko's built-in picker directly:

```bash
soko go    # changes directory via shell hook
```
