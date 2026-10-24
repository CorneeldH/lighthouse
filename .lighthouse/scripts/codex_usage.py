#!/usr/bin/env python3
# lighthouse: core@dev
"""Lees het usage-venster van het Codex-abonnement.

WAAROM
------
Spec- en plan-reviews draaien op Codex en delen het ChatGPT-quotum met ander werk. Voor
Codex merk je de limiet pas als een run faalt; `budget` beslist daarom vóór een
review-ronde of Codex nog past.

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

BUDGET VOOR DE REVIEWS
----------------------
`budget` beslist vóór een spec- of plan-review-ronde: Codex of de fallback (exit 0 of 6, één
JSON-object op stdout). Het gebruikt eigen drempels (`[codex_usage] review_pct_session`,
`review_pct_week` in `lighthouse.toml`) en schrijft nooit een marker en stelt nooit een vraag:
een review wil alleen weten of Codex nog past.

Alleen `read` en `budget` zitten in dit pakket (besluit 26). De gate, die een run parkeert,
hoort bij de usage-guard en zit hier niet in.

Output keys and values are fixed English tokens; a skill or `review.sh` parses them.
"""

from __future__ import annotations

import argparse
import json
import os
import selectors
import subprocess
import sys
import time
from collections.abc import Iterator, Sequence
from dataclasses import asdict, dataclass, replace
from datetime import UTC, datetime
from pathlib import Path

# Run as a file (`python3 .lighthouse/scripts/codex_usage.py`): put `.lighthouse/lib`
# first, so `lighthouse_lib` imports from this installation.
sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "lib"))

from lighthouse_lib import config  # noqa: E402
from lighthouse_lib.config import ConfigError  # noqa: E402

CLIENT_INFO = {"name": "lighthouse-codex-usage", "version": "1"}


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
        raise NietMeetbaar(f"app-server not writable: {exc}") from exc


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
                raise NietMeetbaar(f"app-server returned an error: {bericht['error']}")
            result = bericht.get("result")
            if not isinstance(result, dict):
                raise NietMeetbaar("app-server returned an answer without result")
            return result
        rest = deadline - time.monotonic()
        if rest <= 0:
            raise NietMeetbaar("app-server gave no answer within the timeout")
        if not sel.select(timeout=rest):
            continue
        brok = os.read(fd, 65536)
        if not brok:
            raise NietMeetbaar("app-server stopped without an answer")
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
        raise NietMeetbaar("codex not found") from exc
    except OSError as exc:
        raise NietMeetbaar(f"codex could not start: {exc}") from exc

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
        raise NietMeetbaar(f"window {naam} missing from the app-server answer")
    try:
        return {
            "pct": int(raw["usedPercent"]),
            "reset_at": fmt_ts(datetime.fromtimestamp(raw["resetsAt"], UTC)),
        }
    except (KeyError, TypeError, ValueError, OverflowError, OSError) as exc:
        raise NietMeetbaar(f"window {naam} unreadable: {exc!r}") from exc


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
            "available": int(raw["availableCount"]),
            "expires": [fmt_ts(datetime.fromtimestamp(t, UTC)) for t in vervalt],
        }
    except (
        KeyError,
        TypeError,
        ValueError,
        AttributeError,
        OverflowError,
        OSError,
    ) as exc:
        raise NietMeetbaar(f"reset credits unreadable: {exc!r}") from exc


