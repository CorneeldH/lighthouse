# Versiewissel Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Uit `setups` een wissellijst per dimensie afleiden (`VersionChanges`, `VersionMarkers`) en vanuit de tick per laatste wissel precies één voorstel-issue schrijven.

**Architecture:** Een pure leesfunctie in `internal/telemetry` (alleen `SELECT`, geen opslag) levert de wissels en de markeringen voor #379. Een tickstap na `collectRuns` in `internal/cli` draait op een uurpoort, zoekt met `forge.FindByMarker` de volledige verborgen marker en maakt anders één issue in `[telemetry] proposals_repo`.

**Tech Stack:** Go, SQLite (`lighthouse.db`, tabel `setups`), `internal/forge`, `internal/pins` (`ParseVersion`), BurntSushi/toml.

**Spec:** [`docs/developer/planning/specs/2026-11-24-issue-380-versiewissel-design.md`](../specs/2026-11-24-issue-380-versiewissel-design.md)

## Global Constraints

- Geen tweede opslag: wissels worden afgeleid uit `setups`, geen tabel, geen migratie. Het tijdsbestand `<state>/telemetry/last-version-check` houdt alleen een tijdstip bij.
- Volgorde op `started_at` van run of sessie (niet `setups.recorded_at`); gelijke tijd beslist `setups.id` oplopend.
- Alleen `runs.assignment = 'standard'` en `sessions.assignment = 'standard'` tellen mee.
- `NULL` in de versiekolom wordt overgeslagen; de eerste waarde van een reeks is geen wissel; A→B→A→B levert één wissel.
- Reeksen: `superpowers` één reeks (runs en sessies); `model` per `setups.lane` en alleen dispatcher-runs (`run_id` gezet); `harness` per `harness` (runs en sessies).
- Marker: `<!-- lighthouse-version-change: <dimensie> [<harness|lane> ]→<nieuwe waarde> -->`, zonder oude waarde; zoek altijd de volledige comment tot en met ` -->`.
- Koppeling dimensie→onderdelen: `superpowers` → `task_review`, `final_review`; `model` → `model`; `harness` → `harness`.
- Hooguit eens per uur (`collectEvery`-patroon), tijdsbestand ook na een mislukte poging geschreven.
- Een fout in de stap houdt de tick niet tegen; ontbrekende `proposals_repo` is geen mislukking (één logregel).
- Artefacten (issuetekst, commits, docs) in het Nederlands; code, namen en commentaar in het Engels (`code_language`). Issue-inhoud in `[project] language` van de doelrepo (`nl`/`en`).
- Vóór elke commit: `[test_gate] command` uit `lighthouse.toml` en `lighthouse imports check`. Nooit `--no-verify`.

## Review Focus

- Twee rijen met gelijke `started_at`: *oud* en nieuw moeten deterministisch zijn (op `setups.id`). Test in Task 1.
- Een nooit eerder geziene oudere versie (`nieuw→ouder`): wissel in de lijst, geen voorstel; niet-semver telt als gewone wissel. Test in Task 2.
- Versie `7.0.2` en reeds bestaand issue met `→7.0.20`: mag `→7.0.2` niet als gevonden zien. Test in Task 3.
- Forge-storing: tijdsbestand wordt toch geschreven, de tick gaat door en meldt de stap als mislukt. Test in Task 3.
- Database zonder `setups` (ongemigreerd): herkenbare fout `ErrNoSetups`, geen paniek. Test in Task 1.

---

## File Structure

- Create `internal/telemetry/versions.go` — `VersionChange`, `VersionChanges`, `VersionMarker`, `VersionMarkers`, `ErrNoSetups`, `ComponentsFor`.
- Create `internal/telemetry/versions_test.go` — echte `lighthouse.db` in tempdir (naar `record_test.go`).
- Modify `internal/hostconfig/hostconfig.go` — `LoadTelemetry` (`[telemetry] proposals_repo`).
- Create `internal/cli/tick_versions.go` — `proposeVersionChanges`, marker, tekst, uurpoort.
- Create `internal/cli/tick_versions_test.go` — nep-forge (naar `internal/pins/lane/*_test.go`).
- Modify `internal/cli/tick.go` — aanroep na `collectRuns`.
- Create `changelog.d/issue-380-versiewissel.md`, modify `docs/tick.md`.

---

### Task 1: De wissellijst (`VersionChanges`)

**Files:**
- Create: `internal/telemetry/versions.go`
- Test: `internal/telemetry/versions_test.go`

