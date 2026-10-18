# M0 (b): rest van `pack/core`, vertaald, en lighthouse op zijn eigen `pack/core` — implementatieplan (#7)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Alle core-bestanden uit paia staan vertaald en losgetrokken in `pack/core`, `lighthouse init` installeert ze, en lighthouse draait zijn eigen `pack/core` in plaats van de bootstrap van (a0).

**Architecture:** Zes PR's na elkaar, elk groen op zichzelf: (1) het installatiemechanisme leert skills, templates, workflows, permissies en `host.toml`; (2) de Python-lib (configlezer, koppentabel, comment-protocol, hookhelpers); (3) de hooks; (4) de scripts en templates; (5) de skills en agents, vertaald; (6) `lighthouse init` op lighthouse zelf. Elk bestand uit paia volgt hetzelfde *verhuisrecept*: eerst een letterlijke kopie in een eigen commit, dan de aanpassing in de volgende, zodat de reviewer per bestand ziet wat er stond en wat er staat.

**Tech Stack:** Go 1.26 met de libraries uit (a); Python ≥ 3.11 voor scripts, Python 3.9-syntaxis voor `pack/core/lib/`; bash 3.2-compatibel voor hooks; `uv` + `pytest` voor de Python-lane.

**Spec:** [`2026-10-17-m0-pakket-design.md`](../specs/2026-10-17-m0-pakket-design.md), secties *Inventaris*, *Grensgevallen*, *Taal*, *Portabiliteit* en *Opdeling (b)*; besluiten 7, 8, 13, 14, 15 en 16. Invoer: de inventaris in het eerste comment van #7 (hieronder "inventaris §n"). Plan (a): [`2026-10-17-m0a-skelet-en-mechanisme.md`](2026-10-17-m0a-skelet-en-mechanisme.md).

**Bron:** paia op `33833601349a8b79a1d1b2ac9077cc495a0d2b37` (hieronder `SRC`). Gecontroleerd op 2026-10-18: tussen `SRC` en `origin/main` van paia is geen enkel bronbestand uit dit plan gewijzigd. Elke taak kopieert met `git -C <paia> show SRC:<pad>`.

## Ontwerpkeuzes

Deze punten beslist de spec niet, of de inventaris (§2) legt ze open. Elk punt is een voorstel; wijs aan wat anders moet vóór de uitvoering.

1. **Zes PR's, in volgorde.** Elke PR verwijst met `Refs #7` naar het issue; de zesde sluit het met `Closes #7`. Een PR start vanaf `main` nadat de vorige gemerged is. Branches: `issue-7-b1-mechanism`, `issue-7-b2-lib`, `issue-7-b3-hooks`, `issue-7-b4-scripts`, `issue-7-b5-skills`, `issue-7-b6-dogfood`. Dit plan landt in PR 1.
2. **Verhuisrecept: kopie-commit, dan aanpas-commit.** De kopie-commit zet het paia-bestand en zijn paia-test letterlijk op het doelpad (`chore(core): <naam> letterlijk uit paia`). De aanpas-commit doet alles uit het recept. De literal-grep en de tests mogen op de kopie-commit rood zijn; de CI draait alleen op de kop van de PR.
3. **Wat vertaald wordt.** Engels worden: skills, agents, hookmeldingen, het instructieblok, CLI-help en elke uitvoer die een mens of een skill leest. CLI-subcommando's en -vlaggen worden Engels, want Engelse skills roepen ze aan (`accepteer-open` → `accept-open`, `--heropen` → `--reopen`, `--voor-pr` → `--before-pr`). **Niet** vertaald in (b): interne namen, commentaar en docstrings. Dat zou de diff van elk bestand vullen met ruis en de vergelijking "wat stond er, wat staat er" onleesbaar maken. Dat komt onder *Verbeterpunten*. (a) deed hetzelfde met `git_invocations.py`.
4. **Twee soorten tokens.** (a) Tekst die in een GitHub-artefact landt (issue, PR, comment) en later gelezen wordt, staat in `headings.toml`: schrijven in `[project] language`, lezen accepteert elke variant. (b) Machinetokens, zoals de sleutels van het comment-protocol, script-uitvoer die een skill parset, JSON-velden en de marker, worden vaste Engelse tokens. Sleutels in comments die al op GitHub staan, lezen we ook in hun oude Nederlandse vorm (tabel *Protocoltokens*). Script-uitvoer leest de oude vorm niet, want schrijver en lezer verhuizen samen.
5. **Eén token voor "kon niet controleren"** (inventaris §2.1): de eerste stderr-regel van elke gate is `<gate>: COULD NOT CHECK — <reden>`, en `bash_writes.classify` matcht op `COULD NOT CHECK` in die eerste regel, voor elke gate. Ook `git_invocations.py` gaat over: `<onbekend>` → `<unknown>`, `<nieuw>` → `<new>`.
6. **Lib-code draait als bestand op zijn pad**, net als de guard in (a) (ontwerpkeuze 6 daar). Een skill roept `python3 .lighthouse/lib/lighthouse_lib/config.py forge.repo` aan, een script `python3 .lighthouse/scripts/<naam>.py`. Elke lib-module met een CLI en elk script zet bij het starten zijn eigen lib-map (`.lighthouse/lib`) vooraan in `sys.path`. Dat is de map van het bestand zelf, niet de repo-root. Daarna importeert het `lighthouse_lib.<x>`.
7. **De vertaling van `identity`:** `repo` → `forge.repo`, `owner` → `forge.owner`, `name` → `forge.name`, `project-number` → `forge.board.project_number`, `project-id` → `forge.board.project_id`, `status-field-id` → `forge.board.status_field_id`, `status-option X` → `forge.board.status_options.X`, `priority-field-id` → `forge.board.priority_field_id`, `priority-option X` → `forge.board.priority_options.X`, `host check` → `host-check`. Een skill leest rc 1 ("not set") bij een `forge.board.*`-sleutel als "geen board, stap overgeslagen" en meldt dat (besluit 16). Bij rc 2 stopt hij.
8. **Defaults in de configlezer.** Elke nieuwe sleutel uit tabel *Configsleutels* heeft een default in `config.py`, behalve de bot-sleutels onder `[ci]`. Zo werkt `pack/core` in een repo met alleen `[project]` en `[forge]`.
9. **`[labels]` noemt alleen labels die code of een skill bij naam gebruikt.** De rest van de labeltaxonomie van `write-issue` (inventaris §7, ongeveer 60% van de skill) is projectinhoud. Die staat in het bestand van `[paths] label_taxonomy`, zoals besluit 14 dat voor tenets en areas regelt. Geen `[labels.axes.*]`.
10. **Test-gate zonder `[test_gate] command`** (inventaris §2.3): de commit gaat door, met de melding `pre-commit-tests: no [test_gate] command in lighthouse.toml — nothing was tested` via het berichtkanaal dat de hook al heeft. Een kapotte config (rc 2) blokkeert met COULD NOT CHECK. Het pytest-specifieke deel (exitcodes 3/4/5, de xdist-hint) wordt configuratie: `[test_gate] unchecked_exit_codes`. De hint verhuist in (c) naar `pack/python`.
11. **Ontbrekend taalpakket in `bash-write-gates`** (inventaris §2.2): staat de gate van een extensie niet in `.claude/hooks/lighthouse/`, dan geeft `post` de melding `bash-write-gates: <bestand> not checked — <gate> is not installed (pack/<pakket>)` en geen exit 2. In (b) geldt dat voor `.py` en `.sh`, want `pack/python` en `pack/shell` komen pas in (c).
12. **`codex_usage.py` gaat half mee** (inventaris §2.11): alleen `budget` en `read`, met de drempels uit `[codex_usage]`. `gate`, `_parkeer` en `schrijf_marker` blijven in paia tot M1. (d) laat paia's `usage_window.py` het verhuisde deel laden.
13. **Workflows krijgen hun waarden bij het kopiëren.** In `pack/core/workflows/*.yml` staat een plekhouder `@@<sleutel>@@` (bijvoorbeeld `@@ci.runs_on@@`); `init` vult hem uit de configlezer, met dezelfde defaults. Ontbreekt een sleutel zonder default, dan installeert `init` die workflow niet en meldt hij welke sleutel ontbreekt. De hash in het manifest is die van het ingevulde bestand. De workflow heet in de projectrepo `lighthouse-unblock-dependents.yml`, en `close-issue` zoekt hem onder die naam (inventaris §2.9).
14. **Permissies als eenheid** (inventaris §2.5): `pack/core/permissions.json` (`{"allow": [...]}`) levert per regel een eenheid van de soort `permission`, met `Path ".claude/settings.json"` en als `ID` de regel. `init` voegt een ontbrekende regel achteraan in `permissions.allow` toe en haalt een regel weg die het manifest noemt maar het pakket niet meer levert. Andere regels blijven staan, op hun plek.
15. **Skills in het pakket:** `pack/core/skills/<naam>/` met de gedeelde bestanden, plus `claude/SKILL.md` en `agents/SKILL.md`, of één `SKILL.md` als de varianten gelijk zijn. `init` legt ze neer in `.claude/skills/lighthouse-<naam>/` en `.agents/skills/lighthouse-<naam>/`. De frontmatter-`name` moet `lighthouse-<naam>` zijn. Agents blijven `agents/<naam>.md` (zoals in (a)), alleen voor Claude.
16. **Wat in paia blijft, roept een skill aan als het er is** (inventaris §7, laatste lijst; besluit 17): `test -e <pad>` of een beschikbaarheidscheck vooraf, en anders de regel `<stap>: skipped — <wat ontbreekt>`. Dat geldt voor `backlog-stand`, `board-sweep`, de controller, `controller_tick.sh`, de release-flow en de pre-push-keten. Of zo'n stap ook uit moet kunnen staan, valt onder *Optionaliteit* (Verbeterpunten).
17. **Twee punten uit de spec schuiven door.** Het meten van board-ID's in `init` (spec, *init* punt 2) heeft in (b) geen gebruiker: lighthouse heeft geen board, en paia heeft de ID's al in zijn `lighthouse.toml`. Hetzelfde geldt voor `[forge] default_branch` (inventaris §2.6): `main` staat niet op de deny-lijst. Beide komen onder *Verbeterpunten*. `host.toml` voorstellen doet (b) wel, want `handoff` gebruikt `host-check`.
18. **Namen** (besluit 6): `instrumenteren` → `instrument`, `check-instructies.sh` → `check-instructions.sh`. De rest houdt zijn paia-naam zonder `paia-`.
19. **Een skill noemt een kop als `heading:<key>`.** Het instructieblok (taak 28) legt die vorm één keer uit: de waarde onder `<key>` in `.lighthouse/headings.toml`, in de kolom van `[project] language`, op te vragen met `python3 .lighthouse/lib/lighthouse_lib/headings.py <key>`. Bij het lezen geldt elke variant. Zo staat geen Nederlandse of Engelse kop letterlijk in een skill, en kan de contracttest elke verwijzing controleren.

