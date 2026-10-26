"""pack/core bash-write-gates.sh, as installed (#871).

First the not-looked-at contract of `worktree-linkcheck.sh`, the only gate in
`pack/core`: `COULD NOT CHECK` on the first line (decision 18). `_gate` merges
stdout+stderr, the way the dispatcher reads the output.

Then the end-to-end tests of `bash-write-gates.sh`: `_Session` installs
`pack/core` in a lighthouse root of its own, with stubs for `shellcheck.sh` and
`ruff-format.sh` (those gates come with `pack/shell` and `pack/python`), and runs
pre, the Bash call and post against a tmp repo, via the real wrapper. The stubs
report a fixed finding (`SC2034`, `E722`) and then quote the file, the way
shellcheck and ruff quote source lines.
"""

import functools
import json
import os
import subprocess
import time
import uuid
from pathlib import Path

import pytest

from tests.pack._testkit import (
    CORE,
    GIT_ENV,
    INSTALLED_HOOKS,
    MINIMAL_TOML,
    git,
    init_repo,
    install_core,
    past_this_second,
    tool_bin,
)

CLEAN_SH = '#!/usr/bin/env bash\nset -euo pipefail\necho "hallo"\n'
# SC2034: `x` is never used. Warning severity, so it blocks everywhere.
BROKEN_SH = '#!/usr/bin/env bash\nf() {\n  local x=1\n  echo "hoi"\n}\nf\n'


def sh(dir_: Path, text: str) -> Path:
    """Write `text` to a `.sh` file in `dir_` and return the path."""
    path = dir_ / "script.sh"
    path.write_text(text, encoding="utf-8")
    return path


def _gate(
    name: str,
    path: Path,
    cwd: Path,
    env: dict | None = None,
) -> subprocess.CompletedProcess:
    """Run `.claude/hooks/<name>` with the faked payload.

    Payload: `{"tool_name": "Bash", "cwd": <cwd>, "tool_input": {"file_path":
    <absolute path>}}`, process cwd = `cwd`. stdout+stderr merged so that the
    first line of the combined output can be classified.
    """
    payload = {
        "tool_name": "Bash",
        "cwd": str(cwd),
        "tool_input": {"file_path": str(path)},
    }
    xdg = cwd / ".xdg-runtime"
    xdg.mkdir(parents=True, exist_ok=True)
    full_env = dict(
        os.environ,
        **GIT_ENV,
        CLAUDE_PROJECT_DIR=str(cwd),
        XDG_RUNTIME_DIR=str(xdg),
    )
    full_env.update(env or {})
    return subprocess.run(
        [str(INSTALLED_HOOKS / name)],
        input=json.dumps(payload),
        cwd=str(cwd),
        env=full_env,
        stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT,
        text=True,
        check=False,
    )


# --- worktree-linkcheck.sh ----------------------------------------------------


def test_linkcheck_unchecked_form_is_on_the_first_line(tmp_path):
    bin_dir = tool_bin(
        tmp_path / "bin",
        ("jq", "python3", "git", "realpath", "dirname", "cat"),
        drop="jq",
    )
    note = tmp_path / "note.md"
    note.write_text("# hoi\n", encoding="utf-8")
    out = _gate("worktree-linkcheck.sh", note, tmp_path, {"PATH": str(bin_dir)})
    first = out.stdout.splitlines()[0]
    assert out.returncode == 2, out.stdout
    assert first.startswith("Linkcheck: COULD NOT CHECK")


def test_linkcheck_finding_first_line(tmp_path):
    repo = tmp_path / "repo"
    init_repo(repo)
    note = repo / "note.md"
    note.write_text("[x](nergens.md)\n", encoding="utf-8")
    git(repo, "add", "note.md")
    git(repo, "commit", "-qm", "note")

    out = _gate("worktree-linkcheck.sh", note, repo)
    first = out.stdout.splitlines()[0]
    assert out.returncode == 2, out.stdout
    assert first.startswith("Linkcheck: markdown link")


# --- bash-write-gates.sh: end-to-end (task 7, source 1) ---------------------

# Stubs for the gates of pack/shell and pack/python. Each reads the payload like
# the real gate, reports one fixed finding and then cites the file. The shell
# stub says COULD NOT CHECK when SHELLCHECK_BIN names a file that is not there.
SHELLCHECK_STUB = r"""#!/bin/bash
f=$(jq -r '.tool_input.file_path')
[ -f "$f" ] || exit 0
if [ -n "${SHELLCHECK_BIN:-}" ] && [ ! -e "$SHELLCHECK_BIN" ]; then
  echo "shellcheck-gate: COULD NOT CHECK — no shellcheck at $SHELLCHECK_BIN" >&2
  exit 2
fi
if grep -q 'echo "hoi"' "$f"; then
  echo "$f:3: SC2034 (stub): x appears unused." >&2
  cat "$f" >&2
  exit 2
fi
exit 0
"""
RUFF_STUB = r"""#!/bin/bash
f=$(jq -r '.tool_input.file_path')
[ -f "$f" ] || exit 0
if grep -q '^except:' "$f"; then
  echo "$f:3:1: E722 (stub) Do not use bare except" >&2
  echo "Lint-gate: fix this in the code." >&2
  exit 2
fi
exit 0
"""
GOFMT_STUB = r"""#!/bin/bash
f=$(jq -r '.tool_input.file_path')
[ -f "$f" ] || exit 0
if grep -q 'fmt.Println(' "$f"; then
  echo "gofmt: $f does not parse — fix the syntax." >&2
  echo "$f:2:8: expected ')'" >&2
  exit 2
fi
exit 0
"""
BROKEN_GO = 'package main\nfunc main() { fmt.Println( }\n'
# Clean, and exactly as long as BROKEN_SH: `"x$x"` has as many characters as
# `"hoi"`, so a `cp -p` over it does not change the size.
CLEAN_SAME_SIZE_SH = '#!/usr/bin/env bash\nf() {\n  local x=1\n  echo "x$x"\n}\nf\n'
BARE_EXCEPT_PY = "try:\n    pass\nexcept:\n    pass\n"


def heredoc(name: str, text: str) -> str:
    """A Bash command that writes `text` to `name` via a heredoc."""
    return f"cat > {name} <<'EOF'\n{text}EOF\n"


