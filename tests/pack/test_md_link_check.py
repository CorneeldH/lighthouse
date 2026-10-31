"""pack/core md-link-check.sh, as installed (#886): does it block a dead or
absolute markdown link, and does it say so when it could not look?

The hook is bash and reads its payload as JSON on stdin. These tests run it
as a subprocess with real temporary paths, so the contract (exit code +
stderr) is tested and not a reimplementation of it.
"""

import json
import os
import shutil
import subprocess
from pathlib import Path

from tests.pack._testkit import INSTALLED_HOOKS, load_hook_lib, tool_bin


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
    """A real git repo with one commit, so the hook sees real plumbing."""
    path.mkdir(parents=True, exist_ok=True)
    _git(path, "init", "-q", "-b", branch)
    (path / "README.md").write_text("# repo\n", encoding="utf-8")
    _git(path, "add", "README.md")
    _git(path, "commit", "-qm", "init")


LINKCHECK = INSTALLED_HOOKS / "md-link-check.sh"


def _repo_with_worktree(tmp_path: Path) -> tuple[Path, Path]:
    repo = tmp_path / "repo"
    _init_repo(repo)
    (repo / "docs").mkdir()
    (repo / "docs" / "bestaat.md").write_text("# bestaat\n", encoding="utf-8")
    _git(repo, "add", "docs/bestaat.md")
    _git(repo, "commit", "-qm", "docs")
    wt = repo / ".claude" / "worktrees" / "issue-1-iets"
    _git(repo, "worktree", "add", "-q", "-b", "issue-1-iets", str(wt))
    return repo, wt


def test_linkcheck_allows_link_to_branch_only_spec(tmp_path):
    """AC 2 (#886): a relative link to a file that exists only on this
    branch is no longer a problem: the norm is relative-and-existing,
    not "also exists on main"."""
    _repo, wt = _repo_with_worktree(tmp_path)
    (wt / "docs" / "specs").mkdir()
    (wt / "docs" / "specs" / "x.md").write_text("# x\n", encoding="utf-8")
    (wt / "docs" / "plans").mkdir()
    plan = wt / "docs" / "plans" / "p.md"
    plan.write_text("Zie [spec](../specs/x.md) voor details.\n", encoding="utf-8")

    result = _run(
        LINKCHECK,
        {"tool_name": "Write", "cwd": str(wt), "tool_input": {"file_path": str(plan)}},
        cwd=wt,
    )

    assert result.returncode == 0, result.stderr
    assert load_hook_lib("md_links").absolute_links(plan.read_text()) == []


def test_linkcheck_allows_link_to_file_on_main(tmp_path):
    """Green, and demonstrably green because it looked, not because it bailed out."""
    _repo, wt = _repo_with_worktree(tmp_path)
    note = wt / "docs" / "note.md"
    note.write_text("Zie [bestaat](bestaat.md).\n", encoding="utf-8")

    result = _run(
        LINKCHECK,
        {"tool_name": "Write", "cwd": str(wt), "tool_input": {"file_path": str(note)}},
        cwd=wt,
        env={"LIGHTHOUSE_LINKCHECK_TRACE": "1"},
    )

    assert result.returncode == 0, result.stderr
    # The marker proves the checking branch was hit. Without this assertion
    # "rc 0" cannot be told apart from an early exit.
    assert "linkcheck: checking in" in result.stderr


def test_linkcheck_ignores_urls_and_anchors(tmp_path):
    _repo, wt = _repo_with_worktree(tmp_path)
    note = wt / "docs" / "note.md"
    note.write_text(
        "[web](https://example.com) [mail](mailto:t@example.com) [anker](#kop)\n",
        encoding="utf-8",
    )

    result = _run(
        LINKCHECK,
        {"tool_name": "Write", "cwd": str(wt), "tool_input": {"file_path": str(note)}},
        cwd=wt,
    )

    assert result.returncode == 0, result.stderr


def test_linkcheck_blocks_absolute_link(tmp_path):
    """An absolute path is always a problem: dead on the rendered site and
    as soon as the worktree is gone, whether or not the target exists."""
    _repo, wt = _repo_with_worktree(tmp_path)
    note = wt / "docs" / "note.md"
    note.write_text("Zie [x](/home/iemand/x.md).\n", encoding="utf-8")

    result = _run(
        LINKCHECK,
        {"tool_name": "Write", "cwd": str(wt), "tool_input": {"file_path": str(note)}},
        cwd=wt,
    )

    assert result.returncode == 2, result.stdout
    assert "/home/iemand/x.md" in result.stderr


def test_linkcheck_blocks_dead_relative_link(tmp_path):
    _repo, wt = _repo_with_worktree(tmp_path)
    note = wt / "docs" / "note.md"
    note.write_text("Zie [x](weg.md).\n", encoding="utf-8")

    result = _run(
        LINKCHECK,
        {"tool_name": "Write", "cwd": str(wt), "tool_input": {"file_path": str(note)}},
        cwd=wt,
    )

    assert result.returncode == 2, result.stdout
    assert "weg.md" in result.stderr


