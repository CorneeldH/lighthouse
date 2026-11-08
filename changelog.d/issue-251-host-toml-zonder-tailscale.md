---
type: changed
---
`host.tailscale_ip` is optional in `host.toml`, so a host without Tailscale can set the `gate_*` keys. Run `lighthouse init --upgrade` in every repository on the host first: an older copy rejects a `host.toml` without `tailscale_ip`, and its test gate then blocks every commit.
