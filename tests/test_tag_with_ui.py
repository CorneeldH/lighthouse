"""ci/tag-with-ui.sh: a release tag whose commit carries the built SPA in internal/web/dist."""

from __future__ import annotations

import os
import subprocess
from pathlib import Path

REPO = Path(__file__).resolve().parents[1]
SCRIPT = REPO / "ci" / "tag-with-ui.sh"
TAG = "v1.2.3"


def _env(repo: Path) -> dict[str, str]:
    # Switch off the user's git configuration: a global `commit.gpgsign` must not steer the tests.
    return {
        **os.environ,
        "HOME": str(repo),
        "GIT_CONFIG_GLOBAL": "/dev/null",
        "GIT_CONFIG_SYSTEM": "/dev/null",
    }


def _git(repo: Path, *args: str) -> str:
    return subprocess.run(
        ["git", "-C", str(repo), *args],
        capture_output=True,
        text=True,
        check=True,
        env=_env(repo),
    ).stdout.strip()


def _repo(tmp_path: Path) -> Path:
    repo = tmp_path / "repo"
    repo.mkdir()
    _git(repo, "init", "-q", "-b", "main")
    _git(repo, "config", "user.name", "Test")
    _git(repo, "config", "user.email", "test@example.com")
    (repo / "README.md").write_text("hello\n")
    _git(repo, "add", "README.md")
    _git(repo, "commit", "-q", "-m", "init")
    return repo


def _src(tmp_path: Path, *, index: bool = True) -> Path:
    src = tmp_path / "src"
    (src / "assets").mkdir(parents=True)
    if index:
        (src / "index.html").write_text("<html></html>\n")
    (src / "assets" / "app.js").write_text("console.log(1)\n")
    return src


def _run(repo: Path, src: Path) -> subprocess.CompletedProcess[str]:
    return subprocess.run(
        [str(SCRIPT), TAG, str(src)],
        cwd=repo,
        capture_output=True,
        text=True,
        env=_env(repo),
    )


def _assert_tag_has_dist(repo: Path) -> None:
    files = _git(repo, "ls-tree", "-r", TAG, "--name-only").splitlines()
    assert "internal/web/dist/index.html" in files
    assert "internal/web/dist/assets/app.js" in files
    assert _git(repo, "cat-file", "-t", TAG) == "tag"
    assert _git(repo, "log", "-1", "--format=%s", f"{TAG}^{{commit}}") == (
        f"chore(release): {TAG} web-UI"
    )


def test_tag_points_to_commit_with_dist(tmp_path: Path) -> None:
    repo = _repo(tmp_path)
    result = _run(repo, _src(tmp_path))
    assert result.returncode == 0, result.stderr
    _assert_tag_has_dist(repo)


def test_parent_has_no_dist(tmp_path: Path) -> None:
    repo = _repo(tmp_path)
    assert _run(repo, _src(tmp_path)).returncode == 0
    parent = _git(repo, "ls-tree", "-r", f"{TAG}^", "--name-only")
    assert "internal/web/dist" not in parent


def test_fails_without_index_html(tmp_path: Path) -> None:
    repo = _repo(tmp_path)
    head = _git(repo, "rev-parse", "HEAD")
    result = _run(repo, _src(tmp_path, index=False))
    assert result.returncode != 0
    assert _git(repo, "tag", "--list") == ""
    assert _git(repo, "rev-parse", "HEAD") == head


def test_second_run_after_reset_works(tmp_path: Path) -> None:
    repo = _repo(tmp_path)
    src = _src(tmp_path)
    assert _run(repo, src).returncode == 0
    _git(repo, "tag", "-d", TAG)
    _git(repo, "reset", "-q", "--hard", "HEAD~1")
    result = _run(repo, src)
    assert result.returncode == 0, result.stderr
    _assert_tag_has_dist(repo)


def test_ignored_dist_is_forced(tmp_path: Path) -> None:
    repo = _repo(tmp_path)
    (repo / ".gitignore").write_text("/internal/web/dist/*\n")
    _git(repo, "add", ".gitignore")
    _git(repo, "commit", "-q", "-m", "ignore dist")
    result = _run(repo, _src(tmp_path))
    assert result.returncode == 0, result.stderr
    _assert_tag_has_dist(repo)


def test_script_is_tracked_executable() -> None:
    """Fails when git stops tracking ci/tag-with-ui.sh as executable."""
    out = subprocess.run(
        ["git", "ls-files", "-s", "ci/tag-with-ui.sh"],
        cwd=REPO,
        capture_output=True,
        text=True,
        check=True,
    ).stdout
    assert out.startswith("100755")


def test_stale_tracked_dist_file_is_dropped(tmp_path: Path) -> None:
    """Fails when the script stops clearing dist: a stale file from an earlier run stays in the tag."""
    repo = _repo(tmp_path)
    stale = repo / "internal" / "web" / "dist" / "stale.js"
    stale.parent.mkdir(parents=True)
    stale.write_text("old\n")
    _git(repo, "add", "-f", "internal/web/dist/stale.js")
    _git(repo, "commit", "-q", "-m", "earlier dist")
    result = _run(repo, _src(tmp_path))
    assert result.returncode == 0, result.stderr
    files = _git(repo, "ls-tree", "-r", TAG, "--name-only").splitlines()
    assert "internal/web/dist/stale.js" not in files
    _assert_tag_has_dist(repo)


def test_prestaged_unrelated_file_stays_out_of_the_tag_commit(tmp_path: Path) -> None:
    """Fails when the commit takes more than the dist path."""
    repo = _repo(tmp_path)
    (repo / "other.txt").write_text("unrelated\n")
    _git(repo, "add", "other.txt")
    result = _run(repo, _src(tmp_path))
    assert result.returncode == 0, result.stderr
    files = _git(repo, "ls-tree", "-r", TAG, "--name-only").splitlines()
    assert "other.txt" not in files
    _assert_tag_has_dist(repo)
