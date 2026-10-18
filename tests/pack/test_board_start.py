"""board_start: start-issue zet alleen To do op In progress.

Uit paia's `test_board_start.py`; het script draait uit de geïnstalleerde indeling
(`install_core`). Nieuw: het board is optioneel (besluit 16, 24), en een kapotte config
is in beide modi een fout.
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
OVERGESLAGEN = "board: skipped — no [forge.board] configured"


@pytest.fixture
def config(tmp_path, monkeypatch):
    def schrijf(body: str) -> Path:
        pad = tmp_path / "lighthouse.toml"
        pad.write_text(body, encoding="utf-8")
        monkeypatch.setenv("LIGHTHOUSE_TOML", str(pad))
        return pad

    return schrijf


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
        # Opeenvolgende `api graphql`-reads spelen deze statussen af; leeg = `nodes`.
        self.raise_on_edit: Exception | None = None
        self.statussen: list[str | None] = []

    def __call__(self, *args: str) -> str:
        self.calls.append(args)
        if args[:2] == ("api", "graphql"):
            if self.raise_on_api is not None:
                raise self.raise_on_api
            nodes = (
                [node("PVTI_a", identity().board.project_id, self.statussen.pop(0))]
                if self.statussen
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
def test_oppakbaar_wordt_in_progress(fake_gh, capsys, status):
    fake_gh.nodes = [node("PVTI_a", identity().board.project_id, status)]
    assert bs.main(["7"]) == 0
    edit = fake_gh.edits()
    assert len(edit) == 1
    assert "PVTI_a" in edit[0]
    assert identity().board.status_options["In progress"] in edit[0]
    assert capsys.readouterr().out.strip() == f"board: #7 {status} → In progress"


@pytest.mark.parametrize("status", ["Controller", "In progress", "Done", None])
def test_andere_status_blijft(fake_gh, capsys, status):
    fake_gh.nodes = [node("PVTI_a", identity().board.project_id, status)]
    assert bs.main(["7"]) == 0
    assert not fake_gh.edits()
    assert capsys.readouterr().out.startswith("board: #7 stays ")


def test_alleen_item_op_eigen_board(fake_gh):
    fake_gh.nodes = [
        node("PVTI_x", "PVT_ander", "To do"),
        node("PVTI_a", identity().board.project_id, "Controller"),
    ]
    bs.main(["7"])
    assert not fake_gh.edits()


def test_niet_op_board(fake_gh, capsys):
    fake_gh.nodes = []
    assert bs.main(["7"]) == 0
    assert capsys.readouterr().out.strip() == "board: #7 is not on the board"


def test_gh_faalt_blokkeert_niet(fake_gh, capsys):
    fake_gh.raise_on_api = subprocess.CalledProcessError(1, ["gh"])
    assert bs.main(["7"]) == 0
    assert capsys.readouterr().out.strip() == "board: skipped (CalledProcessError)"


def test_geen_argument(capsys):
    assert bs.main([]) == 0
    assert capsys.readouterr().out.strip() == "board: usage: board_start.py <nr>"


def test_reopen_done_wordt_in_progress(fake_gh, capsys):
    fake_gh.statussen = ["Done", "In progress"]
    assert bs.main(["--reopen", "7"]) == 0
    edits = fake_gh.edits()
    assert len(edits) == 1
    assert "PVTI_a" in edits[0]
    assert identity().board.status_options["In progress"] in edits[0]
    assert capsys.readouterr().out.strip() == "board: #7 Done → In progress"


def test_reopen_al_in_progress(fake_gh, capsys):
    fake_gh.statussen = ["In progress"]
    assert bs.main(["--reopen", "7"]) == 0
    assert not fake_gh.edits()
    assert capsys.readouterr().out.strip() == "board: #7 is already In progress"


@pytest.mark.parametrize("status", ["To do", "On hold", "Controller", None])
def test_reopen_andere_status_faalt(fake_gh, capsys, status):
    fake_gh.statussen = [status]
    assert bs.main(["--reopen", "7"]) == 1
    assert not fake_gh.edits()
    uit = capsys.readouterr().out.strip()
    assert uit.startswith("board: ERROR — ")
    assert len(uit.splitlines()) == 1


def test_reopen_api_fout_faalt(fake_gh, capsys):
    fake_gh.raise_on_api = subprocess.CalledProcessError(1, ["gh"])
    assert bs.main(["--reopen", "7"]) == 1
    assert not fake_gh.edits()
    assert capsys.readouterr().out.startswith("board: ERROR — ")


def test_reopen_edit_fout_faalt(fake_gh, capsys):
    fake_gh.statussen = ["Done"]
    fake_gh.raise_on_edit = subprocess.TimeoutExpired(["gh"], 60)
    assert bs.main(["--reopen", "7"]) == 1
    assert capsys.readouterr().out.startswith("board: ERROR — ")


def test_reopen_terugleesverschil_faalt(fake_gh, capsys):
    fake_gh.statussen = ["Done", "Done"]
    assert bs.main(["--reopen", "7"]) == 1
    assert capsys.readouterr().out.startswith("board: ERROR — ")


def test_reopen_niet_op_board(fake_gh, capsys):
    fake_gh.nodes = []
    assert bs.main(["--reopen", "7"]) == 1
    assert not fake_gh.edits()
    assert capsys.readouterr().out.startswith("board: ERROR — ")


@pytest.mark.parametrize(
    "argv", [["--reopen"], ["--reopen", "x"], ["--reopen", "1", "2"]]
)
def test_reopen_ongeldig_argument(capsys, argv):
    assert bs.main(argv) == 1
    assert (
        capsys.readouterr().out.strip()
        == "board: usage: board_start.py --reopen <nr>"
    )


def _graphql_args(fake_gh: FakeGh) -> tuple[str, ...]:
    [args] = [c for c in fake_gh.calls if c[:2] == ("api", "graphql")]
    return args


def test_andere_repo_stuurt_eigen_owner_naam_en_board(config, fake_gh):
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
def kapotte_config(config):
    return config("dit is geen toml [")


@pytest.mark.parametrize("argv", [["7"], ["--reopen", "7"]])
def test_broken_config_is_error(kapotte_config, fake_gh, capsys, argv):
    assert bs.main(argv) == 2
    regels = capsys.readouterr().out.strip().splitlines()
    assert len(regels) == 1
    assert regels[0].startswith("board: ERROR — ConfigError: ")
    assert str(kapotte_config) in regels[0]
    assert fake_gh.calls == []


def test_board_zonder_repo_is_een_fout(config, fake_gh, capsys):
    config(BOARD.replace('repo = "Owner/repo"\n', ""))
    assert bs.main(["7"]) == 2
    uit = capsys.readouterr().out.strip()
    assert uit.startswith("board: ERROR — ") and "forge.repo" in uit
    assert fake_gh.calls == []


def test_no_board_default_mode(config, fake_gh, capsys):
    config('[forge]\nrepo = "Owner/repo"\n')
    assert bs.main(["7"]) == 0
    assert capsys.readouterr().out.strip() == OVERGESLAGEN
    assert fake_gh.calls == []


def test_no_board_reopen(config, fake_gh, capsys):
    config('[project]\nlanguage = "en"\n')
    assert bs.main(["--reopen", "7"]) == 0
    assert capsys.readouterr().out.strip() == OVERGESLAGEN
    assert fake_gh.calls == []


def test_draait_als_geinstalleerd_bestand(config):
    """Het geïnstalleerde script vindt `lighthouse_lib` naast zich, vanuit elke cwd."""
    config('[forge]\nrepo = "Owner/repo"\n')
    result = subprocess.run(
        [sys.executable, str(SCRIPT), "7"],
        capture_output=True,
        text=True,
        cwd="/",
        check=False,
    )
    assert (result.returncode, result.stdout.strip()) == (0, OVERGESLAGEN), result.stderr
