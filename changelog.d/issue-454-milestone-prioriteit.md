---
type: added
---
De weergave *Milestones* van het tabblad Prioritering heeft een stand *Prioriteit*, naast *Berekend*, met Omhoog en Omlaag. `lighthouse serve` schrijft daarbij een regel `Priority: N` in de beschrijving van de milestone (`POST /api/priorities/milestone-ranks`) en eist daarvoor een `Origin` gelijk aan `Host`. De Vite-devproxy staat daarom op `changeOrigin: false`.
