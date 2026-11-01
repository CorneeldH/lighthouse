#!/usr/bin/env python3
"""Secret scan of entire checkpoints, bounded (#198).

On 2026-11-01 an unbounded `gitleaks` over every checkpoint pushed the host into
swap. Every gitleaks run of the pre-push scan therefore goes through this module,
which puts it in a systemd scope with a memory ceiling, no swap, a CPU quota, an
OOM score and a nice level:

    systemd-run --user --scope --quiet \
      -p MemoryMax=<max> -p MemorySwapMax=0 -p CPUQuota=<quota> --collect [--slice=<slice>] \
      -- env GOMAXPROCS=<n> choom -n 800 -- nice -n 10 gitleaks ...

The limits come from `[host]` in `host.toml`: `scan_memory_max` (default 2G),
`scan_cpu_quota` (default 200%) and `gate_slice` (only when set). Without systemd the
command still runs, with GOMAXPROCS and nice, and the output says why there is no
ceiling.

Subcommands:

    run-bounded -- <argv...>   run one command bounded; rc 0 and 1 pass through,
                               a ceiling hit or any other rc exits 2.

Runs on the host's python3 with `-I`: stdlib only, and `config.py` is found relative
to this file, never through the cwd.
"""

from __future__ import annotations

import argparse
import os
import shutil
import subprocess
import sys
import tempfile
from dataclasses import dataclass
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent.parent.parent / ".lighthouse" / "lib"))

from lighthouse_lib.config import ConfigError, host_path, load_host

PREFIX = "secret-scan"
DEFAULT_MEMORY_MAX = "2G"
DEFAULT_CPU_QUOTA = "200%"

# Ceiling signal, measured on systemd 259 (Fedora 44) with `test_real_ceiling`'s
# setup (MemoryMax=8M, python allocating 64 MB), eight runs plus three by hand:
#
#   - the return code was -9 (SIGKILL) every time from Popen, 137 from a shell.
#     `systemd-run --scope` execs the command, so the OOM kill hits the process we
#     wait for and its status carries the signal.
#   - `oom_kill` in the scope's `memory.events` (with a known `--unit=`) was 1 when
#     read at once in seven of eight runs; in the eighth, and in every read after
#     0.5 s, the scope cgroup was already gone: systemd removes it as soon as the
#     scope is empty.
#
# So the return code is the signal, and `memory.events` is not read at all: a check
# that sometimes finds no file adds nothing the return code does not already say.
CEILING_RCS = (137, -9)


@dataclass(frozen=True)
class Limits:
    memory_max: str
    cpu_quota: str
    slice: str | None


@dataclass
class Run:
    rc: int
    ceiling_hit: bool
    output: str
    max_rss_kb: int | None


def _could_not_check(message: str) -> SystemExit:
    print(f"{PREFIX}: COULD NOT CHECK — {message}", file=sys.stderr)
    return SystemExit(2)


def load_limits() -> Limits:
    """The scan limits from `host.toml`; defaults for a missing key or file.

    A `host.toml` that does not validate exits 2: a scan with limits nobody chose
    is not a bounded scan.
    """
    path = host_path()
    if not path.exists():
        return Limits(DEFAULT_MEMORY_MAX, DEFAULT_CPU_QUOTA, None)
    try:
        host = load_host(path)
    except ConfigError as exc:
        raise _could_not_check(str(exc)) from exc
    return Limits(
        memory_max=host.scan_memory_max or DEFAULT_MEMORY_MAX,
        cpu_quota=host.scan_cpu_quota or DEFAULT_CPU_QUOTA,
        slice=host.gate_slice,
    )


