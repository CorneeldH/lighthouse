"""`pack/core/lib/code_language.py`, as installed (#26): recognising Dutch text and names."""

import ast
import re
from pathlib import Path

import pytest

from tests.pack._testkit import MINIMAL_TOML, load_hook_lib

cl = load_hook_lib("code_language")
REPO = Path(__file__).resolve().parents[2]

BANNED = [
    "die", "met", "door", "over", "van", "map", "pad",
    "is", "in", "op", "been", "had", "was", "hem",
    "de", "te", "er", "dat", "mag", "tot",  # English abbreviations in names and comments
    "fout", "kent",  # "file out" and "Kent Beck"
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
            for name in re.findall(r"[A-Za-z_][A-Za-z0-9_]*", code):
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
        "var leeg error\n"
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
        (5, "leeg"),
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


# --- checking a file ---

DUTCH = "# dit is niet goed\nx = 1\n"
FIXTURE = REPO / "tests" / "pack" / "fixtures" / "code_language" / "bash_writes_nl.py"


def _repo(tmp_path, extra=""):
    toml = MINIMAL_TOML.replace('language = "en"\n', f'language = "en"\ncode_language = "en"\n{extra}')
    (tmp_path / "lighthouse.toml").write_text(toml, encoding="utf-8")
    return tmp_path


def _write(root, relative, content=DUTCH):
    path = root / relative
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(content, encoding="utf-8")
    return path


def test_check_finds_dutch(tmp_path):
    root = _repo(tmp_path)
    found = cl.check_file(_write(root, "a.py"), root)
    assert [(f.line, f.kind) for f in found] == [(1, "comment")]


def test_check_off_without_key(tmp_path):
    (tmp_path / "lighthouse.toml").write_text(MINIMAL_TOML, encoding="utf-8")
    assert cl.check_file(_write(tmp_path, "a.py"), tmp_path) == []


def test_check_off_without_toml(tmp_path):
    assert cl.check_file(_write(tmp_path, "a.py"), tmp_path) == []


def test_check_other_extension(tmp_path):
    root = _repo(tmp_path)
    assert cl.check_file(_write(root, "a.txt"), root) == []


def test_check_exempt_glob(tmp_path):
    root = _repo(tmp_path, 'code_language_exempt = ["tests/fixtures/*"]\n')
    assert cl.check_file(_write(root, "tests/fixtures/a/b.py"), root) == []
    assert cl.check_file(_write(root, "tests/other/b.py"), root) != []


def test_check_manifest_skips_managed(tmp_path):
    root = _repo(tmp_path)
    manifest = '[[unit]]\npath = ".lighthouse/lib/lighthouse_lib/x.py"\n'
    _write(root, ".lighthouse/manifest.toml", manifest)
    assert cl.check_file(_write(root, ".lighthouse/lib/lighthouse_lib/x.py"), root) == []
    assert cl.check_file(_write(root, ".lighthouse/lib/lighthouse_lib/y.py"), root) != []


def test_check_unreadable_manifest_is_cannot_check(tmp_path):
    root = _repo(tmp_path)
    _write(root, ".lighthouse/manifest.toml", "[[unit\n")
    with pytest.raises(cl.CannotCheck):
        cl.check_file(_write(root, "a.py"), root)


def test_check_config_error_is_cannot_check(tmp_path):
    root = _repo(tmp_path)
    (root / "lighthouse.toml").write_text(
        MINIMAL_TOML.replace('language = "en"\n', 'language = "en"\ncode_language = "nl"\n'),
        encoding="utf-8",
    )
    with pytest.raises(cl.CannotCheck):
        cl.check_file(_write(root, "a.py"), root)


def test_check_not_utf8_is_cannot_check(tmp_path):
    root = _repo(tmp_path)
    path = root / "a.py"
    path.write_bytes(b"# \xff\xfe\n")
    with pytest.raises(cl.CannotCheck):
        cl.check_file(path, root)


def test_python_nul_byte_is_cannot_check():
    with pytest.raises(cl.CannotCheck):
        cl.scan_python("x = 1\x00\n")


def test_check_bom_is_not_content(tmp_path):
    root = _repo(tmp_path)
    path = root / "a.py"
    path.write_bytes(b"\xef\xbb\xbf# read the file\nx = 1\n")
    assert cl.check_file(path, root) == []
    path.write_bytes(b"\xef\xbb\xbf# lees het bestand\nx = 1\n")
    assert [f.kind for f in cl.check_file(path, root)] == ["comment"]


def test_shell_english_corpus():
    hooks = sorted((REPO / "pack" / "core" / "hooks").glob("*.sh"))
    assert hooks
    for path in hooks:
        assert cl.scan_shell(path.read_text(encoding="utf-8")) == [], path


def test_check_module_name(tmp_path):
    root = _repo(tmp_path)
    found = cl.check_file(_write(root, "lees_bestand.py", "x = 1\n"), root)
    assert [(f.line, f.kind, f.text) for f in found] == [(1, "name", "lees_bestand")]


# Blocks that really are English, line -> why.
ENGLISH_BLOCKS: dict[int, str] = {
    35: "English: explains the sys.path line below it",
}
# Dutch blocks no word list can catch: every Dutch word in them (van, is, met) is banned
# because it is also English or common in English code. Not a way to hide a missed block.
UNDETECTABLE_BLOCKS: dict[int, str] = {
    72: "only van / is / met as Dutch words",
}


def _blocks(source):
    """First line of every comment block and of every docstring."""
    lines = {}
    previous = -2
    for number, line in enumerate(source.splitlines(), 1):
        if line.lstrip().startswith("#") and not line.startswith("#!"):
            if previous != number - 1:
                lines[number] = "comment"
            previous = number
    for node in ast.walk(ast.parse(source)):
        if isinstance(node, (ast.Module, ast.ClassDef, ast.FunctionDef, ast.AsyncFunctionDef)):
            if ast.get_docstring(node):
                lines[node.body[0].lineno] = "docstring"
    return lines


def test_dutch_corpus_found():
    source = FIXTURE.read_text(encoding="utf-8")
    found = {f.line for f in cl.scan_python(source) if f.kind in ("comment", "docstring")}
    lines = source.splitlines()
    for number in (*ENGLISH_BLOCKS, *UNDETECTABLE_BLOCKS):
        assert number not in found, f"line {number} is now found: remove it from the lists"
    block_lines = _blocks(source)
    for number in (*ENGLISH_BLOCKS, *UNDETECTABLE_BLOCKS):
        assert number in block_lines, f"line {number} is no block start: {lines[number - 1]!r}"
    missed = {
        number: kind
        for number, kind in _blocks(source).items()
        if number not in found and number not in ENGLISH_BLOCKS and number not in UNDETECTABLE_BLOCKS
    }
    assert not missed, "\n".join(
        f"{n}: {k}: {source.splitlines()[n - 1].strip()}" for n, k in sorted(missed.items())
    )


def test_report_explains_module_name_finding(tmp_path, capsys):
    root = _repo(tmp_path)
    path = _write(root, "lees_bestand.py", "x = 1\n")
    assert cl.main([str(path), str(root)]) == 1
    out = capsys.readouterr().out.splitlines()
    assert out[-2].startswith("The whole file must be English.")
    assert "rename the file" in out[-1]
    assert "code_language_exempt" in out[-1]

    path = _write(root, "b.py")
    assert cl.main([str(path), str(root)]) == 1
    assert "rename the file" not in capsys.readouterr().out


def test_main_output(tmp_path, capsys, monkeypatch):
    root = _repo(tmp_path)
    many = "".join(f"# dit is niet goed\nx{n} = 1\n" for n in range(25))
    path = _write(root, "a.py", many)
    assert cl.main([str(path), str(root)]) == 1
    out = capsys.readouterr().out.splitlines()
    assert out[0] == f"code-language: {path} is not English"
    assert out[1].startswith(f"{path}:1: comment: ")
    assert len([line for line in out if ": comment: " in line]) == 20
    assert out[-2] == "… and 5 more"
    assert out[-1].startswith("The whole file must be English.")
    assert "lighthouse: allow-language" in out[-1]

    path = _write(root, "b.py", "x = 1\n")
    assert cl.main([str(path), str(root)]) == 0
    assert capsys.readouterr().out == ""

    path = _write(root, "c.py", "def f(:\n")
    assert cl.main([str(path), str(root)]) == 2
    captured = capsys.readouterr()
    assert captured.out == ""
    assert "COULD NOT CHECK" in captured.err

    def boom(*_):
        raise RecursionError("deep")

    monkeypatch.setattr(cl, "check_file", boom)
    assert cl.main([str(path), str(root)]) == 2
    assert "deep" in capsys.readouterr().err
