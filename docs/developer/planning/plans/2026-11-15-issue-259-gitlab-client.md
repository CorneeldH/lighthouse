# GitLab-client (#259) — implementatieplan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Een GitLab-client (`forge.GitLab`) achter `forge.Forge` uit #258, getest tegen een nep-GitLab-server met dezelfde conformiteitssuite als GitHub, en zichtbaar in het overzicht van `serve`.

**Architecture:** `internal/forge/gitlab*.go` implementeert de 19 methoden van `Forge` met REST v4; alleen de ingebouwde issuestatus gaat via GraphQL (`WorkItemWidgetStatus`, `workItemUpdate`). Een nieuw testpakket `internal/forge/gitlabtest` is de nepserver (model: `forgetest`). `gitlab_contract_test.go` draait `forgesuite.Run` ertegen. Daarna volgen de GitLab-vorm van `[forge.board]` met validatie per soort, de basis-URL per soort in de opener, en de kleine afnemerskanten (`serve`, `run`, `dispatch.Prepare`).

**Tech Stack:** Go (stdlib `net/http`, `encoding/json`, `testing`); Python alleen als een nieuwe sleutel een `SCHEMA`-regel nodig heeft.

**Spec:** [2026-11-08-forge-interface-design.md](../specs/2026-11-08-forge-interface-design.md). Dit plan dekt *#259 levert*, punt 1 tot 6. Plan van #258: [2026-11-08-issue-258-forge-interface.md](2026-11-08-issue-258-forge-interface.md).

## Global Constraints

- GitLab REST API v4: basis `https://<[forge] host>/api/v4`, zonder `host` `https://gitlab.com/api/v4`. Minimum dat we testen: GitLab 19.4.
- Een project-ID in een URL is het pad met `/` als `%2F` (subgroepen: `groep/sub/project`).
- Token nooit naar een host van een andere forge; `Token` header: `PRIVATE-TOKEN`. `forge.Token` kent `GITLAB_TOKEN` al.
- Aanroepbudget: `BoardItems` kost een vast aantal pagina's, geen aanroep per issue per tick. Alleen de uitzondering uit de spec: één aanroep per open run buiten het board.
- Statusnamen hoofdletterongevoelig bij `workItemUpdate`; een onbekende statuskoppeling geeft een fout met de statusnaam en de plek waar je hem toevoegt (*Settings → Work items → Statuses*, bovenste groep).
- `Issue.State` `opened`→`open`; `BoardItem.IssueState`/`Blocker.State` `opened`→`OPEN`, `closed`→`CLOSED`; `Milestone.State` `active`→`open`. Systeemnotes (`system: true`) tellen niet als comment.
- Rollen: 10→`read`; 15 en 20→`triage`; 30→`write`; 40→`maintain`; 50→`admin`; geen lid→`""`. Het user-ID komt uit `GET /users?username=`, per tick gecachet.
- GitHub-gedrag verandert niet: de GitHub-tests blijven inhoudelijk gelijk. Geen derde forge, lighthouse merget nooit uit zichzelf (`MergePR` bestaat alleen omdat de interface hem eist).
- Code, namen en commentaar Engels; artefacten Nederlands. `/lighthouse-go-standard` en `/lighthouse-test-standard`. Elke commit groen; nooit `--no-verify`.
- `pack/core/lib/config.py` is bevroren: een nieuwe sleutel alleen als `SCHEMA`-regel, met de pariteitstests gelijk. Wijzigt `pack/` of wat `init` schrijft, dan ook `go run ./cmd/lighthouse init --upgrade` en de kopie committen.

## Besluiten van dit plan (spec laat ze aan de bouwer)

