# Taalpakket powershell met PSScriptAnalyzer — implementatieplan

> **Voor agentic workers:** VERPLICHTE SUB-SKILL: gebruik superpowers:subagent-driven-development (aanbevolen) of superpowers:executing-plans om dit plan taak voor taak uit te voeren. Stappen gebruiken checkbox-syntax (`- [ ]`).

**Doel:** `pack/powershell` toevoegen, naar het model van `pack/shell`: `lighthouse init` herkent `*.ps1`/`*.psm1`, en een PostToolUse-hook draait PSScriptAnalyzer (via `pwsh`) op wat Edit, Write of een Bash-aanroep schreef en blokkeert op een vaste set regels.

**Architectuur:** Een nieuw pakket `pack/powershell` met `detect.toml`, `requires.toml`, `levels.toml`, een hook `powershell-lint-check.sh` en een helper `psscriptanalyzer-repo.ps1` die `Invoke-ScriptAnalyzer -IncludeRule <vaste set>` draait. De hook faalt luid (`COULD NOT CHECK`, exit 2) als `pwsh` of de module ontbreekt. `init` meldt dat via `requires.toml`; `internal/deps` leert de module te proben (zoals `tomllib`). `internal/bashwrites` krijgt `.ps1`/`.psm1` zodat een Bash-schrijfactie ook gepoort wordt.

**Tech stack:** Go (init, deps, bashwrites, canary-tests), bash (hook), PowerShell 7 (`pwsh`) + PSScriptAnalyzer, GitHub Actions.

**Issue:** #581. Er is geen aparte spec; de acceptatiecriteria van het issue en de milestone M3 (geen host-specifieke skills of units in pakketten, geen nieuwe GitHub-aannames in `pack/`) zijn de bron.

## Globale randvoorwaarden

- Artefacten (commits, changelog, plan) in het Nederlands; hooks, skills en hookmeldingen in het Engels (`AGENTS.md`).
- Nooit `.claude/hooks/lighthouse/` met de hand bewerken: wijzig `pack/` en draai `go run ./cmd/lighthouse init --upgrade`, commit de geïnstalleerde kopie (dogfood; CI eist `No changes.`).
- Elke PR een fragment in `changelog.d/`.
- Voor elke commit: `[test_gate] command` en `lighthouse imports check`. Nooit `--no-verify`.
- Een ontbrekend hulpmiddel is een fout, nooit een stille skip (ook niet in de canary-job met `CI` gezet).
- Geen skill in dit pakket (YAGNI; de milestone verbiedt geen skill, maar het issue vraagt er geen). Daarom bevat `levels.toml` alleen de hook.

## Review Focus

- Een `.ps1` met een BOM of CRLF-regeleinden: de poort moet nog steeds vangen, niet crashen.
- Een bestandspad met spaties of haakjes (`C:\Users\A B\x (1).ps1`): moet aan `pwsh` doorgegeven worden zonder dat het in stukken valt.
- `pwsh` aanwezig maar module ontbreekt: hook exit 2 met `COULD NOT CHECK`, niet 0.
- Een schone `.ps1` en een `.ps1` met alleen niet-blokkerende bevindingen (bijv. `PSAvoidUsingWriteHost`): exit 0.
- Een bestand dat tussen Edit en hook is verwijderd, of een payload zonder `file_path`: exit 0.

## Bestandsstructuur

