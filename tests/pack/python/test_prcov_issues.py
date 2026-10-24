"""Issue, handoff en board per PR (#1016 taak 5): `prcov_issues.py`.

Ontwerp: docs/developer/planning/specs/2026-09-26-ci-doorlooptijd-design.md
Interfaces: src.controller.board (id's), src.controller.comments (KOP, parse_kop,
parse_handoff, beoordeel_commando).
"""

from __future__ import annotations

import json
import os
import subprocess
import sys
from pathlib import Path

import pytest

# scripts/ is geen geïnstalleerd pakket; zelfde vorm als test_quality_prcov.py:15.
_SCRIPTS = Path(__file__).resolve().parents[2] / "scripts"
if str(_SCRIPTS) not in sys.path:
    sys.path.insert(0, str(_SCRIPTS))

from quality import prcov_issues as pi
from quality.diffcov import DiffCoverage

from src.controller.comments import KOP, beoordeel_commando, parse_handoff, parse_kop
from src.controller.identity import identity

PAD = Path(__file__).resolve().parents[2] / "scripts" / "quality" / "prcov_issues.py"


class FakeGh:
    """Logt elke aanroep (args, env, input) en geeft canned antwoorden terug.

    `queue(key, *antwoorden)` hangt antwoorden achter elkaar aan voor een key; elke
    aanroep met die key pop't het volgende antwoord (het laatste blijft staan zodra de
    rij leeg is). `key(args)` groepeert op de eerste twee woorden van het commando,
    behalve voor `api graphql` (key "graphql") en overige `api ...`-aanroepen (key
    "comments") — genoeg onderscheid voor deze module.
    """

    def __init__(self) -> None:
        self.calls: list[tuple[tuple[str, ...], dict[str, str] | None, str | None]] = []
        self._responses: dict[str, list[str]] = {}

    def queue(self, key: str, *antwoorden: str) -> None:
        self._responses.setdefault(key, []).extend(antwoorden)

    @staticmethod
    def key(args: tuple[str, ...]) -> str:
        if args[0] == "api" and len(args) > 1 and args[1] == "graphql":
            return "graphql"
        if args[0] == "api":
            return "comments"
        return f"{args[0]} {args[1]}"

    def __call__(
        self,
        args,
        env: dict[str, str] | None = None,
        input: str | None = None,
    ) -> str:
        args = tuple(args)
        self.calls.append((args, dict(env) if env else None, input))
        k = self.key(args)
        queue = self._responses.get(k)
        if not queue:
            return ""
        return queue[0] if len(queue) == 1 else queue.pop(0)

    def calls_for(
        self, group: str
    ) -> list[tuple[tuple[str, ...], dict | None, str | None]]:
        return [c for c in self.calls if self.key(c[0]) == group]


def project_item_json(nodes: list[dict]) -> str:
    return json.dumps(
        {"data": {"repository": {"issue": {"projectItems": {"nodes": nodes}}}}}
    )


def node(item_id: str, project_id: str, status: str | None) -> dict:
    return {
        "id": item_id,
        "project": {"id": project_id},
        "fieldValueByName": {"name": status} if status else None,
    }


def _result(uncovered: dict[str, list[int]], total: int = 20) -> DiffCoverage:
    return DiffCoverage(total=total, uncovered=uncovered)


def test_title_has_one_decimal():
    assert pi.title(1016, 72.34, 85) == "Diff-coverage #1016 72.3% < 85%"


def test_ranges_compresses_runs():
    assert pi.ranges([3, 4, 5, 9]) == "3-5, 9"


def test_ranges_empty_is_empty_string():
    assert pi.ranges([]) == ""


def test_body_lists_uncovered_files_with_ranges():
    result = _result({"src/a.py": [3, 4, 5, 9]})
    text = pi.body(1016, result, 85, "https://example.invalid/run/1")
    assert "PR #1016 is on `main` with" in text
    assert "below the 85% floor in `quality-baseline.toml`." in text
    assert "Uncovered lines:" in text
    assert "- `src/a.py`: 3-5, 9" in text
    assert "Found by the nightly run: https://example.invalid/run/1" in text
    assert "# pragma: no cover" in text


