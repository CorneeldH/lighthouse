# Systeemkaart en roadmap (K4) — implementatieplan

> **Voor agents:** VERPLICHTE SUB-SKILL: gebruik superpowers:subagent-driven-development (aanbevolen) of superpowers:executing-plans om dit plan taak voor taak uit te voeren. Stappen gebruiken checkboxen (`- [ ]`).

**Doel:** Een pagina `docs/site/system.html` met twee standen, *Now* (een plattegrond van de
onderdelen als schillen) en *Over time* (de issues per milestone), uit één momentopname
`docs/site/data/system.json`, met Python-tests die falen als de data niet klopt.

**Architectuur:** De data staat met de hand in `system.json` (uit `gh api` op het moment van
bouwen). `map.js` krijgt een gedeelde lade (`drawerFor`), die `renderFlowMap` voortaan ook
gebruikt, en twee nieuwe weergaven over dezelfde data: `renderPlanMap` (*Now*, geneste HTML-vakken
in een CSS-grid met een SVG-laag voor twee lijnen) en `renderTimeline` (*Over time*, groepen per
milestone). `system.js` laadt de data, tekent beide standen en wisselt ertussen.

**Tech stack:** HTML, CSS, vanilla JS (ES-modules) en SVG; pytest (`json`, `datetime`, `copy`).

