"""`pack/node/scripts/gate_node.py`: the Node selector of the test gate.
`touches` is tested directly; the CLI runs with a fake `npm` and `node` in `tmp_path/bin`."""

import importlib.util
import os
import subprocess
import sys
from pathlib import Path

import pytest

SCRIPT = Path(__file__).resolve().parents[3] / "pack" / "node" / "scripts" / "gate_node.py"
_spec = importlib.util.spec_from_file_location("gate_node", SCRIPT)
gate_node = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(gate_node)

FAKE_NPM = """#!/bin/sh
echo "$PWD $*" >> "$NPM_LOG"
script="$3"
if [ "$script" = gen ]; then
  if [ -n "$GEN_CONTENT" ]; then printf '%s' "$GEN_CONTENT" > src/api/client.ts; fi
  if [ -n "$GEN_NEW" ]; then printf '%s' "$GEN_NEW" > src/api/new.ts; fi
fi
eval "exit \\${NPM_EXIT_$script:-0}"
"""
FAKE_NODE = "#!/bin/sh\nexit 0\n"


def _exe(path: Path, text: str) -> None:
    path.write_text(text)
    path.chmod(0o755)


@pytest.fixture
def project(tmp_path, monkeypatch):
    web = tmp_path / "web"
    (web / "node_modules").mkdir(parents=True)
    (web / "package.json").write_text("{}")
    (web / "src" / "api").mkdir(parents=True)
    (web / "src" / "api" / "client.ts").write_text("v1")
    bin_dir = tmp_path / "bin"
    bin_dir.mkdir()
    _exe(bin_dir / "npm", FAKE_NPM)
    _exe(bin_dir / "node", FAKE_NODE)
    monkeypatch.setenv("PATH", f"{bin_dir}{os.pathsep}{os.environ['PATH']}")
    monkeypatch.setenv("NPM_LOG", str(tmp_path / "npm.log"))
    for name in ("GEN_CONTENT", "GEN_NEW", "NPM_EXIT_lint", "NPM_EXIT_gen"):
        monkeypatch.delenv(name, raising=False)
    monkeypatch.chdir(tmp_path)
    return tmp_path


def changed(project, monkeypatch, text):
    path = project / "files.txt"
    path.write_text(text)
    monkeypatch.setenv("LIGHTHOUSE_GATE_FILES", str(path))


def log(project):
    path = project / "npm.log"
    return path.read_text().splitlines() if path.exists() else []


def run(argv):
    return gate_node.main(["gate_node.py", *argv])


def test_touches():
    assert gate_node.touches(["web/a.ts"], "web", [])
    assert not gate_node.touches(["pack/x.go"], "web", [])
    assert gate_node.touches(["x"], ".", [])
    assert gate_node.touches(["lighthouse.toml"], "web", ["lighthouse.toml"])
    assert not gate_node.touches([""], "web", [])


def test_dir_is_a_directory_boundary():
    assert not gate_node.touches(["website/a.ts"], "web", [])
    assert not gate_node.touches(["web"], "web", [])


def test_skips_when_commit_touches_nothing_under_dir(project, monkeypatch, capsys):
    changed(project, monkeypatch, "pack/go/x.go\n")
    assert run(["--dir", "web", "--", "lint"]) == 0
    assert "gate-node: skipped — the commit touches nothing under web" in capsys.readouterr().out
    assert not (project / "npm.log").exists()


def test_runs_drift_then_scripts_in_dir(project, monkeypatch, capsys):
    changed(project, monkeypatch, "web/src/a.ts\n")
    argv = ["--dir", "web", "--drift", "gen=src/api", "--", "lint", "typecheck", "test"]
    assert run(argv) == 0
    web = project / "web"
    assert log(project) == [
        f"{web} run --silent gen",
        f"{web} run --silent lint",
        f"{web} run --silent typecheck",
        f"{web} run --silent test",
    ]
    out = capsys.readouterr().out
    assert out.rstrip("\n").endswith("gate-node: passed — gen, lint, typecheck, test")


def test_also_path_touches(project, monkeypatch):
    changed(project, monkeypatch, "lighthouse.toml\n")
    assert run(["--dir", "web", "--also", "lighthouse.toml", "--", "lint"]) == 0
    assert len(log(project)) == 1


