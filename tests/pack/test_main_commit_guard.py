"""pack/core main-commit-guard.sh, as installed: does it block `git commit`/`git push` on `main`?

The hook is bash and reads its payload as JSON on stdin. These tests run it
as a subprocess with real temporary paths, so that the contract (exit code +
stderr/stdout) is tested and not a reimplementation of it.
"""

import json
import os
import shutil
import subprocess

import pytest
from pathlib import Path

from tests.pack._testkit import GUARD, bash_payload, init_repo, install_core, run_hook


def _run(
    hook: Path,
    payload: dict,
    cwd: Path,
    env: dict | None = None,
) -> subprocess.CompletedProcess:
    return subprocess.run(
        [str(hook)],
        input=json.dumps(payload),
        capture_output=True,
        text=True,
        cwd=str(cwd),
        env=dict(os.environ, **(env or {})),
        check=False,
    )


_GIT_ENV = {
    "GIT_AUTHOR_NAME": "t",
    "GIT_AUTHOR_EMAIL": "t@example.com",
    "GIT_COMMITTER_NAME": "t",
    "GIT_COMMITTER_EMAIL": "t@example.com",
}


def _git(repo: Path, *args: str) -> None:
    subprocess.run(
        ["git", "-C", str(repo), *args],
        check=True,
        capture_output=True,
        text=True,
        env=dict(os.environ, **_GIT_ENV),
    )


def _init_repo(path: Path, branch: str = "main") -> None:
    """A real git repo with one commit, so that the hook sees real plumbing."""
    path.mkdir(parents=True, exist_ok=True)
    _git(path, "init", "-q", "-b", branch)
    (path / "README.md").write_text("# repo\n", encoding="utf-8")
    _git(path, "add", "README.md")
    _git(path, "commit", "-qm", "init")


COMMIT_GUARD = GUARD


def _bash(cmd: str, cwd: Path) -> dict:
    return {"tool_name": "Bash", "cwd": str(cwd), "tool_input": {"command": cmd}}


def test_commit_guard_blocks_commit_on_main(tmp_path):
    repo = tmp_path / "repo"
    _init_repo(repo)

    result = _run(COMMIT_GUARD, _bash('git commit -m "iets"', repo), cwd=repo)

    assert result.returncode == 2, result.stdout
    assert "main" in result.stderr


def test_commit_guard_allows_commit_on_feature_branch(tmp_path):
    repo = tmp_path / "repo"
    _init_repo(repo)
    _git(repo, "checkout", "-q", "-b", "issue-1-iets")

    result = _run(COMMIT_GUARD, _bash('git commit -m "iets"', repo), cwd=repo)

    assert result.returncode == 0, result.stderr


def test_commit_guard_blocks_commit_targeting_main_checkout_via_dash_c(tmp_path):
    """The documented escape: worktree cwd, `git -C <main-tree> commit`."""
    repo = tmp_path / "repo"
    _init_repo(repo)
    wt = repo / ".claude" / "worktrees" / "issue-1-iets"
    _git(repo, "worktree", "add", "-q", "-b", "issue-1-iets", str(wt))

    result = _run(COMMIT_GUARD, _bash(f'git -C {repo} commit -m "iets"', wt), cwd=wt)

    assert result.returncode == 2, result.stdout


def test_commit_guard_follows_cd_in_a_chain(tmp_path):
    repo = tmp_path / "repo"
    _init_repo(repo)
    wt = repo / ".claude" / "worktrees" / "issue-1-iets"
    _git(repo, "worktree", "add", "-q", "-b", "issue-1-iets", str(wt))

    result = _run(COMMIT_GUARD, _bash(f'cd {repo} && git commit -m "iets"', wt), cwd=wt)

    assert result.returncode == 2, result.stdout


def test_commit_guard_blocks_push_to_main_from_feature_branch(tmp_path):
    """`git push origin HEAD:main` bypasses the branch check; the refspec does not."""
    repo = tmp_path / "repo"
    _init_repo(repo)
    _git(repo, "checkout", "-q", "-b", "issue-1-iets")

    result = _run(COMMIT_GUARD, _bash("git push origin HEAD:main", repo), cwd=repo)

    assert result.returncode == 2, result.stdout


def test_commit_guard_ignores_heredoc_content(tmp_path):
    """The false positive of pre-commit-tests.sh must not occur here."""
    repo = tmp_path / "repo"
    _init_repo(repo)
    cmd = "cat > plan.md <<'XEOF'\ngit commit -m \"voorbeeld\"\ngit push origin main\nXEOF"

    result = _run(COMMIT_GUARD, _bash(cmd, repo), cwd=repo)

    assert result.returncode == 0, result.stderr


def test_commit_guard_ignores_commit_word_in_a_message(tmp_path):
    repo = tmp_path / "repo"
    _init_repo(repo)
    _git(repo, "checkout", "-q", "-b", "issue-1-iets")

    result = _run(COMMIT_GUARD, _bash('echo "git commit -m main"', repo), cwd=repo)

    assert result.returncode == 0, result.stderr


def test_commit_guard_sentinel_opens_the_gate(tmp_path):
    repo = tmp_path / "repo"
    _init_repo(repo)
    (repo / ".claude").mkdir(parents=True, exist_ok=True)
    (repo / ".claude" / "allow-main").touch()

    result = _run(COMMIT_GUARD, _bash('git commit -m "bewust op main"', repo), cwd=repo)

    assert result.returncode == 0, result.stderr


def test_commit_guard_fails_loud_without_tooling(tmp_path):
    repo = tmp_path / "repo"
    _init_repo(repo)
    empty_bin = tmp_path / "leeg-bin"
    empty_bin.mkdir()

    result = _run(
        COMMIT_GUARD,
        _bash('git commit -m "iets"', repo),
        cwd=repo,
        env={"PATH": str(empty_bin)},
    )

    assert result.returncode == 2
    assert "COULD NOT CHECK" in result.stderr


# --- toolkit for the sharper cases below -------------------

_REAL_TOOLS = ("jq", "python3", "git", "cat", "mktemp", "rm", "sh")


