# Repokaart als visuele boom — implementatieplan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `docs/site/repo.html` toont na `lighthouse init` een ingesprongen boom van je repo met één gekleurde rij per plek, een schakelaar *Everything / Adds / Edits your file* en uitleg als gewone tekst eronder.

**Architecture:** `docs/site/data/repo.json` krijgt een nieuwe vorm (`dirs`/`rows`). Een pure helper `repoEntries(data, mode)` in `docs/site/map.js` maakt daar mappen met rijen en een `dim`-vlag van; `renderRepo(root, data)` bouwt er geneste `ul`/`li` met native links van. `tests/test_site_repo.py` houdt de data gelijk met `internal/initcmd/testdata/places.golden`.

**Tech Stack:** statische HTML/CSS/ES-modules, pytest, node (via `tests/_site_node.py`).

**Spec:** [`../specs/2026-11-01-plattegrond-na-init-design.md`](../specs/2026-11-01-plattegrond-na-init-design.md) (versie 3)

## Global Constraints

- Paginateksten zijn Engels; dit plan, commits en PR zijn Nederlands.
- Data bereikt de pagina alleen via `textContent` en attributen (`tests/test_site_pages.py::test_component_never_writes_html_from_data`).
- Geen nieuwe kleuren: alleen bestaande tokens en `kind-*`/`form-*`-klassen; licht en donker via die tokens.
- Na elke wijziging aan site-JS of -CSS: `python3 -m tests.site_version`.
- `internal/` en `pack/` blijven ongemoeid; de Go-test en `places.golden` veranderen niet. Geen `init --upgrade` nodig.
- `CLAUDE.md` staat niet op de kaart; de test sluit hem uit met `OFF_THE_MAP = {"CLAUDE.md": "<reden>"}`.
- Een ↗ is een gewone `a` zonder zichtbare tekst; geen paneel, geen klik op het vak, geen legenda.

## Review Focus

1. **Toetsenbord.** Tab loopt langs de drie schakelaarknoppen en dan langs elke ↗; elke link heeft een toegankelijke naam uit de rij (`aria-label` `More about <label>`). Test: `test_links_have_an_accessible_name` (taak 2).
2. **360px.** Lange paden (`~/.config/lighthouse/host.toml`, `workflows/lighthouse-*.yml`) breken af; geen horizontale scroll. Alleen met browser te controleren (taak 3, stap browsercheck).
3. **Onbekende soort of modus.** Een `kind` die niet in `SORTS` staat of een onbekende `mode` geeft een fout, geen lege glyph of stille "alles vol". Tests: `test_entries_refuse_an_unknown_mode`, `test_entries_refuse_an_unknown_kind` (taak 2).
4. **Schermlezer en dimmen.** Gedimd is alleen opacity; de rij zegt in `sr-only` of `init` hem toevoegt of aanpast, zodat de schakelaar niet de enige drager is. Test: `test_rows_say_what_init_does` (taak 2).
5. **Donker.** De grijze *your own …*-rijen en het werkstationvak gebruiken `--border`/`--muted`/`--session`, die in donker bestaan. Alleen met browser te controleren (taak 3).

---

## Bestanden

| Bestand | Wat |
|---|---|
| `docs/site/data/repo.json` | nieuwe vorm, handgeschreven |
| `tests/test_site_repo.py` | herschreven: data tegen golden, `repoEntries`, pagina-tekst |
| `docs/site/map.js` | `REPO_ZONES`, `REPO_OWNERS`, `REPO_READER_NAMES`, `repoReader` en oude `repoEntries`/`renderRepo` eruit; nieuwe `REPO_MODES`, `repoEntries(data, mode)`, `renderRepo(root, data)` |
| `docs/site/map.css` | blok *The floor plan of a repo after init* en `.swatch.owner-*`/`.swatch.touched` eruit; blok *The tree of a repo after init* erin |
| `docs/site/repo.html`, `docs/site/repo.js` | legenda en `<aside id="detail">` eruit, korte `why`, tekst onder de kaart |
| `changelog.d/issue-101-plattegrond-na-init.md` → `changelog.d/issue-101-repokaart-boom.md` | zie taak 3 |

