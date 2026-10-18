"""Artefact-comments van de controller: verborgen metadata, zichtbare kop, inhoud.

Een spec of plan staat als versie-comment op het issue. Regel 1 is een HTML-comment
met JSON-metadata (`<!-- paia: {...} -->`), daarna een zichtbare `<sub>`-regel, een lege
regel en de inhoud. Deze module is puur: geen I/O, geen netwerk.

Spec: docs/developer/planning/specs/2026-10-03-spec-plan-als-issue-comment-design.md,
*Formaat*.
"""

from __future__ import annotations

import hashlib
import html
import json
import re
from dataclasses import dataclass, field

from src.controller.comments import Comment
from src.controller.identity import identity

# Maximale lengte van een GitHub-comment, in tekens.
LIMIET_GITHUB = 65_536

SOORTEN = ("spec", "plan")

_METAREGEL = re.compile(r"^<!-- paia: (\{.*\}) -->$")
_SHA = re.compile(r"[0-9a-f]{40}")
_HASH = re.compile(r"[0-9a-f]{12}")


@dataclass(frozen=True)
class Meta:
    soort: str
    versie: int
    deel: int
    delen: int
    bestand: str
    commit: str
    hash: str
    vervangt: str | None
    # Onbekende velden: de parser bewaart ze, `render` schrijft ze terug.
    extra: dict = field(default_factory=dict)


_VELDEN = ("soort", "versie", "deel", "delen", "bestand", "commit", "hash", "vervangt")


def _positief(waarde: object) -> bool:
    return isinstance(waarde, int) and not isinstance(waarde, bool) and waarde >= 1


def parse_meta(body: str) -> Meta | None:
    """Metadata uit de eerste regel van `body`, of `None` als die ontbreekt of ongeldig is."""
    regels = body.strip().splitlines()
    if not regels or not (treffer := _METAREGEL.match(regels[0])):
        return None
    try:
        data = json.loads(treffer.group(1))
    except ValueError:
        return None
    if not isinstance(data, dict) or any(veld not in data for veld in _VELDEN):
        return None
    geldig = (
        data["soort"] in SOORTEN
        and _positief(data["versie"])
        and _positief(data["deel"])
        and _positief(data["delen"])
        and data["deel"] <= data["delen"]
        and isinstance(data["bestand"], str)
        and isinstance(data["commit"], str)
        and _SHA.fullmatch(data["commit"]) is not None
        and isinstance(data["hash"], str)
        and _HASH.fullmatch(data["hash"]) is not None
        and (data["vervangt"] is None or isinstance(data["vervangt"], str))
    )
    if not geldig:
        return None
    extra = {
        sleutel: waarde for sleutel, waarde in data.items() if sleutel not in _VELDEN
    }
    return Meta(**{veld: data[veld] for veld in _VELDEN}, extra=extra)


def inhoud(body: str) -> str:
    """Alles na de metadataregel, de `<sub>`-regel en de lege regel."""
    return "".join(body.lstrip().split("\n", 3)[3:])


def render(meta: Meta, inhoud: str) -> str:
    """Comment-body: metadataregel, zichtbare kop, lege regel, inhoud."""
    data = {veld: getattr(meta, veld) for veld in _VELDEN} | meta.extra
    json_tekst = json.dumps(data, ensure_ascii=False, separators=(",", ":"))
    # `--` kan de HTML-comment voortijdig sluiten.
    if "--" in json_tekst:
        raise ValueError("metadata bevat '--'; dat sluit de HTML-comment")
    pad = html.escape(meta.bestand)
    kop = (
        f"<sub>{meta.soort} · versie {meta.versie} · deel {meta.deel}/{meta.delen} · "
        f'bron <a href="{identity().url}/blob/{meta.commit}/{pad}">{pad}</a> '
        f"@ {meta.commit[:7]}</sub>"
    )
    return f"<!-- paia: {json_tekst} -->\n{kop}\n\n{inhoud}"


def hash_van(data: bytes) -> str:
    """Eerste 12 hex-tekens van sha256 over `data`."""
    return hashlib.sha256(data).hexdigest()[:12]


