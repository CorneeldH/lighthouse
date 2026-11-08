"""`run.sh` finds the lighthouse binary, builds it from the checkout at `version = "dev"` without
ever waiting for that build, and maps a missing binary or an unknown hook per the blocking list
(spec 2026-11-07-zonder-python-design §1, §2, §10).

Every test runs the script from a temporary repo with a PATH of symlinks to the tools it needs,
so no real `go` or `lighthouse` of the host leaks in. The fake `go` logs each call (its cwd and
arguments), sleeps `FAKE_GO_SLEEP` seconds and writes a fake binary to its `-o` path; the fake
PATH `lighthouse` echoes its arguments and has its rc, stdout and stderr set by the environment.
"""

import json
import os
import shutil
import subprocess
import time
from pathlib import Path

import pytest

HOOKS = Path(__file__).resolve().parents[2] / "pack" / "core" / "hooks"
RUN_SH = HOOKS / "run.sh"

TOOLS = [
    "sh", "git", "grep", "sed", "awk", "cat", "mkdir", "rmdir", "rm", "mv", "find", "touch",
    "date", "mktemp", "sleep", "chmod", "dirname", "head", "env", "tr", "nohup", "setsid",
]

FAKE_GO = r"""#!/bin/sh
printf '%s|%s\n' "$(pwd -P)" "$*" >>"$FAKE_GO_LOG"
if [ "$1" != build ] || [ "$2" != -o ] || [ "$4" != ./cmd/lighthouse ]; then
  echo "fake go: unexpected arguments: $*" >&2
  exit 99
fi
sleep "${FAKE_GO_SLEEP:-0}"
if [ -n "${FAKE_GO_FAIL:-}" ]; then
  echo "compile error: boom"
  exit 1
fi
printf '#!/bin/sh\necho "built-binary $*"\n' >"$3"
chmod +x "$3"
"""

FAKE_LIGHTHOUSE = r"""#!/bin/sh
cat >"$FAKE_LH_STDIN"
if [ -n "${FAKE_LH_SHOW_ROOT:-}" ]; then printf 'root=%s\n' "${LIGHTHOUSE_REPO_ROOT-unset}"; fi
if [ -n "${FAKE_LH_OUT+x}" ]; then printf '%s' "$FAKE_LH_OUT"; else echo "path-binary $*"; fi
printf '%s' "${FAKE_LH_ERR:-}" >&2
exit "${FAKE_LH_RC:-0}"
"""

BLOCKING = "main-commit-gate"
NON_BLOCKING = "worktree-check"


class Repo:
    def __init__(self, tmp: Path, *, version: str = "dev", go: bool = True,
                 lighthouse: bool = True, setsid: bool = True, parent: str = "",
                 source: bool = True) -> None:
        self.tmp = tmp
        self.root = tmp / parent / "repo" if parent else tmp / "repo"
        self.root.mkdir(parents=True)
        env = dict(os.environ, GIT_AUTHOR_NAME="t", GIT_AUTHOR_EMAIL="t@example.com",
                   GIT_COMMITTER_NAME="t", GIT_COMMITTER_EMAIL="t@example.com")
        self.git_env = env
        self.git("init", "-q")
        (self.root / "README").write_text("x\n")
        self.git("add", "README")
        self.git("commit", "-q", "-m", "init")
        if source:
            # The layout of lighthouse's own checkout: only there does `dev` build.
            (self.root / "cmd" / "lighthouse").mkdir(parents=True)
            (self.root / "cmd" / "lighthouse" / "main.go").write_text("package main\n")
        hooks = self.root / ".claude" / "hooks" / "lighthouse"
        hooks.mkdir(parents=True)
        shutil.copy2(RUN_SH, hooks / "run.sh")
        shutil.copy2(HOOKS / "blocking", hooks / "blocking")
        self.run_sh = hooks / "run.sh"
        (self.root / ".lighthouse").mkdir()
        (self.root / ".lighthouse" / "manifest.toml").write_text(
            f'# Managed by lighthouse init. Do not edit by hand.\n\nversion = "{version}"\n'
        )
        self.bin = self.root / ".lighthouse" / "bin"

        path = tmp / "path"
        path.mkdir()
        for tool in TOOLS:
            if tool == "setsid" and not setsid:
                continue
            found = shutil.which(tool)
            if found:
                (path / tool).symlink_to(found)
        if go:
            write_script(path / "go", FAKE_GO)
        if lighthouse:
            write_script(path / "lighthouse", FAKE_LIGHTHOUSE)
        self.go_log = tmp / "go.log"
        self.stdin_log = tmp / "lh-stdin"
        self.env = {
            "PATH": str(path),
            "HOME": str(tmp),
            "TMPDIR": str(tmp),
            "FAKE_GO_LOG": str(self.go_log),
            "FAKE_LH_STDIN": str(self.stdin_log),
        }

    def git(self, *args: str) -> str:
        return subprocess.run(["git", "-C", str(self.root), *args], check=True,
                              capture_output=True, text=True, env=self.git_env).stdout.strip()

    def head(self) -> str:
        return self.git("rev-parse", "HEAD")

    def commit(self) -> None:
        (self.root / "README").write_text(f"{time.time()}\n")
        self.git("commit", "-q", "-am", "next")

    def go_calls(self) -> list[str]:
        if not self.go_log.exists():
            return []
        return self.go_log.read_text().splitlines()

    def call(self, hook: str, *args: str, stdin: str = "{}", cwd: Path | None = None,
             script: str | None = None, **env: str) -> subprocess.CompletedProcess:
        return subprocess.run(
            [script or str(self.run_sh), hook, *args],
            input=stdin, capture_output=True, text=True, cwd=str(cwd or self.root),
            env={**self.env, **env}, timeout=20, check=False,
        )

    def wait_built(self, calls: int, timeout: float = 5.0) -> None:
        """Wait until the go log has `calls` entries and the lock is gone."""
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            if len(self.go_calls()) >= calls and not (self.bin / "build.lock").exists():
                return
            time.sleep(0.05)
        raise AssertionError(f"build not done: go calls {self.go_calls()}, "
                             f"lock {(self.bin / 'build.lock').exists()}")


