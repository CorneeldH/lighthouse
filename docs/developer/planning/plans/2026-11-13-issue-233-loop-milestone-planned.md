# Een loop-issue op een milestone staat als planned op de loop-kaart — implementatieplan

> **Voor agents:** VERPLICHTE SUB-SKILL: gebruik superpowers:subagent-driven-development (aanbevolen) of superpowers:executing-plans om dit plan taak voor taak uit te voeren. Stappen gebruiken checkboxen (`- [ ]`).

**Doel:** de hoofdmodus van `ci/loop_roadmap.py` eist dat elk open `loop`-issue met een milestone als item op de loop-kaart staat, en de sitetest en het script zijn het eens over een `planned` item met alleen `milestone`.

**Architectuur:** `open_loop_issues` levert voortaan per open `loop`-issue ook de milestonetitel (`dict[int, str | None]`). `problems` krijgt de omgekeerde controle erbij: een open issue mét milestone dat geen niet-`works` item op de kaart noemt, is een bevinding. In PR-modus tellen de issues die de PR sluit niet mee voor die omgekeerde controle. `tests/test_site_loop.py` volgt het script: een `planned` item zonder `issue` is een bevinding, ook met `milestone`.

**Tech stack:** Python 3.11 stdlib, pytest, `gh api`, GitHub Actions.

**Issue:** #233. **Spec:** [2026-11-01-loop-kaart-bij-pr-design.md](../specs/2026-11-01-loop-kaart-bij-pr-design.md), secties *Hoofdmodus* en *Vervolgissue*.

## Beslissing: `planned` met alleen `milestone` wordt gemeld

Het script meldt zo'n item al ("planned without an issue"); de sitetest trekt bij. Reden: de roadmaplaag komt nu uit issues. Een item zonder issue heeft niets wat het vangnet van de hoofdmodus kan controleren en veroudert onopgemerkt. `milestone` blijft toegestaan *naast* `issue`.

## Globale randvoorwaarden

- Code, docstrings, testnamen, meldingen en changelogfragment in het Engels; `docs/developer/labels.md` en dit plan in het Nederlands.
- Stdlib only, Python 3.11 (`ci/loop_roadmap.py`).
- Exitcodes blijven: 0 ok, 1 oordeel, 2 `COULD NOT CHECK`.
- `write-pr` en `write-issue` blijven ongewijzigd (AC 4 via `docs/developer/labels.md`); geen wijziging onder `pack/`, dus geen `init --upgrade`.
- Elke PR een fragment in `changelog.d/`.
- Verificatie: `uv run pytest -q tests/test_loop_roadmap.py tests/test_site_loop.py`.

## Review Focus

