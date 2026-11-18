# De GitHub-forge van de controller op REST (#395) — implementatieplan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `BoardItems`, `IssueItem`, `BranchPRs`, `SetStatus` en `AddToBoard` van `*forge.GitHub` lezen en schrijven via REST, met een persistente ETag-cache, zodat een tick geen GraphQL meer gebruikt.

**Architecture:** Een cachende `http.RoundTripper` in `internal/forge` stuurt `If-None-Match` en maakt van een 304 de bewaarde 200 (body plus `Link`). De opslag is een SQLite-bestand naast de state-DB (`internal/forgecache`). De client leidt per proces de REST-coördinaten van het project af (eigenaar, numerieke veld-ID's) en houdt een tabel `node_id → numeriek item-ID` bij. De fake `forgetest.Server` houdt zijn fixturevelden (`BoardNodes`, `Branches`, in GraphQL-vorm) en serveert ze voortaan als REST, zodat de ~100 fixtures in andere pakketten blijven zoals ze zijn.

**Tech Stack:** Go (stdlib `net/http`, `modernc.org/sqlite`), `gh api` voor de verificaties.

**Spec:** [2026-11-17-forge-github-rest-design.md](../specs/2026-11-17-forge-github-rest-design.md)

## Global Constraints

- `forge.Forge`, `BoardItem`, `PR` en `Blocker` veranderen niet. Dispatcher, triage en overzicht veranderen niet.
- `BoardItem.ItemID` blijft de node-ID (`PVTI_…`).
- `[forge.board]` krijgt alleen de optionele sleutel `project_owner`; geen nieuwe verplichte sleutel.
- Geen GraphQL-fallback naast REST. `Planning` blijft op GraphQL; `graphql.go` blijft.
- Nooit `SUCCESS` zolang er een check moet komen; elke onbekende waarde is `PENDING`.
- Een lijst die niet compleet te lezen is, geeft een fout en geen items.
- Een cachefout is nooit fataal; één regel in de log per proces.
- Code, namen, commentaar en foutmeldingen Engels; plan, commits, PR en comments Nederlands.
- Elke commit groen (`[test_gate] command`), nooit `--no-verify`. `/lighthouse-go-standard` en `/lighthouse-test-standard` gelden.
- De PR schrijft `Refs #395 — <reden>`, niet `Closes`. Eén fragment in `changelog.d/`: het `changelog.d/issue-395-forge-rest.md` dat `/lighthouse-write-pr` in Task 1 schrijft; Task 10 past dat bestand aan.
- Alles wat Task 1 en Task 11 tegen GitHub meten, draait met het token van de controller, niet met de credential van `gh`: neem het uit de `EnvironmentFile` van de tick-unit, niet uit een vast pad (op deze host is dat `~/.config/paia-controller/env`, niet `forge.env`). Zet het in elke shell vooraf:

  ```bash
  ENVF=$(systemctl --user show -p EnvironmentFiles --value lighthouse-tick.service | awk '{print $1}')
  set -a; . "$ENVF"; set +a
  [ -n "$GH_TOKEN" ] || { echo "STOP: no GH_TOKEN from $ENVF"; exit 1; }
  ```

  Onder cron is het bestand het `forge.env` dat de cronregel leest. Dan gebruikt `gh` `GH_TOKEN`. Is `GH_TOKEN` na het inlezen leeg, of geeft een probe met dat token 401/403, dan **STOP en meld het**: zonder dat token meet je de credential van `gh`, niet die van de controller.

## Besluiten van dit plan (de spec laat ze aan de bouwer)

- **Veldherkenning:** het veld "Linked pull requests" heeft in REST `data_type: "linked_pull_requests"` (gemeten op 2026-11-18, project 2). Herkenning gaat op dat type, nooit op de naam.
- **Vorm van een single-select-waarde in REST:** `{"id": "<optie-id>", "name": {"raw": "...", "html": "..."}}`; zonder waarde is `value` `null` (gemeten op 2026-11-18). `StatusName` = `name.raw`.
- **Cachebestand:** `<state>/lighthouse/forge-cache.db`, met `<state>` = `XDG_STATE_HOME` als dat absoluut is, anders `$HOME/.local/state`; naast `lighthouse.db`, maar een eigen bestand zonder migraties. Eén tabel `entries(key TEXT PRIMARY KEY, etag TEXT NOT NULL, link TEXT NOT NULL, body BLOB NOT NULL, used_at INTEGER NOT NULL)`. Opruimen bij `Open`: regels met `used_at` ouder dan **7 dagen**. Kan het bestand niet geopend worden of klopt het schema niet, dan geen cache (en één logregel); het is een cache, geen staat.
- **Cachesleutel:** `GET <volledige URL> <eerste 16 hex-tekens van sha256(Authorization-header)>`.
- **Rollup-mechanisme (kandidaat, Taak 1 bewijst of verwerpt het):** check-runs + combined status + check-suites van `head.sha`. Een suite telt als *wachtend* als `status != "completed"` en (`latest_check_runs_count > 0` of `app.slug == "github-actions"`). Zo houdt een app die nooit een run maakt de PR niet op `PENDING`, maar telt een Actions-suite die nog geen run heeft wel.
- **Volgorde in de rollup:** `""` → `FAILURE` → `ERROR` → `SUCCESS` → anders `PENDING`; zie Taak 6.
- **Gearchiveerde items:** blijkt in Taak 1 dat de gewone lijst ze niet geeft, maar `q=is:archived` wel, dan leest `IssueItem` na een misser ook die tweede lijst (pad A′). Geeft REST ze op geen enkele manier, dan blijft `IssueItem` op GraphQL (pad B). Taak 8 heeft de drie varianten.
- **Fake server:** `BoardNodes` en `Branches` blijven de invoer in GraphQL-vorm; de fake vertaalt ze naar REST-items, PR-objecten, check-runs, statussen en suites. `StatusSets` blijft dezelfde sleutels (`project`, `item`, `field`, `option`) houden, met node-ID's, zodat de tests van `boardstart` en `cli` blijven zoals ze zijn. GraphQL-specifieke knoppen (`FailOps`, `GraphQLErrors`, `BoardNotAProject`, `BoardStuckCursor`, `Count("Board")` enz.) verdwijnen voor de routes die REST worden; tests die ze gebruiken gaan over op `Fail` met een REST-sleutel en op `Count(<REST-pad>)`.
- **Volgorde van de calls:** sequentieel, geen eigen concurrency. Taak 11 meet de duur van een volle tick; boven 10 s wordt dat een eigen issue, niet deze PR.
- **`BranchPRs`** gebruikt `head=<owner>:<branch>`: een PR uit een fork met dezelfde branchnaam telt niet meer mee. De dispatcher vraagt alleen zijn eigen branches; dat is een aanvaard verschil.

## Review Focus

- **Een PR op het moment dat jobs met `needs:` nog geen check-run hebben** geeft `PENDING`, nooit `SUCCESS`. Bewezen op een echte PR in Taak 1; vastgelegd in de rollup-tabeltest van Taak 6 (rij "actions suite in_progress, runs all success").
- **Een gedeeld board met een item uit een repo die het token niet ziet** laat `BoardItems` slagen; dat item houdt lege `BlockedBy` en `UNKNOWN`/`""`. Test in Taak 7.
- **Een tweede tick zonder wijziging** krijgt alleen 304's en dezelfde items, ook met een nieuwe client op dezelfde opslag. Test in Taak 7.
- **Een cache-regel die niet meer decodeert** (bijvoorbeeld na een wijziging van de struct) verdwijnt; de volgende aanroep slaagt met een verse 200. Test in Taak 2.
- **`boardstart` zet een status en leest hem meteen terug**: de terugleesactie ziet de nieuwe status. Test in Taak 9 (fake past de `PATCH` toe; de client leest het ene item met `GET …/items/{id}`).

---

### Task 1: Verificaties en nulmeting

Geen code. De uitkomst gaat in een commitbaar notitiebestand; elke latere taak leest het.

**Files:**
- Create: `docs/developer/planning/plans/2026-11-18-issue-395-verificaties.md`

- [ ] **Step 1: Nulmeting GraphQL.** Meet per querysoort de kosten met `rateLimit { cost }`: plak `BoardQuery` (één pagina), `BranchPRsQuery` en `IssueItemsQuery` uit `internal/forge/board.go` in `gh api graphql -f query='… rateLimit { cost }'` met echte variabelen (`project: PVT_kwHOAGmU3s4Bnmdy`, owner `CorneeldH`, name `lighthouse`). Meet daarnaast 15 minuten lang elke minuut `gh api rate_limit --jq .resources.graphql.used` terwijl de controller draait. Noteer: kosten per query, verbruik per kwartier, aantal ticks in dat kwartier.

- [ ] **Step 2: Gearchiveerde items.** Kies een item met status Done van een gesloten issue. Archiveer het (`gh project item-archive 2 --owner CorneeldH --id <PVTI_…>`), lees dan beide lijsten:

```bash
gh api "users/CorneeldH/projectsV2/2/items?per_page=100" --paginate --jq '.[] | select(.node_id=="<PVTI_…>") | {node_id, archived_at}'
gh api "users/CorneeldH/projectsV2/2/items?per_page=100&q=is:archived" --paginate --jq '.[] | select(.node_id=="<PVTI_…>") | {node_id, archived_at}'
```

Zet het item terug (`gh project item-archive 2 --owner CorneeldH --id <PVTI_…> --undo`) en controleer dat het weer in de gewone lijst staat. Noteer: pad **A** (gewone lijst geeft het, met `archived_at`), **A′** (alleen `q=is:archived`) of **B** (geen van beide).

- [ ] **Step 3: Read-after-write.** Op een Done-item dat op **pagina 1** van de lijst staat (controleer met `--jq '.[].node_id'` op de URL hieronder; kies anders een ander item): lees de lijst met ETag (`gh api -i …/items?per_page=100&fields=428493778`), zet Priority via `gh api -X PATCH users/CorneeldH/projectsV2/2/items/<numeriek id> --input -` met `{"fields":[{"id":428493778,"value":"321d79fc"}]}`, lees direct daarna de lijst met `-H 'If-None-Match: <oude etag>'` en `GET …/items/<id>?fields=428493778`. Zet Priority terug. Noteer per lezing: status (200/304) en of de nieuwe waarde zichtbaar is.

- [ ] **Step 4: Blokkades.** Zoek een issue met `issue_dependencies_summary.total_blocked_by > blocked_by` (dus met een gesloten blokkade) in de lijst. Lees `repos/CorneeldH/lighthouse/issues/<n>/dependencies/blocked_by`. Noteer of gesloten blokkades erin staan.

- [ ] **Step 5: Check-rollup op een echte PR.** CI draait alleen op `pull_request` (`.github/workflows/ci.yml`), dus de meting vraagt een PR. Controleer eerst dat de controller geen open run op #395 heeft (`GET /api/runs` van de server, zie `docs/install.md`); is er een, **STOP en meld het**: de dispatcher neemt de PR van deze branch dan mee in zijn beslissingen. Push deze branch en open een **gewone** PR (geen draft: `lighthouse forge pr create` kent geen `--draft`) via `/lighthouse-write-pr`, met `Refs #395 — werk in uitvoering, zie het plan`. Die skill schrijft ook `changelog.d/issue-395-forge-rest.md`; Task 10 werkt dat bestand bij. Noteer het PR-nummer in het notitiebestand. Push daarna een lege commit (`git commit --allow-empty`). Sample vanaf de push elke 10 s, tot alle checks klaar zijn, voor de nieuwe `head.sha`:

```bash
gh api graphql -f query='query{repository(owner:"CorneeldH",name:"lighthouse"){pullRequest(number:<n>){commits(last:1){nodes{commit{statusCheckRollup{state}}}}}}}' --jq '..|.state?|strings'
gh api "repos/CorneeldH/lighthouse/commits/<sha>/check-runs?per_page=100" --jq '[.check_runs[]|{name,status,conclusion}]'
gh api "repos/CorneeldH/lighthouse/commits/<sha>/status" --jq '{total_count,statuses:[.statuses[].state]}'
gh api "repos/CorneeldH/lighthouse/commits/<sha>/check-suites?per_page=100" --jq '[.check_suites[]|{app:.app.slug,status,conclusion,latest_check_runs_count}]'
```

Pas de kandidaatregel (Besluiten) met de hand toe op elk sample en zet hem naast `statusCheckRollup.state`. De regel leest de losse `statuses[]`, nooit de bovenste `state` van `…/status`: zonder enige status geeft GitHub daar `"pending"` met `total_count: 0` (gezien op de heads van #410–#412). Let vooral op de samples waarin `changes`, `changelog` en `secret-scan` al klaar zijn en de jobs met `needs: changes` nog geen run hebben.

- [ ] **Step 6: Beslis.** Schrijf in het notitiebestand: de tabel van Step 5, de gekozen regel (de kandidaat of een aangepaste), en het pad van Step 2. **STOP en meld het aan de gebruiker** als de regel in één sample `SUCCESS` geeft waar GraphQL `PENDING`/`EXPECTED` gaf: dan gaat de controller niet op REST. **STOP en meld het** ook als de regel in de laatste samples, nadat alle checks klaar zijn, iets anders geeft dan `statusCheckRollup.state` (bijvoorbeeld `PENDING` waar GraphQL `SUCCESS` geeft): dan merget de controller nooit meer. Alleen in samples waarin nog een check loopt of moet starten, mag de regel `PENDING` geven waar GraphQL al `SUCCESS` gaf; dat is fail-closed en gaat met die afwijking in het notitiebestand.

- [ ] **Step 7: Post de nulmeting** als comment op #395, via REST: `gh api repos/CorneeldH/lighthouse/issues/395/comments -F body=@<bestand>`.

- [ ] **Step 8: Commit**

```bash
git add docs/developer/planning/plans/2026-11-18-issue-395-verificaties.md
git commit -m "docs(plan): verificaties en nulmeting van de REST-forge (#395)"
```

---

### Task 2: De cachende transport

**Files:**
- Create: `internal/forge/cache.go`
- Create: `internal/forge/cache_test.go`
- Modify: `internal/forge/forge.go` (`Config`, `Open`)
- Modify: `internal/forge/github.go` (`GitHub` krijgt `cache CacheStore`; `do`)

**Interfaces:**
- Produces:
  - `type CacheEntry struct { ETag, Link string; Body []byte }`
  - `type CacheStore interface { Get(ctx context.Context, key string) (CacheEntry, bool, error); Put(ctx context.Context, key string, e CacheEntry) error; Delete(ctx context.Context, key string) error }`
  - `Config.Cache CacheStore` (nil: geen cache, gedrag als nu) en `Config.CacheLog io.Writer` (nil: weggooien)
  - `func newCachingTransport(next http.RoundTripper, store CacheStore, log io.Writer) http.RoundTripper` (nil `next` is `http.DefaultTransport`)
  - `func cacheKey(req *http.Request) string`
  - `const cacheHitHeader = "X-Lighthouse-Cache"`, waarde `"hit"` op een uit een 304 herbouwd antwoord
  - `func NewMemoryCache() CacheStore` in `cache.go`, voor tests in andere pakketten (een map achter een mutex)

- [ ] **Step 1: Write the failing tests** in `cache_test.go`, tegen een eigen `httptest.Server` die ETags en `If-None-Match` beantwoordt en zijn aanvragen telt:
  - `TestCacheServesStoredBodyOn304`: twee GET's naar dezelfde URL; de tweede draagt `If-None-Match`, de server geeft 304; `do` decodeert dezelfde waarde; `cacheHitHeader` is `"hit"`.
  - `TestCacheKeepsLinkOf304`: een lijst van 2 pagina's; de tweede doorloop krijgt op pagina 1 een 304 **zonder** `Link`; `OpenIssues` geeft beide keren alle items (beide pagina's).
  - `TestCacheOnlyGET`: een POST en PATCH gaan zonder `If-None-Match` en komen niet in de opslag.
  - `TestCacheKeyPerToken`: twee clients met andere tokens op dezelfde opslag; de tweede stuurt geen `If-None-Match`. `cacheKey` bevat het token niet als substring.
  - `TestCacheStoresOnlyComplete200WithETag`: een 200 zonder ETag, een 404 en een 500 komen niet in de opslag.
  - `TestCacheErrorIsNotFatal`: een opslag waarvan `Get` en `Put` falen; de GET slaagt; `CacheLog` bevat precies één regel, ook na drie aanvragen.
  - `TestCacheUndecodableEntryIsDropped`: zet in de opslag een regel met body `"{"` voor de URL; de server geeft 304; `do` faalt met een decodefout; daarna is de regel weg (`Get` geeft `ok == false`); de volgende aanroep stuurt geen `If-None-Match` en slaagt.

- [ ] **Step 2: Run** `go test ./internal/forge -run TestCache -race`. Expected: FAIL (types bestaan niet).

- [ ] **Step 3: Implement.** De transport leest bij een 200 met ETag de hele body (`io.ReadAll`), slaat hem op en geeft een nieuwe body (`bytes.NewReader`) terug. Bij een 304 met bewaarde regel: een nieuw `*http.Response` met status 200, de bewaarde body, `Link` uit de regel en `cacheHitHeader: hit`. Een 304 zonder bewaarde regel gaat ongewijzigd door (geeft in `do` een statusfout). Logregel: `forge cache: <err>`, één keer per transport (`sync.Once`). `Open` wikkelt bij `cfg.Cache != nil` een nieuwe `http.Client{Timeout: base.Timeout, Transport: newCachingTransport(base.Transport, …)}`; de gedeelde client van `forgeOpener` blijft onaangeroerd. `do` roept bij een decodefout op een antwoord met `cacheHitHeader` `c.cache.Delete(ctx, cacheKey(req))` aan (fout daarvan genegeerd) en geeft de decodefout terug.

- [ ] **Step 4: Run** `go test ./internal/forge -race`. Expected: PASS, ook de bestaande tests.

- [ ] **Step 5: Commit** `feat(forge): cache voor conditionele GET-requests (#395)`

---

### Task 3: De SQLite-opslag en het openen per proces

**Files:**
- Create: `internal/forgecache/forgecache.go`
- Create: `internal/forgecache/forgecache_test.go`
- Modify: `internal/cli/forges.go` (`forgeOpener` krijgt `cache forge.CacheStore`, gezet door `newForgeOpener`)
- Modify: `internal/cli/pinslane.go:36`, `internal/prbody/gate.go:82` (zelfde opslag in `forge.Config`)

**Interfaces:**
- Consumes: `forge.CacheStore`, `forge.CacheEntry` (Task 2)
- Produces:
  - `func DefaultPath(getenv func(string) string) string` — `""` als noch `XDG_STATE_HOME` noch `HOME` absoluut is
  - `func Open(ctx context.Context, path string) (*Store, error)` — maakt de map (0o700), de tabel, ruimt op
  - `func OpenDefault(getenv func(string) string, log io.Writer) forge.CacheStore` — nil zonder pad of bij een fout (die één regel naar `log` gaat); nooit een fout
  - `const MaxAge = 7 * 24 * time.Hour`
  - `(*Store).Get/Put/Delete` (voldoet aan `forge.CacheStore`), `(*Store).Close() error`

- [ ] **Step 1: Write the failing tests:**
  - `TestStoreRoundTrip`: `Put` dan `Get` geeft dezelfde `CacheEntry`; `Delete` dan `Get` geeft `ok == false`.
  - `TestStoreSharedBetweenOpens`: `Put` in een eerste `Open`, `Get` in een tweede `Open` van hetzelfde pad vindt de regel (de procesgrens).
  - `TestStorePrunesOldEntries`: een regel met `used_at` 8 dagen terug (via een `now`-veld of directe SQL in de test) is na een nieuwe `Open` weg; een regel van 6 dagen blijft.
  - `TestStoreGetTouches`: `Get` zet `used_at` op nu.
  - `TestStoreWrongSchemaRecreates`: een bestand met een tabel `entries(x)` wordt bij `Open` vervangen; `Put`/`Get` werken daarna.
  - `TestDefaultPath`: tabel — absolute `XDG_STATE_HOME` → `<xdg>/lighthouse/forge-cache.db`; relatieve `XDG_STATE_HOME` en absolute `HOME` → `<home>/.local/state/lighthouse/forge-cache.db`; beide leeg → `""`.
  - In `internal/cli/forges_test.go`: `TestForgeOpenerPassesCache` — een opener met `XDG_STATE_HOME` op `t.TempDir()` geeft `forge.Config.Cache != nil` aan `open` door; zonder absolute `HOME`/`XDG_STATE_HOME` is het nil. `TestForgeOpenerOpensCacheLazily` — `newForgeOpener` zelf maakt geen bestand onder `XDG_STATE_HOME`; pas de eerste `forRepo` voor github doet dat.

- [ ] **Step 2: Run** `go test ./internal/forgecache ./internal/cli -run 'TestStore|TestDefaultPath|TestForgeOpenerPassesCache' -race`. Expected: FAIL.

- [ ] **Step 3: Implement.** DSN zoals `internal/store` (`busy_timeout(5000)`, WAL), maar zonder migraties en zonder `refuseTooNew`. `forgeOpener` opent de opslag lui: de eerste `forRepo` voor github roept `forgecache.OpenDefault(getenv, os.Stderr)` aan, onder een `sync.Once`, zodat een opener die een test met `os.Getenv` bouwt (`newDefaultForgeOpener`, `forgecmd.go:53`) niets in de echte state-dir schrijft; `forRepo` zet `cfg.Cache` en `cfg.CacheLog = os.Stderr` alleen voor kind `""`/`"github"`.

- [ ] **Step 4: Run** `go test ./internal/forgecache ./internal/cli ./internal/prbody -race`. Expected: PASS.

- [ ] **Step 5: Commit** `feat(forgecache): persistente ETag-cache naast de state-DB (#395)`

---

### Task 4: De optionele sleutel `project_owner`

**Files:**
- Modify: `internal/repoconfig/repoconfig.go:64-71` (`Board.ProjectOwner string \`toml:"project_owner"\``)
- Modify: `internal/config/config.go` (`baseKeys` + `"forge.board.project_owner"`; `Board.ProjectOwner`; optionele string bij kind github, verboden bij gitlab zoals `project_number`)
- Modify: `tests/pack/fixtures/config-schema.json` (`base_keys`), en de `expect.json`-fixtures onder `tests/pack/fixtures/config/` waar `Board` als geheel vergeleken wordt; `docs/site/data/config.json` als die de sleutels opsomt
- Modify: `docs/install.md` (bij de `[forge.board]`-sleutels: wanneer `project_owner` nodig is)
- Test: `internal/config/config_test.go`, `internal/repoconfig/repoconfig_test.go`

- [ ] **Step 1: Write the failing tests:** `TestLoadProjectOwner` (repoconfig: `project_owner = "acme"` geeft `Board.ProjectOwner == "acme"`; zonder sleutel `""`); in config: `project_owner` bij kind gitlab geeft een fout met `forge.board.project_owner`; een niet-string geeft een fout met de sleutelnaam; `IsKnownKey("forge.board.project_owner")` is waar.
- [ ] **Step 2: Run** `go test ./internal/config ./internal/repoconfig -race`. Expected: FAIL.
- [ ] **Step 3: Implement** zoals `priority_field_id` (optioneel) gelezen wordt.
- [ ] **Step 4:** Voeg een configfixture onder `tests/pack/fixtures/config/` toe die `forge.board.project_owner` vraagt (`tests/pack/test_config_fixtures.py::test_fixtures_cover_every_key` eist dat). **Run** `go test ./internal/config ./internal/repoconfig -race` en `uv run pytest -q tests/pack -k config tests/test_site_config.py`. Expected: PASS.
- [ ] **Step 5: Commit** `feat(config): optionele [forge.board] project_owner (#395)`

---

### Task 5: Het project vinden

**Files:**
- Create: `internal/forge/project.go`, `internal/forge/project_test.go`
- Create: `internal/forge/forgetest/server_project.go` (project- en veldroutes)
- Modify: `internal/forge/forgetest/server.go` (velden, routes, ETag-middleware)

**Interfaces:**
- Consumes: `repoconfig.Board.ProjectOwner`, `ProjectNumber`, `ProjectID`, `StatusFieldID`, `PriorityFieldID` (Task 4)
- Produces (client):
  - `type project struct { base string; status, priority, linkedPRs int64 }` — `base` is `BaseURL + "/users/<owner>/projectsV2/<n>"` of `…/orgs/…`; `priority` is 0 als `priority_field_id` leeg is
  - `func (c *GitHub) project(ctx context.Context, b *repoconfig.Board) (project, error)` — één keer per client bewaard (onder `c.mu`); een fout wordt niet bewaard
  - `func (p project) itemsURL() string` — `base + "/items?per_page=100&fields=<status>[,<priority>],<linkedPRs>"`
- Produces (fake):
  - `Server.ProjectAtOrg bool` — het project staat onder `/orgs/{owner}/…`; `/users/…` geeft 404
  - `Server.FieldIDs []string` — node-ID's van de single-select-velden; nil is elk `field.id` uit `BoardNodes` plus `DefaultFieldIDs`
  - `var DefaultFieldIDs []string` — de status- en priority-veld-ID's die de tests van deze repo gebruiken (verzamel met `grep -rn "StatusFieldID:\|PriorityFieldID:\|\"SF\"" --include=*_test.go internal`)
  - Numerieke ID's: project `1`; velden `100 + index` in de gesorteerde lijst; het veld `Linked pull requests` is `99` met `data_type: "linked_pull_requests"`
  - Elke GET-route geeft een `ETag` (sha256 van de body) en beantwoordt een gelijke `If-None-Match` met 304 zonder body en zonder `Link`
  - `func (s *Server) NotModified(key string) int` — hoeveel GET's op `key` (pad zoals `Count`) een 304 kregen

- [ ] **Step 1: Write the failing tests** in `project_test.go` (tegen `forgetest`):
  - `TestProjectUserThenOrg`: `ProjectAtOrg = true`; `project` slaagt, met `base` op `/orgs/o/projectsV2/1`; `Count("/users/o/projectsV2/1") == 1` en `Count("/orgs/o/projectsV2/1") == 1`.
  - `TestProjectNotFoundNamesOwnerKey`: beide 404 → fout bevat `project_owner`.
  - `TestProjectOwnerOverridesRepoOwner`: `ProjectOwner: "acme"` → `Count("/users/acme/projectsV2/1") == 1`.
  - `TestProjectNodeIDMismatch`: `srv.ProjectID = "PVT_other"`, board `PVT_1` → fout bevat `PVT_other` en `PVT_1`.
  - `TestProjectFieldIDs`: `status`, `priority` en `linkedPRs` zijn de numerieke ID's van de fake; zonder `priority_field_id` is `priority == 0` en bevat `itemsURL` geen extra komma.
  - `TestProjectUnknownStatusField`: status-veld niet in `FieldIDs` → fout noemt `status_field_id`. Zelfde voor een gezet `priority_field_id`.
  - `TestProjectWithoutNumber`: `ProjectNumber == 0` → fout noemt `project_number`, zonder request.
  - `TestProjectResolvedOnce`: twee aanroepen → één request per route.
  - In `forgetest/server_test.go`: `TestServerETag304` — tweede GET met de ETag geeft 304 zonder `Link`; `NotModified(<pad>) == 1`.
- [ ] **Step 2: Run** `go test ./internal/forge/... -run 'TestProject|TestServerETag' -race`. Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** `go test ./internal/forge/... -race`. Expected: PASS.
- [ ] **Step 5: Commit** `feat(forge): REST-coördinaten van het project (#395)`

---

### Task 6: PR-velden, check-rollup en `BranchPRs` op REST

**Files:**
- Create: `internal/forge/pr.go`, `internal/forge/pr_test.go`
- Modify: `internal/forge/board.go` (`BranchPRs`)
- Modify: `internal/forge/forgetest/server.go` (`pulls?head=`, `pulls/{n}` uit `Branches`/`BoardNodes`, check-routes)
- Modify: `internal/forge/github_contract_test.go` (`ChecksOnClosedPRs() = false`, commentaar bijwerken)
- Modify: `internal/dispatch/gather_test.go:314,586` (`Count("BranchPRs")` → `Count("/repos/o/r/pulls?head=<branch>")`)

**Interfaces:**
- Consumes: de regel uit het notitiebestand van Task 1
- Produces:
  - `type restPull struct` — `number`, `state`, `draft`, `merged_at`, `closed_at`, `html_url`, `head{sha, ref, repo{full_name}}` (repo kan null zijn), `base{ref, repo{full_name}}`
  - `func (p restPull) pr(closes bool) PR` — zonder calls; `MERGED` als `merged_at` gezet is, anders `OPEN`/`CLOSED`; `CrossRepo` = head.repo null of ongelijk aan base.repo; gemerged → `Mergeable "MERGEABLE"`; gesloten → `"UNKNOWN"`; open → `""` (wordt verrijkt)
  - `func (p restPull) repo() string` — `base.repo.full_name`
  - `func (c *GitHub) enrich(ctx context.Context, repo string, pr *PR) error` — alleen bij `State == "OPEN"`: `Mergeable` en `Checks`
  - `func mergeable(m *bool) string` — `true` `MERGEABLE`, `false` `CONFLICTING`, nil `UNKNOWN`
  - `type checkRun struct{ Status, Conclusion string }`, `type commitStatus struct{ State string }`, `type checkSuite struct{ Status string; Runs int; App string }`
  - `func rollup(runs []checkRun, statuses []commitStatus, suites []checkSuite) string`
  - `func (c *GitHub) checks(ctx context.Context, repo, sha string) (string, error)` — leest de drie lijsten volledig (`per_page=100`, `Link` volgen, aantal tegen `total_count`; te weinig is een fout)
- Fake: `pulls?head=o:<branch>&state=all` serveert `Branches[<branch>]` vertaald, en telt ook `Count("/repos/{o}/{n}/pulls?head=<branch>")`. `pulls/{n}` zoekt eerst in `Pulls`, dan in de PR-nodes van `Branches` en `BoardNodes`. `statusCheckRollup.state` per `headRefOid` wordt: geen → geen runs, geen statussen, geen suites; `SUCCESS` → één run `completed/success` + suite `completed`; `FAILURE` → run `completed/failure`; `ERROR` → status `error`; `PENDING`/`EXPECTED` → run `in_progress` + suite `in_progress` met 1 run. `GET …/commits/{sha}/status` zonder statussen geeft, zoals GitHub, `{"state":"pending","total_count":0,"statuses":[]}`; `checks` leest alleen `statuses[]` en `total_count`, nooit de bovenste `state`.

- [ ] **Step 1: Write the failing tests** in `pr_test.go`:

```go
func TestRollup(t *testing.T) {
	ok := checkRun{"completed", "success"}
	actions := func(status string, runs int) checkSuite { return checkSuite{status, runs, "github-actions"} }
	cases := []struct {
		name     string
		runs     []checkRun
		statuses []commitStatus
		suites   []checkSuite
		want     string
	}{
		{"nothing", nil, nil, nil, ""},
		{"all success", []checkRun{ok, {"completed", "neutral"}, {"completed", "skipped"}}, nil, []checkSuite{actions("completed", 3)}, "SUCCESS"},
		{"one failure", []checkRun{ok, {"completed", "failure"}}, nil, nil, "FAILURE"},
		{"timed out", []checkRun{{"completed", "timed_out"}}, nil, nil, "FAILURE"},
		{"cancelled", []checkRun{{"completed", "cancelled"}}, nil, nil, "FAILURE"},
		{"action required", []checkRun{{"completed", "action_required"}}, nil, nil, "FAILURE"},
		{"startup failure", []checkRun{{"completed", "startup_failure"}}, nil, nil, "FAILURE"},
		{"status failure", []checkRun{ok}, []commitStatus{{"failure"}}, nil, "FAILURE"},
		{"status error", []checkRun{ok}, []commitStatus{{"error"}}, nil, "ERROR"},
		{"run in progress", []checkRun{ok, {"in_progress", ""}}, nil, nil, "PENDING"},
		{"run queued", []checkRun{{"queued", ""}}, nil, nil, "PENDING"},
		{"stale", []checkRun{{"completed", "stale"}}, nil, nil, "PENDING"},
		{"unknown conclusion", []checkRun{{"completed", "brand_new"}}, nil, nil, "PENDING"},
		{"status pending", []checkRun{ok}, []commitStatus{{"pending"}}, nil, "PENDING"},
		{"actions suite in_progress, runs all success", []checkRun{ok}, nil, []checkSuite{actions("in_progress", 1)}, "PENDING"},
		{"actions suite queued without runs", []checkRun{ok}, nil, []checkSuite{actions("queued", 0)}, "PENDING"},
		{"other app queued without runs", []checkRun{ok}, nil, []checkSuite{{"queued", 0, "some-app"}}, "SUCCESS"},
	}
	// for each: rollup(tc.runs, tc.statuses, tc.suites) == tc.want
}
```

  Pas de tabel aan als Task 1 een andere regel koos; de rijen `stale`, `unknown conclusion` en `actions suite in_progress, runs all success` blijven `PENDING`.
  - `TestRestPullToPR`: tabel over open, gesloten, gemerged, draft, head.repo null (`CrossRepo`), head.repo andere naam (`CrossRepo`); `MergedAt`/`ClosedAt` nul als null.
  - `TestMergeable`: `true`/`false`/nil.
  - `TestChecksIncompleteListFails`: tabel over de drie lijsten — check-runs, statussen en suites — elk met `total_count: 3` en 2 regels, waarbij de ontbrekende regel een `failure` (run, status) of een `in_progress` Actions-suite is → fout, nooit `SUCCESS`.
  - `TestChecksFollowPages`: per lijst 150 regels over twee pagina's, één `failure` op pagina 2 → `FAILURE`.
  - `TestChecksNoStatusesIsSuccess`: groene runs, een `completed` suite en `/status` met `state: "pending"`, `total_count: 0` → `SUCCESS`.
  - `TestBranchPRsREST`: één open (`statusCheckRollup SUCCESS`, `mergeable CONFLICTING`) en één gemergede PR op een branch → twee PR's, `Closes == false`; open: `Checks == "SUCCESS"`, `Mergeable == "CONFLICTING"`; gemerged: `Checks == ""`, `Mergeable == "MERGEABLE"`, zonder request naar zijn checks (`Count("/repos/o/r/commits/<sha>/check-runs") == 0`).
  - `TestBranchPRsEscapesBranch`: branch `fix/a b` → de fake krijgt `head=o:fix/a b` gedecodeerd.
  - `TestBranchPRsFollowsPages`: 150 PR's op één branch → 150 PR's.
- [ ] **Step 2: Run** `go test ./internal/forge/... -run 'TestRollup|TestRestPull|TestMergeable|TestChecks|TestBranchPRs' -race`. Expected: FAIL.
- [ ] **Step 3: Implement.** Faalconclusies: `failure`, `timed_out`, `cancelled`, `action_required`, `startup_failure`. Succesconclusies: `success`, `neutral`, `skipped`. Oude `TestBranchPRs*` in `board_test.go` en de GraphQL-route `BranchPRs` van de fake gaan weg.
- [ ] **Step 4: Run** `go test ./internal/forge/... ./internal/dispatch/... ./internal/cli/... -race`. Expected: PASS (de forgesuite-case `PRFields` slaagt met `ChecksOnClosedPRs() == false`). Eén verwachte, door de spec (§5) bedoelde verandering in de beweringen: de gesloten PR 29 van `internal/cli/gather_test.go:56-59` krijgt `Checks ""` en `Mergeable "UNKNOWN"`. Pas daarom `internal/dispatch/testdata/snapshot.json` aan met `go test ./internal/cli -run TestGatherCommandMatchesFixture -update`, en controleer dat de diff alleen die twee velden van PR 29 raakt. Elke andere diff is een fout in de code, geen fixture om bij te werken.
- [ ] **Step 5: Commit** `feat(forge): BranchPRs en PR-checks via REST (#395)`

---

### Task 7: `BoardItems` op REST

**Files:**
- Modify: `internal/forge/board.go` (`BoardItems`, `boardItems`; GraphQL-pad weg), `internal/forge/issues.go` (`BlockedBy` → `blockedBy(ctx, repo, number)`, `BlockedBy` roept het aan met `c.Repo`)
- Modify: `internal/forge/project.go` (`restItem`, `listItems`, de tabel)
- Modify: `internal/forge/forgetest/server_project.go` (`GET …/items`, `…/items/{id}`; `dependencies/blocked_by` afgeleid uit `BoardNodes`)
- Modify: `internal/forge/board_test.go`, `internal/forge/forge_test.go` (GraphQL-tests van `BoardItems` herschrijven naar REST)
- Modify: de testboards zonder `ProjectNumber` (`ProjectNumber: 1`) in `internal/forge`, `internal/dispatch`, `internal/cli`, `internal/boardstart`; en `srv.ProjectID` gelijk aan de `project_id` van het testboard waar een test dat nog niet zet (de controle van Task 5)
- Modify: `internal/cli/tick_test.go:331,345,461,1784`, `internal/cli/run_test.go:168` (`Count("Board")` → `Count("/users/o/projectsV2/<n>/items")`; `GraphQLErrors`/`FailOps["Board"]` → `Fail["GET /users/o/projectsV2/<n>/items"] = 502`). `<n>` is het `project_number` van het testboard: `3` in `tick_test.go` (`completeRepo`), anders `1`
- Modify: `internal/forge/forgetest/server.go`: `FailPage map[string]int` (sleutel `"GET <pad>?page=<p>"`), omdat `Fail`/`FailFirst` op pad zonder query sleutelen en pagina 2 dus niet raken
- Modify (door de spec bedoeld, §3): `internal/dispatch/gather_test.go:147-176` (`TestPrepareFiltersBoard`) en `internal/cli/gather_test.go:45-49` + `internal/dispatch/testdata/snapshot.json:51`: de blokkade #5 staat niet op het board, dus `MergedPR`/`merged_pr` wordt `false`

**Interfaces:**
- Consumes: `project`, `itemsURL` (Task 5); `restPull.pr`, `enrich` (Task 6)
- Produces:
  - `type restItem struct` — `id` (int64), `node_id`, `updated_at`, `archived_at` (*time.Time), `content` (*restContent), `fields` ([]restField)
  - `type restContent struct` — `number`, `title`, `state`, `updated_at`, `repository_url`, `labels[].name`, `pull_request` (json.RawMessage), `issue_dependencies_summary{blocked_by, total_blocked_by}`
  - `func (c *GitHub) listItems(ctx context.Context, p project) ([]restItem, error)` — alle pagina's, archived inbegrepen zoals REST ze geeft; vult de tabel
  - `func (r restItem) boardItem(p project, b *repoconfig.Board) (BoardItem, []restPull)` — zonder calls: `ItemID`, `ItemUpdatedAt`, velden via `named()`, `Repo` (via `repoFromURL`), `Issue`, `Title`, `IssueState` in hoofdletters, `IssueUpdatedAt`, `Labels`, `Archived`; geen content of `pull_request` gezet → alleen `ItemID`, tijd en velden
  - De tabel op `GitHub`: `itemIDs map[string]int64` (node-ID → numeriek) en `issueItems map[issueKey]int64` (`issueKey{repo string; number int}`), onder `c.mu`
  - `func (c *GitHub) blockedBy(ctx context.Context, repo string, number int) ([]Blocker, error)`

- [ ] **Step 1: Write the failing tests** (in `board_test.go`, tegen `forgetest`):
  - `TestBoardItemsREST`: één item met Status, Priority, labels, een open sluitende PR (`SUCCESS`, `MERGEABLE`) en een gemergede; verwacht de volledige `BoardItem` met `ClosingPRs` (`Closes: true`) en `IssueState "OPEN"`.
  - `TestBoardItemsUnknownOption`: optie die `status_options` niet kent → `Status == "unknown:<id>"`, `StatusName` = de naam.
  - `TestBoardItemsNonIssueContent`: draft-item zonder content → alleen `ItemID` en velden.
  - `TestBoardItemsSkipsArchived`: een node met `isArchived: true` (fake zet `archived_at`) staat niet in het resultaat.
  - `TestBoardItemsFollowsPages`: 150 nodes → 150 items, `Count("/users/o/projectsV2/1/items") == 2`.
  - `TestBoardItemsLaterPageFails`: `FailPage["GET /users/o/projectsV2/1/items?page=2"] = 502` → fout en nil, en pagina 1 is wel gelezen (`Count` van de lijst is 2).
  - `TestBoardItemsBlockers`: item met twee blokkades, één op het board met een gemergede linked PR (`MergedPR == true`), één buiten het board (`MergedPR == false`); `State` in hoofdletters; een item met `total_blocked_by == 0` doet geen request naar `…/dependencies/blocked_by`.
  - `TestBoardItemsOtherRepoNotVisible`: een item van `x/y` waarvan `pulls/{n}` en `blocked_by` 404 geven → `BoardItems` slaagt, dat item heeft lege `BlockedBy` en PR `Mergeable "UNKNOWN"`, `Checks ""`. Zelfde opzet met het item in `o/r` → fout.
  - `TestBoardItems304FirstOfTwoPages`: `Config.Cache = forge.NewMemoryCache()`, 150 nodes; twee keer `BoardItems` → beide keren 150 items; de tweede doorloop kreeg op beide pagina's 304 (fake telt 304's: `Server.NotModified(key)`).
  - `TestBoardItemsSecondClientSameStore`: twee clients met dezelfde `MemoryCache`, geen wijziging → de tweede krijgt alleen 304's en `reflect.DeepEqual` op de items.
