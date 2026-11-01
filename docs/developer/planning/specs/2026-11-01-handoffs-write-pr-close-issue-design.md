# Handoffs bouw→write-pr en merge→close-issue, met de regel voor een handoff

Issue: #147. Breidt *Decide* regel 5 en *Handoffs* uit van de
[M2-spec](2026-10-27-m2-dispatcher-design.md) (#114). Zie ook #119 (fix-pr) en #142 (naamgeving).

## Waarom

De dispatcher start elke volgende stap van de keten in een verse sessie, mits er een handoff
staat. De eigenaar legde op 2026-10-31 vast waar een handoff hoort:

> Een handoff staat waar de volgende stap een schone context nodig heeft: de vorige stap maakte
> veel context op, of de volgende stap moet onafhankelijk oordelen. Ook staat er een handoff waar
> de keten wacht op een externe gebeurtenis, zoals rode CI of een merge.

Twee plekken voldoen aan die regel en hebben nog geen handoff:

- **Bouw klaar → write-pr.** De bouwsessie heeft veel context opgemaakt, en het toetsen van de
  acceptatiecriteria hoort onafhankelijk te zijn van wie bouwde. Nu draait de bouwsessie
  `/lighthouse-write-pr` zelf (`internal/dispatch/instructions/run.md`, "Before push + PR").
- **Merge → close-issue.** De keten wacht op de merge, een externe gebeurtenis. Na een `Refs`-PR
  start de tick al `/lighthouse-close-issue N` (regel 5, *Nacontrole*). Na een `Closes`-PR niet:
  het issue is bij de merge al gesloten en het board zet het op `Done`, waar decide niet kijkt.
  Dat was besluit r1-f10 van de M2-spec: ingang C van close-issue is aan de mens.

Naamgeving: het issue noemt `/lighthouse-verify-pr` en `/lighthouse-instrument`. Na #142 heten die
`/lighthouse-write-pr` en `/lighthouse-place-instruction`. De handoff-skill staat in
`pack/core/skills/write-handoff/SKILL.md`. Op de kaart heten de stappen `commit-test-gate`,
`write-pr`, `pr-and-ci` en `close-issue`.

## Doelen

- Een bouwsessie die klaar is, met de laatste commit groen, post `next: /lighthouse-write-pr <nr>`.
  Decide start dat in een verse sessie.
- Write-pr schrijft altijd `Refs #N`, nooit `Closes`: in een controllerrun en interactief.
- Na de merge van een PR die de controller opende, start de tick `/lighthouse-close-issue N`.
- Interactief eindigt write-pr met een handoffblok met `/lighthouse-close-issue <nr>`, voor na de
  merge.
- Een PR die een mens zelf met `Closes` opende, start na de merge niets (zoals nu).
- De regel voor een handoff staat in de handoff-skill.
- De kaart, de routeringstabellen van de handoff-skill en de M2-spec zeggen hetzelfde.

## Niet-doelen

- De handoff pr-and-ci→fix-pr. Die hoort bij #119. Die staat nog als `planned` op de kaart; dat
  rechtzetten valt buiten dit issue.
- Een nacontrole voor een `Closes`-PR. Gesloten issues op `Done` blijven buiten gather en decide.
- Interactieve sessies dwingen om een handoff te posten. De skill biedt hem aan; de mens kiest.
- `Closes` weghalen uit `pr-create-gate` of ingang C uit close-issue. Een mens kan nog een
  `Closes`-PR openen; gate en ingang C blijven daarvoor.

## Anti-doelen

- Een issue dat de controller nooit aanraakte, krijgt na een merge toch een run.
- De dispatcher gaat de kolom `Done` lezen, met de kosten en de achterstand die daarbij horen.

## Randvoorwaarden

- Het board zet een gesloten issue zelf op `Done` (bevestigd door de eigenaar).
- Gather houdt alleen items op `Controller` en `On hold` over (`dispatchStatuses` in
  `internal/dispatch/gather.go`).
- Regel 5 bestaat al voor een `Refs`-PR: gemerged, issue open en op `Controller`, een 🤖 Gestart
  vóór de merge, sleutel `closecheck:<pr>` nog niet besteed (`closeCheck` in
  `internal/dispatch/decide.go`). Close-issue draait dan B7.
- Lighthouse merget nooit (No-Go van M2).
- Dogfood: na de skillwijziging geeft `go run ./cmd/lighthouse init --upgrade` `No changes.`, met
  de geïnstalleerde kopie gecommit.

## Aanpakken

Voor bouw→write-pr is er één zinnige vorm: de bestaande `handoff`-regel van decide, zoals
brainstorming→review-spec. Een eigen trigger, zoals `fixpr` in #119, is niet nodig: de bouwsessie
weet zelf wanneer ze klaar is en kan het zeggen.

Voor merge→close-issue zijn vier aanpakken overwogen:

| | Aanpak | Afweging |
|---|---|---|
| A | Gather leest ook `Done`; decide zet een gesloten issue met een gemergde `Closes`-PR terug op `Controller`, start de nacontrole en zet het daarna terug op `Done`. | Werkt met `Closes`, maar gather moet de grootste kolom lezen, er is een grens tegen achterstand nodig, en een heropening in ingang C laat de run open. |
| B | Starten op `Done`, zonder de status te wijzigen. | Zelfde gather-probleem als A, en breekt r1-f15: een vraag van die run valt buiten het overzicht. |
| C | De board-automatisering uitzetten. | Valt buiten lighthouse. |
| **D** | Write-pr schrijft altijd `Refs #N`, nooit `Closes`. Het issue blijft na de merge open (en in een controllerrun op `Controller`), en regel 5 start de nacontrole zoals nu. Interactief geeft write-pr een handoffblok voor close-issue. | Geen Go-wijziging voor dit deel. Het issue sluit pas na de nacontrole, niet bij de merge, en GitHub legt geen sluitkoppeling tussen PR en issue. |

**Gekozen: D** (voorstel van de eigenaar, 2026-11-01; ook interactief, op zijn aanwijzing). Het eerste ontwerp koos A; de builder-check
liet zien dat gather `Done` niet leest, en D maakt die hele tak overbodig.

## Ontwerp

### 1. Bouw klaar → write-pr

**Run-instructies** (`internal/dispatch/instructions/run.md`):

- Een bouwsessie (een run die routes 1, 2, 4a, 4b of 4c uitvoert) opent zelf geen PR meer. Is de
  bouw klaar en de laatste commit groen, dan post ze met `/lighthouse-write-handoff`:
  `next: /lighthouse-write-pr <nr>`. De handoff-skill pusht de branch zelf (Workflow stap 3).
- "Groen" betekent: `commit-test-gate` meldde na de laatste commit geen rood. Is de laatste
  commit rood, dan herstelt de sessie dat eerst in een nieuwe commit (bestaande regel).
- Bij `finishing-a-development-branch`: niet push + PR, maar de handoff hierboven. Die regel gaat
  boven de keuzes van die skill, zoals "Never wait for CI" nu boven "stop at green" gaat.
- Een run die met `/lighthouse-write-pr <nr>` start, toetst de acceptatiecriteria, opent de PR met
  de body via `--body-file`, en post `next: /lighthouse-close-issue <nr>`. Daarna stopt hij.

**Judge** (`internal/dispatch/judge.go`):

- `/lighthouse-write-pr` komt op de allowlist.
- Het argument is het issuenummer, gelijk aan het issue van de handoff, zoals bij
  `/lighthouse-start-issue`.
- `installed` moet `lighthouse-write-pr` bevatten, anders `VerdictSkillMissing` (bestaand gedrag).

De start gaat via de bestaande `handoff`-regel en trigger `handoff`. Er is geen nieuwe trigger en
geen migratie nodig.

### 2. Merge → close-issue

**Write-pr** (`pack/core/skills/write-pr/SKILL.md`), overal:

- De koppeling naar het issue is altijd `Refs #N — <reden>`, ook als elk criterium
  `demonstrably done` is. De reden zegt dat `/lighthouse-close-issue` na de merge toetst en sluit.
  De uitkomst *Closes* en het mengen van `Closes #5` met `Refs #6` verdwijnen uit de skill.
- De Evidence-tabel blijft in de body, met een rij per criterium. Ze is het startpunt van de
  nacontrole. De checks op die tabel (`tests/pack/test_evidence.py`, `test_pr_body_gate.py`) moeten
  haar onder `Refs` accepteren; nu horen ze bij `Closes`.
- `pr-create-gate` accepteert `Refs #N — <reden>` al. De `Closes`-vorm blijft geldig voor een
  mens die zelf een PR opent.
- **Interactief**, na `gh pr create`: write-pr eindigt met `/lighthouse-write-handoff`, eerste stap
  `/lighthouse-close-issue <nr>`, uit te voeren na de merge. Dat is de bestaande rij "work done,
  issue can be closed" van Workflow stap 4. Close-issue kiest dan ingang B, want het issue is open.
- **Controllerrun**: geen eigen blok; de run post `next: /lighthouse-close-issue <nr>` zoals in
  deel 1. Een controllerrun herkent write-pr aan de bestaande signalen: de prompt eindigt op
  `(issue #<nr>, controller-run)` of `LIGHTHOUSE_CONTROLLER_RUN` staat in de omgeving.

**Decide.** Geen wijziging. Na de merge van de `Refs`-PR is het issue open en op `Controller`, en
staat er een 🤖 Gestart vóór de merge. `closeCheck` start `/lighthouse-close-issue N` met sleutel
`closecheck:<pr>`. Close-issue draait B7: het sluit met bewijs, of laat het issue open met wat
ontbreekt.

**Een `Closes`-PR** van een mens op een controller-issue: het issue sluit bij de merge, het board
zet het op `Done`, decide ziet het niet meer. Er start niets, zoals nu. De bestaande fixture
`closecheck-closes-merged.json` (`"want": []`) pint dat al.

### 3. De regel in de handoff-skill

- De regel uit *Waarom* komt letterlijk in `pack/core/skills/write-handoff/SKILL.md`. De plek in
  de skill kiest `/lighthouse-place-instruction` tijdens de bouw. Verwachting: een korte sectie
  vóór Workflow stap 4, omdat stap 4 de tabel is die de regel toepast.
- **Workflow stap 4** krijgt een rij: "bouw klaar, laatste commit groen, nog geen PR" →
  `/lighthouse-write-pr <nr>`, kolom Controller `offer`.
- **Controller-modus stap 3** (`next:`-tabel) krijgt een rij: "bouw klaar, laatste commit groen"
  → `/lighthouse-write-pr <nr>`.
- De rij "PR open → `/lighthouse-close-issue <nr>`" zegt nu "the controller never starts this, it
  waits for green". Nieuwe tekst: de controller start het niet uit de handoff, maar na de merge
  van de `Refs`-PR als nacontrole.

### 4. Kaart en M2-spec

- `docs/site/data/chain.json`: `commit-test-gate`→`write-pr` en `pr-and-ci`→`close-issue` krijgen
  status `works`, zonder `issue`.
- `tests/test_site_chain.py`, `test_works_handoffs_follow_the_handoff_table`: de verwachte set
  krijgt die twee paren. De test eist al dat `` `/lighthouse-write-pr `` en
  `` `/lighthouse-close-issue `` in de handoff-skill staan.
- M2-spec, *Decide* regel 5 *Nacontrole*: een controllerrun opent altijd een `Refs`-PR, dus regel
  5 dekt elke PR van de controller. De zin over `Closes` blijft, met als reden dat alleen een mens
  nog een `Closes`-PR opent. r1-f10 staat als *herzien in #147*, met de oude keuze leesbaar erbij.

## Bewijs

De acceptatiecriteria 2 en 3 van #147 spreken over een `Closes`-PR. Met aanpak D worden ze, na
akkoord van de eigenaar, in het issue aangepast:

| AC | Bewijs |
|---|---|
| 1 | Snapshot-fixture: een 🤖 Handoff met `next: /lighthouse-write-pr 7` op een issue in `Controller` geeft een start van `/lighthouse-write-pr 7`. Plus judge-tests voor de allowlist en het argument (ander nummer → afgewezen). |
| 2 (nieuw) | Write-pr schrijft altijd `Refs #N`, ook als alles `demonstrably done` is, en eindigt interactief met een handoff naar `/lighthouse-close-issue <nr>`: een test op de skilltekst, en een snapshot-fixture waarin een gemergde `Refs`-PR van een controller-issue `/lighthouse-close-issue 7` start (bestaande `closecheck-refs-merged.json` volstaat als die dit dekt). |
| 3 (nieuw) | Een gemergde `Closes`-PR start niets: `closecheck-closes-merged.json`. |
| 4 | De M2-spec, regel 5 en r1-f10. |
| 5 | De regel in de handoff-skill. |
| 6 | `chain.json`, beide tabelrijen, en `test_works_handoffs_follow_the_handoff_table`. |
| 7 | `go run ./cmd/lighthouse init --upgrade` geeft `No changes.` |

Elke test faalt als de bijbehorende wijziging weg is: zonder de allowlist-regel wijst judge de
handoff af (AC 1); zonder de controllerregel in write-pr verdwijnt de tekst waar de skilltest op
let (AC 2).

Dit verandert het gedrag van write-pr in elke repo die het pack installeert. Het
`changelog.d/`-fragment zegt dat.

## Aan de bouwer

- De precieze tekst in `run.md`, write-pr en de handoff-skill, binnen de regels hierboven. Taal:
  Engels (instructies), heading-sleutels via `.lighthouse/headings.toml`.
- Of de skilltest voor AC 2 in `tests/` (Python, zoals `test_site_chain.py`) of in
  `internal/packs/` (Go) hoort; volg waar de bestaande tests op skilltekst staan.
- Of `instructions_test.go` (pint `run.md`) nieuwe of gewijzigde verwachtingen krijgt.
- Welke tests in `tests/pack/` (`test_write_pr_skill.py`, `test_evidence.py`,
  `test_pr_body_gate.py`, `test_pr_template.py`) een `Closes`-body als goede uitkomst van write-pr
  pinnen, en hoe ze omgaan naar `Refs`. Een test die de gate op een menselijke `Closes`-PR pint,
  blijft.
- Of `chain.json` elders zegt dat close-issue na een `Closes`-merge de rijen opnieuw meet (o.a. de
  write-pr-chip); die tekst gaat naar de nacontrole na een `Refs`-merge.
