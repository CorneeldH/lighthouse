"""Tests voor scripts/review_pair.py — de gepaarde ronde-1-schaduwrun (#988).

Plan: docs/developer/planning/plans/2026-09-27-codex-fallback-meting.md, Taak 3–4.
Getoetst: `blind` (A/B-tabel zonder bron, idempotente loting, werkboomcheck,
schemacheck met de stdlib, escaping), `merge` (ontblinden, dubbelen samenvoegen,
de canonieke `round-1.json` plus de twee sidecars, en de validatie van de blinde
beslissingen), en `paired_records`/`tally`/`verdict` (Taak 4). Geen netwerk: `_coin`
en `_tree_state` zijn gepatcht, behalve in de ene test die `_tree_state` tegen een
wegwerp-repo draait. Geen test leest de echte datamap: `LEKMETING_DATA` wijst naar
`tmp_path`. De uitknop `status` is weg sinds het besluit van 2026-10-04 (#988).
"""

from __future__ import annotations

import importlib.util
import json
import pathlib
import subprocess

import pytest

REPO = pathlib.Path(__file__).resolve().parents[2]


def _load(name: str) -> object:
    spec = importlib.util.spec_from_file_location(name, REPO / "scripts" / f"{name}.py")
    assert spec and spec.loader
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


rp = _load("review_pair")
rec = _load("plan_review_record")

PLAN_SCHEMA = REPO / ".claude/skills/paia-plan-adversarial-review/findings.schema.json"
SPEC_SCHEMA = REPO / ".claude/skills/paia-spec-adversarial-review/findings.schema.json"


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
    assert "werkboom veranderd sinds start of niet schoon" in err
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
    assert "SCHADUWPAAR MISLUKT" in capsys.readouterr().err
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
    assert "SCHADUWPAAR MISLUKT" in capsys.readouterr().err
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
    assert "SCHADUWPAAR MISLUKT: kapot" in capsys.readouterr().err
    for name in ("round-1.json", "round-1.decisions.json", "round-1.sources.json"):
        assert not (attempt / name).exists()


def test_merge_does_not_print_the_mapping_of_a_corrupt_pair(attempt, capsys):
    _blind(attempt, capsys)
    _decide(attempt, FOLD)
    (attempt / "pair.json").write_text(json.dumps({"A": "codex", "B": "codex"}))
    assert rp.main(["merge", str(attempt)]) == 4
    err = capsys.readouterr().err
    assert "pair.json ongeldig" in err
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


# --- Taak 4: `paired_records`, `tally`, `verdict` ------------------------------------


def _f(
    fid: str,
    source: str | None,
    severity: str,
    decision: str | None,
    own: dict | None = None,
) -> dict:
    """Eén ronde-1-bevinding in recordvorm; `own` is `severity_by_source`."""
    if own is None and source in ("codex", "fallback"):
        own = {source: severity}
    return {
        "finding_id": fid,
        "round": 1,
        "severity": severity,
        "decision": decision,
        "source": source,
        "severity_by_source": own,
    }


def _record(
    root: pathlib.Path,
    kind: str,
    chain: str,
    attempt: str,
    findings: list[dict],
    paired: bool = True,
    outcome: str = "converged",
    **fields: object,
) -> pathlib.Path:
    """Schrijf `<root>/<kind>-review/<chain>/<attempt>/record.json` (v3, gepaard)."""
    record = {
        "schema_version": 3,
        "kind": kind,
        "chain_key": chain,
        "attempt": attempt,
        "review_mode": "codex",
        "prompt_sha256": "p" * 64,
        "rounds": 1,
        "outcome": outcome,
        "paired": paired,
        "shadow": {"model": rp.SHADOW_MODEL, "same_family": True} if paired else None,
        "findings": findings,
    }
    record.update(fields)
    path = root / f"{kind}-review" / chain / attempt / "record.json"
    path.parent.mkdir(parents=True)
    path.write_text(json.dumps(record))
    return path


def _pairs(root: pathlib.Path, n: int) -> None:
    for i in range(n):
        _record(
            root, "plan", f"keten-{i}", "a1", [_f("r1-f1", "both", "MAJOR", "accept")]
        )


@pytest.fixture
def data(tmp_path, monkeypatch) -> pathlib.Path:
    """Een lege datamap via `LEKMETING_DATA`."""
    root = tmp_path / "data"
    root.mkdir()
    monkeypatch.setenv("LEKMETING_DATA", str(root))
    return root


