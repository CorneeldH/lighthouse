# De loop-kaart verandert in de PR die het werk levert — implementatieplan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Doel:** de loop-kaart (`docs/site/data/loop.json`) wordt bijgewerkt in de PR die een `loop`-stap
levert, en een PR die een `loop`-issue sluit zonder de kaart bij te werken wordt rood.

**Architectuur:** `docs/developer/labels.md` koppelt per vrij label een document. `write-issue`
zet het label na een toets tegen dat document; `write-pr` werkt het document bij voor elk
`Closes`/`Refs` met zo'n label. `ci/loop_roadmap.py` krijgt een PR-modus naast de hoofdmodus.
Eerst wordt alles van *chain* naar *loop* hernoemd.

**Tech stack:** Python 3.11 stdlib (`ci/`), pytest via `uv run pytest`, GitHub Actions, `gh api`
(REST en GraphQL), Markdown-skills in `pack/core`.

**Spec:** [2026-11-01-loop-kaart-bij-pr-design.md](../specs/2026-11-01-loop-kaart-bij-pr-design.md)

## Globale beperkingen

- Werk in `/home/corneel/repositories/lighthouse/.claude/worktrees/loop-kaart-bij-pr`, branch
  `fix-loop-kaart-bij-pr`; prefix elk shellcommando met `cd <worktree>`.
- Commits als Conventional Commits, in het Nederlands; skilltekst, code, comments en
  foutmeldingen in het Engels.
- `pack/core/skills/write-pr/SKILL.md` en `pack/core/skills/write-issue/{claude,agents}/SKILL.md`
  noemen geen labelnaam (`loop`) en geen pad (`loop.json`): ze lezen het taxonomiebestand via
  `python3 .lighthouse/lib/lighthouse_lib/config.py paths.labels`.
- De twee varianten van `write-issue` verschillen alleen in de gegenereerde regel
  (`test_variants_differ_only_in_the_generated_line`): elke wijziging gaat in beide.
- Na een wijziging onder `pack/`: `go run ./cmd/lighthouse init --upgrade` en de geïnstalleerde
  kopie (`.claude/skills/`, `.agents/skills/`, `.lighthouse/manifest.toml`) mee committen.
- Historische stukken (`CHANGELOG.md`, bestaande specs en plannen onder
  `docs/developer/planning/`) blijven ongewijzigd.
- `ci/loop_roadmap.py`: exit 0 ok, 1 een oordeel, 2 `COULD NOT CHECK`.

## Review focus

1. Een PR die twee `loop`-issues sluit en er één bijwerkt: alleen het andere wordt gemeld
   (taak 2, `test_pr_flags_only_the_issue_left_planned`).
2. Een PR die een `loop`-issue sluit dat een nieuwe stap toevoegt (geen `planned` item, wel
   `loop.json` geraakt): groen (taak 2, `test_pr_new_step_added_as_works_is_clean`).
3. Een PR die een issue met een ander label sluit: geen melding, ook als `loop.json` niet geraakt
   is (taak 2, `test_pr_ignores_issues_without_the_label`).
4. `gh` faalt in PR-modus: exit 2 met `COULD NOT CHECK`, geen vals groen (taak 2,
   `test_pr_mode_gh_failure_could_not_check`).
5. Een re-run van `write-pr` schrijft de kaart niet twee keer bij (taak 6, tekst-test op de
   re-run-regel).

---

### Taak 1: Hernoemen chain → loop

**Bestanden:**
- Hernoem (`git mv`): `docs/site/data/chain.json` → `docs/site/data/loop.json`,
  `ci/chain_roadmap.py` → `ci/loop_roadmap.py`, `.github/workflows/chain-roadmap.yml` →
  `.github/workflows/loop-roadmap.yml`, `tests/test_chain_roadmap.py` →
  `tests/test_loop_roadmap.py`, `tests/test_site_chain.py` → `tests/test_site_loop.py`
- Wijzig: `docs/site/loop.js:1,67`, `tests/_site_node.py`, `tests/test_agent_guide.py`,
  `tests/test_site_{config,packs,reference,pages,route_follow,session,system,download,sorts,repo}.py`,
  `tests/fixtures/site/map.json:3`

