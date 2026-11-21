"""A Claude session against a tmp repo, for the tests of `bash-write-check`.

`_Session` installs `pack/core` in a lighthouse root of its own, with stubs for
`shell-lint-check.sh` and `ruff-format-check.sh`, and runs pre, the Bash call and post.
"""

import hashlib
import json
import os
import subprocess
import uuid
from pathlib import Path

from tests.pack.python._testkit import (
    GIT_ENV,
    git,
    hook_shim,
    init_repo,
    install_core,
    path_with_lighthouse,
)

# Stubs for the gates of pack/shell and pack/python. Each reads the payload like
# the real gate, reports one fixed finding and then cites the file. The shell
# stub says COULD NOT CHECK when SHELLCHECK_BIN names a file that is not there.
SHELLCHECK_STUB = r"""#!/bin/bash
f=$(jq -r '.tool_input.file_path')
[ -f "$f" ] || exit 0
if [ -n "${SHELLCHECK_BIN:-}" ] && [ ! -e "$SHELLCHECK_BIN" ]; then
  echo "shellcheck-gate: COULD NOT CHECK — no shellcheck at $SHELLCHECK_BIN" >&2
  exit 2
fi
if grep -q 'echo "hoi"' "$f"; then
  echo "$f:3: SC2034 (stub): x appears unused." >&2
  cat "$f" >&2
  exit 2
fi
exit 0
"""
RUFF_STUB = r"""#!/bin/bash
f=$(jq -r '.tool_input.file_path')
[ -f "$f" ] || exit 0
if grep -q '^except:' "$f"; then
  echo "$f:3:1: E722 (stub) Do not use bare except" >&2
  echo "Lint-gate: fix this in the code." >&2
  exit 2
fi
exit 0
"""


def heredoc(name: str, text: str) -> str:
    """A Bash command that writes `text` to `name` via a heredoc."""
    return f"cat > {name} <<'EOF'\n{text}EOF\n"


class _Session:
    """One Claude session against a tmp repo: pre, the Bash call, post.

    `env` is the test environment from *Global Constraints*; pre, the Bash call and
    post all receive it, plus any extras from the test. Pre and post
    run via the real wrapper, started directly (not via `bash`).
    """

    def __init__(
        self,
        tmp_path: Path,
        shellcheck: str = "",
        *,
        unborn: bool = False,
        language_gates: bool = True,
    ) -> None:
        self.root = tmp_path / "lighthouse"
        install_core(self.root)
        hooks = self.root / ".claude" / "hooks" / "lighthouse"
        if language_gates:
            for name, stub in (("shell-lint-check.sh", SHELLCHECK_STUB), ("ruff-format-check.sh", RUFF_STUB)):
                (hooks / name).write_text(stub, encoding="utf-8")
                (hooks / name).chmod(0o755)
        self.wrapper = hook_shim(self.root, "bash-write-check")
        self.repo = tmp_path / "repo"
        if unborn:
            # Only `git init`: HEAD points to `main`, but there is no commit yet.
            self.repo.mkdir(parents=True)
            git(self.repo, "init", "-q", "-b", "main")
        else:
            init_repo(self.repo)
        xdg = tmp_path / "xdg"
        xdg.mkdir()
        self.xdg = xdg
        self.env = dict(
            os.environ,
            **GIT_ENV,
            CLAUDE_PROJECT_DIR=str(self.repo),
            XDG_RUNTIME_DIR=str(xdg),
            SHELLCHECK_BIN=shellcheck,
            LIGHTHOUSE_REPO_ROOT=str(self.root),
            PATH=path_with_lighthouse(),
        )
        for seam in ("LIGHTHOUSE_BASH_GATES_DEADLINE", "LIGHTHOUSE_BASH_GATES_START_NS"):
            self.env.pop(seam, None)
        self.session_id = f"sess-{uuid.uuid4().hex[:8]}"

    @property
    def state(self) -> Path:
        return self.xdg / "lighthouse-bash-gates" / self.session_id

    def payload(self, cmd: str, tool_use_id: str, cwd: Path, event: str) -> dict:
        return {
            "session_id": self.session_id,
            "tool_use_id": tool_use_id,
            "hook_event_name": event,
            "tool_name": "Bash",
            "cwd": str(cwd),
            "tool_input": {"command": cmd},
        }

    def hook(
        self, mode: str, payload: dict, env: dict | None = None
    ) -> subprocess.CompletedProcess:
        return subprocess.run(
            [str(self.wrapper), mode],
            input=json.dumps(payload),
            capture_output=True,
            text=True,
            cwd=payload["cwd"],
            env=dict(self.env, **(env or {})),
            check=False,
        )

    def hook_popen(self, mode: str, payload: dict) -> subprocess.Popen:
        proc = subprocess.Popen(
            [str(self.wrapper), mode],
            stdin=subprocess.PIPE,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
            cwd=payload["cwd"],
            env=self.env,
        )
        assert proc.stdin is not None
        proc.stdin.write(json.dumps(payload))
        proc.stdin.close()
        return proc

    def call(
        self,
        cmd: str,
        *,
        cwd: Path | None = None,
        pre: bool = True,
        post: bool = True,
        event: str = "PostToolUse",
        env: dict | None = None,
    ) -> subprocess.CompletedProcess | None:
        cwd = cwd or self.repo
        tool_use_id = f"toolu_{uuid.uuid4().hex[:12]}"
        if pre:
            pre_out = self.hook(
                "pre", self.payload(cmd, tool_use_id, cwd, "PreToolUse"), env
            )
            assert pre_out.returncode == 0, pre_out.stderr
        subprocess.run(
            ["bash", "-c", cmd],
            cwd=str(cwd),
            env=dict(self.env, **(env or {})),
            capture_output=True,
            check=False,
        )
        if not post:
            return None
        return self.hook("post", self.payload(cmd, tool_use_id, cwd, event), env)


def context(result: subprocess.CompletedProcess) -> str:
    """`hookSpecificOutput.additionalContext` from the stdout, or `""`."""
    if not result.stdout.strip():
        return ""
    return json.loads(result.stdout)["hookSpecificOutput"]["additionalContext"]


def managed_script(installed: str) -> str:
    """A manifest with one file unit (#88): `init` wrote `.lighthouse/scripts/managed.py`
    with the text `installed`, and that file belongs to lighthouse, not to the project."""
    sha = hashlib.sha256(installed.encode()).hexdigest()
    return (
        'version = "dev"\npackages = ["core", "python"]\n\n[[unit]]\nkind = "file"\n'
        'path = ".lighthouse/scripts/managed.py"\npackage = "python"\nversion = "dev"\n'
        f'sha256 = "{sha}"\n'
    )
