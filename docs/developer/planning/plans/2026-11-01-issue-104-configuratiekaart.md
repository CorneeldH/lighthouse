# Configuratiekaart (#104) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Kaart 6 van de site: elke sleutel van `lighthouse.toml` en `host.toml` met verplichting,
standaard, niveau (zelf zetten / optioneel / geavanceerd) en de skills, hooks en code die hem
lezen, bewaakt door een test tegen `config.py` en `internal/usage/config.go`.

**Architecture:** Handgeschreven data in `docs/site/data/config.json` (geen generator, no-go van
de milestone). Een dekkingstest in `tests/test_site_config.py` vergelijkt die data met
`SCHEMA`, `DEFAULTS`, `_BASE_KEYS`, `_OPTION_PREFIXES` en `HOST_KEYS` uit
`pack/core/lib/config.py` en met de `[usage]`-tabel uit `internal/usage/config.go`, en eist dat
de lezers per sleutel precies de bestanden zijn die de sleutel noemen (afgeleid uit `pack/`,
`internal/` en `cmd/`). Drie nieuwe functies in `docs/site/map.js` (`configReader`,
`configEntries`, `renderConfig`) tekenen de pagina in de drie niveaus van spec §3: het geheel,
een niveaufilter dat de rest dimt, en een drawer per sleutel. `config.html` + `config.js` volgen
het patroon van `session.html`.

**Tech Stack:** vanilla JS (ES-modules, geen buildstap), JSON, pytest, node (via
`tests/_site_node.py`).

**Spec:** [`docs/developer/planning/specs/2026-10-27-kaarten-design.md`](../specs/2026-10-27-kaarten-design.md)
§4, K6 (regels 267-284). Uitbreiding van de gebruiker bij het oppakken: ook `host.toml`.

## Global Constraints

- Sitepagina's en data zijn Engels (`<html lang="en">`, `test_page_lang_is_english`); dit plan,
  commits en PR zijn Nederlands.
- Data komt alleen via `textContent` en attributen in de DOM, nooit als HTML
  (`test_component_never_writes_html_from_data`: geen `innerHTML` e.d.).
- Elke lokale `.js`/`.css`-verwijzing draagt een `?v=`-token; na een wijziging aan site-JS of -CSS
  `python3 -m tests.site_version` (`tests/test_site_assets.py`).
- Geen controller-opties: geen sleutel uit `[controller]` van `lighthouse.toml`, geen
  `[dispatch]` van `host.toml`, en het woord `Controller` (de statusoptie) niet in de data.
  `usage_guard.controller` staat er wél op, als "advanced" (spec K6).
- Zelf zetten (`set`): `project.language`, `forge.kind`, `forge.repo`, `test_gate.command`, en in
  `host.toml` `host.name` en `host.tailscale_ip` (die schrijft `init`). Optioneel (`optional`):
  elke sleutel onder `forge.board`. De rest `advanced`.
- Geen wijziging onder `pack/` of `internal/`: de dogfood-regel (`init --upgrade`) geldt niet.
- Eén fragment `changelog.d/issue-104-configuratiekaart.md`, `type: added`, Engels.

## Beslissingen bij het oppakken

- `host.toml` telt mee met twee tabellen: `[host]` (`HOST_KEYS` in `config.py`) en `[usage]`
  (alarm van `lighthouse usage`, `internal/usage/config.go`). `[dispatch]`
  (`internal/hostconfig/hostconfig.go`) blijft eraf: dat is de tick/controller, no-go.
- De open optietabellen `forge.board.status_options` en `forge.board.priority_options` staan er
  elk als één sleutel (zonder `.<name>`).
- `forge.owner` en `forge.name` staan erop met `"required": "derived"`: `config.py` leidt ze af
  uit `forge.repo`, je zet ze niet zelf.
- Vier sleutels hebben niets in de repo als lezer, alleen `config.py` controleert ze:
  `paths.history`, `codex_usage.vraag_pct_week`, `codex_usage.halt_pct_session` en `host.name`
  (die laatste noemt alleen `internal/initcmd/initcmd_test.go`; `internal/initcmd/host.go`
  schrijft hem). Ze staan op de kaart met `"readers": []` en in de test in de set `UNREAD`; de
  drawer zegt dan "Nothing in lighthouse reads it yet." Zo liegt de kaart niet (spec §2).
