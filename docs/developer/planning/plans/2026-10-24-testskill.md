# Testskill zonder tautologieën — implementatieplan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Doel:** een taalneutrale testskill `lighthouse-testing` in `pack/core`, python-writing daarop
aangesloten, en een rule tegen "klaar om te pushen als jij wilt" in het core-blok.

**Architectuur:** drie instructiewijzigingen, elk met een contracttest in pytest die de tekst
pint. De testskill en de rule komen in `pack/core` en worden via `init --upgrade` in lighthouse
zelf geïnstalleerd (dogfood). python-writing zit in `pack/python`, dat lighthouse niet zelf
installeert (`.lighthouse/manifest.toml`: `packages = ["core"]`).

**Tech stack:** Markdown-skills, pytest-contracttests, Go-test voor de gepinde bestandslijst,
`go run ./cmd/lighthouse init --upgrade`.

**Spec:** [2026-10-24-testskill-design.md](../specs/2026-10-24-testskill-design.md)

## Globale randvoorwaarden

- Alle instructietekst (skills, het core-blok) in het Engels; commitberichten in het Nederlands,
  Conventional Commits.
- Core-blok `pack/core/instructions/AGENTS.md`: ≤ 250 woorden (`tests/test_instruction_files.py`),
  met behoud van alle tokens die `test_core_block_word_budget` en
  `test_core_block_says_how_to_read_a_heading` pinnen.
- De rule letterlijk: `Finish what you are already authorised to do; never end on an offer to do it ("ready to push if you want" is not done).`
- Testskill: `origin: own`, `source: "#22"`, vers geschreven. Geen formulering van forge overnemen
  (geen licentie); agentsview en forge staan alleen als inspiratie-URL.
- Geen Go-, agentsview- of forge-specifieke termen in de testskill: `require.`, `Playwright`,
  `PostgreSQL`, `pgtest`, `Svelte`, `Huma`, `SSE`, `FTS`.
- Een verwijzing vanuit core naar een skill uit een taalpakket draagt "if present" in dezelfde
  alinea (`tests/pack/test_skill_contracts.py`).
- Na elke wijziging onder `pack/core`: `go run ./cmd/lighthouse init --upgrade`, de geïnstalleerde
  kopie mee committen; een tweede run meldt `No changes.`
- Shell-gates en Go vallen buiten deze PR (vervolgissues, taak 4).

## Reviewfocus

1. **Overlap van descriptions:** de testskill triggert op "test schrijven", python-writing op
   "pytest tests" en `superpowers:test-driven-development` op tests. Check 4 van
   `.lighthouse/scripts/check-instructions.sh` moet groen blijven (taak 1 en 2 draaien hem).
2. **Woordbudget na de rule:** inkorten mag geen gepind token breken (taak 3, bestaande tests
   plus de nieuwe rule-test).
3. **Variantdrift in python-writing:** de nieuwe tekst moet in `claude/` én `agents/` staan
   (taak 2: elke nieuwe test loopt over `VARIANTS`).
4. **Geïnstalleerde kopie loopt achter:** `.claude/skills/lighthouse-testing/`,
   `.agents/skills/lighthouse-testing/` en het blok in `AGENTS.md` (taak 1 en 3:
   `test_installed_core_block_matches_the_package` plus `init --upgrade` → `No changes.`).
5. **Nederlands in de skilltekst:** taak 1 krijgt een Engels-check zoals
   `test_python_writing.py::test_english`.

---

### Taak 1: testskill `lighthouse-testing` in core

**Bestanden:**
- Create: `pack/core/skills/testing/SKILL.md` (één gedeelde `SKILL.md`, zoals `instrument`)
- Create: `tests/pack/test_testing_skill.py`
- Modify: `pack/core/instructions/AGENTS.md` (skillregel)
- Modify: `internal/packs/core_test.go` (`TestCore…`-lijst, twee paden)
- Modify: `pack/core/requires.toml` (alleen voor tools die de tekst echt noemt)
- Modify: `pack/SOURCES.toml` (entry `new = true`)
- Generated: `.claude/skills/lighthouse-testing/SKILL.md`, `.agents/skills/lighthouse-testing/SKILL.md`,
  `AGENTS.md`, `.lighthouse/manifest.toml` (via `init --upgrade`)

