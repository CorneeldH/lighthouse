# Prioritering-tabblad (#374) — implementatieplan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Een tweede tabblad **Prioritering** in de controller-SPA met drie weergaven (Afhankelijkheden, Milestones, Zonder milestone) en een repo-filter, gevoed door één alleen-lezen endpoint `GET /api/priorities`.

**Architecture:** Een nieuwe forge-methode `Planning` levert per repo de open issues met milestone en open `blocked_by`, plus per open milestone een issue-telling zonder PR's. Een eigen, generiek gemaakte `Refresher` (300 s) bouwt het antwoord; de board-status komt uit de snapshot van de overview-bouw. De SPA rekent diepte, wachtenden, cycli, shaping en sortering uit in pure TypeScript-functies; `@dagrejs/dagre` legt de graaf uit, React en SVG tekenen.

**Tech Stack:** Go (huma, stdlib), React 19, TanStack Query, Tailwind, vitest, `@dagrejs/dagre`.

**Spec:** [2026-11-16-prioritering-tabblad-design.md](../specs/2026-11-16-prioritering-tabblad-design.md). Mockup: [2026-11-16-prioritering-tabblad-mockup.html](../specs/2026-11-16-prioritering-tabblad-mockup.html).

## Global Constraints

- De server blijft alleen-lezen: alleen `GET`, geen CSRF. Geen schrijfpad naar de forge.
- Interval van de prioriteringsbouw **300 s** (niet 90 s). Eigen `Refresher`: een mislukte bouw raakt `/api/questions`, `/api/returned` en `/api/runs` niet.
- **Geen tweede bordlezing**: de prioriteringsbouw roept `BoardItems` nergens aan.
- Milestone-telling telt **alleen issues, geen PR's**; gesloten als `NOT_PLANNED` telt niet als klaar en valt buiten de balk (GitLab: elk gesloten issue is klaar).
- Foutafhandeling zoals `forgeError`: 503 `forge.ErrNotConfigured`, anders 502. Faalt één repo, dan faalt de bouw.
- Rekenwerk (diepte, wachtenden, sortering) in de SPA; het repo-filter verbergt alleen en verandert geen getal.
- Palet van acht milestonekleuren zonder geel, grijs en navy; *Geen milestone* altijd grijs; kleur alleen voor milestones in beeld, op volgorde van (repo, milestonenummer).
- UI-tekst Engels; code, namen en commentaar Engels; artefacten Nederlands.
- Nieuwe npm-dependency met exacte versie: `@dagrejs/dagre` `3.1.3`, via `npm ci`. De build (`internal/web/dist`) wordt niet gecommit.
- `/lighthouse-go-standard` en `/lighthouse-test-standard`. Elke commit groen; nooit `--no-verify`. Elke PR krijgt een fragment in `changelog.d/`.

## Besluiten van dit plan (spec laat ze aan de bouwer)

- **Forge-methode:** `Planning(ctx) (Planning, error)` op `forge.Forge`; `BoardItems` blijft los.
- **Board-status (r3-f1):** `overview.Overview` krijgt `Statuses []IssueStatus` (`Repo`, `Issue`, `Status`). `overview.RepoSource` krijgt `Board bool` (repo heeft `[forge.board]`) en `Controller bool` (bord heeft status-optie `Controller`); `InController` en `ControllerIssues` vervallen. `BuildAll` leest `BoardItems` **één keer per repo met bord** en leidt daaruit zowel het Controller-filter als de statussen af. Een repo met bord maar zonder Controller-optie wordt dus nu ook één keer per overview-bouw gelezen (voorheen niet): dat is de prijs van "geen tweede lezing", ~53 punten per bouw per repo. Faalt de overview-bouw, dan faalt de prioriteringsbouw (502); er is geen vorige snapshot.
- **Uurtotaal GitHub-query's** (Taak 1 legt de meting vast in `docs/priorities.md`): 12 bouwen/uur × per repo (issuespagina's ≈ 21 punten per 100 issues met `blockedBy(first: 20)` + 1 punt per milestone-telling, twee aliassen per milestone in één query). Voor paia (21 milestones): ≈ 12 × (21 + 42) ≈ 760 punten/uur, naast ≈ 40 × 53 ≈ 2100/uur voor het bord van de overview. Binnen de 5000/uur.
- **GitLab-budget:** `1 + ⌈open/100⌉` issue-pagina's, één `GET …/issues/:iid/links` per open issue, één `issues_statistics` per open milestone, alles achter elkaar binnen de `BuildTimeout`-context van de bouw. Overschrijdt dat het budget, dan geeft de context-timeout een fout en faalt de bouw voor die repo zichtbaar (geen stille deelstand). Een groter budget is een latere keuze (GitLab GraphQL).
- **Serve-interval:** `serve(...)` krijgt een extra parameter `prioInterval time.Duration`; het commando geeft `server.PrioritiesInterval` (300 s) mee, tests geven een korte.

## Review Focus

