# Open questions en Returned uit één verversend overzicht — implementatieplan (#307)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `/api/questions` en `/api/returned` antwoorden uit een overzicht in het geheugen dat `serve` elke 2 minuten op de achtergrond ververst, met één `overview.BuildAll` voor beide.

**Architecture:** Een nieuw type `Refresher` in `internal/server` houdt het laatste resultaat van `BuildAll` vast (overzicht, fout) en ververst het in één goroutine (`Run`). `serve.go` start die met de context van serve en geeft de leesfunctie door via een nieuw veld `Deps.Overview`; `server.New` start niets. De handlers lezen alleen die functie. De pagina pollt elke minuut.

**Tech Stack:** Go (`internal/server`, `internal/cli`), React Query (`web/src/lib/queries.ts`), Vitest.

**Spec:** [`../specs/2026-11-13-issue-307-forge-blokken-cache-design.md`](../specs/2026-11-13-issue-307-forge-blokken-cache-design.md)

## Global Constraints

- Verversinterval **2 minuten**, gemeten van start tot start; nooit twee builds tegelijk; trager dan het interval → volgende build direct.
- Mislukte build: opnieuw na **20 s**. Time-out per build: **90 s**. Vaste waarden, geen configuratie.
- Fout van de laatste verversing gaat vóór een ouder, geslaagd overzicht: `forge.ErrNotConfigured` → 503, anders 502 (bestaande `forgeError`).
- Een request vóór de eerste build wacht op die build of op zijn eigen context.
- Zonder repo's: 503, geen goroutine die GitHub leest.
- `server.New` start geen goroutine.
- Pagina: `useQuestions` en `useReturned` pollen (`refetchInterval` én `staleTime`) elke **1 minuut** via een eigen constante naast `FORGE_INTERVAL_MS`; `useCleanup` blijft op `FORGE_INTERVAL_MS`.
- Geen wijziging aan tick, `dispatch.Gather`, `tick_state`, `/api/runs`, `/api/cleanup`, `internal/server/latest.go`; `web/openapi.json` en de gegenereerde client blijven gelijk.
- Code, comments en testnamen in het Engels; artefacten (docs, changelog, commits) in het Nederlands. Go volgt `/lighthouse-go-standard`; tests volgen `/lighthouse-test-standard`. Geen `time.Sleep` van minuten in tests.

## Review Focus

- Request vóór de eerste build klaar is: wacht, en geeft het resultaat; een geannuleerde request-context geeft zijn contextfout, geen lege lijst (Task 1).
- Build die na een succes faalt: beide endpoints 502 met de fout, niet de oude lijst; daarna een geslaagde build herstelt (Task 1).
- Build die hangt: time-out na 90 s laat de volgende verversing niet blokkeren, en een time-out telt als fout (Task 1).
- Een build die langer duurt dan het interval: geen overlap, volgende start direct (Task 1).
- Serve zonder repo's: 503 zonder goroutine; shutdown wacht op de goroutine (Task 3).

## Bestandsstructuur

- Create `internal/server/refresher.go` — `Refresher`, de drie constanten.
- Create `internal/server/refresher_test.go`.
- Modify `internal/server/server.go` — `Deps.Overview`.
- Modify `internal/server/forge.go` — `registerForge` leest `Deps.Overview`.
- Modify `internal/server/forge_test.go` — `testDeps` zet `Overview`; test voor "twee requests, één build".
- Modify `internal/cli/serve.go` — `startOverview`, gebruikt in `serve`.
- Modify `internal/cli/serve_test.go`.
- Modify `web/src/lib/queries.ts`, `web/src/lib/queries.test.tsx`.
- Modify `docs/install.md`; Create `changelog.d/issue-307-forge-blokken-cache.md`.

### Task 1: `Refresher`

**Files:**
- Create: `internal/server/refresher.go`
- Test: `internal/server/refresher_test.go`

**Interfaces:**
- Produces:
  ```go
  const (
      RefreshInterval = 2 * time.Minute  // start to start
      RefreshRetry    = 20 * time.Second // after a failed build
      BuildTimeout    = 90 * time.Second // per build
  )
  type BuildFunc func(ctx context.Context) (overview.Overview, error)
  type Refresher struct{ /* unexported */ }
  func NewRefresher(build BuildFunc, interval, retry, timeout time.Duration) *Refresher
  func (r *Refresher) Run(ctx context.Context)                                    // blocks until ctx is done
  func (r *Refresher) Overview(ctx context.Context) (overview.Overview, error)   // last result; waits for the first build
  ```
  Ook een unexporteerde `refresh(ctx context.Context)` die precies één build doet en het resultaat opslaat (de test roept die direct aan).