**Interfaces:**
- Produces: skillnaam `lighthouse-testing`; kop `## Classify first` met de zin
  `` `This is a <class> change; the likely regression surface is <observable behavior>.` `` en de
  zes klassen `pure-logic`, `io/filesystem`, `cli-output`, `hook/gate`, `config`, `test-only`.
  Taak 2 verwijst naar die naam en kop.

- [ ] **Stap 1: schrijf de falende test** `tests/pack/test_testing_skill.py`

```python
from tests.pack._testkit import CORE

SKILL = (CORE / "skills" / "testing" / "SKILL.md").read_text(encoding="utf-8")
FRONT = SKILL.split("---\n", 2)[1]
CLASSES = ("pure-logic", "io/filesystem", "cli-output", "hook/gate", "config", "test-only")

def test_frontmatter():
    assert "name: lighthouse-testing\n" in FRONT
    assert "origin: own" in FRONT and 'source: "#22"' in FRONT
    assert "disable-model-invocation" not in FRONT  # model-invoked, triggert op werk

def test_description_triggers_on_the_work_and_on_planning():
    for word in ("writing", "fixing", "reviewing", "mock", "test strategy"):
        assert word in FRONT, word

def test_description_bounds_tdd_and_the_language_skill():
    assert "superpowers:test-driven-development" in FRONT
    assert "lighthouse-python-writing" in FRONT

def test_core_question_and_mutation_check():
    assert "What production change should make this test fail?" in SKILL
    assert "## Mutation check" in SKILL

def test_contract_tests_are_a_kind_of_their_own():
    assert "## Contract tests and code tests" in SKILL
    assert "lasting invariant" in SKILL

def test_classification_sentence_and_classes():
    assert "This is a <class> change; the likely regression surface is <observable behavior>." in SKILL
    for c in CLASSES:
        assert f"`{c}`" in SKILL, c

def test_language_neutral():
    for term in ("require.", "Playwright", "PostgreSQL", "pgtest", "Svelte", "Huma", "SSE", "FTS"):
        assert term not in SKILL, term

def test_english():
    text = SKILL.lower()
    for word in (" het ", " niet ", " een ", " wordt ", " dat ", " je "):
        assert word not in text, word
```

- [ ] **Stap 2: draai, verwacht FAIL** — `uv run pytest -q tests/pack/test_testing_skill.py`
  → `FileNotFoundError` op `SKILL.md`.

