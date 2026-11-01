"""The bounded runner of `tools/hooks/checkpoint_scan.py` (#198).

`run-bounded` runs a command (gitleaks) in a systemd scope with a memory ceiling,
a CPU quota, an OOM score and a nice level. Every test points `LIGHTHOUSE_HOST_TOML`
at its own complete `host.toml`, so the real one in `~/.config` never takes part,
and puts stubs for `systemctl`, `systemd-run` and `choom` first on `PATH`, so no
test depends on the user bus of the host. Only `test_real_ceiling` uses the real
systemd, and skips without a working user bus.
"""

from __future__ import annotations

import importlib.util
import os
import shutil
import subprocess
import sys
from pathlib import Path

import pytest

from tests.pack._testkit import tool_bin

ROOT = Path(__file__).resolve().parents[1]
MODULE = ROOT / "tools" / "hooks" / "checkpoint_scan.py"

# The real tools the wrapped command line needs besides the stubs.
REAL_TOOLS = ("env", "nice", "python3")

SYSTEMCTL_OK = "#!/bin/sh\nexit 0\n"
SYSTEMCTL_FAIL = '#!/bin/sh\necho "Failed to connect to bus" >&2\nexit 1\n'
# Drops everything up to and including the first `--` and runs the rest.
_PASS_THROUGH = (
    'while [ "$#" -gt 0 ]; do\n'
    '  if [ "$1" = "--" ]; then shift; break; fi\n'
    "  shift\n"
    "done\n"
    'exec "$@"\n'
)
SYSTEMD_RUN = "#!/bin/sh\n" + _PASS_THROUGH
CHOOM = "#!/bin/sh\n" + _PASS_THROUGH
# What a shell reports for a child the kernel killed with SIGKILL.
SYSTEMD_RUN_KILLED = "#!/bin/sh\nexit 137\n"

BOUNDED = {"systemctl": SYSTEMCTL_OK, "systemd-run": SYSTEMD_RUN, "choom": CHOOM}


def _module():
    spec = importlib.util.spec_from_file_location("checkpoint_scan", MODULE)
    module = importlib.util.module_from_spec(spec)
    # dataclasses looks the module up in sys.modules while the class is built.
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    return module


@pytest.fixture
def cs():
    return _module()


def _host(tmp_path: Path, extra: str = "") -> Path:
    path = tmp_path / "host.toml"
    path.write_text(
        '[host]\nname = "h"\ntailscale_ip = "100.64.0.1"\n' + extra, encoding="utf-8"
    )
    return path


def _stub_path(tmp_path: Path, stubs: dict[str, str]) -> str:
    return str(tool_bin(tmp_path / "bin", REAL_TOOLS, stubs=stubs))


@pytest.fixture
def isolated(tmp_path, monkeypatch):
    """In-process tests: an own host.toml, and env changes undone afterwards."""
    monkeypatch.setenv("LIGHTHOUSE_HOST_TOML", str(_host(tmp_path)))
    monkeypatch.setenv("XDG_RUNTIME_DIR", str(tmp_path))
    return monkeypatch


def _cli(tmp_path: Path, path: str, *argv: str, host: Path | None = None):
    env = dict(
        os.environ,
        PATH=path,
        LIGHTHOUSE_HOST_TOML=str(host or _host(tmp_path)),
    )
    return subprocess.run(
        [sys.executable, "-I", str(MODULE), "run-bounded", "--", *argv],
        capture_output=True,
        text=True,
        env=env,
        cwd=tmp_path,
        timeout=60,
        check=False,
    )


def _contains(seq: list[str], part: list[str]) -> bool:
    return any(seq[i : i + len(part)] == part for i in range(len(seq) - len(part) + 1))


def test_wrap_builds_scope(cs, tmp_path, isolated):
    isolated.setenv("PATH", _stub_path(tmp_path, BOUNDED))
    command = ["gitleaks", "dir", "x"]

    argv, reason = cs.wrap(command, cs.Limits("2G", "200%", None))

    assert reason is None
    assert argv[:10] == [
        "systemd-run", "--user", "--scope", "--quiet",
        "-p", "MemoryMax=2G", "-p", "MemorySwapMax=0", "-p", "CPUQuota=200%",
    ]  # fmt: skip
    assert "--collect" in argv[: argv.index("--")]
    assert "GOMAXPROCS=2" in argv
    assert _contains(argv, ["choom", "-n", "800"])
    assert _contains(argv, ["nice", "-n", "10"])
    assert not any(a.startswith("--slice") for a in argv)
    assert argv[-3:] == command