1. Een milestonetitel met spaties → wordt heel gelezen (tab als scheiding in de `--jq`-uitvoer).
2. PR sluit een `loop`-issue met milestone; dat issue is tijdens de run nog open en zijn item is nu `works` zonder `issue` → geen melding.
3. Open `loop`-issue zonder milestone, wel `planned` op de kaart (nu #168) → geen melding (toegestaan, niet verplicht).
4. Open `loop`-issue met milestone dat alleen op een `works` item staat (kan niet: `works` verliest `issue`) of nergens → wel melding.
5. Lege regels of een lege uitvoer van `gh` → lege dict, geen crash.

---

### Taak 1: omgekeerde controle in `ci/loop_roadmap.py`

**Bestanden:**
- Wijzig: `ci/loop_roadmap.py` (docstring r. 2-22, `problems` r. 47-56, `open_loop_issues` r. 75-82, `main` r. 125-134)
- Wijzig: `.github/workflows/loop-roadmap.yml` (r. 1-4 commentaar, r. 9 `types`)
- Test: `tests/test_loop_roadmap.py`

**Interfaces:**
- Produceert: `open_loop_issues(repo: str) -> dict[int, str | None]` — issuenummer → milestonetitel (`None` zonder milestone).
- Produceert: `problems(chain: dict, open_issues: dict[int, str | None]) -> list[str]` — eerst de bestaande meldingen, daarna per open issue met milestone dat in geen `planned(chain)`-waarde staat, oplopend op nummer:
  `f"#{nr}: open issue labelled {LABEL} on milestone {title} is not on the map {HINT}"`.

- [ ] **Stap 1: bestaande tests omzetten naar de dict-vorm.** Elke `set` als tweede argument van `problems` wordt een dict (`{7}` → `{7: None}`, `set()` → `{}`, `{7, 9}` → `{7: None, 9: None}`). In `_pr_mode`: `lambda *_a: dict.fromkeys(closed)`; in de twee touch-tests: `lambda *_a: {9: None}`.

- [ ] **Stap 2: nieuwe falende tests schrijven.**

```python
def test_open_issue_on_a_milestone_missing_from_the_map_is_flagged():
    data = chain({"id": "b", "status": "planned", "issue": 7})
    assert roadmap.problems(data, {7: "M2", 9: "M2 later"}) == [
        "#9: open issue labelled loop on milestone M2 later is not on the map "
        "(see docs/developer/labels.md)"
    ]

def test_open_issue_without_milestone_missing_from_the_map_is_not_flagged():
    assert roadmap.problems(chain({"id": "a", "status": "works"}), {9: None}) == []

def test_open_issue_on_a_milestone_planned_on_the_map_is_clean():
    data = chain(handoffs=({"from": "a", "to": "b", "status": "planned", "issue": 9, "milestone": "M2"},))
    assert roadmap.problems(data, {9: "M2"}) == []

def test_open_loop_issues_reads_number_and_milestone(monkeypatch):
    out = "7\tM2 later\n9\t\n\n"
    monkeypatch.setattr(roadmap.subprocess, "run",
                        lambda *_a, **_k: subprocess.CompletedProcess([], 0, stdout=out))
    assert roadmap.open_loop_issues("o/r") == {7: "M2 later", 9: None}

def test_pr_mode_closing_a_milestoned_issue_is_green(monkeypatch, tmp_path, capsys):
    _pr_mode(monkeypatch, tmp_path, chain({"id": "b", "status": "works"}), {7}, lambda *_a: {MAP})
    monkeypatch.setattr(roadmap, "open_loop_issues", lambda *_a: {7: "M2"})
    assert roadmap.main(["--pr", "5"]) == 0
    assert "map in step" in capsys.readouterr().out

def test_main_mode_flags_a_milestoned_issue_missing_from_the_map(monkeypatch, tmp_path, capsys):
    path = tmp_path / "loop.json"
    path.write_text(json.dumps(chain({"id": "a", "status": "works"})), encoding="utf-8")
    monkeypatch.setattr(roadmap, "LOOP", path)
    monkeypatch.setattr(roadmap, "open_loop_issues", lambda *_a: {9: "M2"})
    assert roadmap.main([]) == 1
    assert "#9: open issue labelled loop on milestone M2" in capsys.readouterr().out
```

`test_open_issue_missing_from_the_map_is_not_flagged` (r. 36-38) vervalt: `test_open_issue_without_milestone_missing_from_the_map_is_not_flagged` vervangt hem.

- [ ] **Stap 3: draai, verwacht rood.** `uv run pytest -q tests/test_loop_roadmap.py` — FAIL op de nieuwe tests (en de omgezette `open_loop_issues`-test).

- [ ] **Stap 4: implementeer.**
  - `open_loop_issues`: `--jq '.[] | select(.pull_request | not) | "\(.number)\t\(.milestone.title // "")"'`; parse per niet-lege regel met `split("\t", 1)`, lege titel → `None`.
  - `problems`: signatuur en melding als in *Interfaces*; "op de kaart" = het nummer staat in `planned(chain).values()` (status ≠ `works`).
  - `main`, PR-modus: `problems(chain, {nr: m for nr, m in open_loop_issues(repo).items() if nr not in closed})`.
  - `main`, hoofdmodus: de ok-regel wordt `f"loop roadmap: {len(issues)} open {LABEL} issues, planned items all open, milestoned issues all on the map"`.
  - Docstring r. 7-9: hoofdmodus controleert beide richtingen; een open issue *zonder* milestone mag ontbreken.
  - Workflow r. 9: `types: [opened, closed, reopened, labeled, unlabeled, milestoned, demilestoned]`; commentaar r. 1-4 noemt de omgekeerde richting.

- [ ] **Stap 5: draai, verwacht groen.** `uv run pytest -q tests/test_loop_roadmap.py` — PASS.

- [ ] **Stap 6: echte kaart tegen GitHub.** `python3 ci/loop_roadmap.py` (met `dangerouslyDisableSandbox`) — exit 0; nu staat alleen #168 open, zonder milestone.

- [ ] **Stap 7: commit.** `feat(ci): loop-roadmap eist een open loop-issue met milestone op de kaart (#233)` — `ci/loop_roadmap.py`, `tests/test_loop_roadmap.py`, `.github/workflows/loop-roadmap.yml`.

### Taak 2: sitetest volgt het script bij `planned` met alleen `milestone`

**Bestanden:**
- Wijzig: `tests/test_site_loop.py` (`planned_problems` r. 130-137, test r. 184-185, fixturetests r. 203-214)
- Wijzig: `tests/fixtures/site/map.json` (chip `planned-milestone` r. 31-35, handoff r. 48)

**Interfaces:**
- Gebruikt: `problems(chain, open_issues: dict[int, str | None])` uit taak 1, geladen via `importlib` zoals `tests/test_loop_roadmap.py` r. 8-12 (modulenaam `lighthouse_ci_loop_roadmap_site`).
- Produceert: `planned_problems(chain: dict) -> list[str]` met melding `f"{name}: planned without an issue"`.

- [ ] **Stap 1: falende overeenstemmingstest schrijven** in `tests/test_site_loop.py`:

```python
@pytest.mark.parametrize("fields, flagged", [
    ({"issue": 7}, False),
    ({"issue": 7, "milestone": "M9"}, False),
    ({"milestone": "M9"}, True),
    ({}, True),
])
def test_site_and_roadmap_agree_on_a_planned_item(fields, flagged):
    chain = {"chips": [{"id": "x", "status": "planned", **fields}], "handoffs": []}
    assert bool(planned_problems(chain)) is flagged
    assert bool(roadmap.problems(chain, {7: "M9"})) is flagged
```

- [ ] **Stap 2: draai, verwacht rood.** `uv run pytest -q tests/test_site_loop.py -k agree` — FAIL op `{"milestone": "M9"}`.

- [ ] **Stap 3: implementeer.** `planned_problems` meldt elk `planned` item zonder `issue`. Fixture: chip `planned-milestone` en de handoff `main-commit-gate → planned-issue` krijgen `"issue": 124` naast hun `milestone`. Hernoem `test_planned_has_an_issue_or_milestone` naar `test_planned_has_an_issue`. Fixturetests: `test_fixture_planned_without_issue_is_flagged` verwacht `["planned-issue: planned without an issue"]`; `test_fixture_planned_handoff_without_milestone_is_flagged` wordt `test_fixture_planned_handoff_with_only_a_milestone_is_flagged`: verwijder `issue` (niet `milestone`) en verwacht `["main-commit-gate->planned-issue: planned without an issue"]`.

- [ ] **Stap 4: draai, verwacht groen.** `uv run pytest -q tests/test_loop_roadmap.py tests/test_site_loop.py` — PASS. Mutatiecheck: zet in `planned_problems` de `milestone`-uitzondering terug; `test_site_and_roadmap_agree_on_a_planned_item[fields2-True]` moet rood worden.

- [ ] **Stap 5: commit.** `test(site): planned-item met alleen milestone is een bevinding, zoals in loop_roadmap (#233)`.

### Taak 3: documentatie en changelog

**Bestanden:**
- Wijzig: `docs/developer/labels.md` (r. 36-52)
- Maak: `changelog.d/issue-233-loop-milestone-planned.md`

- [ ] **Stap 1: `docs/developer/labels.md`.** R. 37-39: een nieuw `loop`-issue zonder milestone krijgt alleen het label en komt met de leverende PR op de kaart; krijgt het een milestone, dan komt het als `planned` item met `issue` (en mag `milestone`) op de kaart. Een `planned` item zonder `issue` mag niet. Hoofdmodus-bullet r. 50-52: beide richtingen; de omgekeerde alleen voor issues met milestone; de workflow draait ook bij `milestoned`/`demilestoned`.

- [ ] **Stap 2: changelogfragment.**

```markdown
---
type: internal
---
The `loop-roadmap` check now requires every open `loop` issue on a milestone to be planned on the loop map, and a planned item on the map needs an issue; a milestone alone is no longer enough.
```

- [ ] **Stap 3: verifieer.** `uv run pytest -q tests/test_loop_roadmap.py tests/test_site_loop.py` — PASS; `md-link-check` blokkeert niets.

- [ ] **Stap 4: commit.** `docs(labels): loop-issue op een milestone hoort op de kaart (#233)`.
