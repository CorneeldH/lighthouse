"""Unit tests for `.lighthouse/scripts/codex_usage.py`: `read` and `budget` (besluit 26).

Config: the thresholds come from `[codex_usage]` in `lighthouse.toml`, with the defaults of
`config.py`. App-server: the parser to the output form, and the reader against a fake
`codex` (a Python script in a temporary directory on `PATH`). The real `codex` never runs
here: no quota, no network.

Taken from paia's `test_codex_usage.py`. Left out, because they test what stays in paia:
the mirror test of `config.yaml`, the gate (`beslis_gate`, the question text, `TestGate`),
the resume markers (`TestResumeDir`) and the coupling with the controller.
"""

from __future__ import annotations

import importlib.util
import json
import os
import subprocess
import sys
import time
from datetime import UTC, datetime
from pathlib import Path

import pytest

from tests.pack._testkit import INSTALL_ROOT

FIXTURES = Path(__file__).resolve().parent / "fixtures" / "codex_usage"
SCRIPT = INSTALL_ROOT / ".lighthouse" / "scripts" / "codex_usage.py"

NU_SPEC = datetime(2026, 9, 28, 19, 25, 46, tzinfo=UTC)

_SPEC = importlib.util.spec_from_file_location("codex_usage", SCRIPT)
assert _SPEC and _SPEC.loader
cu = importlib.util.module_from_spec(_SPEC)
sys.modules[_SPEC.name] = cu
_SPEC.loader.exec_module(cu)


def fixture(name: str) -> dict:
    return json.loads((FIXTURES / name).read_text())


# Four lines: two usable `codex` lines, a `premium` line without windows, and noise.
FRAGMENT = (FIXTURES / "rollout-fragment.jsonl").read_text().splitlines()


def leg_rollout(home: Path, dag: str, naam: str, regels: list[str]) -> Path:
    """Write `regels` as a session log in `home/sessions/2026/09/<dag>/<naam>`."""
    pad = home / "sessions" / "2026" / "09" / dag / naam
    pad.parent.mkdir(parents=True)
    pad.write_text("\n".join(regels) + "\n")
    return pad


def write_config(tmp_path: Path, monkeypatch, body: str = "") -> Path:
    """A `lighthouse.toml` with `[project]` and `body`, via `LIGHTHOUSE_TOML`."""
    path = tmp_path / "lighthouse.toml"
    path.write_text('[project]\nlanguage = "en"\n' + body, encoding="utf-8")
    monkeypatch.setenv("LIGHTHOUSE_TOML", str(path))
    return path


class TestConfig:
    def test_defaults_without_table(self, tmp_path, monkeypatch):
        write_config(tmp_path, monkeypatch)
        cfg = cu.load_config()
        assert (
            cfg.app_server_timeout_seconds,
            cfg.review_pct_session,
            cfg.review_pct_week,
        ) == (20, 90, 95)

    def test_thresholds_from_config(self, tmp_path, monkeypatch):
        """Values that differ from the defaults must come through; equal values prove
        nothing."""
        write_config(
            tmp_path,
            monkeypatch,
            "[codex_usage]\n"
            "app_server_timeout_seconds = 5\n"
            "review_pct_session = 70\n"
            "review_pct_week = 75\n",
        )
        cfg = cu.load_config()
        assert (
            cfg.app_server_timeout_seconds,
            cfg.review_pct_session,
            cfg.review_pct_week,
        ) == (5, 70, 75)

    def test_broken_config_is_not_measurable(self, tmp_path, monkeypatch):
        write_config(tmp_path, monkeypatch, '[codex_usage]\nreview_pct_week = "x"\n')
        with pytest.raises(cu.NietMeetbaar, match="config: .*codex_usage.review_pct_week"):
            cu._config()


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
    """Put a fake `codex` on `PATH`; returns a function that picks the mode."""

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
        # Only the fake directory and the interpreter's: the real `codex` is out of reach.
        monkeypatch.setenv(
            "PATH", f"{bin_dir}{os.pathsep}{Path(sys.executable).parent}"
        )
        return script

    return maak


