#!/usr/bin/env python3
"""Lees het usage-venster van het Codex-abonnement (issue #1117).

WAAROM
------
De controller-run van #1015 loopt als achtergrondrun door de werkweek heen en deelt het
ChatGPT Plus-quotum met #988 en met de spec- en plan-reviews. Voor Claude weten we hoe vol
het venster is (`usage_window.py`); voor Codex merk je de limiet pas als een run faalt.

WAAROM DE APP-SERVER
--------------------
Het sessielog (`<CODEX_HOME>/sessions/**/rollout-*.jsonl`) is juist, maar zo oud als de
laatste `codex exec` in dat home. `codex app-server` (JSON-RPC over stdio) heeft een methode
`account/rateLimits/read` die in ~0,7 s de verse, account-brede stand geeft en GEEN modelbeurt
start, dus geen quotum kost. Het antwoord bevat ook de handmatige reset-tegoeden.

Het protocol heet `[experimental]`; een Codex-update kan het wijzigen. Elke fout geeft daarom
`NietMeetbaar`, nooit een stand met 0%.

NOOIT `account/rateLimitResetCredit/consume`: een reset inzetten is onomkeerbaar en blijft
een handeling van de gebruiker.

Ontwerp: docs/developer/planning/specs/2026-09-28-codex-usage-venster-design.md

BUDGET VOOR DE REVIEWS (#1093)
------------------------------
`budget` beslist vóór een spec- of plan-review-ronde: Codex of de fallback (exit 0 of 6, één
JSON-object op stdout). Het gebruikt eigen drempels (`review_pct_session`, `review_pct_week`)
en schrijft nooit een marker, stelt nooit een vraag en leest `PAIA_CONTROLLER_RUN` niet: een
review wil alleen weten of Codex nog past, niet de run parkeren.
Ontwerp: docs/developer/planning/specs/2026-10-03-codex-budget-review-design.md
"""

from __future__ import annotations

import argparse
import contextlib
import json
import os
import re
import selectors
import subprocess
import sys
import time
from collections.abc import Iterator, Sequence
from dataclasses import asdict, dataclass, replace
from datetime import UTC, datetime
from pathlib import Path

CLIENT_INFO = {"name": "paia-codex-usage", "version": "1"}


class NietMeetbaar(Exception):
    """De stand is niet te lezen; de boodschap is de reden."""


def codex_homes() -> list[Path]:
    """De twee `CODEX_HOME`s: de standaard en die van de review-skills."""
    home = Path.home()
    return [home / ".codex", home / ".codex-specreview"]


def fmt_ts(dt: datetime) -> str:
    return dt.astimezone(UTC).strftime("%Y-%m-%dT%H:%M:%SZ")


def _stuur(proc: subprocess.Popen[bytes], bericht: dict) -> None:
    assert proc.stdin is not None
    try:
        proc.stdin.write((json.dumps(bericht) + "\n").encode())
        proc.stdin.flush()
    except OSError as exc:
        raise NietMeetbaar(f"app-server niet te schrijven: {exc}") from exc


def _wacht_op_antwoord(
    proc: subprocess.Popen[bytes],
    sel: selectors.BaseSelector,
    buf: bytearray,
    msg_id: int,
    deadline: float,
) -> dict:
    """Lees regels tot het antwoord met `msg_id`; ruis wordt overgeslagen.

    Leest met `os.read` op de fd en een eigen regelbuffer: een gebufferde `readline` kan
    meerdere regels inslikken waarna `select` niets meer ziet.
    """
    assert proc.stdout is not None
    fd = proc.stdout.fileno()
    while True:
        while (eind := buf.find(b"\n")) >= 0:
            regel = bytes(buf[:eind])
            del buf[: eind + 1]
            try:
                bericht = json.loads(regel)
            except ValueError:
                continue
            if not isinstance(bericht, dict) or bericht.get("id") != msg_id:
                continue
            if "error" in bericht:
                raise NietMeetbaar(f"app-server gaf een fout: {bericht['error']}")
            result = bericht.get("result")
            if not isinstance(result, dict):
                raise NietMeetbaar("app-server gaf een antwoord zonder result")
            return result
        rest = deadline - time.monotonic()
        if rest <= 0:
            raise NietMeetbaar("app-server gaf geen antwoord binnen de timeout")
        if not sel.select(timeout=rest):
            continue
        brok = os.read(fd, 65536)
        if not brok:
            raise NietMeetbaar("app-server stopte zonder antwoord")
        buf.extend(brok)


