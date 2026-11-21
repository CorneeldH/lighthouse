---
type: fixed
---
A controller run whose handoff or question post fails no longer stalls the chain: the run leaves a `post-failed.json` note, and the controller posts the comment itself or hands the issue back with the error. `lighthouse post-artefact` no longer uses GraphQL.
