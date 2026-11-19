# Canary per blokkerende regel — implementatieplan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Doel:** elke blokkerende regel van elke hook die lighthouse installeert heeft een canary: een minimale overtreding plus een test die exit 2 én de ID van die regel eist, met de lijst uit de hook zelf.

**Architectuur:** shell-hooks declareren één `BLOCKING_RULES="…"`-regel; de 7 core-hooks declareren per hook een lijst `hook.Reason` in Go en maken hun blokkerend resultaat alleen via `hook.Block`. Go- en shell-hook krijgen een vloercheck (de regels op de lijst staan effectief aan, anders blokkeert de hook luid met de ID). De canary-tests draaien de echte hook als subprocess: Python-hooks in pytest, de rest in Go (`internal/canary`, build tag `canary`, één CI-job).

**Tech stack:** Go (`internal/hook`, `internal/hooks`, `internal/commitgate`, `internal/codelang`, nieuw `internal/canary`), bash-hooks in `pack/`, pytest voor `pack/python`, golangci-lint 2.15.0, shellcheck (gepinde lib), `uv`/ruff/ty.

**Spec:** [2026-11-19-canary-per-regel-design.md](../specs/2026-11-19-canary-per-regel-design.md)

## Globale eisen

- Code, namen, commentaar en testdata Engels; `docs/`, changelog-fragment en commitberichten Nederlands (Conventional Commits).
- Een adopter ziet nooit minder blokkades dan vandaag: geen lint-config versoepelen. Ruff blijft een plafond (alleen de lijst), Go en shell een vloer (alles wat nu blokkeert, plus de lijst).
- `COULD NOT CHECK`-paden blijven zoals ze zijn en krijgen geen ID; hun bestaande tests blijven.
- Blokmelding draagt de ID als `[<hook>/<id>]`; de rest van de tekst blijft gelijk. Matchen per tool: ruff `: E722 `, shellcheck `SC2164 (`, golangci `(errcheck)`, de rest `[<hook>/<id>]`; geen losse substring, en de overtreding staat onder een neutrale naam (`canary.<ext>`) in een tijdelijke map zonder de ID in het pad.
- `--norc` is verboden bij shellcheck; `golangci-lint run --enable-only` is verboden.
- De Go-vloercheck vereist golangci-lint v2; op v1 meldt de hook `COULD NOT CHECK`, nooit stil.
- Blokmelding van go- en shell-vloer noemt `issues.new-from-rev` (go) resp. een `# shellcheck disable=… <reden>` (shell) als route voor schuld.
- Dogfood: een wijziging onder `pack/` draait `go run ./cmd/lighthouse init --upgrade` en commit de installed copy (`No changes.` in CI). Gereedschap blijft gepind (`.golangci-version`, shellcheck-pin; ruff via `.ruff-version` alleen als dat bestand bestaat: in deze repo ontbreekt het, dan draait de hook `uvx ruff` ongepind).
- Go-code volgt `/lighthouse-go-standard`, tests `/lighthouse-test-standard` (per test: de productiewijziging die hem rood maakt). Elke PR een fragment in `changelog.d/`.
- Testcommando's: `go test -race -shuffle=on ./...`, `go test -tags canary ./internal/canary/`, `uv run pytest -q tests/`.

## Review-focus

Invoerklassen die de spec impliceert maar die geen taak vanzelf raakt; elke regel krijgt een test in de genoemde taak.

1. **Config die de vloerregel stil weglaat zonder `disable:`** — `linters.default: none` + eigen lijst zonder `errcheck`, of `SHELLCHECK_OPTS=-e SC2164`: de hook blokkeert met de ID (taak 5 en 6).
2. **`.shellcheckrc` boven het script of in `$HOME`/XDG, en een range `disable=SC2000-SC3000`** — de vloercheck ziet het via het effectieve resultaat, niet via syntax (taak 6).
3. **Een hook-bestand zonder `BLOCKING_RULES`-regel, of met een lege/dubbele ID** — de test faalt in plaats van nul canaries te draaien (taak 3).
4. **Een tweede `Code: 2` in een core-hook** (nieuwe tak naast de helper) — de statische test faalt (taak 2).
5. **Een blokmelding met meerdere bevindingen** (`md-link-check` met een `dead` én een `absolute` link): elke regel draagt zijn eigen ID (taak 2).

