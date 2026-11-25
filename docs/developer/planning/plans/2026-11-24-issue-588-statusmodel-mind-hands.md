# Statusmodel: vijf statussen, labels als markering — implementatieplan

> **Voor agentic workers:** VEREIST SUB-SKILL: gebruik superpowers:subagent-driven-development (aanbevolen) of superpowers:executing-plans om dit plan taak voor taak uit te voeren. Stappen gebruiken checkboxen (`- [ ]`).

**Doel:** Het statusmodel van #588 bouwen: deel a (labels `needs_mind`/`needs_hands` per as, migratiecommando) en deel b (vijf statussen plus `Blocked`, vormentaal met kruis, legenda).

**Architectuur:** Eerst een mechanische hernoeming van `[labels]` (taak 1) zodat alles blijft compileren; dan per as de rangregel (`issuestate`), de gebruikers van de labels (triage, tick, map-planned, skills), `DeleteLabel` en het migratiecommando (`internal/labelmigrate`, `lighthouse labels migrate`). Deel b verandert `issuestate.Of`, `cases.json`, `derive.ts` en `/api/priorities` in één keer, daarna de web-componenten. Go en web delen één regel via `tests/pack/fixtures/issue-state/cases.json`.

**Tech Stack:** Go (cobra, BurntSushi/toml, `internal/forge`), React + vitest (`web/`), pack-skills met dogfood-kopie.

**Spec:** [2026-11-24-statusmodel-mind-hands-design.md](../specs/2026-11-24-statusmodel-mind-hands-design.md). Lees ook [2026-11-23-prioritering-zes-statussen-design.md](../specs/2026-11-23-prioritering-zes-statussen-design.md) voor wat blijft staan.

## Globale beperkingen

- Code, commentaar, bestandsnamen en alles onder `pack/**`, `internal/**`, `cmd/**`, `tests/**`, `tools/**` zijn Engels (`code_language = "en"`). Docs, specs, plannen, changelog-fragmenten en commitberichten zijn Nederlands. Alle UI-tekst is Engels.
- Nieuwe labelsleutel `needs_mind`; naam `3-needs-mind`, kleur `FBCA04`; ingebouwde default `needs-mind`. `needs_hands` blijft `3-needs-hands`. De sleutels `needs_shaping`, `needs_decision`, `needs_testing` bestaan nergens meer (ook niet in `[labels]`, schema, fixtures, docs buiten de planning).
- De statussen heten `needs_mind`, `ready`, `active`, `needs_hands`, `done` (UI-tekst *needs mind · to do · active · needs hands · done*). `Of` geeft ook `Blocked bool`; een gesloten issue geeft `Blocked=false`.
- Statusregel, eerste rij die past wint: (1) gesloten → `done`; (2) label `needs_mind` of open 🤖 Vraag → `needs_mind`; (3) label `needs_hands` zonder blokkade → `needs_hands`; (4) run `open`/`parked`, of Status `Controller`, of Status `In progress` zonder blokkade → `active`; (5) rest → `ready`. Blokkade = label `blocked_issue`/`blocked_external` of een open `blocked_by`.
- Rang per as: `blocked_issue` wint alleen van `blocked_external`; `needs_mind` en `needs_hands` staan los van alles. Een blokkade haalt `needs_hands` niet meer weg.
- Vorm: needs mind = achtergrond met `1.5px dashed` in `--foreground`, nummer onderstreept; to do = `--lh-seg-todo`; active = `.lh-stripe`; needs hands = strepen plus `inset 0 0 0 2px` in `--foreground`, nummer onderstreept; done = vol. *Blocked* is het kruis (`Cross`) over de vorm heen. Balkvolgorde: done · needs hands · active · to do · needs mind; binnen to do zonder kruis vóór met kruis.
- Dogfood: een wijziging onder `pack/` of onder `internal/` die wat `init` schrijft verandert, draait `go run ./cmd/lighthouse init --upgrade` en commit de kopie (CI eist `No changes.`). Elke PR krijgt een fragment in `changelog.d/`. Voor een commit: `lighthouse imports check` naast `[test_gate] command`.
- Geen `--no-verify`, geen merge, atomaire commits als Conventional Commits.

## Review Focus

Invoer of omstandigheden die de spec impliceert maar waar geen eigen taak-test op rust; elk heeft een test in de genoemde taak.

1. Repo zonder eigen `[labels]` (oude defaults `needs-shaping`, …) moet migreren (taak 7).
2. Issue met zowel `needs_shaping` als `needs_decision` krijgt `needs_mind` één keer, geen dubbele of mislukte edit (taak 7).
3. Herhaalde run na een fout halverwege (nieuw label erop, oud er nog) loopt door en eindigt gelijk aan een run zonder fout (taak 7).
4. Issue met `blocked_issue` én `blocked_external` én `needs_mind` verliest geen label bij `state label`, en `label: #<nr>` noemt ze alle (taak 2).
5. Gesloten issue met een blokkadelabel: `Blocked=false` en geen `labels` in `/api/priorities` (taak 10, 11).
6. `lighthouse.toml` met oude sleutel: `config.Load`, `repoconfig.LoadFile` en `lighthouse config` falen met de naam van het migratiecommando, maar `lighthouse labels migrate` leest zo'n bestand wel (taak 1, 8).

## Handmatige stappen (na de PR, niet vooraf)

Taken 1–13 hebben geen mensstap en wachten op niemand. Alleen de uitrol (taak 14: pauzeren, mergen, release, binary installeren, migratie draaien op de tick-host en in `personal_ai_agent`) is mensenwerk, en elke stap daarvan kan pas ná de PR (de binary met `labels migrate` bestaat eerder niet). Taak 14 zet die stappen letterlijk in de PR-body en in één `lighthouse ask` (elk commando via `--command`, opties *klaar* en *anders*) ná het openen van de PR; de bouw wacht er niet op.

## Bestandsstructuur

