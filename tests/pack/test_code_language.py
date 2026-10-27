"""`pack/core/lib/code_language.py`, as installed (#26): recognising Dutch text and names."""

import ast
from pathlib import Path

import pytest

from tests.pack._fake_llm import FakeEndpoint
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


def test_python_dutch_comment_line_in_english_block():
    """A comment line is a boundary: no full stop is needed before the Dutch line."""
    source = (
        "# Read the configuration file from disk and return all of its lines\n"
        "# Raise an error when the file does not exist or cannot be read\n"
        "# dit werkt niet\n"
        "x = 1\n"
    )
    found = cl.scan_python(source)
    assert [(f.line, f.kind, f.doubt) for f in found] == [(1, "comment", False)]


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
UNDETECTABLE_BLOCKS: dict[int, str] = {}
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


def test_markdown_dutch_sentence_in_english_paragraph():
    source = (
        "This function reads the configuration file from disk and returns all of its lines\n"
        "as a list of strings. It raises an error when the file does not exist or cannot\n"
        "be read. Dit is een tijdelijke oplossing. The caller handles the error.\n"
    )
    found = cl.scan_markdown(source)
    assert [(f.line, f.kind, f.doubt) for f in found] == [(1, "prose", False)]


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


# --- layer 2 in the checker (#37, decision 10-12, 19) ---

LAYER1_LINE = "lees_bestand = 1\n"
DOUBT_LINE = "legacy_processed_lijst_weg = 2\n"
KEY_ENV = "LLM_TEST_KEY"


def _table(url, key_env=KEY_ENV):
    return f'\n[code_language_llm]\nurl = "{url}"\nmodels = ["m1"]\nkey_env = "{key_env}"\n'


def _session_toml(tmp_path, table=""):
    """The `lighthouse.toml` of the session's repo, the one the hook runs from."""
    session = tmp_path / "session"
    session.mkdir(exist_ok=True)
    (session / "lighthouse.toml").write_text(MINIMAL_TOML + table, encoding="utf-8")
    return session / "lighthouse.toml"


def _llm_repo(tmp_path, url, key_env=KEY_ENV):
    """A repo with `code_language` on; the chain lives in the session's `lighthouse.toml`."""
    _session_toml(tmp_path, _table(url, key_env))
    return _repo(tmp_path)


@pytest.fixture
def llm_key(monkeypatch):
    monkeypatch.setenv(KEY_ENV, "sekrit")


def _resolve(root, path, tmp_path, under_gates=False):
    inspection = cl.check_file(path, root, tmp_path / "session" / "lighthouse.toml")
    return cl.resolve(inspection, path, under_gates=under_gates, state_dir=tmp_path / "state")


def _lines_layers(found):
    return [(f.line, f.layer) for f in found]


def test_not_en_becomes_finding(tmp_path, llm_key):
    with FakeEndpoint(verdict="NOT-EN") as fake:
        root = _llm_repo(tmp_path, fake.url)
        path = _write(root, "a.py", LAYER1_LINE + DOUBT_LINE)
        found, notice = _resolve(root, path, tmp_path)
    assert _lines_layers(found) == [(1, "layer 1"), (2, "layer 2 (m1)")]
    assert notice is None
    prompt = fake.requests[0]["messages"][0]["content"]
    assert "legacy_processed_lijst_weg" in prompt
    assert "lees_bestand" not in prompt


def test_en_drops_doubt(tmp_path, llm_key):
    with FakeEndpoint(verdict="EN") as fake:
        root = _llm_repo(tmp_path, fake.url)
        path = _write(root, "a.py", LAYER1_LINE + DOUBT_LINE)
        found, notice = _resolve(root, path, tmp_path)
    assert _lines_layers(found) == [(1, "layer 1")]
    assert notice is None
    assert len(fake.requests) == 1


def test_layer1_only_no_call(tmp_path, llm_key):
    with FakeEndpoint() as fake:
        root = _llm_repo(tmp_path, fake.url)
        path = _write(root, "a.py", LAYER1_LINE)
        found, notice = _resolve(root, path, tmp_path)
    assert _lines_layers(found) == [(1, "layer 1")]
    assert notice is None
    assert fake.requests == []


def test_foreign_chain_is_never_used(tmp_path, llm_key):
    """The written file's repo cannot choose where the session's key goes (R7)."""
    with FakeEndpoint() as foreign, FakeEndpoint(verdict="EN") as own:
        root = _repo(tmp_path, "")
        with (root / "lighthouse.toml").open("a", encoding="utf-8") as toml:
            toml.write(_table(foreign.url))
        path = _write(root, "a.py", LAYER1_LINE + DOUBT_LINE)
        _session_toml(tmp_path)
        found, notice = _resolve(root, path, tmp_path)
        assert (_lines_layers(found), notice) == ([(1, "layer 1")], None)
        assert foreign.requests == []
        _session_toml(tmp_path, _table(own.url))
        found, notice = _resolve(root, path, tmp_path)
    assert foreign.requests == []
    assert len(own.requests) == 1
    assert own.headers[0]["Authorization"] == "Bearer sekrit"
    assert _lines_layers(found) == [(1, "layer 1")]
    assert notice is None