class _Session:
    """One Claude session against a tmp repo: pre, the Bash call, post.

    `env` is the test environment from *Global Constraints*; pre, the Bash call and
    post all receive it, plus any extras from the test. Pre and post
    run via the real wrapper, started directly (not via `bash`).
    """

    def __init__(
        self,
        tmp_path: Path,
        shellcheck: str = "",
        *,
        unborn: bool = False,
        language_gates: bool = True,
    ) -> None:
        self.root = tmp_path / "lighthouse"
        install_core(self.root)
        hooks = self.root / ".claude" / "hooks" / "lighthouse"
        if language_gates:
            for name, stub in (("shellcheck.sh", SHELLCHECK_STUB), ("ruff-format.sh", RUFF_STUB)):
                (hooks / name).write_text(stub, encoding="utf-8")
                (hooks / name).chmod(0o755)
        self.wrapper = hooks / "bash-write-gates.sh"
        self.repo = tmp_path / "repo"
        if unborn:
            # Only `git init`: HEAD points to `main`, but there is no commit yet.
            self.repo.mkdir(parents=True)
            git(self.repo, "init", "-q", "-b", "main")
        else:
            init_repo(self.repo)
        xdg = tmp_path / "xdg"
        xdg.mkdir()
        self.xdg = xdg
        self.env = dict(
            os.environ,
            **GIT_ENV,
            CLAUDE_PROJECT_DIR=str(self.repo),
            XDG_RUNTIME_DIR=str(xdg),
            SHELLCHECK_BIN=shellcheck,
            LIGHTHOUSE_REPO_ROOT=str(self.root),
        )
        for seam in ("LIGHTHOUSE_BASH_GATES_DEADLINE", "LIGHTHOUSE_BASH_GATES_START_NS"):
            self.env.pop(seam, None)
        self.session_id = f"sess-{uuid.uuid4().hex[:8]}"

    @property
    def state(self) -> Path:
        return self.xdg / "lighthouse-bash-gates" / self.session_id

    def payload(self, cmd: str, tool_use_id: str, cwd: Path, event: str) -> dict:
        return {
            "session_id": self.session_id,
            "tool_use_id": tool_use_id,
            "hook_event_name": event,
            "tool_name": "Bash",
            "cwd": str(cwd),
            "tool_input": {"command": cmd},
        }

    def hook(
        self, mode: str, payload: dict, env: dict | None = None
    ) -> subprocess.CompletedProcess:
        return subprocess.run(
            [str(self.wrapper), mode],
            input=json.dumps(payload),
            capture_output=True,
            text=True,
            cwd=payload["cwd"],
            env=dict(self.env, **(env or {})),
            check=False,
        )

    def hook_popen(self, mode: str, payload: dict) -> subprocess.Popen:
        proc = subprocess.Popen(
            [str(self.wrapper), mode],
            stdin=subprocess.PIPE,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
            cwd=payload["cwd"],
            env=self.env,
        )
        assert proc.stdin is not None
        proc.stdin.write(json.dumps(payload))
        proc.stdin.close()
        return proc

    def call(
        self,
        cmd: str,
        *,
        cwd: Path | None = None,
        pre: bool = True,
        post: bool = True,
        event: str = "PostToolUse",
        env: dict | None = None,
    ) -> subprocess.CompletedProcess | None:
        cwd = cwd or self.repo
        tool_use_id = f"toolu_{uuid.uuid4().hex[:12]}"
        if pre:
            pre_out = self.hook(
                "pre", self.payload(cmd, tool_use_id, cwd, "PreToolUse"), env
            )
            assert pre_out.returncode == 0, pre_out.stderr
        subprocess.run(
            ["bash", "-c", cmd],
            cwd=str(cwd),
            env=dict(self.env, **(env or {})),
            capture_output=True,
            check=False,
        )
        if not post:
            return None
        return self.hook("post", self.payload(cmd, tool_use_id, cwd, event), env)


def context(result: subprocess.CompletedProcess) -> str:
    """`hookSpecificOutput.additionalContext` from the stdout, or `""`."""
    if not result.stdout.strip():
        return ""
    return json.loads(result.stdout)["hookSpecificOutput"]["additionalContext"]


def event_of(result: subprocess.CompletedProcess) -> str:
    return json.loads(result.stdout)["hookSpecificOutput"]["hookEventName"]


@pytest.fixture
def session(tmp_path) -> _Session:
    return _Session(tmp_path)


def test_bad_sh_via_heredoc(session):
    out = session.call(heredoc("x.sh", BROKEN_SH))
    assert out.returncode == 2, (out.stdout, out.stderr)
    assert "SC2034" in out.stderr
    assert "SC2034" in context(out)
    assert "== x.sh (shellcheck.sh) ==" in out.stderr


def test_bad_go_via_heredoc(tmp_path):
    session = _Session(tmp_path)
    hooks = session.root / ".claude" / "hooks" / "lighthouse"
    (hooks / "gofmt.sh").write_text(GOFMT_STUB, encoding="utf-8")
    (hooks / "gofmt.sh").chmod(0o755)
    out = session.call(heredoc("x.go", BROKEN_GO))
    assert out.returncode == 2, (out.stdout, out.stderr)
    assert "== x.go (gofmt.sh) ==" in out.stderr
    assert "x.go:2:8: expected ')'" in out.stderr
    assert "COULD NOT CHECK" not in out.stderr


def test_clean_sh_via_heredoc(session):
    out = session.call(heredoc("x.sh", CLEAN_SH))
    assert out.returncode == 0, (out.stdout, out.stderr)
    assert out.stdout == ""


def test_dirty_before_first_call_is_baseline(session):
    (session.repo / "x.sh").write_text(BROKEN_SH, encoding="utf-8")
    time.sleep(0.05)
    out = session.call("true")
    assert out.returncode == 0, (out.stdout, out.stderr)


def test_failing_command_still_checks(session):
    out = session.call(
        heredoc("x.sh", BROKEN_SH) + "false\n", event="PostToolUseFailure"
    )
    assert out.returncode == 2, (out.stdout, out.stderr)
    assert event_of(out) == "PostToolUseFailure"
    assert "SC2034" in context(out)


