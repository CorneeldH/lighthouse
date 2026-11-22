# Codex-meter leest CODEX_HOME — implementatieplan

> **Voor agents:** VERPLICHTE SUB-SKILL: gebruik superpowers:subagent-driven-development (aanbevolen) of superpowers:executing-plans om dit plan taak voor taak uit te voeren. Stappen gebruiken checkboxen (`- [ ]`).

**Doel:** een gezette `CODEX_HOME` bepaalt welk Codex-account de meter, de roborev-gate en `review-run` gebruiken, zodat een run een eigen codex-home kan hebben.

**Architectuur:** `internal/usage` krijgt één functie `CodexHome()` die `$CODEX_HOME` teruggeeft, of anders `$HOME/.codex`. `NewCodexReader` meet met een gezette `CODEX_HOME` alléén die home; zonder blijft de lijst `$HOME/.codex`, `$HOME/.codex-specreview`. `internal/review/runner.go` haalt `auth.json` voortaan uit `CodexHome()`. De roborev-gate leest het budget al via `usage.ReadCodexStrict` en volgt dus vanzelf.

**Tech stack:** Go, `testing`, `t.Setenv`, een nep-`codex` als `/bin/sh`-script op `PATH`.

**Spec:** issue #477 (geen apart specdocument; de acceptatiecriteria staan in het issue).

## Globale randvoorwaarden

- Code, commentaar en testnamen in het Engels; docs, changelog en PR in het Nederlands (`[project] language = nl`).
- Zonder `CODEX_HOME` (ongezet of leeg) verandert er niets: `Homes` = `$HOME/.codex`, dan `$HOME/.codex-specreview`; de foutmelding `CODEX_UNAUTHENTICATED: ~/.codex/auth.json missing` blijft letterlijk.
- Een lege `CODEX_HOME` telt als ongezet.
- Voor elke commit: `[test_gate] command` uit `lighthouse.toml` en `lighthouse imports check`.
- Geen wijziging onder `pack/`: geen `init --upgrade` nodig.

## Keuzes (gaan mee naar de PR-body)

- **In plaats van, niet vóór.** Met `CODEX_HOME` meet de reader alleen die home. `codex` zelf gebruikt dan ook alleen die home; een terugval op `$HOME/.codex` zou een ander account meten dan het account waarmee de review draait, en zou het gedeelde bestand uit de container weer de bron maken die #477 wil kwijtraken.
- **`internal/review/runner.go`: ja.** De bron van `auth.json` wordt `CodexHome()`. Zonder die keuze faalt `review-run` met `CODEX_UNAUTHENTICATED` in een container met alleen `CODEX_HOME`. De schone home (`CODEX_SPECREVIEW_HOME`, anders `~/.codex-specreview`) blijft wat hij is: die moet schoon blijven.
- **`internal/cli/planreviewrecord.go`: nee.** Hij legt alleen de schone review-home vast (`CODEX_SPECREVIEW_HOME`, anders `~/.codex-specreview`) en leest nergens `~/.codex`.
- **roborev zelf:** de `roborev`-binary staat buiten deze repo en start `codex` met de geërfde omgeving; `codex` leest `CODEX_HOME` zelf. Budget en review meten zo hetzelfde account zolang de run `CODEX_HOME` exporteert.

## Review-focus

1. `CODEX_HOME` gezet en `HOME` leeg: de reader moet werken, niet falen op `HOME is not set`. → test in taak 1.
2. `CODEX_HOME` gezet, de app-server faalt: de terugval op het sessielog leest dan `<CODEX_HOME>/sessions/**`, niet `$HOME/.codex/sessions`. → volgt uit `Homes`; test in taak 1 dekt `Homes`.
3. Een `CODEX_HOME` uit de shell van de ontwikkelaar lekt in bestaande tests die `HOME` zetten. → taak 1 en 3 zetten `CODEX_HOME` leeg in die tests.
4. `CODEX_SPECREVIEW_HOME` gelijk aan `CODEX_HOME`: `copyFile` met bron = doel mag `auth.json` niet afkappen. → test in taak 3.
5. `CODEX_HOME` gezet maar zonder `auth.json`: de melding noemt het echte pad, niet `~/.codex`. → test in taak 3.

---

### Taak 1: `usage.CodexHome` en `NewCodexReader` lezen `CODEX_HOME`

**Bestanden:**
- Wijzig: `internal/usage/codex.go:51-64`
- Test: `internal/usage/codex_test.go:324-343`
- Wijzig: `docs/usage-venster.md:112-113`

**Interfaces:**
- Produceert: `func CodexHome() (string, error)` in `internal/usage/codex.go` — `$CODEX_HOME` als die niet leeg is, anders `filepath.Join($HOME, ".codex")`; fout `HOME is not set` als beide leeg zijn.
- Produceert: `NewCodexReader()` met `Homes = []string{$CODEX_HOME}` als `CODEX_HOME` niet leeg is (ook zonder `HOME`), anders ongewijzigd.

