"""Tests for `.lighthouse/scripts/roborev_gate.py gate` (#119): threshold, Codex budget and
the per-branch state files of the local roborev review.

Each test builds a temporary git repo with `refs/remotes/origin/main`, a `lighthouse.toml`, a
`.roborev.toml` and fake `roborev`/`codex` binaries on `PATH`. The budget is a lambda: the real
`codex_usage.py` never runs here.
"""

from __future__ import annotations

import importlib.util
import json
import os
import shutil
import subprocess
import sys
from pathlib import Path

import pytest

from tests.pack._testkit import GIT_ENV, INSTALL_ROOT

SCRIPT = INSTALL_ROOT / ".lighthouse" / "scripts" / "roborev_gate.py"
_SPEC = importlib.util.spec_from_file_location("roborev_gate", SCRIPT)
assert _SPEC and _SPEC.loader
rg = importlib.util.module_from_spec(_SPEC)
sys.modules[_SPEC.name] = rg
_SPEC.loader.exec_module(rg)

CODEX_OK = (0, '{"decision":"codex","reason":"ok"}')


def _git(repo: Path, *args: str) -> str:
    return subprocess.run(
        ["git", "-C", str(repo), *args],
        capture_output=True,
        text=True,
        check=True,
        env={**os.environ, **GIT_ENV},
    ).stdout.strip()


def _commit_lines(repo: Path, count: int, name: str = "x.go") -> None:
    (repo / name).write_text("".join(f"line {i}\n" for i in range(count)))
    _git(repo, "add", name)
    _git(repo, "commit", "-m", f"add {name}")


@pytest.fixture
def repo(tmp_path, monkeypatch) -> Path:
    """Repo on branch `feat` with 100 added code lines on top of origin/main."""
    root = tmp_path / "repo"
    root.mkdir()
    _git(root, "init", "-b", "main")
    (root / "lighthouse.toml").write_text('[project]\nlanguage = "nl"\n[review]\nroborev = "local"\n')
    (root / ".roborev.toml").write_text("")
    _git(root, "add", ".")
    _git(root, "commit", "-m", "base")
    _git(root, "update-ref", "refs/remotes/origin/main", "HEAD")
    _git(root, "checkout", "-b", "feat")
    _commit_lines(root, 100)
    bin_dir = tmp_path / "bin"
    bin_dir.mkdir()
    for tool in ("roborev", "codex"):
        (bin_dir / tool).write_text("#!/bin/sh\n")
        (bin_dir / tool).chmod(0o755)
    monkeypatch.setenv("PATH", f"{bin_dir}:/usr/bin:/bin")
    monkeypatch.delenv("LIGHTHOUSE_TOML", raising=False)
    return root


def _must_not_run() -> tuple[int, str]:
    raise AssertionError("budget must not be consulted")


def test_count_code_lines_follows_the_paia_size_rule():
    numstat = (
        "10\t2\tinternal/x.go\0" "5\t5\tdocs/a.go\0" "-\t-\tlogo.png\0" "7\t0\tREADME.md\0"
        "3\t1\t.github/workflows/ci.yml\0" "4\t0\tpack/x.toml\0"
    )
    assert rg.count_code_lines(numstat) == 16


def test_gate_is_off_without_the_setting(repo):
    (repo / "lighthouse.toml").write_text('[project]\nlanguage = "nl"\n')
    assert rg.gate(repo, _must_not_run) == {"verdict": "off"}


def test_gate_skips_without_roborev_toml(repo):
    (repo / ".roborev.toml").unlink()
    out = rg.gate(repo, _must_not_run)
    assert (out["verdict"], out["reason"]) == ("skip", "geen .roborev.toml")


def test_gate_skips_without_roborev_on_path(repo, monkeypatch, tmp_path):
    only_git = tmp_path / "only-git"
    only_git.mkdir()
    (only_git / "git").symlink_to(shutil.which("git"))
    monkeypatch.setenv("PATH", str(only_git))
    out = rg.gate(repo, _must_not_run)
    assert (out["verdict"], out["reason"]) == ("skip", "geen roborev of codex op PATH")


def test_gate_skips_below_the_threshold(repo):
    _git(repo, "reset", "--hard", "origin/main")
    _commit_lines(repo, 99)
    out = rg.gate(repo, _must_not_run)
    assert (out["verdict"], out["reason"]) == ("skip", "99 coderegels (drempel 100)")


