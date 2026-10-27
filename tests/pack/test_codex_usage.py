"""Unit tests for `.lighthouse/scripts/codex_usage.py`: `read` and `budget` (decision 26).

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

NOW_SPEC = datetime(2026, 9, 28, 19, 25, 46, tzinfo=UTC)

_SPEC = importlib.util.spec_from_file_location("codex_usage", SCRIPT)
assert _SPEC and _SPEC.loader
cu = importlib.util.module_from_spec(_SPEC)
sys.modules[_SPEC.name] = cu
_SPEC.loader.exec_module(cu)


def fixture(name: str) -> dict:
    return json.loads((FIXTURES / name).read_text())


# Four lines: two usable `codex` lines, a `premium` line without windows, and noise.
FRAGMENT = (FIXTURES / "rollout-fragment.jsonl").read_text().splitlines()


def write_rollout(home: Path, day: str, name: str, lines: list[str]) -> Path:
    """Write `lines` as a session log in `home/sessions/2026/09/<day>/<name>`."""
    path = home / "sessions" / "2026" / "09" / day / name
    path.parent.mkdir(parents=True)
    path.write_text("\n".join(lines) + "\n")
    return path


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
        with pytest.raises(cu.NotMeasurable, match="config: .*codex_usage.review_pct_week"):
            cu._config()


FAKE_CODEX = """#!{python}
import json, os, sys, time

MODUS = {mode!r}
ANTWOORD = {answer}

with open({pid_path!r}, "w") as f:
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
def fake_codex(tmp_path, monkeypatch):
    """Put a fake `codex` on `PATH`; returns a function that picks the mode."""

    def make(mode: str) -> Path:
        bin_dir = tmp_path / "bin"
        bin_dir.mkdir(exist_ok=True)
        script = bin_dir / "codex"
        script.write_text(
            FAKE_CODEX.format(
                python=sys.executable,
                mode=mode,
                answer=repr(fixture("app_server_response.json")),
                pid_path=str(tmp_path / "pid"),
            )
        )
        script.chmod(0o755)
        # Only the fake directory and the interpreter's: the real `codex` is out of reach.
        monkeypatch.setenv(
            "PATH", f"{bin_dir}{os.pathsep}{Path(sys.executable).parent}"
        )
        return script

    return make


