# Usage-guard in `pack/core` — implementatieplan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Doel:** `usage-guard.sh` in `pack/core` waarschuwt, onderbreekt en parkeert op het alarm van `lighthouse usage`, met hervatten en de controller-tak als opties per repo.

**Architectuur:** Een dunne bash-hook (`usage-guard.sh`) houdt het snelle pad (één `[ -f ]`), toetst alarm en pauzevlag zonder Python, start de verversing en geeft daarna één Python-proces de payload: `lighthouse_lib/usage_guard.py`. Die lib beslist wie spreekt (sleutels, claim, opties) en laadt alleen bij een aanstaande optie een module uit `hooks/usage-guard/` (`resume.py`, `controller.py`), die levert wat er gezegd en geschreven wordt. Zelfde patroon als `bash-write-gates.sh` boven `bash_writes.py`.

**Tech stack:** bash (3.2-compatibel), Python 3 (syntaxis 3.9, `tomllib` via `config.py`), pytest, Go-test van `internal/packs`.

**Spec:** [2026-10-25-usage-guard-design.md](../specs/2026-10-25-usage-guard-design.md). Bron van het oude gedrag: `.claude/hooks/usage-guard.sh` in CorneeldH/personal_ai_agent, commit `5e1165861bf6f13385e6c5c390e20c95cf2cadc0`.

**Begeleidende skills:** `/lighthouse-testing` (welke productiewijziging laat elke test falen, mutatiecheck vóór "klaar"), `/lighthouse-go-writing` voor de wijziging in `internal/packs/core_test.go`.

## Globale eisen

- Elke commit haalt de test-gate `uv run pytest -q && go test -shuffle=on ./...`. Een nieuw bestand onder `pack/` komt dus in dezelfde commit als zijn regel in `pack/SOURCES.toml` (`tests/test_sources.py`) en zijn rij in de `want`-lijst van `internal/packs/core_test.go`.
- Paden letterlijk uit de spec: alarm `${XDG_CACHE_HOME:-$HOME/.cache}/lighthouse/usage-window.alarm`; sleutels `${XDG_STATE_HOME:-$HOME/.local/state}/lighthouse/usage-guard/`; markers en handoffs `${XDG_DATA_HOME:-$HOME/.local/share}/lighthouse/resume/`; pauzevlag `${LIGHTHOUSE_CONTROLLER_STATE:-${XDG_STATE_HOME:-$HOME/.local/state}/lighthouse/controller}/PAUSE`.
- Env: `LIGHTHOUSE_USAGE_REFRESH=1` (de guard zwijgt), `LIGHTHOUSE_CONTROLLER_RUN` (telt alleen bij een volle match op `[0-9a-f-]+`), `LIGHTHOUSE_CONTROLLER_STATE`.
- Alarmregel: `<soort> <pct> <reset_local> <reset_at>`, precies vier velden; `soort` ∈ {`venster`, `week`}; `pct` alleen cijfers; `reset_at` `^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z$` en in de toekomst; `_` in `reset_local` wordt een spatie.
- De guard eindigt altijd met exit 0 en zet hooguit één JSON-object op stdout. Een fout geeft geen blokkade.
- Code, commentaar, docstrings, redenen en meldingen in het Engels; meldingen op stderr beginnen met `usage-guard: `. Dit plan, `docs/usage-guard.md`, commits en PR in het Nederlands.
- Python in `pack/core/lib/usage_guard.py` en `pack/core/hooks/usage-guard/*.py`: syntaxis binnen Python 3.9 (`from __future__ import annotations`, geen `match`, geen `X | Y` buiten annotaties). `datetime.fromisoformat` kent `Z` pas in 3.11: vervang `Z` door `+00:00` vóór het parsen.
- Shell: draait onder macOS-bash 3.2 (geen `${var,,}`, geen `$EPOCHSECONDS`, geen `case` binnen `$(…)`), en is schoon onder `shellcheck -x -S style` (`tests/pack/test_shell_style.py`).
- Geen woord `paia` in `pack/core/hooks/usage-guard.sh`, `pack/core/hooks/usage-guard/` en `pack/core/lib/usage_guard.py`.
- Elke test die de guard draait, gebruikt `guard_env` uit taak 3: `HOME`, `XDG_CACHE_HOME`, `XDG_STATE_HOME`, `XDG_DATA_HOME` naar tmp, `LIGHTHOUSE_CONTROLLER_RUN`, `LIGHTHOUSE_CONTROLLER_STATE`, `LIGHTHOUSE_USAGE_REFRESH` en `LIGHTHOUSE_TOML` uit de env, geen echte `lighthouse` op `PATH`, `TZ=UTC`.
- Commits: `feat(usage-guard): … (#66)`, `feat(config): …`, `docs(handoff): …`, `chore(dogfood): …`.

## Review Focus

1. **Een `session_id` of `agent_id` die geen veilige naam is** (`../x`, `a/b`, 300 tekens). Die waarden belanden in bestandsnamen; zonder toets schrijft de guard buiten zijn mappen. Verwacht: telt als afwezig (patroon `[A-Za-z0-9_-]{1,128}`). Test in taak 2.
2. **De losgekoppelde verversing houdt stdout of stdin vast.** Claude wacht op EOF van stdout; een `lighthouse usage refresh` die stdout erft houdt elke prompt seconden op, en een die stdin erft eet de payload. Verwacht: de hook keert direct terug, en de payload komt nog aan (de handoff in de reden draagt de `session_id`). Test in taak 3.
3. **Een onleesbare payload bij `stop`** (`not json`, `[]`). Verwacht: geen blokkade, zoals bij `stop_hook_active` (anders lust een kapotte payload). Bij `tool` blokkeert de guard wel, zonder sessiedeel in de sleutel. Test in taak 2.
4. **Een alarm met een vijfde veld of een `\r` aan het eind.** Verwacht: telt niet (precies vier velden, strikt patroon). Test in taak 3.
5. **Een onschrijfbare sleutelmap** (`XDG_STATE_HOME` wijst naar een bestand). Verwacht: `tool` en `subagent` zwijgen met één stderr-regel; ze blokkeren niet bij elke call opnieuw. Test in taak 2.

