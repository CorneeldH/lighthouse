"""tests/_shard.py and the hooks in tests/conftest.py: one CI job runs one part of the suite."""

import types

import pytest

from tests import conftest
from tests._shard import ENV, in_shard, parse_shard

NODEIDS = [f"tests/test_{i}.py::test_{j}" for i in range(20) for j in range(10)]


def test_unset_means_no_sharding():
    assert parse_shard(None) is None


@pytest.mark.parametrize(("value", "expected"), [("1/3", (1, 3)), ("3/3", (3, 3)), ("1/1", (1, 1))])
def test_parse_valid(value, expected):
    assert parse_shard(value) == expected


@pytest.mark.parametrize("value", ["", "0/3", "4/3", "3", "a/b", "2/0", " 2/3", "1/3/5"])
def test_parse_invalid(value):
    with pytest.raises(ValueError, match=ENV):
        parse_shard(value)


def test_every_test_in_exactly_one_shard():
    for nodeid in NODEIDS:
        assert sum(in_shard(nodeid, i, 3) for i in (1, 2, 3)) == 1, nodeid


def test_shards_are_stable_and_not_empty():
    first = [[n for n in NODEIDS if in_shard(n, i, 3)] for i in (1, 2, 3)]
    assert first == [[n for n in NODEIDS if in_shard(n, i, 3)] for i in (1, 2, 3)]
    assert all(first)


@pytest.mark.parametrize(
    ("nodeid", "expected"),
    [
        ("tests/test_domains.py::test_classify", 2),
        ("tests/test_x.py::test_b", 3),
        ("tests/test_y.py::test_c", 1),
    ],
)
def test_known_nodeid_lands_in_its_crc32_shard(nodeid, expected):
    """Literal shard numbers: hash() differs per process and would fail here."""
    assert [i for i in (1, 2, 3) if in_shard(nodeid, i, 3)] == [expected]


def _config():
    deselected = []
    config = types.SimpleNamespace(
        stash=pytest.Stash(),
        hook=types.SimpleNamespace(pytest_deselected=lambda items: deselected.append(list(items))),
    )
    return config, deselected


def _items():
    return [types.SimpleNamespace(nodeid=n) for n in NODEIDS]


def test_configure_rejects_invalid_env(monkeypatch):
    monkeypatch.setenv(ENV, "0/3")
    config, _ = _config()
    with pytest.raises(pytest.UsageError, match=ENV):
        conftest.pytest_configure(config)


def test_modifyitems_keeps_all_without_env(monkeypatch):
    monkeypatch.delenv(ENV, raising=False)
    config, deselected = _config()
    conftest.pytest_configure(config)
    items = _items()
    conftest.pytest_collection_modifyitems(config, items)
    assert [i.nodeid for i in items] == NODEIDS
    assert deselected == []


def test_modifyitems_keeps_only_its_shard(monkeypatch):
    monkeypatch.setenv(ENV, "2/3")
    config, deselected = _config()
    conftest.pytest_configure(config)
    items = _items()
    conftest.pytest_collection_modifyitems(config, items)
    kept = [n for n in NODEIDS if in_shard(n, 2, 3)]
    assert [i.nodeid for i in items] == kept
    assert len(deselected) == 1
    assert [i.nodeid for i in deselected[0]] == [n for n in NODEIDS if n not in kept]
