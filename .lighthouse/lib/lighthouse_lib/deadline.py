# lighthouse: core@dev
"""Draai een commando tot een absolute epoch-deadline, kill de procesgroep bij afloop.

Een PreToolUse-hook die zijn eigen timeout haalt, is fail-open per de Claude Code
documentatie — Claude ziet dan geen fout, alleen stilte, en de test-gate (taak 3)
zou onopgemerkt overgeslagen worden. Door de gate zélf op een deadline te laten
eindigen (ruim binnen de hooktimeout) en het hele kind incl. subprocessen hard te
killen, faalt de gate luid in plaats van stil weg te vallen.
"""

# `X | None` in annotaties: de hooks draaien ook onder python3 3.9 (macOS).
from __future__ import annotations

import contextlib
import os
import signal
import subprocess
import sys
import tempfile
import time
from collections.abc import Mapping

TIMEOUT_EXIT = 124
NOT_FOUND_EXIT = 127

# Signalen waarmee de hook zelf gestopt kan worden. Het kind draait in een eigen
# sessie en krijgt ze dus niet vanzelf: zonder doorgeven liep de suite als wees door.
FORWARDED = (signal.SIGTERM, signal.SIGINT, signal.SIGHUP)

USAGE = "usage: deadline.py <epoch-seconds> -- <cmd> [args...]"


class _Stopped(Exception):
    """Een doorgestuurd signaal onderbreekt het wachten op het kind."""

    def __init__(self, signum: int) -> None:
        super().__init__(signum)
        self.signum = signum


class Interrupted(Exception):
    """`run_until(..., raise_on_signal=True)`: een doorgestuurd signaal stopte het kind.

    `signum` is het signaal, `output` wat het kind tot dan toe schreef (`b""`
    zonder `capture`). Zo is "dit proces kreeg een signaal" te onderscheiden
    van een kind dat zelf met 128 + n eindigde.
    """

    def __init__(self, signum: int, output: bytes) -> None:
        super().__init__(signum)
        self.signum = signum
        self.output = output


class _Forwarder:
    """Signaalhandler voor FORWARDED zolang `run_until` loopt.

    Het eerste signaal onderbreekt het wachten (`_Stopped`); daarbuiten, o.a.
    midden in Popen, wordt het alleen genoteerd, zodat er nooit een kind
    zonder beheer ontstaat. Een tweede signaal slaat de grace over: SIGKILL.
    """

    def __init__(self) -> None:
        self.proc: subprocess.Popen | None = None
        self.signum: int | None = None
        self.waiting = False

    def __call__(self, signum: int, _frame: object) -> None:
        if self.signum is not None:
            if self.proc is not None:
                _killpg(self.proc.pid, signal.SIGKILL)
            return
        self.signum = signum
        if self.waiting:
            raise _Stopped(signum)

    def wait(self, proc: subprocess.Popen, timeout: float) -> int:
        """`proc.wait(timeout)`, of `_Stopped` bij een (al eerder) ontvangen signaal."""
        self.proc = proc
        self.waiting = True
        try:
            if self.signum is not None:
                raise _Stopped(self.signum)
            return proc.wait(timeout=timeout)
        finally:
            self.waiting = False


def _killpg(pgid: int, sig: int) -> None:
    try:
        os.killpg(pgid, sig)
    except ProcessLookupError:
        pass


def _stop_group(proc: subprocess.Popen, grace: float) -> None:
    """SIGTERM naar de procesgroep, `grace` seconden wachten, dan SIGKILL."""
    # start_new_session maakt het kind sessie- én groepsleider: pid == pgid.
    # Geen os.getpgid: dat faalt als het kind net na de timeout al weg is.
    pgid = proc.pid
    _killpg(pgid, signal.SIGTERM)
    try:
        proc.wait(timeout=grace)
    except subprocess.TimeoutExpired:
        _killpg(pgid, signal.SIGKILL)
        proc.wait()


def _start(
    argv: list[str],
    cwd: str | None,
    stdin: object,
    out: object,
    env: Mapping[str, str] | None = None,
) -> subprocess.Popen | None:
    """Start `argv` in een eigen sessie; None (plus één regel stderr) als het er niet is."""
    kwargs: dict[str, object] = {}
    if out is not None:
        kwargs["stdout"] = out
        kwargs["stderr"] = subprocess.STDOUT
    try:
        return subprocess.Popen(
            argv, start_new_session=True, cwd=cwd, stdin=stdin, env=env, **kwargs
        )
    except FileNotFoundError:
        sys.stderr.write(f"deadline.py: command not found: {argv[0]}\n")
        return None


