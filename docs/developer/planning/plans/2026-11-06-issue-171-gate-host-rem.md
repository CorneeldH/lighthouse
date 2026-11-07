# Test-gate kijkt naar de host (#171) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** De test-gate start geen run als de host te weinig geheugen of swap vrij heeft, haalt zijn slotplafond uit `host.toml`, en de eigen gate van lighthouse laat geen pytest-tmp in `/tmp` achter.

**Architecture:** Drie nieuwe optionele sleutels in `[host]` van `host.toml`, gelezen door `config.py` zoals `gate_slice`. Een nieuw hulpje `lighthouse_lib/host_memory.py` leest `/proc/meminfo` en wacht tot de drempels gehaald worden, zoals `gate_slot.py` op een slot wacht. `commit-test-gate.sh` begrenst `test_gate.slots` met `host.gate_slots` en roept `host_memory.py` aan nadat hij een slot heeft, vóór de run.

**Tech Stack:** bash 3.2 (hook), Python 3 (lib, pytest), TOML.

**Spec:** het issue zelf, #171 (geen aparte spec; de AC leggen het HOE vast).

## Global Constraints

- Code, hookmeldingen en docstrings in het Engels; dit plan, docs onder `docs/` (behalve `docs/install.md`), commitberichten en de PR in het Nederlands.
- Bron van waarheid is `pack/`; de geïnstalleerde kopieën (`.claude/hooks/lighthouse/`, `.lighthouse/lib/lighthouse_lib/`) komen alleen via `go run ./cmd/lighthouse init --upgrade` en gaan in dezelfde commit. CI eist daarna `No changes.`
- Niet fail-open: elke situatie waarin niets getest is, is `COULD NOT CHECK` en exit 2, nooit een pass.
- Bash 3.2: geen `exec {var}>`, geen `declare -A`, geen `${var,,}`.
- `MAX_SLOTS=7` blijft het fd-plafond in de hook (fds 3–9); geen enkele sleutel komt erboven.
- Default zonder sleutel: slots als nu (plafond 7), geen geheugencheck, met een melding waarom niet.
- Wachten op geheugen telt mee in de ene deadline (`DEADLINE`), zoals de slotwacht.

## Besluiten

**Slots: hostplafond naast projectwens.** De lockmap (`/run/user/$UID/lighthouse-gate-slots`) is al hostbreed: alle projecten delen `slot-1 … slot-n`. `[test_gate] slots` (project, default 2) blijft wat een project vraagt; nieuw is `[host] gate_slots` (geheel getal ≥ 1, de hook blokkeert boven 7): het plafond van de host. De hook neemt `min(test_gate.slots, host.gate_slots)`. Zonder `gate_slots` is het plafond `MAX_SLOTS` (7), dus alles werkt als nu. Vraagt het project meer dan de host toestaat, dan meldt de hook `commit-test-gate: test_gate.slots = 3 above host gate_slots = 2, the gate uses 2` en gaat door. Blokkeren zou een project breken op een eigenschap van een andere machine.

**Geheugen: twee drempels, elk los.** `[host] gate_min_mem_available_gb` en `[host] gate_min_swap_free_gb`, getal (int of float) > 0, in GiB zoals `dispatch.min_mem_available_gb`. Elke sleutel controleert alleen zijn eigen waarde. Geen van beide gezet: melding `commit-test-gate: no gate memory threshold in host.toml, host memory not checked`, en de gate werkt als nu.

**Wanneer en hoe lang wachten.** Ná het slot, vóór `mktemp -d`: wie wacht op geheugen houdt zijn slot vast, zodat niet zeven gates tegelijk door dezelfde opening glippen. Het wachtbudget is `min(test_gate.slot_wait_seconds, DEADLINE − nu)`, poll elke seconde. Na het budget: `COULD NOT CHECK — host memory below threshold after <n> s (<tekorten>)`.

**Een host zonder swap.** `SwapTotal: 0 kB` met `gate_min_swap_free_gb` gezet: de swapcheck vervalt, met de melding `commit-test-gate: no swap on this host, SwapFree not checked`. Letterlijk toetsen zou elke commit blokkeren.

