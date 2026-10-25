# Claude-venstermeter in de binary — implementatieplan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Doel:** `lighthouse usage refresh [--force]` en `lighthouse usage tick` meten het venster van Claude, schrijven de cache en houden het alarmbestand bij dat de usage-guard (#66) leest.

**Architectuur:** Een nieuw pakket `internal/usage` draagt de parser, de bestanden (cache, alarm, lock) en een `Meter` met vervangbare naden voor de klok, de zone, de CLI-aanroep en `pgrep`. `internal/cli/usage.go` hangt er de Cobra-groep `usage` aan en eindigt altijd met exit 0. De drempels en de TTL komen uit een tabel `[usage]` in `host.toml`, zodat de timer en de losgekoppelde refresh van de guard dezelfde waarden gebruiken.

**Tech stack:** Go 1.26, Cobra, BurntSushi/toml, stdlib `syscall.Flock`, `os/exec`.

**Spec:** issue #64 (de acceptatiecriteria zijn de spec). Bron van het gedrag: `scripts/usage_window.py` in CorneeldH/personal_ai_agent (`run_usage_cli`, `parse_usage_output`, `parse_reset_time`, `write_alarm`, `prune_alarm`, `claude_is_running`, `cmd_refresh`, `cmd_tick`).

**Begeleidende skills:** `/lighthouse-go-writing` (classificatiezin vóór de eerste edit, KPI-tabel bij de oplevering) en `/lighthouse-testing` (welke productiewijziging laat elke test falen).

## Globale eisen

- Cachemap: `${XDG_CACHE_HOME:-$HOME/.cache}/lighthouse`, letterlijk zoals de shell het uitrekent: een niet-lege `XDG_CACHE_HOME` wint, ook als die relatief is. Nooit `os.UserCacheDir` (macOS geeft daar `~/Library/Caches`, de guard leest het shellpad).
- Bestanden in die map: `usage-window.json` (cache), `usage-window.alarm` (alarm), `usage-window.lock` (lock), `usage-refresh/` (werkmap van de CLI-aanroep).
- CLI-aanroep: `claude -p /usage --output-format json`, cwd de werkmap, omgeving = `os.Environ()` plus `LIGHTHOUSE_USAGE_REFRESH=1`, timeout 60 s.
- Alarmregel: `<soort> <pct> <reset_local> <reset_at>\n`, soort `venster` of `week`, `venster` eerst; spaties in `reset_local` worden `_`; een lege `reset_local` of `reset_at` wordt `?`.
- Standaardwaarden: `halt_pct_session = 85`, `halt_pct_week = 95`, `cache_ttl_seconds = 300`.
- `refresh` en `tick` eindigen altijd met exit 0; bij elke fout blijft de oude cache staan.
- Tijdstempels in cache en alarm: UTC, `2006-01-02T15:04:05Z`.
- `reset_local`: sessie `HH:MM`, week `<dag> HH:MM` met de vaste tabel `zo ma di wo do vr za` (index `time.Weekday`), in de lokale zone.
- Commentaar en CLI-uitvoer in het Engels; dit plan, commits en PR in het Nederlands.
- Geen nieuwe dependency: `golang.org/x/sys` komt er niet in.

## Review Focus

1. **Corrupte cache.** Een `usage-window.json` met onleesbare JSON, een ander schema of een kapotte `generated_at` telt als afwezig: de refresh gaat door. (In paia blokkeerde een kapotte `generated_at` elke refresh voorgoed.) Test in taak 2 en taak 5.
2. **Geen `HOME` en geen `XDG_CACHE_HOME`.** `DefaultPaths` geeft een fout, het commando print één regel op stderr en eindigt met 0. Test in taak 2 en taak 6.
3. **Envelope met `is_error: true`.** Ook met een herkenbare `result`-tekst weigert de parser hem; anders landt een foutmelding als meting. Test in taak 1.
4. **Kapotte of absurde `[usage]` in `host.toml`.** Een TOML-parsefout geeft alle standaardwaarden; een drempel buiten 1–100 of een negatieve TTL geeft voor die ene sleutel de standaardwaarde. Een drempel 0 zou anders elke sessie stilleggen. Test in taak 4.
5. **`pgrep` met exitcode 2 of 3** (syntaxfout, fatale fout). Dat is "niet kunnen meten", geen "nee": de tick ververst. Alleen exitcode 1 is "geen claude". Test in taak 5.