---

## Bestandsstructuur

- Nieuw `internal/hook/reason.go` — `Reason`, `Declare`, `Reasons`, `Block`, `CouldNotCheck`.
- Nieuw `internal/hook/static_test.go` — de statische `Code: 2`-test.
- Wijzig `internal/hooks/*.go`, `internal/commitgate/{message,gate,pre}.go`, `internal/codelang/hook.go` — declaraties en helperaanroepen.
- Nieuw `internal/canary/` — `harness_test.go` (build, `init --yes` in een tijdelijke repo, subprocess-runner), `lists_test.go` (untagged: bijecties), `core_test.go`, `go_test.go`, `shell_test.go`, `bashwrite_test.go` (alle tagged `canary`), `testdata/<hook>/<id>.<ext>`.
- Wijzig `pack/{python,go,shell}/hooks/*.sh`, `pack/shell/scripts/shellcheck-repo.sh`.
- Nieuw `tests/pack/python/test_canary.py` + `tests/pack/python/testdata/<hook>/<id>.py`.
- Wijzig `.github/workflows/ci.yml` (job `canary`), verwijder opgegane pytest-tests, `changelog.d/`.

---

### Taak 1: `hook.Reason` en de twee helpers

**Files:**
- Create: `internal/hook/reason.go`
- Test: `internal/hook/reason_test.go`

**Interfaces:**
- Produces:
  - `type Reason struct{ Hook, ID string }` met `func (r Reason) Tag() string` (`"[<hook>/<id>]"`).
  - `func Declare(hookName string, ids ...string) []Reason` — registreert en geeft de lijst terug; paniekt op een lege of dubbele ID of op een tweede `Declare` voor dezelfde hook.
  - `func Reasons(hookName string) ([]string, bool)` — de IDs in declaratievolgorde; `false` als de hook niets declareerde. `func DeclaredHooks() []string` — gesorteerd.
  - `func Block(r Reason, stderr string) Result` — `Code: 2`, stderr krijgt `r.Tag()+" "` vóór de eerste regel. `func BlockTagged(stderr string, used ...Reason) Result` — voor een melding met een tag per bevindingsregel; paniekt als een `used`-tag niet in `stderr` staat.
  - `func CouldNotCheck(stderr string) Result` — `Code: 2`, stderr ongewijzigd (de eerste regel bevat al `COULD NOT CHECK`; paniekt als dat niet zo is).

- [ ] **Step 1: Tests** `TestBlockPrefixesTheTagOnTheFirstLine`, `TestBlockTaggedRequiresEveryTag`, `TestCouldNotCheckRejectsAMessageWithoutTheToken`, `TestDeclareRejectsDuplicates` (tabel: lege ID, dubbele ID, tweede `Declare` van dezelfde hook), `TestReasonsKeepDeclarationOrder`. Assert o.a. `Block(Reason{"h","x"}, "msg\nmore\n").Stderr == "[h/x] msg\nmore\n"`.
- [ ] **Step 2:** `go test ./internal/hook/ -run 'Block|Reason|Declare|CouldNot'` — verwacht FAIL (symbolen ontbreken).
- [ ] **Step 3: Implementeer** de signaturen hierboven in `internal/hook/reason.go`; registry achter dezelfde `mu` als `Register`.
- [ ] **Step 4:** zelfde commando — PASS.
- [ ] **Step 5: Commit** `feat(hook): gedeclareerde blokkeerredenen met ID (#381)`.

---

### Taak 2: de 7 core-hooks declareren redenen en gebruiken de helpers

**Files:**
- Modify: `internal/hooks/maincommitgate.go` (`blocked` en de blokkade op `:151`), `internal/hooks/committestgate.go`, `internal/hooks/prcreategate.go`, `internal/hooks/workflowpushgate.go`, `internal/hooks/mdlinkcheck.go`, `internal/hooks/bashwritecheck.go`, `internal/commitgate/{message,gate,pre}.go`, `internal/codelang/hook.go`
- Test: `internal/hook/static_test.go`, bestaande `_test.go` van die hooks (verwachte stderr krijgt de tag)

