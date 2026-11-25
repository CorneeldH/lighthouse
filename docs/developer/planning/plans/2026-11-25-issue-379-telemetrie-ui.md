# Telemetrie in de UI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Een tabblad `Telemetry` in de lighthouse-UI dat per onderdeel van de catalogus (#388) toont wat het vond, wat werd overgenomen, wat het kostte en hoe trajecten met en zonder het onderdeel afliepen, met tijdlijn, trajectdetail en dekking.

**Architecture:** Twee schrijvers vullen wat de UI nodig heeft (`review_runs` via de review-index, route/gebied/PR-grootte via een kenmerkenpas in `measure-recovery`, migratie `0014`). Een nieuw pakket `internal/telemetry/report` laadt de trajecten uit een readonly-verbinding (`store.OpenReadOnlyLive`) in één dataset en aggregeert in pure Go-functies; huma-endpoints leveren kant-en-klare aggregaten; React tekent en filtert alleen.

**Tech Stack:** Go (`database/sql`, modernc sqlite, huma), React 19, TypeScript, TanStack Query, orval (`npm run gen`), Vitest, Playwright, gewone SVG (geen grafiekbibliotheek in `web/`).

**Spec:** [`2026-11-24-issue-379-telemetrie-ui-design.md`](../specs/2026-11-24-issue-379-telemetrie-ui-design.md); de lijsten *Catalogus*, *Kenmerken* en *Uitkomstmaten* komen uit [`2026-11-24-issue-388-explore-exploit-design.md`](../specs/2026-11-24-issue-388-explore-exploit-design.md).

## Global Constraints