def _tally(root: pathlib.Path, capsys) -> str:
    assert rp.main(["tally", "--data", str(root)]) == 0
    return capsys.readouterr().out


def _line(out: str, start: str) -> str:
    return next(line for line in out.splitlines() if line.startswith(start))


def _spec_pairs(root: pathlib.Path, n: int) -> None:
    for i in range(n):
        _record(
            root, "spec", f"spec-{i}", "a1", [_f("r1-f1", "both", "MAJOR", "accept")]
        )


def test_no_kill_switch_after_the_stop_threshold(data, capsys):
    """Het paar blijft na plan 15/15 en spec 8/8: `status` bestaat niet meer (#988, 4/10)."""
    _pairs(data, 15)
    _spec_pairs(data, 8)
    assert len(rp.paired_records(data)) == 23
    assert not hasattr(rp, "status")
    assert not hasattr(rp, "PAIR_TARGET")
    with pytest.raises(SystemExit):
        rp.main(["status"])
    capsys.readouterr()


def test_paired_records_ignore_unpaired_staging_and_undecided(data):
    _record(data, "plan", "ongepaard", "a1", [], paired=False)
    _record(data, "plan", "staging", ".x.tmp", [_f("r1-f1", "both", "MAJOR", "accept")])
    _record(
        data,
        "plan",
        "onbeslist",
        "a1",
        [_f("r1-f1", "codex", "MAJOR", None)],
        outcome="aborted",
    )
    _record(
        data,
        "spec",
        "sonnet",
        "a1",
        [_f("r1-f1", "both", "MAJOR", "accept")],
        shadow={"model": "claude-sonnet-5", "same_family": True},
    )
    assert rp.paired_records(data) == []


def test_paired_records_and_tally_skip_legacy_records(data, capsys):
    legacy = {
        "finding_id": "r1-f1",
        "round": 1,
        "severity": "MAJOR",
        "decision": "accept",
    }
    for version, chain in ((2, "v2"), (1, "v1")):
        path = data / "plan-review" / chain / "a1" / "record.json"
        path.parent.mkdir(parents=True)
        path.write_text(
            json.dumps(
                {
                    "schema_version": version,
                    "review_mode": "codex",
                    "rounds": 1,
                    "outcome": "converged",
                    "findings": [legacy],
                }
            )
        )
    assert rp.paired_records(data) == []
    out = _tally(data, capsys)
    assert _line(out, "| totaal (context) |").startswith("| totaal (context) | 0 |")


def test_paired_records_count_a_context_suffixed_model(data):
    _record(
        data,
        "plan",
        "k",
        "a1",
        [_f("r1-f1", "both", "MAJOR", "accept")],
        shadow={"model": "claude-opus-5-5[1m]", "same_family": True},
    )
    assert len(rp.paired_records(data)) == 1
    assert rp.canonical_model("claude-opus-5-5[1m]") == "claude-opus-5-5"
    assert rp.canonical_model("claude-opus-5-5") == "claude-opus-5-5"


def test_paired_records_require_post_1066_schema(data):
    _record(
        data,
        "plan",
        "k",
        "a1",
        [_f("r1-f1", "both", "MAJOR", "accept")],
        schema_version=2,
    )
    assert rp.paired_records(data) == []


def _cells(row: str) -> list[str]:
    return [c.strip() for c in row.strip().strip("|").split("|")]


def test_tally_counts_per_reviewer_by_own_severity(tmp_path, capsys):
    path = _record(
        tmp_path,
        "plan",
        "k",
        "a1",
        [
            _f(
                "r1-f1",
                "both",
                "MAJOR",
                "accept",
                {"codex": "MAJOR", "fallback": "MINOR"},
            ),
            _f("r1-f2", "codex", "MAJOR", "accept"),
            _f("r1-f3", "fallback", "BLOCKER", "reject"),
            _f("r1-f4", "fallback", "MAJOR", "spec"),
        ],
    )
    out = _tally(tmp_path, capsys)
    # Groep | Paren | Codex B/M | Fallback B/M | both | codex | fallback | aandeel
    assert _cells(_line(out, "| totaal (context) |")) == [
        "totaal (context)",
        "1",
        "2",
        "1",
        "1",
        "1",
        "1",
        "0.33",
    ]
    assert _cells(_line(out, "| plan |"))[1:] == ["1", "2", "1", "1", "1", "1", "0.33"]
    rel = path.relative_to(tmp_path)
    assert f"{rel}#r1-f2" in out
    assert f"{rel}#r1-f4" in out
    assert f"{rel}#r1-f1" not in out
    assert f"{rel}#r1-f3" not in out
    assert "Verdict plan: te weinig" in out


