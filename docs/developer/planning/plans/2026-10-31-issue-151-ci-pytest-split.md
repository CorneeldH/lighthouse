# CI zonder Go-jobs bij docs- en testwijzigingen, pytest in drie delen: implementatieplan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** een PR zonder Go-wijziging start geen Go-jobs meer, en de pytest-suite draait in CI
verdeeld over drie parallelle jobs.

**Architecture:** `ci/domains.py` krijgt `tests/` (behalve `tests/pack/fixtures/`), `README.md`
en `CHANGELOG.md` als niet-Go. Een nieuwe `tests/conftest.py` deselecteert op basis van
`LIGHTHOUSE_PYTEST_SHARD=<i>/<n>` alle tests buiten deel `i`. De `python`-job in `ci.yml` wordt
een matrix over drie delen.

**Tech Stack:** Python 3.11 stdlib, pytest met pytest-xdist, GitHub Actions.

**Spec:** [2026-10-31-issue-151-ci-pytest-split-design.md](../specs/2026-10-31-issue-151-ci-pytest-split-design.md)

## Global Constraints

- Geen nieuwe dependency: alleen stdlib (`zlib.crc32`) en pytest zelf.
- Zonder `LIGHTHOUSE_PYTEST_SHARD` draait de hele suite. Lokaal en in de test gate verandert er niets.
- Verdeling: een test hoort bij deel `zlib.crc32(nodeid.encode()) % n + 1`; nooit `hash()`.
- Ongeldige waarde → `pytest.UsageError`, nooit stil alle of geen tests.
- Job-key blijft `python`. Matrix `shard: [1, 2, 3]`, `fail-fast: false`.
- `ci/literal-grep.sh` alleen bij `matrix.shard == 1`.
- `uses:`-telling in `tests/test_ci_domains_pin.py` blijft `18`.
- Commentaar en code in het Engels; plan, commits en het changelogfragment volgen de repo
  (commit in het Nederlands, fragment in het Engels zoals de bestaande fragmenten).
- Atomic commits, Conventional Commits.

## Review Focus

1. Een `README.md` dieper in de boom (`pack/core/README.md`) blijft Go: `pack/` wordt ingebed.
   Alleen de root-`README.md` en root-`CHANGELOG.md` gaan uit Go. → testgeval in taak 1.
2. Een prefix-gelijkende map (`testsx/a.py`) blijft Go, zoals `docsx/` nu. → testgeval in taak 1.
3. `tests/pack/fixtures/…` telt als Go én python. → testgeval in taak 1.
4. Een lege `LIGHTHOUSE_PYTEST_SHARD=""` (verkeerd ingevulde YAML) mag niet stil de hele suite
   in elk deel draaien: telt als ongeldig. → testgeval in taak 2.
5. `1/1` is geldig en draait alles; `0/3`, `4/3`, `3`, `a/b`, `2/0` en ` 2/3` zijn ongeldig.
   → testgevallen in taak 2.

---

### Taak 1: `tests/`, `README.md` en `CHANGELOG.md` uit het Go-domein

**Files:**
- Modify: `ci/domains.py:22-31` (`_NOT_GO_DIRS`, `_NOT_GO`, commentaar), `ci/domains.py:34-46` (`classify`)
- Test: `tests/test_domains.py`

**Interfaces:**
- Consumes: niets.
- Produces: `classify(paths) -> set[str]` met ongewijzigde signatuur; in `tests/test_domains.py`
  de helper `_repo_reads(text: str) -> list[str]`.

- [ ] **Stap 1: Schrijf de falende testgevallen.** Voeg toe aan de parametrize-lijst van `test_classify`:

```python
        (["tests/test_x.py"], {"python"}),
        (["README.md"], {"python"}),
        (["CHANGELOG.md"], {"python"}),
        (["tests/test_x.py", "README.md"], {"python"}),
        (["tests/pack/fixtures/protocol/headers.json"], {"go", "python"}),
        (["pack/core/README.md"], {"go", "python"}),
        (["testsx/a.py"], {"go", "python"}),
```

En een CLI-test naar het model van `test_classify_cli_docs_only_pr_skips_go`:

