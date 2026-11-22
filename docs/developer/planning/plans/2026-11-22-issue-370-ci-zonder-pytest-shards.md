# CI zonder pytest-shards, en de meting tegen de nulmeting — implementatieplan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Doel:** `ci.yml` draait geen drie pytest-shards meer maar één job `pack-python` die alleen
`tests/pack/python/` draait; de laatste `.py`-bestanden buiten `pack/python/` en
`tests/pack/python/` zijn weg of naar Go; de meting en de triagetotalen staan in de PR-body.

**Architectuur:** Eerst gaan de resterende root-tests van `tests/` weg of naar Go (kernvraag van
`/lighthouse-test-standard`: welke productiewijziging laat deze test falen?). Dan kent
`ci/domains` het domein `pack-python`, krijgt `ci.yml` de ene job, volgt `macos.yml`, en krijgen
`pyproject.toml` en `uv.lock` hun slankste vorm. Als laatste meten drie `workflow_dispatch`-runs
tegen de nulmeting van #360.

**Tech stack:** Go 1.26 (`ci/domains`, `internal/repopins`, `e2e/`), GitHub Actions, bash, `uv`/pytest
(alleen voor `tests/pack/python/`), `gh` voor de meting.

**Spec:** [`2026-11-16-repo-zonder-python-design.md`](../specs/2026-11-16-repo-zonder-python-design.md)
(§7 CI en *Bewijs*). Issue: #370. Voorbeeld van opzet en meting:
[`2026-11-19-issue-360-changelog-go.md`](2026-11-19-issue-360-changelog-go.md).

## Globale randvoorwaarden

- Code, comments en testnamen Engels; commits, PR-body, issuecomment en fragment Nederlands.
- Go volgt `/lighthouse-go-standard`; tests volgen `/lighthouse-test-standard`.
- No-Gos van de milestone: `pack/python/` en `tests/pack/python/` blijven Python en pytest; `web/`
  blijft; geen ander gedrag van een script of hook; Go-tests niet versnellen; oude plannen
  blijven staan.
- Geen `uses:`-regel erbij of eraf zonder de telling in de Go-pin (nu 33 in
  `tests/test_ci_domains_pin.py::test_actions_pinned_by_sha`, na Task 1 in Go) bij te werken.
- Een run onder de controller kan een branch die `.github/workflows/` raakt niet zelf pushen
  (`workflow-push-gate`): push via de handoff `next: push-workflows`, zoals in plan 360 Task 2.
- Een `changelog.d/`-fragment van type `internal` (`docs/releasing.md`).
- Vóór elke commit naast `[test_gate] command`: `lighthouse imports check`.

## Besluiten (vastgelegd, de bouwer volgt ze)

1. **Domeinnaam `pack-python`** (job-id, `ci/domains`-sleutel, `changes`-output). Het bestaande
   domein `python` (alles buiten `web/`) verdwijnt: niets in CI heeft het daarna nodig (zie 3).
2. **`pack-python` is waar** voor een pad onder `pack/python/`, `tests/pack/python/`, `pack/core/`,
   `cmd/` of `internal/` (spec §7), plus `pyproject.toml` en `uv.lock` (de invoer van de job zelf).
   Een PR die alleen `docs/site/` raakt: onwaar; alleen `internal/`: waar (AC 3).
3. **`ci/literal-grep.sh` (twee aanroepen) verhuist naar de job `go`.** Het draaide op shard 1 van
   de oude job omdat die op elk pad buiten `web/` liep; het leest `pack/`, `internal`, `cmd` en
   `ci/literal-*`, en het domein `go` omvat die allemaal. `canary` en `no-python` krijgen als
   voorwaarde `go || pack-python` (voorheen `go || python`).
4. **`macos.yml`** krijgt één job `pack-python` zonder matrix en zonder `LIGHTHOUSE_PYTEST_SHARD`;
   de release-gate blijft zo werken.
