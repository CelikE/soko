# CLAUDE.md — soko

## Project overview

soko (倉庫 — "storehouse") is a fast, zero-dependency CLI written in Go for managing
multiple git repositories. It gives developers instant visibility and control across all
their repos from a single command.

One-liner: **"All your repos, one command."**

### Problem

Developers working with microservices or multi-project setups have 5–50 git repos.
Today they `cd` between them, run `git status` one at a time, and lose track of which
repos have uncommitted changes or are behind remote. soko replaces all of that with
a single binary and a global registry.

### How it works

1. `soko init` — run inside a git repo to register it in `~/.config/soko/config.yaml`
2. `soko status` — run from anywhere to see the status of ALL registered repos

---

## Build and run

```bash
# Build
go build -o soko ./cmd/soko

# Run
./soko init
./soko status

# Install locally
go install ./cmd/soko
```

### Makefile targets

```bash
make build       # Build the binary
make test        # Run all tests
make lint        # Run golangci-lint
make clean       # Remove build artifacts
```

### Prerequisites

- Go 1.22+
- `git` on PATH
- `golangci-lint` for linting
- `chagg` for release management

---

## Testing

```bash
# Run all tests
go test ./...

# Run tests with race detection
go test -race ./...

# Run tests for a specific package
go test ./internal/git/...

# Run a specific test
go test ./internal/git/... -run TestParseStatusPorcelainV2
```

### Testing guidelines

- **Table-driven tests** — every test function should use the `tests := []struct{...}` pattern
- **Unit test git parsers** with hardcoded porcelain v2 strings; never shell out to real git in unit tests
- **Unit test config** loading and saving with temporary files (`t.TempDir()`)
- **Integration tests** may use `git init` in temp directories when testing real git interaction
- **Test file placement** — tests live next to the code they test (`foo_test.go` beside `foo.go`)
- **No test helpers in production packages** — shared test utilities go in `internal/testutil/` if needed
- **Assertions** — use stdlib `testing` only; no assertion libraries

---

## Linting

```bash
golangci-lint run
```

Configuration lives in `.golangci.yml` at the project root. Fix all lint issues before
committing. Do not disable linters with `//nolint` directives unless there is a documented
reason in a comment next to the directive.

---

## Project structure

```
soko/
├── cmd/soko/main.go           # Minimal entrypoint, delegates to root command
├── internal/
│   ├── cli/                   # Cobra command definitions (thin wrappers)
│   │   ├── root.go            # Root cobra command, global flags
│   │   ├── init.go            # soko init
│   │   └── status.go          # soko status
│   ├── config/                # Config loading, saving, path resolution
│   │   └── config.go          # Config struct, Load(), Save(), RepoExists()
│   ├── repo/                  # Repo type and status collection
│   │   ├── repo.go            # Repo struct
│   │   └── status.go          # Status struct, CollectStatus()
│   ├── git/                   # Low-level git CLI wrapper
│   │   ├── git.go             # Run() — exec git with args, capture output
│   │   └── status.go          # Parse porcelain v2 status output
│   └── output/                # Table renderer and color helpers
│       ├── table.go           # Render status table
│       └── color.go           # Color helpers, respects NO_COLOR
├── .changes/                  # chagg change entry files (see Release section)
├── go.mod
├── go.sum
├── Makefile
├── .golangci.yml
└── README.md
```

### Structural rules

These rules are **non-negotiable**. Every change must respect them.

1. **`internal/git/` is the ONLY package that calls `exec.Command("git", ...)`.**
   Everything else works with parsed Go types. If you need git data in another package,
   add a function to `internal/git/` and call it from there.

2. **`internal/cli/` commands are thin wrappers.**
   The pattern is: parse flags → load config → call business logic → render output.
   No business logic lives in CLI files. No `fmt.Println` for user-facing output.

3. **`internal/output/` owns all terminal rendering.**
   Commands build result structs and pass them to a renderer. Commands never call
   `fmt.Println`, `fmt.Fprintf`, or color functions directly.

4. **Config file operations live in `internal/config/`.**
   Read/write YAML, path resolution, XDG compliance — all here.

5. **Everything under `internal/`.**
   This is a CLI, not a library. Nothing is importable by external consumers.

---

## Code style

Follow the **Uber Go Style Guide** strictly:
https://github.com/uber-go/guide/blob/master/style.md

### Non-negotiable rules

