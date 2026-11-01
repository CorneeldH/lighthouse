"""The bounded runner of `tools/hooks/checkpoint_scan.py` (#198).

`run-bounded` runs a command (gitleaks) in a systemd scope with a memory ceiling,
a CPU quota, an OOM score and a nice level. Every test points `LIGHTHOUSE_HOST_TOML`
at its own complete `host.toml`, so the real one in `~/.config` never takes part,
and puts stubs for `systemctl`, `systemd-run` and `choom` first on `PATH`, so no
test depends on the user bus of the host. Only `test_real_ceiling` uses the real
systemd, and skips without a working user bus.

`scan` is the blob mode of the pre-push hook: the blobs the remote misses, minus the
ledger, in batches of a fake gitleaks that logs what it got. Only
`test_real_gitleaks_finds_pat` runs the real gitleaks, on a two-checkpoint fixture.
"""

from __future__ import annotations

import hashlib
import importlib.util
import json
import os
import re
import shutil
import subprocess
import sys
import time
from pathlib import Path

import pytest

from tests.pack._testkit import GIT_ENV, git, init_repo, tool_bin
from tests.test_pre_push_secret_scan import FAKE_PAT, needs_gitleaks

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


def test_probe_cached_per_process(cs, tmp_path, isolated):
    """Every batch of one run is bounded the same way: the bus is probed once."""
    count = tmp_path / "probes"
    # Succeeds on the first probe only.
    systemctl = f'#!/bin/sh\necho x >> "{count}"\n[ -e "{count}.seen" ] && exit 1\n: > "{count}.seen"\n'
    isolated.setenv("PATH", _stub_path(tmp_path, dict(BOUNDED, systemctl=systemctl)))
    limits = cs.Limits("2G", "200%", None)

    first, _ = cs.wrap(["gitleaks"], limits)
    second, reason = cs.wrap(["gitleaks"], limits)

    assert reason is None
    assert first == second
    assert count.read_text().count("x") == 1


# --- scan: blob mode, ledger and batches ------------------------------------------

# A fake gitleaks. `version` prints $FAKE_GITLEAKS_VERSION (default `fake 1`).
# `dir <dir> ... -r <report>` counts its calls next to $FAKE_GITLEAKS_LOG, logs the
# name of every file it got, and writes a JSON report with one `fake-rule` finding
# per file holding FAKESECRET; rc 1 with findings, else 0. Like gitleaks it says
# `scanned ~<n> bytes` on stderr, the bytes of every file it got. Knobs:
#   FAKE_GITLEAKS_KILL_AT=<n>   the n-th `dir` call SIGKILLs itself before reading
#   FAKE_GITLEAKS_REPORT=none   write no report; =garbage: write one that is no JSON
#   FAKE_GITLEAKS_RC=<rc>       exit with this rc instead
#   FAKE_GITLEAKS_SCANNED=none  say nothing about the bytes; =short: one byte too few
#   FAKE_GITLEAKS_MEMBER=<path> name the finding the way gitleaks names an archive
#                               member: `<file>!<path>`
#   FAKE_GITLEAKS_MOVE="<git dir> <ref> <sha>"
#                               the first `dir` call sets <ref> to <sha>: a ref
#                               that moves while it is scanned
FAKE_GITLEAKS = f"""#!{sys.executable}
import json, os, signal, sys

args = sys.argv[1:]
if args[:1] == ["version"]:
    print(os.environ.get("FAKE_GITLEAKS_VERSION", "fake 1"))
    sys.exit(0)
assert args[0] == "dir", args
log = os.environ["FAKE_GITLEAKS_LOG"]
calls = log + ".calls"
n = (int(open(calls).read()) if os.path.exists(calls) else 0) + 1
open(calls, "w").write(str(n))
if os.environ.get("FAKE_GITLEAKS_KILL_AT") == str(n):
    os.kill(os.getpid(), signal.SIGKILL)
if n == 1 and os.environ.get("FAKE_GITLEAKS_MOVE"):
    import subprocess
    gitdir, ref, sha = os.environ["FAKE_GITLEAKS_MOVE"].split(" ")
    subprocess.run(["git", "--git-dir", gitdir, "update-ref", ref, sha], check=True)
root, report = args[1], args[args.index("-r") + 1]
# gitleaks runs in the blob dir: a config path relative to the repo would be lost.
assert os.path.isfile(args[args.index("--config") + 1]), args
findings = []
scanned = 0
with open(log, "a") as out:
    for name in sorted(os.listdir(root)):
        path = os.path.join(root, name)
        out.write(name + "\\n")
        body = open(path, "rb").read()
        scanned += len(body)
        if b"FAKESECRET" in body:
            member = os.environ.get("FAKE_GITLEAKS_MEMBER")
            where = f"{{path}}!{{member}}" if member else path
            findings.append({{"RuleID": "fake-rule", "File": where, "Match": "REDACTED"}})
said = os.environ.get("FAKE_GITLEAKS_SCANNED")
if said != "none":
    print(f"INF scanned ~{{scanned - (said == 'short')}} bytes", file=sys.stderr)
mode = os.environ.get("FAKE_GITLEAKS_REPORT", "write")
if mode == "write":
    json.dump(findings, open(report, "w"))
elif mode == "garbage":
    open(report, "w").write("{{not json")
sys.exit(int(os.environ.get("FAKE_GITLEAKS_RC", 1 if findings else 0)))
"""

SCAN_TOOLS = (*REAL_TOOLS, "git")


def _git_out(repo: Path, *args: str, stdin: bytes | None = None) -> str:
    return subprocess.run(
        ["git", "-C", str(repo), *args],
        input=stdin,
        check=True,
        capture_output=True,
        env=dict(os.environ, **GIT_ENV),
    ).stdout.decode()


