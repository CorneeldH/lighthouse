#!/usr/bin/env python3
"""worktree-audit: één join over worktrees, PR-status, branches en sessies.

WAAROM DIT BESTAAT
------------------
Opruimen en terugvinden zijn twee vragen met dezelfde join eronder. Gemeten op
2026-08-24: 12 worktrees, 94 remote branches waarvan 83 met een gemergede PR, en
7 gelockte worktrees. Het is in twee weken twee keer met de hand gedaan en loopt
elke keer weer op.

Vier dingen die `git worktree prune` niet afvangt:

  1. Squash-merge maakt `git branch --merged` een leugen — de originele commits
     zitten niet in main's historie. Een PR-lookup op de headbranch is de enige
     betrouwbare test.
  2. Ongepushte commits: werk dat nergens anders bestaat. `--not --remotes`, niet
     een vergelijking met main. Let op: bij een squash-merge telt die vlag per
     constructie de HELE historie, want de originele commits bereiken nooit een
     remote. Rang 4 corrigeert dat met het merge-moment (`_squash_artefact`).
  3. Fixture-branches uit een mislukte testrun: één commit die zijn eigen root
     is, lokaal, zonder PR. Zie `_is_test_leakage`.
  4. Gelockte worktrees: melden, niet forceren.

DE RIJ IS EEN BRANCH, NIET EEN WORKTREE
---------------------------------------
83 van de 94 opruimbare dingen op origin hebben geen worktree; een
worktree-centrisch model kan die niet dragen zonder tweede tabel. Omgekeerd hangt
een worktree altijd aan een branch. Dus `worktree` en `port_block` zijn nullable.

DIT SCRIPT MUTEERT NIETS
------------------------
Geen delete, geen unlock, geen kill, geen schrijven naar `.port-registry`. Het
levert commando's; de mens plakt. `run_git` weigert een niet-lezende git-verb, dus
die norm zit in het uitvoerpad en niet alleen in de documentatie. Ook `--only`
muteert niets: lighthouse-close-issue voert de geleverde regels uit, zonder
vraag — niet dit script.

Gebruik:
    python3 .lighthouse/scripts/worktree_audit.py            # --cleanup (default)
    python3 .lighthouse/scripts/worktree_audit.py --sessions
    python3 .lighthouse/scripts/worktree_audit.py --origin
    python3 .lighthouse/scripts/worktree_audit.py --kpi
    python3 .lighthouse/scripts/worktree_audit.py --only <branch>   # exit 0 = opruimbaar
    python3 .lighthouse/scripts/worktree_audit.py --issue <nr>   # exit 2 = PR-lookup mislukt

De default branch komt uit `[forge] default_branch`, de extra beschermde refs en paden uit
`[worktree_audit]`, de repo voor de PR-lookup uit `[forge] repo`. Output is English;
`--only` ends on the fixed token `cleanup: yes` or `cleanup: no — <reason>`; `--issue`
prints one such block per branch of the issue, each ending on that token.
"""

from __future__ import annotations

import argparse
import datetime
import fnmatch
import json
import re
import subprocess
import sys
from collections.abc import Callable, Iterable
from dataclasses import dataclass
from pathlib import Path

# Run as a file (`python3 .lighthouse/scripts/worktree_audit.py`): put `.lighthouse/lib`
# first, so `lighthouse_lib` imports from this installation.
sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "lib"))

from lighthouse_lib import config  # noqa: E402
from lighthouse_lib.config import ConfigError  # noqa: E402
from lighthouse_lib.pr_body_gate import refs_targets  # noqa: E402

# --- protect-lijst ---------------------------------------------------------
# De default branch is altijd beschermd; een instelling die je kunt vergeten in
# te vullen is geen bescherming. Daarbovenop komen de refs en paden die alleen
# dit project kent, uit `[worktree_audit] protected_refs` en `protected_paths`
# (bijvoorbeeld checkpointrefs als `entire/*` of een poortregistratie als
# `.port-registry`): zulke dingen hebben geen PR, zijn niet gemerged, en worden
# niet gemist tot iets stukgaat. `apply_config` vult ze; tot dan gelden de
# defaults.
DEFAULT_BRANCH = "main"
PROTECTED_REFS: tuple[str, ...] = (DEFAULT_BRANCH,)
PROTECTED_PATHS: tuple[str, ...] = ()


def apply_config() -> str | None:
    """Lees de beschermde refs en paden en de default branch uit `lighthouse.toml`;
    geef de repo (`forge.repo`) terug, of None als die niet gezet is.

    De origin-remote is geen bron voor de repo."""
    global DEFAULT_BRANCH, PROTECTED_REFS, PROTECTED_PATHS
    cfg = config.load(config.config_path())
    DEFAULT_BRANCH = str(config.lookup(cfg, "forge.default_branch"))
    PROTECTED_REFS = (
        DEFAULT_BRANCH,
        *config.lookup(cfg, "worktree_audit.protected_refs"),
    )
    PROTECTED_PATHS = tuple(config.lookup(cfg, "worktree_audit.protected_paths"))
    try:
        return str(config.lookup(cfg, "forge.repo"))
    except KeyError:
        return None


@dataclass(frozen=True)
class Row:
    """Eén regel van de audit. De sleutel is een branchnaam, of — bij een
    wees-map — de mapnaam onder `.claude/worktrees/`."""

    name: str
    local: bool
    remote: bool
    worktree: str | None = None
    locked: bool = False
    lock_pid: int | None = None
    # De tekst na `locked `, of None bij een kale `locked` (#963). De
    # sessie-server van Remote Control zet een reden die begint met
    # "claude agent bridge-"; `_lock_state` toetst daarop.
    lock_reason: str | None = None
    dirty: bool | None = None
    unpushed: int | None = None
    pr_number: int | None = None
    pr_state: str | None = None  # "open" | "merged" | "closed" | None
    # Epoch-seconden. Bewust epoch en niet ISO: `merged_at` komt van GitHub in
    # UTC ("Z") en de commitdatum van git met een lokale offset. Die twee als
    # string vergelijken leest 08:24+02:00 als later dan 08:08Z, en dat is
    # precies andersom. Epoch kent geen offset.
    pr_merged_at: float | None = None
    tip_committed_at: float | None = None
    # None = niet gemeten. True = de tip-commit heeft geen ouder.
    root_commit: bool | None = None
    # False = de PR-lookup zelf is mislukt, dus pr_state draagt geen informatie.
    pr_lookup_ok: bool = True
    # None = de inhoudstoets is niet gelukt. Niet hetzelfde als (): dat is
    # "gemeten, niets uniek". Onbekend mag nooit als schoon lezen.
    unique_files: tuple[str, ...] | None = None
    # Commits die niet in main zitten (`main..<ref>`). 0 = de branch is nooit
    # verder gekomen dan main; dan is "niets uniek" leegte, geen geland werk.
    own_commits: int | None = None
    last_session: float | None = None
    session_id: str | None = None
    live: bool = False
    port_block: str | None = None
    orphan_dir: str | None = None
    dir_entries: tuple[str, ...] = ()


@dataclass(frozen=True)
class Verdict:
    rank: int
    icon: str
    text: str
    modifiers: tuple[str, ...] = ()

    @property
    def safe(self) -> bool:
        """Alleen rang 6 en 8 zijn "safe to remove". Alles daarboven blokkeert."""
        return self.rank in (6, 8)

    def render(self) -> str:
        suffix = f" [{', '.join(self.modifiers)}]" if self.modifiers else ""
        return f"{self.icon} {self.text}{suffix}"


def pid_alive(pid: int) -> bool:
    return Path(f"/proc/{pid}").exists()