- **Doc comments** — all exported types and functions must have doc comments
- **Error messages** — lowercase, no punctuation: `fmt.Errorf("loading config: %w", err)`
- **Error wrapping** — always use `%w` with context: `fmt.Errorf("doing thing: %w", err)`
- **Context** — use `context.Context` as the first parameter for all operations that may be cancelled or run concurrently
- **No panics** — prefer returning errors over panicking; panics are only acceptable in `main()` or test helpers
- **Import grouping** — three groups separated by blank lines:
  ```go
  import (
      "context"
      "fmt"

      "github.com/spf13/cobra"

      "github.com/CelikE/soko/internal/config"
  )
  ```
- **Struct literals** — always use field names, never positional:
  ```go
  // Good
  repo := Repo{Name: "foo", Path: "/bar"}

  // Bad
  repo := Repo{"foo", "/bar"}
  ```
- **Variable naming** — short, clear names; receivers are one or two letters (`r` for `Repo`, `c` for `Config`)
- **No naked returns** — always name what you are returning

### Dependencies

Minimal and intentional. The full list for v0.1:

| Dependency                      | Purpose            |
|---------------------------------|--------------------|
| `spf13/cobra`                   | CLI framework      |
| `gopkg.in/yaml.v3`             | Config file parsing |
| `fatih/color`                   | Terminal colors    |
| `olekukonern/tablewriter`       | Table rendering    |
| `golang.org/x/sync/errgroup`   | Parallel execution |

Do not add dependencies without explicit approval. No CGo. No git libraries (go-git, etc.).

---

## Git interaction

soko shells out to the `git` CLI. This is a deliberate decision:

- 100% compatibility with whatever git version the user has
- No CGo dependencies
- Users can debug by running the same git commands

### Commands used

| Command | Purpose |
|---------|---------|
| `git status --porcelain=v2 --branch` | Branch info + file status |
| `git log -1 --format=%ct` | Last commit timestamp (unix) |
| `git rev-parse --show-toplevel` | Confirm we are in a git repo |
| `git remote get-url origin` | Get repo name from remote URL |

### Porcelain v2 format reference

```
# branch.oid <commit>
# branch.head <branch>
# branch.upstream <upstream>
# branch.ab +<ahead> -<behind>
1 .M N... 100644 100644 100644 <hash> <hash> <path>     # modified
? <untracked-path>                                        # untracked
```

All git output parsing lives in `internal/git/status.go`. When adding support for new
git output formats, add parser functions here and unit test them with hardcoded strings.

---

## Config file

Location: `~/.config/soko/config.yaml`

Respects `$XDG_CONFIG_HOME` if set (uses `$XDG_CONFIG_HOME/soko/config.yaml`).

```yaml
repos:
  - name: auth-service
    path: /home/dev/work/auth-service
  - name: backend-api
    path: /home/dev/work/backend-api
```

Keep the config file minimal. No nesting beyond what is shown above.

---

## Output format

### Status table

```
  REPO               BRANCH       STATUS     ↑↓        LAST COMMIT
  ──────────────────────────────────────────────────────────────────
  auth-service        feat/sso     ✎ 3M       ↑2        2h ago
  backend-api         master         ✓ clean    ↓3        1d ago
  frontend            dev          ✎ 1M 2U    ↑1        4h ago
  shared-lib          master         ✓ clean    ·         3d ago

  4 repos │ 2 dirty │ 1 behind remote │ 6 uncommitted changes
```

### Color rules

| Color  | Meaning |
|--------|---------|
| Green  | Clean repo, in sync with remote |
| Yellow | Dirty (uncommitted changes) or ahead of remote |
| Red    | Conflicts or significantly behind remote |

### Status symbols

| Symbol | Meaning |
|--------|---------|
| `✓`    | Clean working tree |
| `✎`    | Has modifications |
| `3M`   | 3 modified files |
| `2U`   | 2 untracked files |
| `1D`   | 1 deleted file |
| `↑2`   | 2 commits ahead of remote |
| `↓3`   | 3 commits behind remote |
| `·`    | In sync with remote |

Respect `NO_COLOR` environment variable. When set, disable all color output.
Support `--json` flag for machine-readable output.

---

## Versioning

