# Forge-interface (#258) — implementatieplan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Eén interface `forge.Forge` met forge-neutrale types, de GitHub-client als eerste implementatie, en alle afnemers erop, met één forge per repo en een token per soort forge.

**Architecture:** `internal/forge.Client` wordt `forge.GitHub` en krijgt het board uit `[forge.board]` mee, zodat de vertaling van statusnaam naar Projects v2-option-ID in de client zit. Daarna schuiven de afnemers één voor één van het concrete type naar `forge.Forge`; elke commit compileert en is groen. `internal/cli` krijgt één opener die per repo een forge opent, met het token van die repo en een gedeelde login per soort, host en token.

**Tech Stack:** Go (stdlib `net/http`, `testing`), bestaande nepserver `internal/forge/forgetest`, Python alleen voor `pack/core/lib/config.py` en `tests/test_site_config.py`.

**Spec:** [2026-11-08-forge-interface-design.md](../specs/2026-11-08-forge-interface-design.md). Dit plan dekt alleen *#258 levert*.

## Global Constraints

- Code, namen, commentaar en foutmeldingen in het Engels; dit plan, commitberichten en de PR in het Nederlands.
- `/lighthouse-go-standard` en `/lighthouse-test-standard` gelden voor elke Go-taak.
- Bestaande tests blijven inhoudelijk gelijk. Toegestaan zijn alleen: een mechanische hernoeming (type, methode, helper), een test die met zijn code verhuist, en de wijzigingen die een taak hieronder bij naam noemt.
- Gedrag op GitHub verandert niet: dezelfde GraphQL-queries en -variabelen, dezelfde meldingen, dezelfde exitcodes.
- Een token komt nooit in `lighthouse.toml`, een melding of een log.
- Een token gaat nooit naar een host van een andere soort forge. De override van de basis-URL (`--forge-url`, `LIGHTHOUSE_FORGE_URL`, `tickEnv.ForgeURL`, `askEnv.ForgeURL`) geldt alleen voor GitHub-repo's.
- `forge.Open` met `Kind: "gitlab"` geeft de fout `forge: GitLab client not implemented yet`.
- Tokenvariabelen: github `GH_TOKEN`, dan `GITHUB_TOKEN`; gitlab `GITLAB_TOKEN`. `[forge] token_env = "NAAM"` vervangt die lijst door één variabele.
- Een slug komt hoogstens één keer voor over de repo's van `host.toml`.
- Werk in de worktree `/home/corneel/repositories/lighthouse/.claude/worktrees/issue-258-forge-interface`. Draai `git` als losse opdracht met `git -C <worktree>`, niet in een samengestelde opdracht.
- Testgate na elke commit: `go test -race ./...` moet groen zijn. Voor het einde ook `golangci-lint run ./...`.
- Niet in dit plan (dat is #259): de GitLab-client, de nep-GitLab-server, de GitLab-vorm van `[forge.board]` en validatie per soort, `IssueItem` in `dispatch.Prepare` voor een open run buiten het board, en de parser van `lighthouse run` voor subgroepen. Op de interne GitLab komen **geen** eigen statussen *On hold* en *Controller* (besluit van de eigenaar, 2026-11-08). #259 moet die twee dus anders invullen. `SetStatus(item, naam)` laat daar ruimte voor; dit plan legt niets vast dat de ingebouwde status veronderstelt.

## Review Focus

1. **Een item in een niet-gekoppelde kolom** (bijv. *In progress*) dat een open run heeft of een blokkeerder is. De GitHub-client geeft dan `Status = "unknown:<optionID>"`. De dispatcher moet daar `""` van maken: anders verandert `Item.Status` en de tekst "(…)" achter een blokkeerder in de 🤖-comment. Test in taak 3 (`TestPrepareUnmappedStatusIsEmpty`).
2. **Een GitHub-override bij een niet-GitHub-repo.** Met `--forge-url` gezet en een repo met `kind = "gitlab"` mag `forge.Config.BaseURL` niet de override zijn. Test in taak 6 (`TestOpenerOverrideOnlyForGitHub`).
3. **Gemengde installatie, één repo zonder token.** Die repo krijgt een skip-regel, de andere wordt gewoon getickt. Alleen als geen enkele repo een forge krijgt, faalt het commando. Test in taak 6 (`TestTickSkipsRepoWithoutToken`).
4. **Twee repo's met dezelfde soort, host en token** vragen samen één `GET /user` per tick, zoals nu. Test in taak 6 (`TestTickOneLoginForSharedToken`).
5. **Dezelfde slug in twee paden van `host.toml`.** Beide worden geweigerd, met een melding die beide paden noemt. Test in taak 1 (`TestLoadAllRefusesDuplicateSlug`) en taak 6 (`TestTickSkipsDuplicateSlug`).

---

### Task 1: `Host`, `TokenEnv` en `LoadAll` in `repoconfig`; `forge.token_env` op de configkaart

**Files:**
- Modify: `internal/repoconfig/repoconfig.go` (`Repo`, `rawFile.Forge`, `Load`), new func `LoadAll`
- Test: `internal/repoconfig/repoconfig_test.go`
- Modify: `pack/core/lib/config.py` (`SCHEMA`: `"forge.token_env": _STR`)
- Modify: `docs/site/data/config.json` (nieuw item `forge.token_env`; reader `internal/repoconfig/repoconfig.go` bij `forge.host`)
- Modify: geïnstalleerde kopie na `init --upgrade` (`.lighthouse/lib/lighthouse_lib/config.py`)

**Interfaces:**
- Produces:
  - `repoconfig.Repo.Host string` (`[forge] host`, `""` zonder) en `repoconfig.Repo.TokenEnv string` (`[forge] token_env`, `""` zonder).
  - `type Loaded struct { Path string; Repo Repo; Err error }`
  - `func LoadAll(paths []string) []Loaded`: één `Loaded` per pad, in volgorde. `Err` is de fout van `Load`, of bij een dubbele slug de fout `"%s: [forge] repo %s is also the repo of %s"` (eigen pad, slug, het andere pad). Beide repo's met die slug krijgen zo'n fout.

- [ ] **Step 1: Schrijf de falende tests**

```go
func TestLoadReadsHostAndTokenEnv(t *testing.T) {
	// lighthouse.toml met [forge] kind = "gitlab", repo = "g/sub/p", host = "git.example.org", token_env = "WORK_GITLAB"
	// want r.Host == "git.example.org", r.TokenEnv == "WORK_GITLAB"
}
func TestLoadRefusesBadTokenEnv(t *testing.T) {
	// token_env = "1 bad" → err contains "token_env"
}
func TestLoadAllRefusesDuplicateSlug(t *testing.T) {
	// drie dirs: a en c met repo "o/r", b met "o/x"
	// want got[0].Err and got[2].Err non-nil, got[1].Err nil
	// got[0].Err.Error() contains dirA and dirC; got[2].Err.Error() contains dirC and dirA
}
func TestLoadAllKeepsLoadError(t *testing.T) {
	// één dir zonder lighthouse.toml → got[0].Err is de fout van Load(dir), got[0].Path == dir
}
```

- [ ] **Step 2: Draai de tests en zie ze falen**

Run: `go test ./internal/repoconfig/ -run 'TestLoadReadsHostAndTokenEnv|TestLoadRefusesBadTokenEnv|TestLoadAll'`
Expected: FAIL (`r.Host undefined`, `undefined: LoadAll`)

- [ ] **Step 3: Implementeer**

`Host` en `TokenEnv` als velden op `rawFile.Forge` (`toml:"host"`, `toml:"token_env"`). `Load` weigert een `token_env` die niet aan `^[A-Za-z_][A-Za-z0-9_]*$` voldoet, met `"%s: [forge] token_env %q is not an environment variable name"`. `LoadAll` laadt elk pad en zoekt dan dubbele slugs over de pads zonder fout.

- [ ] **Step 4: Run de tests**

Run: `go test ./internal/repoconfig/`
Expected: PASS

- [ ] **Step 5: Zet `forge.token_env` op de configkaart**

Voeg `"forge.token_env": _STR` toe aan `SCHEMA` in `pack/core/lib/config.py`. Dat is de enige wijziging daar: #274 is nog open. Is #274 al gemerged, dan landt de sleutel in de Go-lezer van #274 en blijft `config.py` ongewijzigd. Voeg in `docs/site/data/config.json`, na `forge.host`, toe: `"key": "forge.token_env"`, `"level": "advanced"`, `"required": "no"`, about `"The name of the environment variable that holds the forge token, instead of GH_TOKEN and GITHUB_TOKEN (github) or GITLAB_TOKEN (gitlab). The token itself never goes in lighthouse.toml."`, example `"WORK_GITLAB_TOKEN"`, readers `["internal/repoconfig/repoconfig.go"]`. Voeg `internal/repoconfig/repoconfig.go` toe aan de readers van `forge.host`.

- [ ] **Step 6: Draai de kaarttest**

Run: `uv run pytest -q tests/test_site_config.py`
Expected: PASS. Noemt de test nog een reader of een veld, neem dat dan exact over.

- [ ] **Step 7: Dogfood**

Run: `go run ./cmd/lighthouse init --upgrade`, en daarna nog een keer.
Expected: de tweede run meldt `No changes.`

- [ ] **Step 8: Commit**

```bash
git -C <worktree> add internal/repoconfig pack/core/lib/config.py docs/site/data/config.json .lighthouse
git -C <worktree> commit -m "feat(repoconfig): forge host, token_env en weigering van dubbele slug (#258)"
```

---

### Task 2: `forge.Token`

**Files:**
- Create: `internal/forge/token.go`
- Test: `internal/forge/token_test.go` (neemt `TestGithubToken` uit `internal/cli/serve_test.go:66-80` over, dat in taak 6 verdwijnt)

**Interfaces:**
- Produces:
  - `type TokenError struct { Kind string; Vars []string }`; `Error()` geeft `"no GitHub token: set GH_TOKEN or GITHUB_TOKEN"`. Algemeen: `"no " + label + " token: set " + strings.Join(Vars, " or ")`, met label `GitHub` of `GitLab`.
  - `func Token(kind, tokenEnv string, getenv func(string) string) (string, error)`: `kind` `""` is github. Met `tokenEnv` alleen die variabele, anders de standaardlijst. Leeg overal geeft `*TokenError`. Een onbekende kind geeft een gewone fout.

- [ ] **Step 1: Schrijf de falende tests**

```go
func TestTokenGitHubOrder(t *testing.T) // GH_TOKEN wint van GITHUB_TOKEN; alleen GITHUB_TOKEN → die; kind "" == "github"
func TestTokenGitLab(t *testing.T)      // GITLAB_TOKEN; GH_TOKEN telt niet voor gitlab
func TestTokenEnvReplacesDefaults(t *testing.T) // tokenEnv "WORK" en GH_TOKEN gezet, WORK leeg → *TokenError, Vars == ["WORK"]
func TestTokenErrorText(t *testing.T) {
	// github zonder env → err.Error() == "no GitHub token: set GH_TOKEN or GITHUB_TOKEN"
	// gitlab zonder env → "no GitLab token: set GITLAB_TOKEN"
	// gitlab, tokenEnv "WORK" → "no GitLab token: set WORK"
	// errors.As(err, new(*TokenError)) is true
}
```

- [ ] **Step 2: Draai en zie falen**

Run: `go test ./internal/forge/ -run TestToken`
Expected: FAIL (`undefined: Token`)

- [ ] **Step 3: Implementeer `Token` en `TokenError` in `internal/forge/token.go`**

- [ ] **Step 4: Run**

Run: `go test ./internal/forge/ -run TestToken`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git -C <worktree> add internal/forge/token.go internal/forge/token_test.go
git -C <worktree> commit -m "feat(forge): token per soort forge en token_env (#258)"
```

---

### Task 3: De GitHub-client implementeert `forge.Forge`

Dit is de grootste taak. Een methodesignatuur wijzigen breekt elke aanroeper, dus de aanroepers gaan in dezelfde commit mee. Ze houden het concrete `*forge.GitHub`; het type wisselt pas in taak 4 tot 7.

**Files:**
- Create: `internal/forge/forge.go` (`Forge`, `Config`, `Open`, de compile-check)
- Modify: `internal/forge/github.go` (`Client` → `GitHub`, veld `Board`, `Kind`, cache van node-ID's; `Issue.NodeID` weg)
- Modify: `internal/forge/board.go` (`BoardItem`, board-methoden), `issues.go` (`ClosedInMilestone`), `people.go` (`Role`, `Acknowledgers`), `write.go` (`Acknowledge`)
- Modify (tests, mechanisch): `internal/forge/*_test.go`
- Delete: `internal/dispatch/board.go`, `internal/dispatch/board_test.go` (de tests verhuizen naar `internal/forge/board_test.go`)
- Modify: `internal/dispatch/apply.go`, `gather.go`, `trust.go`, `apply_test.go`, `gather_test.go`, `trust_test.go`
- Modify: `internal/overview/overview.go`, `overview_test.go`
- Modify: `internal/cli/tick.go`, `gather.go`, `run.go`, `triage.go`, `triage_gather.go`, `serve.go`, `ask.go`, `serve_test.go`

**Interfaces:**
- Consumes: `repoconfig.Board` (bestaand).
- Produces (in package `forge`):

```go
type Forge interface {
	Kind() string
	Login(ctx context.Context) (string, error)
	Role(ctx context.Context, user string) (string, error)
	Issue(ctx context.Context, number int) (Issue, error)
	OpenIssues(ctx context.Context) ([]Issue, error)
	ClosedInMilestone(ctx context.Context, m Milestone, since time.Time) ([]Issue, error)
	BlockedBy(ctx context.Context, number int) ([]Blocker, error)
	Comments(ctx context.Context, number int) ([]protocol.Comment, error)
	CreateComment(ctx context.Context, number int, body string) (protocol.Comment, error)
	Acknowledge(ctx context.Context, number int, commentID int64) error
	Acknowledgers(ctx context.Context, number int, commentID int64) ([]string, error)
	OpenPRs(ctx context.Context) ([]OpenPR, error)
	BranchPRs(ctx context.Context, branch string) ([]PR, error)
	BoardItems(ctx context.Context) ([]BoardItem, error)
	IssueItem(ctx context.Context, number int) (item BoardItem, ok bool, err error)
	AddToBoard(ctx context.Context, number int) (BoardItem, error)
	SetStatus(ctx context.Context, item BoardItem, status string) error
}

type Config struct {
	Kind, Repo, Host, Token string
	BaseURL string            // "" is the default of Kind; trailing slashes dropped
	Board   *repoconfig.Board // nil: no [forge.board]; board methods then fail
	HTTP    *http.Client      // nil: a client with a 30 s timeout
}
func Open(cfg Config) (Forge, error)

type GitHub struct {
	Repo, Token, BaseURL string
	HTTP  *http.Client
	Board *repoconfig.Board
	// unexported: mu sync.Mutex; nodeIDs map[int]string
}
func New(repo, token string) *GitHub // as today; Board nil
var _ Forge = (*GitHub)(nil)

type BoardItem struct { // SingleSelect is gone
	ItemID         string
	ItemUpdatedAt  time.Time
	Status         string // option name; "" no status; "unknown:<optionID>" an option status_options does not list
	Priority       string // option name; "" no priority or an option priority_options does not list
	Repo           string
	Issue          int
	Title          string
	IssueState     string
	IssueUpdatedAt time.Time
	BlockedBy      []Blocker
	ClosingPRs     []PR
	Archived       bool
}
```

- Gedrag van `GitHub`:
  - `Kind()` geeft `"github"`.
  - `Role` is de oude `RoleName`.
  - `Acknowledge(ctx, number, id)` en `Acknowledgers(ctx, number, id)` zijn de oude `AddRocket(id)` en `RocketLogins(id)`; `number` wordt op GitHub niet gebruikt.
  - `ClosedInMilestone` filtert op `m.Number`.
  - `BoardItems(ctx)` en `IssueItem(ctx, n)` lezen het project `Board.ProjectID`. Ze vullen `Status` en `Priority` via `Board.StatusFieldID` en `PriorityFieldID` en de omgekeerde option-maps.
  - `AddToBoard(ctx, n)` zoekt het node-ID van issue `n` en geeft `BoardItem{ItemID, Repo, Issue: n}`. Het node-ID komt uit de cache, die `Issue` en `OpenIssues` vullen; anders volgt één `GET /repos/{repo}/issues/{n}`.
  - `SetStatus(ctx, item, status)` stuurt de bestaande `SetStatus`-mutatie met dezelfde variabelen: `project`, `item`, `field`, `option`.
  - Fouten:
    - `Board == nil` bij een board-methode: `"forge: %s: missing [forge.board]"`.
    - Een status die niet in `status_options` staat: `"status %q is not in [forge.board] status_options"`, en er gaat geen request uit. Dit is de tekst van het oude `dispatch.NewBoard`.
    - `item.ItemID == ""`: een fout die het issue noemt.
- Gedrag van `Open`: kind `""` of `"github"` geeft een `*GitHub` met `BaseURL` `cfg.BaseURL` (zonder slash aan het eind) of `DefaultBaseURL`. Kind `"gitlab"` geeft `errors.New("forge: GitLab client not implemented yet")`. Een andere kind geeft `fmt.Errorf("forge: kind %q is not github or gitlab", kind)`.

- [ ] **Step 1: Schrijf de falende tests in `internal/forge`**

```go
func TestOpenGitHubDefaults(t *testing.T)      // Open(Config{Repo:"o/r",Token:"t"}) → *GitHub, BaseURL == DefaultBaseURL, Kind()=="github"; BaseURL "http://x/" → "http://x"
func TestOpenGitLabNotImplemented(t *testing.T) // err.Error() == "forge: GitLab client not implemented yet"
func TestOpenUnknownKind(t *testing.T)         // Kind "bitbucket" → err contains "bitbucket"
func TestBoardItemsStatusNames(t *testing.T) {
	// Board{ProjectID:"PVT_x", StatusFieldID:"SF", PriorityFieldID:"PF",
	//   StatusOptions:{"Controller":"c1"}, PriorityOptions:{"P1":"p1"}}
	// nodes: A {SF:c1, PF:p1}, B {} , C {SF:"zz", PF:"qq"}
	// want A Status "Controller" Priority "P1"; B "" ""; C Status "unknown:zz" Priority ""
}
func TestBoardMethodsWithoutBoard(t *testing.T) // Board nil → BoardItems, IssueItem, AddToBoard, SetStatus each err contains "missing [forge.board]"
func TestSetStatusMapsName(t *testing.T)       // was TestNewBoardMapsStatus: SetStatus(BoardItem{ItemID:"I7"}, "On hold") → srv.StatusSets[0] == {project:"PVT_x", item:"I7", field:"SF", option:"h1"}
func TestSetStatusUnknownName(t *testing.T)    // was TestNewBoardUnknownStatus: "Gone" → err contains "Gone"; len(srv.StatusSets)==0
func TestAddToBoardUsesCachedNodeID(t *testing.T) // Issue(ctx,7) then AddToBoard(ctx,7) → srv.Count("/repos/o/r/issues/7")==1; item.ItemID=="item-I_7", item.Issue==7
func TestAddToBoardReadsNodeID(t *testing.T)     // AddToBoard without earlier Issue → one GET of issue 7, then the mutation with content "I_7"
func TestAcknowledgeRoundTrip(t *testing.T)      // Acknowledge(ctx,7,id) → Acknowledgers(ctx,7,id) == [srv.Login]
```

Pas de bestaande forge-tests mechanisch aan:
- `*Client` wordt `*GitHub`.
- `BoardItems(ctx, "PVT_…")` wordt `c.Board = &repoconfig.Board{ProjectID: …}` plus `BoardItems(ctx)`. Hetzelfde geldt voor `IssueItem`, `AddToBoard` en `SetStatus`.
- Een verwachte `SingleSelect` wordt `Status`/`Priority` met een board in de test.
- `RoleName`, `AddRocket` en `RocketLogins` krijgen de nieuwe namen.

- [ ] **Step 2: Draai en zie falen**

Run: `go test ./internal/forge/`
Expected: FAIL (compile: `undefined: Open`, `unknown field Status`)

- [ ] **Step 3: Implementeer in `internal/forge`**

`itemNode.item()` geeft voortaan `(BoardItem, map[string]string, error)` terug: het item en de single-select-waarden per veld. `BoardItems` en `IssueItem` zetten daarmee `Status` en `Priority`. De board-methoden met ID's (projectID, fieldID, optionID) worden ongeëxporteerde helpers. `Issue.NodeID` verdwijnt; `issueItem.NodeID` blijft intern en vult de cache.

- [ ] **Step 4: Run de forge-tests**

Run: `go test ./internal/forge/...`
Expected: PASS

- [ ] **Step 5: Schrijf de falende dispatch-test voor Review Focus 1**

```go
func TestPrepareUnmappedStatusIsEmpty(t *testing.T) {
	// board: issue 7 with open run, status option "opt-prog" (not in testRepo's StatusOptions),
	// blocked by o/r#8 which is also on the board with "opt-prog"; issue 9 Controller blocked by #8
	// want pre.Items[issue 7].Status == "", pre.Statuses[7] == "",
	// pre.Items[issue 9].BlockedBy[0].Status == "" (no "unknown:" leaks into the comment text)
}
```

- [ ] **Step 6: Pas de aanroepers aan**

- `dispatch/gather.go`:
  - `Prepare` leest `bi.Status` en `bi.Priority`, en `c.BoardItems(ctx)`.
  - Nieuwe helper `known(status string) string` geeft `""` voor een waarde met prefix `unknown:`, anders de waarde. `Prepare` gebruikt die voor `Item.Status`, `statuses` en `boardStatus`.
  - `invert` verdwijnt.
  - `tokenRocket(ctx, c, issue int, pc, login)` roept `Acknowledgers(ctx, issue, pc.ID)` aan.
  - De check `repo.Board == nil` met `"dispatch: %s: missing [forge.board]"` blijft.
- `dispatch/trust.go`: `Role`.
- `dispatch/apply.go`:
  - `Poster` wordt `{CreateComment; Acknowledge(ctx, number int, commentID int64) error}`.
  - `Board` wordt `{SetStatus(ctx, item forge.BoardItem, status string) error}`.
  - `setStatus` geeft `forge.BoardItem{ItemID: a.ItemID, Repo: a.Repo, Issue: a.Issue}` mee.
  - `KindRocket` en `announce` roepen `Acknowledge(ctx, a.Issue, a.CommentID)` aan.
- `dispatch/board.go` en `board_test.go`: verwijderen.
- `overview.go`:
  - `BoardReader` wordt `{BoardItems(ctx) ([]forge.BoardItem, error)}`.
  - Nieuwe signatuur `ControllerIssues(r BoardReader, slug string) Filter`; die selecteert `it.Repo == slug && it.Status == "Controller"`.
- `cli/tick.go`:
  - `tickEnv.client(repo repoconfig.Repo, token string) *forge.GitHub` zet `Board = repo.Board`.
  - `applyActions`: `Board: func(slug) dispatch.Board { return env.client(repos[slug], token) }`.
- `cli/triage_gather.go`:
  - `boardFields` wordt `return it.Status, it.Priority`; `optionName` verdwijnt.
  - `IssueItem(ctx, n)` zonder project.
  - `recheck` geeft geen `nodeID` meer terug.
  - `closedIn(ctx, m forge.Milestone)` cachet op `m.Number`.
- `cli/triage.go`:
  - In `apply`: `item := forge.BoardItem{ItemID: after.ItemID, Repo: slug, Issue: f.Number}`. Is `ItemID` leeg, dan `item, err = g.c.AddToBoard(ctx, f.Number)`. Daarna `g.c.SetStatus(ctx, item, "Controller")`.
- `cli/serve.go`: `newForge(repo repoconfig.Repo, baseURL string, getenv) *forge.GitHub` zet ook het board; `ControllerIssues(c, r.Slug)`.
- `cli/ask.go`, `gather.go`, `run.go`: `*forge.Client` wordt `*forge.GitHub`.

Fakes en tests:
- `apply_test.go`:
  - `fakePoster.AddRocket` wordt `Acknowledge(_ context.Context, number int, commentID int64)`. Het logwoord `"AddRocket"` wordt `"Acknowledge"` in de fake en in de verwachte volgordes (`:279`, `:506`, `:1214`, `:1276`).
  - `fakeBoard.SetStatus` neemt `item forge.BoardItem` en logt `item.ItemID`, zoals nu de `itemID`.
- `gather_test.go` en `trust_test.go`: `client(t)` zet `c.Board = testRepo.Board`. `TestPrepareBlockerStatus` zet daarnaast `c.Board = &board`.
- `overview_test.go:227-265`:
  - `fakeBoard.BoardItems(ctx)`.
  - Items met `Status: "Controller"`, `"In progress"`, `""` in plaats van `SingleSelect`.
  - De assert `board.asked == "PVT_x"` vervalt: het project-ID is geen argument meer. De spec noemt deze test bij naam.
- `serve_test.go:395`, `:441`: `*forge.Client` wordt `*forge.GitHub`. `TestNewForgeTrimsSlash` roept `newForge` aan met een `repoconfig.Repo{Slug: "o/r"}`.

- [ ] **Step 7: Draai alles**

Run: `go test -race ./...`
Expected: PASS, met `TestPrepareUnmappedStatusIsEmpty` erbij.

- [ ] **Step 8: Commit**

```bash
git -C <worktree> add internal
git -C <worktree> commit -m "refactor(forge): GitHub-client implementeert forge.Forge met statusnamen (#258)"
```

---

### Task 4: `dispatch` op `forge.Forge`

**Files:**
- Modify: `internal/dispatch/gather.go`, `trust.go`, `apply.go`, `apply_test.go`
- Modify: `internal/cli/tick.go` (`applyActions`)

**Interfaces:**
- Consumes: `forge.Forge` (taak 3).
- Produces:
  - `func Prepare(ctx context.Context, f forge.Forge, repo repoconfig.Repo, open []runs.OpenRun, branches map[int]string) (Pre, error)`
  - `func Complete(ctx context.Context, f forge.Forge, pre Pre, trust *Trust, tokenLogin string, log io.Writer, now time.Time) (Snapshot, error)`
  - `func NewTrust(f forge.Forge, allowed []string) *Trust`
  - `dispatch.Env.Forge func(repo string) forge.Forge` vervangt `Env.Poster` en `Env.Board`. De interfaces `Poster` en `Board` verdwijnen.

- [ ] **Step 1: Pas `apply_test.go` aan (de test faalt dan op compile)**

`fakePoster` en `fakeBoard` worden één `fakeForge struct { forge.Forge; …zelfde velden… }` met `CreateComment`, `Acknowledge` en `SetStatus`; logs en velden blijven gelijk. `h.poster(repo)` geeft `*fakeForge`. De harness zet `Env.Forge`. Een aanroep van een andere methode geeft een panic op de nil-`Forge`: dat is gewenst.

- [ ] **Step 2: Draai en zie falen**

Run: `go test ./internal/dispatch/`
Expected: FAIL (`unknown field Forge in struct literal of type Env`)

- [ ] **Step 3: Implementeer de signaturen hierboven; `applyActions` zet `Forge: func(slug) forge.Forge { return env.client(repos[slug], token) }`**

- [ ] **Step 4: Run**

Run: `go test -race ./internal/dispatch/ ./internal/cli/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git -C <worktree> add internal/dispatch internal/cli/tick.go
git -C <worktree> commit -m "refactor(dispatch): gebruik forge.Forge (#258)"
```

---

### Task 5: `overview` en `server` op `forge.Forge`

**Files:**
- Modify: `internal/overview/overview.go`, `overview_test.go`
- Modify: `internal/server/forge_test.go`, `latest_test.go`, `server_test.go`
- Modify: `internal/cli/serve.go` (alleen types)

**Interfaces:**
- Produces:
  - `overview.RepoSource.Src forge.Forge`
  - `func Build(ctx context.Context, src forge.Forge, inController Filter) (Overview, error)`
  - `func ControllerIssues(f forge.Forge, slug string) Filter`
  - `Source` en `BoardReader` verdwijnen.

- [ ] **Step 1: Pas de fakes aan**

Elke fake bedt `forge.Forge` in en houdt zijn eigen methoden: `overview_test.fakeSource`, `overview_test.fakeBoard`, `server/forge_test.fakeSource`, `latest_test.failingComments` (bedt `fakeSource` in, zoals nu) en `server_test.stubSource`. `testDeps(src forge.Forge, …)` en het tabelveld `src` in `latest_test.go:48` krijgen het type `forge.Forge`.

- [ ] **Step 2: Draai en zie falen**

Run: `go test ./internal/overview/ ./internal/server/`
Expected: FAIL (compile)

- [ ] **Step 3: Implementeer de signaturen**

- [ ] **Step 4: Run**

Run: `go test -race ./internal/overview/ ./internal/server/ ./internal/cli/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git -C <worktree> add internal/overview internal/server internal/cli/serve.go
git -C <worktree> commit -m "refactor(overview): gebruik forge.Forge (#258)"
```

---

### Task 6: Eén forge per repo in `tick`, `gather` en `run`

**Files:**
- Create: `internal/cli/forges.go`
- Test: `internal/cli/forges_test.go`, `internal/cli/tick_test.go`
- Modify: `internal/cli/tick.go`, `gather.go`, `run.go`

**Interfaces:**
- Consumes: `forge.Token`, `forge.TokenError` (taak 2); `forge.Open`, `forge.Config` (taak 3); `repoconfig.LoadAll` (taak 1); `dispatch.Prepare`, `Complete`, `NewTrust`, `Env.Forge` (taak 4).
- Produces (package `cli`):

```go
// forgeOpener opens the forge of each repo with that repo's token, and shares the http client
// and the token login of forges with the same kind, host, base URL and token.
type forgeOpener struct {
	getenv    func(string) string
	githubURL string                     // override for GitHub repos only; "" is the default
	ghToken   func() string              // last resort for a GitHub token (ask); nil is none
	open      func(forge.Config) (forge.Forge, error) // forge.Open; tests replace it
	http      *http.Client
	logins    map[loginKey]func() (string, error)
}
type loginKey struct{ kind, host, baseURL, token string }
type repoForge struct {
	forge.Forge
	who func() (string, error) // tokenLogin, once per loginKey
}
func newForgeOpener(getenv func(string) string, githubURL string) *forgeOpener
func (o *forgeOpener) forRepo(ctx context.Context, repo repoconfig.Repo) (repoForge, error) // the token error is a *forge.TokenError
func tokenLogin(ctx context.Context, f forge.Forge) (string, error) // as today, on forge.Forge
```

- `tickEnv.client` verdwijnt. `tickEnv.ForgeURL` houdt de betekenis "GitHub-API-root, alleen voor GitHub-repo's".
- `tick`:
  - Laadt de repo's met `repoconfig.LoadAll(cfg.Repos)`.
  - Opent eerst, zonder uitvoer, een forge voor elke repo die laadt en `Dispatchable` is.
  - Opent geen enkele repo een forge, dan geeft `tick` `"tick: " + <eerste *TokenError>` terug. Dat gebeurt vóór de store opengaat; dit is de huidige melding.
  - Is er geen tokenfout omdat geen repo een forge nodig had, dan geldt de huidige check `forge.Token("github", "", getenv)`. Zo blijven `TestTickWithoutToken` en `e2e/tick_test.go:101` ongewijzigd.
  - Daarna volgt de bestaande lus in de bestaande volgorde. Een repo met een fout bij het laden, een dubbele slug, een fout van `Dispatchable`, een token- of `Open`-fout krijgt `lighthouse: skip <path>: <err>`.
- `gatherRepo`, `sweepOne`, `decideAndApply` en `applyActions` krijgen `map[string]forge.Forge` of `repoForge` in plaats van `token string`.
- `gather` en `run` openen de forge van hun ene repo. Een tokenfout geeft `"gather: " + err` en `"run: " + err`, met dezelfde teksten als nu.

- [ ] **Step 1: Schrijf de falende tests**

```go
func TestOpenerOverrideOnlyForGitHub(t *testing.T) {
	// open replaced by a spy that records Config
	// githubURL "http://override"; repo kind gitlab, GITLAB_TOKEN set → spy got BaseURL "" and Kind "gitlab"
	// repo kind github → spy got BaseURL "http://override"
}
func TestOpenerTokenPerKind(t *testing.T) {
	// GH_TOKEN "gh", GITLAB_TOKEN "gl": github repo → Config.Token "gh"; gitlab repo → "gl";
	// gitlab repo with TokenEnv "WORK" unset → *forge.TokenError, its text has no token value
}
func TestOpenerSharesLogin(t *testing.T) {
	// two github repos, same token → forRepo(a).who and forRepo(b).who: one GET /user (srv.Count("/user")==1)
}
func TestTickSkipsRepoWithoutToken(t *testing.T) {
	// host.toml with two dispatchable repos: o/r (github, GH_TOKEN set) and o/x (token_env "MISSING")
	// want stderr has "lighthouse: skip <dir of o/x>: no GitHub token: set MISSING\n", "gathered o/r", err == errSilent
}
func TestTickOneLoginForSharedToken(t *testing.T) {
	// two github repos due in one tick → srv.Count("/user") == 1
}
func TestTickSkipsDuplicateSlug(t *testing.T) {
	// two repo dirs with the same slug → two skip lines, each naming both dirs; no gather
}
```

`TestNewForgeTrimsSlash` en `TestGithubToken` in `serve_test.go` gaan op in `TestOpenerOverrideOnlyForGitHub` en in `forge.TestTokenGitHubOrder` (taak 2); verwijder ze in taak 7, samen met `newForge` en `githubToken`.

- [ ] **Step 2: Draai en zie falen**

Run: `go test ./internal/cli/ -run 'TestOpener|TestTickSkips|TestTickOneLogin'`
Expected: FAIL (`undefined: newForgeOpener`)

- [ ] **Step 3: Implementeer `forges.go` en zet `tick`, `gather`, `run` om**

`forRepo` doet in volgorde:
1. `forge.Token(repo.Kind, repo.TokenEnv, getenv)`. Bij een `*TokenError` voor kind github, en alleen als `ghToken != nil`, volgt `ghToken()`.
2. `Config{Kind, Repo: repo.Slug, Host: repo.Host, Token, BaseURL: githubURL alleen als kind github is, Board: repo.Board, HTTP: o.http}`.
3. `o.open(cfg)`.
4. `who` uit `o.logins[key]` of een nieuwe `sync.OnceValues`.

- [ ] **Step 4: Run**

Run: `go test -race ./internal/cli/ && go test ./e2e/...`
Expected: PASS. `TestTickWithoutToken`, `TestGatherWithoutToken` en e2e `no token` slagen ongewijzigd.

- [ ] **Step 5: Commit**

```bash
git -C <worktree> add internal/cli
git -C <worktree> commit -m "feat(cli): één forge per repo in tick, gather en run (#258)"
```

---

### Task 7: `triage`, `serve` en `ask` via de opener

**Files:**
- Modify: `internal/cli/triage.go`, `triage_gather.go`, `serve.go`, `ask.go`
- Test: `internal/cli/triage_test.go`, `serve_test.go`, `ask_test.go`

**Interfaces:**
- Consumes: `forgeOpener`, `repoForge` (taak 6); `repoconfig.LoadAll` (taak 1).
- Produces:
  - `repoGather.c forge.Forge`; `readWork(ctx, f forge.Forge, db, repo)`.
  - `forgeRepos(hostToml, repo, forgeURL string, getenv func(string) string, errOut io.Writer) ([]overview.RepoSource, error)` houdt zijn signatuur.
  - Flag `--forge-url` met default `envOr("LIGHTHOUSE_FORGE_URL", "")`. Helptekst: `"GitHub API base URL for GitHub repos (default: $LIGHTHOUSE_FORGE_URL, else https://api.github.com); the GitHub token is sent there, never a GitLab one"`.
- Gedrag:
  - `triage` laadt met `LoadAll`, opent per repo met `[controller.triage]` en geeft een skip-regel per repo zonder token. Opent geen enkele repo, dan geeft `triage` `&exitError{code: 2, err: "triage: " + <eerste *TokenError>}`, vóór andere uitvoer. Daarmee blijft `triage_brake_test.go:146` letterlijk gelijk.
  - `serve` faalt nooit op een token. Een repo zonder token, met een dubbele slug of met een `Open`-fout krijgt `lighthouse: skip <path>: <err>` en wordt overgeslagen. `--repo` zonder `[dispatch]` is een GitHub-repo (`repoconfig.Repo{Slug: repo, Kind: "github"}`); zonder token krijgt die ook een skip-regel, en de endpoints geven dan 503 zoals nu.
  - `ask` gebruikt een opener met `ghToken: env.GhToken` en `githubURL: env.ForgeURL`. Bij een tokenfout geeft het `<err>; nothing posted`, met dezelfde tekst als nu. `askEnv.token()` verdwijnt.

- [ ] **Step 1: Schrijf de falende tests**

```go
func TestServeSkipsRepoWithoutToken(t *testing.T) // two [dispatch] repos, one with token_env "MISSING" → one RepoSource, errOut "lighthouse: skip <dir>: no GitHub token: set MISSING\n"
func TestServeForgeURLDefaultEmpty(t *testing.T)   // serve --help still has "https://api.github.com"; flag default value == ""
func TestAskGhTokenOnlyForGitHub(t *testing.T)    // kind gitlab repo, no GITLAB_TOKEN, GhToken returns "x" → error contains "GITLAB_TOKEN" and "nothing posted", no POST
func TestTriageSkipsRepoWithoutToken(t *testing.T) // two triage repos, one without token → skip line for it, the other gets its summary line
```

`TestAskTokenOrder` (`ask_test.go:233-244`) toetst `env.token()`, en dat verdwijnt. Herschrijf de test op `forRepo` met dezelfde drie gevallen en dezelfde volgorde: `GH_TOKEN`, `GITHUB_TOKEN`, `GhToken`. `serve_test.go:395`/`:441` asserten `g.Src.(*forge.GitHub)` met dezelfde velden. Verwijder `TestGithubToken` en `TestNewForgeTrimsSlash`, met `githubToken` en `newForge`.

- [ ] **Step 2: Draai en zie falen**

Run: `go test ./internal/cli/ -run 'TestServeSkips|TestServeForgeURL|TestAskGhToken|TestTriageSkips'`
Expected: FAIL

- [ ] **Step 3: Implementeer**

- [ ] **Step 4: Run**

Run: `go test -race ./... && grep -rn 'forge\.Client\|githubToken\|forge\.New(' internal cmd --include=*.go | grep -v '^internal/forge/'`
Expected: de tests PASS en grep vindt niets.

- [ ] **Step 5: Commit**

```bash
git -C <worktree> add internal/cli
git -C <worktree> commit -m "feat(cli): triage, serve en ask openen een forge per repo (#258)"
```

---

### Task 8: Conformiteitssuite, eerst tegen GitHub

**Files:**
- Create: `internal/forge/forgesuite/suite.go` (package `forgesuite`; alleen tests importeren het)
- Create: `internal/forge/github_contract_test.go` (package `forge_test`: de fixture op `forgetest.Server`)

**Interfaces:**
- Consumes: `forge.Forge`, `forge.Open`, `forgetest.New`.
- Produces (model: `internal/workplace/workplacetest`). #259 schrijft een tweede fixture tegen deze `Fixture`.

```go
// Fixture is one forge implementation against its fake server. Its board maps the lighthouse
// statuses "To do", "Controller", "On hold" and "Done" and the priorities "P1" and "P2".
type Fixture interface {
	Forge() forge.Forge
	Repo() string
	Login() string // the token's user
	AddIssue(t *testing.T, is forge.Issue)          // Number, Title, Body, Author, State ("open"/"closed"), Labels, Milestone, ClosedAt
	AddComment(t *testing.T, number int, author, body string) int64
	SetRole(t *testing.T, user, role string)        // a neutral role word
	AddBlocker(t *testing.T, number int, b forge.Blocker)
	AddOpenPR(t *testing.T, pr forge.OpenPR)
	AddBranchPR(t *testing.T, branch string, pr forge.PR)
	PutOnBoard(t *testing.T, number int, status, priority string) // lighthouse names; "" none
	PutOnBoardUnmapped(t *testing.T, number int) (raw string)     // a status the board config does not map
	StatusOf(t *testing.T, number int) string                     // the last status the forge wrote for number; "" none
}
type Case struct { Name string; Run func(t *testing.T, f Fixture) }
var Cases []Case
func Run(t *testing.T, newFixture func(t *testing.T) Fixture)
```

- [ ] **Step 1: Schrijf de suite (eerst falen ze, want er is nog geen fixture)**

Elk geval is een sub-test met een nieuwe fixture. Alle asserts gaan over neutrale velden:

| Geval | Assert |
|---|---|
| `Login` | `Login(ctx) == f.Login()` |
| `Role` | `SetRole("ann","write")` → `Role("ann") == "write"`; `Role("nobody") == ""`, `err == nil` |
| `IssueFields` | `Issue(7)` heeft Number, Title, Body, Author, `State "open"`, Labels, `Milestone{Number, Title, State "open"}` uit `AddIssue`; `URL != ""` |
| `OpenIssuesPages` | 101 open en 1 gesloten → `len(OpenIssues) == 101` |
| `ClosedInMilestone` | twee gesloten in mijlpaal M, gesloten op now−1h en now−48h; `since` now−24h → alleen de eerste |
| `BlockedBy` | blokkeerders open en gesloten → `State` `OPEN`/`CLOSED`, `Repo`, `Issue` |
| `Comments` | `AddComment` → `Comments` bevat Author, Body, `ID`; `CreateComment` → `ID > 0`, `Author == f.Login()`, en daarna in `Comments` |
| `Acknowledge` | `Acknowledge(n, id)` → `Acknowledgers(n, id)` bevat `f.Login()`; `Comments(n)` geeft die comment `Rockets == 1` |
| `BoardStatusAndPriority` | 7 `("Controller","P1")`, 8 `("","")`, 9 unmapped → Status `"Controller"`/`""`/`"unknown:"+raw`, Priority `"P1"`/`""`; `Repo == f.Repo()` |
| `IssueItem` | `PutOnBoard(7,"On hold","")` → `IssueItem(7)` `ok`, `Status "On hold"`, `Issue 7` |
| `AddToBoardThenSetStatus` | issue 7 niet op het board → `AddToBoard(7)`: `Issue == 7`, `ItemID != ""`; `SetStatus(item,"Controller")` → `StatusOf(7) == "Controller"` |
| `SetStatusUnknownName` | `SetStatus(item,"Nope")` → err bevat `"Nope"`; `StatusOf` ongewijzigd |
| `PRFields` | `AddBranchPR("issue-7-x", PR{Number 12, State "MERGED", HeadSHA, HeadRef, Checks "SUCCESS", Mergeable "MERGEABLE", MergedAt})` → `BranchPRs("issue-7-x")` geeft die velden, `Closes == false`; `AddOpenPR` → `OpenPRs` geeft Number, Title, Body, HeadRef |

- [ ] **Step 2: Schrijf de GitHub-fixture in `github_contract_test.go`**

`func TestGitHubConformance(t *testing.T) { forgesuite.Run(t, newGitHubFixture) }`. De fixture vertaalt naar de velden van `forgetest.Server`:
- `Issues[n]` met `node_id "I_<n>"`.
- Blokkeerders in `BlockedBy` en ook in de `blockedBy` van het board-node.
- `Roles`, `Pulls`, `Branches`.
- `BoardNodes` met item-ID `"item-<n>"` en option-ID's uit een vaste `repoconfig.Board`.
- `PutOnBoardUnmapped` gebruikt option `"opt-unmapped"`.

`StatusOf` leest de laatste `srv.StatusSets` met `item` van dat issue (`"item-<n>"` of `"item-I_<n>"`) en vertaalt `option` terug naar de naam. De fixture opent via `forge.Open(forge.Config{Repo: "o/r", Token: "secret-token", BaseURL: srv.URL(), Board: &board})`. `forgetest` verandert niet.

- [ ] **Step 3: Run**

Run: `go test -race ./internal/forge/...`
Expected: PASS, alle 13 gevallen.

- [ ] **Step 4: Mutatiecheck**

Breek in `GitHub.BoardItems` tijdelijk de `unknown:`-prefix en, apart, de omzetting van `Acknowledge` naar de rocket-POST. Draai de suite; elk geval dat bij die code hoort moet falen. Draai dat terug.

- [ ] **Step 5: Commit**

```bash
git -C <worktree> add internal/forge/forgesuite internal/forge/github_contract_test.go
git -C <worktree> commit -m "test(forge): conformiteitssuite, eerst tegen GitHub (#258)"
```

---

### Task 9: Documentatie en eindcontrole

**Files:**
- Modify: `docs/tick.md:15-16`, `docs/triage.md:75`, `docs/ask.md:23`
- Create: `changelog.d/issue-258-forge-interface.md` (door `/lighthouse-write-pr`)

- [ ] **Step 1: Pas de docs aan (in het Nederlands, zoals die pagina's)**

- `tick.md`, stap 2 *Token*: het token komt per repo uit `GH_TOKEN`/`GITHUB_TOKEN` (github) of `GITLAB_TOKEN` (gitlab), of uit de variabele die `[forge] token_env` noemt. Een repo zonder token krijgt een skip-regel. Zonder enig token stopt tick met `tick: no GitHub token: set GH_TOKEN or GITHUB_TOKEN`. `--forge-url` en `LIGHTHOUSE_FORGE_URL` gelden alleen voor GitHub-repo's.
- `triage.md:75` en `ask.md:23`: hetzelfde, in één zin. Bij `ask` blijft `gh auth token` de laatste bron, alleen voor GitHub.

- [ ] **Step 2: Volledige verificatie**

Run: `go test -race ./...`
Expected: PASS

Run: `golangci-lint run ./...`
Expected: `0 issues.`

Run: `uv run pytest -q tests/test_site_config.py`
Expected: PASS

Run: `go run ./cmd/lighthouse init --upgrade`
Expected: `No changes.`

Run: `grep -rn 'SingleSelect\|NodeID\|RoleName\|RocketLogins\|AddRocket\|NewBoard\|StatusSetter' internal --include=*.go | grep -v '_test.go'`
Expected: niets, behalve het interne `issueItem.NodeID` in `internal/forge/issues.go`.

- [ ] **Step 3: Commit**

```bash
git -C <worktree> add docs
git -C <worktree> commit -m "docs: token per repo en --forge-url alleen voor GitHub (#258)"
```

- [ ] **Step 4: PR via `/lighthouse-write-pr`** (die schrijft ook het changelog-fragment, type `added`: `[forge] token_env` en een forge per repo)
