# Instapniveaus Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Drie vaste niveaus (`light`, `standard`, `strict`) als data, gekozen met `level` in `lighthouse.toml`, geïnstalleerd door `lighthouse init`, met een stand `log` voor checks.

**Architecture:** Elk pakket levert `levels.toml` (pakketmetadata, nooit geïnstalleerd). `init` lost niveau plus `[components]`-overrides op tot `.lighthouse/components.toml`, laat units van onderdelen op `off` weg en filtert `blocking` en het managed block. Hooks lezen alleen `components.toml`; één wrapper in `hook.Run` regelt de stand `log` voor alle Go-hooks.

**Tech Stack:** Go (cobra, BurntSushi/toml), shell (`run.sh`), `pack/` als embedded data.

**Spec:** [2026-11-22-issue-19-instapniveaus-design.md](../specs/2026-11-22-issue-19-instapniveaus-design.md)

## Global Constraints

- Namen: niveaus `light`, `standard`, `strict`; standen `off`, `log`, `on`; een skill kent alleen `off` en `on`; hooks van taalpakketten (shellscripts) kennen alleen `off` en `on`.
- Een onderdeel dat het niveau niet noemt staat `on`. Ontbreekt `components.toml`, dan staat alles `on`. Een bestand dat er is maar niet te lezen is, geeft `COULD NOT CHECK`.
- Kerngates: `main-commit-gate`, `worktree-check`, `pr-create-gate`. Een vast niveau zet ze nooit uit; alleen een override mag, met waarschuwing (geen fout).
- Basis (altijd geïnstalleerd, geen override): managed block, `headings.toml`, PR-template, workflows, permissions, `run.sh`, `blocking`, `usage-gate`, `permission-check`, `workflow-push-gate`, `level-check`, `no-verify-check`, scripts die meer dan één onderdeel gebruikt.
- `--no-verify` blijft op elk niveau geblokkeerd. Permissions zijn op elk niveau gelijk; een niveau voegt nooit `permissions.allow` toe.
- Een bestaande repo zonder `level` gedraagt zich als `strict` en `init --upgrade` schrijft de sleutel niet bij. Een nieuwe repo: `light`, `level = "light"` in dezelfde diff.
- `config.Load` controleert alleen de vorm en de standwaarden (`off`, `log`, `on`); alleen `init` en `lighthouse level` weigeren een onbekende niveau- of onderdeelnaam. Hooks lezen nooit `lighthouse.toml` voor de stand.
- Alle meldingen van `init` over dit ontwerp gaan naar stderr; stdout is bij geen wijziging precies `No changes.` (dogfood-CI).
- `.lighthouse/components.toml` is een `file`-unit met `Package = "core"`, zonder marker; de SHA in het manifest volstaat.
- Stand `log`: geen `systemMessage`, geen stderr, exit 0, één regel in een logboek per `git rev-parse --git-common-dir`, buiten de getrackte bestanden, werkend op native Windows. Een onderdeel op `log` dat niet kan draaien gaat zonder blokkade door met exit 0.
- Code, commentaar, bestandsnamen en hookmeldingen in het Engels; artefacten (dit plan, changelog-fragmenten, commits) in het Nederlands. Atomaire Conventional Commits, nooit `--no-verify`.
- Elke wijziging onder `pack/` of die verandert wat `init` schrijft: `go run ./cmd/lighthouse init --upgrade` draaien en het resultaat in dezelfde PR committen. Elke PR krijgt een fragment in `changelog.d/`.
- Go-code volgt `/lighthouse-go-standard`, tests `/lighthouse-test-standard` (elke test noemt de productiewijziging die hem rood maakt).

## Review Focus

- `level = "bogus"` (of een onbekende naam in `[components]`) in `lighthouse.toml`: `config.Load` blijft slagen zodat elke gate blijft draaien; alleen `init` en `lighthouse level` falen, met de lijst geldige namen. (Task 3, 6)
- Een corrupt `components.toml`: een hook meldt `COULD NOT CHECK` met exit 3 (zodat `run.sh` de blokkadelijst toepast) en blokkeert niet stilzwijgend door; een ontbrekend bestand betekent alles `on`; een basishook (ook `level-check`) kijkt niet naar `LevelErr`. (Task 12, 17)
- Wisselen heen en terug (`strict` → `light` → `strict`) geeft byte-identieke bestanden en manifest; een lokaal gewijzigde unit die een wissel zou weghalen geeft de bestaande conflictweigering zonder `--force`. De `since`-datum verschilt na een heen-en-weerwissel bewust; de unitset en de bestanden niet. (Task 7)
- Override op een basisonderdeel (`usage-gate = "off"`), `log` op een skill of shell-hook, of `off` op een skill waar een ingeschakelde skill hard naar verwijst: `init` weigert met een duidelijke fout. (Task 6, 16)
- Het logboek kan niet geschreven worden (map niet schrijfbaar, geen `HOME`): de hook op `log` geeft toch exit 0 zonder uitvoer. (Task 12)

---

# Deel 1: niveaus en selectie

Eindigt met een groene PR-grens: dogfood op `strict`, `No changes.` in CI, changelog-fragment.

### Task 1: `internal/levels`: model, parser en oplosser

**Files:**
- Create: `internal/levels/levels.go`, `internal/levels/installed.go`
- Test: `internal/levels/levels_test.go`, `internal/levels/installed_test.go`

