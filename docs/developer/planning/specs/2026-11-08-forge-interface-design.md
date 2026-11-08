# Forge-interface en GitLab-client — ontwerp

Issues: #258 (interface), #259 (GitLab-client). Milestone: GitLab-forge.
Bron: [M3-spec, vraag 3](2026-11-07-m3-tweede-omgeving-design.md).

## Waarom

Lighthouse moet even goed werken met een repo op GitLab als met een repo op GitHub. GitLab is
dan een volwaardige vervanger als git-server: de handmatige keten, `serve`, triage en de
dispatcher (`tick`). Er draait niets bínnen GitLab, zoals GitLab CI.

De eerste gebruiker is de eigenaar zelf, op het werk. Daar draait een interne, self-hosted GitLab
19.4 EE met minstens een Premium-licentie: de keuze *relates to / blocks / is blocked by* bij
*Linked items* is er. Daarmee zijn de ingebouwde issuestatus, scoped labels en blokkade-links
beschikbaar. Lukt het niet op
die server, dan wacht dit werk tot het bedrijf naar een publieke GitLab gaat. gitlab.com moet
ook werken, maar is niet het eerste doel.

Nu kent lighthouse alleen GitHub. `internal/forge` is één concrete GitHub-client
(`*forge.Client`, REST en GraphQL), en de code die hem gebruikt praat er rechtstreeks mee. De
smalle interfaces die er al zijn (`dispatch.Poster`, `dispatch.StatusSetter`, `overview.Source`)
dragen GitHub-begrippen: Projects v2-ID's, raket-reacties, collaborator-rollen.

## Doelen, niet-doelen, anti-doelen

**Doelen**

- Eén interface `forge.Forge` die beschrijft wat lighthouse van een forge vraagt, met
  forge-neutrale types.
