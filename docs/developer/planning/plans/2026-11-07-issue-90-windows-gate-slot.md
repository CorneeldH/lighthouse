# Issue #90 — het gate-slot in deadline.py: implementatieplan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `commit-test-gate` test een commit op native Windows, met een gate-slot dat
`deadline.py` zelf vasthoudt, terwijl Linux en macOS zich precies als nu gedragen.

**Architecture:** `deadline.py` krijgt vier opties (`--slot-dir --slots --slot-wait
--slot-report`) die het slot nemen met `gate_slot.acquire` op fd's die het zelf opent, de uitslag
in een rapportbestand schrijven en het slot vasthouden tot het commando eindigt. De hook kiest een
**procestak** (Git Bash op Windows, of `LIGHTHOUSE_GATE_SLOT_MODE=process` in tests): geen fd's,
geen `gate_slot.py`, wel deze opties per root, en daarna het rapport lezen. De **fd-tak** blijft
byte voor byte zoals nu.

**Tech Stack:** bash 3.2 (de hook), Python 3.9+ (`pack/core/lib`), pytest via `uv run pytest`,
GitHub Actions (`windows-measure`).

**Spec:** [2026-11-07-issue-90-windows-gate-slot-design.md](../specs/2026-11-07-issue-90-windows-gate-slot-design.md)

## Global Constraints

- PR met `Refs #90`, niet `Closes`: AC 2 en 3 van #90 blijven open.
- Linux en macOS gedragen zich precies als nu: de bestaande tests in `test_commit_test_gate.py`,
  `test_gate_slot.py` en `test_filelock.py` blijven groen **zonder aanpassing**.
- Elke fout in de slotlogica is `COULD NOT CHECK` met exit 2; nooit een ongeteste commit.
- `filelock.py` verandert niet. `gate_slot.acquire` verhuist niet; `deadline.py` importeert het.
- De weigering in `gate_slot.main` blijft en houdt de deeltekst
  `gate slots need flock, which this platform lacks (Windows)`.
- Code, commentaar, testnamen en meldingen van de hook zijn Engels. Dit plan, `docs/test-gate.md`
  en commitberichten Nederlands; het changelog-fragment Engels.
- Hook: bash 3.2 (geen `exec {var}>>`, geen `declare -A`, geen `mapfile`). Python: 3.9
  (`from __future__ import annotations` staat er al).
- Dogfood: na elke wijziging onder `pack/` draait `go run ./cmd/lighthouse init --upgrade --yes`
  (zonder `--yes` weigert init zonder TTY en schrijft niets, `internal/initcmd/initcmd.go:210`) en
  gaat de geïnstalleerde kopie (`.claude/hooks/lighthouse/`, `.lighthouse/lib/lighthouse_lib/`)
  plus `.lighthouse/manifest.toml` (sha256 per geïnstalleerd bestand) mee in dezelfde commit.
  Daarna geeft `go run ./cmd/lighthouse init --upgrade </dev/null` `No changes.` (de CI-check,
  `.github/workflows/ci.yml:143`), en `git status --porcelain` is na de commit leeg.
- Changelog: `changelog.d/issue-90-windows-gate-slot.md`, frontmatter `type: fixed`, 1-3 Engelse
  zinnen.
- Elk shellcommando begint met `cd /home/corneel/repositories/lighthouse/.claude/worktrees/issue-90-windows-gate-slot`
  (de cwd drift terug naar de hoofdcheckout).

## Review Focus

1. **Een suite die zelf met 75 of 1 eindigt in de procestak.** Verwacht: rood ("tests failed"),
   geen `busy`-melding en geen slotfout; het rapport is de bron van waarheid. → Task 2,
   `test_process_mode_suite_exit_75_is_a_failure_not_busy`.
2. **`pytest --basetemp="$LIGHTHOUSE_GATE_TMP"` maakt `GATE_TMP` leeg.** Verwacht: het rapport
   staat ernaast en overleeft dat. → Task 2, `test_process_mode_report_survives_a_wiped_gate_tmp`.
3. **De hook wordt gestopt terwijl de suite draait.** Verwacht: `deadline.py` houdt het slot vast
   tot zijn deadline plus grace. → Task 2, `test_process_mode_slot_stays_held_until_deadline_py_ends`
   (Linux). De Windows-kant (stopt Claude Code de hele boom?) is een meetpunt voor AC 3, geen test.
