"""Gedeeld rekenwerk voor diff-coverage: één implementatie, twee aanroepers.

Gebruikt door scripts/quality/ratchet.py (drempel uit quality-baseline.toml) en
door scripts/opencode-lane/lane/gates.py (die zijn eigen 100%-eis op toegevoegde
src/-regels houdt). Wat gedeeld wordt is het rekenwerk, niet het oordeel.

De diff-parser komt ongewijzigd uit lane/gates.py::_added_src_lines. Het oordeel
eromheen kreeg bij de verhuizing drie correcties (§4.1 van het ontwerp): alleen
`src/**/*.py`, een ontbrekende rapportregel is een fout in plaats van "alles
gemist", en vervolgregels worden op hun statement teruggelegd.
"""

from __future__ import annotations

import ast
import re
from collections.abc import Callable, Iterable, Mapping, Sequence
from dataclasses import dataclass
from typing import Any

_HUNK_RE = re.compile(r"^@@ -\d+(?:,\d+)? \+(?P<start>\d+)(?:,\d+)? @@")

# Velden waarin een samengesteld statement zijn eigen sub-statements draagt.
# Ze bepalen waar de kop van dat statement ophoudt.
_BODY_FIELDS = ("body", "orelse", "finalbody", "handlers", "cases")

# ExceptHandler en match_case zijn geen ast.stmt maar hebben wel een eigen
# regelnummer dat coverage.py als statement rapporteert; zonder hen valt een
# gewijzigde `except X:`- of `case ...:`-regel terug op de omsluitende kop
# (`try`/`match`) en telt een nooit-uitgevoerde `case`-arm als gedekt zodra
# een andere arm draait.
_HEADER_NODES = (ast.stmt, ast.ExceptHandler, ast.match_case)


class CoverageReportError(RuntimeError):
    """Het rapport en de diff passen niet bij elkaar. Fail closed: rood."""


@dataclass(frozen=True)
class DiffCoverage:
    total: int
    uncovered: dict[str, list[int]]

    @property
    def covered(self) -> int:
        return self.total - sum(len(lines) for lines in self.uncovered.values())

    @property
    def pct(self) -> float:
        """100.0 bij een lege noemer — geen deling door nul, en niets te oordelen."""
        return 100.0 if self.total == 0 else 100.0 * self.covered / self.total


def added_lines(diff: str) -> dict[str, list[int]]:
    """{pad: [regelnummers in het nieuwe bestand]} uit een `--unified=0`-diff."""
    added: dict[str, list[int]] = {}
    path = ""
    lineno = 0
    for line in diff.splitlines():
        if line.startswith("+++ "):
            raw = line[4:].strip()
            path = raw.removeprefix("b/")
            continue
        hunk = _HUNK_RE.match(line)
        if hunk:
            lineno = int(hunk.group("start"))
            continue
        if line.startswith("+") and not line.startswith("+++"):
            if path and path != "/dev/null":
                added.setdefault(path, []).append(lineno)
            lineno += 1
        elif line.startswith("-") or line.startswith("\\"):
            continue  # verwijderde regel / "\ No newline" — geen nieuw regelnummer
        elif line.startswith(" "):
            lineno += 1  # contextregel (afwezig bij --unified=0, voor de zekerheid)
    return added


def _lineno(node: ast.AST) -> int | None:
    """Regelnummer van de kop van dit knooppunt.

    match_case draagt zelf geen `lineno` (geen `_attributes`-regel in de
    grammatica); zijn `pattern` — de `case ...`-uitdrukking — wel.
    """
    if isinstance(node, ast.match_case):
        return node.pattern.lineno
    return getattr(node, "lineno", None)


