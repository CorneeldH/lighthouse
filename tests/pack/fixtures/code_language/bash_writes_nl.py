"""`bash-write-gates` (#871): wortels, git-records, reflog, de per-sessie
ledger, `pre`, en `post` met de dispatch naar de bestaande gates.

Zie `docs/developer/planning/specs/2026-09-26-bash-write-gates-design.md`,
secties *Datastroom* (Wortels, post bron 1/2), *Staat*, *Lock*, *pre*, *post*,
*De pool*, *Dispatch*, *Uitvoer* en *Foutafhandeling* voor de semantiek.
Aangeroepen als `python3 bash_writes.py pre|post` met de hookpayload op
stdin, via de wrapper `.claude/hooks/lighthouse/bash-write-gates.sh`. De gates
staan in `<repo-root>/.claude/hooks/lighthouse/` (`config.repo_root()`); een gate
die er niet staat, omdat zijn taalpakket ontbreekt, meldt post per bestand
zonder te blokkeren (besluit 19).

Twee omgevingsvariabelen zijn test-only naden: `LIGHTHOUSE_BASH_GATES_DEADLINE`
(seconden, override van de post-deadline, default 90, gelezen door
`run_post`) en `LIGHTHOUSE_BASH_GATES_START_NS` (override van de pre-starttijd, in
nanoseconden, gelezen door `run_pre`).
"""

from __future__ import annotations

import contextlib
import fcntl
import hashlib
import json
import os
import re
import subprocess
import sys
import tempfile
import time
from collections.abc import Iterator
from pathlib import Path
from typing import NamedTuple

# Run as a file on its path: put `.lighthouse/lib` first, so `lighthouse_lib`
# imports from this installation.
if not __package__:
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from lighthouse_lib import config, git_invocations, repo_identity
from lighthouse_lib.deadline import Interrupted, run_until

# De map van de gates, onder de repo-root.
HOOK_DIR = Path(".claude/hooks/lighthouse")

GATES: dict[str, str] = {
    ".sh": "shellcheck.sh",
    ".bash": "shellcheck.sh",
    ".py": "ruff-format.sh",
    ".md": "worktree-linkcheck.sh",
    ".markdown": "worktree-linkcheck.sh",
}

# Het pakket dat elke gate levert; ontbreekt de gate, dan noemt de melding het.
GATE_PACKS: dict[str, str] = {
    "shellcheck.sh": "shell",
    "ruff-format.sh": "python",
    "worktree-linkcheck.sh": "core",
}


def gate_path(root: str, ext: str) -> Path | None:
    """De gate van extensie `ext` onder `root`; `None` als de extensie geen gate heeft."""
    gate = GATES.get(ext)
    return None if gate is None else Path(root) / HOOK_DIR / gate

# Fallback-tokenizer voor een Unparseable commando (open quote): geen echte
# shell-parse meer mogelijk, dus een ruwe woordsplitsing op wat sowieso nooit
# een pad-teken is.
_TOKEN_RE = re.compile(r"[^\s'\"=;&|<>()]+")

# `%gd` van `git reflog --format=...%gd...` is `HEAD@{<epoch>}` met
# `--date=unix`.
_EPOCH_RE = re.compile(r"HEAD@\{(\d+)\}")

# Sentinel in de sessiemap: post verving ooit een onleesbare ledger. Zolang
# hij er is, seedt pre geen ontbrekende wortels (zie `Ledger`, `_run_pre`).
_RESET_NAME = "ledger.reset"

# Markers ouder dan dit worden door `prune_markers` opgeruimd (spec: 3600 s).
_MARKER_MAX_AGE_S = 3600.0

# Pre-timeout is 5s (spec); de deadline voor de git-reads erbinnen laat marge
# voor het schrijven van de marker zelf.
_PRE_DEADLINE_S = 4.0


class GitError(Exception):
    """Een git-leesactie faalde: niet-nul exit, timeout, of het proces start niet.

    `returncode` is `None` bij een timeout of een `OSError` (het proces is
    dan nooit geëindigd met een exitcode); de exception-boodschap is de
    stderr-tekst van git, of de vaste tekst `git read past the deadline`
    bij een timeout.
    """

    def __init__(self, root: str, returncode: int | None, message: str) -> None:
        super().__init__(message)
        self.root = root
        self.returncode = returncode


def git(root: str, *args: str, deadline: float) -> bytes:
    """`git --no-optional-locks -C root <args>`, stdout als bytes.

    Elke niet-nul exit, `TimeoutExpired` of `OSError` wordt een `GitError`.
    De timeout van het onderliggende `subprocess.run` is wat er van
    `deadline` nog rest, met een bodem van 0.1s zodat een al verstreken
    deadline niet als een negatieve of nul-timeout bij `subprocess` belandt.
    """
    argv = ["git", "--no-optional-locks", "-C", root, *args]
    timeout = max(0.1, deadline - time.time())
    try:
        result = subprocess.run(argv, capture_output=True, timeout=timeout, check=False)
    except subprocess.TimeoutExpired as exc:
        raise GitError(root, None, "git read past the deadline") from exc
    except OSError as exc:
        raise GitError(root, None, str(exc)) from exc
    if result.returncode != 0:
        message = result.stderr.decode(errors="replace").strip()
        raise GitError(root, result.returncode, message)
    return result.stdout


def _root_of(path: str | None) -> str | None:
    """Repowortel van `path`: dichtstbijzijnde bestaande voorouder, dan `repo_of`."""
    if path is None:
        return None
    ancestor = git_invocations.existing_ancestor(path)
    if ancestor is None:
        return None
    return git_invocations.repo_of(ancestor)


def _resolved_path(word: str, literal: bool, tilde: bool, base: str) -> str | None:
    """`resolve(word, ...)`, dan absoluut maken tegen `base` als dat nog niet zo is."""
    resolved = git_invocations.resolve(word, literal, tilde)
    if resolved is None:
        return None
    if os.path.isabs(resolved):
        return os.path.normpath(resolved)
    return os.path.normpath(os.path.join(base, resolved))


def derive_roots(command: str, cwd: str) -> set[str]:
    """De wortelregels uit de spec (Datastroom, *Wortels*).

    Bronnen: de payload-`cwd`; per `Step` van `git_invocations.walk` de
    mapstand `dirs["here"]`; bij het werkwoord `git` het `git_target`; elk
    woord met een `/` of een `~`-begin, herleid tegen de mapstand van dat
    moment (of `cwd` zonder mapstand). Bij een `Unparseable` commando (open
    quote) valt dit terug op de cwd plus een ruwe tokenisering van het hele
    commando, tegen dezelfde padregel. Elke wortel gaat door
    `os.path.realpath`. Niet gefilterd op project — dat doet `project_filter`.

    `step.dirs` is de LEVENDE mapstand van `walk` (zie `git_invocations.Step`):
    buiten een sub-shell delen alle Steps hetzelfde dict-object, dat na elke
    `yield` verder muteert zodra de consument om de volgende Step vraagt. Elke
    Step moet dus VERWERKT worden binnen deze `for`-lus, vóórdat de lus zelf
    naar de volgende Step doorschuift — niet via `list(walk(...))` en dan pas
    lezen, want dan zien alle niet door een sub-shell gescheiden Steps de
    EINDstand van de hele wandeling in plaats van hun eigen moment.
    """
    roots: set[str] = set()

    def add(path: str | None) -> None:
        root = _root_of(path)
        if root is not None:
            roots.add(root)

    add(cwd)
    try:
        for step in git_invocations.walk(command, cwd):
            here = step.dirs["here"]
            add(here)
            if git_invocations.basename(step.words[step.idx]) == "git":
                target, _raw, _from_new_dir, _action_idx = git_invocations.git_target(
                    step
                )
                add(target)
            base = here if here is not None else cwd
            for k, word in enumerate(step.words):
                if "/" in word or word.startswith("~"):
                    add(_resolved_path(word, step.literal[k], step.tildes[k], base))
    except git_invocations.Unparseable:
        for token in _TOKEN_RE.findall(command):
            if "/" in token or token.startswith("~"):
                add(_resolved_path(token, True, token.startswith("~"), cwd))

    return {os.path.realpath(root) for root in roots}


