# Plattegrond van je repo na `lighthouse init` (#101) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Kaart 3 van de site: een plattegrond van de repo van de gebruiker na `lighthouse init`,
met per gebied eigenaar, pakketten, lezers en wat `--upgrade` doet, bewaakt door een Go-test die
een golden file schrijft en een Python-test die kaart en golden vergelijkt.

**Architecture:** Een Go-test in `internal/initcmd` draait een echte `init` met alle pakketten in
een lege repo en vat elk geschreven pad samen tot een plaatssleutel in
`internal/initcmd/testdata/places.golden` (Go mag `docs/` niet lezen). Handgeschreven data in
`docs/site/data/repo.json`; `tests/test_site_repo.py` eist dat elke regel van de golden precies één
gebied is, met dezelfde pakketten en een eigenaar die bij de soorten past. In `docs/site/map.js`
komen `repoReader`, `repoEntries` en `renderRepo`: kamers als geneste `section`s in een CSS-grid,
gebieden als `button`, zoneknoppen die dimmen zoals de routeknoppen, en het paneel via
`drawerFor`. Stap 2 van de systeemkaart en de index linken naar `repo.html`.

**Tech Stack:** Go (`testing`, golden file met `-update`), vanilla JS (ES-modules, geen
buildstap), JSON, pytest, node (via `tests/_site_node.py`).

**Spec:** [`docs/developer/planning/specs/2026-11-01-plattegrond-na-init-design.md`](../specs/2026-11-01-plattegrond-na-init-design.md)
(versie 2, commit `19bc83b`).

## Global Constraints

- **Poort (AC 1).** Bouwen begint pas als Corneel de schets van versie 2 van de spec heeft
  goedgekeurd met een comment in #101 (spec §3). Controleer dat vóór Taak 1:
  `gh issue view 101 --json comments -q '.comments[] | select(.author.login=="CorneeldH") | .body'`
  moet een goedkeuring bevatten die ná de spec-comment van versie 2
  (`#issuecomment-6556576136`) staat. Staat die er niet: stop, meld BLOCKED, bouw niets.
- Sitepagina's en data zijn Engels (`<html lang="en">`); plan, commits en PR zijn Nederlands.
- Data alleen via `textContent` en attributen, nooit HTML (`test_component_never_writes_html_from_data`).
- Na elke wijziging aan site-JS of -CSS: `python3 -m tests.site_version` (`tests/test_site_assets.py`).
- Geen `.go`-bestand noemt `docs/`, `changelog.d` of `ci/literal-` (`tests/test_domains.py:92`);
  ook niet in een foutmelding of commentaar.
- Geen wijziging onder `pack/` en geen wijziging aan wat `init` schrijft: de dogfood-regel
  (`init --upgrade`) geldt niet. Alleen `_test.go` en `testdata/` onder `internal/initcmd/`.
- Geen nieuwe kleuren: de tokens van `map.css` (`--lighthouse`, `--lighthouse-fg`, `--panel`,
  `--border`, `--muted`, `--focus`).
- Eigenaren: `lighthouse` | `shared` | `yours`. Kamers: `root`, `.claude`, `.agents`,
  `.lighthouse`, `.github`, `yours`, `workstation`.
- Vaste lezernamen: `Claude Code`, `Codex`, `OpenCode`, `GitHub`, `GitHub Actions`.
- Legenda en zoneknoppen exact uit de schets (spec §3): zones `All`, `Lighthouse's`, `Shared`,
  `Yours`, `--upgrade touches`; legendaregels `lighthouse's: --upgrade replaces it`,
  `shared: only lighthouse's block or keys`, `yours: init leaves it alone`,
  `dashed line: the zone --upgrade touches`, en
  `changed or deleted a file, block or hook of lighthouse's? --upgrade stops and writes nothing until you pass --force`.
- Eén fragment `changelog.d/issue-101-plattegrond-na-init.md`, `type: added`, Engels.

## Beslissingen bij het plannen

- **Gebied-id's** (de mutatietests noemen ze): `agents-md`, `claude-md`, `lighthouse-toml`,
  `claude-hooks`, `claude-skills`, `agents-skills`, `claude-agents`, `claude-settings`,
  `lighthouse-lib`, `lighthouse-scripts`, `headings`, `manifest`, `pr-template`, `workflows`,
  `host-toml`.
- **`label` = `place`** voor elk gebied met een `place` (het pad zoals de gebruiker het ziet is de
  plaatssleutel). `host-toml` heeft `label` `~/.config/lighthouse/host.toml`.
