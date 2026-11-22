# IssueItem en Planning via REST — implementatieplan

> **Voor agents:** VERPLICHTE SUB-SKILL: gebruik superpowers:subagent-driven-development (aanbevolen) of superpowers:executing-plans om dit plan taak voor taak uit te voeren. Stappen gebruiken checkboxen (`- [ ]`).

**Doel:** `(*forge.GitHub).IssueItem` en `(*forge.GitHub).Planning` doen geen GraphQL-request meer, zodat een leeg GraphQL-budget ze niet laat falen.

**Architectuur:** `Planning` wordt opgebouwd uit de bestaande REST-lezers (open issues, `blocked_by`, milestones, gesloten issues per milestone), allemaal via `c.do` en dus met de ETag-cache van #428. `IssueItem` zoekt het numerieke item-id van een issue op en leest `GET …/projectsV2/<n>/items/<id>`, dat ook een gearchiveerd item geeft. Omdat de REST-lijst gearchiveerde items weglaat, bewaart de client de tabel issue → item-id in de bestaande `CacheStore` (SQLite, gedeeld door alle processen), zodat een nieuw proces een eerder geziene gearchiveerde item terugvindt.

**Tech stack:** Go, `internal/forge`, `internal/forge/forgetest` (nep-server met `Count`), `forgecache`.

**Spec:** issue #440 (geen apart specdocument; de acceptatiecriteria staan in het issue). Achtergrond: [verificaties van #395](2026-11-18-issue-395-verificaties.md), stap 2.

## Globale randvoorwaarden

- Code, commentaar en testnamen in het Engels; docs, changelog en PR in het Nederlands (`[project] language = nl`).
- GitLab (`gitlab_board.go`, `gitlab_planning.go`) blijft ongewijzigd; het `Forge`-contract (`ErrIssueNotFound` is geen `ok=false`) ook.
- `MilestoneCount.Done` telt na de overstap nog altijd alleen issues gesloten als *completed* (een lege `state_reason` telt als completed, zoals nu). Pull requests tellen niet mee. `Closed` blijft oplopend op nummer en nooit `nil`.
- Milestone #18, No-Gos: geen nieuwe controllerfunctie, geen telemetrie, geen statusmodel-migratie (#436).
- Andere GraphQL-gebruikers (`report.go`, `issues.go`, `graphql.go`) blijven; `docs/tick.md` noemt er alleen die echt in de tick lopen.
- Voor elke commit: `[test_gate] command` uit `lighthouse.toml` en `lighthouse imports check`. Smalle check tussendoor: `go test -race ./internal/forge/... ./internal/dispatch/... ./internal/issuestate/... ./internal/boardstart/... ./internal/priorities/... ./internal/server/... ./internal/cli/...`.
- Wijzigt een taak iets onder `pack/` (of een `internal/`-pad dat bepaalt wat `init` schrijft): ook `go run ./cmd/lighthouse init --upgrade` en de installed copy committen.
- Elke PR krijgt een fragment in `changelog.d/` (zie taak 3).

## Keuzes (gaan mee naar de PR-body)

