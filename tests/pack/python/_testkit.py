"""Shared helpers for the tests of `pack/core`: the hook-test plumbing, plus a
copy of the installed layout so the hooks run from where `lighthouse init` puts
them.

Shared helpers for the hook tests (tasks 1 to 5 of #884/#886).

Taken over from `test_instructie_hooks.py` (the private `_run`, `_git`,
`_init_repo`, `_bash`, `_tool_bin` from before this task) so that the hook
tests (`test_pre_commit_gate.py`, `test_md_links.py`, `test_hook_deadline.py`)
use the same plumbing instead of
reimplementing it. `test_instructie_hooks.py` itself stays unchanged: this
file refactors no existing tests, it gives new tests the same building
blocks.
"""

import atexit
import functools
import http.server
import threading
import json
import os
import pwd
import re
import shlex
import shutil
import subprocess
import tempfile
import time
import types
from collections.abc import Iterable
from datetime import datetime, timedelta, timezone
from pathlib import Path

REPO = Path(__file__).resolve().parents[3]
PACKS = REPO / "pack"
CORE = PACKS / "core"

# The tree git hashes for an empty repository.
EMPTY_TREE = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

GIT_ENV = {
    "GIT_AUTHOR_NAME": "t",
    "GIT_AUTHOR_EMAIL": "t@example.com",
    "GIT_COMMITTER_NAME": "t",
    "GIT_COMMITTER_EMAIL": "t@example.com",
}


def run_hook(
    hook: Path,
    payload: dict,
    cwd: Path,
    env: dict | None = None,
    args: tuple[str, ...] = (),
) -> subprocess.CompletedProcess:
    return subprocess.run(
        [str(hook), *args],
        input=json.dumps(payload),
        capture_output=True,
        text=True,
        cwd=str(cwd),
        env=dict(os.environ, **(env or {})),
        check=False,
    )


def git(repo: Path, *args: str) -> None:
    subprocess.run(
        ["git", "-C", str(repo), *args],
        check=True,
        capture_output=True,
        text=True,
        env=dict(os.environ, **GIT_ENV),
    )


def init_repo(path: Path, branch: str = "main") -> None:
    """A real git repo with one commit, so the hook sees real plumbing."""
    path.mkdir(parents=True, exist_ok=True)
    git(path, "init", "-q", "-b", branch)
    (path / "README.md").write_text("# repo\n", encoding="utf-8")
    git(path, "add", "README.md")
    git(path, "commit", "-qm", "init")


def build_fixture_setup(root: Path, steps: list[dict]) -> None:
    """Build the directory tree of a shared-fixture `setup` under `root`.

    The steps are the ones of the git-invocations fixtures (`repo`,
    `dir`, `file`, `symlink`, `overwrite`), plus `worktree` (`{"worktree":
    "x/wt", "of": "x", "branch": "wt"}`: a linked worktree of repo `x`) and
    `origin` (`{"origin": "x", "url": ...}`: remote `origin` of repo `x`),
    `unborn` (`{"unborn": "x"}`: `git init -b main` without a commit),
    `bare` (`{"bare": "x"}`: `git init --bare`),
    `commit` (`{"commit": "x", "message": "m"}`: `add -A` and commit; the files
    come in before, through `file`) and `mv` (`{"mv": "x", "from": "a", "to": "b"}`).
    """
    for step in steps:
        if "repo" in step:
            init_repo(root / step["repo"], branch=step["branch"])
        elif "dir" in step:
            (root / step["dir"]).mkdir(parents=True, exist_ok=True)
        elif "file" in step:
            path = root / step["file"]
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(step["text"], encoding="utf-8")
        elif "symlink" in step:
            (root / step["symlink"]).symlink_to(root / step["to"])
        elif "overwrite" in step:
            (root / step["overwrite"]).write_text(step["text"], encoding="utf-8")
        elif "worktree" in step:
            git(
                root / step["of"],
                "worktree",
                "add",
                "-q",
                str(root / step["worktree"]),
                "-b",
                step["branch"],
            )
        elif "origin" in step:
            git(root / step["origin"], "remote", "add", "origin", step["url"])
        elif "unborn" in step:
            (root / step["unborn"]).mkdir(parents=True, exist_ok=True)
            git(root / step["unborn"], "init", "-q", "-b", "main")
        elif "bare" in step:
            (root / step["bare"]).mkdir(parents=True, exist_ok=True)
            git(root / step["bare"], "init", "-q", "--bare")
        elif "commit" in step:
            git(root / step["commit"], "add", "-A")
            git(root / step["commit"], "commit", "-qm", step["message"])
        elif "mv" in step:
            git(root / step["mv"], "mv", step["from"], step["to"])
        else:
            raise AssertionError(f"unknown setup step {step}")