class TestParseAppServer:
    def test_uitvoervorm(self):
        stand = cu.parse_app_server(fixture("app_server_response.json"), now=NU_SPEC)
        assert stand == {
            "source": "app-server",
            "generated_at": "2026-09-28T19:25:46Z",
            "age_s": 0,
            "source_file": None,
            "lower_bound": False,
            "session": {"pct": 55, "reset_at": "2026-09-28T23:23:29Z"},
            "week": {"pct": 66, "reset_at": "2026-10-03T17:17:59Z"},
            "reset_credits": {
                "available": 2,
                "expires": ["2026-10-04T23:53:00Z", "2026-10-22T20:18:18Z"],
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
        with pytest.raises(cu.NietMeetbaar, match=f"window {venster} missing"):
            cu.parse_app_server(res, now=NU_SPEC)

    def test_float_percentage_wordt_afgekapt(self):
        res = fixture("app_server_response.json")
        res["rateLimitsByLimitId"]["codex"]["primary"]["usedPercent"] = 54.6
        assert cu.parse_app_server(res, now=NU_SPEC)["session"]["pct"] == 54

    def test_zonder_reset_tegoeden_is_none(self):
        res = fixture("app_server_response.json")
        del res["rateLimitResetCredits"]
        assert cu.parse_app_server(res, now=NU_SPEC)["reset_credits"] is None

    def test_alleen_beschikbare_tegoeden_tellen_voor_vervalt(self):
        res = fixture("app_server_response.json")
        res["rateLimitResetCredits"]["credits"][0]["status"] = "used"
        stand = cu.parse_app_server(res, now=NU_SPEC)
        assert stand["reset_credits"]["expires"] == ["2026-10-22T20:18:18Z"]


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

    def test_client_info_is_lighthouse(self):
        assert cu.CLIENT_INFO == {"name": "lighthouse-codex-usage", "version": "1"}

    @pytest.mark.parametrize("modus", ["error", "kapot"])
    def test_fout_is_niet_meetbaar(self, nep_codex, tmp_path, modus):
        nep_codex(modus)
        with pytest.raises(cu.NietMeetbaar):
            # "kapot" gives a result that only fails in parse_app_server.
            cu.parse_app_server(cu.lees_app_server(tmp_path, timeout=10), now=NU_SPEC)

    def test_timeout(self, nep_codex, tmp_path):
        nep_codex("hang")
        start = time.monotonic()
        with pytest.raises(cu.NietMeetbaar, match="no answer within the timeout"):
            cu.lees_app_server(tmp_path, timeout=1)
        assert time.monotonic() - start < 5

    def test_zonder_codex(self, tmp_path, monkeypatch):
        leeg = tmp_path / "leeg"
        leeg.mkdir()
        monkeypatch.setenv("PATH", str(leeg))
        with pytest.raises(cu.NietMeetbaar, match="codex not found"):
            cu.lees_app_server(tmp_path, timeout=5)


class TestRollout:
    def test_nieuwste_bruikbare_regel(self, tmp_path):
        home = tmp_path / "a"
        leg_rollout(home, "28", "rollout-x.jsonl", FRAGMENT)
        stand = cu.lees_rollout([home], now=datetime(2026, 9, 28, 21, 35, tzinfo=UTC))
        # 54.6 is truncated; the premium line (younger, without windows) is skipped.
        assert stand["session"] == {"pct": 54, "reset_at": "2026-09-28T23:23:29Z"}
        assert stand["week"]["pct"] == 66
        assert (
            stand["source"],
            stand["lower_bound"],
            stand["age_s"],
            stand["reset_credits"],
        ) == ("rollout-log", True, 600, None)
        assert stand["generated_at"] == "2026-09-28T21:35:00Z"
        assert stand["source_file"].endswith("rollout-x.jsonl")
        assert list(stand) == [
            "source",
            "generated_at",
            "age_s",
            "source_file",
            "lower_bound",
            "session",
            "week",
            "reset_credits",
        ]

    def test_jongste_over_twee_homes(self, tmp_path):
        a, b = tmp_path / "a", tmp_path / "b"
        leg_rollout(a, "28", "rollout-a.jsonl", FRAGMENT)
        jonger = FRAGMENT[1].replace("21:25:00", "21:28:00").replace("54.6", "70.2")
        pad_b = leg_rollout(b, "28", "rollout-b.jsonl", [jonger])
        stand = cu.lees_rollout([a, b], now=datetime(2026, 9, 28, 21, 35, tzinfo=UTC))
        assert stand["session"]["pct"] == 70
        assert stand["age_s"] == 420
        assert stand["source_file"] == str(pad_b)

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
    """`lees_stand`: the app-server per home, then the session log; and the `read`
    subcommand."""

    def _patch(self, monkeypatch, *, per_home, rollout=None):
        """`per_home` is a dict home -> result or exception; `rollout` the same for the
        log."""
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
                a: cu.NietMeetbaar("no auth"),
                b: fixture("app_server_response.json"),
            },
        )
        stand = cu.lees_stand(homes=[a, b], timeout=5, now=NU_SPEC)
        assert stand["source"] == "app-server"
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
            cu.lees_stand(homes=[a, b], timeout=5, now=NU_SPEC)["source"] == "app-server"
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
        log = {"source": "rollout-log", "lower_bound": True}
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
            per_home={a: cu.NietMeetbaar("no auth"), b: cu.NietMeetbaar("timeout")},
            rollout=cu.NietMeetbaar("no log"),
        )
        with pytest.raises(cu.NietMeetbaar) as exc:
            cu.lees_stand(homes=[a, b], timeout=5, now=NU_SPEC)
        reden = str(exc.value)
        assert f"{a}: no auth" in reden
        assert f"{b}: timeout" in reden
        assert "rollout-log: no log" in reden

    def test_read_niet_meetbaar(self, monkeypatch, capsys):
        def faal(**_):
            raise cu.NietMeetbaar("x")

        monkeypatch.setattr(cu, "lees_stand", faal)
        monkeypatch.setattr(cu, "codex_homes", list)
        monkeypatch.setattr(cu, "load_config", lambda: cu_config(20))
        assert cu.main(["read"]) == 1
        uit = capsys.readouterr()
        assert uit.out == ""
        assert "not measurable: x" in uit.err

    def test_read_config_laadfout_is_niet_meetbaar_exit_1(self, monkeypatch, capsys):
        def stuk():
            raise cu.ConfigError("lighthouse.toml: file is missing")

        monkeypatch.setattr(cu, "load_config", stuk)
        monkeypatch.setattr(cu, "codex_homes", list)
        assert cu.main(["read"]) == 1
        uit = capsys.readouterr()
        assert uit.out == ""
        assert "not measurable: config: lighthouse.toml: file is missing" in uit.err

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
        assert stand["source"] == "app-server"

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

    def test_gate_is_not_a_subcommand(self, capsys):
        """The gate stays in paia (besluit 26)."""
        with pytest.raises(SystemExit) as exc:
            cu.main(["gate"])
        assert exc.value.code == 2


