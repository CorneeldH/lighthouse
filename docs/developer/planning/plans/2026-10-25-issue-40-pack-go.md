# Taalpakket `pack/go` Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** een taalpakket `pack/go` met de skill `lighthouse-go-writing`, twee hooks en een
Bash-write-gate, geïnstalleerd in lighthouse zelf.

**Architecture:** `pack/go` volgt de indeling van `pack/python`; `internal/packs` pakt een nieuwe
map onder `pack/` vanzelf op (`//go:embed all:*/*`), dus er is geen Go-code nodig buiten één
testrij. De hooks zijn bash, getest als subprocess met stubs voor `go`, `gofmt` en
`golangci-lint` op het `PATH`.

**Tech Stack:** bash, jq, Python 3 + pytest (tests), Go 1.26.4 (`internal/packs`-test), golangci-lint
2.15.0.

**Spec:** [2026-10-25-pack-go-design.md](../specs/2026-10-25-pack-go-design.md)

## Global Constraints

- Skills, hookmeldingen en code in het Engels; commitberichten en dit plan in het Nederlands.
- Elk nieuw bestand onder `pack/` krijgt in `pack/SOURCES.toml` een entry `[[file]]` met
  `path = "pack/go/…"` en `new = true` (`tests/test_sources.py`), in dezelfde commit.
- Hooks melden "niets gecontroleerd" met de eerste regel `<hook>: COULD NOT CHECK — <reason>`;
  nooit stil groen.
- `pack/go` heeft **geen** `lighthouse.toml.fragment`.
- Geen `.golangci.blocking.yml`, geen `.claude/rules/go.md` als pakketbestand.
- De standaard testopdracht is `go test -race -shuffle=on ./...`; `-count=1` is geen standaard.
- De pin van golangci-lint is `v2.15.0` (`.golangci-version`), gelijk aan `ci.yml`.
- Gemeten exitcodes (golangci-lint 2.15.0, Go 1.26.4): `golangci-lint run` 0 = schoon, 1 =
  bevindingen (ook een build die faalt); `golangci-lint config verify` 0 = geldig, 3 = ongeldig,
  6 = geen configbestand; `golangci-lint version --short` print `2.15.0` (zonder `v`);
  `go vet` 0 = schoon, 1 = bevinding of build die faalt; `gofmt -w` 0 = ok, 2 = parset niet.
  Een build die faalt is dus niet van een bevinding te scheiden en blokkeert ook (spec,
  *go-check.sh*); alleen een rc die "kon niet linten" betekent geeft `COULD NOT CHECK`.
- Na een wijziging onder `pack/`: `go run ./cmd/lighthouse init --upgrade` en de geïnstalleerde
  kopie meecommitten (Task 7 doet de eerste installatie).
- Test-gate vóór elke commit: `uv run pytest -q && go test ./...` (Task 7 voegt `-shuffle=on` toe).

## Review Focus

1. **Een repo zonder `go.mod` in de root** (het pakket handmatig met `--pack go` toegevoegd, of
   Go in een submap): `go-check.sh` zegt niets en blokkeert niet, in plaats van elke beurt op
   `go: go.mod file not found` te blokkeren. → test in Task 2.
2. **Geen `.golangci.yml` in de repo**: `config verify` geeft 6; dat is geen fout, de hook lint
   met de standaardset. → test in Task 2.
3. **Een pinbestand met `v` en een newline** (`v2.15.0\n`) tegen `version --short` zonder `v`:
   moet gelijk zijn. → test in Task 2.
4. **Een `.go`-bestand dat via Bash geschreven wordt** en niet parset: de Bash-write-gate
   rapporteert een bevinding, geen `unchecked`. → test in Task 3.
5. **Een herhaalde `Stop` na een blokkade** (`stop_hook_active: true`): exit 0, geen lus. → test
   in Task 2.

---

## File Structure

