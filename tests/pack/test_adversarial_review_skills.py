"""Contract test for the skills `lighthouse-spec-adversarial-review` and
`lighthouse-plan-adversarial-review`.

What lives only in skill text: the convergence rules (round 1 is a review, every later round only
verifies the fixes), the budget fallback at exit 6, the paired round 1 (Codex and Opus, decided
blind) and the Opus dispatch in Claude Code, the round limit that records the open points and
carries on instead of asking, and the controller run that runs only if present (design choice 16).
The `agents` variant lacks the controller run, the paired round 1 and the Opus dispatch, and
records a review from inside Codex as `codex_same_family` (inventaris §8).

From paia's `test_adversarial_review_skills.py`, `test_review_pair_skills.py`,
`test_review_round_limit_instructies.py`, `test_controller_vanaf_spec_instructies.py` and
`test_controller_config_regels.py` (the parts about these two skills).
"""

from __future__ import annotations

import json
import re
import subprocess
from pathlib import Path

import pytest

from tests.pack._testkit import CORE

SKILLS = CORE / "skills"
NAMES = ("spec-adversarial-review", "plan-adversarial-review")
VARIANTS = ("claude", "agents")
ADVERSARY = CORE / "agents" / "spec-adversary.md"


def skill(name: str, variant: str) -> str:
    return (SKILLS / name / variant / "SKILL.md").read_text(encoding="utf-8")


def flat(text: str) -> str:
    return " ".join(text.split())


def section(text: str, heading: str) -> str:
    start = text.index(heading)
    end = text.find("\n## ", start + len(heading))
    return text[start:] if end == -1 else text[start:end]


ALL = [pytest.param(n, v, id=f"{n}-{v}") for n in NAMES for v in VARIANTS]
BOTH = [pytest.param(v, id=v) for v in VARIANTS]


# --- convergence -------------------------------------------------------------------------------


@pytest.mark.parametrize(("name", "variant"), ALL)
def test_review_verifies_instead_of_rereviewing(name, variant):
    text = skill(name, variant)
    assert "verify.sh" in text
    assert "Only round 1 is a review" in text
    assert "No fixes, no round" in text
    assert "MINORs never start a round" in text
    assert "`git diff --no-index` exits 1 when there is a difference" in text
    assert "A third round only if a fix from round 2 does not hold" in text
    assert "Every accepted ID is in `verified` or in `verifies` of a finding" in text
    assert "\\(.verifies" in text
    assert ".verified" in text


@pytest.mark.parametrize("variant", BOTH)
def test_plan_review_has_no_open_question_category(variant):
    assert "open_vraag" not in skill("plan-adversarial-review", variant)


# --- budget --------------------------------------------------------------------------------------


@pytest.mark.parametrize(("name", "variant"), ALL)
def test_skills_know_budget_exit_6(name, variant):
    text = skill(name, variant)
    assert "exit 6" in text
    assert ".budget.json" in text
    assert "`reset_at`" in text
    assert "`reason`" in text
    assert "reden" not in text


@pytest.mark.parametrize("name", NAMES)
def test_claude_skills_fall_back_at_exit_6(name):
    text = skill(name, "claude")
    assert "Three triggers" in text
    assert "before the round (exit 6)" in text
    assert "Only round 1 runs on Codex, so only round 1 has a fallback" in text


def test_plan_failure_table_has_exit_6():
    assert (
        "| Codex exit 6 (budget exhausted beforehand) | "
        "`mv <ATTEMPT>/round-1.fallback.json <ATTEMPT>/round-1.json` | "
        "`fallback --reviewer-model <reported model>` |"
    ) in skill("plan-adversarial-review", "claude")


@pytest.mark.parametrize("name", NAMES)
def test_agents_skills_stop_at_exit_6(name):
    assert "stop **before** the round" in skill(name, "agents")


def test_spec_adversary_knows_exit_6_without_retry():
    text = ADVERSARY.read_text(encoding="utf-8")
    assert "| 6 | `CODEX_BUDGET`" in text
    assert "no retry" in text


