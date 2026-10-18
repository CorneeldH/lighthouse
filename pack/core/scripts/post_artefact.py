#!/usr/bin/env python3
"""Post een gecommit spec- of plan-bestand als versie-keten van issue-comments.

Stappen (spec *scripts/post_artefact.py*):
    1. pad oplossen, git-toplevel bepalen, soort uit de map (specs/ of plans/)
    2. het bestand moet gecommit en schoon zijn
    3. de commit moet op GitHub staan (zo nodig pushen, behalve op main/HEAD/met open PR)
    4. de bestaande comments lezen en de huidige complete versie bepalen
    5. zelfde hash en een nog bereikbare commit: ongewijzigd, niets posten
    6. splitsen, alle delen renderen, dan in volgorde posten
    7. faalt deel k, dan melden hoeveel delen er wel staan
    8. succesregel op stdout

USAGE
    post_artefact.py <issue-nr> <bestand>

Exit-codes: 0 gepost of ongewijzigd, 1 elke `artefact:`-fout (op stderr), 2 gebruiksfout.
"""

from __future__ import annotations

import argparse
import json
import subprocess
import sys
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parent.parent
if str(REPO_ROOT) not in sys.path:
    sys.path.insert(0, str(REPO_ROOT))

from src.controller.artefacten import (
    Meta,
    hash_van,
    huidige,
    parse_meta,
    render,
    splits,
    volgende_versie,
)
from src.controller.comments import Comment
from src.controller.identity import identity

# Map van het bestand (vanaf de git-root) naar de soort van het artefact.
MAPPEN = {
    "docs/developer/planning/specs/": "spec",
    "docs/developer/planning/plans/": "plan",
}


class ArtefactFout(Exception):
    """`str(fout)` is de volledige `artefact:`-regel."""


class RunFout(Exception):
    """git of gh gaf geen 0 (of duurde te lang); het bericht draagt de stderr."""


def _draai(commando: list[str], invoer: str | None = None) -> str:
    # 300 s, niet 60: `git push` draait de hele pre-push-keten.
    try:
        return subprocess.run(
            commando,
            input=invoer,
            capture_output=True,
            text=True,
            timeout=300,
            check=True,
        ).stdout.strip()
    except subprocess.CalledProcessError as fout:
        # De staart: pre-push-hooks printen eerst hun `ok`-regels, de echte fout staat achteraan.
        raise RunFout((fout.stderr or "").strip()[-300:]) from fout
    except subprocess.TimeoutExpired as fout:
        raise RunFout(f"time-out na {fout.timeout:.0f} s") from fout


def git(cwd: Path, *args: str) -> str:
    return _draai(["git", "-C", str(cwd), *args])


def gh(*args: str, invoer: str | None = None) -> str:
    return _draai(["gh", *args], invoer)


def commit_bekend(sha: str) -> bool:
    """Staat `sha` op GitHub? Alleen een 404/422 van gh telt als nee; elke andere fout stijgt."""
    try:
        gh("api", f"repos/{identity().repo}/commits/{sha}", "--jq", ".sha")
    except RunFout as fout:
        if "HTTP 404" in str(fout) or "HTTP 422" in str(fout):
            return False
        raise
    return True


def open_pr(branch: str) -> bool:
    return (
        gh(
            "pr",
            "list",
            "--repo",
            identity().repo,
            "--head",
            branch,
            "--state",
            "open",
            "--json",
            "number",
            "--jq",
            "length",
        )
        != "0"
    )


def lees_comments(nr: int) -> list[Comment]:
    uit = gh(
        "api",
        "--paginate",
        f"repos/{identity().repo}/issues/{nr}/comments?per_page=100",
        "--jq",
        ".[] | {id, body, created_at, html_url}",
    )
    return [Comment.from_gh(json.loads(regel)) for regel in uit.splitlines() if regel]


def post_comment(nr: int, body: str) -> tuple[int, str]:
    uit = gh(
        "api",
        f"repos/{identity().repo}/issues/{nr}/comments",
        "--input",
        "-",
        "--jq",
        "{id, html_url}",
        invoer=json.dumps({"body": body}),
    )
    data = json.loads(uit)
    return int(data["id"]), str(data["html_url"])


def _bepaal_bestand(pad_arg: str) -> tuple[Path, str, str]:
    """(git-root, pad t.o.v. die root, soort) van `pad_arg`."""
    pad = Path(pad_arg).resolve()
    if not pad.is_file():
        raise ArtefactFout(f"artefact: {pad_arg} bestaat niet")
    try:
        top = Path(git(pad.parent, "rev-parse", "--show-toplevel"))
    except RunFout as fout:
        raise ArtefactFout(f"artefact: {pad_arg} ligt niet in een git-repo") from fout
    bestand = pad.relative_to(top).as_posix()
    for prefix, soort in MAPPEN.items():
        if bestand.startswith(prefix):
            return top, bestand, soort
    raise ArtefactFout(f"artefact: {bestand} is geen spec of plan")