5. **AC 2 en `ci/no-python.sh`:** het script noemt `python3` als naald van zijn eigen gate. De
   naam wordt in het script samengesteld (`py=python; needle="${py}3"`), commentaar zegt
   "de Python 3-interpreter"; gedrag blijft gelijk. De release-fixtures in
   `ci/changelog/testdata/releases/` bevatten `python3` in oude releasetekst, byte-gelijk aan wat
   de release schreef; die blijven. De PR-body noemt beide en geeft de AC-grep met
   `--exclude-dir=testdata` als meting.
6. **`pytest-xdist`**: houd het alleen als `uv run pytest -n auto tests/pack/python/` meetbaar
   sneller is dan serieel (Task 4 Step 5 meet het); zo niet, uit `pyproject.toml` en `uv.lock`.

## Bestandsoverzicht

- Weg: `tests/__init__.py`, `tests/_shard.py`, `tests/conftest.py`, `tests/test_ci_domains_pin.py`,
  `tests/test_no_python_script.py`, `tests/test_tag_with_ui.py`.
- Nieuw/uitgebreid (Go): `internal/repopins/workflows_test.go` (pins van `ci.yml`),
  `internal/repopins/tagwithui_test.go` (nieuw), `e2e/nopython_test.go` (nieuw, tag `e2e`),
  `ci/domains/domains.go`, `main.go`, `domains_test.go`, `sources_test.go`.
- Gewijzigd: `tests/pack/python/conftest.py` (fixtures `lighthouse_bin`, `_lighthouse_build_dir`),
  `.github/workflows/ci.yml`, `.github/workflows/macos.yml`, `ci/no-python.sh`, `pyproject.toml`,
  `uv.lock`, `docs/releasing.md` (alleen de zin over de pytest-suite).
- Nieuw: `changelog.d/issue-370-afronden-ci-zonder-pytest-shards-en-de.md`.

## Review Focus

- Een PR die alleen `ci/` of `docs/developer/` raakt draait `pack-python` niet: bedoeld; `go` wel
  voor `ci/` (literal-grep blijft bewaakt). Pin in `domains_test.go`.
- `pyproject.toml`/`uv.lock` alleen gewijzigd: de job draait (anders dekt niets een kapotte lockfile).
- `tests.pack.python._testkit`-imports blijven werken zonder `tests/__init__.py`
  (`consider_namespace_packages`): `uv run pytest -q tests/pack/python/` groen na Task 2.
- `canary` en `no-python` draaien bij een `cmd/`- of `internal/`-wijziging nog.
- De release-gate (`macos.yml`, aangeroepen door `release.yml`) start nog: `actionlint`-achtige pin
  in `workflows_test.go` voor het jobbestand.

---

### Task 1: De root-tests: triage, port of weg

**Files:**
- Delete: `tests/test_ci_domains_pin.py`, `tests/test_no_python_script.py`, `tests/test_tag_with_ui.py`
- Modify: `internal/repopins/workflows_test.go`
- Create: `internal/repopins/tagwithui_test.go`, `e2e/nopython_test.go`

**Interfaces:**
- Consumes: de helpers `read`, `jobs`, `runBlocks` en `hasLine` in `internal/repopins`
  (`workflows_test.go`); de bouwhelper van `e2e/` voor `cmd/lighthouse`.
- Produces: `TestCIPins*`-tests in Go die Task 3 aanpast aan de nieuwe jobvorm.

Per testfunctie één bestemming, met de kernvraag. Schrijf de triagetabel (test, voor/na,
bestemming, reden, bij "gedekt" de naam van de Go-test) in de PR-body; tel voor/na/weg.