4. **`slot_wait_seconds` langer dan wat er van de deadline over is.** Verwacht: `busy` binnen de
   deadline, niet pas na `W`. → Task 1, `test_slot_wait_is_capped_by_the_deadline`.
5. **Een commit over twee roots met `slots = 1`.** Verwacht: de tweede root pakt het slot nadat
   de eerste het losliet; geen zelfblokkade. → Task 2, `test_process_mode_two_roots_with_one_slot`.

---

### Task 1: `deadline.py` neemt het slot met `--slot-*`-opties

**Files:**
- Modify: `pack/core/lib/deadline.py` (`USAGE`, `main`, nieuwe helpers)
- Modify: `pack/core/lib/gate_slot.py:65` (alleen de tekst van de weigering)
- Test: `tests/pack/test_deadline.py` (nieuwe tests onderaan)
- Dogfood: `.lighthouse/lib/lighthouse_lib/deadline.py`, `.lighthouse/lib/lighthouse_lib/gate_slot.py`,
  `.lighthouse/manifest.toml`

**Interfaces:**
- Consumes: `gate_slot.acquire(fds: list[int], wait: float, interval: float = 1.0) -> int | None`,
  `gate_slot.SLOT_BUSY_EXIT == 75`, `run_until(deadline, argv) -> int` (bestaand).
- Produces (Task 2 leunt hierop):
  - CLI: `deadline.py <epoch> [--slot-dir D --slots N --slot-wait W --slot-report F] -- <cmd> [args...]`.
    Alle vier opties of geen; elke optie precies één keer, met een waarde als los argument.
  - Rapport `F`, precies één regel: `<n>\n` (slot gepakt, `1 <= n <= N`), `busy\n`, of
    `error: <oorzaak>\n`.
  - Exitcodes: met slot de exitcode van het commando (zoals nu); `busy` → 75; `error` → 1;
    usagefout → 2 (zonder rapport).
  - `USAGE = "usage: deadline.py <epoch-seconds> [--slot-dir D --slots N --slot-wait W --slot-report F] -- <cmd> [args...]"`.
  - `SLOT_ERROR_EXIT = 1`.
  - `run_with_slot(deadline: float, argv: list[str], slot_dir: str, slots: int, wait: float, report: str) -> int`.

- [ ] **Step 1: Schrijf de falende tests in `tests/pack/test_deadline.py`**

POSIX-tests via de CLI (`[sys.executable, str(INSTALLED_LIB / "deadline.py"), ...]`, zoals
`test_cli_runs_command_and_passes_exit_code`). Een helper `_slot_cli(slot_dir, report, *cmd,
slots="2", wait="0", deadline=None, popen=False)` bouwt die argv. Een "vastgehouden" slot is een
eigen `os.open(path, O_WRONLY|O_CREAT|O_APPEND)` met `fcntl.flock(fd, LOCK_EX|LOCK_NB)`, zoals
`_hold` in `test_gate_slot.py`.