**Interfaces:**
- Produces:
  ```go
  type Stand string // StandOff = "off", StandLog = "log", StandOn = "on"
  type Table struct{ Components map[string]Stand; Flags map[string]bool }
  type File struct {
      Order  []string            // top-level `order`, core only: ["light","standard","strict"]
      Levels map[string]Table    // [<level>.components], [<level>.flags]
      Base   []string            // top-level `base`: component names that are always installed
      On     []string            // top-level `on`: components that are `on` at every level (named so they are known and never reported as unlisted)
      Owners map[string]string   // [owners]: package-relative path -> component, or "base"
      Calls  map[string][]string // [calls]: "skill:x" -> skills it hands over to
  }
  func Parse(data []byte) (File, error)  // rejects a stand other than off|log|on
  type Resolved struct { Level string; Components map[string]Stand; Flags map[string]bool }
  func Resolve(files []File, level string, overrides map[string]Stand) (Resolved, error) // one function: name + overrides -> stand per component
  func (r Resolved) Stand(component string) Stand // unlisted -> StandOn
  func (r Resolved) Names() []string              // every component any pack names in levels.toml: level tables, `base` and the new `on` list, sorted
  var CoreGates = []string{"main-commit-gate", "worktree-check", "pr-create-gate"}
  type Installed struct { Level string; Since string /* RFC3339 */; Components map[string]Stand }
  func (i Installed) Stand(component string) Stand
  func ReadInstalled(root string) (Installed, bool, error) // bool=false: file absent -> all on; error: present but unreadable
  func (i Installed) Marshal() []byte                     // deterministic; lists only non-on components, sorted
  const InstalledPath = ".lighthouse/components.toml"
  ```
- Consumes: niets.

- [ ] **Step 1: Write the failing tests** in `levels_test.go`: `TestResolveOverrideWins` (`light` + `{"md-link-check": On}` → On), `TestResolveUnlistedIsOn`, `TestResolveUnknownLevel` (error names the known levels), `TestResolveMergesPacks` (two `File`s, each its own components), `TestParseRejectsBadStand` (`"maybe"`), and in `installed_test.go`: `TestReadInstalledAbsent` (false, nil), `TestReadInstalledCorrupt` (error), `TestMarshalRoundTrip` (Marshal→parse equal; output has no `on` entries; same input gives identical bytes).
- [ ] **Step 2: Run** `go test ./internal/levels/...` — Expected: FAIL (package does not exist).
- [ ] **Step 3: Implement** both files. `Installed.Marshal` starts with the header comment `# Managed by lighthouse init. Do not edit by hand.`; `since` is a quoted RFC3339 string, not a TOML datetime.
- [ ] **Step 4: Run** the same command — Expected: PASS. Run `golangci-lint run ./internal/levels/...` — Expected: no findings.
- [ ] **Step 5: Commit** `feat(levels): niveaumodel, oplosser en components.toml (#19)`.

### Task 2: `levels.toml` in de pakketlader en de unit→onderdeel-koppeling

**Files:**
- Modify: `internal/packs/packs.go` (`Load`: `levels.toml` as known top-level file, never a unit; `Pack.Levels levels.File`)
- Create: `internal/packs/component.go`, `pack/core/levels.toml`, `pack/go/levels.toml`, `pack/python/levels.toml`, `pack/shell/levels.toml`, `pack/expo/levels.toml`
- Test: `internal/packs/component_test.go`, `internal/packs/packs_test.go`

**Interfaces:**
- Consumes: `levels.File`, `levels.Parse` (Task 1).
- Produces: `func Component(u Unit, f levels.File) (name string, base bool, err error)` — the one place a unit belongs to. Derivation: hook registration → the command `run.sh <name> …` or `<name>.sh` gives `<name>`; hook file unit `hooks/<name>.sh` → `<name>`; skill files `.claude/skills/lighthouse-<n>/…` and `.agents/skills/lighthouse-<n>/…` → `skill:<n>`; kinds `block`, `permission`, workflows, PR template, `headings.toml` → base; `run.sh`, `blocking` → base; agents and `scripts/` via `f.Owners` (key = path inside the package, e.g. `agents/issue-impact-scout.md`); names in `f.Base` → base. A unit with no place, or two, is an error, except in a package without `levels.toml` (test fixtures): there every unit counts as `base`.

- [ ] **Step 1: Write the failing tests**: `TestEveryUnitHasExactlyOnePlace` loads every package of `pack.Files` through `loadAll`-style code (`fs.Sub` per directory), runs `Component` on each unit, fails with the unit path when no place; `TestComponentDerivation` (table: `commit-test-gate` registration under PreToolUse and PostToolUseFailure → the same name; `skills/go-standard` file → `skill:go-standard`; agent via owners; permission → base); `TestLoadIgnoresLevelsToml` (a fixture package with `levels.toml` yields no unit for it).
- [ ] **Step 2: Run** `go test ./internal/packs/...` — Expected: FAIL.
- [ ] **Step 3: Implement** `Component` and the `Load` change. Author the `levels.toml` files from the ladder in the spec (`## De ladder`) exactly: `pack/core/levels.toml` has `order`, `base = ["run.sh", "blocking", "usage-gate", "permission-check", "workflow-push-gate", "level-check", "no-verify-check"]`, `[owners]` for `agents/issue-impact-scout.md = "skill:start-issue"`, and one `[light.*]`, `[standard.*]`, `[strict.*]` table each (`strict.components` empty but present), and `on = ["main-commit-gate", "worktree-check", "pr-create-gate", "bash-write-check", …]` naming every other component of the package that no level table lists (every `levels.toml` likewise); `TestEveryDerivedComponentIsNamed` fails with the name of a derived component that is in no table, `base` or `on`. `light`: `commit-test-gate = "off"`, `md-link-check = "log"`, `code-language-check = "log"`, skills `write-issue`, `fix-pr`, `audit-worktrees`, `audit-agent-config`, `test-standard`, `review-spec`, `review-plan` off, `[light.flags] labels = false`. `standard`: `review-spec`, `review-plan` off, `[standard.flags] labels = true`. `strict`: `labels = true`. Language packs: `gofmt-check`, `go-lint-gate`, `ruff-format-check`, `ty-gate`, `shell-lint-check` and their `skill:*-standard` off on `light`. Scripts: list **every** file under `pack/*/scripts/` (helpers in `lib/` and `quality/` included) in `[owners]` or `base`: a script one hook calls (directly or through another script, e.g. `lib/shellcheck-bin.sh` via `shellcheck-repo.sh`) gets that hook; a script no hook calls (`gate_python.py`, `quality/*.py`, used by skills, `test_gate` and instructions) or used by more than one → `"base"`. `TestScriptOwnersTable` pins the concrete table. `[calls]` stays empty here (Task 14). `level-check` is in `base` before its hook exists; that is fine.
- [ ] **Step 4: Run** `go test ./internal/packs/... ./pack/...` — Expected: PASS (the `strict` row of the ladder is all `on`).
- [ ] **Step 5: Commit** `feat(packs): levels.toml per pakket en koppeling unit naar onderdeel (#19)`.

