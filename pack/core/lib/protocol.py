"""Comment-koppen van de controller: herkennen, parsen en schrijven.

Elke comment van de controller begint met `🤖 <Soort>`. Daaronder staan, tot de
eerste lege regel, velden als `sleutel: waarde`. De voet `<sub>sessie <uuid> ...</sub>`
koppelt een uitkomst aan precies één run; de LAATSTE voet telt, zodat een
sessie-id in vrije tekst niets kan overschrijven.

Spec: docs/developer/planning/specs/2026-09-24-controller-romp-design.md,
*Comment-typen* en *Vraag-format*. Plan: B5 (eind/brainstorm/interactief), B10 (antwoord).
"""

from __future__ import annotations

import re
from collections.abc import Iterable
from dataclasses import dataclass, field
from typing import Literal

KOP = "🤖"
ARTEFACT = "<!-- paia:"
SOORTEN = (
    "Handoff",
    "Gestart",
    "Vraag",
    "Hervat",
    "Klaar",
    "Teruggegeven",
    "Herbaseerd",
)

# Vastgesteld 4. Exact: wat hier niet letterlijk staat, start nooit.
ALLOWLIST = (
    "/paia-start-issue",
    "/paia-spec-adversarial-review",
    "/paia-plan-adversarial-review",
    "/superpowers:writing-plans",
    "/superpowers:subagent-driven-development",
    "/paia-fix-pr",
)
EIND = "/paia-close-issue"
BRAINSTORM = "/superpowers:brainstorming"
# Een plan dat de gebruiker zelf moet begeleiden: nooit headless, altijd terug.
INTERACTIEF = "/superpowers:executing-plans"
# Handoffveld `lane: native` (#1461): dat plan start de controller wél, headless, op dit model.
NATIVE = "native"
NATIVE_MODEL = "sonnet"

Oordeel = Literal["start", "eind", "brainstorm", "interactief", "afgewezen"]

_VELD = re.compile(r"^([a-z_]+):\s?(.*)$")
_SESSIE = re.compile(
    r"<sub>sessie ([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})"
)
# Absoluut pad naar een .md-bestand, zonder spaties of shell-tekens.
_PAD = re.compile(r"/[A-Za-z0-9._/\-]+\.md")
_GETAL = re.compile(r"[0-9]+")
_LETTERS = "abcdef"
_VRAAG_KOP = re.compile(r"^.* · fase (.*?)(?: · taak (.*))?$")
_VRAAG_VELD = re.compile(
    r"^\*\*(Waar het staat|Wat er speelt|Vraag(?: [1-4])?):\*\*(?: (.*))?$"
)
_VRAAG_OPTIE = re.compile(r"^- \*\*[1-4]?[a-f]\)\*\*(?: (.*))?$")
_VRAAG_VELDNAMEN = {
    "Waar het staat": "waar_het_staat",
    "Wat er speelt": "wat_er_speelt",
}
_VRAAG_SLOT = "**Max 1 comment als reactie.**"
# Alleen bij een bundel, direct na de slotregel; voor de mens, de parser slaat hem over.
_VRAAG_BUNDELHINT = "Antwoord per vraag, bijvoorbeeld `1a, 2c`."
_MAX_VRAGEN = 4
# #1351: wat de gebruiker zelf moet draaien, letterlijk, elk commando een eigen codeblok.
_ZELF_DRAAIEN = "**Zelf draaien:**"
FENCE_OPEN = re.compile(r"^(`{3,})bash$")
_AANBEVOLEN = "*(aanbevolen)*"


@dataclass(frozen=True)
class Comment:
    id: int
    body: str
    created_at: str
    rockets: int = 0
    url: str = ""

    @classmethod
    def from_gh(cls, raw: dict) -> Comment:
        """Eén element uit `gh api repos/<repo>/issues/<nr>/comments`."""
        reactions = raw.get("reactions") or {}
        return cls(
            id=int(raw["id"]),
            body=str(raw.get("body") or ""),
            created_at=str(raw.get("created_at") or ""),
            rockets=int(reactions.get("rocket") or 0),
            url=str(raw.get("html_url") or ""),
        )


@dataclass(frozen=True)
class Kop:
    soort: str
    velden: dict[str, str] = field(default_factory=dict)
    sessie: str | None = None


@dataclass(frozen=True)
class Handoff:
    issue: int
    worktree: str
    branch: str
    anker: str
    volgende: str
    wanneer: str
    lane: str = ""


