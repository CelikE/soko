---
bump: minor
---

`soko ui` gains its first mutating key: `P` fast-forward pulls the selected repo after a confirmation prompt, runs asynchronously, and records a journal entry so `soko undo` rewinds it to the pre-pull SHA. Satisfies feature 44's scope guard — mutations only once undo exists.
