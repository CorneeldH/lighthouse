# Afronden zonder Python — implementatieplan (#285)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Doel:** De laatste Python van `pack/core` verdwijnt (`lib/*.py`, `scripts/create_labels.py`),
`init --upgrade` ruimt de geïnstalleerde kopieën op, en een CI-job bewijst dat een verse `init`
met `core`, `go`, `node`, `shell` en `expo` zonder `python3` werkt.

**Architectuur:** `create_labels.py` is de enige Python-afnemer van de bevroren lib die nog
geïnstalleerd wordt; hij wordt een Go-pakket `internal/labels`, dat `init` direct aanroept.
`ty-gate.sh` (`pack/python`) leest zijn config voortaan met `lighthouse config`. De pytest-tests
die `lighthouse_lib.config` importeren om sleutels te toetsen, vragen het voortaan aan de binary
via een verborgen vlag `lighthouse config --kind`. Daarna gaat `pack/core/lib/` weg, en de
bestaande manifest-route (`reconcile.Remove`) ruimt de installatie op. Het bewijs is een
bash-script `ci/no-python.sh` naar het patroon van `ci/windows-measure.sh`, met vaste payloads
onder `tests/pack/fixtures/hooks/`.

**Tech stack:** Go (cobra, `os/exec`), bash + `jq`, GitHub Actions, pytest.

**Spec:** [`2026-11-07-zonder-python-design.md`](../specs/2026-11-07-zonder-python-design.md),
§5, §7, §8, §10, *Anti-doelen*, *Opdeling in issues* stap 12, *Bewijs* en *Gevolgen voor M3*.

## Global Constraints

- Geen ander gedrag (spec, *Niet-doelen*): het label-commando geeft dezelfde regels en exitcodes
  als `create_labels.py` (exit 0 bij elke `gh`-uitkomst, exit 2 bij een ontbrekend of leeg
  taxonomiebestand of kapotte config; nooit `--force`; `gh` met stdin dicht en
  `GH_PROMPT_DISABLED=1`).
- Letterlijke regels van het label-commando: `labels: all <n> present on <repo>`,
  `labels: created <name>`, `labels: missing on <repo>; create them with:`,
  `labels: could not read the labels of <repo|the repository> (<reason>); create the missing ones with:`,
  `labels: ERROR — <reden>` (stderr), en per ontbrekend label één `gh label create <name> [--repo <repo>] --color <HEX>`
  in shell-quoting (`shlex.join`-equivalent: alleen quoten wat het nodig heeft, met `'…'`).
- `pack/python` blijft Python; alleen zijn aanroep van de core-lib (`ty-gate.sh`) gaat naar de
  binary (milestone, *No-Gos*). Python in `tests/`, `tools/`, `ci/` blijft.
- De bash-hooks van de taalpakketten blijven bash.
- Een hook die niet kan checken, zegt `COULD NOT CHECK` (*Anti-doelen*).
- Code, namen en commentaar Engels; artefacten (commits, PR, changelog, docs) Nederlands.
- Go volgt `/lighthouse-go-standard`, tests `/lighthouse-test-standard`.
- Elke wijziging onder `pack/` of aan wat `init` schrijft: `go run ./cmd/lighthouse init --upgrade`
  draaien en de geïnstalleerde kopie meecommitten (AGENTS.md, *Dogfood*).
- Verificatie per task: `go test -shuffle=on ./...` en de genoemde pytest-bestanden met
  `uv run pytest -q <pad>`.

## Review Focus

1. **Upgrade van een repo met een lokaal gewijzigde `config.py`.** §8: het bestand blijft staan
   en wordt gemeld; de rest van de lib verdwijnt. Test in Task 4.
2. **Lege mappen na het opruimen.** `.lighthouse/lib/lighthouse_lib/` en `__pycache__` mogen niet
   achterblijven als alles weg is; `.lighthouse/scripts/` blijft zolang `shell` of `python` er iets
   heeft. Test in Task 4.
3. **`[test_gate] command` dat nog `lighthouse_lib` aanroept.** Na de upgrade is de lib weg; `init`
   moet zeggen dat de gate breekt, niet dat hij "phasing out" is. Test in Task 4.
