---
type: added
---
`lighthouse serve` zet met `[node] token_file` in `host.toml` de node-routes onder `/api/node/v1/` aan, zodat een andere node de run-inhoud van deze node kan lezen. Zonder `[node]` blijven die routes 404; zonder forge-token start `serve` niet. Voorbeeld voor macOS: `deploy/launchd/com.lighthouse.serve.plist`.