def test_gate_runs_at_the_threshold(repo):
    out = rg.gate(repo, lambda: CODEX_OK)
    base = _git(repo, "rev-parse", "origin/main")
    head = _git(repo, "rev-parse", "HEAD")
    assert out["verdict"] == "run"
    assert out["range"] == f"{base}..{head}"
    assert out["lines"] == 100


@pytest.mark.parametrize(
    "budget,reason",
    [
        ((6, '{"decision":"fallback","reason":"5h 93%"}'), "Codex-budget: fallback - 5h 93%"),
        (
            (0, '{"decision":"not_measurable","reason":"not measurable"}'),
            "Codex-budget: not_measurable - not measurable",
        ),
        ((1, "Traceback"), "Codex-budget: onleesbaar - exit 1"),
        ((0, "not json"), "Codex-budget: onleesbaar - exit 0"),
    ],
)
def test_gate_skips_on_the_budget(repo, budget, reason):
    out = rg.gate(repo, lambda: budget)
    assert (out["verdict"], out["reason"]) == ("skip", reason)


def test_gate_records_its_skip_and_repeats_it(repo):
    first = rg.gate(repo, lambda: (6, '{"decision":"fallback","reason":"5h 93%"}'))
    written = json.loads(Path(first["state"]).read_text())
    base = _git(repo, "rev-parse", "origin/main")
    head = _git(repo, "rev-parse", "HEAD")
    assert written == {
        "range": f"{base}..{head}",
        "uitkomst": "skip",
        "reden": "Codex-budget: fallback - 5h 93%",
    }
    again = rg.gate(repo, lambda: CODEX_OK)
    assert (again["verdict"], again["reason"]) == ("skip", "Codex-budget: fallback - 5h 93%")


def test_gate_reads_a_done_record(repo):
    state = rg.state_dir(repo) / "feat.json"
    state.parent.mkdir(parents=True)
    state.write_text(json.dumps({"range": "a..b", "uitkomst": "done", "sha": "abc", "bevindingen": []}))
    out = rg.gate(repo, _must_not_run)
    assert (out["verdict"], out["sha"]) == ("done", "abc")


def test_gate_asks_to_classify_raw_output_without_a_record(repo):
    raw = rg.state_dir(repo) / "feat.md"
    raw.parent.mkdir(parents=True)
    raw.write_text("findings")
    out = rg.gate(repo, _must_not_run)
    assert out["verdict"] == "classify"
    assert out["raw"] == str(raw)


def test_state_files_of_a_slashed_branch(repo):
    _git(repo, "checkout", "-b", "fix/x")
    out = rg.gate(repo, lambda: CODEX_OK)
    assert out["state"] == str(rg.state_dir(repo) / "fix-x.json")
    assert out["raw"] == str(rg.state_dir(repo) / "fix-x.md")


def test_every_verdict_names_state_and_raw(repo):
    sd = rg.state_dir(repo)
    run = rg.gate(repo, lambda: CODEX_OK)
    skip = rg.gate(repo, lambda: (6, '{"decision":"fallback","reason":"r"}'))
    (sd / "feat.json").unlink()
    (sd / "feat.md").parent.mkdir(parents=True, exist_ok=True)
    (sd / "feat.md").write_text("x")
    classify = rg.gate(repo, _must_not_run)
    (sd / "feat.json").write_text(json.dumps({"range": "a..b", "uitkomst": "done", "sha": "s"}))
    done = rg.gate(repo, _must_not_run)
    for out in (run, skip, classify, done):
        assert out["state"] == str(sd / "feat.json")
        assert out["raw"] == str(sd / "feat.md")
        assert Path(out["state"]).is_absolute()


def test_state_dir_is_per_worktree(repo, tmp_path):
    other = tmp_path / "wt"
    _git(repo, "worktree", "add", str(other), "-b", "other")
    assert rg.state_dir(other) != rg.state_dir(repo)
    assert rg.state_dir(other).is_absolute()
    assert rg.state_dir(repo) == repo / ".git" / "lighthouse" / "roborev"


def test_cli_prints_the_verdict_as_json(repo):
    (repo / ".roborev.toml").unlink()  # skips before the budget: codex_usage never runs
    result = subprocess.run(
        [sys.executable, str(SCRIPT), "gate"],
        cwd=repo,
        capture_output=True,
        text=True,
        env={"PATH": os.environ["PATH"], **GIT_ENV},
    )
    assert result.returncode == 0
    out = json.loads(result.stdout)
    assert (out["verdict"], out["reason"]) == ("skip", "geen .roborev.toml")


