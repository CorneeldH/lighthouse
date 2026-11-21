# Prioriteit per milestone Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Een milestone krijgt een rang (`Priority: N` in zijn beschrijving op de forge) die de gebruiker in de weergave *Milestones* met omhoog en omlaag aanpast.

**Architecture:** Eén Go-functiepaar leest en schrijft de regel; de UI leest dezelfde regel uit `description` (TypeScript-spiegel). De UI berekent welke milestones een nieuwe rang krijgen en stuurt die lijst naar `POST /api/priorities/milestone-ranks`; de server controleert `Origin`, leest vers, weigert bij een verouderde rang (409), schrijft met `EditMilestone` en past de `Refresher`-cache aan zonder te herbouwen.

**Tech Stack:** Go (huma, `net/http`), React 19 + TanStack Query, orval-gegenereerde client (`web/openapi.json`), vitest.

**Spec:** [`../specs/2026-11-20-milestone-prioriteit-design.md`](../specs/2026-11-20-milestone-prioriteit-design.md)

## Global Constraints

- Prioriteitsregel: na `strings.TrimSpace` en hoofdletterongevoelig past de hele regel op `priority:\s*[1-9][0-9]*`. `Priority: high` en `Priority: 0` zijn gewone tekst. Meerdere regels: de eerste telt.
- Schrijven vervangt de eerste prioriteitsregel op zijn plek; zonder regel komt `\n\nPriority: N` achter de beschrijving (lege beschrijving: alleen `Priority: N`). Alle andere bytes blijven gelijk, ook regeleinden.
- Endpoint: `POST /api/priorities/milestone-ranks`, body `{"repo": "...", "changes": [{"number": 6, "from": 2, "to": 1}]}`, `from` is `null` zonder rang, één repo per verzoek, `Content-Type: application/json`.
- Foutcodes: herkomst 403, onbekende repo 404, verouderde rang 409 (niets geschreven), forge-fout via `forgeError` (502/503).
- `Origin`: host[:poort] van de `Origin`-URL letterlijk gelijk aan de header `Host`; schema telt niet mee, geen standaardpoorten invullen; zonder `Origin` 403. Niet voor `/api/node/`.
- `web/vite.config.ts`: `changeOrigin: false`.
- Foutmelding bij 409 in de UI: "De volgorde is net ergens anders veranderd; de lijst is ververst."
- Volgorde stand *Prioriteit*: plaats in repo (rang, gelijk → berekende volgorde); over repo's heen plaats 1 van alle repo's, dan plaats 2, …, binnen een plaats berekende volgorde; zonder rang daaronder in berekende volgorde.
- Code, namen, commentaar in het Engels (`code_language = "en"`); artefacten Nederlands. Test gate: `lighthouse gate go -- go test -race` en `lighthouse gate node --dir web …` draaien per commit.

## Review Focus

- Beschrijving met `\r\n`-regeleinden: schrijven laat de overige regels byte voor byte staan (test in Task 1 en Task 6).
- Beschrijving zonder eindregel of met alleen spaties: `SetRank` geeft `…\n\nPriority: N`, nooit een lege dubbele regel te veel (Task 1).
- Twee klikken vlak na elkaar op dezelfde repo: knoppen van die repo staan uit tijdens het verzoek (Task 9).
- Een bouw die over een aanpassing heen loopt schrijft de oude waarde niet terug (Task 2).
- Een mislukte schrijfactie halverwege: antwoord noemt de al geschreven nummers en de cache toont precies die (Task 4).

---

## File Structure

- Create `internal/priorities/rank.go`, `rank_test.go`: `ParseRank`, `SetRank`, `WithDescription`.
- Modify `internal/server/refresher.go`: `Modify`, generatieteller.
- Create `internal/server/origin.go`, `origin_test.go`: `requireSameOrigin`.
- Create `internal/server/milestone_ranks.go`, `milestone_ranks_test.go`: het endpoint.
- Modify `internal/server/server.go`: `Deps` + registratie. Modify `internal/cli/serve.go`: wiring.
- Modify `web/vite.config.ts`, `web/openapi.json`, `web/src/api/*` (gegenereerd).
- Create `web/src/lib/priorities/rank.ts`, `rank.test.ts`; modify `derive.ts`, `prefs.ts` (+ tests).
- Modify `web/src/lib/api.ts`, `queries.ts`; modify `MilestonesView.tsx`, `Toolbar.tsx`, `PrioritiesTab.tsx` (+ tests).
- Create `changelog.d/issue-454-milestone-prioriteit.md`.