4. **Label-taxonomie met een label dat al bestaat in andere hoofdletters of met een spatie.** Geen
   dubbele `gh label create`. Tests in Task 1 (geport uit `test_create_labels.py`).
5. **Een nieuwe hookregistratie zonder payload-fixture.** De CI-job moet dan rood worden, niet de
   registratie overslaan. Test in Task 5.

---

### Task 1: `internal/labels` vervangt `create_labels.py`

**Files:**
- Create: `internal/labels/labels.go`, `internal/labels/labels_test.go`
- Modify: `internal/initcmd/initcmd.go:53,234-236` (default van `SyncLabels`), `internal/initcmd/labelsync.go` (weg; `isolatedEnv` heeft geen andere gebruiker)
- Delete: `pack/core/scripts/create_labels.py`, `tests/pack/test_create_labels.py`
- Modify: `pack/SOURCES.toml` (entry `pack/core/scripts/create_labels.py` weg), `internal/packs/core_test.go:83`
- Modify: `internal/initcmd/testdata/places.golden:11` (de rij `.lighthouse/scripts/` verliest `core`; regenereer met `-update`), `internal/initcmd/labels_test.go` (de `ghStub`-helper slaat nu over zonder `python3`: die skip weg, de stub wordt een `runGH`-nep)
- Modify: `docs/site/data/config.json:155,458` (lezer van `paths.labels` en `forge.repo` wordt `internal/labels/labels.go` in plaats van `create_labels.py`)

**Interfaces:**
- Produces: `labels.Label{Name, Color string}`; `labels.ParseTaxonomy(text string) []labels.Label`;
  `labels.Sync(root string, create bool, out, errw io.Writer) error` — exact de signatuur van
  `Options.SyncLabels`, zodat `initcmd` hem als default zet. Exit-2-gevallen komen terug als error
  waarvan de tekst al op `errw` staat (zoals nu de non-zero exit van het script). `initcmd` print bij die
  error alleen een korte vaste regel (`labels: not synchronised`), niet nog eens de tekst: anders staat
  de melding dubbel op stderr (`initcmd.go:237-239`).
- `gh` wordt aangeroepen via een pakketvariabele `var runGH = func(args []string) (stdout string, err error)`
  zodat tests een nep zetten; productie: `exec.Command("gh", …)` met `Stdin = nil`,
  `GH_PROMPT_DISABLED=1` in de env, en als reden de laatste stderr-regel of `gh exited <n>`.
- Config uit `internal/config` (de Go-lezer van `lighthouse.toml` in `root`): `paths.labels`
  (default `docs/developer/labels.md`) en `forge.repo`.

- [ ] **Step 1: Port de tests.** Elke `test_*` uit `tests/pack/test_create_labels.py` wordt een
  Go-test met dezelfde input en dezelfde verwachte regels: `TestParseTemplate`,
  `TestListsMissingWithoutCreate`, `TestCreateMakesOnlyMissing`,
  `TestExistingLabelCaseInsensitive`, `TestAllPresent`, `TestReadsLabelsPaginated`,
  `TestGHMissingPrintsAllLines`, `TestCreateFailurePrintsLine`, `TestNoForgeRepoPrintsAllLines`,
  `TestMissingTaxonomyIsError`, `TestRemoteLabelWithSpace`,
  `TestParseTaxonomyAcceptsDutchAndAreaHeaders`. De taxonomie-fixtures en verwachte stdout neem je
  letterlijk over uit het Python-bestand.
- [ ] **Step 2:** `go test ./internal/labels/` → FAIL (pakket bestaat niet).
- [ ] **Step 3:** Implementeer `ParseTaxonomy` en `Sync` volgens de docstring van
  `create_labels.py` (kolomkoppen `label`/`area` en `color`/`colour`/`kleur`, hoofdletterongevoelig;
  alleen regels die met `|` beginnen; scheidingsregel overslaan; kleur als zes hex in backticks,
  uitvoer in hoofdletters). Zet in `initcmd.go` de default van `SyncLabels` op `labels.Sync` en
  pas het veldcommentaar aan.
- [ ] **Step 4:** `go test -shuffle=on ./internal/labels/ ./internal/initcmd/` → PASS.
  Mutatiecheck: maak de naamvergelijking hoofdlettergevoelig → `TestExistingLabelCaseInsensitive` faalt.