### Task 3: `level` en `[components]` in `internal/config`

**Files:**
- Modify: `internal/config/config.go` (schema, `Config`, `build`), and the parity test fixtures if `parity_test.go` demands it
- Test: `internal/config/config_test.go`

**Interfaces:**
- Produces: `Config.Level string` ("" when unset) and `Config.Components map[string]string` (raw values, each checked to be `off|log|on`); keys `level` (str) and `components.<name>` known to `IsKnownKey`.

- [ ] **Step 1: Write the failing tests**: `TestLevelAndComponentsParse`, `TestComponentsBadStandIsError` (`md-link-check = "maybe"` → `*Error`), `TestUnknownLevelNameIsNotAnError` (`level = "bogus"` loads), `TestUnknownComponentNameIsNotAnError`.
- [ ] **Step 2: Run** `go test ./internal/config/...` — Expected: FAIL.
- [ ] **Step 3: Implement** following how `optionPrefixes` handles free-form key tables.
- [ ] **Step 4: Run** the same — Expected: PASS, including `parity_test.go`.
- [ ] **Step 5: Commit** `feat(config): sleutels level en [components] (#19)`.

### Task 4: `projectconfig.SetTopLevel`

**Files:**
- Modify: `internal/projectconfig/projectconfig.go`
- Test: `internal/projectconfig/projectconfig_test.go`

**Interfaces:**
- Produces: `func SetTopLevel(existing []byte, key, value string) ([]byte, error)` — `value` is a TOML literal such as `"light"`. Inserts the key before the first table header, or replaces the existing top-level value in place; comments stay; invalid TOML is an error.

- [ ] **Step 1: Write the failing tests**: `TestSetTopLevelInsertsBeforeFirstTable`, `TestSetTopLevelReplacesAndKeepsComments` (`level = "light" # why` → value changes, comment stays), `TestSetTopLevelFileWithoutTables`, `TestSetTopLevelIgnoresSameKeyInsideATable` (`[forge] level = "x"` is not the top-level key).
- [ ] **Step 2: Run** `go test ./internal/projectconfig/...` — Expected: FAIL.
- [ ] **Step 3: Implement** with the existing `header` regexp; no change to `AppendMissing`.
- [ ] **Step 4: Run** — Expected: PASS.
- [ ] **Step 5: Commit** `feat(projectconfig): schrijver voor een sleutel op het hoogste niveau (#19)`.

### Task 5: Selectie in `init`: units op `off` vallen weg, `components.toml` erbij

**Files:**
- Create: `internal/initcmd/levels.go`
- Modify: `internal/initcmd/initcmd.go` (`execute`, `Options.Level`), `internal/packs/packs.go` (`IsInstallPath`: allow `levels.InstalledPath`)
- Test: `internal/initcmd/levels_test.go`

**Interfaces:**
- Consumes: `levels.Resolve`, `levels.Installed`, `packs.Component` (Tasks 1–2), `config.Config.Level/Components` (Task 3).
- Produces: `func selectUnits(desired []packs.Unit, files []levels.File, r levels.Resolved) (kept []packs.Unit, err error)` — drops units whose component stand is `off`; base units stay. `func componentsUnit(r levels.Resolved, since string) packs.Unit` — `Kind: KindFile`, `Package: "core"`, `Path: levels.InstalledPath`, content from `Installed.Marshal`, mode 0o644. `Options.Level string` (`--level`; wired in Task 8).
- Effective level for this task: `lighthouse.toml` `level`, else `strict` (a repo without `level`). Overrides come from `[components]`.