- [ ] **Step 2: Run** `go test ./internal/forge/... -run TestBoardItems -race`. Expected: FAIL.
- [ ] **Step 3: Implement.** `BoardItems` = `project` → `listItems` → index `issueKey → linked PR's` over alle items (ook archived) voor `MergedPR` → per niet-archived item `boardItem`, `blockedBy` (alleen bij `total_blocked_by > 0`), `pr(true)` + `enrich` per linked PR. Een 403/404 op een extra call wordt alleen genegeerd als het item een andere repo dan `c.Repo` heeft. De fake vertaalt een node naar een REST-item: `id = 5000 + index`, `node_id = node["id"]`, velden met `{"id", "name": {"raw", "html"}}`, het linked-PR-veld uit `closedByPullRequestsReferences.nodes`, `repository_url = <srv>/repos/<nameWithOwner>`, `issue_dependencies_summary` uit `blockedBy.nodes`. `GET …/dependencies/blocked_by` serveert `BlockedBy[n]` als die gezet is, anders de vertaalde `blockedBy.nodes` van de node met dat nummer in die repo.
- [ ] **Step 4: Run** `go test ./... -race`. Expected: PASS. Faalt een test in `dispatch`/`cli`/`boardstart`, pas dan alleen de fixture-mechaniek aan (pad, `Fail`, `ProjectNumber`, `srv.ProjectID`), nooit een bewering over het gedrag van de dispatcher. De enige uitzondering is de `MergedPR` van de blokkade buiten het board (Files); werk `snapshot.json` bij met `go test ./internal/cli -run TestGatherCommandMatchesFixture -update` en controleer dat de diff alleen `merged_pr` van #5 raakt.
- [ ] **Step 5: Commit** `feat(forge): BoardItems via REST met ETag-cache (#395)`