**Interfaces:**
- Consumes: taak 1.
- Produces — de gedeclareerde redenen (IDs vast, `var Reasons<Hook> = hook.Declare(…)` per pakket):

| Hook | IDs | Tak |
|---|---|---|
| `main-commit-gate` | `commit-to-main`, `push-to-main` | de blokkade na `reason := …` (per `inv.Action`) |
| `commit-test-gate` | `no-verify`, `tests-red` | `noVerifyBlock` (`commitgate/pre.go`), de `failLine`-tak in `commitgate/gate.go` |
| `pr-create-gate` | `pr-body` | `case code == 2` |
| `workflow-push-gate` | `workflow-path` | `checkPush`, tak `len(paths) > 0` |
| `bash-write-check` | geen eigen (lege lijst via `hook.Declare("bash-write-check")`) | de `unknownMode`-tak is `CouldNotCheck` |
| `md-link-check` | `dead`, `absolute` (de sleutels van `problemReasons`) | per bevindingsregel `[md-link-check/<kind>]`, `BlockTagged` |
| `code-language-check` | `non-english` | `case len(found) > 0` |

  `commitgate` krijgt een eigen paar `block(r hook.Reason, b *strings.Builder) Result` en `couldNotCheck(b *strings.Builder) Result` ter vervanging van `blockResult`; `hooks.blocked(stderr string)` wordt `hooks.couldNotCheck`/`hook.Block` per aanroep. `Declare` voor een hook zonder redenen (`bash-write-check`) is toegestaan met nul IDs.

- [ ] **Step 1: Statische test (rood).** `TestNoBareBlockCodeInCoreHooks` in `internal/hook/static_test.go`: parse met `go/parser` de niet-testbestanden van `internal/hooks`, `internal/commitgate`, `internal/codelang`; faal op elke `KeyValueExpr` met sleutel `Code` en waarde het letterlijke `2`, tenzij de omsluitende functie een van `hook.Block`, `hook.CouldNotCheck`, `commitgate.block`, `commitgate.couldNotCheck` is. De foutmelding noemt `bestand:regel`.
- [ ] **Step 2:** `go test ./internal/hook/ -run TestNoBareBlockCode` — verwacht FAIL met de huidige ~12 plekken.
- [ ] **Step 3:** Zet elke plek om volgens de tabel; geen meldingstekst wijzigt behalve de tag-prefix. `md-link-check`: de regel `"  " + pr.Target + …` wordt `"  " + reason.Tag() + " " + pr.Target + …`, het resultaat via `hook.BlockTagged`. Declareer in `init()` zodat `hook.Reasons` ook via een blank import werkt.
- [ ] **Step 4:** Pas de verwachte stderr in de bestaande tests aan; voeg `TestMdLinkCheckTagsEveryFinding` toe (bestand met één `dead` en één `absolute` link ⇒ beide tags in stderr, elk op de eigen regel).
- [ ] **Step 5:** `go test -race ./internal/hook/ ./internal/hooks/ ./internal/commitgate/ ./internal/codelang/ ./internal/bashwrites/` — PASS; `golangci-lint run ./...` schoon.
- [ ] **Step 6: Commit** `feat(hooks): blokkeerredenen van de core-hooks met ID en helper (#381)`.

---

### Taak 3: canary-harnas en de core-canaries

**Files:**
- Create: `internal/canary/harness_test.go`, `internal/canary/lists_test.go`, `internal/canary/core_test.go` (tag `canary`), `internal/canary/testdata/<hook>/<id>.json` (payload-sjablonen met `{{repo}}`, vullen via `fixturesetup.Fill`)
- Modify: `internal/hook` (alleen als `DeclaredHooks` iets mist)

