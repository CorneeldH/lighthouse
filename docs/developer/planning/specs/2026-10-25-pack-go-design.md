# Ontwerp: taalpakket `pack/go`

Issue: #40 · Datum: 2026-10-25 · Vervolg op [het ontwerp van #22](2026-10-24-testskill-design.md),
sectie *Vervolg*.

## Aanleiding en doel

Geen incident. Lighthouse is een Go-repo, maar alleen Python heeft een schrijfskill
(`lighthouse-python-writing`). Het idee komt uit die skill en uit wat de skills van kenn-io laten
zien. De gebruiker schrijft zelf geen Go: **alleen agents programmeren Go**, en lighthouse is de
eerste en belangrijkste repo waarin dat gebeurt.

Het doel: een agent die Go schrijft, weet vóór het eerste bestand hoe hij test en meet, krijgt
na elke edit automatisch `gofmt`, en meldt "klaar" pas met een meting per KPI-rij, inclusief de
omgeving waarin hij mat.

## Scope

#40 levert:

1. een nieuw pakket `pack/go`: skill (beide harnasvarianten), detectie, instructieblok, hooks en
   een `requires.toml`;
2. een Bash-write-gate voor `.go` in `pack/core/lib/bash_writes.py`;
3. een verwijzing naar `lighthouse-go-writing` in de description van `lighthouse-testing`;
4. contracttests, en het pakket geïnstalleerd in lighthouse zelf (dogfood), met een
   `.golangci-version` en `-shuffle=on` in lighthouse's eigen test-gate.

**Buiten scope:**

