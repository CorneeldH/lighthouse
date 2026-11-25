# De plek op de kaart draagt de velden per soort item Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Het taxonomiebestand zegt per label en soort item welke gegevens de plek draagt; `write-issue` vraagt door tot de plek ze noemt, `map-planned` plaatst zonder vraag, en de site-tests bewaken de tabel (issue #554).

**Architecture:** Eerst de kaart en zijn tests losser voor geplande stappen (taak 1 en 2), dan de veldentabel met de test die hem tegen `loop.json` houdt (taak 3), dan de generieke skillregels met hun packcontract-tests (taak 4 en 5), tot slot het contract in `docs/tick.md`, het changelogfragment en de dogfood (taak 6). Geen Go-wijziging buiten tests.

**Tech Stack:** Markdown (taxonomie, skills, docs), Node `node:test` (`docs/site/test`), Go (`internal/packcontract`), vanilla JS (`docs/site/map.js`).

**Spec:** [2026-11-24-issue-554-plek-draagt-velden-design.md](../specs/2026-11-24-issue-554-plek-draagt-velden-design.md)

## Global Constraints

- `pack/core` noemt geen labelnaam, geen pad en geen veldnaam van `loop.json`; alles per soort item staat in `docs/developer/labels.md`.
- Zonder veldentabel, of zonder rij voor het label, blijft stap 3f van `write-issue` zoals hij is.
- Kolomkoppen van de nieuwe tabel, Engels en exact: `Label`, `Item`, `Place`, `From issue`. De tabel heeft geen kolommen `Document`, `Planned` of een kleurkolom (anders lezen `mapplanned.Rows` of `labels.ParseTaxonomy` hem mee).
- Rijen: `loop` / stap (`chips`) / `phase`, `after`, `kind`, `trigger`, `label` / `what`; `loop` / handoff (`handoffs`) / `from`, `to` / leeg.
- `status` en `issue` staan niet in de tabel. `after` is geen veld van de chip en is in de bewakingstest bij naam uitgezonderd.
- Regelvorm onder `### Op de kaart`: `<item>: <veld> <waarde>, <veld> <waarde>`, één regel per item; voorbeeld `handoff: from close-issue, to close-followup`.
- UI-tekst van de site is Engels: ontbrekende `how` of `without` toont `not described yet`.
- Bestaande vaste zinnen die `internal/packcontract/writeissue_test.go` en `mapplanned_test.go` pinnen blijven staan; `map-planned` bevat de woorden `loop`, `kaart`, `loop.json`, `kaart.yaml` niet (case-insensitief, ook niet als deel van een woord).
- Skills zijn Engels; artefacten (labels.md, tick.md, changelog) Nederlands. Beide `write-issue`-varianten (`claude`, `agents`) krijgen dezelfde tekst; ze verschillen alleen in de "Generated with"-regel.
- Voor elke commit: `[test_gate] command` uit `lighthouse.toml` en `lighthouse imports check`. Site-tests: `node --test "docs/site/test/*.test.mjs"`; packcontract: `go test ./internal/packcontract/`.

## Review Focus

- Een `planned` chip zonder `how`/`when`/`without` valt niet om (site-test, `map.js`); een `works` chip zonder die velden blijft fout. Taak 1 en 2.
- Een veldentabel die een veld noemt dat op geen chip van `chips` voorkomt, of een array die niet in `loop.json` staat, laat de site-test falen. Taak 3.
- De veldentabel breekt `mapplanned.Rows` en `labels.ParseTaxonomy` niet: de job plaatst nog steeds. Taak 3 (bestaande Go-tests blijven groen).
- Een skill die `loop.json` of een vaste veldnaam tussen backticks noemt, faalt de generieke test. Taak 4.
- Headless `write-issue` schrijft geen onvolledige regel; `map-planned` vraagt bij een ongeldige waarde. Gepind in taak 4 en 5.

---

### Task 1: Site-tests: `how`, `when`, `without` optioneel bij `planned`