## Global Constraints

- Paden in een projectrepo, letterlijk uit de spec: skills `.claude/skills/lighthouse-<naam>/` en `.agents/skills/lighthouse-<naam>/`; agents `.claude/agents/lighthouse-<naam>.md`; hooks `.claude/hooks/lighthouse/<naam>.sh`; Python `.lighthouse/lib/lighthouse_lib/`; scripts `.lighthouse/scripts/`; workflows `.github/workflows/lighthouse-<naam>.yml`; PR-template `.github/pull_request_template.md`; koppentabel `.lighthouse/headings.toml`.
- Repo-root: `LIGHTHOUSE_REPO_ROOT`, anders `git rev-parse --show-toplevel`. Nooit afgeleid uit het eigen pad (spec, *Indeling*).
- Marker: frontmatter `metadata.lighthouse: "<pakket>@<versie>"`; shell, Python en YAML een regel `# lighthouse: <pakket>@<versie>` (`internal/packs/marker.go` uit (a), ongewijzigd). Geen marker in JSON, TOML of `pull_request_template.md`.
- Geen literal uit `ci/literal-denylist.txt` in `pack/core/`, dus ook geen `paia`, `PAIA_*` of `/paia-*`. Twee uitzonderingen, elk met een test: `pack/SOURCES.toml` (hij noemt bronpaden als `.claude/skills/paia-start-issue/SKILL.md`, en `go:embed` neemt alleen `core/` mee, dus hij wordt nooit geïnstalleerd), en de exacte regels in `ci/literal-allowlist.txt` (taak 8).
- `pack/core/lib/*.py` parseert met de syntaxis van Python 3.9 en heeft geen naam uit de standaardbibliotheek (de bestaande tests uit (a)).
- Hooks draaien onder bash 3.2 (macOS): geen `exec {var}>>`, geen `declare -A`, geen `${x,,}`.
- Geen guard doet stil niets: elke uitgang zonder controle is een blokkade met COULD NOT CHECK (guards) of een zichtbare melding (gates die niet blokkeren).
- Env-namen: `PAIA_<X>` wordt `LIGHTHOUSE_<X>`. Paden onder `/tmp` of `$XDG_RUNTIME_DIR` die `paia-` heten, worden `lighthouse-`.
- Datamap van scripts: `LIGHTHOUSE_DATA`, anders `${XDG_DATA_HOME:-~/.local/share}/lighthouse/<forge.name>`, met de naam van de repo-map als `[forge]` ontbreekt.
- Taal: dit plan, commits en PR's in het Nederlands; alles in `pack/` in het Engels (zie ontwerpkeuze 3 voor de uitzonderingen).
- Elk overgenomen bestand staat in `pack/SOURCES.toml` met `source_path` en `source_sha = "33833601349a8b79a1d1b2ac9077cc495a0d2b37"`. Een bestand dat uit meer bronnen komt, krijgt een lijst in `source_path`. Een nieuw bestand krijgt `new = true`.

## Review Focus

1. **Een repo met alleen `[project]` en `[forge]` in `lighthouse.toml`.** Verwacht: elke hook werkt met de defaults, of meldt zichtbaar dat hij overslaat. Geen enkele hook blokkeert een gewoon commando en geen enkele crasht met een traceback. Test: `test_hooks_with_minimal_config` in taak 14, geparametriseerd over alle hooks.
2. **Oude comments op lopende issues.** Een handoff met `<!-- paia: … -->`, `volgende:` en `wanneer: nacht`, een `🤖 Gestart` met `stapel:` en een artefact met `soort: plan`. Verwacht: ze parsen naar dezelfde waarden als hun nieuwe vorm. Test: `test_reads_legacy_*` in taak 8.
3. **Een Nederlands issue in een Engelse repo.** `### Acceptatiecriteria` in een issue, terwijl `[project] language = "en"`. Verwacht: `pr_body_gate` doet zijn AC-check zoals bij `### Acceptance criteria`. Test: `test_ac_heading_any_language` in taak 10.
4. **Een `.py`- of `.sh`-write zonder taalpakket.** Verwacht: een melding "not checked — … is not installed", geen exit 2. Test: `test_missing_language_pack_is_reported_not_blocked` in taak 11 en `test_post_without_language_packs` in taak 13.
5. **Een skill die naar iets wijst dat niet bestaat.** Een vertaalfout laat een skill verwijzen naar een script, lib-module, configsleutel, kop of `/lighthouse-*`-skill die niet bestaat. Verwacht: een rode test. Test: `tests/pack/test_skill_contracts.py` in taak 22, met nieuwe gevallen in elke skilltaak. Daarnaast drift tussen `pack/` en de geïnstalleerde kopie in lighthouse zelf: de CI-stap `lighthouse init --upgrade` moet "No changes." geven (taak 29).

---

## Het verhuisrecept

Elke taak die een paia-bestand overneemt, volgt deze stappen. Een taak noemt alleen wat er bijzonder aan is.

- **V1. Kopie-commit.** Kopieer elk bronbestand en zijn paia-test letterlijk naar het doelpad (`git -C <paia> show SRC:<bron> > <doel>`). Commit: `chore(core): <namen> letterlijk uit paia`.
- **V2. Tests eerst.** Pas de gekopieerde test aan: import via `lighthouse_lib.<x>` (de `IMPORT_ROOT` uit `tests/pack/_testkit.py`), hooks en scripts uit de geïnstalleerde indeling (`install_core`), en Engelse meldingen en tokens. Voeg de nieuwe tests van de taak toe. Gevallen die alleen paia-gedrag testen, gaan eruit; de commit noemt ze. Draai de tests: ze falen, en wel om de goede reden.
- **V3. Losmaken.** Imports: `src.controller.identity` en `host` worden `lighthouse_lib.config`, en de sibling-imports via `sys.path.insert(dirname(__file__))` worden `from lighthouse_lib import <x>` (inventaris §3). Daarnaast de repo-root via `config.repo_root()` (Python) of `LIGHTHOUSE_REPO_ROOT`/`git rev-parse` (shell), en literals naar de sleutels uit tabel *Configsleutels*.
- **V4. Vertalen**, volgens ontwerpkeuze 3 en 4, met de tabel *Protocoltokens* en `headings.toml`.
- **V5. Fail-open dichten**, waar inventaris §6 een gat noemt in dit bestand.
- **V6. Herkomst.** Een entry in `pack/SOURCES.toml`, een regel in `requires.toml` voor elke tool die het bestand nodig heeft, en `ci/literal-grep.sh` groen.
- **V7. Draai** `uv run pytest -q && go test ./... && ci/literal-grep.sh` — verwacht: groen. Commit met het bericht van de taak.

## Configsleutels

Nieuw in `lighthouse.toml`, met de default in `config.py`. Een sleutel zonder default is optioneel: rc 1 betekent "stap overgeslagen, gemeld".

| Sleutel | Type | Default | Bron (inventaris) |
|---|---|---|---|
| `paths.specs`, `paths.plans` | str | `docs/developer/planning/specs`, `…/plans` | §4 `[paths]` |
| `paths.tenets`, `paths.areas`, `paths.history`, `paths.label_taxonomy` | str | geen | §4, ontwerpkeuze 9 |
| `paths.search_dirs` | list[str] | `["."]` | start-issue 78 |
| `paths.mirror_exceptions` | str | geen | check 11 |
| `labels.blocked_issue`, `blocked_external`, `needs_shaping`, `needs_decision`, `needs_hands`, `needs_testing` | str | `blocked-issue`, `blocked-external`, `needs-shaping`, `needs-decision`, `needs-hands`, `needs-testing` | §4 `[labels]` |
| `labels.location_prefix`, `labels.area_prefix` | str | geen | start-issue 55-56 |
| `test_gate.command` | str (via `bash -c`) | geen | §2.3 |
| `test_gate.deadline_seconds`, `slots`, `slot_wait_seconds` | int | `270`, `2`, `180` | §4 `[test_gate]` |
| `test_gate.slice` | str (systemd-slice) | geen: geen `systemd-run`-rem | `pre-commit-tests.sh:342-374` |
| `test_gate.unchecked_exit_codes` | list[int] | `[]` | ontwerpkeuze 10 |
| `test_gate.evidence_test_dir` | str | `tests/` | `evidence.py:72` |
| `ci.runs_on` | str | `ubuntu-latest` | §4 `[ci]` |
| `ci.bot_client_id_var`, `ci.bot_private_key_secret`, `ci.bot_login` | str | geen | §4 `[ci]` |
| `codex_usage.halt_pct_session`, `vraag_pct_week`, `app_server_timeout_seconds`, `review_pct_session`, `review_pct_week` | int | `85`, `80`, `20`, `90`, `95` | §1 |
| `worktree_audit.protected_refs`, `worktree_audit.protected_paths` | list[str] | `[]` | §2.6 |
| `project.instructions_word_ceiling` | int | `3000` | §2.6 |
| `project.skill_prefix` | str | geen | §2.6 |

## Protocoltokens

Schrijvers gebruiken de rechterkolom; lezers accepteren beide (ontwerpkeuze 4). De koppen (`🤖 <soort>`) en de vraagvelden staan in `headings.toml`, niet hier.

| Oud | Nieuw |
|---|---|
| marker `<!-- paia: {json} -->` | `<!-- lighthouse: {json} -->` |
| artefact: `soort` (`spec`/`plan`), `versie`, `deel`, `delen`, `bestand`, `commit`, `hash`, `vervangt` | `kind`, `version`, `part`, `parts`, `file`, `commit`, `hash`, `replaces` |
| handoff: `volgende`, `wanneer` (`nu`/`nacht`), `anker`, `basis` | `next`, `when` (`now`/`night`), `anchor`, `base` |
| gestart: `sessie`, `commando`, `stapel`, `blokkade` (`verrekend`), `sprong`, `terugval` | `session`, `command`, `stack`, `blocker` (`accounted`), `skip`, `fallback` |
| herbaseerd: `voorganger`, `op`; hervat: `antwoord`; teruggegeven: `reden` | `predecessor`, `onto`, `answer`, `reason` |
| voet `sessie <id>` | `session <id>` |

