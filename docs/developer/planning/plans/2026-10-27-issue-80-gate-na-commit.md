# Test-gate na de commit — implementatieplan (deel 1)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** De test-gate draait na de commit, alleen op de bestanden van die commit, via selectors per taalpakket; deze repo stapt erop over.

**Architecture:** `pre-commit-tests.sh` wordt `commit-tests.sh pre|post`. `pre` blokkeert `--no-verify`, test een commit naar de default branch vooraf zoals nu, en legt voor de rest per root de `HEAD` vast in een marker (`commit_base.py`). `post` leest die marker, en draait `[test_gate] command` voor elke root waarvan `HEAD` verschoof, met de lijst van `basis..HEAD`. De selectors `gate_go.py` (`pack/go`) en `gate_python.py` (`pack/python`) kiezen uit die lijst de tests.

**Tech Stack:** bash 3.2-compatibel (hooks), Python 3.9-compatibel stdlib (lib en selectors: de hooks draaien onder `python3` van de host), Go (`go list -json`), pytest.

**Spec:** [`docs/developer/planning/specs/2026-10-27-gate-na-commit-design.md`](../specs/2026-10-27-gate-na-commit-design.md) · Issue #80.

**Buiten dit plan** (eigen plannen, daarna): de Stop-checks per domein (scope 4 van de spec) en `pack/node` (scope 5).

## Global Constraints

- Hooks blijven bash-3.2-compatibel (geen `exec {var}>`, geen `mapfile`); Python in `.lighthouse/lib` en in pakketscripts begint met `from __future__ import annotations` en gebruikt alleen stdlib.
- Niet fail-open: een stand die niet kan testen, eindigt met exit 2 en `COULD NOT CHECK`.
- Slots, deadline (`[test_gate] deadline_seconds`, standaard 270 s), geheugenrem (`gate_slice`) en `unchecked_exit_codes` werken in `pre` (default branch) en in `post` zoals nu.
- Hooktimeout in `hooks.json`: 300 s voor `commit-tests.sh pre` en voor `commit-tests.sh post`.
- Env-variabelen voor het commando: `LIGHTHOUSE_GATE_TMP` (zoals nu), `LIGHTHOUSE_GATE_BASE`, `LIGHTHOUSE_GATE_HEAD`, `LIGHTHOUSE_GATE_FILES`.
- De lege boom: `4b825dc642cb6eb9a060e54bf8d69288fbee4904`.
- De gate gebruikt geen `-shuffle=on`; CI houdt `go test -race -shuffle=on ./...`.
- Elke wijziging onder `pack/` draait `go run ./cmd/lighthouse init --upgrade` en commit de geïnstalleerde kopie; elk nieuw pakketbestand krijgt een regel in `pack/SOURCES.toml` (`new = true`).
- Commits: Conventional Commits, Nederlands, één logische wijziging per commit.

**Afwijking van de spec (plankeuze):** `LIGHTHOUSE_GATE_FILES` wijst naar `<gate-tmp-root>/run-<n>.files`, naast `LIGHTHOUSE_GATE_TMP` en niet erin. `pytest --basetemp="$LIGHTHOUSE_GATE_TMP"` leegt die map bij de start, en een selector die later in het commando komt, moet de lijst nog kunnen lezen.

## Review Focus

1. **`git commit -m x && git push` op een featurebranch.** `post` test precies die commit, ook als de push faalt (PostToolUseFailure). → Taak 3, `test_post_tests_commit_when_call_failed`.
2. **Commit zonder resultaat** (`nothing to commit`, een geweigerde commit): `HEAD` verschuift niet, `post` zwijgt en geeft exit 0. → Taak 3, `test_post_silent_when_head_did_not_move`.
3. **`post` zonder marker** (hook halverwege een sessie geïnstalleerd, marker na een uur opgeruimd): exit 0, geen test, geen fout. → Taak 1, `test_moved_without_marker_is_empty`.
4. **Bestandsnamen met spaties** in de commit: de lijst heeft één pad per regel, de selectors splitsen alleen op `\n`. → Taak 4 en 5, `test_*_path_with_space`.
5. **Een commit in een andere repo** (`cd /elders && git commit`): overgeslagen met een melding, zoals nu. → Taak 2, de bestaande test `test_other_project_is_skipped` blijft groen onder `pre`, en `post` krijgt geen marker voor die root.

