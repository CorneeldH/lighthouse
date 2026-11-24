# Statusmodel: vijf statussen in de flow, labels als markering — ontwerp

Vervangt de statustabel en de vormentaal van
[Prioritering: zes statussen](2026-11-23-prioritering-zes-statussen-design.md) (§1, §2, §3 en §6),
en de rangregel "hoogstens één blokkadelabel" uit [labels.md](../../labels.md). De rest van dat
ontwerp (graafkaart met één statusblokje, *Without milestone* onder *Milestones*, filter op
milestone) blijft staan.

## Waarom

Het huidige model gooit twee vragen op één hoop: **waar staat het werk in de flow** en **wat houdt
het tegen**. Dat geeft drie problemen:

- **Shaping en blokkade sluiten elkaar uit, terwijl ze samen kunnen.** Een spec mag al terwijl een
  voorganger open is; alleen het plan wacht. Toch staat er hoogstens één blokkadelabel op een issue.
  Na een goedgekeurde spec moet een sessie `needs_shaping` wisselen voor `blocked_issue`.
- **`In progress` wint van een blokkade.** #379 had een goedgekeurde spec, wacht op #375, en staat
  op *active*: `lighthouse-start-issue` zette het board op `In progress` bij `needs_shaping` met een
  open blocker, en niets zette het terug.
- **"Status" betekent twee dingen.** In de legenda van *Dependencies* en *Milestones* staat
  *blocked* naast *to do* en *active*, alsof het een plek in de flow is. Op de kanban is *blocked*
  geen kolom die je doorloopt.

Daarnaast zijn de vier mens-labels fijner dan het werk. `needs_decision` en `needs_testing` staan op
0 open issues (24 november); `needs_shaping` en `needs_decision` geven in de route al hetzelfde
(gate 4c).

## Doelen

- **Status** is de plek in de flow, precies één per issue: **needs mind · to do · active · needs
  hands · done**.
- **Labels** zijn markeringen die tegelijk kunnen gelden: **needs mind**, **blocked**, **needs
  hands**.
- **Active** ligt tussen spec en done en betekent: er wordt echt aan gewerkt. Interactief shapen is
  nooit *active*.
- Twee mens-labels in plaats van vier: `needs_mind` (denken) en `needs_hands` (doen).
- `needs_mind` kan naast een blokkade staan. Na de spec gaat alleen `needs_mind` eraf.
- Eén regel voor Go en web, zoals nu: `issuestate.Of`, `issueState()` en `cases.json`.

## Niet-doelen

- Een andere betekenis voor de board-statussen `To do`, `In progress`, `Controller`, `Done`. Ze
  blijven bestaan en worden gezet zoals nu, met één uitzondering (start-issue, §5).
- Slepen op de kanban.
- Wijzigingen in prio-gewicht of milestone-rang.

## Anti-doelen

- **Twee regels voor de status.** Web en `lighthouse state show` geven voor elk issue dezelfde
  status.
- **Een issue op *active* waar niemand aan werkt.** Dat is precies #379.
- **Oude en nieuwe labels naast elkaar** na de migratie. Na de migratie bestaan
  `3-needs-shaping`, `3-needs-decision` en `3-needs-testing` niet meer, ook niet in `[labels]`.

## Beperkingen

- De statusregel staat in Go (`internal/issuestate`) en in de web
  (`web/src/lib/priorities/derive.ts`), getest op `tests/pack/fixtures/issue-state/cases.json`.
- Label-namen komen uit `[labels]` (`internal/repoconfig`); skills lezen ze met
  `lighthouse config labels.<key>`.
- De pack-skills (`pack/core/skills/`) en hun geïnstalleerde kopie veranderen samen (dogfood,
  `go run ./cmd/lighthouse init --upgrade`).
- Alle UI-tekst is Engels; artefacten Nederlands.

## Aanpakken