def lees_app_server(home: Path, *, timeout: float) -> dict:
    """Het ruwe `result` van `account/rateLimits/read` voor één `CODEX_HOME`."""
    deadline = time.monotonic() + timeout
    try:
        proc = subprocess.Popen(
            ["codex", "app-server"],
            stdin=subprocess.PIPE,
            stdout=subprocess.PIPE,
            stderr=subprocess.DEVNULL,
            env={**os.environ, "CODEX_HOME": str(home)},
        )
    except FileNotFoundError as exc:
        raise NietMeetbaar("codex niet gevonden") from exc
    except OSError as exc:
        raise NietMeetbaar(f"codex niet te starten: {exc}") from exc

    try:
        assert proc.stdout is not None
        with selectors.DefaultSelector() as sel:
            sel.register(proc.stdout, selectors.EVENT_READ)
            buf = bytearray()
            _stuur(
                proc,
                {
                    "id": 1,
                    "method": "initialize",
                    "params": {"clientInfo": CLIENT_INFO},
                },
            )
            _wacht_op_antwoord(proc, sel, buf, 1, deadline)
            _stuur(proc, {"method": "initialized"})
            _stuur(
                proc,
                {
                    "id": 2,
                    "method": "account/rateLimits/read",
                    "params": {"excludeResetCreditDetails": False},
                },
            )
            return _wacht_op_antwoord(proc, sel, buf, 2, deadline)
    finally:
        if proc.stdin is not None:
            try:
                proc.stdin.close()
            except OSError:
                pass  # de app-server is al weg; de pipe is dan al stuk
        proc.terminate()
        try:
            proc.wait(2)
        except subprocess.TimeoutExpired:
            proc.kill()
            proc.wait()
        if proc.stdout is not None:
            proc.stdout.close()  # na de wait: geen leesser meer, fd niet aan GC overlaten


def _venster(raw: object, naam: str) -> dict:
    if not isinstance(raw, dict):
        raise NietMeetbaar(f"venster {naam} ontbreekt in het app-server-antwoord")
    try:
        return {
            "pct": int(raw["usedPercent"]),
            "reset_at": fmt_ts(datetime.fromtimestamp(raw["resetsAt"], UTC)),
        }
    except (KeyError, TypeError, ValueError, OverflowError, OSError) as exc:
        raise NietMeetbaar(f"venster {naam} onleesbaar: {exc!r}") from exc


def _reset_tegoeden(raw: object) -> dict | None:
    if not isinstance(raw, dict):
        return None
    try:
        vervalt = sorted(
            c["expiresAt"]
            for c in raw.get("credits", [])
            if c.get("status") == "available"
        )
        return {
            "beschikbaar": int(raw["availableCount"]),
            "vervalt": [fmt_ts(datetime.fromtimestamp(t, UTC)) for t in vervalt],
        }
    except (
        KeyError,
        TypeError,
        ValueError,
        AttributeError,
        OverflowError,
        OSError,
    ) as exc:
        raise NietMeetbaar(f"reset-tegoeden onleesbaar: {exc!r}") from exc


