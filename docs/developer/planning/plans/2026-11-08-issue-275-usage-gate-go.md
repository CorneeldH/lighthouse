# usage-gate, permission-check en md-link-check in Go — implementatieplan

> **Voor agents:** VERPLICHTE SUB-SKILL: gebruik superpowers:subagent-driven-development (aanbevolen) of superpowers:executing-plans om dit plan taak voor taak uit te voeren. Stappen hebben checkboxen (`- [ ]`).

**Doel:** de hooks `usage-gate`, `permission-check` en `md-link-check` draaien als `lighthouse hook <naam>` via `run.sh`, met hetzelfde oordeel als hun bash- en Python-voorgangers. `usage_guard.py`, `md_links.py`, de drie `.py` onder de hooks, `alarm.sh` en de drie bash-hooks verdwijnen.

**Architectuur:** twee lib-pakketten (`internal/mdlinks`, `internal/usageguard`) en een lezer van het alarm in `internal/usage`. Eén pakket `internal/hooks` registreert de drie hooks bij de runtime van stap 1 (`internal/hook`). Eerst gaan de gevallen die nu inline in pytest staan naar gedeelde fixtures, met de Python als orakel. Daarna lezen de Go-tests dezelfde fixtures. Pas dan schakelen de registraties om en gaat de Python weg.

**Tech stack:** Go (cobra, stdlib `regexp`, `os/exec`), POSIX `sh` (`run.sh`), pytest voor de black-box-tests.

**Spec:** [`2026-11-07-zonder-python-design.md`](../specs/2026-11-07-zonder-python-design.md), stap 2 van *Opdeling*, met §1, §2, §5, §9 en §10. Issue #275.

## Globale eisen

- **AC 1 "zonder dat er een verwachting is aangepast"** betekent: de functionaliteit blijft gelijk (besluit gebruiker, 2026-11-08). Een technische wijziging die de overstap van bash en Python naar Go meebrengt, mag wel. Voorbeelden: hoe een test de hook aanroept, een test op een tool die verdwijnt (`date`, `python3`, `alarm.sh`), of JSON-opmaak die een lezer toch parseert. Zo'n wijziging moet in de commit staan, met wat er technisch verandert en waarom het gedrag gelijk blijft. Een assert op wat de gebruiker of de harness ziet (oordeel, exitcode, tekst van de melding, bestanden die worden geschreven) blijft staan.
- Elke hook geeft hetzelfde oordeel en dezelfde tekst als zijn voorganger, op dezelfde payloads (spec §2). Exitcodes: `usage-gate` en `permission-check` altijd 0; `md-link-check` exit 2 met de reden op stderr.
- `COULD NOT CHECK` blijft: `md-link-check` blokkeert met `Linkcheck: COULD NOT CHECK — <reden>` op de eerste regel (anti-doel *een gate die stil verdwijnt*).
- `usage-gate` geeft hoogstens één JSON-object op stdout; een fout is één regel op stderr die met `usage-gate: ` begint.
- Registratievorm: `${CLAUDE_PLUGIN_ROOT}/hooks/run.sh <naam> [stand]` in `pack/core/hooks/hooks.json` (spec §2).
- `go-lint-gate.sh` en `ty-gate.sh` blijven bash (no-go van de milestone); alleen hun alarmlezing verandert.
- Go volgt `/lighthouse-go-standard`, tests volgen `/lighthouse-test-standard`. Code, namen en commentaar in het Engels; commitberichten in het Nederlands, Conventional Commits.
- Wie `pack/` wijzigt, draait `go run ./cmd/lighthouse init --upgrade` en commit de geïnstalleerde kopie in dezelfde commit (AGENTS.md, *Dogfood*).
- Een `minor`-fragment in `changelog.d/` (spec §10). `docs/releasing.md` regel 15: nieuw zichtbaar gedrag is minor; het fragment krijgt `type: added`, zoals dat van #274.
- Een XDG-variabele die geen absoluut pad is, telt als niet gezet. Dat volgt de XDG-spec en `internal/park`, en wijkt bewust af van `usage_guard.py` (die een relatief pad gebruikte). Geen enkele test of host leunt op een relatief XDG-pad.

## Aandachtspunten voor review

1. **Een `\r` of tab in het alarm.** `strings.Fields` ziet `\r` als witruimte, en dan wordt `venster 92 14:30 <at>\r` geldig. Bash splitst alleen op spatie, tab en newline, en `\r` bleef aan het laatste veld hangen. Verwachting: ongeldig. De fixture `carriage return` in taak 1 pint het vast, de test zit in taak 3.
2. **Twee parallelle `tool`-calls van één sessie.** Precies één spreekt (claim met `O_EXCL`). Test in taak 4: twee goroutines roepen `Decide` aan op dezelfde context, en precies één krijgt een uitvoer.
3. **Eén JSON-object, ook met een melding van de runtime.** `hook.Run` voegt bij code 0 een `systemMessage` toe (versie, mislukte build). De `prompt`-uitvoer van `usage-gate` moet dan nog steeds één object zijn, met beide sleutels. Test in taak 5.
4. **Een relatief `file_path` zonder `cwd` in de payload.** Bash nam `$PWD`. Verwachting: oplossen tegen de werkmap van het hookproces. Test in taak 5.
5. **Een `.md`-bestand dat niet leesbaar is.** Dat is geen "geen problemen", maar `COULD NOT CHECK` met exit 2. Vroeger viel dit onder "de parser crasht". `test_linkcheck_fails_loud_when_parser_crashes` blijft daarom staan en moet via `run.sh` groen zijn (taak 7).