- Spec §3 (drie niveaus) geldt ook hier, vertaald naar configuratie: (1) het geheel = alle
  sleutels per bestand en tabel als knoppen; (2) de zone = een niveaufilter (`Set this`,
  `Optional`, `Advanced`) dat de sleutels van de andere niveaus dimt; (3) één onderdeel = klik op
  een sleutel opent de drawer (`drawerFor`) met eerst `about`, dan waar hij staat (bestand en
  tabel), `Required`, wat er zonder gebeurt (`Without it: <standaard>` of `Without it: not set`),
  wie hem leest (de lezers, met links) en `Status: works` (elke sleutel staat in `config.py` op
  `main`).

## Review Focus

1. Een lezer die de sleutel in een andere notatie noemt (`[test_gate] command` in plaats van
   `test_gate.command`) → telt als lezer; gedekt door `reader_names_key` in Taak 1.
2. Een lezer-pad naar een dogfood-kopie (`.claude/skills/...`) → afgewezen, alleen `pack/`,
   `internal/` en `cmd/`; test in Taak 1.
3. Een lijststandaard (`paths.search_dirs = (".",)`) → JSON-lijst `["."]` is gelijk, een tuple
   in `DEFAULTS` mag niet als verschil tellen; test in Taak 1.
4. Een sleutel zonder standaard die toch een `default` in de JSON krijgt → gemeld; test in Taak 1.
5. Een lezer van een standaardskill (`go-standard`) krijgt het glyph van een standaardskill, niet
   van een workflowskill; test in Taak 2.

---

### Taak 1: Data en dekkingstest

**Files:**
- Create: `docs/site/data/config.json`
- Create: `tests/test_site_config.py`

**Interfaces:**
- Produces: `config.json` met deze vorm (later gelezen door `renderConfig`):

```json
{
  "title": "The configuration",
  "subtitle": "Every key of lighthouse.toml and host.toml: whether you set it, its default, and what reads it.",
  "files": [
    {
      "file": "lighthouse.toml",
      "where": "in the root of your repo",
      "tables": [
        {
          "table": "project",
          "keys": [
            {"key": "project.language", "level": "set", "required": "no", "default": "en",
             "about": "The language of issues, PRs and commit messages: nl or en.",
             "readers": ["pack/core/skills/write-issue", "pack/core/skills/close-issue"]}
          ]
        }
      ]
    },
    {"file": "host.toml", "where": "~/.config/lighthouse/ on your workstation", "tables": ["…[host], [usage]"]}
  ]
}
```

  (De `readers` hierboven zijn een ingekort voorbeeld; de echte lijst komt uit `found_readers`.)
  `level` ∈ `set|optional|advanced`; `required` ∈ `yes|no|with [forge.board]|derived`;
  `default` alleen als de sleutel er een heeft (JSON-waarde, lijst voor een tuple); `readers` is
  een gesorteerde lijst repo-relatieve paden (skillmap, of bestand), precies `found_readers(key)`;
  leeg alleen voor een sleutel in `UNREAD`.
- Produces (test-helpers, alleen in dit bestand): `expected_keys() -> dict[str, object]`
  (sleutel → standaard of `NO_DEFAULT`), `usage_defaults(go: str) -> dict[str, int]`,
  `reader_names_key(text: str, key: str, path: str) -> bool`,
  `found_readers(key: str, root: Path) -> list[str]`,
  `config_problems(data: dict, root: Path) -> list[str]`, en de constante
  `UNREAD = {"paths.history", "codex_usage.vraag_pct_week", "codex_usage.halt_pct_session", "host.name"}`.

- [ ] **Stap 1: Schrijf de test**

`tests/test_site_config.py`, in de stijl van `tests/test_site_session.py` (helpers die een
gesorteerde lijst problemen geven, plus mutatietests op een `copy.deepcopy` van de data).
`ROOT`, `load` uit `tests.test_site_chain`. `lighthouse_lib` staat niet op `sys.path` buiten
`tests/pack/` (alleen `tests/pack/conftest.py:13` zet het erop; `pyproject.toml:18` geeft alleen
`.`). Laad `config.py` daarom per pad, zoals `tests/_changelog.py`, en registreer de module in
`sys.modules` vóór `exec_module` (de `@dataclass`-klassen in `config.py` zoeken hun module daar):

