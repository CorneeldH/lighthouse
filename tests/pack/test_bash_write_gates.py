"""Pint het niet-gekeken-contract van de drie bestaande PostToolUse-gates
(shellcheck.sh, ruff-format.sh, worktree-linkcheck.sh), vóórdat de
bash-write-gates-dispatcher (#871) op de classificatie van hun eerste
uitvoerregel gaat bouwen.

Deze tests wijzigen geen productiecode: ze leggen bestaand gedrag vast. Elke
gate schrijft naar stderr; `_gate` voegt stdout+stderr samen
(`stderr=subprocess.STDOUT`) zodat de eerste regel van de gecombineerde
uitvoer te toetsen is, precies zoals de latere dispatcher hem zal lezen.

Vanaf taak 7 ook de end-to-end-tests van `bash-write-gates.sh`: `_Session`
draait pre, de Bash-call en post tegen een tmp-repo, via de echte wrapper.
"""

import functools
import json
import os
import re
import subprocess
import time
import uuid
from pathlib import Path

import pytest

from tests.unit._hook_testkit import (
    GIT_ENV,
    HOOKS,
    git,
    init_repo,
    past_this_second,
    tool_bin,
)

REPO = Path(__file__).resolve().parents[2]
SHELLCHECK_LIB = REPO / "scripts" / "hooks" / "lib" / "shellcheck-bin.sh"

CLEAN_SH = '#!/usr/bin/env bash\nset -euo pipefail\necho "hallo"\n'
# SC2034: `x` wordt nooit gebruikt. Warning-ernst, dus blokkeert overal.
BROKEN_SH = '#!/usr/bin/env bash\nf() {\n  local x=1\n  echo "hoi"\n}\nf\n'


def sh(dir_: Path, text: str) -> Path:
    """Schrijf `text` naar een `.sh`-bestand in `dir_` en geef het pad terug."""
    path = dir_ / "script.sh"
    path.write_text(text, encoding="utf-8")
    return path