| Bestand | Rol | Task |
|---|---|---|
| `pack/go/hooks/gofmt.sh` | PostToolUse-hook: `gofmt -w`, blokkeert op een parsefout | 1 |
| `pack/go/hooks/go-check.sh` | Stop-hook: `go vet` + `golangci-lint`, met pin | 2 |
| `pack/go/hooks/hooks.json` | registratie van beide hooks | 1, 2 |
| `tests/pack/go/{__init__.py,conftest.py}` | installatie core + go voor de hooktests | 1 |
| `tests/pack/go/test_gofmt.py`, `test_go_check.py` | hooktests met stubs | 1, 2 |
| `pack/core/lib/bash_writes.py` | `.go` → `gofmt.sh` | 3 |
| `pack/go/skills/go-writing/{claude,agents}/SKILL.md` | de skill | 4 |
| `pack/go/requires.toml` | tools per eenheid (de skilltest leest hem) | 4 |
| `tests/pack/go/test_go_writing.py` | contracttests op de skilltekst | 4 |
| `pack/go/{detect.toml,instructions/AGENTS.md}` | detectie, blok | 5 |
| `tests/pack/go/test_go_pack.py`, `internal/packs/core_test.go` | pakkettests | 5 |
| `pack/core/skills/testing/SKILL.md`, `tests/pack/test_testing_skill.py` | verwijzing | 6 |
| `.golangci-version`, `lighthouse.toml`, `tests/test_golangci_pin.py`, `tests/test_dogfood.py`, geïnstalleerde kopie | dogfood | 7 |

---

### Task 1: `gofmt.sh`

**Files:**
- Create: `pack/go/hooks/gofmt.sh`, `pack/go/hooks/hooks.json`
- Create: `tests/pack/go/__init__.py`, `tests/pack/go/conftest.py`, `tests/pack/go/test_gofmt.py`
- Modify: `pack/SOURCES.toml` (twee entries: `gofmt.sh`, `hooks.json`; de tests staan buiten `pack/`)

**Interfaces:**
- Consumes: `tests.pack._testkit.install_pack`, `tool_bin`, `MINIMAL_TOML`.
- Produces: `tests/pack/go/conftest.py` met `GO_ROOT: Path` en `GO_HOOKS: Path`
  (`install_pack("go", GO_ROOT)`), en een autouse-fixture die `LIGHTHOUSE_REPO_ROOT=GO_ROOT`
  zet, zoals `tests/pack/python/conftest.py`. Hook-uitvoer: exit 0 / exit 2, eerste stderr-regel
  `gofmt: COULD NOT CHECK — <reason>` of `gofmt: <file> does not parse — fix the syntax.`

- [ ] **Step 1: Schrijf de falende tests** in `tests/pack/go/test_gofmt.py`. Run de hook als in
  `tests/pack/python/test_ruff_format.py` (`_run(payload, cwd, env)`), payload
  `{"tool_input": {"file_path": str(f)}}`. Stub `gofmt` op een `tool_bin`-map vóór het `PATH`;
  de stub schrijft zijn argumenten naar `$STUB_LOG` en eindigt met `exit ${STUB_RC:-0}`, met bij
  rc 2 de regel `x.go:2:8: expected ')', found 'EOF'` op stderr.

  | Test | Invoer | Verwacht |
  |---|---|---|
  | `test_formats_a_go_file` | bestaand `a.go`, `STUB_RC=0` | rc 0, `STUB_LOG` == `-w <pad van a.go>` |
  | `test_blocks_a_file_that_does_not_parse` | `STUB_RC=2` | rc 2, eerste regel `gofmt: <pad> does not parse — fix the syntax.`, de parsefout in stderr |
  | `test_other_extension_passes` | `a.py` | rc 0, `STUB_LOG` bestaat niet |
  | `test_missing_file_passes` | pad dat niet bestaat | rc 0 |
  | `test_no_path_passes` | `{"tool_input": {}}` | rc 0 |
  | `test_classify_reads_the_parse_error_as_a_finding` | de echte `gofmt` (geen stub) op een `x.go` met `package m\n\nfunc F( {\n` | rc 2, `bash_writes.classify(2, result.stderr.encode()) == "finding"`, als `test_ruff_format.py:96-103` |
  | `test_without_gofmt_could_not_check` | `tool_bin(..., drop="gofmt")` zonder stub, `PATH` = alleen die map (als `test_ty_check.py:176`) | rc 2, eerste regel begint met `gofmt: COULD NOT CHECK — ` |
  | `test_without_jq_could_not_check` | `drop="jq"`, `PATH` = alleen die map | idem |
  | `test_empty_payload_could_not_check` | `""` | idem |
  | `test_invalid_json_could_not_check` | `"{"` | idem |
  | `test_registered_on_edit_write` | `pack/go/hooks/hooks.json` | `PostToolUse` → matcher `Edit\|Write` → command `${CLAUDE_PLUGIN_ROOT}/hooks/gofmt.sh`, timeout 15 |

