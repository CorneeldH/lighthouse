"""Switching strict -> light -> strict on the real pack ends with the same installation.

settings.json keeps a registration where it stands and appends a new one (internal/settings
Apply), so the hook entries may come back in another order: the file is compared as a set of
commands, every other file byte for byte."""

import json
import os
import subprocess
from pathlib import Path

PACKS = ("core", "go", "shell")


def _snapshot(root: Path) -> dict[str, bytes]:
    skip = {".git", ".lighthouse/components.toml"}
    files = {}
    for p in sorted(root.rglob("*")):
        rel = p.relative_to(root).as_posix()
        if p.is_file() and not any(rel == s or rel.startswith(s + "/") for s in skip):
            files[rel] = p.read_bytes()
    return files


def _commands(raw: bytes) -> list[str]:
    hooks = json.loads(raw).get("hooks", {})
    return sorted(
        h["command"] for groups in hooks.values() for g in groups for h in g["hooks"] if "command" in h
    )


def test_strict_light_strict_gives_the_same_files(tmp_path, lighthouse_bin):
    env = {k: v for k, v in os.environ.items() if not k.startswith("LIGHTHOUSE_")}
    subprocess.run(["git", "init", "-q", "-b", "main"], cwd=tmp_path, check=True)

    def run(*args: str) -> None:
        subprocess.run([str(lighthouse_bin), "init", "--yes", *args], cwd=tmp_path, env=env,
                       check=True, capture_output=True)

    packs = [a for p in PACKS for a in ("--pack", p)]
    run("--level", "strict", *packs)
    first = _snapshot(tmp_path)
    run("--upgrade", "--level", "light")
    light = _snapshot(tmp_path)
    run("--upgrade", "--level", "strict")
    again = _snapshot(tmp_path)

    assert light != first, "light installed the same as strict: the switch did nothing"
    assert again.keys() == first.keys()
    different = [k for k in first if k != ".claude/settings.json" and again[k] != first[k]]
    assert not different
    assert _commands(again[".claude/settings.json"]) == _commands(first[".claude/settings.json"])