def test_run_verdict_raw_path_is_writable_at_once(repo):
    out = rg.gate(repo, lambda: CODEX_OK)
    assert out["verdict"] == "run"
    with open(out["raw"], "w") as handle:
        handle.write("review")


def test_quoted_non_ascii_paths_are_not_counted(repo):
    (repo / "docs").mkdir()
    (repo / "docs" / "café.md").write_text("x\n" * 50)
    (repo / "café.md").write_text("x\n" * 50)
    _git(repo, "add", ".")
    _git(repo, "commit", "-m", "prose with non-ascii names")
    out = rg.gate(repo, lambda: CODEX_OK)
    assert (out["verdict"], out["lines"]) == ("run", 100)


def test_cli_cannot_check_outside_a_repo(tmp_path):
    result = subprocess.run(
        [sys.executable, str(SCRIPT), "gate"],
        cwd=tmp_path,
        capture_output=True,
        text=True,
        env={"PATH": os.environ["PATH"], "GIT_CEILING_DIRECTORIES": str(tmp_path.parent)},
    )
    assert result.returncode == 3
    assert result.stderr.startswith("COULD NOT CHECK:")


# --- classify, fixed_findings, record, body_line (#119 task 3) ---

FIXTURES = Path(__file__).parent / "fixtures" / "roborev"


def FIX(name: str) -> str:
    return (FIXTURES / name).read_text()


def _triples(got: dict) -> list[tuple]:
    return [(f["path"], f["line"], f["severity"]) for f in got["findings"]]


def test_classify_reads_the_measured_local_output():
    got = rg.classify(FIX("review-local.md"))
    assert got["verdict"] == "done" and got["sha"] == "803c1a6"
    assert _triples(got) == [
        ("pack/core/scripts/roborev_gate.py", 184, "medium"),
        ("pack/core/scripts/roborev_gate.py", 169, "medium"),
    ]
    assert got["findings"][0]["text"].startswith("Bij de eerste uitkomst `run`")


def test_classify_reads_a_local_pass():
    got = rg.classify(FIX("review-local-passed.md"))
    assert (got["verdict"], got["sha"], got["findings"]) == ("done", "HEAD", [])


def test_classify_reads_sha_and_findings_of_a_complete_review():
    got = rg.classify(FIX("review-complete.md"))
    assert got["verdict"] == "done" and got["sha"] == "bf1eb24"
    assert _triples(got) == [
        ("scripts/forge_drager.py", 93, "medium"),
        ("scripts/forge_agent.sh", 612, "medium"),
    ]


def test_classify_passed_is_done_without_findings():
    got = rg.classify(FIX("review-passed.md"))
    assert (got["verdict"], got["sha"], got["findings"]) == ("done", "bf1eb24", [])


def test_classify_combined_review_is_done():
    got = rg.classify("## roborev: Combined Review (`abc1234`)\n\n### High\n\n- a.py:3: bad\n")
    assert (got["verdict"], got["kind"]) == ("done", "Combined Review")
    assert _triples(got) == [("a.py", 3, "high")]


def test_classify_bullet_without_location_has_no_path():
    got = rg.classify("## roborev: Review Complete (`abc1234`)\n\n### Low\n\n- no location here\n")
    assert _triples(got) == [(None, None, "low")]


def test_classify_local_finding_without_location_has_no_path():
    raw = "## roborev: Review Complete (`abc`)\n\n## Findings\n\n### 1. High\n\n**Problem:** vague\n"
    assert _triples(rg.classify(raw)) == [(None, None, "high")]


@pytest.mark.parametrize(
    "body,expected",
    [
        ("### Major\n\n- a.py:3: bad\n", ("a.py", 3, "major")),
        (
            "## Findings\n\n### 1. Warning\n\n**Location:** b.py:7\n\n**Problem:** bad\n",
            ("b.py", 7, "warning"),
        ),
    ],
)
def test_classify_keeps_a_finding_under_an_unknown_severity(body, expected):
    got = rg.classify(f"## roborev: Review Complete (`abc1234`)\n\n{body}")
    assert got["verdict"] == "done" and _triples(got) == [expected]


