"""Contract van de adversariële review-runner (#972).

`paia-spec-adversarial-review/review.sh` is de enige Codex-runner; de plan-review
roept hem aan via een wrapper met eigen prompt en schema. Deze tests draaien het
echte script tegen een nep-`codex` op PATH die de prompt en de argv vastlegt. Zo
toetsen ze wat #972 belooft zonder netwerk of Codex-account:

- zonder nieuwe vlaggen en zonder sidecar is de prompt byte-gelijk aan die van
  vóór #972 (`legacy_prompt` hieronder is dat oude algoritme, letterlijk);
- met een decisions-sidecar verandert alleen het prior-blok;
- de wrapper stuurt de plan-prompt en het plan-schema mee, en `--context` landt
  in `{{CONTEXT_PATHS}}`.
"""

from __future__ import annotations

import json
import os
import pathlib
import subprocess
import sys

import pytest

REPO = pathlib.Path(__file__).resolve().parents[2]
ROOTS = (REPO / ".claude" / "skills", REPO / ".agents" / "skills")

FAKE_CODEX = """#!/usr/bin/env python3
import json, os, pathlib, sys
args = sys.argv[1:]
cap = pathlib.Path(os.environ["FAKE_CODEX_CAPTURE"])
cap.mkdir(parents=True, exist_ok=True)
(cap / "argv.json").write_text(json.dumps(args))
(cap / "prompt.txt").write_text(args[-1])
out = args[args.index("-o") + 1]
pathlib.Path(out).write_text(json.dumps({
    "review_markdown": "x", "status": "Clean", "findings": [],
    "top3": [], "verdict": "v", "could_not_verify": [],
}))
"""

FAKE_BUDGET = """#!/usr/bin/env python3
import json, os, sys
mode = os.environ.get("FAKE_BUDGET_MODE", "codex")
if mode == "codex":
    print(json.dumps({"besluit": "codex", "reden": "onder de drempels"}))
elif mode == "fallback":
    print(json.dumps({"besluit": "fallback", "reden": "5u 92% \u2265 90%"}))
    sys.exit(6)
elif mode == "fallback-kapot":
    print("geen json")
    sys.exit(6)
elif mode == "crash":
    sys.stderr.write("Traceback (most recent call last):\\n  boem\\n")
    sys.exit(1)
elif mode == "lijst":
    print("[]")
elif mode == "lijst6":
    print("[]")
    sys.exit(6)
"""

FAKE_UV = """#!/usr/bin/env python3
import json, os, pathlib, sys
cap = pathlib.Path(os.environ["FAKE_CODEX_CAPTURE"])
cap.mkdir(parents=True, exist_ok=True)
(cap / "uv-argv.json").write_text(json.dumps(sys.argv))
print(json.dumps({"besluit": "codex", "reden": "onder de drempels"}))
"""

NEGEN = [
    "besluit",
    "reden",
    "venster",
    "reset_at",
    "session_pct",
    "week_pct",
    "bron",
    "ondergrens",
    "drempels",
]

SCRIPTS = [
    ("paia-spec-adversarial-review", "review.sh"),
    ("paia-plan-adversarial-review", "review.sh"),
    ("paia-plan-adversarial-review", "verify.sh"),
    ("paia-spec-adversarial-review", "verify.sh"),
]


def legacy_prompt(
    template: pathlib.Path, doc: str, repo: str, round_no: str, prior: str
) -> str:
    """De substitutie van review.sh zoals hij vóór #972 was, regel voor regel.

    `print(body)` plus `"$(cat ...)"` in bash: de afsluitende newline valt weg.
    """
    text = template.read_text()
    body = (
        text.split("---BEGIN PROMPT---", 1)[1].split("---END PROMPT---", 1)[0].strip()
    )
    prior_block = ""
    if prior != "none":
        prior_block = (
            f"Previous review: `{prior}`. The author's responses are recorded there.\n"
            "Check specifically which findings were answered in prose but not actually\n"
            "resolved in the spec. Do not re-raise a finding the author rejected with a\n"
            "stated reason — instead say whether that reasoning is wrong, and why."
        )
    body = (
        body.replace("{{SPEC_PATH}}", doc)
        .replace("{{REPO}}", repo)
        .replace("{{ROUND}}", round_no)
        .replace("{{PRIOR_REVIEW_BLOCK}}", prior_block)
    )
    return body.rstrip("\n")