def project_filter(
    roots: set[str], project_dir: str, *, deadline: float
) -> tuple[set[str], list[str]]:
    """Beperk `roots` tot wortels met dezelfde `--git-common-dir` als `project_dir`,
    of met dezelfde `origin` (`repo_identity.same_origin`, #36).

    Letterlijk `rev-parse --path-format=absolute --git-common-dir`, door
    `os.path.realpath` — zonder die vlag geeft de hoofdcheckout een relatief
    `.git` en een gelinkte worktree een absoluut pad, en zou de worktree
    onterecht wegvallen (`pre-commit-tests.sh:182`). Een wortel waarvan die
    aanroep faalt komt als foutregel terug, niet als `GitError`; faalt het
    voor `project_dir` zelf, dan gooit deze functie wel door.
    """

    def common_dir(root: str) -> str:
        out = git(
            root,
            "rev-parse",
            "--path-format=absolute",
            "--git-common-dir",
            deadline=deadline,
        )
        return os.path.realpath(out.decode(errors="replace").strip())

    ref_common = common_dir(project_dir)
    kept: set[str] = set()
    errors: list[str] = []
    for root in roots:
        try:
            common = common_dir(root)
        except GitError as exc:
            errors.append(f"COULD NOT CHECK: {root} ({exc})")
            continue
        if common == ref_common:
            kept.add(root)
            continue
        # Een worktree van een aparte clone (forge) met dezelfde `origin`
        # hoort er ook bij (#36).
        try:
            same, _reason = repo_identity.same_origin(
                project_dir, root, timeout=deadline - time.time()
            )
        except repo_identity.OriginError as exc:
            errors.append(f"COULD NOT CHECK: {root} ({exc})")
            continue
        if same:
            kept.add(root)
    return kept, errors


def head_of(root: str, *, deadline: float) -> str:
    """De huidige `HEAD`-sha, of `"unborn"` in een repo zonder commits.

    `rev-parse --verify -q HEAD` slaagt normaal met de sha. Faalt hij met
    exact returncode 1, dan is dat een lege repo: `symbolic-ref -q HEAD`
    bevestigt dat er wel een branch-naam is (alleen nog geen commit) en het
    resultaat is `"unborn"`. Elke andere `GitError` — een andere returncode,
    `None` (timeout/OSError), of een falende `symbolic-ref` — wordt
    doorgegooid: dat is een kapotte `.git`, geen lege repo.
    """
    try:
        out = git(root, "rev-parse", "--verify", "-q", "HEAD", deadline=deadline)
    except GitError as exc:
        if exc.returncode == 1:
            git(root, "symbolic-ref", "-q", "HEAD", deadline=deadline)
            return "unborn"
        raise
    return out.decode(errors="replace").strip()


def parse_porcelain(data: bytes) -> tuple[set[str], set[str]]:
    """`git status --porcelain -z -uall`-uitvoer naar `(gewijzigd, verwijderd)`.

    Recordgrammatica: `XY pad\\0`, bij een `R`/`C` in X of Y gevolgd door nog
    één veld met het oude pad. Een `D` in X of Y betekent verwijderd. Het oude
    pad van een `R` telt als verwijderd: het staat nog in HEAD maar niet meer
    op schijf, en het nettofilter van post zou het anders als `KON NIET
    CONTROLEREN` melden in plaats van laten afvallen. Het oude pad van een
    `C` (copy) bestaat nog en wordt overgeslagen. Geen van beide is ooit
    kandidaat. Paden komen via `os.fsdecode`, nooit `.decode()` of
    `splitlines()`: een bestandsnaam met een newline of niet-ASCII-bytes mag
    niet uiteenvallen.
    """
    changed: set[str] = set()
    removed: set[str] = set()
    fields = data.split(b"\0")
    if fields and fields[-1] == b"":
        fields.pop()
    records = iter(fields)
    for field in records:
        if len(field) < 3:
            continue
        status = field[:2]
        path = os.fsdecode(field[3:])
        if b"R" in status or b"C" in status:
            old = next(records, None)
            if old is not None and b"R" in status:
                removed.add(os.fsdecode(old))
        if b"D" in status:
            removed.add(path)
        else:
            changed.add(path)
    return changed, removed


def parse_diff_tree(data: bytes) -> tuple[set[str], set[str]]:
    """`diff-tree --name-status -z`-uitvoer naar `(gewijzigd, verwijderd)`.

    Records zijn `status\\0pad\\0`-paren (met `--no-renames` is er geen
    apart oud-pad-veld). Bij een combined diff (`-c`, een merge) staat er
    één statusletter per ouder in hetzelfde veld (`"MM"`); een `D` ergens
    daarin betekent verwijderd.
    """
    changed: set[str] = set()
    removed: set[str] = set()
    fields = data.split(b"\0")
    if fields and fields[-1] == b"":
        fields.pop()
    records = iter(fields)
    for status in records:
        path_bytes = next(records, None)
        if path_bytes is None:
            break
        path = os.fsdecode(path_bytes)
        if b"D" in status:
            removed.add(path)
        else:
            changed.add(path)
    return changed, removed


class ReflogEntry(NamedTuple):
    """Eén regel van `git reflog --format='%H%x1f%gd%x1f%gs'`."""

    sha: str
    epoch: int
    subject: str


def parse_reflog(text: str) -> list[ReflogEntry]:
    """`%H\\x1f%gd\\x1f%gs`-regels naar `ReflogEntry`'s, nieuwste eerst.

    De epoch komt uit `%gd` (`HEAD@{<epoch>}` met `--date=unix`), niet uit
    een los datumveld: dat is precies wat `%gd` al bevat.

    Splitst op `"\\n"`, nooit `.splitlines()`: die laatste breekt ook op
    `\\x1c`–`\\x1e`, `\\x0c`, `\\x85`, U+2028 en U+2029, en git bewaart die
    letterlijk in `%gs` als het commit-subject ze bevat. Zo'n regelfragment
    mist dan zijn `\\x1f`-scheidingstekens en de unpack hieronder crasht met
    een kale `ValueError` in plaats van dat de aanroeper het als een
    `GitError` (`KON NIET CONTROLEREN`) kan behandelen.
    """
    entries: list[ReflogEntry] = []
    for line in text.split("\n"):
        if not line:
            continue
        sha, gd, subject = line.split("\x1f", 2)
        match = _EPOCH_RE.search(gd)
        epoch = int(match.group(1)) if match else 0
        entries.append(ReflogEntry(sha, epoch, subject))
    return entries


def _is_merge_commit(subject: str) -> bool:
    return subject.startswith("commit (merge)")


def _commits_until_stop(
    entries: list[ReflogEntry], stop: str
) -> tuple[list[tuple[str, bool]], list[str]]:
    """`stop`-tak van `commits_to_check`: loop tot de entry met `sha == stop`.

    Een entry met een leeg subject (de eerste entry van een nieuwe worktree)
    is geen beweging: hij zet geen melding en blokkeert de melding van een
    latere echte beweging niet.
    """
    collected: list[tuple[str, bool]] = []
    moved_subject: str | None = None
    for entry in entries:
        if entry.sha == stop:
            notes = (
                [f"HEAD moved without a commit ({moved_subject})"]
                if moved_subject
                else []
            )
            return collected, notes
        if entry.subject.startswith("commit"):
            collected.append((entry.sha, _is_merge_commit(entry.subject)))
        elif moved_subject is None and entry.subject:
            moved_subject = entry.subject
    return [], [f"reflog gap: ledger HEAD {stop} not found"]


def _commits_from_unborn(
    entries: list[ReflogEntry],
) -> tuple[list[tuple[str, bool]], list[str]]:
    """`stop == "unborn"`-tak: alle commit-entries, geen gat-melding mogelijk."""
    collected = [
        (entry.sha, _is_merge_commit(entry.subject))
        for entry in entries
        if entry.subject.startswith("commit")
    ]
    return collected, []


