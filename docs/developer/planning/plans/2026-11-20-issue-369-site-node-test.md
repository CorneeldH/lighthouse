# Site-tests naar `node --test` — implementatieplan (#369)

> **Voor agentische uitvoerders:** VERPLICHTE SUB-SKILL: gebruik superpowers:subagent-driven-development
> (aanbevolen) of superpowers:executing-plans om dit plan taak voor taak uit te voeren. Stappen gebruiken
> checkboxes (`- [ ]`).

**Doel:** de 12 `tests/test_site_*.py` plus `tests/_site_node.py` en `tests/site_version.py` worden na
triage `node --test`-tests onder `docs/site/test/`, zonder `package.json` en zonder afhankelijkheden, en
de site is lokaal te bekijken zonder Python.

**Architectuur:** de tests importeren `docs/site/map.js` en `docs/site/platform.js` rechtstreeks (ESM;
geen subprocess meer). Gedeelde helpers staan in `docs/site/test/helpers.mjs`, het cache-bust-gereedschap
in `docs/site/test/version.mjs`. Alleen de configuratiekaart heeft de Go-binary nodig (`lighthouse config
--schema` en `lighthouse config <key>`); die test bouwt hem zelf als `LIGHTHOUSE_BIN` ontbreekt. Een
nieuw Go-programma `tools/servedir` serveert een map lokaal. Een nieuwe CI-job `site` draait de tests op
elke PR.

**Tech stack:** Node 22 (`web/.nvmrc`: 22.22.2), `node:test`, `node:assert/strict`, `node:fs`,
`node:crypto`, `node:child_process`; Go (stdlib `net/http`) voor `tools/servedir`.

**Spec:** [`2026-11-16-repo-zonder-python-design.md`](../specs/2026-11-16-repo-zonder-python-design.md)
§1 (triage) en §5 (site-tests). Issue: #369.

## Globale randvoorwaarden

- Geen `package.json`, geen npm-afhankelijkheid, geen externe HTML-parser (spec §5).
- Elke test buiten `tests/pack/python/` krijgt één bestemming: weg (onderwerp weg / gedekt / beschermt
  niets), naar Go of naar `node --test` (spec §1). Een test bestaat na deze PR niet meer in pytest én
  in Node.
- Een test die je niet kunt laten falen met een mutatie van productiecode of -data, port je niet (spec,
  *Aan de bouwer*). Per geporte test staat in de PR één mutatie van `docs/site/*` (of van de data/bron
  die hij bewaakt) waarop hij faalt.
