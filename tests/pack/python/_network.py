"""A test that fails on a network error of the runner is skipped, not red (#338).

Some tests reach the network through a tool: `uvx` fetches ruff or ty, the shellcheck lib
downloads its pinned release with curl. On a runner without DNS for a moment, such a test
fails on the runner, not on lighthouse. `tests/pack/python/conftest.py` turns such a failure into a skip
whose reason is the line of the tool that names the network error, but only when the runner
is offline at that moment: `offline()` must fail to reach a probe target. With the network up,
a failure that names a network error stays red: then it is no blip of the runner.

Only the transport errors of the tools themselves count. A message of lighthouse's own
("download failed (offline?)", "does not start (offline, or the pin does not resolve)") does
not: a pin that does not exist says that too, and it must stay red.

pytest writes compared values into the explanation as well. A test that compares against
such a line as a value carries the marker `network_stays_red`, or its failure is a skip.
"""

import os
import re
import socket

# `host:port,host:port`: what `offline()` tries to reach. A test points it elsewhere.
PROBE_ENV = "LIGHTHOUSE_TEST_NETWORK_PROBE"
_PROBES = "pypi.org:443,github.com:443"
_PROBE_TIMEOUT_S = 5.0

# One pattern per transport error, as the tool prints it.
_PATTERNS = (
    # curl: 6 resolve host, 7 connect, 28 timeout, 35 TLS connect, 52 empty reply, 56 receive.
    re.compile(r"curl: \((?:6|7|28|35|52|56)\) .+"),
    # uv, after its own retries.
    re.compile(r"Request failed after \d+ retr.+"),
    re.compile(r"error sending request for url .+"),
    re.compile(r"dns error: .+"),
    # Go, while it fetches a module.
    re.compile(r"dial tcp: lookup \S+.*(?:no such host|i/o timeout)"),
)


def network_error(longrepr: str) -> str | None:
    """The first explanation line (`E   …`) of a pytest failure that names a network error
    of a tool, or None. Only the explanation counts, never the source lines of the traceback:
    a test that holds such a line as a literal must stay red on its own failure."""
    for line in longrepr.splitlines():
        if not line.startswith("E "):
            continue
        for pattern in _PATTERNS:
            match = pattern.search(line)
            if match:
                return match.group(0).strip()
    return None


def offline() -> bool:
    """True when a probe target (`PROBE_ENV`, default pypi.org and github.com, the hosts uv
    and the shellcheck lib fetch from) cannot be reached over TCP within 5 s."""
    for target in os.environ.get(PROBE_ENV, _PROBES).split(","):
        host, _, port = target.strip().rpartition(":")
        try:
            socket.create_connection((host, int(port)), timeout=_PROBE_TIMEOUT_S).close()
        except OSError:
            return True
    return False