## Bestanden

| Bestand | Verantwoordelijkheid |
|---|---|
| `pack/core/lib/config.py` | booleaans type, `usage_guard.resume` en `usage_guard.controller` |
| `pack/core/lib/usage_guard.py` | payload, context, paden, sleutels, claim, opties, basisredenen, wie spreekt |
| `pack/core/hooks/usage-guard.sh` | standen, snelle pad, alarm- en vlagtoets in bash, verversen, Python starten |
| `pack/core/hooks/usage-guard/resume.py` | module hervatten: marker, wekker, handoff-stappen |
| `pack/core/hooks/usage-guard/controller.py` | module controller: parkeren bij alarm en bij pauze |
| `pack/core/hooks/hooks.json` | vier registraties |
| `pack/core/requires.toml`, `pack/SOURCES.toml` | eenheden en herkomst |
| `pack/core/skills/handoff/SKILL.md` | de padvorm `/lighthouse-handoff <pad>` |
| `tests/pack/_testkit.py` | `guard_env`, `alarm_line`, `run_guard`, `BSD_DATE_STUB` |
| `tests/pack/test_usage_guard_lib.py` | de lib in-process |
| `tests/pack/test_usage_guard.py` | de geïnstalleerde guard, basis |
| `tests/pack/test_usage_guard_resume.py`, `test_usage_guard_controller.py` | de modules via de geïnstalleerde guard |
| `docs/usage-guard.md`, `CLAUDE.md` | contract en hooklijst |

---

### Taak 1: booleaans type in `config.py`

**Files:**
- Modify: `pack/core/lib/config.py` (`_STR, _INT, …` regel 60, `SCHEMA`, `DEFAULTS`, `_check_type`)
- Test: `tests/pack/test_config.py`

**Interfaces:**
- Produces: `config.lookup(cfg, "usage_guard.resume") -> bool` en `"usage_guard.controller"`, standaard `False`; de CLI drukt `true`/`false` af (`_scalar` kan dat al).

- [ ] **Stap 1: Schrijf de falende tests** in `tests/pack/test_config.py`:
  - in de parametrize van `test_defaults_for_absent_keys`: `("usage_guard.resume", "false")` en `("usage_guard.controller", "false")`;
  - `test_bool_key_set_true`: `VALID` plus `[usage_guard] resume = true` (via `_with(("usage_guard", "resume"), True)`) → `main(["usage_guard.resume"]) == 0`, stdout `"true\n"`;
  - `test_bool_key_rejects_non_bool`, parametrize over `"yes"`, `1`, `0`: `load` geeft `ConfigError` met `usage_guard.resume` en `bool` in de tekst; `main([...]) == 2`.

- [ ] **Stap 2: Draai ze en zie ze falen**

  Run: `uv run pytest -q tests/pack/test_config.py -k "usage_guard or bool_key"`
  Expected: FAIL (`not set` / rc 1 bij de defaults, geen `ConfigError` bij `"yes"`)

- [ ] **Stap 3: Implementeer.** `_BOOL = "bool"` naast de andere vier; `_check_type` krijgt een eigen tak `if kind == _BOOL: return isinstance(value, bool)` vóór de laatste regel (die valt nu terug op `list[int]`). `SCHEMA` en `DEFAULTS` krijgen beide sleutels, default `False`. Zet de installatiekopie niet met de hand bij: dat doet taak 7.

- [ ] **Stap 4: Draai de tests en zie ze slagen** (dezelfde run) — Expected: PASS

- [ ] **Stap 5: Commit** — `feat(config): booleaans type en [usage_guard] (#66)`

### Taak 2: de lib `usage_guard.py`

**Files:**
- Create: `pack/core/lib/usage_guard.py`, `tests/pack/test_usage_guard_lib.py`
- Modify: `pack/SOURCES.toml` (regel met `source_path = ".claude/hooks/usage-guard.sh"`, `source_sha = "5e1165861bf6f13385e6c5c390e20c95cf2cadc0"`: de redenen en de markerlogica komen uit de paia-guard), `pack/core/requires.toml` (`lighthouse_lib.usage_guard` onder `python3` en `tomllib`), `internal/packs/core_test.go` (rij `.lighthouse/lib/lighthouse_lib/usage_guard.py` na `turn_roots.py`)