### Task 1: De regel lezen en schrijven in Go

**Files:**
- Create: `internal/priorities/rank.go`
- Test: `internal/priorities/rank_test.go`

**Interfaces:**
- Produces: `func ParseRank(description string) (rank int, ok bool)`; `func SetRank(description string, rank int) string`; `func WithDescription(repos []Repo, slug string, number int, description string) []Repo` (kopieert de geraakte `Repo` en `Milestones`-slice, muteert de invoer niet).

- [ ] **Step 1: Write the failing tests** in `rank_test.go`, table-driven, namen `TestParseRank`, `TestSetRank`, `TestWithDescription`. Gevallen `ParseRank` (spiegel in Task 6): `""`→(0,false); `"Priority: 3"`→(3,true); `"  priority:7  "`→(7,true); `"PRIORITY:  12"`→(12,true); `"Priority: high"`, `"Priority: 0"`, `"Priority: 03"`, `"Priority: 2 extra"`, `"- Priority: 2"`→(0,false); `"a\nPriority: 2\nPriority: 5"`→(2,true); `"Priority: high\nPriority: 4"`→(4,true). `SetRank`: `("",1)`→`"Priority: 1"`; `("Pitch",2)`→`"Pitch\n\nPriority: 2"`; `("Pitch\nPriority: 2\nrest",1)`→`"Pitch\nPriority: 1\nrest"`; `("a\r\nPriority: 2\r\nb",9)`→`"a\r\nPriority: 9\r\nb"`; `("Priority: high\nPriority: 4",1)`→`"Priority: high\nPriority: 1"`; `("  Priority: 2  \n",1)` → `"  Priority: 1  \n"` (inspringing en eindspaties blijven staan). `WithDescription`: nieuwe waarde bevat de beschrijving, de invoerslice niet.
- [ ] **Step 2: Run** `go test ./internal/priorities -run 'TestParseRank|TestSetRank|TestWithDescription'` — verwacht FAIL (undefined).
- [ ] **Step 3: Implement** de drie functies in `rank.go`. Eén gedeelde `regexp` `(?i)^priority:\s*([1-9][0-9]*)$` op de getrimde regel; splits op `\n` (een `\r` aan het eind valt onder trim en blijft in de regel behouden bij vervangen: vervang alleen de getrimde kern, bewaar de omringende spaties/`\r`).
- [ ] **Step 4: Run** dezelfde tests — PASS.
- [ ] **Step 5: Commit** `feat(priorities): Priority-regel lezen en schrijven (#454)`.

### Task 2: `Refresher.Modify` met generatieteller

**Files:**
- Modify: `internal/server/refresher.go`
- Test: `internal/server/refresher_test.go`

**Interfaces:**
- Produces: `func (r *Refresher[T]) Modify(fn func(T) T) bool` — onder de lock: heeft de laatste bouw een fout of is er nog geen waarde, dan niets doen en `false`; anders `r.val = fn(r.val)`, generatie ophogen, `true`.

- [ ] **Step 1: Write failing tests**: `TestRefresherModifyAdjustsLatest` (na een bouw met waarde 1 geeft `Modify(+1)` daarna `Latest` 2); `TestRefresherModifyIgnoredOnFailedBuild` (bouw met fout → `Modify` geeft false, `Latest` geeft nog steeds de fout); `TestRefresherModifyDuringBuildKeepsModified` (een bouw die blokkeert op een kanaal, `Modify` terwijl hij loopt, bouw loslaten → `Latest` geeft de aangepaste waarde, niet het bouwresultaat); `TestRefresherModifyBeforeFirstBuild` (false).
- [ ] **Step 2: Run** `go test ./internal/server -run TestRefresherModify` — FAIL.
- [ ] **Step 3: Implement**: veld `gen uint64`; `refresh` leest `gen` onder `RLock` vóór de bouw en slaat het resultaat alleen op als `gen` onder `Lock` gelijk is gebleven (anders: geen opslag, `readyOnce` niet raken, return `true` zodat de gewone interval volgt — de volgende ronde bouwt vers). Gooit een weggegooide bouw niets weg dat `Latest` wacht? `Modify` werkt pas na de eerste opgeslagen bouw, dus `ready` is dan al dicht.
- [ ] **Step 4: Run** de refresher-tests volledig (`go test -race ./internal/server -run Refresher`) — PASS.
- [ ] **Step 5: Commit** `feat(server): Refresher.Modify met generatieteller (#454)`.