**Interfaces:**
- Produces:
  ```go
  type Dimension string // "superpowers", "model", "harness"
  const (DimSuperpowers Dimension = "superpowers"; DimModel Dimension = "model"; DimHarness Dimension = "harness")
  type VersionChange struct {
      Dimension Dimension
      Series    string    // "" for superpowers; harness name; lane ("" = no lane → "-" in text only)
      Old, New  string
      RunID     sql.NullInt64; SessionID sql.NullInt64 // first run or session with New
      At        time.Time // started_at of that run or session
      Skip      bool      // true: older-than-seen version, never proposed (Task 2 sets it from the same pass)
  }
  var ErrNoSetups = errors.New("telemetry: setups table missing")
  func VersionChanges(ctx context.Context, db *sql.DB) ([]VersionChange, error) // sorted by At, then series
  ```

- [ ] **Step 1: Write failing tests** in `versions_test.go`. Helper `insertSetup(t, db, row)` inserts into `runs`/`sessions` (with `started_at`, `assignment`) and `setups`. Tests:
  - `TestVersionChanges_TwoRunsOneChange`: runs superpowers `7.0.1`, `7.0.2` → exactly one change `Old:"7.0.1" New:"7.0.2"`, `RunID` of the second.
  - `TestVersionChanges_ABABIsOneChange`: A,B,A,B → one change A→B.
  - `TestVersionChanges_FirstValueNoChange`, `TestVersionChanges_NullSkipped` (A, NULL, A → none).
  - `TestVersionChanges_OrdersByStartedAtNotRecordedAt`: `recorded_at` reversed → *old/new* still follow `started_at`.
  - `TestVersionChanges_EqualStartedAtOrdersBySetupsID`.
  - `TestVersionChanges_ModelPerLaneDispatcherOnly`: `sdd` rows (`''`/opus) and `native` rows (sonnet) interleaved → no change; a session row with another model → ignored; a run in lane `native` going `sonnet-a`→`sonnet-b` → one change with `Series:"native"`.
  - `TestVersionChanges_HarnessPerHarness`: `claude 2.1` and `codex 0.6` rows → no change; `claude 2.1`→`2.2` → one, `Series:"claude"`.
  - `TestVersionChanges_OnlyStandardAssignment`: `shadow:x` run and `variant:a=b` session with a new version → none.
  - `TestVersionChanges_NoSetupsTable`: DB without `setups` → `errors.Is(err, ErrNoSetups)`.
- [ ] **Step 2: Run** `go test ./internal/telemetry -run TestVersionChanges -v` — Expected: FAIL (undefined).
- [ ] **Step 3: Implement `VersionChanges`** in `versions.go`. One `SELECT` per dimension joining `setups` with `runs` (`s.run_id = r.id`) and `sessions`, `UNION ALL`, filtered on `assignment = 'standard'` and non-NULL column, `ORDER BY started_at, s.id`; walk rows per series keeping the set of seen values and the last seen value. Parse `started_at` as `time.RFC3339Nano`. Map "no such table: setups" to `ErrNoSetups` (`strings.Contains` on the sqlite error). Only `SELECT`.
- [ ] **Step 4: Run** the same command — Expected: PASS.
- [ ] **Step 5: Commit** `feat(telemetry): VersionChanges leidt versiewissels af uit setups (#380)`.

### Task 2: Markeringen, voorstel-filter en onderdelen

**Files:**
- Modify: `internal/telemetry/versions.go`
- Test: `internal/telemetry/versions_test.go`

**Interfaces:**
- Consumes: `VersionChange` from Task 1.
- Produces:
  ```go
  type VersionMarker struct{ Week time.Time; Kind Dimension; Text string } // Week = Monday 00:00 UTC (ISO week)
  func VersionMarkers(ctx context.Context, db *sql.DB) ([]VersionMarker, error)
  func ComponentsFor(d Dimension) []string                 // task_review,final_review | model | harness
  func LatestProposals(changes []VersionChange) []VersionChange // last change per (dimension, series), Skip=false, ComponentsFor non-empty
  ```
  `VersionChanges` sets `Skip` when `Dimension` is `superpowers` or `harness`, both versions parse with `pins.ParseVersion`, and `New` is lower than the highest value seen earlier in the series; `model` never skips; unparseable never skips.