Ongewijzigd: `issue`, `worktree`, `branch`, `lane`, `start`, `head`, `runner`, `workspace`, `pr`.

---

## Bestandsstructuur na (b)

```
pack/core/
  headings.toml                    koppentabel (nl, en)
  permissions.json                 permissions.allow-regels
  requires.toml                    tool → eenheden
  instructions/AGENTS.md           het beheerde blok
  hooks/      hooks.json, main-commit-guard.sh, worktree-guard.sh, worktree-linkcheck.sh,
              bash-write-gates.sh, pr-create-gate.sh, pre-commit-tests.sh
  lib/        __init__.py, config.py, headings.py, protocol.py, artefacts.py, datadir.py,
              git_invocations.py, bash_writes.py, deadline.py, evidence.py, gate_slot.py,
              md_links.py, no_verify.py, pr_body_gate.py
  scripts/    post_artefact.py, board_start.py, plan_review_record.py, review_pair.py,
              codex_usage.py, measure_start_issue_kpi.py, measure_fix_pr_kpi.py,
              worktree_audit.py, unblock_dependents.py, check-instructions.sh
  workflows/  unblock-dependents.yml
  templates/  pull_request_template.md
  agents/     issue-impact-scout.md, spec-adversary.md
  skills/     start-issue, route-issue, handoff, verify-pr, fix-pr, close-issue,
              spec-adversarial-review, plan-adversarial-review, write-issue, worktree-audit,
              instrument
tests/pack/   per bestand een test; test_skill_contracts.py, test_sources.py
```

---

## PR 1 — mechanisme (`issue-7-b1-mechanism`)

### Taak 1: Skills in een pakket

**Files:**
- Modify: `internal/packs/packs.go`, `internal/packs/packs_test.go`, `internal/initcmd/initcmd_test.go`

**Interfaces:**
- Produces: `packs.Load` mapt `skills/<n>/<rel>` (behalve `claude/` en `agents/`) naar `.claude/skills/lighthouse-<n>/<rel>` én `.agents/skills/lighthouse-<n>/<rel>`; `skills/<n>/claude/SKILL.md` alleen naar `.claude/…/SKILL.md`; `skills/<n>/agents/SKILL.md` alleen naar `.agents/…/SKILL.md`. `IsInstallPath(KindFile, p)` accepteert beide skillmappen.

- [ ] **Stap 1: Schrijf de falende tests** in `packs_test.go`:
  - `TestLoadSkillShared`: `skills/x/SKILL.md` (frontmatter `name: lighthouse-x`) en `skills/x/review.sh` → vier eenheden, in beide skillmappen; `review.sh` krijgt `0o755` en een marker.
  - `TestLoadSkillVariants`: `skills/x/claude/SKILL.md` en `skills/x/agents/SKILL.md` → elk alleen in zijn eigen map, als `SKILL.md`.
  - `TestLoadSkillRejects`: `name` ≠ `lighthouse-x`; `SKILL.md` naast `claude/SKILL.md`; alleen `agents/SKILL.md` zonder `claude/SKILL.md` en zonder gedeelde `SKILL.md`; een ander bestand in `claude/` of `agents/`. Elke fout noemt het pad.
  - Pas `TestLoadRejects` aan: de fout `skills/ is not supported yet` bestaat niet meer.
  - `initcmd_test.go`: `TestInitInstallsSkill`, een `MapFS` met een skill → beide mappen geschreven, met `metadata.lighthouse` in de frontmatter; `--upgrade` direct daarna geeft `No changes.`
- [ ] **Stap 2: Draai** `go test ./internal/...` — verwacht: FAIL.
- [ ] **Stap 3: Implementeer** in `Load`; de `name` lees je met een regel `name: ` binnen de frontmatter, zonder YAML-library.
- [ ] **Stap 4: Draai** `go test ./... && golangci-lint run` — verwacht: PASS.
- [ ] **Stap 5: Commit** `feat(init): skills met harnasvarianten installeren`

### Taak 2: Koppentabel, templates en workflows

**Files:**
- Modify: `internal/packs/packs.go`, `internal/packs/packs_test.go`, `internal/initcmd/initcmd.go`, `internal/initcmd/initcmd_test.go`
- Create: `internal/packs/fill.go`, `internal/packs/fill_test.go`

**Interfaces:**
- Produces: `headings.toml` bovenin het pakket → `.lighthouse/headings.toml`; `templates/pull_request_template.md` → `.github/pull_request_template.md`; `workflows/<n>.yml` → `.github/workflows/lighthouse-<n>.yml`. `type Lookup func(key string) (value string, ok bool, err error)`; `Fill(content []byte, lookup Lookup) (out []byte, missing []string, err error)` vervangt elke `@@<sleutel>@@` (sleutel: `[a-z_.]+`). `Load` krijgt geen lookup: `initcmd` vult de workflows na `Load`.

- [ ] **Stap 1: Schrijf de falende tests**:
  - `TestFill`: `runs-on: @@ci.runs_on@@` met lookup `ubuntu-latest` → ingevuld; een onbekende sleutel → in `missing`, uitvoer ongewijzigd; `${{ secrets.X }}` blijft staan; een lookup-fout → `err`.
  - `TestLoadMapsTemplatesAndWorkflows`: de drie paden hierboven; de PR-template krijgt geen marker, de workflow wel.
  - `TestInitFillsWorkflow`: `lighthouse.toml` zonder `[ci]` → de workflow heeft `runs-on: ubuntu-latest`, en het manifest draagt de hash van het ingevulde bestand. Ontbreekt een sleutel zonder default (`@@ci.bot_login@@`), dan staat de workflow er niet, en noemt `Err` `workflow lighthouse-x.yml not installed: ci.bot_login is not set`. rc 0.
- [ ] **Stap 2: Draai** — verwacht: FAIL.
- [ ] **Stap 3: Implementeer.** `initcmd` krijgt de lookup door `config.py` uit het pakket te draaien, uitgepakt in een tijdelijke map, met `LIGHTHOUSE_REPO_ROOT=<root>`. Zo is er één bron voor de defaults (taak 6). rc 0 → waarde; rc 1 → `ok=false`; rc 2 → `err`, en `init` stopt zonder te schrijven. Ontbreekt `python3`, dan installeert `init` geen workflows en meldt hij dat. Een test-hook `Options.Lookup` vervangt de subprocess in de unit-tests.
- [ ] **Stap 4: Draai** `go test ./...` — verwacht: PASS.
- [ ] **Stap 5: Commit** `feat(init): koppentabel, PR-template en workflows met waarden uit de config`

### Taak 3: Permissies als eenheid

**Files:**
- Modify: `internal/packs/packs.go`, `internal/settings/settings.go`, `internal/settings/settings_test.go`, `internal/initcmd/workspace.go`, `internal/initcmd/initcmd.go`, `internal/manifest/manifest.go`, plus hun tests

**Interfaces:**
- Produces: `packs.KindPermission = "permission"`; `permissions.json` → per regel een `Unit{Kind: KindPermission, Path: ".claude/settings.json", ID: <regel>, Content: []byte(<regel>)}`. `settings.Allow(doc) ([]string, error)`; `settings.ApplyAllow(doc *orderedjson.Object, want []string, drop []string) error`. `manifest.Read` accepteert de soort `permission`.

- [ ] **Stap 1: Schrijf de falende tests**: `TestApplyAllowAppends` (bestaande regels houden hun volgorde, een nieuwe komt achteraan; ontbreekt `permissions` of `allow`, dan komt die achteraan in het object); `TestApplyAllowDrops` (alleen regels uit `drop`); `TestApplyAllowIdempotent`; `TestAllowRejectsShape` (`allow` geen array of een niet-string erin → fout met JSON-pad). In `initcmd_test.go`: `TestInitMergesPermissions` (een projectregel blijft staan, de pakketregel komt erbij, `--upgrade` geeft `No changes.`) en `TestUpgradeDropsPermission` (een pakket zonder de regel → de regel weg, de projectregel blijft).
- [ ] **Stap 2: Draai** — verwacht: FAIL.
- [ ] **Stap 3: Implementeer.** In `current()` is een permissie aanwezig als de regel in `allow` staat. `drop` = de permissies die `Remove` krijgen uit `reconcile.Plan`.
- [ ] **Stap 4: Draai** `go test ./...` — verwacht: PASS.
- [ ] **Stap 5: Commit** `feat(init): permissions.allow-regels uit het pakket mergen`

### Taak 4: `host.toml` voorstellen en de Superpowers-check

**Files:**
- Modify: `internal/initcmd/initcmd.go`, `internal/initcmd/initcmd_test.go`, `internal/deps/deps.go`, `internal/deps/deps_test.go`

**Interfaces:**
- Produces: `Options.HostTOML string` (default `$LIGHTHOUSE_HOST_TOML`, anders `~/.config/lighthouse/host.toml`); `Options.Hostname func() (string, error)`; `Options.TailscaleIP func() (string, error)`. `deps`: de bijzondere naam `superpowers` is aanwezig als `~/.claude/plugins/installed_plugins.json` een sleutel heeft die met `superpowers` begint. `Checker.Home string` maakt dat testbaar.

- [ ] **Stap 1: Schrijf de falende tests**:
  - `TestInitProposesHostTOML`: geen `host.toml`, `Hostname` = `box`, `TailscaleIP` = `100.64.0.1` → de diff toont `+++ b/~/.config/lighthouse/host.toml` met `name = "box"` en `tailscale_ip = "100.64.0.1"`; na akkoord staat hij er. Hij komt niet in het manifest.
  - `TestInitSkipsHostTOMLWithoutTailscale`: `TailscaleIP` faalt → geen voorstel, `Err` meldt `host.toml not proposed: tailscale ip unavailable; skills that check the host will report it`.
  - `TestInitLeavesExistingHostTOML`: een bestaand bestand blijft byte voor byte gelijk.
  - `TestSuperpowers` in `deps_test.go`: `installed_plugins.json` met `"plugins": {"superpowers@x": …}` → aanwezig; zonder die sleutel, of zonder bestand → `Missing{Tool: "superpowers"}`.
