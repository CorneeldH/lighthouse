# Ontwerp: inzet, lane, kosten en tokens per run en per interactieve sessie

Issue #375 (T2 van milestone *Telemetrie: wat voegt elk onderdeel toe*). Bouwt op het gegevensmodel
van #373: [`2026-11-19-telemetrie-gegevensmodel-design.md`](2026-11-19-telemetrie-gegevensmodel-design.md).
Raakt #378: [`2026-11-24-menswerk-wachttijd-design.md`](https://github.com/CorneeldH/lighthouse/blob/issue-378-menswerk-wachttijd/docs/developer/planning/specs/2026-11-24-menswerk-wachttijd-design.md)
(nog op zijn branch). Herkomst: [`2026-11-16-zelflerende-lighthouse-design.md`](2026-11-16-zelflerende-lighthouse-design.md),
rij T2.

## Aanleiding en doel

Lighthouse weet per run en per interactieve Claude Code-sessie niet welk model draaide, met welke
skills en versies, hoeveel tokens het gebruikte en wat dat kostte. Daardoor is de kernvraag van de
milestone, *wat voegt een onderdeel toe en wat kost het*, niet te beantwoorden. In paia bepaalde de
plangrootte de kosten sterker dan de lane (SDD tot 4 taken $8,00, 5 tot 8 taken $17,39, boven 8
taken $54,01). Een vergelijking hoort dus binnen een grootteklasse, en daarvoor moet het aantal
plantaken bij het traject staan.

Het gegevensmodel van #373 staat er al: `sessions`, `setups`, `usage` en `trajectories`, met
idempotente schrijvers. Niemand schrijft er nog in. Dit issue levert de lezer die ze vult.

## Wat het transcript draagt (gemeten)

Een Claude Code-transcript (`~/.claude/projects/<project>/<sessie>.jsonl`, subagents in
`<sessie>/subagents/*.jsonl`) draagt per assistant-regel:

- `message.model` en `message.usage`: `input_tokens`, `output_tokens`, `cache_read_input_tokens`,
  `cache_creation_input_tokens`, gesplitst in `cache_creation.ephemeral_5m_input_tokens` en
  `ephemeral_1h_input_tokens`;
- `effort`, `version` (de Claude Code-versie), `gitBranch`, `cwd`, `sessionId`, `timestamp`.

Eén API-bericht staat over meerdere regels verspreid, één per inhoudsblok, met dezelfde `usage`.
In een gemeten transcript stonden 17 assistant-regels voor 8 unieke `message.id`'s. Wie per regel
optelt, telt dubbel.

`total_cost_usd` staat **niet** in het transcript. Het staat alleen in de envelope van
`claude -p --output-format json`, en zo draaien runs en sessies niet. Wel staan er regels van het
type `cost-state` met `totalCostUSD` en een `modelUsage` per model (ook modellen zonder
assistant-regel, en model-id's met `[1m]`). Ze staan niet in elk transcript (op de meetmachine in
ongeveer de helft), hun gedrag bij een hervatting is niet gemeten, en ze zijn niet per runvenster te
splitsen. Ze zijn daarom geen bron *(eigen keuze na spec-review, r1-f9; keuze a van de gebruiker
blijft staan)*; de bouwer mag ze als kruiscontrole lezen.
Andere regeltypen (`api-request-blob`, `api-request`, `attachment`, `cost-state`) tellen niet mee in
de regels van de lezer: alleen `type` `user` en `assistant`.

## Doelen, niet-doelen, anti-doelen

**Doelen.**

- Per run en per interactieve sessie één `setups`-rij met de volledige inzet: model en effort,
  harness en versie, superpowers-, lighthouse- en packversie, skillhash en lane. Een waarde zonder
  bron is `NULL` (ontbrekend), nooit `''`.
- Per run en per sessie één `usage`-rij per model: invoer, uitvoer, cache-read, cache-write en een
  kostenraming. Subagents tellen mee bij de run of sessie die ze startte.
- Het aantal plantaken per traject.
- Verzamelen zonder losse opdracht, en terugverwerken van bestaande transcripts met hetzelfde
  commando.
- Vragen en antwoorden (AskUserQuestion) uit interactieve sessies naar `RecordEvent` van #378
  (tabel `trajectory_events`), in dezelfde gang door het transcript.
