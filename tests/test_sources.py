"""Every file in a package under pack/ has a provenance entry in pack/SOURCES.toml
(spec, Opdeling (b) and (c))."""

import re
import tomllib
from pathlib import Path

REPO = Path(__file__).resolve().parents[1]
SOURCES = REPO / "pack" / "SOURCES.toml"
JUNK = {"__pycache__", ".pytest_cache", ".DS_Store"}


def _pack_files(repo: Path = REPO) -> set[str]:
    """Every file in every package directory: what `go:embed all:*/*` puts in the binary."""
    files = set()
    for p in (repo / "pack").glob("*/**/*"):
        rel = p.relative_to(repo)
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


SOURCE_FIELDS = ("source_repo", "source_sha", "source_path")


def check_entry(e: dict) -> str | None:
    """None when the entry has exactly one provenance, else the reason it does not."""
    path = e.get("path", "?")
    has_source = [f for f in SOURCE_FIELDS if f in e]
    kinds = [k for k, on in (("new", "new" in e), ("pin", "pin" in e), ("source", bool(has_source))) if on]
    if len(kinds) != 1:
        return f"{path}: exactly one of new, pin or the source fields, got {kinds or 'none'}"
    if "new" in e:
        return None if e["new"] is True else f"{path}: new must be true"
    if "pin" in e:
        return None if isinstance(e["pin"], str) and e["pin"] else f"{path}: pin must be a non-empty string"
    if len(has_source) != len(SOURCE_FIELDS):
        return f"{path}: source fields must come together, got {has_source}"
    if not (isinstance(e["source_repo"], str) and re.fullmatch(r"[\w.-]+/[\w.-]+", e["source_repo"])):
        return f"{path}: source_repo must be owner/repo"
    if not (isinstance(e["source_sha"], str) and re.fullmatch(r"[0-9a-f]{40}", e["source_sha"])):
        return f"{path}: source_sha must be 40 hex"
    if not (isinstance(e["source_path"], str) and e["source_path"]):
        return f"{path}: source_path must be one non-empty string"
    return None


def test_entry_has_exactly_one_provenance():
    problems = [m for e in _entries() if (m := check_entry(e))]
    assert not problems, problems


SHA = "0" * 40


def test_check_entry_accepts_each_kind():
    assert check_entry({"path": "p", "new": True}) is None
    assert check_entry({"path": "p", "pin": "core/skills/x"}) is None
    assert check_entry({"path": "p", "source_repo": "o/r", "source_sha": SHA, "source_path": "a/b"}) is None


def test_check_entry_rejects_bad_entries():
    bad = [
        {"path": "p"},
        {"path": "p", "new": True, "pin": "k"},
        {"path": "p", "new": True, "source_sha": SHA},
        {"path": "p", "source_sha": SHA, "source_path": "a"},
        {"path": "p", "source_repo": "o/r", "source_sha": SHA, "source_path": ["a", "b"]},
        {"path": "p", "source_repo": "o/r", "source_sha": SHA, "source_path": ""},
        {"path": "p", "source_repo": "o/r", "source_sha": "abc", "source_path": "a"},
        {"path": "p", "source_repo": "nope", "source_sha": SHA, "source_path": "a"},
    ]
    for e in bad:
        assert check_entry(e), e


def test_rejects_new_and_pin_in_a_written_file(tmp_path):
    f = tmp_path / "SOURCES.toml"
    f.write_text('[[file]]\npath = "pack/core/a"\nnew = true\npin = "core/skills/a"\n')
    entries = tomllib.loads(f.read_text())["file"]
    assert check_entry(entries[0])


def test_header_does_not_mention_paia():
    head = "\n".join(SOURCES.read_text().splitlines()[:10])
    assert "paia" not in head.lower()


def test_walks_every_package(tmp_path):
    (tmp_path / "pack" / "core").mkdir(parents=True)
    (tmp_path / "pack" / "core" / "a").write_text("a")
    (tmp_path / "pack" / "zz" / "lib").mkdir(parents=True)
    (tmp_path / "pack" / "zz" / "lib" / "x").write_text("x")
    (tmp_path / "pack" / "SOURCES.toml").write_text("")
    assert _pack_files(tmp_path) == {"pack/core/a", "pack/zz/lib/x"}
