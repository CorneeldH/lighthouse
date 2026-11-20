# Triage van de root-tests in `tests/`: weg of naar Go — implementatieplan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Doel:** Elk van de twaalf genoemde root-testbestanden en hun helpers krijgt per testfunctie een
bestemming; wat de kernvraag beantwoordt en nergens anders gedekt is, wordt een Go-test, de rest
gaat weg, en de `.py`-bestanden verdwijnen.

**Architectuur:** Eén nieuw Go-pakket `internal/repopins` met alleen `_test.go`-bestanden, dat
bestanden van de repo leest (workflows, `AGENTS.md`, `pack/SOURCES.toml`, `docs/`) via een
helper die omhoog loopt tot `go.mod`. Tests die een proces draaien (de geregistreerde hook, het
script `ci/literal-grep.sh`) gaan naar `e2e/` (build tag `e2e`, de `go-e2e`-job, spec §3). Per
taak: eerst de Go-test, dan de mutatie die hem rood maakt (genoteerd voor de PR-body), dan het
`.py`-bestand weg, in één commit.

**Tech stack:** Go 1.26.4 (stdlib: `regexp`, `os`, `path/filepath`, `os/exec`; `BurntSushi/toml`
of wat `go.mod` al heeft, zie Taak 3), GitHub Actions-YAML als tekst.

**Spec:** [`2026-11-16-repo-zonder-python-design.md`](../specs/2026-11-16-repo-zonder-python-design.md)
(§1 triage, §2 contracten, §3 black box, §7 CI). Issue: #365.

## Globale randvoorwaarden

- Code, comments en testnamen Engels; commits, PR-body en fragment Nederlands.
- Go volgt `/lighthouse-go-standard`; tests volgen `/lighthouse-test-standard`. Vóór elke
  Go-test de classificatiezin: *welke productiewijziging laat deze test falen?* Een test zonder
  antwoord gaat niet naar Go.
- Geen ander gedrag van productiecode, workflows of docs. Alleen testbestanden, hun helpers en
  verwijzingen daarnaar veranderen. Twee uitzonderingen, beide bewust: `ci/domains.py` met
  `tests/test_domains.py` (Taak 1, stap 2a: de `docs/`- en `ci/literal-*`-bestanden die Go-tests
  nu lezen, horen bij het `go`-domein) en de verwijzing naar `tests/test_sources.py` in de
  commentaarregel van `pack/SOURCES.toml` (Taak 3, stap 5a).
- `tests/test_domains.py` blijft draaien: het faalt op elk git-tracked `.go`-bestand dat
  `docs/`, `changelog.d` of `ci/literal-` noemt, buiten de vaste uitzonderingen. Draai het in
  elke taak (`uv run pytest -q tests/test_domains.py`), naast de andere verificaties.
- `tests/pack/python/` blijft Python. `tests/test_site_*.py`, `tests/_site_node.py`,
  `tests/test_domains.py`, `tests/_domains.py`, `tests/test_check_cla.py`,
  `tests/test_checkpoint_scan.py`, `tests/test_loop_roadmap.py`,
  `tests/test_gen_code_language_freq.py`, `tests/test_pre_push_secret_scan.py`,
  `tests/test_tag_with_ui.py`, `tests/test_no_python_script.py` en `tests/pack/**` zijn van
  andere issues van milestone #11 en blijven staan.
- Een test die na dit issue zowel in pytest als in Go bestaat, telt als mislukt: port en
  verwijdering staan in dezelfde commit.
- Een Go-test die een mutatie niet rood maakt, is geen port: herschrijf hem of laat hem weg.

## Review focus

- `tests/test_agent_guide.py` importeert helpers uit `tests/test_site_loop.py`,
  `tests/test_site_download.py` en `tests/test_site_pages.py` (taak van stap 10). Na de port mag
  geen van die bestanden iets uit de verwijderde bestanden importeren: `grep` vóór de
  verwijdering (Taak 4, 6).
- De Go-leestests moeten ook draaien als `go test ./internal/repopins` vanuit een andere map
  loopt dan de repo-root; de helper zoekt `go.mod`, geen vast `../..`.
- Een workflow die wordt hernoemd of verwijderd mag een pin niet stil groen laten: elke
  workflowtest leest het bestand met `os.ReadFile` en faalt op een ontbrekend bestand.
- `lighthouse.toml` `[test_gate] command` bevat het pytest-deel nog; de pin op de Go-vlaggen
  (Taak 1) moet blijven werken als dat deel verdwijnt (stap 11).

## Bestandsstructuur

