# `lighthouse lint check`: per taalpack controleren dat de linter is aangesloten — implementatieplan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `lighthouse lint check` stelt per geïnstalleerd taalpack vast dat de linter gepind is, dat zijn config de vloer van de hook aanzet en dat een CI-stap hem met die pin en config laat falen; `init` toont wat ontbreekt en lighthouse draait het in eigen CI.

**Architecture:** Nieuw pakket `internal/lintcheck` met een `Run` naar het model van `internal/importcheck` (vindt de talen, geeft elk een `Checker`, combineert tot exit 0/1/3). Eén gedeeld CI-model (`ci.go`) leest GitHub-workflows en `.gitlab-ci.yml` statisch en levert herkende lintstappen met hun geldigheid; vier checkers (python, go, shell, node) controleren pin en selectie en vragen het CI-model om de stap. De vloer wordt gelezen uit `BLOCKING_RULES` min `NON_LINTER_RULES` van de geïnstalleerde hook. Dunne cobra-laag in `internal/cli/lint.go`; `initcmd` krijgt een `LintCheck`-callback zoals `CheckHooks`.

**Tech Stack:** Go, `gopkg.in/yaml.v3`, `github.com/BurntSushi/toml` (staan al in `go.mod`), cobra. Volgt `/lighthouse-go-standard` en `/lighthouse-test-standard`.

**Spec:** [`2026-11-19-issue-382-linter-aangesloten-design.md`](../specs/2026-11-19-issue-382-linter-aangesloten-design.md); issue #382. #381 en #383 zijn gemerged: `NON_LINTER_RULES` bestaat in `go-lint-gate.sh` en `shell-lint-check.sh`, `internal/importcheck` bestaat.

## Global Constraints

