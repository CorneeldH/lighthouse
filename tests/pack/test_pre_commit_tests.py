"""End-to-end tests voor de test-gate `.claude/hooks/pre-commit-tests.sh` (#884).

De hook draait als subprocess, met echte git-repo's in `tmp_path` en een
nep-`uv` vooraan op `PATH`. Veiligheid: elke test zet `CLAUDE_PROJECT_DIR` op
een tijdelijke repo en zet de stubmap met de nep-`uv` als eerste op `PATH`.
Een gate-test die zonder nep-`uv` de echte repo bereikt, draait de hele suite
binnen de suite.
"""

import fcntl
import json
import os
import re
import shutil
import signal
import subprocess
import time
import tomllib
from pathlib import Path

import pytest

from tests.unit._hook_testkit import (
    HOOKS,
    bash_payload,
    git,
    init_repo,
    run_hook,
    tool_bin,
)

HOOK = HOOKS / "pre-commit-tests.sh"
REPO = HOOKS.parents[1]
SETTINGS = HOOKS.parent / "settings.json"
REAL_GIT = shutil.which("git")

# Nep-`uv`: noteert waar hij draaide, geeft een pytest-achtige slotregel en
# de exitcode uit UV_EXIT. Met UV_SLOW_SECOND slaapt de tweede aanroep eerst,
# zodat een gedeelde deadline bij de tweede wortel verloopt.
FAKE_UV = (
    "#!/bin/sh\n"
    'if [ -n "$UV_SLOW_SECOND" ] && [ -s "$UV_MARK" ]; then\n'
    '  pwd >> "$UV_MARK"\n'
    "  sleep 8\n"
    "fi\n"
    'pwd >> "$UV_MARK"\n'
    'printf \'%s\\n\' "$*" >> "$UV_MARK.args"\n'
    # --basetemp: noteer de map en laat er iets in achter, zodat een test kan
    # zien dat de hook hem opruimt.
    'for a in "$@"; do\n'
    '  case "$a" in\n'
    "    --basetemp=*)\n"
    '      bt="${a#--basetemp=}"\n'
    '      echo "$bt" >> "$UV_MARK.bt"\n'
    '      mkdir -p "$bt" && echo x > "$bt/leftover"\n'
    "      ;;\n"
    "  esac\n"
    "done\n"
    'echo "3 passed in 0.01s"\n'
    "exit ${UV_EXIT:-0}\n"
)

# Doorgeef-stubs: de rem (Taak 3) is op de Mac en in de devcontainer niet
# aanwezig; zo hangt geen gate-test af van wat de host heeft.
FAKE_SYSTEMCTL = '#!/bin/sh\necho "Failed to connect to bus" >&2\nexit 1\n'
# Schuift alles tot en met de eerste `--` weg en voert de rest uit.
_PASS_THROUGH = (
    'while [ "$#" -gt 0 ]; do\n'
    '  if [ "$1" = "--" ]; then shift; break; fi\n'
    "  shift\n"
    "done\n"
    'exec "$@"\n'
)
FAKE_SYSTEMD_RUN = '#!/bin/sh\necho "$*" >> "$UV_MARK.wrap"\n' + _PASS_THROUGH
FAKE_CHOOM = '#!/bin/sh\necho "$*" >> "$UV_MARK.choom"\n' + _PASS_THROUGH


class Gate:
    """Een tijdelijke projectrepo plus een stubmap met nep-`uv`."""

    def __init__(self, tmp_path: Path, project: Path) -> None:
        self.tmp = tmp_path
        self.project = project
        init_repo(project)
        self.stub = tmp_path / "stub"
        tool_bin(
            self.stub,
            (),
            stubs={
                "uv": FAKE_UV,
                "systemctl": FAKE_SYSTEMCTL,
                "systemd-run": FAKE_SYSTEMD_RUN,
                "choom": FAKE_CHOOM,
            },
        )
        self.mark = tmp_path / "mark"
        self.slot_dir = tmp_path / "slots"
        self.cgroup = tmp_path / "cgroup"

    def add_stub(self, name: str, script: str) -> None:
        tool_bin(self.stub, (), stubs={name: script})

    def run(self, cmd: str, cwd: Path, path: str | None = None, **env: str):
        tail = os.environ["PATH"] if path is None else path
        full_env = {
            # Stubmap altijd eerst: zo bereikt geen enkele test de echte `uv`.
            "PATH": f"{self.stub}{os.pathsep}{tail}",
            "CLAUDE_PROJECT_DIR": str(self.project),
            "UV_MARK": str(self.mark),
            # Elke gate-test draait zelf binnen een echte gate die een echt
            # slot houdt: zonder eigen lockmap blokkeert de suite zichzelf.
            "PAIA_GATE_SLOT_DIR": str(self.slot_dir),
            "PAIA_GATE_CGROUP_ROOT": str(self.cgroup),
            "PAIA_GATE_SLOT_WAIT": "1",
            **env,
        }
        return run_hook(HOOK, bash_payload(cmd, cwd), cwd, full_env)

    def marks(self) -> list[str]:
        if not self.mark.exists():
            return []
        return self.mark.read_text(encoding="utf-8").splitlines()

    def args(self) -> list[str]:
        args_file = Path(f"{self.mark}.args")
        if not args_file.exists():
            return []
        return args_file.read_text(encoding="utf-8").splitlines()