def _commits_since(
    entries: list[ReflogEntry], since: int | None
) -> tuple[list[tuple[str, bool]], list[str]]:
    """`stop is None`-tak: terugval op reflogtijd, commit-entries met epoch `>= since`.

    `since` is in hele epoch-SECONDEN, zoals de reflog-epoch van
    `--date=unix`; de aanroeper rondt de pre-starttijd (nanoseconden) naar
    BENEDEN af — naar boven zou een commit uit dezelfde seconde laten vallen.
    """
    collected: list[tuple[str, bool]] = []
    for entry in entries:
        if since is not None and entry.epoch < since:
            break
        if entry.subject.startswith("commit"):
            collected.append((entry.sha, _is_merge_commit(entry.subject)))
    return collected, []


def commits_to_check(
    entries: list[ReflogEntry], stop: str | None, since: int | None
) -> tuple[list[tuple[str, bool]], list[str]]:
    """Welke reflog-commits horen bij deze wandeling: `([(sha, is_merge)], meldingen)`.

    - `stop` gezet (niet `"unborn"`): loop tot de eerste entry met
      `sha == stop`. Entries met een subject dat met `commit` begint gaan
      mee; is er ook een entry die geen commit is (`checkout`, `pull`, …),
      dan één melding voor de eerste zo'n beweging. Komt `stop` nooit voor
      in `entries`, dan is dat een reflog-gat: lege lijst plus melding.
    - `stop == "unborn"`: alle commit-entries tellen mee, geen gat-melding
      mogelijk — er was nog geen eerdere HEAD om naar te zoeken.
    - `stop is None`: terugval op reflogtijd. Commit-entries tellen mee
      zolang hun epoch `>= since` (nieuwste eerst, dus dit stopt zodra de
      tijdgrens gepasseerd is). `since` is in hele epoch-seconden, naar
      beneden afgerond van de pre-starttijd (`start_ns // 10**9`).

    Met `stop` gezet (niet `"unborn"`) ÉN een `since` is het resultaat de
    vereniging van beide wandelingen, ontdubbeld op sha (stop-wandeling
    eerst). De stop-wandeling alleen is blind zodra de call HEAD terugbrengt
    naar, of langs, de ledger-sha: `switch -c x; commit; switch main` begint
    de reflog al met die sha, en een `merge --ff-only` terug stopt vóór de
    commit. Ook een reflog-gat (verse worktree op een oud pad) houdt zo zijn
    melding, maar niet langer zijn blinde vlek. Alleen commit-entries tellen,
    dus een checkout of pull levert nog steeds niets op. Beide wandelingen
    zijn een prefix van `entries`, dus de vereniging blijft nieuwste-eerst.

    `is_merge` is `subject.startswith("commit (merge)")`.
    """
    if stop is not None and stop != "unborn":
        collected, notes = _commits_until_stop(entries, stop)
        if since is None:
            return collected, notes
        known = {sha for sha, _merge in collected}
        extra, _none = _commits_since(entries, since)
        for sha, is_merge in extra:
            if sha not in known:
                known.add(sha)
                collected.append((sha, is_merge))
        return collected, notes
    if stop == "unborn":
        return _commits_from_unborn(entries)
    return _commits_since(entries, since)


def read_reflog(root: str, *, deadline: float) -> list[ReflogEntry]:
    """`git reflog --date=unix --format='%H%x1f%gd%x1f%gs'`, geparsed."""
    data = git(
        root,
        "reflog",
        "--date=unix",
        "--format=%H%x1f%gd%x1f%gs",
        deadline=deadline,
    )
    return parse_reflog(data.decode(errors="replace"))


def diff_commit(
    root: str, sha: str, is_merge: bool, *, deadline: float
) -> tuple[set[str], set[str]]:
    """Wat `sha` wijzigde: `(gewijzigd, verwijderd)`, repo-relatief.

    Een gewone commit gebruikt `--root` (zodat een root-commit ook een diff
    geeft); een merge gebruikt de combined diff (`-c`), die alleen paden
    noemt die van ELKE ouder verschillen — conflictoplossingen, niet wat de
    andere tak binnenbracht.
    """
    args = [
        "diff-tree",
        "-z",
        "-r",
        "--no-commit-id",
        "--name-status",
        "--no-renames",
        "-c" if is_merge else "--root",
        sha,
    ]
    data = git(root, *args, deadline=deadline)
    return parse_diff_tree(data)


# --- Staat (taak 6): handtekeningen, sessiemap, ledger, marker, pre --------

Signature = tuple[int, int, int, int]


def signature(path: str) -> Signature | None:
    """`(st_mtime_ns, st_ctime_ns, st_ino, st_size)` van `path`, `None` als hij er niet is."""
    try:
        st = os.stat(path)
    except OSError:
        return None
    return (st.st_mtime_ns, st.st_ctime_ns, st.st_ino, st.st_size)


def state_dir(session_id: str) -> Path:
    """`${XDG_RUNTIME_DIR:-/tmp}/lighthouse-bash-gates/<session_id>/`, aangemaakt met modus `0700`.

    Een `session_id` met een `/` of gelijk aan `..` zou de statusmap kunnen
    ontsnappen; die wordt geweigerd. `mkdir(..., parents=True)` maakt
    ontbrekende oudermappen (`lighthouse-bash-gates/` zelf) met de standaardmodus
    aan — alleen de sessiemap zelf moet `0700` zijn, en die modus wordt na
    het aanmaken nog eens expliciet gezet: een `umask` kan de modus van
    `mkdir` zelf beïnvloeden.
    """
    if "/" in session_id or session_id == "..":
        raise ValueError(f"invalid session_id: {session_id!r}")
    base = Path(os.environ.get("XDG_RUNTIME_DIR") or "/tmp") / "lighthouse-bash-gates"
    path = base / session_id
    path.mkdir(mode=0o700, parents=True, exist_ok=True)
    os.chmod(path, 0o700)
    return path


def marker_key(payload: dict) -> str:
    """`tool_use_id`, anders `session_id + "-" + sha256(tool_input.command)[:16]`.

    Een `/` in het resultaat zou de markernaam buiten de statusmap laten
    vallen (`<state>/<key>.json`); die wordt vervangen, verder wordt de
    sleutel niet gesaneerd.
    """
    tool_use_id = payload.get("tool_use_id")
    if tool_use_id:
        key = str(tool_use_id)
    else:
        command = payload.get("tool_input", {}).get("command", "")
        digest = hashlib.sha256(command.encode("utf-8")).hexdigest()[:16]
        key = f"{payload.get('session_id', '')}-{digest}"
    return key.replace("/", "_")


class Ledger:
    """Eén sessie-ledger: `roots` (pad → wortelregel) plus `unreadable`.

    `unreadable` is `True` als `ledger.json` er wél was maar geen geldige
    JSON bevatte of niet de vorm `{wortel: {...}}` had — een lege maar
    bruikbare ledger, met een signaal voor de aanroeper dat er iets kapot
    was. `locked_ledger` schrijft nooit terug zolang `unreadable` bij het
    verlaten nog `True` is (zie daar): een aanroeper die de kapotte ledger
    bewust wil vervangen, zet `unreadable = False` vóórdat de `with` sluit.

    Wie dat doet (alleen post, in `_claim`), maakt ook de sentinel
    `<state>/ledger.reset` aan: de verse ledger kent alleen de wortels die
    die post wandelde, en een wortel die ontbreekt kan eerdere sessie-writes
    hebben. Zolang de sentinel bestaat (de rest van de sessie) seedt pre geen
    ontbrekende wortel; post valt er dan één keer voor terug en neemt hem op.
    Eén keer overcontroleren per wortel, nooit blind.
    """

    def __init__(
        self, roots: dict[str, dict] | None = None, unreadable: bool = False
    ) -> None:
        self.roots: dict[str, dict] = roots if roots is not None else {}
        self.unreadable = unreadable


def _read_ledger(path: Path) -> Ledger:
    try:
        raw = path.read_text(encoding="utf-8")
    except OSError:
        return Ledger()
    try:
        data = json.loads(raw)
    except json.JSONDecodeError:
        return Ledger(unreadable=True)
    if not isinstance(data, dict):
        return Ledger(unreadable=True)
    return Ledger(roots=data)


def _atomic_write_json(directory: Path, target: Path, data: object) -> None:
    """Schrijf `data` als JSON naar `target`, atomisch via een tempfile + `os.replace`."""
    fd, tmp_name = tempfile.mkstemp(dir=str(directory), prefix=".tmp-", suffix=".json")
    try:
        with os.fdopen(fd, "w", encoding="utf-8") as handle:
            json.dump(data, handle)
        os.replace(tmp_name, str(target))
    except BaseException:
        with contextlib.suppress(FileNotFoundError):
            os.unlink(tmp_name)
        raise


