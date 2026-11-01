# Lopende runs in de UI en de overstap (#118) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `GET /api/runs` en een UI-kaart tonen de lopende en geparkeerde runs en de uitgestelde starts van alle repo's uit `host.toml [dispatch]`; daarna de end-to-end-proef en de overstap van paia naar `lighthouse tick`.

**Architecture:** De tick schrijft elke tick per repo een rij `tick_state` van soort `run` per open of geparkeerde run: issue, titel, commando, trigger, start, profiel, workspace en de gemeten stand. `serve` leest `/api/runs` alleen uit `tick_state` (soorten `run` en `deferred`), zoals de spec voorschrijft (§1, r1-f11). `serve` bedient met `[dispatch] repos` al die repo's: elk item van `/api/questions`, `/api/returned` en `/api/runs` krijgt `repo`. De overstap (taken 7 en 8) is handwerk met bewijs in het issue, en een PR in paia.

**Tech Stack:** Go (`internal/runs`, `internal/workplace`, `internal/dispatch`, `internal/overview`, `internal/server`, `internal/cli`), React/Vite in `web/` (TanStack Query, Vitest, Playwright), gegenereerde client uit `web/openapi.json`.

**Spec:** [2026-10-27-m2-dispatcher-design.md](../specs/2026-10-27-m2-dispatcher-design.md), secties *1. Database* (tickstand), *12. Zicht*, *13. Overstap*, *Indeling in issues* punt 8.

## Global Constraints

- Code, comments, UI-teksten en bestandsnamen Engels; plan, docs, commits en changelog Nederlands.
- `/api/runs` is alleen lezen (Huma, `huma.Get`), OperationID `get-runs`; een lege lijst is `[]`, nooit `null`.
- `serve` leest `/api/runs` uit de tickstand, niet uit de werkplek of de forge (spec §1).
- Met `[dispatch] repos` leest `serve` die repo's en hun `[forge.board]`; zonder `[dispatch]` het gedrag van M1: één repo uit `--repo` (spec §12).
- Geen nieuwe tabel en geen migratie: soort `run` staat al in de CHECK van `tick_state` (`0002_runs.sql`, `0003_cleanup.sql`).
- Geen tweede database, lighthouse merget nooit (No-Gos van M2).
- Stap 3 van de overstap gebruikt de stop op nieuwe starts, niet de pauzevlag (spec §13, r2-f3).
- Na elke wijziging aan een route: `go test ./internal/server -run TestOpenAPISnapshot -update` en `cd web && npm run gen`; de gate controleert drift.
- Elke PR voegt `changelog.d/issue-118-lopende-runs.md` toe.
- Verificatie per taak: `go test -race` op de geraakte pakketten, of `cd web && npm run lint && npm run typecheck && npm test`; aan het eind `[test_gate] command` uit `lighthouse.toml`.

## Besluiten die dit plan neemt

- **De tick schrijft de hele regel, `serve` leest alleen.** Een rij van soort `run` heeft key = issuenummer en als value de JSON van `dispatch.RunView`. De tick vervangt alle `run`-rijen van een repo in één transactie, zoals `SaveDeferred`. Hij schrijft ze na `measuredPre` in `gatherRepo`, dus elke tick, ook als de repo niet aan de beurt is. De stand loopt zo hoogstens één tick achter, en de UI toont `at`.
- **Geparkeerd** = een controller-marker waarvan de laatste run van de sessie `runs.Resumable` is: de definitie die `readHostFacts` al telt. `readHostFacts` onthoudt daarvan per repo de run-id's in `hostFacts.Parked`.
- **Commando en trigger** komen uit een nieuwe smalle query `runs.Brief`. `OpenRun` en `Row` blijven ongewijzigd: `OpenRun` staat in de decide-fixtures.
- **De titel** komt uit `Pre.Items` (het board). Een run waarvan het issue niet meer op het board staat, krijgt titel `""`.
- **Stand** is tekst: `working`, `waiting`, `done`, of `""` als de run niet meetbaar was (geen workspace, geen sessiesleutel, meetfout).
- **Uitgestelde starts** krijgen in hun value ook `title`; `SaveDeferred` krijgt daarvoor een `titles map[int]string`.
- **Weggevallen repo's:** `serve` filtert de tickstand op de repo's die hij bedient. Rijen van een repo die uit `host.toml` is gehaald, blijven staan maar verschijnen niet.
- **`New` krijgt een `Deps`-struct.** De aanroep groeit met `/api/runs` en de bronnen per repo; positionele parameters worden onleesbaar. Alle test-aanroepen gaan mee.
- **Meerdere repo's in het overzicht:** de volgorde van `host.toml`, en binnen een repo de oudste kopcomment eerst, zoals nu. Mislukt één repo, dan mislukt het hele overzicht: een halve lijst verbergt issues, net als in `overview.Build`. Met `[dispatch]` negeert `serve` `--repo`; staat die repo niet in `[dispatch]`, dan schrijft hij één regel `lighthouse: --repo <slug> ignored: serving the repos of [dispatch]`.
- **`lighthouse run`** schrijft geen `run`-rijen; de volgende tick doet dat.