@pytest.fixture
def gate(tmp_path: Path) -> Gate:
    return Gate(tmp_path, tmp_path / "X")


def _context(stdout: str) -> str:
    return json.loads(stdout)["hookSpecificOutput"]["additionalContext"]


def _worktree(repo: Path, path: Path, branch: str) -> None:
    git(repo, "worktree", "add", "-q", "-b", branch, str(path))


def test_gate_tests_the_tree_after_cd(gate: Gate) -> None:
    result = gate.run(f"cd {gate.project} && git commit -m x", gate.tmp)

    assert result.returncode == 0, result.stderr
    assert gate.marks() == [str(gate.project.resolve())]
    context = _context(result.stdout)
    assert "3 passed" in context
    assert str(gate.project) in context
    assert json.loads(result.stdout)["systemMessage"] == context


def test_gate_tests_the_dash_c_target_not_cwd(gate: Gate) -> None:
    other = gate.tmp / "Z"
    _worktree(gate.project, other, "z")

    result = gate.run(f"git -C {gate.project} commit -m x", other)

    assert result.returncode == 0, result.stderr
    assert gate.marks() == [str(gate.project.resolve())]


def test_gate_runs_four_workers_with_its_own_basetemp(gate: Gate) -> None:
    result = gate.run("git commit -m x", gate.project)

    assert result.returncode == 0, result.stderr
    lines = gate.args()
    assert len(lines) == 1
    line = lines[0]
    assert "-n auto --maxprocesses 4" in line
    assert "--maxprocesses 8" not in line
    assert "--basetemp=" in line
    assert "-m not hardware_audio and not slow" in line


def test_gate_removes_its_basetemp(gate: Gate) -> None:
    result = gate.run("git commit -m x", gate.project)

    assert result.returncode == 0, result.stderr
    recorded = Path(f"{gate.mark}.bt").read_text(encoding="utf-8").splitlines()
    assert len(recorded) == 1
    assert recorded[0]
    # De nep-uv liet een bestand in de map achter; de hook ruimt hem op.
    assert not Path(recorded[0]).exists()


def test_gate_blocks_when_xdist_is_missing(gate: Gate) -> None:
    gate.add_stub(
        "uv",
        '#!/bin/sh\necho "pytest: error: unrecognized arguments: -n" >&2\nexit 4\n',
    )

    result = gate.run("git commit -m x", gate.project)

    # Een boom zonder xdist is niet getest, niet rood: de melding wijst naar
    # de rebase, niet naar kapotte tests. De ruwe pytest-uitvoer blijft staan.
    assert result.returncode == 2, result.stdout
    assert "Test-gate: KON NIET CONTROLEREN" in result.stderr
    assert "tests failed" not in result.stderr
    assert "pytest-xdist ontbreekt" in result.stderr
    assert "origin/main" in result.stderr
    assert "unrecognized arguments: -n" in result.stderr


@pytest.mark.parametrize("exit_code", ["3", "5"])
def test_gate_reports_untested_on_pytest_error_exit(gate: Gate, exit_code: str) -> None:
    # pytest 3 (interne fout), 4 (gebruiksfout), 5 (niets verzameld): er is
    # niets getest. Dat blokkeert, maar heet geen falende test.
    result = gate.run("git commit -m x", gate.project, UV_EXIT=exit_code)

    assert result.returncode == 2, result.stdout
    assert "Test-gate: KON NIET CONTROLEREN" in result.stderr
    assert f"exitcode {exit_code}" in result.stderr
    assert "tests failed" not in result.stderr
    assert "pytest-xdist" not in result.stderr
    assert "3 passed" in result.stderr


