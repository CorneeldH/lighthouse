---
type: internal
---
The pre-push secret-scan tests put the `lighthouse` binary of the checkout on `PATH` themselves, so they also pass on a runner without one, such as the macOS release gate.
