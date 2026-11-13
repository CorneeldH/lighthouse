"""`pack/core/lib/code_language_score.py`, as installed (#37): layer 1, the score per unit."""

import json
from pathlib import Path

import pytest

from tests.pack._testkit import load_hook_lib

score = load_hook_lib("code_language_score")
FIXTURES = Path(__file__).resolve().parents[2] / "tests" / "pack" / "fixtures" / "code_language"


def _load(name):
    """A shared fixture: the Go tests read the same files (#276)."""
    return json.loads((FIXTURES / name).read_text(encoding="utf-8"))


SCORE = _load("score.json")


# Measured against the spec appendix: these are not found with the given constants.


@pytest.mark.parametrize(("name", "want"), SCORE["split_name"].items())
def test_split_name(name, want):
    assert score.split_name(name) == want


@pytest.mark.parametrize(("name", "want"), SCORE["names"].items())
def test_judge_name(name, want):
    assert score.judge_name(name) == want


@pytest.mark.parametrize(
    "case", SCORE["texts"], ids=lambda case: f"{case['line_breaks']}-{case['text'][-30:]}"
)
def test_judge_text(case):
    assert score.judge_text(case["text"], line_breaks=case["line_breaks"]) == case["want"]


@pytest.mark.parametrize(("word", "want"), SCORE["dutch_split"].items())
def test_dutch_split_real_table(word, want):
    assert score.dutch_split(word) is want


@pytest.mark.parametrize(("word", "want"), SCORE["dutch_participle"].items())
def test_dutch_participle_real_table(word, want):
    assert score.dutch_participle(word) is want


@pytest.fixture
def table(monkeypatch):
    def install(entries, jargon=frozenset()):
        monkeypatch.setattr(score, "frequencies", lambda: entries)
        monkeypatch.setattr(score, "jargon", lambda: frozenset(jargon))

    return install


CASES = [
    (group, case)
    for group in _load("score_table.json")
    for case in group["cases"]
]


@pytest.mark.parametrize(
    ("group", "case"),
    CASES,
    ids=[f"{group['name']}-{i}" for i, (group, _case) in enumerate(CASES)],
)
def test_table_case(table, group, case):
    table({word: tuple(entry) for word, entry in group["table"].items()}, jargon=group["jargon"])
    kwargs = {"line_breaks": case["line_breaks"]} if "line_breaks" in case else {}
    assert getattr(score, case["fn"])(case["arg"], **kwargs) == case["want"]


def test_missing_data_is_data_error(monkeypatch):
    score.frequencies.cache_clear()
    monkeypatch.setattr(score, "FREQ_FILE", "nope.tsv.gz")
    try:
        with pytest.raises(score.DataError):
            score.frequencies()
    finally:
        monkeypatch.undo()
        score.frequencies.cache_clear()


def test_corrupt_data_is_data_error(monkeypatch, tmp_path):
    import gzip
    import zlib

    rows = "".join(f"w{i}\t{i % 70}\t{i % 50}\n" for i in range(3000))
    body = bytearray(gzip.compress(rows.encode()))
    body[20] ^= 0xFF  # one corrupted byte early in the deflate stream
    with pytest.raises(zlib.error):  # guards the fixture: it must trigger zlib.error
        gzip.decompress(bytes(body))
    (tmp_path / "bad.tsv.gz").write_bytes(bytes(body))
    monkeypatch.setattr(score, "_data_path", lambda filename: tmp_path / "bad.tsv.gz")
    score.frequencies.cache_clear()
    try:
        with pytest.raises(score.DataError):
            score.frequencies()
    finally:
        monkeypatch.undo()
        score.frequencies.cache_clear()
