"""De --no-verify-weigering van pack/core `pre-commit-tests.sh`, as installed.

Uit paia's `test_quality_hooks.py`: de hook draait als subprocess met de JSON
op stdin, zodat het contract (exit code + stderr) getest wordt en niet een
herimplementatie ervan.
"""

import json
import subprocess
from pathlib import Path

import pytest

from tests.pack._testkit import INSTALLED_HOOKS


def _run(hook: Path, payload: dict, cwd: Path) -> subprocess.CompletedProcess:
    return subprocess.run(
        [str(hook)],
        input=json.dumps(payload),
        capture_output=True,
        text=True,
        cwd=str(cwd),
        check=False,
    )


PRECOMMIT_HOOK = INSTALLED_HOOKS / "pre-commit-tests.sh"


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
    """De vlag als tekst in een quoted -m-boodschap is geen echte --no-verify."""
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
    """-a en -n gebundeld (git honoreert dit echt): -an moet net zo blokkeren als -n."""
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
    """-n vóór de waarde-opeisende -m in dezelfde cluster: nog steeds een blokkade."""
    result = _run(
        PRECOMMIT_HOOK,
        {"tool_input": {"command": 'git commit -anm "x"'}, "cwd": str(tmp_path)},
        tmp_path,
    )
    assert result.returncode == 2, result.stdout
    assert "--no-verify" in result.stderr


def test_precommit_hook_allows_am_bundle(tmp_path):
    """-am is -a + -m (waarde), geen -n erin — mag door."""
    result = _run(
        PRECOMMIT_HOOK,
        {"tool_input": {"command": 'git commit -am "x"'}, "cwd": str(tmp_path)},
        tmp_path,
    )
    assert (
        result.returncode != 2 or "--no-verify is not allowed" not in result.stderr
    )


def test_precommit_hook_allows_apostrophe_idiom_in_message(tmp_path):
    """De bash-apostrof-in-single-quote-truc mag geen ongepaarde quote achterlaten."""
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
    """ "-n" als letterlijke -m-waarde is geen vlag."""
    result = _run(
        PRECOMMIT_HOOK,
        {"tool_input": {"command": 'git commit -m "-n"'}, "cwd": str(tmp_path)},
        tmp_path,
    )
    assert (
        result.returncode != 2 or "--no-verify is not allowed" not in result.stderr
    )


def test_precommit_hook_allows_untracked_files_no_short_flag(tmp_path):
    """-uno is --untracked-files=no; het scannen mag niet doorlopen tot de "n" in "no".

    Regressie voor issue #591 finding 2: -u/-S nemen een aan-de-letter-vastzittende
    waarde, dus de cluster-scan moet stoppen zodra hij ze tegenkomt.
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
    """-Sjohn is --gpg-sign=john; dezelfde attached-only-waarde als -u."""
    result = _run(
        PRECOMMIT_HOOK,
        {"tool_input": {"command": 'git commit -Sjohn -m "x"'}, "cwd": str(tmp_path)},
        tmp_path,
    )
    assert (
        result.returncode != 2 or "--no-verify is not allowed" not in result.stderr
    )


def test_precommit_hook_allows_no_verify_after_bare_separator(tmp_path):
    """Na een losse -- is --no-verify een pad-argument, geen optie."""
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
    """Een `-n` van `grep`/`git log`/`head` in dezelfde opdracht is geen --no-verify (#1453)."""
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
    """De vlag op de commit-aanroep zelf blijft blokkeren, ook naast andere commando's (#1453)."""
    result = _run(
        PRECOMMIT_HOOK,
        {"tool_input": {"command": command}, "cwd": str(tmp_path)},
        tmp_path,
    )
    assert result.returncode == 2, result.stdout
    assert "--no-verify is not allowed" in result.stderr
