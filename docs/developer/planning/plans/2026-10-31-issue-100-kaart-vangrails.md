# Kaart van de vangrails (K2) — implementatieplan

> **Voor agents:** VERPLICHTE SUB-SKILL: gebruik superpowers:subagent-driven-development (aanbevolen) of superpowers:executing-plans om dit plan taak voor taak uit te voeren. Stappen gebruiken checkboxen (`- [ ]`).

**Doel:** Een tweede kaart, `docs/site/guardrails.html`, die de levensloop van één beurt toont
met elke hook uit `pack/*/hooks/hooks.json` op zijn event, met matcher, pakket en gedrag, en
routes per situatie. Een test faalt als kaart en `hooks.json` uit elkaar lopen.

**Architectuur:** De data staat met de hand in `docs/site/data/guardrails.json`, in het formaat
van `chain.json`. De gedeelde component `map.js` tekent hem met `renderFlowMap`. Hij krijgt
alleen een trigger `event` en een regel per hook-chip (matcher · pakket · gedrag) erbij. Een
nieuwe testmodule leest elke `hooks.json` en vergelijkt per (pakket, event, matcher, command) met
de chips. De generieke kaartchecks uit `tests/test_site_chain.py` lopen al over elke kaart in
`docs/site/data/`.

**Tech stack:** HTML, CSS, vanilla JS (ES-modules) en SVG; pytest (`json`, `re`).

**Spec:** [2026-10-27-kaarten-design.md](../specs/2026-10-27-kaarten-design.md), §4 (K2), §7
(tests 2 en 3, het hookdeel) en §8. Issue #100. Bouwt op het plan van #99:
[2026-10-31-issue-99-kaartsite.md](2026-10-31-issue-99-kaartsite.md).

## Globale randvoorwaarden

- Alle tekst op de site is Engels. Plan en commits zijn Nederlands. Testcode, namen en commentaar
  zijn Engels.
- Geen framework, geen buildstap, geen externe bibliotheek, geen generator (No-Go van de
  milestone). De data schrijf je met de hand.
- Verzin geen gedrag. Chipteksten komen uit `CLAUDE.md` *Hooks* en uit de kop van elk script.
  `code-language.sh`, `gofmt.sh`, `go-check.sh`, `ruff-format.sh`, `ty-check.sh` en
  `shellcheck.sh` staan niet in `CLAUDE.md`, dus hun tekst komt uit het script. De hook-chips in
  `chain.json` mag je hergebruiken waar de tekst past bij de modus.
- Een `source` is `https://github.com/CorneeldH/lighthouse/blob/main/pack/<pakket>/hooks/<script>`.
- `chain.json`, `chain.html`, `chain.js`, `packs.json` en `ci/chain_roadmap.py` veranderen niet.
- Dit raakt `pack/` en `internal/` niet, dus geen `init --upgrade`.
- Testgate: `[test_gate] command` uit `lighthouse.toml`. Gericht:
  `uv run pytest -q tests/test_site_chain.py tests/test_site_pages.py tests/test_site_guardrails.py`.

### Afwijkingen van de spec

- **Een vierde gedrag, `records`.** `bash-write-gates.sh pre` blokkeert niet, waarschuwt niet en
  formatteert niet: hij legt een baseline vast voor `post` (scriptkop: "Always exit 0").
  Blokkeert, waarschuwt en formatteert alleen zou dus gedrag verzinnen.
- **Een trigger `event`** (◷, "a turn event") voor hooks op `UserPromptSubmit`, `SubagentStop`
  en `Stop`. `tool` ("a tool call") klopt daar niet. Hooks op `PreToolUse` en `PostToolUse`
  houden `tool`.
- **`usage-guard` blokkeert met `{"decision": "block"}` en exit 0**, niet met exit 2
  (`lighthouse_lib/usage_guard.py`). Zijn `how` zegt dat. Het gedrag blijft `blocks`.

## Vaste keuzes

**Kolommen** (`phases`, in deze volgorde; `label` letterlijk, `blurb` vrij):

