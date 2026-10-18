"""Unit tests voor `scripts/codex_usage.py` en de Codex-usageconfig (issue #1117).

Config: `CodexUsageConfig` in `src/config.py` komt correct door `load_config()`.
App-server: de parser naar de uitvoervorm, en de lezer tegen een nep-`codex` (een Python-script
in een tijdelijke map op `PATH`). De echte `codex` draait hier nooit: geen quotum, geen netwerk.
"""

from __future__ import annotations

import importlib.util
import json
import os
import sys
import time
from datetime import UTC, datetime
from pathlib import Path

import pytest

from src.controller.runmap import lees_markers
from tests.helpers.controller import NU as NU_CONTROLLER
from tests.helpers.controller import (
    S1,
    beslis_,
    comment,
    handoff,
    invoer,
    issue,
    run,
    uitkomst,
    van_soort,
)

REPO_ROOT = Path(__file__).resolve().parents[2]
FIXTURES = REPO_ROOT / "tests" / "fixtures" / "codex_usage"

# Niet NU: `NU_CONTROLLER` (tests.helpers.controller) is de klok van de controller-tests.
NU_SPEC = datetime(2026, 9, 28, 19, 25, 46, tzinfo=UTC)

_SPEC = importlib.util.spec_from_file_location(
    "codex_usage", REPO_ROOT / "scripts" / "codex_usage.py"
)
assert _SPEC and _SPEC.loader
cu = importlib.util.module_from_spec(_SPEC)
sys.modules[_SPEC.name] = cu
_SPEC.loader.exec_module(cu)


def fixture(name: str) -> dict:
    return json.loads((FIXTURES / name).read_text())


# Vier regels: twee bruikbare `codex`-regels, een `premium`-regel zonder vensters, en ruis.
FRAGMENT = (FIXTURES / "rollout-fragment.jsonl").read_text().splitlines()


def leg_rollout(home: Path, dag: str, naam: str, regels: list[str]) -> Path:
    """Schrijf `regels` als sessielog in `home/sessions/2026/09/<dag>/<naam>`."""
    pad = home / "sessions" / "2026" / "09" / dag / naam
    pad.parent.mkdir(parents=True)
    pad.write_text("\n".join(regels) + "\n")
    return pad


class TestConfig:
    def test_codex_usage_defaults_via_get_config(self):
        from src.config import get_config

        cfg = get_config().codex_usage
        assert (
            cfg.halt_pct_session,
            cfg.vraag_pct_week,
            cfg.app_server_timeout_seconds,
            cfg.review_pct_session,
            cfg.review_pct_week,
        ) == (85, 80, 20, 90, 95)

    def test_codex_usage_block_is_loaded(self, tmp_path):
        """Afwijkende waarden in de yaml moeten doorkomen; gelijke waarden bewijzen niets
        (zo bleef het `usage_window:`-blok jaren dood, zie src/config.py)."""
        import yaml

        from src.config import load_config

        data = yaml.safe_load((REPO_ROOT / "config.yaml").read_text())
        data["codex_usage"] = {
            "halt_pct_session": 70,
            "vraag_pct_week": 60,
            "app_server_timeout_seconds": 5,
            "review_pct_session": 70,
            "review_pct_week": 75,
        }
        pad = tmp_path / "config.yaml"
        pad.write_text(yaml.safe_dump(data))
        cfg = load_config(pad).codex_usage
        assert (
            cfg.halt_pct_session,
            cfg.vraag_pct_week,
            cfg.app_server_timeout_seconds,
            cfg.review_pct_session,
            cfg.review_pct_week,
        ) == (70, 60, 5, 70, 75)

    def test_codex_usage_blokken_gelijk_en_defaults_kloppen(self):
        import dataclasses

        import yaml

        from src.config import CodexUsageConfig

        root = yaml.safe_load((REPO_ROOT / "config.yaml").read_text())["codex_usage"]
        beelink = yaml.safe_load(
            (REPO_ROOT / "config" / "profiles" / "beelink.yaml").read_text()
        )["codex_usage"]
        assert root == beelink
        defaults = {f.name: f.default for f in dataclasses.fields(CodexUsageConfig)}
        assert root == defaults


NEP_CODEX = """#!{python}
import json, os, sys, time

MODUS = {modus!r}
ANTWOORD = {antwoord}

with open({pidpad!r}, "w") as f:
    f.write(str(os.getpid()))


def stuur(obj):
    sys.stdout.write((obj if isinstance(obj, str) else json.dumps(obj)) + "\\n")
    sys.stdout.flush()


if MODUS == "hang":
    time.sleep(60)

for regel in sys.stdin:
    bericht = json.loads(regel)
    if bericht.get("id") == 1:
        stuur({{"id": 1, "result": {{}}}})
    elif bericht.get("id") == 2:
        if MODUS == "ok":
            stuur("niet-json")
            stuur({{"method": "account/rateLimits/updated", "params": {{}}}})
            stuur({{"id": 2, "result": ANTWOORD}})
            time.sleep(60)
        elif MODUS == "error":
            stuur({{"id": 2, "error": {{"code": -32600, "message": "not logged in"}}}})
        elif MODUS == "kapot":
            stuur({{"id": 2, "result": {{"rateLimits": {{"primary": None}}}}}})
"""


@pytest.fixture
def nep_codex(tmp_path, monkeypatch):
    """Zet een nep-`codex` op `PATH`; geeft een functie die de modus kiest."""

    def maak(modus: str) -> Path:
        bin_dir = tmp_path / "bin"
        bin_dir.mkdir(exist_ok=True)
        script = bin_dir / "codex"
        script.write_text(
            NEP_CODEX.format(
                python=sys.executable,
                modus=modus,
                antwoord=repr(fixture("app_server_response.json")),
                pidpad=str(tmp_path / "pid"),
            )
        )
        script.chmod(0o755)
        # Alleen de nep-map en de map van de interpreter: de echte `codex` is onbereikbaar.
        monkeypatch.setenv(
            "PATH", f"{bin_dir}{os.pathsep}{Path(sys.executable).parent}"
        )
        return script

    return maak


