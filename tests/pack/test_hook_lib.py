"""The Python lib of pack/core (`pack/core/lib`, installed as `.lighthouse/lib/lighthouse_lib`).

The hooks and scripts that still import it run under the host's `python3`, so it must import on
Python 3.9 and must not shadow the standard library. Moved here when the commit parser of the
lib became Go (`internal/gitinvocations`).
"""

import ast
import sys

import pytest

from tests.pack._testkit import LIB


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
    and then a hook that imports the lib ends in exit 2 for
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
    """A hook that runs `python3 <lib>/<module>.py` makes the lib directory
    `sys.path[0]`, and a lib file named like a stdlib module would shadow it."""
    names = {p.stem for p in LIB.glob("*.py")} - {"__init__"}
    assert not names & set(sys.stdlib_module_names)
