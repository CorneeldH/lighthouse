# Forge-wrapper voor skills en hooks (#260) — implementatieplan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `lighthouse forge …` dekt wat de keten `start-issue` → `route-issue` → `write-issue` → `write-pr` van een forge vraagt, op GitHub en GitLab; die skills, `board-start`, `pr-create-gate`, `pr_body_gate` en `evidence` werken ermee, en GitHub gedraagt zich als nu.

**Architecture:** De `forge.Forge`-interface (#258, #259) krijgt de ontbrekende schrijf- en leesmethodes, beide implementaties draaien er dezelfde `forgesuite`-cases op. Een nieuw cobra-commando `lighthouse forge` (model: `internal/cli/evidence.go`) opent de forge via `forgeOpener.forRepo` en drukt één JSON-vorm af. Wat per forge verschilt staat in een nieuwe skill `lighthouse-forge` met `github.md` en `gitlab.md`, waar de ketenskills naar verwijzen. De hooks en `board-start` gaan van `gh` naar de forge-interface.

**Tech Stack:** Go (stdlib `net/http`, `encoding/json`, cobra, `testing`); Markdown-skills in `pack/core`; Python-contracttests in `tests/pack`.

**Spec:** [2026-11-16-issue-260-forge-wrapper-design.md](../specs/2026-11-16-issue-260-forge-wrapper-design.md). Voorgaande plannen: [#258](2026-11-08-issue-258-forge-interface.md), [#259](2026-11-15-issue-259-gitlab-client.md).

## Global Constraints

- Geen `merge`-commando in `lighthouse forge` (milestone-no-go). Geen `glab`-afhankelijkheid; de wrapper praat via `internal/forge` met de API.
- Exitcodes van `lighthouse forge`: 0 goed, 1 forge-fout (met de API-melding), 2 gebruiksfout of ontbrekende config. Niet die van `evidence`.
- JSON-vorm voor issue en PR/MR: `number`, `title`, `body`, `state` (`open`/`closed`; PR ook `merged`), `labels` (namen), `milestone` (`number`, `title`, `description`, of `null`), `url`, `created_at`, `author`, en bij `--comments` een lijst `comments` (`author`, `body`, `created_at`). `issue blockers`: per blocker `number`, `state` (`open`/`closed`), `repo`, `merged_pr`.
- `--field <pad>`: pad met punten, een scalar als tekst, de rest als JSON. Alleen op leescommando's; `api` kent alleen `-f` en `-F`, nooit `--field`.
- Milestones: `--milestone` neemt een titel op beide forges. Op GitLab is `milestone.number` het globale id; `milestone list`/`view` geven project- én groepsmilestones (`include_ancestors`); `milestone edit` weigert een groepsmilestone (exit 1, melding noemt `api`); een titel die op twee milestones past is exit 2 en noemt beide.
- `AddBlocker`: GitHub `dependencies/blocked_by` met het gehele database-`id` van de blocker; GitLab `POST …/links` met `link_type=is_blocked_by`. Een weigering van de API (GitLab Free) komt als forge-fout met de API-melding (exit 1). `BlockedBy` geeft op GitLab Free een lege lijst zonder fout.
- `board-start` behoudt zijn contract: token valt terug op `gh auth token`; een token- of forgefout blijft exit 0 met een `board: …`-regel; een status buiten `status_options` beslist op de ruwe optienaam (`BoardItem.StatusName`).
- Hooks worden nergens minder streng; een AC-telling die nu doorlaat met een melding (gh ontbreekt, timeout, rc≠0) laat nu ook door met een melding (ontbrekend token, forge-fout); een telling die nu blokkeert (een kopjestabel die niet te lezen is, `countACs`/`acHeadingRe`) blijft blokkeren; een body zonder AC-kopje blijft doorlaten met `no acceptance criteria heading`. Dezelfde `evidence`-verboden als nu (geen `env`, geen invoerbestand).
- Code, namen, commentaar en skill-instructies Engels; artefacten (docs, commits, changelog, issues) Nederlands (`[project] language` = `nl`). `/lighthouse-go-standard` en `/lighthouse-test-standard`. Elke commit groen; nooit `--no-verify`.
- Wijzigt `pack/` of wat `init` schrijft: `go run ./cmd/lighthouse init --upgrade` en de geïnstalleerde kopie vóór de PR (Taak 13 doet het voor Taak 11 en 12 samen; tussencommits mogen afwijken, CI toetst de kop van de PR). Elke PR krijgt een fragment in `changelog.d/`.

## Review Focus

Elke regel heeft een test in de genoemde taak.

- `issue edit` met `--milestone` waarvan de titel niet bestaat of op twee milestones past: heldere fout, niets gewijzigd. Taak 3 (suite) en Taak 4 (CLI exit 2).
- `issue create` zonder token of met een ongeldige `forge.kind`: exit 2 respectievelijk een duidelijke melding, geen stacktrace. Taak 4.
- `--field` op een pad dat niet bestaat of op `null` (`milestone.title` zonder milestone): lege uitvoer, exit 0 voor `null`, exit 1 met melding voor een onbekend pad. Taak 4.
- Een body-bestand dat ontbreekt of leeg is bij `issue create`/`pr create`/`comment`: exit 2, niets gepost. Taak 5.
- Een PR-body die `lighthouse forge pr create` in een heredoc of na `&&` bevat, en `glab mr create -R` op een ander project: gate beslist per stap als bij `gh`. Taak 8.
- GitLab-project onder subgroepen in `Closes group/sub/proj#7` en in een issue-URL met `/-/issues/7`: juiste doel, geen valse match op GitHub-repos. Taak 8.

---

### Taak 1: Domeintypes en GitHub-methodes

**Files:**
- Modify: `internal/forge/github.go` (`Issue` krijgt `CreatedAt time.Time`), `internal/forge/issues.go` (vul `CreatedAt` in `issueItem.issue()`), `internal/forge/gitlab.go` (`glIssue` krijgt `CreatedAt time.Time` met tag `created_at`, ook in de MR-vorm, en vult `Issue.CreatedAt`/`Change.CreatedAt`)
- Create: `internal/forge/write_issue.go`, `internal/forge/write_issue_test.go`
- Modify: `internal/forge/forgetest/server.go` (de fake krijgt de routes hieronder)

**Interfaces:**
- Produces (types, in `write_issue.go`):
  - `type NewIssue struct{ Title, Body string; Labels []string; Milestone string }`
  - `type IssueEdit struct{ Title, Body, Milestone *string; AddLabels, RemoveLabels []string }` (`Milestone` `""` haalt de milestone weg)
  - `type IssueFilter struct{ State, Label, Milestone string }` (`State` `open|closed|all`, `""` = open)
  - `type NewPR struct{ Title, Body, Head, Base, Milestone string }`
  - `type Change struct{ Number int; Title, Body, State string; Labels []string; Milestone *Milestone; URL string; CreatedAt time.Time; Author, HeadRef, BaseRef string }` (PR of MR; `State` `open|closed|merged`)
  - `type APIRequest struct{ Method, Path string; Fields []APIField }`, `type APIField struct{ Name, Value string; Typed bool }`
  - `var ErrMilestoneNotFound`, `type AmbiguousMilestone struct{ Title string; Numbers []int }` met `Error()`, `type GroupMilestone struct{ Number int }` met `Error()` (GitLab gebruikt hem in Taak 2)
- Produces (methodes op `*GitHub`, signaturen van de interface in Taak 3):
  `CreateIssue(ctx, NewIssue) (Issue, error)`, `EditIssue(ctx, number int, IssueEdit) (Issue, error)`, `ListIssues(ctx, IssueFilter) ([]Issue, error)`, `SearchIssues(ctx, text string) ([]Issue, error)`, `CreateChange(ctx, NewPR) (Change, error)`, `Change(ctx, number int) (Change, error)`, `CreateChangeComment(ctx, number int, body string) (protocol.Comment, error)`, `Milestones(ctx, state string) ([]Milestone, error)`, `MilestoneByNumber(ctx, number int) (Milestone, error)`, `CreateMilestone(ctx, title, description string) (Milestone, error)`, `EditMilestone(ctx, number int, title, description *string) (Milestone, error)`, `Labels(ctx) ([]string, error)`, `AddBlocker(ctx, number, blocker int) error`, `API(ctx, APIRequest) ([]byte, error)`.

- [ ] **Step 1: Schrijf de tests** in `write_issue_test.go`, op de fake van `forgetest`: `TestGitHubCreateIssueResolvesMilestoneByTitle` (titel `M3` wordt het nummer in de POST-body; het teruggegeven `Issue.Milestone.Title == "M3"`), `TestGitHubEditIssueAddsAndRemovesLabels` (`AddLabels`/`RemoveLabels` worden aparte verzoeken, `PATCH` alleen voor titel/body/milestone), `TestGitHubMilestoneTitleAmbiguous` (twee milestones `M3`: fout `*AmbiguousMilestone` met beide nummers), `TestGitHubMilestoneUnknown` (`errors.Is(err, ErrMilestoneNotFound)`), `TestGitHubAddBlockerSendsDatabaseID` (de fake kent issue 5 met id 9005; de POST bevat `issue_id=9005` als getal), `TestGitHubAddBlockerRefusedKeepsMessage` (422 met `message` komt in `err.Error()`), `TestGitHubListIssuesFollowsPages` (101 issues, geen PR's), `TestGitHubChangeStateMerged` (gesloten PR met `merged_at` → `State == "merged"`), `TestGitHubAPIPostsTypedFields` (`-F n=3` wordt getal 3, `-F ok=true` bool, `-F body=@bestand` de inhoud, `-f n=3` tekst `"3"`).
- [ ] **Step 2: Draai ze** — `go test ./internal/forge/ -run 'TestGitHub(Create|Edit|Milestone|AddBlocker|List|Change|API)'` — verwacht FAIL (niet gedefinieerd).
- [ ] **Step 3: Implementeer de types en methodes** in `write_issue.go` met de bestaande `c.do`; paginering zoals `ClosedInMilestone`; zoek en lijst sluiten pull requests uit. `SearchIssues` is `GET search/issues?q=repo:<repo> is:issue <text>`. Milestone-titel → nummer via `Milestones(ctx, "all")`. `CreateChange` op GitHub: `POST pulls` kent geen milestone; zet die met een tweede verzoek `PATCH issues/{n}` met `milestone`, en laat de fake onbekende velden op `POST pulls` weigeren (anders slaagt de test terwijl GitHub het veld negeert); test `TestGitHubCreateChangeSetsMilestoneViaIssuePatch`. Breid `forgetest/server.go` uit met de routes die de tests raken; een onverwacht verzoek laat de test falen, zoals nu.
- [ ] **Step 4: Draai de tests** — `go test ./internal/forge/` — verwacht PASS.
- [ ] **Step 5: Commit** — `feat(forge): schrijf- en leesmethodes voor issues, PR's, milestones en blockers op GitHub (#260)`.

### Taak 2: GitLab-methodes

**Files:**
- Create: `internal/forge/gitlab_write.go`, `internal/forge/gitlab_write_test.go`
- Modify: `internal/forge/gitlabtest/server.go` (routes: `POST/PUT projects/:id/issues`, `issues?search=`, `projects/:id/milestones` en `groups/:gid/milestones` (via `include_ancestors`), `labels`, `merge_requests` POST en GET `/:iid`, MR-notes, `…/links` POST, willekeurig `GET`/`POST` pad voor `API`)

**Interfaces:**
- Consumes: de types uit Taak 1, `glIssue.issue()`, `mrToPR`-vormen, `getAll`, `g.do`.
- Produces: dezelfde methodes als Taak 1 op `*GitLab`, met dezelfde signaturen en semantiek: `Milestone.Number` is het globale id; `MilestoneByNumber` zoekt dat id in `include_ancestors`; `EditMilestone` op een groepsmilestone geeft `*GroupMilestone`; `Change.State` `opened`→`open`, `merged`→`merged`, `closed`→`closed`; `AddBlocker` doet `POST projects/:id/issues/:iid/links` met `target_project_id`, `target_issue_iid` en `link_type=is_blocked_by`.

- [ ] **Step 1: Schrijf de tests** in `gitlab_write_test.go`: `TestGitLabCreateIssueMilestoneByTitleInGroup` (titel staat alleen op de groep: `milestone_id` in de POST is het project-/groeps-id van die milestone), `TestGitLabMilestoneNumberIsGlobalID`, `TestGitLabEditGroupMilestoneRefused` (`errors.As(err, &*GroupMilestone)`), `TestGitLabCreateChangeIsMR` (POST `…/merge_requests` met `source_branch`, `target_branch`, `description`; `Change.URL` is de `web_url`), `TestGitLabAddBlockerFreeRefusal` (403 van de fake: melding van de API in de fout), `TestGitLabBlockedByFreeEmpty` (fake zonder links-feature geeft `[]`, geen fout), `TestGitLabEditIssueLabels` (`add_labels`/`remove_labels` in één PUT), `TestGitLabSearchIssues`, `TestGitLabAPIKeepsToken` (token alleen naar de eigen host; pad zonder `/api/v4`-prefix wordt eraan toegevoegd), `TestGitLabAPIGraphQLPath` (pad `api/graphql` gaat naar `graphqlURL()`, dus `/api/graphql` op de ingestelde host, niet `/api/v4/api/graphql`).
- [ ] **Step 2: Draai ze** — `go test ./internal/forge/ -run TestGitLab` — verwacht FAIL.
- [ ] **Step 3: Implementeer** in `gitlab_write.go`; de project-ID in het pad is `g.project()`. `BlockedBy` van #259 moet op Free geen fout geven: pas het aan als de test dat laat zien.
- [ ] **Step 4: Draai** `go test ./internal/forge/...` — PASS.
- [ ] **Step 5: Commit** — `feat(forge): schrijf- en leesmethodes voor GitLab (#260)`.

### Taak 3: Interface en contracttests

**Files:**
- Modify: `internal/forge/forge.go` (interface), `internal/forge/forgesuite/suite.go`, `internal/forge/github_contract_test.go`, `internal/forge/gitlab_contract_test.go`, de fakes in `internal/cli/forges_test.go`, `internal/cli/ask_test.go`, `internal/cli/serve_test.go`, `internal/dispatch/apply_test.go`, `internal/dispatch/gather_test.go`, `internal/server/forge_test.go` (compileerfouten oplossen: embed `forge.Forge` of voeg de methodes toe, zoals die bestanden nu doen)

**Interfaces:**
- Consumes: alle methodes van Taak 1 en 2.
- Produces: de methodes van Taak 1 staan in `forge.Forge`, onder een commentaar "wrapper" (`lighthouse forge`); `Fixture` krijgt alleen de toevoegingen uit Step 1.

- [ ] **Step 1: Voeg cases toe** aan `Cases` in `suite.go`: `CreateIssueRoundTrip` (create met label en milestone `M3`, daarna `Issue(n)`: titel, body, label, milestone gelijk), `EditIssue` (titel, body, `AddLabels`, `RemoveLabels`), `ListIssuesStateAndLabel`, `SearchIssues` (vindt op titel; geen PR's), `CreateAndViewChange` (create met `Head`; `Change(n)` geeft `State == "open"`, `HeadRef`), `ChangeComment`, `MilestonesAndLookup` (`Milestones("open")`, `MilestoneByNumber`, `CreateMilestone`, `EditMilestone`), `MilestoneAmbiguousTitle` (`*AmbiguousMilestone`), `LabelsList`, `AddBlockerThenBlockedBy` (`BlockedBy` toont de blocker met `State == "OPEN"`). Voeg aan de Fixture-interface alleen toe wat een case echt nodig heeft: `AddMilestone(t, title string, group bool)` voor de milestone-cases, en een vlag `UniqueMilestoneTitles` (GitHub weigert twee milestones met dezelfde titel): `MilestoneAmbiguousTitle` draait alleen waar de vlag uit staat, dus op GitLab; bijvoorbeeld ook `AddBranch(t, name)` als `CreateChange` een bestaande branch eist. De zin "`Fixture` blijft ongewijzigd" hierboven geldt daardoor niet.
- [ ] **Step 2: Draai** `go test ./internal/forge/ -run 'Contract'` — verwacht FAIL op compileren (interface mist de methodes) en daarna per case.
- [ ] **Step 3: Zet de methodes in `forge.Forge`**, laat beide `var _ Forge` regels compileren, pas de fakes aan, en zet de cases groen op beide forges. Gedrag dat per forge verschilt (bijvoorbeeld GitLab Free) krijgt een Fixture-vlag naar het voorbeeld van `ChecksOnClosedPRs`.
- [ ] **Step 4: Draai** `go build ./... && go test ./...` — PASS.
- [ ] **Step 5: Commit** — `feat(forge): wrapper-methodes in de interface en de contracttests (#260)`.

### Taak 4: `lighthouse forge`, kern en leescommando's

**Files:**
- Create: `internal/cli/forgecmd.go` (oudercommando, opener, exitcodes), `internal/cli/forgecmd_render.go` (JSON-vormen, `--field`), `internal/cli/forgecmd_read.go` (`issue view|list|search|blockers`, `pr view`, `milestone list|view`, `label list`), tests ernaast
- Modify: `internal/cli/root.go` (voeg `newForgeCommand()` toe bij r. 95)

**Interfaces:**
- Consumes: `forgeOpener.forRepo(ctx, repoconfig.Repo)` en de config van `lighthouse.toml` (zoals `internal/cli/ask.go` doet; lees daar hoe de repo wordt geladen), de interface uit Taak 3.
- Produces:
  - `func newForgeCommand() *cobra.Command` en een testnaad `type forgeEnv struct{ open func(ctx) (forge.Forge, error); out, err io.Writer }`; elk subcommando krijgt een `*forgeEnv`.
  - `type forgeError struct{ code int; err error }`-gedrag via bestaande `exitError{code}`: `usage(cmd, msg)` → exit 2, forge-fout → exit 1 met de API-melding op stderr.
  - `func renderIssue(forge.Issue) map[string]any` (JSON-vorm uit Global Constraints, `created_at` RFC 3339), `func renderChange(forge.Change) map[string]any`, `func pickField(v any, path string) (string, error)`.

- [ ] **Step 1: Schrijf de tests** met een fake `forge.Forge`: `TestIssueViewJSONShape` (alle velden uit de spec, `milestone` `null` zonder milestone, labels als namen), `TestIssueViewComments` (`--comments` voegt `comments` toe), `TestFieldScalarAndObject` (`--field milestone.title` → `M3\n`; `--field labels` → JSON-lijst; `--field milestone.title` bij `null` milestone → lege regel, exit 0; `--field nope` → exit 1 met `unknown field`), `TestPRViewMerged` (`state` `merged`), `TestIssueBlockersShape` (`number`, `state` `open|closed`, `repo`, `merged_pr`), `TestIssueListAllPages`, `TestMilestoneViewAmbiguousTitleExit2`, `TestMissingConfigExit2`, `TestOpenerFallsBackToGhAuthToken` (geen `GH_TOKEN`/`GITHUB_TOKEN`, de `ghToken`-naad geeft een token: het commando slaagt; de opener van `lighthouse forge` wordt gebouwd met `ghToken = ghAuthToken`, niet leeg zoals `forges.go:54-58` nu doet), `TestForgeErrorExit1KeepsMessage`, `TestNoMergeSubcommand` (`lighthouse forge pr merge` geeft exit 2, geen commando).
- [ ] **Step 2: Draai ze** — `go test ./internal/cli/ -run 'TestIssue|TestField|TestPR|TestMilestone|TestMissing|TestForge|TestNoMerge'` — FAIL.
- [ ] **Step 3: Implementeer** het oudercommando en de leescommando's. Eén `--field` vlag op elk leescommando via een helper die het resultaat éérst rendert en dan filtert. `issue list` en `issue search` blijven twee subcommando's (de spec laat het aan de bouwer; zo blijft elke vlaggenset eenduidig).
- [ ] **Step 4: Draai** `go test ./internal/cli/` — PASS.
- [ ] **Step 5: Commit** — `feat(cli): lighthouse forge, leescommando's (#260)`.

### Taak 5: `lighthouse forge`, schrijfcommando's

**Files:**
- Create: `internal/cli/forgecmd_write.go` (`issue create|edit|comment|block|status`, `pr create|comment`, `milestone create|edit`), `internal/cli/forgecmd_write_test.go`

**Interfaces:**
- Consumes: Taak 4, `NewIssue`, `IssueEdit`, `NewPR`, `forge.IssueItem`/`SetStatus`/`AddToBoard`.
- Produces: `--body-file` en `--description-file` zijn verplicht en worden gelezen (geen stdin); `--label`, `--add-label`, `--remove-label` herhaalbaar; `pr create` zonder `--head` neemt de huidige branch (`git rev-parse --abbrev-ref HEAD` via een naad `forgeEnv.branch func() (string, error)`) en zonder `--base` de standaardbranch van de forge. Uitvoer van `issue create` en `pr create`: regel 1 het nummer, regel 2 de URL; de tekst noemt "MR" bij `Kind() == "gitlab"`.

- [ ] **Step 1: Schrijf de tests**: `TestIssueCreatePrintsNumberAndURL`, `TestIssueCreateMissingBodyFileExit2` (bestand ontbreekt of is leeg: exit 2, de fake kreeg geen aanroep), `TestIssueEditLabelsAndMilestone`, `TestIssueEditNothingExit2` (geen enkele wijziging), `TestIssueBlockPassesAPIRefusal` (fake geeft fout: exit 1, melding op stderr), `TestIssueStatusMovesBoardItem` (`IssueItem` ok=false → `AddToBoard` dan `SetStatus`; onbekende statusnaam → exit 2 met de geldige namen), `TestPRCreateDefaultsHeadAndBase`, `TestPRCreateSaysMROnGitLab`, `TestMilestoneEditGroupExit1` (`*GroupMilestone`, melding noemt `lighthouse forge api`), `TestMilestoneAmbiguousExit2` (beide nummers genoemd).
- [ ] **Step 2: Draai** — `go test ./internal/cli/ -run 'TestIssue(Create|Edit|Block|Status)|TestPRCreate|TestMilestone'` — FAIL.
- [ ] **Step 3: Implementeer** de schrijfcommando's; vertaal `*AmbiguousMilestone` naar exit 2 en `*GroupMilestone` naar exit 1.
- [ ] **Step 4: Draai** `go test ./internal/cli/` — PASS.
- [ ] **Step 5: Commit** — `feat(cli): lighthouse forge, schrijfcommando's (#260)`.

### Taak 6: `lighthouse forge api`

**Files:**
- Create: `internal/cli/forgecmd_api.go`, `internal/cli/forgecmd_api_test.go`

**Interfaces:**
- Consumes: `forge.APIRequest`, `Forge.API`.
- Produces: `lighthouse forge api <pad> [-X <methode>] [-f naam=tekst]… [-F naam=waarde]…`. Geen `--field`, geen `--raw-field`, geen `--jq` (unknown flag → exit 2). Uitvoer is de body van de forge, ongewijzigd. Een GET die een JSON-lijst teruggeeft volgt de paginering (GitHub `Link rel=next`, GitLab `x-next-page`) en voegt de pagina's samen tot één lijst; `APIRequest` krijgt daarvoor geen vlag, het is het gedrag van `API` op GET.

- [ ] **Step 1: Schrijf de tests**: `TestAPIDefaultMethodGETWithoutFields` en `POST` zodra `-f`/`-F` voorkomt, `TestAPITypedFields` (`-F n=3`, `-F ok=true`, `-F x=null`, `-F body=@pad`), `TestAPIRejectsLongFieldFlag` (`--field a=b` → exit 2), `TestAPIGraphQLPath` (pad `graphql` op GitHub, `api/graphql` op GitLab komt ongewijzigd in `APIRequest.Path`), `TestAPIForgeErrorExit1`, en per forge in `internal/forge`: `TestGitHubAPIFollowsPages` en `TestGitLabAPIFollowsPages` (twee pagina's, één samengevoegde lijst).
- [ ] **Step 2: Draai** — FAIL.
- [ ] **Step 3: Implementeer.**
- [ ] **Step 4: Draai** — PASS.
- [ ] **Step 5: Commit** — `feat(cli): lighthouse forge api (#260)`.

### Taak 7: `board-start` op de forge-interface

**Files:**
- Modify: `internal/forge/board.go` (`BoardItem.StatusName string`; `fieldValuesSelection` krijgt `name`; `itemNode`-veldwaarden lezen `name`), `internal/forge/gitlab_board.go` (vul `StatusName` uit `status.name`), `internal/boardstart/boardstart.go` (`Env` krijgt `Forge func(ctx) (forge.Forge, error)` in plaats van `Run`; `readItem` → `IssueItem`; `setInProgress` → `SetStatus(ctx, item, "In progress")`), `internal/cli/` (waar `board-start` wordt aangeroepen: `Env` vullen met `forgeOpener` en `ghToken`), `internal/boardstart/fixtures_test.go`
- Modify: `internal/cli/forges.go` — `ghToken` ook voor `lighthouse forge`, `board-start` en `prbody` instelbaar via een constructor `newDefaultForgeOpener()` die `gh auth token` draait; `board-start` en `prbody` laden `repoconfig.Repo` (met `TokenEnv`) voor de opener, niet `internal/config.Config`, die `TokenEnv` mist
- Modify: de `gh`-stubcases in `tests/pack/fixtures/scripts/board-start/cases.json` (37 gevallen) worden tests op een fake `forge.Forge` in `internal/boardstart/boardstart_test.go`; verwijder het JSON-bestand en wat het leest alleen als niets anders het gebruikt. `tests/pack/test_script_cases.py` globt `fixtures/scripts/*/cases.json` en blijft voor de andere scripts bestaan; haal alleen de `board-start`-map uit zijn dekking en controleer dat de test nog slaagt

**Interfaces:**
- Consumes: `forge.IssueItem(ctx, nr) (BoardItem, bool, error)`, `SetStatus`.
- Produces: `BoardItem.StatusName` (ruwe optienaam; `""` zonder status) op beide forges; `status` en de `BoardItems`-query van de dispatcher blijven gelijk in betekenis.

- [ ] **Step 1: Zet de 37 gevallen om** in table-tests op een fake `forge.Forge`: exitcode en de `board: …`-regels van de status- en stroomgevallen blijven letterlijk gelijk. De foutregels die nu een Python-uitzonderingsnaam dragen (`board: skipped (CalledProcessError)`, `(JSONDecodeError)`, `(KeyError)`, `(TypeError)`, `board: ERROR — TimeoutExpired`, `board: ERROR — CalledProcessError`) krijgen een nieuwe, vaste vorm: `board: skipped (<forge-foutmelding>)` en `board: ERROR — <forge-foutmelding>` (exitcodes ongewijzigd); schrijf die nieuwe teksten in de table-tests en zet in het PR-bewijs welke gevallen letterlijk gelijk zijn en welke de nieuwe vorm hebben (geen claim van 37 gelijke uitkomsten). Voeg toe `TestBoardStartStatusNameDecides` (een status die niet in `status_options` staat, bijvoorbeeld "On hold", beslist op de ruwe `StatusName`), `TestBoardStartTokenErrorExit0` (opener geeft `*forge.TokenError` → exit 0 met `board: …`), `TestBoardStartIssueNotFound`. Voeg in `forgesuite` een case `IssueItemStatusName` toe (`PutOnBoard(…, "To do", …)` → `StatusName == "To do"`; `PutOnBoardUnmapped` → de ruwe naam; de GitHub-fixture geeft nu een optie-id `opt-unmapped` terug, laat hem de optienaam teruggeven (`github_contract_test.go:150-153`)).
- [ ] **Step 2: Draai** — `go test ./internal/boardstart/ ./internal/forge/` — FAIL.
- [ ] **Step 3: Implementeer** `StatusName` (GitHub: `name` toevoegen aan `fieldValuesSelection` en `itemNode`; pas de GraphQL-fixtures van `board_test.go` aan), `board-start` op de interface en de opener-terugval op `gh auth token`. Houd de bestaande teksten van de `board: …`-regels.
- [ ] **Step 4: Draai** `go test ./...` — PASS; draai ook `go test ./tests/...` of de pytest-suite die `tests/pack` dekt, voor zover die `board-start` raakt.
- [ ] **Step 5: Commit** — `feat(boardstart): board-start via de forge-interface in plaats van gh (#260)`.

### Taak 8: `pr_body_gate`, sluitwoorden en doelen

**Files:**
- Modify: `internal/prbody/find.go` (`FindPrCreates` herkent drie vormen), `internal/prbody/form.go` (sluitwoorden en doelen per `forge.kind`), `internal/prbody/gate.go` (`Env`, `fetchACCount`), `internal/prbody/scope.go`, bijbehorende tests (`gate_test.go`, `fixtures_test.go`, `inline_twins_test.go`)

**Interfaces:**
- Consumes: `forge.Forge.Issue`, `forgeOpener` (de opener staat in `internal/cli`; geef `prbody.Env` een veld `Issue func(ctx context.Context, repo string, nr int) (body string, err error)` zodat `prbody` geen cli importeert; `DefaultEnv()` vult het via `forge.Open` met `forge.Token` en de `gh auth token`-terugval).
- Produces:
  - `type PrCreate` krijgt `Tool string` (`"lighthouse"`, `"gh"` of `"glab"`); `FindPrCreates` vindt `lighthouse forge pr create`, `gh pr create|new` en `glab mr create|new`; de body-vlag van `glab` is `-d`/`--description`, `--body-file` bij `lighthouse forge`; `-R`/`--repo` geldt voor `gh` en `glab`; voor `lighthouse forge` is de repo `forge.repo`.
  - `ClosesTargets(body, repo string)` kent bij `forge.kind = "gitlab"` de extra woorden `closing`, `fixing`, `resolving`, `implement(s|ed|ing)`, en als doel `<groep>/<sub>/<project>#N` en `https://<host>/<pad>/-/issues/N`; `Env` of de `Main`-aanroep krijgt `Kind` en `Host` uit de config.

- [ ] **Step 1: Schrijf de tests**: `TestFindPrCreatesLighthouseForge` (`lighthouse forge pr create --title t --body-file p` en na `&&`/heredoc), `TestFindPrCreatesGlabMR` (`glab mr create -d "…" -R g/p`), `TestGlabRepoFlagScope`, `TestGlabSubgroupRepoIsOwnProject` (`-R g/sub/p` gelijk aan de eigen `forge.repo` op gitlab: de gate toetst; `NormalizeRepo` is kind-bewust voor gitlab en haalt bij ≥3 segmenten geen host-segment af), `TestForgePrCreateIgnoresGHRepo` (`GH_REPO=other/x` in de omgeving: `lighthouse forge pr create` wordt toch getoetst, want dat commando leest `GH_REPO` niet; sluit `Tool=="lighthouse"` uit van de `GH_REPO`-invulling in de `Main`-lus), `TestClosesTargetsGitLabWords` (`Implements #7` telt op gitlab, niet op github), `TestClosesTargetsSubgroup` (`Closes g/sub/p#7` en `https://gitlab.example/g/sub/p/-/issues/7`; een GitHub-repo-regex pakt `g/sub/p` niet), `TestFetchACCountViaForge` (fake `Env.Issue` geeft body; telling gelijk aan de `gh`-uitkomst van de bestaande fixtures), `TestFetchACCountTokenMissingPassesLoudly` (melding noemt het token, de gate laat door met een melding zoals nu bij `gh is missing`), `TestFetchACCountForgeErrorPassesLoudly` (forge-fout of timeout: door met melding, zoals nu bij rc≠0), `TestFetchACCountHeadingsTableErrorBlocks` (de bestaande blokkade bij een fatale fout uit `countACs` blijft) en `TestFetchACCountNoHeadingPasses` (zonder AC-kopje: door met die melding). Vervang `gate_test.go:479-480`-achtige verwachtingen waar de oorzaaktekst verandert. Pas de bestaande fixtures aan waar `Env.GH` nu wordt gestubd: de naad `Env.GH` blijft bestaan als testnaad van de `gh auth token`-terugval (r1-f9).
- [ ] **Step 1b: Python-tests** — `tests/pack/test_pr_create_gate.py` stubt `gh` voor de AC-telling (`FAKE_GH`, r. 42-50; `test_ac_count_differs_blocks`, `test_gh_fails_passes_loudly`, `test_gh_missing_passes_loudly`, `test_slow_gh_passes_loudly`). Die kunnen na deze taak niet meer slagen. Geef de Go-code één testnaad: de omgevingsvariabele `LIGHTHOUSE_GITHUB_API_URL` (alleen gelezen door de `forge.Open`-bouw voor GitHub, zodat een nep-HTTP-server in de pytest het API-adres overneemt; zet hem in `DefaultEnv()`-pad, niet in productieconfig), en herschrijf die vier tests op een nep-server met dezelfde verwachte uitkomsten (blokkeren bij afwijkende telling; doorlaten met melding bij 500, bij geen token en bij een traag antwoord). Houd de testnaad klein en noem hem in de PR.
- [ ] **Step 2: Draai** — `go test ./internal/prbody/` — FAIL.
- [ ] **Step 3: Implementeer** de drie plekken (`find.go:233`, `form.go`, `gate.go:62-72`); de twee herkenningsplekken (`find.go` en `prcreategate.go`) veranderen samen in Taak 9.
- [ ] **Step 4: Draai** `go test ./internal/prbody/` — PASS.
- [ ] **Step 5: Commit** — `feat(prbody): pr_body_gate herkent forge pr create en glab mr create, GitLab-sluitwoorden (#260)`.

### Taak 9: `pr-create-gate` hook

**Files:**
- Modify: `internal/hooks/prcreategate.go` (tweestaps-regel r. 83-90, meldingen r. 26 en 111), `internal/hooks/prcreategate_test.go`

**Interfaces:**
- Consumes: `prbody.FindPrCreates`, `PrCreate.Tool` uit Taak 8.
- Produces: de tweestaps-regel laat een commando door als geen van `gh`, `glab` of `forge` met `pr`/`mr` in de ruwe tekst staat; een onparseerbaar commando met `glab`/`mr` of `lighthouse` + `forge` krijgt dezelfde `COULD NOT CHECK`-blokkade als nu voor `gh`.

- [ ] **Step 1: Schrijf de tests**: `TestHookBlocksForgePrCreateWithoutClosesLine`, `TestHookBlocksGlabMrCreateBody`, `TestHookAllowsForgePrCreateValidBody`, `TestHookUnparseableGlabBlocks` (open quote met `glab mr create`), `TestHookPassesUnrelatedCommand` (geen `gh`/`glab`/`forge`: door zonder blokkade; `git commit -m "pr"` blijft door).
- [ ] **Step 2: Draai** — `go test ./internal/hooks/ -run 'TestHook'` — FAIL.
- [ ] **Step 3: Implementeer**; pas de teksten aan ("every 'gh pr create'" → "every PR or MR create").
- [ ] **Step 4: Draai** `go test ./internal/hooks/ ./internal/prbody/` — PASS.
- [ ] **Step 5: Commit** — `feat(hooks): pr-create-gate herkent lighthouse forge en glab (#260)`.

### Taak 10: `evidence` op `lighthouse forge`

**Files:**
- Modify: `internal/evidence/allowlist.go` (`check`: `case "lighthouse"`), `internal/evidence/run.go` (probe en `ghOK`-gate, r. 402 en 436-458), `internal/evidence/fixtures_test.go`, `internal/evidence/run_test.go`

**Interfaces:**
- Consumes: de subcommando's van Taak 4 en 6.
- Produces: `func checkLighthouse(args []string) string` (leeg = toegestaan). Toegestaan: `forge issue view|list|search|blockers`, `forge pr view`, `forge milestone list|view`, `forge label list`, elk met `--field`, en `forge api <pad>` alleen zonder `-X` ≠ GET en zonder `-f`/`-F`. Alles anders: reden. `Env` en `--` flags zoals `gh` (geen invoerbestand, geen `env`). De probe gaat naar `lighthouse forge api <rate-pad>` op `forge.kind`; rijen met `argv[0] == "lighthouse"` en `forge` tellen mee voor de probe (`runsGH`) en worden bij een onbereikbare forge *onmeetbaar*.

- [ ] **Step 1: Schrijf de tests**: `TestCheckLighthouseReadForms` (alle leesvormen toegestaan, ook met `--field milestone.title`), `TestCheckLighthouseDeniesWrites` (`issue create`, `issue edit`, `issue comment`, `pr create`, `issue block`, `issue status`, `milestone create|edit` → reden), `TestCheckLighthouseAPIGetOnly` (`api repos/x` mag; `-X POST`, `-f a=b`, `-F a=b`, GraphQL-pad `graphql` met `-F query=…` → reden), `TestCheckLighthouseOtherSubcommand` (`evidence run`, `serve` → reden), `TestRunRowForgeUnreachableIsUnmeasurable` (probe faalt: `Unmeasurable`, geen `Fails`), `TestRunsForgeCountsForProbe`.
- [ ] **Step 2: Draai** — `go test ./internal/evidence/` — FAIL.
- [ ] **Step 3: Implementeer**; houd `gh`-vormen ongewijzigd. De probe op GitHub blijft `gh api rate_limit`; op GitLab `lighthouse forge api user`.
- [ ] **Step 4: Draai** `go test ./internal/evidence/ ./internal/prbody/` — PASS.
- [ ] **Step 5: Commit** — `feat(evidence): leesvormen van lighthouse forge in de allowlist (#260)`.

### Taak 11: De skill `lighthouse-forge`

**Files:**
- Create: `pack/core/skills/forge/SKILL.md`, `pack/core/skills/forge/github.md`, `pack/core/skills/forge/gitlab.md`
- Modify: `internal/packs/core_test.go` (pint elk geïnstalleerd bestand: voeg de drie `forge/`-bestanden toe; skills worden uit de map ontdekt, een manifest bestaat niet), `pack/core/requires.toml` (voeg `forge` toe bij `gh`; github.md noemt `gh api` en `gh project`; test `test_requires_names_the_skill_per_tool`)
- Test: `tests/pack/test_skill_contracts.py`

**Interfaces:**
- Produces: de secties `§board`, `§blockers`, `§sub-issues`, `§sluitwoorden`, `§proxy` in beide bestanden, dezelfde volgorde, met de inhoud van de tabel in §2 van de spec. `SKILL.md`: `description` zegt dat de skill alleen via een andere skill gelezen wordt.

- [ ] **Step 1: Schrijf de contracttests** in `test_skill_contracts.py`: `test_forge_skill_has_both_references` (twee bestanden, zelfde `##`-secties in dezelfde volgorde), `test_forge_skill_description_not_self_triggering` (bevat "only"/"alleen" en "via another skill"), `test_forge_reference_mentions_no_glab_command` (`glab` komt alleen voor in `gitlab.md` als tekst, nooit als codeblokcommando).
- [ ] **Step 2: Draai** — `uv run pytest tests/pack/test_skill_contracts.py -k forge` — FAIL.
- [ ] **Step 3: Schrijf de drie bestanden** (Engels voor instructies, zoals de andere skills; bij §board en §blockers alleen wat per forge verschilt, de rest via `lighthouse forge`; §sub-issues en wat geen commando heeft via `lighthouse forge api`).
- [ ] **Step 4: Draai** de test — PASS — en `go run ./cmd/lighthouse init --upgrade` in een tijdelijke kopie om te zien dat de skill wordt geïnstalleerd (zie Taak 13).
- [ ] **Step 5: Commit** — `feat(core): skill lighthouse-forge met referenties per forge (#260)`.

### Taak 12: De ketenskills op `lighthouse forge`

**Files:**
- Modify: `pack/core/skills/start-issue/claude/SKILL.md`, `start-issue/agents/SKILL.md`, `route-issue/SKILL.md`, `write-issue/claude/SKILL.md`, `write-issue/agents/SKILL.md`, `write-pr/SKILL.md`, `pack/core/requires.toml` (alleen als de eis "skill die `gh` noemt heeft een regel" nog klopt)
- Test: `tests/pack/test_skill_contracts.py` (de #224-test rond r. 404 en de `requires.toml`-test), `tests/pack/test_write_pr_skill.py` (r. 86-88, 238, 314, 322, 324: pas de asserts aan op de nieuwe vormen; `test_allowlist_table_covers_the_code` blijft groen doordat de `gh`-rijen blijven staan), `tests/pack/test_write_issue_skill.py` (r. 91-93, 202-226, 247: `test_invariant_check_runs_against_gh` draait het stap-6-blok nu tegen een strikte nep-`gh`; schrijf het om op een nep-HTTP-server met `LIGHTHOUSE_GITHUB_API_URL` uit Taak 8, of maak het een tekstcontract als draaien niet kan, en zeg in het PR-bewijs welke van de twee)

**Interfaces:**
- Consumes: de commando's van Taak 4 tot 6 en de secties van Taak 11.
- Produces: in deze vijf skills (en tweelingen) staat geen `gh issue|pr|api|project|label`-aanroep meer die in de tabel van spec §1 valt; elk geval dat een sectie van `lighthouse-forge` dekt, verwijst naar `../lighthouse-forge/<kind>.md §<sectie>` met `<kind>` uit `lighthouse config forge.kind`. De regel over `gh issue list --json` verhuist naar `§proxy`. `write-pr` houdt zijn verbodslijst voor `lighthouse forge api` (`-f`/`-F` alleen als schrijf, nooit in Evidence) en noemt in de gate-sectie `lighthouse forge pr create --title … --body-file …` als pad, `gh pr create` en `glab mr create` als herkende vormen.

- [ ] **Step 1: Pas de contracttests aan** en voeg toe: `test_chain_skills_do_not_call_gh_for_forge_operations` (regex op `\bgh (api|issue|pr|label|project)\b` in de vijf skills en tweelingen, behalve in een expliciete uitzonderingslijst voor `gh auth token`, het citaat van wat de gate herkent en de evidence-allowlisttabel van `write-pr` (`pack/core/skills/write-pr/SKILL.md:241-243`, blijft `gh`-vormen noemen naast de nieuwe `lighthouse forge`-rijen)), `test_chain_skills_point_at_forge_reference` (`../lighthouse-forge/` komt voor), en vervang de #224-asserts over `gh pr create --repo <forge.repo>` door de nieuwe vorm. Draai — FAIL.
- [ ] **Step 2: Herschrijf de skills**, skill voor skill, in de volgorde start-issue → route-issue → write-issue → write-pr; houd de twee tweelingen gelijk (diff blijft alleen de bestaande verschillen). Elk `gh` → `lighthouse forge` volgens de tabel in spec §1; `jq`-stukken passen op de JSON-vorm van Global Constraints (`.milestone.title`, `.labels`, `.state`).
- [ ] **Step 3: Draai** `uv run pytest tests/pack/` — PASS.
- [ ] **Step 4: Draai een droge controle** met `go run ./cmd/lighthouse forge issue view 260 --field title` (de geïnstalleerde binary kan ouder zijn; in een Evidence-rij telt `unknown command` van `lighthouse` als onmeetbaar, niet als mislukt) op deze repo (token nodig) en neem de uitvoer op in de PR als Evidence-rij; zonder token: noteer dat het niet meetbaar was.
- [ ] **Step 5: Commit** — `feat(core): ketenskills gebruiken lighthouse forge in plaats van gh (#260)`.

### Taak 13: Dogfood, changelog, vervolgissues

**Files:**
- Modify: `.claude/skills/…`, `.agents/skills/…` (geïnstalleerde kopie, via `init --upgrade`), `.claude/hooks/lighthouse/…` indien gewijzigd
- Create: `changelog.d/issue-260-forge-wrapper.md`
- Modify: `docs/` waar `gh` als vereiste of de lijst commando's staat (`grep -rn "lighthouse evidence" docs README.md`: voeg `lighthouse forge` toe waar `evidence` staat)

- [ ] **Step 1: Draai** `go run ./cmd/lighthouse init --upgrade` en commit de gewijzigde kopie; draai het nogmaals en verwacht `No changes.`
- [ ] **Step 2: Schrijf het fragment** `changelog.d/issue-260-forge-wrapper.md` met `type: added` en één Nederlandse alinea: `lighthouse forge`, de skill `lighthouse-forge`, hooks en `evidence` op beide forges, `board-start` zonder `gh`.
- [ ] **Step 3: Schrijf de twee vervolgissues** in dezelfde milestone met `/lighthouse-write-issue`: (1) `/lighthouse-close-issue` op GitLab (`unblock-dependents`, releases, `closedByPullRequestsReferences`), (2) de overige units met `gh` (`fix-pr`, `write-handoff`, `review-plan`, `upgrade`, `audit-worktrees`, `post-artefact`, `unblock-dependents`, `create_labels.py`, workflows, en een `requires.toml`-voorwaarde per forge). Noem #358 (echte run op GitLab) in beide. Dit dekt AC 5.
- [ ] **Step 4: Draai de hele suite**: `go build ./... && go vet ./... && golangci-lint run && go test -race ./... && uv run pytest tests/pack/` — alles groen.
- [ ] **Step 5: Commit** — `docs(forge): changelog, documentatie en geïnstalleerde kopie (#260)`.

## Zelfreview

- **Spec-dekking:** §1 → Taak 1 tot 6 (commando's, JSON, `--field`, milestones, `api`, exitcodes, geen merge, nieuwe methodes, board-start in Taak 7); §2 → Taak 11; §3 → Taak 12 (twins, contracttests, `requires.toml`, dogfood in 13); §4 → Taak 8 (prbody, sluitwoorden, AC-telling), 9 (hook), 10 (evidence, probe); AC 5 → Taak 13.
- **Typeconsistentie:** `Change`, `NewIssue`, `IssueEdit`, `IssueFilter`, `NewPR`, `APIRequest`, `AmbiguousMilestone`, `GroupMilestone` en `StatusName` zijn in Taak 1 en 7 gedefinieerd en komen zo terug in 2 tot 6; `PrCreate.Tool` in Taak 8 voor 9.
- **Open voor de bouwer (spec):** vlaggen en veldvolgorde zijn hierboven vastgelegd waar ze later taken raken; `issue search` en `issue list` zijn twee subcommando's; `glab`-parsing is `-d`/`--description` met `-R`/`--repo`.