def test_body_without_run_url_drops_that_line():
    result = _result({"src/a.py": [3]})
    text = pi.body(1016, result, 85, None)
    assert "Found by the nightly run" not in text


def test_the_search_matches_the_exact_pr_not_a_prefix():
    gh = FakeGh()
    gh.queue(
        "issue list",
        json.dumps(
            [
                {
                    "number": 101,
                    "state": "OPEN",
                    "url": "u",
                    "title": "Diff-coverage #101 x",
                    "labels": [],
                }
            ]
        ),
    )
    gh.queue("issue create", "https://github.com/x/y/issues/55")
    result = _result({"src/a.py": [1]})
    action = pi.ensure_issue(gh, 10, result, 85, None)
    assert action is not None
    assert action[0] == "created"
    creates = gh.calls_for("issue create")
    assert len(creates) == 1


def test_no_issue_yet_creates_one_with_both_labels():
    gh = FakeGh()
    gh.queue("issue list", json.dumps([]))
    gh.queue("issue create", "https://github.com/x/y/issues/55")
    result = _result({"src/a.py": [3, 4]})
    action = pi.ensure_issue(gh, 1016, result, 85, "https://example.invalid/run/9")
    assert action == ("created", 55, "https://github.com/x/y/issues/55")
    creates = gh.calls_for("issue create")
    assert len(creates) == 1
    create_args = creates[0][0]
    assert create_args.count("--label") == 2
    assert "1-loc:container" in create_args
    assert "2-area:meta" in create_args
    assert creates[0][1] is None  # geen board-token op een issue-call


def test_an_open_issue_is_reconciled_not_duplicated():
    gh = FakeGh()
    gh.queue(
        "issue list",
        json.dumps(
            [
                {
                    "number": 12,
                    "state": "OPEN",
                    "url": "https://github.com/x/y/issues/12",
                    "title": "Diff-coverage #1016 70.0% < 85%",
                    "labels": [{"name": "1-loc:container"}],
                }
            ]
        ),
    )
    result = _result({"src/a.py": [1]})
    action = pi.ensure_issue(gh, 1016, result, 85, None)
    assert action == ("reconciled", 12, "https://github.com/x/y/issues/12")
    assert not gh.calls_for("issue create")
    edits = gh.calls_for("issue edit")
    assert len(edits) == 1
    assert "--add-label" in edits[0][0]
    assert "2-area:meta" in edits[0][0]
    assert "1-loc:container" not in edits[0][0][edits[0][0].index("--add-label") + 1 :]


def test_a_closed_issue_counts_as_handled():
    gh = FakeGh()
    gh.queue(
        "issue list",
        json.dumps(
            [
                {
                    "number": 12,
                    "state": "CLOSED",
                    "url": "u",
                    "title": "Diff-coverage #1016 70.0% < 85%",
                    "labels": [],
                }
            ]
        ),
    )
    result = _result({"src/a.py": [1]})
    action = pi.ensure_issue(gh, 1016, result, 85, None)
    assert action is None
    assert not gh.calls_for("issue create")
    assert not gh.calls_for("issue edit")


def test_a_new_item_gets_status_controller():
    gh = FakeGh()
    for _ in range(5):
        gh.queue(
            "graphql",
            project_item_json([node("PVTI_a", identity().board.project_id, None)]),
        )
    gh.queue(
        "graphql",
        project_item_json([node("PVTI_a", identity().board.project_id, "Controller")]),
    )
    sleeps: list[float] = []
    line = pi.ensure_on_board(
        gh, 1016, "https://github.com/x/y/issues/55", "tok", sleep=sleeps.append
    )
    edits = gh.calls_for("project item-edit")
    assert len(edits) == 1
    assert identity().board.status_options["Controller"] in edits[0][0]
    assert edits[0][1] == {"GH_TOKEN": "tok"}
    assert line == "#1016 op board, Status Controller"
    adds = gh.calls_for("project item-add")
    assert len(adds) == 1
    assert adds[0][1] == {"GH_TOKEN": "tok"}