- **Status:** de ingebouwde issuestatus (spec). Het AC "scoped label `status::<naam>`" is achterhaald; Taak 8 past het AC aan, zoals spec *#259 levert* 6 vraagt.
- **`[forge.board]` voor GitLab:** alleen `status_options` (lighthouse-naam → GitLab-statusnaam, bijvoorbeeld `"To do" = "To do"`) en `priority_options` (lighthouse-naam → label, bijvoorbeeld `"P1" = "priority::P1"`). `project_number`, `project_id`, `status_field_id` en `priority_field_id` zijn voor GitLab niet toegestaan en niet nodig.
- **Raketten:** de notes-lijst bevat geen award emoji. `Comments` haalt `GET …/notes/:id/award_emoji` alleen voor notes waarvan `protocol.ParseHeader(body)` een lighthouse-header vindt, en telt `rocket`. Andere notes krijgen `Rockets = 0`.
- **Venster gesloten issues in `BoardItems`:** gesloten issues met `updated_at` binnen de laatste 14 dagen (constante `closedWindow` in `gitlab_board.go`), samen met alle open issues; `dispatch.Prepare` vangt de rest op via `IssueItem`.
- **Fake:** `internal/forge/gitlabtest` is een eigen pakket, geen uitbreiding van `forgetest`; dezelfde regel: een onverwacht verzoek laat de test falen.

## Review Focus

- Een project onder een subgroep (`g/sub/p`): elk pad is URL-gecodeerd en de fake ziet `g%2Fsub%2Fp`. Test in Taak 2.
- Paginering via `x-next-page` en 101 records: geen record dubbel of kwijt. Test in Taak 2 (suite `Pagination`).
- Een issue met een open run dat uit het venster van `BoardItems` valt: de run sluit af. Test in Taak 7.
- Een statuskoppeling die niet bestaat in de lifecycle: foutmelding noemt status en plek. Test in Taak 5.
- Een self-hosted host met pad-prefix of een `BaseURL` met slash achteraan: geen dubbele `/`. Test in Taak 2.

---

### Taak 1: Nep-GitLab-server

**Files:**
- Create: `internal/forge/gitlabtest/server.go`, `internal/forge/gitlabtest/server_test.go`

**Interfaces:**
- Consumes: niets.
- Produces: `gitlabtest.Server` met `New(t *testing.T) *Server`, `URL() string` (eindigt op `/api/v4`), veld `Login string`, en seed-methoden die de fixture van Taak 2 tot 5 gebruikt: `AddIssue(m map[string]any)`, `AddNote(iid int, author, body string, system bool) int64`, `AddAward(iid int, noteID int64, name, user string)`, `SetMember(user string, accessLevel int)`, `AddLink(iid int, link map[string]any)`, `AddMR(m map[string]any)`, `SetStatuses(names ...string)` (de lifecycle van de bovenste groep), `SetWorkItemStatus(iid int, name string)`, `WorkItemStatus(iid int) string`, `Requests() []string`. Een niet-bekend pad of project geeft 404 en `t.Errorf`.

- [ ] **Step 1: Schrijf `TestServerRejectsUnknownRoute`, `TestServerPaginates`, `TestServerDecodesEncodedProject`** in `server_test.go`: een onbekend pad laat de test falen; 101 issues geven pagina 1 met 100 en header `x-next-page: 2`, pagina 2 met 1 en een lege `x-next-page`; project `g%2Fsub%2Fp` wordt herkend, `g/sub/p` zonder codering niet.
- [ ] **Step 2: Draai ze, verwacht FAIL** — `go test ./internal/forge/gitlabtest/`.
- [ ] **Step 3: Implementeer `gitlabtest.Server`** op `httptest.Server`. Routes onder `/api/v4`: `user`, `users?username=`, `projects/:id/members/all/:uid`, `projects/:id/issues` en `/:iid`, `…/notes` (POST en GET, met `page`/`per_page`), `…/notes/:nid/award_emoji` (GET, POST), `…/links`, `…/merge_requests`, `…/merge_requests/:iid/merge`, `groups`-milestones niet; `POST /api/graphql` met de twee operaties uit Taak 5 (herken op operatienaam in `query`). Paginering met `x-next-page`. Eén generieke `paginate(w, r, items)`-helper.
- [ ] **Step 4: Draai de tests, verwacht PASS.**
- [ ] **Step 5: Commit** — `feat(forge): nep-GitLab-server voor de conformiteitstests (#259)`.

### Taak 2: GitLab-client, issues en comments

**Files:**
- Create: `internal/forge/gitlab.go`, `internal/forge/gitlab_test.go`, `internal/forge/gitlab_contract_test.go`
- Modify: `internal/forge/forge.go:44` (compile-check) en `:70-71` (Open), `internal/forge/forge_test.go:38`

