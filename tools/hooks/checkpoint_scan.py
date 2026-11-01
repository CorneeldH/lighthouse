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
    scan --remote <name> [--full] [--keep-going] [--config <toml>] <ref>=<sha>...
                               the blob mode for `refs/entire/*`: every blob the
                               remote misses and the ledger does not hold, once,
                               in batches of gitleaks `dir`. Exit 0 clean, 1
                               findings (one line per ref and path), 2 could not
                               scan, including more than HOOK_VOLUME_LIMIT
                               unscanned without `--full`.

The ledger, `<git-common-dir>/lighthouse/secret-scan/clean-<key>.txt`, holds the
blobs gitleaks saw without a finding in a batch that completed (rc 0 or 1 with a
report read). The key covers the config and the gitleaks version, so a change to
either starts a new ledger and a full rescan.

Runs on the host's python3 with `-I`: stdlib only, and `config.py` is found relative
to this file, never through the cwd.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import re
import shutil
import subprocess
import sys
import tempfile
from collections import Counter
from collections.abc import Iterable
from dataclasses import dataclass, field
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent.parent.parent / ".lighthouse" / "lib"))

from lighthouse_lib.config import ConfigError, host_path, load_host

PREFIX = "secret-scan"
DEFAULT_MEMORY_MAX = "2G"
DEFAULT_CPU_QUOTA = "200%"
# The same config as CI and the commit-range scan of the hook.
DEFAULT_CONFIG = Path(__file__).resolve().parent.parent.parent / ".gitleaks.toml"

# Blobs per gitleaks run, by size: the batch bounds the findings, and so the memory.
BATCH_BYTES = 64 * 2**20
# More unscanned than this and the hook refuses instead of scanning: run `push`.
HOOK_VOLUME_LIMIT = 256 * 2**20
# More findings of one rule in one batch: an allowlist is probably missing.
FINDINGS_CAP = 200
# Test-only overrides of BATCH_BYTES and HOOK_VOLUME_LIMIT.
BATCH_BYTES_ENV = "LIGHTHOUSE_SCAN_BATCH_BYTES"
VOLUME_LIMIT_ENV = "LIGHTHOUSE_SCAN_VOLUME_LIMIT"

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


# The probe runs once per process: every batch of one scan is bounded the same way.
_probed: list[str | None] = []


def _no_ceiling_reason() -> str | None:
    """Why there is no scope here; the first hit is the reason. None: there is one."""
    if not _probed:
        _probed.append(_probe_ceiling())
    return _probed[0]


def _probe_ceiling() -> str | None:
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


@dataclass
class Blob:
    size: int
    # (ref, path): where the blob sits, for the message.
    places: set[tuple[str, str]] = field(default_factory=set)


@dataclass
class Finding:
    blob: str
    rule: str
    match: str


@dataclass
class ScanResult:
    findings: list[Finding]
    # The cap lines: one per rule with more than FINDINGS_CAP findings in one batch.
    overloaded: list[str]
    # The line that ended the scan early (ceiling, COULD NOT CHECK); None: completed.
    aborted: str | None


class _ScanError(Exception):
    """A batch that could not be scanned: COULD NOT CHECK."""


def _git(repo: Path, *args: str, stdin: str | None = None) -> str:
    try:
        proc = subprocess.run(
            ["git", "-C", str(repo), *args],
            input=stdin,
            capture_output=True,
            text=True,
            encoding="utf-8",
            errors="surrogateescape",
            check=False,
        )
    except OSError as exc:
        raise _could_not_check(f"git did not start: {exc}") from exc
    if proc.returncode != 0:
        raise _could_not_check(f"git {args[0]} failed: {proc.stderr.strip()}")
    return proc.stdout


def missing_blobs(repo: Path, refs: dict[str, str], remote: str) -> dict[str, Blob]:
    """Every blob reachable from `refs` (ref -> sha) but not from `remote`'s
    remote-tracking refs, with its size and the (ref, path) places it sits in."""
    places: dict[str, set[tuple[str, str]]] = {}
    for ref, sha in refs.items():
        out = _git(repo, "rev-list", "--objects", sha, "--not", f"--remotes={remote}")
        for line in out.splitlines():
            oid, named, path = line.partition(" ")
            if named:  # trees and blobs; commits carry no path
                places.setdefault(oid, set()).add((ref, path))
    if not places:
        return {}
    blobs: dict[str, Blob] = {}
    check = _git(repo, "cat-file", "--batch-check", stdin="".join(f"{oid}\n" for oid in places))
    for line in check.splitlines():
        parts = line.split(" ")
        if len(parts) != 3:
            raise _could_not_check(f"git cat-file: {line}")
        oid, kind, size = parts
        if kind == "blob":
            blobs[oid] = Blob(int(size), places[oid])
    return blobs