---

### Task 8: `IssueItem`

Het pad komt uit het notitiebestand van Task 1.

**Files:**
- Modify: `internal/forge/board.go` (`IssueItem`), `internal/forge/board_test.go`, `internal/forge/forgetest/server_project.go`
- Modify: `internal/dispatch/gather_test.go:751` (`Count("IssueItems")` → het REST-pad), `internal/boardstart/boardstart_test.go`, `internal/cli/triage_test.go` waar ze `GraphQLErrors`/`FailOps` voor `IssueItems` gebruiken

**Interfaces:**
- Consumes: `listItems`, `boardItem`, de tabel (Task 7)
- Produces: `func (c *GitHub) issueItemByID(ctx context.Context, p project, id int64) (restItem, error)` — `GET <base>/items/{id}?fields=…`

**Pad A / A′ (REST):**

- [ ] **Step 1: Write the failing tests:**
  - `TestIssueItemFromList`: item van `o/r#7` op het board → `ok`, `Status`, `Priority`, `StatusName`; geen request naar `…/dependencies/blocked_by` of `…/pulls/…`.
  - `TestIssueItemOtherRepoSameNumber`: alleen `x/y#7` op het board, `o/r#7` bestaat → `ok == false`.
  - `TestIssueItemArchived`: archived node van `#7` → `ok`, `Archived == true` (A′: via de tweede lijst met `q=is:archived`).
  - `TestIssueItemNotOnBoard`: `Issues[7]` bestaat, geen node → `ok == false`, geen fout.
  - `TestIssueItemNotFound`: tabel — 404, 410, een 200 met `pull_request`, een 200 met een andere `repository_url` → `errors.Is(err, ErrIssueNotFound)`.
  - `TestIssueItemUsesKnownItemID`: na `BoardItems` leest `IssueItem(7)` alleen `GET …/items/<id>` (`Count` van de lijst blijft gelijk).
