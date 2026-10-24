"""The nightly arm of the ratchet: `cmd_nightly` with prcov and prcov_issues.

From the source repository's `test_quality_ratchet.py` (the nightly part). The board
comes from `lighthouse.toml` (`BOARD_TOML`) instead of the source repository's
controller; new are the cases without a workflow, without a board and with other
source directories.
"""

import json
import subprocess
import sys
from pathlib import Path

import pytest

from tests.pack.python.conftest import BOARD_TOML

_SCRIPTS = Path(__file__).resolve().parents[3] / "pack" / "python" / "scripts"
if str(_SCRIPTS) not in sys.path:
    sys.path.insert(0, str(_SCRIPTS))

from quality import prcov_issues, ratchet

NO_BOARD_TOML = BOARD_TOML[: BOARD_TOML.index("[forge.board]")]


@pytest.fixture(autouse=True)
def _board(lighthouse_toml, monkeypatch):
    monkeypatch.setenv("LIGHTHOUSE_HANDOFF_WORKTREE", "/srv/checkout")
    monkeypatch.delenv("GITHUB_RUN_ID", raising=False)
    return lighthouse_toml


_NIGHTLY_GIT_IDENTITY = {
    "GIT_AUTHOR_NAME": "Test",
    "GIT_AUTHOR_EMAIL": "test@example.invalid",
    "GIT_COMMITTER_NAME": "Test",
    "GIT_COMMITTER_EMAIL": "test@example.invalid",
}


class _NightlyRepo:
    """Minimale fixture-repo voor cmd_nightly — zelfde vorm als `_Repo` in
    test_prcov.py, hier lokaal zodat dit testbestand geen afhankelijkheid krijgt
    van een ander testmodule."""

    def __init__(self, root: Path) -> None:
        self.root = root
        self.initial_sha = ""

    def _run(self, args: list[str], env: dict[str, str] | None = None) -> str:
        from quality.gitenv import suite_env

        full_env = suite_env() | _NIGHTLY_GIT_IDENTITY | (env or {})
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

    def commit(self, files: dict[str, str], *, subject: str) -> str:
        for rel, content in files.items():
            path = self.root / rel
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(content, encoding="utf-8")
        self._run(["add", "-A"])
        self._run(["commit", "-q", "-m", subject])
        return self._run(["rev-parse", "HEAD"]).strip()

    def head(self) -> str:
        return self._run(["rev-parse", "HEAD"]).strip()


@pytest.fixture
def nightly_repo(tmp_path) -> _NightlyRepo:
    r = _NightlyRepo(tmp_path)
    r.init()
    r.initial_sha = r.commit({"src/a.py": "X = 0\n"}, subject="initial")
    return r


def nightly_baseline_text(min_diff_pct: int = 85, min_total_pct: int = 0) -> str:
    return f"""\
[ruff]
C901   = 0
TRY400 = 0
F401   = 0
F841   = 0
S110   = 0
PT011  = 0
S112   = 0

[complexipy]
cognitive_over_15 = 0

[coverage]
min_diff_pct = {min_diff_pct}
min_total_pct = {min_total_pct}
"""


def write_nightly_baseline(root: Path, **kwargs) -> None:
    (root / "quality-baseline.toml").write_text(
        nightly_baseline_text(**kwargs), encoding="utf-8"
    )


def nightly_report(total_pct: float, **files: dict[str, list[int]]) -> dict:
    return {"totals": {"percent_covered": total_pct}, "files": files}


def _lines(executed_lines: list[int], missing_lines: list[int]) -> dict:
    return {"executed_lines": executed_lines, "missing_lines": missing_lines}


def write_report(root: Path, report: dict) -> Path:
    path = root / "coverage.json"
    path.write_text(json.dumps(report), encoding="utf-8")
    return path


