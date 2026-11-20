# Triage skillcontracten in tests/pack (#367) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Elke contracttest op skill- en pakkettekst in de 18 bestanden hieronder krijgt één bestemming (spec §1). Wat blijft, draait als Go-test in één nieuw pakket `internal/packcontract`. De 18 Python-bestanden verdwijnen.

**Architecture:** Een testpakket `internal/packcontract` leest `pack/` via `../../pack/...` met `os.ReadFile`, `os.ReadDir` of `filepath.Glob`, zoals `internal/kpi/fixpr_test.go:64-110`. De `go test`-cache hasht elk bestand dat de test opent, dus een gewijzigde `SKILL.md` maakt de cache ongeldig (AC 3, bewezen in taak 1). Het pakket staat buiten `pack/`, omdat `pack/pack.go` met `//go:embed all:*/*` alles onder `pack/` in de binary bakt. Tests die nu de binary draaien, roepen in Go de functie zelf aan: `checkinstr.Run`, `config.Parse`/`config.IsKnownKey`, `evidence`/`prbody`, `worktreeaudit`.

**Tech Stack:** Go (`testing`, `os`, `regexp`, `github.com/BurntSushi/toml`, `gopkg.in/yaml.v3`), pytest (alleen om te verwijderen).

**Spec:** [`docs/developer/planning/specs/2026-11-16-repo-zonder-python-design.md`](../specs/2026-11-16-repo-zonder-python-design.md) §1 (triage), §2 (skillcontracten). Issue: #367.

## Global Constraints

- Bestemmingen, letterlijk uit spec §1: **weg — onderwerp weg**, **weg — gedekt** (noem de Go-test), **weg — beschermt niets**, **naar Go**.
- De kernvraag geldt per testfunctie: *welke productiewijziging laat deze test falen?* Een geparametriseerde test (`UNITS`, `SKILL_PARAMS`, `VARIANTS`, `ALL`, `NAMES`, `BOTH`, `CHAIN_SKILLS`) telt als **één** test. Spec §1: "Geparametriseerde gevallen tellen als één test als ze op dezelfde mutatie falen". De tabel telt dus `def test_`-functies. Het totaal voor is **297**.
- `lighthouse-test-standard`, *Contract tests*: voor een instructiebestand is de tekst het product, dus een pin mag blijven. Een verbod mag alleen voor een **blijvende invariant**. Een "staat er nog steeds niet in" na een migratie is een negative-existence-test en gaat weg.
- Een test die je wilt porten maar niet met een mutatie kunt laten falen, port je niet (spec, *Aan de bouwer*).
- Twijfel tussen *gedekt* en *naar Go*: mutateer de productietekst of -code en draai de Go-kandidaat. Faalt die, dan is het *gedekt*.
- Go volgt `/lighthouse-go-standard`. Elke Go-test krijgt een `// Classification: fails when …`-commentaar, zoals `internal/kpi/fixpr_test.go:62`.
- Een skill met `claude/` en `agents/` wordt per variant gecontroleerd, met `t.Run(variant, …)`. Een gedeelde `SKILL.md` telt voor beide.
- Geen `go:embed` uit `pack/` in het testpakket (embed-paden kennen geen `..`). Geen `_test.go` onder `pack/`.
- Raak `pack/**`, `.claude/skills`, `.agents/skills`, `tests/pack/python/`, `tests/pack/fixtures/` en bestaande Go-tests niet aan (milestone-no-gos, en het Go-pakket leest fixtures).
- De andere 24 `tests/pack/test_*.py` zijn van #366, inclusief `test_skill_scripts_gone.py` en de verhuizing van `_testkit.py`. `tests/pack/{go,shell,node,expo}` is #368. `tests/pack/_testkit.py` en `conftest.py` blijven in deze PR staan.
- Artefacten (PR-body, commitberichten, changelog) in het Nederlands, als Conventional Commits.

## Review Focus

1. **Een pin die leeg slaagt.** Een regex of sectie-snede die niets vindt, laat een "geen fout"-lus groen. Elke lus over hits (config-sleutels, heading-sleutels, skill-links, tool-patronen) asserteert ook dat er minstens één hit was. Dat vervangt `test_config_re_finds_the_keys` en `test_tool_patterns_see_python_and_tomllib` (taak 2).
2. **Een nieuwe skill valt buiten de lus.** De structurele tests lopen over `pack/*/skills/*` per glob, niet over een vaste lijst. Een vaste lijst mag alleen waar het origineel er ook een had (`CHAIN_SKILLS`, `NAMES`).
3. **Eén variant ongetest.** Een pin die alleen `claude/SKILL.md` leest, mist een drift in `agents/SKILL.md`. Helper `variants` (taak 1) geeft altijd beide.
4. **Een warme cache na een `SKILL.md`-wijziging.** Bewijs het in taak 1 en opnieuw in taak 9 (AC 3). Bewijs het ook voor een **nieuw** bestand in een skill-map, dat alleen via `ReadDir`/`Glob` gezien wordt.
5. **`tests/pack/python/` breekt.** Geen bestand uit `tests/pack/python/` mag iets importeren uit de 18 bestanden. Controleer met `grep -rn` in taak 9 en draai `uv run pytest -q tests/pack/python`.

