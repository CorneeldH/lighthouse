"""Git-omgeving zonder repo-bindende variabelen.

Verhuisd uit ratchet.py (#1016 taak 4): `prcov.py` heeft dezelfde schoonmaak
nodig als de coverage-arm, en `ratchet.py` importeert 'm er nu op zijn beurt
weer terug — één bron, twee aanroepers.
"""

from __future__ import annotations

import os
from collections.abc import Mapping

# Variabelen waarmee git een proces aan één repo vastklinkt. git leest ze vóór
# de cwd, dus met GIT_DIR gezet negeert zelfs `git -C /tmp/fixture rev-parse`
# die -C en wijst naar de echte repo.
#
# Dat is hier geen theorie. git zet deze bij het aanroepen van een hook, en de
# coverage-arm was het eerste in deze repo dat de volle pytest-suite ván
# binnen een hook draaide. Zonder deze schoonmaak bouwt elke test die een
# fixture-repo aanmaakt en `git` aanroept in de ECHTE repo:
# `scripts/opencode-lane/lane/cli.py` doet `git add -A` + `git commit`, en dat
# verving de branchtip door een fixture-commit die 702 bestanden uit
# `.planning/` als verwijderd optekende. Twee pushes lang, allebei
# teruggedraaid via de reflog.
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
