# Een niet-geaccepteerde launch met reden — implementatieplan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Een launch die de werkplek weigert of die onzeker is, noemt haar reden op de tick-regel, en een geweigerde launch staat ook op het issue, met de reden.

**Architecture:** `workplace.Launch` geeft bij `NotAccepted` voortaan de reden terug waarmee hij de run sloot. `applier.start` (`internal/dispatch/apply.go`) zet die reden, en bij `Uncertain` de fout, op één logregel, en post na een `NotAccepted` een 🤖 Teruggegeven met `reason: start mislukt: <reden>` en de sessie van de run. De volgorde van een start (Gestart vóór de launch, plan 114) blijft.

**Tech Stack:** Go, SQLite (`internal/runs`), het commentprotocol in `internal/protocol`.

**Spec:** issue #195; de volgorde van een start komt uit [het plan van #114](2026-10-31-issue-114-decide-apply-keten.md) (Besluit "de volgorde van een start") en [de M2-spec](../specs/2026-10-27-m2-dispatcher-design.md) §3 stap 5.

## Besluit: een vervolg-comment, en wel een Teruggegeven

Het issue laat twee wegen open: de 🤖 Gestart pas na een geaccepteerde launch, of een vervolg op de Gestart. Dit plan kiest het vervolg, als 🤖 Teruggegeven met de bestaande reden `reason_start_failed`.

- **Gestart blijft vóór de launch.** Plan 114 legde die volgorde bewust vast: de rij staat vóór de comment, en de comment staat vóór de agent, zodat de agent zijn eigen Gestart vindt en de wezenregel hem nooit voor een wees aanziet. Een post ná de launch zou bovendien een agent kunnen laten draaien zonder Gestart als die post mislukt, en `Uncertain` zou dan ook moeten posten.
- **Teruggegeven, geen nieuw soort comment.** De tekst bestaat al: na twee mislukte pogingen post decide (`issueCtx.startFailed`) precies een Teruggegeven met `start mislukt: <runs.reason>` en de sessie van de tweede rij. Post apply die nu bij elke `NotAccepted` zelf, dan ziet `startFailed` na de tweede poging een Teruggegeven van die sessie staan en post hij niets dubbel; hij blijft het vangnet als de post van apply mislukte. Een nieuw soort (`🤖 Niet gestart`) zou `header.go`, twee `headings.toml`, de Python-tweeling en de dogfood-upgrade raken voor dezelfde zin.
- **Wat dat kost.** Na de eerste weigering staat het issue tot de volgende tick als *teruggegeven* in de UI (`overview`), terwijl decide de tweede poging nog start: `retry` kijkt alleen naar de runhistorie, niet naar comments. De tweede poging post een nieuwe Gestart, en die is dan de laatste header. `docs/tick.md` zegt dat.
- **Onzeker post niets op het issue.** Bij `Uncertain` kan er een agent draaien; "geen agent" zou dan onwaar zijn. Alleen de logregel noemt de reden.

## Global Constraints

- Code, commentaar en testnamen in het Engels; `docs/*.md` in het Nederlands; het changelog-fragment in het Engels, zoals `changelog.d/issue-118-lopende-runs.md`.
- Go volgt `/lighthouse-go-standard`; tests volgen `/lighthouse-test-standard`.
- Logregels gaan via `ap.logf` en dragen geen `lighthouse: `-prefix zelf (die zet `Env.Log`).
- Eén regel: een reden met regeleindes wordt `strings.ReplaceAll(strings.TrimSpace(s), "\n", "; ")`, dezelfde vorm als `oneLine` in `internal/cli/tick.go`.
- Geen wijziging onder `pack/`, dus geen `init --upgrade`.
- Testcommando: `go test -race ./internal/dispatch ./internal/workplace ./internal/cli`; de hele gate staat in `lighthouse.toml` `[test_gate]`.

## Review Focus

1. **Een reden met regeleindes of een lange JSON-body** (de 409 van kenn-forge) → één logregel, en de `reason:`-regel van de Teruggegeven blijft één regel. → Taak 1 (`TestApplyNotAcceptedLogsReason`), Taak 2 (`RenderReturned` vlakt al af).
2. **De Teruggegeven na poging 1 mag de tweede poging niet tegenhouden.** → Taak 2, golden `dedup-one-start-failed-returned.json`.
3. **Na de tweede weigering geen dubbele Teruggegeven** (apply en decide). → bestaande golden `dedup-two-start-failed-returned.json`; Taak 2 zet de sessie van de run in de footer, zodat die golden geldt.
4. **De post van de Teruggegeven mislukt.** De run is al dicht; dat is geen actiefout, wel een logregel. → Taak 2, `TestApplyNotAcceptedReturnPostFails`.
5. **`Uncertain` post niets op het issue.** → Taak 2, `TestApplyUncertainPostsNoReturn`.

---

### Task 1: De reden op de tick-regel

**Files:**
- Modify: `internal/workplace/lifecycle.go:28-55` (`Launch`)
- Modify: `internal/workplace/lifecycle_test.go:90-176` (de drie aanroepen van `Launch`)
- Modify: `internal/dispatch/apply.go:267-289` (de `switch outcome`)
- Test: `internal/dispatch/apply_test.go`

