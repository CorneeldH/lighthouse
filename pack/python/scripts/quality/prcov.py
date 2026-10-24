"""Wijs elke regel onder `python.source_dirs` op `HEAD` toe aan de squash-PR die
hem schreef.

Voedt de nightly-arm van `ratchet.py`: één `coverage.json` van de volledige
suite, per-PR diff-coverage via `git blame` in plaats van een diff tegen één
merge-base. Het rekenwerk zelf komt uit `diffcov.diff_coverage` — deze module
levert alleen de toewijzing (welke regel hoort bij welke PR).
"""

from __future__ import annotations

import re
import subprocess
import sys
from collections.abc import Callable, Mapping, Sequence
from dataclasses import dataclass
from pathlib import Path
from typing import Any

from quality import diffcov
from quality.gitenv import suite_env

# De laatste `(#N)` aan het eind van de titel — wat GitHub bij squash-mergen
# aanplakt. Een eerdere ref middenin de titel (issue-move, cherry-pick) telt
# niet mee; alleen $ aan het eind onderscheidt "(#12) staat vooraan" (geen
# match) van "... (#1009)" (wel).
_PR_RE = re.compile(r"\(#(\d+)\)\s*$")

# Kopregel van `git blame --porcelain`: <40 hex sha> <orig-regel> <final-regel>
# [<aantal regels in de groep>]. Elke bronregel draagt zo'n kop, ook als hij
# niet de eerste van zijn groep is — dan alleen zonder de extra info-regels
# erachter. `final` is het regelnummer op HEAD.
_BLAME_HEADER_RE = re.compile(r"^([0-9a-f]{40}) \d+ (\d+)")


class GitCommandError(RuntimeError):
    """Een git-aanroep in deze module faalde. Fail closed."""


@dataclass(frozen=True)
class SquashCommit:
    sha: str
    pr: int


def pr_number(subject: str) -> int | None:
    """Het PR-nummer uit de **laatste** `(#N)` aan het eind van de titel."""
    match = _PR_RE.search(subject)
    return int(match.group(1)) if match else None


def _git_process(root: Path, args: list[str]) -> subprocess.CompletedProcess[str]:
    return subprocess.run(
        ["git", *args],
        cwd=root,
        capture_output=True,
        text=True,
        env=suite_env(),
        check=False,
    )


def _run_git(root: Path, args: list[str]) -> str:
    proc = _git_process(root, args)
    if proc.returncode != 0:
        raise GitCommandError(f"`git {' '.join(args)}` failed:\n{proc.stderr}")
    return proc.stdout


def _is_ancestor(root: Path, since: str) -> bool:
    proc = _git_process(root, ["merge-base", "--is-ancestor", since, "HEAD"])
    return proc.returncode == 0


def select_commits(root: Path, since: str | None) -> list[SquashCommit]:
    """Squash-commits op `HEAD` sinds `since`, elk met een PR-nummer.

    `since..HEAD` via `--first-parent` (de mainline, geen feature-commits die
    een squash-merge toch al wegschudt). Ontbreekt `since`, of is hij geen
    voorouder van `HEAD` (`git merge-base --is-ancestor`) — een verse repo,
    een force-push, een watermark die buiten de geschiedenis viel — dan valt
    de selectie terug op de laatste 24 uur in plaats van een range die faalt
    of stil de hele historie meeneemt. Commits zonder `(#N)` in de titel
    (geen squash-merge, een losse fix) vallen weg.
    """
    if since is not None and _is_ancestor(root, since):
        args = ["log", "--first-parent", "--format=%H%x1f%s", f"{since}..HEAD"]
    else:
        if since is not None:
            # Niet stil: een shallow clone of herschreven geschiedenis valt hier
            # anders elke nacht terug, en dan ook nog met een verkeerde blame.
            print(
                f"nightly: {since} is not an ancestor of HEAD (shallow clone or "
                "rewritten history?); judging the merges of the last 24 hours",
                file=sys.stderr,
            )
        args = [
            "log",
            "--first-parent",
            "--format=%H%x1f%s",
            "--since=24.hours.ago",
            "HEAD",
        ]
    output = _run_git(root, args)
    commits: list[SquashCommit] = []
    for line in output.splitlines():
        if not line:
            continue
        sha, _, subject = line.partition("\x1f")
        pr = pr_number(subject)
        if pr is not None:
            commits.append(SquashCommit(sha=sha, pr=pr))
    return commits


def blame(root: Path, path: str) -> dict[str, list[int]]:
    """sha → regelnummers op `HEAD`, uit `git blame --porcelain HEAD -- path`."""
    output = _run_git(root, ["blame", "--porcelain", "HEAD", "--", path])
    result: dict[str, list[int]] = {}
    for line in output.splitlines():
        match = _BLAME_HEADER_RE.match(line)
        if match is None:
            continue
        sha, final = match.group(1), int(match.group(2))
        result.setdefault(sha, []).append(final)
    return result


def _changed_python_files(root: Path, sha: str, source_dirs: Sequence[str]) -> list[str]:
    """`.py`-paden onder `source_dirs` die deze commit raakte (aangemaakt,
    gewijzigd of verwijderd) — of hij nog op `HEAD` bestaat, checkt de aanroeper."""
    if not source_dirs:
        return []
    output = _run_git(root, ["show", "--name-only", "--format=", sha, "--", *source_dirs])
    return [line for line in output.splitlines() if line.strip().endswith(".py")]


def attribute(
    root: Path, commits: Sequence[SquashCommit], source_dirs: Sequence[str] = ("src",)
) -> dict[int, dict[str, list[int]]]:
    """PR → {pad: regels}, via `git blame` op elk pad dat een commit raakte.

    `blame` draait op `HEAD` en dus per pad maar één keer, niet per commit:
    het resultaat wijst elke regel toe aan wie hem het laatst schreef, dus
    een latere commit die een regel herschrijft neemt hem daar automatisch
    over van een eerdere — er is geen apart "overwrite"-geval nodig. Een pad
    dat een latere commit weer verwijderde staat niet meer op `HEAD` en
    levert stil geen entry op, geen exceptie. Elke PR uit `commits` staat in
    de uitkomst, ook zonder een geraakt pad.
    """
    result: dict[int, dict[str, list[int]]] = {commit.pr: {} for commit in commits}
    blame_cache: dict[str, dict[str, list[int]]] = {}
    for commit in commits:
        for path in _changed_python_files(root, commit.sha, source_dirs):
            if not (root / path).exists():
                continue
            if path not in blame_cache:
                blame_cache[path] = blame(root, path)
            lines = blame_cache[path].get(commit.sha)
            if lines:
                result[commit.pr][path] = lines
    return result


def per_pr(
    attributed: Mapping[int, Mapping[str, Sequence[int]]],
    report: Mapping[str, Any],
    read_source: Callable[[str], str],
    exclude: Sequence[str] = (),
) -> dict[int, diffcov.DiffCoverage]:
    """Dekking per PR uit hetzelfde rapport — geen aparte suite-run per PR."""
    return {
        pr: diffcov.diff_coverage(paths, report, read_source, exclude)
        for pr, paths in attributed.items()
    }