def ledger_path(repo: Path, config: Path) -> Path:
    """The ledger for this config and this gitleaks, shared by every worktree."""
    common = _git(repo, "rev-parse", "--path-format=absolute", "--git-common-dir").strip()
    try:
        body = config.read_bytes()
    except OSError as exc:
        raise _could_not_check(f"cannot read {config}: {exc}") from exc
    try:
        version = subprocess.run(
            ["gitleaks", "version"],
            stdin=subprocess.DEVNULL,
            capture_output=True,
            timeout=30,
            check=False,
        )
    except (OSError, subprocess.TimeoutExpired) as exc:
        raise _could_not_check(f"gitleaks version failed: {exc}") from exc
    if version.returncode != 0:
        raise _could_not_check(f"gitleaks version exited {version.returncode}")
    key = hashlib.sha256(body + version.stdout.strip()).hexdigest()
    return Path(common) / "lighthouse" / "secret-scan" / f"clean-{key}.txt"


def load_ledger(path: Path) -> set[str]:
    try:
        return set(path.read_text(encoding="utf-8").split())
    except FileNotFoundError:
        return set()
    except OSError as exc:
        raise _could_not_check(f"cannot read the ledger {path}: {exc}") from exc


def add_to_ledger(path: Path, blobs: Iterable[str]) -> None:
    """Merge `blobs` into the ledger: sorted, through a temp file and a rename, so
    a kill leaves the old ledger or the new one, never half of one."""
    new = set(blobs)
    if not new:
        return
    path.parent.mkdir(parents=True, exist_ok=True)
    merged = load_ledger(path) | new
    fd, tmp = tempfile.mkstemp(dir=path.parent, prefix=f"{path.name}.", suffix=".tmp")
    try:
        with os.fdopen(fd, "w", encoding="utf-8") as out:
            out.write("".join(f"{oid}\n" for oid in sorted(merged)))
            out.flush()
            os.fsync(out.fileno())
        os.replace(tmp, path)
    except BaseException:
        Path(tmp).unlink(missing_ok=True)
        raise


def batches(blobs: dict[str, Blob], limit: int = BATCH_BYTES) -> list[list[str]]:
    """The blobs by size, smallest first, in batches of at most `limit` bytes; a
    blob larger than `limit` gets a batch of its own."""
    out: list[list[str]] = []
    batch: list[str] = []
    total = 0
    for oid in sorted(blobs, key=lambda b: (blobs[b].size, b)):
        size = blobs[oid].size
        if batch and total + size > limit:
            out.append(batch)
            batch, total = [], 0
        batch.append(oid)
        total += size
    if batch:
        out.append(batch)
    return out


def _env_bytes(name: str, default: int) -> int:
    value = os.environ.get(name)
    if not value:
        return default
    try:
        return int(value)
    except ValueError as exc:
        raise _could_not_check(f"{name}={value} is not a number of bytes") from exc


def _mb(size: int) -> str:
    return f"{size / 2**20:.0f}"


def _write_blobs(repo: Path, batch: list[str], dest: Path) -> None:
    """Each blob of `batch` to `dest/<blob id>`, through one `git cat-file --batch`,
    streamed: no more than one chunk of a blob is in memory at once."""
    with tempfile.TemporaryFile() as ids:
        ids.write("".join(f"{oid}\n" for oid in batch).encode())
        ids.seek(0)
        proc = subprocess.Popen(
            ["git", "-C", str(repo), "cat-file", "--batch"],
            stdin=ids,
            stdout=subprocess.PIPE,
            stderr=subprocess.DEVNULL,
        )
    try:
        for oid in batch:
            header = proc.stdout.readline().decode(errors="replace").split()
            if header[:2] != [oid, "blob"] or len(header) != 3:
                raise _ScanError(f"git cat-file --batch gave {' '.join(header) or 'nothing'} for {oid}")
            left = int(header[2])
            with open(dest / oid, "wb") as out:
                while left:
                    chunk = proc.stdout.read(min(left, 2**20))
                    if not chunk:
                        raise _ScanError(f"git cat-file --batch ended inside {oid}")
                    out.write(chunk)
                    left -= len(chunk)
            proc.stdout.read(1)  # the newline after the content
    finally:
        proc.stdout.close()
        if proc.poll() is None:
            proc.kill()
        proc.wait()


