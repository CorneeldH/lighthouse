"""End-to-end tests for pack/core `commit-tests.sh pre`, as installed (#884, #80).

The hook runs as a subprocess, with real git repos in `tmp_path` and a
fake `uv` at the front of `PATH`. The test command comes from `[test_gate] command`
(via `LIGHTHOUSE_TOML`), the slice from `[host] gate_slice` (via
`LIGHTHOUSE_HOST_TOML`). Safety: every test sets `CLAUDE_PROJECT_DIR` to a
temporary repo and puts the stub directory with the fake `uv` first on `PATH`.
"""

import fcntl
import json
import os
import re
import shutil
import signal
import subprocess
import time
from pathlib import Path

import pytest

from tests.pack._testkit import (
    CORE,
    INSTALLED_HOOKS,
    MINIMAL_TOML,
    ORIGIN,
    bash_payload,
    forge_worktree,
    git,
    init_repo,
    run_hook,
    tool_bin,
)

HOOK = INSTALLED_HOOKS / "commit-tests.sh"
HOOKS_JSON = CORE / "hooks" / "hooks.json"
REAL_GIT = shutil.which("git")
REAL_PYTHON3 = shutil.which("python3")
SESSION = "s1"

# The test command of a project the way paia would set it; the fake `uv`
# records its arguments. `slot_wait_seconds = 1`: no test waits 180 s.
TEST_GATE = (
    "[test_gate]\n"
    "command = 'uv run pytest -q --basetemp=\"$LIGHTHOUSE_GATE_TMP\"'\n"
    "slot_wait_seconds = 1\n"
)
HOST = '[host]\nname = "h"\ntailscale_ip = "100.64.0.1"\ngate_slice = "x.slice"\n'
HOST_WITHOUT_SLICE = '[host]\nname = "h"\ntailscale_ip = "100.64.0.1"\n'

# Fake `uv`: records where it ran, gives a pytest-like closing line and
# the exit code from UV_EXIT. With UV_SLOW_SECOND the second invocation sleeps first,
# so that a shared deadline expires at the second root.
FAKE_UV = (
    "#!/bin/sh\n"
    'if [ -n "$UV_SLOW_SECOND" ] && [ -s "$UV_MARK" ]; then\n'
    '  pwd >> "$UV_MARK"\n'
    "  sleep 8\n"
    "fi\n"
    'pwd >> "$UV_MARK"\n'
    'printf \'%s\\n\' "$*" >> "$UV_MARK.args"\n'
    # --basetemp: record the directory and leave something in it, so that a test can
    # see that the hook cleans it up.
    'for a in "$@"; do\n'
    '  case "$a" in\n'
    "    --basetemp=*)\n"
    '      bt="${a#--basetemp=}"\n'
    '      echo "$bt" >> "$UV_MARK.bt"\n'
    '      mkdir -p "$bt" && echo x > "$bt/leftover"\n'
    "      ;;\n"
    "  esac\n"
    "done\n"
    # The contract of `post` (#80): what the command saw of base, head and files.
    "{\n"
    '  echo "BASE=$LIGHTHOUSE_GATE_BASE"\n'
    '  echo "HEAD=$LIGHTHOUSE_GATE_HEAD"\n'
    '  echo "FILES=$LIGHTHOUSE_GATE_FILES"\n'
    '  [ -z "$LIGHTHOUSE_GATE_FILES" ] || cat "$LIGHTHOUSE_GATE_FILES"\n'
    '} >> "$UV_MARK.env"\n'
    'echo "3 passed in 0.01s"\n'
    "exit ${UV_EXIT:-0}\n"
)

# Pass-through stubs: the brake (Task 3) is not present on the Mac and in the devcontainer;
# that way no gate test depends on what the host has.
FAKE_SYSTEMCTL = '#!/bin/sh\necho "Failed to connect to bus" >&2\nexit 1\n'
# Drops everything up to and including the first `--` and runs the rest.
_PASS_THROUGH = (
    'while [ "$#" -gt 0 ]; do\n'
    '  if [ "$1" = "--" ]; then shift; break; fi\n'
    "  shift\n"
    "done\n"
    'exec "$@"\n'
)
FAKE_SYSTEMD_RUN = '#!/bin/sh\necho "$*" >> "$UV_MARK.wrap"\n' + _PASS_THROUGH
FAKE_CHOOM = '#!/bin/sh\necho "$*" >> "$UV_MARK.choom"\n' + _PASS_THROUGH


class Gate:
    """A temporary project repo plus a stub directory with a fake `uv`."""

    def __init__(self, tmp_path: Path, project: Path) -> None:
        self.tmp = tmp_path
        self.project = project
        init_repo(project)
        self.stub = tmp_path / "stub"
        tool_bin(
            self.stub,
            (),
            stubs={
                "uv": FAKE_UV,
                "systemctl": FAKE_SYSTEMCTL,
                "systemd-run": FAKE_SYSTEMD_RUN,
                "choom": FAKE_CHOOM,
            },
        )
        self.mark = tmp_path / "mark"
        self.slot_dir = tmp_path / "slots"
        self.cgroup = tmp_path / "cgroup"
        self.toml = tmp_path / "lighthouse.toml"
        self.host = tmp_path / "host.toml"
        self.configure(TEST_GATE)
        self.host.write_text(HOST, encoding="utf-8")

    def configure(self, test_gate: str) -> None:
        """`lighthouse.toml`: the minimal config plus this `[test_gate]` block."""
        self.toml.write_text(MINIMAL_TOML + "\n" + test_gate, encoding="utf-8")

    def add_stub(self, name: str, script: str) -> None:
        tool_bin(self.stub, (), stubs={name: script})

    def run(
        self,
        cmd: str,
        cwd: Path,
        path: str | None = None,
        background: bool = False,
        mode: tuple[str, ...] = ("pre",),
        event: str | None = None,
        **env: str,
    ):
        tail = os.environ["PATH"] if path is None else path
        full_env = {
            # The markers of `record` land under this test's own tmp dir, not in the
            # real state dir shared with every other test. Overridable through `env`.
            "XDG_RUNTIME_DIR": str(self.tmp),
            # Stub directory always first: that way no test reaches the real `uv`.
            "PATH": f"{self.stub}{os.pathsep}{tail}",
            "CLAUDE_PROJECT_DIR": str(self.project),
            "UV_MARK": str(self.mark),
            "LIGHTHOUSE_TOML": str(self.toml),
            "LIGHTHOUSE_HOST_TOML": str(self.host),
            # Every gate test itself runs inside a real gate that holds a real
            # slot: without a lock directory of its own the suite blocks itself.
            "LIGHTHOUSE_GATE_SLOT_DIR": str(self.slot_dir),
            "LIGHTHOUSE_GATE_CGROUP_ROOT": str(self.cgroup),
            **env,
        }
        payload = dict(bash_payload(cmd, cwd), session_id=SESSION)
        if background:
            payload["tool_input"]["run_in_background"] = True
        if event is not None:
            payload["hook_event_name"] = event
        return run_hook(HOOK, payload, cwd, full_env, args=mode)

    def post(self, cmd: str, cwd: Path, event: str = "PostToolUse", **env: str):
        """`post` for the same Bash call as an earlier `pre` (same command, same marker)."""
        return self.run(cmd, cwd, mode=("post",), event=event, **env)

    def seen(self) -> tuple[dict[str, str], str]:
        """What the fake `uv` saw: BASE, HEAD and FILES, plus the content of FILES."""
        lines = Path(f"{self.mark}.env").read_text(encoding="utf-8").splitlines(keepends=True)
        names = dict(line.rstrip("\n").split("=", 1) for line in lines[:3])
        return names, "".join(lines[3:])

    def markers(self) -> list[Path]:
        """The `record` markers of this session."""
        state = self.tmp / "lighthouse-commit-tests" / SESSION
        return sorted(state.glob("*.json")) if state.is_dir() else []

    def marks(self) -> list[str]:
        if not self.mark.exists():
            return []
        return self.mark.read_text(encoding="utf-8").splitlines()

    def args(self) -> list[str]:
        args_file = Path(f"{self.mark}.args")
        if not args_file.exists():
            return []
        return args_file.read_text(encoding="utf-8").splitlines()