def _tool_bin(
    path: Path, *, drop: str = "", stubs: dict[str, str] | None = None
) -> Path:
    """A PATH directory with the real tooling, minus one, plus any stubs.

    That way each limb of the preflight can be tested to fail loudly, that very limb —
    an empty PATH covers all three at once and so none separately (M11).
    """
    path.mkdir(parents=True, exist_ok=True)
    for tool in _REAL_TOOLS:
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


def test_commit_guard_fails_loud_without_jq(tmp_path):
    """M11: the jq limb separately, not hidden behind an empty PATH."""
    repo = tmp_path / "repo"
    _init_repo(repo)

    result = _run(
        COMMIT_GUARD,
        _bash('git commit -m "iets"', repo),
        cwd=repo,
        env={"PATH": str(_tool_bin(tmp_path / "zonder-jq", drop="jq"))},
    )

    assert result.returncode == 2, result.stdout
    assert "COULD NOT CHECK" in result.stderr
    # Precisely this limb: the preflight itself, not the safety net behind it.
    assert "missing tooling" in result.stderr
    assert "jq" in result.stderr


def test_commit_guard_fails_loud_without_python3(tmp_path):
    """M11: de python3-limb apart."""
    repo = tmp_path / "repo"
    _init_repo(repo)

    result = _run(
        COMMIT_GUARD,
        _bash('git commit -m "iets"', repo),
        cwd=repo,
        env={"PATH": str(_tool_bin(tmp_path / "zonder-py", drop="python3"))},
    )

    assert result.returncode == 2, result.stdout
    assert "COULD NOT CHECK" in result.stderr
    assert "missing tooling" in result.stderr
    assert "python3" in result.stderr


def test_commit_guard_fails_loud_without_git(tmp_path):
    """C3: without git the hook cannot see the branch — that is no green light."""
    repo = tmp_path / "repo"
    _init_repo(repo)

    result = _run(
        COMMIT_GUARD,
        _bash('git commit -m "iets"', repo),
        cwd=repo,
        env={"PATH": str(_tool_bin(tmp_path / "zonder-git", drop="git"))},
    )

    assert result.returncode == 2, result.stdout
    assert "COULD NOT CHECK" in result.stderr
    assert "missing tooling" in result.stderr
    assert "git" in result.stderr


def test_commit_guard_blocks_when_rev_parse_breaks(tmp_path):
    """C3/I8: a git that breaks down is not a 'no repo' — so block.

    The preflight is satisfied (git exists), the parser found a commit, but
    `rev-parse` gives an unknown error. Silently letting it through would be the gap here.
    """
    repo = tmp_path / "repo"
    _init_repo(repo)
    broken_git = _tool_bin(
        tmp_path / "kapotte-git",
        stubs={"git": "#!/bin/sh\necho 'fatal: kapot object store' >&2\nexit 1\n"},
    )

    result = _run(
        COMMIT_GUARD,
        _bash('git commit -m "iets"', repo),
        cwd=repo,
        env={"PATH": str(broken_git)},
    )

    assert result.returncode == 2, result.stdout
    assert "COULD NOT CHECK" in result.stderr


def test_commit_guard_passes_when_target_is_simply_not_a_repo(tmp_path):
    """The counterpart of the previous test: 'no repo' is a real outcome."""
    los = tmp_path / "geen-repo"
    los.mkdir()

    result = _run(COMMIT_GUARD, _bash('git commit -m "iets"', los), cwd=los)

    assert result.returncode == 0, result.stderr


def test_commit_guard_blocks_when_the_parser_crashes(tmp_path):
    """I8: a parser that does not end with 0 or 3 must not become a silent pass."""
    repo = tmp_path / "repo"
    _init_repo(repo)
    # Only the parser crashes; the config read before it uses the real python3.
    real = shutil.which("python3")
    broken_parser = _tool_bin(
        tmp_path / "kapotte-python",
        stubs={
            "python3": "#!/bin/sh\ncase \"$1\" in *git_invocations.py) "
            "echo 'Traceback: stuk' >&2; exit 1;; esac\n"
            f'exec {real} "$@"\n'
        },
    )

    result = _run(
        COMMIT_GUARD,
        _bash('git commit -m "iets"', repo),
        cwd=repo,
        env={"PATH": str(broken_parser)},
    )

    assert result.returncode == 2, result.stdout
    assert "COULD NOT CHECK" in result.stderr
    assert "parser" in result.stderr


def test_commit_guard_blocks_unparseable_command(tmp_path):
    """I9: open quote = no conclusion. Not being able to look is no negative result."""
    repo = tmp_path / "repo"
    _init_repo(repo)

    result = _run(
        COMMIT_GUARD,
        _bash("echo 'unbalanced && git commit -m y", repo),
        cwd=repo,
    )

    assert result.returncode == 2, result.stdout
    assert "COULD NOT CHECK" in result.stderr


def test_commit_guard_passes_unparseable_input_without_a_commit(tmp_path):
    """The two-step fallback: 'cannot be parsed' is not 'cannot look'.

    If the raw text contains nowhere `git` WITH `commit` or `push`, there is
    nothing to protect and nothing to report here. Without that step, one
    imbalance outside any repo locked up ALL following commands — a lockout
    that gains the norm nothing. The counterweight is in
    test_commit_guard_blocks_unparseable_command: if there CAN be a commit in it,
    it blocks without exception.
    """
    repo = tmp_path / "repo"
    _init_repo(repo)
    los = tmp_path / "geen-repo"
    los.mkdir()

    for cmd, cwd in (
        ("echo 'unbalanced", los),
        ("echo 'unbalanced", repo),
        ('ls -la "open', repo),
        ("awk '{print $1}", repo),
        # Both steps separately: 'push' without `git` is no push, and `git`
        # without 'commit'/'push' is nothing to guard.
        ("echo 'we push this door de brievenbus", repo),
        ("git status && echo 'unbalanced", repo),
    ):
        result = _run(COMMIT_GUARD, _bash(cmd, cwd), cwd=cwd)
        assert result.returncode == 0, f"{cmd!r} werd geblokkeerd: {result.stderr}"