| Bestand | Rol |
|---|---|
| `internal/repopins/repo_test.go` | Helpers: `repoRoot(t)`, `read(t, rel...)`, `jobs(text)`, `runBlocks(text)`. |
| `internal/repopins/workflows_test.go` | Pins op `ci.yml`, `macos.yml`, `pages.yml`, de golangci-versie. |
| `internal/repopins/release_test.go` | Pins op `release.yml`, `build.yml`, `prerelease.yml`. |
| `internal/repopins/instructions_test.go` | `AGENTS.md`, `CLAUDE.md`, het core-blok. |
| `internal/repopins/sources_test.go` | `pack/SOURCES.toml` tegen de bestanden onder `pack/`. |
| `internal/repopins/docs_test.go` | `docs/getting-started.md` en `docs/site/agents/install.md` tegen de code. |
| `e2e/gettingstarted_test.go` | `TestConfigExampleLoads` (Taak 4). |
| `e2e/dogfood_test.go` | De geregistreerde hooks van `.claude/settings.json` als black box. |
| `e2e/literalgrep_test.go` | `ci/literal-grep.sh` als black box. |
| `tests/pack/conftest.py`, `tests/pack/_network.py`, `tests/pack/test_network.py` | Verhuisd uit `tests/` (Taak 6). |

## Triagetabel (voorstel; de bouwer bevestigt elke regel met de mutatie)

De cijfers zijn `grep -c '^def test_'` plus parametrisaties op de huidige `main`; de bouwer telt
opnieuw voor de PR-body.

| Bestand | Bestemming | Reden in één zin |
|---|---|---|
| `test_golangci_pin.py` (1) | naar Go | `.golangci-version` en de lint-action in `ci.yml` driften anders stil uit elkaar; niets anders vergelijkt ze. |
| `test_ci_go_test_pin.py` (5) | naar Go: `every_ci_go_test_has_gate_flags`, `every_macos_go_test_has_gate_flags`, `ci_cross_builds_release_targets`, `ci_builds_without_cgo` | Elke test faalt op het weghalen van één vlag of stap in een workflow; CI zou het pas bij de release merken. |
| `test_pages_workflow.py` (5) | naar Go: `publishes_the_site_folder`, `runs_on_site_changes_on_main`, `permissions_for_pages`; weg — beschermt niets: `job_has_a_runner`; weg — gedekt: `actions_are_pinned` (door de nieuwe `TestEveryWorkflowPinsActionsBySHA`) | Een deploy die stil niet meer publiceert merkt niemand; een `runs-on`-regel is een tautologie. |
| `test_release_workflows_pin.py` (~30) | per functie, zie Taak 2; criterium: de test noemt een mutatie van een workflow die een release breekt of een recht vergroot | Het releasepad heeft geen tweede vangnet. |
| `test_instruction_files.py` (11) | naar Go: `claude_md_imports_agents_md`, `agents_md_names_the_guards_and_the_escape_hatch`, `core_block_word_budget`, `core_block_says_how_to_read_a_heading`, `core_block_says_an_old_binary_is_no_unset_key`, `core_block_says_finish_what_is_authorised`, `project_part_does_not_repeat_the_core_block`, `project_part_keeps_the_lighthouse_rules`, `agents_md_headings_are_unique`; weg — gedekt: `installed_core_block_matches_the_package` (CI-stap `lighthouse init --upgrade` eist `No changes.`); weg — onderwerp weg: `project_part_points_at_the_release_policy` (negatief deel `tags are set by hand` is een migratie-pin) | De tekst is het product (`lighthouse-test-standard`, *Contract tests*). |
| `test_agent_guide.py` (~28) | naar Go: `init_flags_exist`, `superpowers_count`, `every_required_tool_is_named`, `languages_are_the_config_languages`, `failure_message_is_printed`, `upgrade_notes_are_named`, `no_changes_is_what_init_prints`, `claude_md_import_is_asked_not_left_open`, `binary_blocks`; de overige weg — beschermt niets (woorden pinnen, kop-volgorde, `--yes`-verbod als tekst), behalve de drie site-HTML-tests (`paste_block_*`, `guide_is_linked`): die gaan mee met stap 10 niet; ze vervallen hier als weg — gedekt door `TestAgentGuideIsLinkedFromInstallPage` (Taak 4) | De gids moet kloppen met wat `init` echt print en kent; woorden zonder gedrag niet. |
| `test_dogfood.py` (11) | naar Go (`e2e/`): `registered_bash_hook_blocks_a_commit_on_main`, `registered_bash_hook_allows_a_commit_on_a_branch`, `registered_edit_hook_warns_in_main_checkout`, `no_registration_outside_lighthouse_hooks`, `go_hooks_registered`, `allow_main_sentinel_is_not_tracked`; weg — gedekt: `bash_pretooluse_hooks_live_in_one_block` (door `no_registration…` plus de `main-commit-gate`-test: een tweede blok laat dezelfde test dubbel matchen en `_registered` eist er één), `node_pack_installed` (door `lighthouse init --upgrade` in CI); weg — beschermt niets: `test_gate_uses_selectors` (de test herhaalt de config letterlijk), `allow_main_sentinel_is_not_ignored` | Een fout pad of ontbrekende registratie in `settings.json` laat de guard stil uit. |
| `test_sources.py` (9) | naar Go: `every_pack_file_has_a_source`, `no_entry_without_file`, `entry_has_exactly_one_provenance`; weg — beschermt niets: `check_entry_accepts_each_kind`, `check_entry_rejects_bad_entries`, `rejects_new_and_pin_in_a_written_file`, `walks_every_package` (testen de testhelper), `header_does_not_mention_paia` | Een pack-bestand zonder herkomst raakt onbewaakt; de helper-tests bewaken de test. |
| `test_literal_grep.py` (~17) | naar Go (`e2e/`): `hit_fails_case_insensitively`, `empty_denylist_is_rc_2`, `allowlisted_line_passes`, `same_text_in_other_file_fails`, `other_line_in_allowlisted_file_fails`, `sources_toml_is_skipped`, `unreadable_allowlist_is_rc_2`, `go_tests_are_skipped_only_on_request`, `comment_and_blank_lines_in_denylist_match_nothing`; weg — gedekt: `pack_is_clean`, `go_is_clean` (CI draait `ci/literal-grep.sh` op `pack`, `internal`, `cmd`), `clean_dir_passes`, `several_dirs_are_scanned`, `sources_toml_is_skipped_in_every_dir` (variant van een geporte test); weg — beschermt niets: `go_denylist_has_no_bare_paia` (herhaalt de lijst), `python_caches_are_skipped` (zie Taak 5: eerst beslissen of het script nog `.pyc` hoeft te skippen) | Het script is een poort van de portabiliteitsregel; een kapotte poort laat literals door. |
| `test_shard.py` (13) | weg — onderwerp weg | De pytest-shards verdwijnen in de afronding van milestone #11; `_shard.py` en de hooks blijven tot dan staan (zie Taak 6, *Besluit*). |
| `test_network.py` (10) | blijft Python, verhuist naar `tests/pack/` | De hook slaat netwerkfouten van `uvx`/`curl` over in `tests/pack/python/test_ruff_format.py`, `tests/pack/shell` e.a.; dat blijft pytest (spec §7: gedeelde helpers verhuizen). Geen Go-bestemming: het is een pytest-plugin. |
| `test_getting_started.py` (~12) | naar Go: `requirements_table_names_every_tool`, `requirements_match_code_spans_exactly`, `init_prompt_matches_the_binary`, `init_flags_exist`, `first_issue_names_only_existing_skills`, `first_issue_walks_the_chain_in_order`; naar Go (`e2e/`): `config_example_loads`; weg — beschermt niets: `requirements_name_claude_code`, `requirements_say_what_falls_away`, `install_offers_binary_then_go`, `config_keys_point_to_the_go_reader`, `readme_links_the_guide` | Een gids die een niet-bestaand commando of een verdwenen tool noemt, is een kapotte gids. |