def _gate(
    name: str,
    path: Path,
    cwd: Path,
    env: dict | None = None,
) -> subprocess.CompletedProcess:
    """Draai `.claude/hooks/<name>` met de nagemaakte payload.

    Payload: `{"tool_name": "Bash", "cwd": <cwd>, "tool_input": {"file_path":
    <absoluut pad>}}`, proces-cwd = `cwd`. stdout+stderr samengevoegd zodat de
    eerste regel van de gecombineerde uitvoer te classificeren is.
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
        SHELLCHECK_BIN="",
    )
    full_env.update(env or {})
    return subprocess.run(
        [str(HOOKS / name)],
        input=json.dumps(payload),
        cwd=str(cwd),
        env=full_env,
        stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT,
        text=True,
        check=False,
    )


def _shellcheck_pin() -> str:
    match = re.search(
        r"^SHELLCHECK_PIN=(\S+)", SHELLCHECK_LIB.read_text(), re.MULTILINE
    )
    assert match, "SHELLCHECK_PIN niet gevonden in de lib"
    return match.group(1)


def _pinned_version(bin_path: str) -> bool:
    try:
        out = subprocess.run(
            [bin_path, "--version"], capture_output=True, text=True, check=False
        ).stdout
    except OSError:
        return False
    return f"version: {_shellcheck_pin()}" in out.splitlines()


@pytest.fixture(scope="module")
def shellcheck_bin() -> str:
    """Absoluut pad naar een al-geïnstalleerde, gepinde shellcheck.

    Geen `resolve_shellcheck`: die kan downloaden of een afwijkende
    PATH-versie teruggeven. Kandidaten: de gedeelde toolcache
    (`--git-common-dir`/workflow-audit-tools/shellcheck) en `PATH`. Geen van
    beide gepind aanwezig -> skip, nooit downloaden in een unit-test.
    """
    import shutil

    common_dir = subprocess.run(
        [
            "git",
            "-C",
            str(REPO),
            "rev-parse",
            "--path-format=absolute",
            "--git-common-dir",
        ],
        capture_output=True,
        text=True,
        check=False,
    ).stdout.strip()
    candidates = []
    if common_dir:
        candidates.append(str(Path(common_dir) / "workflow-audit-tools" / "shellcheck"))
    which = shutil.which("shellcheck")
    if which:
        candidates.append(which)
    for candidate in candidates:
        if Path(candidate).is_file() and _pinned_version(candidate):
            return candidate
    pytest.skip(
        f"geen gepinde shellcheck ({_shellcheck_pin()}) gevonden onder {candidates}"
    )


# --- shellcheck.sh -----------------------------------------------------------


def test_shellcheck_unchecked_form_is_on_the_first_line(tmp_path):
    out = _gate(
        "shellcheck.sh",
        sh(tmp_path, CLEAN_SH),
        tmp_path,
        {"SHELLCHECK_BIN": "/nergens/shellcheck"},
    )
    first = out.stdout.splitlines()[0]
    assert out.returncode == 2, out.stdout
    assert first.startswith("shellcheck-gate:") and "NIET gecontroleerd" in first


def test_shellcheck_finding_does_not_start_with_the_unchecked_form(
    tmp_path, shellcheck_bin
):
    text = BROKEN_SH + "# NIET gecontroleerd\n"
    out = _gate(
        "shellcheck.sh",
        sh(tmp_path, text),
        tmp_path,
        {"SHELLCHECK_BIN": shellcheck_bin},
    )
    first = out.stdout.splitlines()[0]
    assert out.returncode == 2 and "SC2034" in out.stdout, out.stdout
    assert "NIET gecontroleerd" not in first


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
    assert first.startswith("Linkcheck: KON NIET CONTROLEREN")


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
    assert first.startswith("Linkcheck: markdown-link")


# --- ruff-format.sh ------------------------------------------------------------


def test_ruff_finding_first_line(tmp_path):
    repo = tmp_path / "repo"
    init_repo(repo)
    ruff_version = (REPO / ".ruff-version").read_text(encoding="utf-8")
    (repo / ".ruff-version").write_text(ruff_version, encoding="utf-8")

    bad = repo / "bad.py"
    bad.write_text("try:\n    pass\nexcept:\n    pass\n", encoding="utf-8")

    out = _gate("ruff-format.sh", bad, repo)
    first = out.stdout.splitlines()[0]
    assert out.returncode == 2, out.stdout
    assert "E722" in out.stdout and "Lint-gate:" in out.stdout
    # ruff-format.sh is fail-open en heeft geen "unchecked"-vorm: pass 1's eigen
    # statusregel (bv. "1 file left unchanged") staat vóór "Lint-gate:" op de
    # samengevoegde stream, dus de pin hier is "geen unchecked-vorm", niet
    # "begint met Lint-gate:".
    is_unchecked = (
        first.startswith("shellcheck-gate:") and "NIET gecontroleerd" in first
    ) or (first.startswith("Linkcheck: KON NIET CONTROLEREN"))
    assert not is_unchecked


# --- bash-write-gates.sh: end-to-end (taak 7, bron 1) ---------------------

WRAPPER = HOOKS / "bash-write-gates.sh"
# Schoon, en exact even lang als BROKEN_SH: `"x$x"` telt net zoveel tekens als
# `"hoi"`, dus een `cp -p` eroverheen verandert de grootte niet.
CLEAN_SAME_SIZE_SH = '#!/usr/bin/env bash\nf() {\n  local x=1\n  echo "x$x"\n}\nf\n'
BARE_EXCEPT_PY = "try:\n    pass\nexcept:\n    pass\n"


def heredoc(name: str, text: str) -> str:
    """Een Bash-commando dat `text` via een heredoc naar `name` schrijft."""
    return f"cat > {name} <<'EOF'\n{text}EOF\n"


class _Session:
    """Eén Claude-sessie tegen een tmp-repo: pre, de Bash-call, post.

    `env` is de testomgeving uit *Global Constraints*; pre, de Bash-call en
    post krijgen hem allemaal, plus eventuele extra's van de test. Pre en post
    lopen via de echte wrapper, direct gestart (niet via `bash`).
    """

    def __init__(
        self, tmp_path: Path, shellcheck: str = "", *, unborn: bool = False
    ) -> None:
        self.repo = tmp_path / "repo"
        if unborn:
            # Alleen `git init`: HEAD wijst naar `main`, maar er is nog geen commit.
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
        )
        for seam in ("PAIA_BASH_GATES_DEADLINE", "PAIA_BASH_GATES_START_NS"):
            self.env.pop(seam, None)
        self.session_id = f"sess-{uuid.uuid4().hex[:8]}"

    @property
    def state(self) -> Path:
        return self.xdg / "paia-bash-gates" / self.session_id

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
            [str(WRAPPER), mode],
            input=json.dumps(payload),
            capture_output=True,
            text=True,
            cwd=payload["cwd"],
            env=dict(self.env, **(env or {})),
            check=False,
        )

    def hook_popen(self, mode: str, payload: dict) -> subprocess.Popen:
        proc = subprocess.Popen(
            [str(WRAPPER), mode],
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
    """`hookSpecificOutput.additionalContext` uit de stdout, of `""`."""
    if not result.stdout.strip():
        return ""
    return json.loads(result.stdout)["hookSpecificOutput"]["additionalContext"]


def event_of(result: subprocess.CompletedProcess) -> str:
    return json.loads(result.stdout)["hookSpecificOutput"]["hookEventName"]


@pytest.fixture
def session(tmp_path, shellcheck_bin) -> _Session:
    return _Session(tmp_path, shellcheck_bin)


def test_bad_sh_via_heredoc(session):
    out = session.call(heredoc("x.sh", BROKEN_SH))
    assert out.returncode == 2, (out.stdout, out.stderr)
    assert "SC2034" in out.stderr
    assert "SC2034" in context(out)
    assert "== x.sh (shellcheck.sh) ==" in out.stderr


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
    oud = time.mktime((2000, 1, 1, 0, 0, 0, 0, 0, -1))  # niet `touch -d`: GNU-only
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
    # Zelfde mtime én grootte als de nullijn: alleen ctime verraadt de copy.
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
    ruff_version = (REPO / ".ruff-version").read_text(encoding="utf-8")
    (s.repo / ".ruff-version").write_text(ruff_version, encoding="utf-8")
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
    assert "python3 ontbreekt" in out.stderr
    assert "python3 ontbreekt" in context(out)
    assert "KON NIET CONTROLEREN" in context(out)
    assert event_of(out) == "PostToolUseFailure"


def test_wrapper_dispatcher_crash_is_loud(tmp_path):
    s = _Session(tmp_path)
    stub_dir = tool_bin(
        tmp_path / "stubs", (), stubs={"python3": "#!/bin/sh\nexit 7\n"}
    )
    path = f"{stub_dir}{os.pathsep}{os.environ['PATH']}"
    out = s.call("true", env={"PATH": path})
    assert out.returncode == 2, (out.stdout, out.stderr)
    assert "dispatcher faalde (rc=7)" in out.stderr
    assert "dispatcher faalde (rc=7)" in context(out)
    assert event_of(out) == "PostToolUse"


def test_wrapper_without_the_lib_is_loud(tmp_path):
    # Zonder lib/bash_writes.py eindigt python3 met rc 2 — dat mag niet als
    # een oordeel doorgaan. Pre blijft exit 0: die blokkeert anders de call.
    s = _Session(tmp_path)
    hooks = tmp_path / "hooks"
    (hooks / "lib").mkdir(parents=True)
    wrapper = hooks / "bash-write-gates.sh"
    wrapper.write_bytes(WRAPPER.read_bytes())
    wrapper.chmod(0o755)
    payload = s.payload("true", "toolu_nolib", s.repo, "PostToolUseFailure")
    run = functools.partial(
        subprocess.run,
        capture_output=True,
        text=True,
        cwd=str(s.repo),
        env=s.env,
        check=False,
    )
    pre = run(
        [str(wrapper), "pre"],
        input=json.dumps(dict(payload, hook_event_name="PreToolUse")),
    )
    assert pre.returncode == 0, pre.stderr
    out = run([str(wrapper), "post"], input=json.dumps(payload))
    assert out.returncode == 2, (out.stdout, out.stderr)
    assert "lib/bash_writes.py ontbreekt" in out.stderr
    assert "KON NIET CONTROLEREN" in context(out)
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
    assert "KON NIET CONTROLEREN" in first.stderr
    assert "SC2034" not in first.stderr
    second = session.call("true")
    assert second.returncode == 2, (second.stdout, second.stderr)
    assert "SC2034" in second.stderr


def test_real_verdict_with_the_phrase_in_the_file(session):
    first = session.call(heredoc("x.sh", BROKEN_SH + "# NIET gecontroleerd\n"))
    assert first.returncode == 2, (first.stdout, first.stderr)
    assert "SC2034" in first.stderr
    second = session.call("true")
    assert second.returncode == 0, (second.stdout, second.stderr)


def test_late_pre_does_not_baseline_the_write(session):
    t0 = time.time_ns()
    # Een grove ctime-klok kan een fractie achterlopen op `time_ns`; zonder
    # deze pauze kan het bestand een ctime net vóór t0 krijgen.
    time.sleep(0.05)
    (session.repo / "x.sh").write_text(BROKEN_SH, encoding="utf-8")
    out = session.call("true", env={"PAIA_BASH_GATES_START_NS": str(t0)})
    assert out.returncode == 2, (out.stdout, out.stderr)
    assert "SC2034" in out.stderr


def test_broken_index_is_loud(session):
    out = session.call("echo garbage > .git/index")
    assert out.returncode == 2, (out.stdout, out.stderr)
    assert "KON NIET CONTROLEREN" in out.stderr
    assert "KON NIET CONTROLEREN" in context(out)


def test_deadline_lists_unchecked_files(session):
    out = session.call(
        heredoc("late.sh", BROKEN_SH), env={"PAIA_BASH_GATES_DEADLINE": "0"}
    )
    assert out.returncode == 2, (out.stdout, out.stderr)
    assert "KON NIET CONTROLEREN" in out.stderr
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
    first = session.call(cmd, env={"PAIA_BASH_GATES_DEADLINE": "0"})
    assert first.returncode == 2, (first.stdout, first.stderr)
    assert "KON NIET CONTROLEREN" in first.stderr
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
    # Een losse `.git/` zonder HEAD boven de cwd (op de Mac: `~/.git/` met
    # alleen een toolcache) maakte van elke map onder `~` een wortel; git zei
    # "not a git repository" en post blokkeerde met KON NIET CONTROLEREN op een
    # `ls` buiten elke repo, waar niets te bewaken valt.
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


# --- bron 2: wat in de call gecommit werd (taak 8) -----------------------


def _commit_all(repo: Path, message: str) -> None:
    """`git add -A && git commit` buiten elke Bash-call om (geen pre, geen post)."""
    git(repo, "add", "-A")
    git(repo, "commit", "-qm", message)


def _ledger_record(s: _Session) -> dict:
    """De ledgerregel van de tmp-repo, rechtstreeks uit `ledger.json`."""
    ledger = json.loads((s.state / "ledger.json").read_text(encoding="utf-8"))
    return ledger[os.path.realpath(s.repo)]


def _no_unchecked(result: subprocess.CompletedProcess) -> bool:
    return "KON NIET" not in result.stderr and "KON NIET" not in context(result)


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
    """Een sessie zonder shellcheck, met de gepinde ruff in de tmp-repo."""
    s = _Session(tmp_path)
    ruff_version = (REPO / ".ruff-version").read_text(encoding="utf-8")
    (s.repo / ".ruff-version").write_text(ruff_version, encoding="utf-8")
    return s


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
        # De combined diff noemt de conflictoplossing: c.md gaat door de gate.
        assert "== c.md (worktree-linkcheck.sh) ==" in out.stderr


def test_pending_path_survives_a_missed_deadline(session):
    first = session.call(
        heredoc("x.sh", BROKEN_SH) + "git add x.sh && git commit -qm x\n",
        env={"PAIA_BASH_GATES_DEADLINE": "0"},
    )
    assert first.returncode == 2, (first.stdout, first.stderr)
    assert "KON NIET" in first.stderr
    assert "SC2034" not in first.stderr
    second = session.call("true")
    assert second.returncode == 2, (second.stdout, second.stderr)
    assert "SC2034" in second.stderr
    third = session.call("true")
    assert third.returncode == 0, (third.stdout, third.stderr)


def test_committed_path_missing_on_disk_is_unchecked(session):
    # skip-worktree verbergt de verwijdering voor `git status`: in HEAD, niet op
    # schijf, niet verwijderd in bron 1 — alle drie de gates zouden exit 0 geven.
    out = session.call(
        heredoc("x.sh", BROKEN_SH)
        + "git add x.sh && git commit -qm x"
        + " && git update-index --skip-worktree x.sh && rm x.sh\n"
    )
    assert out.returncode == 2, (out.stdout, out.stderr)
    assert "KON NIET CONTROLEREN" in out.stderr
    assert "x.sh" in out.stderr
    assert "SC2034" not in out.stderr
    assert _ledger_record(session)["pending"] == ["x.sh"]


def test_checkout_moves_head_without_checking(tmp_path):
    s = _python_session(tmp_path)
    repo = s.repo
    _commit_all(repo, "ruff-pin")
    git(repo, "checkout", "-q", "-b", "side")
    # Beide fout, en de .py ook ongeformatteerd: een dispatch zou hem
    # herschrijven, dus byte-gelijk bewijst dat er niets draaide.
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
    assert "zonder commit" in context(out)
    assert "bash-write-gates:" in context(out)


def test_reset_soft_makes_it_dirty(session):
    # Bron 1 vangt dit al (taak 7): na `reset --soft` staat x.sh vuil in de index.
    (session.repo / "x.sh").write_text(BROKEN_SH, encoding="utf-8")
    _commit_all(session.repo, "x")
    out = session.call("git reset -q --soft HEAD~1")
    assert out.returncode == 2, (out.stdout, out.stderr)
    assert "SC2034" in out.stderr


def test_first_commit_in_an_unborn_repo(tmp_path, shellcheck_bin):
    s = _Session(tmp_path, shellcheck_bin, unborn=True)
    first = s.call("true")
    assert first.returncode == 0, (first.stdout, first.stderr)
    assert _no_unchecked(first), (first.stdout, first.stderr)
    out = s.call(heredoc("x.sh", BROKEN_SH) + "git add x.sh && git commit -qm x\n")
    assert out.returncode == 2, (out.stdout, out.stderr)
    assert "SC2034" in out.stderr


def test_orphan_checkout_is_not_unchecked(session):
    # HEAD wordt ongeboren terwijl de ledger een sha heeft: `git reflog` zou
    # daar rc 128 geven, dus bron 2 moet hem niet eens aanroepen.
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
    # HEAD eindigt weer op de ledger-`head`: de stop-wandeling ziet niets, de
    # wandeling sinds de start van de call vindt de commit op `feat`. bad.sh
    # staat dan niet op schijf en niet in HEAD, maar is niet verwijderd: niet
    # te bekijken, dus luid. Terug op `feat` staat hij nog in `pending` en
    # gaat hij alsnog door de gate.
    assert session.call("true").returncode == 0
    out = session.call(
        "git switch -q -c feat && "
        + heredoc("bad.sh", BROKEN_SH)
        + "git add bad.sh && git commit -qm x && git switch -q main\n"
    )
    assert out.returncode == 2, (out.stdout, out.stderr)
    assert "KON NIET CONTROLEREN" in out.stderr
    assert "bad.sh (gecommit in " in out.stderr
    assert "KON NIET CONTROLEREN" in context(out)
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
    # De call gooit zijn eigen commit weer weg: de reflog kent hem nog, maar
    # geen ref bevat hem. bad.sh is dan niet elders gecommit maar weg.
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
    # De stop-wandeling stopt bij de terugkeer naar `main` (sha == ledger-`head`)
    # vóór de commit; de fast-forward brengt bad.sh toch op main.
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


# --- meerdere wortels en de terugval (taak 9) -----------------------------


def _worktree(s: _Session, path: Path, *args: str) -> Path:
    """`git worktree add` buiten elke Bash-call om; geeft het pad terug."""
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
    """Pre, de Bash-call, dan `between()`, dan post — met één `tool_use_id`."""
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
    assert "terugval" not in context(out)
    wt = os.path.realpath(session.repo / "wt")
    head = subprocess.run(
        ["git", "-C", wt, "rev-parse", "HEAD"],
        capture_output=True,
        text=True,
        check=True,
    ).stdout.strip()
    assert _ledger(session)[wt]["head"] == head


def test_recreated_worktree_reflog_gap_still_walks_since_the_call(session):
    # De ledger kent `wt` nog met de HEAD van de verwijderde worktree; de
    # nieuwe worktree heeft een verse reflog zonder die sha (reflog-gat). De
    # wandeling sinds de start van de call vindt de commit toch.
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
    assert "reflog-gat" in context(out)


def test_relative_path_into_a_gitignored_nested_worktree(session):
    (session.repo / ".gitignore").write_text("wt/\n", encoding="utf-8")
    _commit_all(session.repo, "ignore wt")
    _worktree(session, session.repo / "wt")
    out = session.call(heredoc("wt/x.sh", BROKEN_SH))
    assert out.returncode == 2, (out.stdout, out.stderr)
    assert "SC2034" in out.stderr


def test_open_quote_with_an_absolute_path(session, tmp_path):
    wt2 = _worktree(session, tmp_path / "wt2")
    # Bash zou deze hele regel weigeren (open quote): pre en post met de hand,
    # de schrijfactie zelf via Python.
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
    assert "terugval" in context(out)


def test_ledger_gone_commit_in_the_call_is_found(session):
    # Bron 2 via reflogtijd: de commit valt meestal in dezelfde seconde als de
    # start van pre — `since` rondt naar beneden af, anders viel hij weg.
    cmd = heredoc("x.sh", BROKEN_SH) + "git add x.sh && git commit -qm x\n"
    out = _manual_call(session, cmd, lambda: _drop_ledger(session))
    assert out.returncode == 2, (out.stdout, out.stderr)
    assert "SC2034" in out.stderr
    assert "terugval" in context(out)
    # De wortel staat weer in de ledger: de volgende call valt niet terug.
    again = session.call("true")
    assert again.returncode == 0, (again.stdout, again.stderr)
    assert "terugval" not in context(again)


def test_note_on_exit_zero_names_the_failure_event(session):
    out = _manual_call(
        session,
        heredoc("x.sh", CLEAN_SH),
        lambda: _drop_ledger(session),
        event="PostToolUseFailure",
    )
    assert out.returncode == 0, (out.stdout, out.stderr)
    assert event_of(out) == "PostToolUseFailure"
    assert "terugval" in context(out)


def test_unreadable_ledger_falls_back_and_is_replaced(session):
    def garble() -> None:
        (session.state / "ledger.json").write_text("{kapot", encoding="utf-8")

    out = _manual_call(session, heredoc("x.sh", BROKEN_SH), garble)
    assert out.returncode == 2, (out.stdout, out.stderr)
    assert "SC2034" in out.stderr
    assert "onleesbaar" in context(out)
    assert "terugval" in context(out)
    head = subprocess.run(
        ["git", "-C", str(session.repo), "rev-parse", "HEAD"],
        capture_output=True,
        text=True,
        check=True,
    ).stdout.strip()
    assert _ledger_record(session)["head"] == head
    assert (session.state / "ledger.reset").exists()
    assert "vervangen" in context(out)
    again = session.call("true")
    assert again.returncode == 0, (again.stdout, again.stderr)
    assert context(again) == ""


def test_replaced_ledger_does_not_let_pre_baseline_an_unwalked_root(session, tmp_path):
    # Reviewer-probe: wt2 staat in de ledger, krijgt buiten een call om (een
    # achtergrondcommando) een fout bestand; de ledger gaat kapot; een call in
    # alleen de hoofdrepo vervangt hem. De volgende pre mag wt2 dan niet
    # seeden — anders wordt y.sh stil nullijn.
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
    assert "terugval zonder ledger" in context(out)


def test_no_marker_and_no_ledger_skips_source_2_with_a_note(session):
    def drop_both() -> None:
        _drop_markers(session)
        _drop_ledger(session)

    cmd = heredoc("x.sh", BROKEN_SH) + "git add x.sh && git commit -qm x\n"
    out = _manual_call(session, cmd, drop_both)
    assert out.returncode == 0, (out.stdout, out.stderr)
    assert "bron 2 overgeslagen" in context(out)
    # Zonder marker bestond de wortel niet aantoonbaar vóór pre: geen terugvalmelding.
    assert "terugval zonder ledger" not in context(out)


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
    assert f"{os.path.realpath(wt2)} bestaat niet meer" in context(out)


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
    # De shim zat echt op het pad: pre en post riepen git aan, geen enkele faalde.
    assert "rev-parse" in calls.read_text(encoding="utf-8")
    assert not log.exists(), log.read_text(encoding="utf-8")
