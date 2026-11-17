# Ontwerp: de GitHub-forge van de controller op REST

Datum: 2026-11-17 · Issue: [#395](https://github.com/CorneeldH/lighthouse/issues/395)

## Aanleiding en doel

De GraphQL-limiet van GitHub raakt herhaaldelijk op. Dan slaat de tick een repo over
(`forge graphql: API rate limit already exceeded`). Een run kan dan ook zijn handoff niet posten
en de keten staat stil. De weigering komt ook als `gh api rate_limit` nog duizenden punten
GraphQL over geeft. Dat zagen we op 2026-11-16 en weer op 2026-11-17, toen `remaining` op 3010
stond. Het uurbudget is dus niet de enige grens: de punten komen in pieken.

De controller is zelf de grootste verbruiker. Bij elke tick, dus elke 3 minuten, leest de
precheck (`dispatch.Prepare`) het hele board met `BoardQuery`. Voor lighthouse zijn dat
198 items, 2 pagina's van 53 punten, dus ~106 punten per tick en ~2100 per uur. Daar komen de
andere repo, `BranchPRs` per branch, `IssueItem` en de mutaties bij, allemaal in dezelfde minuut.

Via REST kost hetzelfde board 2 requests uit het `core`-budget. Dat budget is 5000 requests per
uur en telt per request, niet per punt. Een conditioneel request dat `304 Not Modified` geeft,
telt helemaal niet. Gemeten op 2026-11-17: `X-Ratelimit-Used` bleef gelijk na twee
304-antwoorden.

**Doel:** de GitHub-forge gebruikt geen GraphQL meer op de paden van de controller (tick,
`lighthouse run`, triage, `boardstart`). De controller leest en schrijft het board, de PR's en de
blokkades via REST, met conditionele requests waar het kan. Een lege GraphQL-limiet laat de
controller dan doorlopen.

Er is één mogelijke uitzondering: `IssueItem`. Geeft de REST-lijst geen gearchiveerde items
(§4), dan blijft die methode op GraphQL. Triage, `boardstart` en `lighthouse forge` gebruiken
`IssueItem`; die blijven dan van GraphQL afhankelijk. De tick roept `IssueItem` alleen aan
voor een open run waarvan het issue niet in de boardlijst staat: het item is gearchiveerd, van
het board gehaald of verwijderd. In die GraphQL-tak doet zo'n tick dus één GraphQL-request per
zulke run. Dat zijn er weinig, en het verschil met nu is een orde van grootte *(r3-f1)*.

Succes:

- Een tick doet geen enkel GraphQL-request. De enige uitzondering is de GraphQL-tak van
  `IssueItem` (§4): één request per open run waarvan het issue niet in de boardlijst staat.
- Een rustige tick, waarin niets veranderde, kost hoogstens een paar `core`-requests: de
  conditionele requests geven 304.
- De dispatcher beslist hetzelfde als nu: dezelfde `BoardItem`'s en `PR`'s, op de verschillen
  na die hieronder staan.

## Doelen, niet-doelen, anti-doelen

**Doelen**

- `BoardItems`, `IssueItem`, `BranchPRs`, `SetStatus` en `AddToBoard` van `*forge.GitHub` op
  REST.
- Een persistente cache voor conditionele GET-requests (ETag, `If-None-Match`), die ook werkt
  over de losse `lighthouse tick`-processen heen.
- De GraphQL-queries van deze methodes verdwijnen. Die van `IssueItem` alleen als hij naar REST
  gaat (§4).

**Niet-doelen**

- De `gh`-aanroepen in skills en sessies (`gh issue comment`, `gh pr list`/`create`/`view`),
  `boardstart` en `worktreeaudit`. Die gebruiken ook GraphQL. Ze horen bij de andere criteria
  van #395 (posten via REST, een spoor bij een mislukte post) en bij #260 (`lighthouse forge`
  als wrapper). Dit ontwerp sluit #395 dus niet; de PR schrijft `Refs #395`. `boardstart` valt
  hier niet onder: dat gaat via `forge.Forge` en verhuist dus mee *(eigen keuze na
  spec-review, r1-f5)*.
