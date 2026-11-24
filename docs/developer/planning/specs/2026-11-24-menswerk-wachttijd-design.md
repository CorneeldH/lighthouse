# Ontwerp: menswerk en wachttijd per traject

Issue #378 (T5 van milestone *Telemetrie: wat voegt elk onderdeel toe*). Bouwt op het gegevensmodel
van #373: [`2026-11-19-telemetrie-gegevensmodel-design.md`](2026-11-19-telemetrie-gegevensmodel-design.md).
Herkomst: [`2026-11-16-zelflerende-lighthouse-design.md`](2026-11-16-zelflerende-lighthouse-design.md),
rij T5.

## Aanleiding en doel

Een onderdeel dat goedkoper is maar meer mensvragen oplevert, is niet beter. Om dat te zien moet
lighthouse per traject weten hoeveel menswerk het kostte (vragen, antwoorden, handmatige overnames)
en hoe lang het stilstond, en waarom: actief bezig, wachten op een mens, wachten op CI of review,
of een pauze om usage.

Vandaag staat een deel daarvan impliciet in `runs`, maar met de verkeerde tijdstippen:

- `ended_at` van een run met uitkomst `question` is het moment waarop een tick de Vraag zag
  (`runs.Release` stempelt `now`), niet het moment waarop de Vraag gesteld werd.
- Het moment waarop de mens antwoordde staat nergens; `runs` kent alleen de commentaar-id in
  `trigger_key`.
- Een agent-run blijft open tot de merge, `green` of een fix-pr, ook als de sessie allang Done
  meldde (`internal/dispatch/decide.go`, `mergeReady` en verwant). `started_at`..`ended_at` slikt
  daardoor het hele wachten op CI en review.

De tijdstippen van Vraag en antwoord staan wel op de forge, als `created_at` van de protocolcommentaren.
Done heeft geen eigen tijdstip: het `🤖 Done`-commentaar plaatst `apply` pas bij het sluiten van de
run (`internal/dispatch/apply.go`, `KindDone`, en de merge-tak), terwijl de sessie al eerder Done was
(`done()` in `internal/dispatch/decide.go` leest de werkplekstand). Dit ontwerp slaat de Vraag- en
antwoordtijdstippen op, neemt voor Done het tijdstip waarop een tick de stand Done het eerst ziet, en
leidt de rest af.

## Doelen, niet-doelen, anti-doelen

**Doelen.**

- Per traject: elke mensvraag, elk antwoord en elke handmatige overname, met tijdstip.
- Per traject: doorlooptijd opgesplitst naar oorzaak, zonder dubbeltelling van parallelle runs,
  sessies en hervattingen.
- Een tijd zonder bron staat als *ontbrekend* of *schatting*, nooit als nul.
- Een opslagplek waar de transcriptlezer van #375 vragen en antwoorden uit interactieve sessies in
  schrijft.

**Niet-doelen.**

- Transcripts lezen. Vragen en antwoorden uit interactieve sessies komen van de lezer van #375
  *(gekozen door de gebruiker, aanpak A)*; #378 levert de tabel en de schrijffunctie waar die lezer
  in schrijft.
- De UI. Die is #379; #378 levert de leesfunctie die #379 gebruikt, plus een opdracht om een
  traject in de terminal te bekijken.
- De bestede tijd van een mens meten. Er is geen bron voor: een open interactieve sessie zegt niet
  dat de mens werkte. Menswerk is in dit issue een *aantal* (vragen, antwoorden, overnames), geen
  duur. Wachttijd *op* een mens is wel een duur.
- Interactieve sessies zonder issuebranch (`main_only`, `no_issue_branch`, `no_issue_number`):
  die hebben geen traject (#373) en tellen hier niet mee.
- Backfill van oude trajecten. Die krijgen schattingen (zie *Tijdlijn*).

**Anti-doelen.** Het is mislukt, ook als het werkt, als:

- een gat zonder bron als `0` of als actieve tijd verschijnt;
- wachten op CI of review als actieve tijd telt terwijl er een Done-tijdstip is;
- een interval twee keer telt omdat een run en een sessie overlappen, of een hervatting de vorige
  run overlapt;
- hetzelfde gegeven twee keer opgeslagen staat (milestone-no-go *geen tweede opslag*);
- er transcripttekst of commentaartekst in `lighthouse.db` komt.

## Randvoorwaarden

- Eén opslag: `lighthouse.db`. Wat al in `runs` of `sessions` staat, wordt afgeleid bij het lezen en
  niet opnieuw opgeslagen. Het tijdstip van een protocolcommentaar staat nergens in `runs` en is
  dus geen dubbele opslag.
- Schrijvers zijn idempotent met `source`/`source_id`, zoals in #373 (`Origin` in
  `internal/telemetry/record.go`).
