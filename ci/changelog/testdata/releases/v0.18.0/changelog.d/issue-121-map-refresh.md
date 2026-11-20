---
type: added
---
Scheduled job `map-refresh` (#121): a `[[schedule]]` block with `job = "map-refresh"` keeps the codebase map fresh. A Go gate measures the drift of `origin/<default branch>` since the last commit that touched `map_dir` (changed files under `drift_paths` plus a weight per new directory) and skips, runs the fast map or the full map; the result is checked for completeness (`documents`, `min_lines`), mirrored into `publish_dir` with an `index.md`, and lands through the auto-merged PR of the scheduled run. Invalid params are a config error logged at each tick. See `docs/tick.md`.