def test_mv_keeps_old_mtime_but_is_checked(session, tmp_path):
    outside = tmp_path / "outside"
    outside.mkdir()
    src = outside / "x.sh"
    src.write_text(BROKEN_SH, encoding="utf-8")
    oud = time.mktime((2000, 1, 1, 0, 0, 0, 0, 0, -1))  # not `touch -d`: GNU-only
    os.utime(src, (oud, oud))
    out = session.call(f"mv {src} x.sh")
    assert out.returncode == 2, (out.stdout, out.stderr)
    assert "SC2034" in out.stderr


def test_call_without_post_is_caught_next_time(session):
    assert session.call(heredoc("x.sh", BROKEN_SH), post=False) is None
    out = session.call("true")
    assert out.returncode == 2, (out.stdout, out.stderr)
    assert "SC2034" in out.stderr


def test_same_bad_file_is_reported_once(session):
    first = session.call(heredoc("x.sh", BROKEN_SH))
    second = session.call("true")
    third = session.call("true")
    assert first.returncode == 2, (first.stdout, first.stderr)
    assert second.returncode == 0, (second.stdout, second.stderr)
    assert third.returncode == 0, (third.stdout, third.stderr)


def test_cp_p_over_dirty_same_size(session, tmp_path):
    assert len(CLEAN_SAME_SIZE_SH) == len(BROKEN_SH)
    target = session.repo / "a.sh"
    target.write_text(CLEAN_SAME_SIZE_SH, encoding="utf-8")
    src = tmp_path / "broken-src.sh"
    src.write_text(BROKEN_SH, encoding="utf-8")
    # Same mtime AND size as the baseline: only ctime gives the copy away.
    subprocess.run(["touch", "-r", str(target), str(src)], check=True)
    time.sleep(0.05)
    assert session.call("true").returncode == 0
    out = session.call(f"cp -p {src} a.sh")
    assert out.returncode == 2, (out.stdout, out.stderr)
    assert "SC2034" in out.stderr


def test_space_in_path(session):
    out = session.call(heredoc("'my x.sh'", BROKEN_SH))
    assert out.returncode == 2, (out.stdout, out.stderr)
    assert "== my x.sh (shellcheck.sh) ==" in out.stderr


def test_py_blocking_rule(tmp_path):
    s = _Session(tmp_path)
    out = s.call(heredoc("bad.py", BARE_EXCEPT_PY))
    assert out.returncode == 2, (out.stdout, out.stderr)
    assert "E722" in out.stderr
    assert "== bad.py (ruff-format.sh) ==" in out.stderr


def test_wrapper_without_python3_is_loud(tmp_path):
    s = _Session(tmp_path)
    bin_dir = tool_bin(
        tmp_path / "bin",
        ("bash", "cat", "dirname", "sed", "python3"),
        drop="python3",
    )
    out = s.call("true", env={"PATH": str(bin_dir)}, event="PostToolUseFailure")
    assert out.returncode == 2, (out.stdout, out.stderr)
    assert "python3 is missing" in out.stderr
    assert "python3 is missing" in context(out)
    assert "COULD NOT CHECK" in context(out)
    assert event_of(out) == "PostToolUseFailure"


def test_wrapper_dispatcher_crash_is_loud(tmp_path):
    s = _Session(tmp_path)
    stub_dir = tool_bin(
        tmp_path / "stubs", (), stubs={"python3": "#!/bin/sh\nexit 7\n"}
    )
    path = f"{stub_dir}{os.pathsep}{os.environ['PATH']}"
    out = s.call("true", env={"PATH": path})
    assert out.returncode == 2, (out.stdout, out.stderr)
    assert "dispatcher failed (rc=7)" in out.stderr
    assert "dispatcher failed (rc=7)" in context(out)
    assert event_of(out) == "PostToolUse"


def test_wrapper_without_the_lib_is_loud(tmp_path):
    # Without lib/bash_writes.py python3 ends with rc 2 — that must not pass as
    # a verdict. Pre stays exit 0: it would otherwise block the call.
    s = _Session(tmp_path)
    wrapper = s.wrapper
    payload = s.payload("true", "toolu_nolib", s.repo, "PostToolUseFailure")
    nolib = tmp_path / "nolib"
    nolib.mkdir()
    run = functools.partial(
        subprocess.run,
        capture_output=True,
        text=True,
        cwd=str(s.repo),
        env=dict(s.env, LIGHTHOUSE_REPO_ROOT=str(nolib)),
        check=False,
    )
    pre = run(
        [str(wrapper), "pre"],
        input=json.dumps(dict(payload, hook_event_name="PreToolUse")),
    )
    assert pre.returncode == 0, pre.stderr
    out = run([str(wrapper), "post"], input=json.dumps(payload))
    assert out.returncode == 2, (out.stdout, out.stderr)
    assert "bash_writes.py is missing" in out.stderr
    assert "COULD NOT CHECK" in context(out)
    assert event_of(out) == "PostToolUseFailure"


def test_md_dead_link(tmp_path):
    s = _Session(tmp_path)
    out = s.call(heredoc("note.md", "[x](nergens.md)\n"))
    assert out.returncode == 2, (out.stdout, out.stderr)
    assert "Linkcheck" in out.stderr
    assert "== note.md (worktree-linkcheck.sh) ==" in out.stderr


def test_gate_unchecked_is_retried(session):
    missing = {"SHELLCHECK_BIN": "/nergens/shellcheck"}
    first = session.call(heredoc("x.sh", BROKEN_SH), env=missing)
    assert first.returncode == 2, (first.stdout, first.stderr)
    assert "COULD NOT CHECK" in first.stderr
    assert "SC2034" not in first.stderr
    second = session.call("true")
    assert second.returncode == 2, (second.stdout, second.stderr)
    assert "SC2034" in second.stderr


def test_real_verdict_with_the_phrase_in_the_file(session):
    first = session.call(heredoc("x.sh", BROKEN_SH + "# COULD NOT CHECK\n"))
    assert first.returncode == 2, (first.stdout, first.stderr)
    assert "SC2034" in first.stderr
    second = session.call("true")
    assert second.returncode == 0, (second.stdout, second.stderr)


def test_late_pre_does_not_baseline_the_write(session):
    t0 = time.time_ns()
    # A coarse ctime clock can lag a fraction behind `time_ns`; without
    # this pause the file can get a ctime just before t0.
    time.sleep(0.05)
    (session.repo / "x.sh").write_text(BROKEN_SH, encoding="utf-8")
    out = session.call("true", env={"LIGHTHOUSE_BASH_GATES_START_NS": str(t0)})
    assert out.returncode == 2, (out.stdout, out.stderr)
    assert "SC2034" in out.stderr