```python
def test_slot_options_take_the_first_slot_and_pass_the_exit_code(tmp_path):
    # slots 2, cmd `sh -c "exit 7"`
    assert r.returncode == 7
    assert report.read_text() == "1\n"

def test_slot_options_skip_a_held_slot(tmp_path):
    # slot-1 vastgehouden door deze testprocess
    assert r.returncode == 0 and report.read_text() == "2\n"

def test_slot_is_held_while_the_command_runs_and_free_after(tmp_path):
    # Popen; cmd: sh -c 'echo > started; while [ ! -e go ]; do sleep 0.05; done'
    # poll tot `started` bestaat (max 5 s), dan:
    with pytest.raises(BlockingIOError):
        fcntl.flock(probe, fcntl.LOCK_EX | fcntl.LOCK_NB)   # probe op slot-1
    # maak `go`, proc.wait(timeout=10) == 0, dan slaagt flock op een nieuwe probe

def test_slot_busy_writes_busy_and_does_not_run(tmp_path):
    # slot-1 en slot-2 vastgehouden, wait 0, cmd `touch ran`
    assert r.returncode == 75
    assert report.read_text() == "busy\n"
    assert not (tmp_path / "ran").exists()

def test_slot_wait_is_capped_by_the_deadline(tmp_path):
    # slots 1, slot-1 vastgehouden, --slot-wait 30, deadline = time.time() + 1
    assert r.returncode == 75 and report.read_text() == "busy\n"
    assert elapsed < 5

def test_slot_error_when_the_lock_dir_is_missing(tmp_path):
    # --slot-dir tmp_path / "nope", cmd `touch ran`
    assert r.returncode == 1
    assert report.read_text().startswith("error: ")
    assert report.read_text().endswith("\n")
    assert not (tmp_path / "ran").exists()

def test_slot_error_when_a_lock_file_is_a_directory(tmp_path):
    # (slot_dir / "slot-1").mkdir()
    assert r.returncode == 1 and report.read_text().startswith("error: ")

@pytest.mark.parametrize("opts", [
    ["--slot-dir", "D"],                                                   # deels
    ["--slot-dir", "D", "--slots", "2", "--slot-wait", "0"],              # zonder report
    ["--slot-dir", "D", "--slots", "x", "--slot-wait", "0", "--slot-report", "F"],
    ["--slot-dir", "D", "--slots", "0", "--slot-wait", "0", "--slot-report", "F"],
    ["--slot-dir", "D", "--slots", "2", "--slot-wait", "-1", "--slot-report", "F"],
    ["--slot-dir", "D", "--slot-dir", "D", "--slots", "2", "--slot-wait", "0", "--slot-report", "F"],
    ["--bogus", "1"],
])
def test_slot_options_usage_errors_exit_2(opts):
    # argv: <epoch> *opts -- true
    assert r.returncode == 2 and "usage" in r.stderr.lower()
    assert "--slot-report" in r.stderr

def test_without_slot_options_gate_slot_is_not_imported(monkeypatch):
    monkeypatch.delitem(sys.modules, "lighthouse_lib.gate_slot", raising=False)
    assert dl.main([str(time.time() + 30), "--", "true"]) == 0
    assert "lighthouse_lib.gate_slot" not in sys.modules
```

De `msvcrt`-kant, in-process met de fake uit `test_filelock.py` (`from tests.pack.test_filelock
import FakeMsvcrt`; `test_filelock.py` zelf verandert niet). Fixture `windows_gate_slot`:
`monkeypatch.setitem(sys.modules, "fcntl", None)`, `setitem(sys.modules, "msvcrt", fake)`,
`win = load_hook_lib("filelock")`, `import lighthouse_lib.gate_slot as gs`,
`monkeypatch.setattr(gs, "filelock", win)` en `monkeypatch.setitem(sys.modules,
"lighthouse_lib.filelock", win)`; geeft `(win, fake)` terug. `dl.main` draait daarna in dit
proces, dus `deadline.py` raakt via `gate_slot` de fake.

```python
def test_slot_options_work_without_fcntl(tmp_path, windows_gate_slot):
    win, fake = windows_gate_slot
    rc = dl.main([str(time.time() + 30), "--slot-dir", str(d), "--slots", "2",
                  "--slot-wait", "0", "--slot-report", str(report), "--", "true"])
    assert rc == 0 and report.read_text() == "1\n"
    assert os.stat(d / "slot-1").st_ino in fake.held

def test_slot_busy_without_fcntl(tmp_path, windows_gate_slot):
    win, fake = windows_gate_slot
    # slots 1; slot-1 vooraf vast via win.try_lock(os.open(d / "slot-1", ...)); cmd `touch ran`
    assert rc == 75 and report.read_text() == "busy\n"
    assert not (tmp_path / "ran").exists()
```

- [ ] **Step 2: Draai de tests en zie ze falen**

Run: `uv run pytest tests/pack/test_deadline.py -q`
Expected: de nieuwe tests FAIL (usage exit 2 op `--slot-dir`, geen rapport), behalve
`test_without_slot_options_gate_slot_is_not_imported`: die pint gedrag dat er al is en slaagt nu
al. De bestaande PASS.

- [ ] **Step 3: Implementeer in `pack/core/lib/deadline.py`**

