"""`pack/go/scripts/gate_go.py`: the Go selector of the test gate. `packages_for` is
tested on a fixed `go list` listing; the CLI on a mini module with the real `go`."""

import importlib.util
import os
import shutil
import subprocess
import sys
from pathlib import Path

import pytest

SCRIPT = Path(__file__).resolve().parents[3] / "pack" / "go" / "scripts" / "gate_go.py"
_spec = importlib.util.spec_from_file_location("gate_go", SCRIPT)
gate_go = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(gate_go)

ROOT = Path("/repo")


def pkg(rel, **lists):
    path = "example.com/m" + (f"/{rel}" if rel else "")
    return {"ImportPath": path, "Dir": str(ROOT / rel) if rel else str(ROOT), **lists}


def select(files, listing):
    return gate_go.packages_for(files, listing, ROOT)


def test_go_file_maps_to_its_package():
    listing = [pkg("a", GoFiles=["a.go"]), pkg("b", GoFiles=["b.go"])]
    assert select(["a/a.go"], listing) == ["example.com/m/a"]


def test_test_file_maps_to_its_package():
    listing = [pkg("a", GoFiles=["a.go"], TestGoFiles=["a_test.go"])]
    assert select(["a/a_test.go"], listing) == ["example.com/m/a"]


def test_embedded_file_maps_to_embedding_package():
    listing = [pkg("pack", GoFiles=["pack.go"], EmbedFiles=["core/hooks/x.sh"])]
    assert select(["pack/core/hooks/x.sh"], listing) == ["example.com/m/pack"]


def test_testdata_maps_to_package():
    listing = [pkg("a", GoFiles=["a.go"])]
    assert select(["a/testdata/in/case.txt"], listing) == ["example.com/m/a"]


def test_go_mod_alone_selects_nothing():
    listing = [pkg("", GoFiles=["main.go"])]
    assert select(["go.mod", "go.sum"], listing) == []


def test_unrelated_file_selects_nothing():
    listing = [pkg("a", GoFiles=["a.go"])]
    assert select(["a/README.md", "docs/x.md"], listing) == []


def test_importers_are_not_selected():
    listing = [
        pkg("a", GoFiles=["a.go"], Imports=["example.com/m/b"]),
        pkg("b", GoFiles=["b.go"]),
    ]
    assert select(["b/b.go"], listing) == ["example.com/m/b"]


def test_deleted_go_file_maps_to_its_directory():
    listing = [pkg("a", GoFiles=["a.go"])]
    assert select(["a/gone.go"], listing) == ["example.com/m/a"]


def test_deleted_go_file_in_removed_directory_selects_nothing():
    listing = [pkg("a", GoFiles=["a.go"])]
    assert select(["old/gone.go"], listing) == []


def test_gate_go_path_with_space():
    listing = [pkg("dir x", GoFiles=["y.go"])]
    assert select(["dir x/y.go"], listing) == ["example.com/m/dir x"]