---

### Task 1: `commit_base.py` — de basis vastleggen en terugvinden

**Files:**
- Create: `pack/core/lib/commit_base.py`
- Test: `tests/pack/test_commit_base.py`
- Modify: `pack/SOURCES.toml`, `pack/core/requires.toml` (regels `git`, `python3` krijgen `"lighthouse_lib.commit_base"`)

**Interfaces:**
- Produces:
  - `EMPTY_TREE: str = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"`
  - `class Moved(NamedTuple): root: str; base: str; head: str`
  - `state_dir(session_id: str) -> Path` — `${XDG_RUNTIME_DIR:-/tmp}/lighthouse-commit-tests/<session_id>/`, mode `0700`; `ValueError` bij `/` of `..` (zoals `bash_writes.state_dir`).
  - `record(payload: dict, roots: list[str]) -> None` — schrijft `<state>/<bash_writes.marker_key(payload)>.json` = `{"heads": {root: sha-of-"unborn"}}` atomisch, en ruimt markers ouder dan een uur op.
  - `moved(payload: dict) -> list[Moved]` — leest en verwijdert de marker; geeft alleen roots waarvan `HEAD` nu anders is; `"unborn"` wordt `EMPTY_TREE`. Geen marker: `[]`.
  - `changed_files(root: str, base: str, head: str) -> list[str]` — `git diff --name-only --no-renames -z <base> <head>`, gesorteerd.
  - CLI (payload op stdin): `commit_base.py record ROOT...` (rc 0); `commit_base.py moved` (print `root\tbase\thead` per regel, rc 0); `commit_base.py files ROOT BASE HEAD OUT` (schrijft één pad per regel naar `OUT`). Elke git- of I/O-fout: rc 2 met de reden op stderr.
- Consumes: `bash_writes.marker_key`, `bash_writes.head_of`, `bash_writes._atomic_write_json`.

- [ ] **Step 1: Schrijf de falende tests** in `tests/pack/test_commit_base.py`, met echte git-repos in `tmp_path` (`init_repo`, `git` uit `tests.pack._testkit`) en `XDG_RUNTIME_DIR` op `tmp_path`:
  - `test_moved_after_commit`: `record` → commit → `moved` geeft `[Moved(root, oude_sha, nieuwe_sha)]`.
  - `test_moved_is_empty_without_new_commit`: `record` → `moved` → `[]`.
  - `test_moved_consumes_marker`: twee keer `moved` na één commit → tweede keer `[]`.
  - `test_moved_without_marker_is_empty`: alleen `moved` → `[]`.
  - `test_unborn_base_is_empty_tree`: `record` in een repo zonder commits → eerste commit → `base == EMPTY_TREE`.
  - `test_changed_files_rename_gives_both_sides`: `git mv a.txt b.txt` + commit → `["a.txt", "b.txt"]`.
  - `test_changed_files_path_with_space`: commit `dir/x y.py` → `["dir/x y.py"]`.
  - `test_cli_moved_prints_rows`: CLI `record` en `moved` → stdout `f"{root}\t{base}\t{head}\n"`.
  - `test_cli_files_writes_one_path_per_line`.
  - `test_cli_git_failure_is_rc_2`: `files` met een onbekende sha → rc 2, stderr niet leeg.

- [ ] **Step 2: Draai ze en zie ze falen.** Run: `uv run pytest tests/pack/test_commit_base.py -q` · Expected: FAIL, `ModuleNotFoundError: commit_base`.