- Go, geen Python; tests in Go (`t.TempDir()` nep-repo's), niet in pytest.
- Code, uitvoer van het subcommand, foutmeldingen en codecommentaar in het Engels; plan en PR in het Nederlands, het changelog-fragment in het Engels (`docs/releasing.md`: één tot drie zinnen voor een gebruiker, in het Engels) (`code_language_paths` bevat `internal/**`).
- Uitvoerregel per ontbrekend onderdeel: `<bestand>[:<regel>]: <linter> — <wat ontbreekt>`; succes: `<linter>: wired` op stdout; exit 0 / 1 / 3 (`gate.Unchecked`), foutmelding bij 3 `lint check: COULD NOT CHECK — <reden>` op stderr; 1 wint van 3, 3 wint van 0.
- Een niet herkende lintstap is exit 1 (`step not recognised`), nooit groen. Geen uitweg in `lighthouse.toml`, geen tweede regellijst naast `BLOCKING_RULES`, geen Go-uitzonderingslijst voor niet-linter-IDs.
- Vloer uit de **geïnstalleerde** hook onder `.claude/hooks/lighthouse/` (`ruff-format-check.sh`, `go-lint-gate.sh`, `shell-lint-check.sh`); ontbrekende hook of hook zonder `BLOCKING_RULES` is exit 3. Node en Expo: geen vloer.
- Pins: `.ruff-version`; `.golangci-version`; Node `eslint` exact in `devDependencies`; shell `SHELLCHECK_PIN` uit geïnstalleerde `.lighthouse/scripts/lib/shellcheck-bin.sh`.
- Paden die de CI-stap lint tellen niet; alleen configbestand telt (uitzondering shell: geen `.shellcheckrc` is goed).
- Exitstatus in CI is een allowlist (spec §3 "Rood in CI"); niet-leesbare `runs-on`/`shell` is `step not recognised`.
- `init` print niets extra bij een aangesloten repo (CI eist exact `No changes.`); exit 1 en 3 laten `init` niet falen.
- Vóór elke commit: `[test_gate] command` en `lighthouse imports check`. Atomic Conventional Commits. Dogfood: na `internal/` of `pack/`-wijziging die `init` raakt, `go run ./cmd/lighthouse init --upgrade` en de kopie committen. Changelog-fragment in `changelog.d/`.

## Review Focus

Invoerklassen die de spec impliceert maar geen taak al dekt; elk heeft een test in de taak die de code bezit (tussen haakjes).

1. Een workflow met `on: [push]` als lijst of string in plaats van map, of `on: workflow_dispatch` alleen: eerste telt, tweede niet (Task 2).
2. `run:` met meerregelige scripts, `\`-regelvoortzetting en `ruff check` in een commentaar of `echo "ruff check"`: commentaar en echo tellen niet (Task 3).
3. Een `package.json` zonder `scripts` of zonder `devDependencies`, of ongeldige JSON: geen panic; ontbrekende pin is exit 1, ongeldige JSON is exit 3 (Task 6).
4. Ruff-config met `extend` die een cyclus vormt of naar een niet-bestaand pad wijst: geen oneindige lus; onleesbaar is exit 3 (Task 4).
5. Lege repo of manifest zonder taalpacks: exit 0 en geen uitvoer; `--lang` voor een niet-geïnstalleerd pack is exit 3 (Task 7).

---

## File Structure

- `internal/lintcheck/lintcheck.go` — `Lang`, `Finding`, `Env`, `Checker`, `Run`, `UncheckedError`, uitvoerformaat.
- `internal/lintcheck/floor.go` — `Floor(root, hook string) ([]string, error)`.
- `internal/lintcheck/ci.go` — CI-bestanden laden, `Step`, `Steps(root, forge)`.
- `internal/lintcheck/ciexit.go` — exitstatus-allowlist en shellbepaling.
- `internal/lintcheck/ciflags.go` — herkenning per linter, flags die de vloer versmallen.
- `internal/lintcheck/python.go`, `golang.go`, `shell.go`, `node.go` — de vier checkers.
- `internal/cli/lint.go` — `lighthouse lint check`.
- `internal/initcmd/initcmd.go` + `internal/cli/init.go` — `LintCheck`-aansluiting.
- `.github/workflows/ci.yml`, `docs/lint-check.md`, `changelog.d/issue-382-linter-aangesloten.md`.

Tests naast elk bestand (`*_test.go`), met één gedeelde helper `writeRepo(t, map[string]string) string` in `helpers_test.go`; de helper doet `git init` en `git add -A`, omdat `importcheck.Discover` alleen gevolgde bestanden ziet (`internal/importcheck/projects.go:97`). Tests voor workspace-kinderen en testdata-fixtures lopen via `Run` zonder `Dirs` en eisen dat er geen regel voor dat pad is.

---

### Task 1: Pakketskelet, uitvoer, vloer

**Files:**
- Create: `internal/lintcheck/lintcheck.go`, `internal/lintcheck/floor.go`, `internal/lintcheck/helpers_test.go`
- Test: `internal/lintcheck/lintcheck_test.go`, `internal/lintcheck/floor_test.go`

**Interfaces:**
- Produces:
  - `type Lang string` — `"python"`, `"go"`, `"node"`, `"shell"`.
  - `type Finding struct{ File string; Line int; Linter, Msg string }` en `func FormatFinding(f Finding) string` (`File[:Line]: Linter — Msg`).
  - `type UncheckedError string` met `Error()`.
  - `type Env struct{ Root string; Stdout, Stderr io.Writer; ForgeKind string }`.
  - `type Checker interface{ Check(dir string, env Env) (findings []Finding, wired []string, err error) }` — `dir` is `"."` of een Node-root; `wired` bevat de linters die volledig aangesloten zijn.
  - `type Options struct{ Root string; Langs []Lang; Dirs []string; Checkers map[Lang]Checker }` en `func Run(o Options, env Env) int`.
  - `func Floor(root, hook string) ([]string, error)` — leest `BLOCKING_RULES` en `NON_LINTER_RULES` uit `<root>/.claude/hooks/lighthouse/<hook>`, geeft het verschil; `UncheckedError` bij ontbrekend bestand of ontbrekende `BLOCKING_RULES`.

- [ ] **Step 1: Write the failing tests** — `TestFloorSubtractsNonLinterRules` (hook met `BLOCKING_RULES="a,b,typecheck"`, `NON_LINTER_RULES="typecheck"` → `["a","b"]`); `TestFloorMissingHookIsUnchecked`; `TestFloorWithoutBlockingRulesIsUnchecked`; `TestRunExitCodes` met nep-`Checker`s: alleen findings → 1; findings plus een `UncheckedError` → 1 met beide meldingen (finding op stdout, `lint check: COULD NOT CHECK — …` op stderr); alleen unchecked → 3; niets → 0 met `<linter>: wired` op stdout; een taal zonder checker → 3, nooit 0.
- [ ] **Step 2: Run** `go test ./internal/lintcheck/ -run 'Floor|Run' -v`. Expected: FAIL (pakket bestaat niet).
- [ ] **Step 3: Implement** de types, `Floor` en `Run` zoals in Interfaces. Talen uit `.lighthouse/manifest.toml` via `manifest.Read` (`python`, `go`, `shell`; `node` en `expo` geven `node`; ontbrekend manifest = geen talen). Voor `node` zonder `Dirs`: `importcheck.Discover(root, "node")` (dekt workspace-roots en `testdata`). `importcheck.manifestLanguages` kent `shell` niet (`internal/importcheck/importcheck.go:161-187`) en blijft ongewijzigd: lintcheck leest `manifest.Read` zelf, met een eigen switch inclusief `shell`.
- [ ] **Step 4: Run** dezelfde tests. Expected: PASS.
- [ ] **Step 5: Commit** `feat(lintcheck): pakketskelet, uitvoer en vloer uit de hook (#382)`.

---

### Task 2: CI-model — bestanden, triggers, stappen

**Files:**
- Create: `internal/lintcheck/ci.go`
- Test: `internal/lintcheck/ci_test.go`

**Interfaces:**
- Consumes: `Env.ForgeKind` uit Task 1 (leeg = `github`).
- Produces:
  - `type Step struct{ File string; Job string; Index int; Line int; Run string; Uses string; With map[string]string; Env map[string]string; WorkDir string; Shell string; RunsOn string; ContinueOnError, JobContinueOnError string; IfFalse bool; GitLab bool; AllowFailure, Manual bool }`
  - `Job` is de jobnaam, `Index` de positie van de stap in die job (0-gebaseerd); `Steps` geeft de stappen in jobvolgorde terug. Een voorafgaande installatie telt alleen met dezelfde `File`+`Job` en een lagere `Index`.
  - `func Steps(root, forgeKind string) ([]Step, error)` — alle stappen van workflows die bij een wijziging draaien; `UncheckedError` bij ongeldige YAML.
  - `func ciFilesPresent(root, forgeKind string) bool`.

- [ ] **Step 1: Write the failing tests** — `TestStepsIncludesPushAndPullRequestWorkflows`, `TestStepsOnAsListOrString` (Review Focus 1: `on: [push]` en `on: push` tellen), `TestStepsSkipsWorkflowDispatchOnly`, `TestStepsFollowsWorkflowCallOneLevel` (aanroep via `uses: ./.github/workflows/lint.yml`; twee niveaus diep telt niet), `TestStepsWorkingDirectoryFromDefaults` (stap > job-defaults > workflow-defaults), `TestStepsIfFalseLiteralIsMarked` (`if: false` → `IfFalse`; `if: github.event_name == 'push'` niet), `TestStepsInvalidYAMLIsUnchecked`, `TestStepsMissingCIFilesIsEmptyNotError` (exit 1 komt van de checker), `TestStepsReadsYamlExtension`.
- [ ] **Step 2: Run** `go test ./internal/lintcheck/ -run Steps -v`. Expected: FAIL.
- [ ] **Step 3: Implement** `Steps` met `yaml.v3` Node-API zodat regelnummers (`Step.Line`) meekomen; volg `internal/pusher/policy.go` voor het workflow-lezen. Alleen GitHub in deze taak; GitLab komt in Task 3.
- [ ] **Step 4: Run** dezelfde tests. Expected: PASS.
- [ ] **Step 5: Commit** `feat(lintcheck): GitHub-workflows lezen tot stappen (#382)`.

---

### Task 3: CI-model — herkenning, flags, exitstatus, GitLab

**Files:**
- Create: `internal/lintcheck/ciexit.go`, `internal/lintcheck/ciflags.go`
- Modify: `internal/lintcheck/ci.go` (GitLab-stappen)
- Test: `internal/lintcheck/ciexit_test.go`, `internal/lintcheck/ciflags_test.go`

**Interfaces:**
- Consumes: `Step`, `Steps` uit Task 2.
- Produces:
  - `type Verdict int` met `NotFound`, `Valid`, `Unrecognised`, `Narrowed`, `Unpinned` (`Verdict` per linter-aanroep).
  - `type Call struct{ Step Step; Linter string; Args []string; Verdict Verdict; Reason string }`
  - `func Calls(steps []Step, linter string, pkgJSON func(dir string) map[string]string, floor []string, cfgNarrows func(dir, cfgPath string) bool) []Call` — `linter` is `ruff`, `golangci-lint`, `shellcheck` of `eslint`; `pkgJSON` geeft de `scripts` van de `package.json` in een map (voor `npm run <script>`, één niveau); `floor` zijn de vloercodes uit `Floor` voor de `Narrowed`-beoordeling; `cfgNarrows` laat de checker van het pack beoordelen of een `--config <bestand>` de vloer mist (ruff, golangci-lint) of geen keuze bevat (eslint); `Calls` kan dat zelf niet. Aggregatieregel: een linter is aangesloten als minstens één herkende aanroep `Valid` is; anders wordt de eerste niet-`Valid` herkende aanroep gemeld, en zonder herkende aanroep `no CI step runs <linter>`. Een `uses:`-actie met `install-only: true` is geen lintaanroep (`.github/workflows/ci.yml:242`); `args:` van een actie volgt dezelfde `Narrowed`- en exit-zero-regels als `run:`.
  - `func effectiveShell(s Step) (name string, errexit bool, ok bool)`.

- [ ] **Step 1: Write the failing tests** (tabeltests, één rij per spec-geval):
  - Herkenning: `ruff check`, `uvx ruff@0.9.0 check`, `astral-sh/ruff-action`, `golangci-lint run`, de golangci-actie, `shellcheck x.sh`, `.lighthouse/scripts/shellcheck-repo.sh`, `eslint .`, `expo lint`, `npm run lint` (script `eslint .`). Niet herkend: `make lint`, `just lint`, `pre-commit run`, `ratchet.py`; `echo "ruff check"` en `# ruff check` tellen niet (Review Focus 2); `\`-voortzetting en meerregelige `run:` worden samengevoegd.
  - Exitstatus `Valid`: eigen regel, `&&`-keten, `shell: bash` met pipe (pipefail), geen shell op `ubuntu-latest`/`macos-latest`. `Unrecognised`: `|| true`, `| tee` zonder pipefail, `; ` na de aanroep, `set +e` ervoor, `continue-on-error` op stap of job anders dan letterlijk `false`/afwezig (ook een `${{ }}`-expressie), `shell: bash {0}` (ook via `defaults.run.shell`), `shell: pwsh`, `runs-on: windows-latest` zonder shell, `runs-on: ${{ matrix.os }}`, `--exit-zero`, `--issues-exit-code=0`. `uses:`-actie zonder `continue-on-error` is `Valid`.
  - Bereik telt niet: `ruff check --exclude migrations/` blijft `Valid`.
  - Pin voor `run:`-aanroepen: een herkende aanroep zonder pinbewijzende installatie (`ruff@<v>`, `ruff==<v>`, `go install …@<pin>`, `shellcheck-repo.sh`, of de actie met `version:`) in dezelfde of een eerdere stap van dezelfde job is `Unpinned`; `go install …@latest` ook. Tests per linter: kale aanroep, installatie op de pin, installatie op `@latest`.
  - `Narrowed`: `ruff check --select E9`, `--ignore <vloercode>`, `--extend-ignore`; `golangci-lint run --disable`/`--enable-only`/`--default none`; `shellcheck -e SC2164`, `--severity=error` boven een vloercode, `SHELLCHECK_OPTS: -e SC2164` in `env:` van workflow, job of stap; `.shellcheckrc` met `severity=error` boven een vloercode (de bouwer bevestigt of `severity=` een geldige rc-sleutel is; zo niet, leg dat vast in een test); `eslint --no-config-lookup` zonder eigen `--config`; `--config <bestand>` bij ruff/golangci-lint/eslint via `cfgNarrows`; `shellcheck -i SC2086` (zonder vloercode) en `--include`; actie-invoer `install-only: true` (geen aanroep), `args: --issues-exit-code=0` (`Unrecognised`), `args: --disable errcheck` (`Narrowed`); twee aanroepen voor één linter (één `Valid` en één `Narrowed` → aangesloten). De vloercodes komen binnen via de `floor`-parameter van `Calls`; geen hardgecodeerde lijst.
  - GitLab (`.gitlab-ci.yml`): lintjob `Valid`; `allow_failure: true`, `allow_failure: {exit_codes: 1}`, `when: manual`, `when: never`, `rules:` met `when: manual|never` → `Unrecognised`.
- [ ] **Step 2: Run** `go test ./internal/lintcheck/ -run 'Calls|Exit|Flags|GitLab' -v`. Expected: FAIL.
- [ ] **Step 3: Implement.** `run:` ontleden met een kleine tokenizer (regels samenvoegen, commentaar en `echo`-argumenten wegstrippen, splitsen op `&&`, `||`, `;`, `|`); geen volledige shell-parser. Alles wat de tokenizer niet kan plaatsen is `Unrecognised`.
- [ ] **Step 4: Run** dezelfde tests. Expected: PASS.
- [ ] **Step 5: Commit** `feat(lintcheck): lintstappen herkennen en exitstatus-allowlist (#382)`.

---

### Task 4: Checker Python

**Files:**
- Create: `internal/lintcheck/python.go`
- Test: `internal/lintcheck/python_test.go`

**Interfaces:**
- Consumes: `Floor(root, "ruff-format-check.sh")`, `Steps`, `Calls` (Task 1–3).
- Produces: `type PythonChecker struct{}` met `Check(dir string, env Env) ([]Finding, []string, error)`; `func ruffEffective(cfgPaths []string, root string, code string) (bool, error)` (ruff-voorrang: meest specifieke prefix wint).

- [ ] **Step 1: Write the failing tests** (spec §Tests, Python): `TestPythonWired` (`.ruff-version`, `ruff.toml` met `select` dat de acht vloercodes dekt via prefix `B` en `E722`/`F`/`PT`/`PGH`, CI-stap `uvx ruff@0.9.0 check`); `TestPythonNoCIStep` (`no CI step runs ruff check`); `TestPythonFloorCodeInIgnore`; `TestPythonFloorCodeInExtendIgnore`; `TestPythonMoreSpecificSelectWins` (`select=["PT012"]`, `ignore=["PT"]` laat PT012 aan); `TestPythonSelectAll`; `TestPythonPyprojectToolRuffLint`; `TestPythonExtendFollowed` en `TestPythonExtendCycleTerminates` (Review Focus 4; onleesbaar pad → `UncheckedError`); `TestPythonPinMissing` (`.ruff-version: ruff — pin missing`); `TestPythonCIVersionMismatch` (`ruff@0.8.0` vs pin `0.9.0`, melding met regelnummer); `TestPythonUnpinnedCIInstall` (`pip install ruff`); `TestPythonVersionFromCat` (`ruff@"$(cat .ruff-version)"` volgt de pin); `TestPythonConfigMissing` (`ruff: config missing`); `TestPythonFloorFromHook` (nep-hook met extra code; config zonder die code faalt).
- [ ] **Step 2: Run** `go test ./internal/lintcheck/ -run Python -v`. Expected: FAIL.
- [ ] **Step 3: Implement** `PythonChecker`. Selectie via `toml.DecodeFile`; `extend` recursief met een bezocht-set tegen cycli. Pin-vergelijking: CI-versie uit `ruff@<v>`, `ruff==<v>` of `with: version:`; `$(cat .ruff-version)` telt als gelijk.
- [ ] **Step 4: Run** dezelfde tests. Expected: PASS.
- [ ] **Step 5: Commit** `feat(lintcheck): ruff controleren op pin, selectie en CI-stap (#382)`.

---

### Task 5: Checkers Go en shell

**Files:**
- Create: `internal/lintcheck/golang.go`, `internal/lintcheck/shell.go`
- Test: `internal/lintcheck/golang_test.go`, `internal/lintcheck/shell_test.go`

**Interfaces:**
- Consumes: Task 1–3.
- Produces: `type GoChecker struct{}`, `type ShellChecker struct{}`, beide met de `Checker`-signatuur; `func shellcheckCodeSeverity(code string) string` (de koppeling vloercode → ernst voor de `severity=`-regel; bouwer kiest de tabel uit de shellcheck-wiki, dekt de vloercodes van `shell-lint-check.sh`).

- [ ] **Step 1: Write the failing tests.**
  - Go: `TestGoWired` (`.golangci-version`, `.golangci.yml` met `version: "2"` en `linters.default: standard`, golangci-actie `version: v2.15.0`); `TestGoNoCIStep`; `TestGoCIVersionMismatch` (CI ≠ pin, melding `CI version v2.14.0, pin v2.15.0` met regelnummer); `TestGoConfigWithoutDefault` (geen `linters.default` → melding); `TestGoConfigMissingWithOtherwiseCompleteSetup` (`golangci-lint: config missing`, exit 1); `TestGoV1ConfigNotVersion2` (`config not version 2`); `TestGoFloorLinterInDisable`; `TestGoDefaultAllAndEnable`; `TestGoFloorExcludesTypecheck` (`typecheck` uit `NON_LINTER_RULES` is geen eis); `TestGoConfigAsTomlJson` (`.golangci.toml`, `.golangci.json`).
  - Shell: `TestShellNoShellcheckrcIsFine` (CI met `shellcheck-repo.sh`); `TestShellrcWithoutFloorDisable`; `TestShellrcDisablesFloorCode`; `TestShellrcDisableAll`; `TestShellrcDisableRange` (`SC2000-SC2999`); `TestShellOptsEnvNarrows` (`SHELLCHECK_OPTS: -e SC2164` op workflow, job en stap); `TestShellNoCIStep`; `TestShellPinFromInstalledLib` (`SHELLCHECK_PIN` uit `.lighthouse/scripts/lib/shellcheck-bin.sh`; ontbrekend → `UncheckedError`); `TestShellCIInstallVersionMismatch`; `TestShellUnpinnedAptInstall`; `TestShellFloorExcludesStrictStyle`.
- [ ] **Step 2: Run** `go test ./internal/lintcheck/ -run 'Go|Shell' -v`. Expected: FAIL.
- [ ] **Step 3: Implement** beide. Go-config via `yaml.v3` (en `toml`/`encoding/json` voor de andere extensies), mapping naar één struct. Shell: `.shellcheckrc` regel voor regel; `disable=` met komma's, `all` en bereiken tegen de vloercodes.
- [ ] **Step 4: Run** dezelfde tests. Expected: PASS.
- [ ] **Step 5: Commit** `feat(lintcheck): golangci-lint en shellcheck controleren (#382)`.

---

### Task 6: Checker Node en Expo

**Files:**
- Create: `internal/lintcheck/node.go`
- Test: `internal/lintcheck/node_test.go`

**Interfaces:**
- Consumes: Task 1–3; `Check(dir, env)` krijgt de Node-root als `dir`.
- Produces: `type NodeChecker struct{}`; `func exactVersion(v string) bool` (geen `^`, `~`, bereik, tag).

- [ ] **Step 1: Write the failing tests** (spec §Tests, Node/Expo): `TestNodeWired` (exacte `eslint`, `eslint.config.js` met `extends`, CI `npm ci` + `npm run lint` met `defaults.run.working-directory`); `TestNodeNoCIStep`; `TestNodeEslintCaret` (`^10.0.0` → pin-melding); `TestNodeConfigOnlyIgnores`; `TestNodeConfigSignsOfChoice` (`rules`, `extends`, `configs.`, `eslint-config-expo`); `TestNodeConfigMissing`; `TestNodeLockfileInstallRequired` (lintstap zonder voorafgaande `npm ci`/`pnpm install --frozen-lockfile`/`yarn install --immutable`/`bun install --frozen-lockfile` → melding); `TestNodeWorkspaceChildNoFinding` (root met `workspaces`, child krijgt geen melding); `TestNodeTestdataFixtureIgnored`; `TestNodeExpo` (`expo` in `package.json`, `"lint": "expo lint"`; zonder CI-stap faalt het); `TestNodeNoScriptsNoDevDependencies` en `TestNodeInvalidPackageJSONIsUnchecked` (Review Focus 3); `TestNodeFlagNoConfigLookup` (`--no-config-lookup` zonder `--config` is `Narrowed`).
- [ ] **Step 2: Run** `go test ./internal/lintcheck/ -run Node -v`. Expected: FAIL.
- [ ] **Step 3: Implement** `NodeChecker`. De check parseert geen JavaScript: alleen tekstuele tekens in het configbestand. `pkgJSON` voor `Calls` leest `scripts` van de `package.json` in de map van de stap.
- [ ] **Step 4: Run** dezelfde tests. Expected: PASS.
- [ ] **Step 5: Commit** `feat(lintcheck): eslint controleren voor Node en Expo (#382)`.

---

### Task 7: CLI `lighthouse lint check`

**Files:**
- Create: `internal/cli/lint.go`
- Modify: `internal/cli/root.go` (voeg `root.AddCommand(newLintCommand())` toe naast `newImportsCommand()`)
- Test: `internal/cli/lint_test.go`

**Interfaces:**
- Consumes: `lintcheck.Run`, `Options`, `Env`, de vier checkers.
- Produces: `func newLintCommand() *cobra.Command`; `func lintRun(root string, langs []lintcheck.Lang, dirs []string, forgeKind string, out, errOut io.Writer) int` (hergebruikt door Task 8).

- [ ] **Step 1: Write the failing tests**, naar `imports_test.go`: `TestLintCheckWiredRepoExit0`; `TestLintCheckMissingExit1WithLines`; `TestLintCheckUnknownLangExit3`; `TestLintCheckUnknownSubcommandExit3`; `TestLintCheckUnexpectedArgExit3`; `TestLintCheckFlagErrorExit3` (alle gebruiksfouten 3, zoals `imports check`); `TestLintCheckEmptyManifestExit0Silent` (Review Focus 5); `TestLintCheckLangNotInstalledExit3`; `TestLintCheckDirFlagNodeRoot`; `TestLintCheckForgeKindFromConfig` (GitLab-repo leest `.gitlab-ci.yml`; zonder `forge.kind` is het GitHub, naar `internal/config/config.go:686`).
- [ ] **Step 2: Run** `go test ./internal/cli/ -run LintCheck -v`. Expected: FAIL.
- [ ] **Step 3: Implement** `newLintCommand` met `Use: "lint check [--lang python|go|node|shell]... [--dir PATH]..."` en dezelfde vorm als `newImportsCommand` (RunE op de ouder geeft 3, `SetFlagErrorFunc`, `errSilent` + `exitError{code}`). Stderr-prefix `lint check: COULD NOT CHECK — `. Forge-kind via `importsConfig("forge.kind")`.
- [ ] **Step 4: Run** dezelfde tests. Expected: PASS. Controleer ook `go run ./cmd/lighthouse lint check` in deze worktree.
- [ ] **Step 5: Commit** `feat(cli): lighthouse lint check (#382)`.

---

### Task 8: Aansluiting op `init` en `init --upgrade`

**Files:**
- Modify: `internal/initcmd/initcmd.go` (veld `LintCheck func(root string, out, errOut io.Writer)` (zonder returnwaarde, zoals `CheckHooks`), aanroep direct na `o.CheckHooks`, ook bij `No changes.`), `internal/cli/init.go` (`o.LintCheck = lintAfterInit`)
- Test: `internal/initcmd/initcmd_test.go`, `internal/cli/init_test.go`

**Interfaces:**
- Consumes: `lintRun` (Task 7).
- Produces: `func lintAfterInit(root string, out, errOut io.Writer)` — roept `lintRun`, laat de uitvoer van een volledig aangesloten repo leeg (onderdruk `<linter>: wired`), print bij 1 of 3 een kop `lint check: some linters are not wired` gevolgd door de regels, alles op `errOut` (zoals `checkHooksAfterInit`, `internal/cli/checkhooks.go:21`) zodat stdout exact `No changes.` blijft, wijzigt de exitcode van `init` nooit.

- [ ] **Step 1: Write the failing tests** — `TestInitPrintsNothingForWiredRepo` (uitvoer is exact `No changes.` bij een tweede `init --upgrade` op een aangesloten repo); `TestInitShowsMissingUnderHeading`; `TestInitDoesNotFailOnLintExit1And3`; `TestUpgradeRunsLintCheck`. Een `lintCheck`-variabele in `cli` is vervangbaar door de test, zoals `hooksCheck`.
- [ ] **Step 2: Run** `go test ./internal/initcmd/ ./internal/cli/ -run 'Init|Upgrade' -v`. Expected: FAIL.
- [ ] **Step 3: Implement.** In `lintAfterInit` de stdout van `lintRun` bufferen en alleen tonen als de code ≠ 0.
- [ ] **Step 4: Run** dezelfde tests plus `go test ./internal/initcmd/ ./internal/cli/`. Expected: PASS.
- [ ] **Step 5: Commit** `feat(init): lint check na de installatie als waarschuwing (#382)`.

---

### Task 9: Lighthouse zelf, CI-job, documentatie, fragment

**Files:**
- Modify: `.github/workflows/ci.yml` (nieuwe job `lint-check`)
- Create: `docs/lint-check.md`, `changelog.d/issue-382-linter-aangesloten.md`
- Test: `internal/cli/lint_self_test.go` (build-tag-vrij; `lintRun` is niet-geëxporteerd in `cli` en `lintcheck` kan `cli` niet importeren)

- [ ] **Step 1: Write the failing test** `TestLighthouseItselfIsWired` (in `internal/cli`): draait `lintRun` op de repo-root (via `config.RepoRoot()`/`../..`) met `ForgeKind: "github"` en eist exit 0. Bewijst dat de check niet slaagt door een versoepelde config. Een tweede test `TestCIRunsLintCheckJob` leest `.github/workflows/ci.yml` en eist dat job `lint-check` een stap `go run ./cmd/lighthouse lint check` heeft, zonder `needs:` en zonder `if:` (AC 5).
- [ ] **Step 2: Run** `go test ./internal/cli/ -run TestLighthouseItselfIsWired -v`. Expected: FAIL als lighthouse niet aangesloten is, anders PASS; bij FAIL sluit je aan (pin, CI-stap), nooit de config versoepelen (spec §5).
- [ ] **Step 3: Implement** de job `lint-check` in `ci.yml`: `runs-on: ubuntu-latest`, `timeout-minutes: 5`, checkout met `persist-credentials: false`, `setup-go` met `go-version-file: go.mod`, stap `go run ./cmd/lighthouse lint check`; geen `needs: changes` en geen `if:` (spec §4). Gebruik dezelfde actie-SHA's als de `go`-job. Deze taak wijzigt `.github/workflows/`: in een controller-run gaat de push via de workflow-pusher (`next: push-workflows`), `workflow-push-gate` blokkeert hem anders. Schrijf `docs/lint-check.md` (Nederlands): wat de check eist per pack, de herkende aanroepen, exitcodes, dat een niet herkende stap exit 1 geeft, en de keuze dat paden niet tellen. Schrijf het fragment (`type: added`, Engels, één tot drie zinnen, naar `docs/releasing.md`).
- [ ] **Step 4: Run** `go run ./cmd/lighthouse lint check` (exit 0, `<linter>: wired` per linter), `go run ./cmd/lighthouse init --upgrade` (verwacht `No changes.`), de test uit Step 1, `lighthouse imports check`, en `[test_gate] command` uit `lighthouse.toml`. Expected: alles groen.
- [ ] **Step 5: Commit** `feat(ci): lint check in eigen CI, documentatie en changelog (#382)`.
- [ ] **Step 6:** Draai de pakketten van deze branch met `-race` (`go test -race ./internal/lintcheck/ ./internal/cli/ ./internal/initcmd/`); rood wordt in een nieuwe commit hersteld.

---

## Self-Review

- **Spec-dekking:** §1 subcommand en uitvoer/exit → Task 1, 7; §2 vloer → Task 1; §3 pin/selectie/CI per pack → Task 4–6, CI-model → Task 2–3; GitLab → Task 3; §4 `init`, CI-job, docs → Task 8, 9; §5 lighthouse zelf → Task 9; Tests-tabel: Python/Go/Node/Workspace/Fixture/Expo/Shell/GitLab en de "Plus"-lijst zijn elk verdeeld over Task 3–7; `init` stil bij aangesloten repo → Task 8.
- **Open na review r3-f1** (shell via `defaults.run.shell` of Windows-runner): gedekt door `effectiveShell` en de rijen `shell: bash {0}` via defaults en `windows-latest` in Task 3.
- **Typeconsistentie:** `Checker.Check(dir, env) ([]Finding, []string, error)` in Task 1, 4, 5, 6; `Calls(steps, linter, pkgJSON, floor)` uit Task 3 wordt in Task 4–6 zo aangeroepen; `lintRun` in Task 7 en 8.
- **Proportie:** het plan beschrijft interfaces en testnamen; de bouwer kiest patronen per herkende aanroep (spec "Wat aan de bouwer is").
