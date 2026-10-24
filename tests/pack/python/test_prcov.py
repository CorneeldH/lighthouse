"""Regels toewijzen aan hun squash-PR: `pr_number`, `select_commits`, `blame`,
`attribute`, `per_pr`.

Ontwerp: docs/developer/planning/specs/2026-09-26-ci-doorlooptijd-design.md
"""

import subprocess
import sys
import time
from pathlib import Path

import pytest

# scripts/ is geen geïnstalleerd pakket; zelfde vorm als test_quality_ratchet.py:14.
_SCRIPTS = Path(__file__).resolve().parents[2] / "scripts"
if str(_SCRIPTS) not in sys.path:
    sys.path.insert(0, str(_SCRIPTS))

from quality import prcov
from quality.gitenv import suite_env

# Vaste identiteit voor de fixture-commits — los van wie de suite draait, en
# los van de echte git-config (die kan ontbreken in CI).
_GIT_IDENTITY = {
    "GIT_AUTHOR_NAME": "Test",
    "GIT_AUTHOR_EMAIL": "test@example.invalid",
    "GIT_COMMITTER_NAME": "Test",
    "GIT_COMMITTER_EMAIL": "test@example.invalid",
}


class _Repo:
    """Dunne wrapper om een fixture-repo op te bouwen met squash-achtige commits.

    Elke git-aanroep gaat via `suite_env()` + `cwd=root`: zonder die schoonmaak
    schrijft een test die per ongeluk GIT_DIR uit de hook-omgeving erft in de
    ECHTE repo (zie gitenv.py).
    """

    def __init__(self, root: Path) -> None:
        self.root = root

    def _run(self, args: list[str], env: dict[str, str] | None = None) -> str:
        full_env = suite_env() | _GIT_IDENTITY | (env or {})
        proc = subprocess.run(
            ["git", *args],
            cwd=self.root,
            capture_output=True,
            text=True,
            env=full_env,
            check=False,
        )
        assert proc.returncode == 0, proc.stderr
        return proc.stdout

    def init(self) -> None:
        self._run(["init", "-q", "-b", "main"])

    def commit(
        self,
        files: dict[str, str] | None = None,
        *,
        subject: str,
        deletes: tuple[str, ...] = (),
        date: str | None = None,
    ) -> str:
        for rel in deletes:
            (self.root / rel).unlink()
        for rel, content in (files or {}).items():
            path = self.root / rel
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(content, encoding="utf-8")
        self._run(["add", "-A"])
        env = {}
        if date is not None:
            env = {"GIT_AUTHOR_DATE": date, "GIT_COMMITTER_DATE": date}
        self._run(["commit", "-q", "-m", subject], env=env)
        return self._run(["rev-parse", "HEAD"]).strip()


@pytest.fixture
def repo(tmp_path) -> _Repo:
    r = _Repo(tmp_path)
    r.init()
    r.commit(
        {"src/a.py": "X = 0\nY = 1\n", "src/b.py": "Y = 0\n"},
        subject="initial",
    )
    return r


def read_source(root: Path):
    def _read(path: str) -> str:
        return (root / path).read_text(encoding="utf-8")

    return _read


def report_for(**files: dict[str, list[int]]) -> dict:
    return {"files": files}


# --- pr_number ----------------------------------------------------------------


def test_pr_number_takes_the_last_ref():
    assert prcov.pr_number("Start-issue moves (#1007) (#1009)") == 1009
    assert prcov.pr_number("Geen ref") is None
    assert prcov.pr_number("(#12) staat vooraan") is None


# --- select_commits / attribute -------------------------------------------------


def test_two_squash_commits_are_attributed_per_pr(repo):
    repo.commit(
        {"src/a.py": "X = 0\nY = 1\nA = 2\nB = 3\n"}, subject="Voeg x toe (#11)"
    )
    repo.commit({"src/b.py": "Y = 0\nW = 1\n"}, subject="Voeg y toe (#12)")

    commits = prcov.select_commits(repo.root, None)
    result = prcov.attribute(repo.root, commits)

    assert result == {11: {"src/a.py": [3, 4]}, 12: {"src/b.py": [2]}}


def test_an_overwritten_line_moves_to_the_later_pr(repo):
    repo.commit(
        {"src/a.py": "X = 0\nY = 1\nA = 2\nB = 3\n"}, subject="Voeg x toe (#11)"
    )
    repo.commit(
        {"src/a.py": "X = 0\nY = 1\nA2 = 2\nB = 3\n"}, subject="Herschrijf x (#12)"
    )

    commits = prcov.select_commits(repo.root, None)
    result = prcov.attribute(repo.root, commits)

    assert result[11] == {"src/a.py": [4]}
    assert result[12] == {"src/a.py": [3]}


