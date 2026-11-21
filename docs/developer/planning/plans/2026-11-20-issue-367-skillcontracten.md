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
- De andere 25 `tests/pack/test_*.py` zijn van #366, inclusief `test_skill_scripts_gone.py`, `test_forge_neutral_units.py` en de verhuizing van `_testkit.py`. `tests/pack/{go,shell,node,expo}` is #368. `tests/pack/_testkit.py` en `conftest.py` blijven in deze PR staan.
  - Enige uitzondering: `test_forge_neutral_units.py` importeert `_TOOL_USES` uit `test_skill_contracts.py`, dat taak 2 verwijdert. Taak 2 vervangt in dat bestand alleen die ene import door een letterlijke kopie van de dict. De 6 tests ervan triagert #366.
- `tests/test_domains.py::test_no_go_file_names_a_path_outside_go` faalt als een `.go`-bestand `docs/`, `changelog.d` of `ci/literal-` bevat. Geen van die tekenreeksen komt voor in `internal/packcontract`; schrijf `developer/planning/specs/` zonder `docs/`-voorvoegsel. Een pin die `ci/literal-allowlist.txt` leest (domein `not-go`) gaat niet naar Go.
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
  - Er is minstens één verwijzing, per variant (`t.Run(variant, …)`), zoals `assert refs` per `SKILL.md` in het origineel (`tests/pack/test_skill_contracts.py:459-463`).
- [ ] **Stap 3:** `go test ./internal/packcontract/ -v` en verwacht PASS.
- [ ] **Stap 4: Cachebewijs (AC 3).**
  1. Draai `go test ./internal/packcontract/` twee keer. Verwacht op de tweede regel `(cached)`.
  2. Vervang `lighthouse config forge.kind` in `pack/core/skills/route-issue/SKILL.md` (of de variant waarin het staat) door `lighthouse config forge.kinds`.
  3. Draai opnieuw. Verwacht `FAIL`, niet `(cached)`.
  4. `git checkout -- pack/`.
  (Het bewijs voor een nieuw bestand staat in taak 9 stap 2, want `TestHeadingKeysKnown` bestaat pas na taak 2.)
  Noteer de uitvoer van stappen 1 en 3 voor de PR-body.
- [ ] **Stap 5:** `golangci-lint run ./internal/packcontract/` en verwacht schone uitvoer.
- [ ] **Stap 6: Commit:** `test(packcontract): testpakket voor contracten op pack/, eerste pin (#367)`.

### Taak 2: `test_skill_contracts.py` (22 tests)

**Files:** Modify `internal/packcontract/skills_test.go`, Modify `tests/pack/test_forge_neutral_units.py` (alleen de import van `_TOOL_USES`, zie Global Constraints), Delete `tests/pack/test_skill_contracts.py`.

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
| `test_descriptions_pass_the_overlap_check` | naar Go | Kopieer alle skills naar een tijdelijke `.claude/skills` en `.agents/skills`, zoals `_install_skills`. Roep `checkinstr.Run(root, personal, &out, &errOut)` aan. `checkinstr.Run` geeft exit 2 met lege uitvoer zonder laadbare `lighthouse.toml` (`internal/checkinstr/run.go:71-75`, `config.Load`). Schrijf dus eerst een minimale `lighthouse.toml` (en `.lighthouse/headings.toml`) in de tijdelijke root, zoals `install_core` met `MINIMAL_TOML` (`tests/pack/_testkit.py:703-709`), en zet `t.Setenv("LIGHTHOUSE_TOML", "")`. Assert `rc != 2`, lege `errOut`, en dat `out` geen `overlap without a bounding clause` bevat. Een test die met een overlappende beschrijving (stap e) niet faalt, port je niet. |
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

- [ ] Volg stappen a–g, met deze aanvulling vóór `git rm`: vervang in `tests/pack/test_forge_neutral_units.py:15` de regel `from tests.pack.test_skill_contracts import _TOOL_USES` door een letterlijke kopie van de dict `_TOOL_USES`, en draai `uv run pytest -q tests/pack/test_forge_neutral_units.py` (verwacht PASS). Verder niets in dat bestand.
- [ ] Commit: `test(packcontract): structurele skillcontracten naar Go, test_skill_contracts.py weg (#367)`.

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
  - `test_one_shared_skill_file` gaat weg: beschermt niets (R2). `TestCorePack` dekt het niet: een splitsing in twee varianten blijft groen (taak 4a).
  - `test_skill_runs_before_pr` (`--voor-pr`) en `test_after_pr_is_gone` worden alleen positief geport. Hun negatieven vallen weg (R3).
  - `test_outcomes_are_headings`: `klopt` en `KON NIET` vallen weg (R3).
  - `test_allowlist_table_covers_the_code` leest `tests/pack/fixtures/evidence/allowlist.json`. Port het naar Go met hetzelfde fixture, of met de constanten uit `internal/evidence` als die geëxporteerd zijn. `TestAllowlistMatchesFixture` bindt fixture en code al aan elkaar.
  - `test_example_row_holds_against_the_template` en `test_example_block_is_a_valid_refs_body` draaien de binary. Port ze in-process via het `internal/evidence`- en `internal/prbody`-pakket (zoek de functie achter `lighthouse evidence run` en `lighthouse pr-body-gate` in `internal/cli`).
    - De `gh`-nep wordt een assertie dat er geen forge-aanroep plaatsvindt, met de seam die `internal/prbody/gate_test.go` al gebruikt.
    - Lukt in-process niet zonder productiewijziging, zet de test dan in `e2e/` (build-tag `e2e`, `go-e2e`-job). Een e2e-pin draait niet onder `go test ./...`: noem dat in de PR-body bij AC 3, en houd de tekstpin zelf in `packcontract`.
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
  - `test_go_is_set_up_only_for_a_dev_checkout`: naar Go.
  - `test_the_one_literal_is_the_release_download` leest `ci/literal-allowlist.txt` (`tests/pack/test_workflow_unblock.py:232`), een `not-go`-pad (`tests/test_domains.py:121-137`). Port alleen de assertie op de workflow-YAML; de allowlistkant gaat niet naar Go. Noteer dat in de tabel (*naar Go (ingekort)*).

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
  - `resolve_shellcheck` downloadt de gepinde shellcheck zelf (`pack/shell/scripts/lib/shellcheck-bin.sh:31-41,74-75`), dus de `go-test`-job heeft geen installatiestap en geen workflowwijziging nodig. Roep het script aan met `cmd.Dir` op de repo-root (`../..`, nodig voor `git rev-parse`) en faal op exit ≠ 0. Raak `.github/workflows/` niet aan.

### Taak 9: Afronden

- [ ] **Stap 1:** `grep -rn -E 'test_(skill_contracts|skills_issue_flow|write_pr_skill|write_issue_skill|close_issue_skill|fix_pr_skill|upgrade_skill|test_standard_skill|audit_worktrees_skill|place_instruction_skill|adversarial_review_skills|issue_impact_scout|pr_template|workflow_unblock|fragments|portable_shell|shell_style|requires_without_python)' --include='*.py' --include='*.go' --include='*.yml' --include='*.toml' . | grep -v docs/developer/planning`. Verwacht geen hit.
  - Bekende hits die blijven: een commentaarverwijzing in `internal/prbody/inline_twins_test.go:16` (bestaande Go-test, niet aanraken) en `tests/test_pages_workflow.py:3` (buiten deze issue). Verwijzingen in bestanden van #366 laat je staan.
  - Andere hits: los ze op in deze taak, behalve in bestanden van #366 of bestaande Go-tests.
- [ ] **Stap 2: Opnieuw het cachebewijs (AC 3)** zoals taak 1 stap 4, nu op een pin uit taak 3: `Scout: lighthouse-issue-impact-scout` in `pack/core/skills/start-issue/claude/SKILL.md`. Verwacht `(cached)`, dan na de mutatie `FAIL`. Zet de bron terug.
  - Bewijs ook een **nieuw** bestand: maak `pack/core/skills/route-issue/extra.md` aan met `heading:bestaat_niet`, draai `go test ./internal/packcontract/` en verwacht `FAIL` (`TestHeadingKeysKnown`), niet `(cached)`. Verwijder het bestand.
- [ ] **Stap 3: Changelog-fragment** `changelog.d/issue-367-skillcontracten.md`:

  ```markdown
  ---
  type: internal
  ---
  De contracttests op skills, agents, PR-template, workflow, fragmenten en shell onder `pack/` draaien in Go (`internal/packcontract`), na triage; de pytest-bestanden zijn weg.
  ```

- [ ] **Stap 4:** `go test -race -shuffle=on ./...` en verwacht PASS. `golangci-lint run ./...` en verwacht schone uitvoer.
- [ ] **Stap 5:** `uv run pytest -q tests/pack tests/test_domains.py` en verwacht PASS. `tests/pack` is de rest: #366, #368 en `tests/pack/python/`; `test_domains.py` bewaakt dat het Go-pakket geen `not-go`-pad noemt.
- [ ] **Stap 6:** Tel de concepttabel op: tests voor (297), naar Go, weg per soort. Zet de totalen onder de tabel. De PR-body neemt de tabel en de totalen over (AC 2), plus de uitvoer van het cachebewijs (AC 3).
- [ ] **Stap 7: Commit:** `chore(packcontract): changelog-fragment en triagetotalen (#367)`.

Merge-volgorde met #366: #366 verplaatst `tests/pack/_testkit.py` en past de importeurs aan. Merget #366 eerst, dan kan `git merge origin/main` een conflict geven in een bestand dat deze branch verwijdert. Houd dan de verwijdering aan (`git rm`). Pas niets aan in de bestanden van #366.

---

## Triagetabel (concept)

De uitvoerder werkt deze tabel per taak bij. De PR-body neemt hem over. Telling: per `def test_`; een geparametriseerde test telt als één.

| Bestand | Tests | Concept | Taak |
|---|---:|---|---|
| `test_skill_contracts.py` | 22 | 16 naar Go, 2 zelftests opgegaan in de niet-leeg-assertie, 2 gedekt, 1 beschermt niets, 1 onderwerp weg | 2 |
| `test_skills_issue_flow.py` | 49 | 45 naar Go (waarvan 1 ingekort: de plan-naad), 3 gedekt, 1 onderwerp weg | 3 |
| `test_write_pr_skill.py` | 25 | 24 naar Go (2 in-process, geen e2e), 0 gedekt, 1 beschermt niets, negatieven R3 binnen 8 tests | 4 |
| `test_write_issue_skill.py` | 15 | 15 naar Go (1 als shellblok tegen de echte `lighthouse config`, geen e2e), negatieven R3 binnen 7 tests | 4 |
| `test_close_issue_skill.py` | 34 | 34 naar Go (2 met het jq-filterdeel gedekt door `TestRefsTargetsMatchesCloseIssueJqFilter`), 0 geheel gedekt, negatieven R3 binnen 7 tests | 4 |
| `test_fix_pr_skill.py` | 15 | 15 naar Go (blokaantal gedekt door `TestFixPRDiagnosisBlockIsReadOnly`, pre-push-alinea door `TestOptionalStepsGuarded`), 0 geheel gedekt, negatieven R3 binnen 3 tests | 5 |
| `test_upgrade_skill.py` | 17 | 16 naar Go, 1 gedekt (`test_frontmatter`: `TestCorePack`, `TestEverySkillHasOrigin`; de waarde `own` beschermt niets) | 5 |
| `test_test_standard_skill.py` | 15 | 14 naar Go (`test_frontmatter` ingekort: `name` en `origin` gedekt door `TestCorePack` en `TestEverySkillHasOrigin`, de waarden `own` en `#22` beschermen niets), 1 onderwerp weg (`## Inspiration`) | 5 |
| `test_audit_worktrees_skill.py` | 9 | 8 naar Go (kop, KPI-velden en `cleanup: no — ` uit een echte run van `worktree-audit`), 1 gedekt (`board-sweep`: `TestOptionalStepsGuarded`), negatieven R3 binnen 2 tests | 5 |
| `test_place_instruction_skill.py` | 9 | 8 naar Go (check 4 via `checkinstr.Run`), 1 gedekt (`python3`: `TestRequiresNamesTheSkillPerTool`), negatieven R3 binnen 4 tests | 5 |
| `test_adversarial_review_skills.py` | 41 | 33 naar Go (PR-kop uit `review.PROpenPoints`, exit 6 uit `review.ExitBudgetEmpty`, `bij_rondegrens` uit `repoconfig.LoadFile`), 3 gedekt (`TestBuildPromptByteIdentical`, `TestCorePack` ×2), 1 beschermt niets, 4 onderwerp weg, negatieven R3 binnen 4 tests | 6 |
| `test_issue_impact_scout.py` | 15 | ~12 naar Go, 2–3 gedekt (`TestCorePack`, `TestScoutNameMatchesPersona`) | 7 |
| `test_pr_template.py` | 8 | naar Go, minus wat `inline_twins_test` dekt | 7 |
| `test_workflow_unblock.py` | 14 | 14 naar Go (5 als uitvoertest van het stapscript), negatieven R3 | 7 |
| `test_fragments.py` | 4 | 1 naar Go, 3 beschermt niets | 8 |
| `test_requires_without_python.py` | 1 | 1 gedekt (`TestLanguagePacks`) | 8 |
| `test_portable_shell.py` | 3 | 3 naar Go | 8 |
| `test_shell_style.py` | 1 | 1 naar Go (open: shellcheck in de Go-job) | 8 |
| **Totaal** | **297** | voorlopig ~265 naar Go, ~32 weg of open | |

Per test (in te vullen): `bestand | test | bestemming | reden | Go-test of mutatie`.

### `test_skill_contracts.py` (taak 2, afgerond)

16 naar Go, 2 zelftests opgegaan in een niet-leeg-assertie, 2 gedekt, 1 beschermt niets, 1 onderwerp weg (22). Alle Go-tests staan in `internal/packcontract/skills_test.go`. Elke mutatie is gedraaid: de genoemde test faalde, daarna `git checkout -- pack/` en een lege `git status --short pack/`.

