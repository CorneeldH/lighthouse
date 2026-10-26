"""board_start: start-issue only moves To do to In progress.

From paia's `test_board_start.py`; the script runs from the installed layout
(`install_core`). New: the board is optional (decision 16, 24), and a broken config
is an error in both modes.
"""

import importlib.util
import json
import subprocess
import sys
from pathlib import Path

import pytest

from tests.pack._testkit import INSTALL_ROOT

SCRIPT = INSTALL_ROOT / ".lighthouse" / "scripts" / "board_start.py"
_SPEC = importlib.util.spec_from_file_location("board_start", SCRIPT)
assert _SPEC and _SPEC.loader
bs = importlib.util.module_from_spec(_SPEC)
sys.modules[_SPEC.name] = bs
_SPEC.loader.exec_module(bs)


PROJECT = "PVT_test"
BOARD = f"""
[forge]
kind = "github"
repo = "Owner/repo"

[forge.board]
project_number = 1
project_id = "{PROJECT}"
status_field_id = "FIELD_test"

[forge.board.status_options]
"To do" = "opt-todo"
"In progress" = "opt-progress"
Done = "opt-done"
"On hold" = "opt-hold"
"""
SKIPPED = "board: skipped — no [forge.board] configured"


@pytest.fixture
def config(tmp_path, monkeypatch):
    def write(body: str) -> Path:
        path = tmp_path / "lighthouse.toml"
        path.write_text(body, encoding="utf-8")
        monkeypatch.setenv("LIGHTHOUSE_TOML", str(path))
        return path

    return write


@pytest.fixture(autouse=True)
def board(config):
    config(BOARD)


def identity():
    class _Board:
        project_id = PROJECT
        status_options = {"In progress": "opt-progress"}

    class _Id:
        board = _Board

    return _Id


def node(item_id: str, project_id: str, status: str | None) -> dict:
    return {
        "id": item_id,
        "project": {"id": project_id},
        "fieldValueByName": {"name": status} if status else None,
    }


class FakeGh:
    def __init__(self) -> None:
        self.calls: list[tuple[str, ...]] = []
        self.nodes: list[dict] = []
        self.raise_on_api: Exception | None = None
        # Successive `api graphql` reads play back these statuses; empty = `nodes`.
        self.raise_on_edit: Exception | None = None
        self.statuses: list[str | None] = []

    def __call__(self, *args: str) -> str:
        self.calls.append(args)
        if args[:2] == ("api", "graphql"):
            if self.raise_on_api is not None:
                raise self.raise_on_api
            nodes = (
                [node("PVTI_a", identity().board.project_id, self.statuses.pop(0))]
                if self.statuses
                else self.nodes
            )
            return json.dumps(
                {"data": {"repository": {"issue": {"projectItems": {"nodes": nodes}}}}}
            )
        if args[:2] == ("project", "item-edit") and self.raise_on_edit is not None:
            raise self.raise_on_edit
        return ""

    def edits(self) -> list[tuple[str, ...]]:
        return [c for c in self.calls if c[:2] == ("project", "item-edit")]


@pytest.fixture
def fake_gh(monkeypatch):
    fake = FakeGh()
    monkeypatch.setattr(bs, "gh", fake)
    return fake


@pytest.mark.parametrize("status", ["To do", "On hold"])
def test_pickable_becomes_in_progress(fake_gh, capsys, status):
    fake_gh.nodes = [node("PVTI_a", identity().board.project_id, status)]
    assert bs.main(["7"]) == 0
    edit = fake_gh.edits()
    assert len(edit) == 1
    assert "PVTI_a" in edit[0]
    assert identity().board.status_options["In progress"] in edit[0]
    assert capsys.readouterr().out.strip() == f"board: #7 {status} → In progress"


@pytest.mark.parametrize("status", ["Controller", "In progress", "Done", None])
def test_other_status_stays(fake_gh, capsys, status):
    fake_gh.nodes = [node("PVTI_a", identity().board.project_id, status)]
    assert bs.main(["7"]) == 0
    assert not fake_gh.edits()
    assert capsys.readouterr().out.startswith("board: #7 stays ")


def test_only_item_on_own_board(fake_gh):
    fake_gh.nodes = [
        node("PVTI_x", "PVT_ander", "To do"),
        node("PVTI_a", identity().board.project_id, "Controller"),
    ]
    bs.main(["7"])
    assert not fake_gh.edits()


def test_not_on_board(fake_gh, capsys):
    fake_gh.nodes = []
    assert bs.main(["7"]) == 0
    assert capsys.readouterr().out.strip() == "board: #7 is not on the board"


def test_gh_failure_does_not_block(fake_gh, capsys):
    fake_gh.raise_on_api = subprocess.CalledProcessError(1, ["gh"])
    assert bs.main(["7"]) == 0
    assert capsys.readouterr().out.strip() == "board: skipped (CalledProcessError)"


def test_no_argument(capsys):
    assert bs.main([]) == 0
    assert capsys.readouterr().out.strip() == "board: usage: board_start.py <nr>"


