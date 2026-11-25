---
type: added
---
`init` zet `autoMemoryEnabled` in `.claude/settings.json` op `false`, volgens de stand van de hook `memory-gate`, en controleert dat bestand nu altijd: een misvormde `settings.json` laat `init` ook mislukken als er geen hook verandert. `memory-gate` blokkeert een schrijfactie in een auto-memory-map van Claude Code, via `Edit`, `Write` of een Bash-commando; lezen blijft vrij. Op `light` logt hij alleen. `lighthouse memory scan` toont de memory-bestanden van de repo met een voorgestelde bestemming, en de skill `/lighthouse-migrate-memory` verdeelt ze per groep na akkoord over instructies, issues, docs en `host.toml` (#497).