@pytest.mark.parametrize("kind", ["Review Complete", "Combined Review"])
def test_classify_complete_review_without_readable_findings_is_a_skip(kind):
    got = rg.classify(f"## roborev: {kind} (`abc1234`)\n\n**Verdict:** 2 findings.\n\nSomething odd.\n")
    assert got == {"verdict": "skip", "reason": "review faalde: bevindingen onleesbaar"}


@pytest.mark.parametrize("kind", ["Review Failed", "Review Skipped"])
def test_classify_failed_heads_skip(kind):
    got = rg.classify(FIX("review-failed.md").replace("Review Failed", kind))
    assert got == {"verdict": "skip", "reason": f"review faalde: {kind}"}


def test_classify_without_heading_names_the_first_line():
    got = rg.classify("\nError: codex: 401\nmore\n")
    assert got == {"verdict": "skip", "reason": "review faalde: Error: codex: 401"}


def test_classify_cuts_a_long_first_line_at_200_chars():
    got = rg.classify("x" * 500)
    assert got["reason"] == "review faalde: " + "x" * 200


@pytest.mark.parametrize("raw", ["", "  \n\n"])
def test_classify_empty_output(raw):
    assert rg.classify(raw) == {"verdict": "skip", "reason": "review faalde: geen output"}


def _fix_commit(repo: Path, name: str, subject: str) -> str:
    _commit_lines(repo, 1, name)
    _git(repo, "commit", "--amend", "-m", subject)
    return _git(repo, "rev-parse", "HEAD")


def _write_raw(repo: Path, text: str) -> Path:
    raw = rg.state_dir(repo) / "feat.md"
    raw.parent.mkdir(parents=True, exist_ok=True)
    raw.write_text(text)
    return raw


def test_resume_after_abort_routes_to_classify_and_recovers_fixes(repo):
    _write_raw(repo, FIX("review-complete.md"))
    c1 = _fix_commit(repo, "a.py", "fix: a\n\nroborev: scripts/forge_drager.py:93 (medium)")
    c2 = _fix_commit(repo, "b.py", "fix: b\n\nroborev: scripts/forge_agent.sh:612 (medium)")
    _fix_commit(repo, "c.py", "fix: unrelated")
    assert rg.gate(repo, _must_not_run)["verdict"] == "classify"
    assert [(f["path"], f["line"], f["severity"], f["commit"]) for f in rg.fixed_findings(repo)] == [
        ("scripts/forge_drager.py", 93, "medium", c1),
        ("scripts/forge_agent.sh", 612, "medium", c2),
    ]


def _decisions(*rows: tuple) -> list[dict]:
    return [
        {"path": p, "line": ln, "severity": s, "uitkomst": o, "commit": c, "reden": r}
        for p, ln, s, o, c, r in rows
    ]


COMPLETE_DECISIONS = (
    ("scripts/forge_drager.py", 93, "medium", "fixed", "abc1234", None),
    ("scripts/forge_agent.sh", 612, "medium", "refuted", None, "klopt niet"),
)


def test_record_refuses_a_missing_decision(repo):
    _write_raw(repo, FIX("review-complete.md"))
    with pytest.raises(ValueError):
        rg.record(repo, sha="bf1eb24", decisions=_decisions(COMPLETE_DECISIONS[0]))


def test_record_refuses_a_double_decision(repo):
    _write_raw(repo, FIX("review-complete.md"))
    with pytest.raises(ValueError):
        rg.record(repo, sha="bf1eb24", decisions=_decisions(*COMPLETE_DECISIONS, COMPLETE_DECISIONS[0]))


@pytest.mark.parametrize(
    "row",
    [
        ("scripts/forge_drager.py", 93, "medium", "fixed", None, None),
        ("scripts/forge_drager.py", 93, "medium", "fixed", "abc", "why"),
        ("scripts/forge_drager.py", 93, "medium", "refuted", None, None),
        ("scripts/forge_drager.py", 93, "medium", "left", "abc", "why"),
        ("scripts/forge_drager.py", 93, "medium", "maybe", None, "why"),
    ],
)
def test_record_refuses_an_inconsistent_decision(repo, row):
    with pytest.raises(ValueError):
        rg.record(repo, sha="s", decisions=_decisions(row))