def _read_captured(out: object) -> bytes:
    """Lees alles terug uit de gedeelde tempfile, ongeacht waar de schrijfpositie staat."""
    out.seek(0)
    return out.read()


def run_until(
    deadline: float,
    argv: list[str],
    grace: float = 10.0,
    *,
    cwd: str | None = None,
    stdin_bytes: bytes | None = None,
    capture: bool = False,
    raise_on_signal: bool = False,
    env: Mapping[str, str] | None = None,
) -> int | tuple[int, bytes]:
    """Draai `argv` tot `deadline` (epoch-seconden); exitcode of TIMEOUT_EXIT.

    Een deadline die al verstreken is start het commando niet en geeft direct
    TIMEOUT_EXIT (met `capture=True`: `(TIMEOUT_EXIT, b"")`, het commando is
    nooit gestart). Zonder `capture` erven stdout/stderr van de ouder, exact
    het gedrag van vóór deze parameters. Bij verlopen deadline eerst SIGTERM
    naar de hele procesgroep, dan `grace` seconden wachten, daarna SIGKILL.
    Een ontbrekend commando geeft NOT_FOUND_EXIT met één regel op stderr,
    geen traceback: 'niet kunnen starten' is iets anders dan 'tests falen'.
    Krijgt dit proces SIGTERM/SIGINT/SIGHUP, dan stopt het de groep op
    dezelfde manier en geeft 128 + signaalnummer; een tweede signaal tijdens
    het opruimen slaat de grace over en killt direct.

    `cwd` geeft de werkmap van het kind mee (anders erft het die van de
    ouder). `stdin_bytes`, indien gezet, gaat via een `tempfile.TemporaryFile`
    (geseekt naar 0) naar het kind i.p.v. een pipe — bij None erft het kind
    stdin van de ouder, zoals voorheen.

    `capture=True` laat stdout én stderr van het kind naar één gedeelde
    `tempfile.TemporaryFile` schrijven (nooit een pipe: die loopt bij ~64 KiB
    vol terwijl de ouder op het kind wacht) en geeft `(exitcode, output)`
    terug, teruggelezen ná afloop — ook bij TIMEOUT_EXIT (wat er tot dan toe
    geschreven was) en bij NOT_FOUND_EXIT (dan `b""`; de ontbrekend-commando-
    melding gaat in dat geval alleen naar stderr van dit proces, niet in de
    buffer). Zonder `capture` blijft het returntype een kale `int`.

    `env` is the child's whole environment; with None it inherits the parent's,
    as before.

    `raise_on_signal=True` gooit bij een doorgestuurd signaal `Interrupted`
    (ná het stoppen van de groep) in plaats van 128 + signaalnummer terug te
    geven: dat getal kan het kind ook zelf als exitcode hebben.
    """
    remaining = deadline - time.time()
    if remaining <= 0:
        return (TIMEOUT_EXIT, b"") if capture else TIMEOUT_EXIT

    with contextlib.ExitStack() as files:
        stdin_file = None
        if stdin_bytes is not None:
            stdin_file = files.enter_context(tempfile.TemporaryFile())
            stdin_file.write(stdin_bytes)
            stdin_file.seek(0)
        out_file = files.enter_context(tempfile.TemporaryFile()) if capture else None

        forwarder = _Forwarder()
        previous = {sig: signal.signal(sig, forwarder) for sig in FORWARDED}
        try:
            proc = _start(argv, cwd, stdin_file, out_file, env)
            if proc is None:
                return (NOT_FOUND_EXIT, b"") if capture else NOT_FOUND_EXIT
            signum = None
            try:
                code = forwarder.wait(proc, remaining)
                return (code, _read_captured(out_file)) if capture else code
            except subprocess.TimeoutExpired:
                code = TIMEOUT_EXIT
            except _Stopped as stopped:
                signum = stopped.signum
                code = 128 + signum
            _stop_group(proc, grace)
            output = _read_captured(out_file) if capture else b""
            if signum is not None and raise_on_signal:
                raise Interrupted(signum, output)
            return (code, output) if capture else code
        finally:
            for sig, handler in previous.items():
                signal.signal(sig, handler)


def main(argv: list[str]) -> int:
    if len(argv) < 3 or argv[1] != "--":
        sys.stderr.write(USAGE + "\n")
        return 2
    try:
        deadline = float(argv[0])
    except ValueError:
        sys.stderr.write(USAGE + "\n")
        return 2
    cmd = argv[2:]
    return run_until(deadline, cmd)


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