**Interfaces:**
- Consumes: `hook.Reasons`, `hook.DeclaredHooks` (taak 1–2), `internal/fixturesetup` (`Build`, `Fill`, `Git`).
- Produces (voor taak 4–7, in package `canary`): 
  - `func lighthouseBin(t *testing.T) string` — bouwt één keer per testbinary (`sync.Once`) naar een tijdelijke map, zoals `e2e/e2e_test.go:buildLighthouse`.
  - `func installRepo(t *testing.T, packs ...string) string` — tijdelijke git-repo op `main`, `lighthouse init --yes --pack <p>…`, geeft de root terug.
  - `type run struct{ Code int; Stdout, Stderr string }` en `func runHook(t *testing.T, root, script string, payload string, env ...string) run` — draait `<root>/.claude/hooks/lighthouse/<script>` (of `run.sh <hook> [stand]`) met `payload` op stdin, `PATH` met `lighthouse` voorop, zonder `LIGHTHOUSE_*` uit de omgeving.
  - `func blockingRules(t *testing.T, hookFile string) []string` — leest de enige `BLOCKING_RULES="…"`-regel uit het hookbestand onder `pack/`; faalt bij nul of meer dan één regel, een lege of dubbele ID.
  - `func requireCanary(t *testing.T, r run, id string, marker string)` — eist `Code == 2` en `marker` in stderr.

- [ ] **Step 1: Bijectietests (untagged, rood)** in `lists_test.go`: `TestCoreReasonsMatchBlockingFile` (de sleutels van `hook.DeclaredHooks()` zijn exact de regels van `pack/core/hooks/blocking`, plus `code-language-check`-check dat de hooks in `blocking` allemaal declareren), `TestEveryReasonHasATestdataFile` en `TestEveryTestdataFileHasAReason` (per hook, `testdata/<hook>/<id>.*`; één generieke helper `assertBijection(t, hook string, ids []string)` die later ook voor de shell-hooks dient). Review-focus 3: `TestBlockingRulesRejectsMissingEmptyOrDuplicate` met drie tijdelijke hookbestanden.
- [ ] **Step 2:** `go test ./internal/canary/` — verwacht FAIL (geen testdata).
- [ ] **Step 3:** Schrijf harnas en per core-ID één fixture + één subtest in `core_test.go` (`go:build canary`), tabel `ID → setup`:

| ID | Hook-aanroep | Opzet van de repo |
|---|---|---|
| `commit-to-main` | `run.sh main-commit-gate` | repo op `main`; payload `git commit -m x` |
| `push-to-main` | `run.sh main-commit-gate` | repo op `main` met bare remote; payload `git push origin main` |
| `no-verify` | `run.sh commit-test-gate pre` | payload `git commit --no-verify -m x` |
| `tests-red` | `run.sh commit-test-gate pre` | `[test_gate] command = "false"`, commit op `main` |
| `pr-body` | `run.sh pr-create-gate` | `forge.repo` gezet; payload `gh pr create --title t --body "geen verwijzing"` |
| `workflow-path` | `run.sh workflow-push-gate` | branch met een wijziging onder `.github/workflows/`, bare remote; payload `git push -u origin HEAD` (zet `LIGHTHOUSE_CONTROLLER_RUN`, want de hook is alleen daar actief) |
| `dead`, `absolute` | `run.sh md-link-check` | markdown-bestand met één dode resp. absolute link; payload `{"tool_input":{"file_path":…}}` |
| `non-english` | `run.sh code-language-check` | Go- of Python-bestand met een Nederlandse commentaarregel die de deterministische laag vangt (kies de kortste die `internal/codelang` zonder LLM vangt) |

  Elke test eist `requireCanary(t, r, id, "["+hook+"/"+id+"]")` en dat het bestand met neutrale naam in een map zonder de ID in het pad staat.
- [ ] **Step 4:** `go test ./internal/canary/` PASS; `go test -tags canary -run Core ./internal/canary/` PASS.
- [ ] **Step 5: Commit** `test(canary): core-hooks, een canary per blokkeerreden (#381)`.

---

### Taak 4: Python-hooks — ruff-plafond en ty-gate

**Files:**
- Modify: `pack/python/hooks/ty-gate.sh`, `pack/python/hooks/ruff-format-check.sh` (alleen de commentaarverwijzing; `BLOCKING_RULES` blijft)
- Create: `tests/pack/python/test_canary.py`, `tests/pack/python/testdata/ruff-format-check/<code>.py` (8 bestanden), `tests/pack/python/testdata/ty-gate/{type-error,unresolved-imports-only}.py`
- Modify: `tests/pack/python/test_ruff_format.py` (de twee losse blokkeertests gaan op in de canary), `pack/python/scripts/quality/baseline.py` niet.