def write_script(path: Path, text: str) -> None:
    path.write_text(text)
    path.chmod(0o755)


def age(path: Path, seconds: float) -> None:
    t = time.time() - seconds
    os.utime(path, (t, t))


def old_checkout_binary(repo: Repo) -> None:
    repo.bin.mkdir(parents=True)
    write_script(repo.bin / "lighthouse", '#!/bin/sh\necho "old-binary $*"\n')
    (repo.bin / "stamp").write_text("0000000000000000000000000000000000000000\n")


INSTALL = ("COULD NOT CHECK: lighthouse not on PATH — install v0.12.1: go install "
           "github.com/CorneeldH/lighthouse/cmd/lighthouse@v0.12.1, or download it from the release")


# --- missing binary ---------------------------------------------------------------------------

def test_missing_binary_blocks_blocking_hook(tmp_path):
    repo = Repo(tmp_path, version="v0.12.1", lighthouse=False)
    r = repo.call(BLOCKING, "pre")
    assert r.returncode == 2
    assert r.stderr == INSTALL + "\n"
    assert r.stdout == ""


def test_missing_binary_warns_for_other_hook(tmp_path):
    repo = Repo(tmp_path, version="v0.12.1", lighthouse=False)
    r = repo.call(NON_BLOCKING)
    assert r.returncode == 0
    assert json.loads(r.stdout) == {"systemMessage": INSTALL}
    assert r.stderr == ""


def test_missing_binary_at_dev_names_checkout_build_and_use_path(tmp_path):
    repo = Repo(tmp_path, go=False, lighthouse=False)
    r = repo.call(BLOCKING)
    assert r.returncode == 2
    assert r.stderr.startswith("COULD NOT CHECK: lighthouse not on PATH")
    assert "go install ./cmd/lighthouse" in r.stderr
    assert ".lighthouse/bin/use-path" in r.stderr
    assert "@v" not in r.stderr


# --- the binary runs --------------------------------------------------------------------------

def test_binary_output_passes_untouched(tmp_path):
    repo = Repo(tmp_path, version="v0.12.1")
    r = repo.call(BLOCKING, "pre", stdin='{"tool_name":"Bash"}',
                  FAKE_LH_OUT='{"decision":"block"}\n', FAKE_LH_ERR="reason: on main\n",
                  FAKE_LH_RC="2")
    assert r.returncode == 2
    assert r.stdout == '{"decision":"block"}\n'
    assert r.stderr == "reason: on main\n"
    assert repo.stdin_log.read_text() == '{"tool_name":"Bash"}'