- [ ] **Step 1: Write failing tests:**
  - `TestVersionMarkers_WeekKindText`: change at `2026-11-25` (Wed) → `Week` Monday `2026-11-23`, `Kind:"superpowers"`, `Text:"superpowers 7.0.1→7.0.2"`; harness text `"harness claude 2.1→2.2"`; model text `"model native sonnet-a→sonnet-b"`.
  - `TestVersionChanges_OlderNeverSeenIsSkip`: `7.0.2`,`7.0.1` (first seen) → one change `Skip:true`; `7.0.2`→`7.0.3` `Skip:false`; model `a`→`b` `Skip:false`; `vlatest`→`nightly` (non-semver) `Skip:false`.
  - `TestLatestProposals`: three superpowers changes → only the last; a skipped last one → none for that series (not an earlier one); an unknown dimension is not possible, so assert `ComponentsFor("superpowers")` equals `[]string{"task_review","final_review"}`.
  - `TestVersionMarkers_ReadOnlyConnection`: run `VersionMarkers` on a connection opened with `store.OpenReadOnly` on a copy of the DB; no error.
- [ ] **Step 2: Run** `go test ./internal/telemetry -run 'TestVersionMarkers|TestLatestProposals|OlderNeverSeen' -v` — Expected: FAIL.
- [ ] **Step 3: Implement** the four items above. `Week` via the Monday-of-ISO-week of `At.UTC()`. Skip rule computed in the same pass as Task 1.
- [ ] **Step 4: Run** `go test ./internal/telemetry -v` — Expected: PASS.
- [ ] **Step 5: Commit** `feat(telemetry): VersionMarkers en voorstelfilter voor versiewissels (#380)`.

### Task 3: Configuratie en de tickstap

**Files:**
- Modify: `internal/hostconfig/hostconfig.go` (+ `hostconfig_test.go`)
- Create: `internal/cli/tick_versions.go`
- Test: `internal/cli/tick_versions_test.go`
- Modify: `internal/cli/tick.go` (na `collectRuns`, rond regel 271)

**Interfaces:**
- Consumes: `telemetry.VersionChanges`, `telemetry.LatestProposals`, `telemetry.ComponentsFor`; `forge.Forge.FindByMarker`, `CreateIssue(ctx, forge.NewIssue{Title, Body})`; `ticked map[string]forge.Forge`; `repoconfig.Repo.Language`; `collect.StateDir`.
- Produces:
  ```go
  // hostconfig
  type Telemetry struct{ ProposalsRepo string } // "" = off
  func LoadTelemetry(path string) (Telemetry, error) // invalid owner/name → error naming the key
  // cli
  const versionCheckEvery = time.Hour
  const lastVersionCheckFile = "last-version-check"
  func versionMarker(c telemetry.VersionChange) string // full comment incl. " -->"
  func proposeVersionChanges(ctx context.Context, env tickEnv, db *sql.DB, tel hostconfig.Telemetry,
      repos []repoconfig.Repo, ticked map[string]forge.Forge, errOut io.Writer) (failed bool)
  ```

- [ ] **Step 1: Write failing tests.**
  - hostconfig: `TestLoadTelemetry` — `[telemetry] proposals_repo = "o/r"` → `"o/r"`; missing file/key → `""`; `"nope"` → error containing `proposals_repo`.
  - cli, with fake forge (`forgetest.Server`) and a DB seeded via the Task 1 helper (copy a small `seedSetups` helper into the cli test file):
    - `TestVersionMarker_FullComment`: `superpowers`→`<!-- lighthouse-version-change: superpowers →7.0.2 -->`; harness `... harness codex →0.6 -->`; model `... model native →claude-sonnet-5-5 -->`.
    - `TestPropose_OneIssueThenNothing`: two steps (second after `env.Now` +1h) → exactly one issue; body holds the marker, old→new, first run id, components, a reference to the #388 spec; a third run on the same version gives no new change.
    - `TestPropose_PrefixVersionNotFound`: existing issue with `→7.0.20`, change to `7.0.2` → issue is created.
    - `TestPropose_ClosedProposalNotRecreated`: existing closed issue with the marker → nothing created.
    - `TestPropose_LateRowNoSecondProposal`: marker `superpowers →B` exists; a row with earlier `started_at` and value X arrives (so latest change is X→B or B stays) → nothing created.
    - `TestPropose_OnlyLatestPerSeries`: three history changes → one issue.
    - `TestPropose_HourGate`: second call within the hour does not even call `FindByMarker` (count calls); file written after a forge failure too, and `failed == true` with one log line.
    - `TestPropose_NoProposalsRepoOrNotDispatchRepo`: no write, one log line with the reason, `failed == false`.
    - `TestPropose_SkipAndNoComponent`: a `Skip` change gets no issue.
    - `TestPropose_LanguageNL`: repo language `nl` → Dutch title/body; `en` → English.