## Bestanden

| Bestand | Verantwoordelijkheid |
|---|---|
| `internal/usage/parse.go` | `/usage`-envelope naar `Windows`, resettijd naar `time.Time` |
| `internal/usage/state.go` | paden, atomair schrijven, cache lezen en schrijven |
| `internal/usage/alarm.go` | alarmregel bepalen, schrijven, opruimen |
| `internal/usage/config.go` | `[usage]` uit `host.toml` |
| `internal/usage/meter.go` | `Meter.Refresh`, `Meter.Tick`, de echte `claude`- en `pgrep`-aanroep |
| `internal/usage/lock_flock.go`, `lock_other.go` | exclusieve lock, per platform |
| `internal/usage/testdata/*.json` | de vijf fixtures uit paia |
| `internal/hostconfig/hostconfig.go` | `Path()`: waar `host.toml` staat (verhuist uit `initcmd`) |
| `internal/cli/usage.go` | Cobra-groep `usage` met `refresh` en `tick` |
| `e2e/usage_test.go` | binary met een nep-`claude` op `PATH` |
| `docs/usage-venster.md` | gebruikersdocumentatie |

---

### Taak 1: parser

**Files:**
- Create: `internal/usage/parse.go`, `internal/usage/parse_test.go`
- Create: `internal/usage/testdata/{usage_full,usage_whole_hour,usage_session_no_reset,usage_missing_week,usage_no_model}.json` — byte voor byte gekopieerd uit `/home/corneel/repositories/personal_ai_agent/tests/fixtures/usage/`

**Interfaces:**
- Produces:
  ```go
  type Window struct {
      Pct   int
      Reset time.Time // zero: /usage named no reset (an unused session window)
  }
  type Windows struct{ Session, Week Window }
  func Parse(envelope []byte, now time.Time, loc *time.Location) (Windows, error)
  ```

- [ ] **Stap 1: Schrijf de falende tests** (in `parse_test.go`, met `import _ "time/tzdata"` en `loc, _ := time.LoadLocation("Europe/Amsterdam")`; `now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)`, behalve waar anders staat):
  - `TestParseFixtures`, tabel over de fixtures:
    - `usage_full.json` → Session `{99, 2026-09-06T12:30:00Z}`, Week `{48, 2026-09-10T14:59:00Z}`
    - `usage_whole_hour.json`, `now` 2026-09-10T12:00Z → Session `{28, 2026-09-10T22:30:00Z}` (12:30am wordt 00:30 lokaal), Week `{3, 2026-09-17T15:00:00Z}`
    - `usage_session_no_reset.json`, `now` 2026-10-10T12:00Z → Session `{0, time.Time{}}`, Week `{61, 2026-10-15T15:00:00Z}`
    - `usage_no_model.json` → Session `{7, 2026-09-06T17:29:00Z}`, Week `{49, 2026-09-10T14:59:00Z}`
  - `TestParseRejects`, elk geval geeft een fout en een lege `Windows{}`: `usage_missing_week.json`; `{"result": 42}`; `not json`; de envelope van `usage_full.json` met `"is_error": true`; een `result` waarin de sessieregel eindigt op `used · soon`; een resettijd `Sep 31, 5pm`; een resettijd `Foo 6, 5pm`.
  - `TestParseResetYearRollover`: `now` 2026-12-30T12:00Z, resettijd `Jan 2, 5pm` → 2027-01-02T16:00:00Z.
  - `TestParseResetDST`: `now` 2026-10-20T12:00Z, `Oct 26, 9am` → 2026-10-26T08:00:00Z (wintertijd, +01:00), terwijl `Oct 24, 9am` → 2026-10-24T07:00:00Z (+02:00).

- [ ] **Stap 2: Draai ze en zie ze falen**

  Run: `go test ./internal/usage/ -run TestParse -v`
  Expected: FAIL, `undefined: Parse`