- [ ] **Stap 3: schrijf `pack/core/skills/testing/SKILL.md`** volgens spec §1, via
  `/lighthouse-instrument` (laag, herkomst) en `superpowers:writing-skills` (craft). Frontmatter
  zoals `pack/core/skills/instrument/SKILL.md` (`metadata: binding: default, origin: own`) plus
  `source: "#22"` onder `metadata`, zonder `disable-model-invocation`. Description met de
  negatieve grens in de vorm die check 4 accepteert (`NOTE — not for …`): de rood-groen-lus is
  `superpowers:test-driven-development`, het taalgereedschap staat in `lighthouse-python-writing`
  (if present). Secties: kernvraag, required checks (spec §1), `## Contract tests and code tests`,
  `## Classify first` (zin + tabel klasse/regressievraag/minimale check, plus "zin in de chat,
  vóór de eerste edit"), `## Mutation check`, `## Red flags`. De inspiratie-URL's onderaan.

- [ ] **Stap 4: aansluiten op core**
  - `pack/core/instructions/AGENTS.md`: regel ``- `/lighthouse-testing`: tests that fail when behaviour breaks.`` in de skilllijst.
  - `internal/packs/core_test.go`: `.agents/skills/lighthouse-testing/SKILL.md` en
    `.claude/skills/lighthouse-testing/SKILL.md` op hun gesorteerde plek in `want`.
  - `pack/core/requires.toml`: `"testing"` toevoegen bij elk tool dat
    `test_requires_names_the_skill_per_tool` eist (minstens `superpowers`, want de description
    noemt `superpowers:`).
  - `pack/SOURCES.toml`: `[[file]] path = "pack/core/skills/testing/SKILL.md"` met `new = true`.

- [ ] **Stap 5: draai, verwacht PASS**
  - `uv run pytest -q tests/pack/test_testing_skill.py tests/pack/test_skill_contracts.py tests/test_sources.py tests/test_instruction_files.py` → alles groen behalve eventueel `test_installed_core_block_matches_the_package` (komt in stap 6).
  - `go test ./internal/packs/... ./pack/...` → `ok`.
  - `bash .lighthouse/scripts/check-instructions.sh` → geen bevinding op `lighthouse-testing`.

- [ ] **Stap 6: dogfood** — `go run ./cmd/lighthouse init --upgrade`, daarna nogmaals → `No changes.`
  Dan de volledige gate: `uv run pytest -q && go test ./...` → groen.

- [ ] **Stap 7: commit**

```bash
git add pack/core tests/pack/test_testing_skill.py internal/packs/core_test.go pack/SOURCES.toml .claude/skills/lighthouse-testing .agents/skills/lighthouse-testing AGENTS.md .lighthouse/manifest.toml
git commit -m "feat(core): testskill lighthouse-testing zonder tautologieën (#22)"
```

### Taak 2: python-writing sluit aan op de testskill

**Bestanden:**
- Modify: `pack/python/skills/python-writing/claude/SKILL.md`
- Modify: `pack/python/skills/python-writing/agents/SKILL.md`
- Modify: `tests/pack/python/test_python_writing.py`

**Interfaces:**
- Consumes: `lighthouse-testing`, kop `## Classify first` (taak 1).

- [ ] **Stap 1: schrijf de falende tests** in `tests/pack/python/test_python_writing.py`, met de
  bestaande helpers `VARIANTS` en `_frontmatter`:

```python
def test_description_triggers_on_planning():
    for path in VARIANTS:
        front = _frontmatter(path)
        for word in ("refactor", "merging modules", "test strategy"):
            assert word in front, f"{path}: {word}"

def test_points_to_the_testing_skill():
    for path in VARIANTS:
        text = path.read_text(encoding="utf-8")
        assert "lighthouse-testing" in text, path
        assert "This is a <class> change" in text, f"{path}: the classification sentence"

def test_python_form_of_the_testing_norms():
    for path in VARIANTS:
        text = path.read_text(encoding="utf-8")
        assert "## The Python form of lighthouse-testing" in text, path
        for tool in ("pytest.mark.parametrize", "create_autospec", "tmp_path", "subprocess.run"):
            assert tool in text, f"{path}: {tool}"

def test_reporting_done_runs_every_kpi_row():
    for path in VARIANTS:
        text = path.read_text(encoding="utf-8")
        assert "## Reporting done" in text, path
        section = text.split("## Reporting done", 1)[1].split("\n## ", 1)[0]
        for word in ("command", "outcome", "extras", "not measurable"):
            assert word in section, f"{path}: {word}"

def test_pairing_names_the_testing_skill():
    for path in VARIANTS:
        pairing = path.read_text(encoding="utf-8").split("## The pairing", 1)[1].split("\n## ", 1)[0]
        assert "lighthouse-testing" in pairing, path
```

- [ ] **Stap 2: draai, verwacht FAIL** — `uv run pytest -q tests/pack/python/test_python_writing.py`
  → de vijf nieuwe tests falen, de bestaande blijven groen.

- [ ] **Stap 3: pas beide varianten aan** volgens spec §2:
  - description: plannen als trigger (refactor, merging modules, choosing a test strategy); de
    bestaande `NOT for … lighthouse-shell-gates` blijft;
  - in `## Domain` of vlak erna: vóór het editen de classificatiezin, de klassen in
    `lighthouse-testing` (sectie *Classify first*);
  - nieuwe sectie `## The Python form of lighthouse-testing`, na *Merging or deepening*: de vier
    punten uit spec §2, geframed als gereedschap voor de norm uit de testskill (het principe
    "norms in `.claude/rules/python.md`" blijft staan);
  - nieuwe sectie `## Reporting done`, na *Non-negotiable*: alle KPI-rijen draaien, per rij
    commando, uitkomst, gesynchroniseerde extras en wat niet meetbaar was (rij 4 zonder
    ratchetbestanden = not measurable);
  - pairing-tabel: rij `lighthouse-testing` | the test norms and the class table | for every test,
    and before the first edit.

  Het verschil tussen de varianten blijft alleen de OpenCode-tekst.
  Kop `## Reaching a complexity limit: deepen, don't split` en de KPI-tabel niet aanraken.

- [ ] **Stap 4: draai, verwacht PASS**
  - `uv run pytest -q tests/pack/python/ tests/pack/test_skill_contracts.py` → groen.
  - `bash .lighthouse/scripts/check-instructions.sh` → geen nieuwe bevinding.
  - `go run ./cmd/lighthouse init --upgrade` → `No changes.` (python wordt hier niet geïnstalleerd).

- [ ] **Stap 5: commit**

```bash
git add pack/python/skills/python-writing tests/pack/python/test_python_writing.py
git commit -m "feat(python): python-writing sluit aan op lighthouse-testing (#22)"
```

### Taak 3: de rule in het core-blok

**Bestanden:**
- Modify: `pack/core/instructions/AGENTS.md`
- Modify: `tests/test_instruction_files.py`
- Generated: `AGENTS.md` (via `init --upgrade`)

- [ ] **Stap 1: schrijf de falende test** in `tests/test_instruction_files.py`:

```python
def test_core_block_says_finish_what_is_authorised():
    """#22: an agent does not hand back an action it already has permission for."""
    text = " ".join((CORE / "instructions" / "AGENTS.md").read_text(encoding="utf-8").split())
    assert (
        'Finish what you are already authorised to do; never end on an offer to do it '
        '("ready to push if you want" is not done).'
    ) in text
```

- [ ] **Stap 2: draai, verwacht FAIL** — `uv run pytest -q tests/test_instruction_files.py::test_core_block_says_finish_what_is_authorised`.

- [ ] **Stap 3: voeg de rule toe en kort in tot ≤ 250 woorden.** De rule als bullet bij de
  regels, na de bullet over artefacten. Inkorten: de bullet over `heading:<key>` (behoud de drie
  zinsdelen die `test_core_block_says_how_to_read_a_heading` pint en `.lighthouse/headings.toml`)
  en de omschrijvingen in de skilllijst. Geen regel verliest zijn betekenis; geen skill verdwijnt
  uit de lijst.

- [ ] **Stap 4: draai, verwacht PASS** — `uv run pytest -q tests/test_instruction_files.py`
  (budget, headings-test, nieuwe rule-test; `test_installed_core_block_matches_the_package` na stap 5).

- [ ] **Stap 5: dogfood** — `go run ./cmd/lighthouse init --upgrade`, nogmaals → `No changes.`;
  volledige gate `uv run pytest -q && go test ./...` → groen.

- [ ] **Stap 6: commit**

```bash
git add pack/core/instructions/AGENTS.md tests/test_instruction_files.py AGENTS.md .lighthouse/manifest.toml
git commit -m "feat(core): rule — maak af waarvoor je al toestemming hebt (#22)"
```

### Taak 4: vervolgissues

Geen code. Via `/lighthouse-write-issue`, in het Nederlands, elk met `Refs #22`:

- [ ] **Issue shell-gates:** `pack/shell/skills/shell-gates` (beide varianten) verwijst naar
  `lighthouse-testing`, vraagt de classificatiezin, en geeft de shell-vorm van de normen. Inhoud uit
  spec §*Vervolg*.
- [ ] **Issue Go-skill (`pack/go`):** inhoud uit spec §*Vervolg*, met het concept "Go schrijven in
  deze repo" uit de body van #22 als startpunt en de correctie `-shuffle=on` / `-count=1`.
- [ ] Noteer beide nummers voor de PR-body (`/lighthouse-verify-pr`).
