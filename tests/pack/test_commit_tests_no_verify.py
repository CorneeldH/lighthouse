"""The --no-verify refusal of pack/core `commit-tests.sh pre`, as installed.

From paia's `test_quality_hooks.py`: the hook runs as a subprocess with the JSON
on stdin, so that the contract (exit code + stderr) is tested and not a
reimplementation of it.
"""

import json
import subprocess
from pathlib import Path

import pytest

from tests.pack._testkit import INSTALLED_HOOKS


def _run(hook: Path, payload: dict, cwd: Path) -> subprocess.CompletedProcess:
    return subprocess.run(
        [str(hook), "pre"],
        input=json.dumps(payload),
        capture_output=True,
        text=True,
        cwd=str(cwd),
        check=False,
    )


PRECOMMIT_HOOK = INSTALLED_HOOKS / "commit-tests.sh"


def test_precommit_hook_blocks_no_verify(tmp_path):
    result = _run(
        PRECOMMIT_HOOK,
        {
            "tool_input": {"command": "git commit --no-verify -m 'x'"},
            "cwd": str(tmp_path),
        },
        tmp_path,
    )
    assert result.returncode == 2, result.stdout
    assert "--no-verify" in result.stderr


def test_precommit_hook_blocks_short_no_verify_flag(tmp_path):
    result = _run(
        PRECOMMIT_HOOK,
        {"tool_input": {"command": "git commit -n -m 'x'"}, "cwd": str(tmp_path)},
        tmp_path,
    )
    assert result.returncode == 2, result.stdout


def test_precommit_hook_ignores_non_commit_commands(tmp_path):
    result = _run(
        PRECOMMIT_HOOK,
        {
            "tool_input": {"command": "git status --no-verify-nonsense"},
            "cwd": str(tmp_path),
        },
        tmp_path,
    )
    assert result.returncode == 0, result.stderr


def test_precommit_hook_ignores_no_verify_in_commit_message_text(tmp_path):
    """The flag as text in a quoted -m message is not a real --no-verify."""
    result = _run(
        PRECOMMIT_HOOK,
        {
            "tool_input": {
                "command": 'git commit -m "docs: explain --no-verify guard"'
            },
            "cwd": str(tmp_path),
        },
        tmp_path,
    )
    assert (
        result.returncode != 2 or "--no-verify is not allowed" not in result.stderr
    )


def test_precommit_hook_blocks_an_bundle(tmp_path):
    """-a and -n bundled (git really honours this): -an must block just like -n."""
    result = _run(
        PRECOMMIT_HOOK,
        {"tool_input": {"command": 'git commit -an -m "x"'}, "cwd": str(tmp_path)},
        tmp_path,
    )
    assert result.returncode == 2, result.stdout
    assert "--no-verify" in result.stderr


def test_precommit_hook_blocks_na_bundle(tmp_path):
    result = _run(
        PRECOMMIT_HOOK,
        {"tool_input": {"command": 'git commit -na -m "x"'}, "cwd": str(tmp_path)},
        tmp_path,
    )
    assert result.returncode == 2, result.stdout
    assert "--no-verify" in result.stderr


def test_precommit_hook_blocks_anm_bundle(tmp_path):
    """-n before the value-demanding -m in the same cluster: still a block."""
    result = _run(
        PRECOMMIT_HOOK,
        {"tool_input": {"command": 'git commit -anm "x"'}, "cwd": str(tmp_path)},
        tmp_path,
    )
    assert result.returncode == 2, result.stdout
    assert "--no-verify" in result.stderr


def test_precommit_hook_allows_am_bundle(tmp_path):
    """-am is -a + -m (value), no -n in it — may pass."""
    result = _run(
        PRECOMMIT_HOOK,
        {"tool_input": {"command": 'git commit -am "x"'}, "cwd": str(tmp_path)},
        tmp_path,
    )
    assert (
        result.returncode != 2 or "--no-verify is not allowed" not in result.stderr
    )