- [ ] **Stap 2: Draai** — verwacht: FAIL.
- [ ] **Stap 3: Implementeer.** `TailscaleIP` draait `tailscale ip -4` en neemt de eerste regel; ongeldig IP telt als fout.
- [ ] **Stap 4: Draai** `go test ./...` — verwacht: PASS.
- [ ] **Stap 5: Commit** `feat(init): host.toml voorstellen en Superpowers als afhankelijkheid`

### Taak 5: Herkomst controleren

**Files:**
- Create: `tests/test_sources.py`
- Modify: `pack/SOURCES.toml` (entry `new = true` voor `pack/core/lib/__init__.py`, `config.py`, `hooks/hooks.json`, `instructions/AGENTS.md`, `requires.toml`)

- [ ] **Stap 1: Schrijf de test** `test_every_pack_file_has_a_source`: elk bestand onder `pack/core/` (behalve caches) heeft precies één entry. Elke entry wijst naar een bestaand bestand en heeft óf `new = true`, óf `source_sha` (40 hex-tekens) met `source_path` (een string of een niet-lege lijst). Plus `test_no_entry_without_file`.
- [ ] **Stap 2: Draai** `uv run pytest tests/test_sources.py -q` — verwacht: FAIL op de vijf bestanden zonder entry.
- [ ] **Stap 3: Vul `SOURCES.toml` aan.** Draai — verwacht: PASS.
- [ ] **Stap 4: Commit** `test(core): elk pakketbestand heeft een herkomst in SOURCES.toml`. Open PR 1; dit plan staat al als eerste commit op de branch.

---

## PR 2 — lib (`issue-7-b2-lib`)

### Taak 6: Configlezer uitbreiden

**Files:**
- Modify: `pack/core/lib/config.py`, `tests/pack/test_config.py`
- Create: `pack/core/lib/datadir.py`, `tests/pack/test_datadir.py`

**Interfaces:**
- Produces: `config.DEFAULTS: Mapping[str, object]` (tabel *Configsleutels*); `config.is_known_key(key: str) -> bool` (de sleutels van (a) onder `forge`, `forge.board` en `project`, de afgeleide `forge.owner`/`forge.name`, `host.<x>`, alles uit de tabel *Configsleutels*, en `forge.board.status_options.<optie>`/`priority_options.<optie>` met elke optienaam); `lookup(cfg, key)` geeft de default als de sleutel ontbreekt en er een default is; `load` valideert het type van elke bekende sleutel uit de tabel als hij er staat (`ConfigError` noemt pad en sleutel). Het bestand werkt als script op zijn pad (ontwerpkeuze 6). `datadir.data_dir() -> Path` (Global Constraints; maakt de map niet aan).

- [ ] **Stap 1: Schrijf de falende tests**: `test_defaults_for_absent_keys` (`paths.specs`, `test_gate.slots`, `ci.runs_on`, `codex_usage.review_pct_week` → defaults); `test_bot_keys_have_no_default` (`main(["ci.bot_login"]) == 1`); `test_wrong_type_is_rc_2` (geparametriseerd: `test_gate.slots = "2"`, `paths.search_dirs = "src"`, `test_gate.unchecked_exit_codes = ["3"]`); `test_list_value_prints_one_per_line`; `test_runs_as_file` (subprocess `python3 <install>/.lighthouse/lib/lighthouse_lib/config.py forge.repo` zonder `PYTHONPATH`, cwd een andere repo → rc 0); `test_data_dir_env_wins`, `test_data_dir_uses_forge_name`, `test_data_dir_without_forge_uses_dir_name`.
- [ ] **Stap 2: Draai** `uv run pytest tests/pack/test_config.py tests/pack/test_datadir.py -q` — verwacht: FAIL.
- [ ] **Stap 3: Implementeer.** Bovenaan elk lib-bestand met een CLI, vóór de eigen imports: als `__package__` leeg is, zet je `Path(__file__).resolve().parents[1]` vooraan in `sys.path`. `install_core` in `_testkit.py` kopieert voortaan alles uit `pack/core` behalve `skills/`, `agents/`, `templates/`, `workflows/` en de JSON/TOML bovenin. `headings.toml` gaat naar `.lighthouse/headings.toml`.
- [ ] **Stap 4: Draai** `uv run pytest -q` — verwacht: PASS.
- [ ] **Stap 5: Commit** `feat(core): configlezer met defaults, typecheck en datamap`

### Taak 7: Koppentabel

**Files:**
- Create: `pack/core/headings.toml`, `pack/core/lib/headings.py`, `tests/pack/test_headings.py`

**Interfaces:**
- Produces: `headings.toml` met per sleutel een tabel `{nl = "…", en = "…"}`. `headings.path() -> Path` (`LIGHTHOUSE_HEADINGS`, anders `config.repo_root() / ".lighthouse/headings.toml"`); `variants(key: str) -> tuple[str, ...]` (alle talen, `nl` eerst); `heading(key: str, language: str | None = None) -> str` (`None` = `[project] language`); `any_of(key: str) -> str` (regex-alternatie van `variants`, met `re.escape`). CLI: `headings.py <key>` geeft `heading(key)`. Een onbekende sleutel geeft `KeyError`; via de CLI is dat rc 2.
- Sleutels in (b), met deze `nl`-waarden letterlijk uit paia: `acceptance_criteria` (`Acceptatiecriteria`); de soorten `kind_handoff` (`Handoff`), `kind_started` (`Gestart`), `kind_question` (`Vraag`), `kind_resumed` (`Hervat`), `kind_done` (`Klaar`), `kind_returned` (`Teruggegeven`), `kind_rebased` (`Herbaseerd`); de vraag `question_where` (`Waar het staat`), `question_what` (`Wat er speelt`), `question_label` (`Vraag`), `question_close` (`**Max 1 comment als reactie.**`), `question_bundle_hint` (``Antwoord per vraag, bijvoorbeeld `1a, 2c`.``), `question_run_yourself` (`**Zelf draaien:**`), `question_recommended` (`*(aanbevolen)*`), `question_phase` (`fase`), `question_task` (`taak`); `gate_line` (`Poort`); `scout_skipped` (`Scout: overgeslagen`); `impact_pack` (`IMPACT PACK`); de evidence-uitkomsten `evidence_holds` (`klopt`), `evidence_fails` (`klopt niet`), `evidence_invalid` (`ongeldig`), `evidence_unmeasurable` (`niet meetbaar`), `evidence_after_pr` (`na PR: `), `evidence_after_pr_unmeasured` (`na PR, niet gemeten`), `evidence_table_head` (`| AC | Uitkomst | Detail |`), `evidence_closes_without_block` (`Closes zonder bewijsblok:`); `open_after_review` (`## Open na review`); `milestone_dod` (`Definition of Done`), `milestone_no_gos` (`No-Gos`), `milestone_not_yet_decided` (`Nog niet vastgelegd`). De taken 10, 15, 16 en 24 voegen de koppen toe die ze bij V4 tegenkomen. De `en`-waarden kiest de vertaler; een `en`-waarde mag niet gelijk zijn aan een `nl`-waarde van een andere sleutel.

- [ ] **Stap 1: Schrijf de falende tests**: `test_every_key_has_nl_and_en`; `test_variants_nl_first`; `test_heading_follows_project_language` (met `LIGHTHOUSE_TOML` op `language = "nl"` en op `"en"`); `test_any_of_escapes` (`**Max 1 comment als reactie.**` matcht letterlijk); `test_unknown_key_cli_rc_2`; `test_no_cross_key_collision` (geen waarde komt onder twee sleutels voor, binnen of tussen talen; `question_label`/`kind_question` zijn de enige toegestane uitzondering, want het is hetzelfde woord in een andere rol).
- [ ] **Stap 2–4:** Draai (FAIL), implementeer, draai `uv run pytest -q` (PASS).
- [ ] **Stap 5: Commit** `feat(core): koppentabel headings.toml met lezer`

### Taak 8: Comment-protocol

**Files:**
- Create (V1 uit `src/controller/comments.py` en `src/controller/artefacten.py`): `pack/core/lib/protocol.py`, `pack/core/lib/artefacts.py`, `tests/pack/test_protocol.py` (uit `test_controller_comments.py`), `tests/pack/test_artefacts.py` (uit `test_controller_artefacten.py`)

**Interfaces:**
- Consumes: `headings.variants`, `headings.heading`, `config.load`.
- Produces in `protocol.py` (publiek, inventaris §2.10): `MARKER = "<!-- lighthouse:"`, `LEGACY_MARKERS = ("<!-- paia:",)` (de enige plek in `pack/core` met die string; stap 3); `KINDS = ("handoff", "started", "question", "resumed", "done", "returned", "rebased")`; `ROBOT = "🤖"`; dataclasses `Comment`, `Header(kind: str, fields: dict[str, str], session: str | None)`, `Handoff(issue: int, worktree: str, branch: str, anchor: str, next: str, when: str, lane: str = "")`; `HandoffError(ValueError)`; `LETTERS = "abcdef"`; `MAX_QUESTIONS = 4`; `parse_header(body) -> Header | None` (elke kopvariant → canonieke `kind`, elke oude veldnaam → nieuwe); `parse_handoff(header) -> Handoff` (`when` ∈ {`now`, `night`}, oud `nu`/`nacht` → nieuw; `lane` wordt niet gevalideerd: dat is controllerbeleid); `stack_base(bodies) -> str | None`; `active_stack(bodies) -> Header | None`; `footer(session, *extra) -> str`; `render_started(**…)`, `fill_started(message, **fields)`, `render_rebased(…)`, `render_resumed(…)`, `render_done(*, session, pr_url) -> str` (alleen kop, velden en voet, zonder de controllerproza), `render_returned(…)`, `render_question(…)`, `parse_question(body) -> dict | None`, `questions_of(fields) -> list[dict]`. Elke `render_*` neemt `language: str | None = None`.
- Produces in `artefacts.py`: `KINDS = ("spec", "plan")`, `GITHUB_LIMIT = 65_536`; `Meta(kind, version, part, parts, file, commit, hash, replaces)`; `parse_meta(body) -> Meta | None` (beide markers, oude en nieuwe sleutels); `content(body) -> str`; `render(meta, content) -> str` (nieuwe marker en sleutels); `hash_of(data: bytes) -> str`; `split(text, limit=60_000) -> list[str]`; `current(comments, kind, file) -> list[Comment]`; `next_version(comments, kind, file) -> int`.
- **Blijft in paia** (controllerbeleid): `ALLOWLIST`, `EIND`, `BRAINSTORM`, `INTERACTIEF`, `NATIVE`, `NATIVE_MODEL`, `beoordeel_commando`, `is_antwoord`, `antwoord_prompt`, de proza van `render_klaar` en de lane-validatie uit `parse_handoff`. Noteer ze in de PR-beschrijving voor (d).

