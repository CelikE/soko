<p align="center">
  <img src="docs/soko-banner.svg" alt="soko — All your repos, one command." width="600">
</p>

<p align="center">
  <strong>All your repos, one command.</strong>
</p>

---

soko (倉庫 — "storehouse") is a fast, lightweight CLI for managing multiple git repositories. Register your repos once, then see the status of all of them from anywhere with a single command. No more `cd`-ing between directories and running `git status` one at a time.

## Prerequisites

- **Git** — soko shells out to `git` for all repository operations
- **Go 1.26+** — only needed if installing from source or via `go install`

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

# From source (requires Go 1.26+)
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

## Commands

| Command | Description |
|---------|-------------|
| `soko init` | Register the current git repo (detects worktrees) |
| `soko scan` | Discover and register all git repos in a directory |
| `soko discover` | Auto-register repos as you cd into them (opt-in) |
| `soko status [repos...]` | Show status of all (or specific) repos |
| `soko remotes [repos...]` | Show each repo's remotes + upstream tracking, flag misconfig |
| `soko diff [repos...]` | Show uncommitted file changes across repos |
| `soko stash [repos...]` | Stash/pop uncommitted changes across repos |
| `soko clean [repos...]` | Delete merged branches across repos |
| `soko list` | List all registered repos |
| `soko remove` | Remove a repo from the registry |
| `soko prune` | Remove repos whose directories no longer exist |
| `soko fetch [repos...]` | Fetch all (or specific) registered repos in parallel |
| `soko pull [repos...]` | Pull all (or specific) registered repos in parallel |
| `soko sync [repos...]` | Fetch all repos, fast-forward the safe ones, report the rest |
| `soko ctx` | Save and restore workspace contexts (branches + stashes) |
| `soko worktree` | Create, list, and remove git worktrees with registry bookkeeping |
| `soko branch [name]` | Current branch per repo, or where a branch exists; `switch`/`stale` subcommands |
| `soko cd` | Navigate to a repo by name |
| `soko go` | Interactive repo picker |
| `soko exec` | Run a command in all registered repos |
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

## Flags

| Flag | Scope | Description |
|------|-------|-------------|
| `--json` | Global | Output in JSON format |
| `--quiet`, `-q` | Global | Suppress hints, progress, and summary lines (also via `SOKO_QUIET`) |
| `--fetch` | `status` | Fetch from remotes before showing status |
| `--dirty` | `status` | Show only repos with uncommitted changes |
| `--clean` | `status` | Show only clean repos in sync with remote |
| `--ahead` | `status` | Show only repos ahead of remote |
| `--behind` | `status` | Show only repos behind remote |
| `--missing-upstream` | `remotes` | Show only repos with no remote or no upstream |
| `--tag` | `init`, `scan`, `status`, `remotes`, `diff`, `stash`, `list`, `fetch`, `pull`, `sync`, `branch`, `exec`, `grep`, `open`, `report`, `stats`, `health`, `clean`, `prune`, `go`, `discover on` | Filter by tag (repeatable, combines with OR) |
| `--meta` | `list`, `status` | Filter by metadata `key=value` (repeatable, combines with AND) |
| `--root` | `discover on` | Restrict auto-discovery to repos under these directories (repeatable) |
| `--ignore` | `discover on` | Glob patterns of paths to skip during auto-discovery (repeatable) |
| `--worktree` | `init` | Register as a linked worktree instead of resolving to main repo |
| `--worktrees` | `scan` | Also discover and register linked git worktrees |
| `--no-worktrees` | `fetch`, `pull`, `sync`, `exec`, `grep` | Skip worktree entries, only operate on parent repos |
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
| `--all` | `status` | Show all repos without truncation |
| `--prune` | `fetch`, `clean` | Prune stale remote tracking refs |
| `--force` | `remove`, `clean`, `prune` | Skip confirmation prompt |
| `--select` | `clean`, `prune`, `remove --all` | Open the interactive picker to choose exactly which repos the operation touches (requires a TTY) |
| `--set` | `annotate` | Set a metadata `key=value` (repeatable) |
| `--unset` | `annotate` | Remove a metadata key (repeatable) |
| `--clear` | `annotate` | Remove all metadata from a repo |
| `--list` | `annotate` | List every repo that has metadata |
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

soko ctx switch client-a            # restore branches, pop the stashes
soko ctx list                       # saved contexts with age and stash counts
soko ctx show client-a              # per-repo branch + stash detail
soko ctx drop client-a              # forget it (stashes stay in their repos)
```

`save` stashes only dirty repos (including untracked files) under a
per-context stash message, so unrelated stashes are never touched. `switch`
refuses to touch any repo that is dirty *right now* — save the current state
under another name first. A stash that was popped manually degrades to a
note, never an error. Contexts live in the same config file as the registry.

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
```

Metadata lives in the same `~/.config/soko/config.yaml` registry and survives
`soko scan` / `soko prune`. A repo with no annotations writes no `meta:` block,
so existing configs are untouched.

### Aliases

```bash
soko alias set morning "sync --tag work"          # create an alias
soko alias set deploy "exec --tag prod -- make deploy"
soko alias list                                    # show all aliases
soko alias remove morning                          # remove an alias

soko morning                                       # runs soko sync --tag work
soko deploy                                        # runs soko exec --tag prod -- make deploy
```

Built-in commands always take priority over aliases.

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

### Run commands across repos

```bash
soko exec -- git pull --rebase      # pull all repos
soko exec -- git stash              # stash everything
soko exec -- make test              # run tests everywhere
soko exec --seq -- git log -1       # sequential, one at a time
soko exec --tag backend -- go vet   # only in backend repos
```

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
```

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
soko open                           # current repo homepage
soko open auth-service              # by name
soko open --prs                     # pull/merge requests
soko open --issues                  # issues
soko open --actions                 # CI/CD
soko open --tag backend             # open all backend repos
soko open --tag backend --prs       # PRs for all backend repos
```

Supports GitHub, GitLab, and Bitbucket — auto-detects the platform from the remote URL.

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
soko clean --select                # pick which repos to clean before deleting
soko clean --prune                 # also prune stale remote refs
soko clean --tag backend           # only backend repos
soko clean auth                    # specific repo
```

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

# Status works per-worktree
soko status --dirty

# Remove a parent — linked worktrees are removed too
soko remove api

# Skip worktrees for bulk operations
soko fetch --no-worktrees
soko exec --no-worktrees -- git pull
```

Without `--worktrees`, soko detects when you're in a worktree and registers the main repo instead — no duplicates.

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
remote-only ones. A dirty repo is refused and left untouched while the others
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

## Building from source

```bash
git clone https://github.com/CelikE/soko.git
cd soko
make build
make test
```

## Dependencies

soko is a single binary with minimal dependencies. No CGo, no git libraries — it shells out to the `git` CLI directly.

| Dependency | Purpose |
|-----------|---------|
| [spf13/cobra](https://github.com/spf13/cobra) | CLI framework |
| [gopkg.in/yaml.v3](https://github.com/go-yaml/yaml) | Config file parsing |
| [fatih/color](https://github.com/fatih/color) | Terminal colors (respects `NO_COLOR`) |
| [golang.org/x/sync](https://pkg.go.dev/golang.org/x/sync) | Parallel execution |
| [golang.org/x/term](https://pkg.go.dev/golang.org/x/term) | Interactive picker (raw terminal) |

## License

[MIT](LICENSE)