class _FakeGh:
    """Logt elke aanroep en geeft canned antwoorden terug — kleine, lokale
    kopie van `FakeGh` in test_prcov_issues.py. `queue` mag ook
    een Exception-instantie doorgeven, om een falende `gh`-aanroep te
    simuleren."""

    def __init__(self) -> None:
        self.calls: list[tuple[tuple[str, ...], dict[str, str] | None, str | None]] = []
        self._responses: dict[str, list] = {}

    def queue(self, key: str, *responses: object) -> None:
        self._responses.setdefault(key, []).extend(responses)

    @staticmethod
    def key(args: tuple[str, ...]) -> str:
        if args[0] == "api" and len(args) > 1 and args[1] == "graphql":
            return "graphql"
        if args[0] == "api":
            return "comments"
        return f"{args[0]} {args[1]}"

    def __call__(
        self,
        args,
        env: dict[str, str] | None = None,
        input: str | None = None,
    ) -> str:
        args = tuple(args)
        self.calls.append((args, dict(env) if env else None, input))
        k = self.key(args)
        queue = self._responses.get(k)
        if not queue:
            return ""
        item = queue[0] if len(queue) == 1 else queue.pop(0)
        if isinstance(item, BaseException):
            raise item
        return item

    def calls_for(
        self, group: str
    ) -> list[tuple[tuple[str, ...], dict | None, str | None]]:
        return [c for c in self.calls if self.key(c[0]) == group]


def _board_item(item_id: str, status: str | None) -> str:
    node = {
        "id": item_id,
        "project": {"id": "PVT_other"},
        "fieldValueByName": {"name": status} if status else None,
    }
    return json.dumps(
        {"data": {"repository": {"issue": {"projectItems": {"nodes": [node]}}}}}
    )


def _queue_happy_remediation(gh: _FakeGh, issue_url: str, item_id: str) -> None:
    """Alle vier stappen (issue, handoff, board) slagen in één keer."""
    gh.queue("issue list", "[]")
    gh.queue("issue create", issue_url)
    gh.queue("comments", "")
    gh.queue("project item-add", json.dumps({"id": item_id}))
    gh.queue("graphql", _board_item(item_id, "Controller"))


def test_nightly_floor_below_is_red(nightly_repo):
    """totals.percent_covered onder min_total_pct, geen PR's sinds since → rc 1."""
    write_nightly_baseline(nightly_repo.root, min_diff_pct=85, min_total_pct=90)
    report_path = write_report(nightly_repo.root, nightly_report(50.0))

    gh = _FakeGh()
    rc = ratchet.cmd_nightly(
        nightly_repo.root,
        str(report_path),
        nightly_repo.initial_sha,
        None,
        False,
        gh=gh,
    )

    assert rc == 1
    assert gh.calls == [], (
        "geen commits sinds since — er is niets om gh voor aan te roepen"
    )


def test_nightly_opens_an_issue_below_the_threshold(nightly_repo, monkeypatch, capsys):
    monkeypatch.setenv(prcov_issues.BOARD_TOKEN_ENV, "tok")
    write_nightly_baseline(nightly_repo.root, min_diff_pct=85, min_total_pct=0)
    nightly_repo.commit(
        {"src/a.py": "X = 0\nY = 1\nA = 2\nB = 3\n"}, subject="Voeg iets toe (#11)"
    )
    report_path = write_report(
        nightly_repo.root,
        nightly_report(95.0, **{"src/a.py": _lines([2], [3, 4])}),
    )

    gh = _FakeGh()
    _queue_happy_remediation(gh, "https://github.com/x/y/issues/501", "PVTI_a")

    rc = ratchet.cmd_nightly(
        nightly_repo.root,
        str(report_path),
        nightly_repo.initial_sha,
        None,
        False,
        gh=gh,
    )

    assert rc == 0
    assert len(gh.calls_for("issue create")) == 1
    assert len(gh.calls_for("issue comment")) == 1
    assert len(gh.calls_for("project item-add")) == 1
    out = capsys.readouterr().out
    assert "ratchet: PR #11 33.3% (1/3)" in out


def test_nightly_min_diff_pct_override_beats_the_baseline(nightly_repo, monkeypatch):
    """Baseline op 85: 90% is groen. Override naar 100: dezelfde 90% is rood."""
    monkeypatch.setenv(prcov_issues.BOARD_TOKEN_ENV, "tok")
    write_nightly_baseline(nightly_repo.root, min_diff_pct=85, min_total_pct=0)
    content = "X = 0\n" + "\n".join(f"L{i} = {i}" for i in range(10)) + "\n"
    nightly_repo.commit({"src/a.py": content}, subject="Tien regels (#11)")
    report_path = write_report(
        nightly_repo.root,
        nightly_report(95.0, **{"src/a.py": _lines(list(range(2, 11)), [11])}),
    )

    without_override = ratchet.cmd_nightly(
        nightly_repo.root,
        str(report_path),
        nightly_repo.initial_sha,
        None,
        False,
        gh=_FakeGh(),
    )
    assert without_override == 0

    gh = _FakeGh()
    _queue_happy_remediation(gh, "https://github.com/x/y/issues/502", "PVTI_b")
    with_override = ratchet.cmd_nightly(
        nightly_repo.root,
        str(report_path),
        nightly_repo.initial_sha,
        100,
        False,
        gh=gh,
    )

    assert with_override == 0
    assert len(gh.calls_for("issue create")) == 1


