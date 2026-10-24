# lighthouse: core@dev
"""Pak één van de gate-slots met `flock`; het slot overleeft dit proces.

De test-gate laat hoogstens twee suites tegelijk draaien (#1184). De bash van de
hook opent de lockbestanden als fd 8 en 9; dit script erft die fd's, probeert ze
in volgorde met `flock(LOCK_EX | LOCK_NB)` en print het 1-gebaseerde nummer van
het eerste slot dat lukt. Zijn ze allemaal bezet, dan probeert het elke
`interval` seconden opnieuw tot het wachtplafond, en eindigt dan met
`SLOT_BUSY_EXIT`.

Waarom Python en geen `flock(1)`: macOS levert dat commando niet mee, en de hook
draait daar ook.

Waarom het slot blijft staan: een `flock` hoort bij de *open file description*,
niet bij het proces. Die description is dezelfde als die van de bash die de fd
opende. Eindigt dit script, dan blijft de lock dus staan zolang de bash (en
`deadline.py`, dat de fd erft) hem openhoudt; pas als die beide weg zijn, gaat
het slot vrij. De hook sluit zelf de fd van het slot dat hij niet kreeg.
"""

# `X | None` in annotaties: de hooks draaien ook onder python3 3.9 (macOS).
from __future__ import annotations

import argparse
import fcntl
import sys
import time

SLOT_BUSY_EXIT = 75


def acquire(fds: list[int], wait: float, interval: float = 1.0) -> int | None:
    """Pak het eerste vrije slot; geef de 1-gebaseerde index, of None na `wait` seconden.

    De eerste ronde is direct; daarna slaapt het `interval` tussen de rondes tot
    `time.monotonic()` voorbij start + `wait` is. `BlockingIOError` betekent
    "bezet"; elke andere `OSError` (bv. een gesloten fd) gaat omhoog, want dan is
    er niet getest of het slot vrij was.
    """
    deadline = time.monotonic() + wait
    while True:
        for index, fd in enumerate(fds, start=1):
            try:
                fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
            except BlockingIOError:
                continue
            return index
        if time.monotonic() >= deadline:
            return None
        time.sleep(interval)


def main(argv: list[str]) -> int:
    parser = argparse.ArgumentParser(prog="gate_slot.py")
    parser.add_argument("--fd", type=int, action="append", required=True)
    parser.add_argument("--wait", type=float, required=True)
    args = parser.parse_args(argv)
    try:
        slot = acquire(args.fd, args.wait)
    except OSError as err:
        sys.stderr.write(f"gate_slot.py: {err}\n")
        return 1
    if slot is None:
        return SLOT_BUSY_EXIT
    sys.stdout.write(f"{slot}\n")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