```python
def test_classify_cli_tests_only_pr_skips_go(monkeypatch, capsys):
    """A PR of #151's kind: a pytest test, the root README and a fragment."""
    paths = b"tests/test_site_pages.py\0README.md\0changelog.d/x.md\0"
    monkeypatch.setattr(sys, "stdin", io.TextIOWrapper(io.BytesIO(paths)))
    assert domains.main(["classify"]) == 0
    assert capsys.readouterr().out.splitlines() == ["web=false", "python=true", "go=false"]
```

- [ ] **Stap 2: Schrijf de falende bewaking.** In `tests/test_domains.py`:

```python
def test_repo_reads_finds_a_read_above_the_package():
    text = 'os.ReadFile("../../tests/x.py")\nos.ReadFile(`../README.md`)\n"../../CHANGELOG.md"'
    assert _repo_reads(text) == ["../../tests/x.py", "../README.md", "../../CHANGELOG.md"]


def test_repo_reads_skips_fixtures_temp_repos_and_inner_dotdot():
    text = (
        'os.ReadFile("../../tests/pack/fixtures/protocol/headers.json")\n'
        'writeFile(t, repo, "tests/test_x.py", "")\n'
        'filepath.Join(repo, "README.md")\n'
        'unitText("file", ".claude/hooks/lighthouse/../../../README.md", "")\n'
    )
    assert _repo_reads(text) == []


def test_no_go_file_reads_tests_or_root_md():
    """Fails when Go code starts to read tests/ (outside the fixtures), README.md or CHANGELOG.md."""
```

De laatste test leest alle `.go`-bestanden zoals `test_no_go_file_names_a_path_outside_go`
(dezelfde `git ls-files`-aanroep) en asserteert `_repo_reads(...) == []` per bestand, met
`rel` in de foutmelding. Docstring of commentaar noemt de beperking: een pad uit losse delen
(`filepath.Join(root, "tests", …)`) ziet de bewaking niet.

- [ ] **Stap 3: Draai en zie rood.**

Run: `uv run pytest -q tests/test_domains.py`
Expected: FAIL; de nieuwe `classify`-gevallen geven `{"go", "python"}`, `_repo_reads` is niet gedefinieerd.