- **Persistentie via `CacheStore`, geen nieuwe store.** `forgecache` is al de gedeelde, per-bestand SQLite-opslag naast de state-db; één extra entry met een eigen sleutel (`ITEMIDS …`, botst niet met `GET …`-sleutels) volstaat. Een client zonder cache (tests, `cache == nil`) houdt de tabel alleen per proces; dat staat in de doc van `IssueItem`.
- **Open vraag uit het issue (AC 2: "ook in een nieuw proces") is hiermee beantwoord:** de tabel wordt bij elke volledige lijstlezing bijgewerkt en bevat ook items die uit de lijst verdwenen zijn (gearchiveerd), tot 30 dagen na het laatste zien.
- **`Done` blijft alleen *completed*.** REST `closed_issues` telt *not planned* mee en wordt daarom niet gebruikt; `Done` en `Closed` komen uit `issues?state=closed&milestone=<n>` met een filter op `state_reason`. Kost één request per open milestone, die met de ETag-cache op `304` terugkomen. `Open` is het aantal open issues (zonder pull requests) uit dezelfde lijst als `Planning.Issues`, dus zonder search-queries.
- **`ErrIssueNotFound` zonder GraphQL:** vindt `IssueItem` geen item, dan leest hij `GET repos/<repo>/issues/<n>`; `404` én `410` (verwijderd issue) zijn `ErrIssueNotFound`, een andere fout wordt teruggegeven, een bestaand issue zonder item is `ok=false`.
- **Beperking, bewust (AC 2 vraagt alleen "eerder in de boardlijst gezien"):** een item dat al gearchiveerd was voordat een lijstlezing het ooit zag, of dat langer dan 30 dagen weg is, is niet meer te vinden en geeft `ok=false`. Triage (`internal/cli/triage_gather.go`) leest zo'n issue dan als "niet op het board". Dat staat in de doc van `IssueItem` en in de PR-body; de bestaande tests die een nooit-geziene gearchiveerde node veronderstellen, krijgen eerst een lijstlezing waarin het item nog staat. `lighthouse forge issue status N <s>` (`internal/cli/forgecmd_write.go:297-303`) roept voor zo'n nooit-gezien gearchiveerd item `AddToBoard` aan (ok=false); dat is aanvaard als zeldzaam geval (GitHub geeft bij een dubbele add het bestaande item terug; de uitvoerder controleert dat in de PR-notitie, niet in code). `docs/triage.md` (~r.156-158) en de doc van `boardItem` in `internal/cli/triage_gather.go:73-75` krijgen dezelfde beperking (taak 3).

## Review-focus

1. Een gearchiveerd item in een **nieuw proces** (lege in-memory tabel, gevulde cache) → `Archived: true`. → test in taak 2.
2. Een issue dat nooit op het board stond → `ok=false`, geen fout; een verwijderd issue → `ErrIssueNotFound`. → test in taak 2.
3. Een item dat uit de lijst verdwijnt en later terugkomt (ongedaan archiveren) → de tabel verwijst naar het actuele item, geen dubbele entry. → test in taak 2.
4. Een open issue in een gesloten milestone, een issue zonder milestone en een pull request in de issue-lijst → niet in `Planning.Issues` als PR, wel met `Milestone` ingevuld of `nil`. → test in taak 1.
5. Een issue dat als *not planned* sloot, telt niet in `Done` en niet in `Closed`. → test in taak 1.

---

### Task 1: `Planning` via REST

**Files:**
- Modify: `internal/forge/planning.go` (vervang `planIssues`, `planMilestones`, `planCounts`, de twee queries en `planCountsChunk`; verwijder `nextCursor` en `planMilestoneNode` als niets anders ze gebruikt)
- Modify: `internal/forge/issues.go` (`issueItem` krijgt `issue_dependencies_summary`)
- Modify: `internal/forge/github_contract_test.go` (`AddMilestone` vult `srv.Milestones` met een kleine-letter `state`, niet meer alleen `PlanMilestones`)
- Modify: `internal/forge/forgetest/server.go` (de GraphQL-handlers `Planning`, `PlanMilestones`, `PlanCounts` vervallen; de REST-fake moet `issue_dependencies_summary` en `state_reason` in de issue-lijst geven)
- Test: `internal/forge/planning_test.go`

**Interfaces:**
- Consumes: `(*GitHub).OpenIssues`-achtige paginering via `c.do` (github.go:93), `(*GitHub).Milestones(ctx, "open")` (write_issue.go:481), `(*GitHub).ClosedInMilestone(ctx, m, time.Time{})` (issues.go:79), `(*GitHub).blockedBy(ctx, repo, n) ([]Blocker, error)` (issues.go:~110).
- Produces: ongewijzigd `func (c *GitHub) Planning(ctx context.Context) (Planning, error)`; de typen `Planning`, `PlanIssue`, `MilestoneCount`, `ClosedIssue` blijven zoals ze zijn.