- [ ] **Step 5:** Verwijder script, Python-test, SOURCES-entry en de regel in `core_test.go`.
  `go run ./cmd/lighthouse init --upgrade` → `.lighthouse/scripts/create_labels.py` verdwijnt.
  `go test -shuffle=on ./...` → PASS. Pas `docs/site/data/config.json` aan en draai
  `uv run pytest -q tests/test_site_config.py` → PASS.
- [ ] **Step 6: Commit** — `feat(labels): labels synchroniseren in Go, create_labels.py weg (#285)`.

### Task 2: sleutels toetsen via de binary in de pytest-tests

**Files:**
- Modify: `internal/config/config.go` (nieuw `KindOf` en `SchemaDump`), `internal/cli/config.go` (verborgen vlaggen `--kind` en `--schema`), `internal/config/config_test.go`, `internal/cli/config_test.go`
- Modify: `tests/test_site_config.py:17,39,98,477,481` en `tests/test_agent_guide.py:104-110`: ze laden nu `pack/core/lib/config.py` voor `SCHEMA`, `DEFAULTS`, `_BASE_KEYS`, `HOST_KEYS`, `_OPTION_PREFIXES`, `_BOOL` en `LANGUAGES`; ze lezen die voortaan uit `lighthouse config --schema` (via `_testkit.config_schema()`), en `SKIPPED_READERS` verliest de regel `pack/core/lib/config.py`
- Modify: `tests/pack/_testkit.py` (helpers), `tests/pack/expo/test_refresh_expo_lockfiles.py:8,55`, `tests/pack/go/test_go_writing.py:11,147`, `tests/pack/python/test_python_writing.py:12,95`, `tests/pack/shell/test_shell_gates.py:11,39`, `tests/pack/test_fragments.py:9,32-47`, `tests/pack/test_skill_contracts.py:27,196`, `tests/pack/test_workflow_unblock.py:19,253-254`, `tests/test_dogfood.py:181`

**Interfaces:**
- Produces Go: `config.KindOf(key string) string` — de schema-kind (`"str"`, `"list[str]"`, …)
  voor een sleutel uit `schema`; `"known"` voor een andere sleutel waarvoor `IsKnownKey` waar is;
  `"unknown"` anders.
- Produces Go: `config.SchemaDump() map[string]any` — `schema` (sleutel → kind), `defaults`, `base_keys`,
  `host_keys`, `option_prefixes`, `languages`, uit dezelfde variabelen als `KindOf`.
- Produces CLI: `lighthouse config --schema` (verborgen): `SchemaDump` als JSON op stdout, exit 0, leest
  geen `lighthouse.toml`.
- Produces CLI: `lighthouse config --kind <key>...` (cobra `MarkHidden`): per argument één regel
  `<key>\t<kind>` op stdout, exit 0; leest geen `lighthouse.toml`.
- Produces Python (`tests/pack/_testkit.py`): `config_kinds(keys: Iterable[str]) -> dict[str, str]`
  (één binary-aanroep voor alle sleutels, via `lighthouse_dir()` uit `tests/pack/_testkit.py:364`) en
  `is_known_key(key: str) -> bool` (`config_kinds([key])[key] != "unknown"`) en
  `config_schema() -> types.SimpleNamespace` (velden `SCHEMA`, `DEFAULTS`, `BASE_KEYS`, `HOST_KEYS`,
  `OPTION_PREFIXES`, `LANGUAGES`; `BOOL = "bool"`).

- [ ] **Step 1: Failing tests.** Go: `TestKindOf` met `forge.default_branch` → `"str"`,
  `paths.search_dirs` → `"list[str]"`, `no.such_key` → `"unknown"`, een sleutel met een
  `optionPrefixes`-prefix → `"known"`. CLI: `TestConfigKind` — `--kind ci.runs_on no.such_key`
  geeft `"ci.runs_on\tstr\nno.such_key\tunknown\n"`, exit 0, ook zonder `lighthouse.toml`.
  `TestConfigSchema`: `--schema` geeft geldige JSON met `languages == ["nl","en"]` en `schema["ci.runs_on"] == "str"`.