- [ ] **Step 1: Schrijf de falende tests** in `refresher_test.go`, met een tellende `BuildFunc` (atomic teller, instelbaar resultaat/fout):
  - `TestRefresherOverviewWaitsForFirstBuild`: build blokkeert op een kanaal; `Overview` in een goroutine geeft nog niets; na vrijgave komt het opgeslagen `Overview` terug en de build is één keer geroepen.
  - `TestRefresherOverviewHonoursContextBeforeFirstBuild`: nooit gestarte `Run`; `Overview` met een geannuleerde context geeft `context.Canceled`, geen lege `Overview` zonder fout.
  - `TestRefresherServesStoredResultWithoutBuilding`: na `refresh` roepen drie `Overview`-aanroepen de build niet opnieuw (teller blijft 1).
  - `TestRefresherFailureWinsOverOlderOverview`: `refresh` slaagt, dan faalt de build; `Overview` geeft de fout (`errors.Is`) en niet de oude lijst; daarna een geslaagde `refresh` geeft weer het overzicht.
  - `TestRefresherBuildTimeout`: build wacht op `ctx.Done()`; `NewRefresher(..., timeout: 20*time.Millisecond)`; `refresh` keert terug en `Overview` geeft een fout met `errors.Is(err, context.DeadlineExceeded)`.
  - `TestRefresherRunRebuildsOnInterval`: `Run` met interval 10 ms; wacht (kanaal, geen vaste sleep) tot de teller ≥ 3; cancel; `Run` keert terug (kanaal gesloten binnen 1 s).
  - `TestRefresherRunRetriesSoonerAfterFailure`: interval 1 h, retry 10 ms, build faalt eerst en slaagt daarna; binnen een deadline van 5 s is de tweede build gedraaid en geeft `Overview` het overzicht.
  - `TestRefresherNoOverlappingBuilds`: build duurt langer dan het interval (interval 5 ms, build 30 ms); een atomic "in flight"-teller komt nooit boven 1, en na afloop van een build start de volgende zonder extra wachttijd (≥ 2 builds binnen 1 s).

- [ ] **Step 2: Run** `go test ./internal/server -run Refresher -v` — verwacht FAIL (`undefined: NewRefresher`).

- [ ] **Step 3: Implementeer** `refresher.go`. `refresh` maakt `context.WithTimeout(ctx, timeout)`, roept build, slaat `(overview, err)` onder een mutex op en sluit bij de eerste keer een `ready`-kanaal (`sync.Once`). `Run` doet `refresh`, rekent het volgende starttijdstip uit als `start + interval` (of `start + retry` bij een fout) en wacht met een `time.Timer` tot dat tijdstip, of niet als het al voorbij is; stopt bij `ctx.Done()`. `Overview` doet `select { case <-ready: case <-ctx.Done(): return overview.Overview{}, ctx.Err() }` en geeft dan de opgeslagen waarden. Een build die onderbroken is doordat de context van `Run` eindigde slaat niets op.

- [ ] **Step 4: Run** `go test -race ./internal/server -run Refresher -v` — verwacht PASS.

- [ ] **Step 5: Commit**
  ```bash
  git add internal/server/refresher.go internal/server/refresher_test.go
  git commit -m "feat(server): Refresher houdt het forge-overzicht in het geheugen (#307)"
  ```

### Task 2: Handlers lezen `Deps.Overview`

**Files:**
- Modify: `internal/server/server.go` (`Deps`, `New`)
- Modify: `internal/server/forge.go` (`registerForge`)
- Modify: `internal/server/forge_test.go` (`testDeps`, nieuwe test)

**Interfaces:**
- Consumes: `BuildFunc`-vorm uit Task 1 (`func(context.Context) (overview.Overview, error)`).
- Produces: `Deps.Overview func(context.Context) (overview.Overview, error)`; `registerForge(api huma.API, read func(context.Context) (overview.Overview, error))`. `Deps.Forge` blijft bestaan voor `registerLatestComment`.

- [ ] **Step 1: Schrijf de falende test** in `forge_test.go`: `TestEndpointsShareOneBuild` — een `countingSource` (telt `OpenIssues`-aanroepen, levert `populated()`-inhoud), een `Refresher` met `overview.BuildAll` over die ene repo, `r.refresh(ctx)` één keer; `Deps.Overview = r.Overview`; vier requests (twee per endpoint) geven 200 met dezelfde inhoud als vandaag en de teller is 1. Voeg `TestEndpointFailureAfterSuccessIs502`: `Refresher` met een build die eerst slaagt, dan `errors.New("dial tcp: connection refused")` geeft; na tweede `refresh` geven beide routes 502 (en geen `questions`/`returned`-sleutel in de body).