def test_nightly_keeps_going_after_one_pr_fails_and_ends_red(
    nightly_repo, monkeypatch, capsys
):
    monkeypatch.setenv(prcov_issues.BOARD_TOKEN_ENV, "tok")
    write_nightly_baseline(nightly_repo.root, min_diff_pct=85, min_total_pct=0)
    nightly_repo.commit(
        {"src/a.py": "X = 0\nY = 1\nA = 2\nB = 3\n"}, subject="Eerste (#11)"
    )
    nightly_repo.commit({"src/b.py": "Y = 0\nW = 1\n"}, subject="Tweede (#12)")
    report_path = write_report(
        nightly_repo.root,
        nightly_report(
            95.0,
            **{
                "src/a.py": _lines([2], [3, 4]),
                "src/b.py": _lines([], [2]),
            },
        ),
    )

    gh = _FakeGh()
    gh.queue(
        "issue list",
        subprocess.CalledProcessError(1, ["gh"], output="", stderr="gh: boom"),
        "[]",
    )
    _queue_happy_remediation(gh, "https://github.com/x/y/issues/503", "PVTI_c")
    # _queue_happy_remediation queuet zelf ook "issue list"; de eerste (falende)
    # entry hierboven staat er dus voor.

    rc = ratchet.cmd_nightly(
        nightly_repo.root,
        str(report_path),
        nightly_repo.initial_sha,
        None,
        False,
        gh=gh,
    )

    captured = capsys.readouterr()
    assert rc == 1
    assert "ratchet: PR #11" in captured.out
    assert "ratchet: PR #12" in captured.out
    assert "PR #11:" in captured.err
    assert len(gh.calls_for("issue create")) == 1  # alleen PR #12 kwam er doorheen


def test_nightly_keeps_going_after_a_pr_with_an_unreadable_report_entry(
    nightly_repo, monkeypatch, capsys
):
    """M1: `src/missing.py` ontbreekt uit het coverage-rapport (report en diff
    komen niet uit dezelfde run) — `diffcov.diff_coverage` gooit
    `CoverageReportError` voor die ene PR. `cmd_nightly` moet die vangen,
    doorgaan met de volgende PR (die zijn issue alsnog krijgt) én de
    vloercontrole nog uitvoeren, in plaats van de hele nacht ongevangen te
    laten crashen (geen issues, geen vloercontrole, watermerk blijft steken)."""
    monkeypatch.setenv(prcov_issues.BOARD_TOKEN_ENV, "tok")
    write_nightly_baseline(nightly_repo.root, min_diff_pct=85, min_total_pct=0)
    nightly_repo.commit({"src/missing.py": "X = 0\nY = 1\n"}, subject="Kapot (#11)")
    nightly_repo.commit(
        {"src/a.py": "X = 0\nY = 1\nA = 2\nB = 3\n"}, subject="Gezond (#12)"
    )
    # src/missing.py ontbreekt bewust uit "files" — alsof het rapport uit een
    # andere run komt dan de diff.
    report_path = write_report(
        nightly_repo.root,
        nightly_report(95.0, **{"src/a.py": _lines([2], [3, 4])}),
    )

    gh = _FakeGh()
    _queue_happy_remediation(gh, "https://github.com/x/y/issues/504", "PVTI_d")

    rc = ratchet.cmd_nightly(
        nightly_repo.root,
        str(report_path),
        nightly_repo.initial_sha,
        None,
        False,
        gh=gh,
    )

    captured = capsys.readouterr()
    assert rc == 1
    assert "PR #11:" in captured.err
    assert "no report entry" in captured.err
    assert "ratchet: PR #12" in captured.out
    assert len(gh.calls_for("issue create")) == 1  # PR #12 kwam er alsnog doorheen