**Interfaces:**
- Produceert: `tests/test_site_loop.py` met dezelfde exports als nu (`ROOT`, `SITE`, `REPO_BLOB`,
  `REPO_TREE`, `load`); `load("loop.json")`; `ci/loop_roadmap.py` met `LABEL = "loop"` en
  `LOOP = .../docs/site/data/loop.json`.

- [ ] **Stap 1:** `git mv` de vijf bestanden.
- [ ] **Stap 2:** vervang elke verwijzing naar de oude bestandsnamen:
  `rg -l 'chain\.json|chain_roadmap|chain-roadmap|test_site_chain' --glob '!docs/developer/planning/**' --glob '!CHANGELOG.md'`
  moet daarna leeg zijn. In `ci/loop_roadmap.py` wordt `LABEL = "loop"` en de constante `CHAIN`
  heet `LOOP`; docstrings en workflowcomment zeggen *loop map* en label `loop`. Lokale
  variabelen die `chain` heten mogen blijven.
- [ ] **Stap 3:** in `tests/test_loop_roadmap.py` worden de verwachte meldingen
  `... labelled loop` (zelfde tests, nieuw label).
- [ ] **Stap 4:** draai `uv run pytest -q tests/test_site_*.py tests/test_loop_roadmap.py tests/test_agent_guide.py`.
  Verwacht: alles groen.
- [ ] **Stap 5:** commit `refactor(site): de loop-kaart heet loop.json, de check loop_roadmap`.

### Taak 2: De check: PR-modus en een kleinere hoofdmodus

**Bestanden:**
- Wijzig: `ci/loop_roadmap.py`, `.github/workflows/loop-roadmap.yml`
- Test: `tests/test_loop_roadmap.py`

**Interfaces:**
- Consumeert: `planned(loop: dict) -> dict[str, int | None]` (bestaat).
- Produceert:
  - `problems(loop: dict, open_issues: set[int]) -> list[str]` — alleen nog "planned without an
    issue" en "#N is not an open issue labelled loop"; de melding "labelled loop, but not on the
    map" vervalt.
  - `pr_problems(loop: dict, closed: set[int], touched: set[str]) -> list[str]` — `closed` zijn de
    issues met label `loop` die de PR sluit, `touched` de gewijzigde paden van de PR.
  - `closed_loop_issues(repo: str, pr: int) -> set[int]` — GraphQL `closingIssuesReferences` van
    de PR, gefilterd op label `loop`; `CalledProcessError` bij een falende `gh`.
  - `touched_files(repo: str, pr: int) -> set[str]` — `gh api repos/{repo}/pulls/{pr}/files --paginate --jq '.[].filename'`.
  - `main(argv: list[str] | None = None) -> int` — `--pr <n>` kiest de PR-modus.

