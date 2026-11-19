"""`pack/core/workflows/unblock-dependents.yml`: the release lane, filled by `init`.

Workflows read no TOML (spec, Portabiliteit): every project value is a `@@<key>@@`
placeholder that `init` fills from the config reader. In a project the file lands as
`.github/workflows/lighthouse-unblock-dependents.yml`; `close-issue` looks for the run
under that name and by the issue number at the end of its `run-name`.
"""

from __future__ import annotations

import os
import re
import shutil
import subprocess
from pathlib import Path

import pytest


from tests.pack._testkit import CORE, config_kinds

ROOT = Path(__file__).resolve().parents[2]
WORKFLOW = CORE / "workflows" / "unblock-dependents.yml"
PLACEHOLDER = re.compile(r"@@([a-z_.]+)@@")


def _text() -> str:
    return WORKFLOW.read_text(encoding="utf-8")


def _lines() -> list[str]:
    return [line.strip() for line in _text().splitlines()]


def test_runs_on_from_config() -> None:
    assert "runs-on: @@ci.runs_on@@" in _lines()


def test_bot_names_from_config() -> None:
    text = _text()
    assert "client-id: ${{ vars.@@ci.bot_client_id_var@@ }}" in text
    assert "private-key: ${{ secrets.@@ci.bot_private_key_secret@@ }}" in text


def test_default_branch_from_config() -> None:
    assert "github.ref == 'refs/heads/@@forge.default_branch@@' &&" in _lines()


def test_run_name_ends_on_the_issue_number() -> None:
    (run_name,) = [line for line in _lines() if line.startswith("run-name:")]
    assert re.search(r' #\$\{\{ [^}]+ \}\}"$', run_name), run_name


def test_calls_the_binary() -> None:
    text = _text()
    assert 'run: lighthouse unblock-dependents --issue "$ISSUE"' in _lines()
    assert "uv " not in text and "setup-uv" not in text
    assert "python" not in text.lower().replace("no python", "")
    assert "unblock_dependents.py" not in text


def _step_run(name: str) -> str:
    """The `run:` block of the step `name`, dedented as the runner hands it to bash."""
    lines = _text().splitlines()
    start = lines.index(f"      - name: {name}")
    body: list[str] = []
    in_run = False
    for line in lines[start + 1 :]:
        if line.startswith(("      - ", "      #")):
            break
        if line.strip() == "run: |":
            in_run = True
        elif in_run:
            body.append(line[10:])
    while body and not body[-1].strip():
        body.pop()
    assert body, name
    return "\n".join(body) + "\n"


def _fake_tool(directory: Path, name: str, script: str) -> None:
    path = directory / name
    path.write_text("#!/bin/bash\n" + script, encoding="utf-8")
    path.chmod(0o755)


def _get_lighthouse(
    tmp_path: Path, *, version: str, runner_os: str, arch: str, cmd: bool, sha256sum: bool = True, base_path: str | None = None
) -> tuple[subprocess.CompletedProcess[str], Path, list[str]]:
    """Run the `Get lighthouse` block in a tmp checkout with fake gh, sha256sum and go."""
    checkout = tmp_path / "checkout"
    (checkout / ".lighthouse").mkdir(parents=True)
    (checkout / ".lighthouse" / "manifest.toml").write_text(
        f'# Managed\n\nversion = "{version}"\n\n[[unit]]\nversion = "0.0.1"\n', encoding="utf-8"
    )
    if cmd:
        (checkout / "cmd" / "lighthouse").mkdir(parents=True)
        (checkout / "cmd" / "lighthouse" / "main.go").write_text("package main\n", encoding="utf-8")
    fakes = tmp_path / "fakes"
    fakes.mkdir()
    calls = tmp_path / "calls"
    calls.write_text("", encoding="utf-8")
    _fake_tool(
        fakes,
        "gh",
        f"""echo "gh $*" >> {calls}
dir=; while [ $# -gt 0 ]; do [ "$1" = --dir ] && dir=$2; shift; done
mkdir -p "$dir"; : > "$dir"/SHA256SUMS
for f in lighthouse_linux_amd64 lighthouse_linux_arm64 lighthouse_darwin_amd64 lighthouse_darwin_arm64; do
  printf '#!/bin/sh\\n' > "$dir/$f"
done
""",
    )
    if sha256sum:
        _fake_tool(fakes, "sha256sum", f'echo "sha256sum $*" >> {calls}\n')
    _fake_tool(fakes, "shasum", f'echo "shasum $*" >> {calls}\n')
    _fake_tool(
        fakes,
        "go",
        f"""echo "go $*" >> {calls}
[ "$1" = build ] && [ "$2" = -o ] && : > "$3"
""",
    )
    runner_temp = tmp_path / "runner"
    runner_temp.mkdir()
    github_path = tmp_path / "github_path"
    env = {
        "PATH": f"{fakes}{os.pathsep}{base_path if base_path is not None else os.environ['PATH']}",
        "RUNNER_OS": runner_os,
        "RUNNER_ARCH": arch,
        "RUNNER_TEMP": str(runner_temp),
        "GITHUB_PATH": str(github_path),
        "GH_TOKEN": "t",
    }
    proc = subprocess.run(
        ["bash", "-c", _step_run("Get lighthouse")],
        cwd=checkout,
        env=env,
        capture_output=True,
        text=True,
        timeout=60,
    )
    return proc, runner_temp, calls.read_text(encoding="utf-8").splitlines()


