"""Tests voor scripts/review_pair.py — de gepaarde ronde-1-schaduwrun (#988).

Plan: docs/developer/planning/plans/2026-09-27-codex-fallback-meting.md, Taak 3–4.
Getoetst: `blind` (A/B-tabel zonder bron, idempotente loting, werkboomcheck,
schemacheck met de stdlib, escaping), `merge` (ontblinden, dubbelen samenvoegen,
de canonieke `round-1.json` plus de twee sidecars, en de validatie van de blinde
beslissingen). Geen netwerk: `_coin` en `_tree_state` zijn gepatcht, behalve in de
ene test die `_tree_state` tegen een wegwerp-repo draait. De uitknop `status` is weg
sinds het besluit van 2026-10-04 (#988).

In lighthouse: het script draait uit de geïnstalleerde indeling (`install_core`);
`tally` en zijn tests blijven in paia. De findings-schema's komen uit `fixtures/`
(letterlijk uit paia) tot de review-skills in de pack staan.
"""

from __future__ import annotations

import importlib.util
import json
import pathlib
import shutil
import subprocess

import pytest

from tests.pack._testkit import INSTALL_ROOT

SCRIPTS = INSTALL_ROOT / ".lighthouse" / "scripts"
FIXTURES = pathlib.Path(__file__).resolve().parent / "fixtures"


def _load(name: str) -> object:
    spec = importlib.util.spec_from_file_location(name, SCRIPTS / f"{name}.py")
    assert spec and spec.loader
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


rp = _load("review_pair")
rec = _load("plan_review_record")

PLAN_SCHEMA = FIXTURES / "plan-findings.schema.json"
SPEC_SCHEMA = FIXTURES / "spec-findings.schema.json"


@pytest.fixture(autouse=True)
def root(tmp_path, monkeypatch) -> pathlib.Path:
    """Een repo-wortel met de findings-schema's waar `DEFAULT_SKILL_DIRS` ze zoekt."""
    root = tmp_path / "root"
    for kind, schema in (("plan", PLAN_SCHEMA), ("spec", SPEC_SCHEMA)):
        skill = root / rec.DEFAULT_SKILL_DIRS[kind]
        skill.mkdir(parents=True)
        shutil.copyfile(schema, skill / "findings.schema.json")
    monkeypatch.setenv("LIGHTHOUSE_REPO_ROOT", str(root))
    return root


def _plan_finding(severity: str, problem: str, **overrides: str) -> dict:
    finding = {
        "severity": severity,
        "cat": "missing_artifact",
        "plan_section": "Task 1",
        "problem": problem,
        "evidence": f"scripts/x.py:1 {problem}",
        "failure_scenario": f"faalt omdat {problem}",
        "proposed_fix": f"los {problem} op",
    }
    finding.update(overrides)
    return finding


def _spec_finding(severity: str, problem: str) -> dict:
    return {
        "severity": severity,
        "spec_section": "Sectie 2",
        "problem": problem,
        "evidence": f"docs/x.md:3 {problem}",
        "failure_scenario": f"faalt omdat {problem}",
        "proposed_fix": f"los {problem} op",
    }


def _raw(name: str, findings: list[dict]) -> dict:
    return {
        "review_markdown": f"# review {name}",
        "status": "Majors only",
        "findings": findings,
        "top3": [f"top {name}"],
        "verdict": f"verdict {name}",
        "could_not_verify": ["gedeeld", f"alleen {name}"],
    }


def _make_attempt(
    root: pathlib.Path, kind: str, codex: list[dict], fallback: list[dict]
) -> pathlib.Path:
    root.mkdir(parents=True)
    (root / "attempt.json").write_text(
        json.dumps({"kind": kind, "reviewed_sha": "abc123"})
    )
    (root / "round-1.codex.json").write_text(json.dumps(_raw("c", codex)))
    (root / "round-1.fallback.json").write_text(json.dumps(_raw("f", fallback)))
    return root


@pytest.fixture
def patched(monkeypatch):
    monkeypatch.setattr(rp, "_coin", lambda: True)
    monkeypatch.setattr(rp, "_tree_state", lambda: ("abc123", True))


@pytest.fixture
def attempt(tmp_path, patched) -> pathlib.Path:
    """Codex `[MAJOR "x ontbreekt", MINOR]`, fallback `[MAJOR "zelfde x", BLOCKER "y"]`."""
    return _make_attempt(
        tmp_path / "attempt",
        "plan",
        [_plan_finding("MAJOR", "x ontbreekt"), _plan_finding("MINOR", "typo")],
        [_plan_finding("MAJOR", "zelfde x"), _plan_finding("BLOCKER", "y")],
    )


