# Telemetrie-gegevensmodel Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Migratie `0010_telemetry.sql`, het pakket `internal/telemetry` (koppelregel en vier idempotente schrijffuncties) en trajectkoppeling in `runs.Take`.

**Architecture:** Genormaliseerd model in `lighthouse.db`: `trajectories` (repo + issue) als spil; `runs` en `sessions` verwijzen ernaar; `setups`, `usage` en `findings` hangen aan run of sessie. Elke schrijffunctie draait in één transactie en doet `INSERT … ON CONFLICT DO NOTHING`; alleen `sessions` en `findings` werken bij een tweede aanbieding hun afgeleide velden bij. De koppelregel is een pure functie.

**Tech Stack:** Go, SQLite via `modernc.org/sqlite`, embedded migraties in `internal/store/migrations/`.

**Spec:** [`../specs/2026-11-19-telemetrie-gegevensmodel-design.md`](../specs/2026-11-19-telemetrie-gegevensmodel-design.md)

## Global Constraints

- `[project] code_language = en`: tabel- en kolomnamen, opgeslagen waarden, Go-namen en commentaar in `internal/**` zijn Engels. Artefacten (docs, commitberichten) Nederlands.
- Migratie heet `0010_telemetry.sql`; één transactie; `runs` krijgt nieuwe kolommen met `ALTER TABLE … ADD COLUMN`, nooit een tabelherbouw (`foreign_keys(ON)`).
- `assignment`: `standard`, `shadow:<component>` of `variant:<dimension>=<value>`; check `assignment = 'standard' OR assignment GLOB 'shadow:?*' OR assignment GLOB 'variant:?*=?*'`.
- Elke nieuwe tabel (behalve `trajectories`): `source`, `source_id`, `recorded_at` (RFC 3339 UTC, uit de struct van de aanroeper) en `UNIQUE (source, source_id)`.
- Geen transcriptinhoud of reviewrecordinhoud in de database: alleen pad en sha256 (`review.Sum`).
- Branchpatroon `^issue-(\d+)(-|$)`; standaardbranch komt als parameter.
- Verificatie: `go test -race ./internal/store/... ./internal/runs/... ./internal/telemetry/... ./internal/cli/... ./internal/dispatch/...`; test gate `[test_gate] command` na elke commit; `/lighthouse-go-standard` en `/lighthouse-test-standard` volgen.
- Elke PR krijgt een fragment in `changelog.d/`.

## Review Focus

- Sessie zonder enige transcriptregel en zonder issuenummer: `no_issue_number`, geen paniek, geen traject. Test in Task 3.
- Repo-slug in andere hoofdletters (`corneeldh/Lighthouse`) maakt geen tweede traject. Test in Task 4.
- Geweigerde `RecordFinding` (run van ander issue, ongekoppelde sessie) laat geen traject achter. Test in Task 4.
- `RecordSession` die dezelfde aanbieding herhaalt na een `RecordFinding` op die sessie en het traject ongewijzigd laat: geen fout. Test in Task 4.
- Issuenummer `0` of negatief uit een branch (`issue-0-x`) of van de aanroeper: geen traject met issue 0. Test in Task 3 (parser) en Task 4 (writers weigeren `issue < 1`).

---

## File Structure

- Create `internal/store/migrations/0010_telemetry.sql`: schema en backfill van bestaande runs.
- Modify `internal/store/migrate_test.go`, `internal/cli/tick_test.go`: versieteller 10, tabellenlijst, migratietests.
- Modify `internal/runs/runs.go`, `internal/runs/runs_test.go`: `Take` koppelt traject.
- Create `internal/telemetry/link.go` (+ `link_test.go`): `IssueFromBranch`, `Link`.
- Create `internal/telemetry/record.go` (+ `record_test.go`): schrijffuncties en gedeelde helpers (`resolveRepo`, `ensureTrajectory`).
- Modify `docs/install.md` (sectie *Running runs* krijgt een alinea; `docs/tick.md` stap 4 noemt de tabellen met een verwijzing); create `changelog.d/issue-373-telemetrie-gegevensmodel.md`.

