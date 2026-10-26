# Open vragen en teruggegeven issues uit de forge — implementatieplan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Doel:** `GET /api/questions` en `GET /api/returned` geven de open vragen en de teruggegeven issues van een instelbare GitHub-repo, alleen lezend uit de forge.

**Architectuur:** Drie nieuwe pakketten, elk met één verantwoordelijkheid. `internal/protocol` leest de kop van een `🤖`-comment (de Go-tegenhanger van `parse_header` in `protocol.py`) en deelt een issue in zoals `staat_van` in paia. `internal/forge` is een GitHub-client die alleen GET doet en alle pagina's volgt. `internal/overview` haalt de open issues en hun comments op en bouwt beide lijsten. `internal/server` krijgt de twee routes; `lighthouse serve` krijgt `--repo` en leest het token uit de omgeving.

**Tech stack:** Go 1.26, Huma v2, BurntSushi/toml (al in `go.mod`), stdlib `net/http`. Python-kant: pytest.

**Spec:** issue #67 (de acceptatiecriteria zijn de spec). Bron van de indeling: `staat_van`, `_laatste`, `_vraagtekst` en `_onder_de_rij` in `src/controller/overzicht.py`, en `is_antwoord` in `src/controller/comments.py`, in CorneeldH/personal_ai_agent. Bron van de kop: `ROBOT`, `KINDS`, `_LEGACY_KEYS`, `_LEGACY_VALUES`, `_FIELD`, `_kind_of` en `parse_header` in [pack/core/lib/protocol.py](../../../../pack/core/lib/protocol.py).

**Begeleidende skills:** `/lighthouse-go-writing` (classificatiezin vóór de eerste edit, KPI-tabel bij de oplevering) en `/lighthouse-testing` (welke productiewijziging laat elke test falen).

## Globale eisen

- De taalvarianten komen uit `core/headings.toml` in `pack.Files` (de embed van [pack/pack.go](../../../../pack/pack.go)), sleutels `kind_<soort>`. Geen met de hand overgetikte tabel in Go.
- Soorten, letterlijk: `handoff`, `started`, `question`, `resumed`, `done`, `returned`, `rebased`.
- Een kop: de eerste regel van de getrimde body begint met `🤖 ` (robot plus spatie). De rest, getrimd, begint met een variant; daarna volgt niets, witruimte of `·`. Langste variant eerst.
- Velden: de regels na de kopregel tot de eerste lege regel; elke getrimde regel die op `^([a-z_]+):\s?(.*)$` past, met de waarde getrimd. Oude sleutels en waarden worden vertaald zoals `_LEGACY_KEYS` en `_LEGACY_VALUES` (alle 14 sleutels, alle 3 waarden).
- Antwoord (= `is_antwoord`): getrimde body niet leeg, en begint niet met `🤖`, `<!-- lighthouse:` of `<!-- paia:`.
- De forge-client doet alleen GET. Basis-URL `https://api.github.com`; headers `Authorization: Bearer <token>`, `Accept: application/vnd.github+json`, `X-GitHub-Api-Version: 2022-11-28`; `per_page=100`; de volgende pagina komt uit de `Link`-header (`rel="next"`). HTTP-timeout 30 s.
- Token: `GH_TOKEN`, anders `GITHUB_TOKEN`. Repo: `--repo owner/name`, standaard uit `LIGHTHOUSE_FORGE_REPO`.
- Zonder repo of token: HTTP 503. Forge onbereikbaar of een niet-2xx-antwoord: HTTP 502. Nooit een lege lijst bij een fout, ook geen halve.
- Geen nieuwe dependency (geen `golang.org/x/sync`, geen GitHub-SDK).
- Code, commentaar en foutteksten in het Engels; dit plan, commits en PR in het Nederlands.
- Niets onder `pack/` verandert, dus geen `init --upgrade`.

## Review Focus