**Interfaces:**
- Consumes: `gitlabtest.Server` uit Taak 1; `forgesuite.Fixture` uit `internal/forge/forgesuite/suite.go:20-45`.
- Produces: `type GitLab struct` met `Kind() string` = `"gitlab"`; `func (g *GitLab) get(ctx, path string, q url.Values, out any) (next string, err error)` en `getAll[T any](ctx, g, path string, q url.Values) ([]T, error)` (volgt `x-next-page`, `per_page=100`); `func (g *GitLab) project() string` (URL-gecodeerd pad); methoden `Login`, `Role`, `Issue`, `OpenIssues`, `ClosedInMilestone`, `Comments`, `CreateComment`. `Open` geeft voor `kind = "gitlab"` een `*GitLab`, met `cfg.BaseURL` of `https://<host|gitlab.com>/api/v4` (slashes achteraan weg).
- Fixture: `gitlabFixture` in `gitlab_contract_test.go` implementeert `forgesuite.Fixture`; methoden voor latere taken beginnen met `t.Skip("taak N")` en worden daar ingevuld.

- [ ] **Step 1: Schrijf `TestGitLabConformance`** (`forgesuite.Run(t, newGitLabFixture)`) en de fixture voor `AddIssue`, `AddComment`, `SetRole`, `Login`, `Repo` (`"g/sub/p"`). Schrijf `TestOpenGitLabBaseURL` in `gitlab_test.go`: tabel `{host "" → https://gitlab.com/api/v4, host "git.example.nl" → https://git.example.nl/api/v4, BaseURL "http://x/api/v4/" → "http://x/api/v4"}`; `TestGitLabEncodesSubgroupPath` (de fake ziet `g%2Fsub%2Fp`).
- [ ] **Step 2: Draai, verwacht FAIL** — `go test ./internal/forge/ -run 'GitLab'`.
- [ ] **Step 3: Implementeer `GitLab` en de zeven methoden** volgens de tabel van de spec. `Issue.Number` is de `iid`, `URL` is `web_url`, `Author` is `author.username`. `Role`: `GET /users?username=` (cache per `GitLab`-instantie, vers per tick doordat de opener per tick opent; controleer in `internal/cli/forges.go`) en dan `/members/all/:id`; een 404 geeft `""`. `ClosedInMilestone` filtert op `milestone=<m.Title>` en `state=closed`, `updated_after=since`. Pas `forge_test.go:38` aan: `Open` met `kind = "gitlab"` geeft geen fout meer.
- [ ] **Step 4: Draai, verwacht PASS** voor de suitecases `Login`, `Role`, `IssueFields`, `OpenIssuesPages`, `ClosedInMilestone`, `Comments`, `Pagination` (voor issues); de rest `SKIP`.
- [ ] **Step 5: Commit** — `feat(forge): GitLab-client voor issues en comments (#259)`.

### Taak 3: Bevestigen en blokkades

**Files:**
- Create: `internal/forge/gitlab_people.go`
- Modify: `internal/forge/gitlab.go` (`Comments`), `internal/forge/gitlab_contract_test.go`

**Interfaces:**
- Consumes: `getAll`, `project()` uit Taak 2; `gitlabtest.Server.AddAward`, `AddLink`.
- Produces: `Acknowledge(ctx, number int, commentID int64) error` (`POST …/notes/:id/award_emoji?name=rocket`; een 404 of 409 "already awarded" is geen fout), `Acknowledgers(ctx, number int, commentID int64) ([]string, error)`, `BlockedBy(ctx, number int) ([]Blocker, error)` (links met `link_type = is_blocked_by`; `Repo` uit het project-pad van de blokkeerder, `MergedPR` waar een MR uit `closed_by` van de blokkeerder `merged` is).

- [ ] **Step 1: Vul de fixture** (`AddAcknowledgement`, `AddBlocker`) en schrijf `TestGitLabCommentsCountRocketsOnlyOnLighthouseNotes`: een gewone note krijgt geen `award_emoji`-verzoek (`Requests()` bevat het pad niet); een note met lighthouse-header krijgt `Rockets` = aantal `rocket`.
- [ ] **Step 2: Draai, verwacht FAIL** voor `Acknowledge`, `BlockedBy` en de nieuwe test.
- [ ] **Step 3: Implementeer de drie methoden** en breid `Comments` uit met de raketten volgens het besluit hierboven.
- [ ] **Step 4: Draai, verwacht PASS** voor `Comments`, `Acknowledge`, `BlockedBy`.
- [ ] **Step 5: Commit** — `feat(forge): GitLab-client bevestigt en leest blokkades (#259)`.