```python
_spec = importlib.util.spec_from_file_location("lighthouse_site_config", ROOT / "pack/core/lib/config.py")
config = importlib.util.module_from_spec(_spec)
sys.modules[_spec.name] = config
_spec.loader.exec_module(config)
```

- `expected_keys()`: `SCHEMA` ∪ `_BASE_KEYS` ∪ `{p.rstrip(".") for p in _OPTION_PREFIXES}` ∪
  `HOST_KEYS` ∪ de `usage.*`-sleutels. Standaard: `DEFAULTS[k]` (tuple → list),
  `project.language` → `"en"`, `usage.*` uit `usage_defaults`, anders `NO_DEFAULT`.
- `usage_defaults(go)`: de `toml:"…"`-tags in het `Usage struct`-blok van
  `internal/usage/config.go`, gekoppeld aan de waarden uit
  `Config{HaltPctSession: 85, HaltPctWeek: 95, TTL: 300 * time.Second}` in `DefaultConfig`:
  `{"usage.halt_pct_session": 85, "usage.halt_pct_week": 95, "usage.cache_ttl_seconds": 300}`.
  Faalt luid (assert) als de regex niets vindt.
- `reader_names_key(text, key, path)` (tabel = alles voor de laatste punt, naam = het laatste
  deel): waar als
  1. `key` letterlijk in `text` staat; of
  2. `[<tabel>] <naam>` erin staat; of
  3. `path` eindigt op `.go` en `text` bevat zowel `toml:"<naam>"` als `toml:"<laatste deel van
     de tabel>"` (zo telt `internal/usage/config.go` voor `usage.halt_pct_session`, met
     `toml:"usage"` op regel 37, maar niet voor `codex_usage.halt_pct_session`); of
  4. `path` eindigt op `.py` en `text` bevat zowel `f"<tabel>.{` als `"<naam>"` (de dynamische
     lezers: `pack/core/scripts/codex_usage.py:430-435` bouwt `f"codex_usage.{name}"` uit
     `"app_server_timeout_seconds"`, `"review_pct_session"`, `"review_pct_week"`;
     `pack/core/lib/code_language_llm.py:58-62` bouwt `f"code_language_llm.{key}"` en vraagt
     `get("key_env")`).
- `found_readers(key, root)`: alle bestanden uit `git ls-files pack internal cmd`, behalve
  `pack/core/lib/config.py`, `*_test.go` en alles onder een `testdata/`-map, waarvoor
  `reader_names_key` waar is. Een bestand onder `pack/<p>/skills/<n>/` telt als
  `pack/<p>/skills/<n>`. Gesorteerd, zonder dubbelen. Lees elk bestand één keer (cache op
  moduleniveau), met `errors="replace"`.
- `config_problems(data, root)` meldt, gesorteerd:
  - `not on the map: <key>` en `not in config.py: <key>`;
  - `<key>: default <x>, config.py says <y>` en `<key>: default on the map, none in config.py`;
  - `<key>: level <x>, expected <y>` tegen `LEVELS` (zie Global Constraints);
  - `<key>: required <x>, expected <y>` tegen `REQUIRED = {"forge.board.project_number": "with [forge.board]", "forge.board.project_id": …, "forge.board.status_field_id": …, "forge.board.status_options": …, "host.name": "yes", "host.tailscale_ip": "yes", "forge.owner": "derived", "forge.name": "derived"}`, de rest `"no"`;
  - `<key>: no readers` (lege `readers` voor een sleutel buiten `UNREAD`),
    `<key>: reader <p> outside pack/, internal/ or cmd/`, `<key>: reader <p> does not exist`,
    `<key>: reader <p> does not name it` (op de kaart, niet in `found_readers`) en
    `<key>: reader <p> missing from the map` (in `found_readers`, niet op de kaart). Een lezer
    buiten `pack/`, `internal/` of `cmd/` of een die niet bestaat krijgt alleen die ene melding,
    geen `does not name it` erbij.

Tests:

```python
def test_every_key_is_on_the_map():
    assert config_problems(load("config.json"), ROOT) == []

def test_usage_defaults_come_from_go():
    go = (ROOT / "internal/usage/config.go").read_text(encoding="utf-8")
    assert usage_defaults(go) == {"usage.halt_pct_session": 85, "usage.halt_pct_week": 95,
                                  "usage.cache_ttl_seconds": 300}

def test_reader_names_key_accepts_the_table_notation():
    assert reader_names_key("run `[test_gate] command`", "test_gate.command", "a/SKILL.md")
    assert not reader_names_key("test_gate.slots", "test_gate.command", "a/SKILL.md")

def test_reader_names_key_needs_the_table_tag_in_go():
    go = (ROOT / "internal/usage/config.go").read_text(encoding="utf-8")
    assert reader_names_key(go, "usage.halt_pct_session", "internal/usage/config.go")
    assert not reader_names_key(go, "codex_usage.halt_pct_session", "internal/usage/config.go")

def test_reader_names_key_finds_a_dynamic_python_reader():
    text = (ROOT / "pack/core/scripts/codex_usage.py").read_text(encoding="utf-8")
    assert reader_names_key(text, "codex_usage.review_pct_week", "pack/core/scripts/codex_usage.py")
    assert not reader_names_key(text, "codex_usage.vraag_pct_week", "pack/core/scripts/codex_usage.py")

def test_found_readers_skips_go_tests():
    assert found_readers("host.name", ROOT) == []  # alleen internal/initcmd/initcmd_test.go noemt hem

def test_unread_keys_have_no_reader():
    assert {k: found_readers(k, ROOT) for k in UNREAD} == {k: [] for k in UNREAD}

def test_no_controller_options():
    raw = (ROOT / "docs/site/data/config.json").read_text(encoding="utf-8")
    keys = {k["key"] for f in load("config.json")["files"] for t in f["tables"] for k in t["keys"]}
    assert not {k for k in keys if k.startswith(("controller.", "dispatch."))}
    assert "Controller" not in raw
```

Mutatietests (elk op een kopie, met de exacte verwachte lijst):
`test_problems_flag_a_removed_key` (haal `test_gate.slots` weg →
`["not on the map: test_gate.slots"]`), `test_problems_flag_a_dispatch_key` (voeg
`dispatch.slots` toe → `["not in config.py: dispatch.slots"]`),
`test_problems_flag_another_default` (`labels.needs_hands` → `"x"`),
`test_problems_flag_a_default_without_one` (`default` op `paths.tenets`),
`test_problems_flag_a_lowered_level` (`forge.repo` → `advanced`),
`test_problems_flag_a_reader_that_does_not_name_it` (lezer van `shell.strict_dirs` →
`pack/core/skills/close-issue`), `test_problems_flag_a_missing_reader` (haal
`pack/core/hooks/commit-test-gate.sh` weg bij `test_gate.command` →
`["test_gate.command: reader pack/core/hooks/commit-test-gate.sh missing from the map"]`),
`test_problems_flag_an_empty_reader_list` (`readers: []` bij `test_gate.command` → de
`no readers`-melding plus een `missing from the map` per gevonden lezer),
`test_problems_flag_a_dogfood_reader` (`.claude/skills/lighthouse-start-issue`),
`test_problems_flag_a_tuple_default_as_equal` (`paths.search_dirs` met `["."]` geeft geen
melding).

- [ ] **Stap 2: Draai de test, zie hem falen**

Run: `uv run pytest -q tests/test_site_config.py`
Expected: FAIL — `FileNotFoundError` op `docs/site/data/config.json` in de tests die de data
laden; de tests van `usage_defaults`, `reader_names_key` en `found_readers` slagen al.

- [ ] **Stap 3: Schrijf `docs/site/data/config.json`**

Tabellen in deze volgorde: `lighthouse.toml` → `project`, `forge`, `forge.board`, `test_gate`,
`paths`, `labels`, `ci`, `usage_guard`, `codex_usage`, `worktree_audit`, `python`, `shell`,
`code_language_llm`; `host.toml` → `host`, `usage`. Binnen een tabel de `set`-sleutels eerst.
Elke `about` is één Engelse zin met wat de sleutel doet (bron: het commentaar in `config.py`, de
lezer, of `docs/`). De `readers` van elke sleutel zijn precies wat `found_readers` vindt; druk
ze af en neem ze over:

```bash
uv run python -c "from tests.test_site_config import expected_keys, found_readers, ROOT
for k in sorted(expected_keys()): print(k, found_readers(k, ROOT))"
```

