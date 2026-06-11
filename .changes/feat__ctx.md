---
bump: minor
---

Add `soko ctx` — save and restore workspace contexts: `ctx save` records each repo's branch and stashes dirty trees under a per-context message, `ctx switch` checks the branches back out and pops the stashes (refusing repos that are dirty right now), plus `ctx list`/`show`/`drop`.