def parse_app_server(result: dict, *, now: datetime) -> dict:
    """Zet het `result` van `account/rateLimits/read` om naar de uitvoervorm.

    Accountgegevens worden niet overgenomen: alleen de genoemde velden komen in de stand.
    """
    per_id = result.get("rateLimitsByLimitId")
    limieten = per_id.get("codex") if isinstance(per_id, dict) else None
    if limieten is None:
        limieten = result.get("rateLimits")
    if not isinstance(limieten, dict):
        raise NietMeetbaar("geen rateLimits in het app-server-antwoord")
    return {
        "bron": "app-server",
        "generated_at": fmt_ts(now),
        "leeftijd_s": 0,
        "bronbestand": None,
        "ondergrens": False,
        "session": _venster(limieten.get("primary"), "primary"),
        "week": _venster(limieten.get("secondary"), "secondary"),
        "reset_tegoeden": _reset_tegoeden(result.get("rateLimitResetCredits")),
    }


def _rollout_venster(raw: object) -> dict | None:
    """Een venster uit een logregel; `None` als het geen bruikbaar object is.

    `used_percent` is in het log een float (`54.6`); `int()` kapt af, zodat de ondergrens
    een ondergrens blijft.
    """
    if not isinstance(raw, dict):
        return None
    try:
        return {
            "pct": int(raw["used_percent"]),
            "reset_at": fmt_ts(datetime.fromtimestamp(raw["resets_at"], UTC)),
        }
    except (KeyError, TypeError, ValueError, OverflowError, OSError):
        return None


def _rollout_regels(pad: Path) -> Iterator[tuple[datetime, dict, dict]]:
    """De bruikbare `rate_limits`-regels van een logbestand: (regeltijd, session, week).

    Bruikbaar: `limit_id` is `codex` of ontbreekt, en beide vensters zijn een object. Het log
    bevat ook regels met `limit_id: "premium"` en `primary: null`; die tellen niet. Een
    onleesbare regel of een onleesbaar bestand wordt overgeslagen.
    """
    try:
        with pad.open(encoding="utf-8") as bestand:
            for regel in bestand:
                if "rate_limits" not in regel:
                    continue
                try:
                    rec = json.loads(regel)
                    limieten = rec["payload"]["rate_limits"]
                    if limieten.get("limit_id", "codex") != "codex":
                        continue
                    tijd = datetime.fromisoformat(rec["timestamp"])
                    if tijd.tzinfo is None:
                        continue
                except (ValueError, KeyError, TypeError, AttributeError):
                    continue
                session = _rollout_venster(limieten.get("primary"))
                week = _rollout_venster(limieten.get("secondary"))
                if session is not None and week is not None:
                    yield tijd, session, week
    except (OSError, ValueError):
        return  # onleesbaar bestand (ook geen utf-8): wat al gelezen is, blijft bruikbaar


def lees_rollout(homes: Sequence[Path], *, now: datetime) -> dict:
    """De nieuwste bruikbare `rate_limits`-regel uit de sessielogs van alle `homes`.

    Het log is zo oud als de laatste `codex exec` in dat home: de stand is dus een
    ondergrens (`ondergrens: True`) en `leeftijd_s` zegt hoe oud. "Nieuwste" is de jongste
    regeltijd over alle homes samen. Een ontbrekende home telt als leeg.
    """
    beste: tuple[datetime, dict, dict, Path] | None = None
    for home in homes:
        for pad in sorted((home / "sessions").glob("**/rollout-*.jsonl")):
            for tijd, session, week in _rollout_regels(pad):
                if beste is None or tijd > beste[0]:
                    beste = (tijd, session, week, pad)
    if beste is None:
        raise NietMeetbaar("geen bruikbare rate_limits-regel in de sessielogs")
    tijd, session, week, pad = beste
    return {
        "bron": "rollout-log",
        "generated_at": fmt_ts(now),
        "leeftijd_s": int((now - tijd).total_seconds()),
        "bronbestand": str(pad),
        "ondergrens": True,
        "session": session,
        "week": week,
        "reset_tegoeden": None,
    }


