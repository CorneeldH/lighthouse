#!/usr/bin/env python3
"""Python selector for the test gate: run a command on the tests that belong to the commit.

Usage: `gate_python.py [--tests DIR]... -- CMD...`, for example
`gate_python.py -- uv run pytest -q`. `--tests` defaults to `tests`.

Without `LIGHTHOUSE_GATE_FILES` (a commit to the default branch) it runs `CMD` bare: the
whole domain. With it, it maps the changed paths (one per line, relative to the root) to
test files and runs `CMD <test files>`. A changed `test_*.py` under a test directory
selects itself; any other path `.../X.py`, `.../X.sh` or `.../X` selects every existing
`test_<X, - as _>.py` under the test directories. The gate tests too little rather than
too much, CI tests everything.
"""

from __future__ import annotations

import os
import subprocess
import sys
from pathlib import Path

USAGE = "usage: gate_python.py [--tests DIR]... -- CMD...\n"


def select_tests(files: list[str], root: Path, test_dirs: list[str]) -> list[str]:
    """Test files (relative to `root`, sorted, unique) that belong to the changed `files`."""
    root = root.resolve()
    existing: list[Path] = []
    for directory in test_dirs:
        base = (root / directory).resolve()
        if base.is_dir():
            existing.extend(p for p in base.rglob("test_*.py") if p.is_file())
    selected: set[str] = set()
    for name in files:
        if not name:
            continue
        path = (root / name).resolve()
        if path.name.startswith("test_") and path.suffix == ".py":
            if path in existing:
                selected.add(path.relative_to(root).as_posix())
            continue
        stem = path.name
        if path.suffix in (".py", ".sh"):
            stem = path.stem
        wanted = f"test_{stem.replace('-', '_')}.py"
        selected.update(p.relative_to(root).as_posix() for p in existing if p.name == wanted)
    return sorted(selected)


def main(argv: list[str]) -> int:
    args = argv[1:]
    if "--" not in args:
        sys.stderr.write(USAGE)
        return 2
    split = args.index("--")
    options, cmd = args[:split], args[split + 1 :]
    test_dirs: list[str] = []
    while options:
        if options[0] != "--tests" or len(options) < 2:
            sys.stderr.write(USAGE)
            return 2
        test_dirs.append(options[1])
        options = options[2:]
    if not cmd:
        sys.stderr.write(USAGE)
        return 2
    files_path = os.environ.get("LIGHTHOUSE_GATE_FILES")
    if not files_path:
        return subprocess.call(cmd)
    try:
        changed = Path(files_path).read_text(encoding="utf-8").split("\n")
    except OSError as exc:
        sys.stderr.write(f"gate-python: could not read the changed files: {exc}\n")
        return 2
    tests = select_tests(changed, Path.cwd(), test_dirs or ["tests"])
    if not tests:
        print("gate-python: skipped — no test belongs to the commit", flush=True)
        return 0
    print(f"gate-python: {len(tests)} test files", flush=True)
    return subprocess.call([*cmd, *tests])


if __name__ == "__main__":
    sys.exit(main(sys.argv))
