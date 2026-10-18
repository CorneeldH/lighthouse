"""`.claude/hooks/lib/evidence.py` (#1091): argv-allowlist en runner voor Evidence-rijen.

Een PR-body is onbetrouwbare tekst: wat buiten de allowlist valt draait nooit,
en er is geen shell. Netwerkverbod: elke test die `gh` kan bereiken zet een
stub-`gh` vooraan op `PATH`; er draait nergens een echte `uv`.
"""

import os
import subprocess
import sys
import time

import pytest

from tests.unit._hook_testkit import HOOKS, git, load_hook_lib, tool_bin

ev = load_hook_lib("evidence")
Row = ev.Row
LIB = HOOKS / "lib" / "evidence.py"

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
    "gh api repos/CorneeldH/personal_ai_agent/issues/1 --jq .state",
    "gh api graphql -f 'query={viewer{login}}' --jq .data",
    "gh pr view 5 --json body --jq .body",
    "gh issue view 5 -R CorneeldH/personal_ai_agent --comments",
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
    "uv run pytest tests/integration/test_beelink_hardening.py::test_ntfy_publish",
    "uv run pytest tests/unit/../integration/x.py",
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
    # Aanvullend: elke weigering uit de brief die de lijst hierboven niet raakt.
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
    # Een lege `--opt=` slikt het volgende woord niet: dat is een nieuwe optie.
    "git log --format= --output=/tmp/pwn",
    "git show --pretty= --output=/tmp/pwn HEAD",
    "git log --format= origin/main...HEAD",
    "gh api --jq= --method=DELETE repos/x/y",
    "gh api --jq= -XDELETE repos/x/y",
    "gh pr view 5 --json= --web",
    "gh issue view 5 --repo= -w",
    "git log --grep= --format=%G?",
    # Zelfde patroon, andere vormen: kaal `--pretty`, lege of `=`-beginnende korte waarde.
    "git log --pretty --output=/tmp/pwn",
    "git log --format %H --output=/tmp/pwn",
    "git log -n= --output=/tmp/pwn",
    "git log -n --output=/tmp/pwn",
    "git grep -e= --open-files-in-pager=vi x",
    "grep -e '' f",
    "gh api repos/x/y -q= -XDELETE",
    "gh pr view 5 -R= -w",
    # Zonder nummer leest gh de branch van de cwd; in de detached worktree van ingang C
    # is die er niet, en de rij faalt na de merge zonder dat er iets mis is.
    "gh pr view",
    "gh pr view --json number --jq .number",
    "gh issue view --json body",
]


@pytest.mark.parametrize("cmd", ALLOWED)
def test_toegestaan(cmd):
    assert ev.check(ev.split_command(f"`{cmd}`")) is None


@pytest.mark.parametrize("cmd", REFUSED)
def test_geweigerd(cmd):
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
def test_shell_operatoren_geweigerd(cmd):
    assert isinstance(ev.split_command(f"`{cmd}`"), str)


@pytest.mark.parametrize("cmd", ["gh pr view --json number", "gh issue view -c"])
def test_gh_view_zonder_nummer_noemt_de_reden(cmd):
    assert "precies één" in ev.check(ev.split_command(f"`{cmd}`"))


def test_backtick_in_rest_geweigerd():
    assert isinstance(ev.split_command("`echo `id``"), str)


def test_markdown_pipe_escape_in_quotes():  # Review Focus 3
    assert ev.split_command(r"`grep -E 'a\|b' f`") == ["grep", "-E", "a|b", "f"]


def test_markdown_pipe_escape_bare_is_pipe():
    assert isinstance(ev.split_command(r"`grep a f \| head`"), str)


@pytest.mark.parametrize(
    ("cel", "verwacht"),
    [
        ("`passed`", ("passed", False)),
        ("passed", ("passed", False)),
        ('`na PR: "number":`', ('"number":', True)),
        ("na PR: wereld", ("wereld", True)),
        ("`a\\|b`", ("a|b", False)),
    ],
)
def test_expected_normaliseert(cel, verwacht):
    assert ev.expected(cel) == verwacht