def lees_stand(*, homes: Sequence[Path], timeout: float, now: datetime) -> dict:
    """De stand: eerst de app-server per home, dan het sessielog als ondergrens.

    De eerste home die een leesbare stand geeft wint; het quotum is account-breed, dus de
    tweede home is alleen een reserve voor als de eerste geen auth heeft of niet antwoordt.
    Pas als alle homes falen volgt het log. Faalt ook dat, dan bevat `NietMeetbaar` de
    redenen van beide bronnen.
    """
    redenen: list[str] = []
    for home in homes:
        try:
            return parse_app_server(lees_app_server(home, timeout=timeout), now=now)
        except NietMeetbaar as exc:
            redenen.append(f"{home}: {exc}")
    try:
        return lees_rollout(homes, now=now)
    except NietMeetbaar as exc:
        app_server = "; ".join(redenen) or "geen homes"
        raise NietMeetbaar(f"app-server: {app_server}; rollout-log: {exc}") from exc


@dataclass(frozen=True)
class Uitkomst:
    """De beslissing van de gate: exitcode, tekst, en bij 3/4 het venster en de reset."""

    code: int
    tekst: str
    window: str | None = None
    reset_at: str | None = None


def _lokaal(iso: str) -> str:
    """Een `fmt_ts`-tijd (UTC) als lokale `dd-mm HH:MM` van de host."""
    return datetime.fromisoformat(iso).astimezone().strftime("%d-%m %H:%M")


def _tegoedregel(tegoeden: dict | None) -> str:
    if tegoeden is None:
        return (
            "Reset-tegoeden onbekend (stand uit het sessielog, "
            "de app-server was niet bereikbaar)."
        )
    if tegoeden["beschikbaar"] == 0:
        return "Reset-tegoeden: geen beschikbaar."
    vervalt = ", ".join(_lokaal(v) for v in tegoeden["vervalt"])
    return (
        f"Reset-tegoeden: {tegoeden['beschikbaar']} beschikbaar, vervallen {vervalt}."
    )


def vraagtekst(stand: dict, *, vraag_week: int) -> str:
    """De vraag bij een volle week (exit 4): wachten tot de reset, of zelf een reset inzetten."""
    week = stand["week"]
    return (
        f"Het Codex-weekvenster staat op {week['pct']}% (drempel {vraag_week}%); "
        f"het 5-uursvenster op {stand['session']['pct']}%.\n"
        f"De week-reset is {_lokaal(week['reset_at'])}.\n"
        f"{_tegoedregel(stand['reset_tegoeden'])}\n"
        "Wat wil je?\n"
        "a) Wachten tot de week-reset; de run parkeert tot dan.\n"
        "b) Ik zet zelf een reset in (Codex-UI) en de run gaat daarna verder."
    )


def beslis_gate(
    stand: dict | None, *, halt_5u: int, vraag_week: int, now: datetime
) -> Uitkomst:
    """Puur: de gate-uitkomst voor een stand. `None` is niet meetbaar. Eerste treffer wint.

    De week gaat vóór het 5-uursvenster: een volle week parkeren is zinloos zonder dat de
    gebruiker eerst kiest (exit 4). Een logstand is een ondergrens: boven een drempel telt
    hij, eronder bewijst hij niets; een verlopen reset maakt hem onbruikbaar.
    """
    if stand is None:
        return Uitkomst(5, "niet meetbaar")
    sessie, week = stand["session"], stand["week"]
    ondergrens = stand["ondergrens"]
    # "Elk venster verlopen" begrenst de overschatting na een handmatige reset: een logstand
    # is dan te hoog (veilig), maar hooguit tot de 5u-`reset_at` van die logregel.
    if ondergrens and any(
        datetime.fromisoformat(v["reset_at"]) <= now for v in (sessie, week)
    ):
        return Uitkomst(5, "niet meetbaar: logstand verlopen")
    if week["pct"] >= vraag_week:
        return Uitkomst(
            4, vraagtekst(stand, vraag_week=vraag_week), "codex-week", week["reset_at"]
        )
    if sessie["pct"] >= halt_5u:
        return Uitkomst(
            3, "", "codex-5u", sessie["reset_at"]
        )  # tekst: de gate-opdracht
    if ondergrens:
        return Uitkomst(5, "niet meetbaar: logstand onder de drempels is geen bewijs")
    return Uitkomst(0, f"door: 5u {sessie['pct']}%, week {week['pct']}%")


