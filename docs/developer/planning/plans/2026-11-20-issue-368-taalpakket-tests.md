# Triage taalpakket-tests (#368) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Elke pytest-test in `tests/pack/{go,shell,node,expo}/` krijgt één bestemming (weg of naar Go), en de Python-testdata van `tests/pack/fixtures/code_language/` verhuist naar `internal/codelang/testdata/`, zodat onder `tests/pack/` buiten `python/` geen `.py` overblijft.

**Architecture:** Drie soorten bestemming. De bash-hooks van `pack/go` en `pack/shell` hebben buiten pytest geen enkele test die ze draait; die gaan als black box naar `e2e/` (build tag `e2e`, job `go-e2e`), via het commando dat `lighthouse init` in `.claude/settings.json` registreert. Pins op skilltekst gaan naar het contractpakket dat #367 maakt. Detectie van de echte pakketten gaat naar `internal/initcmd`. De rest gaat weg met een reden in de triagetabel.

**Tech Stack:** Go 1.26 (`testing`, `os/exec`), bash, `jq`, de vastgepinde shellcheck uit `pack/shell/scripts/lib/shellcheck-bin.sh`.

**Spec:** [`docs/developer/planning/specs/2026-11-16-repo-zonder-python-design.md`](../specs/2026-11-16-repo-zonder-python-design.md), §1 (triage), §2 (skillcontracten), §3 (black box). Issue: #368.

## Global Constraints

- Kernvraag per testfunctie (spec §1): *welke productiewijziging laat deze test falen?* Geparametriseerde gevallen tellen als één test als ze op dezelfde mutatie falen.
- Bestemmingen: **weg — onderwerp weg**, **weg — gedekt** (noem de Go-test), **weg — beschermt niets**, **naar Go**.
- Een test die je wilt porten maar niet kunt laten falen met een mutatie, port je niet (spec, *Aan de bouwer*).
- Twijfel tussen *gedekt* en *naar Go*: muteer de productiecode en draai de Go-test. Faalt hij, dan is het gedekt.
- Geen test die na de PR zowel in pytest als in Go bestaat.
- No-go: ander gedrag van een hook of script. Er verandert niets onder `pack/`.
- No-go: `tests/pack/python/` en `pack/python/` blijven. Oude plannen niet herschrijven.
- Milestone-DoD: `git ls-files '*.py'` geeft alleen paden onder `pack/python/` en `tests/pack/python/`. Dus ook geen `.py` onder `internal/codelang/testdata/`.
- Go-code volgt `/lighthouse-go-standard`; tests volgen `/lighthouse-test-standard` (classificatiezin in de chat vóór de eerste edit).
- Artefacten (PR-body, changelog, commitberichten) in het Nederlands; namen en commentaar in Go in het Engels (`code_language = "en"`).

## Afhankelijkheden en volgorde

- **#367** maakt het Go-pakket voor skillcontracten en port de generieke checks van `tests/pack/test_skill_contracts.py` (frontmatter-naam, bestaande verwijzingen, bekende configsleutels, skill-links). Taak 6 hangt daarvan af: de pins van dit issue landen in dat pakket, en een aantal rijen hieronder is *gedekt* door die generieke port. Classificeert #367 een generieke check als weg, dan worden de rijen die erop leunen hier opnieuw beoordeeld.
- **#366** verhuist `tests/pack/_testkit.py` naar `tests/pack/python/` en port `bash-write-check` naar e2e. Taak 7 (opruimen) hangt daarvan af.
- **Volgorde:** taken 1 t/m 5 kunnen nu. Taak 6 na de merge van #367, taak 7 na de merge van #366 en #367. #368 merget als laatste van de drie; alleen dan is AC 4 waar.
- Merge `origin/main` in deze branch vóór taak 6 en vóór taak 7 (`git merge`, geen rebase: de branch staat dan op origin).

## Review Focus

1. **Hook op macOS (bash 3.2).** `macos.yml` draait `go-e2e` vóór elke release; de pytest-versie draaide daar ook. De e2e-port moet daar groen zijn. Taak 3 laat de helper geen GNU-only vlaggen gebruiken.
2. **Ontbrekende tool in CI.** `jq`, `bash` of de shellcheck-pin ontbreekt: in CI (`CI` gezet) faalt de test, lokaal skipt hij met de reden, naar het patroon van `needUV` (`e2e/e2e_test.go:309`). Een stille skip in CI meet niets. Taak 3 legt `needTool` vast.
3. **Registratie in plaats van scriptpad.** Een hook die alleen via zijn pad draait, bewijst niet dat `init` hem registreert. Elke e2e-test draait het commando uit `.claude/settings.json` (event, matcher en timeout); taak 3 test die drie.
4. **Fixturepad verschuift stil.** Na de verhuizing moet `TestUnitsFixture` elk unitbestand nog vinden en lezen; een lege glob is rood (`no units fixtures`), niet groen. Taak 1 controleert het aantal subtests.
5. **Dubbele dekking na de port.** Een pytest-test die blijft naast zijn Go-port. Taak 7 controleert met `git ls-files` dat de vier mappen weg zijn.

---

## Triagetabel (concept)

