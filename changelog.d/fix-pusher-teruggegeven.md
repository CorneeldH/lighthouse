---
type: changed
---
A red step of the workflow pusher that an answer `push` cannot act on (trigger, config, route, floor, policy, lint, and every refusal of the merge pusher) is no longer a 🤖 Question but a 🤖 Returned with `reason: workflow pusher red: <step>`, the diff folded and the command under *Push it yourself* or *Merge it yourself*; tick then gives the issue back with `needs_hands`. A real pusher Question opens with the question itself, so the controller's list of open questions no longer shows the whole diff.
