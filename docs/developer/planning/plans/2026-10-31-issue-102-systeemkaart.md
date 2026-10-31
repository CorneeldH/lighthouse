# Systeemkaart in drie stappen (K4, #102) — implementatieplan

> **Voor agents:** VERPLICHTE SUB-SKILL: gebruik superpowers:subagent-driven-development (aanbevolen) of superpowers:executing-plans om dit plan taak voor taak uit te voeren. Stappen gebruiken checkboxen (`- [ ]`).

**Doel:** `docs/site/system.html` toont wat je hebt na installeren, in drie stappen (*Install once*,
`lighthouse init`, *Run the loop*). Elk vak linkt door. Eén tekenset per soort geldt op elke kaart,
de ketenkaart heet "The loop" en een nieuwe `reference.html` toont elk onderdeel uit `pack/`.

**Architectuur:** `map.js` krijgt één soortentabel `SORTS` (teken en naam per soort) met
`sortOf`/`glyphOf`. Die vervangt `GLYPHS` per trigger. De ketenkaart, de sessiekaart, de
systeempagina en elke legenda lezen hun tekens daar. Twee nieuwe weergaven komen erbij:
`renderStory` (de drie stappen uit `data/system.json`) en `renderReference` (de onderdelen per pack
uit `data/packs.json`). De pure delen (`SORTS`, `sortOf`, `glyphOf`, `referenceEntries`) zijn
exports. Pytest test ze via `node`, zoals `tests/test_site_download.py` dat met `platform.js`
doet. `renderPlanMap`, `renderTimeline` en de *Over time*-schakelaar uit PR #157 gaan weg.

**Tech stack:** HTML, CSS, vanilla JS (ES-modules); pytest, met `node` (v22) voor de pure functies
van `map.js`.

**Spec:** [2026-10-31-systeemkaart-design.md](../specs/2026-10-31-systeemkaart-design.md), met
[het kaartenontwerp](../specs/2026-10-27-kaarten-design.md) §3, §7 en §8 ernaast. Issue #102.
Mockup: lay-out B, versie 1 (`.superpowers/brainstorm/1529626-1793460855/content/now-layout.html`,
niet in git). Dit plan vervangt het plan voor de afgekeurde vorm (schillen plus *Over time*).

## Globale randvoorwaarden

- **Pas na PR #155.** De uitvoering start pas als PR #155 (`fix-hookkaart`, "Inside a session")
  op `main` staat en deze branch daarop is gerebaset (spec §6). Taak 0 controleert dat.
- Alle tekst op de site is Engels. Plan, commits, changelog en PR-body zijn Nederlands. Code,
  testnamen en commentaar zijn Engels.
- Geen framework, geen buildstap, geen externe bibliotheek. `<html lang="en">`. Data komt alleen
  via `textContent` en attributen op de pagina: geen `innerHTML`, `outerHTML`,
  `insertAdjacentHTML` of `document.write` (`test_component_never_writes_html_from_data`).
- `system.json` heeft geen sleutel `chips`, anders pikt `data_maps()` in
  `tests/test_site_chain.py` hem op als stroomkaart.
- De soorten en hun tekens, letterlijk (spec §5):
  `program` ⛯, `workflow-skill` ↪, `standard-skill` ✓, `agent` ◆, `hook` ⚡, `rules` §,
  `config` ⚙, `github` ⑂, `loop` ∞, `session` ❯, `you` ▣.
- Kleur volgt `kind`: `lighthouse`, `superpowers`, `hook`, `github` of `you`. Uitzonderingen per
  soort, net als `.form-agent` nu: `agent` paars, `rules` en `config` geel (nieuw token
  `--config`), `loop` en `session` donker (`--badge-bg`/`--badge-fg`).
- Niets op `system.html` noemt `serve`, `tick`, dashboard of controller. Niets toont een roadmap,
  milestones of issues. Docs en site staan er niet op.
- De pagina zegt niet dat CI alleen op GitHub kan. De tests na een commit draaien op je werkstation
  (`commit-tests`), CI draait op GitHub.
- De bestandsnaam `chain.html` blijft. "Chain" buiten de kaart (`AGENTS.md`, "chain rules", de
  ondertitel van de site) blijft buiten deze wijziging.
- Repo-URL's: `https://github.com/CorneeldH/lighthouse/blob/main/<pad>` (`REPO_BLOB`) voor een
  bestand, `https://github.com/CorneeldH/lighthouse/tree/main/<pad>` (`REPO_TREE`) voor een map.
- Geen DOM- of browsertest in de suite (kaartenontwerp §7). De headless check in taak 5 is een
  wegwerpscript in de scratchpad en wordt niet gecommit.