@dataclass(frozen=True)
class BudgetUitkomst:
    """De beslissing van `budget`; de veldvolgorde is de sleutelvolgorde van de JSON.

    `venster` en `reset_at` zijn alleen gevuld bij `fallback`; `None` is "er is geen".
    """

    besluit: str
    reden: str
    venster: str | None = None
    reset_at: str | None = None
    session_pct: int | None = None
    week_pct: int | None = None
    bron: str | None = None
    ondergrens: bool | None = None
    drempels: dict | None = None


def beslis_budget(
    stand: dict | None, *, review_5u: int, review_week: int, now: datetime
) -> BudgetUitkomst:
    """Puur: `codex`, `fallback` of `niet_meetbaar` voor een review-ronde. Eerste treffer wint.

    Volgt `beslis_gate` (week vóór 5u; een logstand is een ondergrens), maar kent geen
    parkeren of vragen. Een voorbije `reset_at` in het venster dat de drempel raakt is een
    verouderde stand (net als in `_parkeer`): niet meetbaar, geen fallback.
    """
    drempels = {"session": review_5u, "week": review_week}
    if stand is None:
        return BudgetUitkomst("niet_meetbaar", "niet meetbaar", drempels=drempels)
    sessie, week = stand["session"], stand["week"]
    ondergrens = stand["ondergrens"]

    def uitkomst(besluit: str, reden: str, **extra) -> BudgetUitkomst:
        return BudgetUitkomst(
            besluit,
            reden,
            session_pct=sessie["pct"],
            week_pct=week["pct"],
            bron=stand["bron"],
            ondergrens=ondergrens,
            drempels=drempels,
            **extra,
        )

    if ondergrens and any(
        datetime.fromisoformat(v["reset_at"]) <= now for v in (sessie, week)
    ):
        return uitkomst("niet_meetbaar", "niet meetbaar: logstand verlopen")
    for venster, naam, label, pct, drempel in (
        (week, "codex-week", "week", week["pct"], review_week),
        (sessie, "codex-5u", "5u", sessie["pct"], review_5u),
    ):
        if pct < drempel:
            continue
        if datetime.fromisoformat(venster["reset_at"]) <= now:
            reden = f"niet meetbaar: venster rolt nog (reset {venster['reset_at']} is voorbij)"
            return uitkomst("niet_meetbaar", reden)
        return uitkomst(
            "fallback",
            f"{label} {pct}% ≥ {drempel}%",
            venster=naam,
            reset_at=venster["reset_at"],
        )
    if ondergrens:
        return uitkomst(
            "niet_meetbaar", "niet meetbaar: logstand onder de drempels is geen bewijs"
        )
    return uitkomst("codex", f"door: 5u {sessie['pct']}%, week {week['pct']}%")


def _nu() -> datetime:
    return datetime.now(UTC)


def load_config():
    """De `codex_usage`-sectie uit config.yaml.

    Lokale import en repo-root op `sys.path`, zoals `usage_window.load_config`: dit bestand
    draait als los script en `src.config` trekt yaml, dotenv en dacite mee.
    """
    repo_root = str(Path(__file__).resolve().parent.parent)
    if repo_root not in sys.path:
        sys.path.insert(0, repo_root)
    from src.config import get_config

    return get_config().codex_usage


def _config():
    """`load_config()`, met elke laadfout als `NietMeetbaar("config: ...")`.

    De laadfout heeft geen vaste soort (ontbrekend of kapot `config.yaml`, yaml/dacite-fout,
    ImportError van `src.config`); de CLI-contracten kennen alleen 'niet meetbaar'.
    """
    try:
        return load_config()
    except Exception as exc:
        raise NietMeetbaar(f"config: {exc}") from exc


