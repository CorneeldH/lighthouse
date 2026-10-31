# `pack/node` en de overstap van deze repo — implementatieplan (#80, scope 5)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Een nieuw pakket `pack/node` levert de selector `gate_node.py`, die de webchecks van #68 (API-drift, lint, typecheck, test) draait als de commit het frontenddomein raakt. Deze repo gebruikt die selector in `[test_gate] command`, en `ci/domains.py gate` verdwijnt.

**Architecture:** `gate_node.py` volgt het contract van `gate_go.py` en `gate_python.py`: hij leest `LIGHTHOUSE_GATE_FILES`, eindigt met 0 en één regel als de commit zijn domein niet raakt, en test zonder lijst het hele domein. Anders dan die twee kiest hij geen losse tests: raakt de commit het domein, dan draait hij in de pakketmap eerst de driftchecks en dan de npm-scripts die het project noemt, in volgorde, en stopt bij de eerste fout. De logica komt uit `ci/domains.py` (`gate`, `api_drift`, `_web_missing`). Deze repo installeert `pack/node` met `init --upgrade --pack node`; daarna houdt `ci/domains.py` alleen `classify` voor CI.

**Tech Stack:** Python 3.9-compatibel stdlib (pakketscript), pytest, Go (`internal/packs`-test), npm.

**Spec:** [`docs/developer/planning/specs/2026-10-27-gate-na-commit-design.md`](../specs/2026-10-27-gate-na-commit-design.md), scope 5 en 6, secties *Selectors* en *Verhouding tot #68* · Issue #80. Eerdere delen: [`2026-10-27-issue-80-gate-na-commit.md`](2026-10-27-issue-80-gate-na-commit.md) (PR #122) en [`2026-10-31-issue-80-stop-checks.md`](2026-10-31-issue-80-stop-checks.md) (PR #132).

**Na dit plan** zijn alle AC van #80 gedekt. De PR zegt `Closes #80`.

## Besluiten van dit plan