@contextlib.contextmanager
def locked_ledger(state: Path) -> Iterator[Ledger]:
    """Lees/schrijf `ledger.json` onder een exclusieve `flock` op `ledger.lock`.

    De lock omvat alleen het lezen, de body van de `with` en het
    terugschrijven — nooit een gate: pre (timeout 5s) mag niet op een
    lopende post wachten. Het terugschrijven gebeurt atomisch (tempfile in
    dezelfde map, dan `os.replace`) en alleen als de body zonder exception
    eindigt ÉN `ledger.unreadable` op dat moment `False` is; raakt de body
    een exception, of bleef `unreadable` `True`, dan blijft `ledger.json`
    ongewijzigd.

    Dat tweede is bewust: een kapotte `ledger.json` mag pre niet stilzwijgend
    vervangen door een verse, lege ledger — dan verliest de spec's
    "Ledger onleesbaar"-terugval (bron 1 zonder `seen`-filter, plus melding)
    zijn signaal, en wat deze sessie al eerder schreef wordt ongezien
    baseline. Wil een aanroeper de kapotte ledger toch bewust vervangen (de
    latere `post`-terugval doet dat), dan zet die `ledger.unreadable = False`
    binnen de `with` vóór het einde van de body, en maakt onder dezelfde
    lock de sentinel `ledger.reset` aan (zie `Ledger`).
    """
    state = Path(state)
    lock_path = state / "ledger.lock"
    ledger_path = state / "ledger.json"
    lock_fd = os.open(str(lock_path), os.O_CREAT | os.O_RDWR, 0o600)
    try:
        fcntl.flock(lock_fd, fcntl.LOCK_EX)
        try:
            ledger = _read_ledger(ledger_path)
            clean_exit = False
            try:
                yield ledger
                clean_exit = True
            finally:
                # Alleen terugschrijven bij een schone yield ÉN een leesbare
                # ledger: een `except`/`else` hier zou alleen maar re-raisen
                # (TRY203), dus de vlag draagt het onderscheid.
                if clean_exit and not ledger.unreadable:
                    _atomic_write_json(state, ledger_path, ledger.roots)
        finally:
            fcntl.flock(lock_fd, fcntl.LOCK_UN)
    finally:
        os.close(lock_fd)


def seed_root(root: str, start_ns: int, *, deadline: float) -> dict:
    """De nullijn voor `root`: al vuile gate-bestanden van vóór `start_ns`, plus `head`.

    Bron is `git status --porcelain -z -uall`, beperkt tot paden met een
    gate-extensie (`GATES`) die nog bestaan én waarvan `ctime_ns < start_ns`
    — een pad dat op of na de starttijd van pre is aangeraakt, kan al door
    het commando zelf geschreven zijn (liep pre uit, dan draait het commando
    al) en hoort niet in de nullijn. Verwijderde paden (`D` in
    `parse_porcelain`) komen niet in `changed` terecht en dus nooit in
    `seen`.
    """
    data = git(root, "status", "--porcelain", "-z", "-uall", deadline=deadline)
    changed, _deleted = parse_porcelain(data)
    seen: dict[str, list[int]] = {}
    for rel in changed:
        if os.path.splitext(rel)[1] not in GATES:
            continue
        sig = signature(os.path.join(root, rel))
        if sig is None or sig[1] >= start_ns:
            continue
        seen[rel] = list(sig)
    return {
        "seen": seen,
        "head": head_of(root, deadline=deadline),
        "pending": [],
        "claims": {},
    }


def write_marker(state: Path, key: str, start_ns: int, roots: list[str]) -> None:
    """`<state>/<key>.json` <- `{"start_ns": start_ns, "roots": roots}`, atomisch."""
    state = Path(state)
    target = state / f"{key}.json"
    _atomic_write_json(state, target, {"start_ns": start_ns, "roots": list(roots)})


def read_marker(state: Path, key: str) -> dict | None:
    """De marker van `key`, of `None` als hij ontbreekt of geen geldige JSON-dict is."""
    path = Path(state) / f"{key}.json"
    try:
        raw = path.read_text(encoding="utf-8")
    except OSError:
        return None
    try:
        data = json.loads(raw)
    except json.JSONDecodeError:
        return None
    return data if isinstance(data, dict) else None


def prune_markers(state: Path, now: float) -> None:
    """Verwijder markerbestanden ouder dan een uur.

    `ledger.json`, `ledger.lock` en de sentinel `ledger.reset` blijven.
    """
    state = Path(state)
    try:
        names = os.listdir(state)
    except OSError:
        return
    for name in names:
        if name in ("ledger.json", "ledger.lock", _RESET_NAME):
            continue
        if not name.endswith(".json"):
            continue
        path = state / name
        try:
            age = now - path.stat().st_mtime
        except OSError:
            continue
        if age > _MARKER_MAX_AGE_S:
            with contextlib.suppress(OSError):
                path.unlink()


def _run_pre(payload: dict) -> None:
    """De werkelijke stappen van `run_pre`; exceptions gaan ongevangen naar de aanroeper."""
    project_dir = os.environ.get("CLAUDE_PROJECT_DIR")
    if not project_dir:
        return
    start_ns_raw = os.environ.get("LIGHTHOUSE_BASH_GATES_START_NS")
    start_ns = int(start_ns_raw) if start_ns_raw else time.time_ns()
    deadline = time.time() + _PRE_DEADLINE_S

    command = payload["tool_input"]["command"]
    cwd = payload["cwd"]
    roots = derive_roots(command, cwd)
    kept, _errors = project_filter(roots, project_dir, deadline=deadline)

    state = state_dir(payload.get("session_id", ""))

    # De marker eerst: wordt pre op zijn timeout van 5 s gekild tijdens het
    # zaaien, dan heeft post nog steeds de wortels en de starttijd (`since`,
    # de ondergrens van bron 2). Zaaien stopt bij de pre-deadline; een wortel
    # zonder seed valt in post terug, overcontroleren maar nooit blind.
    write_marker(state, marker_key(payload), start_ns, sorted(kept))

    # Best-effort blik zonder lock: bespaart een git-status-aanroep per al
    # bekende wortel. Een onleesbare ledger seedt pre helemaal niet — dat is
    # de terugval van `post` (bron 1 zonder `seen`-filter), en een seed nu
    # zou zijn resultaat toch nooit weggeschreven zien (`locked_ledger`
    # schrijft niet terug zolang `unreadable` blijft staan), dus de
    # git-status-aanroepen zijn dan pure verspilling van de pre-deadline. De
    # echte "nog niet aanwezig?"-beslissing valt onder de lock, vlak vóór het
    # inschrijven — dat dekt ook de race waarin de ledger tússen deze blik en
    # die lock alsnog kapotgaat.
    #
    # Staat de sentinel `ledger.reset` er (post verving een onleesbare
    # ledger), dan seedt pre niets: een ontbrekende wortel kan eerdere
    # sessie-writes hebben, en een seed zou die tot nullijn maken. Post valt
    # voor zo'n wortel terug (hij staat wel in de marker). Ook onder de lock
    # opnieuw gecheckt: post maakt de sentinel onder diezelfde lock aan.
    reset = state / _RESET_NAME
    peek = _read_ledger(state / "ledger.json")
    if not peek.unreadable and not reset.exists():
        existing = peek.roots
        for root in sorted(kept):
            if time.time() >= deadline:
                break
            if root in existing:
                continue
            try:
                record = seed_root(root, start_ns, deadline=deadline)
            except Exception:  # noqa: BLE001, S112 -- een falende seed slaat post over, niet pre
                continue
            with locked_ledger(state) as ledger:
                if root not in ledger.roots and not reset.exists():
                    ledger.roots[root] = record

    prune_markers(state, time.time())


