# Blokkerende Stop-gates zwijgen bij een geldig usage-alarm — implementatieplan

> **Voor agents:** VERPLICHTE SUB-SKILL: gebruik superpowers:subagent-driven-development (aanbevolen) of superpowers:executing-plans om dit plan taak voor taak uit te voeren. Stappen gebruiken checkboxen (`- [ ]`).

**Doel:** `go-check.sh` en `ty-check.sh` blokkeren niet meer zolang er een geldig usage-alarm ligt, met de alarmregels op één plek in `pack/core`.

**Architectuur:** De regels uit stap 6 van `pack/core/hooks/usage-guard.sh` verhuizen naar een bash-bibliotheek om te sourcen, `pack/core/hooks/usage-guard/alarm.sh`. Na `init` staat die in `.claude/hooks/lighthouse/usage-guard/alarm.sh`, naast elke gate uit elk pakket. De guard sourcet hem; elke gate sourcet hem via het eigen pad (`BASH_SOURCE`), na de `stop_hook_active`-check. Ontbreekt het bestand, dan loopt de gate door als nu.

**Tech stack:** bash (GNU en BSD `date`), pytest met de testkit in `tests/pack/_testkit.py`, Go (`internal/packs/core_test.go`).

**Spec:** issue #74 en de spec van #66, [2026-10-25-usage-guard-design.md](../specs/2026-10-25-usage-guard-design.md) (sectie *Scope*). Contract nu: [docs/usage-guard.md](../../../usage-guard.md).

## Globale randvoorwaarden

- Code, commentaar en hookmeldingen in het Engels; plan, docs en commits in het Nederlands.
- Alarmregels ongewijzigd: gewoon bestand, geen symlink, precies vier velden, `kind` `venster` of `week`, `pct` alleen cijfers, `reset_at` exact `^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z$`, epoch via `date -u -d` en anders `date -j -u -f '%Y-%m-%dT%H:%M:%SZ'`, epoch > nu. `_` in `reset_local` wordt een spatie.
- Alarmpad: `${XDG_CACHE_HOME:-${HOME:-}/.cache}/lighthouse/usage-window.alarm`.
- De helper werkt onder `set -u` (guard) én zonder (gates).
- Elk nieuw `.sh` onder `pack/` is schoon onder `shellcheck -S style -x` (`tests/pack/test_shell_style.py`).
- Nooit met de hand in `.claude/hooks/lighthouse/`; de geïnstalleerde kopie komt uit `go run ./cmd/lighthouse init --upgrade`.
- Testgate: `uv run pytest -q && go test -race -shuffle=on ./...`.

## Review-focus

1. **Echt alarm op de machine van de ontwikkelaar.** `test_ty_check.py` draait de geïnstalleerde kopie met `os.environ`; ligt er lokaal een alarm, dan zwijgt de gate en worden bestaande tests rood. Taak 3 zet `XDG_CACHE_HOME` in `_run` op `tmp_path`; taak 2 doet dat in de nieuwe go-tests.
2. **Een herhaalde Stop met `stop_hook_active: true`.** De gate zwijgt dan al stil; de alarmcheck staat erna, dus geen tweede melding per Stop-cyclus. Test in taak 2.
3. **Een onleesbaar alarm** (symlink, drie velden) laat de gate gewoon blokkeren. Test in taak 2 en 3.
4. **Gate uit `pack/` gedraaid, zonder `pack/core` ernaast**, met een geldig alarm: blokkeert als nu. Test in taak 2 en 3.
5. **De guard zonder helper** (kapotte installatie): hij zegt `COULD NOT CHECK` op stderr en eindigt met exit 0, nooit een crash onder `set -u`. Test in taak 1.

---

### Taak 1: helper `usage-guard/alarm.sh`, en de guard gebruikt hem

**Bestanden:**
- Nieuw: `pack/core/hooks/usage-guard/alarm.sh`
- Wijzig: `pack/core/hooks/usage-guard.sh` (stap 2, 5, 6 en 8; regels 38-133)
- Wijzig: `pack/SOURCES.toml` (nieuwe `[[file]]` met `new = true`, naast de entries van `usage-guard/`)
- Wijzig: `internal/packs/core_test.go:59-66` (regel `.claude/hooks/lighthouse/usage-guard/alarm.sh`)
- Test: `tests/pack/test_usage_guard.py`

