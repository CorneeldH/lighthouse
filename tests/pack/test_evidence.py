"""`pack/core/lib/evidence.py` (#1091): argv allowlist and runner for Evidence rows.

A PR body is untrusted text: what falls outside the allowlist never runs,
and there is no shell. Network ban: every test that can reach `gh` puts a
stub `gh` first on `PATH`; no real `uv` runs anywhere.
"""

import os
import subprocess
import sys
import time

import pytest

from tests.pack._testkit import INSTALLED_LIB, git, load_hook_lib, tool_bin

ev = load_hook_lib("evidence")
Row = ev.Row
LIB = INSTALLED_LIB / "evidence.py"

ALLOWED = [
    "grep -F 'Evidence #' .github/pull_request_template.md",
    "grep -rnF x src/",
    "git grep -nF walk -- .claude/hooks",
    "test -f AGENTS.md",
    "ls -1 docs",
    "wc -l AGENTS.md",
    "cat AGENTS.md",
    "git log --oneline -n 5 -- AGENTS.md",
    "git log -n5 --format=%H%x09%s",
    "git show --stat abc123",
    "git ls-files -- .claude/hooks",
    "uv run pytest tests/unit/test_x.py::test_y -q",
    "gh api repos/Acme/widgets/issues/1 --jq .state",
    "gh api graphql -f 'query={viewer{login}}' --jq .data",
    "gh pr view 5 --json body --jq .body",
    "gh issue view 5 -R Acme/widgets --comments",
]
REFUSED = [
    "git show --output=/tmp/x HEAD",
    "git log --output=x",
    "git grep -O vi x",
    "git grep --open-files-in-pager=vi x",
    "git -c core.pager=sh log",
    "git -C /tmp log",
    "git log --format=%G?",
    "git log origin/main...HEAD",
    "git log HEAD~1",
    "git log @{u}",
    "uv run --with x pytest tests/unit/a.py",
    "uv run pytest -p no:x tests/unit/a.py",
    "uv run pytest -o cache_dir=/tmp tests/unit/a.py",
    "uv run pytest -k x tests/unit/a.py",
    "uv run pytest integration/test_host_hardening.py::test_ntfy_publish",
    "uv run pytest tests/../integration/x.py",
    "gh api -X DELETE repos/x/y",
    "gh api --method=POST repos/x/y",
    "gh api repos/x/y -f a=b",
    "gh api repos/x/y --input f",
    "gh api -H 'X-HTTP-Method-Override: DELETE' repos/x/y",
    "gh api graphql -f 'query=mutation{x}'",
    "gh api graphql -f query=a -f query=b",
    "gh pr view 5 --web",
    "gh issue view 5 -w",
    "FOO=1 grep x f",
    "/usr/bin/grep x f",
    "rm -rf x",
    "python3 -c 1",
    "bash -c ls",
    # Additional: every refusal from the brief that the list above does not cover.
    "uv run pytest -q",
    "uv run pytest -- tests/unit/a.py",
    "uv run pytest @args tests/unit/a.py",
    "git log -n x",
    "git log --pretty=format:%GS",
    "git log --grep=x -- ../x",
    "git show x^2",
    "git status",
    "git",
    "grep -P x f",
    "grep --include=*.py -m 3 x f",
    "ls --color=always",
    "ls -- x",
    "wc --files0-from=x",
    "cat -E f",
    "gh",
    "gh api",
    "gh api repos/x/y repos/x/z",
    "gh api https://evil.example/x",
    "gh api graphql --jq .data",
    "gh api repos/x/y --jq env",
    "gh api repos/x/y --paginate",
    "gh pr view 5 6",
    "gh pr create",
    "gh issue close 5",
    "gh issue view 5 -- x",
    # An empty `--opt=` does not swallow the next word: that is a new option.
    "git log --format= --output=/tmp/pwn",
    "git show --pretty= --output=/tmp/pwn HEAD",
    "git log --format= origin/main...HEAD",
    "gh api --jq= --method=DELETE repos/x/y",
    "gh api --jq= -XDELETE repos/x/y",
    "gh pr view 5 --json= --web",
    "gh issue view 5 --repo= -w",
    "git log --grep= --format=%G?",
    # Same pattern, other forms: bare `--pretty`, an empty or `=`-leading short value.
    "git log --pretty --output=/tmp/pwn",
    "git log --format %H --output=/tmp/pwn",
    "git log -n= --output=/tmp/pwn",
    "git log -n --output=/tmp/pwn",
    "git grep -e= --open-files-in-pager=vi x",
    "grep -e '' f",
    "gh api repos/x/y -q= -XDELETE",
    "gh pr view 5 -R= -w",
    # Without a number, gh reads the branch of the cwd; in the detached worktree of entry C
    # there is none, and the row fails after the merge with nothing wrong.
    "gh pr view",
    "gh pr view --json number --jq .number",
    "gh issue view --json body",
]


