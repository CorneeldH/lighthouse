"""Tests for scripts/plan_review_record.py — the measurement record of the plan review (#972).

Spec: docs/developer/planning/specs/2026-09-25-plan-adversarial-review-design.md,
section 5. Tested: the attempt directory, the content of record.json, the host check
(simulated) and a write error (simulated): only the staging directory stays, a
loud message, exit 4.

In lighthouse: the script runs from the installed layout (`install_core`), the
world sets `LIGHTHOUSE_REPO_ROOT`, `LIGHTHOUSE_DATA` and a `lighthouse.toml` with
`language = "nl"` (so the texts below stay those of paia).
"""

from __future__ import annotations

import hashlib
import importlib.util
import json
import pathlib
import re
import subprocess

import pytest

from lighthouse_lib.config import Host
from tests.pack._testkit import INSTALL_ROOT

SCRIPT = INSTALL_ROOT / ".lighthouse" / "scripts" / "plan_review_record.py"
HEADINGS = INSTALL_ROOT / ".lighthouse" / "headings.toml"
_SPEC = importlib.util.spec_from_file_location("plan_review_record", SCRIPT)
assert _SPEC and _SPEC.loader
rec = importlib.util.module_from_spec(_SPEC)
_SPEC.loader.exec_module(rec)
# The fixture `world` stubs `host_refusal`; the host tests below put the real one back.
REAL_HOST_REFUSAL = rec.host_refusal

PLAN = "docs/developer/planning/plans/2026-09-25-voorbeeld.md"
SPEC = "docs/developer/planning/specs/2026-09-25-voorbeeld-design.md"


def _git(repo: pathlib.Path, *args: str) -> str:
    return subprocess.run(
        ["git", "-C", str(repo), "-c", "user.name=t", "-c", "user.email=t@t", *args],
        capture_output=True,
        text=True,
        check=True,
    ).stdout.strip()


@pytest.fixture(autouse=True)
def config(tmp_path, monkeypatch) -> pathlib.Path:
    """A `lighthouse.toml` with `language = "nl"` outside the repo, and the headings table."""
    toml = tmp_path / "lighthouse.toml"
    toml.write_text('[project]\nlanguage = "nl"\n')
    monkeypatch.setenv("LIGHTHOUSE_TOML", str(toml))
    monkeypatch.setenv("LIGHTHOUSE_HEADINGS", str(HEADINGS))
    return toml


@pytest.fixture
def world(tmp_path, monkeypatch):
    """Throwaway repo with plan and spec committed, TMPDIR and data directory outside the repo."""
    repo = tmp_path / "repo"
    for rel, text in ((PLAN, "# plan v1\n"), (SPEC, "# spec\n")):
        (repo / rel).parent.mkdir(parents=True, exist_ok=True)
        (repo / rel).write_text(text)
    skill = repo / rec.DEFAULT_SKILL_DIRS["plan"]
    skill.mkdir(parents=True)
    (skill / "reviewer-prompt.md").write_text("prompt\n")
    (skill / "findings.schema.json").write_text("{}\n")
    spec_skill = repo / rec.DEFAULT_SKILL_DIRS["spec"]
    spec_skill.mkdir(parents=True)
    (spec_skill / "reviewer-prompt.md").write_text("spec prompt\n")
    (spec_skill / "findings.schema.json").write_text("{}\n")
    _git(repo, "init", "-q")
    _git(repo, "add", "-A")
    _git(repo, "commit", "-qm", "init")
    codex_home = tmp_path / "ch"
    codex_home.mkdir()
    (codex_home / "config.toml").write_text(
        'model = "gpt-5.6-sol"\nmodel_reasoning_effort = "high"\n'
    )
    monkeypatch.chdir(repo)
    monkeypatch.setenv("HOME", str(tmp_path / "home"))
    monkeypatch.setenv("TMPDIR", str(tmp_path / "tmp"))
    monkeypatch.setenv("LIGHTHOUSE_DATA", str(tmp_path / "data"))
    monkeypatch.setenv("LIGHTHOUSE_REPO_ROOT", str(repo))
    monkeypatch.setenv("CODEX_SPECREVIEW_HOME", str(codex_home))
    monkeypatch.setattr(rec, "host_refusal", lambda kind: None)
    monkeypatch.setattr(rec, "codex_version", lambda: "codex-cli 9.9.9")
    return {"repo": repo, "tmp": tmp_path}


def _start(capsys) -> pathlib.Path:
    assert rec.main(["start", PLAN]) == 0
    return pathlib.Path(capsys.readouterr().out.strip())


def _round(
    attempt: pathlib.Path,
    n: int,
    severities: list[str],
    verified: list[str] | None = None,
) -> None:
    findings = [
        {
            "severity": s,
            "cat": "code",
            "plan_section": "Taak 1",
            "problem": "p",
            "evidence": "e",
            "proposed_fix": "f",
        }
        for s in severities
    ]
    doc = {"review_markdown": "x", "status": "Majors only", "findings": findings}
    if verified is not None:
        doc["verified"] = verified
    (attempt / f"round-{n}.json").write_text(json.dumps(doc))


def test_start_reserves_a_fresh_attempt_dir(world, capsys):
    first = _start(capsys)
    second = _start(capsys)
    assert first != second
    assert first.parent == world["tmp"] / "tmp" / "plan-review" / "2026-09-25-voorbeeld"
    assert re.fullmatch(r"\d{8}T\d{6}Z-[0-9a-f]{6}", first.name)
    meta = json.loads((first / "attempt.json").read_text())
    assert meta["reviewed_sha"] == _git(world["repo"], "rev-parse", "HEAD")
    assert meta["chain_key"] == "2026-09-25-voorbeeld"
    # On the Beelink the attempt is in the data directory at once, even if `write` never
    # runs: the newest attempt is authoritative (spec section 5).
    chain = world["tmp"] / "data" / "plan-review" / "2026-09-25-voorbeeld"
    for attempt in (first, second):
        reserved = chain / f".{attempt.name}.tmp" / "attempt.json"
        assert json.loads(reserved.read_text()) == json.loads(
            (attempt / "attempt.json").read_text()
        )


def test_start_refuses_an_uncommitted_plan(world, capsys):
    (world["repo"] / PLAN).write_text("# plan, niet gecommit\n")
    assert rec.main(["start", PLAN]) == 2
    assert "commit first" in capsys.readouterr().err


