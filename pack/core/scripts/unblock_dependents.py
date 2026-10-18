#!/usr/bin/env python3
"""Zet issues vrij die op een zojuist gesloten issue wachtten, als paia-bot.

De vrijgeef-lane (`.github/workflows/unblock-dependents.yml`) draait dit script bij
elke `issues: closed`. Voor elk open issue dat het gesloten issue blokkeerde
(`dependencies/blocking`) telt het de blockers die nog open staan. Staan er geen
meer open en draagt het issue `3-blocked-issue`, dan verdwijnt dat label en krijgt
het issue een comment. Het gesloten issue krijgt één comment terug met wat er
vrijkwam en wat nog wacht.

Normen, bindend voor elke wijziging aan dit bestand:

- **Alleen `completed` geeft vrij.** Een issue dat als `not_planned` of `duplicate`
  dichtging heeft het werk niet gedaan; de blokkade blijft staan en een mens beslist.
- **Alleen wat het label draagt.** Een afhankelijke zonder `3-blocked-issue` wordt
  niet gelezen, niet bewerkt en niet genoemd; een tweede run na een geslaagde
  vrijgave post dus niets nieuws. Een die nog wacht, wordt wel opnieuw genoemd.
- **Eerst de comments, dan het label.** Eerst de terugcomment op het gesloten issue,
  dan per afhankelijke de comment en pas dan de DELETE. Faalt een comment, dan blijft
  het label staan en vangt een herstart het; een 404 op de DELETE betekent "al weg".
- **Niet gelezen is geen nul.** Faalt het lezen van de blockers van één issue, dan
  blijft dat label staan en is de run rood; "kon niet lezen" telt nooit als vrij.
- **Stdlib-only**: het script draait met `uv run --no-project`, zonder `uv sync`.

Waarom een workflow en geen stap in `paia-close-issue`: een sessie mag niet als
paia-bot schrijven (het token bestaat alleen in Actions), en een issue sluit ook
via een PR-merge of met de hand. De skill controleert achteraf. Workflowregels:
`.claude/rules/workflows.md`.
"""

from __future__ import annotations

import argparse
import json
import os
import subprocess
import sys
from collections.abc import Callable, Sequence
from typing import Any

Gh = Callable[[Sequence[str]], str]

LABEL = "3-blocked-issue"
REPO_PAD = "repos/{owner}/{repo}"


class _GhFout(subprocess.CalledProcessError):
    """Zoals `_GhFout` in scripts/chore_merge.py: de stderr zit in `str()`."""

    def __str__(self) -> str:
        basis = super().__str__()
        stderr = (self.stderr or "").strip()
        return f"{basis}\n{stderr}" if stderr else basis


def run_gh(args: Sequence[str]) -> str:
    """Kopie van `run_gh` in scripts/chore_merge.py: standalone."""
    try:
        klaar = subprocess.run(
            ["gh", *args],
            capture_output=True,
            text=True,
            timeout=60,
            check=True,
        )
    except subprocess.CalledProcessError as fout:
        raise _GhFout(fout.returncode, fout.cmd, fout.output, fout.stderr) from fout
    except subprocess.TimeoutExpired as fout:
        melding = f"gh {args[0]}: time-out na {fout.timeout:.0f} s"
        raise _GhFout(-1, fout.cmd, None, melding) from fout
    return klaar.stdout


def _foutregel(fout: Exception) -> str:
    """De oorzaak in een regel: bij een `gh`-fout de stderr, niet de `Command [...]`-kop."""
    if isinstance(fout, subprocess.CalledProcessError):
        for regel in (fout.stderr or "").splitlines():
            if regel.strip():
                return regel.strip()
    if isinstance(fout, KeyError):
        return f"ontbrekend veld {fout}"
    return str(fout)


def _api(gh: Gh, pad: str) -> Any:
    return json.loads(gh(["api", f"{REPO_PAD}/{pad}"]))


def _lijst(gh: Gh, pad: str) -> list[dict[str, Any]]:
    """Een dependency-lijst, of `TypeError` als `gh` iets anders dan een lijst geeft."""
    lijst = _api(gh, f"{pad}?per_page=100")
    if not isinstance(lijst, list):
        raise TypeError(f"{pad} gaf geen JSON-lijst ({type(lijst).__name__})")
    return lijst


def _comment(gh: Gh, nr: int, tekst: str) -> None:
    gh(["api", "-X", "POST", f"{REPO_PAD}/issues/{nr}/comments", "-f", f"body={tekst}"])


def _labels(issue: dict[str, Any]) -> set[str]:
    return {label["name"] for label in issue["labels"]}


def _verwijder_label(gh: Gh, nr: int) -> None:
    """DELETE van het label; een 404 (tussendoor met de hand weggehaald) is geen fout."""
    try:
        gh(["api", "-X", "DELETE", f"{REPO_PAD}/issues/{nr}/labels/{LABEL}"])
    except subprocess.CalledProcessError as fout:
        if "HTTP 404" not in (fout.stderr or ""):
            raise


