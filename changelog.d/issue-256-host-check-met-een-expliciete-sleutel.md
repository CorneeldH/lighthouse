---
type: added
---
`host.toml` gets an optional boolean key `host.controller`. `true` makes `lighthouse config host-check` and `plan-review-record` treat this machine as the controller host without measuring an address (for a host without Tailscale); `false` says it is not. Without the key nothing changes: `host.tailscale_ip` bound on this machine decides. Upgrade order: run `lighthouse init --upgrade` in every repository on the host first, then set the key.
