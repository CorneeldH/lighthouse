---
type: fixed
---
`workflow-push-gate` and `lighthouse post-artefact` count only a branch's own workflow change. A merge of main that only brings in the workflows of `origin/main` no longer blocks the push or sends the run to the workflow pusher; the branch's own workflow change beside that merge still counts (#606).