- [ ] **Stap 1: falende tests.** Vervang `test_labelled_issue_missing_from_the_map_is_flagged` door
  `test_open_issue_missing_from_the_map_is_not_flagged` (`problems(data, {7, 9}) == []` met alleen
  #7 op de kaart) en voeg toe:

```python
MAP = "docs/site/data/loop.json"

def test_pr_closing_a_planned_item_without_update_is_flagged():
    data = chain({"id": "b", "status": "planned", "issue": 7})
    assert roadmap.pr_problems(data, {7}, {MAP}) == [
        "b: still planned, but this PR closes #7; set status works and drop issue "
        "(see docs/developer/labels.md)"
    ]

def test_pr_closing_without_touching_the_map_is_flagged():
    data = chain({"id": "b", "status": "works"})
    assert roadmap.pr_problems(data, {7}, {"ci/x.py"}) == [
        "#7: this PR closes a loop issue but leaves docs/site/data/loop.json untouched "
        "(see docs/developer/labels.md)"
    ]

def test_pr_flags_only_the_issue_left_planned():
    data = chain({"id": "a", "status": "works"}, {"id": "b", "status": "planned", "issue": 8})
    assert roadmap.pr_problems(data, {7, 8}, {MAP}) == [
        "b: still planned, but this PR closes #8; set status works and drop issue "
        "(see docs/developer/labels.md)"
    ]

def test_pr_new_step_added_as_works_is_clean():
    assert roadmap.pr_problems(chain({"id": "new", "status": "works"}), {7}, {MAP}) == []

def test_pr_ignores_issues_without_the_label():
    # closed holds only loop-labelled issues; a PR closing none is clean whatever it touches.
    assert roadmap.pr_problems(chain({"id": "b", "status": "planned", "issue": 7}), set(), set()) == []

def test_pr_mode_gh_failure_could_not_check(monkeypatch, capsys):
    def fail(*_a, **_k):
        raise subprocess.CalledProcessError(1, "gh")
    monkeypatch.setattr(roadmap, "closed_loop_issues", fail)
    assert roadmap.main(["--pr", "5"]) == 2
    assert "COULD NOT CHECK" in capsys.readouterr().err
```

  Voeg ook `test_closed_loop_issues_keeps_only_the_label` toe: monkeypatch `subprocess.run` op een
  GraphQL-antwoord met issue 7 (labels `loop`, `bug`) en 8 (label `bug`); verwacht `{7}`.
- [ ] **Stap 2:** `uv run pytest -q tests/test_loop_roadmap.py`. Verwacht: de nieuwe tests FAIL
  (`pr_problems` bestaat niet).
- [ ] **Stap 3:** implementeer de functies hierboven. Volgorde in `pr_problems`: eerst de items die
  nog `planned` staan (op volgorde van `planned()`), dan, als `MAP` niet in `touched` staat, één
  regel per issue in `sorted(closed)` dat geen gemelde `planned`-regel kreeg. De hoofdmodus-melding
  "#N is not an open issue labelled loop" krijgt dezelfde verwijzing
  `(see docs/developer/labels.md)`. `main` leest `GITHUB_REPOSITORY` zoals nu; groen in PR-modus
  print `loop roadmap: PR #<n> closes <k> loop issues, map in step`.
- [ ] **Stap 4:** `uv run pytest -q tests/test_loop_roadmap.py`. Verwacht: PASS.
- [ ] **Stap 5: workflow.** In `loop-roadmap.yml`: `pull_request` met
  `types: [opened, synchronize, reopened, edited]` en zonder `paths`; `permissions` krijgt
  `pull-requests: read`; de stap draait
  `python3 ci/loop_roadmap.py ${{ github.event_name == 'pull_request' && format('--pr {0}', github.event.pull_request.number) || '' }}`.
  `issues`, `push` (paden naar `loop.json` en `ci/loop_roadmap.py`) en `workflow_dispatch` blijven.
- [ ] **Stap 6:** `python3 ci/loop_roadmap.py`. Verwacht nu nog exit 1 met de twee #119-regels
  (taak 3 lost ze op) — en geen regel "not on the map".
- [ ] **Stap 7:** commit `feat(ci): loop-roadmap wordt rood in de PR die een loop-issue sluit`.

### Taak 3: Herstel van #119 op de kaart

**Bestanden:**
- Wijzig: `docs/site/data/loop.json` (chip `roborev`, handoff `pr-and-ci → fix-pr`)
- Test: `tests/test_site_loop.py` (`test_works_handoffs_follow_the_handoff_table`)

- [ ] **Stap 1: falende test.** In `test_works_handoffs_follow_the_handoff_table` komt
  `("pr-and-ci", "fix-pr")` in de verwachte `works`-set. De tabelcontrole slaat handoffs over die
  de dispatcher start:

```python
# Handoffs the dispatcher starts itself, not through a handoff block.
DISPATCHED = {("pr-and-ci", "fix-pr")}
...
    judge = (ROOT / "internal" / "dispatch" / "judge.go").read_text(encoding="utf-8")
    for source, target in works:
        if (source, target) in DISPATCHED:
            assert f'"/lighthouse-{target}"' in judge, target
            continue
        ...  # bestaande tabelcontrole
```

- [ ] **Stap 2:** `uv run pytest -q tests/test_site_loop.py -k handoff_table`. Verwacht: FAIL (set
  verschilt).
- [ ] **Stap 3:** in `loop.json`: chip `roborev` krijgt `"status": "works"`, verliest `issue`,
  krijgt `"source": "https://github.com/CorneeldH/lighthouse/blob/main/pack/core/scripts/roborev_gate.py"`;
  handoff `pr-and-ci → fix-pr` krijgt `"status": "works"` en verliest `issue`.
- [ ] **Stap 4:** `uv run pytest -q tests/test_site_*.py tests/test_loop_roadmap.py && python3 ci/loop_roadmap.py`.
  Verwacht: tests PASS; de check exit 1 alleen omdat het label nog `chain` heet (zie taak 7), of
  exit 0 als het label al hernoemd is.
- [ ] **Stap 5:** commit `fix(site): roborev en de handoff naar fix-pr werken sinds #119`.

### Taak 4: `labels.md` — het document per vrij label

**Bestanden:**
- Wijzig: `docs/developer/labels.md` (de alinea over het vrije label, regels 20–25)

- [ ] **Stap 1:** vervang de alinea door een sectie *Vrije labels met een document* met deze tabel:

| Label | Kleur | When | Document |
|---|---|---|---|
| `loop` | `F4D74B` | Voegt een stap, handoff of route van de loop toe of verandert die | `docs/site/data/loop.json` |

  De koppen `When` en `Document` zijn Engels: de skills zoeken ze (taak 5, 6).
  Eronder: wat een document hebben betekent (`write-issue` toetst aan *When*, `write-pr` werkt
  *Document* bij); wat bijwerken voor `loop.json` is (een `planned` item met het issuenummer wordt
  `works` en verliest `issue`; een nieuwe stap of handoff komt erbij als `works`, met `source`);
  dat een nieuw `loop`-issue alleen het label krijgt en pas met de leverende PR op de kaart komt;
  en wat `ci/loop_roadmap.py` en de workflow `loop-roadmap` controleren (PR-modus bij `Closes`,
  hoofdmodus als vangnet). Uitleg over de controller of een verbetering binnen een bestaande stap
  krijgt het label niet (bestaande zin, blijft).
- [ ] **Stap 2:** `rg -n 'chain' docs/developer/labels.md`. Verwacht: geen treffer.
- [ ] **Stap 3:** commit `docs(labels): vrij label loop met het document dat een PR bijwerkt`.

### Taak 5: `write-issue` — stap 3f

**Bestanden:**
- Wijzig: `pack/core/skills/write-issue/claude/SKILL.md`, `pack/core/skills/write-issue/agents/SKILL.md`
  (nieuwe `#### 3f. Free labels with a document`, na 3e, vóór `### 4. Create the issue`)
- Test: `tests/pack/test_write_issue_skill.py`

- [ ] **Stap 1: falende test.**

```python
def test_free_labels_with_a_document():
    """A free label whose taxonomy row names a document is set after reading that document."""
    for body in skill():
        step = _between(body, "#### 3f. Free labels with a document", "### 4. Create the issue")
        flat = " ".join(step.split())
        assert "`Document` column" in flat and "`When` column" in flat
        assert "open that document" in flat
        assert "no such row: skip this step" in flat
        for named in ("loop", "loop.json", "docs/site"):
            assert named not in step, named
```

- [ ] **Stap 2:** `uv run pytest -q tests/pack/test_write_issue_skill.py`. Verwacht: FAIL.
- [ ] **Stap 3:** schrijf 3f in beide varianten, met de zinnen die de test vraagt: lees in het
  taxonomiebestand (uit stap 3) de vrije labels met een rij in een tabel met kolommen `When` en
  `Document`; open that document en toets het issue aan de `When` column — raakt het iets dat in
  het document staat, of voegt het er iets aan toe, dan het label. No taxonomy file, or no such
  row: skip this step.
- [ ] **Stap 4:** `uv run pytest -q tests/pack/test_write_issue_skill.py`. Verwacht: PASS.
- [ ] **Stap 5:** `go run ./cmd/lighthouse init --upgrade`, daarna `uv run pytest -q tests/pack`.
- [ ] **Stap 6:** commit `feat(write-issue): vrij label met een document na een toets tegen dat document`
  (met de geïnstalleerde kopie).

### Taak 6: `write-pr` — de documenten van de gekoppelde issues

**Bestanden:**
- Wijzig: `pack/core/skills/write-pr/SKILL.md` (nieuwe `## Documents of the linked issues` tussen
  `## V3. Choose the form` en `## Changelog fragment (if present)`)
- Test: `tests/pack/test_write_pr_skill.py`
- Maak: `changelog.d/fix-loop-kaart-bij-pr.md`

- [ ] **Stap 1: falende test.**

```python
def test_documents_of_the_linked_issues():
    s = " ".join(_section(TEXT, "## Documents of the linked issues").split())
    assert "python3 .lighthouse/lib/lighthouse_lib/config.py paths.labels" in s
    assert "every `Closes #N` and every `Refs #N`" in s
    assert "gh issue view" in s and "--json labels" in s
    assert "does the branch deliver" in s
    assert "before `gh pr create`" in s
    assert "nothing twice" in s  # re-run
    assert "no note in the body" in s
    for named in ("loop.json", "`loop`", "docs/site"):
        assert named not in s, named
    assert TEXT.index("## V3. Choose the form") < TEXT.index("## Documents of the linked issues") \
        < TEXT.index("## Changelog fragment")
```

- [ ] **Stap 2:** `uv run pytest -q tests/pack/test_write_pr_skill.py`. Verwacht: FAIL.
- [ ] **Stap 3:** schrijf de sectie volgens spec §4: for every `Closes #N` and every `Refs #N`
  read the labels (`gh issue view N --json labels`); for a label with a `Document` in the taxonomy
  file: does the branch deliver what the issue changes? With `Closes` yes; with `Refs` judge the
  diff and the AC. Yes → update the document as the taxonomy file says, commit and push before
  `gh pr create`. No → nothing, no note in the body. A re-run writes nothing twice: no `planned`
  item names #N any more and the branch already touches the document. No taxonomy file or no
  such label: skip the section. Controleer of `gh issue view` met `--json labels` door de
  evidence-allowlist van de skill mag; zo niet, gebruik `gh api repos/<repo>/issues/<N> --jq '[.labels[].name]'`
  en pas de test-assert daarop aan.
- [ ] **Stap 4:** `uv run pytest -q tests/pack/test_write_pr_skill.py`. Verwacht: PASS.
- [ ] **Stap 5:** changelog-fragment, type `changed`, Engels, bijvoorbeeld: "`lighthouse-write-pr`
  updates the document a linked issue's label names, such as a map, in the same PR;
  `lighthouse-write-issue` sets such a label after checking the issue against that document."
- [ ] **Stap 6:** `go run ./cmd/lighthouse init --upgrade`, daarna `uv run pytest -q tests/pack`
  en `go run ./cmd/lighthouse init --upgrade` nog eens: verwacht `No changes.`
- [ ] **Stap 7:** commit `feat(write-pr): werk het document van een gekoppeld issue bij in de PR`
  (met de geïnstalleerde kopie en het fragment).

### Taak 7: Label, issues en PR

Buiten de repo; elke stap hier vraagt eerst bevestiging van Corneel.

- [ ] **Stap 1:** schrijf met `/lighthouse-write-issue` (a) een issue voor dit werk, met de
  acceptatiecriteria uit de spec, label `loop`; (b) het vervolgissue uit spec *Vervolgissue*:
  *Een `loop`-issue op een milestone staat als `planned` op de loop-kaart.*
- [ ] **Stap 2:** hernoem het label vlak vóór de PR: `gh label edit chain --name loop`. Daarna
  `python3 ci/loop_roadmap.py`. Verwacht: exit 0.
- [ ] **Stap 3:** `/lighthouse-write-pr` met `Closes #<issue uit stap 1a>`. Die PR sluit zelf een
  `loop`-issue en raakt `loop.json`: de nieuwe PR-modus moet groen zijn. Dat is het eerste
  echte bewijs.

## Zelfcontrole

- Spec §1 → taak 1 (+ label in taak 7); §2 → taak 4; §3 → taak 5; §4 → taak 6; §5 → taak 2;
  §6 → taak 3; §7 → taak 6; vervolgissue → taak 7.