- [ ] **Step 2:** `go test ./internal/config/ ./internal/cli/` → FAIL.
- [ ] **Step 3:** Implementeer `KindOf`, `SchemaDump` en de vlaggen.
- [ ] **Step 4:** `go test -shuffle=on ./internal/config/ ./internal/cli/` → PASS.
- [ ] **Step 5:** Zet de tests om. `config.is_known_key` → `_testkit.is_known_key`;
  `config.SCHEMA[key] == "str"` → `config_kinds(keys)[key] == "str"`. `test_fragments.py`:
  `cfgmod.load(path)` wordt `lighthouse config project.language` met `LIGHTHOUSE_TOML=<path>`,
  waarbij exit 2 de `ConfigError` vervangt (`TestConfigInvalidFileExitsTwo` dekt die exit al);
  `test_check_catches_a_wrong_type` verwacht dan exit 2. `test_dogfood.py:181`:
  `["lighthouse", "config", "test_gate.command"]` met de binary van `lighthouse_dir()`, zonder
  `PYTHONPATH`. `test_site_config.py` en `test_agent_guide.py`: `config.X` wordt `config_schema().X`.
- [ ] **Step 6:** `uv run pytest -q tests/pack/expo tests/pack/go tests/pack/python/test_python_writing.py tests/pack/shell/test_shell_gates.py tests/pack/test_fragments.py tests/pack/test_skill_contracts.py tests/pack/test_workflow_unblock.py tests/test_dogfood.py tests/test_site_config.py tests/test_agent_guide.py` → PASS.
  Mutatiecheck: zet in één SKILL.md een verzonnen sleutel `lighthouse config forge.nope` →
  `test_config_keys_known` faalt; terugdraaien.
- [ ] **Step 7: Commit** — `test(config): sleutels toetsen via lighthouse config --kind (#285)`.

### Task 3: `ty-gate.sh` leest zijn config met de binary

**Files:**
- Modify: `pack/python/hooks/ty-gate.sh:19,50,65-73`, `pack/python/requires.toml:6`
- Test: `tests/pack/python/test_ty_gate.py`

**Interfaces:**
- Consumes: `lighthouse config python.source_dirs` (één map per regel; exit 1 = niet gezet, exit 2
  = kapotte config; default `["src"]`).

- [ ] **Step 1: Failing test.** In de ty-gate-tests: een test die de hook draait met een PATH
  zonder `python3` (een map met symlinks naar alleen `bash`, `jq`, `git`, `lighthouse`, `uvx` en
  coreutils) en een geldige payload, en asserteert dat de uitvoer geen `COULD NOT CHECK` bevat.
- [ ] **Step 2:** draai die test → FAIL (`python3 is missing`).
- [ ] **Step 3:** Vervang de `config.py`-aanroep door
  `DIRS_RAW=$(LIGHTHOUSE_REPO_ROOT="$LIB_ROOT" lighthouse config python.source_dirs 2>&1)`; exit 1
  (niet gezet) behandel je als lege lijst, elke andere non-zero als `cannot_check` met dezelfde
  tekst als nu. Haal `python3` uit de tool-lus en uit het commentaar van regel 19, en `LIB`
  weg. In `requires.toml`: `python3 = ["python-standard", "gate_python"]`.
- [ ] **Step 4:** alle ty-gate-tests → PASS. Mutatiecheck: zet `python3` terug in de tool-lus →
  de nieuwe test faalt.
- [ ] **Step 5:** `go run ./cmd/lighthouse init --upgrade` (geen wijziging verwacht: deze repo
  heeft `pack/python` niet — controleer met `git status`).
- [ ] **Step 6: Commit** — `fix(python): ty-gate leest python.source_dirs met lighthouse config (#285)`.

### Task 4: `pack/core/lib` weg en de upgrade ruimt op

**Files:**
- Delete: `pack/core/lib/` (vier bestanden), `.lighthouse/lib/` (via `init --upgrade`)
- Modify: `pack/core/requires.toml:7,9` (regels `python3` en `tomllib` weg), `pack/SOURCES.toml:23,27,35,44` (alle vier de `lib`-entries)
- Modify: `internal/packs/packs.go:8-9,315-316` (`case "lib"` weg; een pakket met `lib/` geeft
  dan `unknown file`). `installDirs` (:160) **houdt** `.lighthouse/lib/lighthouse_lib/`: een oude
  manifest noemt die paden nog, en `IsInstallPath` moet ze kunnen lezen om ze weg te halen.
  Commentaar erbij dat het pad alleen nog voor opruimen bestaat.