- [ ] **Stap 1: V1, dan V2.** Neem de tests over voor alles wat meeverhuist; tests van het beleid hierboven vallen af. Nieuw:

```python
def test_reads_legacy_handoff():
    body = "🤖 Handoff\nissue: #12\nworktree: /w/x/\nbranch: b\nvolgende: /x\nwanneer: nacht\nanker: a\n"
    assert parse_handoff(parse_header(body)) == Handoff(12, "/w/x", "b", "a", "/x", "night")

def test_reads_legacy_started_stack():
    assert stack_base(["🤖 Gestart\nsessie: s\nstapel: #3 b3\nbasis: abc\n"]) == "abc"

def test_reads_legacy_artefact_marker():
    meta = parse_meta('<!-- paia: {"soort":"plan","versie":2,"deel":1,"delen":1,"bestand":"p.md","commit":"' + "a"*40 + '","hash":"' + "b"*12 + '"} -->\nx')
    assert meta.kind == "plan" and meta.version == 2 and meta.file == "p.md"

def test_writes_new_marker_and_keys():
    out = render(Meta("plan", 1, 1, 1, "p.md", "a"*40, "b"*12, None), "x")
    assert out.startswith('<!-- lighthouse: {"kind": "plan"') and "soort" not in out

def test_render_follows_language(nl_env):   # LIGHTHOUSE_TOML met language = "nl"
    assert render_resumed(session="s", answer_id=1).startswith("🤖 Hervat\nsession: s")

def test_english_header_parses(): assert parse_header("🤖 Started\nsession: s\n").kind == "started"
def test_lane_not_validated(): assert parse_handoff(parse_header("🤖 Handoff\nissue: 1\nworktree: w\nbranch: b\nnext: /x\nwhen: now\nlane: anything\n")).lane == "anything"
```

- [ ] **Stap 2: Draai** `uv run pytest tests/pack/test_protocol.py tests/pack/test_artefacts.py -q` — verwacht: FAIL.
- [ ] **Stap 3: V3–V6.** `artefacts.py` importeert `Comment` en `LEGACY_MARKERS` uit `protocol` en niets uit `identity`. Literal-grep:
  - nieuw bestand `ci/literal-allowlist.txt`, één uitzondering per regel in de vorm `<pad>:<exacte regeltekst>`, met als enige regel `pack/core/lib/protocol.py:LEGACY_MARKERS = ("<!-- paia:",)`;
  - `ci/literal-grep.sh` laat een treffer vallen als `<pad>:<tekst>` (zonder regelnummer) exact in die lijst staat, en slaat `pack/SOURCES.toml` over;
  - tests in `tests/test_literal_grep.py`: `test_allowlisted_line_passes`, `test_same_text_in_other_file_fails`, `test_other_line_in_allowlisted_file_fails`, `test_sources_toml_is_skipped` (een `paia`-pad in `pack/SOURCES.toml` geeft rc 0, dezelfde tekst in `pack/core/x.md` rc 1).
- [ ] **Stap 4: V7.** Commit `feat(core): comment-protocol en artefacten naar pack/core/lib, met lighthouse-marker`

### Taak 9: Kleine hookhelpers

**Files:**
- Create (V1): `pack/core/lib/deadline.py`, `gate_slot.py`, `md_links.py`, `no_verify.py`; tests uit `test_hook_deadline.py`, `test_gate_slot.py`, `test_md_links.py` (en de `no_verify`-gevallen uit `test_instructie_hooks.py`)
- Modify: `pack/core/lib/git_invocations.py`, `tests/pack/test_git_invocations.py` (ontwerpkeuze 5)

- [ ] **Stap 1–2: V1, V2.** Specifiek: `md_links` geeft `dead`/`absolute` in plaats van `dood`/`absoluut`; `gate_slot` en `deadline` gebruiken `lighthouse-gate-slots` en `LIGHTHOUSE_*` (Global Constraints); de test met `paia-bestaat-niet-xyz` wordt `lighthouse-does-not-exist-xyz`. `git_invocations`: `<unknown>`, `<new>`, en de guard en zijn test lezen die tokens.
- [ ] **Stap 3: V3–V7.** Commit `feat(core): deadline, gate_slot, md_links en no_verify naar pack/core/lib`

### Taak 10: `evidence` en `pr_body_gate`

**Files:**
- Create (V1): `pack/core/lib/evidence.py`, `pack/core/lib/pr_body_gate.py`, `tests/pack/test_evidence.py` (uit `test_evidence_allowlist.py`), `tests/pack/test_pr_body_gate.py`

**Interfaces:**
- Consumes: `config.load`/`lookup` (`forge.repo`, `test_gate.evidence_test_dir`), `headings.any_of("acceptance_criteria")`, `headings.heading` voor de evidence-uitkomsten.
- Produces: CLI `evidence.py check '<cel>'`, `evidence.py run --issue N --body-file P --cwd D [--before-pr]`; `pr_body_gate.py <cwd>`. De blokkadetekst noemt `/lighthouse-verify-pr`.

- [ ] **Stap 1–2: V1, V2.** De vaste repo-naam in `test_evidence_allowlist.py:34,37` wordt `Acme/widgets` uit `LIGHTHOUSE_TOML`. `test_beelink_hardening` gaat over op de hostloze hardening, onder een nieuwe naam. Nieuw:
  - `test_ac_heading_any_language`: een issue met `### Acceptatiecriteria` en een met `### Acceptance criteria`, beide onder `language = "en"` → dezelfde AC-check.
  - `test_pytest_path_from_config`: `evidence_test_dir = "t/"` → `uv run pytest t/x.py` toegestaan, `tests/x.py` geweigerd.
  - `test_outcomes_follow_language`: een `run` onder `nl` schrijft `klopt`, onder `en` de `en`-waarde.
  - `test_not_a_git_repository_under_other_locale`: met `LANG=nl_NL.UTF-8` en `LC_ALL` leeg geeft een map buiten git dezelfde uitkomst als onder `C` (inventaris §2.12).
- [ ] **Stap 3: V3–V6.** Zet `LC_ALL=C` in de env van elke git-aanroep waarvan de tekst gematcht wordt; dat geldt ook voor de `not a git repository`-match in `pack/core/hooks/main-commit-guard.sh` (met een test in `test_main_commit_guard.py`) en straks `pre-commit-tests.sh:197`. `parents[3]` wordt `config.repo_root()`.
- [ ] **Stap 4: V7.** Commit `feat(core): evidence en pr_body_gate naar pack/core/lib, koppen uit headings.toml`

### Taak 11: `bash_writes`

**Files:**
- Create (V1): `pack/core/lib/bash_writes.py`, `tests/pack/test_bash_writes.py`

**Interfaces:**
- Produces: `classify(rc: int, output: bytes) -> str` (`"ok"`, `"finding"`, `"unchecked"`; rc 2 is `unchecked` als de eerste regel `COULD NOT CHECK` bevat); `GATES` ongewijzigd; `gate_path(root: str, ext: str) -> Path | None` (`None` als de extensie geen gate heeft); `GATE_PACKS = {"shellcheck.sh": "shell", "ruff-format.sh": "python", "worktree-linkcheck.sh": "core"}`; `run_gate` geeft `("missing", b"<gate> is not installed (pack/<p>)")` als het script niet bestaat.

- [ ] **Stap 1–2: V1, V2.** Pin de nieuwe tokens in `test_bash_writes.py:406,1010`. Nieuw: `test_missing_language_pack_is_reported_not_blocked` (geen `ruff-format.sh` geïnstalleerd → oordeel `missing`, en de samenvatting bevat `not checked — ruff-format.sh is not installed (pack/python)`); `test_classify_first_line_only` (een bevinding waarvan de tweede regel `COULD NOT CHECK` citeert, blijft `finding`); `test_hook_dir_from_repo_root` (de gates worden gezocht in `<LIGHTHOUSE_REPO_ROOT>/.claude/hooks/lighthouse/`).
- [ ] **Stap 3: V3–V6.** `HOOK_DIR` (42) wordt `config.repo_root() / ".claude/hooks/lighthouse"`; `PAIA_BASH_GATES_*` worden `LIGHTHOUSE_BASH_GATES_*`, de map `lighthouse-bash-gates`.
- [ ] **Stap 4: V7.** Commit `feat(core): bash_writes naar pack/core/lib, één COULD NOT CHECK-token`. Open PR 2.

---

## PR 3 — hooks (`issue-7-b3-hooks`)

Elke hook komt in `pack/core/hooks/` en in `hooks.json`, met event, matcher en timeout zoals in paia's `.claude/settings.json` op `SRC`:

| Event | Matcher | Hook (volgorde binnen de groep) | Timeout |
|---|---|---|---|
| `PreToolUse` | `Bash` | `main-commit-guard.sh`, `pre-commit-tests.sh`, `bash-write-gates.sh pre`, `pr-create-gate.sh` | 15, 300, 5, 15 |
| `PreToolUse` | `Edit\|Write` | `worktree-guard.sh` | 15 |
| `PostToolUse` | `Edit\|Write` | `worktree-linkcheck.sh` | 15 |
| `PostToolUse` | `Bash` | `bash-write-gates.sh post` | 120 |
| `PostToolUseFailure` | `Bash` | `bash-write-gates.sh post` | 120 |

Elke hook vindt zijn lib als de guard uit (a): `LIB_ROOT="${LIGHTHOUSE_REPO_ROOT:-$(git -C "$(dirname "$0")" rev-parse --show-toplevel 2>/dev/null)}"`; geen lib → COULD NOT CHECK. `internal/packs/core_test.go` (`TestCorePack`) groeit mee met elke hook.

