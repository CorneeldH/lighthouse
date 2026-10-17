"""`pack/core/lib/git_invocations.py` (#884): de gedeelde commitparser.

Verhuisd uit de `IFS= read -r -d '' PARSE <<'PYEOF'`-heredoc in
main-commit-guard.sh. De bestaande dekking van dat gedrag staat in
`test_instructie_hooks.py::test_commit_guard_*` (blackbox via de hook-CLI);
deze tests toetsen de module direct — de eenheid die vanaf nu ook door de
pre-commit test-gate (taak 2+) hergebruikt wordt.
"""

import ast
import subprocess
import sys

import pytest

from tests.pack._testkit import LIB, init_repo, load_hook_lib

gi = load_hook_lib("git_invocations")


@pytest.mark.parametrize(
    "cmd",
    [
        "git commit -m x",
        "cd {X} && git commit -m x",
        "cd {X}; git commit -m x",
        "true | git commit -m x",
        "false || git commit -m x",
        "git -C {X} commit -m x",
        "rtk git commit -m x",
        "git commit --amend --no-edit",
        "git commit -a",
    ],
)
def test_finds_commit_with_target(tmp_path, cmd):
    x = tmp_path / "x"
    init_repo(x)
    found = gi.find_invocations(
        cmd.format(X=x), str(x if "{X}" not in cmd else tmp_path)
    )
    assert [(a, t) for a, t, _, _ in found] == [("commit", str(x))]


@pytest.mark.parametrize(
    "cmd",
    [
        "cat <<'EOF'\ngit commit -m x\nEOF",
        'echo "git commit"',
        "# git commit",
    ],
)
def test_finds_no_commit(tmp_path, cmd):
    assert gi.find_invocations(cmd, str(tmp_path)) == []


def test_new_dir_outside_repo_is_dropped_by_default_and_marked_on_request(tmp_path):
    cmd = f"cd {tmp_path / 'nieuw'} && git commit -m x"
    assert gi.find_invocations(cmd, str(tmp_path)) == []
    found = gi.find_invocations(cmd, str(tmp_path), report_new_dirs=True)
    assert [t for _, t, _, _ in found] == [gi.NEW_DIR]


def test_new_dir_inside_repo_uses_ancestor_by_default_and_marked_on_request(tmp_path):
    x = tmp_path / "x"
    init_repo(x)
    cmd = f"cd {x / 'sub'} && git commit -m x"
    assert [t for _, t, _, _ in gi.find_invocations(cmd, str(x))] == [str(x)]
    found = gi.find_invocations(cmd, str(x), report_new_dirs=True)
    assert [t for _, t, _, _ in found] == [gi.NEW_DIR]


def test_repo_of_skips_a_dot_git_dir_without_head(tmp_path):
    # Een losse `.git/` zonder HEAD (op de Mac: `~/.git/workflow-audit-tools/`,
    # een verdwaalde toolcache) is voor git geen repo; zijn discovery loopt er
    # voorbij. Telt `repo_of` hem wel, dan wordt elke map eronder een wortel
    # waar git "not a git repository" op zegt.
    home = tmp_path / "home"
    (home / ".git" / "workflow-audit-tools").mkdir(parents=True)
    sub = home / ".claude" / "skills"
    sub.mkdir(parents=True)
    assert gi.repo_of(str(sub)) is None

    outer = tmp_path / "outer"
    init_repo(outer)
    inner = outer / "inner"
    (inner / ".git").mkdir(parents=True)
    assert gi.repo_of(str(inner)) == str(outer)


def test_repo_of_keeps_a_gitfile_and_a_dot_git_with_a_broken_head(tmp_path):
    # Het oordeel "kapot" blijft aan git: een worktree-gitfile naar een
    # verdwenen gitdir, of een `.git/HEAD` met rommel, is wél een wortel.
    wt = tmp_path / "wt"
    wt.mkdir()
    (wt / ".git").write_text("gitdir: /nergens/.git/worktrees/wt\n", encoding="utf-8")
    assert gi.repo_of(str(wt)) == str(wt)

    broken = tmp_path / "broken"
    init_repo(broken)
    (broken / ".git" / "HEAD").write_text("garbage\n", encoding="utf-8")
    assert gi.repo_of(str(broken)) == str(broken)


def test_open_quote_raises(tmp_path):
    with pytest.raises(gi.Unparseable):
        gi.find_invocations("git commit -m 'x", str(tmp_path))


def test_import_reads_no_stdin():  # de import hierboven slaagde zonder stdin; pin het
    assert callable(gi.main)


# `dirs` is levend: lees hem tijdens de iteratie, niet na list().
def _trace(command, cwd):
    return [(s.words[s.idx], s.dirs["here"]) for s in gi.walk(command, cwd)]