@pytest.mark.parametrize(
    ("runner_os", "arch", "asset"),
    [
        ("Linux", "X64", "lighthouse_linux_amd64"),
        ("Linux", "ARM64", "lighthouse_linux_arm64"),
        ("macOS", "ARM64", "lighthouse_darwin_arm64"),
        ("macOS", "X64", "lighthouse_darwin_amd64"),
    ],
)
def test_get_lighthouse_maps_runner_and_refuses_unknown(tmp_path, runner_os, arch, asset) -> None:
    proc, runner_temp, calls = _get_lighthouse(
        tmp_path, version="0.12.1", runner_os=runner_os, arch=arch, cmd=False
    )
    assert proc.returncode == 0, proc.stderr
    assert any(
        c.startswith("gh release download v0.12.1 --repo ") and f"--pattern {asset} " in c + " "
        and "--pattern SHA256SUMS" in c and f"--dir {runner_temp}/bin" in c
        for c in calls
    ), calls
    assert "sha256sum --check --ignore-missing SHA256SUMS" in calls
    assert not any(c.startswith("shasum") for c in calls), calls
    assert (runner_temp / "bin" / "lighthouse").is_file()
    assert os.access(runner_temp / "bin" / "lighthouse", os.X_OK)
    assert not (runner_temp / "bin" / asset).exists()
    assert (tmp_path / "github_path").read_text(encoding="utf-8") == f"{runner_temp}/bin\n"


def test_get_lighthouse_verifies_with_shasum_where_sha256sum_is_missing(tmp_path) -> None:
    """macOS runners have shasum only; a PATH without sha256sum must not fail the step."""
    safe = tmp_path / "safe"
    safe.mkdir()
    for tool in ("bash", "dirname", "mkdir", "mv", "chmod", "cat", "grep", "sed", "uname", "env", "head", "tr", "cut", "rm", "touch"):
        found = shutil.which(tool)
        if found:
            (safe / tool).symlink_to(found)
    proc, _, calls = _get_lighthouse(
        tmp_path, version="0.12.1", runner_os="macOS", arch="ARM64", cmd=False,
        sha256sum=False, base_path=str(safe),
    )
    assert proc.returncode == 0, proc.stderr
    assert "shasum -a 256 -c --ignore-missing SHA256SUMS" in calls
    assert not any(c.startswith("sha256sum") for c in calls), calls


def test_get_lighthouse_refuses_an_unknown_runner(tmp_path) -> None:
    for runner_os, arch, message in [
        ("Linux", "X86", "runner architecture X86"),
        ("Windows", "X64", "runner OS Windows"),
    ]:
        proc, runner_temp, calls = _get_lighthouse(
            tmp_path / arch, version="0.12.1", runner_os=runner_os, arch=arch, cmd=False
        )
        assert proc.returncode == 1
        assert f"::error::no lighthouse binary for {message}" in proc.stdout
        assert not any(c.startswith("gh ") for c in calls)
        assert not (runner_temp / "bin" / "lighthouse").exists()
        assert not (tmp_path / arch / "github_path").exists()