@pytest.fixture
def gate(tmp_path: Path) -> Gate:
    return Gate(tmp_path, tmp_path / "X")


def _context(stdout: str) -> str:
    return json.loads(stdout)["hookSpecificOutput"]["additionalContext"]


def _worktree(repo: Path, path: Path, branch: str) -> None:
    git(repo, "worktree", "add", "-q", "-b", branch, str(path))


def _worktree_on_default(repo: Path, path: Path) -> None:
    """A second worktree on `main` too (`--force`): `pre` tests only a commit to the
    default branch, and a test of the loop needs two roots that it tests."""
    git(repo, "worktree", "add", "-q", "--force", str(path), "main")


def test_gate_tests_the_tree_after_cd(gate: Gate) -> None:
    result = gate.run(f"cd {gate.project} && git commit -m x", gate.tmp)

    assert result.returncode == 0, result.stderr
    assert gate.marks() == [str(gate.project.resolve())]
    context = _context(result.stdout)
    assert "3 passed" in context
    assert str(gate.project) in context
    assert json.loads(result.stdout)["systemMessage"] == context


def test_gate_tests_the_dash_c_target_not_cwd(gate: Gate) -> None:
    other = gate.tmp / "Z"
    _worktree(gate.project, other, "z")

    result = gate.run(f"git -C {gate.project} commit -m x", other)

    assert result.returncode == 0, result.stderr
    assert gate.marks() == [str(gate.project.resolve())]


def test_gate_runs_the_configured_command(gate: Gate) -> None:
    result = gate.run("git commit -m x", gate.project)

    assert result.returncode == 0, result.stderr
    lines = gate.args()
    assert len(lines) == 1
    # `bash -c` fills in $LIGHTHOUSE_GATE_TMP; the rest is there literally.
    assert lines[0].startswith("run pytest -q --basetemp=/")
    assert "$LIGHTHOUSE_GATE_TMP" not in lines[0]


def test_command_sees_gate_tmp(gate: Gate) -> None:
    gate.configure("[test_gate]\ncommand = 'test -d \"$LIGHTHOUSE_GATE_TMP\"'\n")

    result = gate.run("git commit -m x", gate.project)

    assert result.returncode == 0, result.stderr


def test_command_runs_in_the_root(gate: Gate) -> None:
    gate.configure("[test_gate]\ncommand = 'pwd > \"$UV_MARK\"'\n")

    result = gate.run(f"git -C {gate.project} commit -m x", gate.tmp)

    assert result.returncode == 0, result.stderr
    assert gate.marks() == [str(gate.project.resolve())]


def test_no_command_is_loud_and_allows(gate: Gate) -> None:
    gate.configure("")

    result = gate.run("git commit -m x", gate.project)

    assert result.returncode == 0, result.stderr
    out = json.loads(result.stdout)
    message = "commit-tests: no test command configured, nothing was tested"
    assert message in out["systemMessage"]
    assert message in out["hookSpecificOutput"]["additionalContext"]
    assert not gate.mark.exists()


def test_empty_command_is_off(gate: Gate) -> None:
    # `command = ""` is the opt-out (design choice 4 of M0 (c)): same notice as
    # without the key, because `--upgrade` puts a removed key back.
    gate.configure('[test_gate]\ncommand = ""\n')

    result = gate.run("git commit -m x", gate.project)

    assert result.returncode == 0, result.stderr
    message = "commit-tests: no test command configured, nothing was tested"
    assert message in json.loads(result.stdout)["systemMessage"]
    assert not gate.mark.exists()


def test_failure_names_the_command_and_the_opt_out(gate: Gate) -> None:
    # A default command that does not fit this project (no pytest) fails;
    # the notice points to the key and to the opt-out.
    result = gate.run("git commit -m x", gate.project, UV_EXIT="2")

    assert result.returncode == 2, result.stdout
    assert "tests failed" in result.stderr
    assert "[test_gate] command" in result.stderr
    assert str(gate.toml) in result.stderr
    assert 'command = ""' in result.stderr


def test_unchecked_exit_code_names_the_command_and_the_opt_out(gate: Gate) -> None:
    # pytest without tests gives 5: the most likely first block in a
    # new Python project. That one also points to the key and the opt-out.
    gate.configure(TEST_GATE + "unchecked_exit_codes = [3, 4, 5]\n")

    result = gate.run("git commit -m x", gate.project, UV_EXIT="5")

    assert result.returncode == 2, result.stdout
    assert "exit code 5" in result.stderr
    assert "[test_gate] command" in result.stderr
    assert 'command = ""' in result.stderr


def test_command_not_found_names_the_command(gate: Gate) -> None:
    gate.configure('[test_gate]\ncommand = "no-such-tool-xyz"\n')

    result = gate.run("git commit -m x", gate.project)

    assert result.returncode == 2, result.stdout
    assert "could not start" in result.stderr
    assert "[test_gate] command" in result.stderr
    assert 'command = ""' in result.stderr


def test_broken_config_blocks(gate: Gate) -> None:
    gate.toml.write_text("[test_gate\n", encoding="utf-8")

    result = gate.run("git commit -m x", gate.project)

    assert result.returncode == 2, result.stdout
    assert "commit-tests: COULD NOT CHECK" in result.stderr
    assert str(gate.toml) in result.stderr
    assert not gate.mark.exists()


def test_broken_host_toml_blocks(gate: Gate) -> None:
    gate.host.write_text("[host\n", encoding="utf-8")

    result = gate.run("git commit -m x", gate.project)

    assert result.returncode == 2, result.stdout
    assert "commit-tests: COULD NOT CHECK" in result.stderr
    assert str(gate.host) in result.stderr
    assert not gate.mark.exists()


def test_deadline_from_config(gate: Gate) -> None:
    gate.configure(TEST_GATE + "deadline_seconds = 30\n")

    result = gate.run("git commit -m x", gate.project, UV_EXIT="124")

    assert result.returncode == 2, result.stdout
    assert "deadline of 30 s" in result.stderr


@pytest.mark.parametrize("seconds", [0, 281])
def test_deadline_outside_the_hook_timeout_blocks(gate: Gate, seconds: int) -> None:
    # The hook timeout is 300 s, the grace of deadline.py 10 s: a deadline
    # above 280 s lets the timeout expire first, and a timeout is fail-open.
    gate.configure(TEST_GATE + f"deadline_seconds = {seconds}\n")

    result = gate.run("git commit -m x", gate.project)

    assert result.returncode == 2, result.stdout
    assert "COULD NOT CHECK" in result.stderr
    assert "test_gate.deadline_seconds" in result.stderr
    assert not gate.mark.exists()


def test_gate_removes_its_basetemp(gate: Gate) -> None:
    result = gate.run("git commit -m x", gate.project)

    assert result.returncode == 0, result.stderr
    recorded = Path(f"{gate.mark}.bt").read_text(encoding="utf-8").splitlines()
    assert len(recorded) == 1
    assert recorded[0]
    # The fake uv left a file in the directory; the hook cleans it up.
    assert not Path(recorded[0]).exists()