**Interfaces:**
- Produces: `ty-gate.sh` bevat `BLOCKING_RULES="type-error,unresolved-imports-only"`; de gewone-fouttak print `[ty-gate/type-error] …` als eerste regel, de alleen-imports-tak `[ty-gate/unresolved-imports-only] …`. Ruffmelding blijft de ruff-output (`: E722 `).

- [ ] **Step 1: Test** `test_every_ruff_rule_blocks` (parametrize over `BLOCKING_RULES` uit `ruff-format-check.sh`, gelezen met een regex op de enige regel; kopieert `testdata/ruff-format-check/<code>.py` naar `tmp_path/canary.py`; eist `returncode == 2` en `f": {code} "` in stderr), `test_ruff_rules_have_a_file_each` (bijectie), `test_every_ty_reason_blocks` (zelfde patroon; ty via echte `uvx ty@<TY_VERSION>` in een tijdelijke repo met `.venv`, `python.source_dirs`; hergebruik de repo-opzet uit `test_ty_gate.py`), `test_ty_reasons_have_a_file_each`, `test_baseline_py_lists_the_same_ruff_rules` (leest de 8 codes uit `baseline.py:18-25`; mag, spec §5).
- [ ] **Step 2:** `uv run pytest -q tests/pack/python/test_canary.py` — verwacht FAIL (testdata en `BLOCKING_RULES` in ty-gate ontbreken).
- [ ] **Step 3:** Voeg de `BLOCKING_RULES`-regel en de tags aan `ty-gate.sh` toe (alle andere exit-2-takken zijn `COULD NOT CHECK`); schrijf de fixtures (minimale overtredingen, één per code: `E722` bare `except:`, `B006` mutable default, `F403` `from x import *`, `F811` herdefinitie, `B023` closure in loop, `PT012` `pytest.raises` met meerdere statements, `PGH003` `# type: ignore`, `PGH004` `# noqa` zonder code; ty: `x: int = "a"`, en een import van een niet bestaande module met verder schone code).
- [ ] **Step 4:** pytest PASS. Verwijder de twee opgegane tests uit `test_ruff_format.py`.
- [ ] **Step 5: Commit** `feat(python-pack): canary per ruff-regel en ty-reden (#381)`.

---

### Taak 5: Go-pack — vloer, `parse-error`, `typecheck`

**Files:**
- Modify: `pack/go/hooks/go-lint-gate.sh`, `pack/go/hooks/gofmt-check.sh`
- Create: `internal/canary/go_test.go` (tag `canary`), `internal/canary/testdata/gofmt-check/parse-error.go`, `internal/canary/testdata/go-lint-gate/{errcheck,govet,ineffassign,staticcheck,unused,typecheck}.go`
- Modify: `tests/pack/go/test_gofmt.py` (de blokkeertest gaat op), `tests/pack/go/test_go_lint_gate.py` (`test_finding_blocks` gaat op)

**Interfaces:**
- Produces:
  - `gofmt-check.sh`: `BLOCKING_RULES="parse-error"`; de parse-takmelding wordt `[gofmt-check/parse-error] gofmt-check: <file> does not parse — …`.
  - `go-lint-gate.sh`: `BLOCKING_RULES="errcheck,govet,ineffassign,staticcheck,unused,typecheck"`. Vóór de lintrun in `check_root`: de effectieve set uit `golangci-lint linters --json | jq -r '.Enabled[].name'` (met de config van de repo, `--no-config` nooit); voor elke ID uit `BLOCKING_RULES` behalve `typecheck` die niet in die set staat: blokkeer (exit 2) met `[go-lint-gate/<id>] <id> is switched off in the golangci-lint configuration of <root>; …` en de `issues.new-from-rev`-route. Op golangci-lint v1 (`version --short` begint niet met `2`): `COULD NOT CHECK`. De lintrun zelf is ongewijzigd; een bevinding print voortaan `[go-lint-gate/<linter>]`-loos zoals golangci (`(errcheck)`), de test matcht daarop.
  - In de test: `func blockingRules` (taak 3) leest ook `go-lint-gate.sh`; de IDs behalve `typecheck` zijn de vloerlinters (de test leest dat onderscheid uit de hook: `typecheck` staat in een aparte regel `NON_LINTER_RULES="typecheck"` in het script, door `blockingRules`-hulpfunctie `nonLinterRules` gelezen).

