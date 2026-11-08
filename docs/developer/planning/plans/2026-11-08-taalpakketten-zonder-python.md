# Taalpakketten zonder Python — implementatieplan (#284)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Doel:** `gate_go.py` en `gate_node.py` worden `lighthouse gate go` en `lighthouse gate node`,
`init --upgrade` zet een bestaand `[test_gate] command` om, en de pakketten `go`, `node` en
`shell` roepen geen `python3` meer aan (behalve `go-lint-gate`, zie *Randvoorwaarde*).

**Architectuur:** De logica komt in een nieuw pakket `internal/gate` (`go.go`, `node.go`), met
een dun cobra-commando in `internal/cli/gate.go`, naar het patroon van `lighthouse config`
(`internal/cli/config.go` + `internal/config`). De herschrijving van het commando komt in
`internal/initcmd/gatecommand.go`, aangeroepen vanuit `planConfig`. De shell-hooks blijven bash
en lezen hun config met `lighthouse config`.

**Tech stack:** Go (cobra, `os/exec`, `encoding/json`), bash, pytest voor de black-box- en
teksttests.

**Spec:** [`2026-11-07-zonder-python-design.md`](../specs/2026-11-07-zonder-python-design.md),
§4, §5, §9, §10 en *Opdeling in issues*, stap 11.

## Global Constraints

