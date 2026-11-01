"""The pre-push secret scan in `tools/hooks/` (#194).

The pushes are real: a fixture repo with bare remotes, the hook installed in its
`.git/hooks/pre-push`, and the test reads what reached the remote. Only the cases
git cannot reach without a network (a non-local remote URL) call the script
directly with the ref lines git would hand it.

gitleaks is needed for every scan. Without it the scan tests skip, unless
`LIGHTHOUSE_REQUIRE_GITLEAKS` is set: the `secret-scan` job in CI sets it, so a
runner without gitleaks fails there instead of skipping.
"""

import json
import os
import re
import shutil
import subprocess
from pathlib import Path

import pytest

from tests.pack._testkit import GIT_ENV, git, init_repo, tool_bin

ROOT = Path(__file__).resolve().parents[1]
SCAN = ROOT / "tools" / "hooks" / "pre-push-secret-scan.sh"
INSTALL = ROOT / "tools" / "hooks" / "install.sh"
ZERO = "0" * 40

# A GitHub PAT that gitleaks' github-pat rule matches. Built at runtime, so neither
# this file nor its .pyc carries a match for the repository's own scan: the compiler
# folds `"a" + "b"` into one constant, a join it leaves alone.
FAKE_PAT = "".join(("ghp_", "Zq9XmW2pL7vR4tY8uN3bK6cJ1hF5dG0sA2eQ"))

needs_gitleaks = pytest.mark.skipif(
    shutil.which("gitleaks") is None and not os.environ.get("LIGHTHOUSE_REQUIRE_GITLEAKS"),
    reason="gitleaks is not installed",
)
needs_entire = pytest.mark.skipif(shutil.which("entire") is None, reason="entire is not installed")


def _env(**extra: str) -> dict[str, str]:
    return dict(os.environ, **GIT_ENV, **extra)


def _out(repo: Path, *args: str) -> str:
    return subprocess.run(
        ["git", "-C", str(repo), *args], check=True, capture_output=True, text=True, env=_env()
    ).stdout.strip()


def _commit(repo: Path, name: str, text: str) -> str:
    (repo / name).write_text(text, encoding="utf-8")
    git(repo, "add", name)
    git(repo, "commit", "-qm", f"edit {name}")
    return _out(repo, "rev-parse", "HEAD")


def _push(repo: Path, remote: str, *refspecs: str) -> subprocess.CompletedProcess:
    return subprocess.run(
        ["git", "-C", str(repo), "push", remote, *refspecs],
        capture_output=True,
        text=True,
        env=_env(),
    )


def _remote_refs(bare: Path) -> list[str]:
    return _out(bare, "for-each-ref", "--format=%(refname)").splitlines()


def _settings(repo: Path, strategy: dict, local: dict | None = None) -> None:
    (repo / ".entire").mkdir(exist_ok=True)
    body = {"enabled": True, "strategy_options": strategy}
    (repo / ".entire" / "settings.json").write_text(json.dumps(body), encoding="utf-8")
    if local is not None:
        (repo / ".entire" / "settings.local.json").write_text(
            json.dumps({"strategy_options": local}), encoding="utf-8"
        )


@pytest.fixture
def fx(tmp_path: Path) -> dict[str, Path]:
    """A repo on branch `work`, a bare `origin` holding it and an empty bare `ckpt`,
    entire configured the way lighthouse is (no automatic push, `ckpt` as the
    checkpoint remote), and the scan as the repo's pre-push hook."""
    repo, origin, ckpt = tmp_path / "repo", tmp_path / "origin.git", tmp_path / "ckpt.git"
    init_repo(repo, branch="work")
    for bare in (origin, ckpt):
        subprocess.run(["git", "init", "-q", "--bare", str(bare)], check=True)
    git(repo, "remote", "add", "origin", str(origin))
    git(repo, "remote", "add", "ckpt", str(ckpt))
    git(repo, "push", "-q", "origin", "work")
    _settings(repo, {"push_sessions": False}, local={"checkpoint_push_remote": "ckpt"})
    hook = repo / ".git" / "hooks" / "pre-push"
    hook.write_text(f'#!/bin/sh\nexec bash "{SCAN}" "$@"\n', encoding="utf-8")
    hook.chmod(0o755)
    return {"repo": repo, "origin": origin, "ckpt": ckpt}


