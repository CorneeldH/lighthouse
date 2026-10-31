# Forge schrijven en de snapshot — implementatieplan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Lighthouse leest en schrijft GitHub zelf (REST en GraphQL in Go), bouwt per repo de snapshot voor de tick, en slaat met een vingerafdruk een ongewijzigde repo over.

**Architecture:** `internal/forge` krijgt één request-kern voor elke methode, schrijfacties (comment, 🚀, board-status), de permissie- en loginreads en een GraphQL-kern met board- en PR-reads. Een nieuw pakket `internal/dispatch` zet die reads om in een `Snapshot` (JSON, versie 1), filtert comments op vertrouwde auteurs en 🚀 op de token-login, en rekent de vingerafdruk en de precheck uit; de stand staat in de bestaande tabel `tick_state`. `internal/cli/tick.go` roept precheck en gather aan per bedienbare repo; `lighthouse gather <repo>` dumpt de snapshot als JSON. Decide en apply komen in #114.

**Tech Stack:** Go, `net/http`, `net/http/httptest`, `encoding/json`, `crypto/sha256`, cobra, `modernc.org/sqlite` (bestaand).

**Spec:** [`docs/developer/planning/specs/2026-10-27-m2-dispatcher-design.md`](../specs/2026-10-27-m2-dispatcher-design.md), §3 *De tick* (precheck, token, snapshot), §4 *Dedup* (laatste alinea, 🚀), §5 *Antwoorden en vertrouwde auteurs*, *Teststrategie* · Issue #112.

## Global Constraints