**Interfaces:**
- Produceert, na `. <pad>/alarm.sh`:
  - variabele `USAGE_ALARM`: het alarmpad (zie randvoorwaarden), gezet bij het sourcen;
  - functie `usage_alarm_valid`: geen argumenten; leest `$USAGE_ALARM`; return 0 bij een geldig alarm en zet dan `USAGE_ALARM_KIND`, `USAGE_ALARM_PCT`, `USAGE_ALARM_LOCAL` (met `_` → spatie), `USAGE_ALARM_AT`; anders return 1 en die vier leeg. Geen fork zonder bestand; geen uitvoer.
  - De helper roept geen `exit` aan en laat `set -f` uit staan na afloop.

- [ ] **Stap 1: falende test schrijven**

In `tests/pack/test_usage_guard.py`:

```python
def test_guard_without_alarm_helper_cannot_check(tmp_path: Path) -> None:
    root = tmp_path / "repo"
    install_core(root)
    (root / ".claude/hooks/lighthouse/usage-guard/alarm.sh").unlink()
    env = guard_env(tmp_path)
    write_alarm(env, alarm_line())
    result = run_guard(root, "stop", {}, env)
    assert result.returncode == 0
    assert result.stdout == ""
    assert "usage-guard: COULD NOT CHECK — usage-guard/alarm.sh is missing" in result.stderr
```

Plus een test op de helper zelf, via `bash -c '. alarm.sh; usage_alarm_valid; echo "$? $USAGE_ALARM_KIND|$USAGE_ALARM_PCT|$USAGE_ALARM_LOCAL|$USAGE_ALARM_AT"'` met `set -u` vooraf, `test_alarm_helper_fields_under_set_u`: alarm `venster 92 14_30 <at>` geeft `0 venster|92|14 30|<at>`; zonder bestand `1 |||`.

- [ ] **Stap 2: rood zien**

Run: `uv run pytest -q tests/pack/test_usage_guard.py -k "helper"`
Verwacht: FAIL (`alarm.sh` bestaat niet; `unlink` geeft `FileNotFoundError`).

- [ ] **Stap 3: helper schrijven en guard ombouwen**

`alarm.sh`: geen shebang-afhankelijk gedrag, kop-commentaar met de regels en wie hem sourcet (guard, `go-check.sh`, `ty-check.sh`), `# shellcheck shell=bash`. De body van `usage_alarm_valid` is stap 6 van de guard met `local` voor `LINE`, `AT_RE`, `AT_EPOCH`, `NOW`; `set -- $LINE` binnen de functie raakt alleen de functie-argumenten.

Guard: `HOOKS_DIR` (nu stap 8, pure parameterexpansie) naar stap 2; daar `. "$HOOKS_DIR/usage-guard/alarm.sh"`; mislukt dat, dan `printf 'usage-guard: COULD NOT CHECK — usage-guard/alarm.sh is missing\n' >&2; exit 0`. `ALARM` wordt `$USAGE_ALARM`. Stap 6 wordt `usage_alarm_valid` en kopieert de vier velden naar `KIND PCT LOCAL AT`. Het kop-commentaar over het snelle pad wordt "one source and one `[ -f ]`: no fork". De `source=`-directive voor shellcheck: `# shellcheck source=usage-guard/alarm.sh`.

- [ ] **Stap 4: groen zien, guard-regressie incluis**

Run: `uv run pytest -q tests/pack/test_usage_guard.py tests/pack/test_usage_guard_lib.py tests/pack/test_shell_style.py tests/test_sources.py tests/pack/test_code_language_corpus.py tests/pack/test_hooks_minimal_config.py && go test ./internal/packs/`
Verwacht: alles PASS (`test_alarm_cases`, `test_bsd_date`, `test_fast_path_runs_nothing`, `test_alarm_fields_are_not_globbed` ongewijzigd groen).