@pytest.fixture
def sandbox(tmp_path: pathlib.Path) -> dict:
    """Een wegwerp-gitrepo met één document, een nep-codex en een schone HOME."""
    repo = tmp_path / "repo"
    (repo / "docs").mkdir(parents=True)
    (repo / "docs" / "doc.md").write_text("# doc\n")
    (repo / "docs" / "spec.md").write_text("# spec\n")
    subprocess.run(["git", "init", "-q", str(repo)], check=True)
    bin_dir = tmp_path / "bin"
    bin_dir.mkdir()
    codex = bin_dir / "codex"
    codex.write_text(
        FAKE_CODEX.replace("#!/usr/bin/env python3", f"#!{sys.executable}", 1)
    )
    codex.chmod(0o755)
    budget = bin_dir / "budget-stub"
    budget.write_text(
        FAKE_BUDGET.replace("#!/usr/bin/env python3", f"#!{sys.executable}", 1)
    )
    budget.chmod(0o755)
    home = tmp_path / "home"
    (home / ".codex").mkdir(parents=True)
    (home / ".codex" / "auth.json").write_text("{}")
    env = {
        **os.environ,
        "PATH": f"{bin_dir}{os.pathsep}{os.environ['PATH']}",
        "HOME": str(home),
        "CODEX_SPECREVIEW_HOME": str(tmp_path / "ch"),
        "TMPDIR": str(tmp_path),
        "FAKE_CODEX_CAPTURE": str(tmp_path / "capture"),
        "PAIA_CODEX_BUDGET_CMD": str(budget),
    }
    return {"repo": repo, "env": env, "capture": tmp_path / "capture", "tmp": tmp_path}


def run(script: pathlib.Path, sandbox: dict, *args: str) -> subprocess.CompletedProcess:
    return subprocess.run(
        [str(script), *args],
        cwd=sandbox["repo"],
        env=sandbox["env"],
        capture_output=True,
        text=True,
        check=False,
    )


def captured(sandbox: dict) -> tuple[str, list[str]]:
    cap = sandbox["capture"]
    return (cap / "prompt.txt").read_text(), json.loads((cap / "argv.json").read_text())


@pytest.mark.parametrize("root", ROOTS, ids=lambda p: p.parent.name)
@pytest.mark.parametrize("prior", ["none", "PRIOR"])
def test_spec_review_prompt_is_byte_identical_without_new_flags(root, prior, sandbox):
    skill = root / "paia-spec-adversarial-review"
    out = sandbox["tmp"] / "out" / "round-2.json"
    prior_arg = (
        "none" if prior == "none" else str(sandbox["tmp"] / "out" / "round-1.json")
    )
    result = run(skill / "review.sh", sandbox, "docs/doc.md", "2", str(out), prior_arg)
    assert result.returncode == 0, result.stderr
    prompt, argv = captured(sandbox)
    toplevel = subprocess.run(
        ["git", "rev-parse", "--show-toplevel"],
        cwd=sandbox["repo"],
        capture_output=True,
        text=True,
        check=True,
    ).stdout.strip()
    assert prompt == legacy_prompt(
        skill / "reviewer-prompt.md", "docs/doc.md", toplevel, "2", prior_arg
    )
    assert argv[argv.index("--output-schema") + 1] == str(
        skill / "findings.schema.json"
    )