- [ ] **Step 3: Implementeer `pack/core/lib/commit_base.py`** volgens de interfaces. Markers ouder dan 3600 s worden opgeruimd in `record`.

- [ ] **Step 4: Draai `go run ./cmd/lighthouse init --upgrade`** en dan de tests. Expected: PASS. `uv run pytest tests/test_sources.py -q`: PASS.

- [ ] **Step 5: Commit** — `feat(core): commit_base legt de HEAD vóór een commit vast (#80)`; `pack/core/lib/commit_base.py`, de geïnstalleerde kopie, `pack/SOURCES.toml`, `pack/core/requires.toml`, de test.

### Task 2: `commit-tests.sh pre` — de hernoeming en de standen

**Files:**
- Rename: `pack/core/hooks/pre-commit-tests.sh` → `pack/core/hooks/commit-tests.sh`
- Modify: `pack/core/hooks/hooks.json`, `pack/SOURCES.toml`, `pack/core/requires.toml` (`pre-commit-tests` → `commit-tests`), `pack/python/requires.toml:4`, `internal/packs/core_test.go:61`
- Rename: `tests/pack/test_pre_commit_tests.py` → `tests/pack/test_commit_tests.py`; `tests/pack/test_pre_commit_no_verify.py` → `tests/pack/test_commit_tests_no_verify.py`
- Modify: `tests/pack/test_hooks_minimal_config.py`, `tests/pack/test_git_invocations.py`, `tests/pack/test_main_commit_guard.py`, `tests/test_instruction_files.py`, `tests/pack/test_verify_pr_skill.py` (alleen de verwijzingen naar de hooknaam)

**Interfaces:**
- Consumes: `commit_base.py record` (Task 1).
- Produces: `commit-tests.sh pre` en `commit-tests.sh post` (post: Task 3). Een onbekende of ontbrekende stand: exit 2, `COULD NOT CHECK — unknown mode, expected pre or post`. `HOOK_NAME="commit-tests"`.

`hooks.json`: PreToolUse `Bash` → `commit-tests.sh pre` (timeout 300) op de plek van `pre-commit-tests.sh`; PostToolUse `Bash` en PostToolUseFailure `Bash` → `commit-tests.sh post` (timeout 300), elk vóór `bash-write-gates.sh post`.

Gedrag van `pre`, in deze volgorde: parsen en `--no-verify` blokkeren (ongewijzigd); roots bepalen en andere projecten overslaan (ongewijzigd). Daarna:
- roots met minstens één commit met `TO_MAIN = yes`: de huidige testlus, ongewijzigd, zonder `LIGHTHOUSE_GATE_FILES`;
- de overige projectroots: `commit_base.py record` met de payload op stdin. Geen tests;
- `tool_input.run_in_background == true` en minstens één overige projectroot: geen `record`, wel de melding `commit-tests: <root> — commit in a background call is not tested; commit in the foreground.` als `systemMessage` en `additionalContext`;
- `record` faalt: exit 2, `COULD NOT CHECK — could not record HEAD: <stderr>`.

De testlus (van `RUN=0` tot `emit_message_and_exit`) wordt een functie `run_gate_roots <roots> <event>`, zodat Task 3 hem hergebruikt. `emit_message_and_exit` krijgt het event (`PreToolUse` of `PostToolUse`) als argument.

- [ ] **Step 1: Pas de tests aan.** In `test_commit_tests.py` roept `run_hook` voortaan `commit-tests.sh pre`. Tests die nu een commit op een featurebranch laten testen, committen op `main` met `TO_MAIN` (hun gedrag blijft dat van vandaag). Nieuwe tests:
  - `test_pre_records_instead_of_testing_on_feature_branch`: commit op `issue-1-x` → de fake `uv` is niet gestart, en er staat een marker in `$XDG_RUNTIME_DIR/lighthouse-commit-tests/<session>/`.
  - `test_pre_tests_before_commit_to_main`: commit op `main` met `.claude/allow-main` → de fake `uv` draait, `UV_EXIT=1` geeft exit 2.
  - `test_pre_background_commit_is_reported`: payload met `run_in_background: true` op een featurebranch → exit 0, `systemMessage` bevat `not tested`, geen marker.
  - `test_unknown_mode_blocks`: zonder argument → exit 2, `COULD NOT CHECK`.
  - `test_hooks_json_registers_pre_and_post`: PreToolUse heeft `commit-tests.sh pre`; PostToolUse en PostToolUseFailure hebben `commit-tests.sh post`, beide met timeout 300.