**Waar de basetemp heen gaat.** `LIGHTHOUSE_GATE_TMP` is `$BT/run-<n>`, en de `EXIT`-trap van de hook doet al `rm -rf "$BT"`, ook na een deadline-kill (`deadline.py` killt de procesgroep, de hook loopt door en eindigt). Er hoeft dus niets aan de opruiming te veranderen; alleen het commando in `lighthouse.toml` moet `--basetemp` meegeven. Een test bewijst het met een echte pytest.

**Docs.** Er is geen doc over de gate op de host. Nieuw: `docs/test-gate.md` (Nederlands), met de tabel van alle `[host]`-sleutels van de gate en het Beelink-voorbeeld. De `gate_slice`-rij in `docs/secret-scan.md` linkt ernaar.

## Review Focus

1. **Een host zonder swap** met `gate_min_swap_free_gb` gezet: de gate moet testen, niet elke commit blokkeren. Test in taak 2 (`test_no_swap_skips_swap_check`) en taak 4 (melding in de hookoutput).
2. **`/proc/meminfo` onleesbaar of zonder `MemAvailable:`** terwijl een drempel gezet is (oude kernel, container): `COULD NOT CHECK`, nooit een pass. Test in taak 2 (exit 2) en taak 4 (`test_memory_unmeasurable_blocks`).
3. **Geheugenwacht na een lange slotwacht**: de som mag niet voorbij `DEADLINE`. Het budget is `min(slot_wait, DEADLINE − nu)`; test in taak 4 met `deadline_seconds = 2`, `slot_wait_seconds = 30` en te weinig geheugen: klaar binnen ~5 s.
4. **Een drempel als string of 0** in `host.toml` (`"6"`, `0`, `true`): rc 2 uit `config.py`, en de hook zegt `COULD NOT CHECK` met de sleutelnaam. Test in taak 1.
5. **`test_gate.slots` boven `host.gate_slots`**: niet blokkeren, wel melden en het lagere getal gebruiken. Test in taak 3.

---

### Task 1: drie hostsleutels in `config.py`

**Files:**
- Modify: `pack/core/lib/config.py` (`HOST_KEYS` :177, `_OPTIONAL_HOST_KEYS` :185, `Host` :224, `load_host` :465, waardenmap :534)
- Modify: `docs/site/data/config.json` (achter `host.scan_cpu_quota`, :980)
- Test: `tests/pack/test_config.py` (naast de `gate_slice`-tests, :772–809)

**Interfaces:**
- Produces: `config.py host.gate_slots` → print een geheel getal; `config.py host.gate_min_mem_available_gb` en `config.py host.gate_min_swap_free_gb` → print het getal zoals in TOML (`6`, `2.5`). Ontbreekt de sleutel of `host.toml`: rc 1. Verkeerd type of ≤ 0: rc 2, met de sleutelnaam in de fout. `Host` krijgt `gate_slots: int | None`, `gate_min_mem_available_gb: float | None`, `gate_min_swap_free_gb: float | None`.

- [ ] **Stap 1: Schrijf de falende tests** in `tests/pack/test_config.py`, in de stijl van de `gate_slice`-tests:
  - `test_gate_host_keys_from_host` (geparametriseerd over `gate_slots = 2` → `"2"`, `gate_min_mem_available_gb = 6` → `"6"`, `gate_min_swap_free_gb = 2.5` → `"2.5"`): rc 0 en die uitvoer.
  - `test_gate_host_keys_absent` (geparametriseerd over de drie sleutels): sleutel weg → rc 1; geen `host.toml` → rc 1.
  - `test_gate_host_keys_invalid` (geparametriseerd): `gate_slots = "2"`, `gate_slots = 0`, `gate_slots = true`, `gate_slots = 1.5`, `gate_min_mem_available_gb = "6"`, `gate_min_mem_available_gb = 0`, `gate_min_swap_free_gb = -1`, `gate_min_swap_free_gb = true` → rc 2, en stderr noemt `host.<sleutel>`.