@pytest.mark.parametrize("root", ROOTS, ids=lambda p: p.parent.name)
def test_sidecar_changes_only_the_prior_block(root, sandbox):
    skill = root / "paia-spec-adversarial-review"
    outdir = sandbox["tmp"] / "out"
    outdir.mkdir()
    prior = outdir / "round-1.json"
    sidecar = outdir / "round-1.decisions.json"
    sidecar.write_text('[{"finding_id": "r1-f1", "decision": "reject", "reason": "x"}]')
    result = run(
        skill / "review.sh",
        sandbox,
        "docs/doc.md",
        "2",
        str(outdir / "round-2.json"),
        str(prior),
    )
    assert result.returncode == 0, result.stderr
    prompt, _ = captured(sandbox)
    toplevel = subprocess.run(
        ["git", "rev-parse", "--show-toplevel"],
        cwd=sandbox["repo"],
        capture_output=True,
        text=True,
        check=True,
    ).stdout.strip()
    legacy = legacy_prompt(
        skill / "reviewer-prompt.md", "docs/doc.md", toplevel, "2", str(prior)
    )
    assert prompt != legacy
    assert f"`{sidecar}`" in prompt
    assert "r<round>-f<index>" in prompt
    # Alles buiten het prior-blok is gelijk: knip het blok uit beide en vergelijk.
    before, _, rest = legacy.partition("Previous review:")
    after_legacy = rest.split("and why.", 1)[1]
    new_before, _, new_rest = prompt.partition("Previous review:")
    after_new = new_rest.split("wrong, and why.", 1)[1]
    assert (before, after_legacy) == (new_before, after_new)


@pytest.mark.parametrize("root", ROOTS, ids=lambda p: p.parent.name)
def test_context_on_a_template_without_the_placeholder_fails(root, sandbox):
    """De spec-prompt kent geen {{CONTEXT_PATHS}}: stil negeren zou de spec(s)
    wegmoffelen die de operator juist meegaf."""
    skill = root / "paia-spec-adversarial-review"
    out = sandbox["tmp"] / "out" / "round-1.json"
    result = run(
        skill / "review.sh",
        sandbox,
        "docs/doc.md",
        "1",
        str(out),
        "--context",
        "docs/spec.md",
    )
    assert result.returncode != 0
    assert "{{CONTEXT_PATHS}}" in result.stderr


@pytest.mark.parametrize("root", ROOTS, ids=lambda p: p.parent.name)
def test_plan_wrapper_sends_plan_prompt_schema_and_context(root, sandbox):
    plan_skill = root / "paia-plan-adversarial-review"
    out = sandbox["tmp"] / "out" / "round-1.json"
    result = run(
        plan_skill / "review.sh",
        sandbox,
        "docs/doc.md",
        "1",
        str(out),
        "none",
        "--context",
        "docs/spec.md",
    )
    assert result.returncode == 0, result.stderr
    prompt, argv = captured(sandbox)
    assert "Plan: docs/doc.md" in prompt
    assert "- docs/spec.md" in prompt
    assert "{{" not in prompt
    schema = pathlib.Path(argv[argv.index("--output-schema") + 1]).resolve()
    assert schema == (plan_skill / "findings.schema.json").resolve()


@pytest.mark.parametrize("root", ROOTS, ids=lambda p: p.parent.name)
def test_missing_context_path_is_a_usage_error(root, sandbox):
    plan_skill = root / "paia-plan-adversarial-review"
    out = sandbox["tmp"] / "out" / "round-1.json"
    result = run(
        plan_skill / "review.sh",
        sandbox,
        "docs/doc.md",
        "1",
        str(out),
        "--context",
        "docs/bestaat-niet.md",
    )
    assert result.returncode == 2
    assert "contextpad niet gevonden" in result.stderr


EXECUTABILITY_CATS = [
    "spec_mismatch",
    "dependency_order",
    "repo_falsehood",
    "missing_artifact",
    "verification_gap",
    "unverified_external_assumption",
    "likely_runtime_failure",
]


@pytest.mark.parametrize("root", ROOTS, ids=lambda p: p.parent.name)
def test_plan_schema_requires_one_of_seven_categories(root):
    schema = json.loads(
        (root / "paia-plan-adversarial-review" / "findings.schema.json").read_text()
    )
    item = schema["properties"]["findings"]["items"]
    assert {"cat", "plan_section", "failure_scenario"} <= set(item["required"])
    assert "spec_section" not in item["properties"]
    assert item["properties"]["cat"]["enum"] == EXECUTABILITY_CATS


@pytest.mark.parametrize("root", ROOTS, ids=lambda p: p.parent.name)
def test_plan_prompt_asks_executability_not_improvement(root):
    text = (root / "paia-plan-adversarial-review" / "reviewer-prompt.md").read_text()
    body = text.split("---BEGIN PROMPT---", 1)[1].split("---END PROMPT---", 1)[0]
    for cat in EXECUTABILITY_CATS:
        assert f"`{cat}`" in body
    for gone in ("open_vraag", "operationeel", "tegenspraak"):
        assert gone not in body
    assert "prove that executing this plan will fail" in body
    assert "no concrete failure scenario, it is not a BLOCKER or MAJOR" in body


