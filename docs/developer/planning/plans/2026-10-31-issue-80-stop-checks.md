# Stop-checks per domein — implementatieplan (#80, scope 4)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `go-check.sh` (`pack/go`) en `ty-check.sh` (`pack/python`) slaan bij Stop een checkout over waarin de beurt geen pad in hun domein raakte, en melden dat met één regel.

**Architecture:** `turn_roots.py` (`pack/core/lib`) krijgt de vlag `--match PATTERN` (herhaalbaar). Daarmee geeft het per root niet alleen de root, maar ook of de beurt daar een pad raakte dat op een patroon past. De paden van de beurt komen uit drie bronnen: de `file_path` van Edit/Write-calls, `git status` (gewijzigd sinds de prompt), en wat `HEAD` sinds de prompt verschoof (reflog). Elke Stop-hook geeft zijn eigen patronen mee en slaat een root met `untouched` over.

**Tech Stack:** bash 3.2-compatibel (hooks), Python 3.9-compatibel stdlib (`.lighthouse/lib`), pytest.

**Spec:** [`docs/developer/planning/specs/2026-10-27-gate-na-commit-design.md`](../specs/2026-10-27-gate-na-commit-design.md), scope 4 · Issue #80. Deel 1 (scope 1, 2, 3, 6, 7): [`2026-10-27-issue-80-gate-na-commit.md`](2026-10-27-issue-80-gate-na-commit.md), gemerged in PR #122.

**Buiten dit plan:** `pack/node` (scope 5) krijgt een eigen plan. #80 blijft open: de PR van dit plan zegt `Refs #80`, niet `Closes`.

## Besluiten van dit plan

De spec laat open "hoe de Stop-checks weten welke paden de beurt raakte" (*Aan de bouwer*). Dit plan kiest:

1. **De beurt begint bij de prompt**: de `timestamp` van de prompt-entry die `turn_roots.turn_entries` al vindt. `since` = die tijd in hele epoch-seconden, naar beneden afgerond (zoals `bash_writes._commits_since`).
2. **Paden van de beurt, per root** (relatief aan de root):
   - het pad van elke Edit, Write, MultiEdit en NotebookEdit in de beurt (`PATH_KEYS`) dat onder de root valt;
   - uit `git status --porcelain -z -uall`: een gewijzigd of nieuw pad met `int(st_mtime) >= since`, en elk verwijderd pad (zonder mtime; telt altijd);
   - uit de reflog van `HEAD`: `git diff --name-only --no-renames -z <basis> HEAD`. De basis is de nieuwste reflog-entry met epoch `< since`; is die er niet, dan de oudste entry. Zo telt elke commit, amend, rebase, pull of checkout in de beurt mee, ook als er daarna niets meer dirty is.
3. **Bij twijfel checken zoals vóór scope 4.** Heeft de prompt geen leesbare `timestamp`, of faalt een git-call voor een root (`bash_writes.GitError`), dan telt die root als `touched`. Een `unborn` `HEAD` slaat alleen de reflog-bron over.
4. **Te weinig is acceptabel** (besluit 3 van de spec, en de keuze van de gebruiker): een dirty pad uit een eerdere beurt telt niet; een Bash-write buiten git (genegeerd bestand) telt niet; de eerste commit van een repo die in de beurt ontstond, telt niet via de reflog.
5. **Een beurt zonder writes** viel tot nu terug op de root van de payload-`cwd` en werd gelint. Die root krijgt nu ook een oordeel, meestal `untouched`: een beurt met alleen gesprek of Read lint niets en meldt dat.
6. **Domeinen** (patronen voor `fnmatch.fnmatchcase` op het relatieve pad; `*` matcht ook `/`):
   - Go: `*.go`, `go.mod`, `go.sum`, `go.work`, `go.work.sum`, `.golangci.yml`, `.golangci.yaml`, `.golangci.toml`, `.golangci.json`, `.golangci-version`.
   - Python: per map `D` in `python.source_dirs` `D/*.py` en `D/*.pyi` (`D` zonder `./` vooraan en `/` achteraan; `.` geeft `*.py` en `*.pyi`), plus `pyproject.toml`, `ty.toml`, `uv.lock`. Een `.py` buiten `source_dirs` checkt ty niet, dus die telt niet.

## Global Constraints

