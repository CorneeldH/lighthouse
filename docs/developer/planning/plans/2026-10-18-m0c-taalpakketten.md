# M0 (c): taalpakketten `pack/python`, `pack/shell` en `pack/expo` — implementatieplan (#8)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `lighthouse init` herkent welke taalpakketten bij een repo passen en stelt ze voor, en `pack/python`, `pack/shell` en `pack/expo` staan vertaald en losgetrokken in lighthouse.

**Architecture:** Vier PR's (tabel *PR-indeling*). PR c1 leert `init` taalpakketten: alle pakketmappen in de binary, een herkenningsregel per pakket (`detect.toml`), een fragment voor `lighthouse.toml` en de vlaggen `--pack`/`--no-pack`. Daarna één PR per pakket, met het verhuisrecept van (b). De pakket-PR's wachten op lijn 3B van (b), want hun gates schrijven de eerste regel die `bash_writes.classify` leest (besluit 18), en de test-gate-AC heeft `pre-commit-tests` nodig (taak 14 van (b)).

**Tech Stack:** Go 1.26 met de libraries uit (a); Python ≥ 3.11 voor scripts, Python 3.9-syntaxis voor `pack/*/lib/`; bash 3.2-compatibel voor hooks; `uv` + `pytest` voor de Python-lane.

**Spec:** [`2026-10-17-m0-pakket-design.md`](../specs/2026-10-17-m0-pakket-design.md), secties *Inventaris* (met *Kruisverwijzingen*), *Taal*, *Indeling in de lighthouse-repo*, *init* en *Opdeling (c)*; besluiten 4, 6, 7, 14, 18, 19 en 20. Plan (b): [`2026-10-18-m0b-pack-core.md`](2026-10-18-m0b-pack-core.md). Dat plan is het voorbeeld voor vorm en werkwijze. Het **verhuisrecept** (V1–V7), de tabel *Configsleutels* en de *Global Constraints* van (b) gelden hier ongewijzigd, met `pack/<pakket>` waar (b) `pack/core` zegt. Dit plan herhaalt ze niet. Invoer: de inventaris in *Inventaris (c)* hieronder.

**Bron:** paia op `33833601349a8b79a1d1b2ac9077cc495a0d2b37` (hieronder `SRC`), zoals in (b). Elke taak kopieert met `git -C <paia> show SRC:<pad>`.

## Ontwerpkeuzes

De spec en de opdracht laten deze punten open. Elk punt is een voorstel; wijs aan wat anders moet vóór de uitvoering.

1. **Vier PR's.** c1 (dit plan en het init-deel) heeft niets nodig dat nog niet op `main` staat. c2 (python), c3 (shell) en c4 (expo) starten pas als lijn 3B van (b) gemerged is; c2 wacht ook op 3A (zie *PR-indeling*). Elke PR noemt `Refs #8`; de laatste die merget, sluit het issue met `Closes #8`.
2. **Alle pakketmappen in de binary.** `pack/pack.go` embedt `all:*/*`. Dat patroon neemt elke map onder `pack/` mee met zijn hele inhoud, ook `_`- en `.`-bestanden, en laat de bestanden in `pack/` zelf (`pack.go`, `SOURCES.toml`) buiten de binary. Een map die nog niet bestaat, geeft geen buildfout: `pack/python` kan in c2 komen zonder dat `pack.go` verandert. Een pakket is een map onder `pack/`; `init` vindt ze met `fs.ReadDir(o.Packs, ".")`.
3. **Herkenningsregel per pakket: `detect.toml` in het pakket.** Twee soorten regels, en het pakket past als één ervan raakt:
   - `files`: een lijst git-pathspecs met glob-semantiek (`*` stopt bij `/`, `**/` raakt ook de wortel). Raak: een bestand dat git volgt of zou volgen, `git ls-files --cached --others --exclude-standard -- ':(glob)<p>' …`. Dus geen bestanden onder `.gitignore`, zoals `.venv/` of `node_modules/`.
   - `[[contains]]` met `files` (pathspecs als hierboven) en `text`: raak als zo'n bestand `text` letterlijk bevat. Een bestand groter dan 1 MiB telt niet mee.

   De regel staat bij het pakket zelf, zodat een nieuw pakket geen Go-wijziging vraagt. `core` heeft geen `detect.toml` en komt er altijd bij. Een ander pakket zonder `detect.toml` komt er alleen bij met `--pack`. De regels:
   - `python`: `files = ["pyproject.toml", "setup.py", "setup.cfg"]`, alleen in de wortel. De default `[test_gate]` draait `uv run pytest` in de wortel. In een repo met alleen losse `.py`-bestanden zou die elke commit laten mislukken; zo'n repo kiest `pack/python` dan zelf met `--pack python`.
   - `shell`: `files = ["**/*.sh", "**/*.bash"]`. De shellcheck-gate werkt per bestand en heeft geen projectcommando nodig, dus elk shellscript telt.
   - `expo`: `[[contains]] files = ["**/package.json"], text = "\"expo\""`. In paia staan de apps in `apps/<app>/`, elk met `"expo": "~54.0.37"` in `package.json` en zonder `app.config.*` of `eas.json` (inventaris (c), expo). Een regel op `app.json` alleen zou ook React Native zonder Expo raken.