def fake_gh_bin(tmp_path: Path, calls: list[dict]) -> tuple[Path, Path]:
    """A fake `gh` on a bin dir that replays `calls` in order.

    Returns `(bin_dir, log)`: put `bin_dir` first on PATH; `log` stays empty
    unless a call deviated (the fake then exits 97 and writes the expected and
    the actual argv to it), and the fake leaves a `count` file next to it with
    the number of calls made. A call has `argv`, `stdin`, `stdout`, `stderr`,
    `code` and optionally `timeout`.
    """
    bin_dir = tmp_path / "fakegh-bin"
    bin_dir.mkdir(parents=True, exist_ok=True)
    calls_file = tmp_path / "fakegh-calls.json"
    count = tmp_path / "fakegh-count"
    log = tmp_path / "fakegh-log"
    calls_file.write_text(json.dumps(calls), encoding="utf-8")
    count.write_text("0", encoding="utf-8")
    log.write_text("", encoding="utf-8")
    gh = bin_dir / "gh"
    gh.write_text(
        "#!/bin/sh\n"
        f"FAKE_CALLS={shlex.quote(str(calls_file))} "
        f"FAKE_COUNT={shlex.quote(str(count))} "
        f"FAKE_LOG={shlex.quote(str(log))} "
        f'exec python3 -I {shlex.quote(str(Path(__file__).with_name("_fakegh.py")))} "$@"\n',
        encoding="utf-8",
    )
    gh.chmod(0o755)
    return bin_dir, log


class ForgeReplay:
    """A forge API on a local port that replays the scripted `forge_calls` in order.

    Generic, not tied to one command: the binary reaches it through
    `LIGHTHOUSE_GITHUB_API_URL` and a dummy token. A call has `method`, `path` (the
    request URI, query included), optional `request_body` (JSON the request must
    carry), `status`, optional `body` (JSON) and `headers`; `{forge_url}` in a body or
    a header value is this server's base URL. A request that is not the next call, or
    one after the list ran out, is answered 599 and recorded in `problems`; `made` is
    the number of calls served. The Go twin is `internal/scriptcase.ReplayForge`.
    """

    def __init__(self, calls: list[dict]):
        self.calls = calls
        self.made = 0
        self.problems: list[str] = []
        replay = self

        class Handler(http.server.BaseHTTPRequestHandler):
            def log_message(self, *_args):
                pass

            def _serve(self):
                length = int(self.headers.get("Content-Length") or 0)
                raw = self.rfile.read(length) if length else b""
                replay._answer(self, f"{self.command} {self.path}", raw)

            do_GET = do_POST = do_PATCH = do_PUT = do_DELETE = _serve

        self._server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        self._lock = threading.Lock()

    @property
    def url(self) -> str:
        return f"http://127.0.0.1:{self._server.server_port}"

    def _answer(self, handler, got: str, raw: bytes) -> None:
        with self._lock:
            problem, call = self._match(got, raw)
            if problem:
                self.problems.append(problem)
                handler.send_response(599)
                handler.send_header("Content-Length", "0")
                handler.end_headers()
                return
            self.made += 1
        body = json.dumps(call["body"]).replace("{forge_url}", self.url).encode() if "body" in call else b""
        handler.send_response(call["status"])
        for key, value in call.get("headers", {}).items():
            handler.send_header(key, value.replace("{forge_url}", self.url))
        if body:
            handler.send_header("Content-Type", "application/json")
        handler.send_header("Content-Length", str(len(body)))
        handler.end_headers()
        handler.wfile.write(body)

    def _match(self, got: str, raw: bytes):
        if self.made >= len(self.calls):
            return f"unexpected request {got}: all {len(self.calls)} scripted calls were used", None
        call = self.calls[self.made]
        want = f"{call['method']} {call['path']}"
        if want != got:
            return f"call {self.made + 1}: expected {want}, got {got}", None
        if "request_body" in call:
            try:
                same = json.loads(raw) == call["request_body"]
            except ValueError:
                same = False
            if not same:
                return f"call {self.made + 1} {got}: expected request body {call['request_body']!r}, got {raw!r}", None
        return None, call

    def __enter__(self):
        threading.Thread(target=self._server.serve_forever, daemon=True).start()
        return self

    def __exit__(self, *_exc):
        self._server.shutdown()
        self._server.server_close()


