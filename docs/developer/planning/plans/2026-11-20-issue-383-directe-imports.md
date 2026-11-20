# Poort op ongedeclareerde directe imports — implementatieplan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Doel:** `lighthouse imports check` faalt per taalpack (Python, Go, Node/Expo) op een import die alleen transitief in de omgeving staat, en meldt bestand, regel, ID en import; een Stop-hook per pack en de commit-gate roepen hem aan.

**Architectuur:** de logica staat in een eigen Go-pakket `internal/importcheck` (per taal een checker achter één interface, externe gereedschappen via een naad voor tests). Een cobra-subcommand `imports check` is de enige ingang; drie Stop-hooks (`pack/{python,go,node}/hooks/imports-gate-<taal>.sh`) en een stap in `internal/commitgate` roepen die aan. Pins van deptry en knip zijn Go-constanten naast de aanroep.

**Tech stack:** Go (cobra, `go/parser`), deptry (via `uv run`), knip (via `npx`), `go mod tidy -diff`, bash-hooks, pytest voor de Python-canary.

**Spec:** [2026-11-19-issue-383-directe-imports-design.md](../specs/2026-11-19-issue-383-directe-imports-design.md)

## Globale eisen

- Code, namen, commentaar, hook-meldingen en testdata Engels; spec, PR, changelog-fragment en commitberichten Nederlands (Conventional Commits, eindigen op `(#383)`).
- Geen nieuwe Python-code buiten `pack/python/`. deptry is extern.
- Elk gereedschap gepind (Go-constante), licentie in het commentaar bij de constante en in de PR genoemd. Versies: laatste stabiele, minstens twee weken oud. knip verwacht ISC, deptry MIT.
- Nooit stil groen: geen gereedschap, geen venv, geen `node_modules`, Go < 1.23, een te oude binary (`unknown command`) en exit 1 zonder bevinding zijn `COULD NOT CHECK`.
- Exitcodes: 0 schoon, 1 bevindingen, 3 (`gate.Unchecked`) kon niet controleren, ook bij een onbekende vlag. Combinatie: bevindingen winnen (exit 1, ook de `COULD NOT CHECK`-regels in de uitvoer), anders wint 3 van 0.
- Uitvoer: een regel per bevinding op stdout `<bestand>:<regel>: <ID> <import> — <reden>`; `COULD NOT CHECK — <reden>` per project op stderr.
- Blokkerende ID's: python `DEP001,DEP003`; go `indirect-import,missing-require`; node `unlisted`. Niet blokkerend: `DEP002`, `DEP004`, overige Go-drift.
- Opt-out: `[imports] check = false` in `lighthouse.toml` zet hook én commit-gate uit; `[test_gate] command = ""` niet.
- Dogfood: elke wijziging onder `pack/` draait `go run ./cmd/lighthouse init --upgrade` en commit de geïnstalleerde kopie. Elk nieuw bestand onder `pack/` krijgt een `[[file]]`-regel met `new = true` in `pack/SOURCES.toml`.
- Go-code volgt `/lighthouse-go-standard`, tests `/lighthouse-test-standard`. Testcommando: `go test -race -shuffle=on ./...`; canaries: `go test -tags canary ./internal/importcheck/`.
- Geen `--no-verify`; rood na een commit herstel je in een nieuwe commit.

## Review-focus

Invoerklassen die de spec impliceert maar geen taak vanzelf raakt; elke regel heeft een test in de genoemde taak.

1. **Een Go-repo met meerdere gevolgde `go.mod`'s, `--files` in maar één** (lighthouse zelf: `go.mod` + `web/go.mod`): de andere module wordt niet gedraaid (taak 2, 3).
2. **Bestandsnaam met spatie of niet-ASCII in `git ls-files` / `--files`**: projecten en scope werken (taak 2, via `-z`).
3. **Python-bestand dat deptry als `DEP003` meldt in een bestand buiten `--files`, zonder manifestwijziging**: geen bevinding (taak 2 scope-test, taak 4).
4. **Go `tidy -diff` met een hunk in een `replace`- of `go`-regel**: geen bevinding, geen crash (taak 3).
5. **`--files` leeg of afwezig bij de commit-gate (lege commit, alleen verwijderingen)**: de stap draait niets en blokkeert niet; pre draait op staged bestanden (taak 8).

---

## Bestandsstructuur

- Nieuw `internal/importcheck/` — `importcheck.go` (types, `Run`, scope, combinatie, uitvoer), `projects.go` (ontdekking via git), `golang.go`, `python.go`, `node.go` (checkers), `pins.go` (constanten), `*_test.go`, `canary_test.go` (tag `canary`), `testdata/<hook>/<id>/` (fixtures).
- Nieuw `internal/cli/imports.go` (+ `imports_test.go`) — subcommand; registreren in `internal/cli/root.go`.
- Wijzig `internal/config/config.go` — sleutel `imports.check`.
- Nieuw `internal/commitgate/imports.go` (+ test) — de stap; aanroep in `RunRoots` (`gate.go`).
- Nieuw `pack/{python,go,node}/hooks/imports-gate-<taal>.sh`; wijzig `pack/{python,go}/hooks/hooks.json`; nieuw `pack/node/hooks/hooks.json`, `pack/node/instructions/AGENTS.md`; wijzig `pack/{python,go}/instructions/AGENTS.md`, `pack/node/requires.toml`, `pack/SOURCES.toml`.
- Nieuw `tests/pack/python/test_imports_gate_canary.py` + `tests/pack/python/testdata/imports-gate-python/<id>/`.
- Wijzig `.github/workflows/ci.yml` (job `canary`), `changelog.d/`, docs; de geïnstalleerde kopie na `init --upgrade`.