def _checkpoints(repo: Path, trees: dict[str, dict[str, bytes]]) -> dict[str, str]:
    """One root commit per ref holding `files`, all through one `git fast-import`."""
    stream = bytearray()
    for mark, (ref, files) in enumerate(trees.items(), 1):
        stream += (
            f"reset {ref}\ncommit {ref}\nmark :{mark}\n"
            "committer t <t@example.com> 0 +0000\ndata 10\ncheckpoint\n"
        ).encode()
        for path, body in files.items():
            stream += f"M 100644 inline {path}\ndata {len(body)}\n".encode() + body + b"\n"
        stream += b"\n"
    marks = repo / ".git" / "fast-import.marks"
    _git_out(repo, "fast-import", "--quiet", f"--export-marks={marks}", stdin=bytes(stream))
    sha = dict(line.split(" ") for line in marks.read_text().splitlines())
    return {ref: sha[f":{mark}"] for mark, ref in enumerate(trees, 1)}


def _bulk_checkpoints(repo: Path, start: int, n: int) -> dict[str, str]:
    """Checkpoints `start..start+n-1` the way entire grows them: `full.jsonl` of
    checkpoint i holds lines 0..i%10 of session i//10, and every checkpoint shares
    one `metadata.json`."""
    trees = {}
    for i in range(start, start + n):
        session, last = divmod(i, 10)
        transcript = "".join(
            json.dumps({"session": session, "line": j, "text": f"turn {j}"}) + "\n"
            for j in range(last + 1)
        )
        trees[f"refs/entire/checkpoints/{i % 100:02d}/ck{i:04d}"] = {
            "0/full.jsonl": transcript.encode(),
            "0/metadata.json": b'{"agent": "Claude Code"}\n',
        }
    return _checkpoints(repo, trees)


def _tree_blobs(repo: Path, shas) -> set[str]:
    blobs = set()
    for sha in shas:
        for line in _git_out(repo, "ls-tree", "-r", sha).splitlines():
            meta, _, _ = line.partition("\t")
            _, kind, oid = meta.split(" ")
            if kind == "blob":
                blobs.add(oid)
    return blobs


class ScanFx:
    """A repo with a bare checkpoint remote `ckpt`, a fake gitleaks first on PATH,
    an own host.toml and an own gitleaks config."""

    def __init__(self, tmp_path: Path, gitleaks: str = FAKE_GITLEAKS):
        self.repo = tmp_path / "repo"
        init_repo(self.repo)
        ckpt = tmp_path / "ckpt.git"
        subprocess.run(["git", "init", "-q", "--bare", str(ckpt)], check=True)
        git(self.repo, "remote", "add", "ckpt", str(ckpt))
        self.log = tmp_path / "gitleaks.log"
        self.config = tmp_path / "gitleaks.toml"
        self.config.write_text('title = "test"\n', encoding="utf-8")
        stubs = dict(BOUNDED, gitleaks=gitleaks) if gitleaks else BOUNDED
        self.env = dict(
            os.environ,
            PATH=str(tool_bin(tmp_path / "bin", SCAN_TOOLS, stubs=stubs)),
            LIGHTHOUSE_HOST_TOML=str(_host(tmp_path)),
            FAKE_GITLEAKS_LOG=str(self.log),
        )

    def scan(self, refs: dict[str, str], *flags: str, **env: str):
        argv = [f"{ref}={sha}" for ref, sha in refs.items()]
        command = [
            sys.executable, "-I", str(MODULE), "scan",
            "--remote", "ckpt", "--config", str(self.config), *flags, *argv,
        ]  # fmt: skip
        return subprocess.run(
            command,
            capture_output=True,
            text=True,
            env=dict(self.env, **env),
            cwd=self.repo,
            timeout=120,
            check=False,
        )

    def logged(self) -> list[str]:
        """The files gitleaks got since the last call, then a fresh log."""
        lines = self.log.read_text().splitlines() if self.log.exists() else []
        self.log.unlink(missing_ok=True)
        Path(f"{self.log}.calls").unlink(missing_ok=True)
        return lines

    def calls(self) -> int:
        calls = Path(f"{self.log}.calls")
        return int(calls.read_text()) if calls.exists() else 0

    def ledger(self) -> set[str]:
        files = list((self.repo / ".git" / "lighthouse" / "secret-scan").glob("clean-*.txt"))
        assert len(files) <= 1, files
        return set(files[0].read_text().split()) if files else set()


@pytest.fixture
def sx(tmp_path) -> ScanFx:
    return ScanFx(tmp_path)


def _blob_id(body: bytes) -> str:
    return hashlib.sha1(b"blob %d\0" % len(body) + body).hexdigest()


def test_second_scan_touches_only_new_blobs(sx):
    started = time.monotonic()
    old = _bulk_checkpoints(sx.repo, 0, 500)

    first = sx.scan(old)

    assert first.returncode == 0, first.stderr
    logged = sx.logged()
    assert sorted(logged) == sorted(_tree_blobs(sx.repo, old.values()))

    new = _bulk_checkpoints(sx.repo, 500, 5)
    second = sx.scan({**old, **new})

    assert second.returncode == 0, second.stderr
    fresh = _tree_blobs(sx.repo, new.values()) - _tree_blobs(sx.repo, old.values())
    assert len(fresh) == 5  # the shared metadata.json is not among them
    assert sorted(sx.logged()) == sorted(fresh)
    assert time.monotonic() - started < 20


def test_shared_blob_scanned_once(sx):
    body = b'{"text": "FAKESECRET"}\n'
    refs = _checkpoints(
        sx.repo,
        {
            "refs/entire/checkpoints/aa/one": {"0/full.jsonl": body},
            "refs/entire/checkpoints/bb/two": {"1/full.jsonl": body},
        },
    )

    result = sx.scan(refs)

    assert result.returncode == 1, result.stderr
    assert sx.logged() == [_blob_id(body)]
    # One hit in a shared blob makes every ref that holds it dirty.
    assert "refs/entire/checkpoints/aa/one  0/full.jsonl  fake-rule  REDACTED" in result.stdout
    assert "refs/entire/checkpoints/bb/two  1/full.jsonl  fake-rule  REDACTED" in result.stdout


