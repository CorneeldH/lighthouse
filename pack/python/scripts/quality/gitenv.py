"""Git-omgeving zonder repo-bindende variabelen.

`prcov.py` heeft dezelfde schoonmaak nodig als de coverage-arm van
`ratchet.py` — één bron, twee aanroepers.
"""

from __future__ import annotations

import os
from collections.abc import Mapping

# Variabelen waarmee git een proces aan één repo vastklinkt. git leest ze vóór
# de cwd, dus met GIT_DIR gezet negeert zelfs `git -C /tmp/fixture rev-parse`
# die -C en wijst naar de echte repo.
#
# Dat is geen theorie. git zet deze bij het aanroepen van een hook, en de
# coverage-arm draait de volle pytest-suite ván binnen een hook. Zonder deze
# schoonmaak bouwt elke test die een fixture-repo aanmaakt en `git add -A` +
# `git commit` aanroept in de ECHTE repo, en vervangt hij de branchtip door een
# fixture-commit.
_GIT_SCOPING_VARS = (
    "GIT_DIR",
    "GIT_WORK_TREE",
    "GIT_INDEX_FILE",
    "GIT_OBJECT_DIRECTORY",
    "GIT_ALTERNATE_OBJECT_DIRECTORIES",
    "GIT_QUARANTINE_PATH",
    "GIT_COMMON_DIR",
    "GIT_NAMESPACE",
    "GIT_PREFIX",
)


def suite_env(environ: Mapping[str, str] | None = None) -> dict[str, str]:
    """De omgeving voor een git-subprocess, zonder git's repo-bindende variabelen.

    `environ` is injecteerbaar zodat een test het hook-geval kan nabootsen
    zonder os.environ te muteren.
    """
    source = os.environ if environ is None else environ
    return {k: v for k, v in source.items() if k not in _GIT_SCOPING_VARS}