def cu_config(timeout: int, session: int = 90, week: int = 95):
    return cu.CodexUsageConfig(
        app_server_timeout_seconds=timeout,
        review_pct_session=session,
        review_pct_week=week,
    )


def fixture_stand() -> dict:
    return cu.parse_app_server(fixture("app_server_response.json"), now=NU_SPEC)


def test_codex_homes(monkeypatch, tmp_path):
    monkeypatch.setattr(cu.Path, "home", lambda: tmp_path)
    assert cu.codex_homes() == [tmp_path / ".codex", tmp_path / ".codex-specreview"]


def test_fmt_ts_normaliseert_naar_utc():
    plus2 = datetime(2026, 9, 28, 21, 0, 0, tzinfo=UTC).astimezone()
    assert cu.fmt_ts(plus2) == "2026-09-28T21:00:00Z"


RESET_S = "2026-09-28T23:00:00Z"  # after NU_SPEC
RESET_W = "2026-10-02T08:00:00Z"
TEGOEDEN_GEEN = {"available": 0, "expires": []}


def stand(
    sessie: int,
    week: int,
    *,
    ondergrens: bool = False,
    reset_s: str = RESET_S,
    reset_w: str = RESET_W,
    tegoeden: dict | None = TEGOEDEN_GEEN,
) -> dict:
    """A reading in the output form; by default the resets lie after `NU_SPEC`."""
    return {
        "source": "rollout-log" if ondergrens else "app-server",
        "generated_at": "2026-09-28T19:25:46Z",
        "age_s": 0,
        "source_file": None,
        "lower_bound": ondergrens,
        "session": {"pct": sessie, "reset_at": reset_s},
        "week": {"pct": week, "reset_at": reset_w},
        "reset_credits": tegoeden,
    }