class TestParseAppServer:
    def test_uitvoervorm(self):
        stand = cu.parse_app_server(fixture("app_server_response.json"), now=NU_SPEC)
        assert stand == {
            "bron": "app-server",
            "generated_at": "2026-09-28T19:25:46Z",
            "leeftijd_s": 0,
            "bronbestand": None,
            "ondergrens": False,
            "session": {"pct": 55, "reset_at": "2026-09-28T23:23:29Z"},
            "week": {"pct": 66, "reset_at": "2026-10-03T17:17:59Z"},
            "reset_tegoeden": {
                "beschikbaar": 2,
                "vervalt": ["2026-10-04T23:53:00Z", "2026-10-22T20:18:18Z"],
            },
        }

    def test_valt_terug_op_ratelimits(self):
        res = fixture("app_server_response.json")
        del res["rateLimitsByLimitId"]
        assert cu.parse_app_server(res, now=NU_SPEC)["session"]["pct"] == 55

    def test_geen_account_id_in_uitvoer(self):
        res = {**fixture("app_server_response.json"), "accountId": "acc-geheim"}
        assert "acc-geheim" not in json.dumps(cu.parse_app_server(res, now=NU_SPEC))

    @pytest.mark.parametrize("venster", ["primary", "secondary"])
    def test_ontbrekend_venster_niet_meetbaar(self, venster):
        res = fixture("app_server_response.json")
        res["rateLimitsByLimitId"]["codex"][venster] = None
        with pytest.raises(cu.NietMeetbaar):
            cu.parse_app_server(res, now=NU_SPEC)

    def test_float_percentage_wordt_afgekapt(self):
        res = fixture("app_server_response.json")
        res["rateLimitsByLimitId"]["codex"]["primary"]["usedPercent"] = 54.6
        assert cu.parse_app_server(res, now=NU_SPEC)["session"]["pct"] == 54

    def test_zonder_reset_tegoeden_is_none(self):
        res = fixture("app_server_response.json")
        del res["rateLimitResetCredits"]
        assert cu.parse_app_server(res, now=NU_SPEC)["reset_tegoeden"] is None

    def test_alleen_beschikbare_tegoeden_tellen_voor_vervalt(self):
        res = fixture("app_server_response.json")
        res["rateLimitResetCredits"]["credits"][0]["status"] = "used"
        stand = cu.parse_app_server(res, now=NU_SPEC)
        assert stand["reset_tegoeden"]["vervalt"] == ["2026-10-22T20:18:18Z"]


class TestLeesAppServer:
    def test_slaat_ruis_over_en_stopt_het_proces(self, nep_codex, tmp_path):
        nep_codex("ok")
        result = cu.lees_app_server(tmp_path, timeout=10)
        assert result == fixture("app_server_response.json")
        pid = int((tmp_path / "pid").read_text())
        with pytest.raises(ProcessLookupError):
            os.kill(pid, 0)

    def test_geeft_home_door_als_codex_home(self, nep_codex, tmp_path):
        script = nep_codex("ok")
        script.write_text(
            script.read_text().replace(
                "ANTWOORD = ", 'ANTWOORD = {"codex_home": os.environ["CODEX_HOME"]}  # '
            )
        )
        home = tmp_path / "eigen-home"
        assert cu.lees_app_server(home, timeout=10) == {"codex_home": str(home)}

    @pytest.mark.parametrize("modus", ["error", "kapot"])
    def test_fout_is_niet_meetbaar(self, nep_codex, tmp_path, modus):
        nep_codex(modus)
        with pytest.raises(cu.NietMeetbaar):
            # "kapot" levert een result dat pas in parse_app_server faalt.
            cu.parse_app_server(cu.lees_app_server(tmp_path, timeout=10), now=NU_SPEC)

    def test_timeout(self, nep_codex, tmp_path):
        nep_codex("hang")
        start = time.monotonic()
        with pytest.raises(cu.NietMeetbaar):
            cu.lees_app_server(tmp_path, timeout=1)
        assert time.monotonic() - start < 5

    def test_zonder_codex(self, tmp_path, monkeypatch):
        leeg = tmp_path / "leeg"
        leeg.mkdir()
        monkeypatch.setenv("PATH", str(leeg))
        with pytest.raises(cu.NietMeetbaar, match="codex niet gevonden"):
            cu.lees_app_server(tmp_path, timeout=5)