---

### Task 1: Migratie 0010

**Files:**
- Create: `internal/store/migrations/0010_telemetry.sql`
- Modify: `internal/store/migrate_test.go` (`TestMigrateTwice`, `TestMigrationPushedDefault` ongewijzigd op 9; nieuwe tests), `internal/cli/tick_test.go:239`

**Interfaces:**
- Produces: tabellen `trajectories`, `sessions`, `setups`, `usage`, `findings` en kolommen `runs.trajectory_id`, `runs.assignment`, exact zoals in het schema van de spec (sectie *Tabellen*); backfill uit de spec (*Bestaande runs koppelen*). Alle latere tasks lezen deze kolomnamen.

- [ ] **Step 1: Schrijf falende tests in `internal/store/migrate_test.go`**
  - Pas `TestMigrateTwice` aan: `v2 != 10`, tabellenlijst `action_failures, findings, meta, runs, schedule_slots, sessions, setups, tick_state, trajectories, usage`. Pas `internal/cli/tick_test.go:239` aan naar 10.
  - `TestMigrate0010KeepsRuns` (patroon `TestMigrate0008KeepsRuns`, helpers `bareDB`, `embeddedUpTo`, `upTo7` + 0008, 0009): migreer tot 9, voeg een issue-run (`o/r`, issue 7) en een geplande run (`schedule` gezet, `issue` NULL) in, migreer naar 10. Assert: beide runs behouden `id`, `trigger_key`, `session_id`; issue-run heeft `trajectory_id` naar rij met `repo='o/r', issue=7` en `created_at` = `min(started_at)` van die groep; geplande run heeft `trajectory_id IS NULL`; beide `assignment='standard'`; twee runs van hetzelfde issue geven één traject.
  - `TestTelemetryConstraints` (tabelgedreven, `store.OpenShared`-database): `assignment='foo'` geweigerd op `runs` en `sessions`; `shadow:plan_review` en `variant:model=opus` aanvaard; `sessions` met traject én `unlinked_reason`, en met geen van beide, geweigerd; `trajectory_id` zonder `link_basis` geweigerd; `setups` met zowel `run_id` als `session_id` geweigerd, en met geen van beide; `usage` met `cost` zonder `cost_kind` geweigerd; `findings` met `decision='maybe'` geweigerd; `findings` met `run_id` én `session_id` geweigerd.
  - `TestMigrate0010ForeignKeysIntact`: na migratie `PRAGMA foreign_key_check` leeg, en een rij in `runs` met bestaande kindrij (`sessions` onbetrokken: gebruik `setups.run_id`) overleeft de migratie.
  - `TestRebuildRunsWithChildrenCommits` (bewijst het herbouwrecept uit de spec): na migratie 10, met een `setups`-rij en een `usage`-rij die naar een run verwijzen, voer in één transactie uit: `PRAGMA defer_foreign_keys=ON`; kopieer `runs` naar stagingtabel `runs_stage`; `DROP TABLE runs`; maak `runs` opnieuw (kopieer de `CREATE`-tekst uit `sqlite_master` en verruim een `CHECK`); `INSERT … SELECT` terug; `DROP TABLE runs_stage`. Assert: `COMMIT` slaagt, `PRAGMA foreign_key_check` leeg, kindrijen intact.
  - `TestTelemetrySchemaHasNoContentColumns`: `PRAGMA table_info` van `findings` en `sessions` is exact de lijst kolommen uit de spec (geen extra inhoudskolom).

- [ ] **Step 2: Draai ze en zie ze falen**

Run: `go test ./internal/store/ -run 'Migrate|Telemetry|Rebuild' -v`
Expected: FAIL (`0010_telemetry.sql` ontbreekt; versie 9).

- [ ] **Step 3: Schrijf `0010_telemetry.sql`**

