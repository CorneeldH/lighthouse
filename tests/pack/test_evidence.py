"""`lighthouse evidence` (#1091, #279): argv allowlist and runner for Evidence rows.

A PR body is untrusted text: what falls outside the allowlist never runs,
and there is no shell. Black-box tests of the binary; the `run_row` tests are
Go twins in `internal/evidence`. Network ban: every test that can reach `gh`
puts a stub `gh` first on `PATH`; no real `uv` runs anywhere.
"""

import os
import subprocess

import pytest

from tests.pack._testkit import tool_bin

# The binary of this checkout, first on the PATH.
pytestmark = pytest.mark.usefixtures("lighthouse_bin")

GH_STUB = """#!/bin/sh
touch "$GH_MARKER"
case "$1 $2" in
  "api rate_limit") echo 4999; exit 0 ;;
esac
echo "${GH_STUB_OUT:-y}"
"""


def _evidence(*args, env=None):
    return subprocess.run(
        ["lighthouse", "evidence", *args],
        capture_output=True,
        text=True,
        env=env,
        check=False,
    )


def _cli(tmp_path, *args, gh_out="y", with_stub=True):
    bindir = tool_bin(
        tmp_path / "bin",
        ("grep", "ls", "cat", "test", "wc", "git", "env", "sh", "touch", "echo"),
        stubs={"gh": GH_STUB} if with_stub else None,
    )
    marker = tmp_path / "gh-aangeroepen"
    env = dict(
        os.environ,
        PATH=f"{bindir}{os.pathsep}{os.environ['PATH']}",
        GH_MARKER=str(marker),
        GH_STUB_OUT=gh_out,
    )
    return _evidence(*args, env=env), marker


def _body(nr, rows):
    table = "\n".join(f"| {ac} | {cmd} | {res} |" for ac, cmd, res in rows)
    return f"Closes #{nr}\n\n### Evidence #{nr}\n\n| AC | Command | Result |\n|---|---|---|\n{table}\n"


def _run(tmp_path, rows, *extra, nr=5, gh_out="y"):
    (tmp_path / "f").write_text("hallo wereld\n")
    body = tmp_path / "body.md"
    body.write_text(_body(nr, rows))
    return _cli(
        tmp_path,
        "run",
        "--issue",
        str(nr),
        "--body-file",
        str(body),
        "--cwd",
        str(tmp_path),
        *extra,
        gh_out=gh_out,
    )


NA_PR_ROWS = [
    ("1. een", "`grep -F hallo f`", "wereld"),
    ("2. twee", "`gh api graphql -f 'query={viewer{login}}'`", "`na PR: x`"),
]


def test_cli_run_before_pr_skips_after_pr(tmp_path):
    proc, marker = _run(tmp_path, NA_PR_ROWS, "--before-pr")
    assert proc.returncode == 0, proc.stdout + proc.stderr
    assert "| AC | Outcome | Detail |" in proc.stdout
    assert "after PR, not measured" in proc.stdout
    assert not marker.exists()


def test_cli_run_without_before_pr_runs_after_pr(tmp_path):
    proc, marker = _run(tmp_path, NA_PR_ROWS, gh_out="y")
    assert proc.returncode == 1, proc.stdout + proc.stderr
    assert marker.exists()
    assert "does not hold" in proc.stdout


def test_cli_run_without_before_pr_holds(tmp_path):
    proc, _ = _run(tmp_path, NA_PR_ROWS, gh_out="x")
    assert proc.returncode == 0, proc.stdout + proc.stderr


def test_cli_run_before_pr_after_pr_outside_allowlist_is_invalid(tmp_path):
    rows = [("1. een", "`rm -rf x`", "na PR: x")]
    proc, _ = _run(tmp_path, rows, "--before-pr")
    assert proc.returncode == 1
    assert "invalid" in proc.stdout


def test_cli_run_before_pr_empty_after_pr_result_is_invalid(tmp_path):
    """An empty `na PR: ` is `invalid` in entry C; so before the PR too."""
    rows = [("1. een", "`grep -F hallo f`", "`na PR: `")]
    proc, _ = _run(tmp_path, rows, "--before-pr")
    assert proc.returncode == 1, proc.stdout + proc.stderr
    assert "invalid" in proc.stdout
    assert "empty Result cell" in proc.stdout
    assert "after PR, not measured" not in proc.stdout


