"""Issue, handoff and board per PR: `prcov_issues.py`.

From the source repository's `test_quality_prcov_issues.py`. The identity comes from
`lighthouse.toml` (the `lighthouse_toml` fixture, with a board) instead of the source
repository's controller; the handoff is checked with `protocol` from `pack/core`.
"""

from __future__ import annotations

import json
import os
import subprocess
import sys
from pathlib import Path

import pytest

from lighthouse_lib import protocol
from tests.pack._testkit import GIT_ENV, git, init_repo
from tests.pack.python.conftest import BOARD_TOML, PY_SCRIPTS

_SCRIPTS = Path(__file__).resolve().parents[3] / "pack" / "python" / "scripts"
if str(_SCRIPTS) not in sys.path:
    sys.path.insert(0, str(_SCRIPTS))

from quality import prcov_issues as pi
from quality.diffcov import DiffCoverage

LEGACY_HANDOFF = (
    "🤖 Handoff\n"
    "issue: #12\n"
    "worktree: /srv/checkout\n"
    "branch: main\n"
    "anker: #12\n"
    "volgende: /old-start-issue 12\n"
    "wanneer: nu\n"
)


@pytest.fixture(autouse=True)
def _board(lighthouse_toml, monkeypatch):
    monkeypatch.setenv("LIGHTHOUSE_HANDOFF_WORKTREE", "/srv/checkout")
    return lighthouse_toml


class FakeGh:
    """Logs every call (args, env, input) and returns canned answers.

    `queue(key, *answers)` lines answers up for a key; every call with that key pops the
    next one (the last stays once the queue is empty). `key(args)` groups on the first
    two words of the command, except `api graphql` ("graphql") and other `api ...`
    calls ("comments").
    """

    def __init__(self) -> None:
        self.calls: list[tuple[tuple[str, ...], dict[str, str] | None, str | None]] = []
        self._responses: dict[str, list[str]] = {}

    def queue(self, key: str, *answers: str) -> None:
        self._responses.setdefault(key, []).extend(answers)

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


def comments(*bodies: str) -> str:
    """What `gh api … --jq '.[].body | @json'` prints: one JSON string per comment."""
    return "".join(json.dumps(body) + "\n" for body in bodies)


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


def test_body_names_the_default_branch(lighthouse_toml):
    lighthouse_toml(BOARD_TOML.replace('repo = "Other/repo"', 'repo = "Other/repo"\ndefault_branch = "trunk"'))
    assert "PR #7 is on `trunk` with" in pi.body(7, _result({"src/a.py": [1]}), 85, None)


def test_body_without_run_url_drops_that_line():
    result = _result({"src/a.py": [3]})
    text = pi.body(1016, result, 85, None)
    assert "Found by the nightly run" not in text


def test_the_search_matches_the_exact_pr_not_a_prefix():
    gh = FakeGh()
    gh.queue(
        "issue list",
        json.dumps(
            [{"number": 101, "state": "OPEN", "url": "u", "title": "Diff-coverage #101 x", "labels": []}]
        ),
    )
    gh.queue("issue create", "https://github.com/x/y/issues/55")
    action = pi.ensure_issue(gh, 10, _result({"src/a.py": [1]}), 85, None)
    assert action is not None
    assert action[0] == "created"
    assert len(gh.calls_for("issue create")) == 1


def test_no_issue_yet_creates_one_without_labels_by_default():
    gh = FakeGh()
    gh.queue("issue list", json.dumps([]))
    gh.queue("issue create", "https://github.com/x/y/issues/55")
    action = pi.ensure_issue(gh, 1016, _result({"src/a.py": [3, 4]}), 85, "https://example.invalid/run/9")
    assert action == ("created", 55, "https://github.com/x/y/issues/55")
    [create] = gh.calls_for("issue create")
    assert "--label" not in create[0]
    assert create[1] is None  # no board token on an issue call