def test_linkcheck_ignores_links_inside_fenced_code_blocks(tmp_path):
    _repo, wt = _repo_with_worktree(tmp_path)
    note = wt / "docs" / "note.md"
    note.write_text(
        "Voorbeeld:\n\n```markdown\n[verzonnen](er-is-niets.md)\n```\n",
        encoding="utf-8",
    )

    result = _run(
        LINKCHECK,
        {"tool_name": "Write", "cwd": str(wt), "tool_input": {"file_path": str(note)}},
        cwd=wt,
    )

    assert result.returncode == 0, result.stderr


def test_linkcheck_ignores_link_in_tilde_fence(tmp_path):
    _repo, wt = _repo_with_worktree(tmp_path)
    note = wt / "docs" / "note.md"
    note.write_text(
        "Voorbeeld:\n\n~~~markdown\n[verzonnen](er-is-niets.md)\n~~~\n",
        encoding="utf-8",
    )

    result = _run(
        LINKCHECK,
        {"tool_name": "Write", "cwd": str(wt), "tool_input": {"file_path": str(note)}},
        cwd=wt,
    )

    assert result.returncode == 0, result.stderr


def test_linkcheck_skips_target_outside_repo(tmp_path):
    """A relative target that falls outside the repo (`ROOT`) is not this
    hook's problem: `md_links.py` skips it, whether it exists or not."""
    _repo, wt = _repo_with_worktree(tmp_path)
    note = wt / "docs" / "note.md"
    note.write_text("Zie [buiten](../../../../buiten-de-repo.md).\n", encoding="utf-8")

    result = _run(
        LINKCHECK,
        {"tool_name": "Write", "cwd": str(wt), "tool_input": {"file_path": str(note)}},
        cwd=wt,
    )

    assert result.returncode == 0, result.stderr


def test_linkcheck_checks_main_checkout_too(tmp_path):
    """The main checkout is no longer an exception: a dead relative link
    there blocks just as hard as in a worktree."""
    repo = tmp_path / "repo"
    _init_repo(repo)
    (repo / "docs").mkdir()
    note = repo / "docs" / "note.md"
    note.write_text("Zie [niets](er-is-niets.md).\n", encoding="utf-8")

    result = _run(
        LINKCHECK,
        {
            "tool_name": "Write",
            "cwd": str(repo),
            "tool_input": {"file_path": str(note)},
        },
        cwd=repo,
    )

    assert result.returncode == 2, result.stdout
    assert "er-is-niets.md" in result.stderr


def test_linkcheck_checks_other_project_too(tmp_path):
    """#1014: no project filter, unlike the test gate. A .md in another
    repo is tested against the root of ITS repo, even if
    CLAUDE_PROJECT_DIR points elsewhere."""
    project = tmp_path / "project"
    _init_repo(project)
    _repo, wt = _repo_with_worktree(tmp_path)
    note = wt / "docs" / "note.md"
    note.write_text("Zie [weg](weg.md).\n", encoding="utf-8")

    result = _run(
        LINKCHECK,
        {"tool_name": "Write", "cwd": str(wt), "tool_input": {"file_path": str(note)}},
        cwd=wt,
        env={"CLAUDE_PROJECT_DIR": str(project), "LIGHTHOUSE_LINKCHECK_TRACE": "1"},
    )

    assert result.returncode == 2
    assert f"linkcheck: checking in {wt.resolve()}" in result.stderr
    assert "weg.md" in result.stderr


def test_linkcheck_checks_own_project(tmp_path):
    """#1014, the second case: CLAUDE_PROJECT_DIR is the repo of the file."""
    repo, wt = _repo_with_worktree(tmp_path)
    note = wt / "docs" / "note.md"
    note.write_text("Zie [weg](weg.md).\n", encoding="utf-8")

    result = _run(
        LINKCHECK,
        {"tool_name": "Write", "cwd": str(wt), "tool_input": {"file_path": str(note)}},
        cwd=wt,
        env={"CLAUDE_PROJECT_DIR": str(repo), "LIGHTHOUSE_LINKCHECK_TRACE": "1"},
    )

    assert result.returncode == 2
    assert f"linkcheck: checking in {wt.resolve()}" in result.stderr


def test_linkcheck_ignores_non_markdown(tmp_path):
    _repo, wt = _repo_with_worktree(tmp_path)
    target = wt / "docs" / "script.py"
    target.write_text("# [niets](er-is-niets.md)\n", encoding="utf-8")

    result = _run(
        LINKCHECK,
        {
            "tool_name": "Write",
            "cwd": str(wt),
            "tool_input": {"file_path": str(target)},
        },
        cwd=wt,
    )

    assert result.returncode == 0, result.stderr