def _step2_jq(text: str) -> str:
    block = text.split("```bash\n   jq -r --arg round \"$ROUND\" '", 1)[1]
    return block.split("' \"$OUT\"", 1)[0]


def test_spec_adversary_does_not_collapse_minors(tmp_path):
    """Every ID the operator decides on is in view with its problem and fix."""
    text = ADVERSARY.read_text(encoding="utf-8")
    assert "Collapse MINORs" not in text
    assert "One row per finding, MINORs included" in text

    findings = [
        {
            "severity": "MINOR",
            "spec_section": f"S{i}",
            "problem": f"problem {i}",
            "failure_scenario": f"fails {i}",
            "evidence": f"a.md:{i}",
            "proposed_fix": f"fix {i}",
        }
        for i in range(1, 6)
    ]
    findings.insert(2, {**findings[0], "severity": "MAJOR", "problem": "big"})
    out = tmp_path / "round-1.json"
    out.write_text(json.dumps({"findings": findings}))

    rows = subprocess.run(
        ["jq", "-r", "--arg", "round", "1", _step2_jq(text), str(out)],
        capture_output=True,
        text=True,
        check=True,
    ).stdout.splitlines()

    assert len(rows) == len(findings)
    assert rows[0].startswith("| r1-f3 | MAJOR |")
    for idx, finding in enumerate(findings, start=1):
        (row,) = [r for r in rows if r.startswith(f"| r1-f{idx} |")]
        assert f"| {finding['problem']} |" in row
        assert f"| {finding['proposed_fix']} |" in row


# --- spec gate -----------------------------------------------------------------------------------


@pytest.mark.parametrize("variant", BOTH)
def test_spec_review_motivates_against_builder_check(variant):
    text = skill("spec-adversarial-review", variant)
    prompt = (SKILLS / "spec-adversarial-review" / "reviewer-prompt.md").read_text()
    for stale in ("spec-document-reviewer-prompt", "self-review", "SKILL.md:119"):
        assert stale not in text
        assert stale not in prompt
    assert "builder check" in text
    assert "builder-check-prompt.md" in text


def test_reviewer_prompt_skips_builder_freedom():
    text = (SKILLS / "spec-adversarial-review" / "reviewer-prompt.md").read_text()
    assert "explicitly leaves to the implementer" in text
    assert "a reasonable implementer could decide" in text
    assert "never to Ground truth or Gaps" in text


@pytest.mark.parametrize("variant", BOTH)
def test_spec_gate_sorts_three_piles(variant):
    text = skill("spec-adversarial-review", variant)
    for pile in ("**Already answered**", "**Small**", "**The user's**"):
        assert pile in text
    assert "*(own choice after spec review, <ID>)*" in text
    assert "at most four" in text
    assert "each MINOR gets its own line" in text


def test_claude_spec_gate_asks_through_the_controller_only_if_present():
    s = flat(skill("spec-adversarial-review", "claude"))
    assert "in a controller run (if present) a numbered list as the last text" in s


# --- paired round 1 and the Opus dispatch --------------------------------------------------------


def test_claude_plan_skill_runs_the_pair_in_round_one():
    text = skill("plan-adversarial-review", "claude")
    for needle in (
        "python3 .lighthouse/scripts/review_pair.py blind",
        "python3 .lighthouse/scripts/review_pair.py merge",
        "round-1.blind-decisions.json",
        "--shadow-model",
        "pair.json",
        "round-1.codex.log",
        "git status --porcelain",
    ):
        assert needle in text, needle


def test_claude_spec_skill_records_and_pairs():
    text = skill("spec-adversarial-review", "claude")
    for needle in (
        "python3 .lighthouse/scripts/plan_review_record.py start --kind spec",
        "python3 .lighthouse/scripts/plan_review_record.py write",
        "minor_yes",
        "defer",
        "blind-decisions",
    ):
        assert needle in text, needle


@pytest.mark.parametrize("name", NAMES)
def test_agents_skills_say_the_pair_is_claude_code_only(name):
    text = skill(name, "agents")
    assert "Paired round 1: Claude Code only" in text
    assert "review_pair.py blind" not in text


