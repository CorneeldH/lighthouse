"""The canary of `python-imports-gate.sh` (#383): the real hook, the real binary and the real
deptry against a small repository per blocking rule ID, in a fake environment (a venv with fake
`.dist-info`s, offline). The same fixtures, `testdata/python-imports-gate/<id>/` and
`testdata/extra/<name>/`, are run by the Go canary (internal/importcheck/canary_test.go).

Without `uv` the tests skip locally and fail when `CI` is set: a canary that skips on CI reads
as green.
"""

import json
import os
import re
import shutil
import subprocess
from datetime import datetime, timedelta, timezone
from pathlib import Path

import pytest

from tests.pack._testkit import git, stop_payload

pytestmark = pytest.mark.usefixtures("lighthouse_bin")

ROOT = Path(__file__).resolve().parents[3]
DATA = Path(__file__).parent / "testdata"
HOOK = ROOT / "pack" / "python" / "hooks" / "python-imports-gate.sh"
RULE_FIXTURES = sorted(p.name for p in (DATA / "python-imports-gate").iterdir() if p.is_dir())
EXTRA_FIXTURES = sorted(
    p.name
    for p in (DATA / "extra").iterdir()
    if p.is_dir() and json.loads((p / "canary.json").read_text())["hook"] == "python"
)
TOML = '[project]\nlanguage = "en"\n\n[forge]\nkind = "github"\nrepo = "Acme/widgets"\n'


def _need_uv() -> None:
    if shutil.which("uv") is None:
        if os.environ.get("CI"):
            pytest.fail("uv is not on PATH, and the canary must run on CI")
        pytest.skip("uv is not on PATH")


def _blocking_rules() -> list[str]:
    lines = [l for l in HOOK.read_text().splitlines() if l.startswith("BLOCKING_RULES=")]
    assert len(lines) == 1
    return sorted(re.fullmatch(r'BLOCKING_RULES="([^"]*)"', lines[0]).group(1).split(","))


def test_one_fixture_per_blocking_rule():
    """Fails on a blocking rule without a fixture, and on a fixture of a rule the hook does not
    block on."""
    assert RULE_FIXTURES == _blocking_rules()


def _build_repo(tmp_path: Path, fixture: Path, case: dict) -> Path:
    """A copy of the fixture with `git init` and a commit, then the fake venv in the copy: in the
    repository itself `turn-roots` and Discover do not see the fixture (testdata is skipped)."""
    repo = (tmp_path / "repo").resolve()
    shutil.copytree(fixture, repo, ignore=shutil.ignore_patterns("canary.json"))
    (repo / ".gitignore").write_text(".venv/\n")
    (repo / "lighthouse.toml").write_text(TOML)
    subprocess.run(["git", "init", "-q"], cwd=repo, check=True)
    git(repo, "add", "-A")
    git(repo, "commit", "-q", "-m", "fixture")
    subprocess.run(["uv", "venv", "--quiet", ".venv"], cwd=repo, check=True)
    (site,) = (repo / ".venv" / "lib").glob("python*/site-packages")
    for dist in case["dists"]:
        info = site / f"{dist['name']}-1.0.dist-info"
        info.mkdir()
        requires = "".join(f"Requires-Dist: {r}\n" for r in dist["requires"])
        (info / "METADATA").write_text(f"Metadata-Version: 2.1\nName: {dist['name']}\nVersion: 1.0\n{requires}")
        (info / "top_level.txt").write_text(dist["name"] + "\n")
    return repo


def _run_hook(tmp_path: Path, repo: Path, touched: str) -> subprocess.CompletedProcess:
    # The prompt lies in the future: the commit of the fixture is then before the turn, and only
    # the Write below touches a path.
    prompt = datetime.now(timezone.utc) + timedelta(hours=1)

    def stamp(at: datetime) -> str:
        return at.strftime("%Y-%m-%dT%H:%M:%S.000Z")

    entries = [
        {"type": "user", "timestamp": stamp(prompt), "cwd": str(repo), "message": {"role": "user", "content": "go"}},
        {
            "type": "assistant",
            "timestamp": stamp(prompt + timedelta(seconds=1)),
            "cwd": str(repo),
            "message": {
                "role": "assistant",
                "content": [
                    {"type": "tool_use", "id": "toolu_1", "name": "Write", "input": {"file_path": str(repo / touched)}}
                ],
            },
        },
    ]
    transcript = tmp_path / "transcript.jsonl"
    transcript.write_text("".join(json.dumps(e) + "\n" for e in entries))
    uv_cache = subprocess.run(["uv", "cache", "dir"], capture_output=True, text=True, check=True).stdout.strip()
    return subprocess.run(
        [str(HOOK)],
        input=json.dumps(stop_payload(repo, transcript)),
        capture_output=True,
        text=True,
        cwd=repo,
        env={
            **os.environ,
            "CLAUDE_PROJECT_DIR": str(repo),
            "LIGHTHOUSE_REPO_ROOT": str(repo),
            "XDG_CACHE_HOME": str(tmp_path / "xdg-cache"),  # the usage state: no alarm of this machine
            "UV_CACHE_DIR": uv_cache,
        },
        check=False,
    )


def _canary(tmp_path: Path, fixture: Path) -> None:
    _need_uv()
    case = json.loads((fixture / "canary.json").read_text())
    repo = _build_repo(tmp_path, fixture, case)
    result = _run_hook(tmp_path, repo, case["touch"])
    assert result.returncode == case["exit"], (result.stdout, result.stderr)
    if case["exit"] == 2:
        # A finding line, not the ID alone: the hook's own header names every blocking ID.
        assert re.search(rf"(?m)^\S+:\d+: {re.escape(case['id'])} ", result.stderr), result.stderr
    else:
        # A clean check says nothing; a systemMessage is "not checked" or "could not check".
        assert result.stdout == ""


@pytest.mark.parametrize("rule", RULE_FIXTURES)
def test_blocks_on_each_rule(tmp_path, rule):
    fixture = DATA / "python-imports-gate" / rule
    assert json.loads((fixture / "canary.json").read_text())["id"] == rule
    _canary(tmp_path, fixture)


@pytest.mark.parametrize("name", EXTRA_FIXTURES)
def test_green_counterpart_and_extras(tmp_path, name):
    """`numpy` in [project.dependencies] is clean; so is `try: import httpx` with httpx in an
    extra (AC 3)."""
    _canary(tmp_path, DATA / "extra" / name)
