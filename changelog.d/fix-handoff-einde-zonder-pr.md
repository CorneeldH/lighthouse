---
type: fixed
---
A controller run that ends with a handoff to `/lighthouse-close-issue` and no pull request now closes at once and starts the close-check run, instead of waiting for a pull request until `max_runtime` hands the issue back with *time-out*.