- [ ] **Stap 2: Draai ze**: `uv run pytest -q tests/pack/test_config.py -k gate_host` → FAIL (`KeyError`/rc 1 op de onbekende sleutel).
- [ ] **Stap 3: Implementeer** in `load_host`: `gate_slots` int en ≥ 1 (bool uitgesloten); de twee drempels int of float, > 0 (bool uitgesloten), opgeslagen als float. De waardenmap print een float die geheel is als int (`6.0` → `6`), anders `repr`-vrij (`2.5`). Alle drie in `HOST_KEYS` en `_OPTIONAL_HOST_KEYS`.
- [ ] **Stap 4: Map**: drie entries in `docs/site/data/config.json` met `"level": "advanced"`, `"required": "no"`, `"readers": ["pack/core/hooks/commit-test-gate.sh"]`, `about` (Engels, één zin: plafond van de gateslots op deze host, default 7 / minimale `MemAvailable` in GiB voordat de gate start / minimale `SwapFree` in GiB voordat de gate start), `example` `2`, `6`, `4`. De reader bestaat pas na taak 3 en 4: zet de entries tot dan op `"readers": []` en voeg de sleutels toe aan `UNREAD` in `tests/test_site_config.py:24`; taak 3 en 4 halen ze er weer uit.
- [ ] **Stap 5: Draai**: `uv run pytest -q tests/pack/test_config.py tests/test_site_config.py` → PASS.
- [ ] **Stap 6: Dogfood en commit**:
  ```bash
  go run ./cmd/lighthouse init --upgrade
  git add pack/core/lib/config.py .lighthouse/lib/lighthouse_lib/config.py docs/site/data/config.json tests/pack/test_config.py tests/test_site_config.py
  git commit -m "feat(config): host.gate_slots en twee geheugendrempels voor de gate in host.toml (#171)"
  ```

### Task 2: `host_memory.py` meet en wacht

**Files:**
- Create: `pack/core/lib/host_memory.py`
- Modify: `pack/SOURCES.toml` (`[[file]] path = "pack/core/lib/host_memory.py"`, `new = true`, alfabetisch bij de andere lib-bestanden)
- Modify: `internal/packs/core_test.go:110` (`.lighthouse/lib/lighthouse_lib/host_memory.py` in de lijst)
- Test: `tests/pack/test_host_memory.py`

**Interfaces:**
- Produces (Python):
  - `read_meminfo(path: Path) -> dict[str, int]` — waarden in kB; `OSError` of `ValueError` als het bestand niet leesbaar is of `MemAvailable:` mist.
  - `shortfalls(info: dict[str, int], min_available_gib: float | None, min_swap_free_gib: float | None) -> tuple[list[str], list[str]]` — (tekorten, notities). Tekort: `"MemAvailable 0.7 GiB < 4 GiB"`, `"SwapFree 0.5 GiB < 2 GiB"` (gemeten waarde met één decimaal, drempel met `%g`). Notitie: `"no swap on this host, SwapFree not checked"` bij `SwapTotal == 0` en een swapdrempel.
- Produces (CLI): `python3 host_memory.py --meminfo PATH [--min-available-gib X] [--min-swap-free-gib Y] --wait SECONDS [--interval SECONDS]` (interval default 1). Exit 0: drempels gehaald; stdout per notitie één regel. Exit 75: na `--wait` nog tekort; stdout één regel, de tekorten gescheiden door `; `. Exit 2: niet meetbaar; stderr de reden.

