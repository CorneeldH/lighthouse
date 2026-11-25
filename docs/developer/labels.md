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

## Vrije labels met een document

Een vrij label kan een document hebben: het bestand dat een PR bijwerkt zodra het issue met dat
label geleverd wordt. Een vrij label zonder document, zoals de GitHub-standaardlabels, laat
`write-pr` met rust.

| Label | Kleur | When | Document | Planned |
|---|---|---|---|---|
| `loop` | `F4D74B` | Voegt een stap, handoff of route van de loop toe of verandert die | `docs/site/data/loop.json` | `milestone` |

Een label met een document betekent twee dingen. `write-issue` toetst een issue aan de kolom
*When* en geeft het dan het label. `write-pr` werkt het bestand uit de kolom *Document* bij: altijd
bij `Closes`, en bij `Refs` wanneer de branch levert wat het issue verandert (de sessie beoordeelt
diff en acceptatiecriteria). Uitleg over de controller, of een verbetering binnen een bestaande stap,
krijgt het label `loop` niet.

Bijwerken van `loop.json` betekent: een `planned` item met het issuenummer wordt `works` en
verliest `issue`; een nieuwe stap of handoff komt erbij als `works`, met `source`. Een nieuw
`loop`-issue zonder milestone krijgt alleen het label en komt pas met de leverende PR op de kaart
(`docs/site/loop.html`). Krijgt het issue een milestone, dan komt het als `planned` item met
`issue` op de kaart (`milestone` mag erbij staan). Een `planned` item zonder `issue` mag niet.

"Als gepland toevoegen" is in `loop.json` alleen een nieuwe stap, handoff of route als `planned`
item met `issue`. Een wijziging aan een bestaand onderdeel geeft geen item.

De kolom *Planned* zegt wanneer een issue met dit label op de kaart hoort: `milestone` (alleen
met een milestone) of `always` (altijd); leeg betekent dat het label geen aanmaakkant heeft. De
dagelijkse job `map-planned` zet zo'n issue als `planned` item op de kaart, of stelt een vraag
als de plek onduidelijk is; `write-issue` legt plek en wens vast bij het aanmaken.

Welke velden de plek op de kaart draagt, staat per soort item in een tabel:

| Label | Item | Place | From issue |
|---|---|---|---|
| `loop` | stap (`chips`) | `phase`, `after`, `kind`, `trigger`, `label` | `what` |
| `loop` | handoff (`handoffs`) | `from`, `to` | |

- *Item*: de soort item, met tussen haakjes en backticks de plek in het document waar zo'n item
  staat. Voor `loop.json` is dat de array.
- *Place*: wat de plek onder `### Op de kaart` noemt. `write-issue` vraagt het aan wie het issue
  schrijft.
- *From issue*: de velden die `map-planned` uit titel en body van het issue overneemt, in de taal
  van het document. Vertalen telt als overnemen, niet als verzinnen. Leeg: het item heeft er geen.
- `status` en `issue` staan niet in de tabel: die zet `map-planned` altijd (`planned` en het
  nummer van het issue).

Per soort item geldt:

- **Stap.**
  - `phase` is een `id` uit `phases`; `kind` en `trigger` zijn waarden die andere chips al dragen.
  - `after` is geen veld van de chip maar een verwijzing: het `id` van de stap waarna de nieuwe
    komt. De job neemt de nieuwe stap op in elke route waarvan `chips` dat `id` bevat, direct na
    het eerste voorkomen. Een `after` die op geen enkele route staat, of waarvan de `phase` na de
    eigen `phase` van de nieuwe stap ligt, is geen geldige plek.
  - `row` is de eerstvolgende vrije rij in die fase; andere chips schuiven niet.
  - `label` is een korte Engelse naam in kleine letters met streepjes, zoals de bestaande `id`'s;
    `write-issue` vraagt hem. `id` is gelijk aan `label`; een `label` dat al een `id` is, is geen
    geldige plek.
  - `what` is één Engelse zin die de job uit titel en body van het issue overneemt.
  - `how`, `when` en `without` blijven weg tot de PR die de stap levert.
- **Handoff.** `from` en `to` zijn `id`'s van stappen die samen op één route liggen, `from` eerst.
  Een stap die hetzelfde issue nieuw toevoegt, telt mee met zijn `label`, dat zijn `id` wordt.
- **Route.** Krijgt nog geen gepland item.
- **Vorm.** Eén regel per item onder `### Op de kaart`: `<item>: <veld> <waarde>, <veld> <waarde>`,
  bijvoorbeeld `stap: phase close, after close-issue, kind lighthouse, trigger skill, label close-followup`
  of `handoff: from close-issue, to close-followup`. Een issue met een nieuwe stap en een
  handoff ernaartoe heeft twee regels; elke regel is compleet.