- [ ] **Stap 3: Implementeer `Parse` in `internal/usage/parse.go`**

  RE2 (geen lookahead nodig: de modelvensters vallen weg). De drie regexes, letterlijk:
  ```go
  sessionRE = regexp.MustCompile(`(?m)Current session:\s*(\d+)%\s+used(?:\s*·\s*resets\s+(.+?)\s*\(|[ \t]*$)`)
  weekRE    = regexp.MustCompile(`Current week \(all models\):\s*(\d+)%\s+used\s*·\s*resets\s+(.+?)\s*\(`)
  resetRE   = regexp.MustCompile(`(?i)^([A-Za-z]{3})\s+(\d{1,2}),\s+(\d{1,2})(?::(\d{2}))?\s*(am|pm)$`)
  ```
  Envelope: `json.Unmarshal` in `struct{ Result *string; IsError bool \`json:"is_error"\` }`; `Result == nil` of `IsError` is een fout. Alles-of-niets: ontbreekt de sessie- of de weekregel, of faalt één resettijd, dan een fout. Resettijd: eigen maandtabel (`Jan`…`Dec`, hoofdletterongevoelig), uur `h%12`, `+12` bij pm, minuten optioneel. Jaar = `now.In(loc).Year()`; `time.Date(…, loc)`. Normaliseert `time.Date` de dag (31 september wordt 1 oktober), dan is dat een fout. Ligt de kandidaat meer dan 31 dagen vóór `now`, dan het jaar erna; bestaat de dag in dat jaar niet (29 februari), dan blijft het huidige jaar staan. Het resultaat in UTC.

- [ ] **Stap 4: Draai de tests en zie ze slagen**

  Run: `go test ./internal/usage/ -run TestParse -v`
  Expected: PASS

- [ ] **Stap 5: Commit**

  ```bash
  git add internal/usage/parse.go internal/usage/parse_test.go internal/usage/testdata
  git commit -m "feat(usage): parser voor de /usage-uitvoer van Claude (#64)"
  ```

### Taak 2: paden en cache

**Files:**
- Create: `internal/usage/state.go`, `internal/usage/state_test.go`

**Interfaces:**
- Consumes: `Windows`, `Window` uit taak 1.
- Produces:
  ```go
  type Paths struct{ Dir string } // the lighthouse cache directory
  func DefaultPaths() (Paths, error) // ${XDG_CACHE_HOME:-$HOME/.cache}/lighthouse; error when both are empty
  func (p Paths) Cache() string   // usage-window.json
  func (p Paths) Alarm() string   // usage-window.alarm
  func (p Paths) Lock() string    // usage-window.lock
  func (p Paths) Workdir() string // usage-refresh

  const schemaVersion = 1
  type WindowState struct {
      Pct        int    `json:"pct"`
      ResetAt    string `json:"reset_at"`    // UTC, "" without a reset
      ResetLocal string `json:"reset_local"` // "14:30" or "do 16:59", "" without a reset
  }
  type State struct {
      Schema      int         `json:"schema"`
      GeneratedAt string      `json:"generated_at"`
      Session     WindowState `json:"session"`
      Week        WindowState `json:"week"`
  }
  func NewState(w Windows, now time.Time, loc *time.Location) State
  func ReadState(path string) (State, time.Time, bool) // false: absent, unreadable, other schema or bad generated_at
  func WriteState(path string, s State) error
  func writeAtomic(path string, data []byte) error
  ```
  Dit cacheschema is het contract voor #65 (`GET /api/usage`).

- [ ] **Stap 1: Schrijf de falende tests**
  - `TestDefaultPaths` (met `t.Setenv`, niet parallel): `XDG_CACHE_HOME=/x` → `/x/lighthouse`; `XDG_CACHE_HOME=""`, `HOME=/h` → `/h/.cache/lighthouse`; `XDG_CACHE_HOME=rel` → `rel/lighthouse`; beide leeg → fout.
  - `TestNewState`: de `Windows` van `usage_full` met `now` 2026-09-05T12:00Z en Amsterdam → `State{Schema: 1, GeneratedAt: "2026-09-05T12:00:00Z", Session: {99, "2026-09-06T12:30:00Z", "14:30"}, Week: {48, "2026-09-10T14:59:00Z", "do 16:59"}}`; een sessie zonder reset → `{0, "", ""}`.
  - `TestStateRoundTrip`: `WriteState` en dan `ReadState` geeft dezelfde `State` en `generated_at` als `time.Time`; daarna staat er naast het bestand geen `.tmp` meer in `t.TempDir()`.
  - `TestReadStateRejects`: afwezig bestand, `{`, `{"schema":2,…}`, `generated_at` `"gisteren"` → alle vier `ok == false`.
  - `TestWriteAtomicKeepsOldOnFailure`: doelmap read-only (`os.Chmod(dir, 0o500)` met `t.Cleanup(func() { os.Chmod(dir, 0o700) })`, anders faalt het opruimen van `t.TempDir()`; `t.Skip` als `os.Getuid() == 0`) → fout, het oude bestand onveranderd.

- [ ] **Stap 2: Draai ze en zie ze falen**

  Run: `go test ./internal/usage/ -run 'TestDefaultPaths|TestNewState|TestStateRoundTrip|TestReadState|TestWriteAtomic' -v`
  Expected: FAIL, `undefined: DefaultPaths`

- [ ] **Stap 3: Implementeer `state.go`**

  `writeAtomic`: `os.MkdirAll(dir, 0o755)`, `os.CreateTemp(dir, "."+base+".*.tmp")`, schrijven, `Sync`, `Close`, `os.Rename`; bij elke fout het tmp-bestand weg. `NewState` formatteert met de vaste dagtabel uit de globale eisen.

- [ ] **Stap 4: Draai de tests en zie ze slagen** (dezelfde run als stap 2) — Expected: PASS

- [ ] **Stap 5: Commit** — `feat(usage): cachepaden en atomair schrijven (#64)`

### Taak 3: alarm

**Files:**
- Create: `internal/usage/alarm.go`, `internal/usage/alarm_test.go`

**Interfaces:**
- Consumes: `State`, `writeAtomic` (taak 2); `Config` (taak 4) — maak in deze taak alvast in `config.go` alleen het type en `DefaultConfig`:
  ```go
  type Config struct {
      HaltPctSession int           // default 85
      HaltPctWeek    int           // default 95
      TTL            time.Duration // default 300 s
  }
  func DefaultConfig() Config
  ```
- Produces:
  ```go
  func AlarmLine(s State, c Config) (line string, ok bool)
  func WriteAlarm(path string, s State, c Config) error // writes atomically, or removes the file when ok is false
  func PruneAlarm(path string, now time.Time) (removed bool, err error)
  ```

- [ ] **Stap 1: Schrijf de falende tests**
  - `TestAlarmLine`, tabel met `DefaultConfig()`:
    - sessie 85, week 10 → `"venster 85 14:30 2026-09-06T12:30:00Z\n"`
    - sessie 84, week 95 → `"week 95 do_16:59 2026-09-10T14:59:00Z\n"`
    - sessie 99, week 99 → regel begint met `"venster 99 "`
    - sessie 84, week 94 → `ok == false`
    - sessie 90 met lege reset (`""`, `""`) → `"venster 90 ? ?\n"`
  - `TestWriteAlarm`: boven de drempel staat het bestand er met de regel; daarna onder beide drempels is het weg; onder de drempel zonder bestand geeft geen fout.
  - `TestPruneAlarm`, `now` 2026-09-06T13:00Z:
    - reset 2026-09-06T12:30:00Z (verlopen) → weg, `removed == true`
    - reset 2026-09-06T14:00:00Z → blijft, `false`
    - inhoud `venster 90` (te weinig velden) → weg
    - reset `?` → weg
    - geen bestand → `false`, geen fout

- [ ] **Stap 2: Draai ze en zie ze falen** — `go test ./internal/usage/ -run 'Alarm' -v` — Expected: FAIL, `undefined: AlarmLine`

- [ ] **Stap 3: Implementeer `alarm.go`** — `AlarmLine` loopt `venster`/`Session`/`HaltPctSession` en dan `week`/`Week`/`HaltPctWeek`; de eerste met `Pct >= drempel` wint. `PruneAlarm` splitst met `strings.Fields`, parseert veld 4 met `time.Parse(time.RFC3339, …)`, verwijdert bij `!now.Before(reset)`.

- [ ] **Stap 4: Draai de tests en zie ze slagen** — Expected: PASS

- [ ] **Stap 5: Commit** — `feat(usage): alarmbestand schrijven en opruimen (#64)`

### Taak 4: drempels en TTL uit `host.toml`

**Files:**
- Create: `internal/hostconfig/hostconfig.go`, `internal/hostconfig/hostconfig_test.go`
- Modify: `internal/initcmd/host.go:18-29` — `defaultHostTOML` roept `hostconfig.Path()` aan; `hostLabel` houdt zijn vergelijking met `hostconfig.HomeRel`
- Modify: `internal/usage/config.go`, Create: `internal/usage/config_test.go`

**Interfaces:**
- Produces:
  ```go
  // package hostconfig
  const HomeRel = ".config/lighthouse/host.toml"
  func Path() string // $LIGHTHOUSE_HOST_TOML, else ~/HomeRel, "" without a home directory

  // package usage
  func LoadConfig(path string) (Config, error)
  ```
  Tabel in `host.toml`:
  ```toml
  [usage]
  halt_pct_session = 85
  halt_pct_week = 95
  cache_ttl_seconds = 300
  ```

- [ ] **Stap 1: Schrijf de falende tests**
  - `TestPath` (`t.Setenv`): `LIGHTHOUSE_HOST_TOML=/a/b.toml` → `/a/b.toml`; leeg en `HOME=/h` → `/h/.config/lighthouse/host.toml`.
  - `TestLoadConfig`, tabel, `want` letterlijk:
    - geen bestand → `DefaultConfig()`, geen fout
    - `path == ""` → `DefaultConfig()`, geen fout
    - bestand zonder `[usage]` → `DefaultConfig()`
    - `halt_pct_session = 70` → `{70, 95, 300s}`
    - `cache_ttl_seconds = 0` → `{85, 95, 0}` (elke refresh ververst)
    - `halt_pct_session = 0`, `halt_pct_week = 101`, `cache_ttl_seconds = -1` → `DefaultConfig()`
    - `halt_pct_session = 70`, `halt_pct_week = 101` → `{70, 95, 300s}` (terugval per sleutel, niet voor de hele tabel)
    - `[usage` (parsefout) → `DefaultConfig()` en een fout
  - De bestaande tests van `internal/initcmd` blijven groen.

- [ ] **Stap 2: Draai ze en zie ze falen** — `go test ./internal/hostconfig/ ./internal/usage/ -run 'TestPath|TestLoadConfig' -v` — Expected: FAIL, `undefined`

- [ ] **Stap 3: Implementeer** `hostconfig.Path`, de aanpassing in `initcmd/host.go`, en `LoadConfig` met `toml.DecodeFile` in `struct{ Usage struct{ HaltPctSession *int \`toml:"halt_pct_session"\`; … } }`. Pointers, zodat "niet gezet" en "0" verschillen. Geldig: drempel 1–100, TTL ≥ 0.

- [ ] **Stap 4: Draai de tests en zie ze slagen** — `go test ./internal/hostconfig/ ./internal/usage/ ./internal/initcmd/` — Expected: PASS

- [ ] **Stap 5: Commit** — `feat(usage): drempels en TTL uit host.toml (#64)`

### Taak 5: meter — refresh, tick, lock

**Files:**
- Create: `internal/usage/meter.go`, `internal/usage/meter_test.go`
- Create: `internal/usage/lock_flock.go` (`//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd`), `internal/usage/lock_other.go` (de negatie daarvan)

**Interfaces:**
- Consumes: alles uit taak 1–4.
- Produces:
  ```go
  type Outcome string
  const (
      Refreshed Outcome = "refreshed"
      Fresh     Outcome = "fresh"   // within the TTL, CLI not started
      Busy      Outcome = "busy"    // another refresh holds the lock
      Idle      Outcome = "idle"    // tick: no claude process
      Failed    Outcome = "failed"  // old cache kept; the error says why
  )
  type Meter struct {
      Paths         Paths
      Config        Config
      Now           func() time.Time
      Loc           *time.Location
      RunUsage      func(ctx context.Context, workdir string) ([]byte, error)
      ClaudeRunning func() bool
  }
  func NewMeter(p Paths, c Config) Meter // real clock, time.Local, claude and pgrep
  func (m Meter) Refresh(force bool) (Outcome, error)
  func (m Meter) Tick() (Outcome, error)

  const refreshTimeout = 60 * time.Second // deadline of the context RunUsage receives
  const waitDelay = 5 * time.Second       // cmd.WaitDelay: a grandchild holding stdout does not block Output after the kill

  func tryLock(path string) (unlock func(), held bool, err error)
  func usageCommand(ctx context.Context, workdir string) *exec.Cmd
  func claudeRunning(pgrep func() (exitCode int, err error)) bool
  ```

- [ ] **Stap 1: Schrijf de falende tests** — `m := Meter{Paths: Paths{Dir: t.TempDir()}, Config: DefaultConfig(), Now: fixed, Loc: amsterdam, RunUsage: fake, ClaudeRunning: …}`, de fake telt aanroepen en geeft een fixture terug.
  - `TestRefreshWritesCacheAndAlarm`: fixture `usage_full` → `Refreshed`, cache gelijk aan `NewState(...)`, alarm `"venster 99 14:30 2026-09-06T12:30:00Z\n"`, `RunUsage` kreeg `Paths.Workdir()` en die map bestaat. De fake controleert ook de context: `ctx.Deadline()` bestaat en `time.Until(deadline)` ligt tussen 55 s en `refreshTimeout`.
  - `TestRefreshCreatesDir`: `Paths{Dir: filepath.Join(t.TempDir(), "new", "lighthouse")}` (de map bestaat nog niet, zoals bij de eerste run op een schone host) → `Refreshed`, cache geschreven.
  - `TestRefreshRemovesAlarmBelowThreshold`: bestaand alarm, fixture `usage_no_model` → alarm weg.
  - `TestRefreshTTL`: cache met `generated_at` 299 s oud → `Fresh`, 0 aanroepen; 300 s oud → `Refreshed`; 10 s oud met `force` → `Refreshed`.
  - `TestRefreshCorruptCacheRefreshes`: cache `{` → `Refreshed`.
  - `TestRefreshKeepsOldCache`, tabel: `RunUsage` geeft een fout; geeft `usage_missing_week`; geeft `not json`; geeft `usage_full` terwijl op `Paths.Alarm()` een niet-lege map staat (het alarm schrijven faalt) → telkens `Failed`, cachebestand byte voor byte ongewijzigd; in de eerste drie gevallen ook het alarmbestand.
  - `TestRefreshBusy`: houd `tryLock(Paths.Lock())` vast in de test → `Busy`, 0 aanroepen.
  - `TestTick`, tabel: `ClaudeRunning` false → `Idle`, 0 aanroepen; true → `Refreshed`; verlopen alarm met `ClaudeRunning` false → alarm toch weg (opruimen vóór de poort).
  - `TestUsageCommand`: `Args == []string{"claude", "-p", "/usage", "--output-format", "json"}`, `Dir == workdir`, `Env` bevat `LIGHTHOUSE_USAGE_REFRESH=1` en de `PATH` van de test, `WaitDelay == waitDelay`.
  - `TestUsageCommandTimeout` (niet parallel, `t.Setenv("PATH", …)`): een tijdelijke map op `PATH` met een uitvoerbaar `claude`-script `#!/bin/sh` + `sleep 30` (de `sleep` houdt stdout open na de kill van de shell); `usageCommand` met een context van 200 ms, dan `Output()` → een fout, binnen 10 s terug. Faalt zonder `CommandContext` (30 s) en zonder `WaitDelay` (wacht op `sleep`).
  - `TestClaudeRunning`, tabel: `(0, nil)` → true; `(1, nil)` → false; `(2, nil)` → true; `(3, nil)` → true; `(0, exec.ErrNotFound)` → true.

- [ ] **Stap 2: Draai ze en zie ze falen** — `go test ./internal/usage/ -run 'TestRefresh|TestTick|TestUsageCommand|TestClaudeRunning' -v` — Expected: FAIL, `undefined: Meter`

- [ ] **Stap 3: Implementeer**
  - `Refresh`: eerst `os.MkdirAll(Paths.Workdir(), 0o755)` (maakt ook `Paths.Dir`, waar de lock in staat; fout → `Failed`), dan de lock (niet gekregen → `Busy`), dan de TTL op `ReadState` (niet ok → verversen), dan `RunUsage` met `context.WithTimeout(…, refreshTimeout)`, `Parse`, `WriteAlarm`, `WriteState`. Het alarm vóór de cache: faalt het alarm, dan is de cache nog niet aangeraakt; faalt daarna `WriteState`, dan laat `writeAtomic` de oude cache staan. Elke fout → `Failed` met die fout; een panic komt er niet uit (`defer recover` die `Failed` geeft).
  - `Tick`: `PruneAlarm` (een fout daarvan stopt de tick niet), dan `ClaudeRunning`, dan `Refresh(false)`.
  - `usageCommand`: `exec.CommandContext`, `Dir`, `Env = append(os.Environ(), "LIGHTHOUSE_USAGE_REFRESH=1")`, `WaitDelay = waitDelay`. De echte `RunUsage`: `cmd.Output()`.
  - De echte `pgrep`: `exec.Command("pgrep", "-x", "claude").Run()`; een `*exec.ExitError` geeft zijn `ExitCode()` en `nil`, elke andere fout gaat als fout door.
  - `tryLock` (flock): `os.OpenFile(path, O_CREATE|O_RDWR, 0o644)`, `syscall.Flock(fd, LOCK_EX|LOCK_NB)`; `EWOULDBLOCK` → `held == false`, geen fout. `lock_other.go`: `nil, false, errors.New("usage: file lock not supported on this platform")`.

- [ ] **Stap 4: Draai de tests en zie ze slagen** — `go test -race ./internal/usage/` — Expected: PASS

- [ ] **Stap 5: Commit** — `feat(usage): meter met refresh, tick en lock (#64)`

### Taak 6: commando, e2e en documentatie

**Files:**
- Create: `internal/cli/usage.go`, `internal/cli/usage_test.go`
- Modify: `internal/cli/root.go:37` — `root.AddCommand(newUsageCommand())`
- Create: `e2e/usage_test.go` (build tag `e2e`, gebruikt de binary die `e2e_test.go` al bouwt)
- Create: `docs/usage-venster.md`
- Modify: `README.md` — één regel met een relatieve link naar `docs/usage-venster.md`

**Interfaces:**
- Consumes: `DefaultPaths`, `LoadConfig`, `hostconfig.Path`, `NewMeter`, `Meter.Refresh`, `Meter.Tick`.
- Produces: `func newUsageCommand() *cobra.Command`; `lighthouse usage refresh [--force]`, `lighthouse usage tick`.

  Uitvoer: bij succes één regel `usage: <outcome>` op stdout; bij `Failed` of een pad- of configfout één regel `usage: <outcome>: <fout>` op stderr. `RunE` geeft altijd `nil`. Een configfout is geen stop: de meter draait met `DefaultConfig()`.

- [ ] **Stap 1: Schrijf de falende tests**
  - `TestUsageRefreshExitsZeroWithoutHome` (`internal/cli`): `t.Setenv("HOME", "")`, `t.Setenv("XDG_CACHE_HOME", "")`, `NewRoot(...).SetArgs([]string{"usage", "refresh"})` → `Execute()` geeft `nil`, stderr bevat `usage: failed:`.
  - `TestUsageHelp`: `usage refresh --help` noemt `--force`.
  - `TestUsageRefreshE2E` (`e2e`): een tijdelijke map op `PATH` met een uitvoerbaar `claude`-script dat `pwd` en `$LIGHTHOUSE_USAGE_REFRESH` naar een bestand schrijft en `testdata`-fixture `usage_full.json` print; `XDG_CACHE_HOME` en `LIGHTHOUSE_HOST_TOML` op tijdelijke paden; `TZ=Europe/Amsterdam`. Draai `usage refresh` → exit 0; `<xdg>/lighthouse/usage-window.json` heeft `session.pct == 99`; het alarm begint met `venster 99 `; het script zag cwd `<xdg>/lighthouse/usage-refresh` en `LIGHTHOUSE_USAGE_REFRESH=1`. Tweede `usage refresh` → het script draaide niet nog eens (TTL). Daarna een script dat met 1 eindigt en `usage refresh --force` → exit 0, cache ongewijzigd.
  - `TestUsageTickWithoutClaude` (`e2e`): `XDG_CACHE_HOME` en `LIGHTHOUSE_HOST_TOML` op tijdelijke paden (zoals hierboven; anders ruimt de tick het echte alarm van de ontwikkelaar op), `PATH` met een `pgrep`-script dat met 1 eindigt en een `claude`-script dat een markeerbestand maakt → `usage tick` exit 0, geen markeerbestand.

- [ ] **Stap 2: Draai ze en zie ze falen** — `go test ./internal/cli/ -run TestUsage -v && go test -tags e2e ./e2e/ -run TestUsage -v` — Expected: FAIL, `unknown command "usage"`

- [ ] **Stap 3: Implementeer `internal/cli/usage.go`** naar het patroon van `internal/cli/init.go`, en schrijf `docs/usage-venster.md`: de twee commando's, de vier paden, het alarmformaat, het cacheschema (het contract voor #65), de `[usage]`-tabel met de standaardwaarden, en dat `LIGHTHOUSE_USAGE_REFRESH=1` in de kindsessie staat.

- [ ] **Stap 4: Draai de tests en zie ze slagen** — dezelfde run als stap 2 — Expected: PASS

- [ ] **Stap 5: Commit** — `feat(cli): lighthouse usage refresh en tick (#64)`

### Taak 7: oplevering

- [ ] **Stap 1: KPI-tabel van `/lighthouse-go-writing`**, per rij met GOOS, tags, `-race` en GOWORK:

  ```bash
  git ls-files -z '*.go' | xargs -0r gofmt -l        # leeg
  go mod tidy -diff                                   # leeg
  go vet ./... && go vet -tags e2e ./e2e/             # geen uitvoer
  golangci-lint run ./...                             # 0 issues
  go test -race -shuffle=on ./...                     # ok
  go test -shuffle=on -tags e2e ./e2e/                # ok
  GOOS=darwin go vet ./internal/usage/ && GOOS=windows go build ./...   # flock en stub bouwen
  uv run pytest -q && go test -shuffle=on ./...       # test-gate
  ```

- [ ] **Stap 2: Echte meting** — `go run ./cmd/lighthouse usage refresh --force` met een echte `claude` op `PATH`; toon `usage-window.json`. Lukt de aanroep niet, meld dat dan in de PR (de fixtures bewijzen de parser, niet de echte CLI).

- [ ] **Stap 3: PR** via `/lighthouse-verify-pr` (`Closes #64`). De PR-body noemt de lezing van de AC bij `pgrep`: exitcode 2 en 3 ("kon niet zoeken") tellen als "pgrep er niet", dus de tick ververst; de bron in paia telt ze als "nee". Geen `init --upgrade`: er verandert niets onder `pack/` en niets aan wat `init` schrijft.

## Buiten dit plan

- Het opruimen van de eigen transcripts (`prune_transcripts` in paia): niet in de AC. Claude Code ruimt zelf op na `cleanupPeriodDays`.
- De modelvensters (`Current week (Fable)`), de ANSI-statusregel en `wake_cron`: geen afnemer in M1.
- De Codex-meter en `GET /api/usage` (#65), de usage-guard (#66), de timer en het uitzetten van de Python-schrijver (personal_ai_agent#1634).