1. **Comment met `\r\n`.** Een body die in de GitHub-UI met Windows-regeleinden is geplakt moet dezelfde soort en velden geven als met `\n`; Python's `splitlines` splitst er al op. Fixture-geval in taak 1.
2. **Pull requests in de issuelijst.** De REST-lijst `issues?state=open` geeft ook PR's (met een sleutel `pull_request`). Een PR met een `🤖 Vraag` hoort niet in `/api/questions`. Test in taak 3.
3. **Fout halverwege het ophalen.** Faalt de comments-call van één issue van de vijftig, dan geeft het endpoint 502, geen lijst met 49. Test in taak 4.
4. **Beantwoord met een raket.** Een mens-comment met een 🚀-reactie telt in `staat_van` niet als antwoord (`c.rockets == 0`): de vraag blijft open. Test in taak 2.
5. **Herbaseerd na de vraag.** `🤖 Herbaseerd` is een statusnotitie en wordt overgeslagen; een Vraag gevolgd door Herbaseerd blijft een open vraag. Test in taak 2.

## Bestanden

| Bestand | Verantwoordelijkheid |
|---|---|
| `internal/protocol/header.go` | `ParseHeader`: soort en velden van een `🤖`-comment |
| `internal/protocol/status.go` | `Comment`, `Classify`, `IsAnswer`, `QuestionText` |
| `internal/protocol/header_test.go`, `status_test.go` | tests, waaronder de gedeelde fixture |
| `tests/pack/fixtures/protocol/headers.json` | gedeelde fixture: body → soort (en velden) |
| `tests/pack/test_protocol.py` | Python-kant van de gedeelde fixture |
| `internal/forge/github.go` | `Client`, `Issue`, `OpenIssues`, `Comments`, `ErrNotConfigured` |
| `internal/forge/github_test.go` | nep-transport: alleen GET, paginering, fouten |
| `internal/overview/overview.go` | `Source`, `Build`, `Question`, `Returned` |
| `internal/overview/overview_test.go` | indeling over meerdere issues, volgorde, fout halverwege |
| `internal/server/server.go`, `internal/server/forge.go` | routes en foutvertaling |
| `internal/cli/serve.go` | `--repo`, token, de client doorgeven |
| `docs/install.md`, `deploy/systemd/lighthouse.service` | repo en token instellen |

---

### Taak 1: Kop-parser met gedeelde fixture

**Files:**
- Create: `internal/protocol/header.go`, `internal/protocol/header_test.go`
- Create: `tests/pack/fixtures/protocol/headers.json`
- Modify: `tests/pack/test_protocol.py` (één nieuwe test onderaan)

**Interfaces:**
- Produces:
  - `type Kind string` met constanten `KindHandoff`, `KindStarted`, `KindQuestion`, `KindResumed`, `KindDone`, `KindReturned`, `KindRebased` (waarden zoals in de globale eisen).
  - `type Header struct { Kind Kind; Fields map[string]string }`
  - `func ParseHeader(body string) (Header, bool)` — `false` als de body geen protocolkop heeft.
  - `const Robot = "🤖"`

- [ ] **Stap 1: Schrijf de fixture.** Een JSON-array van `{"name": string, "body": string, "kind": string|null, "fields": object (optioneel)}`. Minstens deze gevallen:

  | name | body | kind | fields |
  |---|---|---|---|
  | `nl-question` | `🤖 Vraag · fase plan` | `question` | |
  | `en-question` | `🤖 Question` | `question` | |
  | per soort nl en en | `🤖 <variant>` | de soort | |
  | `returned-legacy-key` | `🤖 Teruggegeven\nreden: geen worktree` | `returned` | `{"reason": "geen worktree"}` |
  | `returned-en` | `🤖 Returned\nreason: x\nsession: <uuid>\n\nvrije tekst: y` | `returned` | `{"reason": "x", "session": "<uuid>"}` |
  | `legacy-value` | `🤖 Handoff\nwanneer: nacht` | `handoff` | `{"when": "night"}` |
  | `crlf` | `🤖 Teruggegeven\r\nreden: x\r\n` | `returned` | `{"reason": "x"}` |
  | `tab-after-kind` | `🤖 Klaar\tpr` | `done` | |
  | `prefix-only` | `🤖 Klaarheid` | `null` | |
  | `no-space` | `🤖Vraag` | `null` | |
  | `not-first-line` | `tekst\n🤖 Vraag` | `null` | |
  | `leading-blank` | `\n  \n🤖 Gestart` | `started` | |
  | `human` | `ja, doe 1a` | `null` | |

