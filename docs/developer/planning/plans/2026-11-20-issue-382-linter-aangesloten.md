# Linter aangesloten per taalpack — implementatieplan

> **Voor agentic workers:** VERPLICHTE SUB-SKILL: gebruik superpowers:subagent-driven-development (aanbevolen) of superpowers:executing-plans om dit plan taak voor taak uit te voeren. Stappen gebruiken checkbox-syntax (`- [ ]`).

**Doel:** `lighthouse lint check` stelt per geïnstalleerd taalpack vast dat de linter gepind is, de vloer van de hook aanzet en rood wordt in CI.

**Architectuur:** Eén pakket `internal/lintcheck` met een gemeenschappelijk CI-model (YAML → stappen met effectieve shell, trigger, `if`, exitstatus-allowlist) en per taal een `checkX(env) []Finding`. Een dunne cobra-laag in `internal/cli` en een in-process aanroep in `initcmd`. De vloer komt uit `BLOCKING_RULES` van de geïnstalleerde hook; er komt geen tweede lijst.

**Tech Stack:** Go, `gopkg.in/yaml.v3`, `github.com/BurntSushi/toml` (beide al in `go.mod`), cobra.

**Spec:** [`../specs/2026-11-19-issue-382-linter-aangesloten-design.md`](../specs/2026-11-19-issue-382-linter-aangesloten-design.md). Lees ook `/lighthouse-go-standard` en `/lighthouse-test-standard`.

## Global Constraints