---

### Taak 1: configsleutel `imports.check`

**Files:**
- Modify: `internal/config/config.go` (schema ~r.96-116, `defaults` ~r.140-170)
- Modify: `tests/pack/fixtures/config-schema.json` (snapshot van schema en defaults, `TestSchemaMatchesSnapshot`), `tests/pack/fixtures/config/defaults/expect.json` en `tests/pack/fixtures/config/full/` (pariteit, `internal/config/parity_test.go`), `docs/site/data/config.json` (de sleutel op de kaart) en `tests/test_site_config.py` (`UNREAD`: de sleutel `imports.check` staat daar tijdelijk in, want `test_every_key_is_on_the_map` eist readers en `internal/config/config.go` telt niet als reader; taak 6 haalt hem weer uit `UNREAD`, want `test_unread_keys_have_no_reader` faalt anders zodra `internal/cli/imports.go` hem leest)
- Test: `internal/config/config_test.go`

**Interfaces:**
- Produces: `lighthouse config imports.check` geeft `true` (default) of de gezette waarde; type `kBool`.

- [ ] **Step 1: Test** `TestImportsCheckDefaultsToTrue` (leeg `lighthouse.toml` → exit 0, stdout `true`) en `TestImportsCheckFalse` (`[imports]\ncheck = false` → `false`), in de stijl van de bestaande bool-sleutel-tests.
- [ ] **Step 2:** `go test ./internal/config/ -run ImportsCheck` — FAIL (`unknown key`).
- [ ] **Step 3:** voeg `{"imports.check", kBool}` aan het schema en `"imports.check": true` aan `defaults` toe.
- [ ] **Step 4:** werk de snapshot- en pariteitsfixtures en `docs/site/data/config.json` bij; draai `go test ./internal/config/` (volledig, niet alleen `-run ImportsCheck`) en `uv run pytest -q tests/test_site_config.py` — PASS. De sleutel staat hier in `UNREAD` en zonder readers; taak 6 en 8 voegen de reader-paden toe.
- [ ] **Step 5: Commit** `feat(config): sleutel imports.check (#383)`.

---

### Taak 2: kern van `internal/importcheck`

**Files:**
- Create: `internal/importcheck/importcheck.go`, `internal/importcheck/projects.go`
- Test: `internal/importcheck/importcheck_test.go`, `internal/importcheck/projects_test.go`

**Interfaces:**
- Produces:
```go
type Lang string // "python", "go", "node"
type Finding struct { File string; Line int; ID, Import, Reason string } // File relative to the repo root, slash-separated
type Project struct { Lang Lang; Dir string } // Dir relative to the repo root, "." for the root
type Checker interface {
    // Check returns the findings of one project, or an UncheckedError.
    Check(p Project, env Env) ([]Finding, error)
}
type UncheckedError string // implements error; the text is the reason after "COULD NOT CHECK — "
type Env struct {
    Root string; Stdout, Stderr io.Writer
    Exec func(dir string, argv []string, extraEnv []string) (stdout, stderr []byte, code int, err error) // seam; default os/exec, err only when the process cannot start
    LookPath func(string) (string, error)
    GoVersion func() (string, error)
}
type Options struct {
    Root string
    Langs []Lang   // empty: the packs of .lighthouse/manifest.toml (expo → node)
    Files []string // empty: everything
    Checkers map[Lang]Checker // the CLI (task 6) builds the map of the real checkers; Run gives UncheckedError for a lang without one, never a silent pass
}
func Run(o Options, env Env) int // 0, 1 or 3; writes findings to env.Stdout, COULD NOT CHECK lines to env.Stderr
func Discover(root string, lang Lang) ([]Project, error) // from `git ls-files -z`
func InScope(p Project, files []string) (all bool, touched bool) // touched: a file under p.Dir; all: a manifest/lockfile of p is in files
func FormatFinding(f Finding) string // "<file>:<line>: <ID> <import> — <reason>"
func ManifestFiles(lang Lang) []string // pyproject.toml, uv.lock | go.mod, go.sum | package.json, package-lock.json, pnpm-lock.yaml, yarn.lock, bun.lockb, bun.lock
```
- `Discover` slaat elk pad over met een `testdata`- of `node_modules`-segment (zoals het go-gereedschap): fixture-projecten en geïnstalleerde pakketten zijn geen project van de repo zelf (spec §1, verduidelijking).
- Node projecten: een gevolgde `package.json` die niet in `workspaces` of `pnpm-workspace.yaml` van een `package.json` erboven valt (workspace-root, spec §4). `Discover` leest die twee bronnen.