class TestRollout:
    def test_nieuwste_bruikbare_regel(self, tmp_path):
        home = tmp_path / "a"
        leg_rollout(home, "28", "rollout-x.jsonl", FRAGMENT)
        stand = cu.lees_rollout([home], now=datetime(2026, 9, 28, 21, 35, tzinfo=UTC))
        # 54.6 wordt afgekapt; de premium-regel (jonger, zonder vensters) is overgeslagen.
        assert stand["session"] == {"pct": 54, "reset_at": "2026-09-28T23:23:29Z"}
        assert stand["week"]["pct"] == 66
        assert (
            stand["bron"],
            stand["ondergrens"],
            stand["leeftijd_s"],
            stand["reset_tegoeden"],
        ) == ("rollout-log", True, 600, None)
        assert stand["generated_at"] == "2026-09-28T21:35:00Z"
        assert stand["bronbestand"].endswith("rollout-x.jsonl")
        assert list(stand) == [
            "bron",
            "generated_at",
            "leeftijd_s",
            "bronbestand",
            "ondergrens",
            "session",
            "week",
            "reset_tegoeden",
        ]

    def test_jongste_over_twee_homes(self, tmp_path):
        a, b = tmp_path / "a", tmp_path / "b"
        leg_rollout(a, "28", "rollout-a.jsonl", FRAGMENT)
        jonger = FRAGMENT[1].replace("21:25:00", "21:28:00").replace("54.6", "70.2")
        pad_b = leg_rollout(b, "28", "rollout-b.jsonl", [jonger])
        stand = cu.lees_rollout([a, b], now=datetime(2026, 9, 28, 21, 35, tzinfo=UTC))
        assert stand["session"]["pct"] == 70
        assert stand["leeftijd_s"] == 420
        assert stand["bronbestand"] == str(pad_b)

    def test_ontbrekende_home_telt_als_leeg(self, tmp_path):
        home = tmp_path / "a"
        leg_rollout(home, "28", "rollout-x.jsonl", FRAGMENT)
        stand = cu.lees_rollout(
            [tmp_path / "weg", home], now=datetime(2026, 9, 28, 21, 35, tzinfo=UTC)
        )
        assert stand["session"]["pct"] == 54

    def test_onleesbaar_bestand_wordt_overgeslagen(self, tmp_path):
        home = tmp_path / "a"
        leg_rollout(home, "28", "rollout-x.jsonl", FRAGMENT)
        pad = leg_rollout(home, "29", "rollout-kapot.jsonl", [])
        pad.write_bytes(b"\xff\xfe\x00 geen utf-8\n")
        stand = cu.lees_rollout([home], now=datetime(2026, 9, 28, 21, 35, tzinfo=UTC))
        assert stand["session"]["pct"] == 54

    def test_niets_bruikbaar_is_niet_meetbaar(self, tmp_path):
        home = tmp_path / "a"
        leg_rollout(home, "28", "rollout-x.jsonl", FRAGMENT[2:])
        with pytest.raises(cu.NietMeetbaar):
            cu.lees_rollout([home], now=datetime(2026, 9, 28, 21, 35, tzinfo=UTC))

    def test_geen_homes_met_log_is_niet_meetbaar(self, tmp_path):
        with pytest.raises(cu.NietMeetbaar):
            cu.lees_rollout([tmp_path / "weg"], now=NU_SPEC)


class TestLeesStand:
    """`lees_stand`: app-server per home, dan het sessielog; en het `read`-subcommando."""

    def _patch(self, monkeypatch, *, per_home, rollout=None):
        """`per_home` is een dict home -> result of exception; `rollout` idem voor het log."""
        aanroepen = {"app": [], "rollout": 0}

        def nep_app_server(home, *, timeout):
            aanroepen["app"].append(home)
            uitkomst = per_home[home]
            if isinstance(uitkomst, Exception):
                raise uitkomst
            return uitkomst

        def nep_rollout(homes, *, now):
            aanroepen["rollout"] += 1
            if isinstance(rollout, Exception):
                raise rollout
            return rollout

        monkeypatch.setattr(cu, "lees_app_server", nep_app_server)
        monkeypatch.setattr(cu, "lees_rollout", nep_rollout)
        return aanroepen

    def test_eerste_home_faalt_tweede_levert(self, monkeypatch, tmp_path):
        a, b = tmp_path / "a", tmp_path / "b"
        aanroepen = self._patch(
            monkeypatch,
            per_home={
                a: cu.NietMeetbaar("geen auth"),
                b: fixture("app_server_response.json"),
            },
        )
        stand = cu.lees_stand(homes=[a, b], timeout=5, now=NU_SPEC)
        assert stand["bron"] == "app-server"
        assert stand["session"]["pct"] == 55
        assert aanroepen["app"] == [a, b]
        assert aanroepen["rollout"] == 0

    def test_home_met_onleesbaar_antwoord_telt_als_gefaald(self, monkeypatch, tmp_path):
        a, b = tmp_path / "a", tmp_path / "b"
        self._patch(
            monkeypatch,
            per_home={a: {"onbekend": 1}, b: fixture("app_server_response.json")},
        )
        assert (
            cu.lees_stand(homes=[a, b], timeout=5, now=NU_SPEC)["bron"] == "app-server"
        )

    def test_eerste_home_levert_tweede_niet_aangeroepen(self, monkeypatch, tmp_path):
        a, b = tmp_path / "a", tmp_path / "b"
        aanroepen = self._patch(
            monkeypatch,
            per_home={a: fixture("app_server_response.json"), b: AssertionError("nee")},
        )
        cu.lees_stand(homes=[a, b], timeout=5, now=NU_SPEC)
        assert aanroepen["app"] == [a]

    def test_beide_homes_falen_terugval_op_log(self, monkeypatch, tmp_path):
        a, b = tmp_path / "a", tmp_path / "b"
        log = {"bron": "rollout-log", "ondergrens": True}
        aanroepen = self._patch(
            monkeypatch,
            per_home={a: cu.NietMeetbaar("x"), b: cu.NietMeetbaar("y")},
            rollout=log,
        )
        assert cu.lees_stand(homes=[a, b], timeout=5, now=NU_SPEC) == log
        assert aanroepen["rollout"] == 1

    def test_alles_faalt_reden_noemt_beide_bronnen(self, monkeypatch, tmp_path):
        a, b = tmp_path / "a", tmp_path / "b"
        self._patch(
            monkeypatch,
            per_home={a: cu.NietMeetbaar("geen auth"), b: cu.NietMeetbaar("timeout")},
            rollout=cu.NietMeetbaar("geen log"),
        )
        with pytest.raises(cu.NietMeetbaar) as exc:
            cu.lees_stand(homes=[a, b], timeout=5, now=NU_SPEC)
        reden = str(exc.value)
        assert f"{a}: geen auth" in reden
        assert f"{b}: timeout" in reden
        assert "rollout-log: geen log" in reden

    def test_read_niet_meetbaar(self, monkeypatch, capsys):
        def faal(**_):
            raise cu.NietMeetbaar("x")

        monkeypatch.setattr(cu, "lees_stand", faal)
        monkeypatch.setattr(cu, "codex_homes", list)
        monkeypatch.setattr(cu, "load_config", lambda: cu_config(20))
        assert cu.main(["read"]) == 1
        uit = capsys.readouterr()
        assert uit.out == ""
        assert "niet meetbaar: x" in uit.err

    def test_read_config_laadfout_is_niet_meetbaar_exit_1(self, monkeypatch, capsys):
        def stuk():
            raise ImportError("src.config stuk")

        monkeypatch.setattr(cu, "load_config", stuk)
        monkeypatch.setattr(cu, "codex_homes", list)
        assert cu.main(["read"]) == 1
        uit = capsys.readouterr()
        assert uit.out == ""
        assert "niet meetbaar: config: src.config stuk" in uit.err

    def test_read_print_json(self, monkeypatch, capsys, tmp_path):
        self._patch(
            monkeypatch, per_home={tmp_path: fixture("app_server_response.json")}
        )
        monkeypatch.setattr(cu, "codex_homes", lambda: [tmp_path])
        monkeypatch.setattr(cu, "load_config", lambda: cu_config(20))
        monkeypatch.setattr(cu, "_nu", lambda: NU_SPEC)
        assert cu.main(["read"]) == 0
        stand = json.loads(capsys.readouterr().out)
        assert stand["session"]["pct"] == 55
        assert stand["bron"] == "app-server"

    def test_read_geeft_geconfigureerde_timeout_door(self, monkeypatch):
        gezien = {}

        def spion(*, homes, timeout, now):
            gezien["timeout"] = timeout
            return fixture_stand()

        monkeypatch.setattr(cu, "lees_stand", spion)
        monkeypatch.setattr(cu, "codex_homes", list)
        monkeypatch.setattr(cu, "load_config", lambda: cu_config(7))
        assert cu.main(["read"]) == 0
        assert gezien["timeout"] == 7

    def test_main_vereist_een_subcommando(self, capsys):
        with pytest.raises(SystemExit) as exc:
            cu.main([])
        assert exc.value.code == 2