### Taak 4: Merge requests

**Files:**
- Create: `internal/forge/gitlab_mr.go`
- Modify: `internal/forge/gitlab_contract_test.go`

**Interfaces:**
- Consumes: `getAll`, `project()`; `OpenPR` en `PR` uit `internal/forge/issues.go:19` en `board.go:13`.
- Produces: `OpenPRs(ctx) ([]OpenPR, error)`, `BranchPRs(ctx, branch string) ([]PR, error)`, `MergePR(ctx, number int, sha string) error` (`PUT …/merge` met `sha` en `squash=true`; `409` → `ErrHeadMoved`; `405`/`406` → `*MergeRefused`), en `func mrToPR(mr gitlabMR) PR` met de vertaaltabel uit de spec (State, Checks via `head_pipeline.status`, Mergeable via `has_conflicts`/`detailed_merge_status`, `CrossRepo` via `source_project_id` ≠ `target_project_id`, `Closes` via `closes_issues`).

- [ ] **Step 1: Schrijf `TestMRToPR`** als tabeltest met de rijen van de spec: `opened`/`closed`/`merged`/`locked`; elke pipelinestatus; `unchecked`/`checking`/`preparing` → UNKNOWN; `has_conflicts` → CONFLICTING; geen pipeline → `""`. Vul de fixture (`AddOpenPR`, `AddBranchPR`).
- [ ] **Step 2: Draai, verwacht FAIL.**
- [ ] **Step 3: Implementeer `mrToPR` en de drie methoden.** `Closes` en `MergedAt`/`ClosedAt` komen uit de MR zelf; geen aanroep per MR als de lijst ze al levert (`closes_issues` komt alleen via een extra aanroep: één per MR die `description` een `Closes`-verwijzing heeft, anders niet; documenteer de keuze in een commentaar).
- [ ] **Step 4: Draai, verwacht PASS** voor `PRFields` en `TestMRToPR`.
- [ ] **Step 5: Commit** — `feat(forge): GitLab-client leest en merget merge requests (#259)`.

### Taak 5: Board — status en prioriteit

**Files:**
- Create: `internal/forge/gitlab_board.go`, `internal/forge/gitlab_graphql.go`
- Modify: `internal/repoconfig/repoconfig.go:62-69` (alleen indien GitLab een extra veld nodig heeft; het besluit hierboven vraagt er geen), `internal/forge/gitlab_contract_test.go`

**Interfaces:**
- Consumes: `repoconfig.Board.StatusOptions` en `PriorityOptions` (voor GitLab: lighthouse-naam → GitLab-statusnaam resp. label); `gitlabtest.Server.SetStatuses`, `SetWorkItemStatus`, `WorkItemStatus`.
- Produces: `BoardItems(ctx) ([]BoardItem, error)` (GraphQL: één query per pagina van 100 work items met `WorkItemWidgetStatus`, labels, `updatedAt`, `blockedByItems` en state; alle open plus gesloten binnen `closedWindow = 14 * 24 * time.Hour`); `IssueItem(ctx, number int) (BoardItem, bool, error)` (altijd `ok = true` voor een bestaand issue); `AddToBoard(ctx, number int) (BoardItem, error)` (geen schrijfactie, geeft `IssueItem`); `SetStatus(ctx, item BoardItem, status string) error` (`workItemUpdate` met `statusWidget: {name: …}`; `item.ItemID` is de `iid`); `Status` van een niet-gekoppelde GitLab-statusnaam is `unknown:<ruwe naam>`; `Priority` is de lighthouse-naam van het label `priority::…` dat in `PriorityOptions` staat, anders `""`.