def test_tally_gives_a_verdict_per_kind(tmp_path, capsys):
    """Plan en spec krijgen elk een eigen verdict; het totaal is alleen context."""
    findings = [_f(f"r1-f{i}", "both", "MAJOR", "accept") for i in range(10)]
    _record(tmp_path, "plan", "p", "a1", findings)
    spec = [_f(f"r1-f{i}", "codex", "MAJOR", "accept") for i in range(10)]
    _record(tmp_path, "spec", "s", "a1", spec)
    out = _tally(tmp_path, capsys)
    assert "Verdict plan: houden" in out
    assert "Verdict spec: terugdraaien" in out
    assert "Verdict:" not in out


def test_tally_counts_defer_as_justified_for_spec_reviews(tmp_path, capsys):
    _record(tmp_path, "spec", "k", "a1", [_f("r1-f1", "codex", "MAJOR", "defer")])
    out = _tally(tmp_path, capsys)
    assert _cells(_line(out, "| spec |"))[1:] == ["1", "1", "0", "0", "1", "0", "1.00"]


def test_tally_verdict_uses_the_unrounded_share(tmp_path, capsys):
    """3/29 = 0,103: getoond als 0.10, maar boven 0,10 en dus `aanscherpen`."""
    findings = [_f(f"r1-f{i}", "codex", "MAJOR", "accept") for i in (1, 2, 3)]
    findings += [_f(f"r1-f{i}", "both", "MAJOR", "accept") for i in range(4, 30)]
    _record(tmp_path, "plan", "k", "a1", findings)
    out = _tally(tmp_path, capsys)
    assert _cells(_line(out, "| plan |"))[4:] == ["26", "3", "0", "0.10"]
    assert "Verdict plan: aanscherpen" in out


def test_tally_fails_on_a_missing_data_dir(tmp_path, capsys):
    missing = tmp_path / "bestaat-niet"
    assert rp.main(["tally", "--data", str(missing)]) == 4
    captured = capsys.readouterr()
    assert captured.err == f"TELLING MISLUKT: datamap bestaat niet: {missing}\n"
    assert captured.out == ""


def test_tally_withholds_verdict_on_mixed_prompts(tmp_path, capsys):
    finding = [_f("r1-f1", "codex", "MAJOR", "accept")]
    _record(tmp_path, "plan", "k1", "a1", finding, prompt_sha256="a" * 64)
    _record(tmp_path, "plan", "k2", "a1", finding, prompt_sha256="b" * 64)
    out = _tally(tmp_path, capsys)
    assert "Verdict plan: gemengd: 2 prompts, geen verdict" in out
    assert _cells(_line(out, f"| plan (prompt {'a' * 12}) |"))[1] == "1"
    assert _cells(_line(out, f"| plan (prompt {'b' * 12}) |"))[1] == "1"
    pairs = out.split("## Context")[0]
    assert "| plan |" not in pairs
    assert "Verdict spec: te weinig" in out


def test_tally_context_splits_on_the_1066_boundary(tmp_path, capsys):
    _record(
        tmp_path,
        "plan",
        "oud",
        "a1",
        [],
        paired=False,
        outcome="round_limit",
        schema_version=1,
        review_mode="fallback",
        rounds=3,
    )
    _record(
        tmp_path,
        "plan",
        "nieuw",
        "a1",
        [],
        paired=False,
        outcome="converged",
        schema_version=2,
        review_mode="codex",
        rounds=2,
    )
    out = _tally(tmp_path, capsys)
    context = out.split("## Context, ongepaard (geen deel van het verdict)")[1]
    rows = [line for line in context.splitlines() if line.startswith("| plan |")]
    assert [_cells(r) for r in rows] == [
        ["plan", "voor #1066", "fallback", "1", "3: 1", "round_limit: 1"],
        ["plan", "na #1066", "codex", "1", "2: 1", "converged: 1"],
    ]
    assert "Verdict plan: te weinig" in out


@pytest.mark.parametrize(
    ("share", "total", "expected"),
    [
        (0.33, 9, "te weinig"),
        (0.10, 10, "houden"),
        (0.30, 10, "aanscherpen"),
        (0.31, 10, "terugdraaien"),
    ],
)
def test_verdict_thresholds(share, total, expected):
    assert rp.verdict(share, total) == expected