- Tests volgen `/lighthouse-testing`: per test de productiewijziging die hem rood maakt.
- Dit raakt `pack/` en `internal/` niet: geen `init --upgrade`.
- Testgate: `[test_gate] command` uit `lighthouse.toml`. Die draait ook `gate_node.py`, dus eerst
  `cd web && npm ci` in de worktree. Gericht: `uv run pytest -q tests/test_site_*.py`.
- Commits als Conventional Commits in het Nederlands, eindigend op
  `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.

## Review-focus

1. **Smal scherm (390 px).** De drie stappen staan onder elkaar, de pagina scrolt niet
   horizontaal, de terminalregel `lighthouse init` loopt niet over de rand. Check: taak 5, stap 2.
2. **Donker thema.** Het gele config-vak en het witte vak "You bring" moeten leesbaar zijn in
   licht en donker, ook met de themaschakelaar. Test: `test_config_token_in_every_theme` (taak 1)
   plus de headless check (taak 5).
3. **Pagina van schijf geopend.** `reference.html` haalt `packs.json` op met `fetch`; vanaf
   `file://` moet de serve-hint zichtbaar blijven. Test: `reference.html` in `MAP_PAGES` (taak 3).
4. **Een nieuw onderdeel in `pack/`.** Een skill, agent, hook of workflow die later bijkomt moet
   op de referentiepagina verschijnen, niet stil ontbreken. Test:
   `test_every_pack_part_is_on_the_reference` (taak 3).
5. **Een skill met het verkeerde teken.** Een skill die op een stroomkaart `trigger: description`
   heeft, moet op de referentiepagina ✓ dragen, en omgekeerd. Test:
   `test_standard_skills_follow_the_maps` (taak 3).

---

### Taak 0: Na PR #155, rebase en nulmeting

**Files:** geen wijziging.

- [ ] **Stap 1:** `gh pr view 155 --json state -q .state` geeft `MERGED`. Anders: stop en meld dat.
- [ ] **Stap 2:** `git fetch origin && git rebase origin/main`. Los een conflict op dit plan of de
  spec op door de versie van deze branch te houden.
- [ ] **Stap 3:** Controleer dat de staat klopt waarop dit plan bouwt:
  `docs/site/session.html`, `docs/site/session.js`, `docs/site/data/session.json` en
  `tests/test_site_session.py` bestaan; `guardrails.*` bestaat niet; `chain.json` heeft geen chip
  met `phase: "always"`; `test_every_pack_part_is_on_the_chain` in `tests/test_site_chain.py`
  verwacht `["pack/core/skills/instrument", "pack/core/skills/worktree-audit"]`; `MAP_PAGES` in
  `tests/test_site_pages.py` noemt `session.html`. Wijkt één af: stop en meld welke.
- [ ] **Stap 4:** `cd web && npm ci && cd ..`, dan `uv run pytest -q tests/test_site_*.py`.
  Verwacht: alles groen. Noteer het aantal.

---

### Taak 1: Eén soortentabel in `map.js`, op de ketenkaart en de sessiekaart

**Files:**
- Create: `tests/_site_node.py`, `tests/test_site_sorts.py`
- Modify: `docs/site/map.js` (constanten bovenaan, `glyphOf`, de kicker in `showChip`),
  `docs/site/map.css` (tokens en soortklassen), `docs/site/chain.html`, `docs/site/session.html`,
  `docs/site/chain.js`, `docs/site/session.js`

**Interfaces:**
- Produces, in `map.js`:
  - `export const SORTS: { [sort: string]: { glyph: string, label: string } }`, de elf soorten uit
    de randvoorwaarden. Labels: `program` "lighthouse, the program", `workflow-skill`
    "Workflow skill", `standard-skill` "Standard skill", `agent` "Agent", `hook` "Hook", `rules`
    "Rules", `config` "Config", `github` "GitHub", `loop` "The loop", `session` "A session",
    `you` "You bring it".
  - `export function sortOf(item) -> string`: een sleutel van `SORTS`.
  - `export function glyphOf(item) -> string`: `SORTS[sortOf(item)].glyph`, of `"?"`.
  - `export function fillLegend(list: HTMLElement) -> void`: vult `list` met één `<li>` per soort
    uit `list.dataset.sorts` (spatiegescheiden), tekst `"<glyph> <label>"`.
- Produces, in `tests/_site_node.py`: `run_map(expression: str)`, de JSON-waarde van `expression`,
  in `node` geëvalueerd met `docs/site/map.js` geïmporteerd als `m`. Faalt, zonder skip, als `node`
  ontbreekt.
- Produces, in CSS: tokens `--config`/`--config-fg` en `--you`/`--you-fg` in elk themablok;
  klassen `.kind-you`, `.form-rules`, `.form-config`, `.form-loop`, `.form-session`.

- [ ] **Stap 1: Schrijf `tests/_site_node.py`** naar het patroon van `run_node` in
  `tests/test_site_download.py`. Het script is
  `import * as m from <map.js als file-URI>; console.log(JSON.stringify(<expression>));`.

