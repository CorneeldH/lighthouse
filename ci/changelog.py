#!/usr/bin/env python3
"""Changelog fragments: parse `changelog.d/*.md` and check that a PR adds one.

A fragment is a markdown file with a one-line front matter (`type: <t>`) and the text of the
entry. The release workflow merges the fragments into CHANGELOG.md; see docs/releasing.md.

- CI runs `python3 ci/changelog.py check --base "$BASE" --head "$HEAD"` on every PR. It needs
  at least one fragment added in `base...head` and refuses any fragment that does not parse.
  Exit 0 is ok, 1 is a verdict (no new fragment, or a broken one), 2 is COULD NOT CHECK (git
  failed).

Stdlib only, Python 3.11.
"""

import argparse
import subprocess
import sys
from dataclasses import dataclass
from pathlib import Path

TYPES = ("added", "changed", "fixed", "removed", "internal")
RELEASABLE = TYPES[:4]
MARKER = ".lighthouse"
FRAGMENT_DIR = "changelog.d"


class FragmentError(Exception):
    """A fragment that cannot be used. The message starts with its path."""


@dataclass(frozen=True)
class Fragment:
    path: Path
    type: str
    text: str


def parse_fragment(path: Path) -> Fragment:
    """Parse one fragment: utf-8 (BOM allowed), CRLF allowed, front matter with one `type:`."""

    def refuse(reason: str) -> FragmentError:
        return FragmentError(f"{path}: {reason}")

    try:
        raw = path.read_bytes().decode("utf-8-sig")
    except (OSError, UnicodeDecodeError) as err:
        raise refuse(f"cannot read: {err}") from err
    lines = raw.replace("\r\n", "\n").split("\n")
    if lines[0] != "---":
        raise refuse("first line must be exactly `---`")
    try:
        close = lines.index("---", 1)
    except ValueError:
        raise refuse("front matter is not closed by a `---` line") from None
    entries = [line for line in lines[1:close] if line.strip()]
    if len(entries) != 1:
        raise refuse(f"front matter must hold exactly one line `type: <type>`, found {len(entries)}")
    key, sep, value = entries[0].partition(":")
    if not sep or key.strip() != "type":
        raise refuse(f"unknown front matter line {entries[0]!r}, only `type:` is allowed")
    typ = value.strip()
    if typ not in TYPES:
        raise refuse(f"unknown type {typ!r}, expected one of {', '.join(TYPES)}")
    text = "\n".join(lines[close + 1 :]).strip()
    if not text:
        raise refuse("empty text")
    return Fragment(path, typ, text)


def read_fragments(directory: Path) -> list[Fragment]:
    """All fragments in `directory`, sorted by file name. The marker is skipped, any other
    item that is not a `*.md` file is refused. A missing directory holds no fragments."""
    if not directory.is_dir():
        return []
    fragments = []
    for item in sorted(directory.iterdir(), key=lambda p: p.name):
        if item.name == MARKER:
            continue
        if not (item.is_file() and item.suffix == ".md"):
            raise FragmentError(f"{item}: not a `*.md` fragment")
        fragments.append(parse_fragment(item))
    return fragments


def added_fragments(root: Path, base: str, head: str) -> list[str]:
    """Paths of fragments added in `base...head`. A rename counts as an add (`--no-renames`)."""
    try:
        out = subprocess.run(
            ["git", "-C", str(root), "diff", "--name-only", "--diff-filter=A", "--no-renames",
             "-z", f"{base}...{head}", "--", f"{FRAGMENT_DIR}/"],
            check=True, capture_output=True,
        ).stdout.decode("utf-8")
    except (OSError, subprocess.CalledProcessError) as err:
        detail = err.stderr.decode("utf-8", "replace").strip() if isinstance(
            err, subprocess.CalledProcessError) else str(err)
        raise RuntimeError(f"git diff {base}...{head} failed: {detail}") from err
    return [p for p in out.split("\0") if p and Path(p).name != MARKER]


def _check(args: argparse.Namespace) -> int:
    root = Path(args.root)
    try:
        added = added_fragments(root, args.base, args.head)
    except RuntimeError as err:
        print(f"COULD NOT CHECK: {err}", file=sys.stderr)
        return 2
    try:
        read_fragments(root / FRAGMENT_DIR)
    except FragmentError as err:
        print(f"{err}", file=sys.stderr)
        return 1
    if not added:
        print(
            f"no new fragment in {FRAGMENT_DIR}/: add one (see docs/releasing.md), "
            "type `internal` if the change needs no changelog entry",
            file=sys.stderr,
        )
        return 1
    return 0


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    sub = parser.add_subparsers(dest="command", required=True)
    check = sub.add_parser("check", help="a PR adds a valid fragment")
    check.add_argument("--base", required=True)
    check.add_argument("--head", required=True)
    check.add_argument("--root", default=".")
    check.set_defaults(func=_check)
    args = parser.parse_args(argv)
    return args.func(args)


if __name__ == "__main__":
    sys.exit(main())
