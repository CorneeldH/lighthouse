"""tools/gen_code_language_freq.py: the denylist filter and the byte-stable encoding."""

import gzip
import importlib.util
from pathlib import Path

REPO = Path(__file__).resolve().parents[1]
SCRIPT = REPO / "tools" / "gen_code_language_freq.py"


def _load():
    spec = importlib.util.spec_from_file_location("gen_code_language_freq", SCRIPT)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def test_keep_drops_denied_substrings():
    gen = _load()
    patterns = ["corneel", "paia"]
    assert gen.keep("corneel", patterns) is False
    assert gen.keep("xpaiax", patterns) is False
    assert gen.keep("Paia", patterns) is False
    assert gen.keep("read", patterns) is True


def test_denied_patterns_skip_comments_and_blanks(tmp_path):
    gen = _load()
    path = tmp_path / "deny.txt"
    path.write_text("# x\n\nBeelink\n")
    assert gen.denied_patterns(path) == ["beelink"]


def test_encode_is_deterministic_and_sorted():
    gen = _load()
    rows = [("b", 10, 0), ("a", 74, 56)]
    first = gen.encode(rows)
    assert gen.encode(rows) == first
    assert gzip.decompress(first).decode() == "a\t74\t56\nb\t10\t0\n"