def test_path_with_space_touches(project, monkeypatch):
    changed(project, monkeypatch, "web/src/a b.ts\n")
    assert run(["--dir", "web", "--", "lint"]) == 0
    assert len(log(project)) == 1


def test_stops_at_first_failure(project, monkeypatch):
    changed(project, monkeypatch, "web/a.ts\n")
    monkeypatch.setenv("NPM_EXIT_lint", "7")
    assert run(["--dir", "web", "--", "lint", "typecheck"]) == 7
    assert not any("typecheck" in line for line in log(project))


def test_without_gate_files_runs_everything(project, monkeypatch):
    monkeypatch.delenv("LIGHTHOUSE_GATE_FILES", raising=False)
    assert run(["--dir", "web", "--", "lint", "typecheck"]) == 0
    assert len(log(project)) == 2


def test_skips_without_node_modules(project, monkeypatch, capsys):
    (project / "web" / "node_modules").rmdir()
    changed(project, monkeypatch, "pack/go/x.go\n")
    assert run(["--dir", "web", "--", "lint"]) == 0
    assert "gate-node: skipped" in capsys.readouterr().out


def test_drift_stale_fails(project, monkeypatch, capsys):
    changed(project, monkeypatch, "web/a.ts\n")
    monkeypatch.setenv("GEN_CONTENT", "v2")
    assert run(["--dir", "web", "--drift", "gen=src/api", "--", "lint"]) == 1
    out = capsys.readouterr().out
    assert (
        "gate-node: web/src/api was stale; npm run gen regenerated it — commit the result" in out
    )
    assert not any("lint" in line for line in log(project))


def test_drift_new_file_is_stale(project, monkeypatch):
    changed(project, monkeypatch, "web/a.ts\n")
    monkeypatch.setenv("GEN_NEW", "x")
    assert run(["--dir", "web", "--drift", "gen=src/api", "--", "lint"]) == 1


def test_drift_up_to_date_passes(project, monkeypatch):
    changed(project, monkeypatch, "web/a.ts\n")
    monkeypatch.setenv("GEN_CONTENT", "v1")
    assert run(["--dir", "web", "--drift", "gen=src/api", "--", "lint"]) == 0


def test_drift_gen_failure_returns_its_code(project, monkeypatch):
    changed(project, monkeypatch, "web/a.ts\n")
    monkeypatch.setenv("NPM_EXIT_gen", "4")
    assert run(["--dir", "web", "--drift", "gen=src/api", "--", "lint"]) == 4
    assert not any("lint" in line for line in log(project))


def test_missing_node_modules_is_unchecked(project, monkeypatch, capsys):
    (project / "web" / "node_modules").rmdir()
    changed(project, monkeypatch, "web/a.ts\n")
    assert run(["--dir", "web", "--", "lint"]) == 3
    assert capsys.readouterr().err == (
        "gate-node: COULD NOT CHECK — web/node_modules missing; fix: cd web && npm ci\n"
    )


def test_npm_not_on_path_is_unchecked(project, monkeypatch, capsys):
    only_node = project / "only-node"
    only_node.mkdir()
    _exe(only_node / "node", FAKE_NODE)
    monkeypatch.setenv("PATH", str(only_node))
    changed(project, monkeypatch, "web/a.ts\n")
    assert run(["--dir", "web", "--", "lint"]) == 3
    assert "npm not on PATH" in capsys.readouterr().err


def test_unreadable_list_is_unchecked(project, monkeypatch):
    monkeypatch.setenv("LIGHTHOUSE_GATE_FILES", str(project / "missing.txt"))
    assert run(["--dir", "web", "--", "lint"]) == 3


@pytest.mark.parametrize(
    "argv",
    [
        [],
        ["--"],
        ["--dir"],
        ["--drift", "gen", "--", "lint"],
        ["--dir", "a", "--dir", "b", "--", "lint"],
        ["--bogus", "--", "lint"],
    ],
)
def test_usage_errors_are_unchecked(argv, capsys):
    assert run(argv) == 3
    assert "usage: gate_node.py" in capsys.readouterr().err


def test_cli_subprocess(project, monkeypatch):
    changed(project, monkeypatch, "pack/go/x.go\n")
    result = subprocess.run(
        [sys.executable, str(SCRIPT), "--dir", "web", "--", "lint"],
        cwd=project,
        env=os.environ.copy(),
        capture_output=True,
        text=True,
    )
    assert result.returncode == 0, result.stderr
