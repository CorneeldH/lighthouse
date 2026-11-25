# POSIX-paden in tests Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Tests gebruiken geen POSIX-literal meer als absoluut pad en zetten `USERPROFILE` samen met `HOME`, zodat ze op Windows slagen (issue #567, onderdeel van #559).

**Architecture:** Een klein niet-test-package `internal/testpath` met twee helpers, `Abs` en `SetHome`, en een skip-helper voor tests die expres een POSIX-pad toetsen. Daarna gaan de tests per package-groep over op die helpers; verwachtingen worden met `filepath.Join` opgebouwd in plaats van als literal. Productiecode verandert niet.

**Tech Stack:** Go, `testing`, `path/filepath`.

**Spec:** geen spec; het issue (#567) is de spec. De scout-uitkomst (IMPACT PACK) zit in de sessie: de lijst van bestanden hieronder komt daaruit en is een startpunt, geen volledige telling.

## Global Constraints

- Code, namen en commentaar in het Engels (`code_language = "en"`); commits, dit plan en artefacten in het Nederlands; het changelog-fragment in het Engels.
- Alleen testbestanden en het nieuwe package `internal/testpath` veranderen. Geen wijziging in productiecode: de code weigert een niet-absoluut pad terecht.
- Een relatief-pad-geval (`"rel"`, `"relative/state"`) blijft relatief; alleen de absolute invoer en verwachting gaan via `testpath.Abs`.
- Verwachtingen worden met `filepath.Join(testpath.Abs(...), ...)` gebouwd, nooit met een `"/h/…"`-literal.
- Voor elke commit: `lighthouse imports check` naast `[test_gate] command` (AGENTS.md, Lighthouse: go).
- Een test die naar een kale `HOME` kijkt zet `HOME` en `USERPROFILE` via `testpath.SetHome`; een test die al beide zet (`internal/levellog/levellog_test.go:99-105`) blijft zoals hij is.

## Review Focus

- `testpath.Abs()` zonder elementen: geeft de wortel (`/` of `C:\`), geen lege string.
- `testpath.Abs("a", "..", "b")`: geschoond zoals `filepath.Join` doet, nog steeds absoluut.
- Een test met `HOME` leeg (`""`) moet leeg blijven: `SetHome(t, "")` zet beide leeg, het "both empty"-geval van `usage.TestDefaultPaths` blijft dus een fout geven.
- Op Windows is `os.TempDir()` soms een korte 8.3-naam; de helper mag er niet op steunen dat het volume `C:` is, maar leidt het af.
- Een Windows-only skip mag Linux/macOS niet raken: de skip-helper doet daar niets.

---

### Task 1: Package `internal/testpath`

**Files:**
- Create: `internal/testpath/testpath.go`
- Test: `internal/testpath/testpath_test.go`

**Interfaces:**
- Consumes: niets.
- Produces:
  - `func Abs(elem ...string) string`: `filepath.Join` van de wortel van het platform en `elem`. Wortel = `filepath.VolumeName(os.TempDir()) + string(filepath.Separator)` (op Linux/macOS `/`, op Windows bv. `C:\`). Geen bestandssysteemtoegang, geen `t` nodig, dus bruikbaar in tabel-literals.
  - `func SetHome(t testing.TB, dir string)`: `t.Setenv("HOME", dir)` en `t.Setenv("USERPROFILE", dir)`.
  - `func SkipOnWindowsPOSIXAbs(t testing.TB)`: `t.Skip` met reden op Windows, anders niets. Reden letterlijk: `a POSIX path such as /abs is not absolute on Windows`.

- [ ] **Step 1: Schrijf de falende tests** in `internal/testpath/testpath_test.go`: `TestAbsIsAbsolute` (`filepath.IsAbs(Abs("h", "x"))` is waar), `TestAbsRootWithoutElements` (`Abs()` geeft de wortel, `filepath.IsAbs` waar, niet leeg), `TestAbsCleans` (`Abs("a", "..", "b") == Abs("b")`), `TestSetHomeSetsBoth` (na `SetHome(t, Abs("h"))` zijn `os.Getenv("HOME")` en `os.Getenv("USERPROFILE")` gelijk aan `Abs("h")` en `os.UserHomeDir()` geeft dat pad), `TestSetHomeEmptyClearsBoth` (`SetHome(t, "")` laat `os.Getenv` beide leeg geven).
- [ ] **Step 2: Draai ze, verwacht een compilatiefout** (`undefined: Abs`): `go test ./internal/testpath/`
- [ ] **Step 3: Implementeer** de drie functies in `internal/testpath/testpath.go` met de signaturen hierboven; package-commentaar in het Engels, met de reden dat het geen productiecode importeert en dus overal in een `_test.go` mag.
- [ ] **Step 4: Draai en verwacht PASS:** `go test -race ./internal/testpath/` en `lighthouse imports check` (kijk of het nieuwe package een regel nodig heeft; zo ja, in dezelfde commit).
- [ ] **Step 5: Commit**

```bash
git add internal/testpath
git commit -m "test(testpath): helpers voor absolute paden en HOME met USERPROFILE (#567)"
```

### Task 2: usage, usageguard, store, forgecache

**Files:**
- Modify: `internal/usage/state_test.go:9-40` (`TestDefaultPaths`), `internal/usage/codex_test.go`, `internal/usageguard/decide_test.go`, `internal/store/store_test.go` (`TestDefaultPath`, onder andere `absolute_xdg` rond regel 31), `internal/forgecache/forgecache_test.go`

**Interfaces:**
- Consumes: `testpath.Abs`, `testpath.SetHome` (Task 1).
- Produces: niets.

- [ ] **Step 1: Herschrijf `TestDefaultPaths`.** De tabel krijgt de absolute invoer en de verwachting uit `testpath.Abs`: `x := testpath.Abs("x")`, `h := testpath.Abs("h")`, enzovoort; verwachtingen als `filepath.Join(h, ".cache", "lighthouse")` en `filepath.Join(h, ".local", "state", "lighthouse", "controller", "usage-window.alarm")`. De tabel mag velden als functies of de verwachting in de loop berekenen. Geval `"relative xdg kept"` houdt `"rel"` en verwacht `filepath.Join("rel", "lighthouse")`. `t.Setenv("HOME", …)` wordt `testpath.SetHome`.
- [ ] **Step 2: Draai de test voor én na de wijziging** op Linux: `go test -race ./internal/usage/ -run TestDefaultPaths`. Verwacht PASS beide keren (Linux is groen); het bewijs voor Windows is stap 4.
- [ ] **Step 3: Zelfde omzetting** in `codex_test.go`, `decide_test.go`, `store_test.go` en `forgecache_test.go`: elk `"/…"`-literal dat als HOME, XDG-map of pad dat de code op `IsAbs` toetst wordt gebruikt, wordt `testpath.Abs(...)`; elk verwacht pad wordt met `filepath.Join` afgeleid.
- [ ] **Step 4: Controleer de Windows-compilatie en een nagebootste Windows-ketting:** `GOOS=windows go vet ./internal/usage/... ./internal/usageguard/... ./internal/store/... ./internal/forgecache/...` (geen compileerfout). Dat bewijst niet dat de test op Windows slaagt; dat doet alleen `windows-measure` (zie Task 7).
- [ ] **Step 5: Commit**

```bash
git add internal/usage internal/usageguard internal/store internal/forgecache
git commit -m "test(usage): absolute paden via testpath in usage, usageguard, store en forgecache (#567)"
```

### Task 3: workplace en kennforge

**Files:**
- Modify: `internal/workplace/{transcript_test.go,carrier_test.go,git_test.go}`, `internal/workplace/kennforge/{adapter_test.go,client_test.go,contract_test.go}`, `internal/workplace/local/contract_test.go`

**Interfaces:**
- Consumes: `testpath.Abs` (Task 1).
- Produces: niets.

- [ ] **Step 1: Vervang `"/abs/instr.md"` en `"/abs/mcp.json"`** (`carrier_test.go:20,34,35,64`, `kennforge/contract_test.go:60`, `kennforge/adapter_test.go:555`, `local/contract_test.go:46`) door een package-variabele of helper, bv. `instrFile := testpath.Abs("abs", "instr.md")`, en gebruik die ook in de verwachte argumentenlijst (`--append-system-prompt-file`). `kennforge.TestSpawnAccepted` (`adapter_test.go` rond regel 580) moet de fout `carrier: instructions file … is not absolute` niet meer geven.
- [ ] **Step 1b: Zet ook `"/state"`** om (de `stateDir` van elke `NewCarrier`-aanroep in `carrier_test.go:27,43,57,88,105,268,302,317`; `NewCarrier` weigert hem, `carrier.go:78`), samen met de verwachte `LIGHTHOUSE_STATE_DIR`, en `"/x"` in `git_test.go:203,206`. Maak er een grep over elk `"/…"`-literal in de genoemde bestanden van, niet alleen over de regels hierboven.
- [ ] **Step 2: Doorloop de 14 treffers in `kennforge/client_test.go`** en de 4 in `transcript_test.go`: per treffer beslissen of het een invoer is die de code op `IsAbs` toetst (omzetten) of een kale string-join (laten staan, zoals `/d/usage-window.json` in `usage/state_test.go:42-54`). Zet in het commitbericht welke je liet staan.
- [ ] **Step 3: Draai** `go test -race ./internal/workplace/...` (PASS) en `GOOS=windows go vet ./internal/workplace/...`.
- [ ] **Step 4: Commit**

```bash
git add internal/workplace
git commit -m "test(workplace): absolute paden via testpath in workplace en kennforge (#567)"
```

### Task 4: hostconfig, park, cli, repoconfig, nodeserve, dispatch, kpi, codelang

**Files:**
- Modify: `internal/hostconfig/hostconfig_test.go`, `internal/park/{pause_test.go,marker_test.go}`, `internal/cli/{tick_statedir_test.go,pushworkflows_test.go,hook_test.go}`, `internal/dispatch/apply_test.go` (`TestRelocate`), `internal/kpi/{startissue_test.go,fixpr_test.go}`, `internal/codelang/llm_test.go`, `internal/repoconfig/repoconfig_test.go` (o.a. regel 496), en de nodeserve-tests

**Interfaces:**
- Consumes: `testpath.Abs`, `testpath.SetHome` (Task 1).
- Produces: niets.

- [ ] **Step 1: Omzetten volgens de regel van Task 2**: invoer en verwachting via `testpath.Abs` en `filepath.Join`.
- [ ] **Step 2: `repoconfig` en `nodeserve`** vonden geen treffer in de eenvoudige scan; zoek ze op met `grep -rn '"/' internal/repoconfig internal/nodeserve --include=*_test.go` en met de lijst uit het issue (de Windows-uitvoer van #559 staat in de run uit het issue). De rij `absolute dir` (`repoconfig_test.go:496`, `report.dir = "/abs"`) hoort bij Task 6: op Windows is `/abs` niet absoluut en geeft de code geen fout, dus die rij krijgt daar een skip.
- [ ] **Step 3: `dispatch.TestRelocate`**: lees de test, vervang de POSIX-wortel door `testpath.Abs` en bouw de verwachting met `filepath.Join`.
- [ ] **Step 4: Draai** `go test -race` op deze packages en `GOOS=windows go vet` erop.
- [ ] **Step 5: Commit** (één commit per package-groep mag, liever per package als de diff groot is):

```bash
git add internal/hostconfig internal/park internal/cli internal/dispatch internal/kpi internal/codelang internal/repoconfig internal/nodeserve
git commit -m "test(cli): absolute paden via testpath in de overige packages (#567)"
```

### Task 5: HOME zonder USERPROFILE

**Files:**
- Modify: elk `_test.go` dat `t.Setenv("HOME", …)` zet zonder `USERPROFILE`. Vind ze met `grep -rn 't.Setenv("HOME"' internal tools --include=*_test.go`. Het scoutrapport telde 47 treffers in 30 bestanden, waaronder `datadir/datadir_test.go`, `agentaudit/{sources_test.go,testtree_test.go}`, `hostconfig/hostconfig_test.go`, `usage/{codex_test.go,state_test.go}`, `store/store_test.go`, `kpi/startissue_test.go`, ongeveer vijftien `cli/*_test.go`, `pusher/review_test.go`, `review/*`, `hooks/*`, `roborev/budget_test.go` en `tools/check_cla/main_test.go`.

**Interfaces:**
- Consumes: `testpath.SetHome` (Task 1).
- Produces: niets.

- [ ] **Step 1: Beslis per treffer.** Alleen een test waarvan de code onder test `os.UserHomeDir` (direct of via een helper) leest, of die op Windows faalt zonder `USERPROFILE`, gaat over op `testpath.SetHome(t, dir)`. Een test die `HOME` zet om een git- of shellproces te sturen, niet de Go-code, laat je staan; noteer dat in het commitbericht.
- [ ] **Step 2: Zet de benoemde gevallen** `agentaudit.TestClaudeJSONHonoursConfigDir`, `kpi.TestTranscriptDirFromRepoRoot`, de `datadir`-tests en de `hostconfig`-tests zeker om.
- [ ] **Step 3: Controleer dat niets `USERPROFILE` dubbel zet** (`grep -rn USERPROFILE internal`): `levellog_test.go:102` blijft staan.
- [ ] **Step 4: Draai** `go test -race ./...` (lokaal, Linux) en `GOOS=windows go vet ./...`.
- [ ] **Step 5: Commit**

```bash
git add -A internal tools
git commit -m "test: HOME gaat samen met USERPROFILE via testpath.SetHome (#567)"
```

### Task 6: Test die `/abs` als absoluut toetst slaat op Windows over

**Files:**
- Modify: de test die expres controleert dat een POSIX-pad absoluut geldt. Kandidaat: de rij `absolute dir` in `internal/repoconfig/repoconfig_test.go:496` (`dir = "/abs"` moet een `report.dir`-fout geven, maar `filepath.IsAbs("/abs")` is op Windows onwaar, `internal/repoconfig/repoconfig.go:490`, dus daar wordt het pad geaccepteerd). `config.TestPyPath` (`internal/config/config_test.go:253`) is GEEN kandidaat: `pyPath` is zuivere `/`-stringverwerking en slaagt op Windows. Zoek verder met `grep -rn 'IsAbs' internal --include=*_test.go`.

**Interfaces:**
- Consumes: `testpath.SkipOnWindowsPOSIXAbs` (Task 1).
- Produces: niets.

- [ ] **Step 1: Bepaal** welke tests de eigenschap "een POSIX-pad is absoluut" zelf toetsen (de uitkomst hangt van `/abs` af, niet alleen de invoer). Die tests, en alleen die, krijgen `testpath.SkipOnWindowsPOSIXAbs(t)` als eerste regel (of in het subtest-geval op de rij). Het windows-schone gedeelte van een test houdt zijn eigen dekking.
- [ ] **Step 2: Draai op Linux** `go test -race ./internal/config/...` (PASS, de skip is een no-op) en `GOOS=windows go vet ./internal/config/...`.
- [ ] **Step 3: Commit**

```bash
git add internal
git commit -m "test: tests van POSIX-paden slaan op Windows over met reden (#567)"
```

### Task 7: Changelog en eindcontrole

**Files:**
- Create: `changelog.d/issue-567-posix-paden-als-absoluut-pad-in-tests-en.md` (de branchnaam, zie `docs/releasing.md`)

- [ ] **Step 1: Schrijf het fragment** in het Engels, met `type: internal` (`docs/releasing.md`: tests starten geen release), in de vorm van `changelog.d/issue-91-lighthouse-uninstall.md` (front matter `---\ntype: …\n---`, één alinea die eindigt op `(#567)`). Inhoud: tests use one helper for absolute paths and set `USERPROFILE` with `HOME`, so they pass on Windows; no change to shipped behavior.
- [ ] **Step 2: Volledige controle op Linux:** `go build ./... && go vet ./... && go test -race ./...` en `lighthouse imports check`; verwacht alles groen.
- [ ] **Step 3: Cross-compile:** `GOOS=windows go vet ./...` en `GOOS=darwin go vet ./...`.
- [ ] **Step 4: Leg vast wat dit niet bewijst.** `windows-measure.yml` draait de Windows-tests (`go test -shuffle=on ./...`, job `tests`) alleen bij een push naar `main` of `workflow_dispatch`, niet op PR's, en de stappen zijn `continue-on-error`. De laatste AC ("slagen op Windows in `windows-measure`") is dus pas na de merge te lezen: in de PR-body onder bewijs benoemen als *na merge te controleren* met het commando `gh workflow run windows-measure.yml --ref <branch>` (zonder `--ref` meet het `main`; de job `tests` draait op elke ref) en het artefact `go-test.txt` van `windows-measure-tests`. Wacht er niet op en poll niet: dispatch na de push en noteer het in de PR-body, niet als bewezen.
- [ ] **Step 5: Commit**

```bash
git add changelog.d/issue-567-posix-paden-als-absoluut-pad-in-tests-en.md
git commit -m "docs(changelog): fragment voor #567"
```