@pytest.mark.parametrize("change", ["config", "gitleaks-version"])
def test_config_change_rescans_everything(sx, change):
    refs = _bulk_checkpoints(sx.repo, 0, 20)
    assert sx.scan(refs).returncode == 0
    everything = sorted(sx.logged())
    assert sx.scan(refs).returncode == 0
    assert sx.logged() == []  # unchanged: all in the ledger

    env = {}
    if change == "config":
        sx.config.write_text('title = "test"\n# an allowlist more\n', encoding="utf-8")
    else:
        env["FAKE_GITLEAKS_VERSION"] = "fake 2"
    result = sx.scan(refs, **env)

    assert result.returncode == 0, result.stderr
    assert sorted(sx.logged()) == everything


def _three_batches(sx) -> dict[str, str]:
    """Three checkpoints of one 1000-byte blob each; with a 1500-byte batch, three batches."""
    sx.env["LIGHTHOUSE_SCAN_BATCH_BYTES"] = "1500"
    return _checkpoints(
        sx.repo,
        {f"refs/entire/checkpoints/0{i}/ck{i}": {"0/full.jsonl": bytes([65 + i]) * 1000} for i in range(3)},
    )


def test_kill_mid_run_keeps_earlier_batches(sx):
    refs = _three_batches(sx)

    result = sx.scan(refs, FAKE_GITLEAKS_KILL_AT="2")

    assert result.returncode == 2
    assert "secret-scan: batch 1/3, " in result.stderr
    assert (
        "secret-scan: ABORTED at memory ceiling (2G) in batch 2; "
        "raise host.scan_memory_max or lower the batch size"
    ) in result.stderr
    assert sx.calls() == 2  # batch 3 never ran
    batch_one = sx.logged()
    assert len(batch_one) == 1
    assert sx.ledger() == set(batch_one)


@pytest.mark.parametrize(
    ("report", "rc"),
    [("none", "0"), ("none", "1"), ("garbage", "0"), ("garbage", "1")],
)
def test_missing_report_is_not_clean(sx, report, rc):
    refs = _bulk_checkpoints(sx.repo, 0, 3)

    result = sx.scan(refs, FAKE_GITLEAKS_REPORT=report, FAKE_GITLEAKS_RC=rc)

    assert result.returncode == 2
    assert "secret-scan: COULD NOT CHECK — " in result.stderr
    assert sx.logged()  # gitleaks ran
    assert sx.ledger() == set()


def test_rc1_with_empty_report_is_not_clean(sx):
    """rc 1 is findings only with findings in the report; gitleaks also exits 1 on an error."""
    refs = _bulk_checkpoints(sx.repo, 0, 3)

    result = sx.scan(refs, FAKE_GITLEAKS_RC="1")

    assert result.returncode == 2
    assert "COULD NOT CHECK — " in result.stderr
    assert sx.ledger() == set()


def test_findings_cap_blocks_scan(sx):
    refs = _checkpoints(
        sx.repo,
        {f"refs/entire/checkpoints/cap/ck{i:03d}": {"0/full.jsonl": f"FAKESECRET {i}\n".encode()} for i in range(201)},
    )

    result = sx.scan(refs)

    assert result.returncode == 1, result.stderr
    assert sx.calls() == 1  # one batch
    assert (
        "secret-scan: 201 findings for rule fake-rule in one batch; "
        "an allowlist is probably missing for this pattern (sample: REDACTED)"
    ) in result.stderr


def _cap_then_clean(sx) -> dict[str, str]:
    """201 dirty one-line blobs (batch 1, over the cap), then two clean 3000-byte
    blobs that each get a batch of their own with a 4000-byte batch."""
    sx.env["LIGHTHOUSE_SCAN_BATCH_BYTES"] = "4000"
    trees = {f"refs/entire/checkpoints/cap/ck{i:03d}": {"0/full.jsonl": f"FAKESECRET {i}\n".encode()} for i in range(201)}
    trees.update({f"refs/entire/checkpoints/ok/ck{i}": {"0/full.jsonl": bytes([65 + i]) * 3000} for i in range(2)})
    return _checkpoints(sx.repo, trees)


def test_cap_stops_later_batches(sx):
    refs = _cap_then_clean(sx)

    result = sx.scan(refs)

    assert result.returncode == 1, result.stderr
    assert "allowlist is probably missing" in result.stderr
    assert sx.calls() == 1  # batches 2 and 3 never ran


def test_keep_going_scans_past_the_cap(sx):
    refs = _cap_then_clean(sx)

    result = sx.scan(refs, "--keep-going")

    assert result.returncode == 1, result.stderr
    assert "allowlist is probably missing" in result.stderr
    assert sx.calls() == 3


def test_other_gitleaks_rc_with_a_report_is_not_clean(sx):
    """A readable `[]` report does not make rc 2 clean."""
    refs = _bulk_checkpoints(sx.repo, 0, 3)

    result = sx.scan(refs, FAKE_GITLEAKS_RC="2")

    assert result.returncode == 2
    assert "secret-scan: COULD NOT CHECK — gitleaks exited 2" in result.stderr
    assert sx.logged()  # gitleaks ran and wrote its report
    assert sx.ledger() == set()


@pytest.mark.parametrize("said", ["none", "short"])
def test_skipped_bytes_are_not_clean(sx, said):
    """gitleaks scanned fewer bytes than the batch holds, or did not say: it skipped
    a blob, and no blob of the batch goes into the ledger."""
    refs = _bulk_checkpoints(sx.repo, 0, 3)

    result = sx.scan(refs, FAKE_GITLEAKS_SCANNED=said)

    assert result.returncode == 2
    assert "secret-scan: COULD NOT CHECK — gitleaks " in result.stderr
    assert "in batch 1" in result.stderr
    assert sx.ledger() == set()