---

## Bestandsstructuur

| Bestand | Verantwoordelijkheid |
|---|---|
| `internal/packcontract/doc.go` | Alleen `package packcontract` met het pakketcommentaar: contracttests op de tekst onder `pack/`, geen productiecode. |
| `internal/packcontract/helpers_test.go` | `packRoot`, `readPack`, `variants`, `skillDirs`, `section`, `flat`, `frontmatter` (taak 1). |
| `internal/packcontract/skills_test.go` | Structurele checks over alle skills en agents (uit `test_skill_contracts.py`). |
| `internal/packcontract/issueflow_test.go` | route-issue, start-issue, write-handoff. |
| `internal/packcontract/writepr_test.go`, `writeissue_test.go`, `closeissue_test.go`, `fixpr_test.go`, `upgrade_test.go`, `teststandard_test.go`, `auditworktrees_test.go`, `placeinstruction_test.go` | Eén bestand per skill. |
| `internal/packcontract/review_test.go` | review-spec en review-plan (uit `test_adversarial_review_skills.py`). |
| `internal/packcontract/scout_test.go`, `prtemplate_test.go`, `workflowunblock_test.go` | Agent, PR-template, workflow. |
| `internal/packcontract/packfiles_test.go` | Fragmenten, `requires.toml`, portable shell, shellcheck. |

Een bestand dat na de triage geen test overhoudt, maak je niet aan.

Naam en plek: `internal/packcontract`. De tests bewaken het contract van wat onder `pack/` staat (skills, agents, template, workflow, fragmenten, shell), niet alleen skills. Daarom niet `skillcontract`.

## Werkwijze per brontaak (taken 2–8)

Elke taak volgt dezelfde zeven stappen per bronbestand.

- [ ] **a. Lees** het Python-bestand volledig.
- [ ] **b. Bepaal per `def test_` de bestemming** met de beslisregels hieronder. Werk de concepttabel onderaan dit plan bij: bestemming, reden in één zin, bij *gedekt* de Go-test, bij *naar Go* de invariant en de mutatie.
- [ ] **c. Schrijf de Go-tests** voor *naar Go*. Neem alleen de asserties mee die de kernvraag beantwoorden. Laat een negatieve assertie vallen als die alleen een migratie bewaakt (regel R3), ook binnen een test die verder blijft.
- [ ] **d. Draai** `go test ./internal/packcontract/ -run '<Naam>' -v` en verwacht PASS.
- [ ] **e. Mutatiecheck per geporte test.** Breek de gepinde zin in de bron (`pack/...`) en draai de test. Verwacht FAIL. Zet de bron terug met `git checkout -- pack/`. Noteer de mutatie in de tabel. Faalt de test niet, dan port je hem niet: bestemming *beschermt niets*.
- [ ] **f. Controleer *gedekt*.** Doe dezelfde mutatie, draai de genoemde Go-test en verwacht FAIL. Zet de bron terug.
- [ ] **g. Verwijder en commit.** `git rm tests/pack/<bestand>.py`, dan `go test ./internal/packcontract/` en `git status --short pack/`. De status moet leeg zijn. Commit Go-tests, verwijdering en de bijgewerkte tabel in dit plan samen: `test(packcontract): <skill> naar Go, <bestand>.py weg (#367)`.

### Beslisregels

- **R1 naar Go.** De pin bewaakt iets waar een lezer op rekent:
  - een binary parseert of telt de tekst: `lighthouse measure-start-issue-kpi` telt rapportregels, `lighthouse protocol` leest de tokens, `pr-create-gate` de body;
  - een sleutel moet bestaan: `heading:<key>`, `lighthouse config <key>`, `@@<key>@@`;
  - de skill roept de binary aan met vlaggen die moeten bestaan;
  - een blijvende invariant: labels via config en niet letterlijk, "if present" bij wat in paia blijft, forge-neutraal (`lighthouse forge` en geen `gh`), geen bron-issuenummers, Engels, het verschil tussen de varianten;
  - een instructie die het gedrag van de agent stuurt: een stap, een volgorde, een stopregel.
- **R2 beschermt niets.**
  - Een pin op uitleg of motivatie die geen stap stuurt.
  - Een pin die een hele alinea letterlijk vastlegt, terwijl één sleutelzin de instructie al draagt. Port dan die zin en noteer in de tabel *naar Go (ingekort)*.
  - Een zelftest van een hulpregex. Die gaat op in de niet-leeg-assertie (Review Focus 1).