## Review Focus

1. **Een open run zonder workspace** (een onzekere start, nog niet geadopteerd) staat in de lijst met stand `""` en valt er niet uit → `TestRunViewsUnmeasured` (taak 2).
2. **Een geparkeerde run en een nieuwe open run van hetzelfde issue** (hervat, marker nog niet weg): het issue verschijnt één keer, als `open` → `TestRunViewsOpenWinsOverParked` (taak 2).
3. **Een tick die een repo overslaat** (laadfout) laat de oude rijen staan; een repo zonder runs meer moet ze juist wissen → `TestSaveRunViewEmptyClears` (taak 2).
4. **`serve` zonder `[dispatch]` en zonder `--repo`**: `/api/runs` geeft `200` met twee lege lijsten, de forge-routes `503` zoals nu → `TestServeRunsWithoutDispatch` (taak 5).
5. **Een repo in `[dispatch]` die niet laadt**: één regel op stderr; de andere repo's blijven zichtbaar in alle drie de routes → `TestForgeReposSkipsBrokenRepo` (taak 5).

---

### Task 1: Korte rijen en stand als tekst

**Files:**
- Create: `internal/runs/brief.go`
- Test: `internal/runs/brief_test.go`
- Modify: `internal/workplace/workplace.go` (na `type Stand`)
- Test: `internal/workplace/workplace_test.go`

**Interfaces:**
- Produces:
  - `type runs.Brief struct { ID int64; Repo string; Issue int; Command, Trigger, Profile, WorkspaceID string; StartedAt time.Time }`
  - `func runs.OpenBriefs(ctx context.Context, db *sql.DB, repo string) ([]Brief, error)`: open runs van repo, op id.
  - `func runs.BriefsByID(ctx context.Context, db *sql.DB, ids []int64) ([]Brief, error)`: in de volgorde van ids; een onbekende id slaat hij over.
  - `func (s workplace.Stand) String() string`: `"working"`, `"waiting"`, `"done"`.

- [ ] **Step 1: Schrijf de falende tests**

`TestOpenBriefs`: claim via de bestaande testhelper twee runs in `o/a` (issue 3 met `command: "/lighthouse-start-issue 3"`, trigger `handoff`, profiel `software`, workspace `ws-3`) en één in `o/b`, sluit er geen. Verwacht voor `o/a` precies twee Briefs, op id, met die velden en `StartedAt` gelijk aan wat de claim schreef. `TestBriefsByID`: een gesloten run (outcome `parked`) is vindbaar op id; `[]int64{id, 9999}` geeft één Brief. `TestStandString`: `Working.String() == "working"`, `Waiting` → `"waiting"`, `Done` → `"done"`.

- [ ] **Step 2: Draai ze en zie ze falen**

Run: `go test -race ./internal/runs ./internal/workplace -run 'Brief|StandString'`
Expected: FAIL, `undefined: OpenBriefs`, `undefined: BriefsByID`, `s.String undefined`

- [ ] **Step 3: Implementeer**