def test_unchecked_exit_code(gate: Gate) -> None:
    gate.configure('[test_gate]\ncommand = "exit 5"\nunchecked_exit_codes = [5]\n')

    result = gate.run("git commit -m x", gate.project)

    assert result.returncode == 2, result.stdout
    assert "commit-tests: COULD NOT CHECK" in result.stderr
    assert "exit code 5" in result.stderr
    assert "tests failed" not in result.stderr


@pytest.mark.parametrize("exit_code", ["3", "5"])
def test_unchecked_exit_codes_keep_the_output(gate: Gate, exit_code: str) -> None:
    # At paia: pytest 3 (internal error), 4 (usage error), 5 (nothing
    # collected). That blocks, but is not called a failing test.
    gate.configure(TEST_GATE + "unchecked_exit_codes = [3, 4, 5]\n")

    result = gate.run("git commit -m x", gate.project, UV_EXIT=exit_code)

    assert result.returncode == 2, result.stdout
    assert "commit-tests: COULD NOT CHECK" in result.stderr
    assert f"exit code {exit_code}" in result.stderr
    assert "tests failed" not in result.stderr
    assert "3 passed" in result.stderr


def test_exit_code_not_listed_is_a_failure(gate: Gate) -> None:
    result = gate.run("git commit -m x", gate.project, UV_EXIT="5")

    assert result.returncode == 2, result.stdout
    assert "tests failed" in result.stderr
    assert "COULD NOT CHECK" not in result.stderr


def test_gate_blocks_on_failing_tests(gate: Gate) -> None:
    result = gate.run("git commit -m x", gate.project, UV_EXIT="1")

    assert result.returncode == 2, result.stdout
    assert "3 passed" in result.stderr
    assert "tests failed" in result.stderr


def test_gate_blocks_on_deadline(gate: Gate) -> None:
    result = gate.run("git commit -m x", gate.project, UV_EXIT="124")

    assert result.returncode == 2, result.stdout
    assert "deadline of 270 s" in result.stderr
    assert "COULD NOT CHECK" in result.stderr


def test_gate_skips_foreign_repo_visibly(gate: Gate) -> None:
    foreign = gate.tmp / "Y"
    init_repo(foreign)

    result = gate.run(f"cd {foreign} && git commit -m x", gate.tmp)

    assert result.returncode == 0, result.stderr
    assert not gate.mark.exists()
    assert "skipped — other project" in result.stdout
    assert "skipped — other project" in _context(result.stdout)


def test_gate_tests_forge_worktree_of_same_repo(gate: Gate) -> None:
    """A worktree of a separate clone with the same `origin` is this project (#36)."""
    git(gate.project, "remote", "add", "origin", ORIGIN)
    work = forge_worktree(gate.project, gate.tmp)
    # On the default branch: `pre` only tests a commit there.
    git(work, "switch", "-q", "main")

    result = gate.run(f"git -C {work} commit -m x", gate.tmp)

    assert result.returncode == 0, result.stderr
    assert gate.marks() == [str(work.resolve())]
    assert "other project" not in result.stdout


def test_gate_names_common_dirs_and_origins_when_skipping(gate: Gate) -> None:
    git(gate.project, "remote", "add", "origin", ORIGIN)
    work = forge_worktree(gate.project, gate.tmp, "https://github.com/Acme/gadgets.git")

    result = gate.run(f"git -C {work} commit -m x", gate.tmp)

    assert result.returncode == 0, result.stderr
    assert not gate.mark.exists()
    common = (gate.tmp / "clone.git").resolve()
    ref = (gate.project / ".git").resolve()
    assert (
        f"{work.resolve()} skipped — other project (git-common-dir {common} vs {ref};"
        f" origin https://github.com/Acme/gadgets.git vs {ORIGIN})"
    ) in _context(result.stdout)


def test_gate_silent_outside_any_repo(gate: Gate) -> None:
    plain = gate.tmp / "plain"
    plain.mkdir()

    result = gate.run(f"cd {plain} && git commit -m x", gate.tmp)

    assert result.returncode == 0, result.stderr
    assert result.stdout == ""
    assert not gate.mark.exists()


def test_gate_silent_outside_any_repo_under_other_locale(gate: Gate) -> None:
    plain = gate.tmp / "plain"
    plain.mkdir()
    env = {"LANG": "de_DE.UTF-8", "LC_ALL": "", "LANGUAGE": ""}
    probe = subprocess.run(
        ["git", "-C", str(plain), "rev-parse", "--show-toplevel"],
        capture_output=True,
        text=True,
        env=dict(os.environ, **env),
        check=False,
    )
    if "not a git repository" in probe.stderr:
        pytest.skip("git speaks no German here")

    result = gate.run(f"cd {plain} && git commit -m x", gate.tmp, **env)

    assert result.returncode == 0, result.stderr
    assert result.stdout == ""


def test_gate_silent_without_commit(gate: Gate) -> None:
    result = gate.run("git status", gate.project)

    assert result.returncode == 0, result.stderr
    assert result.stdout == ""
    assert not gate.mark.exists()


def test_gate_loud_on_unresolved_path(gate: Gate) -> None:
    result = gate.run('git -C "$ONBEKEND" commit -m x', gate.project)

    assert result.returncode == 2, result.stdout
    assert "COULD NOT CHECK" in result.stderr
    assert "cannot be resolved" in result.stderr
    assert not gate.mark.exists()


def test_gate_loud_on_new_dir(gate: Gate) -> None:
    result = gate.run(f"cd {gate.project}/nieuw && git commit -m x", gate.tmp)

    assert result.returncode == 2, result.stdout
    assert "COULD NOT CHECK" in result.stderr
    assert "does not exist yet" in result.stderr
    assert not gate.mark.exists()


def test_gate_loud_when_git_breaks(gate: Gate) -> None:
    gate.add_stub(
        "git",
        "#!/bin/sh\n"
        'case "$*" in\n'
        '  *--show-toplevel*) echo "fatal: detected dubious ownership" >&2; '
        "exit 128 ;;\n"
        "esac\n"
        f'exec "{REAL_GIT}" "$@"\n',
    )

    result = gate.run("git commit -m x", gate.project)

    assert result.returncode == 2, result.stdout
    assert "COULD NOT CHECK" in result.stderr
    assert "dubious ownership" in result.stderr
    assert not gate.mark.exists()


def _fail_common_dir_for(gate: Gate, target: Path) -> None:
    gate.add_stub(
        "git",
        "#!/bin/sh\n"
        f'if [ "$1" = "-C" ] && [ "$2" = "{target}" ]; then\n'
        '  case "$*" in\n'
        '    *--git-common-dir*) echo "fatal: kapot" >&2; exit 128 ;;\n'
        "  esac\n"
        "fi\n"
        f'exec "{REAL_GIT}" "$@"\n',
    )


def test_gate_loud_when_common_dir_fails_for_target(gate: Gate) -> None:
    second = gate.tmp / "X2"
    _worktree(gate.project, second, "x2")
    _fail_common_dir_for(gate, second.resolve())

    result = gate.run("git commit -m x", second)

    assert result.returncode == 2, result.stdout
    assert "COULD NOT CHECK" in result.stderr
    assert "kapot" in result.stderr
    assert not gate.mark.exists()


def test_gate_loud_when_common_dir_fails_for_reference(gate: Gate) -> None:
    second = gate.tmp / "X2"
    _worktree(gate.project, second, "x2")
    _fail_common_dir_for(gate, gate.project)

    result = gate.run("git commit -m x", second)

    assert result.returncode == 2, result.stdout
    assert "COULD NOT CHECK" in result.stderr
    assert "kapot" in result.stderr
    assert not gate.mark.exists()