def test_labels_from_config(lighthouse_toml):
    lighthouse_toml(BOARD_TOML + '\n[python]\ncoverage_issue_labels = ["x", "y"]\n')
    gh = FakeGh()
    gh.queue("issue list", json.dumps([]))
    gh.queue("issue create", "https://github.com/x/y/issues/55")
    pi.ensure_issue(gh, 1016, _result({"src/a.py": [3]}), 85, None)
    [create] = gh.calls_for("issue create")
    args = create[0]
    assert [args[i + 1] for i, a in enumerate(args) if a == "--label"] == ["x", "y"]


def test_an_open_issue_is_reconciled_not_duplicated(lighthouse_toml):
    lighthouse_toml(BOARD_TOML + '\n[python]\ncoverage_issue_labels = ["loc", "area"]\n')
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
                    "labels": [{"name": "loc"}],
                }
            ]
        ),
    )
    action = pi.ensure_issue(gh, 1016, _result({"src/a.py": [1]}), 85, None)
    assert action == ("reconciled", 12, "https://github.com/x/y/issues/12")
    assert not gh.calls_for("issue create")
    [edit] = gh.calls_for("issue edit")
    assert edit[0] == ("issue", "edit", "12", "--add-label", "area")


def test_a_closed_issue_counts_as_handled():
    gh = FakeGh()
    gh.queue(
        "issue list",
        json.dumps(
            [{"number": 12, "state": "CLOSED", "url": "u", "title": "Diff-coverage #1016 70.0% < 85%", "labels": []}]
        ),
    )
    assert pi.ensure_issue(gh, 1016, _result({"src/a.py": [1]}), 85, None) is None
    assert not gh.calls_for("issue create")
    assert not gh.calls_for("issue edit")


def test_a_new_item_gets_status_controller():
    gh = FakeGh()
    for _ in range(5):
        gh.queue("graphql", project_item_json([node("PVTI_a", "PVT_other", None)]))
    gh.queue("graphql", project_item_json([node("PVTI_a", "PVT_other", "Controller")]))
    sleeps: list[float] = []
    line = pi.ensure_on_board(gh, 1016, "https://github.com/x/y/issues/55", "tok", sleep=sleeps.append)
    [edit] = gh.calls_for("project item-edit")
    assert "opt-controller" in edit[0]
    assert edit[1] == {"GH_TOKEN": "tok"}
    assert line == "#1016 on board, status Controller"
    [add] = gh.calls_for("project item-add")
    assert add[1] == {"GH_TOKEN": "tok"}


def test_auto_add_to_do_is_moved_to_controller():
    gh = FakeGh()
    gh.queue("graphql", project_item_json([node("PVTI_a", "PVT_other", None)]))
    gh.queue("graphql", project_item_json([node("PVTI_a", "PVT_other", "To do")]))
    gh.queue("graphql", project_item_json([node("PVTI_a", "PVT_other", "Controller")]))
    pi.ensure_on_board(gh, 1016, "u", "tok", sleep=lambda _s: None)
    [edit] = gh.calls_for("project item-edit")
    assert "opt-controller" in edit[0]


def test_the_status_is_polled_at_most_five_times():
    gh = FakeGh()
    for _ in range(6):
        gh.queue("graphql", project_item_json([node("PVTI_a", "PVT_other", None)]))
    sleeps: list[float] = []
    pi.ensure_on_board(gh, 1016, "u", "tok", sleep=sleeps.append)
    assert len(gh.calls_for("graphql")) == 5 + 1  # 5 polls + 1 reread after the first item-edit
    # 4 sleeps between the 5 polls, then one sleep(3) after the item-edit before the reread.
    assert sleeps == [3, 3, 3, 3, 3]
    [edit] = gh.calls_for("project item-edit")
    assert edit[0] == (
        "project",
        "item-edit",
        "--id",
        "PVTI_a",
        "--project-id",
        "PVT_other",
        "--field-id",
        "FIELD_other",
        "--single-select-option-id",
        "opt-controller",
    )