- Modify: `internal/packs/core_test.go:79-82,95` (lib-bestanden weg; tools-string zonder
  `python3` en `tomllib`), `internal/packs/packs_test.go:45,189`, `internal/initcmd/testdata/places.golden:10`, `internal/initcmd/places_test.go:28,52`
- Modify: `internal/initcmd/gatecommand.go:94-98` en `gatecommand_test.go:100-111`
- Delete: `tests/pack/test_config.py`, `tests/pack/test_datadir.py`, `tests/pack/test_headings.py`,
  `tests/pack/test_hook_lib.py`; in `tests/pack/test_config_fixtures.py` alleen de Python-helft
  (de fixtures blijven: `internal/config/parity_test.go` en `headings_parity_test.go` lezen ze) —
  is er niets meer over, dan het bestand weg.
- Modify: `tests/pack/_testkit.py:544,628-632` (lib uit `_INSTALL_DIRS`, `INSTALLED_LIB`,
  `IMPORT_ROOT` weg), `tests/pack/conftest.py:8,12`, `tests/pack/test_skill_scripts_gone.py:35`
  (`KEPT` leeg: alle core-lib-bestanden horen weg), `tests/pack/test_skill_contracts.py:338,374-379`
  (`tomllib`-patroon: geen core-unit noemt het nog)
- Modify: `tools/hooks/checkpoint_scan.py:74,148` en `tests/test_checkpoint_scan.py:264-275`: `load_limits()` importeert `lighthouse_lib.config` (`host_path`, `load_host`) vanaf `.lighthouse/lib`; dat wordt `lighthouse config host.scan_memory_max`, `host.scan_cpu_quota` en `host.gate_slice` (exit 1 = niet gezet → default; exit 2 of een ontbrekende binary → `CouldNotCheck`). De `sys.path.insert` weg. De tests zetten `LIGHTHOUSE_HOST_TOML` zoals nu en gebruiken de binary van `lighthouse_dir()`.
- Modify: `tests/test_site_repo.py:153-154` en `docs/site/data/repo.json:92-98` (rij `lighthouse-lib` weg), `docs/site/data/config.json:35` (lezer `pack/core/lib/headings.py` weg)
- Modify (Go-tests die op `core/lib` leunen): `internal/initcmd/initcmd_test.go:28` (`core/lib/a.py` weg uit `testPacks()`), `:334-337,435-455,790` (lib-verwijder- en behoud-tests: herschrijf naar `scripts/`-bestanden of verwijder met reden), `:1150-1159` (`TestConfigReadsProposedHostTOML` draait `python3 config.py`: weg, Go dekt dit in `internal/config`), `:1340,1364` (gitignore-tests: verwijs naar `.lighthouse/scripts/`), `internal/initcmd/budget_test.go:99-103`, `internal/initcmd/fragment_test.go:90-104` (embed-met-underscore-getuige: kies een ander pad onder `core/`), `pack/pack_test.go:38-41` (stat een bestaand bestand, bv. `core/requires.toml`)
- Modify: `docs/host-assumptions.md:25,32` en `docs/getting-started.md:110`: links naar `pack/core/lib/*.py` en de beschrijving van `.lighthouse/lib/lighthouse_lib/`
- Test: `internal/initcmd/initcmd_test.go`

**Interfaces:**
- Consumes: de bestaande `reconcile.Remove`-route en de `__pycache__`-opruiming
  (`internal/initcmd/initcmd.go:668-727`).