- [ ] **Stap 1: Schrijf de falende tests** in `internal/usage/codex_test.go`.
  - `TestNewCodexReaderNoHome` en `TestNewCodexReaderHomes`: voeg `t.Setenv("CODEX_HOME", "")` toe; verder ongewijzigd.
  - `TestNewCodexReaderCodexHome`: `HOME=/h`, `CODEX_HOME=/run/codex` → `Homes` is precies `[]string{"/run/codex"}`, `Timeout` 20 s.
  - `TestNewCodexReaderCodexHomeWithoutHome`: `HOME=""`, `CODEX_HOME=/run/codex` → geen fout, `Homes` is `[]string{"/run/codex"}`.
  - `TestCodexHome`: tabel met `{home, codexHome, want, wantErr}`: `{"/h", "", "/h/.codex", false}`, `{"/h", "/run/codex", "/run/codex", false}`, `{"", "/run/codex", "/run/codex", false}`, `{"", "", "", true}`.
  - Zet in elke andere test in het pakket die `HOME` zet en `NewCodexReader`, `ReadCodex` of `ReadCodexStrict` raakt ook `t.Setenv("CODEX_HOME", "")` (zoek met `grep -n 'Setenv("HOME"' internal/usage/*_test.go`).

- [ ] **Stap 2: Draai ze en zie ze falen**

  Run: `go test ./internal/usage/ -run 'TestNewCodexReader|TestCodexHome' -v`
  Verwacht: FAIL — `undefined: CodexHome` (compileerfout).

- [ ] **Stap 3: Implementeer `CodexHome()` en pas `NewCodexReader()` aan** in `internal/usage/codex.go`. `NewCodexReader` kijkt eerst naar `os.Getenv("CODEX_HOME")`; leeg → het huidige pad met de `HOME`-check. Werk het doc-commentaar bij: noem `CODEX_HOME` en dat die dan de enige home is.

- [ ] **Stap 4: Draai de pakkettests**

  Run: `go test -race ./internal/usage/`
  Verwacht: PASS.

- [ ] **Stap 5: Docs.** Vervang in `docs/usage-venster.md` de bullet **Homes.** door: met een gezette `CODEX_HOME` alleen die home (ook zonder `HOME`); anders eerst `$HOME/.codex`, dan `$HOME/.codex-specreview`; de eerste home met een leesbare stand wint; zonder `CODEX_HOME` en zonder `HOME` is Codex niet meetbaar.

- [ ] **Stap 6: Commit**

```bash
git add internal/usage/codex.go internal/usage/codex_test.go docs/usage-venster.md
git commit -m "feat(usage): codex-meter leest een gezette CODEX_HOME (#477)"
```

### Taak 2: roborev-gate met alleen `CODEX_HOME` geeft `decision: codex`

**Bestanden:**
- Test: `internal/roborev/budget_test.go`

**Interfaces:**
- Consumeert: `NewCodexReader` uit taak 1 (via `DefaultBudget()` → `usage.ReadCodexStrict`).

- [ ] **Stap 1: Schrijf de test** `TestDefaultBudgetReadsCodexHome` in `internal/roborev/budget_test.go`.
  - Roep `budgetEnv(t)` aan (die zet `HOME` op een niet-bestaande map, `CODEX_HOME` op `<dir>/codex-home` en `PATH` op `<dir>`); maak `<dir>/codex-home` aan. `<dir>` haal je uit `os.Getenv("PATH")`.
  - Schrijf `<dir>/codex` (mode `0o755`), een `#!/bin/sh`-script zonder externe commando's (alleen builtins: `PATH` bevat niets anders):
    - `[ "$CODEX_HOME" = "<dir>/codex-home" ] || exit 1` — zo faalt de test als de reader `CODEX_HOME` weer negeert.
    - Print `{"id":1,"result":{}}` en daarna één regel `{"id":2,"result":<R>}`, dan `while read -r l; do :; done` om stdin leeg te lezen.
    - `<R>` = `{"rateLimits":{"limitId":"codex","primary":{"usedPercent":10,"windowDurationMins":300,"resetsAt":4102444800},"secondary":{"usedPercent":1,"windowDurationMins":10080,"resetsAt":4102444800}},"rateLimitResetCredits":{"availableCount":0,"credits":[]}}`. Weigert `ReadStrict` deze `rateLimitResetCredits`, neem dan het blok uit `internal/usage/testdata/codex/app_server_response.json` over.
  - Assert: `code == 0`, `decision == "codex"`.