4. **`lighthouse.toml.fragment`: platte tabellen, één regel per sleutel.** Het fragment bestaat uit kopregels `[<tabel>]` (`[a-z_]+(\.[a-z_]+)*`) en regels `<sleutel> = <waarde>`. Elke waarde is één TOML-waarde op één regel. Commentaar en lege regels mogen, maar ze komen niet in `lighthouse.toml`. Wat daarbuiten valt, weigert `packs.Load`: een sleutel vóór de eerste kop, een waarde over meer regels, een lege tabel, een dubbele tabel of sleutel. Zo kan `init` het fragment zonder herschrijven in `lighthouse.toml` invoegen, met `projectconfig.AppendMissing` (spec, *init* punt 2), en blijft de letterlijke waarde staan zoals het pakket hem schrijft. Bij `init` en `--upgrade` vult `init` alleen ontbrekende sleutels aan. Een sleutel die het project al heeft, blijft zoals hij is. Leveren twee gekozen pakketten dezelfde sleutel, dan stopt `init` zonder te schrijven, en de fout noemt beide pakketten.
5. **De gekozen pakketten staan in het manifest.** `manifest.Packages` bestaat al sinds (a) en `--upgrade` leest het al. Bij `init` is de lijst `core` plus de herkende pakketten, plus `--pack`, min `--no-pack`. Bij `--upgrade` is het `manifest.Packages` plus `--pack`, min `--no-pack`. `--upgrade` herkent dus niet opnieuw, zodat een pakket er nooit stil bij komt. Volgorde: `core` eerst, dan alfabetisch. Die volgorde bepaalt ook de volgorde van hookregistraties en fragmenten.
6. **Een pakket erbij of eraf, via `--upgrade`.** `lighthouse init --upgrade --pack python` voegt toe; `lighthouse init --upgrade --no-pack shell` haalt weg. Weghalen gaat via de bestaande reconcile: elke eenheid van het pakket wordt `Remove`, en een eenheid die lokaal gewijzigd is, wordt `Conflict`. Dan weigert `init`, tenzij met `--force` (besluit 4). Wat het fragment in `lighthouse.toml` zette, blijft staan, want dat is nu projectconfig. `init` meldt dan op stderr: `lighthouse.toml: the keys package <p> added stay; remove them by hand if you no longer want them`. Past een pakket bij de repo maar staat het niet in het manifest, dan meldt `--upgrade` op stderr: `package <p> fits this repository (found <bestand>); add it with: lighthouse init --upgrade --pack <p>`. `--no-pack core` geeft een fout. Een onbekende naam bij `--pack` of `--no-pack` geeft de bestaande fout `package <p> is not in this binary`.
7. **Eén akkoord, geen vraag per pakket.** Een verse `init` drukt vóór de diff één regel op stdout: `Packages: core, python (found pyproject.toml), shell (found ci/x.sh)`, met `(--pack)` bij een pakket dat met de vlag kwam. Een `--upgrade` drukt alleen bij een wijziging `Adding package <p>.` of `Removing package <p>.` op stdout. Zo blijft de uitvoer van een `--upgrade` zonder wijziging exact `No changes.`, en daar hangen de e2e-test en de CI-stap van (b) aan. Wie een herkend pakket niet wil, draait opnieuw met `--no-pack`. Een akkoord per pakket zou een tweede vraag naast de diff zetten, en de diff toont al per bestand wat een pakket schrijft.
8. **`requires.toml` per pakket.** Het formaat is dat van `pack/core/requires.toml`. `init` voegt de vereisten van de gekozen pakketten per tool samen, zodat een tool die twee pakketten nodig hebben, één melding geeft met de eenheden van beide.
9. **De config-lookup ziet het fragment.** `pythonLookup` leest `lighthouse.toml` zoals `init` hem achterlaat (ontwerpkeuze 13 van (b)). Dat is na het invoegen van de fragmenten. Een `@@test_gate.command@@` in een workflow krijgt dus de waarde uit het fragment.
10. **Kruisverwijzingen** (spec, *Inventaris*): `fix-pr` (core) roept `quality/ratchet.py` aan als het er is (ontwerpkeuze 16 van (b)), en `bash_writes` meldt een ontbrekende gate van een taalpakket (besluit 19, taak 11 van (b)). Dit plan legt daar niets aan toe. Elke pakket-PR toetst alleen dat zijn bestanden op de paden staan die core verwacht. De contracttest voor skills (taak 22 van (b)) kijkt alleen in `pack/core/skills/`; `fix-pr` noemt `/lighthouse-refresh-expo-lockfiles` (inventaris (c), expo). Daarom breidt de eerste pakket-PR die na taak 22 van (b) merget, die test uit naar `pack/*/skills/`. Een link naar een skill in een taalpakket staat in een alinea met `if present`.
11. **`ratchet.py nightly`, `prcov.py` en `prcov_issues.py` blijven in paia** (inventaris (c), quality). **Open vraag aan Corneel, nodig vóór taak 7; niet blokkerend voor c1.** Alleen paia's `nightly-tests.yml` roept `nightly` aan. Geen verhuizende skill heeft het nodig. `prcov_issues.py` is controllercode: het schrijft een 🤖 Handoff met de oude sleutels naar `/paia-start-issue`, zet de boardstatus `Controller` en leest `PAIA_BOARD_TOKEN`, de labels `1-loc:container`/`2-area:meta` en een vast hoofdcheckoutpad. Voorstel, naar het voorbeeld van besluit 26: `pack/python` krijgt `ratchet.py` met `ruff`, `coverage`, `show` en `tighten`, plus `baseline.py`, `diffcov.py` en `gitenv.py`. `cmd_nightly`, `prcov.py` en `prcov_issues.py` blijven in paia tot M2, bij de controller, en importeren in (d) uit `.lighthouse/scripts/quality/`. Het alternatief: alles mee, met `[python.nightly]` en `[labels]` voor de literals, en `prcov_issues` op het protocol van 3A. Dan wacht c2 ook op 3A.
12. **Configsleutels van de taalpakketten**, in `config.py` van core (één schema, ontwerpkeuze 8 van (b)). Voor shell: `shell.strict_dirs` (list[str], default `[".claude/hooks"]`) vervangt `GATE_DIRS` in `shellcheck-repo.sh` en de mappen in `shellcheck.sh` (79-83); een git-pathspec `<dir>/*.sh` raakt ook submappen, dus ook `.claude/hooks/lighthouse/`. Paia zet in (d) zijn drie mappen. Voor python: `python.source_dirs` (list[str], default `["src"]`) vervangt `src/` in `ty-check.sh`, in `ratchet.py` (`--cov`, `git diff -- src/`, complexipy) en in de skill. `python.lint_dirs` (list[str], default `["src", "tests"]`) vervangt `src/ tests/` in `ratchet.py ruff`. `python.coverage_command` (str, default `uv run pytest -q`) vervangt `COVERAGE_ARGV` zonder de `--cov`-vlaggen, die `ratchet.py` er zelf achter zet uit `source_dirs`. De basis van `ratchet.py coverage` is `origin/<forge.default_branch>` (besluit 23). De pinbestanden `.ruff-version` en `.complexipy-version`, het baselinebestand `quality-baseline.toml` en de grenzen in `ratchet.py` (`CYCLOMATIC_LIMIT` en de rest) zijn projectbestanden en codebeleid, geen paia-literals, en blijven zoals ze zijn. Het fragment levert alleen `[test_gate]`.
13. **Wat een gate doet als hij niet kon controleren.** `ruff-format.sh` en `shellcheck.sh` zijn gates op een write: exit 2 met de eerste regel `<gate>: COULD NOT CHECK — <reden>` (besluit 18), net als `shellcheck.sh` nu al blokkeert met `NIET gecontroleerd`. Daarmee gaan de stille uitgangen van `ruff-format.sh` dicht: geen `jq`, ongeldige JSON, geen `uvx` en geen `ruff`, een mislukte versieprobe. Geen gat zijn de uitgangen waar niets te controleren valt: geen `file_path`, een andere extensie, een bestand dat niet bestaat. `ty-check.sh` draait bij `Stop`. Blokkeren op "niet gecontroleerd" zou daar de sessie niet laten stoppen, dus hij meldt het met exit 0 en een `systemMessage` `ty-check: COULD NOT CHECK — <reden>`. Ook als geen map uit `python.source_dirs` bestaat: `ty-check: not checked — no directory from python.source_dirs exists (set it in lighthouse.toml; [] turns the check off)`. Een lege lijst zet de check uit, zonder melding.
14. **Wat in paia blijft.** `.codex/hooks/ruff-format.sh` en `.codex/hooks.json` (een oudere fork, en Codex draait geen hooks: AGENTS.md *Outside Claude Code*). `scripts/hooks/pre-push-quality.sh` en `workflow-audit.sh` (besluit 17). De workflows `nightly-tests.yml`, `quality-baseline-ratchet.yml`, `apps.yml` en `.github/dependabot.yml`. De tests die paia's eigen boom lezen (`test_blocking_rules_are_selected_in_pyproject`, `test_ty_hook_is_green_on_this_repo`, `test_shellcheck_pin_matches_workflow_audit`, `test_shellcheck_gate_set_is_clean`, de echte `quality-baseline.toml` in `test_quality_ratchet.py:722-728`). Elke PR noemt ze voor (d).

## Global Constraints