- [ ] **Step 1: Schrijf de falende tests in `planning_test.go`** (pas de bestaande aan, die op `PlanCounts`/GraphQL tellen):
  - `TestPlanningNoGraphQL`: een volledige `Planning`-lezing tegen de nep-server; assert dat er nul GraphQL-requests waren (bestaat er in `forgetest` geen totaalteller voor GraphQL, voeg `Server.GraphQLCount() int` toe).
  - `TestPlanningSecondReadIs304`: client met `NewMemoryCache()` (stijl `cachedClient`, cache_test.go:61); lees twee keer zonder wijziging; assert dat elke GET van de tweede lezing `304` kreeg (stijl van `cachedBoardClient`, board_test.go:393) en dat het resultaat gelijk is.
  - `TestPlanningDoneCountsOnlyCompleted`: milestone met één issue completed, één `not_planned`, één met lege `state_reason`; assert `Done == 2`, `len(Closed) == 2`, `Closed` oplopend, en dat de milestone-`closed_issues` van de fake (die hier 3 zegt) niet gebruikt wordt.
  - `TestPlanningOpenCountExcludesPullRequests`: een PR in de issuelijst telt niet in `Open` en niet in `Issues`.
  - `TestPlanningBlockers`: alleen open blockers in `PlanIssue.BlockedBy`; een issue met `total_blocked_by == 0` kost geen `blocked_by`-request (`Count`); is `issue_dependencies_summary` afwezig, dan wordt `blocked_by` wel gevraagd.
  - `TestPlanningMilestoneShapes`: open issue in gesloten milestone → `Milestone.State == "closed"`; issue zonder milestone → `Milestone == nil`; een open milestone zonder issues → `Open == 0`, `Done == 0`, `Closed` is `[]` (niet `nil`); `Labels` is nooit `nil`.

- [ ] **Step 2: Draai ze en zie ze falen**

  Run: `go test ./internal/forge/ -run 'TestPlanning' -v`
  Expected: FAIL (GraphQL-teller > 0, `304`-assert, `Done`).