@pytest.mark.parametrize("token", [None, ""])
def test_nightly_without_board_token_creates_the_issue_and_is_red(
    nightly_repo, monkeypatch, token, capsys
):
    if token is None:
        monkeypatch.delenv(prcov_issues.BOARD_TOKEN_ENV, raising=False)
    else:
        monkeypatch.setenv(prcov_issues.BOARD_TOKEN_ENV, token)
    # Vloer groen (min_total_pct=0): de rode rc moet hier zuiver van het
    # ontbrekende token komen, niet van de vloer — anders bewijst deze test
    # niets over de token-tak zelf (review ronde 1, bevinding 1). Een
    # ontbrekend token is wél degelijk fataal: een groene run zou het
    # watermerk optillen en de volgende nacht reconcilieert de board-stap dan
    # nooit meer.
    write_nightly_baseline(nightly_repo.root, min_diff_pct=85, min_total_pct=0)
    nightly_repo.commit(
        {"src/a.py": "X = 0\nY = 1\nA = 2\nB = 3\n"}, subject="Iets (#11)"
    )
    report_path = write_report(
        nightly_repo.root,
        nightly_report(95.0, **{"src/a.py": _lines([2], [3, 4])}),
    )

    gh = _FakeGh()
    gh.queue("issue list", "[]")
    gh.queue("issue create", "https://github.com/x/y/issues/504")
    gh.queue("comments", "")

    rc = ratchet.cmd_nightly(
        nightly_repo.root,
        str(report_path),
        nightly_repo.initial_sha,
        None,
        False,
        gh=gh,
    )
    captured = capsys.readouterr()

    assert rc == 1
    assert prcov_issues.BOARD_TOKEN_ENV in captured.err
    assert len(gh.calls_for("issue create")) == 1
    assert len(gh.calls_for("issue comment")) == 1
    assert not gh.calls_for("project item-add")
    assert not gh.calls_for("graphql")
    assert not gh.calls_for("project item-edit")


def test_nightly_a_refused_board_write_names_the_token_and_keeps_going(
    nightly_repo, monkeypatch, capsys
):
    """Token aanwezig, maar de gh-aanroep van ensure_on_board zelf faalt (het
    token mag bijvoorbeeld niet schrijven). De melding moet BOARD_TOKEN_ENV
    met naam noemen — anders wijst niets naar de oorzaak — en de volgende PR
    moet nog steeds verwerkt worden."""
    monkeypatch.setenv(prcov_issues.BOARD_TOKEN_ENV, "tok")
    write_nightly_baseline(nightly_repo.root, min_diff_pct=85, min_total_pct=0)
    nightly_repo.commit(
        {"src/a.py": "X = 0\nY = 1\nA = 2\nB = 3\n"}, subject="Eerste (#11)"
    )
    nightly_repo.commit({"src/b.py": "Y = 0\nW = 1\n"}, subject="Tweede (#12)")
    report_path = write_report(
        nightly_repo.root,
        nightly_report(
            95.0,
            **{
                "src/a.py": _lines([2], [3, 4]),
                "src/b.py": _lines([], [2]),
            },
        ),
    )

    gh = _FakeGh()
    gh.queue("issue list", "[]")
    gh.queue("issue create", "https://github.com/x/y/issues/505")
    gh.queue("comments", "")
    gh.queue("project item-add", json.dumps({"id": "PVTI_d"}))
    gh.queue(
        "graphql",
        subprocess.CalledProcessError(1, ["gh"], output="", stderr="HTTP 403"),
    )
    _queue_happy_remediation(gh, "https://github.com/x/y/issues/506", "PVTI_e")

    rc = ratchet.cmd_nightly(
        nightly_repo.root,
        str(report_path),
        nightly_repo.initial_sha,
        None,
        False,
        gh=gh,
    )
    captured = capsys.readouterr()

    assert rc == 1
    assert "PR #11" in captured.err
    assert prcov_issues.BOARD_TOKEN_ENV in captured.err
    assert "ratchet: PR #12" in captured.out
    assert len(gh.calls_for("issue create")) == 2  # beide PR's kwamen er doorheen


def test_nightly_dry_run_mutates_nothing(nightly_repo, monkeypatch):
    monkeypatch.setenv(prcov_issues.BOARD_TOKEN_ENV, "tok")
    write_nightly_baseline(nightly_repo.root, min_diff_pct=85, min_total_pct=0)
    nightly_repo.commit(
        {"src/a.py": "X = 0\nY = 1\nA = 2\nB = 3\n"}, subject="Iets (#11)"
    )
    report_path = write_report(
        nightly_repo.root,
        nightly_report(95.0, **{"src/a.py": _lines([2], [3, 4])}),
    )

    gh = _FakeGh()
    rc = ratchet.cmd_nightly(
        nightly_repo.root, str(report_path), nightly_repo.initial_sha, None, True, gh=gh
    )

    assert rc == 0
    assert gh.calls == [], (
        "een --dry-run mag geen enkele gh-aanroep doen die iets wijzigt — hier "
        "helemaal geen, want since is expliciet gegeven"
    )