def test_commit_guard_reads_ansi_c_quoting_with_an_escaped_apostrophe(tmp_path):
    """F4: in `$'...'` a backslash-escaped apostrophe does not close the string.

    The tokenizer read `$'it\\'s'` as an ordinary quote, closed it at the escaped
    apostrophe and left the last one open — 'cannot be parsed', so a block.

    The rc-0 cases alone would also stay green if the tokenizer still
    stumbles, because the two-step fallback catches them (no `git` in the text).
    What the fix really pins down is the last case: THERE a commit has to be read
    after the ANSI-C string, and it is on a feature branch, so
    letting it through is possible only if the command is really parsed.
    """
    los = tmp_path / "geen-repo"
    los.mkdir()
    repo = tmp_path / "repo"
    _init_repo(repo)

    assert _run(COMMIT_GUARD, _bash("echo $'it\\'s'", los), cwd=los).returncode == 0
    assert _run(COMMIT_GUARD, _bash("echo $'don\\'t'", repo), cwd=repo).returncode == 0

    _git(repo, "checkout", "-q", "-b", "issue-1-iets")
    doorgeparsed = _run(
        COMMIT_GUARD, _bash("echo $'don\\'t' && git commit -m x", repo), cwd=repo
    )
    assert doorgeparsed.returncode == 0, doorgeparsed.stderr


def test_commit_guard_blocks_on_invalid_json_payload(tmp_path):
    """Unreadable payload: no command, so no check either — and so no pass."""
    repo = tmp_path / "repo"
    _init_repo(repo)

    result = subprocess.run(
        [str(COMMIT_GUARD)],
        input="dit is geen json",
        capture_output=True,
        text=True,
        cwd=str(repo),
        check=False,
    )

    assert result.returncode == 2, result.stdout
    assert "COULD NOT CHECK" in result.stderr


def test_commit_guard_never_writes_to_stdout(tmp_path):
    """Contract: blocking goes via exit 2 + stderr, never via a systemMessage."""
    repo = tmp_path / "repo"
    _init_repo(repo)

    result = _run(COMMIT_GUARD, _bash('git commit -m "iets"', repo), cwd=repo)

    assert result.returncode == 2
    assert result.stdout == ""


# --- C1: newline is a command separator -----------------------------------


def test_commit_guard_sees_a_commit_on_the_second_line(tmp_path):
    """C1: Claude Code routinely sends multi-line Bash. Dominant form."""
    repo = tmp_path / "repo"
    _init_repo(repo)

    result = _run(
        COMMIT_GUARD, _bash('git add -A\ngit commit -m "iets"', repo), cwd=repo
    )

    assert result.returncode == 2, result.stdout


def test_commit_guard_sees_a_commit_after_a_non_git_line(tmp_path):
    """Line 1 begins with a non-git word; that must not cover line 2."""
    repo = tmp_path / "repo"
    _init_repo(repo)

    result = _run(
        COMMIT_GUARD, _bash('echo hallo\ngit commit -m "iets"', repo), cwd=repo
    )

    assert result.returncode == 2, result.stdout


def test_commit_guard_sees_a_push_to_main_on_a_later_line(tmp_path):
    repo = tmp_path / "repo"
    _init_repo(repo)
    _git(repo, "checkout", "-q", "-b", "issue-1-iets")

    result = _run(
        COMMIT_GUARD, _bash("echo hallo\ngit push origin HEAD:main", repo), cwd=repo
    )

    assert result.returncode == 2, result.stdout


def test_commit_guard_follows_a_line_continuation(tmp_path):
    """`git add -A && \\<newline>git commit` is one command, not two halves."""
    repo = tmp_path / "repo"
    _init_repo(repo)

    result = _run(
        COMMIT_GUARD, _bash('git add -A && \\\ngit commit -m "iets"', repo), cwd=repo
    )

    assert result.returncode == 2, result.stdout


def test_commit_guard_stays_silent_on_a_harmless_multiline_command(tmp_path):
    """Counterweight to C1: multi-line must not suddenly block everything."""
    repo = tmp_path / "repo"
    _init_repo(repo)

    result = _run(
        COMMIT_GUARD, _bash("git status\ngit log --oneline -1\nls -la", repo), cwd=repo
    )

    assert result.returncode == 0, result.stderr


# --- C2: paths with ~ and variables ------------------------------------------


def test_commit_guard_expands_tilde_in_dash_c(tmp_path):
    """C2: `~/repositories/personal_ai_agent` is exactly how CLAUDE.md spells it."""
    repo = tmp_path / "repo"
    _init_repo(repo)
    elders = tmp_path / "elders"
    elders.mkdir()

    result = _run(
        COMMIT_GUARD,
        _bash('git -C ~/repo commit -m "iets"', elders),
        cwd=elders,
        env={"HOME": str(tmp_path)},
    )

    assert result.returncode == 2, result.stdout


def test_commit_guard_expands_home_variable_in_cd(tmp_path):
    repo = tmp_path / "repo"
    _init_repo(repo)
    elders = tmp_path / "elders"
    elders.mkdir()

    result = _run(
        COMMIT_GUARD,
        _bash('cd "$HOME/repo" && git commit -m "iets"', elders),
        cwd=elders,
        env={"HOME": str(tmp_path)},
    )

    assert result.returncode == 2, result.stdout


def test_commit_guard_blocks_when_a_path_variable_is_unknown(tmp_path):
    """C2: an unknown variable must not silently resolve to something harmless."""
    repo = tmp_path / "repo"
    _init_repo(repo)
    _git(repo, "checkout", "-q", "-b", "issue-1-iets")

    result = _run(
        COMMIT_GUARD, _bash('git -C "$HOOFDBOOM" commit -m "iets"', repo), cwd=repo
    )

    assert result.returncode == 2, result.stdout
    assert "COULD NOT CHECK" in result.stderr


def test_commit_guard_unresolvable_path_still_honours_the_sentinel(tmp_path):
    """The escape hatch must also work when the path cannot be traced."""
    repo = tmp_path / "repo"
    _init_repo(repo)
    (repo / ".claude").mkdir(parents=True, exist_ok=True)
    (repo / ".claude" / "allow-main").touch()

    result = _run(
        COMMIT_GUARD, _bash('git -C "$HOOFDBOOM" commit -m "iets"', repo), cwd=repo
    )

    assert result.returncode == 0, result.stderr


# --- I4/I5: wrappers, absolute paden, subshells -----------------------------


