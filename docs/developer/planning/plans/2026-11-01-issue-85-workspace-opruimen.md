# Workspace opruimen zodra een issue van Controller af gaat — implementatieplan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** De tick roept de opruimregel van #113 echt aan: elke tick ruimt hij de forge-workspace op van een issue dat niet (meer) in Controller staat, zonder open run, schoon en gepusht; anders komt de workspace onder *Workspaces to clean up*.

**Architecture:** `workplace.Cleanup` (#113) bestaat, maar heeft geen aanroeper. Dit plan voegt drie dingen toe: een kandidatenbron (`runs.Workspaces`: elke workspace uit de runrijen die nog niet verwijderd is, migratie `0005`), een veegfunctie `workplace.Sweep` die per kandidaat de board-status en de gemergede PR invult en `Cleanup` aanroept, en de aanroep in `lighthouse tick`, na decide en apply, **elke tick**, ook als de repo niet *due* is. Het pad van een workspace komt uit een nieuwe interfacemethode `Lookup`. Het overzicht filtert sinds #175 al op Controller; daar komt alleen een regressietest bij.

**Tech Stack:** Go, `modernc.org/sqlite`, `net/http/httptest`.

**Spec:** [`docs/developer/planning/specs/2026-10-27-m2-dispatcher-design.md`](../specs/2026-10-27-m2-dispatcher-design.md), §4 regel 8 *Opruimen*, §8 *Werkplek bezet en opruimen*, §12 *Overzicht (#85 deel 2)* · Issue #85 · Voorganger: [`2026-10-31-issue-113-forge-werkplek.md`](2026-10-31-issue-113-forge-werkplek.md).

## Global Constraints

- Go volgt `/lighthouse-go-standard`; tests volgen `/lighthouse-test-standard` (classificatiezin vóór de eerste edit, mutatiecheck vóór "klaar").
- Code-taal Engels onder `internal/**`: namen, commentaar, foutmeldingen, redenen, logregels. Plan, docs (`docs/*.md`), commits en PR in het Nederlands; het changelog-fragment in het Engels, zoals de bestaande.
- Een lopende run wordt nooit afgebroken voor het opruimen; een vreemde sessie stopt lighthouse nooit (spec §8). `Cleanup` bewaakt dat al; de veegfunctie roept niets anders aan dat een workspace verwijdert.
- Onmeetbaar is nooit "schoon": een fout bij board, PR, forge of git wordt een achterblijver-record met `not measurable: …`, nooit een verwijdering.
- Geen nieuwe tabel, geen tweede database (milestone No-Gos). Migratie `0005` voegt alleen een kolom toe aan `runs`.
- Subprocessen alleen via een injecteerbare `workplace.Runner`; geen test raakt een echte forge of een git-remote op het net.
- Commits: Conventional Commits, Nederlands, één logische wijziging per commit. De PR krijgt een fragment in `changelog.d/`.
- Verificatie zoals CI: `go test -race -shuffle=on ./...` en `golangci-lint run`.

**Besluit: opruimen buiten Decide.** Spec §4 noemt opruimen als regel 8 van Decide. Decide is puur en draait alleen als de repo *due* is (de vingerafdruk ziet alleen Controller- en On-hold-items). Een vuile workspace die de gebruiker later opruimt of pusht, verandert die vingerafdruk niet, en zou dan nooit meer opnieuw getoetst worden. Bovendien meet `Cleanup` zelf (forge-sessies, git) en past dat niet in een pure regel. Daarom: een eigen stap in de tick, na apply, elke tick, voor elke repo die `Dispatchable` is en waarvan de precheck slaagde. `docs/tick.md` noemt de stap.

**Besluit: On hold telt als in de controller.** Spec §8 zegt "niet meer in Controller". De tick leest ook On hold en zet een geparkeerd issue zelf terug (§9); dan hoort zijn workspace nog bij de controller. `InController` is dus waar bij Status `Controller` of `On hold`, dezelfde set als `dispatchStatuses` in `internal/dispatch/gather.go`. Een menselijk On hold houdt zijn workspace daardoor ook; dat kost niets.

**Besluit: verwijderd blijft verwijderd.** Na een geslaagde verwijdering (ook een 404) krijgt elke runrij met die workspace `workspace_removed_at`. Zonder die markering vraagt elke tick forge opnieuw naar elke oude workspace.

## Review Focus

1. **Issue terug naar Controller terwijl zijn workspace als achterblijver staat**: het record verdwijnt uit *Workspaces to clean up*, de workspace blijft. Test in Task 3 (`TestSweepClearsLeftoverInController`).
2. **Board niet leesbaar**: dan is van geen enkel issue bekend of het in Controller staat; er wordt niets verwijderd en geen record geschreven. Test in Task 4 (`TestTickSweepSkipsRepoWithoutBoard`: precheck faalt → geen `Sessions`, geen `Remove`).
3. **Squash-merge, branch op GitHub verwijderd**: `HEAD` staat op geen enkele `origin`-ref maar is de head van de gemergede PR; dat telt niet als niet-gepusht. Test in Task 3 (`TestSweepPassesMergedHead`).
4. **Vier runs, één workspace (#945)**: één kandidaat, één `Remove`, en alle vier rijen gemarkeerd. Test in Task 1 (`TestWorkspacesOnePerWorkspace`) en Task 4.
5. **Remove faalt met iets anders dan 409**: de tick meldt de fout (exit 1), de workspace staat als `not measurable: remove: …` in de lijst, en de volgende tick probeert opnieuw. Test in Task 3 (`TestSweepRemoveErrorKeepsGoing`: de tweede kandidaat wordt nog geveegd).

---

### Task 1: Kandidaten uit de runrijen

**Files:**
- Create: `internal/store/migrations/0005_workspace_removed.sql`
- Create: `internal/runs/workspaces.go`
- Test: `internal/runs/workspaces_test.go`, `internal/store/migrate_test.go`

**Interfaces:**
- Produces:
  - `type WorkspaceRef struct { Issue int; WorkspaceID, Branch string }`
  - `func Workspaces(ctx context.Context, db *sql.DB, repo string) ([]WorkspaceRef, error)` — één ref per `workspace_id != ''` waarvoor geen rij `workspace_removed_at` heeft; `Issue` en `Branch` van de rij met het hoogste `id`; gesorteerd op issue, dan workspace-id; nooit nil.
  - `func MarkWorkspaceRemoved(ctx context.Context, db *sql.DB, repo, workspaceID string, now time.Time) error` — zet `workspace_removed_at` (RFC 3339, UTC, via `stamp`) op elke rij van repo met die workspace en nog geen markering.

- [ ] **Step 1: Migratie.** `0005_workspace_removed.sql`: `ALTER TABLE runs ADD COLUMN workspace_removed_at TEXT;`. Pas in `migrate_test.go` de verwachte `user_version` aan van 4 naar 5 en voeg `TestMigrate0005KeepsRuns` toe naar het patroon van `TestMigrate0004KeepsRuns`: een rij van vóór de migratie houdt al zijn velden en heeft `workspace_removed_at IS NULL`.
- [ ] **Step 2: Falende tests** in `workspaces_test.go`:
  - `TestWorkspacesOnePerWorkspace`: vier gesloten rijen van issue 945 met `workspace_id = "d7c0b0ea0de54804"` en branch `work-issue-945-immich-smart-search-191b7750`, plus een rij van issue 12 met `ws-12` → precies `[{12 ws-12 …} {945 d7c0b0ea0de54804 work-issue-945-…}]`.
  - `TestWorkspacesSkipsEmptyAndRemoved`: een rij zonder workspace-id en een workspace na `MarkWorkspaceRemoved` komen niet terug; een rij van een andere repo ook niet.
  - `TestWorkspacesLastRowWins`: twee rijen met dezelfde workspace en verschillende branch → de branch van het hoogste id.
  - `TestMarkWorkspaceRemovedOnlyThatWorkspace`: markeert alle rijen van die workspace in die repo, geen andere workspace, geen andere repo.
- [ ] **Step 3:** `go test ./internal/runs/ ./internal/store/` → FAIL (undefined: `Workspaces`).
- [ ] **Step 4:** Implementeer de twee functies (één `SELECT … GROUP BY workspace_id` met `MAX(id)`, gejoined op de rij zelf).
- [ ] **Step 5:** `go test -race ./internal/runs/ ./internal/store/` → PASS.
- [ ] **Step 6:** Commit `feat(runs): workspaces van runrijen als opruimkandidaten (#85)`.

### Task 2: Het pad van een workspace

**Files:**
- Modify: `internal/workplace/workplace.go` (interface), `internal/workplace/lifecycle.go` (`Cleanup`), `internal/workplace/kennforge/adapter.go`
- Modify (fakes): `internal/workplace/lifecycle_test.go`, `internal/dispatch/apply_test.go`, `internal/cli/tick_test.go`
- Test: `internal/workplace/kennforge/adapter_test.go`, `internal/workplace/lifecycle_test.go`

**Interfaces:**
- Produces: in `Workplace`: `Lookup(ctx context.Context, workspaceID string) (Workspace, error)` — `Path` uit `worktree_path`, `Branch` uit `git_head_ref`; een 404 is `ErrGone` (gewrapt met `%w`); een workspace zonder `worktree_path` is een fout.
- Produces: `Cleanup` met een kandidaat zonder `Workspace.Path` zoekt het pad op met `w.Lookup`. `ErrGone` daar telt als verwijderd, precies zoals `ErrGone` van `Sessions` nu (drager wissen, record wissen, `removed = true`). Een andere fout wordt het record `not measurable: lookup: <fout>`.

- [ ] **Step 1: Falende tests.**
  - `adapter_test.go`: `TestLookup` (httptest-forge geeft `{"id":"w1","worktree_path":"/w/1","git_head_ref":"issue-7"}` → `Workspace{ID:"w1", Path:"/w/1", Branch:"issue-7"}`), `TestLookupGone` (404 → `errors.Is(err, workplace.ErrGone)`), `TestLookupNoPath` (leeg `worktree_path` → fout, geen `ErrGone`).
  - `lifecycle_test.go`: `TestCleanupLooksUpPath` (kandidaat zonder pad, fake `Lookup` geeft `/p/w1` → de git-checks draaien op `/p/w1` en de workspace wordt verwijderd), `TestCleanupLookupGone` (fake `Lookup` geeft `ErrGone` → `removed == true`, `Discard` aangeroepen, geen `Sessions`), `TestCleanupLookupFails` (andere fout → record met reden die begint met `not measurable: lookup:`, geen `Remove`).
- [ ] **Step 2:** `go test ./internal/workplace/...` → FAIL.
- [ ] **Step 3:** Voeg `Lookup` toe aan de interface, de adapter (zelfde GET en 404-afhandeling als `TranscriptDir`) en de drie fakes. De fakes in `apply_test.go` en `tick_test.go` geven `Workspace{ID: id, Path: "/work/" + id}`; de tick-fake krijgt in Task 4 meer. Pas `Cleanup` aan: de padcontrole (`filepath.IsAbs`) blijft, maar komt na de lookup.
- [ ] **Step 4:** `go test -race ./internal/...` → PASS.
- [ ] **Step 5:** Commit `feat(workplace): Lookup geeft het pad van een workspace (#85)`.

### Task 3: Vegen per repo

**Files:**
- Create: `internal/workplace/sweep.go`
- Test: `internal/workplace/sweep_test.go`

**Interfaces:**
- Consumes: `runs.Workspaces`, `runs.MarkWorkspaceRemoved` (Task 1); `Cleanup` met lookup (Task 2); `ClearLeftover`, `RecordLeftover`.
- Produces:

  ```go
  // SweepInput is what Sweep needs of one repo besides the database and the workplace.
  type SweepInput struct {
  	Repo string
  	// InController reports whether the controller still holds the issue: board Status
  	// Controller or On hold.
  	InController func(issue int) bool
  	// MergedHead is the head SHA of the last merged PR of branch, or "" when none.
  	MergedHead func(ctx context.Context, branch string) (string, error)
  }

  // Sweep runs Cleanup for every workspace of in.Repo that runs.Workspaces returns. removed
  // lists the workspace ids it removed, in the order of runs.Workspaces. err joins the errors
  // of the candidates; one candidate's error never stops the others.
  func Sweep(ctx context.Context, db *sql.DB, w Workplace, run Runner, in SweepInput, now time.Time) (removed []Leftover, err error)
  ```

  `removed` hergebruikt `Leftover` voor issue, workspace-id en pad, met lege `Reason`.

Gedrag per kandidaat, in deze volgorde:
1. `InController(issue)` of een open run van het issue (`runs.Open`, één keer per aanroep gelezen) → `ClearLeftover` en door. `Cleanup` zou hier ook niets doen, maar laat een oud record staan.
2. `MergedHead(ctx, branch)`; een fout → `RecordLeftover` met `not measurable: merged PR: <fout>` en door. Lege branch → `""` zonder aanroep.
3. `Cleanup` met `Candidate{Repo, Issue, InController: false, MergedHead, Workspace{ID, Branch}}` (pad leeg: `Cleanup` zoekt het op).
4. `removed == true` → `runs.MarkWorkspaceRemoved`; ook als `Cleanup` daarnaast een fout gaf (het record-wissen kan falen nadat de workspace al weg is).

- [ ] **Step 1: Falende tests** in `sweep_test.go`, met `fakeWP` uit `lifecycle_test.go` en een fake `Runner` die `git status --porcelain` en `rev-list --count` beantwoordt:
  - `TestSweepRemovesOffController` — fixture #945: vier gesloten rijen, één workspace, `InController` false, schoon en gepusht → `Remove("d7c0b0ea0de54804")` precies één keer, `removed` heeft issue 945, `runs.Workspaces` daarna leeg.
  - `TestSweepKeepsDirty` — zelfde, git-status niet leeg → geen `Remove`, `Leftovers` geeft `{945, d7c0b0ea0de54804, <pad uit Lookup>, "uncommitted changes"}`.
  - `TestSweepKeepsUnpushed` — `rev-list` geeft `2`, geen merged head → record `unpushed commits`.
  - `TestSweepPassesMergedHead` — `MergedHead` geeft `abc`, `rev-parse HEAD` geeft `abc`, `rev-list` geeft `2` → verwijderd.
  - `TestSweepSkipsController` — `InController` true → geen `Lookup`, geen `Sessions`, geen `Remove`.
  - `TestSweepSkipsOpenRun` — het issue heeft een open rij (`runs.Take` zonder `Release`) en staat niet in Controller → geen `Stop`, geen `Remove` (AC 4).
  - `TestSweepClearsLeftoverInController` — een bestaand record voor de workspace, `InController` true → record weg, workspace staat.
  - `TestSweepMergedHeadFails` — `MergedHead` geeft een fout → record `not measurable: merged PR: …`, geen `Remove`.
  - `TestSweepRemoveErrorKeepsGoing` — twee kandidaten; `Remove` van de eerste geeft een 500-achtige fout → `err != nil`, de tweede is verwijderd en gemarkeerd, de eerste niet gemarkeerd.
  - `TestSweepGoneMarksRemoved` — `Lookup` geeft `ErrGone` → in `removed`, gemarkeerd, geen `Remove`.
- [ ] **Step 2:** `go test ./internal/workplace/ -run Sweep` → FAIL (undefined: `Sweep`).
- [ ] **Step 3:** Implementeer `Sweep`.
- [ ] **Step 4:** `go test -race ./internal/workplace/` → PASS. Mutatiecheck: haal stap 1 (de `InController`-check) weg → `TestSweepSkipsController` en `TestSweepClearsLeftoverInController` worden rood; zet terug.
- [ ] **Step 5:** Commit `feat(workplace): Sweep ruimt de workspaces van een repo op (#85)`.

### Task 4: De tick veegt

**Files:**
- Modify: `internal/dispatch/gather.go` (`Pre`, `Prepare`), `internal/cli/tick.go`
- Test: `internal/dispatch/gather_test.go`, `internal/cli/tick_test.go`

**Interfaces:**
- Consumes: `workplace.Sweep`, `workplace.SweepInput` (Task 3); `forge.Client.BranchPRs`.
- Produces:
  - In `dispatch.Pre`: `Statuses map[int]string` — de Status-naam van elk board-item van `repo.Slug` (alle statussen, niet alleen Controller en On hold); een issue zonder item ontbreekt. `Fingerprint` leest het veld niet.
  - `func InController(statuses map[int]string) func(issue int) bool` in `internal/dispatch` — waar voor `dispatchStatuses`.
  - `func MergedHead(prs []forge.PR) string` in `internal/dispatch` — de `HeadSHA` van de PR met `State == "MERGED"`, `!CrossRepo` en de laatste `MergedAt`; `""` zonder.
  - In `tickEnv`: `Run workplace.Runner` — nil is `workplace.ExecRunner()`. De kennforge-adapter in `tickEnv.workplace` gebruikt hetzelfde.
  - `gatherRepo` geeft daarnaast `statuses map[int]string` terug (uit de precheck, ook als de repo niet *due* is); de tick bewaart per repo een `sweepRepo{repo repoconfig.Repo; statuses map[int]string}` voor elke repo waarvan `gatherRepo` slaagde.
  - `func sweepRepos(ctx context.Context, env tickEnv, db *sql.DB, cfg hostconfig.Dispatch, token string, repos []sweepRepo, errOut io.Writer) (failed bool)` — na `decideAndApply`. Per verwijderde workspace één regel `lighthouse: removed workspace <id> of <slug>#<issue>`; een fout één regel `lighthouse: cleanup <slug>: <fout op één regel>` en `failed = true` (tick eindigt met `errSilent`, exit 1). Een achterblijver geeft geen regel: hij staat in de lijst.

- [ ] **Step 1: Falende tests.**
  - `gather_test.go`: `TestPrepareStatuses` — board met issue 945 op `In progress`, 12 op `Controller`, 13 van een andere repo → `Statuses == {945: "In progress", 12: "Controller"}`, `Items` alleen 12. `TestMergedHead` — tabel: geen PR, open PR, twee gemergede (laatste wint), cross-repo gemerged (telt niet).
  - `tick_test.go`, met de bestaande forge-testserver en `fakeWorkplace`, die nu `Lookup`, `Remove` en `Discard` per workspace bijhoudt in plaats van ze te weigeren, en een fake `Run`:
    - `TestTickSweepsWorkspaceOffController` — fixture #945: open issue 945, Status `In progress`, vier rijen `done` met workspace `d7c0b0ea0de54804`, levende workspace zonder sessies, schoon en gepusht → na één tick `Remove("d7c0b0ea0de54804")`, regel `removed workspace d7c0b0ea0de54804 of o/r#945` op errOut; een tweede tick roept `Lookup` of `Sessions` voor die workspace niet meer aan (AC 1).
    - `TestTickSweepKeepsDirtyWorkspace` — zelfde, git-status niet leeg → geen `Remove`; `workplace.Leftovers` geeft issue 945, id `d7c0b0ea0de54804`, het pad uit `Lookup` en `uncommitted changes`; `GET /api/cleanup` is al gedekt door `internal/server` (AC 2).
    - `TestTickSweepLeavesControllerIssue` — Status `Controller` → geen `Lookup`, geen `Remove`.
    - `TestTickSweepLeavesOpenRun` — issue met open rij en sessie `working`, Status `In progress` → geen `Stop`, geen `Remove` (AC 4).
    - `TestTickSweepSkipsRepoWithoutBoard` — board-query geeft 500 → `skip`-regel zoals nu, geen `Lookup`, geen `Remove`.
    - `TestTickSweepRunsWhenNotDue` — tweede tick met dezelfde vingerafdruk (geen `gathered`-regel), een record dat er al stond, git nu schoon → de workspace wordt alsnog verwijderd.
- [ ] **Step 2:** `go test ./internal/dispatch/ ./internal/cli/` → FAIL.
- [ ] **Step 3:** Implementeer `Statuses` in `Prepare` (uit de bestaande `boardStatus`-lus), `InController`, `MergedHead`, `tickEnv.Run`, de extra terugwaarde van `gatherRepo` en `sweepRepos`. `MergedHead` in `SweepInput` wordt `func(ctx, branch) { prs, err := env.client(slug, token).BranchPRs(ctx, branch); …; return dispatch.MergedHead(prs), nil }`. `lighthouse run` veegt niet.
- [ ] **Step 4:** `go test -race -shuffle=on ./...` → PASS. Mutatiecheck: laat `sweepRepos` niet aanroepen → `TestTickSweepsWorkspaceOffController` rood; zet terug.
- [ ] **Step 5:** Commit `feat(dispatch): de tick ruimt workspaces van issues buiten Controller op (#85)`.

### Task 5: Overzicht, documentatie, changelog

**Files:**
- Test: `internal/dispatch/decide_test.go` of de golden fixtures onder `internal/dispatch/testdata/decide/`, `internal/dispatch/view_test.go`
- Modify: `docs/workplace.md` (*Opruimen*, *Wie roept dit aan*), `docs/tick.md`
- Create: `changelog.d/issue-85-workspace-opruimen.md`

AC 3 is voor *Open vragen* en *Teruggegeven* gedekt door `TestBuildOnlyController` (#175, al met #945 als fixture). *Klaar voor review* bestaat in de Go-UI niet. *Loopt* en *Wacht* zijn de kaart *Running runs* (`RunViews`): een open run, of een geparkeerde run met een resume-marker.

- [ ] **Step 1: Regressietests** (verwacht groen; de mutatie bewijst dat ze iets toetsen):
  - `view_test.go`: `TestRunViewsNoRunNoView` — #945: geen open rij, geen geparkeerde → `RunViews` geeft een lege lijst, ook al staan er vier `done`-rijen in de historie. Mutatie: laat `RunViews` ook de gesloten rijen tonen → rood.
  - decide: een geparkeerde run van issue 945 met een marker, issue niet in de snapshot (Status `In progress`) → één `drop_marker`. Bestaat zo'n fixture al (`dropMarkers`, "its issue left the snapshot"), noem hem in de PR en voeg niets toe. Anders een golden fixture `parked-off-controller-945.json`.
- [ ] **Step 2: Docs.** `docs/workplace.md`: `Lookup` in de interface-alinea; stap 1 van *Opruimen* wordt "pad opzoeken (`Lookup`); een 404 telt als verwijderd"; *Wie roept dit aan*: `Sweep` loopt elke tick na apply, voor elke repo met een leesbaar board; On hold telt als in de controller; een verwijderde workspace krijgt `workspace_removed_at` en wordt niet meer getoetst; vervang de zin "`Cleanup` heeft nog geen aanroeper". `docs/tick.md`: de stap na apply, de twee logregels en dat een achterblijver geen regel geeft. Relatieve links.
- [ ] **Step 3: Changelog.** `changelog.d/issue-85-workspace-opruimen.md` met `type: fixed` en één alinea in het Engels: the tick now removes the forge workspace of an issue that left Controller once it has no open run, no live foreign session, no local changes and no unpushed commits; a workspace it must keep is listed under *Workspaces to clean up*.
- [ ] **Step 4:** `go test -race -shuffle=on ./...` en `golangci-lint run` → groen.
- [ ] **Step 5:** Commit `docs(workplace): de tick ruimt op, met test voor het overzicht (#85)`.