def test_precommit_hook_allows_apostrophe_idiom_in_message(tmp_path):
    """The bash apostrophe-in-single-quote trick must not leave an unpaired quote behind."""
    result = _run(
        PRECOMMIT_HOOK,
        {
            "tool_input": {
                "command": "git commit -m 'don'\"'\"'t use --no-verify here'"
            },
            "cwd": str(tmp_path),
        },
        tmp_path,
    )
    assert (
        result.returncode != 2 or "--no-verify is not allowed" not in result.stderr
    )


def test_precommit_hook_allows_escaped_quotes_in_message(tmp_path):
    result = _run(
        PRECOMMIT_HOOK,
        {
            "tool_input": {
                "command": 'git commit -m "he said \\"--no-verify\\" today"'
            },
            "cwd": str(tmp_path),
        },
        tmp_path,
    )
    assert (
        result.returncode != 2 or "--no-verify is not allowed" not in result.stderr
    )


def test_precommit_hook_allows_no_verify_as_message_value(tmp_path):
    """ "-n" as a literal -m value is not a flag."""
    result = _run(
        PRECOMMIT_HOOK,
        {"tool_input": {"command": 'git commit -m "-n"'}, "cwd": str(tmp_path)},
        tmp_path,
    )
    assert (
        result.returncode != 2 or "--no-verify is not allowed" not in result.stderr
    )


def test_precommit_hook_allows_untracked_files_no_short_flag(tmp_path):
    """-uno is --untracked-files=no; the scan must not continue to the "n" in "no".

    Regression for issue #591 finding 2: -u/-S take a value attached to the letter,
    so the cluster scan must stop as soon as it meets them.
    """
    result = _run(
        PRECOMMIT_HOOK,
        {"tool_input": {"command": 'git commit -uno -m "x"'}, "cwd": str(tmp_path)},
        tmp_path,
    )
    assert (
        result.returncode != 2 or "--no-verify is not allowed" not in result.stderr
    )


def test_precommit_hook_allows_gpg_sign_short_flag(tmp_path):
    """-Sjohn is --gpg-sign=john; the same attached-only value as -u."""
    result = _run(
        PRECOMMIT_HOOK,
        {"tool_input": {"command": 'git commit -Sjohn -m "x"'}, "cwd": str(tmp_path)},
        tmp_path,
    )
    assert (
        result.returncode != 2 or "--no-verify is not allowed" not in result.stderr
    )


def test_precommit_hook_allows_no_verify_after_bare_separator(tmp_path):
    """After a bare -- --no-verify is a path argument, not an option."""
    result = _run(
        PRECOMMIT_HOOK,
        {"tool_input": {"command": "git commit -- --no-verify"}, "cwd": str(tmp_path)},
        tmp_path,
    )
    assert (
        result.returncode != 2 or "--no-verify is not allowed" not in result.stderr
    )


@pytest.mark.parametrize(
    "command",
    [
        'git add x && git commit -m "a" && grep -n foo x',
        "git commit -m a && git log -n 1",
        "head -n 3 x; git commit -m a",
    ],
)
def test_precommit_hook_ignores_short_n_of_other_commands(tmp_path, command):
    """A `-n` of `grep`/`git log`/`head` in the same command is no --no-verify (#1453)."""
    result = _run(
        PRECOMMIT_HOOK,
        {"tool_input": {"command": command}, "cwd": str(tmp_path)},
        tmp_path,
    )
    assert (
        result.returncode != 2 or "--no-verify is not allowed" not in result.stderr
    )


@pytest.mark.parametrize(
    "command",
    [
        "git log -n 1 && git commit -an -m x",
        "git -C /tmp commit -n -m x",
        'bash -c "git commit -n -m x"',
        'echo "$(git commit --no-verify -m x)"',
    ],
)
def test_precommit_hook_still_blocks_no_verify_in_compound_command(tmp_path, command):
    """The flag on the commit invocation itself keeps blocking, also next to other
    commands (#1453)."""
    result = _run(
        PRECOMMIT_HOOK,
        {"tool_input": {"command": command}, "cwd": str(tmp_path)},
        tmp_path,
    )
    assert result.returncode == 2, result.stdout
    assert "--no-verify is not allowed" in result.stderr
