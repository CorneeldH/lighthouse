"""`pack/core/lib/code_language.py`, as installed (#26): recognising Dutch text and names."""

import re
from pathlib import Path

import pytest

from tests.pack._testkit import load_hook_lib

cl = load_hook_lib("code_language")
REPO = Path(__file__).resolve().parents[2]

BANNED = [
    "die", "met", "door", "over", "van", "map", "pad",
    "is", "in", "op", "been", "had", "was", "hem",
    "de", "te", "er", "dat", "mag", "tot",  # English abbreviations in names and comments
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


def _kinds(source):
    return [(f.line, f.kind, f.text) for f in cl.scan_python(source)]


def test_python_comment_block():
    source = "x = 1\n# first line\n# dit is niet goed\ny = 2\n"
    assert _kinds(source) == [(2, "comment", "first line")]


def test_python_docstring():
    source = 'def f():\n    """Lees het bestand."""\n    return 1\n'
    assert _kinds(source) == [(2, "docstring", "Lees het bestand.")]


def test_python_names():
    source = "def lees_bestand(pad_naam):\n    resultaat = 1\n\n\nclass Gevonden:\n    pass\n"
    found = {(f.kind, f.text) for f in cl.scan_python(source)}
    assert ("name", "lees_bestand") in found
    assert ("name", "pad_naam") in found
    assert ("name", "Gevonden") in found


def test_python_strings_are_not_read():
    assert cl.scan_python('x = "het bestand"\n') == []


def test_python_use_site_is_not_a_finding():
    assert cl.scan_python("from elders import lees_bestand\nlees_bestand()\n") == []


def test_python_marker():
    assert cl.scan_python("# niet goed  lighthouse: allow-language\n") == []
    assert cl.scan_python("lees_bestand = 1  # lighthouse: allow-language\n") == []
    source = 'def f():\n    """Lees het bestand.\n\n    lighthouse: allow-language\n    """\n'
    assert cl.scan_python(source) == []


def test_python_syntax_error_is_cannot_check():
    with pytest.raises(cl.CannotCheck):
        cl.scan_python("def f(:\n")


def test_python_english_clean():
    source = (REPO / "pack" / "core" / "lib" / "config.py").read_text(encoding="utf-8")
    assert cl.scan_python(source) == []


def _shell(source):
    return [(f.line, f.kind, f.text) for f in cl.scan_shell(source)]


def test_shell_comment():
    assert _shell("echo hi\n# lees het bestand\n") == [(2, "comment", "lees het bestand")]


def test_shell_shebang_skipped():
    assert _shell("#!/usr/bin/env bash\n# read the file\n") == []


def test_shell_hash_in_string_and_param():
    source = 'echo "# het bestand"\necho \'# het bestand\'\nn=${#lines}\necho $#\n'
    assert _shell(source) == []


def test_shell_heredoc_is_data():
    source = "cat <<EOF\n# het bestand\nEOF\n"
    assert _shell(source) == []
    assert _shell("cat <<-'EOF'\n\t# het bestand\n\tEOF\n") == []
    assert _shell("cat <<EOF\n# het bestand\nEOF\n# het bestand\n") == [
        (4, "comment", "het bestand")
    ]


def test_shell_names():
    source = (
        "lees_bestand() {\n"
        "  local bestand\n"
        "  regels=3\n"
        "}\n"
        "function controleer {\n"
        "  export NAAM_BESTAND=x\n"
        "}\n"
    )
    assert _shell(source) == [
        (1, "name", "lees_bestand"),
        (2, "name", "bestand"),
        (3, "name", "regels"),
        (5, "name", "controleer"),
        (6, "name", "NAAM_BESTAND"),
    ]


def test_shell_use_site_is_not_a_finding():
    assert _shell('echo "$regels" lees_bestand\n') == []


def test_shell_marker():
    assert _shell("# niet goed  lighthouse: allow-language\n") == []
    assert _shell("regels=3  # lighthouse: allow-language\n") == []


def _go(source):
    return [(f.line, f.kind, f.text) for f in cl.scan_go(source)]


def test_go_line_and_block_comment():
    assert _go("// lees het bestand\nx := 1\n") == [(1, "comment", "lees het bestand")]
    assert _go("/* niet\n gelezen */\nx := 1\n") == [(1, "comment", "niet")]


def test_go_comment_in_string():
    source = 'a := "http://x"\nb := `// niet het bestand`\nc := \'/\'\nd := "/* het bestand"\n'
    assert _go(source) == []


def test_go_names():
    source = (
        "package bestanden\n"
        "func leesBestand(regel string) {}\n"
        "func (r *T) Controleer() {}\n"
        "type Melding struct { Naam string }\n"
        "var fout error\n"
        "const Leeg = 0\n"
        "gevonden := 1\n"
        "var (\n"
        "\tbron int\n"
        ")\n"
    )
    found = {(f.line, f.text) for f in cl.scan_go(source)}
    assert found >= {
        (1, "bestanden"),
        (2, "leesBestand"),
        (2, "regel"),
        (3, "Controleer"),
        (4, "Melding"),
        (4, "Naam"),
        (5, "fout"),
        (6, "Leeg"),
        (7, "gevonden"),
        (9, "bron"),
    }
    assert all(f.kind == "name" for f in cl.scan_go(source))


def test_go_multiline_params_and_fields():
    source = "func f(\n\tregel string,\n\ta, b int,\n) {}\ntype T struct {\n\tNaam string\n}\n"
    assert {(f.line, f.text) for f in cl.scan_go(source)} == {
        (2, "regel"),
        (6, "Naam"),
    }


def test_go_marker():
    assert _go("// niet goed  lighthouse: allow-language\n") == []
    assert _go("gevonden := 1 // lighthouse: allow-language\n") == []


def test_go_english_clean():
    files = [*(REPO / "internal").rglob("*.go"), *(REPO / "cmd").rglob("*.go")]
    assert files
    for path in files:
        assert cl.scan_go(path.read_text(encoding="utf-8")) == [], path