def is_protected(row: Row) -> bool:
    # `refs/heads/` strippen voordat we matchen: een latere parser die de volle
    # ref levert (in plaats van `%(refname:short)`) mag de guard niet stilletjes
    # ontwapenen.
    short_name = row.name.removeprefix("refs/heads/")
    if any(fnmatch.fnmatch(short_name, pattern) for pattern in PROTECTED_REFS):
        return True
    return any(
        fnmatch.fnmatch(entry, pattern)
        for entry in row.dir_entries
        for pattern in PROTECTED_PATHS
    )


_SERVER_LOCK_PREFIX = "claude agent bridge-"


def _lock_state(
    row: Row, alive: Callable[[int], bool]
) -> tuple[Verdict | None, tuple[str, ...]]:
    """Rang 2, plus de modifier die een stale lock — of een server-lock —
    achterlaat.

    Een lock met een dode pid is geen blokkade maar een aantekening: de rij zakt
    door naar de volgende rang en draagt "stale lock" mee. Ontgrendelen doet
    dit script nooit. Een lock zonder pid blokkeert wel — onbekend is niet dood.

    Een lock waarvan de reden begint met "claude agent bridge-" is gezet door de
    sessie-server van Remote Control (#963), niet door een sessie zelf. Een spike
    mat: die pid is
    van de SERVER en blijft weken levend, ook lang nadat de sessie is gearchiveerd
    — hij bewijst dus niets over of er nu iemand werkt. Zo'n lock blokkeert daarom
    nooit op zichzelf (geen "stale lock" ernaast, ongeacht of de pid leeft):
    de rij zakt door naar de gewone ladder, met "server-lock" als aantekening. Of
    er nu wél iemand werkt, beslist de aparte `row.live`-trede in `judge`.
    """
    if not row.locked:
        return None, ()
    if row.lock_reason is not None and row.lock_reason.startswith(_SERVER_LOCK_PREFIX):
        return None, ("server-lock",)
    if row.lock_pid is None:
        return Verdict(2, "⛔", "locked, pid unknown"), ()
    if alive(row.lock_pid):
        return Verdict(2, "⛔", f"active session (pid {row.lock_pid})"), ()
    return None, ("stale lock",)


def _branch_verdict(row: Row, mods: tuple[str, ...]) -> Verdict:
    """Rang 5-8: de PR-toets en daarna de inhoudstoets.

    Alleen voor een rij die een branch heeft. Een wees-map komt hier niet langs —
    zonder die grens zou hij op rang 8 "safe to remove" heten.
    """
    if row.pr_state == "open":
        return Verdict(5, "⏸️", f"PR #{row.pr_number} open", mods)
    if row.pr_state == "merged":
        return Verdict(6, "✅", "safe to remove", mods)
    # Onbekend is nooit schoon. Een gemergede PR is het sterkere signaal en
    # bereikt rang 6 hierboven ook als de inhoudstoets faalde; alleen wat
    # anders naar 7/8 zou zakken wordt hier geblokkeerd.
    if not row.pr_lookup_ok:
        return Verdict(7, "⚠️", "PR status unknown — check", mods)
    if row.unique_files is None:
        return Verdict(7, "⚠️", "content check failed — check", mods)
    if row.unique_files:
        n = len(row.unique_files)
        return Verdict(7, "⚠️", f"unique work — review ({n} files)", mods)
    # Zelfde rang, andere reden: een branch zonder eigen commits heeft niets
    # uniek omdat er niets ís. "safe to remove" zou lezen als "het werk landde in
    # main" — zo leek issue-941, aangemaakt en nooit gecommit.
    if row.own_commits == 0:
        return Verdict(8, "✅", "empty — no own commits", mods)
    return Verdict(8, "✅", "safe to remove", mods)


def _squash_artefact(row: Row) -> bool:
    """Verklaart een ongepushte telling die alleen de squash-merge weerspiegelt.

    `git rev-list --count <b> --not --remotes` beantwoordt "hoeveel commits van
    deze branch staan nergens op een remote". Bij een squash-merge is dat de
    HELE historie: GitHub gooit de losse commits weg en zet één nieuwe commit op
    main, dus de originelen bereiken nooit een remote. Op deze repo maakte dat 9
    van de 14 ⚠️-rijen vals, en omdat rang 4 boven de inhoudstoets staat werden
    die branches nooit op inhoud gemeten.

    De vervangende maat is "is er ná de merge nog doorgecommit". Zo nee, dan
    landde alles in de squash-commit. Zo ja — of weten we het niet — dan blijft
    rang 4 staan; dat is het #544-geval, en onbekend is hier nooit schoon.
    """
    if row.pr_state != "merged":
        return False
    if row.pr_merged_at is None or row.tip_committed_at is None:
        return False
    return row.tip_committed_at <= row.pr_merged_at


def _is_test_leakage(row: Row) -> bool:
    """Een branch met één commit die zijn eigen root is, lokaal, zonder PR.

    Dat is de handtekening van `git checkout --orphan` in de verkeerde repo.
    `tests/integration/test_pre_push_checkpoint_scan.sh` beschrijft het
    mechanisme: faalt `mktemp -d` (een gesandboxte shell), dan landen zijn
    fixture-branches in de ECHTE repo. Zo ontstonden `branch-dirty` en
    `branch-allowlisted`.

    De toets is bewust smal. Een pre-rewrite branch als `v2.1-mac-thin-client`
    heeft óók een losse historie maar wél ouders; die mag niet meegesleept
    worden. En het verdict blijft rang 4: dit benoemt de vermoedelijke oorzaak,
    het geeft nooit zelf toestemming om te verwijderen.
    """
    return bool(
        row.root_commit
        and row.local
        and not row.remote
        and row.pr_state is None
        and row.pr_number is None
    )


def _unpushed_verdict(
    row: Row, mods: tuple[str, ...]
) -> tuple[Verdict | None, tuple[str, ...]]:
    """Rang 4, of `mods` aangevuld met "squash-merge" als het artefact is.

    Losgetrokken uit `judge` om zijn cyclomatische complexiteit onder de
    ruff-drempel (C901) te houden na de rang-2-trede van #963; het gedrag is
    ongewijzigd.
    """
    if row.local and row.unpushed is None:
        return Verdict(4, "⚠️", "unpushed commits unknown — check", mods), mods
    if row.unpushed:
        if _is_test_leakage(row):
            return (
                Verdict(
                    4, "⚠️", "orphan commit without parent — likely test leakage", mods
                ),
                mods,
            )
        if _squash_artefact(row):
            mods = (*mods, "squash-merge")
        else:
            return (
                Verdict(4, "⚠️", f"{row.unpushed} local commits — check", mods),
                mods,
            )
    return None, mods


