---
type: fixed
---
`lighthouse serve` no longer shows `done · next tick decides` for a run whose session still has background work or an open turn after its Stop. The page now shows such a run as `working`, the same measure the tick uses to decide a run is done.