- [ ] **Stap 1: Schrijf de falende tests** in `tests/pack/test_host_memory.py`, met een nagemaakte meminfo (`MemTotal`, `MemAvailable`, `SwapTotal`, `SwapFree` in kB) in `tmp_path`:
  - `test_shortfalls_both_low`: `MemAvailable: 734003 kB`, `SwapFree: 524288 kB`, drempels 4 en 2 → `["MemAvailable 0.7 GiB < 4 GiB", "SwapFree 0.5 GiB < 2 GiB"]`, geen notities.
  - `test_shortfalls_only_set_threshold_counts`: alleen `min_available_gib=4`, swap laag → alleen de `MemAvailable`-regel.
  - `test_no_swap_skips_swap_check`: `SwapTotal: 0 kB`, `SwapFree: 0 kB`, swapdrempel 2 → geen tekort, notitie `"no swap on this host, SwapFree not checked"`.
  - `test_read_meminfo_missing_memavailable`: bestand zonder die regel → `ValueError`; ontbrekend bestand → `OSError`.
  - `test_cli_ok` (exit 0), `test_cli_times_out` (`--wait 0`, te laag → exit 75, stdout `MemAvailable 0.7 GiB < 4 GiB`), `test_cli_waits_until_enough` (`--wait 5 --interval 0.1`, een `threading.Timer(0.5)` herschrijft het bestand naar genoeg → exit 0 binnen 3 s), `test_cli_unreadable` (exit 2, stderr noemt het pad).
- [ ] **Stap 2: Draai**: `uv run pytest -q tests/pack/test_host_memory.py` → FAIL (module bestaat niet).
- [ ] **Stap 3: Implementeer** `host_memory.py` met `argparse` zoals `gate_slot.py`; de lus meet, slaapt `interval`, en stopt op `time.monotonic()` ≥ start + `wait`; minstens één meting, ook bij `--wait 0`. Een leesfout midden in het wachten is exit 2.
- [ ] **Stap 4: Registreer** in `pack/SOURCES.toml` en `internal/packs/core_test.go`.
- [ ] **Stap 5: Draai**: `uv run pytest -q tests/pack/test_host_memory.py tests/test_sources.py && go test ./internal/packs/...` → PASS na `go run ./cmd/lighthouse init --upgrade`.
- [ ] **Stap 6: Commit** (met `.lighthouse/lib/lighthouse_lib/host_memory.py`):
  ```bash
  git commit -m "feat(gate): host_memory.py meet MemAvailable en SwapFree en wacht op een drempel (#171)"
  ```

### Task 3: slots begrensd door `host.gate_slots`

**Files:**
- Modify: `pack/core/hooks/commit-test-gate.sh` (lezen bij :331, range-check bij :339, header :59–71)
- Modify: `tests/test_site_config.py` (`host.gate_slots` uit `UNREAD`), `docs/site/data/config.json` (reader terug)
- Test: `tests/pack/test_commit_test_gate.py` (bij de slottests, :721–766)

**Interfaces:**
- Consumes: `config.py host.gate_slots` (taak 1).
- Produces: in de hook `SLOTS` = het effectieve aantal; de bestaande busy-melding `all gate slots busy after <n> s (slots: <SLOTS>)` toont dat getal.

- [ ] **Stap 1: Schrijf de falende tests**:
  - `test_host_gate_slots_caps_the_project`: `gate.configure(TEST_GATE + "slots = 3\n")`, `host.toml` = `HOST + "gate_slots = 2\n"`, `hold_slots(gate, "slot-1", "slot-2")` → exit 2, `"all gate slots busy after 1 s (slots: 2)"` in stderr, `not gate.mark.exists()`, en `"test_gate.slots = 3 above host gate_slots = 2, the gate uses 2"` in stderr (de melding komt vóór de blok-uitvoer).
  - `test_host_gate_slots_above_project_changes_nothing`: `slots = 2` in het project, `gate_slots = 5` → met slot-1 en slot-2 bezet: busy met `(slots: 2)`.
  - `test_without_host_gate_slots_ceiling_is_seven`: `slots = 7`, geen `gate_slots`, slot-1 t/m slot-6 bezet → exit 0 (bestaand gedrag).
  - `test_host_gate_slots_out_of_range_blocks`: `gate_slots = 8` → exit 2, `COULD NOT CHECK`, `host.gate_slots` in stderr.
  - `test_host_gate_slots_invalid_blocks`: `gate_slots = "2"` → exit 2, `COULD NOT CHECK`.