**Interfaces:**
- Consumes: `lighthouse_lib.config` (`load`, `config_path`, `lookup`, `ConfigError`) uit taak 1.
- Produces (alles in `lighthouse_lib.usage_guard`):
  ```python
  SAFE_ID = re.compile(r"[A-Za-z0-9_-]{1,128}")   # fullmatch, else the id counts as absent
  RUN_ID = re.compile(r"[0-9a-f-]+")              # fullmatch on LIGHTHOUSE_CONTROLLER_RUN
  KIND_LABEL = {"venster": "session window", "week": "weekly window"}

  @dataclass(frozen=True)
  class Alarm:
      kind: str; pct: int; reset_local: str; reset_at: str   # reset_local with spaces

  @dataclass(frozen=True)
  class Context:
      stand: str               # as the hook was called: prompt | tool | subagent | stop
      alarm: Optional[Alarm]   # None without a valid alarm
      session_id: str          # "" when absent or not SAFE_ID
      agent_id: str            # idem
      cwd: str                 # payload cwd, else os.getcwd()
      run_id: str              # "" outside a controller run
      pause_until: str         # "" without a valid pause flag
      pause_flag: str          # path of the flag, "" without
      stop_hook_active: bool   # True as well for an unreadable or non-object payload
      payload_ok: bool
      now: datetime            # aware, UTC

  @dataclass(frozen=True)
  class Options:
      resume: bool; controller: bool

  @dataclass(frozen=True)
  class Decision:
      output: Optional[dict]     # the one JSON object, or None
      key_after: Optional[Path]  # stop: the key main writes after printing

  def build_context(stand: str, alarm: Optional[Alarm], pause_until: str, pause_flag: str,
                    payload_text: str, now: datetime) -> Context
  def state_dir() -> Path    # ${XDG_STATE_HOME:-$HOME/.local/state}/lighthouse/usage-guard
  def resume_dir() -> Path   # ${XDG_DATA_HOME:-$HOME/.local/share}/lighthouse/resume
  def handoff_path(ctx: Context) -> Path   # <resume>/<session_id>.handoff.md, else no-session-<epoch>.handoff.md
  def effective_stand(ctx: Context) -> str # "subagent" for a tool call with agent_id
  def alarm_key(ctx: Context, stand: str) -> Path
  def pause_key(ctx: Context) -> Path      # <state>/pause-<pause_until>.<run_id>.done
  def marker(ctx: Context, *, session_id: str, window: str, reset_at: str, source: str) -> dict
      # {session_id, cwd, window, reset_at, created_at (UTC, %Y-%m-%dT%H:%M:%SZ), source}
  def write_marker(fields: dict) -> Path   # <resume>/<session_id>.json, tmp in that dir + os.replace
  def block(reason: str) -> dict           # {"decision": "block", "reason": reason}
  def prompt_note(text: str) -> dict       # {"hookSpecificOutput": {"hookEventName": "UserPromptSubmit", "additionalContext": text}}
  def base_speak(ctx: Context, stand: str) -> dict      # prompt, tool, stop of the base
  def subagent_speak(ctx: Context) -> dict
  def read_options() -> Options            # ConfigError → Options(False, False) + "usage-guard: config: <error>" on stderr
  def load_module(hooks_dir: Path, name: str) -> ModuleType  # <hooks_dir>/usage-guard/<name>.py via importlib
  def decide(ctx: Context, hooks_dir: Path, options: Callable[[], Options]) -> Decision
  def main(argv: Optional[list] = None) -> int   # always 0
  ```
  CLI (taak 3 roept dit aan), payload op stdin:
  `python3 usage_guard.py <stand> --hooks <dir> [--alarm KIND PCT LOCAL AT] [--pause UNTIL FLAG]`.
  Een module heeft `speak(ctx: Context, stand: str) -> dict`; `controller` ook `pause(ctx: Context) -> dict`.

- [ ] **Stap 1: Schrijf de falende tests** in `tests/pack/test_usage_guard_lib.py` (import via `from lighthouse_lib import usage_guard as ug`; `monkeypatch.setenv` voor `XDG_STATE_HOME`, `XDG_DATA_HOME` naar `tmp_path`; `NOW = datetime(2026, 9, 6, 11, 58, tzinfo=timezone.utc)`; `ALARM = ug.Alarm("venster", 92, "14:30", "2026-09-06T12:30:00Z")`; een helper `ctx(stand, payload: dict | str, **over)` rond `build_context`):
  - `test_alarm_key_forms`: `venster-2026-09-06T12:30:00Z.tool.s1.done`; zonder `session_id` `venster-2026-09-06T12:30:00Z.tool.done`; met `run_id` `R` `….tool.s1.R.done`; stand `subagent` met `agent_id` `a1` en `run_id` `R` → `….subagent.s1.a1.done`. Allemaal onder `state_dir()`.
  - `test_pause_key`: `pause_until` `2026-10-03T21:00:00+02:00`, `run_id` `R` → `state_dir() / "pause-2026-10-03T21:00:00+02:00.R.done"`.
  - `test_unsafe_ids_count_as_absent` (Review Focus 1): `session_id` `../x`, `a/b`, `"s" * 129` → `ctx.session_id == ""`; zo ook `agent_id`.
  - `test_handoff_path`: met `s1` → `resume_dir() / "s1.handoff.md"`; zonder → `resume_dir() / f"no-session-{int(NOW.timestamp())}.handoff.md"`.
  - `test_unreadable_payload` (Review Focus 3): `not json` en `[]` → `stop_hook_active is True`, `payload_ok is False`, `session_id == ""`; `decide` in `stop` → `output is None`; in `tool` → een block.
  - `test_prompt_warns_without_key`: `decide` in `prompt` → `output["hookSpecificOutput"]["additionalContext"]` bevat `"92%"`, `"14:30"` en `f"/lighthouse-handoff {handoff_path(c)}"`; `state_dir()` heeft geen `.done`.
  - `test_tool_blocks_once_per_window`: eerste `decide` in `tool` → `output["decision"] == "block"`, reden bevat `"commit"` en `f"/lighthouse-handoff {handoff_path(c)}"`; tweede → `output is None`; een andere `session_id` → weer een block.
  - `test_tool_with_agent_id_is_subagent`: `tool` met `agent_id` `a1` → reden van `subagent_speak` (bevat `"main thread"`, niet `"/lighthouse-handoff"`, niet `"CronCreate"`); `alarm_key(c, "subagent")` bestaat, `alarm_key(c, "tool")` niet.
  - `test_stop`: met `stop_hook_active: true` → `None`; zonder → block met `"/lighthouse-handoff"`, `key_after == alarm_key(c, "stop")`, en dat bestand bestaat nog niet.
  - `test_stop_prunes_old_keys`: een `.done` met `os.utime` 15 dagen terug en een van 13 dagen → na `decide` in `stop` alleen de oude weg.
  - `test_failing_module_releases_key`: `hooks_dir = tmp_path / "hooks"` met `usage-guard/resume.py` = `def speak(ctx, stand): raise RuntimeError("x")`; `options = lambda: ug.Options(True, False)` → `output is None`, `alarm_key(c, "tool")` bestaat niet.
  - `test_module_output_is_the_only_output`: dezelfde map met `speak` dat `{"decision": "block", "reason": "R"}` teruggeeft → `output == {"decision": "block", "reason": "R"}`.
  - `test_subagent_never_loads_a_module`: de falende module van hierboven, `Options(True, True)`, `run_id` gezet, stand `subagent` → de subagent-reden.
  - `test_unwritable_state_dir_is_silent` (Review Focus 5): `XDG_STATE_HOME` naar een bestaand bestand → `tool` geeft `None` en `capsys` stderr begint met `usage-guard: `; ook bij een tweede call.
  - `test_no_alarm_no_pause`: `alarm=None`, geen vlag → `Decision(None, None)` in elke stand.
  - `test_options_config_error`: `LIGHTHOUSE_TOML` naar een bestand met `[usage_guard]\nresume = "yes"\n` plus `[project]`/`[forge]` uit `MINIMAL_TOML` → `read_options() == Options(False, False)`, stderr bevat `usage-guard: config:`.