def cmd_read() -> int:
    """Print de stand als JSON (exit 0); niet meetbaar: reden op stderr, exit 1."""
    try:
        stand = lees_stand(
            homes=codex_homes(),
            timeout=_config().app_server_timeout_seconds,
            now=_nu(),
        )
    except NietMeetbaar as exc:
        print(f"niet meetbaar: {exc}", file=sys.stderr)
        return 1
    print(json.dumps(stand, indent=2))
    return 0


def resume_dir() -> Path:
    """De map met usage-markers: `${XDG_DATA_HOME:-~/.local/share}/paia/resume`."""
    basis = os.environ.get("XDG_DATA_HOME") or str(Path.home() / ".local" / "share")
    return Path(basis) / "paia" / "resume"


def controller_sessie() -> str | None:
    """`PAIA_CONTROLLER_RUN` als het een geldige sessie-id is, anders `None`.

    Dezelfde toets als `usage-guard.sh`: alleen `[0-9a-f-]`, niet leeg. De waarde komt in
    een bestandsnaam terecht, dus `../x` of hoofdletters tellen niet.
    """
    waarde = os.environ.get("PAIA_CONTROLLER_RUN", "")
    return waarde if re.fullmatch(r"[0-9a-f-]+", waarde) else None


def schrijf_marker(
    *, sessie: str, window: str, reset_at: str, cwd: str, now: datetime
) -> Path:
    """Schrijf de usage-marker atomisch (`.tmp` + `os.replace`); zelfde vorm als de guard.

    Bij een `OSError` wordt een achtergebleven `.tmp` opgeruimd en de fout opnieuw
    opgeworpen: een parkering zonder marker zou de controller nooit hervatten.
    """
    doel = resume_dir() / f"{sessie}.json"
    tmp = doel.with_name(doel.name + ".tmp")
    marker = {
        "session_id": sessie,
        "cwd": cwd,
        "window": window,
        "reset_at": reset_at,
        "handoff_path": "",
        "created_at": fmt_ts(now),
        "source": "codex-gate",
        "controller": True,
    }
    try:
        doel.parent.mkdir(parents=True, exist_ok=True)
        tmp.write_text(json.dumps(marker, indent=2) + "\n", encoding="utf-8")
        os.replace(tmp, doel)
    except OSError:
        with contextlib.suppress(OSError):
            tmp.unlink(missing_ok=True)
        raise
    return doel


def _parkeer(uitkomst: Uitkomst, stand: dict, *, now: datetime) -> tuple[int, str]:
    """Exit 3: schrijf de marker (alleen in een controller-run) en geef (code, tekst).

    Een voorbije `reset_at` is exit 5 zonder marker: het venster is al gerold.
    """
    assert uitkomst.window is not None and uitkomst.reset_at is not None
    if datetime.fromisoformat(uitkomst.reset_at) <= now:
        # De controller onthoudt een hervatting op markerpad + `reset_at`
        # (`decide._verbruikt`): een marker met een voorbije reset geldt als al afgehandeld
        # en de run zou nooit hervatten.
        return (
            5,
            f"niet meetbaar: venster rolt nog (reset {uitkomst.reset_at} is voorbij)",
        )
    week = uitkomst.window == "codex-week"
    naam = "weekvenster" if week else "5-uursvenster"
    pct = stand["week" if week else "session"]["pct"]
    wanneer = _lokaal(uitkomst.reset_at)
    sessie = controller_sessie()
    if sessie is None:
        return 3, f"Het Codex-{naam} staat op {pct}%; reset om {wanneer}."
    try:
        schrijf_marker(
            sessie=sessie,
            window=uitkomst.window,
            reset_at=uitkomst.reset_at,
            cwd=os.getcwd(),
            now=now,
        )
    except OSError as exc:
        return 5, f"marker niet geschreven: {exc}"
    return 3, (
        f"STOP. Het Codex-{naam} staat op {pct}%. Commit het werk-in-uitvoering en "
        f"stop. De controller hervat deze sessie na {wanneer}."
    )