def judge(row: Row, *, alive: Callable[[int], bool] = pid_alive) -> Verdict:
    """De ladder: eerste treffer wint, veiligheid bovenaan.

    Rang 3 en 4 staan boven rang 6 — een gemergede branch met ongepushte commits
    krijgt nooit plat "safe to remove". Dat is het #544-geval en de reden dat rang 4
    niet met rang 6 is samengevoegd.

    Rang 2 dekt twee dingen: een lock met een levende of onbekende pid, en een
    lopende sessie met de worktree als cwd (#963) — ongeacht wie de worktree
    aanmaakte. Een server-lock (`claude agent bridge-…`, zie `_lock_state`)
    blokkeert op zichzelf niet; alleen deze `row.live`-trede beslist dan nog.
    """
    if is_protected(row):
        return Verdict(1, "🔒", "never touch")

    lock, mods = _lock_state(row, alive)
    if lock is not None:
        return lock

    # Een lopende sessie met deze worktree als cwd (#963). De sessie-server van
    # Remote Control maakt worktrees die nooit een PR krijgen; zonder deze trede
    # heet een schone, lege worktree van een lópende sessie rang 8 "safe to remove".
    # Het signaal is "hier werkt nu iemand", ongeacht wie de worktree aanmaakte —
    # en dus ook los van of `_lock_state` hierboven een server-lock doorliet.
    if row.live:
        return Verdict(2, "⛔", "running session", mods)

    if row.worktree is not None and row.dirty is not False:
        text = "save first" if row.dirty else "tree status unreadable"
        return Verdict(3, "⛔", text, mods)

    verdict, mods = _unpushed_verdict(row, mods)
    if verdict is not None:
        return verdict

    if row.local or row.remote:
        return _branch_verdict(row, mods)

    # Minor 7: git kent een losgekoppelde worktree (hij staat in `worktree
    # list`) maar kent een wees-map niet — en de wees-map is degene die iemand
    # per ongeluk `rm -rf`'t. Beide renderen rang 9, maar niet dezelfde tekst.
    if row.worktree is not None:
        return Verdict(9, "❓", "detached worktree — manual", mods)
    # Rang 9 wordt overruled door rang 1 zodra de map een `.port-registry` bevat.
    # Zonder die overrule zou `.claude/worktrees/worktrees/` als wees worden
    # gemeld, en een wees-melding is de eerste stap naar een `rm -rf`.
    return Verdict(9, "❓", "orphan — manual", mods)


# Alleen-lezen git-verbs. De weigering hieronder is geen documentatie maar een
# poort: dit script muteert niets, ook niet als een latere wijziging dat per
# ongeluk probeert. Precies de verbs die dit script uitgeeft — geen "kan later
# nog van pas komen"-entries: een allowlist die meer toelaat dan nodig is, is
# geen allowlist meer, en toevoegen aan een aanroep moet een bewuste daad
# blijven (Minor 5).
READONLY_GIT_VERBS: frozenset[str] = frozenset(
    {
        "worktree",
        "branch",
        "status",
        "rev-list",
        "diff",
        "cat-file",
        "remote",
    }
)
# `branch` staat alleen alleen-lezen vlaggen toe. Een denylist kende ooit
# `d/delete/m/move/c/copy` en liet `-f`, `--force` en `-u`/`--set-upstream`
# gewoon door — die verplaatsen of herschrijven een ref. Een allowlist kan dat
# per constructie niet: alleen wat hier staat komt door.
#
# `--list`, `--contains`, `--merged` en `--no-merged` staan hier bewust niet:
# elk vereist een bare positional (een patroon, een commit) en die wordt
# hieronder sowieso al geweigerd, dus ze waren dode entries. `--merged` is
# daarnaast geen oversight maar een projectregel — squash-merge maakt
# `git branch --merged` een leugen (zie moduledoc); merge-detectie loopt via
# de PR-headbranch, nooit via deze vlag.
_READONLY_BRANCH_FLAGS: frozenset[str] = frozenset(
    {
        "-a",
        "--all",
        "-r",
        "--remotes",
        "-v",
        "-vv",
        "--verbose",
        "--show-current",
    }
)
# `worktree` staat alleen `list` toe. Een denylist met mutatie-subverbs kan een
# toekomstig git-subcommando niet kennen; een allowlist hoeft niets te kennen.
_READONLY_WORKTREE_SUBVERBS: frozenset[str] = frozenset({"list"})


def _assert_readonly(args: list[str]) -> None:
    """De vier allowlist-toetsen. Los van `run_git` zodat een unit-test de
    poort kan bevragen zonder ooit `subprocess.run` te raken."""
    verb = args[0] if args else ""
    if verb not in READONLY_GIT_VERBS:
        raise ValueError(f"git {verb}: read-only commands only")
    if verb == "worktree" and (
        len(args) < 2 or args[1] not in _READONLY_WORKTREE_SUBVERBS
    ):
        subverb = args[1] if len(args) > 1 else "<none>"
        raise ValueError(f"git worktree {subverb}: read-only commands only")
    if verb == "branch" and any(
        not a.startswith("-")
        or not (a in _READONLY_BRANCH_FLAGS or a.startswith(("--format=", "--sort=")))
        for a in args[1:]
    ):
        raise ValueError("git branch: read-only commands only")
    if verb == "remote" and (len(args) < 2 or args[1] not in {"get-url", "-v"}):
        raise ValueError("git remote: read-only commands only")


def run_git(args: list[str], *, cwd: str | Path | None = None) -> str | None:
    """Draai een alleen-lezen git-commando. Geeft None als het faalde.

    None is een derde uitkomst naast geslaagd en leeg: de aanroeper moet hem
    conservatief afhandelen (onbekende staat blokkeert, hij heet nooit "schoon").
    """
    _assert_readonly(args)
    try:
        proc = subprocess.run(
            ["git", *args],
            cwd=cwd,
            capture_output=True,
            text=True,
            check=False,
            timeout=60,
        )
    except (OSError, subprocess.SubprocessError):
        return None
    return proc.stdout if proc.returncode == 0 else None


@dataclass(frozen=True)
class WorktreeInfo:
    path: str
    branch: str | None
    locked: bool
    lock_pid: int | None
    lock_reason: str | None = None


_LOCK_PID = re.compile(r"\(pid (\d+)")


def parse_worktrees(porcelain: str) -> list[WorktreeInfo]:
    """Parse `git worktree list --porcelain`.

    Blokken zijn gescheiden door een lege regel. `locked` kan kaal voorkomen of
    met reden; de Claude-lock draagt de pid in die reden.
    """
    out: list[WorktreeInfo] = []
    path: str | None = None
    branch: str | None = None
    locked = False
    lock_pid: int | None = None
    lock_reason: str | None = None

    def flush() -> None:
        nonlocal path, branch, locked, lock_pid, lock_reason
        if path is not None:
            out.append(WorktreeInfo(path, branch, locked, lock_pid, lock_reason))
        path, branch, locked, lock_pid, lock_reason = None, None, False, None, None

    for line in porcelain.splitlines():
        if line.startswith("worktree "):
            flush()
            path = line[len("worktree ") :]
        elif line.startswith("branch refs/heads/"):
            branch = line[len("branch refs/heads/") :]
        elif line == "locked" or line.startswith("locked "):
            locked = True
            lock_reason = line[len("locked ") :] if line.startswith("locked ") else None
            match = _LOCK_PID.search(line)
            lock_pid = int(match.group(1)) if match else None
    flush()
    return out


def parse_branches(text: str, *, remote: bool = False) -> list[str]:
    """Parse `git branch --format=%(refname:short)`, remote-variant strip origin/."""
    names: list[str] = []
    for line in text.splitlines():
        name = line.strip()
        if not name:
            continue
        if remote:
            prefix, _, rest = name.partition("/")
            if prefix != "origin" or not rest or rest == "HEAD":
                continue
            name = rest
        names.append(name)
    return names


def _epoch(stamp: object) -> float | None:
    """GitHub-ISO ("2026-08-02T08:08:31Z") naar epoch-seconden.

    `fromisoformat` slikt de "Z" zelf sinds 3.11; een handmatige vervanging naar
    "+00:00" is dood gewicht (FURB162).

    Alleen epoch verlaat deze functie. Een ISO-string uit GitHub staat in UTC en
    een commitdatum uit git draagt een lokale offset; die twee als tekst
    vergelijken leest "08:24+02:00" als later dan "08:08Z" terwijl het twee uur
    eerder is. Dat is geen theoretische val — het gaf een volle tabel valse
    "COMMIT NA MERGE"-regels voordat deze functie bestond.
    """
    if not isinstance(stamp, str) or not stamp:
        return None
    try:
        return datetime.datetime.fromisoformat(stamp).timestamp()
    except ValueError:
        return None