def test_linkcheck_fails_loud_without_tooling(tmp_path):
    """Blocking hook: missing tooling is 'did not look', not a green light."""
    _repo, wt = _repo_with_worktree(tmp_path)
    note = wt / "docs" / "note.md"
    note.write_text("Zie [bestaat](bestaat.md).\n", encoding="utf-8")
    empty_bin = tmp_path / "leeg-bin"
    empty_bin.mkdir()

    result = _run(
        LINKCHECK,
        {"tool_name": "Write", "cwd": str(wt), "tool_input": {"file_path": str(note)}},
        cwd=wt,
        env={"PATH": str(empty_bin)},
    )

    assert result.returncode == 2
    assert "COULD NOT CHECK" in result.stderr


def test_linkcheck_fails_loud_when_parser_crashes(tmp_path):
    """Blocking hook: the python link parser crashing (here: the target
    file is unreadable) is no more a green light than missing
    tooling."""
    _repo, wt = _repo_with_worktree(tmp_path)
    note = wt / "docs" / "note.md"
    note.write_text("Zie [bestaat](bestaat.md).\n", encoding="utf-8")
    os.chmod(note, 0o000)

    try:
        result = _run(
            LINKCHECK,
            {
                "tool_name": "Write",
                "cwd": str(wt),
                "tool_input": {"file_path": str(note)},
            },
            cwd=wt,
        )
    finally:
        os.chmod(note, 0o644)

    assert result.returncode == 2, result.stdout
    assert "COULD NOT CHECK" in result.stderr




# --- no silent exits (inventory §6) ---------------------------------------

_TOOLS = ("jq", "git", "python3", "cat", "dirname", "realpath")


def _note(tmp_path: Path) -> tuple[Path, dict]:
    _repo, wt = _repo_with_worktree(tmp_path)
    note = wt / "docs" / "note.md"
    note.write_text("Zie [bestaat](bestaat.md).\n", encoding="utf-8")
    return wt, {"tool_name": "Write", "cwd": str(wt), "tool_input": {"file_path": str(note)}}


def _assert_unchecked(result: subprocess.CompletedProcess) -> None:
    assert result.returncode == 2, result.stdout
    assert result.stderr.startswith("Linkcheck: COULD NOT CHECK — "), result.stderr


def test_linkcheck_invalid_json_is_unchecked(tmp_path):
    proc = subprocess.run(
        [str(LINKCHECK)], input="{nope", capture_output=True, text=True, cwd=tmp_path, check=False
    )
    _assert_unchecked(proc)


def test_linkcheck_without_git(tmp_path):
    wt, payload = _note(tmp_path)
    bin_dir = tool_bin(tmp_path / "bin", _TOOLS, drop="git")
    _assert_unchecked(_run(LINKCHECK, payload, cwd=wt, env={"PATH": str(bin_dir)}))


def test_linkcheck_unresolvable_path(tmp_path):
    wt, payload = _note(tmp_path)
    real = shutil.which("python3")
    bin_dir = tool_bin(
        tmp_path / "bin",
        _TOOLS,
        stubs={
            "realpath": "#!/bin/sh\nexit 1\n",
            "python3": f'#!/bin/sh\ncase "$1" in -c) exit 1;; esac\nexec {real} "$@"\n',
        },
    )
    _assert_unchecked(_run(LINKCHECK, payload, cwd=wt, env={"PATH": str(bin_dir)}))


def test_linkcheck_git_failure_is_unchecked(tmp_path):
    wt, payload = _note(tmp_path)
    real = shutil.which("git")
    bin_dir = tool_bin(
        tmp_path / "bin",
        _TOOLS,
        drop="git",
        stubs={
            "git": f'#!/bin/sh\ncase "$*" in *rev-parse*) echo "fatal: broken" >&2; exit 128;; esac\n'
            f'exec {real} "$@"\n'
        },
    )
    _assert_unchecked(_run(LINKCHECK, payload, cwd=wt, env={"PATH": str(bin_dir)}))


def test_linkcheck_outside_git_stays_silent(tmp_path):
    outside = tmp_path / "outside"
    outside.mkdir()
    note = outside / "note.md"
    note.write_text("[weg](weg.md)\n", encoding="utf-8")
    payload = {"tool_name": "Write", "cwd": str(outside), "tool_input": {"file_path": str(note)}}
    env = {"LANG": "de_DE.UTF-8", "LC_ALL": "", "LANGUAGE": ""}
    result = _run(LINKCHECK, payload, cwd=outside, env=env)
    assert result.returncode == 0, result.stderr


def test_linkcheck_names_the_problem_in_english(tmp_path):
    wt, payload = _note(tmp_path)
    Path(payload["tool_input"]["file_path"]).write_text("[x](weg.md) [y](/abs.md)\n")
    result = _run(LINKCHECK, payload, cwd=wt)
    assert result.returncode == 2
    assert "weg.md — does not exist" in result.stderr
    assert "/abs.md — an absolute path" in result.stderr
