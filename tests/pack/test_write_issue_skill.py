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


def test_write_issue_legt_relatie_voor_label():
    for body in skill("paia-write-issue"):
        sectie = body[body.index("#### 3e. Blokkade leggen") :]
        assert sectie.index('dependencies/blocked_by" -F issue_id=') < sectie.index(
            "--add-label 3-blocked-issue"
        )
        assert "onbekend" in sectie  # toestand niet uitleesbaar -> nooit succes
        assert "niet uit de summary" in sectie  # Review Focus 4
        assert "created_at" in sectie  # herstel na create: nooit een ouder issue
        for oordeel in ("klopt", "vervallen", "ongekoppeld", "label ontbreekt"):
            assert f"**{oordeel}**" in sectie


def test_write_issue_zoekt_dubbelingen_voor_het_template():
    for body in skill("paia-write-issue"):
        stap0 = body[
            body.index("### 0. Bestaat dit al?") : body.index(
                "### 1. Kies het template"
            )
        ]
        assert (
            "search/issues" in stap0 and ".items[:5]" in stap0
        )  # open én gesloten, begrensd
        assert "git grep -l" in stap0 and "origin/main" in stap0  # codebase/docs-blik
        assert "dubbel? nee — " in stap0
        assert "git fetch origin main || echo 'FETCH FAALDE" in stap0
        assert "dubbel? niet meetbaar — fetch faalde" in stap0
        assert "exit ≥ 2" in stap0  # exit 1 = nul treffers, geen fout
        for keuze in ("niet aanmaken", "aanvullen", "toch aanmaken"):
            assert keuze in stap0
        assert "gh issue list --json" not in stap0 and "wontfix" not in stap0


def test_pitch_heeft_sectie_nog_niet_vastgelegd():
    for body in skill("paia-write-issue"):
        pitch = body[
            body.index("### Pitch (Shape Up)") : body.index("## Title Standards")
        ]
        assert pitch.index("### No-Gos") < pitch.index("### Nog niet vastgelegd")
        assert "→ #<nr>" in pitch  # uitstroom: een eigen, opvraagbaar issue
        assert "/paia-write-issue" in pitch
        assert "geen eigen timebox" in pitch  # appetite blijft poortwachter
        assert "wayfinder" not in pitch
