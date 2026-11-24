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

## Handmatige stappen (vooraf één vraag via `lighthouse ask`)

Alleen taak 13 vraagt de gebruiker iets: pauzeren, binary installeren, migratie draaien op de tick-host en in `personal_ai_agent`. Stel die ene vraag aan het begin van de uitvoering, met elk commando uit taak 13 letterlijk via `--command`, opties *klaar* en *anders*. Taken 1–12 en 14 hebben geen mensstap.

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
- Wijzig: `internal/config/config.go` (schema ~r.91-93, defaults ~r.154-157, `Load` r.651), `internal/repoconfig/repoconfig.go` (`Labels` r.63, `Blocking`, `NeedsName`, `rawFile.Labels`, `DefaultLabels`, `LoadFile`), `internal/projectconfig/projectconfig.go:235` (`LabelTables`), `tests/pack/fixtures/config-schema.json`, `tests/pack/fixtures/config/{full,defaults}/*`, `docs/site/data/config.json`, `lighthouse.toml` blijft in deze taak ongewijzigd.
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
- [ ] **Stap 4: herstel de rest** — werk fixtures en `config-schema.json` bij; regenereer `docs/site/data/config.json` met het commando dat `docs/site/` daarvoor heeft (`grep -rn "config.json" docs/site tools` toont het), en laat de bestaande tests die de oude namen noemen de nieuwe namen gebruiken.
- [ ] **Stap 5: volledige Go-tests** — `go build ./... && go vet ./... && go test ./...`; verwacht groen. Het eigen `lighthouse.toml` heeft nog oude sleutels: `internal/cli`-tests die dat bestand laden mogen niet rood worden; gebruik hun eigen fixture-toml.
- [ ] **Stap 6: commit** — `refactor(labels): needs_mind en needs_hands in [labels], oude sleutels geweigerd`

### Taak 2: rangregel per as in `issuestate`

**Bestanden:**
- Wijzig: `internal/issuestate/label.go`, `internal/issuestate/state.go` (`GateZero`, `HasNeedsLabel`), `internal/issuestate/cmd.go` (uitvoerregel `label:`), `internal/cli/state.go` (help-tekst), `internal/issuestate/giveback.go`.
- Test: `internal/issuestate/label_test.go`, `giveback_test.go`, `state_test.go`, `internal/cli/state_test.go`.

**Interfaces:**
- Produceert: `issuestate.Keys = []string{"needs_mind","blocked_issue","blocked_external","needs_hands"}`; `Name(key, l)` kent deze vier; `Marks(names []string, l repoconfig.Labels) []string` (de sleutels van blokkade- en mens-labels op het issue, in de volgorde van `Keys`); `SetLabel(current []string, wantKey string, l) (LabelEdit, error)` met `LabelEdit{Add, Remove []string}` (het veld `Winner` vervalt); `GateZero` onveranderd van vorm. `BlockingKey` vervalt (taak 10 vervangt het gebruik).
- Rang: `needs_mind` en `needs_hands` zetten voegt alleen dat label toe en haalt niets weg. `blocked_issue` zetten voegt toe en haalt alleen `blocked_external` weg. `blocked_external` zetten met `blocked_issue` aanwezig voegt niets toe (`Add` leeg).

- [ ] **Stap 1: tests (rood)** in `label_test.go`:
  - `TestSetLabelNeedsMindKeepsBlock`: current `[blocked_issue]`, want `needs_mind` → `Add=[needs-mind]`, `Remove=nil`.
  - `TestSetLabelBlockedIssueKeepsHumanLabels`: current `[needs-mind, needs-hands, blocked-external]`, want `blocked_issue` → `Add=[blocked-issue]`, `Remove=[blocked-external]`.
  - `TestSetLabelBlockedExternalLosesToIssue`: current `[blocked-issue]`, want `blocked_external` → `Add` en `Remove` leeg.
  - `TestMarksListsAll`: names met `needs-mind`, `blocked-issue`, `blocked-external`, `needs-hands` → alle vier sleutels in `Keys`-volgorde (Review Focus 4).
  - `cmd`-test: de uitvoer na `state label 7 needs_mind` op een issue met `blocked-issue` begint met `label: #7` en noemt beide namen.
  - `GateZero`-test: issue met `needs-mind` en open blocker geeft géén stop (zoals nu met `needs_shaping`), met `blocked-issue` wél (ongewijzigd).
