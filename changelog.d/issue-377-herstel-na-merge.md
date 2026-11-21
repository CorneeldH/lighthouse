---
type: added
---
`lighthouse measure-recovery` meet per gemergde PR het herstel in de 7 dagen na de merge (fix- en revert-commits op dezelfde bestanden, bug-issues die de PR noemen) en het aantal `fix-pr`-rondes ervoor, en schrijft dat idempotent in `lighthouse.db` (tabellen `recoveries` en `recovery_items`, migratie 0011). Alleen GitHub.