def test_broken_index_is_loud(session):
    out = session.call("echo garbage > .git/index")
    assert out.returncode == 2, (out.stdout, out.stderr)
    assert "COULD NOT CHECK" in out.stderr
    assert "COULD NOT CHECK" in context(out)


def test_deadline_lists_unchecked_files(session):
    out = session.call(
        heredoc("late.sh", BROKEN_SH), env={"LIGHTHOUSE_BASH_GATES_DEADLINE": "0"}
    )
    assert out.returncode == 2, (out.stdout, out.stderr)
    assert "COULD NOT CHECK" in out.stderr
    assert "late.sh" in out.stderr
    assert "SC2034" not in out.stderr


def test_two_posts_report_once(session):
    cmd = heredoc("x.sh", BROKEN_SH)
    ids = ["toolu_para_a", "toolu_para_b"]
    for tool_use_id in ids:
        pre = session.hook(
            "pre", session.payload(cmd, tool_use_id, session.repo, "PreToolUse")
        )
        assert pre.returncode == 0
    subprocess.run(
        ["bash", "-c", cmd], cwd=str(session.repo), env=session.env, check=True
    )
    procs = [
        session.hook_popen(
            "post", session.payload(cmd, tool_use_id, session.repo, "PostToolUse")
        )
        for tool_use_id in ids
    ]
    results = [(proc.wait(timeout=120), proc.stderr.read()) for proc in procs]
    for proc in procs:
        proc.stdout.close()
        proc.stderr.close()
    reported = [err for _rc, err in results if "SC2034" in err]
    assert len(reported) == 1, results
    assert sorted(rc for rc, _err in results) == [0, 2], results


def test_deadline_lets_the_next_call_retry_at_once(session):
    cmd = heredoc("a.sh", BROKEN_SH) + heredoc("b.sh", BROKEN_SH)
    first = session.call(cmd, env={"LIGHTHOUSE_BASH_GATES_DEADLINE": "0"})
    assert first.returncode == 2, (first.stdout, first.stderr)
    assert "COULD NOT CHECK" in first.stderr
    assert "a.sh" in first.stderr and "b.sh" in first.stderr
    second = session.call("true")
    assert second.returncode == 2, (second.stdout, second.stderr)
    assert "== a.sh (shellcheck.sh) ==" in second.stderr
    assert "== b.sh (shellcheck.sh) ==" in second.stderr
    assert second.stderr.count("SC2034") >= 2


def test_symlinked_cwd_is_one_root(session, tmp_path):
    link = tmp_path / "link"
    link.symlink_to(session.repo)
    out = session.call(heredoc("x.sh", BROKEN_SH), cwd=link)
    assert out.returncode == 2, (out.stdout, out.stderr)
    assert out.stderr.count("== x.sh") == 1, out.stderr


def test_relative_write_from_a_subdir(session):
    sub = session.repo / "sub"
    sub.mkdir()
    out = session.call(heredoc("y.sh", BROKEN_SH), cwd=sub)
    assert out.returncode == 2, (out.stdout, out.stderr)
    assert "== sub/y.sh (shellcheck.sh) ==" in out.stderr


@pytest.mark.parametrize("via", ["cwd", "path"])
def test_call_outside_any_repo_is_silent(session, tmp_path, via):
    # A bare `.git/` without HEAD above the cwd (on the Mac: `~/.git/` with
    # only a tool cache) made a root of every directory under `~`; git said
    # "not a git repository" and post blocked with COULD NOT CHECK on an
    # `ls` outside any repo, where there is nothing to guard.
    home = tmp_path / "home"
    (home / ".git" / "workflow-audit-tools").mkdir(parents=True)
    sub = home / ".claude" / "skills"
    sub.mkdir(parents=True)
    if via == "cwd":
        out = session.call("ls", cwd=sub)
    else:
        out = session.call(f"ls {sub}")
    assert out.returncode == 0, (out.stdout, out.stderr)
    assert out.stdout == ""
    assert out.stderr == ""


# --- source 2: what was committed in the call (task 8) -----------------------


def _commit_all(repo: Path, message: str) -> None:
    """`git add -A && git commit` outside any Bash call (no pre, no post)."""
    git(repo, "add", "-A")
    git(repo, "commit", "-qm", message)


def _ledger_record(s: _Session) -> dict:
    """The ledger line of the tmp repo, straight from `ledger.json`."""
    ledger = json.loads((s.state / "ledger.json").read_text(encoding="utf-8"))
    return ledger[os.path.realpath(s.repo)]


def _no_unchecked(result: subprocess.CompletedProcess) -> bool:
    return "COULD NOT" not in result.stderr and "COULD NOT" not in context(result)


def test_write_and_commit_in_one_call(session):
    out = session.call(
        heredoc("x.sh", BROKEN_SH) + "git add x.sh && git commit -qm x\n"
    )
    assert out.returncode == 2, (out.stdout, out.stderr)
    assert "== x.sh (shellcheck.sh) ==" in out.stderr
    assert "SC2034" in out.stderr


def test_commit_without_post_is_caught_next_time(session):
    cmd = heredoc("x.sh", BROKEN_SH) + "git add x.sh && git commit -qm x\n"
    assert session.call(cmd, post=False) is None
    out = session.call("true")
    assert out.returncode == 2, (out.stdout, out.stderr)
    assert "SC2034" in out.stderr


def test_git_rm_and_git_mv_are_not_unchecked(session):
    for name in ("a.sh", "b.sh"):
        (session.repo / name).write_text(CLEAN_SH, encoding="utf-8")
    _commit_all(session.repo, "a en b")
    first = session.call("git rm -q a.sh && git commit -qm rm")
    assert first.returncode == 0, (first.stdout, first.stderr)
    assert _no_unchecked(first), (first.stdout, first.stderr)
    second = session.call("git mv b.sh c.sh")
    assert second.returncode == 0, (second.stdout, second.stderr)
    assert _no_unchecked(second), (second.stdout, second.stderr)


def _python_session(tmp_path: Path) -> _Session:
    """A session for `.py` writes (the ruff stub)."""
    return _Session(tmp_path)