- De GitLab-forge. Die gebruikt zijn eigen GraphQL voor het board en blijft zoals hij is.
- `Planning` (`internal/forge/planning.go`) voor het tabblad Prioritering. Dat leest via
  GraphQL, maar alleen als iemand het tabblad opent, niet elke tick. *Keuze van de ontwerper:*
  het blijft op GraphQL, en `graphql.go` blijft dus bestaan. Moet het later ook over, dan wordt
  dat een eigen issue.
- kenn-forge, dat hetzelfde budget gebruikt (#41).
- De tickfrequentie of de precheck-logica veranderen.

**Anti-doelen** (het werkt, maar het is toch mislukt)

- GraphQL als fallback naast REST. Dan bestaan er twee codepaden, die allebei getest en
  onderhouden moeten worden, en het GraphQL-pad roest stil.
- Het `core`-budget leegtrekken. Zonder cache zou een tick per item losse calls doen, zoals
  ~28 blokkade-lookups en checks per open PR. Dat verschuift het probleem alleen. Het verbruik
  van een rustige tick moet bijna nul zijn.
- Een stil verschil in wat de dispatcher ziet, bijvoorbeeld een PR die als `MERGEABLE` +
  `SUCCESS` doorgaat terwijl hij dat niet is, met een onterechte merge als gevolg.

## Randvoorwaarden

- De interface `forge.Forge` en de types `BoardItem`, `PR` en `Blocker` blijven gelijk. De
  dispatcher, triage en het overzicht veranderen niet.
- `BoardItem.ItemID` blijft de node-ID (`PVTI_…`). Hij staat in snapshots en in acties
  (`decide.Action.ItemID`) en gaat naar `SetStatus` via `apply.go`.
- `[forge.board]` in `lighthouse.toml` blijft zoals hij is: `project_number`, `project_id`
  (node-ID), `status_field_id`/`priority_field_id` (node-ID's) en de optie-ID's. Er komt geen
  nieuwe verplichte sleutel bij. Wel komt er één optionele sleutel bij, `project_owner` (§2).
  De optie-ID's zijn in REST dezelfde strings (`5995ef76`).
- `lighthouse tick` is een los proces per tick. Een cache die alleen in het geheugen staat,
  levert daar niets op.
- De fake GitHub-server in `internal/forge/forgetest` en de contracttests in
  `internal/forge/forgesuite` dekken nu GraphQL. Ze moeten de REST-endpoints krijgen.

## Wat REST levert (geverifieerd op 2026-11-17)

`GET users/{owner}/projectsV2/{number}/items?per_page=100&fields=<status>,<priority>,<linked PRs>`
geeft per item:

- `id` (numeriek), `node_id` (`PVTI_…`), `updated_at`, `archived_at`
- de Status en Priority, met het optie-ID als `value.id`
- `content`: het issue zelf. Daarin zitten `number`, `title`, `state`, `updated_at`, `labels`,
  `repository_url` en `issue_dependencies_summary.blocked_by`. Dat laatste is alleen het aantal.
- het veld "Linked pull requests": een lijst PR-objecten met `number`, `state`, `draft`,
  `head.sha`, `head.ref`, `head.repo`, `base.ref`, `base.repo`, `merged_at`, `closed_at` en
  `html_url`. Voor alle
  198 items van dit board is die lijst gelijk aan `closedByPullRequestsReferences(includeClosedPrs:
  true)` uit GraphQL.

Verder:

- `fields` vraagt numerieke veld-ID's (`428493743`), geen node-ID's. Een node-ID geeft 400
  `invalid_ids`. `GET users/{owner}/projectsV2/{number}/fields` geeft per veld zowel `id` als
  `node_id`.
- `GET users/{owner}/projectsV2/{number}` geeft het `node_id` van het project. Daarmee is
  `project_id` te controleren.
- De lijst geeft een ETag; `If-None-Match` geeft 304 zonder dat het budget daalt. **Een 304
  heeft geen `Link`-header.** De 200 van dezelfde pagina wel (gemeten op 2026-11-17).
- `q` filtert met de board-syntaxis (`status:Controller` geeft 8 items). Een issuenummer is
  geen filter: `number:254` geeft 0 items.
- Toevoegen: `POST …/items` met `{"type":"Issue","owner":…,"repo":…,"number":…}`. Daarvoor is
  geen node-ID van het issue nodig.
- Een veld zetten: `PATCH …/items/{item_id}` met
  `{"fields":[{"id":<veld-id>,"value":"<optie-id>"}]}`. `item_id` is numeriek.

**Wat REST niet in de lijst geeft:** `mergeable` en de check-rollup van een PR, en welke issues
een item blokkeren. Daarvoor zijn per stuk losse calls nodig:

- `GET repos/{repo}/pulls/{n}` voor `mergeable`
- `GET repos/{repo}/commits/{sha}/check-runs` en `…/status` voor de checks
- `GET repos/{repo}/issues/{n}/dependencies/blocked_by` voor de blokkades. Dat endpoint
  gebruikt `BlockedBy` al.

## Aanpakken

1. **Alleen de rest naar REST, de controller houdt GraphQL.** Dit is klein. Maar de controller
   is zelf de piek, dus dit lost het niet op. Afgewezen.
2. **De hele GitHub-forge naar REST, met een persistente ETag-cache.** De controller is daarna
   onafhankelijk van GraphQL. De losse calls voor PR's en blokkades kosten bijna niets, omdat ze
   meestal 304 geven. **Gekozen**, door de gebruiker op 2026-11-17.
3. **GraphQL eerst, REST als fallback bij `RATE_LIMIT`.** Dan bestaan er twee codepaden met
   dezelfde semantiek, die allebei getest moeten worden. GraphQL blijft de piek maken. Afgewezen
   (zie de anti-doelen).

## Ontwerp

### 1. De cache voor conditionele requests

Een `http.RoundTripper` in `internal/forge` legt per GET-URL de `ETag`, de body en de
`Link`-header vast. Bij het volgende request stuurt hij `If-None-Match`. Op een 304 geeft hij
de bewaarde body terug als 200, met de bewaarde `Link`. Zonder die `Link` stopt de paginering
van `do` na pagina 1, en dan geeft `BoardItems` stil 100 van de 198 items. Een test met een
304 op pagina 1 van een lijst van twee pagina's legt dat vast *(r1-f1)*.

Hij bewaart alleen een volledig gelezen 200-body. Kan de aanroeper een bewaarde body niet
decoderen, dan verdwijnt die regel uit de cache. Regels die een tijd niet gebruikt zijn, zoals
de checks van oude heads, worden opgeruimd; de termijn kiest de bouwer *(eigen keuze na
spec-review, r1-f10)*.
De cache sleutelt op URL plus token-identiteit. Gebruik een hash van het token, nooit het token
zelf: twee tokens kunnen verschillende dingen zien.

De opslag zit achter een kleine interface (`Get(key)` en `Put(key, etag, body)`).
`forge.Config` krijgt een optioneel veld voor die opslag. Zonder opslag is er geen cache, en het
gedrag is dan gelijk aan nu.

- Elk proces dat een GitHub-forge opent en een state-dir heeft, krijgt een SQLite-opslag: de
  tick, `lighthouse run`, triage, `boardstart`, de server en `lighthouse forge`. De opslag
  staat in een **eigen bestand** in de state-dir, buiten de versie-migraties van de state-DB.
  Een oudere binary weigert de state-DB dan niet na een terugrol (`store.go` `refuseTooNew`)
  *(eigen keuze na spec-review, r1-f11)*. De bouwer zoekt de plekken waar een forge geopend
  wordt (onder meer `internal/cli/forges.go` en `pinslane.go`). Zonder state-dir is er geen
  cache; dat werkt nog steeds, alleen duurder.
- Een cachefout is nooit fataal: openen, lezen en schrijven. Het request gaat dan gewoon
  zonder `If-None-Match`, en de fout komt één keer in de log.

Alleen GET gaat door de cache. Een `Link: next` blijft werken, omdat elke pagina een eigen URL
heeft.

### 2. Het project vinden

De client leidt de REST-coördinaten één keer per proces af en bewaart ze. Die afleiding loopt
via de cache, dus meestal is ze een 304.

- **Eigenaar:** de optionele `[forge.board] project_owner`, en anders de eigenaar van `Repo`
  (`owner/name`). Een board kan van iemand anders zijn dan de repo; GraphQL vond het via de
  node-ID, REST heeft de eigenaar nodig *(r1-f4)*. Eerst `users/{owner}/projectsV2/{n}`, bij
  404 `orgs/{owner}/projectsV2/{n}`. Een 404 op beide geeft een fout die `project_owner`
  noemt.
- **Controle:** het `node_id` van het project moet gelijk zijn aan `project_id`. Anders volgt een
  fout die beide noemt. Zo raakt de client nooit stil het verkeerde board.
- **Velden:** de numerieke ID's van Status en Priority komen uit `…/fields`, door het `node_id`
  te vergelijken met `status_field_id`/`priority_field_id`. Daarbij hoort ook het veld
  "Linked pull requests". Herkenning gaat op het type, niet op de naam (die is
  taalafhankelijk). Of REST dat type geeft, is aan de bouwer om na te gaan.

### 3. `BoardItems`

Eén gepagineerde lijst met de drie velden. Per item:

- `ItemID` = `node_id`, `ItemUpdatedAt` = `updated_at`.
- `Status`, `StatusName` en `Priority` via de bestaande `named()`, met het optie-ID uit
  `value.id`. Een onbekende optie wordt `unknown:<id>`, zoals nu.
- `Repo` uit `repository_url`. Daarnaast `Issue`, `Title`, `IssueUpdatedAt` en de labels.
  `IssueState` wordt in hoofdletters omgezet (`OPEN`/`CLOSED`), zoals GraphQL het gaf.
- Een item zonder issue-content (een draft of een PR) krijgt alleen `ItemID` en de velden, zoals
  nu.
- Een item met `archived_at` laat `BoardItems` altijd weg, ongeacht of REST het levert. GraphQL
  `items` liet het ook weg *(eigen keuze na spec-review, r1-f12)*.
- `ClosingPRs` uit "Linked pull requests", met `Closes: true`. Zie §5 voor de PR-velden.
- `BlockedBy`: alleen voor een item met `issue_dependencies_summary.total_blocked_by > 0` volgt
  `GET …/issues/{n}/dependencies/blocked_by`, via de cache. Het is `total_…` omdat GraphQL ook
  gesloten blokkades gaf; de bouwer gaat na of `blocked_by` alleen de open telt. Per blokkade:
  - `Repo`, `Issue` en `State` (hoofdletters).
  - `MergedPR` is waar als de blokkade zelf op het board staat en een "Linked pull request" met
    `merged_at` heeft.
  - Voor een blokkade buiten het board blijft `MergedPR` onwaar. *Keuze van de ontwerper:* de
    dispatcher behandelt een blokkade als opgelost bij `State != OPEN || MergedPR`, en een
    gemergede sluitende PR sluit het issue meestal binnen seconden. Een extra lookup per
    blokkade buiten het board is dat niet waard.

De invariant blijft: een lijst die niet compleet te lezen is, geeft een fout en geen items. De
oude `truncated`-fouten worden: paginering volgen tot het einde, en een fout als een pagina
faalt.

**Een gedeeld board.** Het board van lighthouse bevat ook een issue van `personal_ai_agent`
(1 van 198). De extra calls van `BlockedBy` en §5 gaan dus ook naar andere repo's. Dat kost
weinig: de tweede repo in dezelfde tick krijgt van dezelfde URL's een 304. *Keuze van de
ontwerper:* een extra call voor een item van een **andere** repo die 403 of 404 geeft, maakt
`BoardItems` niet stuk. Dat item houdt dan lege `BlockedBy`, en zijn PR's krijgen `UNKNOWN` en
`""`. Zo deed GraphQL dat ook met content die het token niet zag. De dispatcher gebruikt van zo'n
item alleen de Status. Voor een item van de eigen repo blijft elke fout een fout.

### 4. `IssueItem`

Er is geen REST-endpoint van "de projectitems van een issue". `IssueItem` zoekt het issue
daarom in de boardlijst van §3. Die lijst komt uit de cache en kost dus meestal niets.

- Het item matcht op `Repo == c.Repo` én `Issue == n`: het board is gedeeld (§3), en een
  ander repo kan hetzelfde nummer hebben *(r1-f8)*.
- Gevonden: `ok = true`, zonder de extra calls van `BlockedBy` en de PR's. Ook nu vult
  `IssueItem` die niet.
- Niet gevonden, maar het issue bestaat (`GET repos/{repo}/issues/{n}` geeft 200): `ok = false`.
- Het issue bestaat niet: `ErrIssueNotFound`, zoals nu. Dat geldt bij een 404, bij een 410
  (een verwijderd issue), bij een 200 met `pull_request` (het nummer is een PR) en bij een 200
  met een andere `repository_url` (een verplaatst issue, via een gevolgde 301). GraphQL gaf in
  die gevallen ook NOT_FOUND *(r1-f8)*.
- **Read-after-write.** `boardstart` zet een status en leest hem meteen terug met `IssueItem`
  (`boardstart.go:116-129`). De bouwer meet of de REST-lijst een `PATCH` meteen toont. Kent de
  client het numerieke item-ID al (uit de tabel van §6), dan leest `IssueItem` dat ene item
  met `GET …/items/{id}`, niet de lijst *(eigen keuze na spec-review, r1-f9)*.

**Gearchiveerde items: eerst verifiëren, vóór de ombouw.** GraphQL `projectItems` gaf ook
gearchiveerde items, met `Archived = true`. Triage heeft dat nodig: een gearchiveerd item mag
niet lezen als "niet op het board", anders beoordeelt triage het issue elke ronde opnieuw en
schrijft het ernaast (`triage_gather.go:74-81`, `SkipArchived` in `triage/facts.go:73-76`).
Of de REST-lijst gearchiveerde items geeft, met `archived_at`, is niet geverifieerd: dit board
heeft er nu geen.

De bouwer verifieert dit als eerste stap, samen met de check-rollup (§5), op een item dat hij
archiveert en daarna terugzet. De uitkomst bepaalt het pad:

- **REST geeft ze:** `IssueItem` vindt ze in de lijst, met `Archived = true`.
- **REST geeft ze niet:** `IssueItem` blijft op GraphQL (`IssueItemsQuery`), als enige
  methode van de vijf. Het is dan het enige pad voor `IssueItem`, geen fallback.

`IssueItem` draait weinig: in de tick alleen voor een open run die niet op het board staat
(op GitHub nooit), verder in triage en `boardstart`. Op GraphQL blijven kost dus vrijwel
niets *(r1-f3)*.

### 5. PR-velden en `BranchPRs`

Een PR uit de REST-lijst wordt op deze manier een `forge.PR`:

- `State`: `MERGED` als `merged_at` gezet is, anders `OPEN` of `CLOSED`.
- `Draft`, `HeadSHA` (`head.sha`), `HeadRef`, `BaseRef` (`base.ref`) en `URL` (`html_url`).
  `MergedAt` en `ClosedAt` staan op nul als ze ontbreken.
- `CrossRepo`: `head.repo.full_name` verschilt van `base.repo.full_name`, of `head.repo` is
  null.
- **De repo van een PR** is `base.repo.full_name`, niet de repo van het item. Een sluitende PR
  kan in een andere repo staan dan het issue. `pulls/{n}` en de checks gaan naar die repo
  *(eigen keuze na spec-review, r1-f7)*.
- **Alleen voor een open PR** worden `Mergeable` en `Checks` opgehaald, via de cache:
  - `Mergeable` uit `GET pulls/{n}`: `true` wordt `MERGEABLE`, `false` wordt `CONFLICTING`,
    `null` wordt `UNKNOWN`.
  - `Checks` uit de checks en de combined status van `head.sha`, samengevoegd zoals GraphQL
    `statusCheckRollup.state` dat doet:
    - geen enkele check of status: `""`
    - een conclusion `failure`, `timed_out`, `cancelled`, `action_required` of
      `startup_failure`, of status `failure`: `FAILURE`
    - status `error`: `ERROR`
    - **`SUCCESS` alleen** als elke check klaar is met `success`, `neutral` of `skipped`, en
      de combined status `success` is of geen statussen heeft
    - **al het andere is `PENDING`**: iets dat nog loopt of nog moet starten, status
      `pending`, `stale`, en elke onbekende waarde. Dat is fail-closed: een nieuwe waarde van
      GitHub mag nooit tot een merge leiden *(r1-f2)*
  - Elke lijst van checks wordt volledig gelezen: `per_page=100`, `Link` volgen, en het aantal
    gelezen runs controleren tegen `total_count`. Een lijst die niet compleet is, geeft een
    fout *(eigen keuze na spec-review, r1-f6)*.
- Een gemergede PR krijgt `Mergeable = "MERGEABLE"`; dat volgt uit de merge zelf. Een gesloten,
  niet-gemergede PR krijgt `"UNKNOWN"`. Beide krijgen `Checks = ""`, zonder extra call. Elke
  plek waar de dispatcher `Checks` leest, eist `State == "OPEN"`. De GitHub-fixture van
  `forgesuite` krijgt daarom `ChecksOnClosedPRs() = false`, net als GitLab. De beweringen van de
  suite blijven gelijk. De vingerafdruk verandert één keer na de uitrol; dat geeft één extra
  volle gather.

**Harde eis: nooit `SUCCESS` zolang er een check moet komen.** Een te vroege merge is geen
onschuldige poging. Weigert GitHub de merge (`MergeRefused`), dan legt `apply.go` dat vast met
`runs.NoteMergeFailed`. Daarna stuurt `decide` een fix-pr-sessie op die head, en die head wordt
nooit meer automatisch gemerged (`decide.go` `mergeReady`, `MergeSHA == HeadSHA`). Een geplande
run eindigt zelfs als `Failed`.

In deze repo is dat venster echt. De meeste jobs in `.github/workflows/ci.yml` hebben
`needs: changes`. Als `changes`, `changelog` en `secret-scan` al groen zijn, hebben de jobs die
erop wachten misschien nog geen check-run. GraphQL gaf dan `PENDING` of `EXPECTED`. Check-runs
plus combined status zouden `SUCCESS` geven.

De rollup moet daarom ook tellen wat nog niet gestart is. Een voor de hand liggende weg is
`GET commits/{sha}/check-suites`: een suite die niet `completed` is, geeft `PENDING`. Let wel op
suites van apps die nooit een run maken. Die blijven op `queued` staan, met
`latest_check_runs_count == 0`, en zouden de PR voor altijd op `PENDING` houden.

De bouwer kiest het mechanisme. Hij bewijst het op een echte PR in deze repo, gemeten op het
moment dat de jobs met `needs` nog wachten, en vergelijkt het met `statusCheckRollup`. De
uitkomst komt in de PR. Lukt het niet om dit gelijk te krijgen aan GraphQL, dan stopt de bouwer
en meldt het, voordat de controller op REST gaat.

`BranchPRs(branch)` wordt `GET repos/{repo}/pulls?head={owner}:{branch}&state=all&per_page=100`,
gepagineerd, met `Closes = false`. Daarna volgt dezelfde verrijking voor open PR's.

### 6. `SetStatus` en `AddToBoard`

- **`AddToBoard(number)`:** `POST …/items` met `type`, `owner`, `repo` en `number`. Het geeft
  een `BoardItem` met `ItemID` = `node_id` van het antwoord. De node-ID-cache van het issue
  (`nodeIDs`, `nodeID()`, `remember()`) is dan niet meer nodig.
- **`SetStatus(item, status)`:** `PATCH …/items/{numeriek id}` met het numerieke veld-ID van
  Status en het optie-ID. Het numerieke item-ID komt uit een tabel `node_id → id` in de client.
  Elke boardlijst en elke `AddToBoard` vult die tabel. Staat het item er niet in, dan leest de
  client de boardlijst één keer (via de cache) en zoekt opnieuw. Ontbreekt het dan nog steeds,
  dan volgt een fout die de `ItemID` noemt.

### 7. Wat verdwijnt

- `BoardQuery`, `BranchPRsQuery`, `AddToBoardMutation`, `SetStatusMutation` en `prNode`.
- `IssueItemsQuery`, `itemNode` en `graphqlErrors.notFound` alleen als `IssueItem` naar REST
  gaat (§4).
- De GraphQL-routes van `forgetest` voor de queries die verdwijnen. De routes voor `Planning`
  blijven, en die voor `IssueItemsQuery` ook als `IssueItem` op GraphQL blijft.

`graphql.go` blijft, voor `Planning`. De bouwer zoekt na of iets buiten `internal/forge` de
geëxporteerde query-constanten gebruikt.

### 8. Fouten en limieten

- `httpStatusError.RateLimited` blijft zoals hij is. Een REST-limiet geeft dezelfde fout als nu,
  en de tick slaat de repo over, net als bij GraphQL.
- Secundaire REST-limiet: maximaal 900 punten per minuut. Een GET kost 1 punt, een write 5. Een
  volle tick zonder cache-treffers kost per repo ongeveer:
  - 2 boardpagina's
  - project en velden
  - ~28 blokkade-lookups
  - 2–3 calls per open PR

  Dat blijft ruim binnen de limiet. Er komt geen eigen throttling.

## Meten (het eerste criterium van #395)

De bouwer meet vóór de ombouw het GraphQL-verbruik van de controller per soort query. Dat kan
door bij elke query `rateLimit { cost }` te loggen, of uit `X-RateLimit-Used` voor en na een
tick. Na de ombouw meet de bouwer het `core`-verbruik van een rustige en van een volle tick. Beide
uitkomsten komen als comment op #395.

## Testen

- `forgetest` krijgt de REST-endpoints van de projecten:
  - project, velden, items (met ETag en 304)
  - POST en PATCH van items
  - `pulls?head=`, `pulls/{n}`, check-runs, status en `dependencies/blocked_by`
- De contracttests in `forgesuite` draaien groen voor GitHub en GitLab. Hun beweringen blijven
  gelijk; alleen `ChecksOnClosedPRs` van de GitHub-fixture gaat naar `false` (§5).
- Een test bewijst dat `BoardItems`, `BranchPRs`, `SetStatus` en `AddToBoard` geen request naar
  `/graphql` doen, en `IssueItem` ook niet als die naar REST gaat (§4). De fake server faalt dan
  op dat pad, maar `Planning` mag het nog wel gebruiken.
- Een test met een 304 op pagina 1 van een lijst van twee pagina's: `BoardItems` geeft alle
  items.
- Tabeltests voor de check-rollup met `stale` en een onbekende waarde: beide geven `PENDING`.
- Een test met een item van een andere repo waarvan de PR 404 geeft: `BoardItems` slaagt, en
  die PR krijgt `UNKNOWN` en `""`.
- Een test bewijst dat een tweede tick, zonder wijziging, alleen 304's krijgt en dezelfde
  `BoardItem`'s geeft (cache over de procesgrens: een nieuwe client met dezelfde opslag).
- Tabeltests voor de omzetting naar `Checks` en `Mergeable`.
- Een test met een node-ID die niet in de tabel staat: `SetStatus` leest de lijst één keer en
  slaagt.
- Een test met een project waarvan het `node_id` niet gelijk is aan `project_id`: er volgt een
  fout.

## Voor de bouwer

- De naam van het cachebestand, zijn schema en de termijn voor het opruimen.
- Hoe het veld "Linked pull requests" herkend wordt als REST geen type geeft. Mogelijk een vaste
  naam als laatste uitweg. Meld het als dat nodig is.
- De volgorde van de commits. Een voorstel:
  1. de verificaties: de check-rollup op een echte PR (§5), gearchiveerde items in de REST-lijst
     (§4) en read-after-write (§4). De check-rollup kan het hele plan stoppen
  2. de cache
  3. het project vinden
  4. `BoardItems`
  5. `IssueItem`
  6. `BranchPRs` en de PR-velden
  7. de mutaties
  8. de GraphQL-queries weg
- De documentatie: `docs/tick.md` noemt GraphQL waar het board gelezen wordt; zet dat bij. Voeg
  een fragment toe aan `changelog.d/`.