def test_commit_guard_recognises_git_by_basename_and_through_env(tmp_path):
    repo = tmp_path / "repo"
    _init_repo(repo)
    git_bin = shutil.which("git")
    assert git_bin

    for cmd in (
        f'{git_bin} commit -m "iets"',
        'env git commit -m "iets"',
        '/usr/bin/env git commit -m "iets"',
        'env GIT_PAGER=cat git commit -m "iets"',
    ):
        result = _run(COMMIT_GUARD, _bash(cmd, repo), cwd=repo)
        assert result.returncode == 2, f"{cmd!r} kwam er langs: {result.stdout}"


def test_commit_guard_looks_inside_subshells_and_substitutions(tmp_path):
    repo = tmp_path / "repo"
    _init_repo(repo)
    wt = repo / ".claude" / "worktrees" / "issue-1-iets"
    _git(repo, "worktree", "add", "-q", "-b", "issue-1-iets", str(wt))

    for cmd in (
        f'(cd {repo} && git commit -m "iets")',
        f'{{ cd {repo}; git commit -m "iets"; }}',
        'echo "$(git -C %s commit -m iets)"' % repo,
        f"echo `git -C {repo} commit -m iets`",
    ):
        result = _run(COMMIT_GUARD, _bash(cmd, wt), cwd=wt)
        assert result.returncode == 2, f"{cmd!r} kwam er langs: {result.stdout}"


# --- I6: push forms that land on main --------------------------------------


def test_commit_guard_blocks_push_all_when_a_local_main_exists(tmp_path):
    repo = tmp_path / "repo"
    _init_repo(repo)
    _git(repo, "checkout", "-q", "-b", "issue-1-iets")

    for cmd in ("git push --all origin", "git push --mirror origin"):
        result = _run(COMMIT_GUARD, _bash(cmd, repo), cwd=repo)
        assert result.returncode == 2, f"{cmd!r} kwam er langs: {result.stdout}"


def test_commit_guard_allows_push_all_without_a_local_main(tmp_path):
    """Counterweight: without a local main `--all` pushes nothing to main."""
    repo = tmp_path / "repo"
    _init_repo(repo, branch="issue-1-iets")

    result = _run(COMMIT_GUARD, _bash("git push --all origin", repo), cwd=repo)

    assert result.returncode == 0, result.stderr


def test_commit_guard_blocks_forced_and_full_ref_pushes_to_main(tmp_path):
    repo = tmp_path / "repo"
    _init_repo(repo)
    _git(repo, "checkout", "-q", "-b", "issue-1-iets")

    for cmd in (
        "git push origin +main",
        "git push origin +HEAD:refs/heads/main",
        "git push -u origin main",
    ):
        result = _run(COMMIT_GUARD, _bash(cmd, repo), cwd=repo)
        assert result.returncode == 2, f"{cmd!r} kwam er langs: {result.stdout}"


def test_commit_guard_allows_a_push_to_a_main_prefixed_branch(tmp_path):
    """`main-fix` is not `main` — the refspec really is taken apart."""
    repo = tmp_path / "repo"
    _init_repo(repo)
    _git(repo, "checkout", "-q", "-b", "issue-1-iets")

    result = _run(COMMIT_GUARD, _bash("git push origin HEAD:main-fix", repo), cwd=repo)

    assert result.returncode == 0, result.stderr


# --- I7/M10: heredocs -------------------------------------------------------


def test_commit_guard_does_not_treat_a_shift_or_string_as_a_heredoc(tmp_path):
    """I7: `<<` in a string or in `$(( ))` must not swallow the rest."""
    repo = tmp_path / "repo"
    _init_repo(repo)

    for cmd in (
        'echo "a << b"\ngit commit -m "iets"',
        'echo $((1 << 2))\ngit commit -m "iets"',
    ):
        result = _run(COMMIT_GUARD, _bash(cmd, repo), cwd=repo)
        assert result.returncode == 2, f"{cmd!r} kwam er langs: {result.stdout}"


def test_commit_guard_matches_a_hyphenated_heredoc_delimiter(tmp_path):
    """I7: `<<'EOF-1'` must be matched completely, otherwise it never ends."""
    repo = tmp_path / "repo"
    _init_repo(repo)
    cmd = "cat > plan.md <<'EOF-1'\ngit commit -m voorbeeld\nEOF-1\ngit commit -m echt"

    result = _run(COMMIT_GUARD, _bash(cmd, repo), cwd=repo)

    # The body is skipped, the line AFTER the terminator is not.
    assert result.returncode == 2, result.stdout


def test_commit_guard_strips_a_heredoc_body_containing_an_operator(tmp_path):
    """M10: pins the stripping itself, not the 'does not begin with git' safety net."""
    repo = tmp_path / "repo"
    _init_repo(repo)
    cmd = "cat > plan.md <<'XEOF'\nblah && git commit -m voorbeeld\nXEOF"

    result = _run(COMMIT_GUARD, _bash(cmd, repo), cwd=repo)

    assert result.returncode == 0, result.stderr


def test_commit_guard_ignores_a_tab_stripped_heredoc(tmp_path):
    repo = tmp_path / "repo"
    _init_repo(repo)
    cmd = 'cat > plan.md <<-EOF\n\tgit commit -m "voorbeeld"\n\tEOF'

    result = _run(COMMIT_GUARD, _bash(cmd, repo), cwd=repo)

    assert result.returncode == 0, result.stderr


# --- N1/N2: `#` comments --------------------------------------------------


def test_commit_guard_ignores_a_hash_comment(tmp_path):
    """N1: a stray apostrophe in a comment blocked EVERY command.

    The tokenizer did not know `#`, walked the line looking for a closing
    quote and called 'cannot be parsed' — outside any repo, without git in the
    command. That is the lockout that the I9 fix accidentally made.
    """
    los = tmp_path / "geen-repo"
    los.mkdir()

    for cmd in (
        "ls -la # dat 's genoeg",
        "uv run pytest tests/unit -q  # don't run the slow ones",
        'echo hoi # " losse dubbele quote',
    ):
        result = _run(COMMIT_GUARD, _bash(cmd, los), cwd=los)
        assert result.returncode == 0, f"{cmd!r} werd geblokkeerd: {result.stderr}"