def test_auto_add_to_do_is_moved_to_controller():
    gh = FakeGh()
    gh.queue(
        "graphql",
        project_item_json([node("PVTI_a", identity().board.project_id, None)]),
    )
    gh.queue(
        "graphql",
        project_item_json([node("PVTI_a", identity().board.project_id, "To do")]),
    )
    gh.queue(
        "graphql",
        project_item_json([node("PVTI_a", identity().board.project_id, "Controller")]),
    )
    sleeps: list[float] = []
    pi.ensure_on_board(gh, 1016, "u", "tok", sleep=sleeps.append)
    edits = gh.calls_for("project item-edit")
    assert len(edits) == 1
    assert identity().board.status_options["Controller"] in edits[0][0]


def test_the_status_is_polled_at_most_five_times():
    gh = FakeGh()
    for _ in range(6):
        gh.queue(
            "graphql",
            project_item_json([node("PVTI_a", identity().board.project_id, None)]),
        )
    sleeps: list[float] = []
    pi.ensure_on_board(gh, 1016, "u", "tok", sleep=sleeps.append)
    queries = gh.calls_for("graphql")
    assert len(queries) == 5 + 1  # 5 polls + 1 herlezing na de eerste item-edit
    # 4 sleeps tussen de 5 polls, dan één sleep(3) na de item-edit vóór de herlezing.
    assert sleeps == [3, 3, 3, 3, 3]
    edits = gh.calls_for("project item-edit")
    assert len(edits) == 1
    assert edits[0][0] == (
        "project",
        "item-edit",
        "--id",
        "PVTI_a",
        "--project-id",
        identity().board.project_id,
        "--field-id",
        identity().board.status_field_id,
        "--single-select-option-id",
        identity().board.status_options["Controller"],
    )


def test_a_late_auto_add_is_edited_again():
    gh = FakeGh()
    for _ in range(5):
        gh.queue(
            "graphql",
            project_item_json([node("PVTI_a", identity().board.project_id, None)]),
        )
    gh.queue(
        "graphql",
        project_item_json([node("PVTI_a", identity().board.project_id, "To do")]),
    )
    sleeps: list[float] = []
    pi.ensure_on_board(gh, 1016, "u", "tok", sleep=sleeps.append)
    edits = gh.calls_for("project item-edit")
    assert len(edits) == 2
    assert all(identity().board.status_options["Controller"] in e[0] for e in edits)


def test_item_add_id_seeds_item_edit_when_the_status_read_never_finds_it():
    """`project item-add --format json` levert de node-id synchroon; als de
    GraphQL-statuslezing het item onverwacht nooit terugvindt (`nodes: []`) mag
    `item-edit` niet met een lege `--id` draaien — hij valt terug op het zaad
    uit `item-add`."""
    gh = FakeGh()
    gh.queue("project item-add", json.dumps({"id": "PVTI_seed"}))
    for _ in range(6):
        gh.queue("graphql", project_item_json([]))  # nooit een node op ons project
    sleeps: list[float] = []
    line = pi.ensure_on_board(gh, 1016, "u", "tok", sleep=sleeps.append)
    edits = gh.calls_for("project item-edit")
    assert len(edits) == 1
    assert "PVTI_seed" in edits[0][0]
    assert line == "#1016 op board, Status Controller"