def run_pre(payload: dict) -> int:
    """PreToolUse-stap: schrijf de marker, zaai nieuwe wortels, ruim oude markers op.

    De marker komt vóór het zaaien, en het zaaien stopt bij de pre-deadline:
    een pre die op zijn timeout sneuvelt laat post zo nooit zonder `since`.

    Geen zaaien bij een onleesbare ledger, en ook niet zolang de sentinel
    `ledger.reset` bestaat (post verving de ledger; zie `Ledger`). De marker
    wordt dan wél geschreven: daarmee valt post voor zo'n wortel terug.

    Altijd `0` — een PreToolUse exit 2 blokkeert de Bash-call zelf, en een
    kapotte seed- of markerstap mag geen werk tegenhouden (post merkt het
    gat op). Elke exceptie wordt daarom ingeslikt.
    """
    try:
        _run_pre(payload)
    except Exception:  # noqa: BLE001, S110 -- pre mag nooit de Bash-call blokkeren
        pass
    return 0


# --- post (taak 7): bron 1, claims, dispatch, uitvoer ------------------------

_PREFIX = "bash-write-gates:"
_CANNOT = "COULD NOT CHECK"

# Spec *Dispatch*: één deadline van 90 s na de start van post, 10 s grace per
# gate — samen ruim binnen de hooktimeout van 120 s.
_POST_DEADLINE_S = 90.0
_GATE_GRACE_S = 10.0

# ruff-format.sh herschrijft het bestand zelf: zijn oordeel geldt de versie
# van ná de gate. De andere gates lezen alleen: hun oordeel geldt de versie
# van vlak vóór de gate, zodat een schrijfactie tijdens de gate nieuw blijft.
_SIGN_AFTER_GATE = frozenset({"ruff-format.sh"})

# `run_until` gooit `Interrupted` als de post zelf een doorgestuurd signaal
# kreeg (de hooktimeout, een Ctrl-C): de gate werd afgebroken, en de post zelf
# is aan het stoppen. `run_gate` meldt dat als dit oordeel. Een gate die zelf
# met 128 + n eindigt, is geen signaal maar een gewone niet-oordeel-exit.
_STOPPED = "stopped"

# `run_gate`: de gate staat er niet, want zijn taalpakket ontbreekt (besluit 19).
# Geen blokkade, wel een melding per bestand; het bestand telt daarna als gezien.
_MISSING = "missing"


def _now() -> float:
    """De klok van `run_post`; een module-functie zodat een test hem kan vervangen."""
    return time.time()


def _post_deadline_s() -> float:
    """`LIGHTHOUSE_BASH_GATES_DEADLINE` (test-only naad) of de default van 90 s."""
    raw = os.environ.get("LIGHTHOUSE_BASH_GATES_DEADLINE")
    try:
        return float(raw) if raw else _POST_DEADLINE_S
    except ValueError:
        return _POST_DEADLINE_S


def classify(rc: int, output: bytes) -> str:
    """Het oordeel van een gate: `"ok"`, `"finding"` of `"unchecked"`.

    rc 0 is ok. rc 2 is een bevinding, tenzij de EERSTE uitvoerregel het
    token `COULD NOT CHECK` bevat: de niet-gekeken-vorm van elke gate
    (`<gate>: COULD NOT CHECK — <reden>`, besluit 18). Alleen de eerste regel:
    shellcheck en ruff citeren bronregels, dus een bestand dat dat token zelf
    bevat mag geen echt oordeel ongedaan maken. Elke andere rc (deadline 124,
    ontbrekend 127, een crash) is unchecked.
    """
    if rc == 0:
        return "ok"
    if rc != 2:
        return "unchecked"
    first = output.decode(errors="replace").split("\n", 1)[0]
    if _CANNOT in first:
        return "unchecked"
    return "finding"


def run_gate(root: str, rel: str, deadline: float) -> tuple[str, bytes]:
    """Draai de gate van `rel` met een nagemaakte Edit-achtige payload.

    Payload `{"tool_name":"Bash","cwd":root,"tool_input":{"file_path":<abs>}}`,
    proces-cwd = `root`: `ruff-format.sh` zoekt zijn `.ruff-version` vanuit de
    proces-cwd, niet via de payload. Geeft `(classify(rc, uitvoer), uitvoer)`,
    of `(_STOPPED, uitvoer)` als een doorgestuurd signaal de gate afbrak, of
    `(_MISSING, melding)` als de gate niet onder de repo-root staat.
    """
    gate = GATES[os.path.splitext(rel)[1]]
    path = gate_path(str(config.repo_root()), os.path.splitext(rel)[1])
    assert path is not None  # GATES kent de extensie
    if not path.is_file():
        return _MISSING, f"no {GATE_PACKS[gate]} gate installed".encode()
    payload = {
        "tool_name": "Bash",
        "cwd": root,
        "tool_input": {"file_path": os.path.join(root, rel)},
    }
    try:
        rc, output = run_until(
            deadline,
            [str(path)],
            grace=_GATE_GRACE_S,
            cwd=root,
            stdin_bytes=json.dumps(payload).encode("utf-8"),
            capture=True,
            raise_on_signal=True,
        )
    except Interrupted as stopped:
        return _STOPPED, stopped.output
    return classify(rc, output), output


def _printable(text: str) -> str:
    """`text` zonder surrogaten: een niet-UTF-8-bestandsnaam (`os.fsdecode`) als `\\xNN`.

    Anders crasht het wegschrijven naar stdout/stderr op een
    `UnicodeEncodeError`, en dan is de melding weg.
    """
    try:
        raw = text.encode("utf-8", "surrogateescape")
    except UnicodeEncodeError:
        return text.encode("utf-8", "backslashreplace").decode("utf-8")
    return raw.decode("utf-8", "backslashreplace")


class _Outcome:
    """Wat post meldt: blokkerende delen (exit 2) en meldingen (exit 0).

    Spec *Uitvoer*, plus het besluit uit het plan: bij exit 2 de bundel naar
    stderr én als `additionalContext` naar stdout; bij exit 0 alleen dat
    laatste, en zonder iets te melden een lege stdout.
    """

    def __init__(self) -> None:
        self.parts: list[str] = []
        self.blocking = False

    def block(self, text: str) -> None:
        self.parts.append(text)
        self.blocking = True

    def note(self, text: str) -> None:
        self.parts.append(text)

    def cannot(self, what: str) -> None:
        self.block(f"{_PREFIX} {_CANNOT}: {what}")

    def render(self, event: str) -> tuple[int, str, str]:
        if not self.parts:
            return 0, "", ""
        text = _printable("\n\n".join(self.parts))
        context = {
            "hookSpecificOutput": {
                "hookEventName": event,
                "additionalContext": text,
            }
        }
        stdout = json.dumps(context) + "\n"
        if self.blocking:
            return 2, stdout, text + "\n"
        return 0, stdout, ""


class _RootView(NamedTuple):
    """Wat post vóór de pool van één wortel weet.

    `fallback` is `True` als de ledger geen bruikbare regel voor de wortel
    had (afwezig, kapot, of de hele ledger onleesbaar): dan is `seen` leeg —
    alles wat vuil is, is kandidaat, overcontroleren mag, blind zijn niet.
    `head` is de nu gelezen HEAD: het eindpunt van de wandeling van bron 2,
    de nieuwe ledger-`head` (compare-and-swap) en waarmee post een
    terugvalwortel inschrijft. `start_head` is de ledger-`head` waarmee deze
    post begon (`None` zonder bruikbare ledgerregel), `pending` de
    `pending`-lijst van toen. `since` is de pre-starttijd uit de marker in
    hele epoch-seconden (naar beneden afgerond), of `None` zonder marker: de
    ondergrens van bron 2 in de terugval, en naast `start_head` de tweede
    wandeling die een terugkeer naar de ledger-sha of een reflog-gat dekt.
    """

    root: str
    seen: dict
    fallback: bool
    head: str
    start_head: str | None
    pending: list[str]
    since: int | None = None


def _valid_record(record: object) -> dict | None:
    """`record` als hij de vorm van een ledgerregel heeft, anders `None` (= afwezig)."""
    if not isinstance(record, dict) or not isinstance(record.get("seen"), dict):
        return None
    if not isinstance(record.setdefault("claims", {}), dict):
        return None
    if not isinstance(record.setdefault("pending", []), list):
        return None
    return record