**Files:**
- Modify: `docs/site/test/loop.test.mjs:18` (REQUIRED) en `vocabularyProblems`
- Test: `docs/site/test/loop.test.mjs`

**Interfaces:**
- Produces: `vocabularyProblems(chain)` eist `how`, `when`, `without` niet bij `status: "planned"`, wel bij `works` en `idea`. Constanten `REQUIRED` (altijd) en `REQUIRED_UNLESS_PLANNED = ["how", "when", "without"]`.

- [ ] **Step 1: Schrijf de falende test** `vocabularyProblems loosens how, when and without only for planned` in `loop.test.mjs`, via `fixtureCases` op de fixture: (a) een chip met `status = "planned"` en `how`, `when`, `without` verwijderd geeft `[]`; (b) dezelfde chip met `status = "works"` geeft `["<id>: lacks 'how'", "<id>: lacks 'when'", "<id>: lacks 'without'"]`; (c) `status = "idea"` idem; (d) een `planned` chip zonder `what` geeft nog `"<id>: lacks 'what'"`. Gebruik `planned-issue` of een andere chip uit `test/fixtures/map.json`; pas bij (a) ook `issue` aan als de fixture-chip er geen heeft (anders vangt `plannedProblems` dat, niet deze check).
- [ ] **Step 2:** Run `node --test docs/site/test/loop.test.mjs`. Verwacht: FAIL op (a).
- [ ] **Step 3: Pas `vocabularyProblems` aan** in `docs/site/test/loop.test.mjs`: splits `REQUIRED` in `REQUIRED` zonder `how`, `when`, `without` en `REQUIRED_UNLESS_PLANNED`; de lus eist de tweede lijst alleen als `chip.status !== "planned"`.
- [ ] **Step 4:** Run `node --test "docs/site/test/*.test.mjs"`. Verwacht: PASS.
- [ ] **Step 5: Commit** `test(site): how, when en without optioneel bij een geplande stap (#554)`.

### Task 2: `map.js`: een chip zonder `how` of `without` rendert

**Files:**
- Modify: `docs/site/map.js` (`showChip`, rond regel 1044-1048, en `steps` rond 754)
- Test: de bestaande site-test die `map.js` laadt; bekijk eerst met `grep -ln "map.js" docs/site/test/*.mjs` welke dat is (waarschijnlijk `pages.test.mjs` of `loop`-gerelateerd). Bestaat er geen test die `showChip` draait, voeg dan een kleine test toe in de DOM-stijl van de buurtests.

**Interfaces:**
- Produces: `steps(how)` geeft bij `undefined`/leeg een `<p>` met `not described yet`; `showChip` toont `["What you miss without", chip.without || "not described yet"]`. `when` blijft ongebruikt in het paneel.

- [ ] **Step 1: Schrijf de falende test**: een chip `{ id, phase, row, kind, trigger, label, what, status: "planned", issue: 1 }` zonder `how` en `without`, door `showChip` (of de kleinste publieke ingang die het paneel vult) gerenderd, geeft een paneel dat zowel onder *How it works* als onder *What you miss without* de tekst `not described yet` toont en niet gooit. Een chip met beide velden toont ze ongewijzigd.
- [ ] **Step 2:** Run `node --test "docs/site/test/*.test.mjs"`. Verwacht: FAIL (lege tekst of fout).
- [ ] **Step 3: Pas `steps` en `showChip` aan** zoals bij Interfaces; geen andere plek in `map.js` leest `how`, `when` of `without` van een chip (controleer met grep).
- [ ] **Step 4:** Run de site-tests. Verwacht: PASS.
- [ ] **Step 5: Commit** `feat(site): chip zonder how of without toont "not described yet" (#554)`.

### Task 3: De veldentabel in `labels.md` en de test die hem tegen `loop.json` houdt

**Files:**
- Modify: `docs/developer/labels.md` (sectie *Vrije labels met een document*, na de alinea over `Planned`)
- Test: `docs/site/test/loop.test.mjs`