def test_commit_then_mv_then_commit(tmp_path):
    s = _python_session(tmp_path)
    (s.repo / "a.py").write_text("X = 1\n", encoding="utf-8")
    _commit_all(s.repo, "a")
    first = s.call(
        "printf 'X = 2\\n' > a.py && git commit -qam wip"
        " && git mv a.py b.py && git commit -qm mv"
    )
    assert first.returncode == 0, (first.stdout, first.stderr)
    assert _no_unchecked(first), (first.stdout, first.stderr)
    second = s.call("true")
    assert second.returncode == 0, (second.stdout, second.stderr)
    assert _no_unchecked(second), (second.stdout, second.stderr)


def test_commit_then_rm(session):
    first = session.call(
        heredoc("x.sh", BROKEN_SH) + "git add x.sh && git commit -qm x && rm x.sh\n"
    )
    assert first.returncode == 0, (first.stdout, first.stderr)
    assert _no_unchecked(first), (first.stdout, first.stderr)
    second = session.call("true")
    assert second.returncode == 0, (second.stdout, second.stderr)
    assert _no_unchecked(second), (second.stdout, second.stderr)


@pytest.mark.parametrize(
    ("resolution", "rc"),
    [("opgelost\n", 0), ("[x](nergens.md)\n", 2)],
    ids=["schoon", "dode-link"],
)
def test_conflict_merge_checks_only_the_resolution(tmp_path, resolution, rc):
    s = _Session(tmp_path)
    repo = s.repo
    (repo / "c.md").write_text("basis\n", encoding="utf-8")
    _commit_all(repo, "c")
    git(repo, "checkout", "-q", "-b", "side")
    (repo / "s.sh").write_text(BROKEN_SH, encoding="utf-8")
    (repo / "t.py").write_text(BARE_EXCEPT_PY, encoding="utf-8")
    (repo / "c.md").write_text("side\n", encoding="utf-8")
    _commit_all(repo, "side")
    git(repo, "checkout", "-q", "main")
    (repo / "c.md").write_text("main\n", encoding="utf-8")
    _commit_all(repo, "main")
    past_this_second()
    assert s.call("true").returncode == 0
    out = s.call(
        f"git merge -q side; printf '{resolution}' > c.md"
        " && git add c.md && git commit -qm merge"
    )
    parents = subprocess.run(
        ["git", "-C", str(repo), "log", "-1", "--format=%P"],
        capture_output=True,
        text=True,
        check=True,
    ).stdout.split()
    assert len(parents) == 2, "de merge-commit ontbreekt"
    assert out.returncode == rc, (out.stdout, out.stderr)
    assert "s.sh" not in out.stderr
    assert "t.py" not in out.stderr
    assert _no_unchecked(out), (out.stdout, out.stderr)
    if rc:
        # The combined diff names the conflict resolution: c.md goes through the gate.
        assert "== c.md (worktree-linkcheck.sh) ==" in out.stderr


def test_pending_path_survives_a_missed_deadline(session):
    first = session.call(
        heredoc("x.sh", BROKEN_SH) + "git add x.sh && git commit -qm x\n",
        env={"LIGHTHOUSE_BASH_GATES_DEADLINE": "0"},
    )
    assert first.returncode == 2, (first.stdout, first.stderr)
    assert "COULD NOT" in first.stderr
    assert "SC2034" not in first.stderr
    second = session.call("true")
    assert second.returncode == 2, (second.stdout, second.stderr)
    assert "SC2034" in second.stderr
    third = session.call("true")
    assert third.returncode == 0, (third.stdout, third.stderr)


def test_committed_path_missing_on_disk_is_unchecked(session):
    # skip-worktree hides the deletion from `git status`: in HEAD, not on
    # disk, not deleted in source 1 — all three gates would give exit 0.
    out = session.call(
        heredoc("x.sh", BROKEN_SH)
        + "git add x.sh && git commit -qm x"
        + " && git update-index --skip-worktree x.sh && rm x.sh\n"
    )
    assert out.returncode == 2, (out.stdout, out.stderr)
    assert "COULD NOT CHECK" in out.stderr
    assert "x.sh" in out.stderr
    assert "SC2034" not in out.stderr
    assert _ledger_record(session)["pending"] == ["x.sh"]


def test_checkout_moves_head_without_checking(tmp_path):
    s = _python_session(tmp_path)
    repo = s.repo
    git(repo, "checkout", "-q", "-b", "side")
    # Both wrong, and the .py also unformatted: a dispatch would
    # rewrite it, so byte-equal proves that nothing ran.
    (repo / "t.py").write_text("x=1\n" + BARE_EXCEPT_PY, encoding="utf-8")
    (repo / "s.sh").write_text(BROKEN_SH, encoding="utf-8")
    _commit_all(repo, "side")
    git(repo, "checkout", "-q", "main")
    past_this_second()
    assert s.call("true").returncode == 0
    out = s.call("git checkout -q side")
    assert out.returncode == 0, (out.stdout, out.stderr)
    assert (repo / "t.py").read_text(encoding="utf-8") == "x=1\n" + BARE_EXCEPT_PY
    assert (repo / "s.sh").read_text(encoding="utf-8") == BROKEN_SH
    assert "without a commit" in context(out)
    assert "bash-write-gates:" in context(out)


def test_reset_soft_makes_it_dirty(session):
    # Source 1 already catches this (task 7): after `reset --soft` x.sh is dirty in the index.
    (session.repo / "x.sh").write_text(BROKEN_SH, encoding="utf-8")
    _commit_all(session.repo, "x")
    out = session.call("git reset -q --soft HEAD~1")
    assert out.returncode == 2, (out.stdout, out.stderr)
    assert "SC2034" in out.stderr


def test_first_commit_in_an_unborn_repo(tmp_path):
    s = _Session(tmp_path, unborn=True)
    first = s.call("true")
    assert first.returncode == 0, (first.stdout, first.stderr)
    assert _no_unchecked(first), (first.stdout, first.stderr)
    out = s.call(heredoc("x.sh", BROKEN_SH) + "git add x.sh && git commit -qm x\n")
    assert out.returncode == 2, (out.stdout, out.stderr)
    assert "SC2034" in out.stderr