@pytest.mark.parametrize("cmd", ALLOWED)
def test_allowed(cmd):
    assert ev.check(ev.split_command(f"`{cmd}`")) is None


@pytest.mark.parametrize("cmd", REFUSED)
def test_refused(cmd):
    argv = ev.split_command(f"`{cmd}`")
    assert isinstance(argv, str) or ev.check(argv) is not None


@pytest.mark.parametrize(
    "cmd",
    [
        "grep x f | head",
        "ls; rm x",
        "ls && rm x",
        "cat f > g",
        "echo $(id)",
        "grep 'open f",
    ],
)
def test_shell_operators_refused(cmd):
    assert isinstance(ev.split_command(f"`{cmd}`"), str)


@pytest.mark.parametrize("cmd", ["gh pr view --json number", "gh issue view -c"])
def test_gh_view_without_number_names_the_reason(cmd):
    assert "exactly one" in ev.check(ev.split_command(f"`{cmd}`"))


def test_backtick_in_rest_refused():
    assert isinstance(ev.split_command("`echo `id``"), str)


def test_markdown_pipe_escape_in_quotes():  # Review Focus 3
    assert ev.split_command(r"`grep -E 'a\|b' f`") == ["grep", "-E", "a|b", "f"]


def test_markdown_pipe_escape_bare_is_pipe():
    assert isinstance(ev.split_command(r"`grep a f \| head`"), str)


@pytest.mark.parametrize(
    ("cell", "expected"),
    [
        ("`passed`", ("passed", False)),
        ("passed", ("passed", False)),
        ('`na PR: "number":`', ('"number":', True)),
        ("na PR: wereld", ("wereld", True)),
        ("`after PR: wereld`", ("wereld", True)),
        ("`a\\|b`", ("a|b", False)),
    ],
)
def test_expected_normalises(cell, expected):
    assert ev.expected(cell) == expected


# --- run_row -----------------------------------------------------------------


def test_run_row_holds(tmp_path):
    (tmp_path / "f").write_text("hallo wereld\n")
    out = ev.run_row(Row("1", "`grep -F hallo f`", "wereld"), str(tmp_path), True)
    assert out[0] == ev.HOLDS


def test_run_row_substring_missing(tmp_path):
    (tmp_path / "f").write_text("hallo wereld\n")
    outcome, detail = ev.run_row(
        Row("1", "`grep -F hallo f`", "nope"), str(tmp_path), True
    )
    assert outcome == ev.FAILS
    assert "nope" in detail


def test_run_row_exit_code(tmp_path):
    outcome, detail = ev.run_row(
        Row("1", "`test -f bestaatniet`", "x"), str(tmp_path), True
    )
    assert outcome == ev.FAILS
    assert "1" in detail


def test_run_row_after_pr_prefix(tmp_path):
    (tmp_path / "f").write_text("hallo wereld\n")
    out = ev.run_row(
        Row("1", "`grep -F hallo f`", "na PR: wereld"), str(tmp_path), True
    )
    assert out[0] == ev.HOLDS


def test_run_row_result_with_backticks(tmp_path):
    (tmp_path / "f").write_text("hallo wereld\n")
    out = ev.run_row(Row("1", "`grep -F hallo f`", "`wereld`"), str(tmp_path), True)
    assert out[0] == ev.HOLDS


def test_run_row_empty_expectation_is_invalid(tmp_path):
    out = ev.run_row(Row("1", "`ls`", "``"), str(tmp_path), True)
    assert out[0] == ev.INVALID


def test_run_row_outside_allowlist_does_not_run(tmp_path):
    out = ev.run_row(Row("1", "`git show --output=x HEAD`", "x"), str(tmp_path), True)
    assert out[0] == ev.INVALID and not (tmp_path / "x").exists()
    assert "Evidence outside the allowlist" in out[1]


def test_run_row_gh_not_reachable(tmp_path):
    out = ev.run_row(Row("1", "`gh pr view 1`", "x"), str(tmp_path), False)
    assert out[0] == ev.UNMEASURABLE


def test_run_row_binary_missing(tmp_path, monkeypatch):
    monkeypatch.setenv("PATH", str(tmp_path))
    out = ev.run_row(Row("1", "`grep -F a f`", "a"), str(tmp_path), True)
    assert out[0] == ev.UNMEASURABLE


def test_run_row_timeout(tmp_path):
    # A deadline of 0 s has already passed before the first read: deterministic.
    out = ev.run_row(Row("1", "`ls`", "x"), str(tmp_path), True, timeout=0)
    assert out[0] == ev.UNMEASURABLE
    assert "timeout" in out[1]


