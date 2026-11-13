"""`pack/core/lib/code_language.py`, as installed (#26): recognising Dutch text and names."""

import ast
import base64
import json
from pathlib import Path

import pytest

from tests.pack._fake_llm import FakeEndpoint
from tests.pack._testkit import MINIMAL_TOML, load_hook_lib

cl = load_hook_lib("code_language")
REPO = Path(__file__).resolve().parents[2]
FIXTURES = REPO / "tests" / "pack" / "fixtures" / "code_language"


def _load(name):
    """A shared fixture: the Go tests read the same files (#276)."""
    return json.loads((FIXTURES / name).read_text(encoding="utf-8"))


# --- scanning a source: `scan.json` ---

SCANNERS = {
    "python": cl.scan_python,
    "shell": cl.scan_shell,
    "go": cl.scan_go,
    "markdown": cl.scan_markdown,
}


@pytest.mark.parametrize("case", _load("scan.json"), ids=lambda case: case["name"])
def test_scan(case):
    scan = SCANNERS[case["lang"]]
    if case.get("cannot_check"):
        with pytest.raises(cl.CannotCheck):
            scan(case["source"])
        return
    found = [[f.line, f.kind, f.text, f.doubt] for f in scan(case["source"])]
    assert found == case["want"]


# --- scope: `scope_dirs.json` ---


@pytest.mark.parametrize(
    "case", _load("scope_dirs.json"), ids=lambda case: f"{case['relative']}-{case['globs']}"
)
def test_scope_dirs(case):
    assert cl.scope_dirs(case["relative"], case["globs"]) == case["want"]


# --- checking a file: `check.json` ---


def _check_root(tmp_path, case):
    """The repo of a case: the files in a temporary directory, `code_language` on unless `off`."""
    root = tmp_path / "repo"
    root.mkdir()
    code_language = "" if case.get("off") else 'code_language = "en"\n'
    head = f'language = "en"\n{code_language}{case["toml_extra"]}'
    (root / "lighthouse.toml").write_text(
        MINIMAL_TOML.replace('language = "en"\n', head), encoding="utf-8"
    )
    for relative, text in case["files"].items():
        _write(root, relative, text)
    for relative, encoded in case.get("files_b64", {}).items():
        path = root / relative
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(base64.b64decode(encoded))
    return root


@pytest.mark.parametrize("case", _load("check.json"), ids=lambda case: case["name"])
def test_check_file(tmp_path, case):
    root = _check_root(tmp_path, case)
    extra = []
    if "session_table" in case:
        extra = [_session_toml(tmp_path, case["session_table"])]
    path = root / case["path"]
    if case.get("cannot_check"):
        with pytest.raises(cl.CannotCheck):
            cl.check_file(path, root, *extra)
        return
    inspection = cl.check_file(path, root, *extra)
    assert [[f.line, f.kind, f.text] for f in inspection.findings] == case["findings"]
    assert [[f.line, f.kind, f.text] for f in inspection.doubts] == case["doubts"]


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


def test_check_off_without_toml(tmp_path):
    assert cl.check_file(_write(tmp_path, "a.py"), tmp_path).findings == []


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
    assert out[0] == f"code-language-check: {path} is not English"
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


SCOPED = 'code_language_paths = ["pack/**"]\n'


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
            f"code-language-check: layer 2 did not answer ({reason}); 1 doubtful units not checked:\n"
            f"{path}:2: name: legacy_processed_lijst_weg"
        )
    else:
        assert head.startswith("code-language-check: layer 2 did not answer (key: ")
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
        "code-language-check: NOTICE — layer 2 did not answer (layer 2 skipped under "
        "bash-write-check); 1 doubtful units not checked:\n"
        f"{path}:2: name: legacy_processed_lijst_weg"
    )


def test_notice_truncates_long_text(tmp_path, monkeypatch):
    root = _llm_repo(tmp_path, "http://unused.invalid/v1")
    path = _write(root, "a.py")
    long = cl.Finding(3, "comment", "x" * 70, doubt=True, unit="x" * 70)
    inspection = cl.Inspection([], [long], cl.code_language_llm.Chain("u", ("m1",), None, None))
    _found, notice = cl.resolve(inspection, path, under_gates=True, state_dir=tmp_path)
    assert notice.split("\n")[1] == f"{path}:3: comment: {'x' * 60}"


def test_bad_chain_names_the_missing_env_file(tmp_path):
    root = _repo(tmp_path)
    session = _session_toml(
        tmp_path, '\n[code_language_llm]\nurl = "http://h/v1"\nmodels = ["m1"]\n'
        'env_file = "/nonexistent/lighthouse-test.env"\n'
    )
    with pytest.raises(cl.CannotCheck, match="env_file does not exist"):
        cl.check_file(_write(root, "a.py", "x = 1\n"), root, session)


def test_main_notice_contract(tmp_path, capsys, monkeypatch):
    monkeypatch.setenv(cl.UNDER_GATES_ENV, "1")
    root = _llm_repo(tmp_path, "http://unused.invalid/v1")
    monkeypatch.setenv("LIGHTHOUSE_TOML", str(tmp_path / "session" / "lighthouse.toml"))
    path = _write(root, "a.py", DOUBT_LINE)
    assert cl.main([str(path), str(root)]) == 0
    out = capsys.readouterr().out
    assert out.startswith("code-language-check: NOTICE — layer 2 did not answer")
    path = _write(root, "b.py", LAYER1_LINE + DOUBT_LINE)
    assert cl.main([str(path), str(root)]) == 1
    out = capsys.readouterr().out
    assert out.startswith(f"code-language-check: {path} is not English")
    assert "\ncode-language-check: NOTICE — layer 2 did not answer" in out