- Config: `internal/config/config.go`, `internal/repoconfig/repoconfig.go`, `internal/projectconfig/projectconfig.go`, fixtures in `tests/pack/fixtures/config*/`, `tests/pack/fixtures/config-schema.json`, `docs/site/data/config.json`.
- Regel: `internal/issuestate/{state,label,giveback,cmd}.go`, `internal/cli/state.go`.
- Gebruikers: `internal/triage/*`, `internal/dispatch/decide.go`, `internal/scheduled/mapplanned/*`, `internal/priorities/priorities.go`.
- Nieuw: `internal/labelmigrate/` (taxonomie, `lighthouse.toml`, plan, uitvoering), `internal/cli/labels.go` (commando).
- Forge: `internal/forge/{forge,write_issue,gitlab_write}.go`, `internal/forge/forgesuite/suite.go`.
- Web: `web/src/lib/priorities/derive.ts`, `web/src/components/priorities/{Block,DepsView,MilestonesView,NodeCard,Toolbar}.tsx`.
- Skills en docs: `pack/core/skills/{route-issue,start-issue,write-issue}`, `pack/core/templates/labels.md`, `docs/developer/labels.md`, `docs/{tick,triage,priorities}.md`, installed kopie in `.claude/skills/` en `.agents/skills/`.

---

## Deel a — labels en migratie

### Taak 1: `[labels]` hernoemen en oude sleutels weigeren

**Bestanden:**
- Wijzig: `internal/config/config.go` (schema ~r.91-93, defaults ~r.154-157, `Load` r.651), `internal/repoconfig/repoconfig.go` (`Labels` r.63, `Blocking`, `NeedsName`, `rawFile.Labels`, `DefaultLabels`, `LoadFile`), `internal/projectconfig/projectconfig.go:235` (`LabelTables`), `tests/pack/fixtures/config-schema.json`, `tests/pack/fixtures/config/{full,defaults}/*`, `docs/site/data/config.json`, `lighthouse.toml` (`[labels]`: `needs_shaping`/`needs_decision` worden `needs_mind = "3-needs-mind"`, `needs_testing` vervalt, `needs_hands` blijft; anders weigert `config.Load` het eigen bestand, ook in `internal/codelang` en `init --upgrade`), `tests/pack/fixtures/issue-state/cases.json` (alleen labelnamen en verwachting, zie stap 4), `web/src/lib/priorities/derive.ts` (`issueState`: `issue.label === 'needs_mind'` → `needs_shaping`; `NEEDS_LATER` is alleen `needs_hands`; `needs_decision` vervalt; `derive.test.ts` leest dezelfde `cases.json`).
- Wijzig mechanisch (compileren): elke gebruiker van `NeedsShaping`/`NeedsDecision`/`NeedsTesting` in `internal/` (`grep -rn "NeedsShaping\|NeedsDecision\|NeedsTesting" internal`) wordt `NeedsMind` of vervalt; sleutelstrings `"needs_shaping"`/`"needs_decision"` worden `"needs_mind"`, `"needs_testing"` wordt `"needs_hands"`.
- Test: `internal/config/config_test.go`, `internal/repoconfig/repoconfig_test.go`, `internal/projectconfig/projectconfig_test.go`.

**Interfaces:**
- Produceert: `repoconfig.Labels{BlockedIssue, BlockedExternal, NeedsMind, NeedsHands, LocationPrefix, AreaPrefix string}`; `Labels.Blocking() []string` blijft, met vier namen (`BlockedIssue, BlockedExternal, NeedsMind, NeedsHands`); `Labels.NeedsName(labels []string) string` kijkt naar `NeedsMind`, `NeedsHands`; `config.Default("labels.needs_mind") == "needs-mind"`; `config.OldLabelKeys = []string{"needs_shaping","needs_decision","needs_testing"}`; `config.OldLabelKeyError(path, key string) error` met tekst `<path>: [labels] <key> was removed; run lighthouse labels migrate`, door `config.Load` en `repoconfig.LoadFile` allebei gebruikt.

- [ ] **Stap 1: tests schrijven (rood)**
  - `TestLoadRejectsOldLabelKeys` (config): voor elk van de drie oude sleutels in `[labels]` geeft `config.Load` een fout die de sleutel en `lighthouse labels migrate` bevat.
  - `TestLoadFileRejectsOldLabelKeys` (repoconfig): idem voor `LoadFile`.
  - `TestDefaultLabelsNeedsMind`: `DefaultLabels().NeedsMind == "needs-mind"`, `NeedsHands == "needs-hands"`.
  - `TestLabelTablesUseNewKeys` (projectconfig): `LabelTables` heeft `needs_mind = "needs-mind"` en `needs_hands`, en geen `needs_shaping`/`needs_decision`/`needs_testing`.
- [ ] **Stap 2: draai ze** — `go test ./internal/config ./internal/repoconfig ./internal/projectconfig -run 'OldLabelKeys|NeedsMind|NewKeys'`; verwacht FAIL.
- [ ] **Stap 3: implementeer** het schema en de structs zoals onder Interfaces; weiger oude sleutels in `config.Load` en `repoconfig.LoadFile` met `md.IsDefined("labels", key)`. Voeg geen lenient-pad toe: de migratie leest het toml zelf (taak 6).
- [ ] **Stap 4: herstel de rest** — werk fixtures en `config-schema.json` bij; pas `docs/site/data/config.json` met de hand aan (er is geen generator; `docs/site/test/config.test.mjs` controleert het, draai `node --test docs/site/test/config.test.mjs`), herschrijf `[labels]` in `lighthouse.toml` zoals onder Bestanden, pas in `cases.json` de labelnamen aan (`needs_shaping`/`needs_decision` → `needs_mind`, `needs_testing` → `needs_hands`) en laat de gevallen met `needs_decision` `needs_shaping` verwachten in plaats van `needs_human`; in `Of` geeft `NeedsMind` de oude status `needs_shaping` (tijdelijke mapping tot taak 10), en laat de bestaande tests die de oude namen noemen de nieuwe namen gebruiken.
- [ ] **Stap 5: volledige tests** — `go build ./... && go vet ./... && go test ./... && cd web && npx vitest run src/lib/priorities`; verwacht groen. Het eigen `lighthouse.toml` heeft nu de nieuwe sleutels; de forge heeft nog de oude labels tot taak 14, dat raakt geen test.
- [ ] **Stap 6: commit** — `refactor(labels): needs_mind en needs_hands in [labels], oude sleutels geweigerd`

### Taak 2: rangregel per as in `issuestate`