**Spec:** [2026-10-31-systeemkaart-design.md](../specs/2026-10-31-systeemkaart-design.md), met
[het kaartenontwerp](../specs/2026-10-27-kaarten-design.md) §3, §7 en §8 ernaast. Issue #102.
K3 (#101) valt buiten dit plan; hij hergebruikt later `renderPlanMap`.

## Globale randvoorwaarden

- Alle tekst op de site is Engels. Plan, commits en PR-body zijn Nederlands. Code, testnamen en
  commentaar zijn Engels, ook in `docs/site/`.
- Geen framework, geen buildstap, geen externe bibliotheek. `<html lang="en">`. Data alleen via
  `textContent` en attributen: geen `innerHTML`, `outerHTML`, `insertAdjacentHTML` of
  `document.write` (`test_component_never_writes_html_from_data`).
- `system.json` heeft **geen** sleutel `chips` (spec §5), anders pikt `data_maps()` in
  `tests/test_site_chain.py` hem op.
- De plattegrond en de tijdlijn gebruiken **niet** de klassen `.map` en `.chip`. Ze delen met de
  stroomkaart alleen de kleurtokens (`--lighthouse`, `--idea`, `--border`, …) en de `kind-*`- en
  `status-*`-klassen (spec §6, *Eigen klassen*).
- Nergens op de pagina staat `M<n>`. Een milestone heet naar zijn `title` uit de data:
  `Pakket`, `Romp en zicht`, `Dispatcher`, `Tweede omgeving`, `SDP-tenant als werkplek`,
  `Product: eerste versie voor anderen`, `Kaarten: lighthouse in beeld`. `plannedMark` blijft
  zoals hij is.
- Statussen letterlijk `works`, `planned`, `idea`; het label letterlijk `not in first release`.
- Een `source` is `https://github.com/CorneeldH/lighthouse/blob/main/<pad>` (`REPO_BLOB` in
  `tests/test_site_chain.py`). Een issuelink is `https://github.com/CorneeldH/lighthouse/issues/<n>`.
- Geen roadmapschakelaar ("Show roadmap") op deze pagina (spec §6).
- Tests zijn Python, in het patroon van `tests/test_site_chain.py`. **Geen DOM- of browsertest in
  de testsuite** (r1-f2 verworpen; kaartenontwerp §7). De headless check in taak 6 is een
  wegwerpscript in de scratchpad en wordt niet gecommit.
- Dit raakt `pack/` en `internal/` niet: geen `init --upgrade`.
- Testgate: `[test_gate] command` uit `lighthouse.toml`; die draait ook `gate_node.py`, dus eerst
  `cd web && npm ci` in de worktree. Gericht: `uv run pytest -q tests/test_site_*.py`.
- Commits als Conventional Commits in het Nederlands, eindigend op
  `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.

## Voorwaarde: goedkeuring van de schets

AC 1 van #102: "Corneel keurt die met een comment in dit issue, en pas daarna wordt hij gebouwd."

- [ ] **Stap 1:** `gh issue view 102 --comments`. Staat er geen comment van Corneel die de schets of
  de spec goedkeurt, stop dan hier en meld dat. Bouw niets.
- [ ] **Stap 2:** Met de goedkeuring: pas de AC van #102 aan met `/lighthouse-write-issue` (spec §3,
  *Afwijking van de issue*). In *Now* zijn de onderdelen de chips; de milestones liggen als groepen
  in *Over time*, niet als gebieden op de kaart.

## Review-focus

1. **De pagina via `file://` openen.** `fetch` faalt; de melding met het servercommando staat
   zichtbaar in de statische HTML en `system.js` verbergt haar pas na een geslaagde `loadJson`.
   Test in taak 4: `test_pages_name_the_local_server_command` over `system.html`.
2. **Lijnen meten terwijl *Now* verborgen is.** Dan zijn alle vakken 0×0 en eindigen de lijnen in
   de hoek. `renderPlanMap` meet niet zolang *Now* verborgen is en meet opnieuw zodra *Now*
   zichtbaar wordt. Headless check in taak 6: wissel naar *Over time*, verander de breedte, wissel
   terug, en de uiteinden liggen op hun vakken.
3. **Een smal scherm.** Het grid vouwt tot één kolom, de lijnen volgen de nieuwe plek van de vakken
   en de pagina scrolt niet horizontaal. Headless check in taak 6 op 390px.
4. **Eén lade voor twee standen.** Een open lade bij een onderdeel in *Now*, dan een wissel naar
   *Over time*: de lade sluit, en er is nooit meer dan één vak met `.current`. Escape geeft de
   focus terug aan het vak dat de lade opende. Headless check in taak 6.
5. **Een schil telt zijn onderdelen mee.** De lade van `binary` en het filter op `repo` tonen ook
   de issues van de onderdelen erin; `environments` heeft geen knop en geen issues. Test in taak 1:
   `test_fixture_issue_on_a_group_is_flagged`; headless check in taak 6.

---

### Taak 1: Datachecks op een fixture

**Files:**
- Create: `tests/fixtures/site/system.json`
- Create: `tests/test_site_system.py`

**Interfaces:**
- Consumes: `ROOT`, `SITE`, `REPO_BLOB` uit `tests/test_site_chain.py`.
- Produces: het formaat van `system.json` (spec §5, plus het veld `"group": true` voor een
  groepsonderdeel zonder knop en zonder issues, zoals `environments`), en in
  `tests/test_site_system.py`:
  - `SYSTEMS: list` — pytest-params; taak 2 voegt `docs/site/data/system.json` toe;
  - `load_system(path: Path) -> dict`;
  - `word_problems(system: dict) -> list[str]`, `reference_problems(system: dict) -> list[str]`,
    `uniqueness_problems(system: dict) -> list[str]`, `anchor_problems(system: dict) -> list[str]`,
    `part_source_problems(system: dict) -> list[str]`, `title_problems(system: dict) -> list[str]`,
    `snapshot_problems(system: dict) -> list[str]`.

- [ ] **Stap 1: Schrijf de fixture `tests/fixtures/site/system.json`**

```json
{
  "title": "Fixture system",
  "subtitle": "A small system map in the system.json format, for the checks.",
  "snapshot": "2026-10-31",
  "parts": [
    {"id": "shell", "label": "Shell", "status": "works", "what": "A shell.", "when": "Always.",
     "without": "Nothing holds the parts.",
     "source": "https://github.com/CorneeldH/lighthouse/blob/main/pack"},
    {"id": "leaf", "label": "Leaf", "parent": "shell", "status": "works", "what": "A part.",
     "when": "Always.", "without": "Nothing.",
     "source": "https://github.com/CorneeldH/lighthouse/blob/main/pack/core/skills"},
    {"id": "planned-leaf", "label": "Planned leaf", "parent": "shell", "status": "planned",
     "milestone": 2, "not_in_first_release": true, "what": "A part to come.", "when": "Later.",
     "without": "Nothing yet."},
    {"id": "envs", "label": "Envs", "status": "works", "group": true},
    {"id": "env-a", "label": "Env A", "parent": "envs", "status": "works", "what": "A place.",
     "when": "Always.", "without": "Nowhere to run."}
  ],
  "links": [{"from": "leaf", "to": "planned-leaf", "label": "starts"}],
  "milestones": [
    {"number": 1, "title": "One", "state": "closed"},
    {"number": 2, "title": "Two", "state": "open"}
  ],
  "issues": [
    {"issue": 10, "title": "A done issue", "milestone": 1, "part": "leaf", "status": "works"},
    {"issue": 11, "title": "An open issue", "milestone": 2, "part": "planned-leaf", "status": "planned"}
  ]
}
```

- [ ] **Stap 2: Schrijf de falende tests**

`SYSTEMS = [pytest.param(FIXTURE, id="fixture")]`. Eén geparametriseerde test per check, die
`== []` assert: `test_parts_use_the_fixed_words`, `test_every_reference_exists`,
`test_ids_are_unique_and_parents_acyclic`, `test_planned_has_an_issue_or_milestone`,
`test_every_source_exists`, `test_milestone_titles_carry_no_number`, `test_snapshot_is_an_iso_date`.

Mutatietests op een `copy.deepcopy` van de fixture, elk met een exacte lijst:

```python
def test_fixture_planned_part_without_milestone_is_flagged():
    # del "milestone" of planned-leaf
    assert anchor_problems(system) == ["planned-leaf: planned without issue or milestone"]

def test_fixture_unknown_parent_is_flagged():
    # leaf.parent = "ghost"
    assert reference_problems(system) == ["part leaf: unknown parent ghost"]

def test_fixture_issue_without_milestone_is_flagged():
    # del issues[0]["milestone"]
    assert reference_problems(system) == ["issue 10: no milestone"]

def test_fixture_issue_on_unknown_milestone_is_flagged():
    # issues[0]["milestone"] = 9
    assert reference_problems(system) == ["issue 10: unknown milestone 9"]

def test_fixture_part_on_unknown_milestone_is_flagged():
    # planned-leaf.milestone = 9
    assert reference_problems(system) == ["part planned-leaf: unknown milestone 9"]

def test_fixture_issue_on_a_group_is_flagged():
    # issues[0]["part"] = "envs"
    assert reference_problems(system) == ["issue 10: hangs on group envs"]

def test_fixture_link_to_unknown_part_is_flagged():
    # links[0]["to"] = "ghost"
    assert reference_problems(system) == ["link leaf->ghost: unknown part ghost"]

def test_fixture_duplicate_id_is_flagged():
    # append a copy of the env-a part
    assert uniqueness_problems(system) == ["part env-a: duplicate id"]

def test_fixture_duplicate_issue_is_flagged():
    # append a copy of issues[0]
    assert uniqueness_problems(system) == ["issue 10: duplicate issue"]

def test_fixture_duplicate_milestone_is_flagged():
    # append a copy of milestones[0]
    assert uniqueness_problems(system) == ["milestone 1: duplicate number"]

def test_fixture_parent_cycle_is_flagged():
    # shell.parent = "leaf"
    assert uniqueness_problems(system) == [
        "part shell: parent cycle", "part leaf: parent cycle", "part planned-leaf: parent cycle",
    ]

def test_fixture_idea_issue_is_flagged():
    # issues[1]["status"] = "idea"
    assert word_problems(system) == ["issue 11: an issue is never idea"]

def test_fixture_unknown_status_is_flagged():
    # leaf.status = "done"
    assert word_problems(system) == ["leaf: unknown status done"]

def test_fixture_part_without_what_is_flagged():
    # del leaf["what"]; a group needs no what
    assert word_problems(system) == ["leaf: lacks 'what'"]

def test_fixture_source_pointing_nowhere_is_flagged():
    # leaf.source = REPO_BLOB + "pack/core/skills/no-such-skill"
    assert part_source_problems(system) == [f"leaf: {REPO_BLOB}pack/core/skills/no-such-skill does not exist"]

def test_fixture_numbered_milestone_title_is_flagged():
    # milestones[0]["title"] = "M0 — One"; milestones[1]["title"] = "Two - DONE"
    assert title_problems(system) == ["milestone 1: title carries 'M0 — '", "milestone 2: title carries ' - DONE'"]

def test_fixture_snapshot_not_iso_is_flagged():
    # snapshot = "31-10-2026"
    assert snapshot_problems(system) == ["snapshot is not an ISO date: 31-10-2026"]
```

- [ ] **Stap 3:** `uv run pytest -q tests/test_site_system.py`. Verwacht: FAIL, de checks bestaan
  nog niet (`NameError`).

- [ ] **Stap 4: Implementeer de checks in `tests/test_site_system.py`**

Elke check geeft een lijst van berichten in de volgorde van de data, met precies de teksten van
stap 2:

- `word_problems`: elk onderdeel heeft `id`, `label`, `status`; een onderdeel zonder
  `"group": true` ook `what`, `when`, `without` (`"{id}: lacks '{key}'"`). Elk issue heeft
  `issue`, `title`, `part`, `status`; een lijn `from`, `to`, `label`. `status` in
  `{"works", "planned", "idea"}` (`"{id}: unknown status {s}"`, voor een issue
  `"issue {n}: unknown status {s}"`); een issue met `idea` geeft
  `"issue {n}: an issue is never idea"`.
- `reference_problems`: `parent` bestaat; `milestone` van onderdeel en issue bestaat; elk issue
  heeft een `milestone`; `part` bestaat en is geen groep; `from` en `to` van een lijn bestaan.
- `uniqueness_problems`: dubbele `parts.id`, `milestones.number`, `issues.issue`; daarna één
  `"part {id}: parent cycle"` per onderdeel waarvan de `parent`-keten een id voor de tweede keer
  bezoekt. Een `parent` die niet bestaat beëindigt de keten zonder bericht: die meldt
  `reference_problems`.
- `anchor_problems`: een onderdeel of issue met `planned` zonder `issue` en zonder `milestone`.
- `part_source_problems`: een `source` onder `REPO_BLOB` wijst naar een pad dat bestaat; een
  `source` daarbuiten geeft `"{id}: source is not under REPO_BLOB"`.
- `title_problems`: een titel die met `re.match(r"M\d+ — ", title)` begint, of op `" - DONE"`
  eindigt; het bericht noemt het gevonden stuk tussen enkele aanhalingstekens.
- `snapshot_problems`: `datetime.date.fromisoformat(snapshot)` faalt (`ValueError` of
  `TypeError`).

- [ ] **Stap 5:** `uv run pytest -q tests/test_site_system.py tests/test_site_chain.py`. Verwacht:
  PASS. `test_site_chain.py` blijft groen: de fixture heeft geen `chips`.

- [ ] **Stap 6: Commit**

```bash
git add tests/fixtures/site/system.json tests/test_site_system.py
git commit -m "test(site): datachecks voor de systeemkaart (#102)"
```

### Taak 2: De momentopname `system.json`

**Files:**
- Create: `docs/site/data/system.json`
- Modify: `tests/test_site_system.py`

**Interfaces:**
- Consumes: de checks en `SYSTEMS` uit taak 1.
- Produces: `docs/site/data/system.json`, die taak 4 en 5 tekenen.

- [ ] **Stap 1: Schrijf de falende tests**

Voeg `SYSTEM = SITE / "data" / "system.json"` toe, en `pytest.param(SYSTEM, id="system.json")` aan
`SYSTEMS`, en drie tests die de spec vastpinnen:

```python
PARTS = {  # id: (parent, status), in the order of spec §4
    "binary": (None, "works"), "installer": ("binary", "works"),
    "usage-meter": ("binary", "works"), "dashboard": ("binary", "works"),
    "controller": ("binary", "planned"), "packs": ("binary", "works"),
    "repo": (None, "works"), "chain": ("repo", "works"), "guardrails": ("repo", "works"),
    "environments": (None, "works"), "env-now": ("environments", "works"),
    "env-second": ("environments", "planned"), "env-sdp": ("environments", "planned"),
    "docs": (None, "works"),
}

def test_parts_are_the_fourteen_of_the_spec():
    parts = load_system(SYSTEM)["parts"]
    assert {p["id"]: (p.get("parent"), p["status"]) for p in parts} == PARTS
    assert [p["id"] for p in parts] == list(PARTS)
    assert {p["id"] for p in parts if p.get("not_in_first_release")} == {"dashboard", "controller", "env-now"}
    assert {p["id"]: p["milestone"] for p in parts if "milestone" in p} == {"controller": 3, "env-second": 4, "env-sdp": 5}
    assert {p["id"] for p in parts if p.get("group")} == {"environments"}
    sources = {p["id"]: p["source"][len(REPO_BLOB):] for p in parts if "source" in p}
    assert sources == {"binary": "cmd/lighthouse", "installer": "internal/initcmd",
                       "usage-meter": "internal/usage", "dashboard": "internal/server",
                       "controller": "internal/cli/tick.go", "packs": "pack",
                       "chain": "pack/core/skills", "guardrails": "pack/core/hooks",
                       "env-now": "deploy", "docs": "docs"}

def test_links_are_the_two_of_the_spec():
    links = load_system(SYSTEM)["links"]
    assert {(l["from"], l["to"], l["label"]) for l in links} == {
        ("installer", "repo", "installs"), ("controller", "chain", "starts sessions"),
    }

def test_milestones_in_their_order_without_numbers():
    milestones = load_system(SYSTEM)["milestones"]
    assert [(m["number"], m["title"], m["state"]) for m in milestones] == [
        (1, "Pakket", "closed"), (2, "Romp en zicht", "closed"), (3, "Dispatcher", "open"),
        (4, "Tweede omgeving", "open"), (5, "SDP-tenant als werkplek", "open"),
        (6, "Product: eerste versie voor anderen", "open"), (7, "Kaarten: lighthouse in beeld", "open"),
    ]
```

De `state` van een milestone volgt GitHub op het moment van bouwen; wijkt hij af van de lijst,
pas dan de lijst aan en noem het in de PR-body.

- [ ] **Stap 2:** `uv run pytest -q tests/test_site_system.py`. Verwacht: FAIL,
  `docs/site/data/system.json` bestaat niet.

- [ ] **Stap 3: Haal milestones en issues op**

```bash
gh api 'repos/CorneeldH/lighthouse/milestones?state=all&per_page=50' \
  --jq '.[] | {number, title, state}'
gh api --paginate 'repos/CorneeldH/lighthouse/issues?state=all&per_page=100' \
  --jq '.[] | select(.pull_request == null and .milestone != null)
        | {issue: .number, title, milestone: .milestone.number, state, state_reason}'
```

Bij het schrijven van dit plan: milestones 1–7, 47 issues met een milestone, geen enkele gesloten
als *not planned*.

- [ ] **Stap 4: Schrijf `docs/site/data/system.json`**

- `title` `"The system"`, `snapshot` de datum van stap 3, een `subtitle` van één zin.
- De veertien `parts` in de volgorde van `PARTS`, met `label` uit spec §4 (`Installer (init)`
  enzovoort), `what`/`when`/`without` in gewoon Engels (spec §9). De `what` van `controller` zegt
  dat `tick` nu alleen de repo's in `[dispatch]` controleert en nog geen issues ophaalt of sessies
  start (spec §4, bron `docs/tick.md`). `environments` krijgt `"group": true` en geen
  `what`/`when`/`without`.
- `milestones`: titel zonder `M<n> — ` en zonder ` - DONE`.
- `issues`: open is `planned`; gesloten met `state_reason` `completed` is `works`; elk ander
  gesloten issue valt weg (noem het in de PR-body). `part` volgens de regel van spec §5,
  *Welk onderdeel*. Noteer de toewijzing als tabel (issue, titel, onderdeel) voor de PR-body.

- [ ] **Stap 5:** `uv run pytest -q tests/test_site_system.py tests/test_site_chain.py`. Verwacht:
  PASS. Faalt een check op de echte data, verbeter de data, niet de check.

- [ ] **Stap 6: Commit**

```bash
git add docs/site/data/system.json tests/test_site_system.py
git commit -m "feat(site): momentopname van onderdelen, milestones en issues (#102)"
```

### Taak 3: Gedeelde lade in `map.js`

**Files:**
- Modify: `docs/site/map.js` (de closures `mark`, `close`, `open` en de `keydown`-listener in
  `renderFlowMap`)

**Interfaces:**
- Produces, geëxporteerd uit `map.js`:

  ```js
  /** The drawer on `detail`, one per element however often it is asked for. */
  export function drawerFor(detail)
  // -> { show(node: HTMLElement, boxes: Node[], focus = true, onClose = null): void,
  //      close(): void }
  ```

  `show` haalt `.current` van het vorige vak, zet het op `node`, vult `detail` met de knop
  "Close" (`aria-label` "Close details") en `boxes`, opent, en focust `detail` als `focus`.
  `close` doet niets als de lade dicht is; anders sluit hij, haalt `.current` weg, geeft de focus
  terug aan `node` en roept de `onClose` van de laatste `show` aan. Eén `keydown`-listener voor
  Escape per `detail`, bewaard in een `WeakMap`.

- [ ] **Stap 1: Implementeer `drawerFor` en laat `renderFlowMap` hem gebruiken**

`showChip` geeft `() => { reshow = null; }` mee als `onClose`; de rest van `renderFlowMap` blijft
gelijk. Geen gedragswijziging op de ketenkaart.

- [ ] **Stap 2:** `uv run pytest -q tests/test_site_*.py`. Verwacht: PASS.

- [ ] **Stap 3: Controleer de ketenkaart in de browser**

`python3 -m http.server --directory docs/site 8099` op de achtergrond; open
`http://localhost:8099/chain.html` headless (patroon van taak 6, stap 1). Klik een chip: de lade
toont zijn label en precies één chip heeft `.current`. Kies een andere route: de lade tekent
opnieuw. Escape: de lade sluit en de focus staat op de chip. Kies daarna een route: de lade blijft
dicht.

- [ ] **Stap 4: Commit**

```bash
git add docs/site/map.js
git commit -m "refactor(site): lade als gedeelde helper in map.js (#102)"
```

### Taak 4: De pagina en de stand *Now*

**Files:**
- Create: `docs/site/system.html`, `docs/site/system.js`
- Modify: `docs/site/map.js`, `docs/site/map.css`, `docs/site/index.html`
- Test: `tests/test_site_pages.py`, `tests/test_site_system.py`

**Interfaces:**
- Consumes: `drawerFor` (taak 3); `loadJson`, `initTheme` uit `map.js`; het formaat van taak 1.
- Produces, in `map.js`:
  - `export function renderPlanMap(root, data, detail)` → `{ redraw(): void }`;
  - module-intern, ook voor taak 5: `issuesOf(data, partId) -> issue[]` (de issues van het onderdeel
    en van alles eronder, in de volgorde van de data), `milestoneTitle(data, number) -> string`,
    `planStatus(item, data) -> string` (`"works"`, `"planned, Dispatcher"`, plus
    `" · not in first release"`), `showPart(drawer, part, data, node)` en
    `showIssue(drawer, issue, data, node)`.

- [ ] **Stap 1: Schrijf de falende tests**

In `tests/test_site_pages.py`:
- `test_pages_name_the_local_server_command` en `test_chain_links_back_to_index` worden
  geparametriseerd over `chain.html` en `system.html` (de tweede heet dan
  `test_map_links_back_to_index`).
- `test_index_lists_the_six_maps`: `"system.html"` staat in `relative_links(SITE / "index.html")`
  naast `"chain.html"`, en de lijst issues wordt `["#100", "#101", "#103", "#104"]`, met
  `assert "#102" not in text`.

In `tests/test_site_system.py`:

```python
def test_page_shows_the_snapshot():
    assert 'id="snapshot"' in (SITE / "system.html").read_text(encoding="utf-8")
    assert "snapshot" in (SITE / "system.js").read_text(encoding="utf-8")
```

- [ ] **Stap 2:** `uv run pytest -q tests/test_site_pages.py tests/test_site_system.py`. Verwacht:
  FAIL op de nieuwe en geparametriseerde tests.

- [ ] **Stap 3: Schrijf `system.html` en `system.js`**

`system.html` volgt `chain.html`: zelfde kop met `back-link` en `theme-toggle`, `<title>The
system</title>`, `h1#title`, `p#subtitle`, `p#snapshot`, de `serve-hint` met `system.html` in het
adres, `div#plan-map`, `div#timeline` (leeg tot taak 5), `aside#detail.detail`. Geen legenda van de
ketenkaart; wel een korte legenda: vol is *works*, grijs gearceerd is *planned*.

`system.js` volgt `chain.js`: `initTheme`, `loadJson("data/system.json")`, `title`, `subtitle`,
`snapshot` als `Snapshot of <snapshot>`, `renderPlanMap(...)`, en de `serve-hint` verbergen na
succes; dezelfde `catch`.

- [ ] **Stap 4: Implementeer `renderPlanMap` in `map.js` en de stijl in `map.css`**

- Een top-onderdeel wordt een vak in `.plan` (CSS-grid). Een onderdeel met kinderen is een
  container (`div.plan-shell`) met de kinderen erin; zonder `group` krijgt hij een kopknop
  (`button.plan-part.plan-head`) als eerste kind, broer van de knoppen van zijn onderdelen. Met
  `group` krijgt hij een kop zonder knop (`p.plan-title`). Een onderdeel zonder kinderen is een
  `button.plan-part`. Nooit een knop in een knop.
- Elke knop: klassen `status-<status>`, `data-id`, het label, bij een planned onderdeel een
  `span.plan-mark` met `milestoneTitle`, en bij `not_in_first_release` een `span.release`
  met `not in first release`. Klik: `showPart(drawerFor(detail), part, data, button)`.
- `showPart`: kicker `In <label van parent>` (leeg bij een top-onderdeel), kop `label`,
  `p.summary` `what`; facts `When it runs` (`when`), `What you miss without` (`without`),
  `Status` (`planStatus`); een box `Issues (<works>/<totaal>)` met per issue uit `issuesOf` een
  `li` met een link `#<n> <title>` naar de issuelink en de status; zonder issues
  `No issues in a milestone yet.`; daarna `definition(source)`.
- `showIssue`: kicker `Issue #<n>`, kop `title`; facts `Status`, `Milestone`
  (`milestoneTitle`), `Part` (label); een link `Open #<n> on GitHub`.
- De lijnen: een `svg.plan-lines` (`aria-hidden`) over `.plan`, één `line` plus `text` per
  `links`-item, de `text` op het midden. De lijn loopt van het midden van de zijde van `from` die
  naar `to` kijkt, naar het midden van de zijde van `to` die naar `from` kijkt: horizontale zijden
  als de horizontale afstand tussen de vakken groter is dan de verticale, anders verticale. Het vak
  van `repo` is zijn container. Is een uiteinde niet `works`, dan krijgt de lijn de klasse
  `planned` (grijs, gestreept).
- `redraw()` meet met `getBoundingClientRect` ten opzichte van `.plan` en doet niets als
  `root.offsetParent === null`. Een `ResizeObserver` op `root` roept `redraw` aan.
- `map.css`: een blok `/* System map */`. Works vol met `--lighthouse`, planned grijs gearceerd
  (`repeating-linear-gradient` op `--idea`) met een gestippelde rand, de lijnen dun (1px) in
  `--muted`, `.plan-lines line.planned` met `stroke-dasharray`. Bij `max-width: 700px` één kolom.

- [ ] **Stap 5: Zet de tegel van #102 in `index.html` om**

`<a class="tile kind-lighthouse status-works" href="system.html">` met kicker `Map 4`, dezelfde
kop en tekst, zoals de tegel van de ketenkaart.

- [ ] **Stap 6:** `uv run pytest -q tests/test_site_*.py`. Verwacht: PASS
  (`test_site_links_resolve` dekt `system.html` en `system.js` vanzelf).

- [ ] **Stap 7: Commit**

```bash
git add docs/site/system.html docs/site/system.js docs/site/map.js docs/site/map.css \
  docs/site/index.html tests/test_site_pages.py tests/test_site_system.py
git commit -m "feat(site): systeemkaart met de stand Now (#102)"
```

### Taak 5: De stand *Over time* en de schakelaar

**Files:**
- Modify: `docs/site/map.js`, `docs/site/map.css`, `docs/site/system.html`, `docs/site/system.js`

**Interfaces:**
- Consumes: `issuesOf`, `milestoneTitle`, `planStatus`, `showIssue`, `drawerFor` (taak 3 en 4);
  `renderPlanMap(...).redraw` (taak 4).
- Produces: `export function renderTimeline(root, data, detail)` in `map.js`, zonder
  returnwaarde.

- [ ] **Stap 1: Implementeer `renderTimeline`**

- Bovenaan een `label` "Show part" met een `select`: eerst `All parts` (waarde leeg), dan elk
  onderdeel zonder `group`, in de volgorde van de data.
- Per milestone in de volgorde van `data.milestones` een `section.timeline-group` met een `h2`
  (`title`), de telling `<works>/<totaal>` van zijn issues uit de data, en per issue een
  `button.plan-chip status-<status>` met `#<n>` en de titel. Klik:
  `showIssue(drawerFor(detail), issue, data, button)`.
- Een milestone zonder issues: een grijs vak `p.timeline-empty` met `no issues yet`.
- Het filter zet `dim` op elke `.plan-chip` waarvan het issue niet in `issuesOf(data, waarde)`
  zit; leeg haalt alle `dim` weg.

- [ ] **Stap 2: De schakelaar**

In `system.html` een `div.stand-switch` (`role="group"`, `aria-label="View"`) met twee knoppen
`Now` en `Over time` (`aria-pressed`), boven `#plan-map`. `#timeline` start met `hidden`. In
`system.js`: een klik sluit eerst de lade (`drawerFor(detail).close()`), zet `hidden` en
`aria-pressed`, en roept bij *Now* `plan.redraw()` aan. `renderTimeline` draait direct na
`renderPlanMap`.

- [ ] **Stap 3: Stijl in `map.css`**

`.stand-switch` naar het patroon van `.routes button[aria-pressed]`. `.timeline-group` als `.box`,
`.plan-chip` als pil met dezelfde `status-*`-kleuren als `.plan-part`, `.plan-chip.dim` met
`opacity: 0.25`, `.timeline-empty` grijs gearceerd.

- [ ] **Stap 4:** `uv run pytest -q tests/test_site_*.py`. Verwacht: PASS.

- [ ] **Stap 5: Commit**

```bash
git add docs/site/map.js docs/site/map.css docs/site/system.html docs/site/system.js
git commit -m "feat(site): stand Over time met filter op onderdeel (#102)"
```

### Taak 6: Headless check, changelog en PR

**Files:**
- Create: `changelog.d/issue-102-systeemkaart.md`
- Wegwerp, niet committen: `<scratchpad>/system-check.cjs`

- [ ] **Stap 1: Headless check (wegwerpscript)**

`cd web && npm ci` (Playwright uit `web/package.json`; chromium in `~/.cache/ms-playwright`).
Start `python3 -m http.server --directory docs/site 8099` op de achtergrond. Schrijf een
CommonJS-script naar het patroon van `web/e2e/screenshot.mjs`
(`const { chromium } = require('@playwright/test')`) en draai het met
`NODE_PATH=web/node_modules node <scratchpad>/system-check.cjs`. Het faalt (exit 1) bij de eerste
assert die niet klopt. Op `http://localhost:8099/system.html`, breedte 1280:

- `#snapshot` toont `Snapshot of <snapshot uit de data>`; de `serve-hint` is verborgen.
- *Now*: 13 knoppen `.plan-part`, geen `button button`, geen knop met `data-id="environments"`.
- De tekst van de pagina matcht nergens `/\bM\d\b/`; de knop van `controller` toont `Dispatcher`.
- Twee lijnen; de lijn "starts sessions" heeft `planned`. Elk uiteinde ligt binnen 2px op de rand
  van zijn vak (`getBoundingClientRect`).
- Klik `binary`: de lade toont de issues van `binary` én van zijn onderdelen; precies één element
  heeft `.current`. Klik *Over time*: de lade is dicht en geen element heeft `.current`.
- *Over time*: zeven groepen met de titels van `test_milestones_in_their_order_without_numbers`, in
  die volgorde; de telling van elke groep klopt met de data; "SDP-tenant als werkplek" toont
  `no issues yet`. Filter op `repo`: precies de issues van `repo`, `chain` en `guardrails` zijn
  niet gedimd.
- Klik een issue, druk Escape: de lade sluit en `document.activeElement` is die issueknop.
- Zet de breedte op 900 terwijl *Over time* zichtbaar is, wissel naar *Now*: de uiteinden liggen
  weer op hun vakken.
- Breedte 390: één kolom, `document.documentElement.scrollWidth <= 390`, de uiteinden liggen op hun
  vakken.
- Via `file://…/docs/site/system.html`: de `serve-hint` is zichtbaar.

Draai daarna dezelfde ketenkaartcontrole als taak 3, stap 3.

- [ ] **Stap 2: Changelog-fragment `changelog.d/issue-102-systeemkaart.md`**

```markdown
---
type: added
---
A system map in `docs/site/system.html` shows the parts of lighthouse, in the binary and in your repo, with a switch to the roadmap: the issues of each milestone, as a dated snapshot.
```

- [ ] **Stap 3:** Draai `[test_gate] command` uit `lighthouse.toml` in de worktree. Verwacht: groen.

- [ ] **Stap 4: Commit**

```bash
git add changelog.d/issue-102-systeemkaart.md
git commit -m "docs(changelog): systeemkaart en roadmap (#102)"
```

- [ ] **Stap 5: PR**

`/lighthouse-verify-pr` schrijft de body. Neem de toewijzingstabel van taak 2, stap 4 erin op
(spec §5), en de issues die wegvielen.
