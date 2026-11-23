"""tests/pack/python/_network.py and the hook in tests/pack/python/conftest.py: a network error
while the runner is offline is a skip, every other failure stays red (#338)."""

import re
import socket
import subprocess
import sys
from pathlib import Path

import pytest

from tests.pack.python._network import PROBE_ENV, network_error, offline

ROOT = Path(__file__).resolve().parents[3]
assert (ROOT / "pyproject.toml").is_file(), ROOT


# The lines come from the macOS run 42546929903, where the runner had no DNS for a moment.
@pytest.mark.parametrize(
    "longrepr,want",
    [
        (
            "E       AssertionError: curl: (6) Could not resolve host: github.com",
            "curl: (6) Could not resolve host: github.com",
        ),
        (
            (
                "E       AssertionError: ruff-format-check: COULD NOT CHECK — uvx ruff does not"
                " start (offline, or the pin does not resolve): error: Request failed after 3"
                " retries in 46.0s"
            ),
            "Request failed after 3 retries in 46.0s",
        ),
        (
            "    def test():\nE           cause: error sending request for url (https://x.org/r.whl)\n",
            "error sending request for url (https://x.org/r.whl)",
        ),
        (
            "E   curl: (28) Connection timed out after 10001 ms",
            "curl: (28) Connection timed out after 10001 ms",
        ),
        (
            "E   dial tcp: lookup proxy.golang.org on 127.0.0.53:53: no such host",
            "dial tcp: lookup proxy.golang.org on 127.0.0.53:53: no such host",
        ),
    ],
)
def test_network_error_names_the_tool_line(longrepr, want):
    assert network_error(longrepr) == want


@pytest.mark.parametrize(
    "longrepr",
    [
        "",
        "E       assert 1 == 2",
        # lighthouse's own words: a pin that does not exist says them too.
        "E   shellcheck: download failed (offline?)",
        (
            "E   ruff-format-check: COULD NOT CHECK — uvx ruff does not start (offline, or the"
            " pin does not resolve)"
        ),
        # curl 22 is an HTTP error (a 404 of a wrong pin), not the network.
        "E   curl: (22) The requested URL returned error: 404",
        # A source line of the traceback is no explanation, even with a network line in it.
        ">       assert False, 'curl: (6) Could not resolve host: github.com'\nE       assert False",
    ],
)
def test_network_error_leaves_other_failures(longrepr):
    assert network_error(longrepr) is None


INNER = """\
import pytest

@pytest.fixture
def offline_fixture():
    raise AssertionError('curl: (6) Could not resolve host: github.com')

def test_offline_call():
    assert False, 'error: Request failed after 3 retries in 46.0s'

def test_offline_setup(offline_fixture):
    pass

def test_red():
    assert False, 'shellcheck: download failed (offline?)'

def test_red_with_a_network_literal_in_its_source():
    _ = 'curl: (6) Could not resolve host: github.com'
    assert 1 == 2

@pytest.mark.network_stays_red
def test_exempt():
    assert False, 'curl: (6) Could not resolve host: github.com'
"""


def _check(ok: bool, out: str) -> None:
    """Assert `ok` with the inner run as message, its network lines masked: the failure of
    this test must not read as a network error itself. `ok` is computed before the assert, so
    pytest writes no compared value into the explanation."""
    assert ok, re.sub(r"curl: \(|Request failed", "<net>", out)


# A name under .invalid never resolves (RFC 6761): the probe fails, as on a runner without DNS.
UNREACHABLE = "lighthouse-probe.invalid:443"


@pytest.fixture
def listener():
    """A local port that accepts a connection: the probe reaches it, as with the network up."""
    with socket.create_server(("127.0.0.1", 0)) as server:
        yield f"127.0.0.1:{server.getsockname()[1]}"


def test_offline_when_a_probe_target_is_unreachable(monkeypatch, listener):
    monkeypatch.setenv(PROBE_ENV, f"{listener},{UNREACHABLE}")
    assert offline() is True


def test_online_when_every_probe_target_answers(monkeypatch, listener):
    monkeypatch.setenv(PROBE_ENV, f"{listener},{listener}")
    assert offline() is False


def _inner(tmp_path: Path, probe: str) -> subprocess.CompletedProcess:
    (tmp_path / "conftest.py").write_text(
        "from tests.pack.python.conftest import pytest_runtest_makereport  # noqa: F401\n",
        encoding="utf-8",
    )
    (tmp_path / "test_x.py").write_text(INNER, encoding="utf-8")
    return subprocess.run(
        [sys.executable, "-m", "pytest", "-p", "no:cacheprovider", "-p", "no:xdist", "-rfEs",
         "--rootdir", str(tmp_path), str(tmp_path)],
        capture_output=True, text=True, cwd=ROOT, check=False,
        env={"PYTHONPATH": str(ROOT), "PATH": "/usr/bin:/bin", PROBE_ENV: probe},
    )  # fmt: skip


def _failed(out: str) -> list[str]:
    return sorted(re.findall(r"^(?:FAILED|ERROR) \S*::(test_\w+)", out, re.MULTILINE))


def test_hook_skips_a_network_failure_while_offline(tmp_path):
    result = _inner(tmp_path, UNREACHABLE)

    out = result.stdout
    _check(result.returncode == 1, out)
    _check("3 failed, 2 skipped" in out, out)
    _check(_failed(out) == [
        "test_exempt", "test_red", "test_red_with_a_network_literal_in_its_source"
    ], out)  # fmt: skip
    # The reason is the tool's own part of the line, not the text before it.
    _check("network: Request failed after 3 retries in 46.0s\n" in out, out)
    _check("network: curl: (6) Could not resolve host: github.com\n" in out, out)


def test_hook_keeps_a_network_failure_red_while_online(tmp_path, listener):
    result = _inner(tmp_path, listener)

    out = result.stdout
    _check(result.returncode == 1, out)
    # The fixture failure is an error, the four others are failures; nothing is skipped.
    _check("4 failed" in out and "1 error" in out and "skipped" not in out, out)
    _check(_failed(out) == [
        "test_exempt", "test_offline_call", "test_offline_setup", "test_red",
        "test_red_with_a_network_literal_in_its_source",
    ], out)  # fmt: skip