### Taak 12: `worktree-guard` en `worktree-linkcheck`

**Files:**
- Create: `pack/core/hooks/worktree-guard.sh` (uit de bootstrap `.claude/hooks/worktree-guard.sh`, die al Engels is), `tests/pack/test_worktree_guard.py` (uit `tests/hooks/test_worktree_guard.py`); V1 voor `worktree-linkcheck.sh` met zijn gevallen uit `test_instructie_hooks.py` → `tests/pack/test_worktree_linkcheck.py`

- [ ] **Stap 1: Schrijf de falende tests** (naast de overgenomen):
  - `test_guard_speaks_when_git_breaks`: een `git`-stub die bij `rev-parse` rc 128 geeft met een andere tekst dan "not a git repository" → rc 0, en de `systemMessage` op stdout bevat `COULD NOT CHECK`. De guard waarschuwt alleen en blokkeert nooit.
  - `test_guard_speaks_when_branch_fails`: een `git`-stub die bij `branch --show-current` faalt → idem.
  - `test_linkcheck_invalid_json_is_unchecked`: rc 2, eerste regel `Linkcheck: COULD NOT CHECK — …`.
  - `test_linkcheck_without_git`: `tool_bin(drop="git")` → idem.
  - `test_linkcheck_unresolvable_path`: idem.
- [ ] **Stap 2: Draai** — verwacht: FAIL.
- [ ] **Stap 3: V3–V6.** Dicht de gaten uit inventaris §6. Alleen een echte "not a git repository" (onder `LC_ALL=C`) blijft stil. `PAIA_LINKCHECK_TRACE` wordt `LIGHTHOUSE_LINKCHECK_TRACE`. De regex van check 8 komt in taak 20.
- [ ] **Stap 4: V7.** Commit `feat(core): worktree-guard en worktree-linkcheck, zonder stille uitgangen`

### Taak 13: `pr-create-gate` en `bash-write-gates`

**Files:**
- Create (V1): `pack/core/hooks/pr-create-gate.sh`, `pack/core/hooks/bash-write-gates.sh`, `tests/pack/test_pr_create_gate.py`, `tests/pack/test_bash_write_gates.py`

- [ ] **Stap 1–2: V1, V2.** Pin de nieuwe tokens in `test_bash_write_gates.py:157,516`. Nieuw: `test_post_without_language_packs` (een `.py`- en een `.sh`-write via Bash, alleen `pack/core` geïnstalleerd → rc 0, en de uitvoer noemt beide als "not checked"); `test_pre_without_python_is_silent_post_is_loud` (de bestaande ontwerpkeuze, nu getest).
- [ ] **Stap 3: V3–V6.** `HOOK_DIR` (`bash-write-gates.sh:26-27`, `pr-create-gate.sh:110-111`) via `LIB_ROOT`.
- [ ] **Stap 4: V7.** Commit `feat(core): pr-create-gate en bash-write-gates naar pack/core`

### Taak 14: `pre-commit-tests` als mechanisme

**Files:**
- Create (V1): `pack/core/hooks/pre-commit-tests.sh`, `tests/pack/test_pre_commit_tests.py` (uit `test_pre_commit_gate.py`), `tests/pack/test_hooks_minimal_config.py`

**Interfaces:**
- Consumes: `test_gate.command`, `deadline_seconds`, `slots`, `slot_wait_seconds`, `slice`, `unchecked_exit_codes`.
- Produces: de hook exporteert `LIGHTHOUSE_GATE_TMP` (een lege map per run) voor het commando, dat hem kan gebruiken als `--basetemp="$LIGHTHOUSE_GATE_TMP"`. Commando: `bash -c "$COMMAND"` in de wortel.

- [ ] **Stap 1–2: V1, V2.** De vlaggen- en `gates.slice`-tests worden tests tegen `[test_gate]`: `slice = "x.slice"` → de `systemd-run`-wrap met die slice en de memory-events onder `x.slice` (via `LIGHTHOUSE_GATE_CGROUP_ROOT`); zonder `slice` → geen wrap en geen melding "brake unavailable". Nieuw:
  - `test_no_command_is_loud_and_allows`: rc 0, en de JSON op stdout bevat `no [test_gate] command in lighthouse.toml — nothing was tested`.
  - `test_broken_config_blocks`: ongeldige TOML → rc 2, `COULD NOT CHECK`.
  - `test_unchecked_exit_code`: `command = "exit 5"`, `unchecked_exit_codes = [5]` → rc 2, `COULD NOT CHECK`, niet "tests failed".
  - `test_command_sees_gate_tmp`: `command = 'test -d "$LIGHTHOUSE_GATE_TMP"'` → rc 0.
  - `test_hooks_with_minimal_config`, geparametriseerd over alle hooks in `hooks.json`, elk met de payload die hem raakt (een `git status`, een Edit in een featurebranch-worktree, een `gh pr create` met geldige body, een `echo x > a.md`), onder een `lighthouse.toml` met alleen `[project]` en `[forge]`: geen blokkade, geen `Traceback` op stderr.
- [ ] **Stap 3: V3–V6.** Het hardcoded testcommando (409-410), `gates.slice` (342-374) en de exitcodes 3/4/5 (491-511) worden de sleutels hierboven. `LC_ALL=C` op regel 197. `PAIA_GATE_*` → `LIGHTHOUSE_GATE_*`, `paia-gate-slots` → `lighthouse-gate-slots`.
- [ ] **Stap 4: V7.** Commit `feat(core): pre-commit-tests als mechanisme met [test_gate]`. Open PR 3.

---

## PR 4 — scripts en templates (`issue-7-b4-scripts`)

Elk script begint met de sys.path-regel uit ontwerpkeuze 6 en importeert daarna `lighthouse_lib`. Tests draaien het script vanuit `install_core`.

### Taak 15: `post_artefact` en `board_start`

**Files:**
- Create (V1): `pack/core/scripts/post_artefact.py`, `pack/core/scripts/board_start.py`, `tests/pack/test_post_artefact.py`, `tests/pack/test_board_start.py`

**Interfaces:**
- Consumes: `artefacts.*`, `protocol.Comment`, `config` (`forge.repo`, `paths.specs`, `paths.plans`, `forge.board.*`).
- Produces: `post_artefact.py <nr> <abs-pad>` geeft `artefact: …` (vast token). `board_start.py <nr>` en `board_start.py --reopen <nr>` geven `board: <status>`, `board: skipped — no [forge.board]` of `board: ERROR — <cause>`.

- [ ] **Stap 1–2: V1, V2.** Nieuw: `test_no_board_default_mode` en `test_no_board_reopen` (beide rc 0 met `board: skipped — no [forge.board]`; inventaris §2.8); `test_broken_config_is_error` (rc 2, `board: ERROR`); `test_post_artefact_paths_from_config` (`paths.plans = "p/"` → een plan onder `p/` wordt herkend).
- [ ] **Stap 3: V3–V7.** Commit `feat(core): post_artefact en board_start, board optioneel`

### Taak 16: `plan_review_record` en `review_pair`

**Files:**
- Create (V1): `pack/core/scripts/plan_review_record.py`, `pack/core/scripts/review_pair.py`, `tests/pack/test_plan_review_record.py`, `tests/pack/test_review_pair.py`

**Interfaces:**
- Consumes: `config.host`-functies (in plaats van `src.controller.host`), `datadir.data_dir()` (in plaats van `lk_common.data_dir()`), `headings`.
- Produces: subcommando's `start`, `start --kind spec`, `write`, `accept-open`, `pr-open-points`; `review_pair.py blind|merge`. `DEFAULT_SKILL_DIRS = (".claude/skills/lighthouse-plan-adversarial-review", ".claude/skills/lighthouse-spec-adversarial-review")`. `review_pair` laadt `plan_review_record` als `importlib`-buur vanuit zijn eigen map (inventaris §3: geen repo-root).

- [ ] **Stap 1–2: V1, V2.** `BUDGET_FIELDS` volgt de nieuwe budget-JSON uit taak 17. Nieuw: `test_data_dir_from_env` (`LIGHTHOUSE_DATA` → logs daar, niet onder `~/.local/share/lekmeting-keten`); `test_skill_dirs_are_lighthouse`; `test_open_after_review_heading_follows_language`.
- [ ] **Stap 3: V3–V6.** Van `lk_common` gaat alleen `data_dir()` mee; het bestand zelf niet. Zet `ACCEPTEER-OPEN MISLUKT` en de andere uitvoertokens om naar Engels (`ACCEPT-OPEN FAILED`); de koppen die in een document of PR landen gaan naar `headings.toml`.
- [ ] **Stap 4: V7.** Commit `feat(core): plan_review_record en review_pair`

### Taak 17: `codex_usage` (budget en read)

**Files:**
- Create (V1, alleen de delen voor `budget` en `read`): `pack/core/scripts/codex_usage.py`, `tests/pack/test_codex_usage.py`

**Interfaces:**
- Produces: `codex_usage.py budget` → JSON met `decision`, `reason`, `window`, `reset_at`, `session_pct`, `week_pct`, `source`; waarden `not_measurable`, `fallback` waar paia `niet_meetbaar`/`fallback` gaf. `codex_usage.py read`. Drempels uit `codex_usage.*`. `CLIENT_INFO` naam `lighthouse-codex-usage`. `LIGHTHOUSE_CODEX_BUDGET_CMD` vervangt `PAIA_CODEX_BUDGET_CMD`.

- [ ] **Stap 1–2: V1, V2.** Alleen de tests van `budget` en `read`; de mirror-test van `config.yaml` blijft in paia. Nieuw: `test_thresholds_from_config` en `test_defaults_without_table`.
- [ ] **Stap 3: V3–V6.** `gate`, `_parkeer`, `schrijf_marker` (535-642) en `PAIA_CONTROLLER_RUN` vallen weg. Noteer voor (d) dat `usage_window.py:742-757` het pad moet volgen.
- [ ] **Stap 4: V7.** Commit `feat(core): codex_usage budget en read, drempels uit [codex_usage]`

### Taak 18: Reviewmeting

**Files:**
- Create (V1): `pack/core/scripts/measure_start_issue_kpi.py`, `pack/core/scripts/measure_fix_pr_kpi.py`, `tests/pack/test_start_issue_kpi.py`, `tests/pack/test_fix_pr_kpi.py`