def test_a_late_auto_add_is_edited_again():
    gh = FakeGh()
    for _ in range(5):
        gh.queue("graphql", project_item_json([node("PVTI_a", "PVT_other", None)]))
    gh.queue("graphql", project_item_json([node("PVTI_a", "PVT_other", "To do")]))
    pi.ensure_on_board(gh, 1016, "u", "tok", sleep=lambda _s: None)
    edits = gh.calls_for("project item-edit")
    assert len(edits) == 2
    assert all("opt-controller" in e[0] for e in edits)


def test_item_add_id_seeds_item_edit_when_the_status_read_never_finds_it():
    """`project item-add --format json` gives the node id synchronously; when the status
    read never finds the item, `item-edit` falls back on that seed, not an empty --id."""
    gh = FakeGh()
    gh.queue("project item-add", json.dumps({"id": "PVTI_seed"}))
    for _ in range(6):
        gh.queue("graphql", project_item_json([]))
    line = pi.ensure_on_board(gh, 1016, "u", "tok", sleep=lambda _s: None)
    [edit] = gh.calls_for("project item-edit")
    assert "PVTI_seed" in edit[0]
    assert line == "#1016 on board, status Controller"


@pytest.mark.parametrize("status", ["Controller", "In progress", "On hold", "Done"])
def test_a_handed_over_item_is_not_touched(status):
    gh = FakeGh()
    gh.queue("graphql", project_item_json([node("PVTI_a", "PVT_other", status)]))
    sleeps: list[float] = []
    pi.ensure_on_board(gh, 1016, "u", "tok", sleep=sleeps.append)
    assert not gh.calls_for("project item-edit")
    assert not sleeps


def test_no_controller_option_keeps_to_do(lighthouse_toml):
    lighthouse_toml(BOARD_TOML.replace('"Controller" = "opt-controller"\n', ""))
    gh = FakeGh()
    gh.queue("graphql", project_item_json([node("PVTI_a", "PVT_other", "To do")]))
    sleeps: list[float] = []
    line = pi.ensure_on_board(gh, 1016, "u", "tok", sleep=sleeps.append)
    assert len(gh.calls_for("project item-add")) == 1
    assert not gh.calls_for("project item-edit")
    assert not sleeps
    assert line.startswith("#1016 on board, status To do")
    assert "board: status Controller skipped — no such status option" in line


def test_the_board_comes_from_the_config():
    assert pi.board() is not None
    assert pi.board().project_id == "PVT_other"


def test_no_board_section_means_no_board(lighthouse_toml):
    lighthouse_toml('[project]\nlanguage = "en"\n\n[forge]\nkind = "github"\nrepo = "Other/repo"\n')
    assert pi.board() is None


def test_prcov_issues_finds_lighthouse_lib_from_its_own_path(tmp_path):
    env = {k: v for k, v in os.environ.items() if k != "PYTHONPATH"}
    proc = subprocess.run(
        [sys.executable, str(PY_SCRIPTS / "quality" / "prcov_issues.py")],
        cwd=tmp_path,
        env=env,
        capture_output=True,
        text=True,
        check=False,
    )
    assert proc.returncode == 0, proc.stderr


def test_handoff_parses_with_protocol():
    body = pi.handoff_comment(1234, 1016, 85)
    handoff = protocol.parse_handoff(protocol.parse_header(body))
    assert handoff.issue == 1234
    assert handoff.worktree == "/srv/checkout"
    assert handoff.branch == "main"
    assert handoff.anchor == "#1234"
    assert handoff.next == "/lighthouse-start-issue 1234"
    assert handoff.when == "now"
    for old in ("volgende:", "wanneer:", "anker:"):
        assert old not in body


def test_handoff_branch_follows_default_branch(lighthouse_toml):
    lighthouse_toml(BOARD_TOML.replace('repo = "Other/repo"', 'repo = "Other/repo"\ndefault_branch = "trunk"'))
    handoff = protocol.parse_handoff(protocol.parse_header(pi.handoff_comment(5, 6, 85)))
    assert handoff.branch == "trunk"


