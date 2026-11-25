---
type: changed
---
`[labels]` in `lighthouse.toml` has a new key `needs_mind` (default `needs-mind`) that replaces `needs_shaping` and `needs_decision`, and `needs_testing` is folded into `needs_hands`; a file with an old key is refused with the name of the migration command. `lighthouse labels migrate` rewrites `lighthouse.toml` and moves the labels on the forge (#588).