- [ ] **Stap 2: Draai**: `uv run pytest -q tests/pack/test_commit_test_gate.py -k host_gate_slots` → FAIL.
- [ ] **Stap 3: Implementeer**: na `read_config test_gate.slots` → `HOST_SLOTS=$MAX_SLOTS; if read_config host.gate_slots; then HOST_SLOTS=$CONFIG_VALUE; fi`. Range-check `1..MAX_SLOTS` op beide, met de sleutelnaam in de blokmelding. Is `SLOTS > HOST_SLOTS`: `add_message` met de tekst uit taak-interface en `SLOTS=$HOST_SLOTS`. Een rc 2 van `read_config` loopt al via het bestaande pad naar `COULD NOT CHECK` (controleer dat `read_config` dat voor host-sleutels doet zoals voor `host.gate_slice`). Werk de header (*Why slots*) bij: één zin over het hostplafond.
- [ ] **Stap 4: Draai**: `uv run pytest -q tests/pack/test_commit_test_gate.py tests/test_site_config.py` → PASS.
- [ ] **Stap 5: Dogfood en commit** (met `.claude/hooks/lighthouse/commit-test-gate.sh`):
  ```bash
  go run ./cmd/lighthouse init --upgrade
  git commit -m "feat(gate): host.gate_slots begrenst de slots van elk project op deze host (#171)"
  ```

### Task 4: geen run bij te weinig geheugen

**Files:**
- Modify: `pack/core/hooks/commit-test-gate.sh` (na het sluiten van de vrije slot-fds, vóór `mktemp -d` bij :410; header: nieuwe alinea *Why a host memory check*)
- Modify: `tests/test_site_config.py` (twee drempels uit `UNREAD`), `docs/site/data/config.json` (readers terug)
- Test: `tests/pack/test_commit_test_gate.py`

**Interfaces:**
- Consumes: `config.py host.gate_min_mem_available_gb`, `host.gate_min_swap_free_gb` (taak 1); `host_memory.py` CLI (taak 2).
- Produces: env `LIGHTHOUSE_GATE_MEMINFO` (alleen voor tests, zoals `LIGHTHOUSE_GATE_CGROUP_ROOT`); default `/proc/meminfo`.

- [ ] **Stap 1: Schrijf de falende tests**, met een helper `meminfo(gate, avail_kb, swap_free_kb, swap_total_kb=25165824)` die `gate.tmp / "meminfo"` schrijft en de test geeft `LIGHTHOUSE_GATE_MEMINFO` aan `gate.run`:
  - `test_memory_low_blocks_with_values`: `HOST + "gate_min_mem_available_gb = 4\ngate_min_swap_free_gb = 2\n"`, meminfo 0,7 GiB / 0,5 GiB → exit 2, stderr bevat `COULD NOT CHECK — host memory below threshold after 1 s (MemAvailable 0.7 GiB < 4 GiB; SwapFree 0.5 GiB < 2 GiB)`, `not gate.mark.exists()`. Ook in `post`-modus (via `gate.post` na een `pre`): de `post_untested`-regel staat erin.
  - `test_memory_waits_then_runs`: `TEST_GATE` met `slot_wait_seconds = 5`, meminfo laag; een `threading.Timer(1.0)` schrijft genoeg → exit 0, `gate.marks() == [str(gate.project.resolve())]`.
  - `test_memory_enough_runs_without_notice`: genoeg → exit 0, geen `host memory` in stdout.
  - `test_without_memory_threshold_says_so` (geparametriseerd over `host.toml` weg en `HOST` zonder drempels) → exit 0, `"commit-test-gate: no gate memory threshold in host.toml, host memory not checked"` in `_context(result.stdout)`.
  - `test_memory_unmeasurable_blocks`: drempel gezet, `LIGHTHOUSE_GATE_MEMINFO` wijst naar niets → exit 2, `COULD NOT CHECK — host memory not measurable`.
  - `test_memory_wait_stays_within_deadline`: `deadline_seconds = 2`, `slot_wait_seconds = 30`, te laag → exit 2 binnen 6 s (meet met `time.monotonic()`), `after 2 s` of `after 1 s` in stderr (budget = resterende deadline, naar beneden afgerond).
  - `test_no_swap_notice_in_gate`: alleen `gate_min_swap_free_gb = 2`, `SwapTotal: 0 kB` → exit 0, `"commit-test-gate: no swap on this host, SwapFree not checked"` in de context.