def cu_config(timeout: int):
    from src.config import CodexUsageConfig

    return CodexUsageConfig(app_server_timeout_seconds=timeout)


def fixture_stand() -> dict:
    return cu.parse_app_server(fixture("app_server_response.json"), now=NU_SPEC)


def test_codex_homes(monkeypatch, tmp_path):
    monkeypatch.setattr(cu.Path, "home", lambda: tmp_path)
    assert cu.codex_homes() == [tmp_path / ".codex", tmp_path / ".codex-specreview"]


def test_fmt_ts_normaliseert_naar_utc():
    plus2 = datetime(2026, 9, 28, 21, 0, 0, tzinfo=UTC).astimezone()
    assert cu.fmt_ts(plus2) == "2026-09-28T21:00:00Z"


# --- Taak 5: de gate-beslissing en de vraagtekst ---------------------------------------

RESET_S = "2026-09-28T23:00:00Z"  # na NU_SPEC
RESET_W = "2026-10-02T08:00:00Z"
TEGOEDEN_GEEN = {"beschikbaar": 0, "vervalt": []}


def stand(
    sessie: int,
    week: int,
    *,
    ondergrens: bool = False,
    reset_s: str = RESET_S,
    reset_w: str = RESET_W,
    tegoeden: dict | None = TEGOEDEN_GEEN,
) -> dict:
    """Een stand in de uitvoervorm; de resets liggen standaard na `NU_SPEC`."""
    return {
        "bron": "rollout-log" if ondergrens else "app-server",
        "generated_at": "2026-09-28T19:25:46Z",
        "leeftijd_s": 0,
        "bronbestand": None,
        "ondergrens": ondergrens,
        "session": {"pct": sessie, "reset_at": reset_s},
        "week": {"pct": week, "reset_at": reset_w},
        "reset_tegoeden": tegoeden,
    }


def beslis(s: dict | None):
    return cu.beslis_gate(s, halt_5u=85, vraag_week=80, now=NU_SPEC)


@pytest.fixture
def tz_amsterdam():
    """Lokale tijd op Europe/Amsterdam, onafhankelijk van de host; daarna hersteld."""
    oud = os.environ.get("TZ")
    os.environ["TZ"] = "Europe/Amsterdam"
    time.tzset()
    yield
    if oud is None:
        del os.environ["TZ"]
    else:
        os.environ["TZ"] = oud
    time.tzset()


@pytest.mark.parametrize(
    ("s", "w", "code"), [(10, 10, 0), (85, 10, 3), (10, 80, 4), (90, 90, 4)]
)
def test_gate_uitkomsten_en_week_voor_5u(s, w, code):
    assert beslis(stand(s, w)).code == code


def test_gate_niet_meetbaar():
    uitkomst = beslis(None)
    assert (uitkomst.code, uitkomst.tekst) == (5, "niet meetbaar")


def test_gate_door_noemt_beide_percentages():
    uitkomst = beslis(stand(10, 20))
    assert (uitkomst.code, uitkomst.tekst) == (0, "door: 5u 10%, week 20%")
    assert (uitkomst.window, uitkomst.reset_at) == (None, None)


def test_gate_parkeren_draagt_venster_en_reset_zonder_tekst():
    uitkomst = beslis(stand(90, 10))
    assert uitkomst == cu.Uitkomst(3, "", "codex-5u", RESET_S)