@dataclass(frozen=True)
class PullInfo:
    number: int
    state: str  # "open" | "merged" | "closed"
    # Epoch-seconden, of None als de PR niet gemerged is of GitHub geen
    # bruikbare `merged_at` gaf. Zie Row.pr_merged_at voor waarom epoch.
    merged_at: float | None = None


# Een headbranch kan meerdere PR's dragen. Open weegt het zwaarst (rang 5 pauzeert),
# daarna gemerged, daarna dicht-zonder-merge; binnen dezelfde staat wint de hoogste
# PR-nummer.
_PR_RANK = {"open": 0, "merged": 1, "closed": 2}


def parse_pulls(payload: Iterable[dict]) -> dict[str, PullInfo]:
    """Join-sleutel is de headbranch.

    De REST-val: `state` kent hier alleen "open" en "closed". Gemerged herken je
    aan `merged_at`, niet aan `state`.
    """
    best: dict[str, PullInfo] = {}
    for pull in payload:
        ref = pull.get("head", {}).get("ref")
        if not ref:
            continue
        state = "merged" if pull.get("merged_at") else str(pull.get("state", "closed"))
        info = PullInfo(int(pull["number"]), state, _epoch(pull.get("merged_at")))
        current = best.get(ref)
        if current is None or (_PR_RANK[state], -info.number) < (
            _PR_RANK[current.state],
            -current.number,
        ):
            best[ref] = info
    return best


def parse_port_registry(text: str) -> dict[str, str]:
    """`<sleutel>=<poort>` per regel; de sleutel is een issuenummer."""
    ports: dict[str, str] = {}
    for line in text.splitlines():
        key, sep, value = line.partition("=")
        if sep and key.strip():
            ports[key.strip()] = value.strip()
    return ports


@dataclass(frozen=True)
class SessionInfo:
    pid: int
    session_id: str
    cwd: str


def parse_sessions(records: Iterable[dict]) -> dict[str, SessionInfo]:
    """`~/.claude/sessions/*.json` bevat blijkens meting alleen LOPENDE sessies."""
    out: dict[str, SessionInfo] = {}
    for record in records:
        cwd = record.get("cwd")
        pid = record.get("pid")
        session_id = record.get("sessionId")
        if not (cwd and pid and session_id):
            continue
        out[str(cwd)] = SessionInfo(int(pid), str(session_id), str(cwd))
    return out


def encode_project_dir(path: str) -> str:
    """Pad → mapnaam onder `~/.claude/projects/`: `/`, `_` en `.` worden `-`."""
    return re.sub(r"[/_.]", "-", path)


def latest_transcript(
    projects_root: Path, worktree_path: str
) -> tuple[float, str] | None:
    """(mtime, sessionId) van het nieuwste transcript voor dit pad, of None.

    AC 7: afgesloten sessies komen hiervandaan, niet uit `~/.claude/sessions/`.
    """
    directory = projects_root / encode_project_dir(worktree_path)
    newest: tuple[float, str] | None = None
    # Geen try om de glob heen: `Path.glob` slikt zowel een ontbrekende als een
    # onleesbare map en geeft dan gewoon een lege lijst (gemeten op 3.12). Een
    # except eromheen is dode code, en dode defensie leest als dekking die er
    # niet is. De stat per bestand kan wél racen — die guard blijft.
    for entry in directory.glob("*.jsonl"):
        try:
            mtime = entry.stat().st_mtime
        except OSError:
            continue
        if newest is None or mtime > newest[0]:
            newest = (mtime, entry.stem)
    return newest


@dataclass(frozen=True)
class BranchProbe:
    """Per-branch metingen. None betekent "niet gemeten", nooit "schoon"."""

    dirty: bool | None = None
    unpushed: int | None = None
    unique_files: tuple[str, ...] | None = None
    own_commits: int | None = None
    tip_committed_at: float | None = None
    root_commit: bool | None = None


@dataclass(frozen=True)
class OrphanDir:
    """Een map onder `.claude/worktrees/` die niet in `git worktree list` staat."""

    name: str
    path: str
    dir_entries: tuple[str, ...]


_DIGITS = re.compile(r"\d+")


def _port_for(name: str, ports: dict[str, str]) -> str | None:
    """Het poortblok is per issuenummer geregistreerd; match op cijfers in de naam."""
    for chunk in _DIGITS.findall(name):
        if chunk in ports:
            return ports[chunk]
    return None


def build_rows(
    *,
    worktrees: list[WorktreeInfo],
    local_branches: list[str],
    remote_branches: list[str],
    pulls: dict[str, PullInfo],
    probes: dict[str, BranchProbe],
    sessions: dict[str, SessionInfo],
    transcripts: dict[str, tuple[float, str]],
    detached_dirty: dict[str, bool | None],
    orphans: list[OrphanDir],
    ports: dict[str, str],
    pr_lookup_ok: bool = True,
    alive: Callable[[int], bool] = pid_alive,
) -> list[Row]:
    """De join. Zuiver: geen subprocess, geen filesystem."""
    worktree_by_branch = {w.branch: w for w in worktrees if w.branch}
    local = set(local_branches)
    remote = set(remote_branches)

    rows: list[Row] = []
    for name in sorted(local | remote):
        worktree = worktree_by_branch.get(name)
        probe = probes.get(name, BranchProbe())
        pull = pulls.get(name)
        session = sessions.get(worktree.path) if worktree else None
        transcript = transcripts.get(worktree.path) if worktree else None
        rows.append(
            Row(
                name=name,
                local=name in local,
                remote=name in remote,
                worktree=worktree.path if worktree else None,
                locked=worktree.locked if worktree else False,
                lock_pid=worktree.lock_pid if worktree else None,
                lock_reason=worktree.lock_reason if worktree else None,
                dirty=probe.dirty,
                unpushed=probe.unpushed,
                pr_number=pull.number if pull else None,
                pr_state=pull.state if pull else None,
                pr_merged_at=pull.merged_at if pull else None,
                tip_committed_at=probe.tip_committed_at,
                root_commit=probe.root_commit,
                pr_lookup_ok=pr_lookup_ok,
                unique_files=probe.unique_files,
                own_commits=probe.own_commits,
                last_session=transcript[0] if transcript else None,
                session_id=(
                    session.session_id
                    if session
                    else (transcript[1] if transcript else None)
                ),
                live=session is not None and alive(session.pid),
                port_block=_port_for(name, ports),
            )
        )

    # Minor 6: een worktree zonder branchregel (detached HEAD) staat niet in
    # `local`/`remote` én is geen wees — git kent hem gewoon via
    # `worktree list`. Zonder een eigen rij verdwijnt hij spoorloos uit de
    # tabel, en dat is dezelfde defectklasse als een gat in de protect-lijst.
    for worktree in worktrees:
        if worktree.branch is not None:
            continue
        name = Path(worktree.path).name
        session = sessions.get(worktree.path)
        transcript = transcripts.get(worktree.path)
        rows.append(
            Row(
                name=name,
                local=False,
                remote=False,
                worktree=worktree.path,
                locked=worktree.locked,
                lock_pid=worktree.lock_pid,
                lock_reason=worktree.lock_reason,
                dirty=detached_dirty.get(worktree.path),
                last_session=transcript[0] if transcript else None,
                session_id=(
                    session.session_id
                    if session
                    else (transcript[1] if transcript else None)
                ),
                live=session is not None and alive(session.pid),
                port_block=_port_for(name, ports),
            )
        )

    for orphan in orphans:
        rows.append(
            Row(
                name=orphan.name,
                local=False,
                remote=False,
                orphan_dir=orphan.path,
                dir_entries=orphan.dir_entries,
                port_block=_port_for(orphan.name, ports),
            )
        )
    return rows


WORKTREE_DIRNAME = ".claude/worktrees"