def parse_app_server(result: dict, *, now: datetime) -> dict:
    """Zet het `result` van `account/rateLimits/read` om naar de uitvoervorm.

    Accountgegevens worden niet overgenomen: alleen de genoemde velden komen in de stand.
    """
    per_id = result.get("rateLimitsByLimitId")
    limieten = per_id.get("codex") if isinstance(per_id, dict) else None
    if limieten is None:
        limieten = result.get("rateLimits")
    if not isinstance(limieten, dict):
        raise NietMeetbaar("no rateLimits in the app-server answer")
    return {
        "source": "app-server",
        "generated_at": fmt_ts(now),
        "age_s": 0,
        "source_file": None,
        "lower_bound": False,
        "session": _venster(limieten.get("primary"), "primary"),
        "week": _venster(limieten.get("secondary"), "secondary"),
        "reset_credits": _reset_tegoeden(result.get("rateLimitResetCredits")),
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
    ondergrens (`lower_bound: True`) en `age_s` zegt hoe oud. "Nieuwste" is de jongste
    regeltijd over alle homes samen. Een ontbrekende home telt als leeg.
    """
    beste: tuple[datetime, dict, dict, Path] | None = None
    for home in homes:
        for pad in sorted((home / "sessions").glob("**/rollout-*.jsonl")):
            for tijd, session, week in _rollout_regels(pad):
                if beste is None or tijd > beste[0]:
                    beste = (tijd, session, week, pad)
    if beste is None:
        raise NietMeetbaar("no usable rate_limits line in the session logs")
    tijd, session, week, pad = beste
    return {
        "source": "rollout-log",
        "generated_at": fmt_ts(now),
        "age_s": int((now - tijd).total_seconds()),
        "source_file": str(pad),
        "lower_bound": True,
        "session": session,
        "week": week,
        "reset_credits": None,
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
        app_server = "; ".join(redenen) or "no homes"
        raise NietMeetbaar(f"app-server: {app_server}; rollout-log: {exc}") from exc


@dataclass(frozen=True)
class BudgetUitkomst:
    """De beslissing van `budget`; de veldvolgorde is de sleutelvolgorde van de JSON.

    `window` en `reset_at` zijn alleen gevuld bij `fallback`; `None` is "er is geen".
    """

    decision: str
    reason: str
    window: str | None = None
    reset_at: str | None = None
    session_pct: int | None = None
    week_pct: int | None = None
    source: str | None = None
    lower_bound: bool | None = None
    thresholds: dict | None = None


def beslis_budget(
    stand: dict | None, *, review_5u: int, review_week: int, now: datetime
) -> BudgetUitkomst:
    """Puur: `codex`, `fallback` of `not_measurable` voor een review-ronde. Eerste treffer
    wint.

    De week gaat vóór het 5-uursvenster; een logstand is een ondergrens: boven een drempel
    telt hij, eronder bewijst hij niets, en een verlopen reset maakt hem onbruikbaar. Een
    voorbije `reset_at` in het venster dat de drempel raakt is een verouderde stand: niet
    meetbaar, geen fallback.
    """
    drempels = {"session": review_5u, "week": review_week}
    if stand is None:
        return BudgetUitkomst("not_measurable", "not measurable", thresholds=drempels)
    sessie, week = stand["session"], stand["week"]
    ondergrens = stand["lower_bound"]

    def uitkomst(besluit: str, reden: str, **extra) -> BudgetUitkomst:
        return BudgetUitkomst(
            besluit,
            reden,
            session_pct=sessie["pct"],
            week_pct=week["pct"],
            source=stand["source"],
            lower_bound=ondergrens,
            thresholds=drempels,
            **extra,
        )

    if ondergrens and any(
        datetime.fromisoformat(v["reset_at"]) <= now for v in (sessie, week)
    ):
        return uitkomst("not_measurable", "not measurable: log reading expired")
    for venster, naam, label, pct, drempel in (
        (week, "codex-week", "week", week["pct"], review_week),
        (sessie, "codex-5h", "5h", sessie["pct"], review_5u),
    ):
        if pct < drempel:
            continue
        if datetime.fromisoformat(venster["reset_at"]) <= now:
            reden = (
                "not measurable: window still rolling "
                f"(reset {venster['reset_at']} has passed)"
            )
            return uitkomst("not_measurable", reden)
        return uitkomst(
            "fallback",
            f"{label} {pct}% ≥ {drempel}%",
            window=naam,
            reset_at=venster["reset_at"],
        )
    if ondergrens:
        return uitkomst(
            "not_measurable",
            "not measurable: log reading below the thresholds is no proof",
        )
    return uitkomst("codex", f"ok: 5h {sessie['pct']}%, week {week['pct']}%")


def _nu() -> datetime:
    return datetime.now(UTC)


@dataclass(frozen=True)
class CodexUsageConfig:
    """The `[codex_usage]` keys that `read` and `budget` use."""

    app_server_timeout_seconds: int
    review_pct_session: int
    review_pct_week: int


def load_config() -> CodexUsageConfig:
    """`[codex_usage]` from `lighthouse.toml`, with the defaults of the config reader."""
    cfg = config.load(config.config_path())
    return CodexUsageConfig(
        **{
            name: config.lookup(cfg, f"codex_usage.{name}")
            for name in (
                "app_server_timeout_seconds",
                "review_pct_session",
                "review_pct_week",
            )
        }
    )


def _config() -> CodexUsageConfig:
    """`load_config()`, met elke laadfout als `NietMeetbaar("config: ...")`.

    De CLI-contracten kennen alleen 'niet meetbaar'.
    """
    try:
        return load_config()
    except ConfigError as exc:
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
        print(f"not measurable: {exc}", file=sys.stderr)
        return 1
    print(json.dumps(stand, indent=2))
    return 0


def cmd_budget() -> int:
    """Beslis vóór een review-ronde; print één JSON-object, exit 6 bij `fallback`, anders 0."""
    try:
        cfg = _config()
    except NietMeetbaar as exc:
        # Het 0/6-contract blijft, maar een kapotte config mag niet alleen in de JSON staan.
        print(f"codex-usage: {exc}", file=sys.stderr)
        uitkomst = BudgetUitkomst("not_measurable", f"not measurable: {exc}")
    else:
        now = _nu()
        reden = None
        try:
            stand = lees_stand(
                homes=codex_homes(), timeout=cfg.app_server_timeout_seconds, now=now
            )
        except NietMeetbaar as exc:
            stand, reden = None, f"not measurable: {exc}"
        uitkomst = beslis_budget(
            stand,
            review_5u=cfg.review_pct_session,
            review_week=cfg.review_pct_week,
            now=now,
        )
        if reden is not None:
            uitkomst = replace(uitkomst, reason=reden)
    print(json.dumps(asdict(uitkomst), ensure_ascii=False))
    return 6 if uitkomst.decision == "fallback" else 0


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description="Read the Codex usage window.")
    sub = parser.add_subparsers(dest="cmd", required=True)
    sub.add_parser("read", help="print the reading as JSON")
    sub.add_parser(
        "budget", help="decide before a review round: codex or fallback (exit 0/6)"
    )
    args = parser.parse_args(argv)
    if args.cmd == "read":
        return cmd_read()
    if args.cmd == "budget":
        return cmd_budget()
    raise AssertionError(f"unknown subcommand: {args.cmd}")  # argparse weert dit al


if __name__ == "__main__":
    sys.exit(main())
