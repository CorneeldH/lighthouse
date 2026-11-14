---
type: fixed
---
A fix-pr start no longer waits for a free slot: it replaces the run that closes in the same tick, which already holds one. With all slots held by runs on a red PR, the tick started nothing and those runs stayed open.
