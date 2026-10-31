# Kaartsite met gedeelde kaartcomponent en de ketenkaart — implementatieplan

> **Voor agents:** VERPLICHTE SUB-SKILL: gebruik superpowers:subagent-driven-development (aanbevolen) of superpowers:executing-plans om dit plan taak voor taak uit te voeren. Stappen gebruiken checkboxen (`- [ ]`).

**Doel:** Een statische site in `docs/site/` met één gedeelde kaartcomponent en daarop de
ketenkaart (K1), met de pakkettabel eronder, tests die falen als de kaartdata en `pack/` uit
elkaar lopen, en een Pages-deploy waar #89 alleen een pagina aan toevoegt.

**Architectuur:** De inhoud staat met de hand in `docs/site/data/chain.json` en
`docs/site/data/packs.json`. `map.js` en `map.css` zijn de gedeelde component: ze tekenen een
stroomkaart (kolommen, chips, routes, handoff-badges, detailpaneel, thema) uit één JSON-object.
`chain.html` laadt de data via `chain.js`; `index.html` is de ingang. Python-tests lezen de JSON
en `pack/` en vergelijken. Een eigen workflow `pages.yml` publiceert `docs/site/` zoals het is.

**Tech stack:** HTML, CSS, vanilla JS (ES-modules) en SVG; pytest (`json`, `tomllib`, `re`);
GitHub Actions (`configure-pages`, `upload-pages-artifact`, `deploy-pages`).

**Spec:** [2026-10-27-kaarten-design.md](../specs/2026-10-27-kaarten-design.md), §3, §4 (K1),
§7 (tests 2, 5, 6, 7) en §8, met de
[mockup](../specs/2026-10-27-kaarten-mockup.html) ernaast. Issue #99. K2–K6 (#100–#104) vallen
buiten dit plan.

## Globale randvoorwaarden

- Alle tekst op de site is Engels. Plan en commits zijn Nederlands. Testcode, namen en commentaar
  zijn Engels (`tests/**` staat in `code_language_paths`); dat geldt ook voor code in `docs/site/`.
- Geen framework, geen buildstap, geen externe bibliotheek. De site opent met
  `python3 -m http.server --directory docs/site`.
- Geen Npuls-huisstijl. Een eigen, neutrale stijl, licht en donker (spec §8).
- Een `source` is `https://github.com/CorneeldH/lighthouse/blob/main/<pad>`, nooit een relatieve
  link naar `../../pack/` (spec §8).
- Statussen letterlijk: `works`, `planned`, `idea`; het label letterlijk `not in first release`.
- Kinds letterlijk: `lighthouse`, `superpowers`, `hook`, `github`. Triggers letterlijk: `you` (⌨),
  `skill` (↪), `tool` (⚡), `github` (⚙).
- Verzin geen gedrag. Chipteksten komen uit de `description` en het werkverloop van de
  `SKILL.md`, uit `CLAUDE.md` *Hooks*, en voor `code-language` uit
  `pack/core/hooks/code-language.sh` (spec §10). De mockup is vorm, geen bron: zijn
  `pre-commit-tests` is verouderd, `commit-tests` draait sinds #80 ná de commit.
- Een geplande handoff krijgt `"milestone": "M2"`: de DoD van M2 eist "handoff, start, vraag,
  antwoord, hervat" end-to-end.