def test_gate_vraag_draagt_venster_en_week_reset():
    uitkomst = beslis(stand(10, 80))
    assert (uitkomst.code, uitkomst.window, uitkomst.reset_at) == (
        4,
        "codex-week",
        RESET_W,
    )


def test_ondergrens_boven_drempel_telt():
    assert beslis(stand(90, 10, ondergrens=True)).code == 3
    assert beslis(stand(10, 85, ondergrens=True)).code == 4


def test_ondergrens_onder_drempels_niet_meetbaar():
    uitkomst = beslis(stand(10, 10, ondergrens=True))
    assert (uitkomst.code, uitkomst.tekst) == (
        5,
        "niet meetbaar: logstand onder de drempels is geen bewijs",
    )


@pytest.mark.parametrize("kant", ["session", "week"])
def test_ondergrens_verlopen_reset_niet_meetbaar(kant):
    verlopen = "2026-09-28T19:00:00Z"
    s = stand(
        90,
        10,
        ondergrens=True,
        reset_s=verlopen if kant == "session" else RESET_S,
        reset_w=verlopen if kant == "week" else RESET_W,
    )
    uitkomst = beslis(s)
    assert (uitkomst.code, uitkomst.tekst) == (5, "niet meetbaar: logstand verlopen")


def test_ondergrens_reset_precies_nu_is_verlopen():
    s = stand(90, 10, ondergrens=True, reset_s="2026-09-28T19:25:46Z")
    assert beslis(s).code == 5


def test_app_server_verlopen_reset_telt_gewoon():
    assert beslis(stand(10, 10, reset_s="2026-09-28T19:00:00Z")).code == 0


def test_vraag_met_tegoeden():
    tegoeden = {
        "beschikbaar": 2,
        "vervalt": ["2026-10-20T10:00:00Z", "2026-11-03T12:30:00Z"],
    }
    t = beslis(stand(10, 80, tegoeden=tegoeden)).tekst
    assert "2 beschikbaar" in t
    assert "a) Wachten tot de week-reset" in t
    assert "b) Ik zet zelf een reset in" in t


def test_vraag_terugval_meldt_tegoeden_onbekend():
    t = beslis(stand(10, 85, ondergrens=True, tegoeden=None)).tekst
    assert "Reset-tegoeden onbekend (stand uit het sessielog" in t
    assert "b) Ik zet zelf" in t


def test_vraagtekst_letterlijk_met_geen_tegoeden(tz_amsterdam):
    assert cu.vraagtekst(stand(55, 82), vraag_week=80) == (
        "Het Codex-weekvenster staat op 82% (drempel 80%); het 5-uursvenster op 55%.\n"
        "De week-reset is 02-10 10:00.\n"
        "Reset-tegoeden: geen beschikbaar.\n"
        "Wat wil je?\n"
        "a) Wachten tot de week-reset; de run parkeert tot dan.\n"
        "b) Ik zet zelf een reset in (Codex-UI) en de run gaat daarna verder."
    )


def test_vraagtekst_tegoedregel_toont_vervaldata_lokaal(tz_amsterdam):
    tegoeden = {
        "beschikbaar": 2,
        "vervalt": ["2026-10-20T10:00:00Z", "2026-11-03T12:30:00Z"],
    }
    regel = cu.vraagtekst(stand(10, 80, tegoeden=tegoeden), vraag_week=80).splitlines()[
        2
    ]
    assert regel == "Reset-tegoeden: 2 beschikbaar, vervallen 20-10 12:00, 03-11 13:30."


def test_lokaal_zet_utc_om_naar_de_lokale_zone(tz_amsterdam):
    assert cu._lokaal("2026-09-28T19:00:00Z") == "28-09 21:00"  # zomertijd
    assert cu._lokaal("2026-12-01T19:00:00Z") == "01-12 20:00"  # wintertijd


# --- Taak 6: parkeren, de gate-CLI en de koppeling met de controller -------------------

RESET_5U_GATE = "2026-09-25T09:50:00Z"
RESET_WEEK_GATE = "2026-09-30T08:00:00Z"
# De klok van de gate: vóór beide resets. De controller-klok (`NU_CONTROLLER`, 10:00) ligt
# erna en bepaalt alleen wanneer de tick hervat.
NU_GATE = datetime(2026, 9, 25, 9, 0, tzinfo=UTC)


def _gate_stand(**extra) -> dict:
    return stand(50, 50, reset_s=RESET_5U_GATE, reset_w=RESET_WEEK_GATE) | extra