- Create: `pack/powershell/detect.toml`, `requires.toml`, `levels.toml`
- Create: `pack/powershell/hooks/hooks.json`, `pack/powershell/hooks/powershell-lint-check.sh`
- Create: `pack/powershell/scripts/psscriptanalyzer-repo.ps1`
- Modify: `pack/SOURCES.toml` (een `[[file]] … new = true` per nieuw bestand)
- Modify: `internal/deps/deps.go` (`PSScriptAnalyzer`-probe)
- Modify: `internal/bashwrites/gates.go` (`.ps1`, `.psm1`)
- Create: `internal/canary/powershell_test.go` + `internal/canary/testdata/powershell-lint-check/<Regel>.ps1`
- Modify: `internal/packs/core_test.go`, `internal/deps/deps_test.go`, `internal/bashwrites/*_test.go`, `internal/initcmd/select_test.go`, `ci/domains/domains_test.go` (pins en gevallen)
- Modify: `ci/windows-measure.sh`, `.github/workflows/windows-measure.yml`, `.github/workflows/ci.yml` (canary-job)
- Modify: `docs/levels.md`, `docs/host-assumptions.md`
- Create: `changelog.d/issue-581-taalpakket-powershell.md`
- Dogfood: `.claude/`, `.lighthouse/` via `init --upgrade`

## Blokkerende regels (vast, bron van de canary-ID's)

`BLOCKING_RULES="PSAvoidUsingInvokeExpression,PSAvoidUsingPlainTextForPassword,PSAvoidUsingConvertToSecureStringWithPlainText,PSAvoidUsingEmptyCatchBlock,PSUseDeclaredVarsMoreThanAssignments"`

De hook geeft deze set expliciet door (`-IncludeRule`), leest dus geen `PSScriptAnalyzerSettings.psd1` van de gebruiker en kan niet door zo'n bestand uitgezet worden. Daarom geen floor-check zoals bij shell.

---

### Taak 1: Pakketskelet, herkenning en niveaus

**Bestanden:**
- Create: `pack/powershell/detect.toml`, `requires.toml`, `levels.toml`, `hooks/hooks.json`
- Modify: `pack/SOURCES.toml`, `internal/packs/core_test.go:173-186`, `internal/initcmd/select_test.go`
- Test: `internal/packs/core_test.go`, `internal/initcmd/select_test.go`

**Interfaces:**
- Produceert: pakketnaam `powershell`, component `powershell-lint-check` (gebruikt door taken 2–4).

- [ ] **Stap 1: Schrijf de falende tests.** In `core_test.go` een geval `name: "powershell"` met `files` = `.claude/hooks/lighthouse/powershell-lint-check.sh` en `.lighthouse/scripts/psscriptanalyzer-repo.ps1`, `detect: true`, `tools: "PSScriptAnalyzer,git,jq,pwsh"` (bytewise gesorteerd: hoofdletters eerst, `packs.go:644`), `blocks: 0`, `hooks: 1`. In `select_test.go` een test `TestDetectPowershell`: een repo met `scripts/a.ps1` (en een met `m.psm1`) stelt `powershell` voor; een repo zonder beide niet.
- [ ] **Stap 2: Draai ze, verwacht FAIL.** `go test ./internal/packs/ ./internal/initcmd/ -run 'Powershell|TestCore' -v`
- [ ] **Stap 3: Maak de drie TOML-bestanden.** `detect.toml`: `files = ["**/*.ps1", "**/*.psm1"]` met een commentaarkop naar `pack/shell/detect.toml`. `requires.toml`: `pwsh = ["powershell-lint-check"]`, `PSScriptAnalyzer = ["powershell-lint-check"]`, `git`, `jq` voor `powershell-lint-check`. `levels.toml`: `[owners]` `"scripts/psscriptanalyzer-repo.ps1" = "powershell-lint-check"`; `[light.components] powershell-lint-check = "off"`. Controleer in `internal/levels` hoe `standard` en `strict` uitgedrukt worden; staat `on` daar niet impliciet voor een component zonder regel, voeg dan expliciete `[standard.components]`/`[strict.components]` `= "on"` toe, zodat elk niveau een regel heeft (AC 5).
- [ ] **Stap 4: Voeg `[[file]]`-regels toe aan `pack/SOURCES.toml`** voor elk bestand van het pakket (`new = true`), naar `:284-314` en `:397`.
- [ ] **Stap 5: Draai de tests, verwacht PASS** (de hook en het script bestaan nog niet: maak in deze stap ook `hooks/hooks.json` (PostToolUse, matcher `Edit|Write`, timeout 60, naar `pack/shell/hooks/hooks.json`; hookunits komen alleen uit dat bestand, `packs.go:401`, anders faalt `hooks: 1`) en de hook en het script als plaatsvervangers die taak 2 vult, zodat `files` klopt. Het `.ps1`-script krijgt een eerste regel `#!/usr/bin/env pwsh`: zonder `#!` installeert `fileUnit` het als 0644 (`packs.go:415`) en vindt de `-x`-test in de hook het niet).
- [ ] **Stap 6: Commit** `feat(powershell): pakketskelet met herkenning en niveaus (#581)`.