- [ ] **Step 1: Write the failing tests** (use the `Options.Packs` fixture filesystem like `initcmd_test.go`): `TestInitLightSkipsOffUnits` (no `commit-test-gate` registration, no `skills/lighthouse-fix-pr/`), `TestInitStrictInstallsEverything` (unit set equals today's), `TestInitWritesComponentsToml` (content lists only non-`on`), `TestInitRepoWithoutLevelActsAsStrict`, `TestOverrideWinsOverLevel`, `TestAlwaysOnListedComponentIsNotReported` (`main-commit-gate` in the `on` list of `pack/core/levels.toml` → no stderr line), `TestUnlistedComponentIsReportedOnStderr` (a pack unit no `levels.toml` names → one stderr line, nothing on stdout, still installed).
- [ ] **Step 2: Run** `go test ./internal/initcmd/...` — Expected: FAIL.
- [ ] **Step 3: Implement** and call `selectUnits` after `dropWorkflows` in `execute`; add `componentsUnit` to `desired`. `since` is the previous `components.toml` `since` when the level is unchanged, else `time.Now().UTC()` (an injectable clock in `Options` for tests).
- [ ] **Step 4: Run** — Expected: PASS; `go test ./internal/packs/...` still PASS.
- [ ] **Step 5: Commit** `feat(init): units van onderdelen op off vallen weg, components.toml erbij (#19)`.

### Task 6: Validatie van namen en standen, en de kerngate-waarschuwing bij `init`

**Files:**
- Modify: `internal/initcmd/levels.go`, `internal/initcmd/initcmd.go`
- Test: `internal/initcmd/levels_test.go`

**Interfaces:**
- Produces: `func validate(files []levels.File, level string, over map[string]levels.Stand, loggable func(component string) bool) (warnings []string, err error)`. Errors: unknown level (lists the valid ones), unknown component name (lists them; valid = `Names()`), override on a base component, `off` on a skill that an `on` skill lists in `[calls]` (names both; empty until Task 19), `log` on a component that is not a Go hook of core (skill or shell hook; `loggable` is derived from the units: the component has a hook registration whose command contains `/run.sh `). Warnings (stderr, never an error): one line per core gate whose stand is not `on`: `lighthouse: <gate> is <stand> — this repo works outside the chain`.

- [ ] **Step 1: Write the failing tests**: `TestUnknownLevelNamesTheValidOnes`, `TestUnknownComponentIsAnError`, `TestOverrideOnBaseIsRefused` (`usage-gate = "off"`), `TestLogOnSkillIsRefused`, `TestLogOnShellHookIsRefused` (`gofmt-check = "log"`), `TestLogOnMdLinkCheckIsAccepted`, `TestCoreGateOffWarnsOnStderrAndInstallsWithoutIt` (`main-commit-gate = "off"` → exit 0, warning line, no registration), `TestInitWithBadNameWritesNothing`.
- [ ] **Step 2: Run** `go test ./internal/initcmd/...` — Expected: FAIL.
- [ ] **Step 3: Implement** `validate` and call it before anything is planned or written.
- [ ] **Step 4: Run** — Expected: PASS.
- [ ] **Step 5: Commit** `feat(init): validatie van niveaus en onderdelen, waarschuwing bij uitgezette kerngate (#19)`.

### Task 7: `level` schrijven, nieuwe repo kiest `light`, `--level` en wisselherkenning

**Files:**
- Modify: `internal/initcmd/initcmd.go` (`configTables`/`planConfig`: the `level` line in the `lighthouse.toml` diff), `internal/cli/init.go` (`--level <name>`), `internal/initcmd/levels.go`
- Test: `internal/initcmd/levels_test.go`, `internal/cli/init_test.go`

**Interfaces:**
- Produces: `func switched(installed levels.Installed, found bool, level string) bool` — true on first init or when the `components.toml` level differs from the effective one. `--level <name>` with `init` sets the level; with `--upgrade` it rewrites `lighthouse.toml` via `projectconfig.SetTopLevel` in the same diff and installs the difference. Hand-edited `level` + plain `--upgrade` gives the same result. `init` without `--level` on a new repo writes `level = "light"`.
- Rule: `--upgrade` without `level` in `lighthouse.toml` and without `--level` never writes the key.

- [ ] **Step 1: Write the failing tests**: `TestNewRepoChoosesLightAndWritesLevel` (diff contains `level = "light"`, one `y`), `TestUpgradeWithoutLevelDoesNotWriteIt`, `TestUpgradeLevelFlagSwitchesAndRemovesUnits`, `TestHandEditedLevelThenUpgradeEqualsLevelFlag` (byte-equal tree), `TestSwitchRoundTripIsIdentical` (`strict`→`light`→`strict` with a clock that advances: every installed file except `components.toml` is identical to the start, and `components.toml` differs only in `since`), `TestDowngradeConflictRefusesWithoutForce` (locally edited unit that the downgrade removes → `refused: 1 locally modified unit(s)`), `TestSummaryNamesLevelAndHowToScaleUp`.
- [ ] **Step 2: Run** `go test ./internal/initcmd/... ./internal/cli/...` — Expected: FAIL.
- [ ] **Step 3: Implement**. The summary line (stdout, only when something is written) names the level and `lighthouse init --upgrade --level <next>`.
- [ ] **Step 4: Run** — Expected: PASS.
- [ ] **Step 5: Commit** `feat(init): --level, level schrijven en wisselherkenning (#19)`.

### Task 8: De vlag `labels` van een niveau

**Files:**
- Modify: `internal/initcmd/levels.go`, `internal/initcmd/initcmd.go` (where `o.Labels` is read)
- Test: `internal/initcmd/labels_test.go`

**Interfaces:**
- Consumes: `levels.Resolved.Flags["labels"]`, `switched` (Task 7).
- Behaviour: when the level has `labels` true AND (first init OR `switched`), run as if `--labels` was given. No switch → no labels work, no network call. A repo without `level` → none. `--create-labels` is never implied.

- [ ] **Step 1: Write the failing tests**: `TestLevelLabelsOnFirstInitStandard`, `TestLevelLabelsOnSwitchToStandard`, `TestUpgradeWithoutSwitchTouchesNoLabels` (sync stub not called), `TestLightDoesNotSetLabels`, `TestLevelNeverImpliesCreateLabels`.
- [ ] **Step 2: Run** `go test ./internal/initcmd/...` — Expected: FAIL.
- [ ] **Step 3: Implement** at the top of `execute` after the level is resolved.
- [ ] **Step 4: Run** — Expected: PASS.
- [ ] **Step 5: Commit** `feat(init): een niveau zet --labels bij de eerste init of een wissel (#19)`.

### Task 9: Vereisten filteren

**Files:**
- Modify: `internal/initcmd/select.go` (next to `forgeRequires`), `internal/initcmd/initcmd.go`
- Test: `internal/initcmd/select_test.go`

**Interfaces:**
- Produces: `func levelRequires(reqs []packs.Requirement, r levels.Resolved) []packs.Requirement` — keeps a `NeededBy` entry when the component named by its **first word** (the decoration after it, e.g. `shell-lint-check (download of the pinned binary)`, is ignored) is not `off`: `r.Stand(word) != off` and `r.Stand("skill:"+word) != off`; a requirement without remaining `NeededBy` is dropped.

- [ ] **Step 1: Write the failing tests**: `TestLightDoesNotReportMissingCodex` (`codex` needed only by `review-spec`, `review-plan`, both off → no `missing dependency: codex` line), `TestStrictStillReportsCodex`, `TestRequirementKeepsOnlyOnNames`, `TestDecoratedNeededByUsesFirstWord` (`shell-lint-check (download of the pinned binary)` on `light` → `curl`, `tar`, `xz` not reported; the real `pack/shell/requires.toml` and `pack/python/requires.toml`).
- [ ] **Step 2: Run** `go test ./internal/initcmd/...` — Expected: FAIL.
- [ ] **Step 3: Implement** and apply it before `mergeRequires(forgeRequires(...))`.
- [ ] **Step 4: Run** — Expected: PASS.
- [ ] **Step 5: Commit** `feat(init): vereisten van uitgezette onderdelen tellen niet mee (#19)`.

### Task 10: Dogfood op `strict` en changelog deel 1

**Files:**
- Modify: `lighthouse.toml` (`level = "strict"` before the first table), `.lighthouse/` and `.claude/` installed copy
- Create: `changelog.d/issue-19-instapniveaus-selectie.md` (`type: added`, Dutch, in the style of the other fragments)

- [ ] **Step 1:** add `level = "strict"` with `projectconfig.SetTopLevel`-style placement by hand (top of the file).
- [ ] **Step 2: Run** `go run ./cmd/lighthouse init --upgrade --yes` — Expected: a diff with `.lighthouse/components.toml` and the manifest; then run it again — Expected: stdout exactly `No changes.`
- [ ] **Step 3: Run** the `[test_gate] command`, `golangci-lint run ./...` and `uv run pytest -q` — Expected: green. Update `docs/site/data/config.json` (`level`, `[components]`; `tests/test_site_config.py` demands every schema key) in this PR.
- [ ] **Step 4: Commit** `feat(dogfood): lighthouse draait op level strict (#19)`.
- [ ] **Step 5:** Push-grens deel 1: `/lighthouse-write-pr`-fase volgt het gedrag in de controller-run; stop hier met de handoff als het plan in delen wordt uitgevoerd.

---

# Deel 2: de stand `log`

### Task 11: `internal/levellog`: het logboek

**Files:**
- Create: `internal/levellog/levellog.go`
- Test: `internal/levellog/levellog_test.go`

**Interfaces:**
- Produces:
  ```go
  func Dir() (string, error) // XDG_STATE_HOME/lighthouse/level-log, else LOCALAPPDATA on windows, else $HOME/.local/state/lighthouse/level-log; error when none is absolute
  func Append(root, component, firstLine string, at time.Time) error // JSON line {"component","at","line"}; key = sha256(abs(git rev-parse --git-common-dir))[:16]
  func Count(root string, since time.Time) (map[string]int, error)
  ```
- A relative `--git-common-dir` (`.git`) is made absolute against `root` before hashing, so a worktree and the main checkout share one file.

- [ ] **Step 1: Write the failing tests** (a real `git init` + `git worktree add` in `t.TempDir()`): `TestWorktreesShareOneLog`, `TestCountIgnoresBeforeSince`, `TestAppendWithoutWritableDirIsAnError`, `TestDirNeedsAnAbsoluteBase`, `TestKeyIsStableAcrossCalls`.
- [ ] **Step 2: Run** `go test ./internal/levellog/...` — Expected: FAIL.
- [ ] **Step 3: Implement** with `O_APPEND` writes of one line each (atomic enough for short lines).
- [ ] **Step 4: Run** — Expected: PASS. If a Windows build matters, `GOOS=windows go vet ./internal/levellog/` — Expected: no output.
- [ ] **Step 5: Commit** `feat(levellog): logboek per gedeelde repository (#19)`.

### Task 12: De stand lezen en `log` toepassen in `hook.Run`

**Files:**
- Modify: `internal/hook/hook.go` (`Env`, `Run`), `internal/cli/hook.go` (`hookEnv` fills `Env.Stands`)
- Test: `internal/hook/hook_test.go`, `internal/cli/hook_test.go`

**Interfaces:**
- Consumes: `levels.ReadInstalled`, `levellog.Append`.
- Produces: `Env.Level levels.Installed` and `Env.LevelErr error`. In `Run` for hook `name` (component name = hook name). A **base** hook (`levels.File.Base`: `usage-gate`, `permission-check`, `workflow-push-gate`, `level-check`, and the new `no-verify-check` of Task 20) skips this whole block: no stand lookup, no `LevelErr` check, so a corrupt `components.toml` never blocks it. For every other hook:
  - `LevelErr != nil` → stderr `COULD NOT CHECK: <hook>: components.toml cannot be read: <err>`, return `ExitUnknown` (so `run.sh` applies `blocking`);
  - stand `off` → return 0 without running (a stale registration);
  - stand `log` → run the hook; when its result has a non-zero code, stderr, a `systemMessage` or Text, append one line (`name`, now, first line of that message) via `levellog.Append`; return exit 0 with no output, whatever happened, also on a panic or a failing `Append`;
  - otherwise unchanged.

- [ ] **Step 1: Write the failing tests** (register throwaway hooks in the test): `TestLogStandCountsAndStaysSilent` (hook returns code 2 + stderr → exit 0, empty out/err, one log line with the first line), `TestLogStandPanicStillExitsZero`, `TestLogStandWithUnwritableLogExitsZero`, `TestOffStandSkipsTheHook`, `TestOnStandIsUnchanged`, `TestCorruptComponentsFileIsExit3`, `TestBaseHookIgnoresCorruptComponentsFile` (`level-check` through `hook.Run` with a corrupt file → code 0 with `systemMessage`), `TestLogStandSuppressesVersionNotice`, `TestAbsentComponentsFileMeansOn`.
- [ ] **Step 2: Run** `go test ./internal/hook/... ./internal/cli/...` — Expected: FAIL.
- [ ] **Step 3: Implement.** On stand `log` `withNotices` is skipped too (the spec allows no `systemMessage` on `log`); on `on` the notices stay as they are.
- [ ] **Step 4: Run** — Expected: PASS; `go test ./internal/hooks/... ./internal/codelang/...` still PASS.
- [ ] **Step 5: Commit** `feat(hook): stand log en lezen van components.toml (#19)`.

### Task 13: `blocking` per niveau

**Files:**
- Modify: `internal/initcmd/levels.go` (`filterBlocking`), `internal/initcmd/initcmd.go`
- Test: `internal/initcmd/levels_test.go`

**Interfaces:**
- Produces: `func filterBlocking(u packs.Unit, r levels.Resolved) packs.Unit` — the source list of `pack/core/hooks/blocking`, minus every line whose component is not `on`; everything else stays, base hooks included. Never generates lines (so `worktree-check` never appears; this settles open point r3-f1). The content passes through `packs.Mark` like any file unit.

- [ ] **Step 1: Write the failing tests**: `TestBlockingOnStrictEqualsTodaysList` (byte-equal to `pack/core/hooks/blocking`), `TestBlockingOnLightDropsLogAndOff` (no `commit-test-gate`, `md-link-check`, `code-language-check`; has `main-commit-gate`, `pr-create-gate`, `bash-write-check`, `workflow-push-gate`), `TestBlockingNeverGainsWorktreeCheck`, `TestCoreGateOverrideOffDropsItsLine`.
- [ ] **Step 2: Run** `go test ./internal/initcmd/...` — Expected: FAIL.
- [ ] **Step 3: Implement** and apply after `selectUnits`.
- [ ] **Step 4: Run** — Expected: PASS.
- [ ] **Step 5: Commit** `feat(init): blocking-lijst per niveau (#19)`.

### Task 14: `bash-write-check` past de standen toe

**Files:**
- Modify: `internal/bashwrites/dispatch.go` (`runGate`), `internal/bashwrites/gates.go` (gate → component name map)
- Test: `internal/bashwrites/post_test.go`

**Interfaces:**
- Produces: `var GateComponents = map[string]string{"shell-lint-check.sh": "shell-lint-check", "ruff-format-check.sh": "ruff-format-check", "gofmt-check.sh": "gofmt-check", "md-link-check": "md-link-check", "code-language-check.sh": "code-language-check"}`. In `runGate`: when `levels.ReadInstalled(repoRoot)` gives `off` for the gate's component, the verdict is a skip with no notice (today's `"no <pack> check installed"` stays for a gate that is missing while its component is `on`). Gates on `log` still run: the nested `lighthouse hook` call applies `log` itself (Task 12).

