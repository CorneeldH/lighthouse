# Codex-venstermeter in de binary en GET /api/usage — implementatieplan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Doel:** `lighthouse usage codex` leest de vensters van Codex, en `GET /api/usage` geeft de vensters van Claude (uit de cache van #64) en Codex, per venster met een eigen toestand.

**Architectuur:** `internal/usage` krijgt een `CodexReader`: eerst `codex app-server` per `CODEX_HOME`, dan het sessielog als ondergrens, zoals `codex_usage.py read`. Een `Reporter` in hetzelfde pakket zet de Claude-cache en de Codex-stand om naar één `Report` met per venster `state`, `pct`, `reset_at` en `age_s`; hij onthoudt de Codex-stand kort, zodat niet elke request een app-server start. `internal/server` hangt `GET /api/usage` aan een `func(context.Context) usage.Report`; `serve` geeft de echte `Reporter` mee.

**Tech stack:** Go 1.26, Cobra, Huma v2, `os/exec`, `encoding/json`, `bufio`.

**Spec:** issue #65 (de acceptatiecriteria zijn de spec). Bron van het gedrag: `.lighthouse/scripts/codex_usage.py` (`lees_app_server`, `_wacht_op_antwoord`, `parse_app_server`, `_rollout_regels`, `lees_rollout`, `lees_stand`, `cmd_read`).

**Begeleidende skills:** `/lighthouse-go-writing` (classificatiezin vóór de eerste edit, KPI-tabel bij de oplevering) en `/lighthouse-testing` (welke productiewijziging laat elke test falen).

## Globale eisen

- App-server-protocol, letterlijk en in deze volgorde, één regel JSON per bericht op stdin:
  1. `{"id":1,"method":"initialize","params":{"clientInfo":{"name":"lighthouse-codex-usage","version":"1"}}}`, wacht op antwoord id 1;
  2. `{"method":"initialized"}`;
  3. `{"id":2,"method":"account/rateLimits/read","params":{"excludeResetCreditDetails":false}}`, wacht op antwoord id 2.
- **Nooit** `account/rateLimitResetCredit/consume`, en ook geen andere methode dan de drie hierboven.
- Commando: `codex app-server`, omgeving `os.Environ()` plus `CODEX_HOME=<home>`, stderr weg. Timeout 20 s per home, voor beide antwoorden samen. Na afloop: stdin dicht, dan SIGTERM (`cmd.Cancel`), na 2 s kill (`cmd.WaitDelay`).
- Homes, in deze volgorde: `$HOME/.codex`, `$HOME/.codex-specreview`. De eerste home met een leesbare stand wint; pas als alle homes falen volgt het sessielog. Zonder `HOME` is Codex niet meetbaar.
- Sessielog: `<home>/sessions/**/rollout-*.jsonl`. Alleen regels met `limit_id` `"codex"` of zonder `limit_id`, met een tijdzone in `timestamp` en met `primary` en `secondary` als object. De jongste regeltijd over alle homes wint; `lower_bound` is dan `true`.
- `pct` is het percentage afgekapt naar een geheel getal (`54.6` wordt `54`). Tijdstempels UTC in `2006-01-02T15:04:05Z` (`timeLayout`).
- Niet meetbaar is een fout, nooit een stand met 0%. `lighthouse usage codex`: stand als JSON op stdout en exit 0, of `not measurable: <reden>` op stderr en exit 1.
- Codex schrijft niets: geen alarm, geen cache, geen bestand in `${XDG_CACHE_HOME:-$HOME/.cache}/lighthouse`.
- Verouderd: een meting ouder dan 900 s (`> 900`, niet `>=`). Geldt voor beide bronnen.
- De `Reporter` hergebruikt een gelukte Codex-stand 300 s en een mislukte 60 s.
- Toestanden in `GET /api/usage`: `ok`, `stale`, `not_measurable`. Bij `not_measurable` zijn `pct` en `age_s` JSON `null`.
- `codex_usage.py` en alles onder `pack/` blijven ongemoeid (`budget` blijft de bron voor `review.sh`). Dus geen `init --upgrade`.
- Geen nieuwe dependency. Code en commentaar Engels; dit plan, docs en commits Nederlands.

## Review Focus

