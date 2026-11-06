"""ci/loop_roadmap.py: the planned items of the loop map and the open `loop` issues match."""

import importlib.util
import json
import subprocess
from pathlib import Path

PATH = Path(__file__).resolve().parents[1] / "ci" / "loop_roadmap.py"
_spec = importlib.util.spec_from_file_location("lighthouse_ci_loop_roadmap", PATH)
assert _spec is not None and _spec.loader is not None
roadmap = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(roadmap)


def chain(*items: dict, handoffs: tuple[dict, ...] = ()) -> dict:
    return {"chips": list(items), "handoffs": list(handoffs)}


def test_matching_map_and_issues_have_no_problems():
    data = chain({"id": "a", "status": "works"}, {"id": "b", "status": "planned", "issue": 7})
    assert roadmap.problems(data, {7}) == []


def test_planned_item_without_issue_is_flagged():
    data = chain(handoffs=({"from": "a", "to": "b", "status": "planned", "milestone": "M2"},))
    assert roadmap.problems(data, set()) == ["a->b: planned without an issue"]


def test_planned_item_on_a_closed_or_unlabelled_issue_is_flagged():
    data = chain({"id": "b", "status": "idea", "issue": 7})
    assert roadmap.problems(data, set()) == [
        "b: #7 is not an open issue labelled loop (see docs/developer/labels.md)"
    ]


def test_open_issue_missing_from_the_map_is_not_flagged():
    data = chain({"id": "b", "status": "planned", "issue": 7})
    assert roadmap.problems(data, {7, 9}) == []


MAP = "docs/site/data/loop.json"


def test_pr_closing_a_planned_item_without_update_is_flagged():
    data = chain({"id": "b", "status": "planned", "issue": 7})
    assert roadmap.pr_problems(data, {7}, {MAP}) == [
        "b: still planned, but this PR closes #7; set status works and drop issue "
        "(see docs/developer/labels.md)"
    ]


def test_pr_closing_without_touching_the_map_is_flagged():
    data = chain({"id": "b", "status": "works"})
    assert roadmap.pr_problems(data, {7}, {"ci/x.py"}) == [
        "#7: this PR closes a loop issue but leaves docs/site/data/loop.json untouched "
        "(see docs/developer/labels.md)"
    ]


def test_pr_flags_only_the_issue_left_planned():
    data = chain({"id": "a", "status": "works"}, {"id": "b", "status": "planned", "issue": 8})
    assert roadmap.pr_problems(data, {7, 8}, {MAP}) == [
        "b: still planned, but this PR closes #8; set status works and drop issue "
        "(see docs/developer/labels.md)"
    ]


def test_pr_new_step_added_as_works_is_clean():
    assert roadmap.pr_problems(chain({"id": "new", "status": "works"}), {7}, {MAP}) == []


def test_pr_ignores_issues_without_the_label():
    # closed holds only loop-labelled issues; a PR closing none is clean whatever it touches.
    assert roadmap.pr_problems(chain({"id": "b", "status": "planned", "issue": 7}), set(), set()) == []


def _pr_mode(monkeypatch, tmp_path, data, closed, touched):
    path = tmp_path / "loop.json"
    path.write_text(json.dumps(data), encoding="utf-8")
    monkeypatch.setattr(roadmap, "LOOP", path)
    monkeypatch.setattr(roadmap, "closed_loop_issues", lambda *_a: closed)
    monkeypatch.setattr(roadmap, "touched_files", touched)


def test_pr_mode_gh_failure_could_not_check(monkeypatch, tmp_path, capsys):
    def fail(*_a, **_k):
        raise subprocess.CalledProcessError(1, "gh")
    _pr_mode(monkeypatch, tmp_path, chain({"id": "b", "status": "works"}), set(), None)
    monkeypatch.setattr(roadmap, "closed_loop_issues", fail)
    assert roadmap.main(["--pr", "5"]) == 2
    assert "COULD NOT CHECK" in capsys.readouterr().err


def _no_open_issues(*_a):
    raise AssertionError("open_loop_issues called for a PR that touches neither the map nor the script")


