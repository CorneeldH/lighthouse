"""Replayed-call fixtures of the skill commands: `fixtures/scripts/<command>/cases.json`.

A case has `name`, `argv`, `config` (the text of `lighthouse.toml`), `env`,
optional `setup` (the steps of `build_fixture_setup`), `calls` (the scripted
`gh` calls, in order), or instead `forge_calls` (the requests to the forge API,
replayed by `ForgeReplay` on a local port that the binary reaches through
`LIGHTHOUSE_GITHUB_API_URL` with a dummy `GH_TOKEN`; `gh` is then scripted to be
never called) and `expect` (`stdout`, `stderr`, `code`, optionally
`stdout_contains`, `stderr_contains` and `files`). A case with `"only": "go"`
does not run here. `git` runs for real against the `setup` repo; `gh` is a
fake (`_fakegh.py`) and the forge a local replay. The Go twin is `internal/scriptcase`.

`COMMANDS` says what to start per command directory: the argv of a case is
appended. `_self` is the smoke test of this runner: its argv is the whole
command.
"""

import json
import os
import subprocess
from pathlib import Path

import pytest

from tests.pack._testkit import ForgeReplay, build_fixture_setup, fake_gh_bin, fill_fixture, lighthouse_dir

FIXTURES = Path(__file__).parent / "fixtures" / "scripts"
COMMANDS: dict[str, list[str]] = {
    "_self": [],
    "unblock-dependents": [str(lighthouse_dir() / "lighthouse"), "unblock-dependents"],
    "post-artefact": [str(lighthouse_dir() / "lighthouse"), "post-artefact"],
    "worktree-audit": [str(lighthouse_dir() / "lighthouse"), "worktree-audit"],
}


def _cases() -> list:
    params = []
    for path in sorted(FIXTURES.glob("*/cases.json")):
        for case in json.loads(path.read_text(encoding="utf-8")):
            if case.get("only") == "go":
                continue
            params.append(pytest.param(path.parent.name, case, id=f"{path.parent.name}::{case['name']}"))
    return params


def _run(command: str, case: dict, tmp_path: Path, replay: ForgeReplay | None = None):
    root = tmp_path / "root"
    root.mkdir()
    build_fixture_setup(root, case.get("setup", []))
    fill = lambda value: fill_fixture(value, str(root))  # noqa: E731
    if replay is not None:
        # The placeholders name commits of the setup repo: fill them once it exists.
        replay.calls = fill(case["forge_calls"])
    bin_dir, log = fake_gh_bin(tmp_path, fill(case.get("calls", [])))
    config = tmp_path / "lighthouse.toml"
    config.write_text(fill(case.get("config", "")), encoding="utf-8")
    home = tmp_path / "home"
    home.mkdir()
    env = dict(
        os.environ,
        TZ="UTC",
        HOME=str(home),
        LIGHTHOUSE_TOML=str(config),
        PATH=f"{bin_dir}{os.pathsep}{os.environ['PATH']}",
    )
    # A controller run that runs these cases would add the git calls of workflow-push-gate.
    env.pop("LIGHTHOUSE_CONTROLLER_RUN", None)
    if replay is not None:
        env.pop("GITHUB_TOKEN", None)
        env.update(GH_TOKEN="dummy", LIGHTHOUSE_GITHUB_API_URL=replay.url)
    env.update(fill(case.get("env", {})))
    proc = subprocess.run(
        [*COMMANDS[command], *fill(case["argv"])],
        cwd=root,
        env=env,
        capture_output=True,
        text=True,
        timeout=120,
    )
    return root, log, proc


@pytest.mark.parametrize(("command", "case"), _cases())
def test_script_case(command, case, tmp_path):
    if "forge_calls" in case:
        with ForgeReplay([]) as replay:
            root, log, proc = _run(command, case, tmp_path, replay)
        assert replay.problems == [], proc.stderr
        assert replay.made == len(case["forge_calls"]), "scripted forge calls were not all made"
    else:
        root, log, proc = _run(command, case, tmp_path)
    assert log.read_text(encoding="utf-8") == ""
    count = int((tmp_path / "fakegh-count").read_text(encoding="utf-8"))
    assert count == len(case.get("calls", [])), "scripted gh calls were not all made"
    expect = case["expect"]
    fill = lambda value: fill_fixture(value, str(root))  # noqa: E731
    assert proc.returncode == expect["code"], proc.stderr
    if "stdout" in expect:
        assert proc.stdout == fill(expect["stdout"])
    if "stderr" in expect:
        assert proc.stderr == fill(expect["stderr"])
    for text in expect.get("stdout_contains", []):
        assert fill(text) in proc.stdout
    for text in expect.get("stderr_contains", []):
        assert fill(text) in proc.stderr
    for rel, content in expect.get("files", {}).items():
        assert (root / rel).read_text(encoding="utf-8") == fill(content)


def test_fake_gh_rejects_a_reversed_order(tmp_path):
    calls = [
        {"argv": ["gh", "a"], "stdin": None, "stdout": "A", "stderr": "", "code": 0},
        {"argv": ["gh", "b"], "stdin": None, "stdout": "B", "stderr": "", "code": 0},
    ]
    bin_dir, log = fake_gh_bin(tmp_path, calls)
    env = dict(os.environ, PATH=f"{bin_dir}{os.pathsep}{os.environ['PATH']}")
    proc = subprocess.run(
        ["sh", "-c", "gh b; gh a"], env=env, capture_output=True, text=True
    )
    # `gh b` deviated (exit 97, not counted); the shell goes on with `gh a`.
    assert "call 1: expected ['gh', 'a'], got ['gh', 'b']" in log.read_text()
    assert (tmp_path / "fakegh-count").read_text() == "1"


def test_forge_replay_rejects_a_wrong_order_and_a_wrong_body():
    import urllib.error
    import urllib.request

    calls = [
        {"method": "GET", "path": "/a", "status": 200, "body": {"a": 1}},
        {"method": "POST", "path": "/b", "request_body": {"x": 1}, "status": 201},
    ]

    def send(url, method="GET", data=None):
        request = urllib.request.Request(url, data=data, method=method)
        try:
            with urllib.request.urlopen(request) as response:
                return response.status
        except urllib.error.HTTPError as error:
            return error.code

    with ForgeReplay(calls) as replay:
        assert send(f"{replay.url}/b", "POST", b"{}") == 599  # not the next call
        assert send(f"{replay.url}/a") == 200
        assert send(f"{replay.url}/b", "POST", b'{"x": 2}') == 599  # wrong body
        assert send(f"{replay.url}/b", "POST", b'{"x": 1}') == 201
        assert send(f"{replay.url}/c") == 599  # list ran out
    assert replay.made == 2
    assert len(replay.problems) == 3
    assert "call 1: expected GET /a, got POST /b" in replay.problems[0]