- `pack/shell/skills/shell-gates` aansluiten op `lighthouse-testing` (een eigen vervolgissue uit
  #22).
- Een `quality`-ratchet voor Go, zoals `pack/python/scripts/quality/`.
- De mergeregel voor fragmenten in `internal/initcmd` aanpassen (zie *Geen fragment*).
- Lighthouse's eigen Go-code of `.golangci.yml` wijzigen.

## Non-goals en anti-goals

- **Geen** Go-inhoud in `lighthouse-testing`: die blijft taalneutraal (de tests pinnen dat).
- **Geen** `.golangci.blocking.yml`. Het concept wilde die voor repo's met oude lintschuld.
  Lighthouse staat op 0 issues, dus `.golangci.yml` zelf is de blokkerende set. Een repo met
  schuld gebruikt `--new-from-rev`; dat noemt de skill als valkuil, niet als KPI-rij.
- **Geen** `.claude/rules/go.md`. Een pakket kan geen rules-bestand installeren
  (`internal/packs/packs.go` kent geen `rules/`). De normen per bestand staan in de skill zelf; een
  project dat er toch een wil, noemt de skill met "if present", net als python-writing.
- **Geen** cross-OS-vet- en `go generate`-rij: lighthouse heeft geen cgo en geen
  `//go:generate`. Beide komen als valkuil in de tekst.
- **Anti-goal:** een hook die per edit lint en de agent beurten laat verbranden op een
  halverwege-toestand die nog niet compileert.
- **Anti-goal:** een hook die stil groen geeft als `go` of `golangci-lint` ontbreekt.

## Het pakket

Indeling naar het model van `pack/python` ([`internal/packs/packs.go`](../../../../internal/packs/packs.go)):

| Bestand | Inhoud |
|---|---|
| `pack/go/detect.toml` | `files = ["go.mod"]`: een door git getrackte `go.mod` in de repo-root. Een `go.mod` in een submap telt niet. |
| `pack/go/skills/go-writing/claude/SKILL.md` | de skill, `name: lighthouse-go-writing` |
| `pack/go/skills/go-writing/agents/SKILL.md` | dezelfde skill met de OpenCode-alinea's, zoals python-writing |
| `pack/go/instructions/AGENTS.md` | het blok `## Lighthouse: go` |
| `pack/go/hooks/hooks.json` | `gofmt.sh` op PostToolUse `Edit\|Write`, `go-check.sh` op `Stop` |
| `pack/go/hooks/gofmt.sh` | zie *Hooks* |
| `pack/go/hooks/go-check.sh` | zie *Hooks* |
| `pack/go/requires.toml` | `go`, `gofmt`, `golangci-lint`, `jq`, `git` → de units die ze nodig hebben |

Elk nieuw bestand krijgt een `new = true`-regel in [`pack/SOURCES.toml`](../../../../pack/SOURCES.toml).

### Geen fragment

`pack/go` levert **geen** `lighthouse.toml.fragment`. Reden: `mergeTables` in
[`internal/initcmd/initcmd.go`](../../../../internal/initcmd/initcmd.go) weigert een sleutel die
uit twee pakketten komt. Lighthouse heeft zowel `pyproject.toml` als `go.mod`; een
`[test_gate] command` in beide pakketten maakt `init` in zo'n repo onmogelijk. Een Go-repo zet
`[test_gate] command` zelf; de skill zegt hoe (`go test -shuffle=on ./...`). De mergeregel
verruimen is een eigen issue, als er ooit een tweede taalrepo om vraagt.

### Instructieblok

Kort, zoals dat van python:

```markdown
## Lighthouse: go

- Go code follows `/lighthouse-go-writing`.
- Two gates check it: `gofmt` after every edit (it formats, and blocks only on a file that does not parse), `go-check` when the session stops (`go vet` and `golangci-lint`; it blocks on a finding).
```

## De skill `lighthouse-go-writing`

Engels, zoals elke skill. Structuur en toon volgen
[python-writing](../../../../pack/python/skills/python-writing/claude/SKILL.md); het concept "Go
skill versie 1" uit #22 levert de Go-inhoud. De description triggert op Go-werk *én* op plannen
vóór er een bestand open is, en begrenst met: `NOT for Python (use lighthouse-python-writing if
present), shell that guards something (lighthouse-shell-gates if present), workflow YAML, or files
outside the repository.` De agents-variant voegt de OpenCode-zin toe.

### Secties

1. **Domain.** Go-code en de checks erop: `gofmt`, `go vet`, `golangci-lint`, `go test`. Ook
   vóór het eerste bestand. Wat een goede test is staat in `lighthouse-testing`; vóór de eerste
   edit de classificatiezin uit die skill in de chat, met verwijzing naar *Classify first*, zonder
   eigen klassentabel:

   ```text
   This is a <class> change; the likely regression surface is <observable behaviour>.
   ```

2. **KPIs.** Elke rij is een commando dat in deze repo draait (header `| Norm | Command | Target |`,
   het commando in kolom 2, zoals python-writing):

   | Norm | Command | Target |
   |---|---|---|
   | Formatted | `git ls-files -z '*.go' \| xargs -0r gofmt -l` | empty output |
   | Modules tidy | `go mod tidy -diff` | empty output |
   | Vet clean | `go vet ./...` | rc 0 |
   | Lints at zero | `golangci-lint run ./...` | rc 0 |
   | Tests green, shuffled and race-free | `go test -race -shuffle=on ./...` | rc 0, naming `-race` |
   | The test gate is green | `bash -c "$(python3 .lighthouse/lib/lighthouse_lib/config.py test_gate.command)"` | rc 0 |

   Rij 1 loopt over `git ls-files` en niet over `.`: `gofmt -l .` loopt vanuit de hoofdcheckout
   ook door `.claude/worktrees/`. `-r` voorkomt dat `gofmt` zonder bestanden op stdin wacht.

   Toelichting onder de tabel: `-shuffle=on` is de standaard, zodat een volgordeafhankelijkheid
   tussen tests opvalt. Een run met `-shuffle=on` komt nooit uit de testcache (gemeten met Go
   1.26.4), dus `(cached)` verschijnt daar niet. `-count=1` alleen bij een run zonder shuffle,
   bij verdenking van een verouderd cacheresultaat. Rij 6 als `config.py test_gate.command` rc 1
   of een lege regel geeft: de repo heeft geen test-gate; zet er een in `lighthouse.toml`.

3. **Non-negotiable: hoe je groen meldt.** Uit het concept:
   - een nul zonder zijn omgeving is geen nul: noem GOOS, build tags, `-race` aan of uit, en
     `GOWORK` aan of uit. ✅ "tests green, linux, -race, GOWORK=off" · ❌ "tests green";
   - drie uitkomsten: geslaagd, gefaald, *niet meetbaar vanaf hier* (bijvoorbeeld cgo voor een
     ander OS, of `-race` zonder cgo).

4. **Reporting done.** Alle KPI-rijen, per rij het commando, de uitkomst, de omgeving en wat niet
   meetbaar was. Een rij die ontbreekt leest als geslaagd.

5. **Merging or deepening: replace, don't layer.** Als in python-writing, plus de Go-vorm uit het
   concept: geen interface met één implementatie "voor later"; interfaces klein, aan de kant van
   de gebruiker, pas als er een tweede implementatie of een test-fake nodig is.

6. **The Go form of lighthouse-testing.**
   - table-driven tests met een letterlijke `want` per case, nooit een in de test berekende;
   - `t.TempDir()` voor het bestandssysteem, `httptest` voor HTTP, `t.Setenv` voor de omgeving:
     het echte ding, geen gemockte collaborator;
   - `require` tegen `assert` alleen bij testify: `require` waar de rest van de test zonder die
     voorwaarde zinloos is;
   - een fake die de interface van de gebruiker implementeert, niet een die elke aanroep
     opneemt zonder iets te checken.

7. **Reaching a complexity limit: deepen, don't split.** Uit het concept, met het voorbeeld
   `switch` over berichttypes → map van handlers. Geen getallen voor `gocyclo`/`gocognit`: die
   linters staan in lighthouse niet aan; de sectie geeft de norm, niet een meting.

8. **The pairing.**

   | Half | Carries | When |
   |---|---|---|
   | this skill | procedure, KPI table, measurement rules | while planning and writing |
   | `lighthouse-testing` | test norms and the class table | for every test, before the first edit |
   | `.claude/hooks/lighthouse/gofmt.sh` | formatting, per edit | right after the edit |
   | `.claude/hooks/lighthouse/go-check.sh` | `go vet` and `golangci-lint`, once per turn | at `Stop` |

   De agents-variant zegt, als python-writing, dat OpenCode de hooks niet draait en noemt de
   commando's die je dan zelf draait.

9. **Known pitfalls.** Uit het concept: `go.work` lokaal en niet op CI (`GOWORK=off`); `-race`
   vereist cgo; cross-OS-vet slaat cgo-bestanden over; `t.Setenv` met `t.Parallel()` panikt, en
   package-level `var`-overrides in tests zijn gedeelde staat; loopvar-semantiek hangt af van de
   `go`-directive; een `toolchain`-directive met `GOTOOLCHAIN=auto` kan een andere Go draaien dan
   je denkt. Erbij: build tags (lighthouse's `e2e`) vallen buiten `go vet ./...` en
   `go test ./...`; `golangci-lint` neemt ze mee via `.golangci.yml`. Een repo met lintschuld zet
   `issues.new-from-rev` in zijn eigen `.golangci.yml`; dan meten de hook en de KPI-rij alleen
   nieuwe bevindingen, zonder dat lighthouse daar config voor nodig heeft.

## Hooks

Beide hooks volgen de afspraken van de python-hooks: `COULD NOT CHECK` als eerste regel als er
niets gecontroleerd is, nooit stil groen.

**`gofmt.sh`** (PostToolUse `Edit|Write`):

- leest `tool_input.file_path`; geen pad, geen `*.go` of geen bestaand bestand: exit 0;
- zonder `jq`, met een payload die geen JSON is, of zonder `gofmt`: exit 2 met
  `gofmt: COULD NOT CHECK — <reason>`;
- `gofmt -w <file>`, stil;
- faalt `gofmt` omdat het bestand niet parset: exit 2 met de parsefout. Een `.go`-bestand dat
  niet parset is na de edit altijd fout, ook halverwege een wijziging.

**`go-check.sh`** (Stop, matcher `""`):

- de `stop_hook_active`-guard en de worktree-bewuste `cd` van `ty-check.sh`;
- draait bij elke `Stop`, ook als er geen `.go`-bestand gewijzigd is. Een check "gewijzigd
  ten opzichte van `HEAD`" mist Go-werk dat in dezelfde beurt al gecommit is, en de test-gate bij
  commit draait geen vet en geen lint. Gemeten kost de check in lighthouse 0,3 s;
- zonder `jq`, `git`, `go` of `golangci-lint`: exit 0 met een `systemMessage`
  `go-check: COULD NOT CHECK — <reason>`, zoals `ty-check`; blokkeren bij `Stop` zou de sessie
  vasthouden;
- de pin van `golangci-lint` staat in `.golangci-version` in de repo-root, als `ruff-format.sh`
  met `.ruff-version`. Zonder dat bestand draait de hook ongepind: pinnen is een projectkeuze.
  Geeft `golangci-lint version` een andere versie dan de pin: `COULD NOT CHECK` met beide
  versies, want andere versies vinden andere dingen dan CI. De hook downloadt niets; dat zou
  netwerk vragen en `Stop` traag maken. Lighthouse krijgt `.golangci-version` met `v2.15.0`,
  dezelfde versie als `ci.yml`. Een test pint dat beide gelijk zijn;
- `go vet ./...`, dan `golangci-lint run ./...`; een bevinding: exit 2 met de output, zodat de
  agent het in dezelfde beurt herstelt. Een build die faalt blokkeert net zo: gemeten geven
  `go vet` en `golangci-lint run` er dezelfde exitcode 1 voor als voor een bevinding, en een
  ontbrekende module ziet er in de uitvoer van `go vet` uit als een bevinding. Een build die
  faalt is bovendien bijna altijd code van de agent die niet compileert; met `COULD NOT CHECK`
  zou de agent zijn beurt beëindigen op kapotte code. De `stop_hook_active`-guard voorkomt een
  lus als de oorzaak buiten de code ligt. Een exitcode die "kon niet linten" betekent (een
  kapotte config, een golangci-lint die zelf faalt) geeft `COULD NOT CHECK` in plaats van een
  oordeel over de code.

**Bash-write-gate.** `GATES[".go"] = "gofmt.sh"` en `GATE_PACKS["gofmt.sh"] = "go"` in
[`pack/core/lib/bash_writes.py`](../../../../pack/core/lib/bash_writes.py). Zo draait `gofmt.sh`
ook op een `.go`-bestand dat een Bash-aanroep schreef. `gofmt.sh` herschrijft het bestand, dus
hij komt ook in `_SIGN_AFTER_GATE`, naast `ruff-format.sh`. `test_gate_packs_name_every_gate` in
`tests/pack/test_bash_writes.py` krijgt de nieuwe gate.

## Verwijzing vanuit `lighthouse-testing`

De description van [`pack/core/skills/testing/SKILL.md`](../../../../pack/core/skills/testing/SKILL.md)
noemt `lighthouse-go-writing` naast python-writing en shell-gates, "if present". De test in
`tests/pack/test_testing_skill.py` die python-writing pint, pint ook go-writing.

## Dogfood

Lighthouse installeert het pakket bij zichzelf: `go run ./cmd/lighthouse init --upgrade --pack go`.
Dat zet `go` in `.lighthouse/manifest.toml`, schrijft de skills naar `.claude/skills/` en
`.agents/skills/`, de hooks naar `.claude/hooks/lighthouse/`, de registratie in
`.claude/settings.json` en het blok in `AGENTS.md`. De geïnstalleerde kopie wordt gecommit; CI
eist daarna `No changes.`. `pack/python` blijft in lighthouse buiten het manifest: dat is een
eigen keuze, niet van dit issue.

Lighthouse volgt zijn eigen advies: `[test_gate] command` in `lighthouse.toml` wordt
`uv run pytest -q && go test -shuffle=on ./...`. Een volgordeafhankelijke test valt dan al bij
commit op, niet pas later.

## Tests

- `tests/pack/go/test_go_writing.py`, naar het model van
  [`tests/pack/python/test_python_writing.py`](../../../../tests/pack/python/test_python_writing.py):
  frontmatter; elke KPI-rij draagt een commando; `go test -shuffle=on` is de testrij en `-count=1`
  is geen standaard; de verwijzing naar `lighthouse-testing` en de classificatiezin; de Go-vorm
  van de normen; *Reporting done*; de varianten verschillen alleen in de OpenCode-alinea's;
  Engels.
- `tests/pack/go/test_go_pack.py`, naar `test_python_pack.py`: detectie op `go.mod`, geen
  fragment.
- Hooktests die de scripts draaien tegen een `tmp_path` (klasse `hook/gate`): `gofmt.sh` met een
  bestand dat geformatteerd wordt, een dat niet parset, een niet-`.go`-bestand en zonder `gofmt`
  op het `PATH`; `go-check.sh` met een bevinding, een schone run, een "kon niet linten"-exitcode,
  een pin die niet klopt, een ontbrekende tool en `stop_hook_active`. De tests zetten **stubs**
  voor `go`, `gofmt` en `golangci-lint` op het `PATH`: wat de hook beslist, is hoe hij
  exitcodes en output vertaalt naar blokkeren of doorlaten, en dat test je met gecontroleerde
  exitcodes. Zo draaien ze in de Python-lane van CI zonder `setup-go` of `golangci-lint`, en
  hoeft `ci.yml` niet te veranderen. Wat de echte tools doen, meet de Go-lane al.
- Een `go`-rij in `TestLanguagePacks` in
  [`internal/packs/core_test.go`](../../../../internal/packs/core_test.go).
- `tests/pack/test_skill_contracts.py` dekt de verwijzingen al via zijn glob over `pack/*/skills/*`.
- `.lighthouse/scripts/check-instructions.sh` blijft groen; let op check 4 (overlap tussen
  python-writing, go-writing en testing).

Verificatie: `uv run pytest -q && go test ./...` (de test-gate), `go test -tags e2e ./e2e/`,
`.lighthouse/scripts/check-instructions.sh`, en na de dogfood `go run ./cmd/lighthouse init --upgrade`
met `No changes.`.

## Aan de bouwer

- De exacte Engelse formulering van de skill, binnen de secties hierboven.
- Of de Go-normen per bestand (het concept noemt de netwerkguard in `TestMain`) in de skill
  komen of wegvallen.
- De exitcodes van `golangci-lint` die "kon niet linten" betekenen: vaststellen met de
  geïnstalleerde versie, niet aannemen.
- De volgorde van de commits.

## Mijn keuzes

Niet door de gebruiker besloten, wel door mij gemaakt:

- detectie op `go.mod` in de root;
- geen fragment, vanwege de mergeregel;
- `gofmt` per edit, `vet` en lint bij `Stop`, `go-check` bij elke `Stop`;
- de pin van `golangci-lint` in `.golangci-version`, een afwijkende versie geeft
  `COULD NOT CHECK`;
- stubs in de hooktests, geen wijziging aan `ci.yml`;
- lintschuld via `issues.new-from-rev` in de eigen `.golangci.yml`, geen lighthouse-config;
- lighthouse's test-gate krijgt `-shuffle=on`;
- de testing-description noemt go-writing;
- geen getallen voor de complexiteitsgrens.