def fill_fixture(value, root: str):
    """Fill the placeholders of a shared fixture, recursively.

    `{root}` is the temporary directory (not passed through realpath),
    `{real:<path>}` is the realpath of `{root}/<path>` and `{home:<name>}` is
    the home directory of user `<name>`, `{rev:<path>:<rev>}` the output of
    `git -C {root}/<path> rev-parse <rev>`, `{short:<path>:<rev>}` the same with
    `--short=7` and `{empty_tree}` the empty tree.
    Fill them after the last step of the case. The keys of a dict are filled too.
    """
    if isinstance(value, str):
        value = re.sub(
            r"\{short:([^:}]+):([^}]+)\}",
            lambda m: subprocess.run(
                ["git", "-C", f"{root}/{m[1]}", "rev-parse", "--short=7", m[2]],
                capture_output=True,
                text=True,
                check=True,
            ).stdout.strip(),
            value,
        )
        value = re.sub(
            r"\{rev:([^:}]+):([^}]+)\}",
            lambda m: subprocess.run(
                ["git", "-C", f"{root}/{m[1]}", "rev-parse", m[2]],
                capture_output=True,
                text=True,
                check=True,
            ).stdout.strip(),
            value,
        )
        value = value.replace("{empty_tree}", EMPTY_TREE)
        value = re.sub(
            r"\{real:([^}]+)\}", lambda m: os.path.realpath(f"{root}/{m[1]}"), value
        )
        value = value.replace("{root}", root)
        return re.sub(r"\{home:([^}]+)\}", lambda m: pwd.getpwnam(m[1]).pw_dir, value)
    if isinstance(value, list):
        return [fill_fixture(v, root) for v in value]
    if isinstance(value, dict):
        return {fill_fixture(k, root): fill_fixture(v, root) for k, v in value.items()}
    return value


# The `origin` of a forge worktree and of its main checkout in #36.
ORIGIN = "https://github.com/Acme/widgets.git"


def forge_worktree(project: Path, base: Path, origin: str = ORIGIN) -> Path:
    """A worktree of a separate bare clone of `project`, the way forge sets it up (#36).

    A different `--git-common-dir` than `project`; the clone's `origin` is `origin`.
    """
    clone = base / "clone.git"
    git(base, "clone", "-q", "--bare", str(project), str(clone))
    git(clone, "remote", "set-url", "origin", origin)
    worktree = base / "work"
    git(clone, "worktree", "add", "-q", str(worktree), "-b", "work")
    return worktree


def past_this_second() -> None:
    """Wait until the next whole second.

    Source 2 of bash-write-check also walks the reflog since the start of pre,
    rounded down to whole seconds: a setup commit (e.g. that of `init_repo`)
    from the same second then counts too (deliberate over-checking). A test
    that wants a setup commit to fall outside the call first waits out the
    second.
    """
    time.sleep(1.02 - time.time() % 1.0)


def bash_payload(cmd: str, cwd: Path) -> dict:
    return {"tool_name": "Bash", "cwd": str(cwd), "tool_input": {"command": cmd}}


# A Claude Code transcript, as far as `lighthouse turn-roots` (#54) reads it: one JSON
# object per line, a prompt as a `user` entry with text, a tool call as an
# `assistant` entry with a `tool_use` block.
PROMPT_TS = "2026-10-25T10:00:00.000Z"


def stamp(at: float) -> str:
    """`at` (epoch seconds) as a transcript `timestamp` in UTC, the format of `PROMPT_TS`."""
    moment = datetime.fromtimestamp(at, timezone.utc)
    return moment.strftime("%Y-%m-%dT%H:%M:%S.") + f"{moment.microsecond // 1000:03d}Z"


def prompt_entry(cwd: Path, ts: str = PROMPT_TS) -> dict:
    return {
        "type": "user",
        "timestamp": ts,
        "cwd": str(cwd),
        "message": {"role": "user", "content": "go"},
    }


