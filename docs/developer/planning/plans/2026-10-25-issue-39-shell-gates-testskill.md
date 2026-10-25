# shell-gates sluit aan op lighthouse-testing — implementatieplan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Doel:** `lighthouse-shell-gates` (beide varianten) verwijst voor de testnormen naar
`lighthouse-testing`, vraagt de classificatiezin en geeft de shellvorm van die normen, zoals
`lighthouse-python-writing` dat al doet voor Python (issue #39).

**Architectuur:** drie tekstwijzigingen in `pack/shell/skills/shell-gates/{claude,agents}/SKILL.md`,
byte-identiek in beide varianten, elk gepind door een contracttest in
`tests/pack/shell/test_shell_gates.py`. Lighthouse installeert `pack/shell` niet zelf, dus
`init --upgrade` hoort `No changes.` te geven.

**Tech stack:** Markdown-skills, pytest-contracttests, `go run ./cmd/lighthouse init --upgrade`.

**Spec:** [2026-10-24-testskill-design.md](../specs/2026-10-24-testskill-design.md), sectie
*Vervolg*. Voorbeeld om te spiegelen:
[python-writing](../../../../pack/python/skills/python-writing/claude/SKILL.md) (Domain-alinea,
sectie *The Python form of lighthouse-testing*, pairing-rij) en de tests in
[test_python_writing.py](../../../../tests/pack/python/test_python_writing.py).

## Globale randvoorwaarden

- Skilltekst in het Engels; commitberichten in het Nederlands, Conventional Commits, scope `shell`.
- Elke nieuwe regel staat **byte-identiek** in `claude/SKILL.md` en `agents/SKILL.md`;
  `test_variants_keep_their_difference` laat alleen de OpenCode-noot en de OpenCode-zin in de
  description toe. De frontmatter-description verandert niet.
- De classificatiezin letterlijk zoals in `lighthouse-testing` (*Classify first*), dus Brits:
  `This is a <class> change; the likely regression surface is <observable behaviour>.` Het issue
  schrijft `behavior`; de skill volgt de testskill, zodat een agent één zin leert. De spec
  (*De classificatiezin en de klassentabel*) is daarop rechtgezet. Noem de afwijking van het
  issue-AC in de PR-evidence.
- Buiten Claude Code (Codex, OpenCode) draait de pre-commit-hook niet: draai dan vóór élke commit
  de volledige gate `uv run pytest -q && go test ./...` (`[test_gate] command`), naast de
  gerichte runs in de taken.
- Geen eigen klassentabel in shell-gates: alleen een verwijzing naar *Classify first*.
- Nieuwe tekst bevat geen van de strings die `test_no_paia_references` verbiedt (o.a. `paia`,
  `ceda`, `ruff-format`, `gate-assert`, `2026-08-`), geen Nederlandse woorden uit `test_english`,
  en geen `.lighthouse/…`-pad buiten de drie die `test_paths_are_the_installed_ones` kent.
- Geen nieuwe `##`-kop met de naam `## KPIs` of `## The pairing`; bestaande alinea's die tests op
  openingstekst zoeken (*Three outcomes*, de hook-alinea onder de pairing) blijven ongewijzigd.
- `pack/SOURCES.toml` blijft ongewijzigd: `source_sha` is herkomst (upstream), geen inhoudshash.
- Vóór de eerste edit in de chat: `This is a test-only change; the likely regression surface is the instruction text an agent reads before writing a gate.`

## Review Focus

- **Variantdrift.** Eén variant net anders bewerkt (witregel, spatie) → `test_variants_keep_their_difference` rood. Gedekt door die bestaande test; elke taak draait hem mee.
- **Spelling van de zin.** `behavior` in plaats van `behaviour` breekt de koppeling met de testskill. Gepind in taak 1 op de exacte zin.
- **KPI-sectie verstoord.** Een nieuwe sectie binnen `## KPIs` breekt `_kpi_rows`. Gedekt door de bestaande KPI-tests; de nieuwe sectie komt vóór `## The pairing`, na `## Non-negotiable`.
- **Pairing-test pakt de verkeerde sectie.** De test snijdt `## The pairing` tot de volgende `\n## `; de nieuwe rij moet ín de tabel staan, niet in de OpenCode-noot. Gepind in taak 3.
- **Shellsectie zonder de blokkerende/doorlatende invoer.** Precies dat is de minimale check van `hook/gate`. Gepind in taak 2.

---

### Taak 1: Verwijzing en classificatiezin in Domain

**Files:**
- Modify: `pack/shell/skills/shell-gates/claude/SKILL.md:17` (na `Out of scope: Python and workflow YAML.`)
- Modify: `pack/shell/skills/shell-gates/agents/SKILL.md:17` (zelfde plek, zelfde tekst)
- Test: `tests/pack/shell/test_shell_gates.py`

**Interfaces:**
- Produces: de Domain-sectie noemt `lighthouse-testing` en *Classify first*; taak 2 verwijst daarnaar.

- [ ] **Stap 1: Schrijf de falende test** (onderaan `test_shell_gates.py`)

```python
CLASSIFICATION = "This is a <class> change; the likely regression surface is <observable behaviour>."


@pytest.mark.parametrize("variant", VARIANTS)
def test_points_to_the_testing_skill(variant):
    domain = _text(variant).split("## Domain", 1)[1].split("\n## ", 1)[0]
    assert "lighthouse-testing" in domain
    assert "*Classify first*" in domain
    assert "Before the first edit" in domain
    assert CLASSIFICATION in domain
```

- [ ] **Stap 2: Draai hem en zie hem falen**

Run: `uv run pytest -q tests/pack/shell/test_shell_gates.py -k points_to_the_testing_skill`
Expected: 2 failed (`assert 'lighthouse-testing' in domain`).

- [ ] **Stap 3: Voeg de alinea toe, in beide varianten**

Na de regel `Out of scope: Python and workflow YAML.` een witregel en dan, naar het model van
python-writing r. 22-28:

````markdown
What a good test is lives in `lighthouse-testing`; this file gives only its shell form
(below). Before the first edit, write the classification sentence from that skill in the chat;
the classes and their minimal checks are in its section *Classify first*:

```text
This is a <class> change; the likely regression surface is <observable behaviour>.
```
````

- [ ] **Stap 4: Draai de shelltests**

Run: `uv run pytest -q tests/pack/shell`
Expected: alles groen, ook `test_variants_keep_their_difference` en `test_english`.

- [ ] **Stap 5: Commit**

```bash
git add pack/shell/skills/shell-gates tests/pack/shell/test_shell_gates.py
git commit -m "feat(shell): shell-gates verwijst naar lighthouse-testing en vraagt de classificatiezin (#39)"
```

### Taak 2: Sectie *The shell form of lighthouse-testing*

**Files:**
- Modify: beide `SKILL.md`, nieuwe sectie direct vóór `## The pairing` (r. 92)
- Test: `tests/pack/shell/test_shell_gates.py`

**Interfaces:**
- Consumes: de verwijzing "its shell form (below)" uit taak 1.
- Produces: kop `## The shell form of lighthouse-testing`.

- [ ] **Stap 1: Schrijf de falende test**

```python
@pytest.mark.parametrize("variant", VARIANTS)
def test_shell_form_of_the_testing_norms(variant):
    text = _text(variant)
    assert "## The shell form of lighthouse-testing" in text
    section = text.split("## The shell form of lighthouse-testing", 1)[1].split("\n## ", 1)[0]
    for phrase in (
        "temporary directory",
        "the exit code and the output",
        "never read or grep the source",
        "one input that must block and one that must pass",
    ):
        assert phrase in section, phrase
```

- [ ] **Stap 2: Draai hem en zie hem falen**

Run: `uv run pytest -q tests/pack/shell/test_shell_gates.py -k shell_form_of_the_testing_norms`
Expected: 2 failed op de kop.

- [ ] **Stap 3: Schrijf de sectie, in beide varianten**

Kop `## The shell form of lighthouse-testing`, een introregel
`The norm is in `lighthouse-testing`; this is its form for a gate or a hook.` en vier bullets met
vetgedrukte opening, die samen de vier gepinde frases letterlijk bevatten:

- **Run the script, against controlled input.** Een tijdelijke map (`mktemp -d` of pytest
  `tmp_path`) met precies de fixture die het pad bereikt; nooit de echte repo of `$HOME`.
- **Assert the exit code and the output.** Beide: alleen rc laat defect 1 door (rc 127 is ook
  nonzero); het eerste regel-prefix onderscheidt een bevinding van `COULD NOT CHECK`.
- **Run it, never read or grep the source.** Een test die `grep` op het script doet, blijft groen
  als het mechanisme breekt.
- **Per gate one input that must block and one that must pass.** De minimale check van de klasse
  `hook/gate`; met alleen de blokkerende kan de gate alles blokkeren en toch groen staan.

Formulering mag afwijken, de vier frases niet. Geen klassentabel.

- [ ] **Stap 4: Draai de shelltests**

Run: `uv run pytest -q tests/pack/shell`
Expected: alles groen, ook de KPI-tests.

- [ ] **Stap 5: Commit**

```bash
git add pack/shell/skills/shell-gates tests/pack/shell/test_shell_gates.py
git commit -m "feat(shell): shellvorm van de testnormen in shell-gates (#39)"
```

### Taak 3: Pairing-rij en eindverificatie

**Files:**
- Modify: beide `SKILL.md`, tabel onder `## The pairing` (r. 94-97)
- Test: `tests/pack/shell/test_shell_gates.py`

- [ ] **Stap 1: Schrijf de falende test**

```python
@pytest.mark.parametrize("variant", VARIANTS)
def test_pairing_names_the_testing_skill(variant):
    pairing = _text(variant).split("## The pairing", 1)[1].split("\n## ", 1)[0]
    rows = [line for line in pairing.splitlines() if line.startswith("| `lighthouse-testing` |")]
    assert len(rows) == 1
```

- [ ] **Stap 2: Draai hem en zie hem falen**

Run: `uv run pytest -q tests/pack/shell/test_shell_gates.py -k pairing_names_the_testing_skill`
Expected: 2 failed (`assert 0 == 1`).

- [ ] **Stap 3: Voeg de rij toe, in beide varianten**, tussen `| this skill | …` en de hook-rij:

```markdown
| `lighthouse-testing` | the test norms and the class table | for every test, and before the first edit |
```

- [ ] **Stap 4: Volledige gate en dogfood**

Run: `uv run pytest -q && go test ./...` → groen.
Run: `go run ./cmd/lighthouse init --upgrade` → `No changes.`
Run: `bash .lighthouse/scripts/check-instructions.sh` → rc 0.
Deze repo installeert alleen `core`, dus check 4 ziet shell-gates hier niet; het bewijs voor het
AC over check 4 is `test_descriptions_pass_the_overlap_check`
(`tests/pack/test_skill_contracts.py`), die elke pack-skill installeert en in de volledige gate
meedraait. Noem die test in de PR-evidence naast de rc 0.

- [ ] **Stap 5: Commit**

```bash
git add pack/shell/skills/shell-gates tests/pack/shell/test_shell_gates.py
git commit -m "feat(shell): pairing van shell-gates noemt lighthouse-testing (#39)"
```

- [ ] **Stap 6: Mutatiecheck** (`lighthouse-testing`), op de schone tree na de commit: zet in
  één variant `behaviour` → `behavior` en haal de pairing-rij weg; draai
  `uv run pytest -q tests/pack/shell`; verwacht rood op `test_points_to_the_testing_skill`,
  `test_pairing_names_the_testing_skill` en `test_variants_keep_their_difference` (alleen één
  variant is gemuteerd). Herstel met `git checkout -- pack/shell/skills/shell-gates`. Tweede
  mutatie: haal in béide varianten `and the output` weg; verwacht rood op
  `test_shell_form_of_the_testing_norms`. Herstel weer met `git checkout`.