def _read_report(path: Path) -> list[dict] | None:
    """The gitleaks JSON report, or None when it is missing or not a list of findings."""
    try:
        data = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, ValueError):
        return None
    if not isinstance(data, list):
        return None
    for item in data:
        if not isinstance(item, dict):
            return None
        if not all(isinstance(item.get(key), str) for key in ("RuleID", "File", "Match")):
            return None
    return data


def _one_line(text: str) -> str:
    return text.replace("\r", "\\r").replace("\n", "\\n")


def _scan_batch(
    repo: Path, batch: list[str], config: Path, limits: Limits, i: int
) -> tuple[Run, list[Finding] | None]:
    """One batch in its own temp dir `<root>`: blobs in `<root>/blobs/`, the report
    at `<root>/report.json`, which does not exist before gitleaks runs. Returns the
    run and its findings; findings None when the ceiling was hit."""
    with tempfile.TemporaryDirectory(prefix="secret-scan-") as tmp:
        root = Path(tmp)
        blobdir = root / "blobs"
        blobdir.mkdir()
        report = root / "report.json"
        try:
            _write_blobs(repo, batch, blobdir)
        except OSError as exc:
            raise _ScanError(f"cannot write the blobs of batch {i}: {exc}") from exc
        argv = [
            "gitleaks", "dir", str(blobdir), "--config", str(config), "--no-banner",
            "--redact", "-f", "json", "-r", str(report), "--exit-code", "1",
        ]  # fmt: skip
        try:
            run = run_bounded(argv, limits)
        except OSError as exc:
            raise _ScanError(f"gitleaks did not start: {exc}") from exc
        if run.ceiling_hit:
            return run, None
        if run.rc not in (0, 1):
            raise _ScanError(f"gitleaks exited {run.rc} in batch {i}", run.output)
        items = _read_report(report)
    if items is None:
        raise _ScanError(f"gitleaks exited {run.rc} in batch {i} without a readable report", run.output)
    # gitleaks also exits 1 on an error; rc 1 is findings only with findings to show.
    if run.rc == 1 and not items:
        raise _ScanError(f"gitleaks exited 1 in batch {i} with no findings in its report", run.output)
    members = set(batch)
    found = []
    for item in items:
        # `<root>/blobs/<blob id>`; an archive member would add `!<inner path>`.
        oid = Path(item["File"]).name.split("!")[0]
        if oid not in members:
            raise _ScanError(f"the report of batch {i} names {item['File']}, not a blob of the batch")
        found.append(Finding(oid, item["RuleID"], _one_line(item["Match"])))
    return run, found