- [ ] **Stap 5: mutatiecheck**

Laat in `alarm.sh` de symlinkcheck weg; `test_alarm_cases` moet rood worden. Zet terug.

- [ ] **Stap 6: commit**

```bash
git add pack/core/hooks/usage-guard/alarm.sh pack/core/hooks/usage-guard.sh pack/SOURCES.toml internal/packs/core_test.go tests/pack/test_usage_guard.py
git commit -m "feat(usage-guard): alarmregels in een eigen helper die de guard sourcet (#74)"
```

### Taak 2: `go-check.sh` zwijgt bij een geldig alarm

**Bestanden:**
- Wijzig: `pack/go/hooks/go-check.sh` (na regel 63, de `stop_hook_active`-check; kop-commentaar regel 24-31 aanvullen)
- Test: `tests/pack/go/test_go_check.py`

**Interfaces:**
- Consumeert: `USAGE_ALARM`, `usage_alarm_valid`, `USAGE_ALARM_LOCAL` uit taak 1.
- Produceert: de meldingstekst `go-check: not checked — the usage alarm is up until <reset_local>; the usage guard wraps up this session.` als `systemMessage`, exit 0.

- [ ] **Stap 1: falende tests schrijven**

Nieuwe helper `_run_installed(root, env, payload=None)`: als `_run`, maar draait `root / ".claude/hooks/lighthouse/go-check.sh"` en zet `HOME` en `XDG_CACHE_HOME` onder `tmp_path` (via `guard_env`-achtige velden, niet `PATH` vervangen). Tests, elk met `STUB_RUN_OUT=LINT_FINDING`, `STUB_RUN_RC=1`:

- `test_valid_usage_alarm_skips_the_lint`: geldig alarm (`alarm_line(local="14_30")`) → `returncode == 0`, `systemMessage == "go-check: not checked — the usage alarm is up until 14 30; the usage guard wraps up this session."`, en `"run" not in` de regels van `STUB_LOG`.
- `test_no_usage_alarm_still_blocks`: geen alarm → `returncode == 2`, `LINT_FINDING in stderr`.
- `test_expired_usage_alarm_still_blocks`: `alarm_line(delta=timedelta(hours=-1))` → `returncode == 2`.
- `test_unreadable_usage_alarm_still_blocks`, parametrized: drie velden, en een symlink naar een geldig alarm → `returncode == 2`.
- `test_without_core_a_valid_alarm_still_blocks`: de bestaande `_run` (kopie in `pack/go/hooks/`, geen `usage-guard/` ernaast), geldig alarm in een `XDG_CACHE_HOME` onder `tmp_path` → `returncode == 2`.
- `test_stop_hook_active_with_alarm_is_silent`: `stop_hook_active: true` plus geldig alarm → `returncode == 0`, `stdout == ""`.

- [ ] **Stap 2: rood zien**

Run: `uv run pytest -q tests/pack/go/test_go_check.py -k "usage_alarm or without_core or stop_hook_active_with_alarm"`
Verwacht: `test_valid_usage_alarm_skips_the_lint` FAIL (returncode 2); de rest PASS (gedrag als nu).

- [ ] **Stap 3: check toevoegen**