def _decide(attempt_dir: pathlib.Path, rows: list[dict]) -> None:
    (attempt_dir / "round-1.blind-decisions.json").write_text(json.dumps(rows))


def _row(fid: str, decision: str, also: str | None = None) -> dict:
    row = {"finding_id": fid, "decision": decision, "reason": f"reden {fid}"}
    if also:
        row["also"] = also
    return row


def _blind(attempt_dir: pathlib.Path, capsys) -> str:
    assert rp.main(["blind", str(attempt_dir)]) == 0
    return capsys.readouterr().out


def _table_row(table: str, blind_id: str) -> str:
    return next(
        line for line in table.splitlines() if line.startswith(f"| {blind_id} |")
    )


def _unescaped_pipes(line: str) -> int:
    count, escaped = 0, False
    for ch in line:
        if escaped:
            escaped = False
        elif ch == "\\":
            escaped = True
        elif ch == "|":
            count += 1
    return count


FOLD = [
    _row("A-f1", "accept", also="B-f1"),
    _row("A-f2", "minor_no"),
    _row("B-f2", "accept"),
]


def test_blind_labels_without_revealing_the_reviewer(attempt, capsys):
    out = _blind(attempt, capsys)
    assert json.loads((attempt / "pair.json").read_text()) == {
        "A": "codex",
        "B": "fallback",
    }
    assert (attempt / "round-1.blind.md").read_text() == out
    assert "A-f1" in out
    assert "B-f2" in out
    assert "codex" not in out.lower()
    assert "fallback" not in out.lower()
    header = out.splitlines()[0]
    assert "Failure" in header
    assert "faalt omdat x ontbreekt" in _table_row(out, "A-f1")


def test_blind_sorts_on_severity_then_id(attempt, capsys):
    out = _blind(attempt, capsys)
    ids = [line.split("|")[1].strip() for line in out.splitlines()[2:]]
    assert ids == ["B-f2", "A-f1", "B-f1", "A-f2"]


@pytest.mark.parametrize(
    "state", [("def456", True), ("abc123", False)], ids=["head-moved", "dirty"]
)
def test_blind_refuses_a_changed_tree(attempt, capsys, monkeypatch, state):
    monkeypatch.setattr(rp, "_tree_state", lambda: state)
    assert rp.main(["blind", str(attempt)]) == 4
    err = capsys.readouterr().err
    assert "work tree changed since start or not clean" in err
    assert not (attempt / "pair.json").exists()
    assert not (attempt / "round-1.blind.md").exists()


def test_tree_state_sees_untracked_files(tmp_path):
    repo = tmp_path / "repo"
    repo.mkdir()

    def git(*args: str) -> str:
        return subprocess.run(
            [
                "git",
                "-C",
                str(repo),
                "-c",
                "user.name=t",
                "-c",
                "user.email=t@t",
                "-c",
                "commit.gpgsign=false",
                *args,
            ],
            capture_output=True,
            text=True,
            check=True,
        ).stdout.strip()

    git("init", "-q")
    (repo / "a.txt").write_text("a\n")
    git("add", "a.txt")
    git("commit", "-qm", "init")
    sha = git("rev-parse", "HEAD")
    assert rp._tree_state(repo) == (sha, True)
    (repo / "nieuw.txt").write_text("ongevolgd\n")
    assert rp._tree_state(repo) == (sha, False)


def test_blind_is_idempotent(attempt, capsys, monkeypatch):
    _blind(attempt, capsys)
    before = (attempt / "pair.json").read_text()
    monkeypatch.setattr(rp, "_coin", lambda: False)
    _blind(attempt, capsys)
    assert (attempt / "pair.json").read_text() == before


def test_blind_with_codex_as_b(attempt, capsys, monkeypatch):
    monkeypatch.setattr(rp, "_coin", lambda: False)
    out = _blind(attempt, capsys)
    assert json.loads((attempt / "pair.json").read_text()) == {
        "A": "fallback",
        "B": "codex",
    }
    assert "faalt omdat x ontbreekt" in _table_row(out, "B-f1")