- [ ] **Step 1: Write the failing tests**: `TestGateOffIsSilentNotMissing` (`.go` file written, `gofmt-check` off → no `no go check installed`), `TestGateMissingWhileOnStillNotices`, `TestGateOnLogRunsAndStaysQuiet` (fixture `run.sh` that calls the real `lighthouse hook`, so the nested `log` applies; the gate exits 2 under `log`: no verdict output).
- [ ] **Step 2: Run** `go test ./internal/bashwrites/...` — Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** — Expected: PASS.
- [ ] **Step 5: Commit** `feat(bashwrites): bash-write-check volgt de standen van het niveau (#19)`.

### Task 15: `lighthouse level`

**Files:**
- Create: `internal/cli/level.go`
- Modify: `internal/cli/root.go` (`root.AddCommand(newLevelCommand())`)
- Test: `internal/cli/level_test.go`

**Interfaces:**
- Produces: `lighthouse level` (read-only). Output on stdout, in this order: `level: <name> (since <date>)`; one `<component>  <stand>` row per non-`on` component plus a count of the `on` ones; for each `log` component `<component>  would have reported <n>× since <since>` (from `levellog.Count`); a section `not measured at this level` listing components `off` here and not `off` at the next level in `File.Order` (on `light`: `commit-test-gate`, the language-pack hooks). For each of `levels.CoreGates` that is not `on`: the same line as `init` (`lighthouse: <gate> is <stand> — this repo works outside the chain`), after the rows. No verdict "ready for standard". Without `components.toml`: `level: strict (no level set)`. Errors: unknown level in `lighthouse.toml` → exit 1 with the valid names (same `validate` as `init`).