def tool_entry(name: str, tool_input: dict, cwd: Path, ts: str = "2026-10-25T10:00:01.000Z") -> dict:
    return {
        "type": "assistant",
        "timestamp": ts,
        "cwd": str(cwd),
        "message": {
            "role": "assistant",
            "content": [{"type": "tool_use", "id": "toolu_1", "name": name, "input": tool_input}],
        },
    }


def result_entry(cwd: Path, ts: str = "2026-10-25T10:00:02.000Z") -> dict:
    return {
        "type": "user",
        "timestamp": ts,
        "cwd": str(cwd),
        "message": {
            "role": "user",
            "content": [{"type": "tool_result", "tool_use_id": "toolu_1", "content": "ok"}],
        },
    }


def write_transcript(path: Path, entries: list[dict]) -> Path:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text("".join(json.dumps(e) + "\n" for e in entries), encoding="utf-8")
    return path


def stop_payload(cwd: Path, transcript: Path, active: bool = False) -> dict:
    return {
        "session_id": "s1",
        "transcript_path": str(transcript),
        "cwd": str(cwd),
        "hook_event_name": "Stop",
        "stop_hook_active": active,
    }


def tool_bin(
    path: Path,
    tools: tuple[str, ...],
    *,
    drop: str = "",
    stubs: dict[str, str] | None = None,
) -> Path:
    """A PATH directory with the real tooling, minus one, plus any stubs.

    That way each limb of the preflight can be tested to fail loudly on exactly
    that limb: an empty PATH covers all three at once and so none separately
    (M11).
    """
    path.mkdir(parents=True, exist_ok=True)
    for tool in tools:
        if tool == drop:
            continue
        real = shutil.which(tool)
        if real:
            (path / tool).symlink_to(real)
    for name, script in (stubs or {}).items():
        stub = path / name
        if stub.exists():
            stub.unlink()
        stub.write_text(script, encoding="utf-8")
        stub.chmod(0o755)
    return path


# The usage gate (#66): every test that runs it uses `guard_env`, so a test run
# inside a session with a full window never writes real keys, markers or refreshes.
_GUARD_ENV_DROP = (
    "LIGHTHOUSE_CONTROLLER_RUN",
    "LIGHTHOUSE_CONTROLLER_STATE",
    "LIGHTHOUSE_USAGE_REFRESH",
    "LIGHTHOUSE_TOML",
)


def _path_without_lighthouse(shadow: Path) -> str:
    """PATH with every directory that holds a `lighthouse` replaced by a copy of
    symlinks without it, so the other tools there stay reachable."""
    dirs = []
    for i, entry in enumerate(os.environ.get("PATH", "").split(os.pathsep)):
        directory = Path(entry)
        if not os.access(directory / "lighthouse", os.X_OK):
            dirs.append(entry)
            continue
        copy = shadow / str(i)
        copy.mkdir(parents=True, exist_ok=True)
        for item in directory.iterdir():
            link = copy / item.name
            if item.name != "lighthouse" and not link.is_symlink():
                link.symlink_to(item)
        dirs.append(str(copy))
    return os.pathsep.join(dirs)


@functools.cache
def lighthouse_dir() -> Path:
    """The directory with `lighthouse` built from this checkout: built once per process,
    removed at exit. `tests/pack/python/conftest.py` hands out the same directory."""
    out = Path(tempfile.mkdtemp(prefix="lighthouse-bin-"))
    atexit.register(shutil.rmtree, out, True)
    subprocess.run(
        ["go", "build", "-o", str(out / "lighthouse"), "./cmd/lighthouse"],
        cwd=REPO,
        check=True,
    )
    return out


def _lighthouse(*args: str, env: dict[str, str] | None = None) -> subprocess.CompletedProcess:
    return subprocess.run(
        [str(lighthouse_dir() / "lighthouse"), *args],
        capture_output=True, text=True, check=False, env=env, cwd=REPO,
    )


def config_kinds(keys: Iterable[str]) -> dict[str, str]:
    """key -> schema kind ("str", "list[str]", ...), "known" or "unknown", from one call of
    `lighthouse config --kind`."""
    keys = list(keys)
    if not keys:
        return {}
    result = _lighthouse("config", "--kind", *keys)
    assert result.returncode == 0, result.stderr
    return dict(line.split("\t", 1) for line in result.stdout.splitlines())


def is_known_key(key: str) -> bool:
    return config_kinds([key])[key] != "unknown"