- [ ] **Step 1: Tests.** `TestGoLintGateCanary` (subtest per ID: een tijdelijke module met `go.mod` en `.golangci.yml` `version: "2"` + `linters.default: standard`, overtreding als `canary.go`, Stop-payload + transcript zoals `tests/pack/go/test_go_lint_gate.py`; eist exit 2 en `(<id>)` resp. `(typecheck)` in stderr), `TestGoLintGateFloorHostileConfig` (subtest per vloer-ID behalve `typecheck`: de test schrijft een config `linters.default: standard` + `disable: [<id>]`, draait de hook op een **schone** module en eist exit 2 plus `[go-lint-gate/<id>]` — daarmee faalt hij als de vloercheck verdwijnt), `TestGoLintGateFloorKeepsStricterAdopterConfig` (config met extra linter `gocritic` aan ⇒ de hook blokkeert nog steeds op een `gocritic`-bevinding: geen plafond), `TestGoLintGateV1IsUnchecked` (fake `golangci-lint` op `PATH` met `version --short` = `1.64.8` ⇒ `COULD NOT CHECK`), `TestGofmtParseErrorCanary`, bijecties via `assertBijection`.
- [ ] **Step 2:** `go test -tags canary -run 'GoLint|Gofmt' ./internal/canary/` — verwacht FAIL.
- [ ] **Step 3:** Implementeer in de twee scripts; schrijf de fixtures. Probeer eerst met het ongewijzigde script om te zien dat `TestGoLintGateFloorHostileConfig` nu groen zou zijn zonder vloercheck (het script blokkeert niet; de test hoort rood).
- [ ] **Step 4:** Alle bovenstaande tests PASS; `go run ./cmd/lighthouse init --upgrade` ⇒ commit de installed copy.
- [ ] **Step 5: Commit** `feat(go-pack): vloer en canary per golangci-linter, parse-error (#381)`.

---

### Taak 6: Shell-pack — vloer, drempel-ID

**Files:**
- Modify: `pack/shell/hooks/shell-lint-check.sh`, `pack/shell/scripts/shellcheck-repo.sh`
- Create: `internal/canary/shell_test.go` (tag `canary`), `internal/canary/testdata/shell-lint-check/{SC2164,SC2046,SC2115,SC2155,SC2034,strict-style}.sh`
- Modify: `tests/pack/shell/test_shellcheck.py` (de blokkeer- en strict-tests die opgaan)

**Interfaces:**
- Produces:
  - `shell-lint-check.sh`: `BLOCKING_RULES="SC2164,SC2046,SC2115,SC2155,SC2034,strict-style"` (de SC-codes zijn de keuze van dit plan: allemaal severity `warning`; taak-test bewijst het met `shellcheck -f json`). `strict-style` is de drempel-ID, geen SC-code; hij heeft geen vloercheck.
  - `shellcheck-repo.sh --check-floor <code,code,…>`: draait in de huidige map `shellcheck -s bash -x -f gcc -` met een ingebouwd probe-script per code (`case`-tabel in het script) op stdin, in dezelfde omgeving (cwd, `SHELLCHECK_OPTS`, rc-zoekpad) als de echte run, **zonder** `--norc`; exit 0 als elke code verschijnt, exit 1 met de ontbrekende codes op stdout (één per regel), exit 4 als shellcheck ontbreekt.
  - Hook: vóór de echte run `cd "$FILE_DIR"` en `--check-floor "$BLOCKING_RULES zonder strict-style"`; exit 1 ⇒ blokkeer met `[shell-lint-check/<SC>] <SC> is switched off …` (één regel per ontbrekende code), exit ≠ 0,1 ⇒ `fail` (`COULD NOT CHECK`). `strict-style`: de bestaande drempel-melding krijgt `[shell-lint-check/strict-style]` als de run in `style` ging én alleen style-/info-bevindingen heeft; de test eist dit via de `SC2001`-canary in `.claude/hooks/` en een niet-blokkerende run van dezelfde file buiten een strict dir.