- [ ] **Step 2: Run** `go test ./internal/forge/... -run TestIssueItem -race`. Expected: FAIL.
- [ ] **Step 3: Implement.** Volgorde: tabel kent `(c.Repo, n)` → `issueItemByID`; anders `listItems` (A′: daarna de lijst met `q=is:archived`), zoek op `Repo == c.Repo && Issue == n`; niet gevonden → `GET repos/{repo}/issues/{n}` voor de vier niet-bestaan-gevallen. Een 404 op `issueItemByID` (item weg van het board) valt terug op de lijst. Verwijder `IssueItemsQuery`, `graphqlErrors.notFound` en de fake-route `IssueItems`. `itemNode` en `fieldValuesSelection` blijven tot Task 10: `BoardQuery` gebruikt ze nog.
- [ ] **Step 4: Run** `go test ./... -race`. Expected: PASS.
- [ ] **Step 5: Commit** `feat(forge): IssueItem via de REST-boardlijst (#395)`

**Pad B (GraphQL blijft):**

- [ ] **Step 1:** Laat `IssueItem`, `IssueItemsQuery`, `itemNode`, `fieldValuesSelection`, `notFound` en de fake-route `IssueItems` staan. Voeg in `board.go` bij `IssueItem` een commentaar toe dat REST geen gearchiveerde items geeft (met verwijzing naar het notitiebestand).
- [ ] **Step 2: Run** `go test ./internal/forge/... -race`. Expected: PASS.
- [ ] **Step 3: Commit** `docs(forge): IssueItem blijft op GraphQL, REST geeft geen gearchiveerde items (#395)`