- [ ] **Stap 2: draai** `go test ./internal/issuestate ./internal/cli -run 'SetLabel|Marks|GateZero|StateLabel'`; verwacht FAIL.
- [ ] **Stap 3: implementeer** de signaturen hierboven; `Return` en `Apply` geven `LabelEdit` door; de uitvoerregel in `cmd.go` print `Marks` van `is.Labels` na de wijziging.
- [ ] **Stap 4: draai** dezelfde tests plus `go test ./internal/...`; verwacht PASS.
- [ ] **Stap 5: commit** — `feat(issuestate): rangregel per as, needs_mind naast een blokkade`

### Taak 3: `Of` en `derive.ts` kennen `needs_mind` (tijdelijke mapping)

**Bestanden:**
- Wijzig: `internal/issuestate/state.go` (`Of`), `web/src/lib/priorities/derive.ts` (`issueState`), `tests/pack/fixtures/issue-state/cases.json`, `internal/priorities/priorities.go` (`Label` blijft in deze taak: gevuld uit de eerste sleutel van `Marks` met `needs_mind` vóór de rest).
- Test: `internal/issuestate/state_test.go`, `web/src/lib/priorities/derive.test.ts`.

**Interfaces:**
- Consumeert: `issuestate.Marks`, `repoconfig.Labels.NeedsMind` (taak 1-2).
- Gedrag in deel a: `needs_mind` geeft de oude status `needs_shaping`; een open 🤖 Vraag blijft `needs_human`; `needs_hands` blijft `needs_human`. In `cases.json` worden de labelnamen vervangen (`needs_shaping`/`needs_decision` → `needs_mind`; `needs_testing` → `needs_hands`) en de gevallen met `needs_decision` verwachten `needs_shaping`.

- [ ] **Stap 1:** pas `cases.json` aan zoals hierboven; draai `go test ./internal/issuestate -run Cases` en `cd web && npx vitest run src/lib/priorities/derive.test.ts`; verwacht FAIL op de gewijzigde gevallen.
- [ ] **Stap 2:** implementeer in `Of`: overal waar `l.NeedsShaping` of `l.NeedsDecision` stond, nu `l.NeedsMind` met resultaat `NeedsShaping`; `l.NeedsTesting` vervalt. In `derive.ts`: `issue.label === 'needs_mind'` → `needs_shaping`; `NEEDS_LATER` is alleen `needs_hands`; `needs_decision` vervalt.
- [ ] **Stap 3:** draai beide testsets; verwacht PASS.
- [ ] **Stap 4: commit** — `refactor(issuestate): Of en derive.ts lezen needs_mind, gelijk gedrag`

### Taak 4: triage, tick en map-planned op `needs_mind`

**Bestanden:**
- Wijzig: `internal/triage/{rules,outcome,prompt,judgment}.go`, `internal/dispatch/decide.go` (`ReturnLabelKey` r.292, `heldByHuman` r.519), `internal/scheduled/mapplanned/{mapplanned,corpus}.go`.
- Test: `internal/triage/{rules,outcome,prompt,judgment}_test.go`, `internal/dispatch/decide_test.go`, `apply_test.go`, `internal/scheduled/mapplanned/job_test.go`.

**Interfaces:**
- `triage.Invariant(labels, l) []Deviation`: `Deviation{Kind:"blocking"}` alleen als `BlockedIssue` én `BlockedExternal` samen op het issue staan; `needs_mind` en `needs_hands` tellen niet mee.
- `dispatch.ReturnLabelKey(reason string, blocked bool) string`: `blocked` → `"blocked_issue"`; `reason_needs_brainstorm` of `reason_interactive` → `"needs_mind"`; rest → `"needs_hands"`.
- `heldByHuman(labels, l)` kijkt naar `NeedsMind`, `NeedsHands`, `BlockedExternal`.
- `RowDesign` zet `o.Label = "needs_mind"`; `judgment` eist route 4c bij het label `NeedsMind`; de prompt noemt `labels.needs_mind` en `labels.needs_hands`, niet meer de andere drie; map-planned zet `needs_mind` bij een wensvraag (foutmelding `a wish question needs labels.needs_mind`).

- [ ] **Stap 1: tests (rood)**
  - `TestInvariantBlockingPerAxis`: `[needs-mind, blocked-issue]` → geen afwijking; `[blocked-issue, blocked-external]` → één `blocking`; `[needs-mind, needs-hands]` → geen.
  - `TestReturnLabelKey`: tabel `reason_needs_brainstorm`→`needs_mind`, `reason_interactive`→`needs_mind`, `reason_pr_red`→`needs_hands`, `blocked=true`→`blocked_issue`.
  - `TestHeldByHuman`: `needs-mind` alleen → true; `blocked-issue` alleen → false (zoals nu).
  - triage-outcome-test: een issue met `needs-mind` en route niet 4c is ongeldig (`the issue carries needs-mind, so the route must be 4c`).
  - map-planned-test: een wensvraag zet `needs-mind` via `issuestate.Apply`.