This project follows [Semantic Versioning](https://semver.org/) (semver): `MAJOR.MINOR.PATCH`.

### Version components

| Component | Format | When to bump | Example |
|-----------|--------|--------------|---------|
| **MAJOR** | `X.0.0` | Breaking changes — removing commands, renaming flags, changing config format, altering default behavior in ways that break existing usage | `0.x.x` → `1.0.0` |
| **MINOR** | `0.X.0` | New features — new commands, new flags, new output formats, non-breaking enhancements | `0.1.0` → `0.2.0` |
| **PATCH** | `0.0.X` | Bug fixes and hotfixes — correcting broken behavior, fixing edge cases, typo fixes in user-facing output | `0.1.0` → `0.1.1` |

### Pre-1.0 convention

While the project is pre-1.0 (`0.x.x`), the API is not considered stable. Minor versions
may include small breaking changes if necessary, but should still be documented clearly.

### What bumps what — quick reference

| Change | Bump | chagg `--bump` |
|--------|------|----------------|
| New command (`soko list`) | MINOR | `minor` |
| New flag (`--json` on status) | MINOR | `minor` |
| New output column or status symbol | MINOR | `minor` |
| Bug fix (wrong exit code, bad parsing) | PATCH | `patch` |
| Fix crash or panic | PATCH | `patch` |
| Config format change (rename/remove fields) | MAJOR | `major` |
| Remove or rename a command or flag | MAJOR | `major` |
| Internal refactor (no user-facing change) | none | no entry needed |
| Dependency update (no user-facing change) | none | no entry needed |

### When to release

Claude should autonomously determine when a release is appropriate and execute it using
`chagg`. The user will not tell you when to release — use your judgement based on these
guidelines:

- **PATCH release** — immediately after a bug fix lands on `master`. Don't batch bug fixes; ship them fast. If you just merged a fix, release it.
- **MINOR release** — after a feature branch is merged to `master` and the feature is complete. One feature = one minor release. If you just finished implementing and merging a feature, release it.
- **MAJOR release** — only when breaking changes have been merged. These are rare and significant. If you introduced a breaking change, release it, but flag it to the user.

### How to decide

After merging a PR to `master`, check what changed:

1. Run `chagg log` to see what unreleased entries exist
2. If there are unreleased entries, it is time to release
3. Determine the bump level from the entries (chagg tracks this via `--bump` on each entry)
4. Execute the release:
   ```bash
   chagg check
   chagg generate > CHANGELOG.md
   git add CHANGELOG.md && git commit -m "chore: update changelog"
   chagg release --push
   ```
5. Report the new version to the user after the release is done

Do not ask the user for permission to release. Just do it when the criteria above are met.

---

## Release workflow — chagg

We use [chagg](https://chagg.dev) for changelog management and releases. chagg collects
individual change entry files in a `.changes/` directory throughout development, then
generates a changelog and creates version tags at release time.

### Setup (one-time)

```bash
chagg init
```

This creates the `.changes/` directory in the project root.

### During development — adding change entries

Every user-facing change (feature, fix, breaking change) **must** include a chagg entry.
Add one as part of your PR:

```bash
# Interactive — prompts for type, bump level, and description
chagg add feat__short-description

# Non-interactive — for scripting or when you know exactly what to write
chagg add feat__oauth-login --body "Add OAuth login support." --no-prompt

# With explicit bump level
chagg add fix__config-path --type fix --bump patch --body "Fix XDG config path resolution." --no-prompt

# Grouped entry (group/type__slug)
chagg add cli/feat__json-output --body "Add --json flag to status command." --no-prompt

# Body from stdin (for longer descriptions)
echo "Detailed description here" | chagg add feat__parallel-status --body - --bump minor
```

#### Naming convention

Entry filenames follow the pattern: `[group/]type__slug`

- **group** (optional): logical area (e.g., `cli`, `config`, `git`)
- **type**: the kind of change (`feat`, `fix`, `refactor`, `docs`, `chore`)
- **slug**: short kebab-case description

#### When to add an entry

- New feature or command → `feat` with `--bump minor`
- Bug fix → `fix` with `--bump patch`
- Breaking change → entry with `--bump major`
- Internal refactor with no user-facing impact → no entry needed
- Documentation-only changes → `docs` (optional, `--bump patch`)
- Dependency updates → `chore` (optional, `--bump patch`)

### Before releasing — validate and preview

```bash
# Validate all entries are well-formed
chagg check

# Preview what the next release will look like
chagg log
```

`chagg check` must pass before any release. Run it in CI on every PR.

### Releasing

```bash
# Generate changelog markdown
chagg generate > CHANGELOG.md

# Create the version tag (dry-run first)
chagg release --dry-run

# Create the version tag for real
chagg release

# Create tag and push to remote in one step
chagg release --push
```

### Machine-readable output

All chagg commands support `--format json` for automation:

```bash
chagg log --format json | jq '.next_tag'
chagg check --format json | jq '.summary'
VERSION=$(chagg release --dry-run --version-only)
```

### Exit codes

| Code | Meaning |
|------|---------|
| 0    | Success |
| 1    | General errors (I/O, git failures) |
| 2    | Validation errors (malformed entries, invalid flags) |
| 3    | Conflicts (existing tags, uncommitted changes) |

### CI integration

- Run `chagg check` on every PR to validate change entries
- Run `chagg generate` + `chagg release --push` on merge to master for releases

---

## Branching strategy

Every feature, fix, or chore gets its own branch off `master`. Never commit directly to `master`.

### Branch naming

```
feat/short-description
fix/short-description
chore/short-description
refactor/short-description
test/short-description
docs/short-description
```

Examples:

```
feat/init-command
fix/xdg-config-path
chore/update-cobra
refactor/git-parser
```

### Creating a branch

```bash
git checkout master
git pull origin master
git checkout -b feat/my-feature
```

---

## Commit conventions

Keep commits simple. Each commit has a **header** and an optional **description** separated
by a blank line. The header says what changed. The description says why or adds context
when the header alone isn't enough.

### Format

```
<header>

<description>
```

- **Header** — short (≤72 chars), imperative mood, lowercase after prefix
- **Description** — optional, wrap at 72 chars, explain motivation or context

### Prefix

Start the header with a type prefix:

- `feat:` — new feature
- `fix:` — bug fix
- `refactor:` — restructuring without behavior change
- `test:` — adding or updating tests
- `chore:` — dependency updates, tooling, CI
- `docs:` — documentation only

### Examples

Simple (no description needed):

```
fix: handle missing remote in init
```

With context:

```
feat: add parallel status collection

Use errgroup to query all registered repos concurrently.
Significantly reduces wall-clock time when many repos are registered.
```

---

## Pull requests

When a feature branch is complete, create a PR back to `master`.

### Before creating

- All tests pass: `go test ./...`
- Linter is clean: `golangci-lint run`
- chagg entry added and valid: `chagg check` (if the change is user-facing)
- Branch is up to date with `master`

### Creating the PR

Use `gh pr create` with proper labels and assignee:

```bash
gh pr create \
  --title "Short description of the change" \
  --body "$(cat <<'EOF'
## Summary
What this PR does and why.
EOF
)" \
  --label "<label>" \
  --assignee "@me"
```

PR descriptions should only contain a **Summary** section. Do not include a "Test plan",
"Checklist", or any other sections. Keep it short — a few bullet points explaining what
changed and why is enough.

### Labels

Apply one type label per PR:

| Label         | When to use                                  |
|---------------|----------------------------------------------|
| `feature`     | New functionality                            |
| `bug`         | Bug fix                                      |
| `refactor`    | Code restructuring, no behavior change       |
| `chore`       | Dependencies, tooling, CI                    |
| `docs`        | Documentation only                           |
| `test`        | Test additions or improvements               |

Add additional labels when relevant:

| Label         | When to use                                  |
|---------------|----------------------------------------------|
| `breaking`    | Breaking change to CLI interface or config   |
| `blocked`     | PR is waiting on something else              |

### Assignee

Always assign the developer who worked on the branch using `--assignee "@me"`.
If pairing or handing off, assign the person who will shepherd it to merge.

### PR scope

- One logical change per PR — don't bundle unrelated work
- Every PR that changes user-facing behavior must include a `chagg add` entry

---

## Development workflow summary

```bash
# 1. Create a feature branch
git checkout master && git pull origin master
git checkout -b feat/my-feature

# 2. Make your changes
#    (edit files in internal/...)

# 3. Run tests
go test ./...

# 4. Run linter
golangci-lint run

# 5. Add a chagg entry if the change is user-facing
chagg add feat__my-feature --body "Description of the change." --no-prompt

# 6. Validate the entry
chagg check

# 7. Commit
git add .
git commit -m "feat: my feature description"

# 8. Push and create PR
git push -u origin feat/my-feature
gh pr create --title "Add my feature" --label "feature" --assignee "@me"
```