| `id` | `label` |
|---|---|
| `prompt` | `UserPromptSubmit` |
| `pre-tool` | `PreToolUse` |
| `tool-call` | `The tool call` |
| `post-tool` | `PostToolUse / PostToolUseFailure` |
| `subagent-stop` | `SubagentStop` |
| `stop` | `Stop` |

`tool-call` heeft geen chip. Zijn blurb zegt dat daar Bash, Edit of Write draait. Er is geen
`always`-band.

**Nieuwe optionele chipvelden** (alleen op hook-chips van deze kaart):

- `events`: lijst van eventnamen uit `hooks.json`;
- `matcher`: string; `""` als `hooks.json` geen matcher of een lege noemt;
- `command`: het command zonder `${CLAUDE_PLUGIN_ROOT}/hooks/`, bijvoorbeeld `commit-tests.sh pre`;
- `effects`: niet-lege lijst uit `blocks`, `warns`, `formats` en `records`.

Het pakket volgt uit `source` (`pack/<pakket>/hooks/<script>`).

**Chips** (`kind` overal `hook`, `status` overal `works`):

| `id` | `phase` | `row` | `trigger` | `events` | `matcher` | `command` | `effects` |
|---|---|---:|---|---|---|---|---|
| `usage-guard-prompt` | prompt | 7 | event | UserPromptSubmit | `""` | `usage-guard.sh prompt` | warns |
| `worktree-guard` | pre-tool | 0 | tool | PreToolUse | `Edit\|Write` | `worktree-guard.sh` | warns |
| `main-commit-guard` | pre-tool | 1 | tool | PreToolUse | `Bash` | `main-commit-guard.sh` | blocks |
| `commit-tests-pre` | pre-tool | 2 | tool | PreToolUse | `Bash` | `commit-tests.sh pre` | blocks |
| `pr-create-gate` | pre-tool | 3 | tool | PreToolUse | `Bash` | `pr-create-gate.sh` | blocks |
| `bash-write-gates-pre` | pre-tool | 4 | tool | PreToolUse | `Bash` | `bash-write-gates.sh pre` | records |
| `worktree-linkcheck` | post-tool | 0 | tool | PostToolUse | `Edit\|Write` | `worktree-linkcheck.sh` | blocks |
| `code-language` | post-tool | 1 | tool | PostToolUse | `Edit\|Write` | `code-language.sh` | blocks, warns |
| `commit-tests-post` | post-tool | 2 | tool | PostToolUse, PostToolUseFailure | `Bash` | `commit-tests.sh post` | blocks |
| `gofmt` | post-tool | 3 | tool | PostToolUse | `Edit\|Write` | `gofmt.sh` | formats, blocks |
| `bash-write-gates-post` | post-tool | 4 | tool | PostToolUse, PostToolUseFailure | `Bash` | `bash-write-gates.sh post` | blocks, warns |
| `ruff-format` | post-tool | 5 | tool | PostToolUse | `Edit\|Write` | `ruff-format.sh` | formats, blocks |
| `shellcheck` | post-tool | 6 | tool | PostToolUse | `Edit\|Write` | `shellcheck.sh` | blocks |
| `usage-guard-tool` | post-tool | 7 | tool | PostToolUse | `""` | `usage-guard.sh tool` | blocks |
| `usage-guard-subagent` | subagent-stop | 7 | event | SubagentStop | `""` | `usage-guard.sh subagent` | blocks |
| `go-check` | stop | 3 | event | Stop | `""` | `go-check.sh` | blocks, warns |
| `ty-check` | stop | 5 | event | Stop | `""` | `ty-check.sh` | blocks, warns |
| `usage-guard-stop` | stop | 7 | event | Stop | `""` | `usage-guard.sh stop` | blocks |

