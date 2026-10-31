# Runstaat, claim en `lighthouse tick` — implementatieplan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** De basis van de dispatcher: runstaat en claim in SQLite, veilig bij twee processen, en een `lighthouse tick`-skelet dat naast `serve` draait, met `tick.lock`, `host.toml [dispatch]` en de Go-lezer van `lighthouse.toml`.

**Architecture:** `internal/store` krijgt naast `Open` (het serve-slot) een `OpenShared` zonder slot; de DSN zet `busy_timeout` en `_txlock=immediate`, `open` schakelt WAL één keer apart in met een begrensde retry op `SQLITE_BUSY`, en de migratierunner leest `user_version` opnieuw binnen elke transactie. `0002_runs.sql` voegt drie tabellen toe; een nieuw pakket `internal/runs` claimt, telt pogingen en geeft vrij. `internal/flock` levert de niet-blokkerende `flock` voor het serve-slot en `tick.lock`. `internal/hostconfig` leest `[dispatch]`, een nieuw pakket `internal/repoconfig` leest `[forge]`, `[forge.board]`, `[controller]` en `[usage_guard]` van een repo. `internal/cli/tick.go` knoopt het samen; gather, decide en apply komen in #112 en #114.

**Tech Stack:** Go, `modernc.org/sqlite` v1.61.0, `github.com/BurntSushi/toml` v1.6.0, cobra, `syscall.Flock`.

**Spec:** [`docs/developer/planning/specs/2026-10-27-m2-dispatcher-design.md`](../specs/2026-10-27-m2-dispatcher-design.md), §1 *Database en processen*, §2 *Configuratie*, §3 *De tick*, en *Dedup* in §4 · Issue #111.

## Global Constraints

- Go volgt `/lighthouse-go-writing`; tests volgen `/lighthouse-testing` (classificatiezin vóór de eerste edit, mutatiecheck vóór "klaar").
- Code-taal Engels onder `internal/**` en `cmd/**`: namen, commentaar, foutmeldingen. Commits, plan en docs in het Nederlands.
- Pragma's in de DSN: `busy_timeout(5000)`, `foreign_keys(ON)`, `synchronous(NORMAL)`, plus `_txlock=immediate`. **Geen `journal_mode(WAL)` in de DSN.** `modernc.org/sqlite` v1.61.0 sorteert de `_pragma`-waarden zelf (`busy_timeout` eerst, de rest alfabetisch; `sqlite.go:477-487`), dus de volgorde in de DSN zegt niets. En de WAL-omzetting van een vers bestand in de verbindings-pragma's geeft bij twee gelijktijdige processen `SQLITE_BUSY`, ook met `busy_timeout`. `open` zet daarom na `sql.Open` en vóór de migraties één keer `PRAGMA journal_mode=WAL`, met een begrensde retry op `SQLITE_BUSY` (bijv. 50 pogingen, 20 ms tussenpoos, daarna de fout). WAL blijft in het bestand staan en geldt dan voor elke verbinding.
- `tick.lock`: `${XDG_RUNTIME_DIR}/lighthouse/tick.lock`, map `0700`, bestand `0600`. Zonder absolute `XDG_RUNTIME_DIR`: exit 1 met de reden.
- `tick` exit 0 tenzij hij niet kan checken of een repo luid overslaat; dan exit 1 met de reden (spec §3).
- Token: `GH_TOKEN`, anders `GITHUB_TOKEN` (`githubToken` in `internal/cli/serve.go`). Leeg: exit 1.
- `ErrSchemaTooNew` blijft, voor `Open` en `OpenShared`.
- De bestaande serve-melding blijft letterlijk `another lighthouse serve holds <pad>.lock` (`e2e/serve_test.go`, `internal/store/store_test.go`).
- Precies drie nieuwe tabellen in `0002_runs.sql` (spec §1: "Meer tabellen komen er in M2 niet").
- Geen `lighthouse run` in dit issue: de claim is gedeeld, het commando komt met de start in #114.
- Geen `[controller]` en geen `[usage_guard] controller = true` in de eigen `lighthouse.toml`: dat is #116 (spec, *Indeling in issues* punt 6). Wel het board-blok.
- Commits: Conventional Commits, Nederlands, één logische wijziging per commit.
- Verificatie zoals CI: `go test -race -shuffle=on ./...` en `go test -race -shuffle=on -tags e2e ./e2e/ -v`; `golangci-lint run`.