@functools.cache
def _schema_dump() -> str:
    result = _lighthouse("config", "--schema")
    assert result.returncode == 0, result.stderr
    return result.stdout


def config_schema() -> types.SimpleNamespace:
    """The schema of `lighthouse config --schema`: SCHEMA (key -> kind), DEFAULTS, BASE_KEYS,
    HOST_KEYS, OPTION_PREFIXES and LANGUAGES."""
    data = json.loads(_schema_dump())
    return types.SimpleNamespace(
        SCHEMA=data["schema"], DEFAULTS=data["defaults"], BASE_KEYS=data["base_keys"],
        HOST_KEYS=data["host_keys"], OPTION_PREFIXES=data["option_prefixes"],
        LANGUAGES=data["languages"], BOOL="bool",
    )


def path_with_lighthouse() -> str:
    """The current PATH with the built `lighthouse` first."""
    return f"{lighthouse_dir()}{os.pathsep}{os.environ.get('PATH', '')}"


def lighthouse_stub(body: str) -> str:
    """A fake `lighthouse` that runs `body` for every call but `turn-roots` and `config`,
    which go to the binary built from this checkout: a Stop gate needs them to find its roots
    and to read its configuration."""
    real = shlex.quote(str(lighthouse_dir() / "lighthouse"))
    return f'#!/bin/bash\ncase "$1" in turn-roots|config) exec {real} "$@";; esac\n{body}'


def guard_env(tmp_path: Path, **extra: str) -> dict[str, str]:
    """`os.environ` with HOME and the XDG dirs under `tmp_path`, the controller and
    refresh variables and LIGHTHOUSE_TOML removed, no real `lighthouse` on PATH and
    TZ=UTC; `extra` goes on top."""
    env = dict(os.environ)
    for name in _GUARD_ENV_DROP:
        env.pop(name, None)
    home = tmp_path / "home"
    home.mkdir(parents=True, exist_ok=True)
    env.update(
        HOME=str(home),
        XDG_CACHE_HOME=str(home / ".cache"),
        XDG_STATE_HOME=str(home / ".local" / "state"),
        XDG_DATA_HOME=str(home / ".local" / "share"),
        PATH=_path_without_lighthouse(tmp_path / "path-shadow"),
        TZ="UTC",
    )
    env.update(extra)
    return env


def alarm_line(
    kind: str = "venster",
    pct: int = 92,
    local: str = "14:30",
    delta: timedelta = timedelta(hours=1),
) -> str:
    """`<kind> <pct> <local> <reset_at>` without a newline; reset_at = now + delta, UTC."""
    at = (datetime.now(timezone.utc) + delta).strftime("%Y-%m-%dT%H:%M:%SZ")
    return f"{kind} {pct} {local} {at}"


def write_alarm(env: dict, line: str) -> Path:
    """Write `line` plus a newline as the alarm the guard of `env` reads."""
    path = Path(env["XDG_CACHE_HOME"]) / "lighthouse" / "usage-window.alarm"
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(line + "\n", encoding="utf-8")
    return path


# The tools `run.sh` needs, for a test that runs it on a bare PATH.
RUN_SH_TOOLS = ("sh", "dirname", "grep", "sed", "awk", "cat", "mktemp", "rm")

# A `lighthouse` for the hook tests: `hook ...` runs the binary built from this checkout, every
# other call (the detached `usage refresh` of `usage-gate prompt`) does nothing but write its
# argv and LIGHTHOUSE_USAGE_REFRESH as `<argv>|<value>` to $LIGHTHOUSE_SHIM_TRACE, after
# sleeping $LIGHTHOUSE_SHIM_DELAY seconds. So no test ever starts a real `claude -p /usage`.
_SHIM = """#!/bin/sh
if [ "${{1:-}}" = hook ]; then
  exec {binary} "$@"
fi
if [ -n "${{LIGHTHOUSE_SHIM_DELAY:-}}" ]; then
  sleep "$LIGHTHOUSE_SHIM_DELAY"
fi
if [ -n "${{LIGHTHOUSE_SHIM_TRACE:-}}" ]; then
  printf '%s|%s' "$*" "${{LIGHTHOUSE_USAGE_REFRESH:-}}" > "$LIGHTHOUSE_SHIM_TRACE"
fi
exit 0
"""