- Een nieuwe migratie `0012`, in één transactie, alleen `CREATE TABLE`/`CREATE INDEX`; bestaande
  tabellen veranderen niet. Mergt #375 eerder met een eigen migratie, dan schuift deze op.
- Geen transcriptinhoud en geen commentaartekst: alleen soort, tijdstip en bronsleutel. Daarmee
  raakt dit issue de open milestone-vraag over bewaren en afschermen van transcriptinhoud niet.
- Codex- en OpenCode-sessies zijn geen bron (milestone-no-go).

## Aanpakken

**Waar komen vragen uit interactieve sessies vandaan?**

- **A. Opslag en tijdlijn hier, de transcriptbron in #375** *(gekozen door de gebruiker)*. #378
  maakt de tabel, de schrijffunctie, de tijdlijn en de bron uit de dispatcher. De lezer van #375
  telt AskUserQuestion en het antwoord erop in dezelfde gang door het transcript als tokens en
  model. Eén lezer per transcript; interactieve mensvragen verschijnen pas na #375.
- B. Een eigen kleine transcriptscan in #378. Zelfstandig, maar #375 krijgt een tweede lezer van
  hetzelfde transcript.
- C. #378 wacht op #375. Niets nuttigs tot #375 klaar is.

**Opslag** *(voorstel, door de gebruiker overgenomen)*.

- **Eigen gebeurtenistabel, wachttijd berekend bij lezen.** Alleen wat nergens anders staat wordt
  opgeslagen; de rest volgt uit `runs` en `sessions`.
- Afgeleide kolommen per traject die de schrijver bijwerkt: elke nieuwe run vraagt een herberekening,
  en de kolommen zijn een tweede opslag van wat `runs` al weet.
- Gebeurtenissen plus een samenvatting: dezelfde dubbele opslag, met een extra kans op verschil.

**Wachten op CI en review** *(gekozen door de gebruiker)*.

- **Done als derde soort gebeurtenis.** Het tijdstip waarop een tick de sessie van een open run voor het
  eerst Done ziet (niet het Done-commentaar, dat pas bij het sluiten van de run komt) splitst een agent-run in
  `actief` (start tot Done) en `ci_review` (Done tot het einde van de run). Beide gemeten.
- `actief` laten zoals `runs` het geeft, gemarkeerd als "inclusief CI/review": geen extra opslag,
  maar `ci_review` blijft leeg en AC 2 is maar half gehaald.

**Doorlooptijd zonder dubbeltelling** *(voorstel, overgenomen)*.

- **Eén tijdlijn per traject met een vaste voorrang.** Alle intervallen op één as; elk moment krijgt
  precies één oorzaak. Overlap kan niet dubbel tellen, want een moment bestaat maar één keer.
- Per run optellen en overlap aftrekken: werkt voor twee overlappende runs, wordt foutgevoelig bij
  drie en bij runs die met sessies overlappen.
- Alleen de gaten tussen runs toewijzen: telt een vraag binnen een interactieve sessie als actieve
  tijd.

**Handmatige overname** *(voorstel, overgenomen)*: elke overgang van dispatcher naar mens. Een
commit van een mens op de issuebranch zonder sessie telt niet: die is niet van een agent-commit te
onderscheiden zonder een bron die er niet is.

## Het model

### Wat telt als wat

