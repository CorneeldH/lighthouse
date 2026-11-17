---
type: fixed
---
The controller no longer hands a run back when its workspace only runs ahead of `origin/<branch>`: the run starts on that HEAD, and a resume of the same session is never handed back for own work. `/lighthouse-write-handoff` now pushes the branch before the `🤖 Handoff` in controller mode and in the pause form, never the default branch.