## Taak 1: pakket en de workflow-pins van `ci.yml`, `macos.yml`, `pages.yml`

**Bestanden:**
- Maak: `internal/repopins/repo_test.go`, `internal/repopins/workflows_test.go`
- Verwijder: `tests/test_golangci_pin.py`, `tests/test_ci_go_test_pin.py`, `tests/test_pages_workflow.py`
- Wijzig: `.github/workflows/ci.yml` en `.github/workflows/macos.yml` (alleen de comments die naar `tests/test_ci_go_test_pin.py` verwijzen); `ci/domains.py` en `tests/test_domains.py` (stap 2a)

**Interfaces:**
- Produceert: `func repoRoot(t *testing.T) string`; `func read(t *testing.T, rel ...string) string`
  (faalt de test bij een leesfout); `func jobs(text string) map[string]string` (een workflow per
  job, regels onder `jobs:` met twee spaties inspringing); `func runBlocks(text string) []string`
  (elke `run:`-waarde, ook het blok onder `run: |`). Taak 2 gebruikt ze.

- [ ] **Stap 1: Lees de drie Python-bestanden en de huidige `lighthouse.toml` `[test_gate] command`.**
  Bevestig de triagetabel voor deze drie bestanden; wijkt een regel af, pas de tabel in dit plan
  aan in dezelfde commit.
- [ ] **Stap 2: Schrijf de helpers in `repo_test.go`** (`repoRoot` loopt omhoog tot `go.mod` en
  faalt met `t.Fatal` als die ontbreekt).