De test `docs/site/test/loop.test.mjs` houdt de tabel tegen `loop.json`: een array die er niet is,
of een veld dat op geen enkel item voorkomt (behalve `after`), laat hem falen.

`go run ./ci/loop_roadmap` en de workflow `loop-roadmap` (`.github/workflows/loop-roadmap.yml`)
controleren dit in twee standen:

- PR-modus (bij `pull_request`): voor elk issue met `loop` dat de PR met `Closes` sluit, noemt
  geen `planned` item het nog, en de PR raakt `docs/site/data/loop.json`. Raakt een PR
  `docs/site/data/loop.json` of `ci/loop_roadmap/`, dan krijgt hij ook de hoofdmodus-controle.
  De PR-controle draait niet opnieuw als het label `loop` pas op een issue komt nadat zijn PR al
  open staat (het `issues`-event draait de hoofdmodus op de default branch): push een nieuwe
  commit of bewerk de PR-body om de PR-modus opnieuw te starten.
- Hoofdmodus (bij push naar main, issues-events en workflow_dispatch), als vangnet: elk
  `planned` item noemt een open issue met `loop`. De richting issue naar kaart controleert
  de check niet meer; die draagt de job `map-planned`. Een issue dat de PR sluit telt in
  PR-modus niet mee.

## 2-area: — waar hoort het bij

| Area | Kleur | Vuistregel |
|---|---|---|
| `2-area:core` | `4C1D95` | `pack/core`: hooks, keten-skills, gates |
| `2-area:packs` | `5B21B6` | taalpakketten (`pack/go`, `pack/python`, `pack/shell`, `pack/node`, `pack/expo`) |
| `2-area:init` | `6D28D9` | `lighthouse init`, `--upgrade`, het manifest, het blok in `AGENTS.md`, en het weer verwijderen |
| `2-area:server` | `7C3AED` | `lighthouse serve`, SQLite, de SPA, de forge-lezer |
| `2-area:dispatcher` | `8B5CF6` | tick, decide, apply, werkplekken (M2), en kenn-forge als werkplek |
| `2-area:usage` | `A78BFA` | de venstermeters en de usage-gate |
| `2-area:product` | `C4B5FD` | release, website, installatie, licentie, documentatie voor gebruikers |
| `2-area:meta` | `DDD6FE` | CI, docs voor wie aan lighthouse werkt, planning, deze repo zelf |

Precies één. Passen er twee, kies het zwaartepunt. De kleuren lopen van donker (fundament) naar
licht.

Grensgevallen:

- **core/usage:** de usage-gate is een hook in `pack/core`, maar hoort bij `usage` (#66).
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

Drie assen. `needs_mind` en `needs_hands` staan los van alles: elk kan naast elk ander label
staan. Op de wachtas staat hoogstens één label, en `blocked_issue` wint alleen van
`blocked_external`.

| As | Label | Kleur | Bindt wanneer |
|---|---|---|---|
| denken | `3-needs-mind` | `FBCA04` | Corneel moet denken: wat/waarom nog niet scherp, of één keuze van hem blokkeert |
| wachten | `3-blocked-issue` | `D93F0B` | Wacht op een ander issue in deze repo |
| wachten | `3-blocked-external` | `D93F0B` | Wacht op derde partij, API, vendor, levering |
| doen | `3-needs-hands` | `FBCA04` | Hij moet instellen, invoeren, aansluiten, aanschaffen, draaien of testen, of oordelen of het doet wat bedoeld is |

Amber = de bal ligt bij Corneel, rood = buiten.

`lighthouse state label <nr> <key>` past de regel per as toe: `needs_mind` zetten laat een
blokkadelabel staan; `blocked_issue` zetten laat `needs_mind` en `needs_hands` staan en haalt alleen
`blocked_external` weg. `<key>` is de sleutel uit `[labels]`, zoals `needs_hands`. De uitvoerregel
`label: #<nr> …` noemt alle blokkade- en mens-labels die na de wijziging op het issue staan.
`lighthouse state return <nr> <key>` doet hetzelfde en zet het issue daarna op `To do`.

Een blokkade haalt `needs_hands` niet weg: het label blijft staan, zodat de handeling niet
vergeten wordt als de voorganger dicht gaat. De status laat de blokkade winnen, zie
[priorities.md](../priorities.md#afgeleide-begrippen), *Toestand van een issue*.

Een `needs_*`-label betekent: interactief oppakken. De tick start er geen run voor en triage stuurt
het issue niet naar `Controller`; staat zo'n issue op `Controller`, dan zet de tick het op `To do` en
het label blijft. Het label `blocked_issue` is ook het parkeerlabel: zie [tick.md](../tick.md#blokkades).
Een label en een status `On hold` bestaan niet naast elkaar: `On hold` wordt nergens meer geschreven.

`3-blocked-issue` gaat nooit mee in `gh issue create`; de `blocked_by`-relatie komt eerst.