- [ ] **Step 1: Tests.** `TestShellLintCanary` (subtest per SC-ID: kopieer `testdata/…/<id>.sh` naar `<repo>/canary.sh` (niet in een strict dir), eis exit 2 en `"<id> ("` in stderr), `TestShellLintStrictStyleCanary` (zelfde `SC2001`-fixture: in `<repo>/.claude/hooks/canary.sh` exit 2 + `[shell-lint-check/strict-style]`; in `<repo>/canary.sh` exit 0), `TestShellFloorHostile` (tabel van kanalen × elke vloercode: `.shellcheckrc` in de repo-root met `disable=<code>`, een rc **boven** het script (in de ouder van de repo), `disable=SC2000-SC3000`, `SHELLCHECK_OPTS=-e <code>`, `XDG_CONFIG_HOME`-rc; elk ⇒ exit 2 en `[shell-lint-check/<code>]` op een **schoon** script — review-focus 2), `TestShellFloorKeepsAdopterEnable` (rc met `enable=…` extra ⇒ hook blokkeert nog op die check: `--norc` is verboden), `TestShellFloorCodesAreWarningOrError` (draait elke fixture met `-f json` en eist `level` ∈ {warning, error}), `TestShellFloorProbeFiresForEveryCode` (de `--check-floor` op een schone omgeving ⇒ exit 0), bijectie.
- [ ] **Step 2:** `go test -tags canary -run Shell ./internal/canary/` — verwacht FAIL.
- [ ] **Step 3:** Implementeer; de probe-snippets zijn de minimale fixtures zelf (korte `case`-tak per code); `--check-floor` en de hookwijziging; `strict-style`-tag.
- [ ] **Step 4:** PASS; `init --upgrade` + commit installed copy.
- [ ] **Step 5: Commit** `feat(shell-pack): vloer en canary per shellcheck-regel (#381)`.

---

### Taak 7: `bash-write-check` — een canary per poort

**Files:**
- Create: `internal/canary/bashwrite_test.go` (tag `canary`)
- Modify: `internal/bashwrites/gates.go` (alleen een geëxporteerde `func GateNames() []string` als de set uit `Gates` niet netjes te lezen is)
- Modify: `tests/pack/test_bash_write_check.py` (opgaande tests)

**Interfaces:**
- Consumes: `bashwrites.Gates`, `bashwrites.GateLabel`, taak 3 (`installRepo` met `go`, `python`, `shell`), de testdata van taak 3–6.

- [ ] **Step 1: Test** `TestBashWriteCheckCanaryPerGate`: de unieke gates uit `bashwrites.Gates` (waarden, uniek) ⇒ per gate een subtest; extensie = de sleutel waarin de gate voorkomt; overtreding = `testdata/<hook>/<eerste ID>.*` van die gate (hook = naam zonder `.sh`); de test draait `run.sh bash-write-check pre` en daarna `post` met een Bash-payload die het bestand (neutrale naam) schrijft, en eist exit 2 plus de marker van die poort (dezelfde als in zijn eigen canary). `TestBashWriteCheckGateSetHasNoRemainder`: elke gate uit `Gates` heeft testdata (bijectie), zodat een nieuwe poort zonder canary faalt.
- [ ] **Step 2:** `go test -tags canary -run BashWrite ./internal/canary/` — verwacht FAIL tot taak 3–6 zijn gemerged in de branch; dan PASS.
- [ ] **Step 3:** Implementeer de test (en `GateNames` alleen indien nodig).
- [ ] **Step 4:** PASS. Verwijder de pytest-tests die hierin opgaan.
- [ ] **Step 5: Commit** `test(canary): bash-write-check, een canary per poort (#381)`.

---

### Taak 8: CI-job, opgeruimde pytest, bewijs, fragment

**Files:**
- Modify: `.github/workflows/ci.yml` (nieuwe job `canary`; een wijziging onder `.github/workflows/` pusht de controller niet zelf: gebruik de handoff `next: push-workflows`)
- Delete: de pytest-tests die opgaan (zie stap 2)
- Create: `changelog.d/<naam>.md`; de PR-body noemt de verwijderde tests

