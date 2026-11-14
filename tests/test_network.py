"""tests/_network.py and the hook in tests/conftest.py: a network error of the runner is a
skip, every other failure stays red (#338)."""

import re
import subprocess
import sys
from pathlib import Path

import pytest

from tests._network import network_error

ROOT = Path(__file__).resolve().parents[1]


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


def test_hook_skips_a_network_failure_and_keeps_the_rest_red(tmp_path):
    (tmp_path / "conftest.py").write_text(
        "from tests.conftest import pytest_configure, pytest_runtest_makereport  # noqa: F401\n",
        encoding="utf-8",
    )
    (tmp_path / "test_x.py").write_text(INNER, encoding="utf-8")

    result = subprocess.run(
        [sys.executable, "-m", "pytest", "-p", "no:cacheprovider", "-p", "no:xdist", "-rfEs",
         "--rootdir", str(tmp_path), str(tmp_path)],
        capture_output=True, text=True, cwd=ROOT, check=False,
        env={"PYTHONPATH": str(ROOT), "PATH": "/usr/bin:/bin"},
    )  # fmt: skip

    out = result.stdout
    _check(result.returncode == 1, out)
    _check("3 failed, 2 skipped" in out, out)
    failed = re.findall(r"^(?:FAILED|ERROR) \S*::(test_\w+)", out, re.MULTILINE)
    _check(sorted(failed) == [
        "test_exempt", "test_red", "test_red_with_a_network_literal_in_its_source"
    ], out)  # fmt: skip
    # The reason is the tool's own part of the line, not the text before it.
    _check("network: Request failed after 3 retries in 46.0s\n" in out, out)
    _check("network: curl: (6) Could not resolve host: github.com\n" in out, out)