def test_get_lighthouse_builds_dev_from_the_checkout(tmp_path) -> None:
    proc, runner_temp, calls = _get_lighthouse(
        tmp_path, version="dev", runner_os="Linux", arch="X64", cmd=True
    )
    assert proc.returncode == 0, proc.stderr
    assert calls == [f"go build -o {runner_temp}/bin/lighthouse ./cmd/lighthouse"]
    assert (runner_temp / "bin" / "lighthouse").is_file()


def test_get_lighthouse_refuses_dev_without_a_source(tmp_path) -> None:
    proc, runner_temp, calls = _get_lighthouse(
        tmp_path, version="dev", runner_os="Linux", arch="X64", cmd=False
    )
    assert proc.returncode == 1
    assert "::error::" in proc.stdout and "cmd/lighthouse" in proc.stdout
    assert calls == []
    assert not (runner_temp / "bin" / "lighthouse").exists()


def test_go_is_set_up_only_for_a_dev_checkout() -> None:
    text = _text()
    assert "go-version-file: go.mod" in text
    assert "if: steps.lighthouse.outputs.build == 'true'" in _lines()
    assert "actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e # v7.0.0" in text
    assert "GH_TOKEN: ${{ github.token }}" in _lines()


def test_the_one_literal_is_the_release_download() -> None:
    """The deny list names the repository owner; the download line is the one allowed hit."""
    allow = (ROOT / "ci" / "literal-allowlist.txt").read_text(encoding="utf-8").splitlines()
    hits = [
        f"pack/core/workflows/unblock-dependents.yml:{line}"
        for line in _text().splitlines()
        if "corneeldh" in line.lower()
    ]
    assert hits == ["pack/core/workflows/unblock-dependents.yml:              --repo CorneeldH/lighthouse \\"]
    assert hits[0] in allow


def _token_env_step(tmp_path: Path, *, config_out: str | None) -> tuple[subprocess.CompletedProcess[str], str]:
    """Run the `Token name` block with a fake `lighthouse config`; return the run and $GITHUB_ENV."""
    fakes = tmp_path / "fakes"
    fakes.mkdir()
    if config_out is None:
        _fake_tool(fakes, "lighthouse", 'echo "config: $2 is not set" >&2\nexit 1\n')
    else:
        _fake_tool(fakes, "lighthouse", f'[ "$1 $2" = "config forge.token_env" ] || exit 2\necho {config_out}\n')
    github_env = tmp_path / "github_env"
    env = {
        "PATH": f"{fakes}{os.pathsep}{os.environ['PATH']}",
        "GITHUB_ENV": str(github_env),
        "GH_TOKEN": "app-token",
    }
    proc = subprocess.run(
        ["bash", "-e", "-c", _step_run("Token name")], env=env, capture_output=True, text=True, timeout=60
    )
    return proc, github_env.read_text(encoding="utf-8") if github_env.exists() else ""


def test_token_env_is_exported_under_the_configured_name(tmp_path) -> None:
    proc, github_env = _token_env_step(tmp_path, config_out="FORGEJO_TOKEN")
    assert proc.returncode == 0, proc.stderr
    assert github_env == "FORGEJO_TOKEN=app-token\n"


def test_token_env_unset_exports_nothing_and_does_not_fail(tmp_path) -> None:
    proc, github_env = _token_env_step(tmp_path, config_out=None)
    assert proc.returncode == 0, proc.stderr
    assert github_env == ""


def test_token_name_step_runs_after_the_binary_and_before_the_release() -> None:
    text = _text()
    order = [text.index(f"- name: {n}") for n in ("Get lighthouse", "App token for labels and comments", "Token name", "Release dependents")]
    assert order == sorted(order)
    assert "GH_REPO: ${{ github.repository }}" in _lines()


def test_every_placeholder_is_a_known_scalar_key() -> None:
    """A list key would print one element per line, unescaped into the YAML."""
    keys = set(PLACEHOLDER.findall(_text()))
    assert keys == {
        "ci.runs_on",
        "ci.bot_client_id_var",
        "ci.bot_private_key_secret",
        "forge.default_branch",
    }
    kinds = config_kinds(keys)
    for key in keys:
        assert kinds[key] == "str", key


def test_no_project_literals_left() -> None:
    text = _text().lower()
    for literal in ("self-hosted", "_bot_", "refs/heads/main"):
        assert literal not in text, literal