---

## Bestandsoverzicht

| Pad | Rol |
|---|---|
| `tests/pack/fixtures/md_links/links.json` | gevallen van `strip_code`, `relative_links`, `absolute_links`, `find_problems` |
| `tests/pack/fixtures/usage_alarm/cases.json` | alarmregels en of ze geldig zijn |
| `tests/pack/fixtures/usage_guard/{keys,speech,wake_cron}.json` | sleutelnamen, de exacte teksten van elke spreker, de wekcron |
| `internal/mdlinks/mdlinks.go` (+`_test.go`) | de linkextractie |
| `internal/usage/alarm.go` (+`_test.go`) | `ReadAlarm` naast de bestaande schrijver |
| `internal/cli/usage.go` (+`_test.go`) | `lighthouse usage alarm` |
| `internal/usageguard/{context,speak,decide}.go` (+`_test.go`) | wie spreekt, wat er gezegd en geschreven wordt |
| `internal/hooks/{usagegate,permissioncheck,mdlinkcheck}.go` (+`_test.go`), `detach_unix.go`, `detach_other.go` | de drie hooks, geregistreerd in `init()` |
| `internal/cli/hook.go` | blanco import van `internal/hooks` |
| `pack/core/hooks/hooks.json`, `pack/core/requires.toml`, `pack/SOURCES.toml` | registraties en inventaris |
| `pack/go/hooks/go-lint-gate.sh`, `pack/python/hooks/ty-gate.sh` | alarm via `lighthouse usage alarm` |
| `tests/pack/_testkit.py`, `tests/conftest.py` | binary één keer bouwen, hooks via `run.sh` draaien |
| `pack/core/lib/bash_writes.py`, `tests/pack/test_bash_writes.py`, `tests/pack/test_bash_write_check.py` | `bash-write-check` draait de linkcheck via `run.sh md-link-check` |
| `docs/site/data/{session,loop,config,packs}.json`, `docs/site/map.js` | sitekaarten volgen `hooks.json` en de nieuwe bronnen |
| `pack/core/skills/start-issue/agents/SKILL.md`, `pack/core/skills/fix-pr/**/SKILL.md` | verwijzing naar `md-link-check.sh` |
| `CLAUDE.md`, `docs/usage-gate.md`, `docs/host-assumptions.md`, `changelog.d/issue-275-usage-gate-go.md` | tekst |

---

### Taak 1: gedeelde fixtures, met de Python als orakel

Spec §9: eerst de gevallen naar fixtures, en pas daarna overzetten. In deze taak verandert geen productiecode.

**Bestanden:**
- Maak: de vijf JSON-bestanden uit het overzicht.
- Wijzig: `tests/pack/test_md_links.py`, `tests/pack/test_usage_gate_lib.py`, `tests/pack/test_usage_gate_resume.py` (`test_wake_cron`), `tests/pack/test_usage_gate.py` (`ALARMS`, `test_alarm_cases`).