- [ ] **Step 2: Run** `uv run pytest -q tests/pack/go/test_gofmt.py` — Expected: FAIL (hook bestaat niet).
- [ ] **Step 3: Schrijf `pack/go/hooks/gofmt.sh`** naar de vorm van `pack/python/hooks/ruff-format.sh`
  (`HOOK_NAME="gofmt"`, `cannot_check`, de jq- en payload-checks, `case *.go`, `-f`), dan
  `out=$(gofmt -w "$FILE_PATH" 2>&1)`; rc ≠ 0 → de parsefout-melding plus `$out`, exit 2.
  Kopcommentaar zoals ruff-format: wat hij doet, waarom niet fail-open. Schrijf `hooks.json`
  met alleen de PostToolUse-entry. SOURCES-entries erbij.
- [ ] **Step 4: Run** `uv run pytest -q tests/pack/go/ tests/test_sources.py` — Expected: PASS.
- [ ] **Step 5: Commit** `feat(go): gofmt-hook na elke edit van een .go-bestand (#40)`.

### Task 2: `go-check.sh`

**Files:**
- Create: `pack/go/hooks/go-check.sh`, `tests/pack/go/test_go_check.py`
- Modify: `pack/go/hooks/hooks.json` (Stop-entry), `pack/SOURCES.toml`

**Interfaces:**
- Consumes: `GO_HOOKS` uit Task 1.
- Produces: Stop-hook; blokkeren = exit 2 met eerste stderr-regel `go-check: go vet finds
  problems.` of `go-check: golangci-lint finds problems.`; niet gecontroleerd = exit 0 met stdout
  `{"systemMessage": "go-check: COULD NOT CHECK — <reason> Nothing was vetted or linted; that is not a green light."}`.

- [ ] **Step 1: Schrijf de falende tests.** Elke test maakt een git-repo in `tmp_path` met
  `go.mod`, installeert `install_pack("go", root)` en draait de hook als in
  `tests/pack/python/test_ty_check.py` (`_run(root, payload, env)`, payload
  `{"stop_hook_active": false, "cwd": str(root)}`). Stubs: `go` (bij `vet`: print `$STUB_VET_OUT`,
  `exit ${STUB_VET_RC:-0}`) en `golangci-lint` (`version --short` → print `${STUB_VERSION:-2.15.0}`;
  `config verify` → `exit ${STUB_VERIFY_RC:-0}`; `run` → print `$STUB_RUN_OUT`,
  `exit ${STUB_RUN_RC:-0}`), elk aanroep-argv naar `$STUB_LOG`.

  | Test | Invoer | Verwacht |
  |---|---|---|
  | `test_clean_passes` | alles 0 | rc 0, stdout leeg, `STUB_LOG` bevat `vet ./...` en `run ./...` |
  | `test_vet_finding_blocks` | `STUB_VET_RC=1`, `STUB_VET_OUT="a.go:3:2: fmt.Printf format %d has arg \"s\" of wrong type string"` | rc 2, eerste regel `go-check: go vet finds problems.`, de vet-regel in stderr, `run` niet aangeroepen |
  | `test_build_failure_blocks` | `STUB_VET_RC=1`, `STUB_VET_OUT="vet: ./a.go:3:17: undefined: x"` | rc 2, eerste regel `go-check: go vet finds problems.`, geen `COULD NOT CHECK` (spec: een build die faalt blokkeert) |
  | `test_lint_finding_blocks` | `STUB_RUN_RC=1`, `STUB_RUN_OUT="a.go:5:22: Error return value of \`os.Remove\` is not checked (errcheck)"` | rc 2, eerste regel `go-check: golangci-lint finds problems.` |
  | `test_lint_failure_could_not_check` | `STUB_RUN_RC=3` | rc 0, systemMessage begint met `go-check: COULD NOT CHECK — ` |
  | `test_invalid_config_could_not_check` | `STUB_VERIFY_RC=3` | idem, `run` niet aangeroepen |
  | `test_no_config_file_lints_with_defaults` | `STUB_VERIFY_RC=6` | rc 0, stdout leeg, `run ./...` aangeroepen |
  | `test_pin_matches_with_v_and_newline` | `.golangci-version` = `v2.15.0\n`, `STUB_VERSION=2.15.0` | rc 0, stdout leeg |
  | `test_pin_mismatch_could_not_check` | pin `v2.15.0`, `STUB_VERSION=2.11.4` | systemMessage met `2.11.4` en `2.15.0`; `vet` niet aangeroepen |
  | `test_no_pin_runs_unpinned` | geen `.golangci-version`, `STUB_VERSION=9.9.9` | rc 0, stdout leeg |
  | `test_no_go_mod_says_nothing` | repo zonder `go.mod` | rc 0, stdout leeg, `STUB_LOG` bestaat niet |
  | `test_no_go_mod_without_golangci_lint_says_nothing` | repo zonder `go.mod`, `tool_bin(drop="golangci-lint")`, `PATH` = alleen die map | rc 0, stdout leeg |
  | `test_stop_hook_active_passes` | `stop_hook_active: true`, `STUB_VET_RC=1` | rc 0, `STUB_LOG` bestaat niet |
  | `test_missing_tool_could_not_check` | parametrize over `go`, `golangci-lint`, `jq`, `git` met `tool_bin(drop=…)`, geen stub voor de gedropte tool, `PATH` = alleen die map (als `test_ty_check.py:176`) | systemMessage begint met `go-check: COULD NOT CHECK — `, met de toolnaam (voor `jq` de handgebouwde JSON, als `ty-check.sh`) |
  | `test_registered_on_stop` | `hooks.json` | `Stop` → matcher `""` → `${CLAUDE_PLUGIN_ROOT}/hooks/go-check.sh`, timeout 120 |