def test_gate_blocks_on_failing_tests(gate: Gate) -> None:
    result = gate.run("git commit -m x", gate.project, UV_EXIT="1")

    assert result.returncode == 2, result.stdout
    assert "3 passed" in result.stderr
    assert "tests failed" in result.stderr


def test_gate_blocks_on_deadline(gate: Gate) -> None:
    result = gate.run("git commit -m x", gate.project, UV_EXIT="124")

    assert result.returncode == 2, result.stdout
    assert "deadline van 270 s" in result.stderr
    assert "KON NIET CONTROLEREN" in result.stderr


def test_gate_skips_foreign_repo_visibly(gate: Gate) -> None:
    foreign = gate.tmp / "Y"
    init_repo(foreign)

    result = gate.run(f"cd {foreign} && git commit -m x", gate.tmp)

    assert result.returncode == 0, result.stderr
    assert not gate.mark.exists()
    assert "overgeslagen — ander project" in result.stdout
    assert "overgeslagen — ander project" in _context(result.stdout)


def test_gate_silent_outside_any_repo(gate: Gate) -> None:
    plain = gate.tmp / "plain"
    plain.mkdir()

    result = gate.run(f"cd {plain} && git commit -m x", gate.tmp)

    assert result.returncode == 0, result.stderr
    assert result.stdout == ""
    assert not gate.mark.exists()


def test_gate_silent_without_commit(gate: Gate) -> None:
    result = gate.run("git status", gate.project)

    assert result.returncode == 0, result.stderr
    assert result.stdout == ""
    assert not gate.mark.exists()


def test_gate_loud_on_unresolved_path(gate: Gate) -> None:
    result = gate.run('git -C "$ONBEKEND" commit -m x', gate.project)

    assert result.returncode == 2, result.stdout
    assert "KON NIET CONTROLEREN" in result.stderr
    assert "niet te herleiden" in result.stderr
    assert not gate.mark.exists()


def test_gate_loud_on_new_dir(gate: Gate) -> None:
    result = gate.run(f"cd {gate.project}/nieuw && git commit -m x", gate.tmp)

    assert result.returncode == 2, result.stdout
    assert "KON NIET CONTROLEREN" in result.stderr
    assert "bestaat nog niet" in result.stderr
    assert not gate.mark.exists()


def test_gate_loud_when_git_breaks(gate: Gate) -> None:
    gate.add_stub(
        "git",
        "#!/bin/sh\n"
        'case "$*" in\n'
        '  *--show-toplevel*) echo "fatal: detected dubious ownership" >&2; '
        "exit 128 ;;\n"
        "esac\n"
        f'exec "{REAL_GIT}" "$@"\n',
    )

    result = gate.run("git commit -m x", gate.project)

    assert result.returncode == 2, result.stdout
    assert "KON NIET CONTROLEREN" in result.stderr
    assert "dubious ownership" in result.stderr
    assert not gate.mark.exists()


def _fail_common_dir_for(gate: Gate, target: Path) -> None:
    gate.add_stub(
        "git",
        "#!/bin/sh\n"
        f'if [ "$1" = "-C" ] && [ "$2" = "{target}" ]; then\n'
        '  case "$*" in\n'
        '    *--git-common-dir*) echo "fatal: kapot" >&2; exit 128 ;;\n'
        "  esac\n"
        "fi\n"
        f'exec "{REAL_GIT}" "$@"\n',
    )


def test_gate_loud_when_common_dir_fails_for_target(gate: Gate) -> None:
    second = gate.tmp / "X2"
    _worktree(gate.project, second, "x2")
    _fail_common_dir_for(gate, second.resolve())

    result = gate.run("git commit -m x", second)

    assert result.returncode == 2, result.stdout
    assert "KON NIET CONTROLEREN" in result.stderr
    assert "kapot" in result.stderr
    assert not gate.mark.exists()