def test_handoff_heading_follows_language(lighthouse_toml):
    lighthouse_toml(BOARD_TOML.replace('language = "en"', 'language = "nl"'))
    body = pi.handoff_comment(1234, 1016, 85)
    assert body.startswith("🤖 Handoff\n")
    header = protocol.parse_header(body)
    assert header.kind == "handoff"
    assert {"issue", "worktree", "branch", "anchor", "next", "when"} <= set(header.fields)


def test_worktree_from_env_or_main_checkout(tmp_path, monkeypatch):
    main = tmp_path / "main"
    init_repo(main)
    linked = tmp_path / "linked"
    git(main, "worktree", "add", "-q", str(linked), "-b", "side")
    monkeypatch.setenv("LIGHTHOUSE_HANDOFF_WORKTREE", "/elsewhere/checkout")
    assert pi.handoff_worktree() == "/elsewhere/checkout"
    monkeypatch.delenv("LIGHTHOUSE_HANDOFF_WORKTREE")
    monkeypatch.setenv("LIGHTHOUSE_REPO_ROOT", str(linked))
    assert pi.handoff_worktree() == str(main.resolve())


def test_an_existing_handoff_is_not_posted_twice():
    gh = FakeGh()
    gh.queue("comments", comments("some earlier comment", pi.handoff_comment(12, 1016, 85)))
    assert pi.ensure_handoff(gh, 12, 1016, 85) is False
    assert not gh.calls_for("issue comment")


def test_dedupe_sees_legacy_handoff():
    gh = FakeGh()
    gh.queue("comments", comments("some earlier comment", LEGACY_HANDOFF))
    assert pi.ensure_handoff(gh, 12, 1016, 85) is False
    assert not gh.calls_for("issue comment")


def test_a_missing_handoff_is_posted():
    gh = FakeGh()
    gh.queue("comments", comments("some earlier comment"))
    assert pi.ensure_handoff(gh, 12, 1016, 85) is True
    [comment] = gh.calls_for("issue comment")
    assert comment[1] is None  # no board token on an issue call
    assert comment[2] == pi.handoff_comment(12, 1016, 85)
    [read] = gh.calls_for("comments")
    assert "--paginate" in read[0]
    assert "repos/Other/repo/issues/12/comments" in read[0]


def test_a_handoff_with_extra_text_on_the_heading_line_still_counts():
    """`🤖 Handoff · something` counts: the protocol reads the kind before the `·`."""
    gh = FakeGh()
    gh.queue("comments", comments("🤖 Handoff · replay\nissue: #12\n"))
    assert pi.ensure_handoff(gh, 12, 1016, 85) is False


def test_a_quoted_handoff_line_mid_comment_does_not_count():
    """A comment that quotes `🤖 Handoff` but does not start with it does not count."""
    gh = FakeGh()
    gh.queue("comments", comments("Saw this pass by — quote:\n> 🤖 Handoff\n> issue: #12"))
    assert pi.ensure_handoff(gh, 12, 1016, 85) is True
    assert len(gh.calls_for("issue comment")) == 1


def test_the_handoff_comes_before_the_status():
    gh = FakeGh()
    gh.queue("comments", "")
    gh.queue("graphql", project_item_json([node("PVTI_a", "PVT_other", "To do")]))
    pi.ensure_handoff(gh, 12, 1016, 85)
    pi.ensure_on_board(gh, 12, "u", "tok", sleep=lambda _seconds: None)
    names = [gh.key(c[0]) for c in gh.calls]
    assert names.index("issue comment") < names.index("project item-edit")