def test_record_done_then_gate_says_done(repo):
    _write_raw(repo, FIX("review-complete.md"))
    out = rg.record(repo, sha="bf1eb24", decisions=_decisions(*COMPLETE_DECISIONS))
    assert out == rg.state_dir(repo) / "feat.json"
    written = json.loads(out.read_text())
    base, head = _git(repo, "rev-parse", "origin/main"), _git(repo, "rev-parse", "HEAD")
    assert written["range"] == f"{base}..{head}"
    assert (written["uitkomst"], written["sha"]) == ("done", "bf1eb24")
    assert written["bevindingen"] == _decisions(*COMPLETE_DECISIONS)
    gated = rg.gate(repo, _must_not_run)
    assert (gated["verdict"], gated["sha"]) == ("done", "bf1eb24")


def test_record_without_raw_accepts_decisions_unchecked(repo):
    out = rg.record(repo, sha="s", decisions=_decisions(COMPLETE_DECISIONS[0]))
    assert json.loads(out.read_text())["uitkomst"] == "done"


def test_record_skip_then_gate_says_skip(repo):
    rg.record(repo, skip="review faalde: timeout na 9 min")
    gated = rg.gate(repo, _must_not_run)
    assert (gated["verdict"], gated["reason"]) == ("skip", "review faalde: timeout na 9 min")


def test_record_honours_out(repo, tmp_path):
    target = tmp_path / "elsewhere" / "r.json"
    assert rg.record(repo, skip="x", out=target) == target
    assert json.loads(target.read_text())["reden"] == "x"


def test_record_needs_skip_or_sha(repo):
    with pytest.raises(ValueError):
        rg.record(repo)


@pytest.mark.parametrize(
    "state,line",
    [
        (None, "Roborev overgeslagen: geen reviewuitkomst vastgelegd"),
        ({"uitkomst": "done", "sha": "bf1eb24"}, "<!-- roborev-lokaal: bf1eb24 -->"),
        ({"uitkomst": "skip", "reden": "geen .roborev.toml"}, "Roborev overgeslagen: geen .roborev.toml"),
    ],
)
def test_body_line(repo, state, line):
    if state is not None:
        rg.write_state(rg.state_dir(repo) / "feat.json", {"range": "a..b", **state})
    assert rg.body_line(repo) == line


def test_body_line_is_empty_when_off(repo):
    (repo / "lighthouse.toml").write_text('[project]\nlanguage = "nl"\n')
    assert rg.body_line(repo) == ""


def _cli(repo: Path, *args: str) -> subprocess.CompletedProcess:
    return subprocess.run(
        [sys.executable, str(SCRIPT), *args],
        cwd=repo,
        capture_output=True,
        text=True,
        env={"PATH": os.environ["PATH"], **GIT_ENV},
    )


def test_cli_classify_skip_also_records(repo):
    raw = _write_raw(repo, "Error: codex: 401\n")
    result = _cli(repo, "classify")
    assert result.returncode == 0
    assert json.loads(result.stdout) == {"verdict": "skip", "reason": "review faalde: Error: codex: 401"}
    assert json.loads((raw.with_suffix(".json")).read_text())["reden"] == "review faalde: Error: codex: 401"


def test_cli_classify_takes_a_path_and_prints_findings(repo):
    result = _cli(repo, "classify", str(FIXTURES / "review-complete.md"))
    assert json.loads(result.stdout)["sha"] == "bf1eb24"
    assert not (rg.state_dir(repo) / "feat.json").exists()


def test_cli_record_done_and_body_line(repo, tmp_path):
    _write_raw(repo, FIX("review-complete.md"))
    decisions = tmp_path / "d.json"
    decisions.write_text(json.dumps(_decisions(*COMPLETE_DECISIONS)))
    assert _cli(repo, "record", "--done", "bf1eb24", "--decisions", str(decisions)).returncode == 0
    assert _cli(repo, "body-line").stdout.strip() == "<!-- roborev-lokaal: bf1eb24 -->"


def test_cli_record_refusal_exits_2(repo, tmp_path):
    _write_raw(repo, FIX("review-complete.md"))
    decisions = tmp_path / "d.json"
    decisions.write_text("[]")
    result = _cli(repo, "record", "--done", "s", "--decisions", str(decisions))
    assert result.returncode == 2 and result.stderr.strip()


def test_cli_fixed_prints_json(repo):
    c1 = _fix_commit(repo, "a.py", "fix: a\n\nroborev: p.py:1 (high)")
    out = json.loads(_cli(repo, "fixed").stdout)
    assert out == [{"path": "p.py", "line": 1, "severity": "high", "commit": c1}]