**Bestanden:**
- Wijzig: `internal/issuestate/label.go`, `internal/issuestate/state.go` (`GateZero`, `HasNeedsLabel`), `internal/issuestate/cmd.go` (uitvoerregel `label:`), `internal/cli/state.go` (help-tekst), `internal/issuestate/giveback.go`.
- Test: `internal/issuestate/label_test.go`, `giveback_test.go`, `state_test.go`, `internal/cli/state_test.go`.

**Interfaces:**
- Produceert: `issuestate.Keys = []string{"needs_mind","blocked_issue","blocked_external","needs_hands"}`; `Name(key, l)` kent deze vier; `Marks(names []string, l repoconfig.Labels) []string` (de sleutels van blokkade- en mens-labels op het issue, in de volgorde van `Keys`); `SetLabel(current []string, wantKey string, l) (LabelEdit, error)` met `LabelEdit{Add, Remove []string}` (het veld `Winner` vervalt); `GateZero` houdt zijn vorm maar stopt niet meer als het issue `NeedsMind` draagt, ook niet bij `blocked_issue`/`blocked_external` of een open blocker (spec §5: `needs_mind` met een blokkade routeert door naar 4c). `BlockingKey` blijft tot taak 3 (taak 3 vervangt het enige gebruik in `priorities.go` en laat het dan vervallen).
- Rang: `needs_mind` en `needs_hands` zetten voegt alleen dat label toe en haalt niets weg. `blocked_issue` zetten voegt toe en haalt alleen `blocked_external` weg. `blocked_external` zetten met `blocked_issue` aanwezig voegt niets toe (`Add` leeg).

- [ ] **Stap 1: tests (rood)** in `label_test.go`:
  - `TestSetLabelNeedsMindKeepsBlock`: current `[blocked_issue]`, want `needs_mind` → `Add=[needs-mind]`, `Remove=nil`.
  - `TestSetLabelBlockedIssueKeepsHumanLabels`: current `[needs-mind, needs-hands, blocked-external]`, want `blocked_issue` → `Add=[blocked-issue]`, `Remove=[blocked-external]`.
  - `TestSetLabelBlockedExternalLosesToIssue`: current `[blocked-issue]`, want `blocked_external` → `Add` en `Remove` leeg.
  - `TestMarksListsAll`: names met `needs-mind`, `blocked-issue`, `blocked-external`, `needs-hands` → alle vier sleutels in `Keys`-volgorde (Review Focus 4).
  - `cmd`-test: de uitvoer na `state label 7 needs_mind` op een issue met `blocked-issue` begint met `label: #7` en noemt beide namen.
  - `GateZero`-test: issue met `needs-mind` en open blocker, en met `needs-mind` plus `blocked-issue`, geeft géén stop; `blocked-issue` zonder `needs-mind` wél.
- [ ] **Stap 2: draai** `go test ./internal/issuestate ./internal/cli -run 'SetLabel|Marks|GateZero|StateLabel'`; verwacht FAIL.
- [ ] **Stap 3: implementeer** de signaturen hierboven; `Return` en `Apply` geven `LabelEdit` door; de uitvoerregel in `cmd.go` print `Marks` van `is.Labels` na de wijziging.
- [ ] **Stap 4: draai** dezelfde tests plus `go test ./internal/...`; verwacht PASS.
- [ ] **Stap 5: commit** — `feat(issuestate): rangregel per as, needs_mind naast een blokkade`

### Taak 3: `priorities.go` kent `needs_mind` (tijdelijke mapping)

**Bestanden:**
- Wijzig: `internal/priorities/priorities.go` (`Label` blijft in deze taak: gevuld uit de eerste sleutel van `Marks` met `needs_mind` vóór de rest; `issuestate.BlockingKey` vervalt hier), `internal/issuestate/label.go` (`BlockingKey` weg).
- Test: `internal/priorities/priorities_test.go`.

**Interfaces:**
- Consumeert: `issuestate.Marks`, `repoconfig.Labels.NeedsMind` (taak 1-2).
- Gedrag in deel a: `needs_mind` geeft de oude status `needs_shaping`; een open 🤖 Vraag blijft `needs_human`; `needs_hands` blijft `needs_human`. `Of` en `cases.json` zijn al in taak 1 aangepast; `derive.ts` is al in taak 1 gelijkgetrokken; deze taak vervangt het gebruik van `BlockingKey`.

- [ ] **Stap 1:** schrijf in `internal/priorities/priorities_test.go` een test dat `Issue.Label` voor `[needs-mind, blocked-external]` `needs_mind` is; dit is een gedragsbehoud-test en is al groen vóór stap 2 (`BlockingKey` geeft nog `needs_mind`): verwacht PASS, en na stap 2 nog steeds PASS.
- [ ] **Stap 2:** implementeer in `priorities.go` met `Marks` (`Of` en `derive.ts` zijn al klaar); haal `BlockingKey` weg.
- [ ] **Stap 3:** `go test ./internal/... && cd web && npx vitest run src/lib/priorities`; verwacht PASS.
- [ ] **Stap 4: commit** — `refactor(priorities): priorities.go leest Marks, gelijk gedrag`

### Taak 4: triage, tick en map-planned op `needs_mind`

**Bestanden:**
- Wijzig: `internal/triage/{rules,outcome,prompt,judgment}.go`, `internal/dispatch/decide.go` (`ReturnLabelKey` r.292, `heldByHuman` r.519, `finishReturn` r.1462), `internal/scheduled/mapplanned/{mapplanned,corpus}.go`.
- Test: `internal/triage/{rules,outcome,prompt,judgment}_test.go`, `internal/dispatch/decide_test.go`, `apply_test.go`, `internal/scheduled/mapplanned/job_test.go`.