- [ ] **Stap 2a: Houd het `go`-domein eerlijk.** De nieuwe Go-tests lezen `docs/getting-started.md`,
  `docs/site/agents/install.md`, `docs/site/install.html`, `docs/site/index.html`,
  `docs/site/download.js`, `docs/site/**` (pages-pins, `AGENTS.md`-regel over
  `docs/developer/planning/specs/`) en `ci/literal-denylist*.txt`/`ci/literal-allowlist.txt`
  (Taak 5). Anders loopt `tests/test_domains.py` rood (het ziet `docs/`, `ci/literal-` in een
  `.go`-bestand) én draait een PR die alleen die bestanden wijzigt de Go-job niet, zodat de pin
  stil onbewaakt raakt (spec Niet-doelen). Dus: in `ci/domains.py` breid `_GO_DOCS` uit met de
  gelezen `docs/`-bestanden (`docs/getting-started.md`, `docs/site/agents/install.md`,
  `docs/site/install.html`, `docs/site/index.html`, `docs/site/download.js`) en haal de drie
  `ci/literal-*.txt`-bestanden uit `_NOT_GO` (de doc-comment erboven bijwerken: ze worden nu ook
  door `e2e/literalgrep_test.go` gelezen); voeg `test_classify`-gevallen toe in
  `tests/test_domains.py`; en voeg in `_without_default_paths` een uitzondering toe voor
  `internal/repopins/` en `e2e/literalgrep_test.go` (met uitleg: ze lezen bestanden die
  `ci/domains.py` in `go` houdt, zoals bij `internal/config/config_test.go`). Draai
  `uv run pytest -q tests/test_domains.py` na elke taak. Noem dit in de PR-body. Voor
  `docs/site/**` die al door pages-pins wordt genoemd als pad in een workflow-*tekst*: dat is
  tekst in `.github/workflows/pages.yml`, geen lezen van `docs/site`; de Go-test noemt het alleen
  als verwacht bestandspad in een assertie, dus dezelfde `repopins`-uitzondering dekt het.
- [ ] **Stap 3: Schrijf de tests in `workflows_test.go`:**
  `TestGolangciPinMatchesCI`, `TestEveryCIGoTestHasGateFlags`,
  `TestEveryMacOSGoTestHasGateFlags`, `TestCIBuildsWithoutCGO`, `TestCICrossBuildsReleaseTargets`,
  `TestPagesPublishesTheSiteFolder`, `TestPagesRunsOnSiteChangesOnMain`,
  `TestPagesPermissions`, `TestEveryWorkflowPinsActionsBySHA` (alle `.github/workflows/*.yml`,
  lokale `./.github/workflows/x.yml`-verwijzingen uitgezonderd, patroon
  `@[0-9a-f]{40} # v\d+(\.\d+)*`). De vlaggen komen uit de Go-regel van `[test_gate] command`
  (`lighthouse gate go -- go test …`), gelezen met `os.ReadFile("lighthouse.toml")` en een regexp;
  geen toml-bibliotheek nodig als `go.mod` er geen heeft.
- [ ] **Stap 4: Draai `go test ./internal/repopins` — verwacht PASS.**
- [ ] **Stap 5: Mutatie per test.** Voer de mutatie uit, draai de test, zie hem falen, zet de
  mutatie terug (`git checkout -- <bestand>`), en noteer hem in een scratch-bestand voor de
  PR-body. Mutaties: `.golangci-version` naar `v0.0.0` (`TestGolangciPinMatchesCI`); `-race`
  uit één `go test`-regel van `ci.yml` (`TestEveryCIGoTestHasGateFlags`) en van `macos.yml`
  (`…MacOS…`) — de vlaggen komen uit het Go-deel van `[test_gate] command`, dat alleen
  `go test -race` heeft, dus `-shuffle=on` weghalen zou de test groen laten; `CGO_ENABLED=0 ` uit de build-regel van `ci.yml` (`TestCIBuildsWithoutCGO`);
  `GOOS=darwin GOARCH=arm64` uit `ci.yml` (`…CrossBuilds…`); `path: docs/site` naar `path: docs`
  (`…PublishesTheSiteFolder`); `- "docs/site/**"` weg (`…RunsOnSiteChanges…`); `pages: write`
  weg (`…Permissions`); één `uses:` terug naar `@v4` (`TestEveryWorkflowPinsActionsBySHA`).
- [ ] **Stap 6: Verwijder de drie `.py`-bestanden en pas de twee comments in `ci.yml` en `macos.yml`
  aan** naar `internal/repopins/workflows_test.go`.
- [ ] **Stap 7: Draai `go test ./...` en `uv run pytest -q tests/test_dogfood.py tests/test_instruction_files.py tests/test_domains.py`
  — verwacht groen.** (De tweede draait nog: ze gaan pas in Taak 3 en 5 weg.)
- [ ] **Stap 8: Commit** — `test(repopins): workflow-pins naar Go, pytest-pins weg (#365)`.

## Taak 2: de release-pins

**Bestanden:**
- Maak: `internal/repopins/release_test.go`
- Verwijder: `tests/test_release_workflows_pin.py`

**Interfaces:**
- Verbruikt: `read`, `jobs`, `runBlocks` uit Taak 1.

