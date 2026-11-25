# Ontwerp: telemetrie in de UI — wat voegt elk onderdeel toe, en wanneer

Issue: #379. Milestone: *Telemetrie: wat voegt elk onderdeel toe*. Ontwerp van de milestone:
[`2026-11-16-zelflerende-lighthouse-design.md`](2026-11-16-zelflerende-lighthouse-design.md) (T6).
Gegevensmodel: [`2026-11-19-telemetrie-gegevensmodel-design.md`](2026-11-19-telemetrie-gegevensmodel-design.md).

Dit ontwerp bouwt op:

- #378, gemerged (#570): `trajectory_events` (vraag, antwoord, done, migratie `0012`) en
  `telemetry.LoadTimeline` met de doorlooptijd per oorzaak.
- #375, gemerged (#577): inzet (model, harness, versies, `setups.lane`), tokens en kosten, sessies,
  en `trajectories.plan_tasks` en `plan_path` (migratie `0013`). `telemetry collect` schrijft ook
  `question`- en `answer`-gebeurtenissen uit de transcripts van gesloten, gekoppelde interactieve
  sessies (`source = 'transcript'`, `internal/telemetry/collect/process.go`); de dispatcher schrijft
  de zijne met een `run_id`.
- #388, spec [`2026-11-24-issue-388-explore-exploit-design.md`](2026-11-24-issue-388-explore-exploit-design.md):
  de secties *Catalogus*, *Kenmerken* en *Uitkomstmaten* zijn de lijsten van deze UI. Dit ontwerp
  neemt ze over; het voegt alleen model en harness als extra filter toe (de issuetekst vraagt die).
  De bouw van de indeling (#572) wacht op dit issue.

## Aanleiding en doel

Lighthouse legt sinds #373 per traject vast wat er gebeurde: bevindingen (#376), herstel na merge
(#377), en straks inzet, kosten (#375) en menswerk (#378). Niemand kan het zien. Het doel van de
milestone is één vraag te beantwoorden: **wat voegt een onderdeel toe, en wanneer?** Dit issue maakt
dat antwoord zichtbaar in de lighthouse-UI, als een nieuw tabblad naast *Controller* en
*Priorities*.

Succes: voor de planreview staat per plangrootteklasse hoe vaak hij iets vond, hoe vaak dat werd
overgenomen, wat hij kostte en hoe trajecten met en zonder hem afliepen. Elke vergelijking zegt dat
ze correlatie is. Wat niet gemeten is, staat er met reden en nooit als nul.

## Ontwerpprincipe *(gekozen door de gebruiker)*

**De logica zit in Go en is daar getest; de UI is dun.** Groepsindeling, plangrootteklassen,
het correlatielabel en de dekking zijn Go-functies die op een fixture-database worden getest. De
endpoints leveren kant-en-klare aggregaten. De frontend tekent en filtert alleen; ze rekent niet.

## Doelen

- Een tabblad `Telemetry` met vier delen: onderdelen, tijdlijn, trajecten (doorklikbaar) en dekking.
- Per onderdeel uit de catalogus van #388: gevonden, overgenomen, kosten en afloop met en zonder het
  onderdeel, uitgesplitst naar de kenmerken van #388 (route, plangrootte, PR-grootte, gebied, repo)
  en naar model en harness.
- Een weektijdlijn met kosten, herstel en mensvragen, en een plek voor markeringen.
- Een trajectdetail met runs, sessies, bevindingen, herstel en gebeurtenissen.
- Zichtbare dekking: elk gat met een reden.

## Niet-doelen

- Versiewissels als markering detecteren: dat is #380. Dit issue levert het veld `markers` op de
  tijdlijn (leeg) en de weergave ervan.
- De kosten van een review meten. De codex-uitvoer bevat geen tokens en #375 leest alleen
  Claude-transcripts. De UI toont de reviewkosten als *niet gemeten* *(gekozen door de gebruiker)*.
- Oude reviewrecords koppelen. Geen van de 166 planreview-records op deze host heeft een
  `trajectory`-veld (allemaal van vóór #376). Een eenmalige analyse van die records komt in een eigen
  issue *(gekozen door de gebruiker: het moet gebeuren, maar niet hier)*. In de UI tellen ze als
  dekkingsgat.
- Schaduw- en variantindelingen toekennen: dat is #572. Alle records zijn nu `standard`. Het label
  volgt de indeling: een vergelijking met een `variant:`-traject verliest het label *correlatie*
  zonder UI-werk (§5).
- Codex- en OpenCode-sessies als bron (milestone *Explore/exploit*).

## Anti-doelen

- Een vergelijking die eruitziet als een oorzaak. Elk vergelijkingsblok draagt het label, niet alleen
  een voetnoot op de pagina.
- Een ontbrekende meting die als `0` verschijnt.
- Transcript- of reviewinhoud in de UI. Het trajectdetail toont sleutels, ernst, besluit en
  verwijzingen (`record_path`), geen tekst. Daarmee raakt dit issue de open milestonevraag over
  afscherming van gevoelige inhoud niet.
- Een UI die schrijft. De telemetrie-endpoints lezen via een readonly-verbinding.

## Aanpak

Drie manieren overwogen:

- **A. Aggregatie in Go, kant-en-klaar naar de UI** — readers in `internal/telemetry`, huma-endpoints
  in `internal/server`, een dun React-tabblad. Volgt het patroon van `registerPriorities`; de
  moeilijkste logica is testbaar op fixture-DB's.
- **B. Ruwe rijen naar de browser, aggregatie in React** — dunnere server, maar de kernlogica in TS,
  minder goed te testen tegen schema-randgevallen; de payload groeit met elk traject.
- **C. Eerst een CLI-rapport** — goedkoop, maar AC 3 en 4 (tijdlijn, doorklikken) vragen de UI.

**Gekozen (door de gebruiker): A.**

## Ontwerp

### 1. Schrijver: elke review die draaide, ook zonder vondsten *(gekozen door de gebruiker)*

Vandaag schrijft `review.IndexRecords` alleen een `findings`-rij per vondst. Een review zonder
vondsten laat geen spoor, dus "traject met review, niets gevonden" is niet te onderscheiden van
"traject zonder review". Daarom een nieuwe tabel, gevuld door dezelfde index, alleen voor plan- en
specreviewrecords:

```sql
CREATE TABLE review_runs (
  id INTEGER PRIMARY KEY,
  source TEXT NOT NULL, source_id TEXT NOT NULL, recorded_at TEXT NOT NULL,
  trajectory_id INTEGER REFERENCES trajectories(id),
  unlinked_reason TEXT CHECK (unlinked_reason IN ('no_trajectory')),
  component TEXT NOT NULL CHECK (component IN ('plan_review', 'spec_review')),
  record_path TEXT NOT NULL, record_sum TEXT NOT NULL,
  ran_at TEXT NOT NULL,                  -- written_at van het record
  outcome TEXT,                          -- outcome van het record: converged, round_limit, aborted
  finding_count INTEGER NOT NULL,
  CHECK ((trajectory_id IS NULL) = (unlinked_reason IS NOT NULL)),
  UNIQUE (source, source_id)
);
CREATE INDEX review_runs_trajectory ON review_runs(trajectory_id);
```

- Eén rij per afgerond record (`<kind>-review/<chain>/<attempt>/record.json`). `source_id` is het
  chainprefix, zoals bij de bevindingen; een nieuwere poging vervangt de rij.
- `IndexLedger` blijft zoals het is en schrijft geen `review_runs`-rij: `task_review` wordt in dit
  issue niet gemeten (§3) *(eigen keuze na specreview, r1-f3, r1-f9, r1-f14)*.
- Een record zonder traject wordt nu niet meer stil overgeslagen: het krijgt een rij met
  `trajectory_id = NULL` en een reden. Zo is het dekkingsgat in de database te lezen. De `Skip` in
  `IndexResult` blijft daarnaast bestaan voor de CLI-uitvoer.
- Onleesbare records (kapotte JSON, foute `written_at`) blijven een `Skip` zonder rij: er is geen
  betrouwbare component of tijd.
- Idempotent: dezelfde `record_sum` verandert niets; `DeleteFindingsUnder` krijgt een tegenhanger
  voor `review_runs`.
- Atomair per ketenpoging: het verwijderen van de oude rijen, de `review_runs`-rij en alle
  bevindingen gaan in één transactie. Vandaag opent elke `RecordFinding` een eigen transactie
  (`internal/telemetry/record.go`); die krijgt een `Tx`-variant, zoals `recordEventTx` *(eigen keuze
  na specreview, r1-f6)*.
- Een afgebroken review (`outcome = 'aborted'`) is een review die draaide: hij telt mee in de noemer
  van *gevonden* (#388, *Uitkomstmaten*: mislukte en afgebroken runs tellen mee).
- Migratie: `0014`, het eerstvolgende vrije nummer na #375: `review_runs` en de kolommen van §1b.

### 1b. Schrijver: kenmerken per traject uit de forge *(gekozen door de gebruiker)*

Route, gebied en PR-grootte (#388, *Kenmerken*) staan niet in `lighthouse.db`. `measure-recovery`
leest de forge al (`internal/telemetry/recovery.go`, `forgeissues.go`) en schrijft `recoveries`.
Hij krijgt een tweede pas, de **kenmerkenpas**, die de kenmerken vastlegt:

```sql
ALTER TABLE trajectories ADD COLUMN route TEXT;      -- gate uit de triagemarker: '0', '1', '2', '4a', '4b', '4c'
ALTER TABLE trajectories ADD COLUMN area TEXT;       -- het label met labels.area_prefix, zonder prefix
ALTER TABLE recoveries ADD COLUMN code_lines INTEGER; -- roborev.CountCodeLines op de PR-diff
```

- **Route**: de `Gate` van het laatste issuecomment met een triagemarker
  (`protocol.ParseTriageMarker`) vóór de eerste `review_runs`-rij van het traject, of vóór de merge
  als er geen review was (#388). `invariant` telt als `0` (#388: "poort 0/stop") *(eigen keuze na
  specreview, r1-f16)*; geen marker geeft `NULL`.
- **Gebied**: precies één issuelabel met `labels.area_prefix` geeft de rest van dat label. Nul of
  meer dan één geeft `NULL` *(eigen keuze)*. Zonder `labels.area_prefix` blijft het `NULL`.
- **PR-grootte** per PR: `roborev.CountCodeLines` op `git show --numstat` van de squashcommit, lokaal.
  `GitSource.MergeCommit` geeft alleen bestanden, dus `GitSource` krijgt een numstat-lezer. Het
  traject krijgt de klasse van zijn grootste PR: `< roborev.Threshold` of `≥ roborev.Threshold`
  *(eigen keuze)*.
- **Welke trajecten** *(eigen keuze na specreview, r1-f5)*: de herstelpas slaat gesloten rijen over
  (`internal/cli/measure_recovery.go`, `if hasRow && s.Status != telemetry.RecoveryOpen`) en
  `RecordRecovery` laat een gesloten rij staan. Dat blijft zo. De kenmerkenpas loopt los daarvan
  over elk traject met minstens één `recoveries`-rij:
  - met een `NULL` in route, gebied of een `code_lines` van zijn PR's: vullen. Zo krijgen ook alle
    bestaande gesloten rijen na migratie `0014` hun kenmerken (backfill);
  - met een PR die binnen `--days` mergede: route en gebied opnieuw lezen, zodat een later label of
    comment binnen dat venster meetelt;
  - `code_lines` wordt één keer per PR gezet; een squashcommit verandert niet.
- Per traject één forgeread: het issue met labels en comments. Een forgefout laat de oude waarden
  staan, meldt het en gaat door met het volgende traject, zoals bij het herstel.
- `NULL` is *niet gemeten* en staat in de dekking (§8), nooit als eigen klasse naast de andere.

AC "de UI leest alleen" gaat over de UI. De review-index (§1) en `measure-recovery` (§1b) zijn
bestaande schrijvers die extra gegevens vastleggen.

### 2. Readonly-verbinding

`store.Open` neemt een serverslot en migreert; `OpenShared` migreert ook en zet WAL. Beide schrijven.
Er is al een `store.OpenReadOnly` (#375, `internal/store/store.go`): `mode=ro`, geen migratie, en
`immutable=1` zodra er geen `-wal`-bestand naast de database ligt. Die past niet bij een server die
lang draait terwijl `collect`, de review-index en `measure-recovery` blijven schrijven: met
`immutable=1` ziet de UI tot een herstart oude gegevens. `telemetry collect --dry-run` leunt op het
huidige gedrag, dus dat blijft ongewijzigd.

Nieuw: `store.OpenReadOnlyLive(ctx, path) (*Store, error)`, DSN `mode=ro` en
`_pragma=query_only(1)`, nooit `immutable`, zonder migratie *(eigen keuze na specreview, r1-f4)*.
`lighthouse serve` opent die naast de bestaande store en geeft hem alleen aan de
telemetrie-readers. Een test bewijst dat een `INSERT` via die verbinding faalt, en een tweede dat
een rij die na het openen geschreven is, zichtbaar wordt.

Ontbreekt een tabel, dan geeft het endpoint geen 500 maar een dekkingsmelding "database niet
gemigreerd". Onder `lighthouse serve` gebeurt dat niet, want serve migreert bij de start: het is een
verdediging, alleen met een test bewezen *(r1-f13)*.

### 3. Populaties en groepen

De vergelijkingen draaien binnen een populatie, zodat met en zonder over hetzelfde soort werk gaan:

De onderdelen zijn de catalogus van #388, met dezelfde namen (de waarden van `findings.component`
en van `variant:<onderdeel>=…`):

| Onderdeel | Populatie | Met | Zonder |
|---|---|---|---|
| `plan_review` | trajecten met `plan_tasks IS NOT NULL` | ≥ 1 gekoppelde `review_runs`-rij `plan_review` | geen |
| `spec_review` | idem *(eigen keuze: een spec gaat aan een plan vooraf; trajecten zonder spec zijn niet af te bakenen)* | ≥ 1 `spec_review` | geen |
| `lane` | trajecten met een bekende lane | `sdd` | `native` |
| `model` | trajecten met een bekend model in hun inzet | per model | — |
| `harness` | trajecten met een bekende harness | per harness | — |
| `task_review` | — | *niet gemeten*, zie hieronder | — |
| `final_review`, `pr_review`, `test_gate` | — | *niet gemeten*: geen bron in deze milestone (#388) | — |

`harness` heeft nu alleen Claude Code als bron; de UI toont het blok met één groep en de dekking
zegt *niet gemeten tot Explore/exploit* voor Codex en OpenCode.

*(Gekozen door de gebruiker:* de groep "zonder planreview" zijn trajecten mét een plan maar zonder
reviewrecord, niet alle trajecten.) Lane, model en harness zijn geen aan/uit-onderdelen: ze vergelijken waarden,
en "gevonden" en "overgenomen" zijn daar *niet van toepassing*.

**Cohort van plan- en specreview** *(eigen keuze na specreview, r1-f11)*: de 169 bestaande
planreviewrecords hebben geen `trajectory`-veld en worden ongekoppelde rijen, terwijl hun
trajecten via `collect` wel een `plan_tasks` krijgen. Zonder ingreep belanden gereviewde plannen in
"zonder". De populatie van `plan_review` en `spec_review` begint daarom per repo bij de `ran_at` van
de eerste gekoppelde `review_runs`-rij van dat onderdeel. Eerdere trajecten tellen niet mee en
staan als aantal in de dekking ("vóór de koppeling van reviewrecords").

**`task_review` niet gemeten** *(eigen keuze na specreview, r1-f3, r1-f9, r1-f10)*: een ledger komt
alleen via een handmatige `review-index --ledger` in de database (`docs/review-index.md`), wordt aan
het eind van de SDD-run verwijderd, kent geen review per taak als eenheid en geen besluit
`adopted` (`internal/review/ledger.go`). Een vergelijking met en zonder zou meten wie de ledger
indexeerde. De dekking zegt waarom; het aantal geïndexeerde ledgerbevindingen staat erbij.

**Lane per traject** *(eigen keuze)*: `setups.lane` (#375) staat per run of sessie. Een traject is
`sdd` als minstens één run of sessie lane `sdd` had, `native` als er alleen `native` was, `mixed`
bij beide, en onbekend zonder lane. `mixed` en onbekend vallen buiten de lane-vergelijking en staan
als aantal in de dekking.

Een traject met meer dan één model (bijvoorbeeld Opus in de sessie, Sonnet in een subagent) telt bij
het model met de meeste kosten *(eigen keuze)*; heeft geen enkel model kosten, dan bij het model met
de meeste tokens.

**Kenmerken** (#388) met hun klassen, elk een vaste lijst in Go, één plek:

| Kenmerk | Klassen | Bron |
|---|---|---|
| route | `0`, `1`, `2`, `4a`, `4b`, `4c` | `trajectories.route` (§1b) |
| plangrootte | klein (≤ 3 taken), middel (4–8), groot (≥ 9) | `trajectories.plan_tasks` (#375) |
| PR-grootte | `< roborev.Threshold`, `≥ roborev.Threshold` coderegels | `recoveries.code_lines` (§1b) |
| gebied | de waarden van het gebiedslabel | `trajectories.area` (§1b) |
| repo | `trajectories.repo` | — |

Daarnaast model en harness als filter, uit de inzet (#375).

### 4. De vier antwoorden per onderdeel

Per onderdeel × filtercontext (de kenmerken van §3, plus model en harness). De maten zijn de
*Uitkomstmaten* van #388:

- **Gevonden**: aantal reviews met ≥ 1 bevinding / aantal reviews; plus het totaal aantal bevindingen
  en de verdeling naar ernst.
- **Overgenomen**: bevindingen met `decision = 'adopted'` / bevindingen met een besluit. `left_open`
  en `NULL` tellen apart, niet als afgewezen.
- **Kosten**: voor reviews *niet gemeten — de reviewkosten worden niet vastgelegd*. Daarnaast de
  mediaan van de trajectkosten (som `usage.cost`, #375) per groep, met `cost_kind` erbij; een groep
  die `estimate` en `actual` mengt, zegt dat.
- **Afloop**, per groep met en zonder, over de *afgeronde* trajecten (hieronder):
  - herstel na merge: aandeel trajecten waarvan minstens één PR `recoveries.status = 'recovered'`
    heeft, en het gemiddeld aantal `recovery_items`;
  - mensvragen: mediaan van het aantal `question`-gebeurtenissen (#378), uit beide bronnen: de
    dispatcher (`run_id`) en interactieve sessies (`source = 'transcript'`). Het trajectdetail
    toont de splitsing *(eigen keuze na specreview, r1-f8)*;
  - doorlooptijd: mediaan van de actieve tijd en van de wachttijd op een mens, uit
    `telemetry.LoadTimeline` (#378), met het sluitmoment hieronder als `closedAt`.

**Afgerond traject** *(gekozen door de gebruiker: alleen de database, geen forge)*: een traject met
minstens één `recoveries`-rij, waarvan geen rij `status = 'open'` heeft (het meetvenster is dicht).
Het sluitmoment is de `merged_at` van zijn laatste gemergde PR. Trajecten die nog lopen (een open
venster) of geen herstelmeting hebben (nooit gemerged, of `measure-recovery` draaide niet), tellen
niet mee en staan als aantal bij elke vergelijking. Een issue dat zonder PR sluit, valt daardoor
buiten de afloop; dat staat in de dekking.

**Per maat een eigen `n`** *(eigen keuze na specreview, r1-f12)*: binnen de afgeronde trajecten
heeft een maat geen waarde als zijn bron ontbreekt, en dan valt het traject alleen voor die maat
weg. De trajectkosten zijn `NULL` zonder `usage`-rijen of met één rij zonder prijs
(`cost IS NULL`), want een deels geprijsde som is te laag. De doorlooptijd is `NULL` zonder runs en
sessies. Nooit `0` voor een ontbrekende meting.

Elke celwaarde draagt zijn `n`. Onder een minimum (*eigen keuze*: `n < 5`) toont de UI de waarde
grijs met "te weinig trajecten"; er wordt niets verborgen.

### 5. Het correlatielabel

*(Eigen keuze na specreview, r1-f1.)* De indeling staat op `runs` en `sessions`, niet op
`trajectories`. **Indeling per traject**: `variant:<x>` als al zijn runs en sessies dezelfde
`variant:<x>` dragen; `standard` als geen enkele een `variant:` draagt (`shadow:` telt als
`standard`); anders `mixed`. Bevindingen hebben geen indeling; ze erven die van hun traject.

`telemetry.ComparisonLabel(groups [][]string) string` krijgt per groep van de vergelijking de
indelingen van haar trajecten. Hij geeft `assigned` als elke groep zuiver is (alle trajecten dezelfde
indeling, geen `mixed`) en minstens één groep `variant:` is: lighthouse koos de keuze, dus de
vergelijking is meer dan waarneming (#388). In alle andere gevallen `correlation`. Elk
vergelijkingsblok in de API heeft een veld `label`; de UI toont het in het blok zelf. Bij `correlation` staat de zin *"Observed, not caused: reviews run more often on
hard issues."* Bij `assigned` staat *"Assigned by lighthouse"*. Alle records zijn nu `standard`; de
tak `assigned` is alleen met een fixture te zien.

### 6. Tijdlijn

Per ISO-week (`maandag` als start, UTC): som van de kosten (naar `usage.recorded_at`), aantal
mensvragen (naar het tijdstip van de vraag), en van de trajecten die die week afrondden (sluitmoment
uit §4) het aantal en het aantal met herstel. Het veld `markers` (lijst met week,
soort en tekst) is leeg tot #380 het vult; de UI tekent ze als verticale lijn. Een week vóór de
eerste rij van een reeks krijgt `null`; daarna is een week zonder rijen `0` *(r1-f15)*.

### 7. Trajecten en het detail

Een lijst van trajecten (repo, issue, plangrootte, onderdelen die draaiden, kosten, herstelstatus),
sorteerbaar, met filters zoals de onderdelen. Klikken opent `#telemetry/trajectory/<id>`:

- runs (start, eind, uitkomst, inzet);
- sessies (harness, branch, koppelbasis, inzet, tokens en kosten per model);
- reviews (`review_runs`) met hun bevindingen: sleutel, ernst, besluit, `record_path`;
- herstel: PR, status, items (fix, revert, bug-issue) met verwijzing;
- gebeurtenissen en de doorlooptijd per oorzaak (#378).

Geen inhoud van transcripts of reviews; alleen verwijzingen.

### 8. Dekking

Eén blok, elke regel een aantal met reden:

- sessies zonder koppeling, per `unlinked_reason` (`main_only`, `no_issue_branch`, `no_issue_number`);
- reviewrecords zonder traject, per `unlinked_reason` van `review_runs`;
- trajecten vóór de koppeling van reviewrecords (§3, cohort), per onderdeel;
- trajecten zonder inzet, zonder kosten, of met kosten zonder prijs (`cost IS NULL`);
- trajecten zonder route, gebied of PR-grootte (§1b), per kenmerk;
- trajecten buiten de afloop: lopend (open venster), zonder herstelmeting, of zonder lane / met
  `mixed` lane;
- vragen uit interactieve sessies die nog open of niet gekoppeld zijn: `collect` schrijft vragen
  alleen voor gesloten, gekoppelde sessies;
- `task_review`: *niet gemeten*, met de reden uit §3;
- onderdelen zonder bron in deze milestone: `final_review`, `pr_review`, `test_gate`, en de reviewkosten — als
  *niet gemeten*;
- harnesses zonder bron: Codex en OpenCode — *niet gemeten tot Explore/exploit*.

### 9. API en frontend

Endpoints (huma, `OperationID`, snake_case, nil-slices als `[]`, zoals `registerPriorities`):

- `GET /api/telemetry/components?repo=&route=&size=&pr_size=&area=&model=&harness=`
- `GET /api/telemetry/timeline?repo=`
- `GET /api/telemetry/trajectories?repo=&route=&size=&pr_size=&area=&model=&harness=&component=`
- `GET /api/telemetry/trajectories/{id}`
- `GET /api/telemetry/coverage?repo=`

De filters betekenen op elk endpoint hetzelfde; doorklikken vanuit een vergelijking neemt ze mee
*(r1-f7)*.

De exacte vorm van de responses is aan het plan. `server.Deps` krijgt één veld met de readers,
gezet in `internal/cli/serve.go`.

Frontend: `TabId` wordt `'controller' | 'priorities' | 'telemetry'`; `fromHash` leert
`#telemetry` en `#telemetry/trajectory/<id>`. Een map `web/src/components/telemetry/` met een
component per deel, queries in `web/src/lib/queries.ts`, de client via `npm run gen`. UI-teksten in
het Engels, zoals de rest van de UI *(eigen keuze)*.

## Randvoorwaarden

- Geen tweede opslag: `review_runs` indexeert de recordbestanden met verwijzing en hash, zoals
  `findings`.
- Geen automatische wijziging aan model, skill of configuratie.
- `web/openapi.json` en de gegenereerde client worden meegecommit (`TestOpenAPISnapshot`); de
  build-uitvoer niet.
- Afhankelijkheid: #375 (inzet, kosten, `plan_tasks`, lane) en #378 (`trajectory_events`), beide
  gemerged.
- De telemetrie-readers lezen alleen `lighthouse.db`, geen forge.

## Tests en bewijs

- `internal/review`: een record met nul bevindingen geeft één `review_runs`-rij met
  `finding_count = 0`; een record zonder traject geeft een rij met `no_trajectory`; een tweede
  index met dezelfde hash verandert niets; een nieuwere poging vervangt de rij; een afgebroken
  record krijgt `outcome = 'aborted'`; een fout halverwege laat de oude rijen intact; een ledger
  geeft geen `review_runs`-rij.
- `internal/telemetry` (`measure-recovery`): route uit het laatste triagecomment vóór de eerste
  review, en `invariant` als `0`; gebied bij nul, één en twee gebiedslabels; `code_lines` per PR;
  een bestaande gesloten rij krijgt zijn kenmerken (backfill) zonder dat zijn herstel verandert;
  een forgefout laat de oude waarde staan.
- `internal/telemetry`: fixture-DB met trajecten met en zonder planreview in twee grootteklassen;
  de aggregaten, de `n` per maat, het cohort, het label (`correlation`; `assigned` met zuivere
  `variant:`- en `standard`-groepen; `correlation` bij een `mixed`-traject), `NULL`-kosten bij een
  ongeprijsde rij, `null` voor ontbrekende bronnen en het weglaten van `status = 'open'`.
- `internal/store`: migratietest op `0014`; `OpenReadOnlyLive` weigert een `INSERT` en ziet een
  rij die na het openen geschreven is.
- `internal/server`: handlertests per endpoint, de dekkingsmelding bij een ongemigreerde database,
  en de openapi-snapshot.
- `web`: vitest per deel (label zichtbaar in elk vergelijkingsblok, `n < 5` grijs, `null` als
  *not measured*), de hash-route van het detail; Playwright-e2e voor het tabblad.
- Bewijs AC 7: `lighthouse serve` met een nieuw gebouwde binary op de echte `lighthouse.db`, na
  `lighthouse telemetry collect` (#375), de review-index en `measure-recovery`; screenshot van
  `#telemetry` via Playwright (`e2e/screenshot.mjs` wacht op de controllertitels en moet een
  tabbladparameter krijgen).

## Aan de bouwer

- De responsvormen, de componentindeling in React en de grafiekbibliotheek (bij voorkeur wat
  `web/` al heeft; anders gewone SVG).
- De naam en plek van de readers in `internal/telemetry`.
- Hoe `measure-recovery` de issuecomments en labels leest: een methode op `RecoverySource` of een
  eigen interface, zolang het via `internal/forge` loopt.
- Of `LoadTimeline` per traject snel genoeg is voor de aggregaten, of dat een batchvariant nodig is.

## Open

- Bewaartermijn en afscherming van transcriptinhoud (milestone, *Nog niet vastgelegd*): niet
  geraakt, want de UI toont geen inhoud.
- De eenmalige analyse van de 166 oude planreview-records: eigen issue, na deze spec.