@pytest.mark.parametrize("cmd", ["cat /dev/zero", "cat zeros"])
def test_run_row_output_capped(tmp_path, cmd):
    (tmp_path / "zeros").symlink_to("/dev/zero")
    t0 = time.monotonic()
    outcome, detail = ev.run_row(Row("1", f"`{cmd}`", "x"), str(tmp_path), True)
    assert time.monotonic() - t0 < 20
    assert outcome == ev.UNMEASURABLE
    assert "output larger than" in detail


def test_run_row_output_cap_smaller(tmp_path, monkeypatch):
    monkeypatch.setattr(ev, "MAX_OUTPUT", 10)
    (tmp_path / "f").write_text("een hele lange regel van meer dan tien bytes\n")
    out = ev.run_row(Row("1", "`cat f`", "regel"), str(tmp_path), True)
    assert out[0] == ev.UNMEASURABLE


def test_run_row_output_under_cap_holds(tmp_path):
    (tmp_path / "f").write_text("hallo wereld\n")
    assert ev.run_row(Row("1", "`cat f`", "wereld"), str(tmp_path), True)[0] == ev.HOLDS


@pytest.mark.parametrize(
    "cmd",
    [
        "git log -n1 --format= --output={path}",
        "git show --pretty= --output={path} HEAD",
        "git log --pretty --output={path}",
    ],
)
def test_run_row_empty_option_writes_nothing(tmp_path, cmd):
    git(tmp_path, "init", "-q")
    git(tmp_path, "commit", "-q", "--allow-empty", "-m", "x")
    pwn = tmp_path / "pwn"
    out = ev.run_row(Row("1", f"`{cmd.format(path=pwn)}`", "x"), str(tmp_path), True)
    assert out[0] == ev.INVALID
    assert not pwn.exists()


# --- CLI ---------------------------------------------------------------------

GH_STUB = """#!/bin/sh
touch "$GH_MARKER"
case "$1 $2" in
  "api rate_limit") echo 4999; exit 0 ;;
esac
echo "${GH_STUB_OUT:-y}"
"""


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
    proc = subprocess.run(
        [sys.executable, str(LIB), *args],
        capture_output=True,
        text=True,
        env=env,
        check=False,
    )
    return proc, marker


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
    proc = subprocess.run(
        [
            sys.executable,
            str(LIB),
            "run",
            "--issue",
            "5",
            "--body-file",
            str(body),
            "--cwd",
            str(tmp_path),
        ],
        capture_output=True,
        text=True,
        env=dict(os.environ, PATH=f"{bindir}{os.pathsep}{os.environ['PATH']}"),
        check=False,
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
    ok = subprocess.run(
        [sys.executable, str(LIB), "check", f"`{ALLOWED[0]}`"],
        capture_output=True,
        text=True,
        check=False,
    )
    assert ok.returncode == 0 and ok.stdout == ""
    nok = subprocess.run(
        [sys.executable, str(LIB), "check", f"`{REFUSED[0]}`"],
        capture_output=True,
        text=True,
        check=False,
    )
    assert nok.returncode == 1 and nok.stdout.strip()


# --- lighthouse: config and headings ------------------------------------------


def _toml(tmp_path, body):
    path = tmp_path / "lighthouse.toml"
    path.write_text('[forge]\nkind = "github"\nrepo = "Acme/widgets"\n' + body)
    return path


def test_pytest_path_from_config(tmp_path, monkeypatch):
    monkeypatch.setenv("LIGHTHOUSE_TOML", str(_toml(tmp_path, '[test_gate]\nevidence_test_dir = "t/"\n')))
    assert ev.check(["uv", "run", "pytest", "t/x.py"]) is None
    assert "`t/`" in ev.check(["uv", "run", "pytest", "tests/x.py"])


def test_pytest_path_default_is_tests(tmp_path):
    assert ev.check(["uv", "run", "pytest", "tests/x.py::test_y"]) is None
    assert ev.check(["uv", "run", "pytest", "src/x.py"]) is not None


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


@pytest.mark.parametrize(
    "cell,outcome",
    [
        ("klopt", "HOLDS"),
        (" holds ", "HOLDS"),
        ("klopt niet", "FAILS"),
        ("does not hold", "FAILS"),
        ("ongeldig", "INVALID"),
        ("not measurable", "UNMEASURABLE"),
        ("na PR, niet gemeten", "AFTER_PR_UNMEASURED"),
        ("klopt nietje", None),
        ("holds back", None),
        ("het klopt", None),
    ],
)
def test_outcome_of_is_anchored_at_the_cell(cell, outcome):
    """`klopt` is a prefix of `klopt niet`: only the whole cell decides."""
    assert ev.outcome_of(cell) == (None if outcome is None else getattr(ev, outcome))