_LEESFOUTEN = (subprocess.CalledProcessError, ValueError, KeyError, TypeError)


def _lees_afhankelijken(gh: Gh, nr: int) -> tuple[str | None, list[dict[str, Any]]]:
    """(melding, open afhankelijken); een melding betekent: niets te doen, en waarom."""
    issue = _api(gh, f"issues/{nr}")
    if issue["state"] != "closed":
        return f"#{nr} staat open: niets vrij te geven.", []
    if issue["state_reason"] != "completed":
        reden = issue["state_reason"]
        return f"#{nr} gesloten als `{reden}`: blokkades blijven staan.", []
    blocking = _lijst(gh, f"issues/{nr}/dependencies/blocking")
    afhankelijk = [i for i in blocking if i["state"] == "open"]
    if not afhankelijk:
        return f"#{nr} blokkeerde geen open issues.", []
    return None, afhankelijk


def _oordeel(gh: Gh, dep: dict[str, Any]) -> tuple[str, str]:
    """(soort, tekst) voor één afhankelijke, zonder te schrijven: `vrij`, `wacht` of `al`."""
    if LABEL not in _labels(dep):
        return "al", f"draagt geen `{LABEL}`, niets te doen"
    blockers = _lijst(gh, f"issues/{dep['number']}/dependencies/blocked_by")
    open_blockers = [f"#{b['number']}" for b in blockers if b["state"] == "open"]
    if open_blockers:
        return "wacht", f"wacht nog op {', '.join(open_blockers)}"
    return "vrij", f"vrij, `{LABEL}` gaat eraf"


def _oordeel_alle(
    gh: Gh, afhankelijk: list[dict[str, Any]], regels: list[str]
) -> tuple[int, list[int], list[str]]:
    """Fase 1: (exitcode, vrij te geven nummers, regels voor de terugcomment).

    Vult `regels` aan met één summaryregel per afhankelijke.
    """
    code = 0
    vrij: list[int] = []
    terug: list[str] = []
    for dep in afhankelijk:
        m = dep["number"]
        try:
            soort, tekst = _oordeel(gh, dep)
        except _LEESFOUTEN as fout:
            code = 1
            regels.append(f"- #{m}: **Fout:** {_foutregel(fout)}")
            continue
        regels.append(f"- #{m}: {tekst}")
        if soort == "vrij":
            vrij.append(m)
            terug.append(f"- #{m} is vrij: `{LABEL}` gaat eraf")
        elif soort == "wacht":
            terug.append(f"- #{m} {tekst}")
    return code, vrij, terug


def _geef_vrij(gh: Gh, nr: int, m: int) -> None:
    _comment(
        gh,
        m,
        f"Vrij: de laatste open blocker #{nr} is gesloten; "
        f"de vrijgeef-lane haalt `{LABEL}` weg.",
    )
    _verwijder_label(gh, m)


def run(gh: Gh, nr: int, *, dry_run: bool = False) -> tuple[int, list[str]]:
    """Geef de afhankelijken van `nr` vrij; (exitcode, summaryregels).

    Drie fasen: lezen, de terugcomment op `nr`, dan pas de afhankelijken. Faalt de
    terugcomment, dan is er nog niets aangeraakt en doet een herstart alles opnieuw.
    """
    regels = [f"## Vrijgeef-lane voor #{nr}", ""]
    try:
        melding, afhankelijk = _lees_afhankelijken(gh, nr)
    except _LEESFOUTEN as fout:
        return 1, [*regels, f"**Fout:** #{nr} niet gelezen: {_foutregel(fout)}"]
    if melding is not None:
        return 0, [*regels, melding]

    code, vrij, terug = _oordeel_alle(gh, afhankelijk, regels)
    if dry_run:
        return code, [*regels, "", "_dry-run: niets geschreven._"]
    if not terug:
        return code, regels
    try:
        _comment(gh, nr, "\n".join(["Vrijgeef-lane na deze sluiting:", "", *terug]))
    except _LEESFOUTEN as fout:
        regels.append(
            f"**Fout:** comment op #{nr}: {_foutregel(fout)}; niets aangeraakt"
        )
        return 1, regels
    for m in vrij:
        try:
            _geef_vrij(gh, nr, m)
        except _LEESFOUTEN as fout:
            code = 1
            regels.append(f"- #{m}: **Fout:** {_foutregel(fout)}")
    return code, regels


def main(argv: Sequence[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--issue", type=int, required=True, help="het gesloten issue")
    parser.add_argument(
        "--dry-run", action="store_true", help="alleen lezen, schrijf niets"
    )
    args = parser.parse_args(argv)
    code, regels = run(run_gh, args.issue, dry_run=args.dry_run)
    tekst = "\n".join(regels) + "\n"
    sys.stdout.write(tekst)
    summary = os.environ.get("GITHUB_STEP_SUMMARY")
    if summary:
        with open(summary, "a", encoding="utf-8") as bestand:
            bestand.write(tekst)
    return code


if __name__ == "__main__":
    sys.exit(main())