def test_orphan_checkout_is_not_unchecked(session):
    # HEAD becomes unborn while the ledger has a sha: `git reflog` would
    # give rc 128 there, so source 2 must not even call it.
    out = session.call("git checkout -q --orphan wees")
    assert out.returncode == 0, (out.stdout, out.stderr)
    assert _no_unchecked(out), (out.stdout, out.stderr)


def test_commit_then_staged_mv_is_not_unchecked(session):
    (session.repo / "a.sh").write_text(CLEAN_SH, encoding="utf-8")
    _commit_all(session.repo, "a")
    first = session.call(
        heredoc("a.sh", CLEAN_SAME_SIZE_SH)
        + "git commit -qam wip && git mv a.sh b.sh\n"
    )
    assert first.returncode == 0, (first.stdout, first.stderr)
    assert _no_unchecked(first), (first.stdout, first.stderr)
    second = session.call("true")
    assert second.returncode == 0, (second.stdout, second.stderr)
    assert _no_unchecked(second), (second.stdout, second.stderr)
    assert "a.sh" not in _ledger_record(session)["pending"]


def test_commit_then_switch_back_is_not_silent(session):
    # HEAD ends on the ledger `head` again: the stop walk sees nothing, the
    # walk since the start of the call finds the commit on `feat`. bad.sh
    # is then not on disk and not in HEAD, but not deleted: cannot
    # be looked at, so loud. Back on `feat` it is still in `pending` and
    # goes through the gate after all.
    assert session.call("true").returncode == 0
    out = session.call(
        "git switch -q -c feat && "
        + heredoc("bad.sh", BROKEN_SH)
        + "git add bad.sh && git commit -qm x && git switch -q main\n"
    )
    assert out.returncode == 2, (out.stdout, out.stderr)
    assert "COULD NOT CHECK" in out.stderr
    assert "bad.sh (committed in " in out.stderr
    assert "COULD NOT CHECK" in context(out)
    back = session.call("git switch -q feat")
    assert back.returncode == 2, (back.stdout, back.stderr)
    assert "SC2034" in back.stderr


@pytest.mark.parametrize(
    "discard",
    [
        "git reset -q --hard HEAD~1",
        "git rm -q bad.sh && git commit -q --amend --allow-empty -m x",
        "git rebase -q --onto HEAD~1 HEAD",
    ],
    ids=["reset-hard", "amend-na-rm", "rebase-drop"],
)
def test_commit_then_discard_is_not_unchecked(session, discard):
    # The call throws away its own commit again: the reflog still knows it, but
    # no ref contains it. bad.sh is then not committed elsewhere but gone.
    assert session.call("true").returncode == 0
    out = session.call(
        heredoc("bad.sh", BROKEN_SH)
        + f"git add bad.sh && git commit -qm x && {discard}\n"
    )
    assert not (session.repo / "bad.sh").exists()
    assert out.returncode == 0, (out.stdout, out.stderr)
    assert _no_unchecked(out), (out.stdout, out.stderr)
    assert "bad.sh" not in _ledger_record(session)["pending"]


def test_commit_then_ff_merge_back_is_still_checked(session):
    # The stop walk stops at the return to `main` (sha == ledger `head`)
    # before the commit; the fast-forward brings bad.sh onto main anyway.
    assert session.call("true").returncode == 0
    out = session.call(
        "git switch -q -c tmp && "
        + heredoc("bad.sh", BROKEN_SH)
        + "git add bad.sh && git commit -qm x && git switch -q main"
        + " && git merge -q --ff-only tmp\n"
    )
    assert (session.repo / "bad.sh").exists()
    assert out.returncode == 2, (out.stdout, out.stderr)
    assert "SC2034" in out.stderr


# --- multiple roots and the fallback (task 9) -----------------------------


def _worktree(s: _Session, path: Path, *args: str) -> Path:
    """`git worktree add` outside any Bash call; returns the path."""
    git(s.repo, "worktree", "add", "-q", *args, str(path))
    return path


def _manual_call(
    s: _Session,
    cmd: str,
    between=None,
    *,
    cwd: Path | None = None,
    event: str = "PostToolUse",
) -> subprocess.CompletedProcess:
    """Pre, the Bash call, then `between()`, then post — with one `tool_use_id`."""
    cwd = cwd or s.repo
    tool_use_id = f"toolu_{uuid.uuid4().hex[:12]}"
    pre = s.hook("pre", s.payload(cmd, tool_use_id, cwd, "PreToolUse"))
    assert pre.returncode == 0, pre.stderr
    subprocess.run(
        ["bash", "-c", cmd], cwd=str(cwd), env=s.env, capture_output=True, check=False
    )
    if between is not None:
        between()
    return s.hook("post", s.payload(cmd, tool_use_id, cwd, event))


def _ledger(s: _Session) -> dict:
    return json.loads((s.state / "ledger.json").read_text(encoding="utf-8"))


def _markers(s: _Session) -> list[Path]:
    return [p for p in s.state.glob("*.json") if p.name != "ledger.json"]


def _drop_markers(s: _Session) -> None:
    markers = _markers(s)
    assert markers, "pre schreef geen marker"
    for marker in markers:
        marker.unlink()


def _drop_ledger(s: _Session) -> None:
    (s.state / "ledger.json").unlink()


def test_cd_into_a_second_worktree(session, tmp_path):
    wt2 = _worktree(session, tmp_path / "wt2")
    out = session.call(f"cd {wt2} && " + heredoc("x.sh", BROKEN_SH))
    assert out.returncode == 2, (out.stdout, out.stderr)
    assert "SC2034" in out.stderr


def test_foreign_repo_is_not_a_root(session, tmp_path):
    los = tmp_path / "los"
    init_repo(los)
    out = session.call(f"cd {los} && " + heredoc("x.sh", BROKEN_SH))
    assert out.returncode == 0, (out.stdout, out.stderr)
    assert "SC2034" not in out.stderr
    assert os.path.realpath(los) not in _ledger(session)


def test_worktree_add_then_commit_inside(session):
    cmd = (
        "git worktree add -q wt -b b && cd wt && "
        + heredoc("y.sh", BROKEN_SH)
        + "git add y.sh && git commit -qm y\n"
    )
    out = session.call(cmd)
    assert out.returncode == 2, (out.stdout, out.stderr)
    assert "SC2034" in out.stderr
    assert "fallback" not in context(out)
    wt = os.path.realpath(session.repo / "wt")
    head = subprocess.run(
        ["git", "-C", wt, "rev-parse", "HEAD"],
        capture_output=True,
        text=True,
        check=True,
    ).stdout.strip()
    assert _ledger(session)[wt]["head"] == head


