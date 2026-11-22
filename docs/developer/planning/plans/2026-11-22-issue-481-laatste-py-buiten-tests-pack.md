# Issue 481: laatste .py buiten tests/pack/python/ weg — implementatieplan

> **Voor agentic workers:** VERPLICHTE SUB-SKILL: gebruik `superpowers:subagent-driven-development` (aanbevolen) of `superpowers:executing-plans` om dit plan taak voor taak uit te voeren. Stappen gebruiken checkbox-syntaxis (`- [ ]`).

**Doel:** Onder `tests/pack/` blijft alleen `tests/pack/python/` Python: de vijf losse `.py`-bestanden krijgen elk één bestemming.

**Aanpak:** `test_forge_neutral_units.py` gaat naar Go, naast de skillcontracten in `internal/packcontract/skills_test.go`; wat daar al gedekt is, gaat weg met naam van de Go-test. De offline-skip (`_network.py`, `test_network.py`, de hook) blijft nodig voor `python/` (`test_ruff_format.py` en `test_ty_gate.py` roepen `uvx`, `ruff` en `ty` aan) en verhuist naar `tests/pack/python/`. `tests/pack/conftest.py` en `tests/pack/__init__.py` gaan weg, gemeten met een pytest-run.

**Techniek:** Go (`internal/packcontract`), Python/pytest (alleen `tests/pack/python/`).

**Spec:** [`2026-11-16-repo-zonder-python-design.md`](../specs/2026-11-16-repo-zonder-python-design.md) (§1, stap 9). Voorbeeld van dezelfde triage: [`2026-11-20-issue-368-taalpakket-tests.md`](2026-11-20-issue-368-taalpakket-tests.md).

## Randvoorwaarden

- `pack/python` en `tests/pack/python/` blijven Python en pytest; geen ander gedrag van een script of hook.
- Een test die weggaat, krijgt een rij in de triagetabel van de PR; "gedekt" noemt de Go-test.
- Een Go-port noemt in de PR één mutatie van de productiecode waarop de nieuwe test faalt (`lighthouse-test-standard`: classificatiezin vóór de test, mutatiecheck vóór "klaar").
- Code, commentaar en docstrings Engels; artefacten (changelog, PR, commits) Nederlands. Vóór elke commit: `lighthouse imports check` naast `[test_gate] command`.

## Aandachtspunten