**Interfaces:**
- `triage.Invariant(labels, l) []Deviation`: `Deviation{Kind:"blocking"}` alleen als `BlockedIssue` én `BlockedExternal` samen op het issue staan; `needs_mind` en `needs_hands` tellen niet mee.
- `dispatch.ReturnLabelKey(reason string, blocked bool) string`: `blocked` → `"blocked_issue"`; `reason_needs_brainstorm` of `reason_interactive` → `"needs_mind"`; rest → `"needs_hands"`.
- `heldByHuman(labels, l)` kijkt naar `NeedsMind`, `NeedsHands`, `BlockedExternal`.
- `finishReturn` rekende op de oude rangregel (`blocked_issue` wint van het label van de reden). Nu geeft `ReturnLabelKey` per as `needs_hands` náást `blocked_issue`: `finishReturn` roept `ReturnLabelKey(reason, true)` aan als `blocked_issue` al op het issue staat, zodat er geen extra mens-label bijkomt.
- Gate 0 (`FixedGate0`, `GateZero` uit taak 2) stopt niet meer bij `needs_mind`; de triage-uitvoer van een `needs_mind`-issue met een blokkade is route 4c met `— blocker #<x> open` (de blokkers uit `GateZero`/`OpenBlockers`).
- `RowDesign` zet `o.Label = "needs_mind"`; `judgment` eist route 4c bij het label `NeedsMind`; de prompt noemt `labels.needs_mind` en `labels.needs_hands`, niet meer de andere drie; map-planned zet `needs_mind` bij een wensvraag (foutmelding `a wish question needs labels.needs_mind`).

- [ ] **Stap 1: tests (rood)**
  - `TestInvariantBlockingPerAxis`: `[needs-mind, blocked-issue]` → geen afwijking; `[blocked-issue, blocked-external]` → één `blocking`; `[needs-mind, needs-hands]` → geen.
  - `TestReturnLabelKey`: tabel `reason_needs_brainstorm`→`needs_mind`, `reason_interactive`→`needs_mind`, `reason_pr_red`→`needs_hands`, `blocked=true`→`blocked_issue`.
  - `TestHeldByHuman`: `needs-mind` alleen → true; `blocked-issue` alleen → false (zoals nu).
  - triage-outcome-test: een issue met `needs-mind` en route niet 4c is ongeldig (`the issue carries needs-mind, so the route must be 4c`); een issue met `needs-mind` en `blocked-issue` met open blocker geeft géén `RowStop` maar route 4c met `blocker #<x> open`.
  - decide/apply-test voor `finishReturn`: Returned (reden `reason_pr_red`) op een issue met alleen `blocked-issue` → geen `needs-hands` erbij.
  - map-planned-test: een wensvraag zet `needs-mind` via `issuestate.Apply`.
- [ ] **Stap 2: draai** `go test ./internal/triage ./internal/dispatch ./internal/scheduled/...`; verwacht FAIL.
- [ ] **Stap 3: implementeer** per Interfaces; `FixedGate0` blijft `issuestate.GateZero` (nu zonder stop bij `needs_mind`).
- [ ] **Stap 4:** `go test ./...`; verwacht PASS.
- [ ] **Stap 5: commit** — `feat(triage): needs_mind in triage, tick en map-planned, invariant per as`

### Taak 5: `DeleteLabel` in de forge-laag

**Bestanden:**
- Wijzig: `internal/forge/forge.go` (interface naast `CreateLabel` r.123), `internal/forge/write_issue.go` (GitHub), `internal/forge/gitlab_write.go` (GitLab), elke fake van `forge.Forge` (`grep -rn "CreateLabel(ctx" internal --include=*_test.go internal/forge/forgesuite`), `internal/forge/forgesuite/suite.go`, de nepservers `internal/forge/forgetest/server_write.go` en `internal/forge/gitlabtest/server_write.go` (een `DELETE`-route die het label ook van issues haalt).
- Test: `internal/forge/write_issue_test.go`, `forgesuite/suite.go`, GitLab-test naast `gitlab_write.go`.

**Interfaces:**
- Produceert: `Forge.DeleteLabel(ctx context.Context, name string) error`. Een label dat niet bestaat is géén fout (idempotent). GitHub: `DELETE /repos/{repo}/labels/{name}` (naam URL-geëscaped, 404 → nil). GitLab: `DELETE /projects/:id/labels/:name` (404 → nil). Het label verdwijnt ook van issues; dat is de bedoeling van stap 6.

- [ ] **Stap 1: tests (rood):** `TestDeleteLabel` in de suite: maak label, `DeleteLabel`, `Labels()` bevat het niet meer; tweede `DeleteLabel` geeft nil; een naam met spatie of dubbele punt (`3-needs mind:x`) wordt geëscaped. Per forge één test tegen de bestaande httptest-server.
- [ ] **Stap 2:** draai `go test ./internal/forge/...`; verwacht FAIL (compileert niet).
- [ ] **Stap 3:** implementeer op beide forges en alle fakes.
- [ ] **Stap 4:** `go test ./...`; verwacht PASS.
- [ ] **Stap 5: commit** — `feat(forge): DeleteLabel voor GitHub en GitLab`

### Taak 6: migratie — taxonomiebestand en `lighthouse.toml`

**Bestanden:**
- Maak: `internal/labelmigrate/files.go`, `internal/labelmigrate/files_test.go`.

**Interfaces:**
- Produceert:
  - `type OldNames struct{ Shaping, Decision, Testing string }`.
  - `OldDefaults = OldNames{"needs-shaping","needs-decision","needs-testing"}`.
  - `ReadOldNames(tomlPath string) (OldNames, error)`: leest `[labels]` ruw (BurntSushi, zonder weigering); een ontbrekende sleutel krijgt de oude default. Een ontbrekend of onleesbaar toml is een fout.
  - `RewriteTaxonomy(src []byte, old OldNames, mind, hands string) ([]byte, error)`: in elke tabel met label- en kleurkolom worden de rijen van `old.Shaping` en `old.Decision` één rij `mind` met kleur `FBCA04`; de rij van `old.Testing` vervalt (`needs_hands` bestaat al; ontbreekt die, dan wordt de testing-rij hernoemd naar `hands`). Regels buiten tabellen, kolomvolgorde en andere rijen blijven byte-voor-byte. Geen oude rij aanwezig → ongewijzigd terug (idempotent).
  - `RewriteLabelsTable(src []byte, mind string) ([]byte, error)`: vervangt in `[labels]` de regels `needs_shaping`/`needs_decision`/`needs_testing` door één `needs_mind = "<mind>"`, andere regels en commentaar blijven; al `needs_mind` aanwezig → ongewijzigd.

