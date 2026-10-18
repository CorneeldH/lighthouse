"""Contract van de skill `paia-verify-pr` en het PR-template (#1091, taak 6).

De skill is de tegenhanger van `.claude/hooks/pr-create-gate.sh`: hij schrijft
de PR-body (`Closes #N` + Evidence-tabel, `Refs #N — <reden>` of
`No issue: <reden>`) die de hook afdwingt. Deze tests binden de skilltekst aan
de hook, aan de CLI van `evidence.py` en aan diens allowlist-constanten.
"""

import re
from pathlib import Path

import yaml

from tests.unit._hook_testkit import load_hook_lib

ROOT = Path(__file__).resolve().parents[2]
CLAUDE = ROOT / ".claude" / "skills" / "paia-verify-pr" / "SKILL.md"
AGENTS = ROOT / ".agents" / "skills" / "paia-verify-pr" / "SKILL.md"
TEMPLATE = ROOT / ".github" / "pull_request_template.md"


def front(pad: Path) -> str:
    """De YAML-frontmatter tussen de eerste twee `---`-regels."""
    return pad.read_text(encoding="utf-8").split("---\n", 2)[1]


def test_spiegels_identiek():
    assert CLAUDE.read_bytes() == AGENTS.read_bytes()


def test_binding_hard_onder_metadata():
    assert yaml.safe_load(front(CLAUDE))["metadata"]["binding"] == "hard"


def test_description_niet_afgekapt():
    """Een ongequote ` #N` is in YAML commentaar en knipt de LET OP-clausule af."""
    assert yaml.safe_load(front(CLAUDE))["description"].endswith("is paia-fix-pr.")


def test_skill_draait_run_voor_pr():
    assert "--voor-pr" in CLAUDE.read_text(encoding="utf-8")


def test_noemt_hook():
    """Check 8 koppelt `binding: hard` aan de hook via deze naam."""
    assert ".claude/hooks/pr-create-gate.sh" in CLAUDE.read_text(encoding="utf-8")


def test_noemt_beide_nieuwe_verdicts():
    body = CLAUDE.read_text(encoding="utf-8")
    assert "`na merge`" in body and "`na PR`" in body


def test_noemt_evidence_cli():
    body = CLAUDE.read_text(encoding="utf-8")
    assert "evidence.py check" in body and "evidence.py run" in body


def test_allowlist_tabel_dekt_de_code():
    """De skilltabel noemt elk commando, elke weigering (aanscherpingen a–e) en
    elke vlag uit de allowlist-constanten van `evidence.py`."""
    ev = load_hook_lib("evidence")
    body = CLAUDE.read_text(encoding="utf-8")
    for cmd in (
        "`grep`",
        "`git grep`",
        "`git log`",
        "`uv run pytest`",
        "`gh api <pad>`",
        "`gh api graphql`",
        "`gh pr view`",
    ):
        assert cmd in body
    for geweigerd in ("--web", "--output", "-H", "%G"):
        assert geweigerd in body
    vlaggen = (
        ev.GIT_LOG_FLAGS
        | ev.GIT_LOG_VAL
        | ev.PYTEST_FLAGS
        | ev.GH_API_VAL
        | ev.GH_VIEW_FLAGS
        | ev.GH_VIEW_VAL
        | ev.LS_FLAGS
        | ev.WC_FLAGS
        | ev.CAT_FLAGS
        | ev.GIT_GREP_VAL
    )
    sectie = body[body.index("## Evidence-allowlist") :]
    sectie = sectie[: sectie.index("```")]  # alleen de tabel en zijn proza
    in_code = {re.sub(r"[`=]", "", v) for v in re.findall(r"`[^`]*`", sectie)}
    tokens = {t for cel in in_code for t in re.split(r"[\s/,]+", cel)}
    ontbreekt = sorted(v for v in vlaggen if v not in tokens)
    assert not ontbreekt, (
        f"vlaggen uit evidence.py ontbreken in de skilltabel: {ontbreekt}"
    )
    for sub in ev.GIT_SUBS:
        assert f"git {sub}" in body


def test_template_kent_drie_vormen():
    t = TEMPLATE.read_text(encoding="utf-8")
    assert "Evidence #" in t and "Refs #123 —" in t and "No issue:" in t


def test_template_verbiedt_closes_niet():
    assert "No `Closes`" not in TEMPLATE.read_text(encoding="utf-8")


def test_template_bevat_geen_geldige_vorm_buiten_comments():
    """Het lege template blokkeert: de voorbeeldvormen staan alleen in een comment.

    Netwerkverbod: de AC-telling krijgt een stub; zonder `Closes` buiten de
    comment wordt hij niet eens aangeroepen.
    """
    gate = load_hook_lib("pr_body_gate")
    verdict = gate.check_body(
        TEMPLATE.read_text(encoding="utf-8"), ac_count=lambda _nr: (None, "stub")
    )
    assert verdict.blokkades


def _sectie(body: str, kop: str) -> str:
    s = body[body.index(kop) :]
    return s[: s.index("\n## ", 1)]


def test_voorbeeldrij_klopt_tegen_het_template():
    """Rij 1 van het voorbeeldblok, gekopieerd, moet `klopt` geven (grep, geen netwerk)."""
    gate = load_hook_lib("pr_body_gate")
    ev = load_hook_lib("evidence")
    body = CLAUDE.read_text(encoding="utf-8")
    # Het voorbeeld bevat zelf `## Related issues`: knip tot de volgende echte kop.
    blok = body[body.index("## Vorm van het bewijsblok") : body.index("## Tegenhanger")]
    rijen = gate.evidence_rows(blok, 1091)
    assert isinstance(rijen, list), rijen
    assert ev.run_row(rijen[0], str(ROOT), gh_ok=False)[0] == "klopt"


def test_v4_geeft_titel_en_schrijft_body_apart():
    v4 = _sectie(CLAUDE.read_text(encoding="utf-8"), "## V4.")
    assert "gh pr create --title '<titel>' --body-file <pad>" in v4
    assert "Write-tool" in v4


def test_dod_draait_de_suite_niet_handmatig_in_claude_code():
    dod = _sectie(CLAUDE.read_text(encoding="utf-8"), "## DoD")
    assert ".claude/hooks/pre-commit-tests.sh" in dod
    assert "Buiten Claude Code" in dod
    assert dod.index("Buiten Claude Code") < dod.index("uv run pytest tests/unit/")


def test_gh_view_eist_een_nummer_en_na_pr_meet_via_het_issue():
    body = CLAUDE.read_text(encoding="utf-8")
    assert "precies één nummer of github.com-URL" in _sectie(
        body, "## Evidence-allowlist"
    )
    v3 = _sectie(body, "## V3.")
    assert "closedByPullRequestsReferences" in v3
    assert "`gh pr view` zonder nummer" in v3


def test_git_grep_rij_noemt_wat_evidence_weigert():
    """`git grep` kent alleen `-e` als waarde-optie; de rest van grep's waarde-opties niet."""
    ev = load_hook_lib("evidence")
    body = CLAUDE.read_text(encoding="utf-8")
    [rij] = [r for r in body.splitlines() if r.startswith("| `git grep` |")]
    _, toegestaan, geweigerd = (c.strip() for c in rij.strip("|").split(" | "))
    assert "als `grep`" not in toegestaan
    for optie in ev.GREP_VAL - ev.GIT_GREP_VAL:
        assert f"`{optie}`" in geweigerd
