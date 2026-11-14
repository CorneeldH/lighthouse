"""Contract of the adversarial review runner.

`lighthouse review-run` is the only Codex runner; the plan review and the verification rounds
are `--kind plan` and `--verify`, with their own prompt and schema from the skill directory.
These tests run the real binary against the skill directories installed the way `init`
installs them, and a fake `codex` on PATH that records the prompt and the argv.
So they check what the runner promises without network or Codex account:

- without new flags and without a sidecar the prompt is byte-identical to the original
  algorithm (`legacy_prompt` below is that algorithm, literally);
- with a decisions sidecar only the prior block changes;
- `--kind plan` sends the plan prompt and the plan schema, and `--context` lands in
  `{{CONTEXT_PATHS}}`;
- the budget check runs in the binary, before Codex, with no seam in the way.

From paia's `test_adversarial_review_sh.py`.
"""

from __future__ import annotations

import json
import os
import pathlib
import shutil
import subprocess
import sys

import pytest

from tests.pack._testkit import CORE

SKILLS = CORE / "skills"
NAMES = ("review-spec", "review-plan")


def install_skills(root: pathlib.Path) -> None:
    """Install both skills under `root/.claude/skills` and `root/.agents/skills`, with the
    shared files next to each variant's SKILL.md, as `init` does."""
    for variant, base in (("claude", ".claude/skills"), ("agents", ".agents/skills")):
        for name in NAMES:
            dest = root / base / f"lighthouse-{name}"
            dest.mkdir(parents=True)
            for f in (SKILLS / name).iterdir():
                if f.is_file():
                    shutil.copy2(f, dest / f.name)
            shutil.copy2(SKILLS / name / variant / "SKILL.md", dest / "SKILL.md")


@pytest.fixture(scope="module")
def installed(tmp_path_factory) -> pathlib.Path:
    root = tmp_path_factory.mktemp("install")
    install_skills(root)
    return root


@pytest.fixture(params=[".claude/skills", ".agents/skills"], ids=["claude", "agents"])
def root(request, installed) -> pathlib.Path:
    return installed / request.param

FAKE_CODEX = """#!/usr/bin/env python3
import json, os, pathlib, sys, time
args = sys.argv[1:]
cap = pathlib.Path(os.environ["FAKE_CODEX_CAPTURE"])
if args[:1] == ["app-server"]:
    pct = int(os.environ["FAKE_CODEX_SESSION_PCT"])
    reset = int(time.time()) + 3600
    window = {"usedPercent": pct, "windowDurationMins": 300, "resetsAt": reset}
    week = {"usedPercent": 1, "windowDurationMins": 10080, "resetsAt": reset}
    for line in sys.stdin:
        msg = json.loads(line)
        if msg.get("id") == 1:
            print(json.dumps({"id": 1, "result": {}}), flush=True)
        elif msg.get("id") == 2:
            print(json.dumps({"id": 2, "result": {"rateLimits": {"primary": window, "secondary": week}}}), flush=True)
            break
    sys.exit(0)
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
    print(json.dumps({"decision": "codex", "reason": "below the thresholds"}))
elif mode == "fallback":
    print(json.dumps({"decision": "fallback", "reason": "5h 92% \u2265 90%"}))
    sys.exit(6)
elif mode == "fallback-broken":
    print("no json")
    sys.exit(6)
elif mode == "crash":
    sys.stderr.write("Traceback (most recent call last):\\n  boem\\n")
    sys.exit(1)
elif mode == "list":
    print("[]")
elif mode == "list6":
    print("[]")
    sys.exit(6)
"""

# The fields of `codex_usage.py budget`, in its JSON order.
NINE = [
    "decision",
    "reason",
    "window",
    "reset_at",
    "session_pct",
    "week_pct",
    "source",
    "lower_bound",
    "thresholds",
]

# (kind, verify): the four rounds the old review.sh and verify.sh scripts covered.
ROUNDS = [("spec", False), ("plan", False), ("plan", True), ("spec", True)]