- [ ] **Step 1: Write the failing tests** (golden stdout): `TestLevelShowsStandsAndCounts`, `TestLevelListsUnmeasuredComponentsOfNextLevel`, `TestLevelNeverClaimsReadiness` (no line contains `ready`), `TestLevelWarnsPerDisabledCoreGate`, `TestLevelWithoutComponentsFile`, `TestLevelBogusLevelFails`.
- [ ] **Step 2: Run** `go test ./internal/cli/...` — Expected: FAIL.
- [ ] **Step 3: Implement**; embedded packs via `pack.Files` and the loader of Task 2.
- [ ] **Step 4: Run** — Expected: PASS.
- [ ] **Step 5: Commit** `feat(level): lighthouse level toont niveau, tellingen en wat niet gemeten is (#19)`.

### Task 16: Dogfood en changelog deel 2

- [ ] **Step 1: Run** `go run ./cmd/lighthouse init --upgrade --yes` and commit the installed copy (`blocking` stays byte-identical on `strict`). Second run: stdout exactly `No changes.`
- [ ] **Step 2:** create `changelog.d/issue-19-instapniveaus-log.md` (`type: added`, Dutch).
- [ ] **Step 3: Run** the `[test_gate] command`, `golangci-lint run ./...` and `uv run pytest -q` — Expected: green.
- [ ] **Step 4: Commit** `docs(changelog): stand log en lighthouse level (#19)`. PR-grens deel 2.