- [ ] **Step 1: Tests** (`importcheck_test.go`, met fake `Checker`s):
  - `TestRunExitCodes`: fake checkers → bevinding = 1; alleen `UncheckedError` = 3; beide = 1 met de bevinding op stdout én `COULD NOT CHECK — <reden>` op stderr; niets = 0.
  - `TestRunScopesToFiles`: bevinding in `a.py` en in `b.py`, `Files=["a.py"]` → alleen `a.py` gemeld; `Files=["pyproject.toml"]` → beide; `Files=["web/x.ts"]` draait alleen het project onder `web/`.
  - `TestRunSkipsProjectsOutsideFiles`: een fake die paniekt als hij voor een niet geraakt project draait (review-focus 1).
  - `TestFormatFinding`: `FormatFinding(Finding{"src/a.py",3,"DEP003","numpy","imported but only a transitive dependency"})` == `src/a.py:3: DEP003 numpy — imported but only a transitive dependency`.
  - `TestRunDefaultLangsFromManifest`: manifest met `packages = ["core","expo"]` → alleen `node`; `core`/onbekend → geen talen → exit 0 zonder uitvoer.
- [ ] **Step 2: Tests** (`projects_test.go`, `t.TempDir()` + `git init`): `TestDiscoverGoModules` (`go.mod` en `web/go.mod` → twee projecten), `TestDiscoverIgnoresUntracked` (ongevolgde `go.mod` telt niet), `TestDiscoverNodeWorkspaceRoots` (root met `workspaces: ["packages/*"]` en `packages/a/package.json` → één project; `pnpm-workspace.yaml` idem; twee losse `package.json` → twee), `TestDiscoverWeirdFilenames` (spatie en `é` in een pad; review-focus 2), `TestDiscoverSkipsTestdataAndNodeModules` (gevolgde `go.mod` onder `internal/x/testdata/` en `package.json` onder `node_modules/` → geen project).
- [ ] **Step 3:** `go test ./internal/importcheck/` — FAIL (pakket ontbreekt).
- [ ] **Step 4: Implementeer** de signaturen hierboven. `Discover` gebruikt `git ls-files -z`; `Run` zet bevindingen buiten `--files` weg tenzij `InScope` `all` geeft; sorteer bevindingen op bestand, regel voor stabiele uitvoer.
- [ ] **Step 5:** zelfde commando — PASS.
- [ ] **Step 6: Commit** `feat(importcheck): kern, projectontdekking en scope (#383)`.

---

### Taak 3: Go-checker (`go mod tidy -diff`)

**Files:**
- Create: `internal/importcheck/golang.go`
- Test: `internal/importcheck/golang_test.go`

**Interfaces:**
- Consumes: taak 2 (`Checker`, `Env.Exec`, `Env.GoVersion`, `UncheckedError`).
- Produces: `type GoChecker struct{}` met `Check`; `func parseTidyDiff(diff string) (shifted []string, missing []string)` — modulepaden die van `// indirect` naar direct gaan, resp. een directe `require` die ontbreekt.

- [ ] **Step 1: Tests**, op vaste diff-teksten en een fake `Exec` (geen echte `go`):
  - `TestParseTidyDiff`: een hunk `-\tgithub.com/x/y v1.0.0 // indirect` + `+\tgithub.com/x/y v1.0.0` → `shifted=["github.com/x/y"]`; een `+`-require zonder `// indirect` zonder `-`-tegenhanger → `missing`; een overbodige `-require`, een ontbrekende `// indirect`-`+`, een `go.sum`-hunk, een hunk in `replace`/`go`-regel → geen van beide (review-focus 4); een verschoven module waarvan `-` en `+` in aparte hunks staan, ook met een andere versie → `shifted` (koppel op modulepad over de hele diff, niet op aangrenzende regels).
  - `TestGoCheckerUncheckedCases`: Go < 1.23 (`GoVersion` geeft `go1.22.5`), `tidy -diff` exit 1 zonder diff, `tidy` exit 2, `go` niet op PATH → telkens `UncheckedError` met de stderr als reden.
  - `TestGoCheckerCleanIsExit0EmptyStdout`.
- [ ] **Step 2:** `go test ./internal/importcheck/ -run 'Go|Tidy'` — FAIL.
- [ ] **Step 3: Implementeer** `GoChecker.Check` volgens spec §3: exitcode eerst; classificeren met `parseTidyDiff`; plek zoeken met `go list -e -json ./...` (pakketten die een pakket uit de module importeren) en `go/parser` (`ImportsOnly`) voor bestand en regel — werkt ook als `go list` de import niet oplost, dus loop bij een fout van `go list` terug op `go/parser` over alle `.go` van de module; vindt hij niets: bij `shifted` de regel in `go.mod`, bij `missing` geen bevinding (drift). `go` loopt met `GOFLAGS` leeg overgenomen uit de omgeving (niet zelf zetten).
- [ ] **Step 4:** zelfde commando — PASS.
- [ ] **Step 5: Commit** `feat(importcheck): Go-checker op go mod tidy -diff (#383)`.