class HandoffFout(ValueError):
    """Een handoff die niet te starten is; de tekst is de reden voor 🤖 Teruggegeven."""


def parse_kop(body: str) -> Kop | None:
    regels = body.strip().splitlines()
    if not regels or not regels[0].startswith(KOP + " "):
        return None
    soort = re.split(r"[\s·]", regels[0][len(KOP) + 1 :].strip(), maxsplit=1)[0]
    if soort not in SOORTEN:
        return None
    velden: dict[str, str] = {}
    for regel in regels[1:]:
        if not regel.strip():
            break
        match = _VELD.match(regel.strip())
        if match:
            velden[match.group(1)] = match.group(2).strip()
    sessies = _SESSIE.findall(body)
    return Kop(soort=soort, velden=velden, sessie=sessies[-1] if sessies else None)


def stapel_basis(bodies: Iterable[str]) -> str | None:
    """Het veld `basis` van de laatste `🤖 Gestart` die er een heeft, ongeacht wat erna komt."""
    basis = None
    for body in bodies:
        kop = parse_kop(body)
        if kop is not None and kop.soort == "Gestart" and kop.velden.get("basis"):
            basis = kop.velden["basis"]
    return basis


def actieve_stapel(bodies: Iterable[str]) -> Kop | None:
    """De laatste `🤖 Gestart` met `basis`, mits er geen `Herbaseerd` of `Teruggegeven` na staat."""
    actief = None
    for body in bodies:
        kop = parse_kop(body)
        if kop is None:
            continue
        if kop.soort == "Gestart" and kop.velden.get("basis"):
            actief = kop
        elif kop.soort in ("Herbaseerd", "Teruggegeven"):
            actief = None
    return actief


def parse_handoff(kop: Kop) -> Handoff:
    if kop.soort != "Handoff":
        raise HandoffFout(f"geen handoff maar {kop.soort}")
    velden = kop.velden
    ontbreekt = [
        k
        for k in ("issue", "worktree", "branch", "volgende", "wanneer")
        if not velden.get(k)
    ]
    if ontbreekt:
        raise HandoffFout("velden ontbreken: " + ", ".join(ontbreekt))
    nummer = re.fullmatch(r"#?([0-9]+)", velden["issue"])
    if nummer is None:
        raise HandoffFout(f"issue onleesbaar: {velden['issue']}")
    if velden["wanneer"] not in ("nu", "nacht"):
        raise HandoffFout(f"wanneer moet nu of nacht zijn, niet {velden['wanneer']}")
    lane = velden.get("lane", "")
    if lane and (lane != NATIVE or velden["volgende"].split()[0] != INTERACTIEF):
        raise HandoffFout(f"lane {lane} alleen native bij {INTERACTIEF}")
    return Handoff(
        issue=int(nummer.group(1)),
        worktree=velden["worktree"].rstrip("/"),
        branch=velden["branch"],
        anker=velden.get("anker", ""),
        volgende=velden["volgende"],
        wanneer=velden["wanneer"],
        lane=lane,
    )


def beoordeel_commando(
    volgende: str, issue: int, lane: str = ""
) -> tuple[Oordeel, str]:
    """Strikt: het commando staat exact op de allowlist, en het argument heeft de
    vorm die bij dat commando hoort. Het tweede element is de reden bij afwijzing.
    `lane == NATIVE` maakt van `INTERACTIEF` een gewone start met een planpad."""
    delen = volgende.split()
    if not delen:
        return "afgewezen", "leeg commando"
    commando, argumenten = delen[0], delen[1:]
    if commando == EIND:
        return "eind", ""
    if commando == BRAINSTORM:
        return "brainstorm", ""
    native = commando == INTERACTIEF and lane == NATIVE
    if commando == INTERACTIEF and not native:
        return "interactief", ""
    if commando not in ALLOWLIST and not native:
        return "afgewezen", f"{commando} staat niet op de allowlist"
    if commando == "/paia-start-issue":
        past = argumenten == [str(issue)]
    elif commando == "/paia-fix-pr":
        past = len(argumenten) == 1 and _GETAL.fullmatch(argumenten[0]) is not None
    else:
        past = len(argumenten) == 1 and _PAD.fullmatch(argumenten[0]) is not None
    if past:
        return "start", ""
    return (
        "afgewezen",
        f"argument past niet bij {commando}: {' '.join(argumenten) or '(geen)'}",
    )


