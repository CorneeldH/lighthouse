"""Contracttest op de issue-skills: blokkade, stand van de backlog en sweeprapport.

De spec (docs/developer/planning/specs/2026-09-28-backlog-stand-en-blokkades-design.md) legt
vast wat alleen in skilltekst leeft en door geen runtime wordt afgedwongen: de volgorde
relatie-vóór-label in paia-write-issue, de melding van vrijgekomen issues in paia-close-issue,
poort 0 die de blocker uit de relatie leest, en de split van de poorten naar paia-route-issue. Beide spiegels (.claude/ en .agents/) worden op
inhoud nagelopen; tests/unit/test_skill_mirror.py vergelijkt alleen bestandsnamen.
"""

from __future__ import annotations

import re
from pathlib import Path

import yaml

REPO = Path(__file__).resolve().parents[2]


def skill(naam: str) -> tuple[str, str]:
    return tuple(
        (REPO / wortel / "skills" / naam / "SKILL.md").read_text(encoding="utf-8")
        for wortel in (".claude", ".agents")
    )

def test_poort_0_leest_de_relatie():
    for body in skill("paia-route-issue"):
        assert "dependencies/blocked_by" in body


def test_poort_0_stopt_ook_zonder_blocked_label():
    """Een issue zonder 3-blocked-* label maar met een open blocked_by (3e liet dat toe als
    "label ontbreekt") mag niet ongemerkt door poort 0 heen glippen — anders wordt zo'n issue
    gestart terwijl het op een ander issue wacht."""
    for body in skill("paia-route-issue"):
        poort0 = body[body.index("### Poort 0") : body.index("### Poort 1")]
        assert "issue_dependencies_summary.blocked_by" in poort0
        assert "label ontbreekt" in poort0
        assert "onbekend" in poort0


def test_poort_0_routeert_door_in_een_controller_run():
    """In een controller-run stapelt de controller de uitvoering (#1099): poort 0 stopt dan niet
    op 3-blocked-issue, maar 3-blocked-external stopt altijd. De router blijft read-only."""
    for body in skill("paia-route-issue"):
        poort0 = body[body.index("### Poort 0") : body.index("### Poort 1")]
        assert "controller-run" in poort0
        assert "3-blocked-external" in poort0
        assert "/paia-handoff" not in poort0  # de router post niets


def test_router_invarianten_kennen_de_controller_run():
    """Geen regel in de router spreekt de uitzondering van poort 0 tegen."""
    for body in skill("paia-route-issue"):
        rij = next(r for r in body.splitlines() if r.startswith("| `3-blocked-issue`"))
        assert "controller-run" in rij
        harde_stop = body[body.index("Alleen de twee `blocked-`-labels") :]
        harde_stop = harde_stop[: harde_stop.index("## Non-negotiable")]
        assert "controller-run" in harde_stop
        regel = next(r for r in body.splitlines() if r.startswith("- ✅ `3-blocked-*`"))
        assert "controller-run" in regel
        extern = next(
            r for r in body.splitlines() if r.startswith("| `3-blocked-external`")
        )
        assert "controller-run" not in extern  # blijft een onvoorwaardelijke stop


def test_route_issue_is_read_only_en_meldt_een_regel():
    for body in skill("paia-route-issue"):
        assert "#<nr> Poort <n>[<a|b|c>]: <route> — <reden>" in body
        for poort in ("Poort 0", "Poort 1", "Poort 2", "Poort 3", "Poort 4"):
            assert poort in body
        assert "board_start.py" not in body and "issue-impact-scout" not in body
        assert "using-git-worktrees" not in body
        assert "roept de route nooit aan" in body  # alleen classificeren
        assert "die skill aanroepen, klaar" not in body  # imperatief niet meeverhuisd


def test_route_issue_description_overleeft_de_yaml_parser():
    """Een ongequote ` #` in de description ("welke route voor #610") is in YAML een comment:
    de parser knipt daar af en de LET OP-clausule verdwijnt stil uit de skillcatalogus.
    test_frontmatter_is_valid_yaml ziet dat niet (het parseert prima), check 4 ook niet (die leest
    de regel ruw)."""
    for body in skill("paia-route-issue"):
        voorkop = yaml.safe_load(body.split("---\n", 2)[1])
        assert voorkop["description"].endswith(
            "LET OP — daadwerkelijk aan een issue beginnen (worktree, board, uitvoering) is "
            "paia-start-issue."
        )


def test_route_issue_stuurt_open_fogvraag_naar_4c():
    for body in skill("paia-route-issue"):
        poort4 = body[body.index("### Poort 4") : body.index("## Het `3-`-label")]
        assert "### Nog niet vastgelegd" in poort4
        assert 'raakt open vraag "' in poort4
        assert "→ #<nr>" in poort4  # alleen een open bullet telt
        labels = body[body.index("## Het `3-`-label") : body.index("## Non-negotiable")]
        assert (
            len(re.findall(r"^\| `3-", labels, flags=re.MULTILINE)) == 6
        )  # geen nieuw label