def run_gh_pulls(slug: str | None) -> list[dict] | None:
    """`gh api` — nooit `gh pr list --json`: dat breekt onder de rtk-proxy.

    None betekent "de lookup is mislukt", niet "deze repo heeft geen PR's". Dat
    verschil draagt `Row.pr_lookup_ok`; zonder dat onderscheid leest een kapotte
    `gh` als "geen enkele branch heeft een PR" en zakt alles naar rang 7/8. Zonder
    `forge.repo` is er geen lookup: ook dat is None.
    """
    if slug is None:
        return None
    try:
        proc = subprocess.run(
            [
                "gh",
                "api",
                "--paginate",
                f"repos/{slug}/pulls?state=all&per_page=100",
                "--jq",
                ".[] | {number, state, merged_at, head: {ref: .head.ref}}",
            ],
            capture_output=True,
            text=True,
            check=False,
            timeout=300,
        )
    except (OSError, subprocess.SubprocessError):
        return None
    if proc.returncode != 0:
        return None
    payload: list[dict] = []
    for line in proc.stdout.splitlines():
        line = line.strip()
        if not line:
            continue
        try:
            payload.append(json.loads(line))
        except json.JSONDecodeError:
            continue
    return payload


def repo_slug() -> str | None:
    """De repo uit `lighthouse.toml`; de origin-remote is geen bron."""
    return apply_config()


ISSUE_PRS_QUERY = """query($owner:String!,$name:String!,$nr:Int!){repository(owner:$owner,name:$name){issue(number:$nr){
  closedByPullRequestsReferences(first:50,includeClosedPrs:true){nodes{headRefName merged isCrossRepository repository{nameWithOwner}}}
  timelineItems(itemTypes:[CROSS_REFERENCED_EVENT],first:100){nodes{... on CrossReferencedEvent{source{... on PullRequest{headRefName merged isCrossRepository body repository{nameWithOwner}}}}}}}}}"""


def parse_issue_pr_heads(payload: dict, slug: str, nr: int) -> list[str] | None:
    """De headbranches van de gemergde PR's van issue `nr`, in binnenkomvolgorde.

    Twee bronnen. `closedByPullRequestsReferences` kent alleen PR's met een
    sluitwoord; een PR met `Refs #<nr>` staat daar niet in. Die maakt op het issue
    wel een `CrossReferencedEvent` met de PR als `source`. Zo'n event ontstaat ook
    voor een PR die het issue alleen noemt ("zie #<nr>"), en die hoort er niet bij:
    een cross-ref telt alleen als `refs_targets` (de lezer van `pr-create-gate`:
    buiten HTML-commentaar, zonder reden-eis) `<nr>` in de PR-body vindt. Een
    closing-node telt altijd. Beide moeten `merged` zijn en uit dezelfde repo komen:
    `repository` van een PR is de *basis*-repo, dus een fork-PR naar `slug` slaagt
    daarop; `isCrossRepository` sluit die uit (een forkbranch als `patch-1` mag niet
    met een lokale branch van dezelfde naam verward worden).

    None = de lookup is mislukt (GraphQL-`errors`, of `issue: null` bij een
    verkeerd nummer): dat is geen "geen PR's".
    """
    if not isinstance(payload, dict) or payload.get("errors"):
        return None
    try:
        issue = payload["data"]["repository"]["issue"]
    except (KeyError, TypeError):
        return None
    if not isinstance(issue, dict):
        return None
    heads: list[str] = []

    def take(node: object, *, needs_refs: bool) -> None:
        if not isinstance(node, dict) or not node.get("merged"):
            return
        head = node.get("headRefName")
        repo = (node.get("repository") or {}).get("nameWithOwner")
        if not head or repo != slug or node.get("isCrossRepository"):
            return
        if needs_refs and nr not in refs_targets(node.get("body") or ""):
            return
        if head not in heads:
            heads.append(head)

    closing = (issue.get("closedByPullRequestsReferences") or {}).get("nodes") or []
    for node in closing:
        take(node, needs_refs=False)
    timeline = (issue.get("timelineItems") or {}).get("nodes") or []
    for item in timeline:
        take((item or {}).get("source"), needs_refs=True)
    return heads


def run_gh_issue_pr_heads(slug: str | None, nr: int) -> list[str] | None:
    """`gh api graphql` voor `parse_issue_pr_heads`; None = lookup mislukt.

    Zonder `forge.repo` is er geen lookup: ook dat is None. `-F nr=` en niet `-f`:
    `$nr` is een `Int!`.
    """
    if slug is None:
        return None
    owner, _, name = slug.partition("/")
    try:
        proc = subprocess.run(
            [
                "gh",
                "api",
                "graphql",
                "-f",
                f"owner={owner}",
                "-f",
                f"name={name}",
                "-F",
                f"nr={nr}",
                "-f",
                f"query={ISSUE_PRS_QUERY}",
            ],
            capture_output=True,
            text=True,
            check=False,
            timeout=300,
        )
    except (OSError, subprocess.SubprocessError):
        return None
    if proc.returncode != 0:
        return None
    try:
        payload = json.loads(proc.stdout)
    except json.JSONDecodeError:
        return None
    return parse_issue_pr_heads(payload, slug, nr)


def _tip_shape(target: str, repo_root: Path) -> tuple[float | None, bool | None]:
    """Committijd (epoch) en "heeft de tip een ouder" in één git-aanroep.

    `git rev-list -1 --timestamp --parents <ref>` levert
    `<epoch> <sha> [<parent> ...]`. Twee velden betekent: geen ouder, dus de
    tip is zijn eigen root — de handtekening van `git checkout --orphan`.

    `--timestamp` geeft epoch en dus geen tijdzone. Dat is de reden dat hier
    geen `log --format=%cI` staat: die draagt een offset, en `log` zou boven
    dat alles de alleen-lezen allowlist moeten oprekken.

    Faalt de aanroep, dan blijft alles None — niet gemeten is nooit schoon.
    """
    out = run_git(["rev-list", "-1", "--timestamp", "--parents", target], cwd=repo_root)
    if not out or not out.strip():
        return None, None
    fields = out.split("\n")[0].split()
    if len(fields) < 2 or not fields[0].isdigit():
        return None, None
    return float(fields[0]), len(fields) == 2


def probe_branch(
    name: str,
    worktree: WorktreeInfo | None,
    repo_root: Path,
    *,
    ref: str | None = None,
    skip_content: bool = False,
) -> BranchProbe:
    """Meet de drie dingen die prune niet afvangt.

    `--not --remotes` is het verschil tussen "bestaat nergens anders"
    (dataverlies) en "niet gemerged" (normaal voor een squash-lane).

    `ref` is de ref waartegen git meet. `name` is de rijsleutel, en voor een
    remote-only branch is dat niet hetzelfde: `parse_branches(remote=True)`
    strip de `origin/`-prefix, dus de kale naam is dan geen resolvebare ref
    meer. Zonder `ref` sterft git op "ambiguous argument" en wordt dat via
    `run_git` een `None` — "niet gemeten" — voor 93 van de 101 remote
    branches op deze repo. `target = ref or name` houdt oude aanroepers
    (lokale branch, `ref` niet meegegeven) werkend.
    """
    target = ref or name

    dirty: bool | None = None
    if worktree is not None:
        status = run_git(["status", "--porcelain"], cwd=worktree.path)
        dirty = None if status is None else bool(status.strip())

    count = run_git(
        ["rev-list", "--count", target, "--not", "--remotes"], cwd=repo_root
    )
    unpushed: int | None = None
    if count is not None and count.strip().isdigit():
        unpushed = int(count.strip())

    tip_at, root = _tip_shape(target, repo_root)

    # skip_content=True: de PR is al gemerged, dus rang 6 wint sowieso van
    # rang 7/8 en `unique_files` wordt nooit geraadpleegd — een diff plus een
    # `cat-file -e` per gewijzigd bestand is dan pure verspilling (~106
    # branches × meerdere git-calls op deze repo). `None` blijft staan voor
    # "niet gemeten, en niet nodig"; het mag hier nooit `()` worden, want dat
    # zou een meting claimen die nooit heeft plaatsgevonden.
    unique: tuple[str, ...] | None = None
    own: int | None = None
    if not skip_content:
        ahead = run_git(
            ["rev-list", "--count", f"{DEFAULT_BRANCH}..{target}"], cwd=repo_root
        )
        if ahead is not None and ahead.strip().isdigit():
            own = int(ahead.strip())
        changed = run_git(
            ["diff", "--name-only", f"{DEFAULT_BRANCH}...{target}"], cwd=repo_root
        )
        if changed is not None:
            missing = []
            for path in changed.splitlines():
                path = path.strip()
                if (
                    path
                    and run_git(
                        ["cat-file", "-e", f"{DEFAULT_BRANCH}:{path}"], cwd=repo_root
                    )
                    is None
                ):
                    missing.append(path)
            unique = tuple(missing)
    return BranchProbe(
        dirty=dirty,
        unpushed=unpushed,
        unique_files=unique,
        own_commits=own,
        tip_committed_at=tip_at,
        root_commit=root,
    )


