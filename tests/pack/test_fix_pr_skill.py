"""Structurele eisen aan .claude/skills/paia-fix-pr/SKILL.md (issue #664).

De acceptatiecriteria van #664 zijn grotendeels vormeisen, en een vormeis zonder test is een wens.
Vier daarvan zijn hier hard gemaakt:

- AC 2: de description draagt de triggers EN een expliciete negatieve grens;
- AC 3: de diagnose is EEN blok, niet drie blokken onder elkaar;
- AC 3/7: dat blok is read-only -- het diagnosticeert, het fixt niet;
- AC 5: elke bekende gate staat in de tabel met een commando dat hem lokaal reproduceert.

De spiegel (.agents/) wordt hier NIET op INHOUD nagelopen. tests/unit/test_skill_mirror.py
vergelijkt alleen bestandsnamen en mapstructuur -- die test documenteert zelf expliciet "BEWUST
GEEN BYTE-VERGELIJKING" -- en scripts/hooks/check-instructies.sh check 3 vergelijkt eveneens alleen
namen. Contentdrift tussen de twee SKILL.md-bestanden (bijvoorbeeld: een fix hier wel doorgevoerd,
in .agents/ vergeten) wordt dus door geen enkele test bewaakt; elke wijziging aan dit bestand moet
met de hand, in dezelfde commit, ook naar .agents/skills/paia-fix-pr/SKILL.md.
"""

from __future__ import annotations

import importlib.util
import re
import sys
from pathlib import Path

REPO = Path(__file__).resolve().parents[2]
SKILL = REPO / ".claude" / "skills" / "paia-fix-pr" / "SKILL.md"

_SPEC = importlib.util.spec_from_file_location(
    "measure_fix_pr_kpi", REPO / "scripts" / "measure_fix_pr_kpi.py"
)
assert _SPEC and _SPEC.loader
kpi = importlib.util.module_from_spec(_SPEC)
sys.modules[_SPEC.name] = kpi
_SPEC.loader.exec_module(kpi)

TEXT = SKILL.read_text(encoding="utf-8")
FRONTMATTER = TEXT.partition("---")[2].partition("\n---")[0]
DESCRIPTION = re.search(r"^description:\s*(.+)$", FRONTMATTER, re.MULTILINE).group(1)

DIAGNOSE_HEADING = "## 1. Diagnose in één blok"


def _diagnose_block() -> str:
    """Het codeblok direct onder de diagnose-kop, tot de volgende `##`-kop."""
    _, _, rest = TEXT.partition(DIAGNOSE_HEADING)
    section = rest.split("\n## ")[0]
    blocks = re.findall(r"```bash\n(.*?)```", section, re.DOTALL)
    assert len(blocks) == 1, (
        f"AC 3: {len(blocks)} bash-blokken onder de diagnose-kop, verwacht 1"
    )
    return blocks[0]


def test_description_carries_the_measured_triggers() -> None:
    """De vijf vormen uit de nulmeting moeten het model kunnen laten aanslaan."""
    lower = DESCRIPTION.lower()
    for trigger in ("fix pr", "conflict", "rood"):
        assert trigger in lower, f"description mist trigger {trigger!r}"


def test_description_carries_an_explicit_negative_boundary() -> None:
    """AC 2 en de mitigatie die het ouderdocument voor elke ambient skill eist.

    Dezelfde twee eisen als has_exclusion_clause in scripts/hooks/check-instructies.sh: een
    scope-woord EN een andere skill bij naam. Elk van beide alleen is precies de valkuil.
    """
    lower = DESCRIPTION.lower()
    assert "let op" in lower
    assert "paia-close-issue" in lower
    assert "caveman-review" in lower


def test_the_diagnosis_is_a_single_block() -> None:
    assert _diagnose_block().strip()


def test_the_diagnosis_block_is_read_only() -> None:
    """AC 3 en AC 7: het blok diagnosticeert. Muteren gebeurt daarna, met een menselijke lezing
    van de uitkomst ertussen."""
    block = _diagnose_block()
    hit = kpi.MUTATING_BASH_RE.search(block)
    assert hit is None, f"diagnoseblok bevat een muterend commando: {hit.group(0)!r}"


GATE_HEADING = "## 3. De gates, en hoe je ze lokaal reproduceert"