def test_write_produces_the_record(world, capsys):
    attempt = _start(capsys)
    reviewed = _git(world["repo"], "rev-parse", "HEAD")
    _round(attempt, 1, ["MAJOR", "MINOR"])
    (attempt / "round-1.decisions.json").write_text(
        json.dumps(
            [
                {"finding_id": "r1-f1", "decision": "accept", "reason": "klopt"},
                {"finding_id": "r1-f2", "decision": "minor_yes", "reason": "batch"},
            ]
        )
    )
    (attempt / "round-1.json.events").write_text("groot\n")
    _round(attempt, 2, [], verified=["r1-f1"])
    (world["repo"] / PLAN).write_text("# plan v2\n")
    _git(world["repo"], "commit", "-qam", "fix")
    final_sha = _git(world["repo"], "rev-parse", "HEAD")

    code = rec.main(
        [
            "write",
            str(attempt),
            "--outcome",
            "converged",
            "--review-mode",
            "codex",
            "--spec",
            SPEC,
        ]
    )
    assert code == 0, capsys.readouterr().err
    out = pathlib.Path(capsys.readouterr().out.strip())
    assert (
        out
        == world["tmp"] / "data" / "plan-review" / "2026-09-25-voorbeeld" / attempt.name
    )
    assert sorted(p.name for p in out.iterdir()) == [
        "attempt.json",
        "record.json",
        "round-1.decisions.json",
        "round-1.json",
        "round-2.json",
    ]
    r = json.loads((out / "record.json").read_text())
    assert r["reviewed_sha"] == reviewed
    assert r["final_sha"] == final_sha
    assert r["plan_sha256_reviewed"] == hashlib.sha256(b"# plan v1\n").hexdigest()
    assert r["plan_sha256_final"] == hashlib.sha256(b"# plan v2\n").hexdigest()
    assert r["specs"] == [
        {"path": SPEC, "sha256": hashlib.sha256(b"# spec\n").hexdigest()}
    ]
    assert r["reviewer"] == {
        "model": "gpt-5.6-sol",
        "codex_version": "codex-cli 9.9.9",
        "same_family": False,
    }
    assert r["level"] == "high"
    assert r["rounds"] == 2
    assert r["outcome"] == "converged"
    assert r["schema_version"] == 5
    assert r["round_kinds"] == ["review", "verify"]
    assert (
        r["verifier_prompt_sha256"] is None
    )  # the fixture has no verifier-prompt.md
    assert r["verification_schema_sha256"] is None
    assert [(f["finding_id"], f["decision"]) for f in r["findings"]] == [
        ("r1-f1", "accept"),
        ("r1-f2", "minor_yes"),
    ]
    assert r["last_round_open"] == []
    assert r["issue_spec"] is None


def test_fallback_mode_nulls_the_codex_fields(world, capsys):
    attempt = _start(capsys)
    _round(attempt, 1, ["BLOCKER"])
    code = rec.main(
        [
            "write",
            str(attempt),
            "--outcome",
            "aborted",
            "--review-mode",
            "fallback",
            "--reviewer-model",
            "claude-opus-5-5",
            "--spec",
            SPEC,
        ]
    )
    assert code == 0
    out = pathlib.Path(capsys.readouterr().out.strip())
    r = json.loads((out / "record.json").read_text())
    assert r["reviewer"] == {
        "model": "claude-opus-5-5",
        "codex_version": None,
        "same_family": True,
    }
    assert r["level"] is None
    assert r["last_round_open"] == [
        {"finding_id": "r1-f1", "severity": "BLOCKER", "decision": None}
    ]


def test_issue_as_spec_stores_the_body_in_the_record_dir(world, capsys, tmp_path):
    attempt = _start(capsys)
    _round(attempt, 1, [])
    body = tmp_path / "issue-body.md"
    body.write_text("## AC\n- x\n")
    code = rec.main(
        [
            "write",
            str(attempt),
            "--outcome",
            "converged",
            "--review-mode",
            "codex",
            "--issue",
            "972",
            "--issue-body",
            str(body),
            "--issue-fetched-at",
            "2026-09-25T12:00:00Z",
        ]
    )
    assert code == 0
    out = pathlib.Path(capsys.readouterr().out.strip())
    assert (out / "issue-972.md").read_text() == "## AC\n- x\n"
    r = json.loads((out / "record.json").read_text())
    assert r["issue_spec"] == {
        "issue": 972,
        "fetched_at": "2026-09-25T12:00:00Z",
        "sha256": hashlib.sha256(b"## AC\n- x\n").hexdigest(),
        "file": "issue-972.md",
    }


def test_off_beelink_writes_nothing_and_says_so(world, capsys, monkeypatch):
    monkeypatch.setattr(
        rec, "host_refusal", lambda kind: f"{kind.upper()}-REVIEW NOT RECORDED: test"
    )
    attempt = _start(capsys)
    _round(attempt, 1, [])
    code = rec.main(
        [
            "write",
            str(attempt),
            "--outcome",
            "converged",
            "--review-mode",
            "codex",
            "--spec",
            SPEC,
        ]
    )
    assert code == 3
    assert "PLAN-REVIEW NOT RECORDED" in capsys.readouterr().err
    assert not (world["tmp"] / "data" / "plan-review").exists()


def test_write_unknown_host_returns_3_and_says_so(world, capsys, monkeypatch):
    def unknown(path=None):
        raise rec.HostError("host onbekend: geen host.toml (/x)")

    monkeypatch.setattr(rec, "host_refusal", REAL_HOST_REFUSAL)
    monkeypatch.setattr(rec, "recognize", unknown)
    attempt = _start(capsys)
    _round(attempt, 1, [])
    code = rec.main(
        [
            "write",
            str(attempt),
            "--outcome",
            "converged",
            "--review-mode",
            "codex",
            "--spec",
            SPEC,
        ]
    )
    assert code == 3
    assert "host onbekend" in capsys.readouterr().err
    assert not (world["tmp"] / "data" / "plan-review").exists()


def test_host_refusal_unknown_host(monkeypatch):
    def unknown(path=None):
        raise rec.HostError("host onbekend: geen host.toml (/x)")

    monkeypatch.setattr(rec, "recognize", unknown)
    text = rec.host_refusal("plan")
    assert text is not None
    assert "PLAN-REVIEW NOT RECORDED" in text
    assert "host onbekend: geen host.toml (/x)" in text
    assert "niet op de Beelink" not in text


def test_host_refusal_other_host(monkeypatch):
    monkeypatch.setattr(rec, "recognize", lambda path=None: None)
    text = rec.host_refusal("spec")
    assert text is not None
    assert "not on the host from host.toml" in text
    assert "falls outside the measurement" in text


def test_host_refusal_recognized(monkeypatch):
    monkeypatch.setattr(
        rec,
        "recognize",
        lambda path=None: Host("beelink", "100.76.126.126"),
    )
    assert rec.host_refusal("plan") is None


def test_write_failure_leaves_only_staging(world, capsys, monkeypatch):
    attempt = _start(capsys)
    _round(attempt, 1, [])

    def boom(src, dst):
        raise OSError("schijf vol")

    monkeypatch.setattr(rec.os, "rename", boom)
    code = rec.main(
        [
            "write",
            str(attempt),
            "--outcome",
            "converged",
            "--review-mode",
            "codex",
            "--spec",
            SPEC,
        ]
    )
    assert code == 4
    assert "PLAN-REVIEW-RECORD FAILED" in capsys.readouterr().err
    chain = world["tmp"] / "data" / "plan-review" / "2026-09-25-voorbeeld"
    assert sorted(p.name for p in chain.iterdir()) == [f".{attempt.name}.tmp"]


def test_unknown_decision_is_a_validation_error(world, capsys):
    attempt = _start(capsys)
    _round(attempt, 1, ["MAJOR"])
    (attempt / "round-1.decisions.json").write_text(
        json.dumps([{"finding_id": "r1-f1", "decision": "defer", "reason": "x"}])
    )
    code = rec.main(
        [
            "write",
            str(attempt),
            "--outcome",
            "converged",
            "--review-mode",
            "codex",
            "--spec",
            SPEC,
        ]
    )
    assert code == 4
    assert "unknown decision" in capsys.readouterr().err


def test_missing_decision_blocks_a_converged_record(world, capsys):
    """Without a sidecar for the last round the record would store `decision: null`
    as valid measurement data; only `aborted` may do that."""
    attempt = _start(capsys)
    _round(attempt, 1, ["MAJOR"])
    code = rec.main(
        [
            "write",
            str(attempt),
            "--outcome",
            "converged",
            "--review-mode",
            "codex",
            "--spec",
            SPEC,
        ]
    )
    assert code == 4
    assert "no decision for r1-f1" in capsys.readouterr().err