def legacy_prompt(
    template: pathlib.Path, doc: str, repo: str, round_no: str, prior: str
) -> str:
    """The substitution of review.sh before the sidecar existed, line for line.

    `print(body)` plus `"$(cat ...)"` in bash: the trailing newline drops.
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
def sandbox(tmp_path: pathlib.Path, lighthouse_bin: pathlib.Path) -> dict:
    """A throwaway git repo with one document, a fake codex and a clean HOME."""
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
    base = {k: v for k, v in os.environ.items() if k not in ("LIGHTHOUSE_REPO_ROOT", "LIGHTHOUSE_TOML")}
    env = {
        **base,
        "PATH": f"{bin_dir}{os.pathsep}{lighthouse_bin.parent}{os.pathsep}{os.environ['PATH']}",
        "HOME": str(home),
        "CODEX_SPECREVIEW_HOME": str(tmp_path / "ch"),
        "TMPDIR": str(tmp_path),
        "FAKE_CODEX_CAPTURE": str(tmp_path / "capture"),
        "LIGHTHOUSE_CODEX_BUDGET_CMD": str(budget),
        "LIGHTHOUSE_REPO_ROOT": str(repo.resolve()),
    }
    return {"repo": repo.resolve(), "env": env, "capture": tmp_path / "capture", "tmp": tmp_path}


def run(
    skill: pathlib.Path,
    sandbox: dict,
    *args: str,
    kind: str = "spec",
    verify: bool = False,
    skill_dir: str | None = None,
) -> subprocess.CompletedProcess:
    """`lighthouse review-run` on the skill directory `skill` (or the repo-relative `skill_dir`)."""
    cmd = [
        "lighthouse",
        "review-run",
        "--kind",
        kind,
        "--skill-dir",
        skill_dir or str(skill),
        *(["--verify"] if verify else []),
        *args,
    ]
    return subprocess.run(
        cmd,
        cwd=sandbox["repo"],
        env=sandbox["env"],
        capture_output=True,
        text=True,
        check=False,
    )


def captured(sandbox: dict) -> tuple[str, list[str]]:
    cap = sandbox["capture"]
    return (cap / "prompt.txt").read_text(), json.loads((cap / "argv.json").read_text())


@pytest.mark.parametrize("prior", ["none", "PRIOR"])
def test_spec_review_prompt_is_byte_identical_without_new_flags(root, prior, sandbox):
    skill = root / "lighthouse-review-spec"
    out = sandbox["tmp"] / "out" / "round-2.json"
    prior_arg = (
        "none" if prior == "none" else str(sandbox["tmp"] / "out" / "round-1.json")
    )
    result = run(skill, sandbox, "docs/doc.md", "2", str(out), prior_arg)
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


def test_sidecar_changes_only_the_prior_block(root, sandbox):
    skill = root / "lighthouse-review-spec"
    outdir = sandbox["tmp"] / "out"
    outdir.mkdir()
    prior = outdir / "round-1.json"
    sidecar = outdir / "round-1.decisions.json"
    sidecar.write_text('[{"finding_id": "r1-f1", "decision": "reject", "reason": "x"}]')
    result = run(
        skill,
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
    # Everything outside the prior block is equal: cut the block from both and compare.
    before, _, rest = legacy.partition("Previous review:")
    after_legacy = rest.split("and why.", 1)[1]
    new_before, _, new_rest = prompt.partition("Previous review:")
    after_new = new_rest.split("wrong, and why.", 1)[1]
    assert (before, after_legacy) == (new_before, after_new)


def test_context_on_a_template_without_the_placeholder_fails(root, sandbox):
    """The spec prompt has no {{CONTEXT_PATHS}}: ignoring it silently would hide the
    spec(s) the operator passed on purpose."""
    skill = root / "lighthouse-review-spec"
    out = sandbox["tmp"] / "out" / "round-1.json"
    result = run(
        skill,
        sandbox,
        "docs/doc.md",
        "1",
        str(out),
        "--context",
        "docs/spec.md",
    )
    assert result.returncode != 0
    assert "{{CONTEXT_PATHS}}" in result.stderr


def test_plan_kind_sends_plan_prompt_schema_and_context(root, sandbox):
    plan_skill = root / "lighthouse-review-plan"
    out = sandbox["tmp"] / "out" / "round-1.json"
    result = run(
        plan_skill,
        sandbox,
        "docs/doc.md",
        "1",
        str(out),
        "none",
        "--context",
        "docs/spec.md",
        kind="plan",
    )
    assert result.returncode == 0, result.stderr
    prompt, argv = captured(sandbox)
    assert "Plan: docs/doc.md" in prompt
    assert "- docs/spec.md" in prompt
    assert "{{" not in prompt
    schema = pathlib.Path(argv[argv.index("--output-schema") + 1]).resolve()
    assert schema == (plan_skill / "findings.schema.json").resolve()


def test_agents_skill_dir_reads_its_own_templates(sandbox):
    """`--skill-dir` is repo-relative: the agents variant sends the prompt from its own directory,
    and changes to that directory change the prompt."""
    install_skills(sandbox["repo"])
    skill_dir = ".agents/skills/lighthouse-review-plan"
    template = sandbox["repo"] / skill_dir / "reviewer-prompt.md"
    template.write_text(template.read_text().replace("Plan: ", "Agents plan: "))
    out = sandbox["tmp"] / "out" / "round-1.json"
    result = run(
        pathlib.Path(skill_dir),
        sandbox,
        "docs/doc.md",
        "1",
        str(out),
        kind="plan",
        skill_dir=skill_dir,
    )
    assert result.returncode == 0, result.stderr
    prompt, argv = captured(sandbox)
    assert "Agents plan: docs/doc.md" in prompt
    schema = argv[argv.index("--output-schema") + 1]
    assert pathlib.Path(schema) == sandbox["repo"] / skill_dir / "findings.schema.json"


def test_doc_path_with_space_reaches_codex_verbatim(root, sandbox):
    (sandbox["repo"] / "docs" / "my doc.md").write_text("# doc\n")
    out = sandbox["tmp"] / "out" / "round-1.json"
    result = run(root / "lighthouse-review-spec", sandbox, "docs/my doc.md", "1", str(out))
    assert result.returncode == 0, result.stderr
    prompt, _ = captured(sandbox)
    assert "docs/my doc.md" in prompt
    assert prompt == legacy_prompt(
        root / "lighthouse-review-spec" / "reviewer-prompt.md",
        "docs/my doc.md",
        str(sandbox["repo"]),
        "1",
        "none",
    )


def test_missing_context_path_is_a_usage_error(root, sandbox):
    plan_skill = root / "lighthouse-review-plan"
    out = sandbox["tmp"] / "out" / "round-1.json"
    result = run(
        plan_skill,
        sandbox,
        "docs/doc.md",
        "1",
        str(out),
        "--context",
        "docs/does-not-exist.md",
        kind="plan",
    )
    assert result.returncode == 2
    assert "context path not found" in result.stderr


EXECUTABILITY_CATS = [
    "spec_mismatch",
    "dependency_order",
    "repo_falsehood",
    "missing_artifact",
    "verification_gap",
    "unverified_external_assumption",
    "likely_runtime_failure",
]


def test_plan_schema_requires_one_of_seven_categories(root):
    schema = json.loads(
        (root / "lighthouse-review-plan" / "findings.schema.json").read_text()
    )
    item = schema["properties"]["findings"]["items"]
    assert {"cat", "plan_section", "failure_scenario"} <= set(item["required"])
    assert "spec_section" not in item["properties"]
    assert item["properties"]["cat"]["enum"] == EXECUTABILITY_CATS


def test_plan_prompt_asks_executability_not_improvement(root):
    text = (root / "lighthouse-review-plan" / "reviewer-prompt.md").read_text()
    body = text.split("---BEGIN PROMPT---", 1)[1].split("---END PROMPT---", 1)[0]
    for cat in EXECUTABILITY_CATS:
        assert f"`{cat}`" in body
    for gone in ("open_vraag", "operationeel", "tegenspraak"):
        assert gone not in body
    assert "prove that executing this plan will fail" in body
    assert "no concrete failure scenario, it is not a BLOCKER or MAJOR" in body


def test_plan_verify_sends_verifier_prompt_and_schema(root, sandbox):
    plan_skill = root / "lighthouse-review-plan"
    outdir = sandbox["tmp"] / "out"
    outdir.mkdir()
    (outdir / "round-1.json").write_text("{}")
    diff = outdir / "round-2.fixes.diff"
    diff.write_text("--- a\n+++ b\n")
    result = run(
        plan_skill,
        sandbox,
        "docs/doc.md",
        "2",
        str(outdir / "round-2.json"),
        str(outdir / "round-1.json"),
        "--context",
        "docs/spec.md",
        "--context",
        str(diff),
        kind="plan",
        verify=True,
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


def test_plan_verification_schema_is_scoped(root):
    schema = json.loads(
        (root / "lighthouse-review-plan" / "verification.schema.json").read_text()
    )
    assert "verified" in schema["required"]
    item = schema["properties"]["findings"]["items"]
    assert {"verifies", "failure_scenario", "cat"} <= set(item["required"])
    assert item["properties"]["severity"]["enum"] == ["BLOCKER", "MAJOR"]
    assert item["properties"]["cat"]["enum"] == [*EXECUTABILITY_CATS, "fix_incomplete"]


def test_spec_blocker_needs_a_failure_scenario(root):
    skill = root / "lighthouse-review-spec"
    item = json.loads((skill / "findings.schema.json").read_text())["properties"][
        "findings"
    ]["items"]
    assert "failure_scenario" in item["required"]
    assert "cat" not in item["properties"]
    body = (skill / "reviewer-prompt.md").read_text().split("---BEGIN PROMPT---", 1)[1]
    assert "no concrete failure scenario, it is not a BLOCKER or MAJOR" in body
    assert "{{CONTEXT_PATHS}}" not in body


def test_spec_verify_sends_verifier_prompt_and_schema(root, sandbox):
    skill = root / "lighthouse-review-spec"
    outdir = sandbox["tmp"] / "out"
    outdir.mkdir()
    (outdir / "round-1.json").write_text("{}")
    diff = outdir / "round-2.fixes.diff"
    diff.write_text("--- a\n+++ b\n")
    result = run(
        skill,
        sandbox,
        "docs/doc.md",
        "2",
        str(outdir / "round-2.json"),
        str(outdir / "round-1.json"),
        "--context",
        str(diff),
        verify=True,
    )
    assert result.returncode == 0, result.stderr
    prompt, argv = captured(sandbox)
    assert "Spec: docs/doc.md" in prompt
    assert f"- {diff}" in prompt
    assert "Do not search for new problems" in prompt
    assert "Rejected findings are out of scope" in prompt
    schema = pathlib.Path(argv[argv.index("--output-schema") + 1]).resolve()
    assert schema == (skill / "verification.schema.json").resolve()


def test_runner_is_allowlisted():
    """The allowlist rule comes with the package (decision 14), not from a hand edit."""
    allow = json.loads((CORE / "permissions.json").read_text())["allow"]
    assert "Bash(lighthouse review-run:*)" in allow


@pytest.mark.parametrize(("kind", "verify"), ROUNDS)
def test_budget_fallback_stops_before_codex(root, kind, verify, sandbox):
    sandbox["env"]["FAKE_BUDGET_MODE"] = "fallback"
    out = sandbox["tmp"] / "new" / "deep" / "round-1.json"  # the directory does not exist yet
    res = run(
        root / f"lighthouse-review-{kind}",
        sandbox,
        "docs/doc.md",
        "1",
        str(out),
        kind=kind,
        verify=verify,
    )
    assert res.returncode == 6
    assert "CODEX_BUDGET: fallback \u2014 5h 92% \u2265 90%" in res.stderr
    assert not (sandbox["capture"] / "argv.json").exists()
    record = json.loads((out.parent / "round-1.json.budget.json").read_text())
    assert record["decision"] == "fallback"


@pytest.mark.parametrize("mode", ["fallback-broken", "crash", "list", "list6"])
def test_broken_budget_check_is_not_measurable_and_codex_runs(root, mode, sandbox):
    sandbox["env"]["FAKE_BUDGET_MODE"] = mode
    out = sandbox["tmp"] / "out" / "round-1.json"
    res = run(root / "lighthouse-review-spec", sandbox, "docs/doc.md", "1", str(out))
    assert res.returncode == 0, res.stderr
    assert (sandbox["capture"] / "argv.json").exists()
    data = json.loads((out.parent / "round-1.json.budget.json").read_text())
    assert list(data) == NINE
    assert data["decision"] == "not_measurable"
    stub_exit = {"fallback-broken": 6, "crash": 1, "list": 0, "list6": 6}[mode]
    assert data["reason"] == f"budget check failed: exit {stub_exit}"
    assert all(data[k] is None for k in NINE[2:])


def test_budget_codex_writes_record_and_runs(root, sandbox):
    out = sandbox["tmp"] / "out" / "round-1.json"
    res = run(root / "lighthouse-review-spec", sandbox, "docs/doc.md", "1", str(out))
    assert res.returncode == 0, res.stderr
    record = json.loads((out.parent / "round-1.json.budget.json").read_text())
    assert record["decision"] == "codex"


def _seam_free(sandbox: dict, repo: pathlib.Path, session_pct: int) -> None:
    """No budget seam: the binary reads the fake codex's app-server, with the thresholds of
    the `lighthouse.toml` in `repo`."""
    sandbox["env"].pop("LIGHTHOUSE_CODEX_BUDGET_CMD", None)
    sandbox["env"]["LIGHTHOUSE_REPO_ROOT"] = str(repo)
    sandbox["env"]["FAKE_CODEX_SESSION_PCT"] = str(session_pct)
    (repo / "lighthouse.toml").write_text(
        '[project]\nlanguage = "en"\n\n[codex_usage]\nreview_pct_session = 40\n'
    )


def test_production_path_reads_the_budget_without_a_seam(root, sandbox):
    """Without LIGHTHOUSE_CODEX_BUDGET_CMD the binary reads the Codex windows itself and uses
    the thresholds of lighthouse.toml: a reading above them stops the round before `codex exec`."""
    _seam_free(sandbox, sandbox["repo"], 50)
    out = sandbox["tmp"] / "out" / "round-1.json"
    res = run(root / "lighthouse-review-spec", sandbox, "docs/doc.md", "1", str(out))
    assert res.returncode == 6, res.stderr
    assert "CODEX_BUDGET: fallback \u2014 5h 50% \u2265 40%" in res.stderr
    assert not (sandbox["capture"] / "argv.json").exists()  # codex exec was not called
    record = json.loads((out.parent / "round-1.json.budget.json").read_text())
    assert record["decision"] == "fallback"
    assert record["thresholds"] == {"session": 40, "week": 95}


def test_production_path_below_the_threshold_runs_codex(root, sandbox):
    _seam_free(sandbox, sandbox["repo"], 10)
    out = sandbox["tmp"] / "out" / "round-1.json"
    res = run(root / "lighthouse-review-spec", sandbox, "docs/doc.md", "1", str(out))
    assert res.returncode == 0, res.stderr
    record = json.loads((out.parent / "round-1.json.budget.json").read_text())
    assert record["decision"] == "codex"
    assert record["thresholds"] == {"session": 40, "week": 95}


def test_repo_root_from_lighthouse_repo_root(root, sandbox):
    """`LIGHTHOUSE_REPO_ROOT` wins over the git root of the cwd (inventory section 3): the
    thresholds come from its lighthouse.toml and codex gets it as `--cd`."""
    other = sandbox["tmp"] / "other"
    (other / "docs").mkdir(parents=True)
    (other / "docs" / "doc.md").write_text("# other\n")
    _seam_free(sandbox, other, 50)
    out = sandbox["tmp"] / "out" / "round-1.json"
    res = run(root / "lighthouse-review-spec", sandbox, "docs/doc.md", "1", str(out))
    assert res.returncode == 6, res.stderr
    record = json.loads((out.parent / "round-1.json.budget.json").read_text())
    assert record["thresholds"] == {"session": 40, "week": 95}
    # The repo of the cwd has no lighthouse.toml: the other thresholds are not the default.
    _seam_free(sandbox, other, 10)
    res = run(root / "lighthouse-review-spec", sandbox, "docs/doc.md", "1", str(out))
    assert res.returncode == 0, res.stderr
    _, codex_argv = captured(sandbox)
    assert codex_argv[codex_argv.index("--cd") + 1] == str(other)


def test_runner_messages_are_english(root, sandbox):
    skill = root / "lighthouse-review-spec"
    res = run(skill, sandbox)
    assert res.returncode == 2
    assert res.stderr.startswith("usage: lighthouse review-run")
    res = run(skill, sandbox, "--bogus")
    assert res.returncode == 2
    assert "unknown flag: --bogus" in res.stderr


# --- the paired round 1 without Python ---------------------------------------------------------


def _git_env(env: dict) -> dict:
    return {
        **env,
        "GIT_AUTHOR_NAME": "t",
        "GIT_AUTHOR_EMAIL": "t@example.com",
        "GIT_COMMITTER_NAME": "t",
        "GIT_COMMITTER_EMAIL": "t@example.com",
    }


def test_paired_round_one_without_python3(sandbox, lighthouse_bin, tmp_path):
    """Round 1 of the spec review, start to merge, on a PATH with git, sh, cat, mkdir, the
    binary and a fake codex: no python3 anywhere on it (#282)."""
    repo, env = sandbox["repo"], _git_env(sandbox["env"])
    install_skills(repo)
    for part in (".claude", ".agents"):  # the same skills `init` installs, committed and clean
        assert (repo / part / "skills" / "lighthouse-review-spec" / "findings.schema.json").is_file()
    subprocess.run(["git", "add", "-A"], cwd=repo, env=env, check=True)
    subprocess.run(["git", "commit", "-q", "-m", "docs: spec"], cwd=repo, env=env, check=True)

    only = tmp_path / "only-bin"
    only.mkdir()
    for tool in ("git", "sh", "cat", "mkdir"):
        target = shutil.which(tool)
        assert target, tool
        (only / tool).symlink_to(target)
    (only / "lighthouse").symlink_to(lighthouse_bin)
    (only / "codex").symlink_to(pathlib.Path(env["PATH"].split(os.pathsep)[0]) / "codex")
    env["PATH"] = str(only)
    # No budget stub (a Python script): the binary reads the fake codex's app-server itself.
    env.pop("LIGHTHOUSE_CODEX_BUDGET_CMD", None)
    env["FAKE_CODEX_SESSION_PCT"] = "5"
    assert shutil.which("python3", path=env["PATH"]) is None

    def lh(*args: str) -> str:
        res = subprocess.run(
            ["lighthouse", *args], cwd=repo, env=env, capture_output=True, text=True, check=False
        )
        assert res.returncode == 0, (args, res.stdout, res.stderr)
        return res.stdout

    attempt = pathlib.Path(lh("plan-review-record", "start", "--kind", "spec", "docs/spec.md").strip())
    for name in ("round-1.codex.json", "round-1.fallback.json"):
        lh("review-run", "--kind", "spec", "docs/spec.md", "1", str(attempt / name), "none")
        assert (attempt / name).is_file(), name
    lh("review-pair", "blind", str(attempt))
    (attempt / "round-1.blind-decisions.json").write_text("[]")
    lh("review-pair", "merge", str(attempt))
    assert json.loads((attempt / "round-1.json").read_text())["findings"] == []
