"""Shared helpers for the tests of `pack/core`: the hook-test plumbing, plus a
copy of the installed layout so the hooks run from where `lighthouse init` puts
them.

Shared helpers for the hook tests (tasks 1 to 5 of #884/#886).

Taken over from `test_instructie_hooks.py` (the private `_run`, `_git`,
`_init_repo`, `_bash`, `_tool_bin` from before this task) so that
`test_git_invocations.py` and later tasks (`test_pre_commit_gate.py`,
`test_md_links.py`, `test_hook_deadline.py`) use the same plumbing instead of
reimplementing it. `test_instructie_hooks.py` itself stays unchanged: this
file refactors no existing tests, it gives new tests the same building
blocks.
"""

import atexit
import importlib.util
import json
import os
import shlex
import shutil
import subprocess
import tempfile
import time
import types
from datetime import datetime, timedelta, timezone
from pathlib import Path

PACKS = Path(__file__).resolve().parents[2] / "pack"
CORE = PACKS / "core"
LIB = CORE / "lib"

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


# A Claude Code transcript, as far as `turn_roots.py` (#54) reads it: one JSON
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


def trace_bin(path: Path, trace: Path, tools: tuple[str, ...]) -> Path:
    """A PATH directory of stubs that append their name to `trace`, then run the
    real tool (looked up now, on the current PATH) when there is one.

    With only this directory on PATH, an empty `trace` proves the script forked
    none of `tools`.
    """
    stubs = {}
    for tool in tools:
        real = shutil.which(tool)
        run = f'exec {shlex.quote(real)} "$@"\n' if real else "exit 0\n"
        log = f"printf '%s\\n' {tool} >> {shlex.quote(str(trace))}\n"
        stubs[tool] = f"#!/bin/bash\n{log}{run}"
    return tool_bin(path, (), stubs=stubs)


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


def run_guard(
    root: Path, stand: str, payload: dict | str, env: dict
) -> subprocess.CompletedProcess:
    """Run the installed `usage-gate.sh <stand>` of `root` with `payload` on stdin."""
    return subprocess.run(
        [str(root / _INSTALL_DIRS["hooks"] / "usage-gate.sh"), stand],
        input=payload if isinstance(payload, str) else json.dumps(payload),
        capture_output=True,
        text=True,
        cwd=str(root),
        env=dict(env, LIGHTHOUSE_REPO_ROOT=str(root)),
        check=False,
        timeout=30,
    )


# A BSD `date` on top of the real GNU one: `-d` is rejected, and
# `-j [-u] -f FMT VALUE [+OUT]` parses VALUE (the format itself is not checked).
BSD_DATE_STUB = f"""#!/bin/bash
real={shutil.which("date")}
utc=""
value=""
parse=""
while [ $# -gt 0 ]; do
  case "$1" in
    -d) echo "date: illegal option -- d" >&2; exit 1 ;;
    -j) parse=1 ;;
    -u) utc=-u ;;
    -f) shift; shift; value="$1" ;;
    *) break ;;
  esac
  shift
done
if [ -n "$parse" ]; then
  exec "$real" $utc -d "$value" "$@"
fi
exec "$real" $utc "$@"
"""


def load_hook_lib(name: str) -> types.ModuleType:
    """Load `pack/core/lib/<name>.py` as a module, without running it.

    `importlib.util.spec_from_file_location` gives the module a name of its
    own (not `__main__`), so an `if __name__ == "__main__":` block in the
    loaded file does not run: the module reads no stdin on import and needs
    no argv.
    """
    path = LIB / f"{name}.py"
    spec = importlib.util.spec_from_file_location(name, path)
    assert spec is not None and spec.loader is not None
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


# Where each directory of `pack/core` lands (`internal/packs`). `skills/`, `agents/`,
# `templates/`, `workflows/` and the JSON/TOML at the top are not installed here:
# the hook and script tests do not need them, and `init` itself is tested in Go.
_INSTALL_DIRS = {
    "hooks": Path(".claude/hooks/lighthouse"),
    "lib": Path(".lighthouse/lib/lighthouse_lib"),
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


def install_core(root: Path, toml: str | None = MINIMAL_TOML) -> Path:
    """Copy `pack/core` into `root` the way `lighthouse init` lays it out, with a
    `lighthouse.toml` unless `toml` is None or one is there, and return the path of
    the installed main-commit-gate."""
    if toml is not None and not (root / "lighthouse.toml").exists():
        root.mkdir(parents=True, exist_ok=True)
        (root / "lighthouse.toml").write_text(toml, encoding="utf-8")
    _install_dirs(CORE, root)
    headings = CORE / "headings.toml"
    if headings.is_file():
        (root / ".lighthouse").mkdir(parents=True, exist_ok=True)
        shutil.copyfile(headings, root / ".lighthouse" / "headings.toml")
    return root / _INSTALL_DIRS["hooks"] / "main-commit-gate.sh"


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
INSTALLED_LIB = INSTALL_ROOT / _INSTALL_DIRS["lib"]
INSTALLED_HOOKS = INSTALL_ROOT / _INSTALL_DIRS["hooks"]


# `lighthouse_lib` as an importable package for in-process tests: a symlink to
# pack/core/lib in a temporary directory, placed first on sys.path by the conftest.
IMPORT_ROOT = Path(tempfile.mkdtemp(prefix="lighthouse-lib-"))
atexit.register(shutil.rmtree, IMPORT_ROOT, True)
(IMPORT_ROOT / "lighthouse_lib").symlink_to(LIB, target_is_directory=True)
