# Ontwerp: telemetrie in de UI — wat voegt elk onderdeel toe, en wanneer

Issue: #379. Milestone: *Telemetrie: wat voegt elk onderdeel toe*. Ontwerp van de milestone:
[`2026-11-16-zelflerende-lighthouse-design.md`](2026-11-16-zelflerende-lighthouse-design.md) (T6).
Gegevensmodel: [`2026-11-19-telemetrie-gegevensmodel-design.md`](2026-11-19-telemetrie-gegevensmodel-design.md).

Dit ontwerp bouwt op:

- #378, gemerged (#570): `trajectory_events` (vraag, antwoord, done, migratie `0012`) en
  `telemetry.LoadTimeline` met de doorlooptijd per oorzaak.
- #375, nog niet gemerged; spec
  `docs/developer/planning/specs/2026-11-24-issue-375-inzet-kosten-tokens-design.md` op branch
  `issue-375-inzet-kosten-tokens`: inzet (model, harness, versies, lane), tokens en kosten, sessies,
  en `trajectories.plan_tasks`. Zijn migratie schuift naar `0013`.

Het plan van #379 wacht op de merge van #375.

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
- Per onderdeel: gevonden, overgenomen, kosten en afloop met en zonder het onderdeel, uitgesplitst
  naar repo, plangrootteklasse, model en harness.
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
- Schaduw- en variantvergelijkingen. Alle records zijn `standard`; het label volgt de indeling, zodat
  een latere indeling zonder UI-werk een ander label krijgt.
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

Vandaag schrijft `review.IndexRecords` alleen een `findings`-rij per vondst, en `IndexLedger`
idem voor de SDD-ledger. Een review zonder vondsten laat geen spoor, dus "traject met review, niets
gevonden" is niet te onderscheiden van "traject zonder review". Daarom een nieuwe tabel, gevuld door
dezelfde index:

```sql
CREATE TABLE review_runs (
  id INTEGER PRIMARY KEY,
  source TEXT NOT NULL, source_id TEXT NOT NULL, recorded_at TEXT NOT NULL,
  trajectory_id INTEGER REFERENCES trajectories(id),
  unlinked_reason TEXT CHECK (unlinked_reason IN ('no_trajectory', 'no_plan_header')),
  component TEXT NOT NULL,               -- 'plan_review', 'spec_review', 'task_review'
  record_path TEXT NOT NULL, record_sum TEXT NOT NULL,
  ran_at TEXT NOT NULL,                  -- written_at van het record, of mtime van de ledger
  finding_count INTEGER NOT NULL,
  CHECK ((trajectory_id IS NULL) = (unlinked_reason IS NOT NULL)),
  UNIQUE (source, source_id)
);
CREATE INDEX review_runs_trajectory ON review_runs(trajectory_id);
```

- Eén rij per afgerond record (`<kind>-review/<chain>/<attempt>/record.json`) en per ledger.
  `source_id` is het chainprefix, zoals bij de bevindingen; een nieuwere poging vervangt de rij.
- Een record of ledger zonder traject wordt nu niet meer stil overgeslagen: het krijgt een rij met
  `trajectory_id = NULL` en een reden. Zo is het dekkingsgat in de database te lezen. De `Skip` in
  `IndexResult` blijft daarnaast bestaan voor de CLI-uitvoer.
- Onleesbare records (kapotte JSON, foute `written_at`) blijven een `Skip` zonder rij: er is geen
  betrouwbare component of tijd.
- Idempotent: dezelfde `record_sum` verandert niets; `DeleteFindingsUnder` krijgt een tegenhanger
  voor `review_runs`.
- Migratie: het eerstvolgende vrije nummer na #375 (vermoedelijk `0014`), alleen
  `CREATE TABLE`/`CREATE INDEX`.

Dit is de enige schrijver in dit issue. AC "de UI leest alleen" gaat over de UI; de index is een
bestaande schrijver die een extra rij legt.