def test_converged_with_an_open_major_is_a_validation_error(world, capsys):
    """A MAJOR from the last round that got `accept` is fixed but no longer
    reviewed: that is open, so not `converged` (skill step 8)."""
    attempt = _start(capsys)
    _round(attempt, 1, ["MAJOR"])
    (attempt / "round-1.decisions.json").write_text(
        json.dumps([{"finding_id": "r1-f1", "decision": "accept", "reason": "x"}])
    )
    code = rec.main(
        [
            "write",
            str(attempt),
            "--outcome",
            "converged",
            "--review-mode",
            "codex",
            "--spec",
            SPEC,
        ]
    )
    assert code == 4
    assert "r1-f1 is still open" in capsys.readouterr().err


@pytest.mark.parametrize(
    ("rows", "notice"),
    [
        (
            [
                {"finding_id": "r1-f1", "decision": "accept", "reason": "x"},
                {"finding_id": "r1-f1", "decision": "reject", "reason": "y"},
            ],
            "occurs twice",
        ),
        (
            [
                {"finding_id": "r1-f1", "decision": "accept", "reason": "x"},
                {"finding_id": "r1-f2", "decision": "accept", "reason": "y"},
            ],
            "does not exist",
        ),
    ],
    ids=["dubbel", "onbekend-id"],
)
def test_bad_finding_id_is_a_validation_error(world, capsys, rows, notice):
    attempt = _start(capsys)
    _round(attempt, 1, ["MAJOR"])
    (attempt / "round-1.decisions.json").write_text(json.dumps(rows))
    code = rec.main(
        [
            "write",
            str(attempt),
            "--outcome",
            "aborted",
            "--review-mode",
            "codex",
            "--spec",
            SPEC,
        ]
    )
    assert code == 4
    assert notice in capsys.readouterr().err


def test_existing_attempt_is_never_overwritten(world, capsys):
    attempt = _start(capsys)
    _round(attempt, 1, [])
    args = [
        "write",
        str(attempt),
        "--outcome",
        "converged",
        "--review-mode",
        "codex",
        "--spec",
        SPEC,
    ]
    assert rec.main(args) == 0
    capsys.readouterr()
    assert rec.main(args) == 4
    assert "already exists" in capsys.readouterr().err


def test_malformed_decisions_sidecar_is_a_validation_error(world, capsys):
    """A sidecar that is not a list of objects (here: one bare object) must not give a
    traceback — `main` must stay exit 4 with PLAN-REVIEW-RECORD FAILED."""
    attempt = _start(capsys)
    _round(attempt, 1, ["MAJOR"])
    (attempt / "round-1.decisions.json").write_text(json.dumps({"finding_id": "r1-f1"}))
    code = rec.main(
        [
            "write",
            str(attempt),
            "--outcome",
            "aborted",
            "--review-mode",
            "codex",
            "--spec",
            SPEC,
        ]
    )
    assert code == 4
    err = capsys.readouterr().err
    assert "PLAN-REVIEW-RECORD FAILED" in err
    assert "round-1.decisions.json" in err


def test_non_contiguous_rounds_is_a_validation_error(world, capsys):
    """`round-2.json` is missing: only rounds 1 and 3 are there. That must not end up
    silently as `rounds: 2` in the record."""
    attempt = _start(capsys)
    _round(attempt, 1, [])
    _round(attempt, 3, [])
    code = rec.main(
        [
            "write",
            str(attempt),
            "--outcome",
            "aborted",
            "--review-mode",
            "codex",
            "--spec",
            SPEC,
        ]
    )
    assert code == 4
    err = capsys.readouterr().err
    assert "PLAN-REVIEW-RECORD FAILED" in err
    assert "missing round" in err
    assert "2" in err


def test_verifier_prompt_is_hashed_when_present(world, capsys):
    skill = world["repo"] / rec.DEFAULT_SKILL_DIRS["plan"]
    (skill / "verifier-prompt.md").write_text("verify\n")
    (skill / "verification.schema.json").write_text("{}\n")
    _git(world["repo"], "add", "-A")
    _git(world["repo"], "commit", "-qm", "verifier")
    attempt = _start(capsys)
    _round(attempt, 1, [])
    code = rec.main(
        [
            "write",
            str(attempt),
            "--outcome",
            "converged",
            "--review-mode",
            "codex",
            "--spec",
            SPEC,
        ]
    )
    assert code == 0, capsys.readouterr().err
    r = json.loads(
        (pathlib.Path(capsys.readouterr().out.strip()) / "record.json").read_text()
    )
    assert r["verifier_prompt_sha256"] == hashlib.sha256(b"verify\n").hexdigest()
    assert r["verification_schema_sha256"] == hashlib.sha256(b"{}\n").hexdigest()
    assert r["round_kinds"] == ["review"]


def test_fix_incomplete_in_the_last_verification_blocks_converged(world, capsys):
    """Round 2 finds that the fix of r1-f1 is wrong; the operator accepts and
    fixes, but nobody verified that fix: open, so not `converged`."""
    attempt = _start(capsys)
    _round(attempt, 1, ["MAJOR"])
    (attempt / "round-1.decisions.json").write_text(
        json.dumps([{"finding_id": "r1-f1", "decision": "accept", "reason": "x"}])
    )
    _round(attempt, 2, ["MAJOR"], verified=[])
    (attempt / "round-2.decisions.json").write_text(
        json.dumps([{"finding_id": "r2-f1", "decision": "accept", "reason": "y"}])
    )
    code = rec.main(
        [
            "write",
            str(attempt),
            "--outcome",
            "converged",
            "--review-mode",
            "codex",
            "--spec",
            SPEC,
        ]
    )
    assert code == 4
    assert "r2-f1 is still open" in capsys.readouterr().err


def test_start_kind_spec_uses_the_spec_review_dir(world, capsys):
    assert rec.main(["start", "--kind", "spec", SPEC]) == 0
    out = pathlib.Path(capsys.readouterr().out.strip())
    assert "/spec-review/2026-09-25-voorbeeld-design/" in str(out)
    meta = json.loads((out / "attempt.json").read_text())
    assert meta["kind"] == "spec"
    assert meta["spec"] == SPEC


def test_spec_kind_writes_a_spec_record(world, capsys):
    assert rec.main(["start", "--kind", "spec", SPEC]) == 0
    attempt = pathlib.Path(capsys.readouterr().out.strip())
    _round(attempt, 1, ["MAJOR", "MINOR"])
    (attempt / "round-1.decisions.json").write_text(
        json.dumps(
            [
                {
                    "finding_id": "r1-f1",
                    "decision": "defer",
                    "reason": "hoort in het plan",
                },
                {"finding_id": "r1-f2", "decision": "minor_no", "reason": "nee"},
            ]
        )
    )
    code = rec.main(
        ["write", str(attempt), "--outcome", "converged", "--review-mode", "codex"]
    )
    assert code == 0, capsys.readouterr().err
    out = pathlib.Path(capsys.readouterr().out.strip())
    assert (
        out
        == world["tmp"]
        / "data"
        / "spec-review"
        / "2026-09-25-voorbeeld-design"
        / attempt.name
    )
    r = json.loads((out / "record.json").read_text())
    assert r["kind"] == "spec"
    assert r["spec"] == SPEC
    assert "plan" not in r
    assert r["specs"] == []
    assert r["issue_spec"] is None
    assert r["schema_version"] == 5
    skill_prompt = (
        world["repo"] / rec.DEFAULT_SKILL_DIRS["spec"] / "reviewer-prompt.md"
    )
    assert r["prompt_sha256"] == hashlib.sha256(skill_prompt.read_bytes()).hexdigest()