Neem het schema uit de spec letterlijk over (kolomnamen en `CHECK`s zijn bindend), vul `/* als op runs */` in met de `assignment`-check van de Global Constraints, en sluit af met de twee backfill-statements uit *Bestaande runs koppelen*. Voeg indexen toe op `runs(trajectory_id)`, `sessions(trajectory_id)`, `findings(trajectory_id)`.

- [ ] **Step 4: Draai tests**

Run: `go test -race ./internal/store/... ./internal/cli/ -run 'Migrat|Telemetry|Rebuild|TickNoRepos' -v`
Expected: PASS (inclusief `TestMigrationPushedDefault` ongewijzigd op 9 en `TestEmbeddedMigrationsNumbered`).

- [ ] **Step 5: Commit**

```bash
git add internal/store internal/cli/tick_test.go
git commit -m "feat(store): migratie 0010 met gegevensmodel voor telemetrie (#373)"
```

---

### Task 2: `runs.Take` koppelt het traject

**Files:**
- Modify: `internal/runs/runs.go` (functie `Take`, na de berekening van `attempt`, vóór de `INSERT INTO runs`)
- Test: `internal/runs/runs_test.go`

**Interfaces:**
- Consumes: tabellen uit Task 1.
- Produces: elke run met `issue` heeft `runs.trajectory_id`; geplande runs niet. `Take`-handtekening ongewijzigd.

- [ ] **Step 1: Falende tests**
  - `TestTakeLinksTrajectory`: twee `Take`s voor `("owner/a", 7)` (`start:7` mode `start`, na `Release` met `handoff` `resume:7` mode `resume`, `Trigger: "handoff"`/`"answer"` volgens bestaande `claim`-helper) geven één rij in `trajectories` met `(owner/a, 7)` en beide runs dezelfde `trajectory_id`; `assignment = 'standard'`.
  - `TestTakeScheduledHasNoTrajectory`: geplande claim (zie `scheduled_test.go`) geeft `trajectory_id IS NULL` en een lege `trajectories`.
  - `TestTakeRefusedLeavesNoTrajectory`: een geweigerde claim (`ErrClaimed`: tweede `Take` voor een issue met een open run, met een andere `TriggerKey`) laat geen extra rij in `trajectories` achter en de bestaande run houdt zijn `trajectory_id`.

- [ ] **Step 2: Draai en zie falen**

Run: `go test ./internal/runs/ -run 'TestTake(Links|Scheduled|Refused)' -v` — Expected: FAIL.

- [ ] **Step 3: Implementeer in `Take`**

Alleen als `!scheduled`: `INSERT INTO trajectories (repo, issue, created_at) VALUES (?, ?, ?) ON CONFLICT (repo, issue) DO NOTHING` met `stamp(c.Now)`, daarna `SELECT id` en geef `trajectory_id` mee in de bestaande run-`INSERT` (kolom erbij; `nil` voor scheduled). Alles in dezelfde transactie, na de `ErrTriggerSpent`-controles zodat een weigering niets achterlaat; een `ErrClaimed` rolt via `Rollback` ook het traject terug.

- [ ] **Step 4: Draai de pakkettests**