class TestParseAppServer:
    def test_output_form(self):
        reading = cu.parse_app_server(fixture("app_server_response.json"), now=NOW_SPEC)
        assert reading == {
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

    def test_falls_back_to_ratelimits(self):
        res = fixture("app_server_response.json")
        del res["rateLimitsByLimitId"]
        assert cu.parse_app_server(res, now=NOW_SPEC)["session"]["pct"] == 55

    def test_no_account_id_in_output(self):
        res = {**fixture("app_server_response.json"), "accountId": "acc-geheim"}
        assert "acc-geheim" not in json.dumps(cu.parse_app_server(res, now=NOW_SPEC))

    @pytest.mark.parametrize("window", ["primary", "secondary"])
    def test_missing_window_not_measurable(self, window):
        res = fixture("app_server_response.json")
        res["rateLimitsByLimitId"]["codex"][window] = None
        with pytest.raises(cu.NotMeasurable, match=f"window {window} missing"):
            cu.parse_app_server(res, now=NOW_SPEC)

    def test_float_percentage_is_truncated(self):
        res = fixture("app_server_response.json")
        res["rateLimitsByLimitId"]["codex"]["primary"]["usedPercent"] = 54.6
        assert cu.parse_app_server(res, now=NOW_SPEC)["session"]["pct"] == 54

    def test_without_reset_credits_is_none(self):
        res = fixture("app_server_response.json")
        del res["rateLimitResetCredits"]
        assert cu.parse_app_server(res, now=NOW_SPEC)["reset_credits"] is None

    def test_only_available_credits_count_for_expires(self):
        res = fixture("app_server_response.json")
        res["rateLimitResetCredits"]["credits"][0]["status"] = "used"
        reading = cu.parse_app_server(res, now=NOW_SPEC)
        assert reading["reset_credits"]["expires"] == ["2026-10-22T20:18:18Z"]


class TestReadAppServer:
    def test_skips_noise_and_stops_the_process(self, fake_codex, tmp_path):
        fake_codex("ok")
        result = cu.read_app_server(tmp_path, timeout=10)
        assert result == fixture("app_server_response.json")
        pid = int((tmp_path / "pid").read_text())
        with pytest.raises(ProcessLookupError):
            os.kill(pid, 0)

    def test_passes_home_as_codex_home(self, fake_codex, tmp_path):
        script = fake_codex("ok")
        script.write_text(
            script.read_text().replace(
                "ANTWOORD = ", 'ANTWOORD = {"codex_home": os.environ["CODEX_HOME"]}  # '
            )
        )
        home = tmp_path / "eigen-home"
        assert cu.read_app_server(home, timeout=10) == {"codex_home": str(home)}

    def test_client_info_is_lighthouse(self):
        assert cu.CLIENT_INFO == {"name": "lighthouse-codex-usage", "version": "1"}

    @pytest.mark.parametrize("mode", ["error", "kapot"])
    def test_error_is_not_measurable(self, fake_codex, tmp_path, mode):
        fake_codex(mode)
        with pytest.raises(cu.NotMeasurable):
            # "kapot" gives a result that only fails in parse_app_server.  # lighthouse: allow-language — quotes the Dutch word the test feeds in
            cu.parse_app_server(cu.read_app_server(tmp_path, timeout=10), now=NOW_SPEC)

    def test_timeout(self, fake_codex, tmp_path):
        fake_codex("hang")
        start = time.monotonic()
        with pytest.raises(cu.NotMeasurable, match="no answer within the timeout"):
            cu.read_app_server(tmp_path, timeout=1)
        assert time.monotonic() - start < 5

    def test_without_codex(self, tmp_path, monkeypatch):
        empty = tmp_path / "leeg"
        empty.mkdir()
        monkeypatch.setenv("PATH", str(empty))
        with pytest.raises(cu.NotMeasurable, match="codex not found"):
            cu.read_app_server(tmp_path, timeout=5)


class TestRollout:
    def test_newest_usable_line(self, tmp_path):
        home = tmp_path / "a"
        write_rollout(home, "28", "rollout-x.jsonl", FRAGMENT)
        reading = cu.read_rollout([home], now=datetime(2026, 9, 28, 21, 35, tzinfo=UTC))
        # 54.6 is truncated; the premium line (younger, without windows) is skipped.
        assert reading["session"] == {"pct": 54, "reset_at": "2026-09-28T23:23:29Z"}
        assert reading["week"]["pct"] == 66
        assert (
            reading["source"],
            reading["lower_bound"],
            reading["age_s"],
            reading["reset_credits"],
        ) == ("rollout-log", True, 600, None)
        assert reading["generated_at"] == "2026-09-28T21:35:00Z"
        assert reading["source_file"].endswith("rollout-x.jsonl")
        assert list(reading) == [
            "source",
            "generated_at",
            "age_s",
            "source_file",
            "lower_bound",
            "session",
            "week",
            "reset_credits",
        ]

    def test_youngest_over_two_homes(self, tmp_path):
        a, b = tmp_path / "a", tmp_path / "b"
        write_rollout(a, "28", "rollout-a.jsonl", FRAGMENT)
        younger = FRAGMENT[1].replace("21:25:00", "21:28:00").replace("54.6", "70.2")
        path_b = write_rollout(b, "28", "rollout-b.jsonl", [younger])
        reading = cu.read_rollout([a, b], now=datetime(2026, 9, 28, 21, 35, tzinfo=UTC))
        assert reading["session"]["pct"] == 70
        assert reading["age_s"] == 420
        assert reading["source_file"] == str(path_b)

    def test_missing_home_counts_as_empty(self, tmp_path):
        home = tmp_path / "a"
        write_rollout(home, "28", "rollout-x.jsonl", FRAGMENT)
        reading = cu.read_rollout(
            [tmp_path / "weg", home], now=datetime(2026, 9, 28, 21, 35, tzinfo=UTC)
        )
        assert reading["session"]["pct"] == 54

    def test_unreadable_file_is_skipped(self, tmp_path):
        home = tmp_path / "a"
        write_rollout(home, "28", "rollout-x.jsonl", FRAGMENT)
        path = write_rollout(home, "29", "rollout-kapot.jsonl", [])
        path.write_bytes(b"\xff\xfe\x00 geen utf-8\n")
        reading = cu.read_rollout([home], now=datetime(2026, 9, 28, 21, 35, tzinfo=UTC))
        assert reading["session"]["pct"] == 54

    def test_nothing_usable_is_not_measurable(self, tmp_path):
        home = tmp_path / "a"
        write_rollout(home, "28", "rollout-x.jsonl", FRAGMENT[2:])
        with pytest.raises(cu.NotMeasurable):
            cu.read_rollout([home], now=datetime(2026, 9, 28, 21, 35, tzinfo=UTC))

    def test_no_homes_with_log_is_not_measurable(self, tmp_path):
        with pytest.raises(cu.NotMeasurable):
            cu.read_rollout([tmp_path / "weg"], now=NOW_SPEC)


class TestReadUsage:
    """`read_usage`: the app-server per home, then the session log; and the `read`
    subcommand."""

    def _patch(self, monkeypatch, *, per_home, rollout=None):
        """`per_home` is a dict home -> result or exception; `rollout` the same for the
        log."""
        calls = {"app": [], "rollout": 0}

        def fake_app_server(home, *, timeout):
            calls["app"].append(home)
            outcome = per_home[home]
            if isinstance(outcome, Exception):
                raise outcome
            return outcome

        def fake_rollout(homes, *, now):
            calls["rollout"] += 1
            if isinstance(rollout, Exception):
                raise rollout
            return rollout

        monkeypatch.setattr(cu, "read_app_server", fake_app_server)
        monkeypatch.setattr(cu, "read_rollout", fake_rollout)
        return calls

    def test_first_home_fails_second_delivers(self, monkeypatch, tmp_path):
        a, b = tmp_path / "a", tmp_path / "b"
        calls = self._patch(
            monkeypatch,
            per_home={
                a: cu.NotMeasurable("no auth"),
                b: fixture("app_server_response.json"),
            },
        )
        reading = cu.read_usage(homes=[a, b], timeout=5, now=NOW_SPEC)
        assert reading["source"] == "app-server"
        assert reading["session"]["pct"] == 55
        assert calls["app"] == [a, b]
        assert calls["rollout"] == 0

    def test_home_with_unreadable_answer_counts_as_failed(self, monkeypatch, tmp_path):
        a, b = tmp_path / "a", tmp_path / "b"
        self._patch(
            monkeypatch,
            per_home={a: {"onbekend": 1}, b: fixture("app_server_response.json")},
        )
        assert (
            cu.read_usage(homes=[a, b], timeout=5, now=NOW_SPEC)["source"] == "app-server"
        )

    def test_first_home_delivers_second_not_called(self, monkeypatch, tmp_path):
        a, b = tmp_path / "a", tmp_path / "b"
        calls = self._patch(
            monkeypatch,
            per_home={a: fixture("app_server_response.json"), b: AssertionError("nee")},
        )
        cu.read_usage(homes=[a, b], timeout=5, now=NOW_SPEC)
        assert calls["app"] == [a]

    def test_both_homes_fail_fall_back_to_log(self, monkeypatch, tmp_path):
        a, b = tmp_path / "a", tmp_path / "b"
        log = {"source": "rollout-log", "lower_bound": True}
        calls = self._patch(
            monkeypatch,
            per_home={a: cu.NotMeasurable("x"), b: cu.NotMeasurable("y")},
            rollout=log,
        )
        assert cu.read_usage(homes=[a, b], timeout=5, now=NOW_SPEC) == log
        assert calls["rollout"] == 1

    def test_all_fail_reason_names_both_sources(self, monkeypatch, tmp_path):
        a, b = tmp_path / "a", tmp_path / "b"
        self._patch(
            monkeypatch,
            per_home={a: cu.NotMeasurable("no auth"), b: cu.NotMeasurable("timeout")},
            rollout=cu.NotMeasurable("no log"),
        )
        with pytest.raises(cu.NotMeasurable) as exc:
            cu.read_usage(homes=[a, b], timeout=5, now=NOW_SPEC)
        reason = str(exc.value)
        assert f"{a}: no auth" in reason
        assert f"{b}: timeout" in reason
        assert "rollout-log: no log" in reason

    def test_read_not_measurable(self, monkeypatch, capsys):
        def fail(**_):
            raise cu.NotMeasurable("x")

        monkeypatch.setattr(cu, "read_usage", fail)
        monkeypatch.setattr(cu, "codex_homes", list)
        monkeypatch.setattr(cu, "load_config", lambda: cu_config(20))
        assert cu.main(["read"]) == 1
        out = capsys.readouterr()
        assert out.out == ""
        assert "not measurable: x" in out.err

    def test_read_config_load_error_is_not_measurable_exit_1(self, monkeypatch, capsys):
        def broken():
            raise cu.ConfigError("lighthouse.toml: file is missing")

        monkeypatch.setattr(cu, "load_config", broken)
        monkeypatch.setattr(cu, "codex_homes", list)
        assert cu.main(["read"]) == 1
        out = capsys.readouterr()
        assert out.out == ""
        assert "not measurable: config: lighthouse.toml: file is missing" in out.err

    def test_read_print_json(self, monkeypatch, capsys, tmp_path):
        self._patch(
            monkeypatch, per_home={tmp_path: fixture("app_server_response.json")}
        )
        monkeypatch.setattr(cu, "codex_homes", lambda: [tmp_path])
        monkeypatch.setattr(cu, "load_config", lambda: cu_config(20))
        monkeypatch.setattr(cu, "_now", lambda: NOW_SPEC)
        assert cu.main(["read"]) == 0
        reading = json.loads(capsys.readouterr().out)
        assert reading["session"]["pct"] == 55
        assert reading["source"] == "app-server"

    def test_read_passes_configured_timeout(self, monkeypatch):
        seen = {}

        def spy(*, homes, timeout, now):
            seen["timeout"] = timeout
            return fixture_reading()

        monkeypatch.setattr(cu, "read_usage", spy)
        monkeypatch.setattr(cu, "codex_homes", list)
        monkeypatch.setattr(cu, "load_config", lambda: cu_config(7))
        assert cu.main(["read"]) == 0
        assert seen["timeout"] == 7

    def test_main_requires_a_subcommand(self, capsys):
        with pytest.raises(SystemExit) as exc:
            cu.main([])
        assert exc.value.code == 2

    def test_gate_is_not_a_subcommand(self, capsys):
        """The gate stays in paia (decision 26)."""
        with pytest.raises(SystemExit) as exc:
            cu.main(["gate"])
        assert exc.value.code == 2


def cu_config(timeout: int, session: int = 90, week: int = 95):
    return cu.CodexUsageConfig(
        app_server_timeout_seconds=timeout,
        review_pct_session=session,
        review_pct_week=week,
    )


def fixture_reading() -> dict:
    return cu.parse_app_server(fixture("app_server_response.json"), now=NOW_SPEC)


def test_codex_homes(monkeypatch, tmp_path):
    monkeypatch.setattr(cu.Path, "home", lambda: tmp_path)
    assert cu.codex_homes() == [tmp_path / ".codex", tmp_path / ".codex-specreview"]


def test_fmt_ts_normalises_to_utc():
    plus2 = datetime(2026, 9, 28, 21, 0, 0, tzinfo=UTC).astimezone()
    assert cu.fmt_ts(plus2) == "2026-09-28T21:00:00Z"


RESET_S = "2026-09-28T23:00:00Z"  # after NOW_SPEC
RESET_W = "2026-10-02T08:00:00Z"
CREDITS_NONE = {"available": 0, "expires": []}


def reading(
    session: int,
    week: int,
    *,
    lower_bound: bool = False,
    reset_s: str = RESET_S,
    reset_w: str = RESET_W,
    credits: dict | None = CREDITS_NONE,
) -> dict:
    """A reading in the output form; by default the resets lie after `NOW_SPEC`."""
    return {
        "source": "rollout-log" if lower_bound else "app-server",
        "generated_at": "2026-09-28T19:25:46Z",
        "age_s": 0,
        "source_file": None,
        "lower_bound": lower_bound,
        "session": {"pct": session, "reset_at": reset_s},
        "week": {"pct": week, "reset_at": reset_w},
        "reset_credits": credits,
    }


# --- budget ----------------------------------------------------------------------------


def budget(s):
    return cu.decide_budget(s, review_5h=90, review_week=95, now=NOW_SPEC)


@pytest.mark.parametrize(
    ("s", "w", "decision", "window"),
    [
        (10, 10, "codex", None),
        (89, 94, "codex", None),
        (90, 10, "fallback", "codex-5h"),
        (10, 95, "fallback", "codex-week"),
        (99, 99, "fallback", "codex-week"),  # week before 5h
    ],
)
def test_budget_thresholds(s, w, decision, window):
    u = budget(reading(s, w))
    assert (u.decision, u.window) == (decision, window)
    assert (
        u.reset_at == {"codex-5h": RESET_S, "codex-week": RESET_W, None: None}[window]
    )
    assert (u.session_pct, u.week_pct, u.source, u.lower_bound) == (
        s,
        w,
        "app-server",
        False,
    )
    assert u.thresholds == {"session": 90, "week": 95}


def test_budget_reason_fallback():
    assert budget(reading(92, 41)).reason == "5h 92% ≥ 90%"
    assert budget(reading(10, 96)).reason == "week 96% ≥ 95%"


def test_budget_reason_codex():
    assert budget(reading(10, 20)).reason == "ok: 5h 10%, week 20%"


def test_budget_none_is_not_measurable():
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


def test_budget_log_reading_above_threshold_counts():
    assert budget(reading(92, 10, lower_bound=True)).decision == "fallback"


def test_budget_log_reading_below_thresholds_not_measurable():
    u = budget(reading(10, 10, lower_bound=True))
    assert (u.decision, u.window, u.reset_at) == ("not_measurable", None, None)
    assert "below the thresholds" in u.reason


@pytest.mark.parametrize("side", ["reset_s", "reset_w"])
def test_budget_log_reading_expired_not_measurable(side):
    u = budget(reading(92, 10, lower_bound=True, **{side: "2026-09-28T19:00:00Z"}))
    assert u.decision == "not_measurable"
    assert u.reason == "not measurable: log reading expired"


@pytest.mark.parametrize(("s", "w", "side"), [(92, 10, "reset_s"), (10, 96, "reset_w")])
@pytest.mark.parametrize(
    "stamp", ["2026-09-28T19:00:00Z", "2026-09-28T19:25:46Z"]
)  # past, exactly now
def test_budget_app_server_window_still_rolling(s, w, side, stamp):
    u = budget(reading(s, w, **{side: stamp}))
    assert (u.decision, u.window, u.reset_at) == ("not_measurable", None, None)
    assert f"window still rolling (reset {stamp} has passed)" in u.reason


# The budget JSON is a contract: `review.sh` and `plan_review_record` read these keys.
NINE_KEYS = [
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
    def environment(self, tmp_path, monkeypatch):
        write_config(tmp_path, monkeypatch)
        monkeypatch.setenv("XDG_DATA_HOME", str(tmp_path))
        monkeypatch.setattr(cu, "codex_homes", list)
        monkeypatch.setattr(cu, "_now", lambda: NOW_SPEC)

    @staticmethod
    def _set_reading(monkeypatch, value):
        def fake_read(**_):
            if isinstance(value, Exception):
                raise value
            return value

        monkeypatch.setattr(cu, "read_usage", fake_read)

    @pytest.mark.parametrize(
        ("value", "code", "decision"),
        [
            (reading(92, 41), 6, "fallback"),
            (reading(10, 10), 0, "codex"),
            (cu.NotMeasurable("gone"), 0, "not_measurable"),
        ],
    )
    def test_exit_and_nine_keys_without_marker(
        self, tmp_path, monkeypatch, capsys, value, code, decision
    ):
        self._set_reading(monkeypatch, value)
        assert cu.main(["budget"]) == code
        data = json.loads(capsys.readouterr().out)
        assert list(data) == NINE_KEYS
        assert data["decision"] == decision
        assert not list(tmp_path.rglob("*.json*"))

    def test_broken_config_is_said_on_stderr(self, tmp_path, monkeypatch, capsys):
        """The 0/6 contract stays, but a broken `[codex_usage]` must not hide in the JSON."""
        write_config(tmp_path, monkeypatch, '[codex_usage]\nreview_pct_week = "x"\n')
        assert cu.main(["budget"]) == 0
        out = capsys.readouterr()
        assert json.loads(out.out)["decision"] == "not_measurable"
        assert out.err.startswith("codex-usage: config: ")

    def test_fallback_carries_window_and_thresholds(self, monkeypatch, capsys):
        self._set_reading(monkeypatch, reading(92, 41))
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

    def test_not_measurable_names_the_reason(self, monkeypatch, capsys):
        self._set_reading(monkeypatch, cu.NotMeasurable("gone"))
        assert cu.main(["budget"]) == 0
        data = json.loads(capsys.readouterr().out)
        assert data["reason"] == "not measurable: gone"
        assert data["thresholds"] == {"session": 90, "week": 95}

    def test_config_unreadable(self, tmp_path, monkeypatch, capsys):
        monkeypatch.setenv("LIGHTHOUSE_TOML", str(tmp_path / "gone.toml"))
        assert cu.main(["budget"]) == 0
        data = json.loads(capsys.readouterr().out)
        assert list(data) == NINE_KEYS
        assert data["decision"] == "not_measurable"
        assert data["thresholds"] is None
        assert data["reason"] == f"not measurable: config: {tmp_path / 'gone.toml'}: file is missing"

    def test_uses_the_configured_thresholds(self, tmp_path, monkeypatch, capsys):
        write_config(
            tmp_path,
            monkeypatch,
            "[codex_usage]\nreview_pct_session = 50\nreview_pct_week = 60\n",
        )
        self._set_reading(monkeypatch, reading(55, 10))
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
    assert list(data) == NINE_KEYS
    assert data["decision"] == "not_measurable"
    assert data["thresholds"] == {"session": 90, "week": 95}
    assert "codex not found" in data["reason"]