- [ ] **Stap 2: Draai ze en zie ze falen**

  Run: `uv run pytest -q tests/pack/test_usage_guard_lib.py`
  Expected: FAIL, `cannot import name 'usage_guard'`

- [ ] **Stap 3: Implementeer `pack/core/lib/usage_guard.py`.** Bovenaan dezelfde `sys.path`-regel als `config.py`, zodat het als bestand draait. `decide` in deze volgorde:
  1. Pauze: `stand == "tool"`, `run_id`, `pause_until`, geen `agent_id`, `options().controller`, en `pause_key` ontbreekt of heeft een kleinere mtime dan `pause_flag` → `load_module(…, "controller").pause(ctx)`, daarna `pause_key` openen met `"w"` (zet ook de mtime) en klaar. Een uitzondering in de module: stderr-regel, door naar 2. De lib schrijft de pauzesleutel, niet de module: alle sleutels horen bij de beslisser *(keuze van het plan; de spec laat de module schrijven)*.
  2. Geen `alarm` → `Decision(None, None)`.
  3. `stand = effective_stand(ctx)`; `stop` met `stop_hook_active` → `None`.
  4. `tool`/`subagent`: claim `alarm_key` met `os.open(…, O_CREAT | O_EXCL | O_WRONLY)` na `mkdir(parents=True)`. `FileExistsError` → `None`; elke andere `OSError` → stderr-regel en `None`.
  5. Spreker: `subagent` → `subagent_speak`; `run_id` en `options().controller` → module `controller`; `options().resume` → module `resume`; anders `base_speak`. `options` hooguit één keer aanroepen (cache).
  6. Uitzondering in de spreker → claim weer weg (`unlink`, `missing_ok`), stderr-regel, `None`.
  7. `stop` → oude sleutels (> 14 dagen) best-effort weg, `key_after = alarm_key(ctx, "stop")`.

  `main`: argparse zoals in *Interfaces*, `build_context` met `datetime.now(timezone.utc)`, `decide`, print `json.dumps(output)`, daarna `key_after` aanmaken (best-effort). Alles in één `try`; elke uitzondering → één stderr-regel, rc 0. Redenen: de vaste ankers uit de tests; verder vrije Engelse tekst binnen de stappen van de spec (tool: commit het werk-in-uitvoering, handoff, meld waar het werk staat, geen nieuwe stap en geen subagent; stop: commit niets meer, handoff met wat er ongecommit staat, hervattingsmoment, stop).

- [ ] **Stap 4: Draai de tests en zie ze slagen** (dezelfde run) — Expected: PASS. Daarna `uv run pytest -q tests/test_sources.py && go test ./internal/packs/` — Expected: PASS

- [ ] **Stap 5: Commit** — `feat(usage-guard): lib die beslist wie spreekt (#66)`

### Taak 3: de hook `usage-guard.sh` en zijn registratie

**Files:**
- Create: `pack/core/hooks/usage-guard.sh`, `tests/pack/test_usage_guard.py`
- Modify: `pack/core/hooks/hooks.json`, `pack/core/requires.toml` (`usage-guard` onder `python3` en `tomllib`), `pack/SOURCES.toml` (zelfde `source_path`/`source_sha` als taak 2), `internal/packs/core_test.go` (rij `.claude/hooks/lighthouse/usage-guard.sh` vóór `worktree-guard.sh`; `kinds[KindHook] != 9` wordt `13`), `tests/pack/_testkit.py`, `tests/pack/test_hooks_minimal_config.py`

**Interfaces:**
- Consumes: de CLI van `usage_guard.py` (taak 2).
- Produces, in `tests/pack/_testkit.py`:
  ```python
  def guard_env(tmp_path: Path, **extra: str) -> dict[str, str]   # zie Globale eisen
  def alarm_line(kind: str = "venster", pct: int = 92, local: str = "14:30",
                 delta: timedelta = timedelta(hours=1)) -> str     # reset_at = now + delta, UTC, Z
  def write_alarm(env: dict, line: str) -> Path                    # <XDG_CACHE_HOME>/lighthouse/usage-window.alarm
  def run_guard(root: Path, stand: str, payload: dict | str, env: dict) -> subprocess.CompletedProcess
      # runs <root>/.claude/hooks/lighthouse/usage-guard.sh <stand> with LIGHTHOUSE_REPO_ROOT=root
  BSD_DATE_STUB: str   # a `date` that rejects -d and translates -j [-u] -f FMT VALUE to the real GNU date
  ```
- Registraties in `hooks.json` (timeout 10 voor alle vier): `UserPromptSubmit` → `usage-guard.sh prompt`; een tweede `PostToolUse`-blok met `"matcher": ""` → `usage-guard.sh tool`; `SubagentStop` → `usage-guard.sh subagent`; `Stop` (`"matcher": ""`) → `usage-guard.sh stop`.