- [ ] **Stap 2: draai** `go test ./internal/triage ./internal/dispatch ./internal/scheduled/...`; verwacht FAIL.
- [ ] **Stap 3: implementeer** per Interfaces; `FixedGate0` blijft `issuestate.GateZero`.
- [ ] **Stap 4:** `go test ./...`; verwacht PASS.
- [ ] **Stap 5: commit** — `feat(triage): needs_mind in triage, tick en map-planned, invariant per as`

### Taak 5: `DeleteLabel` in de forge-laag

**Bestanden:**
- Wijzig: `internal/forge/forge.go` (interface naast `CreateLabel` r.123), `internal/forge/write_issue.go` (GitHub), `internal/forge/gitlab_write.go` (GitLab), elke fake van `forge.Forge` (`grep -rn "CreateLabel(ctx" internal --include=*_test.go internal/forge/forgesuite`), `internal/forge/forgesuite/suite.go`.
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
- Consumeert: `labelmigrate.*` (taken 6-7), `flock.TryLock`, het pad van `tick.lock` uit `internal/cli/tick.go` (r.183-200: `$XDG_RUNTIME_DIR/tick.lock`; trek de padbepaling uit in `tickLockPath() (string, error)` en gebruik haar in `tick` en hier), `dbRunState` (`internal/cli/state.go`), de pauzevlag uit `internal/cli/pause.go`.
- Produceert: `lighthouse labels migrate [--stage all|files|forge] [--old key=name ...] [--yes]`.
- Gedrag: leest `lighthouse.toml` zelf via `labelmigrate.ReadOldNames`; `--old needs_shaping=3-needs-shaping` (herhaalbaar) overschrijft de namen (nodig in stage `forge` nadat `[labels]` al nieuw is). Stage `files`: stappen 4 en 5, geen forge, geen lock. Stage `forge`: stappen 1-3 en 6. Stage `all`: eerst stap 4 lezen (kan het taxonomiebestand niet gelezen worden → stop vóór alles), dan de rest in spec-volgorde. Voor elke forge-schrijffase: neem de tick-lock (`flock.TryLock`; bezet → fout `tick is running; pause and retry`), weiger als een run `open` is of als de pauzevlag niet staat (fout noemt `lighthouse pause`), toon het plan, vraag bevestiging (`--yes` slaat de vraag over, niet de preview), lees de forge daarna opnieuw, bouw het plan opnieuw binnen de lock en voer dat uit. Exit 0 bij succes, 1 bij fout, 2 bij een configfout.

- [ ] **Stap 1: tests (rood):**
  - `TestLabelsMigrateRefusesWithoutPause`: geen pauzevlag → exit 1, uitvoer noemt `lighthouse pause`.
  - `TestLabelsMigrateRefusesHeldLock`: een gehouden `tick.lock` → exit 1.
  - `TestLabelsMigrateRefusesOpenRun`.
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
- Wijzig: `pack/core/skills/route-issue/SKILL.md`, `pack/core/skills/start-issue/{claude,agents}/SKILL.md`, `pack/core/skills/write-issue/{claude,agents}/SKILL.md`, `pack/core/templates/labels.md`, `docs/developer/labels.md`, `docs/tick.md`, `docs/triage.md`, `docs/priorities.md` (alleen de labelnamen en as-uitleg; de statusuitleg volgt in taak 12), `ci/literal-denylist.txt` indien die de oude namen noemt, `internal/packcontract/{skills_test,issueflow_test,writeissue_test}.go`, `internal/protocol/triage_test.go`.
- Wijzig via dogfood: `.claude/skills/*`, `.agents/skills/*` (`go run ./cmd/lighthouse init --upgrade`).
- Maak: `changelog.d/issue-588-statusmodel-mind-hands.md`.

**Inhoud (uit spec §1 en §5):**
- route-issue: `needs_mind` → gate 4c; is er al een spec of gaat het om één keuze → smal (de open keuze eerst). Gate 0: `needs_mind` met een blokkade routeert door naar 4c met `— blocker #<x> open`; spec ja, plan nee.
- start-issue: board-start overgeslagen bij elke blokkade (ook met `needs_mind`); na een goedgekeurde spec gaat alleen `needs_mind` eraf, geen wissel naar `blocked_issue`.
- write-issue 3c/3e: `needs_mind` met open `blocked_by` is *label missing* zolang `blocked_issue` ontbreekt; het label komt erbij, `needs_mind` blijft.
- `labels.md` (pack-template en developer): twee assen in plaats van één rangtabel; de toets "kan ik dit gewoon tegen de agent zeggen?" blijft. `needs_hands` neemt testen op.
- Fragment (`type: internal` of het type dat `docs/releasing.md` voor gebruikerszichtbaar noemt — kies dat laatste, het is een breaking config-wijziging): noem de nieuwe sleutels en `lighthouse labels migrate`.