def test_ledger_key_covers_the_scan_method(cs, sx, monkeypatch):
    """A ledger written by another way of scanning (before the blob prefix and the
    byte count) is not trusted: the key changes with the scan method."""
    monkeypatch.setenv("PATH", sx.env["PATH"])
    before = cs.ledger_path(sx.repo, sx.config)

    monkeypatch.setattr(cs, "SCAN_METHOD", cs.SCAN_METHOD + b"+")

    assert cs.ledger_path(sx.repo, sx.config) != before


def test_uncaught_error_is_could_not_check(tmp_path):
    """A module without `.lighthouse/lib` next to it fails on the import of config.py:
    exit 2 (could not check), never 1, which the hook reads as findings."""
    lonely = tmp_path / "tools" / "hooks" / "checkpoint_scan.py"
    lonely.parent.mkdir(parents=True)
    shutil.copy(MODULE, lonely)
    sx = ScanFx(tmp_path)
    refs = _bulk_checkpoints(sx.repo, 0, 1)
    argv = [f"{ref}={sha}" for ref, sha in refs.items()]

    result = subprocess.run(
        [sys.executable, "-I", str(lonely), "scan", "--remote", "ckpt", "--config", str(sx.config), *argv],
        capture_output=True,
        text=True,
        env=sx.env,
        cwd=sx.repo,
        timeout=60,
        check=False,
    )

    assert result.returncode == 2, result.stderr
    assert "secret-scan: COULD NOT CHECK — ModuleNotFoundError: " in result.stderr
    assert "Traceback" not in result.stderr


def test_under_the_cap_says_nothing(sx):
    refs = _checkpoints(
        sx.repo,
        {f"refs/entire/checkpoints/cap/ck{i:03d}": {"0/full.jsonl": f"FAKESECRET {i}\n".encode()} for i in range(200)},
    )

    result = sx.scan(refs)

    assert result.returncode == 1, result.stderr
    assert "allowlist is probably missing" not in result.stderr


def test_volume_limit_refuses(sx):
    refs = _bulk_checkpoints(sx.repo, 0, 20)
    limit = {"LIGHTHOUSE_SCAN_VOLUME_LIMIT": "1000"}

    refused = sx.scan(refs, **limit)

    assert refused.returncode == 2
    assert "pre-push: BLOCKED — " in refused.stderr
    assert "checkpoint blobs (" in refused.stderr
    assert "are not yet scanned" in refused.stderr
    assert "run: python3 tools/hooks/checkpoint_scan.py push ckpt" in refused.stderr
    assert sx.calls() == 0

    # --full is the explicit heavy scan; after it the ledger holds everything.
    assert sx.scan(refs, "--full", **limit).returncode == 0
    assert sx.logged()
    again = sx.scan(refs, **limit)
    assert again.returncode == 0, again.stderr
    assert sx.logged() == []


def test_relative_config_is_found(sx):
    """`--config` relative to the repo still reaches gitleaks, which runs elsewhere."""
    refs = _bulk_checkpoints(sx.repo, 0, 2)

    result = sx.scan(refs, "--config", os.path.relpath(sx.config, sx.repo))

    assert result.returncode == 0, result.stderr
    assert sx.ledger() == _tree_blobs(sx.repo, refs.values())


def test_full_ignores_the_ledger(sx):
    refs = _bulk_checkpoints(sx.repo, 0, 10)
    assert sx.scan(refs).returncode == 0
    everything = sorted(sx.logged())

    result = sx.scan(refs, "--full")

    assert result.returncode == 0, result.stderr
    assert sorted(sx.logged()) == everything
    assert sx.ledger() == set(everything)


def test_archive_member_maps_to_its_blob(sx):
    """A finding in an archive member, `<blob id>!<inner path>`, is a finding of the
    blob: the `!` part may hold a `/`."""
    dirty = b'{"text": "FAKESECRET"}\n'
    refs = _checkpoints(sx.repo, {"refs/entire/checkpoints/01/dirty": {"0/full.jsonl": dirty}})

    result = sx.scan(refs, FAKE_GITLEAKS_MEMBER="inner/dir/file.txt")

    assert result.returncode == 1, result.stderr
    assert "refs/entire/checkpoints/01/dirty  0/full.jsonl  fake-rule  REDACTED" in result.stdout
    assert sx.ledger() == set()


def test_dirty_blob_not_in_ledger(sx):
    dirty = b'{"text": "FAKESECRET"}\n'
    refs = _checkpoints(
        sx.repo,
        {
            "refs/entire/checkpoints/01/clean": {"0/full.jsonl": b"clean one\n", "0/metadata.json": b"{}\n"},
            "refs/entire/checkpoints/02/dirty": {"0/full.jsonl": dirty, "0/metadata.json": b"{}\n"},
        },
    )

    result = sx.scan(refs)

    assert result.returncode == 1, result.stderr
    assert sx.calls() == 1  # one batch
    assert "refs/entire/checkpoints/02/dirty  0/full.jsonl  fake-rule  REDACTED" in result.stdout
    assert "refs/entire/checkpoints/01/clean" not in result.stdout
    assert sx.ledger() == {_blob_id(b"clean one\n"), _blob_id(b"{}\n")}


def test_commits_on_the_remote_are_not_scanned(sx):
    old = _bulk_checkpoints(sx.repo, 0, 3)
    # As if a fetch from `ckpt` had brought them: remote-tracking refs.
    for ref, sha in old.items():
        git(sx.repo, "update-ref", f"refs/remotes/ckpt/{ref[5:]}", sha)
    new = _bulk_checkpoints(sx.repo, 3, 1)

    result = sx.scan({**old, **new})

    assert result.returncode == 0, result.stderr
    # `--not --remotes=` drops whole commits. Whether a blob the new root commit
    # shares with an old one (metadata.json) is excluded is up to git's tree walk;
    # the ledger covers that, not rev-list. So only the two sides are pinned.
    logged = set(sx.logged())
    old_blobs, new_blobs = _tree_blobs(sx.repo, old.values()), _tree_blobs(sx.repo, new.values())
    assert not logged & (old_blobs - new_blobs)
    assert new_blobs - old_blobs <= logged