def test_binary_gets_hook_and_arguments(tmp_path):
    repo = Repo(tmp_path, version="v0.12.1")
    r = repo.call(BLOCKING, "pre")
    assert (r.returncode, r.stdout, r.stderr) == (0, f"path-binary hook {BLOCKING} pre\n", "")


def test_other_failure_passes_untouched(tmp_path):
    repo = Repo(tmp_path, version="v0.12.1")
    r = repo.call(BLOCKING, FAKE_LH_ERR="Error: something else\n", FAKE_LH_RC="1", FAKE_LH_OUT="")
    assert (r.returncode, r.stdout, r.stderr) == (1, "", "Error: something else\n")


UNKNOWN = "COULD NOT CHECK: unknown hook nope — update lighthouse to v0.12.1\n"


def test_unknown_hook_maps_per_list(tmp_path):
    repo = Repo(tmp_path, version="v0.12.1")
    blocking = repo.call(BLOCKING, FAKE_LH_ERR=UNKNOWN, FAKE_LH_RC="3", FAKE_LH_OUT="")
    assert (blocking.returncode, blocking.stdout, blocking.stderr) == (2, "", UNKNOWN)
    other = repo.call(NON_BLOCKING, FAKE_LH_ERR=UNKNOWN, FAKE_LH_RC="3", FAKE_LH_OUT="")
    assert other.returncode == 0
    assert json.loads(other.stdout) == {"systemMessage": UNKNOWN.rstrip("\n")}


def test_unknown_hook_at_dev_never_blocks(tmp_path):
    repo = Repo(tmp_path, go=False)
    r = repo.call(BLOCKING, FAKE_LH_ERR=UNKNOWN, FAKE_LH_RC="3", FAKE_LH_OUT="")
    assert r.returncode == 0
    assert json.loads(r.stdout) == {"systemMessage": UNKNOWN.rstrip("\n")}


def test_unknown_hook_message_is_valid_json(tmp_path):
    repo = Repo(tmp_path, version="v0.12.1")
    text = 'COULD NOT CHECK: unknown hook "x" in C:\\path\twith tab\nsecond line'
    r = repo.call(NON_BLOCKING, FAKE_LH_ERR=text + "\n", FAKE_LH_RC="3", FAKE_LH_OUT="")
    assert r.returncode == 0
    assert json.loads(r.stdout) == {"systemMessage": text}


OLD_BINARY = 'Error: unknown command "hook" for "lighthouse"\nRun \'lighthouse --help\' for usage.\n'


def test_path_binary_without_hook_command_maps_per_list(tmp_path):
    repo = Repo(tmp_path, version="v0.12.1")
    blocking = repo.call(BLOCKING, FAKE_LH_ERR=OLD_BINARY, FAKE_LH_RC="1", FAKE_LH_OUT="")
    assert blocking.returncode == 2
    assert blocking.stderr.startswith("COULD NOT CHECK: lighthouse has no hook command")
    assert "@v0.12.1" in blocking.stderr
    other = repo.call(NON_BLOCKING, FAKE_LH_ERR=OLD_BINARY, FAKE_LH_RC="1", FAKE_LH_OUT="")
    assert other.returncode == 0
    assert json.loads(other.stdout)["systemMessage"] == blocking.stderr.rstrip("\n")


# --- the build from the checkout --------------------------------------------------------------

def _parallel_calls_build_once(repo: Repo) -> None:
    env = {**repo.env, "FAKE_GO_SLEEP": "3"}
    procs = []
    for _ in range(8):
        start = time.monotonic()
        p = subprocess.Popen([str(repo.run_sh), NON_BLOCKING], stdin=subprocess.PIPE,
                             stdout=subprocess.PIPE, stderr=subprocess.PIPE, cwd=str(repo.root),
                             env=env, text=True)
        procs.append((p, start))
    for p, start in procs:
        out, _ = p.communicate("{}", timeout=10)
        assert time.monotonic() - start < 2.0, "run.sh waited for the build"
        assert p.returncode == 0
        assert out == f"path-binary hook {NON_BLOCKING}\n"
    assert len(repo.go_calls()) == 1
    repo.wait_built(1, timeout=5.0)
    assert len(repo.go_calls()) == 1
    assert os.access(repo.bin / "lighthouse", os.X_OK)
    assert (repo.bin / "stamp").read_text().strip() == repo.head()
    assert not (repo.bin / "build.lock").exists()
    assert [p.name for p in repo.bin.iterdir() if p.name.startswith("lighthouse.tmp")] == []
    r = repo.call(NON_BLOCKING)
    assert r.stdout == f"built-binary hook {NON_BLOCKING}\n"
    assert len(repo.go_calls()) == 1