- [ ] **Stap 2: Python-test** `test_shared_header_fixture` in `tests/pack/test_protocol.py`: laad de fixture via `Path(__file__).parent / "fixtures/protocol/headers.json"`; per geval `header = parse_header(body)`; bij `kind` null `header is None`, anders `header.kind == kind`, en per sleutel in `fields` `header.fields[key] == value`. Draai `uv run pytest tests/pack/test_protocol.py -q -k shared_header_fixture`. Verwacht: PASS — de fixture beschrijft het bestaande Python-gedrag. Faalt een geval, dan klopt de fixture niet; pas de fixture aan, nooit `protocol.py`.

- [ ] **Stap 3: Go-test** `TestParseHeaderSharedFixture` in `header_test.go`: leest `../../tests/pack/fixtures/protocol/headers.json` en controleert per geval (`t.Run(name)`) dezelfde asserties. Plus `TestVariantsFromHeadings`: elke soort heeft minstens de varianten `nl` en `en` uit `core/headings.toml` (bijv. `question` → `Vraag` en `Question`), zodat een lege tabel opvalt. Draai `go test ./internal/protocol/ -run 'ParseHeader|Variants'`. Verwacht: FAIL (pakket bestaat niet).

- [ ] **Stap 4: Implementeer `ParseHeader` in `header.go`.** De varianten laadt een `sync.OnceValue` uit `pack.Files` (`core/headings.toml`, decode in `map[string]map[string]string`, alleen sleutels `kind_<soort>`); een onleesbaar embedded bestand is een programmeerfout: `panic`. Splits op `\n` na `strings.ReplaceAll(body, "\r\n", "\n")`. Witruimte na de variant: `unicode.IsSpace`. De legacy-tabellen als twee package-level maps, letterlijk overgenomen uit `protocol.py`.

- [ ] **Stap 5:** Draai `go test ./internal/protocol/ -run 'ParseHeader|Variants'` en de Python-test uit stap 2. Verwacht: beide PASS. Mutatiecheck (lighthouse-testing): haal de grensregel na de variant weg en zie `prefix-only` falen. De langste-eerst-sortering heeft geen mutatiecheck: in `core/headings.toml` is geen variant een voorvoegsel van een andere, dus weghalen verandert geen uitkomst.

- [ ] **Stap 6: Commit** — `feat(protocol): Go-parser van de 🤖-kop met gedeelde fixture`

### Taak 2: Indeling zoals `staat_van`

**Files:**
- Create: `internal/protocol/status.go`, `internal/protocol/status_test.go`

**Interfaces:**
- Consumes: `ParseHeader`, `Header`, `Kind*` uit taak 1.
- Produces:
  - `type Comment struct { Body, CreatedAt, URL string; Rockets int }`
  - `type State int` met `StateNone` (geen `🤖`-kop), `StateQuestion`, `StateReturned`, `StateOther`.
  - `type Status struct { State State; Comment Comment; Header Header }` — `Comment` en `Header` van de laatste kop die telt.
  - `func Classify(comments []Comment) Status` — `comments` in de volgorde van de forge (oudste eerst).
  - `func IsAnswer(body string) bool`
  - `func QuestionText(body string) string`

