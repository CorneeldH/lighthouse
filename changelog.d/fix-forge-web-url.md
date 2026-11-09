---
type: fixed
---
The kenn-forge link in the Controller section can point elsewhere than the forge API lighthouse
talks to: set `[dispatch.forge] web_url` in `host.toml`, for instance to the tailscale serve
address. Without `web_url` the link stays `[dispatch.forge] url`.