def test_spec_kind_rejects_the_plan_only_decision(world, capsys):
    assert rec.main(["start", "--kind", "spec", SPEC]) == 0
    spec_attempt = pathlib.Path(capsys.readouterr().out.strip())
    _round(spec_attempt, 1, ["MAJOR"])
    (spec_attempt / "round-1.decisions.json").write_text(
        json.dumps([{"finding_id": "r1-f1", "decision": "spec", "reason": "x"}])
    )
    code = rec.main(
        [
            "write",
            str(spec_attempt),
            "--outcome",
            "converged",
            "--review-mode",
            "codex",
        ]
    )
    assert code == 4
    assert "unknown decision" in capsys.readouterr().err

    plan_attempt = _start(capsys)
    _round(plan_attempt, 1, ["MAJOR"])
    (plan_attempt / "round-1.decisions.json").write_text(
        json.dumps([{"finding_id": "r1-f1", "decision": "defer", "reason": "x"}])
    )
    code = rec.main(
        [
            "write",
            str(plan_attempt),
            "--outcome",
            "converged",
            "--review-mode",
            "codex",
            "--spec",
            SPEC,
        ]
    )
    assert code == 4
    assert "unknown decision" in capsys.readouterr().err


def test_spec_kind_refuses_spec_or_issue_args(world, capsys):
    assert rec.main(["start", "--kind", "spec", SPEC]) == 0
    attempt = pathlib.Path(capsys.readouterr().out.strip())
    _round(attempt, 1, [])
    code = rec.main(
        [
            "write",
            str(attempt),
            "--outcome",
            "converged",
            "--review-mode",
            "codex",
            "--spec",
            SPEC,
        ]
    )
    assert code == 2


def test_attempt_without_kind_is_a_plan(world, capsys):
    attempt = _start(capsys)
    meta = json.loads((attempt / "attempt.json").read_text())
    del meta["kind"]
    (attempt / "attempt.json").write_text(json.dumps(meta))
    _round(attempt, 1, [])
    code = rec.main(
        [
            "write",
            str(attempt),
            "--outcome",
            "converged",
            "--review-mode",
            "codex",
            "--spec",
            SPEC,
        ]
    )
    assert code == 0, capsys.readouterr().err
    out = pathlib.Path(capsys.readouterr().out.strip())
    r = json.loads((out / "record.json").read_text())
    assert r["kind"] == "plan"
    assert "plan" in r
    assert r["schema_version"] == 5


def test_spec_kind_accept_on_last_round_major_stays_open(world, capsys):
    assert rec.main(["start", "--kind", "spec", SPEC]) == 0
    attempt = pathlib.Path(capsys.readouterr().out.strip())
    _round(attempt, 1, ["MAJOR"])
    (attempt / "round-1.decisions.json").write_text(
        json.dumps([{"finding_id": "r1-f1", "decision": "accept", "reason": "x"}])
    )
    code = rec.main(
        ["write", str(attempt), "--outcome", "converged", "--review-mode", "codex"]
    )
    assert code == 4
    assert "is still open" in capsys.readouterr().err


def _three_rounds(attempt: pathlib.Path, last_decision: str) -> None:
    """Three rounds with one MAJOR each; only the last decision varies."""
    for n in (1, 2, 3):
        _round(attempt, n, ["MAJOR"], verified=[] if n > 1 else None)
        decision = last_decision if n == 3 else "accept"
        (attempt / f"round-{n}.decisions.json").write_text(
            json.dumps(
                [{"finding_id": f"r{n}-f1", "decision": decision, "reason": "x"}]
            )
        )


def _write_round_limit(attempt: pathlib.Path, *extra: str) -> int:
    return rec.main(
        [
            "write",
            str(attempt),
            "--outcome",
            "round_limit",
            "--review-mode",
            "codex",
            *extra,
        ]
    )


def test_round_limit_with_an_open_major_without_accepted_open_is_refused(world, capsys):
    attempt = _start(capsys)
    _three_rounds(attempt, "accept")
    code = _write_round_limit(attempt, "--spec", SPEC)
    assert code == 4
    err = capsys.readouterr().err
    assert "r3-f1" in err and "accept-open" in err
    assert "accept-open <ATTEMPT> --doc <DOC>" in err


def test_round_limit_with_accepted_open_is_written(world, capsys):
    attempt = _start(capsys)
    _three_rounds(attempt, "accepted_open")
    code = _write_round_limit(attempt, "--spec", SPEC)
    assert code == 0, capsys.readouterr().err
    out = pathlib.Path(capsys.readouterr().out.strip())
    record = json.loads((out / "record.json").read_text())
    assert record["outcome"] == "round_limit"
    assert [(o["finding_id"], o["decision"]) for o in record["last_round_open"]] == [
        ("r3-f1", "accepted_open")
    ]


def test_spec_kind_accepts_accepted_open_at_round_limit(world, capsys):
    assert rec.main(["start", "--kind", "spec", SPEC]) == 0
    attempt = pathlib.Path(capsys.readouterr().out.strip())
    _three_rounds(attempt, "accepted_open")
    code = _write_round_limit(attempt)
    assert code == 0, capsys.readouterr().err
    out = pathlib.Path(capsys.readouterr().out.strip())
    assert json.loads((out / "record.json").read_text())["outcome"] == "round_limit"


def _write_sources(attempt: pathlib.Path, rows: list[dict]) -> None:
    (attempt / "round-1.sources.json").write_text(json.dumps(rows))


def test_paired_round_one_carries_the_source(world, capsys):
    attempt = _start(capsys)
    _round(attempt, 1, ["MAJOR", "MINOR"])
    (attempt / "round-1.decisions.json").write_text(
        json.dumps(
            [
                {"finding_id": "r1-f1", "decision": "accept", "reason": "x"},
                {"finding_id": "r1-f2", "decision": "minor_no", "reason": "y"},
            ]
        )
    )
    _write_sources(
        attempt,
        [
            {
                "finding_id": "r1-f1",
                "source": "both",
                "blind_ids": ["A", "B"],
                "severity_by_source": {"codex": "MAJOR", "fallback": "MINOR"},
            },
            {
                "finding_id": "r1-f2",
                "source": "fallback",
                "blind_ids": ["C"],
                "severity_by_source": {"fallback": "MINOR"},
            },
        ],
    )
    (attempt / "pair.json").write_text(json.dumps({"r1-f1": ["A", "C"]}))
    code = rec.main(
        [
            "write",
            str(attempt),
            "--outcome",
            "aborted",
            "--review-mode",
            "codex",
            "--spec",
            SPEC,
            "--shadow-model",
            "claude-opus-5-5",
        ]
    )
    assert code == 0, capsys.readouterr().err
    out = pathlib.Path(capsys.readouterr().out.strip())
    r = json.loads((out / "record.json").read_text())
    assert r["paired"] is True
    assert r["shadow"] == {"model": "claude-opus-5-5", "same_family": True}
    assert [f["source"] for f in r["findings"]] == ["both", "fallback"]
    assert [f["severity_by_source"] for f in r["findings"]] == [
        {"codex": "MAJOR", "fallback": "MINOR"},
        {"fallback": "MINOR"},
    ]
    assert (out / "pair.json").is_file()
    assert (out / "round-1.sources.json").is_file()