- Hooks blijven bash-3.2-compatibel (geen `mapfile`, geen `declare -A`); `.lighthouse/lib` gebruikt alleen stdlib en begint met `from __future__ import annotations`.
- Meldingen, letterlijk: `go-check: not checked — the turn touched no Go path in <root>.` en `ty-check: not checked — the turn touched no Python path in <root>.` — als `systemMessage`, exit 0, en samen met de meldingen van andere roots zoals de hooks dat nu al doen.
- De skip komt in `check_root` vóór elke andere check van die root: een overgeslagen root heeft geen `go`, `golangci-lint`, `uvx`, `go.mod`, `.venv` of bestaande `source_dirs` nodig.
- Wat blijft zoals het is: de `stop_hook_active`-guard, het usage-alarm (#74), `COULD NOT CHECK` als `turn_roots.py` faalt, `python.source_dirs = []` zet `ty-check` stil uit.
- `turn_roots.py` zonder `--match` geeft exact de uitvoer van nu (één root per regel).
- Elke wijziging onder `pack/` draait `go run ./cmd/lighthouse init --upgrade` en commit de geïnstalleerde kopie in dezelfde commit. `pack/python` is in deze repo niet geïnstalleerd: test `ty-check.sh` via `tests/pack/python/`, niet via een geïnstalleerde kopie.
- Geen nieuw pakketbestand: `pack/SOURCES.toml`, `requires.toml` en `internal/packs/core_test.go` veranderen niet.
- De hooks van de lopende sessie komen uit de hoofdcheckout, niet uit deze worktree: test de nieuwe hooks via de testsuite.
- Commits: Conventional Commits, Nederlands, één logische wijziging per commit.

## Review Focus

1. **Werk dat in dezelfde beurt gecommit is** (`git commit` via Bash, daarna niets meer dirty): de check draait. → Taak 1, `test_match_a_commit_in_the_turn_is_touched`.
2. **Een beurt met alleen gesprek of Read**: geen lint, wel de meldregel. → Taak 2, `test_a_turn_without_go_paths_skips_and_says_so`; Taak 3, `test_ty_skips_a_turn_without_python_paths`.
3. **Een dirty Go-bestand uit een eerdere beurt**, deze beurt raakt alleen markdown: overgeslagen. → Taak 1, `test_match_a_dirty_path_from_an_earlier_turn_is_untouched`.
4. **Een repo zonder commits** (`unborn`): een Edit telt, geen crash en geen `COULD NOT CHECK`. → Taak 1, `test_match_an_edit_in_an_unborn_repo_is_touched`.
5. **Een `.py` buiten `source_dirs`** (bijvoorbeeld `tests/`): `ty-check` slaat over. → Taak 3, `test_ty_skips_python_outside_source_dirs`.

---

### Task 1: `turn_roots.py --match` — raakte de beurt een pad in dit domein?

**Files:**
- Modify: `pack/core/lib/turn_roots.py`
- Modify: `tests/pack/_testkit.py` (helper `stamp`)
- Test: `tests/pack/test_turn_roots.py`
- Geïnstalleerd: `.lighthouse/lib/lighthouse_lib/turn_roots.py` (via `init --upgrade`)

**Interfaces:**
- Consumes: `bash_writes.git`, `bash_writes.GitError`, `bash_writes.head_of`, `bash_writes.parse_porcelain`, `bash_writes.read_reflog`, `bash_writes.project_filter`.
- Produces:
  - `turn_entries(transcript: Path) -> tuple[list[dict], datetime | None]` — nu ook het moment van de prompt (`None` zonder leesbare `timestamp`). Alleen `turn_roots.py` roept hem aan.
  - `tool_paths(entries: list[dict]) -> set[str]` — de `os.path.realpath` van elk absoluut pad uit `PATH_KEYS`-calls.
  - `changed_since(root: str, since: int, *, deadline: float) -> set[str]` — status- en reflogbron van besluit 2, relatief aan `root`. Gooit `bash_writes.GitError`.
  - `classify(payload: dict, patterns: list[str]) -> list[tuple[str, bool]]` — dezelfde roots als `resolve(payload)`, gesorteerd, elk met `True` als een pad van de beurt in die root op een patroon past (besluit 2 en 3).
  - CLI: `turn_roots.py [--match PATTERN]... < payload`. Met `--match`: per root één regel `touched\t<root>` of `untouched\t<root>`. Exitcodes en de foutregel zoals nu.
  - `_testkit.stamp(at: float) -> str` — `at` (epoch) als `"YYYY-MM-DDTHH:MM:SS.mmmZ"` in UTC, het formaat van `PROMPT_TS`.

- [ ] **Step 1: Schrijf de falende tests** in `tests/pack/test_turn_roots.py`, met de bestaande fixtures `main` en `wt`. Een helper `_classify(main, cwd, entries, tmp_path, *patterns) -> list[str]` draait het script met `--match` per patroon en geeft de regels. Tests die tijd nodig hebben, nemen `t = time.time()` en zetten de prompt op `stamp(t)`; een oud bestand krijgt `os.utime(p, (t - 3600, t - 3600))`. `init_repo` commit al; `past_this_second()` vóór `t` houdt die commit buiten de beurt.

```python
def test_match_an_edit_in_the_domain_is_touched(tmp_path, main, wt):
    entries = [prompt_entry(main), tool_entry("Edit", {"file_path": str(wt / "internal" / "x" / "a.go")}, main)]
    assert _classify(main, main, entries, tmp_path, "*.go") == [f"touched\t{wt.resolve()}"]

def test_match_an_edit_outside_the_domain_is_untouched(tmp_path, main, wt):
    # zelfde, met wt / "README.md" -> [f"untouched\t{wt.resolve()}"]

def test_match_a_pattern_with_a_directory(tmp_path, main):
    # Edit main/"tests"/"test_a.py" met patroon "src/*.py" -> untouched;
    # Edit main/"src"/"pkg"/"a.py" -> touched

def test_match_a_bash_write_since_the_prompt_is_touched(tmp_path, main):
    # past_this_second(); t = time.time(); prompt op stamp(t);
    # schrijf main/"a.go" (nieuw, untracked, mtime nu); Bash-entry "ls" met cwd main
    # -> [f"touched\t{main.resolve()}"]

def test_match_a_dirty_path_from_an_earlier_turn_is_untouched(tmp_path, main):
    # main/"a.go" geschreven, utime t - 3600; prompt op stamp(t); Edit main/"README.md"
    # -> [f"untouched\t{main.resolve()}"]

def test_match_a_deleted_path_counts(tmp_path, main):
    # commit main/"a.go"; past_this_second(); t; verwijder main/"a.go" (niet gecommit);
    # prompt op stamp(t); Bash "ls" -> touched

def test_match_a_commit_in_the_turn_is_touched(tmp_path, main):
    # past_this_second(); t; prompt op stamp(t);
    # schrijf en commit main/"a.go" (status daarna schoon); Bash "git commit -qm x"
    # -> touched

def test_match_a_commit_before_the_turn_is_untouched(tmp_path, main):
    # commit main/"a.go"; past_this_second(); t; prompt op stamp(t); Bash "ls" -> untouched

def test_match_a_turn_without_writes_judges_the_cwd(tmp_path, main):
    # alleen prompt_entry(main, stamp(t)), schone repo -> [f"untouched\t{main.resolve()}"]

def test_match_a_prompt_without_timestamp_is_touched(tmp_path, main):
    # prompt zonder "timestamp"-sleutel; Bash "ls" in main; schone repo -> touched

def test_match_an_edit_in_an_unborn_repo_is_touched(tmp_path):
    # repo met alleen `git init` (geen commit) als CLAUDE_PROJECT_DIR; Edit repo/"a.go"
    # -> returncode 0, [f"touched\t{repo.resolve()}"]

def test_match_judges_each_root(tmp_path, main, wt):
    # Edit wt/"a.go", Bash "ls" met cwd main, prompt op stamp(t), main schoon
    # -> sorted([f"touched\t{wt.resolve()}", f"untouched\t{main.resolve()}"])
```

- [ ] **Step 2: Draai de tests en zie ze falen**

Run: `uv run pytest tests/pack/test_turn_roots.py -q`
Expected: de nieuwe tests FAIL (onbekende vlag `--match`, of geen `stamp` in `_testkit`); de bestaande PASS.

- [ ] **Step 3: Implementeer** in `pack/core/lib/turn_roots.py` de functies uit *Interfaces* en `stamp` in `_testkit`. `resolve` en `classify` delen de bepaling van de roots (één interne helper, die ook de entries en het startmoment teruggeeft); `resolve` houdt zijn signatuur. `main` leest `--match` met `argparse` (`action="append"`). Een pad uit `tool_paths` hoort bij een root als `os.path.relpath(pad, root)` niet met `..` begint. `changed_since` gebruikt één `deadline` voor alle git-calls; de reflogbasis volgt besluit 2 (`read_reflog` geeft de nieuwste entry eerst; lege reflog of `unborn`: geen reflogbron). Werk de docstring van de module bij: de derde bron, `--match` en besluit 3.

- [ ] **Step 4: Draai de tests en zie ze slagen**

Run: `uv run pytest tests/pack/test_turn_roots.py tests/pack/go/test_go_check.py tests/pack/python/test_ty_check.py -q`
Expected: PASS (de hooks roepen `--match` nog niet aan).

- [ ] **Step 5: Installeer en commit**

```bash
go run ./cmd/lighthouse init --upgrade
git add pack/core/lib/turn_roots.py .lighthouse/lib/lighthouse_lib/turn_roots.py tests/pack/_testkit.py tests/pack/test_turn_roots.py
git commit -m "feat(core): turn_roots --match zegt per root of de beurt een pad in een domein raakte"
```

---

### Task 2: `go-check.sh` slaat een checkout zonder Go-pad over

**Files:**
- Modify: `pack/go/hooks/go-check.sh`
- Modify: `pack/go/skills/go-writing/claude/SKILL.md`, `pack/go/skills/go-writing/agents/SKILL.md` (de tabelregel `once per turn` en de alinea eronder)
- Modify: `pack/go/instructions/AGENTS.md` (regel 4: "`go-check` at Stop (… ; skips a turn without a Go path; blocks on a finding, not during a usage alarm)")
- Test: `tests/pack/go/test_go_check.py`
- Geïnstalleerd: `.claude/hooks/lighthouse/go-check.sh`, de skill onder `.claude/skills/`, het managed block in `AGENTS.md` (via `init --upgrade`)

**Interfaces:**
- Consumes: `turn_roots.py --match` uit taak 1, met de Go-patronen van besluit 6.
- Produces: niets voor latere taken; taak 3 volgt hetzelfde patroon.

- [ ] **Step 1: Pas de bestaande tests aan en schrijf de falende tests.** De standaardpayload van `_run` wordt een beurt met `tool_entry("Edit", {"file_path": str(root / "a.go")}, root)`; werk het commentaar bij. `test_every_checkout_of_the_turn_is_linted` schrijft vóór de run `root / "b.go"` en `wt / "a.go"` (dirty, mtime na `PROMPT_TS`), zodat beide roots `touched` zijn. Nieuw:

```python
def test_a_turn_without_go_paths_skips_and_says_so(tmp_path):
    # _worktree-achtige schone repo (go.mod gecommit); prompt op stamp(time.time()) na past_this_second();
    # Edit root/"README.md"
    # -> _message(result) == f"go-check: not checked — the turn touched no Go path in {os.path.realpath(root)}."
    #    en _runs(env) == []

@pytest.mark.parametrize("rel", ["internal/x/a.go", "go.mod", "go.sum", ".golangci.yml", ".golangci-version"])
def test_a_go_path_lints(tmp_path, rel):
    # schone repo; Edit root/rel -> rc 0, _runs(env) == [f"run ./... @ {os.path.realpath(root)}"]

def test_a_skipped_root_needs_no_go_tooling(tmp_path):
    # PATH is alleen een tool_bin met TOOLS, zonder stubs en zonder de echte PATH
    # (zoals test_missing_tool_could_not_check); schone repo; Edit root/"README.md"
    # -> rc 0, melding "not checked — the turn touched no Go path", geen "COULD NOT CHECK"

def test_an_untouched_root_is_named_next_to_a_finding(tmp_path):
    # _worktree; STUB_FAIL_IN=wt; Edit wt/"a.go" plus Bash "ls" met cwd root; root schoon
    # -> rc 2, LINT_FINDING in stderr,
    #    f"go-check: not checked — the turn touched no Go path in {os.path.realpath(root)}." in stderr,
    #    _runs(env) == [f"run ./... @ {os.path.realpath(wt)}"]
```

- [ ] **Step 2: Draai de tests en zie ze falen**

Run: `uv run pytest tests/pack/go/test_go_check.py -q`
Expected: de vier nieuwe tests FAIL (de hook lint elke root).

- [ ] **Step 3: Implementeer** in `go-check.sh`: roep `turn_roots.py` aan met `--match` per Go-patroon (één bash-array `GO_PATTERNS`), lees per regel `verdict<TAB>root`, en geef het oordeel als tweede argument aan `check_root`. De eerste regel van `check_root` na de `cd`: bij `untouched` `report` met de melding uit *Global Constraints*. Werk het kopcommentaar bij: de alinea "Runs on every Stop, not only after a Go edit" wordt: draait bij elke Stop, lint alleen een checkout waarin de beurt een Go-pad raakte (een edit, een Bash-write of een commit in de beurt; `turn_roots.py --match`), en meldt de rest. Werk de skilltekst en `AGENTS.md` bij zoals bij *Files*.

- [ ] **Step 4: Draai de tests en zie ze slagen**

Run: `uv run pytest tests/pack/go/ tests/test_instruction_files.py tests/test_dogfood.py -q`
Expected: PASS

- [ ] **Step 5: Installeer, controleer en commit**

```bash
go run ./cmd/lighthouse init --upgrade
go run ./cmd/lighthouse init --upgrade   # tweede keer: "No changes."
git add pack/go tests/pack/go/test_go_check.py .claude/hooks/lighthouse/go-check.sh .claude/skills AGENTS.md
git commit -m "feat(go): go-check slaat een checkout zonder Go-pad over en meldt dat"
```

---

### Task 3: `ty-check.sh` slaat een checkout zonder Python-pad over

**Files:**
- Modify: `pack/python/hooks/ty-check.sh`
- Modify: `pack/python/skills/python-writing/claude/SKILL.md`, `pack/python/skills/python-writing/agents/SKILL.md` (de tabelregel `once per turn` en de alinea eronder)
- Modify: `pack/python/instructions/AGENTS.md` (regel 4: "`ty-check` when the session stops, if the turn touched a Python path")
- Test: `tests/pack/python/test_ty_check.py`

**Interfaces:**
- Consumes: `turn_roots.py --match` uit taak 1, met de Python-patronen van besluit 6.

- [ ] **Step 1: Pas de bestaande tests aan en schrijf de falende tests.** De standaardpayload van `_run` wordt een beurt met `tool_entry("Edit", {"file_path": str(root / "src" / "a.py")}, root)`; werk het commentaar bij. Een test die `source_dirs` op een andere map zet (`test_ty_reads_source_dirs`, `test_ty_skips_a_dir_outside_source_dirs`), geeft een Edit in die map mee. Nieuw:

```python
def test_ty_skips_a_turn_without_python_paths(tmp_path):
    # _fake_repo, alles gecommit; past_this_second(); prompt op stamp(time.time()); Edit root/"README.md"
    # -> _message(result) == f"ty-check: not checked — the turn touched no Python path in {os.path.realpath(root)}."

def test_ty_skips_python_outside_source_dirs(tmp_path):
    # source_dirs standaard ["src"], alles gecommit; Edit root/"tests"/"test_a.py" -> zelfde melding

@pytest.mark.parametrize("rel", ["src/pkg/a.py", "src/a.pyi", "pyproject.toml", "uv.lock"])
def test_ty_checks_a_python_path(tmp_path, rel):
    # src/bad.py met een typefout gecommit; Edit root/rel -> rc 2

def test_ty_dot_source_dir_matches_the_root(tmp_path):
    # _fake_repo(tmp_path, python='source_dirs = ["."]'), bad.py in de root gecommit; Edit root/"bad.py" -> rc 2

def test_ty_skipped_root_needs_no_venv(tmp_path):
    # _fake_repo(tmp_path, venv=False), gecommit; Edit root/"README.md" -> de skipmelding, geen "COULD NOT CHECK"
```

- [ ] **Step 2: Draai de tests en zie ze falen**

Run: `uv run pytest tests/pack/python/test_ty_check.py -q`
Expected: de nieuwe skiptests FAIL.

- [ ] **Step 3: Implementeer** in `ty-check.sh`: lees `python.source_dirs` vóór `turn_roots.py` (de stille exit bij `[]` blijft vóór het alarm), bouw de patronen van besluit 6 en roep `turn_roots.py` aan met `--match` per patroon. Daarna zoals taak 2: `verdict<TAB>root` lezen, `untouched` meteen na de `cd` in `check_root` melden. Werk het kopcommentaar, de skilltekst en `AGENTS.md` bij.

- [ ] **Step 4: Draai de tests en zie ze slagen**

Run: `uv run pytest tests/pack/python/ tests/test_instruction_files.py -q`
Expected: PASS

- [ ] **Step 5: Controleer en commit**

```bash
go run ./cmd/lighthouse init --upgrade   # pack/python is hier niet geïnstalleerd: "No changes."
git add pack/python tests/pack/python/test_ty_check.py
git commit -m "feat(python): ty-check slaat een checkout zonder Python-pad over en meldt dat"
```

---

### Afronding

- [ ] `uv run pytest -q` en `go test -race ./...`: groen.
- [ ] `go run ./cmd/lighthouse init --upgrade`: `No changes.`
- [ ] `/lighthouse-verify-pr`: PR-body met `Refs #80 — scope 4 van de spec; scope 5 (pack/node) volgt`.