- [ ] **Step 1: Triage `tests/test_ci_domains_pin.py` (21 tests).** Eerst `go test ./internal/repopins/
  -run Workflow -v` en lees welke pins al bestaan (`TestEveryCIGoTestHasGateFlags`,
  `TestEveryWorkflowPinsActionsBySHA`, …): die tests zijn "gedekt", niet porten. Port wat een
  echte `ci.yml`-fout vangt als Go-test `TestCI<Onderwerp>` in `workflows_test.go`:
  `no_paths_filter`, `domain_jobs_wait_for_changes`, `canary_job_runs_both_canaries_with_every_tool`,
  `changes_job_classifies`, `web_job_runs_every_check`, `node_jobs_cache_npm`, `binary_runs_without_web`,
  `go_jobs_wait_for_changes`, `go_lint_gates_stay_in_go_jobs`, `e2e_runs_apart_from_race_tests`,
  `changelog_job_runs_on_every_pr`, `dispatch_turns_on_every_domain`, `binary_runs_playwright`,
  `smoke_script_executable`. Weg: `no_measurement_step` (beschermt niets), en de shard-pins
  (`python_job_runs_in_three_shards`, `python_job_shows_the_pytest_header`) — die onderwerpen
  verdwijnen in Task 3. `actions_pinned_by_sha` heeft `TestEveryWorkflowPinsActionsBySHA`:
  alleen de telling erbij in Go als die test haar niet kent. `python_job_sets_up_go_before_pytest`
  en `pr_runs_no_macos_job` worden in Task 3 herschreven voor `pack-python`; port ze nu 1-op-1 met
  de huidige jobnaam `python` en pas ze in Task 3 aan. Elke Go-pin faalt aantoonbaar bij de mutatie
  die zijn naam noemt (mutatiecheck van `lighthouse-test-standard`: wijzig `ci.yml` tijdelijk, zie rood).
- [ ] **Step 2: Test.** `TestCIPins…` groen op de huidige `ci.yml`:
  `go test ./internal/repopins/ -run 'CI|Workflow' -v` — verwacht PASS voor elke geporte pin.
- [ ] **Step 3: `tests/test_tag_with_ui.py` (8 tests) naar `internal/repopins/tagwithui_test.go`.**
  Functie `TestTagWithUI` met subtests voor dezelfde situaties (tag wijst naar commit met dist;
  ouder zonder dist; zonder `index.html` faalt; tweede run na reset; genegeerde dist forceren;
  script is tracked executable; verouderd getrackt dist-bestand valt weg; vooraf gestaged
  ongerelateerd bestand blijft buiten de tagcommit). De helper maakt een tempo-repo met
  `GIT_CONFIG_GLOBAL=/dev/null`, `GIT_CONFIG_SYSTEM=/dev/null`, `HOME=<repo>`. Skip bij ontbrekende
  `git`/`bash` niet stil: `t.Fatal` met `COULD NOT CHECK`.
- [ ] **Step 4: `tests/test_no_python_script.py` (2 tests) naar `e2e/nopython_test.go`** (build-tag
  `e2e`, bouwt de binary met de bestaande helper): `TestNoPythonMissingFixtureFails` (kopie van
  `tests/pack/fixtures/hooks` zonder `Stop/go-lint-gate.json`: script faalt, stderr noemt het) en
  `TestNoPythonPython3HitFails` (een bestand met `python3` in de init-repo: script faalt met
  `python3 in <pad>:<regel>`). Draai: `go test -tags e2e ./e2e/ -run NoPython -v` — PASS. Mutatie:
  verwijder de check in `ci/no-python.sh` stap 4 tijdelijk, zie rood.
- [ ] **Step 5: Verwijder de drie pytest-bestanden**, draai de Go-suites
  (`go test -race -shuffle=on ./...`) en commit:
  `test(ci): root-tests naar Go, triage in de commit-body (#370)`.

### Task 2: Geen root-conftest meer

**Files:**
- Delete: `tests/__init__.py`, `tests/_shard.py`, `tests/conftest.py`
- Modify: `tests/pack/python/conftest.py`, `pyproject.toml` (`testpaths`)

**Interfaces:**
- Consumes: `lighthouse_dir()` uit `tests/pack/python/_testkit.py`.
- Produces: fixtures `_lighthouse_build_dir` (session) en `lighthouse_bin` in
  `tests/pack/python/conftest.py`, met dezelfde docstrings en gedrag als nu.

