# CI: docs- en testwijzigingen zonder Go-jobs, pytest in drie delen (#151)

Kort ontwerp voor [#151](https://github.com/CorneeldH/lighthouse/issues/151). De metingen en de
acceptatiecriteria staan in het issue; dit document legt de gemaakte keuzes vast.

## Waarom

Een PR is pas groen als de langste job klaar is. Nu is dat `binary` (109–134 s) of `python`
(98–111 s, waarvan pytest 104 s). Het oorspronkelijke voorstel van #151 was een warme
Go-buildcache, maar die verkort de totale tijd nauwelijks. Twee goedkopere stappen doen dat wel:

1. Een PR die alleen `docs/`, `changelog.d/`, `tests/` of de losse `.md`-bestanden in de root
   raakt, start geen Go-jobs en geen `binary` meer. Dat waren 10 van de laatste 25 merges.
2. De pytest-suite draait verdeeld over drie parallelle jobs.

Doel: een PR zonder Go-wijziging is binnen ~40–45 s groen, nu ~100–134 s. Een PR met een
Go-wijziging blijft ~110–130 s: daar bepalen `binary` en `go` de duur.

## Doelen, niet-doelen, anti-doelen

- **Doel:** kortere totale tijd van een PR-run, zonder extra kosten. De repo is public, de
  runners zijn de gratis standaardrunners.
- **Niet-doel:** de Go-buildcache, `binary` (`npm ci`, Playwright) en een selectie van alleen
  de betrokken tests. Die staan als open punt in #151.
- **Anti-doel:** minder testen. Elke PR met een Python-wijziging draait nog steeds elke test,
  precies één keer. Een extra volledige run op `main` komt er niet: de PR heeft de wijziging dan
  al getest.
- **Anti-doel:** grotere runners. Die kosten geld, ook voor een public repo.

## Aanpakken

Stap 1 heeft maar één vorm. Voor stap 2 lagen er drie op tafel:

| Aanpak | Afweging | Keuze |
|---|---|---|
| **A. Eigen verdeling in `tests/conftest.py`**: elke test gaat op basis van een stabiele hash van zijn naam naar deel 1, 2 of 3 | ~15 regels, geen dependency, niets om bij te houden. Verdeelt op aantal, niet op duur; bij 3372 tests van hooguit 8,3 s valt dat dicht bij gelijk uit | **gekozen** |
| B. Plugin `pytest-split` met een gecommit `.test_durations` | beste balans, maar een extra dependency en een bestand dat veroudert | afgewezen |
| C. Splitsen per map (`tests/pack/` tegen de rest) | simpelst, maar `tests/pack/` is het grootste deel; de verdeling blijft scheef | afgewezen |

De gebruiker koos A.

## Stap 1: het Go-domein in `ci/domains.py`

- `tests/` komt buiten Go, behalve `tests/pack/fixtures/`, die `internal/protocol/header_test.go:19`
  leest. `README.md` en `CHANGELOG.md` in de root komen ook buiten Go. `python` blijft voor
  al die paden aan; `web` verandert niet.
- `tests/pack/fixtures/` telt dus nog als Go, en ook als python.
- Het commentaar bij `_NOT_GO_DIRS` noemt de nieuwe paden, met de meting op `main` waarop dat
  steunt.

**De bewaking in `tests/test_domains.py`.** `test_no_go_file_names_a_path_outside_go` zoekt nu
op substring (`docs/`, `changelog.d`, `ci/literal-`). Voor de nieuwe paden werkt dat niet. Go-tests
schrijven `tests/test_x.py` en `README.md` in tijdelijke repos (`e2e/e2e_test.go:70,319`,
`internal/initcmd/select_test.go:355`), en `internal/manifest/manifest_test.go:126` gebruikt
`.claude/hooks/lighthouse/../../../README.md` als testgeval voor een padcontrole. Geen van die plekken leest deze repo.

Een Go-test leest deze repo vanuit zijn eigen package-map, dus via `../`. `go:embed` kan niet
boven zijn module uit. De bewaking zoekt daarom in elk `.go`-bestand naar een stringliteral die
begint met `../` (een of meer keer), gevolgd door `tests/`, `README.md` of `CHANGELOG.md`. Een
treffer faalt, behalve `tests/pack/fixtures/`. Een padopbouw in losse delen
(`filepath.Join(root, "tests", …)`) ziet de bewaking niet; die beperking geldt nu ook al voor
`docs/` en staat in het commentaar van de test.

Testgevallen in `tests/test_domains.py`, die met de wijziging groen worden:

- een PR met alleen `tests/test_x.py` en `README.md`: `python=true`, `go=false`, `web=false`;
- `tests/pack/fixtures/protocol/headers.json`: `go=true`, `python=true`;
- `CHANGELOG.md`: `go=false`.

## Stap 2: pytest in drie delen

**`tests/conftest.py`** (nieuw, de eerste conftest van de repo). Een hook
`pytest_collection_modifyitems` leest de omgevingsvariabele `LIGHTHOUSE_PYTEST_SHARD` in de vorm
`<i>/<n>`, bijvoorbeeld `2/3`.

- Niet gezet: alle tests draaien. Lokaal en in de test gate verandert er niets.
- Gezet: een test hoort bij deel `zlib.crc32(nodeid) % n + 1`. De rest gaat via
  `config.hook.pytest_deselected` uit de run.
  - `crc32` en niet `hash()`: `hash()` van een string verschilt per proces, en dan zouden de
    xdist-workers en de drie jobs elk anders verdelen.
- Een ongeldige waarde (`0/3`, `4/3`, `x`) stopt de run met een duidelijke fout
  (`pytest.UsageError`), en nooit stil met alle of geen tests.
- Een deel zonder tests geeft pytest exit 5 (`no tests ran`), dus de job faalt. Dat dekt het AC
  "een job die geen tests verzamelt faalt" zonder extra code.

Unit-tests voor de verdeelfunctie (de functie los van de hook, zodat hij zonder subprocess te
testen is):

- elk nodeid uit een vaste lijst valt in precies één van de `n` delen;
- dezelfde invoer geeft dezelfde verdeling;
- ongeldige waarden geven een fout;
- zonder variabele wordt niets gedeselecteerd.

**`ci.yml`, job `python`.**

- De job krijgt `strategy.matrix.shard: [1, 2, 3]` en `fail-fast: false`, zodat een rood deel
  de andere twee niet afbreekt en je alle fouten ziet.
- De job-key blijft `python`: [tests/test_ci_domains_pin.py:39](../../../../tests/test_ci_domains_pin.py)
  zoekt op die naam. Er zijn geen verplichte status checks (`main` heeft geen branch protection
  of ruleset), dus de nieuwe weergavenamen `python (1)` … `python (3)` breken niets.
- `LIGHTHOUSE_PYTEST_SHARD: ${{ matrix.shard }}/3` gaat als `env` op de pytest-stap. De stap
  zelf blijft `uv run pytest -q -n auto`.
- `ci/literal-grep.sh` draait alleen als `matrix.shard == 1`.
- Het commentaar bij de job en het kopcommentaar van `ci.yml` noemen de verdeling en verwijzen
  naar `tests/conftest.py`.

**Pin-tests.** `tests/test_ci_domains_pin.py` pint de matrix (`[1, 2, 3]`), de `env` op de
pytest-stap en de `if` op `ci/literal-grep.sh`. Het aantal `uses:`-regels (`18`) verandert
niet: de matrix voegt geen stappen toe.

## Wat de bouwer zelf beslist

- De precieze regex van de bewaking en waar de verdeelfunctie staat (in `tests/conftest.py`
  zelf of een klein hulpbestand naast `tests/_domains.py`).
- Of de verdeling na de eerste CI-run scheef blijkt. Wijkt een deel meer dan ~15 s af van het
  gemiddelde, dan staat dat in de meetcomment op #151; een andere verdeling is dan een volgend
  issue, geen reden om B alsnog in te bouwen.

## Eigen keuzes in dit ontwerp, niet met de gebruiker besproken

- Verdelen per test (nodeid), niet per bestand. xdist verdeelt binnen een job al per test,
  dus module-fixtures draaien nu ook al meer dan eens.
- `fail-fast: false` op de matrix.
- De naam `LIGHTHOUSE_PYTEST_SHARD` en de vorm `<i>/<n>`.
- `ci/literal-grep.sh` in deel 1, niet in een eigen job: het duurt 0 s.

## Meten

Na de merge staat er een comment op #151 met de duur van de run en van elke job, voor twee PR's:
één met alleen `docs/` of `tests/`, en één met een Python-wijziging. Ernaast staan de cijfers uit
het issue.