Run: `go test -race ./internal/runs/... ./internal/dispatch/... ./internal/cli/...` — Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/runs
git commit -m "feat(runs): Take koppelt elke issue-run aan zijn traject (#373)"
```

---

### Task 3: Koppelregel en branchparser

**Files:**
- Create: `internal/telemetry/link.go`
- Test: `internal/telemetry/link_test.go`

**Interfaces:**
- Produces:
  - `func IssueFromBranch(branch string) (int, bool)`: patroon `^issue-(\d+)(-|$)`; `false` bij geen match of nummer < 1.
  - `type Line struct { Branch string; Issue int }`: één transcriptregel; `Issue` 0 = geen nummer.
  - `type LinkResult struct { Issue int; Basis string; Branch string; Reason string }`: `Issue > 0` ⇒ `Basis` is `branch` of `transcript` en `Reason == ""`; anders `Reason` is `main_only`, `no_issue_branch` of `no_issue_number`.
  - `func Link(defaultBranch string, lines []Line) LinkResult`: de vier stappen van de spec (*De koppelregel*), per issuenummer geteld, gelijkspel wint het laatst voorkomende, `Branch` = branch van het laatste voorkomen van het winnende issue (`''` bij basis `transcript`).

- [ ] **Step 1: Falende tests** (`TestIssueFromBranch`, `TestLink`, tabelgedreven)
  - Parser: `issue-373-telemetrie` → 373; `issue-373` → 373; `issue-373x`, `fix-x`, `main`, `issue-0-x`, `xissue-5-a` → niet gevonden.
  - `Link("main", …)` cases, elk met verwacht `LinkResult`: alleen main zonder nummer → `main_only`; alleen main met `Issue: 373` → `{373, transcript, "", ""}`; 50 main-regels plus 1 regel `issue-5-x` → `{5, branch, "issue-5-x"}`; kale `issue-5`; `issue-5-a` ×2 en `issue-6-b` ×2 (gelijkspel, 6 het laatst) → 6; `issue-5-a` ×3 en `issue-5-b` ×1 samen met `issue-6-c` ×3 → 5 wint op 4 regels, `Branch` is de laatste van issue 5; alleen `fix-x` zonder nummer → `no_issue_branch`; `fix-x` met nummer → `transcript`; geen regels → `no_issue_number`; alleen lege branches → `no_issue_number`; lege branches plus één `main`-regel → `main_only`; transcriptnummers: 5 ×1 en 6 ×2 → 6, gelijkspel → laatst geziene; een main-regel met `Issue: 5` plus een `fix-x`-regel → `transcript`, issue 5; `defaultBranch` `trunk` telt als main.

- [ ] **Step 2: Falen** — Run: `go test ./internal/telemetry/ -v` — Expected: FAIL (pakket ontbreekt).

- [ ] **Step 3: Implementeer** `link.go` zonder bestand-, database- of klokgebruik. Eén telmap per issue met (aantal, positie van laatste voorkomen, laatste branch).

- [ ] **Step 4: Draai** `go test -race ./internal/telemetry/ -v` — Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/telemetry
git commit -m "feat(telemetry): koppelregel voor interactieve sessies (#373)"
```

---

### Task 4: Schrijffuncties

**Files:**
- Create: `internal/telemetry/record.go`
- Test: `internal/telemetry/record_test.go` (gebruikt `store.OpenShared` zoals `runs_test.go`)

**Interfaces:**
- Consumes: `Link`, `Line`, `LinkResult` (Task 3); schema (Task 1); `runs.Take` (Task 2) in de AC-6-test.
- Produces (alle `ctx context.Context, db *sql.DB` als eerste twee parameters; resultaat `(id int64, isNew bool, err error)`):
  - `type Origin struct { Source, SourceID string; RecordedAt time.Time }`
  - `type SessionRecord struct { Origin; Repo, Harness, SessionKey, TranscriptPath, DefaultBranch string; Lines []Line; StartedAt time.Time; EndedAt *time.Time }` en `func RecordSession(ctx, db, SessionRecord) (int64, bool, error)`: roept `Link` aan; `assignment` blijft `standard`.
  - `type SetupRecord struct { Origin; RunID, SessionID int64; Model, Effort, Harness, HarnessVersion, SuperpowersVersion, LighthouseVersion, PackVersion, SkillsHash string }` en `func RecordSetup(ctx, db, SetupRecord)`; precies één van `RunID`/`SessionID` ≠ 0.
  - `type UsageRecord struct { Origin; RunID, SessionID int64; Model string; InputTokens, OutputTokens, CacheReadTokens, CacheWriteTokens int64; Cost *float64; CostKind string }` en `func RecordUsage(ctx, db, UsageRecord)`.
  - `type FindingRecord struct { Origin; Repo string; Issue int; RunID, SessionID int64; Component, FindingKey, Severity, Decision, RecordPath, RecordSum string }` en `func RecordFinding(ctx, db, FindingRecord)`; `Decision == ""` ⇒ NULL.
  - Interne helper `resolveRepo(ctx, tx, repo string) (string, error)`: geeft de reeds opgeslagen schrijfwijze (hoofdletterongevoelig, uit `trajectories` dan `runs`), anders `repo` zelf; `ensureTrajectory(ctx, tx, repo string, issue int, at time.Time) (int64, error)`; weigert `issue < 1`.