- Code, commentaar, testnamen, bestandsnamen en UI-teksten in het Engels (`code_language = "en"`); spec, plan, commitberichten en documentatie in het Nederlands; `changelog.d/` is Engels (voorbeeld `changelog.d/issue-375-inzet-kosten-tokens.md`).
- Onderdeelnamen exact: `plan_review`, `spec_review`, `lane`, `model`, `harness`, `task_review`, `final_review`, `pr_review`, `test_gate`.
- Plangrootteklassen: klein ≤ 3 taken, middel 4–8, groot ≥ 9 (`plan_tasks`). PR-grootte: `< roborev.Threshold` of `≥ roborev.Threshold` coderegels (`roborev.CountCodeLines`). Routes: `0`, `1`, `2`, `4a`, `4b`, `4c`; gate `invariant` telt als `0`.
- Minimum `n < 5`: waarde grijs met "too few trajectories"; er wordt niets verborgen.
- Een ontbrekende meting is `null` (JSON) met een reden en nooit `0`. Reviewkosten: *not measured — review costs are not recorded*.
- Correlatietekst: *"Observed, not caused: reviews run more often on hard issues."*; assigned: *"Assigned by lighthouse"*. Het label staat in elk vergelijkingsblok.
- Week: ISO, maandag als start, UTC. `markers` is een lege lijst (#380 vult hem).
- De UI schrijft nooit: de telemetrie-endpoints lezen via `store.OpenReadOnlyLive` (`mode=ro`, `query_only(1)`, nooit `immutable`, geen migratie). `store.OpenReadOnly` blijft ongewijzigd.
- Geen transcript- of reviewinhoud in de UI: alleen sleutels, ernst, besluit en `record_path`.
- `web/openapi.json` en `web/src/api/lighthouse.ts` worden meegecommit; `web/dist`/build-uitvoer niet.
- Gate-commando's: draai in een verse worktree eerst `cd web && npm ci`. Gebruik `go run ./cmd/lighthouse imports check` naast `[test_gate] command` voor elke commit (de `lighthouse` op PATH kan `COULD NOT CHECK` geven). Nooit `--no-verify`. Pack-/init-wijzigingen: `go run ./cmd/lighthouse init --upgrade` en commit het resultaat.
- Elk nieuw `internal/`-pakket moet door `imports check` komen: draai het na taak 6.

## Review Focus

- Review zonder vondsten (`finding_count = 0`) telt mee in de noemer van *gevonden* en staat niet als "zonder review" (taak 2, 7).
- Plan van vóór de koppeling van reviewrecords (geen `review_runs`-rij, `ran_at` vóór het cohort): valt buiten de populatie en staat als aantal in de dekking, niet in "zonder" (taak 7).
- Traject met één ongeprijsde `usage`-rij: kosten `null`, niet de te lage som, en niet `0` (taak 7).
- Groep met `n < 5`, lege groep en een onderdeel zonder bron: waarde `null` met reden, de UI toont *not measured* of grijs en nooit `0` (taak 8, 13).
- Kapotte of ontbrekende tabel (ongemigreerde database): dekkingsmelding, geen 500 (taak 11).
- Forgefout of ontbrekend label tijdens de kenmerkenpas: oude waarde blijft, `NULL` staat in de dekking (taak 4, 5).

## Bestanden

- Maak: `internal/store/migrations/0014_review_runs.sql`
- Wijzig: `internal/store/store.go` (`OpenReadOnlyLive`), `internal/store/migrate_test.go`, `internal/store/store_test.go`
- Maak: `internal/telemetry/record_review_run.go` (+ `_test`), `internal/telemetry/traits.go` (+ `_test`)
- Wijzig: `internal/telemetry/record.go` (`recordFindingTx`), `internal/telemetry/tx.go`, `internal/telemetry/gitsource.go`, `internal/review/index.go` (+ `_test`)
- Wijzig: `internal/cli/measure_recovery.go` (+ `_test`)
- Maak: `internal/telemetry/report/{classify,label,load,components,timeline,trajectories,coverage}.go` met tests
- Maak: `internal/server/telemetry.go` (+ `_test`); wijzig `internal/server/server.go`, `internal/cli/serve.go`, `web/openapi.json`
- Maak: `web/src/components/telemetry/*` (+ tests); wijzig `web/src/components/Tabs.tsx`, `web/src/App.tsx`, `web/src/lib/{api,queries}.ts`, `web/src/api/lighthouse.ts` (gegenereerd), `web/e2e/*`
- Maak: `docs/telemetry-ui.md`, `changelog.d/issue-379-telemetrie-ui.md`; wijzig `docs/review-index.md`

---

### Task 1: Migratie `0014` en `store.OpenReadOnlyLive`

**Files:**
- Create: `internal/store/migrations/0014_review_runs.sql`
- Modify: `internal/store/store.go`, `internal/store/migrate_test.go` (de twee `user_version`-asserties van 13 naar 14, regels ~81 en ~1004 hebben een eigen betekenis: pas alleen de eerste aan), `internal/store/store_test.go`

**Interfaces:**
- Produces: tabel `review_runs` exact als in spec §1 (CREATE TABLE + index `review_runs_trajectory`); `ALTER TABLE trajectories ADD COLUMN route TEXT`, `... area TEXT`; `ALTER TABLE recoveries ADD COLUMN code_lines INTEGER`; `func OpenReadOnlyLive(ctx context.Context, path string) (*Store, error)`.

- [ ] **Step 1: Schrijf de falende tests.** In `migrate_test.go`: `TestMigration0014AddsReviewRunsAndTraits` (kolommen `route`, `area`, `code_lines` bestaan; `INSERT` in `review_runs` met `trajectory_id` NULL en zonder `unlinked_reason` faalt op de CHECK; met reden slaagt; tweede rij met dezelfde `(source, source_id)` faalt; `component = 'task_review'` faalt). In `store_test.go`: `TestOpenReadOnlyLiveRefusesWrites` (`INSERT INTO trajectories ...` via `st.DB` geeft een fout) en `TestOpenReadOnlyLiveSeesLaterWrites` (open live; schrijf via een `OpenShared`-verbinding een trajectrij; `SELECT count(*)` via de live-verbinding ziet hem; gebruik een database die in WAL staat door eerst `OpenShared` te openen en open te laten, zodat `-wal` bestaat).
- [ ] **Step 2: Draai** `go test ./internal/store -run 'TestMigration0014|TestOpenReadOnlyLive' -v` — verwacht FAIL (kolom/functie ontbreekt).
- [ ] **Step 3: Schrijf `0014_review_runs.sql`** (spec §1 en §1b letterlijk) en **implementeer `OpenReadOnlyLive`** in `store.go`: DSN `file:<path>?mode=ro&_pragma=busy_timeout(5000)&_pragma=query_only(1)`, via `FileURI`, nooit `immutable`, geen `migrate`, geen `setWAL`; ontbrekend bestand wikkelt `fs.ErrNotExist` zoals `OpenReadOnly`.
- [ ] **Step 4: Draai** `go test ./internal/store` — verwacht PASS (ook de bestaande test op 13→14).
- [ ] **Step 5: Commit** `feat(store): migratie 0014 review_runs en kenmerkkolommen, OpenReadOnlyLive (#379)`.

### Task 2: Schrijvers voor `review_runs` en een transactionele `RecordFinding`

**Files:**
- Create: `internal/telemetry/record_review_run.go`, `internal/telemetry/record_review_run_test.go`
- Modify: `internal/telemetry/record.go` (splits `RecordFinding` in `RecordFinding` + `recordFindingTx(ctx, tx *sql.Tx, f FindingRecord)`), `internal/telemetry/tx.go`

**Interfaces:**
- Produces:
  - `type ReviewRunRecord struct { Origin; Repo string; Issue int /* 0: unlinked */; Component string /* plan_review|spec_review */; RecordPath, RecordSum string; RanAt time.Time; Outcome string; FindingCount int }`
  - `func (t *Tx) RecordReviewRun(ctx context.Context, r ReviewRunRecord) (id int64, isNew bool, err error)`: `Issue == 0` schrijft `trajectory_id = NULL, unlinked_reason = 'no_trajectory'`; een tweede aanbod van dezelfde origin werkt `record_path`, `record_sum`, `ran_at`, `outcome`, `finding_count`, `trajectory_id`, `recorded_at` bij.
  - `func (t *Tx) RecordFinding(ctx context.Context, f FindingRecord) (int64, bool, error)`
  - `func (t *Tx) DeleteChain(ctx context.Context, source, prefix, keep string) (findings, runs int64, err error)`: verwijdert `findings` en `review_runs` van `source` waarvan `source_id` met `prefix` begint en niet met `keep` (zelfde `substr`-vergelijking als `DeleteFindingsUnder`; `keep == ""` verwijdert alles onder `prefix`).
  - `func ReviewRunSum(ctx context.Context, db *sql.DB, source, sourceID string) (sum string, found bool, err error)`.

- [ ] **Step 1: Schrijf de falende tests** (fixture-DB via `openDB(t)`): `TestRecordReviewRunLinked` (rij met `finding_count = 0` bestaat, trajectrij is aangemaakt), `TestRecordReviewRunUnlinked` (`trajectory_id IS NULL`, `unlinked_reason = 'no_trajectory'`), `TestRecordReviewRunUpdatesSameOrigin` (zelfde origin, andere sum: één rij, nieuwe waarden, `isNew == false`), `TestDeleteChainRemovesBothTables` (`keep` blijft staan, `_` in de chainnaam is geen wildcard), `TestTxRollsBackReviewRunAndFindings` (fout na `RecordReviewRun` in `InTransaction` laat geen van beide rijen achter).
- [ ] **Step 2: Draai** `go test ./internal/telemetry -run 'ReviewRun|DeleteChain' -v` — verwacht FAIL.
- [ ] **Step 3: Implementeer** de signaturen hierboven; hergebruik `insertOrFind`, `bySource`, `resolveRepo`, `ensureTrajectory`. `RecordFinding(ctx, db, f)` blijft publiek en roept `inTx` + `recordFindingTx` aan; gedrag ongewijzigd.
- [ ] **Step 4: Draai** `go test ./internal/telemetry` — PASS, inclusief bestaande `RecordFinding`-tests.
- [ ] **Step 5: Commit** `feat(telemetry): review_runs schrijven en bevindingen binnen een transactie (#379)`.

### Task 3: `review.IndexRecords` schrijft `review_runs`, atomair per keten

**Files:**
- Modify: `internal/review/index.go`, `internal/review/index_test.go`, `docs/review-index.md`

**Interfaces:**
- Consumes: taak 2 (`telemetry.InTransaction`, `Tx.RecordReviewRun`, `Tx.RecordFinding`, `Tx.DeleteChain`, `telemetry.ReviewRunSum`).
- Produces: `IndexRecords` ongewijzigd van signatuur; `IndexResult` krijgt de velden `Runs int` (nieuwe of bijgewerkte `review_runs`-rijen). Een record zonder traject geeft een rij met `no_trajectory` én blijft een `Skip` in `IndexResult.Skipped`. `IndexLedger` blijft ongewijzigd en schrijft geen `review_runs`.

- [ ] **Step 1: Schrijf de falende tests** in `index_test.go` (fixtures als de bestaande `TestIndexRecords*`): `TestIndexRecordsWritesRunWithoutFindings` (nul bevindingen: één rij, `finding_count = 0`), `TestIndexRecordsUnlinkedRecordGetsRunAndSkip`, `TestIndexRecordsSameSumChangesNothing`, `TestIndexRecordsNewerAttemptReplacesRun`, `TestIndexRecordsAbortedOutcome` (`outcome = 'aborted'`, `finding_count` klopt), `TestIndexRecordsFailureKeepsOldRows` (breek halverwege af, bijvoorbeeld met een bevinding waarvan `RecordFinding` faalt, en bewijs dat de rijen van de vorige poging nog staan), `TestIndexLedgerWritesNoReviewRun`. Onleesbare JSON en een foute `written_at` blijven `Skip` zonder rij.
- [ ] **Step 2: Draai** `go test ./internal/review -run IndexRecords -v` — verwacht FAIL.
- [ ] **Step 3: Pas `indexChain` aan:** verwijderen van oude rijen, de `review_runs`-rij en alle bevindingen in één `InTransaction`; idempotentie via `ReviewRunSum` (zelfde `record_sum` en alle bevindingen aanwezig: niets schrijven, `Unchanged` ophogen). `outcome` komt uit het JSON-veld `outcome` van het record (zie `rec.Set("outcome", …)` in `internal/review/record.go`); `ran_at` is `written_at`.
- [ ] **Step 4: Draai** `go test ./internal/review ./internal/telemetry` — PASS. Werk `docs/review-index.md` bij (nieuwe tabel, uitvoerregel `runs=<n>` indien toegevoegd).
- [ ] **Step 5: Commit** `feat(review): review-index schrijft review_runs, ook zonder vondsten (#379)`.

### Task 4: Kenmerken per traject: route, gebied, PR-grootte

**Files:**
- Create: `internal/telemetry/traits.go`, `internal/telemetry/traits_test.go`
- Modify: `internal/telemetry/gitsource.go`, `internal/telemetry/gitsource_test.go`

**Interfaces:**
- Produces:
  - `func RouteFromComments(comments []protocol.Comment, before time.Time) (route string, ok bool)`: de `Gate` van het laatste comment met `protocol.ParseTriageMarker` waarvan `CreatedAt` (RFC3339) vóór `before` ligt; `invariant` wordt `"0"`; `ok == false` zonder marker. `before` zero: geen bovengrens.
  - `func AreaFromLabels(labels []string, prefix string) (area string, ok bool)`: precies één label met `prefix` geeft de rest; nul of meer dan één, of lege `prefix`: `ok == false`.
  - `func (g GitSource) CodeLines(ctx context.Context, pr int) (int, error)`: vindt de squashcommit zoals `MergeCommit`, draait `git show --numstat --no-renames -z --format=` en geeft `roborev.CountCodeLines`; `ErrNoMergeCommit` bij geen commit.
  - `type Traits struct { Repo string; Issue int; Route, Area *string; CodeLines map[int]int /* per PR */ }` en `func RecordTraits(ctx context.Context, db *sql.DB, t Traits) error`: zet `route`/`area` alleen als de pointer niet nil is, `code_lines` per `recoveries`-rij alleen als die nog `NULL` is; raakt `status`, items en `merged_at` nooit.
  - `type TraitTarget struct { Repo string; Issue int; PRs []int; NeedsFill bool; InWindow bool; FirstReviewAt *time.Time; LastMergedAt time.Time }` en `func TraitTargets(ctx context.Context, db *sql.DB, repo string, now time.Time, days int) ([]TraitTarget, error)`: elk traject van `repo` met minstens één `recoveries`-rij; `NeedsFill` als `route`, `area` of een `code_lines` van een PR `NULL` is; `InWindow` als een PR binnen `days` mergede; `FirstReviewAt` is de kleinste `review_runs.ran_at` van het traject.

- [ ] **Step 1: Schrijf de falende tests:** `TestRouteFromCommentsLastMarkerBeforeReview` (twee markers, de tweede na `before` telt niet), `TestRouteFromCommentsInvariantIsZero`, `TestRouteFromCommentsNoMarker`, `TestAreaFromLabelsZeroOneTwo`, `TestAreaFromLabelsNoPrefix`, `TestGitSourceCodeLines` (bestaande git-fixturehelper uit `gitsource_test.go`: een squashcommit met 150 toegevoegde coderegels en een `.md`-wijziging die niet telt), `TestRecordTraitsKeepsRecoveryUntouched` (gesloten rij krijgt kenmerken; `status` en items identiek), `TestRecordTraitsDoesNotOverwriteCodeLines`, `TestTraitTargets` (open en gesloten rij, NULL-detectie, `FirstReviewAt`).
- [ ] **Step 2: Draai** `go test ./internal/telemetry -run 'Route|Area|CodeLines|Traits' -v` — verwacht FAIL.
- [ ] **Step 3: Implementeer** de signaturen. `route` vóór de eerste `review_runs`-rij; zonder review vóór `LastMergedAt` (die keuze maakt de aanroeper in taak 5 door `before` te kiezen).
- [ ] **Step 4: Draai** `go test ./internal/telemetry` — PASS.
- [ ] **Step 5: Commit** `feat(telemetry): kenmerken route, gebied en PR-grootte vastleggen (#379)`.

### Task 5: Kenmerkenpas in `measure-recovery`

**Files:**
- Modify: `internal/cli/measure_recovery.go`, `internal/cli/measure_recovery_test.go`

**Interfaces:**
- Consumes: taak 4. `recoveryForge` krijgt `Issue(ctx, number int) (forge.Issue, error)` en `Comments(ctx, number int) ([]protocol.Comment, error)` (beide staan al op `forge.Forge`).
- Produces: een tweede pas in `runMeasureRecovery`, na het schrijven van de herstelrijen: `runTraitPass(ctx, f recoveryForge, src telemetry.GitSource, db *sql.DB, repo repoconfig.Repo, now time.Time, days int, out io.Writer) error`. Per doeltraject (`TraitTargets`) met `NeedsFill || InWindow`: één `Issue` + één `Comments`; route en gebied lezen; `GitSource.CodeLines` per PR zonder `code_lines`; `RecordTraits`. Een forgefout, een ontbrekende marker of label laat de oude waarde staan, schrijft één regel `traits #<issue>: <reason>` naar `out` en gaat door; exitcode ongewijzigd. De herstelpas zelf (overslaan van gesloten rijen) blijft zoals hij is.

- [ ] **Step 1: Schrijf de falende tests** met de bestaande fake-forge in `measure_recovery_test.go`: `TestTraitPassBackfillsClosedRow` (gesloten rij krijgt route, gebied en `code_lines`; `status` blijft), `TestTraitPassForgeErrorKeepsOldValue` (bestaande `route` blijft, regel in de uitvoer, tweede traject wordt wel gedaan), `TestTraitPassRereadsInsideWindow` (nieuw label binnen het venster telt), `TestTraitPassSkipsSettledTrajectory` (alle kenmerken gevuld en geen PR in het venster: geen forgeread).
- [ ] **Step 2: Draai** `go test ./internal/cli -run TraitPass -v` — verwacht FAIL.
- [ ] **Step 3: Implementeer** `runTraitPass` en roep hem aan; `labels.area_prefix` komt uit `repo` (`repoconfig.Repo`; zoek het veld daar op en voeg het toe als `Load` het nog niet geeft). Werk de `Long`-tekst van het commando bij.
- [ ] **Step 4: Draai** `go test ./internal/cli ./internal/telemetry` — PASS.
- [ ] **Step 5: Commit** `feat(cli): measure-recovery legt kenmerken per traject vast (#379)`.

### Task 6: Indelingen en het correlatielabel (pure Go)

**Files:**
- Create: `internal/telemetry/report/classify.go`, `classify_test.go`, `label.go`, `label_test.go`

**Interfaces:**
- Produces (package `report`):
  - `const SizeSmall, SizeMedium, SizeLarge = "small", "medium", "large"`; `func SizeClass(planTasks *int) (string, bool)` (nil: `("", false)`).
  - `const PRBelow, PRAbove = "below", "above"`; `func PRSizeClass(codeLines int) string` (`< roborev.Threshold` → `below`).
  - `func LaneOf(lanes []string) string`: `sdd` als minstens één `sdd`, `native` als alleen `native`, `mixed` bij beide, `""` zonder lane (lege strings in de invoer tellen niet).
  - `func AssignmentOf(assignments []string) string`: `variant:<x>` als alle dezelfde `variant:<x>` dragen, `standard` als geen `variant:` voorkomt (`shadow:` telt als `standard`), anders `mixed`; lege invoer: `standard`.
  - `func DominantModel(byModel map[string]ModelUse) string` met `type ModelUse struct{ Cost *float64; Tokens int64 }`: het model met de meeste kosten; zonder enig model met kosten dat met de meeste tokens; gelijkspel: alfabetisch eerste.
  - `const LabelCorrelation, LabelAssigned = "correlation", "assigned"`; `func ComparisonLabel(groups [][]string) string`; `func LabelNote(label string) string` (de twee zinnen uit Global Constraints).

- [ ] **Step 1: Schrijf de falende tests** (tabelgedreven): grenzen 3/4 en 8/9 van `SizeClass`; `PRSizeClass(99)`, `(100)`; `LaneOf` voor `[sdd]`, `[native]`, `[sdd,native]`, `[]`; `AssignmentOf` met `shadow:x`, twee verschillende varianten, variant plus standaard; `DominantModel` bij kosten en bij alleen tokens; `ComparisonLabel`: `correlation` voor `[["standard"],["standard"]]`, `assigned` voor `[["variant:a=1"],["standard"]]`, `correlation` zodra een groep `mixed` bevat, `correlation` bij twee onzuivere groepen en bij een lege lijst.
- [ ] **Step 2: Draai** `go test ./internal/telemetry/report -v` — verwacht FAIL (pakket ontbreekt).
- [ ] **Step 3: Implementeer** de signaturen. `ComparisonLabel`: `assigned` alleen als elke groep zuiver is (één unieke waarde, niet `mixed`) en minstens één groep een `variant:`-waarde heeft.
- [ ] **Step 4: Draai** `go test ./internal/telemetry/report` en `go run ./cmd/lighthouse imports check` — PASS.
- [ ] **Step 5: Commit** `feat(telemetry): indelingen en correlatielabel (#379)`.

### Task 7: De dataset laden uit de database

**Files:**
- Create: `internal/telemetry/report/load.go`, `load_test.go`, `testdb_test.go` (fixture-bouwer)

**Interfaces:**
- Consumes: taken 1–6; `telemetry.LoadTimeline(ctx, db, trajectoryID, closedAt, now)`.
- Produces:
  - `type Dataset struct { Trajectories []Trajectory; Now time.Time; Missing []string /* tabellen die ontbreken */ }`
  - `type Trajectory struct { ID int64; Repo string; Issue int; PlanTasks *int; Route, Area *string; PRSize string /* "", below, above (grootste PR) */; Lane, Model, Harness, Assignment string; Cost *float64; CostKind string /* "", estimate, quota, actual, mixed */; HasSetup, HasUsage bool; Reviews []Review; Findings []Finding; Recoveries []Recovery; Closed bool; ClosedAt *time.Time; Questions QuestionCounts; Active, HumanWait *time.Duration; Runs, Sessions int }`
  - `type Review struct{ Component string; RanAt time.Time; Outcome string; FindingCount int }`; `type Finding struct{ Component, Key, Severity, Decision, RecordPath string; ReviewRunID int64 }`; `type Recovery struct{ PR int; Status string; MergedAt time.Time; Items []RecoveryItem; CodeLines *int }`; `type QuestionCounts struct{ Dispatcher, Transcript int }`
  - `type Cohorts map[string]map[string]time.Time` (repo → onderdeel → `ran_at` van de eerste gekoppelde `review_runs`-rij van dat onderdeel)
  - `func Load(ctx context.Context, db *sql.DB, now time.Time) (*Dataset, error)`: ontbreekt een tabel (`no such table`), dan `Dataset{Missing: [...]}` zonder fout. `func (d *Dataset) Cohorts() Cohorts`.
  - Afgerond: minstens één `recoveries`-rij en geen rij met `status = 'open'`; `ClosedAt` is de grootste `merged_at`. Kosten: `nil` zonder `usage`-rijen of met een rij met `cost IS NULL`. `Active` en `HumanWait` komen uit `LoadTimeline` (`ByCause[CauseActive]`, `ByCause[CauseHuman]`; een afwezige oorzaak is `nil`, niet nul) en alleen voor afgeronde trajecten met minstens één run of sessie. Vragen uit `trajectory_events` met `kind = 'question'`, gesplitst naar `run_id IS NOT NULL` (dispatcher) en `session_id` (transcript). Open `LoadTimeline` per traject: meet eerst of dat snel genoeg is voor de echte database; zo niet, schrijf een batchvariant in dit bestand.

- [ ] **Step 1: Schrijf de fixture-bouwer** `newFixture(t) (*sql.DB, *fixture)` in `testdb_test.go` met helpers `addTrajectory(repo, issue, opts…)`, `addReview`, `addFinding`, `addRecovery`, `addUsage`, `addSetup`, `addRun`; schrijf daarna de falende tests: `TestLoadCostIsNilForUnpricedRow` (twee rijen, één `cost NULL`: `Cost == nil`), `TestLoadClosedNeedsRecoveryAndNoOpenRow`, `TestLoadClosedAtIsLastMerge`, `TestLoadLaneModelAssignment` (mixed lane; model met de meeste kosten), `TestLoadPRSizeUsesLargestPR`, `TestLoadQuestionsSplit`, `TestLoadMissingTable` (database zonder `review_runs`: `Missing` bevat `review_runs`, geen fout), `TestCohortsPerRepoPerComponent`, `TestLoadNoRunsNoSessionsGivesNilDuration`.
- [ ] **Step 2: Draai** `go test ./internal/telemetry/report -run Load -v` — verwacht FAIL.
- [ ] **Step 3: Implementeer** `Load` met een handvol grote queries (per tabel één, gegroepeerd in Go op `trajectory_id`), geen query per traject behalve `LoadTimeline`.
- [ ] **Step 4: Draai** `go test ./internal/telemetry/report` — PASS.
- [ ] **Step 5: Commit** `feat(telemetry): trajecten laden voor de UI (#379)`.

### Task 8: Aggregatie per onderdeel

**Files:**
- Create: `internal/telemetry/report/components.go`, `components_test.go`

**Interfaces:**
- Produces:
  - `type Filter struct{ Repo, Route, Size, PRSize, Area, Model, Harness, Component string }` en `func (f Filter) Match(t *Trajectory) bool` (leeg veld: geen beperking; `Component` selecteert alleen in de trajectenlijst, taak 9).
  - `type Measure struct { Value *float64 \`json:"value"\`; N int \`json:"n"\`; Reason string \`json:"reason,omitempty"\` }`
  - `type Group struct { Name string; Trajectories int; Reviews int; Found Measure; Findings int; BySeverity map[string]int; Adopted Measure; LeftOpen, Undecided int; Cost Measure; CostKind string; Recovered Measure; RecoveryItems Measure; Questions Measure; Active Measure; HumanWait Measure }` (JSON-tags snake_case; `Active`/`HumanWait` in seconden; mediaan voor kosten, vragen en tijden)
  - `type Block struct { Name, Kind /* toggle|values|unmeasured */, Label, LabelNote, Reason string; Groups []Group; Outside Outside }` en `type Outside struct{ Running, NoRecovery, NoLane, MixedLane, BeforeCohort int }`
  - `type ComponentsReport struct{ Blocks []Block; Options Options }`, `type Options struct{ Repos, Routes, Sizes, PRSizes, Areas, Models, Harnesses []string }` (waarden die in de data voorkomen)
  - `func Components(d *Dataset, f Filter) ComponentsReport`: blokken in vaste volgorde `plan_review`, `spec_review`, `lane`, `model`, `harness`, `task_review`, `final_review`, `pr_review`, `test_gate`. `plan_review`/`spec_review`: populatie en cohort volgens spec §3 (plan met `plan_tasks != nil`, per repo vanaf het cohortmoment; groepen `with`/`without`). `lane`: `sdd` en `native`. `model`/`harness`: een groep per waarde. De rest: `unmeasured` met `Reason` (task_review: de reden uit spec §3; de overige: *no source in this milestone*). `Found` = reviews met ≥ 1 bevinding / reviews (afgebroken tellen mee); `Adopted` = `adopted` / bevindingen met besluit; voor groepen zonder review (en voor `lane`/`model`/`harness`) zijn `Found` en `Adopted` `null` met reden *not applicable*. Een maat met `N == 0` heeft `Value == nil` en een reden. De afloopmaten gaan alleen over afgeronde trajecten; elke maat telt zijn eigen `N`. `Label` en `LabelNote` per blok volgens taak 6 over de `AssignmentOf`-waarden van de groepen. Review-kosten staan niet in `Group`; ze staan in `Block.Reason` van de reviewblokken: *not measured — review costs are not recorded*.

- [ ] **Step 1: Schrijf de falende tests** op de fixture: `TestComponentsPlanReviewSplitsWithAndWithout` (twee grootteklassen via `Filter.Size`), `TestComponentsReviewWithoutFindingsCountsInDenominator` (2 reviews, 1 met vondsten: `Found.Value == 0.5`, `N == 2`), `TestComponentsAbortedReviewCounts`, `TestComponentsAdoptedRatioKeepsLeftOpenSeparate`, `TestComponentsCohortExcludesEarlierPlans` (plan zonder review van vóór het cohort: niet in "without", wel in `Outside.BeforeCohort`), `TestComponentsCostNilWhenUnpriced` (`Cost.Value == nil`, `N` telt de rest), `TestComponentsPerMeasureN` (traject zonder runs telt niet mee voor `Active` maar wel voor `Recovered`), `TestComponentsLabelCorrelationAndAssigned` (fixture met `variant:`-trajecten geeft `assigned`; met een `mixed`-traject `correlation`), `TestComponentsFilterAppliesToEveryBlock`, `TestComponentsUnmeasuredBlocksHaveReason`, `TestComponentsOpenWindowIsOutside` (traject met `status = 'open'` valt buiten afloop en telt in `Outside.Running`), `TestComponentsEmptyGroupHasNullNotZero`.
- [ ] **Step 2: Draai** `go test ./internal/telemetry/report -run Components -v` — verwacht FAIL.
- [ ] **Step 3: Implementeer** `Filter.Match`, hulpfuncties `median([]float64) (float64, bool)` en de aggregatie.
- [ ] **Step 4: Draai** `go test ./internal/telemetry/report` — PASS.
- [ ] **Step 5: Commit** `feat(telemetry): aggregaten per onderdeel (#379)`.

### Task 9: Tijdlijn per week, trajectenlijst en trajectdetail

**Files:**
- Create: `internal/telemetry/report/timeline.go`, `trajectories.go` en tests
- Create (in `load.go` of nieuw `detail.go`): `LoadDetail`

**Interfaces:**
- Produces:
  - `type Week struct { Start string /* "2006-01-02", maandag UTC */; Cost *float64; Questions *int; Closed *int; Recovered *int }` en `type Marker struct{ Week, Kind, Text string }`; `type TimelineReport struct{ Weeks []Week; Markers []Marker }`.
  - `func Timeline(d *Dataset, db *sql.DB, f Filter) (TimelineReport, error)`: kosten per `usage.recorded_at` (weken vóór de eerste rij van de reeks: `nil`; daarna zonder rijen `0`), mensvragen per `trajectory_events.at` van `kind = 'question'`, afgeronde trajecten per week van `ClosedAt` en het aantal daarvan met herstel; `Markers` is `[]Marker{}`. Het filter geldt via de trajecten (`f.Match`). Kosten van een ongeprijsde `usage`-rij tellen niet mee; gebruik daarvoor `cost IS NOT NULL`.
  - `type ListRow struct { ID int64; Repo string; Issue int; Size string; Components []string; Cost *float64; CostKind string; Recovery string /* none_measured|open|recovered|none */; Closed bool }`; `func Trajectories(d *Dataset, f Filter) []ListRow` (`f.Component` selecteert trajecten waar dat onderdeel draaide: een `review_runs`-rij van dat onderdeel, of voor `lane`/`model`/`harness` een bekende waarde); standaard gesorteerd op repo, issue; de UI sorteert verder.
  - `type Detail struct { ID int64; Repo string; Issue int; Runs []RunRow; Sessions []SessionRow; Reviews []ReviewRow /* met Findings: sleutel, ernst, besluit, record_path */; Recoveries []RecoveryRow /* PR, status, items met kind en ref */; Events []EventRow; Questions QuestionCounts; ByCause map[string]*float64 /* seconden */ }` en `func LoadDetail(ctx context.Context, db *sql.DB, id int64, now time.Time) (Detail, bool, error)`; geen tekst uit transcripts of reviews.

- [ ] **Step 1: Schrijf de falende tests:** `TestTimelineWeeksStartOnMondayUTC` (zondag 23:59 en maandag 00:00 vallen in verschillende weken), `TestTimelineLeadingWeeksNullThenZero`, `TestTimelineSkipsUnpricedUsage`, `TestTimelineMarkersEmptyNotNil` (JSON `[]`), `TestTimelineFilterByRepo`, `TestTrajectoriesFilterByComponent`, `TestTrajectoriesRecoveryStates`, `TestLoadDetailListsFindingsWithoutContent` (struct heeft geen tekstvelden; asserteer op velden), `TestLoadDetailQuestionSplit`, `TestLoadDetailUnknownIDNotFound`.
- [ ] **Step 2: Draai** `go test ./internal/telemetry/report -run 'Timeline|Trajectories|Detail' -v` — verwacht FAIL.
- [ ] **Step 3: Implementeer.** Weeksleutel via `t.UTC()` min `(weekday+6)%7` dagen.
- [ ] **Step 4: Draai** `go test ./internal/telemetry/report` — PASS.
- [ ] **Step 5: Commit** `feat(telemetry): tijdlijn, trajectenlijst en detail (#379)`.

### Task 10: Dekking

**Files:**
- Create: `internal/telemetry/report/coverage.go`, `coverage_test.go`

**Interfaces:**
- Produces: `type CoverageLine struct{ Key, Reason string; Count *int; Note string }` (`Count == nil` en `Note` voor *not measured*-regels); `type CoverageReport struct{ Lines []CoverageLine; NotMigrated bool }`; `func Coverage(ctx context.Context, d *Dataset, db *sql.DB, repo string) (CoverageReport, error)`. Regels volgens spec §8, elk met `Key` (stabiele snake_case) en `Reason`: `sessions_unlinked` per `unlinked_reason` (`main_only`, `no_issue_branch`, `no_issue_number`), `review_runs_unlinked`, `before_cohort` per onderdeel, `no_setup`, `no_usage`, `unpriced_usage`, `no_route`, `no_area`, `no_pr_size`, `running`, `no_recovery_measurement`, `no_lane`, `mixed_lane`, `questions_unlinked_or_open`, `task_review` (met het aantal geïndexeerde ledgerbevindingen: `findings` met `component = 'task_review'`), `final_review`, `pr_review`, `test_gate`, `review_costs`, `harness_codex`, `harness_opencode` (de laatste: *not measured until Explore/exploit*). Bij `d.Missing` niet leeg: `NotMigrated = true` en alleen de regel *database not migrated*.

- [ ] **Step 1: Schrijf de falende tests:** `TestCoverageCountsUnlinkedSessionsPerReason`, `TestCoverageUnlinkedReviewRuns`, `TestCoverageBeforeCohortPerComponent`, `TestCoverageUnpricedAndNoUsage`, `TestCoverageMissingTraitsPerKind`, `TestCoverageOutsideOutcomeReasons`, `TestCoverageUnmeasuredLinesHaveNoCount` (`Count == nil`, nooit 0), `TestCoverageNotMigrated`, `TestCoverageTaskReviewShowsIndexedLedgerCount`.
- [ ] **Step 2: Draai** `go test ./internal/telemetry/report -run Coverage -v` — verwacht FAIL.
- [ ] **Step 3: Implementeer.**
- [ ] **Step 4: Draai** `go test ./internal/telemetry/report` — PASS.
- [ ] **Step 5: Commit** `feat(telemetry): dekking per gat met reden (#379)`.

### Task 11: Endpoints, `Deps` en `serve`

**Files:**
- Create: `internal/server/telemetry.go`, `internal/server/telemetry_test.go`
- Modify: `internal/server/server.go`, `internal/server/server_test.go` (`testDeps`), `internal/cli/serve.go`, `web/openapi.json`

**Interfaces:**
- Consumes: taken 8–10.
- Produces: `type TelemetryReader interface { Components(ctx, report.Filter) (report.ComponentsReport, error); Timeline(ctx, report.Filter) (report.TimelineReport, error); Trajectories(ctx, report.Filter) ([]report.ListRow, error); Detail(ctx, id int64) (report.Detail, bool, error); Coverage(ctx, repo string) (report.CoverageReport, error) }`; `Deps.Telemetry TelemetryReader` (nil laat de endpoints weg); `func registerTelemetry(api huma.API, r TelemetryReader)`; `func NewTelemetryReader(db *sql.DB, now func() time.Time) TelemetryReader` in `internal/telemetry/report` (laadt per verzoek een verse `Dataset` via `Load`). Endpoints, `OperationID` `get-telemetry-components`, `get-telemetry-timeline`, `get-telemetry-trajectories`, `get-telemetry-trajectory`, `get-telemetry-coverage`: paden en queryparameters exact als spec §9; nil-slices als `[]`; onbekend traject: 404; leesfout: 500. Bij `Missing` geven `components`, `timeline` en `trajectories` een leeg rapport en `coverage` de melding *database not migrated*. In `serve.go` opent `store.OpenReadOnlyLive(ctx, dbPath)` naast de bestaande store; sluit hem bij het afsluiten; mislukt het openen, dan blijft `Deps.Telemetry` nil en schrijft serve één regel naar `errOut`.

- [ ] **Step 1: Schrijf de falende tests:** `TestTelemetryEndpointsReturnJSONShape` (fake reader; velden snake_case, `[]` voor lege lijsten), `TestTelemetryFiltersAreParsedOnEveryEndpoint` (query `?repo=a&route=2&size=small&pr_size=below&area=x&model=m&harness=h` komt in `Filter` aan; `component` alleen op trajectories), `TestTelemetryTrajectoryNotFound` (404), `TestTelemetryUnmigratedDatabaseIsCoverageNotError` (echte reader op een database zonder `review_runs`: coverage 200 met `not_migrated`, components 200), `TestTelemetryEndpointsAbsentWithoutReader`, en `TestOpenAPISnapshot` na `-update`.
- [ ] **Step 2: Draai** `go test ./internal/server -run Telemetry -v` — verwacht FAIL.
- [ ] **Step 3: Implementeer** de registratie; werk daarna de snapshot bij: `go test ./internal/server -run TestOpenAPISnapshot -update`.
- [ ] **Step 4: Draai** `go test ./internal/server ./internal/cli` — PASS. Voeg in `internal/cli` een test toe dat `serve` zonder bestaande database niet faalt op de readonly-open (de gewone `store.Open` heeft hem dan al gemaakt).
- [ ] **Step 5: Commit** `feat(server): telemetrie-endpoints op een readonly-verbinding (#379)`.

### Task 12: Client, queries en tabblad-routering

**Files:**
- Modify: `web/src/components/Tabs.tsx`, `web/src/components/Tabs.test.tsx`, `web/src/App.tsx`, `web/src/App.test.tsx`, `web/src/lib/api.ts`, `web/src/lib/queries.ts`, `web/src/lib/queries.test.tsx`
- Generated: `web/src/api/lighthouse.ts` (`cd web && npm run gen`)
- Create: `web/src/components/telemetry/TelemetryTab.tsx`

**Interfaces:**
- Produces: `type TabId = 'controller' | 'priorities' | 'telemetry'`; `fromHash` kent `#priorities`, `#telemetry` en `#telemetry/trajectory/<id>` (voor beide laatste: tab `telemetry`); `export function telemetryRoute(): { view: 'overview' } | { view: 'trajectory'; id: number }`; `fetchTelemetryComponents(filter, signal)`, `fetchTelemetryTimeline`, `fetchTelemetryTrajectories`, `fetchTelemetryTrajectory(id)`, `fetchTelemetryCoverage` in `api.ts` (null → `[]` zoals `fetchPriorities`); hooks `useTelemetryComponents(filter)`, `useTelemetryTimeline(filter)`, `useTelemetryTrajectories(filter)`, `useTelemetryTrajectory(id)`, `useTelemetryCoverage(filter)` in `queries.ts` (poll `FORGE_INTERVAL_MS`); `type TelemetryFilter = { repo?: string; route?: string; size?: string; pr_size?: string; area?: string; model?: string; harness?: string; component?: string }`.

- [ ] **Step 1: Draai** `cd web && npm run gen`; verwacht dat `lighthouse.ts` de vijf nieuwe operaties bevat. Schrijf de falende tests: in `Tabs.test.tsx` `#telemetry` en `#telemetry/trajectory/7` openen het tabblad `Telemetry`, een klik zet `#telemetry`; `Tabs` toont drie tabs in de volgorde Controller, Priorities, Telemetry; in `queries.test.tsx` roept `useTelemetryComponents({repo:'a/b', route:'2'})` `/api/telemetry/components?repo=a%2Fb&route=2` aan en laat lege filters weg; `App.test.tsx`: het tabblad `Telemetry` rendert `TelemetryTab`.
- [ ] **Step 2: Draai** `cd web && npx vitest run src/components/Tabs.test.tsx src/lib/queries.test.tsx src/App.test.tsx` — verwacht FAIL.
- [ ] **Step 3: Implementeer** `Tabs.tsx`, `App.tsx` (breedte `max-w-[1180px]` ook voor telemetry), `api.ts`, `queries.ts`; `TelemetryTab.tsx` toont voorlopig een kop `Telemetry` en de vier regio's als lege `<section aria-label=…>` (`Components`, `Timeline`, `Trajectories`, `Coverage`).
- [ ] **Step 4: Draai** de drie testbestanden en `cd web && npm run lint && npm run typecheck` — PASS.
- [ ] **Step 5: Commit** `feat(web): tabblad Telemetry, client en queries (#379)`.

### Task 13: Deel *Components*: vergelijkingsblokken en filters

**Files:**
- Create: `web/src/components/telemetry/{FilterBar,ComponentsView,ComparisonBlock,MeasureCell}.tsx` en tests
- Modify: `web/src/components/telemetry/TelemetryTab.tsx`

**Interfaces:**
- Consumes: taak 12 (`useTelemetryComponents`, `TelemetryFilter`).
- Produces: `FilterBar({ options, value, onChange })` (een `<select>` per kenmerk plus model en harness; filter staat in React-state van `TelemetryTab` en wordt doorgegeven aan alle delen); `MeasureCell({ measure, unit?, format? })`: `value === null` toont de tekst `not measured` met de reden als `title`; `n < 5` toont de waarde met klasse `text-muted-foreground` en de tekst `too few trajectories`; `n` staat in elke cel (`n=12`); `ComparisonBlock({ block })` met het `label` als zichtbare badge en `label_note` als tekst *in* het blok, en `Outside`-aantallen als regel *Outside the comparison: …*; `unmeasured`-blokken tonen alleen de reden.

- [ ] **Step 1: Schrijf de falende tests** (Testing Library): `MeasureCell` toont `not measured` voor `value: null` en nooit `0`; `n=3` krijgt `too few trajectories` en de grijze klasse; `n=5` niet; `ComparisonBlock` toont het label en de zin *Observed, not caused: …* binnen het blok (`within(block).getByText(…)`) voor `correlation` en *Assigned by lighthouse* voor `assigned`; een `unmeasured`-blok toont de reden en geen tabel; `FilterBar` roept `onChange` met `{ route: '2' }` bij het kiezen van route 2.
- [ ] **Step 2: Draai** `cd web && npx vitest run src/components/telemetry` — verwacht FAIL.
- [ ] **Step 3: Implementeer** de componenten; groepen als tabelrijen (kolommen Found, Adopted, Cost, Recovered, Questions, Active, Human wait); kostenkolom toont `cost_kind` en `mixed` als waarschuwing.
- [ ] **Step 4: Draai** `cd web && npx vitest run && npm run lint && npm run typecheck` — PASS.
- [ ] **Step 5: Commit** `feat(web): vergelijkingsblokken per onderdeel met label en n (#379)`.

### Task 14: Delen *Timeline* en *Coverage*

**Files:**
- Create: `web/src/components/telemetry/{TimelineView,CoverageView}.tsx` en tests

**Interfaces:**
- Produces: `TimelineView({ filter })`: gewone SVG, een staaf per week voor kosten en een lijn voor mensvragen; `null` in een week is een gat (geen staaf), `0` is een nulstaaf; `markers` als verticale lijn met tekst; een tabel met dezelfde getallen als `<table class="sr-only">` voor toegankelijkheid en tests. `CoverageView({ filter })`: elke regel `Reason` met het aantal; `count === null` toont *not measured* met de reden; `not_migrated` toont de melding *database not migrated*.

- [ ] **Step 1: Schrijf de falende tests:** weken met `cost: null` geven geen `<rect>`, een week met `cost: 0` wel (met hoogte 0, `data-week` attribuut); een marker tekent een `<line data-marker>`; lege `weeks` toont *No data yet*; `CoverageView` toont `not measured` voor `count: null` en nooit `0`; `not_migrated` toont de melding.
- [ ] **Step 2: Draai** `cd web && npx vitest run src/components/telemetry` — verwacht FAIL.
- [ ] **Step 3: Implementeer** met schaalfuncties in het component zelf (alleen tekenen; geen aggregatie).
- [ ] **Step 4: Draai** vitest, lint en typecheck — PASS.
- [ ] **Step 5: Commit** `feat(web): tijdlijn en dekking in het tabblad Telemetry (#379)`.

### Task 15: Trajectenlijst en doorklikbaar detail

**Files:**
- Create: `web/src/components/telemetry/{TrajectoryList,TrajectoryDetail}.tsx` en tests
- Modify: `web/src/components/telemetry/TelemetryTab.tsx`

**Interfaces:**
- Consumes: `telemetryRoute()`, `useTelemetryTrajectories(filter)`, `useTelemetryTrajectory(id)`.
- Produces: `TrajectoryList({ filter })`: sorteerbaar op repo, issue, size, cost en recovery (klik op de kolomkop), `component`-filter als `<select>`, elke rij een link `href="#telemetry/trajectory/<id>"`; `TrajectoryDetail({ id })` met koppen *Runs*, *Sessions*, *Reviews* (per review de bevindingen: sleutel, ernst, besluit, `record_path` als tekst), *Recovery* (PR, status, items), *Events* (vraag-splitsing dispatcher en transcript) en *Time by cause*; een `404` toont *Trajectory not found*; een knop *Back to overview* zet `#telemetry`. `TelemetryTab` kiest op `telemetryRoute()` en luistert op `hashchange`.

- [ ] **Step 1: Schrijf de falende tests:** sorteren op kosten zet een rij met `cost: null` achteraan en toont hem als *not measured*; rij-link heeft de juiste `href`; detail met drie reviews toont de bevindingen met `record_path` maar bevat geen veld `text`; de hash `#telemetry/trajectory/7` rendert het detail van traject 7; de filters van de lijst staan in de aanroep van `useTelemetryTrajectories`.
- [ ] **Step 2: Draai** `cd web && npx vitest run src/components/telemetry` — verwacht FAIL.
- [ ] **Step 3: Implementeer.**
- [ ] **Step 4: Draai** `cd web && npx vitest run && npm run lint && npm run typecheck && npm run build` — PASS (`web/dist` niet committen).
- [ ] **Step 5: Commit** `feat(web): trajectenlijst en detail met doorklikken (#379)`.

### Task 16: e2e, screenshot, documentatie en bewijs

**Files:**
- Modify: `web/e2e/screenshot.mjs`, `web/e2e/ui.e2e.ts`, `web/e2e/fixtures.ts`, `docs/review-index.md`
- Create: `docs/telemetry-ui.md`, `changelog.d/issue-379-telemetrie-ui.md`

**Interfaces:**
- Produces: `screenshot.mjs` neemt een derde argument `tab` (`controller` (standaard) of `telemetry`) en wacht bij `telemetry` op de regio's `Components`, `Timeline`, `Trajectories` en `Coverage` in plaats van de controllertitels.

- [ ] **Step 1: Schrijf de falende e2e** in `ui.e2e.ts`: `test('telemetry: tab shows components, timeline, trajectories and coverage')` op een fixture-`lighthouse.db` (breid `fixtures.ts` uit zodat de test-serve een gemigreerde database met één traject, één review en één herstel heeft; schrijf hem met de bestaande helper of een kleine Go-fixture), en `test('telemetry: a trajectory row opens its detail and back')`. Beide asserteren het correlatielabel en `not measured`.
- [ ] **Step 2: Draai** `cd web && npm run e2e -- -g telemetry` — verwacht FAIL (fixture/tab ontbreekt).
- [ ] **Step 3: Implementeer** de fixture en de `tab`-parameter van `screenshot.mjs`. Schrijf `docs/telemetry-ui.md` (wat het tabblad toont, de vier delen, hoe `review-index`, `measure-recovery` en `telemetry collect` het vullen, wat *not measured* betekent) en het changelog-fragment (`type: added`, Engels, verwijst naar #379).
- [ ] **Step 4: Draai de hele gate:** `[test_gate] command` via een commit, `go run ./cmd/lighthouse imports check`, `go run ./cmd/lighthouse init --upgrade` (verwacht `No changes.` als er niets onder `pack/` veranderde; zo niet, commit het resultaat), `cd web && npm run e2e`.
- [ ] **Step 5: Bewijs AC 7:** bouw de binary (`go build -o /tmp/lh ./cmd/lighthouse`; plaats hem in de scratchpad), draai op de echte `lighthouse.db` `lighthouse telemetry collect`, `lighthouse review-index` en `lighthouse measure-recovery --repo-dir .`, start `lighthouse serve`, neem `node web/e2e/screenshot.mjs http://127.0.0.1:<port> docs/images/ui-telemetry.png telemetry` en controleer met `curl` dat `/api/telemetry/components` een `label` per blok en `not measured` voor de reviewkosten geeft. Zet de screenshot en de curl-uitvoer in de PR-evidence.
- [ ] **Step 6: Commit** `docs(telemetry): UI-documentatie, e2e en screenshot (#379)`.

## Self-review

- **Spec-dekking:** §1 → taken 1–3; §1b → 4–5; §2 → 1, 11; §3 → 6–8; §4 → 7–8; §5 → 6, 8, 13; §6 → 9, 14; §7 → 9, 15; §8 → 10, 14; §9 → 11–12, 15; Tests en bewijs → per taak en taak 16.
- **Typeconsistentie:** `Filter`, `Measure`, `Dataset`, `Trajectory`, `Detail` komen uit taken 7–9 en worden in taak 11 en de web-taken gebruikt onder dezelfde namen; `TelemetryFilter` (web) spiegelt `Filter` (Go).
- **Open punten voor de bouwer** (spec *Aan de bouwer*): de exacte JSON-vorm volgt uit de structs hierboven; `LoadTimeline` per traject in taak 7 moet op de echte database gemeten worden, met een batchvariant als het te traag is.