# (job-label zoals hij in de EERSTE kolom van de rij staat, commando dat in diezelfde rij moet
# voorkomen) -- in tabelvolgorde. Bindt job aan commando per rij, niet los-van-elkaar in TEXT: dat
# is precies waar de losse bestaan-checks doorheen zakten (job en commando van twee verschillende
# rijen kunnen wisselen zonder dat een los-check dat merkt).
GATE_ROWS = (
    ("`ci` — Unit tests", "uv run pytest tests/unit/"),
    ("`ci` — Quality ratchet (ruff)", "scripts/quality/ratchet.py ruff"),
    ("`ci` — REUSE lint", "uvx reuse@6.2.0 lint"),
    ("`ci` — Type-check src/", "uv run --no-sync ty check src/"),
    ("`ci` — Audit workflows", "scripts/hooks/workflow-audit.sh"),
    ("`ci` — Scan working tree", "gitleaks detect"),
    ("`cla`", "niet lokaal"),
    ("`Expo apps` — `expo-doctor`", "/paia-refresh-expo-lockfiles"),
)


def _gate_table_rows() -> list[str]:
    """De datarijen (kop- en scheidingsregel eraf) van de gatetabel in sectie 3."""
    _, _, rest = TEXT.partition(GATE_HEADING)
    section = rest.split("\n## ")[0]
    lines = [ln for ln in section.splitlines() if ln.strip().startswith("|")]
    assert len(lines) >= 2, "gatetabel niet gevonden onder de kop"
    return lines[2:]


def test_every_known_gate_has_a_local_command() -> None:
    """AC 5. Parseert de tabel zelf: elke verwachte rij moet het job-label in de EERSTE cel dragen
    en het bijbehorende commando in diezelfde rij -- niet los ergens in het bestand."""
    rows = _gate_table_rows()
    assert len(rows) == len(GATE_ROWS), (
        f"gatetabel heeft {len(rows)} rijen, verwacht {len(GATE_ROWS)} "
        "-- rij verwijderd of toegevoegd?"
    )
    for row, (job, command) in zip(rows, GATE_ROWS):
        first_cell = row.split("|")[1]
        assert job in first_cell, f"rij mist job {job!r} in de eerste cel: {row!r}"
        assert command in row, (
            f"rij voor {job!r} mist het commando {command!r}: {row!r}"
        )


def test_the_worktree_step_uses_the_head_branch_form() -> None:
    """AC 4. EnterWorktree en using-git-worktrees maken een NIEUWE branch vanaf origin/main; voor
    een bestaande PR-head-branch is dat verkeerd. De skill moet de git-vorm dragen die wél werkt.

    Toetst wat AC 4 vereist (`--track -b` tegen `origin/`), niet de variabelenaam `$B`: die pint
    een implementatiedetail zonder er iets mee te toetsen."""
    assert "git worktree add" in TEXT
    assert "--track -b" in TEXT
    assert "origin/" in TEXT
    assert "EnterWorktree" in TEXT, (
        "de skill moet expliciet zeggen waarom EnterWorktree hier niet kan"
    )


def test_the_kpi_table_names_the_measuring_script() -> None:
    """Milestone-DoD: elke KPI-rij heeft een draaiend commando."""
    assert "scripts/measure_fix_pr_kpi.py" in TEXT
    for metric in ("leadin", "diagnose", "mistrigger", "scope"):
        assert f"measure_fix_pr_kpi.py {metric}" in TEXT


def test_description_names_the_roborev_entry() -> None:
    """#1414: de tweede ingang (groene PR, onverwerkte roborev-review) moet de skill laten aanslaan."""
    assert "roborev" in DESCRIPTION.lower()


def test_diagnosis_block_shows_roborev_state() -> None:
    """#1414: het diagnoseblok toont of er een roborev-comment is en of er een marker onder staat."""
    block = _diagnose_block()
    assert "roborev-pr-comment" in block
    assert "roborev-verwerkt" in block


def test_diagnosis_block_shows_the_full_roborev_body() -> None:
    """#1426-review: sectie 6 werkt per bevinding, dus het blok filtert de body niet tot kop en verdict."""
    block = _diagnose_block()
    assert "| .body | @json" in block
    assert "Verdict:" not in block


def test_roborev_section_carries_the_contract() -> None:
    """#1414: sectie 6 draagt de marker, de receiving-code-review-norm en het verbod op de CLI."""
    assert "## 6. Roborev-review verwerken" in TEXT
    assert "<!-- roborev-verwerkt: <sha> -->" in TEXT
    assert "superpowers:receiving-code-review" in TEXT
    assert re.search(r"(?:❌|[Gg]een|[Nn]ooit)[^\n]*`roborev fix`", TEXT)
    assert "`roborev refine`" in TEXT