def scan_blobs(
    repo: Path,
    blobs: dict[str, Blob],
    config: Path,
    limits: Limits,
    ledger: Path | None,
    stop_on_cap: bool,
) -> ScanResult:
    """Scan `blobs` in batches and report each batch on stderr as it ends.

    After every completed batch its blobs without a finding go into `ledger`. A
    ceiling hit or a batch that could not be scanned ends the scan with `aborted`
    set, and nothing of that batch goes into the ledger. A rule over FINDINGS_CAP
    in one batch ends the scan after that batch only when `stop_on_cap`; without it
    (the measurement) every batch also reports gitleaks' peak RSS.
    """
    result = ScanResult(findings=[], overloaded=[], aborted=None)

    def abort(line: str) -> ScanResult:
        print(line, file=sys.stderr)
        result.aborted = line
        return result

    plan = batches(blobs, _env_bytes(BATCH_BYTES_ENV, BATCH_BYTES))
    for i, batch in enumerate(plan, 1):
        label = f"batch {i}/{len(plan)}"
        try:
            run, found = _scan_batch(repo, batch, config, limits, i)
        except _ScanError as exc:
            message, *output = exc.args
            if output and output[0].strip():
                sys.stderr.write(output[0].rstrip("\n") + "\n")
            return abort(f"{PREFIX}: COULD NOT CHECK — {message}")
        if found is None:
            return abort(
                f"{PREFIX}: ABORTED at memory ceiling ({limits.memory_max}) in batch {i}; "
                "raise host.scan_memory_max or lower the batch size"
            )
        dirty = {f.blob for f in found}
        if ledger is not None:
            try:
                add_to_ledger(ledger, (oid for oid in batch if oid not in dirty))
            except OSError as exc:
                return abort(f"{PREFIX}: COULD NOT CHECK — cannot write the ledger {ledger}: {exc}")
        result.findings.extend(found)
        size = sum(blobs[oid].size for oid in batch)
        print(f"{PREFIX}: {label}, {_mb(size)} MB, {len(found)} findings", file=sys.stderr)
        if not stop_on_cap:
            rss = "unknown" if run.max_rss_kb is None else f"{run.max_rss_kb / 1024:.0f}"
            print(f"{PREFIX}: {label} peak RSS {rss} MB", file=sys.stderr)
        capped = False
        for rule, n in sorted(Counter(f.rule for f in found).items()):
            if n > FINDINGS_CAP:
                sample = next(f.match for f in found if f.rule == rule)
                line = (
                    f"{PREFIX}: {n} findings for rule {rule} in one batch; an allowlist is "
                    f"probably missing for this pattern (sample: {sample})"
                )
                print(line, file=sys.stderr)
                result.overloaded.append(line)
                capped = True
        if capped and stop_on_cap:
            break
    return result


_REF_SHA = re.compile(r"(?P<ref>.+)=(?P<sha>[0-9a-f]{40}|[0-9a-f]{64})")


def _scan_cli(ns: argparse.Namespace, parser: argparse.ArgumentParser) -> int:
    refs: dict[str, str] = {}
    for arg in ns.refs:
        hit = _REF_SHA.fullmatch(arg)
        if hit is None:
            parser.error(f"not <ref>=<sha>: {arg}")
        if set(hit["sha"]) != {"0"}:  # a deletion sends no objects
            refs[hit["ref"]] = hit["sha"]
    limits = load_limits()
    repo = Path.cwd()
    config = Path(ns.config)
    if not config.is_file():
        raise _could_not_check(f"cannot find {config}; scans must use the same config as CI")
    ledger = ledger_path(repo, config)
    blobs = missing_blobs(repo, refs, ns.remote)
    if not ns.full:
        clean = load_ledger(ledger)
        blobs = {oid: blob for oid, blob in blobs.items() if oid not in clean}
        volume = sum(blob.size for blob in blobs.values())
        if volume > _env_bytes(VOLUME_LIMIT_ENV, HOOK_VOLUME_LIMIT):
            print(
                f"pre-push: BLOCKED — {len(blobs)} checkpoint blobs ({_mb(volume)} MB) are not "
                "yet scanned. A first push after a bulk import ('entire import') is heavy; "
                f"run: python3 tools/hooks/checkpoint_scan.py push {ns.remote}",
                file=sys.stderr,
            )
            return 2
    result = scan_blobs(repo, blobs, config, limits, ledger, stop_on_cap=not ns.keep_going)
    lines = {
        f"{ref}  {path}  {f.rule}  {f.match}"
        for f in result.findings
        for ref, path in blobs[f.blob].places
    }
    for line in sorted(lines):
        print(line)
    if result.aborted is not None:
        return 2
    return 1 if result.findings else 0


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
    scan = sub.add_parser("scan", help="scan the blobs a remote misses, per unique blob")
    scan.add_argument("--remote", required=True, help="the remote the push goes to")
    scan.add_argument("--full", action="store_true", help="ignore the ledger (still write it)")
    scan.add_argument(
        "--keep-going",
        action="store_true",
        help="do not stop at the findings cap, and report peak RSS per batch (the measurement)",
    )
    scan.add_argument("--config", default=str(DEFAULT_CONFIG), help="the gitleaks config")
    scan.add_argument("refs", nargs="+", metavar="<ref>=<sha>")
    ns = parser.parse_args(head)
    if ns.cmd == "run-bounded":
        if not command:
            parser.error("run-bounded needs a command after --")
        return _run_bounded_cli(command)
    if command:
        parser.error("scan takes no command after --")
    return _scan_cli(ns, parser)


if __name__ == "__main__":
    sys.exit(main())