def test_gate_loud_when_common_dir_fails_for_reference(gate: Gate) -> None:
    second = gate.tmp / "X2"
    _worktree(gate.project, second, "x2")
    _fail_common_dir_for(gate, gate.project)

    result = gate.run("git commit -m x", second)

    assert result.returncode == 2, result.stdout
    assert "KON NIET CONTROLEREN" in result.stderr
    assert "kapot" in result.stderr
    assert not gate.mark.exists()


def test_gate_loud_without_cat(gate: Gate) -> None:
    bin_dir = tool_bin(gate.tmp / "bin", ("jq", "python3", "git", "cat"), drop="cat")

    # De stubmap blijft vooraan (Gate.run zet hem vóór `path`).
    result = gate.run("git commit -m x", gate.project, path=str(bin_dir))

    assert result.returncode == 2, result.stdout
    assert "ontbrekende tooling" in result.stderr
    assert "cat" in result.stderr
    assert not gate.mark.exists()


def test_gate_loud_without_uv(gate: Gate) -> None:
    # Geen `uv` te vinden: dan is er niet getest, en dat is iets anders dan
    # "tests failed". Geen nep-`uv` in de stubmap, en ook de rest van PATH
    # heeft er geen: zo kan deze test nooit de echte suite starten.
    (gate.stub / "uv").unlink()
    bin_dir = tool_bin(
        gate.tmp / "bin",
        ("jq", "python3", "git", "cat", "dirname", "mktemp", "rm", "date", "mkdir"),
    )

    result = gate.run("git commit -m x", gate.project, path=str(bin_dir))

    assert result.returncode == 2, result.stdout
    assert "Test-gate: KON NIET CONTROLEREN" in result.stderr
    assert "uv" in result.stderr
    assert "tests failed" not in result.stderr
    assert not gate.mark.exists()


def test_gate_blocks_unparseable_commit(gate: Gate) -> None:
    result = gate.run("git commit -m 'x", gate.project)

    assert result.returncode == 2, result.stdout
    assert "KON NIET CONTROLEREN" in result.stderr
    assert not gate.mark.exists()


def test_gate_passes_unparseable_non_commit(gate: Gate) -> None:
    result = gate.run("echo 'x", gate.project)

    assert result.returncode == 0, result.stderr
    assert not gate.mark.exists()


def test_gate_runs_once_per_root(gate: Gate) -> None:
    result = gate.run("git commit -m a && git commit -m b", gate.project)

    assert result.returncode == 0, result.stderr
    assert gate.marks() == [str(gate.project.resolve())]


def test_gate_uses_one_deadline_for_two_roots(gate: Gate) -> None:
    second = gate.tmp / "X2"
    _worktree(gate.project, second, "x2")
    # Nep-`date`: de deadline (nu + 270) valt zo ~5 s na nu. De eerste wortel
    # haalt dat ruim, ook op een drukke runner; de tweede slaapt 8 s en loopt
    # dus over dezelfde deadline.
    # Elke aanroep komt in DATE_LOG: precies één aanroep bewijst dat de
    # deadline gedeeld is — een per wortel herberekende deadline zou hier ook
    # verlopen, dus rc 2 alleen onderscheidt die twee niet.
    date_log = gate.tmp / "date-log"
    gate.add_stub(
        "date",
        '#!/bin/sh\necho call >> "$DATE_LOG"\necho $(( $(/bin/date +%s) - 265 ))\n',
    )

    result = gate.run(
        f"git commit -m a && cd {second} && git commit -m b",
        gate.project,
        UV_SLOW_SECOND="1",
        DATE_LOG=str(date_log),
    )

    assert result.returncode == 2, result.stdout
    assert "deadline van 270 s" in result.stderr
    assert date_log.read_text(encoding="utf-8").splitlines() == ["call"]
    assert gate.marks() == [str(gate.project.resolve()), str(second.resolve())]


def test_gate_handles_space_in_repo_path(tmp_path: Path) -> None:
    spaced = Gate(tmp_path, tmp_path / "a b")

    result = spaced.run(f'cd "{spaced.project}" && git commit -m x', tmp_path)

    assert result.returncode == 0, result.stderr
    assert spaced.marks() == [str(spaced.project.resolve())]


def test_settings_timeout_exceeds_deadline() -> None:
    settings = json.loads(SETTINGS.read_text(encoding="utf-8"))
    entries = [
        hook
        for block in settings["hooks"]["PreToolUse"]
        for hook in block["hooks"]
        if "pre-commit-tests.sh" in hook["command"]
    ]

    assert len(entries) == 1
    assert entries[0]["timeout"] == 300
    assert entries[0]["timeout"] > 270 + 10