- [ ] **Stap 1: Triage elke functie van `tests/test_release_workflows_pin.py`** (ongeveer dertig).
  Per functie de classificatiezin in een commentaarregel boven de Go-test, of een regel in de
  weg-lijst van de PR. Beslisregel: naar Go als de mutatie een release breekt, een
  schrijfrecht (`contents: write`) of een secret naar een stap brengt die ze niet nodig heeft,
  of de volgorde van tag en push verstoort. Weg — beschermt niets als de test alleen een
  stap­naam of een bestaande `runs-on` herhaalt (`every_step_job_has_a_runner`,
  `build_has_no_spa_artifact` zijn kandidaten; bevestig met de mutatie). Verwacht een tabel van
  ongeveer 20 Go-tests en 10 weg.
- [ ] **Stap 2: Schrijf de Go-tests** met de namen van de Python-functies in CamelCase
  (`TestReleasePushesAtomically`, …). Hergebruik `jobs` en `runBlocks`; voeg een helper `steps(job
  string) []string` toe die een job in stappen knipt (zoals `_steps` in het Python-bestand).
  `test_actions_pinned_by_sha` vervalt: `TestEveryWorkflowPinsActionsBySHA` (Taak 1) dekt het.
- [ ] **Stap 3: Draai `go test ./internal/repopins` — PASS.**
- [ ] **Stap 4: Mutatie per Go-test**, zoals in Taak 1, stap 5. Voorbeelden: `--atomic` uit
  `git push --atomic` (`ReleasePushesAtomically`); `contents: read` naar `contents: write` in de
  `ui`-job (`UIJobBuildsInAReadOnlyJob`); `persist-credentials: false` weg uit een checkout
  (`OnlyReleaseJobPersistsCredentials`); `needs: macos` weg uit `release` (`ReleaseWaitsForMacOS`).
  Noteer de mutatie per test.
- [ ] **Stap 5: Verwijder het `.py`-bestand, draai `go test ./...` — PASS, commit** —
  `test(repopins): release-pins naar Go, pytest-pins weg (#365)`.

## Taak 3: instructiebestanden en `SOURCES.toml`

**Bestanden:**
- Maak: `internal/repopins/instructions_test.go`, `internal/repopins/sources_test.go`
- Verwijder: `tests/test_instruction_files.py`, `tests/test_sources.py`

**Interfaces:**
- Verbruikt: `repoRoot`, `read` uit Taak 1.
- Produceert: `func packFiles(t *testing.T) map[string]bool` (elk bestand onder `pack/<pakket>/`,
  zonder `__pycache__`, `.pytest_cache`, `.DS_Store`, `*.pyc`).

- [ ] **Stap 1: Schrijf de instructietests** met de namen uit de triagetabel:
  `TestClaudeMDImportsAgentsMD`, `TestAgentsMDNamesTheGuardsAndTheEscapeHatch`,
  `TestCoreBlockWordBudget` (≤ 365 woorden, elk token, elke `/lighthouse-<skill>` voor elke map
  onder `pack/core/skills`), `TestCoreBlockSaysHowToReadAHeading`,
  `TestCoreBlockSaysAnOldBinaryIsNoUnsetKey`, `TestCoreBlockSaysFinishWhatIsAuthorised`,
  `TestProjectPartDoesNotRepeatTheCoreBlock` (zonder het token `uv run pytest` en zonder
  `git worktree add`-uitzonderingen die het huidige bestand al toelaat: lees het Python-bestand
  nog eens en neem de tokenlijst over, dit is de enige plek waar dat nodig is),
  `TestProjectPartKeepsTheLighthouseRules`, `TestAgentsMDHeadingsAreUnique`. Het core-blok wordt
  uit `AGENTS.md` gesneden tussen `<!-- lighthouse:begin core -->` en `<!-- lighthouse:end core -->`.
- [ ] **Stap 2: Schrijf de sources-tests:** `TestEveryPackFileHasASource` (ook: geen pad twee
  keer), `TestNoSourceEntryWithoutFile`, `TestSourceEntryHasExactlyOneProvenance` (precies één
  van `new = true`, `pin = "<niet leeg>"` of de drie velden `source_repo` `owner/repo`,
  `source_sha` 40 hex, `source_path` niet leeg). Parse `pack/SOURCES.toml` met wat `go.mod` al
  voor toml heeft (`grep -n toml go.mod`); `internal/pins/sources.go` heeft misschien al een
  parser: hergebruik die en importeer vanuit de test als hij exporteert, anders een eigen kleine
  parse in de test.