- [ ] **Stap 1: tests (rood):** pas de pack-contracttests aan zodat ze de nieuwe sleutels en zinnen eisen (`labels.needs_mind` in route-issue; géén `labels.needs_shaping`, `needs_decision`, `needs_testing` in enige skill); `go test ./internal/packcontract ./internal/protocol`; FAIL.
- [ ] **Stap 2:** werk skills, templates en docs bij.
- [ ] **Stap 3:** `go run ./cmd/lighthouse init --upgrade`, daarna nogmaals; de tweede keer meldt `No changes.`
- [ ] **Stap 4:** `grep -rIn "needs_shaping\|needs_decision\|needs_testing\|needs-shaping\|needs-decision\|needs-testing" --exclude-dir=.git --exclude-dir=node_modules . | grep -v 'docs/developer/planning\|CHANGELOG.md\|internal/labelmigrate\|internal/cli/labels'` is leeg, op `lighthouse.toml` en de taxonomie na (die migreert taak 13).
- [ ] **Stap 5:** `go test ./...`; PASS. Commit — `docs(labels): needs_mind en needs_hands in skills, template en docs`

---

## Deel b — status en vorm

### Taak 10: vijf statussen en `Blocked` in `Of`, `cases.json`, `derive.ts`

**Bestanden:**
- Wijzig: `internal/issuestate/state.go`, `tests/pack/fixtures/issue-state/cases.json`, `web/src/lib/priorities/derive.ts`, `internal/priorities/priorities.go`, `internal/issuestate/cmd.go` (`state show`), alle gebruikers van `issuestate.State` (`grep -rn "issuestate\.\(NeedsHuman\|NeedsShaping\|Blocked\)" internal`).
- Test: `internal/issuestate/state_test.go`, `web/src/lib/priorities/derive.test.ts`, `internal/priorities/priorities_test.go`, `internal/server/priorities_test.go`, `internal/cli/serve_test.go`, `internal/cli/state_test.go`.

**Interfaces:**
- Produceert Go: `State` met `Done`, `NeedsMind`, `NeedsHands`, `Active`, `Ready` (waarden `"done"`, `"needs_mind"`, `"needs_hands"`, `"active"`, `"ready"`); `Result{State State; Blocked bool}`; `Input` krijgt niets nieuws. `Of(in Input, l repoconfig.Labels) Result` volgt de statusregel uit de globale beperkingen. `Blocked` is waar bij een blokkade, behalve bij `done`.
- Produceert web: `IssueStateName = 'done'|'needs_mind'|'needs_hands'|'active'|'ready'`; `issueState(issue, runs, repo): { state: IssueStateName; blocked: boolean }` — de wijzigende handtekening raakt `GNode.state` (`GNode.blocked: boolean` erbij) en `Block` (`blocked: boolean` erbij, `buildGraph` vult beide).
- Produceert API: `priorities.Issue.Labels []string` (de `Keys` van `Marks`, nooit nil) vervangt `Label` en `NeedsLabel`; `open_question` blijft. `derive.ts` leest `issue.labels`.
- `state show`: `state: #<nr> <status>` en `, blocked` erachter bij een blokkade.

- [ ] **Stap 1: `cases.json` (rood)** — herschrijf naar de nieuwe uitkomst (`want: {"state": …, "blocked": …}`); minstens één geval per rij, plus de acht combinaties uit §2: `needs_mind` met blokkade; `needs_hands` met blokkade (verwacht `ready`, blocked); open run met blokkade (`active`, blocked); `In progress` met blokkade zonder run (`ready`, blocked); `Controller` met blokkade zonder run (`active`, blocked); `parked` run met `blocked_issue` (`active`, blocked); `needs_mind` met `blocked_external` (`needs_mind`, blocked); gesloten issue met blokkadelabel (`done`, niet blocked — Review Focus 5). Ook: `needs_hands` met open run (`needs_hands`); open 🤖 Vraag (`needs_mind`); `needs_mind` met open run (`needs_mind`, nooit active).
- [ ] **Stap 2:** `go test ./internal/issuestate -run Cases` en `cd web && npx vitest run src/lib/priorities`; FAIL.
- [ ] **Stap 3:** implementeer Go en `derive.ts` volgens de statusregel; `derive.ts` gebruikt `liveRun` (`open` en `parked`).
- [ ] **Stap 4:** `/api/priorities`: vul `Labels` met `Marks`, behalve voor gesloten issues (`Planning` levert alleen open; test dat een issue met gesloten state geen `labels` en `blocked_by` meekrijgt waar milestones `Closed` laden). Test in `internal/priorities/priorities_test.go` dat een issue met `[needs-mind, blocked-external]` `labels == ["needs_mind","blocked_external"]` geeft.
- [ ] **Stap 5:** `go test ./... && cd web && npx vitest run && npm run typecheck && npm run lint`; PASS. (De component-tests uit taak 11 mogen hier nog op de oude `state`-vorm leunen; pas ze minimaal aan zodat ze compileren.)
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

