---
type: changed
---
Breaking: a run on another kenn-forge node now keeps its content (carrier, transcript, markers, usage brake, stop and kill) on that node, reached through `lighthouse serve` with `[node] token_file`; every `[dispatch.nodes.<name>]` in `host.toml` now needs `serve_url` and `serve_token_file`, and the node's own `host.toml` needs `[dispatch.forge]` and `[dispatch.profiles]`. Without `[node]` the routes under `/api/node/v1/` stay 404, and `serve` does not start without a forge token. macOS example: `deploy/launchd/com.lighthouse.serve.plist`.