def test_keep_going_reports_peak_rss(sx):
    refs = _three_batches(sx)

    result = sx.scan(refs, "--keep-going")

    assert result.returncode == 0, result.stderr
    for i in (1, 2, 3):
        assert re.search(rf"secret-scan: batch {i}/3 peak RSS (\d+|unknown) MB", result.stderr)


def test_batches_by_size(cs):
    blobs = {name: cs.Blob(size, set()) for name, size in [("a", 40), ("b", 50), ("c", 10), ("d", 200)]}

    assert cs.batches(blobs, limit=100) == [["c", "a", "b"], ["d"]]
    assert cs.batches(blobs, limit=60) == [["c", "a"], ["b"], ["d"]]


def test_ledger_written_sorted_and_merged(cs, tmp_path):
    path = tmp_path / "sub" / "clean-x.txt"

    cs.add_to_ledger(path, ["b", "a"])
    cs.add_to_ledger(path, ["c", "a"])

    assert path.read_text() == "a\nb\nc\n"
    assert cs.load_ledger(path) == {"a", "b", "c"}
    assert list(path.parent.iterdir()) == [path]  # no temp file left behind


def _real_sx(tmp_path: Path) -> ScanFx:
    """ScanFx with the real gitleaks and the repository's own `.gitleaks.toml`."""
    sx = ScanFx(tmp_path, gitleaks="")
    sx.env["PATH"] = os.environ["PATH"]
    sx.config.write_text((ROOT / ".gitleaks.toml").read_text(encoding="utf-8"), encoding="utf-8")
    return sx


LEAK = f'{{"text": "GITHUB_PAT={FAKE_PAT}"}}\n'.encode()


@needs_gitleaks
def test_real_gitleaks_ignores_the_temp_path(tmp_path):
    """gitleaks' default path allowlist (node_modules, vendor, ...) applies to the path
    it reports: a TMPDIR under such a directory must not make every blob clean."""
    sx = _real_sx(tmp_path)
    tmpdir = tmp_path / "node_modules"
    tmpdir.mkdir()
    refs = _checkpoints(sx.repo, {"refs/entire/checkpoints/01/leak": {"0/full.jsonl": LEAK}})

    result = sx.scan(refs, TMPDIR=str(tmpdir))

    assert result.returncode == 1, result.stderr
    assert "refs/entire/checkpoints/01/leak  0/full.jsonl  github-pat  REDACTED" in result.stdout
    assert sx.ledger() == set()


@needs_gitleaks
def test_real_gitleaks_finds_pat(tmp_path):
    sx = _real_sx(tmp_path)
    refs = _checkpoints(
        sx.repo,
        {
            "refs/entire/checkpoints/01/leak": {"0/full.jsonl": f'{{"text": "GITHUB_PAT={FAKE_PAT}"}}\n'.encode()},
            "refs/entire/checkpoints/02/clean": {"0/full.jsonl": b'{"text": "hello"}\n'},
        },
    )

    result = sx.scan(refs)

    assert result.returncode == 1, result.stderr
    assert "refs/entire/checkpoints/01/leak  0/full.jsonl  github-pat  REDACTED" in result.stdout
    assert FAKE_PAT not in result.stdout + result.stderr
    assert sx.ledger() == {_blob_id(b'{"text": "hello"}\n')}


# Text to git (no NUL in the first 8000 bytes), so `gitleaks git` scans it; but the
# first bytes are the magic number of a binary type, and `gitleaks dir` 8.30.1 skips
# such a file unscanned (rc 0, `[]`). Measured for each of these.
MAGIC = {
    "zip": b"PK\x03\x04",
    "gzip": b"\x1f\x8b\x08",
    "sqlite": b"SQLite format 3",
    "elf": b"\x7fELF",
    "rtf": b"{\\rtf1",
    "pdf": b"%PDF-",
    "ole": b"\xd0\xcf\x11\xe0\xa1\xb1\x1a\xe1",
    "bz2": b"BZh",
    "exe": b"MZ",
}


@needs_gitleaks
@pytest.mark.parametrize("magic", MAGIC.values(), ids=MAGIC.keys())
def test_real_gitleaks_scans_text_with_a_binary_magic(tmp_path, magic):
    sx = _real_sx(tmp_path)
    refs = _checkpoints(sx.repo, {"refs/entire/checkpoints/01/leak": {"0/full.jsonl": magic + LEAK}})

    result = sx.scan(refs)

    assert result.returncode == 1, result.stderr
    assert "refs/entire/checkpoints/01/leak  0/full.jsonl  github-pat  REDACTED" in result.stdout
    assert sx.ledger() == set()


@needs_gitleaks
def test_real_gitleaks_skip_past_the_prefix_is_not_clean(tmp_path):
    """The residual class: a binary signature at a fixed offset past the first byte
    (`ustar` at 257, tar). The blob prefix shifts it by one, so `ustar` at 256 in the
    blob still makes gitleaks skip the file; the byte count catches that."""
    sx = _real_sx(tmp_path)
    body = b"x" * 256 + b"ustar" + LEAK
    refs = _checkpoints(
        sx.repo,
        {
            "refs/entire/checkpoints/01/tar": {"0/full.jsonl": body},
            "refs/entire/checkpoints/02/clean": {"0/full.jsonl": b'{"text": "hello"}\n'},
        },
    )

    result = sx.scan(refs)

    assert result.returncode == 2, result.stderr
    assert f"it skipped {_blob_id(body)}" in result.stderr
    assert sx.ledger() == set()
    assert FAKE_PAT not in result.stdout + result.stderr


# --- push: scan what the checkpoint remote misses, push the clean refs -----------

