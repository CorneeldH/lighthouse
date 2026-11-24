---
type: internal
---
The `tests` job of `windows-measure` runs the `TestPowerShell*` tests of the test gate apart and verbose, and the job summary counts them as passed and skipped. A skip for a missing `pwsh` can no longer pass for a pass (#495).
