"""create_labels.py: lists or creates the labels of the taxonomy file on the remote."""

import importlib.util
import re
import shutil
import subprocess
import sys

from tests.pack._testkit import CORE, install_core, tool_bin

SCRIPT_SRC = CORE / "scripts" / "create_labels.py"
TEMPLATE = CORE / "templates" / "labels.md"
NAMES = [
    "blocked-issue",
    "blocked-external",
    "needs-shaping",
    "needs-decision",
    "needs-hands",
    "needs-testing",
]
TOML = '[project]\nlanguage = "en"\n\n[forge]\nkind = "github"\nrepo = "Owner/repo"\n'
TOML_NO_FORGE = '[project]\nlanguage = "en"\n'
LABELS_PATH = "docs/developer/labels.md"


def _stub(remote: list[str], create_rc: int = 0) -> str:
    listing = "\\n".join(remote)
    return (
        "#!/bin/sh\n"
        'echo "$@" >> "$GH_LOG"\n'
        'case "$1 $2" in\n'
        f'  "api repos/Owner/repo/labels") printf "{listing}\\n"; exit 0;;\n'
        f'  "label create") exit {create_rc};;\n'
        "esac\n"
        "exit 0\n"
    )


def _setup(tmp_path, remote, toml=TOML, drop="", create_rc=0, taxonomy=True):
    root = tmp_path / "repo"
    install_core(root, toml)
    if taxonomy:
        target = root / LABELS_PATH
        target.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(TEMPLATE, target)
    log = tmp_path / "gh.log"
    log.write_text("", encoding="utf-8")
    stubs = {} if drop == "gh" else {"gh": _stub(remote, create_rc)}
    bin_dir = tool_bin(tmp_path / "bin", ("sh", "git"), drop=drop, stubs=stubs)
    env = {
        "PATH": str(bin_dir),
        "LIGHTHOUSE_REPO_ROOT": str(root),
        "GH_LOG": str(log),
        "HOME": str(tmp_path),
    }
    return root, env, log


def _run(root, env, *args):
    return subprocess.run(
        [sys.executable, str(root / ".lighthouse/scripts/create_labels.py"), *args],
        cwd=root, env=env, capture_output=True, text=True, check=False,
    )


def _creates(log):
    return [ln for ln in log.read_text(encoding="utf-8").splitlines() if "label create" in ln]


def test_parse_template():
    spec = importlib.util.spec_from_file_location("create_labels", SCRIPT_SRC)
    module = importlib.util.module_from_spec(spec)
    sys.path.insert(0, str(CORE / "lib"))
    try:
        spec.loader.exec_module(module)
    finally:
        sys.path.remove(str(CORE / "lib"))
    parsed = module.parse_taxonomy(TEMPLATE.read_text(encoding="utf-8"))
    assert sorted(n for n, _ in parsed) == sorted(NAMES)
    assert all(re.fullmatch(r"[0-9A-F]{6}", c) for _, c in parsed)
    assert "area:docs" not in [n for n, _ in parsed]


def test_lists_missing_without_create(tmp_path):
    root, env, log = _setup(tmp_path, ["needs-hands"])
    result = _run(root, env)
    assert result.returncode == 0, result.stderr
    lines = [ln for ln in result.stdout.splitlines() if ln.startswith("gh label create ")]
    assert len(lines) == 5
    assert all(" --repo Owner/repo --color " in ln for ln in lines)
    assert not any("needs-hands" in ln for ln in lines)
    assert "gh label create blocked-issue --repo Owner/repo --color D93F0B" in lines
    assert _creates(log) == []


def test_create_makes_only_missing(tmp_path):
    root, env, log = _setup(tmp_path, ["needs-hands"])
    result = _run(root, env, "--create")
    assert result.returncode == 0, result.stderr
    creates = _creates(log)
    assert len(creates) == 5
    assert not any("needs-hands" in ln for ln in creates)
    assert all("--color" in ln for ln in creates)
    assert "labels: created blocked-issue" in result.stdout


def test_existing_label_case_insensitive(tmp_path):
    root, env, log = _setup(tmp_path, ["Needs-Hands"])
    result = _run(root, env, "--create")
    assert result.returncode == 0, result.stderr
    assert not any("needs-hands" in ln for ln in _creates(log))


def test_all_present(tmp_path):
    root, env, log = _setup(tmp_path, NAMES)
    result = _run(root, env, "--create")
    assert result.returncode == 0
    assert "labels: all 6 present on Owner/repo" in result.stdout
    assert _creates(log) == []


def test_reads_labels_paginated(tmp_path):
    root, env, log = _setup(tmp_path, [])
    _run(root, env)
    api = [ln for ln in log.read_text(encoding="utf-8").splitlines() if ln.startswith("api")]
    assert api
    assert "--paginate" in api[0] and "repos/Owner/repo/labels" in api[0]


def test_gh_missing_prints_all_lines(tmp_path):
    root, env, _ = _setup(tmp_path, [], drop="gh")
    result = _run(root, env, "--create")
    assert result.returncode == 0, result.stderr
    assert "could not read" in result.stdout
    assert len([ln for ln in result.stdout.splitlines() if ln.startswith("gh label create ")]) == 6


def test_create_failure_prints_line(tmp_path):
    root, env, _ = _setup(tmp_path, [], create_rc=1)
    result = _run(root, env, "--create")
    assert result.returncode == 0, result.stderr
    lines = result.stdout.splitlines()
    for name in NAMES:
        assert any(ln.startswith("gh label create " + name + " ") for ln in lines)


def test_no_forge_repo_prints_all_lines(tmp_path):
    root, env, log = _setup(tmp_path, [], toml=TOML_NO_FORGE)
    result = _run(root, env, "--create")
    assert result.returncode == 0, result.stderr
    lines = [ln for ln in result.stdout.splitlines() if ln.startswith("gh label create ")]
    assert len(lines) == 6
    assert not any("--repo" in ln for ln in lines)
    assert _creates(log) == []


def test_missing_taxonomy_exits_2(tmp_path):
    root, env, _ = _setup(tmp_path, [], taxonomy=False)
    result = _run(root, env)
    assert result.returncode == 2
    assert LABELS_PATH in result.stderr


def test_remote_label_with_space(tmp_path):
    root, env, log = _setup(tmp_path, ["good first issue"] + NAMES)
    taxonomy = root / LABELS_PATH
    taxonomy.write_text(
        taxonomy.read_text(encoding="utf-8")
        + "\n| Label | Color |\n|---|---|\n| `good first issue` | `7057FF` |\n",
        encoding="utf-8",
    )
    result = _run(root, env, "--create")
    assert result.returncode == 0, result.stderr
    assert _creates(log) == []
    assert "labels: all 7 present on Owner/repo" in result.stdout