Telling: `def test_`-functies, 123 in totaal. Voorlopig: **95 naar Go, 26 weg, 2 open**. De uitvoerder bevestigt elke *naar Go* met één mutatie (kolom *Mutatie*), en mag een rij naar *weg* verplaatsen als die mutatie al door een andere geporte test valt. De definitieve tabel gaat in de PR-body.

### `tests/pack/go/test_go_lint_gate.py` (29) — hook `pack/go/hooks/go-lint-gate.sh`

Geen Go-test draait deze hook (geen hit in `e2e/` of `internal/`). Bestemming: `e2e/golintgate_test.go`.

| Test | Bestemming | Mutatie / reden |
|---|---|---|
| `test_clean_passes` | naar Go | `golangci-lint run` overgeslagen: de log mist `run ./...` |
| `test_finding_blocks` (vet, build, lint: 1) | naar Go | `exit 2` na een finding wordt `exit 0` |
| `test_lint_failure_could_not_check` | naar Go | rc 3 van golangci-lint gelezen als finding |
| `test_invalid_config_could_not_check` | naar Go | `config verify` overgeslagen |
| `test_no_config_file_lints_with_defaults` | naar Go | rc 6 (geen config) als fout behandeld |
| `test_pin_matches_with_v_and_newline` | naar Go | `v` of newline niet gestript bij `.golangci-version` |
| `test_pin_mismatch_could_not_check` | naar Go | versievergelijking weg |
| `test_no_pin_runs_unpinned` | naar Go | ontbrekende pin wordt een fout |
| `test_no_go_mod_reports_not_checked` | naar Go | go.mod-check weg |
| `test_no_go_mod_without_golangci_lint_reports_not_checked` | naar Go | toolcheck vóór de go.mod-check |
| `test_stop_hook_active_passes` | naar Go | `stop_hook_active` genegeerd |
| `test_missing_tool_could_not_check` (5 tools: 1) | naar Go | één tool uit de preflight weg |
| `test_a_finding_in_the_edited_worktree_blocks_with_the_cwd_on_main` | naar Go | lint op `cwd` in plaats van `lighthouse turn-roots` |
| `test_a_finding_in_an_untouched_worktree_does_not_block` | naar Go | elke worktree gelint |
| `test_every_checkout_of_the_turn_is_linted` | naar Go | alleen de eerste root gelint |
| `test_without_a_transcript_could_not_check` | naar Go | ontbrekend `transcript_path` valt terug op `cwd` |
| `test_registered_on_stop` | naar Go | matcher of timeout 120 in `pack/go/hooks/hooks.json` gewijzigd (via settings.json, taak 3) |
| `test_valid_usage_alarm_skips_the_lint` | naar Go | alarmtak weg |
| `test_no_usage_alarm_still_blocks` | weg — gedekt | door de port van `test_finding_blocks` (zelfde opzet zonder alarm; "altijd skippen" faalt daar) |
| `test_expired_usage_alarm_still_blocks` | weg — gedekt | verloop beslist de binary: `internal/usage/alarm_test.go` `TestReadAlarmFixtures`; uitvoerder bevestigt met mutatie in `internal/usage` |
| `test_unreadable_usage_alarm_still_blocks` (2: 1) | weg — gedekt | idem, `TestReadAlarmFixtures` (drie velden, symlink); bevestigen |
| `test_without_lighthouse_a_valid_alarm_could_not_check` | naar Go | `command -v lighthouse`-tak weg |
| `test_alarm_is_read_with_lighthouse_usage_alarm` | naar Go | hook leest het alarmbestand zelf |
| `test_help_text_from_an_old_lighthouse_is_no_alarm` | naar Go | elke uitvoer met rc 0 als alarm gelezen |
| `test_stop_hook_active_with_alarm_is_silent` | naar Go | alarmcheck vóór de `stop_hook_active`-check |
| `test_a_turn_without_go_paths_skips_and_says_so` | naar Go | padfilter weg |
| `test_a_go_path_lints` (5 paden: 1) | naar Go | één patroon uit het padfilter |
| `test_a_skipped_root_needs_no_go_tooling` | naar Go | toolpreflight vóór het padfilter |
| `test_an_untouched_root_is_named_next_to_a_finding` | naar Go | skipmelding weg naast een finding |

### `tests/pack/go/test_gofmt.py` (12) — hook `pack/go/hooks/gofmt-check.sh`

Geen Go-test draait deze hook. Bestemming: `e2e/gofmt_test.go`. Alle 12 naar Go.

| Test | Mutatie |
|---|---|
| `test_formats_a_go_file` | `gofmt -w` zonder `-w` |
| `test_blocks_a_file_that_does_not_parse` | rc 2 van gofmt wordt rc 0 |
| `test_other_extension_passes` | extensiefilter weg |
| `test_missing_file_passes` | `[ -f ]`-check weg |
| `test_no_path_passes` | lege `file_path` wordt een fout |
| `test_classify_reads_the_parse_error_as_a_finding` | eerste regel zonder `gofmt-check: ` (verdict wordt `unchecked`) |
| `test_without_gofmt_could_not_check` | gofmt-preflight weg |
| `test_without_gofmt_other_extension_passes` | preflight vóór het extensiefilter |
| `test_without_jq_could_not_check` | jq-preflight weg |
| `test_empty_payload_could_not_check` | lege stdin als "geen pad" gelezen |
| `test_invalid_json_could_not_check` | jq-fout genegeerd |
| `test_registered_on_edit_write` | matcher `Edit\|Write` of timeout 15 gewijzigd (taak 3) |