def test_wrap_adds_slice_when_configured(cs, tmp_path, isolated):
    isolated.setenv("PATH", _stub_path(tmp_path, BOUNDED))

    argv, reason = cs.wrap(["gitleaks"], cs.Limits("2G", "200%", "s.slice"))

    assert reason is None
    # Ruling R1: extra systemd-run flags come after the three properties, before `--`.
    assert argv.index("--slice=s.slice") > argv.index("CPUQuota=200%")
    assert argv.index("--slice=s.slice") < argv.index("--")


@pytest.mark.parametrize(("quota", "procs"), [("200%", 2), ("250%", 2), ("50%", 1), ("400%", 4)])
def test_gomaxprocs_follows_quota(cs, tmp_path, isolated, quota, procs):
    isolated.setenv("PATH", _stub_path(tmp_path, BOUNDED))

    argv, _ = cs.wrap(["gitleaks"], cs.Limits("2G", quota, None))

    assert f"GOMAXPROCS={procs}" in argv


@pytest.mark.parametrize(
    ("drop", "systemctl", "reason"),
    [
        ("systemctl", SYSTEMCTL_OK, "no systemctl"),
        ("systemd-run", SYSTEMCTL_OK, "no systemd-run"),
        ("choom", SYSTEMCTL_OK, "no choom"),
        ("", SYSTEMCTL_FAIL, "systemctl --user failed"),
    ],
    ids=["no-systemctl", "no-systemd-run", "no-choom", "no-user-bus"],
)
def test_reason_ladder(cs, tmp_path, isolated, drop, systemctl, reason):
    stubs = dict(BOUNDED, systemctl=systemctl)
    stubs.pop(drop, None)
    isolated.setenv("PATH", _stub_path(tmp_path, stubs))

    argv, got = cs.wrap(["gitleaks"], cs.Limits("2G", "200%", None))

    assert got == reason
    # Without a scope the command still gets GOMAXPROCS and nice.
    assert "systemd-run" not in argv
    assert "GOMAXPROCS=2" in argv
    assert _contains(argv, ["nice", "-n", "10"])
    assert argv[-1] == "gitleaks"


def test_runtime_dir_repaired(cs, tmp_path, isolated):
    """VS Code runs without XDG_RUNTIME_DIR; the wrapper restores it for systemctl."""
    runtime = Path(f"/run/user/{os.getuid()}")
    if not runtime.is_dir():
        pytest.skip(f"{runtime} does not exist on this host")
    systemctl = f'#!/bin/sh\n[ "$XDG_RUNTIME_DIR" = "{runtime}" ] || exit 1\nexit 0\n'
    isolated.setenv("PATH", _stub_path(tmp_path, dict(BOUNDED, systemctl=systemctl)))
    isolated.delenv("XDG_RUNTIME_DIR", raising=False)
    isolated.delenv("DBUS_SESSION_BUS_ADDRESS", raising=False)

    _, reason = cs.wrap(["gitleaks"], cs.Limits("2G", "200%", None))

    assert reason is None


def test_no_systemd_says_why(tmp_path):
    stubs = {"systemctl": SYSTEMCTL_OK, "choom": CHOOM}

    result = _cli(tmp_path, _stub_path(tmp_path, stubs), "python3", "-c", "print('ran')")

    assert result.returncode == 0, result.stderr
    assert "ran" in result.stdout
    assert "secret-scan: no memory ceiling: no systemd-run" in result.stderr