### Task 3: Herkomstcontrole

**Files:**
- Create: `internal/server/origin.go`
- Test: `internal/server/origin_test.go`

**Interfaces:**
- Produces: `func sameOrigin(r *http.Request) bool` — `Origin` aanwezig, parsebaar, en `u.Host == r.Host` (letterlijk, hoofdletterongevoelig). Het endpoint in Task 4 roept hem aan; `/api/node/` niet.

- [ ] **Step 1: Write failing tests** `TestSameOrigin` (table): `Origin: https://x.ts.net` + `Host: x.ts.net` → true; `Origin: http://localhost:5173` + `Host: localhost:5173` → true; `Origin: http://localhost:5173` + `Host: 127.0.0.1:8127` → false; geen `Origin` → false; `Origin: null` → false; `Origin: https://evil.example` + `Host: x.ts.net` → false; `Origin: http://x.ts.net:80` + `Host: x.ts.net` → false (geen standaardpoort invullen).
- [ ] **Step 2: Run** `go test ./internal/server -run TestSameOrigin` — FAIL.
- [ ] **Step 3: Implement** `sameOrigin` met `url.Parse`; lege `Host` in de URL → false.
- [ ] **Step 4: Run** — PASS.
- [ ] **Step 5: Commit** `feat(server): Origin-controle voor schrijf-endpoints (#454)`.

### Task 4: Het endpoint `POST /api/priorities/milestone-ranks`

**Files:**
- Create: `internal/server/milestone_ranks.go`
- Modify: `internal/server/server.go` (`Deps`, `New`)
- Test: `internal/server/milestone_ranks_test.go`

**Interfaces:**
- Consumes: `priorities.ParseRank/SetRank/WithDescription` (Task 1), `sameOrigin` (Task 3), `forgeError` (`forge.go`).
- Produces: in `Deps`: `PriorityForges map[string]forge.Forge` (slug → forge) en `PatchPriorities func(func([]priorities.Repo) []priorities.Repo) bool`. `func registerMilestoneRanks(api huma.API, forges map[string]forge.Forge, patch func(func([]priorities.Repo) []priorities.Repo) bool)`, operation id `post-milestone-ranks`. Body-types `RankChange{Number int; From *int; To int}`, `RanksRequest{Repo string; Changes []RankChange}`, antwoord `RanksResult{Written []int}` (nooit nil).

- [ ] **Step 1: Write failing tests** met een `fakeMilestones`-forge (embed `forge.Forge`, in-memory beschrijvingen, telt `EditMilestone`-aanroepen, optioneel fout bij de n-de aanroep) en een eigen `Refresher` als `patch`. Tests: `TestMilestoneRanksSwap` (twee milestones wisselen: 200, `written == [6,4]`, beide beschrijvingen op de forge kloppen, pitchregels onaangeroerd, ook met `\r\n`); `TestMilestoneRanksNoOrigin` (403, 0 edits); `TestMilestoneRanksWrongOrigin` (403); `TestMilestoneRanksNotJSON` (`text/plain` → 415, 0 edits); `TestMilestoneRanksUnknownRepo` (404); `TestMilestoneRanksStale409` (`from` klopt niet → 409, 0 edits, cache aangepast met de verse beschrijvingen, en een tweede verzoek met de verse `from` slaagt zonder bouw); `TestMilestoneRanksFromNull` (milestone zonder regel krijgt `\n\nPriority: N`); `TestMilestoneRanksPartialFailure` (tweede `EditMilestone` faalt: 502, de foutbody noemt `written=[eerste]`, de cache bevat de eerste wijziging wel en de tweede niet); `TestMilestoneRanksForgeNotConfigured` (503 via `forge.ErrNotConfigured`); lege `changes` → 422 of 400 (huma validatie, kies `minItems:1`).
- [ ] **Step 2: Run** `go test ./internal/server -run TestMilestoneRanks` — FAIL.
- [ ] **Step 3: Implement** `registerMilestoneRanks` met `huma.Register` (POST, `Content-Type` via huma `application/json`, 415 bij andere). De Origin-controle in de handler via `huma.Context`-vrije weg: lees `Origin` en `Host` met input-header-velden `Origin string \`header:"Origin"\`` en `Host`-waarde uit `ctx`-resolver (`huma.Resolver` op de inputstruct geeft `huma.Context` → `ctx.Host()`). Volgorde van stappen volgt de spec (herkomst, repo, vers lezen + vergelijken, schrijven met cache-patch per schrijfactie via `WithDescription` en de beschrijving die `EditMilestone` teruggeeft). Bij 409 eerst `patch` met de verse beschrijvingen, dan de 409. Foutafhandeling halverwege: `huma.NewError(502, msg)` waarin de al geschreven nummers staan. Registreer in `New` naast `registerPriorities`, alleen als `d.PriorityForges != nil` (anders blijft het endpoint weg, zodat bestaande tests heel blijven). De Origin-controle zit per endpoint (spec laat de keuze), `/api/node/` blijft ongemoeid.
- [ ] **Step 4: Run** `go test -race ./internal/server` — PASS.
- [ ] **Step 5: Commit** `feat(server): schrijf-endpoint voor milestone-rang (#454)`.