- **R3 weg — onderwerp weg.** Een `X not in body` waarin X door een migratie verdween. Voorbeelden: `board_start.py`, `measure_start_issue_kpi`, `PAIA_CONTROLLER_RUN`, `volgende:`/`wanneer:`/`anker:`, `--voor-pr`, `review.sh`/`verify.sh`, `python3` of `uv` in een skill of workflow, `` `after PR` ``, `klopt`/`KON NIET`, `unblock_dependents.py`.
  - Uitzondering: een verbod dat een blijvende invariant is, blijft onder R1. Voorbeelden: `3-blocked`/`3-needs` (geen letterlijke labelnamen), `#\d{3,}` (geen bron-issuenummers), `glab` als commando.
- **R4 gedekt.** Alleen na de mutatiecheck van stap f.

---

### Taak 1: Testpakket, helpers en het cachebewijs

**Files:**
- Create: `internal/packcontract/doc.go`
- Create: `internal/packcontract/helpers_test.go`
- Create: `internal/packcontract/skills_test.go` (eerste test)

**Interfaces:**
- Produces, voor alle volgende taken, in `helpers_test.go`:
  - `const packRoot = "../../pack"`
  - `func readPack(t *testing.T, rel string) string` — leest `packRoot/rel`, `t.Fatal` bij een fout.
  - `func variants(t *testing.T, skillDir string) map[string]string` — `skillDir` relatief aan `packRoot` (bijv. `"core/skills/route-issue"`).
    - Met een gedeelde `SKILL.md`: `{"claude": tekst, "agents": tekst}`.
    - Anders leest het `claude/SKILL.md` en `agents/SKILL.md`, en roept `t.Fatal` aan als er één ontbreekt.
  - `func skillDirs(t *testing.T) []string` — alle `pack/*/skills/*`-mappen, core eerst, relatief aan `packRoot`. Gebruikt `filepath.Glob`.
  - `func unitTexts(t *testing.T, unit string) map[string]string` — elk `.md`/`.sh`/`.json`/`.txt`-bestand van een skill-map (recursief), of één agentbestand. De sleutel is het relatieve pad.
  - `func section(t *testing.T, body, start, end string) string` — de tekst van `start` tot `end`. `t.Fatalf` als een van beide ontbreekt.
  - `func flat(s string) string` — `strings.Join(strings.Fields(s), " ")`.
  - `func frontmatter(t *testing.T, body string) string` — de tekst tussen de eerste twee `---`-regels.

- [ ] **Stap 1: Classificeer in de chat** (test-standard): `Class: test-only. At risk: een SKILL.md-wijziging die een pin breekt, blijft groen in go test.`
- [ ] **Stap 2: Schrijf de eerste test** in `skills_test.go`, `TestChainSkillsPointAtForgeReference`. Dit is de port van `test_chain_skills_point_at_forge_reference`.
  - Voor elke `name` in `[]string{"start-issue","route-issue","write-issue","write-pr"}` en elke variant: de tekst bevat `lighthouse config forge.kind`.
  - Elke `../lighthouse-forge/<kind>.md §<sectie>` noemt een sectie uit `forgeSections`: `board`, `blockers`, `sub-issues`, `sluitwoorden`, `proxy`, `ci`, `workflows`.
  - Er is minstens één verwijzing.
- [ ] **Stap 3:** `go test ./internal/packcontract/ -v` en verwacht PASS.
- [ ] **Stap 4: Cachebewijs (AC 3).**
  1. Draai `go test ./internal/packcontract/` twee keer. Verwacht op de tweede regel `(cached)`.
  2. Vervang `lighthouse config forge.kind` in `pack/core/skills/route-issue/SKILL.md` (of de variant waarin het staat) door `lighthouse config forge.kinds`.
  3. Draai opnieuw. Verwacht `FAIL`, niet `(cached)`.
  4. `git checkout -- pack/`.
  5. Maak `pack/core/skills/route-issue/extra.md` aan met `heading:bestaat_niet`, draai opnieuw (na taak 2 vangt `TestHeadingKeysKnown` dit) en verwijder het bestand.
  Noteer de uitvoer van stappen 1 en 3 voor de PR-body.
- [ ] **Stap 5:** `golangci-lint run ./internal/packcontract/` en verwacht schone uitvoer.
- [ ] **Stap 6: Commit:** `test(packcontract): testpakket voor contracten op pack/, eerste pin (#367)`.

### Taak 2: `test_skill_contracts.py` (22 tests)

**Files:** Modify `internal/packcontract/skills_test.go`, Delete `tests/pack/test_skill_contracts.py`.

