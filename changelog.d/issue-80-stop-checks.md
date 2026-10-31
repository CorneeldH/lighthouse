---
type: changed
---
The `go-check` and `ty-check` Stop hooks now skip a checkout where the turn touched no Go or Python path, and say so in one line instead of linting it. An edit, a Bash write or a commit in the turn still runs the check.