def test_gate_loud_without_cat(gate: Gate) -> None:
    bin_dir = tool_bin(gate.tmp / "bin", ("jq", "python3", "git", "cat"), drop="cat")

    # The stub directory stays at the front (Gate.run puts it before `path`).
    result = gate.run("git commit -m x", gate.project, path=str(bin_dir))

    assert result.returncode == 2, result.stdout
    assert "missing tooling" in result.stderr
    assert "cat" in result.stderr
    assert not gate.mark.exists()


def test_gate_loud_without_uv(gate: Gate) -> None:
    # No `uv` to be found: then nothing was tested, and that is something other than
    # "tests failed". No fake `uv` in the stub directory, and the rest of PATH
    # has none either: that way this test can never start the real suite.
    (gate.stub / "uv").unlink()
    bin_dir = tool_bin(
        gate.tmp / "bin",
        ("jq", "python3", "git", "cat", "dirname", "mktemp", "rm", "date", "mkdir"),
    )

    result = gate.run("git commit -m x", gate.project, path=str(bin_dir))

    assert result.returncode == 2, result.stdout
    assert "commit-tests: COULD NOT CHECK" in result.stderr
    assert "uv" in result.stderr
    assert "tests failed" not in result.stderr
    assert not gate.mark.exists()


def test_gate_blocks_unparseable_commit(gate: Gate) -> None:
    result = gate.run("git commit -m 'x", gate.project)

    assert result.returncode == 2, result.stdout
    assert "COULD NOT CHECK" in result.stderr
    assert not gate.mark.exists()


def test_gate_passes_unparseable_non_commit(gate: Gate) -> None:
    result = gate.run("echo 'x", gate.project)

    assert result.returncode == 0, result.stderr
    assert not gate.mark.exists()


def test_gate_runs_once_per_root(gate: Gate) -> None:
    result = gate.run("git commit -m a && git commit -m b", gate.project)

    assert result.returncode == 0, result.stderr
    assert gate.marks() == [str(gate.project.resolve())]


def test_gate_uses_one_deadline_for_two_roots(gate: Gate) -> None:
    second = gate.tmp / "X2"
    _worktree_on_default(gate.project, second)
    # Fake `date`: the deadline (now + 270) thus falls ~5 s after now. The first root
    # makes that easily, even on a busy runner; the second sleeps 8 s and so
    # runs over the same deadline.
    # Every invocation goes into DATE_LOG: exactly one invocation proves that the
    # deadline is shared — a deadline recalculated per root would expire here too,
    # so rc 2 alone does not distinguish those two.
    date_log = gate.tmp / "date-log"
    gate.add_stub(
        "date",
        '#!/bin/sh\necho call >> "$DATE_LOG"\necho $(( $(/bin/date +%s) - 265 ))\n',
    )

    result = gate.run(
        f"git commit -m a && cd {second} && git commit -m b",
        gate.project,
        UV_SLOW_SECOND="1",
        DATE_LOG=str(date_log),
    )

    assert result.returncode == 2, result.stdout
    assert "deadline of 270 s" in result.stderr
    assert date_log.read_text(encoding="utf-8").splitlines() == ["call"]
    assert gate.marks() == [str(gate.project.resolve()), str(second.resolve())]


def test_gate_handles_space_in_repo_path(tmp_path: Path) -> None:
    spaced = Gate(tmp_path, tmp_path / "a b")

    result = spaced.run(f'cd "{spaced.project}" && git commit -m x', tmp_path)

    assert result.returncode == 0, result.stderr
    assert spaced.marks() == [str(spaced.project.resolve())]


def test_hook_timeout_exceeds_deadline() -> None:
    registered = json.loads(HOOKS_JSON.read_text(encoding="utf-8"))
    entries = [
        hook
        for block in registered["hooks"]["PreToolUse"]
        if block.get("matcher") == "Bash"
        for hook in block["hooks"]
        if hook["command"].endswith("commit-tests.sh pre")
    ]

    assert len(entries) == 1
    assert entries[0]["timeout"] == 300
    assert entries[0]["timeout"] > 280 + 10
    assert os.access(HOOK, os.X_OK)


def hold_slots(gate: Gate, *names: str) -> list[int]:
    """Take over the named slots, as another gate would."""
    gate.slot_dir.mkdir(parents=True, exist_ok=True)
    fds: list[int] = []
    for name in names:
        fd = os.open(
            gate.slot_dir / name, os.O_WRONLY | os.O_CREAT | os.O_APPEND, 0o600
        )
        fds.append(fd)
        fcntl.flock(fd, fcntl.LOCK_EX)
    return fds


def release(fds: list[int]) -> None:
    for fd in fds:
        os.close(fd)


def _slot_is_free(path: Path) -> bool:
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_APPEND, 0o600)
    try:
        fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
    except BlockingIOError:
        return False
    finally:
        os.close(fd)
    return True


def test_gate_waits_then_blocks_when_both_slots_are_held(gate: Gate) -> None:
    fds = hold_slots(gate, "slot-1", "slot-2")
    try:
        result = gate.run("git commit -m x", gate.project)
    finally:
        release(fds)

    assert result.returncode == 2, result.stdout
    assert "COULD NOT CHECK — all gate slots busy after 1 s (slots: 2)" in result.stderr
    assert not gate.mark.exists()


def test_slots_from_config(gate: Gate) -> None:
    gate.configure(TEST_GATE + "slots = 1\n")
    fds = hold_slots(gate, "slot-1")
    try:
        result = gate.run("git commit -m x", gate.project)
    finally:
        release(fds)

    assert result.returncode == 2, result.stdout
    assert "all gate slots busy after 1 s (slots: 1)" in result.stderr
    assert not gate.mark.exists()


def test_three_slots_take_the_third(gate: Gate) -> None:
    gate.configure(TEST_GATE + "slots = 3\n")
    fds = hold_slots(gate, "slot-1", "slot-2")
    try:
        result = gate.run("git commit -m x", gate.project)
    finally:
        release(fds)

    assert result.returncode == 0, result.stderr
    assert gate.marks() == [str(gate.project.resolve())]


@pytest.mark.parametrize("slots", [0, 8])
def test_slots_out_of_range_block(gate: Gate, slots: int) -> None:
    gate.configure(TEST_GATE + f"slots = {slots}\n")

    result = gate.run("git commit -m x", gate.project)

    assert result.returncode == 2, result.stdout
    assert "COULD NOT CHECK" in result.stderr
    assert "test_gate.slots" in result.stderr
    assert not gate.mark.exists()


def test_gate_takes_the_free_slot(gate: Gate) -> None:
    fds = hold_slots(gate, "slot-1")
    try:
        result = gate.run("git commit -m x", gate.project)
    finally:
        release(fds)

    assert result.returncode == 0, result.stderr
    assert gate.marks() == [str(gate.project.resolve())]


def test_gate_blocks_when_the_lock_dir_cannot_be_made(gate: Gate) -> None:
    blocker = gate.tmp / "geen-map"
    blocker.write_text("ik ben een bestand\n", encoding="utf-8")

    result = gate.run("git commit -m x", gate.project, LIGHTHOUSE_GATE_SLOT_DIR=str(blocker))

    assert result.returncode == 2, result.stdout
    assert "COULD NOT CHECK — cannot take a gate slot" in result.stderr
    assert f"lock dir {blocker} cannot be created" in result.stderr
    assert not gate.mark.exists()


def test_gate_blocks_when_a_lock_file_cannot_be_opened(gate: Gate) -> None:
    (gate.slot_dir / "slot-1").mkdir(parents=True)

    result = gate.run("git commit -m x", gate.project)

    assert result.returncode == 2, result.stdout
    assert "cannot take a gate slot" in result.stderr
    assert f"lock files in {gate.slot_dir} cannot be opened" in result.stderr
    assert not gate.mark.exists()


