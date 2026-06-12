# Changelog

## Unreleased

### Features

- soko snapshot — save and restore exact repo positions: snapshot save records branch + HEAD SHA per repo, snapshot restore moves every repo back (rewinding moved branches, recreating deleted ones, refusing dirty trees), plus list / show / drop. The save game before a risky bulk operation; completes the trust layer started by soko undo.
- Worktree entries now inherit their parent repo's tags at filter time, so `--tag` filters (status, sync, fetch, list, etc.) include worktrees of tagged repos. Retagging the parent instantly re-scopes its worktrees; `tag remove` on a worktree only removes own tags and errors on inherited ones.
- `soko ui` gains its first mutating key: `P` fast-forward pulls the selected repo after a confirmation prompt, runs asynchronously, and records a journal entry so `soko undo` rewinds it to the pre-pull SHA. Satisfies feature 44's scope guard — mutations only once undo exists.
- `soko undo` reverts the last destructive operation via a capped pre-image journal beside the config (`journal.yaml`). `soko clean` now records the branches it deletes, so `soko undo` recreates them at their exact SHAs; `soko undo --list` shows the journal. This is the trust layer that will unlock mutating keys in `soko ui`.
- `soko ui` gains read-only power keys — a tag legend, group-by-tag view (`G`), live name search (`/`), a filter cycle (`f`: all/dirty/behind/ahead/conflicts), browser-page jumps (`o`/`p`/`i`/`a` for home/PRs/issues/actions), and a `?` help overlay
- Live full-screen TUI dashboard with `soko ui` — auto-refreshing local workspace state (branch, dirty, ahead/behind, last-commit age, health badge) with sort, dirty/tag filters, cd-on-enter, open-in-browser, and optional background `--fetch`
- Add soko branch for cross-repo branch operations: current branch per repo, branch lookup (current/local/remote/missing), bulk switch with -b create-from-default, and stale unmerged branch detection
- Add `soko worktree` to manage git worktrees across repos: `worktree add <repo> <branch>` creates and registers in one step (with `-b` to create the branch, `--path`, `--tag`), `worktree list` shows all worktrees with live branch and dirty status, and `worktree rm` removes the directory and registry entry together (refusing dirty trees without `--force`).
- Add `soko ctx` — save and restore workspace contexts: `ctx save` records each repo's branch and stashes dirty trees under a per-context message, `ctx switch` checks the branches back out and pops the stashes (refusing repos that are dirty right now), plus `ctx list`/`show`/`drop`.
- Add `soko sync` — fetch all repos, then fast-forward the ones that are provably safe (clean, upstream, not diverged); dirty, diverged, and fetch-only-behind repos are reported as needing attention. Supports positional repos, `--tag`, `--fetch-only`, `--no-worktrees`, and `--json`.
- Add soko apply to copy a file into many repos at once, with a diff preview and dry-run-by-default safety
- Add a stable error_code field to per-repo JSON failures in pull, fetch, exec, clean, and stash
- Add a global --perf flag (and SOKO_PERF) reporting per-repo and aggregate timing for status, fetch, pull, and exec
- soko grep now highlights the matched substring within each result line

### Chores

- Document soko snapshot, soko apply, the soko ui dashboard keys, worktree tag inheritance, the global --perf flag, and JSON error_code in the README

## v0.20.0

### Features

- Add a persistent `--quiet`/`-q` flag (and `SOKO_QUIET` env) that suppresses info lines, hints, progress, and summary footers while leaving tables, errors, exit codes, and `--json` untouched
- Add `soko annotate` to attach freeform metadata (owner, status, priority, note, or any key) to a repo, and filter `soko list` / `soko status` by it with `--meta key=value`
- Suggest the closest registered repo name when `cd`, `go`, `open`, or `remove` is given a name that matches nothing — `no repo matching: autth — did you mean: auth?`
- Give `config path`, `config get`, and `config set` a `--json` contract, and add `config list` to dump the effective configuration as a table or JSON
- Add `soko remotes` to show each repo's origin URL and upstream tracking, flagging repos with no remote or no upstream; supports `--tag`, `--missing-upstream`, and `--json`
- Add `soko grep` to run `git grep` across all (or tag/repo-filtered) repos in parallel, grouped by repo, with `-i`/`--regexp`/`--files-only`/`--no-worktrees` and `--json`
- Add `--select` to `clean`, `prune`, and `remove --all` to interactively pick which repos the destructive operation touches before confirming (narrow-only, TTY-gated)
- Add soko health to rank repos by an urgency score, most neglected first
- Add `soko pull` to pull all (or specific) registered repos in parallel — fast-forward only by default, with `--rebase`, `--tag`, and `--no-worktrees` flags. Branches with no upstream are skipped rather than reported as failures.