- [ ] **Step 1: Failing tests** in `initcmd_test.go`:
  - Seed de oude toestand **rechtstreeks**, niet via een pakket-fixture met `lib/` (na Step 3 weigert
    `packs.Load` dat met `unknown file`): init met de gewone testpacks, schrijf dan
    `.lighthouse/lib/lighthouse_lib/config.py` en `headings.py` en voeg ze met de juiste hash toe aan
    `.lighthouse/manifest.toml` (de hulpfuncties van de bestaande remove-tests, `initcmd_test.go:435-455`).
  - `TestUpgradeRemovesCoreLib`: die oude toestand (manifest noemt de bestanden), plus een
    `.lighthouse/lib/lighthouse_lib/__pycache__/config.cpython-312.pyc`; upgrade met de huidige
    packs → geen van die paden bestaat, `.lighthouse/lib/` bestaat niet meer, exit 0.
  - `TestUpgradeKeepsModifiedCoreLib`: idem, maar `config.py` is lokaal gewijzigd → het bestand
    staat er nog, stderr noemt het pad, `headings.py` is weg.
  - `TestUpgradeKeepsShellScripts`: met `shell` actief blijft `.lighthouse/scripts/shellcheck-repo.sh`
    staan na de upgrade.
  - In `gatecommand_test.go`: een `command` met `python3 -m lighthouse_lib.x` geeft na de
    upgrade `warning: lighthouse.toml: [test_gate] command uses lighthouse_lib, which lighthouse no longer installs; the test gate breaks until you change it`.
- [ ] **Step 2:** `go test ./internal/initcmd/ ./internal/packs/` → de nieuwe tests FAIL (de
  verwijdering van de lib is nog niet gedaan, de waarschuwing heeft de oude tekst).
- [ ] **Step 3:** Verwijder `pack/core/lib/`, de `requires.toml`-regels, de SOURCES-entry en
  `case "lib"`; pas de `staleLib`-tekst in `reportGateCommand` aan; werk de tellers en
  `places.golden` bij. `removeEmptyParents` stopt voor een pad buiten `installRoots` (`initcmd.go:692,700-711`), en
  `.lighthouse/lib` is er geen: breid de opruiming dus **altijd** uit tot `.lighthouse/lib/` zelf (alleen
  als hij leeg is). Werk daarna de Go-tests uit *Files* bij.
- [ ] **Step 4:** `go test -shuffle=on ./...` → PASS. Mutatiecheck: zet `case "lib"` terug en een
  lege `pack/core/lib/x.py` → `core_test.go` faalt.
- [ ] **Step 5:** Python-kant: verwijder en pas aan zoals onder *Files*. Daarna
  `uv run pytest -q tests/pack tests/test_dogfood.py tests/test_checkpoint_scan.py tests/test_site_config.py tests/test_site_repo.py tests/test_agent_guide.py tests/test_sources.py` → PASS.
- [ ] **Step 6:** `go run ./cmd/lighthouse init --upgrade` → `.lighthouse/lib/` verdwijnt uit deze
  repo; tweede keer `No changes.` Controleer dat `[test_gate] command` van deze repo geen
  `lighthouse_lib` noemt (`lighthouse.toml:44`: klopt nu al).
- [ ] **Step 7:** `git grep -n 'lighthouse_lib' -- pack/ internal/ cmd/ tools/ tests/ ci/ docs/ .lighthouse/ .claude/ .github/` →
  alleen `installDirs`, `gatecommand.go`, hun tests, en fixture-strings in `internal/deps/deps_test.go`,
  `internal/manifest/manifest_test.go` en de oude-manifest-tests van `initcmd_test.go`.
- [ ] **Step 8: Commit** — `feat(core): Python-lib van pack/core weg, init --upgrade ruimt hem op (#285)`.

### Task 5: het bewijs in CI — `no-python`

**Files:**
- Create: `ci/no-python.sh`, `tests/pack/fixtures/hooks/<event>/<hook>[-<stand>].json` (één per
  registratie, zie hieronder), `tests/pack/fixtures/hooks/transcript.jsonl`
- Modify: `.github/workflows/ci.yml` (nieuwe job `no-python`)
- Test: `tests/test_no_python_script.py` (alleen de foutpaden van het script)

**Interfaces:**
- `ci/no-python.sh <lighthouse binary> <lege werkmap>`; exit 0 = alles groen, 1 = een check
  faalde (met een regel per fout op stderr), 2 = verkeerd gebruik.

Het script, in deze volgorde:
1. Bouw `$WORK/toolbox/` met een symlink voor elk uitvoerbaar bestand uit elke PATH-map,
   behalve namen die met `python` beginnen; zet `PATH="$(dirname BIN):$WORK/toolbox"`. Asserteer
   `! command -v python3`.
