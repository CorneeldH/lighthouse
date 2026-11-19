---
type: internal
---
The known macOS test flakes are gone: timing tests wait on a synchronisation point instead of a wall-clock bound, a non-UTF-8 file name is skipped where the filesystem refuses it, and a network error of the runner is a skip, not a red test.