def test_reopen_done_becomes_in_progress(fake_gh, capsys):
    fake_gh.statuses = ["Done", "In progress"]
    assert bs.main(["--reopen", "7"]) == 0
    edits = fake_gh.edits()
    assert len(edits) == 1
    assert "PVTI_a" in edits[0]
    assert identity().board.status_options["In progress"] in edits[0]
    assert capsys.readouterr().out.strip() == "board: #7 Done → In progress"


def test_reopen_already_in_progress(fake_gh, capsys):
    fake_gh.statuses = ["In progress"]
    assert bs.main(["--reopen", "7"]) == 0
    assert not fake_gh.edits()
    assert capsys.readouterr().out.strip() == "board: #7 is already In progress"


@pytest.mark.parametrize("status", ["To do", "On hold", "Controller", None])
def test_reopen_other_status_fails(fake_gh, capsys, status):
    fake_gh.statuses = [status]
    assert bs.main(["--reopen", "7"]) == 1
    assert not fake_gh.edits()
    out = capsys.readouterr().out.strip()
    assert out.startswith("board: ERROR — ")
    assert len(out.splitlines()) == 1


def test_reopen_api_error_fails(fake_gh, capsys):
    fake_gh.raise_on_api = subprocess.CalledProcessError(1, ["gh"])
    assert bs.main(["--reopen", "7"]) == 1
    assert not fake_gh.edits()
    assert capsys.readouterr().out.startswith("board: ERROR — ")


def test_reopen_edit_error_fails(fake_gh, capsys):
    fake_gh.statuses = ["Done"]
    fake_gh.raise_on_edit = subprocess.TimeoutExpired(["gh"], 60)
    assert bs.main(["--reopen", "7"]) == 1
    assert capsys.readouterr().out.startswith("board: ERROR — ")


def test_reopen_read_back_mismatch_fails(fake_gh, capsys):
    fake_gh.statuses = ["Done", "Done"]
    assert bs.main(["--reopen", "7"]) == 1
    assert capsys.readouterr().out.startswith("board: ERROR — ")


def test_reopen_not_on_board(fake_gh, capsys):
    fake_gh.nodes = []
    assert bs.main(["--reopen", "7"]) == 1
    assert not fake_gh.edits()
    assert capsys.readouterr().out.startswith("board: ERROR — ")


@pytest.mark.parametrize(
    "argv", [["--reopen"], ["--reopen", "x"], ["--reopen", "1", "2"]]
)
def test_reopen_invalid_argument(capsys, argv):
    assert bs.main(argv) == 1
    assert (
        capsys.readouterr().out.strip()
        == "board: usage: board_start.py --reopen <nr>"
    )


def _graphql_args(fake_gh: FakeGh) -> tuple[str, ...]:
    [args] = [c for c in fake_gh.calls if c[:2] == ("api", "graphql")]
    return args


def test_other_repo_sends_own_owner_name_and_board(config, fake_gh):
    config(
        BOARD.replace('"Owner/repo"', '"Other/repo"')
        .replace(PROJECT, "PVT_other")
        .replace("FIELD_test", "FIELD_other")
    )
    fake_gh.nodes = [node("PVTI_a", "PVT_other", "To do")]
    assert bs.main(["7"]) == 0
    args = _graphql_args(fake_gh)
    assert "owner=Other" in args
    assert "name=repo" in args
    assert "number=7" in args
    [edit] = fake_gh.edits()
    assert "PVT_other" in edit
    assert "FIELD_other" in edit
    assert "opt-progress" in edit


@pytest.fixture
def broken_config(config):
    return config("dit is geen toml [")


@pytest.mark.parametrize("argv", [["7"], ["--reopen", "7"]])
def test_broken_config_is_error(broken_config, fake_gh, capsys, argv):
    assert bs.main(argv) == 2
    lines = capsys.readouterr().out.strip().splitlines()
    assert len(lines) == 1
    assert lines[0].startswith("board: ERROR — ConfigError: ")
    assert str(broken_config) in lines[0]
    assert fake_gh.calls == []


def test_board_without_repo_is_an_error(config, fake_gh, capsys):
    config(BOARD.replace('repo = "Owner/repo"\n', ""))
    assert bs.main(["7"]) == 2
    out = capsys.readouterr().out.strip()
    assert out.startswith("board: ERROR — ") and "forge.repo" in out
    assert fake_gh.calls == []


def test_no_board_default_mode(config, fake_gh, capsys):
    config('[forge]\nrepo = "Owner/repo"\n')
    assert bs.main(["7"]) == 0
    assert capsys.readouterr().out.strip() == SKIPPED
    assert fake_gh.calls == []


def test_no_board_reopen(config, fake_gh, capsys):
    config('[project]\nlanguage = "en"\n')
    assert bs.main(["--reopen", "7"]) == 0
    assert capsys.readouterr().out.strip() == SKIPPED
    assert fake_gh.calls == []


def test_runs_as_installed_file(config):
    """The installed script finds `lighthouse_lib` next to itself, from any cwd."""
    config('[forge]\nrepo = "Owner/repo"\n')
    result = subprocess.run(
        [sys.executable, str(SCRIPT), "7"],
        capture_output=True,
        text=True,
        cwd="/",
        check=False,
    )
    assert (result.returncode, result.stdout.strip()) == (0, SKIPPED), result.stderr
