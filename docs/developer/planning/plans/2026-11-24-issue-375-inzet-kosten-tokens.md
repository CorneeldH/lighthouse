# Inzet, lane, kosten en tokens per run en sessie — implementatieplan

> **Voor agentic workers:** VERPLICHTE SUB-SKILL: gebruik superpowers:subagent-driven-development (aanbevolen) of superpowers:executing-plans om dit plan taak voor taak uit te voeren. Stappen gebruiken checkbox-syntaxis (`- [ ]`).

**Doel:** `lighthouse telemetry collect` leest Claude Code-transcripts van runs en interactieve sessies en vult `setups`, `usage`, `sessions.ended_at`, `trajectories.plan_tasks` (en, na #378, vragen en antwoorden).

**Architectuur:** Een pure parser (`internal/telemetry/transcript`) maakt van de bestanden van één sessie een `Reading`. Een collector (`internal/telemetry/collect`) wijst die toe aan runs of een sessie en schrijft atomair per transcript. Drie ingangen voeren hem: het CLI-commando, een SessionStart-hook (achtergrond, plus zijbestand met versies) en `tick` (ruwe bytes via een nieuwe optionele workplace-interface).

**Tech Stack:** Go, `modernc.org/sqlite`, `github.com/BurntSushi/toml` (staat al in `go.mod`), cobra.

**Spec:** [`../specs/2026-11-24-issue-375-inzet-kosten-tokens-design.md`](../specs/2026-11-24-issue-375-inzet-kosten-tokens-design.md). Lees ook het gegevensmodel [`../specs/2026-11-19-telemetrie-gegevensmodel-design.md`](../specs/2026-11-19-telemetrie-gegevensmodel-design.md).

## Globale randvoorwaarden

Elke taak neemt deze regels mee.

- Een ontbrekende waarde is `NULL`, nooit `''` of `0`. Een onbekend model: `cost` en `cost_kind` beide `NULL`.
- `cost_kind` is altijd `'estimate'` (nooit `actual` of `quota`).
- Tokens tellen één keer per `message.id` (terugval `requestId`, anders de regel zelf), over hoofdtranscript en subagents samen. Alleen regeltypen `user` en `assistant` tellen.
- Geen transcripttekst in `lighthouse.db`: alleen getallen, versies, hashes, tijdstippen en verwijzingen.
- `source` = `transcript`; `source_id` = `transcript:<sessionId>` (setup, sessie), `transcript:<sessionId>:<model>` (usage), met achtervoegsel `:<run-id>` bij een transcript dat over runs verdeeld is.
- Eén transactie per transcript. `recorded_at` = het tijdstip vóór het lezen; het bestand wordt gelezen tot de vooraf gemeten grootte.
- Code, commentaar en bestandsnamen onder `internal/**` en `pack/**` zijn Engels (`code_language = "en"`); documenten Nederlands.
- Voor elke commit: `[test_gate] command` uit `lighthouse.toml` en `lighthouse imports check`. Nooit `--no-verify`.
- Migratienummer is `0012`; mergt #378 eerst, dan het volgende vrije nummer (en hernoem het bestand, de test en `docs/`-verwijzingen).

## Review Focus

Invoerklassen die de spec impliceert en geen taak vanzelf toetst; elke regel heeft een test in de genoemde taak.

1. Een transcript waarvan de laatste regel half geschreven is (Claude schrijft nog): die regel wordt overgeslagen, geen fout, geen halve telling. → Taak 4.
2. Een transcript zonder assistant-regel (alleen `user`): `setups`-rij met alles `NULL`, geen `usage`-rijen, geen paniek. → Taak 4.
3. Een assistant-regel zonder `usage`, of met model `<synthetic>`: telt niet mee, ook niet als model. → Taak 4.
4. Een model-id met `[1m]` of datumachtervoegsel dat niet letterlijk in `prices.toml` staat: geen gok, `NULL`/`NULL`. → Taak 3.
5. Een planbestand met `### Task 1` binnen een codeblok (sjabloonvoorbeeld): telt niet mee. → Taak 8.

## Bestandsstructuur

| Pad | Verantwoordelijkheid |
|---|---|
| `internal/store/migrations/0012_inzet.sql` | schema: nullable setups, `lane`, `plan_tasks`, `plan_path` |
| `internal/telemetry/record.go` (wijzig), `internal/telemetry/tx.go` (nieuw) | upserts en een transactiegreep voor meerdere schrijvers |
| `internal/telemetry/prices.go`, `prices.toml` | prijstabel en `Estimate` |
| `internal/telemetry/transcript/` | pure parser: `Parse`, `Reading`, `Tokens`, `Setup` |
| `internal/telemetry/collect/` | toewijzen aan run of sessie, schrijven, einde van sessie, plantaken, zijbestand lezen |
| `internal/cli/telemetry_collect.go` | commando, ontdekken van transcripts, `--since-last`, `--dry-run` |
| `internal/hooks/telemetrycollect.go`, `pack/core/hooks/hooks.json` | SessionStart-hook en zijbestand schrijven |
| `internal/workplace/rawtranscript.go` + adapters | `RawTranscripts`-interface voor `tick` |
| `internal/cli/tick.go` | hooguit eens per uur `collect` over runs |

---

### Taak 1: Migratie 0012

**Bestanden:**
- Maak: `internal/store/migrations/0012_inzet.sql`
- Wijzig: `internal/store/migrate_test.go` (`TestMigrateTwice` wil `user_version` 12), `internal/cli/tick_test.go:240-241` (`user_version` 11 → 12)

**Interfaces:**
- Produceert: `setups(model, effort, harness, harness_version, superpowers_version, lighthouse_version, pack_version, skills_hash)` nullable zonder default; `setups.lane TEXT CHECK (lane IN ('sdd','native'))`; `trajectories.plan_tasks INTEGER`, `trajectories.plan_path TEXT`.

- [ ] **Stap 1: tests.** In `migrate_test.go`: `TestMigrate0012RebuildsSetups` — bouw een database op versie 11, voeg een `setups`-rij toe met `model = ''` en `skills_hash = 'abc'`, migreer, verwacht `model IS NULL`, `skills_hash = 'abc'`; `PRAGMA table_info(setups)` bevat `lane`; `PRAGMA table_info(trajectories)` bevat `plan_tasks` en `plan_path`; `INSERT … lane = 'x'` faalt op de CHECK; de `UNIQUE (run_id)`, `UNIQUE (session_id)`, `UNIQUE (source, source_id)` en de eigenaar-CHECK bestaan nog (dubbele insert faalt). Pas het verwachte `user_version` aan naar 12.
- [ ] **Stap 2:** `go test ./internal/store/ -run 'Migrate'` → FAIL.
- [ ] **Stap 3:** schrijf de migratie: `CREATE TABLE setups_new` met dezelfde beperkingen als `0010_telemetry.sql` maar de acht inzetvelden nullable, plus `lane`; `INSERT INTO setups_new … SELECT … NULLIF(kolom, '') …`; `DROP TABLE setups; ALTER TABLE setups_new RENAME TO setups`; twee `ALTER TABLE trajectories ADD COLUMN`. Maak geen indexen weg die `0010` aan `setups` hangt (controleer met `sqlite_master`).
- [ ] **Stap 4:** `go test ./internal/store/ ./internal/cli/ -run 'Migrat|NoRepos|Tick'` → PASS.
- [ ] **Stap 5:** commit `feat(store): migratie 0012, setups nullable en plantaken per traject (#375)`.

---

### Taak 2: Schrijvers: nullable setup, bijwerken, transactiegreep

**Bestanden:**
- Wijzig: `internal/telemetry/record.go` (`SetupRecord` bij `:55-60`; `RecordSetup` ±`:368-384`; `RecordUsage` ±`:391-416`; controleer de regels met grep), `internal/telemetry/record_test.go`
- Maak: `internal/telemetry/tx.go`, `internal/telemetry/tx_test.go`

**Interfaces:**
- Verbruikt: de tabellen van taak 1.
- Produceert:
  - `SetupRecord` krijgt `Model, Effort, Harness, HarnessVersion, SuperpowersVersion, LighthouseVersion, PackVersion, SkillsHash, Lane *string`.
  - `RecordSetup` en `RecordUsage` werken bij: dezelfde eigenaar (bij usage ook hetzelfde model) schrijft de afgeleide velden en `recorded_at` opnieuw; `source`, `source_id` en eigenaar blijven. `isNew` is onwaar bij bijwerken.
  - `func SetPlan(ctx, db *sql.DB, repo string, issue int, tasks *int, path string) error` (ook als `Tx`-methode) zet `plan_tasks`/`plan_path` op een bestaand traject.
  - `type Tx struct{…}`; `func InTransaction(ctx context.Context, db *sql.DB, fn func(*Tx) error) error`; methoden `RecordSession`, `RecordSetup`, `RecordUsage`, `SetPlan` met dezelfde argumenten en resultaten als de bestaande functies. De bestaande functies blijven bestaan en delegeren naar dezelfde `…Tx`-kern (geen dubbele SQL).

- [ ] **Stap 1: tests** (`record_test.go`): `TestRecordSetupUpdates` (tweede aanbieding met ander `Model` werkt de rij bij, `isNew == false`, één rij), `TestRecordSetupNilIsNull` (`nil` → `NULL`, nooit `''`; lees met `sql.NullString`), `TestRecordUsageUpdates` (tweede aanbieding met meer tokens en nieuwe `Cost` werkt dezelfde rij bij), `TestRecordSetupLaneCheck` (`lane = "x"` geeft een fout). `tx_test.go`: `TestInTransactionRollsBack` (sessie + setup + usage geschreven, daarna `fn` geeft een fout: geen van de drie rijen blijft), `TestSetPlanNullKeepsNull`.
- [ ] **Stap 2:** `go test ./internal/telemetry/ -run 'Setup|Usage|InTransaction|SetPlan'` → FAIL.
- [ ] **Stap 3:** implementeer. `RecordSetup`: bij conflict op bron of eigenaar `UPDATE` van de acht velden, `lane` en `recorded_at`. Zet `nullString`-gebruik om naar `*string`-helper `nullPtr(*string) any`.
- [ ] **Stap 4:** `go test ./internal/telemetry/ ./internal/store/` → PASS (ook de bestaande tests; pas bestaande testaanroepen van `SetupRecord` aan naar pointers).
- [ ] **Stap 5:** commit `feat(telemetry): setup nullable, bijwerken bij herhaling en één transactie per transcript (#375)`.

---

### Taak 3: Prijstabel en kostenraming

**Bestanden:**
- Maak: `internal/telemetry/prices.toml`, `internal/telemetry/prices.go`, `internal/telemetry/prices_test.go`

**Interfaces:**
- Produceert:
  ```go
  type Tokens struct{ Input, Output, CacheRead, CacheWrite5m, CacheWrite1h int64 }
  func (t Tokens) CacheWrite() int64 // 5m + 1h
  func Estimate(model string, at time.Time, t Tokens) (cost float64, ok bool)
  ```
  `as_of` leest als "geldig vanaf": de lookup kiest de regel met de grootste `as_of` ≤ `at`; ligt `at` vóór de eerste regel van het model, dan geldt die eerste regel (terugverwerken van oude transcripts mag niet allemaal `NULL` geven). `Estimate` rekent één bericht; de collector roept hem **per bericht** aan (taak 4), nooit op een som over berichten.
  `Tokens` leeft in `telemetry` zodat parser én collector hem delen.

- [ ] **Stap 1: tests.** `TestEstimateKnownModel` met een eigen tabel via een interne `estimateWith(table, …)`: 1M invoer + 1M uitvoer + 1M cache-read + 1M cache-write-5m + 1M cache-write-1h geeft de som van de vijf prijzen (kies zelf rekenbare prijzen in de testtabel). `TestEstimateUnknownModel` (`ok == false`, ook voor `claude-x[1m]` en `claude-x-20250101` als alleen `claude-x` in de tabel staat). `TestEstimateUsesPriceAtTimestamp` (twee regels voor één model met verschillende `as_of`: een bericht vóór de tweede `as_of` gebruikt de eerste, een bericht erna de tweede; een bericht vóór de eerste `as_of` gebruikt de eerste). `TestEmbeddedPricesParse` (het echte `prices.toml` parseert, is niet leeg, elk model heeft `source` en `as_of`, geen negatieve prijs, en bevat elk model uit de lijst die stap 3 meet: zet die lijst als literal in de test).
- [ ] **Stap 2:** `go test ./internal/telemetry/ -run 'Estimate|Prices'` → FAIL.
- [ ] **Stap 3:** `prices.toml` met `[[price]]`-tabellen: `model`, `as_of` (datum), `source`, `input`, `output`, `cache_read`, `cache_write_5m`, `cache_write_1h` (USD per miljoen). Vul de modellen die in `message.model` van deze repo's transcripts voorkomen (meet met `grep -oh '"model":"[^"]*"' ~/.claude/projects/*/*.jsonl | sort | uniq -c`) met de prijzen van de officiële Anthropic-prijspagina; zet de gebruikte URL in `source`. Kan een prijs voor een gemeten model niet vastgesteld worden, laat dat model er dan uit (→ `NULL`) en noem het in het commitbericht; kan de prijspagina helemaal niet gelezen worden (geen netwerk), stop de taak met status BLOCKED in plaats van een lege tabel te committen. Het 1M-contextmodel: alleen opnemen als de prijspagina een andere prijs noemt; de lookup matcht uitsluitend het letterlijke id. Laad met `go:embed` en `toml.Decode`.
- [ ] **Stap 4:** `go test ./internal/telemetry/` → PASS.
- [ ] **Stap 5:** commit `feat(telemetry): prijstabel en kostenraming per model (#375)`.