- `_gate_slot() -> types.ModuleType`: de bootstrap van `gate_slot.py` (`if not __package__:
  sys.path.insert(0, str(Path(__file__).resolve().parents[1]))`), daarna
  `from lighthouse_lib import gate_slot` en teruggeven. Alleen aangeroepen vanuit
  `run_with_slot`; de imports op moduleniveau blijven zoals ze zijn.
- `_parse(argv: list[str]) -> tuple[float, dict[str, str] | None, list[str]] | None`: met de hand,
  geen `argparse` (die zou `--` en het commando erachter anders lezen). `argv[0]` is de epoch;
  daarna optie/waarde-paren tot de eerste `--`; het commando is alles erna en niet leeg. Geen
  opties → `None` voor de dict. `None` als resultaat bij elke usagefout: onbekende of dubbele
  optie, ontbrekende waarde, niet alle vier, `--slots` geen int of `< 1`, `--slot-wait` geen
  float of `< 0`.
- `run_with_slot(...)`: open `slot_dir/slot-1` … `slot-N` met
  `os.open(path, os.O_WRONLY | os.O_CREAT | os.O_APPEND, 0o600)`; effectieve wacht
  `max(0.0, min(wait, deadline - time.time()))`; `gate_slot.acquire(fds, effective)` (interval
  1.0). Elke `OSError` bij openen of `acquire` → rapport `error: <err>` en `SLOT_ERROR_EXIT`.
  `None` → rapport `busy`, return `gate_slot.SLOT_BUSY_EXIT`. Slot `n` → rapport `f"{n}\n"`,
  sluit de andere fd's, `return run_until(deadline, argv)` met het slot-fd open tot na de return
  (sluit het in een `finally`). Het rapport schrijft `_report(path, line)` met
`open(path, "w", encoding="utf-8", newline="\n")`: op Windows anders `1\r\n`, en dan past de
`case` in de hook niet. Lukt dat schrijven niet
  (`OSError`), dan één regel op stderr (`deadline.py: cannot write slot report: …`), het commando
  start niet, return `SLOT_ERROR_EXIT`. Het commando start nooit vóór het rapport geschreven is.
- `main`: `_parse`; `None` → `USAGE` op stderr, return 2. Zonder opties `run_until(deadline,
  cmd)` zoals nu; met opties `run_with_slot`.
- Docstring van de module: één alinea over de slotopties (waarom: op Windows hoort een
  `msvcrt`-slot bij het proces, dus moet het proces dat de suite bewaakt het houden).

In `pack/core/lib/gate_slot.py:65` wordt de melding
`gate_slot.py: gate slots need flock, which this platform lacks (Windows); the hook takes the slot in deadline.py there\n`.

- [ ] **Step 4: Draai de tests en zie ze slagen**

Run: `uv run pytest tests/pack/test_deadline.py tests/pack/test_gate_slot.py tests/pack/test_filelock.py -q`
Expected: alles PASS.

- [ ] **Step 5: Mutatiecheck**

Zet in `run_with_slot` tijdelijk het sluiten van het slot-fd vóór `run_until`; draai
`uv run pytest tests/pack/test_deadline.py -q -k held_while`. Expected: FAIL. Zet het terug.
Zet tijdelijk de effectieve wacht op `wait` (zonder `min`); `-k capped` moet FAIL geven. Terug.

- [ ] **Step 6: Dogfood en commit**

```bash
cd /home/corneel/repositories/lighthouse/.claude/worktrees/issue-90-windows-gate-slot
go run ./cmd/lighthouse init --upgrade --yes
go run ./cmd/lighthouse init --upgrade </dev/null   # Expected: No changes.
git add pack/core/lib/deadline.py pack/core/lib/gate_slot.py tests/pack/test_deadline.py .lighthouse/lib/lighthouse_lib/ .lighthouse/manifest.toml
git commit -m "feat(test-gate): deadline.py neemt en houdt het gate-slot met --slot-* (#90)"
git status --porcelain                              # Expected: leeg
```

---

### Task 2: de procestak in `commit-test-gate.sh`

**Files:**
- Modify: `pack/core/hooks/commit-test-gate.sh` (header "Why slots", slotblok `:390-448`, lus
  vanaf `:552`)
- Test: `tests/pack/test_commit_test_gate.py` (nieuwe tests na `test_slot_stays_held_until_deadline_py_ends`)
- Dogfood: `.claude/hooks/lighthouse/commit-test-gate.sh`, `.lighthouse/manifest.toml`