def test_gate_blocks_when_gate_slot_breaks(gate: Gate) -> None:
    # A python3 that stops with exit 2 only for gate_slot.py, without a
    # slot number; config, parser and no_verify run as normal.
    gate.add_stub(
        "python3",
        "#!/bin/sh\n"
        'case "$1" in *gate_slot.py) echo kapot >&2; exit 2 ;; esac\n'
        f'exec "{REAL_PYTHON3}" "$@"\n',
    )

    result = gate.run("git commit -m x", gate.project)

    assert result.returncode == 2, result.stdout
    assert "cannot take a gate slot: gate_slot.py gave exit 2" in result.stderr
    assert not gate.mark.exists()


def test_foreign_commit_takes_no_slot(gate: Gate) -> None:
    foreign = gate.tmp / "Y"
    init_repo(foreign)

    fds = hold_slots(gate, "slot-1", "slot-2")
    try:
        start = time.monotonic()
        result = gate.run(f"cd {foreign} && git commit -m x", gate.tmp)
        elapsed = time.monotonic() - start
    finally:
        release(fds)

    assert result.returncode == 0, result.stderr
    assert elapsed < 5
    assert "skipped — other project" in result.stdout
    assert not gate.mark.exists()


def test_slot_stays_held_until_deadline_py_ends(gate: Gate) -> None:
    # Fake `date`: the deadline falls 8 s after now. Fake `uv` records that it runs
    # and then sleeps long; that way `deadline.py` lives until that deadline.
    gate.add_stub("date", "#!/bin/sh\necho $(( $(/bin/date +%s) - 262 ))\n")
    gate.add_stub("uv", '#!/bin/sh\npwd >> "$UV_MARK"\nsleep 30\n')
    tmpdir = gate.tmp / "tmpdir"
    tmpdir.mkdir()
    env = dict(
        os.environ,
        PATH=f"{gate.stub}{os.pathsep}{os.environ['PATH']}",
        CLAUDE_PROJECT_DIR=str(gate.project),
        UV_MARK=str(gate.mark),
        LIGHTHOUSE_TOML=str(gate.toml),
        LIGHTHOUSE_HOST_TOML=str(gate.host),
        LIGHTHOUSE_GATE_SLOT_DIR=str(gate.slot_dir),
        LIGHTHOUSE_GATE_CGROUP_ROOT=str(gate.cgroup),
        TMPDIR=str(tmpdir),
    )
    payload = json.dumps(bash_payload("git commit -m x", gate.project))
    start = time.monotonic()
    hook = subprocess.Popen(
        [str(HOOK), "pre"],
        stdin=subprocess.PIPE,
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
        text=True,
        cwd=str(gate.project),
        env=env,
    )
    assert hook.stdin is not None
    hook.stdin.write(payload)
    hook.stdin.close()
    try:
        while not gate.marks():
            assert time.monotonic() - start < 15, "nep-uv startte niet"
            time.sleep(0.05)

        # The hook (bash) dies while pytest runs: deadline.py holds the
        # slot until the deadline plus its grace of 10 s.
        os.kill(hook.pid, signal.SIGKILL)
        hook.wait()
        slot = gate.slot_dir / "slot-1"
        assert not _slot_is_free(slot)

        while not _slot_is_free(slot):
            assert time.monotonic() - start < 8 + 10 + 2, "slot kwam niet vrij"
            time.sleep(0.2)
    finally:
        if hook.poll() is None:
            hook.kill()
            hook.wait()


BRACE_FD = re.compile(r"\{[A-Za-z_][A-Za-z0-9_]*\}[<>]")


def test_hook_has_no_bash4_fd_variables() -> None:
    # Bash 3.2 (macOS /bin/bash) does not know `exec {var}>>`: the hook would die there on
    # that line with an exit that is not 2, and the commit goes through untested.
    # First prove that the pattern finds the form.
    assert BRACE_FD.search("exec {S1}>>f") is not None

    hits = [
        line
        for line in HOOK.read_text(encoding="utf-8").splitlines()
        if not line.lstrip().startswith("#") and BRACE_FD.search(line)
    ]

    assert hits == []


# --- The brake (#1184, Task 3) ---------------------------------------------------

# systemd does not report the properties in the order of the `-p` arguments;
# the stub deliberately reverses them.
REM_ON_SYSTEMCTL = (
    "#!/bin/sh\n"
    'printf "MemoryMax=9663676416\\nFragmentPath=/x/x.slice\\n"\n'
    "exit 0\n"
)
OOM_NOTICE = "gate throttled (oom-kill in x.slice during this run)"
PRESSURE_NOTICE = "x.slice was under memory pressure"
SIGNAL_NOTICE = "gate killed by signal 9 (systemd-oomd in x.slice?)"


def rem_on(gate: Gate) -> None:
    """A `systemctl` for which `x.slice` is installed and has a ceiling."""
    gate.add_stub("systemctl", REM_ON_SYSTEMCTL)


def events_file(gate: Gate) -> Path:
    return gate.cgroup / "x.slice" / "memory.events"


def events(gate: Gate, text: str) -> None:
    """The counters of `x.slice` before the run."""
    path = events_file(gate)
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(text + "\n", encoding="utf-8")


def uv_writing(gate: Gate, text: str, exit_code: int) -> None:
    """A fake `uv` that rewrites `memory.events` during the run."""
    after = gate.tmp / "events-after"
    after.write_text(text + "\n", encoding="utf-8")
    target = events_file(gate)
    gate.add_stub(
        "uv",
        "#!/bin/sh\n"
        'pwd >> "$UV_MARK"\n'
        f'mkdir -p "{target.parent}"\n'
        f'cat "{after}" > "{target}"\n'
        'echo "3 passed in 0.01s"\n'
        f"exit {exit_code}\n",
    )


def test_gate_wraps_pytest_in_the_slice_and_raises_its_score(gate: Gate) -> None:
    rem_on(gate)

    result = gate.run("git commit -m x", gate.project)

    assert result.returncode == 0, result.stderr
    wrap = Path(f"{gate.mark}.wrap").read_text(encoding="utf-8").splitlines()
    choom = Path(f"{gate.mark}.choom").read_text(encoding="utf-8").splitlines()
    assert len(wrap) == 1
    assert "--user --scope --slice=x.slice --quiet --" in wrap[0]
    assert len(choom) == 1
    assert choom[0].startswith("-n 800 --")
    assert gate.args()[0].startswith("run pytest")
    assert "memory brake" not in result.stdout


@pytest.mark.parametrize("host", ["absent", "without_key"])
def test_without_gate_slice_no_wrap(gate: Gate, host: str) -> None:
    rem_on(gate)
    if host == "absent":
        gate.host.unlink()
    else:
        gate.host.write_text(HOST_WITHOUT_SLICE, encoding="utf-8")

    result = gate.run("git commit -m x", gate.project)

    assert result.returncode == 0, result.stderr
    assert gate.marks() == [str(gate.project.resolve())]
    assert (
        "commit-tests: no gate_slice in host.toml, tests run without memory brake"
        in _context(result.stdout)
    )
    assert not Path(f"{gate.mark}.wrap").exists()


def test_signal_kill_without_gate_slice_names_it(gate: Gate) -> None:
    gate.host.write_text(HOST_WITHOUT_SLICE, encoding="utf-8")

    result = gate.run("git commit -m x", gate.project, UV_EXIT="137")

    assert result.returncode == 2, result.stdout
    assert "gate killed by signal 9 (no gate_slice in host.toml)." in result.stderr
    assert "systemd-oomd" not in result.stderr