- [ ] **Step 1: Falende tests** (benoemd naar de spec-testlijst)
  - `TestOneTrajectoryAcrossSources` (AC 6): `runs.Take` start + resume voor `("o/r", 373)`, `RecordSession` met regels op `issue-373-x` → één `trajectories`-rij; beide runs en de sessie verwijzen ernaar; sessie heeft `link_basis='branch'`, `branch='issue-373-x'`.
  - `TestRecordIdempotent`: elke `Record…` twee keer met dezelfde `(Source, SourceID)` → één rij, tweede `isNew=false`, zelfde id. Tweede bron (andere `Source`, zelfde sessie/run/`(run,model)`) → `isNew=false`, geen fout, één rij.
  - `TestSessionGrows`: eerst alleen `main`-regels → `unlinked_reason='main_only'`, `trajectory_id IS NULL`; dan dezelfde sessie met ook `issue-9-x`-regels en latere `RecordedAt`/`EndedAt` → één rij, traject 9, `link_basis='branch'`, `unlinked_reason IS NULL`; `source`, `source_id`, `started_at`, `transcript_path` ongewijzigd.
  - `TestSessionRepeatSameOffering`: dezelfde aanbieding twee keer geeft een identieke rij (alle kolommen gelijk).
  - `TestSessionUnlinkedStored`: alleen main-regels → rij met reden `main_only`, geen traject (en geen rij in `trajectories`).
  - `TestFindingDecisionLater`: `RecordFinding` zonder besluit (`decision IS NULL`), daarna dezelfde `(Source, SourceID)` met `Decision: "adopted"` en gewijzigde `RecordSum` → één rij, besluit en sum bijgewerkt, `isNew=false`.
  - `TestSessionWithFindingsRefusesRetarget`: sessie gekoppeld aan traject 9 met een bevinding; `RecordSession` met regels die naar issue 10 wijzen → fout, rij ongewijzigd; dezelfde aanbieding als eerder → geen fout.
  - `TestFindingMustFitTrajectory`: run van issue A bij `Issue: B` → fout en `count(*) FROM trajectories` ongewijzigd (ook voor traject B); ongekoppelde sessie → fout, niets geschreven; `Issue: 0` → fout.
  - `TestRepoSlugCaseInsensitive`: bestaand traject `CorneeldH/lighthouse`; `RecordFinding{Repo: "corneeldh/lighthouse", Issue: 1}` → nog één traject, de opgeslagen schrijfwijze blijft.
  - `TestSetupAndUsageOneOwner`: `RunID` én `SessionID` gezet, of geen van beide → fout; `Cost` zonder `CostKind` → fout; `Cost == nil` zonder kind → NULL en OK.

- [ ] **Step 2: Falen** — Run: `go test ./internal/telemetry/ -run 'Record|OneTrajectory|Session|Finding|Repo|Setup' -v` — Expected: FAIL (undefined).

