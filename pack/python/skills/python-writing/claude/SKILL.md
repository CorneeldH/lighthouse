---
name: paia-python-schrijven
description: Use when writing or changing Python in src/ or tests/ — modules, pytest-tests, en de ruff/ty-controle daarop. Carries a KPI table whose every row is a command that runs in this repo, plus the measurement rules (a ty zero without its extras is not a zero). LET OP — gaat het om shell die iets bewaakt, gebruik paia-shell-gates; gaat het om workflow-YAML, zie .claude/rules/workflows.md; gaat het om een bestand in de Obsidian-vault, dan is dit niet de skill.
metadata:
  binding: hard
  origin: own
  domain: factory
---

# Python schrijven in deze repo

## Domein

Python in `src/` en `tests/`, en de controle erop: `ruff`, `ty`, pytest. Ook geldig
wanneer er nog geen bestand open is — tijdens brainstormen of plannen, wanneer je
besluit *hoe* je iets gaat testen. Dat is de reden dat dit een skill is en geen rule.

Buiten scope: shell die iets bewaakt (`paia-shell-gates`), workflow-YAML
(`.claude/rules/workflows.md`), de Expo-app in `apps/` (`apps/CLAUDE.md`).

De **normen** staan in [`.claude/rules/python.md`](../../rules/python.md) — netwerkverbod
in unit-tests, patch op de importnaam, geen kale suppressie. Ze staan daar en niet hier,
zodat er één plek is die ze draagt. Dit bestand draagt de procedure en de meting, plus één
norm die vóór het eerste bestand valt: *replace, don't layer* (zie onder).

## KPIs

Elke rij is op deze repo gedraaid. De kolom "gemeten" is de uitkomst op 2026-08-24 in
worktree `issue-625-python-write-skill`.

| Norm | Meetcommando | Doel | Gemeten |
|---|---|---|---|
| De blokkerende ruff-regels staan op nul | `uvx ruff@0.16.4 check --select E722,B006,F403,F811,B023,PT012,PGH003,PGH004 src/ tests/` | rc 0 | rc 0 |
| `src/` is type-schoon | `uvx ty@0.0.72 check src/` | 0 diagnostics, mét vermelding van de extras | 0 (zonder extras) |
| De netwerkguard bijt | `uv run pytest tests/unit/test_unit_network_guard.py -q` | 10 passed, 2 skipped | 10 passed, 2 skipped |
| De unit-selectie draait zonder netwerklek | `uv run pytest tests/unit -m 'not hardware_audio and not slow' -q` | rc 0, geen `UnitTestNetworkAccess` | 7039 passed, 60 skipped, 62s |
| Diff-coverage op je nieuwe src/-regels | `uv run python scripts/quality/ratchet.py coverage --base origin/main` (optioneel, bij twijfel) | ≥ 90% | — |

Rij 5 is geen verplichte stap vóór de PR — sinds #1016 oordeelt de nachtelijke ratel
(`nightly-tests.yml` → `ratchet.py nightly`) daar per gemergde PR over; deze rij is de
tegenhanger als je vóóraf al wilt weten of je erdoor komt.

### Waarom rij 1 gescoped is

De naïeve vorm — `ruff check` op de gewijzigde bestanden, doel 0 — is sinds #562
onhaalbaar zonder dat er iets mis is met je wijziging. Die issue zette zeven *tellende*
regels bij in `[tool.ruff.lint] extend-select` (`C901`, `TRY400`, `S110`, `F841`, `F401`,
`S112`, `PT011`), samen 160 bestaande hits over `src/` + `tests/`.

**De gelinte set en de blokkerende set zijn twee verschillende sets.** `extend-select` is
wat ruff overal rapporteert; `BLOCKING_RULES` in `.claude/hooks/ruff-format.sh` is de
deelverzameling waarop een edit hard faalt — bewust alleen de acht regels die repo-breed
op nul staan, zodat legacy-schuld nooit een blokkade wordt. De KPI-rij neemt precies die
acht over, zodat skill en hook dezelfde as meten.

### Waarom rij 2 zijn extras noemt