**Interfaces:**
- Produces: `func Launch(ctx context.Context, db *sql.DB, w Workplace, runID int64, branch string, l LaunchSpec, now time.Time) (Outcome, string, error)` — de `string` is de reden waarmee de run als `start_failed` sloot; leeg tenzij `NotAccepted`. De `error` behoudt zijn betekenis (bij `NotAccepted`: de release mislukte; bij `Uncertain`: diagnostisch).
- Produces: `func oneLine(s string) string` in `internal/dispatch/apply.go`.

- [ ] **Step 1: Failing tests**

  - `lifecycle_test.go`: pas de aanroepen aan op drie waarden. In `TestLaunchNotAcceptedReleasesStartFailed`: `reason == got.reason` en `strings.Contains(reason, "forge said 422")`. In de prepare-test (r175): `strings.HasPrefix(reason, "prepare: ")`. Bij `Accepted`/`Uncertain`: `reason == ""`.
  - `apply_test.go`, nieuw `TestApplyNotAcceptedLogsReason`: `h.wp.spawnErr = fmt.Errorf("kenn-forge answered 409: session\nnot live: %w", workplace.ErrNotAccepted)`. Assert: `h.out.String()` bevat `applyRepo + "#7: launch of run 1 not accepted: kenn-forge answered 409: session; not live: workplace: spawn not accepted"`, en `strings.Count(h.out.String(), "\n") == 1`.
  - Nieuw `TestApplyUncertainLogsReason`: `spawnErr = fmt.Errorf("sessions: timeout\nretry: %w", workplace.ErrUncertain)`. Assert: log bevat `"launch of run 1 uncertain: spawn run 1: sessions: timeout; retry: "` en precies één `"\n"`.

- [ ] **Step 2: Draai, verwacht rood**

  Run: `go test ./internal/workplace ./internal/dispatch`
  Expected: compileerfout op `Launch` (twee waarden), daarna de logasserts rood.

- [ ] **Step 3: Implementeer**

  `Launch` geeft bij elke `NotAccepted`-tak de reden terug die hij aan `releaseFailed` geeft (`"prepare: "+…`, `"record workplace: "+…`, `err.Error()`). In `applier.start`: `outcome, reason, err := workplace.Launch(…)`; de `Uncertain`-regel wordt `"%s#%d: launch of run %d uncertain: %s"` met `oneLine(err.Error())`, de `NotAccepted`-regel `"%s#%d: launch of run %d not accepted: %s"` met `oneLine(reason)`.

- [ ] **Step 4: Draai, verwacht groen**

  Run: `go test -race ./internal/workplace ./internal/dispatch ./internal/cli`
  Expected: PASS (ook `TestApplyNotAcceptedLogsNoNilError`).

- [ ] **Step 5: Mutatiecheck**

  Laat in `apply.go` de `: %s` met de reden weg: `TestApplyNotAcceptedLogsReason` moet rood worden. Zet terug.

- [ ] **Step 6: Commit**

  ```bash
  git add internal/workplace/lifecycle.go internal/workplace/lifecycle_test.go internal/dispatch/apply.go internal/dispatch/apply_test.go
  git commit -m "fix(dispatch): de tick-regel noemt de reden van een geweigerde of onzekere launch (#195)"
  ```

### Task 2: Een geweigerde launch staat op het issue

**Files:**
- Modify: `internal/dispatch/apply.go` (`NotAccepted`-tak van `applier.start`; doc van `Result.NotAccepted`)
- Test: `internal/dispatch/apply_test.go`, `internal/cli/run_test.go:259-286`
- Create: `internal/dispatch/testdata/decide/dedup-one-start-failed-returned.json`

**Interfaces:**
- Consumes: `reason` uit `workplace.Launch` (Task 1); `reasonText(Action) string`, `protocol.RenderReturned(reason, session, detail, language string) string`.