Een skill telt als zijn map (`pack/<pack>/skills/<naam>`), niet per `claude/`- of
`agents/`-variant; `config.py` zelf is geen lezer. Een lege lijst hoort alleen bij de vier
sleutels in `UNREAD`. Geeft `found_readers` een lege lijst voor een andere sleutel, of iets voor
een sleutel in `UNREAD`: stop en meld het als BLOCKED; verzin geen lezer en pas `UNREAD` niet
zelf aan.

- [ ] **Stap 4: Draai de test, zie hem slagen**

Run: `uv run pytest -q tests/test_site_config.py`
Expected: PASS, alle tests.

- [ ] **Stap 5: Mutatiecheck (`/lighthouse-testing`)**

Haal met de hand één sleutel uit `config.json` en draai `test_every_key_is_on_the_map`: rood met
`not on the map: <key>`. Zet hem terug.

- [ ] **Stap 6: Commit**

```bash
git add docs/site/data/config.json tests/test_site_config.py
git commit -m "feat(site): data van de configuratiekaart met dekkingstest tegen config.py (#104)"
```

### Taak 2: Renderer in `map.js`

**Files:**
- Modify: `docs/site/map.js` (na `renderReference`, rond regel 262)
- Modify: `docs/site/map.css` (na het `.reference`-blok, rond regel 1135)
- Test: `tests/test_site_config.py`

**Interfaces:**
- Consumes: `config.json` uit Taak 1; `packs.json` (`standard_skills` per pack).
- Produces:
  - `export function configReader(path, packs) -> {name, sort, href}`:
    `pack/<p>/skills/<n>` → `name` `<n>`, sort `standard-skill` als `<n>` in
    `standard_skills` van pack `<p>`, anders `workflow-skill`, `href` `REPO_TREE + path`;
    `pack/<p>/hooks/<x>.sh` → `hook`; `pack/<p>/workflows/<x>.yml` → `github`;
    `pack/<p>/agents/<x>.md` → `agent`; al het andere → `program`. Buiten skills:
    `name` = bestandsnaam, `href` = `REPO_BLOB + path`.
  - `export function configEntries(data, packs) -> [{file, table, key, level, levelLabel, required, without, about, readers}]`:
    puur, zonder DOM, in de volgorde van de data. `levelLabel` ∈ `Set this` / `Optional` /
    `Advanced`; `without` = `Without it: <standaard>` als er een `default` is (een lijst als
    `[a, b]`, `false`/`true` als tekst), anders `Without it: not set`; `readers` =
    `configReader(p, packs)` per lezer.
  - `export function renderConfig(root, data, packs, detail)`, de drie niveaus van spec §3 (zie
    *Beslissingen bij het oppakken*), op `configEntries`:
    1. Het geheel: per bestand een `section.config-file` met `h2` (`file`) en een `p`
       (`where`), per tabel een `h3` `[<table>]` en een `div.config-keys`; per sleutel een
       `button.config-key status-works` (`type="button"`, `data-key`, `data-level`) met een
       `code` (de sleutel) en een `span.config-level level-<level>` (`levelLabel`).
    2. De zone: bovenaan een `div.routes` met vier knoppen (`All keys`, `Set this`,
       `Optional`, `Advanced`, `aria-pressed`); een niveau kiezen zet `dim` op elke
       `config-key` met een ander `data-level`, `All keys` haalt `dim` weg.
    3. Eén onderdeel: een klik op een sleutel roept `drawerFor(detail).show(button, boxes)` aan
       met `box(...)`-blokken: `about` eerst, dan `In <file>, table [<table>]`,
       `Required: <required>`, `without`, `Read by` met een `ul.reference-list` met per lezer
       een link zoals `renderReference` die maakt (glyph + naam,
       `kind-${KIND_OF_SORT[sort]} form-${sort}`; `program` krijgt `kind-lighthouse`), of de zin
       `Nothing in lighthouse reads it yet.` bij een lege lijst, en `Status: works`.
    Alle tekst via `el(...)`/`textContent`, nooit HTML.

- [ ] **Stap 1: Schrijf de test**