2. `git init` een repo met `go.mod`, `package.json`, `app.json` (expo) en een `.sh`-bestand, één
   commit, `lighthouse init --yes --pack core --pack go --pack node --pack shell --pack expo`
   met `LIGHTHOUSE_HOST_TOML`, `XDG_RUNTIME_DIR` en `HOME` in `$WORK` (zoals
   `ci/windows-measure.sh`). Asserteer dat `.lighthouse/manifest.toml` alle vijf pakketten
   noemt.
3. Lees elke registratie uit `.claude/settings.json` met `jq`
   (`.hooks | to_entries[] | .key as $ev | .value[] | .hooks[] | [$ev, .command]`). Leid de
   hooknaam en stand af: `run.sh <naam> [stand]` of `…/<naam>.sh`. Zoek
   `tests/pack/fixtures/hooks/<ev>/<naam>[-<stand>].json`; **ontbreekt hij, dan is dat een fout**.
   Vervang `@REPO@` door het repo-pad en `@TRANSCRIPT@` door het pad van `transcript.jsonl`.
   Draai het commando met `bash -c`, `CLAUDE_PROJECT_DIR=$REPO`, payload op stdin. Fout bij
   exit ≠ 0 of bij `COULD NOT CHECK` of `command not found` in stdout/stderr.
4. `grep -rn python3 --exclude-dir=.git "$REPO"` → elke treffer is een fout.

De fixtures (gewone pad: niets om te blokkeren, alles leesbaar):

| Map (`<ev>`) | Bestanden | Payload |
|---|---|---|
| `PreToolUse` | `main-commit-gate`, `commit-test-gate-pre`, `bash-write-check-pre`, `pr-create-gate` | `tool_name: Bash`, `tool_input.command: "git status"` |
| `PreToolUse` | `worktree-check` | `tool_name: Write`, pad `@REPO@/notes.txt` |
| `PostToolUse` | `md-link-check`, `code-language-check`, `gofmt-check`, `shell-lint-check` | `tool_name: Write`, een bestaand bestand van het juiste soort in `@REPO@` |
| `PostToolUse` | `bash-write-check-post`, `commit-test-gate-post`, `usage-gate-tool` | `tool_name: Bash`, `git status`, lege `tool_response` |
| `PostToolUseFailure` | `bash-write-check-post`, `commit-test-gate-post` | idem, met `error` |
| `UserPromptSubmit` | `usage-gate-prompt` | `prompt: "hallo"` |
| `SubagentStop` | `usage-gate-subagent` | `stop_hook_active: false`, `transcript_path: @TRANSCRIPT@` |
| `Notification` | `permission-check` | `notification_type: permission_prompt` |
| `Stop` | `usage-gate-stop`, `go-lint-gate` | `stop_hook_active: false`, `transcript_path: @TRANSCRIPT@` |

`transcript.jsonl` bevat één `user`-regel met een prompt en een `timestamp` (anders levert
`turn-roots` `Undetermined` en meldt `go-lint-gate` `COULD NOT CHECK`; `internal/turnroots/transcript.go:189`).
Elke payload heeft ook `session_id`, `hook_event_name` en `cwd: @REPO@`. Of een hook in
`PostToolUse` onder `Edit|Write` hangt en welke velden hij leest, staat in zijn bron
(`internal/hooks/…`, `pack/go/hooks/`, `pack/shell/hooks/`); kies het veld uit die bron, niet uit
deze tabel als ze verschillen.

- [ ] **Step 1: Failing test** `tests/test_no_python_script.py`:
  `test_missing_fixture_fails` (een registratie zonder fixture → exit 1, stderr noemt het
  verwachte fixture-pad) en `test_python3_hit_fails` (een via `NO_PYTHON_EXTRA` ingebracht bestand met `python3`
  erin → exit 1, stderr noemt `pad:regel`). Beide met de binary uit `lighthouse_dir()` en een
  werkmap in `tmp_path`; het script krijgt daarvoor twee env-variabelen:
  `NO_PYTHON_FIXTURES=<map>` overschrijft de fixture-map, en `NO_PYTHON_EXTRA=<bestand>` wordt vóór de
  grep van stap 4 in de repo gekopieerd (de test-naad voor `test_python3_hit_fails`: na Task 4 bevat
  geen geïnstalleerd bestand `python3` meer).