# --- budget ----------------------------------------------------------------------------


def budget(s):
    return cu.beslis_budget(s, review_5u=90, review_week=95, now=NU_SPEC)


@pytest.mark.parametrize(
    ("s", "w", "besluit", "venster"),
    [
        (10, 10, "codex", None),
        (89, 94, "codex", None),
        (90, 10, "fallback", "codex-5h"),
        (10, 95, "fallback", "codex-week"),
        (99, 99, "fallback", "codex-week"),  # week before 5h
    ],
)
def test_budget_drempels(s, w, besluit, venster):
    u = budget(stand(s, w))
    assert (u.decision, u.window) == (besluit, venster)
    assert (
        u.reset_at == {"codex-5h": RESET_S, "codex-week": RESET_W, None: None}[venster]
    )
    assert (u.session_pct, u.week_pct, u.source, u.lower_bound) == (
        s,
        w,
        "app-server",
        False,
    )
    assert u.thresholds == {"session": 90, "week": 95}


def test_budget_reden_fallback():
    assert budget(stand(92, 41)).reason == "5h 92% ≥ 90%"
    assert budget(stand(10, 96)).reason == "week 96% ≥ 95%"


def test_budget_reden_codex():
    assert budget(stand(10, 20)).reason == "ok: 5h 10%, week 20%"


def test_budget_none_is_niet_meetbaar():
    u = budget(None)
    assert u.decision == "not_measurable"
    assert (
        u.window,
        u.reset_at,
        u.session_pct,
        u.week_pct,
        u.source,
        u.lower_bound,
    ) == (None,) * 6
    assert u.thresholds == {"session": 90, "week": 95}


def test_budget_logstand_boven_drempel_telt():
    assert budget(stand(92, 10, ondergrens=True)).decision == "fallback"


def test_budget_logstand_onder_drempels_niet_meetbaar():
    u = budget(stand(10, 10, ondergrens=True))
    assert (u.decision, u.window, u.reset_at) == ("not_measurable", None, None)
    assert "below the thresholds" in u.reason


@pytest.mark.parametrize("kant", ["reset_s", "reset_w"])
def test_budget_logstand_verlopen_niet_meetbaar(kant):
    u = budget(stand(92, 10, ondergrens=True, **{kant: "2026-09-28T19:00:00Z"}))
    assert u.decision == "not_measurable"
    assert u.reason == "not measurable: log reading expired"


@pytest.mark.parametrize(("s", "w", "kant"), [(92, 10, "reset_s"), (10, 96, "reset_w")])
@pytest.mark.parametrize(
    "tijd", ["2026-09-28T19:00:00Z", "2026-09-28T19:25:46Z"]
)  # past, exactly now
def test_budget_app_server_venster_rolt_nog(s, w, kant, tijd):
    u = budget(stand(s, w, **{kant: tijd}))
    assert (u.decision, u.window, u.reset_at) == ("not_measurable", None, None)
    assert f"window still rolling (reset {tijd} has passed)" in u.reason


# The budget JSON is a contract: `review.sh` and `plan_review_record` read these keys.
NEGEN = [
    "decision",
    "reason",
    "window",
    "reset_at",
    "session_pct",
    "week_pct",
    "source",
    "lower_bound",
    "thresholds",
]