def test_result_is_sorted_and_unique():
    listing = [pkg("b", GoFiles=["b.go"]), pkg("a", GoFiles=["a.go"])]
    assert select(["b/b.go", "a/a.go", "b/b.go"], listing) == [
        "example.com/m/a",
        "example.com/m/b",
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


@pytest.fixture
def module(tmp_path):
    if shutil.which("go") is None:
        pytest.skip("go not on PATH")
    (tmp_path / "go.mod").write_text("module example.com/m\n\ngo 1.21\n")
    (tmp_path / "a").mkdir()
    (tmp_path / "a" / "a.go").write_text("package a\n")
    (tmp_path / "b").mkdir()
    (tmp_path / "b" / "b.go").write_text("package b\n")
    return tmp_path


def test_cli_without_files_env_runs_whole_module(module):
    done = run_cli(module, None, "--", "echo")
    assert done.returncode == 0
    assert "./..." in done.stdout


def test_cli_selects_touched_package(module):
    files = module / "files.txt"
    files.write_text("a/a.go\n")
    done = run_cli(module, files, "--", "echo")
    assert done.returncode == 0
    assert "gate-go: example.com/m/a" in done.stdout
    assert "example.com/m/b" not in done.stdout


def test_cli_nothing_selected_is_rc_0_with_reason(module):
    files = module / "files.txt"
    files.write_text("go.mod\n")
    done = run_cli(module, files, "--", "false")
    assert done.returncode == 0
    assert "gate-go: skipped — the commit touches no Go package" in done.stdout


def test_cli_returns_rc_of_command(module):
    files = module / "files.txt"
    files.write_text("a/a.go\n")
    assert run_cli(module, files, "--", "false").returncode == 1


# Exit 3 is "nothing was tested" (COULD NOT CHECK): a project lists it in
# `unchecked_exit_codes`. Exit 2 cannot go there, pytest uses it.
UNCHECKED = "gate-go: COULD NOT CHECK — "


def test_cli_go_list_failure_keeps_stderr_and_is_rc_3(module):
    # go list cannot list the module: no package was mapped, so nothing was tested.
    (module / "a" / "a.go").write_text("package a\nimport (\n")
    files = module / "files.txt"
    files.write_text("a/a.go\n")
    done = run_cli(module, files, "--", "echo")
    assert done.returncode == 3
    assert UNCHECKED + "go list failed" in done.stderr
    assert "a.go" in done.stderr
    assert "gate-go: example.com" not in done.stdout


@pytest.mark.parametrize("args", [("echo",), ("--",), ()])
def test_cli_usage_error_is_rc_3(args):
    done = run_cli(Path.cwd(), None, *args)
    assert done.returncode == 3, done.stderr
    assert UNCHECKED + "usage: gate_go.py -- CMD..." in done.stderr


def test_cli_missing_files_list_is_rc_3(module):
    done = run_cli(module, module / "nope.txt", "--", "echo")
    assert done.returncode == 3
    assert UNCHECKED + "cannot read the changed files" in done.stderr


def test_cli_missing_go_is_rc_3(tmp_path):
    files = tmp_path / "files.txt"
    files.write_text("a/a.go\n")
    empty = tmp_path / "bin"
    empty.mkdir()
    env = {k: v for k, v in os.environ.items() if k != "LIGHTHOUSE_GATE_FILES"}
    env.update(LIGHTHOUSE_GATE_FILES=str(files), PATH=str(empty))
    done = subprocess.run(
        [sys.executable, str(SCRIPT), "--", "echo"],
        cwd=tmp_path,
        env=env,
        capture_output=True,
        text=True,
        check=False,
    )
    assert done.returncode == 3
    assert UNCHECKED + "go not on PATH" in done.stderr
    assert "changed files" not in done.stderr


def test_cli_command_that_cannot_start_is_rc_3(module):
    files = module / "files.txt"
    files.write_text("a/a.go\n")
    done = run_cli(module, files, "--", "lighthouse-no-such-cmd")
    assert done.returncode == 3
    assert UNCHECKED + "lighthouse-no-such-cmd cannot start" in done.stderr
    assert "Traceback" not in done.stderr


def test_cli_command_that_cannot_start_without_files_is_rc_3(tmp_path):
    done = run_cli(tmp_path, None, "--", "lighthouse-no-such-cmd")
    assert done.returncode == 3
    assert UNCHECKED + "lighthouse-no-such-cmd cannot start" in done.stderr


def test_cli_symlinked_cwd_still_selects_package(module, tmp_path_factory):
    link = tmp_path_factory.mktemp("links") / "link"
    link.symlink_to(module, target_is_directory=True)
    files = module / "files.txt"
    files.write_text("a/a.go\n")
    env_pwd = str(link)
    env = {**os.environ, "LIGHTHOUSE_GATE_FILES": str(files), "PWD": env_pwd}
    done = subprocess.run(
        [sys.executable, str(SCRIPT), "--", "echo"],
        cwd=link,
        env=env,
        capture_output=True,
        text=True,
        check=False,
    )
    assert done.returncode == 0
    assert "gate-go: example.com/m/a" in done.stdout