| Bestand | Test | Bestemming | Reden | Go-test of mutatie |
|---|---|---|---|---|
| `test_skill_contracts.py` | `test_frontmatter_name` | weg — gedekt | `packs.Load` weigert een verkeerde `name:` per variant; de enige agent pint `TestScoutNameMatchesPersona`. | `TestCorePack`, `TestLanguagePacks`, `TestScoutNameMatchesPersona`. Mutaties: `name: lighthouse-fix-prs` in `fix-pr/agents/SKILL.md`, `lighthouse-forges` in de gedeelde `forge/SKILL.md`, `lighthouse-python-standards` in `python-standard/claude/SKILL.md`, `lighthouse-impact-scout` in de agent: alle vier FAIL. |
| `test_skill_contracts.py` | `test_variants_both_present` | weg — gedekt | `packs.Load` eist `SKILL.md` of beide varianten. | `TestCorePack`, `TestLanguagePacks`. Mutaties: `fix-pr/agents/SKILL.md` en `python-standard/agents/SKILL.md` verwijderd: FAIL. |
| `test_skill_contracts.py` | `test_referenced_files_exist` | naar Go | Geen geïnstalleerd pad dat nergens heen wijst, behalve met "if present". | `TestReferencedFilesExist`. Mutatie: `.claude/hooks/lighthouse/runs.sh` in `fix-pr/agents/SKILL.md:279`: FAIL. |
| `test_skill_contracts.py` | `test_config_keys_known` | naar Go | Een `lighthouse config <key>` moet een sleutel zijn die `config.IsKnownKey` kent. | `TestConfigKeysKnown`. Mutaties: `labels.blocked_issues` en `forge.kinds` in `route-issue/SKILL.md`: FAIL. |
| `test_skill_contracts.py` | `test_config_re_finds_the_keys` | weg — beschermt niets | Zelftest van de regex (R2), opgegaan in de niet-leeg-assertie van `TestConfigKeysKnown` op de drie sleutels van route-issue. | Mutatie: `lighthouse config labels.needs_shaping` onleesbaar gemaakt: FAIL. |
| `test_skill_contracts.py` | `test_heading_keys_known` | naar Go | Een `heading:<key>` die `headings.toml` mist, laat `lighthouse headings` niets printen. | `TestHeadingKeysKnown`. Mutatie: `heading:evidence_hold` in `close-issue/agents/SKILL.md`: FAIL. |
| `test_skill_contracts.py` | `test_skill_links_resolve` | naar Go | Een skill-link moet bestaan; een externe skill moet op de allowlist staan. | `TestSkillLinksResolve`. Mutaties: `/lighthouse-close-issues` in `write-pr/SKILL.md:151` en `/superpowers:subagent-driven-developments` in `review-plan/agents/SKILL.md:29`: FAIL. |
| `test_skill_contracts.py` | `test_optional_steps_guarded` | naar Go | Wat in paia blijft, staat er alleen "if present" of "skipped", in de alinea of een omsluitende kop. | `TestOptionalStepsGuarded`. Mutaties: "only if present" weg in `audit-worktrees/SKILL.md:39`, en uit de kop B7 van `close-issue/claude/SKILL.md`: FAIL. |
| `test_skill_contracts.py` | `test_planned_skills_absent` | weg — beschermt niets | `PLANNED` is leeg, dus geen mutatie van `pack/` laat de test falen. | — |
| `test_skill_contracts.py` | `test_descriptions_pass_the_overlap_check` | naar Go | Alle skills samen moeten check 4 van `check-instructions` doorstaan; `checkinstr.Run` in een tijdelijke root met minimale `lighthouse.toml`. | `TestDescriptionsPassTheOverlapCheck`. Mutatie: dezelfde beschrijving zonder afbakening in `audit-worktrees` en `write-pr`: FAIL. |
| `test_skill_contracts.py` | `test_requires_names_the_skill_per_tool` | naar Go | `init` meldt een ontbrekend hulpmiddel alleen voor de units die `requires.toml` noemt. | `TestRequiresNamesTheSkillPerTool`. Mutaties: `route-issue` weg uit `core/requires.toml`, `close-issue` weg uit `[forge.github] gh`: FAIL. |
| `test_skill_contracts.py` | `test_tool_patterns_see_python_and_tomllib` | weg — beschermt niets | Zelftest van de patronen (R2), opgegaan in de assertie van `TestRequiresNamesTheSkillPerTool` dat python-standard op `python3` en `tomllib` matcht. | Mutatie: `python3`/`tomllib` hernoemd in beide varianten van python-standard: FAIL. |
| `test_skill_contracts.py` | `test_requires_python_only_where_the_skill_uses_it` | naar Go | Omgekeerde richting: geen `python3` of `tomllib` voor een skill die het niet gebruikt. | `TestRequiresPythonOnlyWhereTheSkillUsesIt`. Mutatie: `python3 = ["write-pr"]` in `core/requires.toml`: FAIL. |
| `test_skill_contracts.py` | `test_every_writing_gh_command_in_the_skills_names_the_repo` | naar Go | Zonder `--repo` kiest gh in een fork de upstream (blijvend). | `TestEveryWritingGhCommandNamesTheRepo`. Mutatie: `--repo "$R"` weg uit `gh issue reopen` in `close-issue/agents/SKILL.md:370`: FAIL. |
| `test_skill_contracts.py` | `test_write_pr_creates_the_pr_through_lighthouse_forge` | naar Go | De vorm die `pr-create-gate` leest. De negatieve `gh pr create`-assertie valt weg: `TestChainSkillsDoNotCallGhForForgeOperations` vangt die (mutatie: een fenced `gh pr create` in write-pr: FAIL). | `TestWritePRCreatesThePRThroughLighthouseForge`. Mutaties: `--body '<body>'` in plaats van `--body-file <path>`, en de regel hernoemd naar `pr open`: FAIL. |
| `test_skill_contracts.py` | `test_chain_skills_do_not_call_gh_for_forge_operations` | naar Go | Forge-neutraal, met de check op verouderde uitzonderingen. | `TestChainSkillsDoNotCallGhForForgeOperations`. Mutaties: `gh issue view` in `start-issue/agents/SKILL.md:31`, en de uitzondering `gh api graphql` weg uit write-pr (stale): FAIL. |
| `test_skill_contracts.py` | `test_chain_skills_point_at_forge_reference` | naar Go | Al in taak 1. | `TestChainSkillsPointAtForgeReference` (taak 1). |
| `test_skill_contracts.py` | `test_forge_skill_has_both_references` | naar Go | Beide referenties hebben dezelfde H2's buiten fences, in dezelfde volgorde. | `TestForgeSkillHasBothReferences`. Mutaties: `## §proxies` in `gitlab.md`, `§ci` en `§workflows` omgedraaid in `github.md`: FAIL. |
| `test_skill_contracts.py` | `test_forge_skill_description_not_self_triggering` | naar Go | lighthouse-forge is een referentie, nooit een startpunt. | `TestForgeSkillDescriptionNotSelfTriggering`. Mutaties: "via another skill" en "only" uit de beschrijving: FAIL. |
| `test_skill_contracts.py` | `test_forge_reference_mentions_no_glab_command` | naar Go | Blijvend: `glab` is geen afhankelijkheid. | `TestForgeReferenceMentionsNoGlabCommand`. Mutaties: `` `glab api` `` in `gitlab.md`, "glab" in `github.md`: FAIL. |
| `test_skill_contracts.py` | `test_forge_skills_name_no_source_issue_number` | naar Go | Blijvend (R3-uitzondering): geen bron-issuenummers. | `TestForgeSkillsNameNoSourceIssueNumber`. Mutatie: `#358` in `start-issue/claude/SKILL.md`: FAIL. |
| `test_skill_contracts.py` | `test_review_skills_call_the_binary_not_python_or_a_wrapper` | weg — onderwerp weg | Negative existence na de migratie naar de binary (R3); `python3` in een skill vangt de requires-test. | Mutatie: `python3` in `review-spec/agents/SKILL.md`: `TestRequiresNamesTheSkillPerTool` FAIL. |

### `test_skills_issue_flow.py` (taak 3, afgerond)

45 naar Go (waarvan 1 ingekort: de plan-naad), 3 gedekt, 1 onderwerp weg (49: route-issue 11, start-issue 18, write-handoff 20). Alle Go-tests staan in `internal/packcontract/issueflow_test.go`, per variant met `t.Run`. Elke mutatie is gedraaid: de genoemde test faalde, daarna `git checkout -- pack/` en een lege `git status --short pack/`. Vervallen negatieven (R3): `board_start.py`, `measure_start_issue_kpi(.py)`, `identity`, `controller_tick`, `n=11`/`78,6%`/`2026-08-02`, `#### Gate 1`, "call that skill, done", "it waits for green", "no push", `volgende:`/`wanneer:`/`anker:`/`` `nu` ``/`` `nacht` ``, "verbatim". Blijvende verboden (R1): `3-blocked`/`3-needs`, `1-loc`/`2-area`, `gh issue close`/`wontfix` in stap 2, en geen scout, worktree of handoff in route-issue.