@pytest.mark.parametrize(
    ("show", "reason"),
    [
        ("FragmentPath=\nMemoryMax=infinity\n", "x.slice not installed"),
        (
            "FragmentPath=/x/x.slice\nMemoryMax=infinity\n",
            "x.slice without MemoryMax",
        ),
    ],
)
def test_gate_still_tests_without_the_rem(gate: Gate, show: str, reason: str) -> None:
    gate.add_stub("systemctl", f"#!/bin/sh\ncat <<'EOF'\n{show}EOF\n")

    result = gate.run("git commit -m x", gate.project)

    assert result.returncode == 0, result.stderr
    assert gate.marks() == [str(gate.project.resolve())]
    assert f"memory brake not available: {reason}" in _context(result.stdout)
    assert not Path(f"{gate.mark}.wrap").exists()


def test_gate_reports_a_failing_systemctl(gate: Gate) -> None:
    result = gate.run("git commit -m x", gate.project)

    assert result.returncode == 0, result.stderr
    assert "memory brake not available: systemctl --user failed" in _context(result.stdout)
    assert not Path(f"{gate.mark}.wrap").exists()


def test_gate_reports_a_missing_choom(gate: Gate) -> None:
    rem_on(gate)
    (gate.stub / "choom").unlink()
    # Without /usr/bin, so without the real `choom` of the host.
    bin_dir = tool_bin(
        gate.tmp / "bin",
        ("jq", "python3", "git", "cat", "dirname", "mktemp", "rm", "date", "mkdir"),
    )

    result = gate.run("git commit -m x", gate.project, path=str(bin_dir))

    assert result.returncode == 0, result.stderr
    assert gate.marks() == [str(gate.project.resolve())]
    assert "memory brake not available: no choom" in _context(result.stdout)


def test_gate_reports_oom_kill_in_the_slice(gate: Gate) -> None:
    rem_on(gate)
    events(gate, "oom_kill 0\nhigh 0")
    uv_writing(gate, "oom_kill 1\nhigh 4", 137)

    result = gate.run("git commit -m x", gate.project)

    assert result.returncode == 2, result.stdout
    assert "commit-tests: COULD NOT CHECK — " + OOM_NOTICE in result.stderr
    assert "Root:" in result.stderr
    assert "3 passed" in result.stderr
    assert "tests failed" not in result.stderr


def test_gate_counts_a_missing_events_file_as_zero(gate: Gate) -> None:
    rem_on(gate)
    uv_writing(gate, "oom_kill 1", 1)

    result = gate.run("git commit -m x", gate.project)

    assert result.returncode == 2, result.stdout
    assert OOM_NOTICE in result.stderr
    assert "tests failed" not in result.stderr


def test_gate_ignores_a_negative_delta(gate: Gate) -> None:
    rem_on(gate)
    events(gate, "oom_kill 3")
    uv_writing(gate, "oom_kill 0", 137)

    result = gate.run("git commit -m x", gate.project)

    assert result.returncode == 2, result.stdout
    assert OOM_NOTICE not in result.stderr
    assert SIGNAL_NOTICE in result.stderr


def test_green_run_stays_green_beside_a_killed_neighbour(gate: Gate) -> None:
    rem_on(gate)
    events(gate, "oom_kill 0")
    uv_writing(gate, "oom_kill 1", 0)

    result = gate.run("git commit -m x", gate.project)

    assert result.returncode == 0, result.stderr
    assert gate.marks() == [str(gate.project.resolve())]
    assert OOM_NOTICE not in result.stdout


def test_gate_reports_systemd_run_that_did_not_start(gate: Gate) -> None:
    rem_on(gate)
    gate.add_stub(
        "systemd-run",
        '#!/bin/sh\necho "Failed to start transient scope unit: x" >&2\nexit 1\n',
    )

    result = gate.run("git commit -m x", gate.project)

    assert result.returncode == 2, result.stdout
    assert "commit-tests: COULD NOT CHECK — systemd-run did not start" in result.stderr
    assert "Failed to start transient scope unit: x" in result.stderr
    assert "tests failed" not in result.stderr
    assert not gate.mark.exists()


@pytest.mark.parametrize(
    "bus_error",
    [
        "Failed to connect to bus: x",
        "Failed to create bus connection: x",
    ],
)
def test_gate_reports_systemd_run_without_a_bus(gate: Gate, bus_error: str) -> None:
    rem_on(gate)
    gate.add_stub(
        "systemd-run",
        f'#!/bin/sh\necho "{bus_error}" >&2\nexit 1\n',
    )

    result = gate.run("git commit -m x", gate.project)

    assert result.returncode == 2, result.stdout
    assert "commit-tests: COULD NOT CHECK — systemd-run did not start" in result.stderr
    assert bus_error in result.stderr
    assert "tests failed" not in result.stderr
    assert not gate.mark.exists()


def test_deadline_message_names_memory_pressure(gate: Gate) -> None:
    rem_on(gate)
    events(gate, "high 0")
    uv_writing(gate, "high 7", 124)

    result = gate.run("git commit -m x", gate.project)

    assert result.returncode == 2, result.stdout
    lines = result.stderr.splitlines()
    assert "deadline of 270 s" in lines[0]
    assert PRESSURE_NOTICE in lines[1]


def test_deadline_message_unchanged_without_pressure(gate: Gate) -> None:
    rem_on(gate)
    events(gate, "high 0")
    uv_writing(gate, "high 0", 124)

    result = gate.run("git commit -m x", gate.project)

    assert result.returncode == 2, result.stdout
    assert "deadline of 270 s" in result.stderr
    assert "memory pressure" not in result.stderr


@pytest.mark.parametrize("code", ["137", "247"])
def test_gate_reports_a_signal_kill(gate: Gate, code: str) -> None:
    rem_on(gate)

    result = gate.run("git commit -m x", gate.project, UV_EXIT=code)

    assert result.returncode == 2, result.stdout
    assert "commit-tests: COULD NOT CHECK — " + SIGNAL_NOTICE in result.stderr
    assert "tests failed" not in result.stderr


def test_signal_kill_without_the_rem_names_the_reason_not_the_slice(gate: Gate) -> None:
    # Without the brake (here: the default stub, `systemctl --user` fails) there is no
    # gates.slice and so no systemd-oomd in that slice either.
    result = gate.run("git commit -m x", gate.project, UV_EXIT="137")

    assert result.returncode == 2, result.stdout
    assert (
        "commit-tests: COULD NOT CHECK — gate killed by signal 9 "
        "(memory brake not available: systemctl --user failed)." in result.stderr
    )
    assert "systemd-oomd" not in result.stderr
    assert "tests failed" not in result.stderr


@pytest.mark.skipif(
    not Path(f"/run/user/{os.getuid()}").is_dir(), reason="geen /run/user"
)
def test_gate_finds_the_user_bus_without_xdg(gate: Gate) -> None:
    # VS Code `claude` in the logind scope has no XDG_RUNTIME_DIR; without a
    # bus `systemctl --user` fails and the brake silently drops out.
    gate.add_stub(
        "systemctl",
        "#!/bin/sh\n"
        'echo "${XDG_RUNTIME_DIR:-<leeg>}" > "$UV_MARK.xdg"\n'
        'echo "Failed to connect to bus" >&2\n'
        "exit 1\n",
    )

    result = gate.run(
        "git commit -m x",
        gate.project,
        XDG_RUNTIME_DIR="",
        DBUS_SESSION_BUS_ADDRESS="",
    )

    assert result.returncode == 0, result.stderr
    seen = Path(f"{gate.mark}.xdg").read_text(encoding="utf-8").strip()
    assert seen == f"/run/user/{os.getuid()}"