Eén `briefColumns`-constante (`id, repo, issue, command, trigger, profile, workspace_id, started_at`) en één scanfunctie, zoals `openColumns`/`scanOpen`; `started_at` parsen zoals `scanOpen` dat doet. `BriefsByID` met `WHERE id IN (?,…)` en daarna in de volgorde van ids zetten.

- [ ] **Step 4: Draai de tests**

Run: `go test -race ./internal/runs ./internal/workplace`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/runs/brief.go internal/runs/brief_test.go internal/workplace/workplace.go internal/workplace/workplace_test.go
git commit -m "feat(runs): korte rij met commando en trigger, stand als tekst (#118)"
```

### Task 2: De tickstand van de runs schrijven en lezen

**Files:**
- Create: `internal/dispatch/view.go`
- Test: `internal/dispatch/view_test.go`
- Modify: `internal/dispatch/precheck.go:81-117` (`SaveDeferred`)
- Modify: `internal/dispatch/precheck_test.go` (aanroepen van `SaveDeferred`)

**Interfaces:**
- Consumes: `runs.Brief`, `workplace.Stand.String` (taak 1); `dispatch.Pre` (`Items`, `OpenRuns`, `Stands`).
- Produces:
  - ```go
    type RunView struct {
        Repo        string    `json:"repo"`
        Issue       int       `json:"issue"`
        Title       string    `json:"title"`
        State       string    `json:"state"` // "open" or "parked"
        Command     string    `json:"command"`
        Trigger     string    `json:"trigger"`
        Profile     string    `json:"profile"`
        WorkspaceID string    `json:"workspace_id"`
        StartedAt   time.Time `json:"started_at"`
        Stand       string    `json:"stand"` // "working", "waiting", "done"; "" not measured
        At          time.Time `json:"at"`    // the tick that wrote it
    }
    type DeferredView struct {
        Repo   string    `json:"repo"`
        Issue  int       `json:"issue"`
        Title  string    `json:"title"`
        Reason string    `json:"reason"`
        At     time.Time `json:"at"`
    }
    ```
  - `func RunViews(pre Pre, open, parked []runs.Brief) []RunView`: puur; `State` `open` voor open, `parked` voor parked, op issue gesorteerd; een issue dat in beide staat alleen als `open`. `Stand` uit `pre.Stands[id]`, alleen als die sleutel bestaat. `Title` uit `pre.Items`. `At` blijft nul; `SaveRunView` zet hem.
  - `func SaveRunView(ctx context.Context, db *sql.DB, repo string, views []RunView, now time.Time) error`: vervangt de `run`-rijen van repo in één transactie. Key is het issuenummer, `At` is `now` in UTC.
  - `func LoadView(ctx context.Context, db *sql.DB, repos []string) ([]RunView, []DeferredView, error)`: leest `run` en `deferred` voor die repo's, in de volgorde van repos en dan op issue. Nooit `nil`. Een value die niet parst, geeft een fout met repo en key.
  - `SaveDeferred(ctx, db, repo string, defers []Action, titles map[int]string, now time.Time) error`: de value krijgt `"title"`.

- [ ] **Step 1: Schrijf de falende tests**

- `TestRunViews`: een Pre met Items `{3, "Drie"}` en `{5, "Vijf"}` en `Stands{10: workplace.Waiting}`. Open is `[{ID: 10, Issue: 3, …}]`, parked is `[{ID: 7, Issue: 5, …}]`. Verwacht twee views: issue 3 `open`, titel `Drie`, stand `waiting`; issue 5 `parked`, titel `Vijf`, stand `""`.
- `TestRunViewsUnmeasured`: een open run zonder sleutel in `Stands` geeft stand `""` en staat in de lijst.
- `TestRunViewsOpenWinsOverParked`: open en parked met issue 3 geven één view, `State == "open"`.
- `TestSaveRunViewRoundTrip`: `SaveRunView` voor `o/a` en `o/b`, dan `LoadView(ctx, db, []string{"o/b", "o/a"})`. Verwacht eerst `o/b`, dan `o/a`, `Repo` gevuld en `At == now.UTC()`. `LoadView` met `[]string{"o/a"}` laat `o/b` weg.
- `TestSaveRunViewEmptyClears`: eerst één view, dan `SaveRunView(…, nil, …)`, dan geeft `LoadView` een lege, niet-nil slice.
- `TestLoadViewDeferred`: `SaveDeferred` met een `KindDefer`-actie voor issue 4, reden `"slots full"` en `titles{4: "Vier"}` geeft één `DeferredView{Repo, 4, "Vier", "slots full", now}`.
- `TestLoadViewBadValue`: een met de hand ingevoegde `run`-rij met value `"{"` geeft een fout die `o/a` en de key noemt.

- [ ] **Step 2: Draai ze en zie ze falen**

Run: `go test -race ./internal/dispatch -run 'RunView|LoadView|SaveRunView'`
Expected: FAIL, `undefined: RunViews`

- [ ] **Step 3: Implementeer `view.go` en pas `SaveDeferred` aan**

`SaveRunView` volgt de vorm van `SaveDeferred` (DELETE van de soort, dan INSERT per view). De `precheck_test.go`-aanroepen krijgen `nil` als titles.

- [ ] **Step 4: Draai de tests**

Run: `go test -race ./internal/dispatch`
Expected: PASS (ook de bestaande fixtures)

- [ ] **Step 5: Commit**

```bash
git add internal/dispatch/view.go internal/dispatch/view_test.go internal/dispatch/precheck.go internal/dispatch/precheck_test.go
git commit -m "feat(dispatch): tickstand van open en geparkeerde runs, titel bij uitgestelde starts (#118)"
```

### Task 3: De tick schrijft de tickstand

**Files:**
- Modify: `internal/cli/brake.go:18-117` (`hostFacts`, `readHostFacts`)
- Modify: `internal/cli/tick.go` (`gatherRepo` na `measuredPre`; `decideAndApply` bij `SaveDeferred`)
- Test: `internal/cli/tick_test.go` of `e2e/tick_test.go`; zoek de bestaande test die een tick met een open run draait en volg die.

**Interfaces:**
- Consumes: `runs.OpenBriefs`, `runs.BriefsByID` (taak 1); `dispatch.RunViews`, `dispatch.SaveRunView`, de nieuwe `SaveDeferred` (taak 2).
- Produces: `hostFacts.Parked map[string][]int64`: per repo de id's van de laatste runs waarvoor `runs.Resumable(last, m.Key())` geldt.

- [ ] **Step 1: Schrijf de falende tests**

- `TestTickWritesRunView`: een tick met één open run met workspace en een gemeten sessie `waiting`. Verwacht daarna in `dispatch.LoadView` één view met de titel van het board-item, stand `waiting` en het commando van de run.
- `TestTickWritesParkedRunView`: een controller-marker waarvan de laatste run `parked` is. Verwacht één view met `State == "parked"`.
- `TestTickRunViewWhenNotDue`: een tweede tick zonder wijziging (niet aan de beurt) werkt `At` toch bij.
- `TestTickDeferredTitle`: een uitgestelde start (slots vol) heeft in `LoadView` de titel van het board-item.

- [ ] **Step 2: Draai ze en zie ze falen**

Run: `go test -race ./internal/cli -run 'RunView|DeferredTitle'`
Expected: FAIL, lege `LoadView`

- [ ] **Step 3: Implementeer**

`readHostFacts` vult `f.Parked[repo] = append(…, last.ID)` in de bestaande `Resumable`-tak. `gatherRepo` roept na `measuredPre` en vóór `Due` `OpenBriefs`, `BriefsByID(facts.Parked[slug])`, `RunViews` en `SaveRunView` aan; een fout geeft hij terug, zoals de andere stappen daar. `decideAndApply` geeft `SaveDeferred` per repo de titels uit `g.in.Snapshot.Items`.

- [ ] **Step 4: Draai de tests**

Run: `go test -race ./internal/cli ./e2e`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/cli/brake.go internal/cli/tick.go internal/cli/*_test.go e2e/
git commit -m "feat(tick): schrijf de stand van open en geparkeerde runs elke tick (#118)"
```