- [ ] **Stap 1: Tests.** `TestClassify`, tabelgedreven; elke rij een lijst bodies (rockets 0 tenzij genoemd) en de verwachte `State`:

  | geval | comments | State |
  |---|---|---|
  | geen kop | `hallo` | `StateNone` |
  | vraag zonder antwoord | `🤖 Vraag` | `StateQuestion` |
  | vraag met antwoord | `🤖 Vraag`, `1a` | `StateOther` |
  | antwoord vóór de vraag | `1a`, `🤖 Vraag` | `StateQuestion` |
  | antwoord met raket | `🤖 Vraag`, `1a` (rockets 1) | `StateQuestion` |
  | artefact na de vraag | `🤖 Vraag`, `<!-- lighthouse:plan -->…` | `StateQuestion` |
  | herbaseerd na de vraag | `🤖 Vraag`, `🤖 Herbaseerd` | `StateQuestion` |
  | teruggegeven | `🤖 Teruggegeven\nreden: x` | `StateReturned`, `Header.Fields["reason"] == "x"` |
  | teruggegeven, dan gestart | `🤖 Teruggegeven`, `🤖 Gestart` | `StateOther` |
  | onleesbare kop na de vraag | `🤖 Vraag`, `🤖 Onbekend` | `StateQuestion` (de onleesbare kop is geen antwoord en geen kop) |

  Bij `StateQuestion` en `StateReturned` is `Status.Comment.Body` de body van de vraag- of teruggegeven-comment.

  `TestQuestionText`: kopregel en `<sub>`-regels weg, lege regels buiten het codeblok weg, regels buiten het codeblok getrimd, binnen het codeblok letterlijk (= `_vraagtekst` zonder de inspringing van twee spaties). Invoer en verwachte uitvoer:

  ~~~~text
  🤖 Vraag · fase plan

    **Vraag:** Welke?

  - **a)** dit

  **Zelf draaien:**
  ```bash
    echo  x

  ```

  <sub>session …</sub>
  ~~~~

  ~~~~text
  **Vraag:** Welke?
  - **a)** dit
  **Zelf draaien:**
  ```bash
    echo  x

  ```
  ~~~~

  Plus een tweede geval in `TestQuestionText`: dezelfde invoer met `\r\n` als regeleinde geeft dezelfde uitvoer (`_vraagtekst` splitst met `splitlines`).

- [ ] **Stap 2:** `go test ./internal/protocol/ -run 'Classify|QuestionText'`. Verwacht: FAIL (ongedefinieerd).

- [ ] **Stap 3: Implementeer.** `Classify` volgt `_laatste` + `staat_van`: loop over de comments; een geparste kop met soort `rebased` sla je over; een andere geparste kop wordt de laatste en leegt `after`; een comment zonder geparste kop gaat in `after` als er al een laatste is. Daarna: vraag → `StateQuestion` tenzij een comment in `after` `Rockets == 0 && IsAnswer(body)` heeft; teruggegeven → `StateReturned`; anders `StateOther`. `QuestionText` normaliseert eerst `\r\n` naar `\n`, net als `ParseHeader`, en gebruikt het fence-patroon `^(`{3,})bash$` van `FENCE_OPEN`; de sluitregel is de fence zelf.

- [ ] **Stap 4:** `go test ./internal/protocol/`. Verwacht: PASS. Mutatiecheck: laat `Rockets == 0` weg en zie "antwoord met raket" falen; laat de `rebased`-skip weg en zie "herbaseerd na de vraag" falen.

- [ ] **Stap 5: Commit** — `feat(protocol): indeling open vraag en teruggegeven volgens staat_van`

### Taak 3: GitHub-client, alleen GET

**Files:**
- Create: `internal/forge/github.go`, `internal/forge/github_test.go`

**Interfaces:**
- Consumes: `protocol.Comment` uit taak 2.
- Produces:
  - `var ErrNotConfigured = errors.New("forge not configured")`
  - `type Issue struct { Number int; Title, URL string; Comments int }` — `URL` is `html_url`, `Comments` het aantal uit de lijst.
  - `type Client struct { Repo, Token, BaseURL string; HTTP *http.Client }`
  - `func New(repo, token string) *Client` — `BaseURL` `https://api.github.com`, `HTTP` met timeout 30 s.
  - `func (c *Client) OpenIssues(ctx context.Context) ([]Issue, error)` — `GET /repos/<repo>/issues?state=open&per_page=100`, alle pagina's, zonder PR's.
  - `func (c *Client) Comments(ctx context.Context, number int) ([]protocol.Comment, error)` — `GET /repos/<repo>/issues/<n>/comments?per_page=100`, alle pagina's; `Rockets` uit `reactions.rocket`, `URL` uit `html_url`.