HOOK = ROOT / "tools" / "hooks" / "pre-push-secret-scan.sh"
# The hook needs these besides SCAN_TOOLS.
PUSH_TOOLS = ("bash", "dirname")


class PushFx(ScanFx):
    """ScanFx plus what `push` needs: a bare `origin` next to `ckpt`, entire set up
    the way lighthouse is (no automatic push, `ckpt` as the checkpoint push remote)
    and the pre-push scan installed as the repo's hook. The config is the repo's own
    `.gitleaks.toml`, the one the hook uses, so `push` and the hook share a ledger."""

    def __init__(self, tmp_path: Path, gitleaks: str = FAKE_GITLEAKS):
        super().__init__(tmp_path, gitleaks)
        tool_bin(tmp_path / "bin", PUSH_TOOLS)
        self.ckpt = tmp_path / "ckpt.git"
        self.origin = tmp_path / "origin.git"
        subprocess.run(["git", "init", "-q", "--bare", str(self.origin)], check=True)
        git(self.repo, "remote", "add", "origin", str(self.origin))
        self.config = ROOT / ".gitleaks.toml"
        entire = self.repo / ".entire"
        entire.mkdir()
        (entire / "settings.json").write_text(
            json.dumps({"enabled": True, "strategy_options": {"push_sessions": False}}), encoding="utf-8"
        )
        (entire / "settings.local.json").write_text(
            json.dumps({"strategy_options": {"checkpoint_push_remote": "ckpt"}}), encoding="utf-8"
        )
        hook = self.repo / ".git" / "hooks" / "pre-push"
        hook.write_text(f'#!/bin/sh\nexec bash "{HOOK}" "$@"\n', encoding="utf-8")
        hook.chmod(0o755)
        self.held = self.repo / ".git" / "lighthouse" / "secret-scan" / "held.txt"

    def push(self, *args: str, **env: str):
        return subprocess.run(
            [sys.executable, "-I", str(MODULE), "push", *args],
            capture_output=True,
            text=True,
            env=dict(self.env, **env),
            cwd=self.repo,
            timeout=300,
            check=False,
        )

    def pushed(self, bare: Path | None = None) -> set[str]:
        """The refs/entire/ refs on `bare` (default: ckpt)."""
        out = _git_out(bare or self.ckpt, "for-each-ref", "--format=%(refname)", "refs/entire/")
        return set(out.split())


@pytest.fixture
def px(tmp_path) -> PushFx:
    return PushFx(tmp_path)


@needs_gitleaks
def test_push_sends_clean_and_holds_dirty(tmp_path):
    px = PushFx(tmp_path, gitleaks="")
    px.env["PATH"] = os.environ["PATH"]
    _checkpoints(
        px.repo,
        {
            "refs/entire/checkpoints/01/one": {"0/full.jsonl": b'{"text": "hello"}\n'},
            "refs/entire/checkpoints/02/two": {"0/full.jsonl": b'{"text": "goodbye"}\n'},
            "refs/entire/checkpoints/03/leak": {"0/full.jsonl": f'{{"text": "GITHUB_PAT={FAKE_PAT}"}}\n'.encode()},
        },
    )

    result = px.push("ckpt")

    assert result.returncode == 1, result.stderr
    assert px.pushed() == {"refs/entire/checkpoints/01/one", "refs/entire/checkpoints/02/two"}
    held = px.held.read_text(encoding="utf-8")
    assert "refs/entire/checkpoints/03/leak\n  0/full.jsonl  github-pat  REDACTED\n" in held
    assert "checkpoints/01/one" not in held and "checkpoints/02/two" not in held
    assert result.stdout.endswith(f"2 pushed, 1 held — see {px.held}\n")
    assert FAKE_PAT not in result.stdout + result.stderr + held


def test_shared_dirty_blob_holds_every_ref(px):
    """One dirty blob in a ref that looks clean otherwise: that ref is held too."""
    dirty = b'{"text": "FAKESECRET"}\n'
    _checkpoints(
        px.repo,
        {
            "refs/entire/checkpoints/01/dirty": {"0/full.jsonl": dirty},
            "refs/entire/checkpoints/02/shares": {"0/full.jsonl": b"clean\n", "1/full.jsonl": dirty},
            "refs/entire/checkpoints/03/clean": {"0/full.jsonl": b"clean\n"},
        },
    )

    result = px.push("ckpt")

    assert result.returncode == 1, result.stderr
    assert px.pushed() == {"refs/entire/checkpoints/03/clean"}
    held = px.held.read_text(encoding="utf-8")
    assert "refs/entire/checkpoints/01/dirty\n  0/full.jsonl  fake-rule  REDACTED\n" in held
    assert "refs/entire/checkpoints/02/shares\n  1/full.jsonl  fake-rule  REDACTED\n" in held
    assert px.logged().count(_blob_id(dirty)) == 1  # scanned once
    assert result.stdout.endswith(f"1 pushed, 2 held — see {px.held}\n")


def test_push_refuses_non_checkpoint_remote(px):
    _bulk_checkpoints(px.repo, 0, 3)

    result = px.push("origin")

    assert result.returncode == 2
    assert "is not the checkpoint remote" in result.stderr
    assert px.pushed(px.origin) == set()
    assert px.calls() == 0  # refused before any scan


def test_push_refuses_while_entire_pushes_by_itself(px):
    """entire would push past the hook with --no-verify: push refuses before a scan."""
    (px.repo / ".entire" / "settings.json").write_text(
        json.dumps({"enabled": True, "strategy_options": {"push_sessions": True}}), encoding="utf-8"
    )
    _bulk_checkpoints(px.repo, 0, 3)

    result = px.push("ckpt")

    assert result.returncode == 2
    assert "entire may push checkpoints by itself" in result.stderr
    assert px.pushed() == set()
    assert px.calls() == 0