# --- run_row -----------------------------------------------------------------


def test_run_row_klopt(tmp_path):
    (tmp_path / "f").write_text("hallo wereld\n")
    out = ev.run_row(Row("1", "`grep -F hallo f`", "wereld"), str(tmp_path), True)
    assert out[0] == "klopt"


def test_run_row_substring_ontbreekt(tmp_path):
    (tmp_path / "f").write_text("hallo wereld\n")
    uitkomst, detail = ev.run_row(
        Row("1", "`grep -F hallo f`", "nope"), str(tmp_path), True
    )
    assert uitkomst == "klopt niet"
    assert "nope" in detail


def test_run_row_exitcode(tmp_path):
    uitkomst, detail = ev.run_row(
        Row("1", "`test -f bestaatniet`", "x"), str(tmp_path), True
    )
    assert uitkomst == "klopt niet"
    assert "1" in detail


def test_run_row_na_pr_voorvoegsel(tmp_path):
    (tmp_path / "f").write_text("hallo wereld\n")
    out = ev.run_row(
        Row("1", "`grep -F hallo f`", "na PR: wereld"), str(tmp_path), True
    )
    assert out[0] == "klopt"


def test_run_row_result_met_backticks(tmp_path):
    (tmp_path / "f").write_text("hallo wereld\n")
    out = ev.run_row(Row("1", "`grep -F hallo f`", "`wereld`"), str(tmp_path), True)
    assert out[0] == "klopt"


def test_run_row_lege_verwachting_is_ongeldig(tmp_path):
    out = ev.run_row(Row("1", "`ls`", "``"), str(tmp_path), True)
    assert out[0] == "ongeldig"


def test_run_row_buiten_allowlist_draait_niet(tmp_path):
    out = ev.run_row(Row("1", "`git show --output=x HEAD`", "x"), str(tmp_path), True)
    assert out[0] == "ongeldig" and not (tmp_path / "x").exists()
    assert "Evidence buiten allowlist" in out[1]


def test_run_row_gh_niet_bereikbaar(tmp_path):
    out = ev.run_row(Row("1", "`gh pr view 1`", "x"), str(tmp_path), False)
    assert out[0] == "niet meetbaar"


def test_run_row_binary_ontbreekt(tmp_path, monkeypatch):
    monkeypatch.setenv("PATH", str(tmp_path))
    out = ev.run_row(Row("1", "`grep -F a f`", "a"), str(tmp_path), True)
    assert out[0] == "niet meetbaar"


def test_run_row_timeout(tmp_path):
    # Een deadline van 0 s is al verlopen vóór de eerste lees: deterministisch.
    out = ev.run_row(Row("1", "`ls`", "x"), str(tmp_path), True, timeout=0)
    assert out[0] == "niet meetbaar"
    assert "timeout" in out[1]


@pytest.mark.parametrize("cmd", ["cat /dev/zero", "cat zeros"])
def test_run_row_uitvoer_begrensd(tmp_path, cmd):
    (tmp_path / "zeros").symlink_to("/dev/zero")
    t0 = time.monotonic()
    uitkomst, detail = ev.run_row(Row("1", f"`{cmd}`", "x"), str(tmp_path), True)
    assert time.monotonic() - t0 < 20
    assert uitkomst == "niet meetbaar"
    assert "uitvoer groter dan" in detail


def test_run_row_uitvoer_cap_kleiner(tmp_path, monkeypatch):
    monkeypatch.setattr(ev, "MAX_UITVOER", 10)
    (tmp_path / "f").write_text("een hele lange regel van meer dan tien bytes\n")
    out = ev.run_row(Row("1", "`cat f`", "regel"), str(tmp_path), True)
    assert out[0] == "niet meetbaar"


def test_run_row_uitvoer_onder_cap_klopt(tmp_path):
    (tmp_path / "f").write_text("hallo wereld\n")
    assert ev.run_row(Row("1", "`cat f`", "wereld"), str(tmp_path), True)[0] == "klopt"