**Interfaces:**
- Consumes: de CLI, het rapport en de exitcodes van Task 1.
- Produces: `LIGHTHOUSE_GATE_SLOT_MODE=process` (alleen voor tests) en de procestak op
  `uname -s` = `MINGW*`, `MSYS*` of `CYGWIN*`.

- [ ] **Step 1: Schrijf de falende tests**

Een helper `process(gate, cmd="git commit -m x", **env)` roept `gate.run(cmd, gate.project,
LIGHTHOUSE_GATE_SLOT_MODE="process", **env)` aan. `NO_GATE_SLOT_PY` is de python3-stub uit
`test_gate_blocks_when_gate_slot_breaks` (`*gate_slot.py) echo kapot >&2; exit 2`). `process()`
zet die stub altijd, tenzij de test zelf een andere `python3`-stub zet
(`test_process_mode_without_report_blocks`). Zo bewijst elke procestest dat de procestak
`gate_slot.py` niet aanroept; zonder de stub liepen ze ook groen via de fd-tak.

```python
def test_process_mode_green_passes_without_gate_slot_py(gate):
    result = process(gate)
    assert result.returncode == 0, result.stderr
    assert gate.marks() == [str(gate.project.resolve())]
    assert (gate.slot_dir / "slot-1").exists()

def test_mingw_uname_selects_the_process_mode(gate):
    gate.add_stub("python3", NO_GATE_SLOT_PY)
    gate.add_stub("uname", "#!/bin/sh\necho MINGW64_NT-10.0-26100\n")
    result = gate.run("git commit -m x", gate.project)          # zonder de env-variabele
    assert result.returncode == 0, result.stderr
    assert gate.marks() == [str(gate.project.resolve())]

def test_process_mode_red_blocks(gate):
    result = process(gate, UV_EXIT="1")
    assert result.returncode == 2
    assert "tests failed. Fix the failing tests before committing." in result.stderr

def test_process_mode_suite_exit_75_is_a_failure_not_busy(gate):
    result = process(gate, UV_EXIT="75")
    assert result.returncode == 2
    assert "tests failed" in result.stderr
    assert "busy" not in result.stderr and "gate slot" not in result.stderr

def test_process_mode_busy_blocks(gate):
    fds = hold_slots(gate, "slot-1", "slot-2")
    try:
        result = process(gate)
    finally:
        release(fds)
    assert result.returncode == 2
    assert "COULD NOT CHECK — all gate slots busy after 1 s (slots: 2)" in result.stderr
    assert not gate.mark.exists()

def test_process_mode_lock_dir_cannot_be_made(gate):
    blocker = gate.tmp / "geen-map"; blocker.write_text("x\n")
    result = process(gate, LIGHTHOUSE_GATE_SLOT_DIR=str(blocker))
    assert result.returncode == 2
    assert "COULD NOT CHECK — cannot take a gate slot" in result.stderr
    assert f"lock dir {blocker} cannot be created" in result.stderr
    assert not gate.mark.exists()

def test_process_mode_error_report_blocks(gate):
    (gate.slot_dir / "slot-1").mkdir(parents=True)
    result = process(gate)
    assert result.returncode == 2
    assert "cannot take a gate slot: " in result.stderr
    assert "slot-1" in result.stderr          # de oorzaak uit het rapport
    assert not gate.mark.exists()

def test_process_mode_without_report_blocks(gate):
    gate.add_stub("python3", '#!/bin/sh\ncase "$1" in *deadline.py) exit 0 ;; esac\n'
                  f'exec "{REAL_PYTHON3}" "$@"\n')
    result = process(gate)
    assert result.returncode == 2
    assert "cannot take a gate slot: deadline.py gave no slot report" in result.stderr

def test_process_mode_report_survives_a_wiped_gate_tmp(gate):
    gate.configure("[test_gate]\ncommand = 'rm -rf \"$LIGHTHOUSE_GATE_TMP\" && "
                   "mkdir \"$LIGHTHOUSE_GATE_TMP\" && uv run pytest -q'\nslot_wait_seconds = 1\n")
    result = process(gate)
    assert result.returncode == 0, result.stderr
    assert gate.marks() == [str(gate.project.resolve())]

def test_process_mode_two_roots_with_one_slot(gate):
    gate.configure(TEST_GATE + "slots = 1\n")
    second = gate.tmp / "X2"; _worktree_on_default(gate.project, second)
    result = process(gate, f"git commit -m a && cd {second} && git commit -m b")
    assert result.returncode == 0, result.stderr
    assert gate.marks() == [str(gate.project.resolve()), str(second.resolve())]
```