### Taak 2: De hook en het analyse-script

**Bestanden:**
- Create/Modify: `pack/powershell/hooks/powershell-lint-check.sh`, `scripts/psscriptanalyzer-repo.ps1` (hooks.json komt uit taak 1)
- Modify: `internal/canary/lists_test.go` (`ruleListHooks`: `"powershell-lint-check": "powershell/hooks/powershell-lint-check.sh"`, anders telt `testdata/powershell-lint-check` als stray directory)
- Test: `internal/canary/powershell_test.go`, `internal/canary/testdata/powershell-lint-check/*.ps1`

**Interfaces:**
- Consumeert: component `powershell-lint-check` (taak 1).
- Produceert: de hook blokkeert met exit 2 en `[powershell-lint-check/<Regel>]` op stderr; `COULD NOT CHECK` (exit 2) bij ontbrekend `pwsh`/module/jq/helper. `BLOCKING_RULES` op één regel (zoals `shell-lint-check.sh:25`, anders leest `blockingRules(t, …)` het niet).
- `psscriptanalyzer-repo.ps1 -Path <bestand> -Rules <komma-lijst>` schrijft per bevinding `RuleName<TAB>Line<TAB>Message` naar stdout; exit 10 bij bevindingen (een code die `pwsh -File` zelf nooit geeft: bij een ongevangen fout geeft pwsh rc 1), 0 schoon, 3 als de module ontbreekt, elke andere uitzondering (de hele scriptbody in try/catch) exit 4.