def test_recreated_worktree_reflog_gap_still_walks_since_the_call(session):
    # The ledger still knows `wt` with the HEAD of the deleted worktree; the
    # new worktree has a fresh reflog without that sha (reflog gap). The
    # walk since the start of the call finds the commit anyway.
    first = session.call(
        "git worktree add -q wt -b b1 && cd wt && echo a > a.txt"
        " && git add a.txt && git commit -qm a"
    )
    assert first.returncode == 0, (first.stdout, first.stderr)
    assert session.call("true", cwd=session.repo / "wt").returncode == 0
    assert session.call("git worktree remove --force wt").returncode == 0
    assert (
        session.call("echo m > m.txt && git add m.txt && git commit -qm m").returncode
        == 0
    )
    out = session.call(
        "git worktree add -q wt -b b2 && cd wt && "
        + heredoc("y.sh", BROKEN_SH)
        + "git add y.sh && git commit -qm y\n"
    )
    assert out.returncode == 2, (out.stdout, out.stderr)
    assert "SC2034" in out.stderr
    assert "reflog gap" in context(out)


def test_relative_path_into_a_gitignored_nested_worktree(session):
    (session.repo / ".gitignore").write_text("wt/\n", encoding="utf-8")
    _commit_all(session.repo, "ignore wt")
    _worktree(session, session.repo / "wt")
    out = session.call(heredoc("wt/x.sh", BROKEN_SH))
    assert out.returncode == 2, (out.stdout, out.stderr)
    assert "SC2034" in out.stderr


def test_open_quote_with_an_absolute_path(session, tmp_path):
    wt2 = _worktree(session, tmp_path / "wt2")
    # Bash would refuse this whole line (open quote): pre and post by hand,
    # the write itself via Python.
    cmd = f"cat > {wt2}/x.sh; echo 'open"
    tool_use_id = "toolu_open_quote"
    pre = session.hook(
        "pre", session.payload(cmd, tool_use_id, session.repo, "PreToolUse")
    )
    assert pre.returncode == 0, pre.stderr
    (wt2 / "x.sh").write_text(BROKEN_SH, encoding="utf-8")
    out = session.hook(
        "post", session.payload(cmd, tool_use_id, session.repo, "PostToolUse")
    )
    assert out.returncode == 2, (out.stdout, out.stderr)
    assert "SC2034" in out.stderr


def test_marker_gone_before_post(session):
    out = _manual_call(
        session, heredoc("x.sh", BROKEN_SH), lambda: _drop_markers(session)
    )
    assert out.returncode == 2, (out.stdout, out.stderr)
    assert "SC2034" in out.stderr


def test_ledger_gone_before_post(session):
    out = _manual_call(
        session, heredoc("x.sh", BROKEN_SH), lambda: _drop_ledger(session)
    )
    assert out.returncode == 2, (out.stdout, out.stderr)
    assert "SC2034" in out.stderr
    assert "fallback" in context(out)


def test_ledger_gone_commit_in_the_call_is_found(session):
    # Source 2 via reflog time: the commit usually falls in the same second as the
    # start of pre — `since` rounds down, otherwise it dropped out.
    cmd = heredoc("x.sh", BROKEN_SH) + "git add x.sh && git commit -qm x\n"
    out = _manual_call(session, cmd, lambda: _drop_ledger(session))
    assert out.returncode == 2, (out.stdout, out.stderr)
    assert "SC2034" in out.stderr
    assert "fallback" in context(out)
    # The root is in the ledger again: the next call does not fall back.
    again = session.call("true")
    assert again.returncode == 0, (again.stdout, again.stderr)
    assert "fallback" not in context(again)


def test_note_on_exit_zero_names_the_failure_event(session):
    out = _manual_call(
        session,
        heredoc("x.sh", CLEAN_SH),
        lambda: _drop_ledger(session),
        event="PostToolUseFailure",
    )
    assert out.returncode == 0, (out.stdout, out.stderr)
    assert event_of(out) == "PostToolUseFailure"
    assert "fallback" in context(out)


def test_unreadable_ledger_falls_back_and_is_replaced(session):
    def garble() -> None:
        (session.state / "ledger.json").write_text("{kapot", encoding="utf-8")

    out = _manual_call(session, heredoc("x.sh", BROKEN_SH), garble)
    assert out.returncode == 2, (out.stdout, out.stderr)
    assert "SC2034" in out.stderr
    assert "unreadable" in context(out)
    assert "fallback" in context(out)
    head = subprocess.run(
        ["git", "-C", str(session.repo), "rev-parse", "HEAD"],
        capture_output=True,
        text=True,
        check=True,
    ).stdout.strip()
    assert _ledger_record(session)["head"] == head
    assert (session.state / "ledger.reset").exists()
    assert "replaced" in context(out)
    again = session.call("true")
    assert again.returncode == 0, (again.stdout, again.stderr)
    assert context(again) == ""


def test_replaced_ledger_does_not_let_pre_baseline_an_unwalked_root(session, tmp_path):
    # Reviewer probe: wt2 is in the ledger, gets a wrong file outside a call (a
    # background command); the ledger breaks; a call in
    # only the main repo replaces it. The next pre must then not seed wt2
    # — otherwise y.sh silently becomes baseline.
    wt2 = _worktree(session, tmp_path / "wt2")
    first = session.call(f"cd {wt2} && true")
    assert first.returncode == 0, (first.stdout, first.stderr)
    assert os.path.realpath(wt2) in _ledger(session)
    (wt2 / "y.sh").write_text(BROKEN_SH, encoding="utf-8")
    (session.state / "ledger.json").write_text("{kapot", encoding="utf-8")
    main_only = session.call("true")
    assert main_only.returncode == 0, (main_only.stdout, main_only.stderr)
    assert os.path.realpath(wt2) not in _ledger(session)
    out = session.call(f"cd {wt2} && true")
    assert out.returncode == 2, (out.stdout, out.stderr)
    assert "SC2034" in out.stderr
    assert "fallback without ledger" in context(out)