- Dit raakt `pack/` en `internal/` niet: geen `init --upgrade` (spec §8, *Dogfood*).
- Testgate: `[test_gate] command` uit `lighthouse.toml`. Gericht: `uv run pytest -q tests/test_site_*.py`.
- Afwijking van de spec, op verzoek van de eigenaar (comment op #99): de Pages-workflow hoort bij
  #99, niet bij #89 (spec §8 *Hosting*). #89 voegt alleen een pagina toe.

## Review-focus

1. **De site via `file://` openen.** `fetch` van de JSON faalt dan en de kaart blijft leeg;
   Chromium laadt een ES-module vanaf `file://` niet eens, zodat `chain.js` niet draait. De
   melding met het servercommando staat daarom zichtbaar in de statische HTML en `chain.js`
   verbergt haar pas na een geslaagde `loadJson`. Test in taak 3:
   `test_pages_name_the_local_server_command`; de headless check in taak 3, stap 6, opent `file://`.
2. **Tekst met `<` of `&` in de data**, zoals `issue-<nr>-<slug>`. Die moet letterlijk verschijnen,
   niet als HTML. De component zet data alleen via `textContent`. Test in taak 3:
   `test_component_never_writes_html_from_data`.
3. **Een smal scherm.** Zeven kolommen passen niet op een telefoon. De kaart scrolt dan
   horizontaal binnen haar eigen vak, de pagina niet. Headless check in taak 3, stap 6.
4. **Geen `localStorage`** (privévenster, geblokkeerde opslag). De themaknop werkt dan nog, alleen
   zonder geheugen; lezen en schrijven staan in `try/catch`. Headless check in taak 3, stap 6.
5. **Een route die een badge mist of een badge die nergens oplicht.** Een sprong omhoog zonder
   handoff, of een handoff op een paar dat in geen route opeenvolgt. Tests in taak 1:
   `test_every_upward_jump_has_a_handoff` en `test_every_handoff_sits_on_a_route_step`.

---

### Taak 1: Ketendata met dekkings- en statustests

**Files:**
- Create: `docs/site/data/chain.json`
- Test: `tests/test_site_chain.py`

**Interfaces:**
- Produces: het formaat van `chain.json`, dat taak 3 tekent:

  ```json
  {
    "title": "The chain",
    "subtitle": "From an idea to a closed issue, and what joins in at each step.",
    "phases": [{"id": "issue", "label": "Issue", "blurb": "what needs doing"}],
    "chips": [{
      "id": "write-issue", "phase": "issue", "row": 0,
      "kind": "lighthouse", "trigger": "you", "label": "write-issue",
      "what": "…", "when": "…", "without": "…",
      "status": "works", "source": "https://github.com/CorneeldH/lighthouse/blob/main/pack/core/skills/write-issue"
    }],
    "handoff": {"what": "…", "when": "…", "without": "…", "source": "https://github.com/CorneeldH/lighthouse/blob/main/pack/core/skills/handoff"},
    "handoffs": [{"from": "spec-adversarial-review", "to": "writing-plans", "status": "works"}],
    "routes": [{"id": "4c", "label": "Spec first (4c)", "chips": ["write-issue", "…"]}]
  }
  ```

  Optioneel per route: `note` (str), een zin die de kaart onder de routeknoppen toont zolang
  die route aan staat.

  Optioneel per chip: `issue` (int), `milestone` (str), `not_in_first_release` (bool). `source`
  ontbreekt alleen bij een chip van Superpowers of GitHub die lighthouse niet levert. Chips met
  `"phase": "always"` vormen de band "Always on". `row` is de plek binnen de fase, vanaf 0.
- Produces in de test: `ROOT`, `SITE = ROOT / "docs" / "site"`,
  `REPO_BLOB = "https://github.com/CorneeldH/lighthouse/blob/main/"`,
  `load(name: str) -> dict | list` (leest `SITE / "data" / name`),
  `repo_path(source: str) -> Path` (faalt op een `source` zonder `REPO_BLOB`),
  `missing_parts(chain: dict) -> list[str]` (de skill-, agent- en workflowpaden uit `pack/` die
  geen `source` in `chips` of `handoff` hebben). Taak 2 en 3 importeren `ROOT`, `SITE`, `load`.

- [ ] **Stap 1: Schrijf de falende tests in `tests/test_site_chain.py`**

  Patroon: `tests/test_getting_started.py` (module-docstring, `ROOT` via `parents[1]`, assert op
  de lijst van wat ontbreekt).

  - `test_phases_are_the_seven_of_the_chain`: `[p["label"] for p in phases] == ["Issue", "Pick up", "Spec", "Plan", "Build", "PR", "Close"]`.
  - `test_always_on_band`: `{c["id"] for c in chips if c["phase"] == "always"} == {"usage-guard", "instrument", "worktree-audit"}`.
  - `test_chips_use_the_fixed_vocabulary`: elke chip heeft `id`, `phase`, `row`, `kind`, `trigger`,
    `label`, `what`, `when`, `without`, `status`; `kind`, `trigger` en `status` uit de lijsten van
    de randvoorwaarden; `phase` is een fase-`id` of `"always"`; `id` uniek; `(phase, row)` uniek.
  - `test_every_pack_part_is_on_the_chain`: `missing_parts(load("chain.json")) == []`. Delen zijn
    `pack/*/skills/*/` (map, één keer, ook met `agents/`- en `claude/`-variant),
    `pack/*/agents/*.md` en `pack/*/workflows/*`.
  - `test_coverage_flags_a_removed_skill` (de mutatiecheck uit de AC): haal uit een kopie de chip
    met `id == "write-issue"`; `missing_parts(kopie) == ["pack/core/skills/write-issue"]`.
  - `test_every_lighthouse_source_exists`: elke chip met `kind` `lighthouse` of `hook` heeft een
    `source`; elke `source` (chips en `handoff`) die met `REPO_BLOB` begint, wijst naar een
    bestaand pad. Een `source` met een ander begin dan `REPO_BLOB` bij `lighthouse` of `hook` faalt.
  - `test_planned_has_an_issue_or_milestone`: elke chip en elke handoff met `status == "planned"`
    heeft `issue` of `milestone`.
  - `test_works_handoffs_are_after_the_reviews`: de paren met `works` zijn precies
    `{("spec-adversarial-review", "writing-plans"), ("plan-adversarial-review", "executing-plans")}`,
    en voor elk bevat een `SKILL.md` onder `repo_path(from_chip["source"])` (rglob) de tekst
    `lighthouse-handoff`. Alle andere handoffs zijn `planned`.
  - `test_routes_name_existing_chips`: route-`id`'s zijn precies `{"1", "2", "4a", "4b", "4c", "red-pr"}`
    en elke `id` in een route is een chip. Route `1` heeft een `note` die `refresh-expo-lockfiles`
    een voorbeeld noemt (spec §4: "de skill die het issue sluit").
  - `test_every_upward_jump_has_a_handoff`: voor elk opeenvolgend paar `(a, b)` in een route met
    fase-index `b` > `a` en `b.row < a.row` bestaat een handoff `(a, b)`.
  - `test_every_handoff_sits_on_a_route_step`: elk handoff-paar is een opeenvolgend paar in
    minstens één route en een sprong omhoog volgens dezelfde regel.

- [ ] **Stap 2: Draai de tests en zie ze falen**

  Run: `uv run pytest -q tests/test_site_chain.py`
  Verwacht: FAIL, `FileNotFoundError` op `docs/site/data/chain.json`.

- [ ] **Stap 3: Schrijf `docs/site/data/chain.json`**

  Fasen-`id`'s: `issue`, `pickup`, `spec`, `plan`, `build`, `pr`, `close`. De chips, per fase
  (`id`, kind, trigger, `source`-pad achter `REPO_BLOB`):

  | Fase | Chips |
  |---|---|
  | issue | `write-issue` (lighthouse, you, `pack/core/skills/write-issue`), `github-issue` (github, github, geen source) |
  | pickup | `start-issue` (lighthouse, you), `route-issue` (lighthouse, skill), `using-git-worktrees` (superpowers, skill), `issue-impact-scout` (lighthouse, skill, `pack/core/agents/issue-impact-scout.md`), `worktree-guard` (hook, tool, `pack/core/hooks/worktree-guard.sh`) |
  | spec | `brainstorming` (superpowers, skill, row 0), `spec-adversarial-review` (lighthouse, you, row 1), `spec-adversary` (lighthouse, skill, `pack/core/agents/spec-adversary.md`, row 2) |
  | plan | `writing-plans` (superpowers, skill, row 0), `plan-adversarial-review` (lighthouse, you, row 1) |
  | build | `executing-plans` (superpowers, you, row 0, label `executing-plans / SDD`), `systematic-debugging`, `test-driven-development` (superpowers, skill), `testing`, `go-writing`, `python-writing`, `shell-gates` (lighthouse, skill, onder `pack/<pakket>/skills/`), `refresh-expo-lockfiles` (lighthouse, you), `main-commit-guard`, `commit-tests`, `bash-write-gates`, `worktree-linkcheck`, `code-language` (hook, tool) |
  | pr | `verify-pr` (lighthouse, you), `pr-create-gate` (hook, tool), `pr-and-ci` (github, github, `pack/core/templates/pull_request_template.md`), `fix-pr` (lighthouse, you), `receiving-code-review` (superpowers, skill) |
  | close | `close-issue` (lighthouse, you), `unblock-dependents` (github, github, `pack/core/workflows/unblock-dependents.yml`) |
  | always | `usage-guard` (hook, tool), `instrument` (lighthouse, you), `worktree-audit` (lighthouse, you) |

  Alle chips `works`; geen chip krijgt `not_in_first_release`, want K1 toont geen `serve` en geen
  controller. De rijen buiten de vaste waarden kiest de bouwer.

  Routes (spec §4):

  | `id` | `label` | `chips` |
  |---|---|---|
  | `4c` | `Spec first (4c)` | write-issue, github-issue, start-issue, route-issue, using-git-worktrees, issue-impact-scout, brainstorming, spec-adversarial-review, writing-plans, plan-adversarial-review, executing-plans, test-driven-development, commit-tests, verify-pr, pr-create-gate, pr-and-ci, close-issue, unblock-dependents |
  | `4b` | `Plan (4b)` | als 4c, zonder brainstorming en spec-adversarial-review |
  | `4a` | `Small job (4a)` | write-issue … using-git-worktrees, testing, commit-tests, verify-pr, pr-create-gate, pr-and-ci, close-issue |
  | `2` | `Bug (2)` | write-issue … using-git-worktrees, systematic-debugging, test-driven-development, commit-tests, verify-pr, pr-create-gate, pr-and-ci, close-issue |
  | `1` | `Known recipe (1)` | write-issue … using-git-worktrees, refresh-expo-lockfiles, commit-tests, verify-pr, pr-create-gate, pr-and-ci, close-issue |
  | `red-pr` | `Red PR` | pr-and-ci, fix-pr, receiving-code-review, pr-and-ci |

  Route `1` krijgt `"note": "The skill whose description matches the issue does the work and closes it; refresh-expo-lockfiles is one example."`
  Spec §4 zegt "de skill die het issue sluit": `refresh-expo-lockfiles` staat in de route als
  voorbeeld, niet als de enige.

  `handoffs`: precies de sprongen omhoog die de routes geven (de tests in stap 1 dwingen dat af).
  De twee na de reviews zijn `works`; de rest is `planned` met `"milestone": "M2"`. De tekst van
  `handoff` komt uit `pack/core/skills/handoff/SKILL.md`.

- [ ] **Stap 4: Draai de tests en zie ze slagen**

  Run: `uv run pytest -q tests/test_site_chain.py`
  Verwacht: PASS.

- [ ] **Stap 5: Commit**

  ```bash
  git add docs/site/data/chain.json tests/test_site_chain.py
  git commit -m "feat(site): ketendata met dekkings- en statustests (#99)"
  ```

### Taak 2: Pakkettabel-data met test

**Files:**
- Create: `docs/site/data/packs.json`
- Test: `tests/test_site_packs.py`

**Interfaces:**
- Consumes: `ROOT`, `load` uit `tests/test_site_chain.py`.
- Produces: `packs.json`, een lijst in de volgorde core, go, python, shell, expo:

  ```json
  [{"name": "go", "proposed_when": "a go.mod in the repository root", "detect": ["go.mod"],
    "skills": ["go-writing"], "agents": [], "workflows": [],
    "hooks": [{"name": "gofmt", "events": ["PostToolUse"], "when": "after every edit"},
              {"name": "go-check", "events": ["Stop"], "when": "when the session stops"}],
    "extra": []}]
  ```

  `extra` is vrije tekst per regel (core: de PR-template; python: dat het de test gate zet).

- [ ] **Stap 1: Schrijf de falende tests in `tests/test_site_packs.py`**

  - `test_every_pack_is_listed`: `[p["name"] for p in packs]` als set gelijk aan de mappen in
    `pack/` met een `skills/`-map.
  - `test_detect_matches_detect_toml`: `detect` is de `files`-lijst van `pack/<naam>/detect.toml`,
    of de vereniging van `contains[].files`; zonder `detect.toml` (core) is het `[]` en
    `proposed_when == "always"`. Voor elke `contains[].text` staat die tekst zonder
    aanhalingstekens (`expo`) in `proposed_when`.
  - `test_skills_agents_workflows_match`: `skills`, `agents` en `workflows` zijn gesorteerd gelijk
    aan de mapnamen onder `skills/`, de stems onder `agents/*.md` en onder `workflows/*`.
  - `test_hooks_match_hooks_json`: `{h["name"] for h in hooks}` is de set script-stems uit elk
    `command` in `pack/<naam>/hooks/hooks.json` (`hooks/<stem>.sh`, argumenten weg); zonder
    `hooks.json` leeg. Core telt er 8. Per hook is `sorted(events)` gelijk aan de gesorteerde
    set events in `hooks.json` waaronder die stem voorkomt.

  De aantallen op de pagina komen uit de lengte van deze lijsten (taak 3); daarmee dekt deze test
  ook de aantallen.

- [ ] **Stap 2: Draai de tests en zie ze falen**

  Run: `uv run pytest -q tests/test_site_packs.py`
  Verwacht: FAIL, `FileNotFoundError` op `docs/site/data/packs.json`.

- [ ] **Stap 3: Schrijf `docs/site/data/packs.json`** volgens de tabel in spec §4 (*Pakketten*).
  `events` komt uit `hooks.json`; `when` staat letterlijk hieronder (uit `hooks.json` en
  `CLAUDE.md` *Hooks*), verzin geen andere tekst:

  | Hook | `events` | `when` |
  |---|---|---|
  | `main-commit-guard` | PreToolUse | `before every shell command` |
  | `commit-tests` | PreToolUse, PostToolUse, PostToolUseFailure | `around every shell command` |
  | `bash-write-gates` | PreToolUse, PostToolUse, PostToolUseFailure | `around every shell command` |
  | `pr-create-gate` | PreToolUse | `before every shell command` |
  | `worktree-guard` | PreToolUse | `before every edit` |
  | `worktree-linkcheck` | PostToolUse | `after every edit` |
  | `code-language` | PostToolUse | `after every edit` |
  | `usage-guard` | PostToolUse, Stop, SubagentStop, UserPromptSubmit | `at every prompt, tool call, subagent stop and session stop` |
  | `gofmt`, `ruff-format`, `shellcheck` | PostToolUse | `after every edit` |
  | `go-check`, `ty-check` | Stop | `when the session stops` |

- [ ] **Stap 4: Draai de tests en zie ze slagen**

  Run: `uv run pytest -q tests/test_site_packs.py`
  Verwacht: PASS.

- [ ] **Stap 5: Commit**

  ```bash
  git add docs/site/data/packs.json tests/test_site_packs.py
  git commit -m "feat(site): pakkettabel-data met test tegen pack/ (#99)"
  ```

### Taak 3: Gedeelde component en de ketenpagina

**Files:**
- Create: `docs/site/map.js`, `docs/site/map.css`, `docs/site/chain.html`, `docs/site/chain.js`
- Test: `tests/test_site_pages.py`

**Interfaces:**
- Consumes: het formaat van `chain.json` (taak 1) en `packs.json` (taak 2).
- Produces in `map.js` (ES-module), voor K2–K6:
  - `renderFlowMap(root: HTMLElement, data: object, detail: HTMLElement): void` — kolommen,
    chips als `<button>`, de band "Always on", route-knoppen, handoff-badges, detailpaneel;
  - `initTheme(button: HTMLElement): void` — volgt `prefers-color-scheme`, de knop wisselt en
    onthoudt in `localStorage` (in `try/catch`);
  - `loadJson(url: string): Promise<object>` — gooit bij een mislukte `fetch`.
- Produces in de test: `relative_links(path: Path) -> list[str]` (`href`, `src`, `data-src` in
  HTML; `import … from "…"` en `loadJson("…")` in JS; `](…)` in markdown; zonder `http(s):`,
  `mailto:` en `#`).
  Taak 4 breidt de lijst bestanden uit.

- [ ] **Stap 1: Schrijf de falende tests in `tests/test_site_pages.py`**

  - `test_site_links_resolve`: voor elke `docs/site/**/*.html` en `docs/site/**/*.js` wijst elke
    `relative_links` naar een bestaand bestand, relatief aan het bestand.
  - `test_pages_name_the_local_server_command`: `chain.html` bevat letterlijk
    `python3 -m http.server --directory docs/site`, in een element met `id="serve-hint"` dat in
    de HTML geen attribuut `hidden` en geen `display: none` heeft (de melding moet zonder JS
    zichtbaar zijn).
  - `test_component_never_writes_html_from_data`: `map.js` en `chain.js` bevatten geen
    `innerHTML`, `outerHTML`, `insertAdjacentHTML` of `document.write`.
  - `test_page_lang_is_english`: elke `docs/site/**/*.html` heeft `<html lang="en">`.

- [ ] **Stap 2: Draai de tests en zie ze falen**

  Run: `uv run pytest -q tests/test_site_pages.py`
  Verwacht: FAIL op `chain.html` dat niet bestaat.

- [ ] **Stap 3: Bouw `map.css` en `map.js`**

  Vorm uit de mockup, met deze vaste keuzes:
  - kleurtokens op `:root`, opnieuw gezet onder `@media (prefers-color-scheme: dark)` en onder
    `:root[data-theme="dark"]` / `[data-theme="light"]`; per kind een eigen kleur, in beide thema's
    leesbaar; een expliciete `background` op `body`;
  - het teken van de trigger vóór het label: `you` ⌨, `skill` ↪, `tool` ⚡, `github` ⚙;
  - status: `works` vol, `planned` gestippeld met `#<issue>` of de milestone, `idea` gestippeld en
    grijs; `not_in_first_release` toont het label `not in first release`;
  - de kaarthoogte volgt uit de hoogste `row`; de kaart staat in een vak met `overflow-x: auto` en
    een `min-width`, zodat alleen dat vak scrolt;
  - een route-knop licht de route op en dimt de rest; een handoff-badge licht alleen op als zijn
    paar in de gekozen route opeenvolgt; de eerste route staat bij laden aan; de `note` van de
    gekozen route staat onder de routeknoppen;
  - het detailpaneel toont: het label en het teken, `what`, dan "When it starts" (`when`), "Who
    provides it" (uit `kind`, met het pakket uit het `source`-pad), "What you miss without"
    (`without`), de status, en een link "Source" naar `source`. Een badge toont `handoff` met zijn
    status;
  - alle data via `textContent` of attributen, nooit als HTML.

- [ ] **Stap 4: Bouw `chain.html` en `chain.js`**

  `chain.html`: titel, ondertitel, legenda (kinds, tekens, statussen, badge vol/gestippeld), de
  themaknop, een vak voor de kaart, het detailpaneel, en daaronder `<section id="packs">` met de
  pakkettabel. De link terug naar `index.html` komt in taak 4, als `index.html` bestaat. Een
  `<p id="serve-hint">` met het servercommando staat zichtbaar in de HTML; `chain.js` verbergt
  haar pas als `loadJson` slaagt. `chain.js` laadt `data/chain.json` en
  `data/packs.json`, roept `renderFlowMap` en `initTheme` aan, en rendert de pakkettabel: per
  pakket `proposed_when`, de skills, de agents en workflows, de hooks met `when`, en `extra`, met
  de aantallen uit de lijstlengtes.

- [ ] **Stap 5: Draai de tests en zie ze slagen**

  Run: `uv run pytest -q tests/test_site_pages.py`
  Verwacht: PASS.

- [ ] **Stap 6: Headless check in de browser (wegwerpscript, niet committen)**

  In de worktree: `npm ci --prefix web` (Playwright 1.64.1 uit `web/package.json`; chromium staat
  in de cache `~/.cache/ms-playwright`). Start `python3 -m http.server --directory docs/site 8099`
  op de achtergrond. Schrijf in de scratchpad een CommonJS-script `site-check.cjs` naar het
  patroon van `web/e2e/screenshot.mjs` (`const { chromium } = require('@playwright/test')`) en
  draai het met `NODE_PATH=web/node_modules node <scratchpad>/site-check.cjs`. Het script faalt
  (exit 1) zodra één assert niet klopt, en checkt op `http://localhost:8099/chain.html`:
  - zeven kolommen en de band "Always on" zijn zichtbaar;
  - per route-knop: na een klik zijn precies de chips van die route niet gedimd, lichten precies
    de badges van haar handoff-paren op, en staat haar `note` (als ze die heeft) in beeld;
    over de hele kaart zijn twee badges vol;
  - Tab naar een chip en Enter: het detailpaneel toont het label van die chip;
  - de themaknop wisselt `data-theme` op `<html>`; ook in een context waarin `localStorage`
    gooit (`addInitScript` dat de getter vervangt door een `throw`), zonder `pageerror`;
  - bij een viewport van 375 px breed: `document.documentElement.scrollWidth <= 375` en het
    kaartvak heeft `scrollWidth > clientWidth`;
  - `file://<worktree>/docs/site/chain.html`: `#serve-hint` is zichtbaar;
  - schermafdrukken licht en donker naar de scratchpad, voor het bewijs in de PR.

  Lukt `npm ci` of de start van chromium niet, meld dat dan letterlijk en zet deze lijst als
  handstap voor de eigenaar in de PR-body; vink hem niet zelf af. Stop de server na afloop en
  controleer dat `git status` geen bestand van de check toont.

- [ ] **Stap 7: Commit**

  ```bash
  git add docs/site/map.js docs/site/map.css docs/site/chain.html docs/site/chain.js tests/test_site_pages.py
  git commit -m "feat(site): gedeelde kaartcomponent en de ketenpagina (#99)"
  ```

### Taak 4: Ingang en link vanuit de startgids

**Files:**
- Create: `docs/site/index.html`
- Modify: `docs/site/chain.html` (link terug naar `index.html`)
- Modify: `docs/getting-started.md` (sectie `## Your first issue`)
- Modify: `tests/test_site_pages.py`

**Interfaces:**
- Consumes: `relative_links`, `initTheme`, `map.css`.

- [ ] **Stap 1: Breid de tests uit**

  - `test_site_links_resolve` neemt ook `docs/getting-started.md` mee.
  - `test_index_lists_the_six_maps`: `index.html` linkt naar `chain.html` en noemt `#100`, `#101`,
    `#102`, `#103` en `#104`.
  - `test_chain_links_back_to_index`: `chain.html` heeft een `href="index.html"`.
  - `test_guide_links_to_the_chain_map`: de sectie `## Your first issue` van de gids bevat de
    link `site/chain.html` en het servercommando.

- [ ] **Stap 2: Draai de tests en zie ze falen**

  Run: `uv run pytest -q tests/test_site_pages.py`
  Verwacht: FAIL op `index.html` en op de gids.

- [ ] **Stap 3: Schrijf `index.html` en de gidsregel**

  `index.html`: `<header>` met naam en één zin, dan `<section id="maps">` met zes tegels. K1 is
  een link naar `chain.html`; K2–K6 zijn tegels zonder link, `planned` met hun issue (spec, tabel
  bovenaan). Een HTML-commentaar boven `#maps` zegt dat #89 de download daarboven zet. Zelfde
  `map.css` en themaknop. In `chain.html` komt de link terug naar `index.html`.

  In `docs/getting-started.md`, onder `## Your first issue`: één zin met een relatieve link naar
  `site/chain.html` (als markdownlink) en het commando
  `python3 -m http.server --directory docs/site` om hem te openen.

- [ ] **Stap 4: Draai de tests en zie ze slagen**

  Run: `uv run pytest -q tests/test_site_pages.py tests/test_getting_started.py`
  Verwacht: PASS.

- [ ] **Stap 5: Commit**

  ```bash
  git add docs/site/index.html docs/site/chain.html docs/getting-started.md tests/test_site_pages.py
  git commit -m "feat(site): ingang met de zes kaarten en link vanuit de startgids (#99)"
  ```

### Taak 5: Pages-deploy van `docs/site/`

**Files:**
- Create: `.github/workflows/pages.yml`
- Test: `tests/test_pages_workflow.py`

**Interfaces:**
- Produces: een deploy die alles onder `docs/site/` publiceert. #89 voegt een pagina toe zonder
  deze workflow te raken.

- [ ] **Stap 1: Schrijf de falende tests in `tests/test_pages_workflow.py`**

  Tekstuele checks, zoals `tests/pack/test_workflow_unblock.py` (geen YAML-bibliotheek):
  - `test_publishes_the_site_folder`: een regel `path: docs/site`.
  - `test_runs_on_site_changes_on_main`: `branches: [main]`, `- "docs/site/**"` onder `paths` en
    `workflow_dispatch:`.
  - `test_permissions_for_pages`: `pages: write`, `id-token: write`, `contents: read`.
  - `test_job_has_a_runner`: een regel `runs-on: ubuntu-latest`.
  - `test_actions_are_pinned`: elke `uses:`-regel matcht `@[0-9a-f]{40} # v\d`.

- [ ] **Stap 2: Draai de tests en zie ze falen**

  Run: `uv run pytest -q tests/test_pages_workflow.py`
  Verwacht: FAIL, `FileNotFoundError`.

- [ ] **Stap 3: Schrijf `.github/workflows/pages.yml`**

  Kopcommentaar zoals `ci.yml`. Eén job `deploy` met `runs-on: ubuntu-latest`, `environment: github-pages` met
  `url: ${{ steps.deployment.outputs.page_url }}`, `concurrency: {group: pages, cancel-in-progress: false}`,
  `timeout-minutes: 10`. Stappen met deze pins:

  ```yaml
  - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
  - uses: actions/configure-pages@45bfe0192ca1faeb007ade9deae92b16b8254a0d # v6.0.0
  - uses: actions/upload-pages-artifact@fc324d3547104276b827a68afc52ff2a11cc49c9 # v5.0.0
    with:
      path: docs/site
  - id: deployment
    uses: actions/deploy-pages@368f82528645a54fb793d4d04e342629a3f51346 # v5.0.1
  ```

- [ ] **Stap 4: Draai de tests en zie ze slagen**

  Run: `uv run pytest -q tests/test_pages_workflow.py`
  Verwacht: PASS.

- [ ] **Stap 5: Commit**

  ```bash
  git add .github/workflows/pages.yml tests/test_pages_workflow.py
  git commit -m "ci(pages): publiceer docs/site/ via GitHub Pages (#99)"
  ```

- [ ] **Stap 6: Handstap voor de eigenaar, in de PR-body**

  Pages staat nog uit (`has_pages: false`). Vóór de merge zet de eigenaar
  *Settings → Pages → Source* op **GitHub Actions**: de merge zelf is een push op `docs/site/**`
  en start de eerste deploy. Staat Pages bij de merge nog uit, dan is die run rood en herstelt
  `workflow_dispatch` na het aanzetten hem. De uitvoerder zet die instelling niet zelf. Of de site
  live staat, controleert `/lighthouse-close-issue` na de merge.

### Taak 6: Eindcontrole

- [ ] **Stap 1: Volledige testgate**

  Run: het `[test_gate] command` uit `lighthouse.toml`.
  Verwacht: groen.

- [ ] **Stap 2: Mutatiecheck met de hand**

  Haal de chip `write-issue` uit `docs/site/data/chain.json`, draai
  `uv run pytest -q tests/test_site_chain.py` en zie `test_every_pack_part_is_on_the_chain`
  rood met `pack/core/skills/write-issue`. Zet de chip terug (`git checkout docs/site/data/chain.json`)
  en zie groen. Doe hetzelfde met de hook `gofmt` in `packs.json` en `test_hooks_match_hooks_json`.
  Noteer beide in het bewijs van de PR.

- [ ] **Stap 3: PR via `/lighthouse-verify-pr`**, met `Closes #99` en de handstap uit taak 5.
