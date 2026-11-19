# Ontwerp: gegevensmodel voor telemetrie in `lighthouse.db`

Datum: 2026-11-19 · Issue: [#373](https://github.com/CorneeldH/lighthouse/issues/373) · Milestone:
Telemetrie (T1 in [het milestone-ontwerp](2026-11-16-zelflerende-lighthouse-design.md))

## Aanleiding en doel

Lighthouse wil per onderdeel van de keten kunnen zeggen wat het toevoegt. Het gaat om reviews en
modellen op meerdere plekken in de keten, niet alleen om de planreview. Vandaag weet `lighthouse.db`
per dispatcher-run alleen trigger, poging, start, eind en uitkomst. Interactieve sessies, waarin vaak
de spec- en planreview lopen, staan er helemaal niet in.

Dit issue legt het gegevensmodel neer waarop de schrijvers van de milestone bouwen: kosten en tokens
(T2), reviewbevindingen (T3), herstel na merge (T4) en menswerk (T5). De eenheid van analyse is het
**traject**: één issue in één repo, met al zijn runs en sessies.

Succes:

- Een traject met twee dispatcher-runs (start en hervatting) en één interactieve sessie is in de
  database één traject, en alle drie verwijzen ernaar.
- De bestaande `runs` werkt na de migratie zoals ervoor, en elke run met een issue heeft een traject.
- Een schrijver die hetzelfde record twee keer aanbiedt, levert één rij op.
- Een sessie zonder koppelbaar issue staat in de database, met de reden.

## Doelen, niet-doelen, anti-doelen

**Doelen.**

- Eén migratie die het model toevoegt en de bestaande runs aan een traject koppelt.
- Schrijffuncties in Go voor elke nieuwe tabel, idempotent.
- De koppelregel voor een interactieve sessie als losse, pure functie.
- `runs.Take` koppelt elke nieuwe run met een issue aan zijn traject.

**Niet-doelen** (eigen issues van de milestone):

- Transcripts inlezen, kosten en tokens bepalen (T2). Of kosten uit het resultaatbericht komen of uit
  een prijstabel, is #375.
- Reviewrecords indexeren (T3), herstel na merge meten (T4), menswerk meten (T5).
- De UI (T6) en de versiemarkering (T7).
- Codex- en OpenCode-sessies als bron (milestone *Explore/exploit*).
- Bewaartermijn en afscherming van gevoelige transcriptinhoud. Dit model slaat geen
  transcriptinhoud op, alleen verwijzingen en afgeleide getallen (zie *Randvoorwaarden*).

**Anti-doelen.** Het werk faalt, ook als het technisch draait, wanneer:

- een tweede opslag ontstaat: inhoud van reviewrecords of transcripts gekopieerd in de database;
- een sessie die niet te koppelen is, stil wegvalt;
- een schrijver die opnieuw draait dubbel telt;
- het model om één onderdeel heen gebouwd is, zodat een tweede review of een ander model een
  schemawijziging vraagt.

## Randvoorwaarden

- **Opslag.** Alles staat in `lighthouse.db`, via een migratie in `internal/store/migrations/`. De
  runner eist een doorlopend nummer (`0010_…`), één transactie per bestand, en zet `user_version`.
- **`foreign_keys(ON)`** staat op elke verbinding (`internal/store/store.go`). Een nieuwe kolom op
  `runs` gaat daarom met `ALTER TABLE … ADD COLUMN`, niet met een tabelherbouw: een herbouw
  (`DROP TABLE runs`) zou rijen raken die naar `runs` verwijzen.
- **Taal van de code.** `[project] code_language = en` geldt voor `internal/**`. Tabel- en
  kolomnamen én opgeslagen waarden zijn Engels, net als de bestaande `runs` (`start`, `resume`,
  `handoff`). De Nederlandse begrippen van het issue staan hieronder naast de Engelse naam.
- **Geen tweede opslag.** Een record dat al als bestand bestaat (reviewrecord, transcript) wordt
  verwezen met pad en inhoudshash (`review.Sum`, sha256), niet gekopieerd.
- **Geen transcriptinhoud.** Het model bevat geen tekst uit transcripts: alleen sessie-id, branch,
  tijden, modelnamen, tellingen en verwijzingen.

## Aanpakken

De vorm van het model, gekozen door de gebruiker:

- **A. Genormaliseerd** *(gekozen)*. Aparte tabellen voor traject, sessie, inzet, verbruik en
  bevinding; `runs` krijgt een trajectverwijzing en een indeling. Elke schrijver weet welke tabel hij
  vult, en de UI telt per onderdeel met gewone SQL. Kost meer schema.
- **B. Eén eventtabel** met soort en JSON-payload. Flexibel bij een nieuw onderdeel, maar elke
  vraag van de UI wordt JSON-uitpluizen, en de checks op indeling en reden verhuizen naar Go.
- **C. Brede kolommen op `runs` plus een sessietabel.** Snelst gebouwd, maar meerdere bevindingen
  per run en verbruik per model passen er niet in; loopt bij T2 en T3 al vast.

De koppeling van een interactieve sessie, gekozen door de gebruiker:

- **Hele sessie** *(gekozen)*. Een sessie hoort bij één traject, of bij geen. Regels op `main`
  tellen niet mee; de issuebranch met de meeste regels wint; een issuenummer uit de transcript is de
  terugval.
- *Segmenten per (cwd, branch)*. Precieser bij een sessie die twee issues raakt, maar een extra
  tabel en toerekening per segment. Niet gekozen: eenvoud weegt zwaarder.
- *Alleen het issuenummer uit de transcript*. Steunt op vrije tekst; alleen als terugval gekozen.

Verder neemt dit ontwerp twee aanbevelingen uit de triage over: de trajectsleutel is repo plus
issue, en idempotentie gaat via `UNIQUE (source, source_id)` met `INSERT … ON CONFLICT DO NOTHING`.

## Het model

### Begrippen

| Issue | Tabel of kolom | Wat |
|---|---|---|
| traject | `trajectories` | één issue in één repo |
| bron | `sessions`, `runs` | een interactieve sessie of een dispatcher-run |
| inzet | `setups` | model, effort, harness en versies per run of sessie |
| indeling | kolom `assignment` | `standard`, `shadow:<component>` of `variant:<dimension>=<value>` |
| bevinding | `findings` | wat een reviewonderdeel meldt, met ernst en besluit |
| uitkomst (tokens en kosten) | `usage` | tokens en kosten per model per run of sessie |
| herkomst | kolommen `source`, `source_id`, `recorded_at` | welke schrijver, welk bronrecord, wanneer |

`standard` is `standaard` uit het issue, `shadow:` is `schaduw:`, `variant:` blijft `variant:`.

### Herkomst en idempotentie

Elke nieuwe tabel draagt drie kolommen:

- `source TEXT NOT NULL`: de schrijver, bijvoorbeeld `claude-code-transcript` of
  `plan-review-record`;
- `source_id TEXT NOT NULL`: een sleutel die de schrijver deterministisch uit het bronrecord
  afleidt, bijvoorbeeld de sessie-id, of `<sessie-id>:<model>` voor verbruik;
- `recorded_at TEXT NOT NULL`: RFC 3339 in UTC, zoals de tijden in `runs`.

Op elke tabel staat `UNIQUE (source, source_id)`. Daarnaast heeft elke tabel waar dat kan een
sleutel op het object zelf: `sessions` op `(harness, session_key)`, `setups` op `run_id` en op
`session_id`. Zo telt een tweede schrijver met een andere bron voor dezelfde sessie of run niet
dubbel.

Een schrijffunctie doet `INSERT … ON CONFLICT DO NOTHING`, zonder conflictdoel, zodat elke
uniekheidsbotsing "niet nieuw" oplevert en geen fout. Ze meldt of de rij nieuw was. Een tweede
aanbieding laat de eerste rij staan, met één uitzondering: `sessions` (zie *Schrijffuncties*).

`runs` blijft het eigen record van de dispatcher: zijn bron is impliciet (`dispatcher`, met `id` als
bronsleutel). `trajectories` is een sleutelrij zonder bron: hij ontstaat bij de eerste run, sessie of
bevinding die naar hem verwijst.

### Tabellen

Schets van de migratie `0010_telemetry.sql`. Kolomvolgorde, indexen en namen van constraints laat dit
ontwerp aan de bouwer, de inhoud niet.

```sql
CREATE TABLE trajectories (
  id INTEGER PRIMARY KEY,
  repo TEXT NOT NULL,            -- dezelfde waarde als runs.repo
  issue INTEGER NOT NULL,
  created_at TEXT NOT NULL,
  UNIQUE (repo, issue)
);

ALTER TABLE runs ADD COLUMN trajectory_id INTEGER REFERENCES trajectories(id);
ALTER TABLE runs ADD COLUMN assignment TEXT NOT NULL DEFAULT 'standard'
  CHECK (assignment = 'standard' OR assignment GLOB 'shadow:?*' OR assignment GLOB 'variant:?*=?*');

CREATE TABLE sessions (           -- alleen interactieve sessies, geen dispatcher-runs
  id INTEGER PRIMARY KEY,
  source TEXT NOT NULL, source_id TEXT NOT NULL, recorded_at TEXT NOT NULL,
  repo TEXT NOT NULL,
  harness TEXT NOT NULL,         -- nu alleen 'claude-code'
  session_key TEXT NOT NULL,     -- de sessie-id van de harness
  transcript_path TEXT NOT NULL, -- verwijzing, geen kopie
  branch TEXT NOT NULL DEFAULT '',   -- de winnende branch, '' als er geen is
  trajectory_id INTEGER REFERENCES trajectories(id),
  link_basis TEXT CHECK (link_basis IN ('branch', 'transcript')),
  unlinked_reason TEXT CHECK (unlinked_reason IN ('main_only', 'no_issue_branch', 'no_issue_number')),
  assignment TEXT NOT NULL DEFAULT 'standard' CHECK (/* als op runs */),
  started_at TEXT NOT NULL, ended_at TEXT,
  CHECK ((trajectory_id IS NULL) = (unlinked_reason IS NOT NULL)),
  CHECK ((trajectory_id IS NULL) = (link_basis IS NULL)),
  UNIQUE (harness, session_key),
  UNIQUE (source, source_id)
);

CREATE TABLE setups (             -- inzet, één per run of sessie
  id INTEGER PRIMARY KEY,
  source TEXT NOT NULL, source_id TEXT NOT NULL, recorded_at TEXT NOT NULL,
  run_id INTEGER REFERENCES runs(id), session_id INTEGER REFERENCES sessions(id),
  model TEXT NOT NULL DEFAULT '', effort TEXT NOT NULL DEFAULT '',
  harness TEXT NOT NULL DEFAULT '', harness_version TEXT NOT NULL DEFAULT '',
  superpowers_version TEXT NOT NULL DEFAULT '', lighthouse_version TEXT NOT NULL DEFAULT '',
  pack_version TEXT NOT NULL DEFAULT '', skills_hash TEXT NOT NULL DEFAULT '',
  CHECK ((run_id IS NULL) <> (session_id IS NULL)),
  UNIQUE (run_id), UNIQUE (session_id),
  UNIQUE (source, source_id)
);

CREATE TABLE usage (              -- tokens en kosten per model per run of sessie
  id INTEGER PRIMARY KEY,
  source TEXT NOT NULL, source_id TEXT NOT NULL, recorded_at TEXT NOT NULL,
  run_id INTEGER REFERENCES runs(id), session_id INTEGER REFERENCES sessions(id),
  model TEXT NOT NULL,
  input_tokens INTEGER NOT NULL DEFAULT 0, output_tokens INTEGER NOT NULL DEFAULT 0,
  cache_read_tokens INTEGER NOT NULL DEFAULT 0, cache_write_tokens INTEGER NOT NULL DEFAULT 0,
  cost REAL,                     -- NULL: onbekend
  cost_kind TEXT CHECK (cost_kind IN ('estimate', 'quota', 'actual')),
  CHECK ((run_id IS NULL) <> (session_id IS NULL)),
  CHECK ((cost IS NULL) = (cost_kind IS NULL)),
  UNIQUE (source, source_id)
);

CREATE TABLE findings (           -- bevindingen; de recordbestanden blijven de bron
  id INTEGER PRIMARY KEY,
  source TEXT NOT NULL, source_id TEXT NOT NULL, recorded_at TEXT NOT NULL,
  trajectory_id INTEGER NOT NULL REFERENCES trajectories(id),
  run_id INTEGER REFERENCES runs(id), session_id INTEGER REFERENCES sessions(id),
  component TEXT NOT NULL,       -- bijvoorbeeld 'spec_review', 'plan_review', 'task_review'
  finding_key TEXT NOT NULL,     -- de id in het record, bijvoorbeeld 'r1-f2'
  severity TEXT NOT NULL DEFAULT '',
  decision TEXT CHECK (decision IN ('adopted', 'rejected', 'left_open')),  -- NULL: nog geen besluit
  record_path TEXT NOT NULL, record_sum TEXT NOT NULL,
  CHECK (run_id IS NULL OR session_id IS NULL),
  UNIQUE (source, source_id)
);
```

De ernst blijft vrije tekst: elk reviewonderdeel heeft zijn eigen schaal, en T3 kent die. Het besluit
heeft de drie waarden uit het issue.

Een bevinding heeft geen eigen indeling. Ze hoort bij een run, een sessie of het traject, en de
indeling daarvan geldt.

### Bestaande runs koppelen

In dezelfde migratie, na de nieuwe tabellen:

```sql
INSERT INTO trajectories (repo, issue, created_at)
  SELECT repo, issue, min(started_at) FROM runs WHERE issue IS NOT NULL GROUP BY repo, issue;
UPDATE runs SET trajectory_id =
  (SELECT t.id FROM trajectories t WHERE t.repo = runs.repo AND t.issue = runs.issue)
  WHERE issue IS NOT NULL;
```

Een geplande run (`issue IS NULL`, `schedule` gezet) heeft geen issue en houdt
`trajectory_id IS NULL`. Dat is geen ontbrekende koppeling: gepland werk is geen traject.

### Nieuwe runs

`runs.Take` (`internal/runs/runs.go`) doet bij een claim met een issue, in dezelfde transactie, een
`INSERT … ON CONFLICT (repo, issue) DO NOTHING` op `trajectories` en zet `trajectory_id` op de nieuwe
run. `assignment` krijgt de standaardwaarde; geen enkele code in deze milestone schrijft iets anders.

### De koppelregel voor een interactieve sessie

Een pure functie in een nieuw pakket `internal/telemetry`, zonder bestand- of databasetoegang. Ze
krijgt per transcriptregel de branch en eventueel een issuenummer dat de schrijver uit die regel
haalde, en geeft een issue met basis, of een reden.

1. Voor de branchtelling telt een regel met branch `main` (de standaardbranch,
   `forge.default_branch`) of een lege branch niet mee.
2. Van de overige regels haalt de functie het issuenummer uit de branchnaam met het patroon
   `^issue-(\d+)(-|$)`. Ook een kale `issue-<N>` telt: de dispatcher maakt die zelf als de titel geen
   slug geeft (`dispatch/decide.go`, `issueBranch`). De functie telt per issuenummer, niet per branch:
   twee branches van hetzelfde issue tellen samen. Het issue met de meeste regels wint, basis
   `branch`. Bij gelijkspel wint het issue dat het laatst voorkomt.
3. Valt er geen issuebranch over, dan wint het issuenummer dat de schrijver het vaakst uit de
   transcript haalde, basis `transcript`. Hier tellen alle regels mee, ook die op `main`: zo krijgt
   een sessie die `/lighthouse-start-issue 373` vanuit de hoofdcheckout draaide en daar bleef, toch
   haar traject. Bij gelijkspel het laatst geziene.
4. Anders: geen traject, met reden.
   - `main_only`: elke regel stond op de standaardbranch, en er was geen issuenummer;
   - `no_issue_branch`: er waren andere branches, geen ervan een issuebranch (bijvoorbeeld
     `fix-<slug>`), en er was geen issuenummer;
   - `no_issue_number`: geen bruikbare branch, ook geen issuenummer.

Hoe een issuenummer uit een transcriptregel komt (Skill-args, slashcommando), bepaalt T2: de functie
krijgt het al geëxtraheerd. Het patroon `^issue-(\d+)-` is nu op drie plekken losse stringlogica
(`dispatch/decide.go`, `worktreeaudit/rows.go`, `cli/triage_gather.go`). Dit issue voegt één
gedeelde parser toe in `internal/telemetry` en laat die drie plekken ongemoeid; vervangen mag, hoeft
niet.

De database bewaakt dat `trajectory_id`, `link_basis` en `unlinked_reason` samen kloppen. Later
koppelen gaat via een nieuwe aanbieding van dezelfde sessie (zie *Schrijffuncties*). Een handmatige
koppeling door een mens bouwt dit issue niet; de kolommen laten haar later toe.

### Dispatcher-runs zijn geen sessies

Een dispatcher-run is ook een Claude Code-sessie met een transcript (`runs.session_id`). Die hoort
niet in `sessions`: de schrijver van T2 slaat een sessie-id over die in `runs.session_id` staat. Een
hervatting met dezelfde sessie-id als de start is T2's zorg bij het toerekenen van tokens.

### Schrijffuncties

In `internal/telemetry`, één per tabel: `RecordSession`, `RecordSetup`, `RecordUsage` en
`RecordFinding`. Elke functie:

- neemt een `*sql.DB` (of `*sql.Tx`) en een struct met de velden;
- maakt het traject aan als dat nog niet bestaat (`trajectories`, op repo en issue);
- doet `INSERT … ON CONFLICT DO NOTHING`;
- geeft terug of de rij nieuw was, en de id van de rij, ook als die al bestond.

**Uitzondering: `RecordSession`.** Een transcript groeit zolang de sessie loopt. Een eerste aanbieding
kan dus `main_only` zijn, en een latere, na de worktree, een issuebranch. Bij een tweede aanbieding van
dezelfde sessie (`harness`, `session_key`) werkt `RecordSession` daarom de afgeleide velden bij:
`branch`, `trajectory_id`, `link_basis`, `unlinked_reason`, `ended_at` en `recorded_at`. Herkomst
(`source`, `source_id`), `started_at`, `transcript_path` en `assignment` blijven staan. Dezelfde
aanbieding twee keer geeft nog steeds één rij met dezelfde inhoud: de functie is idempotent, ze
overschrijft alleen wat uit de bron volgt. Een sessie die gekoppeld was, kan zo ook weer ontkoppeld
raken als de bron dat zegt.

**`RecordFinding`** neemt repo en issue en maakt daar het traject van. Geeft de aanroeper ook een run
of sessie mee, dan moet die naar hetzelfde traject verwijzen; anders weigert de functie met een fout.
Een bevinding op een ongekoppelde sessie wordt ook geweigerd: de sessie is eerst te koppelen, of de
bevinding gaat zonder sessie, alleen op het traject.

Er komt geen schrijver die echte bronnen leest; dat zijn T2 tot en met T5.

## Tests

Volgens `/lighthouse-test-standard`; elke test faalt als het gedrag breekt.

- **Migratie behoudt runs.** Volgens het patroon van `TestMigrate0008KeepsRuns`: een database op
  versie 9 met een issue-run en een geplande run, dan migreren. Beide runs bestaan nog met dezelfde
  velden. De issue-run heeft een traject met zijn repo en issue. De geplande run heeft
  `trajectory_id IS NULL`, en beide hebben `assignment = 'standard'`.
- **Teller en tabellen.** `user_version` wordt 10 in `TestMigrate…` en `TestMigrationPushedDefault`.
  De tabellenlijst in `migrate_test.go` krijgt de vijf nieuwe tabellen. `TestMigrateTwice` blijft
  groen.
- **Één traject (AC 6).** Twee runs via `runs.Take` voor hetzelfde issue (mode `start`, daarna
  `resume`) en één sessie via `RecordSession` met de koppelregel op een branch `issue-<n>-x` geven
  samen één rij in `trajectories`, en alle drie verwijzen ernaar.
- **Idempotent.** Elke `Record…` twee keer met hetzelfde `(source, source_id)` geeft één rij; de
  tweede aanroep meldt "niet nieuw" en dezelfde id. Een tweede bron voor dezelfde sessie of run
  (andere `source`, zelfde object) geeft ook "niet nieuw", geen fout.
- **Sessie groeit.** `RecordSession` eerst met alleen main-regels (`main_only`), daarna dezelfde
  sessie met ook regels op `issue-<n>-x`: één rij, nu gekoppeld aan traject `<n>` met basis `branch`.
- **Bevinding past bij haar traject.** `RecordFinding` met een run van een ander issue, of met een
  ongekoppelde sessie, geeft een fout en schrijft niets.
- **Ongekoppelde sessie.** Een sessie met alleen regels op `main` staat in `sessions` met
  `unlinked_reason = 'main_only'` en zonder traject. De database weigert een sessie met traject én
  reden, en een sessie met geen van beide.
- **Koppelregel.** Tabelgedreven: alleen main zonder issuenummer (`main_only`); alleen main met
  issuenummer (`transcript`); main plus één issuebranch (die wint, ongeacht hoeveel main-regels);
  kale `issue-<N>`; twee issuebranches van verschillende issues (meeste regels wint; gelijkspel
  laatste); twee branches van hetzelfde issue (tellen samen); alleen `fix-<slug>` met en zonder
  issuenummer; geen branch, wel issuenummer.
- **Indeling.** De database weigert `assignment = 'foo'` en accepteert `shadow:plan_review` en
  `variant:model=opus`.
- **Verwijzen, niet kopiëren.** `findings` heeft geen kolom voor recordinhoud. Een test op het schema
  (`PRAGMA table_info`) bewaakt de kolommen van `findings` en `sessions`, zodat een later toegevoegde
  inhoudskolom opvalt.

Verificatie: `go test -race ./internal/store/... ./internal/runs/... ./internal/telemetry/...
./internal/cli/... ./internal/dispatch/...`.

## Documentatie

`docs/tick.md` en `docs/install.md` noemen `lighthouse.db`. Een korte paragraaf beschrijft het
traject, de nieuwe tabellen en de koppelregel, in het document waar de runs-tabel nu uitgelegd wordt.
Plus een fragment in `changelog.d/`.

## Aan de bouwer

Vastgelegd door de gebruiker: het genormaliseerde model, de koppeling per hele sessie met de
koppelregel hierboven, runs zonder issue zonder traject.

Eigen keuzes in dit ontwerp, te corrigeren:

- Herstel na merge (T4) en menswerk (T5) krijgen hun tabellen in hun eigen migratie. Hun vorm hangt
  af van wat ze meten, en dat is daar nog niet ontworpen. Dit model geeft ze het patroon: trajectsleutel,
  `source`/`source_id`/`recorded_at`, verwijzen in plaats van kopiëren.
- Opgeslagen waarden zijn Engels (`standard`, `adopted`, …) vanwege `code_language`.
- Bij gelijkspel in de koppelregel wint het laatst geziene; de telling gaat per issuenummer.
- `issue-<N>` zonder slug is ook een issuebranch.
- Een issuenummer uit de transcript telt ook op `main`; `main_only` betekent "alleen main en geen
  issuenummer".
- Een tweede aanbieding overschrijft niet, behalve bij `sessions`: daar werkt ze de afgeleide velden
  bij, omdat een transcript groeit.
- Een tweede bron voor hetzelfde object telt niet dubbel (`UNIQUE` op het object).
- Een bevinding moet bij het traject van haar run of sessie horen; anders weigert de schrijver.
- Handmatig koppelen door een mens zit niet in dit issue.
- De koppelregel krijgt issuenummers al geëxtraheerd; het lezen van transcripts is T2.

Aan de bouwer: kolomvolgorde, indexen buiten de genoemde `UNIQUE`'s, namen van Go-types en de precieze
handtekening van de schrijffuncties.