def test_push_by_url(px):
    """A URL, not a remote name: git hands the hook that URL as both name and URL. The
    URL rule knows only github and gitlab hosts, so a local path is the checkpoint
    remote here through `checkpoint_push_remote` naming that path."""
    url = str(px.ckpt)
    (px.repo / ".entire" / "settings.local.json").write_text(
        json.dumps({"strategy_options": {"checkpoint_push_remote": url}}), encoding="utf-8"
    )
    refs = _bulk_checkpoints(px.repo, 0, 3)

    result = px.push(url)

    assert result.returncode == 0, result.stderr
    assert px.pushed() == set(refs)
    # The remote `ckpt` (same repo, but another name) is no longer the checkpoint remote.
    assert px.push("ckpt").returncode == 2


def test_push_lists_refs_on_the_push_url(px, tmp_path):
    """With a separate pushurl, what the remote misses is read from the push URL, the
    repo the rule checked and the push goes to."""
    fetch = tmp_path / "fetch.git"
    subprocess.run(["git", "init", "-q", "--bare", str(fetch)], check=True)
    git(px.repo, "remote", "set-url", "ckpt", str(fetch))
    git(px.repo, "remote", "set-url", "--push", "ckpt", str(px.ckpt))
    refs = _bulk_checkpoints(px.repo, 0, 2)
    # The fetch repo already holds both refs; the push repo holds none.
    git(px.repo, "push", "-q", "--no-verify", str(fetch), *(f"{r}:{r}" for r in refs))

    result = px.push("ckpt")

    assert result.returncode == 0, result.stderr
    assert px.pushed() == set(refs)


def _push_trace(trace: Path) -> list[list[str]]:
    """The refspecs of every `git push` in a GIT_TRACE file, one list per call."""
    pushes = []
    for line in trace.read_text().splitlines():
        _, hit, argv = line.partition("built-in: git push ")
        if hit:
            pushes.append([a for a in argv.split() if ":refs/" in a])
    return pushes


def test_push_chunks_refspecs(px, tmp_path):
    refs = _bulk_checkpoints(px.repo, 0, 1200)
    trace = tmp_path / "trace"

    result = px.push("ckpt", GIT_TRACE=str(trace))

    assert result.returncode == 0, result.stderr
    assert px.pushed() == set(refs)
    pushes = _push_trace(trace)
    assert len(pushes) >= 3
    assert all(0 < len(spec) <= 500 for spec in pushes), [len(s) for s in pushes]
    assert sorted(s for spec in pushes for s in spec) == sorted(f"{sha}:{r}" for r, sha in refs.items())
    # One gitleaks run, push's own, each blob once: the hook found it all in the ledger.
    assert px.calls() == 1
    logged = px.logged()
    assert sorted(logged) == sorted(_tree_blobs(px.repo, refs.values()))
    assert result.stdout.endswith(f"1200 pushed, 0 held — see {px.held}\n")
    assert "peak RSS" not in result.stderr


def test_push_sends_the_scanned_commit(px):
    """A ref that moves after the scan read it: push sends the commit it scanned,
    not what the ref holds by the time of the push."""
    refs = _bulk_checkpoints(px.repo, 0, 2)
    first, other = sorted(refs)
    moved = f"{px.repo / '.git'} {first} {refs[other]}"

    result = px.push("ckpt", FAKE_GITLEAKS_MOVE=moved)

    assert result.returncode == 0, result.stderr
    assert _git_out(px.repo, "rev-parse", first).strip() == refs[other]  # it moved
    assert _git_out(px.ckpt, "rev-parse", first).strip() == refs[first]  # scanned sha sent


def test_push_full_writes_ledger(px):
    old = _bulk_checkpoints(px.repo, 0, 5)
    assert px.scan(old).returncode == 0
    # drain the log of the earlier scan
    px.logged()
    new = _bulk_checkpoints(px.repo, 5, 5)
    everything = _tree_blobs(px.repo, [*old.values(), *new.values()])

    result = px.push("ckpt", "--full")

    assert result.returncode == 0, result.stderr
    assert sorted(px.logged()) == sorted(everything)  # the ledger was not read
    assert px.ledger() == everything
    assert px.pushed() == set(old) | set(new)


def test_cap_in_push_holds_and_continues(px):
    refs = _cap_then_clean(px)

    result = px.push("ckpt")

    assert result.returncode == 1, result.stderr
    cap = (
        "secret-scan: 201 findings for rule fake-rule in one batch; "
        "an allowlist is probably missing for this pattern (sample: REDACTED)"
    )
    # Past the cap: push's own scan ran batches 2 and 3 (the hook prints nothing on a pass).
    assert "secret-scan: batch 3/3, " in result.stderr
    clean = {r for r in refs if "/ok/" in r}
    assert px.pushed() == clean
    assert result.stdout.startswith(cap + "\n")
    assert result.stdout.endswith(f"2 pushed, 201 held — see {px.held}\n")
    held = px.held.read_text(encoding="utf-8")
    assert held.startswith(cap + "\n")
    assert held.count("\nrefs/entire/checkpoints/cap/") == 201
    assert _tree_blobs(px.repo, [refs[r] for r in clean]) <= px.ledger()


def test_push_aborted_pushes_nothing(px):
    refs = _three_batches(px)

    result = px.push("ckpt", FAKE_GITLEAKS_KILL_AT="2")

    assert result.returncode == 2
    assert "ABORTED at memory ceiling (2G) in batch 2" in result.stderr
    assert px.pushed() == set()
    batch_one = px.logged()
    assert len(batch_one) == 1
    assert px.ledger() == set(batch_one)

    # A rerun goes on where the ledger left off.
    again = px.push("ckpt")

    assert again.returncode == 0, again.stderr
    assert len(px.logged()) == 2
    assert px.pushed() == set(refs)


def test_push_could_not_check_pushes_nothing(px):
    _bulk_checkpoints(px.repo, 0, 3)

    result = px.push("ckpt", FAKE_GITLEAKS_REPORT="none")

    assert result.returncode == 2
    assert "secret-scan: COULD NOT CHECK — " in result.stderr
    assert px.pushed() == set()