- [ ] **Stap 2: Draai de test**

  Run: `go test -race ./internal/roborev/ -run TestDefaultBudget -v`
  Verwacht: PASS (taak 1 is al binnen).

- [ ] **Stap 3: Mutatiecheck.** Zet in `NewCodexReader` tijdelijk de `CODEX_HOME`-tak uit en draai stap 2 opnieuw.
  Verwacht: `TestDefaultBudgetReadsCodexHome` FAIL met `decision "not_measurable"`. Draai de mutatie terug (`git diff` leeg voor `internal/usage/`).

- [ ] **Stap 4: Commit**

```bash
git add internal/roborev/budget_test.go
git commit -m "test(roborev): gate meet het budget in CODEX_HOME (#477)"
```

### Taak 3: `review-run` haalt `auth.json` uit `CodexHome()`

**Bestanden:**
- Wijzig: `internal/review/runner.go:112-128`
- Test: `internal/review/runner_test.go` (rond `:40-51` en `:194-197`)

**Interfaces:**
- Consumeert: `usage.CodexHome() (string, error)` uit taak 1.

- [ ] **Stap 1: Schrijf de falende tests** in `internal/review/runner_test.go`.
  - Zet `t.Setenv("CODEX_HOME", "")` in de bestaande helper of tests die `~/.codex/auth.json` klaarzetten, en in de test die `CODEX_UNAUTHENTICATED: ~/.codex/auth.json missing` vastpint (die melding blijft letterlijk).
  - `TestRunCopiesAuthFromCodexHome`: `HOME` met géén `.codex`, `CODEX_HOME=<tmp>/run-home` met `auth.json` inhoud `"run-token"`, `CODEX_SPECREVIEW_HOME=<tmp>/clean` → de run komt voorbij de auth-stap en `<tmp>/clean/auth.json` bevat `"run-token"`.
  - `TestRunCodexHomeWithoutAuth`: `CODEX_HOME=<tmp>/run-home` zonder `auth.json` → exitcode `ExitNoLogin`, melding `CODEX_UNAUTHENTICATED: <tmp>/run-home/auth.json missing`.
  - `TestRunCodexHomeIsCleanHome`: `CODEX_HOME` en `CODEX_SPECREVIEW_HOME` zelfde map met `auth.json` `"run-token"` → na de auth-stap bevat `auth.json` nog steeds `"run-token"` (niet afgekapt).
  Volg de opzet van de bestaande runner-tests voor hoe ze de run na de auth-stap laten stoppen of slagen.

- [ ] **Stap 2: Draai ze en zie ze falen**

  Run: `go test ./internal/review/ -run 'TestRunCopiesAuthFromCodexHome|TestRunCodexHome' -v`
  Verwacht: FAIL — `CODEX_UNAUTHENTICATED: ~/.codex/auth.json missing` in plaats van een geslaagde auth-stap.

- [ ] **Stap 3: Implementeer** in `internal/review/runner.go`: `auth` = `filepath.Join(usage.CodexHome(), "auth.json")` (fout van `CodexHome` → `ExitNoLogin` met dezelfde melding als nu). Melding: met gezette `CODEX_HOME` `CODEX_UNAUTHENTICATED: <pad> missing`, anders letterlijk de huidige. Sla `copyFile` over als bron en doel na `filepath.Clean` gelijk zijn. Pas het commentaar boven het blok aan.

- [ ] **Stap 4: Draai de tests**

  Run: `go test -race ./internal/review/ ./internal/cli/`
  Verwacht: PASS (ook `internal/cli/reviewrun_test.go`; zet daar `CODEX_HOME` leeg als een test `~/.codex/auth.json` klaarzet en faalt).

- [ ] **Stap 5: Commit**

```bash
git add internal/review/runner.go internal/review/runner_test.go
git commit -m "feat(review): review-run haalt auth.json uit CODEX_HOME (#477)"
```

### Taak 4: changelog-fragment en eindcontrole

**Bestanden:**
- Maak: `changelog.d/issue-477-codex-home.md`

- [ ] **Stap 1: Schrijf het fragment**

```markdown
---
type: added
---
Een run kan zijn eigen `CODEX_HOME` krijgen: is die gezet, dan meten `lighthouse codex-usage`, de roborev-gate en `lighthouse usage` alleen die home, en haalt `review-run` zijn `auth.json` daar vandaan. Zonder `CODEX_HOME` blijft alles bij `~/.codex` en `~/.codex-specreview`.
```

- [ ] **Stap 2: Volledige gate**

  Run: het `[test_gate] command` uit `lighthouse.toml` en `lighthouse imports check`.
  Verwacht: beide exit 0.

- [ ] **Stap 3: Commit**

```bash
git add changelog.d/issue-477-codex-home.md
git commit -m "docs(changelog): fragment voor CODEX_HOME (#477)"
```