def _checkpoint_ref(repo: Path, name: str, text: str) -> str:
    """A checkpoint ref the way entire's git-refs backend writes one: an orphan commit
    under refs/entire/checkpoints/ holding a transcript."""
    blob = subprocess.run(
        ["git", "-C", str(repo), "hash-object", "-w", "--stdin"],
        input=text, check=True, capture_output=True, text=True, env=_env(),
    ).stdout.strip()
    tree = subprocess.run(
        ["git", "-C", str(repo), "mktree"],
        input=f"100644 blob {blob}\tfull.jsonl\n", check=True, capture_output=True, text=True, env=_env(),
    ).stdout.strip()
    sha = _out(repo, "commit-tree", tree, "-m", "checkpoint")
    ref = f"refs/entire/checkpoints/{name[-2:]}/{name}"
    git(repo, "update-ref", ref, sha)
    return ref


def _run_scan(repo: Path, remote: str, url: str, lines: str, path: str | None = None) -> subprocess.CompletedProcess:
    env = _env()
    if path is not None:
        env["PATH"] = path
    return subprocess.run(
        ["bash", str(SCAN), remote, url], input=lines, cwd=repo, capture_output=True, text=True, env=env
    )


@needs_gitleaks
def test_clean_push_passes(fx):
    sha = _commit(fx["repo"], "a.txt", "nothing to see\n")
    result = _push(fx["repo"], "origin", "work")
    assert result.returncode == 0, result.stderr
    assert _out(fx["origin"], "rev-parse", "refs/heads/work") == sha


@needs_gitleaks
def test_secret_in_the_tip_of_a_new_branch_is_blocked(fx):
    git(fx["repo"], "checkout", "-qb", "leak")
    _commit(fx["repo"], "env.txt", f"GITHUB_PAT={FAKE_PAT}\n")
    result = _push(fx["repo"], "origin", "leak")
    assert result.returncode != 0
    assert "BLOCKED" in result.stderr
    assert "refs/heads/leak" not in _remote_refs(fx["origin"])


@needs_gitleaks
def test_secret_added_and_removed_again_before_the_tip_is_blocked(fx):
    _commit(fx["repo"], "env.txt", f"GITHUB_PAT={FAKE_PAT}\n")
    _commit(fx["repo"], "env.txt", "GITHUB_PAT=\n")
    before = _out(fx["origin"], "rev-parse", "refs/heads/work")
    result = _push(fx["repo"], "origin", "work")
    assert result.returncode != 0
    assert "BLOCKED" in result.stderr
    assert _out(fx["origin"], "rev-parse", "refs/heads/work") == before


@needs_gitleaks
def test_secret_already_on_the_remote_is_not_scanned_again(fx):
    # The range is what the remote lacks: an old leak there is history's problem,
    # not this push's, and must not block every later push.
    _commit(fx["repo"], "env.txt", f"GITHUB_PAT={FAKE_PAT}\n")
    git(fx["repo"], "push", "-q", "--no-verify", "origin", "work")
    _commit(fx["repo"], "env.txt", "GITHUB_PAT=\n")
    result = _push(fx["repo"], "origin", "work")
    assert result.returncode == 0, result.stderr


@needs_gitleaks
def test_checkpoint_ref_to_another_remote_than_the_checkpoint_remote_is_blocked(fx):
    ref = _checkpoint_ref(fx["repo"], "01AAAAAAAAAAAAAAAAAAAAAAAA", '{"text": "clean"}\n')
    result = _push(fx["repo"], "origin", f"{ref}:{ref}")
    assert result.returncode != 0
    assert "checkpoint remote" in result.stderr
    assert ref not in _remote_refs(fx["origin"])


@needs_gitleaks
def test_shadow_branch_to_another_remote_is_blocked(fx):
    git(fx["repo"], "branch", "entire/abc1234-e3b0c4")
    result = _push(fx["repo"], "origin", "entire/abc1234-e3b0c4")
    assert result.returncode != 0
    assert "refs/heads/entire/abc1234-e3b0c4" not in _remote_refs(fx["origin"])


@needs_gitleaks
def test_clean_checkpoint_ref_reaches_the_checkpoint_remote(fx):
    ref = _checkpoint_ref(fx["repo"], "01BBBBBBBBBBBBBBBBBBBBBBBB", '{"text": "clean"}\n')
    result = _push(fx["repo"], "ckpt", f"{ref}:{ref}")
    assert result.returncode == 0, result.stderr
    assert ref in _remote_refs(fx["ckpt"])


