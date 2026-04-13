# Changelog

## Unreleased

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