---

### Task 9: `SetStatus` en `AddToBoard` op REST

**Files:**
- Modify: `internal/forge/board.go` (`SetStatus`, `AddToBoard`; `setOption`, `addToBoard` weg)
- Modify: `internal/forge/github.go`, `internal/forge/write_issue.go` (`nodeIDs`, `remember`, `nodeID` weg)
- Modify: `internal/forge/forgetest/server_project.go` (`POST …/items`, `PATCH …/items/{id}`), `internal/forge/forgetest/server_write.go` indien de schrijfroutes daar horen
- Modify: `internal/forge/forge_test.go` (de drie `TestAddToBoard…NodeID`-tests weg; nieuwe tests hieronder), `internal/cli/tick_test.go:1258-1281` (`FailOps["SetStatus"]` → `Fail["PATCH /users/o/projectsV2/3/items/<id>"]`: `completeRepo` heeft `project_number = 3`)

**Interfaces:**
- Consumes: `project`, de tabel, `listItems` (Task 7)
- Produces: `func (c *GitHub) itemNumber(ctx context.Context, p project, nodeID string) (int64, error)` — uit de tabel; anders één `listItems` en opnieuw; anders een fout die `nodeID` noemt
- Fake: `PATCH …/items/{id}` met `{"fields":[{"id":<veld>,"value":"<optie>"}]}` voegt aan `StatusSets` `{"project": ProjectID, "item": <node-ID>, "field": <veld-node-ID>, "option": <optie>}` toe en past de `fieldValues` van de node aan; een mislukte (`Fail`) staat er ook in maar verandert niets. `POST …/items` met `{"type":"Issue","owner","repo","number"}` voegt node `"item-" + Issues[n]["node_id"]` toe en geeft `{"id", "node_id"}`.

