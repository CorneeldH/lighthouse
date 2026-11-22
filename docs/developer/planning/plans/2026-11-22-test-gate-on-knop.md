# Test-gate `on`-knop Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `[test_gate] on = "commit" | "pr" | "both"` bepaalt wanneer `commit-test-gate` de tests draait; zonder sleutel verandert niets.

**Architecture:** De sleutel wordt gelezen in `internal/commitgate` (nieuwe `Gate.readOn`). Het bestaande `commit-test-gate pre` (PreToolUse `Bash`, timeout 300) krijgt een tweede taak: een `gh pr create`, `glab mr create` of `lighthouse forge pr create` herkennen (met `prbody.FindPrCreates`) en dan `RunRoots` draaien op één rij `merge-base(origin/<default>, HEAD)..HEAD`. Zo is er geen nieuwe hookregistratie en past de run in de timeout van 300 s (`pr-create-gate` heeft er 15 en blijft ongemoeid). Onder `pr` slaan `Pre` en `Post` het commit-moment over.

**Tech Stack:** Go (`internal/commitgate`, `internal/hooks`, `internal/config`), pack-docs, `lighthouse init --upgrade`.

**Spec:** [issue #495](https://github.com/CorneeldH/lighthouse/issues/495), de acceptatiecriteria zijn de spec; er is geen aparte spec.

## Global Constraints

- Waarden van `test_gate.on`: `commit` (standaard), `pr`, `both`; elke andere waarde blokkeert met `COULD NOT CHECK`-stijl reden, zoals `checkRanges`.
- `pr` draait op de bestanden van `origin/<default branch>...HEAD`; de default branch komt uit `forge.default_branch` (geen GitHub-aanname buiten `internal/forge`, M3 no-go).
- Rood bij `pr` blokkeert de PR-create met de eerste foutregel van de testuitvoer.
- Zonder sleutel is elk gedrag, elke tekst en elke test van nu ongewijzigd.
- Artefacten (docs, changelog, commits) Nederlands; hookmeldingen en code-commentaar volgen de bestaande taal van het bestand (Engels in `internal/`).
- Voor elke commit: `[test_gate] command` en `lighthouse imports check`. Pack-wijziging ⇒ `go run ./cmd/lighthouse init --upgrade` en de geïnstalleerde kopie meecommitten (CI eist `No changes.`).
- Hooktimeouts in `pack/core/hooks/hooks.json` en `.claude/settings.json` blijven 300 (`commit-test-gate`) en 15 (`pr-create-gate`); `MaxDeadline` blijft 280.

## Review Focus

- `on = "pr"` en een commit op de default branch: geen test, geen opname van HEAD, geen import-check bij het commit-moment.
- `on = "both"` met een PR-create op een branch zonder commits boven `origin/<default>` (`base == head`): niets te testen, melding, geen blok.
- `origin/<default>` bestaat niet lokaal (nooit gefetcht): blokkeren met `COULD NOT CHECK`, geen stille groene doorgang.
- Een Bash-aanroep die `gh` en `pr` noemt maar geen create is (`gh pr view`): geen run, geen config-lezing die een kapotte `lighthouse.toml` blokkeert.
- `command = ""` met `on = "pr"`: dezelfde melding `no test command configured, nothing was tested`, PR gaat door.
- Commando `pwsh -File …` via `bash -c`, ook op Windows.

---

### Task 1: Sleutel `test_gate.on` in config en gate

**Files:**
- Modify: `internal/config/config.go` (sleutellijst rond :97-102, defaults rond :155-159)
- Modify: `internal/commitgate/gate.go` (nieuwe `readOn`, bij `readSettings`)
- Modify: `docs/site/data/config.json` (:379-447, tabel `test_gate`: sleutel `test_gate.on`, level `advanced`, default `"commit"`, `readers` = elk bestand in `pack/`, `internal/` en `cmd/` dat de sleutel noemt; de site-test `docs/site/test/config.test.mjs` leest `lighthouse config --schema` en `lighthouse.toml` laat `node --test` draaien bij een commit onder `internal/`, anders is de gate na deze commit rood met `not on the map: test_gate.on`)
- Modify: `tests/pack/fixtures/config-schema.json` (:80-102 sleutels, :265-269 defaults) en `tests/pack/fixtures/config/{defaults,full,scalars}/expect.json`
- Test: `internal/commitgate/gate_test.go`, `internal/config/config_test.go`

**Interfaces:**
- Produces: `func (g *Gate) readOn() (on string, block *Result)` — `on` is `"commit"`, `"pr"` of `"both"`; niet gezet geeft `"commit"` (een lege string weigert de configvalidator al, `mayBeEmpty` blijft `test_gate.command` alleen); andere waarde geeft een blok met `test_gate.on = "<x>"; it must be commit, pr or both.` Config-sleutel `{"test_gate.on", kStr}` met default `"commit"`.

- [ ] **Step 1: Failing tests.** `TestReadOnDefaultsToCommit` (geen sleutel → `commit`), `TestReadOnValues` (tabel: `commit`, `pr`, `both`), `TestReadOnRejectsUnknown` (`"always"` → `res.Code == 2` en stderr bevat `must be commit, pr or both`); in `config_test.go` de default `"commit"` via de bestaande defaults-test (:96).
- [ ] **Step 2:** `go test ./internal/commitgate/ ./internal/config/ -run 'ReadOn|Defaults'` → FAIL.
- [ ] **Step 3: Implementeer** de sleutel en `readOn` met `g.readConfig("test_gate.on")`; werk de vier fixturebestanden bij met de nieuwe sleutel en default.
- [ ] **Step 4:** `go test ./internal/commitgate/ ./internal/config/ ./internal/packs/ ./internal/initcmd/` → PASS; `node --test docs/site/test/config.test.mjs` → PASS (de sleutel staat op de kaart; als de test `options` voor `test_gate.on` verwacht, voeg ze toe in `config.json` en de verwachte kaart in dat testbestand).
- [ ] **Step 5: Commit** `feat(test-gate): test_gate.on als sleutel (#495)`.

### Task 2: Commit-moment volgt `on`

**Files:**
- Modify: `internal/commitgate/pre.go` (`Pre`, na de `--no-verify`-check), `internal/commitgate/post.go` (`Post`)
- Test: `internal/commitgate/prepost_test.go`

**Interfaces:**
- Consumes: `readOn` (Task 1).
- Produces: `Pre` en `Post` doen bij `on == "pr"` niets behalve `--no-verify` blokkeren: geen `commitRoots`, geen `Record`, geen `RunRoots`. `Post` leest `on` pas als `MovedRoots` een rij geeft (zonder commit geen config-lezing).

- [ ] **Step 1: Failing tests** (harness `newHarness`, `payload`): `TestPreOnPRSkipsDefaultBranchCommit` (commit op de default branch, `on = "pr"` → `Code 0`, `len(h.calls) == 0`); `TestPreOnPRStillBlocksNoVerify`; `TestPreOnBothTestsCommit` en `TestPreDefaultOnTestsCommit` (gedrag ongewijzigd); `TestPostOnPRRunsNothing`.
- [ ] **Step 2:** `go test ./internal/commitgate/ -run 'OnPR|OnBoth|DefaultOn'` → FAIL.
- [ ] **Step 3: Implementeer** de vroege uitgang in `Pre` (en `Post`) met `readOn`; `commit` en `both` lopen het bestaande pad.
- [ ] **Step 4:** `go test ./internal/commitgate/` → PASS, bestaande tests ongewijzigd groen.
- [ ] **Step 5: Commit** `feat(test-gate): on = pr slaat het commit-moment over (#495)`.

### Task 3: PR-moment in `commit-test-gate pre`

**Files:**
- Create: `internal/commitgate/pr.go` — `func Pr(ctx context.Context, g *Gate, command, cwd string) Result`
- Modify: `internal/commitgate/pre.go` (roept `Pr` aan wanneer het commando geen commit bevat), `internal/commitgate/gate.go` (`failLine` voor `g.Mode == "pr"`)
- Test: `internal/commitgate/pr_test.go`

**Interfaces:**
- Consumes: `prbody.FindPrCreates(command, cwd string) ([]prbody.PrCreate, error)` (`internal/prbody/find.go:295`; controleer met `go list -deps` dat `commitgate`→`prbody` geen importcirkel geeft, anders een seam `Gate.FindCreates` met dezelfde signatuur), `readOn`, `projectRoots`, `readConfig("forge.default_branch")`, `RunRoots`.
- Produces: `Pr` doet, in deze volgorde: (1) tweestapsregel op de ruwe tekst (`gh`/`glab`/`forge` en `pr`/`mr`), anders `Result{}` zonder config te lezen; (2) `FindPrCreates`, `ErrUnparseable` ⇒ blok als de tekst een create noemt, geen creates ⇒ `Result{}`; (3) `readOn`, bij `commit` ⇒ `Result{}`; (4) per create de root (`git rev-parse --show-toplevel` in `pc.Cwd`, anders `cwd`) via `projectRoots`; (5) `base = git merge-base origin/<default> HEAD`, `head = git rev-parse HEAD`, een mislukte git-aanroep blokkeert met `COULD NOT CHECK`; (6) `base == head` ⇒ melding `no commits above origin/<default>, nothing was tested`; anders `RunRoots([]Row{{root, base, head}}, "PreToolUse")` met `g.Mode = "pr"`. `failLine` voor `pr`: `commit-test-gate: tests failed on this branch. Fix them before creating the PR.` gevolgd door `First error: <regel>`, de eerste regel van de uitvoer die `(?i)fail|error|panic` bevat, anders de eerste niet-lege regel; de volledige uitvoer blijft eronder staan. `--repo`/`GH_REPO` wordt genegeerd: een create in dit project draait de gate (vastgelegd in de docs, Task 4).

- [ ] **Step 1: Failing tests** (echte `git init`-fixture met een `origin`-remote, `h.calls` voor de run): `TestPrOnPRRunsOnBranchDiff` (`on = "pr"`, `gh pr create --title x` ⇒ één run met `LIGHTHOUSE_GATE_BASE` = merge-base en `_FILES` met de branchbestanden); `TestPrOnBothRunsAtPR`; `TestPrDefaultOnDoesNothing`; `TestPrRedBlocksWithFirstErrorLine` (testcommando faalt met `FAIL TestX` ⇒ `Code 2`, stderr bevat `First error: FAIL TestX`); `TestPrNoCommitsAboveBase`; `TestPrMissingOriginRefBlocks` (`COULD NOT CHECK`); `TestPrNotACreate` (`gh pr view 3` ⇒ `Code 0`, `h.calls` leeg, ook met kapotte `lighthouse.toml`); `TestPrCommandEmptyWithOnPR` (`command = ""` ⇒ nota, `Code 0`); `TestPrForgeAndGlabForms` (`lighthouse forge pr create`, `glab mr create`); `TestPrCombinedCommitAndCreateBlocks` (`git commit -m x && gh pr create`: `on = "pr"` en `"both"` ⇒ `Code 2` met de splits-reden, `on = "commit"` ⇒ commit-pad); `TestPrBlockMessageNamesPR` (ontbrekende `origin/<default>` noemt geen `git commit`).
- [ ] **Step 2:** `go test ./internal/commitgate/ -run '^TestPr'` → FAIL.
- [ ] **Step 3: Implementeer** `Pr` en de aanroep in `Pre` (een commando met commit blijft het commit-pad; een commando met zowel commit als create, `git commit … && gh pr create …`, blokkeert bij `on = "pr"` of `"both"` met de reden `split the commit and the PR create into separate Bash calls: the PR run needs the new commit as HEAD`, want de hook draait vóór de aanroep en `HEAD` is dan nog de oude commit; bij `on = "commit"` (standaard) blijft het commit-pad ongewijzigd). Geef `Mode "pr"` ook een eigen staart in `Block` en in de slot-, geheugen- en import-meldingen (`message.go`, `gate.go`, `imports.go`: nu per `Mode == "post"` of commit-tekst), zodat een blok bij een PR-create niet over `git commit` spreekt.
- [ ] **Step 4:** `go test ./internal/commitgate/ ./internal/hooks/` → PASS; `go vet ./... && golangci-lint run ./internal/commitgate/...`.
- [ ] **Step 5: Commit** `feat(test-gate): on = pr draait de gate voor de PR-create op de branch-diff (#495)`.

### Task 4: Hook-integratie en docs

**Files:**
- Modify: `internal/hooks/committestgate.go` (doc-comment; geen nieuwe modus), `internal/hooks/committestgate_test.go`
- Modify: `docs/test-gate.md`, `docs/developer/areas/core.md`
- Modify: `pack/core/instructions/AGENTS.md` (regel `commit-test-gate runs …`, zie het geïnstalleerde `AGENTS.md:48`)
- Create: `changelog.d/issue-495-test-gate-on-knop.md` (`type: added`, Nederlandse tekst)
- Modify (door `init --upgrade`): `AGENTS.md`

**Interfaces:**
- Consumes: Task 1–3. Produces: een hook-test die bewijst dat `CommitTestGate(ctx, root, "pre", payload("gh pr create …"))` met `on = "pr"` de gate draait.

- [ ] **Step 1: Failing test** `TestCommitTestGatePreRunsAtPRCreate` in `internal/hooks/committestgate_test.go`.
- [ ] **Step 2:** `go test ./internal/hooks/ -run PRCreate` → FAIL, na Task 3 PASS; pas anders alleen de doc-comment aan.
- [ ] **Step 3: Documenteer** in `docs/test-gate.md` een sectie `## Wanneer draait de gate: [test_gate] on` (drie waarden, standaard `commit`; `pr` = branch-diff, import-check meegeschoven naar het PR-moment, `--repo` genegeerd, `command = ""` blijft de uitzetknop; trage tests ⇒ `pr`), het deadlinelimiet (`MaxDeadline` 280 s binnen de hooktimeout van 300 s geldt ook bij het PR-moment: een suite van minuten geeft `COULD NOT CHECK (deadline)` en blokkeert de PR-create; houd de `pr`-run onder die grens), de managed-blocktekst (`commit-test-gate runs [test_gate] command after each commit, before a PR create, or both, as [test_gate] on says; fix red in a new commit.`) en het changelogfragment.
- [ ] **Step 4:** `go run ./cmd/lighthouse init --upgrade` ⇒ commit de geïnstalleerde kopie; daarna nogmaals ⇒ `No changes.`; `go test ./... && go run ./cmd/lighthouse imports check`.
- [ ] **Step 5: Commit** `docs(test-gate): on-knop in docs en managed block (#495)`.

### Task 5: `pwsh -File` als testcommando, getest op Windows-CI

**Files:**
- Create: `internal/commitgate/pwsh_test.go`
- Modify: `.github/workflows/windows-measure.yml` (`paths:` krijgt `internal/commitgate/**`)

**Interfaces:**
- Consumes: `Gate.RunRoots` met de echte `Bash` en `deadline.RunCapture`. Produces: `TestPowerShellCommandRuns` en `TestPowerShellCommandRedBlocks`, beide `t.Skip` als `exec.LookPath("pwsh")` of `bash` ontbreekt; het testcommando is `pwsh -NoProfile -File <tmp>/gate.ps1` met een script dat `exit 0` of `Write-Output "FAIL x"; exit 1` doet (pad via `filepath.ToSlash` in het commando, want `bash -c`).

- [ ] **Step 1: Schrijf** de twee tests (rij zonder Base, `Mode "pre"`, echte executor); verwacht: groen ⇒ `Code 0`; rood ⇒ `Code 2` en `tests failed`.
- [ ] **Step 2:** `go test ./internal/commitgate/ -run PowerShell -v` lokaal (SKIP zonder `pwsh`, anders PASS).
- [ ] **Step 3:** De bestaande stap `go test -shuffle=on ./...` van `windows-measure` draait ze op `windows-latest` (daar staat `pwsh`); de `paths:`-regel laat de workflow bij een wijziging in `internal/commitgate/` lopen. Een wijziging onder `.github/workflows/` pusht de workflow-pusher (`next: push-workflows`), niet deze run; noem dat in de handoff. Het bewijs van de Windows-run is de job summary van die workflow; zonder die run blijft het AC `getest op Windows-CI` open en zegt de PR dat zo.
- [ ] **Step 4: Commit** `test(test-gate): pwsh -File als testcommando (#495)`.

## Self-review

- Spec: waarden/standaard (T1), geen gedragsverandering (T1+T2 tests), `pr`-branch-diff en blok met eerste foutregel (T3), pwsh/Windows (T5), docs en managed block (T4).
- Types: `readOn() (string, *Result)`, `Pr(ctx, *Gate, command, cwd string) Result`, `Row{Root, Base, Head}` komen in alle taken gelijk terug.
- Open punt voor review: `import-check` schuift onder `pr` mee naar het PR-moment, en `--repo` wordt genegeerd; beide zijn bewuste keuzes, vastgelegd in `docs/test-gate.md`.
