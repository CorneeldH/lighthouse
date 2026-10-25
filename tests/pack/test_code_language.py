"""`pack/core/lib/code_language.py`, as installed (#26): recognising Dutch text and names."""

import ast
from pathlib import Path

import pytest

from tests.pack._testkit import MINIMAL_TOML, load_hook_lib

cl = load_hook_lib("code_language")
REPO = Path(__file__).resolve().parents[2]


def test_split_name():
    assert cl.split_name("leesBestand") == ["lees", "bestand"]
    assert cl.split_name("read_file_2") == ["read", "file"]
    assert cl.split_name("HTTPServer") == ["http", "server"]


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
    found = cl.check_file(_write(root, "a.py"), root).findings
    assert [(f.line, f.kind) for f in found] == [(1, "comment")]


def test_doubt_is_not_a_finding(tmp_path):
    root = _repo(tmp_path)
    path = _write(root, "a.py", "legacy_processed_lijst_weg = 1\n")
    inspection = cl.check_file(path, root)
    assert inspection.findings == []
    assert [d.text for d in inspection.doubts] == ["legacy_processed_lijst_weg"]
    assert all(d.doubt for d in inspection.doubts)


def test_finding_layer(tmp_path, capsys):
    root = _repo(tmp_path)
    path = _write(root, "a.py")
    findings = cl.check_file(path, root).findings
    assert findings
    assert {f.layer for f in findings} == {"layer 1"}
    assert cl.main([str(path), str(root)]) == 1
    out = capsys.readouterr().out.splitlines()
    assert out[1] == f"{path}:1: comment: dit is niet goed [layer 1]"


@pytest.fixture
def no_frequency_cache():
    cl.code_language_score.frequencies.cache_clear()
    yield
    cl.code_language_score.frequencies.cache_clear()


def test_missing_data_is_cannot_check(tmp_path, monkeypatch, no_frequency_cache):
    root = _repo(tmp_path)
    monkeypatch.setattr(cl.code_language_score, "FREQ_FILE", "nope")
    with pytest.raises(cl.CannotCheck):
        cl.check_file(_write(root, "a.py"), root)


def test_check_off_without_key(tmp_path):
    (tmp_path / "lighthouse.toml").write_text(MINIMAL_TOML, encoding="utf-8")
    assert cl.check_file(_write(tmp_path, "a.py"), tmp_path).findings == []


def test_check_off_without_toml(tmp_path):
    assert cl.check_file(_write(tmp_path, "a.py"), tmp_path).findings == []


def test_check_other_extension(tmp_path):
    root = _repo(tmp_path)
    assert cl.check_file(_write(root, "a.txt"), root).findings == []


def test_check_exempt_glob(tmp_path):
    root = _repo(tmp_path, 'code_language_exempt = ["tests/fixtures/*"]\n')
    assert cl.check_file(_write(root, "tests/fixtures/a/b.py"), root).findings == []
    assert cl.check_file(_write(root, "tests/other/b.py"), root).findings != []


def test_check_manifest_skips_managed(tmp_path):
    root = _repo(tmp_path)
    manifest = '[[unit]]\npath = ".lighthouse/lib/lighthouse_lib/x.py"\n'
    _write(root, ".lighthouse/manifest.toml", manifest)
    assert cl.check_file(_write(root, ".lighthouse/lib/lighthouse_lib/x.py"), root).findings == []
    assert cl.check_file(_write(root, ".lighthouse/lib/lighthouse_lib/y.py"), root).findings != []


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
    assert cl.check_file(path, root).findings == []
    path.write_bytes(b"\xef\xbb\xbf# lees het bestand\nx = 1\n")
    assert [f.kind for f in cl.check_file(path, root).findings] == ["comment"]


def test_check_module_name(tmp_path):
    root = _repo(tmp_path)
    found = cl.check_file(_write(root, "lees_bestand.py", "x = 1\n"), root).findings
    assert [(f.line, f.kind, f.text) for f in found] == [(1, "name", "lees_bestand")]


# Blocks that really are English, line -> why.
ENGLISH_BLOCKS: dict[int, str] = {
    35: "English: explains the sys.path line below it",
}
# Dutch blocks layer 1 cannot catch at all. Not a way to hide a missed block.
UNDETECTABLE_BLOCKS: dict[int, str] = {
    462: "only 'geparsed', a Dutch word that is in neither list and does not split",
}
# Dutch blocks layer 1 only doubts: a doubt is not a finding, layer 2 decides.
DOUBT_BLOCKS: dict[int, str] = {}


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
    scanned = [f for f in cl.scan_python(source) if f.kind in ("comment", "docstring")]
    found = {f.line for f in scanned if not f.doubt}
    doubted = {f.line for f in scanned if f.doubt}
    lines = source.splitlines()
    block_lines = _blocks(source)
    for number in (*ENGLISH_BLOCKS, *UNDETECTABLE_BLOCKS, *DOUBT_BLOCKS):
        assert number not in found, f"line {number} is now found: remove it from the lists"
        assert number in block_lines, f"line {number} is no block start: {lines[number - 1]!r}"
    for number in DOUBT_BLOCKS:
        assert number in doubted, f"line {number} is no longer a doubt: remove it from the list"
    for number in UNDETECTABLE_BLOCKS:
        assert number not in doubted, f"line {number} is now a doubt: move it to DOUBT_BLOCKS"
    listed = {*ENGLISH_BLOCKS, *UNDETECTABLE_BLOCKS, *DOUBT_BLOCKS}
    missed = {n: k for n, k in block_lines.items() if n not in found and n not in listed}
    assert not missed, "\n".join(f"{n}: {k}: {lines[n - 1].strip()}" for n, k in sorted(missed.items()))


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