- [ ] **Step 1: Write the failing tests:**
  - `TestSetStatusREST`: na `BoardItems`, `SetStatus(item, "Controller")` → één `PATCH` met numeriek veld-ID en optie-ID; `StatusSets[0]["item"] == item.ItemID`.
  - `TestSetStatusUnknownNodeReadsListOnce`: zonder voorafgaande lijst → precies één lijstlezing, dan de `PATCH`.
  - `TestSetStatusMissingNodeFails`: node-ID staat ook na de lijst niet in de tabel → fout bevat de node-ID, geen `PATCH`.
  - `TestSetStatusUnknownName` en `TestSetStatusWithoutItemID` blijven: fout vóór elk request.
  - `TestAddToBoardREST`: `POST` met `type`, `owner`, `repo`, `number`; geen GET van het issue; daarna `SetStatus` op het teruggegeven item doet geen lijstlezing.
  - `TestSetStatusThenIssueItemReadsBack` (Review Focus): `IssueItem` → `SetStatus("In progress")` → `IssueItem` geeft `Status == "In progress"`.
- [ ] **Step 2: Run** `go test ./internal/forge/... -run 'TestSetStatus|TestAddToBoard' -race`. Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** `go test ./... -race`. Expected: PASS (ook `boardstart` en de forgesuite-case `AddToBoardThenSetStatus`).
- [ ] **Step 5: Commit** `feat(forge): SetStatus en AddToBoard via REST (#395)`

