---
type: added
---
The dispatcher can place a run on another kenn-forge node. `[dispatch.nodes.<name>]` in `host.toml` names a node and its slots, a profile gets a `node`, and `[controller.locations]` with `[labels] location_prefix` in `lighthouse.toml` lets an issue's location label pick the profile. The brake counts slots per node, and a resume or retry stays on the node of its run. Real runs on another node still wait for #327, because forge reports no session id there.
