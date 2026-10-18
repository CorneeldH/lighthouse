"""board_start: paia-start-issue zet alleen To do op In progress (#1007)."""

import importlib.util
import json
import subprocess
import sys
from pathlib import Path

import pytest

from src.controller.identity import identity

REPO_ROOT = Path(__file__).resolve().parents[2]
_SPEC = importlib.util.spec_from_file_location(
    "board_start", REPO_ROOT / "scripts" / "board_start.py"
)
assert _SPEC and _SPEC.loader
bs = importlib.util.module_from_spec(_SPEC)
sys.modules[_SPEC.name] = bs
_SPEC.loader.exec_module(bs)


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
    assert capsys.readouterr().out.startswith("board: #7 blijft ")


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
    assert capsys.readouterr().out.strip() == "board: #7 staat niet op het board"


def test_gh_faalt_blokkeert_niet(fake_gh, capsys):
    fake_gh.raise_on_api = subprocess.CalledProcessError(1, ["gh"])
    assert bs.main(["7"]) == 0
    assert capsys.readouterr().out.strip() == "board: overgeslagen (CalledProcessError)"


def test_geen_argument(capsys):
    assert bs.main([]) == 0
    assert capsys.readouterr().out.startswith("board: gebruik:")


def test_heropen_done_wordt_in_progress(fake_gh, capsys):
    fake_gh.statussen = ["Done", "In progress"]
    assert bs.main(["--heropen", "7"]) == 0
    edits = fake_gh.edits()
    assert len(edits) == 1
    assert "PVTI_a" in edits[0]
    assert identity().board.status_options["In progress"] in edits[0]
    assert capsys.readouterr().out.strip() == "board: #7 Done → In progress"


def test_heropen_al_in_progress(fake_gh, capsys):
    fake_gh.statussen = ["In progress"]
    assert bs.main(["--heropen", "7"]) == 0
    assert not fake_gh.edits()
    assert capsys.readouterr().out.strip() == "board: #7 staat al op In progress"


@pytest.mark.parametrize("status", ["To do", "On hold", "Controller", None])
def test_heropen_andere_status_faalt(fake_gh, capsys, status):
    fake_gh.statussen = [status]
    assert bs.main(["--heropen", "7"]) == 1
    assert not fake_gh.edits()
    uit = capsys.readouterr().out.strip()
    assert uit.startswith("board: FOUT")
    assert len(uit.splitlines()) == 1


def test_heropen_api_fout_faalt(fake_gh, capsys):
    fake_gh.raise_on_api = subprocess.CalledProcessError(1, ["gh"])
    assert bs.main(["--heropen", "7"]) == 1
    assert not fake_gh.edits()
    assert capsys.readouterr().out.startswith("board: FOUT")


def test_heropen_edit_fout_faalt(fake_gh, capsys):
    fake_gh.statussen = ["Done"]
    fake_gh.raise_on_edit = subprocess.TimeoutExpired(["gh"], 60)
    assert bs.main(["--heropen", "7"]) == 1
    assert capsys.readouterr().out.startswith("board: FOUT")


def test_heropen_terugleesverschil_faalt(fake_gh, capsys):
    fake_gh.statussen = ["Done", "Done"]
    assert bs.main(["--heropen", "7"]) == 1
    assert capsys.readouterr().out.startswith("board: FOUT")


def test_heropen_niet_op_board(fake_gh, capsys):
    fake_gh.nodes = []
    assert bs.main(["--heropen", "7"]) == 1
    assert not fake_gh.edits()
    assert capsys.readouterr().out.startswith("board: FOUT")


@pytest.mark.parametrize(
    "argv", [["--heropen"], ["--heropen", "x"], ["--heropen", "1", "2"]]
)
def test_heropen_ongeldig_argument(capsys, argv):
    assert bs.main(argv) == 1
    assert (
        capsys.readouterr().out.strip()
        == "board: gebruik: board_start.py --heropen <nr>"
    )


def _graphql_args(fake_gh: FakeGh) -> tuple[str, ...]:
    [args] = [c for c in fake_gh.calls if c[:2] == ("api", "graphql")]
    return args


def test_andere_repo_stuurt_eigen_owner_naam_en_board(other_identity, fake_gh):
    fake_gh.nodes = [node("PVTI_a", "PVT_other", "To do")]
    assert bs.main(["7"]) == 0
    args = _graphql_args(fake_gh)
    assert "owner=Other" in args
    assert "name=repo" in args
    assert not any("personal_ai_agent" in a for a in args)
    [edit] = fake_gh.edits()
    assert "PVT_other" in edit
    assert "FIELD_other" in edit
    assert "opt-progress" in edit


@pytest.fixture
def kapotte_config(monkeypatch, tmp_path):
    from src.controller.identity import identity as identity_fn

    kapot = tmp_path / "kapot.toml"
    kapot.write_text("dit is geen toml [", encoding="utf-8")
    monkeypatch.setenv("LIGHTHOUSE_TOML", str(kapot))
    identity_fn.cache_clear()
    yield kapot
    identity_fn.cache_clear()


def test_board_start_broken_config(kapotte_config, fake_gh, capsys):
    assert bs.main(["7"]) == 0
    regels = capsys.readouterr().out.strip().splitlines()
    assert len(regels) == 1
    assert regels[0].startswith("board: overgeslagen (IdentityError: ")
    assert str(kapotte_config) in regels[0]
    assert not fake_gh.edits()


def test_board_start_broken_config_heropen_faalt_dicht(kapotte_config, fake_gh, capsys):
    assert bs.main(["--heropen", "7"]) == 1
    regels = capsys.readouterr().out.strip().splitlines()
    assert len(regels) == 1
    assert regels[0].startswith("board: FOUT — IdentityError: ")
    assert str(kapotte_config) in regels[0]
    assert not fake_gh.edits()