@needs_gitleaks
def test_checkpoint_ref_with_a_secret_is_blocked_on_the_checkpoint_remote(fx):
    clean = _checkpoint_ref(fx["repo"], "01CCCCCCCCCCCCCCCCCCCCCCCC", '{"text": "clean"}\n')
    leak = _checkpoint_ref(fx["repo"], "01DDDDDDDDDDDDDDDDDDDDDDDD", f'{{"text": "GITHUB_PAT={FAKE_PAT}"}}\n')
    result = _push(fx["repo"], "ckpt", "refs/entire/checkpoints/*:refs/entire/checkpoints/*")
    assert result.returncode != 0
    assert "BLOCKED" in result.stderr
    refs = _remote_refs(fx["ckpt"])
    assert clean not in refs and leak not in refs


def test_entire_pushing_checkpoints_itself_is_refused(fx):
    # entire pushes its own refs with `git push --no-verify`, past this hook (#194).
    _settings(fx["repo"], {"push_sessions": True}, local={"checkpoint_push_remote": "ckpt"})
    result = _push(fx["repo"], "origin", "work")
    assert result.returncode != 0
    assert "push_sessions" in result.stderr


def test_checkpoint_remote_from_settings_matches_its_github_url(fx):
    _settings(fx["repo"], {"push_sessions": False, "checkpoint_remote": {"provider": "github", "repo": "Acme/ckpt"}})
    ref = _checkpoint_ref(fx["repo"], "01EEEEEEEEEEEEEEEEEEEEEEEE", '{"text": "clean"}\n')
    sha = _out(fx["repo"], "rev-parse", ref)
    line = f"{ref} {sha} {ref} {ZERO}\n"
    no_gitleaks = str(tool_bin(fx["repo"].parent / "bin", ("git", "bash", "python3", "cat", "dirname")))
    # With gitleaks absent the scan refuses after the ref check, so the message tells
    # the two rules apart: the ref check passes for the checkpoint repo only.
    other = _run_scan(fx["repo"], "pub", "https://github.com/Acme/widgets.git", line, path=no_gitleaks)
    assert other.returncode != 0 and "checkpoint remote" in other.stderr
    for url in ("https://github.com/Acme/ckpt.git", "git@github.com:Acme/ckpt.git", "https://github.com/Acme/ckpt"):
        own = _run_scan(fx["repo"], "c", url, line, path=no_gitleaks)
        assert own.returncode != 0 and "gitleaks is not installed" in own.stderr, (url, own.stderr)


def test_without_gitleaks_a_real_remote_is_refused_and_a_local_path_allowed(fx):
    sha = _out(fx["repo"], "rev-parse", "HEAD")
    line = f"refs/heads/work {sha} refs/heads/work {ZERO}\n"
    no_gitleaks = str(tool_bin(fx["repo"].parent / "bin", ("git", "bash", "python3", "cat", "dirname")))
    real = _run_scan(fx["repo"], "origin", "https://github.com/Acme/widgets.git", line, path=no_gitleaks)
    assert real.returncode != 0
    assert "gitleaks is not installed" in real.stderr
    local = _run_scan(fx["repo"], "origin", str(fx["origin"]), line, path=no_gitleaks)
    assert local.returncode == 0, local.stderr


def test_a_ref_deletion_needs_no_scan(fx):
    no_gitleaks = str(tool_bin(fx["repo"].parent / "bin", ("git", "bash", "python3", "cat", "dirname")))
    line = f"(delete) {ZERO} refs/heads/old 1111111111111111111111111111111111111111\n"
    result = _run_scan(fx["repo"], "origin", "https://github.com/Acme/widgets.git", line, path=no_gitleaks)
    assert result.returncode == 0, result.stderr


def _ci_scan_command() -> str:
    ci = (ROOT / ".github" / "workflows" / "ci.yml").read_text(encoding="utf-8")
    runs = re.findall(r"^\s*run: (gitleaks git .*?)\s*$", ci, re.MULTILINE)
    assert len(runs) == 1, runs
    return runs[0]