def is_antwoord(body: str) -> bool:
    """Elk mens-comment telt: niet leeg, geen 🤖-kop, en geen artefact-comment.
    Artefact-comments (spec/plan als issue-comment) tellen niet als antwoord zodat
    de controller ze niet als hervatting van een parked sessie interpreteert."""
    tekst = body.strip()
    return bool(tekst) and not tekst.startswith(KOP) and not tekst.startswith(ARTEFACT)


def _schoon(tekst: str) -> str:
    """Vrije tekst kan geen voet en geen codeblok openbreken."""
    return (
        tekst.replace("```", "'''")
        .replace("~~~", "'''")
        .replace("<sub>", "&lt;sub&gt;")
        .strip()
    )


def _codeblok(commando: str) -> list[str]:
    """Eén commando letterlijk als fenced codeblok; de fence is langer dan elke reeks
    backticks in het commando, zodat het blok nooit vroeg sluit (#1351)."""
    langste = max((len(r) for r in re.findall(r"`+", commando)), default=0)
    fence = "`" * max(3, langste + 1)
    return [f"{fence}bash", commando, fence]


def voet(sessie: str, *extra: str) -> str:
    return (
        "<sub>" + " · ".join([f"sessie {sessie}", *[e for e in extra if e]]) + "</sub>"
    )


def render_gestart(
    *,
    sessie: str,
    worktree: str,
    commando: str,
    start: str,
    head_sha: str = "",
    stapel: str = "",
    basis: str = "",
    branch: str = "",
    blokkade: str = "",
    sprong: str = "",
    runner: str = "",
    workspace: str = "",
    terugval: str = "",
) -> str:
    """Een gestapelde start zet `stapel` (`#<x> <branch-x>`), `basis` en `branch`;
    `actieve_stapel` leest ze terug. `blokkade: verrekend` zegt de run dat de stapelregel
    een open blokkeerder al heeft verrekend (poort 0 post dan geen handoff). `sprong` meldt
    dat de controller een review uit de config heeft overgeslagen (#1354). Een forge-run
    (#1058) draagt `runner` en, zodra hij er een heeft, `workspace`; `terugval` is de reden
    waarom een verse forge-start toch headless liep. Elk veld staat er alleen als het niet
    leeg is."""
    regels = [
        "🤖 Gestart",
        f"sessie: {sessie}",
        f"worktree: {worktree}",
        f"commando: {commando}",
        f"start: {start}",
    ]
    for sleutel, waarde in (
        ("head", head_sha),
        ("stapel", stapel),
        ("basis", basis),
        ("branch", branch),
        ("blokkade", blokkade),
        ("sprong", sprong),
        ("runner", runner),
        ("workspace", workspace),
        ("terugval", terugval),
    ):
        if waarde:
            regels.append(f"{sleutel}: {waarde}")
    return "\n".join(regels) + "\n\n" + voet(sessie)


def vul_gestart(bericht: str, **velden: str) -> str:
    """Zet velden in de kop van een `🤖 Gestart`, of vervang ze; de rest blijft staan.

    Apply weet pas ná `voorbereiden` (Taak 8) in welke worktree en met welke runner een
    run echt start. Een bestaand veld houdt zijn plek, een nieuw komt achteraan de kop; een
    lege waarde haalt het veld weg, net als `render_gestart` het dan weglaat. Een waarde is
    één regel: witruimte (ook een newline) wordt één spatie, zodat ze de kop niet openbreekt.
    De voet en alles na de eerste lege regel blijven letterlijk staan."""
    regels = bericht.split("\n")
    einde = next((i for i, r in enumerate(regels) if not r.strip()), len(regels))
    kop = regels[:einde]
    for sleutel, waarde in velden.items():
        schoon = " ".join(str(waarde).split())
        plek = next(
            (i for i, r in enumerate(kop) if i > 0 and r.startswith(f"{sleutel}:")),
            None,
        )
        if not schoon:
            if plek is not None:
                del kop[plek]
        elif plek is not None:
            kop[plek] = f"{sleutel}: {schoon}"
        else:
            kop.append(f"{sleutel}: {schoon}")
    return "\n".join(kop + regels[einde:])


def render_herbaseerd(*, voorganger: int, basis: str, sessie: str | None = None) -> str:
    """Na de squash-merge van de voorganger: de eigen commits staan nu op origin/main."""
    tekst = f"🤖 Herbaseerd\nvoorganger: #{voorganger}\nbasis: {basis}\nop: origin/main"
    if sessie:
        tekst += "\n\n" + voet(sessie)
    return tekst


