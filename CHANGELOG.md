# Changelog

## Unreleased

### Maintenance

- Add end-to-end regression coverage for stale branches checked out in the current checkout or a linked worktree (issue #234).
- Reduce pull request CI to four jobs: validation, Linux race tests with coverage, and full macOS and Windows tests. Run additional cross-platform race tests weekly or manually, avoiding repeated setup and macOS runner contention from sharded jobs.

### Bug Fixes

- Return a non-zero exit status from `soko branch stale` when any repository inspection fails, preserving text reports and per-repository JSON errors.
- Preserve displayed paths for sibling directories whose names share the home directory prefix instead of incorrectly shortening them with `~`.
- Reject negative `soko branch stale --days` values even when no repositories are registered or name/tag filters match nothing; zero remains valid.
- Reject negative `soko scan --depth` values instead of silently reporting no repositories; depth zero still scans only the root.
- Reject non-directory roots in `soko scan` and report root inspection errors instead of succeeding with an empty scan.
- Resolve relative installer `--dir` destinations against the invocation directory so installed binaries survive temporary archive cleanup, including paths containing spaces.
- Ignore Git column, heading, and break presentation settings in `soko grep` so match text remains the original source line without extra column fields or NUL bytes.
- Return an empty JSON array from `soko scan --json` when no repositories are discovered, including quiet and dry-run modes, so successful empty scans remain machine-readable without creating or rewriting configuration.
- Accept native Windows executable paths in `git_path` configuration and derive repository names correctly from local Windows remotes.
- Match the complete `soko stash` message before restoring a stash, leaving manual stashes with similar messages untouched; report branch-switch fallbacks from unconfigured remote refs as having no upstream instead of claiming tracking was configured.
- Reject ambiguous repository names before removal, tagging, or metadata access in legacy or edited configs, reporting candidate paths and repair guidance instead of acting on the first match; worktrees with ambiguous parents retain only their own tags.
- Include the build's short commit SHA in `soko version` for Makefile and release builds, honoring the existing linker flags while keeping version-only output for builds without commit metadata.
- Decode picker keys across split or combined terminal reads so arrow keys over SSH/tmux neither cancel selection nor corrupt search filters in single- and multi-select modes; keep standalone Escape responsive with a short timeout.
- Report duplicate repository paths in `soko doc` as requiring manual repair, with a `soko config edit` hint, instead of advertising an automatic fix that `--fix` cannot perform.
- Replace the undo journal atomically and serialize cross-process append/pop transactions so interrupted saves preserve the previous log and concurrent commands retain recovery entries. Reject stale undo checkpoints and entry removal instead of overwriting or consuming newer history.
- Clear every wrapped terminal row when redrawing or closing single- and multi-select pickers, using the output terminal width and ANSI-aware Unicode measurement so long repository paths leave no stale rows.
- Prevent overlapping dashboard fetches from periodic ticks and repeated `r` / `R` keypresses; keep the timer armed while a fetch is running and allow retries after completion or failure.
- Preserve repository names when Git remote URLs end in one or more slashes, including `.git/`, so registration and name-based commands use the actual repository name.
- Switch only base repositories in `soko branch switch`, excluding linked worktree entries after name and tag filtering so shared branch refs are not checked out concurrently and linked checkouts stay untouched; worktree-only selections return an empty result.
- Reject combined repository-name and `--tag` selectors in `soko open` before opening browser tabs, and reject extra positional names instead of silently ignoring them.
- Print only the new worktree path from `soko worktree add` in quiet mode, including `SOKO_QUIET`, so the documented `cd "$(soko worktree add ... -q)"` workflow works with paths containing spaces.
- Align grouped status headers with their tree rows, and measure status table columns and separators by terminal display width so Unicode repository names, branches, and status symbols stay aligned.
- Replace snapshot files atomically so interrupted overwrites preserve the previous save, and show unreadable or incomplete snapshots with per-file errors in text and JSON listings instead of silently hiding them.
- Resume partial pull undo in the CLI and dashboard by recording each completed reset, skipping completed repos on retry while preserving newer work, and consuming the journal entry only after all targets are undone.
- Return a non-zero exit status when `soko clean --json` cannot delete every planned branch, report successful deletions and per-branch failures in JSON, and preserve exact undo records for successful deletions.
- Reject stale config snapshots under a short cross-process commit lock instead of silently losing completed updates; serialize concurrent discovery registrations with current policy and unique aliases, and stage config edits so conflicts preserve both the live config and editor recovery copy.
- Bind context stashes to their exact saved commits and per-worktree ownership refs, preventing linked-worktree swaps, recycled-checkout ownership, and lookup-time stash insertion from redirecting restoration. Retain stash backups, record completion in Git refs without rewriting concurrent config changes, and refuse missing or legacy ambiguous ownership before checkout.
- Restore staged, unstaged, and untracked work when `soko ctx save` cannot persist its context, preserve older stashes during forced saves, and identify recoverable stashes if automatic rollback fails.
- Make the `soko pr` result cap configurable with `--limit`, validate positive limits, and print a stderr hint when the cap is reached so text and JSON listings no longer silently omit additional pull requests.
- Report missing repository paths in `soko open` with a prune hint, and preserve actual Git errors in text and JSON output instead of diagnosing every remote lookup failure as a missing origin.
- Apply `--no-worktrees` before checking for empty selections in exec, fetch, pull, grep, sync, and apply, showing a no-repos message instead of empty tables or execution banners while preserving empty JSON arrays and quiet output.
- Only report installer checksum verification after a successful comparison, and defer installation until the complete script body has been received when piping downloads into a shell.
- Clear terminal progress by visible display width instead of byte length so ANSI colors and the multibyte bullet do not add spaces that wrap near the terminal edge.
- Support shell-style quotes and arguments in `$EDITOR` for `soko config edit`, validate edited YAML, and only confirm a save when a valid config was created or changed.
- Discover real merged branch refs in `soko clean`, excluding detached-HEAD labels and branches checked out in any worktree so cleanup previews stay accurate and deletions avoid spurious failures.
- Preserve shell-style quotes, escapes, and empty arguments in command aliases, keep appended arguments intact, and reject malformed aliases before saving or executing them.
- Restore Fish shell navigation by keeping the config path in function scope and using `$HOME/.config` when `XDG_CONFIG_HOME` is unset or empty.
- Keep repeated exact repository filters from selecting longer prefix matches, so batch commands such as `soko branch switch feat/x api web api` leave `api-v2` untouched.
- Install and verify `soko.exe` from Windows ZIP releases in Git Bash, MSYS, and Cygwin without changing release archive names.
- Cascade `soko prune --select` to linked worktrees after the picker so deselecting a worktree cannot leave it orphaned when its parent is pruned; previews and confirmation counts include every affected entry.
- Complete shell installs into protected directories by using sudo for both copying and setting executable permissions, and retry directory creation with sudo when needed.
- Report missing paths and Git failures in `soko grep --json` with per-repository errors and stable error codes, preserve successful matches, and return a non-zero exit status for incomplete searches in text and JSON output.
- Confine `soko apply` previews and atomic writes to each repository root, rejecting escaping symlinks even if a destination changes after preview while preserving relative directory symlinks within the checkout.
- Delete merged branches sequentially within each repository during `soko clean` to avoid packed-ref lock contention and spurious failures, while retaining parallel cleanup across repositories and exact branch restoration with `soko undo`.
- Report Git inspection failures in `soko stash` and `stash pop` instead of silently skipping broken repositories, and return a non-zero exit status for repository failures in text and JSON output while preserving successful operations.
- Open repository URLs through the native Windows API instead of `cmd.exe`, preventing shell metacharacters in remote URLs from executing commands while preserving literal URL content.
- Remove broken registry entries by their exact paths in `soko doc --fix`, preserving healthy repositories with the same name in legacy or hand-edited configurations.
- Cascade registry removal to linked worktrees when removing a parent by path or through `soko remove --all --select`, counting cascaded entries before confirmation and including them in text and JSON results.
- Preserve commas and quotes in `soko annotate --set` values and `soko list` / `status --meta` filters instead of splitting them as CSV, preventing rejected notes and unintended metadata changes; repeat flags for multiple keys.
- Match report authors as literal substrings, including the default Git user name, so regex characters neither hide activity nor include unrelated authors regardless of Git's pattern configuration.
- Report missing paths and Git failures in `soko diff` instead of claiming all repositories are clean; preserve successful diffs, include per-repository JSON errors, suggest pruning missing entries, and return a non-zero exit status on failures.
- Protect `soko undo` and dashboard pull undo from discarding edits or newer commits: require a clean checkout on the recorded branch at the pulled tip, use a non-clobbering reset, and refuse older journal entries without safety metadata.
- Report destination and missing-repository errors in `soko apply --write`, preserve successful writes in mixed batches, and return a non-zero exit status for planning or write failures in text and JSON output instead of claiming all repositories are in sync.
- Include untracked files in `soko stash` so untracked-only repositories are actually saved and a later `stash pop` restores the new work instead of an older stash; ignored files remain untouched.
- Return a non-zero exit status from `soko exec --json` when any repository command fails, cannot start, or has a missing path, while still emitting all per-repository results and optional timing data.
- Show final-newline changes in `soko apply` diff previews, including JSON output, with standard no-newline markers instead of an empty diff.
- Serialize terminal progress updates during parallel fetch, pull, grep, and sync operations to prevent data races, interleaved output, and backwards progress counts.
- Honor repository name and prefix filters in `soko stash pop [repos...]`, including when combined with `--tag`, so restoring a targeted stash leaves unrelated repositories untouched.
- Resolve the registered checkout root for `soko tag`, `tag add`, and `tag remove` when run from a repository or linked-worktree subdirectory.
- Preserve exact filenames in `soko grep` results, including non-ASCII characters, colons, and embedded newlines, in line and files-only output.
- Return an empty JSON array from `soko report --json` when registered repositories have no activity in the requested period.
- Count staged and unstaged file type changes as modified so `soko status` and its dirty/clean filters no longer report those repositories as clean.

### Maintenance

- Document `soko pr` usage, GitHub CLI authentication, result limits, and JSON output in the README.
- Complete the historical v0.21.0 changelog heading with its verified release date, keeping released notes separate from Unreleased.
- Shard CLI integration tests across three isolated runners per platform, run normal tests on Linux, macOS, and Windows, and collect race coverage on Linux. Run sharded macOS/Windows race checks weekly, on demand, and for concurrency-sensitive changes; retain JSON test timings and preserve Fish integration and the Windows PowerShell smoke check.
- Cover stash ownership and conflict recovery, detached-HEAD context round trips, worktree identity and parent persistence, and ambiguous or unconfigured remote branch switching with real-Git regression tests.
- Raise the minimum Go version to 1.27.2 and pin CI linting to golangci-lint v2.14.0 with Go 1.27 support.

### Upgrade Notes

- Journal writes now keep a persistent `journal.yaml.lock` sidecar; concurrent writers wait for the short journal transaction, and stale undo snapshots report a conflict instead of replacing newer history. Older binaries do not participate in this locking protocol.
- Config writes now keep a persistent `config.yaml.lock` sidecar and can fail explicitly on concurrent updates; retry the command after the competing writer finishes. `config edit` uses a private copy and retains editor files on failure instead of modifying the live config directly. Current soko writers coordinate through this protocol; older binaries and direct YAML writers do not.
- Existing stashed context records without stash commit and per-worktree ownership now require manual recovery through `git stash list`; clean context records remain compatible. Context restoration retains stash-list entries, `refs/soko/ctx-backups/<stash-oid>` backup refs, `refs/worktree/soko/ctx/<stash-oid>` ownership refs, and `refs/soko/ctx-restored/<stash-oid>` completion refs, including after a context is dropped.
- Source installs require Go 1.27.2 or newer. macOS binaries and source builds now require macOS 13 Ventura or newer; macOS 12 is no longer supported.

## v0.22.0 — 2026-10-14

### Features

- Adopt existing Git-created worktrees with `soko worktree adopt`, including dry-run, JSON, repository-family filtering, and tags for newly registered entries.
- Browse base repositories and their worktrees with `--repo <name>` or `--repo .` on list, status, go, ui, and worktree list/adopt. Group list, status, and dashboard views with `--group-by repo`; dashboard `B` toggles family grouping and `b` retains tag grouping.
- Show unregistered worktrees and Git/registry drift in `worktree list`, including locked/prunable metadata in JSON.
- Add `soko pr` to list the current repository's open pull requests through the GitHub CLI, with JSON output; dashboard `L` opens an in-app PR list with browser navigation.
- Mark dashboard repositories with space (`*` for all visible), batch-pull with one confirmed `P`, undo with `u`, fetch one repository with `R`, and copy its path with `y`.
- Search dashboard names, branches, and tags; Enter/Tab commits the filter, Escape unwinds search and filters, and `S`/`F`/`T` cycle sorting and filters backwards.
- Redesign grouped dashboard headers with repository counts and dirty/critical totals, indented rows, and aligned columns.
- Add a complete `man soko` manual, installed by packages and `install.sh`, with `make man` for source checkouts.

### Bug Fixes

- Normalize registration to the checkout root, preserve existing aliases and metadata, and allocate unique aliases for detached worktrees and separate clones of the same remote. Separate-Git-directory bases are supported when registered first; bare bases are rejected explicitly.
- Persist registrations in `scan --json`, discover families when scanning a linked checkout, and preserve registry state during dry-run.
- Isolate shell navigation requests per session and reject missing targets; PowerShell navigation respects `XDG_CONFIG_HOME`.
- Keep the dashboard cursor attached to its repository across refreshes and pin confirmed pull targets.
- Add viewport scrolling, narrow-pane width clamping, jump/paging keys, and mouse selection; fetch uses `r` and tag grouping uses `b`.
- Expire transient dashboard messages, preserve in-flight fetch indicators, pause overlapping refreshes, and show fetch age.
- Guard actions on missing repositories, show selected-repository details and worktree markers, and make column alignment and truncation display-width aware.

### Maintenance

- Verify worktree registration and PowerShell navigation in native Windows CI, and sync the Windows manifest fork with Microsoft upstream before each release.
- Retire Chagg CI and hosted release-note uploads. GoReleaser now generates GitHub-native release notes; `CHANGELOG.md` is maintained directly, and legacy `.changes` fragments remain an archive.
- Document shell-integration activation and dashboard confirmation keys, and use typed browser pages and severity constants internally.

### Upgrade Notes

- Re-evaluate `soko shell-init` or open a new shell after upgrading to activate session-scoped navigation.

## v0.21.0 — 2026-06-12

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