- [ ] **Stap 1: Schrijf de canary-test** `TestPowershellLintCanary` in `internal/canary/powershell_test.go` (`//go:build canary`), naar `shell_test.go:96-106`: per ID uit `blockingRules(t, "powershell/hooks/powershell-lint-check.sh")` een subtest met `installRepo(t, "core", "powershell")`, fixture `testdata/powershell-lint-check/<ID>.ps1`, `requireCanary(t, r, id, "[powershell-lint-check/"+id+"]")`. Voeg ook `TestPowershellLintBijection` toe met `bijectionProblem("powershell-lint-check", ids)` zodat elke regel een fixture heeft en andersom. Eén fixture per regel, minimaal en alleen die regel schendend, bijv. `PSAvoidUsingInvokeExpression.ps1`: `Invoke-Expression "Get-Date"`.
- [ ] **Stap 2: Schrijf de overige hooktests** (zelfde bestand): schoon bestand → exit 0; alleen `Write-Host` → exit 0; payload zonder `file_path` → exit 0; pad met spaties en haakjes met een schending → exit 2; bestand met BOM en CRLF met een schending → exit 2; `PATH` zonder `pwsh` → exit 2 met `powershell-lint-check: COULD NOT CHECK`; module ontbreekt → exit 2 met `COULD NOT CHECK`; simuleer dat met een stub-`pwsh` op `PATH` die op stderr een melding schrijft en met exit 3 stopt (`PSModulePath` leegmaken verbergt de module niet: PowerShell 7 voegt de gebruikers-, AllUsers- en `$PSHOME`-paden zelf weer toe). Een stub-`pwsh` met rc 1 en lege stdout → exit 2 met `COULD NOT CHECK` (geen vals blok). `PATH` zonder `pwsh`: bouw een tijdelijke bin-map met symlinks naar `bash`, `jq`, `git`, `dirname`, `mktemp` en gebruik die als enige `PATH`; assert de pwsh-specifieke melding. Versiepin: het helper-script krijgt een optionele parameter `-Version`; is die gezet, dan `Import-Module PSScriptAnalyzer -RequiredVersion $Version` (anders zonder versie), mislukt: exit 3. De hook geeft `-Version "$LIGHTHOUSE_PSSA_VERSION"` mee wanneer die omgevingsvariabele gezet is. Canary: met `LIGHTHOUSE_PSSA_VERSION` = de in CI gepinde versie geeft de bad-fixture exit 2 met de regeltag (de gepinde versie is dus geladen); met een niet-bestaande versie (`0.0.1`) geeft dezelfde fixture exit 2 met `COULD NOT CHECK` (bewijst dat de versie wordt afgedwongen). De pin wordt zo bewezen zonder dat de helper een versie hoeft te rapporteren. Een ontbrekend `pwsh` of module op `CI` is een falende test, geen skip.
- [ ] **Stap 3: Draai, verwacht FAIL.** `go test -race -tags canary ./internal/canary/ -run Powershell -v`
- [ ] **Stap 4: Implementeer `psscriptanalyzer-repo.ps1`.** `Import-Module PSScriptAnalyzer` met `-ErrorAction Stop` en, als `-Version` gezet is, `-RequiredVersion` (mislukt: stderr-melding, exit 3); `Invoke-ScriptAnalyzer -Path $Path -IncludeRule $Rules.Split(',')`; schrijf bevindingen als hierboven.
- [ ] **Stap 5: Implementeer `powershell-lint-check.sh`** naar `shell-lint-check.sh`: `fail()` met `powershell-lint-check: COULD NOT CHECK — …`; payloadfilter op `.ps1"`/`.psm1"`; `jq` voor `.tool_input.file_path`; `[ -f ]`-guard; root via `LIGHTHOUSE_REPO_ROOT` of git-toplevel zoals `:62-90` en het script uit `.lighthouse/scripts/`; `command -v pwsh` anders `fail`; roep `pwsh -NoProfile -NonInteractive -File "$SCRIPT" -Path "$FILE_PATH" -Rules "$BLOCKING_RULES"` aan (padargument tussen aanhalingstekens, nooit via een shellstring); rc 0 → exit 0, rc 10 → per bevinding `[powershell-lint-check/<Regel>] …` plus uitleg ("fix this in the code; a suppression needs a reason: `[Diagnostics.CodeAnalysis.SuppressMessageAttribute(...)]`") op stderr, exit 2; rc 10 zonder minstens één parsebare regel `Regel<TAB>Regelnr<TAB>Melding` → `fail` (COULD NOT CHECK); andere rc → `fail`. Het script zoeken met `[ -f ]` (het draait via `pwsh -File`, hoeft niet uitvoerbaar te zijn). `hooks.json`: PostToolUse, matcher `Edit|Write`, timeout 60, naar `pack/shell/hooks/hooks.json`.
- [ ] **Stap 6: Draai, verwacht PASS.** Zelfde commando. Voer daarna een mutatiecontrole uit: haal één regel uit `BLOCKING_RULES`, de bijectietest of zijn canary moet rood worden; zet terug.
- [ ] **Stap 7: Commit** `feat(powershell): lint-hook met PSScriptAnalyzer en canaries (#581)`.

### Taak 3: `init` meldt een ontbrekende module, en Bash-schrijfacties worden gepoort