- [ ] **Stap 1: tests (rood):**
  - `TestReadOldNamesWithoutLabelsTable`: toml zonder `[labels]` → `OldDefaults` (Review Focus 1).
  - `TestReadOldNamesCustom`: `needs_shaping = "3-needs-shaping"` → `Shaping == "3-needs-shaping"`, `Decision` = oude default.
  - `TestRewriteTaxonomyMergesRows`: tabel met rijen `3-needs-shaping`, `3-needs-decision`, `3-needs-testing`, `3-needs-hands` → één rij `3-needs-mind` (`FBCA04`) en één `3-needs-hands`.
  - `TestRewriteTaxonomyIdempotent`: twee keer toepassen geeft dezelfde bytes.
  - `TestRewriteLabelsTable`: voor/na met commentaar en `area_prefix` ongemoeid.
- [ ] **Stap 2:** `go test ./internal/labelmigrate`; FAIL.
- [ ] **Stap 3:** implementeer; hergebruik de parser van `internal/labels` voor tabelherkenning (exporteer een kleine `ParseRows` daar als dat nodig is, zonder het gedrag van `labels.Create` te wijzigen).
- [ ] **Stap 4:** `go test ./internal/labelmigrate ./internal/labels`; PASS.
- [ ] **Stap 5: commit** — `feat(labelmigrate): taxonomiebestand en [labels] herschrijven`

### Taak 7: migratie — plan en uitvoering op de forge

**Bestanden:**
- Maak: `internal/labelmigrate/plan.go`, `plan_test.go`.

**Interfaces:**
- Consumeert: `forge.Forge` (`Planning`, `Issue`, `EditIssue`, `IssueItem`, `Labels`, `CreateLabel`, `DeleteLabel`, `SetStatus`), `OldNames` (taak 6), `repoconfig.Labels` van het *nieuwe* schema.
- Produceert:
  - `type Step struct{ Kind string; Issue int; Add, Remove []string; Status string }` met `Kind` één van `relabel`, `block`, `status`, `delete-label`, `create-label`.
  - `BuildPlan(ctx, f forge.Forge, old OldNames, l repoconfig.Labels, runs RunLookup) ([]Step, error)`. `RunLookup func(issue int) (state string, err error)`; `nil` betekent "geen runs-database": stap 3 weigert dan (fout `stap 3 vraagt de runs-database`), de rest werkt.
  - `Execute(ctx, f forge.Forge, steps []Step, out io.Writer) error`: voert uit in volgorde `create-label` → `relabel` → `block` → `status` → `delete-label`; stopt bij de eerste fout; schrijft per stap een regel.
- Regels (spec §6): stap 1 per issue één `relabel` met `Add=[mind]` als het issue een van `old.Shaping`/`old.Decision` draagt, `Add=[hands]` als het `old.Testing` draagt en `hands` nog niet, `Remove` de oude namen — geen dubbele `Add`. Stap 2: heeft het issue (na stap 1) `mind` en een open `blocked_by` en geen `blocked_issue`, dan `block` met `Add=[blocked_issue]`. Stap 3: Status `In progress`, een blokkade, run-state niet `open`/`parked` → `status` met `Status="To do"` als voorstel; `Controller` komt nooit in het plan. Stap 6: `delete-label` per oude naam die op de forge bestaat, als laatste. `create-label` voor `mind` (`FBCA04`) als die op de forge ontbreekt.
- Idempotent: het plan wordt uit de actuele forge-stand opgebouwd, dus een stap waarvan het resultaat er al staat, komt niet in het plan.

- [ ] **Stap 1: tests (rood)** met een fake forge:
  - `TestBuildPlanMergesShapingAndDecision`: één issue met beide oude labels → één `relabel`, `Add=[3-needs-mind]` (Review Focus 2).
  - `TestBuildPlanTestingToHands`.
  - `TestBuildPlanBlocksMindWithOpenBlocker`: stap 2; en niet als `blocked_issue` er al staat.
  - `TestBuildPlanInProgressBlockedNoRun`: `status` To do; met `RunLookup` die `open` geeft → geen stap; `Controller` → geen stap; `RunLookup == nil` → fout.
  - `TestBuildPlanDeletesLabelsLast`: `delete-label` komt na alle andere stappen.
  - `TestExecuteResumesAfterFailure`: fake faalt op de tweede `relabel`; tweede `BuildPlan`+`Execute` op dezelfde fake eindigt in dezelfde forge-stand als een run zonder fout (Review Focus 3).
- [ ] **Stap 2:** `go test ./internal/labelmigrate`; FAIL.
- [ ] **Stap 3:** implementeer; `Planning` levert alleen open issues, dus `relabel` raakt gesloten issues niet (de label-delete haalt de oude labels daar wel weg).
- [ ] **Stap 4:** PASS.
- [ ] **Stap 5: commit** — `feat(labelmigrate): plan en uitvoering op de forge, idempotent`

### Taak 8: `lighthouse labels migrate`

**Bestanden:**
- Maak: `internal/cli/labels.go`, `internal/cli/labels_test.go`; wijzig `internal/cli/root.go` (`root.AddCommand(newLabelsCommand())`).

**Interfaces:**
- Consumeert: `labelmigrate.*` (taken 6-7), `flock.TryLock`, het pad van `tick.lock` uit `internal/cli/tick.go` (r.183-193: `$XDG_RUNTIME_DIR/lighthouse/tick.lock`; trek de padbepaling uit in `tickLockPath() (string, error)` en gebruik haar in `tick` en hier), `dbRunState` (`internal/cli/state.go`; die geeft bij een ontbrekende database `""` terug, dus de CLI controleert zelf met `os.Stat` op `store.DefaultPath()` en geeft dan `RunLookup=nil` door aan `BuildPlan`), de pauzevlag uit `internal/cli/pause.go`.
- Produceert: `lighthouse labels migrate [--stage all|files|forge] [--old key=name ...] [--yes]`.
- Gedrag: leest `lighthouse.toml` zelf via `labelmigrate.ReadOldNames`; `--old needs_shaping=3-needs-shaping` (herhaalbaar) overschrijft de namen (nodig in stage `forge` nadat `[labels]` al nieuw is). Stage `files`: stappen 4 en 5, geen forge, geen lock. Stage `forge`: stappen 1-3 en 6. Stage `all`: eerst stap 4 lezen (kan het taxonomiebestand niet gelezen worden → stop vóór alles), dan de rest in spec-volgorde. Voor elke forge-schrijffase: neem de tick-lock (`flock.TryLock`; bezet → fout `tick is running; pause and retry`), weiger als een run `open` is of als de pauzevlag niet staat (fout noemt `lighthouse pause`), toon het plan, vraag bevestiging (`--yes` slaat de vraag over, niet de preview), lees de forge daarna opnieuw, bouw het plan opnieuw binnen de lock en voer dat uit. Exit 0 bij succes, 1 bij fout, 2 bij een configfout.