**Interfaces (vormen die taken 2–4 lezen):**
- `links.json`: `{"strip_code": [{"name", "text", "expect": str}], "relative": [{"name", "text", "expect": [str]}], "absolute": [{"name", "text", "expect": [str]}], "problems": [{"name", "files": {relpad: inhoud}, "dirs": [relpad], "md": relpad, "expect": [[kind, target]]}]}`. Gebruik in de tekst nooit drie backticks letterlijk: `md_links.py` legt uit waarom. Schrijf ze in JSON als `` ``` ``.
- `cases.json`: `[{"name", "setup": "absent"|"file"|"symlink"|"unreadable", "line": str, "valid": bool, "fields": [kind, pct, local, at] | null}]`. In `line` staat `{future}` voor nu + 1 uur en `{past}` voor nu − 1 minuut, als `YYYY-MM-DDTHH:MM:SSZ`; `fields` gebruikt dezelfde plaatshouders. `local` in `fields` heeft spaties in plaats van `_`.
- `keys.json`: `[{"name", "kind": "alarm"|"pause"|"handoff", "stand", "payload": {}, "run_id", "pause_until", "now", "expect": pad relatief aan state- of resume-map}]`.
- `speech.json`: `[{"name", "speaker": "base"|"subagent"|"resume"|"controller"|"pause", "stand", "alarm": {"kind","pct","reset_local","reset_at"} | null, "session_id", "agent_id", "run_id", "pause_until", "now", "expect": {uitvoerobject}}]`. In `expect` staat `{resume}` voor de resume-map. Draai met `TZ=UTC`.
- `wake_cron.json`: `[{"reset_at", "tz", "expect"}]`.

- [ ] **Stap 1: schrijf de fixtures.** Neem elk geval over dat nu inline staat: de parametrisaties en asserts van `test_md_links.py`; `ALARMS` plus het geval van `_helper_fields` uit `test_usage_gate.py`; de sleutelvormen, `handoff_path` en elke tekst die een test van `test_usage_gate_lib.py` controleert; `test_wake_cron`. De verwachte waarden komen uit de huidige Python (het orakel). Leg ze naast de oude inline asserts: elke oude assert moet met een fixture-waarde overeenkomen.
- [ ] **Stap 2: laat de Python-tests de fixtures lezen.** Elke genoemde test wordt geparametriseerd over zijn fixture, met dezelfde assert als nu. Testen die toestand opbouwen (claim, prune, "eenmaal per venster") blijven zoals ze zijn: taak 4 zet ze 1-op-1 over naar Go.
- [ ] **Stap 3: draaien.** `uv run pytest -q tests/pack/test_md_links.py tests/pack/test_usage_gate.py tests/pack/test_usage_gate_lib.py tests/pack/test_usage_gate_resume.py` → alles groen, met evenveel of meer tests dan vóór de taak.
- [ ] **Stap 4: mutatiecheck.** Verander één `expect` in elk van de vijf bestanden en zie de bijbehorende test rood worden. Zet het daarna terug.
- [ ] **Stap 5: commit.** `test(usage-gate): gevallen van md_links en usage_guard naar gedeelde fixtures (#275)`

### Taak 2: `internal/mdlinks`

**Bestanden:** maak `internal/mdlinks/mdlinks.go` en `internal/mdlinks/mdlinks_test.go`.

**Interfaces:**
- Produceert:
  - `func StripCode(text string) string`
  - `func RelativeLinks(text string) []string`
  - `func AbsoluteLinks(text string) []string`
  - `type Problem struct{ Kind, Target string }`, met `Kind` gelijk aan `"dead"` of `"absolute"`
  - `func FindProblems(mdPath, root string) ([]Problem, error)`; een leesfout gaat terug naar de aanroeper

- [ ] **Stap 1: test.** `TestFixtures` leest `../../tests/pack/fixtures/md_links/links.json` (het patroon van `internal/config/parity_test.go`). Per sectie één subtest per geval. Voor `problems` bouwt de test `files` en `dirs` in `t.TempDir()`, en vergelijkt hij `FindProblems(<tmp>/<md>, <tmp>)` met `expect`.
- [ ] **Stap 2: draaien.** `go test ./internal/mdlinks/` → FAIL (pakket bestaat niet).
- [ ] **Stap 3: implementeer.** Neem de regex `LINK` uit `pack/core/lib/md_links.py` letterlijk over: RE2 kent benoemde groepen, en lookaround is niet nodig. Fences wisselen per regel, net als in `strip_code`. Decodeer met `url.PathUnescape`; geeft dat een fout (`100%.md`), gebruik dan het ruwe doel, zoals `unquote` in Python. Voeg in taak 1 gevallen toe aan `links.json` voor een ongeldige `%`, CRLF en NBSP, zodat beide kanten ze pinnen. Valt het doel buiten `root`, dan slaat de functie het over: `filepath.Rel` mag niet met `..` beginnen. Een map telt als bestaand.
- [ ] **Stap 4: draaien.** `go test -race ./internal/mdlinks/` → PASS.
- [ ] **Stap 5: commit.** `feat(mdlinks): linkextractie voor markdown in Go (#275)`

### Taak 3: `usage.ReadAlarm` en `lighthouse usage alarm`

**Bestanden:**
- Wijzig: `internal/usage/alarm.go`, `internal/usage/alarm_test.go`, `internal/cli/usage.go`, `internal/cli/usage_test.go`.

**Interfaces:**
- Produceert:
  - `type Alarm struct{ Kind string; Pct int; ResetLocal string; ResetAt string }`; `ResetLocal` heeft spaties
  - `func ReadAlarm(path string, now time.Time) (Alarm, bool)`
  - het commando `lighthouse usage alarm`: exit 0 bij een geldig alarm, met op stdout de regel `<kind> <pct> <reset_local met _> <reset_at>\n`; exit 1 zonder uitvoer bij elk ander geval. Hook en commando lezen beide `usage.DefaultPaths().Alarm()`, hetzelfde pad als waar de meter schrijft (ook bij een relatieve `XDG_CACHE_HOME`). Kan `DefaultPaths` geen pad maken, dan volgt één regel `usage: <fout>` op stderr en exit 1.

De regels van `alarm.sh`:
- alleen een gewoon bestand (`os.Lstat`, geen symlink);
- alleen de eerste regel telt;
- velden gesplitst op spatie en tab, nooit op `\r` (aandachtspunt 1);
- precies vier velden;
- kind `venster` of `week`;
- pct alleen cijfers (past het niet in een `int`, dan is het geen alarm);
- `reset_at` precies `^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$` en later dan `now`.

- [ ] **Stap 1: test.** `TestReadAlarmFixtures` leest `usage_alarm/cases.json`, maakt per `setup` het bestand (`unreadable`: mode 0; sla dit geval over als `os.Geteuid() == 0`) en vergelijkt `ok` en de velden. `TestUsageAlarmCommand`: een geldig alarm geeft exit 0 en precies de regel; geen bestand geeft exit 1 en lege stdout en stderr.
- [ ] **Stap 2: draaien.** `go test ./internal/usage/ ./internal/cli/ -run 'Alarm'` → FAIL.
- [ ] **Stap 3: implementeer** `ReadAlarm` en het subcommando `alarm` in `newUsageCommand`. Exit 1 gaat via `errSilent`, zoals bij `codex`.
- [ ] **Stap 4: draaien.** `go test -race ./internal/usage/ ./internal/cli/` → PASS.
- [ ] **Stap 5: commit.** `feat(usage): lighthouse usage alarm leest het alarm met de regels van alarm.sh (#275)`

### Taak 4: `internal/usageguard`

Zet `usage_guard.py` en de modules `resume.py` en `controller.py` over. De woordkeus wordt letterlijk overgenomen.

**Bestanden:** maak `internal/usageguard/context.go`, `speak.go`, `decide.go` en de bijbehorende `_test.go`.

**Interfaces:**
- Gebruikt: `usage.Alarm` (taak 3); `park.MarkerDir`, `park.Marker`; `config.Path`, `config.Load`, `config.Lookup`.
- Produceert:
  - `type Context struct{ Stand string; Alarm *usage.Alarm; SessionID, AgentID, Cwd, RunID, PauseUntil, PauseFlag string; StopHookActive, PayloadOK bool; Now time.Time; Loc *time.Location }`
  - `func BuildContext(stand string, alarm *usage.Alarm, pauseUntil, pauseFlag string, payload []byte, now time.Time, getenv func(string) string) Context`. `RunID` komt uit `LIGHTHOUSE_CONTROLLER_RUN` als die op `^[0-9a-f-]+$` past. Ids passen op `^[A-Za-z0-9_-]{1,128}$`, anders zijn ze `""`. `Loc` is `time.Local`; de sprekers resume (wekcron) en controller (klok van een bestaande alarmmarker) gebruiken `c.Loc`, zoals `wake_cron(…, None)` en `at.astimezone()` lokale tijd gebruikten. `Cwd` valt terug op `os.Getwd()`. Bij een payload die niet te lezen is, is `StopHookActive` waar.
  - `type Dirs struct{ State, Resume string }` en `func DirsFromEnv(getenv func(string) string) (Dirs, error)`. `State` is `<XDG_STATE_HOME|$HOME/.local/state>/lighthouse/usage-guard`; `Resume` is `park.MarkerDir`.
  - `func EffectiveStand(c Context) string`
  - `func AlarmKey(c Context, stand string, d Dirs) string`
  - `func PauseKey(c Context, d Dirs) string`
  - `func HandoffPath(c Context, d Dirs) string`
  - `func WakeCron(resetAt string, loc *time.Location) (string, error)`
  - `type Options struct{ Resume, Controller bool }`
  - `func ReadOptions(warn func(string)) Options`. Een configfout geeft `warn("config: " + err.Error())` en beide uit.
  - `type Decision struct{ Output map[string]any; KeyAfter string }`
  - `func Decide(c Context, d Dirs, opts func() Options, warn func(string)) Decision`. De stappen 1–7 en de foutpaden staan in `decide()` van `usage_guard.py`. Een spreker die faalt, geeft de claim vrij.
  - De sprekers zijn intern: base, subagent, resume (schrijft een marker met `handoff_path`), controller en pause (schrijft een marker met `"controller": true`; het latere hervatmoment wint, volgens `_park`). Markers worden atomair geschreven: tmp in dezelfde map en dan rename, JSON met inspringing 2 en een newline aan het eind.

- [ ] **Stap 1: tests.**
  - `TestKeysFixtures`, `TestSpeechFixtures` en `TestWakeCronFixtures` lezen de fixtures van taak 1. `{resume}` wordt de resume-map van `t.TempDir()`; de tests zetten `Context.Loc = time.UTC`. De Python-fixtures uit taak 1 draaien met `TZ=UTC` én `time.tzset()` (monkeypatch), want de tests draaien in-process.
  - Zet elke toestandstest uit `test_usage_gate_lib.py` en `test_usage_gate_controller.py` die niet op een fixture leunt, over als Go-test met dezelfde naam in CamelCase en dezelfde asserts. Daaronder vallen claim, prune ouder dan 14 dagen, stop met en zonder `stop_hook_active`, pause vóór alarm, een later pause-einde dat wint, en een onleesbare marker die overschreven wordt.
  - Spreker faalt: maak de resume-map onschrijfbaar (`XDG_DATA_HOME` wijst naar een bestand). Voor resume, controller en pause geldt: de claimsleutel is weg, `Output` is nil (bij een falende pause spreekt daarna het alarm) en `warn` is precies één keer geroepen. Dit vervangt `test_failing_module_releases_key`, `test_bad_module_output_releases_key`, `test_failing_pause_falls_through_to_alarm` en `test_bad_pause_output_falls_through_to_alarm`. De tests die Python-modules injecteren of `main(argv)` aanroepen (`test_module_output_is_the_only_output`, `test_module_may_define_a_dataclass`, `test_subagent_never_loads_a_module`, `test_main_never_fails`) vervallen met hun premisse; noem ze in de commit.
  - `TestParallelToolSpeaksOnce` (aandachtspunt 2): twee goroutines, één `Output`.
  - `TestRelativeXDGIsIgnored`: `XDG_STATE_HOME=rel` geeft een `State` onder `$HOME/.local/state`.
- [ ] **Stap 2: draaien.** `go test ./internal/usageguard/` → FAIL.
- [ ] **Stap 3: implementeer** volgens de interfaces.
- [ ] **Stap 4: draaien.** `go test -race ./internal/usageguard/` → PASS. Mutatiecheck: verander één woord in de stop-reden, en `TestSpeechFixtures` moet rood worden.
- [ ] **Stap 5: commit.** `feat(usageguard): de beslisser van de usage-gate in Go (#275)`

### Taak 5: de drie hooks in `internal/hooks`

**Bestanden:**
- Maak: `internal/hooks/usagegate.go`, `permissioncheck.go`, `mdlinkcheck.go`, `detach_unix.go` (`//go:build unix`, `SysProcAttr{Setsid: true}`), `detach_other.go` (`//go:build !unix`, geen attributen), en de bijbehorende `_test.go`.
- Wijzig: `internal/cli/hook.go` (`import _ "github.com/CorneeldH/lighthouse/internal/hooks"`).

**Interfaces:**
- Gebruikt: `hook.Register`, `hook.Func`, `hook.Result`, en taken 2–4.
- Produceert: de hooks `usage-gate` (stands `prompt|tool|subagent|stop`), `permission-check` en `md-link-check`, elk geregistreerd in `init()`.

Gedrag per hook, telkens zoals de voorganger:

- **`usage-gate`**
  - Met `LIGHTHOUSE_USAGE_REFRESH=1` of een onbekende stand: code 0, niets.
  - Bij `prompt` zoekt de hook `lighthouse` op het PATH (`exec.LookPath`, niet `os.Executable`: zo werkt het zoals bash en zoals de testcode met een nep-binary). Hij start `lighthouse usage refresh` los van zichzelf, met `LIGHTHOUSE_USAGE_REFRESH=1` en stdin, stdout en stderr op nil, en wacht niet (`Process.Release`).
  - De pauzevlag telt alleen in `tool` met een `RunID`: `park.PauseFile` plus `park.ReadPause`. `PauseUntil` is de eerste regel van de vlag zoals hij er staat. `park.ReadPause` leest alleen RFC 3339 met seconden; de bash-versie accepteerde meer (`date -d`). Dat is een technische wijziging van de overstap (`WritePause` schrijft altijd RFC 3339): zet het in de commit.
  - Daarna leest de hook het alarm en geldt het snelle pad. Dan volgen `usageguard.Decide` en `KeyAfter`, dat de hook schrijft voordat hij teruggeeft.
  - Elke waarschuwing wordt `usage-gate: <tekst>\n` in `Result.Stderr`.
- **`permission-check`**
  - Buiten een controller-run (`LIGHTHOUSE_CONTROLLER_RUN` is leeg of geen uuid-vorm) gebeurt niets.
  - Is `LIGHTHOUSE_STATE_DIR` leeg, dan volgt `permission-check: COULD NOT CHECK — LIGHTHOUSE_STATE_DIR is empty\n`.
  - Verder doet de hook wat `note.py` doet: dezelfde soorten, de nieuwste `tool_use` uit het transcript en uit `subagents/*.jsonl`, de regel van 80 tekens voor Bash, en `at` als RFC 3339 UTC met milliseconden. Het bestand krijgt mode 0600 en wordt geschreven als tmp plus rename.
  - Code altijd 0, nooit `Output`.
- **`md-link-check`**
  - De volgorde en teksten van `md-link-check.sh` gelden, met `cannot_check` als exit 2 en het blok van vier regels. Ontbreekt `git` (`exec.LookPath`), dan is de melding `missing tooling: git`.
  - Een payload die geen JSON is, geeft `the payload is not valid JSON.`.
  - Alleen `.md` en `.markdown` worden gecontroleerd. Het pad wordt opgelost tegen `cwd` uit de payload, en anders tegen `os.Getwd()`. Is het geen gewoon bestand, dan volgt exit 0. Anders gaat het pad door `filepath.EvalSymlinks`.
  - `git -C <dir> rev-parse --show-toplevel` draait met `LC_ALL=C`. Bevat de fout `not a git repository`, dan volgt exit 0.
  - De trace-regel verschijnt bij `LIGHTHOUSE_LINKCHECK_TRACE`.
  - Een leesfout van `FindProblems` geeft `the link parser failed.` met de fout als tweede alinea.
  - De probleemregels en de slotregel `In: <pad>` zijn letterlijk die van bash.

- [ ] **Stap 1: tests.**
  - Per hook een tabeltest die de `hook.Func` direct aanroept, met `t.Setenv` en `t.TempDir()`.
  - `usage-gate`: refresh met een nep-`lighthouse` in een tijdelijke PATH-map die zijn argumenten en `LIGHTHOUSE_USAGE_REFRESH` naar een bestand schrijft en 2 s slaapt. De hook is binnen 1 s terug en het bestand krijgt `usage refresh|1`. Zonder `lighthouse` op het PATH: geen stderr (voorheen `test_refresh_without_lighthouse`).
  - Aandachtspunt 3: `hook.Run` met `Env{ManifestVersion: "0.1.0", BinaryVersion: "0.2.0", StateDir: t.TempDir()}` geeft bij `prompt` met een alarm één JSON-regel met `hookSpecificOutput` en `systemMessage`.
  - Aandachtspunt 4: een relatief `file_path` zonder `cwd` wordt opgelost tegen `t.Chdir(dir)`.
- [ ] **Stap 2: draaien.** `go test ./internal/hooks/` → FAIL.
- [ ] **Stap 3: implementeer.**
- [ ] **Stap 4: draaien.** `go test -race ./internal/hooks/ ./internal/hook/ ./internal/cli/` → PASS. Daarna `go build ./... && GOOS=windows go vet ./internal/hooks/` → schoon.
- [ ] **Stap 5: commit.** `feat(hooks): usage-gate, permission-check en md-link-check als lighthouse hook (#275)`

### Taak 6: `go-lint-gate` en `ty-gate` lezen het alarm met `lighthouse usage alarm`

Deze taak gaat vóór de omschakeling, zodat `alarm.sh` pas verdwijnt als niemand hem nog leest.

**Bestanden:**
- Wijzig: `pack/go/hooks/go-lint-gate.sh` (regels 26–34 commentaar, 71–80), `pack/python/hooks/ty-gate.sh` (regels 26, 102–110), `tests/pack/go/test_go_lint_gate.py`, `tests/pack/python/test_ty_gate.py`, `tests/pack/_testkit.py`, `tests/conftest.py`.

**Interfaces:**
- Gebruikt: `lighthouse usage alarm` (taak 3).
- Produceert, in `_testkit.py`:
  - `def lighthouse_dir() -> Path`: bouwt `./cmd/lighthouse` één keer per proces (`functools.cache`) in een tijdelijke map, met opruimen via `atexit`. `tests/conftest.py::_lighthouse_build_dir` geeft voortaan deze map terug, zodat er geen tweede build komt.

Het nieuwe blok in beide gates:

```bash
if command -v lighthouse >/dev/null 2>&1 && ALARM_LINE=$(lighthouse usage alarm 2>/dev/null); then
  read -r _ _ ALARM_LOCAL _ <<<"$ALARM_LINE"
  report "$HOOK_NAME: not checked — the usage alarm is up until ${ALARM_LOCAL//_/ }; the usage gate wraps up this session."
fi
```

- [ ] **Stap 1: tests aanpassen in de opzet, niet in de asserts.** `_alarm_env` en `_alarm_repo` zetten `lighthouse_dir()` vooraan op het PATH. `test_without_core_a_valid_alarm_still_blocks` heeft een premisse die verdwijnt: zonder `pack/core` stond er geen `alarm.sh`. De test heet voortaan `test_without_lighthouse_a_valid_alarm_still_blocks`. Hij haalt `lighthouse` uit het PATH en houdt dezelfde assert. Voeg in beide testbestanden `test_alarm_is_read_with_lighthouse_usage_alarm` toe: een nep-`lighthouse` op het PATH die op `usage alarm` exit 0 geeft met `venster 92 9_15 2099-01-01T00:00:00Z`, zonder alarmbestand. De verwachte melding noemt `until 9 15`. Dat bewijst AC 3.
- [ ] **Stap 2: draaien.** `uv run pytest -q tests/pack/go/test_go_lint_gate.py tests/pack/python/test_ty_gate.py` → de nieuwe test is rood.
- [ ] **Stap 3: vervang** in beide gates het `alarm.sh`-blok en zijn commentaar door het blok hierboven. Laat het `python3` in de toollus van `go-lint-gate.sh:56-58` staan: dat hoort bij `turn_roots.py`, en dat is stap 5 van de spec.
- [ ] **Stap 4: draaien.** Dezelfde pytest-opdracht → groen. Daarna `go run ./cmd/lighthouse init --upgrade` en `git status` → de geïnstalleerde `go-lint-gate.sh` is bijgewerkt.
- [ ] **Stap 5: commit.** `feat(go,python): stop-gates lezen het usage-alarm met lighthouse usage alarm (#275)`

### Taak 7: omschakelen naar `run.sh` en de Python weghalen

**Bestanden:**
- Wijzig:
  - `pack/core/hooks/hooks.json`: de zes registraties uit het scout-pakket worden `${CLAUDE_PLUGIN_ROOT}/hooks/run.sh md-link-check`, `… run.sh usage-gate tool|prompt|subagent|stop` en `… run.sh permission-check`, met dezelfde timeouts en matchers.
  - `pack/core/requires.toml`: haal de drie hooks en de twee modules weg uit de lijsten van `python3`, `tomllib`, `git` en `jq`. Voeg ze toe bij `git` als ze die nodig hebben (`md-link-check`).
  - `pack/SOURCES.toml`: haal de verwijderde bestanden weg.
  - `internal/packs/core_test.go`: de lijst van geïnstalleerde bestanden.
  - `tests/pack/_testkit.py`:
    - `run_guard` draait `run.sh usage-gate <stand>` met een shimmap vooraan op het PATH. Daarin staat een `lighthouse` die voor `hook …` de gebouwde binary uitvoert en voor al het andere (`usage refresh`) zijn argv en `LIGHTHOUSE_USAGE_REFRESH` in een traceboek schrijft en niets doet. De refreshtests (`test_refresh_is_detached`, `test_refresh_only_at_prompt`, `test_usage_refresh_env_silences`) lezen dat traceboek in plaats van hun eigen stub. Zo start een test nooit een echte `claude -p /usage` (`guard_env`: "no real `lighthouse` on PATH").
    - `run_sh_hook` zet altijd de shimmap van `run_guard` vooraan op het PATH (parameter `with_binary=False` voor de test zonder binary), niet de kale binary: ook `test_hooks_minimal_config` draait de registratie `usage-gate prompt`, en die mag nooit een echte `lighthouse usage refresh` en dus `claude -p /usage` starten, ook in de tests die het PATH zelf vervangen (`test_linkcheck_without_git`, `test_linkcheck_git_failure_is_unchecked`, `test_linkcheck_invalid_json_is_unchecked`, `test_hooks_minimal_config`), plus `RUN_SH_TOOLS`.
    - Er komt `def run_sh_hook(root: Path, name: str, payload: dict | str, *, cwd: Path, env: dict | None = None, args: tuple[str, ...] = ()) -> subprocess.CompletedProcess`.
    - Er komt `RUN_SH_TOOLS = ("sh", "dirname", "grep", "sed", "awk", "cat", "mktemp", "rm")`, voor tests met een kaal PATH.
  - `pack/core/lib/bash_writes.py`: `GATES` (regels 53–54) en `GATE_PACKS` (regel 66) noemen `md-link-check.sh` en `run_gate` draait `.claude/hooks/lighthouse/<gate>` als bestand. Laat de linkcheck draaien als `run.sh md-link-check` (argv in plaats van bestandsnaam; pas `gate_path`, `run_gate` en het label aan, hou de labeltekst die gebruikers zien gelijk). Anders meldt `bash-write-check` na het verwijderen alleen "not checked — no core check installed" en verdwijnt de gate stil. Pas `tests/pack/test_bash_writes.py` (57–58, 1326) en `tests/pack/test_bash_write_check.py` (88–113, 463, 514, 768) aan op de nieuwe aanroep, en voeg een test toe dat een met Bash geschreven `.md` met een dode link nog steeds blokkeert. Die tests (`_gate` en `_Session` in `test_bash_write_check.py:55-80`) gebruiken `os.environ["PATH"]`: laat ze de fixture `lighthouse_bin` (`tests/conftest.py:62-66`) nemen en `lighthouse_dir()` vooraan op het PATH zetten, anders meldt `run.sh` `COULD NOT CHECK: lighthouse not on PATH` en gaat elke linkcheck-test rood.
  - Sitekaarten: `docs/site/data/session.json` (registraties `usage-gate.sh <stand>`, `md-link-check.sh`, `permission-check.sh` worden `run.sh <naam> [stand]`; `source` wordt `internal/hooks/<naam>.go`), `docs/site/data/loop.json` (220–227, 463), `docs/site/data/config.json` (687–703: de lezers van `usage_guard.*` worden `internal/usageguard/*.go`), `docs/site/data/packs.json` en `docs/site/map.js:216` (href naar een Go-hook). Pas `tests/test_site_session.py` (`hook_problems`, bron moet eindigen op `hooks/<command.split()[0]>`) aan op de nieuwe vorm.
  - `pack/core/skills/start-issue/agents/SKILL.md:312` en `pack/core/skills/fix-pr/*/SKILL.md:273`: de verwijzing naar `md-link-check.sh` wordt `run.sh md-link-check`.
  - `tests/pack/test_md_link_check.py`, `test_permission_check.py`, `test_usage_gate*.py`, `test_hooks_minimal_config.py`: alles via `run.sh` en de hooknaam in plaats van het `.sh`-pad.
  - `pack/core/scripts/check-instructions.sh` (regex regel 715, `suffix` regel 748) en `tests/pack/test_check_instructions.py`: onvoorwaardelijk. Bij `run.sh <naam>` is de hooknaam het eerste argument, anders faalt de check op `'run' ends on neither -gate nor -check`. Voeg een testgeval voor die vorm toe.
- Verwijder:
  - `pack/core/lib/usage_guard.py`, `pack/core/lib/md_links.py`
  - `pack/core/hooks/usage-gate.sh`, `usage-gate/alarm.sh`, `usage-gate/controller.py`, `usage-gate/resume.py`
  - `pack/core/hooks/permission-check.sh`, `permission-check/note.py`, `md-link-check.sh`
  - `tests/pack/test_usage_gate_lib.py`, `tests/pack/test_md_links.py`

**Tests die verdwijnen omdat hun premisse verdwijnt.** De lijst is gesloten. Elke andere test houdt zijn asserts; alleen hoe de hook wordt aangeroepen, verandert.

| Test | Premisse die weg is | Waar het gedrag nu bewezen wordt |
|---|---|---|
| `test_usage_gate.py::test_fast_path_runs_nothing` | bash forkt geen `python3`/`jq`/`date` | n.v.t.: de Go-hook roept die tools niet aan |
| `test_usage_gate.py::test_bsd_date`, `test_usage_gate_controller.py::test_pause_bsd_date`, `::test_usage_gate_parses_until_with_bsd_date_only` | BSD-`date` | `time.Parse` in taken 3 en 5 |
| `test_usage_gate.py::test_missing_python` | `python3` | n.v.t. |
| `test_usage_gate.py::test_guard_without_alarm_helper_cannot_check`, `::test_guard_without_alarm_helper_and_without_alarm_is_silent`, `::test_alarm_helper_fields_under_set_u` | `alarm.sh` | fixture `usage_alarm` in taak 3 |
| `test_usage_gate.py::test_refresh_without_lighthouse` | geen binary maar wel een hook | taak 5 (Go) en `test_run_sh.py` |
| `test_md_link_check.py::test_linkcheck_unresolvable_path` | `realpath`/`python3 -c` | n.v.t. |
| de assert `load_hook_lib("md_links").absolute_links(...)` in `test_linkcheck_allows_link_to_branch_only_spec` (regel 95) | `md_links.py` | fixture `md_links` in taak 2; de hook-assert van die test blijft |
| `test_bash_write_check.py::test_linkcheck_unchecked_form_is_on_the_first_line` | de premisse "jq ontbreekt" (88–113) | de test houdt de binary (`lighthouse_bin`) en `RUN_SH_TOOLS` op het PATH maar laat `git` weg, zoals `test_linkcheck_without_git`; dan draait de Go-hook zelf en meldt `Linkcheck: COULD NOT CHECK — <reden>` met exit 2. Beide asserts (rc 2, eerste regel met `Linkcheck: COULD NOT CHECK — `) blijven staan. Laat de binary niet weg: `run.sh` meldt dan zijn eigen `COULD NOT CHECK: lighthouse not on PATH` zonder `Linkcheck:` |
| `test_usage_gate_resume.py::test_wake_cron` en de regel `cron = _load().wake_cron(...)` in `test_tool_window_writes_marker` | `resume.py` | fixture `wake_cron.json` (taak 4); de marker-asserts van die test blijven en lezen de cron uit de fixture |
| `test_usage_gate_controller.py::test_pause_invalid_flag_without_alarm_runs_nothing` | `usage-gate.sh` rechtstreeks en `python3` niet forken | een variant via `run.sh` houdt de asserts op lege stdout/stderr en geen marker |
| `test_skills_issue_flow.py::test_guard_names_the_pause_form` | brontekst van `usage_guard.py` en `resume.py` | `speech.json` bevat `/lighthouse-write-handoff ` en `.handoff.md` |
| `test_usage_gate_lib.py`, `test_md_links.py` (hele bestanden) | de Python-module | taken 2 en 4 |

`test_usage_gate.py::test_no_paia_in_guard_files` blijft, met als padenlijst `internal/usageguard`, `internal/hooks/usagegate.go` en `docs/usage-gate.md`. `test_linkcheck_fails_loud_without_tooling` blijft: zijn PATH wordt `tool_bin(RUN_SH_TOOLS)`, zonder `lighthouse`.

- [ ] **Stap 1: omschakelen.** Pas de tests aan zoals hierboven, wijzig `hooks.json` en verwijder de bestanden.
- [ ] **Stap 2: draaien.**
  - `uv run pytest -q tests/test_site_session.py tests/test_site_loop.py tests/test_site_config.py tests/test_site_reference.py` → groen.
  - `uv run pytest -q tests/pack -k "bash_write or usage or md_link or permission or run_sh or minimal_config or check_instructions or skills_issue_flow"` → groen.
  - Leg daarna `git diff <commit vóór taak 7> -- tests/pack` naast de tabel; de aanpassingen van taak 1 en 6 horen daar niet bij en zijn eerder gecommit met hun reden. Elke gewijzigde regel met `assert` hoort bij een rij van de tabel. Anders is er een verwachting aangepast (AC 1), en dat is rood.
- [ ] **Stap 3: Go en dogfood.**
  - `go test -race ./...` → PASS.
  - `go run ./cmd/lighthouse init --upgrade` gevolgd door `git status`: de geïnstalleerde kopieën verdwijnen en `.claude/settings.json` noemt `run.sh`.
  - `go run ./cmd/lighthouse init --upgrade` nogmaals → `No changes.`
- [ ] **Stap 4: grep.** `git grep -n -e usage_guard.py -e md_links.py -e alarm.sh -e note.py -e md-link-check.sh -e usage-gate.sh -e permission-check.sh -- pack internal .claude .lighthouse docs/site` → alleen treffers in `docs/*.md` (taak 8) of geen.
- [ ] **Stap 5: commit.** `feat(core): usage-gate, permission-check en md-link-check via run.sh; Python weg (#275)`

### Taak 8: tekst en changelog

**Bestanden:** wijzig `CLAUDE.md` (de drie hookregels: `run.sh <naam>` in plaats van `<naam>.sh`, met hetzelfde gedrag), `docs/usage-gate.md` (regels 9, 31, 223: `lighthouse hook usage-gate` en `lighthouse usage alarm` in plaats van `usage-gate.sh` en `alarm.sh`) en `docs/host-assumptions.md` (de rij van regel 32: de datumparsing gebeurt nu in Go; de rij van regel 33 linkt naar het verdwenen `usage_guard.py`). `docs/usage-gate.md` regels 33, 37 en 173 noemen `date -d` en `python3`: pas ze aan. Maak `changelog.d/issue-275-usage-gate-go.md`.

- [ ] **Stap 1: schrijf de tekst.** Het fragment krijgt `type: added` en één alinea in het Engels, zoals de andere fragmenten: `lighthouse usage alarm` is nieuw, de drie hooks draaien via `run.sh` op de binary, en de stop-gates van `go` en `python` lezen het alarm via de binary.
- [ ] **Stap 2: controleren.**
  - `rg -n "usage-gate\.sh|md-link-check\.sh|permission-check\.sh|alarm\.sh|usage_guard\.py" CLAUDE.md docs/*.md pack --glob '*.md'` → geen treffers buiten de historische plannen en specs.
  - `uv run pytest -q tests/test_site_reference.py tests/test_dogfood.py` → groen.
  - `md-link-check` blokkeert niets: de hook draait nu zelf in Go.
- [ ] **Stap 3: commit.** `docs(usage-gate): hooks via run.sh en lighthouse usage alarm (#275)`

## Eindcontrole

- `go test -race ./...` en `uv run pytest -q tests` → groen.
- `go run ./cmd/lighthouse init --upgrade` → `No changes.`
- `ls .claude/hooks/lighthouse/usage-gate* .claude/hooks/lighthouse/permission-check* .claude/hooks/lighthouse/md-link-check.sh .lighthouse/lib/lighthouse_lib/usage_guard.py .lighthouse/lib/lighthouse_lib/md_links.py` → elk pad `No such file or directory`.
- `grep -n python3 .claude/hooks/lighthouse/go-lint-gate.sh` → alleen de regels van `turn_roots.py` (stap 5 van de spec), niet meer die van het alarm.

## Open na review

Ronde 3 haalde de rondegrens met deze punten open. Ze zijn aanvaard als `accepted_open`;
de volgende fase of de PR-review weegt ze opnieuw.

- **r3-f1** (MAJOR, Taak 7, table 'Tests die verdwijnen', row test_bash_write_check.py::test_linkcheck_unchecked_form_is_on_the_first_line): The new row has the test drop the lighthouse binary from the PATH and keeps the assert that the first line starts with `Linkcheck: COULD NOT CHECK — `. run.sh without a binary prints `COULD NOT CHECK: lighthouse not on PATH — …` with no `Linkcheck:` prefix, because only the Go hook writes that prefix. The test's stripped PATH also lacks grep, sed, awk and mktemp, so run.sh takes the non-blocking branch and exits 0.