class TestGate:
    """De `gate`-CLI: marker in `tmp_path`, vaste stand en klok, nooit de echte resume-map."""

    @pytest.fixture(autouse=True)
    def omgeving(self, tmp_path, monkeypatch):
        monkeypatch.setenv("XDG_DATA_HOME", str(tmp_path))
        monkeypatch.delenv("PAIA_CONTROLLER_RUN", raising=False)
        monkeypatch.setattr(cu, "lees_stand", lambda **_: _gate_stand())
        monkeypatch.setattr(cu, "_nu", lambda: NU_GATE)

    @staticmethod
    def _stand(monkeypatch, waarde):
        def lees(**_):
            if isinstance(waarde, Exception):
                raise waarde
            return waarde

        monkeypatch.setattr(cu, "lees_stand", lees)

    def test_parkeren_schrijft_marker(self, tmp_path, monkeypatch, capsys):
        monkeypatch.setenv("PAIA_CONTROLLER_RUN", S1)
        assert cu.main(["gate", "--halt-5u", "1"]) == 3
        data = json.loads((tmp_path / "paia/resume" / f"{S1}.json").read_text())
        assert data | {"created_at": "x", "cwd": "x"} == {
            "session_id": S1,
            "cwd": "x",
            "window": "codex-5u",
            "reset_at": RESET_5U_GATE,
            "handoff_path": "",
            "created_at": "x",
            "source": "codex-gate",
            "controller": True,
        }
        assert data["cwd"] == os.getcwd()
        assert data["created_at"] == cu.fmt_ts(NU_GATE)
        out = capsys.readouterr().out
        assert out.startswith("STOP. Het Codex-5-uursvenster staat op 50%.")
        assert "Commit het werk-in-uitvoering en stop. De controller hervat" in out

    @pytest.mark.parametrize("waarde", [None, "", "../../etc", "ABC"])
    def test_geen_marker_zonder_geldige_sessie(
        self, tmp_path, monkeypatch, capsys, waarde
    ):
        if waarde is not None:
            monkeypatch.setenv("PAIA_CONTROLLER_RUN", waarde)
        assert cu.main(["gate", "--halt-5u", "1"]) == 3
        assert not list(tmp_path.rglob("*.json*"))
        out = capsys.readouterr().out
        assert out.startswith("Het Codex-5-uursvenster staat op 50%; reset om ")
        assert "STOP" not in out

    def test_onschrijfbare_resume_map_exit_5(self, tmp_path, monkeypatch, capsys):
        monkeypatch.setenv("PAIA_CONTROLLER_RUN", S1)
        (tmp_path / "paia").write_text("een bestand, geen map")
        assert cu.main(["gate", "--halt-5u", "1"]) == 5
        out = capsys.readouterr().out
        assert "marker niet geschreven:" in out
        assert "STOP" not in out
        assert not list(tmp_path.rglob("*.tmp"))

    def test_replace_faalt_ruimt_tmp_op(self, tmp_path, monkeypatch, capsys):
        monkeypatch.setenv("PAIA_CONTROLLER_RUN", S1)

        def stuk(*_args):
            raise OSError("schijf vol")

        monkeypatch.setattr(cu.os, "replace", stuk)
        assert cu.main(["gate", "--halt-5u", "1"]) == 5
        out = capsys.readouterr().out
        assert "marker niet geschreven: schijf vol" in out
        assert "STOP" not in out
        assert not list(tmp_path.rglob("*.tmp"))
        assert not list(tmp_path.rglob("*.json"))

    def test_wacht_week(self, tmp_path, monkeypatch, capsys):
        monkeypatch.setenv("PAIA_CONTROLLER_RUN", S1)
        assert cu.main(["gate", "--wacht-week"]) == 3
        data = json.loads((tmp_path / "paia/resume" / f"{S1}.json").read_text())
        assert (data["window"], data["reset_at"]) == ("codex-week", RESET_WEEK_GATE)
        assert capsys.readouterr().out.startswith(
            "STOP. Het Codex-weekvenster staat op 50%."
        )

    def test_wacht_week_zonder_sessie_schrijft_niets(
        self, tmp_path, monkeypatch, capsys
    ):
        assert cu.main(["gate", "--wacht-week"]) == 3
        assert not list(tmp_path.rglob("*.json*"))
        assert capsys.readouterr().out.startswith(
            "Het Codex-weekvenster staat op 50%; reset om "
        )

    def test_wacht_week_niet_meetbaar_exit_5(self, tmp_path, monkeypatch, capsys):
        monkeypatch.setenv("PAIA_CONTROLLER_RUN", S1)
        self._stand(monkeypatch, cu.NietMeetbaar("weg"))
        assert cu.main(["gate", "--wacht-week"]) == 5
        assert "niet meetbaar" in capsys.readouterr().out
        assert not list(tmp_path.rglob("*.json*"))

    def test_wacht_week_logstand_met_verlopen_week_reset_exit_5(
        self, tmp_path, monkeypatch, capsys
    ):
        monkeypatch.setenv("PAIA_CONTROLLER_RUN", S1)
        self._stand(
            monkeypatch,
            _gate_stand(ondergrens=True)
            | {"week": {"pct": 50, "reset_at": "2026-09-25T09:00:00Z"}},  # == nu
        )
        assert cu.main(["gate", "--wacht-week"]) == 5
        assert "niet meetbaar" in capsys.readouterr().out
        assert not list(tmp_path.rglob("*.json*"))

    def test_wacht_week_logstand_met_geldige_week_reset_parkeert(
        self, tmp_path, monkeypatch
    ):
        monkeypatch.setenv("PAIA_CONTROLLER_RUN", S1)
        self._stand(monkeypatch, _gate_stand(ondergrens=True))
        assert cu.main(["gate", "--wacht-week"]) == 3
        assert (tmp_path / "paia/resume" / f"{S1}.json").exists()

    def test_park_met_voorbije_5u_reset_exit_5_zonder_marker(
        self, tmp_path, monkeypatch, capsys
    ):
        monkeypatch.setenv("PAIA_CONTROLLER_RUN", S1)
        monkeypatch.setattr(cu, "_nu", lambda: NU_CONTROLLER)  # 10:00, reset 09:50
        assert cu.main(["gate", "--halt-5u", "1"]) == 5
        out = capsys.readouterr().out
        assert out.strip() == (
            f"niet meetbaar: venster rolt nog (reset {RESET_5U_GATE} is voorbij)"
        )
        assert not list(tmp_path.rglob("*.json*"))

    def test_wacht_week_met_voorbije_week_reset_exit_5_zonder_marker(
        self, tmp_path, monkeypatch, capsys
    ):
        monkeypatch.setenv("PAIA_CONTROLLER_RUN", S1)
        monkeypatch.setattr(cu, "_nu", lambda: NU_CONTROLLER)  # 10:00
        verlopen = "2026-09-25T09:30:00Z"
        self._stand(
            monkeypatch,
            _gate_stand() | {"week": {"pct": 50, "reset_at": verlopen}},
        )  # app-server-stand: ondergrens False, beslis_gate laat hem gewoon door
        assert cu.main(["gate", "--wacht-week"]) == 5
        out = capsys.readouterr().out
        assert f"niet meetbaar: venster rolt nog (reset {verlopen} is voorbij)" in out
        assert "STOP" not in out
        assert not list(tmp_path.rglob("*.json*"))

    def test_config_laadfout_is_exit_5_niet_een_traceback(self, monkeypatch, capsys):
        def stuk():
            raise FileNotFoundError("config.yaml weg")

        monkeypatch.setattr(cu, "load_config", stuk)
        assert cu.main(["gate"]) == 5
        out = capsys.readouterr().out
        assert "niet meetbaar: config: config.yaml weg" in out

    def test_flags_overschrijven_config(self, monkeypatch):
        from src.config import CodexUsageConfig

        monkeypatch.setattr(
            cu,
            "load_config",
            lambda: CodexUsageConfig(halt_pct_session=85, vraag_pct_week=80),
        )
        assert cu.main(["gate"]) == 0
        assert cu.main(["gate", "--vraag-week", "40"]) == 4
        assert cu.main(["gate", "--halt-5u", "40"]) == 3

    def test_vraag_print_de_vraagtekst(self, monkeypatch, capsys):
        assert cu.main(["gate", "--vraag-week", "40"]) == 4
        out = capsys.readouterr().out
        assert out.startswith("Het Codex-weekvenster staat op 50% (drempel 40%)")
        assert "a) Wachten tot de week-reset" in out

    def test_gate_niet_meetbaar_exit_5(self, monkeypatch, capsys):
        self._stand(monkeypatch, cu.NietMeetbaar("weg"))
        assert cu.main(["gate"]) == 5
        assert "niet meetbaar" in capsys.readouterr().out

    def test_gate_geeft_geconfigureerde_timeout_door(self, monkeypatch):
        gezien = {}

        def spion(*, homes, timeout, now):
            gezien["timeout"] = timeout
            return _gate_stand()

        monkeypatch.setattr(cu, "lees_stand", spion)
        monkeypatch.setattr(cu, "codex_homes", list)
        monkeypatch.setattr(cu, "load_config", lambda: cu_config(7))
        assert cu.main(["gate"]) == 0
        assert gezien["timeout"] == 7