---

### Taak 4: Python-checker (deptry)

**Files:**
- Create: `internal/importcheck/python.go`, `internal/importcheck/pins.go`
- Test: `internal/importcheck/python_test.go`

**Interfaces:**
- Consumes: taak 2.
- Produces: `const DeptryVersion = "<versie>"` met licentie (MIT) in het commentaar, in `pins.go`; `type PythonChecker struct{ SourceDirs []string; MainCheckout string }`; `func parseDeptry(out []byte) ([]Finding, error)` — JSON van `deptry --json-output`, herschreven naar `Finding` (bestand, regel; kolom vervalt); `func findVenv(projectDir, mainCheckout string) (string, bool)` — eigen `.venv`, anders `<MainCheckout>/.venv`.
- De bouwer bevestigt de exacte aanroep (`uv run --frozen --no-sync --with deptry==<DeptryVersion> deptry <bronmappen> --json-output <tmp>` met `VIRTUAL_ENV`/`UV_PROJECT_ENVIRONMENT` naar de gekozen venv) en het JSON-formaat; zie spec §2.

- [ ] **Step 1: Tests**:
  - `TestParseDeptry`: een JSON-voorbeeld met `DEP001`, `DEP003`, `DEP002`, `DEP004` → alleen `DEP001` en `DEP003` komen uit `Check` als blokkerend; `DEP002`/`DEP004` worden weggelaten.
  - `TestPythonCheckerUnchecked`: geen `uv` (`LookPath`), geen `.venv` (reden exact `no .venv in <project> or the main checkout; run uv sync first.`), onleesbare JSON, deptry kon niet starten → `UncheckedError`.
  - `TestSourceDirsRules`: `SourceDirs=[]` → geen run, geen melding; een niet-bestaande map wordt overgeslagen; geen enkele bestaat → `not checked` (geen bevinding, geen fout).
  - `TestFindVenvFallsBackToMainCheckout`; zonder `MainCheckout` alleen eigen `.venv`.
- [ ] **Step 2:** `go test ./internal/importcheck/ -run 'Python|Deptry|Venv|SourceDirs'` — FAIL.
- [ ] **Step 3: Implementeer** `PythonChecker.Check`, `parseDeptry`, `findVenv`. `SourceDirs` komt uit `python.source_dirs` (gevuld door de CLI in taak 6). Kies de pin: laatste stabiele deptry, ≥ 2 weken oud; noteer de licentietoets in het commentaar.
- [ ] **Step 4:** zelfde commando — PASS.
- [ ] **Step 5: Commit** `feat(importcheck): Python-checker op deptry (#383)`.

---

### Taak 5: Node-checker (knip)

**Files:**
- Create: `internal/importcheck/node.go`
- Modify: `internal/importcheck/pins.go`
- Test: `internal/importcheck/node_test.go`

**Interfaces:**
- Consumes: taak 2.
- Produces: `const KnipVersion = "<versie>"` (ISC verwacht, toets en noem in de PR); `type NodeChecker struct{}`; `func parseKnip(out []byte) ([]Finding, error)` — knip `--reporter json`, alleen `unlisted` (ID `unlisted`).

- [ ] **Step 1: Tests**:
  - `TestParseKnipUnlisted`: JSON met een `unlisted`-bevinding (bestand, importnaam, regel waar knip die geeft; anders regel 1) → één `Finding{ID:"unlisted"}`; andere knip-categorieën worden genegeerd.
  - `TestNodeCheckerUnchecked`: geen `node_modules` in de root (`COULD NOT CHECK — node_modules missing in <root>`, er wordt niet geïnstalleerd), `npx` ontbreekt, onleesbare JSON → `UncheckedError`.
  - `TestNodeCheckerReadsJSONNotExitCode`: exit 1 mét geldige JSON zonder bevindingen = schoon; exit 1 met rommel = `UncheckedError`.
- [ ] **Step 2:** `go test ./internal/importcheck/ -run 'Knip|Node'` — FAIL.
- [ ] **Step 3: Implementeer** `NodeChecker.Check`: `npx --yes knip@<KnipVersion> --include unlisted --reporter json` in `p.Dir`; de binary leest de JSON, niet de exitcode (spec §4).
- [ ] **Step 4:** zelfde commando — PASS.
- [ ] **Step 5: Commit** `feat(importcheck): Node-checker op knip (#383)`.

---

### Taak 6: subcommand `lighthouse imports check`

**Files:**
- Create: `internal/cli/imports.go`
- Modify: `internal/cli/root.go` (registratie)
- Test: `internal/cli/imports_test.go`

**Interfaces:**
- Consumes: `importcheck.Run`, `importcheck.Options`; `config.Run` voor `python.source_dirs` en `imports.check`.
- Produces: `func newImportsCommand() *cobra.Command` — `imports check [--lang python|go|node]... [--files <pad>]...`; exit via `&exitError{code: n, err: errSilent}` zoals `gate.go`.