---

### Task 1: De data en de toetsen tegen de golden

**Files:**
- Modify: `docs/site/data/repo.json` (volledig herschreven)
- Modify: `tests/test_site_repo.py` (volledig herschreven; `test_named_token`, readers, `--force`, legenda en zones verdwijnen)

**Interfaces:**
- Produces: `repo.json` met
  - `title`: `"Your repo after init"`
  - `subtitle`: `"Everything lighthouse init puts in your repo, and the files of yours it edits."`
  - `dirs`: in deze volgorde `{id, label, yours?}`:
    `root` (`"your repo/"`), `.claude` (`".claude/"`, yours `"your own skills, agents and settings"`), `.agents` (`".agents/"`, yours `"your own skills"`), `.lighthouse` (`".lighthouse/"`), `.github` (`".github/"`, yours `"your own workflows"`)
  - `rows`: `{id, place?, label, dir, kind, act, href?}`; `kind` is een sleutel van `SORTS`; `act` ∈ `adds`|`edits`
  - `yours`: `"your code, your tests, your config, …"`
  - `workstation`: `{label: "On your workstation", note: "Only with Tailscale. It is not in the repo, so a teammate or another machine does not have it."}`

  | id | place | label | dir | kind | act | href |
  |---|---|---|---|---|---|---|
  | agents-md | `AGENTS.md` | `AGENTS.md` | root | rules | edits | `system.html#rules` |
  | lighthouse-toml | `lighthouse.toml` | `lighthouse.toml` | root | config | edits | `config.html` |
  | claude-hooks | `.claude/hooks/lighthouse/` | `hooks/lighthouse/` | .claude | hook | adds | `reference.html#hooks` |
  | claude-skills | `.claude/skills/lighthouse-*/` | `skills/lighthouse-*/` | .claude | standard-skill | adds | `reference.html#skills` |
  | claude-agents | `.claude/agents/lighthouse-*.md` | `agents/lighthouse-*.md` | .claude | agent | adds | `reference.html#agents` |
  | claude-settings | `.claude/settings.json` | `settings.json` | .claude | config | edits | — |
  | agents-skills | `.agents/skills/lighthouse-*/` | `skills/lighthouse-*/` | .agents | standard-skill | adds | `reference.html#skills` |
  | lighthouse-lib | `.lighthouse/lib/lighthouse_lib/` | `lib/` | .lighthouse | config | adds | — |
  | lighthouse-scripts | `.lighthouse/scripts/` | `scripts/` | .lighthouse | config | adds | — |
  | headings | `.lighthouse/headings.toml` | `headings.toml` | .lighthouse | config | adds | — |
  | manifest | `.lighthouse/manifest.toml` | `manifest.toml` | .lighthouse | config | adds | — |
  | pr-template | `.github/pull_request_template.md` | `pull_request_template.md` | .github | github | adds | `reference.html#github` |
  | workflows | `.github/workflows/lighthouse-*.yml` | `workflows/lighthouse-*.yml` | .github | github | adds | `reference.html#github` |
  | host-toml | — | `~/.config/lighthouse/host.toml` | workstation | config | adds | `config.html` |

- Produces in `tests/test_site_repo.py` (taak 2 gebruikt ze): `GOLDEN`, `parse_golden(text) -> dict[str, tuple[set[str], list[str]]]`, `golden()`, `mutated() -> dict` (deepcopy van `repo.json`), `row(data, ident) -> dict`.

- [ ] **Step 1: Schrijf de toetsen.** Hergebruik `parse_golden`, `golden`, `room_of`, `listed` uit het huidige bestand. Nieuwe constanten:

```python
DIRS = ["root", ".claude", ".agents", ".lighthouse", ".github"]
REQUIRED = "id label dir kind act".split()
ACTS = {"adds", "edits"}
OFF_THE_MAP = {"CLAUDE.md": "init writes it only when AGENTS.md and CLAUDE.md are both missing, as one line @AGENTS.md, and never touches one that exists"}
INIT_ACTS = {"lighthouse.toml": "edits", ".lighthouse/manifest.toml": "adds"}
EDIT_KINDS = {"block", "hook", "permission"}
SEPARATORS = ("·", ",")
```