def test_board_calls_carry_the_board_token_and_issue_calls_do_not():
    gh = FakeGh()
    gh.queue("issue list", json.dumps([]))
    gh.queue("issue create", "https://github.com/x/y/issues/55")
    gh.queue("comments", "")
    gh.queue("graphql", project_item_json([node("PVTI_a", "PVT_other", "To do")]))
    pi.ensure_issue(gh, 1016, _result({"src/a.py": [1]}), 85, None)
    pi.ensure_handoff(gh, 55, 1016, 85)
    pi.ensure_on_board(gh, 55, "u", "tok", sleep=lambda _seconds: None)
    for group in ("issue list", "issue create", "issue comment"):
        for call in gh.calls_for(group):
            assert call[1] is None
    for group in ("project item-add", "graphql", "project item-edit"):
        for call in gh.calls_for(group):
            assert call[1] == {"GH_TOKEN": "tok"}


def test_board_calls_use_the_repo_from_the_config():
    gh = FakeGh()
    for _ in range(5):
        gh.queue("graphql", project_item_json([node("PVTI_a", "PVT_other", None)]))
    gh.queue("graphql", project_item_json([node("PVTI_a", "PVT_other", "Controller")]))
    pi.ensure_on_board(gh, 1016, "https://github.com/Other/repo/issues/55", "tok", sleep=lambda _: None)
    [(graphql, _, _), *_] = gh.calls_for("graphql")
    assert "owner=Other" in graphql
    assert "name=repo" in graphql
    [(add, _, _)] = gh.calls_for("project item-add")
    assert add[:5] == ("project", "item-add", "7", "--owner", "Other")
    [(edit, _, _), *_] = gh.calls_for("project item-edit")
    assert "PVT_other" in edit and "FIELD_other" in edit and "opt-controller" in edit


def test_the_watermark_asks_the_workflow_branch_and_success(lighthouse_toml):
    lighthouse_toml(BOARD_TOML + '\n[python]\nnightly_workflow = "nightly.yml"\n')
    gh = FakeGh()
    gh.queue("run list", json.dumps([{"headSha": "deadbeef"}]))
    assert pi.last_success_sha(gh, "nightly.yml") == "deadbeef"
    args = gh.calls[0][0]
    for flag, value in (("--workflow", "nightly.yml"), ("--branch", "main"), ("--status", "success"), ("--event", "schedule")):
        assert args[args.index(flag) + 1] == value


def test_no_successful_run_gives_no_watermark():
    gh = FakeGh()
    gh.queue("run list", "[]")
    assert pi.last_success_sha(gh, "nightly.yml") is None


def test_the_workflow_comes_from_the_config(lighthouse_toml):
    assert pi.nightly_workflow() is None
    lighthouse_toml(BOARD_TOML + '\n[python]\nnightly_workflow = "nightly.yml"\n')
    assert pi.nightly_workflow() == "nightly.yml"


def test_the_board_token_env():
    assert pi.BOARD_TOKEN_ENV == "LIGHTHOUSE_BOARD_TOKEN"


def test_run_gh_passes_a_timeout(monkeypatch):
    """A hanging `gh` must not hold the nightly."""
    seen: dict[str, object] = {}

    def fake_run(cmd, **kwargs):
        seen.update(kwargs)
        return subprocess.CompletedProcess(cmd, 0, stdout="ok", stderr="")

    monkeypatch.setattr(pi.subprocess, "run", fake_run)
    assert pi.run_gh(["issue", "list"]) == "ok"
    assert seen.get("timeout") == 60


def test_run_gh_keeps_stderr_in_the_error_message(monkeypatch):
    def fake_run(cmd, **kwargs):
        raise subprocess.CalledProcessError(1, cmd, output="", stderr="gh: issue not found")

    monkeypatch.setattr(pi.subprocess, "run", fake_run)
    with pytest.raises(subprocess.CalledProcessError) as error:
        pi.run_gh(["issue", "view", "999"])
    assert "gh: issue not found" in str(error.value)


def test_output_is_english():
    from tests.pack.python.test_ratchet import _messages

    for name in ("prcov.py", "prcov_issues.py"):
        for word in (" de ", " het ", " een ", " geen ", " op board", "Status ", "faalde"):
            hits = [t for t in _messages(_SCRIPTS / "quality" / name) if word in f" {t} "]
            assert not hits, (name, word, hits[:3])