def _gecommitte_sha(top: Path, bestand: str) -> str:
    """De laatste commit van `bestand`; fout als het bestand gewijzigd of niet getrackt is."""
    if git(top, "status", "--porcelain", "--", bestand):
        raise ArtefactFout(f"artefact: {bestand} niet gecommit")
    sha = git(top, "log", "-1", "--format=%H", "--", bestand)
    if not sha:
        raise ArtefactFout(f"artefact: {bestand} niet gecommit")
    return sha


def _commit_bekend_of_gh_fout(sha: str) -> bool:
    try:
        return commit_bekend(sha)
    except RunFout as fout:
        raise ArtefactFout(f"artefact: gh faalde: {fout}") from fout


def _zorg_dat_commit_op_github_staat(top: Path, sha: str) -> None:
    if _commit_bekend_of_gh_fout(sha):
        return
    branch = git(top, "rev-parse", "--abbrev-ref", "HEAD")
    try:
        heeft_pr = open_pr(branch)
    except RunFout as fout:
        raise ArtefactFout(f"artefact: gh faalde: {fout}") from fout
    # Een force-push naar main, een losse HEAD of een branch met open PR is nooit aan ons.
    if branch in ("main", "HEAD") or heeft_pr:
        raise ArtefactFout(f"artefact: commit {sha[:7]} niet op GitHub")
    try:
        git(top, "push", "-u", "--force-with-lease", "origin", "HEAD")
    except RunFout as fout:
        raise ArtefactFout(f"artefact: push van {branch} faalde: {fout}") from fout


def _is_ongewijzigd(huidig: list[Comment], hash_: str) -> Meta | None:
    """De meta van de huidige versie als die dezelfde inhoud en een bereikbare commit heeft."""
    meta = parse_meta(huidig[0].body) if huidig else None
    if (
        meta is not None
        and meta.hash == hash_
        and _commit_bekend_of_gh_fout(meta.commit)
    ):
        return meta
    return None


def _post_delen(nr: int, bodies: list[str]) -> list[str]:
    """Post de delen in volgorde; geeft de url's. Faalt deel k, dan een `ArtefactFout`."""
    urls: list[str] = []
    for k, body in enumerate(bodies, start=1):
        try:
            urls.append(post_comment(nr, body)[1])
        except RunFout as fout:
            raise ArtefactFout(
                f"artefact: deel {k}/{len(bodies)} niet gepost ({k - 1} wel): {fout}"
            ) from fout
    return urls


def _post_artefact(nr: int, pad_arg: str) -> str:
    top, bestand, soort = _bepaal_bestand(pad_arg)
    try:
        sha = _gecommitte_sha(top, bestand)
        _zorg_dat_commit_op_github_staat(top, sha)
    except RunFout as fout:
        raise ArtefactFout(f"artefact: git faalde: {fout}") from fout

    try:
        comments = lees_comments(nr)
    except RunFout as fout:
        raise ArtefactFout(f"artefact: gh faalde: {fout}") from fout
    huidig = huidige(comments, soort, bestand)

    data = (top / bestand).read_bytes()
    hash_ = hash_van(data)
    if meta := _is_ongewijzigd(huidig, hash_):
        return f"artefact: {soort} ongewijzigd (versie {meta.versie})"

    versie = volgende_versie(comments, soort, bestand)
    vervangt = huidig[0].url if huidig else None
    delen = splits(data.decode("utf-8"))
    try:
        # Eerst alles renderen: bij een weigering staat er niets half gepost.
        bodies = [
            render(
                Meta(soort, versie, k, len(delen), bestand, sha, hash_, vervangt), deel
            )
            for k, deel in enumerate(delen, start=1)
        ]
    except ValueError as fout:
        raise ArtefactFout(f"artefact: metadata onbruikbaar: {fout}") from fout
    urls = _post_delen(nr, bodies)
    return f"artefact: {soort} versie {versie} gepost, {len(delen)} delen → {urls[0]}"


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    parser.add_argument("nr", type=int, help="issuenummer")
    parser.add_argument("bestand", help="spec- of plan-bestand")
    args = parser.parse_args(argv)
    try:
        regel = _post_artefact(args.nr, args.bestand)
    except ArtefactFout as fout:
        print(fout, file=sys.stderr)
        return 1
    print(regel)
    return 0


if __name__ == "__main__":
    sys.exit(main())