Functies, elk `-> list[str]`, gesorteerd:
- `place_problems(data, gold)`: `not on the map: <key>` voor elke golden-sleutel buiten `OFF_THE_MAP` zonder rij; `not in places.golden: <place>`; `<place>: on <n> rows`; `<key>: off the map, yet a row` voor een rij met een place in `OFF_THE_MAP`; `<id>: no place outside the workstation`; `expected one row without a place, found <n>`.
- `act_problems(data, gold)`: per rij met place in gold: `edits` als `kinds & EDIT_KINDS`, `adds` als `kinds == {"file"}`, anders `INIT_ACTS[place]`; melding `<id>: act <act>, expected <want>`.
- `shape_problems(data, sorts)`: dirs-ids ≠ `DIRS` → `dirs are [...], not [...]`; `<id>: lacks <k>`; `duplicate id <id>`; `<id>: unknown kind <k>` (niet in `sorts`); `<id>: unknown act <a>`; `<id>: unknown dir <d>` (niet in `DIRS` of `workstation`); `<id>: dir <d>, place says <room_of(place)>`.
- `link_problems(data)`: als nu, maar alleen voor rijen met `href`; meldingen `<id>: <page> points nowhere` en `<id>: <page> has no id <anchor>`.
- `label_problems(data)`: `<id>: label <label> holds two things` als een `SEPARATORS`-teken in `label` staat.

`sorts` komt uit node: `SORT_KEYS = run_map("Object.keys(m.SORTS)")` in een fixture of helper `sort_keys()`.

Tests:
- `test_the_map_matches_the_places_init_writes`: `place_problems(load("repo.json"), golden()) == []`; idem voor `act_problems`, `shape_problems`, `link_problems`, `label_problems` (elk een eigen test).
- `test_problems_flag_a_missing_row`: rij `claude-agents` weg → `["not on the map: .claude/agents/lighthouse-*.md"]`.
- `test_problems_flag_a_new_place_in_the_golden`: gold + `.claude/commands/lighthouse-*/` → `["not on the map: .claude/commands/lighthouse-*/"]`.
- `test_problems_flag_a_place_init_does_not_write`: `headings.place = ".lighthouse/cache/"` → `["not in places.golden: .lighthouse/cache/", "not on the map: .lighthouse/headings.toml"]`.
- `test_problems_flag_claude_md_on_the_map`: rij `{id: "claude-md", place: "CLAUDE.md", label: "CLAUDE.md", dir: "root", kind: "rules", act: "adds"}` toegevoegd → `["CLAUDE.md: off the map, yet a row"]`.
- `test_problems_flag_a_second_row_without_place`: `del manifest.place` → `["expected one row without a place, found 2", "manifest: no place outside the workstation", "not on the map: .lighthouse/manifest.toml"]`.
- `test_problems_flag_a_wrong_act`: `claude-settings.act = "adds"` → `["claude-settings: act adds, expected edits"]`; `manifest.act = "edits"` → `["manifest: act edits, expected adds"]`.
- `test_problems_flag_an_unknown_kind_and_dir`: `headings.kind = "skill"`, `headings.dir = ".claude"` → bevat `"headings: unknown kind skill"` en `"headings: dir .claude, place says .lighthouse"`; `host-toml.dir = "home"` → `"host-toml: unknown dir home"`.
- `test_problems_flag_an_anchor_that_does_not_exist`: `claude-skills.href = "reference.html#nope"` → `["claude-skills: reference.html has no id nope"]`; `agents-md.href = "system.html#nope"` → `["agents-md: system.html has no id nope"]`.
- `test_problems_flag_two_things_in_one_row`: `lighthouse-lib.label = "lib/ · scripts/"` → `["lighthouse-lib: label lib/ · scripts/ holds two things"]`.