- De GitHub-client is de eerste implementatie, de GitLab-client de tweede (#259).
- Alle afnemers (`internal/dispatch`, `internal/overview`, `internal/triage`, `internal/server`,
  `internal/cli`) gebruiken `forge.Forge`, nooit meer `*forge.Client`.
- Het token komt per soort forge uit de omgeving, met een configuratiesleutel voor de naam van
  de variabele.
- De milestone-vraag "Of de dispatcher op GitLab in deze milestone hoort" is beantwoord: ja, alles
  werkt met GitLab, de dispatcher inbegrepen.
- **Gemengd in één installatie.** Eén `tick`, `triage` en `serve` bedienen tegelijk repo's op
  GitHub en op GitLab (ook op verschillende hosts). Een GitLab-repo is gewoon een andere repo.

**Niet-doelen**

- De forge-wrapper voor skills en hooks (`lighthouse forge …`, #260). Die wordt wel een afnemer
  van deze interface; zie *Raakvlak met #260*.
- Een werkplek zonder kenn-forge. Dat is een eigen milestone; de dispatcher op een GitLab-repo
  gebruikt de werkplek die er nu is.
- Een derde forge (no-go van de milestone).

**Anti-doelen** (werkt technisch, maar is toch mislukt)

- Een interface die alleen GitHub goed past, waarbij GitLab met trucs wordt ingepast. Daarom
  staan interface en GitLab-client in één spec.
- Gedrag op GitHub dat verandert. De bestaande tests blijven inhoudelijk gelijk.
- Een token dat in `lighthouse.toml` of in een log terechtkomt.

## Randvoorwaarden

- GitLab REST API v4 (`https://<host>/api/v4`), voor gitlab.com en self-hosted. GitLab 19.4 is
  het minimum dat we testen. De GitLab GraphQL-API mag voor lezen, waar die aanroepen bespaart
  (*mijn keuze*).
- **Aanroepbudget.** `BoardItems` draait elke tick (5 min) voor elke repo, en ook in triage en
  `serve`. Op GitLab kost het een vast aantal pagina's, niet een aantal aanroepen per issue. De
  bouwer kiest hoe (GraphQL, of REST met batching); een ontwerp met aanroepen per issue per tick
  voldoet niet (*mijn keuze*).
- Premium-functies mogen gebruikt worden: de ingebouwde issuestatus (work item status), scoped
  labels (`priority::…`, één per scope, door GitLab afgedwongen) en issue-links van het type
  `is_blocked_by`.
- **De ingebouwde status kent alleen GraphQL.** Lezen via de widget `WorkItemWidgetStatus`,
  zetten via `workItemUpdate` met `statusWidget: {name: …}` (naam, hoofdletterongevoelig). In
  GitLab 19.3 heet die widget nog *Experiment*: de API kan veranderen. De conformiteitssuite moet
  zo'n wijziging vangen.
- **Eigen statussen staan op de bovenste groep.** GitLab levert standaard *To do*, *In progress*,
  *Done*, *Won't do* en *Duplicate*. Lighthouse gebruikt ook *On hold* en *Controller*. Die moet
  iemand met de rol Maintainer of Owner op de **bovenste** groep toevoegen (*Settings → Work
  items → Statuses*). Zonder die twee werkt de handmatige keten wel, de dispatcher niet.
- **De configuratie leest Go, niet Python.** #274 maakt Go de enige lezer van `lighthouse.toml`
  (milestone *Lighthouse zonder Python*). Wat deze spec aan configuratie toevoegt, landt daar.
- Een GitLab-pad kan subgroepen hebben (`groep/sub/project`); in de API wordt het URL-gecodeerd
  als project-ID gebruikt. `[forge] host` uit #257 geeft de self-hosted host.
- Code, namen en commentaar in het Engels; deze spec in het Nederlands.
- `/lighthouse-go-standard` en `/lighthouse-test-standard`.

## Aanpakken

| | Aanpak | Gevolg |
|---|---|---|
| **A** | **Eén brede interface `forge.Forge` in `internal/forge`; alle afnemers gebruiken die** | **Gekozen door de eigenaar.** Het hele contract op één plek, makkelijk uit te breiden. Testnabootsingen moeten de hele interface leveren |
| B | Smalle interfaces bij elke afnemer, gedeelde types in `internal/forge` | Bestaande fakes blijven ongewijzigd, past bij de M3-spec. Het contract staat verspreid |
| C | Kleine kern plus optionele extra's (bevestiging, rechten, board) die alleen GitHub heeft | Valt af: GitLab moet alles kunnen |

Waarom A: de eigenaar wil één plek waar het contract staat, zodat er later op verder gebouwd kan
worden. Daarmee wijkt dit af van de richting in de M3-spec ("een interface aan de kant van de
afnemer"); deze spec gaat voor.

## Het ontwerp

### De interface

Eén interface in `internal/forge`. Elke methode werkt op de repo waarvoor de forge is geopend.
Een schets; de precieze namen zijn aan de bouwer.

```go
type Forge interface {
    Kind() string // "github" of "gitlab"

    // Identiteit en rechten
    Login(ctx context.Context) (string, error)
    Role(ctx context.Context, user string) (Role, error)

    // Issues
    Issue(ctx context.Context, number int) (Issue, error)
    OpenIssues(ctx context.Context) ([]Issue, error)
    ClosedInMilestone(ctx context.Context, m Milestone, since time.Time) ([]Issue, error)
    BlockedBy(ctx context.Context, number int) ([]Blocker, error)

    // Comments en het bevestigingsprotocol
    Comments(ctx context.Context, number int) ([]protocol.Comment, error)
    CreateComment(ctx context.Context, number int, body string) (protocol.Comment, error)
    Acknowledge(ctx context.Context, number int, commentID int64) error
    Acknowledgers(ctx context.Context, number int, commentID int64) ([]string, error)

    // Pull requests (GitHub) en merge requests (GitLab)
    OpenPRs(ctx context.Context) ([]OpenPR, error)
    BranchPRs(ctx context.Context, branch string) ([]PR, error)

    // Het board: status en prioriteit per issue
    BoardItems(ctx context.Context) ([]BoardItem, error)
    IssueItem(ctx context.Context, number int) (item BoardItem, ok bool, err error)
    AddToBoard(ctx context.Context, number int) (BoardItem, error)
    SetStatus(ctx context.Context, item BoardItem, status string) error
}
```

Wat verandert ten opzichte van `*forge.Client` nu:

- **Geen GitHub-ID's meer in de interface.** `SetStatus` neemt een statusnaam ("In progress"),
  geen project-, field- en option-ID. `BoardItem` krijgt de velden `Status` en `Priority` als
  naam. De vertaling van naam naar Projects v2-option-ID (nu `dispatch.NewBoard` en
  `optionName` in `internal/cli/triage_gather.go`) verhuist naar de GitHub-client.
  `BoardItem.SingleSelect` en het project-ID als argument verdwijnen.
  **Status leeg betekent "geen status"**, en alleen dat. Een status die de config niet kent,
  wordt `unknown:<ruwe waarde>` (option-ID op GitHub, statusnaam op GitLab), zoals triage nu al
  doet (`internal/cli/triage_gather.go:64-72`). Zo blijft een issue in een niet-gekoppelde kolom
  geen triagekandidaat. De dispatcher behandelt `unknown:…` als een status die hij niet kent,
  net als nu een lege naam uit `invert` in `internal/dispatch/gather.go:73-77`
  *(eigen keuze na spec-review, r1-f3)*.
- **`BoardItems` geeft het hele board.** Op GitHub zijn dat de items van alle repo's op het
  Projects v2-board, zoals nu; afnemers blijven filteren op `Repo` en gebruiken de andere items
  voor de status van een blokkeerder. Op GitLab is het board het project
  *(eigen keuze na spec-review, r1-f9)*.
- **`AddToBoard` neemt een issuenummer**, geen GraphQL-node-ID. De GitHub-client zoekt het
  node-ID zelf op. `Issue.NodeID` verdwijnt uit het neutrale type als niets anders het nog
  gebruikt.
- **`AddRocket`/`RocketLogins` heten `Acknowledge`/`Acknowledgers`** en krijgen het issuenummer
  mee, omdat GitLab een award emoji op een note alleen via het issue kan zetten.
- **`RoleName` wordt `Role`** en geeft een rolnaam als string terug (zie *Rechtentoets*).
- **`ClosedInMilestone` neemt een `Milestone`**, geen nummer. GitLab filtert op titel; de
  GitHub-client gebruikt het nummer. Op GitLab is `Milestone.Number` het globale milestone-ID,
  niet het `iid`: een project- en een groepsmilestone kunnen hetzelfde `iid` hebben, en triage
  cachet per nummer (`internal/cli/triage_gather.go:163-178`)
  *(eigen keuze na spec-review, r1-f12)*.
- `Kind()` is er voor meldingen en voor de paar plekken die tekst per forge kiezen (`#N` tegen
  `!N` voor een MR).

### Openen: `forge.Open`

```go
func Open(cfg Config) (Forge, error)
```

`Config` bevat `Kind`, `Repo`, `Host`, `Token`, `BaseURL` (leeg is de standaard), het board uit
`[forge.board]` en een `*http.Client`. `Open` kiest de GitHub- of de GitLab-client. Tot #259
klaar is, geeft `kind = "gitlab"` een duidelijke fout ("GitLab client not implemented yet").
Beide clients staan in package `forge` (bijvoorbeeld `github.go` en `gitlab.go`), zodat `Open`
geen importcyclus maakt *(eigen keuze na spec-review, r1-f11)*.

**Basis-URL.** Een token gaat nooit naar een host van een andere forge. De GitHub-basis is
`https://api.github.com`; de GitLab-basis is `https://<[forge] host>/api/v4`, zonder `host`
`https://gitlab.com/api/v4`. Een override (`--forge-url`, `LIGHTHOUSE_FORGE_URL`,
`tickEnv.ForgeURL`, `askEnv.ForgeURL`) geldt alleen voor repo's van één soort: de flag krijgt een
lege default en een soort-specifieke tegenhanger, of geldt alleen voor GitHub. Nu krijgt elke
repo in `serve` de basis `https://api.github.com` (`internal/cli/serve.go:58`, `:72-75`); een
GitLab-repo zou daar `GITLAB_TOKEN` naartoe sturen *(eigen keuze na spec-review, r1-f7)*.

Alle plekken die nu `forge.New` aanroepen (`internal/cli/serve.go:73`, `ask.go:153`,
`tick.go:71`) gaan via `forge.Open`.

**Eén forge per repo.** Omdat een installatie GitHub- en GitLab-repo's mengt, opent elk commando
een forge per repo, met het token en de login van die repo. Forges met dezelfde soort, host en
token delen hun client en hun `Login`-uitkomst, zodat er per tick niet meer aanroepen komen dan
nu. Nu leest `tick` één token bij de start en neemt de login van de eerste repo
(`internal/cli/tick.go:193-206`); dat verdwijnt. Ontbreekt het token van één repo, dan slaat het
commando die repo over met een melding en gaat het door met de rest. Alleen zonder enig
bruikbaar token faalt het hele commando. `repoconfig.Repo` krijgt het veld `Host` uit
`[forge] host` (#257 heeft de sleutel in Python en `init` gelegd, nog niet in `repoconfig`).

**De slug blijft de identiteit.** Run-rijen, markers, `Env.Poster(slug)` en `Env.Board(slug)`
sleutelen op het repo-pad (`internal/store/migrations/0004_runs_fixpr.sql:23`,
`internal/dispatch/apply.go:34-35`, `internal/cli/tick.go:390-392`). Een slug komt daarom
hoogstens één keer voor per installatie, welke soort forge of host het ook is: ook een
GitHub-repo en een gitlab.com-repo met hetzelfde pad botsen (`Host` is voor beide leeg).
Lighthouse controleert dat bij het laden van de `lighthouse.toml` van elke repo uit
`host.toml` (`host.toml` zelf bevat alleen paden, `internal/hostconfig/hostconfig.go:39`) en
weigert een dubbele slug met een melding die beide repo's noemt *(r2-f1)*. Een hostbewuste identiteit met een migratie van de runs-database is
pas nodig als dat echt voorkomt *(eigen keuze na spec-review, r1-f1)*.

### Hoe GitLab elk begrip invult

| Begrip | GitHub (nu) | GitLab |
|---|---|---|
| Issue | issue, `number` | issue, `iid` (`#N`); project-ID is het URL-gecodeerde pad |
| `Issue.State` | `open`, `closed` (REST) | `opened`→`open`, `closed`→`closed` |
| `BoardItem.IssueState`, `Blocker.State` | `OPEN`, `CLOSED` (GraphQL) | `opened`→`OPEN`, `closed`→`CLOSED` |
| `Milestone.State` | `open`, `closed` | `active`→`open`, `closed`→`closed` |
| Comment | issue comment | note op het issue; **systeemnotes (`system: true`) tellen niet mee** |
| PR | pull request | merge request (`!N`) |
| `PR.State` | OPEN/CLOSED/MERGED | `opened`→OPEN, `closed`→CLOSED, `merged`→MERGED; `locked` als OPEN |
| `PR.Checks` | `statusCheckRollup.state` | status van `head_pipeline`: `success`, `skipped`→SUCCESS; `failed`, `canceled`→FAILURE; `running`, `pending`, `created`, `preparing`, `waiting_for_resource`, `scheduled`, `manual`→PENDING; geen pipeline → "" |
| `PR.Mergeable` | MERGEABLE/CONFLICTING/UNKNOWN | betekent alleen "geen conflicten", zoals op GitHub: `has_conflicts` → CONFLICTING; `detailed_merge_status` `unchecked`, `checking`, `preparing` en elke andere status waarin de conflictcontrole nog niet klaar is → UNKNOWN; anders MERGEABLE. Goedkeuringen en open threads tellen niet mee *(r1-f5)* |
| `PR.Closes` | `closedByPullRequestsReferences` | het issue staat in `closes_issues` van de MR (of de MR in `closed_by` van het issue) |
| `PR.CrossRepo` | head in een fork | `source_project_id` ≠ `target_project_id` |
| Status | Projects v2 single-select-veld | ingebouwde issuestatus (work item status), via GraphQL; de config koppelt elke lighthouse-statusnaam aan een GitLab-statusnaam |
| Prioriteit | Projects v2 single-select-veld | scoped label `priority::<naam>` (GitLab heeft geen prioriteitsveld) |
| Board | Projects v2-project | alle issues van het project: elk issue heeft een status; `ItemID` is het `iid`. `BoardItems` geeft open issues plus gesloten issues uit een venster dat de bouwer kiest. De dispatcher sluit een run alleen af via zijn board-item (`internal/dispatch/gather.go:88-98`, `decide.go:494-497`). Daarom haalt `dispatch.Prepare`, forge-neutraal, met `IssueItem` het item op van elk issue met een open run dat niet in `BoardItems` zit. Op GitHub kost dat niets extra, want daar staat zo'n issue al op het board. Een dispatch-test dekt een gesloten issue met een open run dat buiten het venster valt: de run wordt afgesloten. Het aanroepbudget geldt voor open issues plus het venster, plus één aanroep per open run die buiten het board valt *(eigen keuze na spec-review, r1-f2, r2-f2)* |
| `BoardItem.ItemUpdatedAt` | `updatedAt` van het project-item | `updated_at` van het issue. Die schuift ook bij een comment, dus de volgorde van starts binnen één prioriteit kan verschillen van GitHub (*mijn keuze*: geen extra aanroep per issue) |
| `AddToBoard` | item toevoegen aan het project | niets te doen: elk issue staat al op het board. `IssueItem` geeft op GitLab dus altijd `ok = true` |
| Blokkades | issue-dependencies (`blocked_by`) | issue-links met `link_type = is_blocked_by`, ook naar andere projecten |
| `Blocker.MergedPR` | blijft `false` (REST zegt het niet) | `true` als een MR uit `closed_by` van de blokkeerder gemerged is |
| Milestone | repo-milestone, nummer | project- of groepsmilestone; filter op titel |
| Bevestiging | 🚀-reactie op een comment | award emoji `rocket` op een note |
| Login | `GET /user` → `login` | `GET /user` → `username` |
| Rechtentoets | collaborator-rol | `access_level` van het lid, inclusief geërfd (`/members/all/:id`) |

Statusnamen en prioriteitsnamen komen uit de configuratie (AC van #259). Voor GitLab koppelt
`[forge.board.status_options]` elke lighthouse-statusnaam aan een GitLab-statusnaam in plaats van
een option-ID (bijvoorbeeld `"To do" = "To do"`, `"Controller" = "Controller"`); prioriteiten
worden labelnamen. Het precieze formaat is aan de bouwer, met één eis: de Go-lezer van
`lighthouse.toml` uit #274 valideert per soort forge. De huidige controles (`project_number`,
`project_id`, `status_field_id`, option-ID's) gelden alleen voor GitHub; anders breekt elke hook
op een GitLab-repo. Een nieuwe sleutel (`[forge] token_env`, de GitLab-vorm van `[forge.board]`)
komt ook in `docs/site/data/config.json`. `pack/core/lib/config.py` is na #274 bevroren en krijgt
niets. Daarmee hangt #259 af van #274; #258 niet, want `token_env` en `Host` kunnen in
`internal/repoconfig`.

**Een statuskoppeling die niet bestaat.** Bestaat een gekoppelde GitLab-status niet in de
lifecycle van de bovenste groep, dan geeft `SetStatus` een fout met de naam van de status en de
plek waar je hem toevoegt. Een controle bij het openen van de forge is aan de bouwer.

**Labels aanmaken.** GitLab maakt een projectlabel aan zodra de API het voor het eerst op een
issue zet. De client hoeft `priority::`-labels dus niet vooraf te maken. Labels met kleur en
beschrijving aanmaken bij `init` hoort bij de wrapper (#260), net als het aanmaken voor GitHub via
een script (*mijn keuze*).

### Rechtentoets

`Role` geeft een rolnaam als string, vergeleken met `[dispatch] answer_permissions`. Op GitHub
blijft dat de `role_name` zoals nu, ook een custom role (`internal/forge/people.go:53-70`); zo
blijft de bestaande configuratie geldig. Op GitLab vertaalt de client het `access_level` naar de
woorden hieronder. Omdat `/members/all/:id` een numeriek user-ID neemt, zoekt de client het ID
eerst op bij de gebruikersnaam (`GET /users?username=`), met een cache per tick
*(eigen keuze na spec-review, r1-f4)*.

| GitLab `access_level` | Rol |
|---|---|
| 10 Guest | `read` |
| 15 Planner, 20 Reporter | `triage` |
| 30 Developer | `write` |
| 40 Maintainer | `maintain` |
| 50 Owner | `admin` |
| geen lid | leeg |

### Bevestigingsprotocol

Het protocol blijft hetzelfde: een 🚀 op een vraag- of antwoordcomment betekent "gezien en
verwerkt". `protocol.Comment.Rockets` blijft het aantal. Op GitLab is dat de award emoji
`rocket`, die in GitLab bestaat.

Een risico voor de bouwer: de notes-lijst van GitLab bevat geen award emoji. Het aantal raketten
per note kost dan één extra aanroep per note. Zie *Aan de bouwer*.

### Token

Per soort forge een standaard omgevingsvariabele:

| Kind | Variabelen, in volgorde |
|---|---|
| github | `GH_TOKEN`, `GITHUB_TOKEN` |
| gitlab | `GITLAB_TOKEN` |

Een nieuwe optionele sleutel `[forge] token_env = "NAAM"` kiest één andere variabele. Het token
zelf staat nooit in `lighthouse.toml`. De foutmelding noemt de variabelen die gezocht zijn,
bijvoorbeeld "no GitLab token: set GITLAB_TOKEN". De `gh auth token`-terugval in
`internal/cli/ask.go` blijft alleen voor GitHub.

Eén functie leest het token (nu staat `githubToken` in `serve.go` en wordt het op vijf plekken
herhaald). Die functie krijgt de soort forge en de sleutel mee.

### Tests

- **Bestaande tests:** inhoudelijk gelijk (AC van #258). Een testnabootsing die nu een smalle
  interface invult, neemt `forge.Forge` over door hem als veld in te bedden en vult alleen in wat
  die test gebruikt. Een aanroep van een niet-ingevulde methode geeft dan een panic, dus een test
  die iets onverwachts aanroept, faalt luid. De type-assertions op `*forge.Client` in
  `internal/cli/serve_test.go:395` en `:441` worden mechanisch aangepast.
  Twee soorten wijziging gelden niet als inhoudelijk *(eigen keuze na spec-review, r1-f10)*:
  een test die met zijn code verhuist (`internal/dispatch/board_test.go` met `NewBoard` naar de
  GitHub-client; `SingleSelect` in `internal/overview/overview_test.go:238-250` wordt
  `Status`/`Priority`), en een test die een exacte melding toetst die dit ontwerp verandert (de
  tokenmelding in `internal/cli/triage_brake_test.go:146` en `internal/cli/tick_test.go:185`).
  Het plan noemt elke zo'n test bij naam.
- **Conformiteitssuite:** één gedeelde suite in een pakket naast `internal/forge/forgetest`, naar
  het model van `internal/workplace/workplacetest`. Die draait dezelfde gedragstests tegen de
  GitHub-client (met `forgetest.Server`) en de GitLab-client (met een nieuwe nep-GitLab-server,
  AC van #259). Hij dekt per methode het neutrale resultaat: status lezen en zetten, blokkades,
  bevestigen, rollen, PR-velden, paginering.
- **Compile-check:** `var _ Forge = (*GitHub)(nil)` en idem voor GitLab.
- **Echte server:** de DoD van de milestone (één echt issue tot een MR) draait op de interne
  GitLab. Dat is een handmatige run, geen test in CI.

### Raakvlak met #260

De wrapper `lighthouse forge …` uit #260 wordt een afnemer van `forge.Forge`. Wat de handmatige
keten vraagt en hier nog ontbreekt (een PR/MR aanmaken, issue-labels lezen), voegt #260 toe aan
dezelfde interface. Dat is het voordeel van aanpak A.

## Verdeling over de issues

**#258 levert:**

1. `forge.Forge`, de neutrale types en `forge.Open`.
2. De GitHub-client als implementatie, met de verhuisde vertaling van statusnamen naar ID's.
3. Alle afnemers op `forge.Forge`.
4. Het token per soort forge en `[forge] token_env`; `Host` in `repoconfig`; één forge per repo,
   met overslaan bij een ontbrekend token.
5. De conformiteitssuite, eerst alleen tegen GitHub.
6. `forge.Open` met `kind = "gitlab"` geeft een duidelijke fout.

**#259 levert:**

1. De GitLab-client volgens de tabel hierboven.
2. De nep-GitLab-server en de conformiteitssuite tegen GitLab.
3. De GitLab-vorm van `[forge.board]` en de validatie per soort forge, in de Go-lezer van #274.
4. `serve` toont een GitLab-repo.
5. `lighthouse run` accepteert een pad met subgroepen (`groep/sub/project#5`): de parser in
   `internal/cli/run.go:47` neemt nu precies twee segmenten; hij splitst op de laatste `#`
   *(eigen keuze na spec-review, r1-f8)*.
6. Het AC van #259 zegt nu "status als scoped label `status::<naam>`". Dat wordt de ingebouwde
   issuestatus; het AC wordt aangepast bij de start van #259.

## Aan de bouwer

- De precieze methodenamen en of `Role` een eigen type of een string is.
- Hoe het aantal raketten per note op GitLab efficiënt wordt gelezen: elke note, of alleen notes
  die in de dispatcher een rol spelen. Let op het aantal API-aanroepen per tick.
- Het formaat van statussen en prioriteiten voor GitLab in `[forge.board]`.
- Hoe `BoardItems` op GitLab het venster van gesloten issues afbakent (zie de tabel, *Board*);
  `Prepare` vangt een open run buiten dat venster op.
- De commitvolgorde binnen #258, zolang elke commit groen is.

## Keuzes van de schrijver

- Status via de ingebouwde issuestatus van GitLab: keuze van de eigenaar, omdat dat het logische
  GitLab-begrip is. Het risico (alleen GraphQL, nog *Experiment*, eigen statussen op de bovenste
  groep) staat bij *Randvoorwaarden*.
- Prioriteit als scoped label (`priority::…`): GitLab heeft geen prioriteitsveld, en `weight` is
  een getal zonder namen.
- De typenamen `PR` en `OpenPR` blijven; ze staan voor een PR én een MR.
- De rolwoorden van GitHub worden de neutrale rollen, zodat `answer_permissions` niet verandert.