```python
def test_config_reader_marks_the_sort():
    packs = json.dumps(load("packs.json"))
    read = lambda p: run_map(f"m.configReader({json.dumps(p)}, {packs})")
    assert read("pack/go/skills/go-standard")["sort"] == "standard-skill"
    assert read("pack/core/skills/write-issue") == {
        "name": "write-issue", "sort": "workflow-skill",
        "href": REPO_TREE + "pack/core/skills/write-issue"}
    assert read("pack/core/hooks/commit-test-gate.sh")["sort"] == "hook"
    assert read("pack/core/workflows/unblock-dependents.yml")["sort"] == "github"
    assert read("internal/usage/config.go") == {
        "name": "config.go", "sort": "program", "href": REPO_BLOB + "internal/usage/config.go"}

def test_every_reader_on_the_map_gets_a_link():
    packs = json.dumps(load("packs.json"))
    paths = sorted({r for f in load("config.json")["files"] for t in f["tables"]
                    for k in t["keys"] for r in k["readers"]})
    links = run_map(f"{json.dumps(paths)}.map((p) => m.configReader(p, {packs}).href)")
    assert all(h.startswith((REPO_BLOB, REPO_TREE)) for h in links)

def config_entries():
    data, packs = json.dumps(load("config.json")), json.dumps(load("packs.json"))
    return {e["key"]: e for e in run_map(f"m.configEntries({data}, {packs})")}

def test_config_entries_cover_every_key_with_its_label():
    entries = config_entries()
    keys = {k["key"] for f in load("config.json")["files"] for t in f["tables"] for k in t["keys"]}
    assert set(entries) == keys
    assert entries["forge.repo"]["levelLabel"] == "Set this"
    assert entries["forge.board.project_id"]["levelLabel"] == "Optional"
    assert entries["usage_guard.controller"]["levelLabel"] == "Advanced"

def test_config_entries_say_what_happens_without_the_key():
    entries = config_entries()
    assert entries["paths.search_dirs"]["without"] == "Without it: [.]"
    assert entries["usage_guard.resume"]["without"] == "Without it: false"  # config.py: False
    assert entries["paths.tenets"]["without"] == "Without it: not set"
    assert entries["paths.history"]["readers"] == []
```

`KIND_OF_SORT` krijgt `program: "lighthouse"`; controleer dat `renderReference` daar niet door
verandert.

- [ ] **Stap 2: Draai, zie falen**

Run: `uv run pytest -q tests/test_site_config.py -k config_reader`
Expected: FAIL — `m.configReader is not a function`.

- [ ] **Stap 3: Implementeer `configReader`, `configEntries` en `renderConfig`, plus CSS**

`renderConfig` gebruikt de bestaande `el`, `box` (`map.js:381`) en `drawerFor` (`map.js:467`);
de niveauknoppen hergebruiken `.routes button` (`map.css:340-355`). CSS: hergebruik de tokens
van `.reference` (geen nieuwe kleuren); `.config-key.dim` krijgt `opacity: 0.25` en
`.config-key.dim.current` `opacity: 1`, zoals `.chip.dim` (`map.css:523-535`); `.level-set`
krijgt de accentkleur van `status-works`, `.level-optional` een rand, `.level-advanced` gedempt.
Werkt in licht en donker (de bestaande tokens doen dat).

- [ ] **Stap 4: Versietokens**

Run: `python3 -m tests.site_version` (`map.js` en `map.css` zijn veranderd; zonder dit is
`tests/test_site_assets.py` rood na deze commit).

- [ ] **Stap 5: Draai, zie slagen**

Run: `uv run pytest -q tests/test_site_config.py tests/test_site_reference.py tests/test_site_sorts.py tests/test_site_pages.py tests/test_site_assets.py`
Expected: PASS.

- [ ] **Stap 6: Commit**

```bash
# docs/site geheel: site_version herschrijft de ?v=-tokens in alle pagina's en scripts.
git add docs/site tests/test_site_config.py
git commit -m "feat(site): configReader, configEntries en renderConfig voor de configuratiekaart (#104)"
```

### Taak 3: Pagina, index en changelog

**Files:**
- Create: `docs/site/config.html`, `docs/site/config.js`
- Modify: `docs/site/index.html:120-124`, `tests/test_site_pages.py:91` en `:121-135`,
  `tests/test_site_sorts.py:52-56`, `docs/getting-started.md` (sectie `## Configure lighthouse.toml`,
  regel 120)
