#!/usr/bin/env python3
"""Post a committed spec or plan file as a version chain of issue comments.

Stappen (spec *scripts/post_artefact.py*):
    1. pad oplossen, git-toplevel bepalen, soort uit de map (`paths.specs` of `paths.plans`)
    2. het bestand moet gecommit en schoon zijn
    3. de commit moet op GitHub staan (zo nodig pushen, behalve op `forge.default_branch`,
       HEAD of met open PR)
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

# Run as a file on its path: put `.lighthouse/lib` first, so `lighthouse_lib` imports
# from this installation.
if not __package__:
    sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "lib"))

from lighthouse_lib.artefacts import (
    Meta,
    current,
    hash_of,
    next_version,
    parse_meta,
    render,
    split,
)
from lighthouse_lib.config import ConfigError, config_path, load, lookup
from lighthouse_lib.protocol import Comment


class ArtefactFout(Exception):
    """`str(fout)` is de volledige `artefact:`-regel."""


def _config_fout(fout: ConfigError) -> ArtefactFout:
    return ArtefactFout(f"artefact: config: {' '.join(str(fout).split())}")


def _instelling(sleutel: str) -> str:
    """Een sleutel uit `lighthouse.toml`, met zijn default."""
    try:
        return str(lookup(load(config_path()), sleutel))
    except ConfigError as fout:
        raise _config_fout(fout) from fout


def _repo() -> str:
    """`forge.repo`; een `ArtefactFout` als hij ontbreekt of de config kapot is."""
    try:
        repo = load(config_path()).repo
    except ConfigError as fout:
        raise _config_fout(fout) from fout
    if repo is None:
        raise ArtefactFout(f"artefact: forge.repo is not set in {config_path()}")
    return repo


def _mappen() -> dict[str, str]:
    """Map van het bestand (vanaf de git-root) naar de soort, met één `/` erachter."""
    return {
        _instelling("paths.specs").strip("/") + "/": "spec",
        _instelling("paths.plans").strip("/") + "/": "plan",
    }


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
        raise RunFout(f"timed out after {fout.timeout:.0f} s") from fout


def git(cwd: Path, *args: str) -> str:
    return _draai(["git", "-C", str(cwd), *args])


def gh(*args: str, invoer: str | None = None) -> str:
    return _draai(["gh", *args], invoer)


def commit_bekend(sha: str) -> bool:
    """Staat `sha` op GitHub? Alleen een 404/422 van gh telt als nee; elke andere fout stijgt."""
    try:
        gh("api", f"repos/{_repo()}/commits/{sha}", "--jq", ".sha")
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
            _repo(),
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
        f"repos/{_repo()}/issues/{nr}/comments?per_page=100",
        "--jq",
        ".[] | {id, body, created_at, html_url}",
    )
    return [Comment.from_gh(json.loads(regel)) for regel in uit.splitlines() if regel]


def post_comment(nr: int, body: str) -> tuple[int, str]:
    uit = gh(
        "api",
        f"repos/{_repo()}/issues/{nr}/comments",
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
        raise ArtefactFout(f"artefact: {pad_arg} does not exist")
    try:
        top = Path(git(pad.parent, "rev-parse", "--show-toplevel"))
    except RunFout as fout:
        raise ArtefactFout(f"artefact: {pad_arg} is not in a git repository") from fout
    bestand = pad.relative_to(top).as_posix()
    for prefix, soort in _mappen().items():
        if bestand.startswith(prefix):
            return top, bestand, soort
    raise ArtefactFout(f"artefact: {bestand} is no spec or plan")


def _gecommitte_sha(top: Path, bestand: str) -> str:
    """De laatste commit van `bestand`; fout als het bestand gewijzigd of niet getrackt is."""
    if git(top, "status", "--porcelain", "--", bestand):
        raise ArtefactFout(f"artefact: {bestand} not committed")
    sha = git(top, "log", "-1", "--format=%H", "--", bestand)
    if not sha:
        raise ArtefactFout(f"artefact: {bestand} not committed")
    return sha


def _commit_bekend_of_gh_fout(sha: str) -> bool:
    try:
        return commit_bekend(sha)
    except RunFout as fout:
        raise ArtefactFout(f"artefact: gh failed: {fout}") from fout


def _zorg_dat_commit_op_github_staat(top: Path, sha: str) -> None:
    if _commit_bekend_of_gh_fout(sha):
        return
    branch = git(top, "rev-parse", "--abbrev-ref", "HEAD")
    try:
        heeft_pr = open_pr(branch)
    except RunFout as fout:
        raise ArtefactFout(f"artefact: gh failed: {fout}") from fout
    # Een force-push naar de default branch, een losse HEAD of een branch met open PR is
    # nooit aan ons.
    if branch in (_instelling("forge.default_branch"), "HEAD") or heeft_pr:
        raise ArtefactFout(f"artefact: commit {sha[:7]} not on GitHub")
    try:
        git(top, "push", "-u", "--force-with-lease", "origin", "HEAD")
    except RunFout as fout:
        raise ArtefactFout(f"artefact: push of {branch} failed: {fout}") from fout


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
                f"artefact: part {k}/{len(bodies)} not posted ({k - 1} were): {fout}"
            ) from fout
    return urls


def _post_artefact(nr: int, pad_arg: str) -> str:
    _repo()
    top, bestand, soort = _bepaal_bestand(pad_arg)
    try:
        sha = _gecommitte_sha(top, bestand)
        _zorg_dat_commit_op_github_staat(top, sha)
    except RunFout as fout:
        raise ArtefactFout(f"artefact: git failed: {fout}") from fout

    try:
        comments = lees_comments(nr)
    except RunFout as fout:
        raise ArtefactFout(f"artefact: gh failed: {fout}") from fout
    huidig = current(comments, soort, bestand)

    data = (top / bestand).read_bytes()
    hash_ = hash_of(data)
    if meta := _is_ongewijzigd(huidig, hash_):
        return f"artefact: {soort} unchanged (version {meta.version})"

    versie = next_version(comments, soort, bestand)
    vervangt = huidig[0].url if huidig else None
    delen = split(data.decode("utf-8"))
    try:
        # Eerst alles renderen: bij een weigering staat er niets half gepost.
        bodies = [
            render(
                Meta(soort, versie, k, len(delen), bestand, sha, hash_, vervangt), deel
            )
            for k, deel in enumerate(delen, start=1)
        ]
    except ConfigError as fout:
        raise _config_fout(fout) from fout
    except ValueError as fout:
        raise ArtefactFout(f"artefact: metadata unusable: {fout}") from fout
    urls = _post_delen(nr, bodies)
    return f"artefact: {soort} version {versie} posted, {len(delen)} parts → {urls[0]}"


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    parser.add_argument("nr", type=int, help="issue number")
    parser.add_argument("bestand", help="spec or plan file")
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