- [ ] **Step 2: Run** `uv run pytest -q tests/pack/go/test_go_check.py` — Expected: FAIL.
- [ ] **Step 3: Schrijf `pack/go/hooks/go-check.sh`** naar de vorm van `pack/python/hooks/ty-check.sh`
  (`report`, `cannot_check`, `stop_hook_active`-guard, worktree-bewuste `cd` naar de git-root).
  De toolcheck splitst: vóór het lezen van de payload alleen `jq` en `git`; na de `cd` geen
  `$ROOT/go.mod` → exit 0, stil; pas dan `go` en `golangci-lint` → `cannot_check` met de
  toolnaam. Zo zegt een repo zonder Go-module niets, ook zonder Go-tooling. Daarna: pin uit `$ROOT/.golangci-version`
  (`tr -d '[:space:]'`, voorloop-`v` weg) tegen `golangci-lint version --short` → mismatch is
  `cannot_check "golangci-lint <have> runs; .golangci-version pins <want>."`; `go vet ./...`
  rc ≠ 0 → blok (ook een build die faalt); `golangci-lint config verify` rc ∉ {0, 6} → `cannot_check` met de eerste drie
  regels; `golangci-lint run ./...` rc 1 → blok, rc ∉ {0, 1} → `cannot_check`. De blokmelding
  zegt op regel 2: `Fix them in the code — do not add a //nolint over a finding. If you really want a linter off, ask.`
  Kopcommentaar: waarom Stop en niet per edit (een package compileert halverwege een wijziging
  vaak niet), waarom elke Stop (Go-werk dat in de beurt al gecommit is), en waarom een build die
  faalt blokkeert (zelfde rc als een bevinding; meestal code die niet compileert). Stop-entry in
  `hooks.json`, SOURCES-entry.
- [ ] **Step 4: Run** `uv run pytest -q tests/pack/go/ tests/test_sources.py` — Expected: PASS.
- [ ] **Step 5: Commit** `feat(go): go-check-hook met go vet en golangci-lint bij Stop (#40)`.

### Task 3: Bash-write-gate voor `.go`

**Files:**
- Modify: `pack/core/lib/bash_writes.py:46-58` (`GATES`, `GATE_PACKS`), `:810` (`_SIGN_AFTER_GATE`)
- Modify: `tests/pack/test_bash_writes.py:1262-1268`, `tests/pack/test_bash_write_gates.py`