**Afwijking van de AC-tekst.** AC 4 citeert de melding "vorige tick loopt nog". Onder `internal/**` is alle tekst Engels (`AGENTS.md`, code_language). De melding wordt `previous tick still running`; de PR noemt dat bij het bewijs van AC 4.

## Review Focus

1. **Een `XDG_RUNTIME_DIR` die ontbreekt of relatief is** (een timer zonder user session). `tick` moet dan niet stil in `/tmp` gaan locken, maar exit 1 met de reden. → Taak 5, `TestTickWithoutRuntimeDir`.
2. **Een `host.toml` met een relatief of niet-bestaand repo-pad.** Een relatief pad hangt af van de cwd van de timer. → Taak 3, `TestLoadDispatchRelativeRepo`; een niet-bestaand pad is een luide skip in taak 5, `TestTickSkipsMissingRepo`.
3. **Een tweede `start_failed` voor dezelfde trigger.** Die mag geen derde poging geven. → Taak 2, `TestSecondStartFailedSpendsTrigger`.
4. **Een database die een nieuwere lighthouse schreef, geopend door `tick`.** Moet geweigerd worden zonder iets te wijzigen. → Taak 1, `TestOpenSharedSchemaTooNew`.
5. **Een `host.toml` zonder `[dispatch]` of zonder repo's.** `tick` doet dan niets en geeft exit 0, geen fout. → Taak 5, `TestTickNoRepos`.

---

### Task 1: `store` — open zonder serve-slot, migraties veilig bij twee processen

**Files:**
- Create: `internal/flock/flock.go`, `internal/flock/flock_test.go`
- Modify: `internal/store/store.go`, `internal/store/lock.go`, `internal/store/migrate.go`
- Test: `internal/store/store_test.go`, `internal/store/migrate_test.go`, nieuw `internal/store/process_test.go`

**Interfaces:**
- Produces:
  - `flock.TryLock(path string) (*os.File, error)` — `O_CREATE|O_RDWR`, `0600`, `LOCK_EX|LOCK_NB`. Bezet: `flock.ErrHeld` (te testen met `errors.Is`); andere fout: gewikkeld met het pad.
  - `store.OpenShared(ctx context.Context, path string) (*Store, error)` — als `Open`, zonder `<db>.lock`; maakt geen `.lock`-bestand.
  - `(*Store).Close()` werkt ook met een nil-slot.
  - `store.acquireLock` gebruikt `flock.TryLock` en vertaalt `ErrHeld` naar `*LockedError` (melding ongewijzigd).