- [ ] **Stap 1–2: V1, V2.** Nieuw: `test_transcript_dir_from_git_root` (de projectmap onder `~/.claude/projects/` is de wortel met `/` en `.` vervangen door `-`; `HOME` in de test op `tmp_path`); `test_since_option` (`CONVENTION_DATE` wordt `--since YYYY-MM-DD`, zonder default); `test_pickup_verbs_both_languages`; `test_gate_line_any_language` (`Poort` en de `en`-variant).
- [ ] **Stap 3: V3–V6.** `measure_fix_pr_kpi` laadt `measure_start_issue_kpi` als buur in zijn eigen map. `SKILL_NAME`/`SCOPE_SKILLS` worden `lighthouse-*`.
- [ ] **Stap 4: V7.** Commit `feat(core): reviewmeting voor start-issue en fix-pr`

### Taak 19: `worktree_audit`, `unblock_dependents` en de workflow

**Files:**
- Create (V1): `pack/core/scripts/worktree_audit.py`, `pack/core/scripts/unblock_dependents.py`, `pack/core/workflows/unblock-dependents.yml`, `tests/pack/test_worktree_audit.py`, `tests/pack/test_unblock_dependents.py`, `tests/pack/test_workflow_unblock.py`

- [ ] **Stap 1–2: V1, V2.** Nieuw: `test_protected_from_config` (`entire/*` en `.port-registry` zijn alleen beschermd als ze in `worktree_audit.protected_refs`/`protected_paths` staan; `main` altijd); `test_blocked_label_from_config` (`labels.blocked_issue = "x"`); `test_release_line_token` (`unblock_dependents` print `Released lane after this close:` als vast token); in `test_workflow_unblock.py`: de workflow heeft `runs-on: @@ci.runs_on@@`, `@@ci.bot_client_id_var@@`, `@@ci.bot_private_key_secret@@`, een `run-name` die eindigt op ` #${{ … }}`, en roept `.lighthouse/scripts/unblock_dependents.py` aan.
- [ ] **Stap 3: V3–V6.** `worktree_audit` haalt alleen `repo_slug()` uit de config (`forge.repo`). Zijn uitvoertokens `opruimen: ja`/`nee — <reden>` worden `cleanup: yes`/`no — <reason>`.
- [ ] **Stap 4: V7.** Commit `feat(core): worktree_audit, unblock_dependents en de workflow`

### Taak 20: `check-instructions.sh`

**Files:**
- Create (V1): `pack/core/scripts/check-instructions.sh` (uit `scripts/hooks/check-instructies.sh`), `tests/pack/test_check_instructions.py` (uit `test_check_instructies.py`)

- [ ] **Stap 1–2: V1, V2.** Alleen de generieke checks gaan mee: 1, 2, 3, 4, 5, 7, 8, 10 en 11, met hun nummers; 6, 9, 12 en 13 blijven in paia, en hun tests ook. Nieuw: `test_checks_1_2_without_skills_dir_say_skipped` (geen `.claude/skills` → `skipped — no .claude/skills`, niet `ok`; inventaris §6); `test_check_8_sees_hook_subdirs` (een hook in `.claude/hooks/lighthouse/`); `test_check_11_skips_marked_skills` (een skillmap waarvan `SKILL.md` `metadata.lighthouse` draagt); `test_check_10_ceiling_from_config`; `test_check_4_stopwords_both_languages` (`LIGHTHOUSE_STOPWORDS` vervangt `PAIA_STOPWORDS`).
- [ ] **Stap 3: V3–V6.** De repo-root via `LIGHTHOUSE_REPO_ROOT`/`git rev-parse` in plaats van `BASH_SOURCE/../..` (18); de uitzonderingslijst van check 11 uit `paths.mirror_exceptions` (ontbreekt hij, dan geen uitzonderingen).
- [ ] **Stap 4: V7.** Commit `feat(core): check-instructions.sh met de generieke checks`

### Taak 21: PR-template

**Files:**
- Create (V1): `pack/core/templates/pull_request_template.md`; `tests/pack/test_pr_template.py`

- [ ] **Stap 1: Test** `test_template_names_lighthouse_verify_pr` (`/lighthouse-verify-pr` op de plekken van `/paia-verify-pr`, regels 11 en 23) en `test_template_is_hostless` (geen regel over een vaste host; regel 16 wordt "a test that cannot run in CI").
- [ ] **Stap 2: V4–V7.** De template is een pakketbestand en dus Engels; zijn tokens (`Closes #N`, `### Evidence #N`) zijn al vast Engels. Een project dat hem in het Nederlands wil, wijzigt hem lokaal; `--upgrade` meldt dat dan als lokale wijziging (besluit 4). Commit `feat(core): PR-template`. Open PR 4.

---

## PR 5 — skills en agents (`issue-7-b5-skills`)