def test_agents_spec_skill_records_with_its_own_skill_dir():
    text = skill("spec-adversarial-review", "agents")
    assert "python3 .lighthouse/scripts/plan_review_record.py start --kind spec" in text
    assert "--skill-dir .agents/skills/lighthouse-spec-adversarial-review" in text


@pytest.mark.parametrize("name", NAMES)
def test_claude_skills_pair_and_verify_on_opus(name):
    text = skill(name, "claude")
    for needle in (
        "Round 1: Codex and Opus",
        "## Opus dispatch",
        'model: "opus"',
        "verifier-prompt.md",
    ):
        assert needle in text, needle
    assert "review_pair.py status" not in text


@pytest.mark.parametrize("name", NAMES)
def test_variants_keep_their_difference(name):
    """Inventaris §8: the `agents` variant lacks the controller run, the paired round 1 and  # lighthouse: allow-language — reference to a Dutch inventory section
    the Opus dispatch, and records a review from inside Codex as `codex_same_family`."""
    claude, agents = skill(name, "claude"), skill(name, "agents")
    for only_claude in ("## Controller run", "Round 1: Codex and Opus", "## Opus dispatch"):
        assert only_claude in claude, only_claude
        assert only_claude not in agents, only_claude
    assert "codex_same_family" in agents
    assert "codex_same_family" not in claude


# --- round limit ---------------------------------------------------------------------------------


@pytest.mark.parametrize(("name", "variant"), ALL)
def test_round_limit_records_and_carries_on(name, variant):
    text = skill(name, variant)
    assert "python3 .lighthouse/scripts/plan_review_record.py accept-open <ATTEMPT> --doc" in text
    assert "heading:open_after_review" in text
    assert "never a question" in text
    assert "ACCEPT-OPEN FAILED" in text
    assert "nothing to accept" in text


@pytest.mark.parametrize("variant", BOTH)
def test_spec_decision_in_round_3_does_not_stop_the_chain(variant):
    assert "in round 3 a `spec` decision does not stop the chain" in skill(
        "plan-adversarial-review", variant
    )


def test_pr_template_knows_the_open_review_points_heading():
    assert "## Open review points" in (CORE / "templates" / "pull_request_template.md").read_text()


# --- controller run (only if present) ------------------------------------------------------------


@pytest.mark.parametrize("name", NAMES)
def test_controller_run_section_is_guarded(name):
    text = skill(name, "claude")
    heading = "## Controller run (only if present)"
    assert text.index(heading) < text.index("## Workflow")
    s = flat(section(text, heading))
    for phrase in (
        "(issue #<nr>, controller-run)",
        "`LIGHTHOUSE_CONTROLLER_RUN`",
        "grep -q '^\\[controller\\]' lighthouse.toml",
        "*Fallback without Codex* at once, without a question",
        "`🤖 heading:kind_question`",
        "`controller.bij_rondegrens`",
    ):
        assert phrase in s, phrase
    assert "PAIA_CONTROLLER_RUN" not in text


def test_plan_controller_run_section():
    s = flat(section(skill("plan-adversarial-review", "claude"), "## Controller run"))
    for phrase in (
        "*Adjust the spec*",
        "`next: /lighthouse-spec-adversarial-review <absolute spec path>`",
        "*Carry on with the point open*",
        "The run never changes the spec materially by itself.",
        "*On to SDD with the open points*",
        "a fix with which the plan departs from the spec",
        "doubt whether something contradicts the spec",
    ):
        assert phrase in s, phrase


def test_plan_round_1_carries_the_controller_wait_rule():
    text = skill("plan-adversarial-review", "claude")
    start = text.index("#### Round 1: Codex and Opus")
    round1 = text[start : text.index("### 6. Gate", start)]
    assert "**Controller run (if present):**" in round1