def test_pr_mode_closing_no_loop_issue_and_not_touching_the_map_is_green(monkeypatch, tmp_path):
    _pr_mode(monkeypatch, tmp_path, chain({"id": "b", "status": "planned", "issue": 7}), set(),
             lambda *_a: {"ci/x.py"})
    monkeypatch.setattr(roadmap, "open_loop_issues", _no_open_issues)
    assert roadmap.main(["--pr", "5"]) == 0


def test_pr_mode_touching_the_map_also_runs_the_main_check(monkeypatch, tmp_path, capsys):
    data = chain({"id": "b", "status": "planned", "issue": 7})
    _pr_mode(monkeypatch, tmp_path, data, set(), lambda *_a: {MAP})
    monkeypatch.setattr(roadmap, "open_loop_issues", lambda *_a: {9})
    assert roadmap.main(["--pr", "5"]) == 1
    assert "b: #7 is not an open issue labelled loop" in capsys.readouterr().out


def test_pr_mode_touching_the_script_also_runs_the_main_check(monkeypatch, tmp_path, capsys):
    data = chain({"id": "b", "status": "planned", "issue": 7})
    _pr_mode(monkeypatch, tmp_path, data, set(), lambda *_a: {"ci/loop_roadmap.py"})
    monkeypatch.setattr(roadmap, "open_loop_issues", lambda *_a: {9})
    assert roadmap.main(["--pr", "5"]) == 1
    assert "b: #7 is not an open issue labelled loop" in capsys.readouterr().out


def test_pr_mode_verdict_exits_1(monkeypatch, tmp_path, capsys):
    data = chain({"id": "b", "status": "planned", "issue": 7})
    _pr_mode(monkeypatch, tmp_path, data, {7}, lambda *_a: {MAP})
    assert roadmap.main(["--pr", "5"]) == 1
    assert "b: still planned, but this PR closes #7" in capsys.readouterr().out


def test_pr_mode_clean_exits_0(monkeypatch, tmp_path, capsys):
    _pr_mode(monkeypatch, tmp_path, chain({"id": "b", "status": "works"}), {7}, lambda *_a: {MAP})
    assert roadmap.main(["--pr", "5"]) == 0
    assert "map in step" in capsys.readouterr().out


def test_closed_loop_issues_keeps_only_the_label(monkeypatch):
    reply = {"data": {"repository": {"pullRequest": {"closingIssuesReferences": {"nodes": [
        {"number": 7, "repository": {"nameWithOwner": "O/R"},
         "labels": {"nodes": [{"name": "loop"}, {"name": "bug"}]}},
        {"number": 8, "repository": {"nameWithOwner": "o/r"}, "labels": {"nodes": [{"name": "bug"}]}},
        {"number": 9, "repository": {"nameWithOwner": "other/repo"}, "labels": {"nodes": [{"name": "loop"}]}},
    ]}}}}}

    calls = []

    def fake_run(args, **_k):
        calls.append(args)
        return subprocess.CompletedProcess([], 0, stdout=json.dumps(reply))
    monkeypatch.setattr(roadmap.subprocess, "run", fake_run)
    assert roadmap.closed_loop_issues("o/r", 5) == {7}
    args = calls[0]
    assert args[args.index("pr=5") - 1] == "-F"
    assert args[args.index("owner=o") - 1] == "-f"
    assert args[args.index("name=r") - 1] == "-f"


def test_null_pull_request_reply_could_not_check(monkeypatch, tmp_path, capsys):
    reply = {"data": {"repository": {"pullRequest": None}}}
    path = tmp_path / "loop.json"
    path.write_text(json.dumps(chain({"id": "b", "status": "works"})), encoding="utf-8")
    monkeypatch.setattr(roadmap, "LOOP", path)
    monkeypatch.setattr(
        roadmap.subprocess, "run",
        lambda *_a, **_k: subprocess.CompletedProcess([], 0, stdout=json.dumps(reply)),
    )
    assert roadmap.main(["--pr", "5"]) == 2
    assert "COULD NOT CHECK" in capsys.readouterr().err


def test_touched_files_keeps_names_with_spaces(monkeypatch):
    monkeypatch.setattr(
        roadmap.subprocess, "run",
        lambda *_a, **_k: subprocess.CompletedProcess([], 0, stdout="a b.md\n\nc.py\n"),
    )
    assert roadmap.touched_files("o/r", 5) == {"a b.md", "c.py"}