def _root_view(
    ledger: Ledger, root: str, since: int | None, *, deadline: float
) -> _RootView:
    """`seen`, `head` en `pending` van `root` uit de ledger, plus HEAD nu.

    De ene plek die bepaalt wat post per wortel al weet. Zonder bruikbare
    ledgerregel is het de terugval: `seen` leeg, en bron 2 wandelt vanaf
    `since` (de pre-starttijd uit de marker). Mét ledgerregel gaat `since`
    ook mee: bron 2 wandelt dan tot de ledger-`head` én sinds de start (zie
    `commits_to_check`). Gooit `GitError` als HEAD niet te lezen is.
    """
    head = head_of(root, deadline=deadline)
    record = None if ledger.unreadable else _valid_record(ledger.roots.get(root))
    if record is None:
        return _RootView(root, {}, True, head, None, [], since)
    start = record.get("head")
    pending = [p for p in record["pending"] if isinstance(p, str)]
    return _RootView(
        root,
        record["seen"],
        False,
        head,
        start if isinstance(start, str) else None,
        pending,
        since,
    )


_NO_START_NOTE = (
    "source 2 skipped: no ledger HEAD and no marker with a start time"
    " to walk back to"
)


class _Committed(NamedTuple):
    """Bron 2 van één wortel.

    `paths`: alles wat een gewandelde commit wijzigde. `stranded`: per pad de
    nieuwste gewandelde commit die het wijzigde, tenzij een nog nieuwere het
    verwijderde — staat zo'n pad niet op schijf en niet in HEAD, dan is het
    niet weg maar alleen elders gecommit (`switch -c x; commit; switch main`).
    `notes`: de meldingen van de wandeling.
    """

    paths: set[str]
    stranded: dict[str, str]
    notes: list[str]


def _committed_paths(view: _RootView, *, deadline: float) -> _Committed:
    """Bron 2: wat in de call gecommit werd (zie `_Committed`).

    Wandelt de reflog van de nu gelezen HEAD terug tot de ledger-`head` en,
    met een marker, ook tot de pre-starttijd (de vereniging, zie
    `commits_to_check`); in de terugval (geen ledger-`head`) alleen tot die
    starttijd. Zonder marker én zonder ledger-`head` is er geen ondergrens
    en wordt bron 2 met een melding overgeslagen. `HEAD == ledger-head` is
    alleen zonder marker een kortsluiting: mét marker kan de call langs een
    andere tak gecommit en teruggekeerd zijn. Alleen commit-entries tellen,
    per entry `diff_commit` (een merge via de combined diff). Verwijderde
    paden vallen af; een pad dat een latere commit weghaalde valt pas in het
    nettofilter af. Een ongeboren HEAD levert niets en is geen fout — en
    `git reflog` mag daar niet eens draaien (rc 128). Gooit `GitError` als
    reflog of diff-tree faalt.
    """
    stop = view.start_head
    if view.head == "unborn":
        return _Committed(set(), {}, [])
    if stop == view.head and view.since is None:
        return _Committed(set(), {}, [])
    if stop is None and view.since is None:
        return _Committed(set(), {}, [_NO_START_NOTE])
    entries = read_reflog(view.root, deadline=deadline)
    commits, notes = commits_to_check(entries, stop, view.since)
    committed: set[str] = set()
    # Nieuwste eerst: de eerste commit die een pad noemt, bepaalt zijn lot.
    fate: dict[str, str | None] = {}
    for sha, is_merge in commits:
        changed, deleted = diff_commit(view.root, sha, is_merge, deadline=deadline)
        committed |= changed
        for rel in changed:
            fate.setdefault(rel, sha)
        for rel in deleted:
            fate.setdefault(rel, None)
    stranded = {rel: sha for rel, sha in fate.items() if sha is not None}
    return _Committed(committed, stranded, notes)


def _in_head(root: str, head: str, paths: list[str], *, deadline: float) -> set[str]:
    """Welke van `paths` in de boom van `head` staan (`git cat-file -e <head>:<pad>`).

    Tegen de gelezen sha, niet tegen `HEAD`: dezelfde boom als waar bron 2
    eindigde. Een ontbrekend pad geeft rc 128, net als een kapotte repo; maar
    HEAD is net gelezen, dus elke exit telt als "niet in HEAD". Alleen een
    timeout of een git die niet start (`returncode is None`) gooit door.
    """
    if head == "unborn":
        return set()
    present: set[str] = set()
    for rel in paths:
        try:
            git(root, "cat-file", "-e", f"{head}:{rel}", deadline=deadline)
        except GitError as exc:
            if exc.returncode is None:
                raise
            continue
        present.add(rel)
    return present


def _in_a_ref(root: str, shas: set[str], *, deadline: float) -> set[str]:
    """Welke van `shas` een ref nog bevat (`git for-each-ref --contains <sha>`).

    Een commit die alleen de reflog nog kent, is bewust weggegooid (`reset
    --hard`, `--amend`, een rebase die hem dropt): zijn paden staan niet
    elders, ze zijn weg. Gooit `GitError` als for-each-ref faalt.
    """
    kept: set[str] = set()
    for sha in shas:
        out = git(
            root,
            "for-each-ref",
            "--count=1",
            "--format=%(refname)",
            "--contains",
            sha,
            deadline=deadline,
        )
        if out.strip():
            kept.add(sha)
    return kept


def _is_utf8(rel: str) -> bool:
    """Is het pad geldige UTF-8? De gates lezen het via jq, dat er U+FFFD van maakt."""
    try:
        os.fsencode(rel).decode("utf-8")
    except UnicodeDecodeError:
        return False
    return True


class _Pool(NamedTuple):
    """De pool van één wortel, na het nettofilter en `seen`.

    `candidates` gaan naar de dispatch; `unchecked` zijn `(pad, reden)` voor
    paden waarnaar niet te kijken valt (in HEAD maar niet op schijf, alleen
    elders gecommit, of geen geldige UTF-8); `keep` zijn de bron-2- en `pending`-paden zonder oordeel
    (die blijven in `pending`); `settled` zijn paden die uit `pending` mogen
    (verwijderd, of al zo in `seen`). `notes` zijn de meldingen van bron 2.
    """

    view: _RootView
    candidates: list[str]
    unchecked: list[tuple[str, str]]
    keep: list[str]
    settled: list[str]
    notes: list[str]


def _root_pool(view: _RootView, *, deadline: float) -> _Pool:
    """Bron 1, bron 2 en `pending`, op extensie en netto gefilterd, gesorteerd.

    `git status --porcelain -z -uall` is repo-relatief, ook als de Bash-call
    vanuit een submap schreef. Netto: een pad dat niet op schijf en niet in
    HEAD staat is verwijderd en valt af, net als een pad dat bron 1 als
    verwijderd noemt; staat het wél in HEAD, dan is het `unchecked`. Een
    uitzondering op "verwijderd": een pad dat deze wandeling gecommit zag en
    dat geen latere gewandelde commit verwijderde (`_Committed.stranded`)
    staat alleen elders, bv. op de tak waar de call vandaan terugkeerde — ook
    `unchecked`, met de commit erbij. Maar alleen als een ref die commit nog
    bevat (`_in_a_ref`): kent alleen de reflog hem, dan gooide de call hem weg
    en is het pad gewoon verwijderd. Gooit `GitError` als status, reflog,
    diff-tree of for-each-ref faalt.
    """
    data = git(view.root, "status", "--porcelain", "-z", "-uall", deadline=deadline)
    dirty, removed = parse_porcelain(data)
    committed, stranded, notes = _committed_paths(view, deadline=deadline)
    from_source_2 = committed | set(view.pending)
    pool = sorted(
        rel for rel in dirty | from_source_2 if os.path.splitext(rel)[1] in GATES
    )
    sigs = {rel: signature(os.path.join(view.root, rel)) for rel in pool}
    missing = [rel for rel in pool if sigs[rel] is None and rel not in removed]
    in_head = _in_head(view.root, view.head, missing, deadline=deadline)
    elsewhere_shas = {
        stranded[r] for r in missing if r in stranded and r not in in_head
    }
    kept = _in_a_ref(view.root, elsewhere_shas, deadline=deadline)
    stranded = {rel: sha for rel, sha in stranded.items() if sha in kept}

    result = _Pool(view, [], [], [], [], notes)
    for rel in pool:
        sig = sigs[rel]
        elsewhere = sig is None and rel not in in_head
        if elsewhere and (rel in removed or rel not in stranded):
            result.settled.append(rel)
            continue
        if sig is not None and view.seen.get(rel) == list(sig):
            result.settled.append(rel)
            continue
        if elsewhere:
            why = f"committed in {stranded[rel][:12]}, not on disk and not in HEAD"
            result.unchecked.append((rel, why))
        elif sig is None:
            result.unchecked.append((rel, "in HEAD, not on disk"))
        elif not _is_utf8(rel):
            result.unchecked.append((rel, "name is not valid UTF-8"))
        else:
            result.candidates.append(rel)
        if rel in from_source_2:
            result.keep.append(rel)
    return result