### `tests/pack/go/test_go_writing.py` (20) — skill `pack/go/skills/go-standard`

| Test | Bestemming | Mutatie / reden |
|---|---|---|
| `test_frontmatter_name` | weg — gedekt | generieke frontmatter-check (#367-port van `test_skill_contracts.py::test_frontmatter_name`) |
| `test_config_keys_are_known` | weg — gedekt | generiek, `::test_config_keys_known` (#367) |
| `test_english` | weg — gedekt | `internal/codelang/corpus_test.go` `TestCorpusPackMarkdownEnglish` (scant elke `.md` onder `pack/`) |
| `test_sections_in_order` | weg — beschermt niets | volgorde van koppen stuurt geen gedrag |
| `test_go_form_of_the_testing_norms` | weg — beschermt niets | pint een lijst gereedschapsnamen; elke herformulering breekt hem |
| `test_no_concept_leftovers` | weg — beschermt niets | "staat nog steeds niet in de tekst" na de migratie uit #22 |
| `test_variants_keep_their_difference` | **open** | dekt check 11 van `lighthouse check-instructions` (`internal/checkinstr/mirror_test.go`) dit voor `pack/go`? Ja: weg — gedekt. Nee: naar Go |
| `test_description_has_a_negative_boundary` | naar Go (contract) | grens naar python-/shell-standard weg uit de description |
| `test_description_triggers_on_planning` | naar Go (contract) | trigger "before a file is open" weg |
| `test_kpi_rows_all_carry_a_command` | naar Go (contract) | KPI-rij zonder commando |
| `test_test_row_shuffles` | naar Go (contract) | `-shuffle=on` weg of `-count=1` erbij |
| `test_selector_unchecked_exit_code` | naar Go (contract) | exit 3 niet in `unchecked_exit_codes` |
| `test_gate_command_uses_the_selector_without_shuffle` | naar Go (contract) | gate-commando met `-shuffle=on` |
| `test_kpi_rows` | naar Go (contract) | één KPI-commando weg |
| `test_points_to_the_testing_skill` | naar Go (contract) | eigen klassentabel in de skill |
| `test_reporting_done` | naar Go (contract) | "A row left out of the report reads as passed." weg |
| `test_names_the_environment` | naar Go (contract) | GOOS/-race/GOWORK weg uit *Non-negotiable* |
| `test_pairing_go_lint_gate_row` | naar Go (contract) | pairing noemt `go vet` naast de hook |
| `test_vet_row_caveat_with_new_from_rev` | naar Go (contract) | voorbehoud bij `issues.new-from-rev` weg |
| `test_opencode_names_the_hook_commands` | naar Go (contract) | OpenCode-noot zonder de handcommando's |

### `tests/pack/go/test_go_pack.py` (3)

| Test | Bestemming | Mutatie / reden |
|---|---|---|
| `test_detect_on_go_mod` | naar Go | nieuwe `TestInitProposesRealGo` (taak 2); mutatie: `files = ["go.work"]` in `pack/go/detect.toml` |
| `test_no_fragment` | weg — gedekt | `internal/packs/core_test.go` `TestLanguagePacks/go` (`fragment` leeg) |
| `test_instruction_block_is_short_and_names_skill_and_gates` | naar Go (contract) | blok noemt skill, `gofmt`, `go-lint-gate`, `govet`, geen `go vet`. Het woordplafond is gedekt door `internal/packs/budget_test.go` `TestInstructionBlocksWithinCeiling` (go 50 < 80) |

### `tests/pack/node/test_node_pack.py` (2)

| Test | Bestemming | Reden |
|---|---|---|
| `test_detect_on_package_json` | weg — gedekt | `internal/initcmd/initcmd_test.go` `TestInitProposesRealExpo` verwacht `node (found apps/a/package.json)`; `files = ["package.json"]` laat hem falen |
| `test_no_fragment_hooks_or_instructions` | weg — gedekt | `TestLanguagePacks/node` (geen bestanden, 0 blokken, 0 hooks, geen fragment) |

### `tests/pack/expo/test_refresh_expo_lockfiles.py` (11) — skill `pack/expo/skills/refresh-expo-lockfiles`

| Test | Bestemming | Mutatie / reden |
|---|---|---|
| `test_frontmatter_name` | weg — gedekt | generiek (#367) |
| `test_skill_links_are_lighthouse_or_external` | weg — gedekt | generiek `::test_skill_links_resolve` (#367) |
| `test_english` | weg — gedekt | `TestCorpusPackMarkdownEnglish` |
| `test_finds_apps_itself` | weg — beschermt niets / gedekt | paia-appnamen: negative existence na migratie; de positieve helft is gedekt door de port van `test_app_loop_finds_only_apps_with_a_lockfile` |
| `test_no_paia_measurements` | weg — beschermt niets | negative existence na migratie |
| `test_optional_refs_guarded` | naar Go (contract) | "if present" weg bij `dependabot.yml` |
| `test_write_issue_link` | naar Go (contract) | prefix hard als `2-area:` in plaats van `lighthouse config labels.area_prefix` |
| `test_checks_node_and_npm` | naar Go (contract) | `command -v npm` weg |
| `test_diff_checked_before_commit` | naar Go (contract) | diff-check na de commit-stap |
| `test_cd_in_subshell` | naar Go (contract) | `cd` buiten een subshell in een bash-blok |
| `test_app_loop_finds_only_apps_with_a_lockfile` | naar Go (contract, voert uit) | lockfile-voorwaarde weg uit de lus in de skill |

### `tests/pack/shell/test_shell_gates.py` (18) — skill `shell-standard` en het instructieblok

| Test | Bestemming | Mutatie / reden |
|---|---|---|
| `test_frontmatter_name` | weg — gedekt | generiek (#367) |
| `test_paths_are_the_installed_ones` | weg — gedekt | generiek `::test_referenced_files_exist` (#367) |
| `test_english` | weg — gedekt | `TestCorpusPackMarkdownEnglish` |
| `test_skill_links_are_lighthouse` | weg — gedekt | generiek `::test_skill_links_resolve` (#367) |
| `test_no_paia_references` | weg — beschermt niets | negative existence na migratie |
| `test_shell_form_of_the_testing_norms` | weg — beschermt niets | pint gereedschapsnamen |
| `test_pairing_names_the_testing_skill` | weg — beschermt niets | pint een vermelding; `test_points_to_the_testing_skill` draagt de regel |
| `test_variants_keep_their_difference` | **open** | als bij go: check 11 |
| `test_strict_dirs_from_config` | naar Go (contract) | skill noemt een vaste map in plaats van `lighthouse config shell.strict_dirs` |
| `test_instruction_block` | naar Go (contract) | `COULD NOT CHECK` of de skillnaam weg; plafond gedekt (shell 54 < 60) |
| `test_outcomes_match_the_hook` | naar Go (contract) | skill noemt andere exitcodes dan de hook |
| `test_root_advice_matches_the_hook` | naar Go (contract) | advies over `$CLAUDE_PROJECT_DIR` weg |
| `test_points_to_the_testing_skill` | naar Go (contract) | eigen klassentabel |
| `test_detect_rule` | naar Go | `TestInitProposesRealShell` dekt `**/*.sh`; taak 2 voegt `x.bash` toe (mutatie: `**/*.bash` weg) |
| `test_kpi_commands_run` | naar Go (e2e) | KPI-commando dat niet draait of zijn rc mist |
| `test_kpi_rows_go_red` | naar Go (e2e) | KPI-rij die groen blijft op kapotte invoer |
| `test_kpi_gitignore_row_is_not_green_on_broken_config` | naar Go (e2e) | configfout gelezen als "niets genegeerd" |
| `test_kpi_gitignore_row_handles_a_space` | naar Go (e2e) | spatie in `strict_dirs` breekt de rij |

### `tests/pack/shell/test_shellcheck.py` (28) — hook `shell-lint-check.sh`, `shellcheck-repo.sh`, `lib/shellcheck-bin.sh`

Geen Go-test draait deze scripts (`internal/bashwrites/post_test.go` gebruikt alleen een stub). Bestemming: `e2e/shellcheck_test.go`.

| Test | Bestemming | Mutatie / reden |
|---|---|---|
| `test_pin_and_hashes_unchanged` | weg — beschermt niets | spiegel van letterlijke constanten uit de bron; elke bewuste bump wijzigt beide |
| `test_strict_dirs_from_lighthouse_config` | weg — gedekt (kandidaat) | door de port van `test_strict_dirs_from_config`; bevestigen met de mutatie "strict set uit een vaste lijst" |
| overige 26 | naar Go (e2e) | per test de branch uit zijn docstring; o.a. `test_shellcheck_hook_blocks_a_deliberately_broken_script` (exit 2 wordt 0), `test_strict_dirs_does_not_match_a_prefix` (pathspec als prefix), `test_download_verifies_with_shasum_when_sha256sum_is_missing` (alleen `sha256sum`), `test_resolve_prefers_the_pinned_cache_over_a_divergent_path_version` (PATH vóór cache), `test_repo_script_shellcheck_failure_is_rc_4` (rc 2–4 als finding), `test_bash_write_through_gate` (`bash-write-check post` vindt de shell-gate niet) |

---

## Bestanden

- Verplaatsen: `tests/pack/fixtures/code_language/**` naar `internal/codelang/testdata/` (taak 1).
- Wijzigen: `internal/codelang/score_test.go:14`, `internal/codelang/corpus_test.go:69-74`, `internal/codelang/llm_test.go:225`, `lighthouse.toml:5` (taak 1).
- Wijzigen: `internal/initcmd/initcmd_test.go` (taak 2).
- Nieuw: `e2e/hookkit_test.go`, `e2e/gofmt_test.go`, `e2e/golintgate_test.go`, `e2e/shellcheck_test.go`, `e2e/shellkpi_test.go` (taken 3–5).
- Wijzigen: het contractpakket van #367 (taak 6).
- Verwijderen: `tests/pack/{go,shell,node,expo}/` (taken 2–6), daarna de restbestanden (taak 7).
- Nieuw: `changelog.d/issue-368-taalpakket-tests.md` (taak 7).

---

### Task 1: Fixtures van de taaldetectie naar `internal/codelang/testdata/`

**Files:**
- Move: `tests/pack/fixtures/code_language/` naar `internal/codelang/testdata/`
- Rename: elk `*.py` daarin naar `*.py.txt` (zelfde patroon als `corpus/hook.go.txt`); `stdlib/LICENSE-PSF.txt` gaat mee
- Rename: `units/tests__pack__fixtures__code_language__<x>.json` naar `units/internal__codelang__testdata__<x>.json`, met `.py` in `<x>` als `.py.txt`
- Modify: `internal/codelang/score_test.go:14`, `internal/codelang/corpus_test.go:69-74`, `internal/codelang/llm_test.go:225`, `lighthouse.toml:5`

**Interfaces:**
- Produces: `const fixtureDir = "testdata"` in `internal/codelang/score_test.go`; het `source`-veld van elke unitfixture is `internal/codelang/testdata/<pad>` (repo-relatief, `TestUnitsFixture` leest het via `repoRoot`).

Classificatie: `test-only`. Mutatie: geen productiecode; het bewijs is dat dezelfde tests met hetzelfde aantal subtests groen draaien.

- [ ] **Step 1: Tel de subtests vóór de verhuizing**

Run: `go test ./internal/codelang/ -run 'TestUnitsFixture|TestCorpusStdlibClean|TestScoreFixture' -v 2>&1 | grep -c '^    --- PASS'`
Noteer het getal (verwacht: 14 unitbestanden plus de overige subtests).

- [ ] **Step 2: Verplaats en hernoem met `git mv`**

`git mv tests/pack/fixtures/code_language internal/codelang/testdata`, dan per `.py` een `git mv x.py x.py.txt`, en per unitbestand een `git mv` naar de nieuwe naam. Zet in elk unitbestand `source` op het nieuwe pad (`jq` of `sed` op de ene regel `"source": ...`).

- [ ] **Step 3: Pas de lezers aan**

`fixtureDir = "testdata"`. In `TestCorpusStdlibClean` de glob `stdlib/*.py.txt`; `want 10` blijft. Commentaar in `llm_test.go:225` naar `testdata/llm.json`. In `lighthouse.toml` `code_language_exempt` het pad `internal/codelang/testdata/**` in plaats van `tests/pack/fixtures/code_language/**`.

- [ ] **Step 4: Draai en vergelijk**

Run: `go test ./internal/codelang/ -run 'TestUnitsFixture|TestCorpusStdlibClean|TestScoreFixture' -v 2>&1 | grep -c '^    --- PASS'`
Expected: hetzelfde getal als stap 1, en `ok`.
Run: `git ls-files '*.py' | grep -c codelang` — Expected: `0`.
Run: `go test -race ./internal/codelang/ ./internal/hooks/ ./internal/cli/` — Expected: `ok` (de code-language-hook leest `lighthouse.toml`).

- [ ] **Step 5: Commit**

`git commit -m "test(codelang): fixtures van de taaldetectie naar testdata/ (#368)"`

---

### Task 2: Detectie van de echte pakketten go en shell

**Files:**
- Modify: `internal/initcmd/initcmd_test.go` (naast `TestInitProposesRealShell`, regel ~1020)
- Delete: `tests/pack/go/test_go_pack.py::test_detect_on_go_mod`, `::test_no_fragment`; `tests/pack/node/` (hele map); `tests/pack/shell/test_shell_gates.py::test_detect_rule`

Classificatie: `config`. Op het spel: welk pakket `init` voorstelt bij welk bestand.

- [ ] **Step 1: Schrijf `TestInitProposesRealGo`**

Naar het patroon van `TestInitProposesRealExpo`: `write(t, root, "go.mod", "module example.com/m\n\ngo 1.26\n")`, `o.Yes = true; o.Packs = nil`, verwacht prefix `"Packages: core, go (found go.mod)\n"` en dat `.claude/hooks/lighthouse/go-lint-gate.sh` bestaat.

- [ ] **Step 2: Breid `TestInitProposesRealShell` uit met `.bash`**

Een tweede geval met alleen `x.bash`: verwacht prefix `"Packages: core, shell (found x.bash)\n"`. Houd het bestaande `x.sh`-geval.

- [ ] **Step 3: Draai**

Run: `go test ./internal/initcmd/ -run 'TestInitProposesReal(Go|Shell|Expo)' -v` — Expected: PASS (gedrag bestaat al).

- [ ] **Step 4: Mutatiecheck (niet committen)**

Zet in `pack/go/detect.toml` `files = ["go.work"]`: `TestInitProposesRealGo` FAIL. Haal `"**/*.bash"` uit `pack/shell/detect.toml`: het `.bash`-geval FAIL. Zet in `pack/node/detect.toml` `files = ["package.json"]`: `TestInitProposesRealExpo` FAIL (bewijs voor *gedekt* bij node). Draai na elke mutatie `git checkout -- pack/`.

- [ ] **Step 5: Verwijder de pytest-tegenhangers en commit**

Verwijder de drie functies en `tests/pack/node/`. Run: `uv run pytest -q tests/pack/go tests/pack/shell` — Expected: groen.
`git commit -m "test(initcmd): detectie van go en shell op het echte pakket, pytest-tegenhangers weg (#368)"`

---

### Task 3: e2e-harnas voor pakkethooks, en `gofmt-check`

**Files:**
- Create: `e2e/hookkit_test.go`, `e2e/gofmt_test.go`
- Delete: `tests/pack/go/test_gofmt.py`

**Interfaces:**
- Consumes: `buildLighthouse`, `freshRepo`, `git`, `writeFile`, `hookEnv`, `cleanEnv` uit `e2e/e2e_test.go`.
- Produces (in `e2e/hookkit_test.go`, build tag `e2e`):
  - `packRepo(t *testing.T, bin string, trigger map[string]string, wantPackages string) string` — `freshRepo`, schrijft de triggerbestanden, `lighthouse init --yes`, faalt als stdout niet met `wantPackages` begint.
  - `registeredHook(t *testing.T, repo, event, matcher, script string) (command string, timeout int)` — leest `.claude/settings.json`, precies één hook onder `event`/`matcher` waarvan het commando eindigt op `script`; anders `t.Fatal`.
  - `toolDir(t *testing.T, tools []string, drop string, stubs map[string]string) string` — map met symlinks naar de echte tools (min `drop`) plus uitvoerbare stubs; Go-tegenhanger van `_testkit.tool_bin`.
  - `runHook(t *testing.T, repo, command string, payload any, env []string) (code int, stdout, stderr string)` — `bash -c command`, `CLAUDE_PROJECT_DIR=repo`, payload als JSON op stdin (een `string` gaat ongewijzigd), `env` overschrijft `hookEnv(bin)`.
  - `needTool(t *testing.T, name string)` — patroon `needUV`: in CI `t.Fatal`, lokaal `t.Skip` met de reden.

Classificatie: `hook/gate`. Op het spel: wat `gofmt-check.sh` blokkeert of doorlaat.

Als #366 eerder merget en al zulke helpers in `e2e/` heeft, gebruik die en maak geen tweede set; pas alleen de ontbrekende aan.

- [ ] **Step 1: Schrijf `TestGoHooksRegistered`**

`packRepo` met `go.mod`, dan `registeredHook(t, repo, "PostToolUse", "Edit|Write", "gofmt-check.sh")` geeft timeout `15`, en `registeredHook(t, repo, "Stop", "", "go-lint-gate.sh")` geeft timeout `120`. Commando's beginnen met `"$CLAUDE_PROJECT_DIR"/.claude/hooks/lighthouse/`.

- [ ] **Step 2: Draai, mutatiecheck**

Run: `go test -tags e2e ./e2e/ -run TestGoHooksRegistered -v` — Expected: PASS. Zet de timeout in `pack/go/hooks/hooks.json` op 16: FAIL. Terugzetten.

- [ ] **Step 3: Port de 11 gedragstests van `test_gofmt.py`**

Eén `TestGofmtCheck` met subtests, namen gelijk aan de pytest-namen zonder `test_`. Stub `gofmt` en verwachtingen letterlijk uit `test_gofmt.py` (`STUB`, `PARSE_ERROR`, `UNCHECKED = "gofmt-check: COULD NOT CHECK — "`, eerste regel `gofmt-check: <pad> does not parse — fix the syntax.`). Tools: `bash`, `jq`, `cat`. Het classify-geval gebruikt de echte `gofmt` (`needTool`) en het verdict van `bashwrites.Classify(rc int, output []byte) string` (`internal/bashwrites/gates.go:70`), de Go-tegenhanger van `_testkit.classify`: verwacht `"finding"`, en `"unchecked"` bij de twee preflight-gevallen.

- [ ] **Step 4: Draai en mutatiecheck**

Run: `go test -race -tags e2e ./e2e/ -run 'TestGofmtCheck' -v` — Expected: PASS. Mutaties uit de tabel (minstens: `-w` weg, jq-preflight weg, extensiefilter weg): elk FAIL. Terugzetten.

- [ ] **Step 5: Verwijder `tests/pack/go/test_gofmt.py` en commit**

`git commit -m "test(e2e): gofmt-check als black box via de registratie, pytest-versie weg (#368)"`

---

### Task 4: `go-lint-gate` als black box

**Files:**
- Create: `e2e/golintgate_test.go`
- Delete: `tests/pack/go/test_go_lint_gate.py`

**Interfaces:**
- Consumes: alles uit taak 3.
- Produces (in `e2e/golintgate_test.go` of `hookkit_test.go` als #366 ze ook nodig heeft): `writeTranscript(t *testing.T, path string, entries ...map[string]any) string`, `promptEntry(cwd string, ts string) map[string]any`, `toolEntry(name string, input map[string]any, cwd, ts string) map[string]any`, `resultEntry(cwd, ts string) map[string]any`, `stopPayload(cwd, transcript string, active bool) map[string]any`. Formaat letterlijk uit `tests/pack/_testkit.py:329-385`.

Classificatie: `hook/gate`. Op het spel: wanneer de Stop-hook blokkeert, niet controleert of zwijgt.

- [ ] **Step 1: Port de 26 tests met bestemming *naar Go***

Eén `TestGoLintGate` met subtests. Stubs `go` en `golangci-lint` letterlijk uit `test_go_lint_gate.py` (`GO_STUB`, `LINT_STUB`, `VET_FINDING`, `BUILD_FAILURE`, `LINT_FINDING`), meldingen letterlijk (`UNCHECKED`, `NOT_CHECKED`, de twee regels van een finding, de skipmelding, de alarmmelding met `14 30` en `9 15`). `lighthouse` is de gebouwde binary; het alarm gaat via `XDG_CACHE_HOME` in `t.TempDir()`. Tools: `bash jq git cat tr head sed lighthouse mktemp rm`.

- [ ] **Step 2: Draai en mutatiecheck**

Run: `go test -race -tags e2e ./e2e/ -run TestGoLintGate -v` — Expected: PASS. Mutaties: `exit 2` na finding naar `exit 0`; `lighthouse turn-roots` vervangen door de `cwd`; de `stop_hook_active`-check weg. Elk FAIL. Terugzetten.

- [ ] **Step 3: Bevestig de drie *gedekt*-rijen**

Laat in `internal/usage` het verloop van een alarm negeren: `TestReadAlarmFixtures` FAIL. Lukt dat niet, dan gaan `test_expired_usage_alarm_still_blocks` en `test_unreadable_usage_alarm_still_blocks` alsnog naar Go in deze taak.

- [ ] **Step 4: Verwijder `test_go_lint_gate.py` en commit**

`git commit -m "test(e2e): go-lint-gate als black box, pytest-versie weg (#368)"`

---

### Task 5: `shell-lint-check`, `shellcheck-repo.sh`, de lib en de KPI-commando's

**Files:**
- Create: `e2e/shellcheck_test.go`, `e2e/shellkpi_test.go`
- Delete: `tests/pack/shell/test_shellcheck.py`; uit `tests/pack/shell/test_shell_gates.py` de vier KPI-tests (`test_kpi_commands_run`, `test_kpi_rows_go_red`, `test_kpi_gitignore_row_is_not_green_on_broken_config`, `test_kpi_gitignore_row_handles_a_space`)

**Interfaces:**
- Consumes: taak 3.
- Produces: `pinnedShellcheck(t *testing.T) string` — eenmalig (`sync.Once`) `. lib/shellcheck-bin.sh; resolve_shellcheck` in de checkout; rc ≠ 0 of stderr niet leeg is `t.Fatal`, geen skip (zoals `pinned_shellcheck` in `tests/pack/shell/conftest.py`). `kpiRows(t *testing.T, skill string) [][2]string` — (commando, doel) uit de sectie `## KPIs`, parse als `_kpi_rows` in `test_shell_gates.py:110-118`.

Classificatie: `hook/gate` en `cli-output`. Op het spel: wat de shellgate blokkeert, en dat elk KPI-commando uit de skill draait en rood wordt op kapotte invoer.

- [ ] **Step 1: Port de 26 tests van `test_shellcheck.py` met bestemming *naar Go***

`TestShellLintCheck`, `TestShellcheckRepo` en `TestShellcheckBin` met subtests. Scripts letterlijk uit de pytest-versie (`CLEAN_SH`, `BROKEN_SH`, de stubs `_fake_shellcheck`, `_failing_curl`, `_crashing_shellcheck`). `test_bash_write_through_gate`: draai de geregistreerde `run.sh bash-write-check` pre en post (`eventHookCommand` voor `PreToolUse` en `PostToolUse`, `XDG_RUNTIME_DIR` naast de repo) met een heredoc-payload die `x.sh` schrijft; verwacht rc 2, `SC2034`, niet `no shell gate installed`. Gebruik een helper van #366 als die er is.

- [ ] **Step 2: Port de vier KPI-tests**

`TestShellKPI` met subtests. De skilltekst komt uit het geïnstalleerde `.claude/skills/lighthouse-shell-standard/SKILL.md` van de repo die `packRepo` maakt, niet uit `pack/`.

- [ ] **Step 3: Draai en mutatiecheck**

Run: `go test -race -tags e2e ./e2e/ -run 'TestShell' -v` — Expected: PASS. Mutaties: in de hook `exit 2` na een finding naar `exit 0`; in `shellcheck-repo.sh` de severity-switch voor `strict_dirs` weg; in de lib de `shasum`-fallback weg. Elk FAIL. Bevestig ook de *gedekt*-kandidaat: haal in de hook de `lighthouse config shell.strict_dirs`-aanroep weg; de port van `test_strict_dirs_from_config` moet FAIL geven. Terugzetten.

- [ ] **Step 4: Verwijder de pytest-tegenhangers en commit**

Run: `uv run pytest -q tests/pack/shell` — Expected: groen (alleen de contracttests resten).
`git commit -m "test(e2e): shellgate, shellcheck-repo, lib en KPI-commando's als black box, pytest-versies weg (#368)"`

---

### Task 6: Contractpins naar het pakket van #367 (na de merge van #367)

**Files:**
- Modify: het contractpakket van #367 (pad en helpers uit die PR; lees zijn README of pakketcommentaar)
- Delete: `tests/pack/go/test_go_writing.py`, `tests/pack/go/test_go_pack.py`, `tests/pack/shell/test_shell_gates.py`, `tests/pack/expo/test_refresh_expo_lockfiles.py`

**Interfaces:**
- Consumes: de lees-helper van #367 voor `pack/<pakket>/skills/<skill>/{claude,agents}/SKILL.md` en voor een gedeelde `SKILL.md`.

Classificatie: `test-only` (contracttests op instructietekst; lezen is hier geen rode vlag).

- [ ] **Step 1: Merge `origin/main`**

`git fetch origin main && git merge origin/main`. Controleer dat de generieke checks waarop de *gedekt*-rijen leunen (frontmatter-naam, verwijzingen, configsleutels, skill-links) in Go bestaan. Ontbreekt er één, dan wordt die rij hier *naar Go*.

- [ ] **Step 2: Beslis de twee open rijen**

Breek in `pack/go/skills/go-standard/agents/SKILL.md` één alinea die niet over OpenCode gaat, en draai `go run ./cmd/lighthouse init --upgrade` gevolgd door `go run ./cmd/lighthouse check-instructions`. Rood op check 11: beide `test_variants_keep_their_difference` zijn *weg — gedekt*. Groen: port ze als pin. Terugzetten.

- [ ] **Step 3: Port de pins**

Eén testfunctie per rij *naar Go (contract)* uit de tabel, per variant (`claude`, `agents`) waar de pytest-versie dat deed. Letterlijke strings uit de pytest-versie. `test_app_loop_finds_only_apps_with_a_lockfile` voert het bash-blok uit de skill uit in een `t.TempDir()`-repo met de zes bestanden uit de pytest-versie; verwacht `["apps/a"]`. Elke functie krijgt een commentaar met de invariant die hij bewaakt.

- [ ] **Step 4: Draai, mutatiecheck en cache**

Run: `go test ./<contractpakket>/` twee keer; de tweede keer `(cached)`. Breek één geporte zin in `pack/go/skills/go-standard/claude/SKILL.md` en draai opnieuw: FAIL, niet `(cached)`. Doe één mutatie per geporte pin (kolom *Mutatie*). Terugzetten.

- [ ] **Step 5: Verwijder de vier pytest-bestanden en commit**

`git commit -m "test(contract): pins van go-, shell- en expo-skill naar Go, pytest-versies weg (#368)"`

---

### Task 7: Opruimen, changelog en eindbewijs (na de merge van #366 en #367)

**Files:**
- Delete: `tests/pack/{go,shell,expo}/` (`conftest.py`, `__init__.py`, de mappen)
- Delete, als er geen importer meer is: `tests/pack/conftest.py`, `tests/pack/__init__.py`, `tests/pack/_fake_llm.py`, `tests/pack/_fakegh.py`
- Create: `changelog.d/issue-368-taalpakket-tests.md`

- [ ] **Step 1: Merge `origin/main` en zoek importers**

`git fetch origin main && git merge origin/main`.
Run: `git grep -n 'tests.pack.conftest\|tests\.pack\._fake_llm\|tests\.pack\._fakegh\|from tests\.pack import' -- '*.py'`. Verwijder alleen wat geen treffer meer heeft. Een treffer onder `tests/pack/python/`: verhuis het bestand naar `tests/pack/python/` en pas de import aan, zoals #366 met `_testkit.py` deed.

- [ ] **Step 2: Schrijf het changelog-fragment**

```markdown
---
type: internal
---
De tests van de taalpakketten go, shell, node en expo zijn gesorteerd: wat iets bewijst draait in Go (`e2e/` en het contractpakket), de rest is weg. De testdata van de taaldetectie staat in `internal/codelang/testdata/`.
```

- [ ] **Step 3: Eindbewijs**

Run: `git ls-files 'tests/pack/**/*.py' | grep -v '^tests/pack/python/'` — Expected: geen uitvoer.
Run: `git ls-files '*.py' | grep -v -e '^pack/python/' -e '^tests/pack/python/' -e '^tests/[^/]*\.py$' -e '^ci/' -e '^tools/'` — Expected: geen uitvoer (root-tests, `ci/` en `tools/` zijn van #361–#365).
Run: `go test -race -shuffle=on ./...` — Expected: `ok` voor elk pakket.
Run: `go test -race -shuffle=on -tags e2e ./e2e/ -v` — Expected: PASS.
Run: `uv run pytest -q tests/pack/python` — Expected: groen.

- [ ] **Step 4: Commit**

`git commit -m "test(pack): restbestanden van tests/pack weg, changelog-fragment (#368)"`

De PR-body (via `/lighthouse-write-pr`) bevat de definitieve triagetabel met per *naar Go* de mutatie en per *gedekt* de Go-test.