1. **Sessielog met een regel langer dan 64 KiB.** Een rollout-regel met een grote tooluitvoer mag de rest van het bestand niet stil overslaan (`bufio.Scanner` stopt daar standaard). Test in taak 2.
2. **App-server die ruis print.** Een niet-JSON-regel of een antwoord met een ander id vóór het echte antwoord wordt overgeslagen, niet als fout gelezen. Test in taak 1.
3. **App-server die hangt en SIGTERM negeert.** `Read` keert terug binnen timeout plus 2 s en laat geen proces achter. Test in taak 1.
4. **Gelijktijdige requests tijdens een trage Codex-meting.** De UI vraagt `/api/usage` vaker op; er draait dan één app-server, niet één per request. Test in taak 4.
5. **Een Claude-cache uit de toekomst** (klokscheefstand): `age_s` wordt 0, niet negatief, en de toestand is `ok`. Test in taak 4.

## Bestanden

| Bestand | Verantwoordelijkheid |
|---|---|
| `internal/usage/codex.go` | `CodexState`, `CodexReader`, de app-server-client en het parsen van zijn antwoord |
| `internal/usage/codex_rollout.go` | de terugval op het sessielog |
| `internal/usage/codex_test.go`, `codex_rollout_test.go` | met een nep-app-server als hulpproces van de testbinary |
| `internal/usage/testdata/codex/` | `app_server_response.json` en `rollout-fragment.jsonl`, byte voor byte uit `tests/pack/fixtures/codex_usage/` |
| `internal/usage/report.go`, `report_test.go` | `Report`, `Reporter`, de toestanden en de 900 s-grens |
| `internal/cli/usage.go`, `usage_test.go` | subcommando `usage codex` |
| `internal/server/server.go`, `server_test.go` | `GET /api/usage` |
| `internal/cli/serve.go` | de echte `Reporter` aan de server geven |
| `e2e/usage_test.go` | binary met een nep-`codex` op `PATH` |
| `docs/usage-venster.md`, `README.md` | gebruikersdocumentatie |

---

### Taak 1: app-server-client

**Files:**
- Create: `internal/usage/codex.go`, `internal/usage/codex_test.go`
- Create: `internal/usage/testdata/codex/app_server_response.json` (kopie van `tests/pack/fixtures/codex_usage/app_server_response.json`)

**Interfaces:**
- Produces:
  ```go
  type CodexWindow struct {
      Pct     int    `json:"pct"`
      ResetAt string `json:"reset_at"` // UTC, timeLayout
  }
  type CodexState struct {
      Source      string      `json:"source"` // "app-server" or "rollout-log"
      GeneratedAt string      `json:"generated_at"`
      AgeS        int         `json:"age_s"`  // age of the reading at GeneratedAt
      SourceFile  string      `json:"source_file,omitempty"`
      LowerBound  bool        `json:"lower_bound"`
      Session     CodexWindow `json:"session"`
      Week        CodexWindow `json:"week"`
  }
  type CodexReader struct {
      Homes   []string
      Timeout time.Duration                                    // per home
      Now     func() time.Time
      Command func(ctx context.Context, home string) *exec.Cmd // must use exec.CommandContext
  }
  func NewCodexReader() (CodexReader, error) // homes from $HOME, Timeout 20s, Now time.Now, Command codexCommand; error without HOME
  func (r CodexReader) Read(ctx context.Context) (CodexState, error)
  ```
  In deze taak probeert `Read` alleen de app-server per home; taak 2 voegt de terugval toe.

