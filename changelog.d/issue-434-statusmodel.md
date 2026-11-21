---
type: changed
---
The status model now runs through tick and triage: every returned issue ends on To do with one label, parking is To do with `blocked_issue` (the tick moves it back), a `needs_*` label on a Controller issue starts no run, and triage sets `needs_shaping` at gate 4c. `On hold` is no longer written and no longer a required board status. After the upgrade, Controller issues whose last protocol comment is their own return move once to To do with a label.
