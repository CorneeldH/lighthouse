"""Every file in pack/core has a provenance entry in pack/SOURCES.toml (spec, Opdeling (b))."""

import re
import tomllib
from pathlib import Path

REPO = Path(__file__).resolve().parents[1]
SOURCES = REPO / "pack" / "SOURCES.toml"
JUNK = {"__pycache__", ".pytest_cache", ".DS_Store"}


def _pack_files() -> set[str]:
    files = set()
    for p in (REPO / "pack" / "core").rglob("*"):
        rel = p.relative_to(REPO)
        if p.is_file() and not JUNK & set(rel.parts) and p.suffix != ".pyc":
            files.add(rel.as_posix())
    return files


def _entries() -> list[dict]:
    return tomllib.loads(SOURCES.read_text())["file"]


def test_every_pack_file_has_a_source():
    paths = [e["path"] for e in _entries()]
    assert len(paths) == len(set(paths)), "a path appears twice"
    missing = sorted(_pack_files() - set(paths))
    assert not missing, f"no entry in pack/SOURCES.toml: {missing}"


def test_no_entry_without_file():
    gone = sorted({e["path"] for e in _entries()} - _pack_files())
    assert not gone, f"entry without a file: {gone}"


def test_entry_is_new_or_has_a_source():
    for e in _entries():
        if e.get("new") is True:
            assert "source_sha" not in e and "source_path" not in e, e["path"]
            continue
        assert re.fullmatch(r"[0-9a-f]{40}", e.get("source_sha", "")), e["path"]
        src = e.get("source_path")
        assert (isinstance(src, str) and src) or (
            isinstance(src, list) and src and all(isinstance(s, str) and s for s in src)
        ), e["path"]