`test_process_mode_slot_stays_held_until_deadline_py_ends`: dezelfde opzet als
`test_slot_stays_held_until_deadline_py_ends` (`:1117`), met
`LIGHTHOUSE_GATE_SLOT_MODE="process"` in de `env` en de `NO_GATE_SLOT_PY`-stub. Dezelfde asserts: na `SIGKILL` op de hook is
`slot-1` niet vrij, en binnen `8 + 10 + 2` s wel. Trek de gedeelde opzet niet uit de bestaande
test: die blijft ongewijzigd.

- [ ] **Step 2: Draai de tests en zie ze falen**

Run: `uv run pytest tests/pack/test_commit_test_gate.py -q -k "process_mode or mingw"`
Expected: FAIL (de hook kent de procestak niet; met `NO_GATE_SLOT_PY` blokkeert hij op
`gate_slot.py gave exit 2`). Uitzondering: `test_process_mode_lock_dir_cannot_be_made` faalt al
vóór `gate_slot.py` (op `mkdir`) en kan nu al slagen.

- [ ] **Step 3: Implementeer in `pack/core/hooks/commit-test-gate.sh`**

- Na de bepaling van `SLOT_DIR`: `SLOT_MODE=fd`; `process` als
  `[ "${LIGHTHOUSE_GATE_SLOT_MODE:-}" = process ]` of `case "$(uname -s 2>/dev/null)" in
  MINGW*|MSYS*|CYGWIN*)`. Alleen de waarde `process` telt.
- De `busy`-melding (nu `:418-432`) wordt een functie `slots_busy()` met dezelfde tekst; beide
  takken roepen hem aan.
- `fd`: het bestaande blok van `mkdir -p -m 700` t/m het sluiten van de vrije fd's, ongewijzigd
  binnen `if [ "$SLOT_MODE" = fd ]; then … fi`.
- `process`: `mkdir -p "$SLOT_DIR"` met dezelfde `slot_failed "lock dir $SLOT_DIR cannot be
  created ($MKDIR_OUT)"`; verder niets vóór de lus.
- Het geheugenblok blijft waar het staat. In de procestak loopt het dus zonder slot; dat is
  goed (de geheugendrempels zijn Linux-only, niet-doel).
- In de lus: `SLOT_OPTS=()`; in de procestak
  `SLOT_REPORT="$BT/run-$RUN.slot"` en
  `SLOT_OPTS=(--slot-dir "$SLOT_DIR" --slots "$SLOTS" --slot-wait "$SLOT_WAIT" --slot-report "$SLOT_REPORT")`,
  elk als los argument (MSYS zet alleen argumenten om naar een Windows-pad). De aanroep wordt
  `python3 "$LIB/deadline.py" "$DEADLINE" ${SLOT_OPTS[@]+"${SLOT_OPTS[@]}"} -- …` (bash 3.2 en
  `set -u`-veilig voor een lege array).
- Direct na `STATUS=$?`, vóór de `memory.events`-delta's, in de procestak:
  `SLOT_LINE=$(cat "$SLOT_REPORT" 2>/dev/null)`, dan `case`: `busy` → `slots_busy`;
  `error:\ *` → `slot_failed "${SLOT_LINE#error: }"`; een getal in `1..$SLOTS` → door; al het
  andere → `slot_failed "deadline.py gave no slot report"`.
- Header "Why slots": één alinea over de procestak (wat, wanneer, slot per root, de lockmap
  zonder `-m 700`, rapport naast `GATE_TMP`).

- [ ] **Step 4: Draai de tests en zie ze slagen**

Run: `uv run pytest tests/pack/test_commit_test_gate.py tests/pack/test_deadline.py -q`
Expected: alles PASS, ook alle bestaande slot-tests (fd-tak).

- [ ] **Step 5: Mutatiecheck**

