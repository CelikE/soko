# Audit 356: terminal workflow validation

This records the terminal workflow validation for [issue #356](https://github.com/CelikE/soko/issues/356). It is not a claim that every terminal scenario or platform has been covered.

## Environment and reproduction

- Base: `d3f93418b2d68254018c8e192d45f8293aba0a5f` plus the accompanying tests.
- Executed on macOS Darwin 25.6.0 arm64, Git 2.56.0, Go 1.27.3.
- Command: `go test -race ./internal/cli -run '^TestTerminalAudit' -count=1 -v`.
- Save the command's output to retain the complete PTY transcripts. Each test logs its raw terminal transcript with `-v`; failed tests include their transcript without `-v`.
- When the manual Linux race workflow runs, its `race-timings.jsonl` artifact also contains the Go test output events; no CI execution is claimed here.
- All repositories, Git remotes, config, browser and clipboard adapters are disposable fixtures. The PTY tests are excluded on Windows.

## Observed results

| Scenario | Outcome and independent state checks |
| --- | --- |
| Dashboard pull and undo | `P`, confirmation and `u` operated on a local bare remote. The journal contained the exact pre/post SHAs, branch and checkout path; successful undo restored the pre-pull SHA and tracked content. |
| Refusals and retry | Dirty tracked content, an untracked file, a changed branch and a newer HEAD each caused refusal, preserving the journal and affected user state. After resolving each condition, retry restored the pre-pull SHA. |
| Batch retry | Two marked checkouts produced one journal entry. A dirty second checkout caused partial undo; the first was checkpointed. A new commit on that first checkout survived retry while the second returned to its pre-pull SHA. |
| Remove, prune and clean selectors | Picker cancellation and declined confirmation preserved state. Search and deselection narrowed actual operations after resizing to 55 columns with long, wrapped paths; registry entries and branch refs were inspected afterwards. Tag-excluded repos remained untouched for prune and clean. |
| Dashboard input | Real terminal input exercised sorting, filtering, search, marking, help, a PR overlay, copying, resizing and navigation. Fake browser, clipboard and PR adapters recorded the actual selected URL and path. |
| Fetch overlap | A wrapper held the first manual Git fetch while further manual requests and the 30-second periodic interval elapsed. A later timer-initiated fetch was held while manual requests were sent. Neither ordering started overlapping Git fetches, and a manual fetch succeeded after each release. |

## Remaining boundaries

- Resize cases exercise a narrow, clipped dashboard and picker redraws with wrapped paths, not an exhaustive matrix of terminal widths or Unicode content.
- Browser, clipboard and PR services were adapters, not live integrations. No native Windows or Linux execution is claimed by this record.