def test_later_rounds_have_no_source(world, capsys):
    attempt = _start(capsys)
    _round(attempt, 1, ["MAJOR"])
    (attempt / "round-1.decisions.json").write_text(
        json.dumps([{"finding_id": "r1-f1", "decision": "accept", "reason": "x"}])
    )
    _write_sources(
        attempt,
        [
            {
                "finding_id": "r1-f1",
                "source": "codex",
                "blind_ids": ["A"],
                "severity_by_source": {"codex": "MAJOR"},
            }
        ],
    )
    _round(attempt, 2, ["MINOR"], verified=["r1-f1"])
    (attempt / "round-2.decisions.json").write_text(
        json.dumps([{"finding_id": "r2-f1", "decision": "minor_no", "reason": "y"}])
    )
    code = rec.main(
        [
            "write",
            str(attempt),
            "--outcome",
            "converged",
            "--review-mode",
            "codex",
            "--spec",
            SPEC,
            "--shadow-model",
            "claude-opus-5-5",
        ]
    )
    assert code == 0, capsys.readouterr().err
    out = pathlib.Path(capsys.readouterr().out.strip())
    r = json.loads((out / "record.json").read_text())
    round1 = next(f for f in r["findings"] if f["finding_id"] == "r1-f1")
    round2 = next(f for f in r["findings"] if f["finding_id"] == "r2-f1")
    assert round1["source"] == "codex"
    assert round2["source"] is None
    assert round2["severity_by_source"] is None


def test_sources_without_shadow_model_is_a_validation_error(world, capsys):
    attempt = _start(capsys)
    _round(attempt, 1, ["MAJOR"])
    (attempt / "round-1.decisions.json").write_text(
        json.dumps([{"finding_id": "r1-f1", "decision": "accept", "reason": "x"}])
    )
    _write_sources(
        attempt,
        [
            {
                "finding_id": "r1-f1",
                "source": "codex",
                "blind_ids": ["A"],
                "severity_by_source": {"codex": "MAJOR"},
            }
        ],
    )
    code = rec.main(
        [
            "write",
            str(attempt),
            "--outcome",
            "aborted",
            "--review-mode",
            "codex",
            "--spec",
            SPEC,
        ]
    )
    assert code == 4
    assert "--shadow-model" in capsys.readouterr().err

    attempt2 = _start(capsys)
    _round(attempt2, 1, [])
    code = rec.main(
        [
            "write",
            str(attempt2),
            "--outcome",
            "converged",
            "--review-mode",
            "codex",
            "--spec",
            SPEC,
            "--shadow-model",
            "claude-opus-5-5",
        ]
    )
    assert code == 4
    assert "--shadow-model" in capsys.readouterr().err


def test_sources_must_cover_round_one_exactly(world, capsys):
    attempt = _start(capsys)
    _round(attempt, 1, ["MAJOR", "MINOR"])
    (attempt / "round-1.decisions.json").write_text(
        json.dumps(
            [
                {"finding_id": "r1-f1", "decision": "accept", "reason": "x"},
                {"finding_id": "r1-f2", "decision": "minor_no", "reason": "y"},
            ]
        )
    )
    _write_sources(
        attempt,
        [
            {
                "finding_id": "r1-f1",
                "source": "codex",
                "blind_ids": ["A"],
                "severity_by_source": {"codex": "MAJOR"},
            }
        ],
    )
    code = rec.main(
        [
            "write",
            str(attempt),
            "--outcome",
            "aborted",
            "--review-mode",
            "codex",
            "--spec",
            SPEC,
            "--shadow-model",
            "claude-opus-5-5",
        ]
    )
    assert code == 4

    attempt2 = _start(capsys)
    _round(attempt2, 1, ["MAJOR", "MINOR"])
    (attempt2 / "round-1.decisions.json").write_text(
        json.dumps(
            [
                {"finding_id": "r1-f1", "decision": "accept", "reason": "x"},
                {"finding_id": "r1-f2", "decision": "minor_no", "reason": "y"},
            ]
        )
    )
    _write_sources(
        attempt2,
        [
            {
                "finding_id": "r1-f1",
                "source": "codex",
                "blind_ids": ["A"],
                "severity_by_source": {"codex": "MAJOR"},
            },
            {
                "finding_id": "r1-f2",
                "source": "fallback",
                "blind_ids": ["B"],
                "severity_by_source": {"fallback": "MINOR"},
            },
            {
                "finding_id": "r1-f3",
                "source": "codex",
                "blind_ids": ["C"],
                "severity_by_source": {"codex": "MINOR"},
            },
        ],
    )
    code = rec.main(
        [
            "write",
            str(attempt2),
            "--outcome",
            "aborted",
            "--review-mode",
            "codex",
            "--spec",
            SPEC,
            "--shadow-model",
            "claude-opus-5-5",
        ]
    )
    assert code == 4


def test_fallback_file_without_sources_is_not_paired(world, capsys):
    attempt = _start(capsys)
    _round(attempt, 1, ["MINOR"])
    (attempt / "round-1.decisions.json").write_text(
        json.dumps([{"finding_id": "r1-f1", "decision": "minor_no", "reason": "x"}])
    )
    (attempt / "round-1.fallback.json").write_text(json.dumps({"findings": []}))
    code = rec.main(
        [
            "write",
            str(attempt),
            "--outcome",
            "converged",
            "--review-mode",
            "fallback",
            "--reviewer-model",
            "m",
            "--spec",
            SPEC,
        ]
    )
    assert code == 0, capsys.readouterr().err
    out = pathlib.Path(capsys.readouterr().out.strip())
    assert (out / "round-1.fallback.json").is_file()
    r = json.loads((out / "record.json").read_text())
    assert r["paired"] is False
    assert r["shadow"] is None
    assert all(f["source"] is None for f in r["findings"])


def test_spec_kind_final_hash_follows_a_committed_fix(world, capsys):
    assert rec.main(["start", "--kind", "spec", SPEC]) == 0
    attempt = pathlib.Path(capsys.readouterr().out.strip())
    _round(attempt, 1, [])
    (world["repo"] / SPEC).write_text("# spec v2\n")
    _git(world["repo"], "commit", "-qam", "fix spec")
    final_sha = _git(world["repo"], "rev-parse", "HEAD")
    code = rec.main(
        ["write", str(attempt), "--outcome", "converged", "--review-mode", "codex"]
    )
    assert code == 0, capsys.readouterr().err
    out = pathlib.Path(capsys.readouterr().out.strip())
    r = json.loads((out / "record.json").read_text())
    assert r["spec_sha256_reviewed"] == hashlib.sha256(b"# spec\n").hexdigest()
    assert r["spec_sha256_final"] == hashlib.sha256(b"# spec v2\n").hexdigest()
    assert r["spec_sha256_reviewed"] != r["spec_sha256_final"]
    assert r["final_sha"] == final_sha


def _budget(attempt, name, doc_or_text):
    text = doc_or_text if isinstance(doc_or_text, str) else json.dumps(doc_or_text)
    (attempt / name).write_text(text)


FALLBACK = {
    "decision": "fallback",
    "reason": "5u 92% ≥ 90%",
    "window": "codex-5u",
    "reset_at": "2026-10-03T19:06:00Z",
    "session_pct": 92,
    "week_pct": 41,
    "source": "app-server",
    "thresholds": {"session": 90, "week": 95},
}
SYNTHETIC = {
    "decision": "not_measurable",
    "reason": "budgetcheck faalde: exit 1",
    **dict.fromkeys(
        [
            "window",
            "reset_at",
            "session_pct",
            "week_pct",
            "source",
            "thresholds",
        ]
    ),
}