- Bestaande `CHANGELOG.md`-regels en oude plannen blijven staan (No-Go).
- Geen `.go`-bestand noemt `docs/` (`tests/test_domains.py:127`, straks `TestGoSourcesDoNotReadNotGoPaths`
  van #361): `tools/servedir` krijgt de map als argument, en bijgewerkte Go-commentaren noemen geen pad
  onder `docs/`.
- Lees `pack/core/skills/test-standard/SKILL.md` vóór de eerste test; schrijf per taak de
  classificatiezin in de chat.

## Review-focus

1. **Ontdekking.** `node --test docs/site/` faalt op Node 22.22.2: een maparg wordt als bestand gelezen
   (`not ok 1 - site`, gemeten). Het werkende commando is `node --test "docs/site/test/*.test.mjs"`
   (glob, Node ≥ 21). Een test die niet ontdekt wordt, draait stil niet: Taak 8 telt de tests.
2. **HTML-entiteiten.** De installatieblokken in `install.html` bevatten `&amp;&amp;`. Python's
   `HTMLParser` decodeerde die; de Node-extractie moet `&amp; &lt; &gt; &quot; &#39;` decoderen, anders
   faalt `bash_blocks_stop_on_the_first_failure` vals of slaagt hij vals. Test in Taak 5.
3. **Versietoken blijft gelijk.** `version.mjs` moet exact hetzelfde token geven als `site_version.py`
   (sha256 over naam + inhoud zonder tokens, gesorteerd op naam, eerste 8 hex). Bewijs: `node
   docs/site/test/version.mjs` op de huidige tree wijzigt geen bestand. Taak 1.
4. **Testbestanden in de linkcontrole.** `docs/site/test/*.mjs` importeren `node:test`; de port van
   `test_site_links_resolve` moet `docs/site/test/` overslaan, anders is hij rood op eigen imports. Taak 1.
5. **Binary ontbreekt.** Zonder `LIGHTHOUSE_BIN` en zonder Go faalt `config.test.mjs` met een duidelijke
   melding (geen stille skip), zoals `_site_node.py` dat met `node` deed. Taak 4.

---

## Beslissingen

**(a) `tests/test_agent_guide.py`** (bestand van #365) importeert `ROOT, SITE` uit `test_site_loop`,
`DOWNLOAD, INDEX, INSTALL, TABS` uit `test_site_download` en `ElementById` uit `test_site_pages`. #365
verwijdert `test_agent_guide.py` (plan #365, regel 200). Staat het bestand er bij uitvoering nog, dan
inline je in Taak 8 alleen die zeven namen bovenaan `test_agent_guide.py` (constanten letterlijk, de
klasse `ElementById` gekopieerd), zonder triage. Is #365 al gemerged, dan vervalt de stap.

**(b) `test_site_config.py`** (cross-tree: `git ls-files`, Go-schema, binary) gaat naar Node, niet naar
Go. Reden: een Go-test die `docs/site/data/config.json` leest, draait niet op een PR die alleen die map
raakt (`docs/` valt buiten het domein `go`, `ci/domains.py:44`), en #361 legt die regel juist vast. De
Node-test krijgt het schema van `lighthouse config --schema` en draait altijd (beslissing e).

**(c) `site_version.py`** wordt `docs/site/test/version.mjs` (zelfde algoritme, zelfde token) met CLI
`node docs/site/test/version.mjs`. De `?v=`-controle beschermt bezoekers met een oude cache tegen een
nieuwe pagina met een oude module: dat is gedrag, dus hij blijft.

**(d) Serve-commando:** `go run ./tools/servedir docs/site`, op `localhost:8000`. Elke
lighthouse-ontwikkelaar heeft Go; Node niet per se. De map is een argument, zodat de Go-bron geen `docs/`
noemt. Het commando komt in `docs/getting-started.md:205` en in de `serve-hint` van de zes kaartpagina's
(`config, loop, reference, repo, session, system.html`, regel 20-23).

**(e) CI:** een nieuwe job `site` in `ci.yml`, **zonder** domeinvoorwaarde: hij draait op elke PR
(Node-tests duren seconden; de binary-build ongeveer 30 s). Zo raakt deze PR `ci/domains.py` niet, dat
#361 naar Go port. De site-tests lezen ook `pack/`, `internal/` en `.github/workflows/build.yml`, dus een
domein "alleen `docs/site/`" zou te smal zijn. Botsing met #361: geen in `domains`; wel raken beide
`ci.yml` (andere regels). Lokaal komt de stap in `[test_gate] command` van `lighthouse.toml`.

**(f) Testbestanden in `docs/site/test/`** worden mee gepubliceerd door `pages.yml` (`path: docs/site`).
Dat is onschadelijk (statische `.mjs`, `.json`); geen wijziging aan `pages.yml`.

**(g) Checker-zelftests.** De `*_flagged`/`problems_flag_*`-tests muteren data in de test en bewijzen dat
een checker kan falen. Zonder hen kan een kapotte checker stil groen geven en bewaakt de datacontrole
niets meer. Ze gaan mee, maar per checker als één tabeltest (één `test()` met een array van
`{naam, mutatie, verwacht}`), niet als losse tests.

## Bestandsstructuur

| Pad | Verantwoordelijkheid |
|---|---|
| `docs/site/test/helpers.mjs` | `ROOT`, `SITE`, `REPO_BLOB`, `REPO_TREE`, `load(name)`, `relativeLinks(path)`, `siteFiles(...suffixes)`, `elementById(html, id)`, `decodeEntities(text)`, `lighthouseBin()` |
| `docs/site/test/version.mjs` | `siteVersion(site)`, `assetRefs(path)`, `writeVersion(site)`; CLI |
| `docs/site/test/fixtures/{map,system}.json` | verhuisd uit `tests/fixtures/site/` |
| `docs/site/test/{assets,pages,loop,packs,session,system,route-follow,sorts,reference,repo,config,download,serve}.test.mjs` | de geporte tests |
| `tools/servedir/main.go`, `tools/servedir/main_test.go` | statische server voor een map |
| `.github/workflows/ci.yml` | job `site` |
| `lighthouse.toml` | `[test_gate] command` + site-stap |
| `docs/getting-started.md`, 6× `docs/site/*.html` | nieuw serve-commando |
| `internal/initcmd/places_test.go:172`, `internal/kpi/startissue.go:463` | verwijzing naar de nieuwe test, zonder `docs/` |
| `changelog.d/issue-369-site-node-test.md` | fragment, `type: internal` |

Verwijderd: `tests/test_site_*.py` (12), `tests/_site_node.py`, `tests/site_version.py`,
`tests/fixtures/site/`.

## Triage (concept voor de PR-body)

Telling: 168 testfuncties, 228 verzamelde gevallen (`pytest --collect-only`). Geparametriseerde
gevallen die op dezelfde mutatie falen tellen als één (spec §1). N = naar `node --test`. Mutaties zijn
voorbeelden; de uitvoerder bevestigt elk door hem uit te voeren.

| Bestand (defs/gevallen) | Bestemming | Reden | Mutatie (N) |
|---|---|---|---|
| `test_site_assets` (4/4) | N: `every_local_asset_carries_the_site_version`, `version_follows_the_assets_not_the_tokens`, `asset_refs_misses_no_local_asset`. Weg — beschermt niets: `asset_refs_finds_the_map_import_in_loop` (zelfde mutatie faalt in `asset_refs_misses_no_local_asset`) | Cache-coherentie voor bezoekers | `?v=` weghalen uit één `<script src>` in `loop.html` |
| `test_site_pages` (9/19) | N: `site_links_resolve`, `pages_name_the_local_server_command` (6→1), `component_never_writes_html_from_data`, `page_lang_is_english`, `map_pages_link_back_to_index` (6→1), `index_lists_the_six_maps` (alleen links + `id="maps"`). Serve-regel van `guide_links_to_the_maps` gaat in `serve.test.mjs`. Weg — beschermt niets: `guide_links_to_the_maps` (rest), `guide_links_to_the_config_map`, `guide_links_to_the_repo_map`; de issuenummer- en "roadmap"/"chain"-asserts van `index_lists_the_six_maps` | Links die er zijn, controleert `site_links_resolve`; de rest pint woorden of is een "staat er niet meer"-check | `innerHTML` in `map.js`; `hidden` op `#serve-hint`; `lang="nl"` |
| `test_site_loop` (22/37) | N: de zes kaartcontroles over fixture + datakaarten, `site_and_roadmap_agree_on_a_planned_item` (alleen sitehelft; roadmaphelft is `TestProblems` van #361), de 8 fixture-tests (1 tabel), `works_handoffs_follow_the_handoff_table` (zonder de letterlijke set), `the_old_chain_page_forwards_to_the_loop`. Weg — beschermt niets: `phases_are_the_seven_of_the_chain`, `coverage_flags_a_removed_skill` (test een helper die geen controle gebruikt), `chain_routes_are_the_known_ones`, `the_chain_map_is_called_the_loop`, `route_labels_carry_no_gate_number` | Data-integriteit van de kaart vs `pack/`; de rest pint inhoud | onbekende `status` in `loop.json`; chip van alle routes halen |
| `test_site_config` (34/34) | N: alles behalve één; de 14 `problems_flag_*` als 1 tabel. Weg — beschermt niets: `every_table_has_a_title_and_what_it_is_for` | Kaart vs Go-schema en lezers; JS van de configuratiepagina | sleutel uit `config.json` halen; default in `config.json` wijzigen; padding in `configToml` |
| `test_site_download` (13/30) | N: `choose_platform` (13→1), `release_label` (6→1), `site_assets_are_the_built_assets`, `every_platform_has_an_install_block`, `checksum_comes_before_the_binary_is_used`, `bash_blocks_stop_on_the_first_failure`, `windows_block_works_in_windows_powershell`, `install_page_works_without_js`, `install_page_names_the_other_routes`. Weg — beschermt niets: `install_page_title`, `footer_links`, `landing_page_puts_the_why_first`, `old_download_link_redirects_to_the_install_page` (leest de bron) | Platformkeuze en veilige installatieblokken | `MacIntel` → `null` in `choosePlatform`; `&&` weg in een blok |
| `test_site_packs` (6/6) | N: alle 6 (zonder `len(core["hooks"]) == 10`) | `packs.json` vs `pack/` | hook toevoegen aan `hooks.json` zonder `packs.json` |
| `test_site_reference` (5/5) | N: 4. Weg — beschermt niets: `reference_page_has_the_four_anchors` (de linkcontroles van `repo`/`system` volgen `reference.html#skills` enz.) | Referentie vs `pack/` | `referenceEntries` laat agents weg |
| `test_site_repo` (28/28) | N: 5 kaartcontroles, 9 mutatietests (1 tabel), 9 `repoEntries`-tests. Weg — beschermt niets: `links_have_an_accessible_name`, `workstation_heading_is_beside_the_repo_not_under_it` (lezen de bron), `page_has_no_legend_or_panel_and_explains_below`, `notes_say_only_the_first_init_proposes_host_toml`, `notes_count_the_manifest_per_unit_not_per_file` (proza) | Kaart vs `places.golden`; JS van de repopagina | rij uit `repo.json`; `"unknown mode"`-throw weg |
| `test_site_route_follow` (7/8) | N: 6. Weg — beschermt niets: `commit_test_gate_on_the_session_map_switches_to_red_tests` | `routeWith` | `routeWith` negeert de volgorde |
| `test_site_sorts` (6/19) | N: 5 (`glyph_follows_the_sort_not_the_trigger` 12→1). Weg — beschermt niets: `how_it_starts_legend_is_gone` (negatief na verwijdering) | Sortentabel, thema's | glyph in `SORTS` wijzigen; token uit één thema in `map.css` |
| `test_site_session` (15/15) | N: `hook_entries_reads_every_pack`, `every_hook_is_on_its_event`, 7 mutatietests (1 tabel), `routes_light_up_the_most_hooks_first`. Weg — beschermt niets: `phases_are_the_moments_of_a_turn`, `writing_skills_load_at_the_skill_call`, `the_agent_runs_at_the_agent_call_not_on_the_loop`, `routes_are_the_situations`, `session_why_names_the_loop_not_the_chain` | `session.json` vs `hooks.json` | matcher van een chip wijzigen |
| `test_site_system` (19/23) | N: 4 controles (fixture + echt), 12 fixture-tests (1 tabel). Weg — beschermt niets: `init_step_is_the_command_in_the_repo`, `init_step_links_to_the_repo_map`, `bring_groups_in_order` | `system.json` houdt samen | `href` naar niet-bestaande pagina |

Totaal: 168 → naar Node 138 functies, na tabelvorming ongeveer 93 `test()`-aanroepen; weg 30
(beschermt niets 30, gedekt 0); naar Go 0.

---

### Taak 1: helpers, versiegereedschap, assets en pagina's

**Bestanden:**
- Aanmaken: `docs/site/test/helpers.mjs`, `docs/site/test/version.mjs`, `docs/site/test/assets.test.mjs`, `docs/site/test/pages.test.mjs`
- Verwijderen: `tests/test_site_assets.py`, `tests/site_version.py`
- Lezen (bron): `tests/test_site_pages.py`, `tests/test_site_loop.py:14-17,35-37`, `tests/site_version.py`

**Interfaces (produceert):**
- `ROOT: string` (repo-root), `SITE: string` (`<ROOT>/docs/site`), `REPO_BLOB = "https://github.com/CorneeldH/lighthouse/blob/main/"`, `REPO_TREE = ".../tree/main/"`
- `load(name: string): any` — JSON uit `docs/site/data/<name>`
- `relativeLinks(path: string): string[]` — zelfde regels als `tests/test_site_pages.py:27-48` (`href/src/data-src` in `.html`, `import … from`, kale `import`, `loadJson(...)` in `.js/.mjs`, `](...)` in `.md`; extern en `#` overslaan; fragment en query eraf)
- `siteFiles(...suffixes: string[]): string[]` — recursief onder `SITE`, **zonder** `SITE/test/`, gesorteerd
- `elementById(html: string, id: string): {attrs: Record<string,string|null>, text: string} | null` — eerste element met dat id; tekst van alle geneste elementen; diepte tellen op start/eindtags van dezelfde tagnaam; tekst door `decodeEntities`
- `decodeEntities(text: string): string` — `&amp; &lt; &gt; &quot; &#39;` en numerieke `&#NN;`
- `siteVersion(site = SITE): string`, `assetRefs(path: string): string[]`, `writeVersion(site = SITE): string[]` (gewijzigde paden); regexes letterlijk uit `tests/site_version.py:12-14`

- [ ] **Stap 1:** classificatiezin in de chat (`test-only` + `io/filesystem` voor `writeVersion`).
- [ ] **Stap 2:** schrijf `helpers.mjs` en `version.mjs`. `version.mjs` draait `writeVersion()` en print elk pad als `import.meta.url === pathToFileURL(process.argv[1]).href`.
- [ ] **Stap 3:** draai `node docs/site/test/version.mjs && git status --porcelain docs/site`. Verwacht: geen uitvoer (zelfde token als Python).
- [ ] **Stap 4:** schrijf `assets.test.mjs` (de drie N-tests uit de tabel; de tweede op een kopie in `fs.mkdtempSync`) en `pages.test.mjs` (de zes N-tests; `pages_name_the_local_server_command` en `map_pages_link_back_to_index` als lus over de zes pagina's binnen één `test()`; de serve-assert wacht op Taak 7 en gebruikt hier nog de oude string `python3 -m http.server --directory docs/site`).
- [ ] **Stap 5:** `node --test "docs/site/test/*.test.mjs"`. Verwacht: PASS.
- [ ] **Stap 6:** mutatiecontrole per test uit de tabel (o.a. `?v=` weg in `loop.html`, `innerHTML` in `map.js`, `hidden` op `#serve-hint`, `lang="nl"`). Elk: FAIL; terugzetten.
- [ ] **Stap 7:** verwijder `tests/test_site_assets.py` en `tests/site_version.py`; zet in de commentaarkop van `version.mjs` de instructie "na een wijziging aan een `.js`/`.css`: `node docs/site/test/version.mjs`".
- [ ] **Stap 8:** commit `test(site): helpers, versietoken en paginatests naar node --test (#369)`.

### Taak 2: kaarten loop, packs, session, system, route-follow

**Bestanden:**
- Aanmaken: `docs/site/test/{loop,packs,session,system,route-follow}.test.mjs`
- Verplaatsen: `tests/fixtures/site/{map,system}.json` → `docs/site/test/fixtures/`
- Verwijderen: `tests/test_site_packs.py`, `tests/test_site_session.py`, `tests/test_site_system.py`, `tests/test_site_route_follow.py` (`test_site_loop.py` pas in Taak 8: anderen importeren hem)

**Interfaces:** gebruikt `helpers.mjs`; importeert `../map.js` als `m` (`m.routeWith`, `m.SORTS`).

- [ ] **Stap 1:** classificatiezin.
- [ ] **Stap 2:** port de checkers als lokale functies met dezelfde namen in camelCase (`vocabularyProblems`, `sourceProblems`, `plannedProblems`, `routeProblems`, `handoffProblems`, `coverageProblems`, `hookProblems`, `shapeProblems`, `sortProblems`, `linkProblems`, `wordProblems`) en dezelfde letterlijke meldingen. `MAPS` = fixture + elke `docs/site/data/*.json` met `chips`. De agree-test: vier gevallen tegen `plannedProblems` alleen. `works_handoffs`: zonder de letterlijke set; wel "works-handoff heeft geen issue", de controle op de tabel in `pack/core/skills/write-handoff/SKILL.md` en `internal/dispatch/judge.go`, en "alle statussen zijn works of planned". `packs`: tomllib is er niet; `detect.toml` leest de test met een minimale parser die alleen `files = [...]` en `[[contains]]`-blokken met `files`/`text` kent en op elke andere regel faalt (geen stille negering).
- [ ] **Stap 3:** de mutatietests als één tabeltest per checker (beslissing g), met de meldingen letterlijk uit de Python-tests.
- [ ] **Stap 4:** `node --test "docs/site/test/*.test.mjs"`. Verwacht: PASS.
- [ ] **Stap 5:** mutatiecontrole: onbekende `status` in `loop.json`; matcher van `worktree-check` in `session.json`; hook toevoegen aan `pack/core/hooks/hooks.json` zonder `packs.json`; `routeWith` die de volgorde negeert; `href` naar `nowhere.html` in `system.json`. Elk: FAIL; terugzetten.
- [ ] **Stap 6:** verwijder de vier Python-bestanden en `tests/fixtures/site/`.
- [ ] **Stap 7:** commit `test(site): kaarttests loop, packs, session, system en route-follow naar node --test (#369)`.

### Taak 3: sorts, reference, repo

**Bestanden:**
- Aanmaken: `docs/site/test/{sorts,reference,repo}.test.mjs`
- Verwijderen: `tests/test_site_sorts.py`, `tests/test_site_reference.py`, `tests/test_site_repo.py`

- [ ] **Stap 1:** classificatiezin.
- [ ] **Stap 2:** port de N-tests uit de tabel. `repo`: `parse_golden` leest `internal/initcmd/testdata/places.golden` (tab-gescheiden); de weigeringen via `assert.throws(() => m.repoEntries(data, mode), {message: "unknown mode: upgrade"})`. `sorts`: `kindColour(css, cls)` zoals `tests/test_site_sorts.py:72-80`.
- [ ] **Stap 3:** `node --test "docs/site/test/*.test.mjs"`. Verwacht: PASS.
- [ ] **Stap 4:** mutatiecontrole: glyph van `agent` in `SORTS`; `--rules:` uit één thema in `map.css`; `referenceEntries` laat `agents` weg; rij `claude-agents` uit `repo.json`; de `unknown act`-throw in `repoEntries` weg. Elk: FAIL; terugzetten.
- [ ] **Stap 5:** verwijder de drie Python-bestanden.
- [ ] **Stap 6:** commit `test(site): sorts, reference en repo naar node --test (#369)`.

### Taak 4: configuratiekaart

**Bestanden:**
- Aanmaken: `docs/site/test/config.test.mjs`
- Wijzigen: `docs/site/test/helpers.mjs` (`lighthouseBin`)
- Verwijderen: `tests/test_site_config.py`

**Interfaces (produceert):** `lighthouseBin(): string` — `process.env.LIGHTHOUSE_BIN` als die gezet en niet leeg is (een lege waarde telt als niet gezet);
anders één keer `go build -o <mkdtemp>/lighthouse ./cmd/lighthouse` in `ROOT`; faalt met
`"lighthouse binary needed: set LIGHTHOUSE_BIN or install Go (no silent skip)"` als dat niet lukt.

- [ ] **Stap 1:** classificatiezin (`config` + `cli-output`).
- [ ] **Stap 2:** port `config_problems` en zijn helpers. Het schema komt uit `JSON.parse(execFileSync(lighthouseBin(), ["config", "--schema"]))` (velden `schema`, `defaults`, `base_keys`, `host_keys`, `option_prefixes`, `languages`). `usage_defaults` blijft de regex op `internal/usage/config.go` (`tests/test_site_config.py:68-80`), met zijn test op de letterlijke waarden 85/95/300. `repo_files` via `git ls-files pack internal cmd`. `NOT_READERS`, `INDIRECT_READERS`, `UNREAD`, `SET`, `REQUIRED` letterlijk. De twee monkeypatch-tests worden rijen in de tabeltest die een kopie van de uitzonderingsmap doorgeven: `configProblems(data, root, {notReaders, indirectReaders})`.
- [ ] **Stap 3:** `toml_block_sets_exactly_the_defaults`: geen TOML-bibliotheek. Parse de uitvoer van `m.configToml` met een parser voor precies dit subset: `[a.b]`-koppen, `# …`-commentaar, lege regels, `key = <waarde>` waarin `<waarde>` met `JSON.parse` leest (strings, getallen, booleans, string-arrays zijn geldige JSON). Een andere regel is een fout. Precies: trim elke regel; begint hij na het trimmen met `#`, dan is het commentaar (ook de ingesprongen vervolgregels van `configToml`); bij een `key = waarde`-regel haal je eerst een afsluitend `\s+#.*$` weg dat buiten een string valt, daarna `JSON.parse`. Inline tabellen (`{ P0 = … }`) staan alleen op uitgecommentarieerde regels.
- [ ] **Stap 4:** `toml_examples_pass_the_config_reader`: schrijf de ongecommentarieerde tekst naar `mkdtemp`, draai `lighthouseBin() config <key>` met `LIGHTHOUSE_TOML`/`LIGHTHOUSE_HOST_TOML`; verwachte uitvoer letterlijk uit `tests/test_site_config.py:461-475`.
- [ ] **Stap 5:** `node --test "docs/site/test/*.test.mjs"`. Verwacht: PASS. Zonder `LIGHTHOUSE_BIN` duurt de eerste run langer (build).
- [ ] **Stap 6:** mutatiecontrole: `test_gate.slots` uit `config.json`; default van `labels.needs_hands` in `config.json`; een sleutel toevoegen aan `internal/config` zonder de kaart (of de `--schema`-uitvoer in gedachten); padding in `configToml`; `isDefault` omdraaien in `configEntries`. Elk: FAIL; terugzetten.
- [ ] **Stap 7:** verwijder `tests/test_site_config.py`; zet `internal/kpi/startissue.go:463` op `// readers: the config-map site test (config.test.mjs) finds them by these literals` (geen `docs/`).
- [ ] **Stap 8:** `go vet ./internal/kpi/ && go test ./internal/kpi/` PASS; commit `test(site): configuratiekaart naar node --test (#369)`.

### Taak 5: download en installatie

**Bestanden:**
- Aanmaken: `docs/site/test/download.test.mjs`
- Verwijderen: geen (`test_site_download.py` pas in Taak 8: `test_agent_guide.py` importeert hem)

- [ ] **Stap 1:** classificatiezin (`pure-logic` voor `choosePlatform`/`releaseLabel`).
- [ ] **Stap 2:** `import { choosePlatform, releaseLabel } from "../platform.js"`. De 13 en 6 gevallen letterlijk uit `tests/test_site_download.py:50-81`, elk als tabelrij. `installBlocks()`: per `<details … data-platform="X">` de tekst van `<pre><code>…</code></pre>`, tags eraf, door `decodeEntities`. `builtAssets()`: regex `goos:\s*(\w+)\s*\n\s*goarch:\s*(\w+)` op `.github/workflows/build.yml`.
- [ ] **Stap 3:** voeg een test toe voor review-focus 2: het `linux-amd64`-blok bevat een regel die eindigt op `&&` na decodering, en geen `&amp;`.
- [ ] **Stap 4:** `node --test "docs/site/test/*.test.mjs"`. Verwacht: PASS.
- [ ] **Stap 5:** mutatiecontrole: `MacIntel` geeft `null` in `choosePlatform`; `releaseLabel` accepteert een getal; `&&` weg in het `linux-arm64`-blok; `-UseBasicParsing` weg; `darwin/amd64` uit `build.yml`. Elk: FAIL; terugzetten.
- [ ] **Stap 6:** commit `test(site): download en installatieblokken naar node --test (#369)`.

### Taak 6: `tools/servedir`

**Bestanden:**
- Aanmaken: `tools/servedir/main.go`, `tools/servedir/main_test.go`

**Interfaces (produceert):**
- `func handler(dir string) http.Handler` — `http.FileServer(http.Dir(dir))`
- `func parseArgs(args []string) (dir, addr string, err error)` — precies één map verplicht; vlag `-addr` met standaard `localhost:8000`
- `func run(args []string, stdout, stderr io.Writer, serve func(addr string, h http.Handler) error) int` — 2 bij een fout in de argumenten of een map die niet bestaat; print `serving <dir> at http://<addr>/` op stdout

- [ ] **Stap 1:** classificatiezin (`cli-output` + `io/filesystem`). Lees `/lighthouse-go-standard`.
- [ ] **Stap 2:** schrijf de falende tests: `TestHandlerServesAFile` (tempdir met `loop.html`, `httptest`, 200 en de inhoud), `TestHandlerMissingFileIs404`, `TestRunWithoutDirExits2` (stderr bevat `usage`), `TestRunWithMissingDirExits2`, `TestRunPrintsTheURL` (fake `serve` die nil geeft; stdout `serving <dir> at http://localhost:8000/`).
- [ ] **Stap 3:** `go test ./tools/servedir/`. Verwacht: FAIL (niet gedefinieerd).
- [ ] **Stap 4:** implementeer; `main` roept `os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, http.ListenAndServe))`. Geen `docs/` in de bron.
- [ ] **Stap 5:** `go test -race ./tools/servedir/ && go vet ./tools/servedir/`. Verwacht: PASS. Daarna met de hand: `go run ./tools/servedir docs/site` en `curl -sI http://localhost:8000/loop.html` geeft `200`.
- [ ] **Stap 6:** mutatiecontrole: standaardadres `:8080`; geen controle op een ontbrekende map. Elk: FAIL; terugzetten.
- [ ] **Stap 7:** commit `feat(tools): servedir serveert een map lokaal (#369)`.

### Taak 7: serve-commando in docs en pagina's

**Bestanden:**
- Wijzigen: `docs/getting-started.md:205-206`, `docs/site/{config,loop,reference,repo,session,system}.html` (`serve-hint`)
- Aanmaken: `docs/site/test/serve.test.mjs`; wijzigen: `docs/site/test/pages.test.mjs` (serve-assert eruit, naar `serve.test.mjs`)

- [ ] **Stap 1:** schrijf `serve.test.mjs` met `SERVE_COMMAND = "go run ./tools/servedir docs/site"`: elke kaartpagina heeft `#serve-hint`, zichtbaar (geen `hidden`, geen `display:none`), met het commando in de witruimte-genormaliseerde tekst; de sectie `## Your first issue` van `docs/getting-started.md` bevat het commando.
- [ ] **Stap 2:** `node --test "docs/site/test/*.test.mjs"`. Verwacht: FAIL (oude tekst).
- [ ] **Stap 3:** vervang in de zes pagina's `<code>python3 -m http.server --directory docs/site</code>` door `<code>go run ./tools/servedir docs/site</code>` en in de gids de zin op regel 205 door "run `go run ./tools/servedir docs/site` from its root". `http://localhost:8000/…` blijft.
- [ ] **Stap 4:** `node --test "docs/site/test/*.test.mjs"`. Verwacht: PASS. `grep -n "http.server" docs/getting-started.md docs/site/*.html` is leeg.
- [ ] **Stap 5:** commit `docs(site): de site bekijken zonder Python (#369)`.

### Taak 8: CI, test-gate, opruimen

**Bestanden:**
- Wijzigen: `.github/workflows/ci.yml` (job `site`), `lighthouse.toml` (`[test_gate] command`), `internal/initcmd/places_test.go:172`, `tests/test_agent_guide.py:10-12` (alleen als hij nog bestaat, beslissing a)
- Verwijderen: `tests/test_site_loop.py`, `tests/test_site_pages.py`, `tests/test_site_download.py`, `tests/_site_node.py`
- Aanmaken: `changelog.d/issue-369-site-node-test.md`

- [ ] **Stap 1:** job `site` in `ci.yml`, na `no-python`, in de vorm van die job: `runs-on: ubuntu-latest`, `timeout-minutes: 10`, checkout met `persist-credentials: false`, `actions/setup-go` (`go-version-file: go.mod`) en `actions/setup-node` (`node-version-file: web/.nvmrc`, geen npm-cache), SHA-pins gekopieerd van de bestaande jobs, `go build -o "$RUNNER_TEMP/lighthouse" ./cmd/lighthouse`, dan `node --test "docs/site/test/*.test.mjs"` met `env: LIGHTHOUSE_BIN: ${{ runner.temp }}/lighthouse`. Geen `needs`/`if` (beslissing e); een commentaarregel erboven zegt waarom.
- [ ] **Stap 2:** `lighthouse.toml`: voeg aan het eind van `[test_gate] command` een stap toe die alleen draait als de commit `docs/site/`, `pack/`, `internal/`, `cmd/`, `tools/` of `.github/workflows/build.yml` raakt (lees `$LIGHTHOUSE_GATE_FILES`, `grep -q`), en die bij een ontbrekende `node` met exit 3 stopt (`command -v node >/dev/null || exit 3`, dat `unchecked_exit_codes` dekt), anders `node --test \"docs/site/test/*.test.mjs\"`; één zin aan het commentaar erboven. Het segment, letterlijk, achter de bestaande `&&`-keten: ``&& { [ -n "$LIGHTHOUSE_GATE_FILES" ] && ! grep -qE '^(docs/site/|pack/|internal/|cmd/|tools/|\.github/workflows/build\.yml)' "$LIGHTHOUSE_GATE_FILES" || { command -v node >/dev/null || exit 3; node --test "docs/site/test/*.test.mjs"; }; }``. Drie gevallen: geen treffer geeft 0 (stap overgeslagen); variabele leeg (pre-commit op de default branch) draait de stap; ontbrekende `node` geeft 3. Werk de verwachte string in `tests/test_dogfood.py:187-193` (`test_test_gate_uses_selectors`) bij naar het nieuwe commando, met precies dit segment.
- [ ] **Stap 3:** `tests/test_agent_guide.py`: alleen als hij bestaat, de zeven namen inlinen (beslissing a). `uv run pytest -q tests/test_agent_guide.py` PASS.
- [ ] **Stap 4:** verwijder de vier resterende Python-bestanden. `internal/initcmd/places_test.go:172`: `(tests/test_site_repo.py)` wordt `(repo.test.mjs of the site tests)`.
- [ ] **Stap 5:** fragment `changelog.d/issue-369-site-node-test.md`:
  ```markdown
  ---
  type: internal
  ---
  De site-tests draaien met `node --test` naast `docs/site/`, en `go run ./tools/servedir docs/site` serveert de site lokaal zonder Python.
  ```
- [ ] **Stap 6:** verificatie, alle vier groen:
  - `LIGHTHOUSE_BIN= node --test "docs/site/test/*.test.mjs"`; de `# tests`-regel telt minstens 90.
  - `uv run pytest -q tests/test_ci_domains_pin.py tests/test_domains.py tests/test_dogfood.py` (job `site` breekt geen pin: `test_pr_runs_no_macos_job` wil `ubuntu-latest`).
  - `go test ./internal/initcmd/ ./internal/kpi/ ./tools/servedir/`
  - `git ls-files 'tests/test_site_*' tests/_site_node.py tests/site_version.py tests/fixtures/site` is leeg; `grep -rn "test_site_\|_site_node\|site_version" --include=*.py --include=*.go --include=*.toml --include=*.yml . | grep -v planning/` is leeg, op `tests/test_domains.py:240` na (een synthetisch pad voor `classify`, geen verwijzing; blijft staan).
- [ ] **Stap 7:** commit `ci(site): job site draait node --test, pytest-sitetests weg (#369)`.

## Afhankelijkheden en botsingen

- **#361** wijzigt `tests/test_site_loop.py` (roadmap-import weg). Deze PR verwijdert het bestand. Bij
  een merge: verwijderen wint. De roadmaphelft van de agree-test is daar `TestProblems`.
- **#365** verwijdert `tests/test_agent_guide.py` en `tests/test_getting_started.py`; zie beslissing a.
  #365 test `docs/getting-started.md` in Go (`internal/repopins/docs_test.go`); controleer na een merge
  dat de nieuwe serve-zin daar niet botst.
- **#370** maakt de `python`-job smaller; de job `site` hangt daar niet van af.
- In een controller-run pusht de workflow-pusher de `ci.yml`-wijziging (`workflow-push-gate`).

## Open punten voor de gebruiker

1. **AC "`node --test docs/site/` is groen".** Op Node 22 werkt een maparg niet (gemeten). Het plan
   gebruikt `node --test "docs/site/test/*.test.mjs"`; de PR noemt dat als afwijking.
2. **AC "`docs/getting-started.md` noemt geen `python3` meer".** Regel 17 noemt `python3` als eis van
   het `python`-pakket (dat blijft Python, No-Go). Het plan laat die regel staan en haalt alleen het
   serve-commando weg. Wil je de regel herschreven, zeg het vóór Taak 7.
3. **Checker-zelftests als tabel (beslissing g)** in plaats van weg: 50 losse tests (loop 8, config 14, repo 9, session 7, system 12) worden 5 tabellen.