- [ ] **Stap 2: Schrijf de falende tests** in `tests/test_site_sorts.py`:

```python
SPEC_GLYPHS = {
    "program": "⛯", "workflow-skill": "↪", "standard-skill": "✓", "agent": "◆", "hook": "⚡",
    "rules": "§", "config": "⚙", "github": "⑂", "loop": "∞", "session": "❯", "you": "▣",
}

def test_every_sort_has_the_glyph_of_the_spec():
    assert run_map("Object.fromEntries(Object.entries(m.SORTS).map(([k, v]) => [k, v.glyph]))") == SPEC_GLYPHS

SKILL = REPO_BLOB + "pack/core/skills/testing"
@pytest.mark.parametrize("item, glyph", [
    ({"kind": "lighthouse", "trigger": "description", "source": SKILL}, "✓"),
    ({"kind": "lighthouse", "trigger": "you", "source": SKILL}, "↪"),
    ({"kind": "superpowers", "trigger": "skill"}, "↪"),
    ({"kind": "superpowers", "trigger": "description"}, "✓"),
    ({"kind": "lighthouse", "trigger": "skill", "source": REPO_BLOB + "pack/core/agents/issue-impact-scout.md"}, "◆"),
    ({"kind": "hook", "trigger": "event"}, "⚡"),
    ({"kind": "hook", "trigger": "tool"}, "⚡"),
    ({"kind": "github", "trigger": "github"}, "⑂"),
    ({"kind": "github", "trigger": "github", "source": REPO_BLOB + "pack/core/workflows/unblock-dependents.yml"}, "⑂"),
    ({"kind": "lighthouse", "trigger": "skill", "form": "tool"}, "▣"),
    ({"kind": "lighthouse", "trigger": "github", "form": "dispatcher"}, "⛯"),
    ({"kind": "lighthouse", "form": "rules"}, "§"),
])
def test_glyph_follows_the_sort_not_the_trigger(item, glyph):
    assert run_map(f"m.glyphOf({json.dumps(item)})") == glyph

@pytest.mark.parametrize("page", ["chain.html", "session.html"])
def test_how_it_starts_legend_is_gone(page):
    assert "How it starts" not in (SITE / page).read_text(encoding="utf-8")

def test_legends_name_known_sorts():
    # Every data-sorts on a page names only sorts of SORTS, and chain.html and session.html have one.
    ...

def test_config_token_in_every_theme():
    css = (SITE / "map.css").read_text(encoding="utf-8")
    for token in ["--config:", "--config-fg:", "--you:", "--you-fg:"]:
        assert css.count(token) == 3, token
```

`test_legends_name_known_sorts` leest elke `data-sorts="…"` uit de HTML-pagina's (regex) en
vergelijkt de woorden met `run_map("Object.keys(m.SORTS)")`. Hij eist minstens één `data-sorts`
op `chain.html` en op `session.html`.

- [ ] **Stap 3:** `uv run pytest -q tests/test_site_sorts.py`. Verwacht: FAIL (`m.SORTS` is
  undefined, "How it starts" staat er nog).

- [ ] **Stap 4: Implementeer in `map.js`.** Vervang `GLYPHS` en `AGENT_GLYPH` door `SORTS`.
  `sortOf` bouwt op de bestaande `form()`:

```js
// A form of the flow maps that is no sort of its own: a GitHub Actions workflow is GitHub, an
// external tool is something you bring, the dispatcher is the lighthouse program.
const SORT_OF_FORM = { workflow: "github", tool: "you", dispatcher: "program" };

export function sortOf(item) {
  const kind = form(item);
  if (kind === "skill") {
    return item.trigger === "description" ? "standard-skill" : "workflow-skill";
  }
  return SORT_OF_FORM[kind] || kind;
}
```

  Een `form` uit `system.json` (`rules`, `loop`, …) is al een soort en gaat zo door. De kicker
  in `showChip` toont geen triggerteken meer: `` `${phase} · ${kind} · ${TRIGGERS[…]}` ``.
  `TRIGGERS`, `STARTS` en `FORMS` blijven (title, sr-only, lade). Werk de kopcommentaar van
  `map.js` bij. Voeg `fillLegend` toe. Is een soort onbekend, dan gooit hij een `Error` met de naam.

- [ ] **Stap 5: Legenda's.** Vervang in `chain.html` en `session.html` de lijst
  `aria-label="How it starts"` door `<ul aria-label="What it is" data-sorts="…"></ul>`:
  `chain.html` krijgt `workflow-skill standard-skill agent hook github program you`, `session.html`
  krijgt `standard-skill hook`. In "Who provides it" op `chain.html` verliest de agent zijn ◆:
  `lighthouse agent`. `chain.js` en `session.js` roepen
  `fillLegend(document.querySelector("[data-sorts]"))` aan vóór `loadJson`, zodat de legenda er
  ook staat als de data niet laadt.