def test_walk_yields_the_dir_in_effect_for_each_segment(tmp_path):
    a = tmp_path / "a"
    a.mkdir()
    assert _trace(f"cd {a} && touch x; (cd /; ls); pwd", str(tmp_path)) == [
        ("cd", str(tmp_path)),
        ("touch", str(a)),
        ("cd", str(a)),
        ("ls", "/"),
        ("pwd", str(a)),
    ]


def test_walk_recurses_into_bash_c(tmp_path):
    assert _trace("bash -c 'cd / && ls'; pwd", str(tmp_path)) == [
        ("bash", str(tmp_path)),
        ("cd", str(tmp_path)),
        ("ls", "/"),
        ("pwd", str(tmp_path)),
    ]


def test_walk_raises_unparseable_on_an_open_quote(tmp_path):
    with pytest.raises(gi.Unparseable):
        list(gi.walk("echo 'open", str(tmp_path)))


def test_git_target_follows_dash_c(tmp_path):
    step = next(gi.walk(f"git -C {tmp_path}/x status", "/"))
    target, _raw, _new, action_idx = gi.git_target(step)
    assert target == f"{tmp_path}/x" and step.words[action_idx] == "status"


def test_cli_contract(tmp_path):
    x = tmp_path / "x"
    init_repo(x)
    out = subprocess.run(
        [sys.executable, str(LIB / "git_invocations.py"), str(x)],
        input="git commit -m x",
        capture_output=True,
        text=True,
        check=False,
    )
    assert out.returncode == 0 and out.stdout == f"commit\t{x}\tno\t{x}\n"
    bad = subprocess.run(
        [sys.executable, str(LIB / "git_invocations.py"), str(x)],
        input="git commit -m 'x",
        capture_output=True,
        text=True,
        check=False,
    )
    assert bad.returncode == 3


def _union_annotations(tree: ast.Module) -> list[int]:
    """Regelnummers van `X | Y` in een annotatie (runtime-fout vóór Python 3.10)."""
    annotations: list[ast.expr] = []
    for node in ast.walk(tree):
        if isinstance(node, (ast.FunctionDef, ast.AsyncFunctionDef)):
            args = node.args
            every = [*args.posonlyargs, *args.args, *args.kwonlyargs]
            every += [a for a in (args.vararg, args.kwarg) if a is not None]
            annotations += [a.annotation for a in every if a.annotation is not None]
            if node.returns is not None:
                annotations.append(node.returns)
        elif isinstance(node, ast.AnnAssign):
            annotations.append(node.annotation)
    return [
        sub.lineno
        for annotation in annotations
        for sub in ast.walk(annotation)
        if isinstance(sub, ast.BinOp) and isinstance(sub.op, ast.BitOr)
    ]


@pytest.mark.parametrize(
    "lib", sorted(LIB.glob("*.py")), ids=lambda p: p.name
)
def test_hook_lib_stays_importable_on_python_39(lib):
    """Pin: de hooks draaien onder `python3` van de host — op macOS 3.9.6.

    Daar is `list[str] | None` in een annotatie een TypeError bij de import,
    en dan eindigen main-commit-guard.sh en pre-commit-tests.sh op exit 2 voor
    élk Bash-commando. Dus: syntaxis van 3.9, en `X | Y` in een annotatie
    alleen onder `from __future__ import annotations`.
    """
    source = lib.read_text(encoding="utf-8")
    tree = ast.parse(source, filename=str(lib), feature_version=(3, 9))
    postponed = any(
        isinstance(node, ast.ImportFrom)
        and node.module == "__future__"
        and any(alias.name == "annotations" for alias in node.names)
        for node in tree.body
    )
    if not postponed:
        assert _union_annotations(tree) == [], (
            f"{lib.name}: `X | Y` in een annotatie zonder "
            "`from __future__ import annotations` breekt de import op Python 3.9"
        )


def test_heredoc_body_with_commit_text_is_no_invocation(tmp_path):
    """Tekst in een heredoc is nooit een aanroep (#1453, tweede deel)."""
    command = (
        "gh issue create --body \"$(cat <<'EOF'\nstap 1: git commit -m x\nEOF\n)\""
    )
    assert gi.find_invocations(command, str(tmp_path)) == []


def test_lib_names_do_not_shadow_the_stdlib():
    """The guard runs `python3 <lib>/git_invocations.py`: the lib directory is then
    `sys.path[0]`, and a lib file named like a stdlib module would shadow it."""
    names = {p.stem for p in LIB.glob("*.py")} - {"__init__"}
    assert not names & set(sys.stdlib_module_names)