- [ ] **Stap 1: Schrijf de falende tests** in `codex_test.go`. De nep-app-server is de testbinary zelf: `TestFakeAppServer` doet `t.Skip()` zonder `LIGHTHOUSE_FAKE_APP_SERVER`; met die variabele doet hij in **elke** modus eerst `signal.Ignore(syscall.SIGTERM)`, zodat hij stdin tot EOF leegleest en elke regel logt die de client vóór het sluiten van de pipe schreef (anders racet een methode na het id-2-antwoord tegen de SIGTERM van `cmd.Cancel`). Dan leest hij stdin regel voor regel, schrijft per bericht de `method` als regel naar `$FAKE_LOG` (en eenmalig `home=$CODEX_HOME`), antwoordt op id 1 met `{"id":1,"result":{}}` en op id 2 volgens de modus, en eindigt met `os.Exit(0)` bij EOF. De fixture is meerregelig: de fake schrijft hem met `json.Compact` als één regel. Krijgt hij `account/rateLimitResetCredit/consume`, dan logt hij die en eindigt met `os.Exit(3)`. Helper `fakeReader(t, mode string, homes ...string) (CodexReader, logPath string)` met `Command` = `exec.CommandContext(ctx, os.Args[0], "-test.run=^TestFakeAppServer$")` en de env-variabelen; `Now` vast op 2026-09-28T22:00:00Z; `Timeout` 5 s.
  Modi en verwachtingen:
  - `TestCodexAppServerOK` (`ok`: antwoord id 2 = de fixture): Session `{55, "2026-09-28T23:23:29Z"}`, Week `{66, "2026-10-03T17:17:59Z"}`, Source `app-server`, AgeS 0, LowerBound false, GeneratedAt `2026-09-28T22:00:00Z`.
  - `TestCodexAppServerMethods` (`ok`): de log is exact `home=<home>\ninitialize\ninitialized\naccount/rateLimits/read\n`. Deze test faalt zodra er `consume` (of welke extra methode ook) binnenkomt.
  - `TestCodexAppServerNoise` (`noise`: eerst `niet-json`, dan `{"id":7,"result":{}}`, dan de fixture op id 2): zelfde stand als OK.
  - `TestCodexAppServerFallsBackToRateLimits` (`flat`: fixture zonder `rateLimitsByLimitId`): zelfde stand.
  - `TestCodexAppServerFloatPct` (`float`: `usedPercent` 54.6): Session.Pct 54.
  - `TestCodexAppServerRejects`, tabel, elk een fout en `CodexState{}`: `error` (`{"id":2,"error":{"message":"x"}}`), `noresult` (`{"id":2}`), `eof` (stopt na id 1), `nowindow` (`secondary` ontbreekt), `nullreset` (`resetsAt: null`), `hang` (antwoordt niet op id 2; `Timeout` 300 ms, en `Read` keert terug binnen 3 s).
  - `TestCodexAppServerStubborn` (`stubborn`: logt `pid=<os.Getpid()>`, antwoordt op id 1 en hangt daarna met `time.Sleep(time.Hour)`, ook na EOF; niet met `select {}`, want de child draait zonder `-test.timeout` en zonder andere goroutine, zodat de runtime dan `all goroutines are asleep - deadlock!` meldt en de child direct stopt; SIGTERM negeert hij zoals elke modus; `Timeout` 300 ms): fout binnen 300 ms + 2 s + 1 s marge, de fouttekst bevat `no answer within the timeout` (niet `stopped without an answer`, dat zou betekenen dat de child zelf stopte), én daarna geeft `syscall.Kill(pid, 0)` `syscall.ESRCH` (de kill na `WaitDelay` en `cmd.Wait` hebben het proces opgeruimd; een implementatie zonder `Wait` laat een zombie of een levend proces achter en faalt hier).
  - `TestCodexReaderFirstHomeWins`: homes `a` (`ok`) en `b`; de log bevat alleen `home=a`.
  - `TestCodexReaderSecondHome`: home `a` met `eof`, home `b` met `ok` (modus per home via `FAKE_MODE_<base>` of een map in de helper): stand van `b`.
  - `TestNewCodexReaderNoHome`: `t.Setenv("HOME", "")` geeft een fout.

- [ ] **Stap 2: Draai ze en zie ze falen**

  Run: `go test ./internal/usage/ -run 'Codex' -v`
  Expected: FAIL, `undefined: CodexReader`