### Task 5: Wiring in `lighthouse serve`

**Files:**
- Modify: `internal/cli/serve.go` (`startPriorities`, `startRefresher`, `serve`)
- Test: `internal/cli/serve_test.go` (bestaande helper-conventies volgen)

**Interfaces:**
- Consumes: `Refresher.Modify` (Task 2), `Deps.PriorityForges`/`PatchPriorities` (Task 4).
- Produces: `startRefresher` geeft naast `read` en `stopped` een `patch func(func(T) T) bool` terug (no-op `false` zonder refresher); `startPriorities` geeft die door. `serve` vult `PriorityForges` uit `prioSrcs` (`Slug` → `Src`) en `PatchPriorities`.

- [ ] **Step 1: Write failing test** `TestServeMilestoneRankEndToEnd`: bestaande serve-teststijl; een fake forge met twee milestones; POST met juiste `Origin`/`Host` wisselt de rang en daarop geeft `GET /api/priorities` direct de nieuwe beschrijvingen (zonder te wachten op een bouw). Is er geen serve-testhelper die dit toelaat, test dan `startPriorities` direct: na `patch` geeft `read` de aangepaste waarde.
- [ ] **Step 2: Run** `go test ./internal/cli -run TestServeMilestoneRank` — FAIL.
- [ ] **Step 3: Implement** de signatuurwijziging en de vulling van `Deps`; pas alle aanroepers van `startOverview`/`startRefresher` aan (de overview negeert `patch`).
- [ ] **Step 4: Run** `go test -race ./internal/cli ./internal/server` — PASS.
- [ ] **Step 5: Commit** `feat(cli): serve verbindt de milestone-rang met de cache (#454)`.

### Task 6: OpenAPI-snapshot, client en Vite-proxy

**Files:**
- Modify: `web/openapi.json`, `web/src/api/*` (gegenereerd), `web/vite.config.ts`

**Interfaces:**
- Produces: gegenereerde `postMilestoneRanks(body, options)` en types `RanksRequest`, `RankChange`, `RanksResult` in `@/api/lighthouse`.

- [ ] **Step 1: Run** `go test ./internal/server -run TestOpenAPISnapshot` — verwacht FAIL (`web/openapi.json is stale`). Zorg dat `Deps` van `testDeps` het endpoint registreert (`PriorityForges` gevuld) zodat de snapshot het bevat.
- [ ] **Step 2: Run** `go test ./internal/server -run TestOpenAPISnapshot -update`, dan `cd web && npm run gen`.
- [ ] **Step 3: Zet** in `web/vite.config.ts` `changeOrigin: false` met een commentaarregel waarom (Origin = Host, anders 403).
- [ ] **Step 4: Run** `go test ./internal/server` en `cd web && npm run typecheck` — PASS.
- [ ] **Step 5: Commit** `feat(web): milestone-rank in de OpenAPI-snapshot en client (#454)`.

### Task 7: Rang en volgorde in de web-lib

**Files:**
- Create: `web/src/lib/priorities/rank.ts`, `rank.test.ts`
- Modify: `web/src/lib/priorities/derive.ts` (+ `derive.test.ts`), `web/src/lib/priorities/prefs.ts` (+ `prefs.test.ts`)

**Interfaces:**
- Produces: `parseRank(description: string): number | null` (zelfde gevallen als `TestParseRank`, uitkomst `null` voor `ok=false`); `MilestoneRow` krijgt `rank: number | null`; `priorityOrder(rows: MilestoneRow[]): MilestoneRow[]` (de volgorde uit *Global Constraints*; invoer is al in berekende volgorde); `Prefs` krijgt `msOrder: 'computed' | 'priority'` (standaard `'computed'`, ongeldige waarde → standaard); `placeInRepo(rows, repo, number): number | null` (1-gebaseerde plaats onder de milestones met rang in die repo).