### Task 4: Het overzicht over meerdere repo's

**Files:**
- Modify: `internal/overview/overview.go` (`Question`, `Returned`, nieuwe `RepoSource`, `BuildAll`)
- Test: `internal/overview/overview_test.go`

**Interfaces:**
- Produces:
  - `Question.Repo string \`json:"repo"\``, `Returned.Repo string \`json:"repo"\``
  - `type RepoSource struct { Slug string; Src Source; InController Filter }`
  - `func BuildAll(ctx context.Context, repos []RepoSource) (Overview, error)`: `Build` per repo, met `Repo` op elk item, in de volgorde van repos. De eerste fout geeft hij terug met de slug ervoor (`fmt.Errorf("%s: %w", slug, err)`, zodat `errors.Is(err, forge.ErrNotConfigured)` blijft werken). Zonder repos geeft hij `forge.ErrNotConfigured`.

- [ ] **Step 1: Schrijf de falende tests**

- `TestBuildAllSetsRepo`: twee stub-sources (`o/a` met een vraag op #1, `o/b` met een vraag op #2). Verwacht `Questions[0].Repo == "o/a"`, `Questions[1].Repo == "o/b"`.
- `TestBuildAllFailsWhole`: `o/b` faalt, dus `BuildAll` geeft een fout die `o/b` noemt en een leeg overzicht.
- `TestBuildAllNoRepos`: `errors.Is(err, forge.ErrNotConfigured)`.

- [ ] **Step 2: Draai ze en zie ze falen**

Run: `go test -race ./internal/overview -run BuildAll`
Expected: FAIL, `undefined: BuildAll`

- [ ] **Step 3: Implementeer `BuildAll`**

- [ ] **Step 4: Draai de tests**

Run: `go test -race ./internal/overview`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/overview/
git commit -m "feat(overview): vragen en teruggegeven issues over meerdere repo's, met repo per item (#118)"
```

### Task 5: `/api/runs` en `serve` voor alle repo's

**Files:**
- Modify: `internal/server/server.go` (`New` → `Deps`)
- Modify: `internal/server/forge.go` (`registerForge` op `BuildAll`; nieuwe `registerRuns`)
- Modify: `internal/server/forge_test.go`, `server_test.go`, `host_test.go`, `openapi_snapshot_test.go` (aanroepen van `New`; `/api/runs` in de routetabel en de lus over de API-paden)
- Modify: `internal/cli/serve.go` (`controllerFilter` → `forgeRepos`; `serve` krijgt `[]overview.RepoSource`)
- Modify: `internal/cli/serve_test.go`, `e2e/serve_test.go`
- Regenerate: `web/openapi.json`, `web/src/api/`

**Interfaces:**
- Consumes: `overview.RepoSource`, `overview.BuildAll` (taak 4); `dispatch.LoadView`, `RunView`, `DeferredView` (taak 2).
- Produces:
  - ```go
    type Deps struct {
        Report    func(context.Context) usage.Report
        Forge     []overview.RepoSource
        Leftovers func(context.Context) ([]workplace.Leftover, error)
        Runs      func(context.Context) ([]dispatch.RunView, []dispatch.DeferredView, error)
    }
    func New(site fs.FS, d Deps) http.Handler
    ```
  - `GET /api/runs` → `{"runs": [RunView…], "deferred": [DeferredView…]}`, OperationID `get-runs`, `500` bij een fout.
  - `func forgeRepos(hostToml, repo, forgeURL string, getenv func(string) string, errOut io.Writer) ([]overview.RepoSource, error)`: zonder `[dispatch] repos` één bron `{repo, newForge(repo…), nil}`. Met `[dispatch] repos` één bron per repo die laadt, met `ControllerIssues` als die repo een board met optie `Controller` heeft. Een repo die niet laadt, krijgt de bestaande `skip`-regel. Staat `--repo` niet in `[dispatch]`, dan volgt de regel uit *Besluiten*.
  - `serve(ctx, addr, dbPath string, repos []overview.RepoSource, errOut io.Writer) error`; `Runs` = `dispatch.LoadView(ctx, st.DB, slugs van repos)`.

- [ ] **Step 1: Schrijf de falende tests**

- `internal/server`: `TestRunsList` geeft met een stub-`Runs` `200` en beide lijsten. `TestRunsEmpty` geeft `{"runs":[],"deferred":[]}` letterlijk. `TestRunsError` geeft `500`. Zet `/api/runs` in de routetabel van `server_test.go`.
- `internal/cli`: `TestForgeReposSkipsBrokenRepo`: `host.toml` met twee repo's, waarvan één met een kapotte `lighthouse.toml`. Verwacht één bron en één `skip`-regel. `TestForgeReposWithoutDispatch`: één bron met slug `--repo`, filter nil. `TestForgeReposIgnoresRepoFlag`: de `ignored`-regel.
- `TestServeRunsWithoutDispatch`: `serve` zonder `host.toml` en zonder `--repo` geeft `/api/runs` `200` met lege lijsten en `/api/questions` `503`.
- `TestServeRunsFromTickState`: een database met een `SaveRunView`-rij voor de bediende repo geeft die run in `/api/runs`.

- [ ] **Step 2: Draai ze en zie ze falen**

Run: `go test -race ./internal/server ./internal/cli -run 'Runs|ForgeRepos'`
Expected: FAIL, compileerfouten op `Deps`/`forgeRepos`

- [ ] **Step 3: Implementeer, en zet alle aanroepen van `New` en `serve` om**

`registerRuns` volgt `registerCleanup`: nil wordt `[]`.

- [ ] **Step 4: Snapshot en client bijwerken**

Run: `go test ./internal/server -run TestOpenAPISnapshot -update && cd web && npm run gen`
Expected: `web/openapi.json` heeft `/api/runs`, `repo` op `Question` en `Returned`; `web/src/api/lighthouse.ts` heeft `getRuns`.

- [ ] **Step 5: Draai de tests**

Run: `go test -race ./... && cd web && npm run typecheck`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/server internal/cli e2e web/openapi.json web/src/api
git commit -m "feat(serve): GET /api/runs uit de tickstand en alle repo's uit [dispatch] (#118)"
```

### Task 6: De kaart *Running runs*, de repo bij elk item, docs en changelog

**Files:**
- Create: `web/src/components/RunsList.tsx`
- Modify: `web/src/lib/api.ts` (`fetchRuns`), `web/src/lib/queries.ts` (`queryKeys.runs`, `useRuns`), `web/src/App.tsx` (kaart na `UsagePanel`)
- Modify: `web/src/components/QuestionList.tsx`, `ReturnedList.tsx` (`repo#number` in plaats van `#number`)
- Modify: `web/src/components/lists.test.tsx`, `web/e2e/ui.e2e.ts`, `web/e2e/screenshot.mjs` (`TITLES` met `Running runs`)
- Modify: `docs/install.md` (de routes, `/api/runs`, meerdere repo's), `docs/tick.md` (de tickstand van de runs)
- Create: `changelog.d/issue-118-lopende-runs.md`

**Interfaces:**
- Consumes: `getRuns` uit de gegenereerde client (taak 5).
- Produces: `fetchRuns(signal?: AbortSignal): Promise<{ runs: RunView[]; deferred: DeferredView[] }>`; `useRuns()` met `refetchInterval: USAGE_INTERVAL_MS`. De tickstand is lokaal: geen forge-kosten.

- [ ] **Step 1: Schrijf de falende tests in `lists.test.tsx`**

- `RunsList` toont per run `o/a#3`, de titel, `open` of `parked`, de stand, het commando, de trigger, het profiel en de workspace, met regio-naam `Running runs`.
- Een run met stand `""` toont `not measured`.
- Een uitgestelde start toont `o/a#4`, de titel en de reden onder het kopje `Deferred starts`.
- Twee lege lijsten tonen `No running runs`.
- Een `500` toont een alert `Runs unavailable: <detail>`.
- `QuestionList` toont `o/a#7` in plaats van `#7`; pas de bestaande verwachtingen aan.

- [ ] **Step 2: Draai ze en zie ze falen**

Run: `cd web && npm test -- lists`
Expected: FAIL, `Cannot find module './RunsList'`

- [ ] **Step 3: Implementeer `RunsList.tsx` naar het patroon van `CleanupList.tsx`**

- [ ] **Step 4: Draai lint, types en tests, dan de e2e**

Run: `cd web && npm run lint && npm run typecheck && npm test && npm run e2e`
Expected: PASS

- [ ] **Step 5: Schermafdruk**

Draai `serve` tegen een database met een open run, een geparkeerde run en een uitgestelde start. Vul die met de helper uit de test van taak 5, of na een echte tick in taak 7. Run daarna `cd web && npm run screenshot -- http://127.0.0.1:8127 ../docs/images/ui-m2.png`.
Expected: `wrote ../docs/images/ui-m2.png`; de kaart staat erop. Die afbeelding is het bewijs voor AC 2.

- [ ] **Step 6: Docs en changelog**

`changelog.d/issue-118-lopende-runs.md` met `type: added`: `/api/runs` en de kaart *Running runs*, en `serve` over alle repo's uit `[dispatch]`, met `repo` op elk item.

- [ ] **Step 7: Commit**

```bash
git add web docs changelog.d/issue-118-lopende-runs.md
git commit -m "feat(web): kaart Running runs en de repo bij elk item (#118)"
```

### Task 7: End-to-end-proef op lighthouse (handwerk, bewijs in #118)

Geen code; vereist dat taken 1–6 gemerged zijn en een gebouwde binary op de host. Spec §13 stap 0 (de paia-PR uit *Launch*) moet staan: zonder die PR start lighthouse geen agent. Controleer dat eerst en stop met een vraag aan de gebruiker als die PR er niet is.

- [ ] **Step 1:** `host.toml [dispatch] repos` bevat alleen lighthouse (Software-profiel). De Python-controller bedient lighthouse niet (spec §13 stap 1).
- [ ] **Step 2:** Een testissue in Controller met een Handoff → de tick start een run: 🚀 op de Handoff, de run staat in `/api/runs` als `open`.
- [ ] **Step 3:** De run post een Vraag → de gebruiker antwoordt → de tick hervat (🤖 Hervat).
- [ ] **Step 4:** De run levert een groene PR → 🤖 Klaar en de nacontrole.
- [ ] **Step 5:** Twee runs tegelijk, dan een geforceerde usage-pauze (`lighthouse pause`, of de drempel tijdelijk laag). Beide verschijnen als `parked` in `/api/runs`; na het opheffen hervat de tick ze.
- [ ] **Step 6:** Plaats op #118 één comment met per stap de link naar de comment of PR, de tickregels uit het journal en een schermafdruk van de kaart met twee geparkeerde runs.

### Task 8: Overstap van paia (handwerk en een PR in paia)

Werk in `CorneeldH/personal_ai_agent`, niet in deze worktree; vraag de gebruiker vóór elke stap die de productie-controller raakt.

- [ ] **Step 1 (leeglopen):** zet in paia de stop op nieuwe starts aan, niet de pauzevlag (spec §13 stap 3). Wacht tot geen paia-issue meer een open, geparkeerde of vragende run van de Python-controller heeft. Een open Vraag beantwoordt de gebruiker of geeft hij zelf terug.
- [ ] **Step 2 (bewijs leeg):** leg de lege toestand van de Python-runstaat vast (het commando en de uitvoer) in een comment op #118.
- [ ] **Step 3 (paia-config):** paia's `lighthouse.toml` krijgt `[usage_guard] controller = true` en `[controller] profile`; voeg paia toe aan `host.toml [dispatch] repos`.
- [ ] **Step 4 (PR in paia):** `controller-tick.timer` roept `lighthouse tick` aan, `forge_agent.sh` leest de nieuwe drager, en `src/controller/` en `scripts/controller_{tick,launch,classify}.sh` gaan weg. `controller_{gather,apply,forge,state,triage,vraag,auto}.py` gaan alleen weg als ze geen afnemer meer hebben (spec §13 stap 4).
- [ ] **Step 5 (controle):** na de eerste tick in paia geen `🤖 Teruggegeven` met "runstaat kwijt" op een paia-issue; leg dat vast op #118 met een link naar de gemergde paia-PR.
