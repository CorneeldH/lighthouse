---
type: changed
---
The spec and plan reviews run through `lighthouse review-run`, `lighthouse plan-review-record` and `lighthouse review-pair`; they no longer need `python3`. New: `lighthouse codex-usage read|budget`. `lighthouse usage codex` changes its JSON: `source_file` is now always present (`null` when unset), there is a new `reset_credits` key, and the time per Codex home comes from `[codex_usage] app_server_timeout_seconds` instead of a fixed 20 s.