`ty` vindt alleen typefouten in code waarvan het de imports kan resolven. Een module die
het niet vindt maakt élk symbool eruit `Unknown`, en op `Unknown` is geen typefout
mogelijk. Dezelfde commit geeft daardoor twee uitkomsten: met
`uv sync --extra test --group dev` resolvet pyannote niet en is `src/` groen; met
`--extra all --group dev` checkt `ty` die lichamen écht. Die toestand staat nergens vast
en komt niet in de diff voor. Sinds #1016 draait de stap `Type-check src/` in de ene CI-job
`ci` de tweede vorm.

Praktisch: de pre-commit-gate draait `test_quality_hooks.py::test_ty_hook_is_green_on_this_repo`,
dus een worktree met `--extra all` kan een commit blokkeren op een diagnostic. Sinds #1016 is dat
geen losse venv meer die van CI afwijkt: de ene job `ci` synct óók `--extra all --extra test
--group dev` (niet-Dependabot), en `Type-check src/` draait daarna `--no-sync` in precies die
venv — dezelfde extras als een lokale `uv sync --extra all --group dev`. Wat lokaal blokkeert,
blokkeert dus ook in díe stap op CI, niet als toeval maar als voorspelling. Alleen `Unit tests`
— die in dezelfde job óók `--no-sync` uit diezelfde venv draait — ziet de diagnostic zelf niet:
pytest checkt geen types, `ty` draait daar niet. (Tot #806 raakte pre-push hem ook, via de
coverage-arm die de hele suite draaide; die arm is eruit.)

### De rij die geschrapt is

Bewust vermeld: een KPI-tabel die doet alsof ze de hele foutklasse dekt, is zelf een
instantie van die foutklasse.

**"`mock.patch` zonder assert: 0 treffers."** Gemeten op 2026-08-24: 112 bestanden in
`tests/unit` gebruiken `patch(`, waarvan **72 zonder enige** `assert_called*` /
`assert_awaited*` / `assert_any_call` — 64%, en vrijwel allemaal terecht: een patch die
alleen config, tijd of een pad wegstubt hoort geen call-assertie te hebben. Het
gevaarlijke geval (een patch die *doel mist*) is met grep niet van het onschuldige te
onderscheiden; het verschil is semantisch. Staat daarom als norm in de rule, niet als
teller hier.

## Non-negotiable

Deze twee gaan over de meting, niet over het bestand. De normen over het bestand staan in
de rule.

- **Een nul zonder vermelding van de extras is geen nul.** ✅ "0 diagnostics met
  `uv sync --extra all --group dev`" · ❌ "`ty` is groen".
- **Drie uitkomsten, niet twee**: geslaagd, gefaald, en *niet meetbaar vanaf hier*. Acht
  bestanden importeren macOS-frameworks (pyobjc, rumps, mlx) die op geen enkele runner
  resolven. ✅ zeggen dat `src/dictation/quartz_listener.py` hier niet te meten is ·
  ❌ de repo-brede nul presenteren alsof hij die bestanden dekt.

## Samenvoegen of verdiepen: replace, don't layer

Voeg je ondiepe modules samen tot één diepere interface, of trek je logica achter een
bestaande interface, dan zijn de tests op de oude, ondiepe modules afval zodra de nieuwe
interface hetzelfde gedrag dekt. Verwijder ze in dezelfde change; laat ze niet naast de
nieuwe tests staan.

- ✅ `test_a.py` en `test_b.py` weg in de commit waarin `test_ab.py` hun gedrag via de nieuwe
  interface toetst · ❌ beide sets laten staan "voor de zekerheid".
- **Eerst gedekt, dan weg.** Een oude test gaat pas weg als het gedrag dat hij toetst via de
  nieuwe interface getoetst wordt. Anders is verwijderen dekking kwijtraken, geen opruimen.