# --- Markdown ---

NL_DOC = """---
description: Lees het bestand en geef de regels terug.
---

# Het bestand lezen

Dit is niet goed en het
bestand is niet gevonden.

- Lees het bestand en geef de regels terug.

| name | explanation |
| --- | --- |
| a | niet gevonden |
"""
EN_DOC = """---
description: Read the file and return the lines.
---

# Reading the file

This is not good and the
file was not found.

- Read the file and return the lines.

| name | explanation |
| --- | --- |
| a | not found |
"""


def _prose(source):
    return [(f.line, f.kind) for f in cl.scan_markdown(source)]


def test_markdown_units():
    assert _prose(NL_DOC) == [(2, "prose"), (5, "prose"), (7, "prose"), (10, "prose"), (14, "prose")]
    assert cl.scan_markdown(EN_DOC) == []


def test_markdown_folded_frontmatter():
    source = "---\ndescription: >\n  Lees het bestand en\n  geef de regels terug.\n---\n"
    assert _prose(source) == [(2, "prose")]


def test_markdown_code_is_not_language():
    source = (
        "```\nlees_bestand = 1\n```\n\n"
        "~~~\nlees_bestand = 1\n~~~\n\n"
        "Intro line:\n\n"
        "    controleer_poort(invoer)\n\n"
        "Call `lees_bestand` in this sentence and read [the file](docs/lees-bestand.md).\n\n"
        "See https://example.com/lees-het-bestand-en-geef-de-regels-terug for details.\n"
    )
    assert cl.scan_markdown(source) == []


def test_markdown_marker():
    assert cl.scan_markdown("Dit is niet goed. <!-- lighthouse: allow-language -->\n") == []


# --- scope ---


def test_scope_dirs():
    assert cl.scope_dirs("pack/core/skills/x/SKILL.md", ["pack/**"]) == ["core", "skills", "x"]
    assert cl.scope_dirs("pack/core/skills/x/SKILL.md", ["pack/core/skills/*/SKILL.md"]) == ["x"]
    assert cl.scope_dirs("docs/a.md", ["pack/**"]) is None
    assert cl.scope_dirs("pack/notulen.md", ["pack/**"]) == []
    assert cl.scope_dirs("docs/x.md", ["docs/x.md"]) == []


def test_scope_dirs_union_in_path_order():
    globs = ["pack/core/**", "pack/**"]
    assert cl.scope_dirs("pack/core/a/b.md", globs) == ["core", "a"]
    assert cl.scope_dirs("pack/core/a/b.md", ["pack/**", "pack/core/**"]) == ["core", "a"]


SCOPED = 'code_language_paths = ["pack/**"]\n'
PARAGRAPH = "Dit is niet goed en het bestand is niet gevonden.\n"


def test_scope_markdown_and_toml(tmp_path):
    root = _repo(tmp_path, SCOPED)
    found = cl.check_file(_write(root, "pack/a.md", PARAGRAPH), root).findings
    assert [(f.line, f.kind) for f in found] == [(1, "prose")]
    found = cl.check_file(_write(root, "pack/notulen.toml", 'a = "niet goed"\n'), root).findings
    assert [(f.kind, f.text) for f in found] == [("name", "notulen")]
    found = cl.check_file(_write(root, "pack/bestanden/a.toml", "x = 1\n"), root).findings
    assert [(f.line, f.kind, f.text) for f in found] == [(1, "directory", "bestanden")]
    assert cl.check_file(_write(root, "docs/a.md", PARAGRAPH), root).findings == []
    assert cl.check_file(_write(root, "docs/notulen.toml", "x = 1\n"), root).findings == []
    found = cl.check_file(_write(root, "docs/lees_bestand.py", "x = 1\n"), root).findings
    assert [(f.kind, f.text) for f in found] == [("name", "lees_bestand")]


def test_scope_binary_only_name(tmp_path):
    root = _repo(tmp_path, SCOPED)
    found = cl.check_file(_write_bytes(root, "pack/plaatje.png", b"\xff\xd8"), root).findings
    assert [(f.kind, f.text) for f in found] == [("name", "plaatje")]
    assert cl.check_file(_write_bytes(root, "pack/logo.png", b"\xff\xd8"), root).findings == []


def _write_bytes(root, relative, content):
    path = root / relative
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_bytes(content)
    return path


def test_scope_off_without_code_language(tmp_path):
    toml = MINIMAL_TOML.replace('language = "en"\n', f'language = "en"\n{SCOPED}')
    (tmp_path / "lighthouse.toml").write_text(toml, encoding="utf-8")
    inspection = cl.check_file(_write(tmp_path, "pack/a.md", PARAGRAPH), tmp_path)
    assert inspection.findings == [] and inspection.doubts == []


def test_out_of_scope_does_not_load_data(tmp_path, monkeypatch):
    root = _repo(tmp_path, SCOPED)

    def boom():
        raise AssertionError("frequencies loaded")

    monkeypatch.setattr(cl.code_language_score, "frequencies", boom)
    inspection = cl.check_file(_write(root, "docs/a.toml", "x = 1\n"), root)
    assert inspection.findings == [] and inspection.doubts == []
    with pytest.raises(AssertionError, match="frequencies loaded"):
        cl.check_file(_write(root, "pack/notulen.toml", "x = 1\n"), root)


def test_report_names_path(tmp_path, capsys):
    root = _repo(tmp_path, SCOPED)
    path = _write(root, "pack/bestanden/a.toml", "x = 1\n")
    assert cl.main([str(path), str(root)]) == 1
    out = capsys.readouterr().out.splitlines()
    assert f"{path}:1: directory: bestanden [layer 1]" in out
    assert "rename the file" in out[-1]
    assert "code_language_exempt" in out[-1]