- [ ] **Step 2: Draai ze en zie ze falen.** Run: `uv run pytest tests/pack/test_commit_tests.py tests/pack/test_commit_tests_no_verify.py -q` · Expected: FAIL (bestand bestaat niet).
- [ ] **Step 3: Implementeer** de hernoeming, de standen, `pre` en `run_gate_roots`. Werk de kopcommentaar bij: de gate draait na de commit; de asymmetrie met `allow-main` geldt nu alleen voor `TO_MAIN`.
- [ ] **Step 4: `go run ./cmd/lighthouse init --upgrade`.** Expected: `.claude/hooks/lighthouse/pre-commit-tests.sh` is weg, `.claude/hooks/lighthouse/commit-tests.sh` staat er, en `.claude/settings.json` registreert alleen de nieuwe naam. Ruimt `init` het oude bestand of de oude registratie niet op, voeg dat dan toe in `internal/packs` of `internal/settings`, met een Go-test die een installatie met `pre-commit-tests.sh` upgradet. Run: `go test ./internal/... && uv run pytest tests/pack -q -k "commit_tests or hooks_minimal or git_invocations or main_commit_guard"` · Expected: PASS.
- [ ] **Step 5: Commit** — `feat(core)!: pre-commit-tests wordt commit-tests met pre en post (#80)`.

### Task 3: `commit-tests.sh post` — testen wat de commit raakte

**Files:**
- Modify: `pack/core/hooks/commit-tests.sh`
- Test: `tests/pack/test_commit_tests.py`

**Interfaces:**
- Consumes: `commit_base.py moved` en `files` (Task 1); `run_gate_roots` (Task 2).
- Produces: het contract voor de selectors: in elke run zijn `LIGHTHOUSE_GATE_BASE`, `LIGHTHOUSE_GATE_HEAD` en `LIGHTHOUSE_GATE_FILES` (pad naar `$BT/run-<n>.files`) gezet.

Gedrag van `post`: `moved` geen rijen → exit 0 zonder uitvoer, vóór config, slot of rem. Wel rijen → config, slot en rem zoals nu, dan per rij `files` en de testlus met de drie variabelen. Rood: exit 2, de uitvoer op stderr, eerste regel `commit-tests: tests failed on the commit you just made. Fix them in a new commit.` Groen: `additionalContext` met `hookEventName: "PostToolUse"` (of `PostToolUseFailure`, uit de payload). `moved` of `files` faalt: exit 2, `COULD NOT CHECK`.

- [ ] **Step 1: Schrijf de falende tests** (de fake `uv` schrijft ook `$LIGHTHOUSE_GATE_BASE`, `$LIGHTHOUSE_GATE_HEAD` en de inhoud van `$LIGHTHOUSE_GATE_FILES` naar `$UV_MARK.env`):
  - `test_post_runs_command_with_commit_files`: `pre` → echte commit van `a.py` en `b/c.sh` → `post` → de fake `uv` zag `BASE` = de oude sha, `HEAD` = de nieuwe, en de bestanden `a.py\nb/c.sh\n`.
  - `test_post_red_is_exit_2_with_fix_hint`: `UV_EXIT=1` → exit 2, stderr begint met `commit-tests: tests failed on the commit you just made`.
  - `test_post_silent_when_head_did_not_move`: `pre` → geen commit → `post` → exit 0, lege stdout, de fake `uv` niet gestart.
  - `test_post_tests_commit_when_call_failed`: `pre` → commit → `post` met `hook_event_name: "PostToolUseFailure"` → de fake `uv` draait, `additionalContext` staat onder `PostToolUseFailure`.
  - `test_post_two_commits_in_one_call`: twee commits → `BASE` is de sha van vóór de eerste.
  - `test_post_unchecked_exit_code_blocks`: `unchecked_exit_codes = [5]`, `UV_EXIT=5` → exit 2, `COULD NOT CHECK`.
  - `test_post_deadline_still_applies`: de bestaande deadlinetest, nu via `post`.
