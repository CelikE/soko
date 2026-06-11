---
bump: minor
---

`soko undo` reverts the last destructive operation via a capped pre-image journal beside the config (`journal.yaml`). `soko clean` now records the branches it deletes, so `soko undo` recreates them at their exact SHAs; `soko undo --list` shows the journal. This is the trust layer that will unlock mutating keys in `soko ui`.