Voor elke skill geldt het recept, met V4 als zwaartepunt. Per skill: één kopie-commit met de `.claude`- en de `.agents`-variant uit paia, en één aanpas-commit. Daardoor toont de diff van de aanpas-commit de vertaling regel voor regel (spec, *Risico's*). Een skill met byte-identieke varianten (inventaris §8) wordt één `SKILL.md`. Elke aanroep gaat over naar ontwerpkeuze 6 en 7; elke kop uit inventaris §5 naar `headings.toml`; elke verwijzing uit inventaris §7 naar ontwerpkeuze 16.

### Taak 22: Skillcontracttest en besluit grensgevallen

**Files:**
- Create: `tests/pack/test_skill_contracts.py`
- Modify: `docs/developer/planning/specs/2026-10-17-m0-pakket-design.md` (nieuwe sectie *Besluit grensgevallen* onder *Grensgevallen*)

**Interfaces:**
- Produces: per skill in `pack/core/skills/` en per agent, geparametriseerd:
  - `test_frontmatter_name`: de naam is `lighthouse-<map>`.
  - `test_referenced_files_exist`: elk pad `.lighthouse/scripts/<x>`, `.lighthouse/lib/lighthouse_lib/<x>.py` en `.claude/hooks/lighthouse/<x>` bestaat in `pack/core`.
  - `test_config_keys_known`: voor elke sleutel na `config.py ` geldt `config.is_known_key`.
  - `test_heading_keys_known`: elke verwijzing `heading:<key>` (ontwerpkeuze 19) noemt een sleutel uit `headings.toml`.
  - `test_skill_links_resolve`: elke `/lighthouse-<x>` is een skill in `pack/core`, en elke andere `/…`-skill staat in `ALLOWED_EXTERNAL` (de Superpowers-skills).
  - `test_optional_steps_guarded`: elke regel die `backlog-stand`, `board-sweep`, `release`, `pre-push` of `controller` noemt, staat in een alinea met `if present` of `skipped`.
  - `test_variants_both_present`: elke skill heeft een Claude- en een Agents-variant.

- [ ] **Stap 1: Schrijf de test** met een lege `pack/core/skills/`. Draai: PASS, want er valt niets te parametriseren. De volgende taken maken hem rood en weer groen.
- [ ] **Stap 2: Schrijf de sectie** *Besluit grensgevallen* in de spec. Neem de tabel uit inventaris §7 over, met per skill de reden en de wijziging van ontwerpkeuze 9 (`write-issue`: taxonomie via `[paths] label_taxonomy`, geen `[labels.axes.*]`). Neem ook de indeling van de checks uit taak 20 op.
- [ ] **Stap 3: Commit** `docs(spec): besluit grensgevallen voor M0 (b)` en `test(core): contracttest voor skills en agents`

### Taak 23: Agents

**Files:**
- Create (V1): `pack/core/agents/issue-impact-scout.md`, `pack/core/agents/spec-adversary.md`, `tests/pack/test_issue_impact_scout.py`

- [ ] **Stap 1–4:** V1–V7. Het `IMPACT PACK` en zijn zeven secties: de kop uit `headings.toml`, de sectienamen vast Engels (de scout schrijft ze naar de sessie, niet naar GitHub). De uitvoer van `spec-adversary` (`ROUND`, `STATUS:`, `VERDICT:`) is al Engels en blijft gelijk. Commit `feat(core): agents issue-impact-scout en spec-adversary`

### Taak 24: `start-issue`, `route-issue`, `handoff`

**Files:**
- Create (V1): `pack/core/skills/{start-issue,route-issue,handoff}/…`; tests uit `test_issue_skills_contract.py` (het deel van deze drie) en de handoff-regels uit `test_instructielagen.py:1617-1742` → `tests/pack/test_skills_issue_flow.py`

- [ ] **Stap 1–2: V1, V2.** De labels `3-blocked-*` in de tests worden `config.py labels.*`-aanroepen. Nieuw: `test_start_issue_skips_board_without_config`, waarin de bordstap het pad met rc 1 noemt; `test_handoff_writes_new_tokens`, waarin het handoff-blok `next:`, `when: now|night`, `anchor:` en de marker `<!-- lighthouse:` gebruikt; `test_start_issue_reads_legacy_stack`, waarin de skill `stack:` noemt en ook `stapel:` in oude comments.
- [ ] **Stap 3: V3–V6.** Uit `start-issue` gaan weg: de KPI-tabel 266-286 en de paia-data. De meetaanroepen blijven. `PAIA_CONTROLLER_RUN` → `LIGHTHOUSE_CONTROLLER_RUN` (noteer voor (d)). "Beelink" → "the always-on host (`host.toml`)". Het `.agents`-blok *Buiten Claude Code* (start-issue 241-248) staat alleen in `agents/SKILL.md`.
- [ ] **Stap 4: V7.** Commit per skill: `feat(core): skill <naam>, vertaald`

### Taak 25: `verify-pr`, `fix-pr`, `close-issue`

**Files:**
- Create (V1): `pack/core/skills/{verify-pr,fix-pr,close-issue}/…`; tests uit `test_verify_pr_skill.py`, `test_fix_pr_skill.py` en het close-issue-deel van `test_issue_skills_contract.py`

- [ ] **Stap 1–2: V1, V2.** Nieuw: `test_close_issue_finds_lighthouse_workflow` (`--workflow lighthouse-unblock-dependents.yml`); `test_close_issue_board_skip`, waarin `board: skipped` geen `FOUT`/`ERROR` meldt; `test_fix_pr_uses_test_gate_command`, waarin het testcommando uit `config.py test_gate.command` komt en de ratel van `pack/python` met "if present" staat; `test_verify_pr_evidence_by_path`, waarin `python3 .lighthouse/lib/lighthouse_lib/evidence.py` voorkomt.
- [ ] **Stap 3: V3–V6.** De release-flow (close-issue 602-698) en de pre-push-keten (fix-pr 137, 145-148) krijgen ontwerpkeuze 16. Haal `paia-bot` (close-issue 361-406) uit `config.py ci.bot_login`, met een overgeslagen stap als de sleutel ontbreekt. De roborev-stap van `fix-pr` (C:71-75, 189-209) krijgt ook ontwerpkeuze 16, met `test -e .roborev.toml`. De commit-URL's in close-issue (125, 480) komen uit `forge.repo`.
- [ ] **Stap 4: V7.** Commit per skill.

### Taak 26: De twee adversarial reviews

**Files:**
- Create (V1): `pack/core/skills/spec-adversarial-review/` (`SKILL.md`-varianten, `findings.schema.json`, `verification.schema.json`, `reviewer-prompt.md`, `verifier-prompt.md`, `review.sh`, `verify.sh`), idem `plan-adversarial-review/`; `pack/core/permissions.json`; tests uit `test_adversarial_review_skills.py`, `test_review_pair_skills.py`, `test_review_round_limit_instructies.py`, `test_controller_vanaf_spec_instructies.py`, `test_controller_config_regels.py`, `test_adversarial_review_sh.py`

**Interfaces:**
- Produces: `permissions.json` = `{"allow": ["Bash(.claude/skills/lighthouse-spec-adversarial-review/review.sh:*)", "Bash(.claude/skills/lighthouse-spec-adversarial-review/verify.sh:*)", "Bash(.claude/skills/lighthouse-plan-adversarial-review/review.sh:*)", "Bash(.claude/skills/lighthouse-plan-adversarial-review/verify.sh:*)"]}`. Plan-review `review.sh` en `verify.sh` doen `exec "$(dirname "$0")/../lighthouse-spec-adversarial-review/<x>.sh"`.

- [ ] **Stap 1–2: V1, V2.** `test_adversarial_review_sh.py:412-417` pint nu `permissions.json`. Nieuw: `test_permissions_cover_scripts`, waarin elk `review.sh`/`verify.sh` een regel in `permissions.json` heeft; `test_variants_keep_their_difference`: de `agents`-variant mist de controller-run, de gepaarde ronde 1 en de Opus-dispatch, en heeft `codex_same_family` (inventaris §8).
- [ ] **Stap 3: V3–V6.** De controller-run-blokken (spec-review C:52-62, plan-review C:38-47) krijgen ontwerpkeuze 16. "Beelink" → "the always-on host (`host.toml`)". `review.sh:102` roept `.lighthouse/scripts/codex_usage.py budget` aan.
- [ ] **Stap 4: V7.** Commit per skill.

### Taak 27: `write-issue`, `worktree-audit`, `instrument`

**Files:**
- Create (V1): `pack/core/skills/{write-issue,worktree-audit,instrument}/…` (`instrument` uit `paia-instrumenteren`)

- [ ] **Stap 1–2: V1, V2.** Nieuw: `test_write_issue_taxonomy_from_paths`, waarin de skill de taxonomie uit `config.py paths.label_taxonomy` leest en zonder dat bestand alleen `[labels]` en de GitHub-standaardlabels gebruikt; `test_write_issue_blocking_invariant` (de blokkeerregel noemt `labels.blocked_issue`); `test_instrument_lists_generic_checks_only` (3, 4, 5, 7, 8, 10, 11); `test_worktree_audit_no_opencode` (de verwijzing naar `scripts/opencode-lane/worktree-run.sh` krijgt ontwerpkeuze 16).
- [ ] **Stap 3: V3–V6.** De labeltabellen van `write-issue` (95-197, 345-350, 385-414) gaan uit de skill. Ze komen in de PR-beschrijving als inhoud voor paia's `label_taxonomy`-bestand in (d). `instrument` gebruikt `project.skill_prefix` en `project.instructions_word_ceiling`; de aanroepen van `measure_skill_surface.py` krijgen ontwerpkeuze 16.
- [ ] **Stap 4: V7.** Commit per skill.

### Taak 28: Instructieblok

**Files:**
- Modify: `pack/core/instructions/AGENTS.md`, `pack/core/requires.toml`, `internal/packs/core_test.go`

- [ ] **Stap 1: Test** in `core_test.go`: `TestCorePack` noemt nu alle eenheden van `pack/core`. Met het mechanisme van (a) en PR 1 geeft `Load` geen fout. `requires.toml` noemt `gh` (skills, `pr-create-gate`), `codex` (de reviewskills), `superpowers` (de skills die een Superpowers-skill aanroepen), `jq`, `git`, `python3` en `tomllib`.
- [ ] **Stap 2: Schrijf het blok.** Het bestaande blok, plus de ketenafspraken die een core-skill of -hook afdwingt, in het Engels: eerst een worktree, niets direct op `main`, elke wijziging via een PR met squash-merge, de test-gate, en de taal van artefacten uit `[project] language`. Daarbij de uitleg van `heading:<key>` (ontwerpkeuze 19) en de lijst van de skills met één regel per skill. Plafond: het blok telt ten hoogste 250 woorden, want paia zit op 2996 van de 3000 (spec, *Opdeling (d)*). Test `test_core_block_word_budget` in `tests/test_instruction_files.py`.
- [ ] **Stap 3: Draai** `go test ./... && uv run pytest -q && ci/literal-grep.sh` — verwacht: groen.
- [ ] **Stap 4: Commit** `feat(core): instructieblok met de ketenafspraken`. Open PR 5.

---

## PR 6 — lighthouse op zijn eigen `pack/core` (`issue-7-b6-dogfood`)

### Taak 29: `lighthouse init` op lighthouse

**Files:**
- Create: `lighthouse.toml`, `.lighthouse/` (door `init`), `.claude/skills/lighthouse-*`, `.agents/skills/lighthouse-*`, `.claude/agents/lighthouse-*`, `.claude/hooks/lighthouse/*`, `.github/pull_request_template.md`
- Delete: `.claude/hooks/main-commit-guard.sh`, `.claude/hooks/worktree-guard.sh`, `.claude/hooks/lib/`, `tests/hooks/`
- Modify: `.claude/settings.json`, `AGENTS.md`, `CLAUDE.md`, `.github/workflows/ci.yml`, `e2e/e2e_test.go`

- [ ] **Stap 1: Schrijf `lighthouse.toml`** met de hand, vóór `init`:
  ```toml
  [project]
  language = "nl"

  [forge]
  kind = "github"
  repo = "CorneeldH/lighthouse"

  [test_gate]
  command = "uv run pytest -q && go test ./..."
  ```
- [ ] **Stap 2: Draai** `go run ./cmd/lighthouse init` en lees de diff. Verwacht: geen conflict, geen schrijfactie buiten de paden van *Global Constraints*, `lighthouse.toml` alleen aangevuld (`[ci]`), en een melding dat de unblock-workflow niet geïnstalleerd is (geen `ci.bot_*`). Keur goed.
- [ ] **Stap 3: Ruim de bootstrap op**, in dezelfde commit: haal de twee oude registraties met de hand uit `.claude/settings.json` (`init` beheert alleen registraties onder `.claude/hooks/lighthouse/`), en verwijder de oude scripts, `lib/` en `tests/hooks/`. Werk `CLAUDE.md` *Hooks* bij naar `.claude/hooks/lighthouse/` en de hooks van het blok. Haal uit `AGENTS.md` de sectie *Bootstrap* weg, plus elke regel die het beheerde blok nu draagt (niets dubbel; `test_instruction_files.py`). De regels voor lighthouse zelf blijven (taal Nederlands, `allow-main`, links, planningsmappen).
- [ ] **Stap 4: CI.** In de job `go`, na de e2e-stap: `go run ./cmd/lighthouse init --upgrade` met de controle dat de uitvoer exact `No changes.` is (`test "$(go run ./cmd/lighthouse init --upgrade)" = "No changes."`). Breid `e2e_test.go` uit met `TestFreshRepoGetsFullCore`: na `init --yes` bestaan `.claude/skills/lighthouse-start-issue/SKILL.md`, `.agents/skills/lighthouse-start-issue/SKILL.md` en `.claude/hooks/lighthouse/pre-commit-tests.sh`. Een commit in een featurebranch zonder `[test_gate]` gaat door met de melding `nothing was tested`.
- [ ] **Stap 5: Draai** `uv run pytest -q && go test ./... && go test -tags e2e ./e2e/ && ci/literal-grep.sh && test "$(go run ./cmd/lighthouse init --upgrade)" = "No changes."` — verwacht: alles groen. Doe daarna een echte commit in de worktree: de geïnstalleerde `pre-commit-tests` draait de suite, en `main-commit-guard` uit `.claude/hooks/lighthouse/` blokkeert een commit op `main` (controleer met een payload zoals in de e2e-test).
- [ ] **Stap 6: Commit** `feat: lighthouse draait zijn eigen pack/core; bootstrap van (a0) weg`. Open PR 6 met `Closes #7`.

---

## Na de laatste taak

- Zet in #7 onder *Verbeterpunten*: commentaar, docstrings en interne namen in `pack/` naar het Engels (ontwerpkeuze 3); board-ID's meten in `init` en `[forge] default_branch` (ontwerpkeuze 17); de xdist-hint naar `pack/python` (ontwerpkeuze 10, voor (c)); de punten die tijdens de uitvoering opkomen.
- Zet in het issue van (d) wat paia moet aanpassen: het controllerbeleid uit taak 8 importeert `lighthouse_lib.protocol`; `triage.py` gebruikt de marker uit `protocol`; `LIGHTHOUSE_CONTROLLER_RUN` naast `PAIA_CONTROLLER_RUN`; `usage_window.py` laadt `.lighthouse/scripts/codex_usage.py`; de aanroepers van de hernoemde CLI-subcommando's; de labeltaxonomie in paia's `label_taxonomy`-bestand; paia's `[test_gate]` met `--basetemp="$LIGHTHOUSE_GATE_TMP"` en `unchecked_exit_codes = [3, 4, 5]`; `[labels]` met paia's labelnamen.