---

### Task 10: GraphQL weg van de controllerpaden, docs en changelog

**Files:**
- Modify: `internal/forge/board.go` (`BoardQuery`, `BranchPRsQuery`, `AddToBoardMutation`, `SetStatusMutation`, `prFields`; in pad A ook `itemNode`, `fieldValuesSelection`, `prNode` en `repoRef`)
- Modify: `internal/forge/forgetest/server.go` (GraphQL-routes `Board`, `BranchPRs`, `AddToBoard`, `SetStatus`; velden `BoardNotAProject`, `BoardStuckCursor`; in pad A ook `FailOps`; documentatie van `BoardNodes`/`Branches`: "invoer in GraphQL-vorm, geserveerd als REST")
- Modify: `docs/tick.md` (de plekken die GraphQL noemen bij het lezen van het board)
- Modify: `changelog.d/issue-395-forge-rest.md` (het fragment dat `/lighthouse-write-pr` in Task 1 schreef; geen tweede fragment)

**Pad B:** `IssueItem` blijft op GraphQL. Dan blijven `IssueItemsQuery`, `itemNode`, `fieldValuesSelection`, `prNode`, `repoRef` (`itemNode` gebruikt ze, `board.go:241-265`), de fake-route `IssueItems` en `FailOps` (de foutentests van `IssueItem` gebruiken het) staan. `golangci-lint` beslist wat verder ongebruikt is.
- Test: `internal/forge/board_test.go`