- [ ] **Step 2: Run** `go test ./internal/server -run 'Endpoints|Endpoint' -v` — FAIL (`Deps` heeft geen veld `Overview`).

- [ ] **Step 3: Implementeer.** `Deps.Overview` toevoegen; `New` roept `registerForge(api, d.Overview)`; beide handlers vervangen `overview.BuildAll(ctx, repos)` door `read(ctx)`; `forgeError` blijft ongewijzigd. `testDeps` zet `Overview` op `func(ctx) { return overview.BuildAll(ctx, repos) }` over dezelfde `[]overview.RepoSource`, zodat de bestaande tests (`TestQuestionsEndpoint`, `TestReturnedEndpoint`, `TestEmptyListIsArray`, `TestEndpointNotConfigured`, `TestEndpointForgeDown`, snapshot) ongewijzigd blijven slagen.

- [ ] **Step 4: Run** `go test -race ./internal/server -v` — alles PASS, ook `openapi_snapshot_test.go` (de vorm van de responses is niet veranderd).

- [ ] **Step 5: Commit**
  ```bash
  git add internal/server/server.go internal/server/forge.go internal/server/forge_test.go
  git commit -m "feat(server): questions en returned lezen één gedeeld overzicht (#307)"
  ```

### Task 3: `serve` start de verversing

**Files:**
- Modify: `internal/cli/serve.go`
- Test: `internal/cli/serve_test.go`

**Interfaces:**
- Consumes: `server.NewRefresher`, `server.RefreshInterval`, `server.RefreshRetry`, `server.BuildTimeout`, `(*Refresher).Run/Overview`, `server.Deps.Overview`.
- Produces: `func startOverview(ctx context.Context, repos []overview.RepoSource) (read func(context.Context) (overview.Overview, error), stopped func())`. Zonder repo's: `read` roept `overview.BuildAll(ctx, repos)` (geeft `forge.ErrNotConfigured`), er start geen goroutine en `stopped` is een no-op. Met repo's: start `go r.Run(ctx)`; `stopped` blokkeert tot die goroutine terug is.

- [ ] **Step 1: Schrijf de falende tests** in `serve_test.go`:
  - `TestStartOverviewNoReposIsNotConfigured`: `read(ctx)` geeft `errors.Is(err, forge.ErrNotConfigured)`; `stopped()` keert direct terug.
  - `TestStartOverviewBuildsOnceAndStops`: één `overview.RepoSource` met een kleine tellende stub-`Source` (levert geen issues); `read(ctx)` geeft een leeg, niet-nil `Overview` zonder fout; na `cancel()` keert `stopped()` terug binnen 1 s.
  Bestaande `TestServe…`-tests (`serve(ctx, …, nil, …)`) blijven ongewijzigd slagen.

- [ ] **Step 2: Run** `go test ./internal/cli -run StartOverview -v` — FAIL (`undefined: startOverview`).

- [ ] **Step 3: Implementeer** `startOverview` en gebruik het in `serve` met de context van serve: `Overview: read` in `server.Deps`, en `stopped()` aanroepen na `srv.Shutdown`/vóór `st.Close()` in de afsluitvolgorde (de context is dan al geannuleerd door `ctx.Done()`; bij een `serveErr`-uitgang eerst een eigen `cancel` van een `context.WithCancel(ctx)` die `serve` voor de verversing aanmaakt).

- [ ] **Step 4: Run** `go test -race ./internal/cli -v` — PASS.

- [ ] **Step 5: Commit**
  ```bash
  git add internal/cli/serve.go internal/cli/serve_test.go
  git commit -m "feat(cli): serve ververst het forge-overzicht op de achtergrond (#307)"
  ```

### Task 4: Pagina pollt elke minuut

**Files:**
- Modify: `web/src/lib/queries.ts`
- Test: `web/src/lib/queries.test.tsx`

**Interfaces:**
- Produces: `export const QUESTIONS_INTERVAL_MS = 60_000` naast `FORGE_INTERVAL_MS`; `useQuestions` en `useReturned` gebruiken die voor `refetchInterval` en `staleTime`; `useCleanup` blijft op `FORGE_INTERVAL_MS`.

- [ ] **Step 1: Schrijf de falende test** (bestaand patroon van `useUsage` in `queries.test.tsx`, fake timers): `useQuestions` haalt `/api/questions` na `QUESTIONS_INTERVAL_MS` opnieuw op (`fetchMock` 2 aanroepen); en een test dat `useReturned` hetzelfde doet voor `/api/returned`. Pas de bestaande test "does not refetch on tab focus within the interval" aan zodat die de nieuwe constante gebruikt als hij `FORGE_INTERVAL_MS` aanhaalt.

