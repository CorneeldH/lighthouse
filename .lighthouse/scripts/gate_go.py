#!/usr/bin/env python3
# lighthouse: go@dev
"""Go selector for the test gate: run a command on the packages the commit touches.

Usage: `gate_go.py -- CMD...`, for example `gate_go.py -- go test`.

Without `LIGHTHOUSE_GATE_FILES` (a commit to the default branch) it runs `CMD ./...`.
With it, it maps the changed paths (one per line, relative to the root) to Go packages
and runs `CMD <import paths>`. Importers of a touched package are not selected: the
gate tests too little rather than too much, CI tests everything.

Exit codes: those of `CMD`, or 3 when nothing was tested — a usage error, a list that
cannot be read, no `go`, a `go list` that fails, or a `CMD` that cannot start — with a
line `gate-go: COULD NOT CHECK — <reason>` on stderr. List 3 in
`[test_gate] unchecked_exit_codes`; 2 cannot go there, pytest uses it.
"""

from __future__ import annotations

import json
import os
import subprocess
import sys
from pathlib import Path

FILE_KEYS = (
    "GoFiles",
    "CgoFiles",
    "TestGoFiles",
    "XTestGoFiles",
    "IgnoredGoFiles",
    "EmbedFiles",
    "TestEmbedFiles",
    "XTestEmbedFiles",
)


def packages_for(files: list[str], listing: list[dict], root: Path) -> list[str]:
    """Import paths of the packages the changed `files` belong to, sorted and unique."""
    root = root.resolve()
    selected: set[str] = set()
    for name in files:
        if not name or name in ("go.mod", "go.sum"):
            continue
        path = root / name
        for pkg in listing:
            if _belongs(path, pkg):
                selected.add(pkg["ImportPath"])
    return sorted(selected)


def _belongs(path: Path, pkg: dict) -> bool:
    directory = Path(pkg["Dir"]).resolve()
    try:
        rel = path.relative_to(directory)
    except ValueError:
        return False
    rel_posix = rel.as_posix()
    if rel.parts[0] == "testdata":
        return True
    if any(rel_posix in pkg.get(key, []) for key in FILE_KEYS):
        return True
    # A deleted .go file is in no listing; it counts for the package of its directory.
    return path.suffix == ".go" and len(rel.parts) == 1


UNCHECKED = 3


class Unchecked(Exception):
    """Nothing was tested; the message is the reason."""


def go_list(root: Path) -> list[dict]:
    """`go list -json ./...` decoded; raises Unchecked when go is missing or go list fails."""
    try:
        done = subprocess.run(
            ["go", "list", "-json", "./..."],
            cwd=root,
            capture_output=True,
            text=True,
            check=False,
        )
    except FileNotFoundError as exc:
        raise Unchecked("go not on PATH") from exc
    if done.returncode != 0:
        sys.stderr.write(done.stderr)
        raise Unchecked(f"go list failed (exit {done.returncode}), see above")
    decoder = json.JSONDecoder()
    text, pos, out = done.stdout, 0, []
    try:
        while True:
            while pos < len(text) and text[pos].isspace():
                pos += 1
            if pos >= len(text):
                return out
            obj, pos = decoder.raw_decode(text, pos)
            out.append(obj)
    except ValueError as exc:
        raise Unchecked(f"go list gave output that is not JSON: {exc}") from exc


def _run(cmd: list[str]) -> int:
    try:
        return subprocess.call(cmd)
    except OSError as exc:
        raise Unchecked(f"{cmd[0]} cannot start: {exc.strerror or exc}") from exc


def _select_and_run(argv: list[str]) -> int:
    if "--" not in argv[1:]:
        raise Unchecked("usage: gate_go.py -- CMD...")
    cmd = argv[argv.index("--") + 1 :]
    if not cmd:
        raise Unchecked("usage: gate_go.py -- CMD...")
    files_path = os.environ.get("LIGHTHOUSE_GATE_FILES")
    if not files_path:
        return _run([*cmd, "./..."])
    root = Path.cwd()
    try:
        changed = Path(files_path).read_text(encoding="utf-8").split("\n")
    except OSError as exc:
        raise Unchecked(f"cannot read the changed files: {exc}") from exc
    pkgs = packages_for(changed, go_list(root), root)
    if not pkgs:
        print("gate-go: skipped — the commit touches no Go package", flush=True)
        return 0
    print(f"gate-go: {' '.join(pkgs)}", flush=True)
    return _run([*cmd, *pkgs])


def main(argv: list[str]) -> int:
    try:
        return _select_and_run(argv)
    except Unchecked as exc:
        sys.stderr.write(f"gate-go: COULD NOT CHECK — {exc}\n")
        return UNCHECKED


if __name__ == "__main__":
    sys.exit(main(sys.argv))