| Bestand | Test | Bestemming | Reden | Go-test of mutatie |
|---|---|---|---|---|
| `test_skills_issue_flow.py` | `test_gate_0_reads_the_relation` | naar Go | Gate 0 leest de blocker uit de relatie, per forge via §blockers. | `TestRouteIssueGate0ReadsTheRelation`. Mutatie: `§blockers` → `§blocked`: FAIL. |
| `test_skills_issue_flow.py` | `test_gate_0_stops_without_blocked_label` | naar Go | Open blocker zonder label stopt ook; verdicten **label missing**/**unknown**. | `TestRouteIssueGate0StopsWithoutBlockedLabel`. Mutatie: `**label missing**` → `**missing label**`: FAIL. |
| `test_skills_issue_flow.py` | `test_gate_0_routes_on_in_a_controller_run` | naar Go | Controller-run-uitzondering; `blocked_external` stopt altijd; router post niets. | `TestRouteIssueGate0RoutesOnInAControllerRun`. Mutaties: "always stops" → "stops", en `/lighthouse-write-handoff` in gate 0: FAIL. |
| `test_skills_issue_flow.py` | `test_router_invariants_know_the_controller_run` | naar Go | Geen routerregel spreekt de uitzondering van gate 0 tegen. | `TestRouteIssueInvariantsKnowTheControllerRun`. Mutaties: controller-run weg uit de `blocked_issue`-rij, en toegevoegd aan de `blocked_external`-rij: FAIL. |
| `test_skills_issue_flow.py` | `test_route_issue_is_read_only_and_reports_one_line` | naar Go (negatieven deels R3) | De KPI telt de gate-regel; de router blijft read-only. | `TestRouteIssueIsReadOnlyAndReportsOneLine`. Mutaties: `— <reason>` → `: <reason>`, en `lighthouse-issue-impact-scout` in de tekst: FAIL. |
| `test_skills_issue_flow.py` | `test_route_issue_description_survives_the_yaml_parser` | naar Go | Een ongequote ` #610` maakt van de NOTE-zin YAML-commentaar; nu met `gopkg.in/yaml.v3`. | `TestRouteIssueDescriptionSurvivesTheYAMLParser`. Mutatie: quotes om de description weg: FAIL. |
| `test_skills_issue_flow.py` | `test_route_issue_sends_an_open_fog_question_to_4c` | naar Go | Open mijlpaalvraag naar 4c; precies 6 labelrijen. | `TestRouteIssueSendsAnOpenFogQuestionTo4c`. Mutaties: `touches open question` → `open question`, en een 7e labelrij: FAIL. |
| `test_skills_issue_flow.py` | `test_route_issue_reads_labels_from_config` | naar Go | Labels via config, nooit letterlijk (R1-uitzondering). | `TestRouteIssueReadsLabelsFromConfig`. Mutaties: `labels.needs_testing` → `labels.needs_tests`, en `3-blocked` in de tekst: FAIL. |
| `test_skills_issue_flow.py` | `test_route_issue_reads_the_forge_from_config` | weg — gedekt | `forge.kind` uit config pint taak 1 al; `identity` is R3. | `TestChainSkillsPointAtForgeReference`. Mutatie: `lighthouse config forge.kind` → `forge.type`: FAIL. |
| `test_skills_issue_flow.py` | `test_route_issue_guards_what_stays_in_paia` | weg — gedekt | backlog-stand en board-sweep alleen "if present". `TestOptionalStepsGuarded` is iets ruimer: die aanvaardt ook `skipped` of een bewaking in een omsluitende kop. | `TestOptionalStepsGuarded`. Mutaties: "if present" weg bij backlog-stand en bij board-sweep: FAIL. |
| `test_skills_issue_flow.py` | `test_route_issue_names_the_subcommand` | naar Go | `lighthouse measure-start-issue-kpi` leest de gate-regel. | `TestRouteIssueNamesTheSubcommand`. Mutatie: `lighthouse kpi` in plaats van het subcommando: FAIL. |
| `test_skills_issue_flow.py` | `test_start_issue_holds_a_blocked_execution` | naar Go | Gates 1, 2, 4a voeren geblokkeerd niet uit; `stack:`/`blocker:` verrekent. | `TestStartIssueHoldsABlockedExecution`. Mutatie (agents): "Gate `1`, `2` or `4a`" → "Gate `1` or `2`": FAIL. |
| `test_skills_issue_flow.py` | `test_start_issue_reads_legacy_stack` | naar Go | Oude `stapel:`/`blokkade:` tellen als `stack:`. | `TestStartIssueReadsLegacyStack`. Mutatie (agents): `` `stapel:` or `` weg: FAIL. |
| `test_skills_issue_flow.py` | `test_start_issue_delegates_step_4` | naar Go (negatief R3) | Stap 4 roept route-issue aan; `#### Gate 1` niet bewaakte een verhuizing. | `TestStartIssueDelegatesStep4`. Mutatie (claude): "Call the router": FAIL. |
| `test_skills_issue_flow.py` | `test_start_issue_asks_already_done_before_the_worktree` | naar Go | Recept in één call, drie rapportregels, stop en keuze, sluit niets. | `TestStartIssueAsksAlreadyDoneBeforeTheWorktree`. Mutaties: `not measurable` → `unknown` (agents), `FETCH FAILED` → `FETCH ERROR` in het recept (claude), `gh issue close` in stap 2 (agents): FAIL. |
| `test_skills_issue_flow.py` | `test_start_issue_reads_fog_from_the_milestone` | naar Go | DoD, no-gos en open vragen uit de mijlpaal; `fog:`-regel; scout krijgt ze. | `TestStartIssueReadsFogFromTheMilestone`. Mutatie (agents): `fog: <question>` → `open: <question>`: FAIL. |
| `test_skills_issue_flow.py` | `test_start_issue_reads_axes_from_config` | naar Go | Prefixen via config, geen letterlijke `1-loc`/`2-area` (R1-uitzondering). | `TestStartIssueReadsAxesFromConfig`. Mutatie (claude): `labels.area_prefix` → `labels.area`: FAIL. |
| `test_skills_issue_flow.py` | `test_start_issue_skips_board_without_config` | naar Go | Zonder `[forge.board]` meldt het bord en gaat door. | `TestStartIssueSkipsBoardWithoutConfig`. Mutatie (agents): "is not a stop" → "is a stop": FAIL. |
| `test_skills_issue_flow.py` | `test_start_issue_forbids_skipping_without_a_decision` | naar Go | Plan-review overslaan alleen op besluit van de gebruiker. | `TestStartIssueForbidsSkippingWithoutADecision`. Mutatie (claude): "without a decision of the user" weg: FAIL. |
| `test_skills_issue_flow.py` | `test_start_issue_names_the_project_paths` | naar Go | Projectpaden overrulen de superpowers-map. | `TestStartIssueNamesTheProjectPaths`. Mutatie (agents): `paths.plans` → `paths.plan`: FAIL. |
| `test_skills_issue_flow.py` | `test_start_issue_keeps_the_measurements_not_the_numbers` | naar Go (negatieven R3) | Alleen de zes subcommando's; `n=11`, `78,6%`, `2026-08-02` en `measure_start_issue_kpi` zijn R3. | `TestStartIssueKeepsTheMeasurements`. Mutatie (claude): `… gate` → `… gates`: FAIL. |
| `test_skills_issue_flow.py` | `test_start_issue_controller_run_env` | weg — onderwerp weg | `PAIA_CONTROLLER_RUN` verdween bij de migratie (R3). | — |
| `test_skills_issue_flow.py` | `test_both_variants_mention_the_scout` | naar Go | De scout heet bij zijn subagent-type. | `TestStartIssueMentionsTheScout`. Mutatie (agents): `subagent_type: issue-impact-scout`: FAIL. |
| `test_skills_issue_flow.py` | `test_both_variants_carry_the_three_report_lines` | naar Go | `measure-start-issue-kpi scout` telt de drie regels. | `TestStartIssueCarriesTheThreeReportLines`. Mutatie (claude): `Scout: failed — ` → `Scout failed: `: FAIL. |
| `test_skills_issue_flow.py` | `test_the_scout_is_bound_to_gate_four_b_and_c` | naar Go | Alleen 4b/4c; 0, 1, 2 en 4a nooit. Pint de zinnen, niet losse `4a`. | `TestStartIssueBindsTheScoutToGate4bAnd4c`. Mutatie (agents): `4a` weg uit "never call it": FAIL. |
| `test_skills_issue_flow.py` | `test_the_escape_hatch_demands_a_concrete_reason` | naar Go | Overslaan vraagt een concrete reden. Pint de instructiezin, niet de markeerregel die de zin herhaalt. | `TestStartIssueEscapeHatchDemandsAConcreteReason`. Mutatie (claude): `**not a valid reason**` → `**no valid reason**`: FAIL. |
| `test_skills_issue_flow.py` | `test_the_failure_path_is_fail_open` | naar Go | Een mislukte scout stopt de route niet. | `TestStartIssueFailurePathIsFailOpen`. Mutatie (agents): "The route stops.": FAIL. |
| `test_skills_issue_flow.py` | `test_the_handover_has_one_unambiguous_demand` | naar Go (negatief R3) | Vaste verwijzing naar het IMPACT PACK; "verbatim" bewaakte een oude formulering. | `TestStartIssueHandoverHasOneDemand`. Mutatie (claude): `IMPACT PACK: see above`: FAIL. |
| `test_skills_issue_flow.py` | `test_the_agents_variant_names_the_missing_persona` | naar Go | Verschil tussen de varianten (R1). | `TestStartIssueAgentsVariantNamesTheMissingPersona`. Mutaties: `**not simulate**` → `**not imitate**` (agents), `##### Outside Claude Code` in claude: FAIL. |
| `test_skills_issue_flow.py` | `test_handoff_blocked_restart_from_its_own_worktree` | naar Go | Geblokkeerde herstart uit de eigen worktree, niet main. | `TestHandoffBlockedRestartFromItsOwnWorktree`. Mutatie: "worktree of the issue": FAIL. |
| `test_skills_issue_flow.py` | `test_handoff_carries_the_rule` | naar Go | De regel waar een handoff hoort, vóór de routeringstabel. | `TestHandoffCarriesTheRule`. Mutatie: "must judge independently" → "must judge": FAIL. |
| `test_skills_issue_flow.py` | `test_handoff_routes_a_finished_build_to_write_pr` | naar Go | Build klaar → write-pr met `offer`, in beide tabellen. | `TestHandoffRoutesAFinishedBuildToWritePR`. Mutatie: `offer` → `no` in de build-rij: FAIL. |
| `test_skills_issue_flow.py` | `test_handoff_build_row_pushes_after_v2b_first` | naar Go | V2b en push vóór post of aanbod. | `TestHandoffBuildRowPushesAfterV2bFirst`. Mutatie: `V2b` → `V2`: FAIL. |
| `test_skills_issue_flow.py` | `test_handoff_pr_open_row_names_the_close_check` | naar Go (negatief R3) | Sluitcheck pas na de merge van de `Refs`-PR. | `TestHandoffPROpenRowNamesTheCloseCheck`. Mutatie: "of the `Refs` PR" weg: FAIL. |
| `test_skills_issue_flow.py` | `test_handoff_plan_seam_asks_in_chat_text` | naar Go (ingekort) | 17 van 24 zinnen blijven (keuze, volgorde, stop); "Without an issue number" staat in de volle zin met zijn voorwaarde. Weg: 4 al in volgorde gepind door `TestHandoffPlanSeamBlocksBeforeTheQuestion` (Exception-anker, vraag, `My advice:`, executing-plans-blok); 3 R2 (tabelkop, "The Controller column says…", "Only *controller* still asks…"). | `TestHandoffPlanSeamAsksInChatText`. Mutaties: "When in doubt: **interactive**", `choice` → `offer` in de plan-rij, en "Without an issue number (step 1) there is nothing to post:" weg: FAIL. |
| `test_skills_issue_flow.py` | `test_handoff_plan_seam_blocks_before_the_question` | naar Go | Review-blok, interactief blok, dan link, advies, vraag. | `TestHandoffPlanSeamBlocksBeforeTheQuestion`. Mutatie: de twee First-step-blokken omgewisseld: FAIL. |
| `test_skills_issue_flow.py` | `test_handoff_plan_seam_exception_before_controller_post` | naar Go | De uitzondering staat vóór "post at once". | `TestHandoffPlanSeamExceptionBeforeControllerPost`. Mutatie: "(never at `default` above)" weg: FAIL. |
| `test_skills_issue_flow.py` | `test_handoff_post_artefact` | naar Go | `post-artefact` buiten de sandbox, vóór de handoff, blokkeert niet. | `TestHandoffPostArtefact`. Mutatie: "An error blocks the handoff": FAIL. |
| `test_skills_issue_flow.py` | `test_handoff_posts_nothing_after_a_question` | naar Go | Na `lighthouse ask` geen handoff. | `TestHandoffPostsNothingAfterAQuestion`. Mutatie: "except when this run asked": FAIL. |
| `test_skills_issue_flow.py` | `test_handoff_post_artefact_is_in_step_3` | naar Go | `post-artefact` in stap 3. | `TestHandoffPostArtefactIsInStep3`. Mutatie: `<nr>` weg uit de aanroep: FAIL. |
| `test_skills_issue_flow.py` | `test_handoff_pause_form` | naar Go | Pauzevorm: pad, atomair schrijven, chat, hervatvelden. `mkdir -p`/`mv` als zin gepind. | `TestHandoffPauseForm`. Mutatie: "then copy it onto the path": FAIL. |
| `test_skills_issue_flow.py` | `test_handoff_pause_form_skips_phase_side_effects` | naar Go | Pauze slaat fase-neveneffecten over. | `TestHandoffPauseFormSkipsPhaseSideEffects`. Mutatie: `/clear` weg uit de pauzesectie: FAIL. |
| `test_skills_issue_flow.py` | `test_handoff_pause_form_pushes_the_branch` | naar Go (negatief R3) | Push vóór het schrijven, nooit de default branch; "no push" bewaakte de oude tekst. | `TestHandoffPauseFormPushesTheBranch`. Mutatie: "A failed push blocks the pause": FAIL. |
| `test_skills_issue_flow.py` | `test_handoff_controller_mode_pushes_before_the_post` | naar Go | Push vóór de comment, ook zonder nieuw artefact. | `TestHandoffControllerModePushesBeforeThePost`. Mutatie: "posted something": FAIL. |
| `test_skills_issue_flow.py` | `test_guard_names_the_pause_form` | weg — gedekt | Leest het fixture, geen skilltekst; `internal/usageguard` toetst fixture en productie. | `TestSpeechFixtures` (mutatie: `/lighthouse-write-handoff ` uit `speech.json`: FAIL); `decide_test` (mutatie: idem in `speak.go`: FAIL); `fixtures_test` (mutatie: `.handoff.md` → `.md` in `context.go`: FAIL). |
| `test_skills_issue_flow.py` | `test_handoff_knows_lane_native` | naar Go | `lane: native`, op Sonnet, optie *Yes, native*. | `TestHandoffKnowsLaneNative`. Mutatie: "starts it on Sonnet" weg (beide plekken): FAIL. |
| `test_skills_issue_flow.py` | `test_handoff_writes_new_tokens` | naar Go (negatieven R3) | `lighthouse protocol` leest `anchor:`/`next:`/`when:`; oude Nederlandse tokens vallen weg. | `TestHandoffWritesProtocolTokens`. Mutatie: `anchor:` → `anker:` in het postblok: FAIL. |
| `test_skills_issue_flow.py` | `test_handoff_board_and_host_from_config` | naar Go (negatieven R3) | Bord en comment via `lighthouse forge`, scratch gedefinieerd en geketend, host-check. | `TestHandoffBoardAndHostFromConfig`. Mutatie: `&&` → `;` tussen schrijven en posten: FAIL. |
| `test_skills_issue_flow.py` | `test_handoff_prints_the_block_after_the_question` | naar Go | Blok na de vraag, niet ervoor. | `TestHandoffPrintsTheBlockAfterTheQuestion`. Mutatie: "Print the block, then ask": FAIL. |

### `test_write_pr_skill.py` (taak 4a, afgerond)

24 naar Go, 1 beschermt niets (25). Alle Go-tests staan in `internal/packcontract/writepr_test.go`, per variant met `t.Run` (write-pr heeft één gedeelde `SKILL.md`). De twee tests die de binary draaiden, lopen in-process: `evidence.Run` en `prbody.Main` met een `prbody.Env` waarvan `GH` en `Issue` elke aanroep vastleggen; geen productiewijziging, geen e2e. De allowlist-tests lezen `evidence.Allowlist`, niet het fixture. Elke mutatie is gedraaid: de genoemde test faalde, daarna `git checkout -- pack/` (of `internal/`) en een lege `git status --short pack/`. Vervallen negatieven (R3): `--voor-pr`, `` `after PR` ``/`evidence_after_pr`/`closedByPullRequestsReferences`, `.claude/hooks/lib/`, "Mixing is allowed", "last outcome of the commit gate", "as `grep`", `klopt`/`KON NIET`, en in de documentensectie `planned`/`loop.json`/`` `loop` ``/de site-map. Blijvende verboden (R1): `tests/unit`, `Closes #N` in de description en in het voorbeeld, `--comment` en `run_in_background` in V2b.

| Bestand | Test | Bestemming | Reden | Go-test of mutatie |
|---|---|---|---|---|
| `test_write_pr_skill.py` | `test_one_shared_skill_file` | weg — beschermt niets | Eén gedeelde `SKILL.md` of twee varianten is een indelingskeuze die niets leest, en elke write-pr-test pint via `variants` al beide varianten (R2); `packs.Load` weigert alleen `SKILL.md` naast `claude/`, niet een splitsing. | Mutatie: `SKILL.md` gesplitst in `claude/SKILL.md` + `agents/SKILL.md` en de gedeelde weg: `internal/packcontract` en `internal/packs` (incl. `TestCorePack`) blijven groen. |
| `test_write_pr_skill.py` | `test_binding_hard_under_metadata` | naar Go | Check 8 bindt `binding: hard` aan de hook; nu via `yaml.v3`. | `TestWritePRBindingIsHard`. Mutaties: de regel weg, en `binding` uit `metadata:` naar het hoogste niveau: FAIL. |
| `test_write_pr_skill.py` | `test_description_not_cut_off` | naar Go | Een ongequote ` #N` maakt van de NOTE-zin YAML-commentaar. | `TestWritePRDescriptionSurvivesTheYAMLParser`. Mutatie: quotes om de description weg: FAIL. |
| `test_write_pr_skill.py` | `test_skill_runs_before_pr` | naar Go (negatief R3) | Elke `lighthouse evidence run` draagt `--before-pr`, en de binary kent de vlaggen (`cli.NewRoot`). | `TestWritePRRunsEvidenceBeforePR`. Mutaties: `--before-pr` weg uit de V3-zin, `--pre-pr` in het commando, en de vlag hernoemd in `internal/cli/evidence.go`: FAIL. |
| `test_write_pr_skill.py` | `test_names_the_hook` | naar Go | Check 8 vindt de hook via dit pad. | `TestWritePRNamesTheHook`. Mutatie: pad weg in *Counterpart*: FAIL. |
| `test_write_pr_skill.py` | `test_after_pr_is_gone` | naar Go (negatieven R3) | Een criterium dat op de PR wacht is `after merge`; gepind op de verdictrij. | `TestWritePRAfterMergeCoversWhatWaitsOnThePR`. Mutaties: "once the PR exists"-bijzin weg, en `` `after merge` `` → `` `after PR` ``: FAIL. |
| `test_write_pr_skill.py` | `test_verify_pr_evidence_through_the_binary` | naar Go (negatief R3) | Checker en bodycheck via de binary; subcommando's bestaan. | `TestWritePRVerifiesEvidenceThroughTheBinary`. Mutaties: `.claude/hooks/lib/evidence check`, en `pr-gate` in plaats van `pr-body-gate`: FAIL. |
| `test_write_pr_skill.py` | `test_allowlist_table_covers_the_code` | naar Go | Elke vlag uit `evidence.Allowlist` staat in de Allowed-cel van zijn eigen rij (Python telde over de hele tabel, waardoor een `-m` uit de pytest-weigering `wc -m` dekte). | `TestWritePRAllowlistTableCoversTheCode`. Mutaties: `-m` weg bij `wc`, `--grep=`, `-v` bij pytest, `--repo`/`-R`, `issue blockers`, de `issue list`-vlaggen, `` `%` ``, `git show`, en `-L` toegevoegd aan `WC_FLAGS` in `allowlist.go`: FAIL. |
| `test_write_pr_skill.py` | `test_pytest_dir_comes_from_config` | naar Go | Testmap via config, geen letterlijke `tests/unit` (blijvend). | `TestWritePRPytestDirComesFromConfig`. Mutatie: `tests/unit` in plaats van de config-sleutel: FAIL. |
| `test_write_pr_skill.py` | `test_example_row_holds_against_the_template` | naar Go (in-process) | Rij 1 van het voorbeeld klopt tegen het PR-template; `evidence.Run`. | `TestWritePRExampleRowHoldsAgainstTheTemplate`. Mutaties: `'Evidence:'` in het commando, `### Bewijs #` als Result, en `Evidence #` → `Proof #` in het template: FAIL. |
| `test_write_pr_skill.py` | `test_description_names_refs_not_closes` | naar Go | Description noemt `Refs`, nooit `Closes #N` (blijvend). | `TestWritePRDescriptionNamesRefsNotCloses`. Mutatie: `` `Closes #N` with an Evidence table ``: FAIL. |
| `test_write_pr_skill.py` | `test_v3_always_writes_refs` | naar Go (negatief R3) | Altijd `Refs`, rijregels, geen tabel als alles open is. | `TestWritePRV3AlwaysWritesRefs`. Mutaties: "or `Closes #N`", "an empty table", "names the criterion": FAIL. |
| `test_write_pr_skill.py` | `test_example_block_is_a_valid_refs_body` | naar Go (in-process) | `prbody.Main` laat het voorbeeld door als `Refs #1091` zonder de forge te vragen. | `TestWritePRExampleBlockIsAValidRefsBody`. Mutaties: `Closes #1091` (ook: gate vraagt de forge), kolom Result weg, reden `short`: FAIL. |
| `test_write_pr_skill.py` | `test_v2b_runs_in_the_build_session` | naar Go | Build-sessie reviewt; write-pr-run legt een skip vast, ook in de verdicttabel. | `TestWritePRV2bRunsInTheBuildSession`. Mutaties: de override onder de tabel weg, "never reviews" → "reviews only when needed": FAIL. |
| `test_write_pr_skill.py` | `test_v5_hands_off_to_close_issue` | naar Go | Handoff naar close-issue na de merge; controller-run-signalen; V5 na V4. | `TestWritePRV5HandsOffToCloseIssue`. Mutaties: `LIGHTHOUSE_CONTROLLER_RUN` weg, "No PR opened" weg, V5 hernoemd: FAIL. |
| `test_write_pr_skill.py` | `test_v4_gives_a_title_and_writes_the_body_separately` | naar Go | `--title` en `--body-file`, body in een aparte Write-stap, open reviewpunten. | `TestWritePRV4GivesATitleAndWritesTheBodySeparately`. Mutaties: `--title` weg, "in the same Bash call", `open-points`: FAIL. |
| `test_write_pr_skill.py` | `test_dod_does_not_run_the_suite_by_hand_in_claude_code` | naar Go (negatief R3) | Commit-gate bewijst alleen de commit; de suite is CI; buiten Claude Code het configcommando. | `TestWritePRDoDLeavesTheSuiteToCI`. Mutaties: "proves the suite", "do not run the full suite by hand" weg, "Outside Claude Code" hernoemd: FAIL. |
| `test_write_pr_skill.py` | `test_gh_view_demands_a_number` | naar Go | Zonder nummer valt gh terug op de branch. | `TestWritePRGhViewDemandsANumber`. Mutatie: "one number or URL": FAIL. |
| `test_write_pr_skill.py` | `test_git_grep_row_names_what_evidence_refuses` | naar Go (negatief R3) | De weigercel noemt `GREP_VAL` min `GIT_GREP_VAL`. | `TestWritePRGitGrepRowNamesWhatEvidenceRefuses`. Mutaties: `--exclude` weg uit de cel, en `-f` toegevoegd aan `GREP_VAL`: FAIL. |
| `test_write_pr_skill.py` | `test_outcomes_are_headings` | naar Go (negatieven R3) | Uitkomsten via heading-sleutels, gepind op de zin en de exittabel. | `TestWritePROutcomesAreHeadings`. Mutaties: `` `holds` `` in de zin en in rij 0, `NOT CHECKED`, `heading:evidence_table_head` weg: FAIL. |
| `test_write_pr_skill.py` | `test_changelog_fragment_is_opt_in_and_english` | naar Go | Opt-in, eerst kijken wat de branch toevoegt, naamregel, vijf types, Engels, vóór de PR. Paden zonder het mapvoorvoegsel (domeincheck). | `TestWritePRChangelogFragmentIsOptInAndEnglish`. Mutaties: "(if present)", "Otherwise skip", `--diff-filter=A`, "after `lighthouse forge pr create`", `removed` weg, taal i.p.v. Engels, "next `-<n>`": FAIL. |
| `test_write_pr_skill.py` | `test_roborev_step_sits_between_v2_and_the_verdicts` | naar Go | Volgorde V2, V2b, Verdicts. | `TestWritePRRoborevStepSitsBetweenV2AndTheVerdicts`. Mutatie: Verdicts vóór V2b: FAIL. |
| `test_write_pr_skill.py` | `test_roborev_step_carries_the_contract` | naar Go | Commando's, voorgrond-timeout, sandbox, fixregel, push, exitregel; geen `--comment`, geen achtergrond (blijvend). | `TestWritePRRoborevStepCarriesTheContract`. Mutaties: `--agent codex` weg, `120000`, `(<severity>)` weg, `--range` weg, `No issue`-zin weg, exitregel-bijzin weg, `run_in_background`, `--comment`, `git push origin HEAD` weg: FAIL. |
| `test_write_pr_skill.py` | `test_v4_writes_the_marker_and_posts_the_comment` | naar Go | `body-line`, render met `--bron lokaal`, comment via `lighthouse forge`. | `TestWritePRV4WritesTheMarkerAndPostsTheComment`. Mutaties: `--bron lokaal` weg, `gh pr comment`, "add the roborev line": FAIL. |
| `test_write_pr_skill.py` | `test_documents_of_the_linked_issues` | naar Go (negatieven R3) | Taxonomie via config, labels van elke gelinkte issue, beslisregel, skippaden, plek tussen V3 en changelog. | `TestWritePRDocumentsOfTheLinkedIssues`. Mutaties: `labels.md` i.p.v. config, alleen `Closes`, `Refs`: yes, "note it in the body", rc 2 skipt, secties omgewisseld: FAIL. |

### `test_write_issue_skill.py` (taak 4b, afgerond)

15 naar Go (15). Alle Go-tests staan in `internal/packcontract/writeissue_test.go`, per variant met `t.Run`. `test_invariant_check_runs_against_the_forge` draait het bashblok van stap 6 zoals het er staat: een `lighthouse`-stub op een `PATH` met alleen `jq`, `sh` en `cat` beantwoordt `forge issue list --state open` uit een pagina en weigert elke andere argv; elke andere aanroep draait het testbinary zelf als `lighthouse` (`TestMain` in `helpers_test.go`, `cli.Execute`), dus de echte `config`-code zonder `go build` en onder `go test ./...`. Geen e2e: dat draaide niet onder `go test ./...`, en het patroon (testbinary als hulpproces) bestaat al in `internal/runs` en `internal/store`. Overgeslagen op Windows en zonder `bash`/`jq`/`sh`/`cat`, zoals `TestRefsTargetsMatchesCloseIssueJqFilter`. Elke mutatie is gedraaid (in `agents/SKILL.md`, zodat een claude-only-pin zou missen): de genoemde test faalde, daarna `git checkout -- pack/` en een lege `git status --short pack/`. Vervallen negatieven (R3): `wontfix` in stap 0, `wayfinder` in de pitch, `uv run`, `src.controller.identity`, `<area>: <descriptive name>`, `${P:+`, en in 3f de oude zin "If the issue touches something that document describes" plus `loop`/`loop.json`/de site-map. Blijvende verboden (R1): Nederlandse woorden, `#\d{3,}`, `Beelink`, `developer/` (het repo-pad zonder `docs`-voorvoegsel, domeincheck), en de letterlijke labelnamen `intake`/`nexus`/`1-loc`/`2-area`/`3-blocked`/`3-needs`/`Corneel`. `gh issue list --json` in stap 0 vangt `TestChainSkillsDoNotCallGhForForgeOperations` (mutatie: `gh issue list --json number --search` in stap 0: FAIL).

| Bestand | Test | Bestemming | Reden | Go-test of mutatie |
|---|---|---|---|---|
| `test_write_issue_skill.py` | `test_write_issue_sets_relation_before_label` | naar Go | Relatie vóór label, verifiëren uit de lijst, **unknown**, `created_at`-herstel, de vier verdicten; gepind op hele zinnen. | `TestWriteIssueSetsRelationBeforeLabel`. Mutaties: "comes **before**" → "comes first", het `issue block`-commando weg, "not from the summary" weg, **unknown**-uitkomst → "an error", `created_at` zonder starttijd, `**lapsed**` zonder vet: FAIL. |
| `test_write_issue_skill.py` | `test_write_issue_blocking_invariant` | naar Go | Het label via de config-sleutel in het commando zelf, en de rangtabel in volgorde. | `TestWriteIssueBlockingLabelsComeFromConfigInRankOrder`. Mutaties: `--add-label blocked` in plaats van `"$BLOCKED"`, rang 2 en 4 omgewisseld (twee kanten): FAIL. |
| `test_write_issue_skill.py` | `test_write_issue_searches_duplicates_before_the_template` | naar Go (negatief R3) | Begrensde zoekactie, fetch met waarschuwing, git-grep-exitregel, meldregels, drie keuzes. `wontfix` valt weg (R3); `gh issue list` is gedekt (zie boven). | `TestWriteIssueSearchesDuplicatesBeforeTheTemplate`. Mutaties: `.[:5]` weg, `\|\|` → `;`, "every non-zero exit", "at most one line", `not measurable` weg, `**create anyway**` zonder vet: FAIL. |
| `test_write_issue_skill.py` | `test_existing_issue_has_its_own_path` | naar Go | Welke stappen vervallen en welke gelden, edit via `--body-file`, stap 4 nooit. | `TestWriteIssueExistingIssueHasItsOwnPath`. Mutaties: stap 1 `holds`, 3c `drops`, "never `lighthouse forge issue create`" weg, `--body '<body>'`: FAIL. |
| `test_write_issue_skill.py` | `test_existing_issue_lapsed_block_path` | naar Go | Wissel in één edit, na de relatie, slotcontrole altijd. | `TestWriteIssueLapsedBlockPath`. Mutaties: wissel in twee edits, relatie "afterwards", eindcontrole "when a relation is involved": FAIL. |
| `test_write_issue_skill.py` | `test_pitch_has_section_not_yet_decided` | naar Go (negatief R3) | Kopvolgorde DoD, no-gos, not-yet-decided; uitstroom `→ #<nr>`; geen eigen timebox. `wayfinder` valt weg (R3). | `TestWriteIssuePitchHasSectionNotYetDecided`. Mutaties: kop als letterlijke tekst, "a timebox of its own", "the bullet is removed": FAIL. |
| `test_write_issue_skill.py` | `test_task_template_carries_the_acceptance_criteria_heading` | naar Go | `pr-body-gate` en close-issue lezen de AC-kop via `heading:`. | `TestWriteIssueTaskTemplateCarriesTheAcceptanceCriteriaHeading`. Mutatie: `### Acceptance criteria`: FAIL. |
| `test_write_issue_skill.py` | `test_write_issue_taxonomy_from_paths` | naar Go | Taxonomie uit `paths.labels` met terugval, assen uit de prefix-sleutels per stap, geen letterlijke labelnamen (blijvend). | `TestWriteIssueTaxonomyFromPaths`. Mutaties: `cat labels.md`, terugval "any labels", `` `loc-` `` in 3a, `` `area-` `` in 3b (de Python-test bleef hier groen: stap 6 noemt de sleutel ook), `3-blocked-issue` in de rangtabel: FAIL. |
| `test_write_issue_skill.py` | `test_repo_and_board_from_config` | naar Go (negatief R3) | Forge-soort uit config, §board-verwijzing, status via `lighthouse forge`, skip-melding. `src.controller.identity` valt weg (R3). | `TestWriteIssueBoardFromConfig`. Mutaties: `forge.kind` weg, `§board` weg uit de eerste zin (de Python-test bleef groen: de tweede noemt het ook), skip-melding weg: FAIL. |
| `test_write_issue_skill.py` | `test_variants_differ_only_in_the_generated_line` | naar Go | De regelgewijze diff is precies het paar "Generated with Claude Code" / "Generated with Codex"; nu ook in de richting agents → claude. | `TestWriteIssueVariantsDifferOnlyInTheGeneratedLine`. Mutaties: een punt na "Sentence case" in agents, en "Codex" in de claude-regel: FAIL. |
| `test_write_issue_skill.py` | `test_skill_is_english_and_detached` | naar Go (negatief R3) | Engels, geen bron-issuenummers, geen host- of repo-pad (blijvend). `uv run` valt weg (R3, Python-migratie). | `TestWriteIssueIsEnglishAndDetached`. Mutaties: "Wees specifiek, niet vaag", `(#1234)`, een verwijzing naar het repo-pad `developer/x.md`: FAIL. |
| `test_write_issue_skill.py` | `test_write_issue_milestone_prefix_from_config` | naar Go (negatief R3) | Prefix uit `project.milestone_prefix`, meldregel, vorm in de taxonomie, omvang, DoD-regel, poortwachter. `<area>: <descriptive name>` valt weg (R3). | `TestWriteIssueMilestonePrefixFromConfig`. Mutaties: `MP=$(cat prefix)`, "at least two issues", "a guide": FAIL. |
| `test_write_issue_skill.py` | `test_invariant_check_runs_against_the_forge` | naar Go (shellblok, testbinary als `lighthouse`) | Stap 6 zoals geschreven: twee blokkerende labels gemeld, een schone issue niet. | `TestWriteIssueInvariantCheckRunsAgainstTheForge`. Mutaties: `or .blk > 1` weg, `labels.needs_decisions` (de echte `config` weigert), `--state all` (de stub weigert), de `index`-select altijd false: FAIL. |
| `test_write_issue_skill.py` | `test_milestone_create_reads_its_own_prefix` | naar Go (negatief R3) | Het create-blok leest zijn eigen prefix als `MP`, meldt een ontbrekende en zet hem voor de titel. `${P:+` valt weg (R3: de positieve pin op `${MP:+` draagt het). | `TestWriteIssueMilestoneCreateReadsItsOwnPrefix`. Mutaties: de `MP=`-regel → `rc=0`, `${P:+$P }` in de titel: FAIL. |
| `test_write_issue_skill.py` | `test_free_labels_with_a_document` | naar Go (negatieven R3) | Document lezen, label alleen op de `When`-kolom, vóór stap 4, skippad; rij in de bestaande-issue-tabel; `--label` in stap 4. | `TestWriteIssueFreeLabelsWithADocument`. Mutaties: "only if needed", "or when it touches", "after step 4", ", since step 4 drops" weg, `[--label '<free label>']` weg, "No taxonomy file" weg: FAIL. |

### `test_close_issue_skill.py` (taak 4b, afgerond)

34 naar Go (34), geen geheel gedekt. Alle Go-tests staan in `internal/packcontract/closeissue_test.go`, per variant met `t.Run`; de binary-aanroepen met vlaggen (`evidence run`, `worktree-audit --issue`, `board-start`) controleert `cliFlags` tegen `cli.NewRoot` (`board-start` leest zijn argumenten rauw, dus daar alleen het subcommando). Elke mutatie is gedraaid in `agents/SKILL.md`: de genoemde test faalde, daarna `git checkout -- pack/` en een lege `git status --short pack/`.

Gedekt-check (stap f): `TestReleaseLineIsWhatCloseIssueReads` (`internal/unblock`) leest alleen `claude/SKILL.md` en eist alleen dat de regel `Released lane after this close:` ergens staat. Mutaties `startswith("Released:")` in beide varianten, en `dependencies/blocked_by` plus `--remove-label blocked-issue` in beide: de unblock-test bleef groen. Beide close-issue-tests over de lane gaan dus naar Go. `TestRefsTargetsMatchesCloseIssueJqFilter` (`internal/prbody`) dekt het commentaarfilter en de `Refs`-test van beide jq-filters, in beide varianten: mutaties `gsub(...)` weg uit stap 0, weg uit B2a, en de rechtergrens `([^\w/]|$)` weg (agents): FAIL. Die naald valt daarom weg uit de Go-ports van `test_step_zero_stops_on_an_open_refs_pr` en `test_entry_b_runs_the_evidence_of_merged_refs_prs`; de rest van die twee tests gaat naar Go.

Vervallen negatieven (R3): de oude voorrang `` `OPEN` and a node with `state` `OPEN`, **or** `` (de positieve rij pint de nieuwe vorm letterlijk), `FOUT`, `after confirmation`, `never clean up`/`you do not clean up yourself`, `identity` (`src.controller.identity`), `<area>: <name>` en "the title prefix of the milestone". Blijvende verboden (R1): geen `gh issue close` in entry C, geen `AskUserQuestion` in de opruimstap, geen `board not updated` in de alinea van `board: skipped`, en elke `*unblock-dependents.yml` is de geïnstalleerde `lighthouse-unblock-dependents.yml` (Go-regexp zonder lookbehind: alle vermeldingen, met niet-leeg-assertie).

| Bestand | Test | Bestemming | Reden | Go-test of mutatie |
|---|---|---|---|---|
| `test_close_issue_skill.py` | `test_close_issue_reports_released_issues` | naar Go | Wachtende issues uit de blocking-lijst; terugval haalt het label via de config-sleutel weg. Niet gedekt: zie boven. | `TestCloseIssueReportsReleasedIssues`. Mutaties: `dependencies/blocked_by`, `--remove-label blocked-issue`, de configbijzin weg: FAIL. |
| `test_close_issue_skill.py` | `test_close_issue_finds_lighthouse_workflow` | naar Go | De lane onder de geïnstalleerde naam, en de antwoordregel die de lane schrijft. Niet gedekt: zie boven. | `TestCloseIssueFindsTheInstalledUnblockLane`. Mutaties: `--workflow unblock-dependents.yml`, `test -e …/unblock-dependents.yml`, `startswith("Released:")`: FAIL. |
| `test_close_issue_skill.py` | `test_close_issue_bot_from_config` | naar Go | Bot uit `ci.bot_login`, skipregel. | `TestCloseIssueBotFromConfig`. Mutaties: `` `ci-bot` ``, skipmelding weg: FAIL. |
| `test_close_issue_skill.py` | `test_step_zero_waits_for_merge` | naar Go | Meldregel en **stop**; stap 0 vóór entry B. | `TestCloseIssueStepZeroWaitsForMerge`. Mutatie: "and go on" in plaats van "and **stop**": FAIL. |
| `test_close_issue_skill.py` | `test_step_zero_stops_on_an_open_refs_pr` | naar Go (commentaarfilter gedekt) | Tweede query over alle open PR's op headbranch, buiten de sandbox. Het `gsub`-filter dekt `TestRefsTargetsMatchesCloseIssueJqFilter`. | `TestCloseIssueStepZeroStopsOnAnOpenRefsPR`. Mutaties: `--limit 9999` weg, de `headRefName`-tak weg, "run in the sandbox": FAIL. |
| `test_close_issue_skill.py` | `test_step_zero_open_pr_stop_is_scoped_to_an_open_issue` | naar Go (negatief R3) | De stop alleen bij een open issue; de doorvalrij. | `TestCloseIssueStepZeroStopIsScopedToAnOpenIssue`. Mutaties: de oude voorrang zonder haakjes, de doorvalrij zonder tweede query: FAIL. |
| `test_close_issue_skill.py` | `test_entry_b_runs_the_evidence_of_merged_refs_prs` | naar Go (jq-filterdeel gedekt) | Gemergde PR's met Evidence-tabel, nieuwste eerst, één scratchmap, laatste rij per criterium, runner op verse default branch, uitkomsttabel. Commentaarfilter en `Refs`-test dekt `TestRefsTargetsMatchesCloseIssueJqFilter`. | `TestCloseIssueEntryBRunsTheEvidenceOfMergedRefsPRs`. Mutaties: `reverse` weg, de Evidence-select weg, "every row of every merged PR", de celregel weg, `evidence_invalid` weg uit de `not`-rij, een tweede `mktemp -d` per PR, `&&` → `;`: FAIL. |
| `test_close_issue_skill.py` | `test_b2a_sits_between_b2_and_b3` | naar Go | Volgorde B2, B2a, B3. | `TestCloseIssueB2aSitsBetweenB2AndB3`. Mutatie: kop hernoemd naar B3a: FAIL. |
| `test_close_issue_skill.py` | `test_b2a_per_pr_body_fetch_runs_outside_the_sandbox` | naar Go | Bodyfetch per PR buiten de sandbox; nu op de hele zin in plaats van een venster van 120 tekens. | `TestCloseIssueB2aBodyFetchRunsOutsideTheSandbox`. Mutatie: de sandbox-bijzin weg: FAIL. |
| `test_close_issue_skill.py` | `test_b2a_never_pastes_body_text_into_a_command` | naar Go | Bodytekst alleen via `--body-file`. | `TestCloseIssueB2aNeverPastesBodyTextIntoACommand`. Mutatie: "paste it where needed": FAIL. |
| `test_close_issue_skill.py` | `test_b7_includes_b2a` | naar Go | Controllermodus draait B2a. | `TestCloseIssueB7IncludesB2a`. Mutatie: "Step 0 and B1–B4": FAIL. |
| `test_close_issue_skill.py` | `test_entry_c_runs_only_the_runner` | naar Go | Alleen de runner, alleen allowlist-rijen, op de default branch uit config. | `TestCloseIssueEntryCRunsOnlyTheRunner`. Mutaties: "Run the rows.", `sh "$scratch/body.md"` in plaats van de runner: FAIL. |
| `test_close_issue_skill.py` | `test_entry_c_reopens_fail_closed` | naar Go | Eerst de issue, dan het bord; bordfout luid; heading-sleutels. | `TestCloseIssueEntryCReopensFailClosed`. Mutaties: "First the board", de twee regels omgewisseld, "loudly" en de bordmelding weg, "Evidence outside the allowlist" weg: FAIL. |
| `test_close_issue_skill.py` | `test_close_issue_board_skip` | naar Go (negatief R3) | `board: skipped` is geen fout en de alinea meldt geen bordfout (blijvend). `FOUT` valt weg (R3). | `TestCloseIssueBoardSkipIsNoError`. Mutaties: "is an error", en `issue reopened, board not updated` toegevoegd aan de skip-alinea: FAIL. |
| `test_close_issue_skill.py` | `test_entry_c_reports_an_unmeasurable_run_loudly` | naar Go | Exit 3, onbekende exit, onherkende uitvoer of geen `rc=`: niet meetbaar, luid, geen reopen. De losse naald `unknown` werd een zin (ze stond ook in "an unknown exit code"). | `TestCloseIssueEntryCReportsAnUnmeasurableRunLoudly`. Mutaties: alleen "Exit `3`", "loudly to the user" weg: FAIL. |
| `test_close_issue_skill.py` | `test_entry_c_never_closes_itself` | naar Go | Entry C sluit nooit (blijvend verbod) plus de positieve zin. | `TestCloseIssueEntryCNeverClosesItself`. Mutatie: "Entry C runs `gh issue close` when needed": FAIL. |
| `test_close_issue_skill.py` | `test_entry_c_does_not_paste_runner_output_in_a_quoted_line` | naar Go | Runneruitvoer alleen via `run.txt`. | `TestCloseIssueEntryCDoesNotPasteRunnerOutputInAQuotedLine`. Mutaties: `--comment "<runner output>"`, "So paste runner output": FAIL. |
| `test_close_issue_skill.py` | `test_entry_c_catches_the_exit_code_before_the_worktree_remove` | naar Go | `rc=$?` vóór `git worktree remove`, stderr apart; gepind op de hele commandostaart. | `TestCloseIssueEntryCCatchesTheExitCodeBeforeTheWorktreeRemove`. Mutatie: `2>&1` en `rc=$?` na de remove: FAIL. |
| `test_close_issue_skill.py` | `test_entry_c_reopens_only_on_a_recognised_outcome` | naar Go | C3 scheidt een echte uitkomst van een kapotte runner; gepind op de rijen. | `TestCloseIssueEntryCReopensOnlyOnARecognisedOutcome`. Mutaties: rij 1 "anything", traceback-geval weg: FAIL. |
| `test_close_issue_skill.py` | `test_entry_c_measures_the_pr_that_actually_closed_the_issue` | naar Go | `closer` van de laatste `ClosedEvent`; alleen een PR-closer gaat naar C2. | `TestCloseIssueEntryCMeasuresThePRThatActuallyClosedTheIssue`. Mutaties: `first:1`, de laatste node van `closedByPullRequestsReferences`, de melding weg: FAIL. |
| `test_close_issue_skill.py` | `test_entry_c_scratch_is_fresh_and_chained` | naar Go | Verse `mktemp -d` vóór de runner, `&&`-keten, geen `rc=` is `COULD NOT CHECK`. | `TestCloseIssueEntryCScratchIsFreshAndChained`. Mutaties: vaste `/tmp`-map, `&&` → `;`, "that is fine": FAIL. |
| `test_close_issue_skill.py` | `test_entry_c_exit_4_writes_a_comment_file` | naar Go | Exit 4 schrijft een commentaarbestand en post het via `--body-file`. | `TestCloseIssueEntryCExit4WritesACommentFile`. Mutaties: `> <scratch>/comment.md` weg, `--body "$(cat …)"`: FAIL. |
| `test_close_issue_skill.py` | `test_cleanup_reads_the_audit_token` | naar Go | Oordeel uit `lighthouse worktree-audit --issue` (vlag bestaat), tokens `cleanup: yes`/`cleanup: no — <reason>`. | `TestCloseIssueCleanupReadsTheAuditToken`. Mutaties: `--issue` weg, `cleanup: ok`, `cleanup: no` zonder reden: FAIL. |
| `test_close_issue_skill.py` | `test_cleanup_asks_no_question` | naar Go (negatief R3) | Geen vraag, in geen enkele modus; `AskUserQuestion` blijft verboden. `after confirmation` valt weg (R3, oude zin). | `TestCloseIssueCleanupAsksNoQuestion`. Mutaties: "Ask with AskUserQuestion first", "Interactive: ask first.": FAIL. |
| `test_close_issue_skill.py` | `test_cleanup_runs_headless_too` | naar Go (negatieven R3) | B7 ruimt op vóór het sluitcommentaar; headless gaan de regels het commentaar in. | `TestCloseIssueCleanupRunsHeadlessToo`. Mutaties: "After the close, run", "Headless: you do not clean up yourself.": FAIL. |
| `test_close_issue_skill.py` | `test_cleanup_keeps_the_forbidden_lines` | naar Go | De verboden regel, heel. | `TestCloseIssueCleanupKeepsTheForbiddenLines`. Mutatie: alleen `--force`: FAIL. |
| `test_close_issue_skill.py` | `test_cleanup_goes_on_after_a_refusal` | naar Go | Na een weigering door met de volgende branch. | `TestCloseIssueCleanupGoesOnAfterARefusal`. Mutatie: "and stop.": FAIL. |
| `test_close_issue_skill.py` | `test_cleanup_shows_evidence_per_branch` | naar Go | Bewijs vanuit de hoofdcheckout, gepind op het codeblok. | `TestCloseIssueCleanupShowsEvidencePerBranch`. Mutatie: `branch --list` weg uit het blok: FAIL. |
| `test_close_issue_skill.py` | `test_cleanup_never_removes_own_cwd` | naar Go | De eigen cwd blijft. | `TestCloseIssueCleanupNeverRemovesOwnCwd`. Mutatie: "is cleaned up last": FAIL. |
| `test_close_issue_skill.py` | `test_commit_urls_from_forge_repo` | naar Go (negatief R3) | Commit-URL uit `forge.repo`, nooit uit de remote. `identity` valt weg (R3). | `TestCloseIssueCommitURLsFromForgeRepo`. Mutaties: repo uit `gh repo view`, URL zonder `https://github.com/`: FAIL. |
| `test_close_issue_skill.py` | `test_release_flow_only_if_present` | naar Go | Releasestap alleen met `scripts/release.py`, met skipregel. | `TestCloseIssueReleaseFlowOnlyIfPresent`. Mutaties: "(only if present)" weg uit de kop, de `test -e`-check weg: FAIL. |
| `test_close_issue_skill.py` | `test_variants_differ_in_the_instruction_file` | naar Go | De regelgewijze diff is precies het paar `CLAUDE.md`/`AGENTS.md`. | `TestCloseIssueVariantsDifferOnlyInTheInstructionFile`. Mutaties: een punt na "No known bugs" in agents, `AGENTS.md` in claude: FAIL. |
| `test_close_issue_skill.py` | `test_milestone_close_keeps_the_title_form` | naar Go (negatief R3) | Titel kopiëren plus ` - DONE` in dezelfde PATCH. `<area>: <name>` valt weg (R3). | `TestCloseIssueMilestoneCloseKeepsTheTitleForm`. Mutatie: `title='<area>: <name> - DONE'`: FAIL. |
| `test_close_issue_skill.py` | `test_area_of_a_milestone_after_the_prefix` | naar Go (negatief R3) | Gebied = deel vóór `:` na de prefix. "the title prefix of the milestone" valt weg (R3). | `TestCloseIssueAreaOfAMilestoneAfterThePrefix`. Mutatie: terug naar "the title prefix of the milestone": FAIL. |

### `test_fix_pr_skill.py` (taak 5a, afgerond)

15 naar Go (15), geen geheel gedekt. Alle Go-tests staan in `internal/packcontract/fixpr_test.go`, per variant met `t.Run`. De binary-aanroepen met vlaggen (`forge pr view --comments`, `forge pr checks --failed-log`, `roborev-gate record`, `roborev-comment render`, `forge pr comment --body-file`, `measure-fix-pr-kpi`) controleert `cliFlags` tegen `cli.NewRoot`; de KPI-tabel is gebonden aan `kpi.FixPRMetrics` en de marker aan de eerste regel van `roborev.Render`. Elke mutatie is gedraaid in `agents/SKILL.md` (de variantentest ook in `claude/SKILL.md`), in `internal/kpi/fixpr.go`, `internal/roborev/comment.go` of `internal/cli/`: de genoemde test faalde, daarna `git checkout -- pack/ internal/` en een lege `git status --short pack/`.

Gedekt-check (stap f): `TestFixPRDiagnosisBlockIsReadOnly` (`internal/kpi`) dekt dat de diagnosesectie precies één bash-blok heeft (mutatie: het blok vóór `=== checks ===` in tweeën geknipt: FAIL) en dat het blok niet muteert; verder niets, want het leest geen enkele naald van de Python-tests. De helper `fixPRDiagnosis` eist het ene blok toch, omdat de pins anders het verkeerde blok lezen. `TestOptionalStepsGuarded` dekt de pre-push-alinea van `test_pre_push_and_roborev_only_if_present` (mutatie: ", if present" weg na "a pre-push hook", in beide varianten: FAIL); die naald valt weg uit de Go-port.

Vervallen negatieven (R3): `identity` (`src.controller.identity`), `measure_fix_pr_kpi` (het Python-script), `n=9` (oude tabeltekst) en `Verdict:` in het diagnoseblok (de positieve pin op `| .body | @json` draagt "de hele body"). Blijvende verboden (R1): geen `gh ` en geen `|| true` in het diagnoseblok, geen `tests/unit`, en geen variantverschil buiten de drie sandboxregels.

| Bestand | Test | Bestemming | Reden | Go-test of mutatie |
|---|---|---|---|---|
| `test_fix_pr_skill.py` | `test_description_carries_the_measured_triggers` | naar Go | De gemeten triggers (`mistrigger`-KPI) in de YAML-description; nu als hele triggerzinnen. | `TestFixPRDescriptionCarriesTheMeasuredTriggers`. Mutaties: `"fix pr 596"` weg, "merge conflicts" → "merge problems": FAIL. |
| `test_fix_pr_skill.py` | `test_description_carries_an_explicit_negative_boundary` | naar Go | NOTE-grens met close-issue en caveman-review bij naam. | `TestFixPRDescriptionCarriesANegativeBoundary`. Mutaties: "closing the issue is lighthouse-close-issue" weg, "a review of the diff on content is caveman-review" weg: FAIL. |
| `test_fix_pr_skill.py` | `test_the_diagnosis_runs_on_lighthouse_forge` | naar Go (negatief R3; blokaantal gedekt) | Diagnose via `lighthouse forge` met bestaande vlaggen, geen `gh`, geen `\|\| true`, `\|\| echo`-takken (blijvend). `identity` valt weg (R3). | `TestFixPRDiagnosisRunsOnLighthouseForge`. Mutaties: `--comments` weg, `gh pr checks`, `\|\| true` na `tail -15`, `\|\| echo "no checks"` weg, `--failed-log` hernoemd in `internal/cli`: FAIL. |
| `test_fix_pr_skill.py` | `test_every_known_gate_has_a_local_command` | naar Go | Elke bekende gate in volgorde met zijn lokale commando. | `TestFixPREveryKnownGateHasALocalCommand`. Mutaties: Expo-rij weg, ratchet-commando → "run ruff", label "Unit tests": FAIL. |
| `test_fix_pr_skill.py` | `test_fix_pr_uses_test_gate_command` | naar Go | Testcommando uit `[test_gate]`; ratchet en Expo alleen "if present"; geen `tests/unit` (blijvend). | `TestFixPRUsesTheTestGateCommand`. Mutaties: "only if present" weg uit de ratchet-rij, `test -e` weg, `uv run pytest tests/unit` in de Tests-rij: FAIL. |
| `test_fix_pr_skill.py` | `test_pre_push_and_roborev_only_if_present` | naar Go (pre-push-alinea gedekt) | roborev-deel hangt aan `.roborev.toml`, sectie 6 "(only if present)". De pre-push-alinea dekt `TestOptionalStepsGuarded`. | `TestFixPRRoborevOnlyIfPresent`. Mutaties: `if test -e .roborev.toml` → `if true`, kop 6 zonder "(only if present)": FAIL. |
| `test_fix_pr_skill.py` | `test_the_worktree_step_uses_the_head_branch_form` | naar Go | Worktree op de headbranch vanaf origin, `EnterWorktree` uitgesloten; nu het hele commando en de hele zin. | `TestFixPRWorktreeStepUsesTheHeadBranch`. Mutaties: `--track -b "$B" "origin/$B"` weg, de `EnterWorktree`-zin → "Use `EnterWorktree`.": FAIL. |
| `test_fix_pr_skill.py` | `test_the_kpi_table_names_the_measuring_script` | naar Go (negatieven R3) | Elke metric van `kpi.FixPRMetrics` als rij, en niet meer dan die. `measure_fix_pr_kpi` en `n=9` vallen weg (R3). | `TestFixPRKPITableNamesTheMeasuringCommand`. Mutaties: scope-rij weg, een rij `paths` erbij, `leadin` → `lead-in` in `internal/kpi/fixpr.go`: FAIL. |
| `test_fix_pr_skill.py` | `test_description_names_the_roborev_entry` | naar Go | De tweede ingang in de description, als hele bijzin. | `TestFixPRDescriptionNamesTheRoborevEntry`. Mutatie: de roborev-bijzin weg: FAIL. |
| `test_fix_pr_skill.py` | `test_diagnosis_block_shows_the_full_roborev_body` | naar Go (negatief R3) | Selectie op de tag `roborev-pr-comment` (die de dispatcher ook leest), de hele body als JSON-regel, de markerregels. `Verdict:` valt weg (R3). | `TestFixPRDiagnosisShowsTheFullRoborevBody`. Mutaties: `@json` → `split("\n")[0]`, tag → `roborev`, de marker-select → `roborev`: FAIL. |
| `test_fix_pr_skill.py` | `test_roborev_section_carries_the_contract` | naar Go | De marker is de eerste regel van `roborev.Render`; per bevinding receiving-code-review; geen re-review, `roborev fix` of `roborev refine`. | `TestFixPRRoborevSectionCarriesTheContract`. Mutaties: `roborev-processed` in de skill, idem in `internal/roborev/comment.go`, receiving-code-review weg, "`roborev fix` is fine": FAIL. |
| `test_fix_pr_skill.py` | `test_variants_differ_only_in_the_sandbox_wording` | naar Go | De regelgewijze diff is precies de drie sandboxparen; nu ook in de richting agents → claude. | `TestFixPRVariantsDifferOnlyInTheSandboxWording`. Mutaties: een punt na "Known pitfalls" in agents, "outside the sandbox" in de claude-intro: FAIL. |
| `test_fix_pr_skill.py` | `test_roborev_section_renders_the_shared_log` | naar Go | classify, record met `--raw`, render met `--bron ci`, post via `--body-file`, in die volgorde; vlaggen bestaan. | `TestFixPRRoborevSectionRendersTheSharedLog`. Mutaties: `--bron lokaal`, `--raw` weg bij record, record hernoemd, `--bron` hernoemd in `internal/cli/roborev.go`: FAIL. |
| `test_fix_pr_skill.py` | `test_roborev_section_stops_on_an_unreadable_ci_review` | naar Go | Bij `skip` niets renderen of posten en stoppen; hele zin. | `TestFixPRRoborevSectionStopsOnAnUnreadableCIReview`. Mutaties: "post the comment anyway", de stopbijzin weg: FAIL. |
| `test_fix_pr_skill.py` | `test_dispatcher_contract` | naar Go | Vier oorzaken, de tick merget, de dispatcher leest de marker; hele zinnen. | `TestFixPRDispatcherContract`. Mutaties: "This skill merges a green PR.", "a merge that failed" weg, de markerzin weg, de merge-bijzin uit *Not here* weg: FAIL. |

### `test_upgrade_skill.py` (taak 5a, afgerond)

16 naar Go, 1 gedekt (17). Elke Python-test liep over `VARIANTS` en telt als één test; de Go-tests staan in `internal/packcontract/upgrade_test.go`, per variant met `t.Run`. De binary-aanroepen met vlaggen (`version`, `self-update`, `releases --repo`, `init --upgrade --yes`, `check-instructions --print-mirror-exceptions`) controleert `cliFlags` tegen `cli.NewRoot`. De losse naalden (`#v}`, `` `dev`.*stop ``, `ahead.*continue`, `anything else.*stop`, `is empty.*stop`) zijn hele zinnen of hele commandoregels geworden: een regex met `re.S` (`older than`, `ahead`, `anything else`, `is empty`) vond ook een "stop" of "continue" verderop in de tekst. Elke mutatie is gedraaid in `agents/SKILL.md` (de sandboxzin ook in `claude/SKILL.md`), in `pack/core/instructions/AGENTS.md` of in `internal/cli/`: de genoemde test faalde, daarna `git checkout -- pack/ internal/` en een lege `git status --short pack/`. Het commentaar in de literal-allowlist dat naar de Python-test wees, wijst nu naar `TestUpgradeReleaseNotesComeFromTheLighthouseRepo`.

Geen vervallen negatieven: `go install` (alleen `lighthouse self-update` levert een binary) en "elke `--force`-regel zegt never" zijn blijvende verboden (R1).

| Bestand | Test | Bestemming | Reden | Go-test of mutatie |
|---|---|---|---|---|
| `test_upgrade_skill.py` | `test_frontmatter` | weg — gedekt | `name:` weigert `packs.Load`; een ontbrekende `origin` weigert `TestEverySkillHasOrigin` (`internal/pins`). De waarde `own` leest niemand (R2): `pins` gebruikt alleen `upstream`. | `TestCorePack`, `TestEverySkillHasOrigin`. Mutaties: `name: lighthouse-upgrades`, de `origin`-regel weg: FAIL. `origin: extended`: `internal/packs`, `internal/pins`, `internal/packcontract` en `internal/checkinstr` groen. |
| `test_upgrade_skill.py` | `test_reads_both_versions` | naar Go | Repo-versie uit het manifest, binaryversie uit `lighthouse version`; nu de hele toekenningsregels. | `TestUpgradeReadsBothVersions`. Mutaties: `lighthouse.toml` in plaats van het manifest, `git describe` in plaats van `lighthouse version`: FAIL. |
| `test_upgrade_skill.py` | `test_versions_are_compared_without_the_v_prefix` | naar Go | `v` weg en `sort -V`, in stap 1 en in de zin van stap 3. | `TestUpgradeComparesVersionsWithoutTheVPrefix`. Mutaties: `M=${M#v}; B=${B#v};` weg, `sort -V` → `sort`: FAIL. |
| `test_upgrade_skill.py` | `test_dev_build_stops` | naar Go | Een `dev`-build stopt; hele regel. | `TestUpgradeDevBuildStops`. Mutatie: "and stop" weg: FAIL. |
| `test_upgrade_skill.py` | `test_older_binary_stops` | naar Go | Een oudere binary stopt vóór `init --upgrade`; hele regel. | `TestUpgradeOlderBinaryStops`. Mutatie: "and stop" weg: FAIL. |
| `test_upgrade_skill.py` | `test_binary_only_through_self_update_after_consent` | naar Go | Alleen `lighthouse self-update`, gewaarschuwd als hele host, na toestemming; geen `go install` (blijvend). | `TestUpgradeBinaryOnlyThroughSelfUpdateAfterConsent`. Mutaties: de whole-host-waarschuwing weg, een `go install`-regel erbij, "Run it right away": FAIL. |
| `test_upgrade_skill.py` | `test_declined_self_update_continues_only_when_binary_is_ahead` | naar Go | Gelijk stopt met "already current", alleen een voorlopende binary gaat door. | `TestUpgradeDeclinedSelfUpdateContinuesOnlyWhenAhead`. Mutaties: "continue anyway" bij gelijk, "Otherwise continue": FAIL. |
| `test_upgrade_skill.py` | `test_release_notes_come_from_the_lighthouse_repo` | naar Go | Precies één regel noemt het lighthouse-repo (de literal-allowlist staat precies die regel toe); `releases --repo "$L"`. | `TestUpgradeReleaseNotesComeFromTheLighthouseRepo`. Mutaties: het repo letterlijk in het `releases`-commando, `L=someone/lighthouse`, `--repo` hernoemd in `internal/cli`: FAIL. |
| `test_upgrade_skill.py` | `test_never_forces_a_conflict` | naar Go | `refused:` stopt; elke `--force`-regel verbiedt (met hit-telling). | `TestUpgradeNeverForcesAConflict`. Mutaties: "retry with `--force`", "❌ `--force` only when needed": FAIL. |
| `test_upgrade_skill.py` | `test_evidence_in_order` | naar Go | Testgate, check-instructions, mirror-uitzonderingen, `No changes.`; stop bij het eerste rood. | `TestUpgradeEvidenceInOrder`. Mutaties: check-instructions vóór de testgate, de stopzin weg, `No changes.` weg, `--print-mirror-exceptions` hernoemd in `internal/cli`: FAIL. |
| `test_upgrade_skill.py` | `test_commit_holds_only_what_init_wrote` | naar Go | `git status` alleen wat `init` schreef, anders stop; het commitonderwerp. | `TestUpgradeCommitHoldsOnlyWhatInitWrote`. Mutaties: "commit it along", onderwerp `chore: upgrade lighthouse`: FAIL. |
| `test_upgrade_skill.py` | `test_init_applies_only_after_consent_with_yes` | naar Go | Droogloop, dan toestemming, dan `--yes`; testgate met `LIGHTHOUSE_GATE_TMP` zoals de commit-gate. | `TestUpgradeInitAppliesOnlyAfterConsentWithYes`. Mutaties: `--yes` op de eerste aanroep, "Then tell the user", `LIGHTHOUSE_GATE_TMP` weg: FAIL. |
| `test_upgrade_skill.py` | `test_push_and_pr_after_consent_through_write_pr` | naar Go | Push en PR na toestemming via write-pr; hele zin. | `TestUpgradePushAndPRAfterConsentThroughWritePR`. Mutatie: "with `lighthouse forge pr create`": FAIL. |
| `test_upgrade_skill.py` | `test_agents_md_lists_the_skill` | naar Go | De skilllijst in het geïnstalleerde `AGENTS.md`-blok is wat de agent ziet. | `TestUpgradeAgentsMDListsTheSkill`. Mutatie: de regel weg uit `pack/core/instructions/AGENTS.md`: FAIL. |
| `test_upgrade_skill.py` | `test_network_steps_name_the_sandbox_and_report_a_failed_fetch` | naar Go | Stap 2 en 3 zeggen dat ze het netwerk nodig hebben, met de sandboxzin per variant; een mislukte fetch wordt gemeld. | `TestUpgradeNetworkStepsNameTheSandboxAndReportAFailedFetch`. Mutaties: sandboxbijzin weg in stap 3 (claude) en stap 2 (agents), "then go on without notes": FAIL. |
| `test_upgrade_skill.py` | `test_empty_version_stops_before_comparing` | naar Go | Een lege versie stopt vóór elke vergelijking; hele regel. | `TestUpgradeEmptyVersionStopsBeforeComparing`. Mutatie: "and compare anyway": FAIL. |
| `test_upgrade_skill.py` | `test_empty_test_gate_is_no_evidence` | naar Go | Een lege of ontbrekende testgate is geen bewijs; `bash -c ""` nooit groen. | `TestUpgradeEmptyTestGateIsNoEvidence`. Mutatie: "count it as green": FAIL. |

### `test_test_standard_skill.py` (taak 5b, afgerond)

14 naar Go, 1 onderwerp weg (15). De Go-tests staan in `internal/packcontract/teststandard_test.go`; de skill heeft één gedeelde `SKILL.md`, die per variant met `t.Run` telt. De losse woorden (`writing`, `mock`, `in the chat`, `lasting invariant`, `helper, loop or builder`, …) zijn hele zinnen of hele opsommingsregels geworden, zodat een weggehaalde bijzin of een aangeplakte uitzondering faalt. Elke mutatie is gedraaid in `pack/core/skills/test-standard/SKILL.md`: de genoemde test faalde, daarna `git checkout -- pack/` en een lege `git status --short pack/`.

Vervallen: de waarden in `test_frontmatter` (`origin: own`, `source: "#22"`) en `test_no_inspiration_or_source_section`. Blijvende verboden (R1): `disable-model-invocation`, de stacktermen (`Playwright`, `PostgreSQL`, …) en Nederlandse woorden.

| Bestand | Test | Bestemming | Reden | Go-test of mutatie |
|---|---|---|---|---|
| `test_test_standard_skill.py` | `test_frontmatter` | naar Go (ingekort) | Geen `disable-model-invocation`: de skill moet model-invoked zijn (R1). `name:` weigert `packs.Load`, een ontbrekende `origin` weigert `TestEverySkillHasOrigin`. De waarden `own` en `#22` leest niemand (R2; `pins` gebruikt alleen `upstream`). | `TestTestStandardIsModelInvoked`. Mutatie: `disable-model-invocation: true` erbij: FAIL. `name: lighthouse-test-standards`: `TestCorePack` FAIL; de `origin`-regel weg: `TestEverySkillHasOrigin` FAIL. `origin: extended` en de `source`-regel weg: `internal/packs`, `internal/pins`, `internal/packcontract`, `internal/checkinstr` groen. |
| `test_test_standard_skill.py` | `test_description_triggers_on_the_work_and_on_planning` | naar Go | De beschrijving laadt de skill bij het werk en bij het plannen; nu hele zinsdelen. | `TestTestStandardDescriptionTriggersOnTheWorkAndOnPlanning`. Mutaties: "or reviewing" weg, "a test strategy, before any file is open" → "a release": FAIL. |
| `test_test_standard_skill.py` | `test_description_bounds_tdd_and_the_language_skills` | naar Go | De NOTE stuurt de tooling van één taal naar de taalskill; hele zin. Het TDD-deel draagt `test_description_makes_tdd_a_companion`. | `TestTestStandardDescriptionBoundsTheLanguageSkills`. Mutatie: `lighthouse-go-standard or` weg: FAIL. |
| `test_test_standard_skill.py` | `test_core_question_and_mutation_check` | naar Go | De kernvraag letterlijk, en de mutatiecheck met zijn eis "at least one test must fail". | `TestTestStandardCoreQuestionAndMutationCheck`. Mutaties: "turns this test red?" → "makes this test pass?", de kop hernoemd, "For a mutation you like, a test may fail": FAIL. |
| `test_test_standard_skill.py` | `test_contract_tests_are_a_kind_of_their_own` | naar Go | Contracttests lezen tekst; een verbod alleen voor een blijvende invariant; hele zinnen. | `TestTestStandardContractTestsAreAKindOfTheirOwn`. Mutaties: de migratiezin → "It is fine too when …", ", or any removal" aangeplakt: FAIL. |
| `test_test_standard_skill.py` | `test_classification_sentence_and_classes` | naar Go | De vorm van de classificatiezin en een tabelrij per klasse. | `TestTestStandardClassificationSentenceAndClasses`. Mutaties: "At risk: <the code>", de rij `hook/gate` weg: FAIL. |
| `test_test_standard_skill.py` | `test_language_neutral` | naar Go | Blijvende invariant: de skill geldt voor elke taal, dus geen stacktermen. | `TestTestStandardIsLanguageNeutral`. Mutatie: ", for example with Playwright": FAIL. |
| `test_test_standard_skill.py` | `test_english` | naar Go | Blijvende invariant: instructies in het Engels. | `TestTestStandardIsEnglish`. Mutatie: "Dit is niet een test." erbij: FAIL. |
| `test_test_standard_skill.py` | `test_no_inspiration_or_source_section` | weg — onderwerp weg | De bronnensectie verdween in #325 (`docs(test-standard): geen overgenomen zinsdelen, geen bronnensectie`); de herkomst staat nu in `metadata.origin`. Een "staat er nog steeds niet in" na die migratie (R3). | — |
| `test_test_standard_skill.py` | `test_red_flags_catch_hidden_tautologies` | naar Go | Vijf red flags voor verborgen tautologieën; nu de hele regels. | `TestTestStandardRedFlagsCatchHiddenTautologies`. Mutaties: de coverage-regel weg, ", which is fine for small tests" aangeplakt: FAIL. |
| `test_test_standard_skill.py` | `test_core_question_says_where_the_answer_goes` | naar Go | Het antwoord op de kernvraag gaat in de chat; hele zin. | `TestTestStandardCoreQuestionSaysWhereTheAnswerGoes`. Mutatie: "in the chat" → "in the commit message": FAIL. |
| `test_test_standard_skill.py` | `test_classification_sentence_place_is_said_once` | naar Go | Eén plek: vóór de eerste edit, in de chat, niet in commit of PR-body; telling 1. | `TestTestStandardClassificationSentencePlaceIsSaidOnce`. Mutaties: "in the chat or the PR body", een tweede "Write it before the first edit.": FAIL. |
| `test_test_standard_skill.py` | `test_called_mock_is_fine_when_the_call_is_the_contract` | naar Go | De uitzondering op de mock-red-flag; hele regel. | `TestTestStandardCalledMockIsFineWhenTheCallIsTheContract`. Mutatie: de unless-bijzin weg: FAIL. |
| `test_test_standard_skill.py` | `test_hook_messages_are_asserted_from_a_run` | naar Go | Een hookmelding uit de uitvoer van een run, nooit uit de bron; hele zin. | `TestTestStandardHookMessagesAreAssertedFromARun`. Mutatie: "never from the source" → "or from the source": FAIL. |
| `test_test_standard_skill.py` | `test_description_makes_tdd_a_companion` | naar Go | TDD is de metgezel die bepaalt wanneer; hele zin. | `TestTestStandardDescriptionMakesTDDACompanion`. Mutatie: ", which replaces this skill.": FAIL. |

### `test_audit_worktrees_skill.py` (taak 5b, afgerond)

8 naar Go, 1 gedekt (9). De Go-tests staan in `internal/packcontract/auditworktrees_test.go`, per variant met `t.Run` (één gedeelde `SKILL.md`). De tabelkop, de KPI-velden en de regel `cleanup: no — <reason>` komen uit een run van `lighthouse worktree-audit` (de testbinary als lighthouse, via `TestMain`) in een verse repo met een kale `origin` en een `lighthouse.toml` zonder `forge.repo`, zoals de Python-test. `cleanup: yes` vraagt een PR-lookup op een forge; die letterlijke regel bewaken de renderfixtures (`internal/worktreeaudit`, twins). Elke mutatie is gedraaid in `pack/core/skills/audit-worktrees/SKILL.md` of in `internal/` (`cli/worktreeaudit.go`, `worktreeaudit/render.go`, `config/config.go`): de genoemde test faalde, daarna teruggezet en een lege `git status --short pack/ internal/`.

Vervallen negatieven (R3): `uv run` en `python3` (het oude `worktree_audit.py`; `python3` vraagt bovendien een requires-regel: een `python3 -m audit`-regel erbij laat `TestRequiresNamesTheSkillPerTool` falen) en `PROTECTED_REFS  = (` (de vaste lijst vóór `[worktree_audit]`). Blijvend (R1): de bronrepo-namen, `#\d{3,}` en Nederlandse woorden.

| Bestand | Test | Bestemming | Reden | Go-test of mutatie |
|---|---|---|---|---|
| `test_audit_worktrees_skill.py` | `test_every_view_runs_the_binary` | naar Go | Zes commandoregels, nu hele regels in het bash-blok; de vlaggen bestaan op `worktree-audit` (`cliFlags`). | `TestAuditWorktreesEveryViewRunsTheBinary`. Mutaties: de `--origin`-regel weg, `--kpi` → `--kpis`, `--sessions` hernoemd in `internal/cli/worktreeaudit.go`: FAIL. |
| `test_audit_worktrees_skill.py` | `test_only_view_names_the_cleanup_token` | naar Go | De tokens voor close-issue; `cleanup: no — ` ook uit een run op de beschermde standaardbranch; hele zin. | `TestAuditWorktreesOnlyViewNamesTheCleanupToken`. Mutaties: `` `cleanup: no` `` zonder reden, `lighthouse-fix-pr` als lezer, `"cleanup: no: "` in `render.go`: FAIL. |
| `test_audit_worktrees_skill.py` | `test_table_header_is_the_commands` | naar Go | De kop in *Output format* is de eerste regel van `--cleanup`. | `TestAuditWorktreesTableHeaderIsTheCommands`. Mutaties: `live?` weg uit de skill, `dirty tree` → `dirty` in `render.go`: FAIL. |
| `test_audit_worktrees_skill.py` | `test_kpi_fields_are_the_commands` | naar Go | De velden van `--kpi` (vier, in volgorde) staan elk als rij in de KPI-tabel. | `TestAuditWorktreesKPIFieldsAreTheCommands`. Mutaties: `remote_branches=` → `remote=` in de skill, `warn=` → `warnings=` in `render.go`: FAIL. |
| `test_audit_worktrees_skill.py` | `test_protect_list_from_config` | naar Go | De standaardbranch altijd beschermd, de rest uit `[worktree_audit]`; de sleutels kent `config` (`IsKnownKey`). `PROTECTED_REFS  = (` vervalt (R3). | `TestAuditWorktreesProtectListFromConfig`. Mutaties: "protected when you list it", `[audit]`, `protected_paths` hernoemd in `internal/config/config.go`: FAIL. |
| `test_audit_worktrees_skill.py` | `test_worktree_audit_no_opencode` | naar Go | De opencode-lane en `port-registry` blijven in paia: alleen met "if present", met hit-telling. | `TestAuditWorktreesOpencodeOnlyIfPresent`. Mutaties: "only if present" weg bij `worktree-run.sh`, een losse opencode-alinea erbij: FAIL. |
| `test_audit_worktrees_skill.py` | `test_board_sweep_only_if_present` | weg — gedekt | `optionalRe` van `TestOptionalStepsGuarded` kent `board-sweep` en loopt over elke core-unit. | `TestOptionalStepsGuarded`. Mutatie: "(board-sweep, only if present)" → "(board-sweep)": FAIL. |
| `test_audit_worktrees_skill.py` | `test_skill_is_detached_from_the_source_repo` | naar Go | Blijvende invariant: geen bronrepo, -paden, -host of -issuenummers. | `TestAuditWorktreesIsDetachedFromTheSourceRepo`. Mutaties: "See #401", "On deploy/beelink": FAIL. |
| `test_audit_worktrees_skill.py` | `test_skill_is_english` | naar Go | Blijvende invariant: instructies in het Engels. | `TestAuditWorktreesIsEnglish`. Mutatie: "Dit is geen test." erbij: FAIL. |

### `test_place_instruction_skill.py` (taak 5b, afgerond)

8 naar Go, 1 gedekt (9). De Go-tests staan in `internal/packcontract/placeinstruction_test.go`, per variant met `t.Run` (één gedeelde `SKILL.md`). De vorm van de afbakeningszin gaat door `checkinstr.Run` op een tijdelijke boom met twee beschrijvingen die zeven triggerwoorden delen: zonder zin moet check 4 de `FAIL`-regel geven (anders slaagt de test leeg), met de ingevulde vorm de regel `ok    no unbounded overlap between descriptions`. De checknummers komen uit de `// Check <n>:`-koppen in `internal/checkinstr`, zoals de Python-test ze las; de vaste set `GENERIC` is daarin opgegaan. Elke mutatie is gedraaid in `pack/core/skills/place-instruction/SKILL.md` of in `internal/` (`cli/root.go`, `checkinstr/overlap.go`, `checkinstr/run.go`): de genoemde test faalde, daarna teruggezet en een lege `git status --short pack/ internal/`.

Vervallen negatieven (R3): `check-instructions.sh` en `scripts/hooks/check-instructies.sh` (het script vóór de Go-check), `paia-` (het oude prefix, nu `lighthouse config project.skill_prefix`), `uv run`, en `domain:`/`source:` in de frontmatter (paia-velden; `source` mag volgens de skill zelf onder `metadata:`). `check 9`/`12`/`13` in de detach-lijst zijn opgegaan in `TestPlaceInstructionNamesOnlyChecksTheCommandCarries`. Blijvend (R1): de bronrepo-woorden, `#\d{3,}` en Nederlandse woorden.

| Bestand | Test | Bestemming | Reden | Go-test of mutatie |
|---|---|---|---|---|
| `test_place_instruction_skill.py` | `test_instrument_lists_generic_checks_only` | naar Go | Elk genoemd checknummer bestaat in `check-instructions`; geen projecteigen check (6, 9, 12, 13); minstens één hit. | `TestPlaceInstructionNamesOnlyChecksTheCommandCarries`. Mutaties: "Check 7" → "Check 9", "(check 5)" → "(check 13)", elk "check N" weg: FAIL. |
| `test_place_instruction_skill.py` | `test_validates_with_the_lighthouse_command` | naar Go | Stap 7 valideert met het commando; nu het hele blok; het subcommando bestaat (`cliFlags`). Scriptnamen vervallen (R3). | `TestPlaceInstructionValidatesWithTheLighthouseCommand`. Mutaties: blok → `.claude/hooks/check-instructions.sh`, de registratie van `check-instructions` in `internal/cli/root.go` weg: FAIL. |
| `test_place_instruction_skill.py` | `test_prefix_and_ceiling_from_config` | naar Go | Prefix en plafond uit `lighthouse config`; hele regels; sleutels bekend (`IsKnownKey`). `paia-` vervalt (R3). | `TestPlaceInstructionPrefixAndCeilingFromConfig`. Mutaties: prefix → `` `paia-` ``, plafond → "(3000)": FAIL. |
| `test_place_instruction_skill.py` | `test_skill_has_no_python3` | weg — gedekt | `python3` in een skill vraagt een requires-regel die de skill noemt; place-instruction staat er niet onder. | `TestRequiresNamesTheSkillPerTool`. Mutatie: "Run `python3 -m check` first." in stap 7: FAIL. |
| `test_place_instruction_skill.py` | `test_bounding_clause_matches_check_4` | naar Go | De voorgeschreven vorm, ingevuld, laat check 4 slagen op een overlap die zonder zin faalt; via `checkinstr.Run`, niet via de bron van `scopeWords`. | `TestPlaceInstructionBoundingClausePassesCheck4`. Mutaties: vorm `NOTE — X: see Y`, `NOTE — not for X`, vorm zonder backticks, `"instead"`/`"not for"` weg uit `scopeWords`, `checkOverlap` niet aangeroepen in `run.go`: FAIL. |
| `test_place_instruction_skill.py` | `test_skill_is_detached_from_the_source_repo` | naar Go | Blijvende invariant: geen bronhost, -paden, -woorden of -issuenummers. `uv run` vervalt (R3), `check 9/12/13` dekt de eerste test. | `TestPlaceInstructionIsDetachedFromTheSourceRepo`. Mutaties: "See #325.", "On the Beelink host.", "The vault holds it.": FAIL. |
| `test_place_instruction_skill.py` | `test_frontmatter_keeps_disable_model_invocation_top_level` | naar Go (ingekort) | `disable-model-invocation: true` top-level: Claude Code leest het daar. `domain:`/`source:` vervallen (R3). | `TestPlaceInstructionIsCalledByNameOnly`. Mutaties: de regel onder `metadata:`, de regel weg: FAIL. |
| `test_place_instruction_skill.py` | `test_skill_is_english` | naar Go | Blijvende invariant: instructies in het Engels. | `TestPlaceInstructionIsEnglish`. Mutatie: "Dit is geen test." erbij: FAIL. |
| `test_place_instruction_skill.py` | `test_naming_convention_per_form` | naar Go | Vijf naamvormen als hele tabelrijen, de check-14-zin, en stap 5 leidt de soort af uit de naam. | `TestPlaceInstructionNamingConventionPerForm`. Mutaties: de agent-rij weg, "a check anywhere.", "the name carries nothing": FAIL. |

### `test_adversarial_review_skills.py` (taak 6, afgerond)

33 naar Go, 3 gedekt (12, 35, 38), 1 beschermt niets (11), 4 onderwerp weg (2, 7, 34, 39). De Go-tests staan in `internal/packcontract/review_test.go`, per skill en variant met `t.Run` (`claude/` en `agents/` apart). Waar de skill een waarde van de binary noemt, komt die uit de code: exit 6 uit `review.ExitBudgetEmpty`, de velden `reason` en `reset_at` uit de JSON van `usage.BudgetOutcome`, de melding "nothing to accept" uit `review.ErrNoOpenPoints`, de PR-kop uit een echte `review.PROpenPoints` op een document met een blok van `review.RenderOpen`, `bij_rondegrens` uit `repoconfig.LoadFile`, en elk subcommando en elke vlag (`review-run`, `review-pair blind|merge`, `plan-review-record start|write|accept-open`, `agent-audit`) via `cliFlags`. Elke `lighthouse review-run`-aanroep in een skill gaat met al zijn vlaggen door `cliFlags`, met een niet-leeg-assertie. Pins zijn hele zinnen (geplet met `flat`), zodat een geschrapte voorwaarde of een aangeplakte uitzondering faalt. Elke mutatie is gedraaid in `pack/core/` (skill, prompt, template of `permissions.json`): de genoemde test faalde, daarna `git checkout -- pack/` en een lege `git status --short pack/`.

Vervallen negatieven (R3): `open_vraag` (paia-categorie; de besluitenset bewaakt `plan-review-record write`), `spec-adversary` (de agent is weg; de skills doen de retry zelf), `reden` (Nederlandse veldnaam), `review-pair status` (subcommando bestaat niet meer), `PAIA_CONTROLLER_RUN`, `uv run python scripts/`, `accepteer-open` (nu `accept-open`, gebonden via `cliFlags`), `review.sh`/`verify.sh`/`python3`/`review_pair.py`/`plan_review_record.py`, en de oude namen `spec-document-reviewer-prompt`/`self-review`/`SKILL.md:119`. Blijvend (R1): de bronrepo-woorden, `#\d{3,}`/`(#\d+)`, Nederlandse woorden, het letterlijke spec- en plan-pad, `review-pair blind` en de Opus-secties niet in `agents`, `codex_same_family` niet in `claude`.

| Bestand | Test | Bestemming | Reden | Go-test of mutatie |
|---|---|---|---|---|
| `test_adversarial_review_skills.py` | `test_review_verifies_instead_of_rereviewing` | naar Go | Ronde ≥ 2 verifieert alleen, alleen na een geaccepteerde fix; MINORs starten geen ronde; elk geaccepteerd ID geverifieerd. Hele zinnen per skill; `--verify` bestaat. | `TestReviewVerifiesInsteadOfRereviewing`. Mutaties: "MINORs never start a round, unless one is security.", "`defer` does not count." weg, "a round-4 candidate if needed.": FAIL. |
| `test_adversarial_review_skills.py` | `test_plan_review_has_no_open_question_category` | weg — onderwerp weg | `open_vraag` was een paia-categorie; de besluiten die `write` aanvaardt staan in `internal/review/record.go`. | R3. |
| `test_adversarial_review_skills.py` | `test_skills_know_budget_exit_6` | naar Go | Exit 6 en de velden `reason`/`reset_at` van `<OUT>.budget.json`, gebonden aan `review.ExitBudgetEmpty` en `usage.BudgetOutcome`. `reden` vervalt (R3). | `TestReviewExit6FallsBackInClaudeAndStopsInAgents`. Mutaties: zie de drie rijen hieronder. |
| `test_adversarial_review_skills.py` | `test_claude_skills_fall_back_at_exit_6` | naar Go | Claude valt bij exit 6 meteen terug op Opus, zonder vraag. | `TestReviewExit6FallsBackInClaudeAndStopsInAgents`. Mutatie: "For the last two ask first." (spec/claude): FAIL. |
| `test_adversarial_review_skills.py` | `test_plan_failure_table_has_exit_6` | naar Go | De rij van de faaltabel voor exit 6, als hele rij. | `TestReviewExit6FallsBackInClaudeAndStopsInAgents`. Mutatie: de rij weg (plan/claude): FAIL. |
| `test_adversarial_review_skills.py` | `test_agents_skills_stop_at_exit_6` | naar Go | De agents-variant stopt vóór de ronde, uitkomst `aborted`; hele zin per skill. | `TestReviewExit6FallsBackInClaudeAndStopsInAgents`. Mutatie: " and stop **before** the round" weg (plan/agents): FAIL. |
| `test_adversarial_review_skills.py` | `test_no_skill_names_the_adversary` | weg — onderwerp weg | `lighthouse-spec-adversary` is verwijderd (changelog); het verbod bewaakt die migratie, geen blijvende neutraliteit. | R3. |
| `test_adversarial_review_skills.py` | `test_retry_is_inline_codex` | naar Go | Het retry-commando precies één keer, in de alinea met `run_in_background` en `dangerouslyDisableSandbox`; het log nooit lezen. | `TestReviewRetryIsInlineCodex`. Mutatie: "`run_in_background: true` and " weg (plan/claude): FAIL. |
| `test_adversarial_review_skills.py` | `test_spec_skill_maps_the_plan_retry` | naar Go | De spec-skill legt de retry uit de plantabel op zijn eigen stap 2. | `TestReviewRetryIsInlineCodex`. Mutatie: de "Where that table says"-zin weg (spec/claude): FAIL. |
| `test_adversarial_review_skills.py` | `test_retry_skips_exit_6` | naar Go | Nooit de retry herhalen, ook niet bij exit 6; hele zin. | `TestReviewRetryIsInlineCodex`. Mutatie: "exit 6 is retried once, no other exit.": FAIL. |
| `test_adversarial_review_skills.py` | `test_spec_review_motivates_against_builder_check` | weg — beschermt niets | Positief: uitleg in *Why this skill exists*, stuurt geen stap (R2); de drie stapels zelf zijn `TestReviewSpecGateSortsThreePiles`. Negatief: oude superpowers-namen (R3). | R2. |
| `test_adversarial_review_skills.py` | `test_reviewer_prompt_skips_builder_freedom` | weg — gedekt | `reviewer-prompt.md` is byte voor byte vastgelegd in de fixtures van `review-run`. | `TestBuildPromptByteIdentical`. Mutaties: ", unless the spec is small." achter "never to Ground truth or Gaps", de filterzin weg: FAIL. |
| `test_adversarial_review_skills.py` | `test_spec_gate_sorts_three_piles` | naar Go | Drie stapels als hele zinnen, de markering, hoogstens vier vragen, elke MINOR een eigen regel. | `TestReviewSpecGateSortsThreePiles`. Mutatie: "at most six" (spec/agents): FAIL. |
| `test_adversarial_review_skills.py` | `test_claude_spec_gate_asks_through_the_controller_only_if_present` | naar Go | Per variant hoe de gebruikersstapel gevraagd wordt; in Claude via de controller alleen als die er is. | `TestReviewSpecGateSortsThreePiles`. Mutatie: "(if present)" weg (spec/claude): FAIL. |
| `test_adversarial_review_skills.py` | `test_claude_plan_skill_runs_the_pair_in_round_one` | naar Go | Blind paar: schone werkboom, kale `DONE`-regel, niets lezen vóór `merge`, blinde besluiten, `merge`, `--shadow-model`; subcommando's via `cliFlags`. | `TestReviewClaudeRunsThePairInRoundOne`. Mutatie: " and never fill it in yourself" weg (plan/claude): FAIL. |
| `test_adversarial_review_skills.py` | `test_claude_spec_skill_records_and_pairs` | naar Go | `start --kind spec`, `write`, de besluitenlijst met `defer`/`minor_yes`, de blinde besluiten. | `TestReviewClaudeRunsThePairInRoundOne`. Mutatie: " Otherwise `blind` refuses." weg (spec/claude): FAIL. |
| `test_adversarial_review_skills.py` | `test_agents_skills_say_the_pair_is_claude_code_only` | naar Go | Hele alinea "Paired round 1: Claude Code only"; `review-pair blind` niet in agents (blijvend variantverschil). | `TestReviewAgentsPairIsClaudeCodeOnly`. Mutatie: `lighthouse review-pair blind` in spec/agents: FAIL. |
| `test_adversarial_review_skills.py` | `test_agents_spec_skill_records_with_its_own_skill_dir` | naar Go | De hele `start`-regel (`--kind spec` bij review-spec; zonder die vlag telt `start` als plan) en het hele `write`-commando met `--skill-dir .agents/...`, nu voor beide skills; `start --kind` en `write --skill-dir` bestaan. | `TestReviewAgentsPairIsClaudeCodeOnly`. Mutaties: `--kind spec` weg uit `start` (spec/agents), `--kind spec` erbij (plan/agents), `--skill-dir` weg uit het `write`-blok (plan/agents): FAIL. |
| `test_adversarial_review_skills.py` | `test_claude_skills_pair_and_verify_on_opus` | naar Go | Opus-dispatch met `model: "opus"` en de verifier-prompt vanaf ronde 2; altijd Opus voor verificatie. `review-pair status` vervalt (R3). | `TestReviewClaudePairsAndVerifiesOnOpus`. Mutaties: `model: "sonnet"` (plan/claude), "Opus only when Codex still has budget" (spec/claude): FAIL. |
| `test_adversarial_review_skills.py` | `test_variants_keep_their_difference` | naar Go | Controller-run, paar en Opus-dispatch alleen in claude; `codex_same_family` alleen in agents en bekend bij `write --review-mode`. | `TestReviewVariantsKeepTheirDifference`. Mutaties: `## Opus dispatch` in spec/agents, `codex_same_family` → `codex` (plan/agents): FAIL. |
| `test_adversarial_review_skills.py` | `test_round_limit_records_and_carries_on` | naar Go | `accept-open`, het blok onderaan, exit 2 met de melding uit `review.ErrNoOpenPoints`, exit 4, nooit een vraag; hele zinnen. | `TestReviewRoundLimitRecordsAndCarriesOn`. Mutaties: ", never a question" weg (spec/agents), melding ingekort (plan/agents): FAIL. |
| `test_adversarial_review_skills.py` | `test_spec_decision_in_round_3_does_not_stop_the_chain` | naar Go | Uitzondering voor `spec` in ronde 3, als hele tabelcel. | `TestReviewRoundLimitRecordsAndCarriesOn`. Mutatie: de uitzondering weg (plan/claude): FAIL. |
| `test_adversarial_review_skills.py` | `test_pr_template_knows_the_open_review_points_heading` | naar Go | De kop waaronder `pr-open-points` schrijft, uit een echte `review.PROpenPoints`, staat als regel in de PR-template. | `TestReviewPRTemplateKnowsTheOpenReviewPointsHeading`. Mutatie: "## Open points" in de template: FAIL. |
| `test_adversarial_review_skills.py` | `test_controller_run_section_is_guarded` | naar Go | Sectie vóór `## Workflow`, aanwezigheidswacht, herkenning, regels (de no-`codex`-regel als hele zin per skill); `bij_rondegrens` gelezen door `repoconfig`. `PAIA_CONTROLLER_RUN` vervalt (R3). | `TestReviewControllerRunSectionIsGuarded`. Mutaties: "; otherwise this section is skipped" weg (spec/claude), sectie na de workflow (plan/claude), ", unless the user is online." achter de no-`codex`-regel (beide): FAIL. |
| `test_adversarial_review_skills.py` | `test_plan_controller_run_section` | naar Go | De drie specvragen, *Adjust the spec*/*Carry on*, de run wijzigt de spec niet zelf. | `TestReviewControllerRunSectionIsGuarded`. Mutatie: "The run never changes the spec materially by itself." weg: FAIL. |
| `test_adversarial_review_skills.py` | `test_plan_round_1_carries_the_controller_wait_rule` | naar Go | De wachtregel in ronde 1, als hele zin binnen die sectie. | `TestReviewControllerRunSectionIsGuarded`. Mutatie: "end an answer": FAIL. |
| `test_adversarial_review_skills.py` | `test_spec_controller_run_section` | naar Go | Handoffs per uitkomst, `--outcome aborted`, geen tweede `write`, geen nieuwe ronde. | `TestReviewControllerRunSectionIsGuarded`. Mutatie: "A second `write` is fine." in plaats van "No second `write`, no `accept-open`." (spec/claude): FAIL. |
| `test_adversarial_review_skills.py` | `test_spec_review_next_window_only_interactive` | naar Go | Interactief draait writing-plans pas in het volgende venster; hele zin. | `TestReviewControllerRunSectionIsGuarded`. Mutatie: "runs in this window": FAIL. |
| `test_adversarial_review_skills.py` | `test_agents_skills_know_no_controller_run` | naar Go | Controller-runs alleen in Claude Code; hele zin. | `TestReviewControllerRunSectionIsGuarded`. Mutatie: "Controller runs exist in every harness" (plan/agents): FAIL. |
| `test_adversarial_review_skills.py` | `test_skill_is_detached_from_the_source_repo` | naar Go | Blijvende invariant: geen bronhost, -paden of -issuenummers. `uv run python scripts/` en `accepteer-open` vervallen (R3). | `TestReviewIsDetachedFromTheSourceRepo`. Mutatie: "(#412)" (plan/claude): FAIL. |
| `test_adversarial_review_skills.py` | `test_claude_skills_name_the_always_on_host` | naar Go | De host heet `host.toml`, als hele exit-3-rij; nu voor alle vier de varianten. | `TestReviewIsDetachedFromTheSourceRepo`. Mutatie: "not on the Beelink" (spec/agents): FAIL. |
| `test_adversarial_review_skills.py` | `test_spec_dir_from_config` | naar Go | Spec- en plan-map uit `lighthouse config` (sleutels bekend via `IsKnownKey`), nooit letterlijk; nu ook review-plan. | `TestReviewDirectoriesFromConfig`. Mutatie: het letterlijke spec-pad in spec/claude: FAIL. |
| `test_adversarial_review_skills.py` | `test_permissions_cover_the_runner` | naar Go | Precies de twee allow-regels; beide subcommando's bestaan. | `TestReviewPermissionsCoverTheRunner`. Mutatie: de `agent-audit`-regel weg: FAIL. |
| `test_adversarial_review_skills.py` | `test_no_wrapper_scripts_remain` | weg — onderwerp weg | `review.sh`/`verify.sh` zijn vervangen door `lighthouse review-run`. | R3. |
| `test_adversarial_review_skills.py` | `test_prompts_and_schemas_shared` | weg — gedekt | `TestCorePack` legt de geïnstalleerde prompts en schema's per variant vast, en `packs.Load` weigert iets anders dan `SKILL.md` in `claude/`/`agents/`. | `TestCorePack`. Mutaties: `reviewer-prompt.md` gekopieerd naar `review-spec/claude/`, `verification.schema.json` weg, `verifier-prompt.md` weg: FAIL. |
| `test_adversarial_review_skills.py` | `test_shared_files_are_english` | naar Go | Blijvende invariant: de gedeelde prompts in het Engels. | `TestReviewIsEnglish`. Mutatie: "Dit is het begin." in `review-plan/verifier-prompt.md`: FAIL. |
| `test_adversarial_review_skills.py` | `test_skill_is_english` | naar Go | Blijvende invariant: instructies in het Engels. | `TestReviewIsEnglish`. Mutatie: "Dit is het begin." in spec/agents: FAIL. |
| `test_adversarial_review_skills.py` | `test_pinned_paths_exist` | weg — gedekt | De vier `SKILL.md`'s legt `TestCorePack` vast; `permissions.json` leest `TestReviewPermissionsCoverTheRunner`. | `TestCorePack`, `TestReviewPermissionsCoverTheRunner`. Mutaties: `review-spec/agents/SKILL.md` weg, `permissions.json` weg: FAIL. |
| `test_adversarial_review_skills.py` | `test_review_skill_needs_no_python_and_no_wrapper` | weg — onderwerp weg | `python3`, `review.sh`, `verify.sh`, `review_pair.py`, `plan_review_record.py`: de migratie naar Go is af. | R3. |
| `test_adversarial_review_skills.py` | `test_claude_skill_calls_the_runner_with_kind_and_verify` | naar Go | Claude roept de runner met `--kind` en `--verify`; elke vlag van elke aanroep bestaat (`cliFlags`). | `TestReviewSkillCallsTheRunnerWithKindAndVerify`. Mutatie: `--fast` op een aanroep (spec/claude): FAIL. |
| `test_adversarial_review_skills.py` | `test_agents_skill_passes_its_own_skill_dir` | naar Go | Agents geeft zijn eigen `--skill-dir`, ook bij `--verify`. | `TestReviewSkillCallsTheRunnerWithKindAndVerify`. Mutatie: `--verify` weg uit de verificatie-aanroep (plan/agents): FAIL. |
