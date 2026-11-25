---
type: changed
---
Test stubs on the `PATH` now come from one shared helper, `internal/stubbin`, that is executable on every platform, and test helpers build the `PATH` with `os.PathListSeparator`; tests that imitate POSIX behaviour skip on Windows with a reason (#566).