@pytest.mark.parametrize("status", ["Controller", "In progress", "On hold", "Done"])
def test_a_handed_over_item_is_not_touched(status):
    gh = FakeGh()
    gh.queue(
        "graphql",
        project_item_json([node("PVTI_a", identity().board.project_id, status)]),
    )
    sleeps: list[float] = []
    pi.ensure_on_board(gh, 1016, "u", "tok", sleep=sleeps.append)
    assert not gh.calls_for("project item-edit")
    assert not sleeps


def test_prcov_issues_finds_src_from_its_own_path(tmp_path):
    env = {k: v for k, v in os.environ.items() if k != "PYTHONPATH"}
    proc = subprocess.run(
        [sys.executable, str(PAD)],
        cwd=tmp_path,
        env=env,
        capture_output=True,
        text=True,
        check=False,
    )
    assert proc.returncode == 0, proc.stderr


def test_the_handoff_is_what_the_controller_starts():
    body = pi.handoff_comment(1234, 1016, 85)
    kop = parse_kop(body)
    assert kop is not None
    handoff = parse_handoff(kop)
    assert (handoff.issue, handoff.worktree, handoff.branch, handoff.wanneer) == (
        1234,
        pi.HOOFDCHECKOUT,
        "main",
        "nu",
    )
    assert beoordeel_commando(handoff.volgende, 1234) == ("start", "")


def test_an_existing_handoff_is_not_posted_twice():
    gh = FakeGh()
    # `--jq '.[].body | split("\n")[0]'` geeft per comment zijn eerste regel; de
    # tweede comment hier is dus zelf een `🤖 Handoff`-comment.
    gh.queue("comments", "some earlier comment\n" + KOP + " Handoff")
    posted = pi.ensure_handoff(gh, 12, 1016, 85)
    assert posted is False
    assert not gh.calls_for("issue comment")


def test_a_missing_handoff_is_posted():
    gh = FakeGh()
    gh.queue("comments", "some earlier comment")
    posted = pi.ensure_handoff(gh, 12, 1016, 85)
    assert posted is True
    comments = gh.calls_for("issue comment")
    assert len(comments) == 1
    assert comments[0][1] is None  # geen board-token op een issue-call
    assert comments[0][2] == pi.handoff_comment(12, 1016, 85)
    read = gh.calls_for("comments")
    assert len(read) == 1
    assert "--paginate" in read[0][0]


def test_a_handoff_with_extra_text_on_the_kop_line_still_counts():
    """`🤖 Handoff · iets` telt mee — de check is `startswith`, geen exacte match."""
    gh = FakeGh()
    gh.queue("comments", "some earlier comment\n" + KOP + " Handoff · replay")
    posted = pi.ensure_handoff(gh, 12, 1016, 85)
    assert posted is False
    assert not gh.calls_for("issue comment")


def test_a_quoted_handoff_line_mid_comment_does_not_count():
    """Een comment die `🤖 Handoff` ergens citeert, maar er niet mee *begint*,
    telt niet: `split("\\n")[0]` geeft alleen de eigen eerste regel van die
    comment terug, en dat is hier de quote-intro, niet het kopje zelf."""
    gh = FakeGh()
    gh.queue("comments", "Zag dit langskomen, ter info — quote:")
    posted = pi.ensure_handoff(gh, 12, 1016, 85)
    assert posted is True
    assert len(gh.calls_for("issue comment")) == 1


def test_the_handoff_comes_before_the_status():
    gh = FakeGh()
    gh.queue("comments", "")
    gh.queue(
        "graphql",
        project_item_json([node("PVTI_a", identity().board.project_id, "To do")]),
    )
    pi.ensure_handoff(gh, 12, 1016, 85)
    pi.ensure_on_board(gh, 12, "u", "tok", sleep=lambda _seconds: None)
    names = [gh.key(c[0]) for c in gh.calls]
    assert names.index("issue comment") < names.index("project item-edit")