**Interfaces:**
- Consumes: de exit-afspraak van `gofmt.sh` uit Task 1 (2 + eerste regel zonder token = bevinding).
- Produces: `GATES[".go"] == "gofmt.sh"`, `GATE_PACKS["gofmt.sh"] == "go"`,
  `"gofmt.sh" in _SIGN_AFTER_GATE`.

- [ ] **Step 1: Pas de tests aan.** `test_gate_packs_name_every_gate` krijgt `"gofmt.sh": "go"`.
  Nieuw in `test_bash_writes.py`: `test_go_gate_path` — `bw.gate_path(str(gate_root), ".go") == hooks / "gofmt.sh"`,
  en `test_gofmt_signs_after_gate` — `"gofmt.sh" in bw._SIGN_AFTER_GATE`. Nieuw in
  `test_bash_write_gates.py`, naar het patroon van de bestaande `shellcheck.sh`-stub
  (`:168-196`): een `gofmt.sh`-stub die exit 2 geeft met `x.go:2:8: expected ')'`; een Bash-aanroep
  die `x.go` schrijft geeft `== x.go (gofmt.sh) ==` in stderr en blokkeert als bevinding.
- [ ] **Step 2: Run** `uv run pytest -q tests/pack/test_bash_writes.py tests/pack/test_bash_write_gates.py` — Expected: FAIL.
- [ ] **Step 3: Voeg de drie waarden toe** in `bash_writes.py`.
- [ ] **Step 4: Run** dezelfde tests — Expected: PASS.
- [ ] **Step 5: Run** `go run ./cmd/lighthouse init --upgrade --yes` en commit alles wat het
  schreef mee (de geïnstalleerde `.lighthouse/lib/lighthouse_lib/bash_writes.py` en de sha256
  in `.lighthouse/manifest.toml`): `feat(core): Bash-write-gate voor .go-bestanden (#40)`.

### Task 4: De skill `lighthouse-go-writing`

**Files:**
- Create: `pack/go/skills/go-writing/claude/SKILL.md`, `pack/go/skills/go-writing/agents/SKILL.md`
- Create: `pack/go/requires.toml` (hier, niet in Task 5: `test_requires_names_the_skill_per_tool`
  in `tests/pack/test_skill_contracts.py:330-341` leest hem voor de nieuwe skill)
- Create: `tests/pack/go/test_go_writing.py`
- Modify: `pack/SOURCES.toml`

**Interfaces:**
- Consumes: de hooknamen `gofmt.sh`, `go-check.sh` (Task 1, 2); `config.py test_gate.command`.
- Produces: `name: lighthouse-go-writing`; de secties uit de spec, *De skill*, punten 1-9, met deze
  koppen: `## Domain`, `## KPIs`, `## Non-negotiable`, `## Reporting done`,
  `## Merging or deepening: replace, don't layer`, `## The Go form of lighthouse-testing`,
  `## Reaching a complexity limit: deepen, don't split`, `## The pairing`, `## Known pitfalls`.

- [ ] **Step 1: Schrijf de falende tests**, met `_frontmatter` en `_kpi_rows` overgenomen uit
  `tests/pack/python/test_python_writing.py:33-48`:
  - `test_frontmatter_name`: `name: lighthouse-go-writing\n` in beide varianten.
  - `test_description_has_a_negative_boundary`: `NOT for`, `lighthouse-python-writing`,
    `lighthouse-shell-gates` in de frontmatter.
  - `test_kpi_rows_all_carry_a_command`: ≥ 6 rijen, elk een backtick in kolom 2.
  - `test_test_row_shuffles`: een rij bevat `` `go test -race -shuffle=on ./...` ``; geen rij bevat `-count=1`.
  - `test_kpi_rows`: de rijen bevatten `` `git ls-files -z '*.go' \| xargs -0r gofmt -l` ``,
    `` `go mod tidy -diff` ``, `` `go vet ./...` ``, `` `golangci-lint run ./...` `` en `test_gate.command`.
  - `test_config_keys_are_known`: als `test_python_writing.py:72-88`.
  - `test_points_to_the_testing_skill`: `lighthouse-testing`, `Classify first` en
    `This is a <class> change; the likely regression surface is <observable behaviour>.`; geen
    klassentabel (`` `pure-logic` `` komt niet voor).
  - `test_go_form_of_the_testing_norms`: `t.TempDir()`, `httptest`, `testify`, `require`, `want`.
  - `test_reporting_done`: `## Reporting done` en `A row left out of the report reads as passed.`
  - `test_names_the_environment`: `GOOS`, `-race`, `GOWORK`, `not measurable from here`.
  - `test_no_concept_leftovers`: `golangci.blocking` en `-count=1 ./...` komen niet voor;
    `.claude/rules/go.md` alleen met `if present` op dezelfde regel.
  - `test_variants_keep_their_difference`: als `test_python_writing.py:139`; `OpenCode` alleen in
    de agents-variant.
  - `test_english`: als `test_python_writing.py:153`.
