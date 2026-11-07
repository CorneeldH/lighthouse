---
type: fixed
---
On native Windows (Git Bash), `commit-test-gate` and `bash-write-check` no longer block every commit on a missing `fcntl` module, and `lighthouse usage` can take its lock. A `[test_gate] command` still cannot run there: the gate slots need `flock`.