- [ ] **Step 1:** Draai `uv run pytest -q tests/pack/python/` en noteer groen als uitgangspunt.
- [ ] **Step 2:** Verplaats de twee fixtures van `tests/conftest.py` naar
  `tests/pack/python/conftest.py`; laat de shard-hooks (`SHARD`, `pytest_configure`,
  `pytest_report_header`, `pytest_collection_modifyitems`) en `tests/_shard.py` vervallen.
  `tests/__init__.py` weg. In `pyproject.toml`: `testpaths = ["tests/pack/python"]`.
- [ ] **Step 3:** `uv run pytest -q` (zonder pad) en `uv run pytest -q tests/pack/python/`
  — verwacht PASS, zelfde testaantal als Step 1. Valt `from tests.pack.python._testkit import …`
  om zonder `tests/__init__.py`: laat `pythonpath = ["."]` en
  `consider_namespace_packages = true` staan en los de import op met een `tests/pack/python/__init__.py`
  dat al bestaat; voeg niets toe dat Step 1 niet nodig heeft.
- [ ] **Step 4:** `git ls-files '*.py' | grep -v -e '^pack/python/' -e '^tests/pack/python/'` — leeg
  verwacht (AC 1). Commit: `test(pack): shard-conftest weg, fixtures naar tests/pack/python (#370)`.

### Task 3: `ci/domains` en `ci.yml`: één pytest-job

**Files:**
- Modify: `ci/domains/domains.go`, `ci/domains/main.go`, `ci/domains/domains_test.go`,
  `ci/domains/sources_test.go`, `.github/workflows/ci.yml`, `internal/repopins/workflows_test.go`,
  `ci/no-python.sh`

**Interfaces:**
- Consumes: `Classify(paths []string) map[string]bool` (bestaand).
- Produces: dezelfde `Classify`, nu met sleutels `web`, `pack-python`, `go` (geen `python`);
  CLI-uitvoer `web=`, `pack-python=`, `go=` voor `--github-output`.

- [ ] **Step 1: Schrijf de falende tests in `ci/domains/domains_test.go`** (tabel):
  `internal/x.go`→ `pack-python` waar; `docs/site/index.html`→ onwaar (en `go` onwaar, zoals nu);
  `pack/python/scripts/a.py`, `tests/pack/python/t.py`, `pack/core/x`, `cmd/lighthouse/main.go`,
  `pyproject.toml`, `uv.lock` → waar; `web/src/a.ts`, `docs/a.md`, `pack/node/x`, `ci/literal-grep.sh`,
  `tests/pack/fixtures/a.json` → onwaar. De sleutel `python` bestaat niet meer; de bestaande
  `python`-gevallen worden vertaald of vervallen met reden (kernvraag), `sources_test.go:165,229`
  houdt `goFixtures` en past alleen de sleutelnaam aan.
- [ ] **Step 2:** `go test ./ci/domains/ -run Classify -v` — FAIL (sleutel ontbreekt).
- [ ] **Step 3: Implementeer** in `Classify` de sleutel `pack-python` met een lijst
  `packPythonPrefixes = {"pack/python/", "tests/pack/python/", "pack/core/", "cmd/", "internal/"}` en
  `packPythonFiles = {"pyproject.toml", "uv.lock"}`; verwijder `python`. Pas de doc-comments van
  `domains.go` (inclusief de uitleg bij `notGoDirs`) en `main.go` aan.