def test_a_pr_without_src_lines_is_not_measured(repo):
    repo.commit({"docs/readme.md": "hallo\n"}, subject="Doc-update (#13)")

    commits = prcov.select_commits(repo.root, None)
    attributed = prcov.attribute(repo.root, commits)
    result = prcov.per_pr(attributed, report_for(), read_source(repo.root), ())

    assert attributed[13] == {}
    assert result[13].total == 0


def test_an_excluded_prefix_is_not_counted(repo):
    repo.commit({"src/web/pages/x.py": "A = 1\nB = 2\n"}, subject="Nieuwe pagina (#14)")

    commits = prcov.select_commits(repo.root, None)
    attributed = prcov.attribute(repo.root, commits)
    report = report_for(
        **{"src/web/pages/x.py": {"executed_lines": [1, 2], "missing_lines": []}}
    )
    result = prcov.per_pr(
        attributed, report, read_source(repo.root), ("src/web/pages/",)
    )

    assert result[14].total == 0


def test_coverage_per_pr_uses_the_one_report(repo):
    repo.commit(
        {"src/a.py": "X = 0\nY = 1\nA = 2\nB = 3\n"}, subject="Voeg x toe (#11)"
    )
    repo.commit({"src/b.py": "Y = 0\nW = 1\n"}, subject="Voeg y toe (#12)")

    commits = prcov.select_commits(repo.root, None)
    attributed = prcov.attribute(repo.root, commits)
    report = report_for(
        **{
            "src/a.py": {"executed_lines": [4], "missing_lines": [3]},
            "src/b.py": {"executed_lines": [2], "missing_lines": []},
        }
    )
    result = prcov.per_pr(attributed, report, read_source(repo.root), ())

    assert result[11].pct == 50.0
    assert result[11].uncovered == {"src/a.py": [3]}


def test_a_commit_without_pr_ref_is_skipped(repo):
    repo.commit({"src/a.py": "X = 0\nY = 1\nA = 2\n"}, subject="Kleine fix zonder ref")

    commits = prcov.select_commits(repo.root, None)

    assert commits == []


def test_a_watermark_outside_history_falls_back_to_24h(repo):
    three_days_ago = int(time.time()) - 3 * 86400
    repo.commit(
        {"src/old.py": "A = 1\n"},
        subject="Oud werk (#99)",
        date=f"@{three_days_ago} +0000",
    )
    repo.commit(
        {"src/a.py": "X = 0\nY = 1\nA = 2\nB = 3\n"}, subject="Voeg x toe (#11)"
    )

    with_watermark = prcov.select_commits(repo.root, "0" * 40)
    without_watermark = prcov.select_commits(repo.root, None)

    assert with_watermark == without_watermark
    assert {c.pr for c in with_watermark} == {11}


def test_since_a_real_ancestor_excludes_it_and_everything_before(repo):
    """De primaire tak van `select_commits` (`since..HEAD`) — alle andere tests
    hier vallen terug op de 24u-fallback, dus zonder deze test heeft die tak
    geen dekking.

    `since` is een echte voorouder-sha (de watermark van een geslaagde
    nightly-run). #21 staat óp 3 dagen oud — buiten de 24u-fallback — maar wél
    ná `since` in de geschiedenis: alleen het bereik mag beslissen, niet de
    datum. `since` zelf (#20) en alles daarvóór (de initiële commit) vallen af.
    """
    since_sha = repo.commit(
        {"src/a.py": "X = 0\nY = 1\nA = 2\n"}, subject="Watermark (#20)"
    )
    three_days_ago = int(time.time()) - 3 * 86400
    old_but_newer_sha = repo.commit(
        {"src/old.py": "A = 1\n"},
        subject="Ouder dan 24u, maar na since (#21)",
        date=f"@{three_days_ago} +0000",
    )
    recent_sha = repo.commit(
        {"src/a.py": "X = 0\nY = 1\nA = 2\nB = 3\n"}, subject="Recent (#22)"
    )

    result = prcov.select_commits(repo.root, since_sha)

    assert [c.pr for c in result] == [22, 21]
    assert [c.sha for c in result] == [recent_sha, old_but_newer_sha]

    # Bewijs dat het bereik beslist, niet het datumvenster: de 24u-fallback
    # zou #21 juist laten vallen, want zijn datum ligt >24u terug.
    fallback = prcov.select_commits(repo.root, None)
    assert 21 not in {c.pr for c in fallback}


def test_a_file_deleted_since_is_skipped(repo):
    repo.commit({"src/c.py": "A = 1\n"}, subject="Nieuw bestand (#11)")
    repo.commit({}, subject="Verwijder bestand (#12)", deletes=("src/c.py",))

    commits = prcov.select_commits(repo.root, None)
    result = prcov.attribute(repo.root, commits)

    assert "src/c.py" not in result[11]
    assert "src/c.py" not in result[12]