- [ ] **Step 1: CI-job** `canary` (naast `go-e2e`): `actions/checkout`, `setup-go`, `golangci-lint-action` met `version: v2.15.0` (installeert alleen; draait `golangci-lint run` niet: `args: --version`), `astral-sh/setup-uv`, `go build -o /usr/local/bin/lighthouse ./cmd/lighthouse`, `go test -race -tags canary ./internal/canary/ -v`, en `uv run pytest -q tests/pack/python/test_canary.py`. Shellcheck komt via de gepinde lib en wordt door de test opgehaald; ontbreekt hij, dan faalt de job (de `pinned_shellcheck`-regel: nooit overslaan). Gebruik dezelfde actie-SHA's als de bestaande jobs. Een tool die ontbreekt ⇒ `t.Fatal`, geen `t.Skip` (grep-test `TestCanaryNeverSkips` in `lists_test.go`: geen `t.Skip` in `internal/canary/*.go`).
- [ ] **Step 2: Opgaande pytest** verwijderen, per test aangetoond met de canary die hetzelfde bewijst: `tests/pack/go/test_gofmt.py::test_blocks_a_file_that_does_not_parse`, `tests/pack/shell/test_shellcheck.py::test_shellcheck_hook_blocks_a_deliberately_broken_script`, `tests/pack/go/test_go_lint_gate.py::test_finding_blocks`, de blokkeer-case van `tests/pack/test_md_link_check.py`, de blokkeer-case(s) van `tests/pack/test_bash_write_check.py`, de twee uit `test_ruff_format.py` (taak 4). De overige tests (alle `COULD NOT CHECK`-, pin-, config- en alarmtests) blijven. Noteer de lijst voor de PR-body.
- [ ] **Step 3: AC 4-bewijs.** Haal lokaal `PGH004` uit `BLOCKING_RULES` van `ruff-format-check.sh`, draai `uv run pytest -q tests/pack/python/test_canary.py`, verwacht rood (bijectietest `test_ruff_rules_have_a_file_each`), bewaar de output voor de PR-body, herstel de regel. Herhaal met één ID uit `go-lint-gate.sh` en `go test -tags canary -run GoLint ./internal/canary/`.
- [ ] **Step 4: Fragment** in `changelog.d/` (kopieer het formaat van `changelog.d/fix-handoff-einde-zonder-pr.md`): de core-hook-meldingen dragen `[<hook>/<id>]`; go- en shell-hook blokkeren een config die een vloerregel uitzette, met `issues.new-from-rev` (go) of een gemotiveerde `# shellcheck disable` als route voor schuld.
- [ ] **Step 5:** `go run ./cmd/lighthouse init --upgrade` ⇒ `No changes.`; `go test -race -shuffle=on ./...` en `uv run pytest -q tests/` groen.
- [ ] **Step 6: Commit** `ci(canary): job voor de canary-tests, opgegane pytest weg (#381)`.

---

## Zelfreview (uitgevoerd)

- **Spec-dekking:** §1 lijst in hook (taak 4–6), §2 vloer Go/shell (taak 5–6), §3 core-ID's en helpers (taak 1–2), §4 tests, bijectie, vijandige config, `bash-write-check`, build tag + job, bestaande pytest (taak 3–8), §5 `baseline.py`-test (taak 4), randvoorwaarden (globale eisen, taak 8), AC 4 (taak 8 stap 3).
- **Open keuzes die de spec aan de bouwer liet, nu beslist:** SC-codes `SC2164,SC2046,SC2115,SC2155,SC2034`; vloer-afdwinging Go via `linters --json`, shell via `--check-floor`-probe op stdin; Go-vorm `hook.Reason`/`Block`/`CouldNotCheck`, tests in `internal/canary`; CI: nieuwe job `canary`.
- **Aandachtspunt voor de bouwer:** de exacte fixtures voor `non-english` en `workflow-path` zijn in taak 3 niet bij naam vastgelegd; blijft een van beide niet minimaal te maken, noteer de afwijking in de PR in plaats van de ID te schrappen.
