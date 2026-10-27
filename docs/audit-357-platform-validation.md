# Audit 357: native platform and shell validation

Evidence for [issue #357](https://github.com/CelikE/soko/issues/357). Base source:
`e65aebc` plus this change. Native runner results must be attached before claiming
the complete matrix passed.

## Executed locally

- macOS Darwin 25.6.0 arm64, Go 1.27.3, Git 2.56.0, PowerShell 7.6.9.
- PowerShell was downloaded to a temporary directory; no profile or system
  installation was changed. Git repositories, configuration and navigation
  requests in the tests are disposable.
- Before the fix, actual PowerShell execution left cwd unchanged for trailing
  spaces/newlines, failed to consume requests under a config path containing
  literal brackets, and consumed invalid requests before navigation succeeded.
  Ordinary names were positive controls. No native Windows failure is inferred
  from these macOS observations.
- `TestIntegration_PowerShellExactNavigation` passed after the fix: ordinary and
  leading spaces, brackets, Unicode, Unix trailing spaces/newlines, `cd`, `go`,
  retry, exact untrimmed cwd comparison and unrelated-session preservation.
- `TestTerminalAuditShellCompletion` passed with real TAB keystrokes in Bash
  3.2.57, Zsh 5.9 and Fish 4.9.3. Generated scripts were loaded into isolated
  interactive shells. Executed argv, not prefix-matching command success, was
  checked for valid and unmatched repository names. Bash used bash-completion
  1.3 from its upstream tagged source in a temporary directory.
- `TestIntegration_PowerShellCompletion` loaded the generated script and used
  PowerShell's `CommandCompletion.CompleteInput` engine. Its unmatched-name
  error was reproduced before the completion guard and passed afterwards.
  This is completion-engine evidence, not PSReadLine keyboard/console evidence.
- Native volume-root, descendant, parent and sibling-prefix containment controls
  passed locally in `TestPathWithinRoot`.
- The complete local `go test -race ./...` run passed after these fixes.
- [CI on a8d6dd3](https://github.com/CelikE/soko/actions/runs/40562201354)
  passed lint, release configuration, Python runner/helper tests, Linux race and
  coverage, destructive-workflow validation and the Windows worktree/PowerShell
  smoke check. It does not replace full native matrix evidence.

## Reproduction and native runners

```sh
go test ./internal/cli -run 'TestIntegration_PowerShell|TestTerminalAuditShellCompletion' -count=1 -v
go test ./internal/config -run 'TestPathWithinRoot|TestShouldDiscover' -count=1 -v
```

Install PowerShell and the Unix completion prerequisites first; these tests
explicitly skip missing local prerequisites. The manual `Full platform tests`
workflow installs/checks prerequisites and runs the full suite on Linux, macOS
and Windows, then reruns the focused checks with verbose output. Its logs record
the source SHA, OS and tool versions; timing artifacts retain full test events.
Windows uses only supported ordinary filename cases; Unix-only trailing spaces
and newlines are not asserted as supported Windows filenames.

## Native attempt and runtime correction

The authorized [a8d6dd3 native run](https://github.com/CelikE/soko/actions/runs/40562201597)
passed the full macOS suite, but failed Linux and Windows. It is not a passing
matrix. Linux's Zsh fixture blocked on compinit's insecure-directory prompt.
The fixture now uses `compinit -i` to ignore insecure directories without
disabling permission checks; an intentionally insecure fixture directory makes
this regression reproducible locally.

Windows exposed slash-separated Git root output failing exact native-path
lookup, Unix-only quote filenames in a fixture, quoted-path diagnostic assertions
expecting unescaped backslashes, and a CP1252 Python console crash when printing
Unicode failure output. These are corrected separately: native path conversion,
supported filename cases (with Unix cases retained on Unix), exact quoted
diagnostic expectations, and UTF-8 console configuration with a regression test.

The prior Windows timing artifact at
[40478520679](https://github.com/CelikE/soko/actions/runs/40478520679) recorded
584 seconds in one sequential CLI test process. Four isolated CLI shards retain
all tests and run other packages exactly once; the historical serial work would
be 108/156/177/144 seconds across those shards, not a claimed new runtime.
Slow tests now emit progress, artifacts are per-shard, and jobs have a ten-minute
bound. This uses process isolation rather than unsafe parallel execution of
tests that change process-wide environment or cwd.

## Sharded native run

The [21c7c6d run](https://github.com/CelikE/soko/actions/runs/40565260983)
passed Linux and macOS suites and three of four Windows shards, including the
previously failing native path and diagnostic checks. Windows job times were
3m03s, 2m23s, 4m40s and 2m53s, versus 12m02s for the preceding serial attempt.
The final shard exposed a README/manpage example parser assuming LF line endings
in a CRLF Windows checkout. The parser now accepts both, with a focused LF/CRLF
regression; all five README examples and three manual examples remain required.
That run remains a failed matrix, not a pass.

Final closure results, exact tested SHAs and workflow links are recorded on
[issue #357](https://github.com/CelikE/soko/issues/357) and
[PR #379](https://github.com/CelikE/soko/pull/379). The historical attempts above
are retained to distinguish executed failures from the corrected verification.