- [ ] **Step 2: Zie ze falen.** Run: `uv run pytest tests/pack/test_commit_tests.py -q -k post` · Expected: FAIL.
- [ ] **Step 3: Implementeer `post`.**
- [ ] **Step 4: `init --upgrade` en de tests.** Run: `go run ./cmd/lighthouse init --upgrade && uv run pytest tests/pack/test_commit_tests.py -q` · Expected: PASS.
- [ ] **Step 5: Commit** — `feat(core): commit-tests post test de bestanden van de commit (#80)`.

### Task 4: `gate_go.py` — de Go-selector

**Files:**
- Create: `pack/go/scripts/gate_go.py` (installeert naar `.lighthouse/scripts/gate_go.py`)
- Test: `tests/pack/go/test_gate_go.py`
- Modify: `pack/SOURCES.toml`, `pack/go/requires.toml` (`go`, `python3` krijgen `"gate_go"`)

**Interfaces:**
- Produces:
  - `packages_for(files: list[str], listing: list[dict], root: Path) -> list[str]` — import paths, gesorteerd en uniek. Een pad telt voor een package als het in `Dir` ligt en voorkomt in `GoFiles`, `CgoFiles`, `TestGoFiles`, `XTestGoFiles`, `IgnoredGoFiles`, `EmbedFiles`, `TestEmbedFiles` of `XTestEmbedFiles`, of als het onder `<Dir>/testdata/` ligt. Een verwijderd `.go`-bestand telt voor het package van zijn map, als dat er nog is. `go.mod` en `go.sum` tellen niet.
  - `go_list(root: Path) -> list[dict]` — `go list -json ./...`, de stroom van JSON-objecten gedecodeerd met `json.JSONDecoder().raw_decode`.
  - CLI: `gate_go.py -- CMD...`. Zonder `LIGHTHOUSE_GATE_FILES`: draait `CMD ./...`. Met: geen packages → print `gate-go: skipped — the commit touches no Go package`, rc 0; anders `gate-go: <pkgs>` en draait `CMD <pkgs>`, rc van `CMD`. `go list` faalt → zijn stderr en rc. Geen `--`: rc 2 met gebruik.

- [ ] **Step 1: Schrijf de falende tests** (unit op `packages_for` met een vaste `listing`, plus één integratietest op een mini-module in `tmp_path` met `go` op `PATH`; skip als `go` ontbreekt):
  - `test_go_file_maps_to_its_package`, `test_test_file_maps_to_its_package`.
  - `test_embedded_file_maps_to_embedding_package`: `pack/core/hooks/x.sh` met `EmbedFiles: ["core/hooks/x.sh"]` en `Dir: <root>/pack` → `[".../pack"]`.
  - `test_testdata_maps_to_package`.
  - `test_go_mod_alone_selects_nothing`.
  - `test_importers_are_not_selected`: `a` importeert `b`, alleen `b/b.go` gewijzigd → `[".../b"]`.
  - `test_deleted_go_file_maps_to_its_directory`.
  - `test_gate_go_path_with_space`: `dir x/y.go` → het package van `dir x`.
  - `test_cli_without_files_env_runs_whole_module`: `CMD` = `echo` → stdout bevat `./...`.
  - `test_cli_nothing_selected_is_rc_0_with_reason`.