def _record_for(ledger: Ledger, view: _RootView) -> dict:
    """De ledgerregel van `view.root`; ontbreekt hij, dan hier ingeschreven.

    Een terugvalwortel (of een regel die intussen verdween) krijgt een
    verse regel met de nu gelezen HEAD.
    """
    record = _valid_record(ledger.roots.get(view.root))
    if record is None:
        record = {"seen": {}, "head": view.head, "pending": [], "claims": {}}
        ledger.roots[view.root] = record
    return record


def _advance(record: dict, pool: _Pool) -> None:
    """Compare-and-swap op `head`, dan `pending` bijwerken (onder de lock).

    `head` wordt de gelezen HEAD alleen als de ledger nog de waarde heeft
    waarmee deze post begon: een parallelle post die al verder was, wint.
    `pending` krijgt de bron-2-paden zonder oordeel; wat intussen door een
    andere post is toegevoegd, blijft staan.
    """
    view = pool.view
    if record.get("head") == view.start_head:
        record["head"] = view.head
    pending = {p for p in record["pending"] if isinstance(p, str)}
    record["pending"] = sorted((pending | set(pool.keep)) - set(pool.settled))


def _claimed_by_other(holder: object, key: str, now: float) -> bool:
    """Draagt `holder` een levende claim van een andere post?

    Een claim ouder dan deadline plus grace telt als afwezig: die post is
    afgebroken zonder op te ruimen. Een misvormde claim telt ook als afwezig.
    """
    if not isinstance(holder, list) or len(holder) != 2:
        return False
    owner, epoch = holder
    if owner == key or not isinstance(epoch, (int, float)):
        return False
    return now - epoch <= _post_deadline_s() + _GATE_GRACE_S


def _claim(
    state: Path, key: str, pools: list[_Pool]
) -> tuple[list[tuple[_RootView, str]], bool]:
    """Onder de lock: per wortel `_advance`, dan claimen wat nog niemand controleert.

    Een kandidaat die inmiddels met de huidige handtekening in `seen` staat
    valt af (en uit `pending`: hij heeft zijn oordeel). Een onleesbare ledger
    wordt hier bewust vervangen door een verse, met alleen de wortels die
    deze post wandelde (elk met de gewandelde HEAD): anders valt elke
    volgende post opnieuw terug. Onder dezelfde lock komt de sentinel
    `ledger.reset` (zie `Ledger`), zodat pre de wortels die hier ontbreken
    niet seedt. Geeft `(geclaimd, vervangen)`.
    """
    now = _now()
    claimed: list[tuple[_RootView, str]] = []
    with locked_ledger(state) as ledger:
        replaced = ledger.unreadable
        if replaced:
            os.close(os.open(str(state / _RESET_NAME), os.O_CREAT | os.O_WRONLY, 0o600))
            ledger.roots = {}
            ledger.unreadable = False
        for pool in pools:
            view = pool.view
            record = _record_for(ledger, view)
            _advance(record, pool)
            for rel in pool.candidates:
                sig = signature(os.path.join(view.root, rel))
                if sig is not None and record["seen"].get(rel) == list(sig):
                    _drop_pending(record, rel)
                    continue
                if _claimed_by_other(record["claims"].get(rel), key, now):
                    continue
                record["claims"][rel] = [key, now]
                claimed.append((view, rel))
    return claimed, replaced


def _drop_pending(record: dict, rel: str) -> None:
    record["pending"] = [p for p in record["pending"] if p != rel]


def _drop_claim(record: dict, rel: str, key: str) -> None:
    holder = record["claims"].get(rel)
    if isinstance(holder, list) and holder and holder[0] == key:
        del record["claims"][rel]


def _record_verdict(
    state: Path,
    key: str,
    view: _RootView,
    rel: str,
    verdict: str,
    sig: Signature | None,
) -> None:
    """Na één gate, onder de lock: de claim weg; bij een echt oordeel uit `pending` en in `seen`."""
    with locked_ledger(state) as ledger:
        if ledger.unreadable:
            return
        record = _record_for(ledger, view)
        _drop_claim(record, rel, key)
        if verdict == "unchecked":
            return
        _drop_pending(record, rel)
        if sig is not None:
            record["seen"][rel] = list(sig)


def _release_claims(state: Path, key: str) -> None:
    """Verwijder elke claim die nog `key` draagt: een claim bestaat alleen tijdens de gate."""
    with locked_ledger(state) as ledger:
        if ledger.unreadable:
            return
        for raw in ledger.roots.values():
            record = _valid_record(raw)
            if record is None:
                continue
            for rel in list(record["claims"]):
                _drop_claim(record, rel, key)


def _report_verdict(
    outcome: _Outcome, label: str, gate: str, verdict: str, output: bytes
) -> None:
    """Eén gate in de bundel: `== <label> (<gate>) ==` plus zijn uitvoer.

    `label` is het repo-relatieve pad, of het absolute pad als de post meer
    dan één wortel dekt — twee wortels kunnen hetzelfde relatieve pad hebben.
    """
    if verdict == "ok":
        return
    if verdict == _MISSING:
        outcome.note(f"{_PREFIX} {label} not checked — {output.decode()}")
        return
    lines = [f"== {label} ({gate}) =="]
    if verdict == "unchecked":
        lines.append(f"{_PREFIX} {_CANNOT}: {label} — {gate} gave no verdict")
    text = output.decode(errors="replace").rstrip("\n")
    if text:
        lines.append(text)
    outcome.block("\n".join(lines))


def _cannot_left(
    outcome: _Outcome, why: str, left: list[tuple[_RootView, str]]
) -> None:
    """Eén `KON NIET CONTROLEREN` met `why` en elk resterend absoluut pad."""
    outcome.cannot(
        f"{why}, not checked:\n"
        + "\n".join(f"  {os.path.join(v.root, r)}" for v, r in left)
    )


def _vanished(
    state: Path,
    key: str,
    view: _RootView,
    rel: str,
    outcome: _Outcome,
    *,
    deadline: float,
) -> None:
    """Een geclaimde kandidaat staat niet meer op schijf: geen gate, geen oordeel.

    Het nettofilter van de pool, nu op het moment van de gate: staat het pad
    in HEAD, dan is het `KON NIET CONTROLEREN` en blijft het in `pending`;
    anders is het verwijderd en valt het af (ook uit `pending`). Is HEAD
    niet te lezen, dan telt het als "in HEAD": luid, nooit stil.
    """
    try:
        in_head = bool(_in_head(view.root, view.head, [rel], deadline=deadline))
    except GitError:
        in_head = True
    with locked_ledger(state) as ledger:
        if not ledger.unreadable:
            record = _record_for(ledger, view)
            _drop_claim(record, rel, key)
            if not in_head:
                _drop_pending(record, rel)
    if in_head:
        path = os.path.join(view.root, rel)
        outcome.cannot(f"{path} (vanished before the gate; in HEAD, not on disk)")