- [ ] **Stap 1: Tests met een nep-transport.** Een `fakeTransport` (`http.RoundTripper`) met een map URL → antwoord (status, body, `Link`), die bij elke methode anders dan GET `t.Errorf("method %s: forge client must only GET", m)` aanroept en een fout teruggeeft, en elk verzoek telt.
  - `TestOpenIssuesFollowsPages`: pagina 1 met 100 issues en `Link: <…page=2>; rel="next"`, pagina 2 met 1 issue en 1 PR (`"pull_request": {}`) → 101 issues, geen PR.
  - `TestCommentsFollowsPages`: idem, 100 + 1 comments; de laatste heeft `reactions.rocket = 2` → `Rockets == 2`.
  - `TestRequestHeaders`: `Authorization`, `Accept` en `X-GitHub-Api-Version` zoals in de globale eisen.
  - `TestNotConfigured`: `New("", "t")` en `New("o/r", "")` → `errors.Is(err, ErrNotConfigured)` voor beide methoden, en nul verzoeken.
  - `TestForgeErrors`: status 500, status 401, onleesbare JSON en een transportfout geven elk een fout en `nil` als slice.
  - `TestOnlyGET`: roep elke geëxporteerde methode aan tegen het nep-transport; de test faalt op elke andere methode (dat is het criterium uit de spec).

- [ ] **Stap 2:** `go test ./internal/forge/`. Verwacht: FAIL (pakket bestaat niet).

- [ ] **Stap 3: Implementeer.** Eén ongeëxporteerde `get(ctx, url string, into any) (next string, err error)` die als enige een verzoek maakt, met `http.MethodGet`; beide methoden lussen tot `next` leeg is. `Link` parsen met de stdlib (`strings.Split` op `,`, zoek `rel="next"`). Een fout noemt de URL zonder token en de status.

- [ ] **Stap 4:** `go test -race ./internal/forge/`. Verwacht: PASS. Mutatiecheck: volg `next` niet en zie de 101-tests falen; zet in `get` `http.MethodPost` en zie `TestOnlyGET` falen.

- [ ] **Stap 5: Commit** — `feat(forge): GitHub-client die alleen leest en alle pagina's volgt`

### Taak 4: Overzicht over alle open issues

**Files:**
- Create: `internal/overview/overview.go`, `internal/overview/overview_test.go`

**Interfaces:**
- Consumes: `forge.Issue`, `protocol.Comment`, `protocol.Classify`, `protocol.QuestionText` uit taak 2 en 3.
- Produces:
  - `type Source interface { OpenIssues(ctx context.Context) ([]forge.Issue, error); Comments(ctx context.Context, number int) ([]protocol.Comment, error) }` — `*forge.Client` voldoet.
  - `type Question struct { Number int \`json:"number"\`; Title string \`json:"title"\`; URL string \`json:"url"\`; Question string \`json:"question"\`; AnswerURL string \`json:"answer_url"\` }` — `AnswerURL` is de URL van de vraag-comment, anders die van het issue (zoals `Antwoord op` in paia).
  - `type Returned struct { Number int \`json:"number"\`; Title string \`json:"title"\`; URL string \`json:"url"\`; Reason string \`json:"reason"\` }` — `Reason` is veld `reason`, leeg als het ontbreekt.
  - `type Overview struct { Questions []Question; Returned []Returned }` — nooit `nil`-slices.
  - `func Build(ctx context.Context, src Source) (Overview, error)`

- [ ] **Stap 1: Tests met een nep-`Source`.**
  - `TestBuild`: vier issues — open vraag, beantwoorde vraag, teruggegeven, zonder comments (`Comments: 0`) — geeft één `Question` (met `QuestionText` van de vraag) en één `Returned` (met reden); voor het issue met `Comments: 0` wordt `Comments` niet aangeroepen.
  - `TestBuildOrder`: twee open vragen; die met de oudste `CreatedAt` van de vraag-comment komt eerst, bij gelijke tijd het laagste nummer (`open_vragen` in paia). Zelfde regel voor `Returned`.
  - `TestBuildFailsWhole`: vijftig issues, de comments-call van één faalt → fout, en een leeg `Overview{}` (geen halve lijst).
  - `TestBuildListFails`: `OpenIssues` faalt → fout, met `errors.Is` op de oorspronkelijke fout.
  - `TestBuildEmptyIsNotNil`: geen issues → `Questions` en `Returned` zijn lege, niet-`nil` slices.