---

### Taak 4: Parser: tokens, ontdubbelen, modellen

**Bestanden:**
- Maak: `internal/telemetry/transcript/parse.go`, `parse_test.go`, `testdata/` (JSONL-fixtures)

**Interfaces:**
- Verbruikt: `telemetry.Tokens`.
- Produceert:
  ```go
  type File struct{ Name string; R io.Reader } // Name relatief met slashes; "<session>/subagents/…" is sidechain
  type Reading struct{ /* intern: Entries, Messages, Started, Ended, SessionKey */ }
  func Parse(files []File) (Reading, error)
  func (r Reading) SessionKey() string
  func (r Reading) Bounds() (started, ended time.Time)   // eerste en laatste timestamp, ook bij ongeordende regels
  type ModelUsage struct{ Tokens telemetry.Tokens; Cost *float64 } // Cost nil: model onbekend in de prijstabel
  // UsageBetween: [from,to); nul tijd = open einde. Kosten = som van telemetry.Estimate per bericht op
  // zijn eigen timestamp; zodra één bericht van een model geen prijs heeft is Cost nil voor dat model.
  func (r Reading) UsageBetween(from, to time.Time) map[string]ModelUsage
  ```
  `Parse` slaat regels over die geen JSON-object zijn, ook een half geschreven laatste regel. Alleen `type` `user` en `assistant`.