- [ ] **Step 2: Draai, verwacht rood.** `python3 -m pytest tests/test_site_repo.py -q` → FAIL (`KeyError: 'dirs'`).

- [ ] **Step 3: Herschrijf `docs/site/data/repo.json`** volgens de tabel hierboven (2 spaties inspringing, zoals nu).

- [ ] **Step 4: Draai, verwacht groen.** `python3 -m pytest tests/test_site_repo.py -q` → alle tests PASS. De pagina is nu stuk tot taak 2/3; commit toch, de test is groen.

  *Let op:* de oude `renderRepo` leest `rooms`; dat breekt alleen de browser, geen test. Taak 2 lost het op.

- [ ] **Step 5: Mutatiecheck.** Zet tijdelijk `claude-hooks.act` op `edits` en verwijder rij `workflows`: `act_problems` en `place_problems` moeten rood worden. Terugzetten.

- [ ] **Step 6: Commit.** `git add docs/site/data/repo.json tests/test_site_repo.py && git commit -m "feat(site): repokaart-data als boom met adds/edits per rij (#101)"`

---

### Task 2: `repoEntries` en `renderRepo`

**Files:**
- Modify: `docs/site/map.js:536-668` (het repo-blok)
- Modify: `tests/test_site_repo.py` (node-tests erbij)

**Interfaces:**
- Consumes: `repo.json` uit taak 1; `SORTS`, `KIND_OF_SORT`, `el` uit `map.js`.
- Produces:
  - `export const REPO_MODES = [{id: "everything", label: "Everything"}, {id: "adds", label: "Adds"}, {id: "edits", label: "Edits your file"}]`
  - `export function repoEntries(data, mode = "everything")` → array, mappen in volgorde van `dirs`, daarna het werkstation:
    `[{id, label, yours, rows: [Row]}, …, {id: "workstation", label, note, rows: [Row]}]`.
    `yours` van `root` is `data.yours`; van een andere map zijn `yours` of `null`.
    `Row = {id, place, label, kind, glyph, className, act, actLabel, href, dim}`, met
    `glyph = SORTS[kind].glyph`, `className = "kind-" + (KIND_OF_SORT[kind] || kind) + " form-" + kind`,
    `actLabel = act === "adds" ? "init adds it" : "init edits your file"`, `place` en `href` `null` als ze ontbreken,
    `dim = mode !== "everything" && act !== mode`.
    Gooit `Error("unknown mode: <mode>")` en `Error("unknown kind: <kind>")`.
  - `export function renderRepo(root, data)` (geen `detail` meer).