- [ ] **Step 1: Failing tests**

  - `TestApplyNotAcceptedCountsNoStart` → hernoem naar `TestApplyNotAcceptedPostsReturned`, met `spawnErr = fmt.Errorf("kenn-forge answered 409: %w", workplace.ErrNotAccepted)`. Assert: `bodies` heeft lengte 2; `bodies[1]` begint met `"🤖 " + protocol.Heading("kind_returned", "nl") + "\nreason: " + protocol.Heading("reason_start_failed", "nl") + ": kenn-forge answered 409: workplace: spawn not accepted\nsession: "` gevolgd door de sessie van de run (lees die uit `h.wp.spawned[0].SessionID`), en eindigt met `protocol.Footer(<die sessie>)`. De overige asserts (`Started == 0`, `NotAccepted`) blijven.
  - Nieuw `TestApplyNotAcceptedReturnPostFails`: `onPost` telt en zet bij de tweede aanroep `postErr = errors.New("forge down")`. Assert: `err == nil`, `res.Failed[applyRepo] == nil`, `res.NotAccepted` bevat `applyRepo+"#7"`, de log bevat `"post Returned of run 1: forge down"`.
  - Nieuw `TestApplyUncertainPostsNoReturn`: `spawnErr = workplace.ErrUncertain` → `len(bodies) == 1`.
  - `TestApplyNotAcceptedReleaseFailsIsFailed`: voeg `len(bodies) == 1` toe (een open run zonder release krijgt geen Teruggegeven).
  - `internal/cli/run_test.go` `TestRunNotAccepted`: twee comments; `posted[1]` begint met `"🤖 Returned\nreason: start failed: "`; de suffix `"\nlighthouse: not accepted o/r#7\n"` blijft; `errOut` bevat `"launch of run 1 not accepted: "`. Pas het commentaar boven de test aan ("the Started stands, a Returned follows").
  - Golden `dedup-one-start-failed-returned.json`: kopie van `dedup-one-start-failed.json` met na comment 64 een comment 65 van `lh-bot`, `created_at` `2026-10-31T11:06:00Z`, body `"🤖 Teruggegeven\nreason: start mislukt: launch: not accepted\nsession: aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee\n\n<sub>session aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee</sub>"`. `want` is gelijk aan dat van `dedup-one-start-failed.json`: de tweede poging start toch.

- [ ] **Step 2: Draai, verwacht rood**

  Run: `go test ./internal/dispatch ./internal/cli`
  Expected: de nieuwe apply- en run-asserts rood; de golden is al groen (hij pint bestaand gedrag van `retry` vast).

- [ ] **Step 3: Implementeer**

  In de `NotAccepted`-tak, na de logregel en vóór de `append`: bouw `ra := a` met `ra.Reason, ra.ReasonArg = "reason_start_failed", reason` en post `protocol.RenderReturned(reasonText(ra), session, "", a.Language)` via `ap.env.Poster(a.Repo).CreateComment`. Een fout logt `"%s#%d: post Returned of run %d: %v"` en geeft geen actiefout. Ook bij `triggerUsage`: er draait dan evenmin een agent. Werk het doc-commentaar van `Result.NotAccepted` bij ("its Started stays, a Returned follows").

- [ ] **Step 4: Draai, verwacht groen**

  Run: `go test -race ./internal/dispatch ./internal/workplace ./internal/cli`
  Expected: PASS, ook `dedup-two-start-failed-returned`.

- [ ] **Step 5: Mutatiecheck**

  Post de Teruggegeven zonder `session`: `TestApplyNotAcceptedPostsReturned` moet rood worden. Zet terug.

- [ ] **Step 6: Commit**

  ```bash
  git add internal/dispatch/apply.go internal/dispatch/apply_test.go internal/cli/run_test.go internal/dispatch/testdata/decide/dedup-one-start-failed-returned.json
  git commit -m "fix(dispatch): een geweigerde launch post een Teruggegeven met de reden (#195)"
  ```

### Task 3: Docs en changelog

**Files:**
- Modify: `docs/tick.md:100-108` (*De volgorde van een start*), `docs/tick.md:160-167` (tweede poging), `docs/tick.md:230-241` (`run`-regels)
- Modify: `docs/workplace.md:108-119` (`NotAccepted`)
- Create: `changelog.d/issue-195-launch-reden.md`

- [ ] **Step 1: `docs/tick.md`**

  In *De volgorde van een start*, na "als laatste start het de agent": wat de gebruiker ziet bij een weigering (logregel `<repo>#<n>: launch of run <id> not accepted: <reden>`, een 🤖 Teruggegeven *start mislukt: <reden>* onder de Gestart, de run `start_failed`, de volgende tick een tweede poging met een nieuwe Gestart; tot dan toont de UI het issue als teruggegeven) en bij een onzekere launch (logregel `… uncertain: <fout>`, niets op het issue, want er kan een agent draaien; decide beslist de open run later). Regel 165-167: de Teruggegeven van de tweede weigering post apply al; decide post hem alleen als die post mislukte. Bij `lighthouse: not accepted`: "De 🤖 Gestart blijft staan" wordt "Onder de 🤖 Gestart staat een Teruggegeven met de reden".

- [ ] **Step 2: `docs/workplace.md`** — bij `NotAccepted`: de reden staat ook op de logregel en in de Teruggegeven.

- [ ] **Step 3: Fragment** `changelog.d/issue-195-launch-reden.md`, `type: fixed`, één alinea Engels: a refused launch now names its reason on the tick line and posts a 🤖 Returned *start failed: <reason>* under its 🤖 Started; an uncertain launch names its cause on the tick line.

- [ ] **Step 4: Verifieer**

  Run: `python3 .lighthouse/scripts/gate_go.py -- go test -race`
  Expected: groen; `md-link-check` blokkeerde geen van de drie bestanden bij het schrijven.

- [ ] **Step 5: Commit**

  ```bash
  git add docs/tick.md docs/workplace.md changelog.d/issue-195-launch-reden.md
  git commit -m "docs(tick): wat de gebruiker ziet bij een geweigerde of onzekere launch (#195)"
  ```