- [ ] **Stap 1: tests** (fixtures klein en met de hand geschreven; één echte, geanonimiseerde regel uit een eigen transcript als vormcontrole):
  - `TestParseDedupesMessageAcrossLines`: één `message.id` op drie regels telt één keer (AC 7); hoofdtranscript (model A) + subagent (model B) geven twee modellen met de juiste som.
  - `TestParseDedupesAcrossMainAndSubagent`: dezelfde `message.id` in beide bestanden telt één keer.
  - `TestParseFallsBackToRequestID` en `TestParseLineWithoutIDsCountsAlone`.
  - `TestParseIgnoresOtherLineTypes`: `api-request-blob`, `cost-state`, `attachment` tellen niet.
  - `TestParseSplitsCacheWrite`: `ephemeral_5m` en `ephemeral_1h` komen in `CacheWrite5m`/`CacheWrite1h`; zonder splitsing gaat `cache_creation_input_tokens` naar `CacheWrite5m`.
  - `TestParseUsageBetweenWindows`: twee berichten, venster tussen beide, de som over de vensters is de som over alles.
  - `TestUsageCostPerMessageTimestamp`: twee berichten van één model aan weerszijden van een prijsgrens (injecteer een testtabel via een package-variabele of een optie) in één venster: de kosten zijn de som van twee prijzen, niet het totaal tegen één prijs. `TestUsageCostNilForUnknownModel`.
  - `TestParseTruncatedLastLine` en `TestParseOnlyUserLines` (Review Focus 1 en 2: `UsageBetween` geeft een lege map, geen fout).
  - `TestParseSkipsSyntheticAndUsageless` (Review Focus 3).
- [ ] **Stap 2:** `go test ./internal/telemetry/transcript/` → FAIL.
- [ ] **Stap 3:** implementeer `Parse` met `bufio.Reader.ReadString('\n')` (geen vaste regelgrens; transcriptregels kunnen megabytes zijn). Per uniek bericht één `Message{At, Model, Tokens, Sidechain}`; `At` = eerste timestamp van dat bericht. Hergebruik van `kpi.readSession` is niet nodig: die leest een heel bestand in het geheugen en is Python-pariteitscode.
- [ ] **Stap 4:** `go test ./internal/telemetry/transcript/` → PASS.
- [ ] **Stap 5:** commit `feat(transcript): parser met ontdubbelde tokens per model (#375)`.

---

### Taak 5: Parser: inzet, skills, lane

**Bestanden:**
- Wijzig: `internal/telemetry/transcript/parse.go`; maak `setup.go`, `setup_test.go`