def test_parallel_calls_build_once_and_never_wait(tmp_path):
    _parallel_calls_build_once(Repo(tmp_path))


def test_build_detaches_without_setsid(tmp_path):
    repo = Repo(tmp_path, setsid=False)
    assert not (tmp_path / "path" / "setsid").exists()
    _parallel_calls_build_once(repo)


def test_runs_previous_binary_during_build(tmp_path):
    repo = Repo(tmp_path)
    old_checkout_binary(repo)
    start = time.monotonic()
    r = repo.call(NON_BLOCKING, FAKE_GO_SLEEP="3")
    assert time.monotonic() - start < 2.0
    assert r.stdout == f"old-binary hook {NON_BLOCKING}\n"
    repo.wait_built(1)
    assert repo.call(NON_BLOCKING).stdout == f"built-binary hook {NON_BLOCKING}\n"


def test_current_stamp_starts_no_build(tmp_path):
    repo = Repo(tmp_path)
    old_checkout_binary(repo)
    (repo.bin / "stamp").write_text(repo.head() + "\n")
    r = repo.call(NON_BLOCKING)
    assert r.stdout == f"old-binary hook {NON_BLOCKING}\n"
    time.sleep(0.5)
    assert repo.go_calls() == []


def test_new_commit_rebuilds(tmp_path):
    repo = Repo(tmp_path)
    repo.call(NON_BLOCKING)
    repo.wait_built(1)
    repo.commit()
    repo.call(NON_BLOCKING)
    repo.wait_built(2)
    assert (repo.bin / "stamp").read_text().strip() == repo.head()


def test_stale_lock_is_removed(tmp_path):
    repo = Repo(tmp_path)
    (repo.bin / "build.lock").mkdir(parents=True)
    age(repo.bin / "build.lock", 150)
    repo.call(NON_BLOCKING)
    repo.wait_built(1)
    assert (repo.bin / "stamp").read_text().strip() == repo.head()


def test_fresh_lock_starts_nothing(tmp_path):
    repo = Repo(tmp_path)
    (repo.bin / "build.lock").mkdir(parents=True)
    age(repo.bin / "build.lock", 100)
    r = repo.call(NON_BLOCKING)
    assert r.returncode == 0
    time.sleep(0.5)
    assert repo.go_calls() == []
    assert (repo.bin / "build.lock").is_dir()


def test_failed_build_pauses_60s(tmp_path):
    repo = Repo(tmp_path)
    fail = {"FAKE_GO_FAIL": "1"}
    repo.call(NON_BLOCKING, **fail)
    repo.wait_built(1)
    assert (repo.bin / "build.failed").read_text().strip() == repo.head()
    assert "compile error: boom" in (repo.bin / "build.log").read_text()
    assert not (repo.bin / "lighthouse").exists()
    assert [p.name for p in repo.bin.iterdir() if p.name.startswith("lighthouse.tmp")] == []

    age(repo.bin / "build.failed", 50)
    repo.call(NON_BLOCKING, **fail)
    time.sleep(0.5)
    assert len(repo.go_calls()) == 1

    age(repo.bin / "build.failed", 61)
    repo.call(NON_BLOCKING, **fail)
    repo.wait_built(2)

    repo.commit()
    repo.call(NON_BLOCKING)
    repo.wait_built(3)
    assert not (repo.bin / "build.failed").exists()
    assert (repo.bin / "stamp").read_text().strip() == repo.head()


def test_use_path_skips_build(tmp_path):
    repo = Repo(tmp_path)
    old_checkout_binary(repo)
    (repo.bin / "use-path").write_text("")
    r = repo.call(NON_BLOCKING)
    assert r.stdout == f"path-binary hook {NON_BLOCKING}\n"
    time.sleep(0.5)
    assert repo.go_calls() == []


def test_no_go_uses_path(tmp_path):
    repo = Repo(tmp_path, go=False)
    old_checkout_binary(repo)
    r = repo.call(NON_BLOCKING)
    assert r.stdout == f"path-binary hook {NON_BLOCKING}\n"
    assert not (repo.bin / "build.lock").exists()