- [ ] **Stap 3: Implementeer in `internal/usage/codex.go`**

  `codexCommand(ctx, home)`: `exec.CommandContext(ctx, "codex", "app-server")`, `Env = append(os.Environ(), "CODEX_HOME="+home)`. `readAppServer(ctx, cmd, timeout) (json.RawMessage, error)`: zet zelf `cmd.Cancel` (SIGTERM via `cmd.Process.Signal(syscall.SIGTERM)`) en `cmd.WaitDelay = 2 * time.Second`, opent stdin- en stdout-pipes, leest stdout in een goroutine met `bufio.Reader.ReadBytes('\n')` (geen regellimiet) naar een kanaal, en wacht per antwoord met `select` op dat kanaal en `ctx.Done()`. De lezer stuurt met `select` op dat kanaal en een `done`-kanaal dat `readAppServer` bij terugkeer sluit, zodat hij na EOF of een pipefout niet eeuwig blokkeert (anders lekt `serve` één goroutine per meting). De `ctx` van een home is `context.WithTimeout(parent, r.Timeout)`; de `defer` sluit stdin, roept `cancel()` aan en dan `cmd.Wait()`. Een regel die geen JSON-object is of een ander id draagt: overslaan. `error`-sleutel, ontbrekend of niet-object `result`, EOF en deadline: elk een fout met een eigen reden (de Engelse teksten uit `codex_usage.py`). `parseAppServer(result []byte, now time.Time) (CodexState, error)`: `rateLimitsByLimitId.codex`, anders `rateLimits`; `primary` → Session, `secondary` → Week; `usedPercent` en `resetsAt` als `float64`, beide verplicht. `Read` probeert de homes op volgorde en voegt de redenen samen als `<home>: <reden>; ...`.

- [ ] **Stap 4: Draai de tests en zie ze slagen**

  Run: `go test -race ./internal/usage/ -run 'Codex' -v`
  Expected: PASS

- [ ] **Stap 5: Commit**

  ```bash
  git add internal/usage/codex.go internal/usage/codex_test.go internal/usage/testdata/codex
  git commit -m "feat(usage): Codex-vensters uit codex app-server (#65)"
  ```

### Taak 2: terugval op het sessielog

**Files:**
- Create: `internal/usage/codex_rollout.go`, `internal/usage/codex_rollout_test.go`
- Create: `internal/usage/testdata/codex/rollout-fragment.jsonl` (kopie van `tests/pack/fixtures/codex_usage/rollout-fragment.jsonl`)
- Modify: `internal/usage/codex.go` (`Read`)

**Interfaces:**
- Consumes: `CodexState`, `CodexWindow`, `CodexReader` uit taak 1.
- Produces: `func readRollout(homes []string, now time.Time) (CodexState, error)`; `Read` valt erop terug.

- [ ] **Stap 1: Schrijf de falende tests**, `now` = 2026-09-28T22:00:00Z:
  - `TestReadRolloutFixture`: de fixture in `<home>/sessions/2026/09/28/rollout-x.jsonl` → Source `rollout-log`, Session `{54, "2026-09-28T23:23:29Z"}`, Week `{66, "2026-10-03T17:17:59Z"}`, AgeS 2100 (regel 21:25), LowerBound true, SourceFile het pad.
  - `TestReadRolloutNewestOverHomes`: home `a` met de fixture, home `b` met één geldige regel van 21:40 (`used_percent` 70 en 80) → Session.Pct 70, AgeS 1200, SourceFile in `b`.
  - `TestReadRolloutLongLine`: vóór de geldige regels een regel van 200 KiB met `rate_limits` erin maar zonder geldige JSON → nog steeds de stand van de fixture.
  - `TestReadRolloutSkips`, elk zonder bruikbare regel een fout: alleen `limit_id: "premium"`; alleen een `timestamp` zonder zone (`2026-09-28T21:25:00`); alleen `secondary: null`; een home zonder `sessions`; geen homes.
  - `TestCodexReadFallsBackToRollout`: `fakeReader` in modus `eof` voor de home, fixture in die home → Source `rollout-log`.
  - `TestCodexReadNotMeasurable`: modus `eof`, geen log → fout met `app-server:` én `rollout-log:` in de tekst, en `CodexState{}`.

- [ ] **Stap 2: Draai ze en zie ze falen**

  Run: `go test ./internal/usage/ -run 'Rollout|FallsBack|NotMeasurable' -v`
  Expected: FAIL, `undefined: readRollout`

- [ ] **Stap 3: Implementeer `readRollout` in `codex_rollout.go` en de terugval in `Read`**

  `filepath.WalkDir` over `<home>/sessions` (een ontbrekende map is leeg, geen fout), bestanden met `filepath.Match("rollout-*.jsonl", name)`. Per bestand `bufio.Reader.ReadBytes('\n')`; alleen regels met `rate_limits` worden ontleed. `timestamp` met `time.Parse(time.RFC3339Nano, …)`: zonder zone faalt dat, en de regel valt weg. Onleesbaar bestand: overslaan. `Read`: faalt elke home, dan `readRollout`; faalt ook dat, dan `app-server: <redenen>; rollout-log: <reden>`.