def test_max_rss_measured(cs, tmp_path, isolated):
    isolated.setenv("PATH", _stub_path(tmp_path, {"systemctl": SYSTEMCTL_OK}))
    limits = cs.Limits("2G", "200%", None)

    small = cs.run_bounded(["python3", "-c", "pass"], limits)
    big = cs.run_bounded(["python3", "-c", "b = bytearray(32 * 2**20)"], limits)
    huge = cs.run_bounded(["python3", "-c", "b = bytearray(128 * 2**20)"], limits)

    assert big.max_rss_kb is not None and big.max_rss_kb >= 32 * 1024
    # The forked child starts at this process's RSS, so `small` already reports
    # tens of MB; only the contrast shows that the child is measured, not pytest.
    assert small.max_rss_kb is not None and huge.max_rss_kb is not None
    assert huge.max_rss_kb - small.max_rss_kb >= 64 * 1024
    assert (big.rc, big.ceiling_hit) == (0, False)


@pytest.mark.parametrize("rc", [0, 1])
def test_rc_passed_through(tmp_path, rc):
    stubs = dict(BOUNDED, gitleaks=f"#!/bin/sh\necho 'report line'\nexit {rc}\n")

    result = _cli(tmp_path, _stub_path(tmp_path, stubs), "gitleaks", "git", ".")

    assert result.returncode == rc, result.stderr
    assert "report line" in result.stdout


def test_ceiling_message_on_sigkill(tmp_path):
    stubs = dict(BOUNDED, **{"systemd-run": SYSTEMD_RUN_KILLED})

    result = _cli(tmp_path, _stub_path(tmp_path, stubs), "gitleaks", "git", ".")

    assert result.returncode == 2
    assert (
        "secret-scan: ABORTED at memory ceiling (2G); raise host.scan_memory_max"
        in result.stderr
    )


def test_other_rc_is_could_not_check(tmp_path):
    stubs = dict(BOUNDED, gitleaks="#!/bin/sh\nexit 5\n")

    result = _cli(tmp_path, _stub_path(tmp_path, stubs), "gitleaks", "git", ".")

    assert result.returncode == 2
    assert "COULD NOT CHECK — gitleaks exited 5" in result.stderr
    assert "ABORTED" not in result.stderr


def test_config_error_is_could_not_check(tmp_path):
    host = _host(tmp_path, 'scan_memory_max = "lots"\n')
    marker = tmp_path / "ran"
    stubs = dict(BOUNDED, gitleaks=f"#!/bin/sh\ntouch {marker}\nexit 0\n")

    result = _cli(tmp_path, _stub_path(tmp_path, stubs), "gitleaks", host=host)

    assert result.returncode == 2
    assert "COULD NOT CHECK — " in result.stderr
    assert "scan_memory_max" in result.stderr
    assert not marker.exists()


def test_limits_default_without_host_toml(cs, tmp_path, isolated):
    isolated.setenv("LIGHTHOUSE_HOST_TOML", str(tmp_path / "absent.toml"))

    assert cs.load_limits() == cs.Limits("2G", "200%", None)


def test_limits_default_without_keys(cs, isolated):
    assert cs.load_limits() == cs.Limits("2G", "200%", None)


def test_limits_from_host_toml(cs, tmp_path, isolated):
    extra = 'scan_memory_max = "1G"\nscan_cpu_quota = "100%"\ngate_slice = "s.slice"\n'
    isolated.setenv("LIGHTHOUSE_HOST_TOML", str(_host(tmp_path, extra)))

    assert cs.load_limits() == cs.Limits("1G", "100%", "s.slice")


def _user_bus() -> bool:
    if not (shutil.which("systemd-run") and shutil.which("choom")):
        return False
    try:
        probe = subprocess.run(
            ["systemctl", "--user", "show-environment"],
            capture_output=True,
            timeout=10,
            check=False,
        )
    except (OSError, subprocess.TimeoutExpired):
        return False
    return probe.returncode == 0


@pytest.mark.skipif(not _user_bus(), reason="no working systemd user bus")
def test_real_ceiling(tmp_path):
    host = _host(tmp_path, 'scan_memory_max = "8M"\n')

    result = _cli(
        tmp_path,
        os.environ["PATH"],
        "python3",
        "-c",
        "b = bytearray(64 * 2**20)",
        host=host,
    )

    assert result.returncode == 2, result.stderr
    assert "secret-scan: ABORTED at memory ceiling (8M)" in result.stderr