def _spans(tree: ast.AST) -> list[tuple[int, int]]:
    """Per statement de regels die er écht bij horen.

    Voor een samengesteld statement (if/for/def/try/match/case/…) telt alléén
    de kop: zijn body bestaat uit eigen statements met eigen regelnummers.
    Zonder die inperking zou een leegregel middenin een functie op de
    `def`-regel worden teruggelegd en die ten onrechte de noemer in trekken.
    """
    spans: list[tuple[int, int]] = []
    for node in ast.walk(tree):
        if not isinstance(node, _HEADER_NODES):
            continue
        start = _lineno(node)
        if start is None:
            continue
        starts = [
            line
            for field in _BODY_FIELDS
            for child in getattr(node, field, None) or []
            if (line := _lineno(child)) is not None
        ]
        end = (
            min(starts) - 1 if starts else (getattr(node, "end_lineno", None) or start)
        )
        if end >= start:
            spans.append((start, end))
    return spans


def statement_lines(source: str, lines: Iterable[int]) -> set[int]:
    """Leg gewijzigde regelnummers terug op de regel waar hun statement begínt.

    Coverage.py rapporteert per *statement*, en dat is de regel waar het
    statement begint. Een wijziging binnen een multiline-aanroep landt anders op
    een nummer dat in geen van beide sets staat en zou stil uit de noemer vallen
    — ongedekte code die de gate passeert. Dat is de verkeerde richting om fout
    te gaan.

    Meerdere gewijzigde regels binnen hetzelfde statement leveren één nummer op,
    niet drie. Regels die bij geen enkel statement horen (comments, leegregels,
    regels tussen statements) vallen weg — die kún je niet dekken.
    """
    try:
        tree = ast.parse(source)
    except SyntaxError as exc:
        raise CoverageReportError(f"source could not be parsed: {exc}") from exc
    spans = _spans(tree)
    mapped: set[int] = set()
    for number in lines:
        best: tuple[int, int] | None = None
        for start, end in spans:
            if start <= number <= end and (
                best is None or (end - start) < (best[1] - best[0])
            ):
                best = (start, end)
        if best is not None:
            mapped.add(best[0])
    return mapped


def diff_coverage(
    added: Mapping[str, Sequence[int]],
    report: Mapping[str, Any],
    read_source: Callable[[str], str],
    exclude: Sequence[str] = (),
) -> DiffCoverage:
    """Leg de gewijzigde regels tegen `coverage.json` en tel.

    `read_source` is injecteerbaar omdat de opencode-lane zijn bestanden via een
    Runner-seam leest en de ratel gewoon van schijf.

    Een gewijzigd `.py`-bestand dat niet in het rapport staat is een **fout**:
    `--cov=src` noemt élke Python-module onder `src/`, ook een gloednieuwe die
    geen enkele test importeert (geverifieerd op src/web/pages/nexus_page.py,
    die op 0% in het rapport staat). Ontbreekt hij tóch, dan komen rapport en
    diff niet uit dezelfde run en mag de gate niet "dus alles gemist" aannemen.
    """
    files = report.get("files", {})
    total = 0
    uncovered: dict[str, list[int]] = {}
    for path, lines in sorted(added.items()):
        # De gate doet geen uitspraak over niet-Python: er staan 13 getrackte
        # niet-Python-bestanden onder src/ (de browserextensie, een Jinja-template,
        # drie CLAUDE.md) en die kunnen per definitie niet in het rapport staan.
        if not path.endswith(".py"):
            continue
        if any(path.startswith(prefix) for prefix in exclude):
            continue
        entry = files.get(path)
        if entry is None:
            raise CoverageReportError(
                f"no report entry for {path} — report and diff are not from the "
                "same run; not assuming everything was missed"
            )
        executed = set(entry.get("executed_lines", []))
        missing = set(entry.get("missing_lines", []))
        statements = statement_lines(read_source(path), lines)
        # Wat in geen van beide sets staat is niet uitvoerbaar (leegregel,
        # comment, `# pragma: no cover`) en valt uit de noemer.
        relevant = sorted(s for s in statements if s in executed or s in missing)
        total += len(relevant)
        missed = [s for s in relevant if s in missing]
        if missed:
            uncovered[path] = missed
    return DiffCoverage(total=total, uncovered=uncovered)