- Go volgt `/lighthouse-go-writing`; tests volgen `/lighthouse-testing` (classificatiezin vóór de eerste edit, mutatiecheck vóór "klaar").
- Code-taal Engels onder `internal/**`: namen, commentaar, foutmeldingen, logregels. Plan, docs, commits en changelog in het Nederlands.
- Geen `gh` als subproces: alles via `net/http` naar `BaseURL` (REST) en `BaseURL + "/graphql"` (GraphQL).
- Headers op elk request, ook POST: `Authorization: Bearer <token>`, `Accept: application/vnd.github+json`, `X-GitHub-Api-Version: 2022-11-28`. Een POST zet ook `Content-Type: application/json`.
- Het token staat nooit in een fout of logregel. `sameOrigin` blijft gelden voor elke volgende pagina.
- Lighthouse merget nooit: geen merge-endpoint, geen `mergePullRequest`-mutatie (milestone No-Go).
- Permissie: `GET /repos/{repo}/collaborators/{user}/permission`, veld **`role_name`** (niet `permission`: dat kent geen `maintain`). Vertrouwd = `role_name` in `[dispatch] answer_permissions` (standaard `["admin", "maintain"]`). Eén request per login per repo per tick.
- 🚀 telt alleen van de login van het token (`GET /user`, één keer per tick).
- Precheck: overslaan alleen als de vingerafdruk gelijk is aan de vorige, de laatste volledige gather jonger is dan **30 minuten**, en er sinds die gather geen tijdgrens viel. Tijdgrenzen in dit issue: `started_at + max_runtime` (8h) en `started_at + max_wait` (1h) van elke open run, en het begin van het `night`-venster. `reset_at` van markers komt met #116 (de rem) via dezelfde `boundaries`-parameter.
- Geen migratie: vingerafdruk en tijd van de laatste volledige gather staan in `tick_state` met `kind = 'fingerprint'`, sleutels `value` en `full_at` (`0002_runs.sql:23-28`).
- Snapshot-JSON heeft `"version": 1`; tijden in RFC 3339 UTC.
- Buiten dit issue (spec *Snapshot per repo*): `git fetch` en de sha van `origin/main`, resume-markers, pauzevlag, `MemAvailable`, usage-venster (#116), forge-workspaces en hun sessies (#113). De vingerafdruk-input krijgt die velden wanneer hun issue landt.
- Commits: Conventional Commits, Nederlands, één logische wijziging per commit.
- Verificatie zoals CI: `go test -race -shuffle=on ./...` en `golangci-lint run`.

## Review Focus

1. **GraphQL antwoordt 200 met `errors`.** Een board-read met een fout mag nooit als "leeg board" doorgaan: dat verandert de vingerafdruk en ziet eruit als "niets in Controller". Verwacht: een fout, de tick slaat de repo luid over en bewaart geen stand. → test in Taak 3.
2. **Een vreemde auteur zonder collaborator-rechten.** GitHub geeft dan 404 op de permissie-endpoint. Verwacht: niet vertrouwd, geen fout. Een 5xx is wél een fout: gather faalt, de tick bewaart geen stand. → tests in Taak 2 en Taak 5.
3. **Meer dan één pagina.** Board met >100 items, reacties met >100 🚀. Verwacht: alle pagina's gelezen. → tests in Taak 2 en Taak 3.
4. **Volgorde.** Dezelfde stand in een andere volgorde (items, PR's, voorgangers) geeft dezelfde vingerafdruk; anders gathert elke tick. → test in Taak 6.
5. **Board-items die geen issue van deze repo zijn** (draft issue, PR, issue uit een andere repo op hetzelfde board, of een status buiten Controller en On hold). Verwacht: niet in de snapshot en niet in de vingerafdruk. Een verwijderde gebruiker (`user: null`) is niet vertrouwd. → tests in Taak 5.

---

### Task 1: forge — één request-kern en de REST-schrijfacties

**Files:**
- Modify: `internal/forge/github.go` (package-doc `:1`, `Client`-doc `:29`, `get` `:117`, `statusError` `:145`)
- Create: `internal/forge/write.go`
- Modify: `internal/forge/github_test.go` (`fakeTransport` `:21-54`, `TestOnlyGET` `:230`), `internal/protocol/status.go:9`
- Create: `internal/forge/write_test.go`

**Interfaces:**
- Produces:
  - `func (c *Client) do(ctx context.Context, method, url string, body, into any) (next string, err error)` — `body` nil: geen request-body; anders JSON. `into` nil: body niet decoderen. `get` wordt `do(ctx, http.MethodGet, url, nil, into)`.
  - `func (c *Client) CreateComment(ctx context.Context, number int, body string) (protocol.Comment, error)` — `POST /repos/{repo}/issues/{number}/comments` met `{"body": ...}`; vult `ID`, `URL`, `CreatedAt`, `Author`, `Body` uit het antwoord.
  - `func (c *Client) AddRocket(ctx context.Context, commentID int64) error` — `POST /repos/{repo}/issues/comments/{id}/reactions` met `{"content":"rocket"}`; 200 (bestond al) en 201 zijn beide succes.
  - `protocol.Comment` krijgt de velden `ID int64` en `Author string` (`internal/protocol/status.go:9`). In deze taak vult alleen `CreateComment` ze; Taak 2 laat `Comments` ze vullen.

- [ ] **Step 1: Schrijf de falende tests** in `write_test.go`, met `httptest.NewServer`:
  - `TestCreateCommentPosts`: server checkt `r.Method == "POST"`, pad `/repos/o/r/issues/7/comments`, body `{"body":"🤖 Vraag"}`, header `Content-Type: application/json`, `Authorization: Bearer secret-token`, `X-GitHub-Api-Version: 2022-11-28`; antwoordt 201 met `{"id":42,"html_url":"u","created_at":"2026-10-31T10:00:00Z","body":"🤖 Vraag","user":{"login":"lh-bot"}}`. Assert `got.ID == 42`, `got.Author == "lh-bot"`, `got.URL == "u"`.
  - `TestAddRocket`: pad `/repos/o/r/issues/comments/42/reactions`, body `{"content":"rocket"}`; subtests status 200 en 201 → `nil`; 403 met `{"message":"Resource not accessible"}` → fout die `POST`, `403` en `Resource not accessible` bevat en niet `secret-token`.
- [ ] **Step 2: Draai** `go test ./internal/forge/ -run 'TestCreateComment|TestAddRocket'` — verwacht FAIL (`undefined: CreateComment`).
- [ ] **Step 3: Implementeer** `do` (vervangt de body van `get`), `CreateComment` en `AddRocket`. `statusError` krijgt de methode als parameter: `forge <METHOD> <url>: status <n>[: <message>]` — voor GET blijft de tekst dus letterlijk zoals nu. Package-doc: "Package forge reads and writes GitHub issues, comments, reactions and the Projects v2 board. It never merges." `Client`-doc: geen "read-only" meer.
- [ ] **Step 4: Herschrijf de GET-garantie.** `fakeTransport` faalt niet meer op elke niet-GET, maar alleen als het veld `getOnly bool` aan staat; `newTestClient` zet het aan. `TestOnlyGET` heet `TestReadsOnlyGET` en blijft bewijzen dat `OpenIssues` en `Comments` alleen GET doen.
- [ ] **Step 5: Draai** `go test -race ./internal/forge/ ./internal/overview/ ./internal/cli/` — verwacht PASS.
- [ ] **Step 6: Commit** — `feat(forge): comments en 🚀 schrijven via één request-kern`

### Task 2: forge — comment-id en auteur, token-login, 🚀-logins, `role_name`

**Files:**
- Modify: `internal/forge/github.go` (`Comments` `:88-113`)
- Create: `internal/forge/people.go`, `internal/forge/people_test.go`
- Modify: `internal/forge/github_test.go` (`commentsJSON` `:108`)

**Interfaces:**
- Produces:
  - `protocol.Comment{Body, CreatedAt, URL string; Rockets int; ID int64; Author string}` — `Author` is `user.login`, `""` bij `user: null`.
  - `func (c *Client) Login(ctx context.Context) (string, error)` — `GET /user`, veld `login`.
  - `func (c *Client) RocketLogins(ctx context.Context, commentID int64) ([]string, error)` — `GET /repos/{repo}/issues/comments/{id}/reactions?content=rocket&per_page=100`, alle pagina's, `user.login` per reactie.
  - `func (c *Client) RoleName(ctx context.Context, user string) (string, error)` — `GET /repos/{repo}/collaborators/{user}/permission` (user met `url.PathEscape`), veld `role_name`. 404 → `"", nil`. Andere niet-2xx → fout.

- [ ] **Step 1: Schrijf de falende tests:**
  - `TestCommentsCarryIDAndAuthor` (in `github_test.go`): `commentsJSON` krijgt `"id"` en `"user":{"login":"u<i>"}`; assert `ID` en `Author` per comment, en `Author == ""` voor een item met `"user": null`.
  - `people_test.go`, met `httptest`: `TestLogin` (`{"login":"lh-bot"}` → `"lh-bot"`); `TestRocketLoginsFollowsPages` (pagina 1 met 100 reacties, `Link` naar pagina 2 met 1 reactie → 101 logins, laatste `"late"`); `TestRoleName` met subtests `maintain` → `"maintain"`, 404 → `"", nil`, 500 → fout die `500` bevat.
- [ ] **Step 2: Draai** `go test ./internal/forge/` — verwacht FAIL.
- [ ] **Step 3: Implementeer** de drie methoden in `people.go` en de twee velden in `Comments`.
- [ ] **Step 4: Draai** `go test -race ./internal/forge/ ./internal/overview/` — verwacht PASS.
- [ ] **Step 5: Commit** — `feat(forge): auteur, token-login, 🚀-logins en role_name lezen`

### Task 3: forge — GraphQL-kern, board, PR's per branch, board-status zetten

**Files:**
- Create: `internal/forge/graphql.go`, `internal/forge/board.go`, `internal/forge/board_test.go`
- Create: `internal/forge/forgetest/server.go` (testserver voor `forge`, `dispatch` en `cli`)

**Interfaces:**
- Produces:
  - `func (c *Client) graphql(ctx context.Context, query string, vars map[string]any, into any) error` — `POST BaseURL+"/graphql"` met `{"query","variables"}`; decodeert `data` in `into`; een niet-lege `errors` geeft een fout met de eerste `message`, ook bij status 200.
  - Types (met JSON-tags, ze gaan zo de snapshot in):
    ```go
    type PR struct {
        Number    int    `json:"number"`
        State     string `json:"state"`     // OPEN, CLOSED, MERGED
        Draft     bool   `json:"draft"`
        Mergeable string `json:"mergeable"` // MERGEABLE, CONFLICTING, UNKNOWN
        HeadSHA   string `json:"head_sha"`
        HeadRef   string `json:"head_ref"`
        Checks    string `json:"checks"`    // statusCheckRollup.state, "" without checks
        Closes    bool   `json:"closes"`    // in closedByPullRequestsReferences
    }
    type Blocker struct {
        Repo     string `json:"repo"`      // nameWithOwner
        Issue    int    `json:"issue"`
        State    string `json:"state"`     // OPEN, CLOSED
        MergedPR bool   `json:"merged_pr"` // a closing PR of the blocker is merged
    }
    type BoardItem struct {
        ItemID         string
        ItemUpdatedAt  time.Time
        SingleSelect   map[string]string // field ID → option ID
        Repo           string            // nameWithOwner; "" when the content is no issue
        Issue          int
        Title          string
        IssueState     string
        IssueUpdatedAt time.Time
        BlockedBy      []Blocker
        ClosingPRs     []PR
    }
    ```
  - `func (c *Client) BoardItems(ctx context.Context, projectID string) ([]BoardItem, error)` — `query Board($project: ID!, $after: String)`: `node(id: $project) { ... on ProjectV2 { items(first: 100, after: $after) { pageInfo { hasNextPage endCursor } nodes { id updatedAt fieldValues(first: 20) { nodes { ... on ProjectV2ItemFieldSingleSelectValue { optionId field { ... on ProjectV2SingleSelectField { id } } } } } content { ... on Issue { number title state updatedAt repository { nameWithOwner } blockedBy(first: 20) { nodes { number state repository { nameWithOwner } closedByPullRequestsReferences(first: 5, includeClosedPrs: true) { nodes { merged } } } } closedByPullRequestsReferences(first: 5, includeClosedPrs: true) { nodes { <prFields> } } } } } } } }`, met `<prFields>` = `number state isDraft mergeable headRefOid headRefName statusCheckRollup { state }`. Volgt `endCursor` tot `hasNextPage` false.
  - `func (c *Client) BranchPRs(ctx context.Context, branch string) ([]PR, error)` — `query BranchPRs($owner: String!, $name: String!, $branch: String!)`: `repository(owner:, name:) { pullRequests(headRefName: $branch, first: 10, states: [OPEN, CLOSED, MERGED]) { nodes { <prFields> } } }`; `Closes` false.
  - `func (c *Client) SetStatus(ctx context.Context, projectID, itemID, fieldID, optionID string) error` — `mutation SetStatus(...) { updateProjectV2ItemFieldValue(input: {projectId:, itemId:, fieldId:, value: {singleSelectOptionId:}}) { projectV2Item { id } } }`.
  - `forgetest.Server` (`forgetest.New(t *testing.T) *Server`, `URL()`): een `httptest.Server` die REST beantwoordt uit velden (`Login string`, `Comments map[int][]map[string]any`, `Roles map[string]string` — ontbrekende login → 404, `RoleStatus int` om 5xx te forceren, `Rockets map[int64][]string`, `Branches map[string][]map[string]any`) en GraphQL op operatienaam (`Board`, `BranchPRs`, `SetStatus`; de naam staat na `query `/`mutation ` aan het begin van de query). `Board` serveert `BoardNodes []map[string]any` in pagina's van 100, en `GraphQLErrors []string` als die gezet is. Het telt requests per pad of operatie (`Count(key string) int`) en bewaart de body's van POSTs (`Posts []Post{Path string; Body map[string]any}`). Het importeert `forge` niet.

- [ ] **Step 1: Schrijf de falende tests** in `board_test.go`, tegen `forgetest`:
  - `TestBoardItemsFollowsPages`: 101 nodes → 101 items, `Count("Board") == 2`, en de tweede request draagt `after` = de `endCursor` van de eerste.
  - `TestBoardItemsMapsFields`: één node met Status- en Priority-optie, een `blockedBy` met een gemergede sluitende PR, een sluitende PR met `statusCheckRollup: null` → `SingleSelect` heeft beide velden, `BlockedBy[0].MergedPR == true`, `ClosingPRs[0].Closes == true`, `Checks == ""`.
  - `TestBoardItemsNonIssueContent`: een draft-issue-node (`content: {}`) → `Repo == ""`, `Issue == 0`.
  - `TestGraphQLErrorsOn200` (Review Focus 1): `GraphQLErrors = ["Could not resolve to a node"]` → `BoardItems` geeft een fout met die tekst en `nil` items.
  - `TestBranchPRs`: variabelen `owner`, `name`, `branch` komen aan; één PR terug met `Closes == false`.
  - `TestSetStatusPosts`: de bewaarde body heeft `variables` met de vier ID's; geen andere mutatie dan `updateProjectV2ItemFieldValue` in de query.
- [ ] **Step 2: Draai** `go test ./internal/forge/...` — verwacht FAIL.
- [ ] **Step 3: Implementeer** `graphql.go` (op `do`, met URL `BaseURL + "/graphql"`), `board.go` en `forgetest`.
- [ ] **Step 4: Draai** `go test -race ./internal/forge/...` — verwacht PASS.
- [ ] **Step 5: Toets de queries één keer tegen GitHub** (alleen lezen, `dangerouslyDisableSandbox`): `gh api graphql -f query='<de Board-query>' -F project=<project_id uit lighthouse.toml>` en idem `BranchPRs` met een bestaande branch. Verwacht: `data` zonder `errors`. Een veld dat niet bestaat → query aanpassen, test bijwerken.
- [ ] **Step 6: Commit** — `feat(forge): board, PR-stand en board-status via GraphQL`

### Task 4: runs — de open runs van een repo lezen

**Files:**
- Modify: `internal/runs/runs.go`, `internal/runs/runs_test.go`

**Interfaces:**
- Produces:
  ```go
  type OpenRun struct {
      ID          int64     `json:"id"`
      Issue       int       `json:"issue"`
      SessionID   string    `json:"session_id"`
      TriggerKey  string    `json:"trigger_key"`
      Branch      string    `json:"branch"`
      WorkspaceID string    `json:"workspace_id"`
      StartedAt   time.Time `json:"started_at"`
  }
  func Open(ctx context.Context, db *sql.DB, repo string) ([]OpenRun, error) // ended_at IS NULL, ORDER BY issue
  ```
- Consumes: `Take`, `Release` (bestaand).

- [ ] **Step 1: Schrijf `TestOpenListsOnlyOpenRunsOfRepo`**: `Take` drie claims (repo `o/r` issues 9 en 3, repo `o/x` issue 5), `Release` die van issue 9 → `Open(ctx, db, "o/r")` geeft precies issue 3, met `StartedAt` gelijk aan `Claim.Now` (UTC, seconden).
- [ ] **Step 2: Draai** `go test ./internal/runs/ -run TestOpen` — verwacht FAIL.
- [ ] **Step 3: Implementeer** `Open`.
- [ ] **Step 4: Draai** `go test -race ./internal/runs/` — verwacht PASS.
- [ ] **Step 5: Commit** — `feat(runs): open runs per repo lezen`

### Task 5: dispatch — vertrouwde auteurs, 🚀 van het token, de snapshot

**Files:**
- Create: `internal/dispatch/trust.go`, `internal/dispatch/snapshot.go`, `internal/dispatch/gather.go`
- Create: `internal/dispatch/trust_test.go`, `internal/dispatch/gather_test.go`

**Interfaces:**
- Consumes: `forge.Client` (`BoardItems`, `BranchPRs`, `Comments`, `Login`, `RocketLogins`, `RoleName`), `forge.PR`, `forge.Blocker`, `runs.OpenRun`, `repoconfig.Repo`/`Board`, `protocol.Robot`, `forgetest`.
- Produces:
  ```go
  const SnapshotVersion = 1

  type Snapshot struct {
      Version    int            `json:"version"`
      Repo       string         `json:"repo"`
      TokenLogin string         `json:"token_login"`
      GatheredAt time.Time      `json:"gathered_at"`
      Items      []Item         `json:"items"`     // sorted by issue
      OpenRuns   []runs.OpenRun `json:"open_runs"`
  }
  type Item struct {
      Issue          int            `json:"issue"`
      Title          string         `json:"title"`
      State          string         `json:"state"`
      ItemID         string         `json:"item_id"`
      Status         string         `json:"status"`   // "Controller" or "On hold"
      Priority       string         `json:"priority"` // option name, "" when unset
      ItemUpdatedAt  time.Time      `json:"item_updated_at"`
      IssueUpdatedAt time.Time      `json:"issue_updated_at"`
      BlockedBy      []forge.Blocker `json:"blocked_by"`
      PRs            []forge.PR     `json:"prs"` // closing PRs plus PRs on the open run's branch, by number
      Comments       []Comment      `json:"comments"` // trusted authors only, oldest first
  }
  type Comment struct {
      ID        int64  `json:"id"`
      Author    string `json:"author"`
      Body      string `json:"body"`
      CreatedAt string `json:"created_at"`
      URL       string `json:"url"`
      Rocket    bool   `json:"rocket"` // a 🚀 from the token login
  }
  func (i Item) ProtocolComments() []protocol.Comment // Rockets = 1 when Rocket
  func ReadSnapshot(path string) (Snapshot, error)    // refuses another Version

  type Trust struct{ /* client, allowed set, cache login → trusted */ }
  func NewTrust(c *forge.Client, allowed []string) *Trust
  func (t *Trust) Trusted(ctx context.Context, login string) (bool, error) // "" → false, no request

  type Pre struct {
      Repo     repoconfig.Repo
      Items    []Item // board fields and PRs; no comments
      OpenRuns []runs.OpenRun
  }
  func Prepare(ctx context.Context, c *forge.Client, repo repoconfig.Repo, open []runs.OpenRun) (Pre, error)
  func Complete(ctx context.Context, c *forge.Client, pre Pre, trust *Trust, log io.Writer, now time.Time) (Snapshot, error)
  ```
  - `Prepare`: `BoardItems(Board.ProjectID)`; houdt alleen items met `Repo == repo.Slug`, `Issue > 0` en een Status-optie die volgens `Board.StatusOptions` `Controller` of `On hold` heet; Priority-naam via `Board.PriorityOptions`. Voor een item met een open run en een niet-lege `Branch`: `BranchPRs(branch)`, samengevoegd met de sluitende PR's, ontdubbeld op nummer (een PR in beide houdt `Closes: true`).
  - `Complete`: `Login` één keer; per item `Comments`; een comment van een niet-vertrouwde auteur valt weg, en begint zijn body met `protocol.Robot`, dan schrijft hij één regel `lighthouse: <slug>#<issue>: ignored 🤖-comment <url> by <author>: role not in answer_permissions` naar `log`. `Rocket` = `Rockets > 0` en `RocketLogins` bevat de token-login (geen reactie-request bij `Rockets == 0`).

- [ ] **Step 1: Schrijf de falende tests** (`forgetest` + een echte `forge.Client` met `BaseURL = srv.URL()`):
  - `TestTrustedRoles` (tabel): `admin`, `maintain` → true; `write`, `triage`, `read` → false; 404 → false zonder fout; `RoleStatus = 502` → fout. Plus `TestTrustCachesPerLogin`: twee keer `Trusted("ann")` → `Count` op de permissie-route van `ann` is 1.
  - `TestCompleteDropsUntrustedComments` (AC 1): issue 7 met comments `🤖 Vraag` door `owner` (admin), `ja, doe maar` door `stranger` (read), `🤖 Handoff` met `next: /lighthouse-start-issue 7` door `stranger`. Verwacht: `Items[0].Comments` bevat alleen de Vraag; `protocol.Classify(item.ProtocolComments()).State == protocol.StateQuestion`; `log` bevat precies één regel met `ignored 🤖-comment` en `by stranger`. Mutatiecheck: maak `Trusted` altijd true → test rood.
  - `TestCompleteDeletedAuthorIsUntrusted`: comment met `user: null` valt weg, zonder permissie-request.
  - `TestRocketOnlyFromTokenLogin` (AC 2): token-login `lh-bot`; comment A met 🚀 van `someone`, comment B met 🚀 van `someone` en `lh-bot`, comment C zonder 🚀 → `Rocket` is false, true, false; geen reactie-request voor C.
  - `TestPrepareFiltersBoard` (Review Focus 5): nodes voor issue 1 (Controller, `o/r`), 2 (Done), 3 (On hold, repo `o/x`), een draft-issue en issue 4 (On hold, `o/r`) → `Items` zijn 1 en 4, `Status` `Controller` en `On hold`.
  - `TestPrepareMergesBranchPRs`: open run op issue 1 met branch `issue-1-x`; sluitende PR 10 en branch-PR's 10 en 11 → `PRs` nummers `[10 11]`, PR 10 `Closes == true`, PR 11 `Closes == false`; geen `BranchPRs`-request voor issue 4 zonder open run.
  - `TestCompleteFailsOnPermissionError`: `RoleStatus = 500` → `Complete` geeft een fout.
- [ ] **Step 2: Draai** `go test ./internal/dispatch/` — verwacht FAIL.
- [ ] **Step 3: Implementeer** `trust.go`, `snapshot.go`, `gather.go`.
- [ ] **Step 4: Draai** `go test -race ./internal/dispatch/` — verwacht PASS.
- [ ] **Step 5: Commit** — `feat(dispatch): snapshot met alleen vertrouwde auteurs en 🚀 van het token`

### Task 6: dispatch — vingerafdruk, precheck en tickstand

**Files:**
- Create: `internal/dispatch/fingerprint.go`, `internal/dispatch/precheck.go`
- Create: `internal/dispatch/fingerprint_test.go`, `internal/dispatch/precheck_test.go`

**Interfaces:**
- Consumes: `Pre`, `Item` (Taak 5), `runs.OpenRun`, `hostconfig.Dispatch` (`MaxRuntime`, `MaxWait`, `Night`), `store.OpenShared`.
- Produces:
  ```go
  func Fingerprint(pre Pre) string // hex SHA-256

  const FullGatherAfter = 30 * time.Minute

  type TickState struct {
      Fingerprint string
      FullAt      time.Time // last full gather
  }
  func LoadTickState(ctx context.Context, db *sql.DB, repo string) (TickState, error) // no rows → zero value
  func SaveTickState(ctx context.Context, db *sql.DB, repo string, s TickState, now time.Time) error
  func Boundaries(d hostconfig.Dispatch, open []runs.OpenRun, now time.Time) []time.Time
  func Due(prev TickState, fp string, now time.Time, boundaries []time.Time) (due bool, reason string)
  ```
  - `Fingerprint` hasht de canonieke JSON van, per item gesorteerd op issue: `issue`, `status`, `priority`, `item_updated_at`, `issue_updated_at`; de voorgangers (`repo`, `issue`, `state`, `merged_pr`, gesorteerd) alleen voor items in On hold; de PR's (`number`, `state`, `draft`, `mergeable`, `head_sha`, `checks`, gesorteerd op nummer) alleen voor items met een open run; en de open runs (`id`, `issue`, `started_at`, gesorteerd op id). Comments zitten er niet in: een nieuwe comment verschuift `issue_updated_at`.
  - `Due`, in deze volgorde: `prev.Fingerprint == ""` → `true, "first"`; `fp != prev.Fingerprint` → `"changed"`; `now.Sub(prev.FullAt) >= FullGatherAfter` → `"stale"`; een grens `b` met `prev.FullAt < b && !b.After(now)` → `"boundary"`; anders `false, "unchanged"`.
  - `Boundaries`: per open run `StartedAt + MaxRuntime` en `StartedAt + MaxWait`; en het begin van het `night`-venster vandaag en gisteren, in de tijdzone van `now`.
  - `LoadTickState`/`SaveTickState`: rijen `(repo, 'fingerprint', 'value', <hex>)` en `(repo, 'fingerprint', 'full_at', <RFC 3339 UTC>)`, upsert in één transactie.

- [ ] **Step 1: Schrijf de falende tests:**
  - `TestFingerprintChangesOnMergedBlocker` (AC 4): item in On hold met voorganger `{o/r, 5, OPEN, false}`; dan `MergedPR: true` → andere vingerafdruk; ook `State: CLOSED` → andere.
  - `TestFingerprintChangesOnGreenCheck` (AC 4): item met open run en PR `Checks: "PENDING"` → `"SUCCESS"` geeft een andere vingerafdruk; dezelfde wijziging op een item zónder open run niet.
  - `TestFingerprintIgnoresOrder` (Review Focus 4): items, PR's en voorgangers omgekeerd → zelfde vingerafdruk.
  - `TestFingerprintIgnoresComments`: comments toevoegen aan een `Item` → zelfde vingerafdruk.
  - `TestDue` (AC 5, tabel, `now = 2026-10-31T12:00:00Z`, `prev = {fp "a", FullAt now-10m}`): zelfde fp, geen grens → `false, "unchanged"`; `FullAt now-29m59s` → false; `FullAt now-30m` → `true, "stale"`; grens `now-5m` → `true, "boundary"`; grens `now-11m` (vóór `FullAt`) → false; grens `now+1m` → false; fp `"b"` → `"changed"`; lege `prev` → `"first"`.
  - `TestBoundaries`: run gestart `10:00Z`, `MaxRuntime 8h`, `MaxWait 1h`, `Night "21:00-08:00"`, `now` in UTC → bevat `18:00Z`, `11:00Z`, `21:00Z` vandaag en `21:00Z` gisteren.
  - `TestTickStateRoundTrip`: `store.OpenShared` op `t.TempDir()`; `LoadTickState` zonder rijen → nulwaarde; `Save` en `Load` → gelijk; tweede `Save` overschrijft.
- [ ] **Step 2: Draai** `go test ./internal/dispatch/` — verwacht FAIL.
- [ ] **Step 3: Implementeer** `fingerprint.go` en `precheck.go`. Het nachtvenster parse je als `HH:MM` voor het streepje; `hostconfig.LoadDispatch` heeft het al gevalideerd.
- [ ] **Step 4: Draai** `go test -race ./internal/dispatch/` — verwacht PASS.
- [ ] **Step 5: Commit** — `feat(dispatch): vingerafdruk en precheck met tickstand`

### Task 7: cli — precheck en gather in `tick`, `lighthouse gather`, docs

**Files:**
- Modify: `internal/cli/tick.go` (`tickEnv` `:21`, de repolus `:94-104`), `internal/cli/tick_test.go`, `internal/cli/root.go:37-40`
- Create: `internal/cli/gather.go`, `internal/cli/gather_test.go`
- Create: `internal/dispatch/testdata/snapshot.json`, `internal/dispatch/snapshot_test.go`
- Modify: `docs/tick.md`; Create: `changelog.d/issue-112-forge-snapshot.md`

**Interfaces:**
- Consumes: alles uit Taak 4–6.
- Produces:
  - `tickEnv` krijgt `ForgeURL string` (leeg → `forge.DefaultBaseURL`) en `Now func() time.Time` (nil → `time.Now`).
  - `func gatherRepo(ctx context.Context, env tickEnv, db *sql.DB, cfg hostconfig.Dispatch, repo repoconfig.Repo, token string, errOut io.Writer) error` — `runs.Open` → `Prepare` → `Fingerprint` → `LoadTickState` → `Due`; niet due: niets schrijven. Due: `Complete`, dan `SaveTickState(fp, FullAt: now)` en de regel `lighthouse: gathered <slug>: <n> items (<reason>)` op `errOut`. Tot #114 bewaart tick de stand na elke volledige gather; vanaf #114 alleen als geen actie is uitgesteld (spec §3).
  - Een fout in `gatherRepo` is een luide skip (`lighthouse: skip <pad>: <fout>`, exit 1) en bewaart geen stand.
  - `lighthouse gather <repo-dir> [--db]`: zelfde token, `host.toml` en database als `tick`, geen `tick.lock`, geen precheck, geen tickstand; schrijft de snapshot als JSON (twee spaties inspringing, slotnewline) naar stdout.

- [ ] **Step 1: Schrijf de falende tests:**
  - `TestTickGathersThenSkipsUnchanged` (AC 5, integratie): `forgetest` met één Controller-item; `env.ForgeURL = srv.URL()`, vaste `Now`. Tick 1 → `errOut` bevat `gathered o/r: 1 items (first)` en `Count` op de comments-route is 1. Tick 2 met `Now` + 3m → geen `gathered`-regel, comments-`Count` blijft 1, `Count("Board")` is 2. Tick 3 met `Now` + 31m → `(stale)`.
  - `TestTickForgeErrorSkipsLoudlyAndKeepsState`: `GraphQLErrors` gezet → exit `errSilent`, `errOut` bevat `skip` en de GraphQL-melding; `LoadTickState` blijft leeg.
  - `TestGatherCommandMatchesFixture` (AC 3): `forgetest` met een Controller-item met een Vraag en een vertrouwd antwoord, een On hold-item met een voorganger, een open run met branch-PR; uitvoer van `lighthouse gather` is byte-gelijk aan `internal/dispatch/testdata/snapshot.json`. Met de vlag `-update` schrijft de test het bestand.
  - `TestReadSnapshotFixture` (in `internal/dispatch/snapshot_test.go`, AC 3): `ReadSnapshot("testdata/snapshot.json")` lukt, `Version == 1`, en `json.Marshal` daarvan, ingesprongen, is gelijk aan het bestand. Een kopie met `"version": 2` → fout.
- [ ] **Step 2: Draai** `go test ./internal/cli/ ./internal/dispatch/` — verwacht FAIL.
- [ ] **Step 3: Implementeer** `gatherRepo` in `tick.go`, `gather.go` met `newGatherCommand` in `root.go`.
- [ ] **Step 4: Maak de fixture:** `go test ./internal/cli/ -run TestGatherCommandMatchesFixture -update`; lees het bestand na: geen token, de Vraag en het antwoord staan erin, de comment van een vreemde auteur niet.
- [ ] **Step 5: Draai** `go test -race -shuffle=on ./...` en `golangci-lint run` — verwacht PASS en geen bevindingen.
- [ ] **Step 6: Docs.** `docs/tick.md`, *Wat tick nu doet*: stap 6 *Precheck* (de inputs van de vingerafdruk, 30 minuten, de tijdgrenzen, `tick_state`), stap 7 *Gather* (wat de snapshot bevat, vertrouwde auteurs via `role_name`, 🚀 alleen van de token-login), de regel `lighthouse: gathered …`; de zin "Tick haalt nog geen issues op" wordt "Tick beslist nog niet en start nog geen run: decide en apply volgen in #114". Nieuwe sectie *`lighthouse gather`* (gebruik, uitvoer is de fixture voor decide). Bij *Token*: scope `repo` en `project` (schrijven van comments, reacties en het board). Changelog-fragment, `type: added`, één zin naar het model van `changelog.d/issue-111-runstaat-claim-tick.md`.
- [ ] **Step 7: Commit** — `feat(tick): precheck en gather per repo, en lighthouse gather` (code) en `docs(tick): precheck, gather en lighthouse gather` (docs en changelog).