- [ ] **Step 2: Zie ze falen.** Run: `uv run pytest tests/pack/go/test_gate_go.py -q` · Expected: FAIL.
- [ ] **Step 3: Implementeer.**
- [ ] **Step 4: `init --upgrade` en de tests.** Expected: PASS, en `tests/test_sources.py` PASS.
- [ ] **Step 5: Commit** — `feat(go): gate_go kiest de packages van de commit (#80)`.

### Task 5: `gate_python.py` — de Python-selector

**Files:**
- Create: `pack/python/scripts/gate_python.py` (installeert naar `.lighthouse/scripts/gate_python.py`)
- Test: `tests/pack/python/test_gate_python.py`
- Modify: `pack/SOURCES.toml`, `pack/python/requires.toml` (`python3`, `uv` krijgen `"gate_python"`)

**Interfaces:**
- Produces:
  - `select_tests(files: list[str], root: Path, test_dirs: list[str]) -> list[str]` — paden relatief aan `root`, gesorteerd en uniek. Een bestaand gewijzigd `test_*.py` onder een testmap telt zelf. Elk ander pad `…/X.py`, `…/X.sh` of `…/X` geeft elk bestaand `test_<X met - als _>.py` onder de testmappen (`rglob`). Verwijderde testbestanden tellen niet.
  - CLI: `gate_python.py [--tests DIR]... -- CMD...`, standaard `--tests tests`. Zonder `LIGHTHOUSE_GATE_FILES`: draait `CMD` zonder bestanden (het hele domein). Met: niets gekozen → `gate-python: skipped — no test belongs to the commit`, rc 0; anders `gate-python: <n> test files` en draait `CMD <files>`, rc van `CMD`.

- [ ] **Step 1: Schrijf de falende tests:**
  - `test_changed_test_file_selects_itself`.
  - `test_hook_maps_to_test_with_underscores`: `pack/core/hooks/pre-commit-tests.sh` → `tests/pack/test_pre_commit_tests.py`.
  - `test_lib_module_maps_to_test`: `.lighthouse/lib/lighthouse_lib/deadline.py` → `tests/pack/test_deadline.py`.
  - `test_extensionless_file_maps_to_test`.
  - `test_conftest_selects_nothing`.
  - `test_markdown_selects_nothing`.
  - `test_deleted_test_file_selects_nothing`.
  - `test_gate_python_path_with_space`.
  - `test_cli_without_files_env_runs_command_bare`.
  - `test_cli_nothing_selected_is_rc_0_with_reason`.
- [ ] **Step 2: Zie ze falen.** Run: `uv run pytest tests/pack/python/test_gate_python.py -q` · Expected: FAIL.
- [ ] **Step 3: Implementeer.**
- [ ] **Step 4: `init --upgrade` en de tests.** Expected: PASS, `tests/test_sources.py` PASS.
- [ ] **Step 5: Commit** — `feat(python): gate_python kiest de tests bij de commit (#80)`.

### Task 6: Deze repo stapt over

**Files:**
- Modify: `lighthouse.toml`, `ci/domains.py`, `tests/test_domains.py`, `tests/test_ci_go_test_pin.py`, `tests/test_dogfood.py:155-165`

**Interfaces:**
- Consumes: `gate_go.py`, `gate_python.py` (Task 4, 5); het contract van Task 3.
- Produces: `[test_gate] command = "python3 .lighthouse/scripts/gate_go.py -- go test -race && python3 .lighthouse/scripts/gate_python.py -- uv run pytest -q && python3 ci/domains.py gate"`; `unchecked_exit_codes = [3]` blijft.

`ci/domains.py gate` wordt de webgate tot `pack/node` (deel 3): `CHECKS` houdt alleen `"web"`; `gate` neemt de paden uit `LIGHTHOUSE_GATE_FILES` als die gezet is, anders `changed_paths(root)` zoals nu; alleen `web` telt nog. `classify` en de CI-uitvoer (`web=`, `python=`, `go=`) blijven ongewijzigd.