**Bestanden:**
- Modify: `internal/deps/deps.go:36-63`, `internal/bashwrites/gates.go:21-56`, `internal/canary/bashwrite_test.go` (`gateCases`: `"powershell-lint-check.sh": {".ps1", "testdata/powershell-lint-check/PSAvoidUsingInvokeExpression.ps1", "[powershell-lint-check/PSAvoidUsingInvokeExpression]"}`; en `"powershell"` in de `installRepo` van `TestBashWriteCheckCanaryPerGate`, anders faalt `TestBashWriteCheckGateSetHasNoRemainder`)
- Test: `internal/deps/deps_test.go`, `internal/bashwrites/post_test.go`, `internal/bashwrites/scenario_test.go`, `internal/codelang/testdata/bash_writes_nl.py.txt` (alleen als de bijectie-/spiegeltest erom vraagt)

**Interfaces:**
- Produceert: `Checker.present("PSScriptAnalyzer")` is waar als `pwsh` op `PATH` staat en `pwsh -NoProfile -NonInteractive -Command "if (-not (Get-Module -ListAvailable PSScriptAnalyzer)) { exit 1 }"` rc 0 geeft. Gates: `.ps1` en `.psm1` → `{"powershell-lint-check.sh"}`; `GatePacks["powershell-lint-check.sh"] = "powershell"`; `GateComponents["powershell-lint-check.sh"] = "powershell-lint-check"`.

- [ ] **Stap 1: Schrijf de falende tests.** In `deps_test.go`: `PSScriptAnalyzer` ontbreekt zonder `pwsh`; ontbreekt als de `Run`-stub een fout geeft; aanwezig als `pwsh` gevonden wordt en `Run` slaagt; het commando bevat `-NoProfile`. In `bashwrites`: een Bash-schrijfactie naar `x.ps1` en `x.psm1` draait `powershell-lint-check.sh`; ontbreekt het gate-bestand, dan noemt de melding pakket `powershell`; staat het component uit, dan draait het niet. Volg de bestaande `.sh`-gevallen in `post_test.go:552`.
- [ ] **Stap 2: Draai, verwacht FAIL.** `go test ./internal/deps/ ./internal/bashwrites/ -v`
- [ ] **Stap 3: Implementeer** de `PSScriptAnalyzer`-tak in `present` naast `tomllib` en de drie map-uitbreidingen. `.ps1`/`.psm1` krijgen *geen* `code-language-check.sh` (die kent die extensies niet: `internal/codelang/check.go:28`).
- [ ] **Stap 4: Draai, verwacht PASS**, plus `go test ./internal/initcmd/ ./internal/hookcheck/ ./internal/packs/` en repareer pins die het nieuwe pakket tellen (hookaantallen, lijsten van pakketten); verander geen bestaande verwachtingen van andere pakketten.
- [ ] **Stap 5: Controleer AC 4 end-to-end:** een test in `internal/initcmd` met `Deps` zonder `pwsh` en een met `pwsh` zonder module; `Err` bevat `missing dependency: pwsh (needed by: powershell-lint-check)` resp. `missing dependency: PSScriptAnalyzer (needed by: powershell-lint-check)`.
- [ ] **Stap 6: Commit** `feat(powershell): init meldt ontbrekende module, Bash-schrijfacties gepoort (#581)`.

### Taak 4: CI (Linux-canary en Windows), documentatie, dogfood en changelog

**Bestanden:**
- Modify: `.github/workflows/ci.yml` (job `canary`), `.github/workflows/windows-measure.yml`, `ci/windows-measure.sh`, `ci/domains/domains_test.go`, `docs/levels.md:29`, `docs/host-assumptions.md`
- Create: `changelog.d/issue-581-taalpakket-powershell.md`
- Dogfood: gegenereerde kopie

**Interfaces:**
- Consumeert: de hook (taak 2).