**Interfaces:** Consumes de helpers uit taak 1. De Go-tests mogen `config.IsKnownKey`, `config.Variants` en `checkinstr.Run` uit `internal/config` en `internal/checkinstr` aanroepen.

Concept, na lezing:

| Test | Concept | Reden / Go-test |
|---|---|---|
| `test_frontmatter_name` | weg — gedekt | Skills: `packs.Load` weigert een verkeerde `name:` ("wrong name", `internal/packs/packs_test.go:282`). Dat draait op de echte pakketten in `TestCorePack` en `TestLanguagePacks`. Agents: er is één agent, en `TestScoutNameMatchesPersona` (`internal/kpi/fixpr_test.go:78`) pint die naam. Verifieer met stap f. |
| `test_variants_both_present` | weg — gedekt | `packs.Load`: "needs SKILL.md, or both claude/SKILL.md and agents/SKILL.md" (`internal/packs/packs.go:461`), via `TestCorePack` en `TestLanguagePacks`. |
| `test_referenced_files_exist` | naar Go | Invariant: geen geïnstalleerd pad dat nergens heen wijst, behalve met "if present". Neem `_PATH_REFS`, `_visible` en `_guarded` over. |
| `test_config_keys_known` + `test_config_re_finds_the_keys` | naar Go, één test `TestConfigKeysKnown` | `config.IsKnownKey`. De regex-zelftest wordt de assertie dat `route-issue` `labels.blocked_issue`, `labels.needs_shaping` en `forge.kind` oplevert. |
| `test_heading_keys_known` | naar Go | Een `heading:<key>` moet in `pack/core/headings.toml` staan (BurntSushi/toml), anders print `lighthouse headings` niets. |
| `test_skill_links_resolve` | naar Go | Neem `ALLOWED_EXTERNAL` letterlijk over. |
| `test_optional_steps_guarded` | naar Go | Invariant: wat in paia blijft, staat er alleen "if present". Neem `_paragraphs` met de omsluitende koppen over. |
| `test_planned_skills_absent` | weg — beschermt niets | `PLANNED` is leeg, dus de test faalt op geen enkele mutatie. |
| `test_descriptions_pass_the_overlap_check` | naar Go | Kopieer alle skills naar een tijdelijke `.claude/skills` en `.agents/skills`, zoals `_install_skills`. Roep `checkinstr.Run(root, personal, &out, &errOut)` aan. Assert dat `out` geen `overlap without a bounding clause` bevat. `checkinstr.Run` vraagt mogelijk meer dan skills (bijv. een `AGENTS.md`); neem dan wat `install_core` legt over, minimaal. |
| `test_requires_names_the_skill_per_tool` + `test_tool_patterns_see_python_and_tomllib` | naar Go, één test | Neem `_TOOL_USES` over. Lees `requires.toml` met BurntSushi/toml en vouw `[forge.<kind>]` in. De pattern-zelftest wordt de assertie dat `python/skills/python-standard` op `python3` en `tomllib` matcht. |
| `test_requires_python_only_where_the_skill_uses_it` | naar Go | Zelfde lus, omgekeerde richting. |
| `test_every_writing_gh_command_in_the_skills_names_the_repo` | naar Go | #224: zonder `--repo` kiest gh de upstream in een fork. Dit is een blijvende invariant. |
| `test_write_pr_creates_the_pr_through_lighthouse_forge` | naar Go | De vorm die `pr-create-gate` leest. |
| `test_chain_skills_do_not_call_gh_for_forge_operations` | naar Go | Forge-neutraal, inclusief de check op verouderde uitzonderingen (`used == allowed`). |
| `test_chain_skills_point_at_forge_reference` | naar Go | Al in taak 1. |
| `test_forge_skill_has_both_references` | naar Go | Beide referenties hebben dezelfde H2's buiten fences, in dezelfde volgorde. |
| `test_forge_skill_description_not_self_triggering` | naar Go | |
| `test_forge_reference_mentions_no_glab_command` | naar Go | Blijvend: `glab` is geen afhankelijkheid. |
| `test_forge_skills_name_no_source_issue_number` | naar Go | Blijvend (R3-uitzondering). |
| `test_review_skills_call_the_binary_not_python_or_a_wrapper` | weg — onderwerp weg | Negative existence na #282. `python3` in een skill vangt de requires-test ook. |

- [ ] Volg stappen a–g. Commit: `test(packcontract): structurele skillcontracten naar Go, test_skill_contracts.py weg (#367)`.

### Taak 3: `test_skills_issue_flow.py` (49 tests)

**Files:** Create `internal/packcontract/issueflow_test.go`, Delete `tests/pack/test_skills_issue_flow.py`.