- Create: `changelog.d/issue-104-configuratiekaart.md`

**Interfaces:**
- Consumes: `renderConfig(root, data, packs, detail)`, `initTheme`, `fillLegend`, `loadJson` uit
  `map.js`.

- [ ] **Stap 1: Pas de tests aan**

- `MAP_PAGES` krijgt `"config.html"` (serve-hint- en terug-link-tests gelden dan ook hier).
- `test_index_lists_the_six_maps`: `"config.html" in links`; `"#104"` verhuist van de
  `in text`-lijst naar de `not in text`-lijst (`["#100", "#102", "#104"]`).
- `test_legends_name_known_sorts`: `config.html` in de lijst pagina's met een legenda.
- Nieuwe test `test_guide_links_to_the_config_map` (spec §5: de gids linkt naar een kaart zodra
  die er is): in de sectie `## Configure lighthouse.toml` van `GUIDE` staan `](site/config.html)`
  en `[The configuration](https://corneeldh.github.io/lighthouse/config.html)`, zoals
  `test_guide_links_to_the_maps` dat voor `## Your first issue` doet.

Run: `uv run pytest -q tests/test_site_pages.py tests/test_site_sorts.py`
Expected: FAIL — `config.html` bestaat niet, de gids linkt er nog niet naar.

- [ ] **Stap 2: Schrijf de pagina**

`config.html` volgt `session.html`: `<title>The configuration</title>`, `#title`, `#subtitle`,
terug-link, themaknop, `#serve-hint` met `http://localhost:8000/config.html`, een
`<details class="why">` met twee alinea's (waar de twee bestanden staan en wie ze schrijft:
`lighthouse init` schrijft `lighthouse.toml` en biedt `host.toml` aan op een machine met
Tailscale; wat de drie niveaus betekenen, en dat controller-instellingen later volgen), een
legenda `data-sorts="workflow-skill standard-skill hook github program"` en een lijst met de
drie niveaus, `<div id="config-map"></div>` en daarna
`<aside id="detail" class="detail" aria-label="Details"></aside>` (zoals `session.html:74`).
`config.js` volgt `session.js`, laadt `data/config.json` en `data/packs.json` en roept
`renderConfig(document.getElementById("config-map"), config, packs, document.getElementById("detail"))`
aan.

In `docs/getting-started.md`, sectie `## Configure lighthouse.toml`, na de eerste alinea één
zin met beide links: de lokale kopie `[site/config.html](site/config.html)` en
`[The configuration](https://corneeldh.github.io/lighthouse/config.html)`.

- [ ] **Stap 3: Index-tegel**

`index.html:120-124` wordt een `<a class="tile kind-lighthouse status-works" href="config.html">`
met `Map 6`, `The configuration` en `<p>The keys of <code>lighthouse.toml</code> and
<code>host.toml</code>, and what reads them.</p>`.

- [ ] **Stap 4: Versietokens en changelog**

Run: `python3 -m tests.site_version`

`changelog.d/issue-104-configuratiekaart.md`:

```markdown
---
type: added
---
A map of the configuration in `docs/site/config.html` lists every key of `lighthouse.toml` and `host.toml` with whether you set it, its default, and the skills, hooks and code that read it. A test fails when a key in `config.py` or the `[usage]` table of `host.toml` is missing from the map.
```

- [ ] **Stap 5: Draai alles**

Run: `uv run pytest -q tests/`
Expected: PASS. Daarna lokaal `python3 -m http.server --directory docs/site` en
`http://localhost:8000/config.html` openen: serve-hint verborgen, beide bestanden, alle tabellen,
links werken; een niveauknop dimt de andere sleutels; een klik op een sleutel opent de drawer
met `about`, `Required`, `Without it`, `Read by` en `Status: works`, Escape sluit hem; licht en donker.

- [ ] **Stap 6: Commit**

```bash
# docs/site geheel: site_version herschrijft de ?v=-tokens in alle pagina's en scripts.
git add docs/site docs/getting-started.md tests/test_site_pages.py tests/test_site_sorts.py \
  changelog.d/issue-104-configuratiekaart.md
git commit -m "feat(site): configuratiekaart als kaart 6, gelinkt vanaf de index (#104)"
```