- [ ] **Step 1: Tests**:
  - `TestImportsCheckUnknownFlagIsExit3` (`--bogus` → code 3, niet cobra's 1) en `TestImportsCheckUnknownLangIsExit3`.
  - `TestImportsCheckDisabledByConfig`: `[imports] check = false` → exit 0, geen uitvoer.
  - `TestImportsCheckPassesFlagsThrough`: met een fake `Checker` (injecteerbaar via een package-variabele of constructor-parameter, zoals `gate_test.go` doet) komt `--files` bij `Options.Files` en `--lang` bij `Options.Langs` aan.
  - `TestImportsCheckPythonFallsBackToMainCheckoutVenv`: `CLAUDE_PROJECT_DIR` naar een map met `.venv`, project zonder eigen `.venv` → geen `no .venv`-melding (met de nep-`Exec`).
  - `TestImportsHelpListsSubcommand`: `lighthouse imports` zonder `check` toont help zonder te slagen als poort (exit 3 `unknown`-stijl, zoals `gate` zonder naam).
- [ ] **Step 2:** `go test ./internal/cli/ -run Imports` — FAIL.
- [ ] **Step 3: Implementeer** `newImportsCommand` naar het model van `newGateCommand`; `SetFlagErrorFunc` zet elke vlagfout om in exit 3 met `imports: COULD NOT CHECK — <fout>` op stderr. `python.source_dirs` lees je met `config.Run`; zet `PythonChecker.SourceDirs` daarmee en `PythonChecker.MainCheckout` uit `CLAUDE_PROJECT_DIR` (leeg buiten Claude Code). Bouw hier ook de `Checkers`-map met de echte checkers. Haal `imports.check` uit `UNREAD` in `tests/test_site_config.py` en voeg de reader `internal/cli/imports.go` toe aan `docs/site/data/config.json`; draai `uv run pytest -q tests/test_site_config.py` — PASS.
- [ ] **Step 4:** zelfde commando — PASS; dan `go build ./cmd/lighthouse && ./lighthouse imports check --bogus; echo $?` → `3`.
- [ ] **Step 5: Commit** `feat(cli): lighthouse imports check (#383)`.

---

### Taak 6b: `lighthouse turn-roots --files`

**Files:**
- Modify: `internal/turnroots/turnroots.go` (`Verdict` + `Classify`), `internal/cli/turnroots.go`
- Test: `internal/turnroots/turnroots_test.go`, `internal/cli/turnroots_test.go`

**Interfaces:**
- Produces: `Verdict.Files []string` — de paden (relatief aan `Root`, met `/`, gesorteerd) van de beurt die een patroon raken: de `rels` die `Classify` al per root bouwt (`toolPaths` plus `changedSince`). Vlag `lighthouse turn-roots --match <p>... --files`: na de `touched`/`untouched`-regels per geraakt pad een regel `file\t<root>\t<pad>`; zonder `--files` blijft de uitvoer gelijk (`ty-gate.sh` verandert niet).
- In twijfel (`hasStart` onwaar, of een git-fout voor die root) is `Touched` waar en `Files` leeg: de hook roept dan `imports check` zonder `--files` voor die root aan (spec §1: zonder `--files` alles).

- [ ] **Step 1: Tests:** `TestClassifyFilesListsMatchedPaths` (twee geraakte paden, één buiten het patroon → alleen de twee), `TestClassifyDoubtHasNoFiles`, `TestTurnRootsFilesFlag` (regelvorm `file\t<root>\t<pad>`, ook met spatie in het pad) en `TestTurnRootsWithoutFilesFlagUnchanged` (bestaande uitvoer ongewijzigd).
- [ ] **Step 2:** `go test ./internal/turnroots/ ./internal/cli/ -run 'Classify|TurnRoots'` — FAIL.
- [ ] **Step 3: Implementeer** `Verdict.Files` en de vlag. Paden met een newline vallen buiten het formaat: sla ze over en laat de hook dan zonder `--files` draaien.
- [ ] **Step 4:** zelfde commando plus `go test -race -shuffle=on ./internal/turnroots/ ./internal/cli/` — PASS.
- [ ] **Step 5: Commit** `feat(turnroots): --files geeft de geraakte paden per checkout (#383)`.

---

### Taak 7: Stop-hooks, instructies en installatie

**Files:**
- Create: `pack/python/hooks/imports-gate-python.sh`, `pack/go/hooks/imports-gate-go.sh`, `pack/node/hooks/imports-gate-node.sh`, `pack/node/hooks/hooks.json`, `pack/node/instructions/AGENTS.md`
- Modify: `pack/python/hooks/hooks.json`, `pack/go/hooks/hooks.json`, `pack/{python,go}/instructions/AGENTS.md`, `pack/node/requires.toml` (`npx = ["lighthouse imports check"]`), `pack/SOURCES.toml` (nieuwe bestanden, `new = true`), `docs/site/data/packs.json` en `docs/site/data/session.json` (de drie hooks; `tests/test_site_packs.py::test_hooks_match_hooks_json` en `tests/test_site_session.py::test_every_hook_is_on_its_event`), `tests/pack/node/test_node_pack.py` (het contract `geen hooks/instructions` vervalt voor `pack/node`: houd alleen `lighthouse.toml.fragment` verboden en toets de nieuwe `hooks/` en `instructions/`)
- Create (fixtures voor `ci/no-python.sh`, dat per registratie een payload eist): `tests/pack/fixtures/hooks/Stop/imports-gate-go.json`, `tests/pack/fixtures/hooks/Stop/imports-gate-node.json` (zelfde vorm als `go-lint-gate.json`; een beurt zonder relevant pad geeft `not checked`, geen `COULD NOT CHECK`; de python-hook valt buiten `no-python`)
- Test: `tests/pack/python/test_imports_gate_hooks.py` (pytest, naar het model van de bestaande `ty-gate`-tests), `internal/packs/core_test.go`-stijl installatietest voor `pack/node` indien de bestaande tests per pack lopen

**Interfaces:**
- Consumes: `lighthouse imports check --lang <taal> --files <…>` (taak 6); `lighthouse turn-roots --match <patroon> --files` (taak 6b).
- Produces: elk script declareert `BLOCKING_RULES="…"` (tabel in Globale eisen) — taak 9 leest dat.

- [ ] **Step 1: Tests** (pytest, hook als subprocess met een nep-`lighthouse` op `PATH`):
  - per hook `test_blocks_on_exit1_with_finding` (nep-binary: exit 1 + regel `a.py:1: DEP003 numpy — x` op stdout → hook exit 2, stderr bevat `DEP003`);
  - `test_could_not_check_on_exit3`, `test_could_not_check_on_exit1_without_finding`, `test_could_not_check_on_unknown_command` (stderr `unknown command`) → exit 0 met `systemMessage` met `COULD NOT CHECK`;
  - `test_not_checked_during_usage_alarm`; `test_not_checked_when_turn_touched_no_relevant_path`; `test_no_block_when_stop_hook_active`;
  - `test_blocking_rules_declared` (de regel `BLOCKING_RULES=` staat in elk van de drie scripts en noemt de ID's uit de tabel).
- [ ] **Step 2:** `uv run pytest -q tests/pack/python/test_imports_gate_hooks.py` — FAIL.
- [ ] **Step 3: Implementeer** de drie scripts naar `ty-gate.sh`/`go-lint-gate.sh` (usage-alarm, `turn-roots --match` met de relevante paden uit spec §5, `COULD NOT CHECK` zonder te blokkeren, exit 2 met bevindingen op stderr). `--files` krijgt de paden uit `lighthouse turn-roots --match … --files` (taak 6b), één `--files` per pad; zonder paden voor een geraakte root (twijfel) draait hij zonder `--files`. Een eigen scriptnaam per pack (alle hooks landen in één map). Registreer in de `hooks.json`'s (Stop, timeout 120); `pack/node` krijgt een nieuwe `hooks.json` met alleen dit Stop-item. Eén regel per pack in de instructies: draai `lighthouse imports check` voor een commit, naast `[test_gate] command`.
- [ ] **Step 4:** zelfde pytest — PASS; `shellcheck` op de nieuwe scripts (de bestaande shell-pack-gate).
- [ ] **Step 5:** `go run ./cmd/lighthouse init --upgrade` en `go run ./cmd/lighthouse init --upgrade` nogmaals → `No changes.`; `go test ./internal/packs/ ./tests/...`-equivalent (`uv run pytest -q tests/test_sources.py tests/test_dogfood.py tests/pack/node/test_node_pack.py tests/test_site_packs.py tests/test_site_session.py`) — PASS; draai `bash ci/no-python.sh` lokaal als de host dat toelaat.
- [ ] **Step 6: Commit** `feat(pack): imports-gate Stop-hooks per pack (#383)`; aparte commit voor de geïnstalleerde kopie: `chore(dogfood): installed copy na init --upgrade (#383)`.

---

### Taak 8: commit-gate draait de check zelf

**Files:**
- Create: `internal/commitgate/imports.go`
- Modify: `internal/commitgate/gate.go` (`RunRoots`: de importstap vóór de vroege terugkeer op r.372-377)
- Test: `internal/commitgate/imports_test.go`

**Interfaces:**
- Consumes: `Row`; `Gate.readConfig("imports.check")`; `ChangedFiles(root, base, head)` (post); een `Gate`-veld `ImportsCheck func(root string, files []string) (stdout, stderr string, code int, err error)` (default: draait `lighthouse imports check --files …` als subprocess in `root`, met `os.Executable()` als binary en niet `lighthouse` van `PATH`: in een dev-checkout draait `run.sh` de gate met de uit `HEAD` gebouwde binary, terwijl `PATH` een oudere kan hebben zonder `imports`; reader-pad `internal/commitgate/imports.go` komt in `docs/site/data/config.json`).
- Produces: `func (g *Gate) runImports(rows []Row) (stop bool, res Result)` — pre: de files zijn `git diff --cached --name-only -z` in `row.Root`; post: `ChangedFiles`.

- [ ] **Step 1: Tests** (fake `ImportsCheck`):
  - `TestImportsFindingIsRed`: exit 1 + bevindingsregel → resultaat blokkeert (exit 2), melding bevat de bevinding en onderscheidt zich van `tests failed`.
  - `TestImportsUncheckedBlocks`: exit 3, en exit 1 zonder bevinding, en `unknown command` op stderr → blokkerend met `COULD NOT CHECK`.
  - `TestImportsRunsWithoutTestCommand`: geen `[test_gate] command` én `command = ""` → de check draait toch, en de melding `no test command configured` blijft er naast staan (review-focus 5).
  - `TestImportsOptOut`: `[imports] check = false` → de fake wordt niet aangeroepen; `[test_gate] command = ""` zet hem niet uit.
  - `TestImportsEmptyFileListSkips`: lege commit/alleen verwijderingen → niet aanroepen, geen blokkade.
  - `TestImportsPreUsesStagedFiles` en `TestImportsPostUsesChangedFiles`.
  - `TestImportsDefaultRunnerUsesOwnExecutable`: de default draait `os.Executable()` (geïnjecteerde `Exec`-naad), niet een `PATH`-lookup.
  - Bekende beperking (pre): `git commit -a` en `git add -A && git commit` in één Bash-aanroep hebben op PreToolUse niets gestaged → lege lijst → overgeslagen; leg het vast in de melding `imports not checked: nothing staged yet` en in de docs, geen workaround.
- [ ] **Step 2:** `go test ./internal/commitgate/ -run Imports` — FAIL.
- [ ] **Step 3: Implementeer** `runImports` en roep hem in `RunRoots` aan vóór het lezen van `test_gate.command`; een stop-resultaat eindigt `RunRoots` (via `blockResult`), anders gaat het door naar de testcommando-tak. Alleen draaien voor de geïnstalleerde packs (`manifest.Read(root)`; ontbreekt het manifest: overslaan). Hou de melding Engels en per bevinding de regel zoals de binary hem gaf.
- [ ] **Step 4:** zelfde commando — PASS; dan `go test -race -shuffle=on ./internal/commitgate/` — PASS.
- [ ] **Step 5: Commit** `feat(commitgate): importcheck-stap onafhankelijk van test_gate.command (#383)`.

---

### Taak 9: nep-repo's, canaries en CI-job

**Files:**
- Create: `internal/importcheck/testdata/imports-gate-{go,node}/<id>/` (fixture-mappen: `indirect-import`, `missing-require`, `unlisted`; de Python-fixtures staan onder `tests/pack/python/testdata/`; `Discover` slaat `testdata` over, taak 2, dus `lighthouse imports check` op deze repo ziet ze niet), `internal/importcheck/canary_test.go` (tag `canary`), `internal/importcheck/fakeenv_test.go` (hulpfuncties: nep-`.dist-info` in een `uv venv`, nep-`node_modules`, Go-`replace` naar een lokale map), `tests/pack/python/test_imports_gate_canary.py`, `tests/pack/python/testdata/imports-gate-python/<id>/`
- Modify: `.github/workflows/ci.yml` (job `canary`, of aanvullen als #381 hem al maakte)

**Interfaces:**
- Consumes: de scripts (taak 7) en de binary (taak 6); `BLOCKING_RULES` per script.
- Produces: job `canary` in CI; per ID een fixture-map met project en manifest.

- [ ] **Step 1: Tests** (`//go:build canary`):
  - `TestCanaryBijection`: parse `BLOCKING_RULES` uit de drie scripts; eist een bijectie tussen ID's en fixture-mappen `testdata/<hook>/<id>/` (ontbrekende of overbodige map = fail).
  - `TestCanaryPerRule`: per ID bouwt de test de omgeving (Python: `uv venv` + nep-`.dist-info` met `METADATA` waarin `b` `Requires-Dist` van `a` is en `top_level.txt`, offline; Node: `node_modules` met alleen `package.json`'s; Go: `replace` naar lokale map, `GOPROXY=off`, `GOFLAGS` leeg) en draait de echte hook; eist exit 2 én de ID in stderr. Elke canary kopieert zijn fixture naar `t.TempDir()`, doet daar `git init` en commit, bouwt de nep-omgeving (venv, `node_modules`) in die kopie, en wijst `cwd`, `CLAUDE_PROJECT_DIR` en het `Write` in het transcript naar die kopie: in de repo zelf zien `turn-roots` en `Discover` de fixture niet (`testdata` wordt overgeslagen) en zou de hook met exit 0 eindigen. `test_imports_gate_canary.py` doet hetzelfde. Zo draai je de hook: bouw de binary één keer naar een tempmap (`go build -o <tmp>/lighthouse ./cmd/lighthouse`), zet die voorop op `PATH`, en geef de hook een Stop-payload met een transcript waarin een `Write` op het fixturebestand staat na de prompt (zodat `turn-roots` de fixture als geraakt ziet; een `untouched`-uitkomst geeft exit 0 om een andere reden dan de regel). De Python-fixtures staan op één plek, `tests/pack/python/testdata/imports-gate-python/<id>/`; de Go-canary leest ze daar (geen kopie in `internal/importcheck/testdata`).
  - Per pack een groene tegenhanger: `numpy` in `[project.dependencies]`, Go-module direct, `lodash` in `dependencies`.
  - Extra's: `try: import httpx except ImportError` met `httpx` in een extra → schoon (AC 3); als deptry dat anders doet: stop en meld een open punt, geen workaround.
  - Node-workspace (hoisted `node_modules` in de root, import in child) faalt zonder declaratie in de child, slaagt met; Expo-nep-repo (`expo` in `package.json`) faalt/slaagt via `pack/node`.
  - Falen in plaats van overslaan: ontbreekt `uv` of `npx` en `CI` is gezet → `t.Fatal`; lokaal `t.Skip` met reden.
  - Python-hook: `test_imports_gate_canary.py` doet hetzelfde voor `imports-gate-python.sh`, met dezelfde skip/fail-regel (`CI`).
- [ ] **Step 2:** `go test -tags canary ./internal/importcheck/` — FAIL (fixtures ontbreken).
- [ ] **Step 3: Implementeer** de fixtures en hulpfuncties. Bevestig dat `go mod tidy -diff` de `missing-require`-hunk offline met `replace` geeft en dat deptry `DEP003` op de nep-`.dist-info` meldt; lukt een van beide niet, stop en meld dat als open punt (spec §Tests).
- [ ] **Step 4:** `go test -tags canary ./internal/importcheck/` en `uv run pytest -q tests/pack/python/test_imports_gate_canary.py` — PASS.
- [ ] **Step 5: CI**: voeg job `canary` toe met `setup-go`, `setup-uv`, `setup-node` (gepinde actions zoals de andere jobs), `go test -tags canary ./internal/importcheck/` en de pytest-canary; `go-test` blijft ongewijzigd. Draait op PR's die Go of `pack/` raken (zoals `ci/domains.py` bepaalt).
- [ ] **Step 6: Commit** `test(importcheck): canary per regel-ID en nep-repo's (#383)`; `ci: job canary voor importcheck (#383)`.

---

### Taak 10: dogfood, docs en changelog

**Files:**
- Modify: bevindingen in `go.mod`, `web/go.mod`, `web/package.json` (declareren, niet uitzetten); docs die hooks of gates opsommen (zoek `go-lint-gate` onder `docs/`; de site-kaarten `packs.json` en `session.json` zijn al in taak 7 gedaan); `CLAUDE.md`-hooklijst alleen als hij hooks per pack noemt (anders niet).
- Create: `changelog.d/<fragment>.md` (Nederlands; naam volgens de bestaande fragmenten)

**Interfaces:**
- Consumes: alles hierboven.

- [ ] **Step 1:** `go build -o /tmp/lh ./cmd/lighthouse` en draai `lighthouse imports check` in deze repo (Go + `web/`). Verwacht: bevindingen óf schoon; `web/node_modules` ontbreekt lokaal → `COULD NOT CHECK`, dan eerst `npm ci` in `web/`.
- [ ] **Step 2:** los elke bevinding op door de dependency te declareren; draai de check tot exit 0. Geen regel uitzetten, geen config versoepelen.
- [ ] **Step 3:** docs bijwerken (`imports check`, `[imports] check`, de drie hooks, de pins) en het changelog-fragment schrijven (wat verandert voor een adopter: nieuwe poort, opt-out, wat `COULD NOT CHECK` betekent).
- [ ] **Step 4:** volledige check: `go test -race -shuffle=on ./...`, `go test -tags canary ./internal/importcheck/`, `uv run pytest -q`, `go run ./cmd/lighthouse init --upgrade` → `No changes.`
- [ ] **Step 5: Commit** `fix(deps): ongedeclareerde directe imports gedeclareerd (#383)` (alleen als er bevindingen waren), `docs(imports): poort op directe imports (#383)`, `docs(changelog): fragment voor #383`.
- [ ] **Step 6:** de PR-tekst (via `/lighthouse-write-pr`) noemt de pins met licentie (deptry MIT, knip ISC) en de open punten uit de spec (`r3-f1` en het extras-gedrag van deptry).

---

## Zelfreview

- **Spec-dekking:** §1 → taak 2, 6; §2 → 4; §3 → 3; §4 → 5, 7 (`requires.toml`); §5 hook → 7, commit-gate → 8, Codex-regel → 7, pin → 4/5; §6 → 10; Tests → 2-5, 8, 9; CI → 9; `imports.check` → 1.
- **Open punten voor de bouwer** staan in de taken: exacte deptry-/knip-flags (4, 5), indeling `importcheck` (2), plek van de commit-gate-stap (8: vóór de vroege terugkeer in `RunRoots`).
- **Typeconsistentie:** `Checker`, `Finding`, `UncheckedError`, `Env.Exec`, `Options` komen uit taak 2 en worden ongewijzigd gebruikt in 3-6 en 8.
- **Risico:** #381 landt mogelijk eerder en maakt de job `canary` en `internal/canary`; taak 9 vult dan de bestaande job aan en neemt het patroon over.