- Go, geen Python; tests in Go (`t.TempDir()` nep-repo's). Code, uitvoer van het subcommand en hook-meldingen in het Engels; spec en PR in het Nederlands; het `changelog.d/`-fragment in het Engels (`docs/releasing.md`).
- Uitvoerregel per ontbrekend onderdeel: `<bestand>[:<regel>]: <linter> — <wat ontbreekt>`; bij succes `<linter>: wired` op stdout.
- Exit 0 aangesloten, 1 iets ontbreekt, 3 kon niet controleren (`lint check: COULD NOT CHECK — <reden>` op stderr; `gate.Unchecked`). Ontbrekend wint van 3, 3 wint van 0.
- Exit 3 alleen als vloer of CI niet te lezen is: ongeldige YAML, onleesbaar manifest, ontbrekende geïnstalleerde hook uit de vloertabel, of die hook zonder `BLOCKING_RULES`. Ontbrekend CI-bestand, pin of config is exit 1.
- Een niet herkende lintstap (`make lint`, `just lint`, pre-commit, eigen script) is exit 1, `no CI step runs <linter>` of `step not recognised`; nooit een stille groene uitkomst.
- Geen uitweg in `lighthouse.toml`; geen tweede regellijst naast `BLOCKING_RULES`; geen uitzonderingslijst in Go.
- Paden die de CI-stap lint tellen niet (besluit 1b): `ruff check --exclude migrations/` blijft geldig.
- Alleen een configbestand telt als selectie (behalve shell zonder `.shellcheckrc`); CI-flags tellen alleen mee als ze de vloer uitzetten of versmallen.
- Vloer uit de **geïnstalleerde** hook onder `.claude/hooks/lighthouse/`: Python `ruff-format-check.sh`, Go `go-lint-gate.sh`, Shell `shell-lint-check.sh`; Node/Expo geen vloer. Andere hooks met `BLOCKING_RULES` tellen niet.
- CI-bestanden: `.github/workflows/*.yml|*.yaml` bij `forge.kind = "github"` (ook zonder `forge.kind`), `.gitlab-ci.yml` bij `"gitlab"`.
- Bouwt pas op de merge van #381 (de vloer voor Go en shell en het machineleesbare onderscheid linterregel/reden). `lighthouse init` mag nooit falen op exit 1 of 3 van de check en print niets extra bij een aangesloten repo (CI eist exact `No changes.`).
- Dogfood: `go run ./cmd/lighthouse init --upgrade` moet `No changes.` blijven geven; een fragment in `changelog.d/`.

## Review Focus

Invoerklassen die de spec impliceert en die het meest zullen bijten; elk heeft een test in de genoemde taak.

1. Repo zonder `.lighthouse/manifest.toml` of zonder enig taalpack: geen paniek en geen valse groen; verwacht exit 3 bij onleesbaar manifest, anders exit 0 zonder uitvoer (Taak 1).
2. Een `run:`-blok met meerdere regels, regelvoortzetting (`\`), commentaarregels en `ruff check` in een `echo`/commentaar: alleen een echte aanroep telt (Taak 3).
3. Workflow met YAML-anchors, matrix, `runs-on` als expressie of een lijst; geen paniek, expressie wordt `step not recognised` (Taak 3).
4. Ruff-config met `extend`-cyclus, ontbrekend `extend`-doel, of `pyproject.toml` zonder `[tool.ruff.lint]`: melding in plaats van een lus of paniek (Taak 4).
5. `package.json` zonder `scripts`, met `lint` dat `eslint . || true` is, of met ongeldige JSON; geen paniek, juiste melding (Taak 7).

---

### Taak 0: Voorwaarde #381 (geen code)

- [ ] **Stap 1: Controleer dat #381 gemerged is**

Run: `cd <worktree> && git fetch origin && for f in pack/go/hooks/go-lint-gate.sh pack/shell/hooks/shell-lint-check.sh; do git show origin/main:$f | grep -q BLOCKING_RULES && echo "ok $f"; done`
Verwacht: `ok` voor beide. Zo niet: stop en meld dat #381 nog niet op `main` staat (blocked-by). De branch staat al op origin: werk hem bij met `git merge origin/main` (nooit rebase plus force-push), draai daarna `go run ./cmd/lighthouse init --upgrade </dev/null` en commit het geïnstalleerde exemplaar (`.claude/hooks/lighthouse/`), zodat `ReadFloor` en AC 5 de nieuwe hooks lezen. Controleer pas dan met `grep -l BLOCKING_RULES pack/go/hooks/go-lint-gate.sh pack/shell/hooks/shell-lint-check.sh .claude/hooks/lighthouse/go-lint-gate.sh .claude/hooks/lighthouse/shell-lint-check.sh`.

- [ ] **Stap 2: Lees het formaat dat #381 mergede**

Lees in beide hooks hoe een linterregel van een blokkeerreden (`typecheck`, `strict-style`) wordt onderscheiden. Naar het #381-plan: Go markeert reden-ID's in een aparte regel `NON_LINTER_RULES="typecheck"`; de shell-hook zet `strict-style` zonder marker in `BLOCKING_RULES` en alle vloercodes zijn daar severity `warning`. Is dat zo gemerged: `ReadFloor` leest voor Go `NON_LINTER_RULES` en sluit die uit; voor shell houdt hij alleen ID's die `^SC[0-9]+$` matchen (een patroon, geen lijst). Wijkt het gemergede formaat af, pas dan alleen die body aan. De signatuur van `lintcheck.ReadFloor` in Taak 2 vat dat onderscheid in één functie; pas alleen de body daar aan, nooit een lijst in Go.

---

### Taak 1: Pakketskelet, uitvoer, exitcodes, taalset en subcommand

**Files:**
- Create: `internal/lintcheck/lintcheck.go`, `internal/lintcheck/lintcheck_test.go`
- Create: `internal/cli/lintcheck.go`, `internal/cli/lintcheck_test.go`
- Modify: `internal/cli/root.go` (naast `newCheckInstructionsCommand()`, regel ~104)

**Interfaces:**
- Produces:
  ```go
  type Finding struct{ File string; Line int; Linter, Msg string }   // String(): "<File>[:<Line>]: <Linter> — <Msg>"
  type Unchecked struct{ Reason string }                              // implements error
  type Options struct{ Root string; Langs []string; Dirs []string }
  // Run returns the findings, the linters that are wired, and the exit code
  // (0, 1 or gate.Unchecked), after writing to out and errOut.
  func Run(opts Options, out, errOut io.Writer) int
  // checker is what each language implements; wired is the linter name on success.
  type checker func(env *env) (findings []Finding, err error)
  ```
  `env` bundelt `Root`, `Dirs`, forge-kind, geladen CI-bestanden (Taak 3) en `Floor func(lang string) ([]string, error)` (Taak 2).
- Taalset: `Langs` leeg → uit `.lighthouse/manifest.toml` (`manifest.Read`; `Packages` bevat `python`, `go`, `node`, `shell`; `expo` telt als `node`). Een ontbrekend manifest is geen fout (geen packs); een onleesbaar manifest is `Unchecked`.

- [ ] **Stap 1: Schrijf de falende tests**

`TestRunNoManifestIsZeroAndSilent`, `TestRunUnreadableManifestIsUnchecked` (exit 3, stderr `lint check: COULD NOT CHECK — `), `TestExitPriority` (tabel: findings+unchecked → 1 met beide meldingen; alleen unchecked → 3; niets → 0), `TestFindingString` (`.golangci.yml: golangci-lint — floor rule errcheck disabled`, en met regel `.github/workflows/ci.yml:151: …`), `TestLangFlagLimitsPacks` (manifest met python+go, `Langs: ["go"]` toetst alleen go). In `cli`: `TestLintCheckCommandUsageError` (onbekende vlag en onbekend subcommando → exit 3 `gate.Unchecked` met `lint check: COULD NOT CHECK — usage: …` op stderr, via `SetFlagErrorFunc` en `&exitError{code: gate.Unchecked}` zoals `internal/cli/gate.go:41`; de test eist exact 3, niet cobra's 1 of de 2 van `checkinstructions.go`).

- [ ] **Stap 2: Run, verwacht FAIL**

Run: `go test ./internal/lintcheck ./internal/cli -run 'Lint|Run|Exit|Finding|Lang' -count=1`
Verwacht: FAIL (pakket bestaat niet).

- [ ] **Stap 3: Implementeer** `Run`, `Finding.String`, de taalset-resolutie en `newLintCommand()` (`lighthouse lint check [--lang …] [--dir …]…`, een `lint`-groep met subcommand `check`). Registers in `root.go`. Talen zonder checker (nog) leveren niets op; de checkers staan in een package-level `var checkers = map[string]checker{...}` die Taak 4 t/m 7 elk uitbreiden (geen `init()`); `Run` leest die variabele, zodat `TestExitPriority` hem tijdelijk kan vervangen door nepcheckers die findings en `Unchecked` leveren.

- [ ] **Stap 4: Run, verwacht PASS** (zelfde commando).

- [ ] **Stap 5: Commit**

```bash
git add internal/lintcheck internal/cli/lintcheck.go internal/cli/lintcheck_test.go internal/cli/root.go
git commit -m "feat(lintcheck): skelet voor lighthouse lint check (#382)"
```

---

### Taak 2: De vloer uit de geïnstalleerde hook

**Files:**
- Create: `internal/lintcheck/floor.go`, `internal/lintcheck/floor_test.go`

**Interfaces:**
- Produces: `func ReadFloor(root, lang string) ([]string, error)`. Hooktabel (spec §2): `python` → `.claude/hooks/lighthouse/ruff-format-check.sh`, `go` → `go-lint-gate.sh`, `shell` → `shell-lint-check.sh`; `node` geeft `nil, nil` (geen vloer). Leest de waarde van `BLOCKING_RULES="…"` (regex met `(?m)`: `(?m)^\s*BLOCKING_RULES="([^"]*)"`; `NON_LINTER_RULES` op dezelfde manier), splitst op `,` en filtert blokkeerredenen die geen linterregel zijn volgens het formaat van #381 (Taak 0). Hook ontbreekt of heeft geen `BLOCKING_RULES`: fout van het type `Unchecked`.