- [ ] **Step 4:** `go test ./ci/domains/` — PASS.
- [ ] **Step 5: `ci.yml`.** (a) job `python` → `pack-python`: geen `strategy`, geen
  `LIGHTHOUSE_PYTEST_SHARD`, `if: needs.changes.outputs.pack-python == 'true'`; stappen: checkout,
  setup-go, build lighthouse, setup-uv, `uv sync --locked`, `uv run pytest -n auto tests/pack/python/`
  (besluit 6 kan `-n auto` nog schrappen); `timeout-minutes` blijft 10. (b) de twee
  `literal-grep`-aanroepen gaan als stap naar de job `go`, ongewijzigd. (c) `setup-uv` met zijn
  commentaar uit `go-e2e` naar `pack-python` (staat er al); `go-e2e` houdt alleen setup-go en de
  e2e-run, en laat de e2e-tests die `uv` nodig hebben niet stil overslaan: ze skippen bij ontbrekende
  `uv` (`e2e/e2e_test.go:331-408`), dus draai ze in `pack-python`; zoek de testnamen met `grep -n 'uv' e2e/e2e_test.go` (regels 331-408) en gebruik ze in `-run`:
  `go test -tags e2e ./e2e/ -run '<namen>' -v` na de build; zonder `uv` zouden ze stil skippen.
  (d) `changes` job: output `pack-python`, dispatch-regel `echo "pack-python=$ALL"`; `canary` en
  `no-python`: `go == 'true' || pack-python == 'true'`. (e) Kop van `ci.yml`: de zinnen over shards en
  `python-only` herschreven; `workflow_dispatch`-inputbeschrijving `(web, pack-python, go)`.
- [ ] **Step 6: Pins bijwerken in `workflows_test.go`:** `pack-python` heeft geen matrix en geen
  shard-env; draait `uv run pytest` op `tests/pack/python/`; wacht op `changes`; zet Go op vóór
  pytest; `go` bevat beide literal-grep-aanroepen (telling twee, in één job); `go-e2e` heeft géén
  `setup-uv` en de e2e-run met `uv`-testen staat in `pack-python`; `canary`/`no-python` gate
  `go || pack-python`; telling `uses:` bijgewerkt; dispatch zet alle drie de domeinen. Mutatiecheck
  per pin.
- [ ] **Step 7: `ci/no-python.sh`** volgens besluit 5; draai `go test -tags e2e ./e2e/ -run NoPython`
  — PASS. Draai `grep -rn python3 .github/ ci/ tools/ --exclude-dir=testdata` — alleen
  `ci/windows-measure.sh:71,74` en geen treffer in de `pack-python`-job (de job noemt `python3`
  niet; de AC-bedoeling "de pack/python-job" is een jobnaam, geen treffer).
- [ ] **Step 8:** `go test -race -shuffle=on ./...` — PASS. Commit:
  `ci: een pack-python-job in plaats van drie pytest-shards (#370)`.

### Task 4: `macos.yml`, `pyproject.toml`, `uv.lock`, docs

**Files:**
- Modify: `.github/workflows/macos.yml`, `pyproject.toml`, `uv.lock`, `docs/releasing.md`

**Interfaces:**
- Consumes: de job-opbouw van Task 3.
- Produces: `macos.yml` job `pack-python` (zonder matrix) die `release.yml` blijft aanroepen.

- [ ] **Step 1:** `macos.yml`: job `python` → `pack-python`, matrix en shard-env weg, kop zonder
  "in three parts"; stap `uv run pytest -n auto tests/pack/python/`. Pin in `workflows_test.go`
  (`TestEveryMacOSGoTestHasGateFlags` blijft) krijgt een pin: `macos.yml` bevat geen
  `LIGHTHOUSE_PYTEST_SHARD`.