Concept:
- **route-issue (12 tests): naar Go.** Gate 0 leest de relatie, het controller-run-gedrag, de regelvorm `#<nr> heading:gate_line …` (de KPI telt die), fog naar 4c met precies 6 labelrijen, labels via config, forge via config, "if present".
  - De negatieven `board_start.py`, `measure_start_issue_kpi.py` en `identity` vallen weg (R3). `3-blocked`/`3-needs` blijven (R1-uitzondering).
  - `test_route_issue_description_survives_the_yaml_parser` gaat naar Go met `gopkg.in/yaml.v3`. De frontmatter moet parsen en de description moet eindigen op de NOTE-zin.
- **start-issue (19 tests): naar Go.** De drie rapportregels en `IMPACT PACK: in context, do not repeat` telt `lighthouse measure-start-issue-kpi`. Ook de already-done-recept, de board-skip en de projectpaden.
  - `test_start_issue_controller_run_env` gaat weg (R3, `PAIA_CONTROLLER_RUN`).
  - In `test_start_issue_keeps_the_measurements_not_the_numbers` blijven alleen de zes subcommando's. `n=11`, `78,6%` en `2026-08-02` vallen weg (R3).
- **write-handoff (18 tests): naar Go**, met deze uitzonderingen:
  - `test_handoff_writes_new_tokens`: de oude tokens `volgende:`, `wanneer:`, `anker:`, `` `nu` `` en `` `nacht` `` vallen weg (R3). De nieuwe tokens blijven, want `lighthouse protocol` leest ze.
  - `test_handoff_plan_seam_asks_in_chat_text`: dit pint 24 zinnen. Beoordeel elke zin met R1/R2. Port de zinnen die een keuze, een volgorde of een stop sturen. Noteer de rest als ingekort.
  - `test_guard_names_the_pause_form` leest `tests/pack/fixtures/usage_guard/speech.json`, geen skilltekst. Kandidaat *gedekt* door de tests van `internal/usageguard`, die volgens de comment hetzelfde fixture lezen. Controleer met stap f door `/lighthouse-write-handoff ` uit het fixture te halen. Anders naar Go.
- [ ] Volg stappen a–g. Commit: `test(packcontract): issue-skills naar Go, test_skills_issue_flow.py weg (#367)`.

### Taak 4: write-pr, write-issue en close-issue (25 + 15 + 34 tests)

**Files:** Create `writepr_test.go`, `writeissue_test.go` en `closeissue_test.go`. Delete de drie `.py`-bestanden. Eén commit per bronbestand.

Concept:
- **write-pr: naar Go**, met deze uitzonderingen:
  - `test_one_shared_skill_file` gaat weg, gedekt door `TestCorePack`: de bestandslijst bevat geen `write-pr/claude/`.
  - `test_skill_runs_before_pr` (`--voor-pr`) en `test_after_pr_is_gone` worden alleen positief geport. Hun negatieven vallen weg (R3).
  - `test_outcomes_are_headings`: `klopt` en `KON NIET` vallen weg (R3).
  - `test_allowlist_table_covers_the_code` leest `tests/pack/fixtures/evidence/allowlist.json`. Port het naar Go met hetzelfde fixture, of met de constanten uit `internal/evidence` als die geëxporteerd zijn. `TestAllowlistMatchesFixture` bindt fixture en code al aan elkaar.
  - `test_example_row_holds_against_the_template` en `test_example_block_is_a_valid_refs_body` draaien de binary. Port ze in-process via het `internal/evidence`- en `internal/prbody`-pakket (zoek de functie achter `lighthouse evidence run` en `lighthouse pr-body-gate` in `internal/cli`).
    - De `gh`-nep wordt een assertie dat er geen forge-aanroep plaatsvindt, met de seam die `internal/prbody/gate_test.go` al gebruikt.
    - Lukt in-process niet zonder productiewijziging, zet de test dan in `e2e/` (build-tag `e2e`, `go-e2e`-job).
- **write-issue: naar Go.**
  - `test_invariant_check_runs_against_the_forge` draait de binary met een nep-forge (`REAL_LIGHTHOUSE`). Port het in-process, of naar `e2e/` zoals hierboven.
  - `test_variants_differ_only_in_the_generated_line` gaat naar Go: de diff van de varianten is precies de "Generated with"-regel.
- **close-issue: naar Go**, met deze uitzonderingen:
  - Vergelijk `test_close_issue_reports_released_issues` met `TestReleaseLineIsWhatCloseIssueReads` (`internal/unblock/unblock_test.go:74`). Bij overlap is het *gedekt*; controleer met stap f.
  - Vergelijk `test_entry_b_runs_the_evidence_of_merged_refs_prs` met `TestRefsTargetsMatchesCloseIssueJqFilter` (`internal/prbody/inline_twins_test.go:76`). Alleen het deel over het jq-filter is gedekt.

