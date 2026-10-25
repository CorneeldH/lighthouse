"""The gate `gofmt.sh` of `pack/go`: it formats the edited `.go` file, blocks on a file
that does not parse, and says so when it could not check.

The hook is bash and reads its input as JSON on stdin. These tests run it as a
subprocess with a real temporary file; `gofmt` is a stub on the PATH except where the
real one is the point.
"""

import json
import os
import shutil
import subprocess
from pathlib import Path

import pytest
from lighthouse_lib import bash_writes

from tests.pack._testkit import tool_bin
from tests.pack.go.conftest import GO_HOOKS

GOFMT_HOOK = GO_HOOKS / "gofmt.sh"
UNCHECKED = "gofmt: COULD NOT CHECK — "
PARSE_ERROR = "x.go:2:8: expected ')', found 'EOF'"
STUB = f"""#!/bin/bash
echo "$@" > "$STUB_LOG"
if [ "${{STUB_RC:-0}}" -eq 2 ]; then
  echo "{PARSE_ERROR}" >&2
fi
exit ${{STUB_RC:-0}}
"""
TOOLS = ("bash", "jq", "cat")


def _run(
    payload: dict | str, cwd: Path, env: dict | None = None
) -> subprocess.CompletedProcess:
    return subprocess.run(
        [str(GOFMT_HOOK)],
        input=payload if isinstance(payload, str) else json.dumps(payload),
        capture_output=True,
        text=True,
        cwd=str(cwd),
        env=dict(os.environ, **(env or {})),
        check=False,
    )


def _file(tmp_path: Path, name: str, text: str = "package m\n") -> dict:
    target = tmp_path / name
    target.write_text(text)
    return {"tool_input": {"file_path": str(target)}}


def _stubbed(tmp_path: Path, rc: int = 0) -> dict:
    """An environment whose `gofmt` is a stub that logs its arguments to STUB_LOG."""
    bin_dir = tool_bin(tmp_path / "bin", TOOLS, stubs={"gofmt": STUB})
    return {
        "PATH": f"{bin_dir}:{os.environ['PATH']}",
        "STUB_LOG": str(tmp_path / "stub.log"),
        "STUB_RC": str(rc),
    }


def _without(tmp_path: Path, tool: str) -> dict:
    return {"PATH": str(tool_bin(tmp_path / "bin", (*TOOLS, "gofmt"), drop=tool))}


def test_formats_a_go_file(tmp_path):
    payload = _file(tmp_path, "a.go")
    env = _stubbed(tmp_path)
    result = _run(payload, tmp_path, env)
    assert result.returncode == 0, result.stderr
    assert Path(env["STUB_LOG"]).read_text().strip() == f"-w {payload['tool_input']['file_path']}"


def test_blocks_a_file_that_does_not_parse(tmp_path):
    payload = _file(tmp_path, "a.go")
    path = payload["tool_input"]["file_path"]
    result = _run(payload, tmp_path, _stubbed(tmp_path, rc=2))
    assert result.returncode == 2, result.stdout
    assert result.stderr.splitlines()[0] == f"gofmt: {path} does not parse — fix the syntax."
    assert PARSE_ERROR in result.stderr


def test_other_extension_passes(tmp_path):
    env = _stubbed(tmp_path)
    result = _run(_file(tmp_path, "a.py"), tmp_path, env)
    assert result.returncode == 0, result.stderr
    assert not Path(env["STUB_LOG"]).exists()


def test_missing_file_passes(tmp_path):
    result = _run({"tool_input": {"file_path": str(tmp_path / "gone.go")}}, tmp_path, _stubbed(tmp_path))
    assert result.returncode == 0, result.stderr


def test_no_path_passes(tmp_path):
    assert _run({"tool_input": {}}, tmp_path, _stubbed(tmp_path)).returncode == 0


@pytest.mark.skipif(shutil.which("gofmt") is None, reason="the Python lane of CI has no setup-go")
def test_classify_reads_the_parse_error_as_a_finding(tmp_path):
    result = _run(_file(tmp_path, "x.go", "package m\n\nfunc F( {\n"), tmp_path)
    assert result.returncode == 2, result.stdout
    assert result.stderr.startswith("gofmt: "), result.stderr
    assert bash_writes.classify(2, result.stderr.encode()) == "finding"


def test_without_gofmt_could_not_check(tmp_path):
    result = _run(_file(tmp_path, "a.go"), tmp_path, _without(tmp_path, "gofmt"))
    assert result.returncode == 2, result.stdout
    assert result.stderr.splitlines()[0].startswith(UNCHECKED), result.stderr
    assert bash_writes.classify(2, result.stderr.encode()) == "unchecked"


def test_without_gofmt_other_extension_passes(tmp_path):
    result = _run(_file(tmp_path, "a.md", "# hi\n"), tmp_path, _without(tmp_path, "gofmt"))
    assert result.returncode == 0
    assert result.stderr == ""


def test_without_jq_could_not_check(tmp_path):
    result = _run(_file(tmp_path, "a.go"), tmp_path, _without(tmp_path, "jq"))
    assert result.returncode == 2, result.stdout
    assert result.stderr.splitlines()[0].startswith(UNCHECKED), result.stderr


def test_empty_payload_could_not_check(tmp_path):
    result = _run("", tmp_path, _stubbed(tmp_path))
    assert result.returncode == 2, result.stdout
    assert result.stderr.splitlines()[0].startswith(UNCHECKED), result.stderr


def test_invalid_json_could_not_check(tmp_path):
    result = _run("{", tmp_path, _stubbed(tmp_path))
    assert result.returncode == 2, result.stdout
    assert result.stderr.splitlines()[0].startswith(UNCHECKED), result.stderr


def test_registered_on_edit_write():
    registered = json.loads(
        (Path(__file__).resolve().parents[3] / "pack/go/hooks/hooks.json").read_text()
    )
    entries = [
        h
        for block in registered["hooks"]["PostToolUse"]
        if block.get("matcher") == "Edit|Write"
        for h in block["hooks"]
    ]
    assert [(h["command"], h["timeout"]) for h in entries] == [
        ("${CLAUDE_PLUGIN_ROOT}/hooks/gofmt.sh", 15)
    ]
