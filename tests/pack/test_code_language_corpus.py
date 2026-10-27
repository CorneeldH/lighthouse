"""Layer 1 over real code (#37): the stdlib stays clean, lighthouse's own code is English."""

import subprocess
from pathlib import Path

from tests.pack._testkit import load_hook_lib

cl = load_hook_lib("code_language")
REPO = Path(__file__).resolve().parents[2]
STDLIB = REPO / "tests" / "pack" / "fixtures" / "code_language" / "stdlib"


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


def test_stdlib_fixture_clean():
    files = sorted(STDLIB.glob("*.py"))
    assert len(files) == 10
    for path in files:
        assert cl.scan_python(path.read_text(encoding="utf-8")) == [], path


def test_go_english():
    assert [f for f in _go_findings() if not f.doubt] == []


def test_config_py_english():
    assert [f for f in _config_findings() if not f.doubt] == []


def test_hooks_english():
    assert [f for f in _hook_findings() if not f.doubt] == []


def test_pack_markdown_english():
    files = sorted((REPO / "pack").rglob("*.md"))
    assert files
    found = [
        f"{path.relative_to(REPO)}:{f.line}: {f.text}"
        for path in files
        for f in cl.scan_markdown(path.read_text(encoding="utf-8"))
        if not f.doubt  # a doubt ("OOM", "no-op") is not a finding; layer 2 decides
    ]
    assert not found, "\n".join(found)


def test_lighthouse_code_english():
    """The whole repo, as the hook sees it: lighthouse.toml decides scope and exemptions."""
    cfg = cl.config.load(REPO / "lighthouse.toml")
    assert cl.config.lookup(cfg, "project.code_language") == "en"
    assert {"pack/**", "internal/**", "cmd/**"} <= set(cl.config.lookup(cfg, "project.code_language_paths"))
    files = subprocess.run(
        ["git", "-C", REPO, "ls-files", "-z"], capture_output=True, text=True, check=True
    ).stdout.split("\0")
    found = [
        f"{rel}:{f.line}: {f.kind}: {f.text}"
        for rel in files
        if rel and (REPO / rel).is_file()
        for f in cl.check_file(REPO / rel, REPO).findings
    ]
    assert not found, "\n".join(found)
