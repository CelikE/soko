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
- **Go 1.22+** — only needed if installing from source or via `go install`

## Install

```bash
# Quick install (macOS / Linux)
curl -fsSL https://raw.githubusercontent.com/CelikE/soko/master/install.sh | sh

# Homebrew (macOS / Linux)
brew install CelikE/tap/soko

# Windows (Scoop)
scoop bucket add soko https://github.com/CelikE/homebrew-tap
scoop install soko

# From source (requires Go 1.22+)
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
| `soko init` | Register the current git repo |
| `soko scan` | Discover and register all git repos in a directory |
| `soko status` | Show status of all registered repos |
| `soko diff` | Show uncommitted file changes across repos |
| `soko list` | List all registered repos |
| `soko remove` | Remove a repo from the registry |
| `soko fetch` | Fetch all registered repos in parallel |
| `soko cd` | Navigate to a repo by name |
| `soko go` | Interactive repo picker |
| `soko exec` | Run a command in all registered repos |
| `soko open` | Open a repo in the browser |
| `soko tag` | Manage repo tags |
| `soko doc` | Check the health of your soko setup |
| `soko config` | View config path or open in editor |
| `soko shell-init` | Print shell integration hook |
| `soko version` | Print the soko version |

## Flags

| Flag | Scope | Description |
|------|-------|-------------|
| `--json` | Global | Output in JSON format |
| `--fetch` | `status` | Fetch from remotes before showing status |
| `--dirty` | `status` | Show only repos with uncommitted changes |
| `--clean` | `status` | Show only clean repos in sync with remote |
| `--ahead` | `status` | Show only repos ahead of remote |
| `--behind` | `status` | Show only repos behind remote |
| `--tag` | `init`, `scan`, `status`, `list`, `fetch`, `exec`, `go` | Filter by tag (repeatable, combines with OR) |
| `--dry-run` | `scan` | Show repos that would be registered without registering |
| `--depth` | `scan` | Maximum directory depth to scan (default: 5) |
| `--group` | `status`, `list` | Group repos by tag in a tree view |
| `--all` | `status` | Show all repos without truncation |
| `--prune` | `fetch` | Pass `--prune` to git fetch to clean up stale refs |
| `--seq` | `exec` | Run sequentially instead of in parallel |
| `--prs` | `open` | Open pull/merge requests page |
| `--issues` | `open` | Open issues page |
| `--actions` | `open` | Open CI/CD page |
| `--branches` | `open` | Open branches page |
| `--settings` | `open` | Open settings page |
| `--fix` | `doc` | Auto-fix issues (remove stale paths) |
| `--fish` | `shell-init` | Output fish shell syntax |
| `--pwsh` | `shell-init` | Output PowerShell syntax |

## Usage examples

### Status with filters

```bash
soko status                         # all repos
soko status --fetch                 # fetch first, then show status
soko status --dirty                 # only repos with uncommitted changes
soko status --tag backend --behind  # only backend repos behind remote
soko status --json                  # machine-readable output
```

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

### Run commands across repos

```bash
soko exec -- git pull --rebase      # pull all repos
soko exec -- git stash              # stash everything
soko exec -- make test              # run tests everywhere
soko exec --seq -- git log -1       # sequential, one at a time
soko exec --tag backend -- go vet   # only in backend repos
```

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
```

### Health check and config

```bash
soko doc                            # check paths, git, remotes, shell-init
soko doc --fix                      # auto-remove stale entries
soko config path                    # print config file location
soko config edit                    # open config in $EDITOR
soko config set git_path /usr/local/bin/git  # use a custom git binary
soko config get git_path            # check current git binary
```

## Configuration

soko stores registered repos in a single YAML file:

```
~/.config/soko/config.yaml
```

Respects `$XDG_CONFIG_HOME` if set. The format is minimal:

```yaml
repos:
  - name: auth-service
    path: /home/dev/work/auth-service
    tags:
      - backend
      - go
  - name: frontend
    path: /home/dev/work/frontend
    tags:
      - frontend
```

Tags are optional — repos without tags work the same as before.

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
