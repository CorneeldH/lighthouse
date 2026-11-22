---
type: fixed
---
A run in a kenn-forge container whose transcript is not on the host is no longer handed back as *no handoff or question* at its first ended turn with a running subagent (#514). The adapter now reads such a transcript from the devcontainer for every profile, not only `software`, and a transcript that stays not measurable counts as work after the Stop: the run stays open until the silence or runtime rules decide.
