# Teststubs op het PATH uitvoerbaar op Windows — implementatieplan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Eén gedeelde testhelper maakt nep-programma's (`git`, `codex`, `roborev`, `npm`, `claude`, `gh`) die op elk platform uitvoerbaar zijn, en alle testhelpers die het `PATH` bouwen gebruiken die helper.

**Architecture:** Een nieuw testpakket `internal/stubbin` (zelfde vorm als `internal/fixturesetup`: neemt een `*testing.T`, faalt de test bij een fout). Op Linux en macOS schrijft het het bestaande `#!/bin/sh`-script; op Windows een `.cmd`-bestand voor de eenvoudige gevallen (vaste uitvoer en exitcode, argumentenlog). Een stub die echt POSIX-gedrag nabootst (`case`, `$@`, `chmod`) slaat op Windows over via `stubbin.RequirePOSIX(t, reden)`. Het `PATH` wordt altijd met `os.PathListSeparator` gebouwd en behoudt het bestaande `PATH` of de directory's van expliciet genoemde tools; nooit `/usr/bin:/bin` als vaste waarde.

**Tech Stack:** Go (`testing`, `os`, `os/exec`, `runtime`), CI-job `windows-measure` (`.github/workflows/windows-measure.yml`, job `tests`).

**Spec:** geen spec; het issue [#566](https://github.com/CorneeldH/lighthouse/issues/566) met zijn acceptatiecriteria is de opdracht (route 4b: de AC leggen de HOW vast). Meting en indeling staan in #559.

## Global Constraints

- Code, namen en commentaar in het Engels als `[project] code_language` gezet is; het plan, het commitbericht en het issue zijn Nederlands. Fragment in `changelog.d/` in het Engels, type `changed`.
- Geen productiecode wijzigen: alleen testhelpers, hun aanroepers en het nieuwe testpakket.
- Voor elke commit: `lighthouse imports check` naast `[test_gate] command` (zie `lighthouse.toml` `[test_gate]`).
- Elke taak eindigt met `GOOS=windows go vet ./...` (compileert alle Windows-testbestanden, ook die niet draaien) en `go test -shuffle=on ./<pakketten van de taak>/...` groen op Linux.
- Een test die op Windows overslaat noemt altijd een reden die zegt welk POSIX-gedrag de stub nabootst.
- Bestaande `runtime.GOOS == "windows"`-skips (53 hits in 45 bestanden) blijven; een skip mag alleen weg als de test op Windows aantoonbaar slaagt.

## Review Focus

- Een stub waarvan het script `$@`, `case`, `read` of een pipe gebruikt: de helper moet dit niet stil als `.cmd` proberen te vertalen; de test slaat over met reden.
- `PATH` dat op Windows `git` of `cmd.exe` nodig heeft: een `PATH` met alleen de stubmap breekt `exec.LookPath("git")`; de helper behoudt het bestaande `PATH` of de map van genoemde tools.
- Een stub met een spatie of `'` in de uitvoer (`fakeGit` plakt `out` in een `printf '%s' '…'`): de `.cmd`-versie moet `%`, `&`, `|`, `<`, `>` en `^` in de uitvoer escapen of de test slaat over.
- Een stub die zijn aanroepen logt (`NPM_LOG`): het logformaat (één regel per aanroep, argumenten gescheiden door spaties) moet op beide platforms hetzelfde zijn, anders slagen de asserts alleen op Linux.
- `t.Setenv("PATH", …)` zonder `t.Cleanup`: `t.Setenv` herstelt zelf; de helper mag dat niet dubbel doen en geen `t.Parallel` toelaten (de helper roept `t.Setenv` aan, dat paniekt in een parallelle test).

---

### Task 1: Pakket `internal/stubbin`

**Files:**
- Create: `internal/stubbin/stubbin.go`
- Create: `internal/stubbin/stubbin_unix.go` (`//go:build !windows`)
- Create: `internal/stubbin/stubbin_windows.go` (`//go:build windows`)
- Test: `internal/stubbin/stubbin_test.go`

**Interfaces:**
- Produces:
  - `type Spec struct { Stdout string; Stderr string; Exit int; LogFile string }`: wat een eenvoudige stub doet. `LogFile` (leeg = niet loggen) krijgt per aanroep één regel met de argumenten, gescheiden door één spatie.
  - `func Make(t *testing.T, dir, name string, s Spec) string`: schrijft een uitvoerbare stub `name` in `dir` (Windows: `name.cmd`), geeft het pad terug.
  - `func MakeScript(t *testing.T, dir, name, sh string) string`: schrijft het POSIX-script `sh` (zonder shebang; de helper zet `#!/bin/sh\n` ervoor) met mode `0o755`. Op Windows roept het `RequirePOSIX` aan.
  - `func RequirePOSIX(t *testing.T, reason string)`: `t.Skip` op Windows met `"stub imitates POSIX behaviour: " + reason`.
  - `func Prepend(t *testing.T, dir string)`: `t.Setenv("PATH", dir + os.PathListSeparator + os.Getenv("PATH"))`.
  - `func Only(t *testing.T, dir string, keep ...string)`: `t.Setenv("PATH", …)` met `dir` plus de directory van elke tool in `keep` die `exec.LookPath` vindt, gescheiden door `os.PathListSeparator`, zonder dubbelen; faalt de test als een `keep`-tool niet te vinden is.

- [ ] **Step 1: Schrijf de falende tests** in `stubbin_test.go`: `TestMakeRunsAndPrintsStdout` (Make met `Stdout: "hi"`, `Exit: 3`; `exec.LookPath(name)` na `Prepend`; `exec.Command(name).Output()` geeft `hi` en `ExitError.ExitCode() == 3`), `TestMakeLogsArguments` (twee aanroepen met `a b` en `c`; logbestand is `"a b\nc\n"`), `TestMakeEscapesSpecials` (`Stdout: "100% & <ok> | ^x"` komt letterlijk terug), `TestOnlyUsesSeparatorAndKeepsTools` (`Only(t, dir, "git")`: `os.Getenv("PATH")` bevat `string(os.PathListSeparator)`, begint met `dir`, en `exec.LookPath("git")` slaagt), `TestOnlyFailsWithoutTool` (via een `*testing.T`-dubbel is te zwaar: test de interne `lookDirs(keep []string) ([]string, error)` die een fout geeft voor `"no-such-tool-xyz"`).
- [ ] **Step 2: Draai ze, verwacht FAIL**: `go test ./internal/stubbin/ -run . -v` → "undefined: Make".
- [ ] **Step 3: Implementeer** de signaturen hierboven. Unix: shellscript met `printf '%s' '<stdout>'` (enkele aanhalingstekens geëscaped als `'\''`), `printf '%s' '<stderr>' >&2`, optionele logregel `echo "$*" >> LogFile`, `exit <n>`. Windows: `@echo off`, logregel `echo %*>>"<LogFile>"`, `echo`-regels met `^`-escaping van `% & | < > ^`, `exit /b <n>`. Interne `lookDirs` is gedeeld.
- [ ] **Step 4: Draai de tests, verwacht PASS** op Linux; `GOOS=windows go vet ./internal/stubbin/` is stil.
- [ ] **Step 5: Commit**: `git add internal/stubbin && git commit -m "test(stubbin): gedeelde teststub die op elk platform uitvoerbaar is (#566)"`.

### Task 2: Voorbeelden uit het issue (cli, gate, codelang)

**Files:**
- Modify: `internal/cli/roborev_fixture_test.go:113-121` (stubs `roborev`, `codex`; `PATH`)
- Modify: `internal/gate/node_test.go:100-103` (`fakeNpm`, `node`; `NPM_LOG`)
- Modify: `internal/codelang/hook_test.go:174-185` (`fakeGit`)
- Test: dezelfde bestanden, bestaande tests `TestRoborevGateFixtures`, `TestNodeRunsDriftThenScriptsInDir`, `TestHookGitFailures`

**Interfaces:**
- Consumes: `stubbin.Make`, `stubbin.MakeScript`, `stubbin.Prepend`, `stubbin.Only` uit Task 1.

- [ ] **Step 1: Run de drie tests als baseline**: `go test ./internal/cli/ -run TestRoborevGateFixtures ./internal/gate/ -run TestNodeRunsDriftThenScriptsInDir ./internal/codelang/ -run TestHookGitFailures -v` (drie aparte aanroepen) → PASS.
- [ ] **Step 2: Vervang** in `roborev_fixture_test.go` de `#!/bin/sh\n`-stubs door `stubbin.Make(t, bin, tool, stubbin.Spec{})` en `t.Setenv("PATH", bin+":/usr/bin:/bin")` door `stubbin.Only(t, bin, "git")`. In `codelang/hook_test.go` wordt `fakeGit` `stubbin.Make(t, bin, "git", stubbin.Spec{Stdout: out, Exit: code})` plus `stubbin.Only(t, bin)`. In `gate/node_test.go` is `fakeNpm` een echt POSIX-script (`NPM_EXIT_*`-variabelen, `case`): `stubbin.MakeScript` en `RequirePOSIX(t, "fakeNpm reads NPM_EXIT_<script> and branches on $1")` bovenaan de test.
- [ ] **Step 3: Draai de drie tests + de pakketten**: `go test -shuffle=on ./internal/cli/ ./internal/gate/ ./internal/codelang/` → PASS; `GOOS=windows go vet ./internal/cli/ ./internal/gate/ ./internal/codelang/` stil.
- [ ] **Step 4: Commit**: `git commit -m "test(cli,gate,codelang): stubs via stubbin, PATH met PathListSeparator (#566)"`.

### Task 3: Overige pakketten met stubs op het `PATH`

**Files:** (alle `_test.go`; per pakket één commit)
- `internal/hooks/` (`stubGit` maincommitgate_test.go:333, `fakeGit` mdlinkcheck_test.go:166, `fakeLighthouse` usagegate_test.go:361)
- `internal/review/`, `internal/roborev/`, `internal/usage/` (`fakeAnswer`/`fakeReader` codex_test.go:80, :166), `internal/pusher/` (`fakeTools` lint_test.go:55, review_test.go, pusher_test.go), `internal/initcmd/`
- `internal/evidence/run_test.go:39`, `internal/prbody/gate_test.go:52`, `internal/commitgate/roots_test.go:81`, `internal/canary/` (`stubPwsh`, `fakeShellcheck`, `fakeGolangciV1`), `internal/checkpointscan/` (`stubPath`, `fakeGitleaks`)
- `internal/cli/triage_judge_test.go:17` (`fakeClaude`) en de rest van `internal/cli`

**Interfaces:**
- Consumes: `stubbin.*` uit Task 1. Per stub kies je: `Make` als de stub alleen vaste uitvoer, exitcode en een argumentenlog nodig heeft; anders `MakeScript` met `RequirePOSIX` en een reden.

- [ ] **Step 1: Inventariseer** de echte lijst: `git grep -n '#!/bin/sh' -- '*_test.go' ':!e2e'` en `git grep -nE '":[/a-z]|\+":"|":"\+' -- '*_test.go'`; houd alleen de hits die een uitvoerbaar bestand op het `PATH` zetten. Schrijf de lijst (pakket, bestand, aantal) in het commitbericht van de eerste commit; het getal 153 uit #559 is een telling die we niet kennen (de scout vond 201 shebangs, waarvan niet alle op het `PATH`).
- [ ] **Step 2: Per pakket** vervang je de eigen stubhelper door `stubbin.*` (de eigen helper mag blijven als dunne wrapper rond `stubbin` zolang de aanroepers ongewijzigd blijven) en bouw het `PATH` met `Prepend`/`Only`. Stubs met `NPM_LOG`-achtig logformaat gebruiken `Spec.LogFile`.
- [ ] **Step 3: Draai per pakket** `go test -shuffle=on ./internal/<pkg>/` en `GOOS=windows go vet ./internal/<pkg>/`. PASS en stil.
- [ ] **Step 4: Commit per pakket**: `test(<pkg>): stubs via stubbin (#566)`.

### Task 4: e2e-tests

**Files:**
- Modify: `e2e/usage_test.go:27` (`writeScript`), `e2e/runsh_test.go:166` (`writeExec`), `e2e/shellcheck_test.go:112`, `e2e/hookkit_test.go:176` (`lighthouseStub`), `e2e/prepushscan_test.go`
- Test: `TestUsageRefreshE2E`, `TestUsageCodexE2E`, `TestUsageTickWithoutClaude`, `TestUsageKeepsControllerStateOut`, `TestServeUsageE2E`

**Interfaces:**
- Consumes: `stubbin.*`; de e2e-tests draaien met `-tags e2e` en bouwen de binary zelf.

- [ ] **Step 1: Baseline**: `go test -tags e2e ./e2e/ -run 'TestUsage|TestServeUsageE2E' -v` → PASS op Linux.
- [ ] **Step 2: Vervang** `writeScript`/`writeExec`/`lighthouseStub` door `stubbin.Make`/`MakeScript` en zet het `PATH` via `Prepend`/`Only`. Een e2e-stub die `claude`-uitvoer in JSON-regels nabootst met `printf` loopt via `Spec.Stdout`; de rest krijgt `RequirePOSIX`.
- [ ] **Step 3: Draai** `go test -shuffle=on -tags e2e ./e2e/` PASS; `GOOS=windows go vet -tags e2e ./e2e/` stil.
- [ ] **Step 4: Commit**: `git commit -m "test(e2e): stubs via stubbin (#566)"`.

### Task 5: Slot — bewaking, fragment en bewijs

**Files:**
- Create: `internal/stubbin/guard_test.go`
- Create: `changelog.d/issue-566-teststubs-windows.md`

- [ ] **Step 1: Schrijf `TestNoHardcodedUnixPathInTests`**: loopt over alle `*_test.go` onder de repo-root (via `go list -f '{{.Dir}}' ./...` of `filepath.WalkDir` vanaf `../..`) en faalt op `":/usr/bin"` en `":/bin"` als losse string; de lijst `allowed` bevat alleen bestanden waarvan de test echt een POSIX-`PATH` bedoelt, met reden. Verwacht FAIL zolang een hit overblijft, daarna PASS.
- [ ] **Step 2: Fragment** `changelog.d/issue-566-teststubs-windows.md`, front matter `type: changed`, tekst in het Engels: "Test stubs on the `PATH` now come from one shared helper, `internal/stubbin`, that is executable on every platform, and test helpers build the `PATH` with `os.PathListSeparator`; tests that imitate POSIX behaviour skip on Windows with a reason (#566)."
- [ ] **Step 3: Volledige run**: `go test -shuffle=on ./...`, `go test -tags e2e ./e2e/`, `GOOS=windows go vet ./...`, `GOOS=darwin go vet ./...`, `lighthouse imports check` → alles groen.
- [ ] **Step 4: Commit**: `git commit -m "test(stubbin): bewaking tegen vaste Unix-PATH en changelogfragment (#566)"`.
- [ ] **Step 5: Windows-bewijs.** De job `tests` van `windows-measure` draait niet op PR's (`windows-measure.yml:82`). Zet in de PR-body onder Evidence dat het AC "slagen op Windows in `windows-measure`" pas na `workflow_dispatch` op de branch of na de merge op `main` te bewijzen is, en voeg de run-URL toe zodra die er is; tot dan staat dat AC als open in de Evidence-tabel (`GOOS=windows go vet` bewijst alleen dat het compileert).