### Taak 5: fix-pr, upgrade, test-standard, audit-worktrees en place-instruction (15 + 17 + 15 + 9 + 9 tests)

**Files:** Create `fixpr_test.go`, `upgrade_test.go`, `teststandard_test.go`, `auditworktrees_test.go` en `placeinstruction_test.go`. Delete de vijf `.py`-bestanden. Eén commit per bronbestand.

Concept:
- **fix-pr: naar Go.** Vergelijk eerst met `TestFixPRDiagnosisBlockIsReadOnly` (`internal/kpi/fixpr_test.go:90`). Wat die al afdekt, is *gedekt*.
- **upgrade: naar Go.** Elke test loopt over `VARIANTS` en telt als één test. `test_agents_md_lists_the_skill` leest `pack/core/instructions/AGENTS.md` en gaat naar Go: de skilllijst in `AGENTS.md` is wat de agent ziet.
- **test-standard: naar Go**, met deze uitzonderingen:
  - `test_frontmatter`: `origin: own` en `source: "#22"` beschermen niets (R2, metadata zonder lezer). Verifieer dat geen Go-code `origin:` leest met `grep -rn '"origin' internal/`. `disable-model-invocation` niet aanwezig blijft (R1: de skill moet model-invoked zijn).
  - `test_no_inspiration_or_source_section` is R3 als het een migratierest bewaakt. Beslis na lezing.
- **audit-worktrees: naar Go.** De tabelkop en de KPI-velden komen nu uit een run van de binary. Roep in Go de functie aan die `lighthouse worktree-audit` gebruikt (`internal/worktreeaudit`) op een tijdelijke repo, of lees de kop uit de constante als die geëxporteerd is. De pin blijft: skilltekst gelijk aan de echte uitvoer.
- **place-instruction: naar Go.**
  - `test_bounding_clause_matches_check_4` roept de check-4-logica aan via `checkinstr.Run` op een tijdelijke boom met twee overlappende beschrijvingen, waarvan één de voorgeschreven vorm draagt.
  - `test_skill_has_no_python3` is gedekt door de requires-test uit taak 2 (python3 in de tekst vereist een requires-regel). Verifieer met stap f.

### Taak 6: `test_adversarial_review_skills.py` (41 tests)

**Files:** Create `internal/packcontract/review_test.go`, Delete `tests/pack/test_adversarial_review_skills.py`.

Concept:
- **naar Go:**
  - verifiëren in plaats van opnieuw reviewen;
  - exit 6 per variant (Claude valt terug, Agents stopt);
  - de pair- en Opus-regels, rondenlimiet, controller-run-secties, spec-dir via config, Engels;
  - `test_variants_keep_their_difference`;
  - `test_pr_template_knows_the_open_review_points_heading` (`plan-review-record` leest die kop);
  - `test_permissions_cover_the_runner`;
  - `test_pinned_paths_exist`.
- **Weg — onderwerp weg (R3):** `test_no_wrapper_scripts_remain` en `test_review_skill_needs_no_python_and_no_wrapper`. Verwijder ook in een test die naar Go gaat elke assertie op `review.sh`, `verify.sh` of `python3`.
- **Kandidaat gedekt:** `test_reviewer_prompt_skips_builder_freedom` en `test_prompts_and_schemas_shared` raken `reviewer-prompt.md`. `TestBuildPromptByteIdentical` (`internal/review/prompt_test.go:26`) leest die prompts. Controleer met stap f of een verwijderde zin daar faalt.
- **Te beoordelen:** `test_no_skill_names_the_adversary` (R1 als blijvende neutraliteit, R2 als migratierest). Beslis na lezing.
- [ ] Volg stappen a–g. Commit: `test(packcontract): review-skills naar Go, test_adversarial_review_skills.py weg (#367)`.

### Taak 7: scout, PR-template en workflow (15 + 8 + 14 tests)

**Files:** Create `scout_test.go`, `prtemplate_test.go` en `workflowunblock_test.go`. Delete de drie `.py`-bestanden.

Concept:
- **scout (`pack/core/agents/issue-impact-scout.md`): naar Go**, met deze uitzonderingen:
  - `test_frontmatter_pins_name_and_model`: het naamdeel is gedekt door `TestScoutNameMatchesPersona`. Port alleen het model.
  - `test_the_agent_file_exists` is gedekt: `TestCorePack` pint `.claude/agents/lighthouse-issue-impact-scout.md` in de bestandslijst. Verifieer dat met stap f.
  - `test_only_the_scout_is_an_agent` is gedekt door dezelfde bestandslijst.
  - `test_impact_pack_heading_is_the_headings_value` gaat naar Go met `config.Variants`/`config.Heading`.
