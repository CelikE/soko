# CLI argument and flag contracts

This inventory records current application-command behavior for audit #354.
It is a compatibility policy, not evidence that every formerly permissive
invocation was a defect. See `soko <command> --help` for flag values and the
[JSON capability table](../README.md#json-output) for output contracts.

## Positional arguments

Names below follow `soko`. Counts exclude flags and the command/subcommand name.
`repos...` accepts zero or more repository selectors unless a required operand
precedes it. Extra repository selectors are meaningful, not surplus arguments.

| Commands | Positional contract |
|---|---|
| `init`, `list`, `pr`, `prune`, `undo`, `ui`, `health`, `stats`, `doc`, `version`, `shell-init` | None |
| `alias list`, `config path`, `config edit`, `config list`, `tag list`, `ctx list`, `snapshot list`, `worktree list` | None |
| `discover`, `discover on`, `discover off`, `discover status` | None |
| `go`, `open`, `remove`, `annotate`, `discover hook` | Zero or one filter/name/directory as shown in help |
| `cd`, `scan`, `alias remove`, `config get`, `ctx switch`, `ctx show`, `ctx drop`, `snapshot restore`, `snapshot show`, `snapshot drop`, `worktree rm` | Exactly one operand |
| `alias set`, `config set`, `worktree add` | Exactly two operands; quote an alias command as a single argument |
| `status`, `remotes`, `diff`, `fetch`, `pull`, `sync`, `report`, `branch stale`, `stash`, `stash pop`, `clean`, `worktree adopt` | `repos...` |
| `branch` | Optional branch name first, then `repos...`; no operands means current-branch overview |
| `grep`, `apply`, `branch switch`, `ctx save`, `snapshot save` | One required pattern/file/branch/name, then `repos...` |
| `tag` | Zero or more tags; no operands shows current-repo tags, otherwise replaces them |
| `tag add`, `tag remove` | One or more tags |
| `exec` | At least one child-command operand; use `exec [soko flags] -- <command> [child args...]` |
| `alias`, `config`, `ctx`, `snapshot`, `worktree` | Help-only parents; choose a named subcommand |

Cobra supplies `help [command...]`, `completion` (help-only parent), and
`completion bash`, `completion zsh`, `completion fish`, `completion powershell`
(no operands). Hidden `__complete` and its `__completeNoDesc` alias implement
the completion protocol: the command line and final partial word are protocol
operands, not ordinary command execution. They must not inherit leaf cardinality
checks. No application command currently declares a built-in spelling alias;
user-configured aliases still expand before parsing, with appended arguments
preserved and built-in names taking priority.

## Decisions and compatibility impact

- **`version extra`: rejected**, like other zero-operand leaves. Also reject
  ignored operands on `list`, `stats`, `doc`, `alias list`, `tag list`,
  `config path`, and `config edit`, before running their handlers. Scripts that
  passed unused operands must remove them; use supported selector flags instead.
  `shell-init` retains its existing shell-name typo hints.
- **`version --json`: accepted, still plain text.** Inherited `--json` does not
  promise JSON on generators, help, or version. No new global rejection rule.
- **`shell-init --fish --pwsh`: rejected with no script output.** There is no
  useful combined shell script. Previously fish won silently; scripts relying
  on that precedence must choose one mode. Check effective boolean values:
  `--fish=false --pwsh` is valid, both false selects Bash/Zsh, and repeated
  boolean values use the last value.
- **`status --dirty --clean`: accepted, union (OR), not intersection (AND).**
  All four status predicates (`dirty`, `clean`, `ahead`, `behind`) compose with
  OR and each repo appears once. Clean means no uncommitted changes/conflicts
  and no ahead/behind commits, with a successful read. Thus dirty-or-clean
  does not include an otherwise clean ahead-only or behind-only repo, nor an
  unreadable repo. Name/tag/metadata/family selectors narrow the input before
  status predicates are applied.

## Flag composition and precedence inventory

Keep existing combinations unless explicitly changed above. In particular,
do not introduce a global "only one mode flag" rule:

| Flags / commands | Policy |
|---|---|
| Global `--json`, `--quiet`, `--perf` | May appear before or after commands, including nested commands. JSON/perf are command-specific; quiet does not suppress results or errors. Explicit quiet/perf flags (including false) override environment fallbacks. |
| Repeated scalar flags (bool, string, integer, duration) | Last parsed value wins; booleans use `--flag=false` to disable. |
| String-slice flags (`--tag`, `--root`, `--ignore`, `--unset`, `--tag-with`) | Repeated occurrences accumulate; comma-separated values are split using pflag's CSV syntax. Tag filters combine with OR. |
| String-array flags (`--meta`, `annotate --set`) | Repeated occurrences accumulate; commas stay literal. Metadata predicates combine with AND; setting the same metadata key repeatedly leaves the last value. |
| Repeated positional repo selectors | Preserve exact-name-before-prefix matching and deduplicate the selected repositories. |
| `status` / `list`: `--group`, `--group-by` | `--group` means tag grouping. An explicitly supplied `--group-by` must be `tag` when `--group` is enabled; `none`/`repo` conflict. Otherwise use `--group-by` (`none`, `tag`, `repo`). |
| `open`: name and `--tag` | Rejected together. Browser section flags retain precedence: `--prs`, then `--issues`, `--actions`, `--branches`, `--settings`. |
| `report`: `--author`, `--all-authors` | A nonempty explicit author remains the filter; all-authors disables only the default author lookup. Together they do not discard the explicit author. |
| `remove`: `--all`, `--path`, name | Retain precedence: all, then nonempty path, then name. No selector prints usage without mutation. `--select` refines all mode only; `--force` skips its confirmation. |
| `annotate`: `--set`, `--unset`, `--clear` | Enabled mutation modes are mutually exclusive. `--list` rejects a repo operand, nonempty `--repo`, or mutation. Otherwise a positional target wins over `--repo`, then cwd. |
| `clean`, `prune`: `--dry-run`, `--force` | May combine; dry-run prevents deletion/unregistration, force only skips confirmation. `clean --prune` still performs remote pruning during inspection, including with dry-run. |
| `clean`, `prune`, `remove`: `--select`, `--json` | Rejected together on normal operation paths; interactive selection is not a JSON protocol. Clean/prune JSON requires force or dry-run when processing. Empty-selection early returns can precede these checks; see JSON capabilities. |
| `apply`: `--write`, `--force`, `--json` | Default is preview. Force matters with write; JSON plus write requires force. `--to` is required. |
| Remaining operation modifiers | Compose with selectors: fetch/prune, pull/rebase, sync/fetch-only, grep/ignore-case/regexp/files-only, scan/depth/worktrees, init/worktree, branch switch/create, worktree add/create-branch/path, worktree adopt/dry-run/tag-with, save/force, drop/force, undo/list, doc/fix, health/top/threshold, ui/fetch. Preserve their existing value and safety checks. |

## Parsing boundaries

- `--` ends soko flag parsing. For example, `soko status -- -api` selects a
  leading-hyphen registration, and `soko grep -- -needle` searches a literal
  leading-hyphen pattern. It does not bypass the command's argument count.
- After `exec --`, every token belongs to the child, even `--json`, `--quiet`,
  `--tag`, another `--`, empty strings, or whitespace-containing arguments.
  Put soko's flags before that boundary. The legacy no-separator form remains
  accepted, but child flags need `--` to avoid soko interpreting them.
- Do not trim or reinterpret positional values to simplify validation.
  Native Git branch/revision semantics and supported special names remain
  governed by the relevant operation, not a new generic name validator.
- Help and shell-completion requests retain Cobra's own behavior. No blanket
  root argument validator or global unsupported-flag rejection is added.

Regression inventory: `internal/cli/arguments_test.go` checks cardinality for
every application operation, and `arguments_integration_test.go` checks parser
boundaries, side-effect-free rejection, effective shell modes, repeated flags,
selectors, completion protocol, and actual child argv passthrough.