- [ ] **Stap 1: Schrijf de falende tests** in `tests/pack/test_usage_guard.py` (per test `install_core(tmp_path / "root")`, `env = guard_env(tmp_path)`, payload `{"session_id": "s1", "cwd": str(tmp_path), "hook_event_name": …}`):
  - `test_alarm_cases`, parametrize stand ∈ {`prompt`, `tool`, `subagent`, `stop`} × geval: geen bestand; geldig; verlopen (`delta=-timedelta(minutes=1)`); `venster 92 14:30` (drie velden); `venster 92 ? ?`; vijf velden (Review Focus 4); `reset_at` met `\r` erachter (Review Focus 4); soort `dag`; pct `9x`; een symlink naar een geldig alarm. Alleen geldig spreekt: `prompt` → `json.loads(out)["hookSpecificOutput"]["additionalContext"]`, de andere drie → `"decision": "block"`. Alle andere: stdout `""`. Altijd rc 0.
  - `test_fast_path_runs_nothing`, stand ∈ {`tool`, `subagent`}, en ook met `LIGHTHOUSE_CONTROLLER_RUN` = een uuid zonder vlag: `PATH` = alleen een stubmap met `python3`, `jq`, `date`, `git`, `cat`, `dirname` die elk hun naam naar een spoorbestand schrijven; de hook draait als `[shutil.which("bash"), hook, stand]`. Verwacht: spoor leeg, stderr `""`, rc 0.
  - `test_bsd_date`: `BSD_DATE_STUB` als `date` vooraan op `PATH` → geldig alarm blokkeert in `tool`, verlopen niet.
  - `test_refresh_is_detached` (Review Focus 2): stub `lighthouse` = `sleep 2; printf '%s|%s' "$*" "${LIGHTHOUSE_USAGE_REFRESH:-}" > <spoor>`, plus een geldig alarm; `prompt` keert terug binnen 1,5 s, `additionalContext` bevat `s1.handoff.md`; binnen 5 s staat in het spoor `usage refresh|1`.
  - `test_refresh_without_lighthouse`: geen `lighthouse` op `PATH`, `prompt` → rc 0, stderr `""`.
  - `test_usage_refresh_env_silences`, elke stand, geldig alarm, `LIGHTHOUSE_USAGE_REFRESH=1`, stub `lighthouse` die zonder `sleep` meteen een spoor schrijft → stdout `""` en na 1 s geen spoor.
  - `test_once_per_window`: tweede `tool` → `""`; `subagent` blokkeert daarna nog wel (eigen sleutel); `stop` met `stop_hook_active: true` → `""`; tweede `stop` zonder → `""`; `session_id` `s2` in `tool` → block.
  - `test_no_table_no_marker`: na een `tool`-block bestaat `<XDG_DATA_HOME>/lighthouse/resume` niet.
  - `test_missing_python`: `PATH` = `tool_bin(tmp_path / "bin", ("date", "git", "cat"))` (zonder `python3`), de hook via `[shutil.which("bash"), hook, "tool"]`, geldig alarm → rc 0, stdout `""`, stderr begint met `usage-guard: `.
  - `test_unknown_stand`: `usage-guard.sh nonsense` → rc 0, geen uitvoer.
  - `test_no_paia_in_guard_files`: `git grep -n -i paia -- pack/core/hooks/usage-guard.sh pack/core/hooks/usage-guard pack/core/lib/usage_guard.py` geeft rc 1.

  In `tests/pack/test_hooks_minimal_config.py`: `_payload` krijgt een tak voor `usage-guard.sh` (`{"hook_event_name": event, "session_id": "s1", "cwd": str(worktree), "tool_name": "Bash", "tool_input": {"command": "ls"}}`); `test_every_registration_has_a_payload` krijgt `"usage-guard.sh"`; de env van `test_hooks_with_minimal_config` wordt `guard_env(tmp_path, LIGHTHOUSE_REPO_ROOT=…, …)` in plaats van `dict(os.environ, …)`. Breekt een andere hook onder de tmp-`HOME`, gebruik `guard_env` dan alleen voor de `usage-guard.sh`-registraties en noteer dat in de commit.

- [ ] **Stap 2: Draai ze en zie ze falen**

  Run: `uv run pytest -q tests/pack/test_usage_guard.py tests/pack/test_hooks_minimal_config.py`
  Expected: FAIL (hook bestaat niet; `no payload for usage-guard.sh` pas na de registratie)