- [ ] **Stap 3: Draai `go test ./internal/repopins` — PASS.**
- [ ] **Stap 4: Mutatie per test.** Eén regel uit `AGENTS.md` (`main-commit-gate` weg); het
  core-blok één woord langer dan 365 woorden; `lighthouse headings <key>` weg uit
  `pack/core/instructions/AGENTS.md`; een extra bestand `pack/core/x` zonder entry; een entry
  `path` die nergens bestaat; een entry met `new = true` én `pin`; `@AGENTS.md` van regel 1 van
  `CLAUDE.md`; een dubbele kop in `AGENTS.md`.
- [ ] **Stap 5a: Verwijzing in `pack/SOURCES.toml`.** Regel 2 noemt `tests/test_sources.py`; wijs
  hem naar `internal/repopins/sources_test.go` (alleen die commentaarregel). Dit is een wijziging
  onder `pack/`: draai daarna `go run ./cmd/lighthouse init --upgrade` en commit de
  geïnstalleerde kopie mee (dogfood-regel in `AGENTS.md`), zonder dat dat het gedrag verandert.
- [ ] **Stap 5: Verwijder de twee `.py`-bestanden, draai `go test ./...` — PASS, commit** —
  `test(repopins): instructiebestanden en SOURCES.toml naar Go, pytest weg (#365)`.

## Taak 4: de gidsen tegen de code

**Bestanden:**
- Maak: `internal/repopins/docs_test.go`, `e2e/gettingstarted_test.go`
- Verwijder: `tests/test_getting_started.py`, `tests/test_agent_guide.py`

**Interfaces:**
- Verbruikt: `repoRoot`, `read`.
- Produceert: `func section(t *testing.T, doc, title string) string` (de tekst tussen `## <title>`
  en de volgende `## `); `func initFlags(t *testing.T) map[string]bool` (de `Var(&o.x, "<flag>"`
  van `internal/cli/init.go`); `func requiredTools(t *testing.T) map[string]bool` (de tabelnamen van
  `pack/*/requires.toml`, `forge.<kind>`-tabellen meegeteld, `forge` zelf niet).

- [ ] **Stap 1: Controleer wie de Python-helpers nog gebruikt.**
  `grep -rn 'test_getting_started\|test_agent_guide' tests/ --include=*.py` mag alleen de twee
  bestanden zelf geven (anders: pas die importeur aan, of verhuis hem naar de taak van stap 10).
- [ ] **Stap 2: Schrijf `getting-started`-tests:** `TestRequirementsTableNamesEveryTool`,
  `TestRequirementsMatchCodeSpansExactly`, `TestInitPromptMatchesTheBinary` (de prompt uit
  `ask(o.Out, in, "…")` in `internal/initcmd/initcmd.go` staat in de sectie *Run lighthouse init*),
  `TestGettingStartedInitFlagsExist`, `TestFirstIssueNamesOnlyExistingSkills`,
  `TestFirstIssueWalksTheChainInOrder` (`/lighthouse-write-issue`, `…start-issue`, `…write-pr`).
- [ ] **Stap 3: Schrijf `agent_guide`-tests:** `TestAgentGuideInitFlagsExist`,
  `TestAgentGuideSuperpowersCount` (telt `pack/core/requires.toml` `[superpowers]`),
  `TestAgentGuideNamesEveryRequiredTool`, `TestAgentGuideLanguagesAreTheConfigLanguages`
  (vergelijk met `var languages = []string{"nl", "en"}` in `internal/config/config.go:45`: lees
  die regel met een regexp uit de Go-bron, geen Python-schema),
  `TestAgentGuideFailureMessagesArePrinted` (subtests per bericht; plaatshouders `<…>` en `…`
  splitsen; berichten die cobra zelf print, vallen buiten), `TestAgentGuideUpgradeNotesAreNamed`,
  `TestAgentGuideNoChangesIsWhatInitPrints`, `TestAgentGuideClaudeMDImportIsAsked`,
  `TestAgentGuideBinaryBlocks` (de assetnamen komen uit `internal/…` of `docs/site/download.js`:
  lees de `TABS` en `DOWNLOAD` uit `tests/test_site_download.py` en kijk waar de bron staat; is
  het alleen in die pytest-file gedefinieerd, lees ze dan uit `docs/site/download.js`),
  `TestAgentGuideIsLinkedFromInstallPage` (`agents/install.md` staat als `href` in
  `docs/site/install.html` en `index.html`, het `id="agent"`-blok bevat de plak-regel; plus: `docs/site/download.js` roept
  `addCopyButtons` aan met `.agent-prompt` in de selectorlijst, de dekking van
  `test_paste_block_has_copy_button`, mutatie: `.agent-prompt` uit die aanroep).
- [ ] **Stap 4: Schrijf de e2e-test `e2e/gettingstarted_test.go`:** `TestConfigExampleLoads` — het
  toml-blok uit *Configure lighthouse.toml* in een temp-map, `LIGHTHOUSE_REPO_ROOT` erop,
  `lighthouse config project.language` geeft `en` en `forge.kind` geeft `github`. Volg het
  patroon van `e2e/e2e_test.go` voor het bouwen van de binary.
