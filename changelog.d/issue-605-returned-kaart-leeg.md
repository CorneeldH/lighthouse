---
type: fixed
---
The *Returned* card of the controller panel in `lighthouse serve` (and `/api/returned`) lists returned issues again. Since #448 a returned issue moves to `To do` with a `needs_*` label, outside the `Controller` status the overview read; it now also reads the `To do` issues with a `needs_*` label, for the Returned list only (#605).