1. **Volledige hernoeming met migratie** — *gekozen.* `[labels]` krijgt `needs_mind`; de sleutels
   `needs_shaping`, `needs_decision` en `needs_testing` verdwijnen. Een migratiecommando zet de
   labels op de forge om. Go, web, triage, tick en skills volgen. De gebruiker koos dit: hij is de
   enige gebruiker, dus een overgangsperiode is niet nodig.
2. **Hernoeming met een alias-periode.** Eén release lang leest `[labels]` de oude sleutels nog.
   Afgewezen: er is geen andere gebruiker die op eigen tempo upgradet.
3. **Alleen de UI vertaalt.** De forge houdt vier mens-labels, de web toont er twee. Afgewezen: dat
   zijn twee regels (anti-doel).

## Ontwerp

### 1. Labels: twee assen

| As | Labels | Regel |
|---|---|---|
| **denken** | `needs_mind` | staat los van de andere as; kan naast elk ander label |
| **wachten** | `blocked_issue`, `blocked_external` | hoogstens één; `blocked_issue` wint |
| **doen** | `needs_hands` | staat los van de andere assen; kan naast elk ander label |

- `needs_mind` vervangt `needs_shaping` en `needs_decision`: de mens moet denken (shapen, een keuze
  maken). Naam `3-needs-mind`, kleur `FBCA04`.
- `needs_hands` neemt `needs_testing` op: de mens moet iets doen (instellen, draaien, testen,
  oordelen of het werkt). Naam blijft `3-needs-hands`.
- Een blokkade wint van `needs_hands` in de **status** (§2), niet in de labels: `needs_hands`
  blijft staan, zodat de handeling niet vergeten wordt als de voorganger dicht gaat. Nu haalt de
  rang het label weg; dat vervalt.