- [ ] **Step 1: Schrijf de tests:** `TestGitLabSetStatusMissingLifecycleStatus` (de fake kent de status niet: de fout bevat de GitLab-statusnaam en `Settings → Work items → Statuses`), `TestGitLabBoardItemsPageCount` (101 issues kosten vaste pagina's: `Requests()` telt 2 GraphQL-verzoeken, niet 101), `TestGitLabBoardItemsClosedWindow` (een issue 30 dagen gesloten ontbreekt, 3 dagen gesloten staat erin). Vul `PutOnBoard`, `PutOnBoardUnmapped`, `StatusOf` van de fixture; de suitecases `BoardStatusAndPriority`, `IssueItem`, `AddToBoardThenSetStatus`, `SetStatusUnknownName` draaien nu.
- [ ] **Step 2: Draai, verwacht FAIL.**
- [ ] **Step 3: Implementeer de vijf methoden** en een kleine `graphql(ctx, query string, vars map[string]any, out any) error` die GraphQL-fouten als fout teruggeeft. `Controller` en `On hold` bestaan alleen als de gebruiker ze toevoegde; de fout uit `workItemUpdate` voor een onbekende naam wordt verpakt met de plek om hem toe te voegen.
- [ ] **Step 4: Draai de hele suite, verwacht PASS** — `go test -race ./internal/forge/...`.
- [ ] **Step 5: Commit** — `feat(forge): GitLab-board met de ingebouwde issuestatus (#259)`.

### Taak 6: Configuratie per soort forge

**Files:**
- Modify: `internal/config/config.go:414-460` (`board`), `internal/config/config_test.go`, `docs/site/data/config.json` (alleen als de bestaande sleutels hun beschrijving per soort nodig hebben), `docs/install.md:197`

**Interfaces:**
- Consumes: `Config.Kind` (`internal/config/config.go:565`), `Board` (`:231`).
- Produces: `board()` valideert voor `kind = "gitlab"`: `status_options` en `priority_options` zijn tabellen met niet-lege strings; `project_number`, `project_id`, `status_field_id`, `priority_field_id` zijn fout met `key forge.board.<sleutel> is only valid with kind = 'github'`. Voor GitHub verandert niets.

- [ ] **Step 1: Schrijf tests** in `config_test.go`: een geldige GitLab-board geeft geen fout; een GitLab-board met `project_number` geeft de fout hierboven; een GitHub-board zonder `project_id` geeft nog steeds de bestaande fout; een lege statusnaam voor GitLab geeft een fout.
- [ ] **Step 2: Draai, verwacht FAIL** — `go test ./internal/config/`.
- [ ] **Step 3: Pas `board()` aan** zodat de verplichte GitHub-sleutels alleen voor GitHub gelden. Draai de pariteitstests (`tests/pack/test_config_fixtures.py`, `tests/test_site_config.py`); ze moeten groen blijven zonder nieuwe `SCHEMA`-regel. Zo niet: één regel in `SCHEMA`, `tests/pack/fixtures/config-schema.json` en `docs/site/data/config.json`, en `init --upgrade`.
- [ ] **Step 4: Draai de tests, verwacht PASS** — `go test ./internal/config/ ./internal/repoconfig/` en de twee Python-tests.
- [ ] **Step 5: Commit** — `feat(config): [forge.board] voor GitLab met validatie per soort (#259)`.

### Taak 7: `serve`, `run` en `dispatch.Prepare`

**Files:**
- Modify: `internal/cli/serve.go:65-67,105`, `internal/cli/forges.go:61-70`, `internal/cli/run.go:46-47`, `internal/dispatch/` (de `Prepare`-functie), met tests ernaast: `internal/cli/serve_test.go`, `internal/cli/forges_test.go`, `internal/cli/run_test.go`, `internal/dispatch/prepare_test.go` (of het bestaande testbestand van `Prepare`)

**Interfaces:**
- Consumes: `forge.Open` met `kind = "gitlab"` en `Config.BaseURL` (Taak 2); `Forge.IssueItem` (Taak 5).
- Produces: `forRepo` zet voor GitLab-repo's `BaseURL` uit een nieuwe kleine veld `gitlabURL` van `forgeOpener` (leeg: de standaard van `Open`); `--forge-url` en `LIGHTHOUSE_FORGE_URL` blijven alleen voor GitHub (nu al zo: `forges.go:63-65`). `runTarget` accepteert `groep/sub/project#5` (splitst op de laatste `#`). `dispatch.Prepare` haalt via `IssueItem` het item op van elk issue met een open run dat niet in `BoardItems` zit — eerst `grep -n IssueItem internal/dispatch/*.go`: staat dit er al sinds #258, sla de implementatie dan over en houd alleen de test.

- [ ] **Step 1: Schrijf de tests:** `TestServeShowsGitLabRepo` (een `[dispatch] repos`-regel met `kind = "gitlab"` en een opener die `gitlabtest` gebruikt: het overzicht bevat de open issues en comments van die repo, en geen verzoek gaat naar `api.github.com`); `TestRunTargetSubgroup` (`g/sub/p#5` → repo `g/sub/p`, nummer 5; `g/p#5` en `o/r#5` blijven werken; `g/sub/p` zonder `#` is een fout); `TestPrepareClosesRunOutsideBoardWindow` (een gesloten issue met een open run dat niet in `BoardItems` zit: `Prepare` roept `IssueItem` aan en de run sluit af).
- [ ] **Step 2: Draai, verwacht FAIL** — `go test ./internal/cli/ ./internal/dispatch/ -run 'GitLab|Subgroup|OutsideBoard'`.
- [ ] **Step 3: Implementeer de drie wijzigingen.** `serve.go:105` houdt `Kind: "github"` voor de losse `--repo`; een GitLab-repo komt alleen via `[dispatch] repos`. Pas `serve.go:65-67` en de docstring aan zodat duidelijk is dat `--forge-url` alleen GitHub geldt.
- [ ] **Step 4: Draai, verwacht PASS** — `go test -race ./internal/cli/... ./internal/dispatch/... ./internal/overview/...`.
- [ ] **Step 5: Commit** — `feat(cli): serve toont een GitLab-repo, run accepteert subgroepen (#259)`.

### Taak 8: Documentatie, AC en afronding

**Files:**
- Create: `changelog.d/issue-259-gitlab-client.md`
- Modify: `docs/install.md:197` (de melding "GitLab client not implemented yet" vervalt; beschrijf `kind = "gitlab"`, `[forge] host`, `GITLAB_TOKEN`, de statussen *On hold* en *Controller* op de bovenste groep en de `[forge.board]`-vorm), relevante pagina onder `docs/` die `[forge.board]` beschrijft

**Interfaces:** geen.

- [ ] **Step 1: Schrijf de documentatie en het changelog-fragment** (`type: feature`, Nederlands, één alinea; kijk naar een bestaand fragment voor het formaat).
- [ ] **Step 2: Pas het AC van #259 aan** met `gh issue edit 259 --repo CorneeldH/lighthouse --body-file <bestand>`: het derde AC wordt "De status van een issue leest en schrijft via de ingebouwde issuestatus van GitLab; de namen komen uit de configuratie (`[forge.board] status_options`)". Doe dit pas als de rest groen is, zodat `/lighthouse-write-pr` tegen het gewijzigde AC controleert.
- [ ] **Step 3: Volledige gate** — `lighthouse gate go -- go test -race` en de twee Python-tests; daarna `go run ./cmd/lighthouse init --upgrade`, verwacht `No changes.`.
- [ ] **Step 4: Handmatige run (niet in CI):** de milestone-DoD (één echt issue tot een MR op de interne GitLab) hoort bij #260 en een gebruiker; noteer in de PR dat deze run niet door #259 bewezen is.
- [ ] **Step 5: Commit** — `docs(forge): GitLab-client, config en changelog (#259)`.

---

## Self-review

- **Dekking:** AC 1 (REST v4, gitlab.com en self-hosted) → Taak 2 (`TestOpenGitLabBaseURL`); AC 2 (fake server, issues, notes, MR's, labels, paginering) → Taak 1 tot 5; AC 3 (status) → Taak 5 en 8; AC 4 (`serve`) → Taak 7. Spec #259 levert 1 → Taak 2–5, 2 → Taak 1–5, 3 → Taak 6, 4 → Taak 7, 5 → Taak 7, 6 → Taak 8.
- **Open punten voor de uitvoerder:** of `closes_issues` een extra aanroep kost (Taak 4); of de `SCHEMA`-pariteit een regel vraagt (Taak 6); of `dispatch.Prepare` al door #258 is aangepast (Taak 7). Elk heeft een controlestap.
- **Niet in dit plan:** de wrapper `lighthouse forge …`, MR aanmaken en labels met kleur aanmaken (#260).