- [ ] **Step 2: Run** `cd web && npm test -- queries` (of het script uit `web/package.json`) — FAIL (`QUESTIONS_INTERVAL_MS` bestaat niet).

- [ ] **Step 3: Implementeer** de constante en de twee hooks.

- [ ] **Step 4: Run** `cd web && npm run lint && npm run typecheck && npm test` — PASS; `git status` toont geen wijziging in `web/src/api` of `web/openapi.json`.

- [ ] **Step 5: Commit**
  ```bash
  git add web/src/lib/queries.ts web/src/lib/queries.test.tsx
  git commit -m "feat(web): questions en returned pollen elke minuut (#307)"
  ```

### Task 5: Documentatie en changelog

**Files:**
- Modify: `docs/install.md` (tabel rond regel 151–152 en de sectie *Connect the forge* rond regel 157)
- Create: `changelog.d/issue-307-forge-blokken-cache.md`

- [ ] **Step 1: Pas `docs/install.md` aan.** Taal van het bestand blijft zoals het is (Engels); beschrijf: `/api/questions` en `/api/returned` tonen een overzicht dat `serve` bij de start en daarna elke 2 minuten ververst (start tot start, nooit twee tegelijk, time-out 90 s per verversing); een mislukte verversing wordt na 20 s herhaald en beide endpoints geven dan 502 (503 zonder repo's) in plaats van oude data; het eerste request na een start wacht op de eerste verversing; `serve` leest GitHub ook als niemand kijkt (30 verversingen per uur per repo-set). Noem dat een repo zonder Controller-filter van het bord de comments van elk open issue met comments leest en dus zwaarder meetelt in de rate limit.

- [ ] **Step 2: Rate-limit-telling** (de spec zet die in de plan-fase): voeg in `docs/install.md` één zin toe met het aantal calls per build = per repo 1 (open issues, evt. meerdere pagina's) + 1 (board-items, alleen met bord) + 1 per Controller-issue met comments; bij 30 builds per uur tegen 5000 per uur samen met de tick. Controleer de aantallen in `internal/overview/overview.go` (`Build`, `fetchStatuses`) en `internal/forge` (paginering) vóór je ze opschrijft.

- [ ] **Step 3: Schrijf het changelog-fragment** `changelog.d/issue-307-forge-blokken-cache.md` met frontmatter `type: changed` (zie `changelog.d/issue-254-unitprefix-config.md`) en een Nederlandse zin: Open questions en Returned antwoorden direct uit een overzicht dat serve elke 2 minuten ververst; de pagina pollt elke minuut; bij een fout van de forge toont de pagina een fout in plaats van oude data.

- [ ] **Step 4: Run** de linkcheck/markdown-hook door de bestanden te schrijven (de `md-link-check`-hook draait bij Edit/Write); `git diff --stat` toont alleen deze twee bestanden.

- [ ] **Step 5: Commit**
  ```bash
  git add docs/install.md changelog.d/issue-307-forge-blokken-cache.md
  git commit -m "docs(install): forge-overzicht ververst elke 2 minuten (#307)"
  ```

### Task 6: Afronding

- [ ] **Step 1:** `go vet ./... && go test -race ./...` en de webchecks uit Task 4 — alles groen.
- [ ] **Step 2:** `go run ./cmd/lighthouse init --upgrade` — verwacht `No changes.` (er is niets onder `pack/` of een `init`-uitvoer veranderd).
- [ ] **Step 3: Handwerk na de deploy, niet in deze PR te bewijzen:** meet op de Beelink `curl -w '%{time_total}\n' -o /dev/null -s http://127.0.0.1:<poort>/api/questions` (binnen 1 s na de eerste build). Noem dit in de PR-body bij de acceptatiecriteria als nog te doen.

## Self-review

- **Spec-dekking:** overzicht in het geheugen (T1), één build voor beide (T2), interval/retry/time-out/geen overlap (T1), eerste-request-wacht (T1), fout vóór oude data (T1+T2), zonder repo's 503 en geen goroutine (T3), `New` start niets (T2/T3), pagina 1 minuut met eigen constante (T4), docs + changelog + rate-limit-telling (T5), meting (T6).
- **Typeconsistentie:** `BuildFunc`, `Refresher.Overview`, `Deps.Overview` en `startOverview` hebben overal dezelfde signatuur `func(context.Context) (overview.Overview, error)`.
- **Proportie:** één nieuw bestand met logica, de rest is bedrading.
