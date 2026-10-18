"""#1092: een gecommitte spec gaat standaard naar de controller (spec 2026-10-11)."""

from pathlib import Path

import pytest

REPO = Path(__file__).resolve().parents[2]
AGENTS = REPO / "AGENTS.md"
HANDOFF = [
    REPO / ".claude/skills/paia-handoff/SKILL.md",
    REPO / ".agents/skills/paia-handoff/SKILL.md",
]


def _plat(pad: Path) -> str:
    return " ".join(pad.read_text(encoding="utf-8").split())


def _sectie(tekst: str, kop: str) -> str:
    begin = tekst.index(kop)
    einde = tekst.find("\n## ", begin + len(kop))
    return tekst[begin:] if einde == -1 else tekst[begin:einde]


def _rij(tekst: str, begin: str) -> str:
    # lstrip: de rijen in paia-handoff staan ingesprongen in een genummerde lijst
    return next(r for r in tekst.splitlines() if r.lstrip().startswith(begin))


def test_vaste_naad_spec_gaat_naar_de_controller():
    rij = _rij(
        AGENTS.read_text(encoding="utf-8"),
        "| spec gecommit in `docs/developer/planning/specs/` |",
    )
    for stuk in [
        "controller via `paia-handoff`",
        "worktree is dan van hem",
        "opt-out vóór de commit",
        "`/paia-spec-adversarial-review <spec>`",
    ]:
        assert stuk in rij


def test_akkoord_dan_meteen_commit_en_handoff():
    assert "`superpowers:brainstorming` (na akkoord meteen commit en handoff)" in _plat(
        AGENTS
    )


@pytest.mark.parametrize("pad", HANDOFF, ids=lambda p: p.parts[-3])
def test_handoff_spec_rij_standaard(pad):
    t = pad.read_text(encoding="utf-8")
    assert "`/paia-spec-adversarial-review` op die spec | `standaard` |" in t
    assert "`/superpowers:writing-plans` op die spec | `nee` |" in t
    assert "de controller komt pas in beeld vanaf het plan" not in t
    assert "vanaf het plan, zie de kolom Controller" not in t


@pytest.mark.parametrize("pad", HANDOFF, ids=lambda p: p.parts[-3])
def test_handoff_standaard_vraagt_niets(pad):
    s = _plat(pad)
    assert "Bij `standaard` stel je geen vraag" in s
    assert "de worktree is nu van de controller" in s


@pytest.mark.parametrize("pad", HANDOFF, ids=lambda p: p.parts[-3])
def test_handoff_standaard_alleen_op_de_beelink(pad):
    s = _plat(pad)
    i = s.index("Bij `standaard` stel je geen vraag")
    assert "uv run python -m src.controller.host check" in s[i : i + 1500]


@pytest.mark.parametrize("pad", HANDOFF, ids=lambda p: p.parts[-3])
def test_handoff_standaard_alleen_in_het_brainstormvenster(pad):
    s = _plat(pad)
    assert "alleen in het venster dat de spec net committe" in s
    assert "opt-out" in s


@pytest.mark.parametrize("pad", HANDOFF, ids=lambda p: p.parts[-3])
def test_handoff_context_vol_maakt_van_standaard_nee(pad):
    assert "`standaard` wordt `nee`" in _rij(
        pad.read_text(encoding="utf-8"), "| context vol, zelfde fase |"
    )


PLAN_C = REPO / ".claude/skills/paia-plan-adversarial-review/SKILL.md"
PLAN_A = REPO / ".agents/skills/paia-plan-adversarial-review/SKILL.md"


def test_plan_review_controller_run_sectie():
    t = PLAN_C.read_text(encoding="utf-8")
    assert t.index("## Controller-run") < t.index("## Workflow")
    s = " ".join(_sectie(t, "## Controller-run").split())
    for zin in [
        "(issue #<nr>, controller-run)",
        "`PAIA_CONTROLLER_RUN`",
        "meteen *Fallback zonder Codex*, zonder vraag",
        "`🤖 Vraag`",
        "*Spec aanpassen*",
        "`volgende: /paia-spec-adversarial-review <absoluut specpad>`",
        "*Door met het punt open*",
        "De run past de spec nooit uit zichzelf materieel aan.",
        "*Door naar SDD met de open punten*",
        "een fix waarmee het plan van de spec afwijkt",
        "twijfel of iets de spec tegenspreekt",
    ]:
        assert zin in s, zin


def test_plan_review_ronde_1_draagt_de_wachtregel():
    t = PLAN_C.read_text(encoding="utf-8")
    start = t.index("#### Ronde 1: Codex en Opus")
    ronde1 = t[start : t.index("### 6. Gate", start)]
    assert "**Controller-run:**" in ronde1
    assert "#1092" in ronde1


def test_agents_plan_review_kent_geen_controller_run():
    assert "Controller-runs bestaan alleen in Claude Code" in _plat(PLAN_A)


SPEC_C = REPO / ".claude/skills/paia-spec-adversarial-review/SKILL.md"
SPEC_A = REPO / ".agents/skills/paia-spec-adversarial-review/SKILL.md"


def test_spec_review_controller_run_sectie():
    t = SPEC_C.read_text(encoding="utf-8")
    assert t.index("## Controller-run") < t.index("## Workflow")
    s = " ".join(_sectie(t, "## Controller-run").split())
    for zin in [
        "(issue #<nr>, controller-run)",
        "`PAIA_CONTROLLER_RUN`",
        "meteen *Fallback zonder Codex*, zonder vraag",
        "MINOR",
        "(tenet C)",
        "(tenet B)",
        "`scripts/controller_run_instructies.md`",
        "`volgende: /superpowers:writing-plans <absoluut specpad>`",
        "`--outcome aborted`",
        "`🤖 Vraag`",
        "*Door naar writing-plans met de open punten*",
        "Geen tweede `write`",
        "*Interactief verder*",
        "`volgende: /superpowers:executing-plans <absoluut specpad>`",
        "*Nog een ronde* is geen optie",
        "*Ronde 1: Codex en Opus*",
    ]:
        assert zin in s, zin


def test_spec_review_volgend_venster_alleen_interactief():
    assert (
        "Interactief draait `superpowers:writing-plans` pas in het volgende venster"
        in _plat(SPEC_C)
    )


def test_agents_spec_review_kent_geen_controller_run():
    assert "Controller-runs bestaan alleen in Claude Code" in _plat(SPEC_A)