def hold_slots(gate: Gate, *names: str) -> list[int]:
    """Neem de genoemde slots over, als een andere gate dat zou doen."""
    gate.slot_dir.mkdir(parents=True, exist_ok=True)
    fds: list[int] = []
    for name in names:
        fd = os.open(
            gate.slot_dir / name, os.O_WRONLY | os.O_CREAT | os.O_APPEND, 0o600
        )
        fds.append(fd)
        fcntl.flock(fd, fcntl.LOCK_EX)
    return fds


def release(fds: list[int]) -> None:
    for fd in fds:
        os.close(fd)


def _slot_is_free(path: Path) -> bool:
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_APPEND, 0o600)
    try:
        fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
    except BlockingIOError:
        return False
    finally:
        os.close(fd)
    return True


def test_gate_waits_then_blocks_when_both_slots_are_held(gate: Gate) -> None:
    fds = hold_slots(gate, "slot-1", "slot-2")
    try:
        result = gate.run("git commit -m x", gate.project)
    finally:
        release(fds)

    assert result.returncode == 2, result.stdout
    assert "KON NIET CONTROLEREN — beide gate-slots bezet na 1 s" in result.stderr
    assert not gate.mark.exists()


def test_gate_takes_the_free_slot(gate: Gate) -> None:
    fds = hold_slots(gate, "slot-1")
    try:
        result = gate.run("git commit -m x", gate.project)
    finally:
        release(fds)

    assert result.returncode == 0, result.stderr
    assert gate.marks() == [str(gate.project.resolve())]


def test_gate_blocks_when_the_lock_dir_cannot_be_made(gate: Gate) -> None:
    blocker = gate.tmp / "geen-map"
    blocker.write_text("ik ben een bestand\n", encoding="utf-8")

    result = gate.run("git commit -m x", gate.project, PAIA_GATE_SLOT_DIR=str(blocker))

    assert result.returncode == 2, result.stdout
    assert "KON NIET CONTROLEREN — gate-slot niet te nemen" in result.stderr
    assert f"lockmap {blocker} niet aan te maken" in result.stderr
    assert not gate.mark.exists()


def test_gate_blocks_when_a_lock_file_cannot_be_opened(gate: Gate) -> None:
    (gate.slot_dir / "slot-1").mkdir(parents=True)

    result = gate.run("git commit -m x", gate.project)

    assert result.returncode == 2, result.stdout
    assert "gate-slot niet te nemen" in result.stderr
    assert f"lockbestanden in {gate.slot_dir} niet te openen" in result.stderr
    assert not gate.mark.exists()


def test_gate_blocks_when_gate_slot_breaks(gate: Gate) -> None:
    # argparse in gate_slot.py weigert de wachttijd: exit 2, geen slotnummer.
    result = gate.run("git commit -m x", gate.project, PAIA_GATE_SLOT_WAIT="geen-getal")

    assert result.returncode == 2, result.stdout
    assert "gate-slot niet te nemen: gate_slot.py gaf exit 2" in result.stderr
    assert not gate.mark.exists()


def test_foreign_commit_takes_no_slot(gate: Gate) -> None:
    foreign = gate.tmp / "Y"
    init_repo(foreign)

    fds = hold_slots(gate, "slot-1", "slot-2")
    try:
        start = time.monotonic()
        result = gate.run(f"cd {foreign} && git commit -m x", gate.tmp)
        elapsed = time.monotonic() - start
    finally:
        release(fds)

    assert result.returncode == 0, result.stderr
    assert elapsed < 5
    assert "overgeslagen — ander project" in result.stdout
    assert not gate.mark.exists()


