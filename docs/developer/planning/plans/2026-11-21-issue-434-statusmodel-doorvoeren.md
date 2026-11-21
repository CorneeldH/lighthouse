# Statusmodel doorvoeren in tick, triage en skills — implementatieplan

> **Voor agentic workers:** VERPLICHTE SUB-SKILL: gebruik superpowers:subagent-driven-development (aanbevolen) of superpowers:executing-plans om dit plan taak voor taak uit te voeren. Stappen gebruiken checkbox-syntax (`- [ ]`).

**Doel:** het statusmodel uit de spec doorvoeren: een `needs_*`-label betekent "interactief oppakken"; 🤖 Teruggegeven en parkeren zetten het issue op `To do` met een label; `On hold` verdwijnt als Status die lighthouse zet.

**Architectuur:** de deterministische regels staan al in `internal/issuestate` (#433: `Apply`, `Return`, `Of`). Dit plan laat `internal/dispatch` (decide, apply, gather, fingerprint) en `internal/cli/triage.go` die functies aanroepen, en past de skills en docs aan. `Decide` blijft puur: de labelnamen komen als invoer binnen, het schrijven gebeurt in `apply`.

**Tech Stack:** Go (`go test -race`), pack/core skills (Markdown), `lighthouse init --upgrade`.

**Spec:** [`2026-11-20-statusmodel-en-statusschema-design.md`](../specs/2026-11-20-statusmodel-en-statusschema-design.md), vooral *Statussen*, *Teruggeven door de controller* en *Parkeren wordt teruggeven met `blocked_issue`*. Eerdere plan: [`2026-11-20-issue-433-toestand-package.md`](2026-11-20-issue-433-toestand-package.md).

## Globale randvoorwaarden

- Geen nieuw label en geen nieuwe Status. `On hold` wordt nergens meer geschreven.
- Code, namen en commentaar in het Engels; issues, comments, docs en commitberichten in het Nederlands (`lighthouse.toml`). Skills in het Engels.
- Terugkeertabel (spec): `reason_needs_brainstorm` → `needs_shaping`; `reason_interactive` → `needs_decision`; regel 13 met actieve blokkade → `blocked_issue`; alle andere `reason_*` → `needs_hands`. Status wordt `To do`, de board-Priority blijft.
- Een `needs_*`-label op een `Controller`-issue: geen nieuwe run; zonder open run `To do`, label blijft; een lopende run loopt door.
- Een wijziging onder `pack/` draait `go run ./cmd/lighthouse init --upgrade` en commit de installed copy (CI eist `No changes.`). Elke PR krijgt een fragment in `changelog.d/`.
- Voor elke commit: `lighthouse imports check` naast `[test_gate] command`. Tests volgens `/lighthouse-test-standard`: per taak de productiewijziging noemen die de test rood maakt.

## Review Focus

- Een `needs_*`-label dat er pas ná de start op komt, terwijl de run nog loopt: de run mag niet worden afgebroken (Taak 4).
- Een voorganger in een andere repo die dicht gaat: het geparkeerde `To do`-issue moet terug naar `Controller` (Taak 5).
- Een `To do`-issue zonder eigen 🤖 Geparkeerd (door een mens) mag de tick nooit oppakken of verplaatsen (Taak 5).
- `blocked_issue` is al weggehaald door de unblock-lane voordat de tick het ziet: nog steeds terugzetten (Taak 5).
- `lighthouse state return` op een gesloten issue: weigeren zonder label- of boardwijziging (Taak 1).
- Teruggeven mislukt halverwege (label gezet, status niet): de volgende poging moet de rest afmaken, geen tweede Teruggegeven (Taak 3).

---

## Bestandsoverzicht

| Bestand | Verantwoordelijkheid |
|---|---|
| `internal/issuestate/cmd.go`, `internal/cli/state.go` | #433-minors: gesloten issue weigeren, uitvoer, exitcode, `dbRunState` |
| `internal/dispatch/decide.go` | labelnamen als invoer, `needs_*`-regel, unpark vanaf `To do`, `ReturnLabelKey` |
| `internal/dispatch/apply.go` | Return/Park/Hold/Unpark via `issuestate` |
| `internal/dispatch/gather.go`, `fingerprint.go` | `To do`-items met eigen 🤖 Geparkeerd lezen en in de vingerafdruk |
| `internal/workplace/sweep.go` | geparkeerd `To do`-issue houdt zijn werkplek |
| `internal/cli/triage.go`, `internal/triage/outcome.go` | `needs_shaping` bij 4c; geen `Controller` bij `needs_*` |
| `internal/config/config.go`, `lighthouse.toml` | `On hold` uit `requiredStatusOptions` |
| `pack/core/skills/lighthouse-{write,route,start}-issue`, `lighthouse-review-spec` | skilltekst volgt het model |
| `docs/*.md`, `docs/developer/labels.md` | docs volgen het model |

---

### Taak 1: #433-minors in `lighthouse state`

**Files:**
- Modify: `internal/issuestate/cmd.go:84-214`, `internal/cli/state.go:22-94`
- Test: `internal/issuestate/giveback_test.go`, `internal/cli/state_test.go`

**Interfaces:**
- Consumes: `issuestate.Return(ctx, f, nr, wantKey, l, board) (Returned, error)`, `target.input` (kent `is.State`).
- Produces: `GiveBack` en `Label` weigeren een gesloten issue; `Show` en `State` gebruiken één schrijfwijze voor "mens nodig".

- [ ] **Stap 1: faalende tests.** `TestGiveBackRefusesClosedIssue` (`internal/issuestate/giveback_test.go`): fakeForge met een gesloten issue; `GiveBack(…)` geeft exit 1, uitvoer noemt "closed", er is geen `EditIssue` en geen `SetStatus`. `TestLabelRefusesClosedIssue` idem voor `Label`. `TestStateShowReportsOpenQuestionNeedsHuman` (`internal/cli/state_test.go`): laatste 🤖-comment is een open 🤖 Vraag → uitvoer bevat `needs human`. `TestStateMissingTokenExitCode`: ontbrekende token geeft exit 2 (zelfde als `forgeOpenWith`).
- [ ] **Stap 2: run, verwacht rood.** `go test -race ./internal/issuestate/... ./internal/cli/ -run 'ClosedIssue|NeedsHuman|MissingToken'`
- [ ] **Stap 3: implementeer.** Lees de state van het issue vóór `Return`/`Apply` in `GiveBack` en `Label` (via `t.input`); gesloten → een expliciete fout `issue #N is closed` (geen `err == nil` aan `fail` doorgeven: `err.Error()` paniekt) met exit 1; lees voor die check alleen `f.Issue` (de `issuestate`-fakeForge kent `BlockedBy`, `Comments` en `env.Run` niet). Kies in `Show` de schrijfwijze `needs human` voor de weergave; de State-constante `"needs_human"` (gedeeld met `tests/pack/fixtures/issue-state/cases.json` en de web-UI) blijft ongewijzigd. Missing token: map `*forge.TokenError` naar exit 2 in `issuestate.open` (`internal/issuestate/cmd.go`), niet in `exit()` van `internal/cli/state.go`, dat alleen de int uit `issuestate` ontvangt. Besluit over `dbRunState`: `state show` leest alleen en maakt of migreert de database **niet**; ontbreekt hij, dan telt het als "geen run". Dat vraagt een read-only opener (stat het bestand, geen `store.Open`/`OpenShared`, die aanmaken en migreren: `internal/store/store.go:98-108`) en een test die bewijst dat een ontbrekende database niet wordt aangemaakt. Leg het vast in de doc comment.
- [ ] **Stap 4: run, verwacht groen.** Zelfde commando; daarna `go test -race ./internal/issuestate/... ./internal/cli/...`
- [ ] **Stap 5: commit.** `fix(state): gesloten issue weigeren en uitvoer en exitcodes gelijktrekken (#434)`

---

### Taak 2: `On hold` uit `requiredStatusOptions`

**Files:**
- Modify: `internal/config/config.go:43`, `internal/repoconfig/repoconfig.go:371` (`Dispatchable` eist `On hold` ook; haal het uit de lijst `{"Controller", "On hold"}`), `lighthouse.toml:21`
- Test: `internal/repoconfig/repoconfig_test.go:117,562`, `tests/pack/fixtures/config/board-status-option-missing-onhold/` (verwijderen of omkeren naar "geldig"), `internal/config/config_test.go:461`, `internal/forge/forge_test.go:57`, `internal/forge/github_contract_test.go:24`, `tests/pack/fixtures/config/*/expect.json`, `tests/pack/python/conftest.py:44`

**Interfaces:**
- Produces: `requiredStatusOptions = []string{"To do", "In progress", "Done"}`. Een board dat `On hold` nog heeft blijft geldig.

- [ ] **Stap 1: faalende test.** In `config_test.go`: `TestStatusOptionsWithoutOnHoldValid` (config zonder `On hold` laadt) en bestaande pins op `On hold` als vereist omzetten naar de nieuwe verwachting.
- [ ] **Stap 2: run rood.** `go test ./internal/config/ -run StatusOptions`
- [ ] **Stap 3: implementeer.** Verwijder `"On hold"` uit `requiredStatusOptions`. Werk fixtures en `lighthouse.toml` bij zonder de optie te verwijderen waar een board hem nog heeft (alleen de eis verdwijnt).
- [ ] **Stap 4: run groen.** `go test -race ./internal/config/... ./internal/forge/...` en `uv run pytest -q tests/pack -k config` (de gate van `lighthouse.toml` gebruikt `uv run`)
- [ ] **Stap 5: commit.** `feat(config): On hold is geen verplichte Status meer (#434)`

---

### Taak 3: Teruggegeven zet `To do` met label

**Files:**
- Modify: `internal/dispatch/decide.go` (`RepoInput`, `Action`, `settleUnstartedFixPR:236`, nieuwe `ReturnLabelKey`), `internal/dispatch/apply.go:134-145,528-570` (`Env`, `KindReturn`, `returnFixPR`, `returnOwnWork`, `returnStartFailed`), `internal/cli/tick.go:850` (labels in `RepoInput`; `Env.Labels`)
- Test: `internal/dispatch/decide_test.go`, `internal/dispatch/apply_test.go`

**Interfaces:**
- Produces: `func ReturnLabelKey(reason string, blocked bool) string` in `internal/dispatch`: `reason_needs_brainstorm`→`needs_shaping`, `reason_interactive`→`needs_decision`, `blocked` (alleen gezet door regel 13 in `settleUnstartedFixPR`) → `blocked_issue`, anders `needs_hands`. `Action.Blocked bool` (json `blocked,omitempty`). `Env.Labels func(repo string) repoconfig.Labels`.
- Consumes: `issuestate.Return(ctx, f, nr, key, labels, true)`.

- [ ] **Stap 1: faalende tests.** `TestReturnLabelKeyTable` (tabeltest over **elke** `reason_*`-sleutel uit `pack/core/headings.toml` (de test leest ze daaruit, zodat een nieuwe reason de test rood maakt; dynamische varianten als `reason+"_ceiling"` in `decide.go:855` vallen onder de default `needs_hands`); verwacht de tabel van de spec; `blocked=true` met `reason_pr_red` → `blocked_issue`). `TestApplyReturnSetsToDoWithLabel` (apply_test.go, fake forge): na `KindReturn` is de Status `To do`, het label uit de tabel staat erop, de Priority is ongewijzigd, de run is `Returned`. `TestApplyReturnWithoutRunStillLabels` (`RunID == 0`). `TestSettleUnstartedFixPRMarksBlocked`: de Return van regel 13 met actieve blokkade heeft `Blocked == true`. `TestApplyReturnClosedIssueSkipsLabelAndStatus`: een gesloten issue krijgt wel de Teruggegeven-comment maar geen label en geen `SetStatus`. Per direct-Teruggegeven-pad een apply-test (zie stap 3): `TestApplyReturnOwnWorkSetsToDo`, `TestApplyReturnFixPRSetsToDo`, `TestApplyReturnStartFailedSetsToDo`.
- [ ] **Stap 2: run rood.** `go test ./internal/dispatch/ -run 'ReturnLabelKey|ApplyReturn|MarksBlocked'`
- [ ] **Stap 3: implementeer.** `RepoInput.Labels repoconfig.Labels` gevuld in `tick.go` (naast `LocationPrefix`), `Env.Labels` voor apply. In `apply` `KindReturn`: volgorde comment → `issuestate.Return` → `release` → `closeRun`. Faalt `Return`, dan blijft de run open en rapporteert apply de fout; de volgende tick ziet de eigen Teruggegeven als laatste protocolcomment van de open run, sluit de run via `settle` en maakt `Return` af (idempotent: `Apply` met dezelfde sleutel maakt een half geschreven edit af, geen tweede comment). **Controleer dit in `settle`/`next`** (`decide.go`, `next()` ~:1219: `handoff()` eist `KindHandoff`, `newIssue` eist `len(hs)==0`, dus een al gesloten run wordt nooit hervat); werkt het niet zo, voeg een decide-regel toe 'open run met eigen Teruggegeven als laatste turn → maak de Return af' en test die via `Decide`. Test `TestApplyReturnHalfWrittenFinishes` bewijst het via twee `Decide`+`Apply`-rondes, niet door `Return` twee keer met de hand aan te roepen. De Return slaat label en Status over (comment blijft) als het issue gesloten is (`decideIssue` draait `orphan`/`settle` ook voor een gesloten item, `decide.go:338-370`).
  - De drie andere paden die direct een Teruggegeven posten zonder `KindReturn` (`returnFixPR` `apply.go:528`, `returnOwnWork` `:545`, `returnStartFailed` `:561`) gaan door dezelfde helper (`issuestate.Return`, label `needs_hands`): de AC eist *elke* Teruggegeven. Voor `returnStartFailed` eerst `retry()` lezen: hangt de tweede poging van Status `Controller` af, dan blijft dit pad uitgezonderd en legt de bouwer dat per pad vast in een doc comment plus de test die de uitzondering pint; anders gaat het pad mee.
- [ ] **Stap 4: run groen.** `go test -race ./internal/dispatch/...`
- [ ] **Stap 5: commit.** `feat(dispatch): Teruggegeven zet het issue op To do met een label (#434)`

---

### Taak 4: `needs_*` op een `Controller`-issue

**Files:**
- Modify: `internal/dispatch/decide.go:374-396` (`decideIssue`)
- Test: `internal/dispatch/decide_test.go`, `internal/dispatch/testdata/decide/needs-label-*.json`

**Interfaces:**
- Produces: `func (c issueCtx) needsHuman() bool`: het item draagt een label uit `{NeedsShaping, NeedsDecision, NeedsHands, NeedsTesting}` van `c.repo.Labels`. `KindReturn`-loze actie `KindToDo` (nieuw, `Kind = "todo"`): `setStatus(statusToDo)`, geen comment, run en werkplek blijven.

- [ ] **Stap 1: faalende tests.** `TestDecideNeedsLabelNoRunMovesToToDo` (Controller + `needs_hands`, geen open run → één `KindToDo`, geen start). `TestDecideNeedsLabelOpenRunKeepsRunning` (met open run → geen `KindToDo`, de run loopt door; bestaande settle-acties ongewijzigd). `TestDecideNeedsLabelNoNewStart` (nieuwe run wordt niet gestart bij `needs_shaping` ook niet bij een Handoff).
- [ ] **Stap 2: run rood.** `go test ./internal/dispatch/ -run NeedsLabel`
- [ ] **Stap 3: implementeer.** In `case statusController` vóór de fix-pr-start (`decide.go:376-388`; die start anders nog steeds een run voor een `needs_*`-issue; `settleUnstartedFixPR` geeft dan de Return) en vóór `c.next()`: heeft het item een `needs_*`-label en geen open run, dan `KindToDo`; heeft het wel een open run, dan geen nieuwe start (`next` slaat over). Voeg `KindToDo` toe aan `apply` en aan `freesSlot` (geeft false). Bouw `KindToDo` via `c.board(KindToDo)` zodat `ItemID` gevuld is (`apply.go:209` `setStatus` gebruikt `a.ItemID`). Test ook: `needs_hands` + rode PR + `Controller` start geen fix-pr.
- [ ] **Stap 4: run groen.** `go test -race ./internal/dispatch/...`
- [ ] **Stap 5: commit.** `feat(dispatch): Controller-issue met needs-label start geen run en gaat naar To do (#434)`

---

### Taak 5: Parkeren naar `To do` met `blocked_issue`, en terugzetten

**Files:**
- Modify: `internal/dispatch/decide.go:374-396,1647-1653`, `internal/dispatch/apply.go:177-188`, `internal/dispatch/gather.go:21-27,61-66,118-130`, `internal/dispatch/fingerprint.go:65-95`, `internal/workplace/sweep.go:16-18,52`
- Test: `internal/dispatch/decide_test.go`, `decide_merge_test.go`, `apply_test.go:1399`, `gather_test.go:31,65,177,195,215-235`, `fingerprint_test.go`, `internal/workplace/sweep_test.go`, `internal/dispatch/testdata/decide/unpark*.json` (Status `To do`)

**Interfaces:**
- Consumes: `issuestate.Apply(ctx, f, nr, key, labels)`, `Return`.
- Produces: `KindPark` = comment 🤖 Geparkeerd, daarna label `blocked_issue` en Status `To do`. `KindHold` = idem zonder comment (eigen Geparkeerd staat er al). `KindUnpark` = `blocked_issue` eraf (via `f.EditIssue` met `RemoveLabels: [labels.BlockedIssue]`, alleen als het label er staat; `issuestate` heeft geen functie om een label te verwijderen), Status `Controller`. `dispatchStatuses` blijft `Controller`; `Prepare` neemt daarnaast items met Status `To do` en een eigen 🤖 Geparkeerd als laatste protocolcomment (`ownParkedItem`). `InController` telt dat item mee voor `workplace.sweep`.

- [ ] **Stap 1: faalende tests.**
  - `TestApplyParkSetsToDoWithBlockedIssue`: na `KindPark` staat 🤖 Geparkeerd gepost, Status `To do`, label `blocked_issue`, de run en werkplek blijven bestaan.
  - `TestDecideUnparksToDoWhenPredecessorGone`: `To do` + eigen Geparkeerd + geen actieve voorganger → `KindUnpark`; fixture met voorganger in een andere repo (`repo` ≠ eigen) en gemergde PR → ook `KindUnpark`.
  - `TestDecideUnparkWithoutLabel`: `blocked_issue` is al weg (unblock-lane) → toch `KindUnpark`.
  - `TestDecideToDoWithoutOwnParkedUntouched`: `To do` zonder eigen Geparkeerd, ook met een voorganger die dicht is → geen actie.
  - `TestApplyUnparkRemovesLabelSetsController`.
  - `TestPrepareReadsToDoWithOwnParked` (gather_test.go): alleen `To do`-items met eigen laatste Geparkeerd komen mee, andere `To do` niet.
  - `TestFingerprintIncludesParkedToDoBlockers`: een voorganger die sluit verandert de vingerafdruk van zo'n item.
  - `TestSweepKeepsWorkplaceOfParkedToDo`.
- [ ] **Stap 2: run rood.** `go test ./internal/dispatch/ ./internal/workplace/ -run 'Park|ToDo|Fingerprint'`
- [ ] **Stap 3: implementeer.** `decideIssue`: nieuwe `case statusToDo:` met `c.ownParked() && !c.active()` → `KindUnpark`; `case statusOnHold` blijft **alleen-lezen** bestaan als overgang: een issue dat al op `On hold` staat wordt zoals nu teruggezet op `Controller` zodra er geen actieve voorganger is, maar nergens schrijft code nog `On hold` (de migratie van bestaande `On hold`-issues blijft een apart issue; het AC eist alleen dat geen code `On hold` schrijft). Zonder deze overgang blijven de bestaande `On hold`-issues na de merge hangen. Test: `TestDecideOnHoldRemnantStillUnparks`, `dispatchStatuses` houdt `On hold` erbij en `InController` telt het mee. Een `To do`-item met `needs_*`-label of `blocked_external` wordt **niet** ontparkeerd (anders flipt het item elke tick tussen `Controller` en `To do` met `KindToDo` uit Taak 4); test `TestDecideUnparkSkipsNeedsLabel`. In `decideIssue` draaien voor een `To do`-item alleen de unpark-regel: sla `orphan`, `repair` en `settle` over als er geen open run is (een mens die het issue interactief oppakte en terug naar `To do` zette, mag geen Teruggegeven/`needs_hands` krijgen); breid `TestDecideToDoWithoutOwnParkedUntouched` uit met een fixture waarvan het laatste protocolcomment een Started van een vreemde sessie is. `Prepare` en `Fingerprint`: filter en blokkade-hash gebruiken `ownParkedToDo(it)` in plaats van `Status == "On hold"`. Eigen-Geparkeerd-detectie vereist de comments in `Pre`: lees comments alleen voor `To do`-items uit de board-lijst met een open run in `open` (zij hebben een run) of een ander criterium dat de snapshot al kent; `Pre` bevat geen comments (`gather.go:41`) en `Fingerprint` hasht alleen `Pre` (`fingerprint.go:66`); `InController` krijgt alleen een statusmap (`tick.go:441`). Kies daarom een criterium uit `Pre`-data dat ook geparkeerde issues zonder run dekt: een `To do`-item met label `blocked_issue`, of met een branch-run (`IssueBranches`, `decide.go:1562`). Vang het geval dat de unblock-lane `blocked_issue` al weghaalde en het issue geen run heeft: lees dan `unblock`-lane en `handoff()` (`decide.go:1647-1653`, Park zonder open run) en leg vast welke van de twee het dekt; blijft er een gat, noteer het als bekende beperking in de PR. Leid het eigen-Geparkeerd-oordeel alleen in `decide` af (daar zijn de comments), en gebruik voor het sweep-criterium (`InController`) dezelfde `Pre`-data.
- [ ] **Stap 4: run groen.** `go test -race ./internal/dispatch/... ./internal/workplace/...`
- [ ] **Stap 5: commit.** `feat(dispatch): parkeren zet To do met blocked_issue en de tick zet terug (#434)`

---

### Taak 6: Triage zet `needs_shaping` bij 4c, nooit `Controller` bij `needs_*`

**Files:**
- Modify: `internal/cli/triage.go:486-515`, `internal/triage/outcome.go:77-107,126-140`
- Test: `internal/cli/triage_test.go:357-433`, `internal/triage/outcome_test.go:170`

**Interfaces:**
- Consumes: `issuestate.Apply(ctx, f, nr, "needs_shaping", labels)`. `Outcome` krijgt `Label string` (de sleutel, `""` = geen).
- Produces: `judgedRow` geeft voor een issue met `needs_*`-label niet `RowForward` maar `RowInteractive`; `RowDesign` zet `needs_shaping` via `Outcome.Label`.

- [ ] **Stap 1: faalende tests.** `TestTriageDesignSetsNeedsShaping` (4c → `EditIssue` voegt `needs_shaping` toe, geen `SetStatus`). `TestTriageNeedsLabelNotForwarded` (gate 4a/4b/2 met `needs_hands` → geen `SetStatus("Controller")`, row `interactive`, comment benoemt het label; dat is waarschijnlijk een nieuwe heading in `pack/core/headings.toml:409`, dus ook een pack-wijziging met `init --upgrade` in deze taak; `judgedRow(j, g0, authorTrusted)` in `outcome.go:126` heeft nog geen labels-parameter: voeg die toe); beide tests in `triage_test.go`, plus `TestJudgedRowNeedsLabel` in `outcome_test.go`.
- [ ] **Stap 2: run rood.** `go test ./internal/triage/ ./internal/cli/ -run 'NeedsShaping|NeedsLabel'`
- [ ] **Stap 3: implementeer.** In `apply`: bij `RowDesign` na de comment `issuestate.Apply(…)` (een label dat er al staat geeft geen edit); `RowForward` blijft zoals nu, maar `judgedRow` laat een item met `needs_*` (uit `f.Labels`) nooit op `RowForward` uitkomen. Pas prompt/regels in `internal/triage/prompt.go`/`rules.go` alleen aan waar zij de oude regel noemen.
- [ ] **Stap 4: run groen.** `go test -race ./internal/triage/... ./internal/cli/...`
- [ ] **Stap 5: commit.** `feat(triage): needs_shaping bij 4c en geen Controller voor needs-issues (#434)`

---

### Taak 7: Skills en docs volgen het model

**Files:**
- Modify: `pack/core/skills/write-issue/{claude,agents}/SKILL.md` (On hold-tekst, `needs_hands`/`needs_testing`, gate-4-beoordeling met `needs_shaping` bij 4c), `route-issue`, `start-issue` (haalt `needs_shaping` weg na goedgekeurde spec via `lighthouse state label <nr> …`), `review-spec`, telkens beide varianten (`claude/` en `agents/`); `docs/developer/labels.md`, `docs/tick.md`, `docs/triage.md`, `docs/install.md`, `docs/getting-started.md`, `docs/workplace.md`; `changelog.d/<fragment>.md`
- Test: `internal/packcontract/writeissue_test.go`, `issueflow_test.go`, `skills_test.go`; `go run ./cmd/lighthouse init --upgrade`

**Interfaces:**
- Consumes: de gedragsregels uit Taak 3 t/m 6 (de docs beschrijven wat die taken doen).

- [ ] **Stap 1: faalende test.** Breid `writeissue_test.go` uit: `TestWriteIssueSkillSetsNeedsShapingAtGate4c` (skill noemt de gate-4-beoordeling en `needs_shaping` bij 4c; noemt `On hold` niet meer als status) en een test in `issueflow_test.go` dat `start-issue` het label weghaalt na een goedgekeurde spec.
- [ ] **Stap 2: run rood.** `go test ./internal/packcontract/...`
- [ ] **Stap 3: schrijf.** Skills en docs bijwerken; alle `On hold`-verwijzingen die de tick of lighthouse als Status zetten vervangen door `To do` + `blocked_issue`; `needs_*` = "interactief oppakken". `tick.md`: parkeer-/unparkregels, Teruggegeven-tabel, `needs_*` op `Controller`. `triage.md`: 4c zet het label, `needs_*` blijft uit `Controller`. Fragment in `changelog.d/`.
- [ ] **Stap 4: dogfood.** `go run ./cmd/lighthouse init --upgrade`, daarna nogmaals: uitvoer `No changes.` Commit pack en installed copy samen.
- [ ] **Stap 5: volledige gate.** `go test -race ./...` en `lighthouse imports check`; scan `grep -rn '"On hold"' internal --include='*.go'` toont geen schrijfpad meer (alleen tests/commentaar die het model noemen).
- [ ] **Stap 6: commit.** `docs(pack): skills en docs volgen het statusmodel (#434)`

---

## Zelfreview

- **Spec-dekking:** write-issue/4c (7), triage (6), Teruggegeven-tabel (3), parkeren en terugzetten incl. andere repo (5), `needs_*` op `Controller` (4), `On hold` uit config (2), docs, skills en `init --upgrade` (7), #433-minors (1). De **migratie** van bestaande `On hold`-issues valt buiten dit issue (eigen commando met bevestiging, niet in de AC) en blijft een apart issue.
- **Type-consistentie:** `Action.Blocked`, `KindToDo`, `ReturnLabelKey`, `Env.Labels` en `RepoInput.Labels` komen in Taak 3 en 4 voor en worden in 5 hergebruikt.
- **Onzekerheden voor de bouwer:** het precieze criterium om `To do`-items met comments te lezen (Taak 5); of de `Env`/`RepoInput` van `Decide` al labelnamen draagt buiten `LocationPrefix`; de volgorde comment → close → label bij Return (Taak 3, met `HalfWritten`-test).