- [ ] **Stap 6: CSS.** Zet `--config` (licht `#f4d74b`, uit de mockup), `--config-fg`, `--you`
  (`var(--panel)`-achtig wit) en `--you-fg` in alle drie de themablokken. Kies de donkere waarden
  zelf, leesbaar op `--map-bg`. Na `.form-agent` komen `.kind-you` (met een rand in `--border`),
  `.form-rules, .form-config` en `.form-loop, .form-session`.

- [ ] **Stap 7:** `uv run pytest -q tests/test_site_sorts.py tests/test_site_chain.py tests/test_site_session.py tests/test_site_pages.py`.
  Verwacht: PASS.

- [ ] **Stap 8: Commit.** `feat(site): één tekenset per soort op de ketenkaart en de sessiekaart (#102)`

---

### Taak 2: The chain wordt The loop

**Files:**
- Modify: `docs/site/data/chain.json` (`title`, `subtitle`), `docs/site/chain.html` (`<title>`,
  `<h1>`, ondertitel, "Wait, but why"), `docs/site/session.html` (de link naar `chain.html` en
  "On the chain" in "Wait, but why"), `docs/site/index.html` (tegel Map 1),
  `docs/getting-started.md` (linktekst), `tests/test_site_pages.py`, `tests/test_site_chain.py`

- [ ] **Stap 1: Schrijf de falende tests.**

```python
# tests/test_site_chain.py
def test_the_chain_map_is_called_the_loop():
    assert load("chain.json")["title"] == "The loop"
    html = (SITE / "chain.html").read_text(encoding="utf-8")
    assert "<title>The loop</title>" in html
    assert '<h1 id="title">The loop</h1>' in html
    why = html.split('<details class="why">', 1)[1].split("</details>", 1)[0]
    assert not re.search(r"\bchain\b", why, re.IGNORECASE)

# tests/test_site_pages.py, in test_index_lists_the_six_maps
    assert "<h2>The loop</h2>" in text and "<h2>The chain</h2>" not in text

# tests/test_site_pages.py, in test_guide_links_to_the_maps
    assert "[The loop](https://corneeldh.github.io/lighthouse/chain.html)" in section
```

  Ook in `session.html` mag "Wait, but why" `chain` alleen nog in `chain.html` noemen: voeg dat
  toe aan `tests/test_site_session.py`, met dezelfde regex na het weghalen van `chain.html`.

- [ ] **Stap 2:** `uv run pytest -q tests/test_site_chain.py tests/test_site_pages.py tests/test_site_session.py`. Verwacht: FAIL op de nieuwe asserts.