- [ ] **Step 1: Pas de tests aan:**
  - `tests/test_domains.py`: `test_gate_reads_files_from_env` (een lijstbestand met alleen `internal/x.go` → web overgeslagen, rc 0, geen npm); `test_gate_runs_only_web` (`web/src/a.ts` → alleen de webchecks).
  - `tests/test_ci_go_test_pin.py`: leest het Go-deel uit `[test_gate] command` (de woorden na `gate_go.py --` tot `&&`) en eist dat elke vlag daarvan in de `go test`-stap van `ci.yml` staat.
  - `tests/test_dogfood.py`: `test_test_gate_uses_selectors` eist precies het commando hierboven.
- [ ] **Step 2: Zie ze falen.** Run: `uv run pytest tests/test_domains.py tests/test_ci_go_test_pin.py tests/test_dogfood.py -q` · Expected: FAIL.
- [ ] **Step 3: Pas `lighthouse.toml` en `ci/domains.py` aan.**
- [ ] **Step 4: Verifieer.** Run: `go run ./cmd/lighthouse init --upgrade` · Expected: `No changes.` Run: `uv run pytest -q && go test -race -shuffle=on ./...` · Expected: PASS (de volledige suite, één keer, met de hand). Meet daarna een echte commit van één `.go`-bestand in deze worktree: de gate in `post` duurt minder dan 10 s.
- [ ] **Step 5: Commit** — `chore(gate): deze repo test na de commit met de selectors (#80)`.

### Task 7: Instructies en skills

**Files:**
- Modify: `pack/core/instructions/AGENTS.md` (de core-regel over de test-gate), `CLAUDE.md` (lijst *Hooks*), `pack/core/skills/fix-pr/agents/SKILL.md`, `pack/core/skills/fix-pr/claude/SKILL.md`, `pack/core/skills/verify-pr/SKILL.md`, `pack/go/skills/go-writing/agents/SKILL.md`, `pack/go/skills/go-writing/claude/SKILL.md`, `pack/python/skills/python-writing/agents/SKILL.md` en zijn `claude/`-variant, en de geïnstalleerde kopieën.

De core-regel wordt: `` - The test gate: `commit-tests` runs `[test_gate] command` after every commit, on the files that commit touched; a commit to the default branch it tests before. A red run comes back to you: fix it in a new commit. Never use `--no-verify`. ``

`CLAUDE.md` *Hooks*: de regel van `pre-commit-tests.sh` wordt `` `commit-tests.sh` — PreToolUse, PostToolUse and PostToolUseFailure `Bash`. `pre` blocks `--no-verify` and tests a commit to `main` before it; `post` runs `[test_gate] command` on the files of the commit and reports red. ``

`go-writing` en `python-writing`: de KPI-rij voor de test-gate blijft het commando uit `config.py test_gate.command`; de alinea eronder zegt dat de gate na de commit draait en noemt de selector met een voorbeeldcommando (`python3 .lighthouse/scripts/gate_go.py -- go test -race`, `python3 .lighthouse/scripts/gate_python.py -- uv run pytest -q`).

- [ ] **Step 1: Draai** `uv run pytest tests/test_instruction_files.py tests/pack/test_verify_pr_skill.py tests/pack/test_fix_pr_skill.py -q` na de tekstwijziging, en `go run ./cmd/lighthouse init --upgrade`. Expected: PASS; het woordbudget per instructieblok (#48) blijft binnen de grens die `init` toont.
- [ ] **Step 2: Verifieer** dat `git grep -n "pre-commit-tests" -- ':!docs/developer/planning'` alleen nog treffers geeft in tests die de oude naam bewust noemen (de upgradetest van Task 2) en in `pack/SOURCES.toml`-historie, als die er is.
- [ ] **Step 3: Commit** — `docs(core): de test-gate draait na de commit (#80)`.