_FENCE = re.compile(r" {0,3}(`{3,}|~{3,})(.*)")


def _knippunten(tekst: str) -> list[tuple[int, int]]:
    """Alle knippunten als `(offset, klasse)`; klasse 1 is het sterkst.

    Elke regelstart `p > 0` is een knippunt van klasse 4. Klasse 1 (`## `), 2 (`### `) en
    3 (na een lege regel) gelden alleen buiten een codeblok: de regel vóór `p` én de regel
    op `p` beginnen buiten een blok.
    """
    punten: list[tuple[int, int]] = []
    fence: tuple[str, int] | None = None  # (teken, lengte) van het open blok
    vorige_buiten = False
    vorige_leeg = False
    offset = 0
    for regel in tekst.splitlines(keepends=True):
        buiten = fence is None
        if offset > 0:
            klasse = 4
            if vorige_buiten and buiten:
                if regel.startswith("## "):
                    klasse = 1
                elif regel.startswith("### "):
                    klasse = 2
                elif vorige_leeg:
                    klasse = 3
            punten.append((offset, klasse))
        if treffer := _FENCE.match(regel):
            teken, rest = treffer.group(1), treffer.group(2)
            if fence is None:
                fence = (teken[0], len(teken))
            elif teken[0] == fence[0] and len(teken) >= fence[1] and rest.strip() == "":
                fence = None
        vorige_buiten = buiten
        vorige_leeg = regel.strip() == ""
        offset += len(regel)
    return punten


def splits(tekst: str, limiet: int = 60_000) -> list[str]:
    """Knip `tekst` in delen van hoogstens `limiet` tekens, op markdown-grenzen.

    Per deel: de sterkste knippuntklasse in het venster `(start, start + limiet]`, daarbinnen
    het verste punt; zonder knippunt wordt midden in de regel geknipt. Geen deel is leeg
    (behalve bij lege invoer) en de delen samen zijn exact `tekst`.
    """
    punten = _knippunten(tekst)
    delen: list[str] = []
    start = 0
    while len(tekst) - start > limiet:
        kandidaten = [(k, p) for p, k in punten if start < p <= start + limiet]
        if kandidaten:
            sterkste = min(k for k, _ in kandidaten)
            knip = max(p for k, p in kandidaten if k == sterkste)
        else:
            knip = start + limiet
        delen.append(tekst[start:knip])
        start = knip
    delen.append(tekst[start:])
    return delen


def _versies(
    comments: list[Comment], soort: str, bestand: str
) -> dict[int, list[tuple[Meta, Comment]]]:
    """Delen van één keten (`soort` + `bestand`), gegroepeerd per versie."""
    versies: dict[int, list[tuple[Meta, Comment]]] = {}
    for comment in comments:
        meta = parse_meta(comment.body)
        if meta is not None and meta.soort == soort and meta.bestand == bestand:
            versies.setdefault(meta.versie, []).append((meta, comment))
    return versies


def _compleet(delen: list[tuple[Meta, Comment]]) -> bool:
    """Alle delen hebben dezelfde `delen`, en `deel` is exact `1..delen`."""
    totaal = delen[0][0].delen
    nummers = sorted(meta.deel for meta, _ in delen)
    return all(meta.delen == totaal for meta, _ in delen) and nummers == list(
        range(1, totaal + 1)
    )


def huidige(comments: list[Comment], soort: str, bestand: str) -> list[Comment]:
    """Delen van de hoogste complete versie van de keten, op `deel` gesorteerd.

    Onvolledige versies (een post die halverwege faalde) tellen niet mee. `[]` als de
    keten geen complete versie heeft.
    """
    versies = _versies(comments, soort, bestand)
    for versie in sorted(versies, reverse=True):
        if _compleet(versies[versie]):
            gesorteerd = sorted(versies[versie], key=lambda paar: paar[0].deel)
            return [comment for _, comment in gesorteerd]
    return []


def volgende_versie(comments: list[Comment], soort: str, bestand: str) -> int:
    """`max(versies van de keten, ook onvolledige) + 1`; 1 voor een lege keten."""
    return max(_versies(comments, soort, bestand), default=0) + 1
