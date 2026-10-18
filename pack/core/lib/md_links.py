"""Eén linkextractie voor markdown (#886): de norm en de plumbing samen.

Norm: een link in een bestand is relatief en wijst naar een bestaand doel;
een absolute link is altijd een probleem (zie AGENTS.md, regel "Links:
bestand relatief, chat absoluut").

Verhuisd/samengevoegd uit twee eerdere, licht verschillende implementaties:
- de `_strip_code_fences`/`iter_relative_links`-helpers in
  `tests/unit/test_docs_planning.py` (fence- en inline-code-logica);
- de Python-heredoc in `worktree-linkcheck.sh` (linkregex, anker/schema/
  absoluut-filter, padresolutie tegen `root`).

Gebruikt door `worktree-linkcheck.sh` (taak 5, via de CLI hieronder) en door
de docs-linktests (taak 6, via `strip_code`, `relative_links` en
`absolute_links`) — zodat die norm niet langer op twee plekken los
onderhouden wordt.
"""

from __future__ import annotations

import os
import re
import sys
from pathlib import Path
from urllib.parse import unquote

# Drie backticks/tildes als runtime-string, nooit als letterlijke tekst in dit
# bestand — de oude linkhook blokkeert een Edit/Write met drie backticks op
# één regel.
FENCE = "`" * 3
TILDE_FENCE = "~" * 3
_FENCE = re.compile(r"^\s*(" + re.escape(FENCE) + "|" + re.escape(TILDE_FENCE) + ")")
# Inline code-span: `...`. Een pad tussen backticks is documentatie, geen link.
_INLINE_CODE = re.compile(r"`[^`\n]*`")

# Een link volgt CommonMark (#1014): de destination mag tussen hoekhaken staan
# (dan met spaties) en de titel heeft drie vormen: "..", '..' en (..).
LINK = re.compile(
    r"\[[^\]]*\]\("
    r"(?:<(?P<angle>[^<>\n]*)>|(?P<bare>[^)\s<][^)\s]*))"
    r"(?:\s+(?:\"[^\"]*\"|'[^']*'|\([^)]*\)))?"
    r"\s*\)"
)
_SCHEME = re.compile(r"^[a-zA-Z][a-zA-Z0-9+.-]*:")


def link_target(match: re.Match[str]) -> str:
    """Het pad van één `LINK`-treffer, zonder eventuele hoekhaken."""
    angle = match.group("angle")
    return angle if angle is not None else match.group("bare")


def strip_code(text: str) -> str:
    """Verwijder gefencede blokken (drie backticks of drie tildes) en inline code.

    Fences togglen per regel: een regel die, na witruimte, met het fence-teken
    begint schakelt de status om en telt zelf niet mee. Een fence-teken
    MIDDEN in een regel (bijv. in een string-literal) toggelt niets.
    """
    out = []
    inside = False
    for line in text.splitlines():
        if _FENCE.match(line):
            inside = not inside
            continue
        if not inside:
            out.append(_INLINE_CODE.sub("", line))
    return "\n".join(out)


def relative_links(text: str) -> list[str]:
    """Doelen van relatieve markdown-links, buiten code, ontdubbeld op volgorde.

    Sluit uit: een kaal anker (`#kop`), een doel dat met `/` begint (absoluut)
    en een doel met een schema (`https:`, `mailto:`, `obsidian:`, ...). Een
    anker op een verder relatief doel (`b.md#kop`) wordt afgeknipt tot `b.md`.
    """
    seen: set[str] = set()
    out: list[str] = []
    for match in LINK.finditer(strip_code(text)):
        target = link_target(match)
        if target.startswith(("#", "/")):
            continue
        if _SCHEME.match(target):
            continue
        target = target.split("#", 1)[0]
        if not target or target in seen:
            continue
        seen.add(target)
        out.append(target)
    return out


def absolute_links(text: str) -> list[str]:
    """Doelen van markdown-links die met `/` beginnen, buiten code."""
    return [
        link_target(match)
        for match in LINK.finditer(strip_code(text))
        if link_target(match).startswith("/")
    ]


def find_problems(md_path: Path, root: Path) -> list[tuple[str, str]]:
    """Problemen in `md_path`: dode relatieve links en elke absolute link.

    Een relatief doel wordt, na URL-decoding, opgelost tegen `md_path.parent`
    en getoetst op bestaan (bestand of map). Valt het resultaat buiten `root`,
    dan slaat deze functie het over — dat is geen probleem van dit document.
    Een absoluut doel is altijd een probleem, ongeacht bestaan.

    Een `OSError` bij het lezen van `md_path` gaat door naar de aanroeper.
    """
    text = md_path.read_text(encoding="utf-8")
    problems: list[tuple[str, str]] = []

    for target in relative_links(text):
        resolved = os.path.normpath(os.path.join(md_path.parent, unquote(target)))
        rel = os.path.relpath(resolved, root)
        if rel.startswith(".."):
            continue
        if not os.path.exists(resolved):
            problems.append(("dood", target))

    for target in absolute_links(text):
        problems.append(("absoluut", target))

    return problems


def main(argv: list[str] | None = None) -> int:
    """CLI: `md_links.py <md_path> <root>`.

    Print per gevonden probleem één tab-gescheiden regel (`<soort>\\t<doel>`)
    op stdout en geeft 0. Bij een `OSError` (bijv. onleesbaar bestand) gaat de
    melding naar stderr en is de exitcode 1.
    """
    args = sys.argv[1:] if argv is None else argv
    md_path, root = Path(args[0]), Path(args[1])
    try:
        problems = find_problems(md_path, root)
    except OSError as exc:
        print(f"md_links: kon {md_path} niet lezen: {exc}", file=sys.stderr)
        return 1
    for kind, target in problems:
        print(f"{kind}\t{target}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