`label` is het command zonder `.sh` (`commit-tests pre`, `gofmt`). `warns` bij `go-check` en
`ty-check` is de `COULD NOT CHECK`-melding met exit 0 (scriptkop). Bij `code-language` is het de
notice zonder oordeel; bij `bash-write-gates post` een gate zonder geïnstalleerd pakket ("reported
per file, without blocking").

**Routes** (`id`, `label` letterlijk; `chips` in deze volgorde):

| `id` | `label` | `chips` |
|---|---|---|
| `main-commit` | `I commit on main` | worktree-guard, main-commit-guard |
| `red-tests` | `I commit with red tests` | commit-tests-pre, commit-tests-post |
| `pr-evidence` | `I open a PR without evidence` | pr-create-gate |
| `usage-limit` | `My limit is almost reached` | usage-guard-prompt, usage-guard-tool, usage-guard-subagent, usage-guard-stop |
| `dead-link` | `I write a dead link` | bash-write-gates-pre, worktree-linkcheck, bash-write-gates-post |
| `write-gates` | `I write code a gate rejects` | bash-write-gates-pre, code-language, gofmt, ruff-format, shellcheck, bash-write-gates-post, go-check, ty-check |

De zesde route staat niet in de AC. Hij is nodig omdat elke chip op een route moet staan
(`coverage_problems`).

## Review Focus

1. **Een nieuw pakket met een `hooks.json`** (zoals `pack/node` later): de test moet hem zien
   zonder codewijziging. Gedekt door `test_hook_entries_reads_every_pack` (taak 1).
2. **Een matcher die ontbreekt naast een lege matcher** (core laat hem weg, go en python zetten
   `""`): beide tellen als `""`. Gedekt door dezelfde test.
3. **Eén script op twee events met dezelfde chip** (`commit-tests.sh post` op `PostToolUse` en
   `PostToolUseFailure`): haal één event uit `events`, dan moet de test rood worden. Gedekt door
   `test_hook_problems_flags_a_dropped_event` (taak 1).
4. **Een lange matcher of label in een kolom van 210 px** (`Edit|Write · python · formats +
   blocks`): de regel mag afkappen, maar niet over de buurkolom lopen. Visuele check in taak 2,
   stap 7.
5. **Donkere modus**: de hookregel blijft leesbaar op de chipkleur `kind-hook`. Visuele check in
   taak 2, stap 7.

---

### Taak 1: Data van de vangrails en de hooktest

**Files:**
- Create: `docs/site/data/guardrails.json`
- Create: `tests/test_site_guardrails.py`
- Modify: `tests/test_site_chain.py:18-22` (vocabulaire) en `vocabulary_problems` (`:74-99`)

**Interfaces:**
- Produces: `guardrails.json` met de sleutels `title`, `subtitle`, `phases`, `chips`,
  `handoffs` (lege lijst, want `planned_problems` en `handoff_problems` lezen hem) en `routes`.
  Geen `handoff`.
- Produces in `tests/test_site_guardrails.py`:
  - `EVENT_PHASE: dict[str, str]`, de eventnaam naar de kolom-`id` uit *Vaste keuzes*;
  - `hook_entries(pack_root: Path) -> set[tuple[str, str, str, str]]`, met
    (pakket, event, matcher, command) uit elke `pack_root/*/hooks/hooks.json`;
  - `drawn_entries(data: dict) -> set[tuple[str, str, str, str]]`, één tupel per event van
    elke chip met `command`;
  - `hook_problems(data: dict, pack_root: Path) -> list[str]`.
- Produces in `tests/test_site_chain.py`: `TRIGGERS` met `"event"` erbij,
  `EFFECTS = {"blocks", "warns", "formats", "records"}`.

- [ ] **Stap 1: Schrijf de falende tests**

In `tests/test_site_chain.py`: zet `"event"` in `TRIGGERS` en voeg `EFFECTS` toe. In
`vocabulary_problems` komt `f"{name}: unknown effect"` bij een `effects` die leeg is of die geen
deelverzameling van `EFFECTS` is. Fixturetest:

```python
def test_fixture_unknown_effect_is_flagged():
    chain = copy.deepcopy(load_map(FIXTURE))
    chain["chips"][1]["effects"] = ["shouts"]
    assert vocabulary_problems(chain) == ["main-commit-guard: unknown effect"]
```

In `tests/test_site_guardrails.py` (importeer `ROOT`, `load` en `REPO_BLOB` uit
`tests.test_site_chain`):

```python
def test_hook_entries_reads_every_pack(tmp_path):
    for pack, matcher in [("a", None), ("b", "")]:
        group = {"hooks": [{"type": "command", "command": "${CLAUDE_PLUGIN_ROOT}/hooks/x.sh run"}]}
        if matcher is not None:
            group["matcher"] = matcher
        hooks = tmp_path / pack / "hooks"
        hooks.mkdir(parents=True)
        (hooks / "hooks.json").write_text(json.dumps({"hooks": {"Stop": [group]}}))
    assert hook_entries(tmp_path) == {("a", "Stop", "", "x.sh run"), ("b", "Stop", "", "x.sh run")}

def test_every_hook_is_on_its_event():
    assert hook_problems(load("guardrails.json"), ROOT / "pack") == []

def test_hook_problems_flags_a_removed_hook():
    data = copy.deepcopy(load("guardrails.json"))
    data["chips"] = [c for c in data["chips"] if c["id"] != "main-commit-guard"]
    assert hook_problems(data, ROOT / "pack") == ["not on the map: core PreToolUse Bash main-commit-guard.sh"]

def test_hook_problems_flags_another_matcher():
    data = copy.deepcopy(load("guardrails.json"))
    chip(data, "worktree-guard")["matcher"] = "Bash"
    assert hook_problems(data, ROOT / "pack") == [
        "not in hooks.json: core PreToolUse Bash worktree-guard.sh",
        "not on the map: core PreToolUse Edit|Write worktree-guard.sh",
    ]

def test_hook_problems_flags_another_column():
    data = copy.deepcopy(load("guardrails.json"))
    chip(data, "go-check")["phase"] = "subagent-stop"
    assert hook_problems(data, ROOT / "pack") == ["go-check: Stop belongs in column stop, not subagent-stop"]

def test_hook_problems_flags_a_dropped_event():
    data = copy.deepcopy(load("guardrails.json"))
    chip(data, "commit-tests-post")["events"] = ["PostToolUse"]
    assert hook_problems(data, ROOT / "pack") == ["not on the map: core PostToolUseFailure Bash commit-tests.sh post"]

def test_phases_are_the_hook_events():
    labels = [p["label"] for p in load("guardrails.json")["phases"]]
    assert labels == ["UserPromptSubmit", "PreToolUse", "The tool call",
                      "PostToolUse / PostToolUseFailure", "SubagentStop", "Stop"]

def test_routes_are_the_situations():
    routes = {r["id"]: r["label"] for r in load("guardrails.json")["routes"]}
    assert routes == {
        "main-commit": "I commit on main", "red-tests": "I commit with red tests",
        "pr-evidence": "I open a PR without evidence", "usage-limit": "My limit is almost reached",
        "dead-link": "I write a dead link", "write-gates": "I write code a gate rejects",
    }
```

`chip(data, id)` is een kleine helper in de testmodule: de chip met dat `id`.

`hook_problems` sorteert zijn regels. Hij meldt vier soorten:
- `not on the map: <pakket> <event> <matcher> <command>`, voor wat in `hooks.json` staat en niet
  op de kaart;
- `not in hooks.json: …`, voor het omgekeerde;
- `<id>: <event> belongs in column <kolom>, not <phase>`;
- `<id>: source does not match <pakket>/hooks/<script>`, als de bestandsnaam in `source` niet het
  eerste woord van `command` is.

Een lege matcher schrijf je in een regel als `all`, zodat er geen dubbele spatie ontstaat.

- [ ] **Stap 2: Draai de tests en zie ze falen**

Run: `uv run pytest -q tests/test_site_guardrails.py tests/test_site_chain.py`
Verwacht: FAIL. `test_site_guardrails.py` faalt op import (`hook_entries` bestaat niet) en
`test_fixture_unknown_effect_is_flagged` geeft `[]`.

- [ ] **Stap 3: Schrijf de helpers en `guardrails.json`**

Bouw `hook_entries`, `drawn_entries` en `hook_problems` zoals de Interfaces zeggen. `hook_entries`
haalt het voorvoegsel `${CLAUDE_PLUGIN_ROOT}/hooks/` van het command, en `g.get("matcher", "")`
zet een ontbrekende matcher op `""`.

Schrijf `guardrails.json` met de kolommen, chips en routes uit *Vaste keuzes*. Per chip schrijf
je `what`, `how` (een lijst stappen), `when` en `without` uit de bronnen die de
randvoorwaarden noemen. Voorbeelden:
- `title`: `The guardrails`;
- `subtitle`: `One turn in Claude Code, and which hook steps in when.`;
- `what` van `commit-tests-post`: wat de commit raakte gaat door `[test_gate] command`; rood is
  exit 2 na de commit, de commit blijft en de fix komt in een nieuwe;
- `how` van `usage-guard-tool`: noemt de blokkade met `{"decision": "block"}` en exit 0.

- [ ] **Stap 4: Draai de tests en zie ze slagen**

Run: `uv run pytest -q tests/test_site_guardrails.py tests/test_site_chain.py`
Verwacht: PASS. Ook de geparametriseerde `[guardrails.json]`-gevallen in `test_site_chain.py`
slagen: vocabulaire, bronnen, routes en dekking.

- [ ] **Stap 5: Commit**

```bash
git add docs/site/data/guardrails.json tests/test_site_guardrails.py tests/test_site_chain.py
git commit -m "feat(site): data van de vangrailskaart en een test die hem bij hooks.json houdt (#100)"
```

### Taak 2: De pagina en de hookregel in de component

**Files:**
- Create: `docs/site/guardrails.html`, `docs/site/guardrails.js`
- Modify: `docs/site/map.js` (`GLYPHS`, `TRIGGERS`, `STARTS` `:5-21`, `chipButton` `:216-236`,
  `showChip` `:370-399`), `docs/site/map.css` (`.chip .release` `:435-446`)
- Test: `tests/test_site_pages.py`

**Interfaces:**
- Consumes: `docs/site/data/guardrails.json` uit taak 1; `initTheme`, `loadJson` en
  `renderFlowMap` uit `map.js`.
- Produces: de pagina `guardrails.html` en een map-container met `id="guardrails-map"`.

- [ ] **Stap 1: Schrijf de falende tests**

In `tests/test_site_pages.py` parametriseer je `test_pages_name_the_local_server_command` en
`test_chain_links_back_to_index` (hernoem die naar `test_map_pages_link_back_to_index`) over
`["chain.html", "guardrails.html"]`. De pagina staat ook in de f-string van de assert-boodschap.

- [ ] **Stap 2: Draai de tests en zie ze falen**

Run: `uv run pytest -q tests/test_site_pages.py`
Verwacht: FAIL op `[guardrails.html]` (`FileNotFoundError`).

- [ ] **Stap 3: Breid `map.js` uit**

- `GLYPHS.event = "◷"`, `TRIGGERS.event = "a turn event"`, `STARTS.event = "Hook event"`.
- Een `EFFECTS`-map:
  - `blocks` → `"blocks the step"`;
  - `warns` → `"warns, does not block"`;
  - `formats` → `"formats the file"`;
  - `records` → `"records state for a later hook"`.
- `chipButton`: heeft de chip een `command`, dan komt er een `el("span", "hook", …)` bij met
  `` `${chip.matcher || "all"} · ${pack} · ${chip.effects.join(" + ")}` ``. Het pakket haal je
  uit `sourceParts(chip.source)`. Die regel staat ook in de `sr-only`-tekst.
- `showChip`: bij een chip met `command` komen er feiten bij in "When, who and why":
  - `["Event", chip.events.join(" and ")]`;
  - `["Matcher", chip.matcher || "none: every call"]`;
  - `["Command", chip.command]`;
  - `["What it does", chip.effects.map((e) => EFFECTS[e]).join("; ")]`.

  Een chip zonder `command` (de ketenkaart) ziet er precies zo uit als nu. Alles gaat via
  `textContent`, zoals in de rest van de component.

`map.css`: `.chip .hook` krijgt dezelfde regels als `.chip .release` (eigen regel,
`padding-left: 23px`). Voeg daar `overflow: hidden; text-overflow: ellipsis; white-space: nowrap`
aan toe.

- [ ] **Stap 4: Schrijf de pagina**

`guardrails.html` volgt `chain.html:1-55`, met deze verschillen:
- de titel `The guardrails`;
- een serve-hint met `http://localhost:8000/guardrails.html`;
- `<div id="guardrails-map">`;
- geen sectie `packs`.

De legenda heeft drie lijsten:
- "Who provides it": alleen hook;
- "How it starts": ⚡ a tool call, ◷ a turn event;
- "What it does": de vier gedragingen.

"Wait, but why?" heeft twee alinea's. De eerste: deze kaart beantwoordt "why does it stop me
now?". De tweede: elke vangrail is de tegenhanger van een regel in `AGENTS.md` of een skill; de
regel draagt de norm vooraf, de hook trekt de harde lijn op het moment zelf (scriptkoppen van
`main-commit-guard.sh` en `worktree-guard.sh`).

`guardrails.js` volgt `chain.js:63-80` zonder `packs.json` en zonder `packCard`.

- [ ] **Stap 5: Draai de tests en zie ze slagen**

Run: `uv run pytest -q tests/test_site_pages.py tests/test_site_chain.py tests/test_site_guardrails.py`
Verwacht: PASS. `test_site_links_resolve` en `test_component_never_writes_html_from_data` lopen
nu ook over de nieuwe pagina.

- [ ] **Stap 6: Commit**

```bash
git add docs/site/guardrails.html docs/site/guardrails.js docs/site/map.js docs/site/map.css tests/test_site_pages.py
git commit -m "feat(site): pagina van de vangrails, met matcher, pakket en gedrag per hook (#100)"
```

- [ ] **Stap 7: Visuele check**

Run: `python3 -m http.server --directory docs/site` en open
`http://localhost:8000/guardrails.html` en `http://localhost:8000/chain.html`.
Verwacht:
- zes kolommen, waarvan `The tool call` leeg is;
- elke hookregel blijft binnen zijn kolom, in licht en donker;
- de zes routeknoppen lichten hun chips op;
- de detaillade toont Event, Matcher, Command en What it does;
- de ketenkaart ziet er ongewijzigd uit.

### Taak 3: De ingang, de gids en de changelog

**Files:**
- Modify: `docs/site/index.html:56-60`, `docs/getting-started.md` (sectie *Your first issue*,
  bij de link naar de ketenkaart)
- Create: `changelog.d/issue-100-kaart-vangrails.md`
- Test: `tests/test_site_pages.py` (`test_index_lists_the_six_maps`, `test_guide_links_to_the_chain_map`)

**Interfaces:**
- Consumes: `docs/site/guardrails.html` uit taak 2.

- [ ] **Stap 1: Schrijf de falende tests**

In `test_index_lists_the_six_maps`:
- assert `"guardrails.html" in links`;
- haal `"#100"` uit de lijst van geplande issues.

Hernoem `test_guide_links_to_the_chain_map` naar `test_guide_links_to_the_maps` en assert ook
`"](site/guardrails.html)" in section`.

- [ ] **Stap 2: Draai de tests en zie ze falen**

Run: `uv run pytest -q tests/test_site_pages.py`
Verwacht: FAIL op `guardrails.html` in `index.html` en in de gids.

- [ ] **Stap 3: Werk de ingang, de gids en de changelog bij**

- **Tegel 2 in `index.html`.** Hij wordt
  `<a class="tile kind-lighthouse status-works" href="guardrails.html">`, met de kicker `Map 2`
  en dezelfde titel en tekst. Volg Map 1 (`index.html:51-55`).
- **De gids.** In `docs/getting-started.md` komt één zin na de zin over de ketenkaart: welke hook
  wanneer ingrijpt, staat op
  [the guardrails map](https://corneeldh.github.io/lighthouse/guardrails.html), lokaal op
  `site/guardrails.html`. Gebruik relatieve markdownlinks; `worktree-linkcheck` controleert ze.
- **De changelog.** `changelog.d/issue-100-kaart-vangrails.md`, in het Engels, met
  `type: added`. De tekst: een kaart van de vangrails, met elke hook op zijn event, en matcher,
  pakket en gedrag erbij.

- [ ] **Stap 4: Draai de hele testgate**

Run: het `[test_gate] command` uit `lighthouse.toml`.
Verwacht: PASS.

- [ ] **Stap 5: Commit**

```bash
git add docs/site/index.html docs/getting-started.md changelog.d/issue-100-kaart-vangrails.md tests/test_site_pages.py
git commit -m "docs(site): vangrailskaart in de ingang en de startgids (#100)"
```