def test_slot_wait_is_capped_by_the_deadline(gate: Gate) -> None:
    # A wait longer than the hook timeout (300 s) let the commit through untested after the
    # timeout; the wait counts towards the deadline.
    gate.configure(
        TEST_GATE.replace("slot_wait_seconds = 1", "slot_wait_seconds = 100000")
        + "deadline_seconds = 2\n"
    )
    fds = hold_slots(gate, "slot-1", "slot-2")
    try:
        start = time.monotonic()
        result = gate.run("git commit -m x", gate.project)
        elapsed = time.monotonic() - start
    finally:
        release(fds)

    assert result.returncode == 2, result.stdout
    assert "all gate slots busy" in result.stderr
    assert elapsed < 10
    assert not gate.mark.exists()


# --- `pre` per branch (#80): test a commit to the default branch, record the rest ---


def test_pre_records_instead_of_testing_on_feature_branch(gate: Gate) -> None:
    git(gate.project, "checkout", "-q", "-b", "issue-1-x")

    result = gate.run("git commit -m x", gate.project)

    assert result.returncode == 0, result.stderr
    assert not gate.mark.exists()
    markers = gate.markers()
    assert len(markers) == 1
    heads = json.loads(markers[0].read_text(encoding="utf-8"))["heads"]
    assert list(heads) == [str(gate.project.resolve())]


def test_pre_tests_before_commit_to_main(gate: Gate) -> None:
    # `allow-main` lets a commit on main through the guard; it does not skip the tests.
    (gate.project / ".claude").mkdir()
    (gate.project / ".claude" / "allow-main").touch()

    result = gate.run("git commit -m x", gate.project, UV_EXIT="1")

    assert result.returncode == 2, result.stdout
    assert "commit-tests: tests failed" in result.stderr
    assert gate.marks() == [str(gate.project.resolve())]
    assert gate.markers() == []


def test_pre_tests_on_configured_default_branch(gate: Gate) -> None:
    gate.toml.write_text(
        MINIMAL_TOML + 'default_branch = "trunk"\n\n' + TEST_GATE, encoding="utf-8"
    )
    git(gate.project, "branch", "-m", "main", "trunk")

    on_trunk = gate.run("git commit -m x", gate.project)

    assert on_trunk.returncode == 0, on_trunk.stderr
    assert gate.marks() == [str(gate.project.resolve())]
    assert gate.markers() == []

    gate.mark.unlink()
    git(gate.project, "checkout", "-q", "-b", "main")
    on_main = gate.run("git commit -m x", gate.project)

    assert on_main.returncode == 0, on_main.stderr
    assert not gate.mark.exists()
    assert len(gate.markers()) == 1


def test_pre_detached_head_is_not_the_default_branch(gate: Gate) -> None:
    git(gate.project, "checkout", "-q", "--detach")

    result = gate.run("git commit -m x", gate.project)

    assert result.returncode == 0, result.stderr
    assert not gate.mark.exists()
    assert len(gate.markers()) == 1


def test_pre_unreadable_branch_blocks(gate: Gate) -> None:
    gate.add_stub(
        "git",
        "#!/bin/sh\n"
        'case "$*" in\n'
        '  *"branch --show-current"*) echo "fatal: kapot" >&2; exit 128 ;;\n'
        "esac\n"
        f'exec "{REAL_GIT}" "$@"\n',
    )

    result = gate.run("git commit -m x", gate.project)

    assert result.returncode == 2, result.stdout
    assert "commit-tests: COULD NOT CHECK" in result.stderr
    assert "kapot" in result.stderr
    assert not gate.mark.exists()
    assert gate.markers() == []


def test_pre_unreadable_default_branch_blocks(gate: Gate) -> None:
    gate.toml.write_text(
        MINIMAL_TOML + "default_branch = 3\n\n" + TEST_GATE, encoding="utf-8"
    )

    result = gate.run("git commit -m x", gate.project)

    assert result.returncode == 2, result.stdout
    assert "commit-tests: COULD NOT CHECK" in result.stderr
    assert "forge.default_branch" in result.stderr
    assert not gate.mark.exists()


def test_pre_background_commit_is_reported(gate: Gate) -> None:
    git(gate.project, "checkout", "-q", "-b", "issue-1-x")

    result = gate.run("git commit -m x", gate.project, background=True)

    assert result.returncode == 0, result.stderr
    message = (
        f"commit-tests: {gate.project.resolve()} — commit in a background call is not"
        " tested; commit in the foreground."
    )
    assert json.loads(result.stdout)["systemMessage"] == message
    assert _context(result.stdout) == message
    assert not gate.mark.exists()
    assert gate.markers() == []


def test_pre_record_failure_blocks(gate: Gate) -> None:
    git(gate.project, "checkout", "-q", "-b", "issue-1-x")
    # A file where the state dir must go: `record` cannot write its marker.
    blocker = gate.tmp / "not-a-dir"
    blocker.write_text("x", encoding="utf-8")

    result = gate.run("git commit -m x", gate.project, XDG_RUNTIME_DIR=str(blocker))

    assert result.returncode == 2, result.stdout
    assert "commit-tests: COULD NOT CHECK — could not record HEAD: " in result.stderr
    assert not gate.mark.exists()


def test_pre_tests_main_and_records_feature_in_one_call(gate: Gate) -> None:
    feature = gate.tmp / "F"
    _worktree(gate.project, feature, "issue-1-x")

    result = gate.run(
        f"git commit -m a && git -C {feature} commit -m b", gate.project
    )

    assert result.returncode == 0, result.stderr
    assert gate.marks() == [str(gate.project.resolve())]
    markers = gate.markers()
    assert len(markers) == 1
    heads = json.loads(markers[0].read_text(encoding="utf-8"))["heads"]
    assert list(heads) == [str(feature.resolve())]


@pytest.mark.parametrize("mode", [(), ("bogus",)], ids=["missing", "unknown"])
def test_unknown_mode_blocks(gate: Gate, mode: tuple[str, ...]) -> None:
    result = gate.run("git commit -m x", gate.project, mode=mode)

    assert result.returncode == 2, result.stdout
    assert (
        "commit-tests: COULD NOT CHECK — unknown mode, expected pre or post"
        in result.stderr
    )
    assert not gate.mark.exists()


def test_hooks_json_registers_pre_and_post() -> None:
    registered = json.loads(HOOKS_JSON.read_text(encoding="utf-8"))["hooks"]

    def entries(event: str) -> list[dict]:
        return [
            hook
            for block in registered[event]
            if block.get("matcher") == "Bash"
            for hook in block["hooks"]
            if "commit-tests.sh" in hook["command"]
        ]

    pre = entries("PreToolUse")
    assert [h["command"] for h in pre] == ["${CLAUDE_PLUGIN_ROOT}/hooks/commit-tests.sh pre"]
    assert pre[0]["timeout"] == 300
    for event in ("PostToolUse", "PostToolUseFailure"):
        post = entries(event)
        assert [h["command"] for h in post] == [
            "${CLAUDE_PLUGIN_ROOT}/hooks/commit-tests.sh post"
        ], event
        assert post[0]["timeout"] == 300, event


# --- `post` (#80): test what the commit touched, after the commit ---

FIX_HINT = "commit-tests: tests failed on the commit you just made. Fix them in a new commit."


def _head(repo: Path) -> str:
    return subprocess.run(
        ["git", "-C", str(repo), "rev-parse", "HEAD"],
        check=True,
        capture_output=True,
        text=True,
    ).stdout.strip()