### Chores

- Document the remotes, annotate, and stats commands, the `--quiet`/`SOKO_QUIET`, `--meta`, `--missing-upstream`, and annotate flags, and `config list`/`--json`; correct the source-build Go version to 1.26+
- Git-ignore the local assessment and improvement-proposal docs (`docs/ASSESSMENT.md`, `docs/improvements/`), matching the existing `docs/features/` planning-doc convention

## v0.19.0

### Features

- Accept an optional filter argument on soko go to pre-narrow the picker by name
- Automatic repo discovery with `soko discover` — opt in with `soko discover on` and repos register themselves the first time you cd into them, no `soko scan` needed. Driven by the shell hook (fires on directory change); scope it with `--root`, apply `--tag`s, and skip paths with `--ignore`. Skips submodules, the home directory, `node_modules`/`vendor`, and non-interactive shells. `soko doc` reports discovery status
- Prune deleted repos with `soko prune` — remove registry entries whose directories no longer exist on disk (cascading to their linked worktrees), with `--dry-run`/`--force`/`--json` and `--tag` filtering. `status` and `list` now warn when registered repos have gone missing
- Workspace stats with `soko stats` — aggregate repo counts, sizes, and commit totals alongside health signals (dirty, behind, stale branches, no remote) and 30-day activity

## v0.18.1

### Features

- Show a live progress counter during `soko fetch` so users can see how many repos have been fetched

## v0.18.0

### Bug Fixes

- Resolve symlinks in `init` to prevent duplicate registrations when paths differ from `scan`
- Exit with non-zero code when `fetch --json` encounters failures, consistent with table output
- Require `--force` or `--dry-run` when using `clean --json` to prevent interactive prompts in JSON output
- Output empty JSON array instead of plain text when `stash pop --json` finds no stashes
- Accept repo name arguments in `fetch` for filtering, consistent with `status` and `diff`
- Reject positional arguments to `shell-init` with a helpful hint (e.g. `did you mean --fish?`)
- Return non-zero exit code when `open` fails and format errors as JSON when `--json` is set
- Reject negative and zero values for `report --days` with a clear error message
- Reject alias names that shadow built-in commands instead of silently creating dead aliases
- Add column headers and separator line to `soko list --group` output

## v0.17.6

### Bug Fixes

- Fix PATH column alignment in `soko list` when some repos have tags and others don't

## v0.17.5

### Bug Fixes

- Add missing shell completions: repo name completion for status, diff, stash, report, clean positional args and tag completion for init, scan --tag flags

## v0.17.3

### Features

- User-defined command aliases with `soko alias set/list/remove` — shortcuts stored in config that expand transparently when invoked
- Delete merged branches across repos with `soko clean` — detects default branch, confirms before deleting, supports `--dry-run`, `--prune`, and `--force`
- Activity report with `soko report` — summarize commit history across repos with tree-style output, timestamps, branch info, and author filtering
- Positional repo args for `soko status`, `soko diff`, and `soko stash` — filter to specific repos by name or prefix match

### Bug Fixes

- `soko exec` with no command now returns an error (exit 1) instead of silently exiting 0
- Return empty JSON array `[]` instead of plain text for `--json` when diff, stash, or alias list have no results
- Show "no repos found matching" instead of misleading "tag filter" message when positional repo args match nothing
- Correct singular/plural grammar across all CLI output — "1 repo" not "1 repos", "1 branch" not "1 branchs"
- Fix release workflow: prevent shell expansion of backticks in changelog output when updating GitHub release notes