- [ ] **Stap 2: Draai**: `uv run pytest -q tests/pack/test_commit_test_gate.py -k memory` → FAIL.
- [ ] **Stap 3: Implementeer** in de hook: lees beide drempels met `read_config`; geen van beide → `add_message` (tekst uit *Besluiten*). Anders `MEM_WAIT = min(SLOT_WAIT, DEADLINE − $(date +%s))` (≥ 0), en `python3 "$LIB/host_memory.py" --meminfo "${LIGHTHOUSE_GATE_MEMINFO:-/proc/meminfo}" [--min-available-gib …] [--min-swap-free-gib …] --wait "$MEM_WAIT"` met stdout naar een variabele en stderr naar `$ERR_FILE`. Exit 0: elke stdout-regel → `add_message "$HOOK_NAME: <regel>"`. Exit 75: blok `COULD NOT CHECK — host memory below threshold after $MEM_WAIT s (<stdout>)`, met dezelfde opbouw als de slot-busy-melding (pre: "Try the commit again shortly.", post: `post_untested`). Andere exit: `COULD NOT CHECK — host memory not measurable: <stderr>`, ook met `post_untested`.
- [ ] **Stap 4: Draai**: `uv run pytest -q tests/pack/test_commit_test_gate.py tests/pack/test_hooks_minimal_config.py tests/test_site_config.py` → PASS. Controleer dat de bestaande tests met een exacte melding (o.a. :985) groen blijven: `HOST` heeft geen drempels, dus ze krijgen er alleen de notitie bij in de context.
- [ ] **Stap 5: Dogfood en commit**:
  ```bash
  go run ./cmd/lighthouse init --upgrade
  git commit -m "feat(gate): geen run onder de geheugendrempel van de host, wachten binnen de deadline (#171)"
  ```

### Task 5: `--basetemp` in de eigen gate

**Files:**
- Modify: `lighthouse.toml:44`
- Test: `tests/pack/test_commit_test_gate.py` (echte pytest, deadline-kill) en `tests/test_lighthouse_toml.py` (nieuw, of bij een bestaande test die `lighthouse.toml` leest: zoek eerst met `grep -rln "lighthouse.toml" tests/*.py`)

**Interfaces:**
- Consumes: `LIGHTHOUSE_GATE_TMP` (bestaand, `$BT/run-<n>`).

- [ ] **Stap 1: Schrijf de falende tests**:
  - `test_own_gate_passes_basetemp`: lees `lighthouse.toml` met `tomllib`; `command` bevat `pytest -q --basetemp="$LIGHTHOUSE_GATE_TMP"`.
  - `test_real_pytest_leaves_nothing_in_tmp` (geparametriseerd: `green` en `deadline`): `TMPDIR = gate.tmp / "t"` (leeg aangemaakt) via `gate.run(..., TMPDIR=...)`; een testbestand buiten het project met `def test_x(tmp_path): (tmp_path / "f").write_text("x")` plus, voor `deadline`, `time.sleep(30)`; `gate.configure("[test_gate]\ncommand = '<sys.executable> -m pytest -q -p no:cacheprovider --basetemp=\"$LIGHTHOUSE_GATE_TMP\" <bestand>'\nslot_wait_seconds = 1\n" + ("deadline_seconds = 3\n" if deadline))`. Verwacht: `green` → exit 0; `deadline` → exit 2 met `ran past the deadline`. In beide: `list((gate.tmp / "t").iterdir()) == []` (geen `pytest-of-*`, geen gate-tmp).