@pytest.mark.parametrize("root", ROOTS, ids=lambda p: p.parent.name)
def test_plan_verify_wrapper_sends_verifier_prompt_and_schema(root, sandbox):
    plan_skill = root / "paia-plan-adversarial-review"
    outdir = sandbox["tmp"] / "out"
    outdir.mkdir()
    (outdir / "round-1.json").write_text("{}")
    diff = outdir / "round-2.fixes.diff"
    diff.write_text("--- a\n+++ b\n")
    result = run(
        plan_skill / "verify.sh",
        sandbox,
        "docs/doc.md",
        "2",
        str(outdir / "round-2.json"),
        str(outdir / "round-1.json"),
        "--context",
        "docs/spec.md",
        "--context",
        str(diff),
    )
    assert result.returncode == 0, result.stderr
    prompt, argv = captured(sandbox)
    assert "Plan: docs/doc.md" in prompt
    assert f"- {diff}" in prompt
    assert "Do not search for new problems" in prompt
    assert "Rejected findings are out of scope" in prompt
    assert "{{" not in prompt
    schema = pathlib.Path(argv[argv.index("--output-schema") + 1]).resolve()
    assert schema == (plan_skill / "verification.schema.json").resolve()


@pytest.mark.parametrize("root", ROOTS, ids=lambda p: p.parent.name)
def test_plan_verification_schema_is_scoped(root):
    schema = json.loads(
        (root / "paia-plan-adversarial-review" / "verification.schema.json").read_text()
    )
    assert "verified" in schema["required"]
    item = schema["properties"]["findings"]["items"]
    assert {"verifies", "failure_scenario", "cat"} <= set(item["required"])
    assert item["properties"]["severity"]["enum"] == ["BLOCKER", "MAJOR"]
    assert item["properties"]["cat"]["enum"] == [*EXECUTABILITY_CATS, "fix_incomplete"]


@pytest.mark.parametrize("root", ROOTS, ids=lambda p: p.parent.name)
def test_spec_blocker_needs_a_failure_scenario(root):
    skill = root / "paia-spec-adversarial-review"
    item = json.loads((skill / "findings.schema.json").read_text())["properties"][
        "findings"
    ]["items"]
    assert "failure_scenario" in item["required"]
    assert "cat" not in item["properties"]
    body = (skill / "reviewer-prompt.md").read_text().split("---BEGIN PROMPT---", 1)[1]
    assert "no concrete failure scenario, it is not a BLOCKER or MAJOR" in body
    assert "{{CONTEXT_PATHS}}" not in body


@pytest.mark.parametrize("root", ROOTS, ids=lambda p: p.parent.name)
def test_spec_verify_wrapper_sends_verifier_prompt_and_schema(root, sandbox):
    skill = root / "paia-spec-adversarial-review"
    outdir = sandbox["tmp"] / "out"
    outdir.mkdir()
    (outdir / "round-1.json").write_text("{}")
    diff = outdir / "round-2.fixes.diff"
    diff.write_text("--- a\n+++ b\n")
    result = run(
        skill / "verify.sh",
        sandbox,
        "docs/doc.md",
        "2",
        str(outdir / "round-2.json"),
        str(outdir / "round-1.json"),
        "--context",
        str(diff),
    )
    assert result.returncode == 0, result.stderr
    prompt, argv = captured(sandbox)
    assert "Spec: docs/doc.md" in prompt
    assert f"- {diff}" in prompt
    assert "Do not search for new problems" in prompt
    assert "Rejected findings are out of scope" in prompt
    schema = pathlib.Path(argv[argv.index("--output-schema") + 1]).resolve()
    assert schema == (skill / "verification.schema.json").resolve()


def test_verify_wrappers_are_allowlisted():
    allow = json.loads((REPO / ".claude" / "settings.json").read_text())["permissions"][
        "allow"
    ]
    assert "Bash(.claude/skills/paia-plan-adversarial-review/verify.sh:*)" in allow
    assert "Bash(.claude/skills/paia-spec-adversarial-review/verify.sh:*)" in allow