- [ ] **Step 1: Write the failing test** `TestControllerPathsUseNoGraphQL`: één client tegen `forgetest` met een item, een open PR en een branch-PR; roep `BoardItems`, `BranchPRs`, `SetStatus`, `AddToBoard` (en in pad A `IssueItem`) aan; daarna `srv.Count("/graphql") == 0`. Daarna `Planning` → `Count("/graphql") > 0` (Planning mag het nog).
- [ ] **Step 2: Run** `go test ./internal/forge -run TestControllerPathsUseNoGraphQL -race`. Expected: PASS al na Task 9; bevestig dat hij faalt als je in `BranchPRs` tijdelijk `c.graphql` aanroept (mutatiecheck van `/lighthouse-test-standard`), en draai die wijziging terug.
- [ ] **Step 3: Verwijder** de queries, types en fake-routes hierboven. Zoek eerst `grep -rn "forge\.\(BoardQuery\|BranchPRsQuery\|IssueItemsQuery\|AddToBoardMutation\|SetStatusMutation\)" .` (was leeg op 2026-11-18). `golangci-lint run ./...` wijst wat verder ongebruikt is.
- [ ] **Step 4:** `docs/tick.md` bijwerken: het board, de PR's en de blokkades komen via REST met conditionele requests; alleen `Planning` (tabblad Prioritering) en, in pad B, `IssueItem` gebruiken nog GraphQL. Vervang de inhoud van `changelog.d/issue-395-forge-rest.md` door:

```markdown
---
type: changed
---
The controller reads and writes the GitHub board, pull requests and blockers through REST with conditional requests, so an exhausted GraphQL limit no longer stops a tick.
```

- [ ] **Step 5: Run** `go test ./... -race && golangci-lint run ./...`. Expected: PASS, geen bevindingen.
- [ ] **Step 6: Commit** `refactor(forge): GraphQL-queries van de controller weg (#395)`

---

### Task 11: Meting na de ombouw en de PR

- [ ] **Step 1:** De geïnstalleerde `lighthouse` (`~/go/bin`) blijft onaangeroerd; nooit `go install` van deze branch. Bouw de branch naar een eigen pad: `go build -o "$HOME/.cache/lighthouse-395/lighthouse" ./cmd/lighthouse`. Stop de geplande tick (`systemctl --user stop lighthouse-tick.timer`; onder cron: zet de regel uit), zet het token van de controller (Global Constraints) en `XDG_RUNTIME_DIR` zoals de unit, en draai twee ticks met de hand met dat pad. Meet `core.used` (`gh api rate_limit --jq .resources.core.used`) vóór en na een **volle** tick (eerste tick, lege cache: verwijder `forge-cache.db` vooraf) en een **rustige** tick (de tick erna, zonder wijziging). Meet ook de duur van beide ticks en `graphql.used` over dezelfde periode (verwacht: 0, behalve pad B of het Prioritering-tabblad). Start daarna de timer weer (`systemctl --user start lighthouse-tick.timer`), controleer met `systemctl --user is-active lighthouse-tick.timer` dat hij loopt, en verwijder `$HOME/.cache/lighthouse-395/`.
- [ ] **Step 2:** Post de meting als comment op #395 via `gh api repos/CorneeldH/lighthouse/issues/395/comments -F body=@<bestand>`. Duurt een volle tick langer dan 10 s, schrijf dan met `/lighthouse-write-issue` een issue voor concurrency.
- [ ] **Step 3:** De PR uit Task 1 bestaat al; maak geen tweede. Schrijf de body volgens `/lighthouse-write-pr` (`Refs #395 — <reden>`, met de Evidence-tabel, de rollup-tabel van Task 1 en de metingen) naar een bestand, maar sla de stap `lighthouse forge pr create` van die skill over. Zet de body via REST: `gh api -X PATCH repos/CorneeldH/lighthouse/pulls/<n> -F body=@<bestand>`, en controleer met `gh api repos/CorneeldH/lighthouse/pulls/<n> --jq .body | head -3` dat de `Refs #395`-regel erin staat.

---

## Self-review

- **Spec-dekking:** §1 → Task 2, 3; §2 → Task 4, 5; §3 → Task 7; §4 → Task 1 (Step 2, 3), Task 8; §5 → Task 1 (Step 5), Task 6; §6 → Task 9; §7 → Task 8, 10; §8 → ongewijzigd (`httpStatusError.RateLimited` blijft, geen throttling); Meten → Task 1 (Step 1, 7), Task 11; Testen → de tests van Task 2, 5–10; docs en changelog → Task 10.
- **Open na review r3-f1** (een tick doet in pad B één GraphQL-request per open run van een item dat niet meer op het board staat): Task 11 meet `graphql.used`; in pad B noemt de PR dat getal.
