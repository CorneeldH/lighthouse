"""`pack/core/lib/code_language.py`, as installed (#26): recognising Dutch text and names."""

import re
from pathlib import Path

from tests.pack._testkit import load_hook_lib

cl = load_hook_lib("code_language")
REPO = Path(__file__).resolve().parents[2]

BANNED = [
    "die", "met", "door", "over", "van", "map", "pad",
    "is", "in", "op", "been", "had", "was", "hem",
]  # fmt: skip


def test_split_name():
    assert cl.split_name("leesBestand") == ["lees", "bestand"]
    assert cl.split_name("read_file_2") == ["read", "file"]
    assert cl.split_name("HTTPServer") == ["http", "server"]


def test_banned_words_absent():
    for word in BANNED:
        assert word not in cl.FUNCTION_WORDS
        assert word not in cl.CONTENT_WORDS


def test_dutch_text():
    assert cl.is_dutch_text("Het bestand wordt niet gelezen.")
    assert cl.is_dutch_text("# --- post (taak 7): bron 1, claims, dispatch, uitvoer ---")
    assert not cl.is_dutch_text("Read the file and return its lines.")


def test_dutch_name():
    assert cl.is_dutch_name("lees_bestand")
    assert cl.is_dutch_name("gevondenRegels")
    assert not cl.is_dutch_name("read_file")
    assert not cl.is_dutch_name("foundLines")
    assert not cl.is_dutch_name("map_path")


_STRING = re.compile(r"\"(?:\\.|[^\"\\])*\"|`[^`]*`|'(?:\\.|[^'\\])*'")


def test_english_corpus_has_no_dutch_words():
    hits = []
    files = [*(REPO / "internal").rglob("*.go"), *(REPO / "cmd").rglob("*.go")]
    assert files
    for path in files:
        for number, raw in enumerate(path.read_text(encoding="utf-8").splitlines(), 1):
            line = _STRING.sub("", raw)
            if "//" in line:
                code, comment = line.split("//", 1)
                if cl.is_dutch_text(comment):
                    hits.append(f"{path}:{number}: comment: {comment.strip()}")
            else:
                code = line
            for name in re.findall(r"[A-Za-z_][A-Za-z0-9_]*", line):
                if cl.is_dutch_name(name):
                    hits.append(f"{path}:{number}: name: {name}")
    assert not hits, "\n".join(hits[:20])