def _probe_all_branches(
    local: list[str],
    remote: list[str],
    worktree_by_branch: dict[str, WorktreeInfo],
    pulls: dict[str, PullInfo],
    root: Path,
) -> dict[str, BranchProbe]:
    """Bouwt de probe per branch: de juiste `ref` (Kritiek 1) en `skip_content`
    voor een al gemergede PR (Minor 5)."""
    local_set = set(local)
    result: dict[str, BranchProbe] = {}
    for name in sorted(local_set | set(remote)):
        if any(fnmatch.fnmatch(name, p) for p in PROTECTED_REFS):
            continue
        pull = pulls.get(name)
        result[name] = probe_branch(
            name,
            worktree_by_branch.get(name),
            root,
            ref=name if name in local_set else f"origin/{name}",
            skip_content=pull is not None and pull.state == "merged",
        )
    return result


def find_orphans(repo_root: Path, worktrees: list[WorktreeInfo]) -> list[OrphanDir]:
    """Mappen onder `.claude/worktrees/` die git niet kent.

    Wezen uit de devcontainer: hun `.git` wijst naar `/workspaces/...`, dus
    `git worktree prune` raakt ze niet en alleen dit verschil vindt ze.
    """
    known = {w.path for w in worktrees}
    base = repo_root / WORKTREE_DIRNAME
    orphans: list[OrphanDir] = []
    try:
        entries = sorted(base.iterdir())
    except OSError:
        return []
    for entry in entries:
        if not entry.is_dir() or str(entry) in known:
            continue
        try:
            names = tuple(sorted(child.name for child in entry.iterdir()))
        except OSError:
            names = ()
        orphans.append(OrphanDir(entry.name, str(entry), names))
    return orphans


def _load_sessions(sessions_dir: Path) -> dict[str, SessionInfo]:
    """Leest `~/.claude/sessions/*.json`.

    Geen try om de glob heen: `Path.glob` geeft een lege lijst voor zowel een
    ontbrekende als een onleesbare map (gemeten op 3.12, zie ook
    `latest_transcript`) — een except eromheen is dode code en dode defensie
    leest als dekking die er niet is. De try per bestand blijft: `read_text`
    / `json.loads` kan wél echt falen.
    """
    records: list[dict] = []
    for path in sorted(sessions_dir.glob("*.json")):
        try:
            records.append(json.loads(path.read_text()))
        except (OSError, json.JSONDecodeError):
            continue
    return parse_sessions(records)


def _detached_dirty(worktrees: list[WorktreeInfo]) -> dict[str, bool | None]:
    """Tree-status per detached-HEAD worktree (Minor 6).

    Zo'n worktree heeft geen branch en dus geen `probe_branch`-resultaat; hij
    landt anders altijd op rang 3 (dirty is nooit `False`) in plaats van
    rang 3 bij een echt vuile tree en rang 9 ("wees") bij een schone.
    """
    result: dict[str, bool | None] = {}
    for worktree in worktrees:
        if worktree.branch is not None:
            continue
        status = run_git(["status", "--porcelain"], cwd=worktree.path)
        result[worktree.path] = None if status is None else bool(status.strip())
    return result


def collect(repo_root: Path | None = None) -> list[Row]:
    # Minor 4: `or ""` verstopte een mislukte git-call als een lege, schone
    # wereld — en dan vuurt de rang-3-vuile-tree-guard voor niets meer. Een
    # tool die de repo niet kan lezen moet dat zeggen, niet een leeg antwoord
    # geven dat op "niets te doen" lijkt.
    porcelain = run_git(["worktree", "list", "--porcelain"])
    if porcelain is None:
        raise SystemExit(
            "git worktree list --porcelain failed — cannot read the repo."
        )
    worktrees = parse_worktrees(porcelain)
    # `git worktree list --porcelain` zet de hoofd-worktree altijd als eerste
    # blok: entry 0 is de hoofdcheckout, en dat is waar de wees-scan en de
    # branch-listing tegen moeten draaien — ook wanneer dit script vanuit een
    # linked worktree wordt aangeroepen.
    root = repo_root or (Path(worktrees[0].path) if worktrees else Path.cwd())

    local_raw = run_git(["branch", "--format=%(refname:short)"], cwd=root)
    if local_raw is None:
        raise SystemExit(
            "git branch --format=%(refname:short) failed — cannot read the repo."
        )
    local = parse_branches(local_raw)

    remote_raw = run_git(["branch", "-r", "--format=%(refname:short)"], cwd=root)
    if remote_raw is None:
        raise SystemExit(
            "git branch -r --format=%(refname:short) failed — cannot read the repo."
        )
    remote = parse_branches(remote_raw, remote=True)
    try:
        slug = repo_slug()
    except ConfigError as exc:
        # rc 2, niet de rc 1 van SystemExit(<tekst>): onder `--only` betekent rc 1 al
        # "niet op te ruimen".
        print(f"worktree-audit: {exc}", file=sys.stderr)
        raise SystemExit(2) from exc
    if slug is None:
        print(
            "worktree-audit: PR lookup skipped — [forge] repo is not set, so every PR state is unknown",
            file=sys.stderr,
        )
    raw_pulls = run_gh_pulls(slug)
    pr_lookup_ok = raw_pulls is not None
    pulls = parse_pulls(raw_pulls or [])

    worktree_by_branch = {w.branch: w for w in worktrees if w.branch}
    probes = _probe_all_branches(local, remote, worktree_by_branch, pulls, root)

    sessions = _load_sessions(Path.home() / ".claude" / "sessions")

    projects_root = Path.home() / ".claude" / "projects"
    transcripts: dict[str, tuple[float, str]] = {}
    for worktree in worktrees:
        found = latest_transcript(projects_root, worktree.path)
        if found is not None:
            transcripts[worktree.path] = found

    detached_dirty = _detached_dirty(worktrees)

    orphans = find_orphans(root, worktrees)
    ports: dict[str, str] = {}
    for orphan in orphans:
        if ".port-registry" in orphan.dir_entries:
            try:
                ports = parse_port_registry(
                    (Path(orphan.path) / ".port-registry").read_text()
                )
            except OSError:
                ports = {}

    return build_rows(
        worktrees=worktrees,
        local_branches=local,
        remote_branches=remote,
        pulls=pulls,
        probes=probes,
        sessions=sessions,
        transcripts=transcripts,
        detached_dirty=detached_dirty,
        orphans=orphans,
        ports=ports,
        pr_lookup_ok=pr_lookup_ok,
    )