- [ ] **Stap 1: tests (rood):**
  - `TestLabelsMigrateRefusesWithoutPause`: geen pauzevlag → exit 1, uitvoer noemt `lighthouse pause`.
  - `TestLabelsMigrateRefusesHeldLock`: een gehouden `tick.lock` → exit 1.
  - `TestLabelsMigrateRefusesOpenRun`.
  - `TestLabelsMigrateRefusesStep3WithoutDB`: geen runs-database → stap 3 weigert (fout `stap 3 vraagt de runs-database`), geen forge-schrijfaanroep.
  - `TestLabelsMigrateAbortsOnNo`: antwoord `n` → forge-fake kreeg geen schrijfaanroep.
  - `TestLabelsMigrateReadsOldToml`: een toml met oude sleutels laadt zonder fout (Review Focus 6) terwijl `config.Load` van hetzelfde bestand faalt.
  - `TestLabelsMigrateFilesStage`: schrijft taxonomie en `[labels]`, raakt de forge niet aan.
  - `TestLabelsMigrateRereadsAfterConfirm`: tussen preview en uitvoering verandert de fake-forge een issue; het uitgevoerde plan volgt de nieuwe stand.
- [ ] **Stap 2:** `go test ./internal/cli -run LabelsMigrate`; FAIL.
- [ ] **Stap 3:** implementeer; bevestiging leest van `cmd.InOrStdin()`.
- [ ] **Stap 4:** `go test ./internal/cli`; PASS.
- [ ] **Stap 5: commit** — `feat(cli): lighthouse labels migrate met preview, bevestiging en tick-lock`

### Taak 9: pack-skills, template, docs van deel a, dogfood

**Bestanden:**
- Wijzig: `pack/core/skills/route-issue/SKILL.md`, `pack/core/skills/start-issue/{claude,agents}/SKILL.md`, `pack/core/skills/write-issue/{claude,agents}/SKILL.md`, `pack/core/templates/labels.md`, `docs/developer/labels.md` (ook de rij- en rangtabel, met de hand: één rij `3-needs-mind` `FBCA04`, `needs_testing` opgegaan in `needs_hands`, de rangtabel vervangen door twee assen; dit is de enige taak die dat bestand herschrijft), `docs/tick.md`, `docs/triage.md`, `docs/priorities.md` (alleen de labelnamen en as-uitleg; de statusuitleg volgt in taak 12), `ci/literal-denylist.txt` indien die de oude namen noemt, `internal/packcontract/{skills_test,issueflow_test,writeissue_test}.go`, `internal/protocol/triage_test.go`.
- Wijzig via dogfood: `.claude/skills/*`, `.agents/skills/*` (`go run ./cmd/lighthouse init --upgrade`).
- Maak: `changelog.d/fix-statusmodel-mind-hands.md` (Engels, een tot drie zinnen voor een gebruiker, `docs/releasing.md`; de bestandsnaam is de branchnaam).

**Inhoud (uit spec §1 en §5):**
- route-issue: `needs_mind` → gate 4c; is er al een spec of gaat het om één keuze → smal (de open keuze eerst). Gate 0: `needs_mind` met een blokkade routeert door naar 4c met `— blocker #<x> open`; spec ja, plan nee.
- start-issue: board-start overgeslagen bij elke blokkade (ook met `needs_mind`); na een goedgekeurde spec gaat alleen `needs_mind` eraf, geen wissel naar `blocked_issue`.
- write-issue 3c/3e: `needs_mind` met open `blocked_by` is *label missing* zolang `blocked_issue` ontbreekt; het label komt erbij, `needs_mind` blijft.
- `labels.md` (pack-template en developer): twee assen in plaats van één rangtabel; de toets "kan ik dit gewoon tegen de agent zeggen?" blijft. `needs_hands` neemt testen op.
- Fragment (`type: changed`, een breaking config-wijziging; een `changed`-fragment wacht tot maandag op een release, zie taak 14): noem de nieuwe sleutels en `lighthouse labels migrate`.

- [ ] **Stap 1: tests (rood):** pas de pack-contracttests aan zodat ze de nieuwe sleutels en zinnen eisen (`labels.needs_mind` in route-issue; géén `labels.needs_shaping`, `needs_decision`, `needs_testing` in enige skill); `go test ./internal/packcontract ./internal/protocol`; FAIL.
- [ ] **Stap 2:** werk skills, templates en docs bij.
- [ ] **Stap 3:** `go run ./cmd/lighthouse init --upgrade`, daarna nogmaals; de tweede keer meldt `No changes.`
- [ ] **Stap 4:** `grep -rIn "needs_shaping\|needs_decision\|needs_testing\|needs-shaping\|needs-decision\|needs-testing" --exclude-dir=.git --exclude-dir=node_modules . | grep -v 'docs/developer/planning\|CHANGELOG.md\|internal/labelmigrate\|internal/cli/labels'` is leeg, met `internal/config`, `internal/repoconfig`, `internal/projectconfig` en hun tests als extra uitzondering (de oude-sleutelweigering noemt die namen bewust).
- [ ] **Stap 5:** `go test ./...`; PASS. Commit — `docs(labels): needs_mind en needs_hands in skills, template en docs`

---

## Deel b — status en vorm

### Taak 10: vijf statussen en `Blocked` in `Of`, `cases.json`, `derive.ts`

**Bestanden:**
- Wijzig: `internal/issuestate/state.go`, `tests/pack/fixtures/issue-state/cases.json`, `web/src/lib/priorities/derive.ts`, `internal/priorities/priorities.go`, `internal/issuestate/cmd.go` (`state show`), alle gebruikers van `issuestate.State` (`grep -rn "issuestate\.\(NeedsHuman\|NeedsShaping\|Blocked\)" internal`).
- Test: `internal/issuestate/state_test.go`, `web/src/lib/priorities/derive.test.ts`, `internal/priorities/priorities_test.go`, `internal/server/priorities_test.go`, `internal/cli/serve_test.go`, `internal/cli/state_test.go`.
- Wijzig ook: `web/openapi.json` en de gegenereerde client `web/src/api/` (`go test ./internal/server -run TestOpenAPISnapshot -update`, daarna `cd web && npm run gen`; zonder dat is `TestOpenAPISnapshot` rood en kent `typecheck` `issue.labels` niet).