def test_no_chain_is_silent(tmp_path, monkeypatch):
    root = _repo(tmp_path)
    path = _write(root, "a.py", LAYER1_LINE + DOUBT_LINE)
    asked = []
    monkeypatch.setattr(cl.code_language_llm, "ask", lambda *a, **k: asked.append(a))
    found, notice = _resolve(root, path, tmp_path)
    assert _lines_layers(found) == [(1, "layer 1")]
    assert notice is None
    assert asked == []


@pytest.mark.parametrize(
    ("mode", "key", "reason"),
    [
        ("http500", True, "m1: HTTP 500"),
        ("garbage", True, "m1: unreadable answer"),
        ("answer", False, "key: "),
    ],
)
def test_failed_chain_notice(tmp_path, monkeypatch, mode, key, reason):
    if key:
        monkeypatch.setenv(KEY_ENV, "sekrit")
    else:
        monkeypatch.delenv(KEY_ENV, raising=False)
    with FakeEndpoint(default=mode) as fake:
        root = _llm_repo(tmp_path, fake.url)
        path = _write(root, "a.py", LAYER1_LINE + DOUBT_LINE)
        found, notice = _resolve(root, path, tmp_path)
    assert _lines_layers(found) == [(1, "layer 1")]
    head, doubt_line = notice.split("\n")
    assert doubt_line == f"{path}:2: name: legacy_processed_lijst_weg"
    if key:
        assert notice == (
            f"code-language: layer 2 did not answer ({reason}); 1 doubtful units not checked:\n"
            f"{path}:2: name: legacy_processed_lijst_weg"
        )
    else:
        assert head.startswith("code-language: layer 2 did not answer (key: ")
        assert head.endswith("); 1 doubtful units not checked:")
        assert fake.requests == []


def test_under_gates_skips(tmp_path, llm_key):
    with FakeEndpoint() as fake:
        root = _llm_repo(tmp_path, fake.url)
        path = _write(root, "a.py", LAYER1_LINE + DOUBT_LINE)
        found, notice = _resolve(root, path, tmp_path, under_gates=True)
    assert fake.requests == []
    assert _lines_layers(found) == [(1, "layer 1")]
    assert notice == (
        "code-language: NOTICE — layer 2 did not answer (layer 2 skipped under "
        "bash-write-gates); 1 doubtful units not checked:\n"
        f"{path}:2: name: legacy_processed_lijst_weg"
    )


def test_notice_truncates_long_text(tmp_path, monkeypatch):
    root = _llm_repo(tmp_path, "http://unused.invalid/v1")
    path = _write(root, "a.py")
    long = cl.Finding(3, "comment", "x" * 70, doubt=True, unit="x" * 70)
    inspection = cl.Inspection([], [long], cl.code_language_llm.Chain("u", ("m1",), None, None))
    _found, notice = cl.resolve(inspection, path, under_gates=True, state_dir=tmp_path)
    assert notice.split("\n")[1] == f"{path}:3: comment: {'x' * 60}"


def test_bad_chain_is_cannot_check(tmp_path):
    root = _repo(tmp_path)
    session = _session_toml(tmp_path, '\n[code_language_llm]\nurl = "http://h/v1"\nmodels = []\n')
    for content in (LAYER1_LINE + DOUBT_LINE, "x = 1\n"):
        with pytest.raises(cl.CannotCheck):
            cl.check_file(_write(root, "a.py", content), root, session)


def test_bad_chain_spares_unchecked_files(tmp_path):
    """A wrong table blocks only files that are checked, never a doc or an exempt file (R9)."""
    root = _repo(tmp_path, 'code_language_exempt = ["vendor/**"]\n')
    session = _session_toml(
        tmp_path, '\n[code_language_llm]\nurl = "http://h/v1"\nmodels = ["m1"]\n'
        'env_file = "/nonexistent/lighthouse-test.env"\n'
    )
    for relative in ("docs/notes.md", "notes.toml", "vendor/lees.py"):
        inspection = cl.check_file(_write(root, relative), root, session)
        assert (inspection.findings, inspection.doubts) == ([], [])
    with pytest.raises(cl.CannotCheck, match="env_file does not exist"):
        cl.check_file(_write(root, "a.py", "x = 1\n"), root, session)


def test_main_notice_contract(tmp_path, capsys, monkeypatch):
    monkeypatch.setenv(cl.UNDER_GATES_ENV, "1")
    root = _llm_repo(tmp_path, "http://unused.invalid/v1")
    monkeypatch.setenv("LIGHTHOUSE_TOML", str(tmp_path / "session" / "lighthouse.toml"))
    path = _write(root, "a.py", DOUBT_LINE)
    assert cl.main([str(path), str(root)]) == 0
    out = capsys.readouterr().out
    assert out.startswith("code-language: NOTICE — layer 2 did not answer")
    path = _write(root, "b.py", LAYER1_LINE + DOUBT_LINE)
    assert cl.main([str(path), str(root)]) == 1
    out = capsys.readouterr().out
    assert out.startswith(f"code-language: {path} is not English")
    assert "\ncode-language: NOTICE — layer 2 did not answer" in out
