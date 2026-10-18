"""De vier instructie-hooks (#624): waarschuwen/blokkeren ze op het juiste moment?

De hooks zijn bash en lezen hun payload als JSON op stdin. Deze tests draaien ze
als subprocess met echte tijdelijke paden, zodat het contract (exit code +
stderr/stdout) getest wordt en niet een herimplementatie ervan.
"""

import json
import os
import shutil
import subprocess
from pathlib import Path

from tests.unit._hook_testkit import load_hook_lib

HOOKS = Path(__file__).resolve().parents[2] / ".claude" / "hooks"
VAULT_GUARD = HOOKS / "vault-write-guard.sh"


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


WORKTREE_GUARD = HOOKS / "worktree-guard.sh"

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
    """Echte git-repo met één commit, zodat de hook echte plumbing ziet."""
    path.mkdir(parents=True, exist_ok=True)
    _git(path, "init", "-q", "-b", branch)
    (path / "README.md").write_text("# repo\n", encoding="utf-8")
    _git(path, "add", "README.md")
    _git(path, "commit", "-qm", "init")


LINKCHECK = HOOKS / "worktree-linkcheck.sh"


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
    """AC 2 (#886): een relatieve link naar een bestand dat alleen op deze
    branch bestaat is geen probleem meer — de norm is relatief-en-bestaand,
    niet "bestaat ook op main"."""
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
    """Groen — en aantoonbaar groen omdat er gekeken is, niet omdat hij afhaakte."""
    _repo, wt = _repo_with_worktree(tmp_path)
    note = wt / "docs" / "note.md"
    note.write_text("Zie [bestaat](bestaat.md).\n", encoding="utf-8")

    result = _run(
        LINKCHECK,
        {"tool_name": "Write", "cwd": str(wt), "tool_input": {"file_path": str(note)}},
        cwd=wt,
        env={"PAIA_LINKCHECK_TRACE": "1"},
    )

    assert result.returncode == 0, result.stderr
    # De marker bewijst dat de toets-tak geraakt is. Zonder deze assertie is
    # "rc 0" niet te onderscheiden van een vroege exit.
    assert "linkcheck: toetst in" in result.stderr


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
    """Een absoluut pad is altijd een probleem: dood op de gerenderde site en
    zodra de worktree weg is — ongeacht of het doel bestaat."""
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
    """Een relatief doel dat buiten de repo (`ROOT`) valt is niet dit
    hookprobleem — `md_links.py` slaat het over, ongeacht bestaan."""
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
    """De hoofdcheckout is geen uitzondering meer: een dode relatieve link
    daar blokkeert net zo hard als in een worktree."""
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
    """#1014: geen projectfilter, anders dan de testgate. Een .md in een andere
    repo wordt getoetst tegen de wortel van zíjn repo, ook als
    CLAUDE_PROJECT_DIR elders wijst."""
    project = tmp_path / "project"
    _init_repo(project)
    _repo, wt = _repo_with_worktree(tmp_path)
    note = wt / "docs" / "note.md"
    note.write_text("Zie [weg](weg.md).\n", encoding="utf-8")

    result = _run(
        LINKCHECK,
        {"tool_name": "Write", "cwd": str(wt), "tool_input": {"file_path": str(note)}},
        cwd=wt,
        env={"CLAUDE_PROJECT_DIR": str(project), "PAIA_LINKCHECK_TRACE": "1"},
    )

    assert result.returncode == 2
    assert f"linkcheck: toetst in {wt.resolve()}" in result.stderr
    assert "weg.md" in result.stderr


def test_linkcheck_checks_own_project(tmp_path):
    """#1014, het tweede geval: CLAUDE_PROJECT_DIR is de repo van het bestand."""
    repo, wt = _repo_with_worktree(tmp_path)
    note = wt / "docs" / "note.md"
    note.write_text("Zie [weg](weg.md).\n", encoding="utf-8")

    result = _run(
        LINKCHECK,
        {"tool_name": "Write", "cwd": str(wt), "tool_input": {"file_path": str(note)}},
        cwd=wt,
        env={"CLAUDE_PROJECT_DIR": str(repo), "PAIA_LINKCHECK_TRACE": "1"},
    )

    assert result.returncode == 2
    assert f"linkcheck: toetst in {wt.resolve()}" in result.stderr


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
    """Blokkerende hook: ontbrekende tooling is 'niet gekeken', geen groen licht."""
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
    assert "KON NIET CONTROLEREN" in result.stderr


def test_linkcheck_fails_loud_when_parser_crashes(tmp_path):
    """Blokkerende hook: de python-linkparser die op zijn bek gaat (hier: het
    doelbestand is onleesbaar) is evenmin een groen licht als ontbrekende
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
    assert "KON NIET CONTROLEREN" in result.stderr


