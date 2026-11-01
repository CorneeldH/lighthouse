# Uitgestelde reviewpunten van #151 — implementatieplan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Doel:** de zes kleine reviewpunten uit PR #162 opruimen en de ontbrekende CI-meting van #151 als comment vastleggen.

**Architectuur:** vier codewijzigingen in bestaande bestanden: `ci/domains.py` (rootbestanden uit Go), `tests/test_domains.py` (bewaking op Go-leesacties), `tests/conftest.py` (pytest-header) en `tests/_shard.py` met `tests/test_shard.py` (testopruiming). Daarna een changelog-fragment en één GitHub-comment met meetcijfers.

**Tech stack:** Python 3.11 (stdlib), pytest met pytest-xdist, `gh`.

**Spec:** geen aparte spec; het issue is de spec (#163, met de acceptatiecriteria). Achtergrond: [spec van #151](../specs/2026-10-31-issue-151-ci-pytest-split-design.md).

## Globale randvoorwaarden

- Code, commentaar en testnamen in het Engels; commitberichten, comment en dit plan in het Nederlands.
- `ci/domains.py` blijft stdlib-only, Python 3.11.
- Zonder `LIGHTHOUSE_PYTEST_SHARD` verandert er lokaal niets: geen extra headerregel, geen deselectie.
- `AGENTS.md` en `CLAUDE.md` blijven in het Go-domein (de dogfood-check leest ze).
- Testcommando: `uv run pytest -q tests/test_domains.py tests/test_shard.py tests/test_ci_domains_pin.py`.

## Review Focus

1. Een Go-test die via losse delen een fixture leest (`filepath.Join("..", "..", "tests", "pack", "fixtures", …)`) mag de bewaking niet laten falen. Task 2 pint dat.
2. De echte bestaande leesactie `filepath.Join("..", "dispatch", "testdata", "snapshot.json")` (`internal/cli/gather_test.go:23`) mag de bewaking niet laten falen. Task 2 draait de bewaking over de hele repo.
3. `filepath.Join(repo, "README.md")` (tijdelijke repo, geen `".."`) blijft geen treffer. Bestaand negatief geval in Task 2 blijft staan.
4. Een padfragment `"tests/x.py"` als één deel na `".."` is ook een leesactie. Task 2 pint dat.
5. Een PR die `CONTRIBUTING.md` samen met een Go-bestand raakt, start nog steeds Go. Task 1 pint dat.

---

### Task 1: losse `.md`-bestanden in de root uit het Go-domein

AC 1. Gemeten: de drie bestanden staan in de root, geen `.go`-bestand noemt ze, en geen `go:embed` bereikt de root. Dus de eerste tak van het AC: ze gaan uit Go.

**Files:**
- Modify: `ci/domains.py:32-38`
- Modify: `tests/test_domains.py:16-20` (regex), `:28-57` (cases), `:95-101` (docstring)

**Interfaces:**
- Produces: module-constante `_ROOT_MD` in `tests/test_domains.py`, een regex-alternatie van de vijf rootbestanden: `r"README\.md|CHANGELOG\.md|CONTRIBUTING\.md|SECURITY\.md|CLA\.md"`. Task 2 gebruikt hem.

- [ ] **Step 1: testgevallen toevoegen aan `test_classify`**

```python
(["CONTRIBUTING.md"], {"python"}),
(["SECURITY.md"], {"python"}),
(["CLA.md"], {"python"}),
(["CONTRIBUTING.md", "internal/a.go"], {"go", "python"}),
(["AGENTS.md"], {"go", "python"}),
```

En in `test_repo_reads_finds_a_read_above_the_package` drie extra literals in `text`, met de verwachte lijst uitgebreid in dezelfde volgorde: `"../../CONTRIBUTING.md"`, `` `../SECURITY.md` ``, `"../CLA.md"`.

- [ ] **Step 2: draaien, verwacht FAIL**

Run: `uv run pytest -q tests/test_domains.py`
Expected: FAIL op de drie nieuwe `classify`-gevallen (`{'go', 'python'} != {'python'}`) en op `test_repo_reads_finds_a_read_above_the_package`.

- [ ] **Step 3: implementeren**

- `ci/domains.py`: `CONTRIBUTING.md`, `SECURITY.md` en `CLA.md` in `_NOT_GO`. Commentaar l.32-34 bijwerken: "Only the loose root .md files (README.md, CHANGELOG.md, CONTRIBUTING.md, SECURITY.md, CLA.md) leave go; AGENTS.md and CLAUDE.md stay, the dogfood check reads them." Meetregel erbij: "Measured again for #163: no .go file names CONTRIBUTING, SECURITY or CLA.md."
- `tests/test_domains.py`: `_ROOT_MD` definiëren en in `_REPO_READ` de alternatie `README\.md|CHANGELOG\.md` vervangen door `(?:{_ROOT_MD})` (f-string of concatenatie). Commentaar boven de regex en de docstring van `test_no_go_file_reads_tests_or_root_md` noemen "the loose root .md files" in plaats van "README.md or CHANGELOG.md".

- [ ] **Step 4: draaien, verwacht PASS**

Run: `uv run pytest -q tests/test_domains.py`
Expected: alles groen, ook `test_no_go_file_reads_tests_or_root_md` (geen Go-bestand leest de drie).

- [ ] **Step 5: commit**

```bash
git add ci/domains.py tests/test_domains.py
git commit -m "fix(ci): CONTRIBUTING.md, SECURITY.md en CLA.md starten geen Go-jobs meer (#163)"
```

### Task 2: bewaking ziet ook paden uit losse delen

AC 2.

**Files:**
- Modify: `tests/test_domains.py:16-25` (tweede patroon, `_repo_reads`), `:80-101` (tests, docstring)

**Interfaces:**
- Consumes: `_ROOT_MD` uit Task 1.
- Produces: `_repo_reads(text: str) -> list[str]` geeft eerst alle treffers van `_REPO_READ` (zoals nu), daarna die van het nieuwe `_REPO_READ_PARTS`, elk als de tekst van de match.

- [ ] **Step 1: falende tests schrijven**

```python
def test_repo_reads_finds_a_read_built_from_parts():
    text = (
        'filepath.Join("..", "..", "README.md")\n'
        'filepath.Join("..", "..", "tests", "test_x.py")\n'
        'filepath.Join("..", "CHANGELOG.md")\n'
        'filepath.Join("..", "..", "tests/test_x.py")\n'
    )
    assert len(_repo_reads(text)) == 4


def test_repo_reads_parts_skip_fixtures_and_other_dirs():
    text = (
        'filepath.Join("..", "..", "tests", "pack", "fixtures", "protocol", "headers.json")\n'
        'filepath.Join("..", "..", "tests/pack/fixtures/protocol/headers.json")\n'
        'filepath.Join("..", "dispatch", "testdata", "snapshot.json")\n'
        'filepath.Join(repo, "tests", "test_x.py")\n'
    )
    assert _repo_reads(text) == []
```

`len(...) == 4` en niet de exacte strings: het AC vraagt dat de bewaking faalt, niet een vorm van de match.

- [ ] **Step 2: draaien, verwacht FAIL**

Run: `uv run pytest -q tests/test_domains.py -k parts`
Expected: FAIL in `test_repo_reads_finds_a_read_built_from_parts` (`0 == 4`); de skip-test slaagt al.

- [ ] **Step 3: `_REPO_READ_PARTS` implementeren**

Eén of meer `".."`-delen (komma en witruimte ertussen), direct gevolgd door een deel dat de root raakt:

```python
_REPO_READ_PARTS = re.compile(
    r'(?:"\.\."\s*,\s*)+"(?:'
    r'tests"(?!\s*,\s*"pack"\s*,\s*"fixtures")'
    r'|tests/(?!pack/fixtures/)[^"]*"'
    rf'|(?:{_ROOT_MD})"'
    r")"
)
```

`_repo_reads` wordt `_REPO_READ.findall(text) + [m.group(0) for m in _REPO_READ_PARTS.finditer(text)]`. Commentaar boven het patroon: waarom het bestaat (`filepath.Join`-stijl, `internal/cli/gather_test.go:23`). In de docstring van `test_no_go_file_reads_tests_or_root_md` vervalt de zin "A path built from parts … is not seen"; wat overblijft: een pad uit een variabele plus delen (`filepath.Join(root, "tests", …)`) ziet hij niet.

- [ ] **Step 4: draaien, verwacht PASS**

Run: `uv run pytest -q tests/test_domains.py`
Expected: alles groen, ook de bewaking over de hele repo (Review Focus 2).

- [ ] **Step 5: mutatiecheck**

Haal tijdelijk de lookahead `(?!\s*,\s*"pack"\s*,\s*"fixtures")` weg: `test_repo_reads_parts_skip_fixtures_and_other_dirs` moet falen. Zet hem terug.

- [ ] **Step 6: commit**

```bash
git add tests/test_domains.py
git commit -m "test(ci): bewaking op Go-leesacties ziet ook paden uit losse delen (#163)"
```

### Task 3: pytest-header toont het deel

AC 3.

**Files:**
- Modify: `tests/conftest.py` (nieuwe hook)
- Test: `tests/test_shard.py`

**Interfaces:**
- Consumes: `SHARD`-stash uit `pytest_configure`, `ENV` uit `tests/_shard.py`.
- Produces: `pytest_report_header(config) -> list[str]`: `[f"{ENV}={index}/{total}"]` met een deel, `[]` zonder.

- [ ] **Step 1: falende tests schrijven**

```python
def test_report_header_names_the_shard(monkeypatch):
    monkeypatch.setenv(ENV, "2/3")
    config, _ = _config()
    conftest.pytest_configure(config)
    assert conftest.pytest_report_header(config) == [f"{ENV}=2/3"]


def test_report_header_empty_without_env(monkeypatch):
    monkeypatch.delenv(ENV, raising=False)
    config, _ = _config()
    conftest.pytest_configure(config)
    assert conftest.pytest_report_header(config) == []
```

- [ ] **Step 2: draaien, verwacht FAIL**

Run: `uv run pytest -q tests/test_shard.py -k report_header`
Expected: FAIL met `AttributeError: module 'tests.conftest' has no attribute 'pytest_report_header'`.

- [ ] **Step 3: `pytest_report_header` implementeren in `tests/conftest.py`**

Leest `config.stash[SHARD]`. Commentaar: under xdist the closing line lacks `N deselected`; this hook runs in the main process, so the header names the part there too.

- [ ] **Step 4: draaien, verwacht PASS**

Run: `uv run pytest -q tests/test_shard.py`
Expected: groen.

- [ ] **Step 5: controle onder xdist**

Run: `LIGHTHOUSE_PYTEST_SHARD=2/3 uv run pytest -n auto tests/test_shard.py | head -5`
Expected: een headerregel `LIGHTHOUSE_PYTEST_SHARD=2/3` boven de testuitvoer. Zonder de variabele staat die regel er niet.

- [ ] **Step 6: commit**

```bash
git add tests/conftest.py tests/test_shard.py
git commit -m "test(ci): pytest-header toont welk deel draait, ook onder xdist (#163)"
```

### Task 4: shardtests aanscherpen

AC 4, 5 en 6.

**Files:**
- Modify: `tests/_shard.py:17`
- Modify: `tests/test_shard.py:20` (invalid-lijst), `:33-36` (test zonder dubbele assert), nieuwe test

- [ ] **Step 1: tests aanpassen**

- `test_parse_invalid`: `"１/３"` (fullwidth) toevoegen aan de lijst.
- `test_shards_are_stable_and_not_empty` wordt `test_shards_are_not_empty`, met alleen `assert all([n for n in NODEIDS if in_shard(n, i, 3)] for i in (1, 2, 3))`.
- Nieuw:

```python
def test_one_shard_keeps_every_test():
    assert all(in_shard(n, 1, 1) for n in NODEIDS)
```

- [ ] **Step 2: draaien, verwacht FAIL**

Run: `uv run pytest -q tests/test_shard.py`
Expected: FAIL alleen op `test_parse_invalid[１/３]` (DID NOT RAISE). `test_one_shard_keeps_every_test` slaagt meteen: hij pint bestaand gedrag.

- [ ] **Step 3: implementeren**

`tests/_shard.py:17`: `\d` wordt `[0-9]`.

- [ ] **Step 4: draaien, verwacht PASS**

Run: `uv run pytest -q tests/test_shard.py`
Expected: groen.

- [ ] **Step 5: mutatiecheck**

Maak `in_shard` tijdelijk `return zlib.crc32(nodeid.encode()) % total == index`: `test_one_shard_keeps_every_test` moet falen. Zet hem terug.

- [ ] **Step 6: commit**

```bash
git add tests/_shard.py tests/test_shard.py
git commit -m "test(ci): shardtests zonder dubbele assert, ASCII-cijfers, 1/1 vastgelegd (#163)"
```

### Task 5: changelog-fragment en de meting van #151

AC 7, en de fragmentregel uit `AGENTS.md`.

**Files:**
- Create: `changelog.d/issue-163-review-punten-151.md`

- [ ] **Step 1: fragment schrijven**

Frontmatter `type: changed`, één Engelse alinea, in de stijl van `changelog.d/fix-loop-page-2.md`: a PR that touches only `CONTRIBUTING.md`, `SECURITY.md` or `CLA.md` no longer starts the Go jobs, and a CI pytest part names itself in the header.

- [ ] **Step 2: commit**

```bash
git add changelog.d/issue-163-review-punten-151.md
git commit -m "docs(changelog): fragment voor #163"
```

- [ ] **Step 3: een PR met alleen `docs/` of `tests/` zoeken**

Deze PR telt niet: `ci/domains.py` staat in `_WEB_TOO` en start alle jobs. Zoek een gemergede PR na #162 waarvan alle bestanden onder `docs/` of `tests/` vallen (`changelog.d/` mag erbij), met een run op de nieuwe `ci.yml`:

```bash
gh pr list --state merged --limit 30 --json number,mergedAt,files \
  --jq '.[] | select(all(.files[].path; startswith("docs/") or startswith("tests/") or startswith("changelog.d/"))) | .number'
```

Haal per kandidaat de run en de jobduur op met `gh run list --branch <head> --workflow ci.yml` en `gh run view <run> --json jobs`. Geen kandidaat? Dan zit er nog niets op de plank: meld dat in de PR met `Refs #163 — meting wacht op een docs-PR` en laat AC 7 open.

- [ ] **Step 4: comment op #151**

Eén comment, in het Nederlands, met een tabel: per PR het runnummer, de totale duur en de duur van elke job. Rijen: de gevonden docs/tests-PR en #162 (run `41230413149`: 128 s; `python (1)` 46 s, `python (2)` 55 s, `python (3)` 54 s, `binary` 115 s, `go` 90 s, `go-test` 78 s, `go-e2e` 85 s). Ernaast de cijfers uit #151: `binary` 109–134 s, `python` 98–111 s.

```bash
gh issue comment 151 --body-file <bestand>
```