**Interfaces:**
- Produceert Go: `State` met `Done`, `NeedsMind`, `NeedsHands`, `Active`, `Ready` (waarden `"done"`, `"needs_mind"`, `"needs_hands"`, `"active"`, `"ready"`); `Result{State State; Blocked bool}`; `Input` krijgt niets nieuws. `Of(in Input, l repoconfig.Labels) Result` volgt de statusregel uit de globale beperkingen. `Blocked` is waar bij een blokkade, behalve bij `done`.
- Produceert web: `IssueStateName = 'done'|'needs_mind'|'needs_hands'|'active'|'ready'`; `issueState(issue, runs, repo): { state: IssueStateName; blocked: boolean }` — de wijzigende handtekening raakt `GNode.state` (`GNode.blocked: boolean` erbij) en `Block` (`blocked: boolean` erbij, `buildGraph` vult beide).
- Produceert API: `priorities.Issue.Labels []string` (de `Keys` van `Marks`, nooit nil) vervangt `Label` en `NeedsLabel`; `open_question` blijft. `derive.ts` leest `issue.labels`.
- `state show`: `state: #<nr> <status>` en `, blocked` erachter bij een blokkade.

- [ ] **Stap 1: `cases.json` (rood)** — herschrijf naar de nieuwe uitkomst (`want: {"state": …, "blocked": …}`); minstens één geval per rij, plus de acht combinaties uit §2: `needs_mind` met blokkade; `needs_hands` met blokkade (verwacht `ready`, blocked); open run met blokkade (`active`, blocked); `In progress` met blokkade zonder run (`ready`, blocked); `Controller` met blokkade zonder run (`active`, blocked); `parked` run met `blocked_issue` (`active`, blocked); `needs_mind` met `blocked_external` (`needs_mind`, blocked); gesloten issue met blokkadelabel (`done`, niet blocked — Review Focus 5). Ook: `needs_hands` met open run (`needs_hands`); open 🤖 Vraag (`needs_mind`); `needs_mind` met open run (`needs_mind`, nooit active).
- [ ] **Stap 2:** `go test ./internal/issuestate -run Cases` en `cd web && npx vitest run src/lib/priorities`; FAIL.
- [ ] **Stap 3:** implementeer Go en `derive.ts` volgens de statusregel; `derive.ts` gebruikt `liveRun` (`open` en `parked`).
- [ ] **Stap 4:** `/api/priorities`: vul `Labels` met `Marks`, behalve voor gesloten issues (`Planning` levert alleen open; test dat een issue met gesloten state geen `labels` en `blocked_by` meekrijgt waar milestones `Closed` laden). Test in `internal/priorities/priorities_test.go` dat een issue met `[needs-mind, blocked-external]` `labels == ["needs_mind","blocked_external"]` geeft.
- [ ] **Stap 5:** `go test ./internal/server -run TestOpenAPISnapshot -update`, `cd web && npm run gen`; dan `go test ./... && cd web && npx vitest run && npm run typecheck && npm run lint`; PASS. (De component-tests uit taak 11 mogen hier nog op de oude `state`-vorm leunen; pas ze minimaal aan zodat ze compileren.)
- [ ] **Stap 6: commit** — `feat(issuestate): vijf statussen plus Blocked, labels per as in de API`

### Taak 11: blokje met kruis-overlay, balkvolgorde, legenda

**Bestanden:**
- Wijzig: `web/src/components/priorities/Block.tsx`, `MilestonesView.tsx`, `DepsView.tsx`, `NodeCard.tsx`, `Toolbar.tsx`, `web/src/lib/priorities/derive.ts` (`GROUP_ORDER`, sortering binnen *to do*), `web/src/index.css` indien de kruisklasse daar staat.
- Test: `Block.test.tsx`, `DepsView.test.tsx`, `MilestonesView.test.tsx`, `PrioritiesTab.test.tsx`, `derive.test.ts`.

**Interfaces:**
- Consumeert: `IssueStateName`, `Block.blocked` (taak 10).
- Produceert: `BlockProps` krijgt `blocked?: boolean`; `SHAPE` kent de vijf statussen met de vormen uit de globale beperkingen; `Cross` wordt over élke vorm getekend als `blocked`; het nummer is onderstreept (`data-underlined="true"`) bij `needs_mind` en `needs_hands`, ook met kruis. `GROUP_ORDER = ['done','needs_hands','active','ready','needs_mind']`; binnen `ready` staan blokken met `blocked=false` vóór `blocked=true` (stabiel).
- Legenda in *Dependencies* en *Milestones*: drie regels met de kopjes `Milestones`, `Progress` (to do · active · done) en `Labels` (needs mind · blocked · needs hands, met hun vorm; het label van *needs mind* en *needs hands* onderstreept). Het woord `Status` staat in deze legenda niet meer.
- Het kruis over de stippelvorm is een extra laag (`<Cross/>` absoluut over de stippelrand), leesbaar in 14 × 14 px.

- [ ] **Stap 1: tests (rood):**
  - `Block.test.tsx`: per status de verwachte `data-state` en stijl (bijvoorbeeld `needs_mind` heeft `outline: 1.5px dashed`, `needs_hands` heeft `box-shadow: inset 0 0 0 2px`); `blocked` toont het kruis bij `ready`, `needs_mind` en `active`; `data-underlined` alleen bij `needs_mind`/`needs_hands`.
  - `derive.test.ts`: `milestoneRows` zet blokken in volgorde done, needs_hands, active, ready (zonder kruis eerst), needs_mind.
  - `MilestonesView.test.tsx` en `DepsView.test.tsx`: de legenda heeft de kopjes `Milestones`, `Progress`, `Labels` en niet `Status`.
