# Area: init

## Crux

Installeren en upgraden zonder iets van de gebruiker kapot te maken. `lighthouse init` schrijft in
een repo die niet van lighthouse is: skills, hooks, een blok in `AGENTS.md`, regels in
`.claude/settings.json`. Bij elke upgrade moet vaststaan wat van lighthouse is en wat van de
gebruiker, zodat een eigen wijziging nooit stil verdwijnt en een upgrade niets anders meeneemt dan
wat `init` schreef.

## Leidend beleid

- Het manifest is de bron van waarheid: wat lokaal afwijkt van pack en manifest, is een conflict;
  `--upgrade` weigert dan, toont de diff en vraagt `--force`
  ([M0](../planning/specs/2026-10-17-m0-pakket-design.md),
  [repokaart bij upgrade](../planning/specs/2026-11-22-issue-192-repokaart-upgrade-design.md)).
- Lighthouse schrijft nooit zonder akkoord in `AGENTS.md` of `CLAUDE.md` van een projectrepo; `init`
  beheert alleen zijn eigen blok ([M2](https://github.com/CorneeldH/lighthouse/milestone/3),
  [Product](https://github.com/CorneeldH/lighthouse/milestone/6)).
- Een repo-upgrade gebeurt in een worktree, en de commit bevat alleen wat `init --upgrade` schreef
  ([lighthouse-upgrade](../planning/specs/2026-11-15-issue-46-lighthouse-upgrade-design.md)).
- Wat projectafhankelijk is, is optioneel: zonder sleutel slaat een stap over en meldt dat
  ([M0](../planning/specs/2026-10-17-m0-pakket-design.md),
  [gepland op de kaart](../planning/specs/2026-11-22-issue-476-gepland-op-de-kaart-design.md)).

## Bewust niet

- **N1** Een Claude Code-plugin: `init` kopieert, zodat ook Codex en OpenCode de skills krijgen
  ([M0](../planning/specs/2026-10-17-m0-pakket-design.md)).
- **N2** Een automatische upgrade van een repo; `/lighthouse-upgrade` start altijd met de hand
  ([lighthouse-upgrade](../planning/specs/2026-11-15-issue-46-lighthouse-upgrade-design.md)).
- **N3** De binary zelf bijwerken; dat is `lighthouse self-update` met release en checksum → product
  ([automatische upgrade](../planning/specs/2026-11-07-issue-267-auto-upgrade-design.md),
  [lighthouse-upgrade](../planning/specs/2026-11-15-issue-46-lighthouse-upgrade-design.md)).
- **N4** Wat de geschreven hook of skill doet → core ([labels](../labels.md)).
