---
type: added
---
`lighthouse init` kent instapniveaus (`light`, `standard`, `strict`): `--level` kiest wat er wordt geïnstalleerd, `level` in `lighthouse.toml` en `.lighthouse/components.toml` leggen de keuze vast, en `[components]` zet onderdelen per stuk aan of uit; de vereisten van uitgezette onderdelen tellen niet mee. Een eerste `init` in een nieuwe repo kiest `light`, een bestaande repo zonder `level` gedraagt zich als `strict`. `init` faalt nu ook op een ongeldige `lighthouse.toml`, omdat een bestaand bestand met `config.Load` wordt gelezen.
