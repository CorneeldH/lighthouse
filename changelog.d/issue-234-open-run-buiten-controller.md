---
type: fixed
---
`lighthouse tick` now closes an open run on an issue that left Controller and On hold, such as a close-check run whose issue `lighthouse-close-issue` already moved to Done. Before, such a run never closed and kept holding a slot.