- [ ] **Stap 1: Linux-canary in CI.** Voeg in job `canary` na de bestaande stappen een stap toe die de module installeert (`sudo pwsh -NoProfile -Command "Install-Module PSScriptAnalyzer -RequiredVersion <vaste versie> -Scope AllUsers -Force"` en exporteer dezelfde versie als `LIGHTHOUSE_PSSA_VERSION` voor de teststap: `runHook` geeft elke hook een lege `HOME`, dus een CurrentUser-module is er onzichtbaar (`harness_canary_test.go:150`); `pwsh` staat op `ubuntu-latest`), vóór `go test -tags canary`. Pin de versie op een bestaande stabiele release en noteer die in de stap; controleer dat `CI` gezet een ontbrekende module als fout laat falen.
- [ ] **Stap 2: Windows-CI.** In `windows-measure.yml`: voeg `pack/powershell/**` toe aan de `pull_request`-paths; installeer PSScriptAnalyzer in job `hooks` (`pwsh` staat op `windows-latest`). In `ci/windows-measure.sh`: laat rij 1 (`Packages: core`) en rij 12 ongemoeid; draai na rij 16 `"$BIN" init --upgrade --yes --pack powershell` in `$REPO` (`--pack` is herhaalbaar, geen komma-lijst: `init.go:90`) voor één extra rij 17: Write-payload (`HOOK_FILE`) naar een tempbestand `bad.ps1` met `Invoke-Expression "x"` → verwacht rc 2 en `[powershell-lint-check/PSAvoidUsingInvokeExpression]`; rij 18: schoon `.ps1` → rc 0. Controleer dat de rij op Linux ook slaagt (het script moet daar draaien).
- [ ] **Stap 3: Domeinen.** Voeg in `ci/domains/domains_test.go` een geval `{"pack/powershell/x"}` toe; verwachte uitkomst `set("go")` als de logica `pack/**` al naar `go` mapt (controleer in `ci/domains/domains.go`; wijzig de logica alleen als de test het eist).
- [ ] **Stap 4: Documentatie.** `docs/levels.md`: voeg `powershell-lint-check` toe aan de regel met lint- en formathooks. `docs/host-assumptions.md`: een rij voor `pack/powershell` (vereist `pwsh` en PSScriptAnalyzer; Linux, macOS en Windows; niet nodig tenzij de repo `.ps1`/`.psm1` heeft). Alleen relatieve links.
- [ ] **Stap 5: Changelog.** `changelog.d/issue-581-taalpakket-powershell.md` met front matter `type: added` en een Nederlandse zin eindigend op `(#581)`.
- [ ] **Stap 6: Dogfood.** `go run ./cmd/lighthouse init --upgrade`, daarna `git status`: commit wat verschijnt. Draai `go run ./cmd/lighthouse init --upgrade` nogmaals; moet `No changes.` melden.
- [ ] **Stap 7: Volledige verificatie.** `go test -race -shuffle=on ./...`, `go test -race -tags canary ./internal/canary/ -v`, `lighthouse imports check`, `[test_gate] command`; allemaal groen. Ontbreekt lokaal `pwsh`, meld dat in de PR (de canary slaagt dan alleen in CI).
- [ ] **Stap 8: Commit** `feat(powershell): CI, documentatie en dogfood (#581)` (atomaire splitsing per onderdeel is toegestaan).

## Zelfreview

- AC1 (init herkent): taak 1. AC2 (hook op Edit/Write/Bash): taak 2 + 3. AC3 (canary per regel, bijectie): taak 2. AC4 (requires.toml, niet stil): taak 1 + 3 + hookfout in taak 2. AC5 (niveaus): taak 1. AC6 (Linux en Windows-CI): taak 2 + 4.
- Open keuze die de uitvoerder bevestigt: of `standard`/`strict` expliciet in `levels.toml` moeten (taak 1, stap 3), en de PSScriptAnalyzer-versie die CI pint (taak 4, stap 1).
- Risico: PSScriptAnalyzer is een module, geen gepind en met sha256 geverifieerd binair; versiepin gebeurt in CI en de hook rapporteert de versie niet. Een eigen ontwerp voor pinnen valt buiten dit issue.