- [ ] **Stap 3: Hernoem.** In `chain.json` wordt de titel `"The loop"` en de ondertitel
  `"From an idea to a closed issue, over several sessions, and what joins in at each step."`.
  Zet dezelfde ondertitel in `chain.html`. In "Wait, but why" van `chain.html` wordt "The chain is
  the connections" "The loop is the connections", en "routes through the chain" "routes through
  the loop". In `session.html` wordt `<a href="chain.html">The chain</a>` `The loop`, en "On the
  chain" "On the loop". De tegel Map 1 in `index.html` krijgt `<h2>The loop</h2>` en de nieuwe
  ondertitel. In `docs/getting-started.md` wordt de zin: "To see it as a map, open
  [The loop](https://corneeldh.github.io/lighthouse/chain.html)." De zin "The chain runs from an
  issue …" erboven blijft.

- [ ] **Stap 4:** Dezelfde pytest als stap 2. Verwacht: PASS.

- [ ] **Stap 5: Commit.** `docs(site): de ketenkaart heet The loop (#102)`

---

### Taak 3: De referentiepagina

**Files:**
- Create: `docs/site/reference.html`, `docs/site/reference.js`, `tests/test_site_reference.py`
- Modify: `docs/site/data/packs.json` (sleutel `standard_skills` per pack), `docs/site/map.js`,
  `docs/site/map.css`, `tests/test_site_packs.py`, `tests/test_site_chain.py`,
  `tests/test_site_pages.py` (`MAP_PAGES`)

**Interfaces:**
- Consumes: `SORTS`, `fillLegend` (taak 1); `run_map` (taak 1).
- Produces, in `map.js`:
  - `export function referenceEntries(packs, part) -> Array<{ pack: string, items: Array<{ name: string, sort: string, href: string }> }>`,
    met `part` één van `"skills"`, `"agents"`, `"hooks"`, `"workflows"`. Packs zonder onderdeel
    van die soort vallen weg; de volgorde volgt `packs.json`. `href` per soort:
    - skills: `REPO_TREE + "pack/<pack>/skills/<name>"`, met sort `standard-skill` als `name` in
      `standard_skills` staat, anders `workflow-skill`;
    - agents: `REPO_BLOB + "pack/<pack>/agents/<name>.md"`, met sort `agent`;
    - hooks: `REPO_BLOB + "pack/<pack>/hooks/<name>.sh"` (`name` van het hook-object), met sort
      `hook`;
    - workflows: `REPO_BLOB + "pack/<pack>/workflows/<name>.yml"`, met sort `github`.
  - `export function renderReference(root: HTMLElement, packs, part) -> void`: per pack een kop
    en een lijst links (`<a>` met teken, naam, klasse `kind-… form-<sort>`).
- Produces, in `packs.json`: elk pack heeft `standard_skills: string[]`, een deelverzameling van
  `skills` (nu: core `testing`, go `go-writing`, python `python-writing`, shell `shell-gates`).
- Produces, op `reference.html`: elementen met `id` `skills`, `agents`, `hooks` en `github`.

- [ ] **Stap 1: Schrijf de falende tests.**

```python
# tests/test_site_reference.py
def pack_parts() -> set[str]:
    """Every skill, agent, workflow and hook script under pack/, repo-relative."""
    pack = ROOT / "pack"
    parts = [p for p in pack.glob("*/skills/*") if p.is_dir()]
    parts += [*pack.glob("*/agents/*.md"), *pack.glob("*/workflows/*"), *pack.glob("*/hooks/*.sh")]
    return {p.relative_to(ROOT).as_posix() for p in parts}

def reference_paths() -> set[str]:
    """The repo paths the reference page links, from referenceEntries over packs.json."""
    # run_map over the four parts; strip REPO_BLOB or REPO_TREE from each href.

def test_every_pack_part_is_on_the_reference():
    assert pack_parts() - reference_paths() == set()

def test_every_reference_link_exists():
    assert [p for p in sorted(reference_paths()) if not (ROOT / p).exists()] == []

def test_reference_marks_standard_skills():
    entries = run_map(f"m.referenceEntries({json.dumps(load('packs.json'))}, 'skills')")
    sorts = {i["name"]: i["sort"] for e in entries for i in e["items"]}
    assert sorts["testing"] == "standard-skill"
    assert sorts["instrument"] == "workflow-skill"

def test_reference_page_has_the_four_anchors():
    html = (SITE / "reference.html").read_text(encoding="utf-8")
    for anchor in ["skills", "agents", "hooks", "github"]:
        assert f'id="{anchor}"' in html, anchor
    for target in ["tree/main/pack/core/templates", "tree/main/pack/core/workflows"]:
        assert target in html, target
```

```python
# tests/test_site_packs.py
def test_standard_skills_follow_the_maps():
    by_pack = {p["name"]: p for p in packs()}
    for entry in packs():
        assert set(entry["standard_skills"]) <= set(entry["skills"]), entry["name"]
    for name in ["chain.json", "session.json"]:
        for chip in load(name)["chips"]:
            m = re.search(r"/blob/main/pack/([^/]+)/skills/([^/]+)", chip.get("source", ""))
            if m:
                standard = m.group(2) in by_pack[m.group(1)]["standard_skills"]
                assert standard == (chip["trigger"] == "description"), chip["id"]
```

  In `tests/test_site_chain.py` vervalt `test_every_pack_part_is_on_the_chain` met zijn
  uitzondering voor `instrument` en `worktree-audit`: de dekking ligt nu in
  `test_every_pack_part_is_on_the_reference`. `test_coverage_flags_a_removed_skill` blijft; hij
  test de helper `missing_parts`. Voeg `reference.html` toe aan `MAP_PAGES`. Zet
  `REPO_TREE = "https://github.com/CorneeldH/lighthouse/tree/main/"` naast `REPO_BLOB` in
  `tests/test_site_chain.py`; taak 4 importeert hem daar ook.

- [ ] **Stap 2:** `uv run pytest -q tests/test_site_reference.py tests/test_site_packs.py tests/test_site_pages.py`.
  Verwacht: FAIL (`referenceEntries` bestaat niet, `standard_skills` ontbreekt, `reference.html`
  ontbreekt).

- [ ] **Stap 3: Implementeer.** Voeg `standard_skills` toe aan elk pack in `packs.json`, ook
  leeg. `chain.js` negeert de sleutel. Voeg in `map.js` `REPO_BLOB` en `REPO_TREE` toe, plus
  `referenceEntries` en `renderReference`. Bouw `reference.html` naar het patroon van `chain.html`:
  page-head, terug-link, themaknop, serve-hint, een legenda
  `data-sorts="workflow-skill standard-skill agent hook github"`, en vier `<section>`s. Elk heeft
  een `id`, een `<h2>`, één zin wat het is, en een leeg `<div data-part="…">` dat `reference.js`
  vult. `#github` vult `workflows`. Daarboven staan een paar zinnen over wat lighthouse op GitHub
  gebruikt (issues, milestones, labels, PR's, CI), en links naar
  `https://github.com/CorneeldH/lighthouse/tree/main/pack/core/templates` en `…/workflows`.
  `reference.js` laadt `data/packs.json`, roept per `[data-part]` `renderReference` aan en
  verbergt daarna de serve-hint. De foutafhandeling is die van `chain.js`. De stijl hergebruikt
  `.box` en de `kind-*`/`form-*`-kleuren.

- [ ] **Stap 4:** `uv run pytest -q tests/test_site_*.py`. Verwacht: PASS.

- [ ] **Stap 5: Commit.** `feat(site): referentiepagina met elk onderdeel per pack (#102)`

---

### Taak 4: De systeempagina in drie stappen

**Files:**
- Modify (vervangen): `docs/site/data/system.json`, `docs/site/system.html`,
  `docs/site/system.js`, `tests/fixtures/site/system.json`, `tests/test_site_system.py`
- Modify: `docs/site/map.js` (weg: `renderPlanMap`, `renderTimeline`, `issuesOf`,
  `milestoneTitle`, `planStatus`, `issueLink`, `showPart`, `showIssue`, `ISSUE_URL`; erbij:
  `renderStory`), `docs/site/map.css` (weg: het blok "System map" en "Stand switch and timeline":
  `.snapshot`, `.plan*`, `.stand-switch`, `.timeline*`, `.plan-chip*`; erbij: `.story*`),
  `docs/site/index.html` (tegel Map 4, eventueel `#download`), `tests/test_site_pages.py`,
  eventueel `tests/test_site_download.py`

**Interfaces:**
- Consumes: `SORTS`, `sortOf`, `glyphOf`, `fillLegend` (taak 1); de ankers van `reference.html`
  (taak 3).
- Produces: `export function renderStory(root: HTMLElement, data) -> void` in `map.js`.
- Data, `system.json`:
  `{ title, subtitle, steps: [{ id, title, command?, where, items: [Item] }], sections: [{ id, title, text: [string], groups?: [{ label, tools: [string] }] }] }`.
  `Item = { form, kind, label, note?, where?, href }`, met `form` een sleutel van `SORTS` en
  `kind` één van `lighthouse`, `superpowers`, `hook`, `github` en `you`. Een item met `where` staat
  apart onder zijn stap, met `where` als klein bijschrift.

De inhoud van `system.json` (de Engelse teksten van `note` en `sections[].text` schrijft de
bouwer, binnen spec §3):

| Stap | `form` · `kind` · `label` | `href` |
|---|---|---|
| `install`, "Install once", `where` "your workstation" | `program` · `lighthouse` · `lighthouse`, note: één programma met de packs erin, te installeren met curl | `index.html#download` |
| | `workflow-skill` · `superpowers` · `superpowers plugin`, note: extern, `init` controleert het | `#bring` |
| | `you` · `you` · `You bring`, note: `Claude Code, git, gh, jq, python3` | `#bring` |
| `init`, "lighthouse init", `command` "lighthouse init", `where` "your repo" | `workflow-skill` · `lighthouse` · `Skills`, note: workflow and standard | `reference.html#skills` |
| | `agent` · `lighthouse` · `Agents` | `reference.html#agents` |
| | `hook` · `hook` · `Hooks` | `reference.html#hooks` |
| | `rules` · `lighthouse` · `Rules`, note: `AGENTS.md`, `CLAUDE.md` | `#rules` |
| | `config` · `lighthouse` · `Config`, note: `lighthouse.toml`, `.lighthouse/` | `#config` |
| | `github` · `github` · `.github/`, note: PR template, workflows | `reference.html#github` |
| | `config` · `lighthouse` · `host.toml`, `where` "on your workstation", note: only with Tailscale | `#config` |
| `loop`, "Run the loop" | `loop` · `lighthouse` · `The loop`, note: issue to closed issue, over several sessions | `chain.html` |
| | `session` · `lighthouse` · `Inside a session`, note: which skill loads, which hook steps in | `session.html` |
| | `github` · `github` · `Keep track on GitHub`, note: issues, milestones, labels, PRs, CI | `reference.html#github` |

Secties: `bring` ("What you bring, and what lighthouse checks", met `groups` in deze volgorde:
`Needed` Claude Code, git, python3 3.11+, jq, superpowers; `For GitHub` gh; `Per language` uv, go,
gofmt, golangci-lint, node, npm, curl, sha256sum, tar, xz; `Optional` codex. De tekst zegt dat
`init` controleert en bij een ontbrekende tool doorgaat met `missing dependency: <tool> (needed by: …)`,
en linkt naar niets: tekst blijft tekst). `rules` (het beheerde blok in `AGENTS.md`, `CLAUDE.md`
die het importeert). `config` (`lighthouse.toml`, `.lighthouse/` met manifest, headings en
hulpcode, en `~/.config/lighthouse/host.toml` op je werkstation, alleen met Tailscale).

- [ ] **Stap 1: Schrijf de falende tests.** Herschrijf `tests/test_site_system.py` in het
  patroon van `tests/test_site_chain.py`: helpers die problemen teruggeven, geparametriseerd over
  een nieuwe kleine fixture (`tests/fixtures/site/system.json`, drie stappen met elk één item,
  één sectie `bring`) en `docs/site/data/system.json`.

```python
STEPS = ["install", "init", "loop"]
KINDS = {"lighthouse", "superpowers", "hook", "github", "you"}
FORBIDDEN = re.compile(r"\b(serve|tick|dashboard|controller)\b", re.IGNORECASE)

def shape_problems(system) -> list[str]          # keys == {"title","subtitle","steps","sections"}; step ids == STEPS
def sort_problems(system, sorts) -> list[str]    # "<label>: unknown form <f>", "<label>: unknown kind <k>"
def link_problems(system) -> list[str]           # see below
def word_problems(system) -> list[str]           # "<string>: names <word>" for every string anywhere in the JSON

@pytest.mark.parametrize("path", SYSTEMS)
def test_three_steps_in_order(path): assert shape_problems(load_system(path)) == []
@pytest.mark.parametrize("path", SYSTEMS)
def test_every_item_has_a_known_sort(path): assert sort_problems(load_system(path), SORT_KEYS) == []
@pytest.mark.parametrize("path", SYSTEMS)
def test_every_link_resolves(path): assert link_problems(load_system(path)) == []
@pytest.mark.parametrize("path", SYSTEMS)
def test_nothing_that_is_not_for_users(path): assert word_problems(load_system(path)) == []

def test_init_step_is_the_command():
    step = load_system(SYSTEM)["steps"][1]
    assert step["command"] == "lighthouse init"

def test_bring_groups_in_order():
    bring = next(s for s in load_system(SYSTEM)["sections"] if s["id"] == "bring")
    assert [g["label"] for g in bring["groups"]] == ["Needed", "For GitHub", "Per language", "Optional"]
    assert "superpowers" in bring["groups"][0]["tools"]
    assert bring["groups"][3]["tools"] == ["codex"]

def test_fixture_unknown_form_is_flagged():       # form "widget" -> ["<label>: unknown form widget"]
def test_fixture_unknown_kind_is_flagged():       # kind "acme"   -> ["<label>: unknown kind acme"]
def test_fixture_missing_step_is_flagged():       # drop "loop"   -> ["steps are ['install', 'init'], not ['install', 'init', 'loop']"]
def test_fixture_chips_key_is_flagged():          # add "chips"   -> ["unexpected key chips"]
def test_fixture_page_pointing_nowhere_is_flagged():    # href "nowhere.html" -> ["<label>: nowhere.html points nowhere"]
def test_fixture_anchor_pointing_nowhere_is_flagged():  # href "chain.html#nope" -> ["<label>: chain.html has no id nope"]
def test_fixture_section_anchor_pointing_nowhere_is_flagged():  # href "#nope" -> ["<label>: no section nope"]
def test_fixture_repo_path_pointing_nowhere_is_flagged():  # REPO_BLOB + "pack/core/nope" -> ["<label>: pack/core/nope does not exist"]
def test_fixture_item_without_href_is_flagged():  # del href -> ["<label>: no href"]
def test_fixture_dashboard_is_flagged():          # label "Dashboard (serve)" -> 2 problems, one per word
```

  `SORT_KEYS` komt uit `run_map("Object.keys(m.SORTS)")`. `link_problems` beslist zo per `href`:
  een `#id` moet een `sections[].id` zijn. Begint hij met `REPO_BLOB` of `REPO_TREE`, dan moet het
  pad bestaan. Elke andere `http(s)`-link is een probleem: `"<label>: external link <href>"`. De
  rest is `page[#id]`: `SITE / page` bestaat, en een `id` staat als `id="<id>"` in die pagina.

  In `tests/test_site_pages.py`, `test_index_lists_the_six_maps`: `"<h2>The system</h2>" in text`
  en `"roadmap" not in text.lower()`.

- [ ] **Stap 2:** `uv run pytest -q tests/test_site_system.py tests/test_site_pages.py`.
  Verwacht: FAIL (oude vorm van `system.json`, oude tegel).

- [ ] **Stap 3: Data.** Schrijf `docs/site/data/system.json` naar de tabel hierboven, met titel
  `"The system"` en ondertitel `"What you have once lighthouse is installed, and what happens when you work."`.
  Schrijf de fixture.

- [ ] **Stap 4: Weergave.** `renderStory(root, data)` tekent een `<ol class="story">` met per stap
  een `<li class="story-step">`. Daarin staan het stapnummer, de titel (bij `command` als
  terminalregel `<code class="story-command">` met `$ ` ervoor) en `where` als bijschrift. Daarna
  komen de items als `<a class="story-item kind-<kind> form-<sortOf(item)>" href>`, met
  `<span class="glyph" aria-hidden="true">`, het label en de note. Een item met `where` staat
  apart onderaan zijn stap, met dat bijschrift. Daaronder elke sectie als
  `<section class="box story-section" id>`, met `<h2>`, een `<p>` per tekst en de groepen via de
  bestaande `facts()`. Geen lade, geen schakelaar. CSS: drie kolommen naast elkaar, één kolom
  onder `@media (max-width: 700px)`. De terminalregel loopt niet over (`overflow-wrap: anywhere`).
  Een item krijgt een `:focus-visible`-ring zoals `.chip`.

- [ ] **Stap 5: Pagina.** `system.html`: weg met `#snapshot`, de status-legenda, de schakelaar,
  `#plan-map`, `#timeline` en `#detail`. Erin komen een legenda
  `data-sorts="program workflow-skill agent hook rules config github loop session you"` en een
  `<div id="story">`. `system.js` doet `fillLegend`, laadt `data/system.json`, zet titel en
  ondertitel, roept `renderStory` aan en verbergt de serve-hint, met de foutafhandeling van
  `chain.js`. Tegel Map 4 in `index.html`: `<h2>The system</h2>`, met de ondertitel van
  `system.json`.

- [ ] **Stap 5b: Andere installatieroutes.** Spec §3: de installatie noemt naast curl ook
  `go install` en een build uit een clone. Noemt `#download` in `index.html` ze na de rebase nog
  niet, voeg dan onder de platformblokken één `<p>` toe. Daarin staan
  `go install github.com/CorneeldH/lighthouse/cmd/lighthouse@latest`, een build uit een clone en
  een link naar `docs/getting-started.md` (absolute GitHub-URL, net als de andere links op de
  site). Neem de tekst over uit de installatiesectie van `docs/getting-started.md`, met de
  waarschuwing dat een build uit een clone versie `dev` meldt. Test in
  `tests/test_site_download.py`: het blok `id="download"` bevat `go install` en `clone`.

- [ ] **Stap 6: Opruimen.** Haal de functies en CSS-blokken uit **Files** weg. Daarna geeft
  `grep -n "renderPlanMap\|renderTimeline\|plan-\|stand-switch\|timeline\|snapshot" docs/site/*.js docs/site/*.css docs/site/*.html`
  niets. `drawerFor` blijft: `renderFlowMap` gebruikt hem.

- [ ] **Stap 7:** `uv run pytest -q tests/test_site_*.py`. Verwacht: PASS.

- [ ] **Stap 8: Commit.** Twee commits:
  `feat(site): systeempagina in drie stappen (#102)` (data, weergave, pagina, tegel, tests) en
  `refactor(site): plattegrond en Over time uit PR #157 weg (#102)` (stap 6).

---

### Taak 5: Changelog, headless check en volle gate

**Files:**
- Modify: `changelog.d/issue-102-systeemkaart.md`

- [ ] **Stap 1: Changelog.** Het fragment van PR #157 is nog niet uitgebracht (het staat nog in
  `changelog.d/`). Herschrijf het: `type: added`. Eén zin over `system.html` met de drie stappen,
  één over `reference.html`, één over de tekenset per soort op elke kaart, en één over de
  ketenkaart, die nu "The loop" heet. Geen woord over *Over time* of de roadmap.

- [ ] **Stap 2: Headless check** (wegwerpscript in de scratchpad, niet committen). Serveer met
  `python3 -m http.server --directory docs/site`. Open `system.html`, `reference.html`,
  `chain.html` en `session.html` in headless Chromium, op 1280 px en op 390 px breed, licht en
  donker. Controleer:
  - geen consolefouten;
  - op 390 px is `document.documentElement.scrollWidth <= 390` op `system.html` en
    `reference.html`;
  - `system.html` heeft 13 `.story-item`s, elk met een `href`;
  - de serve-hint is verborgen;
  - de tekens op `chain.html` zijn ✓ voor `testing` en ↪ voor `start-issue`;
  - op `session.html` draagt elke hook ⚡;
  - de legenda's staan er.

  Maak een schermafdruk van `system.html` (licht, 1280) en leg die naast de mockup.

- [ ] **Stap 3: Volle gate.** Draai `[test_gate] command` uit `lighthouse.toml`. Verwacht:
  groen. Draai daarna `uv run pytest -q`. Verwacht: groen.

- [ ] **Stap 4: Commit.** `docs(changelog): systeemkaart in drie stappen en referentiepagina (#102)`

- [ ] **Stap 5: PR.** `/lighthouse-verify-pr` schrijft de body (`Closes #102` met een
  bewijstabel per AC), daarna `gh pr create`.
