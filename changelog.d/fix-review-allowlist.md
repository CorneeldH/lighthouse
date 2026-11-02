---
type: fixed
---
The dispatcher starts `/lighthouse-review-spec` and `/lighthouse-review-plan` again: its allowlist
still held the names from before the rename and returned every review handoff. A handoff with an
old name now starts the review under its new name.