def test_budget_checks_in_v4_record(world, capsys):
    attempt = _start(capsys)
    _round(attempt, 1, [])
    _budget(attempt, "round-1.codex.json.budget.json", FALLBACK)
    _budget(attempt, "round-1.json.budget.json", SYNTHETIC)
    _budget(attempt, "round-2.json.budget.json", "geen json")
    _budget(attempt, "round-3.json.budget.json", [])  # valid JSON, not an object
    _round(attempt, 2, [], verified=[])
    _round(attempt, 3, [], verified=[])
    code = rec.main(
        [
            "write",
            str(attempt),
            "--outcome",
            "converged",
            "--review-mode",
            "fallback",
            "--reviewer-model",
            "opus",
            "--spec",
            SPEC,
        ]
    )
    assert code == 0, capsys.readouterr().err
    out = pathlib.Path(capsys.readouterr().out.strip())
    r = json.loads((out / "record.json").read_text())
    assert r["schema_version"] == 5
    assert r["budget_checks"][0] == {
        "round": 1,
        "file": "round-1.codex.json.budget.json",
        "decision": "fallback",
        "reason": "5u 92% ≥ 90%",
        "session_pct": 92,
        "week_pct": 41,
        "source": "app-server",
        "thresholds": {"session": 90, "week": 95},
    }
    assert r["budget_checks"][1]["decision"] == "not_measurable"
    assert r["budget_checks"][1]["session_pct"] is None
    assert [c["decision"] for c in r["budget_checks"][2:]] == [
        "unreadable",
        "unreadable",
    ]
    for name in (
        "round-1.codex.json.budget.json",
        "round-1.json.budget.json",
        "round-2.json.budget.json",
        "round-3.json.budget.json",
    ):
        assert (out / name).is_file()


def test_without_budget_files_empty_list(world, capsys):
    attempt = _start(capsys)
    _round(attempt, 1, [])
    code = rec.main(
        [
            "write",
            str(attempt),
            "--outcome",
            "converged",
            "--review-mode",
            "codex",
            "--spec",
            SPEC,
        ]
    )
    assert code == 0, capsys.readouterr().err
    out = pathlib.Path(capsys.readouterr().out.strip())
    r = json.loads((out / "record.json").read_text())
    assert r["schema_version"] == 5
    assert r["budget_checks"] == []


def test_incomplete_budget_file_becomes_null(world, capsys):
    attempt = _start(capsys)
    _round(attempt, 1, [])
    _budget(attempt, "round-1.json.budget.json", {"decision": "codex"})
    code = rec.main(
        [
            "write",
            str(attempt),
            "--outcome",
            "converged",
            "--review-mode",
            "codex",
            "--spec",
            SPEC,
        ]
    )
    assert code == 0, capsys.readouterr().err
    out = pathlib.Path(capsys.readouterr().out.strip())
    r = json.loads((out / "record.json").read_text())
    assert r["budget_checks"] == [
        {
            "round": 1,
            "file": "round-1.json.budget.json",
            "decision": "codex",
            "reason": None,
            "session_pct": None,
            "week_pct": None,
            "source": None,
            "thresholds": None,
        }
    ]


# --- accept-open (#1352) --------------------------------------------------


def _round_raw(attempt: pathlib.Path, n: int, findings: list[dict]) -> None:
    doc = {"review_markdown": "x", "status": "Majors only", "findings": findings}
    (attempt / f"round-{n}.json").write_text(json.dumps(doc))


def _sidecar(attempt: pathlib.Path, n: int, rows: list[dict]) -> pathlib.Path:
    path = attempt / f"round-{n}.decisions.json"
    path.write_text(json.dumps(rows))
    return path


def _row(fid: str, decision: str, reason: str = "x") -> dict:
    return {"finding_id": fid, "decision": decision, "reason": reason}


def _round3_with_sidecar(attempt: pathlib.Path) -> pathlib.Path:
    """Rounds 1-3; round 3 has MAJOR, BLOCKER, MINOR with three kinds of decision."""
    _round(attempt, 1, ["MAJOR"])
    _round(attempt, 2, ["MAJOR"], verified=[])
    _round(attempt, 3, ["MAJOR", "BLOCKER", "MINOR"], verified=[])
    return _sidecar(
        attempt,
        3,
        [
            _row("r3-f1", "accept", "fix in Taak 2"),
            _row("r3-f2", "reject", "klopt niet"),
            _row("r3-f3", "minor_no", "x"),
        ],
    )


def _doc(tmp_path: pathlib.Path, text: str = "# Plan\n\nTekst.\n") -> pathlib.Path:
    doc = tmp_path / "plan.md"
    doc.write_text(text)
    return doc


def _accept(attempt: pathlib.Path, doc: pathlib.Path) -> int:
    return rec.main(["accept-open", str(attempt), "--doc", str(doc)])


def test_accept_open_rewrites_the_last_sidecar(world, capsys, tmp_path):
    attempt = _start(capsys)
    doc = _doc(tmp_path)
    sidecar = _round3_with_sidecar(attempt)
    assert _accept(attempt, doc) == 0
    out = capsys.readouterr().out
    rows = {r["finding_id"]: r for r in json.loads(sidecar.read_text())}
    assert rows["r3-f1"]["decision"] == "accepted_open"
    assert rows["r3-f1"]["reason"] == "round_limit; was accept: fix in Taak 2"
    assert rows["r3-f2"]["decision"] == "reject"
    assert rows["r3-f3"]["decision"] == "minor_no"
    assert out.startswith("## Open na review\n")
    assert "- **r3-f1** (MAJOR, Taak 1): p" in out
    assert "r3-f2" not in out
    assert doc.read_text() == "# Plan\n\nTekst.\n\n" + out


def test_accept_open_fills_a_missing_row(world, capsys, tmp_path):
    attempt = _start(capsys)
    doc = _doc(tmp_path)
    _round(attempt, 3, ["MAJOR"])
    sidecar = _sidecar(attempt, 3, [])
    assert _accept(attempt, doc) == 0
    assert json.loads(sidecar.read_text()) == [
        {
            "finding_id": "r3-f1",
            "decision": "accepted_open",
            "reason": "round_limit; without an earlier decision",
        }
    ]


def test_accept_open_without_a_sidecar_file_creates_the_rows(
    world, capsys, tmp_path
):
    attempt = _start(capsys)
    doc = _doc(tmp_path)
    _round(attempt, 1, ["BLOCKER", "MAJOR"])
    assert _accept(attempt, doc) == 0
    rows = json.loads((attempt / "round-1.decisions.json").read_text())
    assert [(r["finding_id"], r["decision"]) for r in rows] == [
        ("r1-f1", "accepted_open"),
        ("r1-f2", "accepted_open"),
    ]


def test_accept_open_keeps_same_as_and_appends_missing_rows(world, capsys, tmp_path):
    attempt = _start(capsys)
    doc = _doc(tmp_path)
    _round(attempt, 1, ["MAJOR", "MAJOR", "MAJOR"])
    _sidecar(
        attempt,
        1,
        [
            {**_row("r1-f2", "accept", "dubbel"), "same_as": "r1-f1"},
            _row("r1-f3", "reject", "nee"),
        ],
    )
    assert _accept(attempt, doc) == 0
    rows = json.loads((attempt / "round-1.decisions.json").read_text())
    assert [r["finding_id"] for r in rows] == ["r1-f2", "r1-f3", "r1-f1"]
    assert rows[0]["same_as"] == "r1-f1"
    assert rows[0]["decision"] == "accepted_open"
    assert rows[1]["decision"] == "reject"


def test_accept_open_is_idempotent(world, capsys, tmp_path):
    attempt = _start(capsys)
    doc = _doc(tmp_path)
    sidecar = _round3_with_sidecar(attempt)
    assert _accept(attempt, doc) == 0
    first_out = capsys.readouterr().out
    first_bytes = sidecar.read_bytes()
    assert _accept(attempt, doc) == 0
    assert capsys.readouterr().out == first_out
    assert sidecar.read_bytes() == first_bytes