def _one_pr_below(nightly_repo, files=None) -> Path:
    write_nightly_baseline(nightly_repo.root, min_diff_pct=85, min_total_pct=0)
    nightly_repo.commit(files or {"src/a.py": "X = 0\nY = 1\nA = 2\nB = 3\n"}, subject="Iets (#11)")
    return write_report(nightly_repo.root, nightly_report(95.0, **{"src/a.py": _lines([2], [3, 4])}))


def test_nightly_without_workflow_is_loud(nightly_repo, monkeypatch, capsys):
    """No `python.nightly_workflow`: no watermark, and the run says so."""
    monkeypatch.setenv(prcov_issues.BOARD_TOKEN_ENV, "tok")
    report_path = _one_pr_below(nightly_repo)
    gh = _FakeGh()
    _queue_happy_remediation(gh, "https://github.com/x/y/issues/507", "PVTI_f")
    rc = ratchet.cmd_nightly(nightly_repo.root, str(report_path), None, None, False, gh=gh)
    captured = capsys.readouterr()
    assert rc == 0, captured.err
    assert "nightly: no python.nightly_workflow; judging the merges of the last 24 hours" in captured.err
    assert not gh.calls_for("run list")
    assert "ratchet: PR #11" in captured.out


def test_nightly_without_a_successful_run_says_so(nightly_repo, lighthouse_toml, monkeypatch, capsys):
    lighthouse_toml(BOARD_TOML + '\n[python]\nnightly_workflow = "nightly.yml"\n')
    monkeypatch.setenv(prcov_issues.BOARD_TOKEN_ENV, "tok")
    report_path = _one_pr_below(nightly_repo)
    gh = _FakeGh()
    gh.queue("run list", "[]")
    _queue_happy_remediation(gh, "https://github.com/x/y/issues/508", "PVTI_g")
    rc = ratchet.cmd_nightly(nightly_repo.root, str(report_path), None, None, False, gh=gh)
    captured = capsys.readouterr()
    assert rc == 0, captured.err
    [run_list] = gh.calls_for("run list")
    assert "nightly.yml" in run_list[0]
    assert "no successful run of nightly.yml" in captured.err
    assert "judging the merges of the last 24 hours" in captured.err


def test_no_board_still_files_issue(nightly_repo, lighthouse_toml, monkeypatch, capsys):
    """Without `[forge.board]`: issue and handoff, no board call, no token needed, green."""
    lighthouse_toml(NO_BOARD_TOML)
    monkeypatch.delenv(prcov_issues.BOARD_TOKEN_ENV, raising=False)
    report_path = _one_pr_below(nightly_repo)
    gh = _FakeGh()
    gh.queue("issue list", "[]")
    gh.queue("issue create", "https://github.com/x/y/issues/509")
    gh.queue("comments", "")
    rc = ratchet.cmd_nightly(nightly_repo.root, str(report_path), nightly_repo.initial_sha, None, False, gh=gh)
    captured = capsys.readouterr()
    assert rc == 0, captured.err
    assert len(gh.calls_for("issue create")) == 1
    assert len(gh.calls_for("issue comment")) == 1
    assert not gh.calls_for("project item-add")
    assert not gh.calls_for("graphql")
    assert "board: skipped — no [forge.board] configured" in captured.out


def test_nightly_follows_source_dirs(nightly_repo, lighthouse_toml, monkeypatch, capsys):
    lighthouse_toml(BOARD_TOML + '\n[python]\nsource_dirs = ["lib"]\n')
    monkeypatch.setenv(prcov_issues.BOARD_TOKEN_ENV, "tok")
    write_nightly_baseline(nightly_repo.root, min_diff_pct=85, min_total_pct=0)
    nightly_repo.commit({"src/a.py": "X = 0\nY = 1\n", "lib/m.py": "A = 1\nB = 2\n"}, subject="Twee (#11)")
    report_path = write_report(
        nightly_repo.root,
        nightly_report(95.0, **{"src/a.py": _lines([], [2]), "lib/m.py": _lines([1, 2], [])}),
    )
    gh = _FakeGh()
    rc = ratchet.cmd_nightly(nightly_repo.root, str(report_path), nightly_repo.initial_sha, None, False, gh=gh)
    out = capsys.readouterr().out
    assert rc == 0
    assert "ratchet: PR #11 100.0% (2/2)" in out
    assert gh.calls == []