- [ ] **Step 1: Schrijf de node-tests** (helper `entries(mode)` = `run_map(f"m.repoEntries({json.dumps(load('repo.json'))}, {json.dumps(mode)})")`; rijen vinden op `place`, of `label` voor het werkstation):
- `test_entries_keep_the_order_of_the_tree`: `[e["id"] for e in entries("everything")] == ["root", ".claude", ".agents", ".lighthouse", ".github", "workstation"]`; `root.yours == "your code, your tests, your config, …"`; `.lighthouse.yours is None`.
- `test_everything_dims_nothing`: geen rij met `dim`.
- `test_edits_leaves_three`: niet-gedimde rijen in `edits` hebben places `{"AGENTS.md", "lighthouse.toml", ".claude/settings.json"}`; de host-rij is gedimd.
- `test_adds_leaves_what_init_adds`: niet-gedimde rijen in `adds` == alle rijen uit `repo.json` met `act == "adds"` (op `id`), host-rij inbegrepen.
- `test_rows_carry_glyph_and_colour`: rij `.claude/agents/lighthouse-*.md` heeft `glyph == "◆"`, `className == "kind-lighthouse form-agent"`; rij `AGENTS.md` heeft `glyph == "§"`, `className == "kind-rules form-rules"`.
- `test_rows_say_what_init_does`: rij `.claude/settings.json` `actLabel == "init edits your file"`; rij `.lighthouse/scripts/` `actLabel == "init adds it"`.
- `test_entries_refuse_an_unknown_mode`: `run_map("(() => { try { m.repoEntries(<data>, 'upgrade'); return null; } catch (e) { return e.message; } })()") == "unknown mode: upgrade"`.
- `test_entries_refuse_an_unknown_kind`: idem met `headings.kind = "skill"` → `"unknown kind: skill"`.
- `test_links_have_an_accessible_name`: grep op `map.js` binnen `renderRepo`: `'aria-label'` en `` `More about ${`` staan erin. *(Geen DOM in node; deze toets houdt alleen vast dat de naam gezet wordt.)*

- [ ] **Step 2: Draai, verwacht rood.** `python3 -m pytest tests/test_site_repo.py -q` → de nieuwe tests FAIL (oude `repoEntries` leest `rooms`).

- [ ] **Step 3: Vervang het repo-blok in `map.js`.** Weg: `REPO_ZONES`, `REPO_OWNERS`, `REPO_READER_NAMES`, `repoReader`, de oude `repoEntries` en `renderRepo`. Daarna controleren of `drawerFor`, `box` en `facts` nog door de flowmap gebruikt worden (ja: laten staan). Nieuw: `REPO_MODES`, `repoEntries`, `renderRepo`.

  `renderRepo(root, data)` bouwt:
  - een schakelaar `div.routes.repo-switch` met `role="group"`, `aria-label="What init does"`, per modus een `button` met `data-mode` en `aria-pressed` (`everything` begint op `true`); een klik zet `aria-pressed` en `classList.toggle("dim", row.dim)` op elke rij uit `repoEntries(data, mode)` (rijen op `data-id` terugvinden);
  - `div.repo-tree` met `h2` = label van `root`, dan `ul`: de rijen van `root`, dan per andere map een `li` met `span.repo-dir` (label) en een eigen `ul` met de rijen en, als `yours`, een `li` met `span.repo-yours`; als laatste `li` van de buiten-`ul` `span.repo-yours` met `root.yours`;
  - `section.repo-station` met `h3` = label, de rij(en) in een `ul`, en `p.repo-note` = `note`.
  - Een rij: `li` > `span.repo-row <className>` met `data-id`, daarin `span.glyph` (`aria-hidden="true"`), `code` met label, `span.sr-only` `, <actLabel>`, en als `href` een `a.repo-go` met tekst `↗`, `aria-label` `More about <label>`, `href`.

- [ ] **Step 4: Draai, verwacht groen.** `python3 -m pytest tests/test_site_repo.py tests/test_site_pages.py -q` → PASS.

- [ ] **Step 5: Mutatiecheck.** Zet tijdelijk `dim = act !== mode` (zonder de `everything`-uitzondering): `test_everything_dims_nothing` moet rood. Terugzetten.

- [ ] **Step 6: Commit.** `git add docs/site/map.js tests/test_site_repo.py && git commit -m "feat(site): repokaart als boom met schakelaar adds/edits (#101)"`

---

### Task 3: Pagina, opmaak, tekst onder de kaart, changelog

**Files:**
- Modify: `docs/site/repo.html`, `docs/site/repo.js`, `docs/site/map.css:1493-1605` (repo-blok) en de `.swatch.owner-*`/`.swatch.touched`-selectors
- Modify: `tests/test_site_repo.py` (paginatoets)
- Delete: `changelog.d/issue-101-plattegrond-na-init.md`; Create: `changelog.d/issue-101-repokaart-boom.md`

**Interfaces:**
- Consumes: `renderRepo(root, data)` uit taak 2.

**Changelog.** *(Afwijking van spec §6, keuze van Claude.)* PR #188 is na v0.4.1 gemerged, dus zijn fragment is nog niet uitgebracht. Een `changed` naast dat `added` zou in één release een plattegrond aankondigen die er niet meer is. Het nieuwe fragment vervangt het oude, `type: added`.

- [ ] **Step 1: Schrijf de paginatoets** `test_page_has_no_legend_or_panel_and_explains_below`: `repo.html` bevat geen `class="legend"` en geen `id="detail"`; de sectie `id="repo-notes"` (onder `#repo-map`) noemt `lib/`, `scripts/`, `headings.toml`, `manifest.toml`, `settings.json`, `permissions.allow`, `host.toml` en `--yes`.