def test_failed_chunk_stops_and_counts(cs, px, monkeypatch, capsys):
    """The remote refuses one ref of the second of three chunks: nothing of that chunk
    lands (an atomic push), the third is not tried, and the count says so."""
    refs = sorted(_bulk_checkpoints(px.repo, 0, 6))
    count = px.ckpt / "receives"
    hooks = px.ckpt / "hooks"
    # Shell builtins only: PATH holds no coreutils.
    (hooks / "pre-receive").write_text(f'#!/bin/sh\necho x >> "{count}"\n', encoding="utf-8")
    # Refuses the first ref of chunk 2; its second ref alone would be accepted.
    (hooks / "update").write_text(f'#!/bin/sh\n[ "$1" != "{refs[2]}" ]\n', encoding="utf-8")
    for hook in ("pre-receive", "update"):
        (hooks / hook).chmod(0o755)
    for key, value in px.env.items():
        monkeypatch.setenv(key, value)
    monkeypatch.chdir(px.repo)
    monkeypatch.setattr(cs, "PUSH_CHUNK", 2)

    rc = cs.main(["push", "ckpt"])

    err = capsys.readouterr().err
    assert rc == 2, err
    assert px.pushed() == set(refs[:2])
    assert count.read_text().count("x") == 2
    assert "git push exited 1 at refs 3-4; 2 of 6 clean refs pushed, the rest not" in err


def _settings_dir(tmp_path: Path, shared: dict | None, local: dict | None = None) -> Path:
    entire = tmp_path / ".entire"
    entire.mkdir()
    if shared is not None:
        (entire / "settings.json").write_text(json.dumps(shared), encoding="utf-8")
    if local is not None:
        (entire / "settings.local.json").write_text(json.dumps(local), encoding="utf-8")
    return tmp_path


GITHUB_CKPT = {"strategy_options": {"push_sessions": False, "checkpoint_remote": {"provider": "github", "repo": "Acme/ckpt"}}}


@pytest.mark.parametrize(
    ("remote", "url", "expected"),
    [
        ("c", "https://github.com/Acme/ckpt.git", True),
        ("c", "https://github.com/Acme/ckpt", True),
        ("c", "https://github.com/Acme/ckpt/", True),
        ("c", "git@github.com:Acme/ckpt.git", True),
        ("c", "git@github.com:Acme/ckpt", True),
        ("c", "ssh://git@github.com/Acme/ckpt.git", True),
        ("c", "https://github.com/Acme/widgets.git", False),
        ("c", "https://gitlab.com/Acme/ckpt.git", False),
        ("c", "/srv/ckpt.git", False),
    ],
)
def test_checkpoint_remote_by_url(cs, tmp_path, remote, url, expected):
    settings = cs.read_entire_settings(_settings_dir(tmp_path, GITHUB_CKPT))

    assert cs.is_checkpoint_remote(settings, remote, url) is expected


def test_checkpoint_remote_of_lighthouse(cs, tmp_path):
    """The real checkpoint repo: its URL passes, the public repo's does not."""
    shared = {
        "strategy_options": {
            "push_sessions": False,
            "checkpoint_remote": {"provider": "github", "repo": "CorneeldH/lighthouse-checkpoints"},
        }
    }
    settings = cs.read_entire_settings(_settings_dir(tmp_path, shared))
    url = "https://github.com/CorneeldH/lighthouse-checkpoints.git"

    assert cs.is_checkpoint_remote(settings, url, url)
    public = "https://github.com/CorneeldH/lighthouse.git"
    assert not cs.is_checkpoint_remote(settings, public, public)


def test_checkpoint_remote_by_gitlab_url(cs, tmp_path):
    shared = {"strategy_options": {"checkpoint_remote": {"provider": "gitlab", "repo": "Acme/ckpt"}}}
    settings = cs.read_entire_settings(_settings_dir(tmp_path, shared))

    assert cs.is_checkpoint_remote(settings, "c", "git@gitlab.com:Acme/ckpt.git")
    assert not cs.is_checkpoint_remote(settings, "c", "git@github.com:Acme/ckpt.git")


def test_checkpoint_remote_unknown_provider_never_matches(cs, tmp_path):
    shared = {"strategy_options": {"checkpoint_remote": {"provider": "forgejo", "repo": "Acme/ckpt"}}}
    settings = cs.read_entire_settings(_settings_dir(tmp_path, shared))

    assert not cs.is_checkpoint_remote(settings, "c", "https://forgejo.example/Acme/ckpt.git")


def test_checkpoint_remote_by_name_from_local_settings(cs, tmp_path):
    settings = cs.read_entire_settings(
        _settings_dir(tmp_path, GITHUB_CKPT, local={"strategy_options": {"checkpoint_push_remote": "ckpt"}})
    )

    assert cs.is_checkpoint_remote(settings, "ckpt", "/srv/anything.git")
    assert not cs.is_checkpoint_remote(settings, "origin", "/srv/anything.git")
    # The local file adds to the shared strategy options, it does not replace them.
    assert cs.is_checkpoint_remote(settings, "c", "https://github.com/Acme/ckpt.git")


@pytest.mark.parametrize(
    ("shared", "local", "self_push"),
    [
        (None, None, False),
        ({"strategy_options": {}}, None, True),
        ({"strategy_options": {"push_sessions": False}}, None, False),
        ({"enabled": False, "strategy_options": {}}, None, False),
        ({"strategy_options": {"push_sessions": False}}, {"strategy_options": {"push_sessions": True}}, True),
        ({"strategy_options": {}}, {"enabled": False}, False),
    ],
    ids=["no-settings", "default-on", "push-sessions-off", "disabled", "local-turns-on", "local-disables"],
)
def test_entire_self_push(cs, tmp_path, shared, local, self_push):
    settings = cs.read_entire_settings(_settings_dir(tmp_path, shared, local))

    assert settings.self_push is self_push
