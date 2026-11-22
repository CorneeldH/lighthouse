---
type: internal
---
De e2e-tests geven elk subproces een eigen `XDG_STATE_HOME`, zodat een test nooit meer het usage-alarm in de echte controller-state schrijft en daarmee lopende controller-runs tot een reset een jaar vooruit parkeert.