- **PR-template: naar Go.** Vergelijk met `internal/prbody/inline_twins_test.go:80`, dat het template al leest. Wat dat dekt, is *gedekt*.
- **unblock-workflow:**
  - **Pins op de YAML: naar Go**, met `gopkg.in/yaml.v3` en de tekst. `test_every_placeholder_is_a_known_scalar_key` gaat met `config.KindOf` naar Go.
  - In `test_calls_the_binary` vallen `uv` en `unblock_dependents.py` weg (R3).
  - Vergelijk `test_runs_on_from_config`, `test_bot_names_from_config` en `test_default_branch_from_config` eerst met de vultest van `init` (`internal/initcmd`, `packs.Fill`). `packs.Fill` meldt een onbekende placeholder als `missing`.
  - **De vijf `test_get_lighthouse_*`-tests** voeren het shellscript van de stap uit met nep-tools. Volgens test-standard ("Execute shell scripts") blijft dat een uitvoertest. Port ze naar Go: haal de `run:` van de stap uit de YAML, schrijf nep-`uname`/`sha256sum`/`curl` in een `t.TempDir()` op `PATH`, en draai met `exec.Command("bash", "-c", script)`. Het is dezelfde logica als `_get_lighthouse` in het Python-bestand.
  - `test_go_is_set_up_only_for_a_dev_checkout` en `test_the_one_literal_is_the_release_download`: naar Go.

### Taak 8: fragmenten, requires, portable shell en shellcheck (4 + 1 + 3 + 1 tests)

**Files:** Create `internal/packcontract/packfiles_test.go`. Delete `test_fragments.py`, `test_requires_without_python.py`, `test_portable_shell.py` en `test_shell_style.py`.

Concept:
- **`test_requires_without_python`: weg — gedekt.** `TestLanguagePacks` (`internal/packs/core_test.go:105`) pint `tools` per pakket. Voor go, node, shell en expo staat daar geen `python3` of `tomllib`. Mutatie: voeg `python3 = ["go-standard"]` toe aan `pack/go/requires.toml` en verwacht dat `TestLanguagePacks/go` faalt.
- **`test_fragment_is_valid_config`: naar Go.**
  - Voor elk `pack/*/lighthouse.toml.fragment`: `config.Parse([]byte(base+fragment), "lighthouse.toml")` zonder fout. Elke sleutel geeft `config.KindOf(key) != "unknown"`. Controleer de exacte returnwaarde in `internal/config/config.go:234`.
  - Vergelijk eerst met `internal/initcmd/fragment_test.go`. Die gebruikt nep-fragmenten, dus waarschijnlijk niet gedekt.
