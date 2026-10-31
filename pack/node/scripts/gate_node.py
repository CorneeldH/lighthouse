#!/usr/bin/env python3
"""Node selector for the test gate: run npm scripts when the commit touches a package directory.

Usage: `gate_node.py [--dir DIR] [--also PATH]... [--drift SCRIPT=PATH]... -- SCRIPT...`, for
example `gate_node.py --dir web --drift gen=src/api -- lint typecheck test`. `DIR` is the
directory that holds `package.json`, relative to the root (default `.`, at most once). `SCRIPT`
is an npm script name, run as `npm run --silent SCRIPT` in `DIR`.

With `LIGHTHOUSE_GATE_FILES` (one changed path per line, relative to the root) the gate runs
only when a path lies under `DIR` (a directory boundary: `web/x` is under `web`, `website/x` is
not; `--dir .` takes every path) or equals an `--also` path. Without it (a commit to the default
branch) it always runs. The gate tests too little rather than too much, CI tests everything.

First every `--drift SCRIPT=PATH` runs `npm run --silent SCRIPT` and fails with exit 1 when that
changed the files under `DIR/PATH` (a new file counts). It compares with the worktree, not the
index. Then the scripts run in order. The first exit code other than 0 is the exit code. npm
passes the exit code of the script through, so a check that itself exits 3 is reported as not
tested. Every script name must be declared in `DIR/package.json`, or nothing runs.

Exit codes: those of `npm`, or 3 when nothing was tested — a usage error, a list that cannot be
read, `node` or `npm` not on `PATH`, a missing `DIR/node_modules`, an unreadable
`DIR/package.json` or a script name it lacks, or an `npm` that cannot start
— with a line `gate-node: COULD NOT CHECK — <reason>` on stderr. List 3 in
`[test_gate] unchecked_exit_codes`.
"""

from __future__ import annotations

import json
import os
import posixpath
import shutil
import subprocess
import sys
from pathlib import Path

USAGE = (
    "usage: gate_node.py [--dir DIR] [--also PATH]... [--drift SCRIPT=PATH]... -- SCRIPT..."
)
UNCHECKED = 3


class Unchecked(Exception):
    """Nothing was tested; the message is the reason."""


def touches(files: list[str], package_dir: str, also: list[str]) -> bool:
    """True when a changed path lies under `package_dir` or equals an `also` path."""
    for name in files:
        if not name:
            continue
        if name in also:
            return True
        if package_dir == "." or name.startswith(package_dir + "/"):
            return True
    return False


def snapshot(directory: Path) -> dict[str, bytes]:
    """Relative posix path -> content of every file under `directory`.

    A single file gives {".": content}; a missing path gives {}.
    """
    if directory.is_file():
        return {".": directory.read_bytes()}
    if not directory.is_dir():
        return {}
    return {
        path.relative_to(directory).as_posix(): path.read_bytes()
        for path in sorted(directory.rglob("*"))
        if path.is_file()
    }


def _clean(path: str) -> str:
    """Normalise a root-relative path; an empty, absolute or escaping one is a usage error."""
    if not path or path.startswith("/"):
        raise Unchecked(USAGE)
    cleaned = posixpath.normpath(path)
    if cleaned == ".." or cleaned.startswith("../"):
        raise Unchecked(USAGE)
    return cleaned


def _check_scripts(directory: str, names: list[str]) -> None:
    """Every name must be a script in `directory`/package.json."""
    manifest = Path(directory) / "package.json"
    try:
        scripts = json.loads(manifest.read_text(encoding="utf-8")).get("scripts", {})
    except (OSError, ValueError, AttributeError) as exc:
        raise Unchecked(f"cannot read {directory}/package.json: {exc}") from exc
    if not isinstance(scripts, dict):
        raise Unchecked(f"{directory}/package.json has no scripts object")
    for name in names:
        if name not in scripts:
            raise Unchecked(f"no script {name} in {directory}/package.json")


def _npm(directory: str, script: str) -> int:
    print(f"gate-node: {directory} — npm run {script}", flush=True)
    try:
        return subprocess.call(["npm", "run", "--silent", script], cwd=directory)
    except OSError as exc:
        raise Unchecked(f"npm cannot start: {exc.strerror or exc}") from exc


def _drift(directory: str, script: str, path: str) -> int:
    target = Path(directory) / path
    before = snapshot(target)
    status = _npm(directory, script)
    if status != 0:
        return status
    if snapshot(target) != before:
        shown = path if directory == "." else f"{directory}/{path}"
        print(
            f"gate-node: {shown} was stale; npm run {script} regenerated it — commit the result",
            flush=True,
        )
        return 1
    return 0


def _parse(args: list[str]) -> tuple[str, list[str], list[tuple[str, str]], list[str]]:
    if "--" not in args:
        raise Unchecked(USAGE)
    split = args.index("--")
    options, scripts = args[:split], args[split + 1 :]
    directory: str | None = None
    also: list[str] = []
    drifts: list[tuple[str, str]] = []
    while options:
        flag = options[0]
        if len(options) < 2:
            raise Unchecked(USAGE)
        value = options[1]
        if flag == "--dir" and directory is None:
            directory = _clean(value)
        elif flag == "--also":
            also.append(_clean(value))
        elif flag == "--drift" and "=" in value:
            script, _, path = value.partition("=")
            if not script or not path:
                raise Unchecked(USAGE)
            drifts.append((script, _clean(path)))
        else:
            raise Unchecked(USAGE)
        options = options[2:]
    if not scripts:
        raise Unchecked(USAGE)
    return directory or ".", also, drifts, scripts


def _select_and_run(argv: list[str]) -> int:
    directory, also, drifts, scripts = _parse(argv[1:])
    files_path = os.environ.get("LIGHTHOUSE_GATE_FILES")
    if files_path:
        try:
            changed = Path(files_path).read_text(
                encoding="utf-8", errors="surrogateescape"
            ).split("\n")
        except OSError as exc:
            raise Unchecked(f"cannot read the changed files: {exc}") from exc
        if not touches(changed, directory, also):
            where = "" if directory == "." else f" under {directory}"
            print(f"gate-node: skipped — the commit touches nothing{where}", flush=True)
            return 0
    for tool in ("node", "npm"):
        if shutil.which(tool) is None:
            raise Unchecked(f"{tool} not on PATH")
    if not (Path(directory) / "node_modules").is_dir():
        raise Unchecked(f"{directory}/node_modules missing; fix: cd {directory} && npm ci")
    _check_scripts(directory, [script for script, _ in drifts] + scripts)
    for script, path in drifts:
        status = _drift(directory, script, path)
        if status != 0:
            return status
    for script in scripts:
        status = _npm(directory, script)
        if status != 0:
            return status
    names = [script for script, _ in drifts] + scripts
    print(f"gate-node: passed — {', '.join(names)}", flush=True)
    return 0


def main(argv: list[str]) -> int:
    try:
        return _select_and_run(argv)
    except Unchecked as exc:
        sys.stderr.write(f"gate-node: COULD NOT CHECK — {exc}\n")
        return UNCHECKED


if __name__ == "__main__":
    sys.exit(main(sys.argv))
