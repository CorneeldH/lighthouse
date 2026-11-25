---
type: changed
---
`[labels]` in `lighthouse.toml` has a new key `needs_mind` (default `needs-mind`) that replaces `needs_shaping` and `needs_decision`, and `needs_testing` is folded into `needs_hands`; a file with an old key is refused with the name of the migration command. `lighthouse labels migrate` rewrites `lighthouse.toml` and moves the labels on the forge (#588).
The status of an issue is now one of *needs mind*, *to do*, *active*, *needs hands* and *done*, and a blockade is a flag drawn as a cross over the shape; `/api/priorities` returns the `labels` of each open issue and the legend has the headings *Milestones*, *Progress* and *Labels*.
