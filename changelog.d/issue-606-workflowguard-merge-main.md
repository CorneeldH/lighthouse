---
type: fixed
---
`workflow-push-gate` en `lighthouse post-artefact` tellen alleen nog de eigen workflow-wijziging van een branch. Een merge van main die alleen de workflows van `origin/main` binnenbrengt, blokkeert de push niet meer en stuurt de run niet meer naar de workflow-pusher; een eigen workflow-wijziging naast die merge telt wel (#606).