- [ ] **Step 3: Implementeer `record.go`**
  - Elke functie opent `db.BeginTx` (store gebruikt `_txlock=immediate`), rolt terug bij elke fout, committ aan het eind.
  - Nieuw-detectie: `INSERT … ON CONFLICT DO NOTHING` en `RowsAffected()`; bij 0 de bestaande id opzoeken via `(source, source_id)` en anders via het objectsleutel-paar (`(harness, session_key)`, `run_id`, `session_id`, `(run_id, model)`, `(session_id, model)`).
  - `RecordSession`: bij bestaande rij `UPDATE` van `branch`, `trajectory_id`, `link_basis`, `unlinked_reason`, `ended_at`, `recorded_at`; vóór de update bij gewijzigde `trajectory_id` en bestaande `findings.session_id` → fout. Trajectaanmaak alleen als `Link` een issue geeft.
  - `RecordFinding`: traject uit `(repo, issue)`; een meegegeven run of sessie moet `trajectory_id` gelijk hebben, anders fout (ook bij `NULL`); tweede aanbieding werkt `severity`, `decision`, `record_path`, `record_sum`, `recorded_at` bij. Valideer vóór `ensureTrajectory` zodat een weigering niets achterlaat.
  - `RecordSetup`/`RecordUsage`: alleen insert; objectcontrole via de `CHECK`s plus een duidelijke Go-fout vooraf.

- [ ] **Step 4: Draai**

Run: `go test -race ./internal/store/... ./internal/runs/... ./internal/telemetry/... ./internal/cli/... ./internal/dispatch/...` — Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/telemetry
git commit -m "feat(telemetry): idempotente schrijffuncties voor sessie, inzet, verbruik en bevinding (#373)"
```

---

### Task 5: Documentatie en changelog

**Files:**
- Modify: `docs/install.md` (na de alinea's van *Running runs*, voor *Run the tick*), `docs/tick.md` (stap 4 *Database*, één zin met verwijzing)
- Create: `changelog.d/issue-373-telemetrie-gegevensmodel.md`

- [ ] **Step 1: Schrijf de alinea** in `docs/install.md` (Engels, zoals dat document): traject (repo + issue), de vijf nieuwe tabellen en `runs.trajectory_id`/`assignment`, herkomstkolommen en idempotentie, de koppelregel in vier stappen met de drie redenen, en dat geplande runs geen traject hebben. Voeg in `docs/tick.md` (Nederlands) aan stap 4 toe dat de database ook de telemetrie bevat, met een relatieve link naar `install.md`. Alleen relatieve links naar bestaande doelen (`md-link-check`).

- [ ] **Step 2: Fragment**

```markdown
---
type: feature
---
Gegevensmodel voor telemetrie in `lighthouse.db`: trajecten, sessies, inzet, verbruik en bevindingen, met idempotente schrijffuncties en een koppelregel voor interactieve sessies.
```

Controleer het `type`-veld tegen `docs/releasing.md`; kies daar de geldige waarde.

- [ ] **Step 3: Verifieer** — Run: `go test -race ./internal/store/... ./internal/runs/... ./internal/telemetry/... ./internal/cli/... ./internal/dispatch/...` en `go run ./cmd/lighthouse init --upgrade` (geen `pack/`-wijziging: verwacht `No changes.`). Expected: PASS en `No changes.`

- [ ] **Step 4: Commit**

```bash
git add docs/install.md docs/tick.md changelog.d/issue-373-telemetrie-gegevensmodel.md
git commit -m "docs(telemetry): traject, tabellen en koppelregel (#373)"
```

---

## Self-Review

- **Spec-dekking:** migratie en backfill (T1), herbouwrecept-test (T1), `Take` (T2), koppelregel en parser (T3), idempotentie, groeiende sessie, besluit later, bevinding-bij-traject, repo-normalisatie, ongekoppelde sessie, indeling en schema-bewaking (T1/T4), AC 6 (T4), docs en changelog (T5). Dispatcher-runs-niet-in-`sessions` is een T2-zorg van de spec en krijgt hier geen code.
- **Typeconsistentie:** `Line`, `LinkResult`, `Origin` en de vier `…Record`-structs zijn in T3/T4 één keer gedefinieerd; resultaat overal `(int64, bool, error)`.
- **Proportie:** het plan legt namen, handtekeningen en testassertions vast; schemategels en transactiecode blijven aan de bouwer.