- [ ] **Stap 2:** `go test ./internal/overview/`. Verwacht: FAIL.

- [ ] **Stap 3: Implementeer `Build`.** Haal de comments op voor elk issue met `Comments > 0`, met hoogstens 8 tegelijk (een kanaal als semafoor plus `sync.WaitGroup.Go`); bewaar de eerste fout en annuleer de rest via een afgeleide context. Resultaten in een slice per index, zodat de race-detector niets ziet; sorteer daarna.

- [ ] **Stap 4:** `go test -race ./internal/overview/`. Verwacht: PASS. Mutatiecheck: negeer de fout van één comments-call en zie `TestBuildFailsWhole` falen.

- [ ] **Stap 5: Commit** — `feat(overview): open vragen en teruggegeven issues uit de forge bouwen`

### Taak 5: Endpoints in de server

**Files:**
- Create: `internal/server/forge.go`, `internal/server/forge_test.go`
- Modify: `internal/server/server.go:22` (signatuur `New`), `internal/server/server_test.go`, `internal/server/host_test.go` (aanroepen van `New`)
- Modify: `internal/cli/serve.go:67` (de enige productie-aanroep van `New`; tijdelijk `server.New(web.FS(), forge.New("", ""))`, zodat de test-gate bij de commit compileert; taak 6 vervangt dit)

**Interfaces:**
- Consumes: `overview.Source`, `overview.Build`, `overview.Question`, `overview.Returned`, `forge.ErrNotConfigured`, `forge.New` (alleen in `serve.go`).
- Produces: `func New(site fs.FS, src overview.Source) http.Handler`. Routes `GET /api/questions` (operation `get-questions`, body `{"questions": [...]}`) en `GET /api/returned` (operation `get-returned`, body `{"returned": [...]}`).

- [ ] **Stap 1: Tests** in `forge_test.go`, met de `get`-helper uit `server_test.go` en een nep-`Source`:
  - `TestQuestionsEndpoint` en `TestReturnedEndpoint`: 200 en de velden `number`, `title`, `url` plus `question`/`answer_url` of `reason` in de JSON.
  - `TestEndpointNotConfigured`: `Source` geeft een fout met `forge.ErrNotConfigured` → 503, en de body bevat geen sleutel `questions`/`returned`.
  - `TestEndpointForgeDown`: een andere fout → 502, idem geen lijst.
  - `TestOpenAPI` uitbreiden: `paths` bevat `/api/questions` en `/api/returned`.
  - Bestaande aanroepen `New(site)` worden `New(site, stubSource{})`, met `stubSource` in `server_test.go` die lege lijsten geeft.

- [ ] **Stap 2:** `go test ./internal/server/`. Verwacht: FAIL (compileert niet: `New` heeft één parameter).

- [ ] **Stap 3: Implementeer.** `New` registreert de twee routes via een functie in `forge.go`; elke handler roept `overview.Build` aan. Foutvertaling in één helper: `errors.Is(err, forge.ErrNotConfigured)` → `huma.Error503ServiceUnavailable(err.Error())`, anders `huma.Error502BadGateway(err.Error())`.

- [ ] **Stap 4:** Pas `internal/cli/serve.go:67` aan zoals onder *Files*. Draai `go build ./... && go vet ./... && go test -race ./internal/server/ ./internal/cli/`. Verwacht: PASS. Mutatiecheck: geef bij een fout `200` met een lege lijst en zie beide fouttests falen.

- [ ] **Stap 5: Commit** — `feat(server): GET /api/questions en /api/returned`

### Taak 6: `serve` instelbaar, documentatie

**Files:**
- Modify: `internal/cli/serve.go`, `internal/cli/serve_test.go`, `e2e/serve_test.go` (aanroep van `serve`, als die de signatuur raakt)
- Modify: `docs/install.md`, `deploy/systemd/lighthouse.service`