def _gomaxprocs(cpu_quota: str) -> int:
    """200% means two CPUs; rounded down, at least one."""
    return max(1, int(cpu_quota.rstrip("%")) // 100)


def _repair_runtime_dir() -> None:
    # VS Code `claude` runs in the logind scope without XDG_RUNTIME_DIR and
    # sometimes without DBUS_SESSION_BUS_ADDRESS; without a bus `systemctl --user`
    # fails and the ceiling drops out on exactly the path that needs it.
    # Same repair as `commit-test-gate.sh`.
    runtime = f"/run/user/{os.getuid()}"
    if (
        not os.environ.get("XDG_RUNTIME_DIR")
        and not os.environ.get("DBUS_SESSION_BUS_ADDRESS")
        and os.path.isdir(runtime)
    ):
        os.environ["XDG_RUNTIME_DIR"] = runtime


def _no_ceiling_reason() -> str | None:
    """Why there is no scope here; the first hit is the reason. None: there is one."""
    _repair_runtime_dir()
    for tool in ("systemctl", "systemd-run", "choom"):
        if shutil.which(tool) is None:
            return f"no {tool}"
    try:
        probe = subprocess.run(
            ["systemctl", "--user", "show-environment"],
            stdin=subprocess.DEVNULL,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
            timeout=10,
            check=False,
        )
    except (OSError, subprocess.TimeoutExpired):
        return "systemctl --user failed"
    if probe.returncode != 0:
        return "systemctl --user failed"
    return None


def wrap(argv: list[str], limits: Limits) -> tuple[list[str], str | None]:
    """The bounded command line, and the reason when it has no memory ceiling."""
    procs = f"GOMAXPROCS={_gomaxprocs(limits.cpu_quota)}"
    reason = _no_ceiling_reason()
    if reason is not None:
        return ["env", procs, "nice", "-n", "10", *argv], reason
    scope = [
        "systemd-run", "--user", "--scope", "--quiet",
        "-p", f"MemoryMax={limits.memory_max}",
        "-p", "MemorySwapMax=0",
        "-p", f"CPUQuota={limits.cpu_quota}",
        # Unload the scope when it ends, failed or not: else every ceiling hit
        # leaves a failed unit behind in `systemctl --user --failed`.
        "--collect",
    ]  # fmt: skip
    if limits.slice:
        scope.append(f"--slice={limits.slice}")
    return [*scope, "--", "env", procs, "choom", "-n", "800", "--", "nice", "-n", "10", *argv], None


# A reason is said once per process, not once per batch.
_said: set[str] = set()


def _max_rss_kb(ru_maxrss: int) -> int | None:
    if sys.platform.startswith("linux"):
        return ru_maxrss
    if sys.platform == "darwin":
        return ru_maxrss // 1024  # bytes there
    return None


def run_bounded(argv: list[str], limits: Limits) -> Run:
    """Run `argv` bounded and wait for it; stdout and stderr together in `output`.

    Raises OSError when the command line cannot start at all.
    """
    wrapped, reason = wrap(argv, limits)
    if reason is not None and reason not in _said:
        _said.add(reason)
        print(f"{PREFIX}: no memory ceiling: {reason}", file=sys.stderr)
    # A file, not a pipe: wait4 does not drain a pipe, and a full one would hang the child.
    with tempfile.TemporaryFile() as out:
        proc = subprocess.Popen(
            wrapped, stdin=subprocess.DEVNULL, stdout=out, stderr=subprocess.STDOUT
        )
        # wait4, not Popen.wait: only wait4 returns the child's rusage. Every link of
        # the chain (systemd-run --scope, env, choom, nice) execs, so it is gitleaks'.
        # Its floor is this process's RSS at the fork: a child starts with the
        # parent's pages, so a small command reports what this python held.
        _, status, usage = os.wait4(proc.pid, 0)
        proc.returncode = rc = os.waitstatus_to_exitcode(status)
        out.seek(0)
        output = out.read().decode("utf-8", errors="replace")
    return Run(
        rc=rc,
        ceiling_hit=reason is None and rc in CEILING_RCS,
        output=output,
        max_rss_kb=_max_rss_kb(usage.ru_maxrss),
    )


def _run_bounded_cli(argv: list[str]) -> int:
    limits = load_limits()
    name = Path(argv[0]).name
    try:
        run = run_bounded(argv, limits)
    except OSError as exc:
        raise _could_not_check(f"{name} did not start: {exc}") from exc
    sys.stdout.write(run.output)
    if run.ceiling_hit:
        print(
            f"{PREFIX}: ABORTED at memory ceiling ({limits.memory_max}); "
            "raise host.scan_memory_max",
            file=sys.stderr,
        )
        return 2
    if run.rc in (0, 1):
        return run.rc
    raise _could_not_check(f"{name} exited {run.rc}")


def main(args: list[str] | None = None) -> int:
    args = sys.argv[1:] if args is None else args
    # Everything after the first `--` is the command, untouched by argparse.
    if "--" in args:
        split = args.index("--")
        head, command = args[:split], args[split + 1 :]
    else:
        head, command = args, []
    parser = argparse.ArgumentParser(prog="checkpoint_scan.py", description=__doc__.split("\n")[0])
    sub = parser.add_subparsers(dest="cmd", required=True)
    sub.add_parser("run-bounded", help="run one command bounded: run-bounded -- <argv...>")
    ns = parser.parse_args(head)
    if ns.cmd == "run-bounded":
        if not command:
            parser.error("run-bounded needs a command after --")
        return _run_bounded_cli(command)
    parser.error(f"unknown command {ns.cmd}")
    return 2


if __name__ == "__main__":
    sys.exit(main())