- Die van plan (b) gelden, met `pack/<pakket>` voor `pack/core`.
- Pakketnamen en paden, letterlijk: `pack/python`, `pack/shell`, `pack/expo`; skills `python-writing`, `shell-gates`, `refresh-expo-lockfiles` (besluit 6), geïnstalleerd als `.claude/skills/lighthouse-<naam>/` en `.agents/skills/lighthouse-<naam>/`; hooks in `.claude/hooks/lighthouse/`; scripts in `.lighthouse/scripts/`.
- Fragment: alleen `pack/<pakket>/lighthouse.toml.fragment`, met de vorm van ontwerpkeuze 4. Herkenning: alleen `pack/<pakket>/detect.toml`, met de vorm van ontwerpkeuze 3.
- De eerste stderr-regel van een gate die niet kon controleren: `<gate>: COULD NOT CHECK — <reden>` (besluit 18; ontwerpkeuze 5 van (b)).
- Meldingen van `init`, letterlijk: `Packages: …`, `Adding package <p>.`, `Removing package <p>.`, `package <p> fits this repository (found <bestand>); add it with: lighthouse init --upgrade --pack <p>`, `lighthouse.toml: the keys package <p> added stay; remove them by hand if you no longer want them`, `core cannot be left out`.
- `go test ./...`, `uv run pytest -q`, `golangci-lint run` en `ci/literal-grep.sh` zijn groen op de kop van elke PR.

## Review Focus

1. **Een Python-repo die al een `[test_gate]` heeft.** Verwacht: `init` laat `command` van het project staan en vult alleen ontbrekende sleutels aan. Test: `TestFragmentKeepsProjectKeys` in taak 4.
2. **Een repo die na de eerste `init` Python wordt.** Verwacht: `--upgrade` voegt `pack/python` niet stil toe, meldt hoe het kan, en geeft op stdout nog steeds exact `No changes.`. Test: `TestUpgradeSuggestsFittingPack` in taak 3.
3. **Een pakket weghalen waarvan een hook lokaal gewijzigd is.** Verwacht: weigering met de diff, niets weg; met `--force` wel weg. Test: `TestNoPackRefusesLocalEdit` in taak 3.
4. **Een marker die alleen in een submap staat of onder `.gitignore` valt.** Verwacht: `sub/pyproject.toml` of een genegeerde `setup.py` stelt `pack/python` niet voor; een gevolgd `ci/x.sh` stelt `pack/shell` wel voor. Test: `TestDetectRules` in taak 3.
5. **Twee pakketten met dezelfde sleutel in hun fragment.** Verwacht: `init` stopt vóór de diff, noemt beide pakketten en de sleutel, en schrijft niets. Test: `TestFragmentKeyInTwoPacks` in taak 4.

---

## Inventaris (c)

Per bestand op `SRC`: wat het aanroept, importeert en leest (besluit 14), met de literals, de contracttokens en de stille uitgangen. Regelnummers horen bij `SRC`. Drie lees-agents maakten deze lijst, zoals in (b); de bouwer toetst de nummers bij het overnemen.

### python