- [ ] **Stap 5: Draai `go test ./internal/repopins` en `go test -tags e2e ./e2e -run TestConfigExampleLoads`
  — PASS.**
- [ ] **Stap 6: Mutatie per test.** Een tool uit de *Requirements*-tabel; een niet-bestaande
  vlag in de gids (`--bogus`); `Without Superpowers, 99 lighthouse skills`; een taal `xx` in de
  taalregel; een foutmelding in *When something fails* met een zin die `internal/` niet print;
  `No changes.` uit `initcmd.go` hernoemen; `agents/install.md` uit de `href` in `install.html`;
  `forge.kind` uit het voorbeeldblok.
- [ ] **Stap 7: Verwijder de twee `.py`-bestanden, draai `go test ./...`,
  `go test -tags e2e ./e2e` en `uv run pytest -q tests/test_site_download.py tests/test_site_pages.py
  tests/test_site_loop.py` — PASS** (die drie importeerden niet uit de verwijderde bestanden, maar
  bevestig het). **Commit** — `test(repopins): gidsen tegen de code naar Go, pytest weg (#365)`.

## Taak 5: `test_dogfood` en `test_literal_grep` als black box

**Bestanden:**
- Maak: `e2e/dogfood_test.go`, `e2e/literalgrep_test.go`
- Verwijder: `tests/test_dogfood.py`, `tests/test_literal_grep.py`

**Interfaces:**
- Verbruikt: de binary-bouw en `cleanEnv()` uit `e2e/e2e_test.go`; `repoRoot` is daar nog niet
  (andere module-pad): neem in `e2e/` een eigen `repoRoot(t)` of hergebruik wat daar al staat.

- [ ] **Stap 1: Dogfood.** `TestRegisteredBashHookBlocksACommitOnMain`,
  `…AllowsACommitOnABranch`, `TestRegisteredEditHookWarnsInMainCheckout`: lees het geregistreerde
  commando uit `.claude/settings.json` (precies één onder de matcher), draai het met `sh -c`, met
  `CLAUDE_PROJECT_DIR=<de lighthouse-checkout (repoRoot)>`, de werkmap en het `cwd` in de payload = het
  tijdelijke test-repo, een payload op stdin en een PATH met alleen `git`, `sh`, de gebouwde
  `lighthouse` en wat `run.sh` nodig heeft (kijk welke tools `tests/pack/_testkit.py`
  `RUN_SH_TOOLS` noemt); het test-repo wordt met `git init -b main` en één commit gemaakt.
  Verwacht exit 2 plus `Main-commit gate: blocked` / exit 0 / `systemMessage` met
  `Worktree check:`. Plus `TestNoRegistrationOutsideLighthouseHooks`, `TestGoHooksRegistered`,
  `TestAllowMainSentinelIsNotTracked` (`git ls-files`).
- [ ] **Stap 2: Literal-grep.** Een helper `grep(t, env, args...)` die `ci/literal-grep.sh` draait
  en `(rc, stdout)` geeft. Tests: `TestLiteralGrepHitFailsCaseInsensitively` (parameters
  `PAIA_ROOT`, `Beelink`, `/home/x`, `4-lane:stalled`, `PVT_kwHOAGmU3s4BfI-i`; verwacht rc 1 en
  `a.md:1:`), `TestLiteralGrepEmptyDenylistIsRC2`, `TestLiteralGrepAllowlistedLinePasses`,
  `…SameTextInOtherFileFails`, `…OtherLineInAllowlistedFileFails`, `…SourcesTomlIsSkipped`,
  `…UnreadableAllowlistIsRC2`, `…GoTestsAreSkippedOnlyOnRequest`,
  `…CommentAndBlankDenylistLinesMatchNothing`. `python_caches_are_skipped` weg tenzij het script
  nog `.pyc` skipt: `grep -n 'pyc\|__pycache__' ci/literal-grep.sh`; staat het er nog, dan
  naar Go met de mutatie dat de skip uit het script verdwijnt.
- [ ] **Stap 3: Draai `go test -tags e2e ./e2e` — PASS.**
- [ ] **Stap 4: Mutatie per test.** `.claude/settings.json`: pad `run.sh` naar `run.shx`;
  `main-commit-gate` uit de registratie; een hook buiten `.claude/hooks/lighthouse/` toevoegen;
  `gofmt-check.sh` uit `PostToolUse`; `.claude/allow-main` `git add -f`. Literal-grep: `-i` uit
  het `grep`-commando van het script; de lege-lijst-controle (rc 2) weghalen; de allowlist-vergelijking
  op alleen pad; de `SOURCES.toml`-skip weg; `LITERAL_SKIP_TESTS` negeren.