def test_slot_stays_held_until_deadline_py_ends(gate: Gate) -> None:
    # Nep-`date`: de deadline valt 8 s na nu. Nep-`uv` noteert dat hij draait
    # en slaapt dan lang; zo leeft `deadline.py` tot die deadline.
    gate.add_stub("date", "#!/bin/sh\necho $(( $(/bin/date +%s) - 262 ))\n")
    gate.add_stub("uv", '#!/bin/sh\npwd >> "$UV_MARK"\nsleep 30\n')
    tmpdir = gate.tmp / "tmpdir"
    tmpdir.mkdir()
    env = dict(
        os.environ,
        PATH=f"{gate.stub}{os.pathsep}{os.environ['PATH']}",
        CLAUDE_PROJECT_DIR=str(gate.project),
        UV_MARK=str(gate.mark),
        PAIA_GATE_SLOT_DIR=str(gate.slot_dir),
        PAIA_GATE_CGROUP_ROOT=str(gate.cgroup),
        PAIA_GATE_SLOT_WAIT="1",
        TMPDIR=str(tmpdir),
    )
    payload = json.dumps(bash_payload("git commit -m x", gate.project))
    start = time.monotonic()
    hook = subprocess.Popen(
        [str(HOOK)],
        stdin=subprocess.PIPE,
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
        text=True,
        cwd=str(gate.project),
        env=env,
    )
    assert hook.stdin is not None
    hook.stdin.write(payload)
    hook.stdin.close()
    try:
        while not gate.marks():
            assert time.monotonic() - start < 15, "nep-uv startte niet"
            time.sleep(0.05)

        # De hook (bash) sneuvelt terwijl pytest loopt: deadline.py houdt het
        # slot vast tot de deadline plus zijn grace van 10 s.
        os.kill(hook.pid, signal.SIGKILL)
        hook.wait()
        slot = gate.slot_dir / "slot-1"
        assert not _slot_is_free(slot)

        while not _slot_is_free(slot):
            assert time.monotonic() - start < 8 + 10 + 2, "slot kwam niet vrij"
            time.sleep(0.2)
    finally:
        if hook.poll() is None:
            hook.kill()
            hook.wait()


BRACE_FD = re.compile(r"\{[A-Za-z_][A-Za-z0-9_]*\}[<>]")


def test_hook_has_no_bash4_fd_variables() -> None:
    # Bash 3.2 (macOS /bin/bash) kent `exec {var}>>` niet: de hook zou daar op
    # die regel sterven met een exit die geen 2 is, en de commit gaat ongetest
    # door. Eerst bewijzen dat het patroon de vorm vindt.
    assert BRACE_FD.search("exec {S1}>>f") is not None

    hits = [
        line
        for line in HOOK.read_text(encoding="utf-8").splitlines()
        if not line.lstrip().startswith("#") and BRACE_FD.search(line)
    ]

    assert hits == []


def test_pytest_keeps_only_failed_tmp_paths() -> None:
    config = tomllib.loads((REPO / "pyproject.toml").read_text(encoding="utf-8"))

    assert (
        config["tool"]["pytest"]["ini_options"]["tmp_path_retention_policy"] == "failed"
    )


# --- De rem (#1184, Taak 3) ---------------------------------------------------

# systemd meldt de eigenschappen niet in de volgorde van de `-p`-argumenten;
# de stub draait ze bewust om.
REM_ON_SYSTEMCTL = (
    "#!/bin/sh\n"
    'printf "MemoryMax=9663676416\\nFragmentPath=/x/gates.slice\\n"\n'
    "exit 0\n"
)
OOM_NOTICE = "gate afgeremd (oom-kill in gates.slice tijdens deze run)"
PRESSURE_NOTICE = "gates.slice stond onder geheugendruk"
SIGNAL_NOTICE = "gate gedood door signaal 9 (systemd-oomd in gates.slice?)"


def rem_on(gate: Gate) -> None:
    """Een `systemctl` waarvoor `gates.slice` geïnstalleerd is en een plafond heeft."""
    gate.add_stub("systemctl", REM_ON_SYSTEMCTL)


def events_file(gate: Gate) -> Path:
    return gate.cgroup / "gates.slice" / "memory.events"


def events(gate: Gate, text: str) -> None:
    """De tellers van `gates.slice` vóór de run."""
    path = events_file(gate)
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(text + "\n", encoding="utf-8")


def uv_writing(gate: Gate, text: str, exit_code: int) -> None:
    """Een nep-`uv` die `memory.events` tijdens de run herschrijft."""
    after = gate.tmp / "events-after"
    after.write_text(text + "\n", encoding="utf-8")
    target = events_file(gate)
    gate.add_stub(
        "uv",
        "#!/bin/sh\n"
        'pwd >> "$UV_MARK"\n'
        f'mkdir -p "{target.parent}"\n'
        f'cat "{after}" > "{target}"\n'
        'echo "3 passed in 0.01s"\n'
        f"exit {exit_code}\n",
    )


