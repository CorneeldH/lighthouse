"""In which checkouts of this project did the turn write? (#54)

The Stop hooks (`ty-check.sh`, `go-check.sh`) check a checkout. The `cwd` of the
Stop payload is the shell's directory at that moment, not where the turn changed
files. Measured in #54 (Claude Code 2.1.310, a headless session started in the
main checkout, a Stop hook that logs its payload):

- Write into the worktree by absolute path, then `ls <worktree>`: `cwd` is the
  main checkout;
- `cd <worktree> && pwd`: `cwd` is the worktree.

The `cwd` follows the Bash tool's `cd`, and Claude Code resets it to the start
directory after a `cd` outside the project. This lib reads the turn from
`transcript_path` instead.

The turn starts at the last prompt: a `user` entry that is not `isMeta` and
carries text, not a `tool_result`. From there on, plus the entries since that
moment in the subagent transcripts under `<transcript without .jsonl>/subagents/`,
it collects:

- the `file_path` or `notebook_path` of Edit, Write, MultiEdit and NotebookEdit;
- per Bash call, `bash_writes.derive_roots` with the `cwd` of that entry.

Each path becomes the root of its checkout; `bash_writes.project_filter` keeps the
checkouts of this project (the one of `CLAUDE_PROJECT_DIR`, else of the payload
`cwd`). A turn
that names none gives the root of the payload `cwd`, as before #54.

    python3 turn_roots.py < payload

Exit 0: the roots, sorted, one per line. Exit 1: the checkout cannot be
determined (no or unreadable transcript, no prompt in it, a root git cannot
read), the reason on stdout. Only stdlib: the hooks run under the host `python3`.
"""

from __future__ import annotations

import json
import os
import sys
import time
from collections.abc import Iterator
from datetime import datetime
from pathlib import Path

# Run as a file on its path: put `.lighthouse/lib` first, so `lighthouse_lib`
# imports from this installation.
if not __package__:
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from lighthouse_lib import bash_writes, git_invocations

# The tools that write a file, and the input key that names it.
PATH_KEYS = {
    "Edit": "file_path",
    "Write": "file_path",
    "MultiEdit": "file_path",
    "NotebookEdit": "notebook_path",
}

DEADLINE_S = 20.0


class Undetermined(Exception):
    """The checkout of the turn cannot be determined; the message says why."""


def _entries(path: Path) -> Iterator[dict]:
    """The JSON objects of a transcript; a line that is no object is skipped."""
    with path.open(encoding="utf-8", errors="replace") as handle:
        for line in handle:
            try:
                entry = json.loads(line)
            except ValueError:
                continue
            if isinstance(entry, dict):
                yield entry


def _is_prompt(entry: dict) -> bool:
    if entry.get("type") != "user" or entry.get("isMeta"):
        return False
    content = (entry.get("message") or {}).get("content")
    if isinstance(content, str):
        return True
    if not isinstance(content, list):
        return False
    kinds = {block.get("type") for block in content if isinstance(block, dict)}
    return "text" in kinds and "tool_result" not in kinds


def _moment(entry: dict) -> datetime | None:
    stamp = entry.get("timestamp")
    if not isinstance(stamp, str):
        return None
    try:
        # Before Python 3.11 `fromisoformat` does not read a trailing `Z`.
        return datetime.fromisoformat(stamp.replace("Z", "+00:00"))
    except ValueError:
        return None


def _tool_uses(entries: list[dict]) -> Iterator[tuple[str, dict, str | None]]:
    """`(name, input, cwd)` of every tool call in `entries`."""
    for entry in entries:
        if entry.get("type") != "assistant":
            continue
        content = (entry.get("message") or {}).get("content")
        if not isinstance(content, list):
            continue
        for block in content:
            if isinstance(block, dict) and block.get("type") == "tool_use":
                tool_input = block.get("input")
                if isinstance(tool_input, dict):
                    yield str(block.get("name")), tool_input, entry.get("cwd")


def turn_entries(transcript: Path) -> list[dict]:
    """The entries of the current turn: the main transcript from the last prompt,
    plus the subagent entries from that moment on."""
    try:
        turn: list[dict] | None = None
        prompt: dict | None = None
        for entry in _entries(transcript):
            if _is_prompt(entry):
                turn, prompt = [], entry
            elif turn is not None:
                turn.append(entry)
    except OSError as exc:
        raise Undetermined(f"the transcript {transcript} cannot be read ({exc.strerror}).") from exc
    if turn is None or prompt is None:
        raise Undetermined(f"no prompt in the transcript {transcript}.")

    start = _moment(prompt)
    subagents = transcript.with_suffix("") / "subagents"
    if start is not None and subagents.is_dir():
        for path in sorted(subagents.glob("*.jsonl")):
            try:
                for entry in _entries(path):
                    moment = _moment(entry)
                    if moment is not None and moment >= start:
                        turn.append(entry)
            except OSError as exc:
                raise Undetermined(f"the subagent transcript {path} cannot be read ({exc.strerror}).") from exc
    return turn


def _root_of(path: str) -> str | None:
    ancestor = git_invocations.existing_ancestor(path)
    if ancestor is None:
        return None
    root = git_invocations.repo_of(ancestor)
    return os.path.realpath(root) if root is not None else None


def touched_roots(entries: list[dict]) -> set[str]:
    """The checkout roots the tool calls in `entries` name, not yet per project."""
    roots: set[str] = set()
    for name, tool_input, cwd in _tool_uses(entries):
        key = PATH_KEYS.get(name)
        if key is not None:
            path = tool_input.get(key)
            if isinstance(path, str) and os.path.isabs(path):
                root = _root_of(path)
                if root is not None:
                    roots.add(root)
        elif name == "Bash":
            command = tool_input.get("command")
            if isinstance(command, str) and isinstance(cwd, str):
                roots |= bash_writes.derive_roots(command, cwd)
    return roots


def resolve(payload: dict) -> list[str]:
    """The roots to check for this Stop payload, sorted."""
    cwd = payload.get("cwd") or os.environ.get("CLAUDE_PROJECT_DIR") or ""
    transcript = payload.get("transcript_path")
    if not isinstance(transcript, str) or not transcript:
        raise Undetermined("the payload has no transcript_path.")

    cwd_root = _root_of(cwd) if cwd else None
    project_dir = os.environ.get("CLAUDE_PROJECT_DIR")
    project_root = (_root_of(project_dir) if project_dir else None) or cwd_root
    if project_root is None:
        raise Undetermined(f"not inside a git repository ({cwd}).")

    deadline = time.time() + DEADLINE_S
    roots = touched_roots(turn_entries(Path(transcript)))
    try:
        kept, errors = bash_writes.project_filter(roots, project_root, deadline=deadline)
    except bash_writes.GitError as exc:
        raise Undetermined(f"git cannot read {project_root} ({exc}).") from exc
    if errors:
        raise Undetermined(" ".join(errors))
    if kept:
        return sorted(kept)

    if cwd_root is None:
        raise Undetermined(f"not inside a git repository ({cwd}).")
    return [cwd_root]


def main() -> int:
    try:
        payload = json.loads(sys.stdin.read())
        if not isinstance(payload, dict):
            raise Undetermined("the payload is no JSON object.")
        roots = resolve(payload)
    except ValueError:
        print("the payload is not valid JSON.")
        return 1
    except Undetermined as exc:
        print(exc)
        return 1
    print("\n".join(roots))
    return 0


if __name__ == "__main__":
    sys.exit(main())