**Interfaces:**
- Consumes: `forge.New`, `overview.Source`, `server.New` uit taak 3 en 5.
- Produces:
  - `func serve(ctx context.Context, addr, dbPath string, src overview.Source, errOut io.Writer) error`
  - `func githubToken(getenv func(string) string) string` — `GH_TOKEN`, anders `GITHUB_TOKEN`, anders leeg.
  - Flag `--repo` (standaard `os.Getenv("LIGHTHOUSE_FORGE_REPO")`), helptekst noemt `owner/name`.

- [ ] **Stap 1: Tests.**
  - `TestGithubToken`: beide gezet → `GH_TOKEN`; alleen `GITHUB_TOKEN` → die; geen → leeg.
  - `TestServeDefaults` uitbreiden: `serve --help` bevat `--repo`.
  - `TestServeForgeNotConfigured`: start `serve` zoals `TestServeStopsOnCancel`, met `forge.New("", "")`; `GET /api/questions` op de echte listener geeft 503.
  - Bestaande `serve(...)`-aanroepen krijgen `forge.New("", "")`.

- [ ] **Stap 2:** `go test ./internal/cli/`. Verwacht: FAIL.

- [ ] **Stap 3: Implementeer.** `RunE` bouwt `forge.New(repo, githubToken(os.Getenv))` en geeft hem aan `serve`, die hem aan `server.New` doorgeeft.

- [ ] **Stap 4: Documentatie.** In `docs/install.md` een sectie *Connect the forge*: `--repo owner/name` of `LIGHTHOUSE_FORGE_REPO`, token in `GH_TOKEN`, een fijnmazig token met alleen *Issues: read*; de check `curl -s http://127.0.0.1:8127/api/questions`; 503 = niet ingesteld, 502 = forge onbereikbaar. In de unit: `EnvironmentFile=-%h/.config/lighthouse/forge.env` met een commentaarregel waarom (geheim buiten de unit, mode 0600; het streepje laat de server zonder bestand starten). In het Engels, zoals de rest van `docs/install.md`.

- [ ] **Stap 5:** de volledige test-gate `uv run pytest -q && go test -race -shuffle=on ./...`, plus `go test -race -shuffle=on -tags e2e ./e2e/` en `golangci-lint run ./...`. Verwacht: alles groen, nul bevindingen.

- [ ] **Stap 6: Commit** — `feat(serve): repo en token voor de forge instellen`

### Taak 7: Bewijs tegen CorneeldH/personal_ai_agent

Geen code; dit bewijs gaat in de PR (laatste acceptatiecriterium).

- [ ] **Stap 1:** Start op de achtergrond (de server stopt niet vanzelf): `GH_TOKEN=$(gh auth token) go run ./cmd/lighthouse serve --repo CorneeldH/personal_ai_agent --addr 127.0.0.1:8128 --db <scratchpad>/l.db`. `gh` buiten de sandbox; een leeg token geeft in stap 2 een 503 en daarmee een falende `jq -e`.
- [ ] **Stap 2:** Zo dicht mogelijk na elkaar, met begin- en eindtijd per opname: `curl -s --fail-with-body 127.0.0.1:8128/api/questions | jq -e '.questions | type == "array"' >/dev/null` en daarna dezelfde `curl … | jq` voor de uitvoer; idem voor `/api/returned` met `.returned`. Faalt `curl` of `jq -e`, dan is er geen bewijs: stop en meld de fout, plak nooit een foutantwoord. Daarna in de paia-checkout het overzicht (`scripts/controller_gather.py bundle --zonder-usage` voor een verse bundel, dan `scripts/controller_state.py overzicht --input <bundle>`; zie de USAGE van beide scripts).
- [ ] **Stap 3:** Zet de drie uitvoeren naast elkaar in de PR-body, met de tijden. Elk verschil krijgt één regel uitleg. Te verwachten: paia toont alleen issues uit zijn bundel (board en controller), lighthouse elk open issue met een `🤖`-kop; meer issues bij lighthouse is dan geen fout, een ontbrekend issue wel.
