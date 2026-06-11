---
bump: minor
---

soko snapshot — save and restore exact repo positions: snapshot save records branch + HEAD SHA per repo, snapshot restore moves every repo back (rewinding moved branches, recreating deleted ones, refusing dirty trees), plus list / show / drop. The save game before a risky bulk operation; completes the trust layer started by soko undo.
