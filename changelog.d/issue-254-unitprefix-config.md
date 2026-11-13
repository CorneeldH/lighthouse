---
type: changed
---
The systemd unit prefix of kenn-forge runs now comes from `[dispatch.forge] unit_prefix` in `host.toml`, with no default in the code. Hosts that use kenn-forge must add `unit_prefix = "paia-forge-"`, otherwise every `Stop` fails with an error naming the key.