- [ ] **Step 3: Implementeer** `Planning` in `planning.go` zonder GraphQL: één pagineerlus over `issues?state=open&sort=created&direction=asc&per_page=100` (zelfde volgorde als de oude GraphQL-query, `CREATED_AT ASC`) (zonder PR's, met `labels`, `milestone`, `html_url`, `issue_dependencies_summary`), per issue met `TotalBlockedBy > 0` (of de samenvatting afwezig) `c.blockedBy`, met alleen de open blockers behouden in de vorm die `PlanIssue.BlockedBy` nu heeft (`Blocker{Repo, Issue, State}`); daarna `c.Milestones(ctx, "open")` en per milestone `c.ClosedInMilestone(ctx, m, time.Time{})`, gefilterd op `StateReason in {"", "completed"}`, oplopend op nummer. `Open` per milestone telt de open issues uit de eerste lijst. Verwijder de dode GraphQL-code; `Planning.Milestones` en `Closed` blijven nooit `nil`.

- [ ] **Step 4: Draai de tests en zie ze slagen**

  Run: `go test -race ./internal/forge/... ./internal/priorities/... ./internal/server/...`
  Expected: PASS

- [ ] **Step 5: Commit** (`lighthouse imports check` eerst)

  ```bash
  git add internal/forge
  git commit -m "feat(forge): Planning van de GitHub-forge via REST (#440)"
  ```

---

### Task 2: `IssueItem` via REST met een bewaarde item-id-tabel

**Files:**
- Modify: `internal/forge/board.go` (`IssueItem`; verwijder `IssueItemsQuery`, `itemNode.IsArchived`-selectie en de verouderde doc op `BoardItem.Archived`)
- Modify: `internal/forge/project.go` (`listItems` bewaart de tabel; nieuwe `project.itemURL(id int64) string` naast `itemsURL()` met dezelfde `fields`-parameters)
- Create: `internal/forge/itemtable.go` (laden en bewaren van de tabel in de `CacheStore`)
- Modify: `internal/forge/forgetest/server.go` en `server_project.go` (de REST-lijst laat gearchiveerde items weg, `GET …/items/<id>` geeft ze met `archived_at`, zoals de echte API; de GraphQL-handler `IssueItems` vervalt)
- Test: `internal/forge/board_test.go`, `internal/forge/itemtable_test.go`, `internal/dispatch/gather_test.go:760` (teller `IssueItems` → REST-tellers), `internal/cli/boardstart_test.go:91` (`Posts[0]` is nu de REST-aanroep, geen GraphQL-POST: pas de assert aan), `internal/cli/triage_test.go:713-745` (`TestTriageArchivedItem`: laat het item eerst in de lijstlezing staan, archiveer daarna; zie *Beperking*), `internal/forge/board_test.go` `TestBoardItemsBlockers` (verwacht `MergedPR` voor blocker #4 uit een gearchiveerd item via `mergedIssues`; de fake toont dat item niet meer in de lijst: pas fixture of verwachting aan en de doc op `mergedIssues`)

**Interfaces:**
- Consumes: `restItem`/`restItem.boardItem(p, b)` (project.go), `named(it, values, b)`, `c.readIssue(ctx, n)` (github.go), `GitHub.cache CacheStore`.
- Produces:
  - ongewijzigd `func (c *GitHub) IssueItem(ctx context.Context, number int) (BoardItem, bool, error)`;
  - `const itemTableKeyPrefix = "ITEMIDS "`; sleutel `ITEMIDS <repo> <projectnummer>`;
  - `func (c *GitHub) saveItemTable(ctx context.Context, listed []restItem)` — best-effort, een fout van de store is nooit fataal;
  - `func (c *GitHub) savedItemID(ctx context.Context, key issueKey) (int64, bool)`.

- [ ] **Step 1: Schrijf de falende tests**
  - `TestIssueItemNoGraphQL`: `IssueItem` voor een item op het board; nul GraphQL-requests (teller uit taak 1).
  - `TestIssueItemArchivedInNewProcess`: client A (met `NewMemoryCache()`) leest de boardlijst (het item staat erin); archiveer het item in de fake; maak client B met **dezelfde cache** en een lege in-memory tabel; `IssueItem(n)` geeft `ok == true`, `Archived == true`, juiste `Status`/`Labels`, `ItemID` is het node-id.
  - `TestIssueItemNeverOnBoard`: issue bestaat, staat niet op het board → `ok == false`, `err == nil`.
  - `TestIssueItemDeletedIssue`: issue 99 bestaat niet (de fake geeft `404`; een tweede geval met `410`, voeg die mogelijkheid aan `forgetest` toe) → `errors.Is(err, ErrIssueNotFound)` (de gedeelde suite `forgesuite/suite.go:537-598` moet op beide forges groen blijven).
  - `TestItemTableKeepsVanishedItems`: na het verdwijnen uit de lijst blijft het item in de tabel; na `gone_since` ouder dan 30 dagen niet meer; een terugkerend item (ongedaan archiveren) staat één keer in de tabel.
  - `TestIssueItemWithoutCache`: `cache == nil` → tabel alleen per proces, geen fout.

- [ ] **Step 2: Draai ze en zie ze falen**

  Run: `go test ./internal/forge/ -run 'TestIssueItem|TestItemTable' -v`
  Expected: FAIL

- [ ] **Step 3: Implementeer de tabel in `itemtable.go`.** Eén JSON-blob per (repo, project) in de `CacheStore`: lijst `{node, id, repo, issue, gone_since?}`. `saveItemTable` leest de oude blob, voegt de nieuwe lijst samen (items in de lijst: `gone_since` leeg; items uit de oude blob die niet meer in de lijst staan: `gone_since` = nu als nog leeg, verwijderd bij ouder dan 30 dagen) en schrijft hem alleen terug als de inhoud verandert (`Get` ververst `used_at`, internal/forgecache/forgecache.go:126-135, dus de blob blijft binnen de `MaxAge` zolang hij gelezen wordt). `listItems` roept `saveItemTable` aan na een volledige lijst.

- [ ] **Step 4: Implementeer `IssueItem` in `board.go`:** (1) id uit `issueItems`; (2) anders één `listItems` en opnieuw; (3) anders `savedItemID`; (4) met id: `GET p.itemURL(id)` → `restItem` → `boardItem` → `named`, `it.Repo, it.Issue` gezet; een `404` hierop is `ok=false` (item verwijderd); (5) zonder id: `c.readIssue`; `404` of `410` → `fmt.Errorf("forge: issue %s#%d: %w", …, ErrIssueNotFound)`, een andere fout wordt teruggegeven, gelukt het lezen dan `ok=false`. Werk de doc-comment van `IssueItem` bij: GraphQL-verwijzing weg, beperking zonder cache genoemd.

- [ ] **Step 5: Verwijder dode code** (`IssueItemsQuery`, de selectie van `isArchived`, de fake-handler `IssueItems`, `FailOps{"IssueItems"}`-tests omzetten naar een REST-fout op het item-pad) en pas `board_test.go:509`, `gather_test.go:760` aan op de nieuwe tellers.

- [ ] **Step 6: Draai de tests en zie ze slagen**

  Run: `go test -race ./internal/forge/... ./internal/dispatch/... ./internal/issuestate/... ./internal/boardstart/... ./internal/cli/...`
  Expected: PASS

- [ ] **Step 7: Mutatiecheck:** haal tijdelijk `saveItemTable` uit `listItems` weg; `TestIssueItemArchivedInNewProcess` moet falen. Zet terug.

- [ ] **Step 8: Commit**

  ```bash
  git add internal/forge internal/dispatch internal/cli
  git commit -m "feat(forge): IssueItem van de GitHub-forge via REST (#440)"
  ```

---

### Task 3: Documentatie, changelog en afronding

**Files:**
- Modify: `docs/tick.md:~27` en `:101-105` (de alinea over de gather: noem alleen GraphQL-gebruik dat echt in de tick zit; controleer per kandidaat — `report.go`, `issues.go` — of de tick hem aanroept, en noem ook de regel over een uitgeputte GraphQL-limiet opnieuw)
- Modify: `docs/priorities.md` (beschrijving van de drie GraphQL-queries en de kostenregel `blockedBy(first: 20)` vervangen door de REST-route; zeg wat het tabblad telt: *Done* = alleen als *completed* gesloten issues, geen pull requests, en dat *not planned* niet meetelt)
- Modify: `docs/triage.md` (alinea over een gearchiveerd item, ~r.156-158: de ronde slaat het item over zolang `IssueItem` het kent; een item dat nooit in een lijstlezing stond, wordt niet gevonden) en `internal/cli/triage_gather.go` (doc van `boardItem`, r.73-75: zelfde beperking)
- Modify: `internal/priorities/priorities.go` en de tekst van het tabblad, alleen als daar nu een andere uitleg staat
- Create: `changelog.d/issue-440-issueitem-planning-rest.md`

**Interfaces:**
- Consumes: de werking uit taak 1 en 2.
- Produces: docs die gelijk lopen met de code (AC 5, AC 4).

- [ ] **Step 1:** `grep -rn "GraphQL" docs/tick.md docs/priorities.md pack/ .claude/skills/ | grep -i "IssueItem\|Planning"`; pas elke bewering aan. Raakt dit iets onder `pack/`, draai `go run ./cmd/lighthouse init --upgrade` en commit de kopie; verwacht daarna `No changes.`

- [ ] **Step 2:** Schrijf het changelog-fragment:

  ```markdown
  ---
  type: changed
  ---
  `IssueItem` en `Planning` van de GitHub-forge gebruiken geen GraphQL meer. Een leeg GraphQL-budget laat het opzoeken van een boarditem en het tabblad Prioritering daardoor niet meer falen. Een gearchiveerd item blijft vindbaar, ook in een nieuw proces. `MilestoneCount.Done` telt nog altijd alleen als *completed* gesloten issues.
  ```

- [ ] **Step 3:** Draai de volledige gate: `[test_gate] command` uit `lighthouse.toml` en `lighthouse imports check`. Verwacht groen.

- [ ] **Step 4: Commit**

  ```bash
  git add docs changelog.d
  git commit -m "docs(forge): tick en prioritering zonder GraphQL voor IssueItem en Planning (#440)"
  ```

---

## Zelfreview

- **AC 1** → taak 2 (`TestIssueItemNoGraphQL`). **AC 2** → taak 2 (`TestIssueItemArchivedInNewProcess`, tabel in `CacheStore`). **AC 3** → taak 1 (`TestPlanningNoGraphQL`, `TestPlanningSecondReadIs304`). **AC 4** → taak 1 (`TestPlanningDoneCountsOnlyCompleted`) en taak 3 (docs). **AC 5** → taak 3.
- Onbekend bij schrijven, door de uitvoerder in de code te controleren (niet te raden): of `Get` van `forgecache` `used_at` ververst (stap 3 van taak 2); of de echte API `issue_dependencies_summary` in de issuelijst geeft (zo niet, valt de code terug op één `blocked_by`-request per issue, correct maar duurder); welke van `report.go` en `issues.go` GraphQL echt in de tick draaien (taak 3).
- Echte-API-gedrag (archief-item via `GET items/<id>`) is alleen met de hand bewezen in de verificaties van #395; de nep-server bewijst het contract, niet de live API. Meld dat in de PR.