| Begrip | Herkenning |
|---|---|
| **mensvraag (dispatcher)** | een Vraag-commentaar (`protocol.KindQuestion`) van een run van het traject; ook een Vraag van de workflow-pusher, want die vraagt ook een mens |
| **antwoord (dispatcher)** | het eerste antwoordcommentaar op die Vraag dat de dispatcher oppakt: de run met `trigger_key` `answer:<id>` (hervatting) of `pushanswer:<id>` (pusher). `trigger = 'answer'` alleen is niet genoeg: `repair:<id>` draagt die trigger ook en beantwoordt geen mens |
| **oppakken** | van het antwoordcommentaar tot `started_at` van de run die het oppakte |
| **Done** | de eerste tick waarop `done()` (werkplekstand Done, zonder werk na de Stop) waar is voor een open agent-run; de nauwkeurigheid is die van de tick. Het `🤖 Done`-commentaar telt niet: dat valt samen met het sluiten van de run |
| **mensvraag / antwoord (sessie)** | een AskUserQuestion in het transcript en de gebruikersbeurt erna (lezer #375) |
| **usagepauze** | `runs.outcome = 'parked'` gevolgd door een run met `trigger = 'usage'` |
| **merge-run** | een run met `trigger_key` `merge:<pr>:<head>` (opgeslagen met `trigger = 'handoff'`, er is geen trigger `merge`) |
| **pusher-rij** | een push-rij van de workflow-pusher (`isPush`) of een merge-run: de sessie is al klaar en de rij wacht op de pusher, CI en review; ze draaien geen agentsessie |
| **overname** | zie *Overnames* |

### Tabel

```sql
CREATE TABLE trajectory_events (
  id INTEGER PRIMARY KEY,
  source TEXT NOT NULL, source_id TEXT NOT NULL, recorded_at TEXT NOT NULL,
  trajectory_id INTEGER NOT NULL REFERENCES trajectories(id),
  run_id INTEGER REFERENCES runs(id),          -- dispatcher: de run van de Vraag of Done, of de run die het antwoord oppakte
  session_id INTEGER REFERENCES sessions(id),  -- interactieve sessie (lezer #375)
  kind TEXT NOT NULL CHECK (kind IN ('question', 'answer', 'done')),
  ref TEXT NOT NULL,                           -- zie hieronder
  at TEXT NOT NULL,                            -- RFC 3339, UTC: created_at van het commentaar, het tijdstip in het transcript, of bij Done het tijdstip van de tick
  CHECK ((run_id IS NULL) <> (session_id IS NULL)),
  CHECK (kind <> 'done' OR run_id IS NOT NULL),
  UNIQUE (source, source_id),
  UNIQUE (trajectory_id, kind, ref)
);
```

`ref` koppelt een antwoord aan zijn vraag:

- dispatcher-vraag: de commentaar-id van de Vraag; het antwoord draagt dezelfde `ref` (de id van de
  Vraag), de id van het antwoordcommentaar staat in `source_id`;
- sessievraag en -antwoord: `<sessie-id>:<tool-use-id>` van de AskUserQuestion (een kaal tool-use-id
  is niet uniek over sessies: transcripten hergebruiken ze);
- Done: het run-id.

Per vraag telt één antwoord, het eerste: `UNIQUE (trajectory_id, kind, ref)` houdt een tweede
antwoord op dezelfde vraag buiten de tabel. Een antwoord zonder opgeslagen vraag wordt wel
geaccepteerd (oude vraag van voor deze migratie); de tijdlijn rekent dan met een schatting.
`RecordEvent` zoekt bij een conflict ook op `(trajectory_id, kind, ref)` en geeft dan het bestaande
id terug met `isNew` onwaar, geen fout: de pusher kan meer dan één `pushanswer`-run per Vraag
starten.

### Schrijffunctie

`telemetry.RecordEvent(ctx, db, EventRecord)` volgens het skelet van de andere schrijvers
(valideren, één eigenaar, `inTx`, `insertOrFind` op de bron). Een tweede aanbieding met dezelfde
`source`/`source_id` verandert niets. Hij maakt het traject niet zelf: de run of sessie moet al aan
een traject hangen, en `trajectory_id` moet daarmee kloppen (anders `ErrTrajectoryMismatch`).

Twee schrijvers:

- **De dispatcher**, bron `dispatch`. Hij leest de protocolcommentaren al met hun `created_at`
  (`protocol.Comment`).
  - Vraag: wanneer een tick een run sluit op `question`. `source_id` = `question:<comment-id>`.
  - Antwoord: wanneer een tick een run start met `trigger_key` `answer:<id>` of `pushanswer:<id>`.
    `source_id` = `answer:<comment-id>`, `run_id` = de nieuwe run. Een herstart van die run (poging 2,
    zelfde `trigger_key`) schrijft niets bij; `oppakken` loopt dan tot `started_at` van de laatste run
    met die `trigger_key`.
  - Done: wanneer een tick voor het eerst ziet dat de sessie van een open run Done meldde;
    `at` = het `now` van die tick. `source_id` = `done:<run-id>`, `ref` = het run-id.
  - Mislukt het schrijven, dan gaat de tick door en meldt hij het; de tijdlijn valt voor dat
    moment terug op een schatting. Een mislukte gebeurtenis wordt niet opnieuw geprobeerd. Elke
    gebeurtenis wordt geschreven nadat de run-rij bestaat (na `runs.Release` of `runs.Take`).
- **De transcriptlezer van #375**: vraag en antwoord per AskUserQuestion. Bron en `source_id` legt
  #375 vast in zijn eigen spec, die nog geschreven moet worden.

**Bestaande schrijver.** `RecordSession` weigert een sessie naar een ander traject te verplaatsen
(of te ontkoppelen) zolang er `trajectory_events` naar de sessie wijzen, zoals `checkRetarget` dat
nu doet voor bevindingen (`internal/telemetry/record.go`), met een eigen fout `ErrSessionHasEvents`
(`checkRetarget` kent nu alleen `ErrSessionHasFindings`).

### Tijdlijn

`telemetry.Timeline(in TimelineInput, now time.Time) TrajectoryTimeline` is een pure functie. De
invoer: de runs en sessies van het traject, zijn gebeurtenissen, en optioneel het tijdstip waarop het
issue sloot. `telemetry.LoadTimeline(ctx, db, trajectoryID, closedAt, now)` haalt de rijen op en roept
haar aan.

**Venster.** Van de vroegste `started_at` van een run of sessie van het traject tot het laatste
einde. Staat er nog iets open (een open run, een open sessie, een onbeantwoorde vraag die nog loopt),
dan tot `now`. Tijd vóór de eerste run of sessie (het issue lag op de backlog) telt niet mee.

**Intervallen per oorzaak.**

| Oorzaak | Interval | Soort |
|---|---|---|
| `actief` | agent-run (geen pusher-rij): `started_at` .. eerste Done van die run, of `ended_at` als er geen Done is. Een run met uitkomst `question` en een opgeslagen Vraag eindigt bij `at` van die Vraag, niet bij `ended_at` (de tick zag de Vraag later) | gemeten |
| `actief` (sessie) | sessie: `started_at` .. `ended_at`, min de vraag-antwoordintervallen erin | gemeten |
| `ci_review` | run: eerste Done .. `ended_at` | gemeten |
| `ci_review` | pusher-rij: `started_at` .. `ended_at` (de hele rij: wachten op pusher, CI en review) | gemeten |
| `mens` | dispatcher: `at` van de Vraag .. `at` van het antwoord; sessie: `at` vraag .. `at` antwoord | gemeten |
| `mens` | dispatcher-vraag zonder opgeslagen vraag of antwoord: `ended_at` van de vraagrun .. `at` van het antwoord als dat is opgeslagen, anders `started_at` van de run die het antwoord oppakte | schatting |
| `oppakken` | `at` van het dispatcher-antwoord .. `started_at` van de run die het oppakte | gemeten |
| `usage` | `ended_at` van een run met `outcome = 'parked'` .. `started_at` van de hervatting met `trigger = 'usage'` | gemeten |
| `onverklaard` | elk moment in het venster dat geen van de rijen hierboven dekt | ontbrekend |

Een pusher-rij valt nooit onder `actief` (alleen onder `ci_review`, en `mens` voor haar eigen Vraag). Een agent-run zonder Done (oude run, of een run die zonder Done eindigde) geeft alleen `actief`, met
de markering *inclusief eventueel CI/review*.

**Onbeantwoorde vraag.** Een vraag zonder antwoord geeft `mens` tot het eerste van: de start van een
volgende run of sessie van het traject, een run met uitkomst `returned`, het sluiten van het issue.
Is er geen van drieën, dan loopt hij tot `now` en is hij *lopend*.

**Open sessie.** Een sessie met `ended_at` leeg geldt als open tot `now` en is *lopend*. Het
afsluiten van een sessie is werk van #375 (nu roept niets `RecordSession` met een `ended_at` aan).
Omdat een open sessie niet zegt dat iemand werkt, rangschikt `actief` (sessie) onder `ci_review`
(zie *Voorrang*).

**Voorrang.** Valt een moment onder meer oorzaken, dan wint de eerste in deze volgorde: `actief` (run),
`mens`, `usage`, `ci_review`, `actief` (sessie), `oppakken`, `onverklaard`. Wie actief werkt, wacht niet: een run die
loopt terwijl een sessie op een antwoord wacht, telt als actief. Een sessie die een run overlapt telt
het overlappende deel als `actief` van de run, dus één keer. Een schatting verliest van een
gemeten interval van dezelfde oorzaak.

### Overnames

Een overname is een overgang van dispatcher naar mens. Er is er één op elk van deze momenten:

- een run eindigt met uitkomst `returned` (ook om een automatische reden zoals een mislukte start of
  een stille pusher: ook dan moet een mens het oppakken; de reden gaat mee);
- een interactieve sessie van het traject start terwijl er sinds de laatste overname een
  dispatcher-run van het traject liep.

Een `returned` gevolgd door een sessie is dus één overname. Opeenvolgende sessies zonder
dispatcher-run ertussen ook. Een sessie vóór de eerste dispatcher-run is geen overname: dan was de
mens al aan zet.

### Uitkomst

```go
type TrajectoryTimeline struct {
	Window    Interval
	Questions []Moment             // tijdstip, bron (run of sessie), beantwoord ja/nee, schatting ja/nee
	Answers   []Moment
	Takeovers []Moment             // tijdstip, reden (returned-reden, of "sessie")
	ByCause   map[Cause]CauseTotal // duur, en of daar schatting, lopend of "inclusief CI/review" in zit
}
```

Een oorzaak zonder interval staat niet in `ByCause` (ontbrekend), niet als nul. `onverklaard` staat
er wel in zodra het niet leeg is.

### Bekijken

`lighthouse trajectory <issue> [--repo-dir <pad>] [--db <pad>]` print de tijdlijn van één traject:
de vragen, antwoorden en overnames met tijdstip, en per oorzaak de duur met zijn soort
(`gemeten`, `schatting`, `lopend`). Het sluittijdstip van een gesloten issue haalt de opdracht via
`lighthouse forge`. Zelfde vorm en exitcodes als `lighthouse measure-recovery`
(`internal/cli/measure_recovery.go`). *(eigen keuze: het AC zegt "per traject staan"; zonder UI is
dit de plek waar het te zien is.)*

## Tests

- **AC 4.** Een traject met een startrun waarvan de sessie een Vraag stelt, een antwoord, en een
  hervatting (`answer:<id>`) die Done meldt en daarna merget: `mens` = Vraag tot antwoord,
  `oppakken` = antwoord tot hervatting, `actief` = startrun tot zijn einde plus hervatting tot Done,
  `ci_review` = Done tot merge, niets dubbel, som = venster.
- Een vraagrun die de tick pas na de Vraag sluit (`ended_at` > `at` van de Vraag): `actief` eindigt
  bij de Vraag, `mens` loopt vanaf de Vraag; de fixture heeft een echt gat tussen beide.
- Een Done-tijdstip dat vóór het sluiten van de run ligt: `ci_review` is het verschil, niet nul.
- Een push-rij en een merge-run geven `ci_review`, geen `actief`.
- Hetzelfde zonder opgeslagen vraag en antwoord: `mens` is één schatting van `ended_at` van de
  vraagrun tot de hervatting; geen `oppakken`.
- Een run zonder Done: alleen `actief`, gemarkeerd *inclusief eventueel CI/review*.
- `repair:<id>` en `pushanswer:<id>`: repair telt niet als antwoord, pushanswer wel.
- Overlap: een interactieve sessie die een dispatcher-run overlapt telt het overlappende deel één
  keer als `actief`; een vraag binnen de sessie tijdens die run telt als `actief`, daarbuiten als
  `mens`.
- Usage: `parked` gevolgd door `trigger = 'usage'` geeft een `usage`-interval.
- Onbeantwoorde vraag: eindigt bij een volgende run, een `returned` of het sluiten van het issue;
  anders *lopend* tot `now`.
- Een gat zonder bron staat als `onverklaard`, niet als nul en niet als `actief`; een oorzaak zonder
  interval ontbreekt in `ByCause`.
- Overnames: `returned` plus sessie is één; twee sessies zonder run ertussen is één; een sessie vóór
  de eerste run is nul.
- `RecordEvent`: idempotent bij herhaling; een tweede antwoord op dezelfde vraag wordt niet
  opgeslagen; weigert een traject dat niet bij de run of sessie hoort; laat niets achter bij een
  weigering.
- `RecordSession`: weigert een nieuw traject voor een sessie met gebeurtenissen.
- Migratie: `internal/store/migrate_test.go` en `internal/cli/tick_test.go` op `user_version` 12 en
  de nieuwe tabel.
- Dispatcher: een tick die een Vraag, een antwoord of een Done ziet, schrijft één gebeurtenis met het
  tijdstip van het commentaar; een tweede tick schrijft niets bij.

## Documentatie

- `docs/install.md`: `lighthouse trajectory`, naast `measure-recovery`.
- Een fragment in `changelog.d/`.
- Een comment op #375 dat zijn lezer vragen en antwoorden via `RecordEvent` schrijft.

## Aan de bouwer

- De precieze plek in `internal/dispatch` waar de gebeurtenissen geschreven worden (in `apply`, na de
  beslissing), zolang het in de tick gebeurt, de run-id bekend is, en `decide` puur blijft.
- Hoe de dispatcher "voor het eerst ziet dat de sessie Done meldde" detecteert (`done()` in de
  beslissing, of de werkplekstand in `apply`); idempotentie via `done:<run-id>` maakt een herhaling
  onschadelijk.
- De vorm van `Moment`, `Interval` en `CauseTotal`.