- Zonder de hook in `tests/pack/conftest.py` worden de offline-tests van `python/` rood in plaats van geskipt (#338): `test_network.py` moet ook na de verhuizing groen zijn.
- `test_network.py` bepaalt `ROOT = Path(__file__).resolve().parents[2]` en schrijft een inner-`conftest.py` met `from tests.pack.conftest import …`: beide breken stil bij een verhuizing een niveau dieper.
- `tests/pack/python/conftest.py` heeft al een autouse-fixture voor `LIGHTHOUSE_REPO_ROOT` (`PY_ROOT`); de fixture van de ouder (`INSTALL_ROOT`) wordt dus overschreven. Alleen een run bewijst dat het wegnemen niets verandert.
- `tests/pack/python/test_gate_python.py:27,60` noemt `tests/pack/conftest.py` als pad in een nepboom: geen echte verwijzing, laten staan.

## Bestandsoverzicht

| Bestand | Bestemming |
|---|---|
| `tests/pack/test_forge_neutral_units.py` | naar Go (`internal/packcontract/`) of weg per test, zie Taak 1 |
| `tests/pack/_network.py`, `tests/pack/test_network.py` | `tests/pack/python/` (Taak 2) |
| `tests/pack/conftest.py` | hook naar `tests/pack/python/conftest.py`, rest weg (Taak 2 en 3) |
| `tests/pack/__init__.py` | weg (Taak 3) |

---

### Taak 1: test_forge_neutral_units naar Go

**Bestanden:**
- Wijzig/maak: `internal/packcontract/skills_test.go` (of een nieuw `forgeneutral_test.go` in hetzelfde pakket; volg waar de buurtests staan)
- Verwijder: `tests/pack/test_forge_neutral_units.py`

**Triage per test van het Python-bestand** (leg vast in de PR-tabel):

| Python-test | Go-bestemming |
|---|---|
| `test_unit_calls_no_gh_for_a_forge_operation` × 5 (`review-plan`, `upgrade`, `audit-worktrees`, `write-handoff`, `fix-pr`) | Go: `TestChainSkillsDoNotCallGhForForgeOperations` (`skills_test.go:607`) dekt alleen `chainSkills`; breid dezelfde regex-controle uit naar de vijf units, in een eigen lijst `forgeNeutralUnits`. De Python-regex bevat ook `run|workflow|repo|search|release`; neem die over (`gh run` en `gh release` blijven verboden in deze units). |
| `test_gh_is_required_only_on_github` | Controleer of een Go-test over `requires.toml` dit al afdwingt (`skills_test.go:446-520`, o.a. `TestRequiresNamesTheSkillPerTool`). Zo ja: weg, met naam. Zo nee: port. |
| `test_forge_reference_has_ci_section`, `test_forge_files_have_workflows_section` | `forgeSections` (`skills_test.go:~644`) bevat `ci` en `workflows`; lees `TestForgeSkillHasBothReferences` en zijn buren. Gedekt: weg, met naam. Niet gedekt: port. |
| `test_requires_toml_drops_gh_for_converted_units` | Gedekt door `TestRequiresNamesTheSkillPerTool` als dat `write-handoff` en `audit-worktrees` onder `git` en niet onder `gh` eist; anders port. |
| `test_go_units_do_not_call_gh` | Go-test die de niet-`_test.go`-bronnen van `internal/postartefact`, `internal/labels`, `internal/worktreeaudit`, `internal/cli/{postartefact,worktreeaudit,forge_label}.go` scant op `"gh"` of `gh (label\|api\|pr\|issue)`; de allowlist (`internal/forge/token.go`, `internal/cli/ask.go`) blijft expliciet en wordt bewaakt zoals nu (staat niet in de scanlijst). Pad naar de repowortel: gebruik de helper die de buurtests in dit pakket al gebruiken. |

**Interfaces:**
- Produceert: `forgeNeutralUnits []string` en de Go-tests `TestForgeNeutralUnitsCallNoGh`, `TestGoUnitsDoNotCallGh` (namen vrij, maar elke test een classificatiezin boven zich in de stijl van de buurtests).

- [ ] **Stap 1: Lees de buurtests** (`skills_test.go:446-520`, `592-671`) en vul de triagetabel hierboven in: per Python-test "gedekt door `<Go-test>`" of "port".
- [ ] **Stap 2: Schrijf de Go-tests** voor elke "port"-rij, met de assertie over `hits == []`-gedrag (foutmelding `rel:line: tekst` per treffer, zoals het origineel).
- [ ] **Stap 3: Mutatiecheck** — voeg tijdelijk `gh pr list` toe aan een regel in `pack/core/skills/fix-pr/` (en apart: `exec.Command("gh", …)` in `internal/labels/`); `go test ./internal/packcontract/...` moet falen op de nieuwe test. Draai terug. Noteer één van de twee mutaties voor de PR.
- [ ] **Stap 4:** `go test ./internal/packcontract/...` groen.
- [ ] **Stap 5: Commit** — `git rm tests/pack/test_forge_neutral_units.py`; `git commit -m "test(packcontract): forge-neutrale units naar Go (#481)"`.

### Taak 2: offline-skip naar tests/pack/python/

**Bestanden:**
- Verplaats (`git mv`): `tests/pack/_network.py` → `tests/pack/python/_network.py`; `tests/pack/test_network.py` → `tests/pack/python/test_network.py`
- Wijzig: `tests/pack/python/conftest.py` (hook erbij), `tests/pack/python/test_network.py` (imports, `ROOT`, inner-conftest), `tests/pack/python/_network.py` (docstring-verwijzing naar `tests/pack/conftest.py`), `pyproject.toml:20` (markertekst noemt het nieuwe pad)

**Interfaces:**
- Produceert: `tests.pack.python._network` met `network_error(text: str) -> str | None`, `offline() -> bool`, `PROBE_ENV` (ongewijzigd); in `tests/pack/python/conftest.py` een `pytest_runtest_makereport`-wrapper identiek aan die in `tests/pack/conftest.py` (regel 20-31).

- [ ] **Stap 1: Baseline** — `uv run pytest -q tests/pack/python tests/pack/test_network.py`; noteer het aantal tests.
- [ ] **Stap 2:** `git mv` van beide bestanden; verander imports naar `tests.pack.python._network` en de inner-conftest naar `from tests.pack.python.conftest import pytest_runtest_makereport`.
- [ ] **Stap 3:** Zet de hook (`pytest_runtest_makereport`) over naar `tests/pack/python/conftest.py`, en `ROOT` in `test_network.py` op de repowortel (`parents[3]` na de verhuizing; controleer met `ROOT / "pyproject.toml"` bestaat als assertie of in de test).
- [ ] **Stap 4:** Werk markertekst en docstring bij naar `tests/pack/python/_network.py` / `tests/pack/python/conftest.py`.
- [ ] **Stap 5:** `uv run pytest -q tests/pack/python/test_network.py` groen, en dezelfde test-aantallen als in stap 1 (geen stil verdwenen test).
- [ ] **Stap 6: Mutatiecheck** — haal de `offline()`-voorwaarde uit de hook weg (`if reason:` i.p.v. `if reason and offline():`); `test_network.py` moet falen. Draai terug.
- [ ] **Stap 7: Commit** — `git commit -m "test(pack): offline-skip naar tests/pack/python (#481)"`.

### Taak 3: conftest.py en __init__.py weg, changelog

**Bestanden:**
- Verwijder: `tests/pack/conftest.py`, `tests/pack/__init__.py`
- Maak: `changelog.d/issue-481-laatste-py-buiten-tests-pack.md`

- [ ] **Stap 1: Meet vooraf** — `uv run pytest -q tests/pack/python` groen met de bestanden er nog; noteer aantal.
- [ ] **Stap 2:** `git rm tests/pack/conftest.py tests/pack/__init__.py`. De `INSTALL_ROOT`-fixture en `PYTHONDONTWRITEBYTECODE`-regels uit de ouder-conftest gaan mee weg als de run groen blijft; blijkt `PYTHONDONTWRITEBYTECODE` of de installed-core-fixture toch nodig (rode of `__pycache__`-vervuilde run), verhuis dat stuk naar `tests/pack/python/conftest.py` — niet de hele fixture blind.
- [ ] **Stap 3:** `uv run pytest -q tests/pack/python` groen, zelfde aantal als stap 1. Ook de volledige `uv run pytest -q` (of de gate-commando) groen: controleert dat `tests.pack.python._testkit` zonder `tests/pack/__init__.py` resolveert (namespace-package; `python/__init__.py` bestaat).
- [ ] **Stap 4:** `git ls-files 'tests/pack/*.py' | grep -v '^tests/pack/python/'` geeft niets.
- [ ] **Stap 5:** Fragment, met frontmatter `type: internal`:

  ```markdown
  ---
  type: internal
  ---
  Het laatste Python buiten `tests/pack/python/` is weg: de forge-neutraliteit van de units draait in Go, de offline-skip van de pytest-suite staat bij `tests/pack/python/`.
  ```

- [ ] **Stap 6:** `lighthouse imports check`; `go test ./...` en de gate groen.
- [ ] **Stap 7: Commit** — `git commit -m "test(pack): tests/pack/conftest.py en __init__.py weg (#481)"`.

## Zelfreview

- AC1 (bestemming `test_forge_neutral_units`): Taak 1. AC2 (mutatie): Taak 1 stap 3. AC3 (`test_network`/`_network`): Taak 2. AC4 (`git ls-files`): Taak 3 stap 4. AC5 (pytest zonder conftest/`__init__`): Taak 3 stap 3. AC6 (changelog): Taak 3 stap 5.
- Open punt voor de uitvoerder: of `test_gh_is_required_only_on_github` en de forge-sectietests al in Go gedekt zijn is in de scout niet vastgesteld; Taak 1 stap 1 beslist dat met de gelezen Go-tests, "gedekt" alleen met testnaam.