- [ ] **Stap 4: Implementeer `_repo_reads(text: str) -> list[str]`** in `tests/test_domains.py`:
een regex op een stringliteral (`"` of `` ` ``) die direct begint met één of meer `../`, gevolgd
door `tests/`, `README.md` of `CHANGELOG.md`; geeft het pad zonder aanhalingsteken terug, en
slaat `tests/pack/fixtures/` over (negatieve lookahead).

- [ ] **Stap 5: Pas `ci/domains.py` aan.** `_NOT_GO_DIRS` krijgt `"tests/"`; `_NOT_GO` krijgt
`"README.md"` en `"CHANGELOG.md"`. In `classify` telt een pad onder `tests/pack/fixtures/` weer
als Go: een constante `_GO_FIXTURES = "tests/pack/fixtures/"` naast `_GO_TOO`, en `not_go` is
onwaar voor een pad dat daarmee begint. Het commentaar boven `_NOT_GO_DIRS` noemt de nieuwe
paden en de meting: Go leest uit `tests/` alleen `tests/pack/fixtures/`
(`internal/protocol/header_test.go:19`); de andere `tests/`- en `README.md`-strings in Go zijn
bestanden in tijdelijke repos (#151); `tests/test_domains.py` bewaakt het.

- [ ] **Stap 6: Draai en zie groen.**

Run: `uv run pytest -q tests/test_domains.py tests/test_ci_domains_pin.py`
Expected: PASS.

- [ ] **Stap 7: Mutatiecheck.** Zet tijdelijk `os.ReadFile("../../tests/x.py")` in een
Go-testbestand en draai `uv run pytest -q tests/test_domains.py::test_no_go_file_reads_tests_or_root_md`:
FAIL. Draai de wijziging terug en zie PASS. Haal ook tijdelijk `"tests/"` uit `_NOT_GO_DIRS`:
`test_classify` faalt. Terugdraaien.

- [ ] **Stap 8: Commit.**

```bash
git add ci/domains.py tests/test_domains.py
git commit -m "feat(ci): tests/, README.md en CHANGELOG.md buiten het Go-domein (#151)"
```

### Taak 2: pytest-verdeling via `LIGHTHOUSE_PYTEST_SHARD`

**Files:**
- Create: `tests/_shard.py`, `tests/conftest.py`
- Test: `tests/test_shard.py`

**Interfaces:**
- Consumes: niets.
- Produces:
  - `parse_shard(value: str | None) -> tuple[int, int] | None` in `tests/_shard.py`:
    `None` bij `None`, anders `(index, total)` met `1 <= index <= total`; anders `ValueError`.
  - `in_shard(nodeid: str, index: int, total: int) -> bool`:
    `zlib.crc32(nodeid.encode()) % total + 1 == index`.
  - `ENV = "LIGHTHOUSE_PYTEST_SHARD"`.
  - `tests/conftest.py`: `SHARD` (een `pytest.StashKey`), `pytest_configure(config)` en
    `pytest_collection_modifyitems(config, items)`.

- [ ] **Stap 1: Schrijf de falende tests** in `tests/test_shard.py`:

```python
NODEIDS = [f"tests/test_{i}.py::test_{j}" for i in range(20) for j in range(10)]


def test_unset_means_no_sharding():
    assert parse_shard(None) is None


@pytest.mark.parametrize(("value", "expected"), [("1/3", (1, 3)), ("3/3", (3, 3)), ("1/1", (1, 1))])
def test_parse_valid(value, expected):
    assert parse_shard(value) == expected


@pytest.mark.parametrize("value", ["", "0/3", "4/3", "3", "a/b", "2/0", " 2/3", "1/3/5"])
def test_parse_invalid(value):
    with pytest.raises(ValueError, match=ENV):
        parse_shard(value)


def test_every_test_in_exactly_one_shard():
    for nodeid in NODEIDS:
        assert sum(in_shard(nodeid, i, 3) for i in (1, 2, 3)) == 1, nodeid


def test_shards_are_stable_and_not_empty():
    first = [[n for n in NODEIDS if in_shard(n, i, 3)] for i in (1, 2, 3)]
    assert first == [[n for n in NODEIDS if in_shard(n, i, 3)] for i in (1, 2, 3)]
    assert all(first)


@pytest.mark.parametrize(
    ("nodeid", "expected"),
    [
        ("tests/test_domains.py::test_classify", 2),
        ("tests/test_x.py::test_b", 3),
        ("tests/test_y.py::test_c", 1),
    ],
)
def test_known_nodeid_lands_in_its_crc32_shard(nodeid, expected):
    """Literal shard numbers: hash() differs per process and would fail here."""
    assert [i for i in (1, 2, 3) if in_shard(nodeid, i, 3)] == [expected]
```

De verwachte delen zijn `zlib.crc32(nodeid.encode()) % 3 + 1`, eenmalig uitgerekend en als
getal vastgezet; een mutatie naar `hash(nodeid)` faalt daardoor niet bij toeval maar altijd.

Plus de hooks uit `tests/conftest.py`, direct aangeroepen met nep-objecten (geen `pytester`):
`items` als `types.SimpleNamespace(nodeid=...)`, `config` als een `SimpleNamespace` met een
`stash = pytest.Stash()` en `hook.pytest_deselected` als een opnemende functie; `ENV` via
`monkeypatch.setenv`/`delenv`. Tests:

- `test_configure_rejects_invalid_env`: met `ENV=0/3` geeft `pytest_configure(config)`
  `pytest.UsageError`;
- `test_modifyitems_keeps_all_without_env`: zonder `ENV` blijven alle items staan en wordt
  `pytest_deselected` niet aangeroepen;
- `test_modifyitems_keeps_only_its_shard`: met `ENV=2/3` blijven precies de items met
  `in_shard(nodeid, 2, 3)` staan, in hun volgorde, en gaat de rest in één aanroep naar
  `pytest_deselected(items=[...])`.

- [ ] **Stap 2: Draai en zie rood.**

Run: `uv run pytest -q tests/test_shard.py`
Expected: FAIL met `ModuleNotFoundError: No module named 'tests._shard'`.

- [ ] **Stap 3: Implementeer `tests/_shard.py`** met `ENV`, `parse_shard` en `in_shard` volgens
de Interfaces. `parse_shard` gebruikt een `re.fullmatch(r"(\d+)/(\d+)", value)`; de foutmelding
noemt `ENV`, de waarde en de verwachte vorm `<i>/<n>`. Module-docstring in het stijl van
`tests/_domains.py`.

- [ ] **Stap 4: Implementeer `tests/conftest.py`** met twee hooks.
`pytest_configure(config)` leest `os.environ.get(ENV)`, zet een `ValueError` om in
`pytest.UsageError` en bewaart het resultaat in `config.stash[SHARD]`
(`SHARD = pytest.StashKey[tuple[int, int] | None]()`). De controle staat hier en niet in de
collectie: `pytest_configure` draait in het hoofdproces vóór de xdist-workers starten, dus een
foute waarde geeft één nette fout met exit 4 in plaats van crashende workers.
`pytest_collection_modifyitems(config, items)` leest `config.stash[SHARD]`; bij een deel
`items[:] = kept` en `config.hook.pytest_deselected(items=dropped)` als `dropped` niet leeg is.
Docstring: waarom (CI-verdeling, #151), en dat zonder variabele alles draait.

- [ ] **Stap 5: Draai en zie groen, en controleer de verdeling op de echte suite.**

Run: `uv run pytest -q tests/test_shard.py`
Expected: PASS.

Run: `for i in 1 2 3; do LIGHTHOUSE_PYTEST_SHARD=$i/3 uv run pytest -q -n auto | tail -1; done; uv run pytest -q -n auto | tail -1`
Expected: vier slotregels; de `passed`- plus `skipped`-aantallen van de eerste drie tellen op
tot die van de laatste (nu `3372 passed, 2 skipped`, plus de nieuwe tests), en geen deel is 0.
Onder xdist toont de slotregel `deselected` mogelijk niet; tel alleen `passed` en `skipped`.

Run: `LIGHTHOUSE_PYTEST_SHARD=0/3 uv run pytest -q; echo rc=$?`
Expected: een `UsageError` met `LIGHTHOUSE_PYTEST_SHARD`, `rc=4`.

- [ ] **Stap 6: Mutatiecheck.** Vervang tijdelijk `% total + 1` door `% total`: 
`test_every_test_in_exactly_one_shard` of `test_known_nodeid_lands_in_its_crc32_shard` faalt.
Terugdraaien. Vervang daarna tijdelijk `zlib.crc32(nodeid.encode())` door `hash(nodeid)`:
`test_known_nodeid_lands_in_its_crc32_shard` faalt. Terugdraaien.

- [ ] **Stap 7: Commit.**

```bash
git add tests/_shard.py tests/conftest.py tests/test_shard.py
git commit -m "feat(tests): verdeel de suite over delen met LIGHTHOUSE_PYTEST_SHARD (#151)"
```

### Taak 3: de `python`-job als matrix over drie delen

**Files:**
- Modify: `.github/workflows/ci.yml:1-4` (kopcommentaar), `.github/workflows/ci.yml:52-67` (job `python`)
- Test: `tests/test_ci_domains_pin.py`
- Create: `changelog.d/issue-151-ci-pytest-split.md`

**Interfaces:**
- Consumes: `ENV = "LIGHTHOUSE_PYTEST_SHARD"` en de vorm `<i>/<n>` uit taak 2.
- Produces: niets.

- [ ] **Stap 1: Schrijf de falende pin-test** in `tests/test_ci_domains_pin.py`:

```python
def test_python_job_runs_in_three_shards():
    """Fails when the python job stops splitting pytest, or runs the literal grep more than once."""
    python = _jobs()["python"]
    assert re.search(r"^      fail-fast: false\s*$", python, re.MULTILINE)
    assert re.search(r"^        shard: \[1, 2, 3\]\s*$", python, re.MULTILINE)
    steps = re.split(r"(?m)^      - ", python)[1:]
    pytest_steps = [s for s in steps if "uv run pytest -q -n auto" in s]
    assert len(pytest_steps) == 1, pytest_steps
    assert "LIGHTHOUSE_PYTEST_SHARD: ${{ matrix.shard }}/3" in pytest_steps[0]
    assert python.count("LIGHTHOUSE_PYTEST_SHARD") == 1
    grep = [s for s in steps if "ci/literal-grep.sh" in s]
    assert len(grep) == 1, grep
    assert re.match(r"if: matrix\.shard == 1\n        run: ci/literal-grep\.sh$", grep[0], re.MULTILINE), grep[0]
    assert python.count("ci/literal-grep.sh") == 1
```

De stappen worden geknipt zoals in `test_node_jobs_cache_npm`, zodat de `env` aantoonbaar op
de pytest-stap staat en niet op job-niveau of op een andere stap. `re.MULTILINE` is nodig:
de literal-grep-stap is de laatste van de job, en `_jobs()` rekent het commentaarblok vóór
`go:` (`ci.yml:69-73`) nog tot dat laatste stuk. Als het commentaar uit stap 3
`LIGHTHOUSE_PYTEST_SHARD` letterlijk noemt, faalt de telling `== 1`: noem in het commentaar
`tests/conftest.py`, niet de variabele.

- [ ] **Stap 2: Draai en zie rood.**

Run: `uv run pytest -q tests/test_ci_domains_pin.py::test_python_job_runs_in_three_shards`
Expected: FAIL op `fail-fast`.

- [ ] **Stap 3: Pas `ci.yml` aan.** Job `python`: `strategy:` met `fail-fast: false` en
`matrix: shard: [1, 2, 3]`; de pytest-stap krijgt
`env: LIGHTHOUSE_PYTEST_SHARD: ${{ matrix.shard }}/3`; `ci/literal-grep.sh` wordt
`- if: matrix.shard == 1` / `run: ci/literal-grep.sh`. Het bestaande commentaar over `-n auto`
blijft; erbij komt één zin: de suite is cores-gebonden (104 s op 4 vCPU, #151), `tests/conftest.py`
verdeelt hem op crc32 van de test-id. Kopcommentaar regel 1-4: Python draait in drie delen.

- [ ] **Stap 4: Draai de workflow-tests en zie groen.**

Run: `uv run pytest -q tests/test_ci_domains_pin.py tests/test_ci_go_test_pin.py tests/test_golangci_pin.py tests/test_domains.py`
Expected: PASS, `test_actions_pinned_by_sha` nog steeds met 18.

- [ ] **Stap 5: Changelogfragment** `changelog.d/issue-151-ci-pytest-split.md`:

```markdown
---
type: internal
---
CI no longer runs the Go and binary jobs for a change that touches only `tests/`, the root `README.md` or `CHANGELOG.md`, and runs the Python tests in three parallel parts.
```

- [ ] **Stap 6: Volledige suite.**

Run: `uv run pytest -q -n auto`
Expected: alles groen.

- [ ] **Stap 7: Commit.**

```bash
git add .github/workflows/ci.yml tests/test_ci_domains_pin.py changelog.d/issue-151-ci-pytest-split.md
git commit -m "feat(ci): python-job in drie delen (#151)"
```

De commit raakt `.github/workflows/ci.yml`, dus de test gate start `gate_node.py`. Zonder
`web/node_modules` meldt die `COULD NOT CHECK: web/node_modules missing`
(`.lighthouse/scripts/gate_node.py:171-172`); dat is verwacht en geen rood. Wie de web-checks
toch wil: `cd web && npm ci`, daarna de gate opnieuw.

- [ ] **Stap 8: Controleer het changelogfragment.** Pas na de commit: `ci/changelog.py check`
leest alleen fragmenten die in `base...HEAD` zijn toegevoegd (`ci/changelog.py:111-123`).

Run: `git fetch origin main && python3 ci/changelog.py check --base origin/main --head HEAD; echo rc=$?`
Expected: `rc=0`.

## Na de merge (geen taak in dit plan)

De meetcomment op #151 (laatste AC) vraagt twee echte PR-runs na de merge: één met alleen
`docs/` of `tests/`, één met een Python-wijziging. Die hoort bij `/lighthouse-close-issue`; de
PR-run van deze branch zelf telt als de Python-meting.
