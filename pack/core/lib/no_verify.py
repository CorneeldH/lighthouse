"""Herken `--no-verify` / `-n` in een `git commit`-commando.

Verhuisd uit een `$(cat <<'PYEOF'`-heredoc in pre-commit-tests.sh (#884): een
heredoc binnen een command substitution is precies de bash-3.2-valkuil
(macOS). Als los bestand is die weg, en is de check los te testen.

Preventief: de testgate is een Claude Code PreToolUse-hook, geen git-hook,
dus de vlag slaat vandaag niets over. Zodra er wél een git-hook komt is dit de
ontsnapping; goedkoop nu dichtzetten. Quoted content (bv. `-m "... --no-verify
..."`) telt niet: de segmentatie van `git_invocations.walk` maakt er één woord van,
en dat woord is dan de waarde van `-m`, geen vlag. Alleen de argumenten van een
`git … commit`-aanroep worden gescand (#1453).

CLI: commando op stdin; exit 2 als de vlag er is, anders 0.
"""

import os
import sys
from pathlib import Path

# Run as a file on its path: put `.lighthouse/lib` first, so `lighthouse_lib`
# imports from this installation.
if not __package__:
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from lighthouse_lib.git_invocations import Unparseable, basename, git_target, walk

# Lange opties die een waarde opeisen: zonder "=" is het volgende token de
# waarde, niet een losse vlag om te scannen (anders zou "-m --no-verify"
# de message-waarde zelf als vlag lezen).
VALUE_LONG_OPTS = {
    "--message",
    "--file",
    "--reuse-message",
    "--reedit-message",
    "--template",
    "--author",
    "--date",
    "--cleanup",
    "--fixup",
    "--squash",
}
# Korte opties die een waarde opeisen (git commit -m/-F/-C/-c/-t). Bij een
# losse -m aan het einde van de cluster is het volgende token de waarde.
VALUE_SHORT_LETTERS = set("mFCct")
# -S en -u nemen ook een waarde, maar die moet vastzitten aan de letter
# (-Skeyid, -uno) — hun waarde is optioneel (git valt terug op een default),
# dus een losse -S of -u aan het einde van de cluster eist NIETS van het
# volgende token. Ze moeten wél de rest van de cluster stoppen met scannen,
# anders leest "-uno" de "n" van "no" als de -n-vlag (issue #591 finding 2).
ATTACHED_ONLY_SHORT_LETTERS = set("uS")


def _scan_short_cluster(letters: str) -> tuple[bool, bool]:
    """Lees een cluster korte opties (`-anm` → `anm`).

    Geeft `(heeft_n, slikt_volgend_token)`. Het scannen stopt bij de eerste
    letter die een waarde neemt: wat erna komt is die waarde, geen vlag.
    """
    for idx, ch in enumerate(letters):
        if ch == "n":
            return True, False
        if ch in VALUE_SHORT_LETTERS:
            return False, idx == len(letters) - 1
        if ch in ATTACHED_ONLY_SHORT_LETTERS:
            return False, False
    return False, False


def has_no_verify(tokens: list[str]) -> bool:
    """Staat `--no-verify` of een `-n` (ook in een cluster) tussen de tokens?"""
    i = 0
    n = len(tokens)
    while i < n:
        tok = tokens[i]
        if tok == "--":
            break
        if tok == "--no-verify":
            return True
        if tok.startswith("--"):
            name = tok.split("=", 1)[0]
            takes_value = name in VALUE_LONG_OPTS and "=" not in tok
            i += 2 if takes_value else 1
            continue
        if tok.startswith("-") and len(tok) > 1:
            found_n, consumes_next = _scan_short_cluster(tok[1:])
            if found_n:
                return True
            i += 2 if consumes_next else 1
            continue
        i += 1
    return False


def command_has_no_verify(command: str) -> bool:
    """`has_no_verify` op de argumenten van elke `git … commit`-aanroep.

    Alleen de woorden ná het subcommando `commit` tellen: een `-n` van
    `grep -n` of `git log -n` in dezelfde opdracht is geen --no-verify
    (#1453). De segmentatie is die van de test-gate zelf (`walk`), dus ook
    `bash -c "…"` en `$(…)` tellen, en een heredoc-body niet.

    Een open quote is hier geen blokkade: de test-gate weigert een
    onparseerbaar commit-commando al eerder, via de commandoparser.
    """
    try:
        for step in walk(command, os.getcwd()):
            if basename(step.words[step.idx]) != "git":
                continue
            action_idx = git_target(step)[3]
            is_commit = (
                action_idx < len(step.words) and step.words[action_idx] == "commit"
            )
            if is_commit and has_no_verify(step.words[action_idx + 1 :]):
                return True
    except Unparseable:
        return False
    return False


def main() -> int:
    return 2 if command_has_no_verify(sys.stdin.read()) else 0


if __name__ == "__main__":
    sys.exit(main())