---

# Deel 3: teksten en de kerngate-waarschuwing

### Task 17: Hook `level-check` op SessionStart

**Files:**
- Create: `internal/hooks/levelcheck.go`
- Modify: `pack/core/hooks/hooks.json` (a `SessionStart` group `run.sh level-check`, timeout 10), `internal/packs/packs.go` if `parseHooks` rejects `SessionStart`
- Test: `internal/hooks/levelcheck_test.go`

**Interfaces:**
- Produces: `func LevelCheck(ctx, root, stand string, p hook.Payload) hook.Result`, registered as `level-check`. Never blocks (code 0), so it gets **no** `guards.toml` entry (`internal/agentaudit/sources.go` classifies every guard as blocking). For each of `levels.CoreGates` whose stand is not `on`, a line `lighthouse: <gate> is <stand> — this repo works outside the chain`; the result has `systemMessage` (lines joined) and `hookSpecificOutput.additionalContext` (same text, `hookEventName: "SessionStart"`). All `on` or no `components.toml` → empty result. Unreadable → `COULD NOT CHECK` as `systemMessage`, still code 0. It is a base component: no stand of its own.

- [ ] **Step 1: Write the failing tests**: `TestLevelCheckSilentWhenGatesOn`, `TestLevelCheckWarnsForEachUnderstoodGate` (two gates off → two lines in both fields), `TestLevelCheckLogCountsAsNotOn`, `TestLevelCheckUnreadableFileSaysCouldNotCheckAndExitsZero`; a pack test that `hooks.json` registers it under `SessionStart`; also update `tests/test_site_packs.py` (`WHEN` map gets `SessionStart`, the core hook count), `tests/test_site_session.py` and the site data `docs/site/data/packs.json`/`session.json` so the pytest lane stays green.
- [ ] **Step 2: Run** `go test ./internal/hooks/... ./internal/packs/...` — Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** — Expected: PASS.
- [ ] **Step 5: Commit** `feat(hooks): level-check meldt een uitgezette kerngate bij elke sessie (#19)`.

### Task 18: Het managed block per niveau, met de kerngate-regel

**Files:**
- Create: `internal/initcmd/block.go`
- Modify: `pack/core/instructions/AGENTS.md`, `pack/go/instructions/AGENTS.md`, `pack/python/instructions/AGENTS.md`, `pack/shell/instructions/AGENTS.md` (add trailing markers), `internal/packs/budget_test.go` (raise each `blockCeilings` entry by exactly the number of markers added to that block, in this commit, as a deliberate choice stated in the PR), `tests/test_instruction_files.py` (compare the installed block with the source block after stripping markers), `internal/initcmd/initcmd.go`
- Test: `internal/initcmd/block_test.go`