def test_gate_wraps_pytest_in_the_slice_and_raises_its_score(gate: Gate) -> None:
    rem_on(gate)

    result = gate.run("git commit -m x", gate.project)

    assert result.returncode == 0, result.stderr
    wrap = Path(f"{gate.mark}.wrap").read_text(encoding="utf-8").splitlines()
    choom = Path(f"{gate.mark}.choom").read_text(encoding="utf-8").splitlines()
    assert len(wrap) == 1
    assert "--user --scope --slice=gates.slice --quiet --" in wrap[0]
    assert len(choom) == 1
    assert choom[0].startswith("-n 800 --")
    assert "--maxprocesses 4" in gate.args()[0]
    assert "rem niet beschikbaar" not in result.stdout


@pytest.mark.parametrize(
    ("show", "reason"),
    [
        ("FragmentPath=\nMemoryMax=infinity\n", "gates.slice niet geïnstalleerd"),
        (
            "FragmentPath=/x/gates.slice\nMemoryMax=infinity\n",
            "gates.slice zonder MemoryMax",
        ),
    ],
)
def test_gate_still_tests_without_the_rem(gate: Gate, show: str, reason: str) -> None:
    gate.add_stub("systemctl", f"#!/bin/sh\ncat <<'EOF'\n{show}EOF\n")

    result = gate.run("git commit -m x", gate.project)

    assert result.returncode == 0, result.stderr
    assert gate.marks() == [str(gate.project.resolve())]
    assert f"rem niet beschikbaar: {reason}" in _context(result.stdout)
    assert not Path(f"{gate.mark}.wrap").exists()


def test_gate_reports_a_failing_systemctl(gate: Gate) -> None:
    result = gate.run("git commit -m x", gate.project)

    assert result.returncode == 0, result.stderr
    assert "rem niet beschikbaar: systemctl --user faalde" in _context(result.stdout)
    assert not Path(f"{gate.mark}.wrap").exists()


def test_gate_reports_a_missing_choom(gate: Gate) -> None:
    rem_on(gate)
    (gate.stub / "choom").unlink()
    # Zonder /usr/bin, dus zonder de echte `choom` van de host.
    bin_dir = tool_bin(
        gate.tmp / "bin",
        ("jq", "python3", "git", "cat", "dirname", "mktemp", "rm", "date", "mkdir"),
    )

    result = gate.run("git commit -m x", gate.project, path=str(bin_dir))

    assert result.returncode == 0, result.stderr
    assert gate.marks() == [str(gate.project.resolve())]
    assert "rem niet beschikbaar: geen choom" in _context(result.stdout)


def test_gate_reports_oom_kill_in_the_slice(gate: Gate) -> None:
    rem_on(gate)
    events(gate, "oom_kill 0\nhigh 0")
    uv_writing(gate, "oom_kill 1\nhigh 4", 137)

    result = gate.run("git commit -m x", gate.project)

    assert result.returncode == 2, result.stdout
    assert "Test-gate: KON NIET CONTROLEREN — " + OOM_NOTICE in result.stderr
    assert "Wortel:" in result.stderr
    assert "3 passed" in result.stderr
    assert "tests failed" not in result.stderr


def test_gate_counts_a_missing_events_file_as_zero(gate: Gate) -> None:
    rem_on(gate)
    uv_writing(gate, "oom_kill 1", 1)

    result = gate.run("git commit -m x", gate.project)

    assert result.returncode == 2, result.stdout
    assert OOM_NOTICE in result.stderr
    assert "tests failed" not in result.stderr


def test_gate_ignores_a_negative_delta(gate: Gate) -> None:
    rem_on(gate)
    events(gate, "oom_kill 3")
    uv_writing(gate, "oom_kill 0", 137)

    result = gate.run("git commit -m x", gate.project)

    assert result.returncode == 2, result.stdout
    assert OOM_NOTICE not in result.stderr
    assert SIGNAL_NOTICE in result.stderr


def test_green_run_stays_green_beside_a_killed_neighbour(gate: Gate) -> None:
    rem_on(gate)
    events(gate, "oom_kill 0")
    uv_writing(gate, "oom_kill 1", 0)

    result = gate.run("git commit -m x", gate.project)

    assert result.returncode == 0, result.stderr
    assert gate.marks() == [str(gate.project.resolve())]
    assert OOM_NOTICE not in result.stdout