@pytest.mark.parametrize("root", ROOTS, ids=lambda p: p.parent.name)
@pytest.mark.parametrize(("skill", "script"), SCRIPTS)
def test_budget_fallback_stopt_voor_codex(root, skill, script, sandbox):
    sandbox["env"]["FAKE_BUDGET_MODE"] = "fallback"
    out = sandbox["tmp"] / "nieuw" / "diep" / "round-1.json"  # map bestaat nog niet
    res = run(root / skill / script, sandbox, "docs/doc.md", "1", str(out))
    assert res.returncode == 6
    assert "CODEX_BUDGET: fallback \u2014 5u 92% \u2265 90%" in res.stderr
    assert not (sandbox["capture"] / "argv.json").exists()
    record = json.loads((out.parent / "round-1.json.budget.json").read_text())
    assert record["besluit"] == "fallback"


@pytest.mark.parametrize("root", ROOTS, ids=lambda p: p.parent.name)
@pytest.mark.parametrize("mode", ["fallback-kapot", "crash", "lijst", "lijst6"])
def test_kapotte_budgetcheck_is_niet_meetbaar_en_codex_draait(root, mode, sandbox):
    sandbox["env"]["FAKE_BUDGET_MODE"] = mode
    out = sandbox["tmp"] / "out" / "round-1.json"
    res = run(
        root / "paia-spec-adversarial-review" / "review.sh",
        sandbox,
        "docs/doc.md",
        "1",
        str(out),
    )
    assert res.returncode == 0, res.stderr
    assert (sandbox["capture"] / "argv.json").exists()
    data = json.loads((out.parent / "round-1.json.budget.json").read_text())
    assert list(data) == NEGEN
    assert data["besluit"] == "niet_meetbaar"
    stub_exit = {"fallback-kapot": 6, "crash": 1, "lijst": 0, "lijst6": 6}[mode]
    assert data["reden"] == f"budgetcheck faalde: exit {stub_exit}"
    assert all(data[k] is None for k in NEGEN[2:])


@pytest.mark.parametrize("root", ROOTS, ids=lambda p: p.parent.name)
def test_budget_codex_schrijft_record_en_draait(root, sandbox):
    out = sandbox["tmp"] / "out" / "round-1.json"
    res = run(
        root / "paia-spec-adversarial-review" / "review.sh",
        sandbox,
        "docs/doc.md",
        "1",
        str(out),
    )
    assert res.returncode == 0, res.stderr
    record = json.loads((out.parent / "round-1.json.budget.json").read_text())
    assert record["besluit"] == "codex"


@pytest.mark.parametrize("root", ROOTS, ids=lambda p: p.parent.name)
def test_productiepad_roept_codex_usage_budget_aan(root, sandbox):
    """Zonder PAIA_CODEX_BUDGET_CMD draait de runner de echte check; een fout daarin
    zou anders stil op niet_meetbaar terugvallen en elke test groen laten."""
    del sandbox["env"]["PAIA_CODEX_BUDGET_CMD"]
    uv = sandbox["tmp"] / "bin" / "uv"
    uv.write_text(FAKE_UV.replace("#!/usr/bin/env python3", f"#!{sys.executable}", 1))
    uv.chmod(0o755)
    top = subprocess.run(
        ["git", "rev-parse", "--show-toplevel"],
        cwd=sandbox["repo"],
        capture_output=True,
        text=True,
        check=True,
    ).stdout.strip()
    out = sandbox["tmp"] / "out" / "round-1.json"
    res = run(
        root / "paia-spec-adversarial-review" / "review.sh",
        sandbox,
        "docs/doc.md",
        "1",
        str(out),
    )
    assert res.returncode == 0, res.stderr
    argv = json.loads((sandbox["capture"] / "uv-argv.json").read_text())
    assert argv[1:] == [
        "run",
        "--project",
        top,
        "python",
        f"{top}/scripts/codex_usage.py",
        "budget",
    ]
    record = json.loads((out.parent / "round-1.json.budget.json").read_text())
    assert record["besluit"] == "codex"


def test_review_sh_spiegel_is_byte_gelijk():
    a, b = (r / "paia-spec-adversarial-review" / "review.sh" for r in ROOTS)
    assert a.read_bytes() == b.read_bytes()
