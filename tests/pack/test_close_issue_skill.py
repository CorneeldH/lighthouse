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

def test_close_issue_meldt_vrijgekomen_issues():
    for body in skill("paia-close-issue"):
        assert "dependencies/blocking" in body
        assert "--remove-label 3-blocked-issue" in body
        assert (
            "unblock-dependents.yml" in body
        )  # de lane geeft vrij, de skill controleert


def test_stap_nul_wacht_op_merge():
    for body in skill("paia-close-issue"):
        assert (
            "wacht op merge van #<pr>; draai /paia-close-issue <nr> opnieuw na de merge"
            in body
        )
        assert body.index("## Stap 0") < body.index("## Ingang B")


def test_ingang_c_draait_alleen_de_runner():
    for body in skill("paia-close-issue"):
        assert "evidence.py run" in body
        assert "git worktree add --detach" in body
        assert "origin/main" in body


def test_ingang_c_heropent_fail_closed():
    for body in skill("paia-close-issue"):
        assert "gh issue reopen" in body
        assert "board_start.py --heropen" in body
        assert "issue heropend, board niet bijgewerkt" in body
        assert "Closes zonder bewijsblok" in body
        assert "Evidence buiten allowlist" in body
        assert "na-merge-check: klopt" in body


def test_ingang_c_meldt_een_niet_meetbare_run_luid():
    """rc 3 (KON NIET CONTROLEREN) en een onbekende rc: geen klopt-comment, geen reopen."""
    for body in skill("paia-close-issue"):
        c = body[body.index("## Ingang C") :]
        c = c[: c.index("\n## ", 1)]
        assert "KON NIET CONTROLEREN" in c
        assert "onbekende" in c


def test_ingang_c_sluit_nooit_zelf():
    for body in skill("paia-close-issue"):
        c = body[body.index("## Ingang C") :]
        c = c[: c.index("\n## ", 1)]
        assert "gh issue close" not in c


def _ingang_c(body: str) -> str:
    c = body[body.index("## Ingang C") :]
    return c[: c.index("\n## ", 1)]


def test_ingang_c_plakt_runner_uitvoer_niet_in_een_gequote_regel():
    """De runner-tabel bevat vrije tekst uit de PR-body; in `"..."` zou de shell backticks en
    `$(…)` daaruit uitvoeren. Uitvoer gaat via een bestand."""
    for body in skill("paia-close-issue"):
        c = _ingang_c(body)
        assert '"<runner-tabel>"' not in c
        assert "run.txt" in c
        assert "--body-file" in c or "$(cat" in c
        assert 'gh issue reopen <nr> --comment "$(cat <scratch>/run.txt)"' in c
        assert "nooit runner-uitvoer of body-tekst in een gequote commandoregel" in c


def test_ingang_c_vangt_de_exitcode_voor_de_worktree_remove():
    for body in skill("paia-close-issue"):
        c = _ingang_c(body)
        assert "rc=$?" in c
        assert c.index("rc=$?") < c.index("git worktree remove")
        assert "run.err" in c  # stderr apart, zodat het begin van run.txt zuiver blijft


def test_ingang_c_heropent_alleen_op_een_herkende_uitkomst():
    """rc 2 zonder `Closes zonder bewijsblok` (argparse) en rc 1 zonder tabelkop (traceback)
    zijn geen oordeel over het issue."""
    for body in skill("paia-close-issue"):
        c = _ingang_c(body)
        assert "begint met `Closes zonder bewijsblok`" in c
        assert "| AC | Uitkomst | Detail |" in c
        herkend = c[c.index("### C3") :]
        assert "argparse" in herkend
        assert "traceback" in herkend


def test_ingang_c_meet_de_pr_die_het_issue_werkelijk_sloot():
    """`closedByPullRequestsReferences` houdt elke gemergde PR vast; C1 leest de `closer`."""
    for body in skill("paia-close-issue"):
        stap0 = body[body.index("## Stap 0") : body.index("## Drie ingangen")]
        assert "timelineItems(itemTypes:[CLOSED_EVENT],last:1)" in stap0
        assert "closer{__typename ... on PullRequest{number}}" in stap0
        c1 = _ingang_c(body)
        c1 = c1[c1.index("### C1") : c1.index("### C2")]
        assert "`closer`" in c1
        assert "gesloten zonder sluitende PR — geen nacontrole" in c1
        assert "`MERGED`" not in c1


def test_ingang_c_scratch_is_vers_en_geketend():
    for body in skill("paia-close-issue"):
        c = _ingang_c(body)
        assert "scratch=$(mktemp -d)" in c
        assert 'git worktree add --detach "$scratch/na-merge-<nr>" origin/main &&' in c
        assert c.index("scratch=$(mktemp -d)") < c.index("evidence.py run")
        assert "geen `rc=`-regel" in c


def test_ingang_c_exit_4_schrijft_een_commentbestand():
    for body in skill("paia-close-issue"):
        c = _ingang_c(body)
        assert "> <scratch>/comment.md" in c
        assert "gh issue comment <nr> --body-file <scratch>/comment.md" in c