Na de `stop_hook_active`-check, vóór `LIB_ROOT`: bepaal de hookmap zoals de guard (`case "${BASH_SOURCE[0]}" in */*) … ;; *) . ;; esac`), en `if [ -f "$DIR/usage-guard/alarm.sh" ] && . "$DIR/usage-guard/alarm.sh" && usage_alarm_valid; then report "…"; fi`. Commentaar in de stijl van het bestand: waarom (de guard laat de sessie afronden; repareren kost budget dat er niet meer is, #74) en dat zonder `pack/core` de check loopt als nu. `jq` is hier al gegarandeerd, dus `reset_local` mag in de melding.

- [ ] **Stap 4: groen zien**

Run: `uv run pytest -q tests/pack/go/ tests/pack/test_shell_style.py`
Verwacht: PASS.

- [ ] **Stap 5: mutatiecheck**

Vervang `usage_alarm_valid` door `true`; `test_expired_usage_alarm_still_blocks` moet rood worden. Zet terug.

- [ ] **Stap 6: commit**

```bash
git add pack/go/hooks/go-check.sh tests/pack/go/test_go_check.py
git commit -m "feat(go): go-check blokkeert niet bij een geldig usage-alarm (#74)"
```

### Taak 3: `ty-check.sh` zwijgt bij een geldig alarm

**Bestanden:**
- Wijzig: `pack/python/hooks/ty-check.sh` (na regel 55; kop-commentaar regel 16-21)
- Test: `tests/pack/python/test_ty_check.py`

**Interfaces:**
- Consumeert: als taak 2.
- Produceert: `ty-check: not checked — the usage alarm is up until <reset_local>; the usage guard wraps up this session.`

- [ ] **Stap 1: `_run` isoleren en falende tests schrijven**

`_run` krijgt `HOME` en `XDG_CACHE_HOME` onder `root.parent` (review-focus 1), zodat een alarm van de ontwikkelaar de suite niet raakt. Daarna dezelfde zes tests als taak 2, op een repo met een ty-diagnose (het patroon van `test_ty_hook_blocks_on_code_when_diagnostics_are_mixed`). De `without_core`-variant draait `PACK / "hooks" / "ty-check.sh"` direct. Voor "ty niet gestart" bij het geldige alarm: een `uvx`-stub die naar een log schrijft, of de assert dat er geen diagnostiek in stderr staat.

- [ ] **Stap 2: rood zien**

Run: `uv run pytest -q tests/pack/python/test_ty_check.py -k "usage_alarm or without_core or stop_hook_active_with_alarm"`
Verwacht: alleen `test_valid_usage_alarm_skips_the_check` FAIL.

- [ ] **Stap 3: check toevoegen**

Zelfde vorm als taak 2, met `HOOK_NAME` in de melding.

- [ ] **Stap 4: groen zien**

Run: `uv run pytest -q tests/pack/python/ tests/pack/test_shell_style.py`
Verwacht: PASS.

- [ ] **Stap 5: mutatiecheck**

Zoals taak 2, stap 5.

- [ ] **Stap 6: commit**

```bash
git add pack/python/hooks/ty-check.sh tests/pack/python/test_ty_check.py
git commit -m "feat(python): ty-check blokkeert niet bij een geldig usage-alarm (#74)"
```

### Taak 4: contract in de docs, dogfood

**Bestanden:**
- Wijzig: `docs/usage-guard.md` (regel 25-33 en 205)
- Gegenereerd: `.claude/hooks/lighthouse/usage-guard.sh`, `.claude/hooks/lighthouse/usage-guard/alarm.sh`, `.claude/hooks/lighthouse/go-check.sh`, `.lighthouse/manifest.toml`

- [ ] **Stap 1: docs**

Bij "Wanneer telt het alarm": de regels staan in `usage-guard/alarm.sh`, die de guard en de blokkerende Stop-gates sourcen. Onder *Grenzen* vervangt een sectie "Andere Stop-gates" de regel van #74: `go-check.sh` en `ty-check.sh` blokkeren niet bij een geldig alarm en melden `not checked — the usage alarm is up …`; waarom (tegenstrijdige opdrachten, budget); de check staat na `stop_hook_active`; zonder `pack/core` (geen helper) gedragen ze zich als nu; een verlopen of onleesbaar alarm telt niet. Een nieuwe blokkerende Stop-gate in een pakket volgt hetzelfde patroon.

- [ ] **Stap 2: dogfood**

Run: `go run ./cmd/lighthouse init --upgrade`, daarna nog een keer.
Verwacht: de tweede run meldt `No changes.`

- [ ] **Stap 3: hele testgate**

Run: `uv run pytest -q && go test -race -shuffle=on ./...`
Verwacht: PASS.

- [ ] **Stap 4: commit**

```bash
git add docs/usage-guard.md .claude/hooks/lighthouse .lighthouse/manifest.toml
git commit -m "docs(usage-guard): contract van zwijgende Stop-gates; geïnstalleerde kopie bijgewerkt (#74)"
```