def test_merge_folds_a_duplicate_into_one_finding(attempt, capsys):
    _blind(attempt, capsys)
    _decide(attempt, FOLD)
    assert rp.main(["merge", str(attempt)]) == 0
    out = capsys.readouterr().out
    merged = json.loads((attempt / "round-1.json").read_text())
    sources = json.loads((attempt / "round-1.sources.json").read_text())
    decisions = json.loads((attempt / "round-1.decisions.json").read_text())

    assert len(merged["findings"]) == 3
    assert [s["source"] for s in sources] == ["both", "codex", "fallback"]
    assert sources[0] == {
        "finding_id": "r1-f1",
        "source": "both",
        "blind_ids": ["A-f1", "B-f1"],
        "severity_by_source": {"codex": "MAJOR", "fallback": "MAJOR"},
    }
    assert sources[1]["severity_by_source"] == {"codex": "MINOR"}
    assert sources[2]["severity_by_source"] == {"fallback": "BLOCKER"}
    assert merged["findings"][0]["problem"] == "x ontbreekt"
    assert merged["findings"][2]["severity"] == "BLOCKER"
    assert merged["status"] == "Blockers found"
    assert (
        merged["review_markdown"]
        == "## Codex\n\n# review c\n\n## Fallback\n\n# review f"
    )
    assert merged["top3"] == ["top c"]
    assert merged["verdict"] == "verdict c"
    assert merged["could_not_verify"] == ["gedeeld", "alleen c", "alleen f"]
    assert [d["finding_id"] for d in decisions] == ["r1-f1", "r1-f2", "r1-f3"]
    assert [d["decision"] for d in decisions] == ["accept", "minor_no", "accept"]
    # De sidecars passen op de lezers in plan_review_record (Taak 2).
    assert set(rec._decisions(attempt, 1, 3, "plan")) == {"r1-f1", "r1-f2", "r1-f3"}
    assert rec._sources(attempt, 3)["r1-f1"]["source"] == "both"
    rows = [line for line in out.splitlines() if line.startswith("| r1-f")]
    assert len(rows) == 3
    assert "| r1-f1 | both | MAJOR | accept | reden A-f1 |" in rows


def test_merge_takes_the_heavier_severity(attempt, capsys):
    _blind(attempt, capsys)
    _decide(
        attempt,
        [
            _row("A-f2", "accept", also="B-f1"),
            _row("A-f1", "accept"),
            _row("B-f2", "accept"),
        ],
    )
    assert rp.main(["merge", str(attempt)]) == 0
    merged = json.loads((attempt / "round-1.json").read_text())
    sources = json.loads((attempt / "round-1.sources.json").read_text())
    assert merged["findings"][1]["severity"] == "MAJOR"
    assert merged["findings"][1]["problem"] == "typo"
    assert sources[1]["severity_by_source"] == {"codex": "MINOR", "fallback": "MAJOR"}


def test_merged_round_is_schema_valid(attempt, capsys):
    jsonschema = pytest.importorskip("jsonschema")
    _blind(attempt, capsys)
    _decide(attempt, FOLD)
    assert rp.main(["merge", str(attempt)]) == 0
    jsonschema.validate(
        json.loads((attempt / "round-1.json").read_text()),
        json.loads(PLAN_SCHEMA.read_text()),
    )


def test_spec_kind_blind_and_merge(tmp_path, patched, capsys):
    jsonschema = pytest.importorskip("jsonschema")
    spec_attempt = _make_attempt(
        tmp_path / "spec",
        "spec",
        [_spec_finding("MAJOR", "spec x")],
        [_spec_finding("MINOR", "spec y")],
    )
    out = _blind(spec_attempt, capsys)
    assert _table_row(out, "A-f1").split("|")[3].strip() == "-"
    _decide(spec_attempt, [_row("A-f1", "defer"), _row("B-f1", "minor_yes")])
    assert rp.main(["merge", str(spec_attempt)]) == 0
    capsys.readouterr()
    jsonschema.validate(
        json.loads((spec_attempt / "round-1.json").read_text()),
        json.loads(SPEC_SCHEMA.read_text()),
    )

    with_cat = {**_spec_finding("MAJOR", "spec z"), "cat": "repo_falsehood"}
    bad = _make_attempt(
        tmp_path / "spec-cat", "spec", [_spec_finding("MAJOR", "a")], [with_cat]
    )
    assert rp.main(["blind", str(bad)]) == 4
    assert "round-1.fallback.json" in capsys.readouterr().err
    assert not (bad / "round-1.blind.md").exists()


@pytest.mark.parametrize(
    "rows",
    [
        [
            _row("A-f1", "accept", also="B-f1"),
            _row("A-f2", "minor_no"),
            _row("B-f1", "accept"),
            _row("B-f2", "accept"),
        ],
        [
            _row("A-f1", "accept", also="A-f2"),
            _row("B-f1", "accept"),
            _row("B-f2", "accept"),
        ],
        [_row("A-f1", "accept", also="B-f1"), _row("A-f2", "minor_no")],
        [
            _row("A-f1", "accepted_open", also="B-f1"),
            _row("A-f2", "minor_no"),
            _row("B-f2", "accept"),
        ],
        [
            _row("A-f1", "defer", also="B-f1"),
            _row("A-f2", "minor_no"),
            _row("B-f2", "accept"),
        ],
    ],
    ids=[
        "double-cover",
        "also-same-letter",
        "uncovered",
        "accepted-open",
        "defer-in-plan",
    ],
)
def test_merge_rejects_double_cover(attempt, capsys, rows):
    _blind(attempt, capsys)
    _decide(attempt, rows)
    assert rp.main(["merge", str(attempt)]) == 4
    assert "SHADOW-PAIR FAILED" in capsys.readouterr().err
    for name in ("round-1.json", "round-1.decisions.json", "round-1.sources.json"):
        assert not (attempt / name).exists()


