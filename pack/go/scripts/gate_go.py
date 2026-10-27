#!/usr/bin/env python3
"""Go selector for the test gate: run a command on the packages the commit touches.

Usage: `gate_go.py -- CMD...`, for example `gate_go.py -- go test`.

Without `LIGHTHOUSE_GATE_FILES` (a commit to the default branch) it runs `CMD ./...`.
With it, it maps the changed paths (one per line, relative to the root) to Go packages
and runs `CMD <import paths>`. Importers of a touched package are not selected: the
gate tests too little rather than too much, CI tests everything.
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


def go_list(root: Path) -> list[dict]:
    """`go list -json ./...` decoded; raises CalledProcessError when go list fails."""
    done = subprocess.run(
        ["go", "list", "-json", "./..."],
        cwd=root,
        capture_output=True,
        text=True,
        check=False,
    )
    if done.returncode != 0:
        sys.stderr.write(done.stderr)
        raise subprocess.CalledProcessError(done.returncode, done.args)
    decoder = json.JSONDecoder()
    text, pos, out = done.stdout, 0, []
    while True:
        while pos < len(text) and text[pos].isspace():
            pos += 1
        if pos >= len(text):
            return out
        obj, pos = decoder.raw_decode(text, pos)
        out.append(obj)


def main(argv: list[str]) -> int:
    if "--" not in argv[1:] or len(argv) < 3:
        sys.stderr.write("usage: gate_go.py -- CMD...\n")
        return 2
    cmd = argv[argv.index("--") + 1 :]
    if not cmd:
        sys.stderr.write("usage: gate_go.py -- CMD...\n")
        return 2
    files_path = os.environ.get("LIGHTHOUSE_GATE_FILES")
    if not files_path:
        return subprocess.call([*cmd, "./..."])
    root = Path.cwd()
    try:
        changed = Path(files_path).read_text(encoding="utf-8").split("\n")
        listing = go_list(root)
    except subprocess.CalledProcessError as exc:
        return exc.returncode
    except OSError as exc:
        sys.stderr.write(f"gate-go: could not read the changed files: {exc}\n")
        return 2
    pkgs = packages_for(changed, listing, root)
    if not pkgs:
        print("gate-go: skipped — the commit touches no Go package", flush=True)
        return 0
    print(f"gate-go: {' '.join(pkgs)}", flush=True)
    return subprocess.call([*cmd, *pkgs])


if __name__ == "__main__":
    sys.exit(main(sys.argv))