def _commit(repo: Path, *paths: str) -> None:
    for rel in paths:
        target = repo / rel
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_text(f"{rel}\n", encoding="utf-8")
        git(repo, "add", rel)
    git(repo, "commit", "-qm", "c")


def _pre_on_feature(gate: Gate) -> str:
    """`pre` on a feature branch: records the HEAD, which it returns."""
    git(gate.project, "checkout", "-q", "-b", "issue-1-x")
    base = _head(gate.project)
    result = gate.run("git commit -m x", gate.project)
    assert result.returncode == 0, result.stderr
    assert not gate.mark.exists()
    return base


def test_post_runs_command_with_commit_files(gate: Gate) -> None:
    base = _pre_on_feature(gate)
    _commit(gate.project, "a.py", "b/c.sh")

    result = gate.post("git commit -m x", gate.project)

    assert result.returncode == 0, result.stderr
    assert gate.marks() == [str(gate.project.resolve())]
    names, files = gate.seen()
    assert names["BASE"] == base
    assert names["HEAD"] == _head(gate.project)
    assert files == "a.py\nb/c.sh\n"
    # Beside LIGHTHOUSE_GATE_TMP, not in it: `pytest --basetemp` empties that directory.
    basetemp = Path(f"{gate.mark}.bt").read_text(encoding="utf-8").strip()
    assert names["FILES"] == f"{basetemp}.files"
    output = json.loads(result.stdout)
    assert output["hookSpecificOutput"]["hookEventName"] == "PostToolUse"
    assert "3 passed" in output["hookSpecificOutput"]["additionalContext"]
    assert gate.markers() == []


def test_post_red_is_exit_2_with_fix_hint(gate: Gate) -> None:
    _pre_on_feature(gate)
    _commit(gate.project, "a.py")

    result = gate.post("git commit -m x", gate.project, UV_EXIT="1")

    assert result.returncode == 2, result.stdout
    assert result.stderr.startswith(FIX_HINT + "\n"), result.stderr
    assert "3 passed" in result.stderr


def test_post_silent_when_head_did_not_move(gate: Gate) -> None:
    _pre_on_feature(gate)

    result = gate.post("git commit -m x", gate.project)

    assert result.returncode == 0, result.stderr
    assert result.stdout == ""
    assert result.stderr == ""
    assert not gate.mark.exists()
    assert gate.markers() == []


def test_post_without_record_reads_no_config(gate: Gate) -> None:
    # No marker: nothing to test, so no configuration, slot or brake — a broken
    # lighthouse.toml does not block a call `pre` did not record.
    gate.toml.write_text("[test_gate\n", encoding="utf-8")

    result = gate.post("git commit -m x", gate.project)

    assert result.returncode == 0, result.stderr
    assert result.stdout == ""
    assert not gate.mark.exists()
    assert not gate.slot_dir.exists()


def test_post_tests_commit_when_call_failed(gate: Gate) -> None:
    _pre_on_feature(gate)
    _commit(gate.project, "a.py")

    result = gate.post("git commit -m x", gate.project, event="PostToolUseFailure")

    assert result.returncode == 0, result.stderr
    assert gate.marks() == [str(gate.project.resolve())]
    output = json.loads(result.stdout)["hookSpecificOutput"]
    assert output["hookEventName"] == "PostToolUseFailure"
    assert "3 passed" in output["additionalContext"]


def test_post_two_commits_in_one_call(gate: Gate) -> None:
    base = _pre_on_feature(gate)
    _commit(gate.project, "a.py")
    _commit(gate.project, "b.py")

    result = gate.post("git commit -m x", gate.project)

    assert result.returncode == 0, result.stderr
    names, files = gate.seen()
    assert names["BASE"] == base
    assert names["HEAD"] == _head(gate.project)
    assert files == "a.py\nb.py\n"


def test_post_unchecked_exit_code_blocks(gate: Gate) -> None:
    gate.configure(TEST_GATE + "unchecked_exit_codes = [5]\n")
    _pre_on_feature(gate)
    _commit(gate.project, "a.py")

    result = gate.post("git commit -m x", gate.project, UV_EXIT="5")

    assert result.returncode == 2, result.stdout
    assert "commit-tests: COULD NOT CHECK" in result.stderr
    assert "exit code 5" in result.stderr
    assert "tests failed" not in result.stderr


def test_post_deadline_still_applies(gate: Gate) -> None:
    _pre_on_feature(gate)
    _commit(gate.project, "a.py")

    result = gate.post("git commit -m x", gate.project, UV_EXIT="124")

    assert result.returncode == 2, result.stdout
    assert "deadline of 270 s" in result.stderr
    assert "COULD NOT CHECK" in result.stderr


def test_post_unreadable_record_blocks(gate: Gate) -> None:
    _pre_on_feature(gate)
    _commit(gate.project, "a.py")
    gate.markers()[0].write_text("not json", encoding="utf-8")

    result = gate.post("git commit -m x", gate.project)

    assert result.returncode == 2, result.stdout
    assert "commit-tests: COULD NOT CHECK" in result.stderr
    assert not gate.mark.exists()


def test_post_files_failure_blocks(gate: Gate) -> None:
    _pre_on_feature(gate)
    # A base git does not know: `files` cannot diff it.
    marker = gate.markers()[0]
    marker.write_text(
        json.dumps({"heads": {str(gate.project.resolve()): "0" * 40}}), encoding="utf-8"
    )

    result = gate.post("git commit -m x", gate.project)

    assert result.returncode == 2, result.stdout
    assert "commit-tests: COULD NOT CHECK" in result.stderr
    assert not gate.mark.exists()


SELECTOR_UV = (
    "#!/bin/sh\n"
    'pwd >> "$UV_MARK"\n'
    "echo 'gate-go: skipped — x'\n"
    "echo ok\n"
    "echo 'gate: skipped — y'\n"
    "echo\n"
)


def test_post_green_shows_every_selector_line(gate: Gate) -> None:
    gate.add_stub("uv", SELECTOR_UV)
    _pre_on_feature(gate)
    _commit(gate.project, "a.py")

    result = gate.post("git commit -m x", gate.project)

    assert result.returncode == 0, result.stderr
    root = gate.project.resolve()
    lines = _context(result.stdout).splitlines()
    assert lines[-2:] == [
        f"commit-tests: {root} — gate-go: skipped — x",
        f"commit-tests: {root} — gate: skipped — y",
    ]
    assert f"{root} — ok" not in _context(result.stdout)


def test_pre_green_shows_every_selector_line(gate: Gate) -> None:
    gate.add_stub("uv", SELECTOR_UV)

    result = gate.run("git commit -m x", gate.project)

    assert result.returncode == 0, result.stderr
    root = gate.project.resolve()
    lines = _context(result.stdout).splitlines()
    assert lines[-2:] == [
        f"commit-tests: {root} — gate-go: skipped — x",
        f"commit-tests: {root} — gate: skipped — y",
    ]
    assert f"{root} — ok" not in _context(result.stdout)


def test_pre_runs_without_gate_files(gate: Gate) -> None:
    # `pre` on the default branch tests everything: no selector list, also when an
    # outer gate run (this suite inside a real gate) leaves the variables behind.
    result = gate.run(
        "git commit -m x",
        gate.project,
        LIGHTHOUSE_GATE_BASE="b",
        LIGHTHOUSE_GATE_HEAD="h",
        LIGHTHOUSE_GATE_FILES=str(gate.tmp / "outer.files"),
    )

    assert result.returncode == 0, result.stderr
    names, files = gate.seen()
    assert names == {"BASE": "", "HEAD": "", "FILES": ""}
    assert files == ""