- [ ] **Stap 3: Implementeer `pack/core/hooks/usage-guard.sh`.** Kopcommentaar in de stijl van `bash-write-gates.sh` (wat, standen, waarom het snelle pad). `set -u`. De volgorde is het ontwerp; houd hem precies zo:
  1. `STAND="${1:-}"`; `[ "${LIGHTHOUSE_USAGE_REFRESH:-}" = "1" ] && exit 0`; stand niet in `prompt|tool|subagent|stop` → `exit 0`.
  2. `ALARM="${XDG_CACHE_HOME:-${HOME:-}/.cache}/lighthouse/usage-window.alarm"`. `RUN_ID` uit `LIGHTHOUSE_CONTROLLER_RUN`, leeg bij `""` of `*[!0-9a-f-]*`.
  3. `prompt`: als `command -v lighthouse` slaagt, `(LIGHTHOUSE_USAGE_REFRESH=1 lighthouse usage refresh </dev/null >/dev/null 2>&1 &)`.
  4. Pauze, alleen bij `RUN_ID` en `tool`: stap 1–3 van de spec (gewoon bestand en geen symlink; eerste regel via `IFS= read -r` met `|| true`, alleen `0-9T:+Z-`, `*T*[+-]*` of `*T*Z`; epoch via GNU `date -d`, anders BSD `date -j -f '%Y-%m-%dT%H:%M:%S%z'` met de dubbele punt uit de offset en `Z` → `+0000`; groter dan `date +%s`). Geldig → `PAUSE_UNTIL` en `PAUSE_FLAG`.
  5. Zonder `PAUSE_UNTIL`: `[ -f "$ALARM" ] || exit 0`. Dit is het snelle pad; daarvóór staat alleen werk zonder fork.
  6. Alarm: geen symlink; `IFS= read -r LINE`; `set -f`, splitsen, `set +f`; precies vier velden, soort, pct en het `reset_at`-patroon (`[[ =~ ]]`); epoch via `date -u -d`, anders `date -j -u -f '%Y-%m-%dT%H:%M:%SZ'`; in de toekomst → `ALARM_ARGS=(--alarm KIND PCT LOCAL AT)` met `_` → spatie in `LOCAL`.
  7. Geen `PAUSE_UNTIL` en geen geldig alarm → `exit 0`, stdin ongelezen.
  8. `LIB_ROOT` zoals `bash-write-gates.sh` (`LIGHTHOUSE_REPO_ROOT`, anders `git -C <hookmap> rev-parse --show-toplevel`), `export LIGHTHOUSE_REPO_ROOT`. Ontbreekt `python3` of de lib: één regel `usage-guard: COULD NOT CHECK — <wat>; the alarm is valid` op stderr, `exit 0`.
  9. `python3 "$LIB" "$STAND" --hooks "${BASH_SOURCE[0]%/*}" …` met stdin van de hook; daarna altijd `exit 0`.

  Daarna `hooks.json`, `requires.toml`, `SOURCES.toml`, `core_test.go` en de `_testkit`-helpers volgens *Files* en *Interfaces*.

- [ ] **Stap 4: Draai de tests en zie ze slagen**

  Run: `uv run pytest -q tests/pack/test_usage_guard.py tests/pack/test_hooks_minimal_config.py tests/pack/test_shell_style.py tests/pack/test_code_language.py tests/test_sources.py && go test ./internal/packs/`
  Expected: PASS

- [ ] **Stap 5: Commit** — `feat(usage-guard): hook op vier events, snelle pad zonder fork (#66)`

### Taak 4: module hervatten

**Files:**
- Create: `pack/core/hooks/usage-guard/resume.py`, `tests/pack/test_usage_guard_resume.py`
- Modify: `pack/SOURCES.toml` (`new = true`), `internal/packs/core_test.go` (rij `.claude/hooks/lighthouse/usage-guard/resume.py` na `usage-guard.sh`)

**Interfaces:**
- Consumes: `Context`, `marker`, `write_marker`, `handoff_path`, `block`, `prompt_note`, `KIND_LABEL` (taak 2); `guard_env`, `alarm_line`, `write_alarm`, `run_guard` (taak 3).
- Produces:
  ```python
  def wake_cron(reset_at: str, tz: Optional[tzinfo] = None) -> str  # reset_at + 2 min in tz (None: local), "M H D Mon *", no leading zeros
  def speak(ctx: Context, stand: str) -> dict                        # stand: prompt | tool | stop
  ```

- [ ] **Stap 1: Schrijf de falende tests** in `tests/pack/test_usage_guard_resume.py`. De module in-process via `importlib.util.spec_from_file_location` op `CORE / "hooks" / "usage-guard" / "resume.py"`; de rest via `run_guard` met `install_core(root, toml=MINIMAL_TOML + "\n[usage_guard]\nresume = true\n")`:
  - `test_wake_cron`: `("2026-09-06T12:30:00Z", ZoneInfo("Europe/Amsterdam"))` → `"32 14 6 9 *"`; `("2026-12-31T23:59:00Z", timezone.utc)` → `"1 0 1 1 *"`.
  - `test_tool_venster_writes_marker`: geldig `venster`-alarm, `tool` → `<XDG_DATA_HOME>/lighthouse/resume/s1.json` heeft precies de sleutels `session_id, cwd, window, reset_at, handoff_path, created_at, source`, met `"window": "venster"`, `"source": "tool"`, `handoff_path` = `…/resume/s1.handoff.md`, `created_at` volgens `%Y-%m-%dT%H:%M:%SZ`. De reden bevat `f'cron "{wake_cron(at, timezone.utc)}"'`, `"recurring false"`, `"CronList"`, `"/lighthouse-handoff "` plus het handoff-pad. Geen `.tmp` in de resume-map.
  - `test_week_has_no_wake`: `week`-alarm → marker met `"window": "week"`, reden zonder `"CronCreate"`.
  - `test_stop_notes_uncommitted`: `stop` → reden bevat `"uncommitted"` en `"CronList"`; marker `"source": "stop"`.
  - `test_prompt_names_steps_without_marker`: `prompt` → `additionalContext` bevat `"CronCreate"` en `"/lighthouse-handoff"`; geen marker.
  - `test_no_session_no_marker`: payload zonder `session_id` → geen `.json` in de resume-map; reden bevat `no-session-`.
  - `test_subagent_ignores_resume`: `subagent` → subagent-reden, geen marker, geen `"CronCreate"`.
  - `test_config_error_falls_back_to_base`: `resume = "yes"` → basisreden (bevat `"/lighthouse-handoff"`, geen `"CronCreate"`), geen marker, stderr bevat `usage-guard: config:`.

- [ ] **Stap 2: Draai ze en zie ze falen**

  Run: `uv run pytest -q tests/pack/test_usage_guard_resume.py`
  Expected: FAIL (`resume.py` bestaat niet)