**Interfaces:**
- Verbruikt: `Reading` uit taak 4.
- Produceert:
  ```go
  type Setup struct{ Model, Effort, Harness, HarnessVersion, SuperpowersVersion, SkillsHash, Lane *string }
  // SetupBetween: Model, Effort, HarnessVersion uit de regels in [from,to); Lane, SkillsHash,
  // SuperpowersVersion uit alle regels van het begin tot `to`. Nul `to` = open einde.
  func (r Reading) SetupBetween(from, to time.Time) Setup
  ```
  Model = meeste uitvoertokens op niet-sidechain-regels (niet `<synthetic>`, niet leeg); Effort = laatste niet-lege `effort` op niet-sidechain; `Harness` = `claude-code`; HarnessVersion = laatste `version`.

- [ ] **Stap 1: tests:**
  - `TestSetupModelByOutputTokens`, `TestSetupEffortLast`, `TestSetupEmptyIsNil` (transcript zonder één van de velden: `nil`, niet `""` — Review Focus 2).
  - `TestSetupSuperpowersVersionFromBaseDirectory`: `…/superpowers/7.0.2/skills/…` geeft `7.0.2`; twee versies: de laatste; geen superpowers-skill: `nil`.
  - `TestSetupLane`: `superpowers:subagent-driven-development` → `sdd`; `superpowers:executing-plans` → `native`; beide: de laatste wint; geen: `nil`.
  - `TestSkillsHashIgnoresWorktreeAndArguments`: dezelfde skilltekst met een ander *Base directory for this skill*-pad en een andere `ARGUMENTS:`-staart geeft dezelfde hash; een gewijzigde skilltekst geeft een andere.
  - `TestSetupInheritsSkillsFromEarlierWindow`: venster zonder eigen skillaanroep erft `Lane` en `SkillsHash` uit het deel vóór het venster.
- [ ] **Stap 2:** FAIL.
- [ ] **Stap 3:** een skill laadt in het transcript als `user`-bericht met tekst die *Base directory for this skill:* bevat; de skillnaam komt uit het `<command-name>`-element of uit `input.skill` van de bijbehorende `Skill`-tool-use. Controleer dat tegen een echt transcript van deze repo en leg één geanonimiseerd voorbeeld vast in `testdata/`. Hash: sha256 over de gesorteerde paren `(naam, sha256(genormaliseerde tekst))`; normaliseren = de *Base directory*-regel en alles vanaf een regel die met `ARGUMENTS:` begint verwijderen.
- [ ] **Stap 4:** PASS. **Stap 5:** commit `feat(transcript): inzet, skillhash en lane uit het transcript (#375)`.

---

### Taak 6: Parser: issuenummer per regel, einde, vragen

**Bestanden:**
- Wijzig: `internal/telemetry/transcript/`; maak `lines.go`, `lines_test.go`

**Interfaces:**
- Produceert:
  ```go
  func (r Reading) Lines() []telemetry.Line // branch (gitBranch) en issuenummer per regel
  type WrittenFile struct{ Path string; At time.Time }
  func (r Reading) WrittenFiles() []WrittenFile // chronologisch; `file_path` van Write- en Edit-tool-uses, geen inhoud
  type Question struct{ ToolUseID string; Asked time.Time; Answered *time.Time }
  func (r Reading) Questions() []Question   // AskUserQuestion; antwoord = tool_result met hetzelfde id
  ```
  Issuenummer: eerste getal in de `ARGUMENTS:`-staart van een geladen skill, of in `<command-args>` van een slashcommando, alleen als het getal vooraan staat. Een `#433` in vrije tekst telt niet.