@functools.cache
def shim_dir() -> Path:
    """A directory with only the `lighthouse` shim (`_SHIM`), made once per process."""
    out = Path(tempfile.mkdtemp(prefix="lighthouse-shim-"))
    atexit.register(shutil.rmtree, out, True)
    shim = out / "lighthouse"
    shim.write_text(_SHIM.format(binary=shlex.quote(str(lighthouse_dir() / "lighthouse"))))
    shim.chmod(0o755)
    return out


def refresh_trace(env: dict) -> Path:
    """Where the shim of `run_guard` writes what a non-hook `lighthouse` call got."""
    return Path(env["HOME"]).parent / "lighthouse-shim.trace"


def run_sh_hook(
    root: Path,
    name: str,
    payload: dict | str,
    *,
    cwd: Path,
    env: dict | None = None,
    args: tuple[str, ...] = (),
    with_binary: bool = True,
) -> subprocess.CompletedProcess:
    """Run the installed `run.sh <name> [args]` of `root` with `payload` on stdin.

    `env` is the whole environment (default `os.environ`). With `with_binary` the shim
    directory goes first on its PATH, so `run.sh` finds the binary of this checkout and a
    `lighthouse usage refresh` never runs for real; LIGHTHOUSE_REPO_ROOT defaults to `root`.
    """
    full = dict(os.environ if env is None else env)
    full.setdefault("LIGHTHOUSE_REPO_ROOT", str(root))
    if with_binary:
        full["PATH"] = f"{shim_dir()}{os.pathsep}{full.get('PATH', '')}"
    return subprocess.run(
        [str(root / _INSTALL_DIRS["hooks"] / "run.sh"), name, *args],
        input=payload if isinstance(payload, str) else json.dumps(payload),
        capture_output=True,
        text=True,
        cwd=str(cwd),
        env=full,
        check=False,
        timeout=60,
    )


def run_guard(
    root: Path, stand: str, payload: dict | str, env: dict
) -> subprocess.CompletedProcess:
    """Run the installed `run.sh usage-gate <stand>` of `root` with `payload` on stdin; the
    shim records a refresh in `refresh_trace(env)`."""
    env = dict(env, LIGHTHOUSE_REPO_ROOT=str(root))
    env.setdefault("LIGHTHOUSE_SHIM_TRACE", str(refresh_trace(env)))
    return run_sh_hook(root, "usage-gate", payload, cwd=root, env=env, args=(stand,))


def classify(rc: int, output: bytes) -> str:
    """The verdict of a gate: `"ok"`, `"finding"` or `"unchecked"`.

    The rule of `bash_writes.classify`, for the language-pack tests that read
    the result of one gate: rc 0 is ok, rc 2 is a finding unless the FIRST
    output line holds `COULD NOT CHECK`, every other rc is unchecked.
    """
    if rc == 0:
        return "ok"
    if rc != 2:
        return "unchecked"
    first = output.decode(errors="replace").split("\n", 1)[0]
    return "unchecked" if "COULD NOT CHECK" in first else "finding"


# Where each directory of `pack/core` lands (`internal/packs`). `skills/`, `agents/`,
# `templates/`, `workflows/` and the JSON/TOML at the top are not installed here:
# the hook and script tests do not need them, and `init` itself is tested in Go.
_INSTALL_DIRS = {
    "hooks": Path(".claude/hooks/lighthouse"),
    "scripts": Path(".lighthouse/scripts"),
}


# The smallest `lighthouse.toml` init leaves behind: `[project]` and `[forge]`.
MINIMAL_TOML = '[project]\nlanguage = "en"\n\n[forge]\nkind = "github"\nrepo = "Acme/widgets"\n'


def _install_dirs(pack_dir: Path, root: Path) -> None:
    for name, target in _INSTALL_DIRS.items():
        source_dir = pack_dir / name
        if not source_dir.is_dir():
            continue
        for source in sorted(source_dir.rglob("*")):
            if source.is_dir() or source.suffix == ".json":
                continue
            dest = root / target / source.relative_to(source_dir)
            dest.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(source, dest)
            # Like `internal/packs`: executable when the file starts with "#!".
            if source.read_bytes().startswith(b"#!"):
                dest.chmod(0o755)


# Everything the installed `run.sh` calls: a test that narrows the PATH keeps these on it.
RUN_SH_TOOLS: tuple[str, ...] = (
    "dirname",
    "grep",
    "sed",
    "awk",
    "cat",
    "mktemp",
    "rm",
    "head",
    "mkdir",
    "find",
    "touch",
    "date",
)


