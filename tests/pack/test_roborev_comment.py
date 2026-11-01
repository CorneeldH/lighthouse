"""Tests for scripts/roborev_comment.py — the PR comment of the roborev review (#119).

Run in-process on the installed script; the CLI tests run it as a subprocess.
"""

from __future__ import annotations

import importlib.util
import json
import re
import subprocess
import sys

import pytest

from tests.pack._testkit import INSTALL_ROOT

SCRIPT = INSTALL_ROOT / ".lighthouse" / "scripts" / "roborev_comment.py"
_SPEC = importlib.util.spec_from_file_location("roborev_comment", SCRIPT)
assert _SPEC and _SPEC.loader
comment = importlib.util.module_from_spec(_SPEC)
_SPEC.loader.exec_module(comment)

LIMIT_NOTE = "… ingekort tot de limiet van GitHub"


def _finding(**over) -> dict:
    base = {
        "path": "src/a.py",
        "line": 12,
        "severity": "high",
        "uitkomst": "fixed",
        "commit": "abc1234",
        "reden": "",
    }
    base.update(over)
    return base


def _state(findings: list[dict]) -> dict:
    return {"range": "main..abc1234", "uitkomst": "done", "sha": "abc1234", "bevindingen": findings}


def test_log_block_round_trips_a_hostile_reason():
    reason = "sluit af --> <script>x</script>\nnieuwe regel"
    line = comment.log_block("abc1234", "lokaal", 7, [_finding(uitkomst="refuted", reden=reason)])
    assert "\n" not in line and line.count("-->") == 1 and line.endswith(" -->")
    payload = re.fullmatch(r"<!-- roborev-log: (.*) -->", line).group(1)
    assert json.loads(payload)["bevindingen"][0]["reden"] == reason
    assert "\\u003c" in payload and "\\u003e" in payload and "\\u002d" in payload
    assert "<" not in payload and ">" not in payload


def test_log_block_keeps_the_key_order_and_version():
    line = comment.log_block("abc1234", "ci", 7, [])
    payload = json.loads(re.fullmatch(r"<!-- roborev-log: (.*) -->", line).group(1))
    assert list(payload) == ["v", "sha", "bron", "pr", "bevindingen"]
    assert payload["v"] == 1 and payload["bron"] == "ci" and payload["pr"] == 7


def test_render_starts_with_the_marker_and_the_log():
    lines = comment.render(_state([_finding()]), "ruw", 7, "lokaal").splitlines()
    assert lines[0] == "<!-- roborev-verwerkt: abc1234 -->"
    assert lines[1].startswith("<!-- roborev-log: ")


def test_render_without_findings_says_so():
    out = comment.render(_state([]), "ruw", 7, "lokaal")
    assert "| geen bevindingen | — | — |" in out
    assert '"bevindingen":[]' in out


def test_render_puts_the_raw_output_in_details():
    out = comment.render(_state([]), "RUWE TEKST", 7, "lokaal")
    assert "<details><summary>Ruwe roborev-output</summary>" in out
    assert out.rstrip().endswith("</details>")
    assert out.index("RUWE TEKST") < out.rindex("</details>")


def test_render_truncates_only_the_raw_output():
    findings = [_finding(path=f"f{i}.py", line=i, reden="r" * 40, uitkomst="left") for i in range(30)]
    out = comment.render(_state(findings), "x" * 200_000, 7, "lokaal")
    assert len(out) <= 65536
    assert comment.log_block("abc1234", "lokaal", 7, findings) in out
    assert comment.table(findings) in out
    assert LIMIT_NOTE in out
    assert out.rstrip().endswith("</details>")


def test_render_does_not_truncate_short_output():
    assert LIMIT_NOTE not in comment.render(_state([]), "kort", 7, "lokaal")


def test_table_rows_per_outcome():
    rows = comment.table(
        [
            _finding(),
            _finding(path="b.py", line=3, severity="low", uitkomst="refuted", reden="klopt niet"),
            _finding(path=None, line=None, uitkomst="left", reden="later"),
        ]
    ).splitlines()
    assert rows[0] == "| Bevinding | Severity | Uitkomst |"
    assert rows[2] == "| `src/a.py:12` | high | fixed in abc1234 |"
    assert rows[3] == "| `b.py:3` | low | refuted: klopt niet |"
    assert rows[4] == "| — | high | left: later |"


def test_table_escapes_a_pipe_in_a_reason():
    row = comment.table([_finding(uitkomst="left", reden="a|b")]).splitlines()[2]
    assert row.endswith("left: a\\|b |")


def _cli(tmp_path, state: dict, *extra: str):
    state_path = tmp_path / "s.json"
    raw_path = tmp_path / "r.md"
    state_path.write_text(json.dumps(state))
    raw_path.write_text("ruw")
    return subprocess.run(
        [sys.executable, str(SCRIPT), "render", "--pr", "7", "--bron", "ci",
         "--state", str(state_path), "--raw", str(raw_path), *extra],
        capture_output=True, text=True,
    )


def test_cli_renders_a_done_state_to_stdout(tmp_path):
    result = _cli(tmp_path, _state([_finding()]))
    assert result.returncode == 0
    assert result.stdout.startswith("<!-- roborev-verwerkt: abc1234 -->")
    assert '"bron":"ci"' in result.stdout


def test_cli_exits_2_on_a_skip_state(tmp_path):
    result = _cli(tmp_path, {"range": "a..b", "uitkomst": "skip", "reden": "geen .roborev.toml"})
    assert result.returncode == 2 and result.stdout == ""


def test_table_keeps_a_multiline_reason_on_one_row():
    reason = "regel een\nregel twee\r\nregel drie"
    finding = _finding(uitkomst="left", reden=reason, severity="hi\ngh")
    rows = comment.table([finding]).splitlines()
    assert len(rows) == 3
    assert rows[2] == "| `src/a.py:12` | hi gh | left: regel een regel twee regel drie |"
    line = comment.log_block("abc1234", "lokaal", 7, [finding])
    payload = re.fullmatch(r"<!-- roborev-log: (.*) -->", line).group(1)
    assert json.loads(payload)["bevindingen"][0]["reden"] == reason


def test_render_refuses_when_log_and_table_alone_exceed_the_limit():
    findings = [_finding(path=f"f{i}.py", line=i, reden="r" * 100, uitkomst="left") for i in range(1000)]
    with pytest.raises(ValueError, match="65536"):
        comment.render(_state(findings), "ruw", 7, "lokaal")


def test_cli_exits_2_when_the_comment_cannot_fit(tmp_path):
    findings = [_finding(path=f"f{i}.py", line=i, reden="r" * 100, uitkomst="left") for i in range(1000)]
    result = _cli(tmp_path, _state(findings))
    assert result.returncode == 2 and result.stdout == "" and "65536" in result.stderr