def test_spec_controller_run_section():
    s = flat(section(skill("spec-adversarial-review", "claude"), "## Controller run"))
    for phrase in (
        "MINOR",
        "`next: /superpowers:writing-plans <absolute spec path>`",
        "`--outcome aborted`",
        "*On to writing-plans with the open points*",
        "No second `write`",
        "*Continue interactively*",
        "`next: /superpowers:executing-plans <absolute spec path>`",
        "*Another round* is no option",
        "*Round 1: Codex and Opus*",
    ):
        assert phrase in s, phrase


def test_spec_review_next_window_only_interactive():
    assert (
        "Interactively, `superpowers:writing-plans` runs only in the next window"
        in flat(skill("spec-adversarial-review", "claude"))
    )


@pytest.mark.parametrize("name", NAMES)
def test_agents_skills_know_no_controller_run(name):
    assert "Controller runs exist only in Claude Code" in flat(skill(name, "agents"))


# --- detached from paia --------------------------------------------------------------------------


@pytest.mark.parametrize(("name", "variant"), ALL)
def test_skill_is_detached_from_the_source_repo(name, variant):
    text = skill(name, variant)
    for gone in (
        "Beelink",
        "uv run python scripts/",
        "experiments/",
        "scripts/controller_launch.sh",
        "accepteer-open",
        "docs/developer/tenets.md",
    ):
        assert gone not in text, gone
    assert not re.search(r"\(#\d+\)|#\d{3,}", text)
    assert "python3 .lighthouse/scripts/plan_review_record.py" in text


@pytest.mark.parametrize("name", NAMES)
def test_claude_skills_name_the_always_on_host(name):
    assert "the always-on host (`host.toml`)" in skill(name, "claude")


@pytest.mark.parametrize("variant", BOTH)
def test_spec_dir_from_config(variant):
    text = skill("spec-adversarial-review", variant)
    assert "python3 .lighthouse/lib/lighthouse_lib/config.py paths.specs" in text
    assert "docs/developer/planning/specs/" not in text


# --- permissions ---------------------------------------------------------------------------------


def test_permissions_cover_scripts():
    """Every runner a skill starts in the background has its allowlist rule (decision 14):
    without it an unattended round waits on an approval prompt."""
    allow = json.loads((CORE / "permissions.json").read_text())["allow"]
    scripts = sorted(SKILLS.glob("*/review.sh")) + sorted(SKILLS.glob("*/verify.sh"))
    want = {f"Bash(.claude/skills/lighthouse-{p.parent.name}/{p.name}:*)" for p in scripts}
    assert len(scripts) == 4
    assert set(allow) == want
    assert len(allow) == len(want)


@pytest.mark.parametrize("name", NAMES)
def test_scripts_are_executable(name):
    for script in ("review.sh", "verify.sh"):
        assert (SKILLS / name / script).stat().st_mode & 0o111, script


@pytest.mark.parametrize(("name", "variant"), ALL)
def test_prompts_and_schemas_shared(name, variant):
    """The prompts, schemas and runners are one file per skill, not one per variant."""
    for f in ("reviewer-prompt.md", "verifier-prompt.md", "findings.schema.json",
              "verification.schema.json", "review.sh", "verify.sh"):
        assert (SKILLS / name / f).is_file(), f
        assert not (SKILLS / name / variant / f).exists(), f


def _no_dutch(text: str) -> list[str]:
    return [w for w in (" je ", " het ", " niet ", " geen ", " een ", " wordt ") if w in text.lower()]


@pytest.mark.parametrize("name", NAMES)
def test_shared_files_are_english(name):
    for f in ("reviewer-prompt.md", "verifier-prompt.md", "review.sh", "verify.sh"):
        text = (SKILLS / name / f).read_text()
        assert not _no_dutch(text), (f, _no_dutch(text))


@pytest.mark.parametrize(("name", "variant"), ALL)
def test_skill_is_english(name, variant):
    assert not _no_dutch(skill(name, variant))


def test_pinned_paths_exist():
    for p in (SKILLS / n / v / "SKILL.md" for n in NAMES for v in VARIANTS):
        assert p.is_file(), p
    assert Path(CORE / "permissions.json").is_file()