def test_accept_open_doc_upsert_is_idempotent(world, capsys, tmp_path):
    attempt = _start(capsys)
    doc = _doc(tmp_path)
    _round3_with_sidecar(attempt)
    assert _accept(attempt, doc) == 0
    first = doc.read_text()
    assert _accept(attempt, doc) == 0
    assert doc.read_text() == first
    assert first.count("## Open na review") == 1


def test_accept_open_doc_ignores_a_fenced_heading(world, capsys, tmp_path):
    attempt = _start(capsys)
    example = "## Iets\n\n```markdown\n## Open na review\n\n- oud voorbeeld\n```\n"
    doc = _doc(tmp_path, example)
    _round3_with_sidecar(attempt)
    assert _accept(attempt, doc) == 0
    block = capsys.readouterr().out
    assert doc.read_text() == example + "\n" + block
    assert _accept(attempt, doc) == 0
    assert doc.read_text() == example + "\n" + block


def test_open_section_only_matches_the_last_section_outside_fences():
    block = "## Open na review\n\n- x\n"
    prefix = "# T\n\n## A\n\n"
    assert rec.open_section(prefix + block) == (len(prefix), len(prefix + block))
    assert rec.open_section("## Open na review\n\n- x\n\n## Daarna\n\ny\n") is None
    assert rec.open_section("# T\n\n~~~\n## Open na review\n~~~\n") is None
    assert rec.open_section("# T\n\n```\n## Open na review\n") is None
    assert rec.open_section("# T\n\n### Open na review\n") is None
    assert rec.open_section("# T\n\n## Open na review extra\n") is None
    assert rec.open_section("# T\n") is None


def test_accept_open_doc_without_trailing_newline_gets_one_blank_line(
    world, capsys, tmp_path
):
    attempt = _start(capsys)
    doc = _doc(tmp_path, "# Plan")
    _round3_with_sidecar(attempt)
    assert _accept(attempt, doc) == 0
    assert doc.read_text() == "# Plan\n\n" + capsys.readouterr().out


def test_accept_open_neutralises_closing_keywords(world, capsys, tmp_path):
    attempt = _start(capsys)
    doc = _doc(tmp_path)
    _round_raw(
        attempt,
        1,
        [
            {
                "severity": "MAJOR",
                "cat": "code",
                "plan_section": "Taak 3",
                "problem": "de fix #2 in Taak 3\n  en Closes #7, resolved: #9 (zie #5)",
                "evidence": "e",
                "proposed_fix": "f",
            }
        ],
    )
    assert _accept(attempt, doc) == 0
    out = capsys.readouterr().out
    assert (
        "- **r1-f1** (MAJOR, Taak 3): de fix issue 2 in Taak 3 en Closes issue 7,"
        in out
    )
    assert "resolved: issue 9 (zie #5)" in out
    assert not re.search(r"(?i)\b(close[sd]?|fix(?:e[sd])?|resolve[sd]?):?\s+#\d", out)


def test_accept_open_without_open_points_exits_2(world, capsys, tmp_path):
    attempt = _start(capsys)
    doc = _doc(tmp_path)
    _round(attempt, 1, ["MAJOR"])
    sidecar = _sidecar(attempt, 1, [_row("r1-f1", "reject")])
    before = sidecar.read_bytes()
    assert _accept(attempt, doc) == 2
    captured = capsys.readouterr()
    assert captured.out == ""
    assert "no open" in captured.err
    assert sidecar.read_bytes() == before
    assert doc.read_text() == "# Plan\n\nTekst.\n"


def test_accept_open_round_json_without_severity_exits_4(world, capsys, tmp_path):
    attempt = _start(capsys)
    doc = _doc(tmp_path)
    _round_raw(attempt, 1, [{"cat": "code", "plan_section": "Taak 1", "problem": "p"}])
    assert _accept(attempt, doc) == 4
    captured = capsys.readouterr()
    assert captured.err.startswith("ACCEPT-OPEN FAILED:")
    assert "Traceback" not in captured.err
    assert captured.out == ""
    assert not (attempt / "round-1.decisions.json").exists()
    assert doc.read_text() == "# Plan\n\nTekst.\n"


def test_accept_open_spec_kind_uses_spec_section(world, capsys, tmp_path):
    assert rec.main(["start", "--kind", "spec", SPEC]) == 0
    attempt = pathlib.Path(capsys.readouterr().out.strip())
    doc = _doc(tmp_path)
    _round_raw(
        attempt,
        1,
        [
            {"severity": "MAJOR", "cat": "x", "spec_section": "§1", "problem": "een"},
            {"severity": "MAJOR", "cat": "x", "spec_section": "§2", "problem": "twee"},
        ],
    )
    _sidecar(attempt, 1, [_row("r1-f1", "defer"), _row("r1-f2", "accept")])
    assert _accept(attempt, doc) == 0
    out = capsys.readouterr().out
    assert "- **r1-f2** (MAJOR, §2): twee" in out
    assert "r1-f1" not in out


def test_accept_open_section_falls_back_to_a_question_mark(world, capsys, tmp_path):
    attempt = _start(capsys)
    doc = _doc(tmp_path)
    _round_raw(attempt, 1, [{"severity": "BLOCKER", "cat": "x", "problem": "p"}])
    assert _accept(attempt, doc) == 0
    assert "- **r1-f1** (BLOCKER, ?): p" in capsys.readouterr().out


def test_accept_open_malformed_sidecar_exits_4(world, capsys, tmp_path):
    attempt = _start(capsys)
    doc = _doc(tmp_path)
    _round(attempt, 1, ["MAJOR"])
    sidecar = attempt / "round-1.decisions.json"
    sidecar.write_text("{}")
    assert _accept(attempt, doc) == 4
    assert capsys.readouterr().err.startswith("ACCEPT-OPEN FAILED:")
    assert sidecar.read_text() == "{}"
    assert doc.read_text() == "# Plan\n\nTekst.\n"


def test_accept_open_missing_doc_exits_4_and_leaves_the_sidecar(
    world, capsys, tmp_path
):
    attempt = _start(capsys)
    _round(attempt, 1, ["MAJOR"])
    code = _accept(attempt, tmp_path / "bestaat-niet.md")
    assert code == 4
    assert capsys.readouterr().err.startswith("ACCEPT-OPEN FAILED:")
    assert not (attempt / "round-1.decisions.json").exists()


def test_accept_open_runs_off_the_beelink(world, capsys, tmp_path, monkeypatch):
    attempt = _start(capsys)
    doc = _doc(tmp_path)
    _round(attempt, 1, ["MAJOR"])
    monkeypatch.setattr(
        rec, "host_refusal", lambda kind: f"{kind.upper()}-REVIEW NOT RECORDED: test"
    )
    assert _accept(attempt, doc) == 0


def test_accept_open_requires_doc(world, capsys):
    attempt = _start(capsys)
    with pytest.raises(SystemExit) as exc:
        rec.main(["accept-open", str(attempt)])
    assert exc.value.code == 2


def test_accept_open_then_write_round_limit_succeeds(world, capsys, tmp_path):
    attempt = _start(capsys)
    doc = _doc(tmp_path)
    _three_rounds(attempt, "accept")
    assert _write_round_limit(attempt, "--spec", SPEC) == 4
    capsys.readouterr()
    assert _accept(attempt, doc) == 0
    capsys.readouterr()
    code = _write_round_limit(attempt, "--spec", SPEC)
    assert code == 0, capsys.readouterr().err
    out = pathlib.Path(capsys.readouterr().out.strip())
    record = json.loads((out / "record.json").read_text())
    assert [(o["finding_id"], o["decision"]) for o in record["last_round_open"]] == [
        ("r3-f1", "accepted_open")
    ]