- [ ] **Stap 5: Verwijder de twee `.py`-bestanden, `go test ./...` en `go test -tags e2e ./e2e`
  — PASS, commit** — `test(e2e): dogfood en literal-grep als black box in Go, pytest weg (#365)`.

## Taak 6: shard, network en helpers; opruimen; de PR-body

**Bestanden:**
- Verwijder: `tests/test_shard.py`
- Verhuis (`git mv`): `tests/test_network.py` → `tests/pack/test_network.py`,
  `tests/_network.py` → `tests/pack/_network.py`
- Wijzig: `tests/conftest.py` (het `pytest_runtest_makereport`-hook naar
  `tests/pack/conftest.py`), `pyproject.toml` (marker-tekst), de imports in de verhuisde
  bestanden
- Maak: `changelog.d/issue-365-triage-root-tests.md`

- [ ] **Besluit (vastleggen in de PR-body):** `_shard.py` en de shard-hooks in `tests/conftest.py`
  blijven staan tot de CI-afronding van milestone #11, omdat `ci.yml` en `macos.yml` ze nog
  gebruiken (`LIGHTHOUSE_PYTEST_SHARD`). `tests/test_shard.py` gaat wel weg: het onderwerp verdwijnt
  in dezelfde milestone, en wie het wil terughalen vindt de tekst in de git-historie. Dit is de
  enige regel van de triage waar een reviewer kan zeggen "gedekt niet"; zeg dat in de PR.
- [ ] **Stap 1: Verhuis het network-hook.** `git mv` de twee bestanden; `tests/pack/conftest.py`
  krijgt het `pytest_runtest_makereport`-hook en de import van `_network` (relatief:
  `from tests.pack._network import …`); verwijder hook en import uit `tests/conftest.py`;
  `tests/pack/test_network.py` importeert `PROBE_ENV, network_error, offline` van de nieuwe
  plek en zijn inner-run (`from tests.conftest import pytest_runtest_makereport`) wijst naar
  `tests.pack.conftest`. Pas `pyproject.toml` en de docstring van `_network.py` aan.
- [ ] **Stap 2: Draai `uv run pytest -q tests/pack/` — PASS** (`test_network.py` draait
  nu daar mee, dezelfde tests).
- [ ] **Stap 3: Verwijder `tests/test_shard.py`; draai `uv run pytest -q tests/` en
  `go test ./...` — beide PASS.**
- [ ] **Stap 4: Controleer dat niets in `tests/` of CI nog naar een verwijderd bestand verwijst:**
  `grep -rnE 'test_(golangci_pin|ci_go_test_pin|release_workflows_pin|instruction_files|agent_guide|dogfood|sources|literal_grep|shard|pages_workflow|getting_started)' . --include=*.py --include=*.yml --include=*.toml --include=*.md | grep -v planning/` is leeg. Een treffer in
  een `docs/` buiten `planning/`: vervang door het Go-pad.
- [ ] **Stap 5: Schrijf `changelog.d/issue-365-triage-root-tests.md`:**

  ```markdown
  ---
  type: internal
  ---
  De root-tests van `tests/` zijn getrieerd: de pins op workflows, instructiebestanden, `pack/SOURCES.toml`, de gidsen, `settings.json` en `ci/literal-grep.sh` staan nu als Go-tests in `internal/repopins/` en `e2e/`; de rest is weg.
  ```
- [ ] **Stap 6: Commit** — `test: shard-test weg, network-hook naar tests/pack, changelogfragment (#365)`.
- [ ] **Stap 7: Verzamel de PR-body.** Voor `/lighthouse-write-pr`: de triagetabel (bestand, aantal
  tests voor, bestemming per test of groep, één zin reden, bij *gedekt* de Go-test), de lijst
  mutaties per geporte test uit de stappen 5 van Taak 1–5, de totalen (tests voor, geport, weg,
  verhuisd) en het besluit over `_shard.py`. Het bewijs `go test ./...` groen staat in de
  Evidence-tabel.

## Zelfreview

- **Spec §1:** elke genoemde test staat in de triagetabel met een bestemming; *gedekt* noemt de
  Go-test (`TestEveryWorkflowPinsActionsBySHA`, CI-stap `init --upgrade`, CI-stap `literal-grep`).
- **Acceptatiecriteria:** triagetabel (Taak 6, stap 7), mutaties (stap 5 per taak), `.py`-bestanden
  weg en groen (stap 5/7 per taak), `changelog.d/` (Taak 6, stap 5).
- **Open punt voor de reviewer:** `test_network.py` verhuist naar `tests/pack/` in plaats
  van naar Go of weg; zie de rij in de triagetabel. Het issue noemt het bestand "weg of naar
  Go"; dit is de enige afwijking, en de reden is dat `tests/pack/**` (shellcheck-download in `tests/pack/shell`, ruff via uvx) de hook gebruikt; hij hoort bij `tests/pack/` als geheel, niet alleen bij `python/`.