- [ ] **Step 2: Run** `go test ./internal/hostconfig ./internal/cli -run 'TestLoadTelemetry|TestVersionMarker|TestPropose' -v` — Expected: FAIL.
- [ ] **Step 3: Implement `LoadTelemetry`** in `hostconfig.go`, same decoding pattern as `LoadDispatch` (only the `[telemetry]` table; `owner/name` shape check; unknown key under `telemetry` is an error).
- [ ] **Step 4: Implement `proposeVersionChanges`** in `tick_versions.go`:
  1. empty `tel.ProposalsRepo` or not in `ticked`/`repos` → one log line, return false (no gate file written);
  2. read `<state>/telemetry/last-version-check` (same format as `readLastCollect`; reuse its parse helper by generalising it to `readTimeFile(stateDir, name)`); under an hour → return false;
  3. always `defer` writing the time file (`workplace.WriteFileAtomic`);
  4. `VersionChanges` → `LatestProposals`; for each: `FindByMarker(versionMarker(c))`; none found → `CreateIssue`. Any error → one log line `lighthouse: version change: <reason>`, `failed = true`, continue with the next.
  Title and body texts per language `nl`/`en`: change with old→new, first run or session and time, affected components, reason ("een versiewissel is een trigger volgens #388" / English equivalent), pointer to `docs/developer/planning/specs/2026-11-24-issue-388-explore-exploit-design.md`, then the marker line. State that lighthouse does not start the measurement.
- [ ] **Step 5: Wire in `tick.go`:** after `collectRuns`, load `hostconfig.LoadTelemetry(env.HostToml)` (error → log line, `failed = true`) and call `proposeVersionChanges(ctx, env, st.DB, tel, loadedRepos(loaded), ticked, errOut)`; failure sets `failed` but never returns early. Add a tick-level test in `tick_versions_test.go`: a failing forge leaves the sweeps running.
- [ ] **Step 6: Run** `go test ./internal/hostconfig ./internal/cli -v` — Expected: PASS.
- [ ] **Step 7: Commit** `feat(tick): één voorstel-issue per versiewissel (#380)`.

### Task 4: Documentatie, changelog en koppeling #379

**Files:**
- Create: `changelog.d/issue-380-versiewissel.md`
- Modify: `docs/tick.md`

- [ ] **Step 1:** Add the fragment (`type: added`, one Dutch-or-English line per the sibling fragments, e.g. `The tick proposes one issue per version change of superpowers, model or harness, and derives the change list from setups (#380).`).
- [ ] **Step 2:** In `docs/tick.md` add a short section *Versiewissel*: the hour gate, `[telemetry] proposals_repo`, the marker, and that `VersionMarkers` is what #379 reads (`markers`); note that whoever merges last wires it (`grep -n "markers" internal/telemetry` on `main` decides whether #379 already landed; if so, wire the one call in the timeline reader and add the test that a change appears in the right week).
- [ ] **Step 3:** Run the full gate: `[test_gate] command` from `lighthouse.toml`, `lighthouse imports check`, and `go run ./cmd/lighthouse init --upgrade` (expected `No changes.`; this change touches no `pack/`).
- [ ] **Step 4: Commit** `docs(tick): versiewissel en changelog (#380)`.

---

## Self-Review

- **Spec coverage:** wissellijst en dimensies/reeksen/filters → Task 1; oudere versie, markering, onderdelen → Task 2; uurpoort, `proposals_repo`, marker, één voorstel, taal, fouten → Task 3; AC-tabel: markering (T2), één voorstel + tweede run (T3 `OneIssueThenNothing`), geen installatie (alleen `SELECT` en `CreateIssue`), A,B,A,B (T1), late rij (T3). Koppeling #379 → Task 4.
- **Type consistency:** `VersionChange`, `Dimension`, `Skip`, `LatestProposals`, `ComponentsFor` identiek gebruikt in Task 2 en 3.
- **Aannames om te verifiëren door de uitvoerder:** `pins.ParseVersion` accepteert `7.0.2`-vormen (anders eigen kleine vergelijking in `versions.go`); `forgetest.Server` ondersteunt `FindByMarker` en `CreateIssue`.