- [ ] **Step 2:** `docs/releasing.md:38`: "pytest suite" → "de pytest-job van `pack/python`".
- [ ] **Step 3:** `pyproject.toml`: alleen wat de job nodig heeft. Houd `[tool.uv] package = false`,
  `testpaths`, `pythonpath`, `consider_namespace_packages` en de marker `network_stays_red`
  (`tests/pack/python/_network.py`, #338). Dev-group: `pytest>=8.1`; `pytest-xdist>=3.8.0` volgens
  Step 5. Draai `uv lock` en bewijs met `git diff --stat uv.lock`.
- [ ] **Step 4:** `uv sync --locked && uv run pytest -q tests/pack/python/` — PASS.
- [ ] **Step 5: Besluit 6.** Meet lokaal `time uv run pytest -q tests/pack/python/` en
  `time uv run pytest -n auto -q tests/pack/python/`; houd xdist alleen als `-n auto` merkbaar
  sneller is (nu is het de reden dat de oude shards `-n auto` draaiden). Zet de cijfers in de
  PR-body; pas `ci.yml` (Task 3 Step 5a), `macos.yml` en `tests/pack/python/test_network.py:134`
  (`-p no:xdist`) mee aan als xdist weggaat.
- [ ] **Step 6:** `uv run pytest -q tests/pack/python/test_gate_python.py` — PASS (de test-gate in
  `lighthouse.toml` kiest pytestbestanden; hij moet na het inkorten blijven werken). Commit:
  `ci(pack): macos zonder shards, pyproject en uv.lock ingekort (#370)`.

### Task 5: Changelog-fragment, push, meting, PR

**Files:**
- Create: `changelog.d/issue-370-afronden-ci-zonder-pytest-shards-en-de.md`

- [ ] **Step 1: Fragment** (Nederlands, front matter `type: internal`, formaat als
  `ci/changelog/testdata/releases/v0.18.0/changelog.d/issue-285-afronden-zonder-python.md`): één
  tot drie zinnen: CI draait één `pack-python`-job in plaats van drie pytest-shards; de repo bevat
  geen Python buiten `pack/python/` en `tests/pack/python/`. Commit
  `docs(changelog): fragment voor #370`.
- [ ] **Step 2: Push** (de branch raakt `.github/workflows/`): V2b van `/lighthouse-write-pr`
  (`lighthouse roborev-gate gate --push`), dan handoff `next: push-workflows`,
  `after: /superpowers:subagent-driven-development <absoluut planpad>` en `roborev: <uitvoer van
  lighthouse roborev-gate body-line --push>`; de volgende sessie hervat bij Step 3. Buiten de
  controller: `git push -u origin issue-370-afronden-ci-zonder-pytest-shards-en-de`.
- [ ] **Step 3: De nulmeting lezen.** `lighthouse forge issue view 360 --comments` en zoek de comment
  `Nulmeting #360 (ci.yml, alle domeinen)`: wandklok en som runnerminuten per run plus het
  gemiddelde. Staat die comment er niet: stop en meld het (`lighthouse ask`), meet niet zonder
  referentie.
- [ ] **Step 4: Drie runs na elkaar** zoals plan 360 Task 2 Step 2 (`gh workflow run ci.yml --ref
  <branch> -f all-domains=true`, `gh run watch --exit-status`, elke run pas na de vorige) en per
  run de `gh run view … --json startedAt,updatedAt,jobs`-query van plan 360 Task 2 Step 3. Alle
  domeinjobs (`pack-python`, `go`, `go-test`, `go-e2e`, `web`, `binary`, `canary`,
  `no-python`) moeten draaien en groen zijn; anders telt de run niet.
- [ ] **Step 5: Vergelijk.** Wandklok gemiddelde ≤ nulmeting; som runnerminuten gemiddelde <
  nulmeting. Is een van beide niet gehaald: geen PR met `Closes`; maak één verbeteronderzoek (welke
  job is de zwaarste) en meld het met `lighthouse ask`.
- [ ] **Step 6: Triagetotalen (AC 5).** Lees de PR-bodies van de samengevoegde PR's van #360–#369
  (`git log --oneline --grep '(#36'` voor de nummers; `lighthouse forge pr view <nr>`) en tel per
  stap *tests voor, geport, weg*; tel de eigen Task 1 erbij. Zet een tabel per stap met totaalregel.
- [ ] **Step 7:** `/lighthouse-write-pr`: `Closes #370` met Evidence-tabel: de zes AC (AC 3 met
  `go run ./ci/domains classify` voor `internal/x.go` (waar) en `docs/site/x` (onwaar), AC 4 en 5
  met de cijfers), de uitleg over de twee `python3`-treffers (besluit 5) en de xdist-meting.
  Handoff `next: /lighthouse-close-issue 370`.