### Chores

- Code quality improvements: extracted `output.RenderJSON` helper (replacing 13 duplicate blocks), refactored large closures in scan and doc commands, consistent naming across result structs

## v0.16.0

### Features

- First-class git worktree support: `soko scan --worktrees` discovers linked worktrees, `soko init --worktree` registers them, and all commands handle worktree entries with `--no-worktrees` filtering
- Stash and restore uncommitted changes across all repos with `soko stash` and `soko stash pop`

### Bug Fixes

- Handle git worktrees correctly: `soko init` resolves to main repo, `soko scan` skips linked worktrees
- Security hardening: checksum verification in install script, pinned CI actions to commit SHAs, symlink protection on nav file, URL validation for browser open, git_path executable validation, restrictive directory permissions

## v0.15.0

### Features

- Add winget package support for Windows users

## v0.14.0

### Features

- One-line install script for macOS and Linux with auto-detection

## v0.13.0

### Features

- Cross-platform builds and package manager distribution via GoReleaser
- Type to search in the interactive repo picker
- Scrolling viewport for picker and auto-truncation for status

## v0.12.0

### Features

- Scrolling viewport for picker and auto-truncation for status with --all override

## v0.11.0

### Features

- Type to search in the interactive repo picker
- Show truncated last commit message in status table
- Group status output by tag with --group flag

### Bug Fixes

- Add column headers to grouped status output

## v0.10.0

### Features

- Show file-level uncommitted changes across repos with soko diff
- Show truncated last commit message in status table

## v0.9.0

### Features

- Open repos in the browser with support for PRs, issues, actions, and more

## v0.8.0

### Features

- PowerShell shell-init hook and Windows-aware config paths

### Bug Fixes

- Fix PowerShell nav hook path construction and prompt guard
- Platform-aware shell hints and fish nav path fix
- Config edit defaults to notepad on Windows, doc checks PowerShell profile

## v0.7.0

### Features

- Auto-discover and register all git repos in a directory with soko scan

## v0.6.1

### Chores

- Extract shared helpers, fix swallowed errors, tighten config permissions, document concurrency patterns

## v0.6.0

### Features

- Add soko config set and soko config get commands
- Configurable git binary path via soko config set git_path

## v0.5.0

### Features

- View config path or open in editor with soko config
- Doc checks if shell-init is configured
- Show which command is being run in soko exec output
- Fish shell support via soko shell-init --fish
- Interactive repo picker with arrow-key navigation
- Tree view for soko list --group grouped by tags
- Show tags column in soko list when repos have tags
- Shell hook for direct navigation with soko go and soko cd
- Tag commands detect current repo from working directory

### Bug Fixes

- Include tags in soko list --json output
- Unified output style with dimmed headers and consistent summaries
- Fix picker colors when stdout is piped
- Show error messages on stderr instead of silent exit

## v0.4.0

### Features

- Tag repos with labels and filter any command with --tag
- Add --fetch flag to status for accurate ahead/behind counts

## v0.3.0

### Features

- Health check for soko setup with auto-fix for stale entries
- Run arbitrary commands across all registered repos with soko exec
- Tab-complete repo names in soko remove and soko cd

### Chores

- Remove unused internal/repo/ package
- Remove CLAUDE.md from git tracking

## v0.2.0

### Features

- Print repo path for quick navigation with prefix matching
- Fetch all registered repos in parallel with optional --prune
- Add integration tests exercising all CLI commands end-to-end
- Remove repos from the registry by name, path, or clear all
- Filter status with --dirty, --clean, --ahead, --behind flags

### Bug Fixes

- Fix golangci-lint v2 configuration for CI compatibility
- Fix index out of range panic when status filters reduce result count

## v0.1.1

### Bug Fixes

- Fix golangci-lint v2 configuration for CI compatibility

### Chores

- Add CI and automated release GitHub Actions workflows

## v0.1.0

### Features

- Register git repos in a global config with soko init.
- Global --json flag for machine-readable output on status and list.
- List all registered repos with soko list.
- Show status of all registered repos with parallel collection.
- Print build version with soko version.