| Bestand | Roept aan, leest | Losmaken | Vertalen en tokens | Stil |
|---|---|---|---|---|
| `.claude/hooks/ruff-format.sh` (95) | `jq` op `.tool_input.file_path` (12-13); `$ROOT/.ruff-version` (39-40); `uvx ruff@<pin>`, anders `uvx ruff`, anders `ruff` (44-54); pass 1 `format` + `check --fix` (67-68, stdout lekt); pass 2 `check --select $BLOCKING_RULES` (85-87) | root via `git rev-parse` vanuit cwd (37); `bash_writes.run_gate` zet cwd op de root | "Lint-gate: los dit op…" (88-89) → Engels; eerste regel bij niet-controleren `ruff-format: COULD NOT CHECK — …` (nieuw, besluit 18) | geen `jq`, JSON, geen tool, probe (8-17, 52-63); pass 1 slikt fouten (67-68) |
| `.claude/hooks/ty-check.sh` (105) | `TY_VERSION="0.0.72"` (13); `jq` en `uvx` (15); `.stop_hook_active` (23), `.cwd` (29); `.venv` (41-48); `uvx ty@… check --python $VENV src/` (64); codes uit `error[x]` (68-69) | `src/` (36, 64, 85, 98) → `python.source_dirs`; de Mac-hint over `rumps` (88-89) eruit | "Type-gate: …" (84-102) → Engels | geen `jq`/`uvx`, geen repo, geen `src/`, geen venv, probe (15-57) |
| `paia-python-schrijven` (C 179, A 191) | KPI-tabel met `uvx ruff@0.16.4`, `uvx ty@0.0.72 check src/`, `test_unit_network_guard.py`, `ratchet.py coverage --base origin/main` (33-37); `.claude/rules/python.md` (21, 146); `paia-shell-gates` (3, 18) | issue-nummers, metingen, `apps/`, macOS-extra's (97-98), provenance (173-179) eruit; `ratchet.py` via `.lighthouse/scripts/quality/`; `.claude/rules/python.md` "if present" | Nederlands → Engels; de kop "Een complexiteitsgrens halen…" is een token voor `ratchet.py` (`COMPLEXITY_SKILL`, 927-930) | — |
| Variant `.agents` | +regel 3 (OpenCode), 21 (link zonder pad), +76-79 (#806, testcommando met de hand), +146-153 (OpenCode-blok met de handcommando's) | het handcommando wordt `config.py test_gate.command` | — | — |
| `scripts/quality/ratchet.py` (1063) | `uvx ruff@<pin> check … src/ tests/` (137-151); `uvx complexipy@<pin> <root>/src <root>/tests …` (290-306); `COVERAGE_ARGV` (425-434); `git merge-base`, `git diff -U0 … -- src/` (448, 538); leest `quality-baseline.toml`, `.ruff-version`, `.complexipy-version`, `coverage.json`; schrijft de baseline (`tighten`, 907) | sys.path op de eigen map (33-35) blijft; root via `git rev-parse` (80-89) blijft; `src/`, `tests/`, `tests/unit`, de markers, `origin/main` → ontwerpkeuze 12; `COMPLEXITY_SKILL` → `.claude/skills/lighthouse-python-writing/SKILL.md`; `cmd_nightly` (677) → ontwerpkeuze 11 | alle uitvoer en help Nederlands; interne contracten `BUMPABLE_FAILURE` (925) en `HARD_LIMIT_MARK` (926, moet gelijk zijn aan 353) vertalen samen | `judge_coverage` groen bij 0 regels (464); `tighten` hergebruikt een oud `coverage.json` (882-891) |
| `baseline.py`, `diffcov.py`, `gitenv.py`, `__init__.py` | alleen stdlib; geen subprocessen | — | `BaselineError`-meldingen (68-191) en docstring van `__init__` naar Engels; docstrings verder niet (ontwerpkeuze 3 van (b)) | `diffcov`: een rapport zonder regels valt uit de noemer (184-189) |
| `prcov.py` (165), `prcov_issues.py` (435) | `gh run/issue/api/project` (104-407); `src.controller.comments.KOP`, `identity` (38-39); `PAIA_BOARD_TOKEN` (43); labels (44); `HOOFDCHECKOUT` (51); `/paia-start-issue` (261, 270) | ontwerpkeuze 11: blijft in paia | handoff met oude sleutels (255-272) | terugval op 24 uur (86-95); gesloten issue = "afgehandeld" (234) |
| Tests | `test_quality_hooks.py` 54-292 (ruff 54-101, pins 103-163, ty 166-292), 741-759 (registratie); `test_quality_ratchet.py` (1661; leest paia's boom op 42, 53, 174-203, 658, 722-728); `test_quality_prcov*.py`; `test_quality_ratchet.sh` (kopieert `pre-push-quality.sh`, 95-99) | `test_instructie_hooks.py` heeft géén ruff- of ty-gevallen | pinnen "Lint-gate", "Type-gate", "geen bump", "cyclomatisch 21 > 20" en meer (zie V2) | `test_quality_ratchet.sh` slaat zonder `uv` stil over (32-38) |

Registratie op `SRC` (`.claude/settings.json`): `ruff-format.sh` bij `PostToolUse` `Edit|Write`, timeout 15 (32); `ty-check.sh` bij `Stop`, matcher `""`, timeout 60 (172). Writes via Bash bereiken `ruff-format.sh` via `bash-write-gates` (`GATES[".py"]`, `_SIGN_AFTER_GATE`). "Coverage" is coverage.py-JSON uit `pytest-cov` (`--cov=src --cov-report=json:coverage.json`); er is geen `[tool.coverage]` of `.coveragerc`. Andere importeurs van `quality.*` in paia: `scripts/deep_sweep.py:55-60`, `scripts/opencode-lane/lane/gates.py:17` (voor (d)).

### shell

| Bestand | Roept aan, leest | Losmaken | Vertalen en tokens | Stil |
|---|---|---|---|---|
| `.claude/hooks/shellcheck.sh` (101) | `jq`, anders `python3` op `.tool_input.file_path` (33-36); zoekt `scripts/hooks/shellcheck-repo.sh` in git-root van het bestand, `$HOOK_DIR/../..`, `$CLAUDE_PROJECT_DIR` (59-72); strengheid `style` in de gate-mappen, anders `warning` (79-83) | het script wordt `$ROOT/.lighthouse/scripts/shellcheck-repo.sh`, root via `LIGHTHOUSE_REPO_ROOT` of de git-root van het bestand; de gate-mappen → `shell.strict_dirs` | `shellcheck-gate: … NIET gecontroleerd` (38-39, 74-75, 99) → `shellcheck: COULD NOT CHECK — …`; de bevinding "los dit op in de code…" (91-93) → Engels | een PATH-shellcheck die van de pin afwijkt: de waarschuwing valt weg bij rc 0 (89) |
| `scripts/hooks/shellcheck-repo.sh` (78) | sourcet `$HERE/lib/shellcheck-bin.sh` (19-22); `GATE_DIRS=(scripts/hooks tests/integration .claude/hooks)` (26); zonder bestanden `git ls-files … -- <dir>/*.sh` (67); `shellcheck -x -S <ernst>` (77) | `GATE_DIRS` → `shell.strict_dirs`; de bron-map blijft `$HERE` (een buurbestand, zoals `review_pair` in (b)) | usage en meldingen (30, 56, 69) → Engels; exitcontract 0/1/2/4 (9-13) blijft | elke rc van shellcheck wordt 1 (77) |
| `scripts/hooks/lib/shellcheck-bin.sh` (148) | pin `0.11.0` (18) met sha256 per platform (56-58); cache `$(git rev-parse --git-common-dir)/workflow-audit-tools` (32-43); `curl`, `sha256sum`, `tar -xJ` (49-83); volgorde `SHELLCHECK_BIN`, cache, PATH, download, afwijkende PATH (102-148) | cache → `lighthouse-tools` (gedeeld met paia's `workflow-audit.sh`, dat blijft); pin en hashes blijven | meldingen → Engels; "wijkt af van de pin" (141) wordt Engels, met zijn test | — |
| `paia-shell-gates` (C 173, A 181) | noemt `workflow-audit.sh`, `shellcheck-repo.sh`, `test_quality_hooks.py`, `gate-assert.sh`, `canon_path` in `vault-write-guard.sh`, twee specs, issues en metingen | paia-eigen verwijzingen eruit of "if present"; gate-mappen via `config.py shell.strict_dirs` | Nederlands → Engels | — |
| Variant `.agents` | +regel 3 (OpenCode), +8 regels na 105 (OpenCode-blok met `shellcheck-repo.sh --severity warning -- <bestand>`) | — | — | — |
| Tests | `test_quality_hooks.py` 518-828 (hook, pin, `resolve_shellcheck`, het gate-geheel, `test_shellcheck_hook_reads_the_path_without_jq` 784-828); `test_bash_write_gates.py` 95-172 en de e2e-gevallen; `test_bash_writes.py` 825-851 (classify) | de `bash_writes`-gevallen horen bij (b) taak 11 en 13 | pinnen "NIET gecontroleerd", "wijkt af van de pin" | — |

Registratie op `SRC`: `shellcheck.sh` bij `PostToolUse` `Edit|Write`, timeout 60, na `ruff-format.sh` (36-38).

### expo

| Bestand | Roept aan, leest | Losmaken | Vertalen |
|---|---|---|---|
| `paia-refresh-expo-lockfiles` (153, varianten byte-identiek) | workflow `apps.yml` "Expo apps" (3, 15, 17); `apps/*` en de drie appnamen (10, 17, 63); Node 22 en `NODE_OPTIONS` (59-60); `npm ci`, `npx expo-doctor`, `npx expo install --fix`, `npm dedupe`, `npm test --if-present` (65-103); `.github/dependabot.yml` (19, 95); `/paia-write-issue` met `2-area:clients` (145); `upgrading-expo` (146); `apps/CLAUDE.md` (145, 153) | de apps vindt de skill zelf: elke `package.json` met `"expo"` (ontwerpkeuze 3); de workflow en `dependabot.yml` "if present"; `/lighthouse-write-issue`, met het area-label uit `config.py labels.area_prefix` als dat bestaat; SDK-versies en metingen eruit | Nederlands → Engels; één `SKILL.md` |

Een pinnende test: `test_fix_pr_skill.py:100` (de rij "`Expo apps` — `expo-doctor`" met `/paia-refresh-expo-lockfiles` in `fix-pr`). Die hoort bij (b) taak 25.

---

## PR-indeling

| PR | Branch | Taken | Heeft nodig |
|---|---|---|---|
| c1 plan en init | `issue-8-plan-en-init` | dit plan, 1–4 | — |
| c2 python | `issue-8-c2-python` | 5–8 | c1; (b) 3B; ook 3A als ontwerpkeuze 11 anders uitvalt |
| c3 shell | `issue-8-c3-shell` | 9–10 | c1; (b) 3B |
| c4 expo | `issue-8-c4-expo` | 11 | c1 |

- **Waarom c2 en c3 wachten.** De gates van python en shell moeten de eerste regel `<gate>: COULD NOT CHECK — …` schrijven, die `bash_writes.classify` uit taak 11 van (b) herkent (besluit 18). Hun tests draaien de gate via `bash-write-gates` uit taak 13 van (b). De AC "de test-gate blokkeert een commit met een rode test" heeft `pre-commit-tests` uit taak 14 van (b) nodig. Gaat `prcov_issues.py` toch mee (ontwerpkeuze 11), dan wacht c2 ook op 3A, want het schrijft een handoff met het comment-protocol.
- **c4 wacht niet op (b).** `pack/expo` heeft alleen een skill. Hij kan direct na c1.
- **Gedeelde bestanden.** c2, c3 en c4 voegen elk alleen regels toe aan `pack/SOURCES.toml` en aan de tabel in `internal/packs/` die per pakket de eenheden pint (taak 8, 10, 11). Wie als tweede merget, rebaset.

---

## PR c1 — plan en init (`issue-8-plan-en-init`)

### Task 1: Alle pakketmappen in de binary

**Files:**
- Modify: `pack/pack.go`, `tests/test_sources.py`
- Create: `pack/pack_test.go`

**Interfaces:**
- Produces: `pack.Files embed.FS` bevat `<pakket>/…` voor elke map onder `pack/`, en geen bestand uit `pack/` zelf.

- [ ] **Step 1: Schrijf de falende tests.**
  - `TestEmbedsEveryPackageDir` in `pack/pack_test.go`: voor elke map in `os.ReadDir(".")` geldt `fs.Stat(Files, <map>)` zonder fout; `fs.ReadDir(Files, ".")` bevat alleen mappen (dus geen `pack.go` en geen `SOURCES.toml`).
  - `TestEmbedsHiddenAndUnderscoreFiles`: `core/lib/__init__.py` staat in `Files`.
  - `tests/test_sources.py`: `_pack_files()` loopt over elke map onder `pack/`, niet alleen `pack/core`. Nieuw `test_walks_every_package`: een tijdelijke `pack/zz-test/x` (via `monkeypatch` van `REPO`) valt onder `_pack_files()`.
- [ ] **Step 2: Draai** met een tijdelijke `pack/zz/x` erbij: `go test ./pack/ && uv run pytest tests/test_sources.py -q` — verwacht: FAIL, `package directory zz is not embedded`, en `test_walks_every_package` faalt. Haal `pack/zz/` na stap 4 weer weg.
- [ ] **Step 3: Implementeer.** `//go:embed all:*/*`, met een commentaar dat uitlegt waarom dit patroon (ontwerpkeuze 2).
- [ ] **Step 4: Draai** `go test ./... && uv run pytest -q` — verwacht: PASS.
- [ ] **Step 5: Commit** `feat(pack): elke pakketmap in de binary, ook een die nog niet bestaat`

### Task 2: `detect.toml` en `lighthouse.toml.fragment` in een pakket

**Files:**
- Modify: `internal/packs/packs.go`, `internal/packs/packs_test.go`, `internal/projectconfig/projectconfig.go`, `internal/projectconfig/projectconfig_test.go`

**Interfaces:**
- Produces:
  - `projectconfig.ParseFragment(data []byte) ([]Table, error)`: ontwerpkeuze 4. Elke `KV.Value` is de waarde zoals hij in het fragment staat, zonder spaties eromheen. Elke fout noemt het regelnummer.
  - `type packs.Contains struct { Files []string; Text string }`; `type packs.Detect struct { Files []string; Contains []Contains }`. `packs.Pack` krijgt `Detect *Detect` (nil zonder `detect.toml`) en `Config []projectconfig.Table` (nil zonder fragment).
  - `packs.Load` accepteert bovenin het pakket `detect.toml` en `lighthouse.toml.fragment`. Geen van beide wordt een eenheid. Een pathspec in `detect.toml` is niet leeg, begint niet met `/` of `:` en bevat geen `..`-deel. `text` is niet leeg. `detect.toml` heeft geen andere sleutels dan `files` en `contains`, en minstens één regel.

- [ ] **Step 1: Schrijf de falende tests.**
  - `TestParseFragment`: `"# c\n[test_gate]\ncommand = \"uv run pytest -q\"\nunchecked_exit_codes = [3, 4, 5]\n"` → `[]Table{{Name: "test_gate", Keys: []KV{{"command", `"uv run pytest -q"`}, {"unchecked_exit_codes", "[3, 4, 5]"}}}}`.
  - `TestParseFragmentRejects`, geparametriseerd, elk met het regelnummer in de fout: een sleutel vóór de eerste kop; `a = [1,\n2]`; `[t]` zonder sleutels; `[t]` twee keer; dezelfde sleutel twee keer; `[[t]]`; `a.b = 1`; `[T]` (hoofdletter); `a = ` (geen waarde).
  - `TestLoadDetectAndFragment` in `packs_test.go`: een `MapFS` met `detect.toml` (`files = ["pyproject.toml"]` en één `[[contains]]` met `files = ["**/package.json"]`, `text = "\"expo\""`) en een fragment → `p.Detect` met precies die waarden, `p.Config` gelijk aan `ParseFragment`, en geen eenheid voor een van beide bestanden.
  - `TestLoadRejectsDetect`: `files = ["/x"]`, `[":(top)x"]`, `["a/../b"]`, `[""]`, een extra sleutel `paths = []`, `files = "x"`, een `[[contains]]` zonder `text`, een leeg bestand. Elke fout noemt `detect.toml`.
  - `TestLoadRejectsFragment`: een kapot fragment → de fout noemt `lighthouse.toml.fragment` en het regelnummer.
- [ ] **Step 2: Draai** `go test ./internal/...` — verwacht: FAIL.
- [ ] **Step 3: Implementeer.** `ParseFragment` werkt per regel (kopregel of `sleutel = waarde`), decodeert elke waarde los (`toml.Decode("v = " + waarde)`), en decodeert daarna het hele fragment om dubbele tabellen en sleutels te vangen.
- [ ] **Step 4: Draai** `go test ./... && golangci-lint run` — verwacht: PASS.
- [ ] **Step 5: Commit** `feat(packs): herkenningsregel en lighthouse.toml-fragment per pakket`

### Task 3: Pakketten kiezen in `init`

**Files:**
- Create: `internal/initcmd/select.go`, `internal/initcmd/select_test.go`
- Modify: `internal/initcmd/initcmd.go` (`execute`: de vaste `[]string{"core"}` op regel 118 en de lus erna), `internal/cli/init.go`, `internal/initcmd/initcmd_test.go`

**Interfaces:**
- Consumes: `packs.Pack.Detect` (taak 2), `manifest.Manifest.Packages`.
- Produces:
  - `Options.AddPacks, Options.DropPacks []string` (vlaggen `--pack` en `--no-pack`, herhaalbaar; `StringArrayVar`).
  - `detect(root string, d *packs.Detect) (match string, ok bool, err error)`: eerst `files`, dan elke `contains`-regel; `match` is het eerste bestand dat raakt, in de volgorde van `git -C <root> ls-files -z --cached --others --exclude-standard -- ':(glob)<p>'…`. Een git-fout of een leesfout is een fout; `init` stopt dan zonder te schrijven.
  - `type choice struct { Name, Reason string }` met `Reason` `found <bestand>`, `--pack` of `""` (core of uit het manifest).
  - `choosePacks(o Options, root string, all map[string]packs.Pack, m manifest.Manifest, initialized bool) (chosen []choice, notes []string, err error)`: ontwerpkeuzes 5 en 6. `notes` gaat naar stderr.
  - `execute` laadt elk pakket in `o.Packs` (dus een kapot pakket faalt altijd, ook als het niet gekozen is), kiest, drukt de regels van ontwerpkeuze 7 op stdout, en schrijft `Packages` in het manifest in de volgorde van `chosen`. De vereisten gaan per tool samen (ontwerpkeuze 8).
- Teststub, in `initcmd_test.go`, naast `testPacks()`: `withPython(o)` voegt toe `python/detect.toml` (`files = ["pyproject.toml", "setup.py", "setup.cfg"]`), `python/lighthouse.toml.fragment` (`[test_gate]` met `command = "uv run pytest -q"` en `unchecked_exit_codes = [3, 4, 5]`), `python/hooks/hooks.json` + `python/hooks/ruff-format.sh`, `python/requires.toml` (`ruff = ["ruff-format"]`), `python/instructions/AGENTS.md`. `withShell(o)` voegt toe `shell/detect.toml` (`files = ["**/*.sh", "**/*.bash"]`) en `shell/hooks/shellcheck.sh` met een `hooks.json`.

- [ ] **Step 1: Schrijf de falende tests.**
  - `TestDetectRules` (`select_test.go`, echte git-repo's): `pyproject.toml` in de wortel → `found pyproject.toml`; alleen `sub/pyproject.toml` → geen raak; `setup.py` in `.gitignore` → geen raak; een gevolgd `ci/x.sh` → `found ci/x.sh` voor `**/*.sh`; `x.sh` in de wortel → raak voor `**/*.sh`; `apps/a/package.json` met `"expo": "~54.0.37"` → `found apps/a/package.json`; een `package.json` zonder `"expo"` of alleen onder een genegeerde `node_modules/` → geen raak; een lege repo → geen raak.
  - `TestInitProposesPythonInPythonRepo`: repo met `pyproject.toml`, `withPython` → stdout begint met `Packages: core, python (found pyproject.toml)\n`; de diff toont `+[test_gate]` en `+command = "uv run pytest -q"`; na akkoord: `.claude/hooks/lighthouse/ruff-format.sh` bestaat, `manifest.Packages == []string{"core", "python"}`, en `AGENTS.md` heeft het blok `<!-- lighthouse:begin python -->`.
  - `TestInitWithoutMarkersIsCoreOnly`: dezelfde stub zonder `pyproject.toml` → `Packages: core\n`, geen `[test_gate]`, `manifest.Packages == []string{"core"}`.
  - `TestInitPackFlag`: geen marker, `AddPacks = ["python"]` → `python (--pack)`. `TestInitNoPackFlag`: marker, `DropPacks = ["python"]` → alleen core.
  - `TestNoPackCoreFails`: `DropPacks = ["core"]` → rc 1, `core cannot be left out`, niets geschreven. `TestUnknownPackFails`: `AddPacks = ["rust"]` → rc 1, `package rust is not in this binary`.
  - `TestUpgradeSuggestsFittingPack`: `init` zonder marker, dan `pyproject.toml` erbij, dan `--upgrade` → stdout exact `No changes.\n`, stderr bevat `package python fits this repository (found pyproject.toml); add it with: lighthouse init --upgrade --pack python`, manifest ongewijzigd.
  - `TestUpgradeAddsPack`: daarna `--upgrade --pack python --yes` → stdout begint met `Adding package python.`; de hook staat er; een volgende `--upgrade` geeft `No changes.`.
  - `TestUpgradeDropsPack`: `init` met python, dan `--upgrade --no-pack python --yes` → `Removing package python.`; de hook, de registratie in `.claude/settings.json` en het blok in `AGENTS.md` zijn weg; `[test_gate]` staat nog in `lighthouse.toml`; stderr bevat `lighthouse.toml: the keys package python added stay; remove them by hand if you no longer want them`.
  - `TestNoPackRefusesLocalEdit`: als hierboven, maar `ruff-format.sh` lokaal gewijzigd → rc 1, `conflict:` op stdout, het bestand staat er nog; met `Force` → weg.
  - `TestPackOrderIsCoreThenAlphabetical`: `withPython` en `withShell`, beide markers → `Packages: core, python (…), shell (…)`, en in `.claude/settings.json` staan de registraties van python vóór die van shell.
  - `TestRequiresMergedPerTool`: core en python vragen allebei `jq`, `jq` ontbreekt → precies één regel `missing dependency: jq (needed by: main-commit-guard, ruff-format)`.
  - `TestBrokenUnchosenPackFails`: een `rust/`-map met een onbekend bestand, niet gekozen → rc 1, de fout noemt `package rust`.
  - Bestaande tests: `TestUpgradeRightAfterInitIsEmpty` en de andere `No changes.`-tests blijven ongewijzigd groen; de verse-init-tests die stdout exact vergelijken, krijgen de regel `Packages: core`.
- [ ] **Step 2: Draai** `go test ./internal/...` — verwacht: FAIL.
- [ ] **Step 3: Implementeer.** In `internal/cli/init.go`: `--pack` en `--no-pack` met de help `add a package (repeatable)` en `leave a package out (repeatable)`, en een zin erover in `Long`.
- [ ] **Step 4: Draai** `go test ./... && go test -tags e2e ./e2e/ && golangci-lint run` — verwacht: PASS.
- [ ] **Step 5: Commit** `feat(init): taalpakketten herkennen en kiezen, met --pack en --no-pack`

### Task 4: Het fragment in `lighthouse.toml`

**Files:**
- Modify: `internal/initcmd/initcmd.go` (`proposeConfig`, `planConfig`), `internal/initcmd/workflows.go` (`pythonLookup`), `internal/initcmd/initcmd_test.go`
- Create: `tests/test_fragments.py`

**Interfaces:**
- Consumes: `packs.Pack.Config` (taak 2), de gekozen pakketten (taak 3).
- Produces: `mergeTables(core []projectconfig.Table, packs []packs.Pack) ([]projectconfig.Table, error)`: de kerntabellen eerst, dan de fragmenten in pakketvolgorde, samengevoegd per tabelnaam. Een sleutel in twee pakketten (of in een pakket en de kerntabellen) geeft `lighthouse.toml: <tabel>.<sleutel> comes from both package <a> and package <b>`. `proposeConfig(root string, old []byte, tables []projectconfig.Table)`. `pythonLookup` krijgt een veld `tables` en gebruikt dezelfde `proposeConfig`.

- [ ] **Step 1: Schrijf de falende tests.**
  - `TestFragmentKeepsProjectKeys`: `lighthouse.toml` met `[test_gate]\ncommand = "make test"  # own\n`, `withPython`, `pyproject.toml` → na `init` staat `command = "make test"  # own` er byte voor byte, en `unchecked_exit_codes = [3, 4, 5]` staat direct eronder; geen tweede `[test_gate]`.
  - `TestFragmentAppendsTable`: geen `[test_gate]` → het fragment staat als laatste tabel, na `[project]`.
  - `TestFragmentKeyInTwoPacks`: twee stubpakketten die allebei `test_gate.command` leveren → rc 1, de fout noemt beide pakketten en `test_gate.command`, niets geschreven.
  - `TestUpgradeRestoresFragmentKey`: na `init` met python haalt het project `unchecked_exit_codes` weg; `--upgrade` stelt hem opnieuw voor. Dat is hetzelfde gedrag als bij `[project] language` (spec, *init* punt 2: ontbrekende sleutels aanvullen).
  - `TestDefaultLookupSeesFragment` (met `python3`, zoals `TestDefaultLookupReadsProposedConfig`): de echte `config.py`, `withPython`, een core-workflow `cmd: @@test_gate.command@@` → `cmd: uv run pytest -q`.
  - `tests/test_fragments.py`, geparametriseerd over elk `pack/*/lighthouse.toml.fragment` (in c1 nog geen: de test slaat dan over met de reden `no package has a fragment yet`): `test_fragment_is_valid_config`, waarin `config.load` op `[project]\nlanguage = "en"\n` plus het fragment geen `ConfigError` geeft, en `test_fragment_keys_are_known`, waarin `config.is_known_key` elke sleutel kent.
- [ ] **Step 2: Draai** `go test ./internal/... && uv run pytest tests/test_fragments.py -q` — verwacht: FAIL.
- [ ] **Step 3: Implementeer.**
- [ ] **Step 4: Draai** `uv run pytest -q && go test ./... && go test -tags e2e ./e2e/ && golangci-lint run && ci/literal-grep.sh` — verwacht: PASS.
- [ ] **Step 5: Commit** `feat(init): lighthouse.toml-fragment van een pakket invoegen zonder herschrijven`. Daarna de review van de hele branch (opdracht, stap 3) en de vraag aan Corneel vóór het openen van PR c1.

---

## Pakket-PR's: werkwijze

Elke taak hieronder volgt het verhuisrecept van (b) (V1–V7), met `pack/<pakket>` voor `pack/core` en `chore(<pakket>): …` / `feat(<pakket>): …` als commit-scope. Tests komen in `tests/pack/<pakket>/`. `tests/pack/_testkit.py` krijgt `install_pack(name, root)`, naar het voorbeeld van `install_core`, en installeert core plus het pakket. Elke pakket-PR voegt het pakket toe aan een tabel `TestLanguagePacks` in `internal/packs/` (naast `TestCorePack`): `Load` geeft geen fout en noemt de verwachte eenheden.

## PR c2 — python (`issue-8-c2-python`)

### Task 5: `python-writing`

**Files:**
- Create (V1): `pack/python/skills/python-writing/claude/SKILL.md` en `agents/SKILL.md` (uit `paia-python-schrijven`), `tests/pack/python/test_python_writing.py` (uit `test_instructielagen.py:430-497`)

- [ ] **Step 1–2: V1, V2.** Nieuw: `test_kpi_commands_from_config` (de skill noemt `config.py test_gate.command` en `config.py python.source_dirs`, geen `src/` en geen `tests/unit` letterlijk); `test_ratchet_by_path` (`.lighthouse/scripts/quality/ratchet.py`); `test_complexity_heading_matches_ratchet` (de kop die `ratchet.py` als `COMPLEXITY_SKILL` noemt, staat letterlijk in de skill); `test_variants_keep_their_difference` (alleen `agents/SKILL.md` heeft het OpenCode-blok).
- [ ] **Step 3: V3–V6.** Inventaris (c), python, rij `paia-python-schrijven`. `paia-shell-gates` → `/lighthouse-shell-gates` "if present".
- [ ] **Step 4: V7.** Commit `feat(python): skill python-writing, vertaald`

### Task 6: `ruff-format` en `ty-check`

**Files:**
- Create (V1): `pack/python/hooks/ruff-format.sh`, `pack/python/hooks/ty-check.sh`, `pack/python/hooks/hooks.json`, `pack/python/requires.toml`, `tests/pack/python/test_ruff_format.py` en `test_ty_check.py` (uit `test_quality_hooks.py:54-292`)
- Modify: `pack/core/lib/config.py` (`python.source_dirs`), `tests/pack/test_config.py`

**Interfaces:**
- Consumes: `bash_writes.GATES`, `GATE_PACKS` en `classify` uit taak 11 van (b); de `install_*`-indeling.
- Produces: `hooks.json` met `PostToolUse` `Edit|Write` → `ruff-format.sh` (timeout 15) en `Stop` → `ty-check.sh` (timeout 60). `requires.toml`: `jq = ["ruff-format", "ty-check"]`, `uv = ["ruff-format", "ty-check", "quality/ratchet.py", "test_gate"]`.

- [ ] **Step 1–2: V1, V2.** Nieuw:
  - `test_ruff_unchecked_without_tool`: geen `uvx` en geen `ruff` → rc 2, eerste regel `ruff-format: COULD NOT CHECK — …` (keert `fails_open_when_tool_unresolvable` om; ontwerpkeuze 13).
  - `test_ruff_unchecked_on_invalid_json`, `test_ruff_unchecked_without_jq`: idem.
  - `test_ruff_finding_first_line`: een bevinding begint met `ruff-format:` en is voor `bash_writes.classify` een `finding`; pass 1 zet niets op stdout.
  - `test_ty_reads_source_dirs`: `source_dirs = ["lib"]` → ty draait op `lib`.
  - `test_ty_no_source_dir_is_loud` en `test_ty_empty_source_dirs_is_silent` (ontwerpkeuze 13).
  - `test_bash_write_through_gate`: met core en python geïnstalleerd geeft een Bash-write van een `.py` met `except:` een `finding` via `bash-write-gates post`, en niet `no python gate installed`.
  - Gaat weg (paia-eigen): `test_blocking_rules_are_selected_in_pyproject`, `test_pgh_rules_are_selected`, `test_ty_pin_matches_hook`, `test_ty_hook_is_green_on_this_repo`. Noteer ze voor (d).
- [ ] **Step 3: V3–V6.** Pass 1 met `>/dev/null 2>&1`, zodat de eerste regel van de gate is. De meldingen "Lint-gate"/"Type-gate" worden `ruff-format:`/`ty-check:` + Engels.
- [ ] **Step 4: V7.** Commit `feat(python): hooks ruff-format en ty-check, zonder stille uitgangen`

### Task 7: De kwaliteitsratel

Voorwaarde: het antwoord op ontwerpkeuze 11. Hieronder staat het voorstel.

**Files:**
- Create (V1): `pack/python/scripts/quality/{__init__,baseline,diffcov,gitenv,ratchet}.py`, `tests/pack/python/test_ratchet.py` (uit `test_quality_ratchet.py`, zonder de nightly-gevallen), `tests/pack/python/test_ratchet_integration.py` (het ratchet-deel van `test_quality_ratchet.sh`, als pytest)
- Modify: `pack/core/lib/config.py` (`python.lint_dirs`, `python.coverage_command`), `tests/pack/test_config.py`

**Interfaces:**
- Produces: `python3 .lighthouse/scripts/quality/ratchet.py ruff|coverage [--base REF]|show|tighten`. Exitcodes zoals op `SRC`. De regel `<regel>: <a> -> <b>` en de markering voor een harde grens zijn interne contracten en worden samen vertaald.

- [ ] **Step 1–2: V1, V2.** De tests die paia's boom lezen, krijgen een eigen fixture-repo (`GOOD` zonder `src/web/pages/`; geen echte `quality-baseline.toml`). Nieuw: `test_dirs_from_config` (`source_dirs = ["lib"]`, `lint_dirs = ["lib", "t"]` → ruff, complexipy en `--cov` op die mappen); `test_coverage_command_from_config`; `test_base_follows_default_branch` (`default_branch = "trunk"` → `origin/trunk`); `test_complexity_skill_path` (`.claude/skills/lighthouse-python-writing/SKILL.md`); `test_no_nightly_subcommand`.
- [ ] **Step 3: V3–V6.** Ontwerpkeuze 11 en 12. `test_quality_ratchet.sh` slaat zonder `uv` niet stil over: de pytest-versie gebruikt `pytest.skip` met reden, en de CI heeft `uv`.
- [ ] **Step 4: V7.** Commit `feat(python): kwaliteitsratel ruff, coverage, show en tighten`

### Task 8: Herkenning, fragment en de AC

**Files:**
- Create: `pack/python/detect.toml`, `pack/python/lighthouse.toml.fragment`, `pack/python/instructions/AGENTS.md`
- Modify: `internal/packs/core_test.go` (`TestLanguagePacks`), `internal/initcmd/initcmd_test.go`, `e2e/e2e_test.go`, `pack/SOURCES.toml`

**Interfaces:**
- Produces: `detect.toml` = `files = ["pyproject.toml", "setup.py", "setup.cfg"]`. Het fragment, letterlijk:
  ```toml
  [test_gate]
  command = "uv run pytest -q"
  unchecked_exit_codes = [3, 4, 5]
  ```
  Het blok in `AGENTS.md`, ten hoogste 80 woorden: Python-code volgt `/lighthouse-python-writing`; de gates `ruff-format` en `ty-check`; de ratel vóór een PR.

- [ ] **Step 1: Schrijf de falende tests.**
  - `TestInitProposesRealPython` (`initcmd_test.go`, `o.Packs = nil`): repo met `pyproject.toml` → `Packages: core, python (found pyproject.toml)`, en `lighthouse.toml` krijgt het fragment hierboven.
  - `TestRedTestBlocksCommit` (`e2e_test.go`): een verse repo met `pyproject.toml` (`pytest` als dev-dependency) en een falende test, `init --yes`, featurebranch. De PreToolUse-payload voor `git commit` naar het commando uit `.claude/settings.json` (zoals de DoD-test van (a)) → exit 2 en de melding dat de tests falen. Na het repareren van de test → exit 0. Zonder `uv` slaat de test over met reden; de CI heeft `uv`.
  - `tests/test_fragments.py` draait nu over `pack/python/lighthouse.toml.fragment` en is groen.
- [ ] **Step 2: Draai** `go test ./... && go test -tags e2e ./e2e/ && uv run pytest -q` — verwacht: FAIL.
- [ ] **Step 3: Schrijf de drie bestanden en de `SOURCES.toml`-entries** (`new = true` voor alle drie).
- [ ] **Step 4: Draai** de volledige set van de opdracht — verwacht: groen.
- [ ] **Step 5: Commit** `feat(python): herkenning, default [test_gate] en instructieblok`. Open PR c2.

## PR c3 — shell (`issue-8-c3-shell`)

### Task 9: `shellcheck` en `shellcheck-repo.sh`

**Files:**
- Create (V1): `pack/shell/hooks/shellcheck.sh`, `pack/shell/hooks/hooks.json`, `pack/shell/scripts/shellcheck-repo.sh`, `pack/shell/scripts/lib/shellcheck-bin.sh`, `pack/shell/requires.toml`, `tests/pack/shell/test_shellcheck.py` (uit `test_quality_hooks.py:518-828`)
- Modify: `pack/core/lib/config.py` (`shell.strict_dirs`), `tests/pack/test_config.py`

**Interfaces:**
- Produces: `hooks.json` met `PostToolUse` `Edit|Write` → `shellcheck.sh`, timeout 60. `requires.toml`: `jq = ["shellcheck"]`, `curl = ["shellcheck (download of the pinned binary)"]`. `shellcheck-repo.sh [--severity S] [-- files…]` met exitcontract 0/1/2/4.

- [ ] **Step 1–2: V1, V2.** Nieuw: `test_unchecked_first_line` (geen `shellcheck-repo.sh` geïnstalleerd → rc 2, eerste regel `shellcheck: COULD NOT CHECK — …`, en `bash_writes.classify` zegt `unchecked`); `test_strict_dirs_from_config` (`strict_dirs = ["ops"]` → `style` in `ops/`, `warning` erbuiten); `test_strict_default_covers_lighthouse_hooks`; `test_cache_dir_is_lighthouse_tools`; `test_bash_write_through_gate` (zoals in taak 6, voor `.sh`). Gaan weg (paia-eigen): `test_shellcheck_pin_matches_workflow_audit`, `test_shellcheck_gate_set_is_clean`, `test_every_hook_script_is_registered_in_settings`, `test_no_gate_shell_is_gitignored`.
- [ ] **Step 3: V3–V6.** Inventaris (c), shell. De opzoekvolgorde van het script in `shellcheck.sh` (59-72) wordt: `LIGHTHOUSE_REPO_ROOT`, anders de git-root van het bestand; daarin `.lighthouse/scripts/shellcheck-repo.sh`.
- [ ] **Step 4: V7.** Commit `feat(shell): shellcheck-gate en shellcheck-repo.sh`

### Task 10: `shell-gates`, herkenning en blok

**Files:**
- Create (V1): `pack/shell/skills/shell-gates/claude/SKILL.md` en `agents/SKILL.md`; `pack/shell/detect.toml` (`files = ["**/*.sh", "**/*.bash"]`); `pack/shell/instructions/AGENTS.md` (≤ 60 woorden); `tests/pack/shell/test_shell_gates.py`
- Modify: `internal/packs/core_test.go` (`TestLanguagePacks`), `internal/initcmd/initcmd_test.go`, `pack/SOURCES.toml`

- [ ] **Step 1–2: V1, V2.** Nieuw: `test_strict_dirs_from_config` (de skill noemt `config.py shell.strict_dirs`, niet `scripts/hooks` of `tests/integration`); `test_variants_keep_their_difference`; `TestInitProposesRealShell` (`o.Packs = nil`, een gevolgd `x.sh` → `shell (found x.sh)`).
- [ ] **Step 3: V3–V6.**
- [ ] **Step 4: V7.** Commit `feat(shell): skill shell-gates, herkenning en instructieblok`. Open PR c3.

## PR c4 — expo (`issue-8-c4-expo`)

### Task 11: `refresh-expo-lockfiles`

**Files:**
- Create (V1): `pack/expo/skills/refresh-expo-lockfiles/SKILL.md`, `pack/expo/detect.toml`, `tests/pack/expo/test_refresh_expo_lockfiles.py`
- Modify: `internal/packs/core_test.go` (`TestLanguagePacks`), `internal/initcmd/initcmd_test.go`, `pack/SOURCES.toml`

**Interfaces:**
- Produces: `detect.toml`:
  ```toml
  [[contains]]
  files = ["**/package.json"]
  text = "\"expo\""
  ```
  Geen fragment, geen blok, geen `requires.toml`: de skill noemt `node` en `npm` en meldt zelf als ze ontbreken.

- [ ] **Step 1–2: V1, V2.** Nieuw: `test_finds_apps_itself` (de skill noemt geen appnaam en geen `apps/`; hij zoekt `package.json` met `"expo"` via `git ls-files`); `test_optional_refs_guarded` (`apps.yml`, `dependabot.yml`, `upgrading-expo` staan in een alinea met `if present`); `test_write_issue_link` (`/lighthouse-write-issue`, het label via `config.py labels.area_prefix`); `TestInitProposesRealExpo` (`apps/a/package.json` met `"expo"` → `expo (found apps/a/package.json)`).
- [ ] **Step 3: V3–V6.** Inventaris (c), expo.
- [ ] **Step 4: V7.** Commit `feat(expo): skill refresh-expo-lockfiles en herkenning`. Open PR c4; is hij de laatste van de drie, dan met `Closes #8`.

---

## Na de laatste taak

- Zet in #8 onder *Verbeterpunten*: shell-scripts zonder extensie (alleen een shebang) controleert geen gate; `shellcheck-repo.sh:77` maakt van elke shellcheck-fout een bevinding; `sha256sum` ontbreekt op macOS, dus de download van shellcheck valt daar terug op PATH; `expo` kent alleen `npm`; de stille terugval van `prcov` op 24 uur (als ontwerpkeuze 11 hem meeneemt); de punten die tijdens de uitvoering opkomen.
- Zet in het issue van (d): paia's `[shell] strict_dirs = ["scripts/hooks", "tests/integration", ".claude/hooks"]`, `[python] coverage_command` met paia's markers, de tests en bestanden van ontwerpkeuze 14, de importeurs van `quality.*` (`deep_sweep.py`, `opencode-lane`), en wat ontwerpkeuze 11 in paia laat.