De spec laat de Node-selectie aan de bouwer (*Selectors*: "De bouwer werkt de selectie uit als #68 er is"; *Verhouding tot #68*: "`vitest related <files>` kan de tests kiezen … Dat is aan de bouwer"). Dit plan kiest:

1. **Selectie per domein, niet per test.** Raakt de commit het domein, dan draaien alle checks. `vitest related` laat het plan weg: de frontend-suite is klein, lint en typecheck zijn toch hele-domeinchecks, en een tweede selectiemechanisme per testrunner maakt de selector afhankelijk van vitest. Te weinig selectie is hier de goedkope kant.
2. **Het domein:** een pad onder `--dir` (grens op een map: `web/x` raakt `web`, `website/x` niet; `--dir .` raakt elk pad), of een pad dat letterlijk in `--also` staat. Een verwijderd pad telt gewoon mee.
3. **Generieke CLI**, geen lighthouse-specifieke paden in het pakket:
   `gate_node.py [--dir DIR] [--also PATH]... [--drift SCRIPT=PATH]... -- SCRIPT...`
   - `DIR`: de map met `package.json`, relatief aan de root; standaard `.`; hooguit één keer.
   - `--drift SCRIPT=PATH`: draai `npm run --silent SCRIPT` in `DIR` en faal als dat de bestanden onder `DIR/PATH` veranderde. Vergelijkt met de worktree, niet met de index (zoals `api_drift` nu).
   - `SCRIPT...`: npm-scriptnamen, minstens één, elk als `npm run --silent SCRIPT` in `DIR`.
   - Volgorde: eerst elke drift, dan de scripts, stop bij de eerste exitcode ≠ 0 en geef die terug.
4. **COULD NOT CHECK (exit 3)**: een gebruiksfout, een lijst die niet te lezen is, `node` of `npm` niet op `PATH`, `DIR/node_modules` ontbreekt, of `npm` start niet. De regel op stderr: `gate-node: COULD NOT CHECK — <reden>`; bij `node_modules` met `; fix: cd <DIR> && npm ci`.
5. **Deze repo installeert `pack/node`** (`.lighthouse/scripts/gate_node.py`), zoals `pack/go`. `pack/node` heeft geen hooks, skill of instructieblok, dus de installatie voegt alleen het script toe. Het commando wordt:
   `python3 .lighthouse/scripts/gate_go.py -- go test -race && python3 pack/python/scripts/gate_python.py -- uv run pytest -q && python3 .lighthouse/scripts/gate_node.py --dir web --also .github/workflows/ci.yml --also lighthouse.toml --drift gen=src/api -- lint typecheck test`
   `--also` neemt de twee paden van `_WEB_TOO` over die de webchecks raken; `ci/domains.py` hoort er na dit plan niet meer bij (het draait geen webcheck meer). `_WEB_TOO` en `_GO_TOO` in `ci/domains.py` blijven voor `classify` (CI).
6. **Detectie:** `files = ["**/package.json"]`. Op `--upgrade` noemt `init` het pakket alleen; een nieuw `init` stelt het voor. Een Expo-repo krijgt dan beide voorstellen; dat is juist.

## Global Constraints

- `pack/node/scripts/gate_node.py` gebruikt alleen stdlib, draait onder Python 3.9 en begint met `from __future__ import annotations` (zoals `gate_python.py`).
- Uitvoerregels, letterlijk, op stdout:
  - `gate-node: skipped — the commit touches nothing under <DIR>` (bij `--dir .`: `the commit touches nothing`)
  - `gate-node: <DIR> — npm run <SCRIPT>` vóór elk script en elke drift
  - `gate-node: <DIR>/<PATH> was stale; npm run <SCRIPT> regenerated it — commit the result` bij drift (exit 1)
  - `gate-node: passed — <SCRIPT>, <SCRIPT>, …` (alle drift- en scriptnamen, in volgorde)
- Exit 3 blijft in `[test_gate] unchecked_exit_codes`; de commentaarregel erboven noemt de selectors, niet meer `ci/domains.py`.
- Code, namen, commentaar en het changelog-fragment Engels (`code_language = "en"`, zoals de bestaande fragmenten in `changelog.d/`); commits Nederlands.
- Elke wijziging onder `pack/` draait `go run ./cmd/lighthouse init --upgrade --yes` en commit alles wat dat schrijft (geïnstalleerde kopie, `.lighthouse/manifest.toml`). Eindcontrole: `go run ./cmd/lighthouse init --upgrade </dev/null` geeft `No changes.`
- Elk nieuw pakketbestand krijgt `[[file]] path = "…" new = true` in `pack/SOURCES.toml`.
- `ci/domains.py classify`, `.github/workflows/ci.yml` en `tests/test_ci_domains_pin.py` veranderen niet.
- De hooks van de lopende sessie komen uit de hoofdcheckout: `commit-tests.sh` leest `[test_gate] command` uit de `lighthouse.toml` van de hoofdcheckout (`commit-tests.sh:132`, `:320`) en draait het in de worktree (`:498`). Tot de merge draait na elke commit dus het oude commando, dat eindigt op `python3 ci/domains.py gate`, tegen de `ci/domains.py` van de worktree. Test het nieuwe commando met de hand (Taak 2, stap 6).
- Daarom is het schrappen van `gate` (Taak 4) de **laatste** commit van de branch. Na die commit meldt de hook rood met `invalid choice: 'gate'` (argparse, exit 2). Dat is verwacht: herstel geen `gate`-stub, draai de commit niet terug en maak geen fix-commit. Het bewijs is de handmatige run van Taak 4, stap 4; noem de verwachte rode melding in het PR-bewijs.
- Commits: Conventional Commits, Nederlands, één logische wijziging per commit, `(#80)` in de subject.

## Review Focus

1. **Een map die met dezelfde letters begint** (`website/x.ts` bij `--dir web`): niet geraakt. → Taak 1, `test_dir_is_a_directory_boundary`.
2. **`gen` maakt een nieuw bestand aan** onder `src/api` (niet alleen een wijziging): drift, exit 1. → Taak 1, `test_drift_new_file_is_stale`.
3. **Een commit die alleen `lighthouse.toml` raakt** (bijvoorbeeld het gatecommando zelf): de webchecks draaien. → Taak 1, `test_also_path_touches`.
4. **Geen `web/node_modules` in een verse worktree**: exit 3 met de fix-regel, geen stille pass en geen npm-fout als rood. → Taak 1, `test_missing_node_modules_is_unchecked`.
5. **Pad met spatie of verwijderd bestand in de lijst**: telt als geraakt, splitsen alleen op `\n`. → Taak 1, `test_path_with_space_touches`.

---

### Task 1: `pack/node` met de selector `gate_node.py`

**Files:**
- Create: `pack/node/scripts/gate_node.py` (installeert naar `.lighthouse/scripts/gate_node.py`)
- Create: `pack/node/requires.toml`, `pack/node/detect.toml`
- Create: `tests/pack/node/__init__.py`, `tests/pack/node/test_gate_node.py`, `tests/pack/node/test_node_pack.py`
- Modify: `pack/SOURCES.toml` (drie `[[file]]`-regels, `new = true`)
- Modify: `internal/packs/core_test.go` (`TestLanguagePacks`, rij `node`)
- Modify: `docs/site/data/packs.json` (entry `node`), `tests/test_site_packs.py` (`test_every_pack_is_listed`, `test_skills_agents_workflows_match`)
- Modify: `internal/initcmd/initcmd_test.go:510` (`TestInitProposesRealExpo`)
- Modify: `docs/getting-started.md:12-30` (pakketlijst en de tabel *Requirements*)

**Interfaces:**
- Produces:
  - `UNCHECKED = 3`, `class Unchecked(Exception)`, `USAGE = "usage: gate_node.py [--dir DIR] [--also PATH]... [--drift SCRIPT=PATH]... -- SCRIPT..."`
  - `touches(files: list[str], package_dir: str, also: list[str]) -> bool`
  - `snapshot(directory: Path) -> dict[str, bytes]` — relatief posix-pad → inhoud; `{}` als de map niet bestaat.
  - `main(argv: list[str]) -> int` — `argv[0]` is de scriptnaam, zoals in `gate_python.main`.
  - CLI zoals in *Besluiten* 3 en de uitvoerregels in *Global Constraints*.
- Consumes: niets uit andere taken. Kopieer de vorm van `pack/python/scripts/gate_python.py` (`_run`, `_select_and_run`, `main`); port `_snapshot`, `api_drift` en `_web_missing` uit `ci/domains.py:79-126`.

`requires.toml`: `node = ["gate_node"]`, `npm = ["gate_node"]`, `python3 = ["gate_node"]`, met de kopcommentaar van `pack/go/requires.toml`.
`detect.toml`: `files = ["**/package.json"]`, met een commentaarregel waarom (besluit 6).

- [ ] **Step 1: Schrijf de falende tests** in `tests/pack/node/test_gate_node.py`. Laad het script met `importlib` zoals `tests/pack/python/test_gate_python.py:12-18`. Voor de CLI: een fixture `project(tmp_path, monkeypatch)` met `web/package.json`, `web/node_modules/`, `web/src/api/client.ts` (inhoud `"v1"`), en een fake `npm` in `tmp_path/bin` vóór op `PATH` (plus een fake `node`). De fake `npm` schrijft `"$PWD $*"` per regel naar `$NPM_LOG`, eindigt met `$NPM_EXIT_<script>` (standaard 0), en voor script `gen` schrijft hij `$GEN_CONTENT` naar `src/api/client.ts` als die gezet is (en `$GEN_NEW` naar `src/api/new.ts`). `LIGHTHOUSE_GATE_FILES` wijst naar een bestand in `tmp_path`. De tests draaien `main` met `monkeypatch.chdir(tmp_path)`.
  - `test_touches`: `touches(["web/a.ts"], "web", [])` is `True`; `touches(["pack/x.go"], "web", [])` is `False`; `touches(["x"], ".", [])` is `True`; `touches(["lighthouse.toml"], "web", ["lighthouse.toml"])` is `True`; `touches([""], "web", [])` is `False`.
  - `test_dir_is_a_directory_boundary`: `touches(["website/a.ts"], "web", [])` is `False`; `touches(["web"], "web", [])` is `False`.
  - `test_skips_when_commit_touches_nothing_under_dir`: lijst `pack/go/x.go\n` → rc 0, stdout bevat `gate-node: skipped — the commit touches nothing under web`, `NPM_LOG` bestaat niet.
  - `test_runs_drift_then_scripts_in_dir`: lijst `web/src/a.ts\n`, argv `--dir web --drift gen=src/api -- lint typecheck test` → rc 0; de log is precies `[f"{web} run --silent gen", f"{web} run --silent lint", f"{web} run --silent typecheck", f"{web} run --silent test"]`; stdout eindigt met `gate-node: passed — gen, lint, typecheck, test`.
  - `test_also_path_touches`: lijst `lighthouse.toml\n`, `--also lighthouse.toml` → de scripts draaien.
  - `test_path_with_space_touches`: lijst `web/src/a b.ts\n` → de scripts draaien.
  - `test_stops_at_first_failure`: `NPM_EXIT_lint=7` → rc 7, `typecheck` niet in de log.
  - `test_without_gate_files_runs_everything`: `monkeypatch.delenv("LIGHTHOUSE_GATE_FILES", raising=False)` (de hook exporteert die variabele naar pytest) → alle scripts draaien.
  - `test_skips_without_node_modules`: zonder `web/node_modules`, lijst `pack/go/x.go\n` → rc 0 en de `skipped`-regel (de domeincheck komt vóór de controle op `node`, `npm` en `node_modules`, zoals `ci/domains.py:148-155`).
  - `test_drift_stale_fails`: `GEN_CONTENT=v2` → rc 1, stdout bevat `gate-node: web/src/api was stale; npm run gen regenerated it — commit the result`, `lint` niet in de log.
  - `test_drift_new_file_is_stale`: `GEN_NEW=x` → rc 1.
  - `test_drift_up_to_date_passes`: `GEN_CONTENT=v1` → rc 0.
  - `test_drift_gen_failure_returns_its_code`: `NPM_EXIT_gen=4` → rc 4, `lint` niet in de log.
  - `test_missing_node_modules_is_unchecked`: zonder `web/node_modules` → rc 3, stderr is `gate-node: COULD NOT CHECK — web/node_modules missing; fix: cd web && npm ci\n`.
  - `test_npm_not_on_path_is_unchecked`: `PATH` zonder fake `npm` (alleen een map met `node`) → rc 3, stderr bevat `npm not on PATH`.
  - `test_unreadable_list_is_unchecked`: `LIGHTHOUSE_GATE_FILES` wijst naar een niet-bestaand pad → rc 3.
  - `test_usage_errors_are_unchecked` (parametrize): `[]`, `["--"]`, `["--dir"]`, `["--drift", "gen", "--", "lint"]` (zonder `=`), `["--dir", "a", "--dir", "b", "--", "lint"]`, `["--bogus", "--", "lint"]` → rc 3, stderr bevat `usage: gate_node.py`.
  - `test_cli_subprocess`: `subprocess.run([sys.executable, SCRIPT, "--dir", "web", "--", "lint"], cwd=project)` met een lijst buiten `web` → rc 0 (het script draait als bestand, niet alleen als module).

  En `tests/pack/node/test_node_pack.py`, naar `tests/pack/go/test_go_pack.py`:
  - `test_detect_on_package_json`: `detect.toml` → `files == ["**/package.json"]`.
  - `test_no_fragment_hooks_or_instructions`: `lighthouse.toml.fragment`, `hooks/` en `instructions/` bestaan niet in `pack/node`.

- [ ] **Step 2: Zie ze falen.** Run: `uv run pytest tests/pack/node -q` · Expected: FAIL (script en pakket bestaan niet).

- [ ] **Step 3: Implementeer** `pack/node/scripts/gate_node.py`, `requires.toml` en `detect.toml` volgens *Interfaces* en *Besluiten*. De docstring beschrijft het contract zoals die van `gate_python.py` (gebruik, met en zonder lijst, exitcodes, "list 3 in `unchecked_exit_codes`"). Lees de lijst met `errors="surrogateescape"` en splits op `\n`, zoals `ci/domains.py:136`.

- [ ] **Step 4: Registreer het pakket.**
  - `pack/SOURCES.toml`: `pack/node/scripts/gate_node.py`, `pack/node/requires.toml`, `pack/node/detect.toml`, elk `new = true`.
  - `internal/packs/core_test.go`, `TestLanguagePacks`, een rij tussen `go` en `python`: `name: "node"`, `files: []string{".lighthouse/scripts/gate_node.py"}`, `detect: true`, `tools: "node,npm,python3"`.
  - `tests/test_site_packs.py`, `test_every_pack_is_listed`: `on_disk` wordt elke map onder `pack/` (`{p.name for p in PACK.iterdir() if p.is_dir()}`), niet alleen mappen met `skills/`. Docstring: elk pakket staat op de kaart, ook een zonder skill.
  - `tests/test_site_packs.py`, `test_skills_agents_workflows_match` (regel 68): een ontbrekende `skills/` telt als lege lijst (`… if (base / "skills").is_dir() else []`); anders geeft `iterdir()` een `FileNotFoundError` op `pack/node/skills`.
  - `internal/initcmd/initcmd_test.go:510`, `TestInitProposesRealExpo`: die test gebruikt de echte pakketten (`o.Packs = nil`), en `node` vindt dezelfde `apps/a/package.json`. Het verwachte voorvoegsel wordt `"Packages: core, expo (found apps/a/package.json), node (found apps/a/package.json)\n"`.
  - `docs/site/data/packs.json`: entry `node` na `go`, met `"proposed_when": "a package.json that git tracks"`, `"detect": ["**/package.json"]` en lege `skills`, `agents`, `workflows`, `hooks`, `extra`.
  - `docs/getting-started.md`: `tests/test_getting_started.py` eist een rij per tool uit elke `requires.toml`, dus dit hoort bij deze commit. Regel 12: "`go`, `python`, `shell`, `node` or `expo`". Twee rijen na `golangci-lint` (`python3` staat er al met "all"):
    - `` `node` `` | node | the test gate `gate_node.py` | `gate_node.py` reports `COULD NOT CHECK` (exit 3); with 3 in `unchecked_exit_codes` the gate reports the commit as not tested.
    - `` `npm` `` | node | the web checks of `gate_node.py` (`npm run …`) | As for `node`.

- [ ] **Step 5: Draai de tests.** Run: `uv run pytest tests/pack/node tests/test_site_packs.py tests/test_sources.py tests/test_getting_started.py -q && go test -race ./pack/... ./internal/packs/... ./internal/initcmd/...` · Expected: PASS. Run daarna `go run ./cmd/lighthouse init --upgrade </dev/null` · Expected: `No changes.` (`node` staat nog niet in het manifest), eventueel met de notitie `package node fits this repository`.

- [ ] **Step 6: Mutatiecheck** (`lighthouse-testing`): zet in `touches` de mapgrens om naar `startswith(package_dir)` → `test_dir_is_a_directory_boundary` faalt; laat de drift-snapshot nieuwe bestanden negeren → `test_drift_new_file_is_stale` faalt. Draai beide terug.

- [ ] **Step 7: Commit** — `feat(node): pack/node met de selector gate_node.py voor de webchecks (#80)`.

### Task 2: deze repo stapt over op `gate_node.py`

**Files:**
- Modify: `.lighthouse/manifest.toml` en Create: `.lighthouse/scripts/gate_node.py` (beide via `init`)
- Modify: `lighthouse.toml:41-46`
- Modify: `tests/test_dogfood.py:154-168` (`test_test_gate_uses_selectors`)

**Interfaces:**
- Consumes: `gate_node.py` en zijn CLI (Taak 1).

- [ ] **Step 0: Webafhankelijkheden.** Ontbreekt `web/node_modules`, draai dan eerst `cd web && npm ci`. De commit van stap 5 raakt `lighthouse.toml` (in `_WEB_TOO`), dus de oude gate uit de hoofdcheckout draait de webchecks, en zonder `node_modules` blokkeert hij met `COULD NOT CHECK`. Kan `npm ci` niet (geen netwerk): ga door en noem die exit 3 in het PR-bewijs.

- [ ] **Step 1: Pas `test_test_gate_uses_selectors` aan** naar het commando uit *Besluiten* 5 (de laatste `" && python3 ci/domains.py gate"` wordt `" && python3 .lighthouse/scripts/gate_node.py --dir web --also .github/workflows/ci.yml --also lighthouse.toml --drift gen=src/api -- lint typecheck test"`). Docstring: "…its Go packages, pytest files and web checks from the commit." Voeg toe: `test_node_pack_installed`: `.lighthouse/manifest.toml` heeft `"node"` in `packages`, en `.lighthouse/scripts/gate_node.py` is byte-gelijk aan `pack/node/scripts/gate_node.py`.

- [ ] **Step 2: Zie ze falen.** Run: `uv run pytest tests/test_dogfood.py -q -k "selectors or node_pack"` · Expected: FAIL.

- [ ] **Step 3: Installeer en stap over.**
  - `go run ./cmd/lighthouse init --upgrade --pack node --yes`. Expected: `.lighthouse/scripts/gate_node.py` erbij en `packages = ["core", "go", "node"]` in `.lighthouse/manifest.toml`; geen andere wijziging (`git status`).
  - `lighthouse.toml`: het commando uit *Besluiten* 5. Commentaar erboven: "gate_go.py, gate_python.py and gate_node.py choose the Go packages, the pytest files and the web checks of the commit (see the scripts)." Commentaar bij `unchecked_exit_codes`: "3: a selector could not check, so nothing was tested (see the scripts)."

- [ ] **Step 4: Draai de tests.** Run: `uv run pytest tests/test_dogfood.py tests/test_ci_go_test_pin.py -q` · Expected: PASS (`test_ci_go_test_pin.py` vindt `gate_go.py -- go test …` nog in het commando).

- [ ] **Step 5: Commit** — `build(gate): deze repo test de web-checks met gate_node.py (#80)`; manifest, geïnstalleerde kopie, `lighthouse.toml`, `tests/test_dogfood.py`.

- [ ] **Step 6: Proefdraai van het nieuwe commando** (de lopende hook draait nog de oude). Vereist `web/node_modules` (stap 0).
  - `printf 'web/src/main.tsx\n' > "$T/files" && LIGHTHOUSE_GATE_FILES="$T/files" bash -c "$(python3 .lighthouse/lib/lighthouse_lib/config.py test_gate.command)"` met `T` in de scratchpad · Expected: gate-go en gate-python `skipped`, gate-node draait `gen`, `lint`, `typecheck`, `test` en eindigt met `gate-node: passed — gen, lint, typecheck, test`; rc 0.
  - Dezelfde run met `docs/x.md` in de lijst · Expected: drie `skipped`-regels, rc 0.
  - Kan `npm ci` niet (geen netwerk): meld dat als niet gemeten in het PR-bewijs en noem de exit 3-regel die de run wel gaf.

### Task 3: labels en changelog

**Files:**
- Modify: `docs/developer/labels.md:25`
- Create: `changelog.d/issue-80-pack-node.md`

- [ ] **Step 1: `docs/developer/labels.md:25`**: `pack/node` in de lijst van `2-area:packs`, tussen `pack/shell` en `pack/expo`.
- [ ] **Step 2: Changelog-fragment** `changelog.d/issue-80-pack-node.md`, in de vorm van de bestaande fragmenten (`type: added`): "A new package `node` adds `gate_node.py`, the test-gate selector for a JavaScript project: when the commit touches the package directory it runs the npm scripts you name (lint, typecheck, test) after any drift check (`--drift gen=src/api`). Add it with `lighthouse init --upgrade --pack node` and put it in `[test_gate] command` yourself."
- [ ] **Step 3: Controle.** Run: `uv run pytest tests/test_instruction_files.py tests/test_site_packs.py tests/test_getting_started.py -q` en `go run ./cmd/lighthouse init --upgrade </dev/null` · Expected: PASS en `No changes.`
- [ ] **Step 4: Commit** — `docs: pack/node in labels en changelog (#80)`.

### Task 4: `ci/domains.py gate` verdwijnt (laatste commit)

Deze taak komt als laatste: zie *Global Constraints* over de hook uit de hoofdcheckout. Na de commit van stap 5 meldt de hook rood met `invalid choice: 'gate'`; dat is verwacht en wordt niet gerepareerd.

**Files:**
- Modify: `ci/domains.py`, `tests/test_domains.py`

**Interfaces:**
- Produces: `ci/domains.py` met alleen `DOMAINS`, `_WEB_TOO`, `_GO_TOO`, `_NOT_GO_DIRS`, `_NOT_GO`, `classify`, `_classify_cli` en `main` met het subcommando `classify`. `main` gebruikt nog steeds argparse met verplicht subcommando, zodat `ci.yml:35` ongewijzigd blijft.

- [ ] **Step 1: Schrap `gate` uit `ci/domains.py`.** Verwijder `CHECKS`, `UNCHECKED`, `_say`, `_snapshot`, `api_drift`, `_git_paths`, `changed_paths`, `_web_missing`, `gate`, het subcommando `gate` en de imports die daarna ongebruikt zijn (`os`, `shutil`, `subprocess`, `Callable`, `Path` als niemand ze nog gebruikt). Docstring: één gebruiker, CI (`classify`); de eerste regel wordt "Sort changed paths into the domains web, python and go, for the jobs of CI." Het commentaar bij `_WEB_TOO` noemt "the CI job, this script, and the gate command in lighthouse.toml" en blijft kloppen.
  - `tests/test_domains.py`: verwijder `test_checks_hold_only_web`, alle `test_gate_*` en `test_api_drift` (regel 235; die dekking zit nu in de drifttests van `tests/pack/node/test_gate_node.py`), met hun fixtures en helpers (`repo`, `web_ready`, `markers`, `_touching`, `_ran`, `_no_drift`, `_no_gate_files`), plus imports die daarna ongebruikt zijn. Moduledocstring: "ci/domains.py: which domains a change touches." `test_classify` en `test_no_go_file_names_a_path_outside_go` blijven.
  - Voeg toe aan `tests/test_domains.py`: `test_gate_subcommand_is_gone`: `domains.main(["gate"])` geeft `SystemExit` met code 2 (argparse, onbekende keuze).

- [ ] **Step 2: Draai de tests.** Run: `uv run pytest tests/test_domains.py tests/test_ci_domains_pin.py tests/test_dogfood.py -q && ! grep -rn 'domains.py gate' --include='*.py' --include='*.toml' --include='*.yml' --include='*.sh' . | grep -v '^./docs/developer/planning/'` · Expected: PASS en rc 0 (de `!` maakt van "grep vindt niets" een geslaagde check).

- [ ] **Step 3: Eindcontrole.** Run: `go run ./cmd/lighthouse init --upgrade </dev/null` · Expected: `No changes.`

- [ ] **Step 4: Handmatige run van het nieuwe commando** na de wijziging, met de lijst `ci/domains.py\ntests/test_domains.py\n` in `$T/files` (zoals Taak 2, stap 6): `LIGHTHOUSE_GATE_FILES="$T/files" bash -c "$(python3 .lighthouse/lib/lighthouse_lib/config.py test_gate.command)"` · Expected: gate-go en gate-node `skipped`, gate-python draait de pytest-bestanden van die lijst groen; rc 0. Dit is het bewijs voor deze commit; de hook draait het oude commando.

- [ ] **Step 5: Commit** — `refactor(ci): ci/domains.py houdt alleen classify voor CI (#80)`. De hook meldt daarna rood met `invalid choice: 'gate'`: verwacht, zie *Global Constraints*; geen fix-commit.