- [ ] **Stap 3: Implementeer `resume.py`.** `from lighthouse_lib.usage_guard import …` (de lib staat al op `sys.path` als de module geladen wordt). `wake_cron` via `datetime.fromisoformat(reset_at.replace("Z", "+00:00")) + timedelta(minutes=2)`, dan `.astimezone(tz)`. In `tool` en `stop` met `session_id`: `write_marker(dict(marker(…, source=stand), handoff_path=str(handoff_path(ctx))))`. De wekkerstap alleen bij `venster`, met de zin: zette je in dit venster al een wekker, check dan met `CronList` en neem die job-ID over. De handoffstap noemt de job-ID van de wekker.

- [ ] **Stap 4: Draai de tests en zie ze slagen** (dezelfde run, plus `go test ./internal/packs/` en `uv run pytest -q tests/test_sources.py`) — Expected: PASS

- [ ] **Stap 5: Commit** — `feat(usage-guard): module hervatten met marker en wekker (#66)`

### Taak 5: module controller

**Files:**
- Create: `pack/core/hooks/usage-guard/controller.py`, `tests/pack/test_usage_guard_controller.py`
- Modify: `pack/SOURCES.toml` (`new = true`), `internal/packs/core_test.go` (rij `.claude/hooks/lighthouse/usage-guard/controller.py` vóór `usage-guard/resume.py`)

**Interfaces:**
- Consumes: als taak 4; `BSD_DATE_STUB` (taak 3).
- Produces:
  ```python
  def speak(ctx: Context, stand: str) -> dict  # prompt: warning; tool/stop: marker with "controller": true + block
  def pause(ctx: Context) -> dict              # marker window "pause", reset_at = pause_until, controller, pause + block
  ```

- [ ] **Stap 1: Schrijf de falende tests** in `tests/pack/test_usage_guard_controller.py`. `RUN = "0b9e2c4a-1f00-4c8e-9d7e-5a1b2c3d4e5f"`; toml `[usage_guard]\ncontroller = true\n` (en waar genoemd ook `resume = true`); `LIGHTHOUSE_CONTROLLER_RUN=RUN`; `LIGHTHOUSE_CONTROLLER_STATE=<tmp>/ctl`; vlag `<tmp>/ctl/PAUSE`:
  - `test_alarm_parks_with_controller_reason`: `tool`, ook met `resume = true` erbij → reden bevat `"controller"`, niet `"/lighthouse-handoff"`, niet `"CronCreate"`; marker `s1.json` = de zes basisvelden plus `"controller": true`, zonder `handoff_path` en zonder `pause`; sleutel `….tool.s1.<RUN>.done`. Zo ook `stop` (marker `"source": "stop"`).
  - `test_without_session_id_run_id_is_session`: payload zonder `session_id` → marker `<RUN>.json` met `"session_id": RUN`.
  - `test_three_runs_each_once`: drie run-ids, elk één block, de tweede call per run `""`.
  - `test_option_off_is_plain_session`: `controller = false` → basisreden; ongeldige run-id `XYZ` met `controller = true` → basisreden.
  - `test_prompt_warning`: `prompt` → `additionalContext` bevat `"92%"`, niet `"CronCreate"`, niet `"/lighthouse-handoff"`.
  - `test_subagent_ignores_controller`: `subagent` → subagent-reden, geen marker.
  - `test_pause_parks_once`: vlag `<now + 1 h, met offset +02:00>`, geen alarm, `tool` → block met het eindmoment; marker met `"window": "pause"`, `"pause": true`, `"controller": true`, `"reset_at"` = de vlagtekst; tweede `tool` → `""`; `os.utime` van de vlag 10 s later → weer een block.
  - `test_pause_bsd_date`: `BSD_DATE_STUB` op `PATH` → de geldige vlag parkeert.
  - `test_pause_invalid_flag_falls_through`, parametrize: verlopen vlag, `morgen`, naive `2026-10-03T21:00:00`, een symlink naar een geldige vlag. Zonder alarm: stdout `""` en (met de spoorstubs uit taak 3, echte `date` erachter) geen `python3` in het spoor. Met geldig alarm: de controller-alarmreden.
  - `test_pause_with_agent_id_does_not_park`: geldige vlag, payload met `agent_id`: zonder alarm `""`; met alarm de subagent-reden.
  - `test_pause_option_off_falls_through`: `controller = false`, geldige vlag, geldig alarm → basisreden; geen pauzesleutel.
  - `test_pause_only_in_tool`: geldige vlag, geen alarm, `prompt` en `stop` → `""`.

- [ ] **Stap 2: Draai ze en zie ze falen**

  Run: `uv run pytest -q tests/pack/test_usage_guard_controller.py`
  Expected: FAIL (`controller.py` bestaat niet)

- [ ] **Stap 3: Implementeer `controller.py`.** `session_id = ctx.session_id or ctx.run_id` voor marker en reden. Redenen: commit het werk-in-uitvoering, stop, geen nieuwe stap, geen subagent, geen wekker, geen handoff; "the controller resumes this session after <reset_local of eindmoment>". Het eindmoment in de pauzereden als `HH:MM` uit `fromisoformat`, anders de ruwe tekst.

- [ ] **Stap 4: Draai de tests en zie ze slagen** (dezelfde run, plus `go test ./internal/packs/` en `uv run pytest -q tests/test_sources.py`) — Expected: PASS

- [ ] **Stap 5: Commit** — `feat(usage-guard): module controller, parkeren bij alarm en pauze (#66)`

### Taak 6: padvorm van `lighthouse-handoff`

**Files:**
- Modify: `pack/core/skills/handoff/SKILL.md`, `tests/pack/test_skills_issue_flow.py`

**Interfaces:**
- Consumes: de aanroep `/lighthouse-handoff <pad>` die taak 2 en 4 noemen.

