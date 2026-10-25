"""Layer 1 over real code (#37): the stdlib stays clean, lighthouse's own code is English."""

from pathlib import Path

from tests.pack._testkit import load_hook_lib

cl = load_hook_lib("code_language")
REPO = Path(__file__).resolve().parents[2]
STDLIB = REPO / "tests" / "pack" / "fixtures" / "code_language" / "stdlib"

# Real Dutch in lighthouse itself, word -> why it stays. A word that no longer occurs in
# any finding fails `test_known_dutch_still_occurs`, so the list shrinks with the code.
KNOWN_DUTCH = {
    "ontwerpkeuze": "Dutch in Go until #58 translates it",
    "ontwerpkeuzes": "Dutch in Go until #58 translates it",
    "besluit": "Dutch in config.py and in hook header comments until #58 translates it",
}


def _go_files():
    files = [*(REPO / "internal").rglob("*.go"), *(REPO / "cmd").rglob("*.go")]
    assert files
    return files


def _hook_files():
    hooks = sorted((REPO / "pack" / "core" / "hooks").glob("*.sh"))
    assert hooks
    return hooks


def _scan(scan, paths):
    return [f for path in paths for f in scan(path.read_text(encoding="utf-8"))]


def _go_findings():
    return _scan(cl.scan_go, _go_files())


def _config_findings():
    return _scan(cl.scan_python, [REPO / "pack" / "core" / "lib" / "config.py"])


def _hook_findings():
    return _scan(cl.scan_shell, _hook_files())


def _dutch_words(finding):
    """The lower-case words a finding spans: its name parts, or the words of its unit."""
    if finding.kind == "name":
        return set(cl.split_name(finding.text))
    return set(cl.code_language_score.words(finding.unit or finding.text))


def _unknown(findings):
    return [
        f"{f.line}: {f.kind}: {f.text}"
        for f in findings
        if not f.doubt and not _dutch_words(f) & KNOWN_DUTCH.keys()
    ]


def test_stdlib_fixture_clean():
    files = sorted(STDLIB.glob("*.py"))
    assert len(files) == 10
    for path in files:
        assert cl.scan_python(path.read_text(encoding="utf-8")) == [], path


def test_go_english():
    assert _unknown(_go_findings()) == []


def test_config_py_english():
    assert _unknown(_config_findings()) == []


def test_hooks_english():
    assert _unknown(_hook_findings()) == []


def test_known_dutch_still_occurs():
    """Over all three corpora together: `besluit` lives in config.py and the hook headers,
    `ontwerpkeuze` in Go. Read from the source text: layer 1 only doubts `besluit` inside
    English prose and does not see `ontwerpkeuze` there at all."""
    paths = [*_go_files(), REPO / "pack" / "core" / "lib" / "config.py", *_hook_files()]
    used = set().union(*(set(cl.code_language_score.words(p.read_text(encoding="utf-8"))) for p in paths))
    assert {word for word in KNOWN_DUTCH if word not in used} == set()
