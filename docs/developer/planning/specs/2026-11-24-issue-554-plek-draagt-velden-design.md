# De plek op de kaart draagt de velden die het document per soort item vraagt

Issue: #554. Voorganger: [gepland op de kaart](2026-11-22-issue-476-gepland-op-de-kaart-design.md)
(#476).

## Aanleiding

`write-issue` vraagt bij een vrij label met een document (stap 3f, *Planned on the map*) alleen
*waar hoort het?* en schrijft het antwoord als één vrije regel onder `### Op de kaart`. Niets zegt
welke gegevens die regel moet dragen. Run 883 van `map-planned` kreeg zo #527 met "Nieuwe handoff
in de loop" zonder `from` en `to`. De job mocht niets verzinnen en stelde een vraag. Na het
herschrijven tot `from` `close-issue`, `to` `close-followup` plaatste de job het item zonder vraag
(#553). De job werkt; de aanmaakkant levert te weinig.

## Doel

Het taxonomiebestand zegt per document en per soort item welke gegevens de plek draagt.
`write-issue` vraagt door tot de plek ze allemaal noemt. `map-planned` plaatst het item dan zonder
vraag en zonder te verzinnen, en de PR van de job blijft groen.

## Niet-doelen

- Geen gepland item voor een route zelf. Een route in `loop.json` heeft geen `status` en geen
  `issue`, en `ci/loop_roadmap` leest routes niet. Een geplande stap wordt wel in bestaande routes
  opgenomen (zie *Ontwerp*, 1).
- Geen wijziging aan de Go-job `map-planned` (`internal/scheduled/mapplanned`). Die werkt.
- Geen wijziging aan `ci/loop_roadmap`. De controle dat de veldentabel klopt met `loop.json`
  staat in de site-tests (zie *Bewaking*). *(eigen keuze na spec-review, r1-f3)*
- Geen nieuwe soorten items en geen nieuwe kaartsoorten (No-Go van milestone 17).
- Geen herschrijving van de plek van bestaande open `loop`-issues.

## Anti-doel

Het aanmaken van een issue wordt geen formulier, en `map-planned` schrijft geen beschrijving die
het issue niet heeft. `write-issue` vraagt alleen wat de plek bepaalt, plus de korte naam van een
nieuwe stap; de korte omschrijving komt uit het issue zelf; de rest van de beschrijving volgt
met de PR die de stap levert.

## Randvoorwaarden

- **Generiek** (AC2 van #476): `pack/core` noemt geen labelnaam, geen pad en geen veldnaam van
  `loop.json`. Alle veldnamen en regels per soort item staan in het taxonomiebestand van de repo.
- **Zonder regel zoals nu:** heeft het taxonomiebestand geen veldentabel, of geen rij voor dit
  label, dan blijft stap 3f zoals hij is.
- **Bestaande lezers breken niet.** `mapplanned.Rows` leest alleen tabellen met de kolommen
  `Label`, `Document` en `Planned`. `labels.ParseTaxonomy` leest alleen tabellen met een
  kleurkolom. Een nieuwe tabel zonder die kolommen laten beide met rust.
- **De site-tests blijven de kaart bewaken.** `docs/site/test/loop.test.mjs` eist nu op elke chip
  `id`, `phase`, `row`, `kind`, `trigger`, `label`, `what`, `how`, `when`, `without` en `status`;
  geen twee chips in dezelfde `(phase, row)`; elke chip op een route; elke handoff op een route met
  `from` vóór `to`. Alleen de eerste eis wordt losser (zie *Ontwerp*, 4); de rest blijft.
- **Dogfood:** `go run ./cmd/lighthouse init --upgrade`, de geïnstalleerde kopieën in `.claude/`
  en `.agents/` mee in de commit; CI eist `No changes.` Een fragment in `changelog.d/`.

## Aanpakken

**Vorm van de regel.**

- **A. Een zin in de bestaande uitleg.** Het goedkoopst. Of er een regel is, beoordeelt het
  model, en niets bewaakt dat de genoemde velden in het document bestaan.
- **B. Een veldentabel onder de uitleg (gekozen).** Een vaste vorm die een skill generiek kan
  herkennen ("staat er zo'n tabel met een rij voor dit label") en die een test kan bewaken tegen
  het document. Dat past bij het milestone-risico "een kaart die liegt".
- **C. Een kolom in de hoofdtabel.** Eén cel per label draagt niet meerdere soorten items;
  afgevallen.

Corneel koos B.

**Wat een geplande stap draagt.**

- **X. Licht (gekozen).** De plek plus `label` en `what`; `how`, `when` en `without` worden
  optioneel bij `status: planned`, in de site-tests en in `map.js`.
- **Y. Volledig.** Ook `how`, `when` en `without` uit het issue; de site-tests blijven gelijk.
  Afgevallen: het issue heeft die tekst vaak nog niet, en `map-planned` zou hem verzinnen.

Corneel koos X. Eerder viel ook "`write-issue` vraagt ook de tekst van de stap" af: het aanmaken
blijft licht.

**Naam en omschrijving van een geplande stap** (na spec-review, r1-f1 en r1-f2). De korte naam
staat in geen issue, en issues zijn Nederlands terwijl de kaart Engels is. Opties: de naam vragen
en `what` laten vertalen; naam en `what` allebei vragen; alleen de naam vragen en `what`
Nederlands laten. Corneel koos de eerste: `label` staat onder `Place`, `what` onder `From issue`,
en vertalen naar de taal van het document telt als overnemen.

## Ontwerp

### 1. De veldentabel in het taxonomiebestand

Onder de uitleg in *Vrije labels met een document* (`docs/developer/labels.md`) komt een tabel.
De koppen zijn Engels, zoals `When` en `Planned` in de hoofdtabel, zodat een skill hem in elke
repo herkent:

| Label | Item | Place | From issue |
|---|---|---|---|
| `loop` | stap (`chips`) | `phase`, `after`, `kind`, `trigger`, `label` | `what` |
| `loop` | handoff (`handoffs`) | `from`, `to` | |

- **Item**: de soort item in de taal van het artefact, met tussen haakjes en backticks de plek in
  het document waar zo'n item staat. Voor `loop.json` is dat de array.
- **Place**: wat de plek onder `### Op de kaart` noemt. `write-issue` vraagt het.
- **From issue**: de velden die `map-planned` uit titel en body van het issue overneemt, in de
  taal van het document. Overnemen en vertalen gelden niet als verzinnen. Leeg: het item heeft er
  geen.
- `status` en `issue` staan er niet in: die zet `map-planned` altijd (`planned`, het nummer).

De uitleg erboven krijgt per soort item zinnen die hetzelfde zeggen in woorden (AC1), plus de
regels die de job nodig heeft om zonder vraag te plaatsen:

- **Stap.**
  - `phase` is een `id` uit `phases`; `kind` en `trigger` zijn waarden die andere chips al dragen.
  - `after` is geen veld van de chip maar een verwijzing: het `id` van de stap waarna de nieuwe
    komt. De job neemt de nieuwe stap op in elke route waarvan `chips` dat `id` bevat, direct na
    het eerste voorkomen. Een `after` die op geen route staat, of waarvan de fase ná `phase` ligt,
    is geen geldige plek. *(eigen keuze na spec-review, r1-f8)*
  - `row` is de eerstvolgende vrije rij in die fase; andere chips schuiven niet.
  - `label` is een korte Engelse naam in kleine letters met streepjes, zoals de bestaande `id`'s;
    `write-issue` vraagt hem. `id` is gelijk aan `label`; een `label` dat al een `id` is, is geen
    geldige plek.
  - `what` is één Engelse zin die de job uit titel en body van het issue overneemt.
  - `how`, `when` en `without` blijven weg tot de PR die de stap levert.
- **Handoff.** `from` en `to` zijn `id`'s van stappen die samen op een route liggen, `from` eerst.
  Een stap die hetzelfde issue nieuw toevoegt, telt mee met zijn `label`, dat zijn `id` wordt.
- **Route.** Krijgt nog geen gepland item.
- **Vorm.** Eén regel per item onder `### Op de kaart`:
  `<item>: <veld> <waarde>, <veld> <waarde>`, bijvoorbeeld
  `handoff: from close-issue, to close-followup`. Een issue met een nieuwe stap en een handoff
  ernaartoe heeft twee regels; elke regel is compleet.

### 2. `write-issue` stap 3f

Beide varianten (`pack/core/skills/write-issue/claude/SKILL.md` en `…/agents/SKILL.md`). Nieuw,
na *Where does it belong?*, in generieke woorden:

- Heeft het taxonomiebestand een tabel met de kolommen `Label`, `Item` en `Place` met rijen voor
  dit label, dan zijn die rijen de soorten items. Vraag per item welke soort, dan door tot de plek
  elk gegeven uit `Place` van die rij noemt, met een waarde die geldig is volgens de uitleg.
- Schrijf elke plek als een eigen regel, in de vorm die de uitleg geeft.
- `no item` blijft altijd een kandidaat.
- **Headless:** schrijf een regel alleen als ze elk gegeven uit `Place` noemt met een geldige
  waarde; anders geen regel, en de job vraagt.
- Zonder zo'n tabel, of zonder rij voor dit label: de stap zoals nu.

De vaste zinnen die `internal/packcontract/writeissue_test.go` pint, blijven staan.

### 3. `map-planned`

`pack/core/skills/map-planned/SKILL.md`, stap 3, *Unambiguous place*: nu "nothing else invented".
Erbij, generiek:

- de velden onder `From issue` neemt de job over uit titel en body van het issue, in de taal van
  het document (vertalen mag);
- wat de uitleg afleidt (bij `loop.json`: `row`, `id`, de opname in routes) doet de job zoals de
  uitleg zegt;
- velden die de tabel niet noemt en de uitleg niet afleidt, laat de job weg;
- een plek met een ongeldige waarde (volgens de uitleg) is een onduidelijke plek: de job vraagt.

Geen veldnaam en geen pad in de skill; de voorbeelden tussen haakjes hierboven staan alleen in
deze spec.

### 4. De kaart en zijn tests

- `docs/site/test/loop.test.mjs`: `how`, `when` en `without` zijn niet verplicht bij
  `status: planned`; bij `works` en `idea` blijven ze verplicht. Een test bewijst beide
  richtingen. *(eigen keuze na spec-review, r1-f7)*
- `docs/site/map.js`: een chip zonder `how`, `when` of `without` rendert zonder fout; het
  detailpaneel toont voor een ontbrekende `how` of `without` "not described yet" (de site is
  Engels). `when` toont het paneel nu niet; dat blijft zo. *(eigen keuze na spec-review, r1-f6)*

### 5. Het contract voor andere repo's

`docs/tick.md` beschrijft naast de kolommen `Document` en `Planned` ook de veldentabel: de koppen
`Label`, `Item`, `Place` en `From issue`, wat elk betekent, en dat zonder tabel stap 3f blijft
zoals hij was. *(eigen keuze na spec-review, r1-f9)*

### 6. Bewaking

- **Generiek in `pack/core`:** een packcontract-test faalt als `write-issue` (beide varianten) of
  `map-planned` `loop.json`, of een van de vaste namen `phase`, `after`, `kind`, `trigger`,
  `from`, `to`, `row` en `what`, tussen backticks noemt. De lijst staat vast in de test:
  `internal/packcontract` leest alleen `pack/`, niet `docs/`. *(eigen keuze na spec-review,
  r1-f4, r1-f10)* Corpusvelden van `map-planned` (zoals `label`, `issue`, `document`) vallen
  erbuiten. `mapplanned_test.go` verbiedt al `loop`, `kaart`, `loop.json` en `kaart.yaml` in
  `map-planned`.
- **Nieuwe zinnen gepind:** de packcontract-test van stap 3f pint de zinnen over de veldentabel,
  één regel per item en headless alleen-compleet, in beide varianten; die van `map-planned` de zin
  over `From issue`.
- **De tabel liegt niet:** een test in `docs/site/test/loop.test.mjs` leest de veldentabel uit
  `docs/developer/labels.md` en faalt als een array uit `Item` niet in `loop.json` staat, of een
  veld uit `Place` of `From issue` op geen enkel item van die array voorkomt. `after` is geen veld
  en is uitgezonderd; de test kent die uitzondering bij naam. De site-job draait bij elke PR, ook
  een PR die alleen `loop.json` raakt. *(eigen keuze na spec-review, r1-f3)*

## Keuzes van de schrijver

Na de lezing door een reviewer, zonder aparte vraag aan Corneel; corrigeer bij het lezen:

- `row` is de eerstvolgende vrije rij; geen verschuiving van andere chips.
- `after` bepaalt de opname in routes; dat raakt `chips` van routes, geen gepland route-item.
- `id` gelijk aan `label` (na spec-review vraagt `write-issue` het `label`; zie *Aanpakken*).
- Een handoff moet op een route liggen; `write-issue` toetst dat, headless gaat een ongeldige
  regel niet door, en `map-planned` vraagt bij een ongeldige plek.
- Meer items per issue mag, één regel per item.
- De verboden-woordentest dekt `loop.json` en de `Place`-gegevens, niet de corpusvelden.

## Aan de bouwer

- De precieze Engelse zinnen in stap 3f en `map-planned`, binnen de vaste zinnen die tests pinnen.
- De Nederlandse zinnen in de uitleg van `labels.md`.
- Hoe `map.js` "not described yet" vormgeeft.