class TestResumeDir:
    def test_xdg_data_home(self, tmp_path, monkeypatch):
        monkeypatch.setenv("XDG_DATA_HOME", str(tmp_path))
        assert cu.resume_dir() == tmp_path / "paia" / "resume"

    def test_zonder_xdg_data_home_onder_home(self, tmp_path, monkeypatch):
        monkeypatch.delenv("XDG_DATA_HOME", raising=False)
        monkeypatch.setattr(cu.Path, "home", lambda: tmp_path)
        assert cu.resume_dir() == tmp_path / ".local/share/paia/resume"


class TestKoppelingController:
    """Een marker van de gate, gelezen door lees_markers en beslist door beslis."""

    @pytest.fixture(autouse=True)
    def omgeving(self, tmp_path, monkeypatch):
        monkeypatch.setenv("XDG_DATA_HOME", str(tmp_path))
        monkeypatch.setattr(cu, "lees_stand", lambda **_: _gate_stand())
        monkeypatch.setattr(cu, "_nu", lambda: NU_GATE)

    def _besluit(self, tmp_path, monkeypatch, nu):
        monkeypatch.setenv("PAIA_CONTROLLER_RUN", S1)
        assert cu.main(["gate", "--halt-5u", "1"]) == 3
        markers = lees_markers(tmp_path / "paia" / "resume")
        geclaimd = comment(1, handoff(), minuut=10, rockets=1)
        return beslis_(
            invoer(
                issues=[issue(comments=[geclaimd])],
                runs=[run()],
                uitvoer={S1: uitkomst()},
                markers=markers,
                nu=nu,
            )
        )

    def test_na_reset_ga_verder(self, tmp_path, monkeypatch):
        [start] = van_soort(
            self._besluit(tmp_path, monkeypatch, NU_CONTROLLER), "start"
        )  # 10:00Z, reset 09:50Z
        assert (start["modus"], start["sessie"], start["prompt"]) == (
            "resume",
            S1,
            "ga verder",
        )

    def test_voor_reset_niets(self, tmp_path, monkeypatch):
        b = self._besluit(
            tmp_path, monkeypatch, datetime(2026, 9, 25, 9, 40, tzinfo=UTC)
        )
        assert van_soort(b, "start") == []


# --- #1093: budget ---------------------------------------------------------------------


def budget(s):
    return cu.beslis_budget(s, review_5u=90, review_week=95, now=NU_SPEC)


@pytest.mark.parametrize(
    ("s", "w", "besluit", "venster"),
    [
        (10, 10, "codex", None),
        (89, 94, "codex", None),
        (90, 10, "fallback", "codex-5u"),
        (10, 95, "fallback", "codex-week"),
        (99, 99, "fallback", "codex-week"),  # week vóór 5u
    ],
)
def test_budget_drempels(s, w, besluit, venster):
    u = budget(stand(s, w))
    assert (u.besluit, u.venster) == (besluit, venster)
    assert (
        u.reset_at == {"codex-5u": RESET_S, "codex-week": RESET_W, None: None}[venster]
    )
    assert (u.session_pct, u.week_pct, u.bron, u.ondergrens) == (
        s,
        w,
        "app-server",
        False,
    )
    assert u.drempels == {"session": 90, "week": 95}