- [ ] **Step 2: Draai, verwacht rood.** `python3 -m pytest tests/test_site_repo.py::test_page_has_no_legend_or_panel_and_explains_below -q` → FAIL.

- [ ] **Step 3: Pagina.** In `repo.html`: ondertitel als `repo.json`; `why` naar één alinea (de boom is je repo; gekleurd is wat `init` schrijft; de schakelaar toont wat hij toevoegt en wat hij in een bestand van jou aanpast; ↗ gaat naar de uitleg); legenda en `aside#detail` weg; onder `#repo-map` een `section id="repo-notes" class="repo-notes"` met drie alinea's (`.lighthouse/`, `settings.json`, `host.toml`). Controleer elke zin tegen `buildPlan`/`planConfig` (`internal/initcmd/initcmd.go:294`, `:505`), `offerHost` (`internal/initcmd/host.go:43`) en de lezers in `internal/hostconfig`, en tegen wat `pack/core/lib/`, `pack/*/scripts/` en `.lighthouse/manifest.toml` werkelijk zijn. In `repo.js`: `renderRepo(document.getElementById("repo-map"), repo)`, commentaar "the tree of a repo after init".

- [ ] **Step 4: CSS.** Verwijder het blok *The floor plan of a repo after init* en `.swatch.owner-*`, `.swatch.touched`. Nieuw blok *The tree of a repo after init*, naar mockup A v2: `.repo-tree` (rand `3px solid var(--badge-bg)`, radius 12px, `background: var(--panel)`), geneste `ul` zonder bullets met `border-left: 2px solid var(--border)` en een horizontaal streepje per `li::before`; `.repo-row` inline-flex, `background: var(--kind)`, `color: var(--kind-fg)`, radius 6px, `max-width: 100%`, `code { overflow-wrap: anywhere }`; `.repo-row.dim { opacity: 0.25 }`; `.repo-yours` (rand `1.5px solid var(--border)`, `color: var(--muted)`, geen achtergrondkleur); `.repo-dir` vet; `.repo-go` (kleur erven, `:focus-visible` outline `3px solid var(--focus)`); `.repo-station` (rand `3px solid var(--session)`, radius 12px); `.repo-note`, `.repo-notes` `color: var(--muted)`. Bij `max-width: 720px` kleinere inspringing. Geen nieuwe kleurwaarden.

- [ ] **Step 5: Versie.** `python3 -m tests.site_version`.

- [ ] **Step 6: Changelog.** `git rm changelog.d/issue-101-plattegrond-na-init.md`; schrijf `changelog.d/issue-101-repokaart-boom.md`:

```markdown
---
type: added
---
A map of your repo after `lighthouse init` in `docs/site/repo.html` shows, as a tree, every place `init` writes, with a link to where each is explained. A switch shows what `init` adds and which of your files it edits. A test fails when `init` writes to a place that is not on the map.
```

- [ ] **Step 7: Verouderde woorden.** `grep -rn "floor plan\|whose it is\|--upgrade" docs/site/index.html docs/site/data/system.json docs/getting-started.md docs/site/repo.html docs/site/map.js docs/site/map.css` → geen treffer over de repokaart (een treffer over iets anders blijft).

- [ ] **Step 8: Volledige suite.** `python3 -m pytest tests -q` → PASS. `python3 -m tests.site_version` opnieuw → geen wijziging.

- [ ] **Step 9: Browsercheck** (alleen met browser): `python3 -m http.server --directory docs/site`, `repo.html` op 360px en breed, licht en donker, alle drie standen, Tab door schakelaar en ↗-links. Zonder browser: in de PR melden als niet uitgevoerd.

- [ ] **Step 10: Commit.** `git add -A docs/site tests/test_site_repo.py changelog.d && git commit -m "feat(site): repokaart-pagina zonder legenda, met uitleg onder de boom (#101)"`