def test_commit_guard_only_treats_a_hash_at_word_start_as_comment(tmp_path):
    """Counterweight to N1: not EVERY `#` opens a comment.

    `foo#bar` and `$#` are in the middle of a word, `'a # b'` is in a quote —
    the hook may cut nothing away there, otherwise command text disappears.

    F1: the rc-0 half BELOW does NOT see that difference (it gives 0 whether the line
    is truncated or not). What `word_start` really pins down is the rc-2 half:
    if a `#` in the middle of a word truncates the line, the commit behind it
    disappears into the discarded remainder and a commit on main silently
    slips past. That is fail-open, so that is what has to be pinned here.
    """
    repo = tmp_path / "repo"
    _init_repo(repo)

    # In a quote or in the middle of a word: no comment, so the commit
    # behind it is simply seen.
    for cmd in (
        'echo "a # b" && git commit -m x',
        "echo $# && git commit -m x",
        "echo foo#bar && git commit -m x",
        "curl https://x.dev/a#b && git commit -m x",
    ):
        blocked = _run(COMMIT_GUARD, _bash(cmd, repo), cwd=repo)
        assert blocked.returncode == 2, f"{cmd!r} kwam er langs: {blocked.stdout}"

    _git(repo, "checkout", "-q", "-b", "issue-1-iets")
    for cmd in ("echo foo#bar", "set -- a b; echo $#", "echo 'a # b'"):
        result = _run(COMMIT_GUARD, _bash(cmd, repo), cwd=repo)
        assert result.returncode == 0, f"{cmd!r} werd geblokkeerd: {result.stderr}"


def test_commit_guard_does_not_arm_a_heredoc_from_a_comment(tmp_path):
    """N2: the same blind spot, the other way round.

    `<<` in a comment armed the heredoc skip, after which the commit on the
    next line was swallowed as 'body' and main was simply reached.
    """
    repo = tmp_path / "repo"
    _init_repo(repo)

    result = _run(
        COMMIT_GUARD, _bash("echo x # zie << EOF\ngit commit -m x", repo), cwd=repo
    )

    assert result.returncode == 2, result.stdout


# --- N3: a sub-shell does not pass on its cwd -----------------------------


def test_commit_guard_restores_the_directory_after_a_subshell(tmp_path):
    """N3: `(cd elsewhere)` and `$(cd elsewhere)` apply only INSIDE the brackets.

    Bash restores the cwd, the hook did not — so an ordinary commit on the
    feature branch was blocked because the `cd` had lingered.
    """
    repo = tmp_path / "repo"
    _init_repo(repo)
    wt = repo / ".claude" / "worktrees" / "issue-1-iets"
    _git(repo, "worktree", "add", "-q", "-b", "issue-1-iets", str(wt))

    for cmd in (
        f"(cd {repo} && git rev-parse HEAD)\ngit add -A\ngit commit -m x",
        f'SHA=$(cd {repo} && git rev-parse HEAD)\ngit commit -m "ref $SHA"',
        f"`cd {repo} && git rev-parse HEAD`\ngit commit -m x",
    ):
        result = _run(COMMIT_GUARD, _bash(cmd, wt), cwd=wt)
        assert result.returncode == 0, f"{cmd!r} werd geblokkeerd: {result.stderr}"


# --- N4: a push destination that cannot be traced -----------------------


def test_commit_guard_blocks_an_unresolvable_push_destination(tmp_path):
    """N4: `git push origin $BR` MAY be main — and that cannot be seen.

    Deliberate trade-off of the plan owner: this also blocks an innocent
    `git push origin "$BRANCH"`. The sentinel is the hatch, not an exception
    in the code.
    """
    repo = tmp_path / "repo"
    _init_repo(repo)
    _git(repo, "checkout", "-q", "-b", "issue-1-iets")

    for cmd in ('git push origin "$(echo main)"', "git push origin $BR"):
        result = _run(COMMIT_GUARD, _bash(cmd, repo), cwd=repo)
        assert result.returncode == 2, f"{cmd!r} kwam er langs: {result.stdout}"
        assert "COULD NOT CHECK" in result.stderr


def test_commit_guard_ignores_an_unresolvable_remote(tmp_path):
    """F2: the first positional argument is the REMOTE, not the destination.

    `git push "$REMOTE" issue-1-something` demonstrably does not go to main — the
    branch is literally there. Blocking it anyway fell outside the trade-off of N4
    and made a block of every scripted push.
    """
    repo = tmp_path / "repo"
    _init_repo(repo)
    _git(repo, "checkout", "-q", "-b", "issue-1-iets")

    for cmd in ('git push "$REMOTE" issue-1-iets', "git push -u $REMOTE issue-1-iets"):
        result = _run(COMMIT_GUARD, _bash(cmd, repo), cwd=repo)
        assert result.returncode == 0, f"{cmd!r} werd geblokkeerd: {result.stderr}"

    # Counterweight: an untraceable DESTINATION keeps blocking (N4), and a
    # literal main behind an untraceable remote just as well.
    for cmd in ('git push origin "issue-$NR"', "git push $REMOTE main"):
        result = _run(COMMIT_GUARD, _bash(cmd, repo), cwd=repo)
        assert result.returncode == 2, f"{cmd!r} kwam er langs: {result.stdout}"


def test_commit_guard_unresolvable_push_honours_the_sentinel(tmp_path):
    """The hatch must open here too, otherwise the trade-off of N4 is a trap."""
    repo = tmp_path / "repo"
    _init_repo(repo)
    _git(repo, "checkout", "-q", "-b", "issue-1-iets")
    (repo / ".claude").mkdir(parents=True, exist_ok=True)
    (repo / ".claude" / "allow-main").touch()

    result = _run(COMMIT_GUARD, _bash("git push origin $BR", repo), cwd=repo)

    assert result.returncode == 0, result.stderr


# --- N5: the fourth preflight limb -------------------------------------------


def test_commit_guard_fails_loud_without_cat(tmp_path):
    """N5: `cat` reads the payload from stdin; without it there is no command.

    The same requirement as for jq/python3/git — otherwise nothing pins this limb and it can
    silently go away (I8 again).
    """
    repo = tmp_path / "repo"
    _init_repo(repo)

    result = _run(
        COMMIT_GUARD,
        _bash('git commit -m "iets"', repo),
        cwd=repo,
        env={"PATH": str(_tool_bin(tmp_path / "zonder-cat", drop="cat"))},
    )

    assert result.returncode == 2, result.stdout
    assert "COULD NOT CHECK" in result.stderr
    assert "missing tooling" in result.stderr
    assert "cat" in result.stderr