- [ ] **Step 1: Write failing tests**: `rank.test.ts` spiegelt de `ParseRank`-tabel; `derive.test.ts`: `priorityOrder` met twee repo's (A: rang 2,1; B: rang 1) → `A1, B1, A2`; gelijke rang binnen een repo houdt berekende volgorde; zonder rang onderaan in berekende volgorde; `milestoneRows` zet `rank` uit de beschrijving; `prefs.test.ts`: round-trip en fallback voor `msOrder`.
- [ ] **Step 2: Run** `cd web && npx vitest run src/lib/priorities` — FAIL.
- [ ] **Step 3: Implement** de functies; `priorityOrder` is stabiel en volgt stap 1–3 van de spec letterlijk (plaats per repo eerst, dan sorteren op plaats met de invoerindex als tiebreak).
- [ ] **Step 4: Run** dezelfde vitest + `npm run typecheck` — PASS.
- [ ] **Step 5: Commit** `feat(web): rang en prioriteitsvolgorde voor milestones (#454)`.

### Task 8: Verplaatsing berekenen

**Files:**
- Create: `web/src/lib/priorities/moves.ts`, `moves.test.ts`

**Interfaces:**
- Consumes: `MilestoneRow` (Task 7).
- Produces: `type RankChange = { number: number; from: number | null; to: number }`; `planMove(rows: MilestoneRow[], repo: string, number: number, dir: 'up' | 'down'): RankChange[] | null` — `rows` zijn de milestones van één repo in de actuele prioriteitsvolgorde; `null` als de beweging niet kan; `canMove(rows, number, dir): boolean`.

- [ ] **Step 1: Write failing tests**: omhoog wisselt met de milestone erboven (`from`/`to` kloppen); plaats 1 omhoog → `null`; omlaag op de laatste met rang → `null`; zonder rang omhoog → `to = max+1`, `from: null`, omlaag → `null`; ranggaten `1,1,4` → eerst doornummeren naar `1..k` en dan wisselen, resultaat bevat alle milestones waarvan de rang verandert (en alleen die); een repo met één milestone → alles `null`.
- [ ] **Step 2: Run** `cd web && npx vitest run src/lib/priorities/moves.test.ts` — FAIL.
- [ ] **Step 3: Implement** zoals de spec (hernummer eerst in huidige volgorde, dan wissel; filter wijzigingen waar `from === to`).
- [ ] **Step 4: Run** — PASS.
- [ ] **Step 5: Commit** `feat(web): planMove berekent de rangwijzigingen (#454)`.

### Task 9: Schrijven met optimistisch bijwerken

**Files:**
- Modify: `web/src/lib/api.ts`, `web/src/lib/queries.ts`
- Test: `web/src/lib/api.test.ts`, `web/src/lib/queries.test.tsx`

**Interfaces:**
- Consumes: `postMilestoneRanks` (Task 6), `RankChange` (Task 8), `SetRank`-spiegel hieronder.
- Produces: `setRank(description: string, rank: number): string` in `rank.ts` (zelfde tabel als `TestSetRank`); `postMilestoneRanks(repo: string, changes: RankChange[]): Promise<void>` in `api.ts` (via `call`, 409 → `ApiError`); `useMilestoneRanks()` in `queries.ts` → `{ mutate(vars: {repo; changes}), isPending(repo), error: string | null }` met `onMutate` (query-cache `priorities` aanpassen via `setRank`), `onError` (herladen via `invalidateQueries`, foutmelding; 409 → de vaste tekst uit *Global Constraints*), `onSettled` (herladen).

- [ ] **Step 1: Write failing tests**: `rank.test.ts` voor `setRank` (spiegel van de Go-tabel incl. `\r\n`); `api.test.ts`: `postMilestoneRanks` stuurt `Content-Type: application/json` en gooit `ApiError(409, …)`; `queries.test.tsx`: optimistische cache toont direct de nieuwe beschrijving; bij fout staat de verse data er weer en `error` bevat de 409-tekst; `isPending('o/r')` is true tijdens het verzoek en false voor een andere repo.
- [ ] **Step 2: Run** `cd web && npx vitest run src/lib` — FAIL.
- [ ] **Step 3: Implement** met `useMutation`; geen `Origin` zetten (de browser doet dat).
- [ ] **Step 4: Run** `npx vitest run src/lib && npm run typecheck` — PASS.
- [ ] **Step 5: Commit** `feat(web): milestone-rang schrijven met optimistische update (#454)`.

