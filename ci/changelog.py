#!/usr/bin/env python3
"""Changelog fragments: parse `changelog.d/*.md` and check that a PR adds one.

A fragment is a markdown file with a one-line front matter (`type: <t>`) and the text of the
entry. The release workflow merges the fragments into CHANGELOG.md; see docs/releasing.md.

- CI runs `python3 ci/changelog.py check --base "$BASE" --head "$HEAD"` on every PR. It needs
  at least one fragment added in `base...head` and refuses any fragment that does not parse.
  Exit 0 is ok, 1 is a verdict (no new fragment, or a broken one), 2 is COULD NOT CHECK (git
  failed).
- The release workflow runs `python3 ci/changelog.py release --event "$EVENT" --bump "$BUMP"`:
  it decides the bump from the open fragments, writes the section into CHANGELOG.md, removes
  the fragments and prints the new tag (nothing when there is nothing to release). Exit 0 is
  ok, 1 is a verdict (ReleaseError, FragmentError), 2 is COULD NOT CHECK (no release tag, or
  git failed).
- `python3 ci/changelog.py notes <tag>` prints the CHANGELOG.md section of a tag; exit 1 when
  there is none.

Stdlib only, Python 3.11.
"""

import argparse
import datetime
import re
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


class ReleaseError(Exception):
    """A release that must not happen (exit 1)."""


class TagError(Exception):
    """No usable release tag (exit 2)."""


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
    except (OSError, UnicodeDecodeError, subprocess.CalledProcessError) as err:
        detail = err.stderr.decode("utf-8", "replace").strip() if isinstance(
            err, subprocess.CalledProcessError) else str(err)
        raise RuntimeError(f"git diff {base}...{head} failed: {detail}") from err
    return [p for p in out.split("\0") if p and Path(p).name != MARKER]


BUMPS = ("patch", "minor", "major")
TAG_RE = re.compile(r"^v(\d+)\.(\d+)\.(\d+)$")
GROUPS = tuple((t, t.capitalize()) for t in ("added", "changed", "fixed", "removed"))


def decide_bump(fragments: list[Fragment], event: str, forced: str = "auto") -> str | None:
    """The bump for the open fragments, or None when there is nothing to release.

    `push` releases only when a `fixed` is open (patch, or minor next to another type);
    `schedule` and `workflow_dispatch` release anything releasable. A forced bump below
    what `schedule` would pick, or without a releasable fragment, is refused."""
    types = {f.type for f in fragments if f.type in RELEASABLE}
    if not types:
        if forced != "auto":
            raise ReleaseError("nothing to release: no releasable fragment for the forced bump")
        return None
    natural = "patch" if types == {"fixed"} else "minor"
    if forced != "auto":
        if BUMPS.index(forced) < BUMPS.index(natural):
            raise ReleaseError(f"forced bump {forced} is lower than the {natural} the fragments need")
        return forced
    if event == "push" and "fixed" not in types:
        return None
    return natural


def latest_tag(tags: list[str]) -> str:
    """The highest `vX.Y.Z` tag by semver. Prereleases and other tags are ignored."""
    found = [(tuple(int(n) for n in m.groups()), tag) for tag in tags if (m := TAG_RE.match(tag))]
    if not found:
        raise TagError("no vX.Y.Z release tag found (a checkout without tags?)")
    return max(found)[1]


def next_version(tag: str, bump: str) -> str:
    match = TAG_RE.match(tag)
    if not match or bump not in BUMPS:
        raise ValueError(f"cannot bump {tag!r} by {bump!r}")
    major, minor, patch = (int(n) for n in match.groups())
    if bump == "major":
        return f"v{major + 1}.0.0"
    if bump == "minor":
        return f"v{major}.{minor + 1}.0"
    return f"v{major}.{minor}.{patch + 1}"


def render_section(version: str, date: str, fragments: list[Fragment]) -> str:
    """A CHANGELOG.md section: groups in a fixed order, `internal` never."""
    blocks = [f"## [{version}] - {date}"]
    for typ, title in GROUPS:
        items = ["- " + f.text.replace("\n", "\n  ") for f in fragments if f.type == typ]
        if items:
            blocks.append(f"### {title}\n\n" + "\n".join(items))
    return "\n\n".join(blocks) + "\n"