# --- N6: cd that fails, cd -, pushd/popd -------------------------------------


def test_commit_guard_stays_put_when_cd_cannot_succeed(tmp_path):
    """N6: `cd <does not exist>` does not move bash — the hook took over the path.

    That way `cd typo; git commit` landed on main without the hook seeing it.
    """
    repo = tmp_path / "repo"
    _init_repo(repo)

    result = _run(
        COMMIT_GUARD,
        _bash(f"cd {tmp_path / 'bestaat-niet'}; git commit -m x", repo),
        cwd=repo,
    )

    assert result.returncode == 2, result.stdout


def test_commit_guard_passes_a_commit_in_a_directory_this_command_creates(tmp_path):
    """F3: the incantation that CLAUDE.md prescribes blocked itself.

    `git worktree add X && cd X && git commit` — X does not exist yet at hook time.
    The hook stayed where it was and inherited the branch of the OLD directory: main. That
    assigns a branch to a place where bash is not. After `&&` the
    commit runs only if the directory DID come into being, and a directory that comes into being
    OUTSIDE any repo has no branch that this hook has to protect.

    The boundary is at that ancestor, not at 'does not exist yet': a fresh directory
    UNDER a checkout DOES inherit its branch. See
    test_commit_guard_blocks_a_commit_in_a_new_dir_inside_a_checkout.

    The counterweight is in test_commit_guard_stays_put_when_cd_cannot_succeed:
    without `&&` the rest also runs if the `cd` failed, and then it
    still blocks.
    """
    repo = tmp_path / "repo"
    _init_repo(repo)
    wt = tmp_path / "wt"
    new_dir = tmp_path / "nieuw"

    incantation = _run(
        COMMIT_GUARD,
        _bash(f"git worktree add {wt} -b br2 && cd {wt} && git commit -m x", repo),
        cwd=repo,
    )
    assert incantation.returncode == 0, incantation.stderr

    verse_repo = _run(
        COMMIT_GUARD,
        _bash(
            f"mkdir -p {new_dir} && cd {new_dir} && git init && git commit -m init", repo
        ),
        cwd=repo,
    )
    assert verse_repo.returncode == 0, verse_repo.stderr

    # The asymmetry is deliberate: an untraceable `-C` path NAMES an existing
    # repo and keeps blocking. Do not harmonise those two.
    dash_c = _run(COMMIT_GUARD, _bash('git -C "$ELDERS" commit -m x', repo), cwd=repo)
    assert dash_c.returncode == 2, dash_c.stdout
    assert "COULD NOT CHECK" in dash_c.stderr

    # And a `cd` to an UNTRACEABLE path is something other than a directory that does not
    # exist yet: we know nothing about a variable, so that blocks too.
    variable = _run(
        COMMIT_GUARD, _bash('cd "$ELDERS" && git commit -m x', repo), cwd=repo
    )
    assert variable.returncode == 2, variable.stdout
    assert "COULD NOT CHECK" in variable.stderr


def test_commit_guard_blocks_a_commit_in_a_new_dir_inside_a_checkout(tmp_path):
    """C1: 'the directory does not exist yet' was too broad a free pass.

    `mkdir -p sub && cd sub && git commit` really commits on main — sub is a
    subdirectory of the same checkout, with the same branch. The exemption therefore looks at
    the nearest directory that DOES exist: if that lies in a repo,
    that repo applies. Only an ancestor outside any repo is free.

    Rows 5 and 6 follow from that: now that the fresh directory keeps a real path instead of
    'unknown', `cd ..` and `git -C ..` work relative to it again.
    """
    repo = tmp_path / "repo"
    _init_repo(repo)

    submap = _run(
        COMMIT_GUARD, _bash("mkdir -p sub && cd sub && git commit -m x", repo), cwd=repo
    )
    assert submap.returncode == 2, submap.stdout

    back = _run(
        COMMIT_GUARD,
        _bash("mkdir -p q3 && cd q3 && cd .. && git commit -m x", repo),
        cwd=repo,
    )
    assert back.returncode == 2, back.stdout

    dash_c_back = _run(
        COMMIT_GUARD,
        _bash("mkdir -p q4 && cd q4 && git -C .. commit -m x", repo),
        cwd=repo,
    )
    assert dash_c_back.returncode == 2, dash_c_back.stdout

    # Counterweight: a fresh directory OUTSIDE any repo stays free — that is the
    # prescribed worktree incantation and it must keep working.
    outside = _run(
        COMMIT_GUARD,
        _bash(
            f"mkdir -p {tmp_path / 'los'} && cd {tmp_path / 'los'} && git commit -m x",
            repo,
        ),
        cwd=repo,
    )
    assert outside.returncode == 0, outside.stderr


def test_commit_guard_blocks_a_push_to_main_from_a_dir_this_command_creates(tmp_path):
    """C1: a LITERAL main destination needs no directory knowledge.

    `git worktree add X -b br && cd X && git push origin main` pushes to main,
    wherever X lies. The exemption for a fresh directory outside any repo may not
    override that; it applies only if the destination is not main.
    """
    repo = tmp_path / "repo"
    _init_repo(repo)
    wt = tmp_path / "wtq"

    to_main = _run(
        COMMIT_GUARD,
        _bash(f"git worktree add {wt} -b brq && cd {wt} && git push origin main", repo),
        cwd=repo,
    )
    assert to_main.returncode == 2, to_main.stdout

    # Counterweight: the same form with an ordinary branch stays free.
    to_branch = _run(
        COMMIT_GUARD,
        _bash(f"git worktree add {wt} -b brq && cd {wt} && git push origin brq", repo),
        cwd=repo,
    )
    assert to_branch.returncode == 0, to_branch.stderr


def test_commit_guard_reads_ansi_c_quoting_while_scanning_for_heredocs(tmp_path):
    """F4, second call site: scan_line, not only split_segments.

    Without the `$'...'` branch the scanner closes the string at the escaped
    apostrophe, then reads `<<EOF` as a real heredoc opener and swallows the
    commit on the next line as heredoc body.
    """
    repo = tmp_path / "repo"
    _init_repo(repo)

    result = _run(
        COMMIT_GUARD,
        _bash("echo $'x\\'<<EOF'\ngit commit -m x", repo),
        cwd=repo,
    )

    assert result.returncode == 2, result.stdout


