# Menswerk en wachttijd per traject Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Per traject vragen, antwoorden, overnames en doorlooptijd per oorzaak opslaan en afleiden (issue #378), met `lighthouse trajectory <issue>` om het te bekijken.

**Architecture:** Eén nieuwe tabel `trajectory_events` (alleen wat nergens anders staat: Vraag, antwoord, Done met tijdstip) plus een pure functie `telemetry.Timeline` die uit `runs`, `sessions` en die gebeurtenissen één tijdlijn per traject afleidt, met vaste voorrang tussen oorzaken. De dispatcher schrijft de gebeurtenissen in `apply`; `decide` blijft puur.

**Tech Stack:** Go, SQLite (`internal/store/migrations`), cobra.

**Spec:** [2026-11-24-menswerk-wachttijd-design.md](../specs/2026-11-24-menswerk-wachttijd-design.md); bouwt op [2026-11-19-telemetrie-gegevensmodel-design.md](../specs/2026-11-19-telemetrie-gegevensmodel-design.md).

## Global Constraints

- Code, namen en commentaar Engels (`code_language = "en"`); artefacten (commits, comment op #375, docs; het changelog-fragment is Engels per `docs/releasing.md`) Nederlands. Oorzaken in code: `active` (spec: `actief`), `human` (`mens`), `usage`, `ci_review`, `pickup` (`oppakken`), `unexplained` (`onverklaard`); soorten `measured` / `estimate` / `running` (spec: gemeten / schatting / lopend). `active` heeft twee rangen: `active` (run) en `active` (sessie); `Cause` is één waarde `active`, de rang zit in het interval (`rank`).
- Eén opslag: `lighthouse.db`; wat in `runs` of `sessions` staat wordt bij het lezen afgeleid, nooit opnieuw opgeslagen.
- Geen transcript- of commentaartekst in de database: alleen soort, tijdstip en bronsleutel.
- Migratie `0012_trajectory_events.sql`: één transactie, alleen `CREATE TABLE`/`CREATE INDEX`, bestaande tabellen ongewijzigd.
- Schrijvers idempotent via `source`/`source_id`; dispatcher-bron `dispatch`; `source_id` `question:<comment-id>`, `answer:<comment-id>`, `done:<run-id>`.
- Een tijd zonder bron is `unexplained` of een schatting, nooit `0`; een oorzaak zonder interval ontbreekt in `ByCause`.
- Voorrang bij overlap, eerste wint: `active` (run), `human`, `usage`, `ci_review`, `active` (sessie), `pickup`, `unexplained`; een schatting verliest van een gemeten interval van dezelfde oorzaak.
- Een pusher-rij (`Command == "push-workflows"` of `TriggerKey` met prefix `merge:`) is nooit `active`.
- `decide` blijft puur; geen achtergrondwerk; geen `--no-verify`.
- Voor elke commit: `[test_gate] command` uit `lighthouse.toml` en `lighthouse imports check`.

## Review Focus

- Vraagrun die de tick pas na de Vraag sluit: `active` eindigt bij de Vraag, niet bij `ended_at` (Taak 3).
- Tweede antwoord op dezelfde Vraag, of `repair:<id>` als antwoord: telt niet (Taak 2 en 5).
- Open sessie (`ended_at` leeg) naast een lopende run: telt één keer, als `active`, en is `running` (Taak 3).
- Traject zonder runs of sessies, of alleen een sessie vóór de eerste run: lege tijdlijn, geen paniek, geen overname (Taak 3 en 4).
- Tick die een gebeurtenis niet kan schrijven: tick gaat door, geen dubbele rij bij de volgende (Taak 5).

---

### Task 1: Migratie en tabel

**Files:**
- Create: `internal/store/migrations/0012_trajectory_events.sql`
- Modify: `internal/store/migrate_test.go:81-82` en de lijst tabellen daaronder, `internal/cli/tick_test.go:240-241`

**Interfaces:**
- Produces: tabel `trajectory_events` met de kolommen, checks en uniques uit het spec-blok *Tabel* (letterlijk overnemen), plus `CREATE INDEX trajectory_events_trajectory ON trajectory_events(trajectory_id)`.

- [ ] **Step 1: Pas de tests aan (rood):** `user_version` wordt 12 in `migrate_test.go` en `tick_test.go`; voeg `trajectory_events` toe aan de verwachte tabellenlijst. Voeg `TestTrajectoryEventsConstraints` toe in `migrate_test.go`: op een gemigreerde db weigert `INSERT` (a) beide `run_id` en `session_id` leeg, (b) beide gevuld, (c) `kind = 'done'` zonder `run_id`, (d) een tweede `(trajectory_id, kind, ref)`.
- [ ] **Step 2:** Run `go test ./internal/store/... ./internal/cli/ -run 'Migrat|Constraints|Tick' -race`; verwacht FAIL (versie 11).
- [ ] **Step 3: Schrijf `0012_trajectory_events.sql`** met de tabel uit de spec (zonder de Nederlandse commentaren) en de index.
- [ ] **Step 4:** Zelfde run; verwacht PASS. Run `go test ./internal/store/... -race`.
- [ ] **Step 5: Commit** `feat(store): migratie 0012, tabel trajectory_events (#378)`.

### Task 2: `RecordEvent` en `ErrSessionHasEvents`

**Files:**
- Create: `internal/telemetry/record_event.go`, `internal/telemetry/record_event_test.go`
- Modify: `internal/telemetry/record.go` (`checkRetarget`, de foutenlijst), `internal/telemetry/record_test.go`

**Interfaces:**
- Consumes: `Origin`, `inTx`, `insertOrFind`, `bySource`, `resolveRepo`, `findTrajectory`, `nullID`, `stamp`, `ErrTrajectoryMismatch` uit `record.go`.
- Produces:
  ```go
  type EventKind string // EventQuestion = "question", EventAnswer = "answer", EventDone = "done"
  type EventRecord struct {
      Origin
      Repo string; Issue int          // het traject (bestaat al)
      RunID, SessionID int64         // precies één
      Kind EventKind
      Ref  string
      At   time.Time
  }
  func RecordEvent(ctx context.Context, db *sql.DB, e EventRecord) (id int64, isNew bool, err error)
  var ErrSessionHasEvents = errors.New("telemetry: session has events; its trajectory cannot change")
  ```

- [ ] **Step 1: Tests (`record_event_test.go`, rood).** Gebruik `openDB`, `takeRun`, `session` uit `record_test.go`. Namen en asserties:
  - `TestRecordEventIdempotent`: twee keer dezelfde `Origin` → tweede `isNew == false`, zelfde id, `count(trajectory_events) == 1`.
  - `TestRecordEventSecondAnswerIgnored`: antwoord met `source_id` `answer:2` en `Ref` `q1`, daarna `answer:3` met dezelfde `Ref` → id van de eerste terug, `isNew == false`, geen fout, één rij.
  - `TestRecordEventTrajectoryMismatch`: run van issue 7, `Issue: 8` → `errors.Is(err, ErrTrajectoryMismatch)`, `count(trajectories WHERE issue = 8) == 0`, `count(trajectory_events) == 0`.
  - `TestRecordEventValidation`: `Done` met `SessionID` → fout; beide of geen eigenaar → fout; lege `Ref` of nul `At` → fout; onbekende `Kind` → fout.
  - `TestRecordEventStoresUTC`: `At` in een andere zone wordt als RFC 3339 UTC opgeslagen.
  - In `record_test.go`: `TestRecordSessionRefusesRetargetWithEvents`: sessie met een gebeurtenis, daarna `RecordSession` met regels die naar een ander issue linken → `errors.Is(err, ErrSessionHasEvents)`; zonder gebeurtenis lukt het.
- [ ] **Step 2:** Run `go test ./internal/telemetry/ -run 'Event|Retarget' -race`; verwacht FAIL (compilatie).
- [ ] **Step 3: Implementeer `RecordEvent`** volgens het skelet van `RecordFinding`: valideren, `inTx`, repo resolven, traject zoeken (niet maken; ontbreekt het of hangt de run/sessie ergens anders → `ErrTrajectoryMismatch`, hergebruik de aanpak van `checkOwnerFits`), `insertOrFind` met lookups `bySource` en daarna `lookup{"trajectory_id = ? AND kind = ? AND ref = ?", …}`. Zet `ErrSessionHasEvents` in `record.go` en laat `checkRetarget` ook `count(*) FROM trajectory_events WHERE session_id = ?` controleren (na de bevindingen).
- [ ] **Step 4:** Run `go test ./internal/telemetry/... -race`; verwacht PASS.
- [ ] **Step 5: Commit** `feat(telemetry): RecordEvent en ErrSessionHasEvents (#378)`.

### Task 3: `Timeline`, intervallen en voorrang

**Files:**
- Create: `internal/telemetry/timeline.go`, `internal/telemetry/timeline_test.go`

**Interfaces:**
- Consumes: niets uit de database (pure functie).
- Produces:
  ```go
  type Cause string // CauseActive "active", CauseHuman "human", CauseUsage "usage", CauseCIReview "ci_review", CausePickup "pickup", CauseUnexplained "unexplained"
  type Interval struct{ Start, End time.Time }
  type TimelineRun struct {
      ID int64; Trigger, TriggerKey, Command, Outcome, Reason string
      StartedAt time.Time; EndedAt *time.Time
  }
  type TimelineSession struct{ ID int64; StartedAt time.Time; EndedAt *time.Time }
  type TimelineEvent struct {
      Kind EventKind; RunID, SessionID int64; Ref string; At time.Time
      AnswerTo string // alleen bij answer: gelijk aan Ref; voor leesgemak
  }
  type TimelineInput struct {
      Runs []TimelineRun; Sessions []TimelineSession; Events []TimelineEvent
      ClosedAt time.Time // zero: issue niet gesloten
  }
  type Moment struct {
      At time.Time; RunID, SessionID int64
      Answered, Estimate bool   // alleen betekenisvol bij Questions
      Reason string             // alleen bij Takeovers
  }
  type CauseTotal struct {
      Duration time.Duration
      Estimate, Running, InclusiveCIReview bool
  }
  type TrajectoryTimeline struct {
      Window Interval
      Questions, Answers, Takeovers []Moment
      ByCause map[Cause]CauseTotal
  }
  func Timeline(in TimelineInput, now time.Time) TrajectoryTimeline
  ```
  Deze taak levert de sweep en alle oorzaken uit gemeten gegevens, ook de gemeten `human`- (Vraag-event tot antwoord-event) en `pickup`-intervallen (antwoord-event tot `started_at` van de hervatting). Taak 4 voegt alleen toe: de `Questions`-, `Answers`- en `Takeovers`-momenten, de schattingen (`human`/`pickup` zonder opgeslagen events), `usage` en de onbeantwoorde-vraaglogica.

- [ ] **Step 1: Tests (rood).** Eén helper `mins(n)` = `t0 + n minuten`. Per test een `TimelineInput`; assert op `ByCause[c].Duration` en op `sum(ByCause) == Window.End-Window.Start`:
  - `TestTimelineAC4`: startrun 0–30 (uitkomst `question`, `ended_at` 35), Vraag-event `at` 30 (`Ref` `q1`, `RunID` 1), antwoord-event `at` 60 (`Ref` `q1`, `RunID` 2), hervatting (run 2, `TriggerKey` `answer:9`) start 70, Done-event `at` 100, einde 130 → `active` 30+30 = 60, `human` 30, `pickup` 10, `ci_review` 30, geen `unexplained`, som = 130.
  - `TestTimelineQuestionRunEndsAtQuestion`: zelfde, `ended_at` 35 > Vraag 30: `active` 30, `human` begint bij 30 (niet 35); de 5 minuten verschil staan niet dubbel.
  - `TestTimelineDoneBeforeClose`: run 0–100 met Done 40 → `active` 40, `ci_review` 60.
  - `TestTimelinePusherRowIsCIReview`: push-rij (`Command` `push-workflows`) en merge-run (`TriggerKey` `merge:5:abc`) → `ci_review`, `ByCause` heeft geen `active`.
  - `TestTimelineRunWithoutDone`: alleen `active`, `InclusiveCIReview == true`; `ci_review` ontbreekt in `ByCause`.
  - `TestTimelineGapIsUnexplained`: runs 0–10 en 20–30 → `unexplained` 10, `active` 20; leeg venster-gat nooit `active` of 0.
  - `TestTimelineNoCauseMissingFromMap`: zonder `usage`-interval staat `CauseUsage` niet in `ByCause`.
  - `TestTimelineOpenRunRunsToNow`: open run → `Running == true`, venster eindigt op `now`.
  - `TestTimelineSessionOverlap`: gesloten sessie 10–50 over run 0–30 → overlap 10–30 telt één keer als `active`; 30–50 is `active` (sessie) met `Running == false`; som = venster. Tweede subtest met een open sessie (`EndedAt == nil`): `Running == true`, venster eindigt op `now`.
  - `TestTimelineHumanBeatsCIReview`: pusher-rij (`push-workflows`) 0–100 met een gemeten Vraag-event `at` 20 en antwoord-event `at` 60 binnen die rij → `human` 40, `ci_review` 60; met verwisselde rangen zou `ci_review` 100 zijn.
  - `TestTimelineEmpty`: geen runs en geen sessies → `ByCause` leeg, geen paniek.
  - `TestTimelineWindowStartsAtFirstRunOrSession`: tijd vóór de eerste run telt niet.
- [ ] **Step 2:** Run `go test ./internal/telemetry/ -run Timeline -race`; verwacht FAIL (compilatie).
- [ ] **Step 3: Implementeer `Timeline`** als sweep: bouw een lijst `interval{start, end, cause, rank, estimate, running, inclusive}` uit de runs, sessies en gebeurtenissen volgens de intervaltabel van de spec; verzamel alle begin- en eindpunten binnen het venster, sorteer ze, en wijs elk elementair segment toe aan het dekkende interval met de laagste rang (bij gelijke rang wint `estimate == false`); geen dekking → `unexplained`. Rangen: run-active 0, human 1, usage 2, ci_review 3, session-active 4, pickup 5. Open intervallen eindigen op `now`. Het `Moment`-deel en de overnames blijven leeg tot Taak 4.
- [ ] **Step 4:** Zelfde run; verwacht PASS. Mutatiecheck: wissel de rang van `human` en `ci_review` in de code; `TestTimelineHumanBeatsCIReview` moet falen. Zet terug.
- [ ] **Step 5: Commit** `feat(telemetry): Timeline met oorzaken en voorrang (#378)`.

### Task 4: Vragen, antwoorden, overnames, schattingen en onbeantwoorde vragen

**Files:**
- Modify: `internal/telemetry/timeline.go`, `internal/telemetry/timeline_test.go`

**Interfaces:**
- Consumes: de types uit Taak 3.
- Produces: gevulde `Questions`, `Answers`, `Takeovers`, de geschatte `human`- en `pickup`-intervallen en de `usage`-intervallen van de spec (de gemeten `human`/`pickup` komen uit Taak 3).

- [ ] **Step 1: Tests (rood):**
  - `TestTimelineNoStoredEvents`: vraagrun 0–30 (`question`), hervatting (`answer:9`) start 70, geen events → `human` 40 met `Estimate == true`, `pickup` ontbreekt, `Questions[0].Estimate == true`.
  - `TestTimelineAnswerWithoutQuestion`: alleen een antwoord-event → `human` van `ended_at` van de vraagrun tot `at` van het antwoord, `Estimate`; `pickup` loopt van antwoord tot start hervatting.
  - `TestTimelineRepairIsNoAnswer`: run met `TriggerKey` `repair:4` levert geen `Answers`-moment en geen `human`; `pushanswer:5` wel.
  - `TestTimelineRestartKeepsPickupToLastRun`: twee runs met `TriggerKey` `answer:9` (poging 1 `start_failed`, poging 2) → `pickup` loopt tot `started_at` van de laatste.
  - `TestTimelineUsagePause`: run `parked` eindigt 30, hervatting `Trigger` `usage` start 90 → `usage` 60.
  - `TestTimelineUnansweredQuestion`: vier subtests — eindigt bij een volgende run, bij een run met `Outcome` `returned` (op diens `EndedAt`), bij `ClosedAt`, en anders bij `now` met `Running == true` en `Answered == false`.
  - `TestTimelineSessionQuestion`: sessie 0–100 met vraag 20 en antwoord 50 → `human` 30 (geen run); met een run 10–40 eroverheen telt 20–40 als `active` en alleen 40–50 als `human`.
  - `TestTimelineTakeovers`: `returned` + sessie = 1 overname (`Reason` = de returned-`Reason`); twee sessies zonder run ertussen = 1; sessie vóór de eerste run = 0; `returned` zonder sessie = 1 met eigen reden.
- [ ] **Step 2:** Run `go test ./internal/telemetry/ -run Timeline -race`; verwacht FAIL.
- [ ] **Step 3: Implementeer** in `timeline.go`: koppel vraag en antwoord via `Ref`; een antwoord-run herken je aan `TriggerKey` met prefix `answer:` of `pushanswer:` (nooit `repair:`); schat `human` volgens de tabel; bepaal het einde van een onbeantwoorde vraag zoals in de spec (eerste van volgende run/sessie-start, `returned`-run, `ClosedAt`, anders `now` en `Running`); vul `Takeovers` in tijdsvolgorde met één lopende vlag "dispatcher-run sinds de laatste overname".
- [ ] **Step 4:** Run `go test ./internal/telemetry/... -race`; verwacht PASS.
- [ ] **Step 5: Commit** `feat(telemetry): vragen, antwoorden en overnames in de tijdlijn (#378)`.

### Task 5: `LoadTimeline`

**Files:**
- Create: `internal/telemetry/timeline_load.go`, `internal/telemetry/timeline_load_test.go`

**Interfaces:**
- Consumes: `Timeline`, `TimelineInput` uit Taak 3 en 4.
- Produces: `func LoadTimeline(ctx context.Context, db *sql.DB, trajectoryID int64, closedAt, now time.Time) (TrajectoryTimeline, error)` en `func FindTrajectoryID(ctx context.Context, db *sql.DB, repo string, issue int) (int64, bool, error)`, die de repo hoofdletterongevoelig vergelijkt (zoals `resolveRepo` in `record.go`, `COLLATE NOCASE`).

- [ ] **Step 1: Test (rood)** `TestLoadTimelineFromDB`: bouw met `takeRun`, `runs.Release`, `RecordSession` en `RecordEvent` een traject als in `TestTimelineAC4`; `LoadTimeline` geeft dezelfde `ByCause` als de pure functie op handgebouwde invoer. Tweede test `TestLoadTimelineExcludesOtherTrajectory`: een sessie en een run van een ander traject tellen niet mee (een sessie met `unlinked_reason` heeft altijd `trajectory_id` NULL door een CHECK in `0010_telemetry.sql`, dus daar test je niets mee). `takeRun` zet `Now` op `t0` en `Trigger` op `handoff`: voor runs op andere tijden of met trigger `answer:<id>` roep je `runs.Take` direct aan met expliciete waarden. Derde `TestFindTrajectoryIDMissing`: onbekend issue → `false`, geen fout.
- [ ] **Step 2:** Run `go test ./internal/telemetry/ -run 'Load|FindTrajectory' -race`; verwacht FAIL.
- [ ] **Step 3: Implementeer** drie `SELECT`s (runs, sessions, trajectory_events van `trajectory_id`), vul `TimelineInput`, roep `Timeline` aan. `Pusher`-herkenning zit in `Timeline` (Taak 3), de loader geeft `command` en `trigger_key` ongewijzigd door; tijden parsen als RFC 3339.
- [ ] **Step 4:** Run `go test ./internal/telemetry/... -race`; verwacht PASS.
- [ ] **Step 5: Commit** `feat(telemetry): LoadTimeline leest de tijdlijn uit lighthouse.db (#378)`.

### Task 6: De dispatcher schrijft Vraag, antwoord en Done

**Files:**
- Modify: `internal/dispatch/decide.go` (`Action`: `EventAt time.Time` en `EventRef string`, beide `omitzero` (zoals `view.go` en `fingerprint.go`); `settle`; `answer`; `pushAnswer`; een nieuwe `KindNoteDone`), `internal/dispatch/apply.go`
- Create: `internal/dispatch/apply_events.go`, `internal/dispatch/apply_events_test.go`
- Test: `internal/dispatch/decide_test.go`, `internal/dispatch/apply_test.go`

**Interfaces:**
- Consumes: `telemetry.RecordEvent`, `telemetry.EventRecord` (Taak 2); `parsed.Comment` en `parsed.Time` in `decide.go`.
- Produces: `Action.EventRef` (comment-id van de Vraag, als tekst) en `Action.EventAt` (`created_at` van het commentaar) op de `KindClose` met `Outcome == runs.Question` en op `KindStart`/`KindResume`/`KindPush` van een `answer:`- of `pushanswer:`-trigger (dan `EventRef` = id van de Vraag, `CommentID` = antwoord-id, `EventAt` = `created_at` van het antwoord); `KindNoteDone` = een niet-sluitende actie voor een open agent-run (`!isPush(row)`) waarvoor `c.done()` waar is (een pusher-rij draait geen sessie en krijgt nooit een Done-event), met `RunID`.
  - `func (ap applier) recordEvent(ctx context.Context, a Action, kind telemetry.EventKind, runID int64, sourceID, ref string, at time.Time)` — schrijft, logt een fout en geeft niets terug (de tick gaat door).

- [ ] **Step 1: Tests (rood).**
  - `decide_test.go`: `TestDecideQuestionCloseCarriesQuestionComment` — snapshot met een Vraag van de run: de `KindClose` heeft `EventRef` = Vraag-id en `EventAt` = `created_at` van het commentaar; `TestDecideAnswerCarriesEventFields` — resume met `answer:<id>` en push met `pushanswer:<id>` dragen `EventRef` van de Vraag; `TestDecideRepairCarriesNoEventRef`; `TestDecideNoNoteDoneForPushRow` (een open `push-workflows`- of `merge:`-rij geeft nooit `KindNoteDone`); `TestDecideAnswerToPusherQuestionCarriesEventFields` (`KindPushRecover` draagt `EventRef`/`EventAt`); `TestDecideNoteDoneForOpenDoneRun` — een open run met stand Done en geen werk na de Stop geeft `KindNoteDone`, een run zonder Done niet, en een run die al sluit (Done-commentaar) geeft er geen.
  - `apply_events_test.go`: `TestApplyWritesQuestionEvent` (`KindClose`/`Question` → één rij `kind='question'`, `source_id` `question:<id>`, `at` = `EventAt`, `run_id` = de gesloten run, geschreven nadat `runs.Release` slaagde); `TestApplyWritesAnswerEvent` (na `runs.Take` met de nieuwe run als `run_id`, `source_id` `answer:<antwoord-id>`, `ref` = Vraag-id; een herstart met dezelfde `trigger_key` schrijft niets bij); `TestApplyWritesDoneEventOnce` (`KindNoteDone` twee keer → één rij, `at` van de eerste tick, `source_id` `done:<run-id>`, `ref` = run-id); `TestApplyDoneWriteFailureNotRetried` (`KindNoteDone` met falende `RecordEvent` in twee afzonderlijke `Apply`-aanroepen met dezelfde `StateDir` → één poging, één logregel); `TestApplyWritesAnswerEventAfterPushRecover` (`KindPushRecover` met `pushanswer:`-trigger → antwoord-rij); `TestApplyContinuesWhenEventWriteFails` (een run waarvan `trajectory_id` NULL is, zodat `RecordEvent` weigert: `Apply` geeft `nil` terug, de run is gesloten en het log bevat `event not recorded`).
- [ ] **Step 2:** Run `go test ./internal/dispatch/ -run 'Event|NoteDone|CarriesQuestion|CarriesEvent' -race`; verwacht FAIL.
- [ ] **Step 3: Implementeer.** In `settle` zet je op `closed(runs.Question)` de velden uit de eerste matchende Vraag-`parsed` van deze run (`Comment.ID`, `Time`). In `answer()` en `pushAnswer()` zet je `EventRef` = `q.Comment.ID` en `EventAt` = `parseTime(ans.CreatedAt)`; `repair` blijft zonder. Voeg `KindNoteDone` toe (groep 0 in `planned.group`, mag een gepauzeerde rem passeren, start niets) en emit hem in `decideIssue` voor elke open run van het issue waarvoor `runCtx.done()` waar is en die niet sluit. In `apply`: `KindClose` met `Outcome == runs.Question` roept na `closeRun` `recordEvent(… EventQuestion …)` aan; `start`, `push` en `pushRecover` (de `KindPushRecover` met trigger `pushanswer:`) roepen `recordEvent(… EventAnswer …)` aan direct na een geslaagde `runs.Take` voor een `answer:`- of `pushanswer:`-trigger met `EventRef != ""`; `KindNoteDone` maakt vóór de poging het markerbestand `<env.StateDir>/events/done-<run-id>.tried` aan (`Apply` bouwt de applier per aanroep, dus geheugen overleeft de tick niet) en slaat de actie over als dat bestand al bestaat, zodat ook een mislukte Done-opslag niet opnieuw wordt geprobeerd; hij roept `recordEvent(… EventDone, a.RunID, "done:<id>", "<id>", env.Now())` aan. Het traject volgt uit de run (`runs.trajectory_id`): lees `repo` en `issue` uit de actie. Een fout van `RecordEvent` gaat naar `ap.logf` met de tekst `event not recorded`, nooit als returnwaarde. Werk `internal/cli/tick.go` bij als die op het type actie filtert (zoals bij `KindIdle`) zodat `KindNoteDone` doorgaat naar `Apply`.
- [ ] **Step 4:** Werk de bestaande verwachtingen bij die door `EventRef`/`EventAt`/`KindNoteDone` veranderen: de golden-fixtures `settle-question`, `closecheck-question-closes-as-question`, `answer-resume`, `dedup-one-start-failed-resume` (want krijgt `EventRef`/`EventAt`) en de fixtures met een open Done-run (`done-pr-pending`, `postfailed-repost-question`, `postfailed-repost-handoff`: want krijgt een `note_done`), en de `TestMergeReady`-gevallen in `decide_merge_test.go` (laat `summary()` `note_done` weglaten zodat de verwachte `[]string{}` blijft), en de exacte `Action`-vergelijkingen op `pushanswer:`/`answer:`-triggers: `TestDecidePushAnswerFields` (`decide_test.go:522`) en de tabeltest rond `decide_test.go:1118` krijgen `EventRef` = Vraag-id en `EventAt` = `created_at` van het antwoord; zoek met `grep -n 'pushanswer:\|answer:' internal/dispatch/*_test.go` de overige. Run `go test ./internal/dispatch/... ./internal/cli/... -race`; verwacht PASS. Run `go vet ./...`.
- [ ] **Step 5: Commit** `feat(dispatch): Vraag, antwoord en Done als gebeurtenis opslaan (#378)`.

### Task 7: `lighthouse trajectory`

**Files:**
- Create: `internal/cli/trajectory.go`, `internal/cli/trajectory_test.go`
- Modify: `internal/cli/root.go:110` (registreren)

**Interfaces:**
- Consumes: `telemetry.FindTrajectoryID`, `telemetry.LoadTimeline` (Taak 5); `forge.Forge.Issue` voor `ClosedAt`; `newForgeOpener`, `repoconfig.Load`, `store.OpenShared`, `usageErr`, `exitError` zoals in `internal/cli/measure_recovery.go`.
- Produces: `func newTrajectoryCommand(env trajectoryEnv) *cobra.Command` met `Use: "trajectory <issue> [--repo-dir <path>] [--db <path>]"`; `trajectoryEnv{Getenv, Now, GhToken, Open}` naar het voorbeeld van `measureRecoveryEnv`. Exit 0 geprint, 1 config/forge/db-fout, 2 verkeerd gebruik (geen of ongeldig issuenummer, extra argumenten); onbekend traject: exit 1 met `no trajectory for issue N`.

- [ ] **Step 1: Tests (rood)** met een nep-forge (`Open`-hook) en een tijdelijke db: `TestTrajectoryPrintsTimeline` (de uitvoer bevat per vraag, antwoord en overname het tijdstip, en per oorzaak een regel `<cause> <duur> <measured|estimate|running>`; `unexplained` alleen als het niet leeg is; de oorzaken staan in de vaste voorrangsvolgorde); `TestTrajectoryUsage` (zonder argument en met `abc` → exit 2); `TestTrajectoryUnknownIssue` (exit 1); `TestTrajectoryUsesForgeClosedAt` (een gesloten issue met `ClosedAt` begrenst een onbeantwoorde vraag); `TestTrajectoryForgeErrorIsExit1`.
- [ ] **Step 2:** Run `go test ./internal/cli/ -run Trajectory -race`; verwacht FAIL.
- [ ] **Step 3: Implementeer** het commando zoals `runMeasureRecovery`: `repoconfig.Load(dir)` (`--repo-dir` default `.`), db openen, traject zoeken, `forge.Issue` voor `ClosedAt` (een forge-fout is exit 1), `LoadTimeline`, vaste tekstuitvoer. Registreer in `root.go` naast `measure-recovery`.
- [ ] **Step 4:** Run `go test ./internal/cli/... -race`; verwacht PASS.
- [ ] **Step 5: Commit** `feat(cli): lighthouse trajectory toont menswerk en wachttijd (#378)`.

### Task 8: Documentatie, changelog en #375

**Files:**
- Modify: `docs/install.md` (bij `measure-recovery`, rond regel 418)
- Create: `changelog.d/issue-378-menswerk-wachttijd.md` (zelfde frontmatter als de andere fragmenten; `type` zoals daar voor een nieuwe functie gebruikelijk is, kies uit de bestaande fragmenten en `docs/releasing.md`)

- [ ] **Step 1:** Beschrijf `lighthouse trajectory <issue> [--repo-dir <pad>] [--db <pad>]` in `docs/install.md` (wat het print, de exitcodes) en schrijf het changelog-fragment in het Engels (`docs/releasing.md`).
- [ ] **Step 2:** Run `lighthouse imports check`, `go run ./cmd/lighthouse init --upgrade` (verwacht `No changes.`; zo niet, commit de installatiekopie mee) en `[test_gate] command`.
- [ ] **Step 3: Commit** `docs(telemetry): lighthouse trajectory en changelog (#378)`.
- [ ] **Step 4:** Plaats een comment op #375 (Nederlands): de lezer schrijft vragen en antwoorden uit interactieve sessies via `telemetry.RecordEvent` (`EventRecord` met `SessionID`, `Ref` `<sessie-id>:<tool-use-id>`), met eigen `source`/`source_id`; geen tekst opslaan; `RecordSession` weigert een nieuw traject voor een sessie met gebeurtenissen; en een sessie moet een `ended_at` krijgen bij het afsluiten. Gebruik `gh issue comment 375 --body-file <bestand in de scratchpad>`.

## Self-Review

- **Spec-dekking:** tabel en migratie (T1), `RecordEvent`, idempotentie, tweede antwoord, mismatch, `ErrSessionHasEvents` (T2), tijdlijn, venster, intervaltabel, voorrang, pusher-rij, Done-splitsing (T3), schattingen, onbeantwoorde vraag, usage, overnames, `repair` vs `pushanswer` (T4), laden (T5), dispatcher-bron Vraag/antwoord/Done met mislukt schrijven (T6), `lighthouse trajectory` (T7), docs, changelog, comment #375 (T8). Migratie- en tick-tests: T1.
- **Typeconsistentie:** `EventKind`/`EventRecord` (T2) worden in T6 en T5 gebruikt; `TimelineInput`, `Moment`, `CauseTotal`, `Cause` (T3) in T4, T5, T7; `FindTrajectoryID` en `LoadTimeline` (T5) in T7.
- **Open voor de bouwer (spec, *Aan de bouwer*):** de precieze plek in `decideIssue` voor `KindNoteDone` en de vorm van `Moment`/`Interval`/`CauseTotal` zijn hierboven vastgelegd; wijkt de code af, dan wint de test en past de bouwer dit plan aan.