- [ ] **Step 2:** draai de test → FAIL (script bestaat niet).
- [ ] **Step 3:** Schrijf `ci/no-python.sh` en de fixtures.
- [ ] **Step 4:** `go build -o /tmp/lh ./cmd/lighthouse && ci/no-python.sh /tmp/lh "$(mktemp -d)"`
  → exit 0. Daarna de pytest-test → PASS. Mutatiecheck: verwijder `fixtures/hooks/Stop/go-lint-gate.json`
  → het script geeft exit 1.
- [ ] **Step 5:** Job in `ci.yml`, naast `go-e2e`:
  `no-python`, `needs: changes`, `if: needs.changes.outputs.go == 'true' || needs.changes.outputs.python == 'true'`,
  `ubuntu-latest`, `timeout-minutes: 10`; stappen: checkout (zelfde gepinde SHA,
  `persist-credentials: false`), `setup-go` (zelfde SHA, `go-version-file: go.mod`),
  `go build -o "$RUNNER_TEMP/lighthouse" ./cmd/lighthouse`,
  `ci/no-python.sh "$RUNNER_TEMP/lighthouse" "$RUNNER_TEMP/work"`. Geen `setup-uv`.
- [ ] **Step 6: Commit** — `ci: job no-python bewijst een verse init zonder python3 (#285)`.

### Task 6: documentatie en changelog

**Files:**
- Modify: `docs/getting-started.md:17`, `docs/site/agents/install.md:50,56`, `docs/site/index.html:84`,
  `docs/site/data/system.json:28,121`, `docs/install.md:253-255`
  (de links in `docs/host-assumptions.md` en `getting-started.md:110` staan al in Task 4)
- Modify: `docs/developer/planning/specs/2026-11-07-m3-tweede-omgeving-design.md:11,21,70,80,287,322`
- Create: `changelog.d/issue-285-afronden-zonder-python.md`

- [ ] **Step 1:** Haal Python als vereiste weg: de tabelrij in `getting-started.md:17` en
  `agents/install.md:56` (Python blijft alleen genoemd bij het pakket `python`: `ty-gate`,
  `gate_python`, `python-standard`); `agents/install.md:50` zonder de `tomllib`-check;
  `index.html:84` en `system.json` zonder `python3`; `install.md:253-255` zegt dat geen script
  van `pack/core` nog Python is.
- [ ] **Step 2:** M3-spec volgens *Gevolgen voor M3*: de gids noemt `gh`, `jq`, `git` en de
  binary; de rij "Python ≥ 3.11" en het risico "Python op Windows" vervallen; de zin over
  Python-fallbacks voor `realpath -m` alleen als die er niet meer zijn (`git grep -n 'realpath -m' pack/`).
- [ ] **Step 3:** Fragment, `type: changed` (minor, spec §10): wat weg is, dat `init --upgrade`
  `.lighthouse/lib/` en `create_labels.py` opruimt, en dat een host zonder `python3` werkt
  behalve voor het pakket `python`.
- [ ] **Step 4:** `.claude/hooks/lighthouse/run.sh md-link-check` slaat niet aan op de gewijzigde
  `.md`-bestanden; `git grep -n -i 'python 3.11\|python3' -- docs/getting-started.md docs/site/` →
  alleen regels over het pakket `python` en de dev-server-regels `python3 -m http.server`
  (`getting-started.md:200`, `docs/site/*.html`), die niets met de eis te maken hebben.
- [ ] **Step 5: Commit** — `docs: installatie en M3-spec zonder Python-eis (#285)`.

### Task 7: Windows-meting en eindcontrole

- [ ] **Step 1:** `go run ./cmd/lighthouse init --upgrade` → `No changes.`;
  `go test -shuffle=on ./...` en `uv run pytest -q` → PASS.
- [ ] **Step 2:** Push de branch; `gh workflow run windows-measure.yml --ref issue-285-afronden-zonder-python`;
  wacht op de run (`gh run watch`).
- [ ] **Step 3:** Zet de tabel van de run als comment op #90 (Nederlands; kop: meting na #285,
  met run-URL). Een rode rij is meting, geen blokkade (spec, *Bewijs*).
- [ ] **Step 4:** PR via `/lighthouse-write-pr`.