- Het afsluiten van een sessie (`sessions.ended_at`), dat #378 aan dit issue overlaat.

**Niet-doelen.**

- Werkelijk gefactureerde kosten. Er is geen bron: de gebruiker draait op een abonnement, en de
  envelope met `total_cost_usd` bestaat alleen bij `claude -p`. `cost_kind = 'actual'` blijft in het
  schema voor later, maar dit issue schrijft het niet. (`cost-state` in het transcript is geen
  gefactureerd bedrag en geen bron; zie *Wat het transcript draagt*.)
- Tokens van modellen die alleen in `cost-state` staan, zoals zijvragen van Claude Code zelf zonder
  assistant-regel. `usage` is één rij per model dat in de assistant-regels voorkomt; die zijvragen
  zijn een bekende ondertelling *(eigen keuze, r1-f9)*.
- Quotumaandeel per sessie (`cost_kind = 'quota'`). Toerekenen aan één sessie is onbetrouwbaar als
  sessies parallel lopen *(gekozen door de gebruiker: alleen de dollarraming)*.
- De UI (#379) en de vergelijking Native tegenover SDD zelf (milestone *Explore/exploit*).
- Codex- en OpenCode-transcripts (milestone-no-go).
- Bewaartermijn en afscherming van transcriptinhoud. Dit issue slaat geen transcriptinhoud op,
  alleen getallen, versies, hashes en verwijzingen.

**Anti-doelen.** Het is mislukt, ook als het werkt, als:

- tokens dubbel tellen: per inhoudsblok, tussen hoofdtranscript en subagent, of tussen een run en
  een sessie over hetzelfde transcript;
- een ontbrekende waarde als `''`, `0` of als een geschatte bedrag zonder markering verschijnt;
- een sessie die na de eerste verzamelslag doorliep, blijft staan op de stand van die eerste slag;
- het starten van een sessie merkbaar trager wordt door het verzamelen;
- er transcripttekst in `lighthouse.db` komt.

## Randvoorwaarden

- Eén opslag: `lighthouse.db`. De transcripts blijven de bron; de database verwijst ernaar
  (`sessions.transcript_path`).
- Schrijvers zijn idempotent via `source`/`source_id` (`Origin` in `internal/telemetry/record.go`).
- Migratie `0012` (of het volgende vrije nummer als #378 eerder mergt), in één transactie.
- `SetupRecord` krijgt nullable velden (`*string`); een ontbrekende waarde wordt `NULL` geschreven,
  niet `''` (nu schrijft `RecordSetup` ze zonder `nullString`).
- Alleen Claude Code als harness.

## Aanpakken

**Kostenbron** *(gekozen door de gebruiker: a)*.

- **a. Tokens × een prijstabel in de repo, altijd `cost_kind = 'estimate'`.** Vergelijkbaar met de
  paia-cijfers en altijd beschikbaar.
- b. Quotumaandeel van het 5-uurs- of weekvenster als `quota`. Dichter bij wat de gebruiker
  betaalt, maar niet eerlijk toe te rekenen bij parallelle sessies.
- c. Beide. Meer werk zonder een vraag die het nu beantwoordt.

**Wanneer verzameld wordt** *(gekozen door de gebruiker: c)*.

- a. Eén verzamelcommando plus een geplande `tick`-job. Een gebruiker zonder dispatcher moet zelf
  draaien.
- b. Een `SessionEnd`-hook. Vuurt niet bij een gekilde sessie; je hebt toch een inhaalcommando nodig.
- **c. a plus een inhaalslag bij `SessionStart`, op de achtergrond, alleen voor gewijzigde
  transcripts.** Dekt ook gebruikers zonder dispatcher, zonder te leunen op een betrouwbaar einde.

**Bron van de inzet** *(gekozen door de gebruiker: 1)*.

- **1. Het transcript is de enige bron.** Runs, sessies en oude transcripts lopen door één codepad;
  terugverwerken komt vanzelf. Wat het transcript niet draagt, blijft ontbrekend.
- 2. De dispatcher schrijft de inzet bij de lancering, de collector de tokens. Exacter voor de
  versies van nieuwe runs, maar twee schrijvers voor één rij en oude runs hebben die velden nooit.

## Het model

### Migratie 0012

- `setups`: de inzetvelden `model`, `effort`, `harness`, `harness_version`, `superpowers_version`,
  `lighthouse_version`, `pack_version`, `skills_hash` worden nullable zonder default; nieuw veld
  `lane TEXT CHECK (lane IN ('sdd', 'native'))`, nullable. SQLite kent geen `ALTER COLUMN`: de tabel
  wordt herbouwd (`CREATE TABLE setups_new … ; INSERT … SELECT NULLIF(kolom, '') …; DROP; RENAME`).
  Er zijn nog geen schrijvers, dus de tabel is in de praktijk leeg; de `NULLIF` vangt rijen uit tests
  of handwerk.
- `trajectories`: nieuw veld `plan_tasks INTEGER` (nullable; `NULL` = geen plan gevonden) en
  `plan_path TEXT` (de verwijzing waaruit geteld is).
- `usage` verandert niet. `cache_write_tokens` blijft één getal; de splitsing 5m/1h telt alleen mee
  in de kostenraming.

### De lezer: `internal/telemetry/transcript` *(eigen keuze voor de plek)*

Een pure parser: van de regels van één hoofdtranscript plus zijn subagentbestanden naar één
`Reading`:

```go
type Reading struct {
	SessionKey string               // sessionId
	Started, Ended time.Time        // eerste en laatste timestamp
	Lines      []telemetry.Line     // branch en issuenummer per regel, voor telemetry.Link
	Usage      map[string]Tokens    // per model, ontdubbeld
	Setup      Setup                // inzet, velden als *string (nil = ontbrekend)
	Questions  []Question           // AskUserQuestion: tool-use-id, tijdstip, antwoordtijdstip of nil
	Slices     []Slice              // per regel: tijdstip, zodat Usage per tijdvenster op te splitsen is
}
```

Regels:

- **Ontdubbelen.** Tokens tellen één keer per `message.id`, over hoofdtranscript en subagents samen.
  Een regel zonder `message.id` valt terug op `requestId`; zonder beide telt de regel op zichzelf.
- **Subagents** (`isSidechain` of een bestand onder `subagents/`) tellen bij dezelfde eigenaar, per
  eigen model.
- **Model** in `setups`: het model met de meeste uitvoertokens op niet-sidechain-regels.
  `<synthetic>` en lege modellen tellen niet.
- **Effort**: de laatste niet-lege `effort` op een niet-sidechain-regel.
- **Harness** = `claude-code`; **harness_version** = de laatste `version`.
- **Superpowers-versie**: uit een geladen skillpad van de vorm `…/superpowers/<versie>/skills/…`
  (het bericht *Base directory for this skill*). Meerdere versies in één sessie: de laatste.
  Geen superpowers-skill geladen: ontbrekend.
- **Skillhash**: sha256 over de gesorteerde paren *(skillnaam, sha256 van de geladen skilltekst)*
  van alle skills die in de sessie laadden. De skilltekst wordt eerst genormaliseerd: de regel
  *Base directory for this skill: <pad>* en de staart `ARGUMENTS: …` gaan eraf, anders verschilt de
  hash per worktree en per aanroep *(eigen keuze na spec-review, r1-f12)*. Geen skill geladen: ontbrekend. De tekst zelf wordt
  niet opgeslagen.
- **Lane**: `sdd` als `superpowers:subagent-driven-development` laadde, `native` als
  `superpowers:executing-plans` laadde (in de dispatcher heet dat de native lane), anders ontbrekend.
  Laadden beide, dan wint de laatste. Lane is de geladen skill, niet het dispatcherverdict: een
  mens die zelf `executing-plans` draait, krijgt ook `native`. Wie Native tegenover SDD vergelijkt,
  filtert op runs of op sessies *(r1-f16)*.
- **Lighthouse- en packversie**: het transcript draagt ze niet. Zie *SessionStart-hook*.
- **Vragen**: elke `AskUserQuestion`-tool-use met zijn id en tijdstip; het antwoord is de
  `tool_result` met hetzelfde id. Alleen tijdstippen, geen tekst.
- **Issuenummer per regel** (de regel die #373 aan T2 laat) *(eigen keuze)*: het eerste getal in
  de `ARGUMENTS:`-staart van een geladen skill (`ARGUMENTS: 433`) en het eerste getal in de
  argumenten van een slashcommando (`<command-args>`), als dat getal vooraan staat. Een `#<n>` in
  vrije tekst telt niet: een sessie noemt veel issues die ze niet uitvoert.

### Kostenraming: `internal/telemetry/prices.toml` *(eigen keuze voor de plek)*

Een ingebedde TOML met per model-id de prijs per miljoen tokens voor invoer, uitvoer, cache-read,
cache-write 5m en cache-write 1h, plus `source` (de URL van de prijspagina) en `as_of` (datum).
Kosten = som over de vijf soorten. Een model dat niet in de tabel staat, krijgt `cost = NULL` en
`cost_kind = NULL`; de tokens staan er wel. Een prijswijziging is een nieuwe regel met een nieuwe
`as_of`; de raming van een bericht gebruikt de prijs die gold op zijn `timestamp`.

### Toewijzen: run of sessie

Per hoofdtranscript, op `sessionId`:

1. **Run.** Staat de sessie-id in `runs.session_id`, dan horen de regels bij die run. Delen meerdere
   runs dezelfde sessie-id (een hervatting die de sessie voortzet), dan krijgt elke run de regels
   tussen zijn `started_at` en `ended_at`; een regel buiten elk venster gaat naar de run die er het
   dichtst vóór startte, en een regel vóór de eerste start naar de eerste run. Zo telt een hervatting bij hetzelfde traject (`runs.trajectory_id`) en
   telt geen bericht twee keer. Een run krijgt geen `sessions`-rij. Dat geldt voor de tokens. Voor de inzet geldt per veld *(eigen keuze, r1-f1)*:
   `model`, `effort` en `harness_version` komen uit de regels in het venster van de run;
   `lane`, `skills_hash` en `superpowers_version` uit alle regels van het begin van het transcript
   tot het einde van het venster, want een hervatting laadt zelden opnieuw een skill terwijl de
   skills van de eerdere run nog in context staan. Het zijbestand (zie *Wanneer het draait*) levert
   de waarneming die bij het begin van het venster hoort.
2. **Sessie.** Anders wordt het een interactieve sessie: `RecordSession` met `telemetry.Link`
   (#373). Een sessie zonder issuebranch en zonder issuenummer in het transcript staat in
   `sessions` met `unlinked_reason` en zonder traject. Dat beantwoordt de open milestonevraag over
   sessies op `main`.

**Einde van een sessie** *(eigen keuze)*. `ended_at` wordt de laatste timestamp zodra het
hoofdbestand en zijn subagentbestanden minstens een uur niet gewijzigd zijn (mtime). `--since-last`
slaat daarom een sessie met `ended_at IS NULL` nooit over op grond van versheid alleen: zodra de
laatste mtime een uur oud is, wordt ze herbezocht en afgesloten (r1-f2). Daarvoor
blijft `ended_at` leeg en geldt de sessie voor #378 als lopend. Groeit een afgesloten transcript
weer (een hervatte sessie), dan maakt de volgende slag `ended_at` weer leeg of schuift hem op.

### Schrijven

- **Eén transactie per transcript.** Sessie, setups, usage en gebeurtenissen van één transcript
  worden atomair geschreven (de `Record*`-functies nemen een open transactie, of een variant ervan),
  zodat een vroeg gezet `recorded_at` nooit een half geschreven transcript als klaar markeert
  (r1-f4).
- **`RecordSetup` en `RecordUsage` werken bij.** Nu laat een tweede aanbieding de eerste rij staan.
  Dat wordt: dezelfde eigenaar (en bij `usage` hetzelfde model) werkt de afgeleide velden en
  `recorded_at` bij, zoals `RecordSession` dat al doet. Zo groeit een sessie mee met elke slag.
  `source`, `source_id` en de eigenaar blijven staan.
- **`source_id`** is deterministisch: `transcript:<sessionId>` voor setup en sessie,
  `transcript:<sessionId>:<model>` voor usage, met een `:<run-id>`-achtervoegsel als een transcript
  over runs verdeeld is. Bron `transcript`.
- **Vragen en antwoorden**: per vraag van een gekoppelde sessie `RecordEvent` (#378) met
  `kind = 'question'`, en zodra het antwoord er is `kind = 'answer'`, beide met het tool-use-id als
  `ref`. Bron `transcript`, `source_id` = `transcript:<sessionId>:<tool-use-id>:<kind>`. Een run
  schrijft geen vragen; die lopen via de dispatcher (#378).
- **Koppeling vast** *(eigen keuze)*. #378 laat `RecordSession` weigeren een sessie te verplaatsen
  zolang er gebeurtenissen naar wijzen. Daarom schrijft de collector vragen en antwoorden pas als de
  sessie is afgesloten (`ended_at` gezet): dan ligt de koppeling vast. Weigert `RecordSession` toch
  een verplaatsing (een afgesloten sessie die weer groeit en nu een andere branch heeft), dan
  houdt de sessie haar oude koppeling. De collector meldt dat als overgeslagen met de reden, zonder
  exitcode 1.
- **Plantaken.** Een plan hoort bij een traject als een gekoppelde sessie of run van dat traject
  een bestand onder `lighthouse config paths.plans` schreef (Write of Edit in het transcript). Valt
  dat niet vast te stellen, dan een planbestand waarvan de naam `issue-<nr>-` bevat. Het aantal
  taken is het aantal koppen `## Task <n>`, `### Task <n>`, `## Taak <n>` of `### Taak <n>` in de
  huidige versie van dat bestand (werkboom als het bestaat, anders de default branch). Meerdere
  plannen: het laatst geschreven. Geen plan: `NULL`.

### Verzamelen: `lighthouse telemetry collect`

```text
lighthouse telemetry collect [--repo-dir <pad>] [--db <pad>] [--since-last] [--dry-run]
```

- Zoekt de transcripts van de repo en zijn worktrees (r1-f11). Niet op het mapnaamvoorvoegsel van
  `kpi.sessionFiles` (`--claude-worktrees-`): worktrees onder de kenn-forge-root delen dat niet met
  de clone, en een verwijderde worktree staat niet meer in `git worktree list`. Wel: alle
  projectmappen onder `~/.claude/projects`, gefilterd op de `cwd` van hun regels (onder de repo-dir,
  onder `lighthouse config worktree.root`, of onder een worktree van de repo). De precieze regel is
  aan de bouwer. Verwerkt elk hoofdtranscript.
- Leest elk bestand tot de grootte die vóór het lezen gemeten is, en zet `recorded_at` op het
  tijdstip vóór het lezen. Een regel die Claude tijdens het lezen toevoegt, geeft het bestand dan een
  mtime ná `recorded_at` en wordt in de volgende slag meegenomen (r1-f5).
- `--since-last` slaat een transcript over waarvan de `sessions`- of `setups`-rij een `recorded_at`
  heeft die niet ouder is dan de mtime van het hoofdbestand en van zijn subagentbestanden, behalve
  een sessie met `ended_at IS NULL` (zie *Einde van een sessie*). Er komt geen aparte statustabel.
- `--dry-run` schrijft niets: geen database, geen migratie, geen tick-staat, geen zijbestand. Het
  opent de database alleen-lezen zonder migratie, en meldt wat het zou doen (r1-f8).
- Print per overgeslagen of niet te lezen transcript één regel met de reden (zelfde vorm als
  `measure-recovery`). Een transcript dat niet te parsen is, wordt niet opgeslagen: er is niets om
  op te slaan behalve de reden, en die staat in de uitvoer.
- Exitcodes zoals `measure-recovery` (`internal/cli/measure_recovery.go`): 0 klaar, 1 een
  verwerkings-, config-, git- of databasefout (ook een deels mislukte slag), 2 verkeerd gebruik van
  de opdrachtregel (r1-f3).
- Terugverwerken is dit commando zonder `--since-last`.

### Wanneer het draait

- **In `tick`** *(eigen keuze)*. Het `[[schedule]]`-raamwerk start alleen agentjobs (Gate, Launch,
  Stage, Check, Finish), en een collector zonder agent past daar niet in. Daarom roept `tick` zelf
  `collect --since-last` aan, hooguit eens per uur. Het tijdstip van de laatste slag staat in een
  bestand onder `LIGHTHOUSE_STATE_DIR`. De tick leest het transcript van een run niet via het
  bestandssysteem en niet via `Adapter.Transcript` (dat geeft alleen een samenvatting zonder
  `message.usage`), maar via `Adapter.TranscriptDelta`
  (`internal/workplace/kennforge/transcript.go`), dat de ruwe bestandsdelen van hoofdtranscript en
  subagents uit de container geeft. Voor een run op een andere node en voor de lokale adapter is
  de ruwe-bytes-bron nog niet vastgelegd: zie *Open voor het plan*. De collector leest vanaf
  offset 0 en parst de regels zelf: met het software-profiel draait claude in de gedeelde devcontainer en staan de
  transcripts in een volume dat de host niet leest (`internal/workplace/kennforge/transcript.go`,
  #237). Een run waarvan het transcript onbereikbaar is (de werkplek is al weg en er is geen spiegel),
  staat in de uitvoer met de reden (r1-f10). Dit draait alleen waar een dispatcher loopt en vraagt geen
  regel in `lighthouse.toml`. Een fout in het verzamelen laat de tick doorgaan en staat in zijn
  uitvoer.
- **SessionStart-hook `telemetry-collect`** in `pack/core/hooks/`. Start
  `lighthouse telemetry collect --since-last` losgekoppeld op de achtergrond en keert meteen terug.
  Blokkeert nooit en opent zelf geen database. Hij start geen `collect` in een controller-run
  (`LIGHTHOUSE_CONTROLLER_RUN` gezet): in de devcontainer is er geen host-`lighthouse.db`, en een
  lege tweede database zou elke run als sessie opnemen (`store.DefaultPath`,
  `internal/store/store.go`) *(eigen keuze, r1-f10)*. Een host-gebruiker zonder dispatcher krijgt
  zijn database door de eerste `collect`; de hook zelf maakt er geen. Het zijbestand schrijft hij
  wel altijd, ook in een controller-run: `LIGHTHOUSE_STATE_DIR` is daar de gekoppelde map. Bij een fout schrijft hij één regel naar zijn log en
  meldt `COULD NOT CHECK` alleen in dat log, niet in de sessie.
- **Lighthouse- en packversie** *(eigen keuze)*. Dezelfde hook kent de sessie-id uit zijn invoer,
  de eigen binaryversie en `version` uit `.lighthouse/manifest.toml`. Hij schrijft die in een klein
  zijbestand, `<state>/<sessie-id>.observations.json` (afwijking bij de bouw: niet onder `telemetry/sessions/`, het bestand ligt direct in de statusmap), met `<state>` = `LIGHTHOUSE_STATE_DIR`
  als die gezet is, anders dezelfde terugval als `tick` (`XDG_STATE_HOME` of `~/.local/state`, plus
  `lighthouse/dispatch`; `internal/cli/tick.go`): hook en collector leiden het pad op dezelfde manier
  af, ook voor een interactieve sessie zonder dispatcher (r1-f6). Het bestand is een lijst van
  waarnemingen (tijdstip, lighthouse- en packversie), append-only: een hervatte sessie onder een
  nieuwere versie voegt een waarneming toe, en de collector kiest per run de eerste waarneming die op of na het begin van zijn
  venster valt en vóór zijn einde (of vóór de start van de volgende run): de hook vuurt na
  `runs.started_at`, omdat de dispatcher de run claimt voordat claude start. Vindt hij er geen,
  dan blijven de twee velden ontbrekend (r1-f7). Een database-rij kan hij
  niet schrijven: `setups.session_id` verwijst naar een `sessions`-rij die bij de start nog niet
  bestaat, en een run mag er geen krijgen. De collector leest het zijbestand bij het schrijven van
  de `setups`-rij, van run of sessie. Zonder zijbestand blijven de twee velden ontbrekend, dus bij
  oude transcripts en bij runs op een kenn-forge-node, waar het zijbestand op de node staat.

## Tests

- **AC 7.** Een fixture met een hoofdtranscript (model A, berichten verspreid over meerdere regels)
  en een subagent (model B): `usage` heeft twee rijen met de juiste som per model; een bericht dat
  op drie regels staat, telt één keer.
- Ontdubbelen over hoofdtranscript en subagent heen.
- Een tweede `collect` op een gegroeid transcript werkt de rijen bij, zonder nieuwe rijen.
- Een tweede `collect` op een ongewijzigd transcript verandert niets (idempotent).
- Twee runs met dezelfde sessie-id: elke run krijgt alleen de regels in zijn venster; de som over
  het traject is de som van het transcript.
- Een transcript van een run maakt geen `sessions`-rij.
- Een sessie op `main` zonder issuenummer: `sessions`-rij met `unlinked_reason = 'main_only'`.
- Ontbrekend: een transcript zonder superpowers-skill geeft `superpowers_version IS NULL`, niet
  `''`.
- Lane: `subagent-driven-development` geeft `sdd`, `executing-plans` geeft `native`, geen van
  beide geeft `NULL`.
- Kosten: een bekend model geeft `estimate` met het juiste bedrag, inclusief de splitsing 5m/1h; een
  onbekend model geeft `NULL`/`NULL` met de tokens erbij.
- Plantaken: een plan met 6 taakkoppen geeft `plan_tasks = 6`; geen plan geeft `NULL`.
- Migratie: `internal/store/migrate_test.go` op het nieuwe `user_version`, het herbouwde `setups`
  (een rij met `''` wordt `NULL`) en de nieuwe kolommen.
- Vragen: een AskUserQuestion met antwoord in een afgesloten sessie geeft één vraag- en één
  antwoordgebeurtenis; een tweede slag voegt niets toe; een open sessie schrijft nog niets.
- Einde: een transcript dat korter dan een uur stilstaat, laat `ended_at` leeg; daarna is het de
  laatste timestamp.
- Issuenummer: `ARGUMENTS: 433` koppelt aan #433 met basis `transcript`; `#433` in vrije tekst
  niet.
- Inzet bij een hervatting: een run zonder eigen skillaanroep erft lane en skillhash uit het
  eerdere deel van het transcript.
- Zijbestand: met zijbestand staan lighthouse- en packversie in `setups`; zonder zijbestand zijn
  ze `NULL`.
- `tick` verzamelt hooguit eens per uur.
- Een sessie met `ended_at IS NULL` die een uur stilstond, wordt in een volgende `--since-last`-slag
  afgesloten, ook als haar `recorded_at` nieuwer is dan de mtime.
- `--dry-run` op een database met een ouder schema laat `user_version` en alle rijen ongemoeid.
- Een transcript dat groeit terwijl de collector leest: de volgende slag neemt de nieuwe regels mee.
- Een mislukte schrijf halverwege laat geen sessie-, setup- of usagerij van dat transcript achter.
- De skillhash is gelijk voor twee sessies die dezelfde skill laden vanuit andere worktrees.
- Regels van het type `api-request-blob` en `cost-state` tellen niet mee.
- Een run-transcript uit de container komt via `workplace.Adapter.Transcript` binnen en niet via de
  hook.

## Volgorde met #378

`RecordEvent` en de tabel `trajectory_events` komen uit #378. De taak die vragen en antwoorden
schrijft, is de laatste taak van het plan en wacht op de merge van #378. Mergt #375 eerst zonder die
taak, dan wordt die taak een vervolgissue. Wie als tweede mergt, hernummert zijn migratie.

## Documentatie

- `docs/install.md`: `lighthouse telemetry collect`, naast `measure-recovery`.
- `CLAUDE.md`, sectie *Hooks*: de hook `telemetry-collect`.
- Een fragment in `changelog.d/`.
- Milestone 12, *Nog niet vastgelegd*: `→ #373` achter de sessie-zonder-issuebranch-vraag, en de
  kostenvraag afgehandeld met een verwijzing naar deze spec.

## Aan de bouwer

- De vorm van `Reading`, `Tokens` en `Setup`, en of de parser `kpi`'s JSONL-lezer hergebruikt of
  deelt.
- Hoe de transcriptmap van een kenn-forge-run gevonden wordt, zolang een run waarvan het transcript
  onbereikbaar is in de uitvoer staat met de reden.
- De begininhoud van `prices.toml`, uit de officiële prijspagina, met `source` en `as_of`.
- Of het 1M-contextmodel (`[1m]` in `cost-state`, niet in `message.model`) anders geprijsd is, en
  hoe de raming daarmee omgaat (r1-f17).
- De naam en het formaat van het log van de hook.

## Open voor het plan

- Waar de ruwe transcriptbytes van een run vandaan komen per werkplek (r3-f1, uitgesteld):
  `kennforge.Adapter.TranscriptDelta` werkt alleen voor kenn-forge op deze node
  (`transcriptWorkspace` geeft `ErrNotMeasurable` bij `a.Node != ""`); voor een andere node moet de
  spiegel eerst gesynchroniseerd worden (zoals `nodeTranscript`) en daarna met `workplace.ReadDelta`
  gelezen, voor de lokale adapter volstaat `workplace.ReadDelta` op de hosttranscriptmap. Het plan
  kiest de vorm (een nieuwe methode of een optionele interface).
- De prijs van het 1M-contextmodel (r1-f17).