# --- pr-open-points (#1352) --------------------------------------------------
BLOCK = "## Open na review\n\n- **r3-f1** (MAJOR, §1): eerste\n- **r3-f2** (MAJOR, §2): tweede\n"


def _write(pad: pathlib.Path, text: str) -> pathlib.Path:
    pad.parent.mkdir(parents=True, exist_ok=True)
    pad.write_text(text)
    return pad


def _pr_open(repo: pathlib.Path, *docs: pathlib.Path) -> int:
    return rec._run_pr_open_points(list(docs), repo)


def test_pr_open_points_copies_the_block(tmp_path, capsys):
    plan = _write(tmp_path / "plan.md", "# Plan\n\ntekst\n\n" + BLOCK)
    assert _pr_open(tmp_path, plan) == 0
    out = capsys.readouterr().out
    assert out.startswith("## Open review points\n\nFrom plan.md:\n- **r3-f1**")
    assert "- **r3-f2** (MAJOR, §2): tweede\n" in out
    assert "## Open na review" not in out


def test_pr_open_points_ignores_a_fenced_heading(tmp_path, capsys):
    doc = _write(tmp_path / "plan.md", f"# Plan\n\n```markdown\n{BLOCK}```\n")
    assert _pr_open(tmp_path, doc) == 0
    assert capsys.readouterr().out == ""


def test_pr_open_points_several_docs_and_missing_path(tmp_path, capsys):
    spec = _write(tmp_path / "spec.md", "# Spec\n\n" + BLOCK)
    plan = _write(tmp_path / "plan.md", "# Plan\n\n" + BLOCK)
    assert _pr_open(tmp_path, spec, tmp_path / "weg.md", plan) == 0
    out = capsys.readouterr().out
    assert out.index("From spec.md:") < out.index("From plan.md:")
    assert out.count("From ") == 2


def test_pr_open_points_without_blocks_is_empty(tmp_path, capsys):
    a = _write(tmp_path / "a.md", "# A\n")
    b = _write(tmp_path / "b.md", "# B\n\n## Iets\n\n- punt\n")
    assert _pr_open(tmp_path, a, b) == 0
    assert capsys.readouterr().out == ""


def test_pr_open_points_undecodable_file_is_exit_4(tmp_path, capsys):
    binary = tmp_path / "plaatje.png"
    binary.write_bytes(b"\x89PNG\xff\xfe")
    assert _pr_open(tmp_path, binary) == 4
    assert "PR-OPEN-POINTS FAILED" in capsys.readouterr().err


def test_pr_open_points_directory_is_exit_4(tmp_path, capsys):
    assert _pr_open(tmp_path, tmp_path) == 4
    assert "PR-OPEN-POINTS FAILED" in capsys.readouterr().err


def test_pr_open_points_cli_runs_without_beelink(monkeypatch, capsys, tmp_path):
    monkeypatch.setattr(
        rec, "host_refusal", lambda kind: f"{kind.upper()}-REVIEW NOT RECORDED: test"
    )
    pad = _write(tmp_path / "plan.md", "# Plan\n\n## Iets\n\n- punt\n")
    assert rec.main(["pr-open-points", str(pad), str(tmp_path / "weg.md")]) == 0
    assert capsys.readouterr().out == ""


@pytest.mark.parametrize(
    "spec_line",
    [
        "**Spec:** [ontwerp](../specs/x.md)",
        "**Spec:** `/home/u/repo/.claude/worktrees/w/docs/developer/planning/specs/x.md`",
        "**Spec:** https://github.com/o/r/blob/b/docs/developer/planning/specs/x.md",
        "**Spec:** `docs/developer/planning/specs/x.md`",
    ],
)
def test_pr_open_points_follows_the_spec_line(tmp_path, capsys, spec_line):
    plan = _write(
        tmp_path / "docs/developer/planning/plans/p.md", f"# P\n\n{spec_line}\n"
    )
    _write(tmp_path / "docs/developer/planning/specs/x.md", "# S\n\n" + BLOCK)
    assert _pr_open(tmp_path, plan) == 0
    out = capsys.readouterr().out
    assert out.count("From docs/developer/planning/specs/x.md:") == 1


def test_pr_open_points_spec_follows_its_plan_and_dedups(tmp_path, capsys):
    plan = _write(
        tmp_path / "docs/developer/planning/plans/p.md",
        "**Spec:** [s](../specs/x.md)\n\n" + BLOCK,
    )
    spec = _write(tmp_path / "docs/developer/planning/specs/x.md", "# S\n\n" + BLOCK)
    other = _write(tmp_path / "o.md", "# O\n\n" + BLOCK)
    assert _pr_open(tmp_path, plan, other, spec) == 0
    out = capsys.readouterr().out
    assert out.count("From docs/developer/planning/specs/x.md:") == 1
    assert out.index("plans/p.md:") < out.index("specs/x.md:") < out.index("From o.md:")


def test_pr_open_points_missing_spec_is_skipped(tmp_path, capsys):
    plan = _write(
        tmp_path / "docs/developer/planning/plans/p.md",
        "**Spec:** `docs/developer/planning/specs/weg.md`\n",
    )
    assert _pr_open(tmp_path, plan) == 0
    assert capsys.readouterr().out == ""


# --- lighthouse: data directory, skill directories, language ----------------


def test_data_dir_from_env(world, capsys, monkeypatch):
    """`LIGHTHOUSE_DATA` wins; without an override under `lighthouse/review-records/`."""
    attempt = _start(capsys)
    chain = world["tmp"] / "data" / "plan-review" / "2026-09-25-voorbeeld"
    assert (chain / f".{attempt.name}.tmp" / "attempt.json").is_file()
    assert not (world["tmp"] / "home" / ".local" / "share" / "lekmeting-keten").exists()

    monkeypatch.delenv("LIGHTHOUSE_DATA")
    monkeypatch.delenv("XDG_DATA_HOME", raising=False)
    attempt = _start(capsys)
    chain = (
        world["tmp"]
        / "home"
        / ".local"
        / "share"
        / "lighthouse"
        / "review-records"
        / "plan-review"
        / "2026-09-25-voorbeeld"
    )
    assert (chain / f".{attempt.name}.tmp" / "attempt.json").is_file()


def test_skill_dirs_are_lighthouse():
    assert rec.DEFAULT_SKILL_DIRS == {
        "plan": ".claude/skills/lighthouse-plan-adversarial-review",
        "spec": ".claude/skills/lighthouse-spec-adversarial-review",
    }


@pytest.mark.parametrize(
    ("language", "heading", "sentence"),
    [
        ("nl", "## Open na review", "Ronde 3 haalde de rondegrens"),
        ("en", "## Open after review", "Round 3 reached the round limit"),
    ],
)
def test_open_after_review_heading_follows_language(config, language, heading, sentence):
    config.write_text(f'[project]\nlanguage = "{language}"\n')
    point = {"finding_id": "r3-f1", "severity": "MAJOR", "section": "§1", "problem": "p"}
    block = rec.render_open(3, [point])
    assert block.startswith(f"{heading}\n\n{sentence}")
    assert "- **r3-f1** (MAJOR, §1): p" in block
    # `open_section` finds the block again in every language.
    assert rec.open_section(f"# Plan\n\n{block}") is not None
