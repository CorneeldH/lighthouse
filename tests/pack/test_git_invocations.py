"""`pack/core/lib/git_invocations.py` (#884): the shared commit parser.

Moved out of the `IFS= read -r -d '' PARSE <<'PYEOF'` heredoc in
main-commit-guard.sh. The existing coverage of that behaviour is in
`test_instructie_hooks.py::test_commit_guard_*` (black box via the hook CLI);
these tests exercise the module directly — the unit that from now on is also
reused by the pre-commit test gate (task 2+).
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
    # A bare `.git/` without HEAD (on the Mac: `~/.git/workflow-audit-tools/`,
    # a stray tool cache) is not a repo to git; its discovery walks past
    # it. If `repo_of` does count it, every directory under it becomes a root
    # that git answers with "not a git repository".
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
    # The judgement "broken" stays with git: a worktree gitfile pointing to a
    # vanished gitdir, or a `.git/HEAD` with garbage, IS a root.
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


def test_import_reads_no_stdin():  # the import above succeeded without stdin; pin it
    assert callable(gi.main)


# `dirs` is live: read it during the iteration, not after list().
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
    """Line numbers of `X | Y` in an annotation (runtime error before Python 3.10)."""
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
    """Pin: the hooks run under the host's `python3` — on macOS 3.9.6.

    There `list[str] | None` in an annotation is a TypeError at import,
    and then main-commit-guard.sh and commit-tests.sh end in exit 2 for
    EVERY Bash command. So: 3.9 syntax, and `X | Y` in an annotation
    only under `from __future__ import annotations`.
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
    """Text in a heredoc is never an invocation (#1453, second part)."""
    command = (
        "gh issue create --body \"$(cat <<'EOF'\nstap 1: git commit -m x\nEOF\n)\""
    )
    assert gi.find_invocations(command, str(tmp_path)) == []


def test_lib_names_do_not_shadow_the_stdlib():
    """The guard runs `python3 <lib>/git_invocations.py`: the lib directory is then
    `sys.path[0]`, and a lib file named like a stdlib module would shadow it."""
    names = {p.stem for p in LIB.glob("*.py")} - {"__init__"}
    assert not names & set(sys.stdlib_module_names)


def test_markers_are_english_tokens():
    assert (gi.UNRESOLVED, gi.NEW_DIR) == ("<unknown>", "<new>")


@pytest.mark.parametrize(
    "cmd,default_branch,to_main",
    [
        ("git push origin HEAD:trunk", "trunk", "yes"),
        ("git push origin +refs/heads/trunk", "trunk", "yes"),
        ("git push origin HEAD:main", "trunk", "no"),
        ("git push origin HEAD:main", "main", "yes"),
    ],
)
def test_push_destination_follows_default_branch(tmp_path, cmd, default_branch, to_main):
    x = tmp_path / "x"
    init_repo(x, branch="issue-1-x")
    found = gi.find_invocations(cmd, str(x), default_branch=default_branch)
    assert [t for _, _, t, _ in found] == [to_main]


def test_cli_takes_the_default_branch(tmp_path):
    x = tmp_path / "x"
    init_repo(x, branch="issue-1-x")
    out = subprocess.run(
        [sys.executable, str(LIB / "git_invocations.py"), "--default-branch", "trunk", str(x)],
        input="git push origin HEAD:trunk",
        capture_output=True,
        text=True,
        check=False,
    )
    assert out.returncode == 0 and out.stdout == f"push\t{x}\tyes\t{x}\n"