**Interfaces:**
- Produces: in `loop.test.mjs` de functie `fieldTableProblems(labelsMd: string, loop: object) -> string[]`, plus een test die haar op `docs/developer/labels.md` en `load("loop.json")` draait.

- [ ] **Step 1: Schrijf de falende tests.**
  - `fieldTableProblems` op een kleine inline markdown + mini-`loop`-object, met deze gevallen en problem-tekst: tabel klopt → `[]`; `Item` verwijst naar `(\`nope\`)` dat niet in het object staat → `["nope: not an array of loop.json"]`; een `Place`-veld dat op geen enkel element van die array voorkomt → `["chips: place field 'zzz' on no item"]`; idem `From issue` → `["chips: from-issue field 'zzz' on no item"]`; het veld `after` in `Place` wordt niet geflagd (uitzondering bij naam, met een commentaar waarom); een markdown zonder tabel met de koppen `Label`, `Item`, `Place`, `From issue` → `[]` (geen tabel is geen fout).
  - Een test `labels.md field table matches loop.json` die `fieldTableProblems(fs.readFileSync(<ROOT>/docs/developer/labels.md), load("loop.json"))` gelijk aan `[]` eist én eist dat de tabel bestaat (anders slaagt de test leeg): minstens de rijen voor `chips` en `handoffs`.