- `labels.md` krijgt de twee assen in plaats van de ene rangtabel. De toets ("kan ik dit gewoon tegen
  de agent zeggen?") blijft.
- `lighthouse state label <nr> <key>` en `state return` passen de rang per as toe: `needs_mind`
  zetten laat een blokkadelabel staan; `blocked_issue` zetten laat `needs_mind` en `needs_hands`
  staan en haalt alleen `blocked_external` weg.
- De invariant van `lighthouse-write-issue` 3e verandert: `needs_mind` met een open `blocked_by`
  is **label missing** zolang `blocked_issue` ontbreekt. Het label komt erbij, `needs_mind` blijft.

**Blokkade** is in de rest van dit ontwerp: een label `blocked_issue` of `blocked_external`, of een
open `blocked_by`.

### 2. Status: vijf, in de flow

De eerste rij die past, wint:

| # | Status | Wanneer |
|---|---|---|
| 1 | `done` | het issue is gesloten |
| 2 | `needs_mind` | het label `needs_mind`, of een open 🤖 Vraag |
| 3 | `needs_hands` | het label `needs_hands`, zonder blokkade |
| 4 | `active` | een run met `open`; of Status `In progress` of `Controller`, zonder blokkade |
| 5 | `ready` | de rest (UI-tekst "to do") |

- **Blocked is geen status meer.** Een geblokkeerd issue staat op `ready` (of op `needs_mind`, of op
  `active` bij een open run) en draagt de markering *blocked*. `Of` geeft daarom naast de status
  ook `Blocked bool` terug.
- **Een open run wint van een blokkade.** Een controller-issue in de spec-, plan- of reviewfase met
  een open voorganger werkt echt; het is *active* met een kruis.
- **Een `parked` run telt niet als werk.** Geparkeerd betekent: wacht op een voorganger.
- **Een board-status zonder run telt niet bij een blokkade.** `In progress` of `Controller` met een
  blokkade en zonder open run is `ready` met een kruis. Zo staat #379 niet meer op *active*.
- Een issue met `needs_mind` is nooit *active*. Geeft de controller een besluit terug, of stelt hij
  een vraag, dan staat het issue op *needs mind*. Na het antwoord springt het terug naar *active*.
  De gebruiker vindt dat goed: het is de echte flow.
- `needs_hands` met een open run is *needs hands*: de mens is aan zet, ook al loopt de run nog. De
  tick start bij een `needs_*`-label geen nieuwe run en zet het issue op `To do`, zoals nu.
- **Run-gegevens.** `/api/priorities` levert de runs al; `derive.ts` leest ze met `liveRun`. Go en
  web blijven dus één regel. De runs zijn die van de machine waarop `lighthouse serve` en de tick
  draaien, zoals nu.
- `lighthouse state show <nr>` print `state: #<nr> <status>`, en `, blocked` erachter bij een
  blokkade.
- `cases.json` krijgt minstens één geval per rij, plus: `needs_mind` met blokkade; `needs_hands`
  met blokkade; open run met blokkade; `In progress` met blokkade zonder run; `parked` run met
  `blocked_issue`.

### 3. Vormentaal

Het blokje heeft twee kanalen: de **vorm** zegt de status, het **kruis** zegt *blocked*.

| Status | Vorm | Nummer onderstreept |
|---|---|---|
| needs mind | achtergrond van de kaart, stippelrand (`1.5px dashed` in `--foreground`) | ja |
| to do | lichtgrijs (`--lh-seg-todo`) | nee |
| active | strepen in de milestonekleur (`.lh-stripe`) | nee |
| needs hands | strepen in de milestonekleur, dichte rand (`inset 0 0 0 2px` in `--foreground`) | ja |
| done | vol in de milestonekleur | nee |

- **Blocked** is het vectorkruis van nu, óver de vorm heen. Needs mind met een blokkade is een
  stippelrand om een kruis; to do met een blokkade is grijs met een kruis.
- Het nummer is onderstreept als **een mens nu aan zet is**: *needs mind* altijd, *needs hands*
  altijd (die heeft per definitie geen blokkade).
- **Balkvolgorde** (Milestones): done · needs hands · active · to do · needs mind. Binnen *to do*
  staan de issues zonder kruis vóór die met kruis.

### 4. Legenda

Het woord *Status* staat alleen op de kanban, waar het de kolommen zijn. In *Dependencies* en
*Milestones* heeft de legenda drie regels, elk met een kopje:

- **Milestones**: de kleuren, zoals nu.
- **Progress**: to do · active · done.
- **Labels**: needs mind · blocked · needs hands, met hun vorm. Het label van *needs mind* en
  *needs hands* is onderstreept.

### 5. Skills, triage en tick

| Waar | Nu | Nieuw |
|---|---|---|
| `lighthouse-route-issue` | `needs_shaping` en `needs_decision` → gate 4c | `needs_mind` → gate 4c; is er al een spec of gaat het om één keuze, dan smal (de open keuze als eerste vraag) |
| `lighthouse-route-issue` gate 0 | een blokkadelabel wint van `needs_shaping` (`GateZero`) | `needs_mind` met een blokkade routeert door naar 4c, met `— blocker #<x> open`; spec ja, plan nee |
| `lighthouse-start-issue` board-start | overgeslagen bij `blocked_*` of een blocker zonder label | overgeslagen bij **elke** blokkade, ook met `needs_mind` |
| `lighthouse-start-issue` na goedgekeurde spec | `needs_shaping` eraf; sessie wisselt naar `blocked_issue` | alleen `needs_mind` eraf |
| `lighthouse-write-issue` 3c en 3e | zie §1 | zie §1 |
| triage (`internal/triage`) | 4c zet `needs_shaping`; met `needs_shaping`/`needs_decision` moet de route 4c zijn | zelfde met `needs_mind` |
| tick bij teruggeven | `reason_needs_brainstorm` → `needs_shaping`, `reason_interactive` → `needs_decision`, de rest → `needs_hands` | beide eerste → `needs_mind`; de rest ongewijzigd |
| tick `heldByHuman` | vier `needs_*` plus `blocked_external` | `needs_mind`, `needs_hands`, `blocked_external` |

### 6. Migratie

Eén commando, eenmalig per repo. Het toont eerst de lijst, en schrijft pas na bevestiging (zoals
de migratie van `On hold`):

- `3-needs-shaping` en `3-needs-decision` → `3-needs-mind`; `3-needs-testing` → `3-needs-hands`.
- Een issue met `3-needs-mind` en een open `blocked_by`, zonder `3-blocked-issue`, krijgt
  `3-blocked-issue` erbij (§1).
- Een issue op `In progress` of `Controller` met een blokkade en zonder open run: op de lijst, met
  het voorstel `To do`. Pas na bevestiging.
- Daarna de oude labels weg op de forge, en `[labels]` in `lighthouse.toml` naar de nieuwe sleutels.
- **Volgorde bij een upgrade.** De nieuwe binary weigert de oude sleutels in `[labels]` met een fout
  die het migratiecommando noemt. Het migratiecommando leest `lighthouse.toml` zelf, zonder die
  controle, en is het enige dat ermee werkt. *Mijn keuze:* geen stille alias (aanpak 2 is
  afgewezen), maar ook geen kapotte config zonder uitweg.

Op 24 november: 17 open issues met `3-needs-shaping` in deze repo, 0 met `3-needs-decision` of
`3-needs-testing`. #379 staat op `In progress` met een blokkade.

### 7. Kanban (#557)

Vijf kolommen: **needs mind · to do · active · needs hands · done**. *Blocked* is een kruis op de
kaart, geen kolom. De rest van §6 van het vorige ontwerp blijft (kaart, `controller`/`interactive`,
volgorde, done-kolom, filter op milestone). De kaart van een issue met blokkade toont daarnaast
`waits on 375` (zonder `#`), zoals de lijst onder *Without milestone*; bij `blocked_external`
`waits: external`.

## Opdeling

| | Inhoud | Hangt af van |
|---|---|---|
| a | Labels en migratie: `[labels]`, `issuestate` (assen, `Keys`, `GateZero`, `state label`), triage, tick, pack-skills, `labels.md`, migratiecommando. `Of` mapt `needs_mind` tijdelijk op de oude status `needs_shaping`, en een open 🤖 Vraag blijft *needs human*: `cases.json` verandert in a alleen van labelnamen. | — |
| b | Status en vorm: vijf statussen plus `Blocked` in `Of`, `cases.json` en `derive.ts`; blokje met kruis-overlay; legenda; balkvolgorde; `state show`. | a |
| c | #557 aanpassen en bouwen: vijf kolommen, `waits on` op de kaart. | b |

Na a draait de migratie op deze repo en op `personal_ai_agent`. #556 staat los en blijft zoals hij is,
behalve de balkvolgorde uit b.

## Aan de bouwer

- De naam en vorm van het migratiecommando (`lighthouse labels migrate` of onder `lighthouse state`).
- Hoe `Blocked` naar de web gaat: een veld in `/api/priorities` of berekend uit wat er al is
  (`label`, `blocked_by`). Wel dezelfde regel als Go.
- Hoe het kruis over de stippelvorm ligt (één SVG of twee lagen), zolang het in 14 × 14 px leesbaar
  blijft.
- Of `needs_label` in `/api/priorities` blijft of opgaat in de nieuwe velden.

## Keuzes die ik maakte

Neem deze door bij het lezen. De rest komt van de gebruiker.

1. Een open 🤖 Vraag geeft *needs mind* (nu *needs human*).
2. Een open run wint van een blokkade (*active* met kruis); `parked` telt niet als run.
3. `In progress` of `Controller` zonder run en met een blokkade is *to do* met kruis.
4. `needs_hands` met een blokkade toont geen *needs hands*: de blokkade wint in de status. Het label
   blijft wel staan; de rang haalt het niet meer weg. `needs_hands` met een open run is *needs hands*.
5. Legenda-kopjes *Milestones*, *Progress* en *Labels*; *Status* alleen op de kanban.
6. Binnen *to do* in de balk: zonder kruis vóór met kruis.
7. De migratie stelt `To do` voor bij een board-status met blokkade zonder run, en schrijft pas na
   bevestiging.
8. Opdeling in drie, met labels eerst; in a blijft de statusuitvoer gelijk.
9. De nieuwe binary weigert oude `[labels]`-sleutels en wijst naar het migratiecommando.