def test_commit_guard_does_not_read_a_wrapped_subshell_as_a_function(tmp_path):
    """F5 guard: `time (git commit -m x)` is no function definition.

    Shape and word count DO match — one word before a `( ... )` block — so
    without the requirement that this block is EMPTY (`()`, the brackets of the definition)
    the function isolation takes the commit out of the queue and nobody sees it.
    """
    repo = tmp_path / "repo"
    _init_repo(repo)

    result = _run(COMMIT_GUARD, _bash("time (git commit -m x)", repo), cwd=repo)

    assert result.returncode == 2, result.stdout


def test_commit_guard_forgets_an_and_after_the_segment_that_used_it(tmp_path):
    """F3 separator: `&&` applies to the NEXT segment, not further.

    With `... && cd <fresh dir>; git commit` there is no `&&` before the commit,
    so it also runs if the `cd` failed — and then bash is still in the old
    directory. If the `&&` flag of the previous segment lingers, the commit counts
    as 'in the fresh directory' and slips through the exemption.
    """
    repo = tmp_path / "repo"
    _init_repo(repo)
    outside = tmp_path / "buiten"

    without_and = _run(
        COMMIT_GUARD, _bash(f"echo hoi && cd {outside}; git commit -m x", repo), cwd=repo
    )
    assert without_and.returncode == 2, without_and.stdout

    # Counterweight: WITH `&&` before the commit the exemption keeps applying.
    met_and = _run(
        COMMIT_GUARD,
        _bash(f"echo hoi && cd {outside} && git commit -m x", repo),
        cwd=repo,
    )
    assert met_and.returncode == 0, met_and.stderr


def test_commit_guard_drops_the_new_dir_flag_on_a_following_cd(tmp_path):
    """apply_cd clears the flag: a second `cd` makes the previous fresh directory irrelevant.

    `mkdir -p n3 && cd n3 && cd "$ELSEWHERE" && git commit` — after the
    second `cd` bash is in a place the hook does not know, and that is a block. If
    the flag of the first `cd` stays, that unknown directory inherits the exemption.
    """
    repo = tmp_path / "repo"
    _init_repo(repo)
    outside = tmp_path / "buiten"

    result = _run(
        COMMIT_GUARD,
        _bash(
            f'mkdir -p {outside} && cd {outside} && cd "$ELDERS" && git commit -m x', repo
        ),
        cwd=repo,
    )

    assert result.returncode == 2, result.stdout
    assert "COULD NOT CHECK" in result.stderr


def test_commit_guard_does_not_follow_a_cd_in_a_function_body(tmp_path):
    """F5: a function body runs only at a CALL.

    `foo() { cd <main-checkout>; }; git commit` does not move bash — bash
    only defines. Following the `cd` as if it were executed blocked a
    commit that simply lands in the worktree.
    """
    main_repo = tmp_path / "hoofd"
    _init_repo(main_repo)
    feat = tmp_path / "feat"
    _init_repo(feat)
    _git(feat, "checkout", "-q", "-b", "issue-1-iets")

    definition = _run(
        COMMIT_GUARD,
        _bash("foo() { cd %s; }; git commit -m x" % main_repo, feat),
        cwd=feat,
    )
    assert definition.returncode == 0, definition.stderr

    # Counterweight: the commands INSIDE the body stay visible. Only the
    # directory state is isolated — `f() { git commit; }; f` really commits.
    call = _run(COMMIT_GUARD, _bash("f() { git commit -m x; }; f", main_repo), cwd=main_repo)
    assert call.returncode == 2, call.stdout


def test_commit_guard_follows_cd_dash_back(tmp_path):
    """N6: `cd -` jumps back to the previous directory."""
    repo = tmp_path / "repo"
    _init_repo(repo)
    wt = repo / ".claude" / "worktrees" / "issue-1-iets"
    _git(repo, "worktree", "add", "-q", "-b", "issue-1-iets", str(wt))

    there_and_back = _run(
        COMMIT_GUARD, _bash(f"cd {wt}\ncd -\ngit commit -m x", repo), cwd=repo
    )
    assert there_and_back.returncode == 2, there_and_back.stdout

    # Counterweight: without a jump back the commit stays in the worktree.
    stays = _run(COMMIT_GUARD, _bash(f"cd {wt}\ngit commit -m x", repo), cwd=repo)
    assert stays.returncode == 0, stays.stderr


def test_commit_guard_follows_pushd_and_popd(tmp_path):
    """N6: `pushd` moves just as well as `cd`, `popd` returns."""
    repo = tmp_path / "repo"
    _init_repo(repo)
    wt = repo / ".claude" / "worktrees" / "issue-1-iets"
    _git(repo, "worktree", "add", "-q", "-b", "issue-1-iets", str(wt))

    there = _run(COMMIT_GUARD, _bash(f"pushd {repo} && git commit -m x", wt), cwd=wt)
    assert there.returncode == 2, there.stdout

    back = _run(
        COMMIT_GUARD, _bash(f"pushd {repo} && popd && git commit -m x", wt), cwd=wt
    )
    assert back.returncode == 0, back.stderr


# --- N7: wrappers with arguments --------------------------------------------


def test_commit_guard_sees_through_wrappers_with_arguments(tmp_path):
    """N7: the wrapper list knew only argument-less forms.

    `bash -c '<script>'` is the most striking: the command was in a string and
    was never read as a command.
    """
    repo = tmp_path / "repo"
    _init_repo(repo)

    for cmd in (
        "bash -c 'git commit -m x'",
        'sh -c "git commit -m x"',
        "timeout 5 git commit -m x",
        "timeout -k 5 10 git commit -m x",
        "echo x | xargs git commit -m",
        "xargs -n1 git commit -m",
        "nice -n 10 git commit -m x",
    ):
        result = _run(COMMIT_GUARD, _bash(cmd, repo), cwd=repo)
        assert result.returncode == 2, f"{cmd!r} kwam er langs: {result.stdout}"