class TestBudgetCli:
    @pytest.fixture(autouse=True)
    def omgeving(self, tmp_path, monkeypatch):
        write_config(tmp_path, monkeypatch)
        monkeypatch.setenv("XDG_DATA_HOME", str(tmp_path))
        monkeypatch.setattr(cu, "codex_homes", list)
        monkeypatch.setattr(cu, "_nu", lambda: NU_SPEC)

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
            (cu.NietMeetbaar("gone"), 0, "not_measurable"),
        ],
    )
    def test_exit_en_negen_sleutels_zonder_marker(
        self, tmp_path, monkeypatch, capsys, waarde, code, besluit
    ):
        self._stand(monkeypatch, waarde)
        assert cu.main(["budget"]) == code
        data = json.loads(capsys.readouterr().out)
        assert list(data) == NEGEN
        assert data["decision"] == besluit
        assert not list(tmp_path.rglob("*.json*"))

    def test_fallback_draagt_venster_en_drempels(self, monkeypatch, capsys):
        self._stand(monkeypatch, stand(92, 41))
        assert cu.main(["budget"]) == 6
        data = json.loads(capsys.readouterr().out)
        assert data == {
            "decision": "fallback",
            "reason": "5h 92% ≥ 90%",
            "window": "codex-5h",
            "reset_at": RESET_S,
            "session_pct": 92,
            "week_pct": 41,
            "source": "app-server",
            "lower_bound": False,
            "thresholds": {"session": 90, "week": 95},
        }

    def test_niet_meetbaar_noemt_de_reden(self, monkeypatch, capsys):
        self._stand(monkeypatch, cu.NietMeetbaar("gone"))
        assert cu.main(["budget"]) == 0
        data = json.loads(capsys.readouterr().out)
        assert data["reason"] == "not measurable: gone"
        assert data["thresholds"] == {"session": 90, "week": 95}

    def test_config_onleesbaar(self, tmp_path, monkeypatch, capsys):
        monkeypatch.setenv("LIGHTHOUSE_TOML", str(tmp_path / "gone.toml"))
        assert cu.main(["budget"]) == 0
        data = json.loads(capsys.readouterr().out)
        assert list(data) == NEGEN
        assert data["decision"] == "not_measurable"
        assert data["thresholds"] is None
        assert data["reason"] == f"not measurable: config: {tmp_path / 'gone.toml'}: file is missing"

    def test_gebruikt_de_geconfigureerde_drempels(self, tmp_path, monkeypatch, capsys):
        write_config(
            tmp_path,
            monkeypatch,
            "[codex_usage]\nreview_pct_session = 50\nreview_pct_week = 60\n",
        )
        self._stand(monkeypatch, stand(55, 10))
        assert cu.main(["budget"]) == 6
        data = json.loads(capsys.readouterr().out)
        assert data["thresholds"] == {"session": 50, "week": 60}
        assert data["reason"] == "5h 55% ≥ 50%"


def test_budget_runs_as_installed_file(tmp_path):
    """`python3 .lighthouse/scripts/codex_usage.py budget` finds `lighthouse_lib` itself."""
    config = tmp_path / "lighthouse.toml"
    config.write_text('[project]\nlanguage = "en"\n', encoding="utf-8")
    empty = tmp_path / "bin"
    empty.mkdir()
    env = {
        "PATH": str(empty),
        "HOME": str(tmp_path),
        "LIGHTHOUSE_TOML": str(config),
        "PYTHONDONTWRITEBYTECODE": "1",
    }
    result = subprocess.run(
        [sys.executable, str(SCRIPT), "budget"],
        capture_output=True,
        text=True,
        env=env,
        cwd=tmp_path,
        check=False,
    )
    assert result.returncode == 0, result.stderr
    data = json.loads(result.stdout)
    assert list(data) == NEGEN
    assert data["decision"] == "not_measurable"
    assert data["thresholds"] == {"session": 90, "week": 95}
    assert "codex not found" in data["reason"]