## Uitrol

### Taak 13: migratie draaien (mensstappen, na de merge)

Dit lost r3-f1 op. De spec liet lighthouse zelf vóór de PR migreren met de tick gepauzeerd tot na de merge; dat legt alle dispatch-repo's stil tijdens review en CI. Hier is de pauze alleen het venster rond de merge. De tick leest `lighthouse.toml` en de labelnamen pas bij een tick; zolang `main` nog oude sleutels heeft en de forge oude labels, klopt alles voor de oude binary. Deze taak wijkt daarmee bewust af van §6 *Volgorde per repo*; de acceptatiecriteria van #588 (geen open issue met de oude labels) blijven gelden.

**Vóór de PR (geen pauze, geen forge-schrijfactie), in de worktree van deze branch:**

- [ ] **Stap 1:** `go run ./cmd/lighthouse labels migrate --stage files` herschrijft het taxonomiebestand en `[labels]` in `lighthouse.toml`; commit beide (`chore(labels): lighthouse.toml en taxonomie naar needs_mind`). CI (`init --upgrade` → `No changes.`) ziet nu de nieuwe sleutels. De PR volgt via `/lighthouse-write-pr`.

**Venster rond de merge (korte onderhoudsperiode), op de tick-host, door de gebruiker:**

- [ ] **Stap 2:** `lighthouse pause`; wacht tot geen run `open` is.
- [ ] **Stap 3:** de PR mergen (de gebruiker; nooit de agent), de release afwachten, de nieuwe binary installeren (`lighthouse self-update`).
- [ ] **Stap 4:** `lighthouse labels migrate --stage forge --old needs_shaping=3-needs-shaping --old needs_decision=3-needs-decision --old needs_testing=3-needs-testing`: preview, bevestigen, uitvoeren.
- [ ] **Stap 5:** `lighthouse pause --off` (de exacte vlag staat in `internal/cli/pause.go`). Het venster is dicht. Controle: `gh issue list --label 3-needs-shaping` en dezelfde voor `3-needs-decision` en `3-needs-testing` geven niets; `lighthouse state show 379` toont `blocked` (na deel b).

**`personal_ai_agent` (pack v0.9.0, oude sleutels):**

- [ ] **Stap 6:** tick pauzeren, nieuwe binary, `lighthouse labels migrate` (stage `all`), direct `lighthouse init --upgrade`, committen via een PR, pauze eraf. Tussen de binary en `init --upgrade` falen `lighthouse config`-aanroepen van die repo; daarom staat de tick stil.

### Taak 14: afronden

- [ ] **Stap 1:** `lighthouse evidence`/`/lighthouse-write-pr 588` verifieert de acceptatiecriteria met bewijs per regel; criterium "migratie gedraaid" is pas na taak 13 aantoonbaar, dus de PR is `Refs #588 — <reden>` met een Evidence-tabel, niet `Closes`.
- [ ] **Stap 2:** deel c is #557; laat het issue de kanban-wijzigingen niet bevatten.

## Zelfreview

- **Dekking:** §1 → taken 1-2, 4; §2 → 3, 10; §3 en §4 → 11; §5 → 4, 9; §6 → 5-8, 13; §7 is deel c (buiten dit plan, #557). Doelen "één regel Go en web" → taken 3 en 10 delen `cases.json`.
- **Types:** `Marks`, `LabelEdit{Add, Remove}`, `Result{State, Blocked}`, `issuestate.Keys` en `ReturnLabelKey` hebben overal dezelfde naam; `BlockingKey` verdwijnt in taak 2 en zijn enige gebruiker (`priorities.go`) krijgt in taak 3 `Marks` en in taak 10 `Labels`.
- **Open punt:** in taak 3 gebruikt `priorities.Issue.Label` tijdelijk de eerste sleutel van `Marks`; taak 10 verwijdert het veld.
