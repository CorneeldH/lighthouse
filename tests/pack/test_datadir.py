"""`lighthouse_lib.datadir`: where the review records live (decision 27).

The behaviour follows `data_dir()` from the source repository's measurement
experiment: an override wins, the directory is created, and a directory inside a
git work tree is refused, because the records carry session content.
"""

from pathlib import Path

import pytest

from lighthouse_lib.datadir import DataDirInRepoError, data_dir
from tests.pack._testkit import init_repo


def test_data_dir_env_wins(tmp_path):
    target = tmp_path / "records"
    got = data_dir({"LIGHTHOUSE_DATA": str(target), "XDG_DATA_HOME": str(tmp_path / "x")})
    assert got == target.resolve()
    assert got.is_dir()


def test_data_dir_default_xdg(tmp_path):
    got = data_dir({"XDG_DATA_HOME": str(tmp_path)})
    assert got == (tmp_path / "lighthouse" / "review-records").resolve()
    assert got.is_dir()


def test_data_dir_default_home(tmp_path, monkeypatch):
    monkeypatch.setenv("HOME", str(tmp_path))
    got = data_dir({})
    assert got == (tmp_path / ".local/share/lighthouse/review-records").resolve()


def test_data_dir_relative_xdg_is_ignored(tmp_path, monkeypatch):
    # The XDG base directory spec: a relative path is invalid and is ignored.
    monkeypatch.setenv("HOME", str(tmp_path))
    got = data_dir({"XDG_DATA_HOME": "rel"})
    assert got == (tmp_path / ".local/share/lighthouse/review-records").resolve()


def test_data_dir_refuses_git_tree(tmp_path):
    repo = tmp_path / "repo"
    init_repo(repo)
    with pytest.raises(DataDirInRepoError, match="git work tree"):
        data_dir({"LIGHTHOUSE_DATA": str(repo / "data")})
    assert not (repo / "data").exists()


def test_data_dir_expands_user(tmp_path, monkeypatch):
    monkeypatch.setenv("HOME", str(tmp_path))
    assert data_dir({"LIGHTHOUSE_DATA": "~/r"}) == Path(tmp_path / "r").resolve()