- **`room` volgt uit `place`**: het eerste padsegment (`.claude`, `.agents`, `.lighthouse`,
  `.github`), anders `root`. `host-toml` zit in `workstation`.
- **`--force` in `upgrade`.** De tekst `upgrade` van een gebied met een manifest-soort (`file`,
  `block`, `hook`, `permission`) noemt `--force` (spec §4: "Het paneel zegt dat bij elk van die
  gebieden"); die van een gebied met alleen soort `init`, of zonder `place`, niet. De test checkt
  beide richtingen.
- **`more`** wijst naar `reference.html#skills|#agents|#hooks|#github`, `config.html`, of een
  sectie van de systeemkaart: `system.html#rules` (`AGENTS.md`, `CLAUDE.md`) en
  `system.html#config` (de vier gebieden onder `.lighthouse/`). Die secties rendert `renderStory`
  uit `system.json`, dus de linktest zoekt het anker daar, en `system.js` krijgt de
  hash-scroll van `config.js:14-16`.
- **Paneel**, in deze volgorde: `about`; dan `facts` met `Owner` (het label van de eigenaar uit de
  legenda), `How init writes it` (`how`), `Comes from` (pakketten, of `lighthouse init itself`
  bij een lege lijst), `On --upgrade` (`upgrade`); dan `Read by` als lijst; dan de link `more`.
- **Gids** (kaarten-design §5: de gids linkt naar een kaart zodra die er is): de sectie
  `## Run lighthouse init` van `docs/getting-started.md` krijgt een link naar de kaart, zoals
  `## Configure lighthouse.toml` naar `config.html`.

## Review Focus

1. Een lezer die een map is (`internal/hostconfig`) krijgt een `tree`-link, een bestand een
   `blob`-link; de bouwer gokt dat niet per lezer → test op alle lezers in Taak 3.
2. Een skill met een bestand per harnas (`skills/<n>/claude/SKILL.md`) of een hook in een submap
   (`hooks/usage-gate/resume.py`) valt onder dezelfde plaatssleutel en geeft geen extra regel →
   `TestPlaceOf` in Taak 1.
3. De workflow valt stil weg als de `Lookup`-stub ontbreekt of een sleutel niet beantwoordt;
   dan zou `-update` de plek uit de golden halen en de kaart volgen → expliciete assert in Taak 1.
4. De kaart belooft een weigering met `--force` waar `--upgrade` niet weigert (`lighthouse.toml`,
   `manifest.toml`), of zwijgt erover waar hij wel weigert (anti-doel spec §2) → test in beide
   richtingen in Taak 2.
5. Op telefoonbreedte (360px) stapelen de kamers zonder horizontale scroll, en elk gebied is met
   Tab bereikbaar en sluit met Escape → handmatige check in Taak 4, stap 6.

---

### Taak 1: Go-test en golden file van de plekken

**Files:**
- Create: `internal/initcmd/places_test.go`
- Create: `internal/initcmd/testdata/places.golden` (door `-update`)

**Interfaces:**
- Consumes: de testhelpers in `internal/initcmd/initcmd_test.go`: `newRepo`, `run`, `yes`, `all`,
  `exists`; uit het pakket `loadAll`, `unified`, `configPath`, `claudeMDPath`; `manifest.Read`,
  `manifest.Path`, `packs.HookDir`, `packs.WorkflowPrefix`, `pack.Files`.
- Produces: `internal/initcmd/testdata/places.golden`, gelezen door Taak 2. Formaat: eerste regel
  `# Generated by TestPlacesGolden (internal/initcmd/places_test.go); rerun with -update.`, daarna
  per plaatssleutel één regel `<sleutel>\t<soorten>\t<pakketten>`, soorten en pakketten
  komma-gescheiden en gesorteerd (pakketten leeg mag), regels gesorteerd, afgesloten met `\n`.
  Soorten ∈ `file`, `block`, `hook`, `permission`, `init`.

- [ ] **Stap 1: Schrijf de tests**

`var updatePlaces = flag.Bool("update", false, "rewrite testdata/places.golden from TestPlacesGolden")`.

`placeOf(rel string) string` (in het testbestand):
`packs.HookDir`, `.lighthouse/lib/lighthouse_lib/`, `.lighthouse/scripts/` → die map;
`.claude/skills/lighthouse-` of `.agents/skills/lighthouse-` → die prefix plus `*/`;
`.claude/agents/lighthouse-` → `.claude/agents/lighthouse-*.md`;
`packs.WorkflowPrefix` → `.github/workflows/lighthouse-*.yml`; al het andere `rel`.

```go
func TestPlaceOf(t *testing.T) {
	for rel, want := range map[string]string{
		".claude/skills/lighthouse-go-standard/SKILL.md":       ".claude/skills/lighthouse-*/",
		".agents/skills/lighthouse-close-issue/refs/a.md":      ".agents/skills/lighthouse-*/",
		".claude/hooks/lighthouse/usage-gate/resume.py":        ".claude/hooks/lighthouse/",
		".lighthouse/lib/lighthouse_lib/config.py":             ".lighthouse/lib/lighthouse_lib/",
		".lighthouse/scripts/quality/ratchet.py":               ".lighthouse/scripts/",
		".claude/agents/lighthouse-issue-impact-scout.md":      ".claude/agents/lighthouse-*.md",
		".github/workflows/lighthouse-unblock-dependents.yml":  ".github/workflows/lighthouse-*.yml",
		".github/pull_request_template.md":                     ".github/pull_request_template.md",
		"AGENTS.md":                                            "AGENTS.md",
	} {
		if got := placeOf(rel); got != want {
			t.Errorf("placeOf(%q) = %q, want %q", rel, got, want)
		}
	}
}
```

`TestPlacesGolden`:
1. `loaded, err := loadAll(pack.Files, "dev")` (niet `all`: dat is de helper uit
   `initcmd_test.go`); `names` = de gesorteerde sleutels van `loaded`.
2. `root := newRepo(t)`; `r := run(t, root, "", all(yes, func(o *Options) { o.Packs = nil; o.AddPacks = names; o.Lookup = func(string) (string, bool, error) { return "x", true, nil } }))`;
   `r.code != 0` → `t.Fatalf` met `r.err`. (De stub: zonder hem installeert `init` geen workflow,
   spec §6, r1-f1. `"x"` maakt `instructions_word_ceiling` onmeetbaar; dat is alleen een melding.)
3. Loop met `filepath.WalkDir` over `root`, sla `.git` over; per bestand de plaatssleutel van het
   slash-pad.
4. `manifest.Read(root)`: per entry `placeOf(e.Path)` → soort `e.Kind`, pakket `e.Package`.
5. Een bestand zonder entry krijgt soort `init`. Pakketten daarvan: voor `configPath` `core` plus
   elk pakket uit `names` met `len(loaded[n].Config) > 0` (zoals `configTables`); voor
   `claudeMDPath` en `manifest.Path` geen.
6. Bouw de regels; `-update` schrijft `testdata/places.golden`, anders vergelijk met het bestand
   en geef bij verschil `t.Errorf` met `unified(want, got, "places.golden", "init")` en de melding
   `places.golden is stale: run go test ./internal/initcmd -run TestPlacesGolden -update, then bring the repo map in step (tests/test_site_repo.py)`.
7. Assert, ook met `-update`: er is een regel met sleutel `.github/workflows/lighthouse-*.yml`,
   anders `t.Fatal("no workflow installed: the Lookup stub must answer every key")`.

- [ ] **Stap 2: Draai, zie falen**

Run: `go test ./internal/initcmd -run 'TestPlaceOf|TestPlacesGolden'`
Expected: `TestPlaceOf` PASS zodra `placeOf` er is; `TestPlacesGolden` FAIL op een ontbrekend
`testdata/places.golden`.

- [ ] **Stap 3: Schrijf de golden en lees hem na**

Run: `go test ./internal/initcmd -run TestPlacesGolden -update`

Lees `places.golden` regel voor regel na tegen spec §4: 14 regels (alle gebieden behalve
`host.toml`), o.a. `.claude/settings.json\thook,permission\tcore,go,python,shell`,
`AGENTS.md\tblock\tcore,go,python,shell`, `CLAUDE.md\tinit\t`,
`lighthouse.toml\tinit\tcore,python`, `.lighthouse/manifest.toml\tinit\t`,
`.lighthouse/scripts/\tfile\tcore,go,node,python,shell`. Wijkt er een af van spec §4: stop en
meld het; pas de golden niet met de hand aan.

- [ ] **Stap 4: Draai, zie slagen; mutatiecheck**

Run: `go test ./internal/initcmd -run 'TestPlaceOf|TestPlacesGolden' && go vet ./internal/initcmd`
Expected: PASS.

Mutatie (`/lighthouse-test-standard`): haal één regel uit `places.golden` → rood met de
stale-melding; zet hem terug. Haal `o.Lookup` uit de test → rood op `no workflow installed`;
zet hem terug.

- [ ] **Stap 5: Commit**

```bash
git add internal/initcmd/places_test.go internal/initcmd/testdata/places.golden
git commit -m "test(initcmd): golden file van elke plek waar init schrijft (#101)"
```

### Taak 2: Data van de kaart en dekkingstest tegen de golden

**Files:**
- Create: `docs/site/data/repo.json`
- Create: `tests/test_site_repo.py`

**Interfaces:**
- Consumes: `internal/initcmd/testdata/places.golden` uit Taak 1; `ROOT`, `SITE`, `load` uit
  `tests.test_site_chain`.
- Produces: `repo.json` (gelezen door Taak 3 en 4):

```json
{
  "title": "Your repo after init",
  "subtitle": "Every place lighthouse init writes: whose it is, where it comes from, and what --upgrade does with it.",
  "rooms": [
    {"id": "root", "label": "root"},
    {"id": ".claude", "label": ".claude/"},
    {"id": ".agents", "label": ".agents/"},
    {"id": ".lighthouse", "label": ".lighthouse/"},
    {"id": ".github", "label": ".github/"},
    {"id": "yours", "label": "your code, your tests, …"},
    {"id": "workstation", "label": "on your workstation"}
  ],
  "areas": [
    {"id": "agents-md", "place": "AGENTS.md", "label": "AGENTS.md", "room": "root", "owner": "shared",
     "about": "…", "how": "…", "upgrade": "… --force …",
     "packs": ["core", "go", "python", "shell"], "readers": ["Claude Code", "Codex", "OpenCode"],
     "more": "system.html#rules"}
  ]
}
```

  Gebieden in de volgorde van spec §4; `host-toml` zonder `place`.
- Produces (helpers in `tests/test_site_repo.py`, door Taak 3 hergebruikt):
  `GOLDEN = ROOT / "internal/initcmd/testdata/places.golden"`,
  `parse_golden(text: str) -> dict[str, tuple[set[str], list[str]]]` (sleutel → soorten,
  pakketten; slaat `#`-regels over),
  `named_token(area: dict) -> str`, `shape_problems(data) -> list[str]`,
  `place_problems(data, golden) -> list[str]`, `reader_problems(data, root: Path) -> list[str]`,
  `link_problems(data) -> list[str]`, `upgrade_problems(data, golden) -> list[str]`; constanten
  `OWNERS`, `ROOMS`, `READER_NAMES`, `INIT_OWNERS = {"CLAUDE.md": "yours", "lighthouse.toml": "shared", ".lighthouse/manifest.toml": "lighthouse"}`,
  `MANIFEST_KINDS = {"file", "block", "hook", "permission"}`.

- [ ] **Stap 1: Schrijf de test**

In de stijl van `tests/test_site_config.py`: helpers die een gesorteerde lijst problemen geven,
mutatietests op `copy.deepcopy`. Meldingen exact:

- `shape_problems`: `<id>: unknown owner <x>`, `<id>: unknown room <x>`,
  `<id>: lacks <key>` (verplicht: `id label room owner about how upgrade packs readers more`),
  `duplicate id <id>`, `rooms are <lijst>, not <ROOMS>` (exacte volgorde van Global Constraints),
  `<id>: label <x>, place <y>` (label ≠ place), `<id>: room <x>, place says <y>`,
  `room yours has areas`.
- `place_problems`: `not on the map: <sleutel>` (golden, geen gebied), `not in places.golden: <place>`,
  `<place>: on <n> areas`, `<id>: packs [a, b], places.golden says [c, d]`,
  `<id>: owner <x>, expected <y>` (alleen `file` → `lighthouse`; een van `block hook permission`
  → `shared`; `init` → `INIT_OWNERS`), `<id>: no place outside the workstation`,
  `expected one area without a place, found <n>`.
- `named_token`: basename van `place` (of van `label` zonder `place`), slash aan het eind en `*`
  weg.
- `reader_problems`: `<id>: no readers`; een lezer uit `READER_NAMES` is goed; anders een pad:
  `<id>: reader <r> outside pack/, internal/ or cmd/`, `<id>: reader <r> does not exist`,
  `<id>: reader <r> does not name <token>` (token letterlijk in het bestand, of in een bestand
  onder de map; lees met `errors="replace"`, cache per pad). Buiten of niet-bestaand geeft alleen
  die ene melding.
- `link_problems`: `more` is `page[#anker]`; `<id>: <page> points nowhere`;
  `<id>: <page> has no id <anker>` — voor `system.html` tegen de sectie-id's in
  `data/system.json`, anders `id="<anker>"` in de HTML.
- `upgrade_problems`: `<id>: upgrade does not say --force` (soorten ∩ `MANIFEST_KINDS` niet leeg),
  `<id>: upgrade names --force` (alleen `init`, of geen `place`).

```python
def golden():
    return parse_golden(GOLDEN.read_text(encoding="utf-8"))

def test_the_map_matches_the_places_init_writes():
    assert place_problems(load("repo.json"), golden()) == []

def test_shape():
    assert shape_problems(load("repo.json")) == []

def test_readers():
    assert reader_problems(load("repo.json"), ROOT) == []

def test_links():
    assert link_problems(load("repo.json")) == []

def test_upgrade_says_force_only_where_it_refuses():
    assert upgrade_problems(load("repo.json"), golden()) == []

def test_named_token():
    assert named_token({"place": ".claude/skills/lighthouse-*/"}) == "lighthouse-"
    assert named_token({"place": ".lighthouse/headings.toml"}) == "headings.toml"
    assert named_token({"label": "~/.config/lighthouse/host.toml"}) == "host.toml"
```

Mutatietests, elk met de exacte verwachte lijst:
`test_problems_flag_a_missing_area` (haal `claude-agents` weg →
`["not on the map: .claude/agents/lighthouse-*.md"]`),
`test_problems_flag_a_new_place_in_the_golden` (extra golden-regel
`.claude/commands/lighthouse-*/` met `{"file"}`, `["core"]` →
`["not on the map: .claude/commands/lighthouse-*/"]`),
`test_problems_flag_a_place_init_does_not_write` (`place` en `label` van `headings` →
`.lighthouse/cache/` → `["not in places.golden: .lighthouse/cache/", "not on the map: .lighthouse/headings.toml"]`),
`test_problems_flag_other_packs` (haal `go` uit `agents-md` →
`["agents-md: packs [core, python, shell], places.golden says [core, go, python, shell]"]`),
`test_problems_flag_a_wrong_owner` (`claude-settings` → `lighthouse` →
`["claude-settings: owner lighthouse, expected shared"]`),
`test_problems_flag_a_second_area_without_place` (haal `place` van `manifest` weg →
`["expected one area without a place, found 2", "manifest: no place outside the workstation", "not on the map: .lighthouse/manifest.toml"]`),
`test_problems_flag_a_reader_that_does_not_name_it` (lezer `internal/hostconfig` bij `headings`
→ `["headings: reader internal/hostconfig does not name headings.toml"]`),
`test_problems_flag_a_dogfood_reader` (lezers van `pr-template` →
`[".claude/skills/lighthouse-write-pr/SKILL.md"]` →
`["pr-template: reader .claude/skills/lighthouse-write-pr/SKILL.md outside pack/, internal/ or cmd/"]`),
`test_problems_flag_an_unknown_reader_name` (lezers van `claude-md` → `["Cursor"]` →
`["claude-md: reader Cursor outside pack/, internal/ or cmd/"]`),
`test_problems_flag_force_on_lighthouse_toml` (`upgrade` van `lighthouse-toml` plus
` Pass --force to overwrite.` → `["lighthouse-toml: upgrade names --force"]`),
`test_problems_flag_a_missing_force` (`upgrade` van `claude-skills` zonder `--force` →
`["claude-skills: upgrade does not say --force"]`),
`test_problems_flag_an_anchor_that_does_not_exist` (`more` van `claude-skills` →
`reference.html#nope` → `["claude-skills: reference.html has no id nope"]`).

- [ ] **Stap 2: Draai, zie falen**

Run: `uv run pytest -q tests/test_site_repo.py`
Expected: FAIL — `FileNotFoundError` op `docs/site/data/repo.json`; `test_named_token` slaagt.

- [ ] **Stap 3: Schrijf `docs/site/data/repo.json`**

Inhoud uit spec §4 (tabel en *Lezers*), elke Engelse zin (`about`, `how`, `upgrade`) gecontroleerd
tegen `buildPlan` en `planConfig` in `internal/initcmd/initcmd.go`, `fillWorkflows` in
`internal/initcmd/workflows.go` en `offerHost` in `internal/initcmd/host.go`. `packs` precies uit
de golden. Lezers: begin bij de lijst van spec §4 en vul aan per gebied; elke padlezer moet
`named_token` letterlijk bevatten (check met `grep -rlF`). Vind je voor een gebied geen lezer, stop
en meld BLOCKED; verzin er geen.

- [ ] **Stap 4: Draai, zie slagen; mutatiecheck**

Run: `uv run pytest -q tests/test_site_repo.py`
Expected: PASS.

Mutatie: zet `owner` van `pr-template` op `shared` → rood met de owner-melding; zet terug.

- [ ] **Stap 5: Commit**

```bash
git add docs/site/data/repo.json tests/test_site_repo.py
git commit -m "feat(site): data van de plattegrond na init met dekkingstest tegen de golden (#101)"
```

### Taak 3: Renderer in `map.js`

**Files:**
- Modify: `docs/site/map.js` (na `renderConfig`, rond regel 480)
- Modify: `docs/site/map.css` (na het `.config`-blok)
- Test: `tests/test_site_repo.py`

**Interfaces:**
- Consumes: `repo.json` uit Taak 2; de bestaande `el`, `box`, `facts`, `drawerFor`, `REPO_BLOB`,
  `REPO_TREE` in `map.js`.
- Produces:
  - `export const REPO_ZONES = [{id: "all", label: "All"}, {id: "lighthouse", label: "Lighthouse's"}, {id: "shared", label: "Shared"}, {id: "yours", label: "Yours"}, {id: "upgrade", label: "--upgrade touches"}]`.
  - `export function repoReader(reader) -> {name, href}`: een naam uit de vaste lijst →
    `href: null`; een pad → `name` het pad, `href` `REPO_BLOB + path` als de basename een punt
    heeft, anders `REPO_TREE + path`.
  - `export function repoEntries(data) -> [{id, label, zones, areas: [{id, label, owner, ownerLabel, zones, about, facts, readers, more}]}]`:
    puur, zonder DOM; kamers en gebieden in de volgorde van de data. `zones` van een gebied:
    `["all", owner]` plus `"upgrade"` als `owner` niet `yours` is. `zones` van een kamer: die van
    `yours` voor een kamer zonder gebieden, anders `null`. `ownerLabel` uit de legendaregels
    (Global Constraints). `facts`: `[["Owner", ownerLabel], ["How init writes it", how], ["Comes from", packs.join(", ") || "lighthouse init itself"], ["On --upgrade", upgrade]]`.
    `readers`: `repoReader` per lezer.
  - `export function renderRepo(root, data, detail)`: de zoneknoppen in een `div.routes`
    (`role="group"`, `aria-label="Zones"`, `aria-pressed`; `All` staat aan); een `p.map-hint`
    `Click an area for what it is, who owns it and what --upgrade does with it.`; een
    `div.repo-wall` met kop `your repo/` en de kamers behalve `workstation`, daaronder de kamer
    `workstation`. Kamer: `section.repo-room` met `data-room` en een `h2`; gebied:
    `button.repo-area owner-<owner>` (`type="button"`, `data-zones` spatie-gescheiden) met het
    label in een `code`; een gebied met `upgrade` in zijn zones krijgt ook klasse `touched`.
    Een zone kiezen zet `dim` op elk element met `data-zones` dat die zone niet heeft. Een klik
    opent `drawerFor(detail).show(button, [...])` met: `box(<kamerlabel> · <owner>, label, p(about))`,
    `box("", "", facts(facts))`, `box("Read by", "", ul.reference-list)` (een link per lezer met
    `href`, anders tekst), en een link `More about it` naar `more`.

- [ ] **Stap 1: Schrijf de test**

```python
def repo_entries():
    return run_map(f"m.repoEntries({json.dumps(load('repo.json'))})")

def test_upgrade_zone_is_lighthouse_and_shared():
    areas = [a for room in repo_entries() for a in room["areas"]]
    assert {a["id"] for a in areas if "upgrade" in a["zones"]} == {
        a["id"] for a in load("repo.json")["areas"] if a["owner"] in {"lighthouse", "shared"}}

def test_your_code_lights_only_in_yours():
    room = next(r for r in repo_entries() if r["id"] == "yours")
    assert room["zones"] == ["all", "yours"] and room["areas"] == []

def test_entries_say_where_it_comes_from():
    facts = {a["id"]: dict(a["facts"]) for r in repo_entries() for a in r["areas"]}
    assert facts["claude-md"]["Comes from"] == "lighthouse init itself"
    assert facts["agents-md"]["Comes from"] == "core, go, python, shell"
    assert facts["claude-settings"]["Owner"] == "shared: only lighthouse's block or keys"

def test_repo_reader_links():
    read = lambda r: run_map(f"m.repoReader({json.dumps(r)})")
    assert read("Claude Code") == {"name": "Claude Code", "href": None}
    assert read("internal/hostconfig") == {"name": "internal/hostconfig", "href": REPO_TREE + "internal/hostconfig"}
    assert read("pack/core/lib/config.py")["href"] == REPO_BLOB + "pack/core/lib/config.py"

def test_every_path_reader_links_tree_for_a_dir():
    paths = sorted({r for a in load("repo.json")["areas"] for r in a["readers"] if r not in READER_NAMES})
    links = run_map(f"{json.dumps(paths)}.map((p) => m.repoReader(p).href)")
    for path, href in zip(paths, links):
        assert href == (REPO_TREE if (ROOT / path).is_dir() else REPO_BLOB) + path
```

- [ ] **Stap 2: Draai, zie falen**

Run: `uv run pytest -q tests/test_site_repo.py -k "zone or entries or reader_links or tree"`
Expected: FAIL — `m.repoEntries is not a function`.

- [ ] **Stap 3: Implementeer `REPO_ZONES`, `repoReader`, `repoEntries`, `renderRepo`, plus CSS**

CSS: `.repo-wall` een rand met `--border` en de kop; de kamers in een CSS-grid naar het grondplan
van spec §3 (root bovenaan over de volle breedte, `.claude` eronder breed, `.agents` en
`.lighthouse` naast elkaar, `.github` en `yours` naast elkaar), selectie op `[data-room="…"]`;
onder 720px één kolom, geen horizontale scroll. `.owner-lighthouse` vol (`--lighthouse`,
`--lighthouse-fg`); `.owner-shared` gearceerd (`repeating-linear-gradient` van `--lighthouse` en
`--panel`, tekst op een `--panel`-label); `.owner-yours` alleen een rand. `.touched` een gestippelde
`outline`. `.dim` `opacity: 0.25`, `.dim.current` `opacity: 1`, zoals `.chip.dim`
(`map.css:523-535`). De zoneknoppen hergebruiken `.routes button`. Licht en donker via de tokens.

- [ ] **Stap 4: Versietokens**

Run: `python3 -m tests.site_version`

- [ ] **Stap 5: Draai, zie slagen**

Run: `uv run pytest -q tests/test_site_repo.py tests/test_site_pages.py tests/test_site_assets.py tests/test_site_sorts.py tests/test_site_config.py`
Expected: PASS.

- [ ] **Stap 6: Commit**

```bash
# docs/site geheel: site_version herschrijft de ?v=-tokens in alle pagina's en scripts.
git add docs/site tests/test_site_repo.py
git commit -m "feat(site): repoReader, repoEntries en renderRepo voor de plattegrond (#101)"
```

### Taak 4: Pagina, systeemkaart, index, gids en changelog

**Files:**
- Create: `docs/site/repo.html`, `docs/site/repo.js`
- Modify: `docs/site/data/system.json` (stap `init`), `tests/fixtures/site/system.json` (stap
  `init`), `docs/site/map.js` (`renderStory`), `docs/site/system.js`, `docs/site/map.css`
  (`.story-map`)
- Modify: `docs/site/index.html:105-109`, `docs/getting-started.md` (sectie `## Run lighthouse init`)
- Modify: `tests/test_site_pages.py`, `tests/test_site_system.py`
- Create: `changelog.d/issue-101-plattegrond-na-init.md`

**Interfaces:**
- Consumes: `renderRepo(root, data, detail)`, `initTheme`, `loadJson` uit `map.js`.
- Produces: een optioneel stapveld `map: {label, href}` in `system.json`, door `renderStory`
  onder de items van de stap gerenderd als `a.story-map`.

- [ ] **Stap 1: Pas de tests aan**

- `tests/test_site_pages.py`: `MAP_PAGES` krijgt `"repo.html"`; `test_index_lists_the_six_maps`:
  `"repo.html" in links`, `"#101"` verhuist naar de `not in text`-lijst
  (`["#100", "#101", "#102", "#104"]`); nieuwe `test_guide_links_to_the_repo_map`: in de sectie
  `## Run lighthouse init` van `GUIDE` staan `](site/repo.html)` en
  `[Your repo after init](https://corneeldh.github.io/lighthouse/repo.html)`.
- `tests/test_site_system.py`: `link_problems` controleert ook `step["map"]["href"]` van elke
  stap die een `map` heeft, met label `step["map"]["label"]`; nieuwe
  `test_init_step_links_to_the_repo_map`: `load_system(SYSTEM)["steps"][1]["map"] == {"label": "Map of your repo", "href": "repo.html"}`
  en de zes items houden hun hrefs (`reference.html#skills`, `reference.html#agents`,
  `reference.html#hooks`, `#rules`, `#config`, `reference.html#github`); mutatietest
  `test_fixture_map_pointing_nowhere_is_flagged` (de fixture-stap `init` krijgt
  `map: {label: "Map", href: "repo.html"}`; zet hem op `nowhere.html` →
  `["Map: nowhere.html points nowhere"]`).

Run: `uv run pytest -q tests/test_site_pages.py tests/test_site_system.py`
Expected: FAIL — `repo.html` bestaat niet, stap 2 heeft geen `map`, de gids linkt niet.

- [ ] **Stap 2: Schrijf de pagina**

`repo.html` volgt `config.html` voor de kop (`<title>Your repo after init</title>`, `#title`,
`#subtitle`, terug-link, themaknop, `#serve-hint` met `http://localhost:8000/repo.html`, een
`<details class="why">`) met twee alinea's: wat de kaart toont (elke plek waar `init` schrijft, en
van wie die is), en wat `--upgrade` doet (vervangt wat van lighthouse is, voegt ontbrekende
sleutels toe aan `lighthouse.toml`, en weigert de hele run bij een lokaal gewijzigd deel tot
`--force`). Dan een `section.legend` zonder `data-sorts` met de vijf legendaregels uit Global
Constraints, `<div id="repo-map"></div>` en `<aside id="detail" class="detail" aria-label="Details"></aside>`
(zoals `session.html:74`). `repo.js` volgt `session.js`: laadt `data/repo.json`, zet titel en
ondertitel, roept `renderRepo(document.getElementById("repo-map"), repo, document.getElementById("detail"))` aan.