@needs_gitleaks
def test_ci_step_is_red_on_a_secret_inside_the_pr_range(tmp_path):
    """The command of CI's `secret-scan` step, run as CI runs it: BASE..HEAD of a PR
    whose middle commit adds a secret that its last commit removes."""
    repo = tmp_path / "repo"
    init_repo(repo, branch="work")
    shutil.copy(ROOT / ".gitleaks.toml", repo / ".gitleaks.toml")
    base = _commit(repo, ".gitleaks.toml", (ROOT / ".gitleaks.toml").read_text(encoding="utf-8"))

    def scan(head: str) -> subprocess.CompletedProcess:
        return subprocess.run(
            ["bash", "-c", _ci_scan_command()], cwd=repo, capture_output=True, text=True,
            env=_env(BASE=base, HEAD=head),
        )

    clean = _commit(repo, "a.txt", "nothing to see\n")
    assert scan(clean).returncode == 0
    _commit(repo, "env.txt", f"GITHUB_PAT={FAKE_PAT}\n")
    head = _commit(repo, "env.txt", "GITHUB_PAT=\n")
    assert scan(head).returncode == 1


ENTIRE_HOOK = """#!/bin/sh
# Entire CLI hooks
if command -v entire >/dev/null 2>&1; then entire hooks git pre-push "$1"; else :; fi
"""


def _install(repo: Path) -> subprocess.CompletedProcess:
    return subprocess.run(["bash", str(INSTALL)], cwd=repo, capture_output=True, text=True, env=_env())


def test_install_puts_the_scan_before_entire_and_is_idempotent(tmp_path):
    repo = tmp_path / "repo"
    init_repo(repo, branch="work")
    hook = repo / ".git" / "hooks" / "pre-push"
    hook.write_text(ENTIRE_HOOK, encoding="utf-8")
    assert _install(repo).returncode == 0
    first = hook.read_text(encoding="utf-8")
    assert first.startswith("#!/bin/sh\n")
    assert first.index("pre-push-secret-scan.sh") < first.index("entire hooks git pre-push")
    assert _install(repo).returncode == 0
    assert hook.read_text(encoding="utf-8") == first
    assert os.access(hook, os.X_OK)


def test_install_without_a_hook_writes_one(tmp_path):
    repo = tmp_path / "repo"
    init_repo(repo, branch="work")
    assert _install(repo).returncode == 0
    hook = repo / ".git" / "hooks" / "pre-push"
    assert "pre-push-secret-scan.sh" in hook.read_text(encoding="utf-8")
    assert os.access(hook, os.X_OK)


def test_installed_hook_refuses_when_no_scan_script_is_found(tmp_path):
    # A checkout without tools/hooks/ (and a main checkout without it) must not
    # push unscanned.
    repo, origin = tmp_path / "repo", tmp_path / "origin.git"
    init_repo(repo, branch="work")
    subprocess.run(["git", "init", "-q", "--bare", str(origin)], check=True)
    git(repo, "remote", "add", "origin", str(origin))
    assert _install(repo).returncode == 0
    result = _push(repo, "origin", "work")
    assert result.returncode != 0
    assert "refusing to push unscanned" in result.stderr
    assert _remote_refs(origin) == []


