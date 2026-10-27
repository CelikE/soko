# soko user guide

soko (倉庫 — "storehouse") is a fast, lightweight CLI for managing multiple git repositories. Register your repos once, then see the status of all of them from anywhere with a single command. No more `cd`-ing between directories and running `git status` one at a time.

New to soko? Start with the [README quick start](../README.md#quick-start).
This guide contains the full command reference, advanced workflows, and
development instructions.

- [Installation](#prerequisites)
- [Manual](#manual)
- [Commands](#commands)
- [Flags](#flags)
- [JSON output and automation](#json-output)
- [Usage examples](#usage-examples)
- [Configuration](#configuration)
- [Building and contributing](#building-from-source)
- [Releasing](#releasing)

## Prerequisites

- **Git** — soko shells out to `git` for all repository operations
- **Go 1.27.2+** — only needed if installing from source or via `go install`
- **macOS 13 Ventura+** — required for macOS binaries and source builds

## Install

```bash
# Quick install (macOS / Linux)
curl -fsSL https://raw.githubusercontent.com/CelikE/soko/master/install.sh | sh

# Homebrew (macOS / Linux)
brew install CelikE/tap/soko

# Windows (winget)
winget install CelikE.soko

# Windows (Scoop)
scoop bucket add soko https://github.com/CelikE/homebrew-tap
scoop install soko

# From source (requires Go 1.27.2+)
go install github.com/CelikE/soko/cmd/soko@latest
```

Or download binaries directly from [GitHub Releases](https://github.com/CelikE/soko/releases).

## Quick start

```bash
# Enable shell integration (add to .bashrc or .zshrc)
eval "$(soko shell-init)"

# Register all repos at once
soko scan ~/projects --tag work

# Or register individually
cd ~/projects/auth-service && soko init --tag backend

# See everything at a glance
soko status
```

```
  REPO               BRANCH       STATUS       ↑↓         LAST COMMIT
  ──────────────────────────────────────────────────────────────────────────────
  auth-service       feat/sso     ✎ 3M         ↑2         2h ago  feat: add OAuth
  backend-api        main         ✓ clean      ↓3         1d ago  fix: rate limiter
  frontend           dev          ✎ 1M 2U      ↑1         4h ago  refactor: nav bar

  3 repos · 2 dirty · 1 behind · 6 changes
```

## Manual

soko ships a full `man` page covering every command, flag, key binding,
environment variable, and config file. Package installs (Homebrew, deb/rpm,
the `install.sh` script) place it automatically, so you can read it like any
other manual:

```bash
man soko
```

The published v0.22.0 Homebrew cask omits the manpage. For that release,
install it from a source checkout with `make man` (below), or use the Unix
installer. The release configuration includes the manpage for future casks;
no new release is implied by this source change.

From a source checkout, install or preview it directly:

```bash
make man                 # install to $MANPREFIX/man1 (default /usr/local/share/man)
man -l docs/soko.1       # preview without installing
```

## Commands

| Command | Description |
|---------|-------------|
| `soko init` | Register the current git repo (detects worktrees) |
| `soko scan` | Discover and register all git repos in a directory |
| `soko discover` | Auto-register repos as you cd into them (opt-in) |
| `soko status [repos...]` | Show status of all (or specific) repos |
| `soko remotes [repos...]` | Show each repo's remotes + upstream tracking, flag misconfig |
| `soko pr` | List open pull requests for the current repo via GitHub CLI (`--limit`, default: 50; `--json` supported) |
| `soko diff [repos...]` | Show uncommitted file changes across repos |
| `soko stash [repos...]` | Stash tracked changes and untracked files across repos (ignored files stay untouched) |
| `soko stash pop [repos...]` | Restore soko-created stashes, filtered by repo name/prefix and optionally `--tag` |
| `soko clean [repos...]` | Delete merged branches across repos |
| `soko undo` | Revert the last destructive soko operation (e.g. clean) |
| `soko list` | List all registered repos |
| `soko remove` | Remove a repo from the registry |
| `soko prune` | Remove repos whose directories no longer exist |
| `soko fetch [repos...]` | Fetch all (or specific) registered repos in parallel |
| `soko pull [repos...]` | Pull all (or specific) registered repos in parallel |
| `soko sync [repos...]` | Fetch all repos, fast-forward the safe ones, report the rest |
| `soko ctx` | Save and restore workspace contexts (branches + stashes) |
| `soko snapshot` | Save and restore exact repo positions (branch + commit) |
| `soko worktree` | Create, adopt, list, and remove git worktrees with registry bookkeeping |
| `soko branch [name]` | Current branch per repo, or where a branch exists; `switch`/`stale` subcommands |
| `soko cd` | Navigate to a repo by name |
| `soko go` | Interactive repo picker |
| `soko ui` | Live full-screen dashboard of local workspace state |
| `soko exec` | Run a command in all registered repos |
| `soko apply <file>` | Copy a file into many repos with a diff preview |
| `soko grep <pattern>` | Search file content across repos with git grep |
| `soko open` | Open a repo in the browser |
| `soko report [repos...]` | Summarize commit activity across repos |
| `soko stats` | Show workspace-level statistics and health metrics |
| `soko health` | Rank repos by an urgency score — most neglected first |
| `soko tag` | Manage repo tags |
| `soko annotate [repo]` | Attach metadata (owner/status/priority/note) to a repo |
| `soko alias` | Manage command aliases |
| `soko doc` | Check the health of your soko setup |
| `soko config` | View, get, set, or list configuration (`--json` supported) |
| `soko shell-init` | Print shell integration hook |
| `soko version` | Print the soko version |

### Argument and flag contracts

Commands without positional operands reject extras (including `version extra`).
`shell-init --fish` and `--pwsh` cannot both be enabled; with neither enabled,
output is Bash/Zsh.
Status predicates `--dirty`, `--clean`, `--ahead`, and `--behind` combine with
**OR** within the selected repos, not AND. Clean means no local changes and no
ahead/behind commits; failed reads never count as clean.

Use `--` before leading-hyphen operands (`soko status -- -api`). Everything after
`exec --` belongs to the child, including flags. Global flags may precede or
follow commands; repeated scalar flags use the last value. `version --json`
remains plain text, as documented below. See [CLI contracts](cli-contracts.md)
for the full argument inventory, flag composition, and compatibility decisions.

## Flags

| Flag | Scope | Description |
|------|-------|-------------|
| `--json` | Global | Output JSON on supported commands; see [JSON output](#json-output) for capabilities and exceptions |
| `--quiet`, `-q` | Global | Suppress hints, progress, and summary lines (also via `SOKO_QUIET`) |
| `--perf` | Global | Report timing for `status`, `fetch`, `pull`, `sync`, and `exec`; wraps JSON in `repos`/`timing` (also via `SOKO_PERF`) |
| `--fetch` | `status` | Fetch from remotes before showing status |
| `--dirty` | `status` | Show only repos with uncommitted changes |
| `--clean` | `status` | Show only clean repos in sync with remote |
| `--ahead` | `status` | Show only repos ahead of remote |
| `--behind` | `status` | Show only repos behind remote |
| `--missing-upstream` | `remotes` | Show only repos with no remote or no upstream |
| `--limit` | `pr` | Maximum pull requests to list (positive number, default: 50); reaching the cap prints a stderr hint, unless `--quiet` |
| `--tag` | `init`, `scan`, `status`, `remotes`, `diff`, `stash`, `list`, `fetch`, `pull`, `sync`, `branch`, `exec`, `grep`, `apply`, `open`, `report`, `stats`, `health`, `clean`, `prune`, `go`, `ui`, `snapshot save`, `discover on` | Filter by tag (repeatable, combines with OR) |
| `--meta` | `list`, `status` | Filter by metadata `key=value` (repeatable, combines with AND) |
| `--root` | `discover on` | Restrict auto-discovery to repos under these directories (repeatable) |
| `--ignore` | `discover on` | Glob patterns of paths to skip during auto-discovery (repeatable) |
| `--worktree` | `init` | Register as a linked worktree instead of resolving to main repo |
| `--worktrees` | `scan` | Also discover and register linked git worktrees |
| `--no-worktrees` | `fetch`, `pull`, `sync`, `exec`, `grep`, `apply` | Skip worktree entries, only operate on parent repos |
| `--to` | `apply` | Destination path relative to each repo root (required) |
| `--write` | `apply` | Write the files (default is a dry-run diff) |
| `--fetch-only` | `sync` | Fetch every repo but never pull |
| `--create`, `-b` | `branch switch` | Create the branch from the default branch where missing |
| `--days` | `branch stale` | Staleness threshold in days (default: 90) |
| `--ignore-case`, `-i` | `grep` | Case-insensitive match |
| `--regexp`, `-e` | `grep` | Treat the pattern as a POSIX extended regex (default: fixed string) |
| `--files-only` | `grep` | List matching file paths only, not lines |
| `--rebase` | `pull` | Rebase local commits onto the upstream instead of fast-forward only |
| `--dry-run` | `scan`, `clean`, `prune` | Preview what would happen without making changes |
| `--depth` | `scan` | Maximum directory depth to scan (default: 5) |
| `--group` | `status`, `list` | Group repos by tag in a tree view |
| `--group-by` | `status`, `list`, `ui` | Group by `none`, `tag`, or `repo` (base repo and its worktrees) |
| `--repo`, `-r` | `status`, `list`, `go`, `ui`, `worktree list`, `worktree adopt` | Limit to a base repo and its worktrees; `.` selects the current family |
| `--tag-with` | `worktree adopt` | Tags to apply to newly adopted worktrees (repeatable) |
| `--all` | `status` | Show all repos without truncation |
| `--prune` | `fetch`, `clean` | Prune stale remote tracking refs |
| `--force` | `remove`, `clean`, `prune`, `apply --write` | Skip confirmation prompt |
| `--force` | `snapshot save` | Overwrite an existing snapshot |
| `--fetch` | `ui` | Fetch from remotes in the background every interval (e.g. `--fetch 5m`) |
| `--select` | `clean`, `prune`, `remove --all` | Open the interactive picker to choose exactly which repos the operation touches (requires a TTY) |
| `--set` | `annotate` | Set a metadata `key=value` (repeatable) |
| `--unset` | `annotate` | Remove a metadata key (repeatable) |
| `--clear` | `annotate` | Remove all metadata from a repo |
| `--list` | `annotate` | List every repo that has metadata |
| `--list` | `undo` | Show the undo journal without reverting anything |
| `-r`, `--repo` | `annotate`, `tag add`, `tag remove` | Target repo by name (defaults to the current directory) |
| `--seq` | `exec` | Run sequentially instead of in parallel |
| `--prs` | `open` | Open pull/merge requests page |
| `--issues` | `open` | Open issues page |
| `--actions` | `open` | Open CI/CD page |
| `--branches` | `open` | Open branches page |
| `--settings` | `open` | Open settings page |
| `--days` | `report` | Number of days to look back (default: 7) |
| `--author` | `report` | Filter commits by author name (substring match) |
| `--all-authors` | `report` | Show commits from all authors |
| `--max` | `report` | Max commits per repo (default: 5, 0 for all) |
| `--top` | `health` | Show only the N most-neglected repos |
| `--threshold` | `health` | Minimum severity to display: `warn` or `crit` |
| `--fix` | `doc` | Auto-fix issues (remove stale paths) |
| `--fish` | `shell-init` | Output fish shell syntax |
| `--pwsh` | `shell-init` | Output PowerShell syntax |

## JSON output

`--json` is inherited globally, but serialization is **command-specific**.
The table describes current behavior, not a promise that every inherited flag
changes a command's output. JSON changes rendering, **not side effects**: for
example, `open --json` still opens browser tabs, and `scan --json` still registers
repos unless `--dry-run` is given.

For supported result paths, stdout contains one uncolored JSON document.
`--quiet` suppresses hints, progress, and summaries, **never that document**;
errors and exit status remain unchanged. Fatal validation/configuration/setup
errors may return before any document is written. Capture stdout and stderr
separately and check the exit status before using results. Process exit codes
are 0 (success) and 1 (failure), not the child process's `exec` exit code.

### Command capabilities

All names below follow `soko`. An **array** is a bare top-level array, normally
`[]` when empty. **Fatal** means a command error on stderr and exit 1, not a
universal JSON error object. Row errors do not all imply exit 1; inspect the
listed fields even when the command succeeds. Quiet preserves the JSON shapes
in every JSON-capable row below. Only **Yes** in the Perf column means timing;
other commands accept `--perf` but do not add timing.

| Command / mode | JSON support and normal shape | Empty / no-op shape | Error records and partial-error exit policy | Perf |
|---|---|---|---|---|
| `list` | Yes: repo array (`name`, `path`, optional tags/family/metadata) | `[]` | Fatal registry/selector errors | No |
| `status` | Yes: status array | `[]` | Row `error`; repo read failures do not fail the command | Yes |
| `remotes` | Yes: remote/upstream array | `[]` | Row `error`; inspection/tracking problems do not fail the command | No |
| `diff` | Yes: changed-repo array with `files` | `[]`; clean repos omitted | Row `error` + `error_code`; any failed inspection exits 1 | No |
| `fetch`, `pull`, `sync` | Yes: action-result array | `[]` | Row `error` + `error_code`; actual failures exit 1; skips/attention states are not automatically failures | Yes |
| `exec -- <command>` | Yes: array with `exit_code`, `stdout`, `stderr` | `[]` | Child nonzero exits fail the command; missing-path/spawn errors also have `error` + `error_code` | Yes |
| `grep` | Yes: repo array with `matches` | `[]`; no-match repos omitted | Row `error` + `error_code`, empty `matches`; any failed search exits 1 | No |
| `report` | Yes: active-repo and failure array | `[]`; inactive repos omitted | Row `error` + `error_code`; any failed read exits 1, even with successful activity | No |
| `stats` | Yes: aggregate **object**, not an array | Zero-valued object; `activity_days` stays 30 | `missing` / `errors` counts; collection problems do not fail the command | No |
| `health` | Yes: ranked array (`score`, `severity`, `reasons`) | `[]` | Missing/read-error repos have critical reasons; collection problems do not fail the command | No |
| `branch [name]` | Yes: branch array | `[]` | Row `error` + `error_code`; inspection errors do not fail overview | No |
| `branch switch`, `branch stale` | Yes: outcome / stale-branch array | `[]`; stale omits repos without findings | Row `error` + `error_code`; failed switches/inspections exit 1 | No |
| `stash`, `stash pop` | Yes: action array | `[]`; repos with nothing to do omitted | Failures have `status: "failed"`, `error`, `error_code`; any failure exits 1 | No |
| `clean --dry-run` / `clean --force` | Yes: planned branches / execution-result array | `[]` | Row `error` + `error_code`; inspection, prune, or deletion failures exit 1; execution adds `deleted` / `failed` outcomes | No |
| `prune --dry-run` / `prune --force` | Yes: affected-registration array | `[]` | Fatal registry/save errors | No |
| `init` | Yes: repo object with `already_registered` | Existing registration returns object with `already_registered: true` | Fatal registration/save errors | No |
| `scan <directory>` | Yes: discovered-registration array | `[]` | Row `error` does not fail the scan; root/walk/save failures are fatal | No |
| `remove <name>` / `remove --path` / `remove --all` | Yes: removed-registration array after removal | Empty `--all` emits a text hint, or nothing with quiet; no JSON document | Fatal lookup/save errors; use `--all --force` to avoid prompting | No |
| `apply <file> --to <path>` | Yes: plan array; `--write --force` gives write results | `[]` for empty registry/tag selection; unmatched positional repos emit hint/no document | Row `error`; plan errors exit 0, failed writes exit 1 | No |
| `open` | Yes: opened-repo array with `url` or `error` | Unmatched tag emits hint/no document | Row `error` (no `error_code`); any failed open exits 1; unresolved/ambiguous targets are fatal | No |
| `pr` | Yes: pull-request array | `[]` | Fatal Git/`gh`/auth errors; reaching `--limit` is only a stderr hint, suppressed by quiet | No |
| `cd <name>`, `go [filter]` | Yes: selected `{ "name": "...", "path": "..." }` object; JSON does not request shell navigation | No selection is an error, not `[]` | Fatal selection/path errors; `go` still uses picker with a TTY, requires exactly one match without a TTY | No |
| `worktree add`, `worktree rm` | Yes: created / removed worktree object | No empty-result success contract | Fatal Git/registry/save errors | No |
| `worktree list`, `worktree adopt` | Yes: inventory / registration array | `[]` | Row `error`; failed family inventory/adoption exits 1; list's other row states do not necessarily fail | No |
| `ctx list`, `snapshot list` | Yes: saved-item summary array | `[]` | Snapshot list includes unreadable files as row `error` without failing; directory/config errors are fatal | No |
| `ctx show`, `snapshot show` | Yes: named saved-item object with `repos` | Unknown name is fatal | Fatal lookup/read errors | No |
| `ctx save`, `ctx switch`, `snapshot save`, `snapshot restore` | Yes: per-repo outcome array when processed | Empty save selection emits hint/no document | Row `error` + `error_code`; partial failures exit 1; save with no successful capture is fatal before JSON | No |
| `annotate [repo]` (read), `annotate --list` | Yes: metadata object / annotated-repo array | `meta: {}` / `[]` | Fatal target/config errors | No |
| `tag list`, `alias list` | Yes: tag-count / alias array | `[]` | Fatal config errors | No |
| `discover`, `discover status` | Yes: settings object | `{ "enabled": false }` when unconfigured | Fatal config errors | No |
| `config path`, `config get`, `config set`, `config list` | Yes: path / key-value / before-after / effective-settings object | Default settings object, not `[]` | Fatal key/config/save errors | No |
| `doc` | Yes: check array (`name`, `status`, `message`, `fixable`, `fixed`) | Checks still run without registered repos | Check statuses can report errors with exit 0; failed repair persistence is fatal | No |
| `undo --list` | Yes: journal array; keys use Go field names (`Op`, `Time`, `Branches`, etc.) | `null` when journal absent | Fatal journal read errors | No |
| `ui`, `config edit` | No JSON result; dashboard / external editor | Not applicable | Interactive behavior/errors unchanged | No |
| `tag` shorthand / `tag add` / `tag remove`, `alias set` / `alias remove`, annotation mutations, `discover on` / `discover off`, `ctx drop` / `snapshot drop`, `undo` execution | No JSON result; text confirmations/diagnostics | Quiet may leave no stdout | Normal mutation errors still exit 1 | No |
| `shell-init`, `completion bash/fish/powershell/zsh` | No JSON result; generated shell script | Not applicable | Generator errors exit 1; `--json` does not serialize scripts | No |
| `version`, help and command-group usage | No JSON result; text | Not applicable | Normal usage/error behavior unchanged | No |

These no-document exceptions are existing capability boundaries, not alternate
JSON schemas. Do not substitute `null` or `{}` when a command writes no document.
For reliable empty bulk results, use `list`, `status`, `remotes`, `diff`, `report`,
`health`, `branch`, `branch stale`, `stash`, `stash pop`, `clean --force`, `fetch`,
`pull`, or `sync`: empty registries, unmatched supported selectors, and
post-collection filters produce `[]`. `stats` intentionally returns an object.

### Interactive restrictions

- `clean --json` and `prune --json` require `--force` or `--dry-run` for normal runs.
- `--select` cannot be combined with `--json` on `clean`, `prune`, or `remove --all`.
- `apply --json --write` requires `--force`; `remove --all --json` can still prompt unless forced.
- `go --json` does not disable its TTY picker; without a TTY exactly one matching repo is required.
- `ui` needs terminal input/output; use `status --json` for automation. `config edit` runs `$EDITOR`, not a serializer.

Quiet never supplies confirmation or makes an interactive command noninteractive.
`clean --dry-run --prune` still prunes remotes; omit `--prune` for a read-only preview.

### Array, stats, and perf examples

With one registered repo at `/repos/api`, `soko list --json` returns a bare array:

<!-- json-example: registered | list --json -->
```json
[
  {"name": "api", "path": "/repos/api"}
]
```

With an empty registry, `soko status --json` returns:

<!-- json-example: empty | status --json -->
```json
[]
```

For that same empty registry, `soko stats --json` returns this object (optional
`missing`, `errors`, `most_active`, and `least_active` are absent):

<!-- json-example: empty | stats --json -->
```json
{
  "repos": 0,
  "tags": 0,
  "total_commits": 0,
  "total_branches": 0,
  "total_size_bytes": 0,
  "clean": 0,
  "dirty": 0,
  "behind_remote": 0,
  "stale_branches": 0,
  "stale_repos": 0,
  "no_remote": 0,
  "activity_days": 30,
  "recent_commits": 0,
  "active_repos": 0,
  "active_authors": 0
}
```

`--perf --json` intentionally changes `status`, `fetch`, `pull`, `sync`, and
nonempty `exec` output to an object with **`repos` and `timing`**. The original
rows move under `repos`; measured rows may add `duration_ms` (omitted when it
rounds to zero). `timing.wall_ms` is elapsed collection time, `git_ms` sums
per-repo durations (including failures, or child execution for `exec`), `repos`
counts timed rows, `concurrency` reports the configured limit (currently 8,
also for `exec --seq`), and `slowest` lists up to 25 rows, slowest first.
Status filters also filter timed rows. Quiet does not remove timing.

An empty selection starts no work: `soko status --json --perf` returns:

<!-- json-example: empty | status --json --perf -->
```json
{
  "repos": [],
  "timing": {
    "wall_ms": 0,
    "git_ms": 0,
    "repos": 0,
    "concurrency": 8,
    "slowest": []
  }
}
```

The same empty envelope applies to `fetch`, `pull`, and `sync`. Empty `exec`
currently returns bare `[]` even with perf; it starts no child command.
Without JSON, perf appends a human-readable timing block on stdout when rows
were timed. `SOKO_PERF=1` enables perf too; use **`--perf=false`** to pin a bare
array schema even in an inherited environment:

```bash
soko status --json --perf=false | jq '.[].name'
soko status --json --perf | jq '.repos[].name'
```

When a registered `/repos/missing` checkout is absent, `soko fetch --json`
exits 1 but retains its failure document on stdout (summary on stderr):

<!-- json-example: missing | fetch --json -->
```json
[
  {"name": "missing", "path": "/repos/missing", "status": "failed", "error": "path not found", "error_code": "path_missing"}
]
```

`error_code` is command-specific, not universal. In particular, `exec` child
failures use `exit_code`/`stderr`, stash failures use `status`/`error`, and diagnostic
commands may report findings without a failed process exit. Preserve successful
rows in a partial result, but do not mistake them for a complete successful run.

## Usage examples

### Status with filters

```bash
soko status                         # all repos
soko status auth                    # single repo (prefix match)
soko status auth frontend           # multiple specific repos
soko status --fetch                 # fetch first, then show status
soko status --dirty                 # only repos with uncommitted changes
soko status --tag backend --behind  # only backend repos behind remote
soko status --json                  # machine-readable output
```

### Remotes and upstream tracking

```bash
soko remotes                        # origin + upstream for every repo
soko remotes api worker             # only the named repos
soko remotes --tag backend          # only backend repos
soko remotes --missing-upstream     # only repos with no remote or no upstream
soko remotes --json                 # structured output for scripting
```

`soko remotes` is the read-only sibling of `soko status`: where `status` answers
"what changed?", `remotes` answers "where does this repo push and pull?". It
shows each repo's origin URL and upstream tracking branch and flags the two
problem cases — **no remote** (never pushed) and **no upstream** (a local-only
branch or detached HEAD) — in yellow. It runs no network operations.

### Pull updates

```bash
soko pull                           # fast-forward every repo (--ff-only)
soko pull auth frontend             # pull specific repos
soko pull --rebase                  # replay local commits onto the upstream
soko pull --tag backend             # pull only backend repos
soko pull --no-worktrees            # skip linked worktrees
soko pull --json                    # machine-readable output
```

`soko pull` uses `--ff-only` by default, so it never creates a surprise merge
commit and reports a clear failure on any repo that has diverged from its
upstream. Use `--rebase` when you want your local commits replayed on top.
Repos whose current branch has no upstream (local-only branches, detached HEAD)
are skipped rather than counted as failures.

### Sync everything

```bash
soko sync                           # fetch all, fast-forward what's safe
soko sync --tag backend             # only backend repos
soko sync --fetch-only              # fetch but never pull
soko sync --json                    # machine-readable output
```

`soko sync` is the one-command morning routine: it fetches every repo, then
fast-forwards only the repos where that is provably safe — clean working tree,
an upstream, and no divergence. Everything else is *reported, never touched*:
dirty repos show as `dirty (skipped pull)`, diverged branches as
`diverged (needs rebase)`. sync never creates merge commits, never rebases,
and never risks your uncommitted work.

```
    REPO            ACTION         RESULT
  ────────────────────────────────────────────────────
  ✓ auth-service    fetch + pull   3 new commits
  · backend-api     fetch          up to date
  ⚠ frontend        fetch only     dirty (skipped pull)
  ✓ shared-lib      fetch + pull   12 new commits

  4 repos · 2 pulled · 1 up to date · 1 need attention · 0 skipped · 0 failed · 15 new commits
```

### Workspace contexts

Polyrepo feature work spans several repos, each on its own branch with local
changes. When an interrupt arrives — oncall, another client, an urgent review —
`soko ctx` saves the whole arrangement and brings it back later:

```bash
soko ctx save client-a              # record branch per repo, stash dirty trees
soko ctx save client-a api front    # only these repos
soko ctx save client-a --tag work   # only tagged repos

# ... handle the interrupt on other branches ...

soko ctx switch client-a            # restore branches and saved changes
soko ctx list                       # saved contexts with age and stash counts
soko ctx show client-a              # per-repo branch + stash detail
soko ctx drop client-a              # forget it (stashes stay in their repos)
```

`save` stashes only dirty repos (including untracked files), recording each
member's exact stash commit and creating an ownership ref under Git's
per-worktree `refs/worktree/soko/ctx/<stash-oid>` namespace. Linked worktrees
share a stash list; a context-name message alone cannot identify their saved work.
`switch` applies that exact commit, leaving every stash-list entry untouched.
Backups are retained instead of deleting a numeric position that another Git
process could change between lookup and cleanup.

Context switches restore file contents, not index staging. On a successful
switch, staged-only and mixed staged/unstaged tracked edits return **unstaged**;
untracked files return as untracked. Run `git add` again where needed. This
contents-only policy avoids the extra conflicts and refusals of Git's
`stash apply --index`. If `save` fails before recording the context, its rollback
instead attempts to restore both index and working tree and retains the stash
for manual recovery if that fails.

Shared `refs/soko/ctx-backups/<stash-oid>` refs pin pending work even if its
stash-list entry is removed and GC runs from another linked checkout.
Successful applications create Git refs under `refs/soko/ctx-restored/<stash-oid>`
to prevent reapplication and keep the backup reachable. This does not rewrite
the shared config, so a concurrent context save is not overwritten by switch
completion. Dropping a context leaves backups, ownership refs, and completion
refs intact; stash counts describe what was captured, not pending applications.

`switch` refuses any repo that is dirty *right now* — save the current state
under another name first. Missing ownership refs, replacement checkouts, and
older stashed contexts without ownership metadata fail before checkout; inspect
`git stash list` and recover manually rather than guessing ownership. A recreated
worktree does not inherit the removed worktree's ownership refs, even if Git
reuses its administrative directory. Older clean context records still work.
Conflicted restores retain their backup and remain retryable after the conflict
is resolved. A failure or interruption after application but before completion
recording can require manual recovery; restoration is not a crash-atomic
transaction. Shared stash-list insertion, removal, and reordering cannot redirect
application. Concurrent replacement of a target checkout or changes to its
working files are not serialized by save or switch.

Contexts live in the same config file as the registry. If another command updates
that file while a save is gathering Git state, the stale save fails explicitly
and rolls its captured work back instead of replacing the completed update.

### Snapshots

Where `ctx` moves work-in-progress between branches, `soko snapshot` pins exact
positions — branch and HEAD SHA per repo. Take one before a risky bulk
operation (sync, pull, clean), and restore moves every branch back to the
recorded commit:

```bash
soko snapshot save pre-sync         # record branch + SHA per repo
soko snapshot save pre-sync --tag work --force   # tagged repos, overwrite existing
soko snapshot restore pre-sync      # move each repo back
soko snapshot list                  # saved snapshots
soko snapshot show pre-sync         # per-repo detail
soko snapshot drop pre-sync         # delete it
```

Snapshots pin commits; they never touch uncommitted work. `save` records
whether a repo was dirty, and `restore` refuses dirty repos — stash first, or
use `soko ctx` for work in progress. `restore` rewinds branches that moved and
recreates branches that were deleted.

### Tags

```bash
soko init --tag backend --tag go    # tag during registration
soko tag backend go                 # tag current repo (shorthand)
soko tag add critical               # add tag to current repo
soko tag add -r my-repo critical    # add tag to a specific repo
soko tag remove backend             # remove tag from current repo
soko tag list                       # show all tags with repo counts
soko status --tag backend           # filter any command by tag
soko fetch --tag frontend           # fetch only frontend repos
soko exec --tag go -- go mod tidy   # run in tagged repos only
```

### Annotate repos with metadata

Tags answer "which group is this in?"; annotations answer "who owns it, is it
still active, how important is it, and what should I remember about it?" Owner,
status, priority, and note are well-known keys, but the map is open — any
`key=value` works.

```bash
soko annotate api --set owner=alice --set status=active   # set keys (repeatable)
soko annotate api --set note="migrating to v2"            # quote values with spaces
soko annotate api --set note="waiting, on legal"          # commas stay in the value
soko annotate api                                         # show a repo's metadata
soko annotate api --json                                  # machine-readable
soko annotate api --unset priority                        # remove a key
soko annotate api --clear                                 # remove all metadata
soko annotate --list                                      # every annotated repo
```

Then filter `soko list` and `soko status` by it — repeated `--meta` flags
combine with **AND**:

```bash
soko list   --meta status=active                          # only active repos
soko status --meta priority=high --meta status=active     # high priority AND active
soko list   --meta note="waiting, on legal"                # match the complete value
```

Each `--set` and `--meta` flag accepts one `key=value`; commas are literal.
Repeat the flag to set or filter multiple keys.

Metadata lives in the same `~/.config/soko/config.yaml` registry and survives
`soko scan` / `soko prune`. A repo with no annotations writes no `meta:` block,
so existing configs are untouched.

### Aliases

```bash
soko alias set morning "sync --tag work"          # create an alias
soko alias set deploy "exec --tag prod -- make deploy"
soko alias set greet 'exec -- echo "hello world"'    # preserve a quoted argument
soko alias list                                    # show all aliases
soko alias remove morning                          # remove an alias

soko morning                                       # runs soko sync --tag work
soko deploy                                        # runs soko exec --tag prod -- make deploy
```

Built-in commands always take priority over aliases.
Alias commands support shell-style single/double quotes and backslash escapes.
Variables, globs, and command substitutions stay literal. Extra arguments are
appended as supplied, and malformed quotes or escapes are rejected.

### Activity report

```bash
soko report                         # your commits, last 7 days
soko report --days 1                # standup: yesterday
soko report --days 30               # monthly summary
soko report --tag backend           # only backend repos
soko report auth                    # specific repo
soko report --all-authors           # everyone's commits
soko report --author "John"         # specific author
```

The default author comes from `git config user.name` in the current directory.
If Git cannot resolve it (for example, outside a repo without a global name),
specify `--author` or opt in to `--all-authors`.

Reports are complete by default: a missing checkout or failed Git read causes
exit status 1, even if other repos have activity. Successful activity remains
on stdout; text errors identify each failed repo on stderr (also with `--quiet`).
An inactive repo (including one with no commits yet) is not an error. When all
selected repos are readable but none have matching commits, `--json` returns
`[]` and exits 0. When any read fails, JSON remains an array of active results
plus failure entries containing `name`, `path`, `error`, and `error_code`;
inactive repos are omitted. For example, a partial report may contain:

```json
[
  {"name":"api","path":"/repos/api","branch":"main","commits":[{"message":"Fix login","time":"2026-10-27T09:00:00Z"}]},
  {"name":"missing","path":"/repos/missing","error":"path not found","error_code":"path_missing"}
]
```

Git inspection errors use `git_failure` (or a more specific Git error code).
Failure-only selections return only failure entries and exit 1. Repo names and
tags narrow the selected set before inspection; `--author` remains a literal
substring filter, and `--all-authors` is still opt-in.

### Triage repo health

```bash
soko health                         # ranked table, worst repo first
soko health --tag backend           # only backend repos
soko health --top 5                 # the 5 most-neglected repos
soko health --threshold crit        # only repos that need urgent attention
soko health --json                  # machine-readable ranking
```

`soko health` answers "where do I start?". It reuses the same signals as
`soko stats` — dirty state, commits behind, stale branches, conflicts, detached
HEAD, missing remote — but scores and ranks each repo individually instead of
aggregating workspace totals. It is read-only: it never fetches or mutates a
repo.

### List pull requests for the current repo

```bash
soko pr                             # open pull requests: number, title, branch, author
soko pr --json                      # machine-readable results
soko pr --limit 100 --json          # request up to 100 pull requests
```

Run `soko pr` from anywhere inside a repository's working tree; registration
with soko is not required. Install the [GitHub CLI](https://cli.github.com)
and authenticate for the repository's host with `gh auth login` first. The
command uses `gh` to resolve the repository from its Git remotes.

Only open pull requests are listed, up to 50 by default. `--limit` accepts a
positive number. When the result count reaches that limit, soko prints a hint
to stderr that more results may be available; `--quiet` suppresses the hint.
JSON output contains `number`, `title`, `branch`, `state`, `url`, and `author`
for each pull request, or an empty array when none are open.

### Run commands across repos

```bash
soko exec -- git pull --rebase      # pull all repos
soko exec -- git stash              # stash everything
soko exec -- make test              # run tests everywhere
soko exec --seq -- git log -1       # sequential, one at a time
soko exec --tag backend -- go vet   # only in backend repos
```

### Copy a file into many repos

Polyrepo housekeeping means landing the same CI config, license, or lint
settings everywhere. `soko apply` copies one source file into many repos,
diff-first:

```bash
soko apply ci.yml --to .github/workflows/ci.yml          # dry-run: per-repo diff
soko apply ci.yml --to .github/workflows/ci.yml --tag go # only repos tagged "go"
soko apply LICENSE --to LICENSE backend auth             # only these repos
soko apply ci.yml --to .github/workflows/ci.yml --write  # write after confirmation
soko apply .editorconfig --to .editorconfig --json       # machine-readable plan
```

By default `apply` is a dry run: it shows a per-repo unified diff and writes
nothing. `--write` applies the changes after a confirmation prompt (`--force`
skips it). Writes are local file operations — apply never touches git or the
network, so review and commit per repo as usual.

### Search across repos

```bash
soko grep handleAuth                  # search every registered repo
soko grep handleAuth auth backend     # only these repos (name / prefix match)
soko grep handleAuth --tag go         # only repos tagged "go"
soko grep "func .*Handler" --regexp   # treat the pattern as an extended regex
soko grep TODO -i                     # case-insensitive
soko grep config.yaml --files-only    # list matching file paths, not lines
soko grep handleAuth --json           # machine-readable, grouped by repo
```

`soko grep` runs `git grep` across the selected repos in parallel and groups
matches by repo; repos with no match drop out silently. It is read-only and
honours each repo's tracked-file set and `.gitignore`. The pattern is a fixed
string by default — pass `--regexp` for a POSIX extended regex.

Missing paths and Git failures are reported per repo and cause a non-zero exit
status, while successful matches remain available. JSON failures include `error`
and `error_code` (`path_missing` or a Git error code), with an empty `matches`
array; successful searches with no matches are still omitted.

### Navigation

Requires shell integration (one-time setup):

```bash
# Bash / Zsh
eval "$(soko shell-init)"

# Fish
soko shell-init --fish | source

# PowerShell
soko shell-init --pwsh | Invoke-Expression
```

Then navigate directly:

```bash
soko cd auth                        # jump by name (prefix match)
soko go                             # interactive picker
soko go --tag backend               # picker filtered by tag
soko go --repo api                  # base checkout and worktrees of api
soko go --repo .                    # family of the current checkout
```

### Live dashboard

`soko ui` opens a full-screen, auto-refreshing dashboard of your workspace:
each repo's branch, dirty state, ahead/behind, last-commit age, and a health
badge. Local state refreshes every 5 seconds — cheap, no network. Meant to
live in a tmux pane all day.

```bash
soko ui                             # the whole workspace
soko ui --tag backend               # only backend repos
soko ui --fetch 5m                  # also fetch from remotes every 5 minutes
soko ui --repo api --group-by repo  # browse one repository family
```

Keys: `j`/`k` move · `enter` cd into the repo (needs shell integration) ·
`/` search by name, branch, or tag · `s`/`S` cycle sort · `f`/`F` cycle filter
(all/dirty/behind/ahead/conflicts) · `t`/`T` cycle tag filter · `b` group by tag ·
`B` group by repository family · `o` open in browser (`p`/`i`/`a` for PRs/issues/actions) · `P` pull ·
`r` re-fetch now · `?` help · `q` quit.

Dashboard pulls (`P`) fast-forward the selected or marked repos after a
confirmation prompt. Each batch is recorded in the journal so `soko undo` or
dashboard `u` can rewind it to the pre-pull commits. Pull undo requires a clean checkout,
including no untracked files, on the original branch at the recorded pulled tip;
it refuses newer commits, switched branches, and older journal entries without
that safety metadata. If a batch is only partly undone, resolve the refusal and
retry with `soko undo` or dashboard `u`. Each successful reset is recorded;
completed repos are skipped on retry, preserving any work made there since.
The entry stays in the journal until all its repos are undone, then older undo
history becomes available again.

### Automatic discovery

Tired of running `soko init` or `soko scan`? Turn on auto-discovery and repos
register themselves the first time you `cd` into them. It's opt-in and, like
`mise activate`, runs from the shell hook on directory change.

```bash
soko discover on                    # enable auto-discovery
soko discover on --root ~/work      # only discover under ~/work (repeatable)
soko discover on --tag discovered   # tag everything discovered
soko discover on --ignore '*-tmp'   # skip paths matching a glob (filepath.Match)
soko discover status                # show current settings
soko discover off                   # disable
```

Enabling or disabling changes what `soko shell-init` emits, so open a new shell
or re-run `eval "$(soko shell-init)"` afterwards to activate it. Then just work
as usual:

```bash
cd ~/work/new-service
#  ✓ discovered new-service (~/work/new-service)
```

Discovery is conservative by design: it's off until you enable it, the hook
only runs in interactive shells (never in CI or scripts), and it skips
submodules, your home directory, and `node_modules`/`vendor` trees. Use `--root`
to scope it and `--tag discovered` to make discovered repos easy to find or
clean up later (`soko remove`, `soko list --tag discovered`).

### Open in browser

```bash
soko open                           # registered checkout containing cwd (also from nested dirs)
soko open auth-service              # by name
soko open --prs                     # pull/merge requests
soko open --issues                  # issues
soko open --actions                 # CI/CD
soko open --tag backend             # open all backend repos
soko open --tag backend --prs       # PRs for all backend repos
```

Supports GitHub, GitLab, and Bitbucket — auto-detects the platform from the remote URL.
Without a name or tag, `open` selects the registered Git checkout containing the current directory.
Linked worktrees and submodules use their own checkout roots: an unregistered one does not fall back
to its parent, even if that parent is registered. Outside a Git checkout, shorthand is unavailable;
explicit names and tags still work.
Choose either one repo name or `--tag`; combining them returns an error before opening any browser tabs.

### Manage repos

```bash
soko list                           # show all registered repos
soko list --group                   # tree view grouped by tag
soko list --tag infra               # filter by tag
soko remove old-project             # unregister by name
soko remove --path /old/path        # unregister by path
soko remove --all --force           # clear everything
soko remove --all --select          # pick which repos to unregister
soko prune --dry-run                # preview repos whose dirs were deleted
soko prune                          # drop deleted repos (with confirmation)
soko prune --force                  # skip confirmation
soko prune --select                 # pick which missing repos to drop
soko prune --dry-run --json         # machine-readable preview (--json needs --force or --dry-run)
```

### Clean up stale branches

```bash
soko clean --dry-run               # preview merged branches
soko clean                         # delete with confirmation
soko clean --force                 # skip confirmation
soko clean --force --json          # machine-readable deletion outcomes
soko clean --select                # pick which repos to clean before deleting
soko clean --prune                 # also prune stale remote refs
soko clean --tag backend           # only backend repos
soko clean auth                    # specific repo
```

Branch deletion failures return a non-zero exit status in text and JSON modes.
JSON retains the planned `branches` and adds non-empty `deleted` and `failed`
arrays for actual outcomes. Each failure includes `branch`, `error`, and
`error_code`; the repository also carries an error summary. Dry-run JSON remains
a preview without execution outcomes. Only successful deletions enter the undo
journal.

Completeness policy: both `--dry-run` and execution return non-zero when any
selected repository cannot be inspected (missing path: `path_missing`; existing
non-Git directory or Git read failure: `git_failure`). Healthy repositories
still appear in the preview and are deleted on confirmed/`--force` runs; failed
ones are not touched. An all-valid empty cleanup succeeds, an all-uninspectable
selection fails without prompting, and a mixed selection fails after processing
healthy repos. Inspection failures are counted as repositories, **not** failed
branch deletions. `--prune` may additionally fail independently and never
prevents healthy branch deletion; failed branch deletions also return non-zero.
Text and quiet modes retain per-repo failure rows on stdout; quiet hides only
the summary. The non-zero error summary goes to stderr, and no successful
deletion footer is printed for an incomplete run. JSON (including quiet) emits
one parseable stdout array with per-repo `error` and `error_code` on failure;
`deleted` / `failed` are present only for actual deletion outcomes. The error
summary remains on stderr. `--dry-run --prune` previews branch deletion but
still performs remote pruning; omit `--prune` for a mutation-free preview.

`--select` opens the interactive picker (the one `soko go` uses) with every
matched repo pre-checked; deselect the repos you want to spare with space, press
enter, and only the chosen subset is touched. It can only ever *narrow* the set,
never widen it, so it is strictly safer than the all-or-nothing default. In a
pipe or CI (no TTY) `--select` is ignored and the command runs on the full set.

### Health check and config

```bash
soko doc                            # check paths, git, remotes, shell-init
soko doc --fix                      # auto-remove stale entries
soko config list                    # dump the effective config (table)
soko config list --json             # dump the effective config (JSON)
soko config path                    # print config file location
soko config edit                    # open config in $EDITOR
soko config set git_path /usr/local/bin/git  # use a custom git binary
soko config get git_path            # check current git binary
soko config get git_path --json     # {"key":"git_path","value":"..."}
```

`config path`, `config get`, `config set`, and `config list` all honour the
global `--json` flag, so soko's configuration is fully scriptable.

`soko doc` also warns when a registered worktree's `worktree_of` parent name
is missing or ambiguous, even if its checkout is healthy. These warnings remain
visible with `--quiet` and in `--json`, and identify affected `--repo` filters and
worktree commands. Use `soko config edit` to verify the base registration and
correct references or duplicate names manually; `--fix` never guesses a parent
or merges metadata. Existing stale-path repair still unregisters a removed base's
linked worktrees as a family, without deleting checkout directories.

### Git worktrees

soko supports git worktrees natively. If you use worktrees as your primary branching workflow, use `--worktrees` to discover and register them:

```bash
# Scan and discover repos + worktrees
soko scan ~/projects --worktrees

# Register a single worktree
cd ~/projects/api/feat-oauth
soko init --worktree

# See everything — worktrees show alongside their parent
soko list
#  api                ~/projects/api/main
#  api/feat-oauth     ~/projects/api/feat-oauth  → api
#  api/hotfix-123     ~/projects/api/hotfix-123  → api

# Jump to a worktree
soko cd api/feat                    # prefix match on parent/branch
soko go                             # pick interactively
soko go --repo api                  # only api and its registered worktrees
soko go --repo .                    # same family from any checkout/subdirectory

# Group by base repository, independently of tags
soko list --group-by repo
soko status --group-by repo
soko ui --group-by repo             # B toggles this view; b toggles tag grouping

# Status works per-worktree
soko status --dirty

# Remove a parent — linked worktrees are removed too
soko remove api

# Skip worktrees for bulk operations
soko fetch --no-worktrees
soko exec --no-worktrees -- git pull
```

Without `--worktrees`, soko detects when you're in a worktree and registers the main repo instead — no duplicates.

Worktrees inherit their parent repo's tags at filter time: `--tag backend`
matches a worktree whose parent is tagged `backend`, and retagging the parent
instantly re-scopes its worktrees. `soko tag remove` on a worktree only removes
the worktree's own tags and errors on inherited ones — remove those on the
parent.

Worktrees created with plain Git can be adopted without scanning a directory:

```bash
soko worktree list                  # also shows Git-known unregistered worktrees
soko worktree adopt api --dry-run   # preview registration for one base
soko worktree adopt api             # register its existing linked checkouts
soko worktree adopt                 # reconcile all registered bases
soko worktree adopt --tag backend --tag-with wip
```

`adopt` is idempotent and preserves existing names, tags, and metadata. It
registers live checkouts, including detached worktrees; missing directories
are left for inspection with `worktree list` and cleanup with `soko prune`.
Both `adopt` and `scan --worktrees` support `--json` and `--dry-run`; JSON
changes output only, while dry-run prevents registry writes.

Each checkout is registered at its Git working-tree root, even when `init`
runs from a subdirectory. Aliases default to `parent/branch`; detached
checkouts use `parent/directory`, and collisions receive a numeric suffix.
Aliases remain stable if you later change a checkout's branch. Separate
clones of the same remote remain separate repository families. With a
separate Git directory, register the base checkout before its linked
worktrees. Bare repository bases are rejected explicitly.

Switching worktrees is directory navigation: it does not check out a branch,
stash files, or require a clean working tree. Shell integration isolates
navigation requests per shell. After upgrading, re-run `soko shell-init` or
open a new shell to activate the updated hook.

Beyond tracking, `soko worktree` manages the lifecycle — create, inspect, and
tear down worktrees without leaving the registry stale:

```bash
soko worktree add api feat-x        # create ../api-feat-x + register, print path
soko worktree add api feat-x -b     # create the branch in the same step
soko worktree add api fix --path ~/tmp/hotfix --tag wip
soko worktree list                  # WORKTREE · PARENT · BRANCH · STATUS · PATH
soko worktree rm api/feat-x         # remove the dir + unregister
soko worktree rm api/feat-x --force # even with uncommitted changes
cd "$(soko worktree add api feat-x -q)"   # create and jump in one line
```

`add` places the worktree next to the main repo as `<repo>-<branch>` unless
`--path` says otherwise, names the entry `parent/branch` so `cd`, `go`, and
`status` work unchanged, and prints the new path last for command
substitution. `rm` refuses a dirty worktree without `--force` and never
touches branches — deleting merged branches stays `soko clean`'s job.

### Branches across repos

Polyrepo feature work means the same branch name in N repos. `soko branch`
shows where every repo stands and drives them together:

```bash
soko branch                         # current branch per repo
soko branch feat/sso                # which repos have feat/sso
#  REPO        BRANCH        feat/sso?
#  ──────────────────────────────────────
#  api         feat/sso      ✓ current
#  frontend    main          ✓ local
#  shared      main          ○ remote only
#  infra       fix/tf        — missing

soko branch switch feat/sso         # check it out wherever it exists
soko branch switch feat/sso -b      # create from the default branch where missing
soko branch switch feat/sso --tag backend
soko branch stale                   # unmerged branches untouched > 90 days
soko branch stale --days 30
```

`switch` checks out local branches directly and creates tracking branches for
remote-only ones. It switches base repositories only: linked worktree entries
are excluded even when selected by name or tag, so their checkouts stay untouched.
A dirty repo is refused and left untouched while the others
continue — commit, stash, or `soko ctx save` first. `stale` surfaces the
branches `soko clean` cannot touch: started, never merged, and quietly
abandoned.

### tmux-sessionizer integration

Use soko as the directory source for your tmux-sessionizer:

```bash
# Pick a repo/worktree and create a tmux session for it
TARGET=$(soko list --json | jq -r '.[].path' | fzf)
SESSION=$(basename "$TARGET")
tmux new-session -d -s "$SESSION" -c "$TARGET" 2>/dev/null
tmux switch-client -t "$SESSION"
```

Or use soko's built-in interactive picker, which supports fuzzy search:

```bash
soko go    # pick a repo or worktree, cd into it
```

### Quiet mode for scripts and CI

`--quiet` (`-q`) suppresses the human-facing chrome — info lines, the
missing-repo nudge, progress counters, and the trailing summary footer — while
leaving tables, errors, exit codes, and `--json` output untouched. It is the
orthogonal complement to `--json`: `--json` changes *what* soko prints, `--quiet`
changes *whether* the extras print at all.

```bash
soko status --quiet                 # table only — no summary, no hints
soko fetch --quiet                  # per-repo results, no progress, no footer
soko status --quiet --json          # the JSON document only, nothing before it
soko clean --quiet --force          # destructive run with no chatter
SOKO_QUIET=1 soko status            # same, via env for cron/CI without editing args
```

Errors are never silenced: a failing `soko pull --quiet` still prints its error
to stderr and exits non-zero. An explicit `--quiet` always wins over
`SOKO_QUIET`, so a script can export the env globally and you can still get full
output with `--quiet=false`.

Two more scripting aids: per-repo failures in `--json` output (`pull`, `fetch`,
`exec`, `clean`, `stash`, `grep`) carry a stable `error_code` field, so scripts can
branch on the failure type without parsing human-readable messages. And the
global `--perf` flag (or `SOKO_PERF=1`) reports per-repo and aggregate timing
after a parallel command — handy for spotting the one slow repo that drags
down every `soko fetch`.

## Configuration

soko stores registered repos in a single YAML file:

```
~/.config/soko/config.yaml
```

Respects `$XDG_CONFIG_HOME` if set. The format is minimal:

```yaml
aliases:
  morning: sync --tag work
  deploy: exec --tag prod -- make deploy
repos:
  - name: auth-service
    path: /home/dev/work/auth-service
    tags:
      - backend
      - go
    meta:
      owner: alice
      status: active
  - name: auth-service/feat-oauth
    path: /home/dev/worktrees/feat-oauth
    worktree_of: auth-service
    tags:
      - backend
  - name: frontend
    path: /home/dev/work/frontend
    tags:
      - frontend
```

Tags, `meta`, and `worktree_of` are optional — repos without them work the same
as before.

CLI writes compare the exact loaded file revision while holding a short native
lock on `config.yaml.lock`. A stale or contended interactive write fails with a
retry message instead of silently replacing another command's update. Git work
and prompts run outside this lock. Discovery hooks load and register under the
same lock, rechecking current discovery policy and allocating unique aliases;
they quietly skip if the lock cannot be obtained within one second.

The mode-0600 lock file remains on disk; its presence does not mean it is locked.
The operating system releases locks when the process closes the file or exits.
Do not remove or replace the lock file while soko commands are running.

`soko config edit` opens a private copy, preserves comments and formatting, and
commits only if the original file has not changed. Invalid, failed, or conflicting
edits leave the live config untouched and report where editor files were kept.
This coordination covers current soko writers; older binaries and programs that
write the live YAML directly do not participate in the locking protocol.

## Building from source

Go 1.27.2 or newer is required. The `go` directive in `go.mod` sets the minimum
supported Go version; it does not pin builds to exactly that toolchain.

```bash
git clone https://github.com/CelikE/soko.git
cd soko
make build
make test
```

Development linting uses **golangci-lint v2.14.0**, built with Go 1.27 or newer,
matching CI. Run `make lint` to check the code before submitting changes.

### Local checks and manual CI

CI is opt-in: pushes, pull requests, and schedules do not start test workflows.
Run checks locally before merging:

```bash
make lint
make test                           # full suite with race detection
make man-check
python3 -m unittest discover -s .github/scripts -p 'test_*.py'
```

When needed, use **Actions → Run workflow** or dispatch from the CLI:

```bash
gh workflow run ci.yml --ref master        # lint/config checks, Linux race/coverage, Windows smoke
gh workflow run platform.yml --ref master  # full macOS and Windows suites
gh workflow run race.yml --ref master      # macOS and Windows race suites
```

Use a branch name instead of `master` to verify pending changes. Local checks
validate the host platform only; use manual workflows for other platforms.
Tag-driven release publishing and the Winget release workflow remain automatic.

## Dependencies

soko is a single binary with minimal dependencies. No CGo, no git libraries — it shells out to the `git` CLI directly.

| Dependency | Purpose |
|-----------|---------|
| [spf13/cobra](https://github.com/spf13/cobra) | CLI framework |
| [gopkg.in/yaml.v3](https://github.com/go-yaml/yaml) | Config file parsing |
| [fatih/color](https://github.com/fatih/color) | Terminal colors (respects `NO_COLOR`) |
| [golang.org/x/sync](https://pkg.go.dev/golang.org/x/sync) | Parallel execution |
| [golang.org/x/term](https://pkg.go.dev/golang.org/x/term) | Interactive picker (raw terminal) |

## Releasing

Maintain user-facing changes in `CHANGELOG.md` and merge an audited PR into
`master`. Run `make release-check` (requires GoReleaser and mandoc), dispatch
`ci.yml` manually, and wait for Linux and Windows CI to pass before tagging:

```bash
git tag -a v0.22.0 -m "Release v0.22.0"
git push origin v0.22.0
```

Use the next appropriate semantic version for subsequent releases. The tag
triggers GoReleaser to publish binaries, checksums, package updates, and
GitHub-native release notes. Before publishing, the workflow synchronizes
`CelikE/winget-pkgs` with Microsoft upstream using `WINGET_TOKEN`; the published
release then triggers the Windows manifest submission workflow. The `.changes`
directory is a historical archive; new releases use `CHANGELOG.md` directly.

## License

[MIT](../LICENSE)