- Exitcodes en uitvoer blijven gelijk (spec, *Niet-doelen*: "Een hook geeft na de ombouw
  hetzelfde oordeel als ervoor"). Exit 3 = niets getest, met de regel
  `gate-go: COULD NOT CHECK — <reden>` of `gate-node: COULD NOT CHECK — <reden>` op stderr.
- De overige regels letterlijk als in de scripts: `gate-go: skipped — the commit touches no Go
  package`, `gate-go: <pkgs>`, `gate-node: skipped — the commit touches nothing[ under DIR]`,
  `gate-node: DIR — npm run SCRIPT`, `gate-node: passed — a, b`,
  `gate-node: PATH was stale; npm run SCRIPT regenerated it — commit the result`.
- Enige toegestane tekstwijziging: de usage-regel noemt het nieuwe commando,
  `usage: lighthouse gate go -- CMD...` en
  `usage: lighthouse gate node [--dir DIR] [--also PATH]... [--drift SCRIPT=PATH]... -- SCRIPT...`.
- `LIGHTHOUSE_GATE_FILES` blijft de enige invoer naast de argumenten.
- `pack/go` en `pack/node` krijgen geen `lighthouse.toml.fragment` (spec §5,
  `internal/initcmd/fragment_test.go:54`).
- `init --upgrade` herschrijft alleen de exacte tekst `python3 .lighthouse/scripts/gate_go.py`
  → `lighthouse gate go` en `python3 .lighthouse/scripts/gate_node.py` → `lighthouse gate node`,
  alleen in de waarde van `[test_gate] command`; de rest blijft letterlijk. De upgrade gaat altijd
  door.
- `go-lint-gate.sh`, `gofmt-check.sh` en `shell-lint-check.sh` blijven bash (milestone, *No-Gos*).
- Bevroren Python blijft ongewijzigd: `config.py`, `headings.py`, `datadir.py`, `protocol.py`
  en alles onder `pack/core/lib/` dat deze stap niet noemt.
- Go volgt `/lighthouse-go-standard`, tests volgen `/lighthouse-test-standard`, shell volgt
  `/lighthouse-shell-standard`.
- Instructietekst (skills, hookmeldingen, codecommentaar) in het Engels; plan, commits,
  changelog-fragment en PR in het Nederlands.

## Randvoorwaarde: `python3` in `pack/go/requires.toml`

`go-lint-gate.sh` roept `turn_roots.py` aan met `python3` (`pack/go/hooks/go-lint-gate.sh:56,99`).
Die gaat pas in stap 5 (#278) naar `lighthouse turn-roots`. Deze PR haalt `gate_go` uit de
`python3`-regel, maar `python3 = ["go-lint-gate"]` blijft staan zolang #278 niet gemerged is. De
zin over `python3` in `go-standard` (`SKILL.md:148`/`:152`) blijft dan ook.

- Is #278 gemerged vóór Task 5: rebase eerst, dan valt de regel helemaal weg en is AC 3 dicht.
- Anders blijft AC 3 voor `go` open: de PR krijgt `Refs #284` met die reden, niet `Closes`.

## Dogfood-risico (spec §10)

Na Task 8 roept `[test_gate] command` van deze repo `lighthouse gate go` aan, en
`shell-lint-check.sh` zou `lighthouse config` aanroepen (in repo's met `pack/shell`). De `lighthouse` op het PATH (v0.13.2) kent
beide niet: `unknown command "gate"`, rc 1. Gevolg in deze worktree tot de release:

- de test-gate na de dogfood-commit is rood met `unknown command "gate"`;
- `shell-lint-check` blokkeert elke `.sh`-edit met `COULD NOT CHECK` — alleen in een repo die
  `pack/shell` installeert; deze repo doet dat niet (`.lighthouse/manifest.toml`:
  `packages = ["core", "go", "node"]`), dus hier blokkeert die hook niets en bestaat
  `.lighthouse/scripts/shellcheck-repo.sh` niet.

Daarom zit elke `.sh`-edit vóór Task 8, en draait Task 8 de gate met de branch-binary vooraan op
het PATH als bewijs. Zet nooit `go install` van de branch op de host: andere sessies draaien die
binary ook.

## Review Focus

1. **Een symlink in de cwd of in `Dir`.** `gate_go.py` resolvet root en `Dir`
   (`Path.resolve()`); de Go-versie moet ook via `filepath.EvalSymlinks` vergelijken, anders
   selecteert een worktree onder een symlink niets en meldt `skipped`. Test in Task 2
   (`TestGoSymlinkedCwdSelectsPackage`).
2. **cobra slikt vlaggen.** `lighthouse gate node --dir web -- lint` en
   `lighthouse gate go -- go test -race` moeten hun argumenten ongeschonden krijgen, `--` en
   `-race` inbegrepen. Test in Task 2 en 3 via `NewRoot` (`internal/cli/root.go:57`, `TestGateCommandPassesFlags`).
3. **Een CMD die door een signaal stopt** mag nooit exit 0 of 3 geven. Go geeft `ExitCode() == -1`;
   zet dat om naar `128 + signaalnummer`. Dat is een bewuste afwijking van `gate_go.py` (`sys.exit(-15)` = exit 241): beide zijn rood, geen van beide 3; noem het in de PR. Test in Task 2 (`TestGoSignalledCommandIsRed`).
4. **Een commando dat de oude scripts op een andere manier aanroept** (`python .lighthouse/…`,
   `./.lighthouse/scripts/gate_go.py`, `lighthouse_lib`) wordt niet herschreven maar luid gemeld.
   Een verwijzing naar een bestand dat de upgrade wél installeert (`gate_python.py` van
   `pack/python`, `shellcheck-repo.sh` van `pack/shell`) geeft geen melding. Test in Task 4.
5. **Een tweede upgrade** na de herschrijving verandert niets en meldt niets
   (`No changes.`). Test in Task 4 (`TestUpgradeGateCommandIsIdempotent`).

---

### Task 1: Gedeelde fixtures voor de gate-gevallen (spec §9)

De gevallen van `packages_for` en `touches` staan nu inline in pytest. Ze gaan eerst naar
fixtures, die pytest tegen de `.py` draait; Task 2 en 3 lezen dezelfde bestanden in Go.

**Files:**
- Create: `tests/pack/fixtures/gate_go/select.json`
- Create: `tests/pack/fixtures/gate_node/touches.json`
- Create: `tests/pack/fixtures/gate_node/usage.json`
- Modify: `tests/pack/go/test_gate_go.py:21-88` (inline gevallen weg, fixture-lus erin)
- Modify: `tests/pack/node/test_gate_node.py:73-84,194-217`

**Interfaces:**
- Produces, `select.json`: een lijst van
  `{"name": str, "listing": [{"rel": str, "GoFiles": [...], ...}], "files": [str], "want": [str]}`.
  `rel` is relatief aan de root `/repo`; `ImportPath` is `example.com/m` plus `/rel` (leeg = root).
- Produces, `touches.json`: `[{"name", "files", "dir", "also", "want": bool}]`.
- Produces, `usage.json`: `[{"name", "argv": [str]}]`, elk geval exit 3 met `usage:` op stderr.

- [ ] **Step 1:** Zet de elf gevallen van `test_gate_go.py:30-88`, de zeven asserts van
  `test_gate_node.py:73-84` en de zestien argv's van `test_gate_node.py:194-213` om naar de drie
  JSON-bestanden, met dezelfde invoer en verwachting.
- [ ] **Step 2:** Vervang de inline tests door één geparametriseerde test per bestand
  (`test_select_fixture`, `test_touches_fixture`, `test_usage_fixture`) die de JSON leest. De
  usage-test checkt `"usage:" in err`, niet meer `usage: gate_node.py`.
- [ ] **Step 3:** `uv run pytest -q tests/pack/go/test_gate_go.py tests/pack/node/test_gate_node.py`
  → PASS, met evenveel gevallen als ervoor (`-v | grep -c PASSED` vóór en na vergelijken).
- [ ] **Step 4:** Commit: `test(gate): gevallen van gate_go en gate_node als gedeelde fixtures (#284)`.

### Task 2: `lighthouse gate go`

**Files:**
- Create: `internal/gate/gate.go` (gedeeld: `Env`, `unchecked`, `run`)
- Create: `internal/gate/go.go`
- Create: `internal/gate/go_test.go`
- Create: `internal/cli/gate.go`
- Modify: `internal/cli/root.go:67-80` (`root.AddCommand(newGateCommand())`)
- Modify: `docs/site/data/config.json` (sleutel `test_gate.unchecked_exit_codes`, `readers`): voeg
  `internal/gate/gate.go` toe als lezer; `tests/test_site_config.py` eist dat elk bestand dat de
  sleutel noemt op de kaart staat (`reader_names_key`, `:114-117`) en dat elke lezer bestaat.
  Draai `uv run pytest -q tests/test_site_config.py`.
- Test: `internal/cli/gate_test.go`

**Interfaces:**
- Produces:
  - `type Env struct { Dir, FilesPath string; Stdout, Stderr io.Writer }` — `FilesPath` is
    `LIGHTHOUSE_GATE_FILES` (leeg = niet gezet), `Dir` de cwd.
  - `const Unchecked = 3`
  - `type GoPackage struct { ImportPath, Dir string; GoFiles, CgoFiles, TestGoFiles, XTestGoFiles, IgnoredGoFiles, EmbedFiles, TestEmbedFiles, XTestEmbedFiles []string }`
  - `func GoPackages(files []string, listing []GoPackage, root string) []string`
  - `func Go(args []string, env Env) int` — `args` is alles na `gate go`, `--` inbegrepen.
  - `func newGateCommand() *cobra.Command` met subcommando `go`; `DisableFlagParsing: true`;
    een rc ≠ 0 als `&exitError{code: rc, err: errSilent}`. Het ouder-commando `gate` heeft zelf
    een `RunE` en `Args: cobra.ArbitraryArgs`: `lighthouse gate` zonder subcommando of met een
    onbekende naam (`gate nod -- lint`) print `gate: COULD NOT CHECK — unknown gate <x>` op
    stderr en geeft rc 3. Zonder `RunE` print cobra de help en geeft 0 (cobra v1.10.2
    `command.go:955`, `:1152`), een stil groene gate.

- [ ] **Step 1: Falende tests.** In `go_test.go`:
  - `TestGoPackagesFixtures`: leest `../../tests/pack/fixtures/gate_go/select.json`, bouwt
    `GoPackage{Dir: filepath.Join("/repo", rel)}`, en eist `GoPackages(files, listing, "/repo") == want`.
  - De CLI-gevallen van `test_gate_go.py:117-227` als Go-tests op een mini-module in
    `t.TempDir()` met de echte `go` (`t.Skip` zonder `go` op PATH): zonder FilesPath draait
    `CMD ./...`; een geraakt pakket geeft `gate-go: example.com/m/a`; niets geraakt geeft rc 0
    en de skip-regel; de rc van CMD komt door; een falende `go list` houdt zijn stderr en geeft
    3; usage (`["echo"]`, `["--"]`, `[]`) geeft 3; een ontbrekende lijst geeft 3; geen `go` op
    PATH geeft 3 met `go not on PATH`; een CMD die niet start geeft 3, met en zonder FilesPath.
  - `TestGoSymlinkedCwdSelectsPackage` (Review Focus 1) en `TestGoSignalledCommandIsRed`
    (CMD `sh -c 'kill -TERM $$'`, eist rc 143) (Review Focus 3).
  - In `internal/cli/gate_test.go`: `TestGateUnknownSubcommandIsUnchecked` (`lighthouse gate` en
    `lighthouse gate bogus -- x` geven rc 3 en `COULD NOT CHECK`, nooit 0), en
    `TestGateCommandPassesFlags` — `lighthouse gate go -- sh -c
    'echo "$@"' x -race` zonder FilesPath print `-race ./...` (Review Focus 2).
- [ ] **Step 2:** `go test ./internal/gate/ ./internal/cli/ -run 'Go|Gate'` → FAIL (undefined).
- [ ] **Step 3: Implementeer** `GoPackages` als `packages_for`/`_belongs`
  (`pack/go/scripts/gate_go.py:37-63`): `go.mod`/`go.sum` en lege regels overslaan; root en
  `Dir` via `filepath.EvalSymlinks`, bij een fout `filepath.Abs`; eerste padsegment `testdata`
  telt; een pad in een van de acht lijsten telt; een `.go`-bestand direct in `Dir` telt.
  Resultaat gesorteerd en uniek. `Go` als `_select_and_run` + `main` (`:109-136`); `go list -json
  ./...` decodeer je met een `json.Decoder`-lus. CMD schrijft naar `env.Stdout` en `env.Stderr` (in productie `os.Stdout`/`os.Stderr`) en erft stdin; anders kan `TestGateCommandPassesFlags` de uitvoer niet opvangen.
- [ ] **Step 4:** `go test -race ./internal/gate/ ./internal/cli/` → PASS.
- [ ] **Step 5:** Commit: `feat(gate): lighthouse gate go kiest de Go-pakketten van de commit (#284)`.

### Task 3: `lighthouse gate node`

**Files:**
- Create: `internal/gate/node.go`
- Create: `internal/gate/node_test.go`
- Modify: `internal/cli/gate.go` (subcommando `node`)

**Interfaces:**
- Consumes: `Env`, `Unchecked` uit Task 2.
- Produces: `func NodeTouches(files []string, dir string, also []string) bool`,
  `func Node(args []string, env Env) int`.

- [ ] **Step 1: Falende tests.** `TestNodeTouchesFixtures` en `TestNodeUsageFixtures` lezen de
  twee JSON-bestanden uit Task 1. De gedragsgevallen van `test_gate_node.py:86-276` worden
  Go-tests met een nep-`npm` en nep-`node` in een tijdelijke PATH-map (shellscripts die hun
  aanroep loggen en een rc geven): skip-regels (met en zonder `under DIR`), drift dan scripts,
  `--also`, pad met spatie, stop bij de eerste fout, zonder FilesPath alles, drift verouderd
  (rc 1 en de stale-regel), nieuw bestand telt als verouderd, drift op één bestand, rc van de
  gen-stap komt door, ontbrekende `node_modules` (3, met `fix: cd DIR && npm ci`), `npm` niet op
  PATH (3), onleesbare lijst (3), `./web` en `./lighthouse.toml` genormaliseerd, onbekend script
  en onbekend drift-script (3), ongeldige `package.json` (3).
- [ ] **Step 2:** `go test ./internal/gate/ -run Node` → FAIL.
- [ ] **Step 3: Implementeer** als `pack/node/scripts/gate_node.py:47-191`. `_clean` wordt
  `path.Clean` met dezelfde weigeringen (leeg, absoluut, `..`, `../…`). `snapshot` vergelijkt
  een map van relatief pad naar inhoud (`filepath.WalkDir`). De lijst lees je als bytes en splits
  je op `\n`, zonder UTF-8-check (als `surrogateescape`).
- [ ] **Step 4:** `go test -race ./internal/gate/ ./internal/cli/` → PASS.
- [ ] **Step 5:** Commit: `feat(gate): lighthouse gate node draait de npm-scripts van de commit (#284)`.

### Task 4: `init --upgrade` zet `[test_gate] command` om

**Files:**
- Create: `internal/initcmd/gatecommand.go`
- Create: `internal/initcmd/gatecommand_test.go`
- Modify: `internal/initcmd/initcmd.go:570-591` (`planConfig`)
- Modify: `docs/site/data/config.json`: als `gatecommand.go` of zijn meldingstekst
  `[test_gate] command` of `unchecked_exit_codes` letterlijk noemt, staat het bestand als lezer
  op de kaart. Draai `uv run pytest -q tests/test_site_config.py`.

**Interfaces:**
- Produces: `func migrateGateCommand(old []byte, installed func(rel string) bool) (out []byte, before, after string, stale []string)`.
  - Zoekt in de tabel `[test_gate]` de sleutel `command`, alleen op die regel(s); commentaar en
    andere tabellen blijven onaangeroerd.
  - Vervangt de twee exacte teksten; `before`/`after` zijn de oude en nieuwe waarde (leeg als er
    niets veranderde).
  - `stale`: elk pad `.lighthouse/scripts/<x>` of `.lighthouse/lib/<x>` in de waarde ná de
    herschrijving waarvoor `installed(pad)` false geeft, plus elke `lighthouse_lib` die niet onder
    zo'n geïnstalleerd pad valt.
- `planConfig` krijgt de geplande units (`desired`) om `installed` te bouwen, en roept
  `migrateGateCommand` alleen aan bij `o.Upgrade`, op `p.old`, vóór `proposeConfig`.
- Meldingen op `o.Err`, letterlijk:
  - `lighthouse.toml: [test_gate] command rewritten` / `  old: <before>` / `  new: <after>`
  - `warning: lighthouse.toml: [test_gate] command calls <pad>, which lighthouse no longer installs; the test gate breaks until you change it`
  - voor een `lighthouse_lib`-verwijzing (core installeert `lighthouse_lib` nog, bevroren) een
    neutrale tekst: `warning: lighthouse.toml: [test_gate] command uses lighthouse_lib, which lighthouse is phasing out; move it to a lighthouse command`

- [ ] **Step 1: Falende tests** in `gatecommand_test.go`, met de helpers van `initcmd_test.go`
  (`newRepo`, `run`, `all(upgrade, yes, …)`, `read`):
  - `TestUpgradeRewritesGateGo` (AC 1): `command = "python3 .lighthouse/scripts/gate_go.py -- go test -race && make lint"`
    wordt `command = "lighthouse gate go -- go test -race && make lint"`; stderr bevat `old:` en
    `new:` met beide waarden.
  - `TestUpgradeRewritesGateNode`: idem met `gate_node.py --dir web -- lint`.
  - `TestUpgradeLeavesCommentAlone`: een `#`-regel met `python3 .lighthouse/scripts/gate_go.py`
    erboven blijft staan.
  - `TestUpgradeWarnsOnOtherScriptRef` (Review Focus 4): `python .lighthouse/scripts/gate_go.py
    -- go test` en `python3 -m lighthouse_lib.x` blijven letterlijk, stderr bevat de warning, rc 0.
  - `TestUpgradeKeepsInstalledRefQuiet` (helpers in `initcmd_test.go`/`select_test.go`; de stub
    `withPython` (`select_test.go:22-30`) installeert geen `scripts/gate_python.py`: voeg dat
    bestand toe aan de MapFS van `o.Packs`, anders is `installed(...)` false): met pack `python` en
    `python3 .lighthouse/scripts/gate_python.py -- pytest` geen warning.
  - `TestUpgradeGateCommandIsIdempotent` (Review Focus 5): tweede upgrade → `No changes.`, geen
    melding.
  - `TestInitDoesNotRewrite`: zonder `upgrade` blijft het oude commando staan.
- [ ] **Step 2:** `go test ./internal/initcmd/ -run 'Gate|Upgrade.*Gate|InitDoesNotRewrite'` → FAIL.
- [ ] **Step 3:** Implementeer `migrateGateCommand` en de aanroep in `planConfig`.
- [ ] **Step 4:** `go test -race ./internal/initcmd/` → PASS.
- [ ] **Step 5:** Commit: `feat(init): upgrade zet gate_go.py en gate_node.py in [test_gate] command om (#284)`.

### Task 5: Pakketten `go` en `node` zonder de scripts

Eerst: is #278 gemerged? Zie *Randvoorwaarde*.

**Files:**
- Delete: `pack/go/scripts/gate_go.py`, `pack/node/scripts/gate_node.py`
- Modify: `docs/site/data/config.json:396-402`: de lezers `pack/go/scripts/gate_go.py` en
  `pack/node/scripts/gate_node.py` weg (anders `reader … does not exist` in
  `tests/test_site_config.py`); voeg de `internal/gate`-bestanden toe die de sleutel noemen
- Delete: `tests/pack/go/test_gate_go.py`, `tests/pack/node/test_gate_node.py` (de fixtures
  blijven; Go leest ze)
- Modify: `pack/go/requires.toml` → `go = ["go-lint-gate", "go-standard"]`,
  `python3 = ["go-lint-gate"]` (of weg, na #278), `tomllib` weg
- Modify: `pack/node/requires.toml` → `node = ["lighthouse gate node"]`,
  `npm = ["lighthouse gate node"]`, `python3` weg
- Modify: `pack/go/skills/go-standard/{claude,agents}/SKILL.md:50,55` →
  `lighthouse gate go -- go test -race`; `:148`/`:152` alleen na #278
- Modify: `pack/SOURCES.toml:589,613` (de twee scriptregels weg)
- Modify: `internal/packs/core_test.go:181-192` (bestanden en tools van `go` en `node`)
- Modify: `tests/pack/go/test_go_writing.py:123`, `tests/test_ci_go_test_pin.py:10-15`
  (regex op `lighthouse gate go --`), `docs/getting-started.md:27-28`
- Test: `go test ./internal/packs/`, `uv run pytest -q tests/pack/go tests/pack/node tests/test_ci_go_test_pin.py tests/test_site_config.py`

- [ ] **Step 1:** Pas `core_test.go` aan op de nieuwe bestanden en tools → `go test ./internal/packs/` FAIL.
- [ ] **Step 2:** Voer de deletes en wijzigingen uit.
- [ ] **Step 3:** `go test -race ./internal/packs/` en de pytest-selectie → PASS. Check:
  `grep -rn "python3\|gate_go\.py\|gate_node\.py" pack/go pack/node` geeft alleen
  `go-lint-gate.sh`, zijn `requires.toml`-regel en — zolang #278 open is — de twee regels
  `python3` in `go-standard` `claude/SKILL.md:148` en `agents/SKILL.md:152` (of niets, na #278).
  Haal die twee regels niet weg: de tekst is waar zolang `go-lint-gate` `python3` aanroept.
- [ ] **Step 4:** Commit: `feat(pack): go en node gebruiken lighthouse gate, zonder Python (#284)`.

`tests/test_dogfood.py::test_node_pack_installed` (`:185-197`) leest `pack/node/scripts/gate_node.py`
en is dus rood van Task 5 tot Task 8, waar het wordt omgezet; het hoort niet in de selecties
van Task 5 tot 7 en is geen reden voor een eerder herstel.

`test_ci_go_test_pin.py` leest `lighthouse.toml` van deze repo; die verandert pas in Task 8. Laat
de regex daarom beide vormen accepteren (`(?:gate_go\.py|lighthouse gate go) --`) en haal de oude
vorm weg in Task 8.

### Task 6: Pakket `shell` zonder Python

**Files:**
- Modify: `pack/shell/hooks/shell-lint-check.sh:26-38,83,119-123`
- Modify: `pack/shell/scripts/shellcheck-repo.sh:57-58`
- Modify: `pack/shell/skills/shell-standard/claude/SKILL.md:170`, `agents/SKILL.md:179`
- Modify: `pack/shell/requires.toml` → `python3` en `tomllib` weg; `lighthouse` staat niet in
  `requires.toml` (de binary is verplicht, spec §1)
- Modify: `internal/packs/core_test.go:226`
- Test: `tests/pack/shell/test_shellcheck.py`, `tests/pack/shell/test_shell_gates.py`

**Interfaces:**
- `shell-lint-check.sh`: payload alleen met `jq`; zonder `jq`:
  `fail "jq is not present, so the edited path cannot be read."`. Config:
  `STRICT_DIRS=$(cd "$ROOT" && lighthouse config shell.strict_dirs 2>&1)`; zonder `lighthouse`
  op PATH: `fail "lighthouse not on PATH"`. `systemMessage` met
  `jq -cn --arg m "$msg" '{systemMessage: $m}'`.
- `shellcheck-repo.sh`: `dirs_out="$(cd "$LIB_ROOT" && lighthouse config shell.strict_dirs 2>&1)"`,
  zelfde exit `$SHELLCHECK_UNAVAILABLE` bij een fout.

- [ ] **Step 1: Falende tests.** In `test_shellcheck.py`: de test op `:392-407` (geen `jq`, wel
  `python3`) wordt `test_without_jq_could_not_check` (exit 2, `jq is not present`). Nieuw:
  `test_without_lighthouse_could_not_check` (PATH zonder `lighthouse`, exit 2, `COULD NOT CHECK`)
  en `test_strict_dirs_from_lighthouse_config` met de fixture `lighthouse_bin` uit
  `tests/conftest.py:62`. Bestaande hooktests krijgen `lighthouse_bin`.
- [ ] **Step 2:** `uv run pytest -q tests/pack/shell` → FAIL.
- [ ] **Step 3:** Pas de hook, het script en `requires.toml` aan. In `shell-standard` wordt de
  regel met `python3 -c '…realpath…'` het recept `realpath -m -- "$p"`; met de opmerking dat
  BSD/macOS `realpath` geen `-m` kent. `lighthouse path realpath` bestaat niet en hoort niet in
  deze PR; geef daarom een POSIX-recept dat overal werkt: `realpath -m -- "$p" 2>/dev/null`,
  anders de bovenliggende map oplossen met `d=$(cd -P -- "$(dirname -- "$p")" 2>/dev/null && pwd -P)
  && printf '%s/%s\n' "$d" "$(basename -- "$p")"` (het doelpad van een `Write` hoeft nog niet te
  bestaan). Geen `python3`. Dit wijkt licht af van de spec-parenthese (§4: "anders `lighthouse
  path realpath`"), die een voorbeeld is van "een recept zonder Python"; noem het in de PR.
- [ ] **Step 4:** `uv run pytest -q tests/pack/shell` en `go test ./internal/packs/` → PASS;
  `pack/shell/scripts/shellcheck-repo.sh pack/shell/hooks/shell-lint-check.sh pack/shell/scripts/shellcheck-repo.sh` → schoon (het pad in `pack/`: deze repo installeert `pack/shell` niet; met expliciete bestanden roept het script `lighthouse config` niet aan).
- [ ] **Step 5:** Commit: `feat(shell): shell-lint-check en shellcheck-repo lezen config met lighthouse (#284)`.

### Task 7: `place-instruction` zonder Python

**Files:**
- Modify: `pack/core/skills/place-instruction/SKILL.md:247`
- Modify: `tests/pack/test_place_instruction_skill.py:40-44`

`scripts/measure_skill_surface.py` bestaat in deze repo niet (geen map `scripts/`; het script
leeft in een andere repo), dus de KPI-regel gaat weg (spec §4, tweede optie). Geen voorwaarde
om te evalueren.

- [ ] **Step 1:** Vervang `test_surface_measurement_only_if_present` door
  `test_skill_has_no_python3` (`assert "python3" not in SKILL`) → FAIL. Verwijder de KPI-regel;
  pas de KPI-telling in de tekst aan als die het aantal rijen noemt.
- [ ] **Step 2:** `uv run pytest -q tests -k place_instruction` → PASS.
- [ ] **Step 3:** Commit: `docs(place-instruction): KPI-regel zonder python3 (#284)`.

### Task 8: Dogfood, fragment en eindcontrole

**Files:**
- Modify (door de upgrade): `lighthouse.toml:44`, `.claude/`, `.agents/`, `.lighthouse/`
- Modify (hand): `lighthouse.toml:42` (commentaar), `tests/test_dogfood.py:166-197`,
  `tests/test_ci_go_test_pin.py` (oude vorm weg)
- Create: `changelog.d/issue-284-taalpakketten-zonder-python.md`

- [ ] **Step 1:** `go run ./cmd/lighthouse init --upgrade`. Verwacht op stderr de regels
  `rewritten`/`old:`/`new:` voor `[test_gate] command`; `gate_python.py` (onder `pack/python/`)
  blijft en geeft geen warning. Dit is het tweede bewijs van AC 1, op een echte repo.
- [ ] **Step 2:** Werk het commentaar op `lighthouse.toml:42` bij, en `test_dogfood.py`
  (verwacht commando, en `test_node_pack_installed` checkt nu dat `.lighthouse/scripts/gate_node.py`
  níet bestaat).
- [ ] **Step 3:** Fragment, `type: changed`, met: `[test_gate] command` roept
  `lighthouse gate go` en `lighthouse gate node` aan; `init --upgrade` zet het oude commando om
  en meldt andere verwijzingen; de `lighthouse`-binary op het PATH moet deze release zijn
  (`changed` → minor bij de wekelijkse release; zie `docs/releasing.md:15`). Noem in de PR dat tot de release (of een `go install ./cmd/lighthouse` op main door de eigenaar) elke commit op main een rode of onbeslist gate geeft (`unknown command "gate"`).
- [ ] **Step 4:** `go run ./cmd/lighthouse init --upgrade` nog eens → `No changes.`
- [ ] **Step 5:** Bewijs van de gate met de branch-binary. De `$(…)` wordt door de buitenste shell
  uitgebreid, vóór een `PATH=` op `bash` geldt; zet daarom eerst het PATH en haal het commando
  daarna op:
  `T=$(mktemp -d) && go build -o "$T/lighthouse" ./cmd/lighthouse && export PATH="$T:$PATH" && (cd web && npm ci) && cmd=$(lighthouse config test_gate.command) && [ -n "$cmd" ] && f=$(mktemp) && t=$(mktemp -d) && git -c core.quotePath=false diff --name-only --no-renames origin/main HEAD > "$f" && LIGHTHOUSE_GATE_FILES="$f" LIGHTHOUSE_GATE_TMP="$t" bash -c "$cmd"`
  (zoals de repro-regel van `commit-test-gate.sh:198`). `LIGHTHOUSE_GATE_TMP` is verplicht: het
  commando van deze repo heeft `--basetemp="$LIGHTHOUSE_GATE_TMP"` tussen het go- en het
  node-deel, en een lege waarde laat pytest met een usage-fout stoppen vóór `gate node`.
  `web/node_modules` moet bestaan (`lighthouse.toml:44` heeft `--also lighthouse.toml`, dus
  `gate node --dir web` draait in de branch-diff; zonder `node_modules` geeft het rc 3). Eis
  naast rc 0 dat de uitvoer de regels `gate-go:` en `gate-node:` bevat; een lege `$cmd` of rc 0
  zonder die regels is geen bewijs.
- [ ] **Step 6:** Volledige run: `go test -race ./...` en `uv run pytest -q`. Check AC 3 en
  AC 4: `grep -n python3 pack/{go,node,shell,expo}/requires.toml` (alleen `go-lint-gate`, of
  niets na #278); `git diff --stat origin/main -- pack/core/lib .lighthouse/lib` toont geen
  wijziging aan bevroren modules.
- [ ] **Step 7:** Commit: `chore(dogfood): init --upgrade met lighthouse gate (#284)`. De
  test-gate op deze commit is rood met `unknown command "gate"` zolang de PATH-binary v0.13.2 is
  (zie *Dogfood-risico*); stap 5 is het bewijs. Meld dat in de PR.