TABLE_HEADER = (
    "| worktree | branch | PR state | unpushed commits | dirty tree "
    "| last session | live? | verdict |"
)
TABLE_RULE = "|---|---|---|---|---|---|---|---|"


def _fmt_time(stamp: float | None) -> str:
    """Lokale tijd, tz-bewust opgebouwd: een naïeve fromtimestamp trekt ruff's
    DTZ006, en DTZ zit in de gelinte set."""
    if stamp is None:
        return "—"
    moment = datetime.datetime.fromtimestamp(stamp, tz=datetime.UTC).astimezone()
    return moment.strftime("%m-%d %H:%M")


def _row_line(row: Row, verdict: Verdict) -> str:
    location = row.worktree or row.orphan_dir
    worktree = Path(location).name if location else "—"
    pr = f"#{row.pr_number} {row.pr_state}" if row.pr_number else "—"
    unpushed = "?" if row.unpushed is None else str(row.unpushed)
    dirty = {True: "yes", False: "no", None: "?"}[row.dirty]
    return (
        f"| {worktree} | {row.name} | {pr} | {unpushed} | {dirty} "
        f"| {_fmt_time(row.last_session)} | {'LIVE' if row.live else '—'} "
        f"| {verdict.render()} |"
    )


def _judged(rows: list[Row], alive: Callable[[int], bool]) -> list[tuple[Row, Verdict]]:
    return [(r, judge(r, alive=alive)) for r in rows]


# Modifiers van een rij die nog gelockt is: git weigert `worktree remove` en
# `branch -D` erop, dus nooit in de plaklijsten.
_LOCKED_MODIFIERS = frozenset({"stale lock", "server-lock"})


def _safe_lists(
    pairs: list[tuple[Row, Verdict]],
) -> tuple[list[str], list[str], list[str]]:
    """De drie plaklijsten. Belangrijk 1: een rij met een stale lock is nog
    steeds gelockt — git weigert `worktree remove`/`branch -D` erop, dus zo'n
    rij wordt hier uitgesloten, ook al is haar verdict "safe to remove". De rij
    blijft wél in de tabel staan; alleen de plakbare commando's slaan hem over.

    Hetzelfde geldt voor een server-lock (#963, eindreview F1): het oordeel
    "safe to remove" klopt inhoudelijk, maar de lock van de sessie-server staat er
    nog, dus git weigert net zo goed. Die rijen krijgen hun eigen sectie.
    """
    eligible = [
        (r, v)
        for r, v in pairs
        if v.safe and not _LOCKED_MODIFIERS.intersection(v.modifiers)
    ]
    safe_worktrees = [r.worktree for r, _ in eligible if r.worktree]
    safe_local = [r.name for r, _ in eligible if r.local]
    safe_remote = [r.name for r, _ in eligible if r.remote]
    return safe_worktrees, safe_local, safe_remote


def _render_command_blocks(
    safe_worktrees: list[str], safe_local: list[str], safe_remote: list[str]
) -> list[str]:
    lines = ["Commands — read the table first; this script runs nothing.", ""]
    # Plakvolgorde, niet willekeurig: een branch die in een worktree is
    # uitgecheckt kan git niet verwijderen ("Cannot delete branch … checked
    # out at …"), dus worktree remove moet vóór branch -D. De omgekeerde
    # volgorde breekt een top-naar-onder paste halverwege.
    if safe_worktrees:
        lines.append("```bash")
        lines += [f'git worktree remove "{path}"' for path in safe_worktrees]
        lines.append("```")
    if safe_local:
        lines.append("```bash")
        lines.append("git branch -D " + " ".join(safe_local))
        lines.append("```")
    if safe_remote:
        lines.append("```bash")
        lines.append("git push origin --delete " + " ".join(safe_remote))
        lines.append("```")
    if not (safe_remote or safe_local or safe_worktrees):
        lines.append("Nothing with verdict ✅ — no commands.")
    return lines


def _render_stale_locked_section(pairs: list[tuple[Row, Verdict]]) -> list[str]:
    """Belangrijk 1: de rijen die `_safe_lists` bewust wegliet — een verweesde
    lock is informatie, geen toestemming. Nooit `git worktree unlock`
    aanbieden: ontgrendelen doet dit script nooit, ook niet als suggestie.
    """
    stale = [(r, v) for r, v in pairs if v.safe and "stale lock" in v.modifiers]
    if not stale:
        return []
    lines = [
        "",
        "Locked with a dead pid — release the lock by hand first, then run again:",
    ]
    lines += [
        f"  {r.worktree}   (lock-pid {r.lock_pid}, no longer in /proc)"
        for r, _ in stale
    ]
    return lines


def _render_server_locked_section(pairs: list[tuple[Row, Verdict]]) -> list[str]:
    """Eindreview F1 (#963): de rijen die `_safe_lists` wegliet omdat de
    sessie-server ze nog gelockt houdt. De sessie is voorbij (anders was het
    rang 2), maar de server-pid blijft weken levend (spike R1). Net als bij de
    stale lock: melden, nooit een ontgrendelcommando aanbieden.
    """
    held = [(r, v) for r, v in pairs if v.safe and "server-lock" in v.modifiers]
    if not held:
        return []
    lines = [
        "",
        (
            "Locked by the session server — session ended; release the lock by hand "
            "if you want it gone (or wait for the next server start), then "
            "run again:"
        ),
    ]
    lines += [
        f"  {r.worktree}   (server-pid "
        f"{'unknown' if r.lock_pid is None else r.lock_pid}, session ended)"
        for r, _ in held
    ]
    return lines


def _render_port_blocks_section(rows: list[Row]) -> list[str]:
    """Belangrijk 2: `port_block` werd gemeten en getest, maar nooit getoond.
    Dit is alleen de registratie uit `.port-registry` — welk proces de poort
    daadwerkelijk vasthoudt, wordt hier niet opgezocht (dat is #438); een lege
    lijst betekent dus niet "er draait niets".
    """
    ported = sorted((r for r in rows if r.port_block is not None), key=lambda r: r.name)
    if not ported:
        return []
    lines = [
        "",
        "Port blocks (registered in worktrees/.port-registry, read-only):",
    ]
    lines += [
        f"  {r.name}  port {r.port_block}  {r.worktree or r.orphan_dir or 'no worktree'}"
        for r in ported
    ]
    lines.append(
        "The process that actually holds the port is not looked up here "
        "— an empty list above does not mean that nothing runs."
    )
    return lines


def render_cleanup(rows: list[Row], *, alive: Callable[[int], bool] = pid_alive) -> str:
    pairs = sorted(_judged(rows, alive), key=lambda p: (p[1].rank, p[0].name))
    lines = [TABLE_HEADER, TABLE_RULE]
    lines += [_row_line(row, verdict) for row, verdict in pairs]
    lines.append("")

    safe_worktrees, safe_local, safe_remote = _safe_lists(pairs)
    lines += _render_command_blocks(safe_worktrees, safe_local, safe_remote)
    lines += _render_stale_locked_section(pairs)
    lines += _render_server_locked_section(pairs)
    lines += _render_port_blocks_section(rows)
    return "\n".join(lines) + "\n"


def render_sessions(
    rows: list[Row], *, alive: Callable[[int], bool] = pid_alive
) -> str:
    pairs = [(r, v) for r, v in _judged(rows, alive) if r.session_id]
    # Live sessies eerst, ook zonder transcript (last_session=None): een live
    # sessie zonder .jsonl-bestand is relevanter dan een dode sessie van
    # lang geleden, en mag niet onderaan wegzakken op een ontbrekende tijd.
    pairs.sort(key=lambda p: (p[0].live, p[0].last_session or 0.0), reverse=True)
    lines = [
        "| last session | live? | worktree | resume | verdict |",
        "|---|---|---|---|---|",
    ]
    for row, verdict in pairs:
        worktree = Path(row.worktree).name if row.worktree else "—"
        lines.append(
            f"| {_fmt_time(row.last_session)} | {'LIVE' if row.live else 'dead'} "
            f"| {worktree} | `claude --resume {row.session_id}` | {verdict.render()} |"
        )
    if len(lines) == 2:
        lines.append("| — | — | — | no sessions found | — |")
    return "\n".join(lines) + "\n"