- [ ] **Stap 1: Schrijf de falende tests** in `tests/pack/test_skills_issue_flow.py`, sectie `# --- handoff`:
  - `test_handoff_pause_form`: voor beide varianten (`skill("handoff")`) staat elke frase, na `" ".join(body.split())`: `"## Pause form: /lighthouse-handoff <path>"`, `"an absolute path that ends in `.handoff.md`"`, `"mkdir -p"`, `"mv"`, `"outside the sandbox"`, `"also in the chat"`, `"what is uncommitted"`, `"the resume moment"`, `"job ID"`, `"Without a path the skill works as described below."`.
  - `test_handoff_pause_form_skips_phase_side_effects`: de sectie (van `## Pause form` tot `## Workflow`) noemt `post_artefact.py`, `no push`, `Controller mode`, `no question to the user`, `/clear` en `LIGHTHOUSE_CONTROLLER_RUN`; en `body.index("## Pause form") < body.index("## Workflow")`.
  - `test_guard_names_the_pause_form`: `"/lighthouse-handoff "` staat in `pack/core/lib/usage_guard.py` en in `pack/core/hooks/usage-guard/resume.py`, en `".handoff.md"` in `usage_guard.py`.

- [ ] **Stap 2: Draai ze en zie ze falen**

  Run: `uv run pytest -q tests/pack/test_skills_issue_flow.py -k "pause_form or guard_names"`
  Expected: FAIL op de eerste ontbrekende frase

- [ ] **Stap 3: Schrijf de sectie** `## Pause form: /lighthouse-handoff <path>` tussen *When to Use* en *Workflow*, in het Engels, met de inhoud van spec *Handoff naar een bestand*: absoluut pad op `.handoff.md`; hetzelfde blok naar dat pad via Bash (`mkdir -p`, tmp-bestand in dezelfde map, `mv`), en ook in de chat; het pad ligt buiten de repo en kan buiten de sandbox vallen; het blok krijgt erbij wat er ongecommit staat, het hervattingsmoment en de job-ID van een wekker; het slaat over: `post_artefact.py` (dus no push), *Controller mode* (geen `🤖`-comment, geen boardstatus), no question to the user en de `/clear`-tip, ook als `LIGHTHOUSE_CONTROLLER_RUN` gezet is. Pas de description in de frontmatter aan met de padvorm. Houd de zin "Do not use in the middle of a task" kloppend: de padvorm is de uitzondering.

- [ ] **Stap 4: Draai de tests en zie ze slagen**

  Run: `uv run pytest -q tests/pack/test_skills_issue_flow.py tests/pack/test_skill_contracts.py`
  Expected: PASS

- [ ] **Stap 5: Commit** — `docs(handoff): padvorm voor een pauze (#66)`

### Taak 7: documentatie, dogfood en de overstap

**Files:**
- Create: `docs/usage-guard.md`
- Modify: `CLAUDE.md` (lijst *Hooks*), de geïnstalleerde kopie (`.claude/hooks/lighthouse/`, `.claude/settings.json`, `.lighthouse/lib/lighthouse_lib/`, `.claude/skills/lighthouse-handoff/`, `.agents/skills/lighthouse-handoff/`)

- [ ] **Stap 1: Schrijf `docs/usage-guard.md`** (Nederlands, in de stijl van `docs/usage-venster.md`): wat de guard doet per stand (de tabel uit de spec), de opties `[usage_guard]`, de contracttabel, de markervorm met de veldentabel per soort, de pauzevlag met de toetsvolgorde, de env-variabelen, de sleutels, en een relatieve link naar `usage-venster.md` (zelfde map) voor het alarm. Relatieve links naar bestaande doelen.

- [ ] **Stap 2: Regel in `CLAUDE.md`**, in de lijst *Hooks*, na `worktree-linkcheck.sh`:
  `- \`usage-guard.sh\` — UserPromptSubmit, PostToolUse, SubagentStop and Stop. Reads only the alarm of \`lighthouse usage\`: warns at a prompt, blocks once per window in \`tool\`, \`subagent\` and \`stop\`, never fails red. See \`docs/usage-guard.md\`.`

- [ ] **Stap 3: Dogfood**

  Run: `go run ./cmd/lighthouse init --upgrade </dev/null`, daarna `git status --short`
  Expected: de installatiekopie verandert; `.claude/settings.json` krijgt vier registraties. Daarna nog eens `go run ./cmd/lighthouse init --upgrade </dev/null` → `No changes.`

- [ ] **Stap 4: Volledige verificatie**

  Run: `uv run pytest -q && go test -shuffle=on ./...` en `git grep -n -i paia -- pack/core/hooks/usage-guard.sh pack/core/hooks/usage-guard pack/core/lib/usage_guard.py`
  Expected: PASS; grep zonder treffers (rc 1)

- [ ] **Stap 5: Commit** — twee commits: `docs(usage-guard): contract en hooklijst (#66)` (`docs/usage-guard.md`, `CLAUDE.md`) en `chore(dogfood): installeer de usage-guard (#66)` (de installatiekopie)

- [ ] **Stap 6: Beschrijving van CorneeldH/personal_ai_agent#1634** (een AC van #66). Lees de huidige body (`gh issue view 1634 --repo CorneeldH/personal_ai_agent --json body`, met `dangerouslyDisableSandbox: true`), voeg een sectie `### Keuze voor de controller-tak (lighthouse#66)` toe met de zin uit spec *Gevolgen voor paia* (de tak woont in lighthouse, als optie `controller` van de core-guard; paia houdt geen eigen usage-hook) en de lijst aanpassingen daaronder, en schrijf terug met `gh issue edit 1634 --repo CorneeldH/personal_ai_agent --body-file -`. De rest van de body blijft staan.

**Let op bij de PR:** de AC "Met `resume = true` schrijven `tool`, `subagent` en `stop` één resume-marker" botst met spec-keuze r2-f1 (`subagent` schrijft geen marker). Pas de AC niet zelf aan; `/lighthouse-verify-pr` meldt de afwijking bij die AC, met de verwijzing naar r2-f1.