Waarom hier en niet in de rule: de beslissing valt bij het plannen ("we voegen A en B
samen"), vóór er een `src/`- of `tests/`-bestand open staat — wanneer deze skill laadt en de
rule nog niet. Waarom geen KPI-rij: of een oude test hetzelfde gedrag dekt als een nieuwe is
semantisch, net als bij de geschrapte patch-rij hierboven; grep ziet het verschil niet.

## Een complexiteitsgrens halen: verdiepen, niet opknippen

Staat een functie boven de grens (cyclomatisch 20, cognitief 25; voor nieuwe code 10 en 15),
los die dan op door een beslissing achter een eigen interface te verbergen, niet door hem in
stukken te hakken die alleen samen te begrijpen zijn.

- ✅ een `if/elif`-keten over berichttypes vervangen door een dispatch-tabel, of het parsen
  naar een pure functie met een eigen test tillen, zodat de aanroeper één regel leest · ❌
  `_stap_1(a, b, c, d)` en `_stap_2(a, b, c, d)` die één keer worden aangeroepen en de lokale
  staat heen en weer geven (*conjoined methods*), of een helper die alleen doorgeeft
  (*pass-through method*).
- **Waarom een agent dit nodig heeft.** Hij optimaliseert letterlijk naar het getal, en
  opknippen verlaagt het getal zonder iets eenvoudiger te maken.
- **Meet het.** `uv run python scripts/quality/ratchet.py ruff`. Een nieuwe helper boven
  cognitief 15 laat de teller `cognitive_over_15` stijgen, zodat ondiep opknippen daar
  zichtbaar wordt.

Bron: Ousterhout, *A Philosophy of Software Design* (diepe modules, *conjoined methods*,
*pass-through method*), en #749, dezelfde gedachte als "replace, don't layer" hierboven.
Waarom hier en niet in de rule: de beslissing valt bij het plannen van de refactor ("hoe
snoei ik deze functie"), vóór er een `src/`-bestand open staat — wanneer deze skill laadt en
de rule nog niet.

## De pairing

| Helft | Draagt | Wanneer |
|---|---|---|
| deze skill | de procedure, de KPI-tabel, de meetregels | terwijl je schrijft, ook vóór het eerste bestand |
| `.claude/rules/python.md` | de normen | zodra Claude een `src/`- of `tests/`-bestand leest |
| `.claude/hooks/ruff-format.sh` | de acht blokkerende regels, per edit, exit 2 | direct na de edit |
| `.claude/hooks/ty-check.sh` | een signaal op een type-vuile `src/`, één keer per beurt | bij `Stop` |

Beide hooks zijn **fail-open**: een stukke omgeving (offline, ruff niet op te halen) mag
niet lezen als "de code is fout". Dat is het omgekeerde van `shellcheck.sh`, waar stil
overslaan zélf de fout is die de gate moet voorkomen.

## Bekende valkuilen

- **Een verse worktree heeft geen `.venv`.** `ty` rapporteert dan 444 diagnostics in
  plaats van 0; de hook valt terug op de venv van de hoofd-checkout, een handmatige
  `uvx ty` niet. Draai eerst één `uv run` of `uv sync`.
- **`uv run ruff` bestaat niet in een verse worktree**: ruff zit bewust niet in de
  project-venv. Gebruik `uvx ruff@<pin>`; de pin staat in `.ruff-version`.
- **Het patch-doel is de importnaam, niet de definitienaam.** Zie de rule. Dit is de
  oorzaak van #525 en de reden dat de netwerkguard bestaat.
- **Een test die snel en groen is, bewijst niets over de mock.** Zet de
  `assert_called_once()` erbij; zonder die assertie mist een volgende patch stilletjes
  opnieuw doel.
- **De acht `[[tool.ty.overrides]]` in `pyproject.toml` zijn scoped, geen per-regel
  suppressie.** Op een Mac resolven die imports wél, en zou een `# type: ignore` daar zelf
  een fout worden.

## Herkomst

Eigen materiaal, op één regel na. *Replace, don't layer* komt uit
`vendor/mattpocock-skills/skills/engineering/codebase-design/DEEPENING.md`, gewogen in
`docs/developer/planning/specs/2026-08-27-mattpocock-skills-weging.md` (#749). Alleen die
regel: de rest van `codebase-design` (dependencies injecteren, de depth-definitie) botst
met besluit 2 en 3 in `docs/developer/architecture/decisions.md` en gaat niet mee. De vorm (KPI-tabel + non-negotiables) komt van de CEDA-skills in
`cedanl/ceda-workshop-starter`, via `paia-shell-gates`. Ontwerp:
`docs/developer/planning/specs/2026-08-24-python-schrijfskill-design.md` en
`docs/developer/planning/specs/2026-08-22-instructielagen-rules-skills-hooks-design.md`.