- [ ] **Stap 3: Systeemkaart, index, gids**

- `system.json`, stap `init`: `"map": {"label": "Map of your repo", "href": "repo.html"}`.
- `renderStory`: heeft een stap een `map`, dan na de items een `a.story-map` met `label` en
  `href`, via `el`. CSS `.story-map`: een gewone link onder de vakken, tokens van `.story-where`.
- `system.js`: na `renderStory` dezelfde hash-scroll als `config.js:14-16`, zodat
  `system.html#rules` en `#config` landen.
- `index.html:105-109` wordt `<a class="tile kind-lighthouse status-works" href="repo.html">` met
  `<p class="kicker">Map 3</p>`, `<h2>Your repo after init</h2>` en
  `<p>What <code>init</code> writes, whose it is, and what <code>--upgrade</code> touches.</p>`.
- `docs/getting-started.md`, sectie `## Run lighthouse init`, na de lijst *What `init` writes*:
  `Each of these places, whose it is and what --upgrade does with it, is on [the repo map](site/repo.html) (also online: [Your repo after init](https://corneeldh.github.io/lighthouse/repo.html)).`

- [ ] **Stap 4: Versietokens en changelog**

Run: `python3 -m tests.site_version`

`changelog.d/issue-101-plattegrond-na-init.md`:

```markdown
---
type: added
---
A map of your repo after `lighthouse init` in `docs/site/repo.html` shows every place `init` writes as a room on a floor plan, with whose it is, the packages it comes from, what reads it, and what `--upgrade` does with it. A test fails when `init` writes to a place that is not on the map.
```

- [ ] **Stap 5: Draai alles**

Run: `uv run pytest -q tests/ && go test ./internal/initcmd`
Expected: PASS.

- [ ] **Stap 6: Handmatige check**

`python3 -m http.server --directory docs/site`, open `http://localhost:8000/repo.html`:
serve-hint verborgen; alle kamers en 15 gebieden; *your code, your tests, …* in de muur,
`host.toml` erbuiten; elke zoneknop dimt de rest (*Yours* licht `CLAUDE.md`, `host.toml` en de
lege kamer op; *--upgrade touches* alle volle en gearceerde gebieden); klik op een gebied opent het
paneel met `about`, `Owner`, `How init writes it`, `Comes from`, `On --upgrade`, `Read by` en de
link; Tab bereikt elk gebied, Escape sluit; licht en donker; op 360px breed stapelen de kamers
zonder horizontale scroll. Open `system.html`: stap 2 toont *Map of your repo*;
`system.html#rules` scrollt naar de sectie.

- [ ] **Stap 7: Commit**

```bash
# docs/site geheel: site_version herschrijft de ?v=-tokens in alle pagina's en scripts.
git add docs/site docs/getting-started.md tests/test_site_pages.py tests/test_site_system.py \
  tests/fixtures/site/system.json changelog.d/issue-101-plattegrond-na-init.md
git commit -m "feat(site): plattegrond na init als kaart 3, gelinkt vanaf systeemkaart en index (#101)"
```