- [ ] **Step 2:** Run `node --test docs/site/test/loop.test.mjs`. Verwacht: FAIL (tabel ontbreekt).
- [ ] **Step 3: Implementeer `fieldTableProblems`** in `loop.test.mjs`: vind de tabel aan zijn koptekstrij (`| Label | Item | Place | From issue |`), lees per rij de array uit de backticks tussen haakjes in `Item`, de velden uit de backticks in `Place` en `From issue`; een veld telt als aanwezig als minstens één element van `loop[array]` die sleutel heeft.
- [ ] **Step 4: Schrijf de tabel en de uitleg in `labels.md`** (Nederlands), onder *Vrije labels met een document*: de tabel uit Global Constraints (de rijen letterlijk), een toelichting per kolom (`Item`: soort item met de plek in het document tussen haakjes; `Place`: wat de plek onder `### Op de kaart` noemt en `write-issue` vraagt; `From issue`: wat `map-planned` uit titel en body overneemt, vertalen telt als overnemen; `status` en `issue` zet de job altijd), en de uitleg per soort item uit spec *Ontwerp* 1 (stap: `phase`, `kind`, `trigger`, `after` als verwijzing en opname in routes direct na het eerste voorkomen, `row` is de eerstvolgende vrije rij, `label` kort en Engels in kleine letters met streepjes, `id` gelijk aan `label` en nog niet in gebruik, `what` één Engelse zin, `how`/`when`/`without` blijven weg; handoff: `from`/`to` zijn `id`'s op één route, `from` eerst, ook van een stap in hetzelfde issue; route: geen gepland item; vorm: één regel per item met het voorbeeld). De bestaande zin "Een `planned` item zonder `issue` mag niet" en de rest blijven staan.
- [ ] **Step 5:** Run `node --test "docs/site/test/*.test.mjs"` en `go test ./internal/scheduled/mapplanned/ ./internal/labels/...`. Verwacht: PASS (de nieuwe tabel heeft geen `Document`/`Planned`/kleurkolom).
- [ ] **Step 6: Commit** `docs(labels): veldentabel per soort item, bewaakt tegen loop.json (#554)`.

### Task 4: `write-issue` stap 3f vraagt door tot de plek compleet is

**Files:**
- Modify: `pack/core/skills/write-issue/claude/SKILL.md:266` en `pack/core/skills/write-issue/agents/SKILL.md` (zelfde alinea, stap 3f *Planned on the map*)
- Create: `internal/packcontract/placefields_test.go`
- Modify: `internal/packcontract/writeissue_test.go` (nieuwe test naast `TestWriteIssueFreeLabelsWithADocument`)

**Interfaces:**
- Consumes: de tabelkoppen en regelvorm uit taak 3.
- Produces: `TestSkillsNameNoFieldOfTheDocument` (generiek, ook gebruikt door taak 5) en `TestWriteIssuePlaceCarriesTheFieldsOfTheTable`.

- [ ] **Step 1: Schrijf de falende tests.**
  - `TestWriteIssuePlaceCarriesTheFieldsOfTheTable` in `writeissue_test.go`: per variant, `flat(section(t, body, "#### 3f. Free labels with a document", "### 4. Create the issue"))`, `wantAll` op de vaste zinnen die je in stap 3 kiest en hier letterlijk pint, minstens (a) de tabel met de kolommen `Label`, `Item` en `Place` en rijen voor dit label zijn de soorten items; (b) vraag per item de soort en ga door tot de plek elk gegeven uit `Place` noemt met een geldige waarde volgens de uitleg; (c) schrijf elke plek als een eigen regel in de vorm die de uitleg geeft; (d) `no item` blijft altijd een kandidaat; (e) headless: schrijf een regel alleen als ze elk gegeven uit `Place` noemt, anders geen regel; (f) zonder tabel of zonder rij voor dit label: de stap zoals nu. De bestaande test blijft ongewijzigd groen.
  - `placefields_test.go`: `TestSkillsNameNoFieldOfTheDocument` loopt over `variants(t, writeIssue)` en `variants(t, mapPlanned)` en faalt als een `codeSpanRe`-treffer in de tekst gelijk is aan `loop.json` of aan een van de vaste namen `phase`, `after`, `kind`, `trigger`, `from`, `to`, `row`, `what`. De lijst staat vast in de test, met een commentaar dat `internal/packcontract` alleen `pack/` leest. Corpusvelden van `map-planned` (`label`, `issue`, `document`, …) staan erbuiten.
- [ ] **Step 2:** Run `go test ./internal/packcontract/ -run 'PlaceCarries|NameNoField'`. Verwacht: de eerste FAIL (zinnen ontbreken); de tweede PASS of FAIL naargelang bestaande backticks. Faalt de tweede nu al op bestaande tekst (bijvoorbeeld `--from` is geen exacte treffer, maar `` `from` `` kan er al staan), verwijder die voorkomens in de skill zonder een vaste pinzin te breken en noteer het in de commit.
- [ ] **Step 3: Schrijf de Engelse tekst in beide varianten** van stap 3f, na *Where does it belong?*, volgens spec *Ontwerp* 2: de zes punten (a) t/m (f) hierboven, in generieke woorden, zonder labelnaam, pad of veldnaam. Houd alle bestaande vaste zinnen van `TestWriteIssueFreeLabelsWithADocument` en de buurtests (3f/pitch/heading-tests) intact; de tabel-voorwaarde komt als aparte alinea of opsomming binnen 3f, vóór "This step never writes the document."
- [ ] **Step 4:** Run `go test ./internal/packcontract/`. Verwacht: PASS.
- [ ] **Step 5: Commit** `feat(write-issue): stap 3f vraagt de velden van de veldentabel (#554)`.

### Task 5: `map-planned` neemt de velden over, verzint niets

**Files:**
- Modify: `pack/core/skills/map-planned/SKILL.md` (stap 3, *Unambiguous place*)
- Test: `internal/packcontract/mapplanned_test.go`

**Interfaces:**
- Consumes: `TestSkillsNameNoFieldOfTheDocument` uit taak 4 dekt de verboden namen.

- [ ] **Step 1: Schrijf de falende test** `TestMapPlannedTakesTheFieldsOfTheTable` in `mapplanned_test.go`: per variant, `flat(section(t, body, "### 3.", "\n### 4."))`, `wantAll` op vaste zinnen die je in stap 3 kiest: (a) de velden onder `From issue` neemt de job over uit titel en body van het issue, in de taal van het document (vertalen mag); (b) wat de uitleg afleidt doet de job zoals de uitleg zegt; (c) velden die de tabel niet noemt en de uitleg niet afleidt laat de job weg; (d) een plek met een ongeldige waarde volgens de uitleg is een onduidelijke plek: de job vraagt. Pin ook dat de zin `nothing else invented` verdwenen is of vervangen (kies één van de twee en pin het gekozene).
- [ ] **Step 2:** Run `go test ./internal/packcontract/ -run MapPlanned`. Verwacht: FAIL.
- [ ] **Step 3: Schrijf de tekst** in stap 3, *Unambiguous place*, volgens spec *Ontwerp* 3, generiek (geen veldnaam, geen pad, geen van de woorden `loop`, `kaart`). Let op de woordtest `TestMapPlannedSkillNamesNoLabelOrPath`: ook "loops" of "overloop" zou falen.
- [ ] **Step 4:** Run `go test ./internal/packcontract/ ./internal/scheduled/mapplanned/`. Verwacht: PASS.
- [ ] **Step 5: Commit** `feat(map-planned): velden uit de veldentabel overnemen, niets verzinnen (#554)`.

### Task 6: Contract voor andere repo's, changelog en dogfood

**Files:**
- Modify: `docs/tick.md` (sectie *Job `map-planned`*, rond regel 650)
- Create: `changelog.d/issue-554-plek-draagt-velden.md`
- Modify (gegenereerd): `.claude/skills/lighthouse-write-issue/`, `.claude/skills/lighthouse-map-planned/`, `.agents/skills/…` via `init --upgrade`

- [ ] **Step 1: `docs/tick.md`**: voeg bij de uitleg van de kolommen `Document` en `Planned` een alinea toe over de veldentabel: de koppen `Label`, `Item`, `Place`, `From issue`, wat elk betekent (zoals in `labels.md`), en dat `write-issue` er stap 3f op baseert en dat zonder tabel, of zonder rij voor het label, stap 3f blijft zoals hij was. Houd relatieve links kloppend (`md-link-check`).
- [ ] **Step 2: Changelogfragment** `changelog.d/issue-554-plek-draagt-velden.md` met frontmatter `type: changed` en één Engelse zin in de stijl van de buren: `write-issue` asks for the fields the taxonomy file lists per kind of item before it writes the place on the map, and `map-planned` takes them from the issue without inventing (#554). Controleer met `ls changelog.d` en `docs/releasing.md` of `changed` een geldig type is, anders het type dat de buren gebruiken.
- [ ] **Step 3: Dogfood**: run `go run ./cmd/lighthouse init --upgrade`, daarna nogmaals; de tweede keer moet `No changes.` geven. Controleer met `git status` dat de geïnstalleerde kopieën van beide skills meewijzigen.
- [ ] **Step 4: Verifieer** in deze volgorde: `lighthouse imports check`, `go test ./...` (of de `[test_gate] command` uit `lighthouse.toml`), `node --test "docs/site/test/*.test.mjs"`. Verwacht: alles groen.
- [ ] **Step 5: Commit** `docs(tick): veldentabel in het contract, changelog en dogfood (#554)`.

---

## Self-review

- **Spec-dekking:** 1 → taak 3; 2 → taak 4; 3 → taak 5; 4 → taak 1 en 2; 5 → taak 6; 6 → taak 3 (tabel liegt niet), taak 4 en 5 (pinzinnen, verboden namen). Niet-doelen raken niets.
- **Typeconsistentie:** `fieldTableProblems` (taak 3), `TestSkillsNameNoFieldOfTheDocument` (taak 4) en de tabelkoppen komen overal gelijk terug.
- **Handmatige check na de bouw:** `lighthouse` heeft geen dry-run van de job op een echt issue; de job zelf blijft ongewijzigd en `internal/scheduled/mapplanned` tests bewijzen dat de nieuwe tabel hem niet breekt.
