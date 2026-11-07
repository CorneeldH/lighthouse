---
type: fixed
---
The dispatcher no longer hands back a controller run whose session ended its turn to wait for a background Bash task or an async subagent; such a run now counts as working until that work ends.
