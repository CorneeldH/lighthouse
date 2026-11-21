---
type: fixed
---
Een controller-run die op een ander open issue wacht, start niet langer elke tick opnieuw. De tick geeft het issue terug (`dezelfde handoff als de vorige, zonder voortgang`) zodra een uitvoerende handoff woordelijk gelijk is aan de vorige, waarvan de run al klaar is. De skill `write-handoff` noemt de uitweg: eerst de `blocked_by`-relatie en het label leggen, dan pas de handoff schrijven.