- [ ] **Step 2: Run** `uv run pytest -q tests/pack/go/test_go_writing.py` — Expected: FAIL.
- [ ] **Step 3: Schrijf `pack/go/requires.toml`** met het kopcommentaar van
  `pack/python/requires.toml`: `git = ["go-check", "go-writing"]`, `go = ["go-check", "go-writing"]`,
  `gofmt = ["gofmt", "go-writing"]`, `golangci-lint = ["go-check", "go-writing"]`,
  `jq = ["gofmt", "go-check"]`, `python3 = ["go-writing"]`, `tomllib = ["go-writing"]`. `git`
  noemt go-writing omdat de KPI-rij `git ls-files` het git-patroon van de contracttest
  (`test_skill_contracts.py:322-323`) raakt.
- [ ] **Step 4: Schrijf de claude-variant** volgens de spec, *De skill*, met
  `superpowers:writing-skills` voor vorm en description. De laagkeuze (een eigen skill in
  `pack/go`) staat al in de spec; `/lighthouse-instrument` draait hier niet. Description zoals de spec, met de
  triggers "writing or changing Go", `_test.go`, "planning a refactor … before a file is open".
  Kopieer naar de agents-variant en voeg de OpenCode-zin aan de description en de
  OpenCode-alinea's toe op de plekken waar python-writing ze heeft (na de KPI-toelichting en
  onder *The pairing*), met de handcommando's `gofmt -w <file>`, `go vet ./...`,
  `golangci-lint run ./...`. SOURCES-entries (beide SKILL.md's en `requires.toml`).
- [ ] **Step 5: Run** `uv run pytest -q tests/pack/go/ tests/pack/test_skill_contracts.py tests/test_sources.py` — Expected: PASS.
- [ ] **Step 6: Commit** `feat(go): skill lighthouse-go-writing (#40)`.

### Task 5: Detectie, tools en instructieblok

**Files:**
- Create: `pack/go/detect.toml`, `pack/go/instructions/AGENTS.md` (`requires.toml` komt uit Task 4)
- Create: `tests/pack/go/test_go_pack.py`
- Modify: `internal/packs/core_test.go:133-187` (rij `go`), `pack/SOURCES.toml`

**Interfaces:**
- Consumes: alle units uit Task 1, 2, 4.
- Produces: het complete pakket zoals `Load` het ziet.

- [ ] **Step 1: Schrijf de falende tests.** `test_go_pack.py`, naar `tests/pack/python/test_python_pack.py`:
  - `test_detect_on_go_mod`: `tomllib.loads(detect.toml)["files"] == ["go.mod"]`.
  - `test_no_fragment`: `not (GO / "lighthouse.toml.fragment").exists()`.
  - `test_instruction_block_is_short_and_names_skill_and_gates`: begint met `## Lighthouse: go\n`,
    ≤ 80 woorden, bevat `/lighthouse-go-writing`, `gofmt`, `go-check`.

  `TestLanguagePacks` krijgt de rij:

  ```go
  {
  	name: "go",
  	files: []string{
  		".agents/skills/lighthouse-go-writing/SKILL.md",
  		".claude/hooks/lighthouse/go-check.sh",
  		".claude/hooks/lighthouse/gofmt.sh",
  		".claude/skills/lighthouse-go-writing/SKILL.md",
  	},
  	detect: true,
  	tools:  "git,go,gofmt,golangci-lint,jq,python3,tomllib",
  	blocks: 1,
  	hooks:  2,
  },
  ```
- [ ] **Step 2: Run** `uv run pytest -q tests/pack/go/test_go_pack.py && go test -shuffle=on ./internal/packs/` — Expected: FAIL.
- [ ] **Step 3: Schrijf de twee bestanden.** `detect.toml` met het kopcommentaar van
  `pack/python/detect.toml`, toegepast op `go.mod`. Het blok: de tekst uit de spec,
  *Instructieblok*. SOURCES-entries.
- [ ] **Step 4: Run** dezelfde commando's — Expected: PASS.
- [ ] **Step 5: Commit** `feat(go): detectie, tools en instructieblok van pack/go (#40)`.

### Task 6: `lighthouse-testing` noemt go-writing

**Files:**
- Modify: `pack/core/skills/testing/SKILL.md:3`, `tests/pack/test_testing_skill.py:16-18`

- [ ] **Step 1:** `test_description_bounds_tdd_and_the_language_skill` assert ook
  `"lighthouse-go-writing" in FRONT`. Run — Expected: FAIL.
- [ ] **Step 2:** De description eindigt op
  `NOTE — not for the tooling of one language: that is lighthouse-python-writing, lighthouse-go-writing or lighthouse-shell-gates, if present.`
- [ ] **Step 3: Run** `uv run pytest -q tests/pack/test_testing_skill.py tests/pack/test_skill_contracts.py` — Expected: PASS.
- [ ] **Step 4:** `go run ./cmd/lighthouse init --upgrade --yes`, commit met de geïnstalleerde
  kopieën: `docs(core): testing-skill verwijst naar lighthouse-go-writing (#40)`.

### Task 7: Dogfood

**Files:**
- Create: `.golangci-version` (`v2.15.0\n`), `tests/test_golangci_pin.py`
- Modify: `lighthouse.toml` (`[test_gate] command`), `tests/test_dogfood.py`
- Generated: `.lighthouse/manifest.toml`, `.claude/skills/lighthouse-go-writing/`,
  `.agents/skills/lighthouse-go-writing/`, `.claude/hooks/lighthouse/{gofmt,go-check}.sh`,
  `.claude/settings.json`, `AGENTS.md`

- [ ] **Step 1: Schrijf de falende tests.**
  - `tests/test_golangci_pin.py::test_pin_matches_ci`: de inhoud van `.golangci-version`
    (gestript) is gelijk aan de `version:` onder de stap `golangci/golangci-lint-action` in
    `.github/workflows/ci.yml` (`v2.15.0`).
  - `tests/test_dogfood.py::test_go_hooks_registered`: in `.claude/settings.json` staat
    `gofmt.sh` onder `PostToolUse` matcher `Edit|Write` en `go-check.sh` onder `Stop`.
  - `tests/test_dogfood.py::test_test_gate_shuffles`: `config.py test_gate.command` bevat
    `go test -shuffle=on ./...`.
- [ ] **Step 2: Run** `uv run pytest -q tests/test_golangci_pin.py tests/test_dogfood.py` — Expected: FAIL.
- [ ] **Step 3:** Schrijf `.golangci-version`; zet `[test_gate] command` op
  `uv run pytest -q && go test -shuffle=on ./...`; run
  `go run ./cmd/lighthouse init --upgrade --pack go --yes`.
- [ ] **Step 4: Verifieer.**
  - `uv run pytest -q && go test -shuffle=on ./...` — PASS;
  - `go test -tags e2e ./e2e/` — PASS;
  - `.lighthouse/scripts/check-instructions.sh` — rc 0 (let op check 4: overlap tussen
    go-writing, python-writing en testing; los een melding op met een begrenzende zin in de
    description, niet met een drempel);
  - `go run ./cmd/lighthouse init --upgrade </dev/null` — `No changes.`;
  - de KPI-rijen van de skill in lighthouse: `git ls-files -z '*.go' | xargs -0r gofmt -l`
    (leeg), `go mod tidy -diff` (leeg), `go vet ./...`, `golangci-lint run ./...`,
    `go test -race -shuffle=on ./...` (alle rc 0).
- [ ] **Step 5: Commit** `feat(go): lighthouse draait pack/go (#40)`.

---

## Na het plan

- Acceptatiecriteria van #40 ↔ taken: spec (al gecommit, `dff4468`); `pack/go` met skill en
  KPI-tabel → Task 4, 5; verwijzing en classificatiezin → Task 4; contracttests → Task 4, 5;
  `check-instructions.sh` groen → Task 7.
- PR via `/lighthouse-verify-pr` met `Closes #40`.