def render_origin(rows: list[Row], *, alive: Callable[[int], bool] = pid_alive) -> str:
    return render_cleanup([r for r in rows if r.remote], alive=alive)


def render_only(
    rows: list[Row],
    branch: str,
    *,
    cwd: Path,
    alive: Callable[[int], bool] = pid_alive,
) -> tuple[str, bool]:
    """De view voor lighthouse-close-issue: één rij plus `cleanup: yes|no`.

    Opruimbaar = verdict "safe to remove", niet gelockt, en de eigen cwd staat niet
    in de worktree — git weigert de eerste twee, en de derde zaagt de tak af
    waar de sessie op zit. Bij `yes` volgen hoogstens twee commando's, in
    plakvolgorde; nooit `push origin --delete`: de remote branch is de zaak van
    de merge, niet van het sluiten. Dit script voert ze niet uit.
    """
    match = [r for r in rows if r.name == branch]
    if not match:
        return f"cleanup: no — branch {branch} not found\n", False
    if len(match) > 1:
        # Een losgekoppelde worktree of wees-map draagt zijn mapnaam als naam,
        # en die kan gelijk zijn aan de branch. Kiezen zou de andere rij — met
        # misschien een lopende sessie — stil overslaan.
        pairs = _judged(match, alive)
        lines = [TABLE_HEADER, TABLE_RULE]
        lines += [_row_line(r, v) for r, v in pairs]
        reason = (
            f"{len(match)} rows named {branch} (detached worktree or orphan dir)"
        )
        lines += ["", f"cleanup: no — {reason}"]
        return "\n".join(lines) + "\n", False
    target = match[0]
    ((_, verdict),) = _judged([target], alive)
    lines = [TABLE_HEADER, TABLE_RULE, _row_line(target, verdict), ""]

    wt = Path(target.worktree) if target.worktree else None
    if not verdict.safe:
        reason = verdict.render()
    elif _LOCKED_MODIFIERS.intersection(verdict.modifiers):
        reason = f"{verdict.render()} (locked)"
    elif wt is not None and (wt == cwd or wt in cwd.parents):
        reason = f"own cwd is in {wt}"
    elif wt is None and not target.local:
        reason = "nothing local to clean up"
    else:
        reason = None

    if reason is not None:
        lines.append(f"cleanup: no — {reason}")
        return "\n".join(lines) + "\n", False
    lines.append("cleanup: yes")
    if wt is not None:
        lines.append(f'git worktree remove "{target.worktree}"')
    if target.local:
        lines.append(f"git branch -D {target.name}")
    return "\n".join(lines) + "\n", True


def issue_branches(rows: list[Row], nr: int, pr_heads: list[str]) -> list[str]:
    """Branches van issue `nr`: PR-heads plus rijen met prefix `issue-<nr>-`.

    Het streepje hoort bij de prefix: `issue-3-` mag `issue-32-x` niet pakken.
    Gesorteerd, zonder dubbelen.
    """
    prefix = f"issue-{nr}-"
    found = {r.name for r in rows if r.name.startswith(prefix)}
    return sorted(found | set(pr_heads))


def render_issue(
    rows: list[Row],
    nr: int,
    pr_heads: list[str] | None,
    *,
    cwd: Path,
    alive: Callable[[int], bool] = pid_alive,
) -> str:
    """Per branch van issue `nr` een blok: `branch: <b>` plus de `render_only`-uitvoer.

    Geen tweede ladder: `render_only` beslist. Eén aanvulling: een branch waarvan
    de rij geen gemergde PR heeft, is nooit opruimbaar, ook als de ladder "safe to
    remove" zegt (een lokale `issue-<nr>-`-branch zonder PR). `pr_heads is None`
    betekent dat de PR-lookup mislukte; dan is er geen enkel signaal, en geen
    signaal is geen ja.
    """
    if pr_heads is None:
        return f"cleanup: no — PR lookup for issue #{nr} failed\n"
    branches = issue_branches(rows, nr, pr_heads)
    if not branches:
        return f"cleanup: no — no branches found for issue #{nr}\n"
    blocks: list[str] = []
    for branch in branches:
        text, ok = render_only(rows, branch, cwd=cwd, alive=alive)
        match = [r for r in rows if r.name == branch]
        if ok and not (len(match) == 1 and match[0].pr_state == "merged"):
            # Alles vanaf `cleanup: yes` (ook de commando's eronder) vervalt.
            lines = text.splitlines()
            head = lines[: lines.index("cleanup: yes")]
            text = "\n".join(head + ["cleanup: no — no merged PR"]) + "\n"
        blocks.append(f"branch: {branch}\n{text}")
    return "\n".join(blocks)


def render_kpi(rows: list[Row], *, alive: Callable[[int], bool] = pid_alive) -> str:
    pairs = _judged(rows, alive)
    # Minor 6: de hoofdcheckout heeft ook `worktree` gezet, maar de skill
    # labelt dit getal "Worktrees onder `.claude/worktrees/`" — dus telt
    # alleen mee wat daar ook echt onder staat.
    worktrees = sum(
        1 for r, _ in pairs if r.worktree and WORKTREE_DIRNAME in r.worktree
    )
    safe = sum(1 for _, v in pairs if v.safe)
    remote = sum(1 for r, _ in pairs if r.remote)
    warn = sum(1 for _, v in pairs if v.rank in (4, 7))
    return f"worktrees={worktrees} safe={safe} remote_branches={remote} warn={warn}\n"


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(
        description="Audit worktrees, branches, PR state and sessions. Changes nothing."
    )
    view = parser.add_mutually_exclusive_group()
    view.add_argument(
        "--cleanup",
        action="store_true",
        help="full table + command blocks (default)",
    )
    view.add_argument(
        "--sessions",
        action="store_true",
        help="claude --resume per row, sorted by last session",
    )
    view.add_argument("--origin", action="store_true", help="remote rows only")
    view.add_argument("--kpi", action="store_true", help="the KPI numbers, one line")
    view.add_argument(
        "--only",
        metavar="BRANCH",
        help="one branch + `cleanup: yes|no` (exit 0 only on yes), for lighthouse-close-issue",
    )
    view.add_argument(
        "--issue",
        type=int,
        metavar="NR",
        help="all branches of an issue, one block each with `cleanup: yes|no` "
        "(exit 2 if the PR lookup fails), for lighthouse-close-issue",
    )
    parser.add_argument(
        "--repo",
        type=Path,
        default=None,
        help="path to the main checkout (default: from git worktree list)",
    )
    args = parser.parse_args(argv)

    rows = collect(args.repo)
    if args.only is not None:
        text, ok = render_only(rows, args.only, cwd=Path.cwd().resolve())
        print(text, end="")
        return 0 if ok else 1
    if args.issue is not None:
        pr_heads = run_gh_issue_pr_heads(repo_slug(), args.issue)
        print(
            render_issue(rows, args.issue, pr_heads, cwd=Path.cwd().resolve()), end=""
        )
        return 2 if pr_heads is None else 0
    if args.sessions:
        print(render_sessions(rows), end="")
    elif args.origin:
        print(render_origin(rows), end="")
    elif args.kpi:
        print(render_kpi(rows), end="")
    else:
        print(render_cleanup(rows), end="")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