- [ ] **Stap 1: tests:** `TestLinesIssueFromArguments` (`ARGUMENTS: 433` → 433), `TestLinesIssueFromCommandArgs`, `TestLinesIgnoreFreeTextHash` (`#433` in een gebruikersbericht → 0), `TestLinesIssueNotAtStartIgnored` (`ARGUMENTS: see 433` → 0), `TestQuestionsPairToolResult`, `TestQuestionUnanswered` (`Answered == nil`), `TestWrittenFilesChronological` (twee Write's en een Edit op twee paden: volgorde klopt, Read-tool-uses tellen niet).
- [ ] **Stap 2:** FAIL. **Stap 3:** implementeer; geen tekst bewaren, alleen id en tijdstippen. **Stap 4:** PASS.
- [ ] **Stap 5:** commit `feat(transcript): issuenummer per regel en AskUserQuestion-tijdstippen (#375)`.

---

### Taak 7: Toewijzen aan run of sessie, zijbestand

**Bestanden:**
- Maak: `internal/telemetry/collect/assign.go`, `sidecar.go`, `*_test.go`

**Interfaces:**
- Verbruikt: `Reading` (taken 4-6), tabel `runs` (`id`, `session_id`, `trajectory_id`, `started_at`, `ended_at`).
- Produceert:
  ```go
  type RunWindow struct{ RunID int64; From, To time.Time } // To nul = open
  func Windows(runs []RunRow, readingEnd time.Time) []RunWindow
  type Observation struct{ At time.Time; LighthouseVersion, PackVersion string }
  func ReadSidecar(stateDir, sessionID string) ([]Observation, error)   // ontbreekt het bestand: nil, nil
  func AppendSidecar(stateDir, sessionID string, o Observation) error    // append-only JSON-lijst, atomair
  func PickObservation(obs []Observation, w RunWindow) *Observation       // eerste op of na w.From en vóór w.To
  func StateDir(getenv func(string) string) (string, error)               // LIGHTHOUSE_STATE_DIR, anders de terugval van tick
  ```
  `RunRow{ID int64; Started time.Time; Ended *time.Time}` is een klein eigen type in `collect`; `Process` vult het uit SQL.
  Vensterregel uit de spec: een regel buiten elk venster gaat naar de run die er het dichtst vóór startte; vóór de eerste start naar de eerste run.

- [ ] **Stap 1: tests:** `TestWindowsTwoRunsSameSession` (grenzen, regel buiten elk venster, regel vóór eerste start), `TestWindowsOpenRun`, `TestPickObservationFirstInWindow`, `TestPickObservationNoneInWindow` (→ nil), `TestReadSidecarMissing`, `TestAppendSidecarKeepsOrder` (twee waarnemingen blijven beide staan), `TestStateDirFallback` (`XDG_STATE_HOME` absoluut / `HOME`-terugval; zelfde pad als `tickEnv.stateDir`, assert met de uitkomst van `lighthouse/dispatch`).
- [ ] **Stap 2:** FAIL. **Stap 3:** implementeer; `StateDir` dupliceert de terugval van `tickEnv.stateDir` (`internal/cli/tick.go:85`) niet: zet de logica in `collect.StateDir` en laat `tickEnv.stateDir` alleen de XDG/`HOME`-terugval daaruit aanroepen, met een bestaande test die het pad vastlegt. De `LIGHTHOUSE_STATE_DIR`-controle blijft in `collect.StateDir` voor hook en collector; `tick` honoreert die variabele nog steeds niet.
- [ ] **Stap 4:** PASS. **Stap 5:** commit `feat(collect): vensters per run en zijbestand met versies (#375)`.

---

### Taak 8: Plantaken en het schrijven van één transcript

**Bestanden:**
- Maak: `internal/telemetry/collect/plan.go`, `process.go`, `*_test.go`

**Interfaces:**
- Verbruikt: alles uit taken 2-7.
- Produceert:
  ```go
  func CountPlanTasks(markdown string) int // koppen "## Task <n>", "### Task <n>", "## Taak <n>", "### Taak <n>"; niet binnen ``` of ~~~ blokken
  type Source struct {
      SessionID, TranscriptPath string
      Files []transcript.File
      MTime time.Time      // nieuwste mtime van hoofd- en subagentbestanden; nul bij ruwe bytes zonder mtime (dan geldt de versheidsregel hieronder)
      Recorded time.Time   // tijdstip vóór het lezen
  }
  type Options struct {
      Repo, DefaultBranch, PlansDir, RepoDir, StateDir string
      Now func() time.Time
      SinceLast bool
  }
  type Outcome struct{ Kind string /* "run", "session", "skipped" */; Reason string; SessionID int64; RunIDs []int64 }
  func Process(ctx context.Context, db *sql.DB, src Source, opt Options) (Outcome, error)
  ```
  `Process` volgt de spec, in deze volgorde: lezen en parsen; runs met `runs.session_id` ophalen; per run `Windows`, `UsageBetween` (kosten zijn daar al per bericht geraamd), `SetupBetween` en zijbestand; anders `telemetry.Link` en een sessie; alles in één `InTransaction`. Sessie: `EndedAt` = `Bounds().ended` als `Now − MTime ≥ 1 uur`, anders `nil`. `--since-last` slaat over als `recorded_at ≥ MTime`, behalve bij `ended_at IS NULL` en `Now − MTime ≥ 1 uur`. Is `MTime` nul (ruwe bytes van `tick`), dan slaat `Process` een run alleen over als die een einde heeft (`runs.ended_at`) en elke `setups`-rij van die run een `recorded_at ≥ runs.ended_at` heeft; een open run of een run zonder rij wordt altijd (opnieuw) verwerkt. Een sessie op `ErrSessionHasFindings` (of #378's `ErrSessionHasEvents`) houdt haar koppeling en geeft `Kind = "skipped"` met reden, geen fout. Plantaken: per gekoppeld traject het planbestand dat een Write of Edit in het transcript onder `PlansDir` schreef (laatste, via `Reading.WrittenFiles()` uit taak 6), anders een bestand met `issue-<nr>-` in de naam; lees de werkboom, anders `git show origin/<default>:<pad>`; `SetPlan` met `nil` als er geen plan is.

- [ ] **Stap 1: tests** (in-memory database via `store.Open` op `t.TempDir()`; fixtures uit taak 4-6):
  - `TestCountPlanTasks`: zes koppen → 6; `### Task 1` in een codeblok → telt niet (Review Focus 5); `## Taak 2` en CRLF tellen; geen koppen → 0.
  - `TestProcessSessionWritesRows`: sessie op `main` zonder issuenummer → `sessions`-rij met `unlinked_reason = 'main_only'`, één `setups`-rij, `usage` per model.
  - `TestProcessRunMakesNoSession`: transcript van een run → geen `sessions`-rij, `setups.run_id` gezet.
  - `TestProcessTwoRunsSameSession`: elke run krijgt zijn venster; som per model over beide runs = som van het transcript.
  - `TestProcessSecondPassGrown`: tweede aanroep op gegroeid transcript werkt rijen bij, aantallen rijen gelijk; op ongewijzigd transcript verandert `recorded_at`-gedrag niet in tellingen (idempotent).
  - `TestProcessNoDuplicateAcrossRunAndSession`: één transcript maakt óf run-rijen óf sessierijen.
  - `TestProcessEndedAt`: mtime 10 min oud → `ended_at IS NULL`; mtime 2 uur oud → laatste timestamp; groeit later → weer `NULL`.
  - `TestProcessSinceLastReopensOpenSession`: `ended_at IS NULL`, `recorded_at` nieuwer dan mtime, mtime nu een uur oud → toch verwerkt en afgesloten; zelfde met een afgesloten sessie → overgeslagen.
  - `TestProcessRollbackLeavesNothing`: een schrijf die halverwege faalt (forceer met een `usage`-rij voor een niet-bestaand `run_id` bij `foreign_keys = ON`, of met een faalhaak tussen twee schrijfacties; `usage` heeft geen CHECK op tellingen en een al afgebroken `ctx` faalt al bij `BEGIN`, dus bewijst niets) laat geen sessie-, setup- of usagerij achter.
  - `TestProcessMissingValuesNull`: zonder superpowers-skill `superpowers_version IS NULL`; zonder zijbestand `lighthouse_version IS NULL`; mét zijbestand gevuld.
  - `TestProcessUnknownModelCostNull`: onbekend model → `cost` en `cost_kind` `NULL`, tokens aanwezig; bekend → `estimate`.
  - `TestProcessPlanTasks`: plan met 6 koppen → `plan_tasks = 6`, `plan_path` gezet; geen plan → `NULL`; twee plannen in één transcript geschreven → het laatst geschreven wint.
  - `TestProcessRawBytesGrowingRun`: `MTime` nul, open run, tweede `Process` op gegroeid transcript → usage bijgewerkt; gesloten run waarvan de `setups` na `ended_at` staat → overgeslagen.
  - `TestProcessSessionKeepsLinkOnRefusal`: findings op de sessie, nieuwe branch → `Kind = "skipped"`, geen fout.
- [ ] **Stap 2:** `go test ./internal/telemetry/collect/` → FAIL.
- [ ] **Stap 3:** implementeer in `process.go` en `plan.go`. Eén functie per stap (`assignRuns`, `writeRun`, `writeSession`, `planFor`) zodat de sessiepad en het runpad dezelfde `writeSetupAndUsage` delen.
- [ ] **Stap 4:** PASS (ook `-race`). **Stap 5:** commit `feat(collect): een transcript atomair naar run of sessie schrijven (#375)`.

---

### Taak 9: `lighthouse telemetry collect`

**Bestanden:**
- Maak: `internal/cli/telemetry_collect.go`, `telemetry_collect_test.go`; wijzig `internal/cli/root.go:110` (registreer een nieuw ouder-commando `newTelemetryCommand()` met subcommando `collect` naast `newMeasureRecoveryCommand`; `--dry-run` is een `DryRun`-veld in `collect.Options`: parsen en toewijzen zonder `InTransaction`, op de alleen-lezen verbinding zonder `_txlock=immediate`)

**Interfaces:**
- Verbruikt: `collect.Process`, `store.OpenShared`, `repoconfig.Load`, `lighthouse config paths.plans`.
- Produceert: `lighthouse telemetry collect [--repo-dir <pad>] [--db <pad>] [--since-last] [--dry-run]`. Exit 0 klaar; 1 verwerkings-, config-, git- of databasefout (ook een deels mislukte slag; overgeslagen transcripts met reden zijn geen fout); 2 verkeerd gebruik (zelfde `usageErr`/`exitError` als `measure-recovery`). Per overgeslagen of onleesbaar transcript één regel `skip <pad>: <reden>`.
  - Ontdekken: alle projectmappen onder `~/.claude/projects` (`HOME` via de omgeving, testbaar), per map het eerste regelobject met een `cwd`; neem de map als die `cwd` onder `--repo-dir`, onder `lighthouse config worktree.root` of onder een worktree uit `git worktree list --porcelain` valt, of de `cwd` de padsegmenten `<owner>/<repo>` van de repo (hoofdletterongevoelig, aaneengesloten) bevat, zoals kenn-forge-worktrees onder `…/worktrees/github/github.com/<owner>/<repo>/…` (ook verwijderde worktrees). Elk `*.jsonl` direct in de map is een hoofdtranscript; `<sessie>/subagents/*.jsonl` hoort erbij. Lees tot de vooraf gemeten grootte (`io.LimitReader`).
  - `--dry-run`: opent de database alleen-lezen zonder migratie (aparte `store`-functie `OpenReadOnly` of een `mode=ro`-DSN; geen WAL-wijziging, geen `migrate`), schrijft niets, ook geen zijbestand, en print wat het zou doen. Een database die niet bestaat: meld dat en print wat gevonden is.

- [ ] **Stap 1: tests** (`telemetry_collect_test.go`, met een tijdelijke `HOME` met twee projectmappen en een tijdelijke database): `TestCollectFindsWorktreeTranscripts` (map onder repo-dir, onder worktree-root, een niet-gerelateerde map wordt genegeerd, ook een map zonder naamvoorvoegsel `--claude-worktrees-`; een map met een kenn-forge-`cwd` van een verwijderde worktree wordt wel gevonden), `TestCollectExitCodes` (0, 1 bij kapotte `lighthouse.toml`, 2 bij een argument), `TestCollectPrintsSkipReason` (onparseerbaar transcript → regel met reden, niets opgeslagen, exit 0), `TestCollectDryRunLeavesOldSchemaAlone` (database op `user_version` 10: na `--dry-run` nog 10 en alle rijen gelijk, geen zijbestand), `TestCollectGrowingTranscript` (bestand groeit tijdens het lezen: de volgende slag neemt de nieuwe regels mee; `recorded_at` is van vóór het lezen).
- [ ] **Stap 2:** FAIL. **Stap 3:** implementeer; hergebruik niets uit `kpi.sessionFiles` (andere ontdekregel). `tick` roept later `runCollect` aan: houd de kern in een functie `runCollect(ctx, env, opts) (summary, error)` los van cobra.
- [ ] **Stap 4:** `go test ./internal/cli/ -run Collect` → PASS. **Stap 5:** commit `feat(cli): lighthouse telemetry collect (#375)`.

---

### Taak 10: SessionStart-hook `telemetry-collect`

**Bestanden:**
- Maak: `internal/hooks/telemetrycollect.go`, `telemetrycollect_test.go`
- Wijzig: `pack/core/hooks/hooks.json` (SessionStart), `pack/core/levels.toml` (`base`), `internal/levels/installed.go:82` (`BaseHooks`), `e2e/registrations_test.go` (payload voor `telemetry-collect`, aanvullen van `wantHooks`, en `LIGHTHOUSE_TELEMETRY_COLLECT=off` in de omgeving van die subtests), `internal/hookcheck/smoke.go` (dezelfde omgevingsvariabele), `docs/site/data/session.json` (chip voor `telemetry-collect`: events SessionStart, effects records, bron `internal/hooks/telemetrycollect.go`; `docs/site/test/session.test.mjs` eist een chip per hook in `pack/*/hooks/hooks.json`), `CLAUDE.md` (sectie *Hooks*)
- Daarna: `go run ./cmd/lighthouse init --upgrade` en commit de installatiekopie (`.claude/`, `.lighthouse/manifest.toml`, `docs/site/data/*.json` zoals die opdracht ze wijzigt).

**Interfaces:**
- Verbruikt: `collect.AppendSidecar`, `collect.StateDir`, `hook.Payload.SessionID`.
- Produceert: hook `telemetry-collect` (`hook.Register`): schrijft altijd een waarneming (`time.Now`, binaryversie, `version` uit `.lighthouse/manifest.toml`) in het zijbestand; start alleen als `LIGHTHOUSE_CONTROLLER_RUN` en `LIGHTHOUSE_TELEMETRY_COLLECT=off` beide leeg zijn `lighthouse telemetry collect --repo-dir <root> --since-last` losgekoppeld (`detach_unix.go`/`detach_other.go` bestaan al in `internal/hooks`) en keert meteen terug. Blokkeert nooit (code 0). Opent geen database. Fout: één regel naar `<state>/telemetry/hook.log`, geen melding in de sessie.

- [ ] **Stap 1: tests:** `TestTelemetryCollectWritesSidecar` (waarneming met beide versies), `TestTelemetryCollectSidecarInControllerRun` (`LIGHTHOUSE_CONTROLLER_RUN` gezet: zijbestand wel, geen proces gestart — injecteer de starter), `TestTelemetryCollectNeverBlocks` (kapotte `manifest.toml` → code 0, regel in log), `TestTelemetryCollectReturnsWithoutWaiting` (starter-stub die blokkeert: de hook keert toch terug), `TestTelemetryCollectNoSessionID` (leeg `session_id` → geen zijbestand, code 0). e2e: de registratietest in `e2e/registrations_test.go` slaagt met een payload.
- [ ] **Stap 2:** FAIL. **Stap 3:** implementeer en registreer; voeg `telemetry-collect` toe aan `base` in `levels.toml` en aan `BaseHooks` (geen stand, geen override).
- [ ] **Stap 4:** `go test ./internal/hooks/ ./internal/levels/ ./internal/hookcheck/ ./e2e/ -run 'Registr|Telemetry|Level|Smoke'` en `node --test docs/site/test/*.test.mjs` → PASS; daarna `go run ./cmd/lighthouse init --upgrade` en `go run ./cmd/lighthouse init --upgrade` nog eens: tweede keer `No changes.`
- [ ] **Stap 5:** commit `feat(hooks): telemetry-collect bij SessionStart (#375)`, installatiekopie in dezelfde commit.

---

### Taak 11: `tick` verzamelt runs (ruwe bytes)

**Bestanden:**
- Maak: `internal/workplace/rawtranscript.go`; wijzig `internal/workplace/kennforge/transcript.go`, `kennforge/mirror.go`, `internal/workplace/local/remove.go`, `internal/cli/tick.go`
- Test: `internal/workplace/kennforge/transcript_test.go`, `internal/workplace/local/*_test.go`, `internal/cli/tick_test.go`

**Interfaces:**
- Verbruikt: `collect.Process`, `Adapter.TranscriptDelta` (bestaat).
- Produceert: optionele interface (besluit voor de open vraag uit de spec)
  ```go
  // RawTranscripts is an optional interface next to Workplace: the whole transcript files of a session.
  type RawTranscripts interface {
      RawTranscript(ctx context.Context, workspaceID, sessionID string) ([]FileDelta, error) // From == 0, Data = hele bestand
  }
  ```
  - `kennforge.Adapter.RawTranscript`: `a.Node == ""` → `TranscriptDelta(ctx, ws, session, nil)`; op een andere node eerst de spiegel synchroniseren (haal de synchronisatie uit `nodeTranscript` in een functie `syncMirror(ctx, n, ws, session) (dir string, err error)`; `nodeTranscript` roept hem aan en blijft gedragsgelijk) en dan `workplace.ReadDelta(dir, session, nil)`.
  - `local.Adapter.RawTranscript`: `workplace.ReadDelta(HostTranscriptDir(...), session, nil)`; spiegel in `kennforge.Adapter.RawTranscript` de #514-terugval van `Transcript` (container, wanneer `HostTranscriptDir` `ErrNotMeasurable` geeft voor een niet-software-profiel).
  - Fouten als bij `Transcript` (`ErrNotMeasurable`, `fs.ErrNotExist`, `ErrUnanswered`): `tick` meldt het run-id en de reden in zijn uitvoer en gaat door.
  - In `tick`: **vóór** `sweepRepos` en `sweepNodes` (die verwijderen de werkplekken en transcriptspiegels van gesloten runs), hooguit eens per uur (tijdstip in `<stateDir>/telemetry/last-collect`), per open run, of afgesloten run zonder `setups`-rij of met een `setups`-rij ouder dan `ended_at`, met `session_id`, `workspace_id` en `workspace_removed_at IS NULL`: `RawTranscript` → `collect.Process` met `SinceLast: true` (`MTime` nul). `ErrGone` of `fs.ErrNotExist` van een afgesloten run is "onbereikbaar, niet verwerkt": één regel, geen `failed`. Een andere fout laat de tick doorgaan (zelfde patroon als `sweepNodes`: `failed = true` → `errSilent`, reden op `errOut`). Het zijbestand komt van de node waar de run draaide en is op andere nodes afwezig: velden blijven `NULL`.

- [ ] **Stap 1: tests:** kennforge-test voor `RawTranscript` (lokale node: dezelfde bytes als `ReadDelta` met `from = nil`; andere node: na synchronisatie de spiegelbestanden); `nodeTranscript` bestaande tests blijven groen; local-test; `tick_test.go`: `TestTickCollectsAtMostHourly` (twee ticks binnen een uur → één verzamelslag, derde na een uur → tweede), `TestTickCollectErrorDoesNotStopTick` (RawTranscript-fout → tick gaat door, reden in uitvoer, `errSilent`), `TestTickRunTranscriptViaAdapterNotHook` (de bytes komen via `RawTranscript`; geen bestandssysteemlezing), `TestTickCollectsBeforeSweep` (een run die in dezelfde tick sluit en opgeruimd wordt, is eerst verzameld), `TestTickSkipsRemovedWorkspaceRuns` (run met `workspace_removed_at` gezet: geen aanroep, tick niet `errSilent`).
- [ ] **Stap 2:** FAIL. **Stap 3:** implementeer. **Stap 4:** `go test ./internal/workplace/... ./internal/cli/` → PASS (`-race` voor `internal/cli`).
- [ ] **Stap 5:** commit `feat(tick): runtranscripts verzamelen via RawTranscripts, hooguit eens per uur (#375)`.

---

### Taak 12: Documentatie en changelog

**Bestanden:**
- Wijzig: `docs/install.md` (naast *Herstel na merge*, regel ±418, een sectie *Inzet, kosten en tokens*), `CLAUDE.md` (alleen als taak 10 de sectie *Hooks* nog niet bijwerkte), de beschrijving van GitHub-milestone #12 *Telemetrie: wat voegt elk onderdeel toe* (geen bestand in `docs/`; bewerk via `/lighthouse-write-issue`, sectie milestones, of `gh api -X PATCH repos/CorneelDH/lighthouse/milestones/12`, buiten de commit)
- Maak: `changelog.d/issue-375-inzet-kosten-tokens.md`

- [ ] **Stap 1:** beschrijf in `docs/install.md`: het commando en zijn vlaggen, exitcodes, wat wel en niet gemeten wordt (kosten zijn altijd een raming `estimate` uit `prices.toml` met `as_of`; ondertelling van zijvragen; geen quotum), de hook, het zijbestand, `tick` eens per uur, terugverwerken zonder `--since-last`.
- [ ] **Stap 2:** milestone: `→ #373` achter de sessie-zonder-issuebranch-vraag; de kostenvraag afgehandeld met verwijzing naar de spec.
- [ ] **Stap 3:** changelogfragment met `type: added` (vorm: `changelog.d/issue-555-…md`), één tot drie zinnen in het **Engels** (`docs/releasing.md`).
- [ ] **Stap 4:** `go test ./internal/mdlinks/... ./e2e/ -run 'Link|Doc'` (of de md-link-check hook) → geen kapotte link. **Stap 5:** commit `docs: inzet, kosten en tokens verzamelen (#375)`.

---

### Taak 13: Vragen en antwoorden naar `RecordEvent` (wacht op #378)

Doe `git fetch origin && git merge origin/main` en controleer met `grep -rn "func RecordEvent" internal/telemetry/`. Bestaat `RecordEvent` (#378 is gemerged): voer de taak uit. Bestaat hij niet: sla de taak over, zonder te wachten, en maak een vervolgissue (`/lighthouse-write-issue`) met deze taakbeschrijving; meld het in de overdracht. Mergt #378 later, dan raken zijn wijzigingen aan `record.go` (`checkRetarget`, `RecordEvent`) die van taak 2 (`Tx`-kernen): los het conflict op door `RecordEvent` als `Tx`-methode over te nemen.

**Bestanden:**
- Wijzig: `internal/telemetry/collect/process.go`, `process_test.go`; `internal/telemetry/tx.go` (methode `RecordEvent`)

**Interfaces:**
- Verbruikt: `telemetry.RecordEvent(ctx, db, EventRecord)` en `ErrSessionHasEvents` uit #378; lees de merged `EventRecord`-velden en pas de aanroep daarop aan. `ref` volgt de spec van #378: `<sessie-id>:<tool-use-id>` (de spec van #375 noemt het kale tool-use-id; #378 beslist, want zijn `UNIQUE (trajectory_id, kind, ref)` eist het sessieprefix).
- Produceert: `Tx.RecordEvent`; in `Process`, alleen voor een **afgesloten** gekoppelde sessie (`ended_at` gezet): per `Question` een `question`-gebeurtenis (`source_id` = `transcript:<sessionId>:<tool-use-id>:question`, `at` = `Asked`) en, als `Answered != nil`, een `answer` (`…:answer`, `at` = `*Answered`). Runs schrijven geen vragen.

- [ ] **Stap 1: tests:** `TestProcessQuestionAndAnswerClosedSession` (één vraag- en één antwoordgebeurtenis), `TestProcessQuestionsSecondPassAddsNothing`, `TestProcessOpenSessionWritesNoEvents`, `TestProcessRunWritesNoEvents`, `TestProcessUnansweredQuestion` (alleen de vraag; het antwoord volgt in een latere slag), `TestProcessSessionHasEventsKeepsLink` (nieuwe branch voor een sessie met gebeurtenissen → `skipped`, geen fout).
- [ ] **Stap 2:** FAIL. **Stap 3:** implementeer in dezelfde transactie als de rest. **Stap 4:** PASS.
- [ ] **Stap 5:** commit `feat(collect): vragen en antwoorden uit sessies naar RecordEvent (#375)`.

---

## Zelfcontrole

- **Dekking van de spec.** Doelen: setups per run en sessie (T2, 5, 8); usage per model (T4, 8); plantaken (T8); verzamelen zonder losse opdracht (T9-11) en terugverwerken (T9); vragen en antwoorden (T6, 13); `ended_at` (T8). Anti-doelen: dubbel tellen (T4, 8); `''`/`0` voor ontbrekend (T1, 2, 8); stilstaande sessie (T8 `SinceLastReopensOpenSession`); trage start (T10 `ReturnsWithoutWaiting`); geen transcripttekst (alleen getallen en hashes in T2-8; `Question` bevat alleen id en tijden). Alle testregels uit de spec staan in een taak; *Een run-transcript via `Adapter.Transcript` en niet via de hook* is T11 `RunTranscriptViaAdapterNotHook`, nu met `RawTranscript`.
- **Open voor het plan.** Ruwe bytes per werkplek: besloten in T11 (nieuwe optionele interface `RawTranscripts`). Prijs 1M-contextmodel: T3 neemt alleen een regel op als de prijspagina een andere prijs noemt; zonder die regel geeft `[1m]` `NULL`/`NULL`.
- **Afwijking van de spec, ter beoordeling:** `ref` van een sessiegebeurtenis volgt #378 (`<sessie-id>:<tool-use-id>`), niet het kale tool-use-id uit de spec van #375 (T13).
- **Typeconsistentie.** `telemetry.Tokens` (T3) wordt gebruikt door T4-8; `collect.Source`/`Options`/`Outcome` (T8) door T9 en T11; `InTransaction`/`Tx` (T2) door T8 en T13; `transcript.File` (T4) door T8, T9 en T11.