def _drop_findings(doc: dict) -> None:
    del doc["findings"]


def _drop_evidence(doc: dict) -> None:
    del doc["findings"][0]["evidence"]


def _critical(doc: dict) -> None:
    doc["findings"][0]["severity"] = "CRITICAL"


def _number_in_could_not_verify(doc: dict) -> None:
    doc["could_not_verify"].append(7)


@pytest.mark.parametrize(
    "breakage",
    [_drop_findings, _drop_evidence, _critical, _number_in_could_not_verify],
    ids=["no-findings", "no-evidence", "critical", "non-string-item"],
)
def test_blind_rejects_a_malformed_raw_file(attempt, capsys, breakage):
    path = attempt / "round-1.fallback.json"
    doc = json.loads(path.read_text())
    breakage(doc)
    path.write_text(json.dumps(doc))
    assert rp.main(["blind", str(attempt)]) == 4
    assert "round-1.fallback.json" in capsys.readouterr().err
    assert not (attempt / "round-1.blind.md").exists()
    assert not (attempt / "pair.json").exists()


def test_blind_rejects_invalid_json_and_a_missing_file(attempt, capsys):
    (attempt / "round-1.codex.json").write_text("{niet json")
    assert rp.main(["blind", str(attempt)]) == 4
    assert "round-1.codex.json" in capsys.readouterr().err
    (attempt / "round-1.codex.json").unlink()
    assert rp.main(["blind", str(attempt)]) == 4
    assert "round-1.codex.json" in capsys.readouterr().err


def test_blind_escapes_pipes_and_newlines(attempt, capsys):
    path = attempt / "round-1.codex.json"
    doc = json.loads(path.read_text())
    doc["findings"][0]["evidence"] = "a | b\nc"
    path.write_text(json.dumps(doc))
    out = _blind(attempt, capsys)
    assert "a \\| b c" in _table_row(out, "A-f1")
    for line in out.splitlines():
        assert _unescaped_pipes(line) == 9, line
    assert rp._cell("x\\|y\r\nz") == "x\\\\\\|y z"


def test_merge_mixed_severity_uses_the_heavier_vocabulary(attempt, capsys):
    _blind(attempt, capsys)
    _decide(
        attempt,
        [
            _row("A-f2", "minor_no", also="B-f1"),
            _row("A-f1", "accept"),
            _row("B-f2", "accept"),
        ],
    )
    assert rp.main(["merge", str(attempt)]) == 4
    assert "SHADOW-PAIR FAILED" in capsys.readouterr().err
    assert not (attempt / "round-1.json").exists()
    _decide(
        attempt,
        [
            _row("A-f2", "accept", also="B-f1"),
            _row("A-f1", "accept"),
            _row("B-f2", "accept"),
        ],
    )
    assert rp.main(["merge", str(attempt)]) == 0


def test_merge_writes_no_sidecar_when_building_the_round_fails(
    attempt, capsys, monkeypatch
):
    _blind(attempt, capsys)
    _decide(attempt, FOLD)

    def boom(*_args: object) -> dict:
        raise TypeError("kapot")

    monkeypatch.setattr(rp, "_merged_round", boom)
    assert rp.main(["merge", str(attempt)]) == 4
    assert "SHADOW-PAIR FAILED: kapot" in capsys.readouterr().err
    for name in ("round-1.json", "round-1.decisions.json", "round-1.sources.json"):
        assert not (attempt / name).exists()


def test_merge_does_not_print_the_mapping_of_a_corrupt_pair(attempt, capsys):
    _blind(attempt, capsys)
    _decide(attempt, FOLD)
    (attempt / "pair.json").write_text(json.dumps({"A": "codex", "B": "codex"}))
    assert rp.main(["merge", str(attempt)]) == 4
    err = capsys.readouterr().err
    assert "pair.json invalid" in err
    assert "codex" not in err


def test_merge_without_blind_fails(attempt, capsys):
    _decide(attempt, FOLD)
    assert rp.main(["merge", str(attempt)]) == 4
    assert "pair.json" in capsys.readouterr().err
    assert not (attempt / "round-1.json").exists()


def test_usage_error_exits_2():
    with pytest.raises(SystemExit) as exc:
        rp.main(["onbekend"])
    assert exc.value.code == 2


