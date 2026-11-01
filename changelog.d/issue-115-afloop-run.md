---
type: changed
---
The tick now decides the outcome of every open run: it hands the issue back with an honest reason when a session is silent, waits on a permission prompt, did not start, or leaves a draft, closed or red PR. It stops the session when needed and closes a run whose issue was closed. A new hook, `permission-check`, notes a permission prompt of a controller run.