**Interfaces:**
- Decision (builder's choice, spec "Aan de bouwer"): the block is **filtered**, not varied. A line ending in the one-word marker `<!--level:<component>-->` (one `wc -w` word per marker) is dropped when that component is not `on`; the marker is stripped from the lines that stay. `func filterBlock(u packs.Unit, r levels.Resolved) packs.Unit`. Mark the skill lines (`- /lighthouse-fix-pr: …` → `skill:fix-pr`, …), the `commit-test-gate` line, and the language-pack lines about `gofmt-check`, `go-lint-gate`, `go-standard`.
- Warning line: for each core gate not `on`, `filterBlock` appends to the core block `- Warning: <gate> is <stand> in this repo (override in [components]): it works outside the chain.` (English, in every session of Codex and OpenCode).

- [ ] **Step 1: Write the failing tests**: `TestBlockOnLightLacksOffSkillsAndTestGate`, `TestBlockOnStrictEqualsTodaysBlock` (byte-equal after marker stripping to the committed `AGENTS.md` managed block content), `TestBlockWarnsPerDisabledCoreGate`, `TestBlockLeavesNoMarkerBehind`.
- [ ] **Step 2: Run** `go test ./internal/initcmd/...` — Expected: FAIL.
- [ ] **Step 3: Implement** and apply after `selectUnits` (block units are in `desired`).
- [ ] **Step 4: Run** `go test ./internal/initcmd/... ./internal/packs/...` and `uv run pytest -q tests/test_instruction_files.py` — Expected: PASS; `lighthouse check-instructions` still green (word budget). Core's `--no-verify` rule becomes its own unmarked line (Task 20), so no level drops it.
- [ ] **Step 5: Commit** `feat(init): managed block per niveau met kerngate-waarschuwing (#19)`.

### Task 19: `calls` en de teksten van de skills

**Files:**
- Modify: `pack/core/levels.toml` (`[calls]`), the skill texts under `pack/core/skills/*/` (both `claude/` and `agents/` variants) and the language-pack skills
- Test: `internal/packs/calls_test.go`

**Interfaces:**
- Produces two tests over every level of `pack.Files`:
  - `TestSkillsOnlyCallSkillsThatAreOn`: for each skill that is `on`, every entry of its `calls` is `on` too;
  - `TestHardReferencesToOffSkillsAreConditional`: for each skill `S` and every `/lighthouse-<X>` in its text where `X` is `off` at a level where `S` is `on` and `X` is not in `S`'s `calls`, the line contains the phrase `if installed`.
- A real handover (`start-issue` → `route-issue`, `write-pr` → rules of `fix-pr` §6) becomes a `calls` entry, and when the target is off at a level where the caller is on, the table wins: the text goes conditional (`if installed`) or the rule moves into the caller's own text. `write-pr` always writes `Refs #N`.

- [ ] **Step 1: Write the failing tests** (they fail on today's texts).
- [ ] **Step 2: Run** `go test ./internal/packs/ -run 'Calls|Conditional'` — Expected: FAIL naming skill, line and level.
- [ ] **Step 3:** fix each reported spot (data in `[calls]`, text edits); repeat Step 2 until PASS.
- [ ] **Step 4: Run** `go test ./internal/packs/... ./internal/checkinstr/...` and `uv run pytest -q tests/pack` (skill-content tests such as `test_write_pr_skill.py`, `test_close_issue_skill.py`) — Expected: PASS.
- [ ] **Step 5: Commit** `docs(skills): calls en voorwaardelijke verwijzingen per niveau (#19)`.

### Task 20: `--no-verify` blijft geblokkeerd, ook zonder `commit-test-gate` en met kerngates uit

**Files:**
- Create: `internal/hooks/noverifycheck.go`
- Modify: `internal/commitgate/pre.go` (remove the block), `internal/commitgate/prepost_test.go` (the `no-verify` case at line 62), `pack/core/hooks/hooks.json` (PreToolUse `Bash` `run.sh no-verify-check`), `pack/core/levels.toml` (`no-verify-check` in `base`), `pack/core/instructions/AGENTS.md` (the `--no-verify` rule as its own unmarked line, so Codex and OpenCode keep it on every level), `pack/core/guards.toml` (`[no-verify-check]` with `events`/`terms`; update `commit-test-gate` terms), `tests/pack/test_commit_test_gate_no_verify.py` (move to `no-verify-check`)
- Test: `internal/hooks/noverifycheck_test.go`

**Interfaces:**
- Decision: the block moves to a new **base** hook `no-verify-check` (no stand, no override, never `off`), not to `main-commit-gate`: that is a core gate an override may switch off, and the spec says the refusal is "geen kerngate die je kunt uitzetten". `NoVerifyCheck` calls the existing `commitgate.CommandHasNoVerify(command, cwd)` and blocks with the text of `noVerifyBlock` under its own prefix `no-verify-check:` on every branch. `CommandHasNoVerify` is already exported from `internal/commitgate/noverify.go`; the constant text is copied, `commitgate` keeps no block.

- [ ] **Step 1: Write the failing tests**: `TestNoVerifyCheckBlocksOnAFeatureBranch`, `TestNoVerifyCheckBlocksShortNOnCommit`, `TestNoVerifyCheckAllowsNoVerifyWordInMessage` (`git commit -m "no --no-verify"`), `TestNoVerifyCheckStillBlocksWhenMainCommitGateIsOff` (components.toml with `main-commit-gate = "off"` through `hook.Run`), `TestNoVerifyCheckIgnoresCorruptComponentsFile`; remove the `no-verify` case from `internal/commitgate/prepost_test.go` (the pre stand no longer judges it; `fixtures_test.go` parser cases stay); move `tests/pack/test_commit_test_gate_no_verify.py` to run `run.sh no-verify-check`.
- [ ] **Step 2: Run** `go test ./internal/hooks/... ./internal/commitgate/...` — Expected: FAIL.
- [ ] **Step 3: Implement**; update `guards.toml` so `lighthouse agent-audit` keeps matching.
- [ ] **Step 4: Run** `go test ./internal/hooks/... ./internal/commitgate/... ./internal/agentaudit/...` and `uv run pytest -q tests/pack tests/test_site_packs.py` — Expected: PASS.
- [ ] **Step 5: Commit** `fix(hooks): --no-verify wordt op elk niveau geblokkeerd door no-verify-check (#19)`.

### Task 21: Installatiegids, dogfood en changelog deel 3

**Files:**
- Modify: `docs/install.md` (English: "Start small": begin on `light`, run `lighthouse level`, scale up with `lighthouse init --upgrade --level standard`; mention `[components]` overrides and the core-gate warning), `lighthouse.toml` not needed
- Create: `changelog.d/issue-19-instapniveaus-teksten.md` (`type: added`, Dutch)

- [ ] **Step 1:** write the section; relative links only to existing targets.
- [ ] **Step 2: Run** `go run ./cmd/lighthouse init --upgrade --yes`, commit the installed copy; second run prints `No changes.` on stdout.
- [ ] **Step 3: Run** the `[test_gate] command`, `golangci-lint run ./...`, `go run ./cmd/lighthouse check-instructions` — Expected: green.
- [ ] **Step 4: Commit** `docs(install): instaproute met niveaus (#19)`. PR-grens deel 3; `/lighthouse-write-pr` verifieert de AC's van #19 (tabel "Acceptatiecriteria van #19, gekoppeld" in de spec).

---

## Self-review

- **Spec-dekking:** ladder → Task 2; formaat/`level`/`[components]` → 1, 3; `init`-gedrag (nieuw, zonder level, wisselen, wisselherkenning, labels, bestand als unit, vereisten, permissions gelijk) → 4–9; kerngates → 6, 17, 18; stand `log` (logboek, `blocking`, `bash-write-check`, `lighthouse level`) → 11–15; `calls` → 19; `--no-verify` → 20 (base hook `no-verify-check`); dogfood en gids → 10, 16, 21. De drie "Aan de bouwer"-keuzes zijn vastgelegd: koppeling door afleiding plus `[owners]` (Task 2), logboek in `levellog` (Task 11), block gefilterd (Task 18). Open punt r3-f1: opgelost door filteren in plaats van genereren (Task 13).
- **Typen:** `levels.Resolved`, `levels.Installed`, `Stand` en `CoreGates` worden in Task 1 gedefinieerd en in 5–9, 12–15, 17–18 met dezelfde namen gebruikt.
- **Niet-doelen bewaakt:** geen eigen niveaus, geen knoppen van #493–#498, geen controller-knoppen (#255 leest `components.toml`).