def antwoord_prompt(vraag: str, antwoord: str) -> str:
    """De hervat-prompt: de vraag zoals gepost, dan het antwoord letterlijk.

    De optieletters zet `render_vraag` erbij, en de classifier kan de opties van de
    sessie herschrijven of samenvoegen. Een kaal `a` zegt de sessie dus niets (#1140);
    met de geposte vraag ervoor kan ze elk antwoord lezen, letter of vrije tekst.
    Voet en slotregel zijn voor de mens en blijven weg."""
    regels = [
        r
        for r in vraag.strip().splitlines()
        if not r.startswith("<sub>")
        and r.strip() not in (_VRAAG_SLOT, _VRAAG_BUNDELHINT)
    ]
    gepost = "\n".join(regels).strip()
    return (
        "Je vraag staat zo op het issue; de optieletters zette de controller erbij:\n\n"
        f"{gepost}\n\nAntwoord van de gebruiker:\n\n{antwoord.strip()}"
    )


def render_hervat(*, sessie: str, antwoord_id: int) -> str:
    return f"🤖 Hervat\nsessie: {sessie}\nantwoord: {antwoord_id}\n\n" + voet(sessie)


def render_klaar(*, sessie: str, pr_url: str) -> str:
    return (
        f"🤖 Klaar\nsessie: {sessie}\npr: {pr_url}\n\n"
        "Checks groen, klaar voor review. Mergen blijft bij jou; na de merge toetst de "
        "controller het issue opnieuw als het nog open staat.\n\n" + voet(sessie)
    )


def render_teruggegeven(
    *, reden: str, sessie: str | None = None, detail: str = ""
) -> str:
    regels = ["🤖 Teruggegeven", "reden: " + " ".join(reden.split())]
    if sessie:
        regels.append(f"sessie: {sessie}")
    tekst = "\n".join(regels)
    if detail.strip():
        tekst += "\n\n```\n" + _schoon(detail) + "\n```"
    if sessie:
        tekst += "\n\n" + voet(sessie)
    return tekst


def vragen_van(velden: dict) -> list[dict]:
    """De vragen van een `velden`-dict: `vragen`, of de oude vorm met één `vraag`/`opties`.

    De oude vorm is een `classify.<n>.json` van vóór de bundel, nog op schijf."""
    if "vragen" in velden:
        return list(velden["vragen"])
    return [{"vraag": velden.get("vraag", ""), "opties": velden.get("opties", [])}]


def _zelf_draaien(velden: dict) -> list[str]:
    """Het `**Zelf draaien:**`-blok: elk commando een eigen codeblok, leeg zonder commando's."""
    commandos = [str(c) for c in velden.get("commandos") or [] if str(c).strip()]
    if not commandos:
        return []
    regels = ["", _ZELF_DRAAIEN]
    for commando in commandos:
        regels += ["", *_codeblok(commando)]
    return [*regels, ""]


def render_vraag(
    *,
    issue: int,
    titel: str,
    issue_url: str,
    velden: dict,
    sessie: str,
    worktree: str,
    handoff_url: str = "",
) -> str:
    kop = f"🤖 Vraag · #{issue} {titel} ([link]({issue_url})) · fase {_schoon(str(velden.get('fase') or '?'))}"
    if velden.get("taak"):
        kop += f" · taak {_schoon(str(velden['taak']))}"
    vragen = vragen_van(velden)[:_MAX_VRAGEN] or [{"vraag": "", "opties": []}]
    bundel = len(vragen) > 1
    regels = [
        kop,
        "",
        f"**Waar het staat:** {_schoon(velden['waar_het_staat'])}",
        f"**Wat er speelt:** {_schoon(velden['wat_er_speelt'])}",
    ]
    # Eén `**Zelf draaien:**`-blok voor de hele Vraag (#1351): bij één vraag onder de
    # vraagregel zoals vóór de bundel, bij een bundel vóór `**Vraag 1:**`.
    zelf_draaien = _zelf_draaien(velden)
    if bundel:
        regels += zelf_draaien
    for nummer, vraag in enumerate(vragen, start=1):
        voorvoegsel = str(nummer) if bundel else ""
        if bundel and nummer > 1:
            # Zonder lege regel wordt `**Vraag n:**` een vervolg van de vorige optie.
            regels.append("")
        regels.append(
            f"**Vraag{' ' + voorvoegsel if bundel else ''}:** {_schoon(vraag['vraag'])}"
        )
        if not bundel:
            regels += zelf_draaien
        opties = list(vraag["opties"])[: len(_LETTERS)]
        for letter, optie in zip(_LETTERS, opties, strict=False):
            regel = f"- **{voorvoegsel}{letter})** {_schoon(optie['tekst'])}"
            if optie.get("aanbevolen"):
                regel += " *(aanbevolen)*"
            regels.append(regel)
    regels += ["", _VRAAG_SLOT]
    if bundel:
        regels.append(_VRAAG_BUNDELHINT)
    regels.append(
        voet(
            sessie,
            f"worktree {worktree}",
            f"handoff {handoff_url}" if handoff_url else "",
        )
    )
    return "\n".join(regels)