- [ ] **Stap 2:** `cd web && npx vitest run src/components/priorities src/lib/priorities`; FAIL.
- [ ] **Stap 3:** implementeer; houd `NodeCard` (graaf) op dezelfde `Block`-regels.
- [ ] **Stap 4:** `cd web && npx vitest run && npm run typecheck && npm run lint`; PASS.
- [ ] **Stap 5: commit** — `feat(web): vijf vormen met kruis voor blocked, nieuwe legenda en balkvolgorde`

### Taak 12: docs en afsluiting van deel b

**Bestanden:**
- Wijzig: `docs/priorities.md` (vijf statussen, kruis, legenda, `labels` in `/api/priorities`), `docs/developer/labels.md` (as-uitleg blijft; verwijs naar de statusregel), `docs/tick.md` (de tick zet bij `needs_*` geen run en `To do`), changelog-fragment uit taak 9 (voeg b toe).
- Dogfood opnieuw: `go run ./cmd/lighthouse init --upgrade` moet `No changes.` geven.

- [ ] **Stap 1:** werk de docs bij; `lighthouse imports check`; de md-link-check-hook mag niet blokkeren.
- [ ] **Stap 2:** volledige gate: `go test -race ./... && cd web && npx vitest run && npm run typecheck && npm run lint`; PASS.
- [ ] **Stap 3: commit** — `docs(prioriteiten): statusmodel met vijf statussen en labels als markering`

---

## Afronden en uitrol

### Taak 13: afronden en PR

- [ ] **Stap 1:** `lighthouse evidence`/`/lighthouse-write-pr 588` verifieert de acceptatiecriteria met bewijs per regel; criterium "migratie gedraaid" is pas na taak 14 aantoonbaar, dus de PR is `Refs #588 — <reden>` met een Evidence-tabel, niet `Closes`. De PR-body bevat de uitrolstappen van taak 14 letterlijk.
- [ ] **Stap 2:** deel c is #557; laat het issue de kanban-wijzigingen niet bevatten.
- [ ] **Stap 3:** plaats na het openen van de PR één `lighthouse ask` met de commando's van taak 14 (elk via `--command`, opties *klaar* en *anders*). De bouw wacht er niet op; het mergen doet de gebruiker, nooit de agent.

### Taak 14: migratie draaien (mensstappen, na de PR)

Dit lost r3-f1 op. De spec liet lighthouse zelf vóór de PR migreren met de tick gepauzeerd tot na de merge; dat legt alle dispatch-repo's stil tijdens review en CI. Hier is de pauze alleen het venster rond de merge, en de `lighthouse.toml`- en taxonomie-wijziging staan al in de PR (taken 1 en 9, met de hand), zodat CI (`init --upgrade` → `No changes.`) de nieuwe sleutels ziet. De tick leest `lighthouse.toml` en de labelnamen pas bij een tick; zolang `main` nog oude sleutels heeft en de forge oude labels, klopt alles voor de oude binary. Deze taak wijkt daarmee bewust af van §6 *Volgorde per repo*, zoals de spec zelf toelaat (*Open na review*, r3-f1); de acceptatiecriteria van #588 (geen open issue met de oude labels) blijven gelden.

**Venster rond de merge (korte onderhoudsperiode), op de tick-host, door de gebruiker:**

- [ ] **Stap 1:** `lighthouse pause --until HH:MM` met een tijd die het hele venster dekt (zonder `--until` eindigt de pauze bij het begin van de nacht, vóór een mogelijke release); wacht tot geen run `open` is.
- [ ] **Stap 2:** de PR mergen. Een fragment van het type `changed` wacht tot maandag 06:00 UTC op een release (`docs/releasing.md`): start de `release`-workflow met de hand (`workflow_dispatch`, `bump: minor`) en wacht op de release. Installeer de nieuwe binary (`lighthouse self-update`). Zolang de nieuwe binary er niet is, draait de oude tick op de nieuwe `lighthouse.toml`; daarom dekt de pauze het hele venster.
- [ ] **Stap 3:** `lighthouse labels migrate --stage forge --old needs_shaping=3-needs-shaping --old needs_decision=3-needs-decision --old needs_testing=3-needs-testing`: preview, bevestigen, uitvoeren.
- [ ] **Stap 4:** `lighthouse pause off`. Het venster is dicht. Controle: `gh issue list --label 3-needs-shaping` en dezelfde voor `3-needs-decision` en `3-needs-testing` geven niets; `lighthouse state show 379` toont `blocked` (na deel b).

**`personal_ai_agent` (pack v0.9.0, oude sleutels):**

- [ ] **Stap 5:** tick pauzeren, nieuwe binary, `lighthouse labels migrate` (stage `all`), direct `lighthouse init --upgrade`, committen via een PR, `lighthouse pause off`. Tussen de binary en `init --upgrade` falen `lighthouse config`-aanroepen van die repo; daarom staat de tick stil.

## Zelfreview

- **Dekking:** §1 → taken 1-2, 4; §2 → 3, 10; §3 en §4 → 11; §5 → 4, 9; §6 → 5-8, 14; §7 is deel c (buiten dit plan, #557). Doelen "één regel Go en web" → taken 1, 3 en 10 delen `cases.json`.
- **Types:** `Marks`, `LabelEdit{Add, Remove}`, `Result{State, Blocked}`, `issuestate.Keys` en `ReturnLabelKey` hebben overal dezelfde naam; `BlockingKey` verdwijnt in taak 3 en zijn enige gebruiker (`priorities.go`) krijgt in taak 3 `Marks` en in taak 10 `Labels`.
- **Open punt:** in taak 3 gebruikt `priorities.Issue.Label` tijdelijk de eerste sleutel van `Marks`; taak 10 verwijdert het veld.

## Open na review

Ronde 3 haalde de rondegrens met deze punten open. Ze zijn aanvaard als `accepted_open`;
de volgende fase of de PR-review weegt ze opnieuw.

- **r3-f1** (MAJOR, Taak 3 stap 1-2): The rewritten Taak 3 stap 1 adds a priorities test that `Issue.Label` for `[needs-mind, blocked-external]` is `needs_mind` and expects FAIL. Before stap 2 that test already passes. BlockingKey still exists after Taak 2 (Taak 2 says 'BlockingKey blijft tot taak 3') and walks the new `Keys` from Taak 2 in order, with `needs_mind` first, so it returns `needs_mind` for this input. The stated reason, '(BlockingKey kent het niet meer na stap 2)', describes the state after the implementation, not before it.
