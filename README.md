<p align="center">
  <img src="soko-banner.svg" alt="soko — All your repos, one command." width="600">
</p>

<p align="center">
  <strong>All your repos, one command.</strong>
</p>

---

soko (倉庫 — "storehouse") is a fast, zero-dependency CLI for managing multiple git repositories. Register your repos once, then see the status of all of them from anywhere with a single command. No more `cd`-ing between directories and running `git status` one at a time.

## Quick start

```bash
# Install
go install github.com/CelikE/soko/cmd/soko@latest

# Register your repos
cd ~/projects/auth-service && soko init
cd ~/projects/backend-api  && soko init
cd ~/projects/frontend     && soko init

# See everything at a glance
soko status
```

```
  REPO               BRANCH       STATUS       ↑↓         LAST COMMIT
  ────────────────────────────────────────────────────────────────────────
  auth-service       feat/sso     ✎ 3M         ↑2         2h ago
  backend-api        main         ✓ clean      ↓3         1d ago
  frontend           dev          ✎ 1M 2U      ↑1         4h ago

  3 repos │ 2 dirty │ 1 behind remote │ 6 uncommitted changes
```

## Commands

| Command | Description |
|---------|-------------|
| `soko init` | Register the current git repo |
| `soko status` | Show status of all registered repos |
| `soko list` | List all registered repos |
| `soko version` | Print the soko version |

## Flags

| Flag | Scope | Description |
|------|-------|-------------|
| `--json` | Global | Output in JSON format (works with `status` and `list`) |

```bash
soko status --json
soko list --json
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
  - name: backend-api
    path: /home/dev/work/backend-api
```

## Building from source

```bash
git clone https://github.com/CelikE/soko.git
cd soko
make build
make test
```

## License

[MIT](LICENSE)