def test_commit_guard_keeps_a_shell_c_script_in_its_own_directory(tmp_path):
    """Counterweight to N7: `bash -c 'cd elsewhere'` does not move the parent."""
    repo = tmp_path / "repo"
    _init_repo(repo)
    wt = repo / ".claude" / "worktrees" / "issue-1-iets"
    _git(repo, "worktree", "add", "-q", "-b", "issue-1-iets", str(wt))

    result = _run(
        COMMIT_GUARD,
        _bash(f"bash -c 'cd {repo} && git status'\ngit commit -m x", wt),
        cwd=wt,
    )

    assert result.returncode == 0, result.stderr


# --- N8: a tilde between quotes --------------------------------------------


def test_commit_guard_does_not_expand_a_quoted_tilde(tmp_path):
    """N8: `'~/repo'` is to bash a relative path, not a home directory.

    The counter-image (`~/repo` without quotes) keeps expanding — see
    test_commit_guard_expands_tilde_in_dash_c.
    """
    repo = tmp_path / "repo"
    _init_repo(repo)
    elders = tmp_path / "elders"
    _init_repo(elders, branch="issue-1-iets")

    result = _run(
        COMMIT_GUARD,
        _bash("git -C '~/repo' commit -m \"iets\"", elders),
        cwd=elders,
        env={"HOME": str(tmp_path)},
    )

    assert result.returncode == 0, result.stderr


def test_guard_finds_its_lib_via_git_without_env(tmp_path):
    home = tmp_path / "project"
    init_repo(home)
    guard = install_core(home)
    env = {k: v for k, v in os.environ.items() if k != "LIGHTHOUSE_REPO_ROOT"}
    feature = tmp_path / "feat"
    init_repo(feature, branch="issue-1-x")
    result = subprocess.run(
        [str(guard)],
        input=json.dumps(bash_payload("git commit -m x", feature)),
        capture_output=True,
        text=True,
        cwd=feature,
        env=env,
        check=False,
    )
    assert result.returncode == 0, result.stderr


def test_guard_blocks_when_its_lib_is_missing(tmp_path):
    repo = tmp_path / "repo"
    init_repo(repo)
    result = run_hook(
        COMMIT_GUARD,
        bash_payload("git commit -m x", repo),
        repo,
        env={"LIGHTHOUSE_REPO_ROOT": str(tmp_path / "nowhere")},
    )
    assert result.returncode == 2
    assert "COULD NOT CHECK" in result.stderr


def _trunk_toml(tmp_path: Path) -> dict:
    toml = tmp_path / "lighthouse.toml"
    toml.write_text('[project]\nlanguage = "en"\n\n[forge]\ndefault_branch = "trunk"\n')
    return {"LIGHTHOUSE_TOML": str(toml)}


def test_guard_follows_default_branch(tmp_path):
    env = _trunk_toml(tmp_path)
    trunk = tmp_path / "trunk"
    _init_repo(trunk, branch="trunk")
    on_main = tmp_path / "on-main"
    _init_repo(on_main, branch="main")
    feature = tmp_path / "feature"
    _init_repo(feature, branch="issue-1-x")

    blocked = _run(COMMIT_GUARD, _bash("git commit -m x", trunk), trunk, env)
    assert blocked.returncode == 2 and "'trunk'" in blocked.stderr
    pushed = _run(COMMIT_GUARD, _bash("git push origin HEAD:trunk", feature), feature, env)
    assert pushed.returncode == 2 and "'trunk'" in pushed.stderr

    assert _run(COMMIT_GUARD, _bash("git commit -m x", on_main), on_main, env).returncode == 0
    assert (
        _run(COMMIT_GUARD, _bash("git push origin HEAD:main", feature), feature, env).returncode
        == 0
    )


def test_guard_push_all_looks_for_the_default_branch(tmp_path):
    env = _trunk_toml(tmp_path)
    repo = tmp_path / "repo"
    _init_repo(repo, branch="issue-1-x")
    _git(repo, "branch", "trunk")
    result = _run(COMMIT_GUARD, _bash("git push --all origin", repo), repo, env)
    assert result.returncode == 2


def test_guard_with_broken_config_blocks_a_commit_but_not_a_plain_command(tmp_path):
    toml = tmp_path / "lighthouse.toml"
    toml.write_text("[forge\n")
    env = {"LIGHTHOUSE_TOML": str(toml)}
    repo = tmp_path / "repo"
    _init_repo(repo, branch="issue-1-x")
    commit = _run(COMMIT_GUARD, _bash("git commit -m x", repo), repo, env)
    assert commit.returncode == 2
    assert "COULD NOT CHECK" in commit.stderr and "lighthouse.toml" in commit.stderr
    assert _run(COMMIT_GUARD, _bash("ls -la", repo), repo, env).returncode == 0


def test_guard_reads_not_a_repo_under_other_locale(tmp_path):
    """Git under another language still means 'not a repo' (inventaris §2.12)."""
    repo = tmp_path / "repo"
    _init_repo(repo, branch="issue-1-x")
    outside = tmp_path / "outside"
    outside.mkdir()
    env = {"LANG": "de_DE.UTF-8", "LC_ALL": "", "LANGUAGE": ""}
    probe = subprocess.run(
        ["git", "-C", str(outside), "rev-parse", "--show-toplevel"],
        capture_output=True,
        text=True,
        env=dict(os.environ, **env),
        check=False,
    )
    if "not a git repository" in probe.stderr:
        pytest.skip("git speaks no German here")
    result = _run(COMMIT_GUARD, _bash(f"git -C {outside} commit -m x", repo), repo, env)
    assert result.returncode == 0, result.stderr


def test_guard_names_the_python_cause_without_tomllib(tmp_path):
    # macOS' /usr/bin/python3 is 3.9: no tomllib. A tomllib that cannot be
    # imported, at the front of PYTHONPATH, imitates that python.
    shadow = tmp_path / "shadow"
    shadow.mkdir()
    (shadow / "tomllib.py").write_text("raise ImportError('no tomllib')\n", encoding="utf-8")
    repo = tmp_path / "repo"
    init_repo(repo, branch="feature")

    result = _run(
        GUARD, bash_payload("git commit -m x", repo), repo, {"PYTHONPATH": str(shadow)}
    )

    assert result.returncode == 2, result.stdout
    assert "COULD NOT CHECK" in result.stderr
    assert "python3 >= 3.11 is required" in result.stderr
    assert "Fix lighthouse.toml" not in result.stderr
