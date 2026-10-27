"""`pack/python/scripts/gate_python.py`: the Python selector of the test gate.
`select_tests` is tested on a file tree in `tmp_path`; the CLI with `echo` and `false`."""

import importlib.util
import os
import subprocess
import sys
from pathlib import Path

import pytest

SCRIPT = (
    Path(__file__).resolve().parents[3] / "pack" / "python" / "scripts" / "gate_python.py"
)
_spec = importlib.util.spec_from_file_location("gate_python", SCRIPT)
gate_python = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(gate_python)


@pytest.fixture
def tree(tmp_path):
    for rel in (
        "tests/pack/test_commit_tests.py",
        "tests/pack/test_deadline.py",
        "tests/pack/test_gate_python.py",
        "tests/pack/test_run_all.py",
        "tests/pack/conftest.py",
        "tests/test_dir x.py",
        "tests/test_sp ace.py",
    ):
        path = tmp_path / rel
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text("")
    return tmp_path


def select(tree, files, dirs=("tests",)):
    return gate_python.select_tests(files, tree, list(dirs))


def test_changed_test_file_selects_itself(tree):
    assert select(tree, ["tests/pack/test_deadline.py"]) == ["tests/pack/test_deadline.py"]


def test_hook_maps_to_test_with_underscores(tree):
    files = ["pack/core/hooks/commit-tests.sh"]
    assert select(tree, files) == ["tests/pack/test_commit_tests.py"]


def test_lib_module_maps_to_test(tree):
    files = [".lighthouse/lib/lighthouse_lib/deadline.py"]
    assert select(tree, files) == ["tests/pack/test_deadline.py"]


def test_extensionless_file_maps_to_test(tree):
    assert select(tree, ["bin/run-all"]) == ["tests/pack/test_run_all.py"]


def test_conftest_selects_nothing(tree):
    assert select(tree, ["tests/pack/conftest.py"]) == []


def test_markdown_selects_nothing(tree):
    assert select(tree, ["docs/deadline.md", "README.md"]) == []


def test_deleted_test_file_selects_nothing(tree):
    assert select(tree, ["tests/pack/test_gone.py"]) == []


def test_test_file_outside_test_dirs_does_not_count(tree):
    (tree / "other").mkdir()
    (tree / "other" / "test_x.py").write_text("")
    assert select(tree, ["other/test_x.py"]) == []


def test_several_test_dirs(tree):
    (tree / "t2").mkdir()
    (tree / "t2" / "test_deadline.py").write_text("")
    assert select(tree, ["lib/deadline.py"], ("tests", "t2")) == [
        "t2/test_deadline.py",
        "tests/pack/test_deadline.py",
    ]


def test_gate_python_path_with_space(tree):
    assert select(tree, ["src dir/sp ace.py"]) == ["tests/test_sp ace.py"]
    assert select(tree, ["tests/test_dir x.py"]) == ["tests/test_dir x.py"]


def test_result_is_sorted_and_unique(tree):
    files = ["pack/x/run-all.sh", "tests/pack/test_deadline.py", "a/run_all.py", "", "a/deadline"]
    assert select(tree, files) == [
        "tests/pack/test_deadline.py",
        "tests/pack/test_run_all.py",
    ]


def run_cli(cwd, files_env, *cmd):
    env = {k: v for k, v in os.environ.items() if k != "LIGHTHOUSE_GATE_FILES"}
    if files_env is not None:
        env["LIGHTHOUSE_GATE_FILES"] = str(files_env)
    return subprocess.run(
        [sys.executable, str(SCRIPT), *cmd],
        cwd=cwd,
        env=env,
        capture_output=True,
        text=True,
        check=False,
    )


def test_cli_without_files_env_runs_command_bare(tree):
    done = run_cli(tree, None, "--", "echo", "bare")
    assert done.returncode == 0
    assert done.stdout.strip() == "bare"


def test_cli_selects_tests_and_passes_them(tree):
    files = tree / "files.txt"
    files.write_text("lib/deadline.py\n")
    done = run_cli(tree, files, "--", "echo")
    assert done.returncode == 0
    assert "gate-python: 1 test files" in done.stdout
    assert "tests/pack/test_deadline.py" in done.stdout


def test_cli_tests_option_sets_the_test_dir(tree):
    (tree / "spec").mkdir()
    (tree / "spec" / "test_deadline.py").write_text("")
    files = tree / "files.txt"
    files.write_text("lib/deadline.py\n")
    done = run_cli(tree, files, "--tests", "spec", "--", "echo")
    assert "spec/test_deadline.py" in done.stdout
    assert "tests/pack" not in done.stdout


def test_cli_nothing_selected_is_rc_0_with_reason(tree):
    files = tree / "files.txt"
    files.write_text("docs/x.md\n")
    done = run_cli(tree, files, "--", "false")
    assert done.returncode == 0
    assert "gate-python: skipped — no test belongs to the commit" in done.stdout


def test_cli_returns_rc_of_command(tree):
    files = tree / "files.txt"
    files.write_text("lib/deadline.py\n")
    assert run_cli(tree, files, "--", "false").returncode == 1


# Exit 3 is "nothing was tested" (COULD NOT CHECK): a project lists it in
# `unchecked_exit_codes`. Exit 2 cannot go there, pytest uses it.
UNCHECKED = "gate-python: COULD NOT CHECK — "


def test_cli_missing_files_list_is_rc_3(tree):
    done = run_cli(tree, tree / "nope.txt", "--", "echo")
    assert done.returncode == 3
    assert UNCHECKED + "cannot read the changed files" in done.stderr


@pytest.mark.parametrize(
    "args", [("echo",), ("--",), ("--tests", "--", "echo"), ("--bogus", "x", "--", "echo")]
)
def test_cli_usage_error_is_rc_3(tree, args):
    done = run_cli(tree, None, *args)
    assert done.returncode == 3, done.stderr
    assert UNCHECKED + "usage: gate_python.py" in done.stderr


def test_cli_command_that_cannot_start_is_rc_3(tree):
    files = tree / "files.txt"
    files.write_text("lib/deadline.py\n")
    done = run_cli(tree, files, "--", "lighthouse-no-such-cmd")
    assert done.returncode == 3
    assert UNCHECKED + "lighthouse-no-such-cmd cannot start" in done.stderr
    assert "Traceback" not in done.stderr


def test_cli_command_that_cannot_start_without_files_is_rc_3(tree):
    done = run_cli(tree, None, "--", "lighthouse-no-such-cmd")
    assert done.returncode == 3
    assert UNCHECKED + "lighthouse-no-such-cmd cannot start" in done.stderr


def test_cli_symlinked_cwd_still_selects_tests(tree, tmp_path_factory):
    link = tmp_path_factory.mktemp("links") / "link"
    link.symlink_to(tree, target_is_directory=True)
    files = tree / "files.txt"
    files.write_text("lib/deadline.py\n")
    env = {**os.environ, "LIGHTHOUSE_GATE_FILES": str(files), "PWD": str(link)}
    done = subprocess.run(
        [sys.executable, str(SCRIPT), "--", "echo"],
        cwd=link,
        env=env,
        capture_output=True,
        text=True,
        check=False,
    )
    assert done.returncode == 0
    assert "gate-python: 1 test files" in done.stdout