- [ ] **Stap 4: Draai de tests en zie ze slagen**

  Run: `go test -race ./internal/usage/ -v`
  Expected: PASS

- [ ] **Stap 5: Commit**

  ```bash
  git add internal/usage/codex_rollout.go internal/usage/codex_rollout_test.go internal/usage/codex.go internal/usage/testdata/codex
  git commit -m "feat(usage): Codex valt terug op het sessielog als ondergrens (#65)"
  ```

### Taak 3: `lighthouse usage codex`

**Files:**
- Modify: `internal/cli/usage.go`, `internal/cli/usage_test.go`
- Modify: `e2e/usage_test.go`
- Modify: `docs/usage-venster.md` (sectie "Codex"), `README.md:15`

**Interfaces:**
- Consumes: `NewCodexReader`, `CodexReader.Read`, `CodexState` uit taak 1–2.
- Produces: `func ReadCodex(ctx context.Context) (CodexState, error)` in `internal/usage/codex.go` (`NewCodexReader` plus `Read`); taak 5 gebruikt hem ook.

- [ ] **Stap 1: Schrijf de falende tests**
  - In `usage_test.go`: `TestPrintCodex` roept `printCodex(ctx, out, errOut, read)` aan met een nep-`read`. Gelukt: stdout is `json.MarshalIndent(state, "", "  ")` plus `\n`, stderr leeg, fout `nil`. Mislukt (`errors.New("codex not found")`): stdout leeg, stderr exact `not measurable: codex not found\n`, fout `errSilent`.
  - In `e2e/usage_test.go`, `TestUsageCodexE2E`: de test schrijft de fixture met `json.Compact` op één regel naar een tempbestand. Een nep-`codex` (via `writeScript`) leest stdin regel voor regel in een `while read -r`-lus, schrijft de `"method":"…"` van elke regel naar een logbestand, antwoordt op id 1 met `{"id":1,"result":{}}` en op id 2 met `{"id":2,"result":<compacte fixture>}`. `HOME` = een tempmap (de laatste waarde in `cmd.Env` wint). `XDG_CACHE_HOME` met een vooraf geschreven `lighthouse/usage-window.alarm` (`week 96 do_16:59 2026-09-10T14:59:00Z\n`). Verwacht: exit 0, stdout bevat `"pct": 55`, de log bevat geen `consume`, het alarm is byte voor byte gelijk en de cachemap bevat alleen het alarm.
  - `TestUsageCodexNotMeasurableE2E`: geen `codex` op `PATH` (lege bindir en `PATH` alleen die bindir plus `/usr/bin:/bin`), lege `HOME`-map: exit 1, stdout leeg, stderr begint met `not measurable: `.

- [ ] **Stap 2: Draai ze en zie ze falen**

  Run: `go test ./internal/cli/ -run TestPrintCodex -v && go test -tags e2e ./e2e/ -run UsageCodex -v`
  Expected: FAIL, `undefined: printCodex`

- [ ] **Stap 3: Implementeer**

  Subcommando `codex` in `newUsageCommand` (`Short: "Print the Codex usage windows as JSON"`, `Args: cobra.NoArgs`), `RunE` geeft `printCodex(cmd.Context(), out, errOut, usage.ReadCodex)` terug. Pas `Short`/`Long` van de groep aan: hij meet nu Claude en Codex, en alleen `refresh` en `tick` eindigen altijd met 0. Docs: sectie "Codex" in `docs/usage-venster.md` (bron, homes, terugval, uitvoer, exit 1, schrijft niets, en dat de binary vast 20 s per home wacht en `[codex_usage] app_server_timeout_seconds` uit `lighthouse.toml` niet leest, anders dan `codex_usage.py`) en `README.md:15` noemt `usage codex`.

- [ ] **Stap 4: Draai de tests en zie ze slagen**

  Run: `go test -race ./internal/cli/ && go test -tags e2e ./e2e/ -run Usage -v`
  Expected: PASS