def _wacht_week_uitkomst(stand: dict | None, *, now: datetime) -> Uitkomst:
    """`--wacht-week`: parkeer tot de week-reset, ongeacht de drempels.

    Niet meetbaar blijft exit 5; een logstand met een verlopen week-reset is dat ook.
    """
    if stand is None:
        return Uitkomst(5, "niet meetbaar")
    week = stand["week"]
    if stand["ondergrens"] and datetime.fromisoformat(week["reset_at"]) <= now:
        return Uitkomst(5, "niet meetbaar: logstand verlopen")
    return Uitkomst(3, "", "codex-week", week["reset_at"])


def cmd_gate(*, halt_5u: int | None, vraag_week: int | None, wacht_week: bool) -> int:
    """Beslis vóór een Codex-aanroep; print de tekst en geef de exitcode (0/3/4/5)."""
    try:
        cfg = _config()
    except NietMeetbaar as exc:
        print(f"niet meetbaar: {exc}")
        return 5
    halt_5u = cfg.halt_pct_session if halt_5u is None else halt_5u
    vraag_week = cfg.vraag_pct_week if vraag_week is None else vraag_week
    now = _nu()
    try:
        stand = lees_stand(
            homes=codex_homes(), timeout=cfg.app_server_timeout_seconds, now=now
        )
    except NietMeetbaar as exc:
        print(f"niet meetbaar: {exc}", file=sys.stderr)
        stand = None
    if wacht_week:
        uitkomst = _wacht_week_uitkomst(stand, now=now)
    else:
        uitkomst = beslis_gate(stand, halt_5u=halt_5u, vraag_week=vraag_week, now=now)
    code, tekst = uitkomst.code, uitkomst.tekst
    if code == 3:
        assert stand is not None  # exit 3 komt alleen uit een stand
        code, tekst = _parkeer(uitkomst, stand, now=now)
    print(tekst)
    return code


def cmd_budget() -> int:
    """Beslis vóór een review-ronde; print één JSON-object, exit 6 bij `fallback`, anders 0."""
    try:
        cfg = _config()
    except NietMeetbaar as exc:
        uitkomst = BudgetUitkomst("niet_meetbaar", f"niet meetbaar: {exc}")
    else:
        now = _nu()
        reden = None
        try:
            stand = lees_stand(
                homes=codex_homes(), timeout=cfg.app_server_timeout_seconds, now=now
            )
        except NietMeetbaar as exc:
            stand, reden = None, f"niet meetbaar: {exc}"
        uitkomst = beslis_budget(
            stand,
            review_5u=cfg.review_pct_session,
            review_week=cfg.review_pct_week,
            now=now,
        )
        if reden is not None:
            uitkomst = replace(uitkomst, reden=reden)
    print(json.dumps(asdict(uitkomst), ensure_ascii=False))
    return 6 if uitkomst.besluit == "fallback" else 0


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description="Lees het Codex-usagevenster (#1117).")
    sub = parser.add_subparsers(dest="cmd", required=True)
    sub.add_parser("read", help="print de stand als JSON")
    gate = sub.add_parser("gate", help="beslis vóór een Codex-aanroep (exit 0/3/4/5)")
    gate.add_argument("--halt-5u", type=int, help="drempel 5-uursvenster (%%)")
    gate.add_argument("--vraag-week", type=int, help="drempel weekvenster (%%)")
    gate.add_argument(
        "--wacht-week",
        action="store_true",
        help="parkeer tot de week-reset, ongeacht de drempels",
    )
    sub.add_parser(
        "budget", help="beslis vóór een review-ronde: codex of fallback (exit 0/6)"
    )
    args = parser.parse_args(argv)
    if args.cmd == "read":
        return cmd_read()
    if args.cmd == "gate":
        return cmd_gate(
            halt_5u=args.halt_5u, vraag_week=args.vraag_week, wacht_week=args.wacht_week
        )
    if args.cmd == "budget":
        return cmd_budget()
    raise AssertionError(f"onbekend subcommando: {args.cmd}")  # argparse weert dit al


if __name__ == "__main__":
    sys.exit(main())
