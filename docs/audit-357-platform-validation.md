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

Workflow execution requires explicit user authorization. At document creation,
native runner execution for this change is pending; earlier green runs do not
establish coverage for these new tests.