### Task 10: Switch, knoppen en melding in de UI

**Files:**
- Modify: `web/src/components/priorities/Toolbar.tsx`, `MilestonesView.tsx`, `PrioritiesTab.tsx`, `types.ts`
- Test: `MilestonesView.test.tsx`, `PrioritiesTab.test.tsx`

**Interfaces:**
- Consumes: `priorityOrder`, `placeInRepo`, `planMove`, `canMove`, `useMilestoneRanks`, `Prefs.msOrder`.
- Produces: `Toolbar` krijgt `msOrder` en `onMsOrder` (zichtbaar alleen in weergave *Milestones*; knoppen **Berekend** en **Prioriteit**, `aria-pressed`); `ViewProps` krijgt optioneel `order?: 'computed' | 'priority'`, `onMove?(row, dir)`, `pendingRepos?: Set<string>`; `MilestonesView` toont per rij knoppen "Omhoog"/"Omlaag" (`aria-label`) en de plaats als klein getal bij de titel, alleen in stand *Prioriteit*.

- [ ] **Step 1: Write failing tests**: stand *Prioriteit* toont plaats-getal bij milestones met rang en geen getal zonder; knop omhoog op plaats 1 is `disabled`; omlaag bij een milestone zonder rang is `disabled`; een klik roept `onMove` met de juiste richting; knoppen van een repo met lopend verzoek zijn `disabled`, die van andere repo's niet; in stand *Berekend* staan geen knoppen; `PrioritiesTab`: de switchkeuze wordt bewaard (`localStorage`), de lijst herordent, na een gemockte 409 staat de melding "De volgorde is net ergens anders veranderd; de lijst is ververst." zichtbaar.
- [ ] **Step 2: Run** `cd web && npx vitest run src/components/priorities` — FAIL.
- [ ] **Step 3: Implement**; de rijen komen uit `milestoneRows` → `sortMilestoneRows` → bij *Prioriteit* `priorityOrder`; `onMove` roept `planMove` met de rijen van die repo in actuele prioriteitsvolgorde en geeft het resultaat aan `useMilestoneRanks().mutate`. De repo-filter blijft zoals nu.
- [ ] **Step 4: Run** `cd web && npm run lint && npm run typecheck && npm test` — PASS.
- [ ] **Step 5: Commit** `feat(web): Berekend/Prioriteit-switch met omhoog en omlaag (#454)`.

### Task 11: Changelog en eindcontrole

**Files:**
- Create: `changelog.d/issue-454-milestone-prioriteit.md`

- [ ] **Step 1: Schrijf** het fragment (`type: added`, vorm van `changelog.d/issue-407-forge-neutrale-go-units.md`): de weergave *Milestones* heeft een stand *Prioriteit*; `lighthouse serve` schrijft `Priority: N` in de milestone-beschrijving en eist daarvoor een `Origin` gelijk aan `Host`; de Vite-devproxy staat op `changeOrigin: false`.
- [ ] **Step 2: Run** `go test -race ./... && cd web && npm run lint && npm run typecheck && npm test` — alles PASS. `go run ./cmd/lighthouse init --upgrade` hoeft niet: niets onder `pack/` of wat `init` schrijft is geraakt; draai hem toch en verwacht `No changes.`
- [ ] **Step 3: Commit** `docs(changelog): milestone-prioriteit (#454)`.

---

## Self-review

- **Spec coverage:** regel lezen/schrijven → T1/T7/T9 (spiegel); volgorde → T7; omhoog/omlaag incl. doornummeren → T8/T10; endpoint, 403/404/409/forge-fouten, partial failure → T4; cache (Modify, generatieteller, 409-patch) → T2/T4/T5; Origin + Vite → T3/T6; optimistisch + knoppen uit → T9/T10; melding 409 → T9/T10; `Deps`-verbreding → T4/T5 (r1-f4).
- **Types:** `RankChange` (Go `From *int`, TS `from: number | null`), `Modify` (Task 2) is wat `PatchPriorities` (Task 4/5) aanroept; `planMove`/`canMove` uit Task 8 worden in Task 10 gebruikt.
- **Proportie:** geen codeblokken; signatures en testgevallen beslissen, de implementer schrijft de bodies.