def insert_section(changelog: str, section: str) -> str:
    """Put `section` above the first `## ` line, or at the end when there is none."""
    lines = changelog.splitlines(keepends=True)
    for i, line in enumerate(lines):
        if line.startswith("## "):
            return "".join(lines[:i]) + section + "\n" + "".join(lines[i:])
    if changelog and not changelog.endswith("\n"):
        changelog += "\n"
    return changelog + ("\n" if changelog else "") + section


def extract_section(changelog: str, tag: str) -> str | None:
    """The body under `## [<tag>] - ...` up to the next `## ` line, stripped; None if absent."""
    prefix = f"## [{tag}] - "
    body: list[str] | None = None
    for line in changelog.splitlines():
        if body is None:
            if line.startswith(prefix):
                body = []
        elif line.startswith("## "):
            break
        else:
            body.append(line)
    return None if body is None else "\n".join(body).strip()


def release(root: Path, event: str, forced: str, date: str, tags: list[str]) -> str | None:
    """Write the section for the open fragments into CHANGELOG.md and remove them.

    Returns the new tag, or None when nothing is released (the tree is left alone). The order
    is fixed: tag, fragments and bump, then CHANGELOG.md, so a repo without tags always fails
    with TagError."""
    current = latest_tag(tags)
    fragments = read_fragments(root / FRAGMENT_DIR)
    bump = decide_bump(fragments, event, forced)
    if bump is None:
        return None
    path = root / "CHANGELOG.md"
    try:
        text = path.read_text(encoding="utf-8")
    except OSError as err:
        raise ReleaseError(f"{path}: cannot read: {err}") from err
    version = next_version(current, bump)
    path.write_text(insert_section(text, render_section(version, date, fragments)), encoding="utf-8")
    for fragment in fragments:
        fragment.path.unlink()
    return version


def _release(args: argparse.Namespace) -> int:
    root = Path(args.root)
    try:
        out = subprocess.run(
            ["git", "-C", str(root), "tag", "--list", "v*"],
            check=True, capture_output=True, text=True,
        ).stdout
    except (OSError, subprocess.CalledProcessError) as err:
        detail = err.stderr.strip() if isinstance(err, subprocess.CalledProcessError) else str(err)
        print(f"COULD NOT CHECK: git tag failed: {detail}", file=sys.stderr)
        return 2
    try:
        tag = release(root, args.event, args.bump, args.date, out.split())
    except TagError as err:
        print(f"COULD NOT CHECK: {err}", file=sys.stderr)
        return 2
    except (ReleaseError, FragmentError) as err:
        print(f"{err}", file=sys.stderr)
        return 1
    print(tag or "")
    return 0


def _notes(args: argparse.Namespace) -> int:
    path = Path(args.root) / "CHANGELOG.md"
    try:
        body = extract_section(path.read_text(encoding="utf-8"), args.tag)
    except OSError as err:
        print(f"{path}: cannot read: {err}", file=sys.stderr)
        return 1
    if body is None:
        print(f"no section for {args.tag} in {path}", file=sys.stderr)
        return 1
    print(body)
    return 0


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
    rel = sub.add_parser("release", help="write CHANGELOG.md from the fragments, print the new tag")
    rel.add_argument("--event", required=True, choices=("push", "schedule", "workflow_dispatch"))
    rel.add_argument("--bump", default="auto", choices=("auto", *BUMPS))
    rel.add_argument("--date", default=datetime.date.today().isoformat())
    rel.add_argument("--root", default=".")
    rel.set_defaults(func=_release)
    notes = sub.add_parser("notes", help="print the CHANGELOG.md section of a tag")
    notes.add_argument("tag")
    notes.add_argument("--root", default=".")
    notes.set_defaults(func=_notes)
    args = parser.parse_args(argv)
    return args.func(args)


if __name__ == "__main__":
    sys.exit(main())
