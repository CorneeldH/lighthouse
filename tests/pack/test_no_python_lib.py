"""Installed skills and instructions call the binary, not the Python lib (#274, AC 2).

A fresh `lighthouse init` with every pack must not leave a single `config.py` or
`headings.py` in a skill or in `AGENTS.md`: a skill that still calls them needs Python on the
host, which the milestone removes.
"""

from __future__ import annotations

import os
import subprocess

PACKS = ["go", "node", "shell", "expo", "python"]
NEEDLES = ("config.py", "headings.py")


def test_no_installed_skill_calls_config_or_headings_py(tmp_path, lighthouse_bin):
    env = {k: v for k, v in os.environ.items() if not k.startswith("LIGHTHOUSE_")}
    subprocess.run(["git", "init", "-q", "-b", "main"], cwd=tmp_path, check=True)
    args = [str(lighthouse_bin), "init", "--yes"]
    for pack in PACKS:
        args += ["--pack", pack]
    subprocess.run(args, cwd=tmp_path, env=env, check=True, capture_output=True)

    files = [tmp_path / "AGENTS.md"]
    for root in (".claude/skills", ".agents/skills"):
        files += [p for p in (tmp_path / root).rglob("*") if p.is_file()]
    # Guard against an empty pass: every pack installed skills in both trees.
    assert len([f for f in files if f.name == "SKILL.md"]) > 20
    assert (tmp_path / ".claude/skills/lighthouse-python-standard/SKILL.md").is_file()

    hits = sorted(
        f"{f.relative_to(tmp_path)}: {needle}"
        for f in files
        for needle in NEEDLES
        if needle in f.read_text(encoding="utf-8", errors="replace")
    )
    assert not hits, "\n".join(hits)