- [ ] **Stap 5: Commit**

  ```bash
  git add internal/usage/codex.go internal/cli/usage.go internal/cli/usage_test.go e2e/usage_test.go docs/usage-venster.md README.md
  git commit -m "feat(cli): lighthouse usage codex (#65)"
  ```

### Taak 4: `Report` en `Reporter`

**Files:**
- Create: `internal/usage/report.go`, `internal/usage/report_test.go`

**Interfaces:**
- Consumes: `ReadState`, `State`, `WriteState`, `NewState` (#64); `CodexState` uit taak 1.
- Produces:
  ```go
  const StaleAfter = 900 * time.Second
  const (
      StateOK            = "ok"
      StateStale         = "stale"
      StateNotMeasurable = "not_measurable"
  )
  type WindowReport struct {
      State      string `json:"state"`
      Pct        *int   `json:"pct"`      // null when not_measurable
      ResetAt    string `json:"reset_at"` // "" without a reset
      AgeS       *int   `json:"age_s"`    // null when not_measurable
      LowerBound bool   `json:"lower_bound"`
      Reason     string `json:"reason,omitempty"`
  }
  type SourceReport struct {
      Session WindowReport `json:"session"`
      Week    WindowReport `json:"week"`
  }
  type Report struct {
      Claude SourceReport `json:"claude"`
      Codex  SourceReport `json:"codex"`
  }
  type Reporter struct {
      Cache     string // Claude cache path; "" counts as no cache
      ReadCodex func(context.Context) (CodexState, error)
      Now       func() time.Time
      // unexported: mu sync.Mutex and the remembered Codex reading
  }
  func (r *Reporter) Report(ctx context.Context) Report
  ```

- [ ] **Stap 1: Schrijf de falende tests**, `now` = 2026-09-06T10:00:00Z:
  - `TestReportClaude`, tabel over de leeftijd van een met `WriteState` geschreven cache (`usage_full`-waarden, Session 99, Week 48): 60 s → `ok`, `age_s` 60, `pct` 99, `reset_at` `2026-09-06T12:30:00Z`; 900 s → `ok`; 901 s → `stale` met `pct` 99; −120 s (toekomst) → `ok`, `age_s` 0.
  - `TestReportClaudeNotMeasurable`: geen cachebestand, kapotte JSON, `Cache` `""`: beide vensters `not_measurable`, `Pct` en `AgeS` `nil`, `Reason` niet leeg. En `json.Marshal` geeft voor zo'n venster `"pct":null`, terwijl een cache met Session 0 `"pct":0` en `"state":"ok"` geeft.
  - `TestReportCodex`: app-server-stand → `ok`, `age_s` 0, `lower_bound` false; rollout-stand met `AgeS` 2100 → `stale`, `age_s` 2100, `lower_bound` true; fout `errors.New("codex not found")` → `not_measurable`, `Reason` `codex not found`.
  - `TestReporterRemembersCodex`: tellende nep-`ReadCodex`, klok die de test verzet. Gelukt: tweede `Report` na 300 s → 1 aanroep, na 301 s → 2. Mislukt: na 60 s → 1, na 61 s → 2. Een onthouden app-server-stand van 120 s oud geeft `age_s` 120.
  - `TestReporterOneCodexAtATime`: `ReadCodex` blokkeert op een kanaal; 5 gelijktijdige `Report`-aanroepen; kanaal dicht; precies 1 aanroep.
  - `TestReporterWritesNothing`: cachemap met cache en alarm; na `Report` zijn beide byte voor byte gelijk en staan er geen andere bestanden in de map.

- [ ] **Stap 2: Draai ze en zie ze falen**

  Run: `go test ./internal/usage/ -run 'Report' -v`
  Expected: FAIL, `undefined: Reporter`

- [ ] **Stap 3: Implementeer in `report.go`**

  Leeftijd: `now − meettijd`, met meettijd = `generated_at − AgeS` (voor Claude is `AgeS` 0), onder 0 wordt 0, `> StaleAfter` is `stale`. `Report` leest Codex met `context.WithoutCancel(ctx)`, zodat een afgebroken request de onthouden stand niet bederft, en houdt de mutex vast tijdens de meting. Hergebruik: gelukt 300 s, mislukt 60 s, gerekend vanaf `Now()` bij het einde van de meting.

- [ ] **Stap 4: Draai de tests en zie ze slagen**

  Run: `go test -race ./internal/usage/ -run 'Report' -v`
  Expected: PASS

- [ ] **Stap 5: Commit**

  ```bash
  git add internal/usage/report.go internal/usage/report_test.go
  git commit -m "feat(usage): één rapport over de vensters van Claude en Codex (#65)"
  ```

### Taak 5: `GET /api/usage`

**Files:**
- Modify: `internal/server/server.go`, `internal/server/server_test.go`, `internal/server/host_test.go` (`New(testSite())` op regel 20 en 34)
- Modify: `internal/cli/serve.go:67`
- Modify: `e2e/serve_test.go`
- Modify: `docs/usage-venster.md` (sectie "API": de vorm, de drie toestanden, de 900 s-grens, en dat de service met `ProtectHome=read-only` en een kale `PATH` vaak alleen het sessielog leest)

**Interfaces:**
- Consumes: `usage.Report`, `usage.Reporter`, `usage.ReadCodex`, `usage.DefaultPaths`.
- Produces: `func New(site fs.FS, report func(context.Context) usage.Report) http.Handler`.

- [ ] **Stap 1: Schrijf de falende tests**
  - Alle bestaande `New(testSite())`, in `server_test.go` én `host_test.go`, worden `New(testSite(), fakeReport)` met een vaste `Report`.
  - In `e2e/serve_test.go`, `TestServeUsageE2E`: de echte bedrading in `serve.go`. `startRaw` krijgt een `env []string`-parameter (bestaande aanroepen geven `cleanEnv()` mee) en `startServe` geeft hem door. De test maakt een verse Claude-cache zoals `TestUsageRefreshE2E` (nep-`claude` met `usage_full.json` via `writeScript`, `usageEnv(t, xdg, binDir)`, `usage refresh`), start `serve` met diezelfde env plus `PATH=<binDir>:/usr/bin:/bin` en `HOME=<lege tempmap>` erachter (de laatste waarde in `cmd.Env` wint; `usageEnv` zet de echte `PATH` achter `binDir`, en daar kan een echte `codex` staan) en doet `GET /api/usage`. Verwacht: 200, `claude.session.state` `ok` met `claude.session.pct` 99, `codex.session.state` `not_measurable`. Een `serve.go` die `rep.Cache` niet zet of de verkeerde callback meegeeft, maakt Claude hier `not_measurable`.
  - `TestUsage`: `GET /api/usage` → 200; JSON heeft `claude.session.pct` 99 en `claude.session.state` `ok`, `codex.week.state` `not_measurable` en `codex.week.pct` aanwezig als `null` (decode in `map[string]any`, controleer `v, ok := m["pct"]; ok && v == nil`).
  - `TestRoutes`: rij `{"/api/usage", []int{200}, "", "placeholder"}`.
  - `TestOpenAPI`: `paths` bevat ook `/api/usage`.

- [ ] **Stap 2: Draai ze en zie ze falen**

  Run: `go test ./internal/server/ -v`
  Expected: FAIL, `too many arguments in call to New`

- [ ] **Stap 3: Implementeer**

  `huma.Get(api, "/api/usage", …)` met `OperationID` `get-usage`, uitvoer `struct{ Body usage.Report }`, vóór `mux.Handle("/api/", …)`. In `serve.go`: `rep := &usage.Reporter{ReadCodex: usage.ReadCodex, Now: time.Now}`; geeft `usage.DefaultPaths()` geen fout, dan `rep.Cache = paths.Cache()`. `server.New(web.FS(), rep.Report)`.

- [ ] **Stap 4: Draai de hele gate**

  Run: `uv run pytest -q && go test -race -shuffle=on ./... && go test -race -shuffle=on -tags e2e ./e2e/`
  Expected: PASS. Daarna `golangci-lint run ./...`: geen bevindingen.

- [ ] **Stap 5: Commit**

  ```bash
  git add internal/server/server.go internal/server/server_test.go internal/server/host_test.go internal/cli/serve.go e2e/serve_test.go docs/usage-venster.md
  git commit -m "feat(server): GET /api/usage met de vensters van Claude en Codex (#65)"
  ```