@needs_gitleaks
@needs_entire
def test_entire_checkpoints_leave_only_through_the_scan(tmp_path):
    """The measured path of #194, end to end with the real entire and the installed hook.

    entire pushes checkpoints itself, from its own pre-push line, with
    `git push --no-verify`. The scan therefore sits above that line and refuses
    any push while entire may push (`push_sessions` on). With `push_sessions`
    off, the only way out is a plain `git push`, which the scan sees.
    """
    repo, origin, ckpt = tmp_path / "repo", tmp_path / "origin.git", tmp_path / "ckpt.git"
    init_repo(repo, branch="work")
    for bare in (origin, ckpt):
        subprocess.run(["git", "init", "-q", "--bare", str(bare)], check=True)
    git(repo, "remote", "add", "origin", str(origin))
    git(repo, "remote", "add", "ckpt", str(ckpt))
    git(repo, "push", "-q", "origin", "work")
    (repo / "tools" / "hooks").mkdir(parents=True)
    shutil.copy(SCAN, repo / "tools" / "hooks" / SCAN.name)
    shutil.copy(ROOT / ".gitleaks.toml", repo / ".gitleaks.toml")
    git(repo, "add", "tools", ".gitleaks.toml")
    git(repo, "commit", "-qm", "scan")

    env = _env()
    enable = subprocess.run(
        ["entire", "enable", "--agent", "claude-code", "--telemetry=false", "--checkpoint-push-remote", "ckpt"],
        cwd=repo, capture_output=True, text=True, env=env, stdin=subprocess.DEVNULL,
    )
    assert enable.returncode == 0, enable.stdout + enable.stderr
    assert _install(repo).returncode == 0

    session = "11111111-2222-3333-4444-555555555555"
    transcript = tmp_path / "transcript.jsonl"
    transcript.write_text(
        json.dumps({"type": "user", "uuid": "u1", "sessionId": session, "timestamp": "2026-11-01T10:00:00Z",
                    "message": {"role": "user", "content": "read the env file"}}) + "\n"
        + json.dumps({"type": "assistant", "uuid": "a1", "parentUuid": "u1", "sessionId": session,
                      "timestamp": "2026-11-01T10:00:01Z",
                      "message": {"role": "assistant", "model": "x",
                                  "content": [{"type": "text", "text": f"GITHUB_PAT={FAKE_PAT}"}]}}) + "\n",
        encoding="utf-8",
    )
    payload = json.dumps({"session_id": session, "transcript_path": str(transcript), "cwd": str(repo),
                          "hook_event_name": "X", "prompt": "read the env file"})
    for event in ("session-start", "user-prompt-submit"):
        subprocess.run(["entire", "hooks", "claude-code", event], input=payload, cwd=repo,
                       capture_output=True, text=True, env=env, check=True)
    (repo / "b.txt").write_text("change\n", encoding="utf-8")
    subprocess.run(["entire", "hooks", "claude-code", "stop"], input=payload, cwd=repo,
                   capture_output=True, text=True, env=env, check=True)
    git(repo, "add", "b.txt")
    subprocess.run(["git", "-C", str(repo), "commit", "-qm", "agent work"], check=True, env=env)
    checkpoints = _out(repo, "for-each-ref", "--format=%(refname)", "refs/entire/").splitlines()
    assert checkpoints, "entire wrote no checkpoint ref"

    def push(*args: str) -> subprocess.CompletedProcess:
        return subprocess.run(["git", "-C", str(repo), "push", *args], capture_output=True, text=True, env=env)

    # push_sessions on (entire's default): entire would push past the hook, so the
    # scan above it refuses the push before entire's line runs.
    refused = push("ckpt", "work")
    assert refused.returncode != 0 and "push_sessions" in refused.stderr
    assert not any(r.startswith("refs/entire/") for r in _remote_refs(ckpt))

    # push_sessions off, as in lighthouse: a manual push of the checkpoint refs goes
    # through the hook. entire redacted this transcript's PAT itself, so it passes.
    settings = json.loads((repo / ".entire" / "settings.json").read_text(encoding="utf-8"))
    settings.setdefault("strategy_options", {})["push_sessions"] = False
    (repo / ".entire" / "settings.json").write_text(json.dumps(settings), encoding="utf-8")
    passed = push("ckpt", "refs/entire/checkpoints/*:refs/entire/checkpoints/*")
    assert passed.returncode == 0, passed.stderr
    assert set(checkpoints) <= set(_remote_refs(ckpt))

    # entire calls its redaction best effort. A checkpoint where it missed one, in the
    # git-refs layout, is blocked on that same path.
    leak = _checkpoint_ref(repo, "01ZZZZZZZZZZZZZZZZZZZZZZZZ", f'{{"text": "GITHUB_PAT={FAKE_PAT}"}}\n')
    blocked = push("ckpt", "refs/entire/checkpoints/*:refs/entire/checkpoints/*")
    assert blocked.returncode != 0 and "BLOCKED" in blocked.stderr
    assert leak not in _remote_refs(ckpt)


@needs_gitleaks
def test_gitleaks_config_allows_beta_header_name(tmp_path):
    line = '{"anthropic-beta":"oauth-2025-04-20","interleaved-thinking-2025-05-14"}\n'
    clean = tmp_path / "t.jsonl"
    clean.write_text(line, encoding="utf-8")
    dirty = tmp_path / "u.jsonl"
    dirty.write_text(line + FAKE_PAT + "\n", encoding="utf-8")

    def scan(path: Path) -> int:
        return subprocess.run(
            ["gitleaks", "dir", str(path), "--config", str(ROOT / ".gitleaks.toml"),
             "--no-banner", "--exit-code", "1"],
            capture_output=True, text=True,
        ).returncode

    assert scan(clean) == 0
    assert scan(dirty) == 1