- [ ] **Stap 1: Schrijf de falende tests**

`TestReadFloorPython` (nep-hook met `BLOCKING_RULES="E722,B006"` → `["E722","B006"]`), `TestReadFloorMissingHookIsUnchecked`, `TestReadFloorNoBlockingRulesIsUnchecked`, `TestReadFloorNodeIsEmpty`, `TestReadFloorSkipsReasonIDs` (nep-Go-hook met `typecheck` naast `errcheck`, in het formaat van #381 → alleen `errcheck`), `TestReadFloorIgnoresOtherHooks` (een `ty-gate.sh` met `BLOCKING_RULES` verandert niets).

- [ ] **Stap 2: Run, verwacht FAIL**

Run: `go test ./internal/lintcheck -run ReadFloor -count=1` — Verwacht: FAIL.

- [ ] **Stap 3: Implementeer `ReadFloor`** met één regex op `^\s*BLOCKING_RULES="([^"]*)"`. Wacht niet op een shell: lees alleen tekst.

- [ ] **Stap 4: Run, verwacht PASS.**

- [ ] **Stap 5: Commit** — `feat(lintcheck): lees de vloer uit de geïnstalleerde hook (#382)`

---

### Taak 3: Het CI-model en de exitstatus-allowlist

**Files:**
- Create: `internal/lintcheck/ci.go`, `internal/lintcheck/ci_github.go`, `internal/lintcheck/ci_gitlab.go`, `internal/lintcheck/ci_test.go`

**Interfaces:**
- Consumes: `env` uit Taak 1.
- Produces:
  ```go
  type Step struct {
      File string; Line int
      Run  string            // the run script, or "" for a uses: step
      Uses string; With map[string]string
      WorkDir string         // effective working-directory ("" = repo root)
      Env map[string]string  // workflow+job+step env, step wins
      Active bool            // runs on change: trigger, no literal `if: false`, GitLab rules
      Valid  bool            // exit status provably the step's status (allowlist)
      Why   string           // "step not recognised" reason when !Valid
  }
  // loadSteps reads every CI file of the forge. Invalid YAML → Unchecked.
  func loadSteps(root, forgeKind string) ([]Step, error)
  // Call is one recognised linter invocation inside a step.
  type Call struct{ Step Step; Linter string; Args []string }
  func findCalls(steps []Step, linter string, patterns ...*regexp.Regexp) []Call
  ```
- Regels (spec §3 Rood in CI): GitHub-workflow telt met trigger `push`/`pull_request`, of `workflow_call` zodra een lokale workflow die zelf `push`/`pull_request` als trigger heeft hem aanroept via `uses: ./.github/workflows/<bestand>` (één niveau; een aanroeper met alleen `schedule` of `workflow_dispatch` telt niet); `if:` toegestaan behalve letterlijk `false`; `working-directory` van stap, anders `defaults.run` job, anders workflow; GitLab-job niet bij `allow_failure` waar (ook `exit_codes`-vorm), `when: manual|never`, ook via `rules:`. Exitstatus-allowlist: effectieve shell (`shell:` stap > `defaults.run.shell` job > workflow > runner-standaard) is `bash`, `sh` of leeg op een Linux-/macOS-runner; elke andere waarde, een Windows-runner zonder shell, of `runs-on` als expressie is `Valid=false`, `Why="step not recognised"`; aanroep op eigen regel of met `&&`, zonder `|`, `||`, `;`-keten erna (pipe alleen met `set -o pipefail` of `shell: bash`) en zonder `set +e` ervoor; `continue-on-error` op stap **en** job afwezig of letterlijk `false`. Een `uses:`-actie is geldig zonder `continue-on-error` en zonder exit-zero-input. Regelvoortzetting (`\`) en commentaarregels worden vóór het zoeken samengevoegd/gestript.

- [ ] **Stap 1: Schrijf de falende tests** (tabeltest per regel, met ruff als voorbeeldlinter; assert op `Active`/`Valid`/`Why` van de gevonden `Call`)

`TestStepActive` (push-trigger ja; alleen `schedule` nee; `if: false` nee; padfilter-`if:` ja; reusable workflow zonder aanroeper nee, met push-aanroeper ja, met alleen een `schedule`-aanroeper nee), `TestStepValidAllowlist` (ja: eigen regel, `&&`, `shell: bash` + `| tee`; nee, met `Why`: `continue-on-error: true` op stap, idem op job, `continue-on-error: ${{ x }}`, `|| true`, `| tee` zonder pipefail op `shell: sh`, `set +e` ervoor, `shell: bash {0}` ook via `defaults.run.shell`, `runs-on: windows-latest` zonder shell, `runs-on: ${{ matrix.os }}`, `--exit-zero`), `TestWorkingDirInheritance`, `TestCommentAndEchoAreNoCall` (Review Focus 2), `TestMultilineContinuation`, `TestYamlAnchorsAndMatrixNoPanic` (Review Focus 3), `TestInvalidYAMLIsUnchecked`, `TestGitLabJobs` (lintjob geldig; `allow_failure: true`, `allow_failure: {exit_codes: [1]}`, `when: manual` en `rules: [{when: manual}]` ongeldig), `TestForgeKindDefaultsToGitHub`.

- [ ] **Stap 2: Run, verwacht FAIL**

Run: `go test ./internal/lintcheck -run 'Step|WorkingDir|Comment|Multiline|Yaml|Invalid|GitLab|ForgeKind' -count=1` — Verwacht: FAIL.

- [ ] **Stap 3: Implementeer** `loadSteps` en `findCalls` met yaml.v3 (`yaml.Node` waar anchors/expressies telt; geen `interface{}`-assertions die kunnen paniekeren). Het ontleden van een `run:`-regel blijft bewust klein: splits op regeleinden na het voegen van `\`, strip `#`-commentaar, zoek de aanroep als woordgrens-token aan het begin van een commando (na `&&`, `;`, `|`, begin van regel, of `$(`-loos). De precieze patronen per linter staan bij de taak van die taal.

- [ ] **Stap 4: Run, verwacht PASS.**

- [ ] **Stap 5: Commit** — `feat(lintcheck): CI-model met exitstatus-allowlist voor GitHub en GitLab (#382)`

---

### Taak 4: Python

**Files:**
- Create: `internal/lintcheck/python.go`, `internal/lintcheck/python_test.go`
- Modify: registratie in `Run` (Taak 1)

**Interfaces:**
- Consumes: `ReadFloor(root,"python")`, `loadSteps`, `findCalls`.
- Produces: `func checkPython(e *env) ([]Finding, error)`; linternaam `ruff`.

Controles (spec §3):
- **Pin:** `.ruff-version` in de root; ontbreekt → `.ruff-version: ruff — pin missing`. CI-aanroep volgt de pin: `uvx ruff@<pin>`, `ruff@"$(cat .ruff-version)"`, `pip install ruff==<pin>`, of `astral-sh/ruff-action` met `version: <pin>`; zonder versie → `CI version not pinned`; andere versie → `CI version <x>, pin <y>` met regel.
- **Selectie:** `ruff.toml`, `.ruff.toml` of `[tool.ruff.lint]` in `pyproject.toml` (BurntSushi/toml); in `ruff.toml` staan `select`/`ignore` onder `[lint]` (modern) of op het topniveau (legacy): lees beide, met een test per vorm (`TestPythonRuffTomlLintTableAndLegacy`); elke vloercode effectief aan: `select`/`extend-select` exact, via prefix of `ALL`; niet uit via `ignore`/`extend-ignore`, met voorrang van de meest specifieke code (`select=["PT012"]`, `ignore=["PT"]` laat PT012 aan); `extend = "<pad>"` gevolgd met cyclusbeveiliging. Geen config → `config missing`; vloercode uit → `floor rule <code> disabled`.
- **CI-flags die de vloer versmallen:** `--ignore`, `--extend-ignore`, `--select` zonder vloercode, `--config` naar een bestand dat de vloer mist; `--exclude`, `--extend-exclude` en paden tellen niet.
- Geen herkende stap → `.github/workflows: ruff — no CI step runs ruff check`; herkende maar ongeldige stap → `step has continue-on-error` of `step not recognised`.

- [ ] **Stap 1: Schrijf de falende tests** (nep-repo in `t.TempDir()` met hook, `.ruff-version`, config, workflow)

`TestPythonWired` (pin, config met vloer, `uvx ruff@<pin> check`), `TestPythonNoCIStep`, `TestPythonFloorCodeInIgnore`, `TestPythonFloorCodeInExtendIgnore`, `TestPythonPrefixSelect` (`B` dekt `B006`), `TestPythonSpecificBeatsPrefix`, `TestPythonExtendCycleNoLoop` en `TestPythonExtendTargetMissing` (Review Focus 4), `TestPythonPyprojectWithoutRuffTable` → `config missing`, `TestPythonCLIFlagNarrows` (`ruff check --select E9`, `--ignore E722`), `TestPythonExcludeIsFine`, `TestPythonVersionMismatch`, `TestPythonUnpinnedInstall`, `TestPythonFloorFromHook` (nep-hook met extra code in `BLOCKING_RULES`, config zonder die code → faalt), `TestPythonMakeLintOnlyIsNoStep`.

- [ ] **Stap 2: Run, verwacht FAIL**

Run: `go test ./internal/lintcheck -run Python -count=1` — Verwacht: FAIL.

- [ ] **Stap 3: Implementeer `checkPython`** met een kleine `ruffSelected(code string, cfg ruffCfg) bool` die de voorrangsregel (langste prefix wint; bij gelijk `ignore` boven `select`) toepast.

- [ ] **Stap 4: Run, verwacht PASS.**

- [ ] **Stap 5: Commit** — `feat(lintcheck): Python-check voor ruff (#382)`

---

### Taak 5: Go

**Files:**
- Create: `internal/lintcheck/golang.go`, `internal/lintcheck/golang_test.go`

**Interfaces:**
- Produces: `func checkGo(e *env) ([]Finding, error)`; linternaam `golangci-lint`.

Controles:
- **Pin:** `.golangci-version`; CI: `golangci/golangci-lint-action` met `with.version: <pin>` of een installatie van die versie; andere versie → `CI version <x>, pin <y>` met regel.
- **Selectie:** `.golangci.yml|.yaml|.toml|.json` met `version: "2"` (anders `config not version 2`); elke vloerlinter in `linters.default` (`standard` of `all`) of `linters.enable`, niet in `linters.disable`; zonder `linters.default` geen vloer (`config missing` als er helemaal geen bestand is, anders `floor not explicit`). Geen mapping `standard` → linters in Go-code: `default: standard` of `all` telt als aan voor elke vloerlinter, anders moet de vloerlinter in `enable` staan.
- **CI-flags die versmallen:** `--disable`, `--enable-only`, `--default none`, `--config` naar een bestand dat de vloer mist; `--issues-exit-code=0` maakt de stap ongeldig.

- [ ] **Stap 1: Schrijf de falende tests**

`TestGoWired` (pin, `default: standard`, actie met de pin), `TestGoNoCIStep`, `TestGoCIVersionMismatch`, `TestGoNoLintersDefault` → faalt, `TestGoNoConfigFile` → `config missing`, `TestGoV1Config`, `TestGoFloorLinterDisabled`, `TestGoEnableOnly`, `TestGoIssuesExitCodeZero`, `TestGoActionWithContinueOnError`.

- [ ] **Stap 2: Run, verwacht FAIL** — `go test ./internal/lintcheck -run 'Go' -count=1`

- [ ] **Stap 3: Implementeer `checkGo`** (yaml.v3 voor yml/yaml, toml en encoding/json voor de rest, naar één interne `map[string]any`-boom).

- [ ] **Stap 4: Run, verwacht PASS.**

- [ ] **Stap 5: Commit** — `feat(lintcheck): Go-check voor golangci-lint (#382)`

---

### Taak 6: Shell

**Files:**
- Create: `internal/lintcheck/shell.go`, `internal/lintcheck/shell_test.go`

**Interfaces:**
- Produces: `func checkShell(e *env) ([]Finding, error)`; linternaam `shellcheck`.

Controles:
- **Pin:** `SHELLCHECK_PIN` uit `.lighthouse/scripts/lib/shellcheck-bin.sh` (geïnstalleerde kopie; ontbreekt → bevinding `<pad>: shellcheck — pin missing`, exit 1, zoals bij Python). CI: `.lighthouse/scripts/shellcheck-repo.sh` (gebruikt de pin zelf) of een installatie van die versie; installatie zonder versie → `CI version not pinned`.
- **Selectie:** geen `.shellcheckrc` is goed. Een `.shellcheckrc` mag geen `disable=` van een vloercode bevatten, ook niet via `disable=all` of een bereik (`SC2000-SC2999`). `severity=` en `--severity`: volgens #381 (Taak 0) zijn alle shell-vloercodes `warning`; `error` versmalt de vloer en is een bevinding, `warning`, `info` en `style` niet. Dat is een vergelijking op de waarde, geen lijst per code.
- **CI-flags en env:** `-e`/`--exclude` of `-i`/`--include` zonder vloercode, `--severity` boven een vloercode, `SHELLCHECK_OPTS` in `env:` van workflow, job of stap met een van die flags.

- [ ] **Stap 1: Schrijf de falende tests**

`TestShellWiredWithoutRC`, `TestShellWiredWithHarmlessRC`, `TestShellNoCIStep`, `TestShellRCDisablesFloorCode`, `TestShellRCDisableAll`, `TestShellRCDisableRange`, `TestShellOptsExcludesFloorCode` (`SHELLCHECK_OPTS: -e SC2164` op workflow, job en stap), `TestShellMissingPinFileIsFinding`, `TestShellUnpinnedAptInstall`.

- [ ] **Stap 2: Run, verwacht FAIL** — `go test ./internal/lintcheck -run Shell -count=1`

- [ ] **Stap 3: Implementeer `checkShell`.**

- [ ] **Stap 4: Run, verwacht PASS.**

- [ ] **Stap 5: Commit** — `feat(lintcheck): shell-check voor shellcheck (#382)`

---

### Taak 7: Node en Expo (workspace-roots, fixtures)

**Files:**
- Create: `internal/lintcheck/node.go`, `internal/lintcheck/node_test.go`

**Interfaces:**
- Produces: `func checkNode(e *env) ([]Finding, error)`; linternaam `eslint`; `func nodeRoots(root string, dirs []string) ([]string, error)`. Bestaat na #383 al een gedeelde taalset-/workspace-root-functie in `internal/importcheck`, gebruik die; anders deze, en laat #383 haar overnemen.

Controles:
- **Roots:** zonder `--dir` elke gevolgde `package.json` die geen workspace is van een `package.json` erboven (`workspaces` in `package.json`, of `pnpm-workspace.yaml`); een pad met segment `testdata` wordt overgeslagen. Gevolgde bestanden uit `git ls-files`, met terugval op een directory-walk als geen git-repo.
- **Pin:** `eslint` in `devDependencies` met exacte versie (geen `^`, `~`, bereik of tag). CI: installatie uit de lockfile vóór de lintstap (`npm ci`, `pnpm install --frozen-lockfile`, `yarn install --immutable|--frozen-lockfile`, `bun install --frozen-lockfile`) in dezelfde `working-directory`.
- **Selectie:** `eslint.config.{js,mjs,cjs,ts}` of `.eslintrc.*` bevat een `rules`-sleutel, `extends`, of preset-verwijzing (`configs.`, `eslint-config-…`, `eslint-config-expo`); alleen `ignores` telt niet; geen JavaScript-parser (tekstmatch).
- **CI:** `eslint` of `expo lint`, ook via `npm run <script>` (script in `package.json`, één niveau diep; `eslint . || true` is geen geldige aanroep), in de map van de `package.json`. Vloer is leeg; versmallende flags: `--no-eslintrc`/`--no-config-lookup` zonder eigen `--config`, `--config` naar een bestand zonder keuze.

- [ ] **Stap 1: Schrijf de falende tests**

`TestNodeWired` (exacte eslint, `eslint.config.js` met `extends`, `npm ci` + `npm run lint` met `defaults.run.working-directory`), `TestNodeNoCIStep`, `TestNodeCaretVersion`, `TestNodeConfigOnlyIgnores`, `TestNodeNoInstallBeforeLint`, `TestNodeWorkspaceRootCoversChildren` (child krijgt geen melding), `TestNodeTestdataFixtureSkipped`, `TestExpoWired` (`"lint": "expo lint"`), `TestExpoNoCIStep`, `TestNodeNoScriptsNoPanic` en `TestNodeLintScriptWithOrTrue` en `TestNodeInvalidPackageJSON` (Review Focus 5; ongeldige JSON → bevinding `package.json: eslint — invalid JSON`, exit 1: de exit-3-lijst is gesloten), `TestNodeDirFlagLimitsToOneRoot`.

- [ ] **Stap 2: Run, verwacht FAIL** — `go test ./internal/lintcheck -run 'Node|Expo' -count=1`

- [ ] **Stap 3: Implementeer `checkNode` en `nodeRoots`.**

- [ ] **Stap 4: Run, verwacht PASS.**

- [ ] **Stap 5: Commit** — `feat(lintcheck): Node- en Expo-check voor eslint (#382)`

---

### Taak 8: Aansluiting op `init`

**Files:**
- Modify: `internal/initcmd/initcmd.go` (naast `reportPins(o, root)`, regel ~243; plus een `reportLint(o Options, root string)`)
- Modify/Test: `internal/initcmd/initcmd_test.go`

**Interfaces:**
- Consumes: `lintcheck.Run(Options{Root: root}, out, errOut) int`.
- Produces: `func reportLint(o Options, root string)`: draait `lintcheck.Run` met een buffer, en schrijft bij exit ≠ 0 onder de kop `lint check: some linters are not wired:` de gebufferde regels naar `o.Err` (stderr, zoals `reportPins`). Bij exit 0 niets op stdout of stderr. Exit 1 en 3 laten `init` nooit falen.

- [ ] **Stap 1: Schrijf de falende tests**

`TestInitReportsUnwiredLinter` (nep-repo na init zonder CI-stap → kop en regel op `o.Err`, return 0), `TestInitSilentWhenWired` (geen extra bytes op `o.Out` en `o.Err`), `TestInitUpgradeStillNoChanges` (upgrade op een aangesloten repo print exact `No changes.`).

- [ ] **Stap 2: Run, verwacht FAIL** — `go test ./internal/initcmd -run 'Lint|Unwired|Silent|Upgrade' -count=1`

- [ ] **Stap 3: Implementeer `reportLint`** en roep hem na `reportPins` aan, ook bij `--upgrade`.

- [ ] **Stap 4: Run, verwacht PASS**, daarna `go test ./internal/initcmd ./internal/cli -count=1`.

- [ ] **Stap 5: Commit** — `feat(init): toon wat de linter-check mist zonder te stoppen (#382)`

---

### Taak 9: CI, documentatie, fragment en dogfood

**Files:**
- Modify: `.github/workflows/ci.yml` (nieuwe job `lint-check`, zonder padfilter)
- Create: `docs/lint-check.md`, `changelog.d/issue-382-linter-aangesloten.md`

**Interfaces:**
- Consumes: alles hierboven.

- [ ] **Stap 1: CI-job.** Voeg `lint-check` toe: `actions/checkout` met hetzelfde blok als de bestaande jobs (`ci.yml:27-30`: gepinde SHA plus `with: persist-credentials: false`, anders weigert zizmor `artipacked` de push) en `timeout-minutes` als de andere jobs, `actions/setup-go` met `go-version-file: go.mod`, `run: go run ./cmd/lighthouse lint check`. Geen `needs: changes`, geen `if:`. De push naar `.github/workflows/` doet de workflow-pusher (`workflow-push-gate`); commit de wijziging, push hem niet zelf.

- [ ] **Stap 2: Doc** `docs/lint-check.md` (Nederlands, relatieve links): wat de check eist per pack, de herkende aanroepen, exitcodes, en dat een niet herkende stap exit 1 geeft. Link vanaf `docs/pins.md` en `docs/test-gate.md` niet nodig; voeg één regel toe aan de docs-index als die er is.

- [ ] **Stap 3: Fragment** `changelog.d/issue-382-linter-aangesloten.md` in het Engels (`docs/releasing.md`), met `---`/`type: added`/`---` en een tot drie zinnen voor een gebruiker; er staat nog geen ander fragment in die map.

- [ ] **Stap 4: Bewijs lighthouse zelf** — Run: `go run ./cmd/lighthouse lint check; echo rc=$?`
Verwacht: `golangci-lint: wired`, `eslint: wired`, rc=0 (AC 5). Faalt het, dan los je het op door aan te sluiten, nooit door een config te versoepelen.

- [ ] **Stap 5: Dogfood en volledige suite** — Run: `go run ./cmd/lighthouse init --upgrade </dev/null` (verwacht `No changes.`), `go test ./... -count=1`, `golangci-lint run` (of de gate).

- [ ] **Stap 6: Commit** (atomair: eerst `ci:`/`docs:`, dan fragment)

```bash
git add .github/workflows/ci.yml && git commit -m "ci(lint): draai lighthouse lint check in een eigen job (#382)"
git add docs/lint-check.md changelog.d/issue-382-linter-aangesloten.md && git commit -m "docs(lint-check): beschrijf de check en het fragment (#382)"
```

---

## Zelfreview

- **Dekking:** subcommand en flags (T1), vloer uit hook (T2), CI-model en allowlist incl. GitLab (T3), vier packs en Expo (T4-T7), `init` (T8), CI-job, doc, fragment, AC 5 (T9). `Wat aan de bouwer is` ligt bij de taken: shell-severity (T6), patronen en `run:`-diepte (T3), GitLab-"draait bij wijziging" (T3 `rules`/`only`), pakketnaam `internal/lintcheck`, gedeelde taalset (T1, T7).
- **Open na review r3-f1:** de shell-resolutie in T3 volgt de spec-tekst (stap > job > workflow > runner-OS), wat dat punt al dekt.
- **Typeconsistentie:** `Finding`, `Unchecked`, `env`, `Step`, `Call`, `ReadFloor`, `loadSteps`, `findCalls`, `checkPython|Go|Shell|Node`, `nodeRoots` komen overal met dezelfde signatuur terug.
- **Afhankelijkheid:** T0 blokkeert alles tot #381 op `main` staat.