def hook_shim(root: Path, name: str) -> Path:
    """An executable `root/.lighthouse/test-shims/<name>` that runs the installed
    `run.sh <name>` of `root`, so a test starts a Go hook the way the harness does."""
    shim = root / ".lighthouse" / "test-shims" / name
    shim.parent.mkdir(parents=True, exist_ok=True)
    run_sh = root / _INSTALL_DIRS["hooks"] / "run.sh"
    shim.write_text(f"#!/bin/sh\nexec '{run_sh}' {name} \"$@\"\n", encoding="utf-8")
    shim.chmod(0o755)
    return shim


def install_core(root: Path, toml: str | None = MINIMAL_TOML) -> Path:
    """Copy `pack/core` into `root` the way `lighthouse init` lays it out, with a
    `lighthouse.toml` unless `toml` is None or one is there, and return the shim
    that runs the installed main-commit-gate through `run.sh`."""
    if toml is not None and not (root / "lighthouse.toml").exists():
        root.mkdir(parents=True, exist_ok=True)
        (root / "lighthouse.toml").write_text(toml, encoding="utf-8")
    _install_dirs(CORE, root)
    headings = CORE / "headings.toml"
    if headings.is_file():
        (root / ".lighthouse").mkdir(parents=True, exist_ok=True)
        shutil.copyfile(headings, root / ".lighthouse" / "headings.toml")
    return hook_shim(root, "main-commit-gate")


def install_pack(name: str, root: Path, toml: str | None = MINIMAL_TOML) -> Path:
    """Install `pack/core` plus `pack/<name>` into `root`, as `install_core` does,
    and return the installed hook directory."""
    install_core(root, toml)
    _install_dirs(CORE.parent / name, root)
    return root / _INSTALL_DIRS["hooks"]


# One installation for the whole session; the conftest points LIGHTHOUSE_REPO_ROOT at it.
INSTALL_ROOT = Path(tempfile.mkdtemp(prefix="lighthouse-core-"))
atexit.register(shutil.rmtree, INSTALL_ROOT, True)
GUARD = install_core(INSTALL_ROOT)
INSTALLED_HOOKS = INSTALL_ROOT / _INSTALL_DIRS["hooks"]


DEFAULT_FIXTURE_REPO = "Acme/widgets"


def write_fixture_config(
    root: Path, repo: str | None = DEFAULT_FIXTURE_REPO, test_dir: str | None = None
) -> None:
    """The config of a PR-body-gate or `evidence` fixture case, under `root`.

    A `lighthouse.toml` with `forge.repo = repo` (no key when `repo` is None) and,
    with `test_dir`, `[test_gate] evidence_test_dir`; plus the headings table the
    AC count reads. The caller points `LIGHTHOUSE_REPO_ROOT` at `root`.
    """
    toml = '[project]\nlanguage = "en"\n\n[forge]\nkind = "github"\n'
    if repo is not None:
        toml += f'repo = "{repo}"\n'
    if test_dir is not None:
        toml += f'\n[test_gate]\nevidence_test_dir = "{test_dir}"\n'
    (root / "lighthouse.toml").write_text(toml, encoding="utf-8")
    (root / ".lighthouse").mkdir(parents=True, exist_ok=True)
    shutil.copyfile(CORE / "headings.toml", root / ".lighthouse" / "headings.toml")


# --- PR bodies for the pr-create-gate tests ---------------------------------------------------

ROWS1 = [("1. een", "`grep -F a f`", "a")]
ROWS2 = [*ROWS1, ("2. twee", "`ls`", "f")]
ROWS3 = [*ROWS2, ("3. drie", "`true`", "ok")]

ISSUE_WITH_THREE_AC = (
    "## Context\n\ntekst\n\n"
    "### Acceptatiecriteria\n- [ ] a\n- [x] b\n- [X] c\n\n"
    "### Definition of Done\n- [ ] d\n"
)


def closes_body(nr: int, rows: list[tuple[str, str, str]]) -> str:
    """A body with `Closes #nr` and an Evidence table with the given rows."""
    table = "\n".join(f"| {ac} | {cmd} | {res} |" for ac, cmd, res in rows)
    return (
        f"## Related issues\n\nCloses #{nr}\n\n"
        f"### Evidence #{nr}\n\n"
        f"| AC | Command | Result |\n|---|---|---|\n{table}\n"
    )