def test_board_calls_carry_the_board_token_and_issue_calls_do_not():
    gh = FakeGh()
    gh.queue("issue list", json.dumps([]))
    gh.queue("issue create", "https://github.com/x/y/issues/55")
    gh.queue("comments", "")
    gh.queue(
        "graphql",
        project_item_json([node("PVTI_a", identity().board.project_id, "To do")]),
    )
    result = _result({"src/a.py": [1]})
    pi.ensure_issue(gh, 1016, result, 85, None)
    pi.ensure_handoff(gh, 55, 1016, 85)
    pi.ensure_on_board(gh, 55, "u", "tok", sleep=lambda _seconds: None)
    for group in ("issue list", "issue create", "issue comment"):
        for call in gh.calls_for(group):
            assert call[1] is None
    for group in ("project item-add", "graphql", "project item-edit"):
        for call in gh.calls_for(group):
            assert call[1] == {"GH_TOKEN": "tok"}


def test_the_watermark_asks_main_and_success():
    gh = FakeGh()
    gh.queue("run list", json.dumps([{"headSha": "deadbeef"}]))
    sha = pi.last_success_sha(gh)
    assert sha == "deadbeef"
    args = gh.calls[0][0]
    assert "--branch" in args
    assert "main" in args
    assert "--status" in args
    assert "success" in args
    assert "--event" in args
    assert "schedule" in args


def test_no_successful_run_gives_no_watermark():
    gh = FakeGh()
    gh.queue("run list", "[]")
    assert pi.last_success_sha(gh) is None


def test_run_gh_passes_a_timeout(monkeypatch):
    """Zelfde grens als `scripts/board_start.py`: een hangende `gh` mag de
    nightly niet vasthouden."""
    gevangen: dict[str, object] = {}

    def nep_run(cmd, **kwargs):
        gevangen.update(kwargs)
        return subprocess.CompletedProcess(cmd, 0, stdout="ok", stderr="")

    monkeypatch.setattr(pi.subprocess, "run", nep_run)
    assert pi.run_gh(["issue", "list"]) == "ok"
    assert gevangen.get("timeout") == 60


def test_run_gh_keeps_stderr_in_the_error_message(monkeypatch):
    """Review-ronde 1, bevinding 5: een naïeve `print(fout)` in de nightly-log
    moet de tekst van `gh` zelf tonen, niet alleen "returned non-zero exit
    status N"."""

    def nep_run(cmd, **kwargs):
        raise subprocess.CalledProcessError(
            1, cmd, output="", stderr="gh: issue not found"
        )

    monkeypatch.setattr(pi.subprocess, "run", nep_run)
    with pytest.raises(subprocess.CalledProcessError) as fout:
        pi.run_gh(["issue", "view", "999"])
    assert "gh: issue not found" in str(fout.value)


def test_andere_repo_stuurt_eigen_owner_naam_en_board(other_identity):
    gh = FakeGh()
    for _ in range(5):
        gh.queue("graphql", project_item_json([node("PVTI_a", "PVT_other", None)]))
    gh.queue("graphql", project_item_json([node("PVTI_a", "PVT_other", "Controller")]))
    pi.ensure_on_board(
        gh, 1016, "https://github.com/Other/repo/issues/55", "tok", sleep=lambda _: None
    )
    [(graphql, _, _), *_] = gh.calls_for("graphql")
    assert "owner=Other" in graphql
    assert "name=repo" in graphql
    assert not any("personal_ai_agent" in a for a in graphql)
    [(add, _, _)] = gh.calls_for("project item-add")
    assert add[:5] == ("project", "item-add", "7", "--owner", "Other")
    [(edit, _, _), *_] = gh.calls_for("project item-edit")
    assert "PVT_other" in edit
    assert "FIELD_other" in edit
    assert "opt-controller" in edit


def test_andere_repo_leest_comments_van_eigen_repo(other_identity):
    gh = FakeGh()
    gh.queue("comments", "")
    pi.ensure_handoff(gh, 12, 1016, 85)
    [(args, _, _)] = gh.calls_for("comments")
    assert "repos/Other/repo/issues/12/comments" in args
