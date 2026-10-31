# Labeltaxonomie

De labels van deze repo. `lighthouse-write-issue` leest dit bestand via `[paths] labels` in
`lighthouse.toml`; de skill zelf kent geen labels.

Er zijn twee assen: elk issue heeft precies één area-label en hoogstens één blokkadelabel. Het
cijferprefix is de leesvolgorde in de labellijst.

| As | Cardinaliteit | Vraag |
|---|---|---|
| `2-area:*` | precies 1 | Waar hoort het bij? |
| `3-*` | 0 of 1 | Waarom kan dit nu niet doorlopen? |

Er is geen `1-loc:`-as: lighthouse is software, en alles is op te pakken in de code van deze
repo. Een locatie-as hoort bij een repo die ook machines configureert.

Daarnaast zijn de GitHub-standaardlabels (`bug`, `enhancement`, `documentation`, …) een vrije
as.

## 2-area: — waar hoort het bij

| Area | Kleur | Vuistregel |
|---|---|---|
| `2-area:core` | `4C1D95` | `pack/core`: hooks, keten-skills, gates |
| `2-area:packs` | `5B21B6` | taalpakketten (`pack/go`, `pack/python`, `pack/shell`, `pack/node`, `pack/expo`) |
| `2-area:init` | `6D28D9` | `lighthouse init`, `--upgrade`, het manifest, het blok in `AGENTS.md`, en het weer verwijderen |
| `2-area:server` | `7C3AED` | `lighthouse serve`, SQLite, de SPA, de forge-lezer |
| `2-area:dispatcher` | `8B5CF6` | tick, decide, apply, werkplekken (M2), en kenn-forge als werkplek |
| `2-area:usage` | `A78BFA` | de venstermeters en de usage-guard |
| `2-area:product` | `C4B5FD` | release, website, installatie, licentie, documentatie voor gebruikers |
| `2-area:meta` | `DDD6FE` | CI, docs voor wie aan lighthouse werkt, planning, deze repo zelf |

Precies één. Passen er twee, kies het zwaartepunt. De kleuren lopen van donker (fundament) naar
licht.

Grensgevallen:

- **core/usage:** de usage-guard is een hook in `pack/core`, maar hoort bij `usage` (#66).
- **core/init:** wat `init` schrijft is `init`; wat de geschreven hook of skill doet is `core`.
- **product/meta:** voor wie is het? Wie lighthouse gebruikt → `product` (#99–#104, #88); wie
  aan lighthouse werkt → `meta`.
- **product/init:** platformsteun en release-binaries zijn `product` (#90, #78); de opdracht
  `init` zelf is `init` (#91, #46).
- **dispatcher/server:** wat de controller doet is `dispatcher`; wat je ervan ziet in de SPA is
  `server` (#69).

## Milestones

`lighthouse-write-issue` leest hier de vorm van een milestonetitel. Deze repo heeft geen
`[project] milestone_prefix`, dus de titel begint direct met de vorm:

- **Fase van de roadmap:** `M<n> — <naam>`, bijvoorbeeld `M2 — Dispatcher`. Het cijfer zegt waar
  de fase in de volgorde staat.
- **Thema buiten de fasen:** `<Thema>: <naam>`, bijvoorbeeld `Product: eerste versie voor
  anderen`.
- **Bij sluiten:** de titel krijgt ` - DONE` erachter, in dezelfde PATCH als `state=closed`
  (`M1 — Romp en zicht - DONE`).

## 3- — de blokkade

Een `3-`-label markeert **wat de eerstvolgende stap blokkeert**, niet wat ergens in het issue
voorkomt. Het is een toestand, geen eigenschap: het komt erop zodra het werk de mens-stap
bereikt en gaat er weer af.

De toets: **kan ik dit gewoon tegen de agent zeggen?** Zo ja → geen label, ongeacht hoeveel
werk het is. Geen label betekent oppakbaar.

Hoogstens één. Gelden er twee, dan wint de **laagste rang**.

| Rang | Label | Kleur | Bindt wanneer |
|---|---|---|---|
| 1 | `3-needs-shaping` | `FBCA04` | Wat/waarom nog niet scherp. Alles erna is prematuur |
| 2 | `3-needs-decision` | `FBCA04` | Eén keuze van Corneel blokkeert, de rest is helder |
| 3 | `3-blocked-issue` | `D93F0B` | Wacht op een ander issue in deze repo |
| 4 | `3-blocked-external` | `D93F0B` | Wacht op derde partij, API, vendor, levering |
| 5 | `3-needs-hands` | `FBCA04` | Hij moet instellen, invoeren, aansluiten, aanschaffen |
| 6 | `3-needs-testing` | `FBCA04` | Bouw staat; hij moet oordelen of het doet wat bedoeld is |

Amber = de bal ligt bij Corneel, rood = buiten.

`3-blocked-issue` gaat nooit mee in `gh issue create`; de `blocked_by`-relatie komt eerst.
