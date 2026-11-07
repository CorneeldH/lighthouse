#!/usr/bin/env python3
"""Create the labels of the taxonomy file on the remote, or print the commands that would.

The taxonomy file is `[paths] labels` (default `docs/developer/labels.md`). The script reads
every Markdown table with the columns `Label` and `Color` (case does not matter). A label is
between backticks, a color is six hex characters, also between backticks. Lines that do not
start with `|` are skipped, so an example row in an HTML comment does not count.

Norms:
- Never asks a question on stdin; `gh` runs with `stdin` closed and `GH_PROMPT_DISABLED=1`.
- Never `--force`: a label that exists on the remote stays as it is (name compared without
  regard to case, no new color).
- Exit 0 on every `gh` outcome (no `gh`, no rights, no remote, a failed create): what could
  not be done is printed as a `gh label create` line. Exit 2 on a missing or empty taxonomy
  file or a broken config.

USAGE
    create_labels.py            print the `gh label create` lines of the missing labels
    create_labels.py --create   create the missing labels, print the lines that failed
"""

from __future__ import annotations

import argparse
import os
import re
import shlex
import subprocess
import sys
from pathlib import Path

# Run as a file on its path: put `.lighthouse/lib` first, so `lighthouse_lib` imports
# from this installation.
if not __package__:
    sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "lib"))

from lighthouse_lib.config import ConfigError, config_path, load, lookup

DEFAULT_PATH = "docs/developer/labels.md"
COLOR = re.compile(r"`([0-9A-Fa-f]{6})`")
NAME = re.compile(r"`([^`]+)`")


def _cells(line: str) -> list[str]:
    return [c.strip() for c in line.strip().strip("|").split("|")]


def parse_taxonomy(text: str) -> list[tuple[str, str]]:
    """The (name, COLOR) pairs of every table with the columns `Label` and `Color`."""
    found: list[tuple[str, str]] = []
    cols: tuple[int, int] | None = None
    for line in text.splitlines():
        if not line.startswith("|"):
            cols = None
            continue
        cells = _cells(line)
        lowered = [c.casefold() for c in cells]
        if cols is None:
            if "label" in lowered and "color" in lowered:
                cols = (lowered.index("label"), lowered.index("color"))
            continue
        if set("".join(cells)) <= set("-: "):
            continue
        if max(cols) >= len(cells):
            continue
        name, color = NAME.fullmatch(cells[cols[0]]), COLOR.fullmatch(cells[cols[1]])
        if name and color:
            found.append((name.group(1), color.group(1).upper()))
    return found


def read_remote(repo: str) -> tuple[list[str] | None, str]:
    """The label names on the remote, or (None, reason) when they cannot be read."""
    cmd = ["gh", "api", f"repos/{repo}/labels", "--paginate", "--jq", ".[].name"]
    result = _gh(cmd)
    if isinstance(result, str):
        return None, result
    return result.stdout.split(), ""


def _gh(cmd: list[str]) -> subprocess.CompletedProcess[str] | str:
    """Run `gh`; the reason as a string when it did not run or exited non-zero."""
    try:
        result = subprocess.run(
            cmd,
            stdin=subprocess.DEVNULL,
            capture_output=True,
            text=True,
            check=False,
            env=dict(os.environ, GH_PROMPT_DISABLED="1"),
        )
    except OSError as exc:
        return f"gh did not run: {exc}"
    if result.returncode != 0:
        return result.stderr.strip().splitlines()[-1] if result.stderr.strip() else f"gh exited {result.returncode}"
    return result


def create_command(name: str, color: str, repo: str | None) -> list[str]:
    cmd = ["gh", "label", "create", name]
    if repo:
        cmd += ["--repo", repo]
    return cmd + ["--color", color]


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    parser.add_argument("--create", action="store_true", help="create the missing labels")
    args = parser.parse_args(argv)

    try:
        path = config_path()
        cfg = load(path)
        try:
            rel = str(lookup(cfg, "paths.labels"))
        except KeyError:
            rel = DEFAULT_PATH
    except ConfigError as exc:
        print(f"labels: ERROR — {exc}", file=sys.stderr)
        return 2
    taxonomy = path.parent / rel
    try:
        labels = parse_taxonomy(taxonomy.read_text(encoding="utf-8"))
    except OSError as exc:
        print(f"labels: ERROR — cannot read {taxonomy}: {exc}", file=sys.stderr)
        return 2
    if not labels:
        print(f"labels: ERROR — no labels found in {taxonomy}", file=sys.stderr)
        return 2

    repo = cfg.repo  # forge.repo
    remote: list[str] | None = None
    reason = "no forge.repo configured"
    if repo:
        remote, reason = read_remote(repo)
    if remote is None:
        missing = labels
    else:
        have = {n.casefold() for n in remote}
        missing = [(n, c) for n, c in labels if n.casefold() not in have]
    if not missing:
        print(f"labels: all {len(labels)} present on {repo}")
        return 0

    lines: list[list[str]] = []
    for name, color in missing:
        cmd = create_command(name, color, repo)
        if args.create and remote is not None:
            outcome = _gh(cmd)
            if not isinstance(outcome, str):
                print(f"labels: created {name}")
                continue
        lines.append(cmd)
    if not lines:
        return 0
    if remote is None:
        print(f"labels: could not read the labels of {repo or 'the repository'} ({reason}); "
              "create the missing ones with:")
    else:
        print(f"labels: missing on {repo}; create them with:")
    for cmd in lines:
        print(shlex.join(cmd))
    return 0


if __name__ == "__main__":
    sys.exit(main())