def test_not_dev_never_builds(tmp_path):
    repo = Repo(tmp_path, version="v0.12.1")
    old_checkout_binary(repo)
    r = repo.call(NON_BLOCKING)
    assert r.stdout == f"path-binary hook {NON_BLOCKING}\n"
    time.sleep(0.5)
    assert repo.go_calls() == []


def test_dev_without_source_never_builds(tmp_path):
    """`dev` in a repo that is not lighthouse's own checkout: nothing to build from."""
    repo = Repo(tmp_path, source=False)
    old_checkout_binary(repo)
    r = repo.call(NON_BLOCKING)
    assert r.stdout == f"path-binary hook {NON_BLOCKING}\n"
    time.sleep(0.5)
    assert repo.go_calls() == []
    assert not (repo.bin / "build.lock").exists()


def test_missing_binary_at_dev_without_source_names_release(tmp_path):
    repo = Repo(tmp_path, lighthouse=False, source=False)
    r = repo.call(BLOCKING)
    assert r.returncode == 2
    assert r.stderr == ("COULD NOT CHECK: lighthouse not on PATH — install it: go install "
                        "github.com/CorneeldH/lighthouse/cmd/lighthouse@latest, or download it "
                        "from the release\n")


def test_unknown_hook_at_dev_without_source_blocks(tmp_path):
    """Without a build from the checkout an old PATH binary stays old: map it like a release."""
    repo = Repo(tmp_path, source=False)
    r = repo.call(BLOCKING, FAKE_LH_ERR=UNKNOWN, FAKE_LH_RC="3", FAKE_LH_OUT="")
    assert (r.returncode, r.stdout, r.stderr) == (2, "", UNKNOWN)


# --- the repo root for the binary -------------------------------------------------------------

def test_binary_gets_repo_root_from_outside_cwd(tmp_path):
    repo = Repo(tmp_path, version="v0.12.1")
    elsewhere = tmp_path / "elsewhere"
    elsewhere.mkdir()
    r = repo.call(NON_BLOCKING, cwd=elsewhere, FAKE_LH_SHOW_ROOT="1")
    assert r.stdout == f"root={repo.root.resolve()}\npath-binary hook {NON_BLOCKING}\n"


def test_binary_keeps_a_set_repo_root(tmp_path):
    repo = Repo(tmp_path, version="v0.12.1")
    r = repo.call(NON_BLOCKING, FAKE_LH_SHOW_ROOT="1", LIGHTHOUSE_REPO_ROOT="/given")
    assert r.stdout == f"root=/given\npath-binary hook {NON_BLOCKING}\n"


def test_run_sh_from_subdir_with_space(tmp_path):
    repo = Repo(tmp_path, parent="a b")
    sub = repo.root / "sub dir"
    sub.mkdir()
    script = os.path.join("..", ".claude", "hooks", "lighthouse", "run.sh")
    r = repo.call(NON_BLOCKING, cwd=sub, script=script)
    assert r.stdout == f"path-binary hook {NON_BLOCKING}\n"
    repo.wait_built(1)
    cwd, _, args = repo.go_calls()[0].partition("|")
    assert Path(cwd) == repo.root.resolve()
    assert args.startswith(f"build -o {repo.bin.resolve()}/lighthouse.tmp.")
    r = repo.call(NON_BLOCKING, cwd=sub, script=script)
    assert r.stdout == f"built-binary hook {NON_BLOCKING}\n"


SHELLCHECK_LIB = HOOKS.parents[1] / "shell" / "scripts" / "lib" / "shellcheck-bin.sh"


def _shellcheck() -> str | None:
    """The pinned shellcheck of the shell package, else the one on the PATH."""
    pinned = subprocess.run(["bash", "-c", f'. "{SHELLCHECK_LIB}"\nresolve_shellcheck\n'],
                            capture_output=True, text=True, check=False)
    if pinned.returncode == 0 and pinned.stdout.strip():
        return pinned.stdout.strip()
    return shutil.which("shellcheck")


def test_shellcheck_clean():
    shellcheck = _shellcheck()
    if shellcheck is None:
        pytest.skip("shellcheck not installed")
    r = subprocess.run([shellcheck, "-s", "sh", "-S", "style", str(RUN_SH)], capture_output=True,
                       text=True, check=False)
    assert r.returncode == 0, r.stdout + r.stderr