- [ ] **Step 1: Schrijf de falende tests**
  - `flock_test.go`, `TestTryLockHeld`: twee `TryLock` op hetzelfde pad in één proces (flock werkt per open file description); de tweede geeft `errors.Is(err, ErrHeld)`; na `Close` van de eerste slaagt een derde.
  - `store_test.go`, `TestOpenSharedTakesNoLock`: `Open` en daarna twee keer `OpenShared` op hetzelfde pad slagen alle drie; na alleen `OpenShared` op een vers pad bestaat `<pad>.lock` niet.
  - `TestOpenSharedSetsPragmas`: de lus van `TestOpenSetsPragmas` (wal, 5000, 1, 1) op een `OpenShared`-store.
  - `TestDSNTxLock`: de query van `dsn(p)` bevat `_txlock=immediate` en geen `journal_mode`. (Een test op de volgorde van de pragma's bewijst niets: de driver sorteert ze zelf.)
  - `migrate_test.go`, `TestOpenSharedSchemaTooNew`: `user_version = 99` op een kale database; `OpenShared` geeft `errors.Is(err, ErrSchemaTooNew)` en `sqlite_master` blijft leeg.
  - `process_test.go`, `TestFreshDatabaseTwoProcesses`: helper-process-patroon zoals `internal/usage/codex_test.go:151`. `TestHelperOpenShared` draait alleen met `LIGHTHOUSE_TEST_HELPER=open`; hij wacht tot het bestand `$LIGHTHOUSE_TEST_GO` bestaat, roept `OpenShared` op `$LIGHTHOUSE_TEST_DB` aan en eindigt met exit 0, of 1 met de fout op stderr. De test start twee helpers op één vers pad, maakt dan het go-bestand, en eist: beide exit 0, `user_version` gelijk aan het aantal ingebedde migraties, `SELECT count(*) FROM meta WHERE key = 'created_at'` is 1. Twintig herhalingen, elk met een vers pad.

- [ ] **Step 2: Draai ze en zie ze falen**

Run: `go test -race ./internal/flock/ ./internal/store/`
Expected: FAIL: `undefined: TryLock`, `undefined: OpenShared`, en `TestDSNTxLock` faalt (geen `_txlock`, wel `journal_mode`).

- [ ] **Step 3: Bouw `flock.TryLock`, `OpenShared`, de DSN en de migratierunner**
  - `dsn`: de pragma's uit *Global Constraints* zonder `journal_mode`, plus `_txlock=immediate` als query-parameter naast `_pragma`.
  - `Open` en `OpenShared` delen één niet-geëxporteerde functie `open(ctx, path string, lock bool)`. Die zet na `sql.Open` `PRAGMA journal_mode=WAL` met de begrensde retry op `SQLITE_BUSY` (herken die met `*sqlite.Error` en `Code()&0xff == sqlite3.SQLITE_BUSY`), en controleert dat het antwoord `wal` is. Pas daarna `migrate`.
  - `migrate`: de eerste lezing van `user_version` blijft (voor `ErrSchemaTooNew` en een vroege exit). `apply` leest `user_version` opnieuw binnen de transactie (die is `IMMEDIATE` door de DSN). Is die ≥ `m.number`, dan commit hij zonder de migratie te draaien. Is die > aantal bekende migraties, dan `ErrSchemaTooNew`.
  - Package-doc van `store`: "opens the SQLite database that serve, tick and run share".

- [ ] **Step 4: Draai de tests en zie ze slagen**

Run: `go test -race -count=3 ./internal/flock/ ./internal/store/`
Expected: PASS.

- [ ] **Step 5: Mutatiecheck**

Twee mutaties, elk apart en elk met `go test -race -count=3 -run TestFreshDatabaseTwoProcesses ./internal/store/`:
1. Haal de nieuwe lezing in `apply` weg en zet de DSN terug zonder `_txlock`. Verwacht: FAIL (`table meta already exists` of `SQLITE_BUSY`).
2. Zet `journal_mode(WAL)` terug in de DSN en haal de aparte WAL-stap met retry weg. Verwacht: FAIL (`database is locked (5) (SQLITE_BUSY)`).

Zet elke mutatie terug. Faalt de test onder de mutatie niet, verhoog dan het aantal helpers naar vier en noem dat in het commitbericht.

- [ ] **Step 6: Commit**

```bash
git add internal/flock internal/store
git commit -m "feat(store): open zonder serve-slot, migraties veilig bij twee processen"
```

### Task 2: `0002_runs.sql` en `internal/runs` — claim, pogingen, vrijgeven

**Files:**
- Create: `internal/store/migrations/0002_runs.sql`, `internal/runs/runs.go`, `internal/runs/runs_test.go`, `internal/runs/process_test.go`
- Modify: `internal/store/migrate_test.go` (`TestMigrateTwice` verwacht `user_version` 2 en de exacte tabelset, zie stap 1)

**Interfaces:**
- Consumes: `store.OpenShared` (taak 1).
- Produces (pakket `runs`):
  - `type Outcome string`; constanten `StartFailed = "start_failed"`, `Handoff = "handoff"`, `Question = "question"`, `Parked = "parked"`, `Done = "done"`, `Returned = "returned"` (spec §7).
  - `type Claim struct { Repo string; Issue int; SessionID string; Mode string /* "start" | "resume" */; Trigger string /* handoff, answer, usage, closecheck, manual */; TriggerKey string; Command, Prompt, Profile string; Now time.Time }`
  - `type Run struct { ID int64; Attempt int }`
  - `ErrClaimed` (er is al een open rij voor `(repo, issue)`), `ErrTriggerSpent` (de trigger startte al, of faalde twee keer), `ErrNotOpen` (vrijgeven van een rij die al dicht is).
  - `Take(ctx context.Context, db *sql.DB, c Claim) (Run, error)` — één `BEGIN IMMEDIATE`-transactie: tel de rijen met `repo = c.Repo AND trigger_key = c.TriggerKey`. Een sleutel is alleen binnen één repo uniek (`start:<issue>`, `closecheck:<pr>`), en M2 bedient meerdere repo's. Een open rij of een rij met een andere uitkomst dan `start_failed` → `ErrTriggerSpent`. Twee `start_failed` → `ErrTriggerSpent`. Anders `attempt` = 1 + aantal `start_failed`, en `INSERT`. Een unique-violation (`*sqlite.Error` met `Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE`, uit `modernc.org/sqlite` en `modernc.org/sqlite/lib`) → `ErrClaimed`.
  - `Release(ctx context.Context, db *sql.DB, id int64, o Outcome, reason string, now time.Time) error` — `UPDATE … WHERE id = ? AND ended_at IS NULL`; nul rijen → `ErrNotOpen`; een onbekende `Outcome` → fout vóór de query.
  - `NewSessionID() string` — UUID v4 uit `crypto/rand`, kleine letters met streepjes.

**Schema** (kolomnamen hier vastgelegd; tijden als RFC 3339 UTC-tekst):

```sql
CREATE TABLE runs (
  id INTEGER PRIMARY KEY,
  repo TEXT NOT NULL, issue INTEGER NOT NULL,
  session_id TEXT NOT NULL,
  mode TEXT NOT NULL CHECK (mode IN ('start', 'resume')),
  trigger TEXT NOT NULL CHECK (trigger IN ('handoff', 'answer', 'usage', 'closecheck', 'manual')),
  trigger_key TEXT NOT NULL,
  attempt INTEGER NOT NULL CHECK (attempt IN (1, 2)),
  command TEXT NOT NULL DEFAULT '', prompt TEXT NOT NULL DEFAULT '',
  branch TEXT NOT NULL DEFAULT '', workspace_id TEXT NOT NULL DEFAULT '',
  workplace_session TEXT NOT NULL DEFAULT '', profile TEXT NOT NULL DEFAULT '',
  started_at TEXT NOT NULL, ended_at TEXT,
  outcome TEXT CHECK (outcome IN ('start_failed', 'handoff', 'question', 'parked', 'done', 'returned')),
  reason TEXT NOT NULL DEFAULT '',
  CHECK ((ended_at IS NULL) = (outcome IS NULL))
);
CREATE UNIQUE INDEX runs_open_issue ON runs(repo, issue) WHERE ended_at IS NULL;
CREATE INDEX runs_trigger ON runs(repo, trigger_key);
CREATE TABLE action_failures (
  action_key TEXT PRIMARY KEY, error TEXT NOT NULL,
  first_at TEXT NOT NULL, last_at TEXT NOT NULL
);
CREATE TABLE tick_state (
  repo TEXT NOT NULL,
  kind TEXT NOT NULL CHECK (kind IN ('fingerprint', 'deferred', 'run')),
  key TEXT NOT NULL, value TEXT NOT NULL, updated_at TEXT NOT NULL,
  PRIMARY KEY (repo, kind, key)
);
```

`tick_state` draagt de drie soorten tickstand uit spec §1 (vingerafdruk per repo, uitgestelde start met reden, stand per open run) in één tabel; `value` is JSON. Hier alleen het schema: schrijven en lezen komen in #112 en #118.

- [ ] **Step 1: Schrijf de falende tests**
  - `TestTakeAndRelease`: `Take` geeft `Attempt == 1`; een tweede `Take` voor hetzelfde issue met een andere trigger geeft `ErrClaimed`; na `Release(…, Done, …)` slaagt een `Take` met een nieuwe trigger voor hetzelfde issue.
  - `TestStartFailedGivesSecondAttempt` (AC 2): `Take`, `Release(StartFailed)`, `Take` met dezelfde `TriggerKey` geeft `Attempt == 2`; de eerste rij heeft `outcome = 'start_failed'` en een gevulde `ended_at`.
  - `TestSecondStartFailedSpendsTrigger`: na twee `start_failed` geeft `Take` met dezelfde sleutel `ErrTriggerSpent`.
  - `TestOtherOutcomeSpendsTrigger`: na `Release(Returned)` geeft `Take` met dezelfde sleutel `ErrTriggerSpent`.
  - `TestTriggerKeyPerRepo`: `owner/a#7` met `start:7`, `Release(Done)`; daarna `Take` voor `owner/b#7` met `start:7` geeft `Attempt == 1`, geen fout.
  - `migrate_test.go`, `TestMigrateTwice`: `user_version` 2, en de tabellen in `sqlite_master` (zonder `sqlite_%`) zijn precies `action_failures`, `meta`, `runs`, `tick_state`. Daarnaast `PRAGMA table_info` per nieuwe tabel: `runs` heeft `repo`, `issue`, `trigger_key`, `attempt`, `ended_at`, `outcome`; `action_failures` heeft `action_key` als primaire sleutel; `tick_state` heeft de primaire sleutel `(repo, kind, key)`. De oude assert `hasRuns == false` gaat eruit.
  - `TestReleaseTwice`: tweede `Release` op dezelfde id geeft `ErrNotOpen`; `Release` met `Outcome("bogus")` geeft een fout en wijzigt niets.
  - `TestNewSessionID`: twee aanroepen verschillen, en elke uitkomst matcht `^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`.
  - `process_test.go`, `TestClaimTwoProcesses` (AC 1): het helper-patroon van taak 1. Elke helper opent met `OpenShared`, wacht op het go-bestand en roept `Take` aan voor `owner/name#7` met een eigen `TriggerKey`. Exit 0 = gewonnen, 3 = `ErrClaimed`, 1 = andere fout. De test eist precies één 0 en één 3, en één open rij in `runs`. Twintig herhalingen.

- [ ] **Step 2: Draai ze en zie ze falen**

Run: `go test -race ./internal/runs/ ./internal/store/`
Expected: FAIL: `undefined: Take`; `TestMigrateTwice` faalt nog niet (eerst de migratie).

- [ ] **Step 3: Schrijf `0002_runs.sql` en `runs.go`; zet `TestMigrateTwice` op 2**

- [ ] **Step 4: Draai de tests en zie ze slagen**

Run: `go test -race -count=3 ./internal/runs/ ./internal/store/`
Expected: PASS.

- [ ] **Step 5: Mutatiecheck**

Laat de partiële unieke index tijdelijk weg uit `0002_runs.sql`: `TestClaimTwoProcesses` en `TestTakeAndRelease` moeten falen. Zet `attempt` tijdelijk vast op 1: `TestStartFailedGivesSecondAttempt` moet falen. Haal `repo = ?` tijdelijk uit de telling in `Take`: `TestTriggerKeyPerRepo` moet falen. Laat `tick_state` tijdelijk weg uit `0002_runs.sql`: `TestMigrateTwice` moet falen. Zet alles terug.

- [ ] **Step 6: Commit**

```bash
git add internal/store/migrations/0002_runs.sql internal/store/migrate_test.go internal/runs
git commit -m "feat(runs): runstaat in SQLite met claim, pogingen en vrijgeven"
```

### Task 3: `host.toml [dispatch]`

**Files:**
- Modify: `internal/hostconfig/hostconfig.go`
- Test: `internal/hostconfig/hostconfig_test.go`

**Interfaces:**
- Produces:
  - `type Dispatch struct { Repos []string; Slots, MaxSoftware int; MinMemAvailableGB, ReservePerRunGB int; Night string; MaxRuntime, MaxWait time.Duration; AnswerPermissions []string; Forge DispatchForge; Profiles map[string]Profile }`
  - `type DispatchForge struct { URL, TokenFile string }`, `type Profile struct { TargetKey, MCPConfig string }` (TOML `target_key`, `mcp_config`).
  - `DefaultDispatch() Dispatch` — de standaarden uit spec §2: `Repos` leeg, `Slots` 4, `MaxSoftware` 2, `MinMemAvailableGB` 8, `ReservePerRunGB` 3, `Night` `"21:00-08:00"`, `MaxRuntime` 8h, `MaxWait` 1h, `AnswerPermissions` `["admin", "maintain"]`, `Forge.URL` `"http://127.0.0.1:8091"`, `Forge.TokenFile` `~/.kenn/forge/auth_token` (uitgeschreven met de home-dir).
  - `LoadDispatch(path string) (Dispatch, error)` — leest alleen `[dispatch]`, `[dispatch.forge]` en `[dispatch.profiles.<naam>]`. Ontbrekend bestand of lege `path`: standaarden, geen fout. Ontbrekende sleutel: standaard. Anders dan `usage.LoadConfig` is een ongeldige waarde een fout met de sleutelnaam, want de tick moet dan luid stoppen: een relatief repo-pad, `slots < 1`, `max_software < 1`, een negatief geheugengetal, `night` niet als `HH:MM-HH:MM`, een duur die `time.ParseDuration` weigert of ≤ 0 is.

- [ ] **Step 1: Schrijf de falende tests**: `TestLoadDispatchDefaults` (geen bestand en een bestand zonder `[dispatch]` geven `DefaultDispatch()`), `TestLoadDispatchRepos` (twee absolute paden komen in volgorde terug, `slots = 6` overschrijft), `TestLoadDispatchRelativeRepo` (fout die `dispatch.repos` en het pad noemt), `TestLoadDispatchInvalid` (tabel: `slots = 0`, `night = "21-08"`, `max_wait = "soon"`; elke fout noemt zijn sleutel), `TestLoadDispatchProfiles` (`[dispatch.profiles.software] target_key = "paia-software"` komt terug).
- [ ] **Step 2: Draai ze en zie ze falen.** Run: `go test -race ./internal/hostconfig/`. Expected: FAIL, `undefined: LoadDispatch`.
- [ ] **Step 3: Bouw `LoadDispatch`**: één `toml.DecodeFile` in een anonieme struct met pointervelden, zoals `internal/usage/config.go`.
- [ ] **Step 4: Draai de tests en zie ze slagen.** Run: `go test -race ./internal/hostconfig/ ./internal/usage/`. Expected: PASS (de `[usage]`-tests blijven groen).
- [ ] **Step 5: Commit**: `git add internal/hostconfig && git commit -m "feat(hostconfig): lees [dispatch] uit host.toml"`

### Task 4: `internal/repoconfig` en het board-blok in `lighthouse.toml`

**Files:**
- Create: `internal/repoconfig/repoconfig.go`, `internal/repoconfig/repoconfig_test.go`
- Modify: `lighthouse.toml`

**Interfaces:**
- Produces:
  - `type Repo struct { Dir, Slug string; Board *Board; Controller *Controller; UsageGuardController bool }` — `Slug` is `[forge] repo`.
  - `type Board struct { ProjectNumber int; ProjectID, StatusFieldID, PriorityFieldID string; StatusOptions, PriorityOptions map[string]string }`
  - `type Controller struct { Profile string /* standaard "software" */; SpecReview, PlanReview bool /* standaard true */; AtRoundLimit string /* TOML-sleutel bij_rondegrens */ }` — Engelse veldnaam: `internal/**` valt onder `code_language_paths` (`lighthouse.toml:4`); de TOML-sleutel blijft `bij_rondegrens`.
  - `Load(dir string) (Repo, error)` — leest `<dir>/lighthouse.toml`. Ontbrekend bestand of een parse-fout: fout met het pad. Ontbrekende `[forge] repo`: fout. `Board` en `Controller` zijn nil als hun tabel ontbreekt.
  - `Dispatchable(r Repo) error` — `nil`, of één `errors.Join` met elke reden: geen `[controller]`; `[usage_guard] controller` niet `true`; geen `[forge.board]`; `project_number` ≤ 0; `project_id` leeg; `status_field_id` leeg; status-optie `Controller` of `On hold` ontbreekt; `priority_field_id` leeg. Elke reden noemt de sleutel zoals in `lighthouse.toml` (bijv. `forge.board.status_options.Controller`).

Het board-blok voor board *Lighthouse* (project 2, IDs gelezen op 2026-10-31):

```toml
[forge.board]
project_number = 2
project_id = "PVT_kwHOAGmU3s4Bnmdy"
status_field_id = "PVTSSF_lAHOAGmU3s4BnmdyzhmKS68"
priority_field_id = "PVTSSF_lAHOAGmU3s4BnmdyzhmKS9I"

[forge.board.status_options]
"To do" = "5995ef76"
"In progress" = "5c4502d5"
"Done" = "4a797cd3"
"On hold" = "6a2f466b"
"Controller" = "7c834c1d"

[forge.board.priority_options]
P0 = "e78d8910"
P1 = "5866e321"
P2 = "321d79fc"
```

Gevolg buiten de dispatcher: `board_start.py` en de andere skills zien vanaf nu een board en zetten statussen. Dat is de bedoeling (spec §2).

- [ ] **Step 1: Schrijf de falende tests**: `TestLoadFull` (een fixture met alle vier tabellen geeft de velden terug; standaarden voor `profile`, `spec_review`, `plan_review` als ze ontbreken), `TestDispatchable` (tabel met één ontbrekend onderdeel per rij, ook `project_number`, `project_id` en `status_field_id`; de fout bevat de sleutelnaam; een complete fixture geeft `nil`), `TestLoadMissingRepo` (geen `[forge] repo`: fout), `TestOwnLighthouseToml` (`Load("../..")` geeft `Slug == "CorneeldH/lighthouse"`, `Board.StatusOptions["Controller"] == "7c834c1d"`, en `Dispatchable` noemt `[controller]` en `usage_guard.controller` maar niet het board).
- [ ] **Step 2: Draai ze en zie ze falen.** Run: `go test -race ./internal/repoconfig/`. Expected: FAIL, `undefined: Load`.
- [ ] **Step 3: Bouw `Load` en `Dispatchable`; zet het board-blok in `lighthouse.toml`** onder `[forge]`.
- [ ] **Step 4: Draai de tests en de Python-lezer.**

Run: `go test -race ./internal/repoconfig/ && python3 .lighthouse/lib/lighthouse_lib/config.py forge.board.status_options.Controller && go run ./cmd/lighthouse init --upgrade`
Expected: PASS, dan `7c834c1d`, dan `No changes.`

Run ook `uv run pytest -q tests/test_dogfood.py`. Expected: PASS. Faalt een test die de inhoud van `lighthouse.toml` pint, werk die pin dan bij in dezelfde commit.

- [ ] **Step 5: Commit**: `git add internal/repoconfig lighthouse.toml` (plus een bijgewerkte pin uit stap 4, en wat `init --upgrade` schreef) `&& git commit -m "feat(repoconfig): lees lighthouse.toml van een bediende repo; board Lighthouse in lighthouse.toml"`

### Task 5: `lighthouse tick` — het skelet

**Files:**
- Create: `internal/cli/tick.go`, `internal/cli/tick_test.go`
- Modify: `internal/cli/root.go` (`root.AddCommand(newTickCommand())`)

**Interfaces:**
- Consumes: `flock.TryLock`, `store.OpenShared`, `hostconfig.Path`, `hostconfig.LoadDispatch`, `repoconfig.Load`, `repoconfig.Dispatchable`, `githubToken`.
- Produces:
  - `newTickCommand() *cobra.Command` met vlag `--db` (zoals `serve`).
  - `tick(ctx context.Context, env tickEnv, dbPath string, errOut io.Writer) error` met `type tickEnv struct { Getenv func(string) string; HostToml string }`, zodat de tests de omgeving en `host.toml` meegeven.
  - Minstens één luide skip: `tick` geeft `errSilent` terug (`internal/cli/init.go:49`); de redenen staan dan al op stderr, `Execute` print niets meer en geeft exit 1.

Volgorde in `tick` (spec §3, stap 1, plus de token en de database):

1. `XDG_RUNTIME_DIR` moet absoluut zijn, anders `tick: XDG_RUNTIME_DIR is not set: cannot take tick.lock`. Maak `${XDG_RUNTIME_DIR}/lighthouse/` (`0700`) en neem `tick.lock`. Bezet: schrijf `lighthouse: previous tick still running` naar `errOut` en geef `nil` terug (exit 0).
2. Token leeg: `tick: no GitHub token: set GH_TOKEN or GITHUB_TOKEN`.
3. `LoadDispatch(env.HostToml)`; fout: teruggeven.
4. `OpenShared` op `--db`, anders `store.DefaultPath()`.
5. Per repo: `repoconfig.Load`, dan `Dispatchable`. Een fout: `lighthouse: skip <pad>: <reden>` naar `errOut`, en de repo valt af. Gather, decide en apply volgen in #112 en #114; hier gebeurt niets met een geldige repo.
6. Minstens één skip: `errSilent`. Anders `nil`.

- [ ] **Step 1: Schrijf de falende tests** (elk met `t.TempDir()` als `XDG_RUNTIME_DIR` en `--db`):
  - `TestTickLockBusy` (AC 4): neem `tick.lock` met `flock.TryLock` in de test; `tick` geeft `nil` en `errOut` bevat `previous tick still running`; er komt geen databasebestand.
  - `TestTickWithoutToken` (AC 7): geen `GH_TOKEN`/`GITHUB_TOKEN`; de fout bevat `no GitHub token`.
  - `TestTickWithoutRuntimeDir`: `XDG_RUNTIME_DIR` leeg en daarna `"relative"`; elke keer een fout die `XDG_RUNTIME_DIR` noemt.
  - `TestTickNoRepos`: `host.toml` zonder `[dispatch]`; `tick` geeft `nil`, en de database bestaat en heeft `user_version` 2.
  - `TestTickSkipsBadRepos` (AC 6): drie repo-mappen in `host.toml`: een zonder `[controller]`, een met `[controller]` zonder `[usage_guard] controller = true`, een met een board zonder status-optie `Controller`. `tick` geeft `errSilent`; `errOut` noemt elk pad met zijn reden. Een vierde, complete repo levert geen `skip`-regel.
  - `TestTickSkipsMissingRepo`: een repo-pad zonder `lighthouse.toml` geeft een `skip`-regel met dat pad en `errSilent`.
  - `TestTickBesideServe`: `store.Open` (het serve-slot) op het pad, dan `tick` op hetzelfde pad: `nil`.
- [ ] **Step 2: Draai ze en zie ze falen.** Run: `go test -race ./internal/cli/`. Expected: FAIL, `undefined: tick`.
- [ ] **Step 3: Bouw `tick.go` en registreer het commando.**
- [ ] **Step 4: Draai de tests en zie ze slagen.** Run: `go test -race ./internal/cli/`. Expected: PASS.
- [ ] **Step 5: Mutatiecheck**: draai `tick` tijdelijk zonder de `Dispatchable`-aanroep: `TestTickSkipsBadRepos` moet falen. Zet terug.
- [ ] **Step 6: Commit**: `git add internal/cli/tick.go internal/cli/tick_test.go internal/cli/root.go && git commit -m "feat(cli): lighthouse tick als eigen proces, met tick.lock en luide skip per repo"`

### Task 6: e2e — `serve` en `tick` naast elkaar

**Files:**
- Create: `e2e/tick_test.go` (build tag `e2e`)

**Interfaces:**
- Consumes: `buildLighthouse`, `cleanEnv`, `startServe`, `exitCode`, `health` uit `e2e/`.

- [ ] **Step 1: Schrijf de tests**

  `cleanEnv()` (`e2e/e2e_test.go:34-42`) filtert alleen `LIGHTHOUSE_*`; `GH_TOKEN`, `GITHUB_TOKEN` en `XDG_RUNTIME_DIR` van de host blijven staan. Elke `TestTick*` bouwt daarom de omgeving van het kindproces uit `cleanEnv()` zonder die drie sleutels, en zet ze daarna expliciet: `XDG_RUNTIME_DIR=<t.TempDir()>` altijd (nooit het echte `tick.lock`), `GH_TOKEN=x` waar een token hoort, en geen van beide tokens in het geval zonder token.

  - `TestTickBesideServe` (AC 5): start `serve --db <pad>`; draai `lighthouse tick --db <pad>` met `GH_TOKEN=x`, `XDG_RUNTIME_DIR=<tempdir>` en `LIGHTHOUSE_HOST_TOML` naar een bestand zonder repo's: exit 0. Daarna een tweede `serve` op hetzelfde pad: exit 1 met `another lighthouse serve holds` en `<pad>.lock` op stderr. `health` van de eerste `serve` slaagt nog.
  - `TestTickExitCodes`: zonder token exit 1 en stderr bevat `no GitHub token`; met een bezet `tick.lock` (`syscall.Flock` in de test) exit 0 en stderr bevat `previous tick still running`.
- [ ] **Step 2: Draai ze.** Run: `go test -race -tags e2e -run 'TestTick' ./e2e/ -v`. Expected: PASS. (De code staat al sinds taak 5; deze tests bewijzen de exitcodes van de echte binary. Mutatiecheck: laat in `tick.go` de `nil`-return bij een bezet slot tijdelijk een fout geven; `TestTickExitCodes` moet falen.)
- [ ] **Step 3: Commit**: `git add e2e/tick_test.go && git commit -m "test(e2e): tick naast serve en de exitcodes van tick"`

### Task 7: Documentatie en volledige verificatie

**Files:**
- Create: `docs/tick.md` (Nederlands)
- Modify: `README.md`: één alinea over `lighthouse tick` met een relatieve link naar `docs/tick.md`, naast die van `usage` en `serve` (`README.md:15,19`).
- Modify: `docs/getting-started.md` alleen als die een lijst van subcommando's heeft; anders niet.

- [ ] **Step 1: Schrijf `docs/tick.md`**: wat `tick` nu doet (slot, token, `[dispatch]`, de controle per repo, de database naast `serve`) en wat nog niet (gather, decide, apply: #112, #114). De exitcodes. Een voorbeeld van `[dispatch]` met de standaarden uit spec §2. De vereisten per repo: `[controller]`, `[usage_guard] controller = true`, `[forge.board]` met `Controller`, `On hold` en `priority_field_id`. Een relatieve link naar de spec.
- [ ] **Step 2: Draai de volledige verificatie**

Run: `go test -race -shuffle=on ./... && go test -race -shuffle=on -tags e2e ./e2e/ -v && golangci-lint run && go run ./cmd/lighthouse init --upgrade`
Expected: alles PASS, `golangci-lint` zonder bevindingen, `No changes.`

- [ ] **Step 3: Commit**: `git add docs/tick.md README.md` (plus `docs/getting-started.md` als die wijzigde) `&& git commit -m "docs(tick): lighthouse tick, [dispatch] en de vereisten per repo"`