def test_gate_reports_systemd_run_that_did_not_start(gate: Gate) -> None:
    rem_on(gate)
    gate.add_stub(
        "systemd-run",
        '#!/bin/sh\necho "Failed to start transient scope unit: x" >&2\nexit 1\n',
    )

    result = gate.run("git commit -m x", gate.project)

    assert result.returncode == 2, result.stdout
    assert "Test-gate: KON NIET CONTROLEREN — systemd-run startte niet" in result.stderr
    assert "Failed to start transient scope unit: x" in result.stderr
    assert "tests failed" not in result.stderr
    assert not gate.mark.exists()


@pytest.mark.parametrize(
    "bus_error",
    [
        "Failed to connect to bus: x",
        "Failed to create bus connection: x",
    ],
)
def test_gate_reports_systemd_run_without_a_bus(gate: Gate, bus_error: str) -> None:
    rem_on(gate)
    gate.add_stub(
        "systemd-run",
        f'#!/bin/sh\necho "{bus_error}" >&2\nexit 1\n',
    )

    result = gate.run("git commit -m x", gate.project)

    assert result.returncode == 2, result.stdout
    assert "Test-gate: KON NIET CONTROLEREN — systemd-run startte niet" in result.stderr
    assert bus_error in result.stderr
    assert "tests failed" not in result.stderr
    assert not gate.mark.exists()


def test_deadline_message_names_memory_pressure(gate: Gate) -> None:
    rem_on(gate)
    events(gate, "high 0")
    uv_writing(gate, "high 7", 124)

    result = gate.run("git commit -m x", gate.project)

    assert result.returncode == 2, result.stdout
    lines = result.stderr.splitlines()
    assert "deadline van 270 s" in lines[0]
    assert PRESSURE_NOTICE in lines[1]


def test_deadline_message_unchanged_without_pressure(gate: Gate) -> None:
    rem_on(gate)
    events(gate, "high 0")
    uv_writing(gate, "high 0", 124)

    result = gate.run("git commit -m x", gate.project)

    assert result.returncode == 2, result.stdout
    assert "deadline van 270 s" in result.stderr
    assert "geheugendruk" not in result.stderr


@pytest.mark.parametrize("code", ["137", "247"])
def test_gate_reports_a_signal_kill(gate: Gate, code: str) -> None:
    rem_on(gate)

    result = gate.run("git commit -m x", gate.project, UV_EXIT=code)

    assert result.returncode == 2, result.stdout
    assert "Test-gate: KON NIET CONTROLEREN — " + SIGNAL_NOTICE in result.stderr
    assert "tests failed" not in result.stderr


def test_signal_kill_without_the_rem_names_the_reason_not_the_slice(gate: Gate) -> None:
    # Zonder rem (hier: de standaardstub, `systemctl --user` faalt) is er geen
    # gates.slice en dus ook geen systemd-oomd in die slice.
    result = gate.run("git commit -m x", gate.project, UV_EXIT="137")

    assert result.returncode == 2, result.stdout
    assert (
        "Test-gate: KON NIET CONTROLEREN — gate gedood door signaal 9 "
        "(rem niet beschikbaar: systemctl --user faalde)." in result.stderr
    )
    assert "systemd-oomd" not in result.stderr
    assert "tests failed" not in result.stderr


@pytest.mark.skipif(
    not Path(f"/run/user/{os.getuid()}").is_dir(), reason="geen /run/user"
)
def test_gate_finds_the_user_bus_without_xdg(gate: Gate) -> None:
    # VS Code-`claude` in de logind-scope heeft geen XDG_RUNTIME_DIR; zonder
    # bus faalt `systemctl --user` en valt de rem stil uit.
    gate.add_stub(
        "systemctl",
        "#!/bin/sh\n"
        'echo "${XDG_RUNTIME_DIR:-<leeg>}" > "$UV_MARK.xdg"\n'
        'echo "Failed to connect to bus" >&2\n'
        "exit 1\n",
    )

    result = gate.run(
        "git commit -m x",
        gate.project,
        XDG_RUNTIME_DIR="",
        DBUS_SESSION_BUS_ADDRESS="",
    )

    assert result.returncode == 0, result.stderr
    seen = Path(f"{gate.mark}.xdg").read_text(encoding="utf-8").strip()
    assert seen == f"/run/user/{os.getuid()}"
