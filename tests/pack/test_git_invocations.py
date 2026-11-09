"""`pack/core/lib/git_invocations.py` (#884): the shared commit parser.

Moved out of the `IFS= read -r -d '' PARSE <<'PYEOF'` heredoc in
main-commit-gate.sh. The existing coverage of that behaviour is in
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


def test_import_reads_no_stdin():  # the import above succeeded without stdin; pin it
    assert callable(gi.main)


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
    and then commit-test-gate.sh ends in exit 2 for
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


def test_lib_names_do_not_shadow_the_stdlib():
    """The guard runs `python3 <lib>/git_invocations.py`: the lib directory is then
    `sys.path[0]`, and a lib file named like a stdlib module would shadow it."""
    names = {p.stem for p in LIB.glob("*.py")} - {"__init__"}
    assert not names & set(sys.stdlib_module_names)


def test_markers_are_english_tokens():
    assert (gi.UNRESOLVED, gi.NEW_DIR) == ("<unknown>", "<new>")


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