def test_no_marker_and_no_ledger_skips_source_2_with_a_note(session):
    def drop_both() -> None:
        _drop_markers(session)
        _drop_ledger(session)

    cmd = heredoc("x.sh", BROKEN_SH) + "git add x.sh && git commit -qm x\n"
    out = _manual_call(session, cmd, drop_both)
    assert out.returncode == 0, (out.stdout, out.stderr)
    assert "source 2 skipped" in context(out)
    # Without a marker the root demonstrably did not exist before pre: no fallback notice.
    assert "fallback without ledger" not in context(out)


def test_removed_worktree_is_named_not_silent(session, tmp_path):
    wt2 = _worktree(session, tmp_path / "wt2")
    cmd = (
        f"cd {wt2} && "
        + heredoc("x.sh", BROKEN_SH)
        + f"git add x.sh && git commit -qm x && cd {session.repo}"
        + f" && git worktree remove --force {wt2}\n"
    )
    out = session.call(cmd)
    assert not wt2.exists()
    assert out.returncode == 0, (out.stdout, out.stderr)
    assert f"{os.path.realpath(wt2)} no longer exists" in context(out)


def test_two_roots_with_the_same_rel_path_get_distinct_headers(session, tmp_path):
    wt2 = _worktree(session, tmp_path / "wt2")
    out = session.call(heredoc("x.sh", BROKEN_SH) + heredoc(f"{wt2}/x.sh", BROKEN_SH))
    assert out.returncode == 2, (out.stdout, out.stderr)
    for root in (session.repo, wt2):
        header = f"== {os.path.join(os.path.realpath(root), 'x.sh')} (shellcheck.sh) =="
        assert header in out.stderr, out.stderr


def test_outside_any_repo_is_silent(session, tmp_path):
    plain = tmp_path / "plain"
    plain.mkdir()
    shim = tmp_path / "shim"
    shim.mkdir()
    log = tmp_path / "git-failures.log"
    calls = tmp_path / "git-calls.log"
    real_git = subprocess.run(
        ["bash", "-c", "command -v git"], capture_output=True, text=True, check=True
    ).stdout.strip()
    (shim / "git").write_text(
        "#!/usr/bin/env bash\n"
        f'printf \'%s\\n\' "$*" >> "{calls}"\n'
        f'"{real_git}" "$@"\nrc=$?\n'
        f'[ "$rc" -eq 0 ] || printf \'%s %s\\n\' "$rc" "$*" >> "{log}"\n'
        'exit "$rc"\n',
        encoding="utf-8",
    )
    (shim / "git").chmod(0o755)
    env = {"PATH": f"{shim}{os.pathsep}{session.env['PATH']}"}
    out = session.call(heredoc("x.sh", BROKEN_SH), cwd=plain, env=env)
    assert out.returncode == 0, (out.stdout, out.stderr)
    assert out.stdout == ""
    # The shim really was on the path: pre and post called git, none of them failed.
    assert "rev-parse" in calls.read_text(encoding="utf-8")
    assert not log.exists(), log.read_text(encoding="utf-8")


# --- lighthouse: without language pack (decision 19) ------------------------------


def test_post_without_language_packs(tmp_path):
    s = _Session(tmp_path, language_gates=False)
    out = s.call(heredoc("a.py", BARE_EXCEPT_PY) + heredoc("b.sh", BROKEN_SH))
    assert out.returncode == 0, (out.stdout, out.stderr)
    assert "a.py not checked — no python gate installed" in context(out)
    assert "b.sh not checked — no shell gate installed" in context(out)


def test_pre_without_python_is_silent_post_is_loud(tmp_path):
    s = _Session(tmp_path)
    bin_dir = tool_bin(
        tmp_path / "bin", ("bash", "cat", "dirname", "sed", "git", "python3"), drop="python3"
    )
    payload = s.payload("true", "toolu_nopy", s.repo, "PreToolUse")
    pre = s.hook("pre", payload, env={"PATH": str(bin_dir)})
    assert (pre.returncode, pre.stdout, pre.stderr) == (0, "", "")
    post = s.hook("post", dict(payload, hook_event_name="PostToolUse"), env={"PATH": str(bin_dir)})
    assert post.returncode == 2
    assert "COULD NOT CHECK" in post.stderr and "python3 is missing" in context(post)


def test_hooks_registered_pre_post_and_failure() -> None:
    """Pre before the Bash call, post after a successful AND a failed call."""
    registered = json.loads((CORE / "hooks" / "hooks.json").read_text(encoding="utf-8"))
    want = {
        "PreToolUse": ("bash-write-gates.sh pre", 5),
        "PostToolUse": ("bash-write-gates.sh post", 120),
        "PostToolUseFailure": ("bash-write-gates.sh post", 120),
    }
    for event, (script, timeout) in want.items():
        entries = [
            h
            for block in registered["hooks"].get(event, [])
            if block.get("matcher") == "Bash"
            for h in block["hooks"]
            if h["command"].endswith("/" + script)
        ]
        assert [h["timeout"] for h in entries] == [timeout], (event, entries)
    assert os.access(INSTALLED_HOOKS / "bash-write-gates.sh", os.X_OK)


# --- code-language.sh after the gates of an extension (#26) -------------------

DUTCH_GO = "package main\n\n// lees het bestand en geef de regels terug\nfunc main() {}\n"


@pytest.mark.parametrize("with_key", [True, False])
def test_go_write_is_checked_for_code_language(tmp_path, with_key):
    s = _Session(tmp_path)
    hooks = s.root / ".claude" / "hooks" / "lighthouse"
    (hooks / "gofmt.sh").write_text("#!/bin/sh\nexit 0\n", encoding="utf-8")
    (hooks / "gofmt.sh").chmod(0o755)
    toml = MINIMAL_TOML
    if with_key:
        toml = toml.replace('language = "en"\n', 'language = "en"\ncode_language = "en"\n')
    (s.repo / "lighthouse.toml").write_text(toml, encoding="utf-8")
    git(s.repo, "add", "lighthouse.toml")
    git(s.repo, "commit", "-qm", "config")
    out = s.call(heredoc("main.go", DUTCH_GO))
    if with_key:
        assert out.returncode == 2, (out.stdout, out.stderr)
        assert "== main.go (code-language.sh) ==" in out.stderr
        assert "/main.go is not English" in out.stderr
    else:
        assert out.returncode == 0, (out.stdout, out.stderr)
        assert out.stdout == ""
