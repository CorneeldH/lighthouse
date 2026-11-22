# Prio-gewicht voor tick en triage — implementatieplan

> **Voor agentic workers:** VERPLICHTE SUB-SKILL: gebruik superpowers:subagent-driven-development (aanbevolen) of superpowers:executing-plans om dit plan taak voor taak uit te voeren. Stappen gebruiken checkbox-syntax (`- [ ]`).

**Doel:** Eén Go-functie berekent het prio-gewicht (#445) per open issue; de tick en triage starten daarop, met de board-Priority als handmatige override, en `/api/priorities` levert het mee.

**Architectuur:** Een nieuw blad-package `internal/prioweight` (geen lighthouse-imports) bevat de pure berekening op een graaf van issues, milestone-rangen en `blocked_by`. `priorities.Build` vult het veld `PrioWeight` per issue; `dispatch.Prepare` leest `Planning` (REST, ETag-cache van #428) en zet het gewicht op elk `Item`; `comparePlanned` en `triage.Sort` ordenen op band, gewicht, tijdstip.

**Techniek:** Go, bestaande `forge.Planning`, huma-endpoint, JSON-fixtures in `internal/dispatch/testdata/decide/`.

**Spec:** het issue [#509](https://github.com/CorneeldH/lighthouse/issues/509) met de regel van [#445](https://github.com/CorneeldH/lighthouse/issues/445); er is geen aparte spec.

## Globale randvoorwaarden

- Prioriteitsfactor van een milestone: met *M* milestones met een rang krijgt rang 1 factor *M*, rang *r* factor *M − r + 1*; geen rang of geen milestone = 0.
- Prio-gewicht = eigen factor + som over alle issues die direct of indirect wachten van factor × 2^(k−1), *k* = **langste** afstand; elk wachtend issue telt één keer; een cyclus telt als één groep.
- Band: `P0` vóór alles, `P1` of leeg (ook onbekende waarde) volgt het gewicht, `P2` achter alles. Binnen een band: gewicht hoog→laag, dan tijdstip (tick) of issuenummer (triage) zoals nu. Scheduled starts houden hun voorrang in `comparePlanned`.
- Gewicht is per repo: een blocker in een andere repo telt niet mee in de graaf.
- Een rustige tick wordt niet langer: `Planning` loopt via de ETag-transport (304).
- Code-identifiers Engels, artefacten (docs, changelog, commits) Nederlands; skilltekst in `pack/` Engels, beide varianten (`claude/`, `agents/`).
- Voor elke commit: `lighthouse imports check` naast `[test_gate] command`. Een wijziging onder `pack/` draait ook `go run ./cmd/lighthouse init --upgrade` en commit de geïnstalleerde kopie.

## Review Focus

- Wachtend issue dat via twee paden bereikbaar is (A → B → C en A → C): telt één keer op de langste afstand (A = 1 + 2 = 3).
- Cyclus (A ↔ B): geen oneindige lus, één groep.
- Blocker naar een gesloten of onbekend issue, of naar een andere repo: telt niet mee, geen panic.
- Milestone zonder rang en issue zonder milestone: factor 0, geen fout.
- Onbekende Priority-waarde (`P3`, `p0`): valt in de band van leeg zoals nu.
- Gelijk gewicht: de volgorde blijft deterministisch (tijdstip, dan nummer).

---

### Task 1: Berekening in `internal/prioweight`

**Files:**
- Create: `internal/prioweight/prioweight.go`, `internal/prioweight/prioweight_test.go`

**Interfaces:**
- Produces:
  - `type Issue struct { Number int; Milestone int; BlockedBy []int }` — `Milestone` is het milestonenummer, 0 zonder; `BlockedBy` bevat alleen issuenummers van dezelfde repo.
  - `func Factors(ranks map[int]int) map[int]int` — milestonenummer → rang (alleen milestones met rang) naar factor.
  - `func Compute(issues []Issue, factors map[int]int) map[int]int` — issuenummer → prio-gewicht, voor elk meegegeven issue.

- [ ] **Stap 1: schrijf de falende tests** in `prioweight_test.go`: `TestComputeVoorbeeldenUit445` (7 milestones met rang; #372 in factor 3 met wachtend #500 (factor 7), #439 en #501 (factor 0) → 10; #19 factor 0 met #255 (factor 5) → 5; #437 factor 2 met #438 en #446 op 1 stap en #445 op 2 stappen, alle factor 2 → 10), `TestComputeLangsteAfstand` (A → B → C plus C direct op A, alle factor 1 → A = 3), `TestComputeCyclus` (A ↔ B eindigt en geeft elk een eindig gewicht, tabel-test op twee issues), `TestComputeOnbekendeBlocker` (blocker buiten `issues` telt niet mee), `TestFactors` (rangen {1, 3, 5} van 3 milestones geven factoren 3, 2, 1: de factor volgt de **positie** *p* (1-based) in de oplopend gesorteerde rangen, *M − p + 1*, zodat een gat in de rangen geen negatieve factor geeft; bij aaneengesloten rangen is dat gelijk aan *M − r + 1*; **gelijke rangen krijgen dezelfde factor**: rangen {1, 1, 2} geven 3, 3, 1, dus factor = *M* − (0-based index van de eerste voorkomen van die rang); voeg dit geval toe aan `TestFactors`, zodat de uitkomst niet van de map-volgorde afhangt).
- [ ] **Stap 2: draai** `go test ./internal/prioweight/ -run . -v`; verwacht FAIL (package ontbreekt).
- [ ] **Stap 3: implementeer** `Factors` (sorteer de rangen oplopend, positie → *M − p + 1*, gelijke rangen één factor via de eerste index) en `Compute`. Algoritme: bouw de omgekeerde graaf (blocker → wachtenden); condenseer cycli tot één groep (Tarjan of eenvoudige SCC); bepaal per blocker voor elk bereikbaar wachtend issue de **langste** afstand over de gecondenseerde DAG; gewicht = eigen factor + Σ factor(w) × 2^(k−1). Een issue in dezelfde cyclus telt op afstand 1.
- [ ] **Stap 4: draai** dezelfde test; verwacht PASS. Draai `lighthouse imports check`.
- [ ] **Stap 5: commit** `feat(prioweight): prio-gewicht per issue volgens #445 (#509)`.

### Task 2: `/api/priorities` levert het prio-gewicht

**Files:**
- Modify: `internal/priorities/priorities.go` (`Issue`, `Build`), `internal/priorities/rank.go` (`ParseRank` lezen; `WithDescription` herberekent het gewicht), `web/openapi.json` en `web/src/api/` (gegenereerd)
- Test: `internal/priorities/priorities_test.go`, `internal/server/priorities_test.go`, `internal/cli/serve_test.go:947-1026`

**Interfaces:**
- Consumes: `prioweight.Factors`, `prioweight.Compute`, `ParseRank`.
- Produces: `Issue.PrioWeight int` met JSON-tag `prio_weight`; `Issue.Factor int` met tag `factor` (eigen factor, voor de hover van #445).

- [ ] **Stap 1: schrijf de falende test** `TestBuildPrioWeight` in `priorities_test.go`: een bronnen-fake met twee milestones (rang 1 en 2) en drie issues waar één issue op het ander wacht; verwacht de `PrioWeight` en `Factor` per issue volgens de regel. Pas de gelijkheidsvergelijkingen in `internal/server/priorities_test.go` en `internal/cli/serve_test.go` aan op de nieuwe velden. Voeg `TestWithDescriptionRecomputesWeight` toe: na `WithDescription` met een andere rang verandert `PrioWeight` en `Factor` van de issues in die repo (het ranks-endpoint `internal/server/milestone_ranks.go:83,97` patcht de gecachete repos hiermee en wacht anders tot 5 minuten op de refresher).
- [ ] **Stap 2: draai** `go test ./internal/priorities/ ./internal/server/ ./internal/cli/ -run 'Priorities|PrioWeight|WithDescription' -v`; verwacht FAIL.
- [ ] **Stap 3: implementeer** in `Build`: na het vullen van `r.Issues` en `r.Milestones` rangen uit `ParseRank(m.Description)` per milestonenummer, `Factors`, een `prioweight.Issue` per issue (blockers van een andere repo dan `s.Slug` weglaten), `Compute`, en zet `PrioWeight` en `Factor` per issue. Het endpoint zelf (`internal/server/priorities.go`) hoeft niet te veranderen. Laat `WithDescription` na het vervangen van de beschrijving de factoren en gewichten van die repo herberekenen met dezelfde hulpfunctie als `Build` (één functie, niet dupliceren). Werk daarna het OpenAPI-snapshot bij: `go test ./internal/server -run TestOpenAPISnapshot -update`, dan `cd web && npm ci && npm run gen`, en commit `web/openapi.json` en `web/src/api/` mee (CI controleert de drift).
- [ ] **Stap 4: draai** `go test ./internal/priorities/ ./internal/server/ ./internal/cli/`; verwacht PASS (ook `TestOpenAPISnapshot`).
- [ ] **Stap 5: commit** `feat(priorities): /api/priorities geeft prio-gewicht en factor per issue (#509)`.

### Task 3: De tick ordent op band, gewicht, tijdstip

**Files:**
- Modify: `internal/dispatch/snapshot.go` (`Item.PrioWeight int` met tag `prio_weight,omitempty`), `internal/dispatch/gather.go` (`Prepare`), `internal/forge/forge.go`, `internal/forge/planning.go`, `internal/forge/gitlab_planning.go` en de fakes (`PrioInputs`), `internal/dispatch/decide.go` (`planned`, `comparePlanned`, `priorityRank` callers :1529, :1855, :1864, :1883, :2026), `internal/dispatch/decide_scheduled.go:58`, `internal/dispatch/priority.go`
- Test: `internal/dispatch/decide_test.go`, `internal/dispatch/gather_test.go`, `internal/dispatch/testdata/decide/`

**Interfaces:**
- Consumes: een nieuwe slanke `forge.Forge`-methode (zie Stap 3), `prioweight`. `forge.Planning` zelf blijft ongemoeid: dat leest per open milestone ook `ClosedInMilestone` en per issue `blocked_by` (`internal/forge/planning.go:66-71`, `:111-121`), en dat hoort niet bij elke tick.
- Produces: `func PriorityBand(priority string) int` in `internal/dispatch/priority.go` (0 voor `P0`, 1 voor `P1`, leeg en onbekend, 2 voor `P2`); vervangt `PriorityRank`; `planned.weight int`.

- [ ] **Stap 1: schrijf de falende tests** in `decide_test.go`: `TestStartOrderP0BeforeHighWeight` (P0 met gewicht 1 vóór leeg met gewicht 99), `TestStartOrderP2LastDespiteWeight` (P2 met gewicht 99 achter leeg met gewicht 0), `TestStartOrderWeightThenTime` (twee lege Priority: hoger gewicht eerst; bij gelijk gewicht het eerdere tijdstip), `TestStartOrderUnknownPriorityIsP1Band` (`P3` sorteert als leeg). Dek ook hervattingen (`KindResume`). In `gather_test.go`: `TestPrepareSetsPrioWeight` met een fake forge die `Planning` levert met een blocker-keten; het `Item.PrioWeight` volgt de regel.
- [ ] **Stap 2: draai** `go test ./internal/dispatch/ -run 'StartOrder|PrepareSetsPrioWeight' -v`; verwacht FAIL.
- [ ] **Stap 3: implementeer** `PriorityBand` (hernoem `priorityRank`, bandwaarden 0, 1, 2) en `planned.weight`. In `comparePlanned` na de scheduled-regel: eerst `cmp.Compare(a.band, b.band)`, dan `-cmp.Compare(a.weight, b.weight)` (hoog eerst), dan `a.at`. `issueCtx.planned` vult `band` en `weight` uit `Item.Priority` en `Item.PrioWeight`; ook de `planned{…}`-constructors die `priorityRank` rechtstreeks aanroepen (`decide.go:1529`, `:1855`, `:1864`, `:1883`, KindPush) zetten band én gewicht uit `c.it`, zodat pushes niet op gewicht 0 achter gewogen starts zakken. `decide_scheduled.go:58` blijft de band van leeg (1) gebruiken. In `Prepare`: de spec beperkt de lezing (#427: `blocked_by` alleen wat de bordlezing al heeft; milestone-beschrijvingen via de ETag-cache van #428). Voeg daarom aan `forge.Forge` een methode `PrioInputs(ctx) (PrioInputs, error)` toe (GitHub, GitLab en de fakes), die alleen de milestone per open issue (de al ETag-gecachete open-issuelijst, **zonder** `blocked_by`-lezing) en de open milestones met hun beschrijving (ETag-gecachet, **zonder** `ClosedInMilestone`) teruggeeft; GitLab leest daarvoor geen links. De graaf komt uit de `BlockedBy` van de borditems die `Prepare` al leest (alle borditems van de repo, niet alleen `held`; blockers van een andere repo of buiten het bord tellen niet mee, zoals de spec toelaat). Tick en `/api/priorities` kunnen daardoor voor een blocker buiten het bord verschillen; zeg dat in `docs/tick.md`. De gewichten via `prioweight` (zelfde omzetting als Task 2: milestonerang via `priorities.ParseRank` kan niet vanuit `dispatch` als dat een importcyclus geeft; verplaats dan `ParseRank` en `matchRank` naar `prioweight` en laat `priorities` ernaar verwijzen), en zet `Item.PrioWeight` voor elk open board-item. Een `PrioInputs`-fout is een fout van `Prepare`, net als een `BoardItems`-fout (geen stil terugvallen op gewicht 0). Zet de gewichten niet in de vingerafdruk. Testdekking van `PrioInputs`: `internal/forge/planning_test.go` en de GitLab-test, met de aanname dat de lezing geen `ClosedInMilestone`- en geen `blocked_by`-aanroep doet.
- [ ] **Stap 4: werk de fixtures bij** die de volgorde pinnen: draai `go test ./internal/dispatch/...`; herstel elke rode fixture in `internal/dispatch/testdata/decide/` alleen als de nieuwe volgorde de bedoelde is, en benoem de reden in de commitbody.
- [ ] **Stap 5: draai** `go test -race ./internal/dispatch/... ./internal/cli/...` en `lighthouse imports check`; verwacht PASS.
- [ ] **Stap 6: commit** `feat(dispatch): tick start op band, prio-gewicht, tijdstip (#509)`.

### Task 4: Triage sorteert op dezelfde manier

**Files:**
- Modify: `internal/triage/facts.go` (`Facts.PrioWeight int`, `Sort`), `internal/cli/triage.go:262` (gewicht vullen)
- Test: `internal/triage/facts_test.go:211` (`TestSort`)

**Interfaces:**
- Consumes: `dispatch.PriorityBand`, `prioweight`, `forge.Forge.PrioInputs` (uit Task 3).
- Produces: `Facts.PrioWeight`; `Sort(fs []Facts)` ordent op band, gewicht hoog→laag, issuenummer.

- [ ] **Stap 1: schrijf de falende test** `TestSortBandWeightNumber`: P0 met gewicht 1 vóór leeg met gewicht 99; P2 achter alles; gelijk gewicht → laagste nummer eerst.
- [ ] **Stap 2: draai** `go test ./internal/triage/ -run TestSort -v`; verwacht FAIL.
- [ ] **Stap 3: implementeer** `Sort` met `dispatch.PriorityBand`, daarna gewicht aflopend, daarna `Number`. In `internal/cli/triage.go` vóór `triage.Sort`: dezelfde gewichtsberekening als in `Prepare` (deel één hulpfunctie, bijvoorbeeld `dispatch.Weights(in forge.PrioInputs, blockers map[int][]int) map[int]int`, in plaats van de logica te dupliceren; de blockers komen uit de borditems die triage al leest, dus geen extra `blocked_by`-lezing: de bestaande tests in `internal/cli/triage_test.go:558-560` en `:790-791` die één lezing per issue vastpinnen blijven dan gelden) en zet `Facts.PrioWeight`.
- [ ] **Stap 4: draai** `go test ./internal/triage/... ./internal/cli/...` en `lighthouse imports check`; verwacht PASS.
- [ ] **Stap 5: commit** `feat(triage): kandidaten sorteren op band, prio-gewicht, nummer (#509)`.

### Task 5: Meting, documentatie, skills, changelog

**Files:**
- Modify: `docs/tick.md` (nieuwe alinea bij *De rem*, :792, waar de startvolgorde telt), `docs/triage.md` (:152-153), `pack/core/skills/write-issue/claude/SKILL.md:437`, `pack/core/skills/write-issue/agents/SKILL.md:437`, de geïnstalleerde kopie onder `.claude/skills/` en `.agents/` (via `init --upgrade`)
- Create: `changelog.d/issue-509-prio-gewicht-tick-triage.md`

- [ ] **Stap 1: meting is een handstap, geen onbewaakte stap.** Draai in de onbewaakte uitvoering **geen** `lighthouse tick`: die voert echte acties uit op het live bord en botst met `tick.lock` (`docs/tick.md:252-256`; precedent: `docs/developer/planning/plans/2026-11-21-issue-427-tick-minder-reads.md:85-87`). Leg in de PR-body `Refs #509 — meting rustige tick (AC 5) volgt door de gebruiker` met dat AC als open punt, en zet in de handoff de handstap: een tick zonder wijzigingen op `main` en op de branch, duur en aantal forge-aanroepen als comment op #509. Controleer wel in de tests dat `PrioInputs` geen `ClosedInMilestone` en geen `blocked_by` leest.
- [ ] **Stap 2: documentatie** in `docs/tick.md` en `docs/triage.md`: de volgorde (band, prio-gewicht hoog→laag, tijdstip) en dat Priority een handmatige override is (`P0` vóór alles, `P1` of leeg volgt het gewicht, `P2` achter alles); noem de bron (milestone-rang en `blocked_by`) en verwijs naar #445.
- [ ] **Stap 3: skills** in beide `write-issue`-varianten (Engels, sectie *The board*): Priority is een override op het berekende prio-gewicht, geen berekening; draai daarna `go run ./cmd/lighthouse init --upgrade` en verwacht bij een tweede run `No changes.`
- [ ] **Stap 4: changelog-fragment** met frontmatter `type: changed` en een Nederlandse zin: de tick en triage starten in volgorde van prio-gewicht en de board-Priority is een override; `/api/priorities` toont het gewicht.
- [ ] **Stap 5: draai** de volledige `[test_gate] command` uit `lighthouse.toml` en `lighthouse imports check`; verwacht groen.
- [ ] **Stap 6: commit** `docs(tick): volgorde op prio-gewicht en Priority als override (#509)`; daarna `git push -u origin HEAD` volgens de uitvoerende run.

## Zelfcontrole

- Dekking: AC 1 (Task 1), AC 2 (Task 2), AC 3 (Task 3), AC 4 (Task 4), AC 5 (Task 5 stap 1, handstap van de gebruiker), AC 6 (Task 5 stappen 2-3), AC 7 (Task 5 stap 4).
- Open punt voor de uitvoerder, geen ontwerpkeuze: of `priorities` en `dispatch` zonder cyclus naar `prioweight` kunnen verwijzen; de plaats van `ParseRank` volgt daaruit (Task 3 stap 3).