- [ ] **Stap 2: Draai**: `uv run pytest -q tests/pack/test_commit_test_gate.py -k real_pytest tests/test_lighthouse_toml.py` → `test_own_gate_passes_basetemp` FAIL; `test_real_pytest_leaves_nothing_in_tmp` mag al groen zijn (de trap ruimt al op): noteer dat in de commit, de test borgt het gedrag.
- [ ] **Stap 3: Implementeer**: in `lighthouse.toml` `uv run pytest -q` → `uv run pytest -q --basetemp=\"$LIGHTHOUSE_GATE_TMP\"` (TOML basic string: de quotes escapen). Controleer dat `gate_python.py` de extra optie vóór de bestandslijst laat staan: `LIGHTHOUSE_GATE_TMP=$(mktemp -d) python3 pack/python/scripts/gate_python.py -- uv run pytest -q --basetemp="$LIGHTHOUSE_GATE_TMP"` met `LIGHTHOUSE_GATE_FILES` op een bestand met `tests/pack/test_host_memory.py` → draait alleen dat bestand, exit 0.
- [ ] **Stap 4: Draai**: de tests uit stap 2 → PASS.
- [ ] **Stap 5: Commit**:
  ```bash
  git commit -m "fix(gate): de eigen gate geeft --basetemp mee aan pytest, niets meer in /tmp (#171)"
  ```

### Task 6: docs en changelog

**Files:**
- Create: `docs/test-gate.md`
- Modify: `docs/secret-scan.md:101` (link bij `gate_slice`)
- Create: `changelog.d/issue-171-gate-host-rem.md`

- [ ] **Stap 1: Schrijf `docs/test-gate.md`** (Nederlands): kop *De test-gate op de host*; tabel `[host]`-sleutels met kolommen Sleutel / Standaard / Betekenis voor `gate_slice`, `gate_slots` (standaard 7, plafond 7; het project vraagt met `[test_gate] slots`, de gate neemt het minimum), `gate_min_mem_available_gb` en `gate_min_swap_free_gb` (niet gezet: geen check, met melding); de volgorde slot → geheugen → run en het wachtbudget; de drie meldingen letterlijk (`host memory below threshold`, `host memory not measurable`, `no gate memory threshold in host.toml`); het Beelink-voorbeeld:
  ```toml
  [host]
  gate_slice = "gates.slice"
  gate_slots = 2
  gate_min_mem_available_gb = 6
  gate_min_swap_free_gb = 4
  ```
  met één zin waarom: `gates.slice` heeft `MemoryHigh=7G` en één gate piekt op 2,3 GB, dus twee slots; de drempels laten de host ruimte voor de vaste bewoners. Plus een alinea over `--basetemp="$LIGHTHOUSE_GATE_TMP"` in `[test_gate] command`.
- [ ] **Stap 2: Link** vanuit de `gate_slice`-rij in `docs/secret-scan.md` naar `test-gate.md` (relatief).
- [ ] **Stap 3: Changelog** `changelog.d/issue-171-gate-host-rem.md`:
  ```markdown
  ---
  type: changed
  ---
  The test gate waits, within its deadline, while the host has less `MemAvailable` or free swap than `gate_min_mem_available_gb` and `gate_min_swap_free_gb` in `host.toml`, `gate_slots` caps the gate slots per host, and lighthouse's own gate passes `--basetemp` to pytest.
  ```
- [ ] **Stap 4: Verifieer**: `uv run pytest -q tests/test_fragments.py tests/pack/test_md_links.py` (of de fragment-/linktest die bestaat) → PASS; `.claude/hooks/lighthouse/md-link-check.sh` blokkeert niet.
- [ ] **Stap 5: Commit**:
  ```bash
  git commit -m "docs(gate): hostsleutels van de test-gate met het Beelink-voorbeeld (#171)"
  ```

### Afronding

- [ ] `go run ./cmd/lighthouse init --upgrade` → `No changes.`
- [ ] `[test_gate] command` uit `lighthouse.toml` volledig groen.
- [ ] `/lighthouse-write-pr` voor `Closes #171`.