@pytest.mark.parametrize(
    "cmd",
    [
        "git log -n1 --format= --output={pad}",
        "git show --pretty= --output={pad} HEAD",
        "git log --pretty --output={pad}",
    ],
)
def test_run_row_lege_optie_schrijft_niets(tmp_path, cmd):
    git(tmp_path, "init", "-q")
    git(tmp_path, "commit", "-q", "--allow-empty", "-m", "x")
    pwn = tmp_path / "pwn"
    out = ev.run_row(Row("1", f"`{cmd.format(pad=pwn)}`", "x"), str(tmp_path), True)
    assert out[0] == "ongeldig"
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
    tabel = "\n".join(f"| {ac} | {cmd} | {res} |" for ac, cmd, res in rows)
    return f"Closes #{nr}\n\n### Evidence #{nr}\n\n| AC | Command | Result |\n|---|---|---|\n{tabel}\n"


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


def test_cli_run_voor_pr_slaat_na_pr_over(tmp_path):
    proc, marker = _run(tmp_path, NA_PR_ROWS, "--voor-pr")
    assert proc.returncode == 0, proc.stdout + proc.stderr
    assert "| AC | Uitkomst | Detail |" in proc.stdout
    assert "na PR, niet gemeten" in proc.stdout
    assert not marker.exists()


def test_cli_run_zonder_voor_pr_draait_na_pr(tmp_path):
    proc, marker = _run(tmp_path, NA_PR_ROWS, gh_out="y")
    assert proc.returncode == 1, proc.stdout + proc.stderr
    assert marker.exists()
    assert "klopt niet" in proc.stdout


def test_cli_run_zonder_voor_pr_klopt(tmp_path):
    proc, _ = _run(tmp_path, NA_PR_ROWS, gh_out="x")
    assert proc.returncode == 0, proc.stdout + proc.stderr


def test_cli_run_voor_pr_na_pr_buiten_allowlist_is_ongeldig(tmp_path):
    rows = [("1. een", "`rm -rf x`", "na PR: x")]
    proc, _ = _run(tmp_path, rows, "--voor-pr")
    assert proc.returncode == 1
    assert "ongeldig" in proc.stdout


def test_cli_run_voor_pr_lege_na_pr_result_is_ongeldig(tmp_path):
    """Een lege `na PR: ` is in ingang C `ongeldig`; vóór de PR dus ook al."""
    rows = [("1. een", "`grep -F hallo f`", "`na PR: `")]
    proc, _ = _run(tmp_path, rows, "--voor-pr")
    assert proc.returncode == 1, proc.stdout + proc.stderr
    assert "ongeldig" in proc.stdout
    assert "lege Result-cel" in proc.stdout
    assert "na PR, niet gemeten" not in proc.stdout


def test_cli_run_gh_niet_bereikbaar_is_exit_4(tmp_path):
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
    assert "niet meetbaar" in proc.stdout


def test_cli_run_geen_tabel(tmp_path):
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
    assert proc.stdout.splitlines()[0].startswith("Closes zonder bewijsblok")
    assert not marker.exists()


def test_cli_run_mix(tmp_path):
    rows = [("1. een", "`grep -F hallo f`", "wereld"), ("2. twee", "`rm -rf x`", "x")]
    proc, _ = _run(tmp_path, rows)
    assert proc.returncode == 1
    assert "Evidence buiten allowlist" in proc.stdout
    assert "klopt" in proc.stdout


def test_cli_run_detail_breekt_de_tabel_niet(tmp_path):
    rows = [("1. een", "`grep -F hallo f`", "a\\|b")]
    proc, _ = _run(tmp_path, rows)
    assert proc.returncode == 1
    for regel in proc.stdout.splitlines():
        if regel.startswith("|") and "---" not in regel:
            assert len(regel.replace("\\|", "").split("|")) == 5  # leeg, 3 cellen, leeg


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