- Een `blocked_by`-cyclus (ook over repo's): bouw en weergave hangen niet. Test in Taak 6.
- Een blocker in een niet-gelezen of weggefilterde repo: grijze node, "kan nu" liegt niet, filter wijzigt geen getal. Test in Taak 6 en 8.
- Een milestone met alleen PR's of alleen not-planned-issues: telt geen werk mee en geeft geen 100%. Test in Taak 1.
- Een repo zonder bord: bouw faalt niet, status blijft leeg. Test in Taak 3 en 4.
- Een open issue in een gesloten milestone staat in *Zonder milestone* met die milestone genoemd. Test in Taak 6 en 10.
- Meer milestones in beeld dan het palet kleuren heeft: palet herhaalt, legenda klopt. Test in Taak 6.
- `localStorage` die gooit (private window): tabblad werkt. Test in Taak 7.

---

### Task 1: Forge-methode `Planning` voor GitHub

**Files:**
- Create: `internal/forge/planning.go`, `internal/forge/planning_test.go`, `docs/priorities.md`
- Modify: `internal/forge/forge.go` (interface), `internal/forge/forgetest/server.go` (fake), `internal/forge/forgesuite/suite.go`
- Test: `internal/forge/planning_test.go`, `internal/forge/github_contract_test.go`

**Interfaces:**
- Produces (in `internal/forge/planning.go`):
  - `type PlanIssue struct { Number int; Title, URL string; Milestone *Milestone; BlockedBy []Blocker }` — `BlockedBy` bevat alleen **open** blockers; `Milestone.State` is `open` of `closed`.
  - `type MilestoneCount struct { Milestone; Open, Done int }` — `Open` = open issues, `Done` = gesloten met reden `completed`; geen PR's, geen not-planned.
  - `type Planning struct { Issues []PlanIssue; Milestones []MilestoneCount }` — `Milestones` alle **open** milestones, ook zonder issues; nooit nil.
  - Interface-methode `Planning(ctx context.Context) (Planning, error)` op `Forge`.
  - `(*GitHub).Planning`.

- [ ] **Step 1: Failing test `TestGitHubPlanningMapsIssuesAndBlockers`** in `planning_test.go`, tegen `forgetest.Server`: twee open issues (#1 met milestone "M1", #2 zonder), #2 geblokkeerd door #1 (zelfde repo) en door `other/repo#9` (open) en #3 (gesloten). Assert: `Issues` heeft 2 items; `Issues[1].BlockedBy` is `[{Repo:"o/r",Issue:1,State:"OPEN"},{Repo:"other/repo",Issue:9,State:"OPEN"}]` (gesloten #3 ontbreekt); `Issues[0].Milestone.Title == "M1"`; een open issue in een gesloten milestone houdt `Milestone.State == "closed"`.
- [ ] **Step 2: Failing test `TestGitHubPlanningCountsIssuesOnly`**: milestone "M1" met in de fake 3 open issues, 2 gesloten-completed, 1 gesloten-not_planned, en PR's die het zoekresultaat niet meetelt (de fake beantwoordt `is:issue`-zoekopdrachten). Assert `MilestoneCount{Open:3, Done:2}`. Tweede milestone zonder issues: `Open:0, Done:0`, staat wel in `Milestones`.
- [ ] **Step 3: Failing test `TestGitHubPlanningPaginates`**: 101 open issues ⇒ alle 101 terug, geen dubbele. Een `hasNextPage` met een cursor die niet verandert ⇒ fout (zoals `BoardStuckCursor`).
- [ ] **Step 4: Run** `go test ./internal/forge -run Planning` — verwacht FAIL (`Planning` bestaat niet).
- [ ] **Step 5: Implementeer `(*GitHub).Planning`** in `planning.go`: één GraphQL-query `Planning($after)` voor open issues (`number title url milestone { number title state description } blockedBy(first: 20) { nodes { number state repository { nameWithOwner } } pageInfo { hasNextPage } }`, pagina's van 100; `hasNextPage` op `blockedBy` is een fout, zoals bij `BoardQuery`), één query voor open milestones (`milestones(states: OPEN, first: 100)`, ook paginerend), en één query met twee `search(type: ISSUE, …) { issueCount }`-aliassen per milestone: `repo:<r> milestone:"<titel>" is:issue is:open` en `… is:issue is:closed reason:completed`. Titel met `"` of `\` wordt geëscaped; GitHub documenteert geen escape in een gequote qualifier, dus controleer dit live en laat de bouw zichtbaar falen voor een titel met `"` als het niet werkt. Voeg de operaties toe aan `forgetest.Server.graphql` (switch op operatienaam, vanuit nieuwe velden `PlanIssues`, `PlanMilestones`, `PlanSearch`). **Controleer de zoekqualifiers tegen de live API** (`gh api graphql`) en noteer wat je vond in `docs/priorities.md`.
- [ ] **Step 6: Schrijf `docs/priorities.md`**: wat het tabblad leest, de call- en puntenberekening uit "Besluiten van dit plan" met de gemeten `rateLimit { cost }` van één bouw (zonder token of zonder `gh`: de berekende getallen met de zin "niet gemeten"; dat geldt ook voor de qualifier-controle van Step 5: noteer "niet geverifieerd" in `docs/priorities.md` en in de PR).
- [ ] **Step 6b: Breid `forgesuite.Fixture` uit** met wat de asserties nodig hebben: `AddIssue` krijgt een sluitreden (`completed`/`not_planned`), er komt `AddMilestone` (ook leeg) en een capability-methode `SupportsPlanning() bool` waarmee `Run` het Planning-geval voor GitLab overslaat tot Taak 2. De GitHub-fake (`forgetest.Server`) leidt `PlanIssues`/`PlanMilestones`/`PlanSearch` uit dezelfde issuegegevens af en beantwoordt de search-aliassen door `is:issue`, `is:open|is:closed` en `reason:completed` echt te evalueren; een zoekopdracht zonder `is:issue` geeft een fout. Voeg een `(*GitLab).Planning`-stub toe die een fout geeft, anders compileert `var _ Forge = (*GitLab)(nil)` niet.
- [ ] **Step 7: Voeg `Planning` toe aan `forgesuite`** (conformiteit: dezelfde twee asserties van Step 1 en 2) en draai `go test ./internal/forge` — alleen de GitHub-kant is groen; GitLab stub-faalt tot Taak 2 en slaat het geval tijdelijk over via `SupportsPlanning() == false`.
- [ ] **Step 8: Commit** `feat(forge): Planning leest open issues, blockers en milestonetellingen (#374)`.

### Task 2: Forge-methode `Planning` voor GitLab

**Files:**
- Modify: `internal/forge/gitlab.go` (of nieuw `gitlab_planning.go`), `internal/forge/gitlabtest/` (fake), `internal/forge/gitlab_contract_test.go`
- Test: `internal/forge/gitlab_planning_test.go`

**Interfaces:**
- Consumes: `Planning`, `PlanIssue`, `MilestoneCount` uit Taak 1.
- Produces: `(*GitLab).Planning`; de `t.Skip` uit Taak 1 vervalt.

- [ ] **Step 1: Failing test `TestGitLabPlanningLinksAndStatistics`** tegen `gitlabtest`: twee open issues, issue 2 heeft `links` van type `is_blocked_by` naar issue 1 (open) en naar een issue in ander project (open); een `relates_to`-link en een gesloten blocker tellen niet mee. Milestone "M1" `issues_statistics` ⇒ `opened:3, closed:2`. Assert dezelfde vorm als GitHub: `BlockedBy` alleen open `is_blocked_by`; `MilestoneCount{Open:3, Done:2}`.
- [ ] **Step 2: Failing test `TestGitLabPlanningBudgetIsOneLinksCallPerOpenIssue`**: de fake telt requests; 3 open issues op één pagina, 2 milestones ⇒ exact `1 + 3 + 2` requests (plus de milestone-lijst). Een verzoek na een verlopen context ⇒ de fout van de context, geen deelresultaat.
- [ ] **Step 3: Run** `go test ./internal/forge -run GitLabPlanning` — FAIL.
- [ ] **Step 4: Implementeer `(*GitLab).Planning`** met REST: `OpenIssues`-pagina's, `GET /projects/:id/issues/:iid/links` per open issue (filter `link_type == "is_blocked_by"` en `state == "opened"`, `Blocker.Repo` uit het projectpad van de link), `GET /projects/:id/milestones?state=active`, `issues_statistics?milestone=<titel>`. Geen gesloten issues per milestone ophalen. Leg het budget als commentaar bij de functie vast.
- [ ] **Step 5: Verwijder de `t.Skip`**, draai `go test ./internal/forge -race` — groen.
- [ ] **Step 6: Commit** `feat(forge): Planning voor GitLab (#374)`.

### Task 3: Board-status in de overview-snapshot

**Files:**
- Modify: `internal/overview/overview.go`, `internal/cli/serve.go` (`forgeReposWith`)
- Test: `internal/overview/overview_test.go`, `internal/cli/serve_test.go`, `internal/server/forge_test.go` (`testDeps` neemt `InController overview.Filter` en de Controller-filtertests rond regel 226/241 gebruiken `only(...)`; herschrijf ze naar `Board`/`Controller` plus een fake `forge.Forge` met `BoardItems`)

**Interfaces:**
- Produces:
  - `type IssueStatus struct { Repo string; Issue int; Status string }`
  - `Overview.Statuses []IssueStatus` (nooit nil)
  - `RepoSource{ Slug string; Src forge.Forge; Board bool; Controller bool }` — `InController` en `ControllerIssues` zijn weg.

- [ ] **Step 1: Failing test `TestBuildAllReadsBoardOncePerRepo`**: een fake `forge.Forge` die `BoardItems`-aanroepen telt; repo met `Board:true, Controller:true` en items met status `Controller`, `In progress`, `""`. Assert: `BoardItems` exact 1 keer; `Questions` bevat alleen issues met status `Controller`; `Statuses` bevat alle drie de items met `Repo` = slug; items met `it.Repo != slug` worden overgeslagen.
- [ ] **Step 2: Failing test `TestBuildAllNoBoardNoBoardItemsCall`**: `Board:false` ⇒ `BoardItems` 0 keer, `Statuses` leeg (niet nil), alle open issues in `Questions`/`Returned` zoals voorheen.
- [ ] **Step 3: Failing test `TestBuildAllBoardWithoutControllerOptionShowsEveryIssue`**: `Board:true, Controller:false` ⇒ geen filter, wel `Statuses`.
- [ ] **Step 3b: Failing test `TestBuildAllBoardErrorWithoutControllerOptionKeepsOverview`**: `Board:true, Controller:false` en `BoardItems` geeft een fout ⇒ `BuildAll` slaagt (geen filter, `Statuses` leeg, fout gelogd/genegeerd). Met `Controller:true` blijft een `BoardItems`-fout een fout (voorheen ook). Reden: een repo zonder Controller-optie las het bord voorheen nooit; het tabblad mag de bestaande pagina niet breken (spec: anti-doel). Test ook `Repo`-filtering: een bord-item van een andere repo wordt niet aan deze slug toegewezen.
- [ ] **Step 4: Run** `go test ./internal/overview` — FAIL.
- [ ] **Step 5: Pas `BuildAll`/`Build` aan** (`Build` krijgt de gelezen set `map[int]bool` of nil in plaats van een `Filter`) en `forgeReposWith`: `Board: r.Board != nil`, `Controller: r.Board != nil && r.Board.StatusOptions["Controller"] != ""`; het bestaande `missing forge.board.status_options.Controller`-bericht blijft. Bij `Board && !Controller` is een `BoardItems`-fout geen fout van `BuildAll`: alleen de statussen ontbreken. Pas bestaande tests (ook in `internal/server`) aan op de nieuwe vorm (gedrag gelijk).
- [ ] **Step 6: Run** `go test ./internal/overview ./internal/cli ./internal/server -race` — groen.
- [ ] **Step 7: Commit** `feat(overview): board-status in de snapshot, één bordlezing per repo (#374)`.

### Task 4: Generieke `Refresher` en de prioriteringsbouw

**Files:**
- Create: `internal/priorities/priorities.go`, `internal/priorities/priorities_test.go`
- Modify: `internal/server/refresher.go`, `internal/server/refresher_test.go`, `internal/cli/serve.go`
- Test: `internal/priorities/priorities_test.go`, `internal/server/refresher_test.go`

**Interfaces:**
- Consumes: `forge.Planning`, `overview.Overview.Statuses`, `dispatch.RunView`.
- Produces:
  - `server.Refresher[T any]`, `server.BuildFunc[T any] func(ctx context.Context) (T, error)`, `server.NewRefresher[T any](build BuildFunc[T], interval, retry, timeout time.Duration) *Refresher[T]`, `(*Refresher[T]).Latest(ctx) (T, error)` (hernoemd uit `Overview`; de overview-aanroepers worden bijgewerkt). `const server.PrioritiesInterval = 300 * time.Second`.
  - Package `priorities`: `type Source struct { Slug string; Src forge.Forge }`; `type Repo struct { Slug string; Issues []Issue; Milestones []Milestone }`; `type Issue struct { Number int; Title, URL string; Milestone *MilestoneRef; BlockedBy []Blocker; Status string }`; `type MilestoneRef struct { Number int; Title, State string }`; `type Blocker struct { Repo string; Issue int }`; `type Milestone struct { Number int; Title, Description string; Open, Done int }`; `type Run struct { Repo string; Issue int; State, Phase string }`; `type Result struct { Repos []Repo; Runs []Run }` (nooit nil-slices);
  - `func Build(ctx context.Context, srcs []Source, statuses []overview.IssueStatus) ([]Repo, error)` — leest `Planning` per bron achter elkaar, plakt `Status` er in het geheugen op; geen `BoardItems`.

- [ ] **Step 1: Failing test `TestRefresherTwoTypesDoNotShareFailure`** in `refresher_test.go`: een `Refresher[int]` die faalt en een `Refresher[string]` die slaagt; de tweede geeft zijn waarde. (Vervangt niets: de bestaande Overview-tests draaien op `Refresher[overview.Overview]`.)
- [ ] **Step 2: Failing tests in `priorities_test.go`**: `TestBuildJoinsStatusInMemory` (status uit `statuses` komt op het juiste issue; onbekend issue ⇒ `Status == ""`), `TestBuildNeverCallsBoardItems` (fake forge laat `BoardItems` t.Fatal doen), `TestBuildFailsNamingTheRepoWhenOneRepoFails` (fout bevat de slug; geen deelresultaat), `TestBuildNoSourcesIsErrNotConfigured` (`errors.Is(err, forge.ErrNotConfigured)`), `TestBuildOpenBlockerInOtherRepoKeepsItsRepo`.
- [ ] **Step 3: Run** `go test ./internal/server ./internal/priorities` — FAIL (compileert niet).
- [ ] **Step 4: Maak `Refresher`/`BuildFunc` generiek** (alleen typeparameter en de veldtypes; gedrag gelijk), werk `startOverview` en de bestaande tests bij.
- [ ] **Step 5: Implementeer `priorities.Build`** met de typen hierboven.
- [ ] **Step 6: In `serve.go`:** `startPriorities(ctx, srcs, overviewRead, interval) (read func(ctx) ([]priorities.Repo, error), stopped func())` — de build roept `overviewRead(ctx)`, geeft zijn fout door (502) en roept `priorities.Build(ctx, srcs, ov.Statuses)`; zonder bronnen: bouwt per lezing en geeft `ErrNotConfigured`, start niets. `serve(...)` krijgt `prioInterval`; de aanroep in het commando geeft `server.PrioritiesInterval`. Test: `TestStartPrioritiesWithoutReposReportsNotConfigured` en `TestStartPrioritiesFailedOverviewFailsBuild`.
- [ ] **Step 7: Run** `go test ./internal/... -race` — groen.
- [ ] **Step 8: Commit** `feat(server): eigen Refresher voor de prioriteringsbouw (#374)`.

### Task 5: Endpoint `GET /api/priorities`, OpenAPI en client

**Files:**
- Modify: `internal/server/server.go` (`Deps`), `internal/server/forge.go` (of nieuw `priorities.go`), `internal/cli/serve.go`, `web/openapi.json`, `web/src/api/lighthouse.ts` (gegenereerd), `web/src/lib/api.ts`, `web/src/lib/queries.ts`
- Test: `internal/server/forge_test.go` (of `priorities_test.go`), `internal/server/openapi_snapshot_test.go`, `web/src/lib/api.test.ts`, `web/src/lib/queries.test.tsx`

**Interfaces:**
- Consumes: `priorities.Repo`, `server.Refresher`.
- Produces:
  - `Deps.Priorities func(context.Context) ([]priorities.Repo, error)`; `registerPriorities(api huma.API, read func(context.Context) ([]priorities.Repo, error), runs func(context.Context) ([]dispatch.RunView, []dispatch.DeferredView, error))` met operation-id `get-priorities`; antwoordlichaam `{ "repos": [...], "runs": [...] }` (JSON-namen snake_case, zoals de andere endpoints).
  - TS: `fetchPriorities(signal)` in `api.ts`; `usePriorities()` in `queries.ts` met `FORGE_INTERVAL_MS`; `queryKeys.priorities`.

- [ ] **Step 1: Failing tests**: `TestPrioritiesReturnsReposAndRuns` (JSON-vorm met beide sleutels, lege lijsten als `[]`), `TestPrioritiesNotConfiguredIs503`, `TestPrioritiesBuildFailureIs502AndOthersStillServe` (`/api/questions` blijft 200 terwijl `/api/priorities` 502 geeft), `TestPrioritiesRunsComeFromRunsDep` (`Repo`, `Issue`, `State`, `Phase` van elke `RunView`).
- [ ] **Step 2: Run** `go test ./internal/server` — FAIL.
- [ ] **Step 3: Implementeer `registerPriorities`** en hang het in `server.New` aan `d.Priorities` en `d.Runs`; vul `Deps.Priorities` in `serve.go`.
- [ ] **Step 4: Regenereer de snapshot**: `go test ./internal/server -run TestOpenAPISnapshot -update`, daarna `cd web && npm ci && npm run gen`. Commit beide mee.
- [ ] **Step 5: Failing tests (vitest)**: `fetchPriorities` geeft de data bij 200 en een `ApiError` met `status` en `detail` bij 502; `usePriorities` heeft `refetchInterval === FORGE_INTERVAL_MS`. Implementeer in `api.ts` en `queries.ts` naar het patroon van `fetchRuns`/`useRuns`.
- [ ] **Step 6: Run** `go test ./internal/server -race` en `cd web && npm run lint && npm run typecheck && npm test` — groen.
- [ ] **Step 7: Commit** `feat(server): GET /api/priorities met client (#374)`.

### Task 6: Pure afleidingen in de SPA

**Files:**
- Create: `web/src/lib/priorities/derive.ts`, `web/src/lib/priorities/derive.test.ts`, `web/src/lib/priorities/palette.ts`, `web/src/lib/priorities/palette.test.ts`
- Test: beide `.test.ts`

**Interfaces:**
- Consumes: de gegenereerde types uit Taak 5 (hieronder `Repo`, `Issue`, `Run`).
- Produces (`derive.ts`):
  - `type NodeKey = string` — `"<repo>#<nr>"`.
  - `buildGraph(repos: Repo[], runs: Run[]): Graph` met `Graph = { nodes: Map<NodeKey, GNode>; edges: Array<{ from: NodeKey; to: NodeKey }> }`; `GNode = { key; repo; number; title; url; milestone: MilestoneRef | null; external: boolean; active: boolean; uphill: boolean; depth: number; waiting: number; cycle: boolean; component: number }`. Alleen **open** relaties; een blocker buiten de gelezen repo's wordt `external: true` zonder titel. `from` is de blocker, `to` het geblokkeerde issue. Berekend over **alle** gelezen repo's, nooit gefilterd.
  - `isActive(issue, runs)`, `isUphill(run | undefined)` volgens de spec: actief = run `open`/`parked` of status `In progress`; bergop = fase in `brainstorming|start-issue|spec-review|writing-plans|plan-review`; actief zonder run of met onbekende fase = bergaf.
  - `milestoneRows(repos, graph): MilestoneRow[]` met `MilestoneRow = { repo; number; title; done: number; active: GNode[]; todo: GNode[]; open: number; total: number; percent: number | null; shaping: boolean; empty: boolean; outside: GNode[]; order: GNode[] }`; `percent` is `null` bij `empty`.
  - `isShaping(description: string, issueCount: number): boolean`.
  - `withoutMilestone(repos, graph): UnassignedRow[]` met `{ node: GNode; closedMilestone: string | null; relation: 'blocks' | 'waits' | null; relatedNumber?: number }`.
  - `sortMilestoneRows(rows)`.
- Produces (`palette.ts`): `const PALETTE: readonly string[]` (acht CSS-variabelen/hex, geen geel/grijs/navy); `assignColors(milestones: Array<{repo: string; number: number}>): Map<string, string>` met sleutel `"<repo>#m<nr>"`; `NO_MILESTONE_COLOR`.

- [ ] **Step 1: Failing tests voor de graaf** (`derive.test.ts`), elk met expliciete invoer en asserties:
  - `chain A→B→C`: `depth` A 0, B 1, C 2; `waiting` A 2, B 1, C 0.
  - `diamond A→B, A→C, B→D, C→D`: `waiting(A) === 3` (D telt één keer); `depth(D) === 2`.
  - `cycle A→B→A`, plus `C` achter B: de hele test eindigt (vitest-timeout 1 s); A en B `cycle: true`, zelfde `component`, zelfde `depth`; `waiting(A) === 1` en `waiting(B) === 1`: A en B tellen elkaar niet mee (eigen component), C wel.
  - `cross-repo cycle` (`a/x#1` ↔ `b/y#1`): zelfde uitkomst, geen hang.
  - `external blocker` (`z/q#9` niet in repos): node bestaat met `external: true`, `depth` van het geblokkeerde issue ≥ 1.
  - `closed blockers are not edges` (de server stuurt ze niet; de test bevestigt dat een issue zonder `blocked_by` `depth 0` heeft).
- [ ] **Step 2: Failing tests voor `isActive`/`isUphill`**: run `parked` met fase `executing-plans` ⇒ actief, bergaf; status `In progress` zonder run ⇒ actief, bergaf; run met fase `"onbekend"` ⇒ bergaf; fase `spec-review` ⇒ bergop; open issue zonder run en status `To do` ⇒ niet actief.
- [ ] **Step 3: Failing tests voor `isShaping`**: `"Status: shaping"`, `"  status: SHAPING  "` en `"intro\nStatus: shaping\nrest"` ⇒ true; `"Status: shaping up"` en `"status: shaped"` ⇒ false; lege beschrijving met `issueCount 0` ⇒ true, met `issueCount 3` ⇒ false.
- [ ] **Step 4: Failing tests voor `milestoneRows`**: `percent` is `done / (done + open)`; `open` is het aantal geleverde open issues van de milestone (`Planning.Issues`), `done` komt uit de zoektelling; de zoektelling `Open` wordt niet voor de weergave gebruikt; `outside` bevat open issues buiten de milestone die er transitief achter blokkeren (twee lagen diep) en niet de eigen issues; `order` is topologisch, cyclus bij elkaar op nummer; milestone met `Open:0, Done:0` ⇒ `empty`, `percent === null`, `shaping === true`; milestone met alleen not-planned (dus `Open:0, Done:0`) idem.
- [ ] **Step 5: Failing tests voor `sortMilestoneRows`**: minste `open` bovenaan; gelijkstand ⇒ hoogste `percent` eerst; `empty` onderaan.
- [ ] **Step 6: Failing tests voor `withoutMilestone`**: open issue in gesloten milestone ⇒ `closedMilestone` is de titel; sortering: eerst issues met relatie, dan nummer aflopend.
- [ ] **Step 7: Failing tests voor `assignColors`**: kleuren op volgorde van (repo, nummer), niet op invoervolgorde; negen milestones ⇒ de negende heeft de kleur van de eerste; `NO_MILESTONE_COLOR` is nooit een element van `PALETTE`; test dat `PALETTE.length === 8` en dat geen element gelijk is aan `--lh-yellow`, `--lh-grey`, `--lh-navy`.
- [ ] **Step 8: Run** `cd web && npm test -- derive palette` — FAIL.
- [ ] **Step 9: Implementeer** `derive.ts` en `palette.ts`. Cycli: sterk samenhangende componenten (iteratief, geen recursie die op diepte overloopt); diepte = langste pad in het gecondenseerde DAG; wachtenden = aantal verschillende open issues in stroomafwaartse componenten, zonder zichzelf en zijn eigen component.
- [ ] **Step 10: Valideer het palet** met de `dataviz`-validator tegen licht en donker; noteer de uitkomst in een commentaar boven `PALETTE`. Ontbreekt de validator, noteer dan "niet gevalideerd" in dat commentaar en in de PR.
- [ ] **Step 11: Run** `cd web && npm run lint && npm run typecheck && npm test` — groen.
- [ ] **Step 12: Commit** `feat(web): afleidingen voor het prioritering-tabblad (#374)`.

### Task 7: Tabbladen, toolbar en filter

**Files:**
- Create: `web/src/components/Tabs.tsx`, `web/src/components/priorities/PrioritiesTab.tsx`, `web/src/components/priorities/Toolbar.tsx`, `web/src/lib/priorities/prefs.ts`, bijbehorende `.test.tsx`/`.test.ts`
- Modify: `web/src/App.tsx`, `web/src/App.test.tsx`

**Interfaces:**
- Consumes: `usePriorities`, `useRuns` (Taak 5), `buildGraph`, `assignColors` (Taak 6).
- Produces:
  - `Tabs` met `type TabId = 'controller' | 'prioritering'`; `useHashTab(): [TabId, (t: TabId) => void]` (leest `location.hash`, `#prioritering`; onbekende hash ⇒ `controller`; luistert op `hashchange`).
  - `prefs.ts`: `loadPrefs(): { view: ViewId; repos: string[] | null }`, `savePrefs(p): void` — alles in try/catch; `type ViewId = 'deps' | 'milestones' | 'unassigned'`.
  - `PrioritiesTab` (breedte ~1180 px, niet `max-w-3xl`), met de segmentschakelaar (elk met aantal), repo-chips (standaard alle aan) en legenda; rendert `<DepsView/>`, `<MilestonesView/>`, `<UnassignedView/>` (Taken 8–10) met `{ graph, repos, selected: Set<string>, colors }`.

- [ ] **Step 1: Failing tests**: `TestHashSelectsTab` (`#prioritering` ⇒ tabblad Prioritering actief, klik op Controller zet de hash terug); `TestUnknownHashFallsBackToController`; `TestPrefsSurviveThrowingLocalStorage` (`Storage.prototype.getItem/setItem` gooien ⇒ geen exceptie, defaults, schakelaar werkt); `TestPrefsRoundTrip`; `TestFilterChipsDefaultAllOn`; `TestSwitcherShowsCounts` (aantallen: nodes in graaf, open milestones, issues zonder milestone); `TestErrorStateShowsForgeErrorTextAndControllerTabStillRenders` (`/api/priorities` 502 ⇒ `forgeErrorText` in de kaart, tabblad Controller toont zijn inhoud); `TestLoadingShowsLaden…` (zoals `Panel`: tekst `Loading…`).
- [ ] **Step 2: Run** `cd web && npm test -- Tabs PrioritiesTab prefs` — FAIL.
- [ ] **Step 3: Implementeer** `Tabs.tsx` (navy lijn met gele onderstreping voor het actieve tabblad, `role="tablist"`), `prefs.ts`, `Toolbar.tsx`, `PrioritiesTab.tsx`; `App.tsx` toont per tabblad zijn inhoud, de `Header` en `UsagePanel` blijven boven beide. Legenda: milestonekleuren uit `assignColors` over de milestones in beeld (met repo-prefix zodra >1 repo geselecteerd is) en het teken voor opgepakt. Lege staten: één zin in de kaart.
- [ ] **Step 4: Run** `cd web && npm run lint && npm run typecheck && npm test` — groen.
- [ ] **Step 5: Commit** `feat(web): tabbladen en toolbar voor Prioritering (#374)`.

### Task 8: Weergave Afhankelijkheden (graaf)

**Files:**
- Create: `web/src/components/priorities/DepsView.tsx`, `web/src/components/priorities/layout.ts`, `layout.test.ts`, `DepsView.test.tsx`
- Modify: `web/package.json`, `web/package-lock.json`

**Interfaces:**
- Consumes: `Graph`, `GNode` (Taak 6); props uit Taak 7.
- Produces: `layoutGraph(nodes: GNode[], edges: Edge[]): { nodes: Array<{ key: NodeKey; x: number; y: number; w: 210; h: 38 }>; edges: Array<{ from: NodeKey; to: NodeKey; points: Array<{ x: number; y: number }> }>; width: number; height: number }` — dagre, `rankdir: 'LR'`; `DepsView`.

- [ ] **Step 1: Installeer** `cd web && npm install --save-exact @dagrejs/dagre@3.1.3`; commit `package.json` en lockfile in deze taak.
- [ ] **Step 2: Failing tests `layout.test.ts`**: een keten A→B→C geeft strikt stijgende `x`; het resultaat voor een cyclus A↔B eindigt en geeft eindige coördinaten; geen twee nodes overlappen (`rect`-doorsnede leeg) in een diamant van vier nodes.
- [ ] **Step 3: Failing tests `DepsView.test.tsx`**: kolomkoppen `Can start now`, `Waits on 1 step`, `Waits on 2 steps`; node toont nummer vet en afgekapte titel, bij >1 geselecteerde repo `repo#nr`; actief ⇒ label `picked up` en gestippelde rand; wachtenden-pil alleen bij `waiting >= 2`; **blocker in een weggefilterde repo**: de node blijft, grijs en gestippeld, en zijn geblokkeerde issue blijft in de kolom die de volledige berekening gaf (filter wijzigt geen kolom); hover op een node dimt alle nodes buiten zijn keten (`data-dimmed="true"`); klik opent `node.url` (`<a href>`); onder de graaf `No open dependencies (N)` met één pil per losse issue en de titel als `title`; geen relaties ⇒ één zin, geen lege `<svg>`; de container scrollt horizontaal (`overflow-x-auto`), niet de pagina; cyclus-nodes dragen de markering `cycle`.
- [ ] **Step 4: Run** `cd web && npm test -- layout DepsView` — FAIL.
- [ ] **Step 5: Implementeer** `layout.ts` en `DepsView.tsx`: kaartjes en bochtige pijlen met pijlpunt in SVG; touch ⇒ eerste tik licht de keten op, tweede opent het issue. Kleuren via `assignColors` over de nodes in beeld.
- [ ] **Step 6: Run** `cd web && npm run lint && npm run typecheck && npm test && npm run build` — groen.
- [ ] **Step 7: Commit** `feat(web): afhankelijkhedengraaf voor Prioritering (#374)`.

### Task 9: Weergave Milestones

**Files:**
- Create: `web/src/components/priorities/MilestonesView.tsx`, `MilestonesView.test.tsx`

**Interfaces:**
- Consumes: `milestoneRows`, `sortMilestoneRows`, `MilestoneRow` (Taak 6); kleuren uit Taak 7.

- [ ] **Step 1: Failing tests**: rij toont `N done · N active · N not yet`, met `· N outside blocker(s)` alleen als er zijn; balk heeft per issue (open plus gesloten) één even breed segment, in de volgorde klaar, actief, nog niet; actief bergop heeft de arceer-klasse (`data-hatched="true"`), bergaf niet; een open segment toont bij hover nummer en titel, een klaar-segment alleen `done`; blockers van buiten staan als gestippelde blokjes in een vaste kolom vóór de balk (alle balken starten op dezelfde `x`: test op gelijke `offsetLeft`-vervanger, bijv. dezelfde grid-kolom-klasse); onder de balk `to do:` met de open issues in `order`, zonder de blockers van buiten; rechts percentage en `N to do`, N alleen eigen open issues; **shaping zonder issues**: gearceerde grijze balk met `Not shaped yet — the percentage says nothing`, rechts `—` en `shape first`; **shaping met issues**: percentage grijs met `provisional` en een gestippelde laag; milestone met alleen PR's (`Open:0, Done:0`) is een lege milestone en toont `—`, geen `100%`; sortering volgens `sortMilestoneRows`; alleen rijen van geselecteerde repo's, kleuren alleen voor getoonde rijen.
- [ ] **Step 2: Run** `cd web && npm test -- MilestonesView` — FAIL.
- [ ] **Step 3: Implementeer `MilestonesView`.**
- [ ] **Step 4: Run** `cd web && npm run lint && npm run typecheck && npm test` — groen.
- [ ] **Step 5: Commit** `feat(web): milestone-weergave voor Prioritering (#374)`.

### Task 10: Weergave Zonder milestone

**Files:**
- Create: `web/src/components/priorities/UnassignedView.tsx`, `UnassignedView.test.tsx`

**Interfaces:**
- Consumes: `withoutMilestone`, `UnassignedRow` (Taak 6).

- [ ] **Step 1: Failing tests**: één regel per open issue zonder milestone met nummer (link naar de forge) en titel; relatiepil `blocks #128` / `waits on #44`; een open issue in een gesloten milestone staat erbij met de milestonetitel en het woord `closed`; volgorde: eerst issues met een relatie, dan nummer aflopend; filter op repo verbergt alleen; geen zulke issues ⇒ één zin, geen lege lijst; geen sleepvakken, geen suggesties.
- [ ] **Step 2: Run** `cd web && npm test -- UnassignedView` — FAIL.
- [ ] **Step 3: Implementeer `UnassignedView`** over de volle breedte.
- [ ] **Step 4: Run** `cd web && npm run lint && npm run typecheck && npm test` — groen.
- [ ] **Step 5: Commit** `feat(web): weergave zonder milestone (#374)`.

### Task 11: Afronding

**Files:**
- Modify: `web/e2e/fake-github.ts`, `web/e2e/ui.e2e.ts` (alleen als het e2e-pad het bestaande patroon volgt), `docs/priorities.md`
- Create: `changelog.d/feat-prioritering-tabblad.md`

- [ ] **Step 1: Changelog-fragment** `changelog.d/feat-prioritering-tabblad.md` met frontmatter `type: added` en één zin in het Engels: `lighthouse serve` shows a Prioritering tab with a dependency graph, milestone progress and issues without a milestone.
- [ ] **Step 2: Doc** `docs/priorities.md`: wat het tabblad toont, de afgeleide begrippen in één alinea elk (actief, bergop/bergaf, wachtenden, shaping met de regel `Status: shaping` in de milestonebeschrijving), het 300 s-interval en de puntenberekening uit Taak 1.
- [ ] **Step 3: e2e** (alleen als `fake-github.ts` een GraphQL-route heeft die uitgebreid kan worden zonder de bestaande tests te raken): één test die `#prioritering` opent en een node uit de fake ziet. Anders: laat dit weg en zeg het in de PR.
- [ ] **Step 4: Volledige controle**: `go test ./... -race`, `golangci-lint run`, `cd web && npm run lint && npm run typecheck && npm test && npm run build`, `go run ./cmd/lighthouse init --upgrade` (verwacht `No changes.`).
- [ ] **Step 5: Commit** `docs(priorities): tabblad, afgeleide begrippen en puntenbudget (#374)`.

---

## Self-review

- **Spec-dekking:** data (Taak 1–2), board-status zonder tweede lezing (Taak 3–4), eigen refresher en interval (Taak 4), endpoint/OpenAPI/client (Taak 5), afgeleide begrippen en palet (Taak 6), tabs/hash/localStorage/filter/legenda/foutstanden (Taak 7), drie weergaven (Taak 8–10), docs en changelog (Taak 11). Niet-doelen (schrijven, suggesties, gesloten issues in de graaf) hebben bewust geen taak.
- **Open na review r3-f1:** opgelost in "Besluiten van dit plan" en Taak 3–4; een repo met bord maar zonder Controller-optie krijgt nu één bordlezing per overview-bouw, en een mislukte overview-bouw laat de prioriteringsbouw falen. Dat is een afweging die de PR-review mag herzien.
- **Typeconsistentie:** `Planning`/`PlanIssue`/`MilestoneCount` (Taak 1) → `priorities.Repo`/`Issue`/`Milestone` (Taak 4) → JSON → TS `Repo`/`Issue` (Taak 5) → `Graph`/`GNode`/`MilestoneRow` (Taak 6) → views (Taak 7–10). `Refresher[T].Latest` vervangt `Refresher.Overview` in alle aanroepers in Taak 4.
- **Proportie:** de taken noemen signaturen, testnamen en asserties; lichamen zijn aan de bouwer, behalve het cyclus-algoritme waar de spec een keuze dwingt.