Zet `SLOT_REPORT` tijdelijk op `"$GATE_TMP/slot"`: `-k wiped_gate_tmp` moet FAIL geven. Laat de
`case` tijdelijk op de exitcode 75 beslissen in plaats van het rapport: `-k exit_75` moet FAIL
geven. Beide terugzetten.

- [ ] **Step 6: Dogfood en commit**

`shellcheck` staat niet op deze host en geen CI-workflow draait het: geen shellcheck-stap. De
bash-3.2-regels bewaakt `test_hook_has_no_bash4_fd_variables`.

```bash
cd /home/corneel/repositories/lighthouse/.claude/worktrees/issue-90-windows-gate-slot
go run ./cmd/lighthouse init --upgrade --yes
go run ./cmd/lighthouse init --upgrade </dev/null   # Expected: No changes.
git add pack/core/hooks/commit-test-gate.sh tests/pack/test_commit_test_gate.py .claude/hooks/lighthouse/commit-test-gate.sh .lighthouse/manifest.toml
git commit -m "feat(test-gate): procestak voor het gate-slot op native Windows (#90)"
git status --porcelain                              # Expected: leeg
```

---

### Task 3: docs, changelog en het bewijs op echt Windows

**Files:**
- Modify: `docs/test-gate.md` (na de alinea **Slots.**, `:19-23`)
- Create: `changelog.d/issue-90-windows-gate-slot.md`

- [ ] **Step 1: Schrijf de docs en het fragment**

`docs/test-gate.md`, een alinea **Windows.** (Nederlands): in Git Bash op Windows neemt
`deadline.py` het slot per root en houdt het vast zolang de suite draait, omdat een slot daar bij
het proces hoort; bij een commit over meerdere roots gaat het slot tussen twee roots even vrij;
geheugendrempels en `gate_slice` werken daar niet (`/proc`, systemd).

`changelog.d/issue-90-windows-gate-slot.md`:

```markdown
---
type: fixed
---
The test gate now runs on native Windows (Git Bash): `deadline.py` takes and holds the gate slot itself, so a commit with a `[test_gate] command` is tested instead of blocked with "cannot take a gate slot".
```

- [ ] **Step 2: Volledige suite en commit**

Run: `uv run pytest tests/pack -q`
Expected: PASS.

```bash
cd /home/corneel/repositories/lighthouse/.claude/worktrees/issue-90-windows-gate-slot
git add docs/test-gate.md changelog.d/issue-90-windows-gate-slot.md
git commit -m "docs(test-gate): de procestak op Windows en het changelog-fragment (#90)"
```

- [ ] **Step 3: Meet op echt Windows**

De workflow draait niet vanzelf op een PR die alleen `pack/` raakt.

```bash
cd /home/corneel/repositories/lighthouse/.claude/worktrees/issue-90-windows-gate-slot
git push -u origin issue-90-windows-gate-slot
SINCE=$(date -u +%Y-%m-%dT%H:%M:%SZ)
gh workflow run windows-measure.yml --ref issue-90-windows-gate-slot
# `gh workflow run` keert terug voordat de run bestaat: poll tot er een run na $SINCE is.
gh run list --workflow windows-measure.yml --branch issue-90-windows-gate-slot -L 1 \
  --json databaseId,createdAt --jq ".[] | select(.createdAt >= \"$SINCE\") | .databaseId"
gh run watch <run-id>
gh run view <run-id> --log | grep -n 'test_gate\|--- PASS: TestRedTestBlocksCommit\|--- PASS: TestRedCommitComesBackAfter\|--- PASS: TestDefaultCommandWithoutPytest'
```

Expected: in de job summary stap 11 (`git --version` als `[test_gate] command`) `pass`; in de log
`--- PASS:` voor `TestRedTestBlocksCommit`, `TestRedCommitComesBackAfter` en
`TestDefaultCommandWithoutPytest` (de summary toont alleen FAIL-regels, maximaal 8).
Bewaar de runlink voor de Evidence-tabel van de PR. Faalt een van deze: stop,
`superpowers:systematic-debugging` op de log, geen PR met een rode meting.

---

## Afronding

`superpowers:finishing-a-development-branch`, met `/lighthouse-write-pr` voor de body:
`Refs #90 — <AC 2 en 3 blijven open>` en een Evidence-tabel met de runlink uit Task 3 Step 3.