### 2. Readonly-verbinding

`store.Open` neemt een serverslot en migreert; `OpenShared` migreert ook en zet WAL. Beide schrijven.
Nieuw: `store.OpenReadOnly(ctx, path)`, een `*sql.DB` met DSN `mode=ro` en `_pragma=query_only(1)`,
zonder migratie. `lighthouse serve` opent die naast de bestaande store en geeft hem alleen aan de
telemetrie-readers. Een test bewijst dat een `INSERT` via die verbinding faalt.

Ontbreekt een tabel omdat de database ouder is dan de migraties (zoals de lokale `lighthouse.db` nu,
zonder `findings`), dan geeft het endpoint geen 500 maar een dekkingsmelding "database niet
gemigreerd", en de rest van de UI blijft werken.

### 3. Populaties en groepen

De vergelijkingen draaien binnen een populatie, zodat met en zonder over hetzelfde soort werk gaan:

| Onderdeel | Populatie | Met | Zonder |
|---|---|---|---|
| planreview | trajecten met `plan_tasks IS NOT NULL` | ≥ 1 gekoppelde `review_runs`-rij `plan_review` | geen |
| specreview | idem *(eigen keuze: een spec gaat aan een plan vooraf; trajecten zonder spec zijn niet af te bakenen)* | ≥ 1 `spec_review` | geen |
| taakreview | trajecten met lane `sdd` (#375) | ≥ 1 `task_review` | geen |
| model | trajecten met een bekend model in hun inzet | per model | — |
| lane | trajecten met een bekende lane | `sdd` | `native` |

*(Gekozen door de gebruiker:* de groep "zonder planreview" zijn trajecten mét een plan maar zonder
reviewrecord, niet alle trajecten.) Model en lane zijn geen aan/uit-onderdelen: ze vergelijken waarden,
en "gevonden" en "overgenomen" zijn daar *niet van toepassing*.

**Lane per traject** *(eigen keuze)*: `setups.lane` (#375) staat per run of sessie. Een traject is
`sdd` als minstens één run of sessie lane `sdd` had, `native` als er alleen `native` was, `mixed`
bij beide, en onbekend zonder lane. `mixed` en onbekend vallen buiten de lane-vergelijking en de
taakreview-populatie, en staan als aantal in de dekking.

Een traject met meer dan één model (bijvoorbeeld Opus in de sessie, Sonnet in een subagent) telt bij
het model met de meeste kosten *(eigen keuze)*; heeft geen enkel model kosten, dan bij het model met
de meeste tokens.

**Plangrootteklassen** uit `trajectories.plan_tasks` (#375): `1–4`, `5–8`, `9+` *(eigen keuze, de
grenzen uit de paia-meting die #375 aanhaalt)*. Een vaste lijst in Go, één plek.

### 4. De vier antwoorden per onderdeel

Per onderdeel × filtercontext (repo, plangrootteklasse, model, harness):

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
  - mensvragen: mediaan van het aantal `question`-gebeurtenissen (#378);
  - doorlooptijd: mediaan van de actieve tijd en van de wachttijd op een mens, uit
    `telemetry.LoadTimeline` (#378), met het sluitmoment hieronder als `closedAt`.

**Afgerond traject** *(gekozen door de gebruiker: alleen de database, geen forge)*: een traject met
minstens één `recoveries`-rij, waarvan geen rij `status = 'open'` heeft (het meetvenster is dicht).
Het sluitmoment is de `merged_at` van zijn laatste gemergde PR. Kosten, mensvragen, doorlooptijd en
herstel rekenen alle vier over deze ene groep, zodat de noemer overal gelijk is. Trajecten die nog
lopen (een open venster) of geen herstelmeting hebben (nooit gemerged, of `measure-recovery` draaide
niet), tellen niet mee en staan als aantal bij elke vergelijking. Een issue dat zonder PR sluit, valt
daardoor buiten de afloop; dat staat in de dekking.

Elke celwaarde draagt zijn `n`. Onder een minimum (*eigen keuze*: `n < 5`) toont de UI de waarde
grijs met "te weinig trajecten"; er wordt niets verborgen.

### 5. Het correlatielabel

`telemetry.ComparisonLabel(assignments []string) string` geeft `correlation` zolang alle
indelingen van de runs en sessies in de vergelijking `standard` zijn. Bevindingen hebben geen
indeling; ze erven die van hun traject. Komt er later een `shadow:`- of `variant:`-indeling bij, dan
geeft de functie een ander label (nu niet gebouwd: alleen de tak `correlation` en een
`mixed`-terugval die de UI als correlatie toont). Elk vergelijkingsblok in de API heeft een veld
`label`; de UI toont het in het blok zelf, met de zin *"Observed, not caused: reviews run more often
on hard issues."*

### 6. Tijdlijn

Per ISO-week (`maandag` als start, UTC): som van de kosten (naar `usage.recorded_at`), aantal
mensvragen (naar het tijdstip van de vraag), en van de trajecten die die week afrondden (sluitmoment
uit §4) het aantal en het aantal met herstel. Het veld `markers` (lijst met week,
soort en tekst) is leeg tot #380 het vult; de UI tekent ze als verticale lijn. Een week zonder bron
voor een reeks krijgt `null`, geen `0`.

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
- reviewrecords en ledgers zonder traject, per `unlinked_reason` van `review_runs`;
- trajecten zonder inzet, zonder kosten, of met kosten zonder prijs (`cost IS NULL`);
- trajecten buiten de afloop: lopend (open venster), zonder herstelmeting, of zonder lane / met
  `mixed` lane;
- vragen uit interactieve sessies: *niet gemeten* zolang `trajectory_events` geen rijen uit de
  transcriptbron heeft (#375 kan die taak naar een vervolgissue schuiven); de mediaan heet dan
  "dispatcher questions";
- onderdelen zonder bron in deze milestone: eindreview, test-gate, en de reviewkosten — als
  *niet gemeten*;
- harnesses zonder bron: Codex en OpenCode — *niet gemeten tot Explore/exploit*.

### 9. API en frontend

Endpoints (huma, `OperationID`, snake_case, nil-slices als `[]`, zoals `registerPriorities`):

- `GET /api/telemetry/components?repo=&size=&model=&harness=`
- `GET /api/telemetry/timeline?repo=`
- `GET /api/telemetry/trajectories?repo=&size=&component=`
- `GET /api/telemetry/trajectories/{id}`
- `GET /api/telemetry/coverage`

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
- Afhankelijkheid: #375 (inzet, kosten, `plan_tasks`, lane). Die staat nog niet als
  blocker-relatie op #379; die moet erbij. #378 is gemerged.
- De telemetrie-readers lezen alleen `lighthouse.db`, geen forge.

## Tests en bewijs

- `internal/review`: een record met nul bevindingen geeft één `review_runs`-rij met
  `finding_count = 0`; een record zonder traject geeft een rij met `no_trajectory`; een tweede
  index met dezelfde hash verandert niets; een nieuwere poging vervangt de rij.
- `internal/telemetry`: fixture-DB met trajecten met en zonder planreview in twee grootteklassen;
  de aggregaten, de `n`, het correlatielabel, `null` voor ontbrekende bronnen en het weglaten van
  `status = 'open'`.
- `internal/store`: migratietest op de nieuwe tabel; `OpenReadOnly` weigert een `INSERT`.
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
- De naam en plek van de readers in `internal/telemetry` en het migratienummer.
- Of `LoadTimeline` per traject snel genoeg is voor de aggregaten, of dat een batchvariant nodig is.

## Open

- Bewaartermijn en afscherming van transcriptinhoud (milestone, *Nog niet vastgelegd*): niet
  geraakt, want de UI toont geen inhoud.
- De eenmalige analyse van de 166 oude planreview-records: eigen issue, na deze spec.