def _dispatch(
    claimed: list[tuple[_RootView, str]],
    state: Path,
    key: str,
    deadline: float,
    outcome: _Outcome,
    *,
    absolute: bool,
) -> None:
    """Sequentieel, buiten de lock, in gesorteerde volgorde, tot de deadline.

    Gesorteerd plus direct `seen` bijwerken betekent: ook met veel vuile
    bestanden en een krappe deadline boekt elke post voortgang, en de
    volgende post gaat verder waar deze stopte. Met `absolute` (de post
    dekt meer dan één wortel) noemt de bundelheader het absolute pad.

    Een kandidaat die tussen pool en gate verdween gaat niet naar de gate:
    die zou `[ -f ]` zien falen en exit 0 geven, en dat is geen oordeel (zie
    `_vanished`). Breekt een doorgestuurd signaal een gate af, dan stopt de
    dispatch: de rest is `KON NIET CONTROLEREN`, net als na de deadline.
    """
    for index, (view, rel) in enumerate(claimed):
        if _now() >= deadline:
            _cannot_left(outcome, "deadline passed", claimed[index:])
            return
        path = os.path.join(view.root, rel)
        gate = GATES[os.path.splitext(rel)[1]]
        before = signature(path)
        if before is None:
            _vanished(state, key, view, rel, outcome, deadline=deadline)
            continue
        verdict, output = run_gate(view.root, rel, deadline)
        if verdict == _STOPPED:
            _record_verdict(state, key, view, rel, "unchecked", None)
            _cannot_left(outcome, "signal received", claimed[index:])
            return
        sig = signature(path) if gate in _SIGN_AFTER_GATE else before
        _record_verdict(state, key, view, rel, verdict, sig)
        _report_verdict(outcome, path if absolute else rel, gate, verdict, output)


def _marker_roots(marker: dict | None) -> set[str]:
    """De wortels die pre afleidde; leeg zonder (bruikbare) marker."""
    if marker is None or not isinstance(marker.get("roots"), list):
        return set()
    return {r for r in marker["roots"] if isinstance(r, str)}


def _marker_since(marker: dict | None) -> int | None:
    """De pre-starttijd uit de marker in hele epoch-seconden, naar beneden afgerond.

    De reflog-epoch (`--date=unix`) is in hele seconden; naar boven afronden
    zou een commit uit dezelfde seconde als pre laten vallen. `None` zonder
    marker of zonder geldige `start_ns`.
    """
    start_ns = marker.get("start_ns") if marker is not None else None
    if isinstance(start_ns, bool) or not isinstance(start_ns, int):
        return None
    return start_ns // 10**9


def _post_roots(
    payload: dict,
    marker_roots: set[str],
    project_dir: str,
    deadline: float,
    outcome: _Outcome,
) -> tuple[list[str], list[str]]:
    """`(bestaande wortels, verdwenen wortels)`, beide gesorteerd.

    Marker-wortels ∪ de projectwortels uit de post-payload: de post-`cwd` kan
    verschoven zijn en een wortel waarin gecommit en daarna weggegaan werd,
    mag niet wegvallen. Een wortel die niet meer bestaat (een verwijderde
    worktree) is niet meer te controleren en komt apart terug, zodat post
    hem kan noemen. Gooit `GitError` als `project_dir` zelf niet te lezen is.
    """
    roots = set(marker_roots)
    tool_input = payload.get("tool_input")
    command = tool_input.get("command") if isinstance(tool_input, dict) else None
    cwd = payload.get("cwd")
    derived = derive_roots(
        command if isinstance(command, str) else "",
        cwd if isinstance(cwd, str) and cwd else project_dir,
    )
    kept, errors = project_filter(derived, project_dir, deadline=deadline)
    for line in sorted(errors):
        outcome.block(f"{_PREFIX} {line}")
    roots.update(kept)
    present = sorted(root for root in roots if os.path.isdir(root))
    gone = sorted(roots.difference(present))
    return present, gone


def _post_in_state(
    payload: dict,
    outcome: _Outcome,
    project_dir: str,
    state: Path,
    key: str,
    deadline: float,
) -> None:
    marker = read_marker(state, key)
    marker_roots = _marker_roots(marker)
    try:
        roots, gone = _post_roots(payload, marker_roots, project_dir, deadline, outcome)
    except GitError as exc:
        outcome.cannot(f"{project_dir} ({exc})")
        return
    for root in gone:
        # Exit 0: de bestanden zijn met de wortel mee verdwenen.
        outcome.note(f"{_PREFIX} {root} no longer exists; not checked")

    ledger = _read_ledger(state / "ledger.json")
    if ledger.unreadable and roots:
        outcome.note(f"{_PREFIX} ledger unreadable; fallback for every root")
    pools = _root_pools(
        roots, ledger, marker_roots, _marker_since(marker), deadline, outcome
    )
    if pools:
        claimed, replaced = _claim(state, key, pools)
        if replaced:
            outcome.note(
                f"{_PREFIX} unreadable ledger replaced; a root this call did not"
                " touch falls back at its next call"
            )
        _dispatch(claimed, state, key, deadline, outcome, absolute=len(pools) > 1)


def _root_pools(
    roots: list[str],
    ledger: Ledger,
    marker_roots: set[str],
    since: int | None,
    deadline: float,
    outcome: _Outcome,
) -> list[_Pool]:
    """De pool per wortel, met de meldingen en de niet-te-bekijken paden.

    Een terugvalwortel (geen bruikbare ledgerregel) krijgt alleen een
    melding als hij in de marker staat — dan bestond hij al vóór pre. Een
    wortel die tijdens de call ontstond (`git worktree add`, `git init`)
    valt stil terug.
    """
    pools: list[_Pool] = []
    for root in roots:
        try:
            view = _root_view(ledger, root, since, deadline=deadline)
            pool = _root_pool(view, deadline=deadline)
        except GitError as exc:
            outcome.cannot(f"{root} ({exc})")
            continue
        pools.append(pool)
        if view.fallback and root in marker_roots:
            outcome.note(f"{_PREFIX} fallback without ledger for {root}")
        for note in pool.notes:
            outcome.note(f"{_PREFIX} {root}: {note}")
        for rel, why in pool.unchecked:
            outcome.cannot(f"{os.path.join(root, rel)} ({why})")
    return pools


def _post(payload: dict, outcome: _Outcome) -> None:
    project_dir = os.environ.get("CLAUDE_PROJECT_DIR")
    if not project_dir:
        outcome.cannot("CLAUDE_PROJECT_DIR is missing, the project roots are unknown")
        return
    deadline = _now() + _post_deadline_s()
    state = state_dir(str(payload.get("session_id", "")))
    key = marker_key(payload)
    try:
        _post_in_state(payload, outcome, project_dir, state, key, deadline)
    finally:
        _release_claims(state, key)
        with contextlib.suppress(OSError):
            (state / f"{key}.json").unlink()


def run_post(payload: dict) -> tuple[int, str, str]:
    """PostToolUse(Failure)-stap: `(exitcode, stdout, stderr)`.

    Bron 1 (vuil) per wortel, gefilterd op `seen`, claimen, gesorteerd
    dispatchen tot de deadline, bundelen. Claims die nog de sleutel van deze
    post dragen gaan weg vóór hij terugkeert — normaal, na de deadline of via
    een exceptie — en de marker wordt opgeruimd. Een onverwachte exceptie is
    `KON NIET CONTROLEREN` met exit 2, nooit een traceback met exit 1.
    """
    event = payload.get("hook_event_name") or "PostToolUse"
    outcome = _Outcome()
    try:
        _post(payload, outcome)
    except Exception as exc:  # noqa: BLE001 -- post faalt luid, nooit met een traceback
        outcome.cannot(f"internal error in the dispatcher ({type(exc).__name__}: {exc})")
    return outcome.render(str(event))


def main(argv: list[str]) -> int:
    """`bash_writes.py pre|post`, hookpayload op stdin.

    `pre` geeft altijd 0. `post` schrijft zijn uitvoer zelf weg; een payload
    die geen JSON-object is, is `KON NIET CONTROLEREN` met exit 2.
    """
    mode = argv[0] if argv else ""
    if mode not in ("pre", "post"):
        sys.stderr.write(f"{_PREFIX} usage: bash_writes.py pre|post\n")
        return 2
    try:
        payload = json.loads(sys.stdin.buffer.read())
    except (ValueError, OSError):
        payload = None
    if mode == "pre":
        return run_pre(payload) if isinstance(payload, dict) else 0
    if isinstance(payload, dict):
        code, out, err = run_post(payload)
    else:
        outcome = _Outcome()
        outcome.cannot("the hook payload on stdin is not a JSON object")
        code, out, err = outcome.render("PostToolUse")
    sys.stdout.write(out)
    sys.stderr.write(err)
    return code


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