def test_budget_reden_fallback():
    assert budget(stand(92, 41)).reden == "5u 92% ≥ 90%"
    assert budget(stand(10, 96)).reden == "week 96% ≥ 95%"


def test_budget_reden_codex():
    assert budget(stand(10, 20)).reden == "door: 5u 10%, week 20%"


def test_budget_none_is_niet_meetbaar():
    u = budget(None)
    assert u.besluit == "niet_meetbaar"
    assert (
        u.venster,
        u.reset_at,
        u.session_pct,
        u.week_pct,
        u.bron,
        u.ondergrens,
    ) == (None,) * 6
    assert u.drempels == {"session": 90, "week": 95}


def test_budget_logstand_boven_drempel_telt():
    assert budget(stand(92, 10, ondergrens=True)).besluit == "fallback"


def test_budget_logstand_onder_drempels_niet_meetbaar():
    u = budget(stand(10, 10, ondergrens=True))
    assert (u.besluit, u.venster, u.reset_at) == ("niet_meetbaar", None, None)
    assert "onder de drempels" in u.reden


@pytest.mark.parametrize("kant", ["reset_s", "reset_w"])
def test_budget_logstand_verlopen_niet_meetbaar(kant):
    u = budget(stand(92, 10, ondergrens=True, **{kant: "2026-09-28T19:00:00Z"}))
    assert u.besluit == "niet_meetbaar"
    assert u.reden == "niet meetbaar: logstand verlopen"


@pytest.mark.parametrize(("s", "w", "kant"), [(92, 10, "reset_s"), (10, 96, "reset_w")])
@pytest.mark.parametrize(
    "tijd", ["2026-09-28T19:00:00Z", "2026-09-28T19:25:46Z"]
)  # voorbij, precies nu
def test_budget_app_server_venster_rolt_nog(s, w, kant, tijd):
    u = budget(stand(s, w, **{kant: tijd}))
    assert (u.besluit, u.venster, u.reset_at) == ("niet_meetbaar", None, None)
    assert f"venster rolt nog (reset {tijd} is voorbij)" in u.reden


NEGEN = [
    "besluit",
    "reden",
    "venster",
    "reset_at",
    "session_pct",
    "week_pct",
    "bron",
    "ondergrens",
    "drempels",
]


class TestBudgetCli:
    @pytest.fixture(autouse=True)
    def omgeving(self, tmp_path, monkeypatch):
        from src.config import CodexUsageConfig

        monkeypatch.setenv("XDG_DATA_HOME", str(tmp_path))
        monkeypatch.setenv("PAIA_CONTROLLER_RUN", S1)  # mag niets uitmaken
        monkeypatch.setattr(cu, "codex_homes", list)
        monkeypatch.setattr(cu, "_nu", lambda: NU_SPEC)
        monkeypatch.setattr(cu, "load_config", lambda: CodexUsageConfig())

    @staticmethod
    def _stand(monkeypatch, waarde):
        def lees(**_):
            if isinstance(waarde, Exception):
                raise waarde
            return waarde

        monkeypatch.setattr(cu, "lees_stand", lees)

    @pytest.mark.parametrize(
        ("waarde", "code", "besluit"),
        [
            (stand(92, 41), 6, "fallback"),
            (stand(10, 10), 0, "codex"),
            (cu.NietMeetbaar("weg"), 0, "niet_meetbaar"),
        ],
    )
    def test_exit_en_negen_sleutels_zonder_marker(
        self, tmp_path, monkeypatch, capsys, waarde, code, besluit
    ):
        self._stand(monkeypatch, waarde)
        assert cu.main(["budget"]) == code
        data = json.loads(capsys.readouterr().out)
        assert list(data) == NEGEN
        assert data["besluit"] == besluit
        assert not list(tmp_path.rglob("*.json*"))

    def test_fallback_draagt_venster_en_drempels(self, monkeypatch, capsys):
        self._stand(monkeypatch, stand(92, 41))
        assert cu.main(["budget"]) == 6
        data = json.loads(capsys.readouterr().out)
        assert data == {
            "besluit": "fallback",
            "reden": "5u 92% ≥ 90%",
            "venster": "codex-5u",
            "reset_at": RESET_S,
            "session_pct": 92,
            "week_pct": 41,
            "bron": "app-server",
            "ondergrens": False,
            "drempels": {"session": 90, "week": 95},
        }

    def test_niet_meetbaar_noemt_de_reden(self, monkeypatch, capsys):
        self._stand(monkeypatch, cu.NietMeetbaar("weg"))
        assert cu.main(["budget"]) == 0
        data = json.loads(capsys.readouterr().out)
        assert data["reden"] == "niet meetbaar: weg"
        assert data["drempels"] == {"session": 90, "week": 95}

    def test_config_onleesbaar(self, monkeypatch, capsys):
        def stuk():
            raise FileNotFoundError("config.yaml weg")

        monkeypatch.setattr(cu, "load_config", stuk)
        assert cu.main(["budget"]) == 0
        data = json.loads(capsys.readouterr().out)
        assert list(data) == NEGEN
        assert data["besluit"] == "niet_meetbaar"
        assert data["drempels"] is None
        assert data["reden"] == "niet meetbaar: config: config.yaml weg"

    def test_gebruikt_de_geconfigureerde_drempels(self, monkeypatch, capsys):
        from src.config import CodexUsageConfig

        monkeypatch.setattr(
            cu,
            "load_config",
            lambda: CodexUsageConfig(review_pct_session=50, review_pct_week=60),
        )
        self._stand(monkeypatch, stand(55, 10))
        assert cu.main(["budget"]) == 6
        data = json.loads(capsys.readouterr().out)
        assert data["drempels"] == {"session": 50, "week": 60}
        assert data["reden"] == "5u 55% ≥ 50%"