def test_cli_run_gh_not_reachable_is_exit_4(tmp_path):
    (tmp_path / "f").write_text("x\n")
    body = tmp_path / "body.md"
    body.write_text(_body(5, [("1. een", "`gh pr view 1`", "x")]))
    failing = "#!/bin/sh\nexit 1\n"
    bindir = tool_bin(tmp_path / "bin", ("ls",), stubs={"gh": failing})
    proc = _evidence(
        "run",
        "--issue",
        "5",
        "--body-file",
        str(body),
        "--cwd",
        str(tmp_path),
        env=dict(os.environ, PATH=f"{bindir}{os.pathsep}{os.environ['PATH']}"),
    )
    assert proc.returncode == 4, proc.stdout + proc.stderr
    assert "not measurable" in proc.stdout


def test_cli_run_no_table(tmp_path):
    body = tmp_path / "body.md"
    body.write_text("Closes #5\n\ngeen bewijs\n")
    proc, marker = _cli(
        tmp_path,
        "run",
        "--issue",
        "5",
        "--body-file",
        str(body),
        "--cwd",
        str(tmp_path),
    )
    assert proc.returncode == 2
    assert proc.stdout.splitlines()[0].startswith("Closes without evidence block")
    assert not marker.exists()


def test_cli_run_mix(tmp_path):
    rows = [("1. een", "`grep -F hallo f`", "wereld"), ("2. twee", "`rm -rf x`", "x")]
    proc, _ = _run(tmp_path, rows)
    assert proc.returncode == 1
    assert "Evidence outside the allowlist" in proc.stdout
    assert "| holds |" in proc.stdout


def test_cli_run_detail_does_not_break_the_table(tmp_path):
    rows = [("1. een", "`grep -F hallo f`", "a\\|b")]
    proc, _ = _run(tmp_path, rows)
    assert proc.returncode == 1
    for line in proc.stdout.splitlines():
        if line.startswith("|") and "---" not in line:
            assert len(line.replace("\\|", "").split("|")) == 5  # empty, 3 cells, empty


def test_cli_check():
    ok = _evidence("check", "`grep -F x f`")
    assert ok.returncode == 0 and ok.stdout == ""
    nok = _evidence("check", "`rm -rf x`")
    assert nok.returncode == 1 and nok.stdout.strip()


# --- lighthouse: config and headings ------------------------------------------


def _toml(tmp_path, body):
    path = tmp_path / "lighthouse.toml"
    path.write_text('[forge]\nkind = "github"\nrepo = "Acme/widgets"\n' + body)
    return path


def test_pytest_path_from_config(tmp_path, monkeypatch):
    monkeypatch.setenv("LIGHTHOUSE_TOML", str(_toml(tmp_path, '[test_gate]\nevidence_test_dir = "t/"\n')))
    allowed = _evidence("check", "`uv run pytest t/x.py`")
    assert allowed.returncode == 0 and allowed.stdout == ""
    refused = _evidence("check", "`uv run pytest tests/x.py`")
    assert refused.returncode == 1 and "`t/`" in refused.stdout


def test_pytest_path_default_is_tests(tmp_path):
    allowed = _evidence("check", "`uv run pytest tests/x.py::test_y`")
    assert allowed.returncode == 0 and allowed.stdout == ""
    assert _evidence("check", "`uv run pytest src/x.py`").returncode == 1


def test_run_accepts_a_refs_body(tmp_path):
    (tmp_path / "f").write_text("hallo wereld\n")
    body = tmp_path / "body.md"
    body.write_text(
        _body(5, [("1. een", "`grep -F hallo f`", "wereld")]).replace(
            "Closes #5", "Refs #5 — close-issue toetst na de merge", 1
        )
    )
    proc, _ = _cli(
        tmp_path, "run", "--issue", "5", "--body-file", str(body),
        "--cwd", str(tmp_path), gh_out="y",
    )
    assert proc.returncode == 0, proc.stdout + proc.stderr
    assert "| klopt |" in proc.stdout or "| holds |" in proc.stdout


@pytest.mark.parametrize(
    "language,holds,head",
    [("nl", "| klopt |", "| AC | Uitkomst | Detail |"), ("en", "| holds |", "| AC | Outcome | Detail |")],
)
def test_outcomes_follow_language(tmp_path, monkeypatch, language, holds, head):
    toml = _toml(tmp_path, f'[project]\nlanguage = "{language}"\n')
    monkeypatch.setenv("LIGHTHOUSE_TOML", str(toml))
    proc, _ = _run(tmp_path, [("1. een", "`grep -F hallo f`", "wereld")])
    assert proc.returncode == 0, proc.stdout + proc.stderr
    assert head in proc.stdout and holds in proc.stdout