- **`test_check_catches_a_wrong_type`, `test_check_catches_an_unknown_key` en `test_check_accepts_the_python_default`: weg — beschermt niets.** Ze testen de hulpfunctie `_check` van de test. De Go-port gebruikt direct `config.Parse`, en `internal/config/config_test.go` (`TestIsKnownKey`) dekt die.
- **portable shell (3 tests): naar Go.** Blijvende invariant (#90, macOS). Neem `GNU_ONLY`, `BASH4`, `MARKER`, `PROBE` en `TRAILING_COMMENT` letterlijk over, met Go-`regexp`.
  - Go RE2 kent geen lookbehind: `(?<![\w-])sha256sum\b` wordt `(^|[^\w-])sha256sum\b`.
  - Mutatie: `sed -i 's/x/y/' f` in een `pack/*/hooks/*.sh` moet de test laten falen.
- **shellcheck (1 test): naar Go.** De gepinde shellcheck komt uit `pack/shell/scripts/lib/shellcheck-bin.sh` (`resolve_shellcheck`). Een ontbrekende pin is een fout, geen skip.
  - **Open punt:** draait de `go-test`-job in CI met die shellcheck? Het Python-origineel draaide in de pytest-shards. Controleer met `grep -n shellcheck .github/workflows/ci.yml` en met de log van de shards.
  - Zonder shellcheck in de Go-job: zet `t.Skip` alleen als `CI` leeg is, en voeg de installatie toe aan de job. Een `.github/workflows/`-wijziging pusht de workflow-pusher in een controller-run.

### Taak 9: Afronden

- [ ] **Stap 1:** `grep -rn -E 'test_(skill_contracts|skills_issue_flow|write_pr_skill|write_issue_skill|close_issue_skill|fix_pr_skill|upgrade_skill|test_standard_skill|audit_worktrees_skill|place_instruction_skill|adversarial_review_skills|issue_impact_scout|pr_template|workflow_unblock|fragments|portable_shell|shell_style|requires_without_python)' --include='*.py' --include='*.go' --include='*.yml' --include='*.toml' . | grep -v docs/developer/planning`. Verwacht geen hit.
  - Een hit in `tests/pack/python/test_gate_python.py` is een string-pad; pas het aan naar een bestaand bestand.
  - Andere hits: los ze op in deze taak.
- [ ] **Stap 2: Opnieuw het cachebewijs (AC 3)** zoals taak 1 stap 4, nu op een pin uit taak 3: `Scout: lighthouse-issue-impact-scout` in `pack/core/skills/start-issue/claude/SKILL.md`. Verwacht `(cached)`, dan na de mutatie `FAIL`. Zet de bron terug.
- [ ] **Stap 3: Changelog-fragment** `changelog.d/issue-367-skillcontracten.md`:

  ```markdown
  ---
  type: internal
  ---
  De contracttests op skills, agents, PR-template, workflow, fragmenten en shell onder `pack/` draaien in Go (`internal/packcontract`), na triage; de pytest-bestanden zijn weg.
  ```

- [ ] **Stap 4:** `go test -race -shuffle=on ./...` en verwacht PASS. `golangci-lint run ./...` en verwacht schone uitvoer.
- [ ] **Stap 5:** `uv run pytest -q tests/pack` en verwacht PASS. Dit is de rest: #366, #368 en `tests/pack/python/`.
- [ ] **Stap 6:** Tel de concepttabel op: tests voor (297), naar Go, weg per soort. Zet de totalen onder de tabel. De PR-body neemt de tabel en de totalen over (AC 2), plus de uitvoer van het cachebewijs (AC 3).
- [ ] **Stap 7: Commit:** `chore(packcontract): changelog-fragment en triagetotalen (#367)`.

Merge-volgorde met #366: #366 verplaatst `tests/pack/_testkit.py` en past de importeurs aan. Merget #366 eerst, dan kan `git merge origin/main` een conflict geven in een bestand dat deze branch verwijdert. Houd dan de verwijdering aan (`git rm`). Pas niets aan in de bestanden van #366.

---

## Triagetabel (concept)

De uitvoerder werkt deze tabel per taak bij. De PR-body neemt hem over. Telling: per `def test_`; een geparametriseerde test telt als één.

| Bestand | Tests | Concept | Taak |
|---|---:|---|---|
| `test_skill_contracts.py` | 22 | 15 naar Go (2 samengevoegd met hun zelftest), 2 gedekt, 1 beschermt niets, 1 onderwerp weg | 2 |
| `test_skills_issue_flow.py` | 49 | ~46 naar Go, 1 onderwerp weg, 1 gedekt of naar Go (`speech.json`), 1 ingekort | 3 |
| `test_write_pr_skill.py` | 25 | ~23 naar Go (2 in-process of e2e), 1 gedekt, negatieven R3 binnen tests | 4 |
| `test_write_issue_skill.py` | 15 | 15 naar Go (1 in-process of e2e) | 4 |
| `test_close_issue_skill.py` | 34 | ~32 naar Go, tot 2 gedekt (`unblock`, `prbody`) | 4 |
| `test_fix_pr_skill.py` | 15 | naar Go, minus wat `TestFixPRDiagnosisBlockIsReadOnly` dekt | 5 |
| `test_upgrade_skill.py` | 17 | 17 naar Go | 5 |
| `test_test_standard_skill.py` | 15 | ~14 naar Go, deel van `test_frontmatter` R2, 1 open (R3?) | 5 |
| `test_audit_worktrees_skill.py` | 9 | 9 naar Go (kop/velden uit `internal/worktreeaudit`) | 5 |
| `test_place_instruction_skill.py` | 9 | 8 naar Go, 1 gedekt (requires-test) | 5 |
| `test_adversarial_review_skills.py` | 41 | ~36 naar Go, 2 onderwerp weg, tot 2 gedekt (`review/prompt_test`), 1 open | 6 |
| `test_issue_impact_scout.py` | 15 | ~12 naar Go, 2–3 gedekt (`TestCorePack`, `TestScoutNameMatchesPersona`) | 7 |
| `test_pr_template.py` | 8 | naar Go, minus wat `inline_twins_test` dekt | 7 |
| `test_workflow_unblock.py` | 14 | 14 naar Go (5 als uitvoertest van het stapscript), negatieven R3 | 7 |
| `test_fragments.py` | 4 | 1 naar Go, 3 beschermt niets | 8 |
| `test_requires_without_python.py` | 1 | 1 gedekt (`TestLanguagePacks`) | 8 |
| `test_portable_shell.py` | 3 | 3 naar Go | 8 |
| `test_shell_style.py` | 1 | 1 naar Go (open: shellcheck in de Go-job) | 8 |
| **Totaal** | **297** | voorlopig ~265 naar Go, ~32 weg of open | |

Per test (in te vullen): `bestand | test | bestemming | reden | Go-test of mutatie`.