def _voeg_optie_toe(vragen: list[dict], tekst: str) -> None:
    """Zet een optie bij de laatste vraag; staat er nog geen, dan bij een lege vraag."""
    if not vragen:
        vragen.append({"vraag": "", "opties": []})
    aanbevolen = tekst == _AANBEVOLEN or tekst.endswith(" " + _AANBEVOLEN)
    if aanbevolen:
        tekst = tekst[: -len(_AANBEVOLEN)].strip()
    vragen[-1]["opties"].append({"tekst": tekst, "aanbevolen": aanbevolen})


def _vraag_stukken(
    regels: list[str],
) -> tuple[list[tuple[str, list[str]]], list[str]]:
    """De regels tussen kop en slot als stukken per veld of optie, en apart de commando's:
    de inhoud van elk codeblok onder `**Zelf draaien:**`, letterlijk. Binnen een codeblok
    begint geen regel een nieuw stuk, ook niet een die op een optie lijkt. Elk Vraag-veld
    (`**Vraag:**` of `**Vraag <n>:**`) opent een stuk `vraag`."""
    stukken: list[tuple[str, list[str]]] = []
    commandos: list[str] = []
    fence: str | None = None
    blok: list[str] = []
    for regel in regels:
        if fence is not None:
            if regel == fence:
                commandos.append("\n".join(blok))
                fence, blok = None, []
            else:
                blok.append(regel)
            continue
        open_ = FENCE_OPEN.match(regel)
        veld = _VRAAG_VELD.match(regel)
        optie = _VRAAG_OPTIE.match(regel)
        if regel == _ZELF_DRAAIEN:
            stukken.append(("commandos", []))
        elif open_ and stukken and stukken[-1][0] == "commandos":
            fence = open_.group(1)
        elif veld:
            naam = _VRAAG_VELDNAMEN.get(veld.group(1), "vraag")
            stukken.append((naam, [veld.group(2) or ""]))
        elif optie:
            stukken.append(("optie", [optie.group(1) or ""]))
        elif stukken:
            stukken[-1][1].append(regel)
    return stukken, commandos


def parse_vraag(body: str) -> dict | None:
    """De inverse van `render_vraag`: de `velden` zoals die erin gingen, geschoond.

    `None` als de comment geen 🤖 Vraag is. Het blok loopt van de lege regel na de kop
    tot de LAATSTE slotregel; een lege regel in vrije tekst hoort bij het lopende
    stuk, alleen de kopregels van een veld of optie beginnen een nieuw stuk."""
    kop = parse_kop(body)
    if kop is None or kop.soort != "Vraag":
        return None
    regels = body.strip().splitlines()
    kopmatch = _VRAAG_KOP.match(regels[0])
    velden: dict = {
        "fase": kopmatch.group(1).strip() if kopmatch else "",
        "taak": (kopmatch.group(2) or "").strip() if kopmatch else "",
        "waar_het_staat": "",
        "wat_er_speelt": "",
        "commandos": [],
        "vragen": [],
    }
    slot = max(
        (i for i, r in enumerate(regels) if r.strip() == _VRAAG_SLOT),
        default=len(regels),
    )
    stukken, velden["commandos"] = _vraag_stukken(regels[1:slot])
    for naam, delen in stukken:
        tekst = "\n".join(delen).strip()
        if naam == "commandos":
            continue
        if naam == "optie":
            _voeg_optie_toe(velden["vragen"], tekst)
        elif naam == "vraag":
            velden["vragen"].append({"vraag": tekst, "opties": []})
        else:
            velden[naam] = tekst
    return velden
